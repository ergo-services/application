package grain

import (
	"errors"
	"fmt"
	"time"

	"ergo.services/application/grain/store"
	"ergo.services/ergo/app"
	"ergo.services/ergo/gen"
)

const (
	DefaultDomain         gen.Atom      = "default"
	DefaultActivators     int           = 8
	DefaultLeaseTTL       int64         = 30              // node lease TTL, seconds
	DefaultRenewInterval  time.Duration = 8 * time.Second // heartbeat interval
	DefaultIdleTimeout    time.Duration = 5 * time.Minute // grain idle deactivation
	DefaultSyncEvery      int           = 0               // 0 = flush only on deactivate/shutdown
	DefaultStoreIOTimeout time.Duration = 3 * time.Second // per-call Store deadline
	DefaultGrainInitSecs  int           = 5               // grain ProcessOptions.InitTimeout (no Store IO here)
	DefaultSupInitSecs    int           = 15              // supervisor member init window
	DefaultActivateSecs   int           = 20              // caller Call budget, seconds
	Version               string        = "0.1.0"

	maxActivateAttempts = 6
	maxActivateWall     = 4 * time.Second
)

// Options configures a grain application instance.
type Options struct {
	// Domain scopes the runtime; the Ergo application name is "grain_<Domain>".
	// Several domains coexist on one node. Default: "default".
	Domain gen.Atom
	// Store is the CP ownership-and-persistence backend. Required.
	Store store.Backend
	// Factory returns a fresh grain for each activation. Required.
	Factory func() ActorBehavior
	// Activators is the activator shard count. Default: 8.
	Activators int
	// LeaseTTL is the node lease lifetime in seconds. Default: 30.
	LeaseTTL int64
	// RenewInterval is the heartbeat period. Default: 8s. Kept below LeaseTTL/2.
	RenewInterval time.Duration
	// IdleTimeout deactivates a grain after this idle span. Default: 5m.
	IdleTimeout time.Duration
	// SyncEvery persists a grain every N handled messages; 0 persists only on
	// deactivation and shutdown. Default: 0.
	SyncEvery int
	// StoreIOTimeout bounds every Store call. Default: 3s.
	StoreIOTimeout time.Duration
	// GrainInitSecs is the grain init window in seconds. Default: 5.
	GrainInitSecs int
	// ActivateSecs is the Activate/Delete Call budget in seconds. Default: 20.
	ActivateSecs int
}

func applyDefaults(o Options) Options {
	if o.Domain == "" {
		o.Domain = DefaultDomain
	}
	if o.Activators < 1 {
		o.Activators = DefaultActivators
	}
	if o.LeaseTTL < 1 {
		o.LeaseTTL = DefaultLeaseTTL
	}
	if o.RenewInterval <= 0 {
		o.RenewInterval = DefaultRenewInterval
	}
	if o.IdleTimeout <= 0 {
		o.IdleTimeout = DefaultIdleTimeout
	}
	if o.StoreIOTimeout <= 0 {
		o.StoreIOTimeout = DefaultStoreIOTimeout
	}
	if o.GrainInitSecs < 1 {
		o.GrainInitSecs = DefaultGrainInitSecs
	}
	if o.ActivateSecs < 1 {
		o.ActivateSecs = DefaultActivateSecs
	}
	// A missed renew plus its own latency must stay within half the lease so two
	// beats never expire it.
	if half := time.Duration(o.LeaseTTL) * time.Second / 2; o.RenewInterval+o.StoreIOTimeout > half {
		o.RenewInterval = half - o.StoreIOTimeout
	}
	return o
}

// CreateApp creates the grain application: a CP virtual-actor / durable-object
// runtime over the given Store.
func CreateApp(options Options) gen.ApplicationBehavior {
	return &grainApp{options: applyDefaults(options)}
}

type grainApp struct {
	app.Application
	options Options
}

func (a *grainApp) Load(args ...any) (gen.ApplicationSpec, error) {
	if a.options.Store == nil {
		return gen.ApplicationSpec{}, errors.New("grain: Store is required")
	}
	if a.options.Factory == nil {
		return gen.ApplicationSpec{}, errors.New("grain: Factory is required")
	}
	name := appName(a.options.Domain)
	return gen.ApplicationSpec{
		Name:        name,
		Description: "Grain CP virtual-actor / durable-object runtime",
		Version:     gen.Version{Name: string(name), Release: Version},
		Mode:        gen.ApplicationModePermanent,
		Group: []gen.ApplicationMemberSpec{
			{
				Name:    supName(a.options.Domain),
				Factory: factorySupervisor,
				Args:    []any{a.options},
				Options: gen.ProcessOptions{InitTimeout: DefaultSupInitSecs},
			},
		},
	}, nil
}

func appName(domain gen.Atom) gen.Atom         { return "grain_" + domain }
func supName(domain gen.Atom) gen.Atom         { return appName(domain) + "_sup" }
func leaseHolderName(domain gen.Atom) gen.Atom { return appName(domain) + "_lease" }
func activatorName(domain gen.Atom, i int) gen.Atom {
	return gen.Atom(fmt.Sprintf("%s_activator_%d", appName(domain), i))
}
func grainName(domain gen.Atom, key string) gen.Atom {
	return gen.Atom(fmt.Sprintf("%s_grain_%s", appName(domain), key))
}
