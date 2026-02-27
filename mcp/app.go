package mcp

import (
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
	options Options
}

func (a *mcpApp) Load(node gen.Node, args ...any) (gen.ApplicationSpec, error) {
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

func (a *mcpApp) Start(mode gen.ApplicationMode) {}
func (a *mcpApp) Terminate(reason error)         {}
