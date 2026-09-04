package pulse

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"ergo.services/ergo/gen"
	"ergo.services/ergo/testing/unit"
	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/protobuf/proto"
)

type collector struct {
	server *httptest.Server

	mu       sync.Mutex
	requests []*coltracepb.ExportTraceServiceRequest
	headers  []http.Header
	status   int
}

func startCollector(t *testing.T) *collector {
	t.Helper()

	c := &collector{status: http.StatusOK}
	c.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		req := &coltracepb.ExportTraceServiceRequest{}
		if err := proto.Unmarshal(body, req); err != nil {
			t.Errorf("the exporter posted something that is not an OTLP request: %s", err)
		}

		c.mu.Lock()
		c.requests = append(c.requests, req)
		c.headers = append(c.headers, r.Header.Clone())
		status := c.status
		c.mu.Unlock()

		w.WriteHeader(status)
	}))
	t.Cleanup(c.server.Close)
	return c
}

func (c *collector) setStatus(status int) {
	c.mu.Lock()
	c.status = status
	c.mu.Unlock()
}

func (c *collector) exported() []*coltracepb.ExportTraceServiceRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]*coltracepb.ExportTraceServiceRequest(nil), c.requests...)
}

func (c *collector) spanCount() int {
	total := 0
	for _, req := range c.exported() {
		for _, rs := range req.ResourceSpans {
			for _, ss := range rs.ScopeSpans {
				total += len(ss.Spans)
			}
		}
	}
	return total
}

func spawnWorker(t *testing.T, c *collector, o Options) *unit.Subject {
	t.Helper()

	o.URL = c.server.URL
	sub, err := unit.Spawn(t, factoryWorker, gen.ProcessOptions{}, applyDefaults(o))
	if err != nil {
		t.Fatalf("spawn: %s", err)
	}
	return sub
}

func TestWorkerArmsItsFlushTimerOnInit(t *testing.T) {
	c := startCollector(t)
	sub := spawnWorker(t, c, Options{FlushInterval: time.Second})

	sub.ShouldSendAfter().Message(messageFlushTimer{}).Once().Assert()
}

func TestWorkerHoldsSpansUntilTheBatchIsFull(t *testing.T) {
	c := startCollector(t)
	sub := spawnWorker(t, c, Options{BatchSize: 3})

	sub.DeliverSpan(gen.TracingSpan{SpanID: 1})
	sub.DeliverSpan(gen.TracingSpan{SpanID: 2})

	if n := c.spanCount(); n != 0 {
		t.Fatalf("%d spans left before the batch was full", n)
	}
}

func TestWorkerExportsWhenTheBatchFills(t *testing.T) {
	c := startCollector(t)
	sub := spawnWorker(t, c, Options{BatchSize: 3})

	sub.DeliverSpan(gen.TracingSpan{SpanID: 1})
	sub.DeliverSpan(gen.TracingSpan{SpanID: 2})
	sub.DeliverSpan(gen.TracingSpan{SpanID: 3})

	if n := c.spanCount(); n != 3 {
		t.Fatalf("exported %d spans, want the 3 that filled the batch", n)
	}
}

func TestWorkerStartsANewBatchAfterExporting(t *testing.T) {
	c := startCollector(t)
	sub := spawnWorker(t, c, Options{BatchSize: 2})

	for i := 0; i < 4; i++ {
		sub.DeliverSpan(gen.TracingSpan{SpanID: uint64(i)})
	}

	if n := len(c.exported()); n != 2 {
		t.Fatalf("%d exports for 4 spans at batch size 2, want 2", n)
	}
	if n := c.spanCount(); n != 4 {
		t.Fatalf("exported %d spans, want 4", n)
	}
}

func TestWorkerExportsOnTheFlushTimer(t *testing.T) {
	c := startCollector(t)
	sub := spawnWorker(t, c, Options{BatchSize: 1000, FlushInterval: time.Millisecond})

	sub.DeliverSpan(gen.TracingSpan{SpanID: 1})
	sub.FireTimers()

	if n := c.spanCount(); n != 1 {
		t.Fatalf("exported %d spans on the timer, want the 1 waiting", n)
	}
}

func TestWorkerRearmsTheTimerAfterItFires(t *testing.T) {
	c := startCollector(t)
	sub := spawnWorker(t, c, Options{FlushInterval: time.Millisecond})

	sub.FireTimers()

	sub.ShouldSendAfter().Message(messageFlushTimer{}).Times(2).Assert()
}

func TestWorkerPostsNothingOnAnEmptyTimerTick(t *testing.T) {
	c := startCollector(t)
	sub := spawnWorker(t, c, Options{FlushInterval: time.Millisecond})

	sub.FireTimers()

	if n := len(c.exported()); n != 0 {
		t.Fatalf("%d exports with nothing batched", n)
	}
}

func TestWorkerFlushesWhatIsLeftOnTerminate(t *testing.T) {
	c := startCollector(t)
	sub := spawnWorker(t, c, Options{BatchSize: 1000})

	sub.DeliverSpan(gen.TracingSpan{SpanID: 1})
	sub.DeliverExit(sub.PID(), gen.TerminateReasonShutdown)

	if n := c.spanCount(); n != 1 {
		t.Fatalf("exported %d spans on shutdown, want the 1 still batched", n)
	}
}

func TestWorkerCarriesTheConfiguredHeaders(t *testing.T) {
	c := startCollector(t)
	sub := spawnWorker(t, c, Options{
		BatchSize: 1,
		Headers:   map[string]string{"Authorization": "Bearer token"},
	})

	sub.DeliverSpan(gen.TracingSpan{SpanID: 1})

	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.headers) != 1 {
		t.Fatalf("%d requests reached the collector", len(c.headers))
	}
	if got := c.headers[0].Get("Authorization"); got != "Bearer token" {
		t.Errorf("Authorization = %q, want the configured token", got)
	}
	if got := c.headers[0].Get("Content-Type"); got != "application/x-protobuf" {
		t.Errorf("Content-Type = %q, want application/x-protobuf", got)
	}
}

func TestWorkerNamesTheNodeInEveryExport(t *testing.T) {
	c := startCollector(t)
	node := unit.StartNode(t, "pulse@localhost", gen.NodeOptions{})
	sub, err := node.Spawn(factoryWorker, gen.ProcessOptions{}, applyDefaults(Options{BatchSize: 1, URL: c.server.URL}))
	if err != nil {
		t.Fatalf("spawn: %s", err)
	}

	sub.DeliverSpan(gen.TracingSpan{SpanID: 1})

	exported := c.exported()
	if len(exported) != 1 {
		t.Fatalf("%d exports reached the collector", len(exported))
	}
	var service string
	for _, a := range exported[0].ResourceSpans[0].Resource.Attributes {
		if a.Key == "service.name" {
			service = a.Value.GetStringValue()
		}
	}
	if service != "pulse@localhost" {
		t.Fatalf("service.name = %q, want the node name", service)
	}
}

func TestWorkerDropsTheBatchWhenTheCollectorRejectsIt(t *testing.T) {
	c := startCollector(t)
	c.setStatus(http.StatusInternalServerError)
	sub := spawnWorker(t, c, Options{BatchSize: 1})

	sub.DeliverSpan(gen.TracingSpan{SpanID: 1})
	c.setStatus(http.StatusOK)
	sub.DeliverSpan(gen.TracingSpan{SpanID: 2})

	stats, err := sub.Inspect(gen.PID{})
	if err != nil {
		t.Fatalf("inspect: %s", err)
	}
	if stats["export_errors"] != "1" {
		t.Errorf("export_errors = %q, want 1", stats["export_errors"])
	}
	if stats["spans_exported"] != "1" {
		t.Errorf("spans_exported = %q, want only the span that got through", stats["spans_exported"])
	}
	if stats["batch_size"] != "0" {
		t.Errorf("batch_size = %q, want the rejected batch dropped rather than retried forever", stats["batch_size"])
	}
}

func TestWorkerSurvivesAnUnreachableCollector(t *testing.T) {
	c := startCollector(t)
	sub := spawnWorker(t, c, Options{BatchSize: 1})
	c.server.Close()

	sub.DeliverSpan(gen.TracingSpan{SpanID: 1})

	if sub.Terminated() {
		t.Fatal("the worker died because the collector was down, taking the pool with it")
	}
	stats, err := sub.Inspect(gen.PID{})
	if err != nil {
		t.Fatalf("inspect: %s", err)
	}
	if stats["export_errors"] != "1" {
		t.Errorf("export_errors = %q, want 1", stats["export_errors"])
	}
}

func TestWorkerReportsItsCountersOnInspect(t *testing.T) {
	c := startCollector(t)
	sub := spawnWorker(t, c, Options{BatchSize: 2})

	sub.DeliverSpan(gen.TracingSpan{SpanID: 1})
	sub.DeliverSpan(gen.TracingSpan{SpanID: 2})
	sub.DeliverSpan(gen.TracingSpan{SpanID: 3})

	stats, err := sub.Inspect(gen.PID{})
	if err != nil {
		t.Fatalf("inspect: %s", err)
	}
	if stats["spans_received"] != "3" {
		t.Errorf("spans_received = %q, want 3", stats["spans_received"])
	}
	if stats["spans_exported"] != "2" {
		t.Errorf("spans_exported = %q, want 2", stats["spans_exported"])
	}
	if stats["batch_size"] != "1" {
		t.Errorf("batch_size = %q, want the 1 still waiting", stats["batch_size"])
	}
}
