package observer

import (
	"strings"

	"ergo.services/ergo/app/system/inspect"
)

type lensSpec struct {
	Lens         string
	Sample       any      // a zero value of what the lens publishes, for the mime type
	Target       string   // the uri template expression after the lens, empty when it takes none
	Params       []string // the query parameters it reads, in the order announced
	Capabilities []string // one permitted way is enough to announce it
	Title        string
	Description  string
}

var lensSpecs = []lensSpec{
	{
		Lens:         "node",
		Sample:       inspect.MessageInspectNode{},
		Capabilities: []string{inspect.CapNode},
		Title:        "Node",
		Description: "What the node is right now: uptime, memory, how many processes, " +
			"applications and connections it holds, its version and its environment. The " +
			"first thing to read about one node.",
	},
	{
		Lens:   "processes",
		Sample: inspect.MessageInspectProcessList{},
		Params: []string{
			"namePattern", "behavior", "application", "state", "minMailbox",
			"pidStart", "pidLimit",
		},
		Capabilities: []string{inspect.CapProcessList, inspect.CapProcessRange},
		Title:        "Processes",
		Description: "Every process on the node: what it is, what it runs under, how much it " +
			"has handled and what it is doing now. pidStart and pidLimit walk the id space in " +
			"order, which makes a page repeatable; pidLimit of -1 asks instead for whatever is " +
			"alive now, in no particular order and at the cost of the living rather than of " +
			"every id the node has ever used.",
	},
	{
		Lens:         "process",
		Sample:       inspect.MessageInspectProcess{},
		Target:       "{pid}",
		Capabilities: []string{inspect.CapProcess},
		Title:        "One process",
		Description: "The whole state of one process: mailbox, links, monitors, aliases, " +
			"environment and the meta processes it spawned. The target is a pid as any answer " +
			"writes it, node@host:ID.Creation, or just ID.Creation since the node is already " +
			"in the uri. Info.Metas is where the alias of a meta comes from.",
	},
	{
		Lens:         "meta",
		Sample:       inspect.MessageInspectMeta{},
		Target:       "{alias}",
		Capabilities: []string{inspect.CapMeta},
		Title:        "One meta process",
		Description: "The state of one meta process: a socket, a listener, a stream. The " +
			"target is the alias as the process lens writes it under Info.Metas, or just " +
			"ID.ID.ID.Creation since the node is already in the uri. A meta is in no listing " +
			"of its own.",
	},
	{
		Lens:         "network",
		Sample:       inspect.MessageInspectNetwork{},
		Capabilities: []string{inspect.CapNetwork},
		Title:        "Network",
		Description: "The network of the node as it is configured and as it is running: the " +
			"acceptors, the cookie policy, the flags, the registrar it uses and whether the " +
			"whole stack is stopped.",
	},
	{
		Lens:         "connections",
		Sample:       inspect.MessageInspectConnectionList{},
		Params:       []string{"limit", "namePattern"},
		Capabilities: []string{inspect.CapConnectionList},
		Title:        "Connections",
		Description: "Every peer this node is connected to, with what was negotiated with each " +
			"one and how much has crossed it.",
	},
	{
		Lens:         "connection",
		Sample:       inspect.MessageInspectConnection{},
		Target:       "{+peer}",
		Capabilities: []string{inspect.CapConnection},
		Title:        "One connection",
		Description: "One peer in full: the flags both sides agreed on, the pool behind the " +
			"connection, the bytes and messages each way, and whether it has since dropped. " +
			"The target is the name of the remote node.",
	},
	{
		Lens:   "applications",
		Sample: inspect.MessageInspectApplicationList{},
		Params: []string{},
		Capabilities: []string{
			inspect.CapApplicationList,
		},
		Title: "Applications",
		Description: "The applications loaded on the node, running or not, with their mode, " +
			"their weight, the roles they publish and how many processes each one holds.",
	},
	{
		Lens:   "events",
		Sample: inspect.MessageInspectEventList{},
		Params: []string{
			"namePattern", "limit", "timestamp", "notifyMode", "bufferedMode", "openMode",
			"minSubscribers",
		},
		Capabilities: []string{inspect.CapEventList},
		Title:        "Events",
		Description: "Every event registered on the node: who produces it, how many are " +
			"subscribed, whether it buffers and whether anyone may publish to it. The three " +
			"modes each take yes or no and nothing else: notifyMode keeps the events whose " +
			"producer asked to be told about subscribers, bufferedMode those that keep a " +
			"buffer, openMode those anyone may publish to.",
	},
	{
		Lens:         "event",
		Sample:       inspect.MessageInspectEvent{},
		Target:       "{name}",
		Capabilities: []string{inspect.CapEvent},
		Title:        "One event",
		Description: "What one event is, not what flows through it: its producer, its buffer, " +
			"how many are subscribed and when it last published, sampled while anybody " +
			"reads. The target is the registered name of the event. The node is asked about " +
			"the event rather than subscribed to it, so this costs the producer nothing and " +
			"carries no messages. The messages themselves are the stream lens, which does " +
			"subscribe.",
	},
	{
		Lens:   "stream",
		Sample: inspect.MessageInspectEvent{},
		Target: "{name}",
		Params: []string{
			"limit", "typePattern", "messagePattern", "messageExclude", "force", "verbose",
			"since",
		},
		Capabilities: []string{inspect.CapEventStream},
		Title:        "Event stream",
		Description: "The messages flowing through one event, as they arrive rather than as a " +
			"state. Read it again with since= to take only what has landed since; a reading " +
			"that says dropped means the node moved on while nobody was reading.",
	},
	{
		Lens:         "log",
		Sample:       inspect.MessageInspectLog{},
		Params:       []string{"levels", "limit", "messagePattern", "messageExclude", "since"},
		Capabilities: []string{inspect.CapLog},
		Title:        "Log",
		Description: "The lines the node logs, from the moment this lens is first read. Not " +
			"a history: nothing is kept before somebody asks. levels is a comma separated " +
			"list of debug, info, warning, error and panic. Read it again with since= to take " +
			"only the new lines.",
	},
	{
		Lens:   "tracing",
		Sample: inspect.MessageInspectTracing{},
		Params: []string{
			"limit", "kinds", "points", "messagePattern", "messageExclude", "since",
		},
		Capabilities: []string{inspect.CapTracing},
		Title:        "Tracing",
		Description: "The spans the node emits while tracing is on, accumulating the same way " +
			"the log does. Sampling makes the node do work it was not doing, so this is a " +
			"lens to turn on deliberately and read with since=. kinds and points are bit " +
			"sums: kinds adds 1 send, 2 request, 4 response, 8 spawn, 16 terminate; points " +
			"adds 1 sent, 2 delivered, 4 processed, 8 span. Zero means every one of them.",
	},
}

var lensSpecIndex = buildLensSpecIndex()

func buildLensSpecIndex() map[string]lensSpec {
	index := make(map[string]lensSpec, len(lensSpecs))
	for _, spec := range lensSpecs {
		index[spec.Lens] = spec
	}
	return index
}

func lensSpecOf(lens string) (lensSpec, bool) {
	spec, known := lensSpecIndex[lens]
	return spec, known
}

func mcpResourceTemplates(ceiling Ceiling) []mcpResourceTemplate {
	out := make([]mcpResourceTemplate, 0, len(lensSpecs)+2)

	for _, spec := range lensSpecs {
		if ceilingAllowsAny(ceiling, spec.Capabilities) == false {
			continue
		}
		out = append(out, mcpResourceTemplate{
			URITemplate: spec.uriTemplate(),
			Name:        spec.Lens,
			Title:       spec.Title,
			MimeType:    mcpMimeJSON,
			Description: spec.Description,
		})
	}

	out = append(out,
		mcpResourceTemplate{
			URITemplate: mcpScheme + "{+node}/watch/{key}/{lens}",
			Name:        "watch",
			Title:       "A named reading",
			MimeType:    mcpMimeJSON,
			Description: "Any lens read under a name of your own. The name holds the reading " +
				"and its cursor, so a later read with the same key continues where the last " +
				"one stopped instead of starting a second subscription on the node. The lens " +
				"is one of the names above, and a target and a query follow it as they do " +
				"without a key.",
		},
		mcpResourceTemplate{
			URITemplate: mcpScheme + uriWordJob + "/{key}",
			Name:        uriWordJob,
			Title:       jobTitle,
			MimeType:    mcpMimeJSON,
			Description: "One run started by cluster_query or cluster_batch, under the key you " +
				"gave it. Read it for what has answered so far, and again with since= for " +
				"only what has landed since. job_list names the runs this observer still holds.",
		},
	)
	return out
}

func ceilingAllowsAny(ceiling Ceiling, capabilities []string) bool {
	if len(capabilities) == 0 {
		return true
	}
	for _, capability := range capabilities {
		if ceiling.Allows(capability) {
			return true
		}
	}
	return false
}

func (s lensSpec) uriTemplate() string {
	out := &strings.Builder{}
	out.WriteString(mcpScheme)
	out.WriteString("{+node}/")
	out.WriteString(s.Lens)
	if s.Target != "" {
		out.WriteByte('/')
		out.WriteString(s.Target)
	}
	if len(s.Params) > 0 {
		out.WriteString("{?")
		out.WriteString(strings.Join(s.Params, ","))
		out.WriteByte('}')
	}
	return out.String()
}
