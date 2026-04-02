package pulse

import (
	"time"

	"ergo.services/ergo/gen"
)

const (
	DefaultEndpoint      = "localhost:4318"
	DefaultURLPath       = "/v1/traces"
	DefaultBatchSize     = 512
	DefaultFlushInterval = 5 * time.Second
	DefaultPoolSize      = 3
	DefaultExportTimeout = 10 * time.Second
)

// Options configures the Pulse OTLP/HTTP tracing exporter application.
type Options struct {
	// Endpoint is the OTLP collector address in host:port format.
	// Default: "localhost:4318"
	Endpoint string

	// Insecure disables TLS (uses http instead of https).
	// Default: false
	Insecure bool

	// Headers are added to every export HTTP request.
	// Use for authentication tokens, API keys, etc.
	Headers map[string]string

	// BatchSize is the maximum number of spans per export batch.
	// Flush triggers when this count is reached.
	// Default: 512
	BatchSize int

	// FlushInterval is the maximum time between batch exports.
	// Default: 5 seconds
	FlushInterval time.Duration

	// PoolSize is the number of export worker actors.
	// Default: 3
	PoolSize int

	// ExportTimeout is the HTTP request timeout per export.
	// Default: 10 seconds
	ExportTimeout time.Duration

	// Flags controls which span kinds to export.
	// Default: TracingFlagSend | TracingFlagReceive | TracingFlagProcs
	Flags gen.TracingFlags
}

func applyDefaults(o Options) Options {
	if o.Endpoint == "" {
		o.Endpoint = DefaultEndpoint
	}
	if o.BatchSize < 1 {
		o.BatchSize = DefaultBatchSize
	}
	if o.FlushInterval <= 0 {
		o.FlushInterval = DefaultFlushInterval
	}
	if o.PoolSize < 1 {
		o.PoolSize = DefaultPoolSize
	}
	if o.ExportTimeout <= 0 {
		o.ExportTimeout = DefaultExportTimeout
	}
	if o.Flags == 0 {
		o.Flags = gen.TracingFlagSend | gen.TracingFlagReceive | gen.TracingFlagProcs
	}
	return o
}
