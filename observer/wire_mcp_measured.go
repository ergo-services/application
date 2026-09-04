package observer

import (
	"ergo.services/ergo/app/system/inspect"
	"ergo.services/ergo/gen"
)

type wireMCPMailboxQueues struct {
	Main          int64 `unit:"messages"`
	System        int64 `unit:"messages"`
	Urgent        int64 `unit:"messages"`
	Log           int64 `unit:"messages"`
	LatencyMain   int64 `unit:"ns" sentinel:"-1 = built without -tags=latency, 0 = the queue is empty"`
	LatencySystem int64 `unit:"ns" sentinel:"-1 = built without -tags=latency, 0 = the queue is empty"`
	LatencyUrgent int64 `unit:"ns" sentinel:"-1 = built without -tags=latency, 0 = the queue is empty"`
	LatencyLog    int64 `unit:"ns" sentinel:"-1 = built without -tags=latency, 0 = the queue is empty"`
}

type wireMCPMetaQueues struct {
	Main   int64 `unit:"messages"`
	System int64 `unit:"messages"`
}

func wireMCPMetaQueuesOf(q gen.MailboxQueues) wireMCPMetaQueues {
	return wireMCPMetaQueues{Main: q.Main, System: q.System}
}

func wireMCPMailboxQueuesOf(q gen.MailboxQueues) wireMCPMailboxQueues {
	return wireMCPMailboxQueues{
		Main:          q.Main,
		System:        q.System,
		Urgent:        q.Urgent,
		Log:           q.Log,
		LatencyMain:   q.LatencyMain,
		LatencySystem: q.LatencySystem,
		LatencyUrgent: q.LatencyUrgent,
		LatencyLog:    q.LatencyLog,
	}
}

type wireMCPHeapRecord struct {
	InuseBytes   int64 `unit:"bytes"`
	InuseObjects int64
	AllocBytes   int64 `unit:"bytes"`
	AllocObjects int64
	FreeObjects  int64
	Stack        []string
}

func wireMCPHeapRecords(list []inspect.HeapRecord) []wireMCPHeapRecord {
	if list == nil {
		return nil
	}
	out := make([]wireMCPHeapRecord, len(list))
	for i, record := range list {
		out[i] = wireMCPHeapRecord{
			InuseBytes:   record.InuseBytes,
			InuseObjects: record.InuseObjects,
			AllocBytes:   record.AllocBytes,
			AllocObjects: record.AllocObjects,
			FreeObjects:  record.FreeObjects,
			Stack:        record.Stack,
		}
	}
	return out
}

type wireMCPEventEntry struct {
	Timestamp int64 `unit:"unix ns"`
	Type      string
	Message   string
	Verbose   string
}

func wireMCPEventEntries(list []inspect.InspectEventEntry) []wireMCPEventEntry {
	if list == nil {
		return nil
	}
	out := make([]wireMCPEventEntry, len(list))
	for i, entry := range list {
		out[i] = wireMCPEventEntry{
			Timestamp: entry.Timestamp,
			Type:      entry.Type,
			Message:   entry.Message,
			Verbose:   entry.Verbose,
		}
	}
	return out
}

type wireMCPRemoteNodeShortInfo struct {
	Node             gen.Atom
	ConnectionUptime int64 `unit:"sec"`
	MessagesIn       uint64
	MessagesOut      uint64
	BytesIn          uint64 `unit:"bytes"`
	BytesOut         uint64 `unit:"bytes"`
	Reconnections    uint64
	TLS              bool
}

func wireMCPPeers(list []gen.RemoteNodeShortInfo) []wireMCPRemoteNodeShortInfo {
	if list == nil {
		return nil
	}
	out := make([]wireMCPRemoteNodeShortInfo, len(list))
	for i, peer := range list {
		out[i] = wireMCPRemoteNodeShortInfo{
			Node:             peer.Node,
			ConnectionUptime: peer.ConnectionUptime,
			MessagesIn:       peer.MessagesIn,
			MessagesOut:      peer.MessagesOut,
			BytesIn:          peer.BytesIn,
			BytesOut:         peer.BytesOut,
			Reconnections:    peer.Reconnections,
			TLS:              peer.TLS,
		}
	}
	return out
}
