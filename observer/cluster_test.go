package observer

import (
	"strings"
	"testing"
	"time"

	"ergo.services/ergo/gen"
	"ergo.services/ergo/testing/stage"
)

type lens struct {
	t     *testing.T
	s     *stage.Stage
	gate  *stage.Node
	store *clusterStore
}

func startLens(t *testing.T, options ClusterLensOptions, stageOptions ...stage.StageOptions) *lens {
	t.Helper()

	if options.WatchPeriod == 0 {
		options.WatchPeriod = 100 * time.Millisecond
	}
	if options.ReconcilePeriod == 0 {
		options.ReconcilePeriod = 100 * time.Millisecond
	}

	s := stage.New(t, stageOptions...)
	gate := s.StartNode("obs", stage.NodeOptions{EnableSystemApp: true})

	opts := gen.ProcessOptions{
		Env: map[gen.Env]any{envClusterLensOptions: options},
	}
	if _, err := gate.Native().SpawnRegister(clusterName, factory_cluster, opts); err != nil {
		t.Fatalf("spawn cluster: %s", err)
	}

	l := &lens{t: t, s: s, gate: gate}

	waitFor(t, 3*time.Second, "cluster store published", func() bool {
		v, exist := gate.Native().Env(envClusterStore)
		if exist == false {
			return false
		}
		store, ok := v.(*clusterStore)
		if ok == false {
			return false
		}
		l.store = store
		return true
	})

	return l
}

func (l *lens) startPeer(name string, opts ...stage.NodeOptions) *stage.Node {
	l.t.Helper()

	o := stage.NodeOptions{EnableSystemApp: true}
	if len(opts) > 0 {
		o = opts[0]
		o.EnableSystemApp = true
	}
	return l.s.StartNode(name, o)
}

func (l *lens) connect(from *stage.Node, to *stage.Node) {
	l.t.Helper()
	if _, err := from.Native().Network().GetNode(to.Name()); err != nil {
		l.t.Fatalf("connect %s -> %s: %s", from.Name(), to.Name(), err)
	}
}

func (l *lens) member(node gen.Atom) (*memberState, bool) {
	return l.store.member(node)
}

func (l *lens) status(node gen.Atom) gen.Atom {
	member, found := l.store.member(node)
	if found == false {
		return ""
	}
	return member.Status
}

func (l *lens) clusterState(item ...string) map[string]string {
	l.t.Helper()

	pid, err := l.gate.Native().ProcessPID(clusterName)
	if err != nil {
		l.t.Fatalf("cluster process: %s", err)
	}
	state, err := l.gate.Native().Inspect(pid, item...)
	if err != nil {
		l.t.Fatalf("inspect cluster: %s", err)
	}
	return state
}

func (l *lens) watcherState(name gen.Atom, item ...string) map[string]string {
	l.t.Helper()

	pid, err := l.gate.Native().ProcessPID(name)
	if err != nil {
		return map[string]string{}
	}
	state, err := l.gate.Native().Inspect(pid, item...)
	if err != nil {
		return map[string]string{}
	}
	return state
}

func (l *lens) info() ClusterInfo {
	l.t.Helper()

	result, err := l.gate.Native().Call(gen.ProcessID{Name: clusterName, Node: l.gate.Name()}, RequestClusterInfo{})
	if err != nil {
		l.t.Fatalf("cluster info: %s", err)
	}
	return result.(ClusterInfo)
}

func (l *lens) members() []gen.Atom {
	var nodes []gen.Atom
	l.store.members.Range(func(k, _ any) bool {
		nodes = append(nodes, k.(gen.Atom))
		return true
	})
	return nodes
}

func waitFor(t *testing.T, timeout time.Duration, what string, condition func() bool) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for: %s", what)
}

func TestClusterSnapshotsKeepArriving(t *testing.T) {
	l := startLens(t, ClusterLensOptions{})

	peer := l.startPeer("peer")
	l.connect(l.gate, peer)

	waitFor(t, 5*time.Second, "peer becomes reachable", func() bool {
		return l.status(peer.Name()) == statusReachable
	})

	first, found := l.store.snapshot(peer.Name())
	if found == false {
		t.Fatal("no snapshot for a reachable node")
	}

	waitFor(t, 10*time.Second, "a newer snapshot replaces the first", func() bool {
		current, ok := l.store.snapshot(peer.Name())
		return ok && current.At.After(first.At)
	})

	previous, _ := l.store.snapshot(peer.Name())
	waitFor(t, 10*time.Second, "and keeps being replaced", func() bool {
		current, ok := l.store.snapshot(peer.Name())
		return ok && current.At.After(previous.At)
	})
}

func TestClusterWatchesConnectedNodes(t *testing.T) {
	l := startLens(t, ClusterLensOptions{})

	peer := l.startPeer("peer")
	l.connect(l.gate, peer)

	waitFor(t, 5*time.Second, "peer becomes reachable", func() bool {
		return l.status(peer.Name()) == statusReachable
	})

	snapshot, found := l.store.snapshot(peer.Name())
	if found == false {
		t.Fatal("no snapshot for a reachable node")
	}
	if snapshot.Info.Name != peer.Name() {
		t.Errorf("snapshot belongs to %s, expected %s", snapshot.Info.Name, peer.Name())
	}
	if snapshot.Info.ProcessesTotal == 0 {
		t.Error("snapshot carries no process counters")
	}
	if snapshot.At.IsZero() {
		t.Error("a snapshot must be stamped, or its age cannot be told")
	}

	waitFor(t, 3*time.Second, "self becomes reachable", func() bool {
		return l.status(l.gate.Name()) == statusReachable
	})
}

func TestClusterSnapshotCarriesGroupingAndTraffic(t *testing.T) {
	l := startLens(t, ClusterLensOptions{})

	peer := l.startPeer("peer")
	l.connect(l.gate, peer)

	waitFor(t, 5*time.Second, "peer becomes reachable", func() bool {
		return l.status(peer.Name()) == statusReachable
	})

	snapshot, found := l.store.snapshot(peer.Name())
	if found == false {
		t.Fatal("no snapshot for a reachable node")
	}

	if len(snapshot.Info.Applications) == 0 {
		t.Error("no application names, the cluster map has nothing to group by")
	}
	if int64(len(snapshot.Info.Applications)) != snapshot.Info.ApplicationsTotal {
		t.Errorf("%d application names against a total of %d",
			len(snapshot.Info.Applications), snapshot.Info.ApplicationsTotal)
	}

	var edge *gen.RemoteNodeShortInfo
	for i, p := range snapshot.Info.Peers {
		if p.Node == l.gate.Name() {
			edge = &snapshot.Info.Peers[i]
		}
	}
	if edge == nil {
		t.Fatalf("the node does not list the observer among %v", snapshot.Info.Peers)
	}
	if edge.MessagesIn == 0 || edge.MessagesOut == 0 {
		t.Errorf("message counters are %d in / %d out, expected traffic in both directions",
			edge.MessagesIn, edge.MessagesOut)
	}
	if edge.BytesIn == 0 || edge.BytesOut == 0 {
		t.Errorf("byte counters are %d in / %d out, expected traffic in both directions",
			edge.BytesIn, edge.BytesOut)
	}
	if edge.ConnectionUptime < 0 {
		t.Errorf("connection uptime is %d", edge.ConnectionUptime)
	}
}

func TestClusterTransitiveDiscovery(t *testing.T) {
	l := startLens(t, ClusterLensOptions{})

	middle := l.startPeer("middle")
	far := l.startPeer("far")

	l.connect(middle, far)
	l.connect(l.gate, middle)

	waitFor(t, 5*time.Second, "middle becomes reachable", func() bool {
		return l.status(middle.Name()) == statusReachable
	})

	waitFor(t, 10*time.Second, "far discovered through the peer list of middle", func() bool {
		return l.status(far.Name()) == statusReachable
	})

	member, _ := l.member(far.Name())
	if member.Registrar {
		t.Error("far must not be marked as listed by the registrar")
	}
	if member.Refs[middle.Name()] == false {
		t.Errorf("far must be referenced by middle, refs: %v", member.Refs)
	}
}

func TestClusterKeepsUnreachableWhileReferenced(t *testing.T) {
	l := startLens(t, ClusterLensOptions{GracePeriod: time.Hour})

	middle := l.startPeer("middle")
	hidden := l.startPeer("hidden", stage.NodeOptions{Mode: gen.NetworkModeHidden})

	l.connect(hidden, middle)
	l.connect(l.gate, middle)

	waitFor(t, 5*time.Second, "middle becomes reachable", func() bool {
		return l.status(middle.Name()) == statusReachable
	})

	waitFor(t, 10*time.Second, "hidden node appears as unreachable", func() bool {
		return l.status(hidden.Name()) == statusUnreachable
	})

	member, _ := l.member(hidden.Name())
	if member.Refs[middle.Name()] == false {
		t.Errorf("hidden node must be referenced by middle, refs: %v", member.Refs)
	}
	if member.Reason == "" {
		t.Error("an unreachable node must carry the reason")
	}

	if _, found := l.store.snapshot(hidden.Name()); found {
		t.Error("an unreachable node must not carry a snapshot")
	}

	time.Sleep(2 * time.Second)
	if l.status(hidden.Name()) != statusUnreachable {
		t.Errorf("referenced node left the map, status now %q", l.status(hidden.Name()))
	}
}

func TestClusterDropsUnreferencedNode(t *testing.T) {
	l := startLens(t, ClusterLensOptions{GracePeriod: time.Second})

	peer := l.startPeer("peer")
	l.connect(l.gate, peer)

	waitFor(t, 5*time.Second, "peer becomes reachable", func() bool {
		return l.status(peer.Name()) == statusReachable
	})

	peer.Native().Stop()

	waitFor(t, 10*time.Second, "stopped peer leaves the map", func() bool {
		_, found := l.member(peer.Name())
		return found == false
	})

	if _, found := l.store.snapshot(peer.Name()); found {
		t.Error("snapshot outlived the member record")
	}
}

func TestClusterDropsWithoutReconcile(t *testing.T) {
	l := startLens(t, ClusterLensOptions{
		GracePeriod:     time.Second,
		ReconcilePeriod: time.Hour,
	})

	peer := l.startPeer("peer")
	l.connect(l.gate, peer)

	waitFor(t, 5*time.Second, "peer becomes reachable", func() bool {
		return l.status(peer.Name()) == statusReachable
	})

	peer.Native().Stop()

	waitFor(t, 15*time.Second, "stopped peer leaves the map on the event path alone", func() bool {
		_, found := l.member(peer.Name())
		return found == false
	})
}

func TestClusterWatchLimit(t *testing.T) {
	l := startLens(t, ClusterLensOptions{WatchLimit: 1})

	first := l.startPeer("first")
	second := l.startPeer("second")
	l.connect(l.gate, first)
	l.connect(l.gate, second)

	waitFor(t, 5*time.Second, "both nodes on the map", func() bool {
		found := 0
		for _, node := range l.members() {
			if node == first.Name() || node == second.Name() {
				found++
			}
		}
		return found == 2
	})

	waitFor(t, 5*time.Second, "watch limit reported", func() bool {
		watched := 0
		l.store.members.Range(func(_, v any) bool {
			if v.(*memberState).Watched {
				watched++
			}
			return true
		})
		return watched == 1
	})
}

func TestClusterCentralRegistrar(t *testing.T) {
	l := startLens(t, ClusterLensOptions{}, stage.StageOptions{RegistrarFull: true})

	peer := l.startPeer("peer")

	waitFor(t, 5*time.Second, "peer listed by the registrar appears on the map", func() bool {
		member, found := l.member(peer.Name())
		return found && member.Registrar
	})

	waitFor(t, 5*time.Second, "peer becomes reachable", func() bool {
		return l.status(peer.Name()) == statusReachable
	})
}

func TestClusterCompleteFlag(t *testing.T) {
	mesh := startLens(t, ClusterLensOptions{})

	waitFor(t, 3*time.Second, "mesh mode reports an incomplete map", func() bool {
		result, err := mesh.gate.Native().Call(
			gen.ProcessID{Name: clusterName, Node: mesh.gate.Name()},
			RequestClusterInfo{})
		if err != nil {
			return false
		}
		info := result.(ClusterInfo)
		return info.Complete == false && info.Note != ""
	})

	central := startLens(t, ClusterLensOptions{}, stage.StageOptions{RegistrarFull: true})

	waitFor(t, 3*time.Second, "central mode reports a complete map", func() bool {
		result, err := central.gate.Native().Call(
			gen.ProcessID{Name: clusterName, Node: central.gate.Name()},
			RequestClusterInfo{})
		if err != nil {
			return false
		}
		info := result.(ClusterInfo)
		return info.Complete && info.Note == ""
	})
}

func newTestCluster(grace time.Duration) *cluster {
	return &cluster{
		store:   newClusterStore(),
		options: ClusterLensOptions{GracePeriod: grace}.withDefaults(),
	}
}

func (c *cluster) put(node gen.Atom, status gen.Atom, since int64, registrar bool, refs ...gen.Atom) *memberState {
	member := &memberState{
		Node:      node,
		Status:    status,
		Since:     since,
		Registrar: registrar,
		Refs:      make(map[gen.Atom]bool),
	}
	for _, by := range refs {
		member.Refs[by] = true
	}
	c.store.members.Store(node, member)
	return member
}

func TestClusterAliveReachable(t *testing.T) {
	c := newTestCluster(time.Minute)
	now := time.Now().Unix()

	member := c.put("n@h", statusReachable, now, false)
	if c.alive(member, now) == false {
		t.Error("a reachable node must be alive")
	}
}

func TestClusterAliveByRegistrar(t *testing.T) {
	c := newTestCluster(time.Minute)
	now := time.Now().Unix()

	member := c.put("n@h", statusUnreachable, now, true)
	if c.alive(member, now) == false {
		t.Error("a node listed by the registrar must be alive")
	}

	member = c.put("n@h", statusUnreachable, now, false)
	if c.alive(member, now) {
		t.Error("an unreachable, unlisted, unreferenced node must not be alive")
	}
}

func TestClusterAliveByReferenceFromReachable(t *testing.T) {
	c := newTestCluster(time.Minute)
	now := time.Now().Unix()

	c.put("peer@h", statusReachable, now, false)
	member := c.put("n@h", statusUnreachable, now, false, "peer@h")

	if c.alive(member, now) == false {
		t.Error("a node referenced by a reachable node must be alive")
	}
}

func TestClusterAliveByReferenceWithinGrace(t *testing.T) {
	c := newTestCluster(30 * time.Second)
	now := time.Now().Unix()

	c.put("peer@h", statusUnreachable, now-10, false)
	member := c.put("n@h", statusUnreachable, now-10, false, "peer@h")

	if c.alive(member, now) == false {
		t.Error("within the grace period the reference must still count")
	}

	c.put("peer@h", statusUnreachable, now-31, false)
	if c.alive(member, now) {
		t.Error("past the grace period the reference must expire")
	}
}

func TestClusterAliveIgnoresUnknownReferrer(t *testing.T) {
	c := newTestCluster(time.Minute)
	now := time.Now().Unix()

	member := c.put("n@h", statusUnreachable, now, false, "ghost@h")

	if c.alive(member, now) {
		t.Error("a reference from a node that left the map must not count")
	}
}

func TestClusterRecoversAfterDisconnect(t *testing.T) {
	l := startLens(t, ClusterLensOptions{}, stage.StageOptions{RegistrarFull: true})

	peer := l.startPeer("peer")
	l.connect(l.gate, peer)

	waitFor(t, 5*time.Second, "peer becomes reachable", func() bool {
		return l.status(peer.Name()) == statusReachable
	})

	before, found := l.store.snapshot(peer.Name())
	if found == false {
		t.Fatal("no snapshot before the disconnect")
	}

	remote, err := l.gate.Native().Network().Node(peer.Name())
	if err != nil {
		t.Fatalf("no connection to %s: %s", peer.Name(), err)
	}
	remote.Disconnect()

	waitFor(t, 5*time.Second, "peer goes unreachable", func() bool {
		return l.status(peer.Name()) == statusUnreachable
	})

	stale, found := l.store.snapshot(peer.Name())
	if found == false {
		t.Fatal("the last reading must survive the disconnect")
	}
	if stale != before {
		t.Error("the reading changed while the node was unreachable")
	}

	member, _ := l.member(peer.Name())
	if member.Registrar == false {
		t.Error("the registrar still lists it, so the flag must stay")
	}
	if member.LastSeen == 0 {
		t.Error("last seen must survive the disconnect")
	}
	if len(member.Peers) == 0 {
		t.Error("the last known peer list must survive the disconnect")
	}

	info := l.info()
	var reading *ClusterNodeInfo
	for i := range info.Nodes {
		if info.Nodes[i].Info.Name == peer.Name() {
			reading = &info.Nodes[i]
		}
	}
	if reading == nil {
		t.Fatalf("the node is missing from the map: %+v", info)
	}
	if reading.Status != statusUnreachable {
		t.Errorf("the reading is reported as %q", reading.Status)
	}
	if reading.Reason == "" {
		t.Error("a stale reading must carry the reason the node went")
	}

	waitFor(t, 15*time.Second, "peer recovers", func() bool {
		return l.status(peer.Name()) == statusReachable
	})

	after, found := l.store.snapshot(peer.Name())
	if found == false {
		t.Fatal("no snapshot after the recovery")
	}
	if after == before {
		t.Error("the snapshot must be replaced after reconnecting, not reused")
	}
}

func TestClusterInspect(t *testing.T) {
	l := startLens(t, ClusterLensOptions{}, stage.StageOptions{RegistrarFull: true})

	peer := l.startPeer("peer")
	l.connect(l.gate, peer)

	waitFor(t, 5*time.Second, "peer becomes reachable", func() bool {
		return l.status(peer.Name()) == statusReachable
	})

	state := l.clusterState()
	if state["members"] != "2" {
		t.Errorf("the gate and the peer are on the map, yet members is %q", state["members"])
	}
	if strings.HasPrefix(state["watchers"], "2/") == false {
		t.Errorf("watchers came out as %q", state["watchers"])
	}
	if state["registrar_listing"] != "true" || state["registrar_events"] != "true" {
		t.Errorf("the full registrar is reported as %q / %q",
			state["registrar_listing"], state["registrar_events"])
	}
	if state["last_refresh"] == "never" {
		t.Error("the membership was read, so the refresh is stamped")
	}
	if state["items"] != "help" {
		t.Error("the summary must say that a vocabulary exists")
	}
	if strings.Contains(state["dropped"], "message_unexpected") {
		t.Errorf("a message meant for the lens was dropped: %q", state["dropped"])
	}
	for key, value := range state {
		if strings.Contains(value, string(peer.Name())) {
			t.Errorf("%q names a node, so the answer grows with the cluster", key)
		}
	}

	query := l.clusterState("help", "members", "watchers", "queue",
		"node "+string(peer.Name()), "node nobody@nowhere", "nonsense")

	if strings.Contains(query["help"], "node <name>") == false {
		t.Errorf("help does not name the queries: %q", query["help"])
	}
	if strings.Contains(query["members"], "showing 2 of 2") == false ||
		strings.Contains(query["members"], string(peer.Name())+"=reachable") == false {
		t.Errorf("members came out as %q", query["members"])
	}
	if strings.Contains(query["watchers"], string(peer.Name())) == false {
		t.Errorf("watchers came out as %q", query["watchers"])
	}
	if query["queue"] != "0: none" {
		t.Errorf("an empty queue came out as %q", query["queue"])
	}
	if strings.Contains(query["node "+string(peer.Name())], "status=reachable") == false ||
		strings.Contains(query["node "+string(peer.Name())], "watcher=") == false {
		t.Errorf("the node query came out as %q", query["node "+string(peer.Name())])
	}
	if query["node nobody@nowhere"] != "<not found>" {
		t.Errorf("an unknown node came out as %q", query["node nobody@nowhere"])
	}
	if query["nonsense"] != "<unknown item>" {
		t.Errorf("an unknown item came out as %q", query["nonsense"])
	}
}

func TestClusterStaleReadingExpires(t *testing.T) {
	l := startLens(t, ClusterLensOptions{LastReadingPeriod: 300 * time.Millisecond},
		stage.StageOptions{RegistrarFull: true})

	peer := l.startPeer("peer")
	l.connect(l.gate, peer)

	waitFor(t, 5*time.Second, "peer becomes reachable", func() bool {
		return l.status(peer.Name()) == statusReachable
	})

	remote, err := l.gate.Native().Network().Node(peer.Name())
	if err != nil {
		t.Fatalf("no connection to %s: %s", peer.Name(), err)
	}
	remote.Disconnect()

	waitFor(t, 5*time.Second, "the stale reading expires", func() bool {
		_, found := l.store.snapshot(peer.Name())
		return found == false
	})

	if _, found := l.member(peer.Name()); found == false {
		t.Error("the registrar lists it, so the node must stay on the map without data")
	}
}

func TestClusterWakesWatcherOfReturningNode(t *testing.T) {
	l := startLens(t, ClusterLensOptions{}, stage.StageOptions{RegistrarFull: true})

	peer := l.startPeer("peer")
	l.connect(l.gate, peer)

	waitFor(t, 5*time.Second, "peer becomes reachable", func() bool {
		return l.status(peer.Name()) == statusReachable
	})

	name := watcherName(peer.Name())
	before := l.watcherState(name)
	if before["node"] != string(peer.Name()) {
		t.Fatalf("the watcher does not report its node: %v", before)
	}

	remote, err := l.gate.Native().Network().Node(peer.Name())
	if err != nil {
		t.Fatalf("no connection to %s: %s", peer.Name(), err)
	}
	remote.Disconnect()

	waitFor(t, 5*time.Second, "the watcher starts retrying", func() bool {
		return l.watcherState(name)["state"] == "retrying"
	})

	waitFor(t, 15*time.Second, "the watcher is woken and watches again", func() bool {
		state := l.watcherState(name)
		return state["state"] == "watching" && state["wakes"] != "0"
	})
}

func TestClusterWatchLimitFreesSlot(t *testing.T) {
	l := startLens(t, ClusterLensOptions{WatchLimit: 2, GracePeriod: 100 * time.Millisecond})

	first := l.startPeer("first")
	l.connect(l.gate, first)

	waitFor(t, 5*time.Second, "first is watched", func() bool {
		return l.status(first.Name()) == statusReachable
	})

	second := l.startPeer("second")
	l.connect(l.gate, second)

	waitFor(t, 5*time.Second, "second is on the map but unwatched", func() bool {
		member, found := l.member(second.Name())
		return found && member.Watched == false
	})

	waitFor(t, 15*time.Second, "the map keeps reporting itself incomplete", func() bool {
		result, err := l.gate.Native().Call(
			gen.ProcessID{Name: clusterName, Node: l.gate.Name()},
			RequestClusterInfo{})
		if err != nil {
			return false
		}
		info := result.(ClusterInfo)
		return info.Complete == false && info.Note != ""
	})

	first.Native().Stop()

	waitFor(t, 10*time.Second, "second gets the freed slot", func() bool {
		return l.status(second.Name()) == statusReachable
	})
}

func TestClusterInfoRequest(t *testing.T) {
	l := startLens(t, ClusterLensOptions{WatchLimit: 8}, stage.StageOptions{RegistrarFull: true})

	peer := l.startPeer("peer")
	hidden := l.startPeer("hidden", stage.NodeOptions{Mode: gen.NetworkModeHidden})
	l.connect(hidden, peer)
	l.connect(l.gate, peer)

	waitFor(t, 5*time.Second, "both the peer and the hidden node are on the map", func() bool {
		return l.status(peer.Name()) == statusReachable &&
			l.status(hidden.Name()) == statusUnreachable
	})

	result, err := l.gate.Native().Call(
		gen.ProcessID{Name: clusterName, Node: l.gate.Name()}, RequestClusterInfo{})
	if err != nil {
		t.Fatalf("cluster info: %s", err)
	}

	info, ok := result.(ClusterInfo)
	if ok == false {
		t.Fatalf("unexpected response %T", result)
	}

	if len(info.Nodes) < 2 {
		t.Fatalf("expected the gate and its peer among reachable, got %d", len(info.Nodes))
	}
	for _, node := range info.Nodes {
		if node.Info.ProcessesTotal == 0 {
			t.Errorf("node %s carries no counters", node.Info.Name)
		}
		if node.Age < 0 {
			t.Errorf("node %s reports a negative age %d", node.Info.Name, node.Age)
		}
	}

	if len(info.Offline) != 1 {
		t.Fatalf("expected exactly one offline node, got %d", len(info.Offline))
	}
	if info.Offline[0].Node != hidden.Name() {
		t.Errorf("offline node is %s, expected %s", info.Offline[0].Node, hidden.Name())
	}
	edge := false
	for _, node := range info.Nodes {
		for _, peer := range node.Info.Peers {
			if peer.Node == hidden.Name() {
				edge = true
			}
		}
	}
	if edge == false {
		t.Error("no reachable node lists the hidden one, its edge would vanish")
	}

	if info.Complete == false {
		t.Errorf("map must be complete with a listing registrar, note: %q", info.Note)
	}
	if info.Watched == 0 || info.Limit != 8 {
		t.Errorf("watched=%d limit=%d, expected a non-zero count and the configured limit",
			info.Watched, info.Limit)
	}

	for i := 1; i < len(info.Nodes); i++ {
		if info.Nodes[i-1].Info.Name > info.Nodes[i].Info.Name {
			t.Error("nodes are not sorted")
		}
	}
}
