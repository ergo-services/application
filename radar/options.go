package radar

import "time"

// Options configures the Radar application.
type Options struct {
	// Host for the shared HTTP server. Default: "localhost".
	Host string
	// Port for the shared HTTP server. Default: 9090.
	Port uint16

	// HealthPath is the URL prefix for health endpoints. Default: "/health".
	HealthPath string
	// MetricsPath is the URL path for the Prometheus metrics endpoint. Default: "/metrics".
	MetricsPath string

	// HealthCheckInterval is the heartbeat timeout check interval for the health actor.
	HealthCheckInterval time.Duration
	// MetricsCollectInterval is the interval for collecting base Ergo metrics.
	MetricsCollectInterval time.Duration
	// MetricsTopN controls the top-N process metrics to collect.
	MetricsTopN int
	// MetricsPoolSize is the number of pool workers for custom metrics. Default: 3.
	MetricsPoolSize int64
}
