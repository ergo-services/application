package observer

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"ergo.services/ergo/app/system/inspect"
	"ergo.services/ergo/app/system/manage"
	"ergo.services/ergo/gen"
	"ergo.services/ergo/testing/check"
	"ergo.services/ergo/testing/unit"
)

// cacheable by the revision: discovery, the listings and a read; a call is not one of them
func TestResultsCarryWhatTheRevisionRequires(t *testing.T) {
	cases := []struct {
		method    string
		result    any
		cacheable bool
	}{
		{"server/discover", mcpDiscovery{}, true},
		{"resources/list", mcpResourceList{}, true},
		{"resources/templates/list", mcpTemplateList{}, true},
		{"tools/list", mcpToolList{}, true},
		{"resources/read", mcpContents{Meta: &mcpReadMeta{}}, true},
		{"tools/call", mcpToolResult{}, false},
	}

	for _, c := range cases {
		encoded, err := json.Marshal(c.result)
		if err != nil {
			t.Fatalf("%s: %s", c.method, err)
		}
		var fields map[string]any
		if err := json.Unmarshal(encoded, &fields); err != nil {
			t.Fatalf("%s: %s", c.method, err)
		}

		if _, carried := fields["resultType"]; carried == false {
			t.Errorf("%s answers without resultType: %s", c.method, encoded)
		}
		for _, hint := range []string{"ttlMs", "cacheScope"} {
			if _, carried := fields[hint]; carried != c.cacheable {
				t.Errorf("%s carries %s: %t, want %t", c.method, hint, carried, c.cacheable)
			}
		}

		meta, nested := fields["_meta"].(map[string]any)
		if nested == false {
			t.Errorf("%s answers without _meta: %s", c.method, encoded)
			continue
		}
		if _, named := meta["io.modelcontextprotocol/serverInfo"]; named == false {
			t.Errorf("%s does not say who answered: %s", c.method, encoded)
		}

		// ttlMs is a field of the result: the older shape had it in _meta, where no client
		// looks for it
		if _, misplaced := meta["ttlMs"]; misplaced {
			t.Errorf("%s hides ttlMs in _meta", c.method)
		}
	}
}

func TestIdentityIsTheSameEverywhere(t *testing.T) {
	own := mcpIdentity()
	if own.Name == "" || own.Version == "" {
		t.Fatalf("the identity is incomplete: %#v", own)
	}
	if mcpAnswered().ServerInfo != own {
		t.Error("a result names a different server than mcpIdentity does")
	}

	gate := mcpGate{listener: withLimits(Listener{Port: 9911}), counts: &refusalCounts{}}
	if gate.discovery().Meta.ServerInfo != own {
		t.Error("discovery names a different server than a result does")
	}
}

// a tool result names its kind and has nowhere for a mime type; a reading names what it is a
// reading of and needs no kind
func TestContentBlocksAreTwoShapes(t *testing.T) {
	cases := []struct {
		role     string
		block    any
		required []string
		absent   []string
	}{
		{
			role:     "tools/call",
			block:    mcpText("{}"),
			required: []string{"type", "text"},
			absent:   []string{"mimeType", "uri"},
		},
		{
			role:     "resources/read",
			block:    mcpResourceContent{URI: "ergo://n@h/log", MimeType: mcpMimeJSON, Text: "{}"},
			required: []string{"uri", "text", "mimeType"},
			absent:   []string{"type"},
		},
	}

	for _, c := range cases {
		encoded, err := json.Marshal(c.block)
		if err != nil {
			t.Fatalf("%s: %s", c.role, err)
		}
		var fields map[string]any
		if err := json.Unmarshal(encoded, &fields); err != nil {
			t.Fatalf("%s: %s", c.role, err)
		}

		for _, name := range c.required {
			if _, carried := fields[name]; carried == false {
				t.Errorf("%s block has no %s: %s", c.role, name, encoded)
			}
		}
		for _, name := range c.absent {
			if _, carried := fields[name]; carried {
				t.Errorf("%s block carries %s, which its type has no field for: %s",
					c.role, name, encoded)
			}
		}
	}

	if got := mcpText("x").Type; got != "text" {
		t.Errorf("a text block calls itself %q", got)
	}

	bare, err := json.Marshal(mcpResourceContent{URI: "ergo://n@h/log", Text: "{}"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(bare), `"uri"`) == false {
		t.Errorf("a reading lost its uri: %s", bare)
	}
}

func TestToolBlocksHandOverTheResourceName(t *testing.T) {
	const uri = "ergo://n@h/processes"

	blocks := toolBlocks("processes", uri, mcpRendered{MimeType: mcpMimeJSON, Text: "{}"})
	if len(blocks) != 2 {
		t.Fatalf("a json answer of a lens came back as %d blocks", len(blocks))
	}
	if _, ok := blocks[0].(mcpTextContent); ok == false {
		t.Errorf("the first block is %T", blocks[0])
	}
	link, ok := blocks[1].(mcpResourceLink)
	if ok == false {
		t.Fatalf("the second block is %T", blocks[1])
	}
	if link.Type != "resource_link" {
		t.Errorf("the link calls itself %q", link.Type)
	}
	if link.URI != uri || link.Name != "processes" {
		t.Errorf("the link points at %q under the name %q", link.URI, link.Name)
	}

	if link.MimeType != mcpMimeJSON {
		t.Errorf("the link says a reading of the process listing is %q", link.MimeType)
	}
	if link.Title == "" {
		t.Error("the link carries no title, so a picker has nothing to show")
	}

	// a run is not a lens and has no spec in the table, and its link still describes it
	run := mcpLink(uriWordJob, "ergo://job/sweep")
	if run.URI != "ergo://job/sweep" || run.Title != jobTitle || run.MimeType != mcpMimeJSON {
		t.Errorf("the link to a run came out as %#v", run)
	}

	alone := toolBlocks("", "", mcpRendered{MimeType: mcpMimeJSON, Text: "{}"})
	if len(alone) != 1 {
		t.Fatalf("an answer of no resource came back as %d blocks", len(alone))
	}
}

// the name is a string in two tables, so nothing but this keeps them equal
func TestToolLensesExist(t *testing.T) {
	for _, table := range toolTables {
		for _, tool := range table {
			if tool.Lens == "" {
				continue
			}
			if _, known := lensSpecOf(tool.Lens); known == false {
				t.Errorf("tool %q names lens %q, which no resource answers",
					tool.Name, tool.Lens)
			}
		}
	}
}

// arguments are not under test here, so a refusal about a missing one is a pass and being
// unknown is not
func TestEveryToolActionIsBuilt(t *testing.T) {
	for _, table := range toolTables {
		for _, tool := range table {
			actions := []string{}
			if tool.Action != "" {
				actions = append(actions, tool.Action)
			}
			if tool.Build != nil {
				for _, args := range toolBuildProbes(tool) {
					if action, built, err := tool.Build(args); err == nil {
						actions = append(actions, action)
						_ = built
					}
				}
			}

			for _, action := range actions {
				_, _, err := buildActionRequest(action, map[string]any{})
				if err != nil && strings.Contains(err.Error(), "unknown action") {
					t.Errorf("tool %q asks for action %q, which the builder does not know",
						tool.Name, action)
				}
			}
		}
	}
}

// a builder branches on one field, so one probe per value of that field is enough
func toolBuildProbes(tool mcpTool) []map[string]any {
	probes := []map[string]any{{}}
	for name, item := range tool.Schema.Properties {
		for _, value := range item.Enum {
			probes = append(probes, map[string]any{name: value})
		}
	}
	return probes
}

// two of the pairs are required: 404 belongs to MethodNotFound and nothing else, and an
// internal failure must not read as a bad request a client is right to retry
func TestStatusMatchesTheCode(t *testing.T) {
	cases := map[int]int{
		mcpMethodNotFound:     http.StatusNotFound,
		mcpInternalError:      http.StatusInternalServerError,
		mcpInvalidParams:      http.StatusBadRequest,
		mcpInvalidRequest:     http.StatusBadRequest,
		mcpParseError:         http.StatusBadRequest,
		mcpHeaderMismatch:     http.StatusBadRequest,
		mcpUnsupportedVersion: http.StatusBadRequest,
	}
	for code, status := range cases {
		if got := mcpStatus(code); got != status {
			t.Errorf("code %d carries status %d, expected %d", code, got, status)
		}
	}
}

// a 404 here is the signal to fall back to the deprecated transport, so a single misspelled
// tool name would move the whole connection
func TestUnknownToolIsABadParameter(t *testing.T) {
	n := unit.StartNode(t, testNode, gen.NodeOptions{})
	n.Network().FailRegistrar(gen.ErrUnsupported)

	sub, err := n.Spawn(factory_post_worker, gen.ProcessOptions{})
	check.NoError(t, err)

	message, answer := webRequest(t, false, "tools/call", map[string]any{
		"name":      "no_such_tool",
		"arguments": map[string]any{"node": string(askedNode)},
	})
	sub.SendMessage(gen.PID{}, message)

	var out struct {
		Error *struct {
			Code    int            `json:"code"`
			Message string         `json:"message"`
			Data    map[string]any `json:"data"`
		} `json:"error"`
	}
	if err := json.Unmarshal(answer.Body.Bytes(), &out); err != nil {
		t.Fatalf("body %q: %s", answer.Body.String(), err)
	}
	if out.Error == nil {
		t.Fatalf("an unknown tool was answered with %q", answer.Body.String())
	}
	if out.Error.Code != mcpInvalidParams {
		t.Errorf("code %d (%s), expected %d", out.Error.Code, out.Error.Message, mcpInvalidParams)
	}
	if answer.Code == http.StatusNotFound {
		t.Error("an unknown tool answered 404, which sends the client to the legacy transport")
	}
	if out.Error.Data["name"] != "no_such_tool" {
		t.Errorf("the refusal does not name the tool: %#v", out.Error.Data)
	}
}

func TestRefusedToolCallIsAResult(t *testing.T) {
	n := unit.StartNode(t, testNode, gen.NodeOptions{})
	n.Network().FailRegistrar(gen.ErrUnsupported)

	sub, err := n.Spawn(factory_post_worker, gen.ProcessOptions{})
	check.NoError(t, err)

	cases := map[string]map[string]any{
		"a required argument is missing": {"name": "kill"},
		"the node is not connected": {
			"name":      "capabilities",
			"arguments": map[string]any{"node": "nowhere@nohost"},
		},
	}

	for what, params := range cases {
		message, answer := webRequest(t, false, "tools/call", params)
		sub.SendMessage(gen.PID{}, message)

		var out struct {
			Result *struct {
				ResultType string           `json:"resultType"`
				Content    []map[string]any `json:"content"`
				IsError    bool             `json:"isError"`
				Meta       map[string]any   `json:"_meta"`
			} `json:"result"`
			Error *map[string]any `json:"error"`
		}
		if err := json.Unmarshal(answer.Body.Bytes(), &out); err != nil {
			t.Fatalf("%s: body %q: %s", what, answer.Body.String(), err)
		}

		if answer.Code != http.StatusOK {
			t.Errorf("%s answered %d, and a tool result is a success", what, answer.Code)
		}
		if out.Error != nil {
			t.Errorf("%s came back as a protocol error: %v", what, *out.Error)
			continue
		}
		if out.Result == nil {
			t.Fatalf("%s answered neither a result nor an error: %s", what, answer.Body.String())
		}
		if out.Result.IsError == false {
			t.Errorf("%s answered a success: %s", what, answer.Body.String())
		}
		if out.Result.ResultType != mcpResultComplete {
			t.Errorf("%s answered resultType %q", what, out.Result.ResultType)
		}
		if len(out.Result.Content) != 1 || out.Result.Content[0]["type"] != "text" {
			t.Fatalf("%s carries %v, not one text block", what, out.Result.Content)
		}
		if reason, _ := out.Result.Content[0]["text"].(string); reason == "" {
			t.Errorf("%s does not say why", what)
		}
		if out.Result.Meta["io.modelcontextprotocol/serverInfo"] == nil {
			t.Errorf("%s left out the server identity: %v", what, out.Result.Meta)
		}
	}
}

// a client MUST weigh the annotations, and the revision defaults destructiveHint and
// openWorldHint to true, so every hint has to be stated
func TestToolsCarryAnnotations(t *testing.T) {
	destructive := map[string]bool{
		"kill": true, "send_exit": true, "app_stop": true, "app_unload": true,
	}
	writes := map[string]bool{
		"cluster_query": true, "cluster_batch": true, "job_cancel": true,
	}
	repeats := map[string]bool{"send": true}

	entries := toolEntries(Ceiling{})
	if len(entries) == 0 {
		t.Fatal("no tool is offered")
	}

	for _, tool := range entries {
		if tool.Annotations == nil {
			t.Errorf("%s carries no annotations", tool.Name)
			continue
		}

		readOnly := tool.Mutating == false && writes[tool.Name] == false
		if tool.Annotations.ReadOnlyHint != readOnly {
			t.Errorf("%s says readOnlyHint=%v, expected %v",
				tool.Name, tool.Annotations.ReadOnlyHint, readOnly)
		}
		if tool.Annotations.DestructiveHint != destructive[tool.Name] {
			t.Errorf("%s says destructiveHint=%v, expected %v",
				tool.Name, tool.Annotations.DestructiveHint, destructive[tool.Name])
		}
		if tool.Annotations.OpenWorldHint {
			t.Errorf("%s claims an open world", tool.Name)
		}
		if tool.Annotations.IdempotentHint == repeats[tool.Name] {
			t.Errorf("%s says idempotentHint=%v", tool.Name, tool.Annotations.IdempotentHint)
		}

		encoded, err := json.Marshal(tool)
		if err != nil {
			t.Fatal(err)
		}
		for _, hint := range []string{
			"readOnlyHint", "destructiveHint", "idempotentHint", "openWorldHint",
		} {
			if strings.Contains(string(encoded), hint) == false {
				t.Errorf("%s leaves out %s, and its default is not what we mean", tool.Name, hint)
			}
		}
	}
}

// the list is what a model plans from, so a tool the ceiling will refuse is not in it
func TestToolsTheCeilingRefusesAreNotOffered(t *testing.T) {
	offered := func(ceiling Ceiling) map[string]bool {
		out := map[string]bool{}
		for _, tool := range toolEntries(ceiling) {
			out[tool.Name] = true
		}
		return out
	}

	full := offered(Ceiling{})
	for _, name := range []string{"kill", "goroutines", "processes", "job_list"} {
		if full[name] == false {
			t.Errorf("an open ceiling does not offer %s", name)
		}
	}

	denied := offered(Ceiling{Deny: []string{manage.CapKill, inspect.CapGetGoroutines}})
	if denied["kill"] || denied["goroutines"] {
		t.Error("a denied capability is still offered as a tool")
	}
	if denied["send"] == false || denied["processes"] == false {
		t.Error("denying one capability took away tools that do not ask for it")
	}

	// a tool that may ask for several stays while any of them is left
	narrowed := offered(Ceiling{Deny: []string{manage.CapSend}})
	if narrowed["send"] == false {
		t.Error("send is gone though it may still go to a meta")
	}
	if offered(Ceiling{Deny: []string{manage.CapSend, manage.CapSendMeta}})["send"] {
		t.Error("send is offered with both of its capabilities denied")
	}

	// served here, asks the node for nothing, so no ceiling hides it
	if offered(Ceiling{Allow: []string{}})["job_list"] == false {
		t.Error("an empty allowlist took away a tool that asks the node for nothing")
	}
}

// every capability a tool declares has to be one its builder really asks for
func TestToolCapabilitiesMatchTheBuilders(t *testing.T) {
	known := map[string]bool{}
	for _, name := range append(append([]string{}, inspect.Capabilities()...), manage.Capabilities()...) {
		known[name] = true
	}

	for _, tool := range toolEntries(Ceiling{}) {
		for _, capability := range toolCapabilities(tool) {
			if known[capability] == false {
				t.Errorf("%s declares %q, which the framework does not name", tool.Name, capability)
			}
		}
	}
}

func TestToolsCarryATitle(t *testing.T) {
	entries := toolEntries(Ceiling{})
	if len(entries) == 0 {
		t.Fatal("no tool is offered")
	}

	seen := map[string]string{}
	for _, tool := range entries {
		switch {
		case tool.Title == "":
			t.Errorf("%s carries no title", tool.Name)
		case tool.Title == tool.Name:
			t.Errorf("%s repeats its name as a title", tool.Name)
		case seen[tool.Title] != "":
			t.Errorf("%s and %s share the title %q", seen[tool.Title], tool.Name, tool.Title)
		}
		seen[tool.Title] = tool.Name
	}
}

func TestClusterTTLIsNeverNegative(t *testing.T) {
	cases := map[time.Duration]int{
		3 * time.Second:         3000,
		1500 * time.Millisecond: 1500,
		0:                       ttlReading,
		-1 * time.Second:        ttlReading,
	}

	for period, want := range cases {
		if got := clusterTTL(period); got != want {
			t.Errorf("a period of %s became %d, want %d", period, got, want)
		}
	}
}

func TestFrameMetaKeysAreTheConstants(t *testing.T) {
	meta := reflect.TypeOf(mcpFrameMeta{})

	for field, want := range map[string]string{
		"SubscriptionID": metaSubscriptionID,
		"Refused":        metaRefused,
	} {
		declared, found := meta.FieldByName(field)
		if found == false {
			t.Fatalf("mcpFrameMeta has no %s", field)
		}
		if got := strings.Split(declared.Tag.Get("json"), ",")[0]; got != want {
			t.Errorf("%s goes on the wire as %q, the constant says %q", field, got, want)
		}
	}
}
