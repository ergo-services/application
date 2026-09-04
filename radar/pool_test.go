package radar

import (
	"testing"

	"ergo.services/actor/metrics"
	"ergo.services/ergo/act"
	"ergo.services/ergo/gen"
	"ergo.services/ergo/testing/check"
	"ergo.services/ergo/testing/unit"
)

func TestPoolInitRejectsBadArgs(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []any
	}{
		{"no args", nil},
		{"args of another type", []any{"not pool options"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := unit.Spawn(t, factoryMetricsPool, gen.ProcessOptions{}, tc.args...); err == nil {
				t.Fatal("the pool started with bad args")
			}
		})
	}
}

// The pool is what makes custom metrics survive load: the size radar was
// configured with has to become that many workers.
func TestPoolStartsAWorkerPerConfiguredSlot(t *testing.T) {
	sub, err := unit.Spawn(t, factoryMetricsPool, gen.ProcessOptions{}, act.PoolOptions{
		PoolSize:      4,
		WorkerFactory: metrics.Factory,
		WorkerArgs:    []any{metrics.Options{Shared: metrics.NewShared()}},
	})
	if err != nil {
		t.Fatalf("Init: %s", err)
	}

	sub.ShouldSpawn().
		Where(func(r check.Spawn) bool { return r.Error == nil }).
		Times(4).
		Assert()
}
