package access

import (
	"testing"

	"ergo.services/ergo/gen"
)

// read-only is about the plane, not about a list: a mutating capability this build has never
// heard of must still be refused
func TestReadOnlyRefusesTheMutatingPlane(t *testing.T) {
	c := Ceiling{ReadOnly: true}

	for _, name := range []string{"manage.kill", "manage.send", "manage.something_new"} {
		if c.Allows(name) {
			t.Errorf("read-only allowed %q", name)
		}
	}
	for _, name := range []string{"inspect.node", "inspect.process_state"} {
		if c.Allows(name) == false {
			t.Errorf("read-only refused %q", name)
		}
	}
}

func TestAllowAndDeny(t *testing.T) {
	c := Ceiling{Allow: []string{"manage.kill", "manage.send"}, Deny: []string{"manage.kill"}}

	if c.Allows("manage.send") == false {
		t.Error("an allowed name was refused")
	}
	if c.Allows("manage.kill") {
		t.Error("deny must win over allow")
	}
	if c.Allows("manage.app_stop") {
		t.Error("a name outside a non-empty allow list was permitted")
	}
	if (Ceiling{}).Allows("manage.app_stop") == false {
		t.Error("an empty ceiling must not narrow")
	}
}

func TestNodes(t *testing.T) {
	if (Ceiling{}).AllowsNode("any@host") == false {
		t.Error("an empty list must not narrow")
	}
	c := Ceiling{Nodes: []string{"a@host"}}
	if c.AllowsNode("a@host") == false || c.AllowsNode("b@host") {
		t.Error("the node list is not honoured")
	}
}

func TestNarrowOnlyTightens(t *testing.T) {
	deployment := Ceiling{Deny: []string{"manage.app_unload"}, Nodes: []string{"a@host", "b@host"}}
	listener := Ceiling{ReadOnly: true, Nodes: []string{"b@host", "c@host"}}

	e := Narrow(deployment, listener)

	if e.ReadOnly == false {
		t.Error("read-only did not spread")
	}
	if len(e.Deny) != 1 || e.Deny[0] != "manage.app_unload" {
		t.Errorf("deny came out as %v", e.Deny)
	}
	if len(e.Nodes) != 1 || e.Nodes[0] != "b@host" {
		t.Errorf("nodes came out as %v, want the intersection", e.Nodes)
	}

	kept := Narrow(deployment, Ceiling{})
	if len(kept.Nodes) != 2 || len(kept.Deny) != 1 {
		t.Errorf("an empty inner ceiling widened the result: %+v", kept)
	}
	strict := Narrow(Ceiling{}, listener)
	if strict.ReadOnly == false || len(strict.Nodes) != 2 {
		t.Errorf("an empty outer ceiling widened the result: %+v", strict)
	}
}

func TestFilterKeepsOrder(t *testing.T) {
	c := Ceiling{ReadOnly: true}
	got := c.Filter([]string{"inspect.node", "manage.kill", "inspect.log"})

	if len(got) != 2 || got[0] != "inspect.node" || got[1] != "inspect.log" {
		t.Errorf("filter came out as %v", got)
	}
}

// two levels with nothing in common permit nothing, not everything
func TestNarrowWithNothingInCommon(t *testing.T) {
	e := Narrow(
		Ceiling{Allow: []string{"inspect.node"}, Nodes: []string{"a@host"}},
		Ceiling{Allow: []string{"inspect.process"}, Nodes: []string{"b@host"}},
	)

	for _, name := range []string{"manage.kill", "inspect.node", "inspect.process"} {
		if e.Allows(name) {
			t.Errorf("neither side named %q, and it is permitted", name)
		}
	}
	for _, node := range []gen.Atom{"a@host", "b@host", "evil@host"} {
		if e.AllowsNode(node) {
			t.Errorf("neither side named %s, and it is permitted", node)
		}
	}
	if len(e.Filter([]string{"inspect.node", "manage.kill"})) != 0 {
		t.Error("the browser was offered a capability that nothing permits")
	}
}

func TestNarrowKeepsAnEmptyList(t *testing.T) {
	nothing := Ceiling{Allow: []string{}, Nodes: []string{}}

	under := Narrow(nothing, Ceiling{Allow: []string{"inspect.node"}, Nodes: []string{"a@host"}})
	if under.Allows("inspect.node") || under.AllowsNode("a@host") {
		t.Errorf("an empty outer ceiling was widened from below: %+v", under)
	}

	over := Narrow(Ceiling{Allow: []string{"inspect.node"}, Nodes: []string{"a@host"}}, nothing)
	if over.Allows("inspect.node") || over.AllowsNode("a@host") {
		t.Errorf("an empty inner ceiling was widened from above: %+v", over)
	}

	open := Narrow(Ceiling{}, Ceiling{})
	if open.Allows("manage.kill") == false || open.AllowsNode("any@host") == false {
		t.Errorf("two unset ceilings narrowed something: %+v", open)
	}
	if open.Allow != nil || open.Nodes != nil {
		t.Errorf("unset came out present: %+v", open)
	}
}
