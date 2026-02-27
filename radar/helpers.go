package radar

import (
	"time"

	"ergo.services/actor/health"
	"ergo.services/actor/metrics"
	"ergo.services/ergo/gen"
)

// Health helpers -- delegate to health actor named "radar_health".

// RegisterService registers a service signal with the health actor.
func RegisterService(process gen.Process, signal gen.Atom, probe Probe, timeout time.Duration) error {
	return health.Register(process, nameHealth, signal, probe, timeout)
}

// UnregisterService removes a service signal from the health actor.
func UnregisterService(process gen.Process, signal gen.Atom) error {
	return health.Unregister(process, nameHealth, signal)
}

// Heartbeat sends a heartbeat for the given signal to the health actor.
func Heartbeat(process gen.Process, signal gen.Atom) error {
	return health.Heartbeat(process, nameHealth, signal)
}

// ServiceUp marks a signal as up (healthy).
func ServiceUp(process gen.Process, signal gen.Atom) error {
	return health.SignalUp(process, nameHealth, signal)
}

// ServiceDown marks a signal as down (unhealthy).
func ServiceDown(process gen.Process, signal gen.Atom) error {
	return health.SignalDown(process, nameHealth, signal)
}

// Metrics helpers -- delegate to metrics pool named "radar_metrics".

// RegisterGauge registers a gauge metric via the metrics pool.
func RegisterGauge(process gen.Process, name, help string, labels []string) error {
	return metrics.RegisterGauge(process, nameMetrics, name, help, labels)
}

// RegisterCounter registers a counter metric via the metrics pool.
func RegisterCounter(process gen.Process, name, help string, labels []string) error {
	return metrics.RegisterCounter(process, nameMetrics, name, help, labels)
}

// RegisterHistogram registers a histogram metric via the metrics pool.
func RegisterHistogram(process gen.Process, name, help string, labels []string, buckets []float64) error {
	return metrics.RegisterHistogram(process, nameMetrics, name, help, labels, buckets)
}

// UnregisterMetric removes a previously registered custom metric.
func UnregisterMetric(process gen.Process, name string) error {
	return metrics.Unregister(process, nameMetrics, name)
}

// GaugeSet sets the value of a registered gauge metric.
func GaugeSet(process gen.Process, name string, value float64, labels []string) error {
	return metrics.GaugeSet(process, nameMetrics, name, value, labels)
}

// GaugeAdd adds the value to a registered gauge metric.
func GaugeAdd(process gen.Process, name string, value float64, labels []string) error {
	return metrics.GaugeAdd(process, nameMetrics, name, value, labels)
}

// CounterAdd adds the value to a registered counter metric.
func CounterAdd(process gen.Process, name string, value float64, labels []string) error {
	return metrics.CounterAdd(process, nameMetrics, name, value, labels)
}

// HistogramObserve observes a value on a registered histogram metric.
func HistogramObserve(process gen.Process, name string, value float64, labels []string) error {
	return metrics.HistogramObserve(process, nameMetrics, name, value, labels)
}
