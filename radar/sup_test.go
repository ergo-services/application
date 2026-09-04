package radar

import (
	"net/http"
	"testing"
	"time"

	"ergo.services/actor/health"
	"ergo.services/actor/metrics"
	"ergo.services/ergo/act"
	"ergo.services/ergo/gen"
	"ergo.services/ergo/testing/check"
	"ergo.services/ergo/testing/unit"
)

// supEnv is what CreateApp/Load puts in the application env. A test that wants a
// broken env replaces or drops one entry.
func supEnv(options Options) map[gen.Env]any {
	return map[gen.Env]any{
		"mux":     http.NewServeMux(),
		"shared":  metrics.NewShared(),
		"options": options,
	}
}

func spawnSup(t *testing.T, env map[gen.Env]any) (*unit.Subject, error) {
	t.Helper()
	n := unit.StartNode(t, "test@localhost", gen.NodeOptions{Env: env})
	return n.Spawn(factoryRadarSup, gen.ProcessOptions{})
}

// The supervisor takes everything it needs from the application env, so a
// mistake there is not a compile error - it surfaces as a node that will not
// start. Each case below is one such mistake.
func TestSupInitRejectsBrokenEnv(t *testing.T) {
	for _, tc := range []struct {
		name  string
		spoil func(map[gen.Env]any)
	}{
		{"mux missing", func(e map[gen.Env]any) { delete(e, "mux") }},
		{"mux of another type", func(e map[gen.Env]any) { e["mux"] = "not a mux" }},
		{"shared missing", func(e map[gen.Env]any) { delete(e, "shared") }},
		{"shared of another type", func(e map[gen.Env]any) { e["shared"] = 42 }},
		{"options missing", func(e map[gen.Env]any) { delete(e, "options") }},
		{"options of another type", func(e map[gen.Env]any) { e["options"] = &Options{} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := supEnv(Options{})
			tc.spoil(env)
			if _, err := spawnSup(t, env); err == nil {
				t.Fatal("the supervisor started with a broken env")
			}
		})
	}
}

// Every child radar promises is started, under the name its helpers address.
// A renamed or dropped child leaves those helpers talking to nobody.
func TestSupStartsEveryChild(t *testing.T) {
	sub, err := spawnSup(t, supEnv(Options{}))
	if err != nil {
		t.Fatalf("Init: %s", err)
	}

	for _, name := range []gen.Atom{
		nameHealth, nameMetricsErgo, nameMetrics, nameTopNSup, nameWeb,
	} {
		sub.ShouldSpawn().
			Where(func(r check.Spawn) bool { return r.Register == name }).
			Once().
			Assert()
	}
	sub.ShouldSpawn().Times(5).Assert()
}

// The options reach the children they belong to. Nothing here is a compile
// error either: HealthPath and MetricsPath are both strings, both intervals are
// durations, so a swapped pair serves the wrong endpoint quietly.
func TestSupWiresOptionsIntoChildren(t *testing.T) {
	options := Options{
		Host: "0.0.0.0", Port: 9999,
		HealthPath: "/healthz", MetricsPath: "/prom",
		HealthCheckInterval:    2 * time.Second,
		MetricsCollectInterval: 3 * time.Second,
		MetricsTopN:            7,
		MetricsPoolSize:        9,
	}
	env := supEnv(options)
	sub, err := spawnSup(t, env)
	if err != nil {
		t.Fatalf("Init: %s", err)
	}

	// Init only reads the env and builds the spec, so asking it again is how a
	// test gets the spec to look at - the harness records which children were
	// started, but not the arguments they were started with.
	spec, err := sub.Behavior().(*radarSup).Init()
	if err != nil {
		t.Fatalf("Init: %s", err)
	}
	if len(spec.Children) != 5 {
		t.Fatalf("expected 5 children, got %d", len(spec.Children))
	}

	byName := map[gen.Atom]act.SupervisorChildSpec{}
	for _, c := range spec.Children {
		byName[c.Name] = c
	}
	// A missing or renamed child has to fail here rather than panic on the
	// argument lookups below, or one broken name hides every other mistake.
	argsOf := func(name gen.Atom, want int) []any {
		t.Helper()
		child, ok := byName[name]
		if ok == false {
			t.Fatalf("no child named %q in the spec", name)
		}
		if len(child.Args) < want {
			t.Fatalf("child %q got %d args, want at least %d", name, len(child.Args), want)
		}
		return child.Args
	}

	mux := env["mux"].(*http.ServeMux)
	shared := env["shared"].(*metrics.Shared)

	ho, ok := argsOf(nameHealth, 1)[0].(health.Options)
	if ok == false {
		t.Fatalf("health child got %T", argsOf(nameHealth, 1)[0])
	}
	if ho.Path != options.HealthPath {
		t.Errorf("health serves %q, want %q", ho.Path, options.HealthPath)
	}
	if ho.CheckInterval != options.HealthCheckInterval {
		t.Errorf("health checks every %v, want %v", ho.CheckInterval, options.HealthCheckInterval)
	}
	if ho.Mux != mux {
		t.Error("health got a different mux than the one in the env")
	}

	mo, ok := argsOf(nameMetricsErgo, 1)[0].(metrics.Options)
	if ok == false {
		t.Fatalf("metrics child got %T", argsOf(nameMetricsErgo, 1)[0])
	}
	if mo.Path != options.MetricsPath {
		t.Errorf("metrics serves %q, want %q", mo.Path, options.MetricsPath)
	}
	if mo.CollectInterval != options.MetricsCollectInterval {
		t.Errorf("metrics collects every %v, want %v", mo.CollectInterval, options.MetricsCollectInterval)
	}
	if mo.TopN != options.MetricsTopN {
		t.Errorf("metrics keeps top %d, want %d", mo.TopN, options.MetricsTopN)
	}
	if mo.Shared != shared || mo.Mux != mux {
		t.Error("metrics got a different shared registry or mux than the env's")
	}

	po, ok := argsOf(nameMetrics, 1)[0].(act.PoolOptions)
	if ok == false {
		t.Fatalf("pool child got %T", argsOf(nameMetrics, 1)[0])
	}
	if po.PoolSize != options.MetricsPoolSize {
		t.Errorf("pool size %d, want %d", po.PoolSize, options.MetricsPoolSize)
	}
	wo, ok := po.WorkerArgs[0].(metrics.Options)
	if ok == false {
		t.Fatalf("pool worker got %T", po.WorkerArgs[0])
	}
	if wo.Shared != shared {
		t.Error("pool workers write to a different shared registry")
	}
	if wo.Mux != nil || wo.Path != "" {
		t.Error("a pool worker was given an HTTP surface of its own")
	}

	topn := argsOf(nameTopNSup, 2)
	if topn[0] != any(shared) {
		t.Error("topN supervisor got a different shared registry")
	}
	// The interval every top-N actor flushes at travels through here. Left at
	// zero it used to reach the actor unchanged.
	if topn[1] != any(options.MetricsCollectInterval) {
		t.Errorf("topN flush interval %v, want %v", topn[1], options.MetricsCollectInterval)
	}

	web := argsOf(nameWeb, 3)
	if web[0] != any(mux) {
		t.Error("web serves a different mux than the one the actors registered on")
	}
	if web[1] != any(options.Host) || web[2] != any(options.Port) {
		t.Errorf("web listens on %v:%v, want %v:%v", web[1], web[2], options.Host, options.Port)
	}
}
