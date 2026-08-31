package observer

import (
	"testing"
	"time"

	"ergo.services/ergo/gen"
	"ergo.services/ergo/testing/unit"
)

var poolPID = gen.PID{Node: testNode, ID: 500, Creation: 1}

func newJob(t *testing.T, steps int) *unit.Subject {
	t.Helper()

	spec := jobSpec{key: "k1", subject: "root"}
	for i := 0; i < steps; i++ {
		spec.steps = append(spec.steps, jobStep{
			ID:   string(rune('a' + i)),
			Node: ownerNode,
			Tool: "capabilities",
			Args: map[string]any{nodeArgument: string(ownerNode)},
		})
	}

	n := unit.StartNode(t, testNode, gen.NodeOptions{})
	n.Network().FailRegistrar(gen.ErrUnsupported)

	sub := n.Prepare(factory_job, gen.ProcessOptions{}, spec)
	sub.OnSpawn(factory_fanoutPool).Return(poolPID)
	if err := sub.Run(); err != nil {
		t.Fatalf("job init: %s", err)
	}
	return sub
}

func jobState(t *testing.T, sub *unit.Subject) *job {
	t.Helper()
	j, ok := sub.Behavior().(*job)
	if ok == false {
		t.Fatalf("unexpected behavior %T", sub.Behavior())
	}
	return j
}

func stepAnswer(id string) messageJobFinished {
	return messageJobFinished{
		Step:   jobStep{ID: id, Node: ownerNode, Tool: "capabilities"},
		Status: "ok",
		Value:  map[string]any{"ok": true},
	}
}

func TestJobInitDoesNoWork(t *testing.T) {
	sub := newJob(t, 2)

	sub.ShouldRegisterEvent().Notify(false).Buffer(1).Once().Assert()
	sub.ShouldSpawn().None().Assert()

	if j := jobState(t, sub); j.state != jobWorking || len(j.ring) != 0 {
		t.Errorf("state=%s ring=%d", j.state, len(j.ring))
	}
}

// without LinkParent a cancelled run would leave its workers behind
func TestJobPoolIsAChild(t *testing.T) {
	sub := newJob(t, 3)
	sub.Drain()

	spawned := sub.ShouldSpawn().Factory(factory_fanoutPool).Collect()
	if len(spawned) != 1 {
		t.Fatalf("pools spawned: %d", len(spawned))
	}
	if spawned[0].Options.LinkParent == false {
		t.Error("the pool would outlive the run")
	}

	sent := sub.ShouldSend().To(poolPID).Collect()
	if len(sent) != 3 {
		t.Errorf("steps handed to the pool: %d", len(sent))
	}
}

func TestJobCompletes(t *testing.T) {
	sub := newJob(t, 2)
	sub.Drain()

	sub.SendMessage(gen.PID{}, stepAnswer("a"))
	if j := jobState(t, sub); j.state != jobWorking || j.answered != 1 {
		t.Fatalf("after one answer: state=%s answered=%d", j.state, j.answered)
	}

	sub.SendMessage(gen.PID{}, stepAnswer("b"))
	j := jobState(t, sub)
	if j.state != jobCompleted || j.answered != 2 || len(j.ring) != 2 {
		t.Fatalf("after both: state=%s answered=%d ring=%d", j.state, j.answered, len(j.ring))
	}

	if published := sub.ShouldSendEvent().Collect(); len(published) != 2 {
		t.Errorf("publications: %d", len(published))
	}
}

func TestJobCancelKeepsWhatItHas(t *testing.T) {
	sub := newJob(t, 3)
	sub.Drain()
	sub.SendMessage(gen.PID{}, stepAnswer("a"))

	sub.SendMessage(gen.PID{}, messageJobCancel{})

	j := jobState(t, sub)
	if j.state != jobCancelled {
		t.Fatalf("state after cancel: %s", j.state)
	}
	if len(j.ring) != 1 || j.answered != 1 {
		t.Errorf("what survived: ring=%d answered=%d", len(j.ring), j.answered)
	}
	if j.pool != (gen.PID{}) {
		t.Error("the pool was left running")
	}
	sub.ShouldSendExit().To(poolPID).Once().Assert()

	reading := j.readingFor("")
	value, ok := reading.Value.(jobReading)
	if ok == false || value.State != jobCancelled || value.Pending != 2 || len(value.Results) != 1 {
		t.Errorf("the reading says %#v", reading.Value)
	}
}

func TestJobIgnoresAnswersAfterTheEnd(t *testing.T) {
	sub := newJob(t, 2)
	sub.Drain()
	sub.SendMessage(gen.PID{}, messageJobCancel{})

	sub.SendMessage(gen.PID{}, stepAnswer("a"))
	sub.SendMessage(gen.PID{}, stepAnswer("b"))

	j := jobState(t, sub)
	if j.state != jobCancelled {
		t.Errorf("late answers changed the state to %s", j.state)
	}
	if j.answered != 0 || len(j.ring) != 0 {
		t.Errorf("late answers were applied: answered=%d ring=%d", j.answered, len(j.ring))
	}
	if j.dropped["answer_after_end"] != 2 {
		t.Errorf("late answers counted: %d", j.dropped["answer_after_end"])
	}
}

func TestJobCancelIsIdempotent(t *testing.T) {
	sub := newJob(t, 2)
	sub.Drain()
	sub.SendMessage(gen.PID{}, messageJobCancel{})
	before := jobState(t, sub).seq

	sub.SendMessage(gen.PID{}, messageJobCancel{})

	if j := jobState(t, sub); j.state != jobCancelled || j.seq != before {
		t.Errorf("a second cancel moved things: state=%s seq=%d, was %d", j.state, j.seq, before)
	}
}

func TestJobCursorAndRetention(t *testing.T) {
	sub := newJob(t, 3)
	sub.Drain()
	sub.SendMessage(gen.PID{}, stepAnswer("a"))

	j := jobState(t, sub)
	first := j.readingFor("")
	whole, _ := first.Value.(jobReading)
	if len(whole.Results) != 1 || whole.RetainSec != int(defaultJobMaxRetention.Seconds()) {
		t.Fatalf("first reading: %#v", first.Value)
	}

	sub.SendMessage(gen.PID{}, stepAnswer("b"))
	next := jobState(t, sub).readingFor(first.NextSeq)
	after, _ := next.Value.(jobReading)
	if len(after.Results) != 1 || after.Results[0].ID != "b" {
		t.Errorf("continuing from the cursor: %#v", next.Value)
	}
	if next.Dropped {
		t.Error("a cursor of this run was reported as dropped")
	}

	j = jobState(t, sub)
	own := j.PID()
	successor := epochOf(gen.PID{Node: own.Node, Creation: own.Creation, ID: own.ID + 1})
	foreign := j.readingFor(successor + ".1")
	all, _ := foreign.Value.(jobReading)
	if foreign.Dropped == false || len(all.Results) != 2 {
		t.Errorf("foreign cursor: dropped=%v results=%d", foreign.Dropped, len(all.Results))
	}
}

func TestJobNameIsPrivate(t *testing.T) {
	if jobName("shared", "alice") == jobName("shared", "bob") {
		t.Error("two callers share one run")
	}
	name := jobName("prof-7", "alice")
	if key := keyOf(ownerPrefixJob, "alice", name); key != "prof-7" {
		t.Errorf("the key came back as %q", key)
	}
	if key := keyOf(ownerPrefixJob, "bob", name); key != "" {
		t.Errorf("another subject read the key as %q", key)
	}
}

func TestRunReadNarrowsToTheCeilingOfTheReader(t *testing.T) {
	held := jobReading{
		jobStatus: jobStatus{State: "completed", Steps: 2, Answered: 2},
		Results: []jobResult{
			{ID: "a@h", Node: "a@h", Tool: "node", Status: "ok", Value: 1},
			{ID: "b@h", Node: "b@h", Tool: "node", Status: "ok", Value: 2},
		},
	}
	held.Refused = map[string]string{"c@h": "node c@h is not connected"}
	reading := ownerReadResponse{URI: jobURI("sweep"), Value: held}

	narrowed, ok := narrowRun(reading, Ceiling{Nodes: []string{"a@h"}}).(ownerReadResponse)
	if ok == false {
		t.Fatalf("narrowing answered %T", narrowRun(reading, Ceiling{}))
	}
	out, _ := narrowed.Value.(jobReading)

	if len(out.Results) != 1 || out.Results[0].Node != "a@h" {
		t.Fatalf("the reader was shown %v", out.Results)
	}
	if out.Refused["b@h"] == "" {
		t.Errorf("the hidden node is not named: %v", out.Refused)
	}
	if out.Refused["c@h"] == "" {
		t.Errorf("what the run itself never covered was dropped: %v", out.Refused)
	}
	if out.Answered != 2 || out.Steps != 2 {
		t.Errorf("the run reads as %d of %d answered", out.Answered, out.Steps)
	}

	if len(held.Refused) != 1 {
		t.Errorf("the run's own map was mutated: %v", held.Refused)
	}

	whole, _ := narrowRun(reading, Ceiling{}).(ownerReadResponse)
	if all, _ := whole.Value.(jobReading); len(all.Results) != 2 {
		t.Errorf("an open ceiling hid %d of 2", 2-len(all.Results))
	}
}

func TestJobGivesUpOnALostStep(t *testing.T) {
	sub := newJob(t, 2)
	sub.Drain()
	sub.SendMessage(gen.PID{}, stepAnswer("a"))

	if j := jobState(t, sub); j.state != jobWorking {
		t.Fatalf("state before the deadline: %s", j.state)
	}

	sub.SendMessage(gen.PID{}, messageJobDeadline{})

	j := jobState(t, sub)
	if j.state != jobFailed {
		t.Fatalf("state after the deadline: %s", j.state)
	}
	if j.dropped["deadline"] != 1 {
		t.Errorf("the deadline was not counted: %v", j.dropped)
	}
	if j.pool != (gen.PID{}) {
		t.Error("the pool was left running")
	}
	sub.ShouldSendExit().To(poolPID).Once().Assert()

	reading, _ := j.readingFor("").Value.(jobReading)
	if reading.State != jobFailed || reading.Pending != 1 || len(reading.Results) != 1 {
		t.Errorf("the reading says %#v", reading)
	}

	if j.generation == 0 {
		t.Error("retirement was not armed, so the run holds its slot for good")
	}
}

func TestJobDeadlineOutlivesItsWaves(t *testing.T) {
	one := jobState(t, newJob(t, 1)).deadline()
	if one != jobMinDeadline {
		t.Errorf("a run of one step waits %s", one)
	}

	big := jobSpec{steps: make([]jobStep, jobStepsMax)}
	waves := (jobStepsMax + jobFanoutWorkers - 1) / jobFanoutWorkers
	want := time.Duration(waves) * jobNodeTimeout * time.Second * 2
	if got := (&job{spec: big}).deadline(); got != want {
		t.Errorf("a run of %d steps waits %s, want %s", jobStepsMax, got, want)
	}
}

func TestJobDeadlineDoesNotDisturbAFinishedRun(t *testing.T) {
	sub := newJob(t, 1)
	sub.Drain()
	sub.SendMessage(gen.PID{}, stepAnswer("a"))
	if j := jobState(t, sub); j.state != jobCompleted {
		t.Fatalf("state after the only answer: %s", j.state)
	}

	sub.SendMessage(gen.PID{}, messageJobDeadline{})

	if j := jobState(t, sub); j.state != jobCompleted || j.dropped["deadline"] != 0 {
		t.Errorf("a late deadline changed a completed run: state=%s dropped=%v", j.state, j.dropped)
	}
}
