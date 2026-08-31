package observer

import (
	"ergo.services/ergo/app/system/inspect"
	"ergo.services/ergo/gen"
)

type wireMCPGetCapabilities struct {
	Node         gen.Atom
	Creation     int64
	Version      gen.Version
	Framework    gen.Version
	Manage       bool
	Capabilities []string
	Build        []string
}

func init() {
	mcpRegisterView(inspect.ResponseGetCapabilities{}, func(value any) any {
		r, ok := value.(inspect.ResponseGetCapabilities)
		if ok == false {
			return value
		}
		return wireMCPGetCapabilities{
			Node:         r.Node,
			Creation:     r.Creation,
			Version:      r.Version,
			Framework:    r.Framework,
			Manage:       r.Manage,
			Capabilities: r.Capabilities,
			Build:        r.Build,
		}
	})
}
