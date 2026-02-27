package mcp

import (
	"encoding/json"
	"fmt"
	"time"

	"ergo.services/ergo/act"
	"ergo.services/ergo/gen"
)

type messageSamplerTick struct{}
type messageSamplerStop struct{}

// samplerMode distinguishes active (periodic tool calls) from passive (event-driven) samplers.
type samplerMode string

const (
	samplerModeActive  samplerMode = "active"
	samplerModePassive samplerMode = "passive"
)

type samplerConfig struct {
	ID         string
	Mode       samplerMode
	Interval   time.Duration
	Count      int           // 0 = until stopped
	Duration   time.Duration // 0 = use count
	MaxErrors  int           // 0 = ignore errors (keep retrying), >0 = stop after N consecutive errors
	BufferSize int
	Owner      string // node name that initiated the sampler

	// Active mode: tool to call periodically
	Tool      string
	Arguments json.RawMessage

	// Passive mode: what to listen to
	LogLevels []gen.LogLevel
	LogSource string    // filter: process, meta, node, network, "" = all
	Event     gen.Event // event to subscribe to
}

// ringBuffer is a fixed-size circular buffer.
// No locks -- accessed only from actor callbacks.
type ringBuffer struct {
	items []SampleEntry
	size  int
	head  int
	count int
}

func newRingBuffer(size int) *ringBuffer {
	if size <= 0 {
		size = 256
	}
	return &ringBuffer{
		items: make([]SampleEntry, size),
		size:  size,
	}
}

func (rb *ringBuffer) push(entry SampleEntry) {
	rb.items[rb.head] = entry
	rb.head = (rb.head + 1) % rb.size
	if rb.count < rb.size {
		rb.count++
	}
}

// readSince returns entries with Sequence > since, oldest first.
func (rb *ringBuffer) readSince(since int) []SampleEntry {
	if rb.count == 0 {
		return nil
	}

	var result []SampleEntry
	start := (rb.head - rb.count + rb.size) % rb.size
	for i := 0; i < rb.count; i++ {
		idx := (start + i) % rb.size
		if rb.items[idx].Sequence > since {
			result = append(result, rb.items[idx])
		}
	}
	return result
}

func factorySampler() gen.ProcessBehavior {
	return &sampler{}
}

type sampler struct {
	act.Actor
	config     samplerConfig
	registry   *toolRegistry
	sequence   int
	errors     int // consecutive errors
	completed  bool
	loggerName string // registered logger name (for passive log)
	buffer     *ringBuffer
	startedAt  time.Time
	expiresAt  time.Time // zero if no duration limit
}

func (s *sampler) Init(args ...any) error {
	s.config = args[0].(samplerConfig)
	s.registry = args[1].(*toolRegistry)
	s.buffer = newRingBuffer(s.config.BufferSize)
	s.startedAt = time.Now()
	if s.config.Duration > 0 {
		s.expiresAt = s.startedAt.Add(s.config.Duration)
	}

	// Register with sampler ID as process name for direct addressing
	if err := s.RegisterName(gen.Atom(s.config.ID)); err != nil {
		return fmt.Errorf("cannot register sampler name: %w", err)
	}

	// Schedule duration-based stop
	if s.config.Duration > 0 {
		s.SendAfter(s.PID(), messageSamplerStop{}, s.config.Duration)
	}

	switch s.config.Mode {
	case samplerModeActive:
		// start periodic tick immediately
		s.Send(s.PID(), messageSamplerTick{})

	case samplerModePassive:
		// register as logger
		if len(s.config.LogLevels) > 0 {
			s.loggerName = fmt.Sprintf("mcp_sampler_%s", s.config.ID)
			if err := s.Node().LoggerAddPID(s.PID(), s.loggerName, s.config.LogLevels...); err != nil {
				return fmt.Errorf("cannot register logger: %w", err)
			}
		}

		// subscribe to event
		if s.config.Event.Name != "" {
			if _, err := s.MonitorEvent(s.config.Event); err != nil {
				return fmt.Errorf("cannot monitor event %s: %w", s.config.Event.Name, err)
			}
		}
	}

	return nil
}

func (s *sampler) HandleMessage(from gen.PID, message any) error {
	switch message.(type) {
	case messageSamplerTick:
		result, err := s.registry.dispatch(s, s.config.Tool, s.config.Arguments)
		if err != nil {
			s.errors++
			// MaxErrors 0 = tolerate unlimited errors (keep retrying)
			if s.config.MaxErrors > 0 && s.errors >= s.config.MaxErrors {
				s.completed = true
				return gen.TerminateReasonNormal
			}
			s.SendAfter(s.PID(), messageSamplerTick{}, s.config.Interval)
			return nil
		}

		s.errors = 0
		s.buffer.push(SampleEntry{
			Sequence:  s.sequence,
			Timestamp: time.Now(),
			Data:      result,
		})
		s.sequence++

		if s.config.Count > 0 && s.sequence >= s.config.Count {
			s.completed = true
			return gen.TerminateReasonNormal
		}

		s.SendAfter(s.PID(), messageSamplerTick{}, s.config.Interval)

	case messageSamplerStop:
		s.completed = true
		return gen.TerminateReasonNormal
	}
	return nil
}

// HandleLog is invoked for log messages when this process is registered as a logger.
func (s *sampler) HandleLog(message gen.MessageLog) error {
	entry := buildLogEntry(message, s.config.LogSource)
	if entry == nil {
		return nil
	}

	s.buffer.push(SampleEntry{
		Sequence:  s.sequence,
		Timestamp: message.Time,
		Data:      entry,
	})
	s.sequence++
	return nil
}

// HandleEvent is invoked when a monitored event publishes a message.
func (s *sampler) HandleEvent(message gen.MessageEvent) error {
	s.buffer.push(SampleEntry{
		Sequence:  s.sequence,
		Timestamp: time.Unix(0, message.Timestamp),
		Data: map[string]any{
			"event":   fmt.Sprintf("%s@%s", message.Event.Name, message.Event.Node),
			"message": message.Message,
		},
	})
	s.sequence++
	return nil
}

// HandleCall handles SampleReadRequest to return collected samples.
func (s *sampler) HandleCall(from gen.PID, ref gen.Ref, request any) (any, error) {
	switch r := request.(type) {
	case SampleReadRequest:
		entries := s.buffer.readSince(r.Since)
		return SampleReadResponse{
			SamplerID: s.config.ID,
			Mode:      string(s.config.Mode),
			Tool:      s.config.Tool,
			Sequence:  s.sequence,
			Completed: s.completed,
			Samples:   entries,
		}, nil
	}
	return nil, nil
}

func (s *sampler) HandleInspect(from gen.PID, item ...string) map[string]string {
	info := map[string]string{
		"id":          s.config.ID,
		"description": s.describe(),
		"status":      s.status(),
		"owner":       s.config.Owner,
		"samples":     fmt.Sprintf("%d collected, buffer %d/%d", s.sequence, s.buffer.count, s.buffer.size),
		"uptime":      time.Since(s.startedAt).Truncate(time.Second).String(),
	}

	if s.expiresAt.IsZero() == false {
		info["deadline"] = s.expiresAt.Format(time.RFC3339)
		remaining := time.Until(s.expiresAt)
		if remaining > 0 {
			info["remaining"] = remaining.Truncate(time.Second).String()
		}
	}

	if s.config.Count > 0 {
		info["progress"] = fmt.Sprintf("%d/%d samples", s.sequence, s.config.Count)
	}

	if s.config.Mode == samplerModeActive && s.errors > 0 {
		info["errors"] = fmt.Sprintf("%d consecutive", s.errors)
	}

	return info
}

// describe returns a human-readable summary of what this sampler does.
func (s *sampler) describe() string {
	switch s.config.Mode {
	case samplerModeActive:
		args := ""
		if len(s.config.Arguments) > 0 && string(s.config.Arguments) != "{}" {
			args = formatArgs(s.config.Arguments)
		}
		if args != "" {
			return fmt.Sprintf("%s(%s) every %s", s.config.Tool, args, s.config.Interval)
		}
		return fmt.Sprintf("%s every %s", s.config.Tool, s.config.Interval)

	case samplerModePassive:
		var parts []string
		if len(s.config.LogLevels) > 0 {
			levels := make([]string, len(s.config.LogLevels))
			for i, l := range s.config.LogLevels {
				levels[i] = l.String()
			}
			desc := fmt.Sprintf("log [%s]", joinStrings(levels, ", "))
			if s.config.LogSource != "" {
				desc += fmt.Sprintf(" source=%s", s.config.LogSource)
			}
			parts = append(parts, desc)
		}
		if s.config.Event.Name != "" {
			parts = append(parts, fmt.Sprintf("event %s@%s", s.config.Event.Name, s.config.Event.Node))
		}
		return fmt.Sprintf("listen: %s", joinStrings(parts, " + "))
	}
	return "unknown"
}

// status returns a human-readable status string.
func (s *sampler) status() string {
	if s.completed {
		return "completed"
	}
	if s.expiresAt.IsZero() == false {
		remaining := time.Until(s.expiresAt)
		if remaining <= 0 {
			return "expired"
		}
	}
	return "running"
}

// formatArgs converts JSON arguments to a compact key=value string.
func formatArgs(raw json.RawMessage) string {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return string(raw)
	}
	var parts []string
	for k, v := range m {
		// strip quotes from simple strings
		s := string(v)
		if len(s) > 1 && s[0] == '"' && s[len(s)-1] == '"' {
			s = s[1 : len(s)-1]
		}
		parts = append(parts, fmt.Sprintf("%s=%s", k, s))
	}
	return joinStrings(parts, ", ")
}

func joinStrings(s []string, sep string) string {
	result := ""
	for i, v := range s {
		if i > 0 {
			result += sep
		}
		result += v
	}
	return result
}

func (s *sampler) Terminate(reason error) {
	if s.loggerName != "" {
		s.Node().LoggerDelete(s.loggerName)
	}
}

// buildLogEntry creates a log entry map from a MessageLog, filtering by source.
// Returns nil if the message doesn't match the source filter.
func buildLogEntry(message gen.MessageLog, source string) map[string]any {
	entry := map[string]any{
		"time":    message.Time.Format(time.RFC3339Nano),
		"level":   message.Level.String(),
		"message": fmt.Sprintf(message.Format, message.Args...),
	}

	switch src := message.Source.(type) {
	case gen.MessageLogProcess:
		if source != "" && source != "process" {
			return nil
		}
		entry["type"] = "process"
		if src.Name != "" {
			entry["source"] = fmt.Sprintf("%s (%s) %s", src.PID, src.Name, src.Behavior)
		} else {
			entry["source"] = fmt.Sprintf("%s %s", src.PID, src.Behavior)
		}
	case gen.MessageLogMeta:
		if source != "" && source != "meta" {
			return nil
		}
		entry["type"] = "meta"
		entry["source"] = fmt.Sprintf("%s (parent: %s) %s", src.Meta, src.Parent, src.Behavior)
	case gen.MessageLogNode:
		if source != "" && source != "node" {
			return nil
		}
		entry["type"] = "node"
		entry["source"] = string(src.Node)
	case gen.MessageLogNetwork:
		if source != "" && source != "network" {
			return nil
		}
		entry["type"] = "network"
		entry["source"] = fmt.Sprintf("%s <-> %s", src.Node, src.Peer)
	}

	return entry
}
