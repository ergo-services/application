package observer

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"ergo.services/ergo/app/system/inspect"
	"ergo.services/ergo/gen"
)

// the stream producer terminates and names the reason, so a lens over an event that is not
// there is refused instead of answering an empty list
func TestAbsenceReadsTheStreamError(t *testing.T) {
	uri, err := parseURI("ergo://n@h/stream/nosuch")
	if err != nil {
		t.Fatal(err)
	}

	why, absent := mcpAbsent(inspect.ResponseInspectEventStream{
		Error: errors.New("unknown event"),
	}, uri)
	if absent == false {
		t.Fatal("a stream that cannot be followed reads as present")
	}
	if strings.Contains(why, "unknown event") == false || strings.Contains(why, "n@h") == false {
		t.Errorf("the reason names neither the node nor the cause: %q", why)
	}

	if _, absent := mcpAbsent(inspect.ResponseInspectEventStream{
		WatchReason: "idle_gated",
	}, uri); absent {
		t.Error("a quiet stream reads as absent")
	}
}

// the same absence arrives twice on two paths, and both have to name it the same way
func TestAbsenceSaysTheSameThingOnBothPaths(t *testing.T) {
	uri, err := parseURI("ergo://n@h/connection/peer@h")
	if err != nil {
		t.Fatal(err)
	}

	raised, ok := mcpAbsent(inspect.ResponseInspectConnection{Disconnected: true}, uri)
	if ok == false {
		t.Fatal("a connection that is not there reads as present while it is raised")
	}
	read, ok := mcpAbsent(inspect.MessageInspectConnection{Disconnected: true}, uri)
	if ok == false {
		t.Fatal("a connection that is not there reads as present while it is read")
	}
	if raised != read {
		t.Errorf("one absence, two sentences:\n  raised: %q\n  read:   %q", raised, read)
	}
	if strings.Contains(read, "peer@h") == false || strings.Contains(read, "n@h") == false {
		t.Errorf("the reason names neither the node nor the peer: %q", read)
	}

	// what is there is not absent, whichever path asks
	for _, present := range []any{
		inspect.ResponseInspectConnection{},
		inspect.MessageInspectConnection{},
		inspect.MessageInspectProcess{},
		inspect.MessageInspectMeta{},
		inspect.MessageInspectEvent{},
	} {
		if why, absent := mcpAbsent(present, uri); absent {
			t.Errorf("%T reads as absent: %q", present, why)
		}
	}
}

func TestEverythingRendersAsWhatTheTemplatesAnnounce(t *testing.T) {
	templates := mcpResourceTemplates(Ceiling{})
	if len(templates) != len(lensSpecs)+2 {
		t.Fatalf("%d templates for %d lenses", len(templates), len(lensSpecs))
	}
	for _, template := range templates {
		if template.MimeType != mcpMimeJSON {
			t.Errorf("%s announces %s", template.Name, template.MimeType)
		}
	}

	for _, spec := range lensSpecs {
		reading := ownerReadResponse{Value: spec.Sample}
		if lensAccumulates[lensOf(spec.Lens)] {
			reading = ownerReadResponse{Batches: []any{spec.Sample}}
		}

		rendered, err := mcpRender(mcpRenderReading(reading))
		if err != nil {
			t.Fatalf("%s: %s", spec.Lens, err)
		}
		if rendered.MimeType != mcpMimeJSON {
			t.Errorf("%s renders %s", spec.Lens, rendered.MimeType)
		}
		if json.Valid([]byte(rendered.Text)) == false {
			t.Errorf("%s renders something that is not json: %q", spec.Lens, rendered.Text)
		}
	}

	run, err := mcpRender(mcpRenderReading(ownerReadResponse{Value: jobReading{}}))
	if err != nil {
		t.Fatal(err)
	}
	if run.MimeType != mcpMimeJSON {
		t.Errorf("a run renders %s", run.MimeType)
	}
}

func TestRenderCarriesTheLegend(t *testing.T) {
	value := inspect.MessageInspectProcessList{
		Node:      "n@h",
		Processes: []gen.ProcessShortInfo{{PID: gen.PID{Node: "n@h", ID: 1, Creation: 2}}},
	}

	rendered, err := mcpRender(value)
	if err != nil {
		t.Fatal(err)
	}

	var said map[string]any
	if err := json.Unmarshal([]byte(rendered.Text), &said); err != nil {
		t.Fatalf("%q: %s", rendered.Text, err)
	}
	if said["Node"] != "n@h" {
		t.Errorf("the node did not survive: %v", said)
	}

	legend, carried := said[mcpLegendKey].(map[string]any)
	if carried == false {
		t.Fatalf("the answer carries no legend: %v", said)
	}

	units, _ := legend["units"].(map[string]any)
	if units["Processes[].Uptime"] != "sec" || units["Processes[].StateTime"] != "ns" {
		t.Errorf("the two durations are not told apart: %v", units)
	}

	sentinels, _ := legend["sentinels"].(map[string]any)
	if note, named := sentinels["Processes[].MailboxLatency"]; named == false {
		t.Errorf("a latency of -1 is unexplained: %v", sentinels)
	} else if strings.Contains(note.(string), "latency") == false {
		t.Errorf("the note reads %v", note)
	}

	rows, _ := said["Processes"].([]any)
	if len(rows) != 1 {
		t.Fatalf("the listing came back as %v", said["Processes"])
	}
	row, _ := rows[0].(map[string]any)
	if row["PID"] != "n@h:1.2" {
		t.Errorf("the pid reads %v", row["PID"])
	}
}

func TestRenderLeavesOutAnErrorThatIsNotThere(t *testing.T) {
	value := inspect.ResponseGetProcessRange{
		Node:      "n@h",
		Processes: []gen.ProcessShortInfo{{PID: gen.PID{Node: "n@h", ID: 1, Creation: 2}}},
	}

	rendered, err := mcpRender(value)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rendered.Text, "Error") {
		t.Errorf("a nil error was sent: %s", rendered.Text)
	}

	failed := value
	failed.Error = gen.ErrProcessUnknown
	rendered, err = mcpRender(failed)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rendered.Text, gen.ErrProcessUnknown.Error()) == false {
		t.Errorf("a real error was dropped: %s", rendered.Text)
	}
}

func TestRenderOmitsAnEmptyLegend(t *testing.T) {
	rendered, err := mcpRender(inspect.MessageInspectEvent{
		Node:    "n@h",
		Entries: []inspect.InspectEventEntry{{Message: "x"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rendered.Text, mcpLegendKey) {
		t.Errorf("an empty legend was sent: %s", rendered.Text)
	}
}
