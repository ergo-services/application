package mem

import (
	"testing"

	"ergo.services/application/grain/store"
)

func TestAcquireAbsentKey(t *testing.T) {
	c := &clock{t: 1000}
	s := newStore(c)
	lease := mustAcquireLease(t, s, "a@h", 10)

	st := mustAcquire(t, s, "k", "a@h", lease.Incarnation)

	if st.OwnerNode != "a@h" {
		t.Fatalf("OwnerNode: got %s, want a@h", st.OwnerNode)
	}
	if st.Status != store.StatusRunning {
		t.Fatalf("Status: got %d, want StatusRunning", st.Status)
	}
	if st.Epoch != 1 {
		t.Fatalf("Epoch: got %d, want 1", st.Epoch)
	}
	if st.Incarnation != lease.Incarnation {
		t.Fatalf("Incarnation: got %d, want %d", st.Incarnation, lease.Incarnation)
	}
}

func TestAcquireLiveKeyRejected(t *testing.T) {
	c := &clock{t: 1000}
	s := newStore(c)
	la := mustAcquireLease(t, s, "a@h", 10)
	lb := mustAcquireLease(t, s, "b@h", 10)

	owner := mustAcquire(t, s, "k", "a@h", la.Incarnation)

	// b tries while a's lease is fresh
	got, err := s.Acquire(ctx, "k", "b@h", lb.Incarnation)
	if err != store.ErrAlreadyOwned {
		t.Fatalf("Acquire live: got %v, want ErrAlreadyOwned", err)
	}
	if got.OwnerNode != "a@h" || got.Epoch != owner.Epoch {
		t.Fatalf("ErrAlreadyOwned must return the live stamp: got %+v", got)
	}
}

func TestAcquireReclaimsExpiredLease(t *testing.T) {
	c := &clock{t: 1000}
	s := newStore(c)
	la := mustAcquireLease(t, s, "a@h", 10)
	first := mustAcquire(t, s, "k", "a@h", la.Incarnation)

	// a stops renewing; its lease expires
	c.add(10)
	lb := mustAcquireLease(t, s, "b@h", 10)

	got, err := s.Acquire(ctx, "k", "b@h", lb.Incarnation)
	if err != nil {
		t.Fatalf("Acquire after expiry: %v", err)
	}
	if got.OwnerNode != "b@h" {
		t.Fatalf("OwnerNode after reclaim: got %s, want b@h", got.OwnerNode)
	}
	if got.Epoch <= first.Epoch {
		t.Fatalf("Epoch must advance on reclaim: %d -> %d", first.Epoch, got.Epoch)
	}
}

func TestAcquireReclaimsReleasedKey(t *testing.T) {
	c := &clock{t: 1000}
	s := newStore(c)
	la := mustAcquireLease(t, s, "a@h", 10)
	first := mustAcquire(t, s, "k", "a@h", la.Incarnation)

	// a releases cleanly; lease is still fresh
	if err := s.Release(ctx, "k", first.Epoch, store.StatusReleasedClean); err != nil {
		t.Fatalf("Release: %v", err)
	}

	got, err := s.Acquire(ctx, "k", "a@h", la.Incarnation)
	if err != nil {
		t.Fatalf("Acquire released: %v", err)
	}
	if got.Status != store.StatusRunning {
		t.Fatalf("reacquired status: got %d, want StatusRunning", got.Status)
	}
	if got.Epoch <= first.Epoch {
		t.Fatalf("Epoch must advance on reacquire: %d -> %d", first.Epoch, got.Epoch)
	}
}

func TestAcquireReclaimsBouncedNode(t *testing.T) {
	c := &clock{t: 1000}
	s := newStore(c)
	la := mustAcquireLease(t, s, "a@h", 10)
	first := mustAcquire(t, s, "k", "a@h", la.Incarnation)

	// a reboots: same node name, new (higher) incarnation, lease still fresh
	la2 := mustAcquireLease(t, s, "a@h", 10)
	if la2.Incarnation <= first.Incarnation {
		t.Fatalf("reboot must advance incarnation")
	}

	got, err := s.Acquire(ctx, "k", "a@h", la2.Incarnation)
	if err != nil {
		t.Fatalf("Acquire after reboot: %v", err)
	}
	if got.Epoch <= first.Epoch || got.Incarnation != la2.Incarnation {
		t.Fatalf("reclaim after bounce: got %+v (first epoch %d)", got, first.Epoch)
	}
}

func TestAcquireEpochStrictlyIncreasesAcrossHandoffs(t *testing.T) {
	c := &clock{t: 1000}
	s := newStore(c)

	var last store.Epoch
	for i := 0; i < 5; i++ {
		l := mustAcquireLease(t, s, "a@h", 10)
		st := mustAcquire(t, s, "k", "a@h", l.Incarnation)
		if st.Epoch <= last {
			t.Fatalf("epoch did not advance on handoff %d: %d <= %d", i, st.Epoch, last)
		}
		last = st.Epoch
		if err := s.Release(ctx, "k", st.Epoch, store.StatusReleasedClean); err != nil {
			t.Fatalf("Release: %v", err)
		}
	}
}
