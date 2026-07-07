// Package mem is the in-memory reference implementation of the grain store
// contract. It is single-node only and depends on nothing outside the standard
// library, so it keeps the grain module free of external dependencies. Use it
// as the test double and for single-node deployments; a clustered deployment
// needs a linearizable backend (see the store/pg module).
package mem

import (
	"context"
	"sync"
	"time"

	"ergo.services/application/grain/store"
	"ergo.services/ergo/gen"
)

var _ store.Backend = (*Store)(nil)

// Store is the in-memory grain store. Its mutex is held only for the duration
// of a call to the backend object, never across an actor's message handling, so
// it does not block an actor state machine.
type Store struct {
	mu sync.Mutex

	stamps map[string]store.Stamp
	states map[string][]byte
	leases map[gen.Atom]store.Lease

	// minted is the highest incarnation ever granted per node. It outlives a
	// lease (DropLease does not clear it) so a rebooting node always mints a
	// strictly greater incarnation.
	minted map[gen.Atom]store.Incarnation

	now func() int64
}

// Option configures a Store.
type Option func(*Store)

// WithClock overrides the time source (unix seconds). Tests use it to drive
// lease expiry deterministically.
func WithClock(now func() int64) Option {
	return func(s *Store) { s.now = now }
}

// New returns an empty in-memory store.
func New(opts ...Option) *Store {
	s := &Store{
		stamps: make(map[string]store.Stamp),
		states: make(map[string][]byte),
		leases: make(map[gen.Atom]store.Lease),
		minted: make(map[gen.Atom]store.Incarnation),
		now:    func() int64 { return time.Now().Unix() },
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *Store) AcquireLease(_ context.Context, node gen.Atom, ttl int64) (store.Lease, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	inc := s.minted[node] + 1
	s.minted[node] = inc
	l := store.Lease{Node: node, Incarnation: inc, LastHeartbeat: s.now(), TTL: ttl}
	s.leases[node] = l
	return l, nil
}

func (s *Store) Renew(_ context.Context, node gen.Atom) (store.Lease, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	l, ok := s.leases[node]
	if ok == false {
		return store.Lease{}, store.ErrNoLease
	}
	l.LastHeartbeat = s.now()
	s.leases[node] = l
	return l, nil
}

func (s *Store) ReadLease(_ context.Context, node gen.Atom) (store.Lease, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	l, ok := s.leases[node]
	if ok == false {
		return store.Lease{}, store.ErrNoLease
	}
	return l, nil
}

func (s *Store) DropLease(_ context.Context, node gen.Atom) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.leases, node)
	return nil
}

func (s *Store) Acquire(_ context.Context, key string, node gen.Atom, inc store.Incarnation) (store.Stamp, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cur, exists := s.stamps[key]
	if exists {
		lease, haveLease := s.leases[cur.OwnerNode]
		if store.IsDead(cur, lease, haveLease, s.now()) == false {
			return cur, store.ErrAlreadyOwned
		}
	}

	st := store.Stamp{
		Key:         key,
		OwnerNode:   node,
		Incarnation: inc,
		Status:      store.StatusRunning,
		Epoch:       cur.Epoch + 1, // cur is the zero Stamp (Epoch 0) when absent
	}
	s.stamps[key] = st
	return st, nil
}

func (s *Store) Read(_ context.Context, key string) (store.Stamp, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	st, ok := s.stamps[key]
	if ok == false {
		return store.Stamp{}, store.ErrNoStamp
	}
	return st, nil
}

func (s *Store) SaveState(_ context.Context, key string, epoch store.Epoch, state []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.fenced(key, epoch); err != nil {
		return err
	}
	cp := make([]byte, len(state))
	copy(cp, state)
	s.states[key] = cp
	return nil
}

func (s *Store) LoadState(_ context.Context, key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	st, ok := s.states[key]
	if ok == false {
		return nil, store.ErrNoState
	}
	cp := make([]byte, len(st))
	copy(cp, st)
	return cp, nil
}

func (s *Store) Release(_ context.Context, key string, epoch store.Epoch, status store.Status) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.fenced(key, epoch); err != nil {
		return err
	}
	st := s.stamps[key]
	st.Status = status
	s.stamps[key] = st
	return nil
}

func (s *Store) Delete(_ context.Context, key string, epoch store.Epoch) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.fenced(key, epoch); err != nil {
		return err
	}
	delete(s.stamps, key)
	delete(s.states, key)
	return nil
}

// fenced verifies the caller still holds the key's current epoch.
func (s *Store) fenced(key string, epoch store.Epoch) error {
	st, ok := s.stamps[key]
	if ok == false {
		return store.ErrNoStamp
	}
	if st.Epoch != epoch {
		return store.ErrFenced
	}
	return nil
}
