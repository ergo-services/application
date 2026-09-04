package observer

import (
	"time"

	"ergo.services/ergo/app/system/inspect"
	"ergo.services/ergo/gen"
)

type wireMCPNodeInfo struct {
	Name                  gen.Atom
	Uptime                int64 `unit:"sec"`
	Version               gen.Version
	Framework             gen.Version
	Commercial            []gen.Version
	Env                   map[gen.Env]any `sentinel:"empty unless NodeOptions.Security.ExposeEnvInfo is enabled"`
	LogLevel              string
	Loggers               []wireMCPLoggerInfo
	Tracing               gen.TracingInfo
	TracingExporters      []wireMCPTracingExporterInfo
	LogMessages           [6]uint64 `axis:"trace,debug,info,warning,error,panic"`
	TracingSpans          [5]uint64 `axis:"send,request,response,spawn,terminate"`
	Cron                  gen.CronInfo
	ProcessesTotal        int64
	ProcessesRunning      int64
	ProcessesWaitResponse int64
	ProcessesZombee       int64
	ProcessesSpawned      uint64
	ProcessesSpawnFailed  uint64
	ProcessesTerminated   uint64
	SendErrorsLocal       uint64
	SendErrorsRemote      uint64
	CallErrorsLocal       uint64
	CallErrorsRemote      uint64
	RegisteredAliases     int64
	RegisteredNames       int64
	RegisteredEvents      int64
	EventsPublished       int64
	EventsReceived        int64
	EventsLocalSent       int64
	EventsRemoteSent      int64
	ApplicationsTotal     int64
	ApplicationsRunning   int64
	MemoryUsed            uint64 `unit:"bytes"`
	MemoryAlloc           uint64 `unit:"bytes"`
	MemoryLimit           uint64 `unit:"bytes" sentinel:"MaxInt64 = no limit is set"`
	HeapLive              uint64 `unit:"bytes"`
	HeapGoal              uint64 `unit:"bytes"`
	Goroutines            int64
	GCCycles              uint64
	HeapAllocObjects      uint64  `unit:"objects"`
	HeapFreeObjects       uint64  `unit:"objects"`
	CPUTimeGC             float64 `unit:"sec"`
	CPUTimeTotal          float64 `unit:"sec"`
	UserTime              int64   `unit:"ns"`
	SystemTime            int64   `unit:"ns"`
	ServerTime            time.Time

	Legend map[string]any `json:"services.ergo/legend,omitempty"`
}

type wireMCPLoggerInfo struct {
	Name     string
	Behavior string
	Levels   []string
}

func wireMCPLoggers(list []gen.LoggerInfo) []wireMCPLoggerInfo {
	if list == nil {
		return nil
	}
	out := make([]wireMCPLoggerInfo, len(list))
	for i, info := range list {
		out[i] = wireMCPLoggerInfo{Name: info.Name, Behavior: info.Behavior}
		if info.Levels != nil {
			out[i].Levels = make([]string, len(info.Levels))
			for j, level := range info.Levels {
				out[i].Levels[j] = level.String()
			}
		}
	}
	return out
}

type wireMCPTracingExporterInfo struct {
	Name         string
	Behavior     string
	Flags        []string
	DroppedSpans uint64
}

func wireMCPTracingExporters(list []gen.TracingExporterInfo) []wireMCPTracingExporterInfo {
	if list == nil {
		return nil
	}
	out := make([]wireMCPTracingExporterInfo, len(list))
	for i, info := range list {
		out[i] = wireMCPTracingExporterInfo{
			Name:         info.Name,
			Behavior:     info.Behavior,
			Flags:        info.Flags.Names(),
			DroppedSpans: info.DroppedSpans,
		}
	}
	return out
}

var wireMCPNodeLegend = mcpLegendFor(wireMCPNodeInfo{})

func wireMCPNodeInfoOf(info gen.NodeInfo) wireMCPNodeInfo {
	return wireMCPNodeInfo{
		Name:                  info.Name,
		Uptime:                info.Uptime,
		Version:               info.Version,
		Framework:             info.Framework,
		Commercial:            info.Commercial,
		Env:                   info.Env,
		LogLevel:              info.LogLevel.String(),
		Loggers:               wireMCPLoggers(info.Loggers),
		Tracing:               info.Tracing,
		TracingExporters:      wireMCPTracingExporters(info.TracingExporters),
		LogMessages:           info.LogMessages,
		TracingSpans:          info.TracingSpans,
		Cron:                  info.Cron,
		ProcessesTotal:        info.ProcessesTotal,
		ProcessesRunning:      info.ProcessesRunning,
		ProcessesWaitResponse: info.ProcessesWaitResponse,
		ProcessesZombee:       info.ProcessesZombee,
		ProcessesSpawned:      info.ProcessesSpawned,
		ProcessesSpawnFailed:  info.ProcessesSpawnFailed,
		ProcessesTerminated:   info.ProcessesTerminated,
		SendErrorsLocal:       info.SendErrorsLocal,
		SendErrorsRemote:      info.SendErrorsRemote,
		CallErrorsLocal:       info.CallErrorsLocal,
		CallErrorsRemote:      info.CallErrorsRemote,
		RegisteredAliases:     info.RegisteredAliases,
		RegisteredNames:       info.RegisteredNames,
		RegisteredEvents:      info.RegisteredEvents,
		EventsPublished:       info.EventsPublished,
		EventsReceived:        info.EventsReceived,
		EventsLocalSent:       info.EventsLocalSent,
		EventsRemoteSent:      info.EventsRemoteSent,
		ApplicationsTotal:     info.ApplicationsTotal,
		ApplicationsRunning:   info.ApplicationsRunning,
		MemoryUsed:            info.MemoryUsed,
		MemoryAlloc:           info.MemoryAlloc,
		MemoryLimit:           info.MemoryLimit,
		HeapLive:              info.HeapLive,
		HeapGoal:              info.HeapGoal,
		Goroutines:            info.Goroutines,
		GCCycles:              info.GCCycles,
		HeapAllocObjects:      info.HeapAllocObjects,
		HeapFreeObjects:       info.HeapFreeObjects,
		CPUTimeGC:             info.CPUTimeGC,
		CPUTimeTotal:          info.CPUTimeTotal,
		UserTime:              info.UserTime,
		SystemTime:            info.SystemTime,
		ServerTime:            info.ServerTime,
		Legend:                wireMCPNodeLegend,
	}
}

type wireMCPNode struct {
	Node gen.Atom
	Info wireMCPNodeInfo
}

type wireMCPGetNode struct {
	Node  gen.Atom
	Info  wireMCPNodeInfo
	Error string `json:"Error,omitempty"`
}

func init() {
	mcpRegisterView(inspect.MessageInspectNode{}, func(value any) any {
		m, ok := value.(inspect.MessageInspectNode)
		if ok == false {
			return value
		}
		return wireMCPNode{Node: m.Node, Info: wireMCPNodeInfoOf(m.Info)}
	})

	mcpRegisterView(inspect.ResponseGetNode{}, func(value any) any {
		r, ok := value.(inspect.ResponseGetNode)
		if ok == false {
			return value
		}
		return wireMCPGetNode{
			Node: r.Node, Info: wireMCPNodeInfoOf(r.Info), Error: mcpErrorText(r.Error),
		}
	})
}
