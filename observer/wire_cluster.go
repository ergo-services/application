package observer

import "math"

type wireCluster struct {
	Nodes    []wireClusterNode    `json:"nodes"`
	Offline  []wireClusterOffline `json:"offline,omitempty"`
	Complete bool                 `json:"complete"`
	Note     string               `json:"note,omitempty"`
	Watched  int                  `json:"watched"`
	Limit    int                  `json:"limit"`

	// WatchPeriod in milliseconds, the interval a reading is expected on.
	WatchPeriod int64 `json:"wp,omitempty"`
}

type wireClusterNode struct {
	Name                  string      `json:"n"`
	Creation              int64       `json:"c"`
	Uptime                int64       `json:"u"`
	Version               wireVersion `json:"v,omitempty"`
	Framework             wireVersion `json:"fw,omitempty"`
	Mode                  string      `json:"md,omitempty"`
	LogLevel              string      `json:"ll,omitempty"`
	ProcessesTotal        int64       `json:"p"`
	ProcessesRunning      int64       `json:"pr"`
	ProcessesWaitResponse int64       `json:"pw,omitempty"`
	ProcessesZombee       int64       `json:"pz,omitempty"`
	ProcessesSpawned      uint64      `json:"ps,omitempty"`
	ProcessesSpawnFailed  uint64      `json:"pf,omitempty"`
	ProcessesTerminated   uint64      `json:"pt,omitempty"`
	ApplicationsTotal     int64       `json:"at,omitempty"`
	ApplicationsRunning   int64       `json:"ar,omitempty"`
	Applications          []string    `json:"ap,omitempty"`
	SendErrorsLocal       uint64      `json:"sel,omitempty"`
	SendErrorsRemote      uint64      `json:"ser,omitempty"`
	CallErrorsLocal       uint64      `json:"cel,omitempty"`
	CallErrorsRemote      uint64      `json:"cer,omitempty"`
	LogMessages           [6]uint64   `json:"lm"`
	MemoryUsed            uint64      `json:"m,omitempty"`
	MemoryAlloc           uint64      `json:"ma,omitempty"`

	// MemoryLimit is GOMEMLIMIT, absent when nobody set one.
	MemoryLimit  uint64            `json:"ml,omitempty"`
	HeapLive     uint64            `json:"hl,omitempty"`
	HeapGoal     uint64            `json:"hg,omitempty"`
	Goroutines   int64             `json:"g,omitempty"`
	GCCycles     uint64            `json:"gc,omitempty"`
	CPUTimeGC    float64           `json:"cg,omitempty"`
	CPUTimeTotal float64           `json:"ct,omitempty"`
	UserTime     int64             `json:"ut,omitempty"`
	SystemTime   int64             `json:"syt,omitempty"`
	ServerTime   int64             `json:"st"`
	Age          int64             `json:"ag,omitempty"`
	Peers        []wireClusterPeer `json:"pp,omitempty"`

	// Status and Reason are absent while the node is reachable.
	Status string `json:"sts,omitempty"`
	Reason string `json:"rs,omitempty"`
}

type wireClusterPeer struct {
	Node             string `json:"n"`
	ConnectionUptime int64  `json:"cu,omitempty"`
	MessagesIn       uint64 `json:"mi,omitempty"`
	MessagesOut      uint64 `json:"mo,omitempty"`
	BytesIn          uint64 `json:"bi,omitempty"`
	BytesOut         uint64 `json:"bo,omitempty"`
	Reconnections    uint64 `json:"r,omitempty"`
	TLS              bool   `json:"t,omitempty"`
}

type wireClusterOffline struct {
	Node  string   `json:"n"`
	Peers []string `json:"pp,omitempty"`
}

func memoryLimit(limit uint64) uint64 {
	if limit >= math.MaxInt64 {
		return 0
	}
	return limit
}

func wireClusterFrom(info ClusterInfo) wireCluster {
	out := wireCluster{
		Nodes:       make([]wireClusterNode, 0, len(info.Nodes)),
		Complete:    info.Complete,
		Note:        info.Note,
		Watched:     info.Watched,
		Limit:       info.Limit,
		WatchPeriod: info.WatchPeriod.Milliseconds(),
	}

	for i := range info.Nodes {
		reading := &info.Nodes[i]
		node := &reading.Info

		status := ""
		if reading.Status != "" && reading.Status != statusReachable {
			status = string(reading.Status)
		}

		peers := make([]wireClusterPeer, 0, len(node.Peers))
		for _, peer := range node.Peers {
			peers = append(peers, wireClusterPeer{
				Node:             string(peer.Node),
				ConnectionUptime: peer.ConnectionUptime,
				MessagesIn:       peer.MessagesIn,
				MessagesOut:      peer.MessagesOut,
				BytesIn:          peer.BytesIn,
				BytesOut:         peer.BytesOut,
				Reconnections:    peer.Reconnections,
				TLS:              peer.TLS,
			})
		}

		applications := make([]string, 0, len(node.Applications))
		for _, name := range node.Applications {
			applications = append(applications, string(name))
		}

		out.Nodes = append(out.Nodes, wireClusterNode{
			Name:                  string(node.Name),
			Creation:              node.Creation,
			Uptime:                node.Uptime,
			Version:               wireVersionFrom(node.Version),
			Framework:             wireVersionFrom(node.Framework),
			Mode:                  node.Mode.String(),
			LogLevel:              node.LogLevel.String(),
			ProcessesTotal:        node.ProcessesTotal,
			ProcessesRunning:      node.ProcessesRunning,
			ProcessesWaitResponse: node.ProcessesWaitResponse,
			ProcessesZombee:       node.ProcessesZombee,
			ProcessesSpawned:      node.ProcessesSpawned,
			ProcessesSpawnFailed:  node.ProcessesSpawnFailed,
			ProcessesTerminated:   node.ProcessesTerminated,
			ApplicationsTotal:     node.ApplicationsTotal,
			ApplicationsRunning:   node.ApplicationsRunning,
			Applications:          applications,
			SendErrorsLocal:       node.SendErrorsLocal,
			SendErrorsRemote:      node.SendErrorsRemote,
			CallErrorsLocal:       node.CallErrorsLocal,
			CallErrorsRemote:      node.CallErrorsRemote,
			LogMessages:           node.LogMessages,
			MemoryUsed:            node.MemoryUsed,
			MemoryAlloc:           node.MemoryAlloc,
			MemoryLimit:           memoryLimit(node.MemoryLimit),
			HeapLive:              node.HeapLive,
			HeapGoal:              node.HeapGoal,
			Goroutines:            node.Goroutines,
			GCCycles:              node.GCCycles,
			CPUTimeGC:             node.CPUTimeGC,
			CPUTimeTotal:          node.CPUTimeTotal,
			UserTime:              node.UserTime,
			SystemTime:            node.SystemTime,
			ServerTime:            node.ServerTime.UnixMilli(),
			Age:                   reading.Age,
			Peers:                 peers,
			Status:                status,
			Reason:                reading.Reason,
		})
	}

	for _, node := range info.Offline {
		peers := make([]string, 0, len(node.Peers))
		for _, peer := range node.Peers {
			peers = append(peers, string(peer))
		}
		out.Offline = append(out.Offline, wireClusterOffline{Node: string(node.Node), Peers: peers})
	}

	return out
}
