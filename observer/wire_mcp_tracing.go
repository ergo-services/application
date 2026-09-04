package observer

import (
	"ergo.services/ergo/app/system/inspect"
	"ergo.services/ergo/gen"
)

type wireMCPTracingSpan struct {
	TraceID      string
	SpanID       string
	ParentSpanID string `json:"ParentSpanID,omitempty"`
	ParentPoint  string `json:"ParentPoint,omitempty"`
	Point        string
	Kind         string
	Timestamp    int64 `unit:"unix ns"`
	EndTimestamp int64 `json:"EndTimestamp,omitempty" unit:"unix ns" sentinel:"0 = a point observation, not an interval"`
	Node         gen.Atom
	From         string
	To           string
	Ref          string
	Behavior     string `json:"Behavior,omitempty"`
	Message      string
	Error        string                 `json:"Error,omitempty"`
	Attributes   []gen.TracingAttribute `json:"Attributes,omitempty"`
}

type wireMCPTracing struct {
	Node       gen.Atom
	Spans      []wireMCPTracingSpan
	Suppressed int64
}

func wireMCPTracingSpanOf(span gen.TracingSpan) wireMCPTracingSpan {
	parentPoint := ""
	if span.ParentPoint != 0 {
		parentPoint = span.ParentPoint.String()
	}

	return wireMCPTracingSpan{
		TraceID:      mcpTraceIDText(span.TraceID),
		SpanID:       mcpSpanIDText(span.SpanID),
		ParentSpanID: mcpSpanIDText(span.ParentSpanID),
		ParentPoint:  parentPoint,
		Point:        span.Point.String(),
		Kind:         span.Kind.String(),
		Timestamp:    span.Timestamp,
		EndTimestamp: span.EndTimestamp,
		Node:         span.Node,
		From:         mcpPIDText(span.From),
		To:           mcpTargetText(span.To),
		Ref:          mcpRefText(span.Ref),
		Behavior:     span.Behavior,
		Message:      span.Message,
		Error:        span.Error,
		Attributes:   span.Attributes,
	}
}

func init() {
	mcpRegisterView(inspect.MessageInspectTracing{}, func(value any) any {
		m, ok := value.(inspect.MessageInspectTracing)
		if ok == false {
			return value
		}
		out := wireMCPTracing{Node: m.Node, Suppressed: m.Suppressed}
		if m.Spans != nil {
			out.Spans = make([]wireMCPTracingSpan, len(m.Spans))
			for i, span := range m.Spans {
				out.Spans[i] = wireMCPTracingSpanOf(span)
			}
		}
		return out
	})
}
