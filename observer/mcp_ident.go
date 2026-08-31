package observer

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"ergo.services/ergo/gen"
)

// node:rest. A node name cannot hold a colon, so the rest is opaque and may carry whatever a
// process or event name carries. The node may be left out where it is already known.
const mcpIdentSep = ":"

var mcpIdentTypes = map[reflect.Type]bool{
	reflect.TypeOf(gen.PID{}):       true,
	reflect.TypeOf(gen.Alias{}):     true,
	reflect.TypeOf(gen.Ref{}):       true,
	reflect.TypeOf(gen.Event{}):     true,
	reflect.TypeOf(gen.ProcessID{}): true,
}

func mcpIdentText(value any) (string, bool) {
	switch v := value.(type) {
	case gen.PID:
		return mcpPIDText(v), true
	case gen.Alias:
		return mcpRefText(gen.Ref(v)), true
	case gen.Ref:
		return mcpRefText(v), true
	case gen.Event:
		return mcpEventText(v), true
	case gen.ProcessID:
		return mcpProcessIDText(v), true
	}
	return "", false
}

// gen.Atom prints itself in single quotes, so the node is converted. An identifier nobody set
// is empty rather than a shape that reads as one.
func mcpPIDText(pid gen.PID) string {
	if pid == (gen.PID{}) {
		return ""
	}
	return fmt.Sprintf("%s%s%d.%d", string(pid.Node), mcpIdentSep, pid.ID, pid.Creation)
}

func mcpRefText(ref gen.Ref) string {
	if ref == (gen.Ref{}) {
		return ""
	}
	return fmt.Sprintf("%s%s%d.%d.%d.%d",
		string(ref.Node), mcpIdentSep, ref.ID[0], ref.ID[1], ref.ID[2], ref.Creation)
}

func mcpEventText(event gen.Event) string {
	if event == (gen.Event{}) {
		return ""
	}
	return string(event.Node) + mcpIdentSep + string(event.Name)
}

func mcpProcessIDText(id gen.ProcessID) string {
	if id == (gen.ProcessID{}) {
		return ""
	}
	return string(id.Node) + mcpIdentSep + string(id.Name)
}

func mcpTraceIDText(id [2]uint64) string {
	if id == [2]uint64{} {
		return ""
	}
	return fmt.Sprintf("%016x%016x", id[0], id[1])
}

func mcpSpanIDText(id uint64) string {
	if id == 0 {
		return ""
	}
	return fmt.Sprintf("%016x", id)
}

func mcpParsePID(text string, in gen.Atom) (gen.PID, error) {
	node, rest, err := mcpIdentNode(text, in)
	if err == nil {
		var numbers []uint64
		if numbers, err = mcpIdentNumbers(rest, 2); err == nil {
			return gen.PID{Node: node, ID: numbers[0], Creation: int64(numbers[1])}, nil
		}
	}
	return gen.PID{}, fmt.Errorf("process %q: %w, want [node:]ID.Creation", text, err)
}

func mcpParseAlias(text string, in gen.Atom) (gen.Alias, error) {
	ref, err := mcpParseRef(text, in)
	return gen.Alias(ref), err
}

func mcpParseRef(text string, in gen.Atom) (gen.Ref, error) {
	node, rest, err := mcpIdentNode(text, in)
	if err == nil {
		var numbers []uint64
		if numbers, err = mcpIdentNumbers(rest, 4); err == nil {
			return gen.Ref{
				Node:     node,
				ID:       [3]uint64{numbers[0], numbers[1], numbers[2]},
				Creation: int64(numbers[3]),
			}, nil
		}
	}
	return gen.Ref{}, fmt.Errorf("reference %q: %w, want [node:]ID.ID.ID.Creation", text, err)
}

func mcpParseEvent(text string, in gen.Atom) (gen.Event, error) {
	node, name, err := mcpIdentName(text, in)
	if err != nil {
		return gen.Event{}, fmt.Errorf("event %q: %w, want [node:]name", text, err)
	}
	return gen.Event{Name: gen.Atom(name), Node: node}, nil
}

func mcpParseProcessID(text string, in gen.Atom) (gen.ProcessID, error) {
	node, name, err := mcpIdentName(text, in)
	if err != nil {
		return gen.ProcessID{}, fmt.Errorf("process %q: %w, want [node:]name", text, err)
	}
	return gen.ProcessID{Name: gen.Atom(name), Node: node}, nil
}

// the node written into the text has to agree with the node already known
func mcpIdentNode(text string, in gen.Atom) (gen.Atom, string, error) {
	node, rest, written := strings.Cut(text, mcpIdentSep)
	if written == false {
		if in == "" {
			return "", "", fmt.Errorf("the node is missing")
		}
		return in, text, nil
	}
	switch {
	case node == "":
		return "", "", fmt.Errorf("the node is empty")
	case in != "" && gen.Atom(node) != in:
		return "", "", fmt.Errorf("it names node %s, not %s", node, string(in))
	}
	return gen.Atom(node), rest, nil
}

func mcpIdentName(text string, in gen.Atom) (gen.Atom, string, error) {
	node, name, err := mcpIdentNode(text, in)
	if err != nil {
		return "", "", err
	}
	if name == "" {
		return "", "", fmt.Errorf("the name is empty")
	}
	return node, name, nil
}

func mcpIdentNumbers(rest string, count int) ([]uint64, error) {
	fields := strings.Split(rest, ".")
	if len(fields) != count {
		return nil, fmt.Errorf("holds %d numbers, not %d", len(fields), count)
	}

	numbers := make([]uint64, count)
	for i, field := range fields {
		value, err := strconv.ParseUint(field, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%q is not a decimal number", field)
		}
		numbers[i] = value
	}
	return numbers, nil
}
