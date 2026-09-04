package observer

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"ergo.services/ergo/app/system/inspect"
	"ergo.services/ergo/gen"
)

func TestWireProcessIdentityRoundTrip(t *testing.T) {
	row := wireProcessFrom(gen.ProcessShortInfo{
		PID:    wantPID,
		Name:   "web",
		Parent: gen.PID{Node: "demo@localhost", ID: 3, Creation: 1755000000},
		Leader: gen.PID{Node: "demo@localhost", ID: 3, Creation: 1755000000},
	})

	data, err := json.Marshal(row)
	if err != nil {
		t.Fatalf("marshal: %s", err)
	}
	if strings.Contains(string(data), "demo@localhost") == false {
		t.Fatalf("the node name is missing from %s", data)
	}
	if strings.Contains(string(data), "1755000000") == false {
		t.Fatalf("the creation is missing from %s", data)
	}

	var back wireProcess
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("unmarshal: %s", err)
	}
	if back.PID != wirePIDFrom(wantPID) {
		t.Errorf("pid came back as %#v, want %#v", back.PID, wantPID)
	}
	if back.Parent != row.Parent || back.Leader != row.Leader {
		t.Errorf("parent or leader changed: %#v", back)
	}
}

func TestWireProcessDetailKeepsRemoteNode(t *testing.T) {
	remote := gen.PID{Node: "other@host", ID: 77, Creation: 1700000000}
	remoteAlias := gen.Alias{Node: "other@host", ID: [3]uint64{9, 8, 7}, Creation: 1700000000}

	detail := wireProcessDetailFrom(gen.ProcessInfo{
		PID:               wantPID,
		MonitorsPID:       []gen.PID{remote},
		LinksPID:          []gen.PID{remote},
		MonitorsAlias:     []gen.Alias{remoteAlias},
		MonitorsProcessID: []gen.ProcessID{{Name: "worker", Node: "other@host"}},
		MonitorsEvent:     []gen.Event{{Name: "tick", Node: "other@host"}},
	})

	data, err := json.Marshal(detail)
	if err != nil {
		t.Fatalf("marshal: %s", err)
	}

	var back wireProcessDetail
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("unmarshal: %s", err)
	}
	if len(back.MonitorsPID) != 1 || back.MonitorsPID[0] != wirePIDFrom(remote) {
		t.Errorf("monitored pid came back as %#v", back.MonitorsPID)
	}
	if len(back.LinksPID) != 1 || back.LinksPID[0] != wirePIDFrom(remote) {
		t.Errorf("linked pid came back as %#v", back.LinksPID)
	}
	if len(back.MonitorsAlias) != 1 || back.MonitorsAlias[0] != wireAliasFrom(remoteAlias) {
		t.Errorf("monitored alias came back as %#v", back.MonitorsAlias)
	}
	if len(back.MonitorsProcessID) != 1 || back.MonitorsProcessID[0].Node != "other@host" {
		t.Errorf("monitored process id came back as %#v", back.MonitorsProcessID)
	}
	if len(back.MonitorsEvent) != 1 || back.MonitorsEvent[0].Node != "other@host" {
		t.Errorf("monitored event came back as %#v", back.MonitorsEvent)
	}
}

func TestWireLogEntryOptionalIdentity(t *testing.T) {
	out := wireLogFrom(inspect.MessageInspectLog{
		Node: "demo@localhost",
		Entries: []inspect.InspectLogEntry{
			{Source: "node", Message: "started"},
			{Source: "process", PID: wantPID, Parent: wantPID, Meta: wantAlias, Message: "работает"},
		},
	})

	data, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal: %s", err)
	}

	var back struct {
		Entries []map[string]any `json:"en"`
	}
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("unmarshal: %s", err)
	}
	if len(back.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(back.Entries))
	}
	for _, key := range []string{"i", "p", "m"} {
		if _, exist := back.Entries[0][key]; exist {
			t.Errorf("a node-level entry carries %q", key)
		}
		if _, exist := back.Entries[1][key]; exist == false {
			t.Errorf("a process entry misses %q", key)
		}
	}
}

func TestWireSpanTarget(t *testing.T) {
	cases := []struct {
		to   any
		kind string
	}{
		{wantPID, "pid"},
		{gen.ProcessID{Name: "worker", Node: "demo@localhost"}, "process_id"},
		{wantAlias, "alias"},
		{nil, ""},
	}

	for _, c := range cases {
		out := wireTracingFrom(inspect.MessageInspectTracing{
			Node:  "demo@localhost",
			Spans: []gen.TracingSpan{{From: wantPID, To: c.to}},
		})
		if len(out.Spans) != 1 {
			t.Fatalf("expected one span, got %d", len(out.Spans))
		}
		if out.Spans[0].ToKind != c.kind {
			t.Errorf("%T got kind %q, want %q", c.to, out.Spans[0].ToKind, c.kind)
		}
		if out.Spans[0].From != wirePIDFrom(wantPID) {
			t.Errorf("sender came out as %#v", out.Spans[0].From)
		}
	}
}

// no framework type decides the shape of an answer
func TestWireOwnsItsJSON(t *testing.T) {
	marshaler := reflect.TypeOf((*json.Marshaler)(nil)).Elem()

	seen := map[reflect.Type]bool{}
	var walk func(string, reflect.Type)
	walk = func(path string, typ reflect.Type) {
		if seen[typ] {
			return
		}
		seen[typ] = true

		if typ.PkgPath() == "ergo.services/ergo/gen" {
			if typ.Implements(marshaler) || reflect.PointerTo(typ).Implements(marshaler) {
				t.Errorf("%s at %s marshals itself", typ, path)
			}
		}
		switch typ.Kind() {
		case reflect.Slice, reflect.Array, reflect.Ptr, reflect.Map:
			walk(path+"[]", typ.Elem())
		case reflect.Struct:
			for i := 0; i < typ.NumField(); i++ {
				if typ.Field(i).IsExported() {
					walk(path+"."+typ.Field(i).Name, typ.Field(i).Type)
				}
			}
		}
	}

	for _, payload := range []any{
		wireProcessList{}, wireProcessInfo{}, wireMetaInfo{}, wireAppTree{}, wireSubtree{},
		wireLookup{}, wireApplicationList{}, wireEventList{}, wireEventStream{}, wireLog{},
		wireTracing{}, wireNodeInfo{}, wireNetworkInfo{}, wireConnectionList{},
		wireConnectionInfo{}, wireCluster{}, wireCapabilities{}, wireGoroutines{},
		wireHeapProfile{}, wireTypes{},
	} {
		walk(reflect.TypeOf(payload).Name(), reflect.TypeOf(payload))
	}
	for view := range mcpViewed {
		walk(view.Name(), view)
	}
}

func TestWireEnumNames(t *testing.T) {
	row := wireProcessFrom(gen.ProcessShortInfo{
		PID:      wantPID,
		State:    gen.ProcessStateWaitResponse,
		LogLevel: gen.LogLevelWarning,
	})
	if row.State != "wait response" || row.LogLevel != "warning" {
		t.Errorf("state %q, log level %q", row.State, row.LogLevel)
	}

	detail := wireProcessDetailFrom(gen.ProcessInfo{
		PID:             wantPID,
		MessagePriority: gen.MessagePriorityHigh,
		Compression:     gen.Compression{Level: gen.CompressionBestSize},
	})
	if detail.MessagePriority != "high" || detail.Compression.Level != "best size" {
		t.Errorf("priority %q, compression level %q", detail.MessagePriority, detail.Compression.Level)
	}
}

func TestWireSubscriptionShapes(t *testing.T) {
	data, err := json.Marshal(wireSubscribed{Key: "process_info:pid=x"})
	if err != nil {
		t.Fatalf("marshal: %s", err)
	}
	if string(data) != `{"key":"process_info:pid=x"}` {
		t.Errorf("subscribe answer is %s", data)
	}

	data, err = json.Marshal(wireSubscriptionDown{Keys: []string{"a", "b"}, Type: "process_info"})
	if err != nil {
		t.Fatalf("marshal: %s", err)
	}
	if string(data) != `{"keys":["a","b"],"type":"process_info"}` {
		t.Errorf("subscription down is %s", data)
	}
}

func TestWireCapabilities(t *testing.T) {
	out := wireCapabilitiesFrom(inspect.ResponseGetCapabilities{
		Node:         "demo@localhost",
		Creation:     1755000000,
		Framework:    gen.Version{Name: "Ergo Framework", Release: "3.3.0"},
		Manage:       true,
		Capabilities: []string{inspect.CapNode, "manage.kill"},
		Build:        []string{"latency"},
	})

	if out.Node != "demo@localhost" || out.Creation != 1755000000 {
		t.Errorf("node or creation lost: %#v", out)
	}
	if out.Manage == false {
		t.Error("the mutating plane was reported as down")
	}
	if len(out.Capabilities) != 2 || len(out.Build) != 1 {
		t.Errorf("lists came out as %#v / %#v", out.Capabilities, out.Build)
	}
	if out.Framework.Release != "3.3.0" {
		t.Errorf("framework version came out as %#v", out.Framework)
	}
}

var wireBrowserSplits = map[string][]string{
	"EventInfo.Event":     {"Name", "Node"},
	"ProcessInfo.Tracing": {"TracingSampler", "TracingAttributes"},
}

var wireBrowserNarrows = map[string]string{
	"MetaInfo.MailboxQueues": "a meta has Main and System and no latency: node/node.go " +
		"fills those two and leaves the rest of gen.MailboxQueues at zero",
}

func TestWireBrowserMirrorsTheDomain(t *testing.T) {
	for _, c := range []struct {
		domain any
		view   any
	}{
		{gen.ApplicationInfo{}, wireApplication{}},
		{gen.EventInfo{}, wireEvent{}},
		{gen.RemoteNodeInfo{}, wireConnection{}},
		{gen.NetworkInfo{}, wireNetwork{}},
		{gen.NodeInfo{}, wireNode{}},
		{gen.ProcessShortInfo{}, wireProcess{}},
		{gen.ProcessInfo{}, wireProcessDetail{}},
		{gen.MetaInfo{}, wireMeta{}},
		{gen.NodeShortInfo{}, wireClusterNode{}},
		{gen.RegisteredTypeInfo{}, wireRegisteredType{}},
		{gen.NetworkFlags{}, wireNetworkFlags{}},
	} {
		domain := reflect.TypeOf(c.domain)
		wireCarries(t, domain.Name(), domain, reflect.TypeOf(c.view))
	}
}

func wireCarries(t *testing.T, path string, domain, view reflect.Type) {
	t.Helper()

	if domain.Kind() != reflect.Struct || view.Kind() != reflect.Struct {
		return
	}

	carried := map[string]reflect.StructField{}
	for i := 0; i < view.NumField(); i++ {
		carried[view.Field(i).Name] = view.Field(i)
	}

	for i := 0; i < domain.NumField(); i++ {
		field := domain.Field(i)
		if field.IsExported() == false {
			continue
		}
		here := path + "." + field.Name

		if parts, split := wireBrowserSplits[here]; split {
			for _, name := range parts {
				if _, present := carried[name]; present == false {
					t.Errorf("%s is carried as %v and the view has no %s", here, parts, name)
				}
			}
			continue
		}

		mirror, present := carried[field.Name]
		if present == false {
			t.Errorf("%s is in the framework type and not in the view", here)
			continue
		}
		if _, narrowed := wireBrowserNarrows[here]; narrowed {
			if mirror.Type == field.Type {
				t.Errorf("%s is declared narrower than %s and carries it whole", here, field.Type)
			}
			continue
		}
		wireCarries(t, here, wireElem(field.Type), wireElem(mirror.Type))
	}
}

func wireElem(typ reflect.Type) reflect.Type {
	for {
		switch typ.Kind() {
		case reflect.Slice, reflect.Array, reflect.Ptr, reflect.Map:
			typ = typ.Elem()
		default:
			return typ
		}
	}
}
