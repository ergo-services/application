package mcp

import (
	"ergo.services/ergo/act"
	"ergo.services/ergo/gen"
)

func factorySup() gen.ProcessBehavior {
	return &mcpSup{}
}

type mcpSup struct {
	act.Supervisor
}

func (s *mcpSup) Init(args ...any) (act.SupervisorSpec, error) {
	options := args[0].(Options)

	return act.SupervisorSpec{
		Type: act.SupervisorTypeOneForOne,
		Restart: act.SupervisorRestart{
			Strategy: act.SupervisorStrategyPermanent,
		},
		Children: []act.SupervisorChildSpec{
			{
				Name:    PoolName,
				Factory: factoryMCPPool,
				Args:    []any{options},
			},
			{
				Name:    WebName,
				Factory: factoryMCPWeb,
				Args:    []any{options},
			},
		},
	}, nil
}
