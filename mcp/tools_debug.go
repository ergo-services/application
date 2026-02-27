package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"runtime"
	"runtime/pprof"
	"strings"

	"ergo.services/ergo/gen"
)

func registerDebugTools(r *toolRegistry) {
	r.register(ToolDefinition{
		Name:        "pprof_goroutines",
		Description: "Returns goroutine profile. Without pid: returns all goroutines (use limit to control output size). With pid: returns stack trace for a specific Ergo process goroutine. NOTE: pid lookup requires the node to be built with -tags=pprof. If the process is in Sleep state, its goroutine is parked and will not appear in the dump -- this is normal, not an error. Use process_state to check the process state before requesting its goroutine.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"pid": {
					"type": "string",
					"description": "Process PID to get stack trace for. Requires -tags=pprof build. If empty, returns all goroutines"
				},
				"limit": {
					"type": "integer",
					"description": "Maximum number of goroutines to return (default: 50). Ignored when pid is specified"
				},
				"debug": {
					"type": "integer",
					"description": "Debug level: 1 = summary (count per stack), 2 = full traces (default: 2)"
				}
			}
		}`),
		handler: toolPprofGoroutines,
	})

	r.register(ToolDefinition{
		Name:        "pprof_heap",
		Description: "Returns heap memory profile showing top memory allocators.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"debug": {
					"type": "integer",
					"description": "Debug level: 1 = human-readable (default: 1)"
				}
			}
		}`),
		handler: toolPprofHeap,
	})

	r.register(ToolDefinition{
		Name:        "runtime_stats",
		Description: "Returns Go runtime statistics: goroutine count, memory stats, CPU count, GC info.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {},
			"additionalProperties": false
		}`),
		handler: toolRuntimeStats,
	})
}

type pprofGoroutinesParams struct {
	PID   string `json:"pid"`
	Limit int    `json:"limit"`
	Debug int    `json:"debug"`
}

func toolPprofGoroutines(w gen.Process, params json.RawMessage) (any, error) {
	var p pprofGoroutinesParams
	if len(params) > 0 {
		json.Unmarshal(params, &p)
	}

	if p.PID != "" {
		return pprofProcessGoroutine(p.PID)
	}

	if p.Debug < 1 {
		p.Debug = 2
	}
	if p.Limit < 1 {
		p.Limit = 50
	}

	profile := pprof.Lookup("goroutine")
	if profile == nil {
		return nil, fmt.Errorf("goroutine profile not available")
	}

	var buf bytes.Buffer
	if err := profile.WriteTo(&buf, p.Debug); err != nil {
		return nil, fmt.Errorf("failed to write goroutine profile: %w", err)
	}

	dump := buf.String()

	// For debug=1: lines are compact, limit by line count
	if p.Debug == 1 {
		lines := strings.Split(dump, "\n")
		if len(lines) > p.Limit*3 {
			lines = lines[:p.Limit*3]
			lines = append(lines, fmt.Sprintf("\n... truncated (showing ~%d of %d goroutines, use limit parameter to see more)", p.Limit, profile.Count()))
		}
		return textResult(strings.Join(lines, "\n")), nil
	}

	// For debug=2: split by goroutine blocks, limit by block count
	goroutines := strings.Split(dump, "\n\n")
	total := len(goroutines)
	if total > p.Limit {
		goroutines = goroutines[:p.Limit]
		goroutines = append(goroutines, fmt.Sprintf("... truncated (showing %d of %d goroutines, use limit parameter to see more)", p.Limit, total))
	}

	return textResult(strings.Join(goroutines, "\n\n")), nil
}

type pprofHeapParams struct {
	Debug int `json:"debug"`
}

func toolPprofHeap(w gen.Process, params json.RawMessage) (any, error) {
	var p pprofHeapParams
	if len(params) > 0 {
		json.Unmarshal(params, &p)
	}
	if p.Debug < 1 {
		p.Debug = 1
	}

	profile := pprof.Lookup("heap")
	if profile == nil {
		return nil, fmt.Errorf("heap profile not available")
	}

	var buf bytes.Buffer
	if err := profile.WriteTo(&buf, p.Debug); err != nil {
		return nil, fmt.Errorf("failed to write heap profile: %w", err)
	}
	return textResult(buf.String()), nil
}

type runtimeStatsResult struct {
	Goroutines   int     `json:"goroutines"`
	CPUs         int     `json:"cpus"`
	HeapAlloc    uint64  `json:"heap_alloc"`
	HeapSys      uint64  `json:"heap_sys"`
	HeapInuse    uint64  `json:"heap_inuse"`
	HeapObjects  uint64  `json:"heap_objects"`
	StackInuse   uint64  `json:"stack_inuse"`
	TotalAlloc   uint64  `json:"total_alloc"`
	Sys          uint64  `json:"sys"`
	NumGC        uint32  `json:"num_gc"`
	LastGCPause  uint64  `json:"last_gc_pause_ns"`
	GCCPUPercent float64 `json:"gc_cpu_percent"`
}

func toolRuntimeStats(w gen.Process, params json.RawMessage) (any, error) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	var lastPause uint64
	if m.NumGC > 0 {
		lastPause = m.PauseNs[(m.NumGC+255)%256]
	}

	result := runtimeStatsResult{
		Goroutines:   runtime.NumGoroutine(),
		CPUs:         runtime.NumCPU(),
		HeapAlloc:    m.HeapAlloc,
		HeapSys:      m.HeapSys,
		HeapInuse:    m.HeapInuse,
		HeapObjects:  m.HeapObjects,
		StackInuse:   m.StackInuse,
		TotalAlloc:   m.TotalAlloc,
		Sys:          m.Sys,
		NumGC:        m.NumGC,
		LastGCPause:  lastPause,
		GCCPUPercent: m.GCCPUFraction * 100,
	}

	text, err := marshalResult(result)
	if err != nil {
		return nil, err
	}
	return textResult(text), nil
}
