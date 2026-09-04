package observer

import (
	"ergo.services/ergo/app/system/inspect"
	"ergo.services/ergo/gen"
)

type wireMCPRemoteNodeInfo struct {
	Node                    gen.Atom
	Uptime                  int64 `unit:"sec"`
	ConnectionUptime        int64 `unit:"sec"`
	Version                 gen.Version
	HandshakeVersion        gen.Version
	ProtoVersion            gen.Version
	NetworkFlags            wireMCPNetworkFlags
	PoolSize                int
	PoolLen                 int
	PoolDSN                 []string
	MaxMessageSize          int `unit:"bytes"`
	TLS                     bool
	MessagesIn              uint64
	MessagesOut             uint64
	BytesIn                 uint64 `unit:"bytes"`
	BytesOut                uint64 `unit:"bytes"`
	TransitBytesIn          uint64 `unit:"bytes"`
	TransitBytesOut         uint64 `unit:"bytes"`
	Reconnections           uint64
	FragmentsSent           uint64
	FragmentMessagesSent    uint64
	FragmentsReceived       uint64
	FragmentMessagesRecv    uint64
	FragmentTimeouts        uint64
	TracedSent              uint64
	TracedReceived          uint64
	CompressedSent          uint64
	CompressedBytesSent     uint64 `unit:"bytes"`
	CompressedOrigBytesSent uint64 `unit:"bytes"`
	DecompressedRecv        uint64
	DecompressedBytesRecv   uint64 `unit:"bytes"`
	DecompressedOrigRecv    uint64
	ClockSkew               int64 `unit:"ns" sentinel:"0 = not measured yet"`
}

func wireMCPRemoteNodeInfoOf(info gen.RemoteNodeInfo) wireMCPRemoteNodeInfo {
	return wireMCPRemoteNodeInfo{
		Node:                    info.Node,
		Uptime:                  info.Uptime,
		ConnectionUptime:        info.ConnectionUptime,
		Version:                 info.Version,
		HandshakeVersion:        info.HandshakeVersion,
		ProtoVersion:            info.ProtoVersion,
		NetworkFlags:            wireMCPNetworkFlagsOf(info.NetworkFlags),
		PoolSize:                info.PoolSize,
		PoolLen:                 info.PoolLen,
		PoolDSN:                 info.PoolDSN,
		MaxMessageSize:          info.MaxMessageSize,
		TLS:                     info.TLS,
		MessagesIn:              info.MessagesIn,
		MessagesOut:             info.MessagesOut,
		BytesIn:                 info.BytesIn,
		BytesOut:                info.BytesOut,
		TransitBytesIn:          info.TransitBytesIn,
		TransitBytesOut:         info.TransitBytesOut,
		Reconnections:           info.Reconnections,
		FragmentsSent:           info.FragmentsSent,
		FragmentMessagesSent:    info.FragmentMessagesSent,
		FragmentsReceived:       info.FragmentsReceived,
		FragmentMessagesRecv:    info.FragmentMessagesRecv,
		FragmentTimeouts:        info.FragmentTimeouts,
		TracedSent:              info.TracedSent,
		TracedReceived:          info.TracedReceived,
		CompressedSent:          info.CompressedSent,
		CompressedBytesSent:     info.CompressedBytesSent,
		CompressedOrigBytesSent: info.CompressedOrigBytesSent,
		DecompressedRecv:        info.DecompressedRecv,
		DecompressedBytesRecv:   info.DecompressedBytesRecv,
		DecompressedOrigRecv:    info.DecompressedOrigRecv,
		ClockSkew:               info.ClockSkew,
	}
}

func wireMCPConnections(list []gen.RemoteNodeInfo) []wireMCPRemoteNodeInfo {
	if list == nil {
		return nil
	}
	out := make([]wireMCPRemoteNodeInfo, len(list))
	for i, info := range list {
		out[i] = wireMCPRemoteNodeInfoOf(info)
	}
	return out
}

type wireMCPConnection struct {
	Node         gen.Atom
	Disconnected bool
	Info         wireMCPRemoteNodeInfo

	Legend map[string]any `json:"services.ergo/legend,omitempty"`
}

type wireMCPGetConnection struct {
	Node  gen.Atom
	Info  wireMCPRemoteNodeInfo
	Error string `json:"Error,omitempty"`

	Legend map[string]any `json:"services.ergo/legend,omitempty"`
}

type wireMCPConnectionList struct {
	Node        gen.Atom
	Connections []wireMCPRemoteNodeInfo

	Legend map[string]any `json:"services.ergo/legend,omitempty"`
}

type wireMCPGetConnectionList struct {
	Node        gen.Atom
	Connections []wireMCPRemoteNodeInfo
	Error       string `json:"Error,omitempty"`

	Legend map[string]any `json:"services.ergo/legend,omitempty"`
}

var (
	wireMCPConnectionLegend        = mcpLegendFor(wireMCPConnection{})
	wireMCPGetConnectionLegend     = mcpLegendFor(wireMCPGetConnection{})
	wireMCPConnectionListLegend    = mcpLegendFor(wireMCPConnectionList{})
	wireMCPGetConnectionListLegend = mcpLegendFor(wireMCPGetConnectionList{})
)

func init() {
	mcpRegisterView(inspect.MessageInspectConnection{}, func(value any) any {
		m, ok := value.(inspect.MessageInspectConnection)
		if ok == false {
			return value
		}
		return wireMCPConnection{
			Node: m.Node, Disconnected: m.Disconnected,
			Info: wireMCPRemoteNodeInfoOf(m.Info), Legend: wireMCPConnectionLegend,
		}
	})

	mcpRegisterView(inspect.ResponseGetConnection{}, func(value any) any {
		r, ok := value.(inspect.ResponseGetConnection)
		if ok == false {
			return value
		}
		return wireMCPGetConnection{
			Node: r.Node, Info: wireMCPRemoteNodeInfoOf(r.Info), Error: mcpErrorText(r.Error),
			Legend: wireMCPGetConnectionLegend,
		}
	})

	mcpRegisterView(inspect.MessageInspectConnectionList{}, func(value any) any {
		m, ok := value.(inspect.MessageInspectConnectionList)
		if ok == false {
			return value
		}
		return wireMCPConnectionList{
			Node: m.Node, Connections: wireMCPConnections(m.Connections),
			Legend: wireMCPConnectionListLegend,
		}
	})

	mcpRegisterView(inspect.ResponseGetConnectionList{}, func(value any) any {
		r, ok := value.(inspect.ResponseGetConnectionList)
		if ok == false {
			return value
		}
		return wireMCPGetConnectionList{
			Node: r.Node, Connections: wireMCPConnections(r.Connections),
			Error:  mcpErrorText(r.Error),
			Legend: wireMCPGetConnectionListLegend,
		}
	})
}
