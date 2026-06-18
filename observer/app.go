package observer

import (
	"ergo.services/ergo/app"
	"ergo.services/ergo/app/system"
	"ergo.services/ergo/gen"
)

const (
	appName     gen.Atom = "observer_app"
	supName     gen.Atom = "observer_sup"
	managerName gen.Atom = "observer_manager"
	webName     gen.Atom = "observer_web"
	poolName    gen.Atom = "observer_post_pool"
)

func CreateApp(options Options) gen.ApplicationBehavior {
	if options.Port == 0 {
		options.Port = DefaultPort
	}
	if options.PoolSize < 1 {
		options.PoolSize = defaultPoolSize
	}
	return &observerApp{options: options}
}

type observerApp struct {
	app.Application
	options Options
}

func (a *observerApp) Load(args ...any) (gen.ApplicationSpec, error) {
	spec := gen.ApplicationSpec{
		Name:        appName,
		Description: "Observer Application v2 (SSE)",
		Version:     Version,
		Mode:        gen.ApplicationModePermanent,
		Env: map[gen.Env]any{
			"port":      a.options.Port,
			"host":      a.options.Host,
			"pool_size": a.options.PoolSize,
		},
		Group: []gen.ApplicationMemberSpec{
			{
				Name:    supName,
				Factory: factory_sup,
			},
		},
	}
	spec.Depends = gen.ApplicationDepends{
		Applications: []gen.Atom{system.Name},
	}
	return spec, nil
}
