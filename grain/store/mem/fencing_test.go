package mem

import (
	"bytes"
	"testing"

	"ergo.services/application/grain/store"
)

func TestSaveLoadState(t *testing.T) {
	c := &clock{t: 1000}
	s := newStore(c)
	l := mustAcquireLease(t, s, "a@h", 10)
	st := mustAcquire(t, s, "k", "a@h", l.Incarnation)

	if err := s.SaveState(ctx, "k", st.Epoch, []byte("v1")); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	got, err := s.LoadState(ctx, "k")
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if string(got) != "v1" {
		t.Fatalf("LoadState: got %q, want v1", got)
	}
}

func TestSaveStateWrongEpochFenced(t *testing.T) {
	c := &clock{t: 1000}
	s := newStore(c)
	l := mustAcquireLease(t, s, "a@h", 10)
	st := mustAcquire(t, s, "k", "a@h", l.Incarnation)

	if err := s.SaveState(ctx, "k", st.Epoch, []byte("v1")); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	if err := s.SaveState(ctx, "k", st.Epoch+1, []byte("v2")); err != store.ErrFenced {
		t.Fatalf("SaveState wrong epoch: got %v, want ErrFenced", err)
	}
	// state unchanged
	got, _ := s.LoadState(ctx, "k")
	if string(got) != "v1" {
		t.Fatalf("fenced write leaked: got %q, want v1", got)
	}
}

func TestSaveStateUnownedKey(t *testing.T) {
	s := New()
	if err := s.SaveState(ctx, "k", 1, []byte("v")); err != store.ErrNoStamp {
		t.Fatalf("SaveState unowned: got %v, want ErrNoStamp", err)
	}
}

func TestReleaseWrongEpochFenced(t *testing.T) {
	c := &clock{t: 1000}
	s := newStore(c)
	l := mustAcquireLease(t, s, "a@h", 10)
	st := mustAcquire(t, s, "k", "a@h", l.Incarnation)

	if err := s.Release(ctx, "k", st.Epoch+1, store.StatusReleasedClean); err != store.ErrFenced {
		t.Fatalf("Release wrong epoch: got %v, want ErrFenced", err)
	}
	cur, _ := s.Read(ctx, "k")
	if cur.Status != store.StatusRunning {
		t.Fatalf("fenced release changed status: got %d", cur.Status)
	}
}

func TestDeleteFencingAndRemoval(t *testing.T) {
	c := &clock{t: 1000}
	s := newStore(c)
	l := mustAcquireLease(t, s, "a@h", 10)
	st := mustAcquire(t, s, "k", "a@h", l.Incarnation)
	if err := s.SaveState(ctx, "k", st.Epoch, []byte("v")); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	if err := s.Delete(ctx, "k", st.Epoch+1); err != store.ErrFenced {
		t.Fatalf("Delete wrong epoch: got %v, want ErrFenced", err)
	}
	if err := s.Delete(ctx, "k", st.Epoch); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Read(ctx, "k"); err != store.ErrNoStamp {
		t.Fatalf("Read after delete: got %v, want ErrNoStamp", err)
	}
	if _, err := s.LoadState(ctx, "k"); err != store.ErrNoState {
		t.Fatalf("LoadState after delete: got %v, want ErrNoState", err)
	}
}

// The zombie split-brain guard: a superseded owner cannot persist.
func TestZombieWriteFencedAfterHandoff(t *testing.T) {
	c := &clock{t: 1000}
	s := newStore(c)

	la := mustAcquireLease(t, s, "a@h", 10)
	a := mustAcquire(t, s, "k", "a@h", la.Incarnation)
	if err := s.SaveState(ctx, "k", a.Epoch, []byte("from-a")); err != nil {
		t.Fatalf("a SaveState: %v", err)
	}

	// a's node dies (stops renewing); lease expires; b reclaims
	c.add(10)
	lb := mustAcquireLease(t, s, "b@h", 10)
	b, err := s.Acquire(ctx, "k", "b@h", lb.Incarnation)
	if err != nil {
		t.Fatalf("b Acquire: %v", err)
	}

	// a comes back as a zombie holding its stale epoch
	if err := s.SaveState(ctx, "k", a.Epoch, []byte("zombie")); err != store.ErrFenced {
		t.Fatalf("zombie write: got %v, want ErrFenced", err)
	}
	// b writes with the fresh epoch
	if err := s.SaveState(ctx, "k", b.Epoch, []byte("from-b")); err != nil {
		t.Fatalf("b SaveState: %v", err)
	}
	got, _ := s.LoadState(ctx, "k")
	if bytes.Equal(got, []byte("from-b")) == false {
		t.Fatalf("state after handoff: got %q, want from-b", got)
	}
}

// State persists across a clean release and reacquire.
func TestStateSurvivesReleaseAndReacquire(t *testing.T) {
	c := &clock{t: 1000}
	s := newStore(c)
	l := mustAcquireLease(t, s, "a@h", 10)
	st := mustAcquire(t, s, "k", "a@h", l.Incarnation)
	if err := s.SaveState(ctx, "k", st.Epoch, []byte("persisted")); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	if err := s.Release(ctx, "k", st.Epoch, store.StatusReleasedClean); err != nil {
		t.Fatalf("Release: %v", err)
	}

	st2 := mustAcquire(t, s, "k", "a@h", l.Incarnation)
	got, err := s.LoadState(ctx, "k")
	if err != nil {
		t.Fatalf("LoadState after reacquire: %v", err)
	}
	if string(got) != "persisted" {
		t.Fatalf("state lost across reacquire: got %q", got)
	}
	// the reacquired owner writes under the new epoch
	if err := s.SaveState(ctx, "k", st2.Epoch, []byte("v2")); err != nil {
		t.Fatalf("SaveState after reacquire: %v", err)
	}
}

// SaveState copies its input and LoadState returns a copy, so callers cannot
// mutate stored state through an aliased slice.
func TestStateIsolation(t *testing.T) {
	c := &clock{t: 1000}
	s := newStore(c)
	l := mustAcquireLease(t, s, "a@h", 10)
	st := mustAcquire(t, s, "k", "a@h", l.Incarnation)

	in := []byte("data")
	if err := s.SaveState(ctx, "k", st.Epoch, in); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	in[0] = 'X' // mutate caller's slice after save

	got, _ := s.LoadState(ctx, "k")
	if string(got) != "data" {
		t.Fatalf("stored state aliased caller input: got %q", got)
	}
	got[0] = 'Y' // mutate returned slice

	again, _ := s.LoadState(ctx, "k")
	if string(again) != "data" {
		t.Fatalf("returned slice aliased stored state: got %q", again)
	}
}
