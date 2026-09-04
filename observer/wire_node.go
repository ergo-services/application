package observer

import (
	"time"

	"ergo.services/ergo/app/system/inspect"
	"ergo.services/ergo/gen"
)

type wireLogger struct {
	Name     string   `json:"n"`
	Behavior string   `json:"b,omitempty"`
	Levels   []string `json:"lv,omitempty"`
}

type wireFallback struct {
	Enable bool   `json:"e,omitempty"`
	Name   string `json:"n,omitempty"`
	Tag    string `json:"tg,omitempty"`
}

func wireFallbackFrom(f gen.ProcessFallback) wireFallback {
	return wireFallback{Enable: f.Enable, Name: string(f.Name), Tag: f.Tag}
}

type wireCronJob struct {
	Disabled   bool         `json:"d,omitempty"`
	Name       string       `json:"n"`
	Spec       string       `json:"sp,omitempty"`
	Location   string       `json:"lc,omitempty"`
	ActionInfo string       `json:"ai,omitempty"`
	LastRun    string       `json:"lr,omitempty"`
	LastErr    string       `json:"le,omitempty"`
	Fallback   wireFallback `json:"fb,omitempty"`
}

type wireCron struct {
	Next  string        `json:"nx,omitempty"`
	Spool []string      `json:"sp,omitempty"`
	Jobs  []wireCronJob `json:"j,omitempty"`
}

type wireTracingConfig struct {
	Sampler    string                 `json:"s,omitempty"`
	Attributes []wireTracingAttribute `json:"at,omitempty"`
}

type wireTracingExporter struct {
	Name         string   `json:"n"`
	Behavior     string   `json:"b,omitempty"`
	Flags        []string `json:"fl,omitempty"`
	DroppedSpans uint64   `json:"ds,omitempty"`
}

type wireNode struct {
	Name                  string                `json:"n"`
	Uptime                int64                 `json:"u,omitempty"`
	Version               wireVersion           `json:"v,omitempty"`
	Framework             wireVersion           `json:"fw,omitempty"`
	Commercial            []wireVersion         `json:"cm,omitempty"`
	Env                   map[string]any        `json:"e,omitempty"`
	LogLevel              string                `json:"ll,omitempty"`
	Loggers               []wireLogger          `json:"lg,omitempty"`
	Tracing               wireTracingConfig     `json:"tr,omitempty"`
	TracingExporters      []wireTracingExporter `json:"tx,omitempty"`
	LogMessages           [6]uint64             `json:"lm,omitempty"`
	TracingSpans          [5]uint64             `json:"ts,omitempty"`
	Cron                  wireCron              `json:"cr,omitempty"`
	ProcessesTotal        int64                 `json:"p,omitempty"`
	ProcessesRunning      int64                 `json:"pr,omitempty"`
	ProcessesWaitResponse int64                 `json:"pw,omitempty"`
	ProcessesZombee       int64                 `json:"pz,omitempty"`
	ProcessesSpawned      uint64                `json:"ps,omitempty"`
	ProcessesSpawnFailed  uint64                `json:"pf,omitempty"`
	ProcessesTerminated   uint64                `json:"pt,omitempty"`
	SendErrorsLocal       uint64                `json:"sel,omitempty"`
	SendErrorsRemote      uint64                `json:"ser,omitempty"`
	CallErrorsLocal       uint64                `json:"cel,omitempty"`
	CallErrorsRemote      uint64                `json:"cer,omitempty"`
	RegisteredAliases     int64                 `json:"ra,omitempty"`
	RegisteredNames       int64                 `json:"rn,omitempty"`
	RegisteredEvents      int64                 `json:"re,omitempty"`
	EventsPublished       int64                 `json:"ep,omitempty"`
	EventsReceived        int64                 `json:"er,omitempty"`
	EventsLocalSent       int64                 `json:"els,omitempty"`
	EventsRemoteSent      int64                 `json:"ers,omitempty"`
	ApplicationsTotal     int64                 `json:"at,omitempty"`
	ApplicationsRunning   int64                 `json:"ar,omitempty"`
	MemoryUsed            uint64                `json:"m,omitempty"`
	MemoryAlloc           uint64                `json:"ma,omitempty"`
	MemoryLimit           uint64                `json:"ml,omitempty"`
	HeapLive              uint64                `json:"hl,omitempty"`
	HeapGoal              uint64                `json:"hg,omitempty"`
	Goroutines            int64                 `json:"g,omitempty"`
	GCCycles              uint64                `json:"gc,omitempty"`
	HeapAllocObjects      uint64                `json:"hao,omitempty"`
	HeapFreeObjects       uint64                `json:"hfo,omitempty"`
	CPUTimeGC             float64               `json:"cg,omitempty"`
	CPUTimeTotal          float64               `json:"ct,omitempty"`
	UserTime              int64                 `json:"ut,omitempty"`
	SystemTime            int64                 `json:"st,omitempty"`
	ServerTime            int64                 `json:"srt,omitempty"`
}

type wireNodeInfo struct {
	Node string   `json:"nd"`
	Info wireNode `json:"in"`
}

func wireNodeFrom(info gen.NodeInfo) wireNode {
	loggers := make([]wireLogger, 0, len(info.Loggers))
	for _, logger := range info.Loggers {
		levels := make([]string, 0, len(logger.Levels))
		for _, level := range logger.Levels {
			levels = append(levels, level.String())
		}
		loggers = append(loggers, wireLogger{Name: logger.Name, Behavior: logger.Behavior, Levels: levels})
	}

	commercial := make([]wireVersion, 0, len(info.Commercial))
	for _, version := range info.Commercial {
		commercial = append(commercial, wireVersionFrom(version))
	}

	exporters := make([]wireTracingExporter, 0, len(info.TracingExporters))
	for _, exporter := range info.TracingExporters {
		exporters = append(exporters, wireTracingExporter{
			Name:         exporter.Name,
			Behavior:     exporter.Behavior,
			Flags:        exporter.Flags.Names(),
			DroppedSpans: exporter.DroppedSpans,
		})
	}

	spool := make([]string, 0, len(info.Cron.Spool))
	for _, name := range info.Cron.Spool {
		spool = append(spool, string(name))
	}
	jobs := make([]wireCronJob, 0, len(info.Cron.Jobs))
	for _, job := range info.Cron.Jobs {
		lastRun := ""
		if job.LastRun.IsZero() == false {
			lastRun = job.LastRun.Format(time.RFC3339Nano)
		}
		jobs = append(jobs, wireCronJob{
			Disabled:   job.Disabled,
			Name:       string(job.Name),
			Spec:       job.Spec,
			Location:   job.Location,
			ActionInfo: job.ActionInfo,
			LastRun:    lastRun,
			LastErr:    job.LastErr,
			Fallback:   wireFallbackFrom(job.Fallback),
		})
	}
	next := ""
	if info.Cron.Next.IsZero() == false {
		next = info.Cron.Next.Format(time.RFC3339Nano)
	}

	return wireNode{
		Name:       string(info.Name),
		Uptime:     info.Uptime,
		Version:    wireVersionFrom(info.Version),
		Framework:  wireVersionFrom(info.Framework),
		Commercial: commercial,
		Env:        wireEnv(info.Env),
		LogLevel:   info.LogLevel.String(),
		Loggers:    loggers,
		Tracing: wireTracingConfig{
			Sampler:    info.Tracing.Sampler,
			Attributes: wireTracingAttributesFrom(info.Tracing.Attributes),
		},
		TracingExporters:      exporters,
		LogMessages:           info.LogMessages,
		TracingSpans:          info.TracingSpans,
		Cron:                  wireCron{Next: next, Spool: spool, Jobs: jobs},
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
		MemoryLimit:           memoryLimit(info.MemoryLimit),
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
		ServerTime:            info.ServerTime.UnixMilli(),
	}
}

func wireNodeInfoFrom(m inspect.MessageInspectNode) wireNodeInfo {
	return wireNodeInfo{Node: string(m.Node), Info: wireNodeFrom(m.Info)}
}
