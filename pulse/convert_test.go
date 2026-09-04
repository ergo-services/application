package pulse

import (
	"encoding/binary"
	"testing"

	"ergo.services/ergo/gen"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
)

func spanID(id uint64, slot gen.TracingPoint) []byte {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], id<<2|uint64(slot))
	return b[:]
}

func attrOf(span *tracepb.Span, key string) (string, bool) {
	for _, a := range span.Attributes {
		if a.Key == key {
			return a.Value.GetStringValue(), true
		}
	}
	return "", false
}

func TestConvertSpanTraceID(t *testing.T) {
	out := convertSpan(&gen.TracingSpan{TraceID: [2]uint64{0x0102030405060708, 0x090a0b0c0d0e0f10}})

	want := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	if len(out.TraceId) != 16 {
		t.Fatalf("TraceId is %d bytes, OTLP requires 16", len(out.TraceId))
	}
	for i := range want {
		if out.TraceId[i] != want[i] {
			t.Fatalf("TraceId = %v, want %v", out.TraceId, want)
		}
	}
}

func TestConvertSpanIDCarriesThePointInTheLowBits(t *testing.T) {
	for _, tc := range []struct {
		point gen.TracingPoint
		slot  gen.TracingPoint
	}{
		{gen.TracingPointSent, gen.TracingPointSent},
		{gen.TracingPointDelivered, gen.TracingPointDelivered},
		{gen.TracingPointProcessed, gen.TracingPointProcessed},
		{gen.TracingPointSpan, gen.TracingPointProcessed},
	} {
		out := convertSpan(&gen.TracingSpan{SpanID: 42, Point: tc.point})
		want := spanID(42, tc.slot)
		if string(out.SpanId) != string(want) {
			t.Errorf("point %s: SpanId = %x, want %x", tc.point, out.SpanId, want)
		}
	}
}

func TestConvertSpanIDIsDistinctPerPointOfOneMessage(t *testing.T) {
	seen := map[string]gen.TracingPoint{}
	for _, p := range []gen.TracingPoint{gen.TracingPointSent, gen.TracingPointDelivered, gen.TracingPointProcessed} {
		out := convertSpan(&gen.TracingSpan{SpanID: 7, Point: p})
		if other, clash := seen[string(out.SpanId)]; clash {
			t.Fatalf("%s and %s share SpanId %x", p, other, out.SpanId)
		}
		seen[string(out.SpanId)] = p
	}
}

func TestConvertDeliveredAndProcessedHangUnderSent(t *testing.T) {
	for _, p := range []gen.TracingPoint{gen.TracingPointDelivered, gen.TracingPointProcessed} {
		out := convertSpan(&gen.TracingSpan{SpanID: 42, Point: p, Kind: gen.TracingKindSend})
		want := spanID(42, gen.TracingPointSent)
		if string(out.ParentSpanId) != string(want) {
			t.Errorf("%s: ParentSpanId = %x, want the Sent of the same message %x", p, out.ParentSpanId, want)
		}
	}
}

func TestConvertSentHangsUnderTheProcessedOfItsParent(t *testing.T) {
	out := convertSpan(&gen.TracingSpan{SpanID: 42, ParentSpanID: 10, Point: gen.TracingPointSent})

	want := spanID(10, gen.TracingPointProcessed)
	if string(out.ParentSpanId) != string(want) {
		t.Fatalf("ParentSpanId = %x, want %x", out.ParentSpanId, want)
	}
}

func TestConvertRootSentHasNoParent(t *testing.T) {
	out := convertSpan(&gen.TracingSpan{SpanID: 42, ParentSpanID: 0, Point: gen.TracingPointSent})

	if len(out.ParentSpanId) != 0 {
		t.Fatalf("a span with no parent reported ParentSpanId %x", out.ParentSpanId)
	}
}

func TestConvertTerminateHangsUnderItsParentNotUnderASent(t *testing.T) {
	out := convertSpan(&gen.TracingSpan{
		SpanID: 42, ParentSpanID: 10,
		Point: gen.TracingPointProcessed, Kind: gen.TracingKindTerminate,
	})

	want := spanID(10, gen.TracingPointProcessed)
	if string(out.ParentSpanId) != string(want) {
		t.Fatalf("ParentSpanId = %x, want %x", out.ParentSpanId, want)
	}
}

func TestConvertRootTerminateHasNoParent(t *testing.T) {
	out := convertSpan(&gen.TracingSpan{
		SpanID: 42, ParentSpanID: 0,
		Point: gen.TracingPointProcessed, Kind: gen.TracingKindTerminate,
	})

	if len(out.ParentSpanId) != 0 {
		t.Fatalf("a root Terminate reported ParentSpanId %x", out.ParentSpanId)
	}
}

func TestConvertBusinessSpanHangsUnderTheEnclosingAnchor(t *testing.T) {
	out := convertSpan(&gen.TracingSpan{SpanID: 42, ParentSpanID: 10, Point: gen.TracingPointSpan})

	want := spanID(10, gen.TracingPointProcessed)
	if string(out.ParentSpanId) != string(want) {
		t.Fatalf("ParentSpanId = %x, want %x", out.ParentSpanId, want)
	}
}

func TestConvertRootBusinessSpanHasNoParent(t *testing.T) {
	out := convertSpan(&gen.TracingSpan{SpanID: 42, ParentSpanID: 0, Point: gen.TracingPointSpan})

	if len(out.ParentSpanId) != 0 {
		t.Fatalf("a root business span reported ParentSpanId %x", out.ParentSpanId)
	}
}

func TestMapSpanKind(t *testing.T) {
	for _, tc := range []struct {
		kind  gen.TracingKind
		point gen.TracingPoint
		want  tracepb.Span_SpanKind
	}{
		{gen.TracingKindSend, gen.TracingPointSent, tracepb.Span_SPAN_KIND_PRODUCER},
		{gen.TracingKindSend, gen.TracingPointDelivered, tracepb.Span_SPAN_KIND_CONSUMER},
		{gen.TracingKindSend, gen.TracingPointProcessed, tracepb.Span_SPAN_KIND_CONSUMER},
		{gen.TracingKindRequest, gen.TracingPointSent, tracepb.Span_SPAN_KIND_CLIENT},
		{gen.TracingKindRequest, gen.TracingPointDelivered, tracepb.Span_SPAN_KIND_SERVER},
		{gen.TracingKindRequest, gen.TracingPointProcessed, tracepb.Span_SPAN_KIND_SERVER},
		{gen.TracingKindResponse, gen.TracingPointSent, tracepb.Span_SPAN_KIND_SERVER},
		{gen.TracingKindResponse, gen.TracingPointDelivered, tracepb.Span_SPAN_KIND_CLIENT},
		{gen.TracingKindResponse, gen.TracingPointProcessed, tracepb.Span_SPAN_KIND_SERVER},
		{gen.TracingKindSpawn, gen.TracingPointProcessed, tracepb.Span_SPAN_KIND_INTERNAL},
		{gen.TracingKindTerminate, gen.TracingPointProcessed, tracepb.Span_SPAN_KIND_INTERNAL},
		{gen.TracingKindSend, gen.TracingPointSpan, tracepb.Span_SPAN_KIND_INTERNAL},
		{gen.TracingKindRequest, gen.TracingPointSpan, tracepb.Span_SPAN_KIND_INTERNAL},
	} {
		if got := mapSpanKind(tc.kind, tc.point); got != tc.want {
			t.Errorf("%s.%s = %s, want %s", tc.kind, tc.point, got, tc.want)
		}
	}
}

func TestConvertNameOfAPointObservation(t *testing.T) {
	out := convertSpan(&gen.TracingSpan{
		Point: gen.TracingPointSent, Kind: gen.TracingKindRequest,
		Behavior: "main.Worker", Message: "Query",
	})

	if want := "main.Worker request.sent Query"; out.Name != want {
		t.Fatalf("Name = %q, want %q", out.Name, want)
	}
}

func TestConvertNameWithoutBehaviorOrMessage(t *testing.T) {
	out := convertSpan(&gen.TracingSpan{Point: gen.TracingPointSent, Kind: gen.TracingKindSend})

	if want := "send.sent"; out.Name != want {
		t.Fatalf("Name = %q, want %q", out.Name, want)
	}
}

func TestConvertNameOfABusinessSpanIsItsOperation(t *testing.T) {
	out := convertSpan(&gen.TracingSpan{
		Point: gen.TracingPointSpan, Kind: gen.TracingKindSend,
		Behavior: "main.Worker", Message: "charge card",
	})

	if want := "main.Worker charge card"; out.Name != want {
		t.Fatalf("Name = %q, want %q", out.Name, want)
	}
}

func TestConvertMarksACrossNodeObservationRemote(t *testing.T) {
	remote := convertSpan(&gen.TracingSpan{
		Node: "b@localhost",
		From: gen.PID{Node: "a@localhost", ID: 1},
	})
	if remote.Flags != 0x00000300 {
		t.Errorf("a span observed on another node has Flags %#x, want the IS_REMOTE pair", remote.Flags)
	}

	local := convertSpan(&gen.TracingSpan{
		Node: "a@localhost",
		From: gen.PID{Node: "a@localhost", ID: 1},
	})
	if local.Flags != 0 {
		t.Errorf("a local span has Flags %#x, want none", local.Flags)
	}
}

func TestConvertPointObservationIsInstantaneous(t *testing.T) {
	out := convertSpan(&gen.TracingSpan{Timestamp: 1000, Point: gen.TracingPointSent})

	if out.StartTimeUnixNano != 1000 || out.EndTimeUnixNano != 1000 {
		t.Fatalf("start=%d end=%d, want both 1000", out.StartTimeUnixNano, out.EndTimeUnixNano)
	}
}

func TestConvertBusinessSpanKeepsItsInterval(t *testing.T) {
	out := convertSpan(&gen.TracingSpan{Timestamp: 1000, EndTimestamp: 4000, Point: gen.TracingPointSpan})

	if out.StartTimeUnixNano != 1000 || out.EndTimeUnixNano != 4000 {
		t.Fatalf("start=%d end=%d, want 1000..4000", out.StartTimeUnixNano, out.EndTimeUnixNano)
	}
}

func TestConvertCarriesTheErrorAsStatus(t *testing.T) {
	out := convertSpan(&gen.TracingSpan{Error: "timeout"})

	if out.Status == nil {
		t.Fatal("a span with an error carries no Status")
	}
	if out.Status.Code != tracepb.Status_STATUS_CODE_ERROR || out.Status.Message != "timeout" {
		t.Fatalf("Status = %s/%q", out.Status.Code, out.Status.Message)
	}
}

func TestConvertLeavesStatusUnsetWithoutAnError(t *testing.T) {
	if out := convertSpan(&gen.TracingSpan{}); out.Status != nil {
		t.Fatalf("a span with no error reported Status %v", out.Status)
	}
}

func TestConvertAttributes(t *testing.T) {
	out := convertSpan(&gen.TracingSpan{
		Node:     "a@localhost",
		From:     gen.PID{Node: "a@localhost", ID: 1},
		To:       gen.PID{Node: "b@localhost", ID: 2},
		Kind:     gen.TracingKindRequest,
		Point:    gen.TracingPointSent,
		SpanID:   42,
		Behavior: "main.Worker",
		Message:  "Query",
	})

	for _, key := range []string{"ergo.node", "ergo.from", "ergo.to", "ergo.kind", "ergo.point", "ergo.behavior", "ergo.message"} {
		if _, ok := attrOf(out, key); ok == false {
			t.Errorf("%s is missing", key)
		}
	}
	for _, a := range out.Attributes {
		if a.Key == "ergo.span_id" {
			if a.Value.GetIntValue() != 42 {
				t.Errorf("ergo.span_id = %d, want 42", a.Value.GetIntValue())
			}
		}
	}
}

func TestConvertOmitsEmptyOptionalAttributes(t *testing.T) {
	out := convertSpan(&gen.TracingSpan{Node: "a@localhost"})

	for _, key := range []string{"ergo.behavior", "ergo.message", "ergo.ref"} {
		if _, ok := attrOf(out, key); ok {
			t.Errorf("%s is present although the field is empty", key)
		}
	}
}

func TestConvertCarriesUserAttributes(t *testing.T) {
	out := convertSpan(&gen.TracingSpan{
		Attributes: []gen.TracingAttribute{{Key: "order.id", Value: "A-1"}},
	})

	if v, ok := attrOf(out, "order.id"); ok == false || v != "A-1" {
		t.Fatalf("order.id = %q/%v", v, ok)
	}
}

func TestBuildExportRequestEnvelope(t *testing.T) {
	req := buildExportRequest([]gen.TracingSpan{{SpanID: 1}, {SpanID: 2}}, "node@localhost")

	if len(req.ResourceSpans) != 1 {
		t.Fatalf("ResourceSpans = %d, want 1", len(req.ResourceSpans))
	}
	rs := req.ResourceSpans[0]

	var service string
	for _, a := range rs.Resource.Attributes {
		if a.Key == "service.name" {
			service = a.Value.GetStringValue()
		}
	}
	if service != "node@localhost" {
		t.Errorf("service.name = %q, want the node name", service)
	}

	if len(rs.ScopeSpans) != 1 {
		t.Fatalf("ScopeSpans = %d, want 1", len(rs.ScopeSpans))
	}
	scope := rs.ScopeSpans[0]
	if scope.Scope.Name != "ergo.services/pulse" || scope.Scope.Version != Version {
		t.Errorf("scope = %q/%q", scope.Scope.Name, scope.Scope.Version)
	}
	if len(scope.Spans) != 2 {
		t.Fatalf("Spans = %d, want the 2 handed in", len(scope.Spans))
	}
}

func TestBuildExportRequestWithNoSpans(t *testing.T) {
	req := buildExportRequest(nil, "node@localhost")

	if len(req.ResourceSpans[0].ScopeSpans[0].Spans) != 0 {
		t.Fatal("an empty batch produced spans")
	}
}
