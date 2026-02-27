package radar

import (
	"net/http"

	"ergo.services/actor/metrics"
	"ergo.services/ergo/gen"
)

// CreateApp returns an ApplicationBehavior that bundles health checks and
// Prometheus metrics into a single HTTP endpoint.
func CreateApp(options Options) gen.ApplicationBehavior {
	if options.Host == "" {
		options.Host = DefaultHost
	}
	if options.Port == 0 {
		options.Port = DefaultPort
	}
	if options.HealthPath == "" {
		options.HealthPath = DefaultHealthPath
	}
	if options.MetricsPath == "" {
		options.MetricsPath = DefaultMetricsPath
	}
	if options.MetricsPoolSize < 1 {
		options.MetricsPoolSize = DefaultPoolSize
	}
	return &radarApp{options: options}
}

type radarApp struct {
	options Options
}

func (a *radarApp) Load(node gen.Node, args ...any) (gen.ApplicationSpec, error) {
	mux := http.NewServeMux()
	shared := metrics.NewShared()

	env := map[gen.Env]any{
		"mux":     mux,
		"shared":  shared,
		"options": a.options,
	}

	return gen.ApplicationSpec{
		Name: Name,
		Env:  env,
		Group: []gen.ApplicationMemberSpec{
			{
				Name:    nameSup,
				Factory: factoryRadarSup,
			},
		},
	}, nil
}

func (a *radarApp) Start(mode gen.ApplicationMode) {}
func (a *radarApp) Terminate(reason error)         {}
