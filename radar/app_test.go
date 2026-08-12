package radar

// Both actors radar hosts refuse to start unless their wire types are on the node, and this
// application spec is the only thing that registers them - ApplicationLoad runs before any
// of its processes spawn, which is why it belongs here and not in a child's Init. Forgetting
// one of the two would leave that actor dead on arrival with nothing to say at startup.

import (
	"reflect"
	"testing"

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
