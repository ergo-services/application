package observer

import (
	"encoding/json"
	"fmt"
	"slices"
	"testing"

	"ergo.services/application/observer/access"
	"ergo.services/ergo/gen"
	"ergo.services/ergo/testing/stage"
)

func teamListener(port uint16) Listener {
	return Listener{
		Name: "team",
		Host: "localhost",
		Port: port,
		UI:   SurfaceUI{Disable: true},
		Authorizer: access.TrustedHeader{
			Subject: "X-Auth-Request-Email",
			Tenant:  "X-Auth-Request-Domain",
			Groups:  "X-Auth-Request-Groups",
			Ceilings: map[string]Ceiling{
				"viewer": {ReadOnly: true, Deny: []string{"manage.kill"}},
				"sre":    {Deny: []string{"manage.kill"}},
			},
		},
	}
}

func headerOf(who string, groups string) map[string]string {
	return map[string]string{
		"X-Auth-Request-Email":  who,
		"X-Auth-Request-Groups": groups,
	}
}

func headerOfTenant(tenant string, who string, groups string) map[string]string {
	out := headerOf(who, groups)
	out["X-Auth-Request-Domain"] = tenant
	return out
}

// two tenants naming the same subject do not share runs
func TestTenantsDoNotShareAKeyspace(t *testing.T) {
	s := stage.New(t, stage.StageOptions{RegistrarFull: true})
	port := freePort(t)
	node := s.StartNode("obs", stage.NodeOptions{
		EnableSystemApp: true,
		Applications: []gen.ApplicationBehavior{CreateApp(Options{
			Listeners: []Listener{teamListener(port)},
		})},
	})
	base := fmt.Sprintf("http://localhost:%d", port)

	acme := headerOfTenant("acme", "admin", "sre")
	other := headerOfTenant("other", "admin", "sre")

	if _, refusal := mcpCall(t, base, "cluster_query", map[string]any{
		"tool": "node", "key": "sweep", "nodes": []any{string(node.Name())},
	}, acme); refusal != "" {
		t.Fatalf("cluster_query refused: %s", refusal)
	}

	listed := func(who map[string]string) int {
		answer, refusal := mcpCall(t, base, "job_list", nil, who)
		if refusal != "" {
			t.Fatalf("job_list refused: %s", refusal)
		}
		jobs, _ := answer["jobs"].([]any)
		return len(jobs)
	}
	if listed(acme) != 1 {
		t.Errorf("the tenant that started it lists %d", listed(acme))
	}
	if listed(other) != 0 {
		t.Errorf("the same name under another tenant lists %d", listed(other))
	}

	answer, refusal := mcpCall(t, base, "cluster_query", map[string]any{
		"tool": "applications", "key": "sweep", "nodes": []any{string(node.Name())},
	}, other)
	if refusal != "" {
		t.Fatalf("the other tenant was refused its own key: %s", refusal)
	}
	if answer["started"] != true {
		t.Errorf("the other tenant joined a run it does not own: %v", answer)
	}
}

// what the trusted header says reaches the ceiling
func TestTrustedHeaderOverHTTP(t *testing.T) {
	s := stage.New(t, stage.StageOptions{RegistrarFull: true})
	port := freePort(t)
	node := s.StartNode("obs", stage.NodeOptions{
		EnableSystemApp: true,
		Applications: []gen.ApplicationBehavior{CreateApp(Options{
			Listeners: []Listener{teamListener(port)},
		})},
	})
	base := fmt.Sprintf("http://localhost:%d", port)

	for what, given := range map[string]map[string]string{
		"no header":    nil,
		"empty header": headerOf("  ", "sre"),
	} {
		if code, _ := postConforming(t, base+"/mcp", "tools/list", 1, nil, given); code != 401 {
			t.Errorf("%s answered %d, want 401", what, code)
		}
	}

	if code, _ := postConforming(t, base+"/mcp", "tools/list", 1, nil,
		headerOf("bob@corp", "interns")); code != 403 {
		t.Errorf("an unknown group answered %d, want 403", code)
	}

	offered := func(groups string) []string {
		_, answer := postConforming(t, base+"/mcp", "tools/list", 1, nil,
			headerOf("alice@corp", groups))
		var out struct {
			Result struct {
				Tools []mcpTool `json:"tools"`
			} `json:"result"`
		}
		if err := json.Unmarshal(answer, &out); err != nil {
			t.Fatalf("tools/list for %s: %s", groups, err)
		}
		names := make([]string, 0, len(out.Result.Tools))
		for _, tool := range out.Result.Tools {
			names = append(names, tool.Name)
		}
		return names
	}

	if slices.Contains(offered("viewer"), "kill") {
		t.Error("a read-only caller is offered kill")
	}
	if slices.Contains(offered("sre"), "send") == false {
		t.Error("a writing caller is not offered send")
	}

	target, err := node.Native().Spawn(factory_probe, gen.ProcessOptions{})
	if err != nil {
		t.Fatalf("spawn the target: %s", err)
	}
	args := map[string]any{nodeArgument: string(node.Name()), "pid": mcpPIDText(target)}

	for _, groups := range []string{"sre", "viewer"} {
		if _, refusal := mcpCall(t, base, "kill", args, headerOf("alice@corp", groups)); refusal == "" {
			t.Errorf("%s killed a process the ceiling refuses", groups)
		}
	}
	if _, err := node.Native().ProcessState(target); err != nil {
		t.Fatalf("the target is gone: %s", err)
	}

	if _, refusal := mcpCall(t, base, "send_exit", args, headerOf("alice@corp", "sre")); refusal != "" {
		t.Errorf("send_exit refused for sre: %s", refusal)
	}
	eventually(t, "send_exit terminated the process", func() bool {
		_, err := node.Native().ProcessState(target)
		return err != nil
	})
}

// a run is readable and cancellable only by its caller
func TestRunsBelongToTheCaller(t *testing.T) {
	s := stage.New(t, stage.StageOptions{RegistrarFull: true})
	port := freePort(t)
	node := s.StartNode("obs", stage.NodeOptions{
		EnableSystemApp: true,
		Applications: []gen.ApplicationBehavior{CreateApp(Options{
			Listeners: []Listener{teamListener(port)},
		})},
	})
	base := fmt.Sprintf("http://localhost:%d", port)

	alice := headerOf("alice@corp", "sre")
	bob := headerOf("bob@corp", "sre")

	started, refusal := mcpCall(t, base, "cluster_query", map[string]any{
		"tool": "node", "key": "sweep", "nodes": []any{string(node.Name())},
	}, alice)
	if refusal != "" {
		t.Fatalf("cluster_query refused: %s", refusal)
	}
	if started["started"] != true || started["uri"] != "ergo://job/sweep" {
		t.Fatalf("the run started as %v", started)
	}

	eventually(t, "the run answers for every step", func() bool {
		reading := mcpRead(t, base, "ergo://job/sweep", alice)
		return reading["state"] == "completed"
	})

	again, refusal := mcpCall(t, base, "cluster_query", map[string]any{
		"tool": "node", "key": "sweep", "nodes": []any{string(node.Name())},
	}, alice)
	if refusal != "" {
		t.Fatalf("the repeated key refused: %s", refusal)
	}
	if again["started"] != false {
		t.Errorf("the repeated key started a second run: %v", again)
	}

	listed := func(who map[string]string) []any {
		answer, refusal := mcpCall(t, base, "job_list", nil, who)
		if refusal != "" {
			t.Fatalf("job_list refused: %s", refusal)
		}
		jobs, _ := answer["jobs"].([]any)
		return jobs
	}
	if len(listed(alice)) != 1 {
		t.Errorf("the caller who started it lists %v", listed(alice))
	}
	if len(listed(bob)) != 0 {
		t.Errorf("another caller lists %v", listed(bob))
	}

	if _, answer := postConforming(t, base+"/mcp", "resources/read", 2,
		map[string]any{"uri": "ergo://job/sweep"}, bob); jsonHasError(answer) == false {
		t.Errorf("another caller read the run: %s", answer)
	}
	if answer, refusal := mcpCall(t, base, "job_cancel",
		map[string]any{"key": "sweep"}, bob); refusal == "" && answer["held"] == true {
		t.Errorf("another caller cancelled the run: %v", answer)
	}

	if answer, refusal := mcpCall(t, base, "job_cancel",
		map[string]any{"key": "sweep"}, alice); refusal != "" || answer["held"] != true {
		t.Errorf("the owner could not cancel: %v %s", answer, refusal)
	}
}

func jsonHasError(answer []byte) bool {
	var out struct {
		Error *mcpErrorBody `json:"error"`
	}
	return json.Unmarshal(answer, &out) == nil && out.Error != nil
}
