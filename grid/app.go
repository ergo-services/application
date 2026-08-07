package grid

import (
	"fmt"

	"ergo.services/ergo/app"
	"ergo.services/ergo/gen"
)

const (
	DefaultDomain    gen.Atom = "default"
	DefaultShards    int      = 8
	DefaultSeparator string   = "/"
	Version          string   = "0.1.0"
)

// Options configures a grid application instance.
type Options struct {
	// Domain is the peering domain; a node peers only with grids of the same
	// Domain. The Ergo application name is "grid_<Domain>". Default: "default".
	Domain gen.Atom
	// Shards is the keyspace shard count; all nodes in a domain must agree.
	// Default: 8.
	Shards int
	// Separator is the key hierarchy separator used by MonitorPrefix: a prefix
	// matches the key itself and everything below it at a separator boundary, so
	// MonitorPrefix("a/b") matches "a/b" and "a/b/c" but not "a/bc". Default: "/".
	Separator string
	// Peers are optional static seed nodes for discovery without a registrar.
	Peers []gen.Atom
}

func applyDefaults(o Options) Options {
	if o.Domain == "" {
		o.Domain = DefaultDomain
	}
	if o.Shards < 1 {
		o.Shards = DefaultShards
	}
	if o.Separator == "" {
		o.Separator = DefaultSeparator
	}
	return o
}

// CreateApp creates the grid application: an AP eventually-consistent
// distributed in-memory store and registry.
func CreateApp(options Options) gen.ApplicationBehavior {
	return &gridApp{options: applyDefaults(options)}
}

type gridApp struct {
	app.Application
	options Options
}

func (a *gridApp) Load(args ...any) (gen.ApplicationSpec, error) {
	name := appName(a.options.Domain)
	return gen.ApplicationSpec{
		Name:        name,
		Description: "Grid AP distributed in-memory store/registry",
		Version:     gen.Version{Name: string(name), Release: Version},
		Mode:        gen.ApplicationModePermanent,
		Network: gen.ApplicationNetwork{
			RegisterTypes: []any{
				messagePeerConnect{}, messagePeerConnectAck{},
				messageRegister{}, messageUnregister{}, messageClusterState{}, regEntry{},
				UnregisterReason(0),
			},
		},
		Group: []gen.ApplicationMemberSpec{
			{Name: supName(a.options.Domain), Factory: factorySupervisor, Args: []any{a.options}},
		},
	}, nil
}

func appName(domain gen.Atom) gen.Atom { return "grid_" + domain }
func supName(domain gen.Atom) gen.Atom { return appName(domain) + "_sup" }
func shardName(domain gen.Atom, i int) gen.Atom {
	return gen.Atom(fmt.Sprintf("%s_shard_%d", appName(domain), i))
}
