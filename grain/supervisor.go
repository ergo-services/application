package grain

import (
	"ergo.services/ergo/act"
	"ergo.services/ergo/gen"
)

func factorySupervisor() gen.ProcessBehavior {
	return &supervisor{}
}

// supervisor is the grain runtime root: a lease-holder singleton plus N
// activator shards. The lease-holder is child 0 so it terminates last, keeping
// the node lease alive while activators drain their grains.
type supervisor struct {
	act.Supervisor

	domain gen.Atom
}

func (s *supervisor) Init(args ...any) (act.SupervisorSpec, error) {
	options := args[0].(Options)
	s.domain = options.Domain

	key := catalogKey{node: s.Node().Name(), domain: options.Domain}
	activatorCount.Store(key, options.Activators)
	catalogs.Store(key, &catalog{opts: options})

	children := make([]act.SupervisorChildSpec, 0, options.Activators+1)
	children = append(children, act.SupervisorChildSpec{
		Name:    leaseHolderName(options.Domain),
		Factory: factoryLeaseHolder,
		Args:    []any{options},
	})
	for i := 0; i < options.Activators; i++ {
		children = append(children, act.SupervisorChildSpec{
			Name:    activatorName(options.Domain, i),
			Factory: factoryActivator,
			Args:    []any{options, i},
		})
	}

	spec := act.SupervisorSpec{
		Type:     act.SupervisorTypeOneForOne,
		Children: children,
	}
	spec.Restart.Strategy = act.SupervisorStrategyTransient
	spec.Restart.Intensity = 5
	spec.Restart.Period = 5
	return spec, nil
}

func (s *supervisor) Terminate(reason error) {
	key := catalogKey{node: s.Node().Name(), domain: s.domain}
	catalogs.Delete(key)
	activatorCount.Delete(key)
}
