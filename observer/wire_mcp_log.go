package observer

import (
	"ergo.services/ergo/app/system/inspect"
	"ergo.services/ergo/gen"
)

// Fields carry whatever the caller logged, so a value there is left as it came: it is data of
// the process, not an identifier of the answer.
type wireMCPLogEntry struct {
	Source    string
	Name      gen.Atom
	PID       string
	Behavior  string
	Peer      gen.Atom
	Parent    string
	Meta      string
	Creation  int64
	Timestamp int64 `unit:"unix ns"`
	Level     string
	Message   string
	Fields    []gen.LogField
}

type wireMCPLog struct {
	Node       gen.Atom
	Entries    []wireMCPLogEntry
	Suppressed int64
}

func wireMCPLogEntryOf(entry inspect.InspectLogEntry) wireMCPLogEntry {
	return wireMCPLogEntry{
		Source:    entry.Source,
		Name:      entry.Name,
		PID:       mcpPIDText(entry.PID),
		Behavior:  entry.Behavior,
		Peer:      entry.Peer,
		Parent:    mcpPIDText(entry.Parent),
		Meta:      mcpRefText(gen.Ref(entry.Meta)),
		Creation:  entry.Creation,
		Timestamp: entry.Timestamp,
		Level:     entry.Level.String(),
		Message:   entry.Message,
		Fields:    entry.Fields,
	}
}

func init() {
	mcpRegisterView(inspect.MessageInspectLog{}, func(value any) any {
		m, ok := value.(inspect.MessageInspectLog)
		if ok == false {
			return value
		}
		out := wireMCPLog{Node: m.Node, Suppressed: m.Suppressed}
		if m.Entries != nil {
			out.Entries = make([]wireMCPLogEntry, len(m.Entries))
			for i, entry := range m.Entries {
				out.Entries[i] = wireMCPLogEntryOf(entry)
			}
		}
		return out
	})
}
