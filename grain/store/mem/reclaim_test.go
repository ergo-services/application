package mem

import (
	"testing"

	"ergo.services/application/grain/store"
)

// End-to-end hard-kill reclaim: an owner node dies without releasing, its lease
// expires, a survivor reclaims the key and reloads the last-saved state, and the
// dead node's zombie writes are fenced.
func TestReclaimAfterHardKill(t *testing.T) {
	c := &clock{t: 1000}
	s := newStore(c)

	la := mustAcquireLease(t, s, "a@h", 10)
	a := mustAcquire(t, s, "order/42", "a@h", la.Incarnation)
	if err := s.SaveState(ctx, "order/42", a.Epoch, []byte("balance=100")); err != nil {
		t.Fatalf("a SaveState: %v", err)
	}

	// a is SIGKILLed: no Release, no more Renew. Time passes beyond the TTL.
	c.add(11)

	// survivor b needs the key
	lb := mustAcquireLease(t, s, "b@h", 10)
	b, err := s.Acquire(ctx, "order/42", "b@h", lb.Incarnation)
	if err != nil {
		t.Fatalf("b reclaim: %v", err)
	}
	state, err := s.LoadState(ctx, "order/42")
	if err != nil {
		t.Fatalf("b LoadState: %v", err)
	}
	if string(state) != "balance=100" {
		t.Fatalf("reclaimed state: got %q, want balance=100", state)
	}
	if err := s.SaveState(ctx, "order/42", a.Epoch, []byte("zombie")); err != store.ErrFenced {
		t.Fatalf("zombie a write: got %v, want ErrFenced", err)
	}
	if err := s.SaveState(ctx, "order/42", b.Epoch, []byte("balance=90")); err != nil {
		t.Fatalf("b SaveState: %v", err)
	}
}

// A renewing owner keeps its key: a survivor cannot steal a live lock.
func TestRenewKeepsOwnership(t *testing.T) {
	c := &clock{t: 1000}
	s := newStore(c)

	la := mustAcquireLease(t, s, "a@h", 10)
	mustAcquire(t, s, "k", "a@h", la.Incarnation)
	lb := mustAcquireLease(t, s, "b@h", 10)

	// a keeps renewing across several TTL windows
	for i := 0; i < 5; i++ {
		c.add(8)
		if _, err := s.Renew(ctx, "a@h"); err != nil {
			t.Fatalf("a Renew: %v", err)
		}
		if _, err := s.Acquire(ctx, "k", "b@h", lb.Incarnation); err != store.ErrAlreadyOwned {
			t.Fatalf("b Acquire while a renews: got %v, want ErrAlreadyOwned", err)
		}
	}
}

// DropLease on graceful shutdown lets a survivor reclaim immediately, without
// waiting for the TTL.
func TestDropLeaseEnablesImmediateReclaim(t *testing.T) {
	c := &clock{t: 1000}
	s := newStore(c)

	la := mustAcquireLease(t, s, "a@h", 100)
	a := mustAcquire(t, s, "k", "a@h", la.Incarnation)

	// a shuts down gracefully: releases its keys, then drops its lease.
	if err := s.Release(ctx, "k", a.Epoch, store.StatusReleasedClean); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if err := s.DropLease(ctx, "a@h"); err != nil {
		t.Fatalf("DropLease: %v", err)
	}

	lb := mustAcquireLease(t, s, "b@h", 100)
	if _, err := s.Acquire(ctx, "k", "b@h", lb.Incarnation); err != nil {
		t.Fatalf("b reclaim after graceful drop: %v", err)
	}
}
