package pulse

import (
	"ergo.services/ergo/act"
	"ergo.services/ergo/gen"
)

func factoryPool() gen.ProcessBehavior {
	return &pool{}
}

type pool struct {
	act.Pool

	options Options
}

func (p *pool) Init(args ...any) (act.PoolOptions, error) {
	p.options = args[0].(Options)

	// register as PID-based tracing exporter
	err := p.Node().TracingExporterAddPID(p.PID(), string(Name), p.options.Flags)
	if err != nil {
		return act.PoolOptions{}, err
	}
	p.Log().Info("pulse: registered as tracing exporter (pool=%d, batch=%d, flush=%s)",
		p.options.PoolSize, p.options.BatchSize, p.options.FlushInterval)

	opts := act.PoolOptions{
		PoolSize:      int64(p.options.PoolSize),
		WorkerFactory: factoryWorker,
		WorkerArgs:    []any{p.options},
	}

	return opts, nil
}

func (p *pool) HandleMessage(from gen.PID, message any) error {
	return nil
}

func (p *pool) HandleCall(from gen.PID, ref gen.Ref, request any) (any, error) {
	return nil, nil
}

func (p *pool) Terminate(reason error) {
	p.Node().TracingExporterDeletePID(p.PID())
	p.Log().Info("pulse pool terminated: %s", reason)
}

func (p *pool) HandleEvent(message gen.MessageEvent) error {
	return nil
}

func (p *pool) HandleInspect(from gen.PID, item ...string) map[string]string {
	return nil
}
