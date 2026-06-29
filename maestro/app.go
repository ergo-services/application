package maestro

import (
	"ergo.services/ergo/app"
	"ergo.services/ergo/gen"
)

// CreateApp returns an ApplicationBehavior that provides durable saga
// orchestration: it journals each run, runs it as a supervised saga instance,
// and re-drives incomplete runs after a restart.
func CreateApp(options Options) gen.ApplicationBehavior {
	if options.Journal == nil {
		options.Journal = NewMemoryJournal()
	}
	return &maestroApp{options: options}
}

type maestroApp struct {
	app.Application
	options Options
}

func (a *maestroApp) Load(args ...any) (gen.ApplicationSpec, error) {
	return gen.ApplicationSpec{
		Name:        Name,
		Description: "Durable saga orchestration",
		Env: map[gen.Env]any{
			envOptions: a.options,
		},
		Group: []gen.ApplicationMemberSpec{
			{
				Name:    nameSup,
				Factory: factorySup,
			},
		},
	}, nil
}
