package mcp

import (
	"ergo.services/ergo/act"
	"ergo.services/ergo/gen"
)

const PoolName gen.Atom = "mcp"

func factoryMCPPool() gen.ProcessBehavior {
	return &MCPPool{}
}

// MCPPool distributes HTTP tool requests and remote ToolCallRequests to workers.
// Registered as "mcp" so remote nodes can Call ProcessID{"mcp", node}.
type MCPPool struct {
	act.Pool
}

func (p *MCPPool) Init(args ...any) (act.PoolOptions, error) {
	options := args[0].(Options)

	poolSize := int64(5)
	if options.PoolSize > 0 {
		poolSize = int64(options.PoolSize)
	}

	registry := newToolRegistry()
	registerNodeTools(registry)
	registerProcessTools(registry)
	registerAppTools(registry)
	registerEventTools(registry)
	registerNetworkTools(registry)
	registerCronTools(registry)
	registerRegistrarTools(registry)
	registerDebugTools(registry)
	registerSampleTools(registry)
	registerLogLevelTools(registry)
	if options.ReadOnly == false {
		registerActionTools(registry)
	}

	registry.filter(options.AllowedTools)

	return act.PoolOptions{
		WorkerFactory: factoryMCPWorker,
		PoolSize:      poolSize,
		WorkerArgs:    []any{registry, options},
	}, nil
}
