package observer

import (
	"ergo.services/ergo/app/system"
	"ergo.services/ergo/gen"
)

const (
	appName  gen.Atom = "observer_app"
	mgrName  gen.Atom = "observer_mgr"
	webName  gen.Atom = "observer_web"
	poolName gen.Atom = "observer_post_pool"
)

func Create(options Options) gen.ApplicationBehavior {
	if options.Port == 0 {
		options.Port = DefaultPort
	}
	if options.PoolSize < 1 {
		options.PoolSize = defaultPoolSize
	}
	return &app{options: options}
}

type app struct {
	options Options
}

func (a *app) Load(node gen.Node, args ...any) (gen.ApplicationSpec, error) {
	spec := gen.ApplicationSpec{
		Name:        appName,
		Description: "Observer Application v2 (SSE)",
		Version:     Version,
		Env: map[gen.Env]any{
			"port":      a.options.Port,
			"host":      a.options.Host,
			"pool_size": a.options.PoolSize,
		},
		Group: []gen.ApplicationMemberSpec{
			{
				Name:    mgrName,
				Factory: factory_mgr,
			},
			{
				Name:    poolName,
				Factory: factory_post_pool,
			},
			{
				Name:    webName,
				Factory: factory_web,
			},
		},
	}
	spec.Depends = gen.ApplicationDepends{
		Applications: []gen.Atom{system.Name},
	}
	return spec, nil
}

func (a *app) Start(mode gen.ApplicationMode) {}
func (a *app) Terminate(reason error)         {}
