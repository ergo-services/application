package observer

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ergo.services/ergo/meta"
	"ergo.services/meta/sse"
)

func TestStreamsRefusesOverTheLimit(t *testing.T) {
	hold := make(chan struct{})
	entered := make(chan struct{}, 1)
	layer := &streams{
		limit:            1,
		maxSubscriptions: 7,
		live:             &streamCounters{},
		next: http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
			entered <- struct{}{}
			<-hold
		}),
	}

	done := make(chan struct{})
	go func() {
		layer.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/sse", nil))
		close(done)
	}()
	<-entered

	second := httptest.NewRecorder()
	layer.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "/sse", nil))
	if second.Code != http.StatusTooManyRequests {
		t.Errorf("the second stream got %d, want 429", second.Code)
	}
	if second.Header().Get("Retry-After") == "" {
		t.Error("a refusal without Retry-After tells the client nothing")
	}
	if layer.live.refused.Load() != 1 {
		t.Errorf("refusals counted: %d", layer.live.refused.Load())
	}
	if layer.live.open.Load() != 1 {
		t.Errorf("a refused stream was counted as open: %d", layer.live.open.Load())
	}

	close(hold)
	<-done
	if layer.live.open.Load() != 0 {
		t.Errorf("open streams after the last one closed: %d", layer.live.open.Load())
	}
}

func TestStreamsCarriesTheSubscriptionLimit(t *testing.T) {
	got := 0
	layer := &streams{
		limit:            4,
		maxSubscriptions: 11,
		live:             &streamCounters{},
		next: http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
			got = maxSubscriptionsOf(request)
		}),
	}

	layer.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/sse", nil))
	if got != 11 {
		t.Errorf("the manager would see %d as the limit", got)
	}
}

func originCall(handler http.Handler, origin string, host string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/do/kill", nil)
	request.Host = host
	if origin != "" {
		request.Header.Set("Origin", origin)
	}
	handler.ServeHTTP(recorder, request)
	return recorder
}

// CORS guards the answer, not the act: without a server-side check a page from anywhere
// could drive a mutation and only be denied the reply
func TestOriginGuard(t *testing.T) {
	reached := 0
	refused := &refusals{}
	handler := originGuard{
		origins: []string{cloudOrigin},
		host:    "localhost",
		port:    9911,
		refused: refused,
		next:    http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) { reached++ }),
	}

	cases := []struct {
		name   string
		origin string
		pass   bool
	}{
		{"no origin at all is not a browser", "", true},
		{"the page this listener serves", "http://localhost:9911", true},
		{"an allowed origin", cloudOrigin, true},
		{"anywhere else", "https://evil.example", false},
		{"own host over the wrong scheme", "https://localhost:9911", false},
		{"own host without the port", "http://localhost", false},
	}

	for _, c := range cases {
		before := reached
		answer := originCall(handler, c.origin, "localhost:9911")
		switch {
		case c.pass && reached != before+1:
			t.Errorf("%s: refused with %d", c.name, answer.Code)
		case c.pass == false && reached != before:
			t.Errorf("%s: passed through", c.name)
		case c.pass == false && answer.Code != http.StatusForbidden:
			t.Errorf("%s: answered %d, want 403", c.name, answer.Code)
		}
	}

	if refused.count.Load() != 3 {
		t.Errorf("refusals counted: %d, want 3", refused.count.Load())
	}
}

func TestOriginGuardRefusesAReboundHost(t *testing.T) {
	reached := false
	handler := originGuard{
		host:    "localhost",
		port:    9911,
		refused: &refusals{},
		next:    http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) { reached = true }),
	}

	for _, host := range []string{"evil.example", "evil.example:9911", "localhost:9999"} {
		reached = false
		answer := originCall(handler, "http://"+host, host)
		if reached {
			t.Errorf("Host %q was answered as same-origin", host)
		}
		if answer.Code != http.StatusForbidden {
			t.Errorf("Host %q answered %d, want 403", host, answer.Code)
		}
	}

	// the name it was configured with still serves its own page
	reached = false
	if originCall(handler, "http://localhost:9911", "localhost:9911"); reached == false {
		t.Error("the listener refused its own page")
	}
}

// an empty AllowedOrigins means same-origin only, and that must still serve the bundle
func TestOriginGuardWithoutAllowedOrigins(t *testing.T) {
	reached := false
	handler := originGuard{
		host:    "localhost",
		port:    9911,
		refused: &refusals{},
		next:    http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) { reached = true }),
	}

	if answer := originCall(handler, "http://localhost:9911", "localhost:9911"); reached == false {
		t.Errorf("the listener refused its own page: %d", answer.Code)
	}
	reached = false
	if answer := originCall(handler, cloudOrigin, "localhost:9911"); reached {
		t.Errorf("an undeclared origin passed: %d", answer.Code)
	}
}

// the fallback answers every unknown path with index.html, so a POST must not look accepted
func TestStaticServesReadsOnly(t *testing.T) {
	counts := &refusalCounts{}
	handler := gzipFileServer(nil, counts)

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		recorder := httptest.NewRecorder()
		handler(recorder, httptest.NewRequest(method, "/anything", nil))
		if recorder.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s got %d, want 405", method, recorder.Code)
		}
		if recorder.Header().Get("Allow") == "" {
			t.Errorf("%s: a 405 without Allow", method)
		}
	}

	if counts.method.Load() != 3 {
		t.Errorf("refusals counted: %d, want 3", counts.method.Load())
	}
}

func TestInspectShowsTheLimits(t *testing.T) {
	counts := &refusalCounts{}
	counts.note(http.StatusUnauthorized)
	counts.note(http.StatusTooManyRequests)
	counts.note(http.StatusTooManyRequests)
	if got := counts.String(); got != "401=1 403=0 429=2 405=0" {
		t.Errorf("refusals render as %q", got)
	}

	w := &web{
		listener: withLimits(Listener{Port: 9911}),
		streams:  &streams{limit: defaultMaxStreams, live: &streamCounters{}},
	}
	w.streams.live.open.Store(3)
	w.streams.live.peak.Store(7)
	w.streams.live.refused.Store(2)

	want := "3/64 open, peak 7, refused 2, subscriptions 128"
	if got := w.describeStreams(); got != want {
		t.Errorf("streams render as %q, want %q", got, want)
	}

	empty := &web{listener: Listener{}}
	if got := empty.describeStreams(); got != "none" {
		t.Errorf("a listener without the layer renders as %q", got)
	}
}

func TestJobRetentionIsClamped(t *testing.T) {
	cases := []struct {
		name    string
		ceiling time.Duration
		asked   float64
		want    time.Duration
	}{
		{"nothing asked takes the ceiling", 10 * time.Minute, 0, 10 * time.Minute},
		{"less than the ceiling is granted", 10 * time.Minute, 60, time.Minute},
		{"more than the ceiling is clamped", 10 * time.Minute, 36000, 10 * time.Minute},
		{"no ceiling configured falls back", 0, 36000, defaultJobMaxRetention},
		{"a negative ask takes the ceiling", 10 * time.Minute, -5, 10 * time.Minute},
	}

	for _, c := range cases {
		got := clampRetention(c.ceiling, map[string]any{"retain": c.asked})
		if got != c.want {
			t.Errorf("%s: granted %s, want %s", c.name, got, c.want)
		}
	}

	spec := jobSpec{retain: clampRetention(time.Minute, map[string]any{"retain": float64(36000)})}
	if spec.retain != time.Minute {
		t.Errorf("the spec carries %s", spec.retain)
	}
}

func TestListenerStreamLimits(t *testing.T) {
	single, err := (Options{}).listeners()
	if err != nil {
		t.Fatalf("defaults refused: %s", err)
	}
	l := single[0]
	if l.MaxStreams != defaultMaxStreams || l.MaxSubscriptions != defaultMaxSubscriptions {
		t.Errorf("stream limits came out as %d/%d", l.MaxStreams, l.MaxSubscriptions)
	}
	explicit := Listener{Port: 9912, MaxStreams: 2, MaxSubscriptions: 3}
	resolved, err := (Options{Listeners: []Listener{explicit}}).listeners()
	if err != nil {
		t.Fatalf("explicit limits refused: %s", err)
	}
	if resolved[0].MaxStreams != 2 || resolved[0].MaxSubscriptions != 3 {
		t.Errorf("explicit limits became %d/%d", resolved[0].MaxStreams, resolved[0].MaxSubscriptions)
	}
}

func TestSwapHandlerRefusesInTheDialectOfThePath(t *testing.T) {
	slot := &swapHandler{}

	for path, wants := range map[string]string{
		"/mcp":         mcpMimeJSON,
		"/api/do/kill": mcpMimeJSON,
		"/index.html":  "text/plain",
	} {
		answer := httptest.NewRecorder()
		slot.ServeHTTP(answer, httptest.NewRequest(http.MethodPost, path, nil))

		if answer.Code != http.StatusServiceUnavailable {
			t.Errorf("%s: status %d", path, answer.Code)
		}
		if strings.HasPrefix(answer.Header().Get("Content-Type"), wants) == false {
			t.Errorf("%s: answered %q, want %s", path, answer.Header().Get("Content-Type"), wants)
		}
	}

	reached := false
	slot.set(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) { reached = true }))
	slot.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/mcp", nil))
	if reached == false {
		t.Error("a handler in the slot was not called")
	}

	slot.set(nil)
	reached = false
	answer := httptest.NewRecorder()
	slot.ServeHTTP(answer, httptest.NewRequest(http.MethodPost, "/mcp", nil))
	if reached || answer.Code != http.StatusServiceUnavailable {
		t.Errorf("an emptied slot answered %d, reached=%v", answer.Code, reached)
	}
}

func TestMetaHandlersRefuseInTheDialectOfThePath(t *testing.T) {
	post := meta.CreateWebHandler(meta.WebHandlerOptions{Worker: poolName, Refusal: refuse})
	stream := sse.CreateHandler(sse.HandlerOptions{Refusal: refuse})

	for what, c := range map[string]struct {
		handler http.Handler
		path    string
		status  int
		reason  error
	}{
		"an unspawned post handler": {post, "/mcp", http.StatusServiceUnavailable,
			meta.ErrHandlerNotInitialized},
		"an unspawned stream handler": {stream, "/mcp", http.StatusServiceUnavailable,
			sse.ErrHandlerNotInitialized},
		"the same on the browser path": {post, "/api/do/kill", http.StatusServiceUnavailable,
			meta.ErrHandlerNotInitialized},
	} {
		answer := httptest.NewRecorder()
		c.handler.ServeHTTP(answer, httptest.NewRequest(http.MethodPost, c.path, nil))

		if answer.Code != c.status {
			t.Errorf("%s: status %d, want %d", what, answer.Code, c.status)
		}
		if got := answer.Header().Get("Content-Type"); strings.HasPrefix(got, mcpMimeJSON) == false {
			t.Errorf("%s: answered %q", what, got)
		}
		if strings.Contains(answer.Body.String(), c.reason.Error()) == false {
			t.Errorf("%s: the body says nothing about the cause: %s", what, answer.Body)
		}
	}

	bare := meta.CreateWebHandler(meta.WebHandlerOptions{Worker: poolName})
	answer := httptest.NewRecorder()
	bare.ServeHTTP(answer, httptest.NewRequest(http.MethodPost, "/mcp", nil))
	if got := answer.Header().Get("Content-Type"); strings.HasPrefix(got, "text/plain") == false {
		t.Errorf("a handler without a refusal answered %q", got)
	}
}
