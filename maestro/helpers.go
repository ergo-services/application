package maestro

import (
	"errors"
	"fmt"

	"ergo.services/ergo/gen"
)

// Caller is the subset of the runtime used by the helpers to reach the manager.
// It is satisfied by both gen.Process (from inside an actor) and gen.Node (from
// node-level code), so a run can be driven from either.
type Caller interface {
	Call(to any, request any) (any, error)
}

// Run starts a new durable saga run with the given input and returns its RunID.
// The input must be EDF-registered if maestro runs on a different node than the
// caller. Delegates to the maestro manager.
func Run(c Caller, input any) (RunID, error) {
	res, err := c.Call(nameManager, runRequest{Input: input})
	if err != nil {
		return RunID{}, err
	}
	r, ok := res.(runResponse)
	if ok == false {
		return RunID{}, fmt.Errorf("maestro: unexpected response %T", res)
	}
	if r.Error != "" {
		return RunID{}, errors.New(r.Error)
	}
	return RunID(r.ID), nil
}

// Status reports the current lifecycle state of a run. Settled runs are not
// retained in memory and report RunStateUnknown.
func Status(c Caller, id RunID) (RunState, error) {
	res, err := c.Call(nameManager, statusRequest{ID: gen.Ref(id)})
	if err != nil {
		return RunStateUnknown, err
	}
	r, ok := res.(statusResponse)
	if ok == false {
		return RunStateUnknown, fmt.Errorf("maestro: unexpected response %T", res)
	}
	if r.Error != "" {
		return RunStateUnknown, errors.New(r.Error)
	}
	return r.State, nil
}

// Cancel requests cancellation of a run: the saga's HandleTxCancel runs
// (compensation) and the run settles as cancelled. Returns ErrRunUnknown if the
// run is not active.
func Cancel(c Caller, id RunID, reason error) error {
	rs := ""
	if reason != nil {
		rs = reason.Error()
	}
	res, err := c.Call(nameManager, cancelRequest{ID: gen.Ref(id), Reason: rs})
	if err != nil {
		return err
	}
	r, ok := res.(cancelResponse)
	if ok == false {
		return fmt.Errorf("maestro: unexpected response %T", res)
	}
	if r.Error != "" {
		return errors.New(r.Error)
	}
	return nil
}
