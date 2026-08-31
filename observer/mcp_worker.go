package observer

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"ergo.services/application/observer/access"
	"ergo.services/ergo/app/system/inspect"
	"ergo.services/ergo/gen"
)

// seconds, larger than the owner's wire budget so a timeout here means the owner is stuck
const mcpReadTimeout = 10

func agentPayload(result any, action string, capability string, ceiling Ceiling) any {
	if access.Mutating(capability) {
		return map[string]any{"done": action}
	}
	if report, ok := result.(inspect.ResponseGetCapabilities); ok {
		return capabilitiesUnder(report, ceiling)
	}
	return result
}

func (w *postWorker) handleMCP(writer http.ResponseWriter, request *http.Request) {
	envelope, ok := envelopeOf(request)
	if ok == false {
		writeJSON(writer, request, http.StatusInternalServerError,
			mcpFailed(nil, mcpInternalError, "the request was not parsed", nil))
		return
	}

	// a closed connection is a cancelled request: an agent that gave up must not still cost
	// the observed node a goroutine dump
	if request.Context().Err() != nil {
		return
	}

	// nothing here is paginated, and a silently ignored cursor would leave a client reading
	// the first page again believing it had asked for the next one
	switch envelope.Method {
	case "resources/list", "resources/templates/list", "tools/list":
		if cursor, given := mcpCursorOf(envelope); given {
			w.mcpFail(writer, request, envelope.ID, mcpInvalidParams,
				"this list is one page and issues no cursor",
				map[string]any{"cursor": cursor})
			return
		}
	}

	switch envelope.Method {
	case "resources/read":
		w.mcpRead(writer, request, envelope)
	case "resources/list":
		writeJSON(writer, request, http.StatusOK, mcpOK(envelope.ID, mcpResourceList{
			ResultType: mcpResultComplete,
			Resources: []mcpResourceEntry{{
				URI:      clusterURI,
				Name:     "cluster",
				MimeType: "application/json",
				Description: "The nodes this observer knows about. Every other resource is " +
					"addressed by a node name found here.",
			}},
			TTLMs:      mcpCacheTTL(request),
			CacheScope: mcpCachePrivate,
			Meta:       mcpAnswered(),
		}))

	case "resources/templates/list":
		identity, _ := identityOf(request)
		writeJSON(writer, request, http.StatusOK, mcpOK(envelope.ID, mcpTemplateList{
			ResultType:        mcpResultComplete,
			ResourceTemplates: mcpResourceTemplates(identity.Ceiling),
			TTLMs:             mcpCacheTTL(request),
			CacheScope:        mcpCachePrivate,
			Meta:              mcpAnswered(),
		}))

	case "tools/list":
		identity, _ := identityOf(request)
		writeJSON(writer, request, http.StatusOK, mcpOK(envelope.ID, mcpToolList{
			ResultType: mcpResultComplete,
			Tools:      toolEntries(identity.Ceiling),
			TTLMs:      mcpCacheTTL(request),
			CacheScope: mcpCachePrivate,
			Meta:       mcpAnswered(),
		}))

	case "tools/call":
		w.mcpToolCall(writer, request, envelope)

	default:
		w.mcpFail(writer, request, envelope.ID, mcpInternalError,
			envelope.Method+" was routed here and is not served here", nil)
	}
}

func mcpModifiedAt(at time.Time) string {
	if at.IsZero() {
		return ""
	}
	return at.UTC().Format(time.RFC3339Nano)
}

func mcpCursorOf(envelope mcpEnvelope) (string, bool) {
	if len(envelope.Params) == 0 {
		return "", false
	}
	var params struct {
		Cursor *string `json:"cursor"`
	}
	if err := json.Unmarshal(envelope.Params, &params); err != nil || params.Cursor == nil {
		return "", false
	}
	return *params.Cursor, true
}

func (w *postWorker) mcpRead(writer http.ResponseWriter, request *http.Request, envelope mcpEnvelope) {
	var params struct {
		URI string `json:"uri"`
	}
	if len(envelope.Params) > 0 {
		if err := json.Unmarshal(envelope.Params, &params); err != nil {
			w.mcpFail(writer, request, envelope.ID, mcpInvalidParams, "params are not readable", nil)
			return
		}
	}
	if params.URI == "" {
		w.mcpFail(writer, request, envelope.ID, mcpInvalidParams, "uri is required", nil)
		return
	}

	uri, err := parseURI(params.URI)
	if err != nil {
		w.mcpFail(writer, request, envelope.ID, mcpInvalidParams, err.Error(),
			map[string]any{"uri": params.URI})
		return
	}

	if uri.Lens == uriWordCluster {
		w.mcpCluster(writer, request, envelope)
		return
	}

	identity, _ := identityOf(request)

	// a job is never created by a read: that would restart a run already retired
	if uri.Lens == uriWordJob {
		if identity.Subject == "" {
			w.mcpFail(writer, request, envelope.ID, mcpInvalidParams, errNeedsCaller.Error(),
				map[string]any{"uri": uri.Canonical()})
			return
		}

		reading, err := w.CallWithTimeout(jobName(uri.Key, qualified(identity)),
			ownerReadRequest{Since: uri.Since}, mcpReadTimeout)
		switch {
		case err == gen.ErrProcessUnknown:
			w.mcpFail(writer, request, envelope.ID, mcpInvalidParams,
				"no run under this key: it finished and its retention ran out, or there never was one",
				map[string]any{"uri": uri.Canonical()})
			return
		case err != nil:
			w.mcpFail(writer, request, envelope.ID, mcpInternalError, err.Error(),
				map[string]any{"uri": uri.Canonical()})
			return
		}
		w.mcpReading(writer, request, envelope, uri, narrowRun(reading, identity.Ceiling))
		return
	}
	if refusal := mcpRefusal(identity, uri); refusal != nil {
		w.mcpFail(writer, request, envelope.ID, mcpInvalidParams, refusal.Error(),
			map[string]any{"uri": uri.Canonical(), "node": string(uri.Node)})
		return
	}

	if uri.Node != w.Node().Name() {
		if _, err := w.Node().Network().Node(uri.Node); err != nil {
			w.mcpFail(writer, request, envelope.ID, mcpInvalidParams,
				fmt.Sprintf("node %s is not connected, read %s to see what is", string(uri.Node), clusterURI),
				map[string]any{"uri": uri.Canonical(), "node": string(uri.Node)})
			return
		}
	}

	reading, err := w.readResource(uri, qualified(identity))
	if err != nil {
		w.mcpFail(writer, request, envelope.ID, mcpInternalError, err.Error(),
			map[string]any{"uri": uri.Canonical()})
		return
	}
	if reading.Error != "" {
		w.mcpFail(writer, request, envelope.ID, mcpInvalidParams, reading.Error,
			map[string]any{"uri": uri.Canonical()})
		return
	}

	w.mcpReading(writer, request, envelope, uri, reading)
}

func (w *postWorker) mcpToolCall(writer http.ResponseWriter, request *http.Request, envelope mcpEnvelope) {
	var params struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if len(envelope.Params) > 0 {
		if err := json.Unmarshal(envelope.Params, &params); err != nil {
			w.mcpFail(writer, request, envelope.ID, mcpInvalidParams, "params are not readable", nil)
			return
		}
	}

	tool, served := toolByName(params.Name)
	if served == false {
		// a bad parameter, not MethodNotFound: that would carry a 404, and a 404 here sends a
		// client to the deprecated transport
		w.mcpFail(writer, request, envelope.ID, mcpInvalidParams,
			"there is no tool "+params.Name, map[string]any{"name": params.Name})
		return
	}
	if params.Arguments == nil {
		params.Arguments = map[string]any{}
	}

	if err := checkArgs(tool.Schema, params.Arguments); err != nil {
		w.mcpToolError(writer, request, envelope, err.Error())
		return
	}

	if tool.Fanout {
		w.mcpFanout(writer, request, envelope, tool, params.Arguments)
		return
	}

	switch params.Name {
	case "job_list":
		w.mcpJobList(writer, request, envelope)
		return
	case "job_cancel":
		w.mcpJobCancel(writer, request, envelope, params.Arguments)
		return
	}

	node, _ := params.Arguments[nodeArgument].(string)
	if node == "" {
		w.mcpToolError(writer, request, envelope,
			nodeArgument+" is required, read "+clusterURI+" for the names")
		return
	}
	target := gen.Atom(node)

	identity, _ := identityOf(request)
	if identity.Ceiling.AllowsNode(target) == false {
		w.mcpToolError(writer, request, envelope,
			fmt.Sprintf("node %s is not permitted here", node))
		return
	}

	action, args, err := toolAction(tool, params.Arguments)
	if err != nil {
		w.mcpToolError(writer, request, envelope, err.Error())
		return
	}

	actionRequest, capability, err := buildActionRequest(action, args)
	if err != nil {
		w.mcpToolError(writer, request, envelope, err.Error())
		return
	}
	if identity.Ceiling.Allows(capability) == false {
		w.mcpToolError(writer, request, envelope,
			fmt.Sprintf("%s is not permitted here, ask the capabilities tool of %s", capability, node))
		return
	}

	if target != w.Node().Name() {
		if _, err := w.Node().Network().Node(target); err != nil {
			w.mcpToolError(writer, request, envelope,
				fmt.Sprintf("node %s is not connected, read %s to see what is", node, clusterURI))
			return
		}
	}

	result, err := w.CallWithTimeout(gen.ProcessID{Name: plane(capability), Node: target},
		actionRequest, defaultCallTimeout)
	if err != nil {
		w.mcpToolError(writer, request, envelope, fmt.Sprintf("node %s: %s", node, err))
		return
	}

	if failure := actionError(result); failure != nil {
		w.mcpToolError(writer, request, envelope, fmt.Sprintf("node %s: %s", node, failure))
		return
	}

	uri := ""
	if tool.Lens != "" {
		named := mcpURI{Node: target, Lens: tool.Lens}
		if tool.LensTarget != "" {
			value, _ := args[tool.LensTarget].(string)
			if value == "" {
				named = mcpURI{}
			}
			named.Target = value
		}
		if named.Lens != "" {
			uri = named.Canonical()
		}
	}

	rendered, err := mcpRender(agentPayload(result, action, capability, identity.Ceiling))
	if err != nil {
		w.mcpFail(writer, request, envelope.ID, mcpInternalError, "the result is not renderable", nil)
		return
	}

	writeJSON(writer, request, http.StatusOK, mcpOK(envelope.ID, mcpToolResult{
		ResultType: mcpResultComplete,
		Content:    toolBlocks(tool.Lens, uri, rendered),
		Meta:       mcpAnswered(),
	}))
}

func (w *postWorker) mcpReading(writer http.ResponseWriter, request *http.Request,
	envelope mcpEnvelope, uri mcpURI, result any) {

	reading, ok := result.(ownerReadResponse)
	if ok == false {
		w.mcpFail(writer, request, envelope.ID, mcpInternalError,
			fmt.Sprintf("unexpected answer %T", result), nil)
		return
	}
	if reading.Error != "" {
		w.mcpFail(writer, request, envelope.ID, mcpInvalidParams, reading.Error,
			map[string]any{"uri": uri.Canonical()})
		return
	}

	// the revision forbids answering a non-existent resource with an empty set: it cannot be
	// told apart from something that exists and is quiet
	if why, absent := readingAbsent(reading, uri); absent {
		w.mcpFail(writer, request, envelope.ID, mcpInvalidParams, why,
			map[string]any{"uri": uri.Canonical()})
		return
	}

	rendered, err := mcpRender(mcpRenderReading(reading))
	if err != nil {
		w.mcpFail(writer, request, envelope.ID, mcpInternalError, "the value is not renderable", nil)
		return
	}

	writeJSON(writer, request, http.StatusOK, mcpOK(envelope.ID, mcpContents{
		ResultType: mcpResultComplete,
		Contents: []mcpResourceContent{{
			URI:      uri.Canonical(),
			MimeType: rendered.MimeType,
			Text:     rendered.Text,
		}},
		TTLMs:      ttlReading,
		CacheScope: mcpCachePrivate,
		Meta: &mcpReadMeta{
			ServerInfo:   mcpIdentity(),
			Seq:          reading.Seq,
			LastModified: mcpModifiedAt(reading.At),
			NextSeq:      reading.NextSeq,
			Dropped:      reading.Dropped,
		},
	}))
}

func readingAbsent(reading ownerReadResponse, uri mcpURI) (string, bool) {
	if reading.Seq > 1 {
		return "", false
	}
	if reading.Value != nil {
		return mcpAbsent(reading.Value, uri)
	}
	if len(reading.Batches) > 0 {
		return mcpAbsent(reading.Batches[0], uri)
	}
	return "", false
}

func narrowRun(result any, ceiling Ceiling) any {
	reading, ok := result.(ownerReadResponse)
	if ok == false {
		return result
	}
	held, ok := reading.Value.(jobReading)
	if ok == false {
		return result
	}

	// the map that arrived belongs to the job, and this runs on another goroutine
	refused := make(map[string]string, len(held.Refused))
	for node, why := range held.Refused {
		refused[node] = why
	}

	kept := make([]jobResult, 0, len(held.Results))
	for _, result := range held.Results {
		if ceiling.AllowsNode(gen.Atom(result.Node)) == false {
			refused[result.Node] = "not permitted here any more"
			continue
		}
		kept = append(kept, result)
	}

	held.Results = kept
	if len(refused) > 0 {
		held.Refused = refused
	}
	reading.Value = held
	return reading
}

func mcpRenderReading(reading ownerReadResponse) any {
	if reading.Batches == nil {
		return reading.Value
	}
	return reading.Batches
}

func (w *postWorker) readResource(uri mcpURI, subject string) (ownerReadResponse, error) {
	name := uri.ownerName(subject)
	read := ownerReadRequest{Since: uri.Since}

	result, err := w.CallWithTimeout(name, read, mcpReadTimeout)
	if err == gen.ErrProcessUnknown {
		result, err = w.CallWithTimeout(managerName,
			ownerReadForward{URI: uri, Subject: subject, Read: read}, mcpReadTimeout)
	}
	if err != nil {
		return ownerReadResponse{}, err
	}

	reading, ok := result.(ownerReadResponse)
	if ok == false {
		return ownerReadResponse{}, fmt.Errorf("unexpected answer %T", result)
	}
	return reading, nil
}

func (w *postWorker) mcpFail(writer http.ResponseWriter, request *http.Request, id any, code int, message string, data any) {
	writeJSON(writer, request, mcpStatus(code), mcpFailed(id, code, message, data))
}

// a client only MAY hand a protocol error to the model, and SHOULD hand it this
func (w *postWorker) mcpToolError(writer http.ResponseWriter, request *http.Request,
	envelope mcpEnvelope, message string) {

	writeJSON(writer, request, http.StatusOK, mcpOK(envelope.ID, mcpToolResult{
		ResultType: mcpResultComplete,
		Content:    []any{mcpText(message)},
		IsError:    true,
		Meta:       mcpAnswered(),
	}))
}

func mcpRefusal(identity Identity, uri mcpURI) error {
	if uri.Key != "" && identity.Subject == "" {
		return errNeedsCaller
	}
	if identity.Ceiling.AllowsNode(uri.Node) == false {
		return fmt.Errorf("node %s is not permitted here", string(uri.Node))
	}

	subscription := lensOf(uri.Lens)
	if subscription == "" {
		return fmt.Errorf("there is no lens %q", uri.Lens)
	}
	args, err := lensArgs(uri)
	if err != nil {
		return err
	}
	_, capability, err := buildInspectRequest(subscription, args)
	if err != nil {
		return err
	}
	if identity.Ceiling.Allows(capability) == false {
		return fmt.Errorf("%s is not permitted here", capability)
	}
	if forced, _ := args["force"].(bool); forced {
		if identity.Ceiling.Allows(CapForceProducer) == false {
			return fmt.Errorf("%s is not permitted here", CapForceProducer)
		}
	}
	return nil
}
