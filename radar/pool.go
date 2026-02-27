package radar

import (
	"fmt"

	"ergo.services/ergo/act"
	"ergo.services/ergo/gen"
)

type metricsPool struct {
	act.Pool
}

func factoryMetricsPool() gen.ProcessBehavior {
	return &metricsPool{}
}

func (p *metricsPool) Init(args ...any) (act.PoolOptions, error) {
	if len(args) == 0 {
		return act.PoolOptions{}, fmt.Errorf("radar: missing pool options")
	}
	opts, ok := args[0].(act.PoolOptions)
	if ok == false {
		return act.PoolOptions{}, fmt.Errorf("radar: args[0] is not act.PoolOptions")
	}
	return opts, nil
}
