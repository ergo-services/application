package mcp

import (
	"encoding/json"
	"fmt"

	"ergo.services/ergo/gen"
)

// ToolHandler is the function signature for all tool implementations.
// It receives a gen.Process (the worker handling the request) for access
// to Node() API, Send(), Call(), Spawn(), etc.
type ToolHandler func(p gen.Process, params json.RawMessage) (any, error)

// ToolDefinition describes a tool for MCP tools/list response.
type ToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`

	handler ToolHandler // unexported, not serialized
}

type toolRegistry struct {
	tools []ToolDefinition
	index map[string]ToolHandler
}

func newToolRegistry() *toolRegistry {
	return &toolRegistry{
		index: make(map[string]ToolHandler),
	}
}

func (r *toolRegistry) register(def ToolDefinition) {
	def.InputSchema = injectNodeParam(def.InputSchema)
	r.tools = append(r.tools, def)
	r.index[def.Name] = def.handler
}

// injectNodeParam adds the "node" property to a tool's JSON schema.
// This enables cluster proxy: when "node" is specified and differs from
// the local node, the request is forwarded to the remote node's MCP pool.
func injectNodeParam(schema json.RawMessage) json.RawMessage {
	var s map[string]json.RawMessage
	if err := json.Unmarshal(schema, &s); err != nil {
		return schema
	}

	propsRaw, ok := s["properties"]
	if ok == false {
		return schema
	}

	var props map[string]json.RawMessage
	if err := json.Unmarshal(propsRaw, &props); err != nil {
		return schema
	}

	// don't override if tool already defines "node"
	if _, exists := props["node"]; exists {
		return schema
	}

	props["node"] = json.RawMessage(`{"type":"string","description":"Remote node name for cluster proxy (e.g. 'backend@host'). Omit for local node"}`)

	newProps, err := json.Marshal(props)
	if err != nil {
		return schema
	}
	s["properties"] = newProps

	result, err := json.Marshal(s)
	if err != nil {
		return schema
	}
	return result
}

func (r *toolRegistry) list() []ToolDefinition {
	return r.tools
}

func (r *toolRegistry) filter(allowed []string) {
	if len(allowed) == 0 {
		return
	}
	keep := make(map[string]bool, len(allowed))
	for _, name := range allowed {
		keep[name] = true
	}
	var filtered []ToolDefinition
	for _, def := range r.tools {
		if keep[def.Name] {
			filtered = append(filtered, def)
		} else {
			delete(r.index, def.Name)
		}
	}
	r.tools = filtered
}

func (r *toolRegistry) dispatch(p gen.Process, name string, params json.RawMessage) (any, error) {
	h, ok := r.index[name]
	if ok == false {
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
	return h(p, params)
}
