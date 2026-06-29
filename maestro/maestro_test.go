package maestro

import (
	"errors"
	"testing"

	"ergo.services/actor/saga"
	"ergo.services/ergo/gen"
	"ergo.services/ergo/testing/check"
	"ergo.services/ergo/testing/unit"
)

// dummySaga stands in for the user's saga.Actor. The unit mock allocates a PID
// for it without running it, which is all the manager tests need.
type dummySaga struct {
	saga.Actor
}

func factoryDummySaga() gen.ProcessBehavior { return &dummySaga{} }

// failJournal fails every Started call.
type failJournal struct{}

func (failJournal) Started(RunID, any) error            { return errors.New("disk full") }
func (failJournal) Completed(RunID, any, error) error   { return nil }
func (failJournal) Incomplete() ([]RunRecord, error)    { return nil, nil }

func client() gen.PID { return gen.PID{Node: "test@localhost", ID: 1, Creation: 1} }

func spawnManager(t *testing.T, j Journal) *unit.Subject {
	t.Helper()
	s, err := unit.Spawn(t, factoryManager, gen.ProcessOptions{},
		Options{Saga: factoryDummySaga, Journal: j})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestManager_RunHappy(t *testing.T) {
	j := NewMemoryJournal()
	m := spawnManager(t, j)

	res, err := m.Call(client(), runRequest{Input: "order-1"})
	if err != nil {
		t.Fatal(err)
	}
	resp := res.(runResponse)
	check.Equal(t, "", resp.Error)

	// an instance was spawned and driven with MessageBegin
	spawn, ok := m.ShouldSpawn().Once().Capture()
	if ok == false {
		t.Fatal("expected the manager to spawn a saga instance")
	}
	m.ShouldSend().
		To(spawn.Child).
		Where(func(r check.Send) bool {
			b, ok := r.Message.(saga.MessageBegin)
			return ok && b.Value.(string) == "order-1"
		}).
		Once().
		Assert()

	// the run is journaled as incomplete
	recs, _ := j.Incomplete()
	check.Equal(t, 1, len(recs))

	// instance settles -> journal cleared, instance stopped
	mark := m.Mark()
	m.SendMessage(spawn.Child, saga.MessageSettled{Result: "done"})
	recs, _ = j.Incomplete()
	check.Equal(t, 0, len(recs))
	m.ShouldSendExit().To(spawn.Child).Since(mark).Once().Assert()
}

func TestManager_CrashRedrive(t *testing.T) {
	j := NewMemoryJournal()
	m := spawnManager(t, j)

	if _, err := m.Call(client(), runRequest{Input: "x"}); err != nil {
		t.Fatal(err)
	}
	spawn1, _ := m.ShouldSpawn().Once().Capture()

	// instance crashes without settling -> re-drive
	mark := m.Mark()
	m.DeliverDown(spawn1.Child, gen.TerminateReasonPanic)

	// a fresh instance is spawned and driven again
	m.ShouldSpawn().Since(mark).Once().Assert()
	// still exactly one incomplete run (re-driven, not duplicated/dropped)
	recs, _ := j.Incomplete()
	check.Equal(t, 1, len(recs))
}

func TestManager_SettledThenDown(t *testing.T) {
	j := NewMemoryJournal()
	m := spawnManager(t, j)

	if _, err := m.Call(client(), runRequest{Input: "x"}); err != nil {
		t.Fatal(err)
	}
	spawn, _ := m.ShouldSpawn().Once().Capture()

	// settle first, then the (expected) termination arrives
	m.SendMessage(spawn.Child, saga.MessageSettled{Result: "done"})
	mark := m.Mark()
	m.DeliverDown(spawn.Child, gen.TerminateReasonNormal)

	// must NOT re-drive a settled run
	m.ShouldSpawn().Since(mark).None().Assert()
	recs, _ := j.Incomplete()
	check.Equal(t, 0, len(recs))
}

func TestManager_RestartRedrive(t *testing.T) {
	// a journal carrying an incomplete run from a previous incarnation
	j := NewMemoryJournal()
	j.Started(RunID(gen.Ref{Node: "test@localhost", ID: [3]uint64{1, 2, 3}}), "recovered")

	// the manager re-drives it on start
	m := spawnManager(t, j)

	spawn, ok := m.ShouldSpawn().Once().Capture()
	if ok == false {
		t.Fatal("expected the manager to re-drive the incomplete run on start")
	}
	m.ShouldSend().
		To(spawn.Child).
		Where(func(r check.Send) bool {
			b, ok := r.Message.(saga.MessageBegin)
			return ok && b.Value.(string) == "recovered"
		}).
		Once().
		Assert()
}

func TestManager_Cancel(t *testing.T) {
	j := NewMemoryJournal()
	m := spawnManager(t, j)

	res, err := m.Call(client(), runRequest{Input: "x"})
	if err != nil {
		t.Fatal(err)
	}
	id := res.(runResponse).ID
	spawn, _ := m.ShouldSpawn().Once().Capture()

	mark := m.Mark()
	cres, err := m.Call(client(), cancelRequest{ID: id, Reason: "user requested"})
	if err != nil {
		t.Fatal(err)
	}
	check.Equal(t, "", cres.(cancelResponse).Error)

	// the instance is asked to cancel (so its HandleTxCancel / compensation runs)
	m.ShouldSend().
		To(spawn.Child).
		Since(mark).
		Where(func(r check.Send) bool { _, ok := r.Message.(saga.MessageCancelBegun); return ok }).
		Once().
		Assert()

	// once the instance settles (cancelled), the run is cleared
	m.SendMessage(spawn.Child, saga.MessageSettled{Reason: ErrCancelled})
	recs, _ := j.Incomplete()
	check.Equal(t, 0, len(recs))
}

func TestManager_CancelCrashNoRedrive(t *testing.T) {
	j := NewMemoryJournal()
	m := spawnManager(t, j)

	res, err := m.Call(client(), runRequest{Input: "x"})
	if err != nil {
		t.Fatal(err)
	}
	id := res.(runResponse).ID
	spawn, _ := m.ShouldSpawn().Once().Capture()

	if _, err := m.Call(client(), cancelRequest{ID: id}); err != nil {
		t.Fatal(err)
	}

	// the instance crashes mid-compensation: a cancelling run must NOT be re-driven
	mark := m.Mark()
	m.DeliverDown(spawn.Child, gen.TerminateReasonPanic)
	m.ShouldSpawn().Since(mark).None().Assert()
	recs, _ := j.Incomplete()
	check.Equal(t, 0, len(recs))
}

func TestManager_Status(t *testing.T) {
	j := NewMemoryJournal()
	m := spawnManager(t, j)

	res, err := m.Call(client(), runRequest{Input: "x"})
	if err != nil {
		t.Fatal(err)
	}
	id := res.(runResponse).ID
	spawn, _ := m.ShouldSpawn().Once().Capture()

	st, err := m.Call(client(), statusRequest{ID: id})
	if err != nil {
		t.Fatal(err)
	}
	check.Equal(t, RunStateRunning, st.(statusResponse).State)

	// after settling, the run is no longer tracked
	m.SendMessage(spawn.Child, saga.MessageSettled{Result: "done"})
	st, _ = m.Call(client(), statusRequest{ID: id})
	check.Equal(t, RunStateUnknown, st.(statusResponse).State)
}

func TestManager_CancelUnknown(t *testing.T) {
	m := spawnManager(t, NewMemoryJournal())
	res, err := m.Call(client(), cancelRequest{ID: gen.Ref{Node: "test@localhost", ID: [3]uint64{9, 9, 9}}})
	if err != nil {
		t.Fatal(err)
	}
	check.Equal(t, ErrRunUnknown.Error(), res.(cancelResponse).Error)
}

func TestManager_JournalErrorFailFast(t *testing.T) {
	m := spawnManager(t, failJournal{})

	res, err := m.Call(client(), runRequest{Input: "x"})
	if err != nil {
		t.Fatal(err)
	}
	// Run reports the journal error and spawns nothing
	check.Contains(t, res.(runResponse).Error, "disk full")
	m.ShouldSpawn().None().Assert()
}
