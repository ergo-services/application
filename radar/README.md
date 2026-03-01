# Radar Application

Doc: https://docs.ergo.services/extra-library/applications/radar

Running a production Ergo node typically requires two things: health probes for Kubernetes (liveness, readiness, startup) and a Prometheus metrics endpoint. Setting these up separately means configuring two HTTP servers, managing two sets of actors, and importing two packages in every actor that needs to report its status or update a metric.

Radar solves this by providing a single application that exposes both health probes and Prometheus metrics on one HTTP port. It uses `actor/health` and `actor/metrics` internally, but actors on the node interact with it through helper functions in the `radar` package -- no need to import the underlying packages or know the internal actor names.

## Installation

```bash
go get ergo.services/application/radar
```

## Starting

Load and start the Radar application on your node:

```go
import (
    "ergo.services/application/radar"
    "ergo.services/ergo"
    "ergo.services/ergo/gen"
)

func main() {
    node, _ := ergo.StartNode("mynode@localhost", gen.NodeOptions{
        Applications: []gen.ApplicationBehavior{
            radar.CreateApp(radar.Options{Port: 9090}),
        },
    })

    // Health:  http://localhost:9090/health/live
    //          http://localhost:9090/health/ready
    //          http://localhost:9090/health/startup
    // Metrics: http://localhost:9090/metrics

    node.Wait()
}
```

Radar automatically collects base Ergo metrics (processes, memory, CPU, network, events) and serves them at the metrics endpoint. Health endpoints return healthy by default until actors register their signals.

## Using from Actors

### Health

Register your service for health probes, send heartbeats, and control signal state:

```go
func (w *MyActor) Init(args ...any) error {
    // Register for liveness and readiness probes with 10s heartbeat timeout
    radar.RegisterService(w, "database", radar.ProbeLiveness|radar.ProbeReadiness, 10*time.Second)

    // Start periodic heartbeat
    w.cancelHeartbeat, _ = w.SendAfter(w.PID(), messageHeartbeat{}, 3*time.Second)
    return nil
}

func (w *MyActor) HandleMessage(from gen.PID, message any) error {
    switch message.(type) {
    case messageHeartbeat:
        radar.Heartbeat(w, "database")
        w.cancelHeartbeat, _ = w.SendAfter(w.PID(), messageHeartbeat{}, 3*time.Second)
    }
    return nil
}
```

If the heartbeat is not received within the timeout, the signal is marked as down and the health endpoint returns 503.

Available health functions:

```go
// Registration (sync Call, returns error)
radar.RegisterService(process, signal, probe, timeout)
radar.UnregisterService(process, signal)

// Status updates (async Send)
radar.Heartbeat(process, signal)
radar.ServiceUp(process, signal)
radar.ServiceDown(process, signal)
```

Registration and unregistration are synchronous -- the call blocks until the health actor confirms the operation. This prevents race conditions where a heartbeat arrives before the signal is registered.

Probe constants: `radar.ProbeLiveness`, `radar.ProbeReadiness`, `radar.ProbeStartup`. Combine with bitwise OR.

### Metrics

Register custom metrics and update them:

```go
func (w *MyActor) Init(args ...any) error {
    radar.RegisterGauge(w, "db_connections", "Active database connections", []string{"pool"})
    radar.RegisterCounter(w, "cache_ops_total", "Cache operations", []string{"op"})
    radar.RegisterHistogram(w, "request_duration_seconds", "Request latency",
        []string{"method"}, []float64{0.01, 0.05, 0.1, 0.5, 1.0})
    return nil
}

func (w *MyActor) HandleMessage(from gen.PID, message any) error {
    switch message.(type) {
    case messageUpdate:
        radar.GaugeSet(w, "db_connections", 42, []string{"primary"})
        radar.CounterAdd(w, "cache_ops_total", 1, []string{"hit"})
        radar.HistogramObserve(w, "request_duration_seconds", 0.023, []string{"GET"})
    }
    return nil
}
```

Registration is synchronous (returns error on failure). Updates are asynchronous (fire-and-forget). When the registering process terminates, its metrics are automatically unregistered.

Available metrics functions:

```go
// Registration (sync)
radar.RegisterGauge(process, name, help, labels)
radar.RegisterCounter(process, name, help, labels)
radar.RegisterHistogram(process, name, help, labels, buckets)
radar.UnregisterMetric(process, name)

// Updates (async)
radar.GaugeSet(process, name, value, labels)
radar.GaugeAdd(process, name, value, labels)
radar.CounterAdd(process, name, value, labels)
radar.HistogramObserve(process, name, value, labels)
```

## Options

| Option | Default | Description |
|--------|---------|-------------|
| Host | "localhost" | HTTP server listen address |
| Port | 9090 | HTTP server port |
| HealthPath | "/health" | URL prefix for health endpoints |
| MetricsPath | "/metrics" | URL path for Prometheus metrics |
| HealthCheckInterval | 1s | Heartbeat timeout check frequency |
| MetricsCollectInterval | 10s | Base metrics collection frequency |
| MetricsTopN | 50 | Top-N entries in process/event metrics |
| MetricsPoolSize | 3 | Number of workers handling custom metrics |

## Kubernetes

```yaml
livenessProbe:
  httpGet:
    path: /health/live
    port: 9090
  periodSeconds: 10
readinessProbe:
  httpGet:
    path: /health/ready
    port: 9090
  periodSeconds: 10
startupProbe:
  httpGet:
    path: /health/startup
    port: 9090
  failureThreshold: 30
  periodSeconds: 2
```

Prometheus scrape config:

```yaml
scrape_configs:
  - job_name: 'ergo'
    static_configs:
      - targets: ['localhost:9090']
```

## License

See LICENSE file in the repository root.
