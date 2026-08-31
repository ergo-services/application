package observer

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	"ergo.services/ergo/gen"
)

func TestClusterURIIsObserverLevel(t *testing.T) {
	uri, err := parseURI(clusterURI)
	if err != nil {
		t.Fatalf("%s: %s", clusterURI, err)
	}
	if uri.Node != "" || uri.Lens != uriWordCluster || uri.Key != "" || uri.Target != "" {
		t.Errorf("parsed as %#v", uri)
	}
	if uri.Canonical() != clusterURI {
		t.Errorf("canonical is %q", uri.Canonical())
	}

	for _, raw := range []string{"ergo://cluster/nodes", "ergo://cluster/watch/k", "ergo://node@host/cluster"} {
		if parsed, err := parseURI(raw); err == nil {
			t.Errorf("%s was accepted as %#v", raw, parsed)
		}
	}
}

func clusterMap() ClusterInfo {
	return ClusterInfo{
		Nodes: []ClusterNodeInfo{
			{Info: gen.NodeShortInfo{Name: "a@host"}},
			{Info: gen.NodeShortInfo{Name: "b@host"}},
		},
		Offline: []OfflineNode{
			{Node: "c@host", Peers: []gen.Atom{"a@host", "d@host"}},
			{Node: "d@host"},
		},
		Complete: true,
		Watched:  2,
		Limit:    10,
	}
}

func TestClusterNarrowedByCeiling(t *testing.T) {
	narrowed := narrowCluster(clusterMap(), Ceiling{Nodes: []string{"a@host"}})

	if len(narrowed.Nodes) != 1 || narrowed.Nodes[0].Info.Name != "a@host" {
		t.Errorf("nodes came out as %#v", narrowed.Nodes)
	}
	if len(narrowed.Offline) != 0 {
		t.Errorf("offline came out as %#v", narrowed.Offline)
	}
	if narrowed.Complete {
		t.Error("a narrowed map still claims to be complete")
	}
	if narrowed.Watched != 1 {
		t.Errorf("watched came out as %d", narrowed.Watched)
	}
}

func TestClusterNarrowsPeers(t *testing.T) {
	narrowed := narrowCluster(clusterMap(), Ceiling{Nodes: []string{"a@host", "c@host"}})

	if len(narrowed.Offline) != 1 || narrowed.Offline[0].Node != "c@host" {
		t.Fatalf("offline came out as %#v", narrowed.Offline)
	}
	peers := narrowed.Offline[0].Peers
	if len(peers) != 1 || peers[0] != "a@host" {
		t.Errorf("peers came out as %#v", peers)
	}
}

func TestClusterUnsetCeilingKeepsEverything(t *testing.T) {
	source := clusterMap()
	kept := narrowCluster(source, Ceiling{})

	if len(kept.Nodes) != 2 || len(kept.Offline) != 2 || kept.Complete == false {
		t.Errorf("the map was narrowed without a node list: %#v", kept)
	}
	if len(source.Offline[0].Peers) != 2 {
		t.Error("the source map was modified in place")
	}
}

// a present but empty list permits no node: the answer is empty, not unfiltered
func TestClusterEmptyCeilingHidesEverything(t *testing.T) {
	narrowed := narrowCluster(clusterMap(), Ceiling{Nodes: []string{}})

	if len(narrowed.Nodes) != 0 || len(narrowed.Offline) != 0 {
		t.Errorf("an empty node list showed %#v", narrowed)
	}
}

func TestClusterReadsLikeALens(t *testing.T) {
	info := clusterMap()
	info.Nodes[0].Info.Mode = gen.NetworkModeHidden
	info.Nodes[0].Info.LogLevel = gen.LogLevelWarning
	info.Nodes[0].Info.MemoryLimit = math.MaxInt64
	info.WatchPeriod = 3 * time.Second

	rendered, err := mcpRender(info)
	if err != nil {
		t.Fatal(err)
	}

	var out map[string]any
	if err := json.Unmarshal([]byte(rendered.Text), &out); err != nil {
		t.Fatal(err)
	}

	nodes, _ := out["Nodes"].([]any)
	if len(nodes) != 2 {
		t.Fatalf("the map came back as %s", rendered.Text)
	}
	first, _ := nodes[0].(map[string]any)
	held, _ := first["Info"].(map[string]any)
	if held["Mode"] != "hidden" || held["LogLevel"] != "warning" {
		t.Errorf("the enums came back as %v / %v", held["Mode"], held["LogLevel"])
	}

	legend, _ := out[mcpLegendKey].(map[string]any)
	units, _ := legend["units"].(map[string]any)
	sentinels, _ := legend["sentinels"].(map[string]any)
	axes, _ := legend["axes"].(map[string]any)
	if units["Nodes[].Info.MemoryUsed"] != "bytes" || units["WatchPeriod"] != "ns" {
		t.Errorf("the legend says %v", units)
	}
	if sentinels["Nodes[].Info.MemoryLimit"] == nil {
		t.Errorf("nothing says what %v means", held["MemoryLimit"])
	}
	if axes["Nodes[].Info.LogMessages"] == "" {
		t.Errorf("the log counters have no axis: %v", axes)
	}
}
