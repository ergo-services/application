package observer

import (
	"math"
	"testing"
)

// an unset GOMEMLIMIT is dropped, not sent as MaxInt64
func TestWireClusterDropsAbsentMemoryLimit(t *testing.T) {
	if limit := memoryLimit(math.MaxInt64); limit != 0 {
		t.Errorf("an unset GOMEMLIMIT crossed the wire as %d, expected it dropped", limit)
	}
	if limit := memoryLimit(512 << 20); limit != 512<<20 {
		t.Errorf("a real GOMEMLIMIT came out as %d", limit)
	}
	if limit := memoryLimit(0); limit != 0 {
		t.Errorf("zero came out as %d", limit)
	}
}
