package observer

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"ergo.services/ergo/app/system/inspect"
	"ergo.services/ergo/gen"
)

var mcpAnswers = []any{
	inspect.ResponseGetProcessRange{},
	inspect.ResponseGetProcessList{},
	inspect.ResponseGetAppTree{},
	inspect.ResponseGetSubtree{},
	inspect.ResponseGetTypes{},
	inspect.ResponseGetErrors{},
	inspect.ResponseGetAtoms{},
	inspect.ResponseGetGoroutines{},
	inspect.ResponseGetHeapProfile{},
	inspect.ResponseGetProcessState{},
	inspect.ResponseGetMetaState{},
	inspect.ResponseGetProcessLookup{},
	inspect.ResponseGetCapabilities{},
	inspect.ResponseGetNode{},
	inspect.ResponseGetNetwork{},
	inspect.ResponseGetConnection{},
	inspect.ResponseGetConnectionList{},
	inspect.ResponseGetApplicationList{},
	inspect.ResponseGetEventList{},
	inspect.ResponseGetEvent{},
	inspect.ResponseGetCronInfo{},
	inspect.ResponseGetCronSchedule{},
	inspect.ResponseGetRegistrarNodes{},
	inspect.ResponseGetRegistrarRoutes{},
	inspect.ResponseGetRegistrarProxyRoutes{},
	inspect.ResponseGetRegistrarApplicationRoutes{},
	ClusterInfo{},
}

// every view mirrors its framework type field for field
func TestWireMCPViewsMirrorTheDomain(t *testing.T) {
	if len(mcpViews) == 0 {
		t.Fatal("no view is registered")
	}
	for domain, view := range mcpViews {
		out := view(reflect.Zero(domain).Interface())
		mirrors(t, domain.Name(), domain, reflect.TypeOf(out))
	}
}

// every answer passes through a view
func TestEveryAnswerHasAView(t *testing.T) {
	if len(mcpAnswers) == 0 || len(lensSpecs) == 0 {
		t.Fatal("no answer to walk")
	}

	answers := append([]any{}, mcpAnswers...)
	for _, spec := range lensSpecs {
		answers = append(answers, spec.Sample)
	}

	for _, answer := range answers {
		typ := reflect.TypeOf(answer)
		if _, served := mcpViews[typ]; served == false {
			t.Errorf("%s reaches the agent with no view", typ)
		}
	}
}

func carriesIdentifier(typ reflect.Type, seen map[reflect.Type]bool) bool {
	if mcpIdentTypes[typ] {
		return true
	}
	if seen[typ] {
		return false
	}
	seen[typ] = true

	switch typ.Kind() {
	case reflect.Slice, reflect.Array, reflect.Ptr, reflect.Map:
		return carriesIdentifier(typ.Elem(), seen)
	case reflect.Struct:
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			if field.IsExported() && carriesIdentifier(field.Type, seen) {
				return true
			}
		}
	}
	return false
}

func mirrors(t *testing.T, path string, domain, view reflect.Type) {
	t.Helper()

	if domain.Kind() != reflect.Struct || view.Kind() != reflect.Struct {
		t.Errorf("%s: %s and %s are not both structs", path, domain, view)
		return
	}

	carried := map[string]reflect.StructField{}
	for i := 0; i < view.NumField(); i++ {
		field := view.Field(i)
		if strings.Split(field.Tag.Get("json"), ",")[0] == mcpLegendKey {
			continue
		}
		carried[field.Name] = field
	}

	for i := 0; i < domain.NumField(); i++ {
		field := domain.Field(i)
		if field.IsExported() == false {
			continue
		}
		mirror, present := carried[field.Name]
		if present == false {
			t.Errorf("%s.%s is in the framework type and not in the view", path, field.Name)
			continue
		}
		delete(carried, field.Name)
		mirrorField(t, path+"."+field.Name, field.Type, mirror.Type)
	}

	for name := range carried {
		t.Errorf("%s.%s is in the view and not in the framework type", path, name)
	}
}

var mirrorAsText = map[string]bool{"TraceID": true, "SpanID": true, "ParentSpanID": true}

var mirrorNarrows = map[string]string{
	"MessageInspectMeta.Info.MailboxQueues": "a meta has Main and System and no latency: " +
		"node/node.go fills those two and leaves the rest of gen.MailboxQueues at zero",
}

var mirrorAsName = map[reflect.Type]bool{
	reflect.TypeOf(gen.ProcessState(0)):     true,
	reflect.TypeOf(gen.MetaState(0)):        true,
	reflect.TypeOf(gen.LogLevel(0)):         true,
	reflect.TypeOf(gen.MessagePriority(0)):  true,
	reflect.TypeOf(gen.ApplicationMode(0)):  true,
	reflect.TypeOf(gen.ApplicationState(0)): true,
	reflect.TypeOf(gen.NetworkMode(0)):      true,
	reflect.TypeOf(gen.CompressionLevel(0)): true,
	reflect.TypeOf(gen.TracingPoint(0)):     true,
	reflect.TypeOf(gen.TracingKind(0)):      true,
}

var mirrorAsNames = map[reflect.Type]bool{
	reflect.TypeOf(gen.TracingFlags(0)): true,
}

func mirrorField(t *testing.T, path string, domain, view reflect.Type) {
	t.Helper()

	if _, narrowed := mirrorNarrows[path]; narrowed {
		if view == domain {
			t.Errorf("%s is declared narrower than %s and carries it whole", path, domain)
		}
		return
	}

	switch {
	case mcpIdentTypes[domain]:
		if view.Kind() != reflect.String {
			t.Errorf("%s: identifier %s must be carried as text, the view says %s",
				path, domain, view)
		}
	case mirrorAsName[domain]:
		if view.Kind() != reflect.String {
			t.Errorf("%s: enum %s must be carried as its name, the view says %s",
				path, domain, view)
		}
	case mirrorAsNames[domain]:
		if view.Kind() != reflect.Slice || view.Elem().Kind() != reflect.String {
			t.Errorf("%s: bitmask %s must be carried as names, the view says %s",
				path, domain, view)
		}
	case domain.Kind() == reflect.Slice && mirrorAsName[domain.Elem()]:
		if view.Kind() != reflect.Slice || view.Elem().Kind() != reflect.String {
			t.Errorf("%s: a list of %s must be carried as names, the view says %s",
				path, domain.Elem(), view)
		}
	case domain.Kind() == reflect.Slice && mcpIdentTypes[domain.Elem()]:
		if view.Kind() != reflect.Slice || view.Elem().Kind() != reflect.String {
			t.Errorf("%s: a list of %s must be carried as text, the view says %s",
				path, domain.Elem(), view)
		}
	case domain == view:

	case domain.Kind() == reflect.Interface && view.Kind() == reflect.String:

	case domain.Kind() == reflect.Struct && view.Kind() == reflect.Struct:
		mirrors(t, path, domain, view)
	case domain.Kind() == reflect.Slice && view.Kind() == reflect.Slice:
		mirrorField(t, path+"[]", domain.Elem(), view.Elem())
	case domain.Kind() == reflect.Map && view.Kind() == reflect.Map:
		if domain.Key() != view.Key() {
			t.Errorf("%s: the key is %s in the framework and %s in the view",
				path, domain.Key(), view.Key())
		}
		mirrorField(t, path+"[k]", domain.Elem(), view.Elem())
	case view.Kind() == reflect.String && mirrorAsText[path[strings.LastIndexByte(path, '.')+1:]]:

	default:
		t.Errorf("%s: the framework says %s and the view says %s", path, domain, view)
	}
}

// the process view carries the units and the sentinels
func TestWireMCPProcessCarriesTheLegend(t *testing.T) {
	out := wireMCPProcessOf(inspect.MessageInspectProcess{Info: gen.ProcessInfo{}})
	view, ok := out.(wireMCPProcess)
	if ok == false {
		t.Fatalf("the view answered %T", out)
	}

	legend := view.Info.Legend
	if legend == nil {
		t.Fatal("the process view carries no legend")
	}

	units, _ := legend["units"].(map[string]string)
	sentinels, _ := legend["sentinels"].(map[string]string)
	for field, unit := range map[string]string{
		"Uptime": "sec", "StateTime": "ns", "RunningTime": "ns", "InitTime": "ns",
	} {
		if units[field] != unit {
			t.Errorf("the legend says %s is %q, the framework says %q", field, units[field], unit)
		}
	}
	for _, field := range []string{"MailboxSize", "Env"} {
		if sentinels[field] == "" {
			t.Errorf("%s carries a sentinel in the framework and none in the legend", field)
		}
	}
}

// the meta view carries its identifiers as text
func TestWireMCPMetaCarriesTheLegend(t *testing.T) {
	view := wireMCPMeta{Info: wireMCPMetaInfoOf(gen.MetaInfo{})}

	units, _ := view.Info.Legend["units"].(map[string]string)
	if units["Uptime"] != "sec" {
		t.Errorf("the meta legend says Uptime is %q", units["Uptime"])
	}
	sentinels, _ := view.Info.Legend["sentinels"].(map[string]string)
	if strings.Contains(sentinels["MailboxSize"], "-1") == false {
		t.Errorf("the meta legend does not explain -1: %q", sentinels["MailboxSize"])
	}
}

func TestWireMCPProcessIdentifiersAreText(t *testing.T) {
	node := gen.Atom("n@h")
	pid := gen.PID{Node: node, ID: 42, Creation: 7}
	alias := gen.Alias{Node: node, ID: [3]uint64{1, 2, 3}, Creation: 7}

	out := wireMCPProcessOf(inspect.MessageInspectProcess{
		Node: node,
		Info: gen.ProcessInfo{
			PID: pid, Parent: pid, Leader: pid,
			Metas:             []gen.Alias{alias},
			MonitorsPID:       []gen.PID{pid},
			MonitorsProcessID: []gen.ProcessID{{Name: "worker", Node: node}},
			MonitorsEvent:     []gen.Event{{Name: "moved", Node: node}},
			Events:            []gen.Atom{"moved"},
		},
	})

	encoded, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range identifierObjects(t, encoded) {
		t.Errorf("an identifier reached the agent as an object at %s: %s", path, encoded)
	}

	view := out.(wireMCPProcess)
	for field, got := range map[string]string{
		"PID":               view.Info.PID,
		"Parent":            view.Info.Parent,
		"Metas":             view.Info.Metas[0],
		"MonitorsPID":       view.Info.MonitorsPID[0],
		"MonitorsProcessID": view.Info.MonitorsProcessID[0],
		"MonitorsEvent":     view.Info.MonitorsEvent[0],
	} {
		if strings.HasPrefix(got, string(node)+mcpIdentSep) == false {
			t.Errorf("%s does not name its node: %q", field, got)
		}
	}

	if view.Info.Events[0] != "moved" {
		t.Errorf("a registered event name changed: %q", view.Info.Events[0])
	}
}

// tracing ids reach the agent as zero-padded hex
func TestWireMCPTracingIDsAreHex(t *testing.T) {
	ours := wireMCPTracingSpanOf(gen.TracingSpan{
		TraceID:      [2]uint64{0x0102030405060708, 0x090a0b0c0d0e0f10},
		SpanID:       0xaabbccdd11223344,
		ParentSpanID: 0x1122334455667788,
	})

	for field, want := range map[string]string{
		ours.TraceID:      "0102030405060708090a0b0c0d0e0f10",
		ours.SpanID:       "aabbccdd11223344",
		ours.ParentSpanID: "1122334455667788",
	} {
		if field != want {
			t.Errorf("an id reads %q, want %q", field, want)
		}
	}

	bare := wireMCPTracingSpanOf(gen.TracingSpan{TraceID: [2]uint64{1, 2}, SpanID: 3})
	if bare.ParentSpanID != "" || bare.ParentPoint != "" {
		t.Errorf("a root span claims a parent: %q %q", bare.ParentSpanID, bare.ParentPoint)
	}
	if bare.TraceID != "00000000000000010000000000000002" {
		t.Errorf("a trace id is not padded: %q", bare.TraceID)
	}
}

func TestWireMCPBatchesAreViewed(t *testing.T) {
	pid := gen.PID{Node: "n@h", ID: 42, Creation: 7}
	batches := []any{
		inspect.MessageInspectLog{
			Node:    "n@h",
			Entries: []inspect.InspectLogEntry{{Name: "worker", PID: pid, Message: "up"}},
		},
		inspect.MessageInspectTracing{
			Node:  "n@h",
			Spans: []gen.TracingSpan{{From: pid, To: pid}},
		},
	}

	encoded, err := json.Marshal(mcpViewOf(batches))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range identifierObjects(t, encoded) {
		t.Errorf("a batch reached the agent unviewed, %s is an object: %s", path, encoded)
	}
	if strings.Contains(string(encoded), `n@h:42.7`) == false {
		t.Errorf("the identifier of a batch is not text: %s", encoded)
	}
}

func identifierObjects(t *testing.T, encoded []byte) []string {
	t.Helper()

	var tree any
	if err := json.Unmarshal(encoded, &tree); err != nil {
		t.Fatal(err)
	}

	found := []string{}
	var walk func(string, any)
	walk = func(path string, node any) {
		switch v := node.(type) {
		case map[string]any:
			_, node := v["Node"]
			_, creation := v["Creation"]
			_, name := v["Name"]
			if node && (creation || (name && len(v) == 2)) {
				found = append(found, path)
				return
			}
			for key, value := range v {
				walk(path+"."+key, value)
			}
		case []any:
			for i, value := range v {
				walk(fmt.Sprintf("%s[%d]", path, i), value)
			}
		}
	}
	walk("", tree)
	return found
}

func TestWireMCPLegendsSayWhatTheNumbersAre(t *testing.T) {
	for _, c := range []struct {
		what      string
		legend    map[string]any
		units     map[string]string
		sentinels []string
		axes      []string
	}{
		{
			what:   "node",
			legend: wireMCPNodeLegend,
			units: map[string]string{
				"Uptime": "sec", "MemoryUsed": "bytes", "MemoryLimit": "bytes",
				"HeapGoal": "bytes", "CPUTimeGC": "sec", "UserTime": "ns", "SystemTime": "ns",
			},
			sentinels: []string{"MemoryLimit", "Env"},
			axes:      []string{"LogMessages", "TracingSpans"},
		},
		{
			what:   "network",
			legend: wireMCPNetworkLegend,
			units: map[string]string{
				"MaxMessageSize": "bytes", "Acceptors[].MaxMessageSize": "bytes",
				"Flags.EnableSoftwareKeepAlive":             "sec",
				"Acceptors[].Flags.EnableSoftwareKeepAlive": "sec",
				"Routes[].Flags.EnableSoftwareKeepAlive":    "sec",
			},
			sentinels: []string{"MaxMessageSize", "Flags.EnableSoftwareKeepAlive"},
		},
		{
			what:   "connection",
			legend: wireMCPConnectionLegend,
			units: map[string]string{
				"Info.Uptime": "sec", "Info.ConnectionUptime": "sec",
				"Info.MaxMessageSize": "bytes", "Info.ClockSkew": "ns",
				"Info.NetworkFlags.EnableSoftwareKeepAlive": "sec",
			},
			sentinels: []string{"Info.ClockSkew", "Info.NetworkFlags.EnableSoftwareKeepAlive"},
		},
		{
			what:   "connections",
			legend: wireMCPConnectionListLegend,
			units: map[string]string{
				"Connections[].Uptime": "sec", "Connections[].ClockSkew": "ns",
				"Connections[].NetworkFlags.EnableSoftwareKeepAlive": "sec",
			},
			sentinels: []string{"Connections[].ClockSkew"},
		},
		{
			what:   "processes",
			legend: wireMCPProcessListLegend,
			units: map[string]string{
				"Processes[].Uptime": "sec", "Processes[].StateTime": "ns",
				"Processes[].MailboxLatency": "ns",
			},
			sentinels: []string{"Processes[].MailboxLatency"},
		},
		{
			what:   "heap profile",
			legend: wireMCPHeapProfileLegend,
			units:  map[string]string{"TotalInuse": "bytes", "TotalAlloc": "bytes"},
		},
		{
			what:      "types",
			legend:    wireMCPTypesLegend,
			units:     map[string]string{"Types[].MinSize": "bytes"},
			sentinels: []string{"Types[].Stats.Enabled"},
		},
		{
			what:   "process",
			legend: wireMCPProcessLegend,
			units: map[string]string{
				"Compression.Threshold": "bytes", "Uptime": "sec", "StateTime": "ns",
			},
			sentinels: []string{"MailboxSize", "Env"},
		},
	} {
		units, _ := c.legend["units"].(map[string]string)
		for field, want := range c.units {
			if units[field] != want {
				t.Errorf("%s: the legend says %s is %q, want %q", c.what, field, units[field], want)
			}
		}
		sentinels, _ := c.legend["sentinels"].(map[string]string)
		for _, field := range c.sentinels {
			if sentinels[field] == "" {
				t.Errorf("%s: %s carries no sentinel", c.what, field)
			}
		}
		axes, _ := c.legend["axes"].(map[string]string)
		for _, field := range c.axes {
			if axes[field] == "" {
				t.Errorf("%s: %s carries no axis", c.what, field)
			}
		}
	}
}

// an error has no exported field: whatever answer carries one raw reaches the agent as {}
func TestAnswersDoNotLeakARawError(t *testing.T) {
	failing := reflect.TypeOf((*error)(nil)).Elem()

	for _, answer := range mcpAnswers {
		domain := reflect.TypeOf(answer)
		carries := false
		for i := 0; i < domain.NumField(); i++ {
			if domain.Field(i).Type == failing {
				carries = true
			}
		}
		if carries == false {
			continue
		}

		out := mcpViewOf(answer)
		view := reflect.TypeOf(out)
		if view == domain {
			t.Errorf("%s carries an error and has no view, so a reason reaches the agent as {}",
				domain.Name())
			continue
		}
		for i := 0; i < view.NumField(); i++ {
			if view.Field(i).Name == "Error" && view.Field(i).Type.Kind() != reflect.String {
				t.Errorf("%s carries its error as %s", view.Name(), view.Field(i).Type)
			}
		}
	}
}

// an error has no exported field, so json renders it as {} and the agent learns nothing
func TestWireMCPErrorsCarryTheirReason(t *testing.T) {
	for what, out := range map[string]any{
		"node":         mcpViewOf(inspect.ResponseGetNode{Error: gen.ErrProcessUnknown}),
		"processes":    mcpViewOf(inspect.ResponseGetProcessList{Error: gen.ErrProcessUnknown}),
		"connection":   mcpViewOf(inspect.ResponseGetConnection{Error: gen.ErrProcessUnknown}),
		"types":        mcpViewOf(inspect.ResponseGetTypes{Error: gen.ErrProcessUnknown}),
		"errors":       mcpViewOf(inspect.ResponseGetErrors{Error: gen.ErrProcessUnknown}),
		"atoms":        mcpViewOf(inspect.ResponseGetAtoms{Error: gen.ErrProcessUnknown}),
		"heap":         mcpViewOf(inspect.ResponseGetHeapProfile{Error: gen.ErrProcessUnknown}),
		"applications": mcpViewOf(inspect.ResponseGetApplicationList{Error: gen.ErrProcessUnknown}),
		"routes": mcpViewOf(inspect.ResponseGetRegistrarApplicationRoutes{
			Error: gen.ErrProcessUnknown,
		}),
	} {
		encoded, err := json.Marshal(out)
		if err != nil {
			t.Fatalf("%s: %s", what, err)
		}
		if strings.Contains(string(encoded), gen.ErrProcessUnknown.Error()) == false {
			t.Errorf("%s lost the reason: %s", what, encoded)
		}
	}

	encoded, err := json.Marshal(mcpViewOf(inspect.ResponseGetNode{}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), `"Error"`) {
		t.Errorf("an answer with no error carries the field anyway: %s", encoded)
	}
}

func TestWireMCPViewsFillTheLegend(t *testing.T) {
	for domain, view := range mcpViews {
		out := view(reflect.Zero(domain).Interface())
		legendFilled(t, domain.Name(), reflect.ValueOf(out))
	}
}

func legendFilled(t *testing.T, path string, value reflect.Value) {
	t.Helper()

	if value.Kind() != reflect.Struct {
		return
	}
	typ := value.Type()
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.IsExported() == false {
			continue
		}
		if strings.Split(field.Tag.Get("json"), ",")[0] == mcpLegendKey {
			if value.Field(i).Len() == 0 {
				t.Errorf("%s.%s declares a legend and leaves it empty", path, field.Name)
			}
			continue
		}
		legendFilled(t, path+"."+field.Name, value.Field(i))
	}
}

// an identifier nobody set reads as absent, not as ".0.0", which looks like a real one
func TestWireMCPEmptyIdentifiersAreEmpty(t *testing.T) {
	for what, got := range map[string]string{
		"pid":        mcpPIDText(gen.PID{}),
		"ref":        mcpRefText(gen.Ref{}),
		"alias":      mcpRefText(gen.Ref(gen.Alias{})),
		"event":      mcpEventText(gen.Event{}),
		"process id": mcpProcessIDText(gen.ProcessID{}),
		"target":     mcpTargetText(nil),
	} {
		if got != "" {
			t.Errorf("an unset %s came back as %q", what, got)
		}
	}

	if got := mcpPIDText(gen.PID{Node: "n@h", ID: 1, Creation: 2}); got != "n@h:1.2" {
		t.Errorf("a pid came back as %q", got)
	}
}

// nil and empty are different answers, and the view mirrors which one the framework gave
func TestWireMCPTextListsKeepNil(t *testing.T) {
	if mcpPIDTexts(nil) != nil {
		t.Error("a nil list became a list")
	}
	if got := mcpPIDTexts([]gen.PID{}); got == nil || len(got) != 0 {
		t.Errorf("an empty list became %v", got)
	}
}

// no view leaves an identifier as a framework type
func TestNoViewCarriesAnIdentifier(t *testing.T) {
	if len(mcpViews) == 0 {
		t.Fatal("no view is registered")
	}

	for domain, view := range mcpViews {
		out := view(reflect.Zero(domain).Interface())
		typ := reflect.TypeOf(out)
		if typ.Kind() != reflect.Struct {
			continue
		}
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			if field.IsExported() == false {
				continue
			}
			if carriesIdentifier(field.Type, map[reflect.Type]bool{}) {
				t.Errorf("%s.%s carries an identifier the agent cannot hand back: %s",
					typ.Name(), field.Name, field.Type)
			}
		}
	}
}

var mcpBareByNature = map[string]bool{
	"wireMCPRemoteNodeInfo.PoolSize":         true, // connections in the pool, not bytes
	"wireMCPRemoteNodeInfo.FragmentTimeouts": true, // a counter of timeouts, not a duration
	"wireMCPCluster.Limit":                   true, // how many nodes are watched at most
	"wireMCPHeapRecord.InuseObjects":         true,
	"wireMCPHeapRecord.AllocObjects":         true,
	"wireMCPHeapRecord.FreeObjects":          true,
}

var mcpMeasureWords = []string{
	"Time", "Latency", "Period", "Uptime", "Interval", "Duration", "Timeout",
	"Size", "Bytes", "Alloc", "Limit", "Goal", "Inuse", "Threshold", "Skew", "Age",
}

func TestEveryMeasuredNumberSaysItsUnit(t *testing.T) {
	seen := map[reflect.Type]bool{}

	var walk func(reflect.Type)
	walk = func(typ reflect.Type) {
		typ = mcpLegendElem(typ)
		if typ.Kind() != reflect.Struct || seen[typ] {
			return
		}
		seen[typ] = true

		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			if field.IsExported() == false {
				continue
			}
			walk(field.Type)

			switch mcpLegendElem(field.Type).Kind() {
			case reflect.Int, reflect.Int32, reflect.Int64, reflect.Uint,
				reflect.Uint32, reflect.Uint64, reflect.Float64:
			default:
				continue
			}

			_, unit := field.Tag.Lookup("unit")
			_, sentinel := field.Tag.Lookup("sentinel")
			_, axis := field.Tag.Lookup("axis")
			where := typ.Name() + "." + field.Name
			if unit || sentinel || axis || mcpBareByNature[where] {
				continue
			}

			for _, word := range mcpMeasureWords {
				if strings.Contains(field.Name, word) == false {
					continue
				}
				if typ.PkgPath() == "ergo.services/application/observer" {
					t.Errorf("%s reads as a measure and carries no unit tag", where)
				} else {
					t.Errorf("%s is a framework field reading as a measure: it needs a mirror "+
						"of its own to carry the tag", where)
				}
				break
			}
		}
	}

	for domain, view := range mcpViews {
		walk(reflect.TypeOf(view(reflect.Zero(domain).Interface())))
	}
}
