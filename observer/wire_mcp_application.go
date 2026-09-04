package observer

import (
	"ergo.services/ergo/app/system/inspect"
	"ergo.services/ergo/gen"
)

type wireMCPApplicationInfo struct {
	Name        gen.Atom
	Weight      int
	Tags        []gen.Atom
	Map         map[string]gen.Atom
	Description string
	Version     gen.Version
	Env         map[gen.Env]any `sentinel:"empty unless NodeOptions.Security.ExposeEnvInfo is enabled"`
	Depends     gen.ApplicationDepends
	Mode        string
	State       string
	Parent      gen.Atom
	Uptime      int64 `unit:"sec"`
	Group       []string

	ProcessesTotal int `sentinel:"0 = the application is fully stopped"`
}

func wireMCPApplicationInfoOf(info gen.ApplicationInfo) wireMCPApplicationInfo {
	return wireMCPApplicationInfo{
		Name:        info.Name,
		Weight:      info.Weight,
		Tags:        info.Tags,
		Map:         info.Map,
		Description: info.Description,
		Version:     info.Version,
		Env:         info.Env,
		Depends:     info.Depends,
		Mode:        info.Mode.String(),
		State:       info.State.String(),
		Parent:      info.Parent,
		Uptime:      info.Uptime,
		Group:       mcpPIDTexts(info.Group),

		ProcessesTotal: info.ProcessesTotal,
	}
}

func wireMCPApplications(list map[gen.Atom]gen.ApplicationInfo) map[gen.Atom]wireMCPApplicationInfo {
	if list == nil {
		return nil
	}
	out := make(map[gen.Atom]wireMCPApplicationInfo, len(list))
	for name, info := range list {
		out[name] = wireMCPApplicationInfoOf(info)
	}
	return out
}

type wireMCPApplicationList struct {
	Node         gen.Atom
	Applications map[gen.Atom]wireMCPApplicationInfo

	Legend map[string]any `json:"services.ergo/legend,omitempty"`
}

type wireMCPGetApplicationList struct {
	Node         gen.Atom
	Applications map[gen.Atom]wireMCPApplicationInfo
	Error        string `json:"Error,omitempty"`

	Legend map[string]any `json:"services.ergo/legend,omitempty"`
}

var (
	wireMCPApplicationListLegend    = mcpLegendFor(wireMCPApplicationList{})
	wireMCPGetApplicationListLegend = mcpLegendFor(wireMCPGetApplicationList{})
)

func init() {
	mcpRegisterView(inspect.MessageInspectApplicationList{}, func(value any) any {
		m, ok := value.(inspect.MessageInspectApplicationList)
		if ok == false {
			return value
		}
		return wireMCPApplicationList{
			Node: m.Node, Applications: wireMCPApplications(m.Applications),
			Legend: wireMCPApplicationListLegend,
		}
	})

	mcpRegisterView(inspect.ResponseGetApplicationList{}, func(value any) any {
		r, ok := value.(inspect.ResponseGetApplicationList)
		if ok == false {
			return value
		}
		return wireMCPGetApplicationList{
			Node: r.Node, Applications: wireMCPApplications(r.Applications),
			Error: mcpErrorText(r.Error), Legend: wireMCPGetApplicationListLegend,
		}
	})
}
