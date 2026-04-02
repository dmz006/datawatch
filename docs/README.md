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

## Security

| Document | Description |
|---|---|
| [encryption.md](encryption.md) | Encryption at rest — what's encrypted, formats (DWATCH1/DWDAT1/DWLOG1), export command, env variable, auto-detection |

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
| [architecture.md](architecture.md) | System architecture and component diagram |
| [data-flow.md](data-flow.md) | Message and data flow diagrams |
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
| [flow/signal-flow.md](flow/signal-flow.md) | Signal message flow |
| [flow/multi-source-flow.md](flow/multi-source-flow.md) | Multi-source message routing |

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
