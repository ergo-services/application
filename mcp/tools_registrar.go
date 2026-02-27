package mcp

import (
	"encoding/json"
	"fmt"

	"ergo.services/ergo/gen"
)

func registerRegistrarTools(r *toolRegistry) {
	r.register(ToolDefinition{
		Name:        "registrar_info",
		Description: "Returns registrar service information: server address, version, supported features (proxy, encryption, applications).",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {},
			"additionalProperties": false
		}`),
		handler: toolRegistrarInfo,
	})

	r.register(ToolDefinition{
		Name:        "registrar_resolve",
		Description: "Resolves a node name to connection routes using the registrar. Returns host:port combinations to reach the node.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"name": {
					"type": "string",
					"description": "Node name to resolve (e.g. 'node2@host')"
				}
			},
			"required": ["name"]
		}`),
		handler: toolRegistrarResolve,
	})

	r.register(ToolDefinition{
		Name:        "registrar_resolve_proxy",
		Description: "Resolves proxy routes for connecting to a node through intermediate proxy nodes.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"name": {
					"type": "string",
					"description": "Target node name to resolve proxy route for"
				}
			},
			"required": ["name"]
		}`),
		handler: toolRegistrarResolveProxy,
	})

	r.register(ToolDefinition{
		Name:        "registrar_resolve_app",
		Description: "Discovers which nodes in the cluster have a specific application loaded or running. Returns node names with deployment info.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"name": {
					"type": "string",
					"description": "Application name to search for across the cluster"
				}
			},
			"required": ["name"]
		}`),
		handler: toolRegistrarResolveApp,
	})

	r.register(ToolDefinition{
		Name:        "cluster_nodes",
		Description: "Returns a combined list of all known nodes: connected (from network), discovered (from registrar), and self. Provides a cluster-wide view of all nodes.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {},
			"additionalProperties": false
		}`),
		handler: toolClusterNodes,
	})
}

func toolRegistrarInfo(w gen.Process, params json.RawMessage) (any, error) {
	registrar, err := w.Node().Network().Registrar()
	if err != nil {
		return nil, fmt.Errorf("registrar not available: %w", err)
	}
	info := registrar.Info()
	text, err := marshalResult(info)
	if err != nil {
		return nil, err
	}
	return textResult(text), nil
}

type registrarResolveParams struct {
	Name string `json:"name"`
}

func toolRegistrarResolve(w gen.Process, params json.RawMessage) (any, error) {
	var p registrarResolveParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	registrar, err := w.Node().Network().Registrar()
	if err != nil {
		return nil, fmt.Errorf("registrar not available: %w", err)
	}

	routes, err := registrar.Resolver().Resolve(gen.Atom(p.Name))
	if err != nil {
		return nil, fmt.Errorf("registrar_resolve: %w", err)
	}
	text, err := marshalResult(routes)
	if err != nil {
		return nil, err
	}
	return textResult(text), nil
}

func toolRegistrarResolveProxy(w gen.Process, params json.RawMessage) (any, error) {
	var p registrarResolveParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	registrar, err := w.Node().Network().Registrar()
	if err != nil {
		return nil, fmt.Errorf("registrar not available: %w", err)
	}

	routes, err := registrar.Resolver().ResolveProxy(gen.Atom(p.Name))
	if err != nil {
		return nil, fmt.Errorf("registrar_resolve_proxy: %w", err)
	}
	text, err := marshalResult(routes)
	if err != nil {
		return nil, err
	}
	return textResult(text), nil
}

type registrarResolveAppParams struct {
	Name string `json:"name"`
}

func toolRegistrarResolveApp(w gen.Process, params json.RawMessage) (any, error) {
	var p registrarResolveAppParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	registrar, err := w.Node().Network().Registrar()
	if err != nil {
		return nil, fmt.Errorf("registrar not available: %w", err)
	}

	routes, err := registrar.Resolver().ResolveApplication(gen.Atom(p.Name))
	if err != nil {
		return nil, fmt.Errorf("registrar_resolve_app: %w", err)
	}
	text, err := marshalResult(routes)
	if err != nil {
		return nil, err
	}
	return textResult(text), nil
}

type clusterNode struct {
	Name   gen.Atom `json:"name"`
	Status string   `json:"status"` // "self", "connected", "discovered"
}

func toolClusterNodes(w gen.Process, params json.RawMessage) (any, error) {
	seen := make(map[gen.Atom]bool)
	var result []clusterNode

	// Self
	self := w.Node().Name()
	seen[self] = true
	result = append(result, clusterNode{Name: self, Status: "self"})

	// Connected nodes
	net := w.Node().Network()
	for _, name := range net.Nodes() {
		if seen[name] {
			continue
		}
		seen[name] = true
		result = append(result, clusterNode{Name: name, Status: "connected"})
	}

	// Discovered nodes from registrar (if available)
	registrar, err := net.Registrar()
	if err == nil {
		nodes, err := registrar.Nodes()
		if err == nil {
			for _, name := range nodes {
				if seen[name] {
					continue
				}
				seen[name] = true
				result = append(result, clusterNode{Name: name, Status: "discovered"})
			}
		}
	}

	text, err := marshalResult(result)
	if err != nil {
		return nil, err
	}
	return textResult(text), nil
}
