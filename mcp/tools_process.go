package mcp

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"ergo.services/ergo/gen"
)

func registerProcessTools(r *toolRegistry) {
	r.register(ToolDefinition{
		Name:        "process_list",
		Description: "Returns a list of processes on the node with short info (PID, name, behavior, application, messages in/out, mailbox depth, mailbox latency, running time, init time, wakeups, uptime, state, parent, leader). Supports filtering by all fields. Numeric filters use minimum threshold (returns processes >= value).",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"limit": {
					"type": "integer",
					"description": "Maximum number of processes to return (default: 100, 0 = all)"
				},
				"application": {
					"type": "string",
					"description": "Filter by application name (exact match)"
				},
				"behavior": {
					"type": "string",
					"description": "Filter by behavior type (substring match)"
				},
				"state": {
					"type": "string",
					"description": "Filter by process state: init, sleep, running, wait_response, terminated, zombee"
				},
				"name": {
					"type": "string",
					"description": "Filter by registered name (substring match)"
				},
				"min_messages_in": {
					"type": "integer",
					"description": "Minimum MessagesIn (cumulative messages received)"
				},
				"min_messages_out": {
					"type": "integer",
					"description": "Minimum MessagesOut (cumulative messages sent)"
				},
				"min_mailbox": {
					"type": "integer",
					"description": "Minimum MessagesMailbox (current mailbox depth)"
				},
				"min_mailbox_latency_ms": {
					"type": "number",
					"description": "Minimum MailboxLatency in milliseconds (requires -tags=latency)"
				},
				"min_running_time_ms": {
					"type": "number",
					"description": "Minimum RunningTime in milliseconds (cumulative callback execution)"
				},
				"min_init_time_ms": {
					"type": "number",
					"description": "Minimum InitTime in milliseconds (ProcessInit duration)"
				},
				"min_wakeups": {
					"type": "integer",
					"description": "Minimum Wakeups (Sleep->Running transitions)"
				},
				"min_uptime": {
					"type": "integer",
					"description": "Minimum Uptime in seconds"
				},
				"max_uptime": {
					"type": "integer",
					"description": "Maximum Uptime in seconds (find recently spawned processes, useful for restart loop detection)"
				},
				"sort_by": {
					"type": "string",
					"description": "Sort results by field (descending): mailbox, mailbox_latency, running_time, init_time, wakeups, uptime, messages_in, messages_out, drain",
					"enum": ["mailbox", "mailbox_latency", "running_time", "init_time", "wakeups", "uptime", "messages_in", "messages_out", "drain"]
				}
			}
		}`),
		handler: toolProcessList,
	})

	r.register(ToolDefinition{
		Name:        "process_children",
		Description: "Returns all child processes spawned by the given parent process (by PID or registered name). Shows the supervision tree under a specific process.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"parent": {
					"type": "string",
					"description": "Parent process: registered name or PID string"
				},
				"recursive": {
					"type": "boolean",
					"description": "If true, returns the full subtree (children of children). Default: false (direct children only)"
				}
			},
			"required": ["parent"]
		}`),
		handler: toolProcessChildren,
	})

	r.register(ToolDefinition{
		Name:        "process_info",
		Description: "Returns detailed information about a specific process: state, mailbox queues, message counts, links, monitors, aliases, events, meta processes, parent, leader, environment.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"pid": {
					"type": "string",
					"description": "Process PID string (e.g. '<ABCDEF12.0.1003>')"
				}
			},
			"required": ["pid"]
		}`),
		handler: toolProcessInfo,
	})

	r.register(ToolDefinition{
		Name:        "process_state",
		Description: "Returns the current state of a process (Init, Sleep, Running, WaitResponse, Terminated, Zombee).",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"pid": {
					"type": "string",
					"description": "Process PID string"
				}
			},
			"required": ["pid"]
		}`),
		handler: toolProcessState,
	})

	r.register(ToolDefinition{
		Name:        "process_lookup",
		Description: "Resolves a process name to PID, or a PID to its registered name.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"name": {
					"type": "string",
					"description": "Registered process name to resolve to PID"
				},
				"pid": {
					"type": "string",
					"description": "Process PID string to resolve to registered name"
				}
			}
		}`),
		handler: toolProcessLookup,
	})

	r.register(ToolDefinition{
		Name:        "process_inspect",
		Description: "Sends a custom inspection request to a process. The process HandleInspect callback returns key-value pairs with process-specific state.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"pid": {
					"type": "string",
					"description": "Process PID string"
				},
				"items": {
					"type": "array",
					"items": {"type": "string"},
					"description": "Optional inspection items to request"
				}
			},
			"required": ["pid"]
		}`),
		handler: toolProcessInspect,
	})

	r.register(ToolDefinition{
		Name:        "meta_inspect",
		Description: "Sends a custom inspection request to a meta process (WebSocket, Port, SSE connection, etc). Returns key-value pairs with meta-process-specific state.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"alias": {
					"type": "string",
					"description": "Meta process alias string"
				},
				"items": {
					"type": "array",
					"items": {"type": "string"},
					"description": "Optional inspection items to request"
				}
			},
			"required": ["alias"]
		}`),
		handler: toolMetaInspect,
	})
}

type processListParams struct {
	Limit              int     `json:"limit"`
	Application        string  `json:"application"`
	Behavior           string  `json:"behavior"`
	State              string  `json:"state"`
	Name               string  `json:"name"`
	MinMessagesIn      uint64  `json:"min_messages_in"`
	MinMessagesOut     uint64  `json:"min_messages_out"`
	MinMailbox         uint64  `json:"min_mailbox"`
	MinMailboxLatency  float64 `json:"min_mailbox_latency_ms"`
	MinRunningTime     float64 `json:"min_running_time_ms"`
	MinInitTime        float64 `json:"min_init_time_ms"`
	MinWakeups         uint64  `json:"min_wakeups"`
	MinUptime          int64   `json:"min_uptime"`
	MaxUptime          int64   `json:"max_uptime"`
	SortBy             string  `json:"sort_by"`
}

func toolProcessList(w gen.Process, params json.RawMessage) (any, error) {
	var p processListParams
	if len(params) > 0 {
		json.Unmarshal(params, &p)
	}
	if p.Limit == 0 {
		p.Limit = 100
	}

	// Check if latency data is available when latency filter/sort is requested
	latencyRequested := p.MinMailboxLatency > 0 || p.SortBy == "mailbox_latency"
	if latencyRequested {
		var latencyDisabled bool
		w.Node().ProcessRangeShortInfo(func(info gen.ProcessShortInfo) bool {
			if info.MailboxLatency == -1 {
				latencyDisabled = true
			}
			return false // check first process only
		})
		if latencyDisabled {
			return nil, fmt.Errorf("mailbox latency measurement is disabled. Rebuild the application with -tags=latency to enable it")
		}
	}

	// When sorting, collect all matching then sort+truncate
	collectAll := p.SortBy != ""
	var processes []gen.ProcessShortInfo

	w.Node().ProcessRangeShortInfo(func(info gen.ProcessShortInfo) bool {
		// string filters
		if p.Application != "" && string(info.Application) != p.Application {
			return true
		}
		if p.Behavior != "" && strings.Contains(info.Behavior, p.Behavior) == false {
			return true
		}
		if p.State != "" && strings.EqualFold(info.State.String(), p.State) == false {
			return true
		}
		if p.Name != "" && strings.Contains(string(info.Name), p.Name) == false {
			return true
		}

		// numeric filters (minimum threshold)
		if p.MinMessagesIn > 0 && info.MessagesIn < p.MinMessagesIn {
			return true
		}
		if p.MinMessagesOut > 0 && info.MessagesOut < p.MinMessagesOut {
			return true
		}
		if p.MinMailbox > 0 && info.MessagesMailbox < p.MinMailbox {
			return true
		}
		if p.MinMailboxLatency > 0 && float64(info.MailboxLatency)/1e6 < p.MinMailboxLatency {
			return true
		}
		if p.MinRunningTime > 0 && float64(info.RunningTime)/1e6 < p.MinRunningTime {
			return true
		}
		if p.MinInitTime > 0 && float64(info.InitTime)/1e6 < p.MinInitTime {
			return true
		}
		if p.MinWakeups > 0 && info.Wakeups < p.MinWakeups {
			return true
		}
		if p.MinUptime > 0 && info.Uptime < p.MinUptime {
			return true
		}
		if p.MaxUptime > 0 && info.Uptime > p.MaxUptime {
			return true
		}

		processes = append(processes, info)
		if collectAll == false && p.Limit > 0 && len(processes) >= p.Limit {
			return false
		}
		return true
	})

	// Sort descending by requested field
	if p.SortBy != "" && len(processes) > 0 {
		sort.Slice(processes, func(i, j int) bool {
			return processSortValue(processes[i], p.SortBy) > processSortValue(processes[j], p.SortBy)
		})
		if p.Limit > 0 && len(processes) > p.Limit {
			processes = processes[:p.Limit]
		}
	}

	text, err := marshalResult(processes)
	if err != nil {
		return nil, err
	}
	return textResult(text), nil
}

type processChildrenParams struct {
	Parent    string `json:"parent"`
	Recursive bool   `json:"recursive"`
}

func toolProcessChildren(w gen.Process, params json.RawMessage) (any, error) {
	var p processChildrenParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	// Resolve parent to PID: try as PID first, then as registered name
	var parentPID gen.PID
	pid, err := parsePID(w.Node().Name(), w.Node().Creation(), p.Parent)
	if err == nil {
		parentPID = pid
	} else {
		resolved, rerr := w.Node().ProcessPID(gen.Atom(p.Parent))
		if rerr != nil {
			return nil, fmt.Errorf("cannot resolve parent %q: %w", p.Parent, rerr)
		}
		parentPID = resolved
	}

	if p.Recursive == false {
		// Direct children only
		var children []gen.ProcessShortInfo
		w.Node().ProcessRangeShortInfo(func(info gen.ProcessShortInfo) bool {
			if info.Parent == parentPID {
				children = append(children, info)
			}
			return true
		})

		text, err := marshalResult(children)
		if err != nil {
			return nil, err
		}
		return textResult(text), nil
	}

	// Recursive: collect full subtree
	// First pass: build parent->children index
	parentIndex := make(map[gen.PID][]gen.ProcessShortInfo)
	w.Node().ProcessRangeShortInfo(func(info gen.ProcessShortInfo) bool {
		parentIndex[info.Parent] = append(parentIndex[info.Parent], info)
		return true
	})

	// BFS from parentPID
	var result []gen.ProcessShortInfo
	queue := []gen.PID{parentPID}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, child := range parentIndex[current] {
			result = append(result, child)
			queue = append(queue, child.PID)
		}
	}

	text, err := marshalResult(result)
	if err != nil {
		return nil, err
	}
	return textResult(text), nil
}

func processSortValue(info gen.ProcessShortInfo, field string) float64 {
	switch field {
	case "mailbox":
		return float64(info.MessagesMailbox)
	case "mailbox_latency":
		return float64(info.MailboxLatency)
	case "running_time":
		return float64(info.RunningTime)
	case "init_time":
		return float64(info.InitTime)
	case "wakeups":
		return float64(info.Wakeups)
	case "uptime":
		return float64(info.Uptime)
	case "messages_in":
		return float64(info.MessagesIn)
	case "messages_out":
		return float64(info.MessagesOut)
	case "drain":
		if info.Wakeups == 0 {
			return 0
		}
		return float64(info.MessagesIn) / float64(info.Wakeups)
	}
	return 0
}

type processInfoParams struct {
	PID string `json:"pid"`
}

func toolProcessInfo(w gen.Process, params json.RawMessage) (any, error) {
	var p processInfoParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	pid, err := parsePID(w.Node().Name(), w.Node().Creation(), p.PID)
	if err != nil {
		return nil, fmt.Errorf("invalid pid: %w", err)
	}

	info, err := w.Node().ProcessInfo(pid)
	if err != nil {
		return nil, fmt.Errorf("process_info: %w", err)
	}
	text, err := marshalResult(info)
	if err != nil {
		return nil, err
	}
	return textResult(text), nil
}

type processStateParams struct {
	PID string `json:"pid"`
}

func toolProcessState(w gen.Process, params json.RawMessage) (any, error) {
	var p processStateParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	pid, err := parsePID(w.Node().Name(), w.Node().Creation(), p.PID)
	if err != nil {
		return nil, fmt.Errorf("invalid pid: %w", err)
	}

	state, err := w.Node().ProcessState(pid)
	if err != nil {
		return nil, fmt.Errorf("process_state: %w", err)
	}
	return textResult(state.String()), nil
}

type processLookupParams struct {
	Name string `json:"name"`
	PID  string `json:"pid"`
}

func toolProcessLookup(w gen.Process, params json.RawMessage) (any, error) {
	var p processLookupParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	if p.Name != "" {
		pid, err := w.Node().ProcessPID(gen.Atom(p.Name))
		if err != nil {
			return nil, fmt.Errorf("process_lookup by name: %w", err)
		}
		return textResult(pid.String()), nil
	}

	if p.PID != "" {
		pid, err := parsePID(w.Node().Name(), w.Node().Creation(), p.PID)
		if err != nil {
			return nil, fmt.Errorf("invalid pid: %w", err)
		}
		name, err := w.Node().ProcessName(pid)
		if err != nil {
			return nil, fmt.Errorf("process_lookup by pid: %w", err)
		}
		return textResult(string(name)), nil
	}

	return nil, fmt.Errorf("either 'name' or 'pid' parameter is required")
}

type processInspectParams struct {
	PID   string   `json:"pid"`
	Items []string `json:"items"`
}

func toolProcessInspect(w gen.Process, params json.RawMessage) (any, error) {
	var p processInspectParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	pid, err := parsePID(w.Node().Name(), w.Node().Creation(), p.PID)
	if err != nil {
		return nil, fmt.Errorf("invalid pid: %w", err)
	}

	result, err := w.Node().Inspect(pid, p.Items...)
	if err != nil {
		return nil, fmt.Errorf("process_inspect: %w", err)
	}
	text, err := marshalResult(result)
	if err != nil {
		return nil, err
	}
	return textResult(text), nil
}

type metaInspectParams struct {
	Alias string   `json:"alias"`
	Items []string `json:"items"`
}

func toolMetaInspect(w gen.Process, params json.RawMessage) (any, error) {
	var p metaInspectParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	alias, err := parseAlias(w.Node().Name(), w.Node().Creation(), p.Alias)
	if err != nil {
		return nil, fmt.Errorf("invalid alias: %w", err)
	}

	result, err := w.Node().InspectMeta(alias, p.Items...)
	if err != nil {
		return nil, fmt.Errorf("meta_inspect: %w", err)
	}
	text, err := marshalResult(result)
	if err != nil {
		return nil, err
	}
	return textResult(text), nil
}
