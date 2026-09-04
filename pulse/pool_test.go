package pulse

import (
	"errors"
	"testing"

	"ergo.services/ergo/gen"
	"ergo.services/ergo/testing/unit"
)

func TestPoolRegistersItselfAsTracingExporter(t *testing.T) {
	node := unit.StartNode(t, "pulse@localhost", gen.NodeOptions{})

	var gotName string
	var gotFlags gen.TracingFlags
	var gotPID gen.PID
	node.OnTracingExporterAddPID(func(pid gen.PID, name string, flags gen.TracingFlags) error {
		gotPID, gotName, gotFlags = pid, name, flags
		return nil
	})

	options := applyDefaults(Options{Flags: gen.TracingFlagSend})
	sub, err := node.Spawn(factoryPool, gen.ProcessOptions{}, options)
	if err != nil {
		t.Fatalf("spawn: %s", err)
	}

	if gotName != string(Name) {
		t.Errorf("registered under %q, want %q", gotName, Name)
	}
	if gotFlags != gen.TracingFlagSend {
		t.Errorf("registered with flags %v, want the configured %v", gotFlags, gen.TracingFlagSend)
	}
	if gotPID != sub.PID() {
		t.Errorf("registered PID %s, want the pool's own %s", gotPID, sub.PID())
	}
}

func TestPoolRefusesToStartWhenItCannotRegister(t *testing.T) {
	node := unit.StartNode(t, "pulse@localhost", gen.NodeOptions{})
	node.OnTracingExporterAddPID(func(gen.PID, string, gen.TracingFlags) error {
		return errors.New("taken")
	})

	_, err := node.Spawn(factoryPool, gen.ProcessOptions{}, applyDefaults(Options{}))
	if err == nil {
		t.Fatal("the pool started although it never became the exporter, so spans would go nowhere")
	}
}

func TestPoolUnregistersOnTerminate(t *testing.T) {
	node := unit.StartNode(t, "pulse@localhost", gen.NodeOptions{})
	node.OnTracingExporterAddPID(func(gen.PID, string, gen.TracingFlags) error { return nil })

	deleted := gen.PID{}
	node.OnTracingExporterDeletePID(func(pid gen.PID) { deleted = pid })

	sub, err := node.Spawn(factoryPool, gen.ProcessOptions{}, applyDefaults(Options{}))
	if err != nil {
		t.Fatalf("spawn: %s", err)
	}
	sub.DeliverExit(sub.PID(), gen.TerminateReasonShutdown)

	if deleted != sub.PID() {
		t.Fatalf("the exporter registration outlived the pool: deleted %s, pool was %s", deleted, sub.PID())
	}
}
