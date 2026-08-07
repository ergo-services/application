package grain

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"time"

	"ergo.services/application/grain/store"
	"ergo.services/ergo/gen"
	"ergo.services/ergo/lib"
)

// ActorBehavior is the behavior a durable object implements. Embed grain.Actor
// for defaults and override the hooks you need. The runtime drives ownership,
// the fencing epoch, persistence, idle-deactivation, and reason-aware release
// around these hooks, so they stay pure domain logic and never see the Store,
// the epoch, or the lock.
type ActorBehavior interface {
	gen.ProcessBehavior

	// Init runs once on activation, after the runtime acquired the key and
	// loaded persisted state. state is nil for a grain that was never persisted.
	Init(key string, state []byte) error

	// HandleMessage handles an async message. Return gen.TerminateReasonNormal to
	// deactivate cleanly; any other non-nil error terminates abnormally, and the
	// state is treated as possibly inconsistent (not flushed).
	HandleMessage(from gen.PID, message any) error

	// HandleCall handles a sync request. Return (reply, nil), or (nil, nil) to
	// answer later with SendResponse.
	HandleCall(from gen.PID, ref gen.Ref, request any) (any, error)

	// Snapshot returns the bytes to persist. The runtime calls it on the
	// SyncEvery tick, on idle deactivation, and on orderly shutdown. Keep it pure
	// and fast.
	Snapshot() ([]byte, error)

	// Terminate runs on termination for domain cleanup only; the runtime has
	// already released the lock and, for orderly reasons, flushed a final state.
	Terminate(reason error)

	// HandleInspect answers a gen.Process.Inspect request.
	HandleInspect(from gen.PID, item ...string) map[string]string
}

// grainArgs is the activation payload the activator passes to SpawnRegister.
type grainArgs struct {
	key            string
	state          []byte
	epoch          store.Epoch
	backend        store.Backend
	idleTimeout    time.Duration
	storeIOTimeout time.Duration
	syncEvery      int
}

type messageIdleTick struct{ gen uint64 }

// Actor is the embeddable base for a durable object. It implements
// gen.ProcessBehavior and owns the message loop so it can release the ownership
// lock before the process name is freed and keep control-plane messages out of
// the domain hooks.
type Actor struct {
	gen.Process

	behavior ActorBehavior
	mailbox  gen.ProcessMailbox

	key            string
	epoch          store.Epoch
	backend        store.Backend
	idleTimeout    time.Duration
	storeIOTimeout time.Duration
	syncEvery      int

	idleGen   uint64
	lastSeen  time.Time
	sinceSync int
	released  bool
}

// ProcessKind reports this process as actor-kind.
func (a *Actor) ProcessKind() gen.ProcessKind { return gen.ProcessKindActor }

//
// gen.ProcessBehavior implementation
//

func (a *Actor) ProcessInit(process gen.Process, args ...any) (rr error) {
	b, ok := process.Behavior().(ActorBehavior)
	if ok == false {
		return fmt.Errorf("grain: %s is not a grain.ActorBehavior", process.BehaviorName())
	}

	if lib.Recover() {
		defer func() {
			if r := recover(); r != nil {
				pc, fn, line, _ := runtime.Caller(2)
				a.Log().Panic("grain activation failed. Panic: %#v at %s[%s:%d]",
					r, runtime.FuncForPC(pc).Name(), fn, line)
				rr = gen.TerminateReasonPanic
			}
		}()
	}

	a.Process = process
	a.behavior = b
	a.mailbox = process.Mailbox()

	ga := args[0].(grainArgs)
	a.key = ga.key
	a.epoch = ga.epoch
	a.backend = ga.backend
	a.idleTimeout = ga.idleTimeout
	a.storeIOTimeout = ga.storeIOTimeout
	a.syncEvery = ga.syncEvery

	if err := a.behavior.Init(a.key, ga.state); err != nil { // no Store IO here
		return err
	}
	a.lastSeen = time.Now()
	a.armIdle()
	return nil
}

func (a *Actor) ProcessRun() (rr error) {
	var message *gen.MailboxMessage

	if lib.Recover() {
		defer func() {
			if r := recover(); r != nil {
				pc, fn, line, _ := runtime.Caller(2)
				a.Log().Panic("grain terminated. Panic: %#v at %s[%s:%d]",
					r, runtime.FuncForPC(pc).Name(), fn, line)
				rr = a.stop(gen.TerminateReasonPanic)
			}
		}()
	}

	for {
		if a.State() != gen.ProcessStateRunning {
			return a.stop(gen.TerminateReasonKill)
		}

		if message != nil {
			gen.ReleaseMailboxMessage(message)
			message = nil
		}

		for {
			msg, ok := a.mailbox.Urgent.Pop()
			if ok {
				message = msg.(*gen.MailboxMessage)
				break
			}
			msg, ok = a.mailbox.System.Pop()
			if ok {
				message = msg.(*gen.MailboxMessage)
				break
			}
			msg, ok = a.mailbox.Main.Pop()
			if ok {
				message = msg.(*gen.MailboxMessage)
				break
			}
			if _, ok = a.mailbox.Log.Pop(); ok {
				continue // grains are not loggers; drain and ignore
			}
			return nil // mailbox empty
		}

		switch message.Type {
		case gen.MailboxMessageTypeRegular:
			if tick, ok := message.Message.(messageIdleTick); ok {
				if reason := a.onIdleTick(tick); reason != nil {
					return a.stop(reason)
				}
				continue
			}
			if reason := a.behavior.HandleMessage(message.From, message.Message); reason != nil {
				return a.stop(reason)
			}
			a.lastSeen = time.Now()
			if reason := a.maybeSync(); reason != nil {
				return a.stop(reason)
			}

		case gen.MailboxMessageTypeRequest:
			result, reason := a.behavior.HandleCall(message.From, message.Ref, message.Message)
			if reason != nil {
				if reason == gen.TerminateReasonNormal && result != nil {
					a.SendResponse(message.From, message.Ref, result)
				}
				return a.stop(reason)
			}
			if result != nil {
				a.SendResponse(message.From, message.Ref, result)
			}
			a.lastSeen = time.Now()
			if reason := a.maybeSync(); reason != nil {
				return a.stop(reason)
			}

		case gen.MailboxMessageTypeExit:
			return a.stop(exitReason(message))

		case gen.MailboxMessageTypeInspect:
			result := a.behavior.HandleInspect(message.From, message.Message.([]string)...)
			a.SendResponse(message.From, message.Ref, result)
		}
	}
}

func (a *Actor) ProcessTerminate(reason error) {
	// The lock is normally released inside ProcessRun (before the name is freed).
	// This backstop covers the kill-while-sleeping path that skips ProcessRun.
	if a.released == false {
		a.stop(reason)
	}
	a.behavior.Terminate(reason)
}

//
// durable control plane
//

// stop performs reason-aware ownership release before ProcessRun returns, i.e.
// before the run loop frees the process name. It is idempotent and returns the
// reason unchanged for ProcessRun to propagate.
func (a *Actor) stop(reason error) error {
	if a.released {
		return reason
	}
	a.released = true
	switch {
	case errors.Is(reason, gen.TerminateReasonNormal), errors.Is(reason, gen.TerminateReasonShutdown):
		_ = a.save() // best-effort flush; ErrFenced tolerated
		a.release(store.StatusReleasedClean)
	default: // panic, kill, error, fenced -> state may be inconsistent: no flush
		a.release(store.StatusCrashed)
	}
	return reason
}

func (a *Actor) armIdle() {
	a.idleGen++
	a.SendAfter(a.PID(), messageIdleTick{gen: a.idleGen}, a.idleTimeout)
}

func (a *Actor) onIdleTick(tick messageIdleTick) error {
	if tick.gen != a.idleGen {
		return nil // stale reschedule ghost
	}
	idle := time.Since(a.lastSeen)
	if idle >= a.idleTimeout {
		return gen.TerminateReasonNormal // deactivate
	}
	a.idleGen++
	a.SendAfter(a.PID(), messageIdleTick{gen: a.idleGen}, a.idleTimeout-idle)
	return nil
}

func (a *Actor) maybeSync() error {
	if a.syncEvery <= 0 {
		return nil
	}
	a.sinceSync++
	if a.sinceSync < a.syncEvery {
		return nil
	}
	a.sinceSync = 0
	return a.save()
}

func (a *Actor) save() error {
	b, err := a.behavior.Snapshot()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), a.storeIOTimeout)
	defer cancel()
	return a.backend.SaveState(ctx, a.key, a.epoch, b)
}

func (a *Actor) release(status store.Status) {
	ctx, cancel := context.WithTimeout(context.Background(), a.storeIOTimeout)
	defer cancel()
	_ = a.backend.Release(ctx, a.key, a.epoch, status)
}

// exitReason turns an exit mailbox message into the termination reason, keeping
// the underlying reason wrappable by errors.Is (so a wrapped Shutdown is still
// recognized as Shutdown).
func exitReason(m *gen.MailboxMessage) error {
	switch e := m.Message.(type) {
	case gen.MessageExitPID:
		return fmt.Errorf("%s: %w", e.PID, e.Reason)
	case gen.MessageExitProcessID:
		return fmt.Errorf("%s: %w", e.ProcessID, e.Reason)
	case gen.MessageExitAlias:
		return fmt.Errorf("%s: %w", e.Alias, e.Reason)
	case gen.MessageExitEvent:
		return fmt.Errorf("%s: %w", e.Event, e.Reason)
	case gen.MessageExitNode:
		return fmt.Errorf("%s: %w", e.Name, gen.ErrNoConnection)
	}
	return gen.TerminateReasonKill
}

//
// default ActorBehavior callbacks
//

func (a *Actor) Init(key string, state []byte) error { return nil }

func (a *Actor) HandleMessage(from gen.PID, message any) error {
	a.Log().Warning("grain.Actor.HandleMessage: unhandled message from %s", from)
	return nil
}

func (a *Actor) HandleCall(from gen.PID, ref gen.Ref, request any) (any, error) {
	a.Log().Warning("grain.Actor.HandleCall: unhandled request from %s", from)
	return nil, nil
}

func (a *Actor) Snapshot() ([]byte, error) { return nil, nil }

func (a *Actor) Terminate(reason error) {}

func (a *Actor) HandleInspect(from gen.PID, item ...string) map[string]string { return nil }
