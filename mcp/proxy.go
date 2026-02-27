package mcp

import (
	"encoding/json"
)

// extractNodeParam extracts the "node" field from JSON tool params.
// Returns empty string if not present.
func extractNodeParam(params json.RawMessage) string {
	if len(params) == 0 {
		return ""
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(params, &m); err != nil {
		return ""
	}
	nodeRaw, ok := m["node"]
	if ok == false {
		return ""
	}
	var node string
	if err := json.Unmarshal(nodeRaw, &node); err != nil {
		return ""
	}
	return node
}
