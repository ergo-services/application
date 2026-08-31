package observer

import (
	"ergo.services/ergo/app/system/inspect"
	"ergo.services/ergo/gen"
)

type wireApplicationDepends struct {
	Applications []string `json:"a,omitempty"`
	Network      bool     `json:"nw,omitempty"`
}

type wireApplication struct {
	Name        string                 `json:"n"`
	Weight      int                    `json:"w,omitempty"`
	Tags        []string               `json:"tg,omitempty"`
	Map         map[string]string      `json:"mp,omitempty"`
	Description string                 `json:"d,omitempty"`
	Version     wireVersion            `json:"v,omitempty"`
	Env         map[string]any         `json:"e,omitempty"`
	Depends     wireApplicationDepends `json:"dp,omitempty"`
	Mode        string                 `json:"m,omitempty"`
	State       string                 `json:"s,omitempty"`
	Parent      string                 `json:"p,omitempty"`
	Uptime      int64                  `json:"u,omitempty"`
	Group       []wirePID              `json:"g,omitempty"`

	ProcessesTotal int `json:"pc"`
}

type wireApplicationList struct {
	Node         string                     `json:"nd"`
	Applications map[string]wireApplication `json:"ap"`
}

// the keys are gen.Env and the values are whatever the application put there
func wireEnv(env map[gen.Env]any) map[string]any {
	if len(env) == 0 {
		return nil
	}
	out := make(map[string]any, len(env))
	for key, value := range env {
		out[string(key)] = value
	}
	return out
}

func wireApplicationFrom(info gen.ApplicationInfo) wireApplication {
	tags := make([]string, 0, len(info.Tags))
	for _, tag := range info.Tags {
		tags = append(tags, string(tag))
	}

	var roles map[string]string
	if len(info.Map) > 0 {
		roles = make(map[string]string, len(info.Map))
		for role, name := range info.Map {
			roles[role] = string(name)
		}
	}

	depends := make([]string, 0, len(info.Depends.Applications))
	for _, name := range info.Depends.Applications {
		depends = append(depends, string(name))
	}

	return wireApplication{
		Name:        string(info.Name),
		Weight:      info.Weight,
		Tags:        tags,
		Map:         roles,
		Description: info.Description,
		Version:     wireVersionFrom(info.Version),
		Env:         wireEnv(info.Env),
		Depends:     wireApplicationDepends{Applications: depends, Network: info.Depends.Network},
		Mode:        info.Mode.String(),
		State:       info.State.String(),
		Parent:      string(info.Parent),
		Uptime:      info.Uptime,
		Group:       wirePIDsFrom(info.Group),

		ProcessesTotal: info.ProcessesTotal,
	}
}

func wireApplicationListFrom(m inspect.MessageInspectApplicationList) wireApplicationList {
	out := wireApplicationList{
		Node:         string(m.Node),
		Applications: make(map[string]wireApplication, len(m.Applications)),
	}
	for name, info := range m.Applications {
		out.Applications[string(name)] = wireApplicationFrom(info)
	}
	return out
}
