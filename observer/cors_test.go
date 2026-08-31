package observer

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

const cloudOrigin = "https://ergo.observer"

func corsCall(handler http.Handler, method string, origin string, preflight bool) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, "/api/subscribe", nil)
	if origin != "" {
		request.Header.Set("Origin", origin)
	}
	if preflight {
		request.Header.Set("Access-Control-Request-Method", http.MethodPost)
		request.Header.Set("Access-Control-Request-Headers", "content-type, x-observer-session")
	}
	handler.ServeHTTP(recorder, request)
	return recorder
}

func TestCorsSilentWithoutOrigins(t *testing.T) {
	reached := false
	handler := cors{next: http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) { reached = true })}

	answer := corsCall(handler, http.MethodPost, cloudOrigin, false)

	if reached == false || answer.Code != http.StatusOK {
		t.Fatalf("the request did not pass through: %d", answer.Code)
	}
	if answer.Header().Get("Access-Control-Allow-Origin") != "" || answer.Header().Get("Vary") != "" {
		t.Errorf("headers appeared without any origin declared: %v", answer.Header())
	}
}

func TestCorsAllowsDeclaredOrigin(t *testing.T) {
	handler := cors{
		origins: []string{cloudOrigin},
		next:    http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}),
	}

	allowed := corsCall(handler, http.MethodPost, cloudOrigin, false)
	if allowed.Header().Get("Access-Control-Allow-Origin") != cloudOrigin {
		t.Errorf("the origin was not echoed: %v", allowed.Header())
	}
	if allowed.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Error("an SSE request cannot carry a header of its own, so credentials must be allowed")
	}
	if allowed.Header().Get("Vary") != "Origin" {
		t.Errorf("Vary came out as %q", allowed.Header().Get("Vary"))
	}

	other := corsCall(handler, http.MethodPost, "https://evil.example", false)
	if other.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Errorf("an undeclared origin was allowed: %v", other.Header())
	}
	if other.Header().Get("Vary") != "Origin" {
		t.Error("Vary must be set whether or not the origin is allowed")
	}
}

// the wildcard is the anonymous form: any origin, and therefore no credentials
func TestCorsWildcardWithoutCredentials(t *testing.T) {
	handler := cors{
		origins: []string{"*"},
		next:    http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}),
	}

	answer := corsCall(handler, http.MethodPost, cloudOrigin, false)
	if answer.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("the wildcard was not answered: %v", answer.Header())
	}
	if answer.Header().Get("Access-Control-Allow-Credentials") != "" {
		t.Error("credentials with a wildcard origin are refused by every browser")
	}
}

// a preflight carries no credentials, so it must be answered before anything authorizes it
func TestCorsPreflightSkipsTheGuard(t *testing.T) {
	reached := false
	handler := cors{
		origins: []string{cloudOrigin},
		next: guard{
			authorizer: stubAuthorizer{},
			next:       http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) { reached = true }),
		},
	}

	answer := corsCall(handler, http.MethodOptions, cloudOrigin, true)

	if answer.Code != http.StatusNoContent {
		t.Fatalf("the preflight got %d, want 204", answer.Code)
	}
	if reached {
		t.Error("the preflight reached the handler behind the guard")
	}
	if answer.Header().Get("Access-Control-Allow-Headers") != "content-type, x-observer-session" {
		t.Errorf("the requested headers were not allowed: %v", answer.Header())
	}
	if answer.Header().Get("Access-Control-Allow-Methods") == "" || answer.Header().Get("Access-Control-Max-Age") == "" {
		t.Errorf("the preflight answer is incomplete: %v", answer.Header())
	}

	if code := corsCall(handler, http.MethodOptions, "https://evil.example", true).Code; code != http.StatusForbidden {
		t.Errorf("a preflight from an undeclared origin got %d", code)
	}
}

// the browser must be able to read a refusal, so the headers are set before the guard runs
func TestCorsHeadersSurviveRefusal(t *testing.T) {
	handler := cors{
		origins: []string{cloudOrigin},
		next: guard{
			authorizer: stubAuthorizer{},
			next:       http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}),
		},
	}

	answer := corsCall(handler, http.MethodPost, cloudOrigin, false)

	if answer.Code != http.StatusUnauthorized {
		t.Fatalf("an unidentified caller got %d", answer.Code)
	}
	if answer.Header().Get("Access-Control-Allow-Origin") != cloudOrigin {
		t.Errorf("the browser cannot read this refusal: %v", answer.Header())
	}
}

func TestCorsWildcardSubdomain(t *testing.T) {
	origins := []string{ErgoOrigin, ErgoOriginTenants}
	refused := &refusals{}
	handler := originGuard{
		origins: origins,
		refused: refused,
		next:    cors{origins: origins, next: http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {})},
	}

	allowed := map[string]bool{
		"https://app.ergo.observer":               true,
		"https://acme.app.ergo.observer":          true,
		"https://CDN.app.ergo.observer":           true,
		"https://a.b.app.ergo.observer":           false,
		"https://app.ergo.observer:8443":          false,
		"http://acme.app.ergo.observer":           false,
		"https://ergo.observer":                   false,
		"https://evil.com":                        false,
		"https://notapp.ergo.observer":            false,
		"https://acme.app.ergo.observer.evil.com": false,
	}

	for origin, want := range allowed {
		answer := corsCall(handler, http.MethodPost, origin, false)
		echoed := answer.Header().Get("Access-Control-Allow-Origin")
		if (echoed != "") != want {
			t.Errorf("origin %q came out as allowed=%v (echoed %q)", origin, echoed != "", echoed)
		}
		if want && echoed != origin {
			t.Errorf("origin %q was answered with %q", origin, echoed)
		}
	}

	if refused.count.Load() == 0 {
		t.Error("refusals are not counted")
	}
	if last, _ := refused.last.Load().(string); last == "" {
		t.Error("the last refused origin is not remembered")
	}
}

func TestWildcardOriginValidation(t *testing.T) {
	refused := []string{
		"https://*.observer",
		"https://*",
		"https://ac*.app.ergo.observer",
		"https://*.*.ergo.observer",
	}
	for _, origin := range refused {
		if _, err := (Options{AllowedOrigins: []string{origin}}).listeners(); err == nil {
			t.Errorf("origin %q was accepted", origin)
		}
	}

	accepted := []string{ErgoOrigin, ErgoOriginTenants, "https://*.internal.acme.corp", "*"}
	for _, origin := range accepted {
		if _, err := (Options{AllowedOrigins: []string{origin}}).listeners(); err != nil {
			t.Errorf("origin %q was refused: %s", origin, err)
		}
	}
}
