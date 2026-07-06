package grid

import (
	"testing"
	"time"

	"ergo.services/ergo/gen"
	"ergo.services/ergo/testing/unit"
)

func TestShard_MonitorSnapshot(t *testing.T) {
	sub := spawnShard(t, "mon_snap")
	owner := gen.PID{Node: unitNode, ID: 10, Creation: 1}
	watcher := gen.PID{Node: unitNode, ID: 99, Creation: 1}

	sub.Call(owner, registerRequest{Key: "svc", PID: owner, Meta: "v1"})
	sub.SendMessage(watcher, messageMonitor{Subscriber: watcher, Kind: subKey, Match: "svc"})

	sub.ShouldSend().To(watcher).
		Message(MessageRegistered{Domain: "mon_snap", Key: "svc", Owner: owner, Meta: "v1"}).
		Once().Assert()
	sub.ShouldMonitor().Target(watcher).Once().Assert()
}

func TestShard_MonitorRegisterEvent(t *testing.T) {
	sub := spawnShard(t, "mon_reg")
	owner := gen.PID{Node: unitNode, ID: 10, Creation: 1}
	watcher := gen.PID{Node: unitNode, ID: 99, Creation: 1}

	sub.SendMessage(watcher, messageMonitor{Subscriber: watcher, Kind: subAll})
	sub.Call(owner, registerRequest{Key: "svc", PID: owner, Meta: "v1"})

	sub.ShouldSend().To(watcher).
		Message(MessageRegistered{Domain: "mon_reg", Key: "svc", Owner: owner, Meta: "v1"}).
		Once().Assert()
}

func TestShard_MonitorUnregisterEvent(t *testing.T) {
	sub := spawnShard(t, "mon_unreg")
	owner := gen.PID{Node: unitNode, ID: 10, Creation: 1}
	watcher := gen.PID{Node: unitNode, ID: 99, Creation: 1}

	sub.SendMessage(watcher, messageMonitor{Subscriber: watcher, Kind: subAll})
	sub.Call(owner, registerRequest{Key: "svc", PID: owner, Meta: "v1"})
	sub.Call(owner, unregisterRequest{Key: "svc", PID: owner})

	sub.ShouldSend().To(watcher).
		Message(MessageUnregistered{Domain: "mon_unreg", Key: "svc", Owner: owner, Reason: ReasonUnregister}).
		Once().Assert()
}

func TestShard_MonitorUpdateEvent(t *testing.T) {
	sub := spawnShard(t, "mon_upd")
	owner := gen.PID{Node: unitNode, ID: 10, Creation: 1}
	watcher := gen.PID{Node: unitNode, ID: 99, Creation: 1}

	sub.SendMessage(watcher, messageMonitor{Subscriber: watcher, Kind: subAll})
	sub.Call(owner, registerRequest{Key: "svc", PID: owner, Meta: "v1"})
	sub.Call(owner, registerRequest{Key: "svc", PID: owner, Meta: "v2"})

	sub.ShouldSend().To(watcher).
		Message(MessageUpdated{Domain: "mon_upd", Key: "svc", Owner: owner, Meta: "v2"}).
		Once().Assert()
}

func TestShard_MonitorPrefixFilters(t *testing.T) {
	sub := spawnShard(t, "mon_pre")
	owner := gen.PID{Node: unitNode, ID: 10, Creation: 1}
	watcher := gen.PID{Node: unitNode, ID: 99, Creation: 1}

	// segment-aware (default separator "/"): "acc" scopes the acc subtree without
	// a trailing slash and does not over-match a sibling like "accounts/9".
	sub.SendMessage(watcher, messageMonitor{Subscriber: watcher, Kind: subPrefix, Match: "acc"})
	sub.Call(owner, registerRequest{Key: "acc/1", PID: owner, Meta: "x"})
	sub.Call(owner, registerRequest{Key: "accounts/9", PID: owner, Meta: "y"})
	sub.Call(owner, registerRequest{Key: "other", PID: owner, Meta: "z"})

	sub.ShouldSend().To(watcher).
		Message(MessageRegistered{Domain: "mon_pre", Key: "acc/1", Owner: owner, Meta: "x"}).
		Once().Assert()
	sub.ShouldSend().To(watcher).
		Message(MessageRegistered{Domain: "mon_pre", Key: "accounts/9", Owner: owner, Meta: "y"}).
		None().Assert()
	sub.ShouldSend().To(watcher).
		Message(MessageRegistered{Domain: "mon_pre", Key: "other", Owner: owner, Meta: "z"}).
		None().Assert()
}

// Overlapping scopes deliver a single message per event (per-subscriber dedup).
func TestShard_MonitorOverlappingScopesDedup(t *testing.T) {
	sub := spawnShard(t, "mon_dedup")
	owner := gen.PID{Node: unitNode, ID: 10, Creation: 1}
	watcher := gen.PID{Node: unitNode, ID: 99, Creation: 1}

	sub.SendMessage(watcher, messageMonitor{Subscriber: watcher, Kind: subPrefix, Match: "acc"})
	sub.SendMessage(watcher, messageMonitor{Subscriber: watcher, Kind: subKey, Match: "acc/1"})
	sub.Call(owner, registerRequest{Key: "acc/1", PID: owner, Meta: "x"})

	sub.ShouldSend().To(watcher).
		Message(MessageRegistered{Domain: "mon_dedup", Key: "acc/1", Owner: owner, Meta: "x"}).
		Once().Assert()
}

func TestShard_MonitorOwnerDownEvent(t *testing.T) {
	sub := spawnShard(t, "mon_down")
	owner := gen.PID{Node: unitNode, ID: 10, Creation: 1}
	watcher := gen.PID{Node: unitNode, ID: 99, Creation: 1}

	sub.SendMessage(watcher, messageMonitor{Subscriber: watcher, Kind: subAll})
	sub.Call(owner, registerRequest{Key: "svc", PID: owner, Meta: "v1"})
	sub.DeliverDown(owner, gen.TerminateReasonNormal)

	sub.ShouldSend().To(watcher).
		Message(MessageUnregistered{Domain: "mon_down", Key: "svc", Owner: owner, Reason: ReasonDown}).
		Once().Assert()
}

func TestShard_DemonitorStopsEvents(t *testing.T) {
	sub := spawnShard(t, "mon_demon")
	owner := gen.PID{Node: unitNode, ID: 10, Creation: 1}
	watcher := gen.PID{Node: unitNode, ID: 99, Creation: 1}

	sub.SendMessage(watcher, messageMonitor{Subscriber: watcher, Kind: subAll})
	sub.SendMessage(watcher, messageUnmonitor{Subscriber: watcher, Kind: subAll})
	sub.Call(owner, registerRequest{Key: "svc", PID: owner, Meta: "v1"})

	sub.ShouldSend().To(watcher).None().Assert()
	sub.ShouldDemonitor().Target(watcher).Once().Assert()
}

func TestShard_SubscriberDownCleansUp(t *testing.T) {
	sub := spawnShard(t, "mon_subdown")
	owner := gen.PID{Node: unitNode, ID: 10, Creation: 1}
	owner2 := gen.PID{Node: unitNode, ID: 11, Creation: 1}
	watcher := gen.PID{Node: unitNode, ID: 99, Creation: 1}

	sub.Call(owner, registerRequest{Key: "a", PID: owner, Meta: "1"})
	sub.SendMessage(watcher, messageMonitor{Subscriber: watcher, Kind: subAll})
	sub.DeliverDown(watcher, gen.TerminateReasonNormal)
	sub.Call(owner2, registerRequest{Key: "b", PID: owner2, Meta: "2"})

	// only the snapshot send for "a" reached the watcher; nothing after its down
	sub.ShouldSend().To(watcher).Times(1).Assert()
}

func TestShard_MonitorRefcountSharedPID(t *testing.T) {
	sub := spawnShard(t, "mon_rc")
	p := gen.PID{Node: unitNode, ID: 10, Creation: 1}

	sub.SendMessage(p, messageMonitor{Subscriber: p, Kind: subAll})
	sub.Call(p, registerRequest{Key: "svc", PID: p, Meta: "v1"})
	sub.ShouldMonitor().Target(p).Once().Assert()

	sub.Call(p, unregisterRequest{Key: "svc", PID: p})
	sub.ShouldDemonitor().Target(p).None().Assert()

	sub.SendMessage(p, messageUnmonitor{Subscriber: p, Kind: subAll})
	sub.ShouldDemonitor().Target(p).Once().Assert()
}

func TestShard_MonitorRemoteRegisterEvent(t *testing.T) {
	sub := spawnShard(t, "mon_remote")
	watcher := gen.PID{Node: unitNode, ID: 99, Creation: 1}
	remote := gen.PID{Node: "peer@localhost", ID: 5, Creation: 1}

	sub.SendMessage(watcher, messageMonitor{Subscriber: watcher, Kind: subAll})
	sub.SendMessage(remote, messageRegister{Key: "svc", Owner: remote, Meta: "r", Time: time.Now().UnixNano()})

	sub.ShouldSend().To(watcher).
		Message(MessageRegistered{Domain: "mon_remote", Key: "svc", Owner: remote, Meta: "r"}).
		Once().Assert()
}

// Subscriptions persist in storeData: a fresh shard attached to the surviving
// data rebuilds the subscriber monitor and keeps firing events.
func TestShard_WatchersSurviveRestart(t *testing.T) {
	watcher := gen.PID{Node: unitNode, ID: 99, Creation: 1}
	owner := gen.PID{Node: unitNode, ID: 10, Creation: 1}

	sub1 := spawnShardIndex(t, "surv", 1, 0)
	sub1.SendMessage(watcher, messageMonitor{Subscriber: watcher, Kind: subAll})

	sub2 := spawnShardIndex(t, "surv", 1, 0)
	sub2.ShouldMonitor().Target(watcher).Once().Assert()

	sub2.Call(owner, registerRequest{Key: "svc", PID: owner, Meta: "v1"})
	sub2.ShouldSend().To(watcher).
		Message(MessageRegistered{Domain: "surv", Key: "svc", Owner: owner, Meta: "v1"}).
		Once().Assert()
}

// A subscriber that died while the shard was down is dropped on rebuild: the
// re-monitor fails (Ergo returns an error, not a down, for a dead target), so
// the stale subscription must not linger.
func TestShard_DeadWatcherDroppedOnRebuild(t *testing.T) {
	watcher := gen.PID{Node: unitNode, ID: 99, Creation: 1}

	sub1 := spawnShardIndex(t, "survx", 1, 0)
	sub1.SendMessage(watcher, messageMonitor{Subscriber: watcher, Kind: subAll})

	sub2 := unit.Prepare(t, factoryShard, gen.ProcessOptions{}, Options{Domain: "survx", Shards: 1}, 0)
	sub2.OnMonitor(watcher).Fail(gen.ErrProcessTerminated)
	if err := sub2.Run(); err != nil {
		t.Fatal(err)
	}

	b := sub2.Behavior().(*shard)
	if _, ok := b.watch.scopes[watcher]; ok {
		t.Fatal("dead watcher should be dropped on rebuild")
	}
}

func TestShard_MonitorConflictOwnerChange(t *testing.T) {
	sub := spawnShard(t, "mon_conf")
	local := gen.PID{Node: unitNode, ID: 10, Creation: 1}
	remote := gen.PID{Node: "peer@localhost", ID: 5, Creation: 1}
	watcher := gen.PID{Node: unitNode, ID: 99, Creation: 1}

	sub.SendMessage(watcher, messageMonitor{Subscriber: watcher, Kind: subAll})
	sub.Call(local, registerRequest{Key: "svc", PID: local, Meta: "local"})
	future := time.Now().Add(time.Hour).UnixNano()
	sub.SendMessage(remote, messageRegister{Key: "svc", Owner: remote, Meta: "remote", Time: future})

	sub.ShouldSend().To(watcher).
		Message(MessageUnregistered{Domain: "mon_conf", Key: "svc", Owner: local, Reason: ReasonConflict}).
		Once().Assert()
	sub.ShouldSend().To(watcher).
		Message(MessageRegistered{Domain: "mon_conf", Key: "svc", Owner: remote, Meta: "remote"}).
		Once().Assert()
}

// Overlapping scopes deliver one MessageRegistered per key on snapshot, not one
// per matching scope.
func TestShard_SnapshotDedupPreexistingKey(t *testing.T) {
	sub := spawnShard(t, "snap_dedup")
	owner := gen.PID{Node: unitNode, ID: 10, Creation: 1}
	watcher := gen.PID{Node: unitNode, ID: 99, Creation: 1}

	sub.Call(owner, registerRequest{Key: "acc/1", PID: owner, Meta: "x"})
	sub.SendMessage(watcher, messageMonitor{Subscriber: watcher, Kind: subPrefix, Match: "acc"})
	sub.SendMessage(watcher, messageMonitor{Subscriber: watcher, Kind: subKey, Match: "acc/1"})

	sub.ShouldSend().To(watcher).
		Message(MessageRegistered{Domain: "snap_dedup", Key: "acc/1", Owner: owner, Meta: "x"}).
		Once().Assert()
}

// A replicated unregister carries its reason to subscribers.
func TestShard_RemoteUnregisterPropagatesReason(t *testing.T) {
	sub := spawnShard(t, "reason_prop")
	watcher := gen.PID{Node: unitNode, ID: 99, Creation: 1}
	remote := gen.PID{Node: "peer@localhost", ID: 5, Creation: 1}

	sub.SendMessage(watcher, messageMonitor{Subscriber: watcher, Kind: subAll})
	sub.SendMessage(remote, messageRegister{Key: "svc", Owner: remote, Meta: "r", Time: time.Now().UnixNano()})
	sub.SendMessage(remote, messageUnregister{Key: "svc", Owner: remote, Reason: ReasonDown})

	sub.ShouldSend().To(watcher).
		Message(MessageUnregistered{Domain: "reason_prop", Key: "svc", Owner: remote, Reason: ReasonDown}).
		Once().Assert()
}
