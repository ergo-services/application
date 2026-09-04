package observer

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"ergo.services/ergo/app/system/inspect"
	"ergo.services/ergo/gen"
	"ergo.services/ergo/testing/stage"
)

func fanoutStage(t *testing.T) (string, gen.Atom, map[string]string) {
	t.Helper()

	s := stage.New(t, stage.StageOptions{RegistrarFull: true})
	port := freePort(t)
	node := s.StartNode("obs", stage.NodeOptions{
		EnableSystemApp: true,
		Applications: []gen.ApplicationBehavior{CreateApp(Options{
			Listeners: []Listener{teamListener(port)},
		})},
	})
	return fmt.Sprintf("http://localhost:%d", port), node.Name(), headerOf("alice@corp", "sre")
}

func readRun(t *testing.T, base string, uri string, who map[string]string) (map[string]any, mcpReadMeta) {
	t.Helper()

	_, answer := postConforming(t, base+"/mcp", "resources/read", 2,
		map[string]any{"uri": uri}, who)

	var out struct {
		Result *mcpContents  `json:"result"`
		Error  *mcpErrorBody `json:"error"`
	}
	if err := json.Unmarshal(answer, &out); err != nil {
		t.Fatalf("read %s: %s", uri, err)
	}
	if out.Error != nil {
		t.Fatalf("read %s: %s", uri, out.Error.Message)
	}

	value := map[string]any{}
	if err := json.Unmarshal([]byte(out.Result.Contents[0].Text), &value); err != nil {
		t.Fatalf("read %s payload: %s", uri, err)
	}
	return value, *out.Result.Meta
}

func toolBlocksOf(t *testing.T, base string, name string, args map[string]any,
	who map[string]string) []map[string]any {

	t.Helper()

	_, answer := postConforming(t, base+"/mcp", "tools/call", 3,
		map[string]any{"name": name, "arguments": args}, who)

	var out struct {
		Result struct {
			Content []map[string]any `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal(answer, &out); err != nil {
		t.Fatalf("%s: %s", name, err)
	}
	return out.Result.Content
}

func TestClusterBatchAsksEachStepItsOwnQuestion(t *testing.T) {
	base, node, alice := fanoutStage(t)

	answer, refusal := mcpCall(t, base, "cluster_batch", map[string]any{
		"key": "picture",
		"steps": []any{
			map[string]any{"id": "one", nodeArgument: string(node), "tool": "node"},
			map[string]any{"id": "two", nodeArgument: string(node), "tool": "applications"},
		},
	}, alice)
	if refusal != "" {
		t.Fatalf("cluster_batch refused: %s", refusal)
	}
	if answer["started"] != true || answer["steps"] != float64(2) {
		t.Fatalf("the run started as %v", answer)
	}

	blocks := toolBlocksOf(t, base, "cluster_batch", map[string]any{
		"key": "picture",
		"steps": []any{
			map[string]any{"id": "one", nodeArgument: string(node), "tool": "node"},
			map[string]any{"id": "two", nodeArgument: string(node), "tool": "applications"},
		},
	}, alice)
	if len(blocks) != 2 || blocks[0]["type"] != "text" || blocks[1]["type"] != "resource_link" {
		t.Fatalf("the run came back as %v", blocks)
	}
	if blocks[1]["uri"] != "ergo://job/picture" || blocks[1]["mimeType"] != mcpMimeJSON {
		t.Errorf("the link points at %v", blocks[1])
	}

	var reading map[string]any
	eventually(t, "both steps answer", func() bool {
		reading, _ = readRun(t, base, "ergo://job/picture", alice)
		return reading["state"] == "completed"
	})

	if reading["answered"] != float64(2) || reading["failed"] != float64(0) {
		t.Fatalf("the run finished as %v", reading)
	}

	ids := map[string]bool{}
	results, _ := reading["results"].([]any)
	for _, entry := range results {
		result, _ := entry.(map[string]any)
		id, _ := result["id"].(string)
		ids[id] = true
	}
	if ids["one"] == false || ids["two"] == false {
		t.Errorf("the answers came back as %v", results)
	}
}

// since= takes only what landed after the cursor
func TestRunCursorTakesOnlyWhatIsNew(t *testing.T) {
	base, node, alice := fanoutStage(t)

	if _, refusal := mcpCall(t, base, "cluster_query", map[string]any{
		"tool": "node", "key": "sweep", "nodes": []any{string(node)},
	}, alice); refusal != "" {
		t.Fatalf("cluster_query refused: %s", refusal)
	}

	var meta mcpReadMeta
	var reading map[string]any
	eventually(t, "the run answers", func() bool {
		reading, meta = readRun(t, base, "ergo://job/sweep", alice)
		return reading["state"] == "completed"
	})

	results, _ := reading["results"].([]any)
	if len(results) != 1 {
		t.Fatalf("the first read carried %d answers", len(results))
	}
	if meta.NextSeq == "" {
		t.Fatal("the read handed out no cursor")
	}

	fresh, next := readRun(t, base, "ergo://job/sweep?since="+meta.NextSeq, alice)
	if results, _ := fresh["results"].([]any); len(results) != 0 {
		t.Errorf("reading again from the cursor carried %v", results)
	}
	if next.NextSeq != meta.NextSeq {
		t.Errorf("the cursor moved on a read that took nothing: %q -> %q", meta.NextSeq, next.NextSeq)
	}
	if next.Dropped {
		t.Error("a cursor that is still covered was called dropped")
	}

	if _, refusal := mcpCall(t, base, "cluster_query", map[string]any{
		"tool": "node", "key": "other", "nodes": []any{string(node)},
	}, alice); refusal != "" {
		t.Fatalf("the second run refused: %s", refusal)
	}
	var elsewhere mcpReadMeta
	eventually(t, "the second run answers", func() bool {
		var second map[string]any
		second, elsewhere = readRun(t, base, "ergo://job/other", alice)
		return second["state"] == "completed"
	})

	_, stale := readRun(t, base, "ergo://job/sweep?since="+elsewhere.NextSeq, alice)
	if stale.Dropped == false {
		t.Error("a cursor from another run was taken as covered")
	}
}

// the six the framework serves and nothing here asked for until now
func TestCronAndRegistrarTools(t *testing.T) {
	base, node, alice := fanoutStage(t)
	name := string(node)

	for what, args := range map[string]map[string]any{
		"cron":                         {nodeArgument: name},
		"cron_schedule":                {nodeArgument: name, "hours": 1, "limit": 10},
		"registrar_nodes":              {nodeArgument: name},
		"registrar_routes":             {nodeArgument: name, "peer": name},
		"registrar_proxy_routes":       {nodeArgument: name, "peer": name},
		"registrar_application_routes": {nodeArgument: name, "name": "system_app"},
	} {
		if _, refusal := mcpCall(t, base, what, args, alice); refusal != "" {
			t.Errorf("%s refused: %s", what, refusal)
		}
	}

	for what, args := range map[string]map[string]any{
		"registrar_routes without a peer":             {nodeArgument: name},
		"registrar_proxy_routes without a peer":       {nodeArgument: name},
		"registrar_application_routes without a name": {nodeArgument: name},
	} {
		tool := strings.Fields(what)[0]
		if _, refusal := mcpCall(t, base, tool, args, alice); refusal == "" {
			t.Errorf("%s was accepted", what)
		}
	}

	if _, refusal := mcpCall(t, base, "cron_schedule", map[string]any{
		nodeArgument: name, "since": "yesterday",
	}, alice); refusal == "" {
		t.Error("a since that is not a time was accepted")
	}
}

// enums reach the agent as names, not numbers
func TestApplicationRoutesCarryTheirNames(t *testing.T) {
	out := mcpViewOf(inspect.ResponseGetRegistrarApplicationRoutes{
		Routes: []gen.ApplicationRoute{{
			Node:  "n@h",
			Name:  "shop",
			Mode:  gen.ApplicationModePermanent,
			State: gen.ApplicationStateRunning,
		}},
	})

	view, ok := out.(wireMCPGetRegistrarApplicationRoutes)
	if ok == false {
		t.Fatalf("the view answered %T", out)
	}
	if view.Routes[0].Mode != "permanent" || view.Routes[0].State != "running" {
		t.Errorf("the route came out as %#v", view.Routes[0])
	}
}

func TestFanoutRefusals(t *testing.T) {
	base, node, alice := fanoutStage(t)

	many := make([]any, 0, jobStepsMax+1)
	for i := 0; i <= jobStepsMax; i++ {
		many = append(many, fmt.Sprintf("n%d@localhost", i))
	}

	for what, args := range map[string]map[string]any{
		"no key":       {"tool": "node"},
		"no tool":      {"key": "k1"},
		"unknown tool": {"key": "k1", "tool": "nonsense"},
		"a mutating step": {"key": "k1", "tool": "kill",
			"nodes": []any{string(node)}},
		"only unreachable nodes": {"key": "k1", "tool": "node",
			"nodes": []any{"elsewhere@host"}},
		"more nodes than a run takes": {"key": "k1", "tool": "node", "nodes": many},
	} {
		if answer, refusal := mcpCall(t, base, "cluster_query", args, alice); refusal == "" {
			t.Errorf("%s was accepted: %v", what, answer)
		}
	}

	answer, refusal := mcpCall(t, base, "job_list", nil, alice)
	if refusal != "" {
		t.Fatalf("job_list refused: %s", refusal)
	}
	if jobs, _ := answer["jobs"].([]any); len(jobs) != 0 {
		t.Errorf("a refused call left %v", jobs)
	}
}

// a repeated key joins a run only while it asks the same
func TestARepeatedKeyJoinsOnlyTheSameQuestion(t *testing.T) {
	base, node, alice := fanoutStage(t)

	same := map[string]any{"tool": "node", "key": "again", "nodes": []any{string(node)}}
	first, refusal := mcpCall(t, base, "cluster_query", same, alice)
	if refusal != "" {
		t.Fatalf("cluster_query refused: %s", refusal)
	}
	if first["started"] != true || first["steps"] != float64(1) {
		t.Fatalf("the run started as %v", first)
	}

	again, refusal := mcpCall(t, base, "cluster_query", same, alice)
	if refusal != "" {
		t.Fatalf("the same question was refused: %s", refusal)
	}
	if again["started"] != false || again["steps"] != float64(1) {
		t.Errorf("the retry answered %v", again)
	}

	_, refusal = mcpCall(t, base, "cluster_query", map[string]any{
		"tool": "applications", "key": "again", "nodes": []any{string(node)},
	}, alice)
	if refusal == "" {
		t.Fatal("a different question joined a run under the same key")
	}
	if strings.Contains(refusal, "different question") == false ||
		strings.Contains(refusal, "ergo://job/again") == false {
		t.Errorf("the refusal does not say what it collided with: %q", refusal)
	}

	if _, refusal := mcpCall(t, base, "cluster_query", map[string]any{
		"tool": "node", "key": "again", "nodes": []any{string(node)}, "retain": 30,
	}, alice); refusal != "" {
		t.Errorf("asking for a different retention was taken as a different question: %s", refusal)
	}
}

// a keyed reading is refused without a caller to key it to
func TestKeyedResourceNeedsACaller(t *testing.T) {
	s := stage.New(t, stage.StageOptions{RegistrarFull: true})
	open, team := freePort(t), freePort(t)
	node := s.StartNode("obs", stage.NodeOptions{
		EnableSystemApp: true,
		Applications: []gen.ApplicationBehavior{CreateApp(Options{
			Listeners: []Listener{{Name: "local", Port: open}, teamListener(team)},
		})},
	})

	keyed := "ergo://" + string(node.Name()) + "/watch/mine/node"
	shared := "ergo://" + string(node.Name()) + "/node"

	anonymous := fmt.Sprintf("http://localhost:%d", open)
	if _, answer := postConforming(t, anonymous+"/mcp", "resources/read", 1,
		map[string]any{"uri": keyed}, nil); jsonHasError(answer) == false {
		t.Errorf("an anonymous listener served a keyed reading: %s", answer)
	}
	if _, answer := postConforming(t, anonymous+"/mcp", "resources/read", 1,
		map[string]any{"uri": shared}, nil); jsonHasError(answer) {
		t.Errorf("the shared lens was refused too: %s", answer)
	}

	authorized := fmt.Sprintf("http://localhost:%d", team)
	if _, answer := postConforming(t, authorized+"/mcp", "resources/read", 1,
		map[string]any{"uri": keyed}, headerOf("alice@corp", "sre")); jsonHasError(answer) {
		t.Errorf("a caller with a name was refused a keyed reading: %s", answer)
	}

	_, answer := postConforming(t, anonymous+"/mcp", "resources/read", 1,
		map[string]any{"uri": "ergo://job/anything"}, nil)
	if jsonHasError(answer) == false {
		t.Fatalf("an anonymous listener served a run: %s", answer)
	}
	if strings.Contains(string(answer), errNeedsCaller.Error()) == false {
		t.Errorf("the refusal reads %s", answer)
	}
}

// a run holds a pool of workers, so what one caller can leave behind is bounded
func TestRunsAreCapped(t *testing.T) {
	s := stage.New(t, stage.StageOptions{RegistrarFull: true})
	port := freePort(t)
	node := s.StartNode("obs", stage.NodeOptions{
		EnableSystemApp: true,
		Applications: []gen.ApplicationBehavior{CreateApp(Options{
			JobLimit:        2,
			JobMaxRetention: 300 * time.Millisecond,
			Listeners:       []Listener{teamListener(port)},
		})},
	})
	base := fmt.Sprintf("http://localhost:%d", port)
	alice := headerOf("alice@corp", "sre")

	for _, key := range []string{"one", "two"} {
		if _, refusal := mcpCall(t, base, "cluster_query", map[string]any{
			"tool": "node", "key": key, "nodes": []any{string(node.Name())},
		}, alice); refusal != "" {
			t.Fatalf("run %s refused under the limit: %s", key, refusal)
		}
	}

	_, refusal := mcpCall(t, base, "cluster_query", map[string]any{
		"tool": "node", "key": "three", "nodes": []any{string(node.Name())},
	}, alice)
	if refusal == "" {
		t.Fatal("a third run was started over a limit of two")
	}
	if strings.Contains(refusal, "limit") == false {
		t.Errorf("the refusal does not say why: %q", refusal)
	}

	if _, refusal := mcpCall(t, base, "job_cancel", map[string]any{"key": "one"}, alice); refusal != "" {
		t.Fatalf("job_cancel refused: %s", refusal)
	}
	eventually(t, "the retired run frees a slot", func() bool {
		_, refusal := mcpCall(t, base, "cluster_query", map[string]any{
			"tool": "node", "key": "three", "nodes": []any{string(node.Name())},
		}, alice)
		return refusal == ""
	})
}

// a stream opened on a run is acknowledged and then followed
func TestListenFollowsARun(t *testing.T) {
	base, node, alice := fanoutStage(t)

	if _, refusal := mcpCall(t, base, "cluster_query", map[string]any{
		"tool": "node", "key": "watched", "nodes": []any{string(node)},
	}, alice); refusal != "" {
		t.Fatalf("cluster_query refused: %s", refusal)
	}

	ctx, stop := context.WithTimeout(context.Background(), 10*time.Second)
	defer stop()

	body, headers := mcpConforming("subscriptions/listen", 7, map[string]any{
		"notifications": map[string]any{
			"resourceSubscriptions": []any{"ergo://job/watched"},
		},
	})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/mcp",
		strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	for name, value := range alice {
		request.Header.Set(name, value)
	}

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("listen: %s", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("listen answered %d", response.StatusCode)
	}
	if mime := response.Header.Get("Content-Type"); strings.HasPrefix(mime, "text/event-stream") == false {
		t.Fatalf("listen answered %q, not a stream", mime)
	}

	frames := make(chan map[string]any, 8)
	go func() {
		reader := bufio.NewScanner(response.Body)
		for reader.Scan() {
			line, carried := strings.CutPrefix(reader.Text(), "data: ")
			if carried == false {
				continue
			}
			frame := map[string]any{}
			if json.Unmarshal([]byte(line), &frame) == nil {
				frames <- frame
			}
		}
		close(frames)
	}()

	next := func(what string) map[string]any {
		t.Helper()
		select {
		case frame, open := <-frames:
			if open == false {
				t.Fatalf("the stream ended before %s", what)
			}
			return frame
		case <-time.After(5 * time.Second):
			t.Fatalf("no %s arrived", what)
		}
		return nil
	}

	ack := next("acknowledgment")
	if ack["method"] != mcpSubscriptionsAck {
		t.Fatalf("the first frame was %v", ack)
	}
	params, _ := ack["params"].(map[string]any)
	notifications, _ := params["notifications"].(map[string]any)
	accepted, _ := notifications["resourceSubscriptions"].([]any)
	if len(accepted) != 1 || accepted[0] != "ergo://job/watched" {
		t.Fatalf("the run was not accepted: %v", params)
	}

	update := next("update")
	if update["method"] != mcpResourceUpdated {
		t.Fatalf("the second frame was %v", update)
	}
	params, _ = update["params"].(map[string]any)
	if params["uri"] != "ergo://job/watched" {
		t.Errorf("the update names %v", params["uri"])
	}
	meta, _ := params["_meta"].(map[string]any)
	if meta["io.modelcontextprotocol/subscriptionId"] != float64(7) {
		t.Errorf("the update carries subscription %v", meta)
	}
}

func TestRunNamesTheNodesItDropped(t *testing.T) {
	base, node, alice := fanoutStage(t)

	answer, refusal := mcpCall(t, base, "cluster_query", map[string]any{
		"tool":  "node",
		"key":   "partial",
		"nodes": []any{string(node), "elsewhere@host"},
	}, alice)
	if refusal != "" {
		t.Fatalf("a run over one reachable node was refused: %s", refusal)
	}
	if answer["steps"] != float64(1) {
		t.Errorf("the run covers %v steps", answer["steps"])
	}

	refused, _ := answer["refused"].(map[string]any)
	reason, named := refused["elsewhere@host"].(string)
	if named == false {
		t.Fatalf("the dropped node is not named: %v", answer)
	}
	if strings.Contains(reason, "not connected") == false {
		t.Errorf("the reason reads %q", reason)
	}

	var reading map[string]any
	eventually(t, "the run answers", func() bool {
		reading, _ = readRun(t, base, "ergo://job/partial", alice)
		return reading["state"] == "completed"
	})

	held, _ := reading["refused"].(map[string]any)
	if held["elsewhere@host"] != reason {
		t.Errorf("reading the run says %v about what it does not cover", held)
	}
}

func TestARunOfNothingSaysWhy(t *testing.T) {
	base, _, alice := fanoutStage(t)

	_, refusal := mcpCall(t, base, "cluster_query", map[string]any{
		"tool": "node", "key": "none", "nodes": []any{"elsewhere@host"},
	}, alice)

	var said struct {
		Steps   int               `json:"steps"`
		Refused map[string]string `json:"refused"`
	}
	if err := json.Unmarshal([]byte(refusal), &said); err != nil {
		t.Fatalf("the refusal reads %q: %s", refusal, err)
	}
	if said.Steps != 0 {
		t.Errorf("a run of nothing says it covers %d steps", said.Steps)
	}
	if strings.Contains(said.Refused["elsewhere@host"], "not connected") == false {
		t.Errorf("the refusal says %v", said.Refused)
	}
}
