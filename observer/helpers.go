package observer

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"ergo.services/ergo/gen"
)

const inspectListCap = 20

func inspectList(values []string) string {
	sort.Strings(values)
	return inspectListOrdered(values)
}

func inspectListOrdered(values []string) string {
	if len(values) == 0 {
		return "none"
	}
	if len(values) <= inspectListCap {
		return strings.Join(values, ", ")
	}
	return fmt.Sprintf("%s, +%d more", strings.Join(values[:inspectListCap], ", "), len(values)-inspectListCap)
}

func inspectAge(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	return time.Since(t).Truncate(time.Second).String()
}

func inspectArmed(t time.Time) string {
	if t.IsZero() {
		return "not armed"
	}
	left := time.Until(t).Truncate(time.Second)
	if left < 0 {
		return "due"
	}
	return left.String()
}

func inspectCounters(counters map[string]int64) string {
	if len(counters) == 0 {
		return "none"
	}
	pairs := make([]string, 0, len(counters))
	for reason, count := range counters {
		pairs = append(pairs, fmt.Sprintf("%s=%d", reason, count))
	}
	sort.Strings(pairs)
	return strings.Join(pairs, ", ")
}

// a fresh errors.New with the same text is not equal to the sentinel, and a supervisor reads
// anything but Normal or Shutdown as an abnormal exit worth restarting
func exitReason(s string) error {
	switch s {
	case "", "normal":
		return gen.TerminateReasonNormal
	case "shutdown":
		return gen.TerminateReasonShutdown
	case "kill":
		return gen.TerminateReasonKill
	case "panic":
		return gen.TerminateReasonPanic
	}
	return errors.New(s)
}

func argValue(args map[string]any, key string, into any) error {
	v, exist := args[key]
	if exist == false || v == nil {
		return fmt.Errorf("%s is required", key)
	}
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("invalid %s: %s", key, err)
	}
	if err := json.Unmarshal(data, into); err != nil {
		return fmt.Errorf("invalid %s: %s", key, err)
	}
	return nil
}

// the node takes 1 for yes, -1 for no and 0 for either: read as a plain bool, a missing filter
// would mean no
func triState(args map[string]any, key string) int {
	value, given := args[key].(bool)
	switch {
	case given == false:
		return 0
	case value:
		return 1
	}
	return -1
}

// an agent sends text, the browser sends the object it already holds
func argPID(args map[string]any, key string) (gen.PID, error) {
	if text, ok := args[key].(string); ok {
		node, _ := args[nodeArgument].(string)
		return mcpParsePID(text, gen.Atom(node))
	}

	var pid gen.PID
	if err := argValue(args, key, &pid); err != nil {
		return pid, err
	}
	if pid.Node == "" || pid.ID == 0 {
		return pid, fmt.Errorf("incomplete %s", key)
	}
	return pid, nil
}

func argAlias(args map[string]any, key string) (gen.Alias, error) {
	if text, ok := args[key].(string); ok {
		node, _ := args[nodeArgument].(string)
		return mcpParseAlias(text, gen.Atom(node))
	}

	var alias gen.Alias
	if err := argValue(args, key, &alias); err != nil {
		return alias, err
	}
	if alias.Node == "" {
		return alias, fmt.Errorf("incomplete %s", key)
	}
	return alias, nil
}

// The name of an enum back to the value, the inverse of its String. The framework prints the
// name and the wire carries the name, so the table that reads it belongs on this side too.
func argLogLevel(args map[string]any, key string) (gen.LogLevel, error) {
	name, _ := args[key].(string)
	switch name {
	case "":
		return 0, fmt.Errorf("%s is required", key)
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
	case "system":
		return gen.LogLevelSystem, nil
	case "default":
		return gen.LogLevelDefault, nil
	}
	return 0, fmt.Errorf("invalid %s: %q", key, name)
}

func argMessagePriority(args map[string]any, key string) (gen.MessagePriority, error) {
	name, _ := args[key].(string)
	switch name {
	case "":
		return 0, fmt.Errorf("%s is required", key)
	case "normal":
		return gen.MessagePriorityNormal, nil
	case "high":
		return gen.MessagePriorityHigh, nil
	case "max":
		return gen.MessagePriorityMax, nil
	}
	return 0, fmt.Errorf("invalid %s: %q", key, name)
}

func argCompressionLevel(args map[string]any, key string) (gen.CompressionLevel, error) {
	name, _ := args[key].(string)
	switch name {
	case "":
		return 0, fmt.Errorf("%s is required", key)
	case "best size":
		return gen.CompressionBestSize, nil
	case "best speed":
		return gen.CompressionBestSpeed, nil
	case "default":
		return gen.CompressionDefault, nil
	}
	return 0, fmt.Errorf("invalid %s: %q", key, name)
}

func argApplicationMode(args map[string]any, key string) (gen.ApplicationMode, error) {
	name, _ := args[key].(string)
	switch name {
	case "":
		return 0, fmt.Errorf("%s is required", key)
	case "permanent":
		return gen.ApplicationModePermanent, nil
	case "transient":
		return gen.ApplicationModeTransient, nil
	case "temporary":
		return gen.ApplicationModeTemporary, nil
	}
	return 0, fmt.Errorf("invalid %s: %q", key, name)
}

// The value is text and the type says how to read it: json has one number, and a message the
// receiver reads as int8 has to arrive as int8. Out of range is a refusal, never a wrap.
func argSendMessage(args map[string]any) (any, error) {
	kind, _ := args["type"].(string)
	text, _ := args["value"].(string)
	node, _ := args[nodeArgument].(string)

	switch kind {
	case "":
		return nil, fmt.Errorf("type is required")
	case "string":
		return text, nil
	case "atom":
		return gen.Atom(text), nil
	case "bool":
		switch text {
		case "true":
			return true, nil
		case "false":
			return false, nil
		}
		return nil, fmt.Errorf("value is true or false, not %q", text)

	case "binary":
		out, err := base64.StdEncoding.DecodeString(text)
		if err != nil {
			return nil, fmt.Errorf("value is not base64: %s", err)
		}
		return out, nil

	case "int", "int8", "int16", "int32", "int64":
		bits := map[string]int{"int": 64, "int8": 8, "int16": 16, "int32": 32, "int64": 64}[kind]
		number, err := strconv.ParseInt(text, 10, bits)
		if err != nil {
			return nil, fmt.Errorf("value %q is not %s: %s", text, kind, err)
		}
		switch kind {
		case "int":
			return int(number), nil
		case "int8":
			return int8(number), nil
		case "int16":
			return int16(number), nil
		case "int32":
			return int32(number), nil
		}
		return number, nil

	case "uint", "uint8", "uint16", "uint32", "uint64":
		bits := map[string]int{"uint": 64, "uint8": 8, "uint16": 16, "uint32": 32, "uint64": 64}[kind]
		number, err := strconv.ParseUint(text, 10, bits)
		if err != nil {
			return nil, fmt.Errorf("value %q is not %s: %s", text, kind, err)
		}
		switch kind {
		case "uint":
			return uint(number), nil
		case "uint8":
			return uint8(number), nil
		case "uint16":
			return uint16(number), nil
		case "uint32":
			return uint32(number), nil
		}
		return number, nil

	case "float32", "float64":
		bits := 64
		if kind == "float32" {
			bits = 32
		}
		number, err := strconv.ParseFloat(text, bits)
		if err != nil {
			return nil, fmt.Errorf("value %q is not %s: %s", text, kind, err)
		}
		if kind == "float32" {
			return float32(number), nil
		}
		return number, nil

	case "time":
		at, err := time.Parse(time.RFC3339Nano, text)
		if err != nil {
			return nil, fmt.Errorf("value %q is not a time: %s", text, err)
		}
		return at, nil

	case "pid":
		return mcpParsePID(text, gen.Atom(node))
	case "process_id":
		return mcpParseProcessID(text, gen.Atom(node))
	case "alias":
		return mcpParseAlias(text, gen.Atom(node))
	case "ref":
		return mcpParseRef(text, gen.Atom(node))
	case "event":
		return mcpParseEvent(text, gen.Atom(node))
	}
	return nil, fmt.Errorf("there is no type %q", kind)
}

func hasArg(args map[string]any, key string) bool {
	v, exist := args[key]
	return exist && v != nil
}
