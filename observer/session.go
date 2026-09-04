package observer

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"ergo.services/application/observer/access"
	"ergo.services/ergo/act"
	"ergo.services/ergo/app/system/inspect"
	"ergo.services/ergo/app/system/manage"
	"ergo.services/ergo/gen"
	"ergo.services/meta/sse"
)

func factory_session() gen.ProcessBehavior {
	return &session{}
}

type sessionSpec struct {
	id               string
	sse              gen.Alias
	subject          string
	ceiling          Ceiling
	maxSubscriptions int
}

type session struct {
	act.Actor

	id               string
	sseAlias         gen.Alias
	node             gen.Atom
	creation         int64
	subject          string
	ceiling          Ceiling
	maxSubscriptions int
	dropping         bool
	subscriptions    map[string]*subscribed // eventKey → monitored event and its handles
	subIndex         map[string]string      // lookupKey (handle) → eventKey
	eventCounter     int64
	pool             gen.PID

	started    time.Time
	lastPush   time.Time
	lastChange time.Time
	dropped    map[string]int64
}

type subscribed struct {
	event gen.Event

	handles map[string]int
}

type messageSessionReady struct{}

func (s *session) Init(args ...any) error {
	spec, ok := args[0].(sessionSpec)
	if ok == false {
		return fmt.Errorf("session spec expected, got %T", args[0])
	}
	s.id = spec.id
	s.sseAlias = spec.sse
	s.subject = spec.subject
	s.ceiling = spec.ceiling
	s.maxSubscriptions = spec.maxSubscriptions
	if s.maxSubscriptions < 1 {
		s.maxSubscriptions = defaultMaxSubscriptions
	}
	s.node = s.Node().Name()
	s.creation = s.Node().Creation()
	s.subscriptions = make(map[string]*subscribed)
	s.subIndex = make(map[string]string)
	s.dropped = make(map[string]int64)
	s.started = time.Now()

	s.SetProcessKind(gen.ProcessKindSession)
	s.Log().SetLogger("default")

	if err := s.LinkAlias(s.sseAlias); err != nil {
		s.Log().Error("session %s: LinkAlias failed: %s", s.id, err)
		return err
	}

	pool, err := s.Spawn(factory_sessionPool, gen.ProcessOptions{LinkParent: true},
		sessionWorkerSpec{back: s.PID(), id: s.id})
	if err != nil {
		s.Log().Error("session %s: unable to start the worker pool: %s", s.id, err)
		return err
	}
	s.pool = pool

	if err := s.Send(s.PID(), messageSessionReady{}); err != nil {
		return err
	}

	s.Log().Info("session %s started, SSE: %s", s.id, s.sseAlias)
	return nil
}

func (s *session) HandleMessage(from gen.PID, message any) error {
	switch m := message.(type) {
	case messageSessionReady:
		s.sendConnectedEvent()

	case subscribeResolved:
		s.finishSubscribe(m)

	case switchResolved:
		s.finishSwitch(m)

	case gen.MessageDownEvent:
		key := m.Event.String()
		sub, exist := s.subscriptions[key]
		if exist == false {
			s.dropped["down_unknown"]++
			return nil
		}

		keys := make([]string, 0, len(sub.handles))
		for handle := range sub.handles {
			keys = append(keys, handle)
			delete(s.subIndex, handle)
		}
		sort.Strings(keys)
		delete(s.subscriptions, key)
		s.lastChange = time.Now()

		data, _ := json.Marshal(wireSubscriptionDown{
			Keys: keys,
			Type: inspectEventToSSEType(m.Event.Name),
		})
		s.push("subscription_down", data)

		s.Log().Info("session %s: event source terminated: %s", s.id, key)

	default:
		s.dropped["message_unexpected"]++
		s.Log().Warning("session %s: unexpected message from %s: %#v", s.id, from, message)
	}
	return nil
}

func (s *session) push(event string, data []byte) {
	s.eventCounter++
	s.lastPush = time.Now()

	if err := s.SendAlias(s.sseAlias, sse.Message{
		Event: event,
		Data:  data,
		MsgID: fmt.Sprintf("%d", s.eventCounter),
	}); err != nil {
		s.dropped["sse_send_failed"]++
		if s.dropping == false {
			s.dropping = true
			s.Log().Warning("session %s: the stream is behind, frames are dropped: %s", s.id, err)
		}
		return
	}
	if s.dropping {
		s.dropping = false
		s.Log().Info("session %s: the stream caught up", s.id)
	}
}

func (s *session) HandleCall(from gen.PID, ref gen.Ref, request any) (any, error) {
	switch r := request.(type) {
	case commandRequest:
		if refused := s.notMine(r.Subject); refused != nil {
			return apiResponse{Error: refused.Error()}, nil
		}
		result, err := s.handleCommand(from, ref, r)
		return s.counted("command_refused", result, err)
	case actionRequest:
		if refused := s.notMine(r.Subject); refused != nil {
			return apiResponse{Error: refused.Error()}, nil
		}
		result, err := s.handleAction(from, ref, r)
		return s.counted("action_refused", result, err)
	}
	s.dropped["request_unsupported"]++
	return gen.ErrUnsupported, nil
}

func (s *session) notMine(subject string) error {
	if subject == s.subject {
		return nil
	}
	s.dropped["subject_mismatch"]++
	s.Log().Warning("session %s: %q asked for the session of %q", s.id, subject, s.subject)
	return errors.New("this session belongs to another caller")
}

func (s *session) counted(reason string, result any, err error) (any, error) {
	if response, ok := result.(apiResponse); ok && response.Error != "" {
		s.dropped[reason]++
	}
	return result, err
}

func (s *session) HandleEvent(message gen.MessageEvent) error {
	key := message.Event.String()
	if _, exist := s.subscriptions[key]; exist == false {
		s.dropped["event_unknown"]++
		s.Log().Warning("session %s: event for unknown subscription: %s (have %d subs)", s.id, key, len(s.subscriptions))
		return nil
	}
	s.Log().Debug("session %s: event %s", s.id, key)

	if s.subIndex["registrar_event"] == key {
		s.sendClusterUpdate()
		return nil
	}

	s.forwardEventMessage(message)
	return nil
}

func (s *session) forwardEventMessage(message gen.MessageEvent) {
	data, err := json.Marshal(wireFrom(message.Message))
	if err != nil {
		s.dropped["marshal_failed"]++
		s.Log().Error("session %s: marshal event %s: %s", s.id, message.Event.String(), err)
		return
	}
	s.push(inspectEventToSSEType(message.Event.Name), data)
}

func (s *session) Terminate(reason error) {
	s.Log().Info("session %s terminated: %s", s.id, reason)
	s.SendExitMeta(s.sseAlias, reason)
}

const sessionInspectHelp = "summary keys: id, subject, read_only, observing_node, creation, contract, sse, registrar_event, " +
	"subscriptions, handles, messages_sent, last_message, last_change, uptime, dropped; " +
	"queries: handles, events, dropped, handle <handle>, event <event>"

func (s *session) HandleInspect(from gen.PID, item ...string) map[string]string {
	if len(item) == 0 {
		return s.inspectSummary()
	}

	result := map[string]string{}
	for _, q := range item {
		switch {
		case q == "help":
			result["help"] = sessionInspectHelp

		case q == "handles":
			handles := make([]string, 0, len(s.subIndex))
			for handle := range s.subIndex {
				handles = append(handles, handle)
			}
			result[q] = fmt.Sprintf("%d: %s", len(handles), inspectList(handles))

		case q == "events":
			events := make([]string, 0, len(s.subscriptions))
			for eventKey, sub := range s.subscriptions {
				events = append(events, fmt.Sprintf("%s=%d", eventKey, len(sub.handles)))
			}
			result[q] = fmt.Sprintf("%d: %s", len(events), inspectList(events))

		case q == "dropped":
			result[q] = inspectCounters(s.dropped)

		case strings.HasPrefix(q, "handle "):
			eventKey, exist := s.subIndex[strings.TrimPrefix(q, "handle ")]
			if exist == false {
				result[q] = "<not found>"
				continue
			}
			result[q] = eventKey

		case strings.HasPrefix(q, "event "):
			sub, exist := s.subscriptions[strings.TrimPrefix(q, "event ")]
			if exist == false {
				result[q] = "<not found>"
				continue
			}
			handles := make([]string, 0, len(sub.handles))
			for handle := range sub.handles {
				handles = append(handles, handle)
			}
			result[q] = fmt.Sprintf("handles=%d %s", len(handles), inspectList(handles))

		default:
			result[q] = "<unknown item>"
		}
	}
	return result
}

func (s *session) inspectSummary() map[string]string {
	registrar := "none"
	if eventKey, exist := s.subIndex["registrar_event"]; exist {
		registrar = eventKey
	}

	subject := s.subject
	if subject == "" {
		subject = "anonymous"
	}

	return map[string]string{
		"id":              s.id,
		"subject":         subject,
		"read_only":       fmt.Sprintf("%t", s.ceiling.ReadOnly),
		"observing_node":  string(s.node),
		"creation":        fmt.Sprintf("%d", s.creation),
		"contract":        fmt.Sprintf("%d", wireContractVersion),
		"sse":             s.sseAlias.String(),
		"registrar_event": registrar,
		"subscriptions":   fmt.Sprintf("%d", len(s.subscriptions)),
		"handles":         fmt.Sprintf("%d", len(s.subIndex)),
		"messages_sent":   fmt.Sprintf("%d", s.eventCounter),
		"last_message":    inspectAge(s.lastPush),
		"last_change":     inspectAge(s.lastChange),
		"uptime":          inspectAge(s.started),
		"dropped":         inspectCounters(s.dropped),
		"items":           "help",
	}
}

func (s *session) handleCommand(from gen.PID, ref gen.Ref, cmd commandRequest) (any, error) {
	switch cmd.Command {
	case "subscribe":
		return s.beginSubscribe(from, ref, cmd.Type, cmd.Args)
	case "unsubscribe":
		return s.doUnsubscribe(cmd.Args)
	case "switch":
		node, _ := cmd.Args["node"].(string)
		if node == "" {
			return apiResponse{Error: "node is required"}, nil
		}
		return s.beginSwitch(from, ref, gen.Atom(node), cmd.Args)
	}
	return apiResponse{Error: "unknown command: " + cmd.Command}, nil
}

func (s *session) handleAction(from gen.PID, ref gen.Ref, req actionRequest) (any, error) {
	if req.Action == "inspect" {
		if p, err := argPID(req.Args, "pid"); err == nil && p == s.PID() {
			return apiResponse{OK: true, Data: s.HandleInspect(s.PID(), argStrings(req.Args, "items")...)}, nil
		}
	}

	if req.Action == "cluster_info" {
		job := jobCluster{
			from: from, ref: ref,
			target: gen.ProcessID{Name: clusterName, Node: s.Node().Name()},
		}
		if err := s.Send(s.pool, job); err != nil {
			s.dropped["pool_send_failed"]++
			return apiResponse{Error: fmt.Sprintf("cluster info: %s", err)}, nil
		}
		return nil, nil
	}

	args := make(map[string]any, len(req.Args)+1)
	for name, given := range req.Args {
		args[name] = given
	}
	args[nodeArgument] = string(s.node)

	actionReq, capability, err := buildActionRequest(req.Action, args)
	if err != nil {
		return apiResponse{Error: err.Error()}, nil
	}

	if refused := s.refuse(capability); refused != nil {
		return apiResponse{Error: refused.Error()}, nil
	}

	job := jobAction{
		from: from, ref: ref, action: req.Action,
		target:  gen.ProcessID{Name: plane(capability), Node: s.node},
		request: actionReq,
		ceiling: s.ceiling,
	}
	if err := s.Send(s.pool, job); err != nil {
		s.dropped["pool_send_failed"]++
		return apiResponse{Error: fmt.Sprintf("action %s: %s", req.Action, err)}, nil
	}
	return nil, nil
}

func actionResponse(result any, ceiling Ceiling) apiResponse {
	if e := actionError(result); e != nil {
		return apiResponse{Error: e.Error()}
	}

	switch r := result.(type) {
	case inspect.ResponseGetProcessState:
		return apiResponse{OK: true, Data: r.State}
	case inspect.ResponseGetMetaState:
		return apiResponse{OK: true, Data: r.State}
	case inspect.ResponseGetProcessLookup:
		return apiResponse{OK: true, Data: wireLookupFrom(r)}
	case inspect.ResponseGetGoroutines:
		return apiResponse{OK: true, Data: wireGoroutinesFrom(r)}
	case inspect.ResponseGetHeapProfile:
		return apiResponse{OK: true, Data: wireHeapProfileFrom(r)}
	case inspect.ResponseGetTypes:
		return apiResponse{OK: true, Data: wireTypesFrom(r)}
	case inspect.ResponseGetErrors:
		return apiResponse{OK: true, Data: wireErrorsFrom(r)}
	case inspect.ResponseGetAtoms:
		return apiResponse{OK: true, Data: wireAtomsFrom(r)}
	case inspect.ResponseGetCapabilities:
		return apiResponse{OK: true, Data: wireCapabilitiesUnder(r, ceiling)}
	case inspect.ResponseGetAppTree:
		return apiResponse{OK: true, Data: wireAppTreeFrom(r)}
	case inspect.ResponseGetSubtree:
		return apiResponse{OK: true, Data: wireSubtreeFrom(r)}
	}
	return apiResponse{OK: true}
}

func plane(capability string) gen.Atom {
	if access.Mutating(capability) {
		return manage.Name
	}
	return inspect.Name
}

func (s *session) refuse(capability string) error {
	if s.ceiling.Allows(capability) {
		return nil
	}
	s.dropped["ceiling_refused"]++
	s.Log().Warning("session %s: %s refused by the ceiling", s.id, capability)
	return fmt.Errorf("%s is not permitted here", capability)
}

func buildActionRequest(action string, args map[string]any) (any, string, error) {
	switch action {
	case "send":
		if hasArg(args, "alias") {
			a, err := argAlias(args, "alias")
			if err != nil {
				return nil, "", err
			}
			return manage.RequestDoSendMeta{
				Meta:    a,
				Message: args["message"],
			}, manage.CapSendMeta, nil
		}
		p, err := argPID(args, "pid")
		if err != nil {
			return nil, "", err
		}
		var priority gen.MessagePriority
		if hasArg(args, "priority") {
			parsed, err := argMessagePriority(args, "priority")
			if err != nil {
				return nil, "", err
			}
			priority = parsed
		}
		return manage.RequestDoSend{
			PID:      p,
			Priority: priority,
			Message:  args["message"],
		}, manage.CapSend, nil

	case "send_exit":
		rs, _ := args["reason"].(string)
		reason := exitReason(rs)
		if hasArg(args, "alias") {
			a, err := argAlias(args, "alias")
			if err != nil {
				return nil, "", err
			}
			return manage.RequestDoSendExitMeta{
				Meta:   a,
				Reason: reason,
			}, manage.CapSendExitMeta, nil
		}
		p, err := argPID(args, "pid")
		if err != nil {
			return nil, "", err
		}
		return manage.RequestDoSendExit{
			PID:    p,
			Reason: reason,
		}, manage.CapSendExit, nil

	case "kill":
		p, err := argPID(args, "pid")
		if err != nil {
			return nil, "", err
		}
		return manage.RequestDoKill{PID: p}, manage.CapKill, nil

	case "set_node_tracing_sampler":
		typ, _ := args["type"].(string)
		rate, _ := args["rate"].(float64)
		limit, _ := args["limit"].(float64)
		return manage.RequestDoSetNodeTracingSampler{Type: typ, Rate: rate, Limit: int(limit)}, manage.CapSetNodeTracingSampler, nil

	case "set_process_tracing_sampler":
		p, err := argPID(args, "pid")
		if err != nil {
			return nil, "", err
		}
		typ, _ := args["type"].(string)
		rate, _ := args["rate"].(float64)
		limit, _ := args["limit"].(float64)
		return manage.RequestDoSetProcessTracingSampler{PID: p, Type: typ, Rate: rate, Limit: int(limit)}, manage.CapSetProcessTracingSampler, nil

	case "set_log_level":
		level, err := argLogLevel(args, "level")
		if err != nil {
			return nil, "", err
		}

		target, _ := args["target"].(string)
		switch target {
		case "process":
			p, err := argPID(args, "pid")
			if err != nil {
				return nil, "", err
			}
			return manage.RequestDoSetProcessLogLevel{PID: p, Level: level}, manage.CapSetProcessLogLevel, nil
		case "meta":
			a, err := argAlias(args, "alias")
			if err != nil {
				return nil, "", err
			}
			return manage.RequestDoSetMetaLogLevel{Meta: a, Level: level}, manage.CapSetMetaLogLevel, nil
		default:
			return manage.RequestDoSetLogLevel{Level: level}, manage.CapSetLogLevel, nil
		}
	case "app_start":
		name, _ := args["name"].(string)
		if name == "" {
			return nil, "", fmt.Errorf("name is required")
		}
		var mode gen.ApplicationMode
		if hasArg(args, "mode") {
			parsed, err := argApplicationMode(args, "mode")
			if err != nil {
				return nil, "", err
			}
			mode = parsed
		}
		return manage.RequestDoAppStart{
			Name: gen.Atom(name),
			Mode: mode,
		}, manage.CapAppStart, nil

	case "app_stop":
		name, _ := args["name"].(string)
		if name == "" {
			return nil, "", fmt.Errorf("name is required")
		}
		force, _ := args["force"].(bool)
		return manage.RequestDoAppStop{Name: gen.Atom(name), Force: force}, manage.CapAppStop, nil

	case "app_unload":
		name, _ := args["name"].(string)
		if name == "" {
			return nil, "", fmt.Errorf("name is required")
		}
		return manage.RequestDoAppUnload{Name: gen.Atom(name)}, manage.CapAppUnload, nil

	case "inspect":
		p, err := argPID(args, "pid")
		if err != nil {
			return nil, "", err
		}
		return inspect.RequestGetProcessState{PID: p, Items: argStrings(args, "items")}, inspect.CapGetProcessState, nil

	case "lookup":
		req := inspect.RequestGetProcessLookup{}
		if name, _ := args["name"].(string); name != "" {
			req.Name = gen.Atom(name)
		} else if p, err := argPID(args, "pid"); err == nil {
			req.PID = p
		} else {
			return nil, "", errors.New("name or pid is required")
		}
		return req, inspect.CapGetProcessLookup, nil

	case "meta_inspect":
		a, err := argAlias(args, "alias")
		if err != nil {
			return nil, "", err
		}
		return inspect.RequestGetMetaState{Meta: a, Items: argStrings(args, "items")}, inspect.CapGetMetaState, nil

	case "set_process_send_priority":
		p, err := argPID(args, "pid")
		if err != nil {
			return nil, "", err
		}
		priority, err := argMessagePriority(args, "priority")
		if err != nil {
			return nil, "", err
		}
		return manage.RequestDoSetProcessSendPriority{PID: p, Priority: priority}, manage.CapSetProcessSendPriority, nil

	case "set_process_compression":
		p, err := argPID(args, "pid")
		if err != nil {
			return nil, "", err
		}
		enabled, _ := args["enabled"].(bool)
		return manage.RequestDoSetProcessCompression{PID: p, Enabled: enabled}, manage.CapSetProcessCompression, nil

	case "set_process_compression_type":
		p, err := argPID(args, "pid")
		if err != nil {
			return nil, "", err
		}
		v, _ := args["type"].(string)
		return manage.RequestDoSetProcessCompressionType{PID: p, Type: gen.CompressionType(v)}, manage.CapSetProcessCompressionType, nil

	case "set_process_compression_level":
		p, err := argPID(args, "pid")
		if err != nil {
			return nil, "", err
		}
		level, err := argCompressionLevel(args, "level")
		if err != nil {
			return nil, "", err
		}
		return manage.RequestDoSetProcessCompressionLevel{PID: p, Level: level}, manage.CapSetProcessCompressionLevel, nil

	case "set_process_compression_threshold":
		p, err := argPID(args, "pid")
		if err != nil {
			return nil, "", err
		}
		threshold, _ := args["threshold"].(float64) // JSON numbers are float64
		return manage.RequestDoSetProcessCompressionThreshold{PID: p, Threshold: int(threshold)}, manage.CapSetProcessCompressionThreshold, nil

	case "set_process_keep_network_order":
		p, err := argPID(args, "pid")
		if err != nil {
			return nil, "", err
		}
		order, _ := args["order"].(bool)
		return manage.RequestDoSetProcessKeepNetworkOrder{PID: p, Order: order}, manage.CapSetProcessKeepNetworkOrder, nil

	case "set_process_important_delivery":
		p, err := argPID(args, "pid")
		if err != nil {
			return nil, "", err
		}
		important, _ := args["important"].(bool)
		return manage.RequestDoSetProcessImportantDelivery{PID: p, Important: important}, manage.CapSetProcessImportantDelivery, nil

	case "set_meta_send_priority":
		a, err := argAlias(args, "alias")
		if err != nil {
			return nil, "", err
		}
		priority, err := argMessagePriority(args, "priority")
		if err != nil {
			return nil, "", err
		}
		return manage.RequestDoSetMetaSendPriority{Meta: a, Priority: priority}, manage.CapSetMetaSendPriority, nil

	case "goroutines":
		stack, _ := args["stack"].(string)
		state, _ := args["state"].(string)
		minWait, _ := args["minWait"].(float64)
		return inspect.RequestGetGoroutines{
			Stack:   stack,
			State:   state,
			MinWait: int64(minWait),
		}, inspect.CapGetGoroutines, nil

	case "heap":
		minBytes, _ := args["minBytes"].(float64)
		req := inspect.RequestGetHeapProfile{MinBytes: int64(minBytes)}

		if v, ok := args["limit"].(float64); ok && v >= 1 {
			req.Limit = int(v)
		}
		return req, inspect.CapGetHeapProfile, nil

	case "types":
		req := inspect.RequestGetTypes{}
		if v, ok := args["name"].(string); ok {
			req.Name = v
		}
		if v, ok := args["kind"].(string); ok {
			req.Kind = v
		}
		if v, ok := args["limit"].(float64); ok && v >= 1 {
			req.Limit = int(v)
		}
		return req, inspect.CapGetTypes, nil

	case "errors":
		req := inspect.RequestGetErrors{}
		if v, ok := args["text"].(string); ok {
			req.Text = v
		}
		if v, ok := args["limit"].(float64); ok && v >= 1 {
			req.Limit = int(v)
		}
		return req, inspect.CapGetErrors, nil

	case "atoms":
		req := inspect.RequestGetAtoms{}
		if v, ok := args["name"].(string); ok {
			req.Name = v
		}
		if v, ok := args["limit"].(float64); ok && v >= 1 {
			req.Limit = int(v)
		}
		return req, inspect.CapGetAtoms, nil

	case "capabilities":
		return inspect.RequestGetCapabilities{}, inspect.CapCapabilities, nil

	case "cron_info":
		job, _ := args["job"].(string)
		return inspect.RequestGetCronInfo{Job: gen.Atom(job)}, inspect.CapGetCronInfo, nil

	case "cron_schedule":
		job, _ := args["job"].(string)
		req := inspect.RequestGetCronSchedule{Job: gen.Atom(job)}
		if since, _ := args["since"].(string); since != "" {
			at, err := time.Parse(time.RFC3339, since)
			if err != nil {
				return nil, "", fmt.Errorf("invalid since: %s", err)
			}
			req.Since = at
		}
		if hours, _ := args["hours"].(float64); hours > 0 {
			req.Duration = time.Duration(hours * float64(time.Hour))
		}
		if limit, _ := args["limit"].(float64); limit >= 1 {
			req.Limit = int(limit)
		}
		return req, inspect.CapGetCronSchedule, nil

	case "registrar_nodes":
		return inspect.RequestGetRegistrarNodes{}, inspect.CapGetRegistrarNodes, nil

	case "registrar_routes":
		peer, _ := args["peer"].(string)
		if peer == "" {
			return nil, "", errors.New("peer is required")
		}
		return inspect.RequestGetRegistrarRoutes{Node: gen.Atom(peer)},
			inspect.CapGetRegistrarRoutes, nil

	case "registrar_proxy_routes":
		peer, _ := args["peer"].(string)
		if peer == "" {
			return nil, "", errors.New("peer is required")
		}
		return inspect.RequestGetRegistrarProxyRoutes{Node: gen.Atom(peer)},
			inspect.CapGetRegistrarProxyRoutes, nil

	case "registrar_application_routes":
		name, _ := args["name"].(string)
		if name == "" {
			return nil, "", errors.New("name is required")
		}
		return inspect.RequestGetRegistrarApplicationRoutes{Name: gen.Atom(name)},
			inspect.CapGetRegistrarApplicationRoutes, nil

	case "node":
		return inspect.RequestGetNode{}, inspect.CapNode, nil

	case "network":
		return inspect.RequestGetNetwork{}, inspect.CapNetwork, nil

	case "connections":
		req := inspect.RequestGetConnectionList{}
		if v, ok := args["peer"].(string); ok {
			req.Name = v
		}
		if v, ok := args["limit"].(float64); ok && v >= 1 {
			req.Limit = int(v)
		}
		return req, inspect.CapConnectionList, nil

	case "connection":
		peer, _ := args["peer"].(string)
		if peer == "" {
			return nil, "", fmt.Errorf("peer is required")
		}
		return inspect.RequestGetConnection{RemoteNode: gen.Atom(peer)}, inspect.CapConnection, nil

	case "applications":
		return inspect.RequestGetApplicationList{}, inspect.CapApplicationList, nil

	case "events":
		req := inspect.RequestGetEventList{}
		if v, ok := args["name"].(string); ok {
			req.Name = v
		}
		if v, ok := args["limit"].(float64); ok && v >= 1 {
			req.Limit = int(v)
		}
		if v, ok := args["minSubscribers"].(float64); ok && v > 0 {
			req.MinSubscribers = int64(v)
		}
		if v, ok := args["newestFirst"].(bool); ok && v {
			req.Timestamp = -1
		}
		req.Notify = triState(args, "notify")
		req.Buffered = triState(args, "buffered")
		req.Open = triState(args, "open")
		return req, inspect.CapEventList, nil

	case "event":
		name, _ := args["name"].(string)
		if name == "" {
			return nil, "", fmt.Errorf("name is required")
		}
		return inspect.RequestGetEvent{Name: gen.Atom(name)}, inspect.CapEvent, nil

	case "processes":
		name, _ := args["name"].(string)
		behavior, _ := args["behavior"].(string)
		application, _ := args["app"].(string)
		state, _ := args["state"].(string)
		minMailbox, _ := args["minMailbox"].(float64)

		limit := 1000
		if v, ok := args["limit"].(float64); ok && v >= 1 {
			limit = int(v)
		}

		if scan, _ := args["scan"].(string); scan == "ordered" {
			req := inspect.RequestGetProcessList{
				Limit: limit, Name: name, Behavior: behavior,
				Application: application, State: state, MinMailbox: uint64(minMailbox),
			}
			if v, ok := args["start"].(float64); ok {
				req.Start = int(v)
			}
			return req, inspect.CapProcessList, nil
		}

		return inspect.RequestGetProcessRange{
			Limit: limit, Name: name, Behavior: behavior,
			Application: application, State: state, MinMailbox: uint64(minMailbox),
		}, inspect.CapProcessRange, nil

	case "app_tree":
		req := inspect.RequestGetAppTree{Limit: 1000}
		if v, ok := args["app"].(string); ok && v != "" {
			req.Application = gen.Atom(v)
		}
		if v, ok := args["limit"].(float64); ok && v >= 1 {
			req.Limit = int(v)
		}
		return req, inspect.CapGetAppTree, nil

	case "subtree":
		p, err := argPID(args, "pid")
		if err != nil {
			return nil, "", err
		}
		req := inspect.RequestGetSubtree{PID: p, Limit: 1000}
		if v, ok := args["limit"].(float64); ok && v >= 1 {
			req.Limit = int(v)
		}
		return req, inspect.CapGetSubtree, nil
	}
	return nil, "", fmt.Errorf("unknown action: %s", action)
}

func argStrings(args map[string]any, key string) []string {
	v, ok := args[key].([]any)
	if ok == false {
		return nil
	}
	out := make([]string, 0, len(v))
	for _, e := range v {
		if str, ok := e.(string); ok {
			out = append(out, str)
		}
	}
	return out
}

func actionError(result any) error {
	switch r := result.(type) {
	case manage.ResponseDoSend:
		return r.Error
	case manage.ResponseDoSendMeta:
		return r.Error
	case manage.ResponseDoSendExit:
		return r.Error
	case manage.ResponseDoSendExitMeta:
		return r.Error
	case manage.ResponseDoKill:
		return r.Error
	case manage.ResponseDoSetLogLevel:
		return r.Error
	case manage.ResponseDoAppStart:
		return r.Error
	case manage.ResponseDoAppStop:
		return r.Error
	case manage.ResponseDoAppUnload:
		return r.Error
	case inspect.ResponseGetProcessState:
		return r.Error
	case inspect.ResponseGetMetaState:
		return r.Error
	case inspect.ResponseGetProcessLookup:
		return r.Error
	case inspect.ResponseGetGoroutines:
		return r.Error
	case inspect.ResponseGetHeapProfile:
		return r.Error
	case inspect.ResponseGetTypes:
		return r.Error
	case inspect.ResponseGetErrors:
		return r.Error
	case inspect.ResponseGetAtoms:
		return r.Error
	case inspect.ResponseGetNode:
		return r.Error
	case inspect.ResponseGetNetwork:
		return r.Error
	case inspect.ResponseGetConnection:
		return r.Error
	case inspect.ResponseGetConnectionList:
		return r.Error
	case inspect.ResponseGetApplicationList:
		return r.Error
	case inspect.ResponseGetEventList:
		return r.Error
	case inspect.ResponseGetEvent:
		return r.Error
	case inspect.ResponseGetProcessRange:
		return r.Error
	case inspect.ResponseGetProcessList:
		return r.Error
	case inspect.ResponseGetAppTree:
		return r.Error
	case inspect.ResponseGetSubtree:
		return r.Error
	case manage.ResponseDoSet:
		return r.Error
	case error:
		return r
	}
	return nil
}

func (s *session) beginSubscribe(from gen.PID, ref gen.Ref, subType string, args map[string]any) (any, error) {
	inspectReq, capability, err := buildInspectRequest(subType, args)
	if err != nil {
		return apiResponse{Error: err.Error()}, nil
	}

	if refused := s.refuse(capability); refused != nil {
		return apiResponse{Error: refused.Error()}, nil
	}

	if forced, _ := args["force"].(bool); forced {
		if refused := s.refuse(CapForceProducer); refused != nil {
			return apiResponse{Error: refused.Error()}, nil
		}
	}

	handle := subHandle(s.id, s.node, subType, args)
	if s.overSubscribed(handle) {
		return apiResponse{Error: fmt.Sprintf("this stream already holds %d subscriptions", s.maxSubscriptions)}, nil
	}

	job := jobSubscribe{
		from: from, ref: ref, subType: subType, handle: handle,
		target:  gen.ProcessID{Name: inspect.Name, Node: s.node},
		request: inspectReq,
	}
	if err := s.Send(s.pool, job); err != nil {
		s.dropped["pool_send_failed"]++
		return apiResponse{Error: fmt.Sprintf("subscribe %s: %s", subType, err)}, nil
	}
	return nil, nil
}

func (s *session) overSubscribed(handle string) bool {
	if _, held := s.subIndex[handle]; held {
		return false
	}
	if len(s.subscriptions) < s.maxSubscriptions {
		return false
	}
	s.dropped["subscription_limit"]++
	s.Log().Warning("session %s: refused %s, %d subscriptions is the limit", s.id, handle, s.maxSubscriptions)
	return true
}

func (s *session) finishSubscribe(m subscribeResolved) {
	if m.failed != "" {
		s.reply(m.from, m.ref, apiResponse{Error: m.failed})
		return
	}

	event, err := extractEvent(m.result)
	if err != nil {
		s.reply(m.from, m.ref, apiResponse{Error: fmt.Sprintf("inspect response: %s", err)})
		return
	}

	eventKey := event.String()

	holds := 0
	if oldEventKey, exist := s.subIndex[m.handle]; exist {
		if oldEventKey == eventKey {
			holds = s.subscriptions[oldEventKey].handles[m.handle]
		} else {
			holds = s.dropHandle(m.handle, oldEventKey)
			s.Log().Info("session %s: auto-unsubscribed %s (replaced)", s.id, oldEventKey)
		}
	}

	sub, exist := s.subscriptions[eventKey]
	if exist == false {
		if s.overSubscribed(m.handle) {
			s.reply(m.from, m.ref, apiResponse{Error: fmt.Sprintf("this stream already holds %d subscriptions", s.maxSubscriptions)})
			return
		}
		monStart := time.Now()
		_, monErr := s.MonitorEvent(event)
		s.Log().Debug("session %s: monitor %s took %s", s.id, eventKey, time.Since(monStart))
		if monErr != nil {
			s.reply(m.from, m.ref, apiResponse{Error: fmt.Sprintf("monitor: %s", monErr)})
			return
		}
		sub = &subscribed{event: event, handles: make(map[string]int)}
		s.subscriptions[eventKey] = sub
	}

	sub.handles[m.handle] = holds + 1
	s.subIndex[m.handle] = eventKey
	s.lastChange = time.Now()
	s.Log().Info("session %s: subscribed %s [%s] → %s (total subs: %d)", s.id, m.subType, m.handle, eventKey, len(s.subscriptions))

	s.sendInitialData(m.subType, m.result)

	s.reply(m.from, m.ref, apiResponse{OK: true, Data: wireSubscribed{Key: m.handle}})
}

func (s *session) reply(to gen.PID, ref gen.Ref, response any) {
	if err := s.SendResponse(to, ref, response); err != nil {
		s.dropped["reply_failed"]++
		s.Log().Warning("session %s: reply to %s failed: %s", s.id, to, err)
	}
}

func (s *session) releaseHandle(handle string, eventKey string) {
	sub, exist := s.subscriptions[eventKey]
	if exist == false {
		delete(s.subIndex, handle)
		return
	}
	if sub.handles[handle] > 1 {
		sub.handles[handle]--
		return
	}
	s.dropHandle(handle, eventKey)
}

func (s *session) dropHandle(handle string, eventKey string) int {
	delete(s.subIndex, handle)
	s.lastChange = time.Now()

	sub, exist := s.subscriptions[eventKey]
	if exist == false {
		return 0
	}
	holds := sub.handles[handle]
	delete(sub.handles, handle)
	if len(sub.handles) > 0 {
		return holds
	}

	demonStart := time.Now()
	s.DemonitorEvent(sub.event)
	s.Log().Debug("session %s: demonitor %s took %s", s.id, eventKey, time.Since(demonStart))
	delete(s.subscriptions, eventKey)
	return holds
}

func (s *session) doUnsubscribe(args map[string]any) (any, error) {
	handle, _ := args["key"].(string)
	if handle == "" {
		return apiResponse{Error: "key is required"}, nil
	}

	eventKey, exist := s.subIndex[handle]
	if exist == false {
		s.dropped["unsubscribe_unknown"]++
		s.Log().Warning("session %s: unsubscribe %s not found", s.id, handle)
		return apiResponse{OK: true}, nil
	}

	s.releaseHandle(handle, eventKey)
	s.Log().Info("session %s: unsubscribed %s → %s", s.id, handle, eventKey)
	return apiResponse{OK: true}, nil
}

func (s *session) beginSwitch(from gen.PID, ref gen.Ref, newNode gen.Atom, args map[string]any) (any, error) {
	if s.node == newNode {
		return apiResponse{OK: true}, nil
	}

	if s.ceiling.AllowsNode(newNode) == false {
		s.dropped["ceiling_refused"]++
		s.Log().Warning("session %s: switch to %s refused by the ceiling", s.id, newNode)
		return apiResponse{Error: fmt.Sprintf("node %s is not permitted here", newNode)}, nil
	}

	cookie, _ := args["Cookie"].(string)
	host, _ := args["Host"].(string)
	port, _ := args["Port"].(float64)
	if cookie != "" || host != "" || port > 0 {
		if refused := s.refuse(CapDialRoute); refused != nil {
			return apiResponse{Error: refused.Error()}, nil
		}
	}

	job := jobSwitch{
		from: from, ref: ref, node: newNode,
		target: gen.ProcessID{Name: inspect.Name, Node: newNode},
		args:   args,
	}
	if err := s.Send(s.pool, job); err != nil {
		s.dropped["pool_send_failed"]++
		return apiResponse{Error: fmt.Sprintf("switch to %s: %s", newNode, err)}, nil
	}
	return nil, nil
}

func (s *session) finishSwitch(m switchResolved) {
	if m.failed != "" {
		s.reply(m.from, m.ref, apiResponse{Error: m.failed})
		return
	}

	for key, sub := range s.subscriptions {
		s.DemonitorEvent(sub.event)
		delete(s.subscriptions, key)
	}
	s.subIndex = make(map[string]string)
	s.lastChange = time.Now()

	s.node = m.node
	s.creation = m.creation
	s.sendConnectedEvent()
	s.reply(m.from, m.ref, apiResponse{OK: true})
}

func connectTo(local gen.Node, node gen.Atom, args map[string]any) error {
	if node == local.Name() {
		return nil
	}

	if _, err := local.Network().Node(node); err == nil {
		return nil
	}

	nr := gen.NetworkRoute{}

	if reg, err := local.Network().Registrar(); err == nil {
		if routes, err := reg.Resolver().Resolve(node); err == nil && len(routes) > 0 {
			nr.Route = routes[0]
		}
	}

	if v, ok := args["Cookie"].(string); ok && v != "" {
		nr.Cookie = v
	}
	if v, ok := args["Host"].(string); ok && v != "" {
		nr.Route.Host = v
	}
	if v, ok := args["Port"].(float64); ok && v > 0 {
		nr.Route.Port = uint16(v)
	}
	if v, ok := args["TLS"].(bool); ok && v {
		nr.Route.TLS = true
	}

	if nr.Route.Host != "" || nr.Route.Port > 0 || nr.Cookie != "" {
		_, err := local.Network().GetNodeWithRoute(node, nr)
		return err
	}

	_, err := local.Network().GetNode(node)
	return err
}

type nodeDesc struct {
	Name      gen.Atom `json:"Name"`
	CRC32     string   `json:"CRC32"`
	Connected bool     `json:"Connected"`
}

func (s *session) collectNodes() []nodeDesc {
	networkInfo, _ := s.Node().Network().Info()
	allNodes := make(map[gen.Atom]nodeDesc)
	for _, n := range networkInfo.Nodes {
		allNodes[n] = nodeDesc{Name: n, CRC32: n.CRC32(), Connected: true}
	}

	registrar, err := s.Node().Network().Registrar()
	if err == nil {
		clusterNodes, err := registrar.Nodes()
		if err == nil {
			for _, n := range clusterNodes {
				if _, exist := allNodes[n]; exist == false {
					allNodes[n] = nodeDesc{Name: n, CRC32: n.CRC32(), Connected: false}
				}
			}
		}
	}

	nodes := make([]nodeDesc, 0, len(allNodes))
	for _, desc := range allNodes {
		nodes = append(nodes, desc)
	}
	return nodes
}

func (s *session) sendConnectedEvent() {
	nodes := s.collectNodes()

	registrar, err := s.Node().Network().Registrar()
	if err == nil {
		regEvent, err := registrar.Event()
		if err == nil {
			if _, exist := s.subIndex["registrar_event"]; exist == false {
				s.MonitorEvent(regEvent)
				s.subscriptions[regEvent.String()] = &subscribed{
					event:   regEvent,
					handles: map[string]int{"registrar_event": 1},
				}
				s.subIndex["registrar_event"] = regEvent.String()
				s.lastChange = time.Now()
			}
		}
	}

	intro := struct {
		SessionID string      `json:"SessionID"`
		Contract  int         `json:"Contract"`
		Node      nodeDesc    `json:"Node"`
		Nodes     []nodeDesc  `json:"Nodes"`
		Version   wireVersion `json:"Version"`
	}{
		Contract:  wireContractVersion,
		SessionID: s.id,
		Node:      nodeDesc{Name: s.node, CRC32: s.node.CRC32(), Connected: true},
		Nodes:     nodes,
		Version:   wireVersionFrom(Version),
	}

	data, _ := json.Marshal(intro)
	s.push("connected", data)
}

func (s *session) sendClusterUpdate() {
	payload := struct {
		Nodes []nodeDesc `json:"Nodes"`
	}{Nodes: s.collectNodes()}

	data, _ := json.Marshal(payload)
	s.push("cluster_update", data)
}

func (s *session) sendInitialData(subType string, result any) {
	switch subType {
	case "node_info":
		r, ok := result.(inspect.ResponseInspectNode)
		if ok == false {
			return
		}
		meta := struct {
			OS            string      `json:"OS"`
			Arch          string      `json:"Arch"`
			Cores         int         `json:"Cores"`
			Timezone      string      `json:"Timezone"`
			GoVersion     string      `json:"GoVersion"`
			CRC32         string      `json:"CRC32"`
			Version       wireVersion `json:"Version"`
			Creation      int64       `json:"Creation"`
			BuildMain     string      `json:"BuildMain"`
			BuildRevision string      `json:"BuildRevision"`
			BuildModified bool        `json:"BuildModified"`
			BuildSettings []string    `json:"BuildSettings"`
			BuildDeps     []string    `json:"BuildDeps"`
			BuildReplaces []string    `json:"BuildReplaces"`
		}{
			OS:            r.OS,
			Arch:          r.Arch,
			Cores:         r.Cores,
			Timezone:      r.Timezone,
			GoVersion:     r.GoVersion,
			CRC32:         r.CRC32,
			Version:       wireVersionFrom(r.Version),
			Creation:      r.Creation,
			BuildMain:     r.BuildMain,
			BuildRevision: r.BuildRevision,
			BuildModified: r.BuildModified,
			BuildSettings: r.BuildSettings,
			BuildDeps:     r.BuildDeps,
			BuildReplaces: r.BuildReplaces,
		}
		data, _ := json.Marshal(meta)
		s.push("node_meta", data)

	case "process_info":
		r, ok := result.(inspect.ResponseInspectProcess)
		if ok == false {
			return
		}
		data, _ := json.Marshal(wireProcessInfoFrom(inspect.MessageInspectProcess{Node: s.node, Info: r.Info}))
		s.push("process_info", data)

	case "meta_info":
		r, ok := result.(inspect.ResponseInspectMeta)
		if ok == false {
			return
		}
		data, _ := json.Marshal(wireMetaInfoFrom(inspect.MessageInspectMeta{Node: s.node, Info: r.Info}))
		s.push("meta_info", data)

	case "connection_info":
		r, ok := result.(inspect.ResponseInspectConnection)
		if ok == false {
			return
		}
		data, _ := json.Marshal(wireConnectionInfoFrom(inspect.MessageInspectConnection{Node: s.node, Disconnected: r.Disconnected, Info: r.Info}))
		s.push("connection_info", data)

	case "event_info":
		r, ok := result.(inspect.ResponseInspectEvent)
		if ok == false {
			return
		}
		data, _ := json.Marshal(wireEventStreamFrom(inspect.MessageInspectEvent{Node: s.node, Info: r.Info}))
		s.push("event_info", data)

	case "event_stream":
		r, ok := result.(inspect.ResponseInspectEventStream)
		if ok == false {
			return
		}
		data, _ := json.Marshal(wireEventStreamFrom(inspect.MessageInspectEvent{Node: s.node, Info: gen.EventInfo{Event: r.Target}, Entries: r.Buffer, Watching: r.Watching, WatchReason: r.WatchReason}))
		s.push("event_stream", data)
	}
}

func buildInspectRequest(subType string, args map[string]any) (any, string, error) {
	switch subType {
	case "node_info":
		return inspect.RequestInspectNode{}, inspect.CapNode, nil

	case "process_list":
		namePattern, _ := args["namePattern"].(string)
		behavior, _ := args["behavior"].(string)
		application, _ := args["application"].(string)
		state, _ := args["state"].(string)
		minMailbox, _ := args["minMailbox"].(float64)
		lim, _ := args["pidLimit"].(float64)

		if lim == -1 {
			return inspect.RequestInspectProcessRange{
				Limit:       10000,
				Name:        namePattern,
				Behavior:    behavior,
				Application: application,
				State:       state,
				MinMailbox:  uint64(minMailbox),
			}, inspect.CapProcessRange, nil
		}

		req := inspect.RequestInspectProcessList{Start: 1000, Limit: 500}
		if v, ok := args["pidStart"].(float64); ok {
			req.Start = int(v)
		}
		if lim > 0 {
			req.Limit = int(lim)
		}
		req.Name = namePattern
		req.Behavior = behavior
		req.Application = application
		req.State = state
		req.MinMailbox = uint64(minMailbox)
		return req, inspect.CapProcessList, nil

	case "process_info":
		p, err := argPID(args, "pid")
		if err != nil {
			return nil, "", err
		}
		return inspect.RequestInspectProcess{PID: p}, inspect.CapProcess, nil

	case "meta_info":
		key := "alias"
		if hasArg(args, key) == false {
			key = "id"
		}
		a, err := argAlias(args, key)
		if err != nil {
			return nil, "", err
		}
		return inspect.RequestInspectMeta{Meta: a}, inspect.CapMeta, nil

	case "connection_info":
		node, _ := args["node"].(string)
		if node == "" {
			return nil, "", fmt.Errorf("node is required")
		}
		return inspect.RequestInspectConnection{RemoteNode: gen.Atom(node)}, inspect.CapConnection, nil

	case "network_info":
		return inspect.RequestInspectNetwork{}, inspect.CapNetwork, nil

	case "connection_list":
		req := inspect.RequestInspectConnectionList{Limit: 100}
		if v, ok := args["limit"].(float64); ok {
			if v == -1 {
				req.Limit = 10000
			} else if v >= 1 {
				req.Limit = int(v)
			}
		}
		if v, ok := args["namePattern"].(string); ok {
			req.Name = v
		}
		return req, inspect.CapConnectionList, nil

	case "event_list":
		req := inspect.RequestInspectEventList{Limit: 500}
		if v, ok := args["timestamp"].(float64); ok {
			req.Timestamp = int64(v)
		}
		if v, ok := args["limit"].(float64); ok {
			if v == -1 {
				req.Limit = 100000
			} else if v >= 1 {
				req.Limit = int(v)
			}
		}
		if v, ok := args["namePattern"].(string); ok {
			req.Name = v
		}
		if v, ok := args["notifyMode"].(string); ok {
			switch v {
			case "yes":
				req.Notify = 1
			case "no":
				req.Notify = -1
			}
		}
		if v, ok := args["bufferedMode"].(string); ok {
			switch v {
			case "yes":
				req.Buffered = 1
			case "no":
				req.Buffered = -1
			}
		}
		if v, ok := args["openMode"].(string); ok {
			switch v {
			case "yes":
				req.Open = 1
			case "no":
				req.Open = -1
			}
		}
		if v, ok := args["minSubscribers"].(float64); ok && v > 0 {
			req.MinSubscribers = int64(v)
		}
		return req, inspect.CapEventList, nil

	case "event_info":
		req := inspect.RequestInspectEvent{}
		if v, ok := args["name"].(string); ok {
			req.Name = gen.Atom(v)
		}
		return req, inspect.CapEvent, nil

	case "event_stream":
		req := inspect.RequestInspectEventStream{Limit: 500}
		if v, ok := args["name"].(string); ok {
			req.Name = gen.Atom(v)
		}
		if v, ok := args["limit"].(float64); ok && v >= 1 {
			req.Limit = int(v)
		}
		if v, ok := args["typePattern"].(string); ok {
			req.TypePattern = v
		}
		if v, ok := args["messagePattern"].(string); ok {
			req.MessagePattern = v
		}
		if v, ok := args["messageExclude"].(bool); ok {
			req.MessageExclude = v
		}
		if v, ok := args["force"].(bool); ok {
			req.Force = v
		}
		if v, ok := args["verbose"].(bool); ok {
			req.Verbose = v
		}
		return req, inspect.CapEventStream, nil

	case "application_list":
		return inspect.RequestInspectApplicationList{}, inspect.CapApplicationList, nil

	case "log":
		req := inspect.RequestInspectLog{}
		if v, ok := args["levels"]; ok {
			if levels, ok := v.([]any); ok {
				for _, l := range levels {
					if ls, ok := l.(string); ok {
						switch ls {
						case "debug":
							req.Levels = append(req.Levels, gen.LogLevelDebug)
						case "info":
							req.Levels = append(req.Levels, gen.LogLevelInfo)
						case "warning":
							req.Levels = append(req.Levels, gen.LogLevelWarning)
						case "error":
							req.Levels = append(req.Levels, gen.LogLevelError)
						case "panic":
							req.Levels = append(req.Levels, gen.LogLevelPanic)
						}
					}
				}
			}
		}
		if v, ok := args["limit"].(float64); ok && v >= 1 {
			req.Limit = int(v)
		}
		if v, ok := args["messagePattern"].(string); ok {
			req.MessagePattern = v
		}
		if v, ok := args["messageExclude"].(bool); ok {
			req.MessageExclude = v
		}
		return req, inspect.CapLog, nil

	case "tracing_spans":
		req := inspect.RequestInspectTracing{
			Flags: gen.TracingFlagSend | gen.TracingFlagReceive | gen.TracingFlagProcs,
			Limit: 500,
		}
		if v, ok := args["limit"].(float64); ok && v >= 1 {
			req.Limit = int(v)
		}
		if v, ok := args["kinds"].(float64); ok {
			req.Kinds = uint32(v)
		}
		if v, ok := args["points"].(float64); ok {
			req.Points = uint32(v)
		}
		if v, ok := args["messagePattern"].(string); ok {
			req.MessagePattern = v
		}
		if v, ok := args["messageExclude"].(bool); ok {
			req.MessageExclude = v
		}
		return req, inspect.CapTracing, nil
	}

	return nil, "", fmt.Errorf("unknown subscription type: %s", subType)
}

func extractEvent(result any) (gen.Event, error) {
	switch r := result.(type) {
	case inspect.ResponseInspectNode:
		return r.Event, nil
	case inspect.ResponseInspectProcessList:
		return r.Event, nil
	case inspect.ResponseInspectProcessRange:
		return r.Event, nil
	case inspect.ResponseInspectNetwork:
		return r.Event, nil
	case inspect.ResponseInspectEventList:
		return r.Event, nil
	case inspect.ResponseInspectEvent:
		return r.Event, nil
	case inspect.ResponseInspectEventStream:
		return r.Event, nil
	case inspect.ResponseInspectConnectionList:
		return r.Event, nil
	case inspect.ResponseInspectApplicationList:
		return r.Event, nil
	case inspect.ResponseInspectLog:
		return r.Event, nil
	case inspect.ResponseInspectProcess:
		return r.Event, nil
	case inspect.ResponseInspectMeta:
		return r.Event, nil
	case inspect.ResponseInspectConnection:
		return r.Event, nil
	case inspect.ResponseInspectTracing:
		return r.Event, nil
	case error:
		return gen.Event{}, r
	}
	return gen.Event{}, fmt.Errorf("unexpected response: %T", result)
}

func inspectEventToSSEType(name gen.Atom) string {
	n := string(name)
	switch {
	case n == "inspect_node":
		return "node_info"
	case strings.HasPrefix(n, "inspect_process_list"):
		return "process_list"
	case strings.HasPrefix(n, "inspect_process_range"):
		return "process_list"
	case strings.HasPrefix(n, "inspect_process"):
		return "process_info"
	case strings.HasPrefix(n, "inspect_meta"):
		return "meta_info"
	case n == "inspect_network":
		return "network_info"
	case strings.HasPrefix(n, "inspect_connection_list"):
		return "connection_list"
	case strings.HasPrefix(n, "inspect_connection"):
		return "connection_info"
	case strings.HasPrefix(n, "inspect_event_list"):
		return "event_list"
	case strings.HasPrefix(n, "inspect_event_stream"):
		return "event_stream"
	case strings.HasPrefix(n, "inspect_event"):
		return "event_info"
	case n == "inspect_application_list":
		return "application_list"
	case strings.HasPrefix(n, "inspect_log"):
		return "log"
	case strings.HasPrefix(n, "inspect_tracing"):
		return "tracing_spans"
	}
	return n
}

func subHandle(session string, node gen.Atom, subType string, args map[string]any) string {
	return session + "|" + string(node) + "|" + subLookupKey(subType, args)
}

func subLookupKey(subType string, args map[string]any) string {
	switch subType {
	case "process_info":
		if pid, err := argPID(args, "pid"); err == nil {
			return fmt.Sprintf("%s:pid=%s.%d", subType, pid, pid.Creation)
		}
	case "meta_info":
		key := "alias"
		if hasArg(args, key) == false {
			key = "id"
		}
		if alias, err := argAlias(args, key); err == nil {
			return fmt.Sprintf("%s:alias=%s.%d", subType, alias, alias.Creation)
		}
	case "connection_info":
		if node, ok := args["node"].(string); ok {
			return subType + ":node=" + node
		}
	case "event_info":
		if name, ok := args["name"].(string); ok {
			return subType + ":name=" + name
		}
	case "event_stream":
		if name, ok := args["name"].(string); ok {
			limit := 500
			if v, ok := args["limit"].(float64); ok && v >= 1 {
				limit = int(v)
			}
			tp, _ := args["typePattern"].(string)
			mp, _ := args["messagePattern"].(string)
			mx, _ := args["messageExclude"].(bool)
			force, _ := args["force"].(bool)
			verbose, _ := args["verbose"].(bool)
			return fmt.Sprintf("%s:name=%s|limit=%d|tp=%s|mp=%s|mx=%v|force=%v|verbose=%v",
				subType, name, limit, tp, mp, mx, force, verbose)
		}
	case "process_list":
		start, _ := args["pidStart"].(float64)
		limit, _ := args["pidLimit"].(float64)
		nameP, _ := args["namePattern"].(string)
		behaviorP, _ := args["behavior"].(string)
		appP, _ := args["application"].(string)
		stateP, _ := args["state"].(string)
		mailboxP, _ := args["minMailbox"].(float64)
		if limit == -1 {
			return fmt.Sprintf("%s:range:name=%s:beh=%s:app=%s:state=%s:mbox=%d",
				subType, nameP, behaviorP, appP, stateP, int(mailboxP))
		}
		return fmt.Sprintf("%s:start=%d:limit=%d:name=%s:beh=%s:app=%s:state=%s:mbox=%d",
			subType, int(start), int(limit), nameP, behaviorP, appP, stateP, int(mailboxP))
	case "connection_list":
		clLimit, _ := args["limit"].(float64)
		clName, _ := args["namePattern"].(string)
		return fmt.Sprintf("%s:limit=%d:name=%s", subType, int(clLimit), clName)
	case "event_list":
		ts, _ := args["timestamp"].(float64)
		limit, _ := args["limit"].(float64)
		name, _ := args["namePattern"].(string)
		notifyMode, _ := args["notifyMode"].(string)
		bufferedMode, _ := args["bufferedMode"].(string)
		minSubs, _ := args["minSubscribers"].(float64)
		return fmt.Sprintf("%s:ts=%d:limit=%d:name=%s:notify=%s:buffered=%s:minsubs=%d",
			subType, int(ts), int(limit), name, notifyMode, bufferedMode, int(minSubs))
	case "event":
		name, _ := args["name"].(string)
		limit, _ := args["limit"].(float64)
		typeP, _ := args["typePattern"].(string)
		msgP, _ := args["messagePattern"].(string)
		excl, _ := args["messageExclude"].(bool)
		force, _ := args["force"].(bool)
		return fmt.Sprintf("%s:name=%s:limit=%d:type=%s:msg=%s:excl=%v:force=%v",
			subType, name, int(limit), typeP, msgP, excl, force)
	case "log":
		var levs []string
		if ls, ok := args["levels"].([]any); ok {
			for _, l := range ls {
				if s, ok := l.(string); ok {
					levs = append(levs, s)
				}
			}
			sort.Strings(levs)
		}
		return fmt.Sprintf("%s:%s:%v:%v:%v", subType, strings.Join(levs, ","), args["limit"], args["messagePattern"], args["messageExclude"])
	case "tracing_spans":
		limit, _ := args["limit"].(float64)
		return fmt.Sprintf("%s:limit=%d", subType, int(limit))
	}
	return subType
}
