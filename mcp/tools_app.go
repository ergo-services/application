package mcp

import (
	"encoding/json"
	"fmt"
	"strings"

	"ergo.services/ergo/gen"
)

func registerAppTools(r *toolRegistry) {
	r.register(ToolDefinition{
		Name:        "app_list",
		Description: "Returns a list of applications with info (name, state, mode, version, description, uptime, weight, tags, process count). Supports filtering.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"state": {
					"type": "string",
					"description": "Filter by state: loaded, running, stopped"
				},
				"mode": {
					"type": "string",
					"description": "Filter by mode: permanent, transient, temporary"
				},
				"name": {
					"type": "string",
					"description": "Filter by name (substring match)"
				},
				"min_uptime": {
					"type": "integer",
					"description": "Minimum uptime in seconds"
				}
			}
		}`),
		handler: toolAppList,
	})

	r.register(ToolDefinition{
		Name:        "app_info",
		Description: "Returns detailed information about a specific application: state, mode, version, description, dependencies, environment, group processes.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"name": {
					"type": "string",
					"description": "Application name"
				}
			},
			"required": ["name"]
		}`),
		handler: toolAppInfo,
	})

	r.register(ToolDefinition{
		Name:        "app_processes",
		Description: "Returns processes belonging to an application with short info.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"name": {
					"type": "string",
					"description": "Application name"
				},
				"limit": {
					"type": "integer",
					"description": "Maximum number of processes to return (default: 100)"
				}
			},
			"required": ["name"]
		}`),
		handler: toolAppProcesses,
	})
}

type appListParams struct {
	State    string `json:"state"`
	Mode     string `json:"mode"`
	Name     string `json:"name"`
	MinUptime int64 `json:"min_uptime"`
}

func toolAppList(w gen.Process, params json.RawMessage) (any, error) {
	var p appListParams
	if len(params) > 0 {
		json.Unmarshal(params, &p)
	}

	apps := w.Node().Applications()

	type appSummary struct {
		Name        gen.Atom   `json:"name"`
		State       string     `json:"state"`
		Mode        string     `json:"mode"`
		Description string     `json:"description,omitempty"`
		Version     string     `json:"version,omitempty"`
		Uptime      int64      `json:"uptime"`
		Weight      int        `json:"weight,omitempty"`
		Tags        []gen.Atom `json:"tags,omitempty"`
		Processes   int        `json:"processes"`
	}

	result := make([]appSummary, 0, len(apps))
	for _, name := range apps {
		info, err := w.Node().ApplicationInfo(name)
		if err != nil {
			continue
		}

		// filters
		if p.State != "" && strings.EqualFold(info.State.String(), p.State) == false {
			continue
		}
		if p.Mode != "" && strings.EqualFold(info.Mode.String(), p.Mode) == false {
			continue
		}
		if p.Name != "" && strings.Contains(string(name), p.Name) == false {
			continue
		}
		if p.MinUptime > 0 && info.Uptime < p.MinUptime {
			continue
		}

		ver := ""
		if info.Version.Release != "" {
			ver = info.Version.Release
		}

		result = append(result, appSummary{
			Name:        name,
			State:       info.State.String(),
			Mode:        info.Mode.String(),
			Description: info.Description,
			Version:     ver,
			Uptime:      info.Uptime,
			Weight:      info.Weight,
			Tags:        info.Tags,
			Processes:   len(info.Group),
		})
	}

	text, err := marshalResult(result)
	if err != nil {
		return nil, err
	}
	return textResult(text), nil
}

type appInfoParams struct {
	Name string `json:"name"`
}

func toolAppInfo(w gen.Process, params json.RawMessage) (any, error) {
	var p appInfoParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	info, err := w.Node().ApplicationInfo(gen.Atom(p.Name))
	if err != nil {
		return nil, fmt.Errorf("app_info: %w", err)
	}
	text, err := marshalResult(info)
	if err != nil {
		return nil, err
	}
	return textResult(text), nil
}

type appProcessesParams struct {
	Name  string `json:"name"`
	Limit int    `json:"limit"`
}

func toolAppProcesses(w gen.Process, params json.RawMessage) (any, error) {
	var p appProcessesParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if p.Limit < 1 {
		p.Limit = 100
	}

	processes, err := w.Node().ApplicationProcessListShortInfo(gen.Atom(p.Name), p.Limit)
	if err != nil {
		return nil, fmt.Errorf("app_processes: %w", err)
	}
	text, err := marshalResult(processes)
	if err != nil {
		return nil, err
	}
	return textResult(text), nil
}
