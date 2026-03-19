package observer

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"ergo.services/ergo/act"
	"ergo.services/ergo/app/system/inspect"
	"ergo.services/ergo/gen"
	"ergo.services/meta/sse"
)

func factory_session() gen.ProcessBehavior {
	return &session{}
}

type session struct {
	act.Actor

	id            string
	sseAlias      gen.Alias
	node          gen.Atom
	creation      int64
	subscriptions map[string]gen.Event // eventKey → gen.Event (for DemonitorEvent)
	subIndex      map[string]string    // lookupKey → eventKey (for unsubscribe lookup)
	eventCounter  int64
}

func (s *session) Init(args ...any) error {
	s.id = args[0].(string)
	s.sseAlias = args[1].(gen.Alias)
	s.node = s.Node().Name()
	s.creation = s.Node().Creation()
	s.subscriptions = make(map[string]gen.Event)
	s.subIndex = make(map[string]string)

	s.Log().SetLogger("default")

	// link to SSE connection meta — session dies when SSE dies
	if err := s.LinkAlias(s.sseAlias); err != nil {
		s.Log().Error("session %s: LinkAlias failed: %s", s.id, err)
		return err
	}

	// send "connected" event to browser with session ID
	s.sendConnectedEvent()

	s.Log().Info("session %s started, SSE: %s", s.id, s.sseAlias)
	return nil
}

func (s *session) HandleMessage(from gen.PID, message any) error {
	switch m := message.(type) {
	case gen.MessageDownEvent:
		// inspect event source terminated (e.g. observed process died)
		key := m.Event.String()
		if _, exist := s.subscriptions[key]; exist {
			// build terminated payload in the same format as inspect sends
			// so frontend handles it with the same code path
			eventType := inspectEventToSSEType(m.Event.Name)
			var payload any

			// find lookup key to get the ID
			lookupKey := ""
			for k, v := range s.subIndex {
				if v == key {
					lookupKey = k
					break
				}
			}

			switch eventType {
			case "process_info":
				// extract PID from "process_info:pid=<CRC.0.1006>"
				pid := extractIDFromLookupKey(lookupKey)
				p, _ := str2pid(s.node, s.creation, pid)
				payload = inspect.MessageInspectProcess{
					Node: s.node,
					Info: gen.ProcessInfo{
						PID:   p,
						State: gen.ProcessStateTerminated,
					},
				}
			case "meta_info":
				alias := extractIDFromLookupKey(lookupKey)
				a, _ := str2alias(s.node, s.creation, alias)
				payload = inspect.MessageInspectMeta{
					Node: s.node,
					Info: gen.MetaInfo{
						ID:    a,
						State: gen.MetaStateTerminated,
					},
				}
			case "connection_info":
				node := extractIDFromLookupKey(lookupKey)
				payload = inspect.MessageInspectConnection{
					Node:         s.node,
					Disconnected: true,
					Info: gen.RemoteNodeInfo{
						Node: gen.Atom(node),
					},
				}
			default:
				payload = struct {
					State string `json:"State"`
				}{State: "terminated"}
			}

			data, _ := json.Marshal(payload)
			s.eventCounter++
			s.SendAlias(s.sseAlias, sse.Message{
				Event: eventType,
				Data:  data,
				MsgID: fmt.Sprintf("%d", s.eventCounter),
			})

			delete(s.subscriptions, key)
			for k, v := range s.subIndex {
				if v == key {
					delete(s.subIndex, k)
					break
				}
			}
			s.Log().Info("session %s: event source terminated: %s", s.id, key)
		}

	default:
		s.Log().Warning("session %s: unexpected message from %s: %#v", s.id, from, message)
	}
	return nil
}

func (s *session) HandleCall(from gen.PID, ref gen.Ref, request any) (any, error) {
	switch r := request.(type) {
	case commandRequest:
		return s.handleCommand(r)
	case actionRequest:
		return s.handleAction(r)
	}
	return nil, gen.ErrUnsupported
}

func (s *session) HandleEvent(message gen.MessageEvent) error {
	key := message.Event.String()
	if _, exist := s.subscriptions[key]; exist == false {
		s.Log().Warning("session %s: event for unknown subscription: %s (have %d subs)", s.id, key, len(s.subscriptions))
		return nil
	}
	s.Log().Debug("session %s: event %s", s.id, key)

	// check if this is a registrar event — send cluster_update instead of raw data
	if s.subIndex["registrar_event"] == key {
		s.sendClusterUpdate()
		return nil
	}

	data, err := json.Marshal(message.Message)
	if err != nil {
		s.Log().Error("session %s: marshal event %s: %s", s.id, key, err)
		return nil
	}

	eventType := inspectEventToSSEType(message.Event.Name)
	s.eventCounter++

	sseMsg := sse.Message{
		Event: eventType,
		Data:  data,
		MsgID: fmt.Sprintf("%d", s.eventCounter),
	}
	if err := s.SendAlias(s.sseAlias, sseMsg); err != nil {
		s.Log().Error("session %s: SendAlias failed: %s", s.id, err)
	}
	return nil
}

func (s *session) Terminate(reason error) {
	s.Log().Info("session %s terminated: %s", s.id, reason)
}

// handleCommand processes subscribe/unsubscribe/switch
func (s *session) handleCommand(cmd commandRequest) (any, error) {
	switch cmd.Command {
	case "subscribe":
		return s.doSubscribe(cmd.Type, cmd.Args)
	case "unsubscribe":
		s.doUnsubscribe(cmd.Type, cmd.Args)
		return apiResponse{OK: true}, nil
	case "switch":
		node, _ := cmd.Args["node"].(string)
		if node == "" {
			return apiResponse{Error: "node is required"}, nil
		}
		return s.doSwitch(gen.Atom(node), cmd.Args)
	}
	return apiResponse{Error: "unknown command: " + cmd.Command}, nil
}

// handleAction processes do/* commands by forwarding to system_inspect on the observed node
func (s *session) handleAction(req actionRequest) (any, error) {
	inspectReq, err := s.buildActionRequest(req.Action, req.Args)
	if err != nil {
		return apiResponse{Error: err.Error()}, nil
	}

	inspectPID := gen.ProcessID{Name: inspect.Name, Node: s.node}
	result, err := s.CallWithTimeout(inspectPID, inspectReq, defaultCallTimeout)
	if err != nil {
		return apiResponse{Error: fmt.Sprintf("action %s: %s", req.Action, err)}, nil
	}

	// extract error and optional data from response
	if e := actionError(result); e != nil {
		return apiResponse{Error: e.Error()}, nil
	}
	// some actions return data
	if r, ok := result.(inspect.ResponseDoInspect); ok {
		return apiResponse{OK: true, Data: r.State}, nil
	}
	if r, ok := result.(inspect.ResponseDoGoroutines); ok {
		if r.Error != nil {
			return apiResponse{Error: r.Error.Error()}, nil
		}
		return apiResponse{OK: true, Data: r}, nil
	}
	if r, ok := result.(inspect.ResponseDoHeapProfile); ok {
		if r.Error != nil {
			return apiResponse{Error: r.Error.Error()}, nil
		}
		return apiResponse{OK: true, Data: r}, nil
	}
	return apiResponse{OK: true}, nil
}

func (s *session) buildActionRequest(action string, args map[string]any) (any, error) {
	switch action {
	case "send":
		// meta process
		if aliasStr, ok := args["alias"].(string); ok && aliasStr != "" {
			a, err := str2alias(s.node, s.creation, aliasStr)
			if err != nil {
				return nil, fmt.Errorf("invalid alias: %s", err)
			}
			return inspect.RequestDoSendMeta{
				Meta:    a,
				Message: args["message"],
			}, nil
		}
		// regular process
		pidStr, _ := args["pid"].(string)
		if pidStr == "" {
			return nil, fmt.Errorf("pid or alias is required")
		}
		p, err := str2pid(s.node, s.creation, pidStr)
		if err != nil {
			return nil, fmt.Errorf("invalid pid: %s", err)
		}
		return inspect.RequestDoSend{
			PID:     p,
			Message: args["message"],
		}, nil

	case "send_exit":
		reason, _ := args["reason"].(string)
		if reason == "" {
			reason = "normal"
		}
		// meta process
		if aliasStr, ok := args["alias"].(string); ok && aliasStr != "" {
			a, err := str2alias(s.node, s.creation, aliasStr)
			if err != nil {
				return nil, fmt.Errorf("invalid alias: %s", err)
			}
			return inspect.RequestDoSendExitMeta{
				Meta:   a,
				Reason: errors.New(reason),
			}, nil
		}
		// regular process
		pidStr, _ := args["pid"].(string)
		if pidStr == "" {
			return nil, fmt.Errorf("pid or alias is required")
		}
		p, err := str2pid(s.node, s.creation, pidStr)
		if err != nil {
			return nil, fmt.Errorf("invalid pid: %s", err)
		}
		return inspect.RequestDoSendExit{
			PID:    p,
			Reason: errors.New(reason),
		}, nil

	case "kill":
		pidStr, _ := args["pid"].(string)
		if pidStr == "" {
			return nil, fmt.Errorf("pid is required")
		}
		p, err := str2pid(s.node, s.creation, pidStr)
		if err != nil {
			return nil, fmt.Errorf("invalid pid: %s", err)
		}
		return inspect.RequestDoKill{PID: p}, nil

	case "set_log_level":
		levelStr, _ := args["level"].(string)
		level := parseLogLevel(levelStr)

		target, _ := args["target"].(string)
		switch target {
		case "process":
			pidStr, _ := args["pid"].(string)
			p, err := str2pid(s.node, s.creation, pidStr)
			if err != nil {
				return nil, fmt.Errorf("invalid pid: %s", err)
			}
			return inspect.RequestDoSetProcessLogLevel{PID: p, Level: level}, nil
		case "meta":
			aliasStr, _ := args["alias"].(string)
			a, err := str2alias(s.node, s.creation, aliasStr)
			if err != nil {
				return nil, fmt.Errorf("invalid alias: %s", err)
			}
			return inspect.RequestDoSetMetaLogLevel{Meta: a, Level: level}, nil
		default:
			// node-level log level
			return inspect.RequestDoSetLogLevel{Level: level}, nil
		}
	case "app_start":
		name, _ := args["name"].(string)
		if name == "" {
			return nil, fmt.Errorf("name is required")
		}
		mode, _ := args["mode"].(string)
		return inspect.RequestDoAppStart{
			Name: gen.Atom(name),
			Mode: parseAppMode(mode),
		}, nil

	case "app_stop":
		name, _ := args["name"].(string)
		if name == "" {
			return nil, fmt.Errorf("name is required")
		}
		force, _ := args["force"].(bool)
		return inspect.RequestDoAppStop{Name: gen.Atom(name), Force: force}, nil

	case "app_unload":
		name, _ := args["name"].(string)
		if name == "" {
			return nil, fmt.Errorf("name is required")
		}
		return inspect.RequestDoAppUnload{Name: gen.Atom(name)}, nil

	case "inspect":
		pidStr, _ := args["pid"].(string)
		if pidStr == "" {
			return nil, fmt.Errorf("pid is required")
		}
		p, err := str2pid(s.node, s.creation, pidStr)
		if err != nil {
			return nil, fmt.Errorf("invalid pid: %s", err)
		}
		return inspect.RequestDoInspect{PID: p}, nil

	// process settings

	case "set_process_send_priority":
		p, err := s.requirePID(args)
		if err != nil {
			return nil, err
		}
		v, _ := args["priority"].(string)
		return inspect.RequestDoSetProcessSendPriority{PID: p, Priority: parsePriority(v)}, nil

	case "set_process_compression":
		p, err := s.requirePID(args)
		if err != nil {
			return nil, err
		}
		enabled, _ := args["enabled"].(bool)
		return inspect.RequestDoSetProcessCompression{PID: p, Enabled: enabled}, nil

	case "set_process_compression_type":
		p, err := s.requirePID(args)
		if err != nil {
			return nil, err
		}
		v, _ := args["type"].(string)
		return inspect.RequestDoSetProcessCompressionType{PID: p, Type: parseCompressionType(v)}, nil

	case "set_process_compression_level":
		p, err := s.requirePID(args)
		if err != nil {
			return nil, err
		}
		v, _ := args["level"].(string)
		return inspect.RequestDoSetProcessCompressionLevel{PID: p, Level: parseCompressionLevel(v)}, nil

	case "set_process_compression_threshold":
		p, err := s.requirePID(args)
		if err != nil {
			return nil, err
		}
		threshold, _ := args["threshold"].(float64) // JSON numbers are float64
		return inspect.RequestDoSetProcessCompressionThreshold{PID: p, Threshold: int(threshold)}, nil

	case "set_process_keep_network_order":
		p, err := s.requirePID(args)
		if err != nil {
			return nil, err
		}
		order, _ := args["order"].(bool)
		return inspect.RequestDoSetProcessKeepNetworkOrder{PID: p, Order: order}, nil

	case "set_process_important_delivery":
		p, err := s.requirePID(args)
		if err != nil {
			return nil, err
		}
		important, _ := args["important"].(bool)
		return inspect.RequestDoSetProcessImportantDelivery{PID: p, Important: important}, nil

	// meta settings

	case "set_meta_send_priority":
		a, err := s.requireAlias(args)
		if err != nil {
			return nil, err
		}
		v, _ := args["priority"].(string)
		return inspect.RequestDoSetMetaSendPriority{Meta: a, Priority: parsePriority(v)}, nil

	case "goroutines":
		stack, _ := args["stack"].(string)
		state, _ := args["state"].(string)
		minWait, _ := args["minWait"].(float64)
		return inspect.RequestDoGoroutines{
			Stack:   stack,
			State:   state,
			MinWait: int64(minWait),
		}, nil

	case "heap":
		minBytes, _ := args["minBytes"].(float64)
		return inspect.RequestDoHeapProfile{MinBytes: int64(minBytes)}, nil
	}
	return nil, fmt.Errorf("unknown action: %s", action)
}

func parseAppMode(s string) gen.ApplicationMode {
	switch s {
	case "temporary":
		return gen.ApplicationModeTemporary
	case "transient":
		return gen.ApplicationModeTransient
	case "permanent":
		return gen.ApplicationModePermanent
	}
	return 0 // default
}

func (s *session) requirePID(args map[string]any) (gen.PID, error) {
	pidStr, _ := args["pid"].(string)
	if pidStr == "" {
		return gen.PID{}, fmt.Errorf("pid is required")
	}
	return str2pid(s.node, s.creation, pidStr)
}

func (s *session) requireAlias(args map[string]any) (gen.Alias, error) {
	aliasStr, _ := args["alias"].(string)
	if aliasStr == "" {
		return gen.Alias{}, fmt.Errorf("alias is required")
	}
	return str2alias(s.node, s.creation, aliasStr)
}

func parsePriority(s string) gen.MessagePriority {
	switch s {
	case "high":
		return gen.MessagePriorityHigh
	case "max":
		return gen.MessagePriorityMax
	}
	return gen.MessagePriorityNormal
}

func parseCompressionType(s string) gen.CompressionType {
	switch s {
	case "lzw":
		return gen.CompressionTypeLZW
	case "zlib":
		return gen.CompressionTypeZLIB
	}
	return gen.CompressionTypeGZIP
}

func parseCompressionLevel(s string) gen.CompressionLevel {
	switch s {
	case "best speed":
		return gen.CompressionBestSpeed
	case "best size":
		return gen.CompressionBestSize
	}
	return gen.CompressionDefault
}

func parseLogLevel(s string) gen.LogLevel {
	switch s {
	case "debug":
		return gen.LogLevelDebug
	case "info":
		return gen.LogLevelInfo
	case "warning":
		return gen.LogLevelWarning
	case "error":
		return gen.LogLevelError
	case "panic":
		return gen.LogLevelPanic
	case "disabled":
		return gen.LogLevelDisabled
	}
	return gen.LogLevelInfo
}

func actionError(result any) error {
	switch r := result.(type) {
	case inspect.ResponseDoSend:
		return r.Error
	case inspect.ResponseDoSendMeta:
		return r.Error
	case inspect.ResponseDoSendExit:
		return r.Error
	case inspect.ResponseDoSendExitMeta:
		return r.Error
	case inspect.ResponseDoKill:
		return r.Error
	case inspect.ResponseDoSetLogLevel:
		return r.Error
	case inspect.ResponseDoAppStart:
		return r.Error
	case inspect.ResponseDoAppStop:
		return r.Error
	case inspect.ResponseDoAppUnload:
		return r.Error
	case inspect.ResponseDoInspect:
		return r.Error
	case inspect.ResponseDoSet:
		return r.Error
	}
	return nil
}

// doSubscribe calls system_inspect to start inspector, then MonitorEvent
func (s *session) doSubscribe(subType string, args map[string]any) (any, error) {
	inspectReq, err := s.buildInspectRequest(subType, args)
	if err != nil {
		return apiResponse{Error: err.Error()}, nil
	}

	// call system_inspect to start/reuse the inspector child
	inspectPID := gen.ProcessID{Name: inspect.Name, Node: s.node}
	result, err := s.CallWithTimeout(inspectPID, inspectReq, defaultCallTimeout)
	if err != nil {
		return apiResponse{Error: fmt.Sprintf("inspect call: %s", err)}, nil
	}

	// extract Event from response
	event, err := extractEvent(result)
	if err != nil {
		return apiResponse{Error: fmt.Sprintf("inspect response: %s", err)}, nil
	}

	eventKey := event.String()

	// for log subscriptions, auto-unsubscribe previous if filter changed
	lookupKey := subLookupKey(subType, args)
	if oldEventKey, exist := s.subIndex[lookupKey]; exist && oldEventKey != eventKey {
		if ev, ok := s.subscriptions[oldEventKey]; ok {
			s.DemonitorEvent(ev)
			delete(s.subscriptions, oldEventKey)
		}
		delete(s.subIndex, lookupKey)
		s.Log().Info("session %s: auto-unsubscribed %s (replaced)", s.id, oldEventKey)
	}

	// dedup by event key
	if _, exist := s.subscriptions[eventKey]; exist {
		return apiResponse{OK: true}, nil
	}

	// monitor the inspect event
	if _, err := s.MonitorEvent(event); err != nil {
		return apiResponse{Error: fmt.Sprintf("monitor: %s", err)}, nil
	}

	s.subscriptions[eventKey] = event
	s.subIndex[lookupKey] = eventKey
	s.Log().Info("session %s: subscribed %s [%s] → %s (total subs: %d)", s.id, subType, lookupKey, eventKey, len(s.subscriptions))

	// send initial data from inspect response for types that carry extra info
	s.sendInitialData(subType, result)

	return apiResponse{OK: true}, nil
}

// doUnsubscribe removes a subscription by lookup key
func (s *session) doUnsubscribe(subType string, args map[string]any) {
	lookupKey := subLookupKey(subType, args)
	eventKey, exist := s.subIndex[lookupKey]
	if exist == false {
		s.Log().Warning("session %s: unsubscribe %s not found", s.id, lookupKey)
		return
	}

	if ev, ok := s.subscriptions[eventKey]; ok {
		s.DemonitorEvent(ev)
		delete(s.subscriptions, eventKey)
	}
	delete(s.subIndex, lookupKey)
	s.Log().Info("session %s: unsubscribed %s → %s", s.id, lookupKey, eventKey)
}

// doSwitch changes observed node. Args may contain route options: Cookie, Host, Port, TLS.
func (s *session) doSwitch(newNode gen.Atom, args map[string]any) (any, error) {
	if s.node == newNode {
		return apiResponse{OK: true}, nil
	}

	// establish connection if not yet connected (like old observer's tryConnect)
	if err := s.tryConnect(newNode, args); err != nil {
		return apiResponse{Error: fmt.Sprintf("connect to %s: %s", newNode, err)}, nil
	}

	// inspect remote node
	inspectPID := gen.ProcessID{Name: inspect.Name, Node: newNode}
	result, err := s.CallWithTimeout(inspectPID, inspect.RequestInspectNode{}, defaultCallTimeout)
	if err != nil {
		return apiResponse{Error: fmt.Sprintf("inspect %s: %s", newNode, err)}, nil
	}
	r, ok := result.(inspect.ResponseInspectNode)
	if ok == false {
		return apiResponse{Error: "unexpected response from remote inspect"}, nil
	}

	// unsubscribe all from current node
	for key, ev := range s.subscriptions {
		s.DemonitorEvent(ev)
		delete(s.subscriptions, key)
	}
	s.subIndex = make(map[string]string)

	s.node = newNode
	s.creation = r.Creation
	s.sendConnectedEvent()
	return apiResponse{OK: true}, nil
}

// tryConnect ensures network connection to the target node.
// If already connected — no-op. Otherwise tries registrar, then explicit route from args.
func (s *session) tryConnect(node gen.Atom, args map[string]any) error {
	// connecting to self
	if node == s.Node().Name() {
		return nil
	}

	// already connected
	if _, err := s.Node().Network().Node(node); err == nil {
		return nil
	}

	// build route from args
	nr := gen.NetworkRoute{}

	// try registrar first
	if reg, err := s.Node().Network().Registrar(); err == nil {
		if routes, err := reg.Resolver().Resolve(node); err == nil && len(routes) > 0 {
			nr.Route = routes[0]
		}
	}

	// override with explicit args
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

	// if we have any route info, use explicit route
	if nr.Route.Host != "" || nr.Route.Port > 0 || nr.Cookie != "" {
		_, err := s.Node().Network().GetNodeWithRoute(node, nr)
		return err
	}

	// fallback: auto-discovery
	_, err := s.Node().Network().GetNode(node)
	return err
}

type nodeDesc struct {
	Name      gen.Atom `json:"Name"`
	CRC32     string   `json:"CRC32"`
	Connected bool     `json:"Connected"`
}

// collectNodes gathers connected peers and cluster nodes from registrar
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

// sendConnectedEvent sends session info to browser via SSE.
// Includes peers (connected nodes) and cluster nodes (from registrar).
// Also subscribes to registrar event for cluster changes.
func (s *session) sendConnectedEvent() {
	nodes := s.collectNodes()

	// subscribe to registrar event for cluster changes (skip if already subscribed)
	registrar, err := s.Node().Network().Registrar()
	if err == nil {
		regEvent, err := registrar.Event()
		if err == nil {
			if _, exist := s.subIndex["registrar_event"]; exist == false {
				s.MonitorEvent(regEvent)
				s.subscriptions[regEvent.String()] = regEvent
				s.subIndex["registrar_event"] = regEvent.String()
			}
		}
	}

	intro := struct {
		SessionID string      `json:"SessionID"`
		Node      nodeDesc    `json:"Node"`
		Nodes     []nodeDesc  `json:"Nodes"`
		Version   gen.Version `json:"Version"`
	}{
		SessionID: s.id,
		Node:      nodeDesc{Name: s.node, CRC32: s.node.CRC32(), Connected: true},
		Nodes:     nodes,
		Version:   Version,
	}

	data, _ := json.Marshal(intro)
	s.eventCounter++
	s.SendAlias(s.sseAlias, sse.Message{
		Event: "connected",
		Data:  data,
		MsgID: fmt.Sprintf("%d", s.eventCounter),
	})
}

// sendClusterUpdate re-reads cluster nodes and sends update to browser
func (s *session) sendClusterUpdate() {
	payload := struct {
		Nodes []nodeDesc `json:"Nodes"`
	}{Nodes: s.collectNodes()}

	data, _ := json.Marshal(payload)
	s.eventCounter++
	s.SendAlias(s.sseAlias, sse.Message{
		Event: "cluster_update",
		Data:  data,
		MsgID: fmt.Sprintf("%d", s.eventCounter),
	})
}

// sendInitialData sends extra info from the inspect response as a separate SSE event
func (s *session) sendInitialData(subType string, result any) {
	switch subType {
	case "node_info":
		r, ok := result.(inspect.ResponseInspectNode)
		if ok == false {
			return
		}
		// send node_meta event with OS, Arch, Cores, CRC32, Timezone
		meta := struct {
			OS       string      `json:"OS"`
			Arch     string      `json:"Arch"`
			Cores    int         `json:"Cores"`
			Timezone string      `json:"Timezone"`
			CRC32    string      `json:"CRC32"`
			Version  gen.Version `json:"Version"`
			Creation int64       `json:"Creation"`
		}{
			OS:       r.OS,
			Arch:     r.Arch,
			Cores:    r.Cores,
			Timezone: r.Timezone,
			CRC32:    r.CRC32,
			Version:  r.Version,
			Creation: r.Creation,
		}
		data, _ := json.Marshal(meta)
		s.eventCounter++
		s.SendAlias(s.sseAlias, sse.Message{
			Event: "node_meta",
			Data:  data,
			MsgID: fmt.Sprintf("%d", s.eventCounter),
		})
	}
}

// buildInspectRequest creates RequestInspect* for the subscription type
func (s *session) buildInspectRequest(subType string, args map[string]any) (any, error) {
	switch subType {
	case "node_info":
		return inspect.RequestInspectNode{}, nil

	case "process_list":
		namePattern, _ := args["namePattern"].(string)
		behavior, _ := args["behavior"].(string)
		application, _ := args["application"].(string)
		state, _ := args["state"].(string)
		minMailbox, _ := args["minMailbox"].(float64)
		lim, _ := args["pidLimit"].(float64)

		// "all" mode: full unordered scan via ProcessRange
		if lim == -1 {
			return inspect.RequestInspectProcessRange{
				Limit:       10000,
				Name:        namePattern,
				Behavior:    behavior,
				Application: application,
				State:       state,
				MinMailbox:  uint64(minMailbox),
			}, nil
		}

		// first/last/pid mode: ordered scan via ProcessList (with optional filter)
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
		return req, nil

	case "process_info":
		pid, _ := args["pid"].(string)
		if pid == "" {
			return nil, fmt.Errorf("pid is required")
		}
		p, err := str2pid(s.node, s.creation, pid)
		if err != nil {
			return nil, fmt.Errorf("invalid pid %q: %s", pid, err)
		}
		return inspect.RequestInspectProcess{PID: p}, nil

	case "process_state":
		pid, _ := args["pid"].(string)
		if pid == "" {
			return nil, fmt.Errorf("pid is required")
		}
		p, err := str2pid(s.node, s.creation, pid)
		if err != nil {
			return nil, fmt.Errorf("invalid pid %q: %s", pid, err)
		}
		return inspect.RequestInspectProcessState{PID: p}, nil

	case "meta_info":
		alias, _ := args["alias"].(string)
		if alias == "" {
			alias, _ = args["id"].(string)
		}
		if alias == "" {
			return nil, fmt.Errorf("alias is required")
		}
		a, err := str2alias(s.node, s.creation, alias)
		if err != nil {
			return nil, fmt.Errorf("invalid alias %q: %s", alias, err)
		}
		return inspect.RequestInspectMeta{Meta: a}, nil

	case "meta_state":
		alias, _ := args["alias"].(string)
		if alias == "" {
			alias, _ = args["id"].(string)
		}
		if alias == "" {
			return nil, fmt.Errorf("alias is required")
		}
		a, err := str2alias(s.node, s.creation, alias)
		if err != nil {
			return nil, fmt.Errorf("invalid alias %q: %s", alias, err)
		}
		return inspect.RequestInspectMetaState{Meta: a}, nil

	case "connection_info":
		node, _ := args["node"].(string)
		if node == "" {
			return nil, fmt.Errorf("node is required")
		}
		return inspect.RequestInspectConnection{RemoteNode: gen.Atom(node)}, nil

	case "network_info":
		return inspect.RequestInspectNetwork{}, nil

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
		return req, nil

	case "event_list":
		req := inspect.RequestInspectEventList{Limit: 500}
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
		if v, ok := args["minSubscribers"].(float64); ok && v > 0 {
			req.MinSubscribers = int64(v)
		}
		return req, nil

	case "application_list":
		return inspect.RequestInspectApplicationList{}, nil

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
		return req, nil

	case "application_tree":
		req := inspect.RequestInspectApplicationTree{Limit: 1000}
		if v, ok := args["app"].(string); ok && v != "" {
			req.Application = gen.Atom(v)
		}
		if v, ok := args["limit"].(float64); ok && v >= 1 {
			req.Limit = int(v)
		}
		return req, nil

	case "heap":
		req := inspect.RequestInspectHeap{Limit: 100}
		if v, ok := args["limit"].(float64); ok && v >= 1 {
			req.Limit = int(v)
		}
		if v, ok := args["name"].(string); ok {
			req.Name = v
		}
		return req, nil
	}

	return nil, fmt.Errorf("unknown subscription type: %s", subType)
}

// extractEvent gets gen.Event from inspect response types
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
	case inspect.ResponseInspectProcessState:
		return r.Event, nil
	case inspect.ResponseInspectMetaState:
		return r.Event, nil
	case inspect.ResponseInspectApplicationTree:
		return r.Event, nil
	case inspect.ResponseInspectHeap:
		return r.Event, nil
	case error:
		return gen.Event{}, r
	}
	return gen.Event{}, fmt.Errorf("unexpected response: %T", result)
}

// inspectEventToSSEType maps inspect event name to frontend SSE event type
func inspectEventToSSEType(name gen.Atom) string {
	n := string(name)
	switch {
	case n == "inspect_node":
		return "node_info"
	case strings.HasPrefix(n, "inspect_process_list"):
		return "process_list"
	case strings.HasPrefix(n, "inspect_process_range"):
		return "process_list"
	case strings.HasPrefix(n, "inspect_process_state"):
		return "process_state"
	case strings.HasPrefix(n, "inspect_process"):
		return "process_info"
	case strings.HasPrefix(n, "inspect_meta_state"):
		return "meta_state"
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
	case n == "inspect_application_list":
		return "application_list"
	case strings.HasPrefix(n, "inspect_application_tree"):
		return "application_tree"
	case strings.HasPrefix(n, "inspect_log"):
		return "log"
	case strings.HasPrefix(n, "inspect_heap"):
		return "heap"
	}
	return n
}

// extractIDFromLookupKey extracts the ID value from a lookup key like "process_info:pid=<CRC.0.1006>"
func extractIDFromLookupKey(key string) string {
	if idx := strings.Index(key, "="); idx >= 0 {
		return key[idx+1:]
	}
	return ""
}

// subLookupKey builds a stable key from subscription type + args for O(1) lookup.
// Examples: "node_info", "process_info:pid=<9F35C982.0.1006>", "connection_info:node=remote@host"
func subLookupKey(subType string, args map[string]any) string {
	switch subType {
	case "process_info":
		if pid, ok := args["pid"].(string); ok {
			return subType + ":pid=" + pid
		}
	case "process_state":
		if pid, ok := args["pid"].(string); ok {
			return subType + ":pid=" + pid
		}
	case "meta_info":
		alias, _ := args["alias"].(string)
		if alias == "" {
			alias, _ = args["id"].(string)
		}
		if alias != "" {
			return subType + ":alias=" + alias
		}
	case "connection_info":
		if node, ok := args["node"].(string); ok {
			return subType + ":node=" + node
		}
	case "meta_state":
		alias, _ := args["alias"].(string)
		if alias == "" {
			alias, _ = args["id"].(string)
		}
		if alias != "" {
			return subType + ":alias=" + alias
		}
	case "application_tree":
		if app, ok := args["app"].(string); ok {
			return subType + ":app=" + app
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
		limit, _ := args["limit"].(float64)
		name, _ := args["namePattern"].(string)
		notifyMode, _ := args["notifyMode"].(string)
		bufferedMode, _ := args["bufferedMode"].(string)
		minSubs, _ := args["minSubscribers"].(float64)
		return fmt.Sprintf("%s:limit=%d:name=%s:notify=%s:buffered=%s:minsubs=%d",
			subType, int(limit), name, notifyMode, bufferedMode, int(minSubs))
	case "heap":
		limit, _ := args["limit"].(float64)
		name, _ := args["name"].(string)
		return fmt.Sprintf("%s:limit=%d:name=%s", subType, int(limit), name)
	}
	return subType
}
