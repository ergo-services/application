package observer

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"ergo.services/ergo/act"
	"ergo.services/ergo/gen"
)

func factory_post_worker() gen.ProcessBehavior {
	return &postWorker{}
}

// postWorker handles HTTP POST /api/* requests.
// Receives MessageWebRequest from WebHandler via Pool.
// Resolves session by registered name, Call to session, writes HTTP response.
// Done() called automatically by act.WebWorker.
type postWorker struct {
	act.WebWorker
}

func (w *postWorker) Init(args ...any) error {
	return nil
}

func (w *postWorker) HandlePost(from gen.PID, writer http.ResponseWriter, request *http.Request) error {
	sessionID := request.Header.Get("X-Observer-Session")
	if sessionID == "" {
		writeJSON(writer, http.StatusUnauthorized, apiResponse{Error: "missing X-Observer-Session"})
		return nil
	}

	body, err := io.ReadAll(io.LimitReader(request.Body, 1<<20))
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, apiResponse{Error: "read body failed"})
		return nil
	}

	sessionName := gen.Atom("observer_session_" + sessionID)
	path := request.URL.Path

	switch {
	case path == "/api/subscribe" || path == "/api/unsubscribe" || path == "/api/switch":
		w.handleCommand(writer, sessionName, path, body)
	case strings.HasPrefix(path, "/api/do/"):
		w.handleAction(writer, sessionName, strings.TrimPrefix(path, "/api/do/"), body)
	default:
		writeJSON(writer, http.StatusNotFound, apiResponse{Error: "not found"})
	}
	return nil
}

func (w *postWorker) handleCommand(writer http.ResponseWriter, session gen.Atom, path string, body []byte) {
	var req struct {
		Type string         `json:"type"`
		Args map[string]any `json:"args"`
		Node string         `json:"node"`
		// connect options (cookie, host, port, tls)
		Cookie string `json:"cookie"`
		Host   string `json:"host"`
		Port   int    `json:"port"`
		TLS    bool   `json:"tls"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSON(writer, http.StatusBadRequest, apiResponse{Error: "invalid JSON"})
		return
	}

	cmd := commandRequest{
		Command: strings.TrimPrefix(path, "/api/"),
		Type:    req.Type,
		Args:    req.Args,
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
		writeJSON(writer, http.StatusInternalServerError, apiResponse{Error: err.Error()})
		return
	}

	resp, ok := result.(apiResponse)
	if ok == false {
		writeJSON(writer, http.StatusInternalServerError, apiResponse{Error: "unexpected response"})
		return
	}
	if resp.Error != "" {
		writeJSON(writer, http.StatusBadRequest, resp)
		return
	}
	writeJSON(writer, http.StatusOK, resp)
}

func (w *postWorker) handleAction(writer http.ResponseWriter, session gen.Atom, action string, body []byte) {
	if action == "" {
		writeJSON(writer, http.StatusBadRequest, apiResponse{Error: "missing action"})
		return
	}

	var args map[string]any
	if err := json.Unmarshal(body, &args); err != nil {
		writeJSON(writer, http.StatusBadRequest, apiResponse{Error: "invalid JSON"})
		return
	}

	result, err := w.CallWithTimeout(session, actionRequest{Action: action, Args: args}, defaultCallTimeout)
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, apiResponse{Error: err.Error()})
		return
	}

	resp, ok := result.(apiResponse)
	if ok == false {
		writeJSON(writer, http.StatusInternalServerError, apiResponse{Error: "unexpected response"})
		return
	}
	if resp.Error != "" {
		writeJSON(writer, http.StatusBadRequest, resp)
		return
	}
	writeJSON(writer, http.StatusOK, resp)
}

func (w *postWorker) Terminate(reason error) {}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
