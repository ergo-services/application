// Package access is the vocabulary of observer authorization: what a caller is allowed to
// ask for. It carries no dependencies beyond the framework, so an authorizer can be a
// module of its own without pulling the observer in.
package access

import (
	"slices"
	"strings"

	"ergo.services/ergo/gen"
)

// manage names the mutating plane. A capability outside it only reads.
const manage = "manage."

// Ceiling is the limit of what a caller may ask. Every level of the configuration carries
// one, and a level can only narrow what it was given.
type Ceiling struct {
	// ReadOnly refuses every mutating capability.
	ReadOnly bool

	// Allow lists the capability names permitted. Unset does not narrow; present and empty
	// permits nothing.
	Allow []string

	// Deny lists the capability names refused. Applied after Allow.
	Deny []string

	// Nodes lists the target nodes permitted. Unset does not narrow; present and empty
	// permits no node.
	Nodes []string
}

// Mutating reports whether a capability name belongs to the mutating plane.
func Mutating(capability string) bool {
	return strings.HasPrefix(capability, manage)
}

// Allows reports whether the ceiling permits one capability.
func (c Ceiling) Allows(capability string) bool {
	if c.ReadOnly && Mutating(capability) {
		return false
	}
	if slices.Contains(c.Deny, capability) {
		return false
	}
	// unset, not empty: two allowlists with nothing in common intersect to empty
	if c.Allow == nil {
		return true
	}
	return slices.Contains(c.Allow, capability)
}

// AllowsNode reports whether the ceiling permits a node as the target.
func (c Ceiling) AllowsNode(node gen.Atom) bool {
	if c.Nodes == nil {
		return true
	}
	return slices.Contains(c.Nodes, string(node))
}

// Filter keeps the capability names the ceiling permits, in the order given.
func (c Ceiling) Filter(capabilities []string) []string {
	out := make([]string, 0, len(capabilities))
	for _, name := range capabilities {
		if c.Allows(name) {
			out = append(out, name)
		}
	}
	return out
}

// Narrow composes two ceilings. The result is never wider than either of them: ReadOnly
// spreads, Deny accumulates, and a non-empty list intersects with a non-empty one.
func Narrow(outer, inner Ceiling) Ceiling {
	return Ceiling{
		ReadOnly: outer.ReadOnly || inner.ReadOnly,
		Allow:    intersect(outer.Allow, inner.Allow),
		Deny:     union(outer.Deny, inner.Deny),
		Nodes:    intersect(outer.Nodes, inner.Nodes),
	}
}

// Widen answers with the wider of two comparable ceilings. A union of two is not a ceiling in
// general: one role refusing a capability by ReadOnly and another by name compose, term by
// term, into one that permits it, so an incomparable pair narrows instead.
func Widen(one, other Ceiling) Ceiling {
	switch {
	case AtLeast(one, other):
		return one
	case AtLeast(other, one):
		return other
	}
	return Narrow(one, other)
}

// AtLeast reports whether outer permits everything inner does. It reads the limits, not the
// capability names, so it answers no for a pair it cannot prove comparable.
func AtLeast(outer, inner Ceiling) bool {
	if outer.ReadOnly && inner.ReadOnly == false {
		return false
	}
	if covers(inner.Allow, outer.Allow) == false {
		return false
	}
	if covers(inner.Nodes, outer.Nodes) == false {
		return false
	}
	for _, name := range outer.Deny {
		if slices.Contains(inner.Deny, name) == false {
			return false
		}
	}
	return true
}

// unset names everything, so only an unset list covers it
func covers(narrow, wide []string) bool {
	if wide == nil {
		return true
	}
	if narrow == nil {
		return false
	}
	for _, name := range narrow {
		if slices.Contains(wide, name) == false {
			return false
		}
	}
	return true
}

// intersect keeps what both name. Unset does not narrow, present and empty narrows to nothing.
func intersect(outer, inner []string) []string {
	if outer == nil {
		return slices.Clone(inner)
	}
	if inner == nil {
		return slices.Clone(outer)
	}
	return slices.DeleteFunc(slices.Clone(outer), func(name string) bool {
		return slices.Contains(inner, name) == false
	})
}

// union is a set: Deny is membership, so order carries nothing.
func union(outer, inner []string) []string {
	out := append(slices.Clone(outer), inner...)
	slices.Sort(out)
	return slices.Compact(out)
}
