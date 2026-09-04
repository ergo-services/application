package radar

import (
	"testing"
	"time"

	"ergo.services/actor/health"
	"ergo.services/actor/metrics"

	"ergo.services/ergo/act"
	"ergo.services/ergo/gen"
	"ergo.services/ergo/testing/check"
	"ergo.services/ergo/testing/unit"
)

// helperProbe lends a real gen.Process to a package helper: the helpers take
// the caller's process and address a radar child by name, and that name is the
// only thing tying them to the children the supervisor started.
type helperProbe struct {
	act.Actor
	call func(p gen.Process) error
	err  error
}

func (h *helperProbe) HandleMessage(from gen.PID, message any) error {
	h.err = h.call(h)
	return nil
}

func TestHelpersAddressTheChildrenTheSupervisorStarts(t *testing.T) {
	const topN = "queue_depth"

	for _, tc := range []struct {
		name   string
		target gen.Atom
		// answer is the reply the addressed child would give; non-nil marks the
		// helpers that wait for one rather than firing and forgetting.
		answer any
		fn     func(p gen.Process) error
	}{
		{"RegisterService", nameHealth, health.RegisterResponse{}, func(p gen.Process) error {
			return RegisterService(p, "db", ProbeLiveness, time.Second)
		}},
		{"UnregisterService", nameHealth, health.UnregisterResponse{}, func(p gen.Process) error {
			return UnregisterService(p, "db")
		}},
		{"Heartbeat", nameHealth, nil, func(p gen.Process) error {
			return Heartbeat(p, "db")
		}},
		{"ServiceUp", nameHealth, nil, func(p gen.Process) error {
			return ServiceUp(p, "db")
		}},
		{"ServiceDown", nameHealth, nil, func(p gen.Process) error {
			return ServiceDown(p, "db")
		}},
		{"RegisterGauge", nameMetrics, metrics.RegisterResponse{}, func(p gen.Process) error {
			return RegisterGauge(p, "g", "help", []string{"pid"})
		}},
		{"RegisterCounter", nameMetrics, metrics.RegisterResponse{}, func(p gen.Process) error {
			return RegisterCounter(p, "c", "help", []string{"pid"})
		}},
		{"RegisterHistogram", nameMetrics, metrics.RegisterResponse{}, func(p gen.Process) error {
			return RegisterHistogram(p, "h", "help", []string{"pid"}, []float64{1})
		}},
		{"UnregisterMetric", nameMetrics, nil, func(p gen.Process) error {
			return UnregisterMetric(p, "g")
		}},
		{"GaugeSet", nameMetrics, nil, func(p gen.Process) error {
			return GaugeSet(p, "g", 1, []string{"pid"})
		}},
		{"GaugeAdd", nameMetrics, nil, func(p gen.Process) error {
			return GaugeAdd(p, "g", 1, []string{"pid"})
		}},
		{"CounterAdd", nameMetrics, nil, func(p gen.Process) error {
			return CounterAdd(p, "c", 1, []string{"pid"})
		}},
		{"HistogramObserve", nameMetrics, nil, func(p gen.Process) error {
			return HistogramObserve(p, "h", 1, []string{"pid"})
		}},
		{"RegisterTopN", nameTopNSup, metrics.RegisterResponse{}, func(p gen.Process) error {
			return RegisterTopN(p, topN, "help", 5, TopNMax, []string{"pid"})
		}},
		{"TopNObserve", gen.Atom("radar_topn_" + topN), nil, func(p gen.Process) error {
			return TopNObserve(p, topN, 1, []string{"pid"})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			probe := &helperProbe{call: tc.fn}
			sub, err := unit.Spawn(t,
				func() gen.ProcessBehavior { return probe }, gen.ProcessOptions{})
			if err != nil {
				t.Fatalf("spawn: %s", err)
			}

			if tc.answer != nil {
				sub.OnCall(tc.target).Respond(tc.answer)
			}

			sub.SendMessage(gen.PID{Node: "test@localhost", ID: 1, Creation: 1}, "go")

			check.NoError(t, probe.err)
			if tc.answer != nil {
				sub.ShouldCall().To(tc.target).Once().Assert()
				sub.ShouldSend().None().Assert()
				return
			}
			sub.ShouldSend().To(tc.target).Once().Assert()
			sub.ShouldCall().None().Assert()
		})
	}
}
