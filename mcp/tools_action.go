package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"ergo.services/ergo/gen"
)

func registerActionTools(r *toolRegistry) {
	r.register(ToolDefinition{
		Name:        "message_types",
		Description: "Lists all registered message types (registered via edf.RegisterTypeOf). These types can be inspected with message_type_info and used to send typed messages with send_message.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"filter": {
					"type": "string",
					"description": "Filter type names by substring (case-insensitive)"
				}
			}
		}`),
		handler: toolMessageTypes,
	})

	r.register(ToolDefinition{
		Name:        "message_type_info",
		Description: "Shows the structure of a registered message type: field names, types, and JSON tags. Use this to understand what fields to fill when sending a typed message.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"type_name": {
					"type": "string",
					"description": "Type name: full EDF name (#pkgpath/TypeName) or short name (TypeName)"
				}
			},
			"required": ["type_name"]
		}`),
		handler: toolMessageTypeInfo,
	})

	r.register(ToolDefinition{
		Name:        "send_message",
		Description: "Send an async message to a process. If type_name is specified, constructs a typed Go struct using reflection from the EDF type registry. Without type_name, sends the raw JSON value.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"to": {
					"type": "string",
					"description": "Target: registered process name or PID string"
				},
				"type_name": {
					"type": "string",
					"description": "EDF-registered type name (full or short). If empty, sends raw JSON value"
				},
				"message": {
					"description": "Message data: JSON object with field values (for typed) or any JSON value (for raw)"
				},
				"important": {
					"type": "boolean",
					"description": "Use important delivery (immediate ErrProcessUnknown if remote target missing, instead of timeout). Default: false"
				}
			},
			"required": ["to", "message"]
		}`),
		handler: toolSendMessage,
	})

	r.register(ToolDefinition{
		Name:        "call_process",
		Description: "Make a synchronous request to a process and wait for response. If type_name is specified, constructs a typed request. Returns the process response.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"to": {
					"type": "string",
					"description": "Target: registered process name or PID string"
				},
				"type_name": {
					"type": "string",
					"description": "EDF-registered type name for the request. If empty, sends raw JSON value"
				},
				"request": {
					"description": "Request data: JSON object with field values (for typed) or any JSON value (for raw)"
				},
				"timeout": {
					"type": "integer",
					"description": "Timeout in seconds (default: 5)"
				},
				"important": {
					"type": "boolean",
					"description": "Use important delivery (immediate ErrProcessUnknown if remote target missing). Default: false"
				}
			},
			"required": ["to", "request"]
		}`),
		handler: toolCallProcess,
	})

	r.register(ToolDefinition{
		Name:        "send_exit",
		Description: "Send an exit signal to a process. The process receives a MessageExitPID in its Urgent mailbox and terminates by default (unless it traps exits).",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"pid": {
					"type": "string",
					"description": "Process PID string"
				},
				"reason": {
					"type": "string",
					"description": "Exit reason: normal, shutdown, kill, or custom string (default: normal)"
				}
			},
			"required": ["pid"]
		}`),
		handler: toolSendExit,
	})

	r.register(ToolDefinition{
		Name:        "process_kill",
		Description: "Forcefully kill a process. The process transitions to Zombee state immediately. Use send_exit for graceful termination.",
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
		handler: toolProcessKill,
	})
}

type messageTypesParams struct {
	Filter string `json:"filter"`
}

func toolMessageTypes(w gen.Process, params json.RawMessage) (any, error) {
	var p messageTypesParams
	if len(params) > 0 {
		json.Unmarshal(params, &p)
	}

	names := listRegisteredTypes()
	sort.Strings(names)

	if p.Filter != "" {
		filter := strings.ToLower(p.Filter)
		filtered := make([]string, 0)
		for _, name := range names {
			if strings.Contains(strings.ToLower(name), filter) {
				filtered = append(filtered, name)
			}
		}
		names = filtered
	}

	text, err := marshalResult(names)
	if err != nil {
		return nil, err
	}
	return textResult(text), nil
}

type messageTypeInfoParams struct {
	TypeName string `json:"type_name"`
}

func toolMessageTypeInfo(w gen.Process, params json.RawMessage) (any, error) {
	var p messageTypeInfoParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	t, ok := lookupType(p.TypeName)
	if ok == false {
		return nil, fmt.Errorf("type not found: %s. Use message_types to list registered types", p.TypeName)
	}

	fields := describeType(t)
	result := map[string]any{
		"type_name": fmt.Sprintf("%s/%s", t.PkgPath(), t.Name()),
		"kind":      t.Kind().String(),
		"fields":    fields,
	}
	text, err := marshalResult(result)
	if err != nil {
		return nil, err
	}
	return textResult(text), nil
}

type sendMessageParams struct {
	To        string          `json:"to"`
	TypeName  string          `json:"type_name"`
	Message   json.RawMessage `json:"message"`
	Important bool            `json:"important"`
}

func toolSendMessage(w gen.Process, params json.RawMessage) (any, error) {
	var p sendMessageParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	message, err := buildMessage(p.TypeName, p.Message)
	if err != nil {
		return nil, err
	}

	to := resolveTarget(w, p.To)

	if p.Important {
		if err := w.SendImportant(to, message); err != nil {
			return nil, fmt.Errorf("send_message (important): %w", err)
		}
	} else {
		if err := w.Send(to, message); err != nil {
			return nil, fmt.Errorf("send_message: %w", err)
		}
	}

	typeName := p.TypeName
	if typeName == "" {
		typeName = fmt.Sprintf("%T", message)
	}
	return textResult(fmt.Sprintf("message sent to %s (type: %s, important: %v)", p.To, typeName, p.Important)), nil
}

type callProcessParams struct {
	To        string          `json:"to"`
	TypeName  string          `json:"type_name"`
	Request   json.RawMessage `json:"request"`
	Timeout   int             `json:"timeout"`
	Important bool            `json:"important"`
}

func toolCallProcess(w gen.Process, params json.RawMessage) (any, error) {
	var p callProcessParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	request, err := buildMessage(p.TypeName, p.Request)
	if err != nil {
		return nil, err
	}

	to := resolveTarget(w, p.To)

	var response any
	if p.Important {
		response, err = w.CallImportant(to, request)
	} else if p.Timeout > 0 {
		response, err = w.CallWithTimeout(to, request, p.Timeout)
	} else {
		response, err = w.Call(to, request)
	}

	if err != nil {
		return nil, fmt.Errorf("call_process: %w", err)
	}

	text, merr := marshalResult(response)
	if merr != nil {
		// response may not be JSON-serializable, fallback to fmt
		text = fmt.Sprintf("%#v", response)
	}
	return textResult(text), nil
}

// buildMessage constructs a typed or raw message from JSON params
func buildMessage(typeName string, data json.RawMessage) (any, error) {
	if typeName != "" {
		t, ok := lookupType(typeName)
		if ok == false {
			return nil, fmt.Errorf("type not found: %s. Use message_types to list registered types", typeName)
		}
		msg, err := constructMessage(t, data)
		if err != nil {
			return nil, fmt.Errorf("failed to construct %s: %w", typeName, err)
		}
		return msg, nil
	}

	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	return raw, nil
}

// resolveTarget resolves a string to PID or Atom
func resolveTarget(w gen.Process, target string) any {
	pid, err := parsePID(w.Node().Name(), w.Node().Creation(), target)
	if err == nil {
		return pid
	}
	return gen.Atom(target)
}

type sendExitParams struct {
	PID    string `json:"pid"`
	Reason string `json:"reason"`
}

func toolSendExit(w gen.Process, params json.RawMessage) (any, error) {
	var p sendExitParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	pid, err := parsePID(w.Node().Name(), w.Node().Creation(), p.PID)
	if err != nil {
		return nil, fmt.Errorf("invalid pid: %w", err)
	}

	reason := p.Reason
	if reason == "" {
		reason = "normal"
	}

	var exitReason error
	switch reason {
	case "normal":
		exitReason = gen.TerminateReasonNormal
	case "shutdown":
		exitReason = gen.TerminateReasonShutdown
	case "kill":
		exitReason = gen.TerminateReasonKill
	default:
		exitReason = errors.New(reason)
	}

	if err := w.Node().SendExit(pid, exitReason); err != nil {
		return nil, fmt.Errorf("send_exit: %w", err)
	}
	return textResult(fmt.Sprintf("exit signal sent to %s (reason: %s)", p.PID, reason)), nil
}

type processKillParams struct {
	PID string `json:"pid"`
}

func toolProcessKill(w gen.Process, params json.RawMessage) (any, error) {
	var p processKillParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	pid, err := parsePID(w.Node().Name(), w.Node().Creation(), p.PID)
	if err != nil {
		return nil, fmt.Errorf("invalid pid: %w", err)
	}

	if err := w.Node().Kill(pid); err != nil {
		return nil, fmt.Errorf("process_kill: %w", err)
	}
	return textResult(fmt.Sprintf("process %s killed", p.PID)), nil
}
