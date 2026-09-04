package observer

import (
	"net/http"
	"time"

	"ergo.services/application/observer/access"
	"ergo.services/ergo/app/system/inspect"
)

const (
	FeatureUI               = "observer.ui"
	FeatureSSE              = "observer.api.sse"
	FeatureActions          = "observer.api.actions"
	FeatureStatelessActions = "observer.api.stateless_actions"
	FeatureEnroll           = "observer.api.enroll"
	FeatureClusterLens      = "observer.cluster.lens"
	FeatureMCP              = "observer.mcp"
)

// CapForceProducer is asked for on top of the lens capability when a subscription would wake a
// sleeping producer. Checked against the ceiling, never routed: the manage prefix puts it
// outside ReadOnly while the request still goes to system_inspect.
const CapForceProducer = "manage.force_producer"

const CapDialRoute = "manage.dial_route"

func capabilitiesUnder(r inspect.ResponseGetCapabilities, c Ceiling) inspect.ResponseGetCapabilities {
	r.Capabilities = c.Filter(r.Capabilities)
	r.Manage = false
	for _, capability := range r.Capabilities {
		if access.Mutating(capability) {
			r.Manage = true
			break
		}
	}
	return r
}

func streamMailbox(maxSubscriptions int) int64 {
	return int64(maxSubscriptions*mailboxRounds + 8)
}

const sessionInitTimeout = 5 // seconds

const (
	streamHeartbeat = 5 * time.Second

	streamHeartbeatEvent = "heartbeat"
)

const enrollRateLimit = 1

type wireObserverCapabilities struct {
	Version  wireVersion `json:"v"`
	Auth     bool        `json:"auth"`
	ReadOnly bool        `json:"ro,omitempty"`
	Features []string    `json:"feat,omitempty"`
}

type wireEnrolled struct {
	ClusterID string `json:"cluster"`
}

func capabilitiesHandler(listener Listener, enrollment bool) http.Handler {
	answer := wireObserverCapabilities{
		Version:  wireVersionFrom(Version),
		Auth:     listener.Authorizer != nil,
		ReadOnly: listener.Ceiling.ReadOnly,
		Features: features(listener, enrollment),
	}

	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet && request.Method != http.MethodHead {
			refuse(writer, request, http.StatusMethodNotAllowed, nil)
			return
		}
		writeJSON(writer, request, http.StatusOK, answer)
	})
}

func features(listener Listener, enrollment bool) []string {
	names := []string{FeatureSSE, FeatureActions, FeatureClusterLens}
	if listener.UI.Disable == false {
		names = append(names, FeatureUI)
	}
	if listener.MCP.Disable == false {
		names = append(names, FeatureMCP)
	}
	if listener.Authorizer != nil {
		names = append(names, FeatureStatelessActions)
	}
	if enrollment {
		names = append(names, FeatureEnroll)
	}
	return names
}

type wireJSONRPCError struct {
	Version string `json:"jsonrpc"`
	ID      any    `json:"id"`
	Error   struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func jsonrpcError(status int, message string) wireJSONRPCError {
	out := wireJSONRPCError{Version: "2.0"}
	out.Error.Code = -32600
	if status == http.StatusNotImplemented {
		out.Error.Code = -32601
	}
	out.Error.Message = message
	return out
}

func describeCeiling(c Ceiling) string {
	switch {
	case c.ReadOnly:
		return "read-only"
	case len(c.Allow) > 0 || len(c.Deny) > 0 || len(c.Nodes) > 0:
		return "narrowed"
	}
	return "full"
}

func yesno(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}
