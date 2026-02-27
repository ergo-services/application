package radar

import "ergo.services/actor/health"

// Probe is re-exported from the health package so users don't need to import actor/health directly.
type Probe = health.Probe

const (
	ProbeLiveness  = health.ProbeLiveness
	ProbeReadiness = health.ProbeReadiness
	ProbeStartup   = health.ProbeStartup
)
