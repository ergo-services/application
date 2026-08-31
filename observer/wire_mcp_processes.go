package observer

import (
	"ergo.services/ergo/app/system/inspect"
	"ergo.services/ergo/gen"
)

// the row four listings share: processes, app_tree, subtree and the ordered walk
type wireMCPProcessShortInfo struct {
	PID             string
	Name            gen.Atom
	Application     gen.Atom
	Behavior        string
	Kind            gen.ProcessKind
	MessagesIn      uint64
	MessagesOut     uint64
	MessagesMailbox uint64
	MailboxLatency  int64  `unit:"ns" sentinel:"-1 = built without -tags=latency, 0 = all queues empty"`
	RunningTime     uint64 `unit:"ns"`
	InitTime        uint64 `unit:"ns"`
	Wakeups         uint64
	Uptime          int64 `unit:"sec"`
	State           string
	StateTime       int64 `unit:"ns"`
	Parent          string
	Leader          string
	LogLevel        string
}

func wireMCPProcessShortInfoOf(info gen.ProcessShortInfo) wireMCPProcessShortInfo {
	return wireMCPProcessShortInfo{
		PID:             mcpPIDText(info.PID),
		Name:            info.Name,
		Application:     info.Application,
		Behavior:        info.Behavior,
		Kind:            info.Kind,
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
		Parent:          mcpPIDText(info.Parent),
		Leader:          mcpPIDText(info.Leader),
		LogLevel:        info.LogLevel.String(),
	}
}

func wireMCPProcessRows(list []gen.ProcessShortInfo) []wireMCPProcessShortInfo {
	if list == nil {
		return nil
	}
	out := make([]wireMCPProcessShortInfo, len(list))
	for i, info := range list {
		out[i] = wireMCPProcessShortInfoOf(info)
	}
	return out
}

type wireMCPGetProcessList struct {
	Node      gen.Atom
	Processes []wireMCPProcessShortInfo
	Truncated bool
	Error     string `json:"Error,omitempty"`

	Legend map[string]any `json:"services.ergo/legend,omitempty"`
}

type wireMCPGetProcessRange struct {
	Node      gen.Atom
	Processes []wireMCPProcessShortInfo
	Truncated bool
	Error     string `json:"Error,omitempty"`

	Legend map[string]any `json:"services.ergo/legend,omitempty"`
}

type wireMCPGetAppTree struct {
	Node        gen.Atom
	Application gen.Atom
	Processes   []wireMCPProcessShortInfo
	Truncated   int
	Error       string `json:"Error,omitempty"`

	Legend map[string]any `json:"services.ergo/legend,omitempty"`
}

type wireMCPGetSubtree struct {
	Node      gen.Atom
	PID       string
	Processes []wireMCPProcessShortInfo
	Truncated int
	Error     string `json:"Error,omitempty"`

	Legend map[string]any `json:"services.ergo/legend,omitempty"`
}

type wireMCPProcessList struct {
	Node      gen.Atom
	Processes []wireMCPProcessShortInfo

	Legend map[string]any `json:"services.ergo/legend,omitempty"`
}

var (
	wireMCPProcessListLegend     = mcpLegendFor(wireMCPProcessList{})
	wireMCPGetProcessListLegend  = mcpLegendFor(wireMCPGetProcessList{})
	wireMCPGetProcessRangeLegend = mcpLegendFor(wireMCPGetProcessRange{})
	wireMCPGetAppTreeLegend      = mcpLegendFor(wireMCPGetAppTree{})
	wireMCPGetSubtreeLegend      = mcpLegendFor(wireMCPGetSubtree{})
)

func init() {
	mcpRegisterView(inspect.ResponseGetProcessList{}, func(value any) any {
		r, ok := value.(inspect.ResponseGetProcessList)
		if ok == false {
			return value
		}
		return wireMCPGetProcessList{
			Node: r.Node, Processes: wireMCPProcessRows(r.Processes),
			Truncated: r.Truncated, Error: mcpErrorText(r.Error),
			Legend: wireMCPGetProcessListLegend,
		}
	})

	mcpRegisterView(inspect.ResponseGetProcessRange{}, func(value any) any {
		r, ok := value.(inspect.ResponseGetProcessRange)
		if ok == false {
			return value
		}
		return wireMCPGetProcessRange{
			Node: r.Node, Processes: wireMCPProcessRows(r.Processes),
			Truncated: r.Truncated, Error: mcpErrorText(r.Error),
			Legend: wireMCPGetProcessRangeLegend,
		}
	})

	mcpRegisterView(inspect.ResponseGetAppTree{}, func(value any) any {
		r, ok := value.(inspect.ResponseGetAppTree)
		if ok == false {
			return value
		}
		return wireMCPGetAppTree{
			Node: r.Node, Application: r.Application, Processes: wireMCPProcessRows(r.Processes),
			Truncated: r.Truncated, Error: mcpErrorText(r.Error),
			Legend: wireMCPGetAppTreeLegend,
		}
	})

	mcpRegisterView(inspect.ResponseGetSubtree{}, func(value any) any {
		r, ok := value.(inspect.ResponseGetSubtree)
		if ok == false {
			return value
		}
		return wireMCPGetSubtree{
			Node: r.Node, PID: mcpPIDText(r.PID), Processes: wireMCPProcessRows(r.Processes),
			Truncated: r.Truncated, Error: mcpErrorText(r.Error),
			Legend: wireMCPGetSubtreeLegend,
		}
	})

	mcpRegisterView(inspect.MessageInspectProcessList{}, func(value any) any {
		m, ok := value.(inspect.MessageInspectProcessList)
		if ok == false {
			return value
		}
		return wireMCPProcessList{
			Node: m.Node, Processes: wireMCPProcessRows(m.Processes),
			Legend: wireMCPProcessListLegend,
		}
	})
}
