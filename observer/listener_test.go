package observer

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"ergo.services/application/observer/access"
	"ergo.services/ergo/app/system/manage"
)

func TestListenersFromOneOption(t *testing.T) {
	resolved, err := Options{Ceiling: Ceiling{ReadOnly: true}}.listeners()
	if err != nil {
		t.Fatalf("resolve: %s", err)
	}
	if len(resolved) != 1 {
		t.Fatalf("resolved %d listeners", len(resolved))
	}
	if resolved[0].Port != DefaultPort || resolved[0].Host != defaultHost {
		t.Errorf("the default endpoint came out as %s:%d", resolved[0].Host, resolved[0].Port)
	}
	if resolved[0].Ceiling.ReadOnly == false {
		t.Error("the deployment ceiling did not reach the listener")
	}
}

func TestListenersNarrowedByDeployment(t *testing.T) {
	resolved, err := Options{
		Ceiling: Ceiling{Deny: []string{manage.CapAppUnload}},
		Listeners: []Listener{
			{Port: 9911},
			{
				Port:    9912,
				Host:    "0.0.0.0",
				Ceiling: Ceiling{ReadOnly: true},
				UI:      SurfaceUI{Disable: true},
				MCP:     SurfaceMCP{Disable: true},
			},
		},
	}.listeners()
	if err != nil {
		t.Fatalf("resolve: %s", err)
	}

	local, public := resolved[0], resolved[1]
	if local.Host != defaultHost || public.Host != "0.0.0.0" {
		t.Errorf("hosts came out as %q and %q", local.Host, public.Host)
	}
	if local.Ceiling.Allows(manage.CapKill) == false {
		t.Error("the local listener lost a capability the deployment allows")
	}
	if local.Ceiling.Allows(manage.CapAppUnload) {
		t.Error("the deployment deny list did not reach the listener")
	}
	if public.Ceiling.Allows(manage.CapKill) {
		t.Error("the read-only listener allows a mutation")
	}
	if public.Ceiling.Allows(manage.CapAppUnload) {
		t.Error("the deny list did not survive the narrowing")
	}
}

func TestListenersRefuseAmbiguousConfig(t *testing.T) {
	cases := map[string]Options{
		"port and listeners": {Port: 9911, Listeners: []Listener{{Port: 9912}}},
		"host and listeners": {Host: "0.0.0.0", Listeners: []Listener{{Port: 9912}}},
		"no port":            {Listeners: []Listener{{Host: "localhost"}}},
		"same port twice":    {Listeners: []Listener{{Port: 9911}, {Port: 9911}}},
	}
	for name, options := range cases {
		if _, err := options.listeners(); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

// answers by token: one caller in full, one narrowed, everybody else out
type stubAuthorizer struct{}

func (stubAuthorizer) Authorize(request *http.Request) (Identity, error) {
	switch request.Header.Get("X-Token") {
	case "root":
		return Identity{Subject: "root"}, nil
	case "guest":
		return Identity{Subject: "guest", Ceiling: Ceiling{ReadOnly: true}}, nil
	case "banned":
		return Identity{}, access.ErrForbidden
	}
	return Identity{}, access.ErrUnauthenticated
}

func TestGuardWithoutAuthorizer(t *testing.T) {
	var seen Identity
	handler := guard{
		ceiling: Ceiling{ReadOnly: true},
		next: http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			seen, _ = identityOf(r)
		}),
	}

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/sse", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("an open listener answered %d", recorder.Code)
	}
	if seen.Ceiling.ReadOnly == false {
		t.Error("the listener ceiling did not reach the request")
	}
	if seen.Subject != "" {
		t.Errorf("an anonymous caller came out as %q", seen.Subject)
	}
}

func TestGuardNarrowsAndRefuses(t *testing.T) {
	var seen Identity
	handler := guard{
		authorizer: stubAuthorizer{},
		ceiling:    Ceiling{Deny: []string{manage.CapAppUnload}},
		next: http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			seen, _ = identityOf(r)
		}),
	}

	call := func(token string) int {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/sse", nil)
		if token != "" {
			request.Header.Set("X-Token", token)
		}
		handler.ServeHTTP(recorder, request)
		return recorder.Code
	}

	if code := call("root"); code != http.StatusOK {
		t.Fatalf("the authorized caller got %d", code)
	}
	if seen.Subject != "root" {
		t.Errorf("the subject came out as %q", seen.Subject)
	}
	if seen.Ceiling.Allows(manage.CapKill) == false || seen.Ceiling.Allows(manage.CapAppUnload) {
		t.Errorf("the listener ceiling was not applied: %+v", seen.Ceiling)
	}

	if code := call("guest"); code != http.StatusOK {
		t.Fatalf("the narrowed caller got %d", code)
	}
	if seen.Ceiling.ReadOnly == false || seen.Ceiling.Allows(manage.CapAppUnload) {
		t.Errorf("an authorizer widened the ceiling: %+v", seen.Ceiling)
	}

	if code := call(""); code != http.StatusUnauthorized {
		t.Errorf("an unidentified caller got %d", code)
	}
	if code := call("banned"); code != http.StatusForbidden {
		t.Errorf("a rejected caller got %d", code)
	}
}

func TestLimiterRefills(t *testing.T) {
	l := newLimiter(2)
	start := time.Now()

	if l.allow("a", start) == false || l.allow("a", start) == false {
		t.Fatal("the bucket did not hold its own rate")
	}
	if l.allow("a", start) {
		t.Error("an empty bucket allowed a request")
	}
	if l.allow("b", start) == false {
		t.Error("one caller emptied the bucket of another")
	}
	if l.allow("a", start.Add(500*time.Millisecond)) == false {
		t.Error("half a second did not refill one token")
	}
	if l.allow("a", start.Add(time.Hour)) == false {
		t.Error("a long wait did not refill the bucket")
	}

	if newLimiter(0).allow("a", start) == false {
		t.Error("an unlimited listener refused a request")
	}
}

func TestLimiterForgetsIdleCallers(t *testing.T) {
	l := newLimiter(1)
	now := time.Now()
	for i := 0; i < limiterSweepAt+1; i++ {
		l.allow(fmt.Sprintf("caller-%d", i), now)
	}
	if len(l.seen) <= limiterSweepAt {
		t.Fatalf("the table was swept while every bucket was still spent: %d", len(l.seen))
	}

	// a second later every bucket is full again, so the next call clears the table
	l.allow("caller-0", now.Add(time.Second))
	if len(l.seen) > 1 {
		t.Errorf("idle callers were kept: %d", len(l.seen))
	}
}

func TestThrottleCountsBySubject(t *testing.T) {
	handler := guard{
		authorizer: stubAuthorizer{},
		next: throttle{
			limiter: newLimiter(1),
			next:    http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}),
		},
	}

	call := func(token string) int {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api/do/kill", nil)
		request.Header.Set("X-Token", token)
		handler.ServeHTTP(recorder, request)
		return recorder.Code
	}

	if code := call("root"); code != http.StatusOK {
		t.Fatalf("the first request got %d", code)
	}
	if code := call("root"); code != http.StatusTooManyRequests {
		t.Fatalf("the second request of the same subject got %d, want 429", code)
	}
	if code := call("guest"); code != http.StatusOK {
		t.Errorf("another subject was metered together with the first: %d", code)
	}
}

func TestListenerSurfaceValidation(t *testing.T) {
	cases := map[string]Options{
		"two uis": {Listeners: []Listener{
			{Port: 9911},
			{Port: 9912},
		}},
		"origin with a path":   {AllowedOrigins: []string{"https://ergo.observer/app"}},
		"origin without host":  {AllowedOrigins: []string{"https://"}},
		"origin without proto": {AllowedOrigins: []string{"ergo.observer"}},
	}
	for name, options := range cases {
		if _, err := options.listeners(); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}

	if _, err := (Options{Listeners: []Listener{{
		Port: 9911,
		UI:   SurfaceUI{Disable: true},
		MCP:  SurfaceMCP{Disable: true},
	}}}).listeners(); err != nil {
		t.Errorf("an API-only listener was refused: %s", err)
	}
}

// a header believed because of where it came from is an invitation on a port anything reaches,
// so the deployment has to say how the path is restricted before the node will start
func TestTrustedHeaderNeedsARestrictedPath(t *testing.T) {
	trusted := access.TrustedHeader{Subject: "X-Auth-Request-Email"}

	for name, options := range map[string]Options{
		"every interface": {Listeners: []Listener{
			{Port: 9911, Host: "0.0.0.0", Authorizer: trusted},
		}},
		"a routable address": {Listeners: []Listener{
			{Port: 9911, Host: "10.1.2.3", Authorizer: trusted},
		}},
		"no subject header": {Listeners: []Listener{
			{Port: 9911, Host: "localhost", Authorizer: access.TrustedHeader{}},
		}},
		"ceilings with no groups header": {Listeners: []Listener{
			{Port: 9911, Host: "localhost", Authorizer: access.TrustedHeader{
				Subject: "X-User", Ceilings: map[string]Ceiling{"sre": {}},
			}},
		}},
	} {
		if _, err := options.listeners(); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}

	for name, host := range map[string]string{
		"localhost": "localhost", "ipv4": "127.0.0.1", "ipv6": "::1",
	} {
		if _, err := (Options{Listeners: []Listener{
			{Port: 9911, Host: host, Authorizer: trusted},
		}}).listeners(); err != nil {
			t.Errorf("%s was refused: %s", name, err)
		}
	}

	// stated as restricted some other way: a network policy, a mesh
	if _, err := (Options{Listeners: []Listener{{
		Port: 9911, Host: "0.0.0.0",
		Authorizer: access.TrustedHeader{
			Subject: "X-Auth-Request-Email", ReachableOnlyByProxy: true,
		},
	}}}).listeners(); err != nil {
		t.Errorf("a stated restriction was refused: %s", err)
	}
}

func TestListenerDefaults(t *testing.T) {
	resolved, err := Options{AllowedOrigins: []string{cloudOrigin, "*"}}.listeners()
	if err != nil {
		t.Fatalf("resolve: %s", err)
	}
	l := resolved[0]
	if l.UI.Disable || l.MCP.Disable {
		t.Errorf("the default listener stopped serving something: %+v", l)
	}
	if l.Name != ":9911" {
		t.Errorf("the name came out as %q", l.Name)
	}
}

func TestSurfaceCeilingOnlyNarrows(t *testing.T) {
	readOnly := Ceiling{ReadOnly: true}
	l := Listener{
		Ceiling: Ceiling{Deny: []string{manage.CapAppUnload}},
		MCP:     SurfaceMCP{Ceiling: &readOnly},
	}

	if l.Ceiling.ReadOnly {
		t.Error("the listener inherited a ceiling nobody gave it")
	}
	if l.ceilingMCP().ReadOnly == false {
		t.Error("the MCP surface did not get its own ceiling")
	}
	if l.ceilingMCP().Allows(manage.CapAppUnload) {
		t.Error("the listener deny list did not survive the surface ceiling")
	}

	full := Ceiling{}
	strict := Listener{Ceiling: readOnly, MCP: SurfaceMCP{Ceiling: &full}}
	if strict.ceilingMCP().ReadOnly == false {
		t.Error("a surface widened the listener ceiling")
	}
}

func TestCapabilitiesPayload(t *testing.T) {
	local := Listener{Port: 9911}
	public := Listener{
		Port:       9912,
		Authorizer: stubAuthorizer{},
		Ceiling:    Ceiling{ReadOnly: true},
		UI:         SurfaceUI{Disable: true},
		MCP:        SurfaceMCP{Disable: true},
	}

	anonymous := answerOf(t, capabilitiesHandler(local, false))
	if anonymous.Auth || anonymous.ReadOnly {
		t.Errorf("the local listener came out as %+v", anonymous)
	}
	if has(anonymous.Features, FeatureUI) == false || has(anonymous.Features, FeatureMCP) == false {
		t.Errorf("features came out as %v", anonymous.Features)
	}
	if has(anonymous.Features, FeatureStatelessActions) {
		t.Error("one-shot actions were offered without an authorizer")
	}
	if has(anonymous.Features, FeatureEnroll) {
		t.Error("enrollment was offered while it is not configured")
	}

	authorized := answerOf(t, capabilitiesHandler(public, true))
	if authorized.Auth == false || authorized.ReadOnly == false {
		t.Errorf("the public listener came out as %+v", authorized)
	}
	for _, name := range []string{FeatureStatelessActions, FeatureEnroll, FeatureSSE} {
		if has(authorized.Features, name) == false {
			t.Errorf("%s is missing from %v", name, authorized.Features)
		}
	}
	for _, name := range []string{FeatureUI, FeatureMCP} {
		if has(authorized.Features, name) {
			t.Errorf("%s is offered by a listener that does not serve it", name)
		}
	}
	if authorized.Version.Release == "" {
		t.Error("the version is not reported")
	}
}

func TestCapabilitiesRefusesOtherMethods(t *testing.T) {
	recorder := httptest.NewRecorder()
	capabilitiesHandler(Listener{Port: 9911}, false).
		ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/capabilities", nil))

	if recorder.Code != http.StatusMethodNotAllowed {
		t.Errorf("a POST got %d", recorder.Code)
	}
}

func answerOf(t *testing.T, handler http.Handler) wireObserverCapabilities {
	t.Helper()

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/capabilities", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("capabilities answered %d", recorder.Code)
	}

	var answer wireObserverCapabilities
	if err := json.Unmarshal(recorder.Body.Bytes(), &answer); err != nil {
		t.Fatalf("capabilities payload: %s", err)
	}
	return answer
}

func has(names []string, name string) bool {
	for _, n := range names {
		if n == name {
			return true
		}
	}
	return false
}

func TestQualifiedTellsPrincipalsApart(t *testing.T) {
	seen := map[string]Identity{}

	for _, identity := range []Identity{
		{Tenant: "a", Subject: "b/c"},
		{Tenant: "a/b", Subject: "c"},
		{Tenant: "", Subject: "a/b"},
		{Tenant: "a", Subject: "b"},
		{Tenant: `a\`, Subject: "b"},
		{Tenant: "a", Subject: `\b`},
		{Tenant: "corp.com", Subject: "alice@corp.com"},
		{Tenant: "corp.com", Subject: "system:serviceaccount/ns/deploy"},
	} {
		got := qualified(identity)
		if other, collided := seen[got]; collided {
			t.Errorf("%#v and %#v are one principal %q", other, identity, got)
		}
		seen[got] = identity
	}

	if got := qualified(Identity{Tenant: "corp.com", Subject: "alice@corp.com"}); got != "corp.com/alice@corp.com" {
		t.Errorf("a plain pair reads as %q", got)
	}
	if got := qualified(Identity{Subject: "alice"}); got != "alice" {
		t.Errorf("a subject without a tenant reads as %q", got)
	}
}
