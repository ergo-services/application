package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"runtime"
	"runtime/pprof"
	"sort"
	"strings"
	"time"

	pprofprofile "github.com/google/pprof/profile"

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
					"description": "Maximum number of items to return after filtering (default: 50). Ignored when pid is specified"
				},
				"debug": {
					"type": "integer",
					"description": "Debug level: 1 = summary (count per stack), 2 = full traces (default: 2)"
				},
				"filter": {
					"type": "string",
					"description": "Include only goroutines whose stack contains this substring (server-side filtering)"
				},
				"exclude": {
					"type": "string",
					"description": "Exclude goroutines whose stack contains this substring (applied after filter)"
				}
			}
		}`),
		handler: toolPprofGoroutines,
	})

	r.register(ToolDefinition{
		Name:        "pprof_cpu",
		Description: "Collects CPU profile for a given duration and returns top functions by CPU usage. The worker is blocked during collection.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"duration": {
					"type": "integer",
					"description": "Profiling duration in seconds (default: 5, max: 30)"
				},
				"limit": {
					"type": "integer",
					"description": "Number of top functions to return (default: 20)"
				},
				"filter": {
					"type": "string",
					"description": "Include only functions whose name contains this substring"
				},
				"exclude": {
					"type": "string",
					"description": "Exclude functions whose name contains this substring"
				}
			}
		}`),
		handler: toolPprofCPU,
	})

	r.register(ToolDefinition{
		Name:        "pprof_heap",
		Description: "Returns heap memory profile showing top memory allocators by bytes in use.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"limit": {
					"type": "integer",
					"description": "Number of top functions to return (default: 20)"
				},
				"filter": {
					"type": "string",
					"description": "Include only functions whose name contains this substring"
				},
				"exclude": {
					"type": "string",
					"description": "Exclude functions whose name contains this substring"
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
	PID     string `json:"pid"`
	Limit   int    `json:"limit"`
	Debug   int    `json:"debug"`
	Filter  string `json:"filter"`
	Exclude string `json:"exclude"`
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
	total := profile.Count()

	// Split into blocks: debug=1 groups by "\n\n", debug=2 goroutines by "\n\n"
	blocks := strings.Split(dump, "\n\n")

	// Filter and exclude
	var filtered []string
	for _, block := range blocks {
		if strings.TrimSpace(block) == "" {
			continue
		}
		if p.Filter != "" && strings.Contains(block, p.Filter) == false {
			continue
		}
		if p.Exclude != "" && strings.Contains(block, p.Exclude) {
			continue
		}
		filtered = append(filtered, block)
	}

	matched := len(filtered)
	showing := matched
	if showing > p.Limit {
		filtered = filtered[:p.Limit]
		showing = p.Limit
	}

	// Build header
	header := fmt.Sprintf("goroutine profile: total %d", total)
	if p.Filter != "" || p.Exclude != "" {
		header += fmt.Sprintf(", matched %d", matched)
	}
	header += fmt.Sprintf(", showing %d", showing)

	result := header + "\n\n" + strings.Join(filtered, "\n\n")
	if showing < matched {
		result += fmt.Sprintf("\n\n... %d more matched goroutines not shown", matched-showing)
	}

	return textResult(result), nil
}

type pprofCPUParams struct {
	Duration int    `json:"duration"`
	Limit    int    `json:"limit"`
	Filter   string `json:"filter"`
	Exclude  string `json:"exclude"`
}

func toolPprofCPU(w gen.Process, params json.RawMessage) (any, error) {
	var p pprofCPUParams
	if len(params) > 0 {
		json.Unmarshal(params, &p)
	}
	if p.Duration < 1 {
		p.Duration = 5
	}
	if p.Duration > 30 {
		p.Duration = 30
	}
	if p.Limit < 1 {
		p.Limit = 20
	}

	var buf bytes.Buffer
	if err := pprof.StartCPUProfile(&buf); err != nil {
		return nil, fmt.Errorf("failed to start CPU profile: %w", err)
	}
	time.Sleep(time.Duration(p.Duration) * time.Second)
	pprof.StopCPUProfile()

	prof, err := pprofprofile.Parse(&buf)
	if err != nil {
		return nil, fmt.Errorf("failed to parse CPU profile: %w", err)
	}

	// Aggregate samples by function
	type funcStat struct {
		Name    string
		Flat    int64
		Cum     int64
		Samples int
	}
	flatByFunc := make(map[string]*funcStat)
	var totalSamples int64

	for _, sample := range prof.Sample {
		if len(sample.Value) == 0 || len(sample.Location) == 0 {
			continue
		}
		value := sample.Value[len(sample.Value)-1] // cpu nanoseconds
		totalSamples += value

		// Flat: only the top function
		if fn := topFunction(sample); fn != "" {
			s, ok := flatByFunc[fn]
			if ok == false {
				s = &funcStat{Name: fn}
				flatByFunc[fn] = s
			}
			s.Flat += value
			s.Samples++
		}

		// Cum: all functions in the stack
		seen := make(map[string]bool)
		for _, loc := range sample.Location {
			for _, line := range loc.Line {
				if line.Function == nil {
					continue
				}
				fn := line.Function.Name
				if seen[fn] {
					continue
				}
				seen[fn] = true
				s, ok := flatByFunc[fn]
				if ok == false {
					s = &funcStat{Name: fn}
					flatByFunc[fn] = s
				}
				s.Cum += value
			}
		}
	}

	// Collect, filter, sort
	var stats []*funcStat
	for _, s := range flatByFunc {
		if p.Filter != "" && strings.Contains(s.Name, p.Filter) == false {
			continue
		}
		if p.Exclude != "" && strings.Contains(s.Name, p.Exclude) {
			continue
		}
		stats = append(stats, s)
	}

	sort.Slice(stats, func(i, j int) bool {
		return stats[i].Flat > stats[j].Flat
	})

	matched := len(stats)
	if matched > p.Limit {
		stats = stats[:p.Limit]
	}
	showing := len(stats)

	// Format output
	var result strings.Builder
	fmt.Fprintf(&result, "CPU profile: %d seconds, %d total samples", p.Duration, totalSamples)
	if p.Filter != "" || p.Exclude != "" {
		fmt.Fprintf(&result, ", matched %d functions", matched)
	}
	fmt.Fprintf(&result, ", showing %d\n\n", showing)
	fmt.Fprintf(&result, "%-8s %-8s %s\n", "flat%", "cum%", "function")

	for _, s := range stats {
		flatPct := float64(0)
		cumPct := float64(0)
		if totalSamples > 0 {
			flatPct = float64(s.Flat) / float64(totalSamples) * 100
			cumPct = float64(s.Cum) / float64(totalSamples) * 100
		}
		fmt.Fprintf(&result, "%-8.1f %-8.1f %s\n", flatPct, cumPct, s.Name)
	}

	return textResult(result.String()), nil
}

func topFunction(sample *pprofprofile.Sample) string {
	if len(sample.Location) == 0 {
		return ""
	}
	loc := sample.Location[0]
	if len(loc.Line) == 0 {
		return ""
	}
	if loc.Line[0].Function == nil {
		return ""
	}
	return loc.Line[0].Function.Name
}

type pprofHeapParams struct {
	Limit   int    `json:"limit"`
	Filter  string `json:"filter"`
	Exclude string `json:"exclude"`
}

func toolPprofHeap(w gen.Process, params json.RawMessage) (any, error) {
	var p pprofHeapParams
	if len(params) > 0 {
		json.Unmarshal(params, &p)
	}
	if p.Limit < 1 {
		p.Limit = 20
	}

	profile := pprof.Lookup("heap")
	if profile == nil {
		return nil, fmt.Errorf("heap profile not available")
	}

	var buf bytes.Buffer
	if err := profile.WriteTo(&buf, 0); err != nil {
		return nil, fmt.Errorf("failed to write heap profile: %w", err)
	}

	prof, err := pprofprofile.Parse(&buf)
	if err != nil {
		return nil, fmt.Errorf("failed to parse heap profile: %w", err)
	}

	// Aggregate by function: inuse bytes and alloc bytes
	type heapStat struct {
		Name       string
		InuseBytes int64
		AllocBytes int64
	}
	byFunc := make(map[string]*heapStat)

	var totalInuse int64
	for _, sample := range prof.Sample {
		if len(sample.Value) < 4 || len(sample.Location) == 0 {
			continue
		}
		// heap profile values: alloc_objects, alloc_space, inuse_objects, inuse_space
		inuseBytes := sample.Value[3]
		allocBytes := sample.Value[1]
		totalInuse += inuseBytes

		fn := topFunction(sample)
		if fn == "" {
			continue
		}
		s, ok := byFunc[fn]
		if ok == false {
			s = &heapStat{Name: fn}
			byFunc[fn] = s
		}
		s.InuseBytes += inuseBytes
		s.AllocBytes += allocBytes
	}

	// Collect, filter, sort
	var stats []*heapStat
	for _, s := range byFunc {
		if p.Filter != "" && strings.Contains(s.Name, p.Filter) == false {
			continue
		}
		if p.Exclude != "" && strings.Contains(s.Name, p.Exclude) {
			continue
		}
		stats = append(stats, s)
	}

	sort.Slice(stats, func(i, j int) bool {
		return stats[i].InuseBytes > stats[j].InuseBytes
	})

	matched := len(stats)
	if matched > p.Limit {
		stats = stats[:p.Limit]
	}
	showing := len(stats)

	var result strings.Builder
	fmt.Fprintf(&result, "heap profile: total inuse %s", formatBytes(totalInuse))
	if p.Filter != "" || p.Exclude != "" {
		fmt.Fprintf(&result, ", matched %d functions", matched)
	}
	fmt.Fprintf(&result, ", showing %d\n\n", showing)
	fmt.Fprintf(&result, "%-12s %-12s %s\n", "inuse", "alloc", "function")

	for _, s := range stats {
		fmt.Fprintf(&result, "%-12s %-12s %s\n",
			formatBytes(s.InuseBytes), formatBytes(s.AllocBytes), s.Name)
	}

	return textResult(result.String()), nil
}

func formatBytes(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1fGB", float64(b)/float64(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1fMB", float64(b)/float64(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1fKB", float64(b)/float64(1<<10))
	default:
		return fmt.Sprintf("%dB", b)
	}
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
