package radar

import (
	"fmt"
	"time"

	"ergo.services/actor/metrics"
	"ergo.services/ergo/act"
	"ergo.services/ergo/gen"
)

type topNSup struct {
	act.Supervisor

	shared   *metrics.Shared
	interval time.Duration
}

func factoryTopNSup() gen.ProcessBehavior {
	return &topNSup{}
}

func (s *topNSup) Init(args ...any) (act.SupervisorSpec, error) {
	if len(args) < 2 {
		return act.SupervisorSpec{}, fmt.Errorf("topn sup: expected 2 args, got %d", len(args))
	}

	var ok bool
	s.shared, ok = args[0].(*metrics.Shared)
	if ok == false {
		return act.SupervisorSpec{}, fmt.Errorf("topn sup: args[0] is not *metrics.Shared")
	}
	s.interval, ok = args[1].(time.Duration)
	if ok == false {
		return act.SupervisorSpec{}, fmt.Errorf("topn sup: args[1] is not time.Duration")
	}

	spec := act.SupervisorSpec{
		Type: act.SupervisorTypeSimpleOneForOne,
		Restart: act.SupervisorRestart{
			Strategy: act.SupervisorStrategyTransient,
		},
		Children: []act.SupervisorChildSpec{
			{
				Name:    "topn_worker",
				Factory: metrics.TopNActorFactory,
			},
		},
	}

	return spec, nil
}

func (s *topNSup) HandleCall(from gen.PID, ref gen.Ref, request any) (any, error) {
	switch msg := request.(type) {
	case metrics.RegisterTopNRequest:
		err := s.StartChild("topn_worker", metrics.TopNActorOptions{
			Name:     msg.Name,
			Help:     msg.Help,
			Labels:   msg.Labels,
			TopN:     msg.TopN,
			Order:    msg.Order,
			Shared:   s.shared,
			Interval: s.interval,
			Owner:    from,
		})
		if err != nil {
			return metrics.RegisterResponse{Error: err.Error()}, nil
		}
		return metrics.RegisterResponse{}, nil
	}

	return nil, nil
}

func (s *topNSup) HandleMessage(from gen.PID, message any) error {
	return nil
}

func (s *topNSup) HandleEvent(event gen.MessageEvent) error {
	return nil
}

func (s *topNSup) HandleInspect(from gen.PID, item ...string) map[string]string {
	return nil
}
