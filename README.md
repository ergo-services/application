[![Gitbook Documentation](https://img.shields.io/badge/GitBook-Documentation-f37f40?style=plastic&logo=gitbook&logoColor=white&style=flat)](https://docs.ergo.services/extra-library/applications)
[![MIT license](https://img.shields.io/badge/license-MIT-brightgreen.svg)](https://opensource.org/licenses/MIT)
[![Telegram Community](https://img.shields.io/badge/Telegram-ergo__services-229ed9?style=flat&logo=telegram&logoColor=white)](https://t.me/ergo_services)
[![Reddit](https://img.shields.io/badge/Reddit-r/ergo__services-ff4500?style=plastic&logo=reddit&logoColor=white&style=flat)](https://reddit.com/r/ergo_services)

Extra library of applications for the Ergo Framework 3.0 (and above)

## observer

Web-Dashboard Application for the node made with Ergo Framework

Doc: https://docs.ergo.services/extra-library/applications/observer

## mcp

Sidecar diagnostic application that exposes 46 inspection tools via MCP (Model Context Protocol) over HTTP. Enables AI agents (Claude Code, Claude Desktop) to diagnose performance bottlenecks, inspect processes, profile goroutines and heap, monitor metrics in real time, and trace issues across a cluster.

Doc: https://docs.ergo.services/extra-library/applications/mcp

## radar

Sidecar application that bundles Kubernetes health probes and Prometheus metrics into a single HTTP endpoint. Uses `actor/health` and `actor/metrics` internally; actors interact through helper functions in the `radar` package without importing the underlying dependencies.

Doc: https://docs.ergo.services/extra-library/applications/radar
