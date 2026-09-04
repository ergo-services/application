package observer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ergo.services/ergo/app/system/inspect"
	"ergo.services/ergo/gen"
	"ergo.services/ergo/meta"
	"ergo.services/ergo/testing/check"
	"ergo.services/ergo/testing/unit"
)

const askedNode = gen.Atom("shop-basket-aggregator-5f7c8d9b6-h4k2p@localhost")

func webRequest(t *testing.T, cancelled bool, method string, params map[string]any) (
	meta.MessageWebRequest, *httptest.ResponseRecorder) {

	t.Helper()

	body, headers := mcpConforming(method, 1, params)
	request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	for name, value := range headers {
		request.Header.Set(name, value)
	}

	var parsed *http.Request
	keep := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { parsed = r })
	gate := mcpGate{
		post:     keep,
		listen:   keep,
		listener: withLimits(Listener{Port: 9911}),
		counts:   &refusalCounts{},
	}

	refusal := httptest.NewRecorder()
	gate.ServeHTTP(refusal, request)
	if parsed == nil {
		t.Fatalf("the gate refused the request: %d %s", refusal.Code, refusal.Body.String())
	}

	ctx, cancel := context.WithCancel(parsed.Context())
	if cancelled {
		cancel()
	} else {
		t.Cleanup(cancel)
	}

	answer := httptest.NewRecorder()
	return meta.MessageWebRequest{
		Response: answer,
		Request:  parsed.WithContext(ctx),
		Done:     func() {},
	}, answer
}

// an abandoned request never reaches the observed node
func TestWorkerDropsAnAbandonedRequest(t *testing.T) {
	n := unit.StartNode(t, testNode, gen.NodeOptions{})
	n.Network().FailRegistrar(gen.ErrUnsupported)

	n.Network().OnGetNode(askedNode)

	sub, err := n.Spawn(factory_post_worker, gen.ProcessOptions{})
	check.NoError(t, err)

	sub.OnCall(gen.ProcessID{Name: inspect.Name, Node: askedNode}).
		Respond(inspect.ResponseGetGoroutines{})

	arguments := map[string]any{
		"name":      "goroutines",
		"arguments": map[string]any{"node": string(askedNode)},
	}

	live, answer := webRequest(t, false, "tools/call", arguments)
	sub.SendMessage(gen.PID{}, live)

	sub.ShouldCall().AtLeast(1).Assert()
	if answer.Body.Len() == 0 {
		t.Error("a live request went unanswered")
	}

	mark := sub.Mark()
	abandoned, silence := webRequest(t, true, "tools/call", arguments)
	sub.SendMessage(gen.PID{}, abandoned)

	sub.ShouldCall().Since(mark).None().Assert()
	if silence.Body.Len() != 0 {
		t.Errorf("an abandoned request was answered with %q", silence.Body.String())
	}
}
