package observer

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"ergo.services/application/observer/access"
)

type identityKey struct{}

type guard struct {
	next       http.Handler
	ceiling    Ceiling
	authorizer Authorizer
	counts     *refusalCounts
}

func (g guard) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	identity := Identity{Ceiling: g.ceiling}

	if g.authorizer != nil {
		authorized, err := g.authorizer.Authorize(request)
		if err != nil {
			status := http.StatusForbidden
			if errors.Is(err, access.ErrUnauthenticated) {
				status = http.StatusUnauthorized
			}
			g.counts.note(status)
			refuse(writer, request, status, nil)
			return
		}
		identity = authorized
		identity.Ceiling = access.Narrow(g.ceiling, authorized.Ceiling)
	}

	ctx := context.WithValue(request.Context(), identityKey{}, identity)
	g.next.ServeHTTP(writer, request.WithContext(ctx))
}

type throttle struct {
	next    http.Handler
	limiter *limiter
	counts  *refusalCounts
}

func (t throttle) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	identity, _ := identityOf(request)
	if t.limiter.allow(callerOf(request, identity), time.Now()) == false {
		writer.Header().Set("Retry-After", "1")
		t.counts.note(http.StatusTooManyRequests)
		refuse(writer, request, http.StatusTooManyRequests, nil)
		return
	}
	t.next.ServeHTTP(writer, request)
}

type limitsKey struct{}

// ServeHTTP of a stream blocks for its whole life, so incrementing before and decrementing on
// return counts exactly what is open
type streams struct {
	next             http.Handler
	limit            int
	maxSubscriptions int
	counts           *refusalCounts

	live *streamCounters
}

type streamCounters struct {
	open    atomic.Int64
	refused atomic.Int64
	peak    atomic.Int64
}

func (s *streams) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	open := s.live.open.Add(1)
	if open > int64(s.limit) {
		s.live.open.Add(-1)
		s.live.refused.Add(1)
		writer.Header().Set("Retry-After", "5")
		s.counts.note(http.StatusTooManyRequests)
		refuse(writer, request, http.StatusTooManyRequests, nil)
		return
	}
	if open > s.live.peak.Load() {
		s.live.peak.Store(open)
	}
	defer s.live.open.Add(-1)

	ctx := context.WithValue(request.Context(), limitsKey{}, s.maxSubscriptions)
	s.next.ServeHTTP(writer, request.WithContext(ctx))
}

func maxSubscriptionsOf(request *http.Request) int {
	if request == nil {
		return 0
	}
	max, _ := request.Context().Value(limitsKey{}).(int)
	return max
}

type refusalCounts struct {
	unauthenticated atomic.Int64
	forbidden       atomic.Int64
	throttled       atomic.Int64
	method          atomic.Int64
}

func (r *refusalCounts) note(status int) {
	if r == nil {
		return
	}
	switch status {
	case http.StatusUnauthorized:
		r.unauthenticated.Add(1)
	case http.StatusForbidden:
		r.forbidden.Add(1)
	case http.StatusTooManyRequests:
		r.throttled.Add(1)
	case http.StatusMethodNotAllowed:
		r.method.Add(1)
	}
}

func (r *refusalCounts) String() string {
	if r == nil {
		return "none"
	}
	return fmt.Sprintf("401=%d 403=%d 429=%d 405=%d", r.unauthenticated.Load(),
		r.forbidden.Load(), r.throttled.Load(), r.method.Load())
}

// in the dialect of the surface asked: a caller that speaks JSON-RPC and is handed text
// reports a parse error instead of the refusal. The shape is meta.RefusalHandler, so the
// handlers below this one refuse through it too instead of writing their own plain text.
func refuse(writer http.ResponseWriter, request *http.Request, status int, reason error) {
	message := http.StatusText(status)
	if reason != nil {
		message = reason.Error()
	}

	switch {
	case strings.HasPrefix(request.URL.Path, "/api/"):
		writeJSON(writer, request, status, apiResponse{Error: message})
	case strings.HasPrefix(request.URL.Path, "/mcp"):
		writeJSON(writer, request, status, jsonrpcError(status, message))
	default:
		http.Error(writer, message, status)
	}
}

func callerOf(request *http.Request, identity Identity) string {
	if identity.Subject != "" {
		return qualified(identity)
	}
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil {
		return request.RemoteAddr
	}
	return host
}

// the table size at which idle callers are dropped
const limiterSweepAt = 4096

// a token bucket per caller. Runs on the web server goroutine, so the mutex never reaches an
// actor callback.
type limiter struct {
	rate  float64
	mutex sync.Mutex
	seen  map[string]bucket
}

type bucket struct {
	tokens float64
	at     time.Time
}

func newLimiter(perSecond int) *limiter {
	if perSecond < 1 {
		return nil
	}
	return &limiter{rate: float64(perSecond), seen: make(map[string]bucket)}
}

func (l *limiter) allow(caller string, now time.Time) bool {
	if l == nil {
		return true
	}

	l.mutex.Lock()
	defer l.mutex.Unlock()

	b, exist := l.seen[caller]
	if exist == false {
		b = bucket{tokens: l.rate}
	} else {
		b.tokens = min(l.rate, b.tokens+now.Sub(b.at).Seconds()*l.rate)
	}
	b.at = now

	allowed := b.tokens >= 1
	if allowed {
		b.tokens--
	}
	l.seen[caller] = b

	if len(l.seen) > limiterSweepAt {
		l.sweep(now)
	}
	return allowed
}

func (l *limiter) sweep(now time.Time) {
	for caller, b := range l.seen {
		if b.tokens+now.Sub(b.at).Seconds()*l.rate >= l.rate {
			delete(l.seen, caller)
		}
	}
}

// Who a run, a keyed reading and a session belong to. A tenant groups subjects, so two
// deployments that both call somebody "admin" do not share one keyspace.
func qualified(identity Identity) string {
	if identity.Tenant == "" || identity.Subject == "" {
		return qualifierEscape.Replace(identity.Subject)
	}
	return qualifierEscape.Replace(identity.Tenant) + "/" +
		qualifierEscape.Replace(identity.Subject)
}

var qualifierEscape = strings.NewReplacer(`\`, `\\`, `/`, `\/`)

func identityOf(request *http.Request) (Identity, bool) {
	if request == nil {
		return Identity{}, false
	}
	identity, ok := request.Context().Value(identityKey{}).(Identity)
	return identity, ok
}
