package grid

import "ergo.services/ergo/gen"

// Cross-node wire messages.
type messagePeerConnect struct {
	From      gen.PID
	Domain    gen.Atom
	NumShards int
	Index     int
}

type messagePeerConnectAck struct {
	From      gen.PID
	Domain    gen.Atom
	NumShards int
	Index     int
}

// Cross-node registry replication messages.
type messageRegister struct {
	Key   string
	Owner gen.PID
	Meta  any
	Time  int64
}

type messageUnregister struct {
	Key    string
	Owner  gen.PID
	Reason UnregisterReason
}

type messageClusterState struct {
	Node    gen.Atom
	At      int64
	Entries []regEntry
}

type regEntry struct {
	Key   string
	Owner gen.PID
	Meta  any
	Time  int64
}

// Node-local Call payloads.
type getPeersRequest struct{}

type getPeersResponse struct {
	Nodes []gen.Atom
}

type registerRequest struct {
	Key  string
	PID  gen.PID
	Meta any
}

type registerResponse struct{}

type unregisterRequest struct {
	Key string
	PID gen.PID
}

type unregisterResponse struct{}

// Node-local messages.
type messageBootstrap struct{}

type messageSeedRetry struct{ Node gen.Atom }

type messageReconcile struct{}

// Node-local subscription messages.
type messageMonitor struct {
	Subscriber gen.PID
	Kind       subKind
	Match      string
}

type messageUnmonitor struct {
	Subscriber gen.PID
	Kind       subKind
	Match      string
}
