package radar

// Both actors radar hosts refuse to start unless their wire types are on the node, and this
// application spec is the only thing that registers them - ApplicationLoad runs before any
// of its processes spawn, which is why it belongs here and not in a child's Init. Forgetting
// one of the two would leave that actor dead on arrival with nothing to say at startup.

import (
	"net/http"
	"reflect"
	"testing"
	"time"

	"ergo.services/actor/health"
	"ergo.services/actor/metrics"
)

func TestLoad_RegistersBothActorsWireTypes(t *testing.T) {
	spec, err := CreateApp(Options{}).(*radarApp).Load()
	if err != nil {
		t.Fatalf("Load: %s", err)
	}

	registered := make(map[reflect.Type]bool)
	for _, v := range spec.Network.RegisterTypes {
		registered[reflect.TypeOf(v)] = true
	}

	required := append(health.NetworkTypes(), metrics.NetworkTypes()...)
	if len(required) == 0 {
		t.Fatal("neither actor declares wire types - this test would pass vacuously")
	}
	for _, want := range required {
		if registered[reflect.TypeOf(want)] == false {
			t.Errorf("spec.Network.RegisterTypes is missing %T", want)
		}
	}
}

// The supervisor reads all three of these out of the env and refuses to start
// without any one of them, so the spec is where a missing entry has to be caught.
func TestLoad_SeedsTheEnvItsSupervisorReads(t *testing.T) {
	options := Options{Host: "0.0.0.0", Port: 9999}
	spec, err := CreateApp(options).(*radarApp).Load()
	if err != nil {
		t.Fatalf("Load: %s", err)
	}

	if _, ok := spec.Env["mux"].(*http.ServeMux); ok == false {
		t.Errorf("env 'mux' is %T", spec.Env["mux"])
	}
	if _, ok := spec.Env["shared"].(*metrics.Shared); ok == false {
		t.Errorf("env 'shared' is %T", spec.Env["shared"])
	}
	seeded, ok := spec.Env["options"].(Options)
	if ok == false {
		t.Fatalf("env 'options' is %T", spec.Env["options"])
	}
	// The normalized options, not the caller's - otherwise every default
	// CreateApp filled in would be discarded on the way to the children.
	if seeded.Host != options.Host || seeded.Port != options.Port {
		t.Error("the options in the env are not the ones CreateApp was given")
	}
	if seeded.MetricsCollectInterval == 0 {
		t.Error("the env carries the un-normalized options")
	}

	if len(spec.Group) != 1 || spec.Group[0].Name != nameSup {
		t.Errorf("group is %v, want the one supervisor %q", spec.Group, nameSup)
	}
}

// Every option with a documented default has to be non-zero after CreateApp,
// and has to agree with the actor that consumes it - radar inventing its own
// collection interval while metrics defaults to another is a drift no test of
// either package alone would catch.
func TestCreateApp_FillsEveryDocumentedDefault(t *testing.T) {
	filled := CreateApp(Options{}).(*radarApp).options

	if filled.Host == "" || filled.Port == 0 {
		t.Errorf("host/port left at %q:%d", filled.Host, filled.Port)
	}
	if filled.HealthPath == "" || filled.MetricsPath == "" {
		t.Errorf("paths left at %q and %q", filled.HealthPath, filled.MetricsPath)
	}
	if filled.MetricsPoolSize < 1 {
		t.Errorf("pool size left at %d", filled.MetricsPoolSize)
	}
	if filled.MetricsCollectInterval != metrics.DefaultCollectInterval {
		t.Errorf("collect interval is %v, but the metrics actor defaults to %v",
			filled.MetricsCollectInterval, metrics.DefaultCollectInterval)
	}
	if filled.MetricsTopN != metrics.DefaultTopN {
		t.Errorf("topN is %d, but the metrics actor defaults to %d",
			filled.MetricsTopN, metrics.DefaultTopN)
	}
}

func TestCreateApp_KeepsWhatTheCallerSet(t *testing.T) {
	given := Options{
		Host: "0.0.0.0", Port: 1,
		HealthPath: "/hp", MetricsPath: "/mp",
		HealthCheckInterval:    2 * time.Second,
		MetricsCollectInterval: 3 * time.Second,
		MetricsTopN:            7,
		MetricsPoolSize:        9,
	}
	if kept := CreateApp(given).(*radarApp).options; kept != given {
		t.Errorf("CreateApp changed the options it was given:\n got %+v\nwant %+v", kept, given)
	}
}
