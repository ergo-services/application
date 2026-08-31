package observer

import (
	"net/url"
	"strings"
	"testing"

	"ergo.services/ergo/gen"
)

func TestParseURI(t *testing.T) {
	cases := []struct {
		raw       string
		node      string
		key       string
		lens      string
		target    string
		since     string
		canonical string
	}{
		{
			raw:       "ergo://node@host/process/1234.5",
			node:      "node@host",
			lens:      "process",
			target:    "1234.5",
			canonical: "ergo://node@host/process/1234.5",
		},
		{
			raw:       "ergo://node@host",
			node:      "node@host",
			lens:      "node",
			canonical: "ergo://node@host/node",
		},
		{
			raw:       "ergo://node@host/watch/prof-7/log?level=error",
			node:      "node@host",
			key:       "prof-7",
			lens:      "log",
			canonical: "ergo://node@host/watch/prof-7/log?level=error",
		},
		{
			raw:       "ergo://job/prof-7",
			lens:      "job",
			key:       "prof-7",
			canonical: "ergo://job/prof-7",
		},
		{
			raw:       "ergo://node@host/log?since=12&level=error",
			node:      "node@host",
			lens:      "log",
			since:     "12",
			canonical: "ergo://node@host/log?level=error",
		},
	}

	for _, c := range cases {
		got, err := parseURI(c.raw)
		if err != nil {
			t.Errorf("%s: %s", c.raw, err)
			continue
		}
		if string(got.Node) != c.node || got.Key != c.key || got.Lens != c.lens ||
			got.Target != c.target || got.Since != c.since {
			t.Errorf("%s parsed as node=%q key=%q lens=%q target=%q since=%q",
				c.raw, got.Node, got.Key, got.Lens, got.Target, got.Since)
		}
		if canonical := got.Canonical(); canonical != c.canonical {
			t.Errorf("%s canonical is %q, want %q", c.raw, canonical, c.canonical)
		}
	}
}

func TestParseURIRefusals(t *testing.T) {
	refused := []string{
		"http://node@host/process",
		"ergo://",
		"ergo://node@host/process/1234.5/extra",
		"ergo://watch/key/log",
		"ergo://job",
		"ergo://job/",
		"ergo://node@host/watch/../log",
		"ergo://node@host/log?since=1&since=2",
	}
	for _, raw := range refused {
		if parsed, err := parseURI(raw); err == nil {
			t.Errorf("%s was accepted as %#v", raw, parsed)
		}
	}
}

func TestReservedQueryDoesNotNameTheOwner(t *testing.T) {
	plain, err := parseURI("ergo://node@host/log?level=error")
	if err != nil {
		t.Fatal(err)
	}
	withCursor, err := parseURI("ergo://node@host/log?level=error&since=99")
	if err != nil {
		t.Fatal(err)
	}

	if plain.ownerName("") != withCursor.ownerName("") {
		t.Error("a cursor changed the owner")
	}
	if withCursor.Since != "99" {
		t.Errorf("the cursor came out as %q", withCursor.Since)
	}
	if withCursor.Canonical() != plain.Canonical() {
		t.Errorf("canonical forms differ: %q and %q", withCursor.Canonical(), plain.Canonical())
	}
}

// two spellings of one scope are one owner, or the observed node pays twice for one watch
// an empty key used to canonicalize into the shared lens, so a caller asking for a private
// cursor got everybody's
func TestTargetSurvivesAnyName(t *testing.T) {
	node := "shop-basket@localhost"
	names := []string{"metrics/open", "worker/1/inner", "a?b", "a#b", "a b", "тревога", "a%2Fb"}

	for _, name := range names {
		want := mcpURI{Node: gen.Atom(node), Lens: "event", Target: name}
		raw := want.Canonical()

		if strings.Contains(raw, "/event/"+name) && strings.ContainsAny(name, "/?# %") {
			t.Errorf("%q went into the uri unescaped: %s", name, raw)
		}

		back, err := parseURI(raw)
		if err != nil {
			t.Errorf("%q -> %s: %s", name, raw, err)
			continue
		}
		if back.Target != name {
			t.Errorf("%q came back as %q from %s", name, back.Target, raw)
		}
		if back.Canonical() != raw {
			t.Errorf("%q is not stable: %s then %s", name, raw, back.Canonical())
		}
	}
}

func TestTargetSpellingIsOne(t *testing.T) {
	node := "shop-basket@localhost"
	full, err := parseURI("ergo://" + node + "/process/" + node + ":1042.7")
	if err != nil {
		t.Fatal(err)
	}
	short, err := parseURI("ergo://" + node + "/process/1042.7")
	if err != nil {
		t.Fatal(err)
	}

	if full.Canonical() != short.Canonical() {
		t.Errorf("two spellings, two resources:\n  %s\n  %s", full.Canonical(), short.Canonical())
	}
	if full.ownerName("") != short.ownerName("") {
		t.Error("two spellings, two owners")
	}

	// a name that holds a colon of its own keeps it, because the part before is not the node
	kept, err := parseURI("ergo://" + node + "/event/a:b")
	if err != nil {
		t.Fatal(err)
	}
	if kept.Target != "a:b" {
		t.Errorf("the name came back as %q", kept.Target)
	}
}

func TestWatchNeedsAKey(t *testing.T) {
	if _, err := parseURI("ergo://n@h/watch//log"); err == nil {
		t.Error("an empty watch key was accepted")
	}

	keyed, err := parseURI("ergo://n@h/watch/mine/log")
	if err != nil {
		t.Fatal(err)
	}
	shared, err := parseURI("ergo://n@h/log")
	if err != nil {
		t.Fatal(err)
	}
	if keyed.ownerName("root") == shared.ownerName("root") {
		t.Errorf("a keyed lens shares the owner of the canonical one: %s", keyed.ownerName("root"))
	}
}

func TestScopeOrderDoesNotMatter(t *testing.T) {
	first, err := parseURI("ergo://node@host/log?level=error&limit=10")
	if err != nil {
		t.Fatal(err)
	}
	second, err := parseURI("ergo://node@host/log?limit=10&level=error")
	if err != nil {
		t.Fatal(err)
	}
	if first.ownerName("") != second.ownerName("") {
		t.Errorf("%q and %q are different owners", first.Canonical(), second.Canonical())
	}
}

func TestKeyedOwnerIsPrivate(t *testing.T) {
	keyed, err := parseURI("ergo://node@host/watch/mine/log")
	if err != nil {
		t.Fatal(err)
	}
	if keyed.ownerName("alice") == keyed.ownerName("bob") {
		t.Error("two subjects share a keyed owner")
	}

	shared, err := parseURI("ergo://node@host/log")
	if err != nil {
		t.Fatal(err)
	}
	if shared.ownerName("alice") != shared.ownerName("bob") {
		t.Error("a canonical resource is not shared")
	}
}

func TestKeyedOwnerNamesTheWholeQuestion(t *testing.T) {
	held := map[gen.Atom]string{}
	for _, raw := range []string{
		"ergo://n1@h/watch/mine/log",
		"ergo://n2@h/watch/mine/log",
		"ergo://n1@h/watch/mine/processes",
		"ergo://n1@h/watch/mine/log?level=error",
		"ergo://n1@h/watch/other/log",
	} {
		uri, err := parseURI(raw)
		if err != nil {
			t.Fatalf("%s: %s", raw, err)
		}
		name := uri.ownerName("alice")
		if taken, seen := held[name]; seen {
			t.Errorf("%s and %s are one owner %s", taken, raw, name)
		}
		held[name] = raw
	}

	again, err := parseURI("ergo://n1@h/watch/mine/log")
	if err != nil {
		t.Fatal(err)
	}
	if held[again.ownerName("alice")] != "ergo://n1@h/watch/mine/log" {
		t.Error("reading the same key twice landed on two owners")
	}
}

func TestKeyAlphabet(t *testing.T) {
	refused := []string{"key with space", "key/slash", "ключ", strings_repeat("a", uriKeyMax+1)}
	for _, key := range refused {
		u := mcpURI{Key: key}
		if err := u.validKey(); err == nil {
			t.Errorf("key %q was accepted", key)
		}
	}
	for _, key := range []string{"prof-7", "a.b_c", "A1"} {
		if err := (mcpURI{Key: key}).validKey(); err != nil {
			t.Errorf("key %q was refused: %s", key, err)
		}
	}
}

func strings_repeat(s string, n int) string {
	out := make([]byte, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, s...)
	}
	return string(out)
}

func TestCanonicalScopeEscapes(t *testing.T) {
	u := mcpURI{Node: "node@host", Lens: "log", Scope: url.Values{"pattern": {"a b&c"}}}
	want := "ergo://node@host/log?pattern=a+b%26c"
	if got := u.Canonical(); got != want {
		t.Errorf("canonical is %q, want %q", got, want)
	}
}
