package observer

import (
	"ergo.services/ergo/act"
	"ergo.services/ergo/gen"
)

func factory_post_pool() gen.ProcessBehavior {
	return &postPool{}
}

type postPool struct {
	act.Pool
}

func (p *postPool) Init(args ...any) (act.PoolOptions, error) {
	poolSize := int64(defaultPoolSize)
	if v, exist := p.Env("pool_size"); exist {
		if ps, ok := v.(int); ok && ps > 0 {
			poolSize = int64(ps)
		}
	}

	return act.PoolOptions{
		WorkerFactory: factory_post_worker,
		PoolSize:      poolSize,
	}, nil
}
