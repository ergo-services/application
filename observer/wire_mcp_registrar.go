package observer

import (
	"time"

	"ergo.services/ergo/app/system/inspect"
	"ergo.services/ergo/gen"
)

type wireMCPApplicationRoute struct {
	Node    gen.Atom
	Name    gen.Atom
	Weight  int
	Mode    string
	Tags    []gen.Atom
	State   string
	Version gen.Version
}

type wireMCPGetRegistrarApplicationRoutes struct {
	Routes []wireMCPApplicationRoute
	Error  string `json:"Error,omitempty"`
}

type wireMCPGetRegistrarNodes struct {
	Nodes []gen.Atom
	Error string `json:"Error,omitempty"`
}

type wireMCPGetRegistrarRoutes struct {
	Routes []gen.Route
	Error  string `json:"Error,omitempty"`
}

type wireMCPGetRegistrarProxyRoutes struct {
	Routes []gen.ProxyRoute
	Error  string `json:"Error,omitempty"`
}

type wireMCPGetCronInfo struct {
	Next  time.Time
	Spool []gen.Atom
	Jobs  []gen.CronJobInfo
	Error string `json:"Error,omitempty"`
}

type wireMCPGetCronSchedule struct {
	Schedule  []gen.CronSchedule
	Truncated bool
	Error     string `json:"Error,omitempty"`
}

func init() {
	mcpRegisterView(inspect.ResponseGetRegistrarApplicationRoutes{}, func(value any) any {
		r, ok := value.(inspect.ResponseGetRegistrarApplicationRoutes)
		if ok == false {
			return value
		}

		out := wireMCPGetRegistrarApplicationRoutes{Error: mcpErrorText(r.Error)}
		if r.Routes != nil {
			out.Routes = make([]wireMCPApplicationRoute, len(r.Routes))
			for i, route := range r.Routes {
				out.Routes[i] = wireMCPApplicationRoute{
					Node:    route.Node,
					Name:    route.Name,
					Weight:  route.Weight,
					Mode:    route.Mode.String(),
					Tags:    route.Tags,
					State:   route.State.String(),
					Version: route.Version,
				}
			}
		}
		return out
	})

	mcpRegisterView(inspect.ResponseGetRegistrarNodes{}, func(value any) any {
		r, ok := value.(inspect.ResponseGetRegistrarNodes)
		if ok == false {
			return value
		}
		return wireMCPGetRegistrarNodes{Nodes: r.Nodes, Error: mcpErrorText(r.Error)}
	})

	mcpRegisterView(inspect.ResponseGetRegistrarRoutes{}, func(value any) any {
		r, ok := value.(inspect.ResponseGetRegistrarRoutes)
		if ok == false {
			return value
		}
		return wireMCPGetRegistrarRoutes{Routes: r.Routes, Error: mcpErrorText(r.Error)}
	})

	mcpRegisterView(inspect.ResponseGetRegistrarProxyRoutes{}, func(value any) any {
		r, ok := value.(inspect.ResponseGetRegistrarProxyRoutes)
		if ok == false {
			return value
		}
		return wireMCPGetRegistrarProxyRoutes{Routes: r.Routes, Error: mcpErrorText(r.Error)}
	})

	mcpRegisterView(inspect.ResponseGetCronInfo{}, func(value any) any {
		r, ok := value.(inspect.ResponseGetCronInfo)
		if ok == false {
			return value
		}
		return wireMCPGetCronInfo{
			Next: r.Next, Spool: r.Spool, Jobs: r.Jobs, Error: mcpErrorText(r.Error),
		}
	})

	mcpRegisterView(inspect.ResponseGetCronSchedule{}, func(value any) any {
		r, ok := value.(inspect.ResponseGetCronSchedule)
		if ok == false {
			return value
		}
		return wireMCPGetCronSchedule{
			Schedule: r.Schedule, Truncated: r.Truncated, Error: mcpErrorText(r.Error),
		}
	})
}
