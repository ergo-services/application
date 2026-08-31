package observer

import (
	"reflect"
	"strings"
	"testing"

	"ergo.services/ergo/gen"
)

func reflectZero(typ reflect.Type) any {
	return reflect.New(typ).Elem().Interface()
}

func TestIdentifiersSurviveTheRoundTrip(t *testing.T) {
	nodes := []gen.Atom{
		"shop-basket@localhost",
		"shop.basket.eu-west-1@k8s.cluster.local",
	}

	for _, node := range nodes {
		pid := gen.PID{Node: node, ID: 1001, Creation: 1787725228}
		back, err := mcpParsePID(mcpPIDText(pid), "")
		if err != nil {
			t.Errorf("%s: %s", mcpPIDText(pid), err)
		} else if back != pid {
			t.Errorf("%s came back as %#v", mcpPIDText(pid), back)
		}

		alias := gen.Alias{Node: node, ID: [3]uint64{7, 8, 9}, Creation: 1787725228}
		aliasBack, err := mcpParseAlias(mcpRefText(gen.Ref(alias)), "")
		if err != nil {
			t.Errorf("%s: %s", mcpRefText(gen.Ref(alias)), err)
		} else if aliasBack != alias {
			t.Errorf("alias came back as %#v", aliasBack)
		}

		ref := gen.Ref{Node: node, ID: [3]uint64{1, 2, 3}, Creation: 5}
		refBack, err := mcpParseRef(mcpRefText(ref), "")
		if err != nil {
			t.Errorf("%s: %s", mcpRefText(ref), err)
		} else if refBack != ref {
			t.Errorf("ref came back as %#v", refBack)
		}

		event := gen.Event{Name: "core", Node: node}
		eventBack, err := mcpParseEvent(mcpEventText(event), "")
		if err != nil {
			t.Errorf("%s: %s", mcpEventText(event), err)
		} else if eventBack != event {
			t.Errorf("event came back as %#v", eventBack)
		}
	}
}

func TestIdentifiersTakeTheNodeFromEitherSide(t *testing.T) {
	node := gen.Atom("shop.basket@k8s.cluster.local")
	pid := gen.PID{Node: node, ID: 1001, Creation: 7}
	alias := gen.Alias{Node: node, ID: [3]uint64{7, 8, 9}, Creation: 7}
	event := gen.Event{Name: "a@b", Node: node}

	for _, c := range []struct {
		text string
		in   gen.Atom
	}{
		{mcpPIDText(pid), ""},
		{mcpPIDText(pid), node},
		{"1001.7", node},
	} {
		if back, err := mcpParsePID(c.text, c.in); err != nil || back != pid {
			t.Errorf("pid %q in %q: %#v %v", c.text, string(c.in), back, err)
		}
	}

	if back, err := mcpParseAlias("7.8.9.7", node); err != nil || back != alias {
		t.Errorf("alias left out: %#v %v", back, err)
	}
	if back, err := mcpParseEvent("a@b", node); err != nil || back != event {
		t.Errorf("event left out: %#v %v", back, err)
	}

	if _, err := mcpParsePID(mcpPIDText(pid), "other@host"); err == nil {
		t.Error("a pid of another node was accepted")
	}
	if _, err := mcpParsePID("1001.7", ""); err == nil {
		t.Error("a pid without a node was accepted where none is known")
	}

	// a name holding a colon of its own has to be written with its node, because the first
	// colon is the cut
	withColon := gen.Event{Name: "a:b", Node: node}
	if back, err := mcpParseEvent(mcpEventText(withColon), ""); err != nil || back != withColon {
		t.Errorf("written out: %#v %v", back, err)
	}
	if back, err := mcpParseEvent("a:b", node); err == nil {
		t.Errorf("left out, a name with a colon was read as %#v", back)
	}
}

func TestIdentifiersNameTheirNode(t *testing.T) {
	pid := gen.PID{Node: "worker@remote", ID: 3, Creation: 9}
	text := mcpPIDText(pid)

	if strings.Contains(text, "worker@remote") == false {
		t.Errorf("%q does not name its node", text)
	}
	if strings.HasPrefix(text, "worker@remote"+mcpIdentSep) == false {
		t.Errorf("%q does not start with the node", text)
	}
	// gen.Atom prints itself in single quotes, and a quoted name cannot be handed back
	if strings.Contains(text, "'") {
		t.Errorf("%q carries quotes", text)
	}

	other := gen.PID{Node: "worker@elsewhere", ID: 3, Creation: 9}
	if mcpPIDText(pid) == mcpPIDText(other) {
		t.Error("two nodes produced one text")
	}
	if text == pid.String() {
		t.Errorf("the surface form and the log form are the same: %q", text)
	}
}

func TestNamesMayCarryAnySeparator(t *testing.T) {
	nodes := []gen.Atom{"shop@localhost", "shop.eu@k8s.cluster.local"}
	names := []string{
		"core", "billing@v2", "a@b@c", "@leading", "worker/1", "a:b", "a:b:c", "тревога", "a b",
	}

	for _, node := range nodes {
		for _, name := range names {
			event := gen.Event{Name: gen.Atom(name), Node: node}
			back, err := mcpParseEvent(mcpEventText(event), "")
			if err != nil {
				t.Errorf("event %q: %s", mcpEventText(event), err)
				continue
			}
			if back != event {
				t.Errorf("event %q came back as %#v", mcpEventText(event), back)
			}

			id := gen.ProcessID{Name: gen.Atom(name), Node: node}
			idBack, err := mcpParseProcessID(mcpProcessIDText(id), "")
			if err != nil {
				t.Errorf("process %q: %s", mcpProcessIDText(id), err)
				continue
			}
			if idBack != id {
				t.Errorf("process %q came back as %#v", mcpProcessIDText(id), idBack)
			}
		}
	}
}

func TestIdentifiersRefuseNonsense(t *testing.T) {
	cases := []string{
		"",
		"1001.1787725228",
		"node@host",
		"node@host:1001",
		"node@host:x.1787725228} ",
		"node@host:-1.5",
		":1001.5",
	}

	for _, text := range cases {
		if pid, err := mcpParsePID(text, ""); err == nil {
			t.Errorf("%q was read as %#v", text, pid)
		}
	}

	for _, text := range []string{"", "core", "node@host:", ":core"} {
		if event, err := mcpParseEvent(text, ""); err == nil {
			t.Errorf("%q was read as %#v", text, event)
		}
	}
}

// the renderer decides by type at start, so the two tables have to agree
func TestIdentTextCoversTheIdentifierTypes(t *testing.T) {
	for typ := range mcpIdentTypes {
		value := reflectZero(typ)
		if _, ok := mcpIdentText(value); ok == false {
			t.Errorf("%s is planned as one value but has no text", typ)
		}
	}

	if _, ok := mcpIdentText("just a string"); ok {
		t.Error("a string was taken for an identifier")
	}
	if _, ok := mcpIdentText(gen.Version{Name: "x", Release: "1"}); ok {
		t.Error("a Version was taken for an identifier")
	}

	for _, value := range []any{
		gen.PID{}, gen.Alias{}, gen.Ref{}, gen.Event{}, gen.ProcessID{},
	} {
		if mcpIdentTypes[reflect.TypeOf(value)] == false {
			t.Errorf("%T has a text but the plan does not call it one value", value)
		}
	}
}
