package observer

import (
	"sync"
	"time"

	"ergo.services/ergo/gen"
)

const (
	clusterName gen.Atom = "observer_cluster"

	watcherPrefix = "observer_watcher_"

	envClusterStore gen.Env = "observer_cluster_store"

	envClusterLensOptions gen.Env = "observer_cluster_options"

	clusterRefreshPeriod  = 10 * time.Second
	retryAfterWatcherDown = 5 * time.Second
)

// RequestClusterInfo asks for the whole cluster map at once.
type RequestClusterInfo struct{}

// ClusterNodeInfo is one node with a reading, and how old that reading is.
type ClusterNodeInfo struct {
	Info gen.NodeShortInfo

	// Age is how long ago the observer received this reading, in milliseconds.
	Age int64

	// Status says whether the reading is fresh or the last one before the node went.
	Status gen.Atom

	// Reason is why the node became unreachable.
	Reason string
}

// ClusterInfo is the whole cluster map.
type ClusterInfo struct {
	Nodes []ClusterNodeInfo `json:"nodes"`

	// Offline are the nodes without data: unreachable, or discovered beyond the watch limit.
	Offline []OfflineNode `json:"offline"`

	// Complete reports whether the map covers the whole cluster.
	Complete bool   `json:"complete"`
	Note     string `json:"note,omitempty"`

	// Watched and Limit show how much of the map is actually being observed.
	Watched int `json:"watched"`
	Limit   int `json:"limit"`

	// WatchPeriod is how often a watched node is expected to report.
	WatchPeriod time.Duration `json:"watch_period"`
}

const (
	statusReachable   gen.Atom = "reachable"
	statusUnreachable gen.Atom = "unreachable"
	statusDiscovered  gen.Atom = "discovered"
)

type nodeSnapshot struct {
	Info *gen.NodeShortInfo
	At   time.Time
}

// one writer each: snapshots by the watcher owning the node, members by the cluster actor
type clusterStore struct {
	snapshots sync.Map // node -> *nodeSnapshot, only while reachable
	members   sync.Map // node -> *memberState, every discovered node
}

func newClusterStore() *clusterStore {
	return &clusterStore{}
}

func (s *clusterStore) snapshot(node gen.Atom) (*nodeSnapshot, bool) {
	v, found := s.snapshots.Load(node)
	if found == false {
		return nil, false
	}
	return v.(*nodeSnapshot), true
}

func (s *clusterStore) member(node gen.Atom) (*memberState, bool) {
	v, found := s.members.Load(node)
	if found == false {
		return nil, false
	}
	return v.(*memberState), true
}

// replaced as a whole, never mutated in place
type memberState struct {
	Node   gen.Atom
	Status gen.Atom
	Reason string

	Since    int64 // when the node entered the current status
	LastSeen int64 // last successful snapshot, zero if never reached

	// empty for a node that was never reached: its edges are reported by the nodes listing it
	Peers []gen.Atom

	Registrar bool              // the registrar currently lists this node
	Refs      map[gen.Atom]bool // the nodes listing this one as their peer
	Watched   bool
}

func (m *memberState) clone() *memberState {
	c := *m
	c.Refs = make(map[gen.Atom]bool, len(m.Refs))
	for node := range m.Refs {
		c.Refs[node] = true
	}
	c.Peers = append([]gen.Atom(nil), m.Peers...)
	return &c
}

// OfflineNode is a node on the map that currently has no data.
type OfflineNode struct {
	Node     gen.Atom   `json:"node"`
	Status   gen.Atom   `json:"status"`
	Reason   string     `json:"reason,omitempty"`
	Since    int64      `json:"since"`
	LastSeen int64      `json:"last_seen,omitempty"`
	Peers    []gen.Atom `json:"peers,omitempty"`
}

type messageWatcherPeers struct {
	Node  gen.Atom
	Peers []gen.Atom
}

type messageWatcherReachable struct {
	Node gen.Atom
}

type messageWatcherUnreachable struct {
	Node   gen.Atom
	Reason string
}

type messageWatcherStop struct{}

type messageWatcherWake struct{}

type messageRetryNode struct {
	Node gen.Atom
}

type messageSilence struct{}

// missed snapshots, not a duration
const silencePeriods = 3

type messageGraceExpired struct {
	Node gen.Atom
}

type messageReconcile struct{}

type messageRefresh struct{}

// moves work out of Init, where an error would surface as a spawn failure in the parent
type messageRun struct{}

type watcherArgs struct {
	node   gen.Atom
	store  *clusterStore
	period time.Duration
	keep   time.Duration // how long a reading outlives the node it came from
}
