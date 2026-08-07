package mem

import (
	"testing"

	"ergo.services/application/grain/store"
)

func TestAcquireLeaseMintsMonotonicIncarnation(t *testing.T) {
	c := &clock{t: 1000}
	s := newStore(c)

	l1 := mustAcquireLease(t, s, "n@h", 10)
	l2 := mustAcquireLease(t, s, "n@h", 10)
	l3 := mustAcquireLease(t, s, "n@h", 10)

	if l1.Incarnation >= l2.Incarnation || l2.Incarnation >= l3.Incarnation {
		t.Fatalf("incarnations not strictly increasing: %d, %d, %d",
			l1.Incarnation, l2.Incarnation, l3.Incarnation)
	}
}

// The incarnation is a pure counter, never derived from the clock, so a
// sub-second bounce (same second) or a backward clock step still yields a
// strictly greater incarnation.
func TestAcquireLeaseIncarnationIndependentOfClock(t *testing.T) {
	c := &clock{t: 1000}
	s := newStore(c)

	l1 := mustAcquireLease(t, s, "n@h", 10)

	// same second (sub-second bounce)
	l2 := mustAcquireLease(t, s, "n@h", 10)
	// clock steps backward (e.g. NTP correction)
	c.set(500)
	l3 := mustAcquireLease(t, s, "n@h", 10)

	if l1.Incarnation >= l2.Incarnation || l2.Incarnation >= l3.Incarnation {
		t.Fatalf("incarnation regressed with clock: %d, %d, %d",
			l1.Incarnation, l2.Incarnation, l3.Incarnation)
	}
}

// A per-node counter is independent across nodes.
func TestAcquireLeasePerNodeIncarnation(t *testing.T) {
	c := &clock{t: 1000}
	s := newStore(c)

	a1 := mustAcquireLease(t, s, "a@h", 10)
	b1 := mustAcquireLease(t, s, "b@h", 10)
	a2 := mustAcquireLease(t, s, "a@h", 10)

	if a1.Incarnation != b1.Incarnation {
		t.Fatalf("first incarnation should match per node: a=%d b=%d",
			a1.Incarnation, b1.Incarnation)
	}
	if a2.Incarnation <= a1.Incarnation {
		t.Fatalf("node a did not advance: %d -> %d", a1.Incarnation, a2.Incarnation)
	}
}

func TestRenewRefreshesHeartbeat(t *testing.T) {
	c := &clock{t: 1000}
	s := newStore(c)

	mustAcquireLease(t, s, "n@h", 10)
	c.add(5)

	l, err := s.Renew(ctx, "n@h")
	if err != nil {
		t.Fatalf("Renew: %v", err)
	}
	if l.LastHeartbeat != 1005 {
		t.Fatalf("LastHeartbeat: got %d, want 1005", l.LastHeartbeat)
	}
}

func TestRenewUnknownNode(t *testing.T) {
	s := New()
	if _, err := s.Renew(ctx, "ghost@h"); err != store.ErrNoLease {
		t.Fatalf("Renew unknown: got %v, want ErrNoLease", err)
	}
}

func TestDropLeaseRemovesButKeepsMonotonicMinting(t *testing.T) {
	c := &clock{t: 1000}
	s := newStore(c)

	l1 := mustAcquireLease(t, s, "n@h", 10)

	if err := s.DropLease(ctx, "n@h"); err != nil {
		t.Fatalf("DropLease: %v", err)
	}
	if _, err := s.ReadLease(ctx, "n@h"); err != store.ErrNoLease {
		t.Fatalf("ReadLease after drop: got %v, want ErrNoLease", err)
	}

	// A fresh boot after a drop must still advance past the dropped lease.
	l2 := mustAcquireLease(t, s, "n@h", 10)
	if l2.Incarnation <= l1.Incarnation {
		t.Fatalf("incarnation regressed after DropLease: %d -> %d",
			l1.Incarnation, l2.Incarnation)
	}
}
