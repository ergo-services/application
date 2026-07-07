package grain

import (
	"errors"

	"ergo.services/ergo/gen"
)

// activateRequest asks an activator to ensure a grain is live.
type activateRequest struct{ key string }

// activateResponse carries the live grain PID.
type activateResponse struct{ pid gen.PID }

// deleteRequest asks an activator to deactivate and delete a grain.
type deleteRequest struct{ key string }

// deleteResponse acknowledges a delete.
type deleteResponse struct{}

var (
	// ErrNotRunning is returned when no grain runtime is running for the domain.
	ErrNotRunning = errors.New("grain: runtime is not running for this domain")
	// ErrNoIncarnation is returned by activation before the node lease is acquired.
	ErrNoIncarnation = errors.New("grain: node incarnation not yet published")
	// ErrActivateExhausted is returned when activation retries are exhausted.
	ErrActivateExhausted = errors.New("grain: activation retries exhausted")
)

// Activate ensures the grain for key in domain is live and returns its PID. It
// takes a lock-free fast path when the grain is already live on this node,
// otherwise it routes to the owning activator shard.
func Activate(process gen.Process, domain gen.Atom, key string) (gen.PID, error) {
	if domain == "" {
		domain = DefaultDomain
	}
	node := process.Node().Name()
	if pid, ok := whereisAt(node, domain, key); ok {
		return pid, nil
	}
	d, ok := catalogFor(node, domain)
	if ok == false {
		return gen.PID{}, ErrNotRunning
	}
	to := activatorName(domain, activatorIndexFor(node, domain, key))
	v, err := process.CallWithTimeout(to, activateRequest{key: key}, d.opts.ActivateSecs)
	if err != nil {
		return gen.PID{}, err
	}
	return v.(activateResponse).pid, nil
}

// Whereis returns the live grain PID for key, or false. It is a lock-free read
// and never activates.
func Whereis(process gen.Process, domain gen.Atom, key string) (gen.PID, bool) {
	if domain == "" {
		domain = DefaultDomain
	}
	return whereisAt(process.Node().Name(), domain, key)
}

// Delete deactivates the grain for key (if live) and removes its stamp and
// persisted state.
func Delete(process gen.Process, domain gen.Atom, key string) error {
	if domain == "" {
		domain = DefaultDomain
	}
	node := process.Node().Name()
	d, ok := catalogFor(node, domain)
	if ok == false {
		return ErrNotRunning
	}
	to := activatorName(domain, activatorIndexFor(node, domain, key))
	_, err := process.CallWithTimeout(to, deleteRequest{key: key}, d.opts.ActivateSecs)
	return err
}
