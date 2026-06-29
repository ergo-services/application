package maestro

import (
	"errors"
	"fmt"

	"ergo.services/ergo/gen"
	"ergo.services/ergo/net/edf"
)

// RunID identifies a maestro run (a single durable saga transaction).
type RunID gen.Ref

// String implements fmt.Stringer.
func (id RunID) String() string {
	r := gen.Ref(id)
	return fmt.Sprintf("Run#%d.%d.%d", r.ID[0], r.ID[1], r.ID[2])
}

// RunState reports a run's lifecycle state as known to the manager. Settled
// (completed or cancelled) runs are not retained in memory and report
// RunStateUnknown.
type RunState int

const (
	RunStateUnknown    RunState = iota // not tracked: never started, or already settled
	RunStateRunning                    // active
	RunStateCancelling                 // cancellation requested; compensation in progress
)

// String implements fmt.Stringer.
func (s RunState) String() string {
	switch s {
	case RunStateRunning:
		return "running"
	case RunStateCancelling:
		return "cancelling"
	default:
		return "unknown"
	}
}

// Errors.
var (
	ErrRunUnknown = errors.New("maestro: unknown run")
	ErrCancelled  = errors.New("maestro: run cancelled")
	ErrNoSaga     = errors.New("maestro: no saga factory configured")
)

// wire types for the manager's request/response API (registered in edf so the
// manager can be reached with Call from another node).

type runRequest struct {
	Input any
}

type runResponse struct {
	ID    gen.Ref
	Error string
}

type cancelRequest struct {
	ID     gen.Ref
	Reason string
}

type cancelResponse struct {
	Error string
}

type statusRequest struct {
	ID gen.Ref
}

type statusResponse struct {
	State RunState
	Error string
}

func init() {
	types := []any{
		RunState(0), // nested in statusResponse; register before it
		runRequest{}, runResponse{},
		cancelRequest{}, cancelResponse{},
		statusRequest{}, statusResponse{},
	}
	for _, t := range types {
		if err := edf.RegisterTypeOf(t); err != nil && err != gen.ErrTaken {
			panic(err)
		}
	}
}
