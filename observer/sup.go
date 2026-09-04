package observer

import (
	"errors"

	"ergo.services/ergo/act"
	"ergo.services/ergo/gen"
)

func factory_sup() gen.ProcessBehavior {
	return &sup{}
}

type sup struct {
	act.Supervisor
}

func (s *sup) Init(args ...any) (act.SupervisorSpec, error) {
	listeners, _ := s.Env(envListeners)
	endpoints, ok := listeners.([]Listener)
	if ok == false || len(endpoints) == 0 {
		return act.SupervisorSpec{}, errors.New("no listener to serve")
	}

	spec := act.SupervisorSpec{
		Type: act.SupervisorTypeOneForOne,
		Children: []act.SupervisorChildSpec{
			{
				Name:    managerName,
				Factory: factory_manager,
			},
			{
				Name:    poolName,
				Factory: factory_post_pool,
			},
			{
				Name:    clusterName,
				Factory: factory_cluster,
			},
		},
	}
	for _, endpoint := range endpoints {
		spec.Children = append(spec.Children, act.SupervisorChildSpec{
			Name:    webName(endpoint.Port),
			Factory: factory_web,
			Args:    []any{endpoint},
		})
	}
	spec.Restart.Strategy = act.SupervisorStrategyPermanent
	return spec, nil
}
