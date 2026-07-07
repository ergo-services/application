package grain

import (
	"hash/fnv"
	"sync"
	"sync/atomic"

	"ergo.services/application/grain/store"
	"ergo.services/ergo/gen"
)

// catalogs maps {node, domain} to that node's catalog of live grains.
var catalogs sync.Map // catalogKey -> *catalog

// activatorCount maps {node, domain} to its activator shard count.
var activatorCount sync.Map // catalogKey -> int

type catalogKey struct {
	node   gen.Atom
	domain gen.Atom
}

// liveGrain is a node-local record of an activated grain.
type liveGrain struct {
	pid   gen.PID
	epoch store.Epoch
}

// catalog is a node's directory of live grains for one domain, plus the node
// incarnation the activators stamp keys with. Single-writer per field: live[key]
// is written only by the activator that owns key's shard; incarnation only by
// the lease-holder. Reads are lock-free.
type catalog struct {
	opts        Options
	live        sync.Map     // string(key) -> liveGrain
	incarnation atomic.Int64 // published by the lease-holder; 0 = not yet acquired
}

func catalogFor(node, domain gen.Atom) (*catalog, bool) {
	v, ok := catalogs.Load(catalogKey{node: node, domain: domain})
	if ok == false {
		return nil, false
	}
	return v.(*catalog), true
}

// whereisAt returns the live grain PID for a key. Lock-free read.
func whereisAt(node, domain gen.Atom, key string) (gen.PID, bool) {
	c, ok := catalogFor(node, domain)
	if ok == false {
		return gen.PID{}, false
	}
	v, ok := c.live.Load(key)
	if ok == false {
		return gen.PID{}, false
	}
	return v.(liveGrain).pid, true
}

func numActivatorsFor(node, domain gen.Atom) int {
	if v, ok := activatorCount.Load(catalogKey{node: node, domain: domain}); ok {
		if n := v.(int); n >= 1 {
			return n
		}
	}
	return DefaultActivators
}

func hashKey(key string, n int) int {
	h := fnv.New32a()
	h.Write([]byte(key))
	return int(h.Sum32() % uint32(n))
}

func activatorIndexFor(node, domain gen.Atom, key string) int {
	return hashKey(key, numActivatorsFor(node, domain))
}
