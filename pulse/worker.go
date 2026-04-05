package pulse

import (
	"bytes"
	"fmt"
	"net/http"
	"time"

	"ergo.services/ergo/act"
	"ergo.services/ergo/gen"
	"google.golang.org/protobuf/proto"
)

func factoryWorker() gen.ProcessBehavior {
	return &worker{}
}

type worker struct {
	act.Actor

	options Options
	batch   []gen.TracingSpan
	client     *http.Client
	url        string
	flushTimer gen.CancelFunc

	// stats
	spansReceived uint64
	spansExported uint64
	exportErrors  uint64
}

func (w *worker) Init(args ...any) error {
	w.options = args[0].(Options)

	// pre-allocate batch
	w.batch = make([]gen.TracingSpan, 0, w.options.BatchSize)

	w.url = w.options.URL

	// own HTTP client with keep-alive
	w.client = &http.Client{
		Timeout: w.options.ExportTimeout,
		Transport: &http.Transport{
			MaxIdleConns:        1,
			MaxIdleConnsPerHost: 1,
			IdleConnTimeout:     90 * time.Second,
		},
	}

	w.scheduleFlush()
	return nil
}

func (w *worker) HandleSpan(span gen.TracingSpan) error {
	w.spansReceived++
	w.batch = append(w.batch, span)

	if len(w.batch) >= w.options.BatchSize {
		w.flush()
	}
	return nil
}

func (w *worker) HandleMessage(from gen.PID, message any) error {
	switch message.(type) {
	case messageFlushTimer:
		if len(w.batch) > 0 {
			w.flush()
		}
		w.scheduleFlush()
	}
	return nil
}

func (w *worker) HandleCall(from gen.PID, ref gen.Ref, request any) (any, error) {
	return nil, nil
}

func (w *worker) Terminate(reason error) {
	if w.flushTimer != nil {
		w.flushTimer()
	}
	// flush remaining spans
	if len(w.batch) > 0 {
		w.flush()
	}
	w.Log().Info("pulse worker terminated: received=%d exported=%d errors=%d",
		w.spansReceived, w.spansExported, w.exportErrors)
}

func (w *worker) HandleEvent(message gen.MessageEvent) error {
	return nil
}

func (w *worker) HandleInspect(from gen.PID, item ...string) map[string]string {
	return map[string]string{
		"spans_received": fmt.Sprintf("%d", w.spansReceived),
		"spans_exported": fmt.Sprintf("%d", w.spansExported),
		"export_errors":  fmt.Sprintf("%d", w.exportErrors),
		"batch_size":     fmt.Sprintf("%d", len(w.batch)),
	}
}

func (w *worker) flush() {
	if len(w.batch) == 0 {
		return
	}

	// convert and marshal
	req := buildExportRequest(w.batch, w.Node().Name())
	data, err := proto.Marshal(req)
	if err != nil {
		w.Log().Error("pulse: proto.Marshal failed: %s", err)
		w.batch = w.batch[:0]
		return
	}

	count := len(w.batch)
	w.batch = w.batch[:0]

	// blocking HTTP POST (safe — own goroutine)
	err = w.httpPost(data)
	if err != nil {
		w.exportErrors++
		w.Log().Error("pulse: export failed (%d spans): %s", count, err)
		return
	}

	w.spansExported += uint64(count)
	w.Log().Trace("pulse: exported %d spans", count)
}

func (w *worker) httpPost(data []byte) error {
	req, err := http.NewRequest("POST", w.url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-protobuf")
	for k, v := range w.options.Headers {
		req.Header.Set(k, v)
	}

	resp, err := w.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	return fmt.Errorf("HTTP %d", resp.StatusCode)
}

func (w *worker) scheduleFlush() {
	if w.flushTimer != nil {
		w.flushTimer()
	}
	cancel, err := w.SendAfter(w.PID(), messageFlushTimer{}, w.options.FlushInterval)
	if err == nil {
		w.flushTimer = cancel
	}
}
