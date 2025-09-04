package observer

import (
	"embed"
	"errors"
	"io/fs"
	"net/http"

	"ergo.services/ergo/act"
	"ergo.services/ergo/gen"
	"ergo.services/ergo/meta"
	"ergo.services/meta/websocket"
)

//go:embed web/*
var assets embed.FS

func factory_web() gen.ProcessBehavior {
	return &web{}
}

type web struct {
	act.Actor
}

func (w *web) Init(args ...any) error {

	mux := http.NewServeMux()

	fsroot, _ := fs.Sub(assets, "web")
	mux.Handle("/assets/", http.FileServer(http.FS(fsroot)))
	mux.Handle("/console/", http.FileServer(http.FS(fsroot)))
	mux.Handle("/js/", http.FileServer(http.FS(fsroot)))
	index := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(wr http.ResponseWriter, r *http.Request) {
			r.URL.Path = "/"
			next.ServeHTTP(wr, r)
		})
	}
	mux.Handle("/", index(http.FileServer(http.FS(fsroot))))

	v, _ := w.Env("handlers")
	handlers, _ := v.([]gen.Atom)
	if len(handlers) == 0 {
		return errors.New("no handlers in the handlers pool")
	}
	v, _ = w.Env("port")
	port, _ := v.(uint16)
	if port < 1 {
		return errors.New("option 'port' is not set")
	}
	host := "localhost"
	if v, exist := w.Env("host"); exist {
		host, _ = v.(string)
	}

	wsopt := websocket.HandlerOptions{
		ProcessPool: handlers,
		CheckOrigin: func(r *http.Request) bool { return true },
	}
	wshandler := websocket.CreateHandler(wsopt)
	mopt := gen.MetaOptions{
		LogLevel: w.Log().Level(),
	}
	w.SpawnMeta(wshandler, mopt)
	mux.Handle("/ws", wshandler)

	// create and spawn web server meta process
	serverOptions := meta.WebServerOptions{
		Port:    port,
		Host:    host,
		Handler: mux,
	}

	webserver, err := meta.CreateWebServer(serverOptions)
	if err != nil {
		return err
	}
	if _, err := w.SpawnMeta(webserver, gen.MetaOptions{}); err != nil {
		return err
	}
	return nil
}
