package mcp

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"ergo.services/ergo/act"
	"ergo.services/ergo/gen"
)

func factoryMCPWorker() gen.ProcessBehavior {
	return &MCPWorker{}
}

// MCPWorker handles HTTP tool requests and remote ToolCallRequests.
// Part of MCPPool. Uses act.WebWorker for automatic HTTP dispatch and Done() handling.
type MCPWorker struct {
	act.WebWorker
	registry *toolRegistry
	options  Options
}

func (w *MCPWorker) Init(args ...any) error {
	w.registry = args[0].(*toolRegistry)
	w.options = args[1].(Options)
	// Make registry accessible to tool handlers that need to spawn samplers
	w.SetEnv(gen.Env("mcp_registry"), w.registry)
	return nil
}

// HandlePost handles POST /mcp from the HTTP client via WebHandler -> Pool -> Worker.
// WebWorker calls Done() automatically after this returns.
func (w *MCPWorker) HandlePost(from gen.PID, writer http.ResponseWriter, request *http.Request) error {
	// Auth check
	if w.options.Token != "" {
		auth := request.Header.Get("Authorization")
		if auth != "Bearer "+w.options.Token {
			writer.WriteHeader(http.StatusUnauthorized)
			return nil
		}
	}

	// Read body
	body, err := io.ReadAll(io.LimitReader(request.Body, 1<<20))
	if err != nil {
		writeJSONRPCError(writer, nil, errInternalError, "failed to read request body")
		return nil
	}

	// Parse JSON-RPC
	var rpcReq jsonrpcRequest
	if err := json.Unmarshal(body, &rpcReq); err != nil {
		writeJSONRPCError(writer, nil, errParseError, "parse error")
		return nil
	}

	if rpcReq.JSONRPC != "2.0" {
		writeJSONRPCError(writer, rpcReq.ID, errInvalidRequest, "invalid JSON-RPC version")
		return nil
	}

	// Notifications
	if rpcReq.ID == nil && isNotification(rpcReq.Method) {
		writer.WriteHeader(http.StatusAccepted)
		return nil
	}

	// Dispatch by method
	switch rpcReq.Method {
	case "initialize":
		w.handleInitialize(writer, rpcReq)

	case "ping":
		writeJSON(writer, newSuccessResponse(rpcReq.ID, struct{}{}))

	case "tools/list":
		writeJSON(writer, newSuccessResponse(rpcReq.ID, toolsListResult{
			Tools: w.registry.list(),
		}))

	case "tools/call":
		w.handleToolsCall(writer, rpcReq, request)

	default:
		writeJSONRPCError(writer, rpcReq.ID, errMethodNotFound, "method not found: "+rpcReq.Method)
	}

	return nil
}

// HandleCall handles ToolCallRequest from remote MCPPool workers.
func (w *MCPWorker) HandleCall(from gen.PID, ref gen.Ref, request any) (any, error) {
	switch r := request.(type) {
	case ToolCallRequest:
		result, err := w.registry.dispatch(w, r.Tool, stringToRaw(r.Params))
		if err != nil {
			return ToolCallResponse{Error: err.Error()}, nil
		}
		b, merr := json.Marshal(result)
		if merr != nil {
			return ToolCallResponse{Error: merr.Error()}, nil
		}
		return ToolCallResponse{Result: string(b)}, nil
	}
	return nil, nil
}

func (w *MCPWorker) Terminate(reason error) {}

func (w *MCPWorker) handleInitialize(writer http.ResponseWriter, req jsonrpcRequest) {
	var p initializeParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &p); err != nil {
			writeJSONRPCError(writer, req.ID, errInvalidParams, "invalid initialize params")
			return
		}
	}

	result := initializeResult{
		ProtocolVersion: ProtocolVersion,
		Capabilities: serverCapabilities{
			Tools: &toolsCapability{},
		},
		ServerInfo: implementationInfo{
			Name:    "ergo-mcp",
			Version: Version,
		},
		Instructions: "Ergo Framework MCP server. Use tools/list to discover available inspection tools.",
	}
	writeJSON(writer, newSuccessResponse(req.ID, result))
}

func (w *MCPWorker) handleToolsCall(writer http.ResponseWriter, req jsonrpcRequest, httpReq *http.Request) {
	var p toolsCallParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		writeJSONRPCError(writer, req.ID, errInvalidParams, "invalid tools/call params")
		return
	}

	// Check for remote node
	targetNode := extractNodeParam(p.Arguments)

	if targetNode != "" && gen.Atom(targetNode) != w.Node().Name() {
		// Remote call -- proxy to remote MCPPool
		w.handleRemoteToolCall(writer, req, p, gen.Atom(targetNode))
		return
	}

	// Local call
	result, err := w.registry.dispatch(w, p.Name, p.Arguments)
	if err != nil {
		writeJSONRPCError(writer, req.ID, errInvalidParams, err.Error())
		return
	}
	writeJSON(writer, newSuccessResponse(req.ID, result))
}

func (w *MCPWorker) handleRemoteToolCall(writer http.ResponseWriter, req jsonrpcRequest, p toolsCallParams, targetNode gen.Atom) {
	// Proxy to remote MCPPool
	target := gen.ProcessID{Name: PoolName, Node: targetNode}
	result, err := w.CallWithTimeout(target, ToolCallRequest{
		Tool:   p.Name,
		Params: rawToString(p.Arguments),
	}, 30)

	if err != nil {
		writeJSONRPCError(writer, req.ID, errInternalError,
			fmt.Sprintf("remote call to %s failed: %s", targetNode, err))
		return
	}

	resp, ok := result.(ToolCallResponse)
	if ok == false {
		writeJSONRPCError(writer, req.ID, errInternalError, "unexpected response from remote node")
		return
	}

	if resp.Error != "" {
		writeJSONRPCError(writer, req.ID, errInternalError, resp.Error)
		return
	}

	// Return raw result
	var toolRes any
	if err := json.Unmarshal(stringToRaw(resp.Result), &toolRes); err != nil {
		writeJSONRPCError(writer, req.ID, errInternalError, "cannot decode remote result")
		return
	}
	writeJSON(writer, newSuccessResponse(req.ID, toolRes))
}
