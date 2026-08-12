# TODO

## Self-describing inspection: probe a reserved `mcp` item

**Problem.** `process_inspect` takes an optional `items` array and passes it straight to the
process. Nothing tells the agent what a given actor accepts there. An actor with a large state
typically answers a bounded summary by default and offers parameterised queries - `session <id>`,
`order <id>`, `top slowest` - but that vocabulary exists only in the author's head and in the
source. So the agent either guesses, or the operator has to know the schema and say it out loud.
Either way the drill-down step depends on knowledge that is not in the system.

**Idea.** Establish a reserved inspection item, `mcp`, that returns a machine-readable
description of what this actor's `HandleInspect` accepts and returns. MCP probes it and uses the
answer to drive discovery.

```
Inspect(pid, "mcp") -> {"mcp": "<descriptor>"}
```

The descriptor has to fit in a `map[string]string` value, so a single compact JSON string in the
`mcp` key is the obvious encoding - parseable, versionable, one round trip.

```json
{
  "v": 1,
  "summary": ["sessions_total", "sessions_idle", "oldest_age"],
  "queries": [
    {"item": "session <id>", "returns": "user, state, idle, bytes_in"},
    {"item": "user <id>",    "returns": "session count and up to 20 ids"},
    {"item": "top slowest [n]", "returns": "n slowest sessions, default 10"}
  ]
}
```

`help` stays what it is - free text for a human. `mcp` is the parseable sibling. Both reserved,
and that needs documenting so authors do not collide with them.

**How MCP would use it.**

- On the first `process_inspect` of a process, probe `mcp` alongside the requested items. If the
  answer carries no `mcp` key (or `<unknown item>`), behave exactly as today - this must stay
  optional, most actors will never implement it.
- Cache the descriptor by `BehaviorName()`, not by PID: it describes a type, not an instance, so
  one probe per behavior per node covers every process of that behavior.
- Expose it as its own tool - `process_inspect_schema` - so an agent can ask "what can I ask
  this?" without pulling state, and fold the vocabulary into the `process_inspect` result so it
  is discovered even when the agent did not think to ask.
- Cap the descriptor size and reject a malformed one quietly. A bad descriptor must not break
  plain inspection.

**Why it is worth doing.** It closes the last gap in agent-driven diagnosis. Today the agent can
enumerate processes, read state, follow the topology and correlate with source - but the
drill-down into a large actor needs a vocabulary it cannot discover. With this it can work
breadth-then-depth on an unfamiliar actor with no prior knowledge: read the summary, learn the
queries, take an id out of the summary or a log line, and go straight to the one entity
implicated. No dashboard has to have been built for that question in advance.

The observer benefits from the same descriptor: it can render a query picker instead of a blind
text field.

**Open questions.**

- Should `RegisterType`-style registration exist for descriptors, so a behavior declares it once
  rather than answering it per call? Answering per call is simpler and always in sync with the
  code; registration would allow the schema to be listed without touching a live process.
- Is `v` enough for evolution, or should the descriptor carry the behavior version as well?
- Worth extending to meta-processes (`meta_inspect`) at the same time - the argument is identical.
