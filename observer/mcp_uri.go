package observer

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"ergo.services/ergo/gen"
)

const mcpScheme = "ergo://"

// reserved: no lens may be called either of these
const (
	uriWordWatch   = "watch"
	uriWordJob     = "job"
	uriWordCluster = "cluster"
)

var uriReservedQuery = map[string]bool{"since": true}

type mcpURI struct {
	Node   gen.Atom
	Key    string
	Lens   string
	Target string
	Scope  url.Values
	Since  string
}

var errURIShape = errors.New("want ergo://node[/watch/key]/lens[/target][?scope]")

func parseURI(raw string) (mcpURI, error) {
	if strings.HasPrefix(raw, mcpScheme) == false {
		return mcpURI{}, fmt.Errorf("uri %q: %w", raw, errURIShape)
	}

	body, query, _ := strings.Cut(strings.TrimPrefix(raw, mcpScheme), "?")
	segments := strings.Split(body, "/")
	if len(segments) == 0 || segments[0] == "" {
		return mcpURI{}, fmt.Errorf("uri %q: %w", raw, errURIShape)
	}

	var out mcpURI
	if err := out.readQuery(query); err != nil {
		return mcpURI{}, fmt.Errorf("uri %q: %w", raw, err)
	}

	switch segments[0] {
	case uriWordCluster:
		if len(segments) != 1 {
			return mcpURI{}, fmt.Errorf("uri %q: want ergo://cluster", raw)
		}
		out.Lens = uriWordCluster
		return out, nil

	case uriWordJob:
		if len(segments) != 2 || segments[1] == "" {
			return mcpURI{}, fmt.Errorf("uri %q: want ergo://job/key", raw)
		}
		out.Lens, out.Key = uriWordJob, segments[1]
		return out, out.validKey()

	case uriWordWatch:
		return mcpURI{}, fmt.Errorf("uri %q: watch belongs to a node", raw)
	}

	out.Node = gen.Atom(segments[0])
	rest := segments[1:]

	if len(rest) > 1 && rest[0] == uriWordWatch {
		out.Key, rest = rest[1], rest[2:]
		if out.Key == "" {
			return mcpURI{}, fmt.Errorf("uri %q: watch needs a key, or drop it for the shared lens", raw)
		}
		if err := out.validKey(); err != nil {
			return mcpURI{}, fmt.Errorf("uri %q: %w", raw, err)
		}
	}

	switch len(rest) {
	case 0:
		out.Lens = "node"
	case 1:
		out.Lens = rest[0]
	case 2:
		out.Lens = rest[0]
		target, err := url.PathUnescape(rest[1])
		if err != nil {
			return mcpURI{}, fmt.Errorf("uri %q: target: %w", raw, err)
		}
		out.Target = target
	default:
		return mcpURI{}, fmt.Errorf("uri %q: %w", raw, errURIShape)
	}

	if out.Lens == "" || out.Lens == uriWordWatch || out.Lens == uriWordJob || out.Lens == uriWordCluster {
		return mcpURI{}, fmt.Errorf("uri %q: %q is not a lens of a node", raw, out.Lens)
	}

	if node, rest, written := strings.Cut(out.Target, mcpIdentSep); written && gen.Atom(node) == out.Node {
		out.Target = rest
	}
	return out, nil
}

func (u *mcpURI) readQuery(query string) error {
	if query == "" {
		return nil
	}
	values, err := url.ParseQuery(query)
	if err != nil {
		return errors.New("query is not readable")
	}
	u.Scope = url.Values{}
	for name, list := range values {
		if uriReservedQuery[name] == false {
			u.Scope[name] = list
			continue
		}
		if len(list) != 1 {
			return fmt.Errorf("%s is given more than once", name)
		}
		u.Since = list[0]
	}
	if len(u.Scope) == 0 {
		u.Scope = nil
	}
	return nil
}

func validKeyRune(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return true
	}
	return r == '-' || r == '_' || r == '.'
}

const uriKeyMax = 64

func (u mcpURI) validKey() error {
	if u.Key == "" {
		return nil
	}
	if len(u.Key) > uriKeyMax {
		return fmt.Errorf("key is longer than %d", uriKeyMax)
	}
	letters := 0
	for _, r := range u.Key {
		if validKeyRune(r) == false {
			return fmt.Errorf("key holds %q", r)
		}
		if r != '-' && r != '_' && r != '.' {
			letters++
		}
	}
	if letters == 0 {
		return errors.New("key has no letter or digit")
	}
	return nil
}

func (u mcpURI) Canonical() string {
	var b strings.Builder
	b.WriteString(mcpScheme)

	if u.Node == "" {
		b.WriteString(u.Lens)
		if u.Key != "" {
			b.WriteString("/" + u.Key)
		}
		return b.String()
	}

	b.WriteString(string(u.Node))
	if u.Key != "" {
		b.WriteString("/" + uriWordWatch + "/" + u.Key)
	}
	b.WriteString("/" + u.Lens)
	if u.Target != "" {
		b.WriteString("/" + url.PathEscape(u.Target))
	}
	if scope := canonicalScope(u.Scope); scope != "" {
		b.WriteString("?" + scope)
	}
	return b.String()
}

func canonicalScope(scope url.Values) string {
	if len(scope) == 0 {
		return ""
	}
	names := make([]string, 0, len(scope))
	for name := range scope {
		names = append(names, name)
	}
	sort.Strings(names)

	pairs := make([]string, 0, len(scope))
	for _, name := range names {
		values := append([]string(nil), scope[name]...)
		sort.Strings(values)
		for _, value := range values {
			pairs = append(pairs, url.QueryEscape(name)+"="+url.QueryEscape(value))
		}
	}
	return strings.Join(pairs, "&")
}

const (
	ownerPrefixShared = "observer_uri_"
	ownerPrefixWatch  = "observer_watch_"
	ownerPrefixJob    = "observer_job_"
)

func (u mcpURI) ownerName(subject string) gen.Atom {
	if u.Lens == uriWordJob {
		return gen.Atom(ownerPrefixJob + subjectPrefix(subject) + u.Key)
	}
	sum := sha256.Sum256([]byte(u.Canonical()))
	if u.Key != "" {
		return gen.Atom(ownerPrefixWatch + subjectPrefix(subject) + u.Key + "_" +
			hex.EncodeToString(sum[:12]))
	}
	return gen.Atom(ownerPrefixShared + hex.EncodeToString(sum[:12]))
}

func subjectPrefix(subject string) string {
	sum := sha256.Sum256([]byte(subject))
	return hex.EncodeToString(sum[:8]) + "_"
}

func ownedBy(prefix string, subject string) string {
	return prefix + subjectPrefix(subject)
}

func keyOf(prefix string, subject string, name gen.Atom) string {
	owned := ownedBy(prefix, subject)
	if strings.HasPrefix(string(name), owned) == false {
		return ""
	}
	return strings.TrimPrefix(string(name), owned)
}

func epochOf(pid gen.PID) string {
	return strconv.FormatInt(pid.Creation, 10) + "-" + strconv.FormatUint(pid.ID, 10)
}
