package store

import "testing"

func TestIsDead(t *testing.T) {
	const ttl = 10
	base := Stamp{Key: "k", OwnerNode: "a@h", Incarnation: 5, Status: StatusRunning, Epoch: 1}
	fresh := Lease{Node: "a@h", Incarnation: 5, LastHeartbeat: 1000, TTL: ttl}

	tests := []struct {
		name      string
		stamp     Stamp
		lease     Lease
		haveLease bool
		now       int64
		want      bool
	}{
		{"released-clean", withStatus(base, StatusReleasedClean), fresh, true, 1000, true},
		{"released-deleted", withStatus(base, StatusReleasedDeleted), fresh, true, 1000, true},
		{"crashed", withStatus(base, StatusCrashed), fresh, true, 1000, true},
		{"no-lease", base, Lease{}, false, 1000, true},
		{"owner-bounced", base, withInc(fresh, 6), true, 1000, true},
		{"stale-view", base, withInc(fresh, 4), true, 1000, false},
		{"same-inc-fresh", base, fresh, true, 1005, false},
		{"same-inc-at-ttl", base, fresh, true, 1010, true},
		{"same-inc-expired", base, fresh, true, 1011, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsDead(tc.stamp, tc.lease, tc.haveLease, tc.now); got != tc.want {
				t.Fatalf("IsDead = %v, want %v", got, tc.want)
			}
		})
	}
}

func withStatus(s Stamp, st Status) Stamp  { s.Status = st; return s }
func withInc(l Lease, i Incarnation) Lease { l.Incarnation = i; return l }
