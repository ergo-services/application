package observer

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"ergo.services/ergo/act"
	"ergo.services/ergo/gen"
	"ergo.services/ergo/testing/stage"
)

type browser struct {
	t         *testing.T
	base      string
	sessionID string
	headers   map[string]string

	events chan streamed
	closed chan struct{}
}

type streamed struct {
	Event string
	Data  []byte
}

func factory_probe() gen.ProcessBehavior {
	return &probe{}
}

type probe struct {
	act.Actor
}

func freePort(t *testing.T) uint16 {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("no free port: %s", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	return uint16(port)
}

func openBrowser(t *testing.T, port uint16) *browser {
	t.Helper()
	return openBrowserAs(t, port, nil)
}

func openBrowserAs(t *testing.T, port uint16, headers map[string]string) *browser {
	t.Helper()

	b := &browser{
		t:       t,
		base:    fmt.Sprintf("http://localhost:%d", port),
		headers: headers,
		events:  make(chan streamed, 256),
		closed:  make(chan struct{}),
	}

	request, err := http.NewRequest(http.MethodGet, b.base+"/sse", nil)
	if err != nil {
		t.Fatalf("sse request: %s", err)
	}
	request.Header.Set("Accept", "text/event-stream")
	request.Header.Set("Accept-Encoding", "identity")
	for name, value := range b.headers {
		request.Header.Set(name, value)
	}

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("sse connect: %s", err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("sse answered %s", response.Status)
	}
	t.Cleanup(func() { response.Body.Close() })

	go func() {
		defer close(b.closed)

		var event string
		scanner := bufio.NewScanner(response.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			switch {
			case strings.HasPrefix(line, "event:"):
				event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			case strings.HasPrefix(line, "data:"):
				data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
				select {
				case b.events <- streamed{Event: event, Data: []byte(data)}:
				default:
				}
			}
		}
	}()

	intro := b.wait("connected", 5*time.Second)
	var payload struct {
		SessionID string `json:"SessionID"`
		Contract  int    `json:"Contract"`
	}
	if err := json.Unmarshal(intro, &payload); err != nil {
		t.Fatalf("connected payload: %s", err)
	}
	if payload.Contract != wireContractVersion {
		t.Fatalf("the browser was told contract %d, want %d", payload.Contract, wireContractVersion)
	}
	b.sessionID = payload.SessionID
	return b
}

func (b *browser) wait(event string, timeout time.Duration) []byte {
	b.t.Helper()

	deadline := time.After(timeout)
	for {
		select {
		case message := <-b.events:
			if message.Event == event {
				return message.Data
			}
		case <-deadline:
			b.t.Fatalf("no %q event within %s", event, timeout)
		case <-b.closed:
			b.t.Fatalf("the stream closed while waiting for %q", event)
		}
	}
}

func (b *browser) drain() {
	for {
		select {
		case <-b.events:
		default:
			return
		}
	}
}

func (b *browser) post(path string, body any) apiResponse {
	b.t.Helper()

	data, err := json.Marshal(body)
	if err != nil {
		b.t.Fatalf("marshal %s: %s", path, err)
	}

	request, err := http.NewRequest(http.MethodPost, b.base+path, bytes.NewReader(data))
	if err != nil {
		b.t.Fatalf("%s request: %s", path, err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Observer-Session", b.sessionID)
	for name, value := range b.headers {
		request.Header.Set(name, value)
	}

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		b.t.Fatalf("post %s: %s", path, err)
	}
	defer response.Body.Close()

	var answer apiResponse
	if err := json.NewDecoder(response.Body).Decode(&answer); err != nil {
		b.t.Fatalf("%s answer: %s", path, err)
	}
	return answer
}

func (b *browser) unsubscribeByHandle(handle string) {
	b.t.Helper()

	answer := b.post("/api/unsubscribe", map[string]any{"args": map[string]any{"key": handle}})
	if answer.OK == false {
		b.t.Fatalf("unsubscribe %s: %s", handle, answer.Error)
	}
}

func (b *browser) subscribe(subType string, args map[string]any) string {
	b.t.Helper()

	answer := b.post("/api/subscribe", map[string]any{"type": subType, "args": args})
	if answer.OK == false {
		b.t.Fatalf("subscribe %s: %s", subType, answer.Error)
	}
	data, ok := answer.Data.(map[string]any)
	if ok == false {
		b.t.Fatalf("subscribe %s answered %#v", subType, answer.Data)
	}
	handle, _ := data["key"].(string)
	if handle == "" {
		b.t.Fatalf("subscribe %s returned no handle: %#v", subType, data)
	}
	return handle
}

func TestSessionObservesRemoteNode(t *testing.T) {
	s := stage.New(t, stage.StageOptions{RegistrarFull: true})

	port := freePort(t)
	local := s.StartNode("obs", stage.NodeOptions{
		EnableSystemApp: true,
		Applications:    []gen.ApplicationBehavior{CreateApp(Options{Port: port, Host: "localhost"})},
	})
	remote := s.StartNode("peer", stage.NodeOptions{EnableSystemApp: true})

	target, err := remote.Native().Spawn(factory_probe, gen.ProcessOptions{})
	if err != nil {
		t.Fatalf("spawn the observed process: %s", err)
	}

	b := openBrowser(t, port)

	sessionPID, err := local.Native().ProcessPID(gen.Atom("observer_session_" + b.sessionID))
	if err != nil {
		t.Fatalf("no session process: %s", err)
	}
	inspectSession := func(item ...string) map[string]string {
		t.Helper()
		state, err := local.Native().Inspect(sessionPID, item...)
		if err != nil {
			t.Fatalf("inspect session: %s", err)
		}
		return state
	}

	if state := inspectSession(); state["observing_node"] != string(local.Name()) {
		t.Fatalf("a fresh session observes %q", state["observing_node"])
	}

	if answer := b.post("/api/switch", map[string]any{"node": string(remote.Name())}); answer.OK == false {
		t.Fatalf("switch to %s: %s", remote.Name(), answer.Error)
	}

	state := inspectSession()
	if state["observing_node"] != string(remote.Name()) {
		t.Fatalf("after the switch the session observes %q", state["observing_node"])
	}

	handle := b.subscribe("process_info", map[string]any{"pid": target})

	state = inspectSession()
	if state["handles"] == "0" {
		t.Fatal("the subscription is not in the bookkeeping")
	}
	if state["last_change"] == "never" {
		t.Error("a subscription was made, so the last change is known")
	}

	queried := inspectSession("handles", "events", "handle "+handle)
	if strings.Contains(queried["handles"], handle) == false {
		t.Errorf("the handle the browser holds is missing from %q", queried["handles"])
	}

	watched := queried["handle "+handle]
	if strings.Contains(watched, remote.Name().CRC32()) == false {
		t.Errorf("the session watches %q, which is not on %s", watched, remote.Name())
	}
	if strings.Contains(watched, local.Name().CRC32()) {
		t.Errorf("the session watches a local event: %q", watched)
	}
	if strings.Contains(queried["events"], remote.Name().CRC32()) == false {
		t.Errorf("events came out as %q", queried["events"])
	}

	info := b.wait("process_info", 10*time.Second)
	if bytes.Contains(info, []byte(remote.Name())) == false {
		t.Errorf("the pushed info does not belong to %s: %s", remote.Name(), info)
	}

	if err := remote.Native().Kill(target); err != nil {
		t.Fatalf("kill the observed process: %s", err)
	}

	var down wireSubscriptionDown
	if err := json.Unmarshal(b.wait("subscription_down", 10*time.Second), &down); err != nil {
		t.Fatalf("subscription_down payload: %s", err)
	}
	if len(down.Keys) != 1 || down.Keys[0] != handle {
		t.Errorf("the browser was told %v, want %q", down.Keys, handle)
	}
	if down.Type != "process_info" {
		t.Errorf("the type came out as %q", down.Type)
	}

	waitFor(t, 5*time.Second, "the session releases the handle", func() bool {
		return strings.Contains(inspectSession("handles")["handles"], handle) == false
	})

	final := inspectSession()
	if strings.Contains(final["dropped"], "message_unexpected") ||
		strings.Contains(final["dropped"], "sse_send_failed") {
		t.Errorf("the session dropped something: %q", final["dropped"])
	}
}

func TestSessionSwitchSurvivesStaleRelease(t *testing.T) {
	s := stage.New(t, stage.StageOptions{RegistrarFull: true})

	port := freePort(t)
	local := s.StartNode("obs", stage.NodeOptions{
		EnableSystemApp: true,
		Applications:    []gen.ApplicationBehavior{CreateApp(Options{Port: port, Host: "localhost"})},
	})
	remote := s.StartNode("peer", stage.NodeOptions{EnableSystemApp: true})

	b := openBrowser(t, port)
	sessionPID, err := local.Native().ProcessPID(gen.Atom("observer_session_" + b.sessionID))
	if err != nil {
		t.Fatalf("no session process: %s", err)
	}

	scope := map[string]any{"pidStart": 1000, "pidLimit": 50}
	stale := b.subscribe("process_list", scope)

	if answer := b.post("/api/switch", map[string]any{"node": string(remote.Name())}); answer.OK == false {
		t.Fatalf("switch to %s: %s", remote.Name(), answer.Error)
	}

	fresh := b.subscribe("process_list", scope)
	if fresh == stale {
		t.Fatalf("the handle survived the switch: %q", fresh)
	}
	b.unsubscribeByHandle(stale)
	b.drain()

	held, err := local.Native().Inspect(sessionPID, "handles")
	if err != nil {
		t.Fatalf("inspect session: %s", err)
	}
	if strings.Contains(held["handles"], fresh) == false {
		t.Fatalf("the fresh subscription is gone: %q", held["handles"])
	}

	b.wait("process_list", 10*time.Second)
	b.wait("process_list", 10*time.Second)
}

func TestSwitchRouteNeedsTheMutatingPlane(t *testing.T) {
	s := stage.New(t, stage.StageOptions{RegistrarFull: true})

	port := freePort(t)
	s.StartNode("obs", stage.NodeOptions{
		EnableSystemApp: true,
		Applications: []gen.ApplicationBehavior{CreateApp(Options{
			Port:    port,
			Host:    "localhost",
			Ceiling: Ceiling{ReadOnly: true},
		})},
	})
	remote := s.StartNode("peer", stage.NodeOptions{EnableSystemApp: true})

	b := openBrowser(t, port)

	refused := b.post("/api/switch", map[string]any{
		"node": string(remote.Name()),
		"host": "127.0.0.1",
		"port": 12345,
	})
	if refused.Error == "" {
		t.Fatalf("a read-only session dialled a route of its own choosing: %+v", refused)
	}
	if strings.Contains(refused.Error, CapDialRoute) == false {
		t.Fatalf("switch refused for the wrong reason: %q", refused.Error)
	}

	if cookie := b.post("/api/switch", map[string]any{
		"node":   string(remote.Name()),
		"cookie": "borrowed",
	}); cookie.Error == "" {
		t.Fatalf("a read-only session named the cookie: %+v", cookie)
	}

	if answer := b.post("/api/switch", map[string]any{"node": string(remote.Name())}); answer.OK == false {
		t.Fatalf("switch by the registrar route refused: %s", answer.Error)
	}
}

func TestSessionUnsubscribesByHandle(t *testing.T) {
	s := stage.New(t, stage.StageOptions{RegistrarFull: true})

	port := freePort(t)
	local := s.StartNode("obs", stage.NodeOptions{
		EnableSystemApp: true,
		Applications:    []gen.ApplicationBehavior{CreateApp(Options{Port: port, Host: "localhost"})},
	})

	target, err := local.Native().Spawn(factory_probe, gen.ProcessOptions{})
	if err != nil {
		t.Fatalf("spawn the observed process: %s", err)
	}

	b := openBrowser(t, port)
	sessionPID, err := local.Native().ProcessPID(gen.Atom("observer_session_" + b.sessionID))
	if err != nil {
		t.Fatalf("no session process: %s", err)
	}

	handle := b.subscribe("process_info", map[string]any{"pid": target})

	state, err := local.Native().Inspect(sessionPID, "handles")
	if err != nil {
		t.Fatalf("inspect session: %s", err)
	}
	if strings.Contains(state["handles"], handle) == false {
		t.Fatalf("the handle is not in the bookkeeping: %q", state["handles"])
	}

	b.unsubscribeByHandle(handle)

	waitFor(t, 5*time.Second, "the session releases the handle", func() bool {
		held, err := local.Native().Inspect(sessionPID, "handles")
		return err == nil && strings.Contains(held["handles"], handle) == false
	})

	summary, err := local.Native().Inspect(sessionPID)
	if err != nil {
		t.Fatalf("inspect session: %s", err)
	}
	if strings.Contains(summary["dropped"], "unsubscribe_unknown") {
		t.Errorf("the handle did not resolve: %q", summary["dropped"])
	}
}

func TestReadOnlyCeilingOverHTTP(t *testing.T) {
	s := stage.New(t, stage.StageOptions{RegistrarFull: true})

	port := freePort(t)
	local := s.StartNode("obs", stage.NodeOptions{
		EnableSystemApp: true,
		Applications: []gen.ApplicationBehavior{CreateApp(Options{
			Port:    port,
			Host:    "localhost",
			Ceiling: Ceiling{ReadOnly: true},
		})},
	})

	target, err := local.Native().Spawn(factory_probe, gen.ProcessOptions{})
	if err != nil {
		t.Fatalf("spawn the observed process: %s", err)
	}

	b := openBrowser(t, port)

	offered := b.post("/api/do/capabilities", map[string]any{})
	if offered.OK == false {
		t.Fatalf("capabilities: %s", offered.Error)
	}
	reported, ok := offered.Data.(map[string]any)
	if ok == false {
		t.Fatalf("capabilities answered %#v", offered.Data)
	}
	if manage, _ := reported["mng"].(bool); manage {
		t.Error("the mutating plane was offered by a read-only observer")
	}
	names, _ := reported["cap"].([]any)
	if len(names) == 0 {
		t.Fatal("no capability was reported at all")
	}
	for _, name := range names {
		if text, _ := name.(string); strings.HasPrefix(text, "manage.") {
			t.Errorf("a read-only observer offered %q", text)
		}
	}

	killed := b.post("/api/do/kill", map[string]any{"pid": target})
	if killed.OK {
		t.Fatal("a read-only observer killed a process")
	}
	if strings.Contains(killed.Error, "manage.kill") == false {
		t.Errorf("the refusal came out as %q, expected the capability in it", killed.Error)
	}
	if _, err := local.Native().ProcessInfo(target); err != nil {
		t.Errorf("the observed process did not survive the refused kill: %s", err)
	}

	if read := b.post("/api/do/inspect", map[string]any{"pid": target}); read.OK == false {
		t.Errorf("a read was refused: %s", read.Error)
	}
	if handle := b.subscribe("process_info", map[string]any{"pid": target}); handle == "" {
		t.Error("a subscription was refused")
	}

	sessionPID, err := local.Native().ProcessPID(gen.Atom("observer_session_" + b.sessionID))
	if err != nil {
		t.Fatalf("no session process: %s", err)
	}
	summary, err := local.Native().Inspect(sessionPID)
	if err != nil {
		t.Fatalf("inspect session: %s", err)
	}
	if strings.Contains(summary["dropped"], "ceiling_refused") == false {
		t.Errorf("the refusal was not counted: %q", summary["dropped"])
	}
}

func manageOffered(t *testing.T, b *browser) bool {
	t.Helper()

	answer := b.post("/api/do/capabilities", map[string]any{})
	if answer.OK == false {
		t.Fatalf("capabilities: %s", answer.Error)
	}
	reported, ok := answer.Data.(map[string]any)
	if ok == false {
		t.Fatalf("capabilities answered %#v", answer.Data)
	}
	for _, name := range reported["cap"].([]any) {
		if text, _ := name.(string); strings.HasPrefix(text, "manage.") {
			manage, _ := reported["mng"].(bool)
			if manage == false {
				t.Errorf("%q was offered while the plane was not", text)
			}
			return true
		}
	}
	if manage, _ := reported["mng"].(bool); manage {
		t.Error("the plane was offered with no mutating capability in the list")
	}
	return false
}

func TestListenersServeDifferentCeilings(t *testing.T) {
	s := stage.New(t, stage.StageOptions{RegistrarFull: true})

	full, readOnly := freePort(t), freePort(t)
	local := s.StartNode("obs", stage.NodeOptions{
		EnableSystemApp: true,
		Applications: []gen.ApplicationBehavior{CreateApp(Options{
			Listeners: []Listener{
				{Port: full},
				{Port: readOnly, Ceiling: Ceiling{ReadOnly: true}, UI: SurfaceUI{Disable: true}},
			},
		})},
	})

	target, err := local.Native().Spawn(factory_probe, gen.ProcessOptions{})
	if err != nil {
		t.Fatalf("spawn the observed process: %s", err)
	}

	privileged := openBrowser(t, full)
	if manageOffered(t, privileged) == false {
		t.Error("the full listener offered nothing to change")
	}

	restricted := openBrowser(t, readOnly)
	if manageOffered(t, restricted) {
		t.Error("the read-only listener offered a mutation")
	}
	if killed := restricted.post("/api/do/kill", map[string]any{"pid": target}); killed.OK {
		t.Error("the read-only listener killed a process")
	}
	if _, err := local.Native().ProcessInfo(target); err != nil {
		t.Errorf("the process did not survive: %s", err)
	}

	if killed := privileged.post("/api/do/kill", map[string]any{"pid": target}); killed.OK == false {
		t.Errorf("the full listener refused a kill: %s", killed.Error)
	}
}

func TestAuthorizerGuardsListener(t *testing.T) {
	s := stage.New(t, stage.StageOptions{RegistrarFull: true})

	port := freePort(t)
	local := s.StartNode("obs", stage.NodeOptions{
		EnableSystemApp: true,
		Applications: []gen.ApplicationBehavior{CreateApp(Options{
			Port:       port,
			Authorizer: stubAuthorizer{},
		})},
	})

	target, err := local.Native().Spawn(factory_probe, gen.ProcessOptions{})
	if err != nil {
		t.Fatalf("spawn the observed process: %s", err)
	}

	base := fmt.Sprintf("http://localhost:%d", port)
	for token, want := range map[string]int{"": http.StatusUnauthorized, "banned": http.StatusForbidden} {
		request, err := http.NewRequest(http.MethodGet, base+"/sse", nil)
		if err != nil {
			t.Fatalf("sse request: %s", err)
		}
		if token != "" {
			request.Header.Set("X-Token", token)
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatalf("sse connect: %s", err)
		}
		response.Body.Close()
		if response.StatusCode != want {
			t.Errorf("token %q got %s, want %d", token, response.Status, want)
		}
	}

	root := openBrowserAs(t, port, map[string]string{"X-Token": "root"})
	guest := openBrowserAs(t, port, map[string]string{"X-Token": "guest"})

	if manageOffered(t, root) == false {
		t.Error("the authorized caller was offered nothing to change")
	}
	if manageOffered(t, guest) {
		t.Error("the narrowed caller was offered a mutation")
	}

	sessionPID, err := local.Native().ProcessPID(gen.Atom("observer_session_" + root.sessionID))
	if err != nil {
		t.Fatalf("no session process: %s", err)
	}
	summary, err := local.Native().Inspect(sessionPID)
	if err != nil {
		t.Fatalf("inspect session: %s", err)
	}
	if summary["subject"] != "root" {
		t.Errorf("the session serves %q", summary["subject"])
	}

	stolen := &browser{t: t, base: base, sessionID: root.sessionID, headers: guest.headers}
	if answer := stolen.post("/api/do/kill", map[string]any{"pid": target}); answer.OK {
		t.Error("one caller acted through the session of another")
	}
	if _, err := local.Native().ProcessInfo(target); err != nil {
		t.Errorf("the process did not survive: %s", err)
	}

	summary, err = local.Native().Inspect(sessionPID)
	if err != nil {
		t.Fatalf("inspect session: %s", err)
	}
	if strings.Contains(summary["dropped"], "subject_mismatch") == false {
		t.Errorf("the mismatch was not counted: %q", summary["dropped"])
	}
}

func TestRateLimitOverHTTP(t *testing.T) {
	s := stage.New(t, stage.StageOptions{RegistrarFull: true})

	port := freePort(t)
	s.StartNode("obs", stage.NodeOptions{
		EnableSystemApp: true,
		Applications: []gen.ApplicationBehavior{CreateApp(Options{
			Port:      port,
			RateLimit: 2,
		})},
	})

	b := openBrowser(t, port)

	base := fmt.Sprintf("http://localhost:%d", port)
	code := func(path string) int {
		request, err := http.NewRequest(http.MethodPost, base+path, strings.NewReader("{}"))
		if err != nil {
			t.Fatalf("request: %s", err)
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-Observer-Session", b.sessionID)
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatalf("post: %s", err)
		}
		response.Body.Close()
		return response.StatusCode
	}

	for i := 1; i <= 2; i++ {
		if got := code("/api/do/capabilities"); got != http.StatusOK {
			t.Fatalf("request %d got %d, the stream must not spend the POST budget", i, got)
		}
	}
	if got := code("/api/do/capabilities"); got != http.StatusTooManyRequests {
		t.Fatalf("the request past the limit got %d, want 429", got)
	}

	response, err := http.Get(base + "/index.html")
	if err != nil {
		t.Fatalf("get the bundle: %s", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Errorf("the bundle answered %s", response.Status)
	}
}

func TestStaleReleaseFromAnotherSession(t *testing.T) {
	s := stage.New(t, stage.StageOptions{RegistrarFull: true})

	port := freePort(t)
	local := s.StartNode("obs", stage.NodeOptions{
		EnableSystemApp: true,
		Applications:    []gen.ApplicationBehavior{CreateApp(Options{Port: port, Host: "localhost"})},
	})

	scope := map[string]any{"pidStart": 1000, "pidLimit": 50}

	gone := openBrowser(t, port)
	stale := gone.subscribe("process_list", scope)

	fresh := openBrowser(t, port)
	handle := fresh.subscribe("process_list", scope)
	if handle == stale {
		t.Fatalf("two sessions share the handle %q", handle)
	}
	fresh.drain()

	fresh.unsubscribeByHandle(stale)

	sessionPID, err := local.Native().ProcessPID(gen.Atom("observer_session_" + fresh.sessionID))
	if err != nil {
		t.Fatalf("no session process: %s", err)
	}
	held, err := local.Native().Inspect(sessionPID, "handles")
	if err != nil {
		t.Fatalf("inspect session: %s", err)
	}
	if strings.Contains(held["handles"], handle) == false {
		t.Fatalf("a release from another session took the subscription: %q", held["handles"])
	}

	fresh.wait("process_list", 10*time.Second)
	fresh.wait("process_list", 10*time.Second)

	summary, err := local.Native().Inspect(sessionPID)
	if err != nil {
		t.Fatalf("inspect session: %s", err)
	}
	if strings.Contains(summary["dropped"], "unsubscribe_unknown") == false {
		t.Errorf("the stale handle resolved to something: %q", summary["dropped"])
	}
}

func get(t *testing.T, url string, headers map[string]string) (int, []byte) {
	t.Helper()

	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("request %s: %s", url, err)
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("get %s: %s", url, err)
	}
	defer response.Body.Close()

	body, _ := io.ReadAll(response.Body)
	return response.StatusCode, body
}

func postJSON(t *testing.T, url string, body string, headers map[string]string) (int, apiResponse) {
	t.Helper()

	request, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatalf("request %s: %s", url, err)
	}
	request.Header.Set("Content-Type", "application/json")
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("post %s: %s", url, err)
	}
	defer response.Body.Close()

	var answer apiResponse
	json.NewDecoder(response.Body).Decode(&answer)
	return response.StatusCode, answer
}

func postMCP(t *testing.T, url string, body string, headers map[string]string) (int, []byte) {
	t.Helper()

	request, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatalf("request %s: %s", url, err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("post %s: %s", url, err)
	}
	defer response.Body.Close()

	answer, _ := io.ReadAll(response.Body)
	return response.StatusCode, answer
}

func postConforming(t *testing.T, url string, method string, id any,
	params map[string]any, headers map[string]string) (int, []byte) {
	t.Helper()

	body, mirror := mcpConforming(method, id, params)
	for name, value := range headers {
		mirror[name] = value
	}
	return postMCP(t, url, body, mirror)
}

func TestListenerSurfacesOverHTTP(t *testing.T) {
	s := stage.New(t, stage.StageOptions{RegistrarFull: true})

	local, public := freePort(t), freePort(t)
	s.StartNode("obs", stage.NodeOptions{
		EnableSystemApp: true,
		Applications: []gen.ApplicationBehavior{CreateApp(Options{
			Enrollment: EnrollmentOptions{Token: "one-time-secret", ClusterID: "cluster1"},
			Listeners: []Listener{
				{Name: "local", Port: local},
				{
					Name:           "public",
					Port:           public,
					Authorizer:     stubAuthorizer{},
					Ceiling:        Ceiling{ReadOnly: true},
					UI:             SurfaceUI{Disable: true},
					MCP:            SurfaceMCP{Disable: true},
					AllowedOrigins: []string{cloudOrigin},
				},
			},
		})},
	})

	localBase := fmt.Sprintf("http://localhost:%d", local)
	publicBase := fmt.Sprintf("http://localhost:%d", public)

	if code, body := get(t, localBase+"/", nil); code != http.StatusOK || bytes.Contains(body, []byte("<")) == false {
		t.Errorf("the local listener answered %d for the bundle", code)
	}
	if code, _ := get(t, publicBase+"/", map[string]string{"X-Token": "root"}); code != http.StatusNotFound {
		t.Errorf("the public listener served the bundle: %d", code)
	}

	if code, _ := get(t, localBase+"/mcp", nil); code != http.StatusMethodNotAllowed {
		t.Errorf("the local /mcp answered %d for a GET, want 405", code)
	}
	if code, _ := get(t, publicBase+"/mcp", map[string]string{"X-Token": "root"}); code != http.StatusNotFound {
		t.Errorf("the public /mcp answered %d, want 404", code)
	}

	code, body := postConforming(t, localBase+"/mcp", "server/discover", 1, nil, nil)
	if code != http.StatusOK {
		t.Errorf("server/discover answered %d: %s", code, body)
	}
	if bytes.Contains(body, []byte(mcpProtocolVersion)) == false {
		t.Errorf("server/discover did not name the revision: %s", body)
	}

	if code, body := postConforming(t, localBase+"/mcp", "initialize", 2, nil, nil); code != http.StatusNotFound {
		t.Errorf("initialize answered %d: %s", code, body)
	}

	for base, expect := range map[string]struct{ auth, readOnly bool }{
		localBase:  {auth: false, readOnly: false},
		publicBase: {auth: true, readOnly: true},
	} {
		code, body := get(t, base+"/api/capabilities", nil)
		if code != http.StatusOK {
			t.Fatalf("%s answered %d without a token", base, code)
		}
		var answer wireObserverCapabilities
		if err := json.Unmarshal(body, &answer); err != nil {
			t.Fatalf("%s: %s", base, err)
		}
		if answer.Auth != expect.auth || answer.ReadOnly != expect.readOnly {
			t.Errorf("%s reported %+v", base, answer)
		}
		if has(answer.Features, FeatureEnroll) == false {
			t.Errorf("%s does not offer enrollment: %v", base, answer.Features)
		}
	}
}

func enrollStand(t *testing.T) (*stage.Node, string, string) {
	t.Helper()

	s := stage.New(t, stage.StageOptions{RegistrarFull: true})
	first, second := freePort(t), freePort(t)
	node := s.StartNode("obs", stage.NodeOptions{
		EnableSystemApp: true,
		Applications: []gen.ApplicationBehavior{CreateApp(Options{
			Enrollment: EnrollmentOptions{Token: "one-time-secret", ClusterID: "cluster1"},
			Listeners: []Listener{
				{Name: "local", Port: first},
				{Name: "public", Port: second, UI: SurfaceUI{Disable: true}},
			},
		})},
	})

	return node, fmt.Sprintf("http://localhost:%d", first), fmt.Sprintf("http://localhost:%d", second)
}

func enrollInspect(t *testing.T, node *stage.Node) map[string]string {
	t.Helper()

	managerPID, err := node.Native().ProcessPID(managerName)
	if err != nil {
		t.Fatalf("no manager: %s", err)
	}
	state, err := node.Native().Inspect(managerPID)
	if err != nil {
		t.Fatalf("inspect manager: %s", err)
	}
	return state
}

func TestEnrollRefusesWrongSecret(t *testing.T) {
	node, base, _ := enrollStand(t)

	if code, answer := postJSON(t, base+"/api/enroll", `{"token":"wrong"}`, nil); code != http.StatusForbidden {
		t.Fatalf("a wrong secret got %d (%s)", code, answer.Error)
	}

	state := enrollInspect(t, node)
	if state["enrolled"] != "no" {
		t.Errorf("a wrong secret enrolled the cluster: %v", state)
	}
	if strings.Contains(state["dropped"], "enroll_failed") == false {
		t.Errorf("the attempt was not counted: %q", state["dropped"])
	}
}

func TestEnrollBurnsTheSecret(t *testing.T) {
	node, first, second := enrollStand(t)

	code, answer := postJSON(t, first+"/api/enroll", `{"token":"one-time-secret"}`, nil)
	if code != http.StatusOK || answer.OK == false {
		t.Fatalf("enrollment answered %d (%s)", code, answer.Error)
	}
	data, _ := answer.Data.(map[string]any)
	if cluster, _ := data["cluster"].(string); cluster != "cluster1" {
		t.Errorf("the answer came out as %#v", answer.Data)
	}

	if code, _ := postJSON(t, second+"/api/enroll", `{"token":"one-time-secret"}`, nil); code != http.StatusGone {
		t.Errorf("the spent secret answered %d on the other listener, want 410", code)
	}

	state := enrollInspect(t, node)
	if state["enrolled"] != "yes" || state["last_enroll"] == "never" {
		t.Errorf("the manager reports %v", state)
	}
	if strings.Contains(state["dropped"], "enroll_burned") == false {
		t.Errorf("the second attempt was not counted: %q", state["dropped"])
	}
}

func TestStatelessActionOverHTTP(t *testing.T) {
	s := stage.New(t, stage.StageOptions{RegistrarFull: true})

	port := freePort(t)
	local := s.StartNode("obs", stage.NodeOptions{
		EnableSystemApp: true,
		Applications: []gen.ApplicationBehavior{CreateApp(Options{
			Port:       port,
			Authorizer: stubAuthorizer{},
		})},
	})

	target, err := local.Native().Spawn(factory_probe, gen.ProcessOptions{})
	if err != nil {
		t.Fatalf("spawn the observed process: %s", err)
	}

	base := fmt.Sprintf("http://localhost:%d", port)
	root := map[string]string{"X-Token": "root"}
	guest := map[string]string{"X-Token": "guest"}
	pid := fmt.Sprintf(`{"pid":{"Node":%q,"ID":%d,"Creation":%d}}`,
		string(target.Node), target.ID, target.Creation)

	code, answer := postJSON(t, base+"/api/call/inspect", pid, root)
	if code != http.StatusOK || answer.OK == false {
		t.Fatalf("a read answered %d (%s)", code, answer.Error)
	}

	if code, answer := postJSON(t, base+"/api/call/cluster_info", `{}`, root); code != http.StatusOK || answer.OK == false {
		t.Fatalf("cluster_info answered %d (%s)", code, answer.Error)
	}

	if code, _ := postJSON(t, base+"/api/call/kill", pid, guest); code != http.StatusBadRequest {
		t.Errorf("a read-only caller killed a process: %d", code)
	}
	if _, err := local.Native().ProcessInfo(target); err != nil {
		t.Errorf("the process did not survive the refused kill: %s", err)
	}

	if code, _ := postJSON(t, base+"/api/call/inspect", pid, nil); code != http.StatusUnauthorized {
		t.Errorf("an unidentified one-shot action got %d, want 401", code)
	}

	if code, answer := postJSON(t, base+"/api/call/kill", pid, root); code != http.StatusOK || answer.OK == false {
		t.Fatalf("kill answered %d (%s)", code, answer.Error)
	}
	waitFor(t, 5*time.Second, "the process is gone", func() bool {
		_, err := local.Native().ProcessInfo(target)
		return err != nil
	})
}

func TestUnsupportedRequestDoesNotKillActors(t *testing.T) {
	s := stage.New(t, stage.StageOptions{RegistrarFull: true})

	port := freePort(t)
	local := s.StartNode("obs", stage.NodeOptions{
		EnableSystemApp: true,
		Applications: []gen.ApplicationBehavior{CreateApp(Options{
			Port:       port,
			Enrollment: EnrollmentOptions{Token: "one-time-secret", ClusterID: "cluster1"},
		})},
	})

	b := openBrowser(t, port)
	handle := b.subscribe("node_info", nil)

	targets := map[string]gen.Atom{
		"manager": managerName,
		"session": gen.Atom("observer_session_" + b.sessionID),
		"cluster": clusterName,
	}

	before := map[string]gen.PID{}
	for name, process := range targets {
		pid, err := local.Native().ProcessPID(process)
		if err != nil {
			t.Fatalf("no %s: %s", name, err)
		}
		before[name] = pid
	}

	for name, process := range targets {
		answer, err := local.Call(process, "a request no actor here serves")
		if err != nil {
			t.Errorf("%s did not answer: %s", name, err)
			continue
		}
		if reported, ok := answer.(error); ok == false || errors.Is(reported, gen.ErrUnsupported) == false {
			t.Errorf("%s answered %#v, want ErrUnsupported", name, answer)
		}
	}

	for name, process := range targets {
		pid, err := local.Native().ProcessPID(process)
		if err != nil {
			t.Fatalf("%s is gone: %s", name, err)
		}
		if pid != before[name] {
			t.Errorf("%s was restarted: %s -> %s", name, before[name], pid)
		}
	}

	held, err := local.Native().Inspect(before["session"], "handles")
	if err != nil {
		t.Fatalf("inspect session: %s", err)
	}
	if strings.Contains(held["handles"], handle) == false {
		t.Errorf("the subscription is gone: %q", held["handles"])
	}
	sessionState, err := local.Native().Inspect(before["session"])
	if err != nil {
		t.Fatalf("inspect session: %s", err)
	}
	if strings.Contains(sessionState["dropped"], "request_unsupported") == false {
		t.Errorf("the session did not count it: %q", sessionState["dropped"])
	}
	b.wait("node_info", 10*time.Second)

	managerState, err := local.Native().Inspect(before["manager"])
	if err != nil {
		t.Fatalf("inspect manager: %s", err)
	}
	if managerState["enrollment"] != "yes" || managerState["enrolled"] != "no" {
		t.Errorf("the manager reports %v", managerState)
	}
	if strings.Contains(managerState["dropped"], "request_unsupported") == false {
		t.Errorf("the manager did not count it: %q", managerState["dropped"])
	}
}
