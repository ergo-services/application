package observer

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func answer(nodes int) wireCluster {
	out := wireCluster{Nodes: make([]wireClusterNode, 0, nodes), Complete: true, Watched: nodes}
	for i := 0; i < nodes; i++ {
		out.Nodes = append(out.Nodes, wireClusterNode{
			Name:             "shop-catalog-indexer-7d9f8b5c4-x2m4q@localhost",
			Uptime:           14832,
			ProcessesTotal:   1204,
			ProcessesRunning: 1201,
			MemoryUsed:       193273528,
			MemoryAlloc:      44040192,
			Goroutines:       3218,
			Peers: []wireClusterPeer{
				{Node: "shop-order-matcher-6b8d4f2a1-kk29s@localhost", MessagesIn: 918273, BytesIn: 8172635},
				{Node: "shop-fraud-evaluator-4a2f6c8e5-j3l9v@localhost", MessagesIn: 172635, BytesIn: 2736451},
			},
		})
	}
	return out
}

func serve(t *testing.T, payload any, acceptEncoding string) *http.Response {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/do/cluster_info", strings.NewReader("{}"))
	if acceptEncoding != "" {
		request.Header.Set("Accept-Encoding", acceptEncoding)
	}
	recorder := httptest.NewRecorder()
	writeJSON(recorder, request, http.StatusOK, payload)
	return recorder.Result()
}

// the popup over the live indicator states this in words, so both halves are checked
func TestWriteJSONCompressesLargeAnswers(t *testing.T) {
	small := serve(t, apiResponse{Error: "not found"}, "gzip, deflate, br")
	if encoding := small.Header.Get("Content-Encoding"); encoding != "" {
		t.Errorf("a short answer went out as %q, expected it uncompressed", encoding)
	}

	unwilling := serve(t, answer(40), "identity")
	if encoding := unwilling.Header.Get("Content-Encoding"); encoding != "" {
		t.Errorf("answered %q to a client that asked for identity", encoding)
	}
	if vary := unwilling.Header.Get("Vary"); vary != "Accept-Encoding" {
		t.Errorf("Vary is %q, so a proxy may hand a gzipped answer to a client that cannot read it", vary)
	}

	large := serve(t, answer(40), "gzip, deflate, br")
	if encoding := large.Header.Get("Content-Encoding"); encoding != "gzip" {
		t.Fatalf("a large answer went out as %q, expected gzip", encoding)
	}

	reader, err := gzip.NewReader(large.Body)
	if err != nil {
		t.Fatalf("the body is not gzip: %s", err)
	}
	decoded, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read: %s", err)
	}
	var back wireCluster
	if err := json.Unmarshal(decoded, &back); err != nil {
		t.Fatalf("the decompressed body is not the answer: %s", err)
	}
	if len(back.Nodes) != 40 {
		t.Errorf("decompressed to %d nodes, expected 40", len(back.Nodes))
	}
}

func TestWriteJSONThreshold(t *testing.T) {
	just := serve(t, strings.Repeat("x", gzipMinSize), "gzip")
	if encoding := just.Header.Get("Content-Encoding"); encoding != "gzip" {
		t.Errorf("a %d byte answer went out as %q, expected gzip", gzipMinSize, encoding)
	}
	under := serve(t, strings.Repeat("x", gzipMinSize-16), "gzip")
	if encoding := under.Header.Get("Content-Encoding"); encoding != "" {
		t.Errorf("an answer under the threshold went out as %q", encoding)
	}
}
