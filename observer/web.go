package observer

import (
	"embed"
	"errors"
	"io/fs"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"ergo.services/ergo/act"
	"ergo.services/ergo/gen"
	"ergo.services/ergo/meta"
	"ergo.services/meta/sse"
)

//go:embed web/*
var assets embed.FS

func factory_web() gen.ProcessBehavior {
	return &web{}
}

// web owns the HTTP frontend. The mux and the listener are built once and never
// rebuilt; the metas behind /sse and /api/ are monitored and restarted on death,
// swapped into stable slots so routing and the listener stay up. SSE
// connect/disconnect goes to managerName via ProcessPool.
type web struct {
	act.Actor

	host string
	port uint16

	mux      *http.ServeMux
	sseSlot  *swapHandler
	postSlot *swapHandler

	sseAlias    gen.Alias
	postAlias   gen.Alias
	serverAlias gen.Alias
}

// messageRestartServer is a self-message scheduled to retry the listener rebind
// when the previous web server meta has not released the port yet.
type messageRestartServer struct{}

func (w *web) Init(args ...any) error {
	w.Log().SetLogger("default")

	v, _ := w.Env("port")
	port, _ := v.(uint16)
	if port < 1 {
		return errors.New("port is not set")
	}
	w.port = port

	w.host = "localhost"
	if v, exist := w.Env("host"); exist {
		if h, ok := v.(string); ok && h != "" {
			w.host = h
		}
	}

	// mux and the two stable slots are registered once. A restarted handler meta
	// is swapped into its slot, so the mux is never re-registered.
	w.mux = http.NewServeMux()
	w.sseSlot = &swapHandler{}
	w.postSlot = &swapHandler{}
	w.mux.Handle("/sse", w.sseSlot)
	w.mux.Handle("/api/", w.postSlot)

	// static frontend assets with pre-compressed gzip support
	fsroot, _ := fs.Sub(assets, "web")
	w.mux.HandleFunc("/", gzipFileServer(fsroot))

	if err := w.startSSE(); err != nil {
		return err
	}
	if err := w.startPost(); err != nil {
		return err
	}
	if err := w.startServer(); err != nil {
		return err
	}

	w.Log().Info("Observer listening on %s:%d", w.host, w.port)
	return nil
}

func (w *web) HandleMessage(from gen.PID, message any) error {
	switch msg := message.(type) {
	case gen.MessageDownAlias:
		w.restart(msg.Alias, msg.Reason)

	case messageRestartServer:
		w.restartServer()

	default:
		w.Log().Warning("unknown message from %s: %#v", from, message)
	}
	return nil
}

// restart re-spawns the meta whose alias just went down. A down for an alias we
// have already replaced no longer matches and is ignored.
func (w *web) restart(alias gen.Alias, reason error) {
	switch alias {
	case w.sseAlias:
		w.Log().Warning("SSE handler meta %s died (%s), restarting", alias, reason)
		if err := w.startSSE(); err != nil {
			w.Log().Error("failed to restart SSE handler: %s", err)
		}

	case w.postAlias:
		w.Log().Warning("API handler meta %s died (%s), restarting", alias, reason)
		if err := w.startPost(); err != nil {
			w.Log().Error("failed to restart API handler: %s", err)
		}

	case w.serverAlias:
		w.Log().Warning("web server meta %s died (%s), restarting", alias, reason)
		w.restartServer()
	}
}

// startSSE spawns a fresh SSE handler meta, monitors it, and swaps it into the
// /sse slot. The previous (dead) object is single-use and is dropped.
func (w *web) startSSE() error {
	handler := sse.CreateHandler(sse.HandlerOptions{
		ProcessPool: []gen.Atom{managerName},
		Compression: true,
	})
	alias, err := w.SpawnMeta(handler, gen.MetaOptions{})
	if err != nil {
		return err
	}
	if err := w.MonitorAlias(alias); err != nil {
		return err
	}
	w.sseAlias = alias
	w.sseSlot.set(handler)
	return nil
}

// startPost spawns a fresh API handler meta, monitors it, and swaps it into the
// /api/ slot.
func (w *web) startPost() error {
	handler := meta.CreateWebHandler(meta.WebHandlerOptions{
		Worker:         poolName,
		RequestTimeout: 15 * time.Second,
	})
	alias, err := w.SpawnMeta(handler, gen.MetaOptions{})
	if err != nil {
		return err
	}
	if err := w.MonitorAlias(alias); err != nil {
		return err
	}
	w.postAlias = alias
	w.postSlot.set(handler)
	return nil
}

// startServer binds the listener and spawns a fresh web server meta serving the
// stable mux. Returns an error if the port is still held by the previous server.
func (w *web) startServer() error {
	server, err := meta.CreateWebServer(meta.WebServerOptions{
		Port:    w.port,
		Host:    w.host,
		Handler: w.mux,
	})
	if err != nil {
		return err
	}
	alias, err := w.SpawnMeta(server, gen.MetaOptions{})
	if err != nil {
		return err
	}
	if err := w.MonitorAlias(alias); err != nil {
		return err
	}
	w.serverAlias = alias
	return nil
}

// restartServer rebinds the listener; if the previous server has not released
// the port yet, it retries shortly via a self-message.
func (w *web) restartServer() {
	if err := w.startServer(); err != nil {
		w.Log().Error("web server rebind failed, retrying: %s", err)
		w.SendAfter(w.PID(), messageRestartServer{}, time.Second)
	}
}

// swapHandler is the stable http.Handler registered in the mux for an endpoint
// whose backing meta can be restarted. ServeHTTP runs on the web server
// goroutine while the actor swaps the target from its callback, so the target is
// held in an atomic.Value: a lock-free store that never blocks the actor.
type swapHandler struct {
	current atomic.Value // holds handlerBox
}

type handlerBox struct {
	h http.Handler
}

func (s *swapHandler) set(h http.Handler) {
	s.current.Store(handlerBox{h: h})
}

func (s *swapHandler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	box, ok := s.current.Load().(handlerBox)
	if ok == false || box.h == nil {
		http.Error(writer, "handler not ready", http.StatusServiceUnavailable)
		return
	}
	box.h.ServeHTTP(writer, request)
}

// gzipFileServer serves pre-compressed .gz files when client supports gzip.
// Falls back to index.html for SPA routing.
func gzipFileServer(fsys fs.FS) http.HandlerFunc {
	contentTypes := map[string]string{
		".js":   "application/javascript",
		".css":  "text/css",
		".html": "text/html",
		".svg":  "image/svg+xml",
		".json": "application/json",
	}

	return func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}

		// try .gz file (pre-compressed at build time, originals removed)
		gzPath := path + ".gz"
		if data, err := fs.ReadFile(fsys, gzPath); err == nil {
			ext := filepath.Ext(path)
			if ct, ok := contentTypes[ext]; ok {
				w.Header().Set("Content-Type", ct)
			}
			w.Header().Set("Content-Encoding", "gzip")
			w.Header().Set("Vary", "Accept-Encoding")
			w.Write(data)
			return
		}

		// try original file (images, fonts, etc., not gzipped)
		if data, err := fs.ReadFile(fsys, path); err == nil {
			ext := filepath.Ext(path)
			if ct, ok := contentTypes[ext]; ok {
				w.Header().Set("Content-Type", ct)
			}
			w.Write(data)
			return
		}

		// SPA fallback: serve index.html.gz
		if data, err := fs.ReadFile(fsys, "index.html.gz"); err == nil {
			w.Header().Set("Content-Type", "text/html")
			w.Header().Set("Content-Encoding", "gzip")
			w.Header().Set("Vary", "Accept-Encoding")
			w.Write(data)
			return
		}

		http.Error(w, "not found", http.StatusNotFound)
	}
}
