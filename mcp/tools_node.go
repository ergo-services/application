package mcp

import (
	"encoding/json"
	"fmt"

	"ergo.services/ergo/gen"
)

func registerNodeTools(r *toolRegistry) {
	r.register(ToolDefinition{
		Name:        "node_info",
		Description: "Returns node information: name, uptime, version, process counts, memory usage, CPU time, registered names/aliases/events counts, event statistics, application counts.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {},
			"additionalProperties": false
		}`),
		handler: toolNodeInfo,
	})

	r.register(ToolDefinition{
		Name:        "node_env",
		Description: "Returns the node environment variables as key-value pairs.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {},
			"additionalProperties": false
		}`),
		handler: toolNodeEnv,
	})
}

func toolNodeInfo(w gen.Process, params json.RawMessage) (any, error) {
	info, err := w.Node().Info()
	if err != nil {
		return nil, fmt.Errorf("node.Info: %w", err)
	}
	text, err := marshalResult(info)
	if err != nil {
		return nil, err
	}
	return textResult(text), nil
}

func toolNodeEnv(w gen.Process, params json.RawMessage) (any, error) {
	env := w.Node().EnvList()
	result := make(map[string]string)
	for k, v := range env {
		result[string(k)] = fmt.Sprintf("%v", v)
	}
	text, err := marshalResult(result)
	if err != nil {
		return nil, err
	}
	return textResult(text), nil
}
