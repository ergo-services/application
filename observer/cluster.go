package observer

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"ergo.services/ergo/act"
	"ergo.services/ergo/gen"
)

func factory_cluster() gen.ProcessBehavior {
	return &cluster{}
}

type cluster struct {
	act.Actor

	options ClusterLensOptions
	store   *clusterStore

	enumerable bool
	eventful   bool

	watchers map[gen.Atom]gen.PID
	watched  map[gen.PID]gen.Atom // the same pairing the other way, for a watcher's death
	pending  map[gen.Atom]bool    // watchers that have not reported their first attempt

	queue  []gen.Atom
	queued map[gen.Atom]bool

	note string

	members int

	discovered int64
	evicted    int64
	started    int64
	deaths     int64
	stale      int64
	unwatched  int64
	dropped    map[string]int64

	retriesArmed int
	graceArmed   int

	lastRefresh   time.Time
	lastReconcile time.Time
	lastEviction  time.Time
}

func (c *cluster) Init(args ...any) error {
	c.Log().SetLogger("default")

	c.options = ClusterLensOptions{}
	if v, exist := c.Env(envClusterLensOptions); exist {
		if o, ok := v.(ClusterLensOptions); ok {
			c.options = o
		}
	}
	c.options = c.options.withDefaults()

	c.store = newClusterStore()
	c.watchers = make(map[gen.Atom]gen.PID)
	c.watched = make(map[gen.PID]gen.Atom)
	c.pending = make(map[gen.Atom]bool)
	c.queued = make(map[gen.Atom]bool)
	c.dropped = make(map[string]int64)

	c.Node().SetEnv(envClusterStore, c.store)

	if _, err := c.SendAfter(c.PID(), messageRun{}, 0); err != nil {
		return err
	}
	if _, err := c.SendEvery(c.PID(), messageReconcile{}, c.options.ReconcilePeriod); err != nil {
		return err
	}
	if _, err := c.SendEvery(c.PID(), messageRefresh{}, clusterRefreshPeriod); err != nil {
		return err
	}

	c.Log().Info("cluster lens started: watch limit %d, concurrency %d",
		c.options.WatchLimit, c.options.Concurrency)
	return nil
}

func (c *cluster) HandleMessage(from gen.PID, message any) error {
	switch m := message.(type) {
	case messageRun:
		c.subscribeRegistrar()
		c.refresh()

	case messageRefresh:
		c.refresh()

	case messageReconcile:
		c.reconcile()

	case messageGraceExpired:
		if c.graceArmed > 0 {
			c.graceArmed--
		}
		if member, found := c.store.member(m.Node); found {
			c.recheck(member.Peers...)
		}

	case messageWatcherReachable:
		c.onReachable(m.Node)

	case messageWatcherUnreachable:
		c.onUnreachable(m.Node, m.Reason)

	case messageWatcherPeers:
		c.onPeers(m.Node, m.Peers)

	case messageRetryNode:
		if c.retriesArmed > 0 {
			c.retriesArmed--
		}
		c.startWatcher(m.Node)

	case gen.MessageDownPID:
		c.onWatcherDown(m.PID)

	default:
		c.dropped["message_unexpected"]++
		c.Log().Debug("cluster: unhandled message %T from %s", message, from)
	}
	return nil
}

func (c *cluster) HandleEvent(message gen.MessageEvent) error {
	switch m := message.Message.(type) {
	case gen.MessageRegistrarNodeJoined:
		c.discover(m.Name, true)
		c.wake(m.Name)

	case gen.MessageRegistrarNodeLeft:
		c.onRegistrarLeft(m.Name)

	default:
		c.dropped["registrar_event_ignored"]++
	}
	return nil
}

func (c *cluster) HandleCall(from gen.PID, ref gen.Ref, request any) (any, error) {
	switch request.(type) {
	case RequestClusterInfo:
		return c.info(), nil
	}
	c.dropped["request_unsupported"]++
	return gen.ErrUnsupported, nil
}

const clusterInspectHelp = "summary keys: self, members, watchers, connecting, queue, registrar_listing, " +
	"registrar_events, incomplete, discovered_total, evicted_total, watchers_started, watcher_deaths, " +
	"reconcile_stale, reconcile_unwatched, retries_armed, grace_armed, last_refresh, last_reconcile, " +
	"last_eviction, dropped; " +
	"queries: members, watchers, connecting, queue, dropped, node <name>"

func (c *cluster) HandleInspect(from gen.PID, item ...string) map[string]string {
	if len(item) == 0 {
		return c.inspectSummary()
	}

	result := map[string]string{}
	for _, q := range item {
		switch {
		case q == "help":
			result["help"] = clusterInspectHelp

		case q == "members":
			result[q] = c.inspectMembers()

		case q == "watchers":
			names := make([]string, 0, len(c.watchers))
			for node := range c.watchers {
				names = append(names, string(node))
			}
			result[q] = fmt.Sprintf("%d: %s", len(names), inspectList(names))

		case q == "connecting":
			names := make([]string, 0, len(c.pending))
			for node := range c.pending {
				names = append(names, string(node))
			}
			result[q] = fmt.Sprintf("%d: %s", len(names), inspectList(names))

		case q == "queue":
			names := make([]string, 0, len(c.queue))
			for _, node := range c.queue {
				names = append(names, string(node))
			}
			result[q] = fmt.Sprintf("%d: %s", len(names), inspectListOrdered(names))

		case q == "dropped":
			result[q] = inspectCounters(c.dropped)

		case strings.HasPrefix(q, "node "):
			result[q] = c.inspectNode(gen.Atom(strings.TrimPrefix(q, "node ")))

		default:
			result[q] = "<unknown item>"
		}
	}
	return result
}

func (c *cluster) inspectSummary() map[string]string {
	return map[string]string{
		"self":                string(c.Node().Name()),
		"members":             fmt.Sprintf("%d", c.members),
		"watchers":            fmt.Sprintf("%d/%d", len(c.watchers), c.options.WatchLimit),
		"connecting":          fmt.Sprintf("%d/%d", len(c.pending), c.options.Concurrency),
		"queue":               fmt.Sprintf("%d", len(c.queue)),
		"registrar_listing":   fmt.Sprintf("%v", c.enumerable),
		"registrar_events":    fmt.Sprintf("%v", c.eventful),
		"incomplete":          c.incomplete(),
		"discovered_total":    fmt.Sprintf("%d", c.discovered),
		"evicted_total":       fmt.Sprintf("%d", c.evicted),
		"watchers_started":    fmt.Sprintf("%d", c.started),
		"watcher_deaths":      fmt.Sprintf("%d", c.deaths),
		"reconcile_stale":     fmt.Sprintf("%d", c.stale),
		"reconcile_unwatched": fmt.Sprintf("%d", c.unwatched),
		"retries_armed":       fmt.Sprintf("%d", c.retriesArmed),
		"grace_armed":         fmt.Sprintf("%d", c.graceArmed),
		"last_refresh":        inspectAge(c.lastRefresh),
		"last_reconcile":      inspectAge(c.lastReconcile),
		"last_eviction":       inspectAge(c.lastEviction),
		"dropped":             inspectCounters(c.dropped),
		"items":               "help",
	}
}

func (c *cluster) inspectMembers() string {
	pairs := make([]string, 0, inspectListCap)
	c.store.members.Range(func(k, v any) bool {
		pairs = append(pairs, fmt.Sprintf("%s=%s", string(k.(gen.Atom)), string(v.(*memberState).Status)))
		return len(pairs) < inspectListCap
	})

	sort.Strings(pairs)
	return fmt.Sprintf("showing %d of %d: %s", len(pairs), c.members, strings.Join(pairs, ", "))
}

func (c *cluster) inspectNode(node gen.Atom) string {
	member, found := c.store.member(node)
	if found == false {
		return "<not found>"
	}

	refs := make([]string, 0, len(member.Refs))
	for by := range member.Refs {
		refs = append(refs, string(by))
	}

	snapshot := "none"
	if reading, exist := c.store.snapshot(node); exist {
		snapshot = inspectAge(reading.At)
	}

	watcher := "none"
	if pid, exist := c.watchers[node]; exist {
		watcher = pid.String()
	}

	return fmt.Sprintf("status=%s since=%d last_seen=%d registrar=%v watcher=%s snapshot=%s "+
		"queued=%v connecting=%v peers=%d refs=%d [%s] reason=%q",
		string(member.Status), member.Since, member.LastSeen, member.Registrar, watcher, snapshot,
		c.queued[node], c.pending[node], len(member.Peers), len(refs), inspectList(refs), member.Reason)
}

func (c *cluster) incomplete() string {
	if c.note != "" {
		return c.note
	}
	if len(c.watchers) >= c.options.WatchLimit && len(c.queue) > 0 {
		return fmt.Sprintf("watch limit %d reached: %d node(s) on the map have no data",
			c.options.WatchLimit, len(c.queue))
	}
	return ""
}

func (c *cluster) Terminate(reason error) {
	c.Log().Debug("cluster lens terminated: %s", reason)
}

func (c *cluster) subscribeRegistrar() {
	registrar, err := c.Node().Network().Registrar()
	if err != nil {
		c.note = fmt.Sprintf("no registrar on this node: %s", err)
		return
	}

	event, err := registrar.Event()
	if err != nil {
		c.Log().Debug("registrar has no event: %s", err)
		return
	}
	if _, err := c.MonitorEvent(event); err != nil {
		c.Log().Error("cannot monitor registrar event: %s", err)
		return
	}
	c.eventful = true
}

func (c *cluster) refresh() {
	c.lastRefresh = time.Now()

	self := c.Node().Name()
	c.discover(self, false)

	for _, node := range c.Node().Network().Nodes() {
		c.discover(node, false)
		if member, found := c.store.member(node); found && member.Status != statusReachable {
			c.wake(node)
		}
	}

	registrar, err := c.Node().Network().Registrar()
	if err != nil {
		c.note = fmt.Sprintf("no registrar on this node: %s", err)
		return
	}

	nodes, err := registrar.Nodes()
	if err != nil {
		c.enumerable = false
		c.note = fmt.Sprintf("this registrar cannot list nodes: %s", err)
		return
	}

	c.enumerable = true
	c.note = ""

	listed := make(map[gen.Atom]bool, len(nodes))
	for _, node := range nodes {
		listed[node] = true
		c.discover(node, true)
	}

	var dropped []gen.Atom
	c.store.members.Range(func(k, v any) bool {
		node := k.(gen.Atom)
		member := v.(*memberState)
		if member.Registrar && listed[node] == false && node != self {
			updated := member.clone()
			updated.Registrar = false
			c.store.members.Store(node, updated)
			dropped = append(dropped, node)
		}
		return true
	})
	c.recheck(dropped...)
}

func watcherName(node gen.Atom) gen.Atom {
	return gen.Atom(watcherPrefix + node)
}

func (c *cluster) wake(node gen.Atom) {
	if _, found := c.store.member(node); found == false {
		return
	}
	if err := c.Send(watcherName(node), messageWatcherWake{}); err == nil {
		return
	}
	c.startWatcher(node)
}

func (c *cluster) discover(node gen.Atom, fromRegistrar bool) {
	member, found := c.store.member(node)
	if found {
		if fromRegistrar && member.Registrar == false {
			updated := member.clone()
			updated.Registrar = true
			c.store.members.Store(node, updated)
		}
		if _, exist := c.watchers[node]; exist == false && c.queued[node] == false {
			c.startWatcher(node)
		}
		return
	}

	c.store.members.Store(node, &memberState{
		Node:      node,
		Status:    statusDiscovered,
		Since:     time.Now().Unix(),
		Registrar: fromRegistrar,
		Refs:      make(map[gen.Atom]bool),
	})
	c.members++
	c.discovered++

	c.startWatcher(node)
}

func (c *cluster) startWatcher(node gen.Atom) {
	if _, exist := c.watchers[node]; exist {
		return
	}

	member, found := c.store.member(node)
	if found == false {
		return
	}

	if len(c.watchers) >= c.options.WatchLimit {
		c.enqueue(node)
		return
	}

	if len(c.pending) >= c.options.Concurrency {
		c.enqueue(node)
		return
	}

	delete(c.queued, node)

	args := watcherArgs{
		node:   node,
		store:  c.store,
		period: c.options.WatchPeriod,
		keep:   c.options.LastReadingPeriod,
	}
	pid, err := c.SpawnRegister(watcherName(node), factory_watcher, gen.ProcessOptions{LinkParent: true}, args)
	if err != nil {
		c.dropped["watcher_spawn_failed"]++
		c.Log().Error("cannot spawn watcher for %s: %s", node, err)
		c.SendAfter(c.PID(), messageRetryNode{Node: node}, retryAfterWatcherDown)
		c.retriesArmed++
		return
	}
	c.started++

	if err := c.MonitorPID(pid); err != nil {
		c.dropped["watcher_monitor_failed"]++
		c.Log().Error("cannot monitor watcher for %s: %s", node, err)
	}

	c.watchers[node] = pid
	c.watched[pid] = node
	c.pending[node] = true

	updated := member.clone()
	updated.Watched = true
	c.store.members.Store(node, updated)
}

func (c *cluster) enqueue(node gen.Atom) {
	if c.queued[node] {
		return
	}
	c.queued[node] = true
	c.queue = append(c.queue, node)
}

func (c *cluster) drainQueue() {
	for len(c.queue) > 0 {
		if len(c.pending) >= c.options.Concurrency {
			return
		}
		if len(c.watchers) >= c.options.WatchLimit {
			return
		}

		node := c.queue[0]
		c.queue = c.queue[1:]
		delete(c.queued, node)

		if _, found := c.store.member(node); found == false {
			continue
		}
		c.startWatcher(node)
	}
}

func (c *cluster) onReachable(node gen.Atom) {
	delete(c.pending, node)
	c.drainQueue()

	member, found := c.store.member(node)
	if found == false {
		return
	}

	now := time.Now().Unix()
	updated := member.clone()
	updated.LastSeen = now
	updated.Reason = ""
	if updated.Status != statusReachable {
		updated.Status = statusReachable
		updated.Since = now
	}
	c.store.members.Store(node, updated)
}

func (c *cluster) onUnreachable(node gen.Atom, reason string) {
	delete(c.pending, node)
	c.drainQueue()

	member, found := c.store.member(node)
	if found == false {
		return
	}

	updated := member.clone()
	updated.Reason = reason
	if updated.Status != statusUnreachable {
		updated.Status = statusUnreachable
		updated.Since = time.Now().Unix()
		c.SendAfter(c.PID(), messageGraceExpired{Node: node}, c.options.GracePeriod)
		c.graceArmed++
	}
	c.store.members.Store(node, updated)

	c.recheck(node)
}

func (c *cluster) onPeers(node gen.Atom, peers []gen.Atom) {
	member, found := c.store.member(node)
	if found == false {
		return
	}

	previous := make(map[gen.Atom]bool, len(member.Peers))
	for _, peer := range member.Peers {
		previous[peer] = true
	}
	current := make(map[gen.Atom]bool, len(peers))
	for _, peer := range peers {
		current[peer] = true
	}

	updated := member.clone()
	updated.Peers = append([]gen.Atom(nil), peers...)
	c.store.members.Store(node, updated)

	for peer := range current {
		if previous[peer] {
			continue
		}
		c.addRef(peer, node)
	}

	var dropped []gen.Atom
	for peer := range previous {
		if current[peer] {
			continue
		}
		c.removeRef(peer, node)
		dropped = append(dropped, peer)
	}
	c.recheck(dropped...)
}

func (c *cluster) addRef(node, by gen.Atom) {
	c.discover(node, false)

	member, found := c.store.member(node)
	if found == false {
		return
	}
	if member.Refs[by] {
		return
	}

	updated := member.clone()
	updated.Refs[by] = true
	c.store.members.Store(node, updated)

	if updated.Status != statusReachable {
		c.wake(node)
	}
}

func (c *cluster) removeRef(node, by gen.Atom) {
	member, found := c.store.member(node)
	if found == false {
		return
	}
	if member.Refs[by] == false {
		return
	}

	updated := member.clone()
	delete(updated.Refs, by)
	c.store.members.Store(node, updated)
}

func (c *cluster) onWatcherDown(pid gen.PID) {
	node, found := c.watched[pid]
	if found == false {
		c.dropped["watcher_down_unknown"]++
		return
	}
	c.deaths++
	delete(c.watched, pid)

	if current, exist := c.watchers[node]; exist && current != pid {
		return
	}

	delete(c.watchers, node)
	delete(c.pending, node)
	c.drainQueue()

	member, found := c.store.member(node)
	if found == false {
		return
	}

	updated := member.clone()
	updated.Watched = false
	c.store.members.Store(node, updated)

	if c.alive(updated, time.Now().Unix()) {
		c.SendAfter(c.PID(), messageRetryNode{Node: node}, retryAfterWatcherDown)
		c.retriesArmed++
	}
}

func (c *cluster) onRegistrarLeft(node gen.Atom) {
	member, found := c.store.member(node)
	if found == false {
		return
	}
	updated := member.clone()
	updated.Registrar = false
	c.store.members.Store(node, updated)

	c.recheck(node)
}

func (c *cluster) alive(member *memberState, now int64) bool {
	if member.Status == statusReachable {
		return true
	}
	if member.Registrar {
		return true
	}
	for by := range member.Refs {
		if c.refValid(by, now) {
			return true
		}
	}
	return false
}

func (c *cluster) refValid(by gen.Atom, now int64) bool {
	member, found := c.store.member(by)
	if found == false {
		return false
	}
	if member.Status == statusReachable {
		return true
	}
	return now-member.Since < int64(c.options.GracePeriod/time.Second)
}

func (c *cluster) recheck(seed ...gen.Atom) {
	if len(seed) == 0 {
		return
	}

	self := c.Node().Name()
	now := time.Now().Unix()
	queue := append([]gen.Atom(nil), seed...)
	evicted := 0

	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]

		if node == self {
			continue
		}
		member, found := c.store.member(node)
		if found == false {
			continue
		}
		if c.alive(member, now) {
			continue
		}

		queue = append(queue, c.evict(member)...)
		evicted++
	}

	if evicted > 0 {
		c.lastEviction = time.Now()
		c.drainQueue()
		c.Log().Debug("cluster: %d node(s) left the map", evicted)
	}
}

func (c *cluster) evict(member *memberState) []gen.Atom {
	node := member.Node

	if pid, exist := c.watchers[node]; exist {
		c.Send(pid, messageWatcherStop{})
		delete(c.watchers, node)
		delete(c.watched, pid)
	}
	delete(c.pending, node)
	delete(c.queued, node)

	peers := append([]gen.Atom(nil), member.Peers...)

	c.store.members.Delete(node)
	c.store.snapshots.Delete(node)
	c.members--
	c.evicted++

	for _, peer := range peers {
		c.removeRef(peer, node)
	}
	return peers
}

func (c *cluster) reconcile() {
	c.lastReconcile = time.Now()

	now := time.Now().Unix()
	self := c.Node().Name()

	var stale, unwatched []gen.Atom
	c.store.members.Range(func(k, v any) bool {
		node := k.(gen.Atom)
		if node != self && c.alive(v.(*memberState), now) == false {
			stale = append(stale, node)
			return true
		}
		if _, exist := c.watchers[node]; exist == false && c.queued[node] == false {
			unwatched = append(unwatched, node)
		}
		return true
	})

	if len(unwatched) > 0 {
		c.unwatched += int64(len(unwatched))
		c.Log().Warning("cluster: %d node(s) on the map without a watcher: %v", len(unwatched), unwatched)
		for _, node := range unwatched {
			c.startWatcher(node)
		}
	}

	if len(stale) == 0 {
		return
	}
	c.stale += int64(len(stale))
	c.Log().Warning("cluster: %d unsupported node(s) survived eviction: %v", len(stale), stale)
	c.recheck(stale...)
}

func (c *cluster) info() ClusterInfo {
	now := time.Now()
	note := c.incomplete()
	result := ClusterInfo{
		Nodes:       []ClusterNodeInfo{},
		Offline:     []OfflineNode{},
		Complete:    note == "",
		Note:        note,
		Watched:     len(c.watchers),
		Limit:       c.options.WatchLimit,
		WatchPeriod: c.options.WatchPeriod,
	}

	c.store.members.Range(func(k, v any) bool {
		node := k.(gen.Atom)
		member := v.(*memberState)
		if snapshot, found := c.store.snapshot(node); found {
			reading := ClusterNodeInfo{
				Info:   *snapshot.Info,
				Age:    now.Sub(snapshot.At).Milliseconds(),
				Status: member.Status,
			}
			if member.Status != statusReachable {
				reading.Reason = member.Reason
			}
			result.Nodes = append(result.Nodes, reading)
			return true
		}
		result.Offline = append(result.Offline, offlineFrom(member))
		return true
	})

	sort.Slice(result.Nodes, func(i, j int) bool { return result.Nodes[i].Info.Name < result.Nodes[j].Info.Name })
	sort.Slice(result.Offline, func(i, j int) bool { return result.Offline[i].Node < result.Offline[j].Node })

	return result
}

func offlineFrom(member *memberState) OfflineNode {
	return OfflineNode{
		Node:     member.Node,
		Status:   member.Status,
		Reason:   member.Reason,
		Since:    member.Since,
		LastSeen: member.LastSeen,
		Peers:    member.Peers,
	}
}
