// Ergo TracingSpan to OTLP Span mapping.
//
// Each ergo TracingSpan observation point (Sent, Delivered, Processed) becomes
// a separate OTLP Span. Aggregation is not possible because Sent may be emitted
// on a different node than Delivered/Processed.
//
// OTLP SpanId is deterministic: ergoSpanID << 2 | uint64(Point).
// This ensures uniqueness within a TraceID (each point gets a distinct SpanId)
// and allows any node to compute the SpanId of any other point for the same message.
//
// ParentSpanId mapping:
//   - Sent: parent is Processed of parent message (ParentSpanID<<2|3),
//     or root span if ParentSpanID == 0
//   - Delivered: parent is Sent of same message (SpanID<<2|1)
//   - Processed: parent is Sent of same message (SpanID<<2|1)
//
// This produces a tree where Sent is the anchor for each message,
// with Delivered and Processed as its children. Response spans nest
// under Request.Processed, forming a natural call hierarchy.
//
// SpanId preservation: for remote messages, the ergo SpanID assigned by the
// sending node is preserved on the receiving node (no re-assignment). This means
// Sent, Delivered, and Processed for the same message share the same ergo SpanID
// regardless of which node they occur on.
//
// Example: A@nodeX sends x to B@nodeY during processing of w(SpanID=10):
//
//   nodeX: x.Sent       SpanId=42<<2|1  ParentSpanId=10<<2|3
//   nodeY: x.Delivered   SpanId=42<<2|2  ParentSpanId=42<<2|1
//   nodeY: x.Processed   SpanId=42<<2|3  ParentSpanId=42<<2|1
//
// Example: Call+Response A@nodeX -> B@nodeY:
//
//   nodeX: req.Sent       SpanId=42<<2|1  ParentSpanId=chain
//   nodeY: req.Delivered   SpanId=42<<2|2  ParentSpanId=42<<2|1
//   nodeY: req.Processed   SpanId=42<<2|3  ParentSpanId=42<<2|1
//   nodeY: resp.Sent       SpanId=77<<2|1  ParentSpanId=42<<2|3
//   nodeX: resp.Delivered   SpanId=77<<2|2  ParentSpanId=77<<2|1
//
// Example: Forward A@nodeX -> B@nodeY -> C@nodeZ -> response -> A:
//
//   nodeX: req.Sent        SpanId=42<<2|1  ParentSpanId=chain
//   nodeY: req.Delivered    SpanId=42<<2|2  ParentSpanId=42<<2|1
//   nodeY: req.Processed    SpanId=42<<2|3  ParentSpanId=42<<2|1
//   nodeY: fwd.Sent         SpanId=43<<2|1  ParentSpanId=42<<2|3
//   nodeZ: fwd.Delivered    SpanId=43<<2|2  ParentSpanId=43<<2|1
//   nodeZ: fwd.Processed    SpanId=43<<2|3  ParentSpanId=43<<2|1
//   nodeZ: resp.Sent        SpanId=77<<2|1  ParentSpanId=43<<2|3
//   nodeX: resp.Delivered    SpanId=77<<2|2  ParentSpanId=77<<2|1
//
// TracingKind + TracingPoint to OTLP SpanKind:
//   Send.Sent          -> PRODUCER
//   Send.Delivered      -> CONSUMER
//   Send.Processed      -> CONSUMER
//   Request.Sent        -> CLIENT
//   Request.Delivered   -> SERVER
//   Request.Processed   -> SERVER
//   Response.Sent       -> SERVER
//   Response.Delivered  -> CLIENT
//   Response.Processed  -> SERVER
//   Spawn               -> INTERNAL
//   Terminate           -> INTERNAL
//
//
// Attributes: ergo.span_id, ergo.point, ergo.kind, ergo.from, ergo.to,
//             ergo.message, ergo.node, ergo.ref (if non-empty)
// Resource:   service.name = node name
// Flags:      IS_REMOTE set when From.Node != span Node (cross-node delivery)
package pulse

import (
	"encoding/binary"
	"fmt"

	"ergo.services/ergo/gen"
	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
)

func buildExportRequest(spans []gen.TracingSpan, nodeName gen.Atom) *coltracepb.ExportTraceServiceRequest {
	otlpSpans := make([]*tracepb.Span, 0, len(spans))
	for i := range spans {
		otlpSpans = append(otlpSpans, convertSpan(&spans[i]))
	}

	return &coltracepb.ExportTraceServiceRequest{
		ResourceSpans: []*tracepb.ResourceSpans{
			{
				Resource: &resourcepb.Resource{
					Attributes: []*commonpb.KeyValue{
						stringAttr("service.name", string(nodeName)),
					},
				},
				ScopeSpans: []*tracepb.ScopeSpans{
					{
						Scope: &commonpb.InstrumentationScope{
							Name:    "ergo.services/pulse",
							Version: Version,
						},
						Spans: otlpSpans,
					},
				},
			},
		},
	}
}

func convertSpan(s *gen.TracingSpan) *tracepb.Span {
	// TraceID: [2]uint64 → [16]byte big-endian
	var traceID [16]byte
	binary.BigEndian.PutUint64(traceID[0:8], s.TraceID[0])
	binary.BigEndian.PutUint64(traceID[8:16], s.TraceID[1])

	// SpanId: deterministic from ergo SpanID + Point
	// Sent=1, Delivered=2, Processed=3 encoded in lower 2 bits
	var spanID [8]byte
	binary.BigEndian.PutUint64(spanID[:], s.SpanID<<2|uint64(s.Point))

	// ParentSpanId: Sent is the anchor for each message.
	// Delivered and Processed are children of Sent (SpanID<<2|1).
	// Sent's parent is the Processed point of the parent message (ParentSpanID<<2|3).
	// Terminate has only Processed (no Sent) — uses parent context directly.
	var parentSpanID []byte
	switch s.Point {
	case gen.TracingPointSent:
		if s.ParentSpanID != 0 {
			var p [8]byte
			binary.BigEndian.PutUint64(p[:], s.ParentSpanID<<2|uint64(gen.TracingPointProcessed))
			parentSpanID = p[:]
		}
	case gen.TracingPointDelivered:
		// child of Sent (same message)
		var p [8]byte
		binary.BigEndian.PutUint64(p[:], s.SpanID<<2|uint64(gen.TracingPointSent))
		parentSpanID = p[:]
	case gen.TracingPointProcessed:
		if s.Kind == gen.TracingKindTerminate {
			// Terminate has only Processed (no Sent/Delivered)
			if s.ParentSpanID != 0 {
				var p [8]byte
				binary.BigEndian.PutUint64(p[:], s.ParentSpanID<<2|uint64(gen.TracingPointProcessed))
				parentSpanID = p[:]
			}
		} else {
			// child of Sent (same message)
			var p [8]byte
			binary.BigEndian.PutUint64(p[:], s.SpanID<<2|uint64(gen.TracingPointSent))
			parentSpanID = p[:]
		}
	}

	// Name: "behavior kind.point message_type"
	name := fmt.Sprintf("%s.%s", s.Kind, s.Point)
	if s.Behavior != "" {
		name = s.Behavior + " " + name
	}
	if s.Message != "" {
		name = name + " " + s.Message
	}

	// Flags: set IS_REMOTE when span observed on different node than sender
	var flags uint32
	if s.From.Node != s.Node {
		flags = 0x00000300 // HAS_IS_REMOTE | IS_REMOTE
	}

	span := &tracepb.Span{
		TraceId:           traceID[:],
		SpanId:            spanID[:],
		ParentSpanId:      parentSpanID,
		Name:              name,
		Kind:              mapSpanKind(s.Kind, s.Point),
		StartTimeUnixNano: uint64(s.Timestamp),
		EndTimeUnixNano:   uint64(s.Timestamp),
		Attributes:        buildAttributes(s),
		Flags:             flags,
	}

	// error status
	if s.Error != "" {
		span.Status = &tracepb.Status{
			Code:    tracepb.Status_STATUS_CODE_ERROR,
			Message: s.Error,
		}
	}

	return span
}

func mapSpanKind(k gen.TracingKind, p gen.TracingPoint) tracepb.Span_SpanKind {
	switch k {
	case gen.TracingKindSend:
		if p == gen.TracingPointSent {
			return tracepb.Span_SPAN_KIND_PRODUCER
		}
		return tracepb.Span_SPAN_KIND_CONSUMER

	case gen.TracingKindRequest:
		if p == gen.TracingPointSent {
			return tracepb.Span_SPAN_KIND_CLIENT
		}
		return tracepb.Span_SPAN_KIND_SERVER

	case gen.TracingKindResponse:
		if p == gen.TracingPointDelivered {
			return tracepb.Span_SPAN_KIND_CLIENT
		}
		return tracepb.Span_SPAN_KIND_SERVER

	case gen.TracingKindSpawn, gen.TracingKindTerminate:
		return tracepb.Span_SPAN_KIND_INTERNAL
	}
	return tracepb.Span_SPAN_KIND_UNSPECIFIED
}

func buildAttributes(s *gen.TracingSpan) []*commonpb.KeyValue {
	attrs := []*commonpb.KeyValue{
		stringAttr("ergo.node", string(s.Node)),
		stringAttr("ergo.from", s.From.String()),
		stringAttr("ergo.to", fmt.Sprintf("%v", s.To)),
		stringAttr("ergo.kind", s.Kind.String()),
		stringAttr("ergo.point", s.Point.String()),
		uint64Attr("ergo.span_id", s.SpanID),
	}
	if s.Behavior != "" {
		attrs = append(attrs, stringAttr("ergo.behavior", s.Behavior))
	}
	if s.Message != "" {
		attrs = append(attrs, stringAttr("ergo.message", s.Message))
	}
	if s.Ref != (gen.Ref{}) {
		attrs = append(attrs, stringAttr("ergo.ref", s.Ref.String()))
	}
	for _, a := range s.Attributes {
		attrs = append(attrs, stringAttr(a.Key, a.Value))
	}
	return attrs
}

func stringAttr(key, value string) *commonpb.KeyValue {
	return &commonpb.KeyValue{
		Key: key,
		Value: &commonpb.AnyValue{
			Value: &commonpb.AnyValue_StringValue{StringValue: value},
		},
	}
}

func uint64Attr(key string, value uint64) *commonpb.KeyValue {
	return &commonpb.KeyValue{
		Key: key,
		Value: &commonpb.AnyValue{
			Value: &commonpb.AnyValue_IntValue{IntValue: int64(value)},
		},
	}
}
