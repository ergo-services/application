package observer

import (
	"ergo.services/ergo/app/system/inspect"
	"ergo.services/ergo/gen"
)

type wireMCPMetaInfo struct {
	ID              string
	Parent          string
	Application     gen.Atom
	Behavior        string
	MailboxSize     int64 `sentinel:"-1 = unlimited, otherwise the configured limit"`
	MailboxQueues   wireMCPMetaQueues
	MessagePriority string
	MessagesIn      uint64
	MessagesOut     uint64
	LogLevel        string
	Uptime          int64 `unit:"sec"`
	State           string

	Legend map[string]any `json:"services.ergo/legend,omitempty"`
}

type wireMCPMeta struct {
	Node gen.Atom
	Info wireMCPMetaInfo
}

var wireMCPMetaLegend = mcpLegendFor(wireMCPMetaInfo{})

func wireMCPMetaInfoOf(info gen.MetaInfo) wireMCPMetaInfo {
	return wireMCPMetaInfo{
		ID:              mcpRefText(gen.Ref(info.ID)),
		Parent:          mcpPIDText(info.Parent),
		Application:     info.Application,
		Behavior:        info.Behavior,
		MailboxSize:     info.MailboxSize,
		MailboxQueues:   wireMCPMetaQueuesOf(info.MailboxQueues),
		MessagePriority: info.MessagePriority.String(),
		MessagesIn:      info.MessagesIn,
		MessagesOut:     info.MessagesOut,
		LogLevel:        info.LogLevel.String(),
		Uptime:          info.Uptime,
		State:           info.State.String(),
		Legend:          wireMCPMetaLegend,
	}
}

func init() {
	mcpRegisterView(inspect.MessageInspectMeta{}, func(value any) any {
		m, ok := value.(inspect.MessageInspectMeta)
		if ok == false {
			return value
		}
		return wireMCPMeta{Node: m.Node, Info: wireMCPMetaInfoOf(m.Info)}
	})
}
