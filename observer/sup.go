package observer

import (
	"errors"

	"ergo.services/ergo/act"
	"ergo.services/ergo/gen"
)

func factory_handlers_sup() gen.ProcessBehavior {
	return &handlers_sup{}
}

type handlers_sup struct {
	act.Supervisor
}

func (hs *handlers_sup) Init(args ...any) (act.SupervisorSpec, error) {
	spec := act.SupervisorSpec{
		Type: act.SupervisorTypeOneForOne,
		Restart: act.SupervisorRestart{
			Strategy: act.SupervisorStrategyPermanent,
		},
	}

	v, _ := hs.Env("handlers")
	handlers, _ := v.([]gen.Atom)
	if len(handlers) == 0 {
		return spec, errors.New("no handlers in the handlers pool")
	}

	for _, handler := range handlers {
		child := act.SupervisorChildSpec{
			Name:    handler,
			Factory: factory_observer_handler,
		}
		spec.Children = append(spec.Children, child)
	}

	return spec, nil
}
