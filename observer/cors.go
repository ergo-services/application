package observer

import (
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"

	"ergo.services/ergo/gen"
)

const corsRequestHeaders = "Content-Type, X-Observer-Session, Last-Event-ID, Authorization"

const corsMaxAge = "600" // seconds

type originGuard struct {
	next    http.Handler
	origins []string
	host    string
	port    uint16
	refused *refusals
	log     gen.Log
}

func (o originGuard) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	origin := request.Header.Get("Origin")
	switch {
	case origin == "":
	case o.serves(request.Host) && sameOrigin(origin, request):
	case allowOrigin(o.origins, origin) != "":
	default:
		if o.refused.note(origin) && o.log != nil {
			o.log.Warning("origin %s refused on %s, allowed: %s",
				origin, request.URL.Path, strings.Join(o.origins, ","))
		}
		refuse(writer, request, http.StatusForbidden, nil)
		return
	}
	o.next.ServeHTTP(writer, request)
}

func sameOrigin(origin string, request *http.Request) bool {
	scheme := "http"
	if request.TLS != nil {
		scheme = "https"
	}
	if forwarded := request.Header.Get("X-Forwarded-Proto"); forwarded != "" {
		first, _, _ := strings.Cut(forwarded, ",")
		scheme = strings.ToLower(strings.TrimSpace(first))
	}
	return strings.EqualFold(origin, scheme+"://"+request.Host)
}

func (o originGuard) serves(requested string) bool {
	name, port, err := net.SplitHostPort(requested)
	if err != nil {
		name, port = requested, ""
	}
	if port != "" && port != strconv.Itoa(int(o.port)) {
		return false
	}
	if strings.EqualFold(name, o.host) {
		return true
	}
	if unspecified(o.host) {
		return true
	}
	if o.host != "" && loopback(o.host) == false {
		return false
	}
	return loopback(name)
}

func unspecified(host string) bool {
	address := net.ParseIP(strings.Trim(host, "[]"))
	return address != nil && address.IsUnspecified()
}

type cors struct {
	next    http.Handler
	origins []string
}

type refusals struct {
	count atomic.Int64
	last  atomic.Value // string
}

func (r *refusals) note(origin string) bool {
	if r == nil {
		return false
	}
	r.count.Add(1)
	previous, _ := r.last.Load().(string)
	r.last.Store(origin)
	return previous != origin
}

func (c cors) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if len(c.origins) == 0 {
		c.next.ServeHTTP(writer, request)
		return
	}

	writer.Header().Add("Vary", "Origin")

	allowed := allowOrigin(c.origins, request.Header.Get("Origin"))
	if allowed != "" {
		writer.Header().Set("Access-Control-Allow-Origin", allowed)
		writer.Header().Set("Access-Control-Expose-Headers", "Retry-After")
		if allowed != "*" {
			writer.Header().Set("Access-Control-Allow-Credentials", "true")
		}
	}

	if request.Method != http.MethodOptions || request.Header.Get("Access-Control-Request-Method") == "" {
		c.next.ServeHTTP(writer, request)
		return
	}

	if allowed == "" {
		http.Error(writer, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return
	}

	headers := request.Header.Get("Access-Control-Request-Headers")
	if headers == "" {
		headers = corsRequestHeaders
	}
	writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	writer.Header().Set("Access-Control-Allow-Headers", headers)
	writer.Header().Set("Access-Control-Max-Age", corsMaxAge)
	writer.WriteHeader(http.StatusNoContent)
}

const portWildcard = ":*"

func allowOrigin(origins []string, origin string) string {
	if origin == "" {
		return ""
	}
	for _, o := range origins {
		switch {
		case o == "*":
			return "*"
		case strings.EqualFold(o, origin):
			return origin
		case subdomainOf(o, origin):
			return origin
		case anyPortOf(o, origin):
			return origin
		}
	}
	return ""
}

func anyPortOf(pattern, origin string) bool {
	named, wild := strings.CutSuffix(pattern, portWildcard)
	if wild == false {
		return false
	}
	if strings.EqualFold(named, origin) {
		return true
	}
	port, found := strings.CutPrefix(strings.ToLower(origin), strings.ToLower(named)+":")
	if found == false || port == "" {
		return false
	}
	for _, digit := range port {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}

func subdomainOf(pattern, origin string) bool {
	scheme, suffix, ok := strings.Cut(pattern, "://*.")
	if ok == false {
		return false
	}

	prefix := scheme + "://"
	if strings.HasPrefix(strings.ToLower(origin), prefix) == false {
		return false
	}

	host := strings.ToLower(origin[len(prefix):])
	tail := "." + strings.ToLower(suffix)
	if strings.HasSuffix(host, tail) == false {
		return false
	}

	label := host[:len(host)-len(tail)]
	return label != "" && strings.ContainsAny(label, "./:") == false
}
