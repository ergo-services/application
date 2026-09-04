package observer

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"ergo.services/ergo/act"
	"ergo.services/ergo/gen"
)

func factory_post_worker() gen.ProcessBehavior {
	return &postWorker{}
}

type postWorker struct {
	act.WebWorker
}

func (w *postWorker) Init(args ...any) error {
	return nil
}

func (w *postWorker) HandlePost(from gen.PID, writer http.ResponseWriter, request *http.Request) error {
	body, err := io.ReadAll(io.LimitReader(request.Body, 1<<20))
	if err != nil {
		writeJSON(writer, request, http.StatusBadRequest, apiResponse{Error: "read body failed"})
		return nil
	}

	path := request.URL.Path

	switch {
	case path == "/api/enroll":
		w.handleEnroll(writer, request, body)
		return nil
	case strings.HasPrefix(path, "/api/call/"):
		w.handleAPICall(writer, request, strings.TrimPrefix(path, "/api/call/"), body)
		return nil
	case path == "/mcp":
		w.handleMCP(writer, request)
		return nil
	}

	sessionID := request.Header.Get("X-Observer-Session")
	if sessionID == "" {
		writeJSON(writer, request, http.StatusUnauthorized, apiResponse{Error: "missing X-Observer-Session"})
		return nil
	}
	sessionName := gen.Atom("observer_session_" + sessionID)

	switch {
	case path == "/api/subscribe" || path == "/api/unsubscribe" || path == "/api/switch":
		w.handleCommand(writer, request, sessionName, path, body)
	case strings.HasPrefix(path, "/api/do/"):
		w.handleAction(writer, request, sessionName, strings.TrimPrefix(path, "/api/do/"), body)
	default:
		writeJSON(writer, request, http.StatusNotFound, apiResponse{Error: "not found"})
	}
	return nil
}

func (w *postWorker) handleEnroll(writer http.ResponseWriter, request *http.Request, body []byte) {
	var req struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSON(writer, request, http.StatusBadRequest, apiResponse{Error: "invalid JSON"})
		return
	}

	result, err := w.CallWithTimeout(managerName, EnrollRequest{Token: req.Token}, defaultCallTimeout)
	if err != nil {
		writeJSON(writer, request, http.StatusInternalServerError, apiResponse{Error: err.Error()})
		return
	}

	answer, ok := result.(EnrollResponse)
	if ok == false {
		writeJSON(writer, request, http.StatusInternalServerError, apiResponse{Error: "unexpected response"})
		return
	}
	switch {
	case answer.Burned:
		writeJSON(writer, request, http.StatusGone, apiResponse{Error: "the enrollment secret is spent"})
	case answer.Error != nil:
		writeJSON(writer, request, http.StatusForbidden, apiResponse{Error: answer.Error.Error()})
	default:
		writeJSON(writer, request, http.StatusOK, apiResponse{OK: true, Data: wireEnrolled{ClusterID: answer.ClusterID}})
	}
}

func (w *postWorker) handleAPICall(writer http.ResponseWriter, request *http.Request, action string, body []byte) {
	if action == "" {
		writeJSON(writer, request, http.StatusBadRequest, apiResponse{Error: "missing action"})
		return
	}

	identity, _ := identityOf(request)
	if identity.Subject == "" {
		writeJSON(writer, request, http.StatusForbidden,
			apiResponse{Error: "one-shot actions need an authorized listener"})
		return
	}

	var args map[string]any
	if err := json.Unmarshal(body, &args); err != nil {
		writeJSON(writer, request, http.StatusBadRequest, apiResponse{Error: "invalid JSON"})
		return
	}

	target := w.Node().Name()
	if node, _ := args["node"].(string); node != "" {
		target = gen.Atom(node)
	}
	if identity.Ceiling.AllowsNode(target) == false {
		writeJSON(writer, request, http.StatusForbidden,
			apiResponse{Error: fmt.Sprintf("node %s is not permitted here", target)})
		return
	}

	response := w.call(identity, target, action, args)
	if response.Error != "" {
		writeJSON(writer, request, http.StatusBadRequest, response)
		return
	}
	writeJSON(writer, request, http.StatusOK, response)
}

func (w *postWorker) call(identity Identity, target gen.Atom, action string, args map[string]any) apiResponse {
	if action == "cluster_info" {
		result, err := w.CallWithTimeout(
			gen.ProcessID{Name: clusterName, Node: w.Node().Name()}, RequestClusterInfo{}, defaultCallTimeout)
		if err != nil {
			return apiResponse{Error: fmt.Sprintf("cluster info: %s", err)}
		}
		info, ok := result.(ClusterInfo)
		if ok == false {
			return apiResponse{Error: fmt.Sprintf("unexpected response %T", result)}
		}
		return apiResponse{OK: true, Data: wireClusterFrom(info)}
	}

	req, capability, err := buildActionRequest(action, args)
	if err != nil {
		return apiResponse{Error: err.Error()}
	}
	if identity.Ceiling.Allows(capability) == false {
		w.Log().Warning("%q refused: %s is not permitted here", qualified(identity), capability)
		return apiResponse{Error: fmt.Sprintf("%s is not permitted here", capability)}
	}

	result, err := w.CallWithTimeout(gen.ProcessID{Name: plane(capability), Node: target}, req, defaultCallTimeout)
	if err != nil {
		return apiResponse{Error: fmt.Sprintf("action %s: %s", action, err)}
	}
	return actionResponse(result, identity.Ceiling)
}

func (w *postWorker) handleCommand(writer http.ResponseWriter, request *http.Request, session gen.Atom, path string, body []byte) {
	var req struct {
		Type   string         `json:"type"`
		Args   map[string]any `json:"args"`
		Node   string         `json:"node"`
		Cookie string         `json:"cookie"`
		Host   string         `json:"host"`
		Port   int            `json:"port"`
		TLS    bool           `json:"tls"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSON(writer, request, http.StatusBadRequest, apiResponse{Error: "invalid JSON"})
		return
	}

	identity, _ := identityOf(request)
	cmd := commandRequest{
		Command: strings.TrimPrefix(path, "/api/"),
		Type:    req.Type,
		Args:    req.Args,
		Subject: qualified(identity),
	}
	if cmd.Command == "switch" {
		if cmd.Args == nil {
			cmd.Args = make(map[string]any)
		}
		cmd.Args["node"] = req.Node
		if req.Cookie != "" {
			cmd.Args["Cookie"] = req.Cookie
		}
		if req.Host != "" {
			cmd.Args["Host"] = req.Host
		}
		if req.Port > 0 {
			cmd.Args["Port"] = float64(req.Port)
		}
		if req.TLS {
			cmd.Args["TLS"] = true
		}
	}

	result, err := w.CallWithTimeout(session, cmd, defaultCallTimeout)
	if err != nil {
		writeJSON(writer, request, http.StatusInternalServerError, apiResponse{Error: err.Error()})
		return
	}

	resp, ok := result.(apiResponse)
	if ok == false {
		writeJSON(writer, request, http.StatusInternalServerError, apiResponse{Error: "unexpected response"})
		return
	}
	if resp.Error != "" {
		writeJSON(writer, request, http.StatusBadRequest, resp)
		return
	}
	writeJSON(writer, request, http.StatusOK, resp)
}

func (w *postWorker) handleAction(writer http.ResponseWriter, request *http.Request, session gen.Atom, action string, body []byte) {
	if action == "" {
		writeJSON(writer, request, http.StatusBadRequest, apiResponse{Error: "missing action"})
		return
	}

	var args map[string]any
	if err := json.Unmarshal(body, &args); err != nil {
		writeJSON(writer, request, http.StatusBadRequest, apiResponse{Error: "invalid JSON"})
		return
	}

	identity, _ := identityOf(request)
	result, err := w.CallWithTimeout(session,
		actionRequest{Action: action, Args: args, Subject: qualified(identity)}, defaultCallTimeout)
	if err != nil {
		writeJSON(writer, request, http.StatusInternalServerError, apiResponse{Error: err.Error()})
		return
	}

	resp, ok := result.(apiResponse)
	if ok == false {
		writeJSON(writer, request, http.StatusInternalServerError, apiResponse{Error: "unexpected response"})
		return
	}
	if resp.Error != "" {
		writeJSON(writer, request, http.StatusBadRequest, resp)
		return
	}
	writeJSON(writer, request, http.StatusOK, resp)
}

func (w *postWorker) Terminate(reason error) {}

const gzipMinSize = 1024

func writeJSON(w http.ResponseWriter, r *http.Request, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Add("Vary", "Accept-Encoding")

	body, err := json.Marshal(v)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if len(body) < gzipMinSize || acceptsGzip(r) == false {
		w.WriteHeader(status)
		w.Write(body)
		return
	}

	zw, err := gzip.NewWriterLevel(w, gzip.BestSpeed)
	if err != nil {
		w.WriteHeader(status)
		w.Write(body)
		return
	}

	w.Header().Set("Content-Encoding", "gzip")
	w.WriteHeader(status)
	zw.Write(body)
	zw.Close()
}

func acceptsGzip(r *http.Request) bool {
	if r == nil {
		return false
	}
	accepted := false
	for _, encoding := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		name, params, _ := strings.Cut(encoding, ";")
		name = strings.ToLower(strings.TrimSpace(name))
		if name != "gzip" && name != "*" {
			continue
		}
		if encodingRefused(params) {
			if name == "gzip" {
				return false
			}
			continue
		}
		accepted = true
	}
	return accepted
}

func encodingRefused(params string) bool {
	for _, param := range strings.Split(params, ";") {
		key, value, found := strings.Cut(param, "=")
		if found == false || strings.ToLower(strings.TrimSpace(key)) != "q" {
			continue
		}
		quality, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		return err == nil && quality == 0
	}
	return false
}
