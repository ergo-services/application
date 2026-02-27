package mcp

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"ergo.services/ergo/gen"
)

func parseLogLevels(names []string) []gen.LogLevel {
	if len(names) == 0 {
		return []gen.LogLevel{gen.LogLevelInfo, gen.LogLevelWarning, gen.LogLevelError, gen.LogLevelPanic}
	}
	var levels []gen.LogLevel
	for _, name := range names {
		switch strings.ToLower(name) {
		case "trace":
			levels = append(levels, gen.LogLevelTrace)
		case "debug":
			levels = append(levels, gen.LogLevelDebug)
		case "info":
			levels = append(levels, gen.LogLevelInfo)
		case "warning":
			levels = append(levels, gen.LogLevelWarning)
		case "error":
			levels = append(levels, gen.LogLevelError)
		case "panic":
			levels = append(levels, gen.LogLevelPanic)
		}
	}
	return levels
}

func registerSampleTools(r *toolRegistry) {
	r.register(ToolDefinition{
		Name:        "sample_start",
		Description: "Start an active sampler that periodically calls any MCP tool and stores results in a ring buffer. Read results with sample_read. Use this to monitor any metric over time: process_list with sorting for top-N tracking, node_info for node health, runtime_stats for memory trends, etc.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"tool": {
					"type": "string",
					"description": "Tool name to call periodically (e.g. process_list, node_info, runtime_stats, event_list)"
				},
				"arguments": {
					"type": "object",
					"description": "Arguments to pass to the tool on each call (e.g. {\"sort_by\":\"mailbox\",\"limit\":5})"
				},
				"interval_ms": {
					"type": "integer",
					"description": "Collection interval in ms (default: 5000, min: 100)"
				},
				"count": {
					"type": "integer",
					"description": "Number of samples to collect (0 = until stopped)"
				},
				"duration_sec": {
					"type": "integer",
					"description": "Run for N seconds then stop (default: 60, max: 3600, 0 = until stopped)"
				},
				"buffer_size": {
					"type": "integer",
					"description": "Ring buffer size (default: 256). Oldest entries overwritten when full"
				},
				"max_errors": {
					"type": "integer",
					"description": "Stop after N consecutive tool errors (default: 0 = ignore errors, keep retrying). Use 0 when polling for a rare condition like catching a process stack trace"
				}
			},
			"required": ["tool"]
		}`),
		handler: toolSampleStart,
	})

	r.register(ToolDefinition{
		Name:        "sample_listen",
		Description: "Start a passive sampler that listens for events. Captures log messages and/or event publications into a ring buffer. Read results with sample_read. Both log and event can be combined in one sampler.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"log_levels": {
					"type": "array",
					"items": {"type": "string"},
					"description": "Log levels to capture (trace, debug, info, warning, error, panic). Default: [info, warning, error, panic]. Omit or empty to disable log capture"
				},
				"log_source": {
					"type": "string",
					"description": "Filter logs by source (process, meta, node, network). Empty = all sources",
					"enum": ["", "process", "meta", "node", "network"]
				},
				"event": {
					"type": "string",
					"description": "Event name to subscribe to (receives published messages). Omit to disable event capture"
				},
				"event_node": {
					"type": "string",
					"description": "Node that owns the event (default: local node)"
				},
				"duration_sec": {
					"type": "integer",
					"description": "Run for N seconds then stop (default: 60, max: 3600, 0 = until stopped)"
				},
				"buffer_size": {
					"type": "integer",
					"description": "Ring buffer size (default: 256). Oldest entries overwritten when full"
				}
			}
		}`),
		handler: toolSampleListen,
	})

	r.register(ToolDefinition{
		Name:        "sample_read",
		Description: "Read collected samples from a running or completed sampler. Returns entries with sequence > since. Use since=0 to get all buffered entries, then pass the returned sequence value as since on next call to get only new entries.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"sampler_id": {
					"type": "string",
					"description": "Sampler ID returned from sample_start or sample_listen"
				},
				"since": {
					"type": "integer",
					"description": "Return entries with sequence > since (default: 0, returns all buffered)"
				}
			},
			"required": ["sampler_id"]
		}`),
		handler: toolSampleRead,
	})

	r.register(ToolDefinition{
		Name:        "sample_stop",
		Description: "Stop a running sampler. Remaining data can still be read via sample_read after stopping.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"sampler_id": {
					"type": "string",
					"description": "Sampler ID returned from sample_start or sample_listen"
				}
			},
			"required": ["sampler_id"]
		}`),
		handler: toolSampleStop,
	})

	r.register(ToolDefinition{
		Name:        "sample_list",
		Description: "List all active samplers with their configuration and progress.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {},
			"additionalProperties": false
		}`),
		handler: toolSampleList,
	})
}

// sample_start -- active sampler (periodic tool calls)

type sampleStartParams struct {
	Tool        string          `json:"tool"`
	Arguments   json.RawMessage `json:"arguments"`
	IntervalMS  int             `json:"interval_ms"`
	Count       int             `json:"count"`
	DurationSec int             `json:"duration_sec"`
	BufferSize  int             `json:"buffer_size"`
	MaxErrors   int             `json:"max_errors"`
}

func toolSampleStart(w gen.Process, params json.RawMessage) (any, error) {
	var p sampleStartParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	if p.Tool == "" {
		return nil, fmt.Errorf("tool is required")
	}

	if p.IntervalMS < 100 {
		p.IntervalMS = 5000
	}

	// Duration: always set. Default 60s, max 3600s
	if p.DurationSec == 0 {
		p.DurationSec = 60
	}
	if p.DurationSec > 3600 {
		p.DurationSec = 3600
	}

	bufferSize := 256
	if p.BufferSize > 0 {
		bufferSize = p.BufferSize
	}

	args := p.Arguments
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}

	samplerID := fmt.Sprintf("mcp_sampler_%s", generateSamplerID())

	config := samplerConfig{
		ID:         samplerID,
		Mode:       samplerModeActive,
		Tool:       p.Tool,
		Arguments:  args,
		Interval:   time.Duration(p.IntervalMS) * time.Millisecond,
		Count:      p.Count,
		Duration:   time.Duration(p.DurationSec) * time.Second,
		MaxErrors:  p.MaxErrors,
		BufferSize: bufferSize,
		Owner:      string(w.Node().Name()),
	}

	// get registry from pool -- it's passed as second arg
	reg, _ := w.Env(gen.Env("mcp_registry"))
	registry := reg.(*toolRegistry)

	if _, err := w.Spawn(factorySampler, gen.ProcessOptions{}, config, registry); err != nil {
		return nil, fmt.Errorf("failed to spawn sampler: %w", err)
	}

	result := map[string]any{
		"sampler_id":   samplerID,
		"mode":         "active",
		"tool":         p.Tool,
		"interval_ms":  p.IntervalMS,
		"count":        p.Count,
		"duration_sec": p.DurationSec,
		"buffer_size":  bufferSize,
	}
	text, err := marshalResult(result)
	if err != nil {
		return nil, err
	}
	return textResult(text), nil
}

// sample_listen -- passive sampler (log + event listeners)

type sampleListenParams struct {
	LogLevels   []string `json:"log_levels"`
	LogSource   string   `json:"log_source"`
	Event       string   `json:"event"`
	EventNode   string   `json:"event_node"`
	DurationSec int      `json:"duration_sec"`
	BufferSize  int      `json:"buffer_size"`
}

func toolSampleListen(w gen.Process, params json.RawMessage) (any, error) {
	var p sampleListenParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	hasLog := len(p.LogLevels) > 0
	hasEvent := p.Event != ""

	if hasLog == false && hasEvent == false {
		// default to log capture
		hasLog = true
	}

	// Duration limits
	if p.DurationSec == 0 {
		p.DurationSec = 60
	}
	if p.DurationSec > 3600 {
		p.DurationSec = 3600
	}

	bufferSize := 256
	if p.BufferSize > 0 {
		bufferSize = p.BufferSize
	}

	samplerID := fmt.Sprintf("mcp_sampler_%s", generateSamplerID())

	config := samplerConfig{
		ID:         samplerID,
		Mode:       samplerModePassive,
		Duration:   time.Duration(p.DurationSec) * time.Second,
		BufferSize: bufferSize,
		Owner:      string(w.Node().Name()),
	}

	if hasLog {
		config.LogLevels = parseLogLevels(p.LogLevels)
		config.LogSource = p.LogSource
	}

	if hasEvent {
		eventNode := w.Node().Name()
		if p.EventNode != "" {
			eventNode = gen.Atom(p.EventNode)
		}
		config.Event = gen.Event{
			Name: gen.Atom(p.Event),
			Node: eventNode,
		}
	}

	reg, _ := w.Env(gen.Env("mcp_registry"))
	registry := reg.(*toolRegistry)

	if _, err := w.Spawn(factorySampler, gen.ProcessOptions{}, config, registry); err != nil {
		return nil, fmt.Errorf("failed to spawn sampler: %w", err)
	}

	result := map[string]any{
		"sampler_id":   samplerID,
		"mode":         "passive",
		"duration_sec": p.DurationSec,
		"buffer_size":  bufferSize,
	}
	if hasLog {
		result["log_levels"] = p.LogLevels
		result["log_source"] = p.LogSource
	}
	if hasEvent {
		result["event"] = p.Event
	}

	text, err := marshalResult(result)
	if err != nil {
		return nil, err
	}
	return textResult(text), nil
}

// sample_read, sample_stop, sample_list -- shared

type sampleReadParams struct {
	SamplerID string `json:"sampler_id"`
	Since     int    `json:"since"`
}

func toolSampleRead(w gen.Process, params json.RawMessage) (any, error) {
	var p sampleReadParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	if p.SamplerID == "" {
		return nil, fmt.Errorf("sampler_id is required")
	}

	result, err := w.Call(gen.Atom(p.SamplerID), SampleReadRequest{Since: p.Since})
	if err != nil {
		return nil, fmt.Errorf("sampler %s not found or not responding: %w", p.SamplerID, err)
	}

	resp, ok := result.(SampleReadResponse)
	if ok == false {
		return nil, fmt.Errorf("unexpected response from sampler %s", p.SamplerID)
	}

	text, err := marshalResult(resp)
	if err != nil {
		return nil, err
	}
	return textResult(text), nil
}

type sampleStopParams struct {
	SamplerID string `json:"sampler_id"`
}

func toolSampleStop(w gen.Process, params json.RawMessage) (any, error) {
	var p sampleStopParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	if err := w.Send(gen.Atom(p.SamplerID), messageSamplerStop{}); err != nil {
		return nil, fmt.Errorf("sampler %s not found: %w", p.SamplerID, err)
	}

	return textResult(fmt.Sprintf("sampler %s stopping", p.SamplerID)), nil
}

func toolSampleList(w gen.Process, params json.RawMessage) (any, error) {
	var samplers []map[string]string
	w.Node().ProcessRangeShortInfo(func(info gen.ProcessShortInfo) bool {
		if strings.HasPrefix(string(info.Name), "mcp_sampler_") {
			inspect, err := w.Inspect(info.PID)
			if err != nil {
				return true
			}
			inspect["pid"] = info.PID.String()
			samplers = append(samplers, inspect)
		}
		return true
	})

	if samplers == nil {
		samplers = make([]map[string]string, 0)
	}

	text, err := marshalResult(samplers)
	if err != nil {
		return nil, err
	}
	return textResult(text), nil
}
