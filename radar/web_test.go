package radar

import (
	"net/http"
	"testing"

	"ergo.services/ergo/gen"
	"ergo.services/ergo/testing/unit"
)

func TestWebInitRejectsBadArgs(t *testing.T) {
	mux := http.NewServeMux()
	for _, tc := range []struct {
		name string
		args []any
	}{
		{"no args", nil},
		{"too few args", []any{mux, "localhost"}},
		{"mux of another type", []any{"not a mux", "localhost", uint16(0)}},
		{"host of another type", []any{mux, 8080, uint16(0)}},
		{"port of another type", []any{mux, "localhost", 8080}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := unit.Spawn(t, factoryWeb, gen.ProcessOptions{}, tc.args...); err == nil {
				t.Fatal("the web actor started with bad args")
			}
		})
	}
}

// The single HTTP listener is what makes both endpoints reachable, and it is a
// meta process rather than actor code because serving is a stream.
func TestWebSpawnsTheServerAsMeta(t *testing.T) {
	sub, err := unit.Spawn(t, factoryWeb, gen.ProcessOptions{},
		http.NewServeMux(), "localhost", uint16(0))
	if err != nil {
		t.Fatalf("Init: %s", err)
	}

	sub.ShouldSpawnMeta().Once().Assert()
	if sub.Terminated() {
		t.Error("the web actor terminated right after starting its server")
	}
}
