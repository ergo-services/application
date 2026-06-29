package maestro

import (
	"errors"

	"ergo.services/actor/saga"
	"ergo.services/ergo/act"
	"ergo.services/ergo/gen"
)

type run struct {
	pid        gen.PID
	input      any
	cancelling bool
}

// manager is the maestro entry point. It journals each run, spawns one saga
// instance per run, learns the outcome via saga.MessageSettled, and re-drives
// incomplete runs (after an instance crash, or after its own restart) from the
// journal.
type manager struct {
	act.Actor

	options Options
	journal Journal
	runs    map[RunID]*run
	byPID   map[gen.PID]RunID
}

func factoryManager() gen.ProcessBehavior { return &manager{} }

func (m *manager) Init(args ...any) error {
	if len(args) == 0 {
		return errors.New("maestro: manager started without options")
	}
	m.options = args[0].(Options)
	if m.options.Saga == nil {
		return ErrNoSaga
	}
	m.journal = m.options.Journal
	if m.journal == nil {
		m.journal = NewMemoryJournal()
	}
	m.runs = make(map[RunID]*run)
	m.byPID = make(map[gen.PID]RunID)

	// re-drive runs left incomplete by a previous incarnation
	records, err := m.journal.Incomplete()
	if err != nil {
		return err
	}
	for _, r := range records {
		if err := m.spawn(r.ID, r.Input); err != nil {
			m.Log().Error("maestro: re-drive of %s failed: %s", r.ID, err)
		}
	}
	return nil
}

func (m *manager) HandleCall(from gen.PID, ref gen.Ref, request any) (any, error) {
	switch req := request.(type) {
	case runRequest:
		id, err := m.start(req.Input)
		if err != nil {
			return runResponse{Error: err.Error()}, nil
		}
		return runResponse{ID: gen.Ref(id)}, nil

	case cancelRequest:
		return m.cancel(RunID(req.ID), req.Reason), nil

	case statusRequest:
		return m.status(RunID(req.ID)), nil
	}
	return nil, nil
}

func (m *manager) HandleMessage(from gen.PID, message any) error {
	switch msg := message.(type) {
	case saga.MessageSettled:
		id, ok := m.byPID[from]
		if ok == false {
			return nil
		}
		// terminal outcome: record it and stop the (idle) instance. Demonitor
		// first so the instance's own termination is not seen as a crash.
		m.journal.Completed(id, msg.Result, msg.Reason)
		m.Demonitor(from)
		m.SendExit(from, gen.TerminateReasonNormal)
		delete(m.byPID, from)
		delete(m.runs, id)

	case gen.MessageDownPID:
		id, ok := m.byPID[msg.PID]
		if ok == false {
			return nil
		}
		delete(m.byPID, msg.PID)
		r, ok := m.runs[id]
		if ok == false {
			return nil
		}
		if r.cancelling {
			// instance died while cancelling: do not re-drive a cancelled run
			m.journal.Completed(id, nil, ErrCancelled)
			delete(m.runs, id)
			return nil
		}
		// reached only when the instance died WITHOUT settling: a crash. The
		// journal still has the intent, so re-drive from the start.
		m.Log().Warning("maestro: run %s crashed (%s); re-driving", id, msg.Reason)
		if err := m.spawn(id, r.input); err != nil {
			m.Log().Error("maestro: re-drive of %s failed: %s", id, err)
		}
	}
	return nil
}

// start records and launches a new run.
func (m *manager) start(input any) (RunID, error) {
	id := RunID(m.Node().MakeRef())
	if err := m.journal.Started(id, input); err != nil {
		return RunID{}, err // fail-fast: no durability record, no run
	}
	if err := m.spawn(id, input); err != nil {
		m.journal.Completed(id, nil, err) // do not leave a dangling intent
		return RunID{}, err
	}
	return id, nil
}

// spawn launches a saga instance for the run and drives it with MessageBegin.
func (m *manager) spawn(id RunID, input any) error {
	pid, err := m.Spawn(m.options.Saga, gen.ProcessOptions{LinkParent: true}, id)
	if err != nil {
		return err
	}
	m.Monitor(pid)
	m.runs[id] = &run{pid: pid, input: input}
	m.byPID[pid] = id
	// the instance notifies us (the MessageBegin sender) on settle
	return m.Send(pid, saga.MessageBegin{Options: m.options.TxOptions, Value: input})
}

func (m *manager) cancel(id RunID, reason string) cancelResponse {
	r, ok := m.runs[id]
	if ok == false {
		return cancelResponse{Error: ErrRunUnknown.Error()}
	}
	e := ErrCancelled
	if reason != "" {
		e = errors.New(reason)
	}
	// ask the instance to cancel its transaction: this runs the saga's
	// HandleTxCancel (compensation) and settles back via MessageSettled, which
	// clears the run. The cancelling flag prevents a re-drive if the instance
	// crashes during compensation.
	r.cancelling = true
	m.Send(r.pid, saga.MessageCancelBegun{Reason: e})
	return cancelResponse{}
}

func (m *manager) status(id RunID) statusResponse {
	r, ok := m.runs[id]
	if ok == false {
		return statusResponse{State: RunStateUnknown}
	}
	if r.cancelling {
		return statusResponse{State: RunStateCancelling}
	}
	return statusResponse{State: RunStateRunning}
}
