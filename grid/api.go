package grid

import (
	"fmt"

	"ergo.services/ergo/gen"
)

// Registry

// Register registers key under the calling process, owned by its PID. The meta
// value crosses the wire on replication, so a non-primitive meta type must be
// EDF-registered by the consumer. Returns gen.ErrTaken if a live owner holds it.
func Register(process gen.Process, domain gen.Atom, key string, meta any) error {
	if domain == "" {
		domain = DefaultDomain
	}
	_, err := process.Call(shardName(domain, shardIndexFor(process.Node().Name(), domain, key)),
		registerRequest{Key: key, PID: process.PID(), Meta: meta})
	return err
}

// Unregister removes key if the calling process owns it locally.
func Unregister(process gen.Process, domain gen.Atom, key string) error {
	if domain == "" {
		domain = DefaultDomain
	}
	_, err := process.Call(shardName(domain, shardIndexFor(process.Node().Name(), domain, key)),
		unregisterRequest{Key: key, PID: process.PID()})
	return err
}

// Lookup returns the owner and meta of key, or false if it is not registered.
func Lookup(process gen.Process, domain gen.Atom, key string) (gen.PID, any, bool) {
	if domain == "" {
		domain = DefaultDomain
	}
	return lookupAt(process.Node().Name(), domain, key)
}

// Entry is a registry entry returned by LocalEntries.
type Entry struct {
	Key   string
	Owner gen.PID
	Meta  any
}

// RegistryCount returns the number of keys in the caller's local registry view,
// which converges to the cluster-wide count.
func RegistryCount(process gen.Process, domain gen.Atom) int {
	if domain == "" {
		domain = DefaultDomain
	}
	d, ok := storeFor(process.Node().Name(), domain)
	if ok == false {
		return 0
	}
	return d.count()
}

// LocalRegistryCount returns the number of keys owned by the caller's node.
func LocalRegistryCount(process gen.Process, domain gen.Atom) int {
	if domain == "" {
		domain = DefaultDomain
	}
	node := process.Node().Name()
	d, ok := storeFor(node, domain)
	if ok == false {
		return 0
	}
	return d.localCount(node)
}

// LocalEntries returns the registry entries owned by the caller's node.
func LocalEntries(process gen.Process, domain gen.Atom) []Entry {
	if domain == "" {
		domain = DefaultDomain
	}
	node := process.Node().Name()
	d, ok := storeFor(node, domain)
	if ok == false {
		return nil
	}
	return d.localEntries(node)
}

// Peers returns the peer node names known to the local grid instance of the
// given domain. Empty domain resolves to DefaultDomain.
func Peers(process gen.Process, domain gen.Atom) ([]gen.Atom, error) {
	if domain == "" {
		domain = DefaultDomain
	}
	v, err := process.Call(shardName(domain, 0), getPeersRequest{})
	if err != nil {
		return nil, err
	}
	resp, ok := v.(getPeersResponse)
	if ok == false {
		return nil, fmt.Errorf("grid: unexpected response %T from shard", v)
	}
	return resp.Nodes, nil
}

// Monitor

// MessageRegistered is delivered to a subscriber when a monitored key is
// registered.
type MessageRegistered struct {
	Domain gen.Atom
	Key    string
	Owner  gen.PID
	Meta   any
}

// UnregisterReason says why a key left the registry.
type UnregisterReason uint8

const (
	ReasonUnregister UnregisterReason = iota // the owner called Unregister
	ReasonDown                               // the owner process terminated
	ReasonConflict                           // the owner lost a registry conflict
	ReasonNodeDown                           // the owner's node/peer was lost
)

func (r UnregisterReason) String() string {
	switch r {
	case ReasonDown:
		return "down"
	case ReasonConflict:
		return "conflict"
	case ReasonNodeDown:
		return "nodedown"
	default:
		return "unregister"
	}
}

// MessageUnregistered is delivered when a monitored key is unregistered. Reason
// says why (manual unregister, owner down, lost conflict, or peer loss).
type MessageUnregistered struct {
	Domain gen.Atom
	Key    string
	Owner  gen.PID
	Reason UnregisterReason
}

// MessageUpdated is delivered when a monitored key's meta changes under the same
// owner.
type MessageUpdated struct {
	Domain gen.Atom
	Key    string
	Owner  gen.PID
	Meta   any
}

// MonitorKey subscribes the calling process to lifecycle events for the exact
// key. Events arrive as MessageRegistered/MessageUnregistered/MessageUpdated in
// HandleMessage. On subscribe it first receives MessageRegistered for the key if
// it is already present. A subscription is identified by its scope: cancel with
// DemonitorKey. Re-monitoring the same scope is a no-op.
func MonitorKey(process gen.Process, domain gen.Atom, key string) error {
	return sendToKeyShard(process, domain, key,
		messageMonitor{Subscriber: process.PID(), Kind: subKey, Match: key})
}

// MonitorPrefix subscribes to every key at or below prefix, split on the domain
// Separator (default "/"): "a/b" matches "a/b" and "a/b/c" but not "a/bc".
func MonitorPrefix(process gen.Process, domain gen.Atom, prefix string) error {
	return sendToAllShards(process, domain,
		messageMonitor{Subscriber: process.PID(), Kind: subPrefix, Match: prefix})
}

// MonitorAll subscribes to every key in the domain.
func MonitorAll(process gen.Process, domain gen.Atom) error {
	return sendToAllShards(process, domain,
		messageMonitor{Subscriber: process.PID(), Kind: subAll})
}

// DemonitorKey cancels a MonitorKey subscription for key.
func DemonitorKey(process gen.Process, domain gen.Atom, key string) error {
	return sendToKeyShard(process, domain, key,
		messageUnmonitor{Subscriber: process.PID(), Kind: subKey, Match: key})
}

// DemonitorPrefix cancels a MonitorPrefix subscription for prefix.
func DemonitorPrefix(process gen.Process, domain gen.Atom, prefix string) error {
	return sendToAllShards(process, domain,
		messageUnmonitor{Subscriber: process.PID(), Kind: subPrefix, Match: prefix})
}

// DemonitorAll cancels a MonitorAll subscription.
func DemonitorAll(process gen.Process, domain gen.Atom) error {
	return sendToAllShards(process, domain,
		messageUnmonitor{Subscriber: process.PID(), Kind: subAll})
}

func sendToKeyShard(process gen.Process, domain gen.Atom, key string, msg any) error {
	if domain == "" {
		domain = DefaultDomain
	}
	return process.Send(shardName(domain, shardIndexFor(process.Node().Name(), domain, key)), msg)
}

func sendToAllShards(process gen.Process, domain gen.Atom, msg any) error {
	if domain == "" {
		domain = DefaultDomain
	}
	for i := 0; i < numShardsFor(process.Node().Name(), domain); i++ {
		if err := process.Send(shardName(domain, i), msg); err != nil {
			return err
		}
	}
	return nil
}

// Groups are per-key pub/sub: the key owner calls OpenGroup, members Join to
// receive Dispatch broadcasts in HandleEvent and Leave to stop. Members are not
// enumerable; MemberCount reports how many there are.

func groupEventName(domain gen.Atom, key string) gen.Atom {
	return gen.Atom(string(appName(domain)) + "_grp_" + key)
}

// OpenGroup opens a group for key on the calling process, which should own key in
// the registry. Members can then Join and receive Dispatch. The group is removed
// by CloseGroup or automatically when the owner terminates.
func OpenGroup(process gen.Process, domain gen.Atom, key string) error {
	if domain == "" {
		domain = DefaultDomain
	}
	_, err := process.RegisterEvent(groupEventName(domain, key), gen.EventOptions{Open: true})
	return err
}

// CloseGroup removes the group opened by OpenGroup.
func CloseGroup(process gen.Process, domain gen.Atom, key string) error {
	if domain == "" {
		domain = DefaultDomain
	}
	return process.UnregisterEvent(groupEventName(domain, key))
}

// Join subscribes the calling process to key's group, which must already be open
// on the owner. Dispatched messages arrive in HandleEvent as gen.MessageEvent. It
// returns the joined event; pass it to Leave to unsubscribe. When the owner
// terminates, the subscription ends with a MessageDownEvent.
func Join(process gen.Process, domain gen.Atom, key string) (gen.Event, error) {
	if domain == "" {
		domain = DefaultDomain
	}
	owner, _, ok := lookupAt(process.Node().Name(), domain, key)
	if ok == false {
		return gen.Event{}, gen.ErrUnknown
	}
	event := gen.Event{Name: groupEventName(domain, key), Node: owner.Node}
	if _, err := process.MonitorEvent(event); err != nil {
		return gen.Event{}, err
	}
	return event, nil
}

// Leave unsubscribes the calling process from the group event returned by Join.
func Leave(process gen.Process, event gen.Event) error {
	return process.DemonitorEvent(event)
}

// Dispatch broadcasts message to all members of key's group. It must be called
// on the owner node, where the group is open.
func Dispatch(process gen.Process, domain gen.Atom, key string, message any) error {
	if domain == "" {
		domain = DefaultDomain
	}
	return process.SendEvent(groupEventName(domain, key), gen.Ref{}, message)
}

// MemberCount returns the number of members of key's group. It must be called on
// the owner node.
func MemberCount(process gen.Process, domain gen.Atom, key string) (int, error) {
	if domain == "" {
		domain = DefaultDomain
	}
	info, err := process.Node().EventInfo(gen.Event{Name: groupEventName(domain, key), Node: process.Node().Name()})
	if err != nil {
		return 0, err
	}
	return int(info.Subscribers), nil
}
