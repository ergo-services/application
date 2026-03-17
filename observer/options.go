package observer

import "ergo.services/ergo/gen"

const (
	DefaultPort       uint16 = 9911
	defaultPoolSize   int    = 10
	defaultCallTimeout int   = 5 // seconds
)

type Options struct {
	// Host for HTTP listener. Default: "localhost"
	Host string

	// Port for HTTP listener. Default: 9911
	Port uint16

	// PoolSize is the number of POST request workers. Default: 10
	PoolSize int

	// LogLevel for the observer processes
	LogLevel gen.LogLevel
}
