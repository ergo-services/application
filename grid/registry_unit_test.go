package grid

import (
	"testing"
	"time"

	"ergo.services/ergo/gen"
	"ergo.services/ergo/testing/check"
	"ergo.services/ergo/testing/unit"
)

const unitNode gen.Atom = "unit@localhost"

func spawnShard(t *testing.T, domain gen.Atom) *unit.Subject {
	t.Helper()
	sub, err := unit.Spawn(t, factoryShard, gen.ProcessOptions{}, Options{Domain: domain, Shards: 1}, 0)
	if err != nil {
		t.Fatal(err)
	}
	return sub
}

func TestShard_RegisterLookup(t *testing.T) {
	sub := spawnShard(t, "reg_lookup")
	owner := gen.PID{Node: unitNode, ID: 10, Creation: 1}

	if _, err := sub.Call(owner, registerRequest{Key: "svc", PID: owner, Meta: "v1"}); err != nil {
		t.Fatalf("register: %s", err)
	}

	pid, meta, ok := lookupAt(unitNode, "reg_lookup", "svc")
	check.True(t, ok)
	check.Equal(t, owner, pid)
	check.Equal(t, "v1", meta)
	sub.ShouldMonitor().Target(owner).Once().Assert()
}

func TestShard_RegisterTaken(t *testing.T) {
	sub := spawnShard(t, "reg_taken")
	owner1 := gen.PID{Node: unitNode, ID: 10, Creation: 1}
	owner2 := gen.PID{Node: unitNode, ID: 11, Creation: 1}

	if _, err := sub.Call(owner1, registerRequest{Key: "svc", PID: owner1, Meta: "v1"}); err != nil {
		t.Fatalf("register: %s", err)
	}
	_, err := sub.Call(owner2, registerRequest{Key: "svc", PID: owner2, Meta: "v2"})
	check.ErrorIs(t, err, gen.ErrTaken)

	pid, _, _ := lookupAt(unitNode, "reg_taken", "svc")
	check.Equal(t, owner1, pid)
}

func TestShard_Unregister(t *testing.T) {
	sub := spawnShard(t, "reg_unreg")
	owner := gen.PID{Node: unitNode, ID: 10, Creation: 1}

	sub.Call(owner, registerRequest{Key: "svc", PID: owner, Meta: "v1"})
	if _, err := sub.Call(owner, unregisterRequest{Key: "svc", PID: owner}); err != nil {
		t.Fatalf("unregister: %s", err)
	}

	_, _, ok := lookupAt(unitNode, "reg_unreg", "svc")
	check.False(t, ok)
	sub.ShouldDemonitor().Target(owner).Once().Assert()
}

func TestShard_ProcessDownRemovesKeys(t *testing.T) {
	sub := spawnShard(t, "reg_down")
	owner := gen.PID{Node: unitNode, ID: 10, Creation: 1}
	other := gen.PID{Node: unitNode, ID: 11, Creation: 1}

	sub.Call(owner, registerRequest{Key: "a", PID: owner, Meta: "1"})
	sub.Call(owner, registerRequest{Key: "b", PID: owner, Meta: "2"})
	sub.Call(other, registerRequest{Key: "c", PID: other, Meta: "3"})

	sub.DeliverDown(owner, gen.TerminateReasonNormal)

	if _, _, ok := lookupAt(unitNode, "reg_down", "a"); ok {
		t.Fatal("key a should be gone")
	}
	if _, _, ok := lookupAt(unitNode, "reg_down", "b"); ok {
		t.Fatal("key b should be gone")
	}
	if _, _, ok := lookupAt(unitNode, "reg_down", "c"); ok == false {
		t.Fatal("key c should survive")
	}
}

func TestShard_ConflictRemoteNewerWins(t *testing.T) {
	sub := spawnShard(t, "reg_conf_newer")
	local := gen.PID{Node: unitNode, ID: 10, Creation: 1}
	remote := gen.PID{Node: "peer@localhost", ID: 5, Creation: 1}

	sub.Call(local, registerRequest{Key: "svc", PID: local, Meta: "local"})
	future := time.Now().Add(time.Hour).UnixNano()
	sub.SendMessage(remote, messageRegister{Key: "svc", Owner: remote, Meta: "remote", Time: future})

	pid, meta, ok := lookupAt(unitNode, "reg_conf_newer", "svc")
	check.True(t, ok)
	check.Equal(t, remote, pid)
	check.Equal(t, "remote", meta)
	sub.ShouldSendExit().To(local).ReasonIs(ErrRegistryConflict).Once().Assert()
}

func TestShard_ConflictRemoteOlderLoses(t *testing.T) {
	sub := spawnShard(t, "reg_conf_older")
	local := gen.PID{Node: unitNode, ID: 10, Creation: 1}
	remote := gen.PID{Node: "peer@localhost", ID: 99, Creation: 1}

	sub.Call(local, registerRequest{Key: "svc", PID: local, Meta: "local"})
	sub.SendMessage(remote, messageRegister{Key: "svc", Owner: remote, Meta: "remote", Time: 1})

	pid, _, _ := lookupAt(unitNode, "reg_conf_older", "svc")
	check.Equal(t, local, pid)
	sub.ShouldSendExit().To(local).None().Assert()
}

func TestShard_ConflictEqualTimeTiebreak(t *testing.T) {
	sub := spawnShard(t, "reg_conf_tie")
	local := gen.PID{Node: unitNode, ID: 10, Creation: 1}
	sub.Call(local, registerRequest{Key: "svc", PID: local, Meta: "local"})

	b := sub.Behavior().(*shard)
	v, _ := b.data.regByKey.Load("svc")
	localTime := v.(entry).time

	// higher ID at equal time wins
	winner := gen.PID{Node: "peer@localhost", ID: 20, Creation: 1}
	sub.SendMessage(winner, messageRegister{Key: "svc", Owner: winner, Meta: "remote", Time: localTime})

	pid, _, _ := lookupAt(unitNode, "reg_conf_tie", "svc")
	check.Equal(t, winner, pid)
	sub.ShouldSendExit().To(local).ReasonIs(ErrRegistryConflict).Once().Assert()
}

func TestEntryWins(t *testing.T) {
	a := gen.PID{Node: "a@n", ID: 5, Creation: 1}
	b := gen.PID{Node: "b@n", ID: 5, Creation: 1}
	check.True(t, entryWins(2, a, 1, b))               // later time
	check.False(t, entryWins(1, a, 2, b))              // earlier time
	check.True(t, entryWins(1, gen.PID{ID: 9}, 1, gen.PID{ID: 3})) // higher ID on tie
	check.True(t, entryWins(1, b, 1, a))               // equal ID/creation, higher Node string
}

// A cluster_state snapshot drops a peer-owned key it omits (older than the
// snapshot) and notifies subscribers.
func TestShard_ClusterStateReconcileDropsPhantom(t *testing.T) {
	sub := spawnShard(t, "cs_recon")
	watcher := gen.PID{Node: unitNode, ID: 99, Creation: 1}
	remote := gen.PID{Node: "peer@localhost", ID: 5, Creation: 1}

	sub.SendMessage(watcher, messageMonitor{Subscriber: watcher, Kind: subAll})
	past := time.Now().Add(-time.Hour).UnixNano()
	sub.SendMessage(remote, messageRegister{Key: "svc", Owner: remote, Meta: "r", Time: past})

	sub.SendMessage(remote, messageClusterState{Node: "peer@localhost", At: time.Now().UnixNano(), Entries: nil})

	if _, _, ok := lookupAt(unitNode, "cs_recon", "svc"); ok {
		t.Fatal("phantom svc should be reconciled away")
	}
	sub.ShouldSend().To(watcher).
		Message(MessageUnregistered{Domain: "cs_recon", Key: "svc", Owner: remote, Reason: ReasonNodeDown}).
		Once().Assert()
}

// A key newer than the snapshot survives reconcile.
func TestShard_ClusterStateKeepsNewerThanSnapshot(t *testing.T) {
	sub := spawnShard(t, "cs_keep")
	remote := gen.PID{Node: "peer@localhost", ID: 5, Creation: 1}

	at := time.Now().UnixNano()
	sub.SendMessage(remote, messageRegister{Key: "svc", Owner: remote, Meta: "r", Time: time.Now().Add(time.Hour).UnixNano()})
	sub.SendMessage(remote, messageClusterState{Node: "peer@localhost", At: at, Entries: nil})

	if _, _, ok := lookupAt(unitNode, "cs_keep", "svc"); ok == false {
		t.Fatal("a key newer than the snapshot must be kept")
	}
}

// On restart, a key whose owner is gone is dropped with a MessageUnregistered.
func TestShard_RebuildDeadOwnerEmitsUnregistered(t *testing.T) {
	owner := gen.PID{Node: unitNode, ID: 10, Creation: 1}
	watcher := gen.PID{Node: unitNode, ID: 99, Creation: 1}

	sub1 := spawnShardIndex(t, "rb_owner", 1, 0)
	sub1.SendMessage(watcher, messageMonitor{Subscriber: watcher, Kind: subAll})
	sub1.Call(owner, registerRequest{Key: "svc", PID: owner, Meta: "v1"})

	sub2 := unit.Prepare(t, factoryShard, gen.ProcessOptions{}, Options{Domain: "rb_owner", Shards: 1}, 0)
	sub2.OnMonitor(owner).Fail(gen.ErrProcessTerminated)
	if err := sub2.Run(); err != nil {
		t.Fatal(err)
	}

	sub2.ShouldSend().To(watcher).
		Message(MessageUnregistered{Domain: "rb_owner", Key: "svc", Owner: owner, Reason: ReasonDown}).
		Once().Assert()
	if _, _, ok := lookupAt(unitNode, "rb_owner", "svc"); ok {
		t.Fatal("dead owner's key should be dropped on rebuild")
	}
}

// Shard count is node-scoped: one node's teardown does not affect another
// same-domain node in the same process.
func TestNumShardsFor_NodeScoped(t *testing.T) {
	registryShards.Store(storeKey{node: "n1@h", domain: "d4"}, 16)
	registryShards.Store(storeKey{node: "n2@h", domain: "d4"}, 4)
	defer registryShards.Delete(storeKey{node: "n2@h", domain: "d4"})

	check.Equal(t, 16, numShardsFor("n1@h", "d4"))
	check.Equal(t, 4, numShardsFor("n2@h", "d4"))

	registryShards.Delete(storeKey{node: "n1@h", domain: "d4"})
	check.Equal(t, 4, numShardsFor("n2@h", "d4"))
}
