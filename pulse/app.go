package pulse

import (
	"ergo.services/ergo/app"
	"ergo.services/ergo/gen"
)

const (
	Name    gen.Atom = "pulse"
	Version          = "0.1.0"

	poolName gen.Atom = "pulse_pool"
)

// CreateApp creates the Pulse OTLP/HTTP tracing exporter application.
func CreateApp(options Options) gen.ApplicationBehavior {
	return &pulseApp{options: applyDefaults(options)}
}

type pulseApp struct {
	app.Application
	options Options
}

func (a *pulseApp) Load(args ...any) (gen.ApplicationSpec, error) {
	return gen.ApplicationSpec{
		Name:        Name,
		Description: "Pulse OTLP/HTTP Tracing Exporter",
		Version: gen.Version{
			Name:    string(Name),
			Release: Version,
		},
		Mode: gen.ApplicationModePermanent,
		Group: []gen.ApplicationMemberSpec{
			{
				Name:    poolName,
				Factory: factoryPool,
				Args:    []any{a.options},
			},
		},
	}, nil
}
