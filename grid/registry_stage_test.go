package grid

import (
	"testing"
	"time"

	"ergo.services/ergo/act"
	"ergo.services/ergo/gen"
	"ergo.services/ergo/testing/stage"
)

type holderRegister struct {
	Domain gen.Atom
	Key    string
	Meta   any
}

type holderResult struct {
	Err error
}

func factoryRegHolder() gen.ProcessBehavior { return &regHolder{} }

type regHolder struct {
	act.Actor
}

func (h *regHolder) HandleCall(from gen.PID, ref gen.Ref, request any) (any, error) {
	switch r := request.(type) {
	case holderRegister:
		return holderResult{Err: Register(h, r.Domain, r.Key, r.Meta)}, nil
	}
	return holderResult{Err: gen.ErrUnsupported}, nil
}

func doRegister(t *testing.T, n *stage.Node, owner gen.PID, key string, meta any) {
	t.Helper()
	v, err := n.Call(owner, holderRegister{Domain: "grid", Key: key, Meta: meta})
	if err != nil {
		t.Fatalf("%s: call holder: %s", n.Name(), err)
	}
	if res := v.(holderResult); res.Err != nil {
		t.Fatalf("%s: register %q: %s", n.Name(), key, res.Err)
	}
}

func assertLookupEventually(t *testing.T, n *stage.Node, key string, want gen.PID) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if pid, _, ok := lookupAt(n.Name(), "grid", key); ok && pid == want {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	pid, _, ok := lookupAt(n.Name(), "grid", key)
	t.Fatalf("%s: lookup %q did not converge; want %v, got %v (present=%v)", n.Name(), key, want, pid, ok)
}

func assertLookupGoneEventually(t *testing.T, n *stage.Node, key string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if _, _, ok := lookupAt(n.Name(), "grid", key); ok == false {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("%s: lookup %q was not purged", n.Name(), key)
}

// A registration on node A replicates to node B.
func TestRegistry_Replication(t *testing.T) {
	s := stage.New(t, stage.StageOptions{RegistrarFull: true})
	nA := s.StartNode("ra", stage.NodeOptions{
		Applications: []gen.ApplicationBehavior{CreateApp(Options{Domain: "grid"})},
	})
	nB := s.StartNode("rb", stage.NodeOptions{
		Applications: []gen.ApplicationBehavior{CreateApp(Options{Domain: "grid"})},
	})
	assertPeersEventually(t, nA, "grid", []gen.Atom{nB.Name()})

	owner := nA.Spawn(factoryRegHolder, gen.ProcessOptions{})
	doRegister(t, nA, owner, "svc", "v1")

	assertLookupEventually(t, nB, "svc", owner)
}

// A node joining later receives existing registrations via cluster_state sync.
func TestRegistry_LateJoinSync(t *testing.T) {
	s := stage.New(t, stage.StageOptions{RegistrarFull: true})
	nA := s.StartNode("la", stage.NodeOptions{
		Applications: []gen.ApplicationBehavior{CreateApp(Options{Domain: "grid"})},
	})
	nB := s.StartNode("lb", stage.NodeOptions{
		Applications: []gen.ApplicationBehavior{CreateApp(Options{Domain: "grid"})},
	})
	assertPeersEventually(t, nA, "grid", []gen.Atom{nB.Name()})

	owner := nA.Spawn(factoryRegHolder, gen.ProcessOptions{})
	doRegister(t, nA, owner, "svc", "v1")
	assertLookupEventually(t, nB, "svc", owner)

	nC := s.StartNode("lc", stage.NodeOptions{
		Applications: []gen.ApplicationBehavior{CreateApp(Options{Domain: "grid"})},
	})
	assertLookupEventually(t, nC, "svc", owner)
}

// When a node goes down its registrations are purged on survivors; theirs remain.
func TestRegistry_NodeDownPurge(t *testing.T) {
	s := stage.New(t, stage.StageOptions{RegistrarFull: true})
	nA := s.StartNode("da", stage.NodeOptions{
		Applications: []gen.ApplicationBehavior{CreateApp(Options{Domain: "grid"})},
	})
	nB := s.StartNode("db", stage.NodeOptions{
		Applications: []gen.ApplicationBehavior{CreateApp(Options{Domain: "grid"})},
	})
	assertPeersEventually(t, nA, "grid", []gen.Atom{nB.Name()})

	ownerA := nA.Spawn(factoryRegHolder, gen.ProcessOptions{})
	ownerB := nB.Spawn(factoryRegHolder, gen.ProcessOptions{})
	doRegister(t, nA, ownerA, "svcA", "a")
	doRegister(t, nB, ownerB, "svcB", "b")

	assertLookupEventually(t, nB, "svcA", ownerA)
	assertLookupEventually(t, nA, "svcB", ownerB)

	nA.Native().StopForce()

	assertLookupGoneEventually(t, nB, "svcA")
	if _, _, ok := lookupAt(nB.Name(), "grid", "svcB"); ok == false {
		t.Fatalf("%s: own registration svcB should survive peer down", nB.Name())
	}
}

type qCount struct{}

type qLocalCount struct{}

type qLocalEntries struct{}

func factoryIntrospect() gen.ProcessBehavior { return &introspect{} }

type introspect struct {
	act.Actor
}

func (i *introspect) HandleCall(from gen.PID, ref gen.Ref, request any) (any, error) {
	switch request.(type) {
	case qCount:
		return RegistryCount(i, "grid"), nil
	case qLocalCount:
		return LocalRegistryCount(i, "grid"), nil
	case qLocalEntries:
		return LocalEntries(i, "grid"), nil
	}
	return nil, gen.ErrUnsupported
}

func assertCountEventually(t *testing.T, n *stage.Node, pid gen.PID, want int) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	var last any
	for time.Now().Before(deadline) {
		v, err := n.Call(pid, qCount{})
		if err == nil {
			last = v
			if v.(int) == want {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("%s: RegistryCount did not converge to %d; got %v", n.Name(), want, last)
}

// RegistryCount converges cluster-wide; LocalRegistryCount / LocalEntries are
// scoped to the caller's node.
func TestRegistry_CountsAndEntries(t *testing.T) {
	s := stage.New(t, stage.StageOptions{RegistrarFull: true})
	nA := s.StartNode("pa", stage.NodeOptions{
		Applications: []gen.ApplicationBehavior{CreateApp(Options{Domain: "grid"})},
	})
	nB := s.StartNode("pb", stage.NodeOptions{
		Applications: []gen.ApplicationBehavior{CreateApp(Options{Domain: "grid"})},
	})
	assertPeersEventually(t, nA, "grid", []gen.Atom{nB.Name()})

	ownerA := nA.Spawn(factoryRegHolder, gen.ProcessOptions{})
	doRegister(t, nA, ownerA, "a", "1")
	doRegister(t, nA, ownerA, "b", "2")
	ownerB := nB.Spawn(factoryRegHolder, gen.ProcessOptions{})
	doRegister(t, nB, ownerB, "c", "3")

	assertLookupEventually(t, nA, "c", ownerB)

	introA := nA.Spawn(factoryIntrospect, gen.ProcessOptions{})
	assertCountEventually(t, nA, introA, 3)

	if v, err := nA.Call(introA, qLocalCount{}); err != nil || v.(int) != 2 {
		t.Fatalf("LocalRegistryCount(A) = %v (err %v), want 2", v, err)
	}
	v, err := nA.Call(introA, qLocalEntries{})
	if err != nil {
		t.Fatalf("LocalEntries: %s", err)
	}
	entries := v.([]Entry)
	if len(entries) != 2 {
		t.Fatalf("LocalEntries(A) = %d entries, want 2 (a,b)", len(entries))
	}
	for _, e := range entries {
		if e.Owner != ownerA {
			t.Fatalf("LocalEntries(A) has foreign owner %v", e.Owner)
		}
	}
}
