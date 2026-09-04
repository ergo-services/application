package pulse

import (
	"testing"

	"ergo.services/ergo/gen"
)

func TestCreateAppLoadsASpecTheNodeCanStart(t *testing.T) {
	behavior := CreateApp(Options{})

	spec, err := behavior.Load()
	if err != nil {
		t.Fatalf("Load: %s", err)
	}

	if spec.Name != Name {
		t.Errorf("Name = %q, want %q", spec.Name, Name)
	}
	if spec.Mode != gen.ApplicationModePermanent {
		t.Errorf("Mode = %s, want permanent: the exporter is not optional once registered", spec.Mode)
	}
	if spec.Version.Release != Version {
		t.Errorf("Version = %q, want %q", spec.Version.Release, Version)
	}
	if len(spec.Group) != 1 {
		t.Fatalf("Group has %d members, want the pool alone", len(spec.Group))
	}
	if spec.Group[0].Name != poolName {
		t.Errorf("member name = %q, want %q", spec.Group[0].Name, poolName)
	}
}

func TestCreateAppAppliesDefaultsBeforeTheyReachThePool(t *testing.T) {
	spec, err := CreateApp(Options{}).Load()
	if err != nil {
		t.Fatalf("Load: %s", err)
	}

	options, ok := spec.Group[0].Args[0].(Options)
	if ok == false {
		t.Fatalf("the pool is handed %T, not Options", spec.Group[0].Args[0])
	}
	if options.URL != DefaultURL || options.BatchSize != DefaultBatchSize {
		t.Fatalf("the pool is handed an unfilled config: %+v", options)
	}
}
