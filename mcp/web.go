package mcp

import (
	"encoding/json"
	"fmt"
	"net/http"

	"ergo.services/ergo/act"
	"ergo.services/ergo/gen"
	"ergo.services/ergo/meta"
)

const WebName gen.Atom = "mcp_web"

func factoryMCPWeb() gen.ProcessBehavior {
	return &MCPWeb{}
}

// MCPWeb manages the web server for POST-only MCP endpoint.
type MCPWeb struct {
	act.Actor
	options Options
}

func (w *MCPWeb) Init(args ...any) error {
	w.options = args[0].(Options)

	if w.options.Port == 0 {
		w.Log().Info("MCP web: agent mode (no HTTP listener)")
		return nil
	}

	mux := http.NewServeMux()

	// POST /mcp -- WebHandler routes to Pool "mcp"
	postHandler := meta.CreateWebHandler(meta.WebHandlerOptions{
		Worker:         PoolName,
		RequestTimeout: 30 * 1e9, // 30 seconds for remote proxy calls
	})
	if _, err := w.SpawnMeta(postHandler, gen.MetaOptions{}); err != nil {
		return err
	}

	// POST-only endpoint
	mcpEndpoint := http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			postHandler.ServeHTTP(rw, r)
			return
		}
		http.Error(rw, "Method Not Allowed", http.StatusMethodNotAllowed)
	})
	mux.Handle("/mcp", mcpEndpoint)

	// Web server
	webserver, err := meta.CreateWebServer(meta.WebServerOptions{
		Port:        w.options.Port,
		Host:        w.options.Host,
		Handler:     mux,
		CertManager: w.options.CertManager,
	})
	if err != nil {
		return err
	}
	if _, err := w.SpawnMeta(webserver, gen.MetaOptions{}); err != nil {
		webserver.Terminate(err)
		return err
	}

	w.Log().Info("MCP web started at http://%s:%d/mcp", w.options.Host, w.options.Port)
	return nil
}

func (w *MCPWeb) HandleInspect(from gen.PID, item ...string) map[string]string {
	result := make(map[string]string)
	result["endpoint"] = fmt.Sprintf("http://%s:%d/mcp", w.options.Host, w.options.Port)
	b, _ := json.Marshal(w.options)
	result["options"] = string(b)
	return result
}

func (w *MCPWeb) Terminate(reason error) {
	w.Log().Debug("MCP web terminated: %s", reason)
}
