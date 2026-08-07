package grain

import (
	"testing"
	"time"
)

func TestApplyDefaults(t *testing.T) {
	o := applyDefaults(Options{})
	if o.Domain != DefaultDomain {
		t.Fatalf("Domain: got %q, want %q", o.Domain, DefaultDomain)
	}
	if o.Activators != DefaultActivators {
		t.Fatalf("Activators: got %d, want %d", o.Activators, DefaultActivators)
	}
	if o.LeaseTTL != DefaultLeaseTTL {
		t.Fatalf("LeaseTTL: got %d, want %d", o.LeaseTTL, DefaultLeaseTTL)
	}
	if o.StoreIOTimeout != DefaultStoreIOTimeout {
		t.Fatalf("StoreIOTimeout: got %v", o.StoreIOTimeout)
	}
	if o.ActivateSecs != DefaultActivateSecs {
		t.Fatalf("ActivateSecs: got %d", o.ActivateSecs)
	}
}

// A RenewInterval that, with its IO latency, would let two beats span more than
// half the lease is clamped down.
func TestApplyDefaultsClampsRenewInterval(t *testing.T) {
	o := applyDefaults(Options{LeaseTTL: 10, RenewInterval: 9 * time.Second, StoreIOTimeout: 1 * time.Second})
	half := 5 * time.Second
	if o.RenewInterval+o.StoreIOTimeout > half {
		t.Fatalf("renew spacing not clamped: renew=%v io=%v half=%v", o.RenewInterval, o.StoreIOTimeout, half)
	}
}

func TestHashKeyInRange(t *testing.T) {
	for _, key := range []string{"a", "order/42", "", "long/key/with/segments"} {
		for _, n := range []int{1, 8, 64} {
			if got := hashKey(key, n); got < 0 || got >= n {
				t.Fatalf("hashKey(%q,%d)=%d out of range", key, n, got)
			}
		}
	}
}

func TestHashKeyDeterministic(t *testing.T) {
	if hashKey("order/42", 8) != hashKey("order/42", 8) {
		t.Fatal("hashKey not deterministic")
	}
}
