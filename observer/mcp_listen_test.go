package observer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ergo.services/ergo/app/system/inspect"
)

func listenServe(t *testing.T, identity Identity, limit int, params map[string]any) (
	*httptest.ResponseRecorder, *listenFilter) {

	t.Helper()

	body, headers := mcpConforming("subscriptions/listen", 1, params)
	request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	for name, value := range headers {
		request.Header.Set(name, value)
	}

	var parsed *http.Request
	keep := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { parsed = r })
	transport := mcpGate{
		listen:   keep,
		post:     keep,
		listener: withLimits(Listener{Port: 9911}),
		counts:   &refusalCounts{},
	}
	transport.ServeHTTP(httptest.NewRecorder(), request)
	if parsed == nil {
		t.Fatal("the transport gate refused a well-formed listen")
	}

	var followed *listenFilter
	gate := listenGate{
		next: http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			if filter, carried := listenFilterOf(r); carried {
				followed = &filter
			}
		}),
		listener: withLimits(Listener{Port: 9911, MaxSubscriptions: limit}),
		counts:   &refusalCounts{},
	}

	answer := httptest.NewRecorder()
	gate.ServeHTTP(answer, parsed.WithContext(
		context.WithValue(parsed.Context(), identityKey{}, identity)))
	return answer, followed
}

func followedURIs(filter *listenFilter) []string {
	if filter == nil {
		return nil
	}
	out := make([]string, 0, len(filter.uris))
	for _, uri := range filter.uris {
		out = append(out, uri.Canonical())
	}
	return out
}

func refusedBy(t *testing.T, answer *httptest.ResponseRecorder) map[string]any {
	t.Helper()

	failure := failureOf(t, answer)
	data, ok := failure.Error.Data.(map[string]any)
	if ok == false {
		return nil
	}
	refused, _ := data["refused"].(map[string]any)
	return refused
}

func following(uris ...string) map[string]any {
	return map[string]any{"notifications": map[string]any{"resourceSubscriptions": uris}}
}

func TestListenNeedsSomethingToFollow(t *testing.T) {
	cases := map[string]map[string]any{
		"nothing named":     {},
		"an empty filter":   {"notifications": map[string]any{}},
		"an empty list":     following(),
		"not a list":        {"notifications": map[string]any{"resourceSubscriptions": "ergo://n@h/log"}},
		"not even a URI":    {"notifications": map[string]any{"resourceSubscriptions": []any{7}}},
		"only list changes": {"notifications": map[string]any{"toolsListChanged": true}},
		"the old shape":     {"uris": []string{"ergo://n@h/log"}},
	}

	for name, params := range cases {
		answer, followed := listenServe(t, Identity{Subject: "root"}, 8, params)
		refusedWith(t, name, answer, mcpInvalidParams)
		if followed != nil {
			t.Errorf("%s: the stream was opened on %v", name, followedURIs(followed))
		}
	}
}

func TestListenCarriesTheRequestID(t *testing.T) {
	_, followed := listenServe(t, Identity{Subject: "root"}, 8, following("ergo://n@h/log"))
	if followed == nil {
		t.Fatal("nothing reached the stream")
	}
	if followed.id != float64(1) {
		t.Errorf("the stream would name subscription %#v, want the request id", followed.id)
	}
}

func TestListenHandsTheRefusalsToTheStream(t *testing.T) {
	identity := Identity{Subject: "root", Ceiling: Ceiling{Nodes: []string{"allowed@h"}}}

	_, followed := listenServe(t, identity, 8,
		following("ergo://allowed@h/log", "ergo://refused@h/log"))
	if followed == nil {
		t.Fatal("nothing reached the stream")
	}
	if reason := followed.refused["ergo://refused@h/log"]; reason == "" {
		t.Errorf("the refusal did not travel: %v", followed.refused)
	}
}

func TestListenHonoursTheSubscriptionLimit(t *testing.T) {
	asked := []string{
		"ergo://a@h/log",
		"ergo://b@h/log",
		"ergo://c@h/log",
	}

	answer, followed := listenServe(t, Identity{Subject: "root"}, 2, following(asked...))

	failure := refusedWith(t, "over the limit", answer, mcpInvalidParams)
	if followed != nil {
		t.Errorf("a stream over the limit was opened on %v", followedURIs(followed))
	}

	data, _ := failure.Error.Data.(map[string]any)
	if data["asked"] != float64(len(asked)) {
		t.Errorf("the refusal says %v were asked for", data["asked"])
	}

	if _, atLimit := listenServe(t, Identity{Subject: "root"}, 2,
		following(asked[:2]...)); atLimit == nil {
		t.Error("a stream at the limit was refused")
	}
}

func TestListenFollowsWhatSurvives(t *testing.T) {
	answer, followed := listenServe(t, Identity{Subject: "root"}, 8, following(
		"ergo://n@h/log?level=error",
		"ergo://n@h/watch/mine/processes",
		"ergo://n@h/nosuchlens",
		"not a uri at all",
	))

	if answer.Body.Len() != 0 {
		t.Fatalf("a partially followable listen was refused: %s", answer.Body.String())
	}
	if followed == nil {
		t.Fatal("nothing reached the stream")
	}

	got := followedURIs(followed)
	want := []string{"ergo://n@h/log?level=error", "ergo://n@h/watch/mine/processes"}
	if len(got) != len(want) {
		t.Fatalf("the stream follows %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("the stream follows %q at %d, want %q", got[i], i, want[i])
		}
	}
}

// the last moment a status can still be sent, so the reason for every URI comes back here
func TestListenRefusesWhenNothingSurvives(t *testing.T) {
	answer, followed := listenServe(t, Identity{Subject: "root"}, 8,
		following("ergo://n@h/nosuchlens", "not a uri at all"))

	refusedWith(t, "nothing followable", answer, mcpInvalidParams)
	if followed != nil {
		t.Error("a stream was opened with nothing to follow")
	}

	refused := refusedBy(t, answer)
	for _, uri := range []string{"ergo://n@h/nosuchlens", "not a uri at all"} {
		if reason, named := refused[uri]; named == false || reason == "" {
			t.Errorf("%q was refused without a reason: %v", uri, refused)
		}
	}
}

func TestListenRefusesTheClusterMap(t *testing.T) {
	answer, followed := listenServe(t, Identity{Subject: "root"}, 8, following(clusterURI))

	refusedWith(t, "cluster", answer, mcpInvalidParams)
	if followed != nil {
		t.Error("the cluster map was followed")
	}
	if reason, named := refusedBy(t, answer)[clusterURI]; named == false ||
		strings.Contains(reason.(string), "read") == false {
		t.Errorf("the refusal reads %v", reason)
	}
}

func TestListenJobNeedsANamedCaller(t *testing.T) {
	job := "ergo://job/prof-7"

	answer, followed := listenServe(t, Identity{}, 8, following(job))
	refusedWith(t, "anonymous job", answer, mcpInvalidParams)
	if followed != nil {
		t.Error("an anonymous caller followed a run")
	}

	_, named := listenServe(t, Identity{Subject: "root"}, 8, following(job))
	if named == nil || len(named.uris) != 1 {
		t.Errorf("a named caller follows %v", followedURIs(named))
	}
}

func TestListenAppliesTheCeiling(t *testing.T) {
	identity := Identity{
		Subject: "root",
		Ceiling: Ceiling{Nodes: []string{"allowed@h"}},
	}

	answer, followed := listenServe(t, identity, 8,
		following("ergo://allowed@h/log", "ergo://refused@h/log"))

	if answer.Body.Len() != 0 {
		t.Fatalf("the permitted URI was refused too: %s", answer.Body.String())
	}
	if got := followedURIs(followed); len(got) != 1 || got[0] != "ergo://allowed@h/log" {
		t.Errorf("the stream follows %v", got)
	}
}

func TestListenRefusesADeniedCapability(t *testing.T) {
	denied := Identity{Subject: "root", Ceiling: Ceiling{Deny: []string{inspect.CapLog}}}

	answer, followed := listenServe(t, denied, 8, following("ergo://n@h/log"))

	refusedWith(t, "denied capability", answer, mcpInvalidParams)
	if followed != nil {
		t.Error("a denied lens was followed")
	}
}

func TestListenRefusesAnUnparsedRequest(t *testing.T) {
	followed := 0
	gate := listenGate{
		next:     http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) { followed++ }),
		listener: withLimits(Listener{Port: 9911}),
		counts:   &refusalCounts{},
	}

	answer := httptest.NewRecorder()
	gate.ServeHTTP(answer, httptest.NewRequest(http.MethodPost, "/mcp", nil))

	refusedWith(t, "unparsed", answer, mcpInternalError)
	if followed != 0 {
		t.Error("an unparsed request reached the stream")
	}
}

func TestListenFilterIsAbsentWithoutTheGate(t *testing.T) {
	if _, carried := listenFilterOf(nil); carried {
		t.Error("a nil request carried a filter")
	}
	bare := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	if _, carried := listenFilterOf(bare); carried {
		t.Error("a request that never passed the gate carried a filter")
	}
}

func TestListenNamesRefusalsCanonically(t *testing.T) {
	identity := Identity{Subject: "root", Ceiling: Ceiling{Nodes: []string{"allowed@h"}}}
	const asked = "ergo://refused@h/log?limit=2&levels=error"
	const canonical = "ergo://refused@h/log?levels=error&limit=2"

	_, followed := listenServe(t, identity, 8, following("ergo://allowed@h/log", asked))
	if followed == nil {
		t.Fatal("nothing reached the stream")
	}
	if followed.refused[canonical] == "" {
		t.Errorf("the refusal is keyed as %v, want %q", followed.refused, canonical)
	}
	if _, raw := followed.refused[asked]; raw {
		t.Errorf("the refusal is keyed by what the caller wrote: %v", followed.refused)
	}

	answer, _ := listenServe(t, identity, 8, following("not-a-uri"))
	if refused := refusedBy(t, answer); refused["not-a-uri"] == nil {
		t.Errorf("an unparsable request is keyed as %v", refused)
	}
}
