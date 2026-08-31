package access

import (
	"errors"
	"net/http"
	"slices"
	"testing"
)

func headed(pairs ...string) *http.Request {
	request, _ := http.NewRequest("POST", "/mcp", nil)
	for i := 0; i+1 < len(pairs); i += 2 {
		request.Header.Add(pairs[i], pairs[i+1])
	}
	return request
}

func TestTrustedHeaderReadsTheCaller(t *testing.T) {
	authorizer := TrustedHeader{Subject: "X-Auth-Request-Email", Tenant: "X-Auth-Request-Domain"}

	identity, err := authorizer.Authorize(headed(
		"X-Auth-Request-Email", " alice@corp.com ",
		"X-Auth-Request-Domain", "corp.com",
	))
	if err != nil {
		t.Fatal(err)
	}
	if identity.Subject != "alice@corp.com" || identity.Tenant != "corp.com" {
		t.Errorf("read %#v", identity)
	}
	if identity.Ceiling.Allows("manage.kill") == false {
		t.Error("no table of ceilings must not narrow")
	}
}

// a missing header is a challenge, a caller holding nothing this deployment knows is a refusal
func TestTrustedHeaderTellsTheTwoRefusalsApart(t *testing.T) {
	authorizer := TrustedHeader{
		Subject:  "X-User",
		Groups:   "X-Groups",
		Ceilings: map[string]Ceiling{"sre": {}},
	}

	if _, err := authorizer.Authorize(headed()); errors.Is(err, ErrUnauthenticated) == false {
		t.Errorf("a request without the header answered %v", err)
	}
	if _, err := authorizer.Authorize(headed("X-User", "  ")); errors.Is(err, ErrUnauthenticated) == false {
		t.Errorf("an empty header answered %v", err)
	}
	if _, err := authorizer.Authorize(headed(
		"X-User", "bob@corp.com", "X-Groups", "interns",
	)); errors.Is(err, ErrForbidden) == false {
		t.Errorf("an unknown group answered %v", err)
	}
}

// roles compose only while one contains the other, and then the wider one is the answer
func TestTrustedHeaderComposesComparableGroups(t *testing.T) {
	authorizer := TrustedHeader{
		Subject: "X-User",
		Groups:  "X-Groups",
		Ceilings: map[string]Ceiling{
			"viewer": {ReadOnly: true, Nodes: []string{"a@h", "b@h"}},
			"sre":    {Nodes: []string{"a@h", "b@h"}},
		},
	}
	if err := authorizer.Configured(); err != nil {
		t.Fatalf("a chain of roles was refused: %s", err)
	}

	for _, given := range [][]string{
		{"X-User", "alice", "X-Groups", "viewer,sre"},
		{"X-User", "alice", "X-Groups", " sre , viewer "},
		{"X-User", "alice", "X-Groups", "viewer", "X-Groups", "sre"},
	} {
		identity, err := authorizer.Authorize(headed(given...))
		if err != nil {
			t.Fatalf("%v: %s", given, err)
		}
		if identity.Ceiling.ReadOnly {
			t.Errorf("%v: the wider role did not win", given)
		}
		if slices.Equal(identity.Ceiling.Nodes, []string{"a@h", "b@h"}) == false {
			t.Errorf("%v: nodes came out as %v", given, identity.Ceiling.Nodes)
		}
	}

	identity, err := authorizer.Authorize(headed("X-User", "bob", "X-Groups", "viewer"))
	if err != nil {
		t.Fatal(err)
	}
	if identity.Ceiling.ReadOnly == false || identity.Ceiling.Allows("manage.kill") {
		t.Errorf("viewer alone came out as %#v", identity.Ceiling)
	}
}

// refused at start, not composed into a ceiling that permits what neither role did
func TestTrustedHeaderRefusesRolesThatDoNotCompose(t *testing.T) {
	for what, ceilings := range map[string]map[string]Ceiling{
		"one refuses by read-only, the other by name": {
			"viewer": {ReadOnly: true},
			"sre":    {Deny: []string{"manage.kill"}},
		},
		"each holds its own node": {
			"viewer": {ReadOnly: true, Nodes: []string{"a@h"}},
			"sre":    {Nodes: []string{"b@h"}, Deny: []string{"manage.kill"}},
		},
		"allowlists that do not contain one another": {
			"one":   {Allow: []string{"inspect.node"}},
			"other": {Allow: []string{"inspect.network"}},
		},
	} {
		err := TrustedHeader{Subject: "X-User", Groups: "X-Groups", Ceilings: ceilings}.Configured()
		if err == nil {
			t.Errorf("%s: accepted", what)
		}
	}
}

func TestTrustedHeaderReportsItsConfiguration(t *testing.T) {
	if err := (TrustedHeader{}).Configured(); errors.Is(err, ErrNoSubjectHeader) == false {
		t.Errorf("no subject header answered %v", err)
	}
	if err := (TrustedHeader{
		Subject: "X-User", Ceilings: map[string]Ceiling{"sre": {}},
	}).Configured(); errors.Is(err, ErrNoGroupsHeader) == false {
		t.Errorf("ceilings without a groups header answered %v", err)
	}
	if err := (TrustedHeader{Subject: "X-User"}).Configured(); err != nil {
		t.Errorf("a usable authorizer answered %v", err)
	}

	if (TrustedHeader{Subject: "X-User"}).TrustsTheNetworkPath() == false {
		t.Error("a trusted header trusts the path it arrived on")
	}
	if (TrustedHeader{Subject: "X-User", ReachableOnlyByProxy: true}).TrustsTheNetworkPath() {
		t.Error("a restricted path is stated, so nothing is left to trust")
	}
}

// Widen answers with the wider of two comparable ceilings and never with more than either
func TestWidenPicksTheWiderAndNeverMore(t *testing.T) {
	narrow := Ceiling{ReadOnly: true, Nodes: []string{"n1"}}
	wide := Ceiling{Nodes: []string{"n1", "n2"}}

	if got := Widen(narrow, wide); slices.Equal(got.Nodes, wide.Nodes) == false || got.ReadOnly {
		t.Errorf("the wider one did not win: %#v", got)
	}
	if got := Widen(wide, narrow); slices.Equal(got.Nodes, wide.Nodes) == false || got.ReadOnly {
		t.Errorf("order changed the answer: %#v", got)
	}

	// unset names everything, so it is the wider of any pair
	if got := Widen(narrow, Ceiling{}); got.Nodes != nil || got.ReadOnly {
		t.Errorf("an unset ceiling did not win: %#v", got)
	}

	// a pair neither of which contains the other narrows rather than inventing a permission
	one := Ceiling{ReadOnly: true}
	other := Ceiling{Deny: []string{"manage.kill"}}
	got := Widen(one, other)
	if got.Allows("manage.kill") {
		t.Errorf("composing two roles that both refuse kill permitted it: %#v", got)
	}
	if got.ReadOnly == false {
		t.Errorf("an incomparable pair widened: %#v", got)
	}
}

func TestAtLeastReadsTheLimits(t *testing.T) {
	for what, c := range map[string]struct {
		outer, inner Ceiling
		want         bool
	}{
		"unset over a node list":  {Ceiling{}, Ceiling{Nodes: []string{"a"}}, true},
		"node list over unset":    {Ceiling{Nodes: []string{"a"}}, Ceiling{}, false},
		"superset of nodes":       {Ceiling{Nodes: []string{"a", "b"}}, Ceiling{Nodes: []string{"a"}}, true},
		"writing over read-only":  {Ceiling{}, Ceiling{ReadOnly: true}, true},
		"read-only over writing":  {Ceiling{ReadOnly: true}, Ceiling{}, false},
		"denying less":            {Ceiling{Deny: []string{"x"}}, Ceiling{Deny: []string{"x", "y"}}, true},
		"denying more":            {Ceiling{Deny: []string{"x", "y"}}, Ceiling{Deny: []string{"x"}}, false},
		"allowlist over a subset": {Ceiling{Allow: []string{"a", "b"}}, Ceiling{Allow: []string{"a"}}, true},
		"allowlists side by side": {Ceiling{Allow: []string{"a"}}, Ceiling{Allow: []string{"b"}}, false},
	} {
		if got := AtLeast(c.outer, c.inner); got != c.want {
			t.Errorf("%s: %v, want %v", what, got, c.want)
		}
	}
}
