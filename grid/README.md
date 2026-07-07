# Grid Application

Doc: https://docs.ergo.services/extra-library/applications/grid

In a cluster you often need processes on different nodes to find each other by name, react when one appears or disappears, and broadcast to a changing set of subscribers. Doing this by hand means wiring point-to-point monitors, tracking who lives where, and inventing a naming scheme, all while nodes join and leave.

Grid provides this as an application: a distributed in-memory registry, key-scoped lifecycle monitors, and per-key process groups. It is AP and eventually consistent - every node keeps a full local copy of the registry, so lookups are local and instant, and writes replicate to peers in the background. Actors interact with it through helper functions in the `grid` package; there is no wiring between nodes to maintain.

## Installation

```bash
go get ergo.services/application/grid
```

## Starting

Load and start the Grid application on your node:

```go
import (
    "ergo.services/application/grid"
    "ergo.services/ergo"
    "ergo.services/ergo/gen"
)

func main() {
    node, _ := ergo.StartNode("mynode@localhost", gen.NodeOptions{
        Applications: []gen.ApplicationBehavior{
            grid.CreateApp(grid.Options{Domain: "grid", Shards: 8}),
        },
    })

    node.Wait()
}
```

Every node that starts Grid with the same `Domain` discovers the others automatically - through the registrar, connected nodes, or static `Peers` - and forms a mesh. No further configuration is required.

## Using from Actors

Every helper takes the calling actor (a `gen.Process`) as its first argument. An empty domain resolves to `"default"`.

### Registry

Register a key under the calling process, look it up from any node, and unregister it:

```go
func (w *worker) Init(args ...any) error {
    if err := grid.Register(w, "grid", "order/42", "meta-v1"); err != nil {
        return err // gen.ErrTaken if a live owner already holds the key
    }
    return nil
}

func (w *worker) HandleMessage(from gen.PID, message any) error {
    switch message.(type) {
    case messageDone:
        grid.Unregister(w, "grid", "order/42")
    }
    return nil
}
```

`Register` and `Unregister` are synchronous (they route to the owning shard and return an error). `Lookup` and the counts read the node's local view directly, with no actor call:

```go
// Registry (sync Call, returns error)
grid.Register(process, domain, key, meta)   // gen.ErrTaken if held by another live owner
grid.Unregister(process, domain, key)       // gen.ErrUnknown / gen.ErrIncorrect

// Reads (local, lock-free)
grid.Lookup(process, domain, key)           // (gen.PID, any, bool)
grid.RegistryCount(process, domain)         // int, converges to the cluster total
grid.LocalRegistryCount(process, domain)    // int, keys owned by this node
grid.LocalEntries(process, domain)          // []grid.Entry owned by this node
grid.Peers(process, domain)                 // ([]gen.Atom, error)
```

Re-registering the same key by its current owner with unchanged metadata is a no-op; with changed metadata it updates the entry and notifies monitors.

### Monitoring

Subscribe to lifecycle changes of a key, a prefix, or the whole domain. Notifications arrive in `HandleMessage`:

```go
func (o *observer) Init(args ...any) error {
    // matches "order/42" and "order/42/..." but not "order/420"
    return grid.MonitorPrefix(o, "grid", "order")
}

func (o *observer) HandleMessage(from gen.PID, message any) error {
    switch m := message.(type) {
    case grid.MessageRegistered:
        o.Log().Info("%s registered at %s", m.Key, m.Owner)
    case grid.MessageUpdated:
        o.Log().Info("%s meta changed: %v", m.Key, m.Meta)
    case grid.MessageUnregistered:
        o.Log().Info("%s gone: %s", m.Key, m.Reason)
    }
    return nil
}
```

On subscribe you first receive a `MessageRegistered` for every already-present matching key, then live changes. `MessageUnregistered` carries a `Reason`: `ReasonUnregister`, `ReasonDown` (owner terminated), `ReasonConflict` (owner lost a conflict), or `ReasonNodeDown` (owner's node was lost). Subscriptions survive a shard restart.

```go
// Subscribe (notifications in HandleMessage)
grid.MonitorKey(process, domain, key)
grid.MonitorPrefix(process, domain, prefix)
grid.MonitorAll(process, domain)

// Cancel
grid.DemonitorKey(process, domain, key)
grid.DemonitorPrefix(process, domain, prefix)
grid.DemonitorAll(process, domain)
```

### Groups

A group is per-key pub/sub. The key owner opens a group; other processes join to receive broadcasts. Dispatched payloads arrive in `HandleEvent`:

```go
// owner side
func (w *worker) Init(args ...any) error {
    grid.Register(w, "grid", "room/42", nil)
    return grid.OpenGroup(w, "grid", "room/42")
}

func (w *worker) HandleMessage(from gen.PID, message any) error {
    return grid.Dispatch(w, "grid", "room/42", message) // to all members
}

// member side
func (m *member) HandleMessage(from gen.PID, message any) error {
    switch msg := message.(type) {
    case grid.MessageRegistered:
        grid.Join(m, msg.Domain, msg.Key) // returns the event; keep it for Leave
    }
    return nil
}

func (m *member) HandleEvent(message gen.MessageEvent) error {
    // dispatched payloads land here; gen.MessageDownEvent means the owner is gone
    return nil
}
```

`Dispatch` and `MemberCount` must run on the owner node. A group lives as long as its owner; when the owner terminates, members receive a `gen.MessageDownEvent` and re-`Join` once the key reappears.

```go
grid.OpenGroup(process, domain, key)
grid.CloseGroup(process, domain, key)
grid.Join(process, domain, key)            // (gen.Event, error)
grid.Leave(process, event)                 // pass the event returned by Join
grid.Dispatch(process, domain, key, msg)   // owner node
grid.MemberCount(process, domain, key)     // (int, error), owner node
```

## Options

| Option | Default | Description |
|--------|---------|-------------|
| Domain | "default" | Peering scope. A node peers only with grids of the same domain; the application name is `grid_<Domain>`. |
| Shards | 8 | Keyspace shard count. All nodes in a domain must use the same value; a mismatch is rejected. |
| Separator | "/" | Key hierarchy separator for `MonitorPrefix`. A prefix matches at separator boundaries. |
| Peers | none | Static seed nodes for discovery when no registrar is available. |

## Conflict Resolution

Grid is AP: two nodes can register the same key at the same time. Grid resolves this last-writer-wins, ordered by registration time, then by owner PID (ID, then Creation), then by node name. The losing owner is stopped with `ErrRegistryConflict` so the cluster converges on a single owner. If your application requires at-most-one ownership with no window of divergence, grid's registry is a fast observation layer, not a lock - pair it with a linearizable authority.

Any non-primitive `meta` value, and any custom group payload, crosses the wire and must be EDF-registered by the consumer.

## License

See LICENSE file in the repository root.
