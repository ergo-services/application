package radar

import (
	"net/http"

	"ergo.services/actor/health"
	"ergo.services/actor/metrics"
	"ergo.services/ergo/app"
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
	// Normalized here rather than left to the actors: MetricsCollectInterval
	// also becomes the flush interval of every top-N actor, and that path has
	// no owner to default it - the value reaches the actor through the topN
	// supervisor verbatim.
	if options.MetricsCollectInterval < 1 {
		options.MetricsCollectInterval = metrics.DefaultCollectInterval
	}
	if options.MetricsTopN < 1 {
		options.MetricsTopN = metrics.DefaultTopN
	}
	return &radarApp{options: options}
}

type radarApp struct {
	app.Application
	options Options
}

func (a *radarApp) Load(args ...any) (gen.ApplicationSpec, error) {
	mux := http.NewServeMux()
	shared := metrics.NewShared()

	env := map[gen.Env]any{
		"mux":     mux,
		"shared":  shared,
		"options": a.options,
	}

	return gen.ApplicationSpec{
		Name:        Name,
		Description: "Prometheus metrics exporter and health check endpoints",
		Env:         env,
		// Both actors refuse to start without their wire types on the node, and
		// ApplicationLoad runs before any of this application's processes spawn.
		Network: gen.ApplicationNetwork{
			RegisterTypes:  append(health.NetworkTypes(), metrics.NetworkTypes()...),
			RegisterErrors: append(health.ErrorTypes(), metrics.ErrorTypes()...),
		},
		Group: []gen.ApplicationMemberSpec{
			{
				Name:    nameSup,
				Factory: factoryRadarSup,
			},
		},
	}, nil
}
