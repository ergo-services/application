package mcp

import (
	"encoding/json"
)

// proxyParams holds generic proxy parameters extracted from tool arguments.
type proxyParams struct {
	Node    string
	Timeout int
}

// extractProxyParams extracts "node" and "timeout" fields from JSON tool params.
func extractProxyParams(params json.RawMessage) proxyParams {
	if len(params) == 0 {
		return proxyParams{}
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(params, &m); err != nil {
		return proxyParams{}
	}
	var pp proxyParams
	if nodeRaw, ok := m["node"]; ok {
		json.Unmarshal(nodeRaw, &pp.Node)
	}
	if timeoutRaw, ok := m["timeout"]; ok {
		json.Unmarshal(timeoutRaw, &pp.Timeout)
	}
	return pp
}
