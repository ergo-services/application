package grid

import (
	"sort"
	"testing"
	"time"

	"ergo.services/ergo/gen"
	"ergo.services/ergo/testing/stage"
)

// Registrar-driven discovery: three same-domain nodes converge to a full mesh.
func TestBootstrap_RegistrarDiscovery(t *testing.T) {
	s := stage.New(t, stage.StageOptions{RegistrarFull: true})

	n1 := s.StartNode("grid1", stage.NodeOptions{
		Applications: []gen.ApplicationBehavior{CreateApp(Options{Domain: "grid"})},
	})
	n2 := s.StartNode("grid2", stage.NodeOptions{
		Applications: []gen.ApplicationBehavior{CreateApp(Options{Domain: "grid"})},
	})
	n3 := s.StartNode("grid3", stage.NodeOptions{
		Applications: []gen.ApplicationBehavior{CreateApp(Options{Domain: "grid"})},
	})

	assertPeersEventually(t, n1, "grid", []gen.Atom{n2.Name(), n3.Name()})
	assertPeersEventually(t, n2, "grid", []gen.Atom{n1.Name(), n3.Name()})
	assertPeersEventually(t, n3, "grid", []gen.Atom{n1.Name(), n2.Name()})
}

// Seed discovery without a registrar: B seeds A; both peer via probe + CoreEvent.
func TestBootstrap_SeedDiscovery(t *testing.T) {
	s := stage.New(t) // RegistrarFull:false -> ResolveApplication unsupported

	nA := s.StartNode("gridA", stage.NodeOptions{
		Applications: []gen.ApplicationBehavior{CreateApp(Options{Domain: "grid"})},
	})
	nB := s.StartNode("gridB", stage.NodeOptions{
		Applications: []gen.ApplicationBehavior{CreateApp(Options{Domain: "grid", Peers: []gen.Atom{nA.Name()}})},
	})

	s.Connect(nA, nB)

	assertPeersEventually(t, nA, "grid", []gen.Atom{nB.Name()})
	assertPeersEventually(t, nB, "grid", []gen.Atom{nA.Name()})
}

// Domain isolation: two connected nodes in different domains never peer.
func TestBootstrap_DomainIsolation(t *testing.T) {
	s := stage.New(t, stage.StageOptions{RegistrarFull: true})

	nA := s.StartNode("giA", stage.NodeOptions{
		Applications: []gen.ApplicationBehavior{CreateApp(Options{Domain: "grid_a"})},
	})
	nB := s.StartNode("giB", stage.NodeOptions{
		Applications: []gen.ApplicationBehavior{CreateApp(Options{Domain: "grid_b"})},
	})

	s.Connect(nA, nB)

	// give discovery ample time to (not) happen, then assert both stay empty
	assertNoPeersStable(t, nA, "grid_a", 2*time.Second)
	assertNoPeersStable(t, nB, "grid_b", 2*time.Second)
}

// helpers

func gridPeers(t *testing.T, n *stage.Node, name gen.Atom) []gen.Atom {
	t.Helper()
	v, err := n.Call(n.ProcessID(shardName(name, 0)), getPeersRequest{})
	if err != nil {
		return nil // process may not be registered yet during startup
	}
	resp, ok := v.(getPeersResponse)
	if ok == false {
		t.Fatalf("grid: unexpected response %T from shard", v)
	}
	return resp.Nodes
}

func assertPeersEventually(t *testing.T, n *stage.Node, name gen.Atom, want []gen.Atom) {
	t.Helper()
	sorted := append([]gen.Atom(nil), want...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	deadline := time.Now().Add(20 * time.Second)
	var last []gen.Atom
	for time.Now().Before(deadline) {
		last = gridPeers(t, n, name)
		if atomsEqual(last, sorted) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("node %s: peers did not converge; want %v, got %v", n.Name(), sorted, last)
}

func assertNoPeersStable(t *testing.T, n *stage.Node, name gen.Atom, window time.Duration) {
	t.Helper()
	deadline := time.Now().Add(window)
	for time.Now().Before(deadline) {
		if got := gridPeers(t, n, name); len(got) != 0 {
			t.Fatalf("node %s: expected no peers, got %v", n.Name(), got)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func atomsEqual(a, b []gen.Atom) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
