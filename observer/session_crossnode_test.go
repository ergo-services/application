package observer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"ergo.services/ergo/gen"
	"ergo.services/ergo/testing/stage"
)

func (b *browser) postVia(port uint16, path string, body any) (int, apiResponse) {
	b.t.Helper()

	data, err := json.Marshal(body)
	if err != nil {
		b.t.Fatalf("marshal %s: %s", path, err)
	}

	url := fmt.Sprintf("http://localhost:%d%s", port, path)
	request, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		b.t.Fatalf("%s request: %s", path, err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Observer-Session", b.sessionID)
	request.Header.Set("X-Observer-Node", string(b.node))
	for name, value := range b.headers {
		request.Header.Set(name, value)
	}

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		b.t.Fatalf("post %s: %s", path, err)
	}
	defer response.Body.Close()

	var answer apiResponse
	json.NewDecoder(response.Body).Decode(&answer)
	return response.StatusCode, answer
}

func TestSessionCommandReachesTheObserverHoldingTheStream(t *testing.T) {
	s := stage.New(t, stage.StageOptions{RegistrarFull: true})

	portStream := freePort(t)
	portOther := freePort(t)

	holder := s.StartNode("obs_stream", stage.NodeOptions{
		EnableSystemApp: true,
		Applications: []gen.ApplicationBehavior{
			CreateApp(Options{Port: portStream, Host: "localhost"}),
		},
	})
	other := s.StartNode("obs_other", stage.NodeOptions{
		EnableSystemApp: true,
		Applications: []gen.ApplicationBehavior{
			CreateApp(Options{Port: portOther, Host: "localhost"}),
		},
	})

	if _, err := other.Native().Network().GetNode(holder.Native().Name()); err != nil {
		t.Fatalf("the observers did not connect: %s", err)
	}

	b := openBrowser(t, portStream)

	if b.node != holder.Native().Name() {
		t.Fatalf("the browser was told node %q, the stream is on %q", b.node, holder.Native().Name())
	}

	status, answer := b.postVia(portOther, "/api/subscribe",
		map[string]any{"type": "node_info", "args": map[string]any{}})

	if status != http.StatusOK {
		t.Fatalf("subscribe through the other observer answered %d: %s", status, answer.Error)
	}
	if answer.OK == false {
		t.Fatalf("subscribe through the other observer refused: %s", answer.Error)
	}

	b.wait("node_meta", 10*time.Second)
}

func TestSessionKeepsWorkingAfterSwitch(t *testing.T) {
	s := stage.New(t, stage.StageOptions{RegistrarFull: true})

	port := freePort(t)
	host := s.StartNode("obs_switch", stage.NodeOptions{
		EnableSystemApp: true,
		Applications: []gen.ApplicationBehavior{
			CreateApp(Options{Port: port, Host: "localhost"}),
		},
	})
	peer := s.StartNode("peer_switch", stage.NodeOptions{EnableSystemApp: true})

	if _, err := host.Native().Network().GetNode(peer.Native().Name()); err != nil {
		t.Fatalf("the observer did not reach the peer: %s", err)
	}

	b := openBrowser(t, port)
	observer := b.node

	answer := b.post("/api/switch", map[string]any{"node": string(peer.Native().Name())})
	if answer.OK == false {
		t.Fatalf("switch refused: %s", answer.Error)
	}

	var after struct {
		Observer gen.Atom `json:"Observer"`
		Node     nodeDesc `json:"Node"`
	}
	if err := json.Unmarshal(b.wait("connected", 10*time.Second), &after); err != nil {
		t.Fatalf("connected after the switch: %s", err)
	}
	if after.Node.Name != peer.Native().Name() {
		t.Fatalf("observing %q after the switch, want %q", after.Node.Name, peer.Native().Name())
	}
	if after.Observer != observer {
		t.Fatalf("the browser was told observer %q, want %q", after.Observer, observer)
	}
	b.node = after.Observer

	status, subscribed := b.postVia(port, "/api/subscribe",
		map[string]any{"type": "node_info", "args": map[string]any{}})
	if status != http.StatusOK || subscribed.OK == false {
		t.Fatalf("subscribe after a switch answered %d: %s", status, subscribed.Error)
	}
}

func TestSessionCommandForUnknownNodeIsRefused(t *testing.T) {
	s := stage.New(t, stage.StageOptions{RegistrarFull: true})

	port := freePort(t)
	s.StartNode("obs_only", stage.NodeOptions{
		EnableSystemApp: true,
		Applications: []gen.ApplicationBehavior{
			CreateApp(Options{Port: port, Host: "localhost"}),
		},
	})

	b := openBrowser(t, port)
	b.node = gen.Atom("nowhere@nohost")

	status, answer := b.postVia(port, "/api/subscribe",
		map[string]any{"type": "node_info", "args": map[string]any{}})

	if status == http.StatusOK {
		t.Fatalf("a session on a node that does not exist was served: %#v", answer)
	}
}
