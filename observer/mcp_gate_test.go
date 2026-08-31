package observer

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newGate() (mcpGate, *int) {
	served := 0
	reached := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) { served++ })
	return mcpGate{
		listen:   reached,
		post:     reached,
		listener: withLimits(Listener{Port: 9911}),
		counts:   &refusalCounts{},
	}, &served
}

// a test that wants a broken request breaks one thing after building it
func mcpConforming(method string, id any, params map[string]any) (string, map[string]string) {
	if params == nil {
		params = map[string]any{}
	}
	params["_meta"] = map[string]any{
		metaProtocolVersion:    mcpProtocolVersion,
		metaClientCapabilities: map[string]any{},
	}

	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": id, "method": method, "params": params,
	})
	if err != nil {
		panic(err)
	}

	headers := map[string]string{
		"Accept":         mcpMimeJSON + ", " + mcpMimeStream,
		headerMCPVersion: mcpProtocolVersion,
		headerMCPMethod:  method,
	}
	switch method {
	case "tools/call":
		if name, ok := params["name"].(string); ok {
			headers[headerMCPName] = name
		}
	case "resources/read":
		if uri, ok := params["uri"].(string); ok {
			headers[headerMCPName] = uri
		}
	}
	return string(body), headers
}

// only a test needs this: a server reads these headers and never writes one
func encodeHeaderValue(value string) string {
	return mcpBase64Prefix + base64.StdEncoding.EncodeToString([]byte(value)) + mcpBase64Suffix
}

func gateCall(gate mcpGate, method string, body string, headers map[string]string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, "/mcp", strings.NewReader(body))
	request.Header.Set("Content-Type", mcpMimeJSON)
	request.Header.Set("Accept", mcpMimeJSON+", "+mcpMimeStream)
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	recorder := httptest.NewRecorder()
	gate.ServeHTTP(recorder, request)
	return recorder
}

func failureOf(t *testing.T, recorder *httptest.ResponseRecorder) mcpFailure {
	t.Helper()
	var out mcpFailure
	if err := json.Unmarshal(recorder.Body.Bytes(), &out); err != nil {
		t.Fatalf("body %q: %s", recorder.Body.String(), err)
	}
	return out
}

func refusedWith(t *testing.T, name string, answer *httptest.ResponseRecorder, code int) mcpFailure {
	t.Helper()
	if answer.Code != mcpStatus(code) {
		t.Errorf("%s: status %d, want %d", name, answer.Code, mcpStatus(code))
	}
	failure := failureOf(t, answer)
	if failure.Error.Code != code {
		t.Errorf("%s: code %d (%s), want %d", name, failure.Error.Code, failure.Error.Message, code)
	}
	return failure
}

func TestGateTakesPostOnly(t *testing.T) {
	gate, served := newGate()

	for _, method := range []string{http.MethodGet, http.MethodDelete, http.MethodPut} {
		answer := gateCall(gate, method, "", nil)
		if answer.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s got %d, want 405", method, answer.Code)
		}
		if answer.Header().Get("Allow") != "POST" {
			t.Errorf("%s: Allow is %q", method, answer.Header().Get("Allow"))
		}
	}
	if *served != 0 {
		t.Errorf("%d requests reached the surface", *served)
	}
}

func TestGateRefusesWhatIsNotJSONRPC(t *testing.T) {
	gate, _ := newGate()

	cases := []struct {
		body string
		code int
	}{
		{"not json", mcpParseError},
		{`{"jsonrpc":"1.0","method":"initialize"}`, mcpInvalidRequest},
		{`{"jsonrpc":"2.0"}`, mcpInvalidRequest},
	}
	for _, c := range cases {
		answer := gateCall(gate, http.MethodPost, c.body, nil)
		refusedWith(t, c.body, answer, c.code)
	}
}

// a batch is well-formed JSON-RPC, and an oversized body is well-formed until it is cut:
// neither is a parse error
func TestGateTakesOneRequestPerPost(t *testing.T) {
	gate, served := newGate()

	single, headers := mcpConforming("tools/list", 1, nil)
	refusedWith(t, "batch", gateCall(gate, http.MethodPost,
		"  [ "+single+" ]", headers), mcpInvalidRequest)

	oversized, headers := mcpConforming("tools/call", 1, map[string]any{
		"name":      "kill",
		"arguments": map[string]any{"note": strings.Repeat("x", mcpBodyLimit)},
	})
	refusedWith(t, "oversized",
		gateCall(gate, http.MethodPost, oversized, headers), mcpInvalidRequest)

	if *served != 0 {
		t.Errorf("%d of these reached the surface", *served)
	}
}

func TestGateRefusesANotification(t *testing.T) {
	gate, served := newGate()

	body, headers := mcpConforming("notifications/initialized", nil, nil)
	answer := gateCall(gate, http.MethodPost, body, headers)

	failure := refusedWith(t, "notification", answer, mcpInvalidRequest)
	if failure.ID != nil {
		t.Errorf("the refusal carries an id: %#v", failure.ID)
	}
	if *served != 0 {
		t.Error("a notification reached the surface")
	}
}

func TestGateRequiresTheProtocolVersionHeader(t *testing.T) {
	gate, served := newGate()

	body, headers := mcpConforming("tools/list", 1, nil)
	delete(headers, headerMCPVersion)

	answer := gateCall(gate, http.MethodPost, body, headers)
	failure := refusedWith(t, "no version header", answer, mcpHeaderMismatch)

	data, _ := failure.Error.Data.(map[string]any)
	if data["header"] != headerMCPVersion {
		t.Errorf("the refusal names %#v", data["header"])
	}
	if *served != 0 {
		t.Error("a request without the version header reached the surface")
	}
}

func TestGateAnswersAnUnsupportedVersion(t *testing.T) {
	gate, _ := newGate()

	body, headers := mcpConforming("tools/list", 1, nil)
	headers[headerMCPVersion] = "2025-06-18"

	failure := refusedWith(t, "old revision",
		gateCall(gate, http.MethodPost, body, headers), mcpUnsupportedVersion)

	data, _ := failure.Error.Data.(map[string]any)
	listed, _ := data["supported"].([]any)
	if len(listed) != 1 || listed[0] != mcpProtocolVersion {
		t.Errorf("the refusal offers %#v", data["supported"])
	}
	if data["requested"] != "2025-06-18" {
		t.Errorf("the refusal echoes %#v as asked for", data["requested"])
	}
}

func TestGateRefusesAVersionThatDisagreesWithTheBody(t *testing.T) {
	gate, served := newGate()

	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/list",
		"params": map[string]any{"_meta": map[string]any{
			metaProtocolVersion:    "2025-11-25",
			metaClientCapabilities: map[string]any{},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	answer := gateCall(gate, http.MethodPost, string(body), map[string]string{
		headerMCPVersion: mcpProtocolVersion,
		headerMCPMethod:  "tools/list",
	})
	refusedWith(t, "version header against body", answer, mcpHeaderMismatch)
	if *served != 0 {
		t.Error("a disagreeing version reached the surface")
	}
}

func TestGateRequiresTheRequestMetadata(t *testing.T) {
	gate, served := newGate()

	cases := []struct {
		name   string
		params map[string]any
	}{
		{"no params at all", nil},
		{"no _meta", map[string]any{}},
		{"no protocolVersion", map[string]any{
			metaClientCapabilities: map[string]any{},
		}},
		{"no clientCapabilities", map[string]any{
			metaProtocolVersion: mcpProtocolVersion,
		}},
	}

	for _, c := range cases {
		envelope := map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"}
		switch {
		case c.params == nil:
		case len(c.params) == 0:
			envelope["params"] = map[string]any{}
		default:
			envelope["params"] = map[string]any{"_meta": c.params}
		}
		body, err := json.Marshal(envelope)
		if err != nil {
			t.Fatal(err)
		}

		answer := gateCall(gate, http.MethodPost, string(body), map[string]string{
			headerMCPVersion: mcpProtocolVersion,
			headerMCPMethod:  "tools/list",
		})
		refusedWith(t, c.name, answer, mcpInvalidParams)
	}
	if *served != 0 {
		t.Errorf("%d requests without metadata reached the surface", *served)
	}
}

func TestGateRequiresAnAcceptItCanAnswer(t *testing.T) {
	gate, served := newGate()

	for _, accept := range []string{"", "text/plain", mcpMimeJSON, mcpMimeStream} {
		body, headers := mcpConforming("subscriptions/listen", 1, nil)
		headers["Accept"] = accept

		failure := refusedWith(t, "Accept: "+accept,
			gateCall(gate, http.MethodPost, body, headers), mcpNotAcceptable)

		data, _ := failure.Error.Data.(map[string]any)
		if data["header"] != "Accept" {
			t.Errorf("Accept %q: the refusal names %#v", accept, data["header"])
		}
	}
	if *served != 0 {
		t.Errorf("%d requests this endpoint cannot answer reached the surface", *served)
	}

	for _, accept := range []string{
		mcpMimeJSON + ", " + mcpMimeStream,
		mcpMimeStream + ";q=0.9, " + mcpMimeJSON + ";q=1",
		"*/*",
		"application/*, text/*",
	} {
		body, headers := mcpConforming("tools/list", 1, nil)
		headers["Accept"] = accept

		if answer := gateCall(gate, http.MethodPost, body, headers); answer.Code != 200 {
			t.Errorf("Accept %q was refused with %d: %s", accept, answer.Code, answer.Body)
		}
	}
	if *served != 4 {
		t.Errorf("%d of 4 usable requests reached the surface", *served)
	}
}

func TestGateRequiresTheMirroringHeaders(t *testing.T) {
	gate, served := newGate()

	cases := []struct {
		name   string
		method string
		params map[string]any
		drop   string
	}{
		{"no method header", "tools/list", nil, headerMCPMethod},
		{"no name on a call", "tools/call",
			map[string]any{"name": "kill", "arguments": map[string]any{}}, headerMCPName},
		{"no name on a read", "resources/read",
			map[string]any{"uri": "ergo://n@h/log"}, headerMCPName},
	}

	for _, c := range cases {
		body, headers := mcpConforming(c.method, 1, c.params)
		delete(headers, c.drop)

		failure := refusedWith(t, c.name,
			gateCall(gate, http.MethodPost, body, headers), mcpHeaderMismatch)

		data, _ := failure.Error.Data.(map[string]any)
		if data["header"] != c.drop {
			t.Errorf("%s: the refusal names %#v", c.name, data["header"])
		}
	}

	// the methods that name nothing must not be asked for a name
	body, headers := mcpConforming("tools/list", 1, nil)
	if answer := gateCall(gate, http.MethodPost, body, headers); *served != 1 {
		t.Errorf("tools/list was refused with %d: %s", answer.Code, answer.Body.String())
	}
}

func TestGateRefusesAMirrorThatDisagrees(t *testing.T) {
	gate, served := newGate()

	cases := []struct {
		name   string
		method string
		params map[string]any
		header string
		value  string
	}{
		{"method disagrees", "tools/list", nil, headerMCPMethod, "resources/read"},
		{"tool name disagrees", "tools/call",
			map[string]any{"name": "kill", "arguments": map[string]any{}}, headerMCPName, "spawn"},
		{"uri disagrees", "resources/read",
			map[string]any{"uri": "ergo://n@h/log"}, headerMCPName, "ergo://other@h/log"},
	}

	for _, c := range cases {
		body, headers := mcpConforming(c.method, 1, c.params)
		headers[c.header] = c.value

		failure := refusedWith(t, c.name,
			gateCall(gate, http.MethodPost, body, headers), mcpHeaderMismatch)

		data, _ := failure.Error.Data.(map[string]any)
		if data["header"] != c.header {
			t.Errorf("%s: the refusal names %#v", c.name, data["header"])
		}
	}
	if *served != 0 {
		t.Errorf("%d disagreeing requests reached the surface", *served)
	}
}

func TestGateDecodesAnEncodedName(t *testing.T) {
	gate, served := newGate()
	uri := "ergo://узел@localhost/log"

	body, headers := mcpConforming("resources/read", 1, map[string]any{"uri": uri})
	headers[headerMCPName] = encodeHeaderValue(uri)

	if answer := gateCall(gate, http.MethodPost, body, headers); *served != 1 {
		t.Fatalf("an encoded name was refused with %d: %s", answer.Code, answer.Body.String())
	}

	headers[headerMCPName] = mcpBase64Prefix + "не base64" + mcpBase64Suffix
	refusedWith(t, "undecodable name",
		gateCall(gate, http.MethodPost, body, headers), mcpHeaderMismatch)
}

func TestGateIgnoresAnUndeclaredParamHeader(t *testing.T) {
	gate, served := newGate()

	body, headers := mcpConforming("tools/call", 1,
		map[string]any{"name": "kill", "arguments": map[string]any{"node": "n@h"}})
	headers["Mcp-Param-Tenant"] = "whoever-routes-us"

	if answer := gateCall(gate, http.MethodPost, body, headers); *served != 1 {
		t.Errorf("an undeclared param header was refused with %d: %s",
			answer.Code, answer.Body.String())
	}
}

func TestGateUnknownMethod(t *testing.T) {
	gate, served := newGate()

	body, headers := mcpConforming("resources/subscribe", 7, nil)
	answer := gateCall(gate, http.MethodPost, body, headers)

	failure := refusedWith(t, "resources/subscribe", answer, mcpMethodNotFound)
	if answer.Code != http.StatusNotFound {
		t.Errorf("status %d, want 404", answer.Code)
	}
	if failure.ID != float64(7) {
		t.Errorf("the id came back as %#v", failure.ID)
	}
	if *served != 0 {
		t.Error("an unknown method reached the surface")
	}
}

func TestGateDiscoverAnswersItself(t *testing.T) {
	gate, served := newGate()

	body, headers := mcpConforming("server/discover", "a", nil)
	answer := gateCall(gate, http.MethodPost, body, headers)
	if answer.Code != http.StatusOK {
		t.Fatalf("status %d: %s", answer.Code, answer.Body.String())
	}
	var out struct {
		ID     string       `json:"id"`
		Result mcpDiscovery `json:"result"`
	}
	if err := json.Unmarshal(answer.Body.Bytes(), &out); err != nil {
		t.Fatalf("body %q: %s", answer.Body.String(), err)
	}
	if out.ID != "a" {
		t.Errorf("the id came back as %q", out.ID)
	}
	if out.Result.ResultType != mcpResultComplete {
		t.Errorf("resultType is %q", out.Result.ResultType)
	}
	if len(out.Result.SupportedVersions) != 1 || out.Result.SupportedVersions[0] != mcpProtocolVersion {
		t.Errorf("the revisions offered are %#v", out.Result.SupportedVersions)
	}
	if out.Result.TTLMs <= 0 || out.Result.CacheScope != mcpCachePublic {
		t.Errorf("the caching hints are %d/%q", out.Result.TTLMs, out.Result.CacheScope)
	}
	if out.Result.Meta.ServerInfo.Name == "" || out.Result.Meta.ServerInfo.Version == "" {
		t.Errorf("the server did not name itself: %#v", out.Result.Meta.ServerInfo)
	}
	if out.Result.Capabilities.Resources == nil || out.Result.Capabilities.Resources.Subscribe == false {
		t.Error("following a resource is not offered")
	}
	if out.Result.Capabilities.Tools == nil {
		t.Error("tools are not offered")
	}
	if *served != 0 {
		t.Error("discovery was handed on")
	}
}

func TestGateDiscoverCarriesInstructions(t *testing.T) {
	own := "shop-basket-* holds the baskets; a checkout flow starts at shop-gateway-*."

	read := func(gate mcpGate) string {
		t.Helper()
		body, headers := mcpConforming("server/discover", 1, nil)
		answer := gateCall(gate, http.MethodPost, body, headers)

		var out struct {
			Result mcpDiscovery `json:"result"`
		}
		if err := json.Unmarshal(answer.Body.Bytes(), &out); err != nil {
			t.Fatalf("body %q: %s", answer.Body.String(), err)
		}
		return out.Result.Instructions
	}

	bare, _ := newGate()
	if said := read(bare); said != mcpInstructions {
		t.Errorf("a listener that says nothing answered %q", said)
	}

	told, _ := newGate()
	told.listener.MCP.Instructions = "  " + own + "  "

	said := read(told)
	if strings.Contains(said, mcpInstructions) == false {
		t.Error("the deployment's own text replaced the directions instead of adding to them")
	}
	if strings.Contains(said, own) == false {
		t.Errorf("the deployment's own text is missing: %q", said)
	}
	if strings.HasSuffix(said, own) == false {
		t.Errorf("the deployment's text was not trimmed or not last: %q", said)
	}
}

// a client outlives a restart, and until this expires it calls tools with the arguments of
// the binary it first met
func TestGateCacheTTLIsTheListenersOwn(t *testing.T) {
	gate, _ := newGate()
	gate.listener.MCP.CacheTTL = 3 * time.Second

	body, headers := mcpConforming("server/discover", 1, nil)
	answer := gateCall(gate, http.MethodPost, body, headers)

	var out struct {
		Result mcpDiscovery `json:"result"`
	}
	if err := json.Unmarshal(answer.Body.Bytes(), &out); err != nil {
		t.Fatalf("body %q: %s", answer.Body.String(), err)
	}
	if out.Result.TTLMs != 3000 {
		t.Errorf("discovery promises %dms, want 3000", out.Result.TTLMs)
	}

	parsed := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	parsed = parsed.WithContext(
		context.WithValue(parsed.Context(), cacheTTLKey{}, 3*time.Second))
	if got := mcpCacheTTL(parsed); got != 3000 {
		t.Errorf("the worker would promise %dms", got)
	}

	// zero would mean immediately stale and have every client re-read every listing
	if got := mcpCacheTTL(httptest.NewRequest(http.MethodPost, "/mcp", nil)); got <= 0 {
		t.Errorf("a request without the gate promises %dms", got)
	}
	if withLimits(Listener{Port: 9911}).MCP.CacheTTL != defaultMCPCacheTTL {
		t.Error("an unset CacheTTL did not take the default")
	}
}

func TestGateHasNoInitialize(t *testing.T) {
	gate, served := newGate()

	body, headers := mcpConforming("initialize", 1, nil)
	answer := gateCall(gate, http.MethodPost, body, headers)

	refusedWith(t, "initialize", answer, mcpMethodNotFound)
	if answer.Code != http.StatusNotFound {
		t.Errorf("status %d, want 404", answer.Code)
	}
	if *served != 0 {
		t.Error("initialize reached the surface")
	}
}

func TestGatePassesTheParsedRequest(t *testing.T) {
	var seen mcpEnvelope
	gate := mcpGate{
		post: http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
			seen, _ = envelopeOf(request)
		}),
		listener: withLimits(Listener{Port: 9911}),
		counts:   &refusalCounts{},
	}

	body, headers := mcpConforming("resources/read", 3, map[string]any{"uri": "ergo://n@h/log"})
	gateCall(gate, http.MethodPost, body, headers)

	if seen.Method != "resources/read" || seen.ID != float64(3) {
		t.Fatalf("the surface saw %#v", seen)
	}
	var params struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(seen.Params, &params); err != nil {
		t.Fatal(err)
	}
	if params.URI != "ergo://n@h/log" {
		t.Errorf("params came through as %q", params.URI)
	}
}

func TestGateListenGoesToTheStream(t *testing.T) {
	listened := 0
	posted := 0
	gate := mcpGate{
		listen:   http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) { listened++ }),
		post:     http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) { posted++ }),
		listener: withLimits(Listener{Port: 9911}),
		counts:   &refusalCounts{},
	}

	body, headers := mcpConforming("subscriptions/listen", 1, nil)
	gateCall(gate, http.MethodPost, body, headers)
	if listened != 1 || posted != 0 {
		t.Errorf("listen=%d post=%d", listened, posted)
	}

	body, headers = mcpConforming("resources/read", 2, map[string]any{"uri": "ergo://n@h/log"})
	gateCall(gate, http.MethodPost, body, headers)
	if listened != 1 || posted != 1 {
		t.Errorf("listen=%d post=%d", listened, posted)
	}
}

// the rows are the encoding examples of the revision, verbatim
func TestDecodeHeaderValue(t *testing.T) {
	cases := []struct {
		header string
		want   string
	}{
		{"us-west1", "us-west1"},
		{"=?base64?SGVsbG8sIOS4lueVjA==?=", "Hello, 世界"},
		{"=?base64?IHBhZGRlZCA=?=", " padded "},
		{"=?base64?bGluZTEKbGluZTI=?=", "line1\nline2"},
		{"=?base64?PT9iYXNlNjQ/bGl0ZXJhbD89?=", "=?base64?literal?="},
		{mcpBase64Prefix + mcpBase64Suffix, ""},
	}

	for _, c := range cases {
		got, ok := decodeHeaderValue(c.header)
		if ok == false {
			t.Errorf("%q did not decode", c.header)
			continue
		}
		if got != c.want {
			t.Errorf("%q decoded to %q, want %q", c.header, got, c.want)
		}
	}

	for _, broken := range []string{"=?base64?not base64!?=", "=?base64?=?=?="} {
		if got, ok := decodeHeaderValue(broken); ok {
			t.Errorf("%q decoded to %q, want a refusal", broken, got)
		}
	}

	for _, plain := range []string{"=?BASE64?SGk=?=", "=?base64?SGk=", "SGk=?=", "?=", "=?base64?"} {
		if got, ok := decodeHeaderValue(plain); ok == false || got != plain {
			t.Errorf("%q came back as %q (%t), want itself", plain, got, ok)
		}
	}
}
