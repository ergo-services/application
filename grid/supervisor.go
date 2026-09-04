package grid

import (
	"ergo.services/ergo/act"
	"ergo.services/ergo/gen"
)

func factorySupervisor() gen.ProcessBehavior {
	return &supervisor{}
}

// supervisor is grid's root supervisor.
type supervisor struct {
	act.Supervisor

	domain gen.Atom
}

func (s *supervisor) Init(args ...any) (act.SupervisorSpec, error) {
	options := args[0].(Options)
	s.domain = options.Domain

	registryShards.Store(storeKey{node: s.Node().Name(), domain: options.Domain}, options.Shards)
	stores.Store(storeKey{node: s.Node().Name(), domain: options.Domain}, &storeData{})

	children := make([]act.SupervisorChildSpec, 0, options.Shards)
	for i := 0; i < options.Shards; i++ {
		children = append(children, act.SupervisorChildSpec{
			Name:    shardName(options.Domain, i),
			Factory: factoryShard,
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
	stores.Delete(storeKey{node: s.Node().Name(), domain: s.domain})
	registryShards.Delete(storeKey{node: s.Node().Name(), domain: s.domain})
}
