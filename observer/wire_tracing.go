package observer

import (
	"fmt"

	"ergo.services/ergo/app/system/inspect"
	"ergo.services/ergo/gen"
)

type wireTracingAttribute struct {
	Key   string `json:"k"`
	Value string `json:"v"`
}

func wireTracingAttributesFrom(list []gen.TracingAttribute) []wireTracingAttribute {
	if list == nil {
		return nil
	}
	out := make([]wireTracingAttribute, len(list))
	for i, attribute := range list {
		out[i] = wireTracingAttribute{Key: attribute.Key, Value: attribute.Value}
	}
	return out
}

type wireSpan struct {
	TraceID      string                 `json:"tid"`
	SpanID       string                 `json:"sid"`
	ParentSpanID string                 `json:"psid,omitempty"`
	ParentPoint  string                 `json:"ppt,omitempty"`
	Point        string                 `json:"pt"`
	Kind         string                 `json:"k"`
	Timestamp    int64                  `json:"t"`
	EndTimestamp int64                  `json:"et,omitempty"`
	Node         string                 `json:"nd"`
	From         wirePID                `json:"f"`
	To           any                    `json:"to,omitempty"`
	ToKind       string                 `json:"tok,omitempty"`
	Ref          wireRef                `json:"r"`
	Behavior     string                 `json:"b,omitempty"`
	Message      string                 `json:"msg"`
	Error        string                 `json:"err,omitempty"`
	Attributes   []wireTracingAttribute `json:"at,omitempty"`
}

type wireTracing struct {
	Node       string     `json:"nd"`
	Spans      []wireSpan `json:"sp,omitempty"`
	Suppressed int64      `json:"sup,omitempty"`
}

func wireTracingTarget(to any) (any, string) {
	switch v := to.(type) {
	case gen.PID:
		return wirePIDFrom(v), "pid"
	case gen.ProcessID:
		return wireProcessIDFrom(v), "process_id"
	case gen.Alias:
		return wireAliasFrom(v), "alias"
	case nil:
		return nil, ""
	}
	return fmt.Sprintf("%v", to), "unknown"
}

func wireTracingFrom(m inspect.MessageInspectTracing) wireTracing {
	out := wireTracing{Node: string(m.Node), Suppressed: m.Suppressed}
	for _, span := range m.Spans {
		parent := ""
		if span.ParentSpanID != 0 {
			parent = fmt.Sprintf("%016x", span.ParentSpanID)
		}
		parentPoint := ""
		if span.ParentPoint != 0 {
			parentPoint = span.ParentPoint.String()
		}

		to, toKind := wireTracingTarget(span.To)

		out.Spans = append(out.Spans, wireSpan{
			TraceID:      fmt.Sprintf("%016x%016x", span.TraceID[0], span.TraceID[1]),
			SpanID:       fmt.Sprintf("%016x", span.SpanID),
			ParentSpanID: parent,
			ParentPoint:  parentPoint,
			Point:        span.Point.String(),
			Kind:         span.Kind.String(),
			Timestamp:    span.Timestamp,
			EndTimestamp: span.EndTimestamp,
			Node:         string(span.Node),
			From:         wirePIDFrom(span.From),
			To:           to,
			ToKind:       toKind,
			Ref:          wireRefFrom(span.Ref),
			Behavior:     span.Behavior,
			Message:      span.Message,
			Error:        span.Error,
			Attributes:   wireTracingAttributesFrom(span.Attributes),
		})
	}
	return out
}
