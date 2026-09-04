package grid

import (
	"testing"
	"time"

	"ergo.services/ergo/act"
	"ergo.services/ergo/gen"
	"ergo.services/ergo/testing/stage"
)

type ownerStart struct{ Key string }

type ownerDispatch struct {
	Key string
	Msg any
}

type ownerCount struct{ Key string }

func factoryGroupOwner() gen.ProcessBehavior { return &groupOwner{} }

type groupOwner struct {
	act.Actor
}

func (o *groupOwner) HandleCall(from gen.PID, ref gen.Ref, request any) (any, error) {
	switch r := request.(type) {
	case ownerStart:
		if err := Register(o, "grid", r.Key, nil); err != nil {
			return holderResult{Err: err}, nil
		}
		return holderResult{Err: OpenGroup(o, "grid", r.Key)}, nil
	case ownerDispatch:
		return holderResult{Err: Dispatch(o, "grid", r.Key, r.Msg)}, nil
	case ownerCount:
		n, err := MemberCount(o, "grid", r.Key)
		if err != nil {
			return holderResult{Err: err}, nil
		}
		return n, nil
	}
	return holderResult{Err: gen.ErrUnsupported}, nil
}

type memberJoin struct{ Key string }

type memberLeave struct{}

type memberGot struct{}

type memberGotResult struct{ Msgs []any }

func factoryGroupMember() gen.ProcessBehavior { return &groupMember{} }

type groupMember struct {
	act.Actor

	joined gen.Event
	got    []any
}

func (m *groupMember) HandleCall(from gen.PID, ref gen.Ref, request any) (any, error) {
	switch r := request.(type) {
	case memberJoin:
		ev, err := Join(m, "grid", r.Key)
		if err == nil {
			m.joined = ev
		}
		return holderResult{Err: err}, nil
	case memberLeave:
		return holderResult{Err: Leave(m, m.joined)}, nil
	case memberGot:
		return memberGotResult{Msgs: append([]any(nil), m.got...)}, nil
	}
	return holderResult{Err: gen.ErrUnsupported}, nil
}

func (m *groupMember) HandleEvent(message gen.MessageEvent) error {
	m.got = append(m.got, message.Message)
	return nil
}

func ownerCallOK(t *testing.T, n *stage.Node, pid gen.PID, request any) {
	t.Helper()
	v, err := n.Call(pid, request)
	if err != nil {
		t.Fatalf("%s: call: %s", n.Name(), err)
	}
	if res, ok := v.(holderResult); ok && res.Err != nil {
		t.Fatalf("%s: %+v: %s", n.Name(), request, res.Err)
	}
}

func assertMemberGot(t *testing.T, n *stage.Node, member gen.PID, want any) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		v, err := n.Call(member, memberGot{})
		if err == nil {
			for _, m := range v.(memberGotResult).Msgs {
				if m == want {
					return
				}
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("%s: member never received %v", n.Name(), want)
}

// A member on node B joins a group owned on node A and receives a dispatch.
func TestGroup_DispatchToMember(t *testing.T) {
	s := stage.New(t, stage.StageOptions{RegistrarFull: true})
	nA := s.StartNode("ga", stage.NodeOptions{
		Applications: []gen.ApplicationBehavior{CreateApp(Options{Domain: "grid"})},
	})
	nB := s.StartNode("gb", stage.NodeOptions{
		Applications: []gen.ApplicationBehavior{CreateApp(Options{Domain: "grid"})},
	})
	assertPeersEventually(t, nA, "grid", []gen.Atom{nB.Name()})

	owner := nA.Spawn(factoryGroupOwner, gen.ProcessOptions{})
	ownerCallOK(t, nA, owner, ownerStart{Key: "obj:1"})
	assertLookupEventually(t, nB, "obj:1", owner)

	member := nB.Spawn(factoryGroupMember, gen.ProcessOptions{})
	ownerCallOK(t, nB, member, memberJoin{Key: "obj:1"})

	ownerCallOK(t, nA, owner, ownerDispatch{Key: "obj:1", Msg: "hello"})
	assertMemberGot(t, nB, member, "hello")
}

// Leave unsubscribes via the joined event handle, independent of the registry;
// MemberCount drops back to zero.
func TestGroup_LeaveByHandle(t *testing.T) {
	s := stage.New(t, stage.StageOptions{RegistrarFull: true})
	n := s.StartNode("gl", stage.NodeOptions{
		Applications: []gen.ApplicationBehavior{CreateApp(Options{Domain: "grid"})},
	})

	owner := n.Spawn(factoryGroupOwner, gen.ProcessOptions{})
	ownerCallOK(t, n, owner, ownerStart{Key: "room:9"})

	member := n.Spawn(factoryGroupMember, gen.ProcessOptions{})
	ownerCallOK(t, n, member, memberJoin{Key: "room:9"})
	if v, _ := n.Call(owner, ownerCount{Key: "room:9"}); v.(int) != 1 {
		t.Fatalf("after join: member count = %v, want 1", v)
	}

	ownerCallOK(t, n, member, memberLeave{})
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if v, _ := n.Call(owner, ownerCount{Key: "room:9"}); v.(int) == 0 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("member count did not drop to 0 after Leave")
}

// A local member receives a dispatch and MemberCount reflects it.
func TestGroup_LocalDispatchAndCount(t *testing.T) {
	s := stage.New(t, stage.StageOptions{RegistrarFull: true})
	n := s.StartNode("gc", stage.NodeOptions{
		Applications: []gen.ApplicationBehavior{CreateApp(Options{Domain: "grid"})},
	})

	owner := n.Spawn(factoryGroupOwner, gen.ProcessOptions{})
	ownerCallOK(t, n, owner, ownerStart{Key: "room:7"})

	member := n.Spawn(factoryGroupMember, gen.ProcessOptions{})
	ownerCallOK(t, n, member, memberJoin{Key: "room:7"})

	v, err := n.Call(owner, ownerCount{Key: "room:7"})
	if err != nil {
		t.Fatalf("count: %s", err)
	}
	if c := v.(int); c != 1 {
		t.Fatalf("member count = %d, want 1", c)
	}

	ownerCallOK(t, n, owner, ownerDispatch{Key: "room:7", Msg: "tick"})
	assertMemberGot(t, n, member, "tick")
}
