package observer

import "ergo.services/ergo/app/system/inspect"

var mcpTools = []mcpTool{
	{
		Name:  "processes",
		Title: "Processes",
		Build: buildProcesses,
		Lens:  "processes",
		Description: "Every process on the node: what it is, what it runs under, how much it " +
			"has handled and what it is doing now. The entry point into anything about one " +
			"node: narrow here, then ask process_state about a row. The answer holds what " +
			"the arguments asked for; the linked resource ergo://<node>/processes is the " +
			"listing itself, to read again or to keep under a name of your own.",
		Schema: mcpSchema{
			Type:     "object",
			Required: []string{nodeArgument},
			Properties: map[string]mcpSchemaItem{
				nodeArgument: nodeProperty(),
				"app": {Type: "string",
					Description: "Keep only processes of applications whose name holds this."},
				"name": {Type: "string",
					Description: "Keep only processes whose registered name holds this."},
				"behavior": {Type: "string",
					Description: "Keep only processes whose behavior type holds this."},
				"state": {Type: "string",
					Description: "Keep only processes in this state.",
					Enum:        processStateNames},
				"minMailbox": {Type: "integer",
					Description: "Keep only processes holding at least this many messages."},
				"limit": {Type: "integer",
					Description: "Maximum processes to return. Default 100, and a node may " +
						"hold thousands: Truncated in the answer says whether anything " +
						"matched beyond the page."},
				"scan": {Type: "string",
					Description: "all returns whatever matches, in no particular order, and " +
						"costs only the processes alive now: the default, and what an " +
						"overview wants. ordered returns them by process id, which is what " +
						"makes a page repeatable and lets start walk the node piece by " +
						"piece, and it costs more, because it walks every id the node has " +
						"ever used.",
					Enum: []string{"all", "ordered"}},
				"start": {Type: "integer",
					Description: "Where an ordered scan begins: a process id to walk forward " +
						"from, or a negative number to walk back from the newest. Ignored " +
						"when scan is all."},
			},
		},
	},
	{
		Name:         "process_state",
		Title:        "State of one process",
		Action:       "inspect",
		Capabilities: []string{inspect.CapGetProcessState},
		Description: "The full state of one process: mailbox, links, monitors, aliases, " +
			"environment. Use it after the processes tool has narrowed the search.",
		Schema: mcpSchema{
			Type:     "object",
			Required: []string{nodeArgument, "pid"},
			Properties: map[string]mcpSchemaItem{
				nodeArgument: nodeProperty(),
				"pid":        pidProperty("Process id as it appears in a listing."),
				"items": {
					Type: "array",
					Description: "Sections to return. Empty returns all of them, and " +
						"[\"help\"] asks the process which sections it has.",
					Items: &mcpItem{Type: "string"},
				},
			},
		},
	},
	{
		Name:         "meta_state",
		Title:        "State of one meta process",
		Action:       "meta_inspect",
		Capabilities: []string{inspect.CapGetMetaState},
		Description: "The state of one meta process: a socket, a listener, a stream. A meta " +
			"has no mailbox and no name of its own and is not in any listing of its own. It " +
			"is listed by the process that spawned it, under Info.Metas of the resource " +
			"ergo://<node>/process/<id>.<creation>. Read that resource to get the alias, then " +
			"ask here.",
		Schema: mcpSchema{
			Type:     "object",
			Required: []string{nodeArgument, "alias"},
			Properties: map[string]mcpSchemaItem{
				nodeArgument: nodeProperty(),
				"alias":      aliasProperty("Meta alias as its owning process reports it."),
				"items": {
					Type: "array",
					Description: "Sections to return. Empty returns all of them, and " +
						"[\"help\"] asks the meta which sections it has.",
					Items: &mcpItem{Type: "string"},
				},
			},
		},
	},
	{
		Name:   "node",
		Title:  "Node",
		Action: "node",
		Lens:   "node",
		Description: "What the node is right now: uptime, memory, how many processes, " +
			"applications and connections it holds, its version and its environment. The " +
			"cheapest question about one node, and the one to ask before any other.",
		Schema: mcpSchema{
			Type:       "object",
			Required:   []string{nodeArgument},
			Properties: map[string]mcpSchemaItem{nodeArgument: nodeProperty()},
		},
	},
	{
		Name:   "network",
		Title:  "Network",
		Action: "network",
		Lens:   "network",
		Description: "The network of the node as configured and as running: the acceptors it " +
			"listens on, the flags, the registrar it uses, and whether the stack is stopped " +
			"at all. Read it when a node is up and unreachable.",
		Schema: mcpSchema{
			Type:       "object",
			Required:   []string{nodeArgument},
			Properties: map[string]mcpSchemaItem{nodeArgument: nodeProperty()},
		},
	},
	{
		Name:   "connections",
		Title:  "Connections",
		Action: "connections",
		Lens:   "connections",
		Description: "Every peer this node is connected to, with what was negotiated with " +
			"each one and how much has crossed it. A peer on the cluster map that is missing " +
			"here is a peer this node is not talking to.",
		Schema: mcpSchema{
			Type:     "object",
			Required: []string{nodeArgument},
			Properties: map[string]mcpSchemaItem{
				nodeArgument: nodeProperty(),
				"peer": {Type: "string",
					Description: "Keep only peers whose name holds this."},
				"limit": {Type: "integer",
					Description: "Maximum peers to return."},
			},
		},
	},
	{
		Name:       "connection",
		Title:      "One connection",
		Action:     "connection",
		Lens:       "connection",
		LensTarget: "peer",
		Description: "One peer in full: the flags both sides agreed on, the pool behind the " +
			"connection, the bytes and messages each way, and whether it has since dropped.",
		Schema: mcpSchema{
			Type:     "object",
			Required: []string{nodeArgument, "peer"},
			Properties: map[string]mcpSchemaItem{
				nodeArgument: nodeProperty(),
				"peer": {Type: "string",
					Description: "Name of the remote node, as ergo://cluster spells it."},
			},
		},
	},
	{
		Name:   "applications",
		Title:  "Applications",
		Action: "applications",
		Lens:   "applications",
		Description: "The applications on the node, running or merely loaded, with their " +
			"mode, their weight, the roles they publish and how many processes each one " +
			"holds. An application loaded and never started is not a failure, and this is " +
			"where the difference shows.",
		Schema: mcpSchema{
			Type:       "object",
			Required:   []string{nodeArgument},
			Properties: map[string]mcpSchemaItem{nodeArgument: nodeProperty()},
		},
	},
	{
		Name:   "events",
		Title:  "Events",
		Action: "events",
		Lens:   "events",
		Description: "Every event registered on the node: who produces it, how many are " +
			"subscribed, whether it buffers and whether anyone may publish to it. What has " +
			"actually been published is not here: follow the resource " +
			"ergo://<node>/stream/<name> for that.",
		Schema: mcpSchema{
			Type:     "object",
			Required: []string{nodeArgument},
			Properties: map[string]mcpSchemaItem{
				nodeArgument: nodeProperty(),
				"name": {Type: "string",
					Description: "Keep only events whose name holds this."},
				"limit": {Type: "integer",
					Description: "Maximum events to return."},
				"newestFirst": {Type: "boolean",
					Description: "Return the most recently created first instead of the oldest."},
				"notify": {Type: "boolean",
					Description: "Keep only the events whose producer asked to be told about " +
						"subscribers, or only those that did not."},
				"buffered": {Type: "boolean",
					Description: "Keep only the events that keep a buffer, or only those that " +
						"do not."},
				"open": {Type: "boolean",
					Description: "Keep only the events anyone may publish to, or only those " +
						"only the producer may."},
				"minSubscribers": {Type: "integer",
					Description: "Keep only events with at least this many subscribers."},
			},
		},
	},
	{
		Name:       "event",
		Title:      "One event",
		Action:     "event",
		Lens:       "event",
		LensTarget: "name",
		Description: "One event: its producer, its buffer, how many are subscribed and when " +
			"it last published. A producer that has stopped shows as a stale last " +
			"publication rather than as an error.",
		Schema: mcpSchema{
			Type:     "object",
			Required: []string{nodeArgument, "name"},
			Properties: map[string]mcpSchemaItem{
				nodeArgument: nodeProperty(),
				"name":       {Type: "string", Description: "Registered name of the event."},
			},
		},
	},
	{
		Name:         "process_lookup",
		Title:        "Find a process by name",
		Action:       "lookup",
		Capabilities: []string{inspect.CapGetProcessLookup},
		Description: "Whether a process is alive and what it is now, by registered name or " +
			"by id. Cheaper than a listing when the name is already known.",
		Schema: mcpSchema{
			Type:     "object",
			Required: []string{nodeArgument},
			Properties: map[string]mcpSchemaItem{
				nodeArgument: nodeProperty(),
				"name":       {Type: "string", Description: "Registered process name."},
				"pid":        pidProperty("Process id, when the name is unknown."),
			},
		},
	},
	{
		Name:         "app_tree",
		Title:        "Processes of an application",
		Action:       "app_tree",
		Capabilities: []string{inspect.CapGetAppTree},
		Description: "Every process running under one application. The application has to be " +
			"named and has to be running: read the resource ergo://<node>/applications for " +
			"the names and their state. There is no node-wide form: ask the processes tool " +
			"for that, whose app column says which application each process belongs to.",
		Schema: mcpSchema{
			Type:     "object",
			Required: []string{nodeArgument, "app"},
			Properties: map[string]mcpSchemaItem{
				nodeArgument: nodeProperty(),
				"app":        {Type: "string", Description: "Application name."},
				"limit": {Type: "integer",
					Description: "Maximum processes to return. Default 1000. Truncated in " +
						"the answer says how many were left out."},
			},
		},
	},
	{
		Name:         "subtree",
		Title:        "Processes under one",
		Action:       "subtree",
		Capabilities: []string{inspect.CapGetSubtree},
		Description: "The processes below one supervisor. Use it to see what a supervisor is " +
			"actually running, as opposed to what its spec says.",
		Schema: mcpSchema{
			Type:     "object",
			Required: []string{nodeArgument, "pid"},
			Properties: map[string]mcpSchemaItem{
				nodeArgument: nodeProperty(),
				"pid":        pidProperty("Supervisor process id."),
				"limit":      {Type: "integer", Description: "Maximum processes to return."},
			},
		},
	},
	{
		Name:         "types",
		Title:        "Registered message types",
		Build:        buildTypes,
		Capabilities: []string{inspect.CapGetTypes},
		Description: "The message types registered for the network on that node. A type " +
			"missing here is a type that cannot cross a node boundary. A node registers " +
			"hundreds of them, most belonging to the framework, so ask by name when the " +
			"question is about one type: the whole list is a large answer to a small question.",
		Schema: mcpSchema{
			Type:     "object",
			Required: []string{nodeArgument},
			Properties: map[string]mcpSchemaItem{
				nodeArgument: nodeProperty(),
				"name": {Type: "string",
					Description: "Keep only types whose registered name holds this."},
				"kind": {Type: "string",
					Description: "Keep only types of this kind. struct is what a message " +
						"usually is; the rest name a single value, and framework marks the " +
						"types the framework registers for itself."},
				"limit": {Type: "integer",
					Description: "Maximum types to return, in registration order. Default 50, " +
						"and a node registers hundreds: Truncated says how many were left out."},
			},
		},
	},
	{
		Name:         "goroutines",
		Title:        "Goroutine dump",
		Action:       "goroutines",
		Capabilities: []string{inspect.CapGetGoroutines},
		Description: "A dump of the goroutines of that node, optionally filtered. Every dump " +
			"stops the world for as long as it takes to walk them, so ask once and read carefully.",
		Schema: mcpSchema{
			Type:     "object",
			Required: []string{nodeArgument},
			Properties: map[string]mcpSchemaItem{
				nodeArgument: nodeProperty(),
				"stack":      {Type: "string", Description: "Keep only stacks holding this text."},
				"state":      {Type: "string", Description: "Keep only goroutines in this state."},
				"minWait": {Type: "integer",
					Description: "Keep only goroutines blocked at least this many seconds. " +
						"The node reports a wait in whole minutes and only once it reaches " +
						"one, so anything under 60 keeps the same set as 60 does, and any " +
						"value above zero drops every goroutine that is running rather " +
						"than waiting."},
			},
		},
	},
	{
		Name:         "heap_profile",
		Title:        "Heap profile",
		Build:        buildHeapProfile,
		Capabilities: []string{inspect.CapGetHeapProfile},
		Description: "The heap profile of that node: what is allocated and by which call " +
			"path. Costs a pause, like the goroutine dump. Entries come largest first and " +
			"the totals are of the whole profile, not of the page, so a few entries already " +
			"say where the memory is.",
		Schema: mcpSchema{
			Type:     "object",
			Required: []string{nodeArgument},
			Properties: map[string]mcpSchemaItem{
				nodeArgument: nodeProperty(),
				"minBytes":   {Type: "integer", Description: "Skip entries smaller than this."},
				"limit": {Type: "integer",
					Description: "Maximum entries to return, largest first. Default 25, " +
						"which on a live node is most of the heap in a fraction of the " +
						"answer; Truncated says how many were left out."},
			},
		},
	},
	{
		Name:   "cluster_query",
		Title:  "One question to many nodes",
		Fanout: true,
		Description: "Ask one tool of many nodes at once, in parallel. Answers immediately with " +
			"the URI of the run: read it, and read it again with since= to take only the answers " +
			"that landed since. Every answer is summarised, so ask a single node directly when " +
			"one of them needs a closer look.",
		Schema: mcpSchema{
			Type:     "object",
			Required: []string{"key", "tool"},
			Properties: map[string]mcpSchemaItem{
				"key": {
					Type: "string",
					Description: "Your name for this run, yours alone. Asking the same thing " +
						"under it again joins the run already there instead of starting a " +
						"second one, which makes a lost answer safe to ask for again; asking " +
						"something else under it is refused.",
				},
				"retain": {
					Type: "integer",
					Description: "Seconds to keep the result after the run finishes. Capped by the " +
						"server, and the answer reports what you were actually granted. Every read " +
						"of a finished run starts that window again.",
				},
				"tool": {
					Type:        "string",
					Description: "Tool to ask of every node.",
					Enum:        stepTools,
				},
				"args": {
					Type:        "object",
					Description: "Arguments for that tool, the same on every node. The node is added.",
				},
				"nodes": {
					Type:        "array",
					Description: "Nodes to ask. Empty asks every node on the cluster map.",
					Items:       &mcpItem{Type: "string"},
				},
			},
		},
	},
	{
		Name:   "cluster_batch",
		Title:  "Many questions at once",
		Fanout: true,
		Description: "Run different questions on different nodes at once: one step is a node, a " +
			"tool and its arguments. For when you already know which node runs which application " +
			"and want the whole picture of one business flow in one go. Answers like cluster_query, " +
			"with each answer carrying the id of its step.",
		Schema: mcpSchema{
			Type:     "object",
			Required: []string{"key", "steps"},
			Properties: map[string]mcpSchemaItem{
				"key": {
					Type: "string",
					Description: "Your name for this run, yours alone. Asking the same thing " +
						"under it again joins the run already there instead of starting a " +
						"second one, which makes a lost answer safe to ask for again; asking " +
						"something else under it is refused.",
				},
				"retain": {
					Type: "integer",
					Description: "Seconds to keep the result after the run finishes. Capped by the " +
						"server, and the answer reports what you were actually granted. Every read " +
						"of a finished run starts that window again.",
				},
				"steps": {
					Type: "array",
					Description: "Steps to run in parallel. The id is yours and comes back with " +
						"the answer, because answers arrive in the order they finish.",
					Items: &mcpItem{Type: "object", Properties: map[string]mcpSchemaItem{
						"id":         {Type: "string", Description: "Your name for this step."},
						nodeArgument: nodeProperty(),
						"tool":       {Type: "string", Description: "Tool to ask.", Enum: stepTools},
						"args":       {Type: "object", Description: "Arguments for that tool."},
					}},
				},
			},
		},
	},
	{
		Name:  "job_list",
		Title: "Runs you hold",
		Description: "The runs you still hold here, with their keys. A restarted agent has lost " +
			"its keys, and this is how it finds them again. Read a key's URI for its state.",
		Schema: mcpSchema{Type: "object"},
	},
	{
		Name:   "job_cancel",
		Title:  "Stop a run",
		Writes: true,
		Description: "Stop a run. By default it is asked to stop and keeps what it already " +
			"gathered until its retention runs out; force ends it at once and keeps nothing. A " +
			"key that holds nothing is not an error: held says whether there was anything to stop.",
		Schema: mcpSchema{
			Type:     "object",
			Required: []string{"key"},
			Properties: map[string]mcpSchemaItem{
				"key":   {Type: "string", Description: "The key you gave the run."},
				"force": {Type: "boolean", Description: "End it now instead of asking."},
			},
		},
	},
	{
		Name:         "cron",
		Title:        "Scheduled jobs",
		Action:       "cron_info",
		Capabilities: []string{inspect.CapGetCronInfo},
		Description: "The cron jobs of the node: their spec, timezone, when each last ran and " +
			"what it left behind. LastErr is the reason the last run failed and is the first " +
			"thing to read when a job looks stuck. Name one job to skip the rest.",
		Schema: mcpSchema{
			Type:     "object",
			Required: []string{nodeArgument},
			Properties: map[string]mcpSchemaItem{
				nodeArgument: nodeProperty(),
				"job":        {Type: "string", Description: "One job by name. Absent covers all."},
			},
		},
	},
	{
		Name:         "cron_schedule",
		Title:        "When jobs will fire",
		Action:       "cron_schedule",
		Capabilities: []string{inspect.CapGetCronSchedule},
		Description: "What the node will run and when. Only the node can answer this: the " +
			"schedule is computed against its own clock and each job's timezone, so it is also " +
			"how a wrong timezone shows itself.",
		Schema: mcpSchema{
			Type:     "object",
			Required: []string{nodeArgument},
			Properties: map[string]mcpSchemaItem{
				nodeArgument: nodeProperty(),
				"job":        {Type: "string", Description: "One job by name. Absent covers all."},
				"since": {Type: "string",
					Description: "Where the preview starts, RFC3339. Absent starts now."},
				"hours": {Type: "number",
					Description: "How far ahead to look. Default: 24."},
				"limit": {Type: "integer",
					Description: "Maximum firings to return. A per-minute job over a long " +
						"window answers with thousands. Default: 1000."},
			},
		},
	},
	{
		Name:         "registrar_nodes",
		Title:        "Nodes the registrar knows",
		Action:       "registrar_nodes",
		Capabilities: []string{inspect.CapGetRegistrarNodes},
		Description: "Every node registered with the service registry this node uses. It is " +
			"not the same set as ergo://cluster: that one is what this observer has reached, " +
			"this one is what the registry has been told about.",
		Schema: mcpSchema{
			Type:       "object",
			Required:   []string{nodeArgument},
			Properties: map[string]mcpSchemaItem{nodeArgument: nodeProperty()},
		},
	},
	{
		Name:         "registrar_routes",
		Title:        "How to reach a node",
		Action:       "registrar_routes",
		Capabilities: []string{inspect.CapGetRegistrarRoutes},
		Description: "The routes the registry publishes for one node: host, port, TLS and the " +
			"versions it speaks. Read it when a node is registered and still unreachable.",
		Schema: mcpSchema{
			Type:     "object",
			Required: []string{nodeArgument, "peer"},
			Properties: map[string]mcpSchemaItem{
				nodeArgument: nodeProperty(),
				"peer":       {Type: "string", Description: "The node whose routes to look up."},
			},
		},
	},
	{
		Name:         "registrar_proxy_routes",
		Title:        "Who relays to a node",
		Action:       "registrar_proxy_routes",
		Capabilities: []string{inspect.CapGetRegistrarProxyRoutes},
		Description: "The proxy routes the registry publishes for one node: which node relays " +
			"to it when it cannot be reached directly.",
		Schema: mcpSchema{
			Type:     "object",
			Required: []string{nodeArgument, "peer"},
			Properties: map[string]mcpSchemaItem{
				nodeArgument: nodeProperty(),
				"peer":       {Type: "string", Description: "The node whose proxy routes to look up."},
			},
		},
	},
	{
		Name:         "registrar_application_routes",
		Title:        "Where an application runs",
		Action:       "registrar_application_routes",
		Capabilities: []string{inspect.CapGetRegistrarApplicationRoutes},
		Description: "Which nodes publish one application, with the mode and state each one " +
			"reports. This is how to find the node running something without walking the " +
			"cluster node by node.",
		Schema: mcpSchema{
			Type:     "object",
			Required: []string{nodeArgument, "name"},
			Properties: map[string]mcpSchemaItem{
				nodeArgument: nodeProperty(),
				"name":       {Type: "string", Description: "Application name."},
			},
		},
	},
	{
		Name:         "capabilities",
		Title:        "What a node permits",
		Action:       "capabilities",
		Capabilities: []string{inspect.CapCapabilities},
		Description: "What this observer may do on that node: the operations the node offers " +
			"crossed with the ceiling of this caller. Read it before planning anything that writes.",
		Schema: mcpSchema{
			Type:       "object",
			Required:   []string{nodeArgument},
			Properties: map[string]mcpSchemaItem{nodeArgument: nodeProperty()},
		},
	},
}

// on a live node the largest twenty-five entries are around 99% of what is in use
const (
	defaultHeapEntries = 25
	defaultProcessRows = 100
	defaultTypeRows    = 50
)

func buildProcesses(args map[string]any) (string, map[string]any, error) {
	if args == nil {
		args = map[string]any{}
	}
	if hasArg(args, "limit") == false {
		args["limit"] = float64(defaultProcessRows)
	}
	return "processes", args, nil
}

func buildTypes(args map[string]any) (string, map[string]any, error) {
	if args == nil {
		args = map[string]any{}
	}
	if hasArg(args, "limit") == false {
		args["limit"] = float64(defaultTypeRows)
	}
	return "types", args, nil
}

// the default is here and not in the request builder: the browser goes through that one and
// shows the whole table
func buildHeapProfile(args map[string]any) (string, map[string]any, error) {
	if args == nil {
		args = map[string]any{}
	}
	if hasArg(args, "limit") == false {
		args["limit"] = float64(defaultHeapEntries)
	}
	return "heap", args, nil
}
