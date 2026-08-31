package observer

import (
	"strings"
	"testing"

	"ergo.services/ergo/app/system/inspect"
	"ergo.services/ergo/gen"
	"ergo.services/ergo/testing/unit"
)

var ownerNode = gen.Atom("watched@localhost")

func newOwner(t *testing.T, raw string, answer func(any) (any, error)) (*unit.Subject, mcpURI) {
	t.Helper()

	uri, err := parseURI(raw)
	if err != nil {
		t.Fatalf("uri %s: %s", raw, err)
	}

	n := unit.StartNode(t, testNode, gen.NodeOptions{})
	n.Network().FailRegistrar(gen.ErrUnsupported)

	sub := n.Prepare(factory_uriOwner, gen.ProcessOptions{}, ownerSpec{uri: uri})
	sub.OnCall(gen.ProcessID{Name: inspect.Name, Node: ownerNode}).RespondWith(answer)
	if err := sub.Run(); err != nil {
		t.Fatalf("owner init: %s", err)
	}
	return sub, uri
}

func answerLog(event gen.Event) func(any) (any, error) {
	return func(request any) (any, error) {
		if _, ok := request.(inspect.RequestInspectLog); ok {
			return inspect.ResponseInspectLog{Event: event}, nil
		}
		return nil, gen.ErrUnsupported
	}
}

func ownerState(t *testing.T, sub *unit.Subject) *uriOwner {
	t.Helper()
	o, ok := sub.Behavior().(*uriOwner)
	if ok == false {
		t.Fatalf("unexpected behavior %T", sub.Behavior())
	}
	return o
}

func logSource() gen.Event {
	return gen.Event{Name: "inspect_log_1", Node: ownerNode}
}

// network is a snapshot lens: one value, no cursor
func answerNetwork(event gen.Event) func(any) (any, error) {
	return func(request any) (any, error) {
		if _, ok := request.(inspect.RequestInspectNetwork); ok {
			return inspect.ResponseInspectNetwork{Event: event}, nil
		}
		return nil, gen.ErrUnsupported
	}
}

func TestOwnerInitDoesNoWork(t *testing.T) {
	sub, uri := newOwner(t, "ergo://watched@localhost/log", answerLog(logSource()))

	sub.ShouldCall().None().Assert()
	sub.ShouldMonitor().None().Assert()
	sub.ShouldRegisterEvent().Notify(true).Buffer(1).Once().Assert()

	o := ownerState(t, sub)
	if o.monitored {
		t.Error("the owner took a subscription before anybody asked")
	}
	if o.event != uri.ownerName("")+"_updates" {
		t.Errorf("the event is named %q", o.event)
	}
}

func TestOwnerFollowsDemand(t *testing.T) {
	source := logSource()
	sub, _ := newOwner(t, "ergo://watched@localhost/log", answerLog(source))

	sub.SendMessage(gen.PID{}, gen.MessageEventStart{})
	sub.ShouldMonitor().Target(source).Once().Assert()
	if o := ownerState(t, sub); o.monitored == false || o.subscribed == false {
		t.Fatal("demand did not start the work")
	}

	sub.SendMessage(gen.PID{}, gen.MessageEventStop{})
	sub.ShouldDemonitor().Target(source).Once().Assert()
	if o := ownerState(t, sub); o.monitored || o.subscribed {
		t.Error("the work outlived the demand")
	}
}

func TestOwnerPublishesTheName(t *testing.T) {
	source := logSource()
	sub, uri := newOwner(t, "ergo://watched@localhost/log", answerLog(source))
	sub.SendMessage(gen.PID{}, gen.MessageEventStart{})

	sub.DeliverEvent(source, inspect.MessageInspectLog{Node: ownerNode})

	published := sub.ShouldSendEvent().Collect()
	if len(published) != 1 {
		t.Fatalf("events published: %d", len(published))
	}
	update, ok := published[0].Message.(messageOwnerUpdated)
	if ok == false {
		t.Fatalf("published %T", published[0].Message)
	}
	if update.URI != uri.Canonical() || update.Seq != 1 {
		t.Errorf("published %#v", update)
	}

	if o := ownerState(t, sub); o.value == nil || o.seq != 1 {
		t.Errorf("the owner holds seq=%d value=%v", o.seq, o.value)
	}
}

// the buffer of a lens stays empty until somebody subscribes
func TestOwnerDefersAColdRead(t *testing.T) {
	source := gen.Event{Name: "inspect_network_1", Node: ownerNode}
	sub, _ := newOwner(t, "ergo://watched@localhost/network", answerNetwork(source))

	reader := gen.PID{Node: testNode, ID: 42, Creation: 1}
	result, err := sub.Call(reader, ownerReadRequest{})
	if err != nil {
		t.Fatalf("read: %s", err)
	}
	if result != nil {
		t.Fatalf("a cold read answered at once: %#v", result)
	}

	o := ownerState(t, sub)
	if len(o.pending) != 1 {
		t.Fatalf("pending readers: %d", len(o.pending))
	}
	if o.monitored == false {
		t.Error("a pending read did not raise demand")
	}

	sub.DeliverEvent(source, inspect.MessageInspectNetwork{Node: ownerNode})

	answers := sub.ShouldSendResponse().To(reader).Collect()
	if len(answers) != 1 {
		t.Fatalf("responses sent: %d", len(answers))
	}
	reading, ok := answers[0].Message.(ownerReadResponse)
	if ok == false || reading.Seq != 1 || reading.Value == nil {
		t.Fatalf("answered %#v", answers[0].Message)
	}

	o = ownerState(t, sub)
	if o.monitored == false {
		t.Error("a read stopped the watch, so the next one would answer with stale data")
	}

	sub.DeliverEvent(source, inspect.MessageInspectNetwork{Node: ownerNode})
	if again := ownerState(t, sub); again.seq != 2 {
		t.Errorf("a later publication did not arrive: seq=%d", again.seq)
	}
}

func TestOwnerAccumulatesWithCursor(t *testing.T) {
	source := logSource()
	sub, _ := newOwner(t, "ergo://watched@localhost/log", answerLog(source))
	sub.SendMessage(gen.PID{}, gen.MessageEventStart{})

	sub.DeliverEvent(source, inspect.MessageInspectLog{Node: ownerNode, Suppressed: 1})
	sub.DeliverEvent(source, inspect.MessageInspectLog{Node: ownerNode, Suppressed: 2})

	o := ownerState(t, sub)
	if o.accumulating == false || len(o.ring) != 2 {
		t.Fatalf("the ring holds %d batches, accumulating=%v", len(o.ring), o.accumulating)
	}

	reader := gen.PID{Node: testNode, ID: 43, Creation: 1}
	whole, err := sub.Call(reader, ownerReadRequest{})
	if err != nil {
		t.Fatal(err)
	}
	first, ok := whole.(ownerReadResponse)
	if ok == false || len(first.Batches) != 2 || first.Value != nil {
		t.Fatalf("a read without a cursor answered %#v", whole)
	}
	if first.NextSeq == "" || first.Dropped {
		t.Errorf("nextSeq=%q dropped=%v", first.NextSeq, first.Dropped)
	}

	next, err := sub.Call(reader, ownerReadRequest{Since: first.NextSeq})
	if err != nil {
		t.Fatal(err)
	}
	second, _ := next.(ownerReadResponse)
	if len(second.Batches) != 0 || second.Dropped {
		t.Errorf("continuing answered %d batches, dropped=%v", len(second.Batches), second.Dropped)
	}

	sub.DeliverEvent(source, inspect.MessageInspectLog{Node: ownerNode, Suppressed: 3})
	after, _ := sub.Call(reader, ownerReadRequest{Since: first.NextSeq})
	third, _ := after.(ownerReadResponse)
	if len(third.Batches) != 1 {
		t.Errorf("after one more publication: %d batches", len(third.Batches))
	}

	// a cursor of another incarnation cannot be honoured, so everything held is returned
	own := ownerState(t, sub).PID()
	successor := epochOf(gen.PID{Node: own.Node, Creation: own.Creation, ID: own.ID + 1})
	foreign, _ := sub.Call(reader, ownerReadRequest{Since: successor + ".1"})
	fourth, _ := foreign.(ownerReadResponse)
	if fourth.Dropped == false || len(fourth.Batches) != 3 {
		t.Errorf("a foreign cursor answered %d batches, dropped=%v", len(fourth.Batches), fourth.Dropped)
	}
}

// an accumulating lens gathers only while it is watched, so resting between two polls would
// lose the lines in between
func TestOwnerKeepsWatchingBetweenPolls(t *testing.T) {
	source := logSource()
	sub, _ := newOwner(t, "ergo://watched@localhost/log", answerLog(source))

	reader := gen.PID{Node: testNode, ID: 44, Creation: 1}
	if _, err := sub.Call(reader, ownerReadRequest{}); err != nil {
		t.Fatal(err)
	}
	sub.DeliverEvent(source, inspect.MessageInspectLog{Node: ownerNode})

	if ownerState(t, sub).monitored == false {
		t.Fatal("the owner stopped watching right after answering a poll")
	}
	sub.ShouldDemonitor().None().Assert()

	before := ownerState(t, sub).generation
	if _, err := sub.Call(reader, ownerReadRequest{}); err != nil {
		t.Fatal(err)
	}
	if ownerState(t, sub).generation == before {
		t.Error("a poll did not re-arm the idle timer")
	}
}

func TestOwnerFailsAReadItCannotServe(t *testing.T) {
	sub, _ := newOwner(t, "ergo://watched@localhost/log", func(any) (any, error) {
		return nil, gen.ErrTimeout
	})

	reader := gen.PID{Node: testNode, ID: 42, Creation: 1}
	if _, err := sub.Call(reader, ownerReadRequest{}); err != nil {
		t.Fatalf("read: %s", err)
	}

	answers := sub.ShouldSendResponse().To(reader).Collect()
	if len(answers) != 1 {
		t.Fatalf("responses sent: %d", len(answers))
	}
	reading, ok := answers[0].Message.(ownerReadResponse)
	if ok == false || reading.Error == "" {
		t.Fatalf("answered %#v", answers[0].Message)
	}
}

func TestOwnerLeavesWhenIdle(t *testing.T) {
	sub, _ := newOwner(t, "ergo://watched@localhost/log", answerLog(logSource()))

	o := ownerState(t, sub)
	if o.generation != 1 {
		t.Fatalf("shutdown was armed %d times", o.generation)
	}
	if o.idle != ownerIdlePeriod {
		t.Errorf("idle period is %s", o.idle)
	}

	// a timer of an earlier generation is stale and must not fire
	sub.SendMessage(gen.PID{}, messageOwnerShutdown{generation: 0})
	if sub.Terminated() {
		t.Fatal("a stale shutdown killed the owner")
	}

	sub.SendMessage(gen.PID{}, messageOwnerShutdown{generation: o.generation})
	if sub.Terminated() == false {
		t.Error("the owner stayed with no demand")
	}
}

// outliving a stream is what a key is for
func TestOwnerKeyedWaitsLonger(t *testing.T) {
	sub, _ := newOwner(t, "ergo://watched@localhost/watch/mine/log", answerLog(logSource()))
	if o := ownerState(t, sub); o.idle != ownerKeyedIdle {
		t.Errorf("a keyed owner waits %s", o.idle)
	}
}

func TestLensArgs(t *testing.T) {
	uri, err := parseURI("ergo://watched@localhost/log?limit=10&force=true&namePattern=abc")
	if err != nil {
		t.Fatal(err)
	}
	args, err := lensArgs(uri)
	if err != nil {
		t.Fatalf("args: %s", err)
	}
	if limit, ok := args["limit"].(float64); ok == false || limit != 10 {
		t.Errorf("limit came out as %#v", args["limit"])
	}
	if force, ok := args["force"].(bool); ok == false || force == false {
		t.Errorf("force came out as %#v", args["force"])
	}
	if pattern, ok := args["namePattern"].(string); ok == false || pattern != "abc" {
		t.Errorf("namePattern came out as %#v", args["namePattern"])
	}

	bad, err := parseURI("ergo://watched@localhost/log?limit=many")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lensArgs(bad); err == nil {
		t.Error("limit=many was accepted")
	}
}

func TestLensTarget(t *testing.T) {
	uri, err := parseURI("ergo://watched@localhost/process/1234.1755000000")
	if err != nil {
		t.Fatal(err)
	}
	args, err := lensArgs(uri)
	if err != nil {
		t.Fatalf("args: %s", err)
	}
	pid, ok := args["pid"].(gen.PID)
	if ok == false {
		t.Fatalf("pid came out as %#v", args["pid"])
	}
	if pid.Node != ownerNode || pid.ID != 1234 || pid.Creation != 1755000000 {
		t.Errorf("pid parsed as %s", pid)
	}

	meta, err := parseURI("ergo://watched@localhost/meta/1.2.3.1755000000")
	if err != nil {
		t.Fatal(err)
	}
	margs, err := lensArgs(meta)
	if err != nil {
		t.Fatalf("meta args: %s", err)
	}
	alias, ok := margs["alias"].(gen.Alias)
	if ok == false || alias.ID != [3]uint64{1, 2, 3} || alias.Creation != 1755000000 {
		t.Errorf("alias came out as %#v", margs["alias"])
	}

	stray, err := parseURI("ergo://watched@localhost/network/whatever")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lensArgs(stray); err == nil {
		t.Error("a target was accepted by a lens that has none")
	}
}

func TestOwnerGoesWithItsSource(t *testing.T) {
	source := logSource()
	sub, _ := newOwner(t, "ergo://watched@localhost/log", answerLog(source))

	reader := gen.PID{Node: testNode, ID: 51, Creation: 1}
	if _, err := sub.Call(reader, ownerReadRequest{}); err != nil {
		t.Fatal(err)
	}
	sub.DeliverEvent(source, inspect.MessageInspectLog{Node: ownerNode, Suppressed: 1})
	if ownerState(t, sub).readable() == false {
		t.Fatal("the owner has nothing to hold, so the test proves nothing")
	}

	sub.SendMessage(gen.PID{}, gen.MessageDownEvent{Event: source})
	if sub.Terminated() == false {
		t.Error("the owner outlived the source it answers from")
	}
}

func TestOwnerFailsAPendingReadWhenTheSourceDies(t *testing.T) {
	source := logSource()
	sub, _ := newOwner(t, "ergo://watched@localhost/network", answerNetwork(source))

	reader := gen.PID{Node: testNode, ID: 52, Creation: 1}
	if _, err := sub.Call(reader, ownerReadRequest{}); err != nil {
		t.Fatal(err)
	}
	if len(ownerState(t, sub).pending) != 1 {
		t.Fatal("the read did not wait for the source")
	}

	sub.SendMessage(gen.PID{}, gen.MessageDownEvent{Event: source})

	answers := sub.ShouldSendResponse().To(reader).Collect()
	if len(answers) != 1 {
		t.Fatalf("responses sent: %d", len(answers))
	}
	reading, _ := answers[0].Message.(ownerReadResponse)
	if strings.Contains(reading.Error, "stopped publishing") == false {
		t.Errorf("the reader was told %q", reading.Error)
	}
}
