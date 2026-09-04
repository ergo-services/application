package observer

import (
	"encoding/json"
	"strings"
	"testing"

	"ergo.services/ergo/app/system/inspect"
	"ergo.services/ergo/gen"
	"ergo.services/ergo/testing/unit"
	"ergo.services/meta/sse"
)

const testNode = gen.Atom("unit@localhost")

var (
	testSSE   = gen.Alias{Node: testNode, ID: [3]uint64{1, 1, 1}, Creation: 1}
	eventOne  = gen.Event{Name: "inspect_process_1", Node: testNode}
	eventTwo  = gen.Event{Name: "inspect_process_2", Node: testNode}
	otherNode = gen.Atom("other@localhost")
)

type answers map[gen.Atom]func(request any) (any, error)

type sessionUnit struct {
	*unit.Subject

	worker *unit.Subject
	cursor int
}

func newSession(t *testing.T, answer func(request any) (any, error)) *sessionUnit {
	t.Helper()
	return newSessionWith(t, sessionSpec{id: "test", sse: testSSE}, answer)
}

func newSessionWith(t *testing.T, spec sessionSpec, answer func(request any) (any, error)) *sessionUnit {
	t.Helper()

	n := unit.StartNode(t, testNode, gen.NodeOptions{})
	n.Network().FailRegistrar(gen.ErrUnsupported)

	return startSession(t, n, spec, answers{testNode: answer})
}

func startSession(t *testing.T, n *unit.MockNode, spec sessionSpec, replies answers) *sessionUnit {
	t.Helper()

	s := n.Prepare(factory_session, gen.ProcessOptions{}, spec)
	if err := s.Run(); err != nil {
		t.Fatalf("session init: %s", err)
	}
	s.Drain()

	w := n.Prepare(factory_sessionWorker, gen.ProcessOptions{},
		sessionWorkerSpec{back: s.PID(), id: spec.id})
	for node, answer := range replies {
		w.OnCall(gen.ProcessID{Name: inspect.Name, Node: node}).RespondWith(answer)
	}
	if err := w.Run(); err != nil {
		t.Fatalf("session worker init: %s", err)
	}
	w.Drain()

	return &sessionUnit{Subject: s, worker: w, cursor: s.Mark()}
}

func (u *sessionUnit) settle(t *testing.T) {
	t.Helper()

	for round := 0; round < 8; round++ {
		mark := u.cursor
		u.cursor = u.Mark()

		moved := 0
		for _, record := range u.ShouldSend().Since(mark).Collect() {
			switch job := record.Message.(type) {
			case jobAction, jobCluster, jobSubscribe, jobSwitch:
				u.worker.SendMessage(u.PID(), job)
				moved++
			}
		}
		moved += u.Drain()

		if moved == 0 {
			return
		}
	}
	t.Fatalf("the session and its worker keep exchanging jobs")
}

func (u *sessionUnit) request(t *testing.T, request any) any {
	t.Helper()

	mark := u.Mark()
	result, err := u.Call(gen.PID{}, request)
	if err != nil {
		t.Fatalf("%T: %s", request, err)
	}
	if result != nil {
		return result
	}

	u.settle(t)

	for _, record := range u.ShouldSendResponse().Since(mark).Collect() {
		if record.Error == nil {
			return record.Message
		}
	}
	t.Fatalf("%T was neither answered nor refused", request)
	return nil
}

func answerWith(event gen.Event) func(any) (any, error) {
	return func(request any) (any, error) {
		switch request.(type) {
		case inspect.RequestInspectProcess:
			return inspect.ResponseInspectProcess{Event: event}, nil
		}
		return nil, gen.ErrUnsupported
	}
}

func state(t *testing.T, sub *sessionUnit) *session {
	t.Helper()

	s, ok := sub.Behavior().(*session)
	if ok == false {
		t.Fatalf("unexpected behavior %T", sub.Behavior())
	}
	return s
}

func consistent(t *testing.T, s *session) {
	t.Helper()

	for handle, eventKey := range s.subIndex {
		sub, exist := s.subscriptions[eventKey]
		if exist == false {
			t.Errorf("handle %q points at %q which is gone", handle, eventKey)
			continue
		}
		if sub.handles[handle] < 1 {
			t.Errorf("handle %q is held %d times by %q", handle, sub.handles[handle], eventKey)
		}
	}
	for eventKey, sub := range s.subscriptions {
		if len(sub.handles) == 0 {
			t.Errorf("%q is monitored with no handles left", eventKey)
		}
		for handle := range sub.handles {
			if s.subIndex[handle] != eventKey {
				t.Errorf("handle %q of %q is missing from the index", handle, eventKey)
			}
		}
	}
}

func subscribeProcess(t *testing.T, sub *sessionUnit, pid gen.PID) string {
	t.Helper()

	result := sub.request(t, commandRequest{
		Command: "subscribe",
		Type:    "process_info",
		Args:    map[string]any{"pid": pid},
	})
	response, ok := result.(apiResponse)
	if ok == false || response.OK == false {
		t.Fatalf("subscribe answered %#v", result)
	}
	handle, ok := response.Data.(wireSubscribed)
	if ok == false || handle.Key == "" {
		t.Fatalf("subscribe returned no handle: %#v", response.Data)
	}
	return handle.Key
}

func unsubscribe(t *testing.T, sub *sessionUnit, handle string) apiResponse {
	t.Helper()

	result := sub.request(t, commandRequest{
		Command: "unsubscribe",
		Args:    map[string]any{"key": handle},
	})
	response, ok := result.(apiResponse)
	if ok == false {
		t.Fatalf("unsubscribe answered %#v", result)
	}
	return response
}

func switchTo(t *testing.T, sub *sessionUnit, node gen.Atom) apiResponse {
	t.Helper()

	result := sub.request(t, commandRequest{
		Command: "switch",
		Args:    map[string]any{"node": string(node)},
	})
	response, ok := result.(apiResponse)
	if ok == false || response.OK == false {
		t.Fatalf("switch to %s answered %#v", node, result)
	}
	return response
}

func pidNumber(id uint64) gen.PID {
	return gen.PID{Node: testNode, ID: id, Creation: 1755000000}
}

func TestSessionSubscribe(t *testing.T) {
	sub := newSession(t, answerWith(eventOne))
	handle := subscribeProcess(t, sub, pidNumber(1))

	sub.ShouldMonitor().Target(eventOne).Once().Assert()

	s := state(t, sub)
	if len(s.subscriptions) != 1 || len(s.subIndex) != 1 {
		t.Fatalf("state after one subscribe: %d events, %d handles", len(s.subscriptions), len(s.subIndex))
	}
	if s.subIndex[handle] != eventOne.String() {
		t.Errorf("handle %q points at %q", handle, s.subIndex[handle])
	}
	consistent(t, s)
}

func TestSessionSubscribeTwice(t *testing.T) {
	sub := newSession(t, answerWith(eventOne))
	first := subscribeProcess(t, sub, pidNumber(1))
	second := subscribeProcess(t, sub, pidNumber(1))

	if first != second {
		t.Errorf("the same scope produced two handles: %q and %q", first, second)
	}
	sub.ShouldMonitor().Target(eventOne).Once().Assert()

	s := state(t, sub)
	if len(s.subscriptions) != 1 || len(s.subIndex) != 1 {
		t.Fatalf("state: %d events, %d handles", len(s.subscriptions), len(s.subIndex))
	}
	consistent(t, s)
}

func TestSessionSharedEventKeepsBothHandles(t *testing.T) {
	sub := newSession(t, answerWith(eventOne))
	first := subscribeProcess(t, sub, pidNumber(1))
	second := subscribeProcess(t, sub, pidNumber(2))

	if first == second {
		t.Fatalf("two scopes produced one handle %q", first)
	}
	sub.ShouldMonitor().Target(eventOne).Once().Assert()

	s := state(t, sub)
	if len(s.subscriptions) != 1 {
		t.Fatalf("expected one watched event, got %d", len(s.subscriptions))
	}
	if len(s.subIndex) != 2 {
		t.Fatalf("expected two handles, got %d", len(s.subIndex))
	}
	consistent(t, s)

	unsubscribe(t, sub, first)
	sub.ShouldDemonitor().Target(eventOne).None().Assert()
	s = state(t, sub)
	if len(s.subscriptions) != 1 || len(s.subIndex) != 1 {
		t.Fatalf("after one unsubscribe: %d events, %d handles", len(s.subscriptions), len(s.subIndex))
	}
	if s.subIndex[second] != eventOne.String() {
		t.Errorf("the surviving handle points at %q", s.subIndex[second])
	}
	consistent(t, s)

	unsubscribe(t, sub, second)
	sub.ShouldDemonitor().Target(eventOne).Once().Assert()
	s = state(t, sub)
	if len(s.subscriptions) != 0 || len(s.subIndex) != 0 {
		t.Fatalf("after the last unsubscribe: %d events, %d handles", len(s.subscriptions), len(s.subIndex))
	}
}

func TestSessionUnsubscribe(t *testing.T) {
	sub := newSession(t, answerWith(eventOne))
	handle := subscribeProcess(t, sub, pidNumber(1))

	unsubscribe(t, sub, handle)

	sub.ShouldDemonitor().Target(eventOne).Once().Assert()
	s := state(t, sub)
	if len(s.subscriptions) != 0 || len(s.subIndex) != 0 {
		t.Fatalf("state: %d events, %d handles", len(s.subscriptions), len(s.subIndex))
	}
}

func TestSessionUnsubscribeWithoutHandle(t *testing.T) {
	sub := newSession(t, answerWith(eventOne))
	subscribeProcess(t, sub, pidNumber(1))

	if response := unsubscribe(t, sub, ""); response.Error == "" {
		t.Errorf("answered %#v", response)
	}

	sub.ShouldDemonitor().None().Assert()
	s := state(t, sub)
	if len(s.subscriptions) != 1 || len(s.subIndex) != 1 {
		t.Fatalf("state: %d events, %d handles", len(s.subscriptions), len(s.subIndex))
	}
}

func TestSessionUnsubscribeUnknownHandle(t *testing.T) {
	sub := newSession(t, answerWith(eventOne))
	subscribeProcess(t, sub, pidNumber(1))

	unsubscribe(t, sub, "process_info:pid=<gone>")

	sub.ShouldDemonitor().None().Assert()
	s := state(t, sub)
	if len(s.subscriptions) != 1 || len(s.subIndex) != 1 {
		t.Fatalf("state: %d events, %d handles", len(s.subscriptions), len(s.subIndex))
	}
	consistent(t, s)

	info, err := sub.Inspect(gen.PID{}, "dropped")
	if err != nil {
		t.Fatalf("inspect: %s", err)
	}
	if strings.Contains(info["dropped"], "unsubscribe_unknown=1") == false {
		t.Errorf("dropped came out as %q", info["dropped"])
	}
}

func TestSessionSubscribeReplacesEvent(t *testing.T) {
	current := eventOne
	sub := newSession(t, func(request any) (any, error) {
		switch request.(type) {
		case inspect.RequestInspectProcess:
			return inspect.ResponseInspectProcess{Event: current}, nil
		}
		return nil, gen.ErrUnsupported
	})

	handle := subscribeProcess(t, sub, pidNumber(1))
	current = eventTwo
	again := subscribeProcess(t, sub, pidNumber(1))

	if handle != again {
		t.Errorf("the handle changed with the event: %q then %q", handle, again)
	}
	sub.ShouldDemonitor().Target(eventOne).Once().Assert()
	sub.ShouldMonitor().Target(eventTwo).Once().Assert()

	s := state(t, sub)
	if len(s.subscriptions) != 1 || len(s.subIndex) != 1 {
		t.Fatalf("state: %d events, %d handles", len(s.subscriptions), len(s.subIndex))
	}
	if s.subIndex[handle] != eventTwo.String() {
		t.Errorf("handle points at %q", s.subIndex[handle])
	}
	consistent(t, s)
}

func TestSessionSubscriptionDown(t *testing.T) {
	sub := newSession(t, answerWith(eventOne))
	first := subscribeProcess(t, sub, pidNumber(1))
	second := subscribeProcess(t, sub, pidNumber(2))

	sub.DeliverDownMessage(gen.MessageDownEvent{Event: eventOne})

	sub.ShouldDemonitor().None().Assert()

	s := state(t, sub)
	if len(s.subscriptions) != 0 || len(s.subIndex) != 0 {
		t.Fatalf("state after down: %d events, %d handles", len(s.subscriptions), len(s.subIndex))
	}

	down := lastSubscriptionDown(t, sub)
	if len(down.Keys) != 2 {
		t.Fatalf("reported %v, expected both handles", down.Keys)
	}
	if down.Type != "process_info" {
		t.Errorf("reported type %q", down.Type)
	}
	if (down.Keys[0] == first || down.Keys[1] == first) == false {
		t.Errorf("%v does not contain %q", down.Keys, first)
	}
	if (down.Keys[0] == second || down.Keys[1] == second) == false {
		t.Errorf("%v does not contain %q", down.Keys, second)
	}
}

func TestSessionSubscriptionDownUnknown(t *testing.T) {
	sub := newSession(t, answerWith(eventOne))
	subscribeProcess(t, sub, pidNumber(1))

	mark := sub.Mark()
	sub.DeliverDownMessage(gen.MessageDownEvent{Event: eventTwo})

	sub.ShouldSend().To(testSSE).Since(mark).None().Assert()
	s := state(t, sub)
	if len(s.subscriptions) != 1 || len(s.subIndex) != 1 {
		t.Fatalf("state: %d events, %d handles", len(s.subscriptions), len(s.subIndex))
	}
	consistent(t, s)
}

func TestSessionSwitchReleasesSubscriptions(t *testing.T) {
	n := unit.StartNode(t, testNode, gen.NodeOptions{})
	n.Network().FailRegistrar(gen.ErrUnsupported)
	n.Network().OnGetNode(otherNode)

	sub := startSession(t, n, sessionSpec{id: "test", sse: testSSE}, answers{
		testNode: answerWith(eventOne),
		otherNode: func(request any) (any, error) {
			return inspect.ResponseInspectNode{Creation: 1755999999}, nil
		},
	})

	subscribeProcess(t, sub, pidNumber(1))

	switchTo(t, sub, otherNode)

	sub.ShouldDemonitor().Target(eventOne).Once().Assert()

	s := state(t, sub)
	if len(s.subscriptions) != 0 || len(s.subIndex) != 0 {
		t.Fatalf("state after switch: %d events, %d handles", len(s.subscriptions), len(s.subIndex))
	}
	if s.node != otherNode {
		t.Errorf("still observing %s", s.node)
	}
}

func TestSessionStaleHandleAfterSwitchKeepsFreshSubscription(t *testing.T) {
	n := unit.StartNode(t, testNode, gen.NodeOptions{})
	n.Network().FailRegistrar(gen.ErrUnsupported)
	n.Network().OnGetNode(otherNode)

	sub := startSession(t, n, sessionSpec{id: "test", sse: testSSE}, answers{
		testNode: answerWith(eventOne),
		otherNode: func(request any) (any, error) {
			switch request.(type) {
			case inspect.RequestInspectNode:
				return inspect.ResponseInspectNode{Creation: 1755999999}, nil
			case inspect.RequestInspectProcess:
				return inspect.ResponseInspectProcess{Event: eventTwo}, nil
			}
			return nil, gen.ErrUnsupported
		},
	})

	stale := subscribeProcess(t, sub, pidNumber(1))

	switchTo(t, sub, otherNode)

	fresh := subscribeProcess(t, sub, pidNumber(1))
	if fresh == stale {
		t.Fatalf("the handle survived the switch: %q", fresh)
	}

	unsubscribe(t, sub, stale)

	s := state(t, sub)
	if s.subIndex[fresh] != eventTwo.String() {
		t.Fatalf("the fresh subscription is gone: %d handles, %d events", len(s.subIndex), len(s.subscriptions))
	}
	sub.ShouldDemonitor().Target(eventTwo).None().Assert()
	consistent(t, s)
}

func TestSessionStaleReleaseAfterResubscribe(t *testing.T) {
	sub := newSession(t, answerWith(eventOne))

	handle := subscribeProcess(t, sub, pidNumber(1))
	again := subscribeProcess(t, sub, pidNumber(1))
	if again != handle {
		t.Fatalf("the same scope on the same node produced %q and %q", handle, again)
	}

	unsubscribe(t, sub, handle)

	s := state(t, sub)
	if s.subIndex[handle] != eventOne.String() {
		t.Fatalf("the subscription is gone: %d handles, %d events", len(s.subIndex), len(s.subscriptions))
	}
	sub.ShouldDemonitor().None().Assert()
	consistent(t, s)

	unsubscribe(t, sub, handle)
	sub.ShouldDemonitor().Target(eventOne).Once().Assert()
	s = state(t, sub)
	if len(s.subIndex) != 0 || len(s.subscriptions) != 0 {
		t.Fatalf("after the last hold: %d handles, %d events", len(s.subIndex), len(s.subscriptions))
	}
}

func TestSessionEventWithoutSubscription(t *testing.T) {
	sub := newSession(t, answerWith(eventOne))

	mark := sub.Mark()
	sub.DeliverEvent(eventTwo, inspect.MessageInspectProcess{Node: testNode})

	sub.ShouldSend().To(testSSE).Since(mark).None().Assert()
}

func TestSessionConnectedCarriesContract(t *testing.T) {
	sub := newSession(t, answerWith(eventOne))

	message, found := firstSSE(t, sub, "connected")
	if found == false {
		t.Fatal("no connected event was sent")
	}

	var intro struct {
		SessionID string `json:"SessionID"`
		Contract  int    `json:"Contract"`
	}
	if err := json.Unmarshal(message.Data, &intro); err != nil {
		t.Fatalf("connected payload: %s", err)
	}
	if intro.Contract != wireContractVersion {
		t.Errorf("contract %d, want %d", intro.Contract, wireContractVersion)
	}
	if intro.SessionID != "test" {
		t.Errorf("session id %q", intro.SessionID)
	}
}

func TestSessionCapabilitiesRelay(t *testing.T) {
	sub := newSession(t, func(request any) (any, error) {
		switch request.(type) {
		case inspect.RequestGetCapabilities:
			return inspect.ResponseGetCapabilities{
				Node:         testNode,
				Creation:     1755000000,
				Manage:       true,
				Capabilities: []string{inspect.CapNode, "manage.kill"},
				Build:        []string{"latency"},
			}, nil
		}
		return nil, gen.ErrUnsupported
	})

	result := sub.request(t, actionRequest{Action: "capabilities"})
	response, ok := result.(apiResponse)
	if ok == false || response.OK == false {
		t.Fatalf("answered %#v", result)
	}
	caps, ok := response.Data.(wireCapabilities)
	if ok == false {
		t.Fatalf("data came back as %T", response.Data)
	}
	if caps.Manage == false || len(caps.Capabilities) != 2 || len(caps.Build) != 1 {
		t.Errorf("mapped to %#v", caps)
	}
}

func TestSessionCapabilitiesOldNode(t *testing.T) {
	sub := newSession(t, func(request any) (any, error) {
		return gen.ErrUnsupported, nil
	})

	result := sub.request(t, actionRequest{Action: "capabilities"})
	response, ok := result.(apiResponse)
	if ok == false {
		t.Fatalf("answered %#v", result)
	}
	if response.Error == "" {
		t.Errorf("an unsupported request answered %#v", response)
	}
}

func TestSessionClusterInfo(t *testing.T) {
	sub := newSession(t, answerWith(eventOne))
	sub.worker.OnCall(gen.ProcessID{Name: clusterName, Node: testNode}).Respond(ClusterInfo{
		Nodes:    []ClusterNodeInfo{{Info: gen.NodeShortInfo{Name: testNode}, Status: statusReachable}},
		Complete: true,
		Watched:  1,
		Limit:    10,
	})

	result := sub.request(t, actionRequest{Action: "cluster_info"})
	response, ok := result.(apiResponse)
	if ok == false || response.OK == false {
		t.Fatalf("cluster_info answered %#v", result)
	}
	wire, ok := response.Data.(wireCluster)
	if ok == false {
		t.Fatalf("data came back as %T", response.Data)
	}
	if len(wire.Nodes) != 1 || wire.Nodes[0].Name != string(testNode) {
		t.Errorf("mapped to %#v", wire)
	}
	if wire.Complete == false || wire.Watched != 1 || wire.Limit != 10 {
		t.Errorf("the map came out as %#v", wire)
	}
}

func TestSessionActionFailureReachesTheCaller(t *testing.T) {
	sub := newSession(t, answerWith(eventOne))
	sub.worker.OnCall(gen.ProcessID{Name: clusterName, Node: testNode}).Fail(gen.ErrProcessUnknown)

	result := sub.request(t, actionRequest{Action: "cluster_info"})
	response, ok := result.(apiResponse)
	if ok == false || response.Error == "" {
		t.Fatalf("a failed action answered %#v", result)
	}
}

func firstSSE(t *testing.T, sub *sessionUnit, event string) (sse.Message, bool) {
	t.Helper()

	for _, record := range sub.ShouldSend().To(testSSE).Collect() {
		message, ok := record.Message.(sse.Message)
		if ok && message.Event == event {
			return message, true
		}
	}
	return sse.Message{}, false
}

func lastSubscriptionDown(t *testing.T, sub *sessionUnit) wireSubscriptionDown {
	t.Helper()

	message, found := firstSSE(t, sub, "subscription_down")
	if found == false {
		t.Fatal("no subscription_down was sent")
	}
	var out wireSubscriptionDown
	if err := json.Unmarshal(message.Data, &out); err != nil {
		t.Fatalf("subscription_down payload: %s", err)
	}
	return out
}

// the limit counts what costs the observed node work: one monitored event per scope
func TestSessionSubscriptionLimit(t *testing.T) {
	spec := sessionSpec{id: "test", sse: testSSE, maxSubscriptions: 1}
	sub := newSessionWith(t, spec, answerWith(eventOne))

	first := subscribeProcess(t, sub, pidNumber(1))

	if again := subscribeProcess(t, sub, pidNumber(1)); again != first {
		t.Errorf("the same scope produced two handles: %q and %q", first, again)
	}

	result := sub.request(t, commandRequest{
		Command: "subscribe",
		Type:    "process_info",
		Args:    map[string]any{"pid": pidNumber(2)},
	})
	response, ok := result.(apiResponse)
	if ok == false || response.Error == "" {
		t.Fatalf("the second scope was accepted: %#v", result)
	}

	s := state(t, sub)
	if len(s.subscriptions) != 1 {
		t.Errorf("subscriptions held: %d", len(s.subscriptions))
	}
	if s.dropped["subscription_limit"] != 1 {
		t.Errorf("refusals counted: %d", s.dropped["subscription_limit"])
	}
	consistent(t, s)
}

// force wakes a sleeping producer on the observed node, which is an effect of writing
func TestSessionForceNeedsMoreThanReadOnly(t *testing.T) {
	answer := func(request any) (any, error) {
		if _, ok := request.(inspect.RequestInspectEventStream); ok {
			return inspect.ResponseInspectEventStream{Event: eventOne}, nil
		}
		return nil, gen.ErrUnsupported
	}

	forced := commandRequest{
		Command: "subscribe",
		Type:    "event_stream",
		Args:    map[string]any{"name": "some_event", "force": true},
	}

	readOnly := newSessionWith(t, sessionSpec{id: "ro", sse: testSSE, ceiling: Ceiling{ReadOnly: true}}, answer)
	result := readOnly.request(t, forced)
	if response, ok := result.(apiResponse); ok == false || response.Error == "" {
		t.Fatalf("a read-only ceiling permitted force: %#v", result)
	}
	readOnly.ShouldMonitor().None().Assert()

	full := newSessionWith(t, sessionSpec{id: "rw", sse: testSSE}, answer)
	result = full.request(t, forced)
	if response, ok := result.(apiResponse); ok == false || response.OK == false {
		t.Fatalf("force was refused without a read-only ceiling: %#v", result)
	}
	full.ShouldMonitor().Target(eventOne).Once().Assert()
}
