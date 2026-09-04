package grid

import (
	"errors"
	"reflect"
	"sort"
	"strings"
	"time"

	"ergo.services/ergo/act"
	"ergo.services/ergo/gen"
)

const (
	maxSeedAttempts   = 10
	seedRetryBackoff  = 500 * time.Millisecond
	seedSteadyBackoff = 15 * time.Second
)

// ErrRegistryConflict is the exit reason sent to a local owner that lost a
// registry conflict.
var ErrRegistryConflict = errors.New("grid: registry conflict")

type subKind int

const (
	subKey subKind = iota
	subPrefix
	subAll
)

func scopeMatches(kind subKind, match, key, sep string) bool {
	switch kind {
	case subAll:
		return true
	case subPrefix:
		return prefixMatch(key, match, sep)
	default:
		return match == key
	}
}

// prefixMatch reports whether prefix scopes key: the key itself or anything below
// it at a separator boundary. An empty separator is a raw byte prefix.
func prefixMatch(key, prefix, sep string) bool {
	if sep == "" || strings.HasSuffix(prefix, sep) {
		return strings.HasPrefix(key, prefix)
	}
	return key == prefix || strings.HasPrefix(key, prefix+sep)
}

// watchScope is one subscriber's set of interests within a shard.
type watchScope struct {
	all      bool
	keys     map[string]struct{}
	prefixes map[string]struct{}
}

func (w *watchScope) matches(key, sep string) bool {
	if w.all {
		return true
	}
	if _, ok := w.keys[key]; ok {
		return true
	}
	for p := range w.prefixes {
		if prefixMatch(key, p, sep) {
			return true
		}
	}
	return false
}

func (w *watchScope) empty() bool {
	return w.all == false && len(w.keys) == 0 && len(w.prefixes) == 0
}

// coversExcept reports whether any scope other than (kind, match) matches key.
func (w *watchScope) coversExcept(key string, kind subKind, match, sep string) bool {
	if w.all && kind != subAll {
		return true
	}
	if _, ok := w.keys[key]; ok && (kind == subKey && match == key) == false {
		return true
	}
	for p := range w.prefixes {
		if prefixMatch(key, p, sep) && (kind == subPrefix && match == p) == false {
			return true
		}
	}
	return false
}

func (w *watchScope) add(kind subKind, match string) bool {
	switch kind {
	case subAll:
		if w.all {
			return false
		}
		w.all = true
	case subPrefix:
		if _, ok := w.prefixes[match]; ok {
			return false
		}
		w.prefixes[match] = struct{}{}
	default:
		if _, ok := w.keys[match]; ok {
			return false
		}
		w.keys[match] = struct{}{}
	}
	return true
}

// shardWatch holds a shard's subscriptions.
type shardWatch struct {
	scopes map[gen.PID]*watchScope
}

func (w *watchScope) remove(kind subKind, match string) bool {
	switch kind {
	case subAll:
		if w.all == false {
			return false
		}
		w.all = false
	case subPrefix:
		if _, ok := w.prefixes[match]; ok == false {
			return false
		}
		delete(w.prefixes, match)
	default:
		if _, ok := w.keys[match]; ok == false {
			return false
		}
		delete(w.keys, match)
	}
	return true
}

type eventKind int

const (
	evRegistered eventKind = iota
	evUnregistered
	evUpdated
)

func factoryShard() gen.ProcessBehavior {
	return &shard{}
}

// shard owns one slice of the keyspace: it serializes writes for its keys,
// replicates them to peer shards, and notifies local subscribers.
type shard struct {
	act.Actor

	domain    gen.Atom
	index     int
	numShards int
	sep       string
	data      *storeData

	seeds     []gen.Atom
	peers     map[gen.Atom]gen.PID // node -> counterpart shard pid
	pending   map[gen.Atom]int
	connected map[gen.Atom]bool
	regEvent  gen.Event
	regActive bool

	byPid     map[gen.PID]map[string]struct{}
	watch     *shardWatch
	monitored map[gen.PID]int
}

func (s *shard) Init(args ...any) error {
	options := args[0].(Options)
	s.domain = options.Domain
	s.numShards = options.Shards
	s.sep = options.Separator
	if s.sep == "" {
		s.sep = DefaultSeparator
	}
	s.index = args[1].(int)
	s.peers = make(map[gen.Atom]gen.PID)
	s.pending = make(map[gen.Atom]int)
	s.connected = make(map[gen.Atom]bool)
	s.byPid = make(map[gen.PID]map[string]struct{})
	s.monitored = make(map[gen.PID]int)

	self := s.Node().Name()
	for _, seed := range options.Peers {
		if seed == "" || seed == self {
			continue
		}
		s.seeds = append(s.seeds, seed)
	}

	d, _ := stores.LoadOrStore(storeKey{node: self, domain: s.domain}, &storeData{})
	s.data = d.(*storeData)
	w, _ := s.data.watchers.LoadOrStore(s.index, &shardWatch{scopes: make(map[gen.PID]*watchScope)})
	s.watch = w.(*shardWatch)
	s.rebuildMonitors()
	s.rebuildWatchers()

	if last, err := s.MonitorEvent(gen.Event{Name: gen.CoreEvent}); err == nil {
		s.absorbCoreEvents(last)
	}
	s.subscribeRegistrar()
	s.Send(s.PID(), messageBootstrap{})
	return nil
}

func (s *shard) HandleCall(from gen.PID, ref gen.Ref, request any) (any, error) {
	switch r := request.(type) {
	case registerRequest:
		if err := s.register(r.Key, r.PID, r.Meta); err != nil {
			s.SendResponseError(from, ref, err)
			return nil, nil
		}
		s.SendResponse(from, ref, registerResponse{})
		return nil, nil

	case unregisterRequest:
		if err := s.unregister(r.Key, r.PID); err != nil {
			s.SendResponseError(from, ref, err)
			return nil, nil
		}
		s.SendResponse(from, ref, unregisterResponse{})
		return nil, nil

	case getPeersRequest:
		nodes := make([]gen.Atom, 0, len(s.peers))
		for n := range s.peers {
			nodes = append(nodes, n)
		}
		sort.Slice(nodes, func(i, j int) bool { return nodes[i] < nodes[j] })
		return getPeersResponse{Nodes: nodes}, nil
	}
	s.SendResponseError(from, ref, gen.ErrUnsupported)
	return nil, nil
}

func (s *shard) HandleMessage(from gen.PID, message any) error {
	switch m := message.(type) {
	case messageBootstrap:
		s.discover()

	case messagePeerConnect:
		if s.peerCompatible(m.Domain, m.NumShards, m.Index) == false {
			return nil
		}
		s.Send(m.From, messagePeerConnectAck{From: s.PID(), Domain: s.domain, NumShards: s.numShards, Index: s.index})
		s.confirm(m.From)

	case messagePeerConnectAck:
		if s.peerCompatible(m.Domain, m.NumShards, m.Index) == false {
			return nil
		}
		s.confirm(m.From)

	case messageSeedRetry:
		s.seedRetry(m.Node)

	case messageReconcile:
		s.subscribeRegistrar()
		s.discover()

	case messageRegister:
		s.applyRemoteRegister(m)

	case messageUnregister:
		s.applyRemoteUnregister(m)

	case messageClusterState:
		s.applyClusterState(m)

	case messageMonitor:
		s.addWatch(m)

	case messageUnmonitor:
		s.removeWatch(m)

	case gen.MessageDownPID:
		if node, ok := s.peerNode(m.PID); ok {
			s.dropPeer(node, false)
			return nil
		}
		s.handleDown(m.PID)

	case gen.MessageDownEvent:
		if s.regActive && m.Event == s.regEvent {
			s.regActive = false
			s.subscribeRegistrar()
			s.Send(s.PID(), messageReconcile{})
		}
	}
	return nil
}

func (s *shard) HandleEvent(message gen.MessageEvent) error {
	switch m := message.Message.(type) {
	case gen.MessageCoreNodeConnected:
		s.connected[m.Name] = true
		s.probe(m.Name)
	case gen.MessageCoreNodeDisconnected:
		s.dropPeer(m.Name, true)
	case gen.MessageRegistrarApplicationStarted:
		if m.Route.Name == appName(s.domain) {
			s.probe(m.Route.Node)
		}
	case gen.MessageRegistrarApplicationStopping:
		if m.Route.Name == appName(s.domain) {
			s.dropPeer(m.Route.Node, true)
		}
	case gen.MessageRegistrarApplicationUnloaded:
		if m.Route.Name == appName(s.domain) {
			s.dropPeer(m.Route.Node, true)
		}
	}
	return nil
}

func (s *shard) register(key string, pid gen.PID, meta any) error {
	if v, ok := s.data.regByKey.Load(key); ok {
		e := v.(entry)
		if e.owner == pid {
			if reflect.DeepEqual(e.meta, meta) {
				return nil
			}
			now := time.Now().UnixNano()
			s.data.regByKey.Store(key, entry{owner: pid, meta: meta, time: now})
			s.emitUpdated(key, pid, meta)
			s.replicate(messageRegister{Key: key, Owner: pid, Meta: meta, Time: now})
			return nil
		}
		return gen.ErrTaken
	}

	now := time.Now().UnixNano()
	s.data.regByKey.Store(key, entry{owner: pid, meta: meta, time: now})
	if s.track(pid, key) {
		s.retain(pid)
	}
	s.emitRegistered(key, pid, meta)
	s.replicate(messageRegister{Key: key, Owner: pid, Meta: meta, Time: now})
	return nil
}

func (s *shard) unregister(key string, pid gen.PID) error {
	v, ok := s.data.regByKey.Load(key)
	if ok == false {
		return gen.ErrUnknown
	}
	e := v.(entry)
	if e.owner != pid || e.owner.Node != s.Node().Name() {
		return gen.ErrIncorrect
	}
	s.data.regByKey.Delete(key)
	if s.untrack(pid, key) {
		s.release(pid)
	}
	s.emitUnregistered(key, pid, ReasonUnregister)
	s.replicate(messageUnregister{Key: key, Owner: pid, Reason: ReasonUnregister})
	return nil
}

func (s *shard) handleDown(pid gen.PID) {
	if keys, ok := s.byPid[pid]; ok {
		for key := range keys {
			s.data.regByKey.Delete(key)
			s.emitUnregistered(key, pid, ReasonDown)
			s.replicate(messageUnregister{Key: key, Owner: pid, Reason: ReasonDown})
		}
		delete(s.byPid, pid)
	}
	delete(s.watch.scopes, pid)
	delete(s.monitored, pid)
}

func (s *shard) applyRemoteRegister(m messageRegister) {
	if hashKey(m.Key, s.numShards) != s.index {
		return
	}
	v, ok := s.data.regByKey.Load(m.Key)
	if ok == false {
		s.data.regByKey.Store(m.Key, entry{owner: m.Owner, meta: m.Meta, time: m.Time})
		s.emitRegistered(m.Key, m.Owner, m.Meta)
		return
	}
	e := v.(entry)
	if e.owner == m.Owner {
		if m.Time > e.time {
			s.data.regByKey.Store(m.Key, entry{owner: m.Owner, meta: m.Meta, time: m.Time})
			if reflect.DeepEqual(e.meta, m.Meta) == false {
				s.emitUpdated(m.Key, m.Owner, m.Meta)
			}
		}
		return
	}

	remoteWins := entryWins(m.Time, m.Owner, e.time, e.owner)
	if e.owner.Node == s.Node().Name() {
		if remoteWins == false {
			s.replicate(messageRegister{Key: m.Key, Owner: e.owner, Meta: e.meta, Time: e.time})
			return
		}
		s.data.regByKey.Store(m.Key, entry{owner: m.Owner, meta: m.Meta, time: m.Time})
		if s.untrack(e.owner, m.Key) {
			s.release(e.owner)
		}
		s.SendExit(e.owner, ErrRegistryConflict)
		s.emitUnregistered(m.Key, e.owner, ReasonConflict)
		s.emitRegistered(m.Key, m.Owner, m.Meta)
		return
	}
	if remoteWins {
		s.data.regByKey.Store(m.Key, entry{owner: m.Owner, meta: m.Meta, time: m.Time})
		s.emitUnregistered(m.Key, e.owner, ReasonConflict)
		s.emitRegistered(m.Key, m.Owner, m.Meta)
	}
}

func (s *shard) applyRemoteUnregister(m messageUnregister) {
	if hashKey(m.Key, s.numShards) != s.index {
		return
	}
	v, ok := s.data.regByKey.Load(m.Key)
	if ok == false {
		return
	}
	e := v.(entry)
	if e.owner != m.Owner {
		return
	}
	s.data.regByKey.Delete(m.Key)
	if e.owner.Node == s.Node().Name() && s.untrack(e.owner, m.Key) {
		s.release(e.owner)
	}
	s.emitUnregistered(m.Key, m.Owner, m.Reason)
}

// peer discovery

func (s *shard) peerCompatible(domain gen.Atom, numShards, index int) bool {
	if domain != s.domain || index != s.index {
		return false
	}
	if numShards != s.numShards {
		s.Log().Error("grid shard %s/%d: shard count mismatch from peer (%d != %d)",
			s.domain, s.index, numShards, s.numShards)
		return false
	}
	return true
}

// discover probes the counterpart shard on every candidate node. Idempotent.
func (s *shard) discover() {
	self := s.Node().Name()
	candidates := make(map[gen.Atom]bool)
	for _, seed := range s.seeds {
		candidates[seed] = true
	}
	for n := range s.connected {
		candidates[n] = true
	}
	if registrar, err := s.Node().Network().Registrar(); err == nil {
		if routes, err := registrar.Resolver().ResolveApplication(appName(s.domain)); err == nil {
			for _, r := range routes {
				candidates[r.Node] = true
			}
		}
	}
	delete(candidates, self)
	for n := range candidates {
		s.probe(n)
	}
	for _, seed := range s.seeds {
		if _, ok := s.peers[seed]; ok {
			continue
		}
		if _, armed := s.pending[seed]; armed {
			continue
		}
		s.pending[seed] = 1
		s.SendAfter(s.PID(), messageSeedRetry{Node: seed}, seedRetryBackoff)
	}
}

func (s *shard) seedRetry(node gen.Atom) {
	if _, ok := s.peers[node]; ok {
		delete(s.pending, node)
		return
	}
	attempt := s.pending[node]
	s.pending[node] = attempt + 1
	s.probe(node)
	// retry a static seed indefinitely: fast at first, then slowly.
	backoff := seedRetryBackoff
	if attempt >= maxSeedAttempts {
		backoff = seedSteadyBackoff
	}
	s.SendAfter(s.PID(), messageSeedRetry{Node: node}, backoff)
}

func (s *shard) probe(node gen.Atom) {
	if node == "" || node == s.Node().Name() {
		return
	}
	if _, ok := s.peers[node]; ok {
		return
	}
	s.Send(gen.ProcessID{Node: node, Name: shardName(s.domain, s.index)},
		messagePeerConnect{From: s.PID(), Domain: s.domain, NumShards: s.numShards, Index: s.index})
}

func (s *shard) confirm(from gen.PID) {
	node := from.Node
	if node == "" || node == s.Node().Name() {
		return
	}
	if existing, ok := s.peers[node]; ok {
		if existing == from {
			return
		}
		// the peer restarted with a new pid; drop the stale one and re-establish
		s.DemonitorPID(existing)
		delete(s.peers, node)
	}
	if s.MonitorPID(from) != nil {
		return // peer already gone; do not record a dead, unmonitored peer
	}
	s.peers[node] = from
	s.connected[node] = true
	delete(s.pending, node)
	s.sendClusterState(node)
}

func (s *shard) dropPeer(node gen.Atom, demonitor bool) {
	delete(s.connected, node)
	delete(s.pending, node)
	pid, ok := s.peers[node]
	if ok == false {
		return
	}
	if demonitor {
		s.DemonitorPID(pid)
	}
	delete(s.peers, node)
	s.purgeNode(node)
}

func (s *shard) peerNode(pid gen.PID) (gen.Atom, bool) {
	for node, p := range s.peers {
		if p == pid {
			return node, true
		}
	}
	return "", false
}

func (s *shard) absorbCoreEvents(events []gen.MessageEvent) {
	for _, e := range events {
		switch m := e.Message.(type) {
		case gen.MessageCoreNodeConnected:
			s.connected[m.Name] = true
		case gen.MessageCoreNodeDisconnected:
			delete(s.connected, m.Name)
		}
	}
}

func (s *shard) subscribeRegistrar() {
	if s.regActive {
		return
	}
	registrar, err := s.Node().Network().Registrar()
	if err != nil {
		return
	}
	event, err := registrar.Event()
	if err != nil {
		return
	}
	if _, err := s.MonitorEvent(event); err != nil {
		return
	}
	s.regEvent = event
	s.regActive = true
}

// subscriptions

func (s *shard) addWatch(m messageMonitor) {
	ws, ok := s.watch.scopes[m.Subscriber]
	if ok == false {
		if s.retain(m.Subscriber) == false {
			return
		}
		ws = &watchScope{keys: make(map[string]struct{}), prefixes: make(map[string]struct{})}
		s.watch.scopes[m.Subscriber] = ws
	}
	if ws.add(m.Kind, m.Match) == false {
		return
	}
	s.snapshot(m.Subscriber, ws, m.Kind, m.Match)
}

func (s *shard) removeWatch(m messageUnmonitor) {
	ws, ok := s.watch.scopes[m.Subscriber]
	if ok == false {
		return
	}
	if ws.remove(m.Kind, m.Match) == false {
		return
	}
	if ws.empty() {
		delete(s.watch.scopes, m.Subscriber)
		s.release(m.Subscriber)
	}
}

// rebuildWatchers re-establishes subscriber monitors after a restart.
func (s *shard) rebuildWatchers() {
	for pid := range s.watch.scopes {
		if s.retain(pid) == false {
			delete(s.watch.scopes, pid)
		}
	}
}

func (s *shard) snapshot(subscriber gen.PID, ws *watchScope, kind subKind, match string) {
	s.data.regByKey.Range(func(k, v any) bool {
		key := k.(string)
		if hashKey(key, s.numShards) != s.index || scopeMatches(kind, match, key, s.sep) == false {
			return true
		}
		if ws.coversExcept(key, kind, match, s.sep) {
			return true // already covered by another of this subscriber's scopes
		}
		e := v.(entry)
		s.Send(subscriber, MessageRegistered{Domain: s.domain, Key: key, Owner: e.owner, Meta: e.meta})
		return true
	})
}

func (s *shard) emitRegistered(key string, owner gen.PID, meta any) {
	s.emit(evRegistered, key, owner, meta, 0)
}

func (s *shard) emitUnregistered(key string, owner gen.PID, reason UnregisterReason) {
	s.emit(evUnregistered, key, owner, nil, reason)
}

func (s *shard) emitUpdated(key string, owner gen.PID, meta any) {
	s.emit(evUpdated, key, owner, meta, 0)
}

// emit delivers one message per matching subscriber; reason applies only to
// evUnregistered.
func (s *shard) emit(kind eventKind, key string, owner gen.PID, meta any, reason UnregisterReason) {
	if len(s.watch.scopes) == 0 {
		return
	}
	for pid, ws := range s.watch.scopes {
		if ws.matches(key, s.sep) == false {
			continue
		}
		switch kind {
		case evRegistered:
			s.Send(pid, MessageRegistered{Domain: s.domain, Key: key, Owner: owner, Meta: meta})
		case evUnregistered:
			s.Send(pid, MessageUnregistered{Domain: s.domain, Key: key, Owner: owner, Reason: reason})
		case evUpdated:
			s.Send(pid, MessageUpdated{Domain: s.domain, Key: key, Owner: owner, Meta: meta})
		}
	}
}

// retain monitors pid once, refcounted. Returns false if pid is already gone.
func (s *shard) retain(pid gen.PID) bool {
	if s.monitored[pid] > 0 {
		s.monitored[pid]++
		return true
	}
	if s.MonitorPID(pid) != nil {
		return false
	}
	s.monitored[pid] = 1
	return true
}

// release drops one reference; the monitor is removed when the last one goes.
func (s *shard) release(pid gen.PID) {
	c := s.monitored[pid]
	if c == 0 {
		return
	}
	if c == 1 {
		delete(s.monitored, pid)
		s.DemonitorPID(pid)
		return
	}
	s.monitored[pid] = c - 1
}

func (s *shard) sendClusterState(node gen.Atom) {
	var entries []regEntry
	for pid, keys := range s.byPid {
		for key := range keys {
			if v, ok := s.data.regByKey.Load(key); ok {
				e := v.(entry)
				entries = append(entries, regEntry{Key: key, Owner: pid, Meta: e.meta, Time: e.time})
			}
		}
	}
	// sent even when empty so the receiver can reconcile deletes.
	s.Send(gen.ProcessID{Node: node, Name: shardName(s.domain, s.index)},
		messageClusterState{Node: s.Node().Name(), At: time.Now().UnixNano(), Entries: entries})
}

// applyClusterState merges the sender's snapshot and drops the sender's keys that
// the snapshot omits and are not newer than it.
func (s *shard) applyClusterState(m messageClusterState) {
	present := make(map[string]struct{}, len(m.Entries))
	for _, e := range m.Entries {
		present[e.Key] = struct{}{}
		s.applyRemoteRegister(messageRegister(e))
	}
	s.data.regByKey.Range(func(k, v any) bool {
		key := k.(string)
		e := v.(entry)
		if e.owner.Node != m.Node || hashKey(key, s.numShards) != s.index {
			return true
		}
		if _, ok := present[key]; ok {
			return true
		}
		if e.time > m.At {
			return true
		}
		s.data.regByKey.Delete(key)
		s.emitUnregistered(key, e.owner, ReasonNodeDown)
		return true
	})
}

func (s *shard) purgeNode(node gen.Atom) {
	s.data.regByKey.Range(func(k, v any) bool {
		key := k.(string)
		e := v.(entry)
		if e.owner.Node == node && hashKey(key, s.numShards) == s.index {
			s.data.regByKey.Delete(key)
			s.emitUnregistered(key, e.owner, ReasonNodeDown)
		}
		return true
	})
}

func (s *shard) rebuildMonitors() {
	node := s.Node().Name()
	s.data.regByKey.Range(func(k, v any) bool {
		key := k.(string)
		e := v.(entry)
		if e.owner.Node != node || hashKey(key, s.numShards) != s.index {
			return true
		}
		if s.track(e.owner, key) {
			if s.retain(e.owner) == false {
				// owner is gone; drop and notify
				s.data.regByKey.Delete(key)
				s.untrack(e.owner, key)
				s.emitUnregistered(key, e.owner, ReasonDown)
				s.replicate(messageUnregister{Key: key, Owner: e.owner, Reason: ReasonDown})
			}
		}
		return true
	})
}

func (s *shard) replicate(op any) {
	for node := range s.peers {
		s.Send(gen.ProcessID{Node: node, Name: shardName(s.domain, s.index)}, op)
	}
}

func (s *shard) track(pid gen.PID, key string) bool {
	keys, ok := s.byPid[pid]
	if ok == false {
		keys = make(map[string]struct{})
		s.byPid[pid] = keys
	}
	first := len(keys) == 0
	keys[key] = struct{}{}
	return first
}

func (s *shard) untrack(pid gen.PID, key string) bool {
	keys, ok := s.byPid[pid]
	if ok == false {
		return true
	}
	delete(keys, key)
	if len(keys) == 0 {
		delete(s.byPid, pid)
		return true
	}
	return false
}

// entryWins reports whether (aTime, a) beats (bTime, b): later time wins, ties
// break by ID, then Creation, then Node.
func entryWins(aTime int64, a gen.PID, bTime int64, b gen.PID) bool {
	if aTime != bTime {
		return aTime > bTime
	}
	if a.ID != b.ID {
		return a.ID > b.ID
	}
	if a.Creation != b.Creation {
		return a.Creation > b.Creation
	}
	return string(a.Node) > string(b.Node)
}
