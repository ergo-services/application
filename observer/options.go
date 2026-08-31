package observer

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"ergo.services/application/observer/access"
	"ergo.services/ergo/gen"
)

// The vocabulary of authorization lives in the access package so an authorizer can be a
// module of its own. These are the same types, spelled the short way.
type (
	Ceiling    = access.Ceiling
	Identity   = access.Identity
	Authorizer = access.Authorizer
)

// Page origins of the ergo.observer cloud UI, for Listener.AllowedOrigins. Never allowed by
// default. The wildcard covers one label, not the parent.
const (
	ErgoOrigin        = "https://app.ergo.observer"
	ErgoOriginTenants = "https://*.app.ergo.observer"
)

const (
	DefaultPort        uint16 = 9911
	defaultHost        string = "localhost"
	defaultPoolSize    int    = 25
	defaultCallTimeout int    = 5 // seconds

	defaultMaxStreams       int = 64
	defaultMaxSubscriptions int = 128

	// defaultMCPCacheTTL is what a client may keep the listings and the discovery answer for.
	// Nothing in them changes while the process runs, so this is about how long a client that
	// outlived a restart keeps talking to the binary it first met.
	defaultMCPCacheTTL time.Duration = 5 * time.Minute

	// mailboxRounds is how many full rounds of MaxSubscriptions fit in the mailbox of one
	// stream. Below one round a busy session drops frames on every tick of the inspector.
	mailboxRounds int = 2

	defaultClusterWatchLimit      int           = 5000
	defaultClusterConcurrency     int           = 64
	defaultClusterReconcilePeriod time.Duration = time.Minute
	defaultClusterGracePeriod     time.Duration = time.Minute

	// mirrors the inspector default, asked for explicitly so the map can report it
	defaultClusterWatchPeriod time.Duration = 3 * time.Second
)

type Options struct {
	// Host for HTTP listener. Default: "localhost". Ignored when Listeners is set.
	Host string

	// Port for HTTP listener. Default: 9911. Ignored when Listeners is set.
	Port uint16

	// PoolSize is the number of POST request workers. Default: 10
	PoolSize int

	// Ceiling is the limit of the whole observer: a listener, and then a caller, can only
	// be given less. Mutating actions outside it are refused and are not reported to the
	// browser as available.
	Ceiling Ceiling

	// Authorizer identifies the caller on the single listener Host:Port. Without one the
	// listener is open and everyone gets the Ceiling. Ignored when Listeners is set.
	Authorizer Authorizer

	// RateLimit is how many requests per second one caller may make on the single
	// listener. Zero means no limit. Ignored when Listeners is set.
	RateLimit int

	// AllowedOrigins lets browsers from these origins call the single listener. Empty
	// means same-origin only. Ignored when Listeners is set.
	AllowedOrigins []string

	// Listeners runs one endpoint per entry, each with its own authorization and ceiling,
	// instead of the single Host:Port one. Host and Port must be left unset.
	Listeners []Listener

	// Enrollment lets the cloud prove that a named endpoint is this observer. Empty
	// disables /api/enroll.
	Enrollment EnrollmentOptions

	// JobMaxRetention is the longest a finished run may keep its result. A caller asks for
	// what it needs and gets no more than this: the result sits in memory until it expires,
	// and every read of a finished run pushes the expiry out again. Default: 5m
	JobMaxRetention time.Duration

	// JobLimit is how many runs the observer may hold at once, over every caller. A run
	// holds a pool of workers for its whole life, so this bounds the work one agent can
	// leave behind. Zero means the default, not "no limit". Default: 32
	JobLimit int

	// ClusterLens configures the cluster lens
	ClusterLens ClusterLensOptions

	// LogLevel for the observer processes
	LogLevel gen.LogLevel
}

// EnrollmentOptions is the one-time secret the cloud presents to confirm that the endpoint
// it was given is this observer. The secret burns on the first success.
type EnrollmentOptions struct {
	// Token is the secret. Empty means /api/enroll is not served at all.
	Token string

	// ClusterID is answered on success, so the caller knows which cluster it reached.
	ClusterID string
}

// Listener is one endpoint of the observer, with its own authorization and ceiling.
type Listener struct {
	// Name goes into the start log and into inspect. Default: ":<port>"
	Name string

	// Host to bind. Default: "localhost"
	Host string

	// Port to bind. Required, and unique among the listeners.
	Port uint16

	// CertManager serves this listener over TLS. Nil means plain HTTP.
	CertManager gen.CertManager

	// UI is the bundle built into the binary, served at /
	UI SurfaceUI

	// API is /sse and /api/*
	API SurfaceAPI

	// MCP is /mcp
	MCP SurfaceMCP

	// Authorizer identifies the caller. Without one the listener is open and everyone
	// arriving here gets its Ceiling.
	Authorizer Authorizer

	// Ceiling narrows the deployment ceiling for everyone arriving here.
	Ceiling Ceiling

	// RateLimit is how many requests per second one caller may make here, counted by
	// subject when there is an authorizer and by address otherwise. Zero means no limit.
	// The static bundle is not metered.
	RateLimit int

	// MaxStreams is how many streams may be open here at once, /sse and /mcp together. Each
	// holds a goroutine, an actor and a compressor for its whole life. Zero means the
	// default, not "no limit". Default: 64
	MaxStreams int

	// MaxSubscriptions is how many live subscriptions one stream may hold. It also sizes
	// the mailbox of a stream, which holds a few rounds of it. Zero means the default, not
	// "no limit". Default: 128
	MaxSubscriptions int

	// AllowedOrigins lets browsers from these origins call this listener: a bundle served
	// elsewhere reaches it only from an origin named here. Each entry is scheme://host[:port]
	// with no path. Empty means same-origin only, and "*" means any origin without
	// credentials. Anything else is refused at start.
	AllowedOrigins []string
}

// A surface is served unless it is disabled. The API surface has no switch of its own:
// /sse and /api/* are what a listener is for, and the bundle calls them from its own
// origin. A listener that serves neither the UI nor the API would be an MCP-only one,
// which becomes possible when the MCP application moves here.
type (
	SurfaceUI struct {
		Disable bool
	}

	// SurfaceAPI is /sse and /api/*, what the bundle calls. It has no switch of its own.
	SurfaceAPI struct {
		// Ceiling narrows the listener ceiling for this surface alone. Nil keeps it.
		Ceiling *Ceiling
	}

	SurfaceMCP struct {
		Disable bool

		// Ceiling narrows the listener ceiling for this surface alone. Nil keeps it.
		// It separates surfaces, not callers: whoever reaches this listener reaches both,
		// so narrow API as well or put the surfaces on listeners of their own.
		Ceiling *Ceiling

		// Instructions is what an agent is told about the cluster behind this surface
		// before it asks anything: which node runs which part of the business, where a
		// flow begins, what not to touch. It is added to the guidance every observer
		// gives about itself, not put in its place, so navigating the surface stays
		// described whatever is written here. Empty leaves that default alone.
		Instructions string

		// CacheTTL is how long a client may keep what this surface says about itself: the
		// tool and resource listings, and the discovery answer. It says nothing about the
		// data a tool or a resource returns: a reading is stale in a second either way.
		//
		// Inside one process none of it changes, so a long value costs nothing in a running
		// deployment. It costs during development: a client outlives a restart, and until
		// this expires it calls tools with the arguments of the binary it first met. Set it
		// to a few seconds where the binary is rebuilt, and leave it alone otherwise.
		//
		// Zero means the default. Default: 5m
		CacheTTL time.Duration
	}
)

// ceilingMCP is what the MCP surface of this listener permits.
func (l Listener) ceilingMCP() Ceiling {
	if l.MCP.Ceiling == nil {
		return l.Ceiling
	}
	return access.Narrow(l.Ceiling, *l.MCP.Ceiling)
}

func (l Listener) ceilingAPI() Ceiling {
	if l.API.Ceiling == nil {
		return l.Ceiling
	}
	return access.Narrow(l.Ceiling, *l.API.Ceiling)
}

// listenerName is what the start log and inspect call it.
func listenerName(l Listener) string {
	if l.Name != "" {
		return l.Name
	}
	return fmt.Sprintf(":%d", l.Port)
}

// withLimits fills the stream limits. Zero means the default here, unlike RateLimit, where
// zero means no limit: a missing rate limit is safe, a missing stream limit is not.
func withLimits(l Listener) Listener {
	if l.MaxStreams < 1 {
		l.MaxStreams = defaultMaxStreams
	}
	if l.MaxSubscriptions < 1 {
		l.MaxSubscriptions = defaultMaxSubscriptions
	}
	if l.MCP.CacheTTL <= 0 {
		l.MCP.CacheTTL = defaultMCPCacheTTL
	}
	return l
}

// valid refuses an origin a browser will never send, so a typo fails the start instead of
// silently blocking every cross-origin call.
func valid(l Listener) error {
	if configurable, ok := l.Authorizer.(access.Configurable); ok {
		if err := configurable.Configured(); err != nil {
			return fmt.Errorf("observer: listener :%d has an authorizer that %s", l.Port, err)
		}
	}

	// a header believed because of where it came from is an invitation on a port anything reaches
	if trusting, ok := l.Authorizer.(access.Trusting); ok && trusting.TrustsTheNetworkPath() {
		if loopback(l.Host) == false {
			return fmt.Errorf("observer: listener :%d takes the caller from a trusted header and "+
				"binds %s, which is not loopback: put the proxy beside it, or set "+
				"ReachableOnlyByProxy once the path is restricted some other way", l.Port, l.Host)
		}
	}

	for _, origin := range l.AllowedOrigins {
		if origin == "*" {
			continue
		}
		parsed, err := url.Parse(origin)
		if err != nil || parsed.Host == "" || parsed.Path != "" ||
			(parsed.Scheme != "http" && parsed.Scheme != "https") {
			return fmt.Errorf("observer: listener :%d has origin %q, want scheme://host[:port]", l.Port, origin)
		}
		if err := validWildcard(l.Port, parsed.Host); err != nil {
			return err
		}
	}
	return nil
}

func loopback(host string) bool {
	if host == "localhost" {
		return true
	}
	address := net.ParseIP(strings.Trim(host, "[]"))
	return address != nil && address.IsLoopback()
}

// validWildcard keeps a wildcard bounded: leftmost label, and not over a registry suffix.
func validWildcard(port uint16, host string) error {
	if strings.Contains(host, "*") == false {
		return nil
	}

	suffix, found := strings.CutPrefix(host, "*.")
	if found == false || strings.Contains(suffix, "*") {
		return fmt.Errorf("observer: listener :%d has origin with a wildcard outside the first label", port)
	}
	if strings.Contains(suffix, ".") == false {
		return fmt.Errorf("observer: listener :%d has origin wildcard over %q, too broad", port, suffix)
	}
	return nil
}

// listeners resolves the endpoints to run, each ceiling already narrowed by the
// deployment one.
func (o Options) listeners() ([]Listener, error) {
	if len(o.Listeners) == 0 {
		host := o.Host
		if host == "" {
			host = defaultHost
		}
		port := o.Port
		if port == 0 {
			port = DefaultPort
		}
		single := withLimits(Listener{
			Host:           host,
			Port:           port,
			Authorizer:     o.Authorizer,
			Ceiling:        o.Ceiling,
			RateLimit:      o.RateLimit,
			AllowedOrigins: o.AllowedOrigins,
		})
		if err := valid(single); err != nil {
			return nil, err
		}
		single.Name = listenerName(single)
		return []Listener{single}, nil
	}

	if o.Port != 0 || o.Host != "" || o.Authorizer != nil || o.RateLimit != 0 || len(o.AllowedOrigins) > 0 {
		return nil, errors.New("observer: Host, Port, Authorizer, RateLimit and AllowedOrigins belong to a Listener when Listeners is set")
	}

	taken := make(map[uint16]bool, len(o.Listeners))
	resolved := make([]Listener, 0, len(o.Listeners))
	ui := ""
	for _, l := range o.Listeners {
		if l.Port == 0 {
			return nil, errors.New("observer: a listener without a port")
		}
		if taken[l.Port] {
			return nil, fmt.Errorf("observer: two listeners on port %d", l.Port)
		}
		taken[l.Port] = true

		if l.Host == "" {
			l.Host = defaultHost
		}
		l.Name = listenerName(l)
		l = withLimits(l)
		if err := valid(l); err != nil {
			return nil, err
		}

		// one bundle: two listeners serving it means two ways in with different policies
		if l.UI.Disable == false {
			if ui != "" {
				return nil, fmt.Errorf("observer: listeners %s and %s both serve the UI", ui, l.Name)
			}
			ui = l.Name
		}

		l.Ceiling = access.Narrow(o.Ceiling, l.Ceiling)
		resolved = append(resolved, l)
	}
	return resolved, nil
}

// ClusterLensOptions configures the cluster lens: the map of every discovered node
// and the watchers keeping it up to date.
type ClusterLensOptions struct {
	// WatchLimit is the maximum number of nodes being watched. Nodes discovered
	// beyond it stay on the map without data. Default: 5000
	WatchLimit int

	// Concurrency is the maximum number of nodes being connected at once.
	// Default: 64
	Concurrency int

	// ReconcilePeriod is the interval between the passes that check the membership
	// bookkeeping. Nodes are dropped as they run out of support, not on this timer,
	// so it is a backstop and wants a large value. Default: 1m
	ReconcilePeriod time.Duration

	// GracePeriod is how long the last known peer list of an unreachable node
	// still counts as evidence that its peers exist. Until it expires a network
	// split does not erase the far side of the map. Default: 1m
	GracePeriod time.Duration

	// WatchPeriod is the interval between the snapshots a watched node publishes.
	// Larger clusters want a larger value: every node sends one snapshot per
	// period. Default: 3s
	WatchPeriod time.Duration

	// LastReadingPeriod is how long the last reading of an unreachable node stays
	// on the map, marked stale. Default: GracePeriod
	LastReadingPeriod time.Duration
}

func (o ClusterLensOptions) withDefaults() ClusterLensOptions {
	if o.WatchLimit < 1 {
		o.WatchLimit = defaultClusterWatchLimit
	}
	if o.Concurrency < 1 {
		o.Concurrency = defaultClusterConcurrency
	}
	if o.ReconcilePeriod <= 0 {
		o.ReconcilePeriod = defaultClusterReconcilePeriod
	}
	if o.GracePeriod < time.Second {
		o.GracePeriod = defaultClusterGracePeriod
	}
	if o.WatchPeriod <= 0 {
		o.WatchPeriod = defaultClusterWatchPeriod
	}
	if o.LastReadingPeriod <= 0 {
		o.LastReadingPeriod = o.GracePeriod
	}
	return o
}
