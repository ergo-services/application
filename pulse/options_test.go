package pulse

import (
	"testing"
	"time"

	"ergo.services/ergo/gen"
)

func TestApplyDefaultsFillsAnEmptyConfig(t *testing.T) {
	o := applyDefaults(Options{})

	if o.URL != DefaultURL {
		t.Errorf("URL = %q, want %q", o.URL, DefaultURL)
	}
	if o.BatchSize != DefaultBatchSize {
		t.Errorf("BatchSize = %d, want %d", o.BatchSize, DefaultBatchSize)
	}
	if o.FlushInterval != DefaultFlushInterval {
		t.Errorf("FlushInterval = %s, want %s", o.FlushInterval, DefaultFlushInterval)
	}
	if o.PoolSize != DefaultPoolSize {
		t.Errorf("PoolSize = %d, want %d", o.PoolSize, DefaultPoolSize)
	}
	if o.ExportTimeout != DefaultExportTimeout {
		t.Errorf("ExportTimeout = %s, want %s", o.ExportTimeout, DefaultExportTimeout)
	}
	want := gen.TracingFlagSend | gen.TracingFlagReceive | gen.TracingFlagProcs
	if o.Flags != want {
		t.Errorf("Flags = %v, want %v", o.Flags, want)
	}
}

func TestApplyDefaultsKeepsWhatWasSet(t *testing.T) {
	in := Options{
		URL:           "http://tempo:4318/v1/traces",
		BatchSize:     8,
		FlushInterval: time.Second,
		PoolSize:      1,
		ExportTimeout: 2 * time.Second,
		Flags:         gen.TracingFlagSend,
		Headers:       map[string]string{"authorization": "Bearer x"},
	}

	o := applyDefaults(in)

	if o.URL != in.URL || o.BatchSize != in.BatchSize || o.FlushInterval != in.FlushInterval ||
		o.PoolSize != in.PoolSize || o.ExportTimeout != in.ExportTimeout || o.Flags != in.Flags {
		t.Fatalf("applyDefaults overwrote a configured value: %+v", o)
	}
	if o.Headers["authorization"] != "Bearer x" {
		t.Errorf("Headers were dropped: %v", o.Headers)
	}
}

func TestApplyDefaultsRejectsNonsenseSizes(t *testing.T) {
	o := applyDefaults(Options{BatchSize: -1, PoolSize: 0, FlushInterval: -time.Second, ExportTimeout: 0})

	if o.BatchSize != DefaultBatchSize {
		t.Errorf("a negative BatchSize survived as %d", o.BatchSize)
	}
	if o.PoolSize != DefaultPoolSize {
		t.Errorf("a zero PoolSize survived as %d", o.PoolSize)
	}
	if o.FlushInterval != DefaultFlushInterval {
		t.Errorf("a negative FlushInterval survived as %s", o.FlushInterval)
	}
	if o.ExportTimeout != DefaultExportTimeout {
		t.Errorf("a zero ExportTimeout survived as %s", o.ExportTimeout)
	}
}
