package observer

import (
	"fmt"

	"ergo.services/ergo/app"
	"ergo.services/ergo/app/system"
	"ergo.services/ergo/gen"
)

// envCeiling is the fallback for a request that arrives with no identity on it.
const envCeiling gen.Env = "observer_ceiling"

const envListeners gen.Env = "observer_listeners"

const envEnrollment gen.Env = "observer_enrollment"

// envJobMaxRetention is the ceiling on how long a finished run keeps its result.
const envJobMaxRetention gen.Env = "observer_job_max_retention"

const envJobLimit gen.Env = "observer_job_limit"

const (
	appName     gen.Atom = "observer_app"
	supName     gen.Atom = "observer_sup"
	managerName gen.Atom = "observer_manager"
	poolName    gen.Atom = "observer_post_pool"
)

// webName names a listener's web process by its port.
func webName(port uint16) gen.Atom {
	return gen.Atom(fmt.Sprintf("observer_web_%d", port))
}

func CreateApp(options Options) gen.ApplicationBehavior {
	if options.PoolSize < 1 {
		options.PoolSize = defaultPoolSize
	}
	options.ClusterLens = options.ClusterLens.withDefaults()
	return &observerApp{options: options}
}

type observerApp struct {
	app.Application
	options Options
}

func (a *observerApp) Load(args ...any) (gen.ApplicationSpec, error) {
	listeners, err := a.options.listeners()
	if err != nil {
		return gen.ApplicationSpec{}, err
	}

	spec := gen.ApplicationSpec{
		Name:        appName,
		Description: "Observer Application v2 (SSE)",
		Version:     Version,
		Mode:        gen.ApplicationModePermanent,
		Env: map[gen.Env]any{
			"pool_size":           a.options.PoolSize,
			envCeiling:            a.options.Ceiling,
			envListeners:          listeners,
			envEnrollment:         a.options.Enrollment,
			envJobMaxRetention:    a.options.JobMaxRetention,
			envJobLimit:           a.options.JobLimit,
			envClusterLensOptions: a.options.ClusterLens,
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
