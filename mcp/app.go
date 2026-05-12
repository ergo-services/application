package mcp

import (
	"fmt"

	"ergo.services/ergo/app"
	"ergo.services/ergo/gen"
)

const AppName gen.Atom = "mcp_app"

// CreateApp creates the MCP application with the given options.
// If Port > 0, the application starts in entry point mode with HTTP listener.
// If Port == 0, the application starts in agent mode (actor only, no HTTP).
func CreateApp(options Options) gen.ApplicationBehavior {
	if options.Host == "" {
		options.Host = "localhost"
	}
	return &mcpApp{options: options}
}

type mcpApp struct {
	app.Application
	options Options
}

func (a *mcpApp) Load(args ...any) (gen.ApplicationSpec, error) {
	err := a.Node().Network().RegisterTypes([]any{
		ToolCallRequest{},
		ToolCallResponse{},
	})
	if err != nil {
		return gen.ApplicationSpec{}, fmt.Errorf("mcp types: %w", err)
	}
	return gen.ApplicationSpec{
		Name: AppName,
		Group: []gen.ApplicationMemberSpec{
			{
				Name:    "mcp_sup",
				Factory: factorySup,
				Args:    []any{a.options},
			},
		},
		LogLevel: a.options.LogLevel,
	}, nil
}
