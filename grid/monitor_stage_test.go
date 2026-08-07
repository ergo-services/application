package grid

import (
	"testing"
	"time"

	"ergo.services/ergo/act"
	"ergo.services/ergo/gen"
	"ergo.services/ergo/testing/stage"
)

type watchStart struct{}

type watchKeys struct{}

type watchKeysResult struct {
	Keys []string
}

func factoryGridWatcher() gen.ProcessBehavior { return &gridWatcher{} }

type gridWatcher struct {
	act.Actor

	keys []string
}

func (w *gridWatcher) HandleMessage(from gen.PID, message any) error {
	switch m := message.(type) {
	case MessageRegistered:
		w.keys = append(w.keys, m.Key)
	}
	return nil
}

func (w *gridWatcher) HandleCall(from gen.PID, ref gen.Ref, request any) (any, error) {
	switch request.(type) {
	case watchStart:
		if err := MonitorAll(w, "grid"); err != nil {
			return watchKeysResult{}, err
		}
		return watchKeysResult{}, nil
	case watchKeys:
		return watchKeysResult{Keys: append([]string(nil), w.keys...)}, nil
	}
	return nil, gen.ErrUnsupported
}

func assertWatcherHasKey(t *testing.T, n *stage.Node, watcher gen.PID, key string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		v, err := n.Call(watcher, watchKeys{})
		if err == nil {
			for _, k := range v.(watchKeysResult).Keys {
				if k == key {
					return
				}
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("%s: watcher never observed key %q", n.Name(), key)
}

// A watcher on node B observes, via replication, a registration made on node A.
func TestMonitor_CrossNodeRegister(t *testing.T) {
	s := stage.New(t, stage.StageOptions{RegistrarFull: true})
	nA := s.StartNode("wa", stage.NodeOptions{
		Applications: []gen.ApplicationBehavior{CreateApp(Options{Domain: "grid"})},
	})
	nB := s.StartNode("wb", stage.NodeOptions{
		Applications: []gen.ApplicationBehavior{CreateApp(Options{Domain: "grid"})},
	})
	assertPeersEventually(t, nA, "grid", []gen.Atom{nB.Name()})

	watcher := nB.Spawn(factoryGridWatcher, gen.ProcessOptions{})
	if _, err := nB.Call(watcher, watchStart{}); err != nil {
		t.Fatalf("watchStart: %s", err)
	}

	owner := nA.Spawn(factoryRegHolder, gen.ProcessOptions{})
	doRegister(t, nA, owner, "svc", "v1")

	assertWatcherHasKey(t, nB, watcher, "svc")
}
