package radar

import (
	"ergo.services/actor/health"
	"ergo.services/actor/metrics"
)

// Probe is re-exported from the health package so users don't need to import actor/health directly.
type Probe = health.Probe

const (
	ProbeLiveness  = health.ProbeLiveness
	ProbeReadiness = health.ProbeReadiness
	ProbeStartup   = health.ProbeStartup
)

// TopNOrder is re-exported from the metrics package so users don't need to import actor/metrics directly.
type TopNOrder = metrics.TopNOrder

const (
	TopNMax = metrics.TopNMax
	TopNMin = metrics.TopNMin
)
