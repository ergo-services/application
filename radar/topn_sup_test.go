package radar

import (
	"testing"
	"time"

	"ergo.services/actor/metrics"
	"ergo.services/ergo/act"
	"ergo.services/ergo/gen"
	"ergo.services/ergo/testing/check"
	"ergo.services/ergo/testing/unit"
)

func spawnTopNSup(t *testing.T, interval time.Duration) (*unit.Subject, *metrics.Shared) {
	t.Helper()
	shared := metrics.NewShared()
	sub, err := unit.Spawn(t, factoryTopNSup, gen.ProcessOptions{}, shared, interval)
	if err != nil {
		t.Fatalf("Init: %s", err)
	}
	return sub, shared
}

func TestTopNSupInitRejectsBadArgs(t *testing.T) {
	shared := metrics.NewShared()
	for _, tc := range []struct {
		name string
		args []any
	}{
		{"no args", nil},
		{"one arg", []any{shared}},
		{"shared of another type", []any{"not shared", time.Second}},
		{"interval of another type", []any{shared, 5}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := unit.Spawn(t, factoryTopNSup, gen.ProcessOptions{}, tc.args...); err == nil {
				t.Fatal("the supervisor started with bad args")
			}
		})
	}
}

// Every top-N metric gets its own actor, spawned on demand from one template,
// and an actor that dies must not take its siblings or the supervisor with it.
func TestTopNSupSpecIsAnOnDemandTemplate(t *testing.T) {
	sub, _ := spawnTopNSup(t, time.Second)

	spec, err := sub.Behavior().(*topNSup).Init(metrics.NewShared(), time.Second)
	if err != nil {
		t.Fatalf("Init: %s", err)
	}
	if spec.Type != act.SupervisorTypeSimpleOneForOne {
		t.Errorf("type is %v, want SimpleOneForOne", spec.Type)
	}
	if spec.Restart.Strategy != act.SupervisorStrategyTransient {
		t.Errorf("strategy is %v, want Transient", spec.Restart.Strategy)
	}
	if len(spec.Children) != 1 {
		t.Fatalf("expected one child template, got %d", len(spec.Children))
	}
	if spec.Children[0].Args != nil {
		t.Error("the template carries args, so every worker would start with the same ones")
	}
}

func TestTopNSupRegisterStartsAWorker(t *testing.T) {
	sub, _ := spawnTopNSup(t, 3*time.Second)

	client := gen.PID{Node: "test@localhost", ID: 42, Creation: 1}
	resp, err := sub.Call(client, metrics.RegisterTopNRequest{
		Name:   "queue_depth",
		Help:   "deepest queues",
		Labels: []string{"pid"},
		TopN:   5,
		Order:  metrics.TopNMax,
	})
	check.NoError(t, err)

	registered, ok := resp.(metrics.RegisterResponse)
	if ok == false {
		t.Fatalf("answered with %T", resp)
	}
	if registered.Error != "" {
		t.Fatalf("registration reported %q", registered.Error)
	}

	sub.ShouldSpawn().
		Where(func(r check.Spawn) bool { return r.Error == nil }).
		Once().
		Assert()

	// The flush interval the workers inherit comes from Init, not from the
	// request - the request has no field for it.
	if got := sub.Behavior().(*topNSup).interval; got != 3*time.Second {
		t.Errorf("the supervisor kept %v as the flush interval, want 3s", got)
	}
}

func TestTopNSupRegisterReportsAFailedStart(t *testing.T) {
	sub, _ := spawnTopNSup(t, time.Second)
	sub.OnSpawn(metrics.TopNActorFactory).Fail(gen.ErrProcessTerminated)

	client := gen.PID{Node: "test@localhost", ID: 42, Creation: 1}
	resp, err := sub.Call(client, metrics.RegisterTopNRequest{Name: "m", TopN: 1})
	check.NoError(t, err)

	registered, ok := resp.(metrics.RegisterResponse)
	if ok == false {
		t.Fatalf("answered with %T", resp)
	}
	if registered.Error == "" {
		t.Fatal("a failed start was reported as a successful registration")
	}
	if sub.Terminated() {
		t.Error("one caller's bad registration took the supervisor down")
	}
}

func TestTopNSupIgnoresWhatItDoesNotHandle(t *testing.T) {
	sub, _ := spawnTopNSup(t, time.Second)
	client := gen.PID{Node: "test@localhost", ID: 42, Creation: 1}

	resp, err := sub.Call(client, "not a request")
	check.NoError(t, err)
	check.Nil(t, resp)

	sub.SendMessage(client, "not a message")
	if sub.Terminated() {
		t.Error("an unknown message terminated the supervisor")
	}
	sub.ShouldSpawn().None().Assert()
}
