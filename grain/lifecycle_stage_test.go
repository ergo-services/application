package grain

import (
	"context"
	"encoding/binary"
	"testing"
	"time"

	"ergo.services/application/grain/store"
	"ergo.services/application/grain/store/mem"
	"ergo.services/ergo/act"
	"ergo.services/ergo/gen"
	"ergo.services/ergo/testing/stage"
)

//
// test grain
//

type MessageIncr struct{}
type MessagePanic struct{}
type GetRequest struct{}
type GetResponse struct{ N int }

type counter struct {
	Actor
	n int
}

func newCounter() ActorBehavior { return &counter{} }

func (c *counter) Init(key string, state []byte) error {
	if len(state) == 8 {
		c.n = int(binary.BigEndian.Uint64(state))
	}
	return nil
}

func (c *counter) HandleMessage(_ gen.PID, msg any) error {
	switch msg.(type) {
	case MessageIncr:
		c.n++
	case MessagePanic:
		panic("boom")
	}
	return nil
}

func (c *counter) HandleCall(_ gen.PID, _ gen.Ref, req any) (any, error) {
	if _, ok := req.(GetRequest); ok {
		return GetResponse{N: c.n}, nil
	}
	return nil, gen.ErrUnsupported
}

func (c *counter) Snapshot() ([]byte, error) {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, uint64(c.n))
	return b, nil
}

//
// client actor: exercises the public API from a real gen.Process
//

type doActivate struct {
	domain gen.Atom
	key    string
}
type activateResult struct {
	pid gen.PID
	err string
}

type client struct{ act.Actor }

func newClient() gen.ProcessBehavior { return &client{} }

func (c *client) HandleCall(_ gen.PID, _ gen.Ref, req any) (any, error) {
	if r, ok := req.(doActivate); ok {
		pid, err := Activate(c, r.domain, r.key)
		res := activateResult{pid: pid}
		if err != nil {
			res.err = err.Error()
		}
		return res, nil
	}
	return nil, gen.ErrUnsupported
}

//
// helpers
//

var testctx = context.Background()

func startGrain(t *testing.T, o Options) (*stage.Node, *mem.Store) {
	t.Helper()
	ms := mem.New()
	o.Store = ms
	o.Factory = newCounter
	s := stage.New(t)
	n := s.StartNode("gn", stage.NodeOptions{
		Applications: []gen.ApplicationBehavior{CreateApp(o)},
	})
	return n, ms
}

func activateDirect(t *testing.T, n *stage.Node, domain gen.Atom, key string) gen.PID {
	t.Helper()
	to := n.ProcessID(activatorName(domain, activatorIndexFor(n.Name(), domain, key)))
	v, err := n.Call(to, activateRequest{key: key})
	if err != nil {
		t.Fatalf("activate %q: %v", key, err)
	}
	return v.(activateResponse).pid
}

func getN(t *testing.T, n *stage.Node, pid gen.PID) int {
	t.Helper()
	v, err := n.Call(pid, GetRequest{})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	return v.(GetResponse).N
}

func waitGone(t *testing.T, n *stage.Node, domain gen.Atom, key string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := whereisAt(n.Name(), domain, key); ok == false {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("grain %q still live after idle window", key)
}

//
// tests
//

func TestActivateAndCall(t *testing.T) {
	n, ms := startGrain(t, Options{})

	pid := activateDirect(t, n, DefaultDomain, "c1")
	n.Send(pid, MessageIncr{})
	n.Send(pid, MessageIncr{})
	n.Send(pid, MessageIncr{})
	if got := getN(t, n, pid); got != 3 {
		t.Fatalf("count: got %d, want 3", got)
	}
	if _, ok := whereisAt(n.Name(), DefaultDomain, "c1"); ok == false {
		t.Fatal("grain not in live map")
	}
	st, err := ms.Read(testctx, "c1")
	if err != nil {
		t.Fatalf("stamp read: %v", err)
	}
	if st.Status != store.StatusRunning {
		t.Fatalf("stamp status: got %d, want Running", st.Status)
	}
}

func TestResumeAfterIdle(t *testing.T) {
	n, ms := startGrain(t, Options{IdleTimeout: 200 * time.Millisecond})

	pid := activateDirect(t, n, DefaultDomain, "c1")
	for i := 0; i < 5; i++ {
		n.Send(pid, MessageIncr{})
	}
	if got := getN(t, n, pid); got != 5 {
		t.Fatalf("count before idle: got %d, want 5", got)
	}

	waitGone(t, n, DefaultDomain, "c1")
	st, err := ms.Read(testctx, "c1")
	if err != nil {
		t.Fatalf("stamp read after idle: %v", err)
	}
	if st.Status != store.StatusReleasedClean {
		t.Fatalf("stamp status after idle: got %d, want ReleasedClean", st.Status)
	}

	pid2 := activateDirect(t, n, DefaultDomain, "c1")
	if pid2 == pid {
		t.Fatal("reactivation returned the old PID")
	}
	if got := getN(t, n, pid2); got != 5 {
		t.Fatalf("resumed count: got %d, want 5", got)
	}
}

func TestDelete(t *testing.T) {
	n, ms := startGrain(t, Options{})

	activateDirect(t, n, DefaultDomain, "c1")
	to := n.ProcessID(activatorName(DefaultDomain, activatorIndexFor(n.Name(), DefaultDomain, "c1")))
	if _, err := n.Call(to, deleteRequest{key: "c1"}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	waitGone(t, n, DefaultDomain, "c1")
	if _, err := ms.Read(testctx, "c1"); err != store.ErrNoStamp {
		t.Fatalf("stamp after delete: got %v, want ErrNoStamp", err)
	}
	if _, err := ms.LoadState(testctx, "c1"); err != store.ErrNoState {
		t.Fatalf("state after delete: got %v, want ErrNoState", err)
	}
}

// Panic terminates the grain via the Crashed branch and does NOT flush state.
func TestPanicNoFlush(t *testing.T) {
	n, ms := startGrain(t, Options{SyncEvery: 0})

	pid := activateDirect(t, n, DefaultDomain, "c1")
	n.Send(pid, MessageIncr{}) // in-memory only; SyncEvery=0 never persists
	n.Send(pid, MessagePanic{})

	waitGone(t, n, DefaultDomain, "c1")
	st, err := ms.Read(testctx, "c1")
	if err != nil {
		t.Fatalf("stamp read after panic: %v", err)
	}
	if st.Status != store.StatusCrashed {
		t.Fatalf("stamp status after panic: got %d, want Crashed", st.Status)
	}
	if _, err := ms.LoadState(testctx, "c1"); err != store.ErrNoState {
		t.Fatalf("panic must not flush: got state err %v, want ErrNoState", err)
	}
}

// A graceful node stop delivers Shutdown; the grain flushes and releases clean.
func TestShutdownFlush(t *testing.T) {
	n, ms := startGrain(t, Options{SyncEvery: 0})

	pid := activateDirect(t, n, DefaultDomain, "c1")
	for i := 0; i < 4; i++ {
		n.Send(pid, MessageIncr{})
	}
	if got := getN(t, n, pid); got != 4 {
		t.Fatalf("count: got %d, want 4", got)
	}

	n.Native().Stop() // graceful

	deadline := time.Now().Add(5 * time.Second)
	for {
		st, err := ms.Read(testctx, "c1")
		if err == nil && st.Status == store.StatusReleasedClean {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no clean release after shutdown: stamp=%v err=%v", st, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	state, err := ms.LoadState(testctx, "c1")
	if err != nil {
		t.Fatalf("load after shutdown: %v", err)
	}
	if n := int(binary.BigEndian.Uint64(state)); n != 4 {
		t.Fatalf("shutdown flush: got %d, want 4", n)
	}
}

// Repeated idle-deactivate then reactivate never fails: the lock is released in
// the deactivation handler, before the process name is freed.
func TestReactivateNoRace(t *testing.T) {
	n, _ := startGrain(t, Options{IdleTimeout: 100 * time.Millisecond})

	for i := 0; i < 5; i++ {
		pid := activateDirect(t, n, DefaultDomain, "race")
		n.Send(pid, MessageIncr{})
		getN(t, n, pid) // force handling
		waitGone(t, n, DefaultDomain, "race")
	}
	// final resume must carry the accumulated state
	pid := activateDirect(t, n, DefaultDomain, "race")
	if got := getN(t, n, pid); got != 5 {
		t.Fatalf("accumulated count: got %d, want 5", got)
	}
}

// The public Activate helper routes correctly and its fast path is stable.
func TestPublicActivate(t *testing.T) {
	n, _ := startGrain(t, Options{})

	c := n.Spawn(newClient, gen.ProcessOptions{})
	v, err := n.Call(c, doActivate{domain: DefaultDomain, key: "c1"})
	if err != nil {
		t.Fatalf("client activate: %v", err)
	}
	res := v.(activateResult)
	if res.err != "" {
		t.Fatalf("Activate returned error: %s", res.err)
	}
	direct := activateDirect(t, n, DefaultDomain, "c1")
	if res.pid != direct {
		t.Fatalf("Activate pid %s != direct %s", res.pid, direct)
	}
}
