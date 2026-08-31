package observer

import (
	"embed"
	"errors"
	"fmt"
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

type web struct {
	act.Actor

	listener Listener
	refused  *refusals
	refusals *refusalCounts

	mux        *http.ServeMux
	sseSlot    *swapHandler
	postSlot   *swapHandler
	openSlot   *swapHandler
	listenSlot *swapHandler
	streams    *streams
	live       *streamCounters

	sseAlias    gen.Alias
	postAlias   gen.Alias
	openAlias   gen.Alias
	listenAlias gen.Alias
	serverAlias gen.Alias

	started time.Time
}

type messageRestartServer struct{}

func (w *web) Init(args ...any) error {
	w.Log().SetLogger("default")

	if len(args) == 0 {
		return errors.New("no listener to serve")
	}
	listener, ok := args[0].(Listener)
	if ok == false || listener.Port < 1 {
		return errors.New("port is not set")
	}
	w.listener = listener
	w.refused = &refusals{}
	w.refusals = &refusalCounts{}
	w.started = time.Now()

	w.mux = http.NewServeMux()
	w.sseSlot = &swapHandler{}
	w.postSlot = &swapHandler{}
	w.openSlot = &swapHandler{}
	w.listenSlot = &swapHandler{}

	if err := w.route(); err != nil {
		return err
	}
	if err := w.startServer(); err != nil {
		return err
	}

	w.Log().Info("listener %q %s:%d surfaces=%s authorizer=%s ceiling=%s origins=%d ratelimit=%d",
		w.listener.Name, w.listener.Host, w.listener.Port, strings.Join(w.surfaces(), ","),
		yesno(w.listener.Authorizer != nil), describeCeiling(w.listener.Ceiling),
		len(w.listener.AllowedOrigins), w.listener.RateLimit)
	return nil
}

func (w *web) route() error {
	guarded := func(next http.Handler, ceiling Ceiling) http.Handler {
		return guard{
			next:       throttle{next: next, limiter: newLimiter(w.listener.RateLimit), counts: w.refusals},
			ceiling:    ceiling,
			authorizer: w.listener.Authorizer,
			counts:     w.refusals,
		}
	}

	if err := w.startSSE(); err != nil {
		return err
	}
	if err := w.startPost(); err != nil {
		return err
	}
	w.live = &streamCounters{}
	w.streams = &streams{
		next:             w.sseSlot,
		limit:            w.listener.MaxStreams,
		maxSubscriptions: w.listener.MaxSubscriptions,
		counts:           w.refusals,
		live:             w.live,
	}
	w.mux.Handle("/sse", guarded(w.streams, w.listener.ceilingAPI()))
	w.mux.Handle("/api/", guarded(w.postSlot, w.listener.ceilingAPI()))

	w.mux.Handle("/api/capabilities", throttle{
		next:    capabilitiesHandler(w.listener, w.enrollment().Token != ""),
		limiter: newLimiter(w.listener.RateLimit),
		counts:  w.refusals,
	})

	if w.enrollment().Token != "" {
		if err := w.startOpen(); err != nil {
			return err
		}
		w.mux.Handle("/api/enroll", throttle{
			next:    w.openSlot,
			limiter: newLimiter(enrollRateLimit),
			counts:  w.refusals,
		})
	}

	if w.listener.MCP.Disable == false {
		if err := w.startListen(); err != nil {
			return err
		}
		gate := mcpGate{
			listen: listenGate{
				next: &streams{
					next:             w.listenSlot,
					limit:            w.listener.MaxStreams,
					maxSubscriptions: w.listener.MaxSubscriptions,
					counts:           w.refusals,
					live:             w.live,
				},
				listener: w.listener,
				counts:   w.refusals,
			},
			post:     w.postSlot,
			listener: w.listener,
			counts:   w.refusals,
		}
		w.mux.Handle("/mcp", guarded(gate, w.listener.ceilingMCP()))
	}

	if w.listener.UI.Disable == false {
		fsroot, _ := fs.Sub(assets, "web")
		w.mux.HandleFunc("/", gzipFileServer(fsroot, w.refusals))
	}
	return nil
}

func (w *web) enrollment() EnrollmentOptions {
	v, exist := w.Env(envEnrollment)
	if exist == false {
		return EnrollmentOptions{}
	}
	e, _ := v.(EnrollmentOptions)
	return e
}

func (w *web) surfaces() []string {
	names := []string{"api"}
	if w.listener.UI.Disable == false {
		names = append(names, "ui")
	}
	if w.listener.MCP.Disable == false {
		names = append(names, "mcp")
	}
	return names
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

const webInspectHelp = "summary keys: listener, address, tls, surfaces, authorizer, ceiling, " +
	"origins, origins_refused, ratelimit, streams, refusals, enrollment, uptime"

func (w *web) describeStreams() string {
	if w.streams == nil {
		return "none"
	}
	return fmt.Sprintf("%d/%d open, peak %d, refused %d, subscriptions %d",
		w.streams.live.open.Load(), w.listener.MaxStreams, w.streams.live.peak.Load(),
		w.streams.live.refused.Load(), w.listener.MaxSubscriptions)
}

func (w *web) HandleInspect(from gen.PID, item ...string) map[string]string {
	if len(item) == 0 {
		return map[string]string{
			"listener":        w.listener.Name,
			"address":         fmt.Sprintf("%s:%d", w.listener.Host, w.listener.Port),
			"tls":             yesno(w.listener.CertManager != nil),
			"surfaces":        strings.Join(w.surfaces(), ","),
			"authorizer":      yesno(w.listener.Authorizer != nil),
			"ceiling":         describeCeiling(w.listener.Ceiling),
			"origins":         strings.Join(w.listener.AllowedOrigins, ","),
			"origins_refused": w.refusedOrigins(),
			"ratelimit":       fmt.Sprintf("%d", w.listener.RateLimit),
			"streams":         w.describeStreams(),
			"refusals":        w.refusals.String(),
			"enrollment":      yesno(w.enrollment().Token != ""),
			"uptime":          inspectAge(w.started),
			"items":           "help",
		}
	}

	result := map[string]string{}
	for _, q := range item {
		if q == "help" {
			result["help"] = webInspectHelp
			continue
		}
		result[q] = "<unknown item>"
	}
	return result
}

func (w *web) restart(alias gen.Alias, reason error) {
	switch alias {
	case w.sseAlias:
		w.sseSlot.set(nil)
		w.Log().Warning("SSE handler meta %s died (%s), restarting", alias, reason)
		if err := w.startSSE(); err != nil {
			w.Log().Error("failed to restart SSE handler: %s", err)
		}

	case w.postAlias:
		w.postSlot.set(nil)
		w.Log().Warning("API handler meta %s died (%s), restarting", alias, reason)
		if err := w.startPost(); err != nil {
			w.Log().Error("failed to restart API handler: %s", err)
		}

	case w.openAlias:
		w.openSlot.set(nil)
		w.Log().Warning("open handler meta %s died (%s), restarting", alias, reason)
		if err := w.startOpen(); err != nil {
			w.Log().Error("failed to restart open handler: %s", err)
		}

	case w.listenAlias:
		w.listenSlot.set(nil)
		w.Log().Warning("MCP stream meta %s died (%s), restarting", alias, reason)
		if err := w.startListen(); err != nil {
			w.Log().Error("failed to restart the MCP stream handler: %s", err)
		}

	case w.serverAlias:
		w.Log().Warning("web server meta %s died (%s), restarting", alias, reason)
		w.restartServer()
	}
}

func (w *web) startSSE() error {
	handler := sse.CreateHandler(sse.HandlerOptions{
		ProcessPool: []gen.Atom{managerName},
		// A browser cannot see the keepalive comment, so it is sent as an event instead:
		// silence then means the stream is gone, whatever the socket claims. The beat is
		// skipped whenever anything else was written, and checked on a fixed tick, so a
		// live stream can still go quiet for twice this.
		Heartbeat:        streamHeartbeat,
		HeartbeatEvent:   streamHeartbeatEvent,
		Compression:      true,
		CompressionLevel: gen.CompressionDefault,
		MetaOptions:      gen.MetaOptions{MailboxSize: streamMailbox(w.listener.MaxSubscriptions)},
		Refusal:          refuse,
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

func (w *web) startListen() error {
	handler := sse.CreateHandler(sse.HandlerOptions{
		ProcessPool: []gen.Atom{managerName},
		MetaOptions: gen.MetaOptions{MailboxSize: streamMailbox(w.listener.MaxSubscriptions)},
		Refusal:     refuse,
	})
	alias, err := w.SpawnMeta(handler, gen.MetaOptions{})
	if err != nil {
		return err
	}
	if err := w.MonitorAlias(alias); err != nil {
		return err
	}
	w.listenAlias = alias
	w.listenSlot.set(handler)
	return nil
}

func (w *web) startPost() error {
	handler := meta.CreateWebHandler(meta.WebHandlerOptions{
		Worker:         poolName,
		RequestTimeout: 15 * time.Second,
		Refusal:        refuse,
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

func (w *web) startOpen() error {
	handler := meta.CreateWebHandler(meta.WebHandlerOptions{
		Worker:         poolName,
		RequestTimeout: 15 * time.Second,
		Refusal:        refuse,
	})
	alias, err := w.SpawnMeta(handler, gen.MetaOptions{})
	if err != nil {
		return err
	}
	if err := w.MonitorAlias(alias); err != nil {
		return err
	}
	w.openAlias = alias
	w.openSlot.set(handler)
	return nil
}

func (w *web) startServer() error {
	// both stay outside the guard: a preflight carries no credentials, and a refusal still
	// needs the headers the browser reads it through
	server, err := meta.CreateWebServer(meta.WebServerOptions{
		Port:        w.listener.Port,
		Host:        w.listener.Host,
		CertManager: w.listener.CertManager,
		Handler: originGuard{
			next:    cors{next: w.mux, origins: w.listener.AllowedOrigins},
			origins: w.listener.AllowedOrigins,
			host:    w.listener.Host,
			port:    w.listener.Port,
			refused: w.refused,
			log:     w.Log(),
		},
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

func (w *web) restartServer() {
	if err := w.startServer(); err != nil {
		w.Log().Error("web server rebind failed, retrying: %s", err)
		w.SendAfter(w.PID(), messageRestartServer{}, time.Second)
	}
}

// ServeHTTP runs on the web server goroutine while the actor swaps the target from its
// callback, so the target is held in an atomic.Value: never blocks the actor
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
		refuse(writer, request, http.StatusServiceUnavailable, nil)
		return
	}
	box.h.ServeHTTP(writer, request)
}

func gzipFileServer(fsys fs.FS, counts *refusalCounts) http.HandlerFunc {
	contentTypes := map[string]string{
		".js":   "application/javascript",
		".css":  "text/css",
		".html": "text/html",
		".svg":  "image/svg+xml",
		".json": "application/json",
	}

	// sending nothing is not "no caching": a browser then keeps index.html as long as it
	// likes, pinning itself to asset hashes that no longer exist
	setCache := func(w http.ResponseWriter, path string) {
		if strings.HasPrefix(path, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
	}

	serve := func(w http.ResponseWriter, path string, data []byte, compressed bool) {
		if ct, ok := contentTypes[filepath.Ext(path)]; ok {
			w.Header().Set("Content-Type", ct)
		}
		if compressed {
			w.Header().Set("Content-Encoding", "gzip")
			w.Header().Add("Vary", "Accept-Encoding")
		}
		setCache(w, path)
		w.Write(data)
	}

	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			counts.note(http.StatusMethodNotAllowed)
			refuse(w, r, http.StatusMethodNotAllowed, nil)
			return
		}

		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}

		if data, err := fs.ReadFile(fsys, path+".gz"); err == nil {
			serve(w, path, data, true)
			return
		}

		if data, err := fs.ReadFile(fsys, path); err == nil {
			serve(w, path, data, false)
			return
		}

		if data, err := fs.ReadFile(fsys, "index.html.gz"); err == nil {
			serve(w, "index.html", data, true)
			return
		}

		http.Error(w, "not found", http.StatusNotFound)
	}
}

func (w *web) refusedOrigins() string {
	if w.refused == nil {
		return "0"
	}
	count := w.refused.count.Load()
	last, _ := w.refused.last.Load().(string)
	if count == 0 {
		return "0"
	}
	return fmt.Sprintf("%d, last %s", count, last)
}
