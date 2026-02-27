package mcp

import (
	"encoding/json"
	"fmt"
	"strings"

	"ergo.services/ergo/gen"
)

func registerLogLevelTools(r *toolRegistry) {
	r.register(ToolDefinition{
		Name:        "log_level_get",
		Description: "Get current log level for a target: node (default), process (by PID or name), or meta process (by alias).",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"target": {
					"type": "string",
					"description": "Target: 'node' for node level, PID string for process, alias string for meta process, registered name for process. Default: node"
				}
			}
		}`),
		handler: toolLogLevelGet,
	})

	r.register(ToolDefinition{
		Name:        "log_level_set",
		Description: "Set log level for a target: node, process (by PID or name), or meta process (by alias). Levels: trace, debug, info, warning, error, panic, disabled.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"target": {
					"type": "string",
					"description": "Target: 'node' for node level, PID string for process, alias string for meta process, registered name for process. Default: node"
				},
				"level": {
					"type": "string",
					"description": "Log level to set",
					"enum": ["trace", "debug", "info", "warning", "error", "panic", "disabled"]
				}
			},
			"required": ["level"]
		}`),
		handler: toolLogLevelSet,
	})

	r.register(ToolDefinition{
		Name:        "loggers_list",
		Description: "List all registered loggers with their names and configured levels.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {},
			"additionalProperties": false
		}`),
		handler: toolLoggersList,
	})
}

func parseLogLevel(s string) (gen.LogLevel, error) {
	switch strings.ToLower(s) {
	case "trace":
		return gen.LogLevelTrace, nil
	case "debug":
		return gen.LogLevelDebug, nil
	case "info":
		return gen.LogLevelInfo, nil
	case "warning":
		return gen.LogLevelWarning, nil
	case "error":
		return gen.LogLevelError, nil
	case "panic":
		return gen.LogLevelPanic, nil
	case "disabled":
		return gen.LogLevelDisabled, nil
	}
	return 0, fmt.Errorf("unknown log level: %s", s)
}

type logLevelGetParams struct {
	Target string `json:"target"`
}

func toolLogLevelGet(w gen.Process, params json.RawMessage) (any, error) {
	var p logLevelGetParams
	if len(params) > 0 {
		json.Unmarshal(params, &p)
	}
	if p.Target == "" || p.Target == "node" {
		level := w.Node().Log().Level()
		return textResult(fmt.Sprintf("node log level: %s", level)), nil
	}

	// Try as PID
	pid, err := parsePID(w.Node().Name(), w.Node().Creation(), p.Target)
	if err == nil {
		level, err := w.Node().LogLevelProcess(pid)
		if err != nil {
			return nil, fmt.Errorf("log_level_get process %s: %w", p.Target, err)
		}
		return textResult(fmt.Sprintf("process %s log level: %s", p.Target, level)), nil
	}

	// Try as alias
	alias, err := parseAlias(w.Node().Name(), w.Node().Creation(), p.Target)
	if err == nil {
		level, err := w.Node().LogLevelMeta(alias)
		if err != nil {
			return nil, fmt.Errorf("log_level_get meta %s: %w", p.Target, err)
		}
		return textResult(fmt.Sprintf("meta %s log level: %s", p.Target, level)), nil
	}

	// Try as registered process name
	resolved, err := w.Node().ProcessPID(gen.Atom(p.Target))
	if err != nil {
		return nil, fmt.Errorf("cannot resolve target %q: not a valid PID, alias, or registered name", p.Target)
	}
	level, err := w.Node().LogLevelProcess(resolved)
	if err != nil {
		return nil, fmt.Errorf("log_level_get process %s: %w", p.Target, err)
	}
	return textResult(fmt.Sprintf("process %s (%s) log level: %s", p.Target, resolved, level)), nil
}

type logLevelSetParams struct {
	Target string `json:"target"`
	Level  string `json:"level"`
}

func toolLogLevelSet(w gen.Process, params json.RawMessage) (any, error) {
	var p logLevelSetParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	level, err := parseLogLevel(p.Level)
	if err != nil {
		return nil, err
	}

	if p.Target == "" || p.Target == "node" {
		if err := w.Node().Log().SetLevel(level); err != nil {
			return nil, fmt.Errorf("log_level_set node: %w", err)
		}
		return textResult(fmt.Sprintf("node log level set to %s", level)), nil
	}

	// Try as PID
	pid, perr := parsePID(w.Node().Name(), w.Node().Creation(), p.Target)
	if perr == nil {
		if err := w.Node().SetLogLevelProcess(pid, level); err != nil {
			return nil, fmt.Errorf("log_level_set process %s: %w", p.Target, err)
		}
		return textResult(fmt.Sprintf("process %s log level set to %s", p.Target, level)), nil
	}

	// Try as alias
	alias, aerr := parseAlias(w.Node().Name(), w.Node().Creation(), p.Target)
	if aerr == nil {
		if err := w.Node().SetLogLevelMeta(alias, level); err != nil {
			return nil, fmt.Errorf("log_level_set meta %s: %w", p.Target, err)
		}
		return textResult(fmt.Sprintf("meta %s log level set to %s", p.Target, level)), nil
	}

	// Try as registered name
	resolved, nerr := w.Node().ProcessPID(gen.Atom(p.Target))
	if nerr != nil {
		return nil, fmt.Errorf("cannot resolve target %q: not a valid PID, alias, or registered name", p.Target)
	}
	if err := w.Node().SetLogLevelProcess(resolved, level); err != nil {
		return nil, fmt.Errorf("log_level_set process %s: %w", p.Target, err)
	}
	return textResult(fmt.Sprintf("process %s (%s) log level set to %s", p.Target, resolved, level)), nil
}

func toolLoggersList(w gen.Process, params json.RawMessage) (any, error) {
	names := w.Node().Loggers()

	type loggerInfo struct {
		Name   string   `json:"name"`
		Levels []string `json:"levels"`
	}

	result := make([]loggerInfo, 0, len(names))
	for _, name := range names {
		levels := w.Node().LoggerLevels(name)
		levelStrs := make([]string, 0, len(levels))
		for _, l := range levels {
			levelStrs = append(levelStrs, l.String())
		}
		result = append(result, loggerInfo{
			Name:   name,
			Levels: levelStrs,
		})
	}

	text, err := marshalResult(result)
	if err != nil {
		return nil, err
	}
	return textResult(text), nil
}
