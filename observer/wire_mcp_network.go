package observer

import (
	"ergo.services/ergo/app/system/inspect"
	"ergo.services/ergo/gen"
)

type wireMCPNetworkFlags struct {
	Enable                       bool
	EnableRemoteSpawn            bool
	EnableRemoteApplicationStart bool
	EnableFragmentation          bool
	EnableProxyTransit           bool
	EnableProxyAccept            bool
	EnableImportantDelivery      bool
	EnableSimultaneousConnect    bool
	EnableClockSkew              bool
	EnableTracing                bool
	EnableSoftwareKeepAlive      int `unit:"sec" sentinel:"0 = keepalive is disabled"`
	EnableWrappedErrors          bool
	EnableSchemaEvolution        bool
}

func wireMCPNetworkFlagsOf(flags gen.NetworkFlags) wireMCPNetworkFlags {
	return wireMCPNetworkFlags{
		Enable:                       flags.Enable,
		EnableRemoteSpawn:            flags.EnableRemoteSpawn,
		EnableRemoteApplicationStart: flags.EnableRemoteApplicationStart,
		EnableFragmentation:          flags.EnableFragmentation,
		EnableProxyTransit:           flags.EnableProxyTransit,
		EnableProxyAccept:            flags.EnableProxyAccept,
		EnableImportantDelivery:      flags.EnableImportantDelivery,
		EnableSimultaneousConnect:    flags.EnableSimultaneousConnect,
		EnableClockSkew:              flags.EnableClockSkew,
		EnableTracing:                flags.EnableTracing,
		EnableSoftwareKeepAlive:      flags.EnableSoftwareKeepAlive,
		EnableWrappedErrors:          flags.EnableWrappedErrors,
		EnableSchemaEvolution:        flags.EnableSchemaEvolution,
	}
}

type wireMCPAcceptorInfo struct {
	Interface        string
	MaxMessageSize   int `unit:"bytes"`
	Flags            wireMCPNetworkFlags
	TLS              bool
	CustomRegistrar  bool
	RegistrarServer  string
	RegistrarVersion gen.Version
	HandshakeVersion gen.Version
	ProtoVersion     gen.Version
	HandshakeErrors  uint64
}

func wireMCPAcceptors(list []gen.AcceptorInfo) []wireMCPAcceptorInfo {
	if list == nil {
		return nil
	}
	out := make([]wireMCPAcceptorInfo, len(list))
	for i, info := range list {
		out[i] = wireMCPAcceptorInfo{
			Interface:        info.Interface,
			MaxMessageSize:   info.MaxMessageSize,
			Flags:            wireMCPNetworkFlagsOf(info.Flags),
			TLS:              info.TLS,
			CustomRegistrar:  info.CustomRegistrar,
			RegistrarServer:  info.RegistrarServer,
			RegistrarVersion: info.RegistrarVersion,
			HandshakeVersion: info.HandshakeVersion,
			ProtoVersion:     info.ProtoVersion,
			HandshakeErrors:  info.HandshakeErrors,
		}
	}
	return out
}

type wireMCPRouteInfo struct {
	Match            string
	Weight           int
	UseResolver      bool
	UseCustomCookie  bool
	UseCustomCert    bool
	Flags            wireMCPNetworkFlags
	HandshakeVersion gen.Version
	ProtoVersion     gen.Version
	Host             string
	Port             uint16
}

func wireMCPRoutes(list []gen.RouteInfo) []wireMCPRouteInfo {
	if list == nil {
		return nil
	}
	out := make([]wireMCPRouteInfo, len(list))
	for i, info := range list {
		out[i] = wireMCPRouteInfo{
			Match:            info.Match,
			Weight:           info.Weight,
			UseResolver:      info.UseResolver,
			UseCustomCookie:  info.UseCustomCookie,
			UseCustomCert:    info.UseCustomCert,
			Flags:            wireMCPNetworkFlagsOf(info.Flags),
			HandshakeVersion: info.HandshakeVersion,
			ProtoVersion:     info.ProtoVersion,
			Host:             info.Host,
			Port:             info.Port,
		}
	}
	return out
}

type wireMCPNetworkInfo struct {
	Mode                    string
	Registrar               gen.RegistrarInfo
	Acceptors               []wireMCPAcceptorInfo
	MaxMessageSize          int `unit:"bytes" sentinel:"0 = unlimited"`
	HandshakeVersion        gen.Version
	ProtoVersion            gen.Version
	Nodes                   []gen.Atom
	Routes                  []wireMCPRouteInfo
	ProxyRoutes             []gen.ProxyRouteInfo
	Flags                   wireMCPNetworkFlags
	ConnectionsEstablished  uint64
	ConnectionsLost         uint64
	EnabledSpawn            []gen.NetworkSpawnInfo
	EnabledApplicationStart []gen.NetworkApplicationStartInfo

	Legend map[string]any `json:"services.ergo/legend,omitempty"`
}

var wireMCPNetworkLegend = mcpLegendFor(wireMCPNetworkInfo{})

func wireMCPNetworkInfoOf(info gen.NetworkInfo) wireMCPNetworkInfo {
	return wireMCPNetworkInfo{
		Mode:                    info.Mode.String(),
		Registrar:               info.Registrar,
		Acceptors:               wireMCPAcceptors(info.Acceptors),
		MaxMessageSize:          info.MaxMessageSize,
		HandshakeVersion:        info.HandshakeVersion,
		ProtoVersion:            info.ProtoVersion,
		Nodes:                   info.Nodes,
		Routes:                  wireMCPRoutes(info.Routes),
		ProxyRoutes:             info.ProxyRoutes,
		Flags:                   wireMCPNetworkFlagsOf(info.Flags),
		ConnectionsEstablished:  info.ConnectionsEstablished,
		ConnectionsLost:         info.ConnectionsLost,
		EnabledSpawn:            info.EnabledSpawn,
		EnabledApplicationStart: info.EnabledApplicationStart,
		Legend:                  wireMCPNetworkLegend,
	}
}

type wireMCPNetwork struct {
	Node    gen.Atom
	Stopped bool
	Info    wireMCPNetworkInfo
}

type wireMCPGetNetwork struct {
	Node  gen.Atom
	Info  wireMCPNetworkInfo
	Error string `json:"Error,omitempty"`
}

func init() {
	mcpRegisterView(inspect.MessageInspectNetwork{}, func(value any) any {
		m, ok := value.(inspect.MessageInspectNetwork)
		if ok == false {
			return value
		}
		return wireMCPNetwork{
			Node: m.Node, Stopped: m.Stopped, Info: wireMCPNetworkInfoOf(m.Info),
		}
	})

	mcpRegisterView(inspect.ResponseGetNetwork{}, func(value any) any {
		r, ok := value.(inspect.ResponseGetNetwork)
		if ok == false {
			return value
		}
		return wireMCPGetNetwork{
			Node: r.Node, Info: wireMCPNetworkInfoOf(r.Info), Error: mcpErrorText(r.Error),
		}
	})
}
