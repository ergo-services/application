package observer

import (
	"encoding/json"
	"errors"
	"net/http"
)

const mcpProtocolVersion = "2026-07-28"

var mcpSupportedVersions = []string{mcpProtocolVersion}

const (
	headerMCPVersion = "MCP-Protocol-Version"
	headerMCPMethod  = "Mcp-Method"
	headerMCPName    = "Mcp-Name"
)

const (
	mcpBase64Prefix = "=?base64?"
	mcpBase64Suffix = "?="
)

const mcpResultComplete = "complete"

const (
	mcpCachePublic  = "public"
	mcpCachePrivate = "private"
)

const (
	metaProtocolVersion    = "io.modelcontextprotocol/protocolVersion"
	metaClientCapabilities = "io.modelcontextprotocol/clientCapabilities"
	metaSubscriptionID     = "io.modelcontextprotocol/subscriptionId"
)

const metaRefused = "services.ergo/refused"

// InvalidParams is about what the caller named, InternalError about this surface failing to
// answer a request that was fine, InvalidRequest about the shape of the request, and
// MethodNotFound only about a method nobody implements.
const (
	mcpParseError         = -32700
	mcpInvalidRequest     = -32600
	mcpMethodNotFound     = -32601
	mcpInvalidParams      = -32602
	mcpInternalError      = -32603
	mcpHeaderMismatch     = -32020
	mcpUnsupportedVersion = -32022
	mcpNotAcceptable      = -32023
)

type mcpEnvelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type mcpResult struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id,omitempty"`
	Result  any    `json:"result"`
}

type mcpFailure struct {
	JSONRPC string       `json:"jsonrpc"`
	ID      any          `json:"id,omitempty"`
	Error   mcpErrorBody `json:"error"`
}

type mcpErrorBody struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func mcpOK(id any, result any) mcpResult {
	return mcpResult{JSONRPC: "2.0", ID: id, Result: result}
}

func mcpFailed(id any, code int, message string, data any) mcpFailure {
	return mcpFailure{
		JSONRPC: "2.0",
		ID:      id,
		Error:   mcpErrorBody{Code: code, Message: message, Data: data},
	}
}

func mcpStatus(code int) int {
	switch code {
	case mcpMethodNotFound:
		return http.StatusNotFound
	case mcpInternalError:
		return http.StatusInternalServerError
	case mcpNotAcceptable:
		return http.StatusNotAcceptable
	case mcpHeaderMismatch, mcpUnsupportedVersion,
		mcpParseError, mcpInvalidRequest, mcpInvalidParams:
		return http.StatusBadRequest
	}
	return http.StatusBadRequest
}

type mcpFilter struct {
	ToolsListChanged      bool     `json:"toolsListChanged,omitempty"`
	PromptsListChanged    bool     `json:"promptsListChanged,omitempty"`
	ResourcesListChanged  bool     `json:"resourcesListChanged,omitempty"`
	ResourceSubscriptions []string `json:"resourceSubscriptions,omitempty"`
}

type mcpFrameMeta struct {
	SubscriptionID any               `json:"io.modelcontextprotocol/subscriptionId"`
	Refused        map[string]string `json:"services.ergo/refused,omitempty"`
}

type mcpNotification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params"`
}

func mcpNotify(method string, params any) mcpNotification {
	return mcpNotification{JSONRPC: "2.0", Method: method, Params: params}
}

const (
	mcpResourceUpdated  = "notifications/resources/updated"
	mcpSubscriptionsAck = "notifications/subscriptions/acknowledged"
	mcpCancelled        = "notifications/cancelled"
)

var errNeedsCaller = errors.New(
	"a key belongs to a caller, so this needs an authorized listener")

type mcpResourceRef struct {
	Meta mcpFrameMeta `json:"_meta"`
	URI  string       `json:"uri"`
}

type mcpAcknowledged struct {
	Meta          mcpFrameMeta `json:"_meta"`
	Notifications mcpFilter    `json:"notifications"`
}

type mcpCancelledParams struct {
	Meta      mcpFrameMeta `json:"_meta"`
	RequestID any          `json:"requestId"`
	Reason    string       `json:"reason,omitempty"`
}

type mcpListenResult struct {
	ResultType string       `json:"resultType"`
	Meta       mcpFrameMeta `json:"_meta"`
}

type mcpToolResult struct {
	ResultType string        `json:"resultType"`
	Content    []any         `json:"content"`
	IsError    bool          `json:"isError,omitempty"`
	Meta       mcpResultMeta `json:"_meta"`
}

func toolBlocks(lens string, uri string, rendered mcpRendered) []any {
	out := []any{mcpText(rendered.Text)}
	if uri != "" {
		out = append(out, mcpLink(lens, uri))
	}
	return out
}

type mcpResourceLink struct {
	Type     string `json:"type"`
	URI      string `json:"uri"`
	Name     string `json:"name"`
	Title    string `json:"title,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
}

const jobTitle = "A run over many nodes"

func mcpLink(lens string, uri string) mcpResourceLink {
	out := mcpResourceLink{Type: "resource_link", URI: uri, Name: lens, MimeType: mcpMimeJSON}
	switch spec, known := lensSpecOf(lens); {
	case known:
		out.Title = spec.Title
	case lens == uriWordJob:
		out.Title = jobTitle
	}
	return out
}

type mcpResourceEntry struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	MimeType    string `json:"mimeType,omitempty"`
	Description string `json:"description,omitempty"`
}

type mcpResourceList struct {
	ResultType string             `json:"resultType"`
	Resources  []mcpResourceEntry `json:"resources"`
	TTLMs      int                `json:"ttlMs"`
	CacheScope string             `json:"cacheScope"`
	Meta       mcpResultMeta      `json:"_meta"`
}

type mcpToolList struct {
	ResultType string        `json:"resultType"`
	Tools      []mcpTool     `json:"tools"`
	TTLMs      int           `json:"ttlMs"`
	CacheScope string        `json:"cacheScope"`
	Meta       mcpResultMeta `json:"_meta"`
}

type mcpResourceTemplate struct {
	URITemplate string `json:"uriTemplate"`
	Name        string `json:"name"`
	Title       string `json:"title,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
	Description string `json:"description,omitempty"`
}

type mcpTemplateList struct {
	ResultType        string                `json:"resultType"`
	ResourceTemplates []mcpResourceTemplate `json:"resourceTemplates"`
	TTLMs             int                   `json:"ttlMs"`
	CacheScope        string                `json:"cacheScope"`
	Meta              mcpResultMeta         `json:"_meta"`
}

type mcpContents struct {
	ResultType string               `json:"resultType"`
	Contents   []mcpResourceContent `json:"contents"`
	TTLMs      int                  `json:"ttlMs"`
	CacheScope string               `json:"cacheScope"`
	Meta       *mcpReadMeta         `json:"_meta,omitempty"`
}

const ttlReading = 1000 // ms

type mcpTextContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func mcpText(text string) mcpTextContent {
	return mcpTextContent{Type: "text", Text: text}
}

type mcpResourceContent struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType,omitempty"`
	Text     string `json:"text"`
}

type mcpReadMeta struct {
	ServerInfo   mcpServerName `json:"io.modelcontextprotocol/serverInfo"`
	Seq          int64         `json:"services.ergo/seq"`
	LastModified string        `json:"services.ergo/lastModified,omitempty"`
	Dropped      bool          `json:"services.ergo/dropped,omitempty"`
	NextSeq      string        `json:"services.ergo/nextSeq,omitempty"`
}

type mcpDiscovery struct {
	ResultType        string          `json:"resultType"`
	SupportedVersions []string        `json:"supportedVersions"`
	Capabilities      mcpCapabilities `json:"capabilities"`
	Instructions      string          `json:"instructions,omitempty"`
	TTLMs             int             `json:"ttlMs"`
	CacheScope        string          `json:"cacheScope"`
	Meta              mcpResultMeta   `json:"_meta"`
}

type mcpResultMeta struct {
	ServerInfo mcpServerName `json:"io.modelcontextprotocol/serverInfo"`
}

func mcpIdentity() mcpServerName {
	return mcpServerName{
		Name:    "ergo.observer",
		Title:   Version.Name,
		Version: mcpServerVersion(),
	}
}

func mcpAnswered() mcpResultMeta {
	return mcpResultMeta{ServerInfo: mcpIdentity()}
}

type mcpServerName struct {
	Name    string `json:"name"`
	Title   string `json:"title,omitempty"`
	Version string `json:"version"`
}

func mcpServerVersion() string {
	if Version.Commit == "" {
		return Version.Release
	}
	return Version.Release + "+" + Version.Commit
}

type mcpCapabilities struct {
	Resources *mcpResourcesCap `json:"resources,omitempty"`
	Tools     *mcpToolsCap     `json:"tools,omitempty"`
}

type (
	mcpResourcesCap struct {
		Subscribe   bool `json:"subscribe"`
		ListChanged bool `json:"listChanged"`
	}
	mcpToolsCap struct {
		ListChanged bool `json:"listChanged"`
	}
)
