package observer

import (
	"encoding/json"
	"strings"
	"testing"

	"ergo.services/ergo/gen"
	"ergo.services/ergo/testing/unit"
	"ergo.services/meta/sse"
)

var streamSSE = gen.Alias{Node: testNode, ID: [3]uint64{2, 2, 2}, Creation: 1}

const streamRequestID = float64(7)

func newStream(t *testing.T, raw ...string) *unit.Subject {
	t.Helper()
	return newStreamRefusing(t, nil, raw...)
}

func newStreamRefusing(t *testing.T, refused map[string]string, raw ...string) *unit.Subject {
	t.Helper()

	spec := streamSpec{sse: streamSSE, id: streamRequestID, subject: "root", refused: refused}
	for _, one := range raw {
		uri, err := parseURI(one)
		if err != nil {
			t.Fatalf("uri %s: %s", one, err)
		}
		spec.uris = append(spec.uris, uri)
	}

	n := unit.StartNode(t, testNode, gen.NodeOptions{})
	n.Network().FailRegistrar(gen.ErrUnsupported)

	for _, uri := range spec.uris {
		n.Network().OnGetNode(uri.Node)
	}

	sub := n.Prepare(factory_mcpStream, gen.ProcessOptions{}, spec)

	for _, uri := range spec.uris {
		sub.OnCall(uri.ownerName(spec.subject)).Respond(ownerReadResponse{URI: uri.Canonical()})
	}
	return sub
}

func newStreamOnAnUnknownNode(t *testing.T, raw string) *unit.Subject {
	t.Helper()

	uri, err := parseURI(raw)
	if err != nil {
		t.Fatalf("uri %s: %s", raw, err)
	}

	n := unit.StartNode(t, testNode, gen.NodeOptions{})
	n.Network().FailRegistrar(gen.ErrUnsupported)
	return n.Prepare(factory_mcpStream, gen.ProcessOptions{},
		streamSpec{sse: streamSSE, id: streamRequestID, subject: "root", uris: []mcpURI{uri}})
}

func ownerEventOf(t *testing.T, raw string, subject string) gen.Event {
	t.Helper()
	uri, err := parseURI(raw)
	if err != nil {
		t.Fatal(err)
	}
	return gen.Event{Name: ownerEventName(uri, subject), Node: testNode}
}

func frames(t *testing.T, sub *unit.Subject) []map[string]any {
	t.Helper()

	out := []map[string]any{}
	for _, record := range sub.ShouldSend().To(streamSSE).Collect() {
		message, ok := record.Message.(sse.Message)
		if ok == false || record.Error != nil {
			continue
		}
		var frame map[string]any
		if err := json.Unmarshal(message.Data, &frame); err != nil {
			t.Fatalf("frame %q: %s", message.Data, err)
		}
		out = append(out, frame)
	}
	return out
}

func methodOf(frame map[string]any) string {
	method, _ := frame["method"].(string)
	return method
}

func paramsOf(t *testing.T, frame map[string]any) map[string]any {
	t.Helper()
	params, ok := frame["params"].(map[string]any)
	if ok == false {
		t.Fatalf("frame carries no params: %v", frame)
	}
	return params
}

func metaOf(t *testing.T, frame map[string]any) map[string]any {
	t.Helper()

	holder := frame
	if params, nested := frame["params"].(map[string]any); nested {
		holder = params
	} else if result, answered := frame["result"].(map[string]any); answered {
		holder = result
	}

	meta, ok := holder["_meta"].(map[string]any)
	if ok == false {
		t.Fatalf("frame carries no _meta: %v", frame)
	}
	return meta
}

func followedBy(t *testing.T, frame map[string]any) []any {
	t.Helper()

	notifications, ok := paramsOf(t, frame)["notifications"].(map[string]any)
	if ok == false {
		t.Fatalf("the acknowledgement carries no filter: %v", frame)
	}
	followed, _ := notifications["resourceSubscriptions"].([]any)
	return followed
}

func TestStreamAcknowledges(t *testing.T) {
	sub := newStream(t, "ergo://watched@localhost/log", "ergo://watched@localhost/network")
	if err := sub.Run(); err != nil {
		t.Fatalf("stream init: %s", err)
	}
	sub.Drain()

	sent := frames(t, sub)
	if len(sent) != 1 {
		t.Fatalf("frames sent: %d", len(sent))
	}
	if methodOf(sent[0]) != mcpSubscriptionsAck {
		t.Errorf("the first frame is %q", methodOf(sent[0]))
	}
	if got := metaOf(t, sent[0])[metaSubscriptionID]; got != streamRequestID {
		t.Errorf("the acknowledgement names subscription %#v", got)
	}
	if followed := followedBy(t, sent[0]); len(followed) != 2 {
		t.Errorf("following %d of 2: %v", len(followed), followed)
	}
}

func TestStreamAcknowledgesNoListChanges(t *testing.T) {
	sub := newStream(t, "ergo://watched@localhost/log")
	if err := sub.Run(); err != nil {
		t.Fatalf("stream init: %s", err)
	}
	sub.Drain()

	notifications, _ := paramsOf(t, frames(t, sub)[0])["notifications"].(map[string]any)
	for _, kind := range []string{"toolsListChanged", "promptsListChanged", "resourcesListChanged"} {
		if claimed, present := notifications[kind]; present {
			t.Errorf("the acknowledgement claims %s: %v", kind, claimed)
		}
	}
}

func TestStreamNamesWhatItRefused(t *testing.T) {
	sub := newStream(t, "ergo://watched@localhost/log", "ergo://watched@localhost/network")
	sub.OnMonitorEvent(ownerEventOf(t, "ergo://watched@localhost/network", "root")).
		Fail(gen.ErrEventUnknown)
	if err := sub.Run(); err != nil {
		t.Fatalf("stream init: %s", err)
	}
	sub.Drain()

	ack := frames(t, sub)[0]
	if followed := followedBy(t, ack); len(followed) != 1 {
		t.Errorf("following %d of 1: %v", len(followed), followed)
	}

	refused, _ := metaOf(t, ack)[metaRefused].(map[string]any)
	if len(refused) != 1 {
		t.Fatalf("refused %d of 1: %v", len(refused), refused)
	}
	if _, named := refused["ergo://watched@localhost/network"]; named == false {
		t.Errorf("the refused one is not named: %v", refused)
	}
}

func TestStreamCarriesTheGateRefusals(t *testing.T) {
	sub := newStreamRefusing(t,
		map[string]string{"ergo://forbidden@localhost/log": "node is not permitted here"},
		"ergo://watched@localhost/log")
	if err := sub.Run(); err != nil {
		t.Fatalf("stream init: %s", err)
	}
	sub.Drain()

	refused, _ := metaOf(t, frames(t, sub)[0])[metaRefused].(map[string]any)
	if refused["ergo://forbidden@localhost/log"] != "node is not permitted here" {
		t.Errorf("the gate's refusal did not survive: %v", refused)
	}
}

func TestStreamRefusesAnUnreachableNode(t *testing.T) {
	sub := newStreamOnAnUnknownNode(t, "ergo://gone@localhost/log")
	if err := sub.Run(); err != nil {
		t.Fatalf("stream init: %s", err)
	}
	sub.Drain()

	ack := frames(t, sub)[0]
	if followed := followedBy(t, ack); len(followed) != 0 {
		t.Errorf("following an unreachable node: %v", followed)
	}
	refused, _ := metaOf(t, ack)[metaRefused].(map[string]any)
	if reason, named := refused["ergo://gone@localhost/log"]; named == false {
		t.Errorf("the unreachable node is not accounted for: %v", refused)
	} else if strings.Contains(reason.(string), "not connected") == false {
		t.Errorf("the reason reads %v", reason)
	}

	sub.ShouldCall().None().Assert()
}

func TestStreamFollowsAWarmResource(t *testing.T) {
	sub := newStream(t, "ergo://watched@localhost/log")
	sub.OnMonitorEvent(ownerEventOf(t, "ergo://watched@localhost/log", "root")).
		Return([]gen.MessageEvent{{Message: messageOwnerUpdated{
			URI: "ergo://watched@localhost/log",
			Seq: 7,
		}}})
	if err := sub.Run(); err != nil {
		t.Fatalf("stream init: %s", err)
	}
	sub.Drain()

	sub.FireTimers()

	sent := frames(t, sub)
	if len(sent) != 2 {
		t.Fatalf("frames sent: %d", len(sent))
	}
	if methodOf(sent[1]) != mcpResourceUpdated {
		t.Fatalf("the second frame is %q", methodOf(sent[1]))
	}
	if params := paramsOf(t, sent[1]); params["uri"] != "ergo://watched@localhost/log" {
		t.Errorf("the frame names %v", params["uri"])
	}
}

func TestStreamNamesTheSubscriptionOnEveryFrame(t *testing.T) {
	sub := newStream(t, "ergo://watched@localhost/log")
	if err := sub.Run(); err != nil {
		t.Fatalf("stream init: %s", err)
	}
	sub.Drain()

	event := ownerEventOf(t, "ergo://watched@localhost/log", "root")
	sub.DeliverEvent(event, messageOwnerUpdated{URI: "ergo://watched@localhost/log", Seq: 1})
	sub.FireTimers()
	sub.SendMessage(gen.PID{}, gen.MessageDownEvent{Event: event})

	sent := frames(t, sub)
	if len(sent) < 3 {
		t.Fatalf("only %d frames to check", len(sent))
	}
	for i, frame := range sent {
		if got := metaOf(t, frame)[metaSubscriptionID]; got != streamRequestID {
			t.Errorf("frame %d (%s) names subscription %#v", i, methodOf(frame), got)
		}
	}
}

func TestStreamCoalesces(t *testing.T) {
	sub := newStream(t, "ergo://watched@localhost/log")
	if err := sub.Run(); err != nil {
		t.Fatalf("stream init: %s", err)
	}
	sub.Drain()

	event := ownerEventOf(t, "ergo://watched@localhost/log", "root")
	for seq := 1; seq <= 5; seq++ {
		sub.DeliverEvent(event, messageOwnerUpdated{URI: "ergo://watched@localhost/log", Seq: int64(seq)})
	}
	sub.FireTimers()

	updates := 0
	for _, frame := range frames(t, sub) {
		if methodOf(frame) == mcpResourceUpdated {
			updates++
		}
	}
	if updates != 1 {
		t.Errorf("five changes became %d frames", updates)
	}
}

func TestStreamSourceGone(t *testing.T) {
	sub := newStream(t, "ergo://watched@localhost/log", "ergo://watched@localhost/network")
	if err := sub.Run(); err != nil {
		t.Fatalf("stream init: %s", err)
	}
	sub.Drain()

	event := ownerEventOf(t, "ergo://watched@localhost/log", "root")
	sub.SendMessage(gen.PID{}, gen.MessageDownEvent{Event: event})

	s, ok := sub.Behavior().(*mcpStream)
	if ok == false {
		t.Fatalf("unexpected behavior %T", sub.Behavior())
	}
	if len(s.watched) != 1 {
		t.Errorf("still following %d", len(s.watched))
	}

	sent := frames(t, sub)
	last := sent[len(sent)-1]
	if methodOf(last) != mcpSubscriptionsAck {
		t.Fatalf("the last frame is %q", methodOf(last))
	}

	followed := followedBy(t, last)
	if len(followed) != 1 || followed[0] != "ergo://watched@localhost/network" {
		t.Errorf("the acknowledged set is %v", followed)
	}
	refused, _ := metaOf(t, last)[metaRefused].(map[string]any)
	if refused["ergo://watched@localhost/log"] == nil {
		t.Errorf("the lost uri is not named: %v", refused)
	}
	for _, frame := range sent {
		if methodOf(frame) == mcpResourceUpdated {
			t.Error("a lost subscription was reported as a change of the resource")
		}
	}
}

func TestStreamClosesWhenEmpty(t *testing.T) {
	sub := newStream(t, "ergo://watched@localhost/log")
	sub.OnMonitorEvent(ownerEventOf(t, "ergo://watched@localhost/log", "root")).
		Fail(gen.ErrEventUnknown)
	if err := sub.Run(); err != nil {
		t.Fatalf("stream init: %s", err)
	}
	sub.Drain()

	sent := frames(t, sub)
	if len(sent) != 3 {
		t.Fatalf("frames sent: %d", len(sent))
	}

	if methodOf(sent[1]) != mcpCancelled {
		t.Errorf("the closing frame is %q", methodOf(sent[1]))
	}
	if got := paramsOf(t, sent[1])["requestId"]; got != streamRequestID {
		t.Errorf("the cancellation names request %#v", got)
	}

	if methodOf(sent[2]) != "" {
		t.Errorf("the last frame is a notification %q", methodOf(sent[2]))
	}
	if sent[2]["id"] != streamRequestID {
		t.Errorf("the answer carries id %#v", sent[2]["id"])
	}
	result, _ := sent[2]["result"].(map[string]any)
	if result["resultType"] != mcpResultComplete {
		t.Errorf("the answer is %v", result)
	}

	sub.ShouldSendExitMeta().Meta(streamSSE).Once().Assert()
}

func TestStreamSaysFarewellOnce(t *testing.T) {
	sub := newStream(t, "ergo://watched@localhost/log")
	sub.OnMonitorEvent(ownerEventOf(t, "ergo://watched@localhost/log", "root")).
		Fail(gen.ErrEventUnknown)
	if err := sub.Run(); err != nil {
		t.Fatalf("stream init: %s", err)
	}
	sub.Drain()

	sub.DeliverExit(gen.PID{}, gen.TerminateReasonShutdown)
	if sub.Terminated() == false {
		t.Fatal("the stream survived a shutdown exit")
	}

	cancelled := 0
	for _, frame := range frames(t, sub) {
		if methodOf(frame) == mcpCancelled {
			cancelled++
		}
	}
	if cancelled != 1 {
		t.Errorf("the subscription was cancelled %d times", cancelled)
	}
}

func updatesIn(t *testing.T, sub *unit.Subject) int {
	t.Helper()

	updates := 0
	for _, frame := range frames(t, sub) {
		if methodOf(frame) == mcpResourceUpdated {
			updates++
		}
	}
	return updates
}

func TestStreamRefusesWhatItsOwnerCannotServe(t *testing.T) {
	sub := newStream(t, "ergo://watched@localhost/log", "ergo://watched@localhost/network")

	broken, err := parseURI("ergo://watched@localhost/network")
	if err != nil {
		t.Fatal(err)
	}
	sub.OnCall(broken.ownerName("root")).Respond(ownerReadResponse{
		URI:   broken.Canonical(),
		Error: "the node does not publish this",
	})

	if err := sub.Run(); err != nil {
		t.Fatalf("stream init: %s", err)
	}
	sub.Drain()

	ack := frames(t, sub)[0]
	followed := followedBy(t, ack)
	if len(followed) != 1 || followed[0] != "ergo://watched@localhost/log" {
		t.Errorf("acknowledged %v", followed)
	}

	refused, _ := metaOf(t, ack)[metaRefused].(map[string]any)
	if reason := refused["ergo://watched@localhost/network"]; reason != "the node does not publish this" {
		t.Errorf("the owner's reason did not reach the caller: %v", refused)
	}
}

func TestStreamRepeatsAFrameThatDidNotLand(t *testing.T) {
	full := false
	sub := newStream(t, "ergo://watched@localhost/log")
	sub.OnSend(streamSSE).FailFunc(func() error {
		if full {
			return gen.ErrProcessMailboxFull
		}
		return nil
	})

	if err := sub.Run(); err != nil {
		t.Fatalf("stream init: %s", err)
	}
	sub.Drain()

	full = true
	sub.DeliverEvent(ownerEventOf(t, "ergo://watched@localhost/log", "root"),
		messageOwnerUpdated{URI: "ergo://watched@localhost/log", Seq: 1})
	sub.FireTimers()
	if updates := updatesIn(t, sub); updates != 0 {
		t.Fatalf("%d frames landed while the mailbox was full", updates)
	}

	full = false
	sub.FireTimers()
	if updates := updatesIn(t, sub); updates != 1 {
		t.Errorf("the frame nobody received was repeated %d times", updates)
	}
}
