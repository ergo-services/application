package mcp

import "ergo.services/ergo/gen"

const (
	DefaultPort uint16 = 9922
	Version            = "1.0.0"
)

type Options struct {
	// Host for HTTP listener. Default: "localhost"
	Host string

	// Port for HTTP listener. 0 = agent mode (no HTTP, actor-only for cluster queries)
	Port uint16

	// CertManager for TLS
	CertManager gen.CertManager

	// Token for Bearer authentication. Empty = no auth
	Token string

	// ReadOnly disables action tools
	ReadOnly bool

	// AllowedTools whitelist. nil/empty = all tools enabled (respecting ReadOnly)
	AllowedTools []string

	// PoolSize is the number of worker processes in the tool execution pool (default: 5)
	PoolSize int

	// LogLevel for the MCP application processes
	LogLevel gen.LogLevel
}
