package mem

import (
	"context"
	"testing"

	"ergo.services/application/grain/store"
	"ergo.services/ergo/gen"
)

var ctx = context.Background()

// clock is a manual unix-seconds source for deterministic lease-expiry tests.
type clock struct{ t int64 }

func (c *clock) now() int64  { return c.t }
func (c *clock) set(t int64) { c.t = t }
func (c *clock) add(d int64) { c.t += d }

func newStore(c *clock) *Store {
	return New(WithClock(c.now))
}

// mustAcquireLease establishes a node's lease and fails the test on error.
func mustAcquireLease(t *testing.T, s *Store, node gen.Atom, ttl int64) store.Lease {
	t.Helper()
	l, err := s.AcquireLease(ctx, node, ttl)
	if err != nil {
		t.Fatalf("AcquireLease(%s): %v", node, err)
	}
	return l
}

// mustAcquire takes a key and fails the test on error.
func mustAcquire(t *testing.T, s *Store, key string, node gen.Atom, inc store.Incarnation) store.Stamp {
	t.Helper()
	st, err := s.Acquire(ctx, key, node, inc)
	if err != nil {
		t.Fatalf("Acquire(%s) by %s: %v", key, node, err)
	}
	return st
}

func TestNewEmpty(t *testing.T) {
	s := New()

	if _, err := s.Read(ctx, "k"); err != store.ErrNoStamp {
		t.Fatalf("Read on empty: got %v, want ErrNoStamp", err)
	}
	if _, err := s.LoadState(ctx, "k"); err != store.ErrNoState {
		t.Fatalf("LoadState on empty: got %v, want ErrNoState", err)
	}
	if _, err := s.ReadLease(ctx, "n@h"); err != store.ErrNoLease {
		t.Fatalf("ReadLease on empty: got %v, want ErrNoLease", err)
	}
	if _, err := s.Renew(ctx, "n@h"); err != store.ErrNoLease {
		t.Fatalf("Renew on empty: got %v, want ErrNoLease", err)
	}
}
