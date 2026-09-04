package grid

import (
	"testing"

	"ergo.services/ergo/gen"
	"ergo.services/ergo/testing/check"
	"ergo.services/ergo/testing/unit"
)

func spawnShardIndex(t *testing.T, domain gen.Atom, shards, index int) *unit.Subject {
	t.Helper()
	sub, err := unit.Spawn(t, factoryShard, gen.ProcessOptions{}, Options{Domain: domain, Shards: shards}, index)
	if err != nil {
		t.Fatal(err)
	}
	return sub
}

// Init subscribes the shard to the node CoreEvent bus.
func TestShard_InitMonitorsCoreEvent(t *testing.T) {
	sub := spawnShardIndex(t, "grid", 8, 0)
	sub.ShouldMonitor().
		Target(gen.Event{Name: gen.CoreEvent}).
		Once().Assert()
}

// A bootstrap with a configured seed probes that seed's counterpart shard.
func TestShard_BootstrapProbesSeed(t *testing.T) {
	seed := gen.Atom("seed@localhost")
	options := Options{Domain: "grid", Shards: 8, Peers: []gen.Atom{seed}}

	sub, err := unit.Spawn(t, factoryShard, gen.ProcessOptions{}, options, 3)
	if err != nil {
		t.Fatal(err)
	}

	sub.Node().Network().Registrar().Resolver().OnResolveApplication(appName("grid")).Return()
	sub.SendMessage(sub.PID(), messageBootstrap{})

	want := messagePeerConnect{From: sub.PID(), Domain: "grid", NumShards: 8, Index: 3}
	sub.ShouldSend().
		To(gen.ProcessID{Node: seed, Name: shardName("grid", 3)}).
		Message(want).
		Once().Assert()
}

// An inbound probe for the same index is acked and the sender becomes a peer.
func TestShard_InboundConnectAcksAndAddsPeer(t *testing.T) {
	sub := spawnShardIndex(t, "grid", 8, 2)

	peerPID := gen.PID{Node: "peer@localhost", ID: 42, Creation: 1}
	sub.SendMessage(peerPID, messagePeerConnect{From: peerPID, Domain: "grid", NumShards: 8, Index: 2})

	sub.ShouldSend().
		To(peerPID).
		Message(messagePeerConnectAck{From: sub.PID(), Domain: "grid", NumShards: 8, Index: 2}).
		Once().Assert()

	b := sub.Behavior().(*shard)
	_, ok := b.peers["peer@localhost"]
	check.True(t, ok)
}

// An inbound probe from a different domain is ignored: no ack, no peer.
func TestShard_WrongDomainNoAck(t *testing.T) {
	sub := spawnShardIndex(t, "grid", 8, 0)

	peerPID := gen.PID{Node: "other@localhost", ID: 7, Creation: 1}
	sub.SendMessage(peerPID, messagePeerConnect{From: peerPID, Domain: "other_grid", NumShards: 8, Index: 0})

	sub.ShouldSend().To(peerPID).None().Assert()

	b := sub.Behavior().(*shard)
	check.Equal(t, 0, len(b.peers))
}

// An inbound probe for a different shard index is ignored.
func TestShard_WrongIndexNoAck(t *testing.T) {
	sub := spawnShardIndex(t, "grid", 8, 0)

	peerPID := gen.PID{Node: "peer@localhost", ID: 7, Creation: 1}
	sub.SendMessage(peerPID, messagePeerConnect{From: peerPID, Domain: "grid", NumShards: 8, Index: 5})

	sub.ShouldSend().To(peerPID).None().Assert()

	b := sub.Behavior().(*shard)
	check.Equal(t, 0, len(b.peers))
}

// confirm does not record a peer whose monitor fails.
func TestShard_ConfirmSkipsDeadPeer(t *testing.T) {
	sub := spawnShardIndex(t, "confirm_dead", 8, 0)
	deadPeer := gen.PID{Node: "peer@localhost", ID: 5, Creation: 1}

	sub.OnMonitor(deadPeer).Fail(gen.ErrProcessTerminated)
	sub.SendMessage(deadPeer, messagePeerConnectAck{From: deadPeer, Domain: "confirm_dead", NumShards: 8, Index: 0})

	b := sub.Behavior().(*shard)
	if _, ok := b.peers["peer@localhost"]; ok {
		t.Fatal("a dead peer must not be recorded")
	}
}

// Seed retry keeps retrying past maxSeedAttempts.
func TestShard_SeedRetryNeverGivesUp(t *testing.T) {
	seed := gen.Atom("seed@localhost")
	sub, err := unit.Spawn(t, factoryShard, gen.ProcessOptions{},
		Options{Domain: "seed_persist", Shards: 1, Peers: []gen.Atom{seed}}, 0)
	if err != nil {
		t.Fatal(err)
	}

	b := sub.Behavior().(*shard)
	b.pending[seed] = maxSeedAttempts + 5
	sub.SendMessage(sub.PID(), messageSeedRetry{Node: seed})

	sub.ShouldSendAfter().To(sub.PID()).Message(messageSeedRetry{Node: seed}).Once().Assert()
	if _, armed := b.pending[seed]; armed == false {
		t.Fatal("seed retry must stay armed past maxSeedAttempts")
	}
}
