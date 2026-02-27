# MCP Application

Sidecar diagnostic application for Ergo Framework. Runs inside your node as a regular Ergo application and exposes 46 inspection tools via MCP (Model Context Protocol) over HTTP. Enables AI agents to diagnose performance bottlenecks, inspect processes, profile goroutines and heap, monitor metrics in real time, and trace issues across a cluster -- without restarting or redeploying the node.

Two deployment modes: **entry point** (with HTTP listener) and **agent** (no HTTP, accessible via cluster proxy). A single entry point node gives access to every node in the cluster that runs MCP in agent mode -- one HTTP endpoint to diagnose them all.

```go
// Add to your node -- diagnostics available immediately via HTTP
mcp.CreateApp(mcp.Options{Port: 9922})
```

## Features

- **Zero-friction setup**: sidecar application -- add to `gen.NodeOptions.Applications` and it works
- **46 diagnostic tools**: processes, applications, events, network, cron, registrar, Go runtime
- **Process-level profiling**: goroutine stack traces by PID (with `-tags=pprof`), heap profiling, runtime stats
- **Active sampling**: periodically call any tool into a ring buffer -- monitor trends over time
- **Passive sampling**: capture log streams and event publications as they happen
- **Cluster-wide proxy**: every tool works on remote nodes -- one HTTP entry point for the entire cluster
- **Action tools**: send messages and make sync calls with typed payloads from EDF registry, terminate processes gracefully or forcefully
- **Agent mode**: `Port: 0` -- no HTTP listener, but fully accessible via cluster proxy from another node

## Quick Start

### Installation

```bash
go get ergo.services/application/mcp
```

### Add to Your Node

```go
package main

import (
    "ergo.services/application/mcp"
    "ergo.services/ergo"
    "ergo.services/ergo/gen"
)

func main() {
    node, _ := ergo.StartNode("mynode@localhost", gen.NodeOptions{
        Applications: []gen.ApplicationBehavior{
            mcp.CreateApp(mcp.Options{
                Port: 9922,
            }),
        },
    })

    // MCP endpoint: http://localhost:9922/mcp
    node.Wait()
}
```

### Configuration

```go
mcp.Options{
    Host:         "localhost",     // Listen address
    Port:         9922,           // HTTP port (0 = agent mode, no HTTP)
    Token:        "secret",       // Bearer token authentication (empty = no auth)
    ReadOnly:     false,          // Disable action tools (send_message, send_exit, etc.)
    AllowedTools: nil,            // Tool whitelist (nil = all tools enabled, respects ReadOnly)
    PoolSize:     5,              // Number of worker processes
    CertManager:  nil,            // TLS certificate manager
    LogLevel:     gen.LogLevelInfo,
}
```

**Agent mode**: set `Port: 0`. The application starts without an HTTP listener but remains available for remote tool calls from other nodes via cluster proxy. Use this for backend nodes that should be inspectable but don't need their own HTTP endpoint.

## Connect a Client

### Claude Code

```bash
# 1. Add MCP server
claude mcp add --transport http ergo http://localhost:9922/mcp

# With authentication
claude mcp add --transport http ergo http://localhost:9922/mcp \
  -H "Authorization: Bearer my-secret-token"
```

Allow all tools without per-call prompts -- add to `.claude/settings.json`:

```json
{
  "permissions": {
    "allow": [
      "mcp__ergo"
    ]
  }
}
```

The prefix `mcp__ergo` matches the server name from the `claude mcp add` command.


## Available Tools

Every tool accepts an optional `node` parameter for [cluster proxy](#cluster-proxy).

### Node (2)

| Tool | Description |
|------|-------------|
| `node_info` | Node name, uptime, version, process counts, memory, CPU time, registered names/aliases/events counts, event statistics, application counts |
| `node_env` | Node environment variables as key-value pairs |

### Process (7)

| Tool | Description |
|------|-------------|
| `process_list` | List processes with short info. Filtering by application, behavior, state, name, and numeric thresholds (messages_in/out, mailbox, latency, running_time, init_time, wakeups, uptime). Sorting (descending): mailbox, mailbox_latency, running_time, init_time, wakeups, uptime, messages_in, messages_out, drain |
| `process_children` | Child processes of a parent (by PID or name). Optional `recursive` flag for full subtree |
| `process_info` | Detailed: state, mailbox queues, message counts, links, monitors, aliases, events, meta processes, parent, leader, environment |
| `process_state` | Current state: Init, Sleep, Running, WaitResponse, Terminated, Zombee |
| `process_lookup` | Resolve registered name to PID or PID to name |
| `process_inspect` | Custom HandleInspect callback, returns actor-specific key-value state |
| `meta_inspect` | Same for meta processes (WebSocket, Port connections) |

### Application (3)

| Tool | Description |
|------|-------------|
| `app_list` | Applications with state, mode, uptime, process count. Filters: state, mode, name, min_uptime |
| `app_info` | Detailed: state, mode, version, description, dependencies, environment, group processes |
| `app_processes` | Processes belonging to an application (configurable limit) |

### Event (2)

| Tool | Description |
|------|-------------|
| `event_list` | Events with subscriber count, message stats, buffer, notify mode. Filters: name, producer (PID or process name), notify, subscribers, published, utilization_state. Sorting: subscribers, published, local_sent, remote_sent |
| `event_info` | Detailed info about a specific event: producer, subscribers, message counts |

### Network (7)

| Tool | Description |
|------|-------------|
| `network_info` | Mode, flags, max message size, acceptors, routes |
| `network_nodes` | Connected remote nodes with traffic stats. Filters: name, uptime, messages/bytes |
| `network_node_info` | Detailed info about one remote node |
| `network_acceptors` | Listening ports configuration |
| `network_connect` | Connect to a remote node via registrar/routes |
| `network_connect_route` | Connect with explicit host:port, TLS, cookie |
| `network_disconnect` | Disconnect from a remote node |

### Cron (3)

| Tool | Description |
|------|-------------|
| `cron_info` | Scheduler state, next run, all jobs |
| `cron_job` | One job: spec, last run, last error |
| `cron_schedule` | Jobs planned within N seconds |

### Registrar (5)

| Tool | Description |
|------|-------------|
| `registrar_info` | Server address, version, features |
| `registrar_resolve` | Node name to connection routes |
| `registrar_resolve_proxy` | Proxy routes through intermediaries |
| `registrar_resolve_app` | Which nodes run a specific application |
| `cluster_nodes` | All known nodes: self, connected, discovered |

### Debug (3)

| Tool | Description |
|------|-------------|
| `pprof_goroutines` | Goroutine profile. With `pid`: per-process stack trace (requires `-tags=pprof`). Without: all goroutines. Sleeping processes park their goroutine -- use `sample_start` to poll |
| `pprof_heap` | Heap memory profile, top allocators |
| `runtime_stats` | Goroutine count, heap, GC, CPU stats |

### Sampler (5)

| Tool | Description |
|------|-------------|
| `sample_start` | Active sampler: periodically call any tool into a ring buffer. Params: tool, arguments, interval, count, duration, max_errors |
| `sample_listen` | Passive sampler: capture log messages and/or event publications |
| `sample_read` | Read collected entries (incremental via `since` parameter) |
| `sample_stop` | Stop a running sampler |
| `sample_list` | List active samplers with pid, description, status, owner, samples, uptime, deadline, remaining, progress, errors |

### Log Level (3)

| Tool | Description |
|------|-------------|
| `log_level_get` | Current log level for node, process, or meta process |
| `log_level_set` | Set level: trace, debug, info, warning, error, panic, disabled |
| `loggers_list` | Registered loggers with names and levels |

### Action (6, disabled with ReadOnly)

| Tool | Description |
|------|-------------|
| `message_types` | List EDF-registered message types |
| `message_type_info` | Type structure: field names, Go types, JSON tags |
| `send_message` | Async message to a process (typed via EDF or raw JSON) |
| `call_process` | Sync request with response (typed, configurable timeout) |
| `send_exit` | Exit signal: normal, shutdown, kill, or custom reason |
| `process_kill` | Force kill, immediate Zombee state |

## Sampler

Samplers collect data into ring buffers that agents read via `sample_read`. Two modes: **active** (periodic tool calls) and **passive** (event-driven capture). All samplers are time-limited (default 60s, max 1 hour).

### Active Sampler (sample_start)

Periodically calls any MCP tool and stores results. The sampler is a generic periodic tool executor -- any tool can be sampled.

```bash
# Monitor top processes by mailbox depth every 5 seconds
sample_start tool=process_list arguments={"sort_by":"mailbox","limit":10} interval_ms=5000

# Track node health every 10 seconds for 5 minutes
sample_start tool=node_info interval_ms=10000 duration_sec=300

# Monitor runtime memory trends
sample_start tool=runtime_stats interval_ms=5000

# Catch a sleeping process goroutine (poll until it wakes up)
sample_start tool=pprof_goroutines arguments={"pid":"<XXX.0.1005>"} interval_ms=300 count=1 max_errors=0
```

**Error handling**: `max_errors=0` (default) ignores errors and keeps retrying. `max_errors=N` stops after N consecutive failures. Duration always applies as upper bound.

### Passive Sampler (sample_listen)

Captures log messages and/or event publications as they happen. Both can be combined in one sampler.

```bash
# Capture warning and error logs
sample_listen log_levels=["warning","error"] duration_sec=120

# Subscribe to event publications
sample_listen event=my_event duration_sec=60

# Combined: logs + event
sample_listen log_levels=["warning","error"] event=my_event duration_sec=120
```

### Reading Samples

```bash
# All buffered entries
sample_read sampler_id=mcp_sampler_abcd1234 since=0

# Incremental (only new since last read)
sample_read sampler_id=mcp_sampler_abcd1234 since=5
```

Response:

```json
{
  "sampler_id": "mcp_sampler_abcd1234",
  "mode": "active",
  "tool": "node_info",
  "sequence": 5,
  "completed": false,
  "samples": [
    {"sequence": 0, "timestamp": "2026-02-26T15:10:49+01:00", "data": { ... }},
    {"sequence": 1, "timestamp": "2026-02-26T15:10:50+01:00", "data": { ... }}
  ]
}
```

### Sampler Inspection

`sample_list` returns human-readable status for all active samplers:

```json
[
  {
    "id": "mcp_sampler_2e3eadbf",
    "pid": "<4FF6A515.0.1014>",
    "description": "process_list(sort_by=mailbox, limit=5) every 2s",
    "status": "running",
    "owner": "mynode@localhost",
    "samples": "16 collected, buffer 16/256",
    "uptime": "32s",
    "deadline": "2026-02-26T19:58:10+01:00",
    "remaining": "28s"
  },
  {
    "id": "mcp_sampler_12fd15d2",
    "pid": "<4FF6A515.0.1016>",
    "description": "listen: log [warning, error] source=process",
    "status": "running",
    "owner": "mynode@localhost",
    "samples": "3 collected, buffer 3/256",
    "uptime": "15s",
    "deadline": "2026-02-26T19:58:23+01:00",
    "remaining": "41s"
  },
  {
    "id": "mcp_sampler_703a3bd5",
    "pid": "<4FF6A515.0.1015>",
    "description": "runtime_stats every 1s",
    "status": "running",
    "owner": "mynode@localhost",
    "samples": "6 collected, buffer 6/256",
    "progress": "6/20 samples",
    "uptime": "6s",
    "remaining": "54s"
  }
]
```

### Real-Time Metrics

Active samplers replicate what `actor/metrics` provides for Prometheus, but in real-time via MCP without external dependencies:

| Metric | Sampler |
|--------|---------|
| Node health | `sample_start tool=node_info interval_ms=10000` |
| Go runtime | `sample_start tool=runtime_stats interval_ms=5000` |
| Top mailbox depth | `sample_start tool=process_list arguments={"sort_by":"mailbox","limit":10}` |
| Top mailbox latency | `sample_start tool=process_list arguments={"sort_by":"mailbox_latency","limit":10}` |
| Top CPU utilization | `sample_start tool=process_list arguments={"sort_by":"running_time","limit":10}` |
| Top throughput in/out | `sample_start tool=process_list arguments={"sort_by":"messages_in","limit":10}` |
| Top wakeups | `sample_start tool=process_list arguments={"sort_by":"wakeups","limit":10}` |
| Top drain ratio | `sample_start tool=process_list arguments={"sort_by":"drain","limit":10}` |
| Network nodes | `sample_start tool=network_nodes interval_ms=30000` |
| Events top published | `sample_start tool=event_list arguments={"sort_by":"published","limit":10}` |
| Log stream | `sample_listen log_levels=["warning","error"]` |
| Event stream | `sample_listen event=my_event` |

## Cluster Proxy

Every tool accepts an optional `node` parameter. When specified and the target is a different node, the request is proxied via native Ergo inter-node protocol (not HTTP). The remote node must have MCP application running -- agent mode is sufficient.

```bash
# Get process list from a remote node
curl -s -X POST http://localhost:9922/mcp \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{
    "name":"process_list",
    "arguments":{"node":"backend@host","sort_by":"mailbox","limit":10}
  }}'
```

## Build Tags

| Tag | Enables |
|-----|---------|
| `-tags=pprof` | Per-process goroutine stack traces in `pprof_goroutines` via `runtime/pprof` labels |
| `-tags=latency` | Mailbox latency measurement: `mailbox_latency` sort/filter in `process_list` |

## Authentication

When `Token` is set, all requests require `Authorization: Bearer <token>` header. Missing or wrong token returns HTTP 401.

## License

See LICENSE file in the repository root.
