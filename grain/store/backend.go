// Package store defines the ownership-and-persistence backend a grain
// application uses to guarantee that a key is owned by at most one node at a
// time and to persist a grain's state.
//
// The backend is the linearizable authority for ownership. It keeps two kinds
// of record. A per-key ownership stamp names the owner node and carries a
// fencing epoch: a write to a key succeeds only while the caller holds the
// key's current epoch, so a superseded owner can never write again. A per-node
// lease is refreshed by a heartbeat; a key becomes reclaimable once its owner
// node's lease expires. One lease covers all of a node's keys, so liveness
// costs one heartbeat per node rather than one per key.
//
// A backend without compare-and-set semantics cannot implement Backend correctly.
package store

import (
	"context"
	"errors"

	"ergo.services/ergo/gen"
)

var (
	// ErrAlreadyOwned is returned by Acquire when the key is held by a live node.
	// The returned stamp names that owner.
	ErrAlreadyOwned = errors.New("grain: key already owned")

	// ErrFenced is returned by a fenced write (SaveState, Release, Delete) when
	// the presented epoch is no longer current: ownership was lost to another
	// node. The caller must stop writing and must not retry.
	ErrFenced = errors.New("grain: fenced")

	// ErrNoStamp is returned when a key has no ownership stamp.
	ErrNoStamp = errors.New("grain: no ownership stamp")

	// ErrNoState is returned when a key has no persisted state.
	ErrNoState = errors.New("grain: no persisted state")

	// ErrNoLease is returned when a node has no lease.
	ErrNoLease = errors.New("grain: no lease")
)

// Epoch is a per-key fencing token. It changes only on an ownership handoff
// (Acquire); a fenced write must present the epoch its owner was granted.
type Epoch uint64

// Incarnation identifies one boot of a node. The backend mints it strictly
// increasing per node in AcquireLease, independent of any clock, so a stamp
// left by a previous boot is always distinguishable from the current one.
type Incarnation int64

// Status is the lifecycle state of an ownership stamp.
type Status uint8

const (
	// StatusRunning marks a live, owned key.
	StatusRunning Status = iota

	// StatusReleasedClean marks a key released by an orderly deactivation; its
	// persisted state is valid.
	StatusReleasedClean

	// StatusReleasedDeleted marks a deleted grain.
	StatusReleasedDeleted

	// StatusCrashed marks a key released after a panic or error; its persisted
	// state may be stale.
	StatusCrashed
)

// Stamp is the per-key ownership record.
type Stamp struct {
	Key         string
	OwnerNode   gen.Atom
	Incarnation Incarnation
	Status      Status
	Epoch       Epoch
}

// Lease is a node's heartbeat record. A key owned by the node is reclaimable
// once now-LastHeartbeat reaches TTL.
type Lease struct {
	Node          gen.Atom
	Incarnation   Incarnation
	LastHeartbeat int64 // unix seconds
	TTL           int64 // seconds
}

// Backend is the ownership-and-persistence authority for a grain application.
// Every method honors the context deadline; callers always bound it.
type Backend interface {
	// AcquireLease establishes this node's lease once per boot and returns it.
	// The returned Incarnation is strictly greater than any the node held
	// before. ttl is the lease lifetime in seconds.
	AcquireLease(ctx context.Context, node gen.Atom, ttl int64) (Lease, error)

	// Renew refreshes the node's lease heartbeat and returns the current lease,
	// or ErrNoLease if the node has none.
	Renew(ctx context.Context, node gen.Atom) (Lease, error)

	// ReadLease returns a node's lease, or ErrNoLease.
	ReadLease(ctx context.Context, node gen.Atom) (Lease, error)

	// DropLease removes this node's lease on orderly shutdown so survivors can
	// reclaim its keys without waiting for the lease to expire.
	DropLease(ctx context.Context, node gen.Atom) error

	// Acquire grants ownership of key to node at incarnation inc and returns the
	// stamp with a fresh epoch. It succeeds if the key is absent or its current
	// stamp is dead (see IsDead), preserving any persisted state; otherwise it
	// returns ErrAlreadyOwned together with the live stamp.
	Acquire(ctx context.Context, key string, node gen.Atom, inc Incarnation) (Stamp, error)

	// Read returns a key's stamp, or ErrNoStamp.
	Read(ctx context.Context, key string) (Stamp, error)

	// SaveState persists state for key, fenced by epoch. It returns ErrFenced if
	// the epoch is no longer current, or ErrNoStamp if the key is unowned.
	SaveState(ctx context.Context, key string, epoch Epoch, state []byte) error

	// LoadState returns the last state persisted for key, or ErrNoState.
	LoadState(ctx context.Context, key string) ([]byte, error)

	// Release marks the key released with status, fenced by epoch. Persisted
	// state is kept so a later Acquire can reload it.
	Release(ctx context.Context, key string, epoch Epoch, status Status) error

	// Delete removes a key's stamp and its persisted state, fenced by epoch.
	Delete(ctx context.Context, key string, epoch Epoch) error
}

// IsDead reports whether an ownership stamp can be reclaimed. lease is the owner
// node's lease and haveLease reports whether it exists; now is unix seconds. The
// decision is conservative: on a missing or stale view it favors the current
// owner and refuses to reclaim.
func IsDead(s Stamp, lease Lease, haveLease bool, now int64) bool {
	switch s.Status {
	case StatusReleasedClean, StatusReleasedDeleted, StatusCrashed:
		return true
	}
	if haveLease == false {
		// The owner node has no lease at all: it is gone.
		return true
	}
	if lease.Incarnation > s.Incarnation {
		// The owner node rebooted; the stamp is from a dead incarnation.
		return true
	}
	if lease.Incarnation < s.Incarnation {
		// Our view of the lease predates the stamp: treat as live.
		return false
	}
	return now-lease.LastHeartbeat >= lease.TTL
}
