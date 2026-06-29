package maestro

import (
	"fmt"

	"ergo.services/ergo/act"
	"ergo.services/ergo/gen"
)

type maestroSup struct {
	act.Supervisor
}

func factorySup() gen.ProcessBehavior { return &maestroSup{} }

func (s *maestroSup) Init(args ...any) (act.SupervisorSpec, error) {
	v, exist := s.Env(envOptions)
	if exist == false {
		return act.SupervisorSpec{}, fmt.Errorf("maestro: missing %q in env", envOptions)
	}
	options, ok := v.(Options)
	if ok == false {
		return act.SupervisorSpec{}, fmt.Errorf("maestro: %q is not maestro.Options", envOptions)
	}

	spec := act.SupervisorSpec{
		Type: act.SupervisorTypeOneForOne,
		Restart: act.SupervisorRestart{
			Strategy: act.SupervisorStrategyPermanent,
		},
		Children: []act.SupervisorChildSpec{
			{
				Name:    nameManager,
				Factory: factoryManager,
				Args:    []any{options},
			},
		},
	}
	return spec, nil
}
