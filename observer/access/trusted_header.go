package access

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
)

// TrustedHeader takes the caller from headers set by a proxy in front of the observer. It
// verifies nothing: its whole security is that nothing but the proxy can reach the listener,
// so the listener binds a loopback address or states ReachableOnlyByProxy.
type TrustedHeader struct {
	Subject   string
	Tenant    string
	Groups    string
	Separator string

	Ceilings map[string]Ceiling

	ReachableOnlyByProxy bool
}

func (t TrustedHeader) Authorize(request *http.Request) (Identity, error) {
	subject := strings.TrimSpace(request.Header.Get(t.Subject))
	if t.Subject == "" || subject == "" {
		return Identity{}, ErrUnauthenticated
	}

	identity := Identity{Subject: subject}
	if t.Tenant != "" {
		identity.Tenant = strings.TrimSpace(request.Header.Get(t.Tenant))
	}
	if len(t.Ceilings) == 0 {
		return identity, nil
	}

	separator := t.Separator
	if separator == "" {
		separator = ","
	}

	held := false
	for _, value := range request.Header.Values(t.Groups) {
		for _, name := range strings.Split(value, separator) {
			granted, listed := t.Ceilings[strings.TrimSpace(name)]
			if listed == false {
				continue
			}
			if held {
				identity.Ceiling = Widen(identity.Ceiling, granted)
			} else {
				identity.Ceiling = granted
			}
			held = true
		}
	}

	if held == false {
		return Identity{}, ErrForbidden
	}
	return identity, nil
}

type Trusting interface {
	TrustsTheNetworkPath() bool
}

func (t TrustedHeader) TrustsTheNetworkPath() bool {
	return t.ReachableOnlyByProxy == false
}

type Configurable interface {
	Configured() error
}

func (t TrustedHeader) Configured() error {
	switch {
	case t.Subject == "":
		return ErrNoSubjectHeader
	case len(t.Ceilings) > 0 && t.Groups == "":
		return ErrNoGroupsHeader
	}

	names := make([]string, 0, len(t.Ceilings))
	for name := range t.Ceilings {
		names = append(names, name)
	}
	sort.Strings(names)

	for i, one := range names {
		for _, other := range names[i+1:] {
			if AtLeast(t.Ceilings[one], t.Ceilings[other]) {
				continue
			}
			if AtLeast(t.Ceilings[other], t.Ceilings[one]) {
				continue
			}
			return fmt.Errorf("grants %q and %q that neither contains the other, so a caller "+
				"holding both would get more than either: give one of them what the other "+
				"permits, or put them on listeners of their own", one, other)
		}
	}
	return nil
}
