package grid

import (
	"hash/fnv"
	"sync"

	"ergo.services/ergo/gen"
)

// stores maps {node, domain} to its registry data.
var stores sync.Map // storeKey -> *storeData

// registryShards maps {node, domain} to its shard count.
var registryShards sync.Map // storeKey -> int

type storeKey struct {
	node   gen.Atom
	domain gen.Atom
}

type entry struct {
	owner gen.PID
	meta  any
	time  int64
}

// storeData holds a node's registry and subscriptions.
type storeData struct {
	regByKey sync.Map // string -> entry
	watchers sync.Map // int (shard index) -> *shardWatch
}

func (d *storeData) lookup(key string) (gen.PID, any, bool) {
	v, ok := d.regByKey.Load(key)
	if ok == false {
		return gen.PID{}, nil, false
	}
	e := v.(entry)
	return e.owner, e.meta, true
}

func (d *storeData) count() int {
	n := 0
	d.regByKey.Range(func(_, _ any) bool {
		n++
		return true
	})
	return n
}

func (d *storeData) localCount(node gen.Atom) int {
	n := 0
	d.regByKey.Range(func(_, v any) bool {
		if v.(entry).owner.Node == node {
			n++
		}
		return true
	})
	return n
}

func (d *storeData) localEntries(node gen.Atom) []Entry {
	var out []Entry
	d.regByKey.Range(func(k, v any) bool {
		e := v.(entry)
		if e.owner.Node == node {
			out = append(out, Entry{Key: k.(string), Owner: e.owner, Meta: e.meta})
		}
		return true
	})
	return out
}

func storeFor(node, domain gen.Atom) (*storeData, bool) {
	v, ok := stores.Load(storeKey{node: node, domain: domain})
	if ok == false {
		return nil, false
	}
	return v.(*storeData), true
}

func lookupAt(node, domain gen.Atom, key string) (gen.PID, any, bool) {
	d, ok := storeFor(node, domain)
	if ok == false {
		return gen.PID{}, nil, false
	}
	return d.lookup(key)
}

func numShardsFor(node, domain gen.Atom) int {
	if v, ok := registryShards.Load(storeKey{node: node, domain: domain}); ok {
		if n := v.(int); n >= 1 {
			return n
		}
	}
	return DefaultShards
}

func hashKey(key string, numShards int) int {
	h := fnv.New32a()
	h.Write([]byte(key))
	return int(h.Sum32() % uint32(numShards))
}

func shardIndexFor(node, domain gen.Atom, key string) int {
	return hashKey(key, numShardsFor(node, domain))
}
