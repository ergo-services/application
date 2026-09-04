package observer

import (
	"ergo.services/ergo/app/system/inspect"
	"ergo.services/ergo/gen"
)

type wireMCPProcess struct {
	Node gen.Atom
	Info wireMCPProcessInfo
}

type wireMCPCompression struct {
	Enable    bool
	Type      gen.CompressionType
	Level     string
	Threshold int `unit:"bytes"`
}

func wireMCPCompressionOf(compression gen.Compression) wireMCPCompression {
	return wireMCPCompression{
		Enable:    compression.Enable,
		Type:      compression.Type,
		Level:     compression.Level.String(),
		Threshold: compression.Threshold,
	}
}

type wireMCPProcessInfo struct {
	PID               string
	Name              gen.Atom
	Application       gen.Atom
	Behavior          string
	Kind              gen.ProcessKind
	MailboxSize       int64 `sentinel:"-1 = unlimited, otherwise the configured limit"`
	MailboxQueues     wireMCPMailboxQueues
	MessagesIn        uint64
	MessagesOut       uint64
	RunningTime       uint64 `unit:"ns"`
	InitTime          uint64 `unit:"ns"`
	Wakeups           uint64
	Compression       wireMCPCompression
	MessagePriority   string
	Uptime            int64 `unit:"sec"`
	State             string
	StateTime         int64 `unit:"ns"`
	Parent            string
	Leader            string
	Tracing           gen.TracingInfo
	Fallback          gen.ProcessFallback
	Env               map[gen.Env]any `sentinel:"empty unless NodeOptions.Security.ExposeEnvInfo is enabled"`
	Aliases           []string
	Events            []gen.Atom
	Metas             []string
	MonitorsPID       []string
	MonitorsProcessID []string
	MonitorsAlias     []string
	MonitorsEvent     []string
	MonitorsNode      []gen.Atom
	LinksPID          []string
	LinksProcessID    []string
	LinksAlias        []string
	LinksEvent        []string
	LinksNode         []gen.Atom
	LogLevel          string
	KeepNetworkOrder  bool
	ImportantDelivery bool

	Legend map[string]any `json:"services.ergo/legend,omitempty"`
}

var wireMCPProcessLegend = mcpLegendFor(wireMCPProcessInfo{})

func wireMCPProcessOf(value any) any {
	message, ok := value.(inspect.MessageInspectProcess)
	if ok == false {
		return value
	}
	info := message.Info

	return wireMCPProcess{
		Node: message.Node,
		Info: wireMCPProcessInfo{
			PID:               mcpPIDText(info.PID),
			Name:              info.Name,
			Application:       info.Application,
			Behavior:          info.Behavior,
			Kind:              info.Kind,
			MailboxSize:       info.MailboxSize,
			MailboxQueues:     wireMCPMailboxQueuesOf(info.MailboxQueues),
			MessagesIn:        info.MessagesIn,
			MessagesOut:       info.MessagesOut,
			RunningTime:       info.RunningTime,
			InitTime:          info.InitTime,
			Wakeups:           info.Wakeups,
			Compression:       wireMCPCompressionOf(info.Compression),
			MessagePriority:   info.MessagePriority.String(),
			Uptime:            info.Uptime,
			State:             info.State.String(),
			StateTime:         info.StateTime,
			Parent:            mcpPIDText(info.Parent),
			Leader:            mcpPIDText(info.Leader),
			Tracing:           info.Tracing,
			Fallback:          info.Fallback,
			Env:               info.Env,
			Aliases:           mcpAliasTexts(info.Aliases),
			Events:            info.Events,
			Metas:             mcpAliasTexts(info.Metas),
			MonitorsPID:       mcpPIDTexts(info.MonitorsPID),
			MonitorsProcessID: mcpProcessIDTexts(info.MonitorsProcessID),
			MonitorsAlias:     mcpAliasTexts(info.MonitorsAlias),
			MonitorsEvent:     mcpEventTexts(info.MonitorsEvent),
			MonitorsNode:      info.MonitorsNode,
			LinksPID:          mcpPIDTexts(info.LinksPID),
			LinksProcessID:    mcpProcessIDTexts(info.LinksProcessID),
			LinksAlias:        mcpAliasTexts(info.LinksAlias),
			LinksEvent:        mcpEventTexts(info.LinksEvent),
			LinksNode:         info.LinksNode,
			LogLevel:          info.LogLevel.String(),
			KeepNetworkOrder:  info.KeepNetworkOrder,
			ImportantDelivery: info.ImportantDelivery,
			Legend:            wireMCPProcessLegend,
		},
	}
}

func init() {
	mcpRegisterView(inspect.MessageInspectProcess{}, wireMCPProcessOf)
}
