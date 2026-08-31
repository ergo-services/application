package observer

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// resolved before anything is written: after 200 and text/event-stream nothing is refusable
type filterKey struct{}

type listenFilter struct {
	id      any
	uris    []mcpURI
	refused map[string]string
}

func listenFilterOf(request *http.Request) (listenFilter, bool) {
	if request == nil {
		return listenFilter{}, false
	}
	filter, ok := request.Context().Value(filterKey{}).(listenFilter)
	return filter, ok
}

// nothing here calls another process: this runs on an HTTP goroutine
type listenGate struct {
	next     http.Handler
	listener Listener
	counts   *refusalCounts
}

func (g listenGate) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	envelope, ok := envelopeOf(request)
	if ok == false {
		g.fail(writer, request, nil, mcpInternalError, "the request was not parsed", nil)
		return
	}

	var params struct {
		Notifications mcpFilter `json:"notifications"`
	}
	if len(envelope.Params) > 0 {
		if err := json.Unmarshal(envelope.Params, &params); err != nil {
			g.fail(writer, request, envelope.ID, mcpInvalidParams, "params are not readable", nil)
			return
		}
	}

	asked := params.Notifications.ResourceSubscriptions
	if len(asked) == 0 {
		g.fail(writer, request, envelope.ID, mcpInvalidParams,
			"notifications.resourceSubscriptions is required: a stream follows what you name", nil)
		return
	}

	limit := g.listener.MaxSubscriptions
	if limit > 0 && len(asked) > limit {
		g.fail(writer, request, envelope.ID, mcpInvalidParams,
			fmt.Sprintf("one stream follows at most %d resources", limit),
			map[string]any{"asked": len(asked)})
		return
	}

	identity, _ := identityOf(request)
	filter := listenFilter{id: envelope.ID, refused: map[string]string{}}

	for _, raw := range asked {
		uri, err := parseURI(raw)
		if err != nil {
			filter.refused[raw] = err.Error()
			continue
		}
		if err := followable(uri, identity); err != nil {
			filter.refused[uri.Canonical()] = err.Error()
			continue
		}
		filter.uris = append(filter.uris, uri)
	}

	// the last moment a status can be sent
	if len(filter.uris) == 0 {
		g.fail(writer, request, envelope.ID, mcpInvalidParams,
			"none of these resources can be followed",
			map[string]any{"refused": filter.refused})
		return
	}

	ctx := context.WithValue(request.Context(), filterKey{}, filter)
	g.next.ServeHTTP(writer, request.WithContext(ctx))
}

func (g listenGate) fail(writer http.ResponseWriter, request *http.Request, id any, code int, message string, data any) {
	status := mcpStatus(code)
	g.counts.note(status)
	writeJSON(writer, request, status, mcpFailed(id, code, message, data))
}

func followable(uri mcpURI, identity Identity) error {
	switch uri.Lens {
	case uriWordCluster:
		return fmt.Errorf("the cluster map is read, not followed")
	case uriWordJob:
		if identity.Subject == "" {
			return errNeedsCaller
		}
		return nil
	}
	return mcpRefusal(identity, uri)
}
