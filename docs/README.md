# datawatch Documentation

Complete reference for configuring, operating, and extending datawatch.

---

## Getting Started

| Document | Description |
|---|---|
| [setup.md](setup.md) | Full installation and initial configuration guide |
| [commands.md](commands.md) | Complete command reference (messaging and CLI) |
| [pwa-setup.md](pwa-setup.md) | Progressive Web App setup via Tailscale |

## Backends

| Document | Description |
|---|---|
| [backends.md](backends.md) | Overview of all LLM and messaging backends |
| [llm-backends.md](llm-backends.md) | Detailed guide for every LLM backend (claude-code, aider, goose, gemini, opencode, ollama, openwebui, shell) |
| [messaging-backends.md](messaging-backends.md) | Detailed guide for every messaging backend (Signal, Telegram, Matrix, Discord, Slack, Twilio, ntfy, email, GitHub webhook, generic webhook) |

## Interfaces

| Document | Description |
|---|---|
| [mcp.md](mcp.md) | MCP (Model Context Protocol) server — Cursor, Claude Desktop, VS Code, remote AI agents |
| [cursor-mcp.md](cursor-mcp.md) | Quick-start for Cursor and Claude Desktop MCP integration |
| [pwa-setup.md](pwa-setup.md) | Browser PWA interface over Tailscale |
| [api/openapi.yaml](api/openapi.yaml) | OpenAPI 3.0 REST API specification |

## Memory & Intelligence

| Document | Description |
|---|---|
| [memory.md](memory.md) | Episodic memory — architecture, flow diagrams, configuration, MCP tools, REST API, monitoring, encryption |
| [memory-usage-guide.md](memory-usage-guide.md) | Practical examples — saving, searching, KG, Claude integration, OpenWebUI chat, PostgreSQL, API reference |

## Security

| Document | Description |
|---|---|
| [encryption.md](encryption.md) | Encryption at rest — what's encrypted, formats, memory content encryption, export command, env variable |

## Operations

| Document | Description |
|---|---|
| [operations.md](operations.md) | Day-to-day operations, monitoring, and maintenance |
| [multi-session.md](multi-session.md) | Running datawatch across multiple machines |
| [session-tracking.md](session-tracking.md) | Session lifecycle and state machine |
| [rtk-integration.md](rtk-integration.md) | RTK token savings integration — setup, config, stats, supported backends |
| [channel-testing.md](channel-testing.md) | MCP channel testing guide — manual test procedures for bidirectional flow |
| [config-reference.yaml](config-reference.yaml) | Complete annotated config file reference (all fields, defaults, comments) |
| [addons.md](addons.md) | Optional add-ons and integrations |
| [uninstall.md](uninstall.md) | Manual uninstall instructions for all installation methods |
| [testing-tracker.md](testing-tracker.md) | Interface validation status for all backends |
| [testing.md](testing.md) | Bug test procedures and results log (28 tests, all PASS) |

## Reference

| Document | Description |
|---|---|
| [architecture-overview.md](architecture-overview.md) | Top-level one-screen Mermaid map of every interface, subsystem and data path (incl. planned features) |
| [architecture.md](architecture.md) | System architecture and component diagram |
| [data-flow.md](data-flow.md) | Message and data flow diagrams |
| [agents.md](agents.md) | F10 ephemeral container-spawned agents — REST/MCP/CLI/comm reference, spawn flow, security notes, helm chart pointer |
| [registry-and-secrets.md](registry-and-secrets.md) | Operator setup — point datawatch at *your* registry, K8s cluster, GitHub/GitLab account, and TLS material; covers config knobs across every channel + audit recipes |
| [profiles.md](profiles.md) | Project + Cluster Profile schema, validation, smoke checks (F10 sprints 2-4) |
| [composition-examples.md](composition-examples.md) | Concrete Project + Cluster Profile compositions — agent/lang/tools image pairings, real example configs |
| [container-build.md](container-build.md) | Building the agent-* / lang-* / tools-* images (Dockerfile + Makefile + harbor push flow) |
| [test-coverage.md](test-coverage.md) | Per-package test counts, integration smoke scripts, known thinly-covered areas |
| [plan-attribution.md](plan-attribution.md) | Attribution + comparison to inspiring projects (mempalace, nightwire) — direct features, divergences, what was built because they inspired it |
| [app-flow.md](app-flow.md) | Application state machine |
| [design.md](design.md) | Design goals and philosophy |
| [implementation.md](implementation.md) | Implementation notes and internals |
| [planning.md](planning.md) | Roadmap and feature planning |
| [backends.md](backends.md) | Backend configuration reference table |
| [claude-channel.md](claude-channel.md) | MCP channel server for Claude Code — per-session channels, auto-retry |
| [covert-channels.md](covert-channels.md) | Research notes on DNS tunneling and alternative low-profile channels |
| [future-native-signal.md](future-native-signal.md) | libsignal native integration plan (replacing signal-cli/Java) |
| [plans/README.md](plans/README.md) | **Project tracker** — bugs, plans, backlog, completed items (single source of truth) |
| [plans/](plans/) | Implementation plans (one file per release/feature, YYYY-MM-DD-slug.md) |

## Flow Diagrams

| Document | Description |
|---|---|
| [data-flow.md](data-flow.md) | **Index** — links to all flow diagrams below |
| [flow/system-data-flow.md](flow/system-data-flow.md) | Top-level component interaction (Signal, daemon, tmux, PWA) |
| [flow/new-session-flow.md](flow/new-session-flow.md) | New session sequence (`new:` → tmux → running) |
| [flow/input-required-flow.md](flow/input-required-flow.md) | Input detection → prompt alert → user reply → resume |
| [flow/multi-machine-flow.md](flow/multi-machine-flow.md) | Two hosts sharing one Signal group |
| [flow/multi-source-flow.md](flow/multi-source-flow.md) | Multiple messaging backends + proxy mode variant |
| [flow/websocket-flow.md](flow/websocket-flow.md) | WS connect, subscribe, output push, keepalive |
| [flow/signal-flow.md](flow/signal-flow.md) | Signal message flow (Phone → signal-cli → router) |
| [flow/signal-rpc-flow.md](flow/signal-rpc-flow.md) | signal-cli JSON-RPC protocol detail |
| [flow/persistence-flow.md](flow/persistence-flow.md) | Config, sessions.json, log file write patterns |
| [flow/dns-channel-flow.md](flow/dns-channel-flow.md) | DNS TXT query encoding, HMAC auth, response fragmentation |
| [flow/proxy-flow.md](flow/proxy-flow.md) | Proxy mode — remote routing, session discovery, command forwarding |
| [flow/agent-spawn-flow.md](flow/agent-spawn-flow.md) | F10 agent lifecycle — profile → spawn → bootstrap → clone → terminate → revoke → sweep |

---

## Quick Links

- **Install:** `curl -fsSL https://raw.githubusercontent.com/dmz006/datawatch/main/install/install.sh | bash`
- **Source:** [github.com/dmz006/datawatch](https://github.com/dmz006/datawatch)
- **Issues:** [github.com/dmz006/datawatch/issues](https://github.com/dmz006/datawatch/issues)
- **Live endpoints** (when daemon is running):
  - Swagger UI: `http://<host>:8080/api/docs`
  - OpenAPI spec: `http://<host>:8080/api/openapi.yaml`
  - MCP Tools (HTML): `http://<host>:8080/api/mcp/docs`
  - MCP Tools (JSON): `http://<host>:8080/api/mcp/docs` (Accept: application/json)
  - Ollama models: `http://<host>:8080/api/ollama/models`
  - OpenWebUI models: `http://<host>:8080/api/openwebui/models`
  - Health check: `http://<host>:8080/api/health`
  - Liveness probe: `http://<host>:8080/healthz`
  - Readiness probe: `http://<host>:8080/readyz`
  - Prometheus metrics: `http://<host>:8080/metrics`
