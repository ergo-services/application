// Package integration exercises maestro against the real runtime via the stage
// harness, in its own test binary (isolated from the in-process mock harness).
package integration

import (
	"fmt"
	"testing"
	"time"

	"ergo.services/actor/saga"
	"ergo.services/application/maestro"
	"ergo.services/ergo/act"
	"ergo.services/ergo/gen"
	"ergo.services/ergo/testing/check"
	"ergo.services/ergo/testing/stage"
)

//
// shared types (all local to one node, so no EDF registration needed)
//

type startInput struct {
	Collector gen.PID
	Ledger    gen.PID
}

type jobInput struct {
	Key    string
	Ledger gen.PID
}

type doneMsg struct{ Key string }

// ledger applies a keyed side effect at-most-once and counts attempts, so a test
// can prove that a re-driven run does not double-apply.
type applyAndCheck struct{ Key string }
type applyResult struct{ Attempts int }
type getStats struct{ Key string }
type statsResult struct {
	Effect   int // distinct applications (idempotent count)
	Attempts int // total apply calls
}

type ledger struct {
	act.Actor
	effect   map[string]int
	attempts map[string]int
}

func (l *ledger) Init(args ...any) error {
	l.effect = make(map[string]int)
	l.attempts = make(map[string]int)
	return nil
}

func (l *ledger) HandleCall(from gen.PID, ref gen.Ref, request any) (any, error) {
	switch req := request.(type) {
	case applyAndCheck:
		l.attempts[req.Key]++
		if l.effect[req.Key] == 0 {
			l.effect[req.Key] = 1 // apply once (idempotent by key)
		}
		return applyResult{Attempts: l.attempts[req.Key]}, nil
	case getStats:
		return statsResult{Effect: l.effect[req.Key], Attempts: l.attempts[req.Key]}, nil
	}
	return nil, nil
}

func factoryLedger() gen.ProcessBehavior { return &ledger{} }

type collector struct{ act.Actor }

func (c *collector) HandleMessage(from gen.PID, message any) error { return nil }
func factoryCollector() gen.ProcessBehavior                       { return &collector{} }

//
// the user's saga + worker
//

type idemSaga struct {
	saga.Actor
	key       string
	collector gen.PID
}

func (s *idemSaga) Init(args ...any) (saga.Options, error) {
	// maestro passes the RunID as the first init arg: use it as the idempotency key
	s.key = fmt.Sprintf("%v", args[0])
	return saga.Options{Worker: factoryIdemWorker}, nil
}

func (s *idemSaga) HandleTxNew(id saga.TransactionID, value any) error {
	in := value.(startInput)
	s.collector = in.Collector
	_, err := s.StartJob(id, saga.JobOptions{}, jobInput{Key: s.key, Ledger: in.Ledger})
	return err
}

func (s *idemSaga) HandleJobResult(id saga.TransactionID, from saga.JobID, result any) error {
	return s.SendResult(id, result)
}

func (s *idemSaga) HandleJobFailed(id saga.TransactionID, from saga.JobID, reason error) error {
	return reason // propagate: crash the instance so maestro re-drives the run
}

func (s *idemSaga) HandleTxDone(id saga.TransactionID, result any) (any, error) {
	s.Send(s.collector, doneMsg{Key: s.key})
	return result, nil
}

func factoryIdemSaga() gen.ProcessBehavior { return &idemSaga{} }

type idemWorker struct{ saga.Worker }

func (w *idemWorker) HandleJobStart(job saga.Job) error {
	in := job.Value.(jobInput)
	res, err := w.Call(in.Ledger, applyAndCheck{Key: in.Key})
	if err != nil {
		return err
	}
	if res.(applyResult).Attempts == 1 {
		panic("boom: simulated crash after applying the effect, before settling")
	}
	return w.SendResult("ok")
}

func factoryIdemWorker() gen.ProcessBehavior { return &idemWorker{} }

//
// tests
//

func startMaestroNode(t *testing.T) (*stage.Node, gen.PID, gen.PID) {
	t.Helper()
	s := stage.New(t)
	n := s.StartNode("m", stage.NodeOptions{
		EnableSystemApp: true,
		Applications: []gen.ApplicationBehavior{
			maestro.CreateApp(maestro.Options{Saga: factoryIdemSaga}),
		},
	})
	ledgerPID := n.Spawn(factoryLedger, gen.ProcessOptions{})
	collectorPID := n.Spawn(factoryCollector, gen.ProcessOptions{})
	return n, ledgerPID, collectorPID
}

func TestMaestro_CrashRedriveIdempotent(t *testing.T) {
	n, ledgerPID, collectorPID := startMaestroNode(t)

	id, err := maestro.Run(n.Native(), startInput{Collector: collectorPID, Ledger: ledgerPID})
	if err != nil {
		t.Fatal(err)
	}

	// the run completes despite the first attempt crashing (re-driven)
	n.ShouldDeliver().
		To(collectorPID).
		Message(doneMsg{Key: id.String()}).
		Once().
		Within(5 * time.Second).
		Assert()

	// the effect was applied exactly once (idempotent) across two attempts
	res, err := n.Call(ledgerPID, getStats{Key: id.String()})
	if err != nil {
		t.Fatal(err)
	}
	stats := res.(statsResult)
	check.Equal(t, 1, stats.Effect)
	check.Equal(t, 2, stats.Attempts)
}
