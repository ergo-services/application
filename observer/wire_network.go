package observer

import (
	"ergo.services/ergo/app/system/inspect"
	"ergo.services/ergo/gen"
)

type wireVersion struct {
	Name    string `json:"n,omitempty"`
	Release string `json:"r,omitempty"`
	Commit  string `json:"c,omitempty"`
	License string `json:"l,omitempty"`
}

func wireVersionFrom(v gen.Version) wireVersion {
	return wireVersion{Name: v.Name, Release: v.Release, Commit: v.Commit, License: v.License}
}

type wireCapabilities struct {
	Node         string      `json:"nd"`
	Creation     int64       `json:"c"`
	Version      wireVersion `json:"v,omitempty"`
	Framework    wireVersion `json:"fw,omitempty"`
	Manage       bool        `json:"mng,omitempty"`
	Capabilities []string    `json:"cap,omitempty"`
	Build        []string    `json:"bld,omitempty"`
}

func wireCapabilitiesFrom(r inspect.ResponseGetCapabilities) wireCapabilities {
	return wireCapabilities{
		Node:         string(r.Node),
		Creation:     r.Creation,
		Version:      wireVersionFrom(r.Version),
		Framework:    wireVersionFrom(r.Framework),
		Manage:       r.Manage,
		Capabilities: r.Capabilities,
		Build:        r.Build,
	}
}

func wireCapabilitiesUnder(r inspect.ResponseGetCapabilities, c Ceiling) wireCapabilities {
	return wireCapabilitiesFrom(capabilitiesUnder(r, c))
}

type wireNetworkFlags struct {
	Enable                       bool `json:"e,omitempty"`
	EnableRemoteSpawn            bool `json:"rs,omitempty"`
	EnableRemoteApplicationStart bool `json:"ras,omitempty"`
	EnableFragmentation          bool `json:"f,omitempty"`
	EnableProxyTransit           bool `json:"pt,omitempty"`
	EnableProxyAccept            bool `json:"pa,omitempty"`
	EnableImportantDelivery      bool `json:"id,omitempty"`
	EnableSimultaneousConnect    bool `json:"sc,omitempty"`
	EnableClockSkew              bool `json:"cs,omitempty"`
	EnableTracing                bool `json:"t,omitempty"`
	EnableSoftwareKeepAlive      int  `json:"ka,omitempty"`
	EnableWrappedErrors          bool `json:"we,omitempty"`
	EnableSchemaEvolution        bool `json:"se,omitempty"`
}

func wireNetworkFlagsFrom(f gen.NetworkFlags) wireNetworkFlags {
	return wireNetworkFlags{
		Enable:                       f.Enable,
		EnableRemoteSpawn:            f.EnableRemoteSpawn,
		EnableRemoteApplicationStart: f.EnableRemoteApplicationStart,
		EnableFragmentation:          f.EnableFragmentation,
		EnableProxyTransit:           f.EnableProxyTransit,
		EnableProxyAccept:            f.EnableProxyAccept,
		EnableImportantDelivery:      f.EnableImportantDelivery,
		EnableSimultaneousConnect:    f.EnableSimultaneousConnect,
		EnableClockSkew:              f.EnableClockSkew,
		EnableTracing:                f.EnableTracing,
		EnableSoftwareKeepAlive:      f.EnableSoftwareKeepAlive,
		EnableWrappedErrors:          f.EnableWrappedErrors,
		EnableSchemaEvolution:        f.EnableSchemaEvolution,
	}
}

type wireConnection struct {
	Node                    string           `json:"nd"`
	Uptime                  int64            `json:"u,omitempty"`
	ConnectionUptime        int64            `json:"cu,omitempty"`
	Version                 wireVersion      `json:"v,omitempty"`
	HandshakeVersion        wireVersion      `json:"hv,omitempty"`
	ProtoVersion            wireVersion      `json:"pv,omitempty"`
	NetworkFlags            wireNetworkFlags `json:"fl,omitempty"`
	PoolSize                int              `json:"psz,omitempty"`
	PoolLen                 int              `json:"pln,omitempty"`
	PoolDSN                 []string         `json:"pdsn,omitempty"`
	MaxMessageSize          int              `json:"mms,omitempty"`
	TLS                     bool             `json:"tls,omitempty"`
	MessagesIn              uint64           `json:"mi,omitempty"`
	MessagesOut             uint64           `json:"mo,omitempty"`
	BytesIn                 uint64           `json:"bi,omitempty"`
	BytesOut                uint64           `json:"bo,omitempty"`
	TransitBytesIn          uint64           `json:"tbi,omitempty"`
	TransitBytesOut         uint64           `json:"tbo,omitempty"`
	Reconnections           uint64           `json:"rc,omitempty"`
	FragmentsSent           uint64           `json:"fs,omitempty"`
	FragmentMessagesSent    uint64           `json:"fms,omitempty"`
	FragmentsReceived       uint64           `json:"fr,omitempty"`
	FragmentMessagesRecv    uint64           `json:"fmr,omitempty"`
	FragmentTimeouts        uint64           `json:"ft,omitempty"`
	TracedSent              uint64           `json:"ts,omitempty"`
	TracedReceived          uint64           `json:"tr,omitempty"`
	CompressedSent          uint64           `json:"cs,omitempty"`
	CompressedBytesSent     uint64           `json:"cbs,omitempty"`
	CompressedOrigBytesSent uint64           `json:"cobs,omitempty"`
	DecompressedRecv        uint64           `json:"dr,omitempty"`
	DecompressedBytesRecv   uint64           `json:"dbr,omitempty"`
	DecompressedOrigRecv    uint64           `json:"dor,omitempty"`
	ClockSkew               int64            `json:"sk,omitempty"`
}

func wireConnectionFrom(info gen.RemoteNodeInfo) wireConnection {
	return wireConnection{
		Node:                    string(info.Node),
		Uptime:                  info.Uptime,
		ConnectionUptime:        info.ConnectionUptime,
		Version:                 wireVersionFrom(info.Version),
		HandshakeVersion:        wireVersionFrom(info.HandshakeVersion),
		ProtoVersion:            wireVersionFrom(info.ProtoVersion),
		NetworkFlags:            wireNetworkFlagsFrom(info.NetworkFlags),
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

type wireConnectionList struct {
	Node        string           `json:"nd"`
	Connections []wireConnection `json:"cs"`
}

func wireConnectionListFrom(m inspect.MessageInspectConnectionList) wireConnectionList {
	out := wireConnectionList{Node: string(m.Node), Connections: make([]wireConnection, 0, len(m.Connections))}
	for _, info := range m.Connections {
		out.Connections = append(out.Connections, wireConnectionFrom(info))
	}
	return out
}

type wireConnectionInfo struct {
	Node         string         `json:"nd"`
	Disconnected bool           `json:"dc,omitempty"`
	Info         wireConnection `json:"in"`
}

func wireConnectionInfoFrom(m inspect.MessageInspectConnection) wireConnectionInfo {
	return wireConnectionInfo{
		Node:         string(m.Node),
		Disconnected: m.Disconnected,
		Info:         wireConnectionFrom(m.Info),
	}
}

type wireRegistrar struct {
	Server                     string      `json:"s,omitempty"`
	EmbeddedServer             bool        `json:"es,omitempty"`
	SupportRegisterProxy       bool        `json:"srp,omitempty"`
	SupportRegisterApplication bool        `json:"sra,omitempty"`
	SupportConfig              bool        `json:"sc,omitempty"`
	SupportEvent               bool        `json:"se,omitempty"`
	Version                    wireVersion `json:"v,omitempty"`
}

type wireAcceptor struct {
	Interface        string           `json:"i,omitempty"`
	MaxMessageSize   int              `json:"mms,omitempty"`
	Flags            wireNetworkFlags `json:"fl,omitempty"`
	TLS              bool             `json:"tls,omitempty"`
	CustomRegistrar  bool             `json:"cr,omitempty"`
	RegistrarServer  string           `json:"rs,omitempty"`
	RegistrarVersion wireVersion      `json:"rv,omitempty"`
	HandshakeVersion wireVersion      `json:"hv,omitempty"`
	ProtoVersion     wireVersion      `json:"pv,omitempty"`
	HandshakeErrors  uint64           `json:"he,omitempty"`
}

type wireRoute struct {
	Match            string           `json:"m,omitempty"`
	Weight           int              `json:"w,omitempty"`
	UseResolver      bool             `json:"ur,omitempty"`
	UseCustomCookie  bool             `json:"uck,omitempty"`
	UseCustomCert    bool             `json:"ucr,omitempty"`
	Flags            wireNetworkFlags `json:"fl,omitempty"`
	HandshakeVersion wireVersion      `json:"hv,omitempty"`
	ProtoVersion     wireVersion      `json:"pv,omitempty"`
	Host             string           `json:"h,omitempty"`
	Port             uint16           `json:"p,omitempty"`
}

type wireProxyFlags struct {
	Enable                       bool `json:"e,omitempty"`
	EnableRemoteSpawn            bool `json:"rs,omitempty"`
	EnableRemoteApplicationStart bool `json:"ras,omitempty"`
	EnableEncryption             bool `json:"en,omitempty"`
	EnableImportantDelivery      bool `json:"id,omitempty"`
}

type wireProxyRoute struct {
	Match           string         `json:"m,omitempty"`
	Weight          int            `json:"w,omitempty"`
	UseResolver     bool           `json:"ur,omitempty"`
	UseCustomCookie bool           `json:"uck,omitempty"`
	Flags           wireProxyFlags `json:"fl,omitempty"`
	MaxHop          int            `json:"mh,omitempty"`
	Proxy           string         `json:"px,omitempty"`
}

type wireEnabledSpawn struct {
	Name     string   `json:"n"`
	Behavior string   `json:"b,omitempty"`
	Nodes    []string `json:"nds,omitempty"`
}

type wireEnabledApplicationStart struct {
	Name  string   `json:"n"`
	Nodes []string `json:"nds,omitempty"`
}

type wireNetwork struct {
	Mode                    string                        `json:"m,omitempty"`
	Registrar               wireRegistrar                 `json:"rg,omitempty"`
	Acceptors               []wireAcceptor                `json:"ac,omitempty"`
	MaxMessageSize          int                           `json:"mms,omitempty"`
	HandshakeVersion        wireVersion                   `json:"hv,omitempty"`
	ProtoVersion            wireVersion                   `json:"pv,omitempty"`
	Nodes                   []string                      `json:"nds,omitempty"`
	Routes                  []wireRoute                   `json:"rt,omitempty"`
	ProxyRoutes             []wireProxyRoute              `json:"prt,omitempty"`
	Flags                   wireNetworkFlags              `json:"fl,omitempty"`
	ConnectionsEstablished  uint64                        `json:"ce,omitempty"`
	ConnectionsLost         uint64                        `json:"cl,omitempty"`
	EnabledSpawn            []wireEnabledSpawn            `json:"es,omitempty"`
	EnabledApplicationStart []wireEnabledApplicationStart `json:"eas,omitempty"`
}

type wireNetworkInfo struct {
	Node    string      `json:"nd"`
	Stopped bool        `json:"sp,omitempty"`
	Info    wireNetwork `json:"in"`
}

func wireAtoms(list []gen.Atom) []string {
	out := make([]string, 0, len(list))
	for _, item := range list {
		out = append(out, string(item))
	}
	return out
}

func wireNetworkFrom(info gen.NetworkInfo) wireNetwork {
	acceptors := make([]wireAcceptor, 0, len(info.Acceptors))
	for _, a := range info.Acceptors {
		acceptors = append(acceptors, wireAcceptor{
			Interface:        a.Interface,
			MaxMessageSize:   a.MaxMessageSize,
			Flags:            wireNetworkFlagsFrom(a.Flags),
			TLS:              a.TLS,
			CustomRegistrar:  a.CustomRegistrar,
			RegistrarServer:  a.RegistrarServer,
			RegistrarVersion: wireVersionFrom(a.RegistrarVersion),
			HandshakeVersion: wireVersionFrom(a.HandshakeVersion),
			ProtoVersion:     wireVersionFrom(a.ProtoVersion),
			HandshakeErrors:  a.HandshakeErrors,
		})
	}

	routes := make([]wireRoute, 0, len(info.Routes))
	for _, r := range info.Routes {
		routes = append(routes, wireRoute{
			Match:            r.Match,
			Weight:           r.Weight,
			UseResolver:      r.UseResolver,
			UseCustomCookie:  r.UseCustomCookie,
			UseCustomCert:    r.UseCustomCert,
			Flags:            wireNetworkFlagsFrom(r.Flags),
			HandshakeVersion: wireVersionFrom(r.HandshakeVersion),
			ProtoVersion:     wireVersionFrom(r.ProtoVersion),
			Host:             r.Host,
			Port:             r.Port,
		})
	}

	proxyRoutes := make([]wireProxyRoute, 0, len(info.ProxyRoutes))
	for _, r := range info.ProxyRoutes {
		proxyRoutes = append(proxyRoutes, wireProxyRoute{
			Match:           r.Match,
			Weight:          r.Weight,
			UseResolver:     r.UseResolver,
			UseCustomCookie: r.UseCustomCookie,
			Flags: wireProxyFlags{
				Enable:                       r.Flags.Enable,
				EnableRemoteSpawn:            r.Flags.EnableRemoteSpawn,
				EnableRemoteApplicationStart: r.Flags.EnableRemoteApplicationStart,
				EnableEncryption:             r.Flags.EnableEncryption,
				EnableImportantDelivery:      r.Flags.EnableImportantDelivery,
			},
			MaxHop: r.MaxHop,
			Proxy:  string(r.Proxy),
		})
	}

	spawn := make([]wireEnabledSpawn, 0, len(info.EnabledSpawn))
	for _, s := range info.EnabledSpawn {
		spawn = append(spawn, wireEnabledSpawn{Name: string(s.Name), Behavior: s.Behavior, Nodes: wireAtoms(s.Nodes)})
	}

	appStart := make([]wireEnabledApplicationStart, 0, len(info.EnabledApplicationStart))
	for _, a := range info.EnabledApplicationStart {
		appStart = append(appStart, wireEnabledApplicationStart{Name: string(a.Name), Nodes: wireAtoms(a.Nodes)})
	}

	return wireNetwork{
		Mode: info.Mode.String(),
		Registrar: wireRegistrar{
			Server:                     info.Registrar.Server,
			EmbeddedServer:             info.Registrar.EmbeddedServer,
			SupportRegisterProxy:       info.Registrar.SupportRegisterProxy,
			SupportRegisterApplication: info.Registrar.SupportRegisterApplication,
			SupportConfig:              info.Registrar.SupportConfig,
			SupportEvent:               info.Registrar.SupportEvent,
			Version:                    wireVersionFrom(info.Registrar.Version),
		},
		Acceptors:               acceptors,
		MaxMessageSize:          info.MaxMessageSize,
		HandshakeVersion:        wireVersionFrom(info.HandshakeVersion),
		ProtoVersion:            wireVersionFrom(info.ProtoVersion),
		Nodes:                   wireAtoms(info.Nodes),
		Routes:                  routes,
		ProxyRoutes:             proxyRoutes,
		Flags:                   wireNetworkFlagsFrom(info.Flags),
		ConnectionsEstablished:  info.ConnectionsEstablished,
		ConnectionsLost:         info.ConnectionsLost,
		EnabledSpawn:            spawn,
		EnabledApplicationStart: appStart,
	}
}

func wireNetworkInfoFrom(m inspect.MessageInspectNetwork) wireNetworkInfo {
	return wireNetworkInfo{Node: string(m.Node), Stopped: m.Stopped, Info: wireNetworkFrom(m.Info)}
}
