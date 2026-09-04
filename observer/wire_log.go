package observer

import (
	"ergo.services/ergo/app/system/inspect"
)

type wireLogField struct {
	Name  string `json:"n"`
	Value any    `json:"v"`
}

type wireLogEntry struct {
	Source    string         `json:"s,omitempty"`
	Name      string         `json:"n,omitempty"`
	PID       *wirePID       `json:"i,omitempty"`
	Behavior  string         `json:"b,omitempty"`
	Peer      string         `json:"pr,omitempty"`
	Parent    *wirePID       `json:"p,omitempty"`
	Meta      *wireAlias     `json:"m,omitempty"`
	Creation  int64          `json:"c,omitempty"`
	Timestamp int64          `json:"t"`
	Level     string         `json:"l,omitempty"`
	Message   string         `json:"msg,omitempty"`
	Fields    []wireLogField `json:"f,omitempty"`
}

type wireLog struct {
	Node       string         `json:"nd"`
	Entries    []wireLogEntry `json:"en,omitempty"`
	Suppressed int64          `json:"sup,omitempty"`
}

func wireLogFrom(m inspect.MessageInspectLog) wireLog {
	out := wireLog{Node: string(m.Node), Suppressed: m.Suppressed}
	for _, entry := range m.Entries {
		fields := make([]wireLogField, 0, len(entry.Fields))
		for _, field := range entry.Fields {
			fields = append(fields, wireLogField{Name: field.Name, Value: field.Value})
		}
		out.Entries = append(out.Entries, wireLogEntry{
			Source:    entry.Source,
			Name:      string(entry.Name),
			PID:       pidOrNil(entry.PID),
			Behavior:  entry.Behavior,
			Peer:      string(entry.Peer),
			Parent:    pidOrNil(entry.Parent),
			Meta:      aliasOrNil(entry.Meta),
			Creation:  entry.Creation,
			Timestamp: entry.Timestamp,
			Level:     entry.Level.String(),
			Message:   entry.Message,
			Fields:    fields,
		})
	}
	return out
}
