package observer

import (
	"fmt"

	"ergo.services/ergo/app/system"
	"ergo.services/ergo/gen"
)

const (
	DefaultPort     uint16 = 9911
	defaultHandlers int    = 3
)

func CreateApp(options Options) gen.ApplicationBehavior {
	if options.Port == 0 {
		options.Port = DefaultPort
	}
	if options.Handlers < 1 {
		options.Handlers = defaultHandlers
	}
	return &observerApp{
		options: options,
	}
}

type observerApp struct {
	options Options
}

type Options struct {
	Host       string
	Port       uint16
	Handlers   int
	LogLevel   gen.LogLevel
	Standalone bool
}

func (o *observerApp) Load(node gen.Node, args ...any) (gen.ApplicationSpec, error) {
	handlers := []gen.Atom{}
	for i := 1; i < o.options.Handlers+1; i++ {
		name := fmt.Sprintf("observer_handler%d", i)
		handlers = append(handlers, gen.Atom(name))
	}
	env := map[gen.Env]any{
		"handlers":   handlers,
		"port":       o.options.Port,
		"standalone": o.options.Standalone,
	}
	if o.options.Host != "" {
		env["host"] = o.options.Host
	}

	spec := gen.ApplicationSpec{
		Name:    "observer_app",
		Env:     env,
		Version: Version,
		Group: []gen.ApplicationMemberSpec{
			{
				Name:    "observer_backend",
				Factory: factory_web,
			},
			{
				Name:    "observer_handlers_sup",
				Factory: factory_handlers_sup,
			},
		},
	}

	if o.options.Standalone == false {
		spec.Depends = gen.ApplicationDepends{
			Applications: []gen.Atom{system.Name},
		}
	}

	return spec, nil
}

func (o *observerApp) Start(mode gen.ApplicationMode) {}
func (o *observerApp) Terminate(reason error)         {}
