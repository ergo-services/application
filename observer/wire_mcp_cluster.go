package observer

import (
	"time"

	"ergo.services/ergo/gen"
)

type wireMCPCluster struct {
	Nodes       []wireMCPClusterNode
	Offline     []wireMCPOfflineNode
	Complete    bool
	Note        string `json:"Note,omitempty"`
	Watched     int
	Limit       int
	WatchPeriod time.Duration `unit:"ns"`

	Legend map[string]any `json:"services.ergo/legend,omitempty"`
}

type wireMCPClusterNode struct {
	Info   wireMCPNodeShortInfo
	Age    int64 `unit:"ms"`
	Status gen.Atom
	Reason string `json:"Reason,omitempty"`
}

type wireMCPOfflineNode struct {
	Node     gen.Atom
	Status   gen.Atom
	Reason   string `json:"Reason,omitempty"`
	Since    int64  `unit:"unix sec"`
	LastSeen int64  `unit:"unix sec" sentinel:"0 = the node was never reached"`
	Peers    []gen.Atom
}

type wireMCPNodeShortInfo struct {
	Name                  gen.Atom
	Creation              int64
	Uptime                int64 `unit:"sec"`
	Version               gen.Version
	Framework             gen.Version
	Mode                  string
	LogLevel              string
	ProcessesTotal        int64
	ProcessesRunning      int64
	ProcessesWaitResponse int64
	ProcessesZombee       int64
	ProcessesSpawned      uint64
	ProcessesSpawnFailed  uint64
	ProcessesTerminated   uint64
	ApplicationsTotal     int64
	ApplicationsRunning   int64
	SendErrorsLocal       uint64
	SendErrorsRemote      uint64
	CallErrorsLocal       uint64
	CallErrorsRemote      uint64
	LogMessages           [6]uint64 `axis:"trace,debug,info,warning,error,panic"`
	MemoryUsed            uint64    `unit:"bytes"`
	MemoryAlloc           uint64    `unit:"bytes"`
	MemoryLimit           uint64    `unit:"bytes" sentinel:"MaxInt64 = no limit is set"`
	HeapLive              uint64    `unit:"bytes"`
	HeapGoal              uint64    `unit:"bytes"`
	Goroutines            int64
	GCCycles              uint64
	CPUTimeGC             float64 `unit:"sec"`
	CPUTimeTotal          float64 `unit:"sec"`
	UserTime              int64   `unit:"ns"`
	SystemTime            int64   `unit:"ns"`
	Applications          []gen.Atom
	Peers                 []wireMCPRemoteNodeShortInfo
	ServerTime            time.Time
}

func wireMCPNodeShortInfoOf(info gen.NodeShortInfo) wireMCPNodeShortInfo {
	return wireMCPNodeShortInfo{
		Name:                  info.Name,
		Creation:              info.Creation,
		Uptime:                info.Uptime,
		Version:               info.Version,
		Framework:             info.Framework,
		Mode:                  info.Mode.String(),
		LogLevel:              info.LogLevel.String(),
		ProcessesTotal:        info.ProcessesTotal,
		ProcessesRunning:      info.ProcessesRunning,
		ProcessesWaitResponse: info.ProcessesWaitResponse,
		ProcessesZombee:       info.ProcessesZombee,
		ProcessesSpawned:      info.ProcessesSpawned,
		ProcessesSpawnFailed:  info.ProcessesSpawnFailed,
		ProcessesTerminated:   info.ProcessesTerminated,
		ApplicationsTotal:     info.ApplicationsTotal,
		ApplicationsRunning:   info.ApplicationsRunning,
		SendErrorsLocal:       info.SendErrorsLocal,
		SendErrorsRemote:      info.SendErrorsRemote,
		CallErrorsLocal:       info.CallErrorsLocal,
		CallErrorsRemote:      info.CallErrorsRemote,
		LogMessages:           info.LogMessages,
		MemoryUsed:            info.MemoryUsed,
		MemoryAlloc:           info.MemoryAlloc,
		MemoryLimit:           info.MemoryLimit,
		HeapLive:              info.HeapLive,
		HeapGoal:              info.HeapGoal,
		Goroutines:            info.Goroutines,
		GCCycles:              info.GCCycles,
		CPUTimeGC:             info.CPUTimeGC,
		CPUTimeTotal:          info.CPUTimeTotal,
		UserTime:              info.UserTime,
		SystemTime:            info.SystemTime,
		Applications:          info.Applications,
		Peers:                 wireMCPPeers(info.Peers),
		ServerTime:            info.ServerTime,
	}
}

var wireMCPClusterLegend = mcpLegendFor(wireMCPCluster{})

func wireMCPClusterOf(info ClusterInfo) wireMCPCluster {
	out := wireMCPCluster{
		Complete:    info.Complete,
		Note:        info.Note,
		Watched:     info.Watched,
		Limit:       info.Limit,
		WatchPeriod: info.WatchPeriod,
		Legend:      wireMCPClusterLegend,
	}

	if info.Nodes != nil {
		out.Nodes = make([]wireMCPClusterNode, len(info.Nodes))
		for i, node := range info.Nodes {
			out.Nodes[i] = wireMCPClusterNode{
				Info:   wireMCPNodeShortInfoOf(node.Info),
				Age:    node.Age,
				Status: node.Status,
				Reason: node.Reason,
			}
		}
	}

	if info.Offline != nil {
		out.Offline = make([]wireMCPOfflineNode, len(info.Offline))
		for i, node := range info.Offline {
			out.Offline[i] = wireMCPOfflineNode{
				Node:     node.Node,
				Status:   node.Status,
				Reason:   node.Reason,
				Since:    node.Since,
				LastSeen: node.LastSeen,
				Peers:    node.Peers,
			}
		}
	}

	return out
}

func init() {
	mcpRegisterView(ClusterInfo{}, func(value any) any {
		info, ok := value.(ClusterInfo)
		if ok == false {
			return value
		}
		return wireMCPClusterOf(info)
	})
}
