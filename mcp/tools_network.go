package mcp

import (
	"encoding/json"
	"fmt"
	"strings"

	"ergo.services/ergo/gen"
)

func registerNetworkTools(r *toolRegistry) {
	r.register(ToolDefinition{
		Name:        "network_info",
		Description: "Returns network configuration: mode, flags, max message size, acceptors, routes, proxy routes.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {},
			"additionalProperties": false
		}`),
		handler: toolNetworkInfo,
	})

	r.register(ToolDefinition{
		Name:        "network_nodes",
		Description: "Returns list of connected remote nodes with detailed info (name, uptime, connection uptime, version, pool size, messages/bytes in/out). Supports filtering.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"name": {
					"type": "string",
					"description": "Filter by node name (substring match)"
				},
				"min_uptime": {
					"type": "integer",
					"description": "Minimum node uptime in seconds"
				},
				"min_connection_uptime": {
					"type": "integer",
					"description": "Minimum connection age in seconds"
				},
				"min_messages_in": {
					"type": "integer",
					"description": "Minimum messages received from this node"
				},
				"min_messages_out": {
					"type": "integer",
					"description": "Minimum messages sent to this node"
				},
				"min_bytes_in": {
					"type": "integer",
					"description": "Minimum bytes received from this node"
				},
				"min_bytes_out": {
					"type": "integer",
					"description": "Minimum bytes sent to this node"
				}
			}
		}`),
		handler: toolNetworkNodes,
	})

	r.register(ToolDefinition{
		Name:        "network_node_info",
		Description: "Returns detailed info about a connected remote node: version, handshake/proto version, flags, pool size/DSN, max message size, messages/bytes in/out, uptime.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"name": {
					"type": "string",
					"description": "Remote node name (e.g. 'node2@host')"
				}
			},
			"required": ["name"]
		}`),
		handler: toolNetworkNodeInfo,
	})

	r.register(ToolDefinition{
		Name:        "network_acceptors",
		Description: "Returns list of network acceptors with their configuration (host, port, flags, TLS).",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {},
			"additionalProperties": false
		}`),
		handler: toolNetworkAcceptors,
	})

	r.register(ToolDefinition{
		Name:        "network_connect",
		Description: "Connect to a remote node by name. Uses existing routes and registrar for discovery. If already connected, returns the existing connection info.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"name": {
					"type": "string",
					"description": "Remote node name (e.g. 'node2@host')"
				}
			},
			"required": ["name"]
		}`),
		handler: toolNetworkConnect,
	})

	r.register(ToolDefinition{
		Name:        "network_connect_route",
		Description: "Connect to a remote node using a custom route (host:port). Useful when the node is not discoverable via registrar or static routes.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"name": {
					"type": "string",
					"description": "Remote node name (e.g. 'node2@host')"
				},
				"host": {
					"type": "string",
					"description": "Target host or IP address"
				},
				"port": {
					"type": "integer",
					"description": "Target TCP port"
				},
				"tls": {
					"type": "boolean",
					"description": "Use TLS (default: false)"
				},
				"cookie": {
					"type": "string",
					"description": "Authentication cookie for this connection (uses node cookie if empty)"
				},
				"insecure_skip_verify": {
					"type": "boolean",
					"description": "Skip TLS certificate verification (default: false)"
				}
			},
			"required": ["name", "host", "port"]
		}`),
		handler: toolNetworkConnectRoute,
	})

	r.register(ToolDefinition{
		Name:        "network_disconnect",
		Description: "Disconnect from a remote node.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"name": {
					"type": "string",
					"description": "Remote node name to disconnect from"
				}
			},
			"required": ["name"]
		}`),
		handler: toolNetworkDisconnect,
	})
}

func toolNetworkInfo(w gen.Process, params json.RawMessage) (any, error) {
	net := w.Node().Network()
	info, err := net.Info()
	if err != nil {
		return nil, fmt.Errorf("network_info: %w", err)
	}
	text, err := marshalResult(info)
	if err != nil {
		return nil, err
	}
	return textResult(text), nil
}

type networkNodesParams struct {
	Name                 string `json:"name"`
	MinUptime            int64  `json:"min_uptime"`
	MinConnectionUptime  int64  `json:"min_connection_uptime"`
	MinMessagesIn        uint64 `json:"min_messages_in"`
	MinMessagesOut       uint64 `json:"min_messages_out"`
	MinBytesIn           uint64 `json:"min_bytes_in"`
	MinBytesOut          uint64 `json:"min_bytes_out"`
}

func toolNetworkNodes(w gen.Process, params json.RawMessage) (any, error) {
	var p networkNodesParams
	if len(params) > 0 {
		json.Unmarshal(params, &p)
	}

	net := w.Node().Network()
	nodes := net.Nodes()

	var result []gen.RemoteNodeInfo
	for _, name := range nodes {
		node, err := net.Node(name)
		if err != nil {
			continue
		}
		info := node.Info()

		// filters
		if p.Name != "" && strings.Contains(string(name), p.Name) == false {
			continue
		}
		if p.MinUptime > 0 && info.Uptime < p.MinUptime {
			continue
		}
		if p.MinConnectionUptime > 0 && info.ConnectionUptime < p.MinConnectionUptime {
			continue
		}
		if p.MinMessagesIn > 0 && info.MessagesIn < p.MinMessagesIn {
			continue
		}
		if p.MinMessagesOut > 0 && info.MessagesOut < p.MinMessagesOut {
			continue
		}
		if p.MinBytesIn > 0 && info.BytesIn < p.MinBytesIn {
			continue
		}
		if p.MinBytesOut > 0 && info.BytesOut < p.MinBytesOut {
			continue
		}

		result = append(result, info)
	}

	text, err := marshalResult(result)
	if err != nil {
		return nil, err
	}
	return textResult(text), nil
}

type networkNodeInfoParams struct {
	Name string `json:"name"`
}

func toolNetworkNodeInfo(w gen.Process, params json.RawMessage) (any, error) {
	var p networkNodeInfoParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	net := w.Node().Network()
	node, err := net.Node(gen.Atom(p.Name))
	if err != nil {
		return nil, fmt.Errorf("network_node_info: %w", err)
	}
	info := node.Info()
	text, err := marshalResult(info)
	if err != nil {
		return nil, err
	}
	return textResult(text), nil
}

func toolNetworkAcceptors(w gen.Process, params json.RawMessage) (any, error) {
	net := w.Node().Network()
	acceptors, err := net.Acceptors()
	if err != nil {
		return nil, fmt.Errorf("network_acceptors: %w", err)
	}

	var result []gen.AcceptorInfo
	for _, a := range acceptors {
		result = append(result, a.Info())
	}
	text, err := marshalResult(result)
	if err != nil {
		return nil, err
	}
	return textResult(text), nil
}

type networkConnectParams struct {
	Name string `json:"name"`
}

func toolNetworkConnect(w gen.Process, params json.RawMessage) (any, error) {
	var p networkConnectParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	net := w.Node().Network()
	node, err := net.GetNode(gen.Atom(p.Name))
	if err != nil {
		return nil, fmt.Errorf("network_connect to %s: %w", p.Name, err)
	}
	info := node.Info()
	text, err := marshalResult(info)
	if err != nil {
		return nil, err
	}
	return textResult(text), nil
}

type networkConnectRouteParams struct {
	Name               string `json:"name"`
	Host               string `json:"host"`
	Port               uint16 `json:"port"`
	TLS                bool   `json:"tls"`
	Cookie             string `json:"cookie"`
	InsecureSkipVerify bool   `json:"insecure_skip_verify"`
}

func toolNetworkConnectRoute(w gen.Process, params json.RawMessage) (any, error) {
	var p networkConnectRouteParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	route := gen.NetworkRoute{
		Route: gen.Route{
			Host: p.Host,
			Port: p.Port,
			TLS:  p.TLS,
		},
		Cookie:             p.Cookie,
		InsecureSkipVerify: p.InsecureSkipVerify,
	}

	net := w.Node().Network()
	node, err := net.GetNodeWithRoute(gen.Atom(p.Name), route)
	if err != nil {
		return nil, fmt.Errorf("network_connect_route to %s at %s:%d: %w", p.Name, p.Host, p.Port, err)
	}
	info := node.Info()
	text, err := marshalResult(info)
	if err != nil {
		return nil, err
	}
	return textResult(text), nil
}

type networkDisconnectParams struct {
	Name string `json:"name"`
}

func toolNetworkDisconnect(w gen.Process, params json.RawMessage) (any, error) {
	var p networkDisconnectParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	net := w.Node().Network()
	node, err := net.Node(gen.Atom(p.Name))
	if err != nil {
		return nil, fmt.Errorf("node %s not connected: %w", p.Name, err)
	}

	node.Disconnect()
	return textResult(fmt.Sprintf("disconnected from %s", p.Name)), nil
}
