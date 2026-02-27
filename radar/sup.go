package radar

import (
	"fmt"
	"net/http"

	"ergo.services/actor/health"
	"ergo.services/actor/metrics"
	"ergo.services/ergo/act"
	"ergo.services/ergo/gen"
)

type radarSup struct {
	act.Supervisor
}

func factoryRadarSup() gen.ProcessBehavior {
	return &radarSup{}
}

func (s *radarSup) Init(args ...any) (act.SupervisorSpec, error) {
	v, exist := s.Env("mux")
	if exist == false {
		return act.SupervisorSpec{}, fmt.Errorf("radar: missing 'mux' in env")
	}
	mux, ok := v.(*http.ServeMux)
	if ok == false {
		return act.SupervisorSpec{}, fmt.Errorf("radar: 'mux' is not *http.ServeMux")
	}

	v, exist = s.Env("shared")
	if exist == false {
		return act.SupervisorSpec{}, fmt.Errorf("radar: missing 'shared' in env")
	}
	shared, ok := v.(*metrics.Shared)
	if ok == false {
		return act.SupervisorSpec{}, fmt.Errorf("radar: 'shared' is not *metrics.Shared")
	}

	v, exist = s.Env("options")
	if exist == false {
		return act.SupervisorSpec{}, fmt.Errorf("radar: missing 'options' in env")
	}
	options, ok := v.(Options)
	if ok == false {
		return act.SupervisorSpec{}, fmt.Errorf("radar: 'options' is not radar.Options")
	}

	healthOpts := health.Options{
		Mux:           mux,
		Path:          options.HealthPath,
		CheckInterval: options.HealthCheckInterval,
	}

	primaryMetricsOpts := metrics.Options{
		Mux:             mux,
		Shared:          shared,
		Path:            options.MetricsPath,
		CollectInterval: options.MetricsCollectInterval,
		TopN:            options.MetricsTopN,
	}

	workerMetricsOpts := metrics.Options{
		Shared: shared,
	}

	poolOpts := act.PoolOptions{
		PoolSize:      options.MetricsPoolSize,
		WorkerFactory: metrics.Factory,
		WorkerArgs:    []any{workerMetricsOpts},
	}

	spec := act.SupervisorSpec{
		Type: act.SupervisorTypeOneForOne,
		Restart: act.SupervisorRestart{
			Strategy: act.SupervisorStrategyPermanent,
		},
		Children: []act.SupervisorChildSpec{
			{
				Name:    nameHealth,
				Factory: health.Factory,
				Args:    []any{healthOpts},
			},
			{
				Name:    nameMetricsErgo,
				Factory: metrics.Factory,
				Args:    []any{primaryMetricsOpts},
			},
			{
				Name:    nameMetrics,
				Factory: factoryMetricsPool,
				Args:    []any{poolOpts},
			},
			{
				Name:    nameWeb,
				Factory: factoryWeb,
				Args:    []any{mux, options.Host, options.Port},
			},
		},
	}

	return spec, nil
}
