package observer

import (
	"fmt"
	"math/rand"
	"time"

	"ergo.services/ergo/act"
	"ergo.services/ergo/app/system/inspect"
	"ergo.services/ergo/gen"
)

var watcherBackoff = []time.Duration{
	time.Second,
	2 * time.Second,
	5 * time.Second,
	10 * time.Second,
	30 * time.Second,
	time.Minute,
}

func factory_watcher() gen.ProcessBehavior {
	return &watcher{}
}

type watcher struct {
	act.Actor

	node    gen.Atom
	store   *clusterStore
	period  time.Duration
	keep    time.Duration
	event   gen.Event
	peers   []gen.Atom
	attempt int

	lastSnapshot time.Time
	silences     int

	retryAt   time.Time
	reason    string
	reasonAt  time.Time
	snapshots int64
	wakes     int64
	dropped   map[string]int64
}

func (w *watcher) Init(args ...any) error {
	a := args[0].(watcherArgs)
	w.node = a.node
	w.store = a.store
	w.period = a.period
	w.keep = a.keep
	w.dropped = make(map[string]int64)

	if _, err := w.SendEvery(w.PID(), messageSilence{}, a.period); err != nil {
		return err
	}
	return w.Send(w.PID(), messageRun{})
}

func (w *watcher) HandleMessage(from gen.PID, message any) error {
	switch m := message.(type) {
	case messageRun:
		w.retryAt = time.Time{}
		w.connect()

	case messageWatcherWake:
		w.wakes++
		w.connect()

	case messageWatcherStop:
		return gen.TerminateReasonNormal

	case gen.MessageDownEvent:
		w.unreachable(fmt.Sprintf("inspector down: %s", m.Event))

	case gen.MessageDownNode:
		w.unreachable(fmt.Sprintf("node %s down", m.Name))

	case messageSilence:
		w.checkSilence()
		w.expireReading()

	default:
		w.dropped["message_unexpected"]++
	}
	return nil
}

func (w *watcher) checkSilence() {
	if w.event.Name == "" {
		return // not subscribed; the retry schedule owns this
	}
	if time.Since(w.lastSnapshot) < time.Duration(silencePeriods)*w.period {
		return
	}

	w.silences++
	quiet := time.Since(w.lastSnapshot).Truncate(time.Second)

	if w.silences > 1 {
		w.Log().Warning("no snapshots from %s for %s after resubscribing, treating it as unreachable",
			w.node, quiet)
		w.unreachable(fmt.Sprintf("silent for %s", quiet))
		return
	}

	w.Log().Warning("no snapshots from %s for %s, resubscribing", w.node, quiet)
	w.DemonitorEvent(w.event)
	w.DemonitorNode(w.node)
	w.event = gen.Event{}
	w.connect()
}

func (w *watcher) HandleEvent(message gen.MessageEvent) error {
	info, ok := message.Message.(inspect.MessageInspectNodeShort)
	if ok == false {
		w.dropped["event_unexpected"]++
		return nil
	}

	w.lastSnapshot = time.Now()
	w.silences = 0
	w.snapshots++
	w.store.snapshots.Store(w.node, &nodeSnapshot{Info: &info.Info, At: w.lastSnapshot})

	if samePeers(w.peers, info.Info.Peers) == false {
		w.peers = peerNames(info.Info.Peers)
		w.Send(w.Parent(), messageWatcherPeers{Node: w.node, Peers: w.peers})
	}
	return nil
}

const watcherInspectHelp = "summary keys: node, state, event, period, keep_reading, reading, snapshots, " +
	"last_snapshot, silences, attempt, retry_in, wakes, reason, reason_age, peers, dropped; " +
	"queries: peers, dropped"

func (w *watcher) HandleInspect(from gen.PID, item ...string) map[string]string {
	if len(item) == 0 {
		return w.inspectSummary()
	}

	result := map[string]string{}
	for _, q := range item {
		switch q {
		case "help":
			result["help"] = watcherInspectHelp
		case "peers":
			names := make([]string, 0, len(w.peers))
			for _, peer := range w.peers {
				names = append(names, string(peer))
			}
			result[q] = fmt.Sprintf("%d: %s", len(names), inspectList(names))
		case "dropped":
			result[q] = inspectCounters(w.dropped)
		default:
			result[q] = "<unknown item>"
		}
	}
	return result
}

func (w *watcher) inspectSummary() map[string]string {
	event := "none"
	if w.event.Name != "" {
		event = w.event.String()
	}
	reason := w.reason
	if reason == "" {
		reason = "none"
	}

	return map[string]string{
		"node":          string(w.node),
		"state":         w.state(),
		"event":         event,
		"period":        w.period.String(),
		"keep_reading":  w.keep.String(),
		"reading":       w.reading(),
		"snapshots":     fmt.Sprintf("%d", w.snapshots),
		"last_snapshot": inspectAge(w.lastSnapshot),
		"silences":      fmt.Sprintf("%d", w.silences),
		"attempt":       fmt.Sprintf("%d", w.attempt),
		"retry_in":      inspectArmed(w.retryAt),
		"wakes":         fmt.Sprintf("%d", w.wakes),
		"reason":        reason,
		"reason_age":    inspectAge(w.reasonAt),
		"peers":         fmt.Sprintf("%d", len(w.peers)),
		"dropped":       inspectCounters(w.dropped),
		"items":         "help",
	}
}

func (w *watcher) reading() string {
	if _, exist := w.store.snapshot(w.node); exist == false {
		return "none"
	}
	if w.event.Name == "" {
		return "stale"
	}
	return "live"
}

func (w *watcher) state() string {
	if w.event.Name == "" {
		if w.retryAt.IsZero() {
			return "connecting"
		}
		return "retrying"
	}
	if w.silences > 0 {
		return "resubscribed"
	}
	return "watching"
}

func (w *watcher) Terminate(reason error) {
	w.store.snapshots.Delete(w.node)
}

func (w *watcher) connect() {
	if w.event.Name != "" {
		return
	}

	target := gen.ProcessID{Name: inspect.Name, Node: w.node}

	request := inspect.RequestInspectNodeShort{Period: w.period}
	result, err := w.CallWithTimeout(target, request, defaultCallTimeout)
	if err != nil {
		w.unreachable(err.Error())
		return
	}

	response, ok := result.(inspect.ResponseInspectNodeShort)
	if ok == false {
		w.unreachable(fmt.Sprintf("unexpected response %T", result))
		return
	}
	if response.Error != nil {
		w.unreachable(response.Error.Error())
		return
	}

	if _, err := w.MonitorEvent(response.Event); err != nil {
		w.unreachable(fmt.Sprintf("monitor %s: %s", response.Event, err))
		return
	}

	if err := w.MonitorNode(w.node); err != nil && err != gen.ErrTargetExist {
		w.Log().Debug("cannot monitor node %s: %s", w.node, err)
	}

	w.event = response.Event
	w.attempt = 0
	w.lastSnapshot = time.Now()
	w.snapshots++
	w.reason = ""
	w.reasonAt = time.Time{}

	info := response.Info
	w.store.snapshots.Store(w.node, &nodeSnapshot{Info: &info, At: w.lastSnapshot})
	w.peers = peerNames(info.Peers)

	w.Send(w.Parent(), messageWatcherReachable{Node: w.node})
	w.Send(w.Parent(), messageWatcherPeers{Node: w.node, Peers: w.peers})
}

func (w *watcher) expireReading() {
	if w.event.Name != "" {
		return
	}
	if time.Since(w.lastSnapshot) < w.keep {
		return
	}
	w.store.snapshots.Delete(w.node)
}

func (w *watcher) unreachable(reason string) {
	w.silences = 0
	w.reason = reason
	w.reasonAt = time.Now()

	if w.event.Name != "" {
		w.DemonitorEvent(w.event)
		w.DemonitorNode(w.node)
		w.event = gen.Event{}
	}
	w.expireReading()

	w.Send(w.Parent(), messageWatcherUnreachable{Node: w.node, Reason: reason})

	delay := watcherBackoff[len(watcherBackoff)-1]
	if w.attempt < len(watcherBackoff) {
		delay = watcherBackoff[w.attempt]
	}
	w.attempt++

	jitter := time.Duration(rand.Int63n(int64(delay / 4)))

	w.SendAfter(w.PID(), messageRun{}, delay+jitter)
	w.retryAt = time.Now().Add(delay + jitter)
}

func peerNames(peers []gen.RemoteNodeShortInfo) []gen.Atom {
	names := make([]gen.Atom, 0, len(peers))
	for _, peer := range peers {
		names = append(names, peer.Node)
	}
	return names
}

func samePeers(a []gen.Atom, b []gen.RemoteNodeShortInfo) bool {
	if len(a) != len(b) {
		return false
	}
	seen := make(map[gen.Atom]bool, len(a))
	for _, node := range a {
		seen[node] = true
	}
	for _, peer := range b {
		if seen[peer.Node] == false {
			return false
		}
	}
	return true
}
