package observer

import (
	"encoding/json"
	"testing"

	"ergo.services/ergo/app/system/inspect"
	"ergo.services/ergo/app/system/manage"
	"ergo.services/ergo/gen"
)

// the agent path refuses this by naming the node; the browser path had no name to refuse by
func TestActionRefusesAnIdentifierFromAnotherNode(t *testing.T) {
	for _, c := range []struct {
		action string
		args   map[string]any
	}{
		{"kill", map[string]any{"pid": "other@host:7.1"}},
		{"send_exit", map[string]any{"pid": "other@host:7.1"}},
		{"inspect", map[string]any{"pid": "other@host:7.1"}},
		{"meta_inspect", map[string]any{"alias": "other@host:1.2.3.4"}},
	} {
		args := map[string]any{nodeArgument: "mine@host"}
		for name, given := range c.args {
			args[name] = given
		}
		if _, _, err := buildActionRequest(c.action, args); err == nil {
			t.Errorf("%s took an identifier from another node", c.action)
		}
	}

	if _, _, err := buildActionRequest("kill", map[string]any{
		nodeArgument: "mine@host", "pid": "mine@host:7.1",
	}); err != nil {
		t.Errorf("an identifier of the observed node was refused: %s", err)
	}
}

const (
	argPIDJSON   = `{"Node":"demo@localhost","ID":1006,"Creation":1755000000}`
	argAliasJSON = `{"Node":"demo@localhost","ID":[1,2,3],"Creation":1755000000}`
)

var (
	wantPID   = gen.PID{Node: "demo@localhost", ID: 1006, Creation: 1755000000}
	wantAlias = gen.Alias{Node: "demo@localhost", ID: [3]uint64{1, 2, 3}, Creation: 1755000000}
)

func browserArgs(t *testing.T, body string) map[string]any {
	t.Helper()

	var out map[string]any
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("bad test json: %s", err)
	}
	return out
}

func mustBuild(t *testing.T, action string, body string) any {
	t.Helper()

	request, _, err := buildActionRequest(action, browserArgs(t, body))
	if err != nil {
		t.Fatalf("%s: %s", action, err)
	}
	return request
}

func mustCapability(t *testing.T, action string, body string) string {
	t.Helper()

	_, capability, err := buildActionRequest(action, browserArgs(t, body))
	if err != nil {
		t.Fatalf("%s: %s", action, err)
	}
	return capability
}

func TestActionRequestCarriesIdentity(t *testing.T) {
	if r := mustBuild(t, "kill", `{"pid":`+argPIDJSON+`}`); r != (manage.RequestDoKill{PID: wantPID}) {
		t.Errorf("kill built %#v", r)
	}

	send := mustBuild(t, "send", `{"pid":`+argPIDJSON+`,"message":"ping"}`)
	if r, ok := send.(manage.RequestDoSend); ok == false || r.PID != wantPID || r.Message != "ping" {
		t.Errorf("send built %#v", send)
	}

	sendMeta := mustBuild(t, "send", `{"alias":`+argAliasJSON+`,"message":"ping"}`)
	if r, ok := sendMeta.(manage.RequestDoSendMeta); ok == false || r.Meta != wantAlias {
		t.Errorf("send to meta built %#v", sendMeta)
	}

	exit := mustBuild(t, "send_exit", `{"pid":`+argPIDJSON+`,"reason":"shutdown"}`)
	if r, ok := exit.(manage.RequestDoSendExit); ok == false || r.PID != wantPID || r.Reason != gen.TerminateReasonShutdown {
		t.Errorf("send_exit built %#v", exit)
	}

	exitMeta := mustBuild(t, "send_exit", `{"alias":`+argAliasJSON+`,"reason":"kill"}`)
	if r, ok := exitMeta.(manage.RequestDoSendExitMeta); ok == false || r.Meta != wantAlias {
		t.Errorf("send_exit to meta built %#v", exitMeta)
	}

	state := mustBuild(t, "inspect", `{"pid":`+argPIDJSON+`,"items":["mcp"]}`)
	if r, ok := state.(inspect.RequestGetProcessState); ok == false || r.PID != wantPID || len(r.Items) != 1 {
		t.Errorf("inspect built %#v", state)
	}

	subtree := mustBuild(t, "subtree", `{"pid":`+argPIDJSON+`,"limit":10}`)
	if r, ok := subtree.(inspect.RequestGetSubtree); ok == false || r.PID != wantPID || r.Limit != 10 {
		t.Errorf("subtree built %#v", subtree)
	}
}

func TestActionRequestDecodesEnums(t *testing.T) {
	node := mustBuild(t, "set_log_level", `{"level":"warning"}`)
	if r, ok := node.(manage.RequestDoSetLogLevel); ok == false || r.Level != gen.LogLevelWarning {
		t.Errorf("node log level built %#v", node)
	}

	process := mustBuild(t, "set_log_level", `{"target":"process","pid":`+argPIDJSON+`,"level":"debug"}`)
	if r, ok := process.(manage.RequestDoSetProcessLogLevel); ok == false || r.Level != gen.LogLevelDebug || r.PID != wantPID {
		t.Errorf("process log level built %#v", process)
	}

	meta := mustBuild(t, "set_log_level", `{"target":"meta","alias":`+argAliasJSON+`,"level":"error"}`)
	if r, ok := meta.(manage.RequestDoSetMetaLogLevel); ok == false || r.Level != gen.LogLevelError || r.Meta != wantAlias {
		t.Errorf("meta log level built %#v", meta)
	}

	priority := mustBuild(t, "set_process_send_priority", `{"pid":`+argPIDJSON+`,"priority":"max"}`)
	if r, ok := priority.(manage.RequestDoSetProcessSendPriority); ok == false || r.Priority != gen.MessagePriorityMax {
		t.Errorf("priority built %#v", priority)
	}

	level := mustBuild(t, "set_process_compression_level", `{"pid":`+argPIDJSON+`,"level":"best speed"}`)
	if r, ok := level.(manage.RequestDoSetProcessCompressionLevel); ok == false || r.Level != gen.CompressionBestSpeed {
		t.Errorf("compression level built %#v", level)
	}

	mode := mustBuild(t, "app_start", `{"name":"demo","mode":"transient"}`)
	if r, ok := mode.(manage.RequestDoAppStart); ok == false || r.Mode != gen.ApplicationModeTransient {
		t.Errorf("app_start built %#v", mode)
	}
}

// a typo must not silently become a default and change the log level of a production node
func TestActionRequestRefusesUnknownEnum(t *testing.T) {
	if _, _, err := buildActionRequest("set_log_level", browserArgs(t, `{"level":"warn"}`)); err == nil {
		t.Error("an unknown log level was accepted")
	}
	if _, _, err := buildActionRequest("app_start", browserArgs(t, `{"name":"demo","mode":"forever"}`)); err == nil {
		t.Error("an unknown application mode was accepted")
	}
	if _, _, err := buildActionRequest("set_process_send_priority",
		browserArgs(t, `{"pid":`+argPIDJSON+`,"priority":"urgent"}`)); err == nil {
		t.Error("an unknown priority was accepted")
	}
}

func TestActionPlaneRouting(t *testing.T) {
	pid := `"pid":` + argPIDJSON
	alias := `"alias":` + argAliasJSON

	mutations := map[string]string{
		"send":                              `{` + pid + `,"message":"ping"}`,
		"send_exit":                         `{` + pid + `,"reason":"kill"}`,
		"kill":                              `{` + pid + `}`,
		"set_node_tracing_sampler":          `{"type":"none"}`,
		"set_process_tracing_sampler":       `{` + pid + `,"type":"none"}`,
		"set_log_level":                     `{"level":"info"}`,
		"app_start":                         `{"name":"demo"}`,
		"app_stop":                          `{"name":"demo"}`,
		"app_unload":                        `{"name":"demo"}`,
		"set_process_send_priority":         `{` + pid + `,"priority":"max"}`,
		"set_process_compression":           `{` + pid + `,"enabled":true}`,
		"set_process_compression_type":      `{` + pid + `,"type":"gzip"}`,
		"set_process_compression_level":     `{` + pid + `,"level":"best speed"}`,
		"set_process_compression_threshold": `{` + pid + `,"threshold":1024}`,
		"set_process_keep_network_order":    `{` + pid + `,"order":true}`,
		"set_process_important_delivery":    `{` + pid + `,"important":true}`,
		"set_meta_send_priority":            `{` + alias + `,"priority":"max"}`,
	}
	for action, body := range mutations {
		capability := mustCapability(t, action, body)
		if p := plane(capability); p != manage.Name {
			t.Errorf("%s (%s) routed to %s, expected %s", action, capability, p, manage.Name)
		}
	}

	reads := map[string]string{
		"inspect":      `{` + pid + `}`,
		"meta_inspect": `{` + alias + `}`,
		"app_tree":     `{"name":"demo"}`,
		"subtree":      `{` + pid + `}`,
		"goroutines":   `{}`,
		"heap":         `{}`,
		"types":        `{}`,
		"errors":       `{}`,
		"atoms":        `{}`,
		"capabilities": `{}`,
		"node":         `{}`,
		"network":      `{}`,
		"connections":  `{}`,
		"connection":   `{"peer":"n2@localhost"}`,
		"applications": `{}`,
		"events":       `{}`,
		"event":        `{"name":"orders"}`,
	}
	for action, body := range reads {
		capability := mustCapability(t, action, body)
		if p := plane(capability); p != inspect.Name {
			t.Errorf("%s (%s) routed to %s, expected %s", action, capability, p, inspect.Name)
		}
	}
}

func TestReadOnlyCeilingRefusesMutations(t *testing.T) {
	readOnly := Ceiling{ReadOnly: true}

	for _, capability := range []string{
		manage.CapSend, manage.CapSendMeta, manage.CapSendExit, manage.CapSendExitMeta,
		manage.CapKill, manage.CapSetLogLevel, manage.CapSetProcessLogLevel, manage.CapSetMetaLogLevel,
		manage.CapSetNodeTracingSampler, manage.CapSetProcessTracingSampler,
		manage.CapSetProcessSendPriority, manage.CapSetProcessCompression,
		manage.CapSetProcessCompressionType, manage.CapSetProcessCompressionLevel,
		manage.CapSetProcessCompressionThreshold, manage.CapSetProcessKeepNetworkOrder,
		manage.CapSetProcessImportantDelivery, manage.CapSetMetaSendPriority,
		manage.CapAppStart, manage.CapAppStop, manage.CapAppUnload,
	} {
		if readOnly.Allows(capability) {
			t.Errorf("read-only allowed %s", capability)
		}
	}

	for _, capability := range []string{
		inspect.CapGetProcessState, inspect.CapGetMetaState, inspect.CapGetAppTree,
		inspect.CapGetSubtree, inspect.CapGetGoroutines, inspect.CapGetHeapProfile,
		inspect.CapGetTypes, inspect.CapCapabilities, inspect.CapProcessList,
		inspect.CapEventStream, inspect.CapLog, inspect.CapTracing,
	} {
		if readOnly.Allows(capability) == false {
			t.Errorf("read-only refused %s", capability)
		}
	}
}

func TestCapabilitiesUnderCeiling(t *testing.T) {
	report := inspect.ResponseGetCapabilities{
		Node:         "demo@localhost",
		Manage:       true,
		Capabilities: []string{inspect.CapNode, manage.CapKill, inspect.CapLog, manage.CapSend},
	}

	full := wireCapabilitiesUnder(report, Ceiling{})
	if full.Manage == false || len(full.Capabilities) != 4 {
		t.Errorf("an empty ceiling changed the report: %+v", full)
	}

	readOnly := wireCapabilitiesUnder(report, Ceiling{ReadOnly: true})
	if readOnly.Manage {
		t.Errorf("read-only came out as %+v", readOnly)
	}
	if len(readOnly.Capabilities) != 2 || readOnly.Capabilities[0] != inspect.CapNode {
		t.Errorf("capabilities came out as %v", readOnly.Capabilities)
	}

	denied := wireCapabilitiesUnder(report, Ceiling{Deny: []string{manage.CapKill}})
	if denied.Manage == false {
		t.Errorf("one denied name left the plane unavailable: %+v", denied)
	}
	if len(denied.Capabilities) != 3 {
		t.Errorf("capabilities came out as %v", denied.Capabilities)
	}

	inspectOnly := wireCapabilitiesUnder(report, Ceiling{Allow: []string{inspect.CapNode}})
	if inspectOnly.Manage {
		t.Errorf("the manage plane was offered with no mutating capability left: %+v", inspectOnly)
	}
}

func TestArgIdentityErrors(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"missing", `{}`},
		{"null", `{"pid":null}`},
		{"wrong type", `{"pid":"<9F35C982.0.1006>"}`},
		{"no node", `{"pid":{"ID":1006,"Creation":1755000000}}`},
		{"no id", `{"pid":{"Node":"demo@localhost","Creation":1755000000}}`},
	}
	for _, c := range cases {
		if _, err := argPID(browserArgs(t, c.body), "pid"); err == nil {
			t.Errorf("%s: accepted %s", c.name, c.body)
		}
	}

	if _, err := argAlias(browserArgs(t, `{"alias":{"ID":[1,2,3]}}`), "alias"); err == nil {
		t.Error("an alias without a node was accepted")
	}
}

// the key is also the handle the browser holds, so two processes must never share one
func TestSubLookupKeyIdentities(t *testing.T) {
	first := browserArgs(t, `{"pid":`+argPIDJSON+`}`)
	second := browserArgs(t, `{"pid":{"Node":"demo@localhost","ID":2048,"Creation":1755000000}}`)
	restarted := browserArgs(t, `{"pid":{"Node":"demo@localhost","ID":1006,"Creation":1755999999}}`)

	keyFirst := subLookupKey("process_info", first)
	keySecond := subLookupKey("process_info", second)
	keyRestarted := subLookupKey("process_info", restarted)

	if keyFirst == keySecond {
		t.Errorf("two processes share the handle %q", keyFirst)
	}
	if keyFirst == keyRestarted {
		t.Errorf("the same id from another creation shares the handle %q", keyFirst)
	}
	if keyFirst != subLookupKey("process_info", browserArgs(t, `{"pid":`+argPIDJSON+`}`)) {
		t.Error("the same subscription produced two different handles")
	}

	metaKey := subLookupKey("meta_info", browserArgs(t, `{"alias":`+argAliasJSON+`}`))
	byID := subLookupKey("meta_info", browserArgs(t, `{"id":`+argAliasJSON+`}`))
	if metaKey != byID {
		t.Errorf("alias and id forms gave different handles: %q vs %q", metaKey, byID)
	}

	wide := subLookupKey("process_list", browserArgs(t, `{"pidLimit":500}`))
	narrow := subLookupKey("process_list", browserArgs(t, `{"pidLimit":500,"namePattern":"web"}`))
	if wide == narrow {
		t.Errorf("two scopes of one type share the handle %q", wide)
	}
}
