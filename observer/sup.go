package observer

import (
	"ergo.services/ergo/act"
	"ergo.services/ergo/gen"
)

func factory_sup() gen.ProcessBehavior {
	return &sup{}
}

// sup supervises the observer components. OneForOne: a component that dies is
// restarted on its own. They are decoupled by registered name (web routes to
// managerName/poolName by name), so restarting one does not require restarting
// the others. On a crash loop beyond the restart intensity the supervisor
// terminates and the permanent application stops; bringing it back is left to
// the surrounding orchestration.
type sup struct {
	act.Supervisor
}

func (s *sup) Init(args ...any) (act.SupervisorSpec, error) {
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
				Name:    webName,
				Factory: factory_web,
			},
		},
	}
	spec.Restart.Strategy = act.SupervisorStrategyPermanent
	return spec, nil
}
