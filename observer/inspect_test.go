package observer

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"ergo.services/ergo/app/system/inspect"
	"ergo.services/ergo/gen"
	"ergo.services/ergo/testing/unit"
)

// the default answer does not grow with the subscriptions behind it
func TestSessionInspectSummary(t *testing.T) {
	sub := newSession(t, answerWith(eventOne))
	first := subscribeProcess(t, sub, pidNumber(1))
	subscribeProcess(t, sub, pidNumber(2))

	state, err := sub.Inspect(gen.PID{})
	if err != nil {
		t.Fatalf("inspect: %s", err)
	}

	if state["id"] != "test" || state["observing_node"] != string(testNode) {
		t.Errorf("identity came out as %v", state)
	}
	if state["contract"] != fmt.Sprintf("%d", wireContractVersion) {
		t.Errorf("contract reported as %q", state["contract"])
	}
	if state["subscriptions"] != "1" || state["handles"] != "2" {
		t.Errorf("bookkeeping reported as %q events / %q handles", state["subscriptions"], state["handles"])
	}
	if state["dropped"] != "none" {
		t.Errorf("nothing was dropped, yet %q", state["dropped"])
	}
	if state["items"] != "help" {
		t.Error("the summary must say that a vocabulary exists")
	}
	if state["uptime"] == "never" || state["last_message"] == "never" {
		t.Errorf("ages came out as %q / %q", state["uptime"], state["last_message"])
	}
	if state["last_change"] == "never" {
		t.Error("two subscriptions were made, so the last change is known")
	}

	for key, value := range state {
		if strings.Contains(value, first) {
			t.Errorf("%q carries a handle, so the answer grows with the state", key)
		}
	}
}

func TestSessionInspectQueries(t *testing.T) {
	sub := newSession(t, answerWith(eventOne))
	first := subscribeProcess(t, sub, pidNumber(1))
	second := subscribeProcess(t, sub, pidNumber(2))

	state, err := sub.Inspect(gen.PID{}, "help", "handles", "events",
		"handle "+first, "event "+eventOne.String(), "nonsense")
	if err != nil {
		t.Fatalf("inspect: %s", err)
	}

	if strings.Contains(state["help"], "handle <handle>") == false {
		t.Errorf("help does not name the queries: %q", state["help"])
	}
	if strings.Contains(state["handles"], first) == false || strings.Contains(state["handles"], second) == false {
		t.Errorf("handles came out as %q", state["handles"])
	}
	if state["handle "+first] != eventOne.String() {
		t.Errorf("the handle resolves to %q", state["handle "+first])
	}
	if strings.Contains(state["event "+eventOne.String()], "handles=2") == false {
		t.Errorf("the shared event came out as %q", state["event "+eventOne.String()])
	}
	if strings.Contains(state["events"], eventOne.String()+"=2") == false {
		t.Errorf("events came out as %q", state["events"])
	}
	if state["nonsense"] != "<unknown item>" {
		t.Errorf("an unknown item came out as %q", state["nonsense"])
	}

	state, _ = sub.Inspect(gen.PID{}, "handle gone", "event gone")
	if state["handle gone"] != "<not found>" || state["event gone"] != "<not found>" {
		t.Errorf("a missing entry came out as %q / %q", state["handle gone"], state["event gone"])
	}
}

func TestSessionInspectDropped(t *testing.T) {
	sub := newSession(t, answerWith(eventOne))
	subscribeProcess(t, sub, pidNumber(1))

	sub.DeliverEvent(eventTwo, inspect.MessageInspectProcess{Node: testNode})
	sub.DeliverDownMessage(gen.MessageDownEvent{Event: eventTwo})
	unsubscribe(t, sub, "process_info:pid=<gone>")
	sub.SendMessage(gen.PID{}, "nonsense")
	if _, err := sub.Call(gen.PID{}, commandRequest{Command: "bogus"}); err != nil {
		t.Fatalf("call: %s", err)
	}
	if _, err := sub.Call(gen.PID{}, actionRequest{Action: "bogus"}); err != nil {
		t.Fatalf("call: %s", err)
	}

	state, err := sub.Inspect(gen.PID{}, "dropped")
	if err != nil {
		t.Fatalf("inspect: %s", err)
	}

	for _, expected := range []string{
		"event_unknown=1", "down_unknown=1", "unsubscribe_unknown=1",
		"message_unexpected=1", "command_refused=1", "action_refused=1",
	} {
		if strings.Contains(state["dropped"], expected) == false {
			t.Errorf("%s is missing from %q", expected, state["dropped"])
		}
	}

	summary, _ := sub.Inspect(gen.PID{})
	if summary["dropped"] != state["dropped"] {
		t.Errorf("the summary reports %q, the query %q", summary["dropped"], state["dropped"])
	}
}

func newWatcher(t *testing.T, store *clusterStore, keep time.Duration, event gen.Event) *unit.Subject {
	t.Helper()

	n := unit.StartNode(t, testNode, gen.NodeOptions{})
	args := watcherArgs{node: otherNode, store: store, period: 50 * time.Millisecond, keep: keep}

	sub := n.Prepare(factory_watcher, gen.ProcessOptions{}, args)
	sub.OnCall(gen.ProcessID{Name: inspect.Name, Node: otherNode}).RespondWith(func(request any) (any, error) {
		switch request.(type) {
		case inspect.RequestInspectNodeShort:
			return inspect.ResponseInspectNodeShort{
				Event: event,
				Info: gen.NodeShortInfo{
					Name:  otherNode,
					Peers: []gen.RemoteNodeShortInfo{{Node: testNode}},
				},
			}, nil
		}
		return nil, gen.ErrUnsupported
	})
	if err := sub.Run(); err != nil {
		t.Fatalf("watcher init: %s", err)
	}
	sub.SendMessage(sub.PID(), messageRun{})
	return sub
}

func TestWatcherInspectState(t *testing.T) {
	store := newClusterStore()
	sub := newWatcher(t, store, time.Minute, eventOne)

	state, err := sub.Inspect(gen.PID{})
	if err != nil {
		t.Fatalf("inspect: %s", err)
	}
	if state["state"] != "watching" || state["reading"] != "live" {
		t.Errorf("a subscribed watcher reports %q / %q", state["state"], state["reading"])
	}
	if state["node"] != string(otherNode) || state["event"] != eventOne.String() {
		t.Errorf("identity came out as %v", state)
	}
	if state["retry_in"] != "not armed" {
		t.Errorf("nothing is scheduled, yet retry_in is %q", state["retry_in"])
	}
	if state["reason"] != "none" {
		t.Errorf("reason came out as %q", state["reason"])
	}

	sub.DeliverDownMessage(gen.MessageDownNode{Name: otherNode})

	state, _ = sub.Inspect(gen.PID{})
	if state["state"] != "retrying" {
		t.Errorf("after the node went the state is %q", state["state"])
	}
	if state["reading"] != "stale" {
		t.Errorf("the last reading must stay, yet reading is %q", state["reading"])
	}
	if state["retry_in"] == "not armed" {
		t.Error("a retry was scheduled and must be visible")
	}
	if state["reason"] == "none" || state["reason_age"] == "never" {
		t.Errorf("the reason came out as %q (%q)", state["reason"], state["reason_age"])
	}

	state, _ = sub.Inspect(gen.PID{}, "peers", "help", "nonsense")
	if strings.Contains(state["peers"], string(testNode)) == false {
		t.Errorf("peers are counted, not named: %q", state["peers"])
	}
	if strings.Contains(state["help"], "keep_reading") == false {
		t.Errorf("help does not name the summary keys: %q", state["help"])
	}
	if state["nonsense"] != "<unknown item>" {
		t.Errorf("an unknown item came out as %q", state["nonsense"])
	}
}

func TestWatcherExpiresReading(t *testing.T) {
	store := newClusterStore()
	sub := newWatcher(t, store, time.Millisecond, eventOne)

	if _, found := store.snapshot(otherNode); found == false {
		t.Fatal("no reading while the node is reachable")
	}

	sub.DeliverDownMessage(gen.MessageDownNode{Name: otherNode})
	time.Sleep(5 * time.Millisecond)
	sub.SendMessage(sub.PID(), messageSilence{})

	if _, found := store.snapshot(otherNode); found {
		t.Error("the reading outlived its window")
	}
	state, _ := sub.Inspect(gen.PID{})
	if state["reading"] != "none" {
		t.Errorf("reading came out as %q", state["reading"])
	}
}
