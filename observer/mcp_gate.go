package observer

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const mcpBodyLimit = 1 << 20

type envelopeKey struct{}

type mcpGate struct {
	listen   http.Handler
	post     http.Handler
	listener Listener
	counts   *refusalCounts
}

func (g mcpGate) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writer.Header().Set("Allow", "POST")
		g.counts.note(http.StatusMethodNotAllowed)
		refuse(writer, request, http.StatusMethodNotAllowed, nil)
		return
	}

	body, err := io.ReadAll(io.LimitReader(request.Body, mcpBodyLimit+1))
	if err != nil {
		g.fail(writer, request, nil, mcpParseError, "body is unreadable", nil)
		return
	}
	if len(body) > mcpBodyLimit {
		g.fail(writer, request, nil, mcpInvalidRequest,
			fmt.Sprintf("one request may weigh %d bytes", mcpBodyLimit), nil)
		return
	}

	if trimmed := bytes.TrimLeft(body, " \t\r\n"); len(trimmed) > 0 && trimmed[0] == '[' {
		g.fail(writer, request, nil, mcpInvalidRequest,
			"one POST carries one request, not a batch", nil)
		return
	}

	var envelope mcpEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		g.fail(writer, request, nil, mcpParseError, "body is not JSON", nil)
		return
	}
	if envelope.JSONRPC != "2.0" || envelope.Method == "" {
		g.fail(writer, request, envelope.ID, mcpInvalidRequest, "not a JSON-RPC 2.0 request", nil)
		return
	}

	if envelope.ID == nil {
		g.fail(writer, request, nil, mcpInvalidRequest,
			envelope.Method+" is a notification, and this endpoint takes none", nil)
		return
	}

	if code, message, data := transportRefusal(request, envelope); code != 0 {
		g.fail(writer, request, envelope.ID, code, message, data)
		return
	}

	ctx := context.WithValue(request.Context(), envelopeKey{}, envelope)
	ctx = context.WithValue(ctx, cacheTTLKey{}, g.listener.MCP.CacheTTL)
	request = request.WithContext(ctx)

	switch envelope.Method {
	case "server/discover":
		writeJSON(writer, request, http.StatusOK, mcpOK(envelope.ID, g.discovery()))

	case "subscriptions/listen":
		g.listen.ServeHTTP(writer, request)

	case "resources/read", "resources/list", "resources/templates/list",
		"tools/list", "tools/call":
		g.post.ServeHTTP(writer, request)

	default:
		g.fail(writer, request, envelope.ID, mcpMethodNotFound, envelope.Method+" is not served here", nil)
	}
}

func (g mcpGate) fail(writer http.ResponseWriter, request *http.Request, id any, code int, message string, data any) {
	status := mcpStatus(code)
	g.counts.note(status)
	writeJSON(writer, request, status, mcpFailed(id, code, message, data))
}

const mcpInstructions = "Start with ergo://cluster. It names every node this observer knows, " +
	"and every other resource and tool is addressed by a node name taken from there; for the " +
	"state of the cluster as a whole it is also the answer, saying which nodes are online, " +
	"how long they have been up and which ones fell out and why. Ask the capabilities tool " +
	"of a node before planning anything that writes. One question put to many nodes goes " +
	"through cluster_query or cluster_batch, which answer at once with the URI of a run: read " +
	"that URI, then read it again with since= to take only what has landed since. A goroutine " +
	"dump or a heap profile pauses the node it runs on, so ask for one once and read it " +
	"carefully. Where an answer carries " + mcpLegendKey + ", it says what its numbers are: " +
	"each key is the path to a field from the object holding the legend, [] for a step into a " +
	"list, and the value gives the unit, the meaning of a sentinel value, or the order of the " +
	"cells of a counter array."

func (g mcpGate) instructions() string {
	own := strings.TrimSpace(g.listener.MCP.Instructions)
	if own == "" {
		return mcpInstructions
	}
	return mcpInstructions + "\n\n" + own
}

func (g mcpGate) discovery() mcpDiscovery {
	return mcpDiscovery{
		ResultType:        mcpResultComplete,
		SupportedVersions: mcpSupportedVersions,
		Capabilities: mcpCapabilities{
			Resources: &mcpResourcesCap{Subscribe: true},
			Tools:     &mcpToolsCap{},
		},
		Instructions: g.instructions(),
		TTLMs:        int(g.listener.MCP.CacheTTL.Milliseconds()),
		CacheScope:   mcpCachePublic,
		Meta:         mcpAnswered(),
	}
}

func envelopeOf(request *http.Request) (mcpEnvelope, bool) {
	envelope, ok := request.Context().Value(envelopeKey{}).(mcpEnvelope)
	return envelope, ok
}

type mcpParams struct {
	Meta map[string]json.RawMessage `json:"_meta"`
	Name string                     `json:"name"`
	URI  string                     `json:"uri"`
}

func transportRefusal(request *http.Request, envelope mcpEnvelope) (int, string, any) {
	version := request.Header.Get(headerMCPVersion)
	if version == "" {
		return mcpHeaderMismatch, headerMCPVersion + " is required on every request",
			map[string]any{"header": headerMCPVersion}
	}

	if version != mcpProtocolVersion {
		return mcpUnsupportedVersion, "unsupported protocol version",
			map[string]any{"supported": mcpSupportedVersions, "requested": version}
	}

	if acceptsBoth(request.Header.Get("Accept")) == false {
		return mcpNotAcceptable,
			"Accept must list both " + mcpMimeJSON + " and " + mcpMimeStream,
			map[string]any{"header": "Accept"}
	}

	var params mcpParams
	if len(envelope.Params) > 0 {
		if err := json.Unmarshal(envelope.Params, &params); err != nil {
			return mcpInvalidParams, "params are not readable", nil
		}
	}

	for _, key := range []string{metaProtocolVersion, metaClientCapabilities} {
		if _, carried := params.Meta[key]; carried == false {
			return mcpInvalidParams, "_meta." + key + " is required on every request",
				map[string]any{"missing": key}
		}
	}

	var carried string
	if err := json.Unmarshal(params.Meta[metaProtocolVersion], &carried); err != nil {
		return mcpInvalidParams, "_meta." + metaProtocolVersion + " is not a string", nil
	}
	if carried != version {
		return mcpHeaderMismatch,
			fmt.Sprintf("%s says %s and the body says %s", headerMCPVersion, version, carried),
			map[string]any{"header": headerMCPVersion}
	}

	switch method := request.Header.Get(headerMCPMethod); {
	case method == "":
		return mcpHeaderMismatch, headerMCPMethod + " is required on every request",
			map[string]any{"header": headerMCPMethod}
	case method != envelope.Method:
		return mcpHeaderMismatch,
			fmt.Sprintf("%s says %s and the body says %s", headerMCPMethod, method, envelope.Method),
			map[string]any{"header": headerMCPMethod}
	}

	switch envelope.Method {
	case "tools/call", "resources/read":
		name := request.Header.Get(headerMCPName)
		if name == "" {
			return mcpHeaderMismatch, headerMCPName + " is required on " + envelope.Method,
				map[string]any{"header": headerMCPName}
		}
		decoded, ok := decodeHeaderValue(name)
		if ok == false {
			return mcpHeaderMismatch, headerMCPName + " is not decodable",
				map[string]any{"header": headerMCPName}
		}
		if body := mirrorName(params); decoded != body {
			return mcpHeaderMismatch,
				fmt.Sprintf("%s says %q and the body says %q", headerMCPName, decoded, body),
				map[string]any{"header": headerMCPName}
		}
	}
	return 0, "", nil
}

func mirrorName(params mcpParams) string {
	if params.Name != "" {
		return params.Name
	}
	return params.URI
}

func acceptsBoth(header string) bool {
	json, stream := false, false
	for _, entry := range strings.Split(header, ",") {
		media, _, _ := strings.Cut(entry, ";")
		switch strings.ToLower(strings.TrimSpace(media)) {
		case "*/*":
			json, stream = true, true
		case "application/*":
			json = true
		case "text/*":
			stream = true
		case mcpMimeJSON:
			json = true
		case mcpMimeStream:
			stream = true
		}
	}
	return json && stream
}

func decodeHeaderValue(value string) (string, bool) {
	if len(value) < len(mcpBase64Prefix)+len(mcpBase64Suffix) ||
		strings.HasPrefix(value, mcpBase64Prefix) == false ||
		strings.HasSuffix(value, mcpBase64Suffix) == false {
		return value, true
	}

	decoded, err := base64.StdEncoding.DecodeString(
		value[len(mcpBase64Prefix) : len(value)-len(mcpBase64Suffix)])
	if err != nil {
		return "", false
	}
	return string(decoded), true
}
