package observer

import (
	"embed"
	"errors"
	"io/fs"
	"net/http"
	"path/filepath"
	"strings"
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

// web spawns all meta processes (SSE handler, WebHandler, WebServer).
// Does not handle messages — SSE connect/disconnect goes to mgr via ProcessPool.
type web struct {
	act.Actor
}

func (w *web) Init(args ...any) error {
	w.Log().SetLogger("default")

	v, _ := w.Env("port")
	port, _ := v.(uint16)
	if port < 1 {
		return errors.New("port is not set")
	}

	host := "localhost"
	if v, exist := w.Env("host"); exist {
		if h, ok := v.(string); ok && h != "" {
			host = h
		}
	}

	mux := http.NewServeMux()

	// SSE endpoint → all connect/disconnect go to mgrName
	sseHandler := sse.CreateHandler(sse.HandlerOptions{
		ProcessPool: []gen.Atom{mgrName},
	})
	if _, err := w.SpawnMeta(sseHandler, gen.MetaOptions{}); err != nil {
		return err
	}
	mux.Handle("/sse", sseHandler)

	// POST /api/* → WebHandler → post pool
	postHandler := meta.CreateWebHandler(meta.WebHandlerOptions{
		Worker:         poolName,
		RequestTimeout: 15 * time.Second,
	})
	if _, err := w.SpawnMeta(postHandler, gen.MetaOptions{}); err != nil {
		return err
	}
	mux.Handle("/api/", postHandler)

	// static frontend assets with pre-compressed gzip support
	fsroot, _ := fs.Sub(assets, "web")
	mux.HandleFunc("/", gzipFileServer(fsroot))

	// web server
	webserver, err := meta.CreateWebServer(meta.WebServerOptions{
		Port:    port,
		Host:    host,
		Handler: mux,
	})
	if err != nil {
		return err
	}
	if _, err := w.SpawnMeta(webserver, gen.MetaOptions{}); err != nil {
		return err
	}

	w.Log().Info("Observer listening on %s:%d", host, port)
	return nil
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

		// try original file (images, fonts, etc. — not gzipped)
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
