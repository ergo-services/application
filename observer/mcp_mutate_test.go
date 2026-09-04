package observer

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	"ergo.services/ergo/app"
	"ergo.services/ergo/app/system/manage"
	"ergo.services/ergo/gen"
	"ergo.services/ergo/testing/stage"
)

// a message announced as int8 arrives as int8
func TestSendCarriesTheNamedType(t *testing.T) {
	node := gen.Atom("n@h")
	pid := gen.PID{Node: node, ID: 7, Creation: 1}
	alias := gen.Alias{Node: node, ID: [3]uint64{1, 2, 3}, Creation: 1}
	at := time.Date(2026, 2, 3, 4, 5, 6, 7, time.UTC)

	values := map[string]struct {
		text string
		want any
	}{
		"string":     {"x", "x"},
		"atom":       {"worker", gen.Atom("worker")},
		"bool":       {"true", true},
		"binary":     {base64.StdEncoding.EncodeToString([]byte{1, 2, 3}), []byte{1, 2, 3}},
		"int":        {"-7", int(-7)},
		"int8":       {"-128", int8(-128)},
		"int16":      {"-32768", int16(-32768)},
		"int32":      {"2147483647", int32(2147483647)},
		"int64":      {"9007199254740993", int64(9007199254740993)},
		"uint":       {"7", uint(7)},
		"uint8":      {"255", uint8(255)},
		"uint16":     {"65535", uint16(65535)},
		"uint32":     {"4294967295", uint32(4294967295)},
		"uint64":     {"18446744073709551615", uint64(18446744073709551615)},
		"float32":    {"1.5", float32(1.5)},
		"float64":    {"1.5", float64(1.5)},
		"time":       {at.Format(time.RFC3339Nano), at},
		"pid":        {mcpPIDText(pid), pid},
		"process_id": {mcpProcessIDText(gen.ProcessID{Name: "worker", Node: node}), gen.ProcessID{Name: "worker", Node: node}},
		"alias":      {mcpRefText(gen.Ref(alias)), alias},
		"ref":        {mcpRefText(gen.Ref{Node: node, ID: [3]uint64{4, 5, 6}, Creation: 2}), gen.Ref{Node: node, ID: [3]uint64{4, 5, 6}, Creation: 2}},
		"event":      {mcpEventText(gen.Event{Name: "tick", Node: node}), gen.Event{Name: "tick", Node: node}},
	}

	if len(values) != len(sendTypes) {
		t.Errorf("the schema announces %d types, this test knows %d", len(sendTypes), len(values))
	}
	for _, kind := range sendTypes {
		if _, known := values[kind]; known == false {
			t.Errorf("the schema announces %q and nothing here sends it", kind)
		}
	}

	for kind, c := range values {
		action, out, err := buildSend(map[string]any{
			nodeArgument: string(node), "pid": mcpPIDText(pid), "type": kind, "value": c.text,
		})
		if err != nil {
			t.Errorf("%s: %s", kind, err)
			continue
		}
		request, capability, err := buildActionRequest(action, out)
		if err != nil {
			t.Errorf("%s: %s", kind, err)
			continue
		}
		if capability != manage.CapSend {
			t.Errorf("%s asks for %s", kind, capability)
		}
		send, ok := request.(manage.RequestDoSend)
		if ok == false {
			t.Errorf("%s built %T", kind, request)
			continue
		}
		if reflect.DeepEqual(send.Message, c.want) == false {
			t.Errorf("%s carried %#v, want %#v", kind, send.Message, c.want)
		}
	}

	action, out, err := buildSend(map[string]any{
		nodeArgument: string(node), "alias": mcpRefText(gen.Ref(alias)),
		"type": "atom", "value": "ping",
	})
	if err != nil {
		t.Fatal(err)
	}
	request, capability, err := buildActionRequest(action, out)
	if err != nil {
		t.Fatal(err)
	}
	meta, ok := request.(manage.RequestDoSendMeta)
	if ok == false {
		t.Fatalf("an alias built %T", request)
	}
	if capability != manage.CapSendMeta || meta.Meta != alias || meta.Message != gen.Atom("ping") {
		t.Errorf("the meta send came out as %s %#v", capability, meta)
	}
}

// an absent priority is normal, and a meta takes none
func TestSendCarriesThePriority(t *testing.T) {
	node := gen.Atom("n@h")
	pid := mcpPIDText(gen.PID{Node: node, ID: 7, Creation: 1})

	for name, want := range map[string]gen.MessagePriority{
		"":       gen.MessagePriorityNormal,
		"normal": gen.MessagePriorityNormal,
		"high":   gen.MessagePriorityHigh,
		"max":    gen.MessagePriorityMax,
	} {
		args := map[string]any{
			nodeArgument: string(node), "pid": pid, "type": "atom", "value": "ping",
		}
		if name != "" {
			args["priority"] = name
		}

		action, out, err := buildSend(args)
		if err != nil {
			t.Fatalf("%q: %s", name, err)
		}
		request, _, err := buildActionRequest(action, out)
		if err != nil {
			t.Fatalf("%q: %s", name, err)
		}
		send, ok := request.(manage.RequestDoSend)
		if ok == false {
			t.Fatalf("%q built %T", name, request)
		}
		if send.Priority != want {
			t.Errorf("priority %q became %s, want %s", name, send.Priority, want)
		}
	}

	if _, _, err := buildActionRequest("send", map[string]any{
		nodeArgument: string(node), "pid": pid, "priority": "urgent", "message": "x",
	}); err == nil {
		t.Error("an unknown priority was accepted")
	}
}

// out of range is a refusal: a wrap would deliver a different number than the one asked for
func TestSendRefusesWhatDoesNotFit(t *testing.T) {
	for _, c := range []struct{ kind, value string }{
		{"int8", "128"},
		{"int8", "-129"},
		{"uint8", "-1"},
		{"uint64", "18446744073709551616"},
		{"int", "1.5"},
		{"int32", ""},
		{"bool", "yes"},
		{"binary", "not base64!"},
		{"float64", "one"},
		{"time", "yesterday"},
		{"pid", "nonsense"},
		{"", "x"},
		{"struct", "{}"},
	} {
		_, err := argSendMessage(map[string]any{
			nodeArgument: "n@h", "type": c.kind, "value": c.value,
		})
		if err == nil {
			t.Errorf("%s took %q", c.kind, c.value)
		}
	}
}

func TestProcessTuneHandsTheFieldToTheAction(t *testing.T) {
	tool, served := toolByName("process_tune")
	if served == false {
		t.Fatal("process_tune is not served")
	}

	for _, c := range []struct {
		knob     string
		field    string
		given    any
		argument string
	}{
		{"compression", "enabled", true, "enabled"},
		{"keep_network_order", "enabled", false, "order"},
		{"important_delivery", "enabled", true, "important"},
		{"compression_threshold", "threshold", float64(2048), "threshold"},
		{"send_priority", "priority", "high", "priority"},
		{"compression_type", "type", "gzip", "type"},
		{"compression_level", "level", "best size", "level"},
	} {
		action, args, err := toolAction(tool, map[string]any{"knob": c.knob, c.field: c.given})
		if err != nil {
			t.Errorf("knob %s: %s", c.knob, err)
			continue
		}
		if args[c.argument] != c.given {
			t.Errorf("knob %s: %s came out %#v, want %#v", c.knob, c.argument, args[c.argument], c.given)
		}
		if action != knobs[c.knob].action {
			t.Errorf("knob %s asked %s", c.knob, action)
		}

		if _, _, err := toolAction(tool, map[string]any{"knob": c.knob}); err == nil {
			t.Errorf("knob %s was accepted with no %s", c.knob, c.field)
		}
	}

	action, _, err := toolAction(tool, map[string]any{
		"knob": "send_priority", "priority": "max", "alias": map[string]any{"Node": "n@h"},
	})
	if err != nil || action != "set_meta_send_priority" {
		t.Errorf("a meta asked %s, %v", action, err)
	}

	if _, _, err := toolAction(tool, map[string]any{"knob": "nonsense", "enabled": true}); err == nil {
		t.Error("an unknown knob was accepted")
	}
}

// the schema is a hand-kept copy of what the builders accept, and a copy drifts
func TestSchemaEnumsAreParsed(t *testing.T) {
	pid := mcpPIDText(gen.PID{Node: "n@h", ID: 7, Creation: 1})
	alias := mcpRefText(gen.Ref{Node: "n@h", ID: [3]uint64{1, 2, 3}, Creation: 1})

	for _, c := range []struct {
		action string
		field  string
		values []string
		extra  map[string]any
	}{
		{"set_log_level", "level", logLevelNames, nil},
		{"set_process_send_priority", "priority", priorityNames, map[string]any{"pid": pid}},
		{"set_meta_send_priority", "priority", priorityNames, map[string]any{"alias": alias}},
		{"set_process_compression_type", "type", compressionTypes, map[string]any{"pid": pid}},
		{"set_process_compression_level", "level", compressionLevels, map[string]any{"pid": pid}},
		{"app_start", "mode", applicationModes, map[string]any{"name": "app"}},
	} {
		for _, value := range c.values {
			args := map[string]any{c.field: value}
			for name, given := range c.extra {
				args[name] = given
			}
			if _, _, err := buildActionRequest(c.action, args); err != nil {
				t.Errorf("%s does not take %s=%q: %s", c.action, c.field, value, err)
			}
		}
	}

	if len(knobNames) != len(knobs) {
		t.Errorf("the knob enum lists %d, the table holds %d", len(knobNames), len(knobs))
	}
	for _, name := range knobNames {
		if _, known := knobs[name]; known == false {
			t.Errorf("the knob enum lists %q, which the table does not know", name)
		}
	}
}

func TestStepToolsMatchTheTable(t *testing.T) {
	admitted := map[string]bool{}
	for _, tool := range append(append([]mcpTool{}, mcpTools...), mutationTools...) {
		if tool.Fanout == false && tool.Mutating == false && tool.servesAction() {
			admitted[tool.Name] = true
		}
	}

	for _, name := range stepTools {
		if admitted[name] == false {
			t.Errorf("stepTools lists %q, which a run does not admit", name)
		}
		delete(admitted, name)
	}
	for name := range admitted {
		t.Errorf("a run admits %q, which stepTools does not list", name)
	}
}

func mcpCall(t *testing.T, base string, name string, args map[string]any, headers map[string]string) (map[string]any, string) {
	t.Helper()

	params := map[string]any{"name": name}
	if args != nil {
		params["arguments"] = args
	}

	code, answer := postConforming(t, base+"/mcp", "tools/call", 1, params, headers)
	var out struct {
		Result *mcpToolResult `json:"result"`
		Error  *mcpErrorBody  `json:"error"`
	}
	if err := json.Unmarshal(answer, &out); err != nil {
		t.Fatalf("%s answered %d %q: %s", name, code, answer, err)
	}
	if out.Error != nil {
		return nil, out.Error.Message
	}
	if out.Result.ResultType != mcpResultComplete {
		t.Errorf("%s answered resultType %q", name, out.Result.ResultType)
	}
	if len(out.Result.Content) == 0 {
		t.Fatalf("%s answered without content: %s", name, answer)
	}

	if out.Result.IsError {
		_, reason := blockOf(t, out.Result.Content[0])
		if reason == "" {
			t.Fatalf("%s failed without saying why: %s", name, answer)
		}
		return nil, reason
	}

	mime, text := blockOf(t, out.Result.Content[0])
	if mime != mcpMimeJSON {
		t.Fatalf("%s answered %s, which this helper does not decode", name, mime)
	}

	value := map[string]any{}
	if err := json.Unmarshal([]byte(text), &value); err != nil {
		t.Fatalf("%s content %q: %s", name, text, err)
	}
	return value, ""
}

func blockOf(t *testing.T, block any) (string, string) {
	t.Helper()

	fields, ok := block.(map[string]any)
	if ok == false {
		t.Fatalf("a block came back as %T", block)
	}

	switch fields["type"] {
	case "text":
		text, _ := fields["text"].(string)
		return mcpMimeJSON, text
	case "resource":
		resource, _ := fields["resource"].(map[string]any)
		mime, _ := resource["mimeType"].(string)
		text, _ := resource["text"].(string)
		return mime, text
	}
	t.Fatalf("a block of kind %v is not one this surface sends", fields["type"])
	return "", ""
}

func mcpRead(t *testing.T, base string, uri string, headers map[string]string) map[string]any {
	t.Helper()

	_, answer := postConforming(t, base+"/mcp", "resources/read", 2,
		map[string]any{"uri": uri}, headers)

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
	if out.Result.ResultType != mcpResultComplete {
		t.Errorf("read %s answered resultType %q", uri, out.Result.ResultType)
	}
	if out.Result.CacheScope != mcpCachePrivate || out.Result.TTLMs <= 0 {
		t.Errorf("read %s offered %s for %dms", uri, out.Result.CacheScope, out.Result.TTLMs)
	}

	value := map[string]any{}
	if err := json.Unmarshal([]byte(out.Result.Contents[0].Text), &value); err != nil {
		t.Fatalf("read %s payload: %s", uri, err)
	}
	return value
}

func eventually(t *testing.T, what string, check func() bool) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("%s did not happen", what)
}

func TestMCPKillAndSendExit(t *testing.T) {
	s := stage.New(t, stage.StageOptions{RegistrarFull: true})
	port := freePort(t)
	node := s.StartNode("obs", stage.NodeOptions{
		EnableSystemApp: true,
		Applications:    []gen.ApplicationBehavior{CreateApp(Options{Port: port, Host: "localhost"})},
	})
	base := fmt.Sprintf("http://localhost:%d", port)

	for _, tool := range []string{"kill", "send_exit"} {
		target, err := node.Native().Spawn(factory_probe, gen.ProcessOptions{})
		if err != nil {
			t.Fatalf("spawn the target: %s", err)
		}

		answer, refusal := mcpCall(t, base, tool, map[string]any{
			nodeArgument: string(node.Name()),
			"pid":        mcpPIDText(target),
		}, nil)
		if refusal != "" {
			t.Fatalf("%s refused: %s", tool, refusal)
		}
		if answer["done"] == nil {
			t.Errorf("%s answered %v", tool, answer)
		}

		eventually(t, tool+" terminated the process", func() bool {
			_, err := node.Native().ProcessState(target)
			return err != nil
		})
	}
}

func TestMCPSetsAreVisibleInTheLens(t *testing.T) {
	s := stage.New(t, stage.StageOptions{RegistrarFull: true})
	port := freePort(t)
	node := s.StartNode("obs", stage.NodeOptions{
		EnableSystemApp: true,
		Applications:    []gen.ApplicationBehavior{CreateApp(Options{Port: port, Host: "localhost"})},
	})
	base := fmt.Sprintf("http://localhost:%d", port)
	uri := "ergo://" + string(node.Name()) + "/node"

	if _, refusal := mcpCall(t, base, "log_level_set", map[string]any{
		nodeArgument: string(node.Name()),
		"level":      "debug",
	}, nil); refusal != "" {
		t.Fatalf("log_level_set refused: %s", refusal)
	}

	eventually(t, "the node lens shows the new log level", func() bool {
		info, _ := mcpRead(t, base, uri, nil)["Info"].(map[string]any)
		return info != nil && info["LogLevel"] == "debug"
	})

	if _, refusal := mcpCall(t, base, "tracing_sampler_set", map[string]any{
		nodeArgument: string(node.Name()),
		"type":       "always",
		"rate":       1,
	}, nil); refusal != "" {
		t.Fatalf("tracing_sampler_set refused: %s", refusal)
	}

	eventually(t, "the node lens shows the sampler", func() bool {
		info, _ := mcpRead(t, base, uri, nil)["Info"].(map[string]any)
		if info == nil {
			return false
		}
		tracing, _ := info["Tracing"].(map[string]any)
		return tracing != nil && tracing["Sampler"] == "always"
	})
}

func TestMCPProcessTuneEveryKnob(t *testing.T) {
	s := stage.New(t, stage.StageOptions{RegistrarFull: true})
	port := freePort(t)
	node := s.StartNode("obs", stage.NodeOptions{
		EnableSystemApp: true,
		Applications:    []gen.ApplicationBehavior{CreateApp(Options{Port: port, Host: "localhost"})},
	})
	base := fmt.Sprintf("http://localhost:%d", port)

	target, err := node.Native().Spawn(factory_probe, gen.ProcessOptions{})
	if err != nil {
		t.Fatalf("spawn the target: %s", err)
	}

	turns := []struct {
		knob  string
		field string
		value any
		want  string
	}{
		{"send_priority", "priority", "high", "set_process_send_priority"},
		{"compression", "enabled", true, "set_process_compression"},
		{"compression_type", "type", "gzip", "set_process_compression_type"},
		{"compression_level", "level", "best size", "set_process_compression_level"},
		{"compression_threshold", "threshold", 2048, "set_process_compression_threshold"},
		{"keep_network_order", "enabled", false, "set_process_keep_network_order"},
		{"important_delivery", "enabled", true, "set_process_important_delivery"},
	}

	for _, c := range turns {
		answer, refusal := mcpCall(t, base, "process_tune", map[string]any{
			nodeArgument: string(node.Name()),
			"knob":       c.knob,
			"pid":        mcpPIDText(target),
			c.field:      c.value,
		}, nil)
		if refusal != "" {
			t.Errorf("knob %s refused: %s", c.knob, refusal)
			continue
		}
		if answer["done"] != c.want {
			t.Errorf("knob %s reached %v, want %s", c.knob, answer["done"], c.want)
		}
	}

	if _, refusal := mcpCall(t, base, "process_tune", map[string]any{
		nodeArgument: string(node.Name()), "knob": "nonsense", "pid": mcpPIDText(target),
	}, nil); refusal == "" {
		t.Error("an unknown knob was accepted")
	}

	if _, refusal := mcpCall(t, base, "process_tune", map[string]any{
		nodeArgument: string(node.Name()), "knob": "compression",
		"pid": mcpPIDText(target), "enabled": "maybe",
	}, nil); refusal == "" {
		t.Error("compression accepted \"maybe\" as enabled")
	}

	if _, refusal := mcpCall(t, base, "process_tune", map[string]any{
		nodeArgument: string(node.Name()), "knob": "compression_type",
		"pid": mcpPIDText(target), "type": "gzp",
	}, nil); refusal == "" {
		t.Error("compression_type accepted \"gzp\"")
	}

	state, err := node.Native().ProcessInfo(target)
	if err != nil {
		t.Fatalf("process info: %s", err)
	}
	if state.Compression.Enable == false {
		t.Error("compression is off")
	}
	if state.ImportantDelivery == false {
		t.Error("important delivery is off")
	}
	if state.KeepNetworkOrder {
		t.Error("keep_network_order is still on")
	}
	if state.MessagePriority != gen.MessagePriorityHigh {
		t.Errorf("send priority is %s", state.MessagePriority)
	}
	if state.Compression.Threshold != 2048 {
		t.Errorf("threshold is %d, want 2048", state.Compression.Threshold)
	}

	if _, refusal := mcpCall(t, base, "process_tune", map[string]any{
		nodeArgument: string(node.Name()), "knob": "keep_network_order",
		"pid": mcpPIDText(target), "enabled": true,
	}, nil); refusal != "" {
		t.Fatalf("turning keep_network_order back on: %s", refusal)
	}
	if state, err = node.Native().ProcessInfo(target); err != nil {
		t.Fatal(err)
	} else if state.KeepNetworkOrder == false {
		t.Error("keep_network_order did not come back on")
	}
}

type disposableApp struct {
	app.Application
}

func (d *disposableApp) Load(args ...any) (gen.ApplicationSpec, error) {
	return gen.ApplicationSpec{
		Name: "disposable",
		Mode: gen.ApplicationModeTransient,
		Group: []gen.ApplicationMemberSpec{
			{Name: "disposable_probe", Factory: factory_probe},
		},
	}, nil
}

func TestMCPApplicationLifecycle(t *testing.T) {
	s := stage.New(t, stage.StageOptions{RegistrarFull: true})
	port := freePort(t)
	node := s.StartNode("obs", stage.NodeOptions{
		EnableSystemApp: true,
		Applications: []gen.ApplicationBehavior{
			CreateApp(Options{Port: port, Host: "localhost"}),
			&disposableApp{},
		},
	})
	base := fmt.Sprintf("http://localhost:%d", port)
	uri := "ergo://" + string(node.Name()) + "/applications"

	stateOf := func(name string) string {
		apps, _ := mcpRead(t, base, uri, nil)["Applications"].(map[string]any)
		one, _ := apps[name].(map[string]any)
		if one == nil {
			return "absent"
		}
		state, _ := one["State"].(string)
		return state
	}

	const target = "disposable"
	if state := stateOf(target); state != "running" {
		t.Fatalf("%s is %s before anything was done", target, state)
	}

	if _, refusal := mcpCall(t, base, "app_stop", map[string]any{
		nodeArgument: string(node.Name()), "name": target,
	}, nil); refusal != "" {
		t.Fatalf("app_stop refused: %s", refusal)
	}
	eventually(t, "the application stopped", func() bool { return stateOf(target) == "loaded" })

	if _, refusal := mcpCall(t, base, "app_start", map[string]any{
		nodeArgument: string(node.Name()), "name": target,
	}, nil); refusal != "" {
		t.Fatalf("app_start refused: %s", refusal)
	}
	eventually(t, "the application started again", func() bool { return stateOf(target) == "running" })

	if _, refusal := mcpCall(t, base, "app_start", map[string]any{
		nodeArgument: string(node.Name()), "name": target,
	}, nil); refusal == "" {
		t.Error("a second start was accepted")
	}

	if _, refusal := mcpCall(t, base, "app_unload", map[string]any{
		nodeArgument: string(node.Name()), "name": "nosuchapp",
	}, nil); refusal == "" {
		t.Error("unloading an application that is not there was accepted")
	}
}

func TestMCPReadOnlyCeiling(t *testing.T) {
	s := stage.New(t, stage.StageOptions{RegistrarFull: true})
	full, readOnly := freePort(t), freePort(t)
	node := s.StartNode("obs", stage.NodeOptions{
		EnableSystemApp: true,
		Applications: []gen.ApplicationBehavior{CreateApp(Options{
			Listeners: []Listener{
				{Name: "full", Host: "localhost", Port: full},
				{Name: "readonly", Host: "localhost", Port: readOnly,
					Ceiling: Ceiling{ReadOnly: true}, UI: SurfaceUI{Disable: true}},
			},
		})},
	})

	names := func(port uint16) map[string]bool {
		base := fmt.Sprintf("http://localhost:%d/mcp", port)
		_, answer := postConforming(t, base, "tools/list", 1, nil, nil)
		var out struct {
			Result struct {
				Tools []mcpTool `json:"tools"`
			} `json:"result"`
		}
		if err := json.Unmarshal(answer, &out); err != nil {
			t.Fatalf("tools/list on %d: %s", port, err)
		}
		listed := map[string]bool{}
		for _, tool := range out.Result.Tools {
			listed[tool.Name] = true
		}
		return listed
	}

	offered, narrowed := names(full), names(readOnly)
	if offered["kill"] == false {
		t.Error("kill is not offered on a full ceiling")
	}
	if narrowed["kill"] {
		t.Error("kill is offered to a read-only caller")
	}
	if narrowed["process_state"] == false {
		t.Error("reading was taken away along with writing")
	}

	target, err := node.Native().Spawn(factory_probe, gen.ProcessOptions{})
	if err != nil {
		t.Fatal(err)
	}
	_, refusal := mcpCall(t, fmt.Sprintf("http://localhost:%d", readOnly), "kill", map[string]any{
		nodeArgument: string(node.Name()),
		"pid":        target,
	}, nil)
	if refusal == "" {
		t.Fatal("a read-only caller killed a process")
	}
	if state, err := node.Native().ProcessState(target); err != nil {
		t.Errorf("the process is gone after a refused kill: %s", err)
	} else if state == gen.ProcessStateTerminated {
		t.Error("the process is terminated after a refused kill")
	}
}
