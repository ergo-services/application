package observer

import (
	"ergo.services/ergo/app/system/inspect"
	"ergo.services/ergo/gen"
)

type wireProcess struct {
	PID             wirePID `json:"i"`
	Name            string  `json:"n,omitempty"`
	Application     string  `json:"a,omitempty"`
	Behavior        string  `json:"b,omitempty"`
	Kind            string  `json:"k,omitempty"`
	MessagesIn      uint64  `json:"mi,omitempty"`
	MessagesOut     uint64  `json:"mo,omitempty"`
	MessagesMailbox uint64  `json:"mm,omitempty"`
	MailboxLatency  int64   `json:"ml,omitempty"`
	RunningTime     uint64  `json:"rt,omitempty"`
	InitTime        uint64  `json:"it,omitempty"`
	Wakeups         uint64  `json:"w,omitempty"`
	Uptime          int64   `json:"u,omitempty"`
	State           string  `json:"s,omitempty"`
	StateTime       int64   `json:"st,omitempty"`
	Parent          wirePID `json:"p"`
	Leader          wirePID `json:"l"`
	LogLevel        string  `json:"ll,omitempty"`
}

type wireProcessList struct {
	Node      string        `json:"nd"`
	Processes []wireProcess `json:"ps"`
}

type wireAppTree struct {
	Node        string        `json:"nd"`
	Application string        `json:"app"`
	Processes   []wireProcess `json:"ps"`
	Truncated   int           `json:"tr,omitempty"`
}

type wireSubtree struct {
	Node      string        `json:"nd"`
	PID       wirePID       `json:"i"`
	Processes []wireProcess `json:"ps"`
	Truncated int           `json:"tr,omitempty"`
}

func wireProcessFrom(info gen.ProcessShortInfo) wireProcess {
	return wireProcess{
		PID:             wirePIDFrom(info.PID),
		Name:            string(info.Name),
		Application:     string(info.Application),
		Behavior:        info.Behavior,
		Kind:            string(info.Kind),
		MessagesIn:      info.MessagesIn,
		MessagesOut:     info.MessagesOut,
		MessagesMailbox: info.MessagesMailbox,
		MailboxLatency:  info.MailboxLatency,
		RunningTime:     info.RunningTime,
		InitTime:        info.InitTime,
		Wakeups:         info.Wakeups,
		Uptime:          info.Uptime,
		State:           info.State.String(),
		StateTime:       info.StateTime,
		Parent:          wirePIDFrom(info.Parent),
		Leader:          wirePIDFrom(info.Leader),
		LogLevel:        info.LogLevel.String(),
	}
}

func wireProcessesFrom(list []gen.ProcessShortInfo) []wireProcess {
	out := make([]wireProcess, 0, len(list))
	for _, info := range list {
		out = append(out, wireProcessFrom(info))
	}
	return out
}

func wireProcessListFrom(m inspect.MessageInspectProcessList) wireProcessList {
	return wireProcessList{Node: string(m.Node), Processes: wireProcessesFrom(m.Processes)}
}

func wireAppTreeFrom(r inspect.ResponseGetAppTree) wireAppTree {
	return wireAppTree{
		Node:        string(r.Node),
		Application: string(r.Application),
		Processes:   wireProcessesFrom(r.Processes),
		Truncated:   r.Truncated,
	}
}

type wireLookup struct {
	PID   wirePID `json:"i"`
	Name  string  `json:"n,omitempty"`
	State string  `json:"s"`
}

func wireLookupFrom(r inspect.ResponseGetProcessLookup) wireLookup {
	return wireLookup{
		PID:   wirePIDFrom(r.PID),
		Name:  string(r.Name),
		State: r.State.String(),
	}
}

func wireSubtreeFrom(r inspect.ResponseGetSubtree) wireSubtree {
	return wireSubtree{
		Node:      string(r.Node),
		PID:       wirePIDFrom(r.PID),
		Processes: wireProcessesFrom(r.Processes),
		Truncated: r.Truncated,
	}
}

type wireMailboxQueues struct {
	Main          int64 `json:"m,omitempty"`
	System        int64 `json:"s,omitempty"`
	Urgent        int64 `json:"u,omitempty"`
	Log           int64 `json:"l,omitempty"`
	LatencyMain   int64 `json:"lm,omitempty"`
	LatencySystem int64 `json:"ls,omitempty"`
	LatencyUrgent int64 `json:"lu,omitempty"`
	LatencyLog    int64 `json:"ll,omitempty"`
}

type wireMetaQueues struct {
	Main   int64 `json:"m,omitempty"`
	System int64 `json:"s,omitempty"`
}

type wireCompression struct {
	Enable    bool   `json:"e,omitempty"`
	Type      string `json:"t,omitempty"`
	Level     string `json:"l,omitempty"`
	Threshold int    `json:"th,omitempty"`
}

type wireProcessDetail struct {
	PID               wirePID                `json:"i"`
	Name              string                 `json:"n,omitempty"`
	Application       string                 `json:"a,omitempty"`
	Behavior          string                 `json:"b,omitempty"`
	Kind              string                 `json:"k,omitempty"`
	MailboxSize       int64                  `json:"ms,omitempty"`
	MailboxQueues     wireMailboxQueues      `json:"mq,omitempty"`
	MessagesIn        uint64                 `json:"mi,omitempty"`
	MessagesOut       uint64                 `json:"mo,omitempty"`
	RunningTime       uint64                 `json:"rt,omitempty"`
	InitTime          uint64                 `json:"it,omitempty"`
	Wakeups           uint64                 `json:"w,omitempty"`
	Compression       wireCompression        `json:"cp,omitempty"`
	MessagePriority   string                 `json:"mp,omitempty"`
	Uptime            int64                  `json:"u,omitempty"`
	State             string                 `json:"s,omitempty"`
	StateTime         int64                  `json:"st,omitempty"`
	Parent            wirePID                `json:"p"`
	Leader            wirePID                `json:"l"`
	TracingSampler    string                 `json:"trs,omitempty"`
	TracingAttributes []wireTracingAttribute `json:"tra,omitempty"`
	Fallback          wireFallback           `json:"fb,omitempty"`
	Env               map[string]any         `json:"e,omitempty"`
	Aliases           []wireAlias            `json:"al,omitempty"`
	Events            []string               `json:"ev,omitempty"`
	Metas             []wireAlias            `json:"mt,omitempty"`
	MonitorsPID       []wirePID              `json:"mnp,omitempty"`
	MonitorsProcessID []wireProcessID        `json:"mnn,omitempty"`
	MonitorsAlias     []wireAlias            `json:"mna,omitempty"`
	MonitorsEvent     []wireEventRef         `json:"mne,omitempty"`
	MonitorsNode      []string               `json:"mnd,omitempty"`
	LinksPID          []wirePID              `json:"lkp,omitempty"`
	LinksProcessID    []wireProcessID        `json:"lkn,omitempty"`
	LinksAlias        []wireAlias            `json:"lka,omitempty"`
	LinksEvent        []wireEventRef         `json:"lke,omitempty"`
	LinksNode         []string               `json:"lkd,omitempty"`
	LogLevel          string                 `json:"ll,omitempty"`
	KeepNetworkOrder  bool                   `json:"kno,omitempty"`
	ImportantDelivery bool                   `json:"idl,omitempty"`
}

type wireProcessInfo struct {
	Node string            `json:"nd"`
	Info wireProcessDetail `json:"in"`
}

func wireProcessDetailFrom(info gen.ProcessInfo) wireProcessDetail {
	events := make([]string, 0, len(info.Events))
	for _, name := range info.Events {
		events = append(events, string(name))
	}

	return wireProcessDetail{
		PID:         wirePIDFrom(info.PID),
		Name:        string(info.Name),
		Application: string(info.Application),
		Behavior:    info.Behavior,
		Kind:        string(info.Kind),
		MailboxSize: info.MailboxSize,
		MailboxQueues: wireMailboxQueues{
			Main:          info.MailboxQueues.Main,
			System:        info.MailboxQueues.System,
			Urgent:        info.MailboxQueues.Urgent,
			Log:           info.MailboxQueues.Log,
			LatencyMain:   info.MailboxQueues.LatencyMain,
			LatencySystem: info.MailboxQueues.LatencySystem,
			LatencyUrgent: info.MailboxQueues.LatencyUrgent,
			LatencyLog:    info.MailboxQueues.LatencyLog,
		},
		MessagesIn:  info.MessagesIn,
		MessagesOut: info.MessagesOut,
		RunningTime: info.RunningTime,
		InitTime:    info.InitTime,
		Wakeups:     info.Wakeups,
		Compression: wireCompression{
			Enable:    info.Compression.Enable,
			Type:      string(info.Compression.Type),
			Level:     info.Compression.Level.String(),
			Threshold: info.Compression.Threshold,
		},
		MessagePriority:   info.MessagePriority.String(),
		Uptime:            info.Uptime,
		State:             info.State.String(),
		StateTime:         info.StateTime,
		Parent:            wirePIDFrom(info.Parent),
		Leader:            wirePIDFrom(info.Leader),
		TracingSampler:    info.Tracing.Sampler,
		TracingAttributes: wireTracingAttributesFrom(info.Tracing.Attributes),
		Fallback:          wireFallbackFrom(info.Fallback),
		Env:               wireEnv(info.Env),
		Aliases:           wireAliasesFrom(info.Aliases),
		Events:            events,
		Metas:             wireAliasesFrom(info.Metas),
		MonitorsPID:       wirePIDsFrom(info.MonitorsPID),
		MonitorsProcessID: wireProcessIDsFrom(info.MonitorsProcessID),
		MonitorsAlias:     wireAliasesFrom(info.MonitorsAlias),
		MonitorsEvent:     wireEventRefsFrom(info.MonitorsEvent),
		MonitorsNode:      wireAtoms(info.MonitorsNode),
		LinksPID:          wirePIDsFrom(info.LinksPID),
		LinksProcessID:    wireProcessIDsFrom(info.LinksProcessID),
		LinksAlias:        wireAliasesFrom(info.LinksAlias),
		LinksEvent:        wireEventRefsFrom(info.LinksEvent),
		LinksNode:         wireAtoms(info.LinksNode),
		LogLevel:          info.LogLevel.String(),
		KeepNetworkOrder:  info.KeepNetworkOrder,
		ImportantDelivery: info.ImportantDelivery,
	}
}

func wireProcessInfoFrom(m inspect.MessageInspectProcess) wireProcessInfo {
	return wireProcessInfo{Node: string(m.Node), Info: wireProcessDetailFrom(m.Info)}
}

type wireMeta struct {
	ID              wireAlias      `json:"i"`
	Parent          wirePID        `json:"p"`
	Application     string         `json:"a,omitempty"`
	Behavior        string         `json:"b,omitempty"`
	MailboxSize     int64          `json:"ms,omitempty"`
	MailboxQueues   wireMetaQueues `json:"mq,omitempty"`
	MessagePriority string         `json:"mp,omitempty"`
	MessagesIn      uint64         `json:"mi,omitempty"`
	MessagesOut     uint64         `json:"mo,omitempty"`
	LogLevel        string         `json:"ll,omitempty"`
	Uptime          int64          `json:"u,omitempty"`
	State           string         `json:"s,omitempty"`
}

type wireMetaInfo struct {
	Node string   `json:"nd"`
	Info wireMeta `json:"in"`
}

func wireMetaInfoFrom(m inspect.MessageInspectMeta) wireMetaInfo {
	return wireMetaInfo{
		Node: string(m.Node),
		Info: wireMeta{
			ID:          wireAliasFrom(m.Info.ID),
			Parent:      wirePIDFrom(m.Info.Parent),
			Application: string(m.Info.Application),
			Behavior:    m.Info.Behavior,
			MailboxSize: m.Info.MailboxSize,
			MailboxQueues: wireMetaQueues{
				Main:   m.Info.MailboxQueues.Main,
				System: m.Info.MailboxQueues.System,
			},
			MessagePriority: m.Info.MessagePriority.String(),
			MessagesIn:      m.Info.MessagesIn,
			MessagesOut:     m.Info.MessagesOut,
			LogLevel:        m.Info.LogLevel.String(),
			Uptime:          m.Info.Uptime,
			State:           m.Info.State.String(),
		},
	}
}
