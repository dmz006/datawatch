# datawatch

<p align="center"><img src="internal/server/web/icon-512.svg" width="180" alt="datawatch logo"/></p>

**A distributed control plane for orchestrating AI work — recursive, episodic, and secure across hosts, clusters, and channels.**

[![License: Polyform NC](https://img.shields.io/badge/license-Polyform%20NC%201.0-blue)](LICENSE)
[![Go version](https://img.shields.io/badge/go-1.24%2B-00ADD8)](https://go.dev)
[![Platform](https://img.shields.io/badge/platform-Linux%20%7C%20macOS%20%7C%20WSL2-lightgrey)](docs/setup.md)

`datawatch` started as a daemon that bridged Signal/Telegram to AI coding sessions running in tmux. It's now a single-binary control plane that runs, remembers, plans, and attests AI work — local sessions, ephemeral container workers, persistent memory, and the messaging fabric that ties them together — under one operator with one set of lifecycle, audit, and security guarantees.

**Current release: v6.6.1 (2026-05-04).** Patch: BL246-followup — Automata batch action bar moved to a fixed bottom popup above the nav (Sessions-parity); redundant Launch Automation header button removed (FAB covers it). v6.6.0 minor closed BL252 PWA i18n full coverage (GH#32) and BL246 Automata UX overhaul. See [CHANGELOG.md](CHANGELOG.md) for full history.

**Why a control plane and not a bot.** The same profile that drives a chat-spawned session can drive a Kubernetes-deployed worker in a remote cluster, a child agent of an existing worker, a scheduled cron job, a webhook reaction, or a cross-host fan-out — and the operator only ever interacts with one surface: the daemon's REST API (mirrored verbatim through MCP, CLI, web UI, and every comm channel). That uniformity is the whole point.

<p align="center"><img src="docs/tour.gif" width="300" alt="datawatch web UI tour"/></p>

---

## What it does

- Start an AI coding session by sending `new: <task>` in any configured group
- Receive automatic notifications when sessions complete or need your input
- **Automatic rate-limit recovery** — detects rate limits, pauses the session, and auto-resumes with context after the reset window (persisted across daemon restarts)
- Reply to AI prompts directly from Signal, Telegram, Matrix, or any webhook
- Monitor and manage multiple sessions across multiple machines from one group thread
- Stream live session output in a browser PWA over Tailscale (xterm.js with full ANSI support)
- Install the PWA to your Android/iOS home screen for one-tap access
- **System monitoring dashboard** — CPU, memory, disk, GPU, network, per-session resource usage
- **Communication channel analytics** — per-channel message counts, bytes in/out, error tracking, connection stats
- **LLM backend analytics** — total/active sessions, average duration, prompts per session for each backend
- **eBPF per-process network tracking** — optional kernel-level TCP tracking for daemon and individual sessions
- Persist sessions across daemon restarts with a JSON file store
- Pluggable LLM backend: claude-code, aider, goose, gemini, opencode, opencode-acp, openwebui, or a custom shell script
- **Voice input** — send voice messages via Telegram or Signal; automatically transcribed via Whisper and routed as text commands
- Pluggable messaging backend: Signal, Telegram, Discord, Slack, Matrix, Twilio, GitHub webhooks, generic webhooks, DNS channel
- **RTK integration** — optional [RTK](https://github.com/rtk-ai/rtk) token savings tracking with auto-init and stats dashboard
- **Prometheus metrics** — `/metrics` endpoint for Grafana/monitoring; `/healthz` + `/readyz` for Kubernetes probes
- **Multi-profile fallback chains** — named backend profiles with auto-switch on rate limit
- **Episodic memory system** — vector-indexed project knowledge with semantic search (`remember`, `recall`, `learnings`). SQLite (pure Go, no cgo) or PostgreSQL+pgvector. Ollama or OpenAI embeddings. Deduplication, write-ahead log, embedding cache, export/import. Optional XChaCha20-Poly1305 content encryption with key rotation
- **Temporal knowledge graph** — entity-relationship triples with time validity windows, point-in-time queries, invalidation. `kg query/add/timeline/stats` from any channel
- **4-layer wake-up stack** — L0 identity + L1 critical facts auto-loaded on every session start (~600 tokens of persistent context)
- **Full mempalace spatial schema (6 axes)** — floor / wing / room / hall / shelf / box, all auto-derived at save time (`AutoTagFull`). +34pp retrieval improvement vs flat search; floor adds org grouping, shelf carves sub-rooms, box bundles by author (v6.0.0)
- **Memory pinning** — operator-marked memories always surface in L1 critical-facts regardless of vector rank (v5.26.70)
- **Query sanitization** — recall queries are scrubbed for OWASP-LLM01 prompt-injection patterns before reaching the embedder (v5.26.70)
- **Pre-save normalization** — NFC + whitespace collapse + fancy-quote folding so dedup catches drift (v6.0.0)
- **Similarity-stale eviction** — `last_hit_at` tracking; rows that never surface in queries become eviction candidates (v6.0.0)
- **Periodic refine sweep** — old session/output_chunk rows get LLM-compressed in place to reclaim row count (v6.0.0)
- **Schema-free fact extraction** — heuristic SVO triples + optional LLM fallback (v6.0.0)
- **Conservative spellcheck** — Levenshtein-based suggestions on top of an English + datawatch-domain dictionary; never rewrites (v6.0.0)
- **Convo miners** — Slack JSON exports, IRC logs (weechat/irssi/hexchat), mbox emails round-trip into the memory store (v6.0.0)
- **Corpus origin** — every memory row carries a `Source` tag ("operator", "session", "channel:slack", "mcp:remember", …) populated automatically (v6.0.0)
- **Repair self-check** — scans for missing embeddings, orphan closets, content-sha duplicates; optional inline re-embed (v5.26.70)
- **Conversation-window stitching** — semantic search returns hit + N predecessors + N successors from the same session (v5.26.70)
- **Response capture & copy** — `copy` gets the last LLM response; `prompt` gets the last user input. Rich markdown formatting for Slack/Discord/Telegram. Alerts include both prompt and response
- **Proxy mode** — relay commands and sessions across multiple machines from one group; aggregated session list, WS relay, PWA reverse proxy, circuit breaker, offline queue, `new: @server: task` routing
- **Test message endpoint** — `POST /api/test/message` simulates comm channel commands for testing without Signal/Telegram
- **Session chaining (pipelines)** — chain tasks in a DAG: `pipeline: task1 -> task2 -> task3`. Parallel execution with dependency tracking, cycle detection, cancel support
- **Quality gates** — run tests before and after sessions, detect regressions, block on new failures
- **Remote Ollama server monitoring** — live GPU stats, VRAM usage, loaded models, disk usage from the Ollama API in the Monitor dashboard
- **Rich chat UI** — full chat interface for `output_mode: chat` backends (OpenWebUI, Ollama, OpenCode-ACP): rounded message bubbles with avatars (U/AI/S), timestamps, animated typing dots, hover actions (Copy, Remember to memory), memory command quick bar (recall, research, kg query), markdown rendering with code blocks, thinking overlay, centered system messages. Configurable per-backend via `output_mode` setting. Prompt debounce and notification cooldown prevent alert floods
- **Conversation mining** — import Claude Code, ChatGPT, and generic JSON conversation exports into memory
- **Claude Code hooks** — auto-save to memory every N exchanges, pre-compact context preservation
- MCP (Model Context Protocol) server — 60+ tools covering sessions, memory, cost, audit, routing rules, templates, projects, autonomous, plugins, orchestrator, and more. Works with Cursor, Claude Desktop, VS Code; also available over HTTP/SSE for network LLMs
- **Ephemeral container-spawned workers** — pick a project profile (what repo / agent / language) + cluster profile (where: Docker / Kubernetes) and the daemon spawns a worker container or Pod, mints a short-lived per-spawn git token, the worker bootstraps over a TLS-pinned connection (post-quantum-protected envelope), clones the repo, runs the session, then gets reaped + token-revoked + audit-trailed. Slim distroless agent images (`agent-claude` / `agent-opencode` ship without Node.js — per-platform native tarballs from npm CDN; `stats-cluster` is 11 MB). Helm chart at [`charts/datawatch/`](charts/datawatch/) for in-cluster deploys. Full sequence in [docs/flow/f10-agent-spawn-flow.md](docs/flow/f10-agent-spawn-flow.md); config reference in [docs/agents.md](docs/agents.md)
- **Cross-cluster observer federation** — a primary datawatch can register itself as a peer of a *root* primary; push-with-chain loop prevention, per-envelope source attribution, opt-in via one config key. Every ephemeral worker auto-peers with its parent so the federation card shows live agent CPU/RSS/net with no separate plumbing. PRD-DAG nodes carry per-PRD CPU/RSS/envelope-count rolled up across the local observer + every federated peer
- **Mobile companion** — [`datawatch-app`](https://github.com/dmz006/datawatch-app) is a pre-1.0 Android / Wear OS / Android Auto client (an iOS skeleton sits in the repo). Pairs with the daemon's mobile API: push-device registration, voice transcription, federated session list. The 1.0 badge is held back until full PWA feature parity
- **Autonomous PRD decomposition + verification** — describe a feature in plain English, datawatch splits it into stories and tasks, runs each as a worker session, and an independent verifier attests each result. Auto-fix retries re-prompt on verification failure. See [docs/api/autonomous.md](docs/api/autonomous.md)
- **PRD-DAG orchestrator with guardrails** — compose multiple plans into a dependency graph; after each plan, a configurable set of guardrails (rules / security / release-readiness / docs-diagrams-architecture) returns pass/warn/block. Any block halts the graph. See [docs/api/orchestrator.md](docs/api/orchestrator.md) and [flow](docs/flow/bl117-orchestrator-flow.md)
- **Plugin framework** — drop a manifest + executable under `~/.datawatch/plugins/<name>/` and the daemon discovers it automatically, hot-reloading on directory changes. Plugins hook session start, output (with chained filter fan-out), completion, and alerts via a simple line-oriented JSON-RPC protocol. See [docs/api/plugins.md](docs/api/plugins.md)
- Named sessions with resume — Claude sessions tagged with `--name` for easy identification and `/resume`
- Optional push notifications via ntfy and email
- Optional automatic git commits before and after each session

---

## Memory & Intelligence

datawatch includes an episodic memory system that builds project knowledge over time.
Every completed session, manual note, and extracted learning becomes searchable context.

- **Semantic search** — `recall: deployment process` finds relevant memories by meaning
- **Knowledge graph** — `kg add Alice works_on datawatch` tracks entity relationships with temporal validity
- **4-layer wake-up** — identity + critical facts auto-injected on every session start
- **Spatial organization** — wings (projects) / rooms (topics) / halls (types) for +34pp retrieval improvement
- **Encryption** — optional XChaCha20-Poly1305 content encryption with key rotation
- **Deduplication + WAL** — content hashing prevents duplicates, JSONL audit trail for all writes
- **Export/import** — `memories export` for backup, `memories import` for migration

See [docs/memory.md](docs/memory.md) for full documentation.

---

## Quick Demo

### Session Management
```
You (Signal/Telegram):  new: write unit tests for the auth package
[laptop] Session a3f2 started: write unit tests for the auth package

... 3 minutes later ...

[laptop] Session a3f2 waiting for input:
  Prompt: write unit tests for the auth package
  ---
  Found 3 files to modify. Proceed? [y/N]

You:  send a3f2: y
[laptop] Session a3f2 resumed.

... 2 minutes later ...

[laptop] Session a3f2 complete.
  Tests written: auth_test.go (14 tests, all passing)
```

### Memory & Knowledge Graph
```
You:  remember: the CI pipeline requires Go 1.24 and golangci-lint
[laptop] Saved memory #4

You:  recall: CI requirements
[laptop] Recall results:
  #4 [60%] manual: the CI pipeline requires Go 1.24 and golangci-lint

You:  kg add Alice works_on datawatch
[laptop] Added triple #1: Alice works_on datawatch

You:  kg query Alice
[laptop] KG: Alice
  #1 Alice works_on datawatch (from 2026-04-09)
```

### Pipelines & Response Capture
```
You:  pipeline: analyze code -> write tests -> update docs
[laptop] Pipeline started: [pipe-12345] 3 tasks (0 done, 0 running, 3 pending)

You:  copy
[laptop] Last response [a3f2]:
  **Tests written: auth_test.go (14 tests, all passing)**

You:  prompt
[laptop] Last prompt [a3f2]: write unit tests for the auth package
```

### Memory-Aware Sessions
```
# Memory is automatically loaded on every session start (wake-up stack)
# The LLM starts with context from past sessions, learnings, and knowledge graph

You:  new: fix the JWT validation bug
[laptop] Session c7e2 started: fix the JWT validation bug
# Session auto-injected with:
#   L0: project identity
#   L1: critical facts (auth uses RS256, tests require Go 1.24)
#   L2: related memories about JWT and auth from past sessions
#   L3: knowledge graph entities (Alice works_on auth-module)

# After session completes, learnings are auto-extracted and saved:
[laptop] Session c7e2 complete.
[memory] Saved summary for session c7e2
[memory] Extracted learning: JWT validation must check exp AND nbf claims

# Future sessions about JWT automatically get this context
You:  recall: JWT
[laptop] Recall results:
  #12 [92%] learning: JWT validation must check exp AND nbf claims
  #8  [78%] session c7e2: fixed JWT validation bug in auth middleware
```

### Cross-Session Research
```
# Deep search across all sessions and memories
You:  research: what changes were made to the database schema last month?
[laptop] Research results (3 sessions, 5 memories):
  Session [d4a1] 2026-03-15: added user_preferences table
  Session [e9b3] 2026-03-22: migrated auth tokens to JWT format
  Memory #15: schema requires UUID primary keys for all new tables

# Knowledge graph tracks relationships over time
You:  kg timeline auth-module
[laptop] Timeline: auth-module
  2026-03-10: Alice works_on auth-module
  2026-03-22: auth-module uses JWT (was: session-tokens)
  2026-04-05: auth-module has rate-limiting
```

---

## Security

- **Encryption at rest** — XChaCha20-Poly1305 with Argon2id key derivation for config, sessions, logs, and memory content. See [docs/encryption.md](docs/encryption.md)
- **Memory content encryption** — hybrid encryption: text encrypted, embeddings searchable. Key rotation support
- **Slowloris protection** — ReadHeaderTimeout on all HTTP servers
- **Security scanning** — `gosec ./...` pre-release scan with `.gosec-exclude` for documented suppressions
- **Write-ahead log** — JSONL audit trail for all memory write operations
- **Content deduplication** — SHA-256 hash prevents storing identical memories
- **Bearer token auth** — API and WS connections protected by configurable token
- **TLS** — optional auto-generated or custom certificates with dual-port HTTP+HTTPS

---

## Architecture

The full top-level diagram lives on its own page so it can grow as new interfaces land
(mobile push, voice API, federation fan-out, ephemeral container workers, autonomous planning,
plugin framework, PRD-DAG orchestrator, …) without bloating the README.

➡ **[docs/architecture-overview.md](docs/architecture-overview.md)** — one-screen Mermaid
diagram of every interface, subsystem and data path, with planned features called out.

For deeper drill-downs:

- [docs/architecture.md](docs/architecture.md) — package list, component diagram, session
  state machine, proxy mode (4 Mermaid diagrams)
- [docs/data-flow.md](docs/data-flow.md) — per-feature sequence diagrams
- [docs/plans/README.md](docs/plans/README.md) — open and planned features tracker

---

## Documentation Index

Full documentation lives in [docs/](docs/) — see [docs/README.md](docs/README.md) for a complete index with all flow diagrams.

### Getting Started

| Document | Description |
|---|---|
| [docs/setup.md](docs/setup.md) | Installation, backend setup, voice input, RTK, profiles, proxy mode, encryption |
| [docs/commands.md](docs/commands.md) | Complete command reference (messaging and CLI) |
| [docs/pwa-setup.md](docs/pwa-setup.md) | PWA setup with Tailscale |

### Backends

| Document | Description |
|---|---|
| [docs/llm-backends.md](docs/llm-backends.md) | All LLM backends — claude-code, aider, goose, gemini, opencode, ollama, openwebui, shell |
| [docs/messaging-backends.md](docs/messaging-backends.md) | All messaging backends — Signal, Telegram, Discord, Slack, Matrix, Twilio, ntfy, email, webhooks, DNS; voice input; feature parity matrix |

### Interfaces & Integration

| Document | Description |
|---|---|
| [docs/mcp.md](docs/mcp.md) | MCP server — 60+ tools for Cursor, Claude Desktop, VS Code, remote AI agents via SSE |
| [docs/api/autonomous.md](docs/api/autonomous.md) | Autonomous PRD decomposition with verification — operator + AI-ready usage |
| [docs/api/plugins.md](docs/api/plugins.md) | Subprocess plugin framework — manifest format, hooks, security disclosure, Python example |
| [docs/api/orchestrator.md](docs/api/orchestrator.md) | PRD-DAG orchestrator + guardrails — graph model, verdict aggregation, CLI/MCP |
| [docs/api-mcp-mapping.md](docs/api-mcp-mapping.md) | API ↔ MCP tool mapping — full coverage analysis, gap documentation |
| [docs/claude-channel.md](docs/claude-channel.md) | MCP channel server for Claude Code (per-session channels) |
| [docs/rtk-integration.md](docs/rtk-integration.md) | RTK token savings — setup, config, stats dashboard, supported backends |
| [internal/server/web/openapi.yaml](internal/server/web/openapi.yaml) | OpenAPI 3.0 REST API specification |

### Memory & Intelligence

| Document | Description |
|---|---|
| [docs/memory.md](docs/memory.md) | Episodic memory — architecture, flow diagrams, configuration, MCP tools, REST API, monitoring |
| [docs/memory-usage-guide.md](docs/memory-usage-guide.md) | Practical examples — how to use memory in development workflows, all channels, PostgreSQL setup |

### Operations & Security

| Document | Description |
|---|---|
| [docs/operations.md](docs/operations.md) | Service management, upgrading, CLI, config, monitoring, proxy mode, security, troubleshooting |
| [docs/config-reference.yaml](docs/config-reference.yaml) | Complete annotated config file reference (all fields, defaults, comments) |
| [docs/encryption.md](docs/encryption.md) | Encryption at rest — enable at any time, XChaCha20-Poly1305, export, env variable |
| [docs/multi-session.md](docs/multi-session.md) | Multi-machine configuration |
| [docs/uninstall.md](docs/uninstall.md) | Manual uninstall for all installation methods |

### Architecture & Flows

| Document | Description |
|---|---|
| [docs/architecture-overview.md](docs/architecture-overview.md) | Top-level one-screen Mermaid map of every interface, subsystem and data path (incl. planned features) |
| [docs/architecture.md](docs/architecture.md) | Component overview, diagrams, proxy mode architecture (4 Mermaid diagrams) |
| [docs/data-flow.md](docs/data-flow.md) | Index linking to all 11 flow diagrams (session, input, WS, proxy, DNS, etc.) |
| [docs/covert-channels.md](docs/covert-channels.md) | DNS tunneling and covert channel design |

### Testing & Validation

| Document | Description |
|---|---|
| [docs/testing.md](docs/testing.md) | Testing procedures, interface validation tracker, feature test results (179 tests) |
| [docs/channel-testing.md](docs/channel-testing.md) | MCP channel testing guide — manual test procedures |
| [install/](install/) | Platform-specific installers |

---

## Prerequisites

| Dependency | Version | Notes |
|---|---|---|
| [signal-cli](https://github.com/AsamK/signal-cli) | >= 0.13 | Signal protocol bridge (only if using Signal) |
| Java | >= 17 | Required by signal-cli |
| [tmux](https://github.com/tmux/tmux) | Any recent | Session management |
| [claude CLI](https://docs.anthropic.com/en/docs/claude-code) | Latest | The `claude` binary (default LLM backend) |
| [Tailscale](https://tailscale.com) | Any | Optional — for PWA access |

---

## Installation

### Linux (one-liner)

```bash
curl -fsSL https://raw.githubusercontent.com/dmz006/datawatch/main/install/install.sh | bash
```

Installs to `~/.local/bin` for non-root users, `/usr/local/bin` for root. Includes systemd service.

### From source

```bash
git clone https://github.com/dmz006/datawatch
cd datawatch
go build -o bin/datawatch ./cmd/datawatch
sudo mv bin/datawatch /usr/local/bin/
```

---

## Quick Start

**1. Initialize configuration**

```bash
datawatch config init
```

This creates `~/.datawatch/config.yaml` with sensible defaults.

**2. Set up a messaging backend**

```bash
# Interactive wizard — choose your preferred backend:
datawatch setup telegram    # Telegram bot
datawatch setup discord     # Discord bot
datawatch setup slack       # Slack app
datawatch setup signal      # Signal (requires signal-cli and Java)
datawatch setup web         # Web UI only (no messaging backend needed)
# ... see `datawatch setup --help` for all options
```

**3. Start the daemon**

```bash
datawatch start
```

**4. Verify it works**

Send `help` in the configured channel. You should receive the command reference.

See [docs/setup.md](docs/setup.md) for full installation instructions and per-backend setup guides.

---

## Commands

All commands are sent as plain text messages in the configured group.

| Command | Description | Example |
|---|---|---|
| `new: <task>` | Start a new AI coding session | `new: add error handling to api.go` |
| `list` | List sessions and their current state | `list` |
| `status <id>` | Show recent output from a session | `status a3f2` |
| `tail <id> [n]` | Show last N lines of output (default: 20) | `tail a3f2 50` |
| `send <id>: <msg>` | Send input to a session waiting for input | `send a3f2: yes` |
| `kill <id>` | Terminate a running session | `kill a3f2` |
| `attach <id>` | Get the tmux attach command for SSH access | `attach a3f2` |
| `help` | Show this command reference | `help` |

See [docs/commands.md](docs/commands.md) for the full CLI reference including `session rename`, `session stop-all`, `backend list`, `completion`, `cmd`, `seed`, `update`, `setup server`, and `session schedule`.

**Implicit reply:** If exactly one session on a host is waiting for input, you can reply
without specifying the session ID — just type your response directly.

---

## AI Directory Constraints

Each session runs inside a configured project directory. claude-code receives the
`--add-dir` flag pointing to that directory, limiting its file system access to that
tree. This prevents accidental edits outside your project.

**How project directory is resolved:**

| Context | Directory used |
|---|---|
| Messaging `new:` command | `session.default_project_dir` in config |
| `datawatch session new` CLI | Current working directory (`$PWD`) |
| Explicit `--dir` flag (CLI) | The specified path |

**Automatic git tracking:**

When `session.auto_git_commit: true` (default), the daemon:
1. Creates a pre-session snapshot commit before launching the AI assistant.
2. Creates a post-session commit after the session completes.

---

## CLI Session Management

The `session` subcommand provides local session management without messaging:

```bash
# Start a new session in the current directory
datawatch session new "refactor the database layer"

# Start with a name and specific backend
datawatch session new --name "auth refactor" --backend aider "refactor the auth module"

# List all sessions (shows name, backend, state)
datawatch session list

# Show session status and recent output
datawatch session status <id>

# Tail session output
datawatch session tail <id> [n]

# Send input to a waiting session
datawatch session send <id> "yes"

# Rename a session
datawatch session rename <id> "my session name"

# Kill a session
datawatch session kill <id>

# Kill all running sessions on this host
datawatch session stop-all

# Get tmux attach command
datawatch session attach <id>
```

**Command library:**

```bash
# Save a named reusable command
datawatch cmd add <name> <command>

# List saved commands
datawatch cmd list

# Delete a saved command
datawatch cmd delete <name>

# Pre-populate default commands and filters
datawatch seed
```

| Command | Description | Example |
|---|---|---|
| `datawatch cmd add <name> <cmd>` | Save a named command for reuse | `datawatch cmd add approve "yes"` |
| `datawatch seed` | Pre-populate default commands and filters | `datawatch seed` |

---

## PWA

The built-in web server serves a mobile-first Progressive Web App for real-time
session management from any browser on your Tailscale network.

**URL:** `http://<tailscale-ip>:8080`

**Swagger UI:** `http://<tailscale-ip>:8080/api/docs`

**Install on Android:** Chrome > three-dot menu > Add to Home Screen

### Dashboard Features

- **Sessions view** — live session list with state badges, xterm.js terminal with ANSI rendering
- **Alerts tab** — unread badge counter, real-time push via WebSocket
- **Settings → Monitor** — system resource dashboard with:
  - CPU, Memory, Disk, Swap, GPU progress bars with color coding
  - Network stats (per-process when eBPF active, system-wide otherwise)
  - Daemon stats: memory RSS, goroutines, file descriptors, uptime
  - Infrastructure: web server URL, MCP SSE endpoint, TLS status, tmux sessions
  - Session statistics with expandable resource details per session
  - **Chat Channels** — expandable list of messaging/infra channels with message counts, bytes in/out, errors, last activity
  - **LLM Backends** — expandable list with total/active sessions, average duration, prompts per session
  - Communication channel stats update every 5 seconds via WebSocket
- **Settings → LLM** — detection filters, saved commands, output filters
- **Settings → Comms** — server connection status, remote server management
- **Settings → General** — daemon configuration, interface bindings, TLS
- **Settings → About** — version info, update check, restart

---

## Multi-Machine

Run `datawatch` on multiple machines, all connected to the same group.
Each machine's messages are prefixed with `[hostname]` so you always know which
machine is replying.

---

## Configuration

Config file: `~/.datawatch/config.yaml`

Generate a fully commented config with all fields and defaults:

```bash
datawatch config generate > ~/.datawatch/config.yaml
```

See [`docs/config-reference.yaml`](docs/config-reference.yaml) for the complete annotated reference.

**Minimal config** (everything else uses defaults):

```yaml
signal:
  account_number: +12125551234
  group_id: <base64>

session:
  llm_backend: claude-code
  default_project_dir: ~/projects
```

**Key sections:** Identity (hostname, data_dir), Session (LLM backend, timeouts, git, console size), Web Server (host, port, TLS, token), MCP Server (stdio + SSE), Signal, Messaging Backends (10 backends), DNS Channel, LLM Backends (10 backends with per-LLM console size and detection patterns), Detection Filters, Auto-Update, Remote Servers.

All settings are editable through **three interfaces**:

- **Web UI** — Settings page with sections for every config area, managed list for detection filters, interface selectors, LLM config popups
- **CLI** — `datawatch setup <service>` interactive wizards for each backend, `datawatch config generate` for annotated config, `datawatch config show` to view current config
- **Messaging channels** — `configure <key>=<value>` command via Signal, Telegram, Discord, Slack, Matrix, DNS, or any other enabled messaging backend. Example: `configure session.console_cols=120`

---

## Multi-Machine Setup

Each machine runs its own independent datawatch daemon with its own config, sessions, and data directory.

### Channel Configurations

**Default: unique channel per machine** (recommended)

By default, `datawatch config init` creates a Signal group named `datawatch-<hostname>` for each machine. This gives each machine its own private control channel:

```
Your phone
  ├── Signal group "datawatch-workstation" → workstation daemon
  ├── Signal group "datawatch-pi" → pi daemon
  └── Signal group "datawatch-laptop" → laptop daemon
```

Commands sent to a group only reach that machine. Session IDs include the hostname (`workstation-a3f2`) for clarity.

**Optional: shared channel across machines**

You can manually set the same `group_id` on multiple machines to create a shared control channel. All machines in the group see every command and respond:

```yaml
# Same group_id on workstation and pi:
signal:
  group_id: <same-base64-group-id>
```

```
Signal Group (shared)
  ├── workstation → responds with [workstation] prefix
  ├── pi → responds with [pi] prefix
  └── your phone

You send: "list"
workstation: [workstation] 2 active sessions...
pi: [pi] 1 active session...
```

This also works with Telegram (same `chat_id`), Discord (same `channel_id`), Slack (same `channel_id`), or Matrix (same `room_id`).

**Other messaging backends** (ntfy, email, webhooks) are send-only or inbound-only and work independently per machine.

### Web UI Remote Proxy

The web UI can proxy API calls to other datawatch instances for centralized management:

```yaml
servers:
  - name: workstation
    url: http://203.0.113.10:8080
    token: "bearer-token-for-workstation"
    enabled: true
  - name: pi
    url: http://198.51.100.50:8080
    token: "bearer-token-for-pi"
    enabled: true
```

In Settings → Servers, click a remote server to switch the web UI to that machine. All API calls proxy through your local instance.

### CLI Remote Targeting

```bash
datawatch --server workstation session list
datawatch --server pi session start --task "run tests" --dir ~/project
datawatch session list   # local (default)
```

### Setup

```bash
datawatch setup server   # interactive wizard to add remote instances
```

### DNS Channel

Each machine can run its own DNS server on a unique subdomain for covert control:
- `ws.example.com` → workstation
- `pi.example.com` → pi

Or a single DNS server routes to multiple machines via messaging. All queries are HMAC-authenticated with nonce replay protection.

---

## Interfaces & APIs

datawatch exposes multiple control interfaces:

| Interface | Protocol | Use Case |
|-----------|----------|----------|
| **Web UI / PWA** | HTTP/WebSocket | Browser-based session management with xterm.js terminal |
| **REST API** | HTTP JSON | Programmatic session control, config management |
| **MCP Server** | stdio / HTTP SSE | IDE integration (Cursor, Claude Desktop, VS Code) |
| **Messaging** | Signal, Telegram, Discord, Slack, Matrix, Twilio, ntfy, Email, DNS, Webhook | Remote command & control |
| **CLI** | Shell | Session management, setup wizards, config generation |
| **WebSocket** | WS/WSS | Real-time session output, state changes, terminal streaming |

**CLI commands:** `start`, `stop`, `restart`, `status`, `session list|start|kill|attach`, `setup`, `config`, `seed`, `about`, `version`, `update`, `backend list`, `logs`, `export`

**Messaging commands:** `new`, `list`, `status`, `send`, `kill`, `tail`, `attach`, `history`, `schedule`, `alerts`, `stats`, `configure`, `version/about`, `help`

### MCP Tools

The MCP server exposes **132 tools** (v5.26.3 count) for AI agents to drive the full feature surface. A representative sample:

| Area | Tools (sample) |
|------|----------------|
| Sessions | `list_sessions`, `start_session`, `send_input`, `session_output`, `session_timeline`, `kill_session`, `restart_session`, `delete_session`, `session_bind_agent`, `session_import`, `session_reconcile`, `session_rollback`, `sessions_stale`, `stop_all_sessions` |
| Memory + KG | `memory_recall`, `memory_remember`, `memory_stats`, `memory_export`, `memory_import`, `memory_reindex`, `kg_query`, `kg_add`, `kg_timeline`, `research_sessions`, `copy_response`, `get_prompt` |
| Cost + audit | `cost_summary`, `cost_usage`, `cost_rates`, `audit_query`, `analytics` |
| Operations | `diagnose`, `reload`, `restart_daemon`, `cooldown_status`, `sessions_stale`, `splash_info` |
| Autonomous | `autonomous_status`, `autonomous_config_get/set`, `autonomous_prd_create/decompose/edit/edit_task/delete/instantiate/run/cancel`, `autonomous_prd_approve/reject/request_revision`, `autonomous_prd_set_llm/set_task_llm`, `autonomous_prd_children`, `autonomous_learnings` |
| Observer | `observer_stats`, `observer_envelopes`, `observer_envelopes_all_peers`, `observer_peer_list/get/register/delete/stats`, `observer_agent_list/stats`, `observer_config_get/set`, `ollama_stats` |
| Agents | `agent_spawn`, `agent_list`, `agent_get`, `agent_logs`, `agent_terminate`, `agent_audit` |
| Plugins | `plugins_list`, `plugins_reload`, `plugin_get`, `plugin_enable`, `plugin_disable`, `plugin_test` |
| Orchestrator | `orchestrator_graph_create/plan/run/get/list/cancel`, `orchestrator_verdicts`, `orchestrator_config_get/set` |
| Profiles + projects | `profile_create/update/delete/get/list/smoke`, `project_list/summary/upsert`, `project_alias_delete` |
| Templates / scheduling / cooldown / devices / routing | `template_*`, `schedule_*`, `cooldown_*`, `device_alias_*`, `routing_rules_*` |
| Pipelines + saved commands + ask | `pipeline_*`, `list_saved_commands`, `send_saved_command`, `ask`, `assist` |

The authoritative live list is at `GET /api/mcp/docs`. Full reference: [`docs/mcp.md`](docs/mcp.md). Connect via stdio (Cursor, Claude Desktop, VS Code) or HTTP SSE (remote agents). See [docs/cursor-mcp.md](docs/cursor-mcp.md) for IDE setup.

### REST API

OpenAPI 3.0 spec: [docs/api/openapi.yaml](docs/api/openapi.yaml) — browse at `https://<host>:8443/api/docs`. Per-area reference docs in [`docs/api/`](docs/api/). High-traffic endpoints:

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/sessions` | GET / POST | List / start sessions |
| `/api/sessions/{id}` | GET / DELETE | Session detail / kill |
| `/api/sessions/{id}/state` | POST | Override session state |
| `/api/sessions/{id}/output` | GET | Tail session output |
| `/api/sessions/{id}/rollback` | POST | Checkout pre-run git tag |
| `/api/sessions/import` | POST | Import a foreign tmux record |
| `/api/sessions/reconcile` | POST | Reconcile post-restart state |
| `/api/sessions/stale` | GET | Stale sessions (tmux gone) |
| `/api/config` | GET / PUT | Read / patch config (dot-path keys) |
| `/api/reload` | POST | Hot-reload config (BL17) |
| `/api/restart` | POST | Restart daemon in-place |
| `/api/update` | POST | Self-update install + restart |
| `/api/stats` | GET | System metrics (CPU, mem, disk, GPU, tmux, uptime) |
| `/api/observer/stats` | GET | Observer roll-up |
| `/api/observer/envelopes` | GET | Per-host envelopes |
| `/api/observer/envelopes/all-peers` | GET | Cross-host federation view (BL180) |
| `/api/observer/peers` | GET / POST | Peer registry (BL172) |
| `/api/observer/peers/{name}` | GET / DELETE | Peer detail / unregister |
| `/api/autonomous/prds` | GET / POST | List / create PRDs |
| `/api/autonomous/prds/{id}` | GET / PATCH / DELETE | PRD CRUD (PATCH = edit, DELETE+`?hard=true` = remove) |
| `/api/autonomous/prds/{id}/{action}` | POST | `decompose`, `run`, `approve`, `reject`, `request_revision`, `cancel`, `set_llm`, `set_task_llm`, `instantiate`, `edit_task` |
| `/api/autonomous/prds/{id}/children` | GET | Child PRDs (BL191 Q4) |
| `/api/orchestrator/graphs` | GET / POST | Graph CRUD |
| `/api/orchestrator/graphs/{id}/{action}` | POST | `plan`, `run`, `cancel` |
| `/api/agents` | GET / POST | Agent worker CRUD |
| `/api/agents/bootstrap` | POST | Worker pinned-mTLS bootstrap (parent-side) |
| `/api/profiles/projects` + `/api/profiles/clusters` | GET/POST/PUT/DELETE | Profile CRUD |
| `/api/projects/summary` | POST | Project summary one-shot |
| `/api/memory/*` | various | Memory + KG operations |
| `/api/channel/reply` | POST | Inbound channel message (claude MCP / opencode-acp) |
| `/api/channel/send` | POST | Outbound channel message |
| `/api/channel/history` | GET | Per-session channel ring buffer (v5.26.1) |
| `/api/channel/ready` | POST | Channel-server ready callback |
| `/api/voice/transcribe` | POST | Whisper / OpenAI / OpenWebUI / Ollama transcription |
| `/api/devices` | GET / POST / DELETE | Mobile push-token registry |
| `/api/proxy/{server}/{rest}` | various | Remote-server proxy passthrough (BL90) |
| `/api/test/message` | POST | Dry-run a comm-channel inbound through the router |
| `/api/diagnose` | GET | Diagnostic battery (BL37) |
| `/api/health` / `/healthz` | GET | Daemon health check (no auth) |
| `/ws` | WebSocket | Real-time output + state + terminal + alerts + PRD updates |

---

## Extensibility

datawatch is designed for modularity. Both the LLM assistant and messaging
protocol are replaceable via Go interfaces.

### LLM Backends (`internal/llm`)

```go
type Backend interface {
    Name() string
    Launch(ctx context.Context, task, tmuxSession, projectDir, logFile string) error
    SupportsInteractiveInput() bool
    Version() string
}
```

Available: `claude-code`, `aider`, `goose`, `gemini`, `opencode`, `shell`.

### Messaging Backends (`internal/messaging`)

```go
type Backend interface {
    Name() string
    Send(recipient, message string) error
    Subscribe(ctx context.Context, handler func(Message)) error
    Link(deviceName string, onQR func(qrURI string)) error
    SelfID() string
    Close() error
}
```

Available: `signal`, `telegram`, `discord`, `slack`, `matrix`, `twilio`, `github` (webhook), `webhook` (generic), `dns` (covert channel), `ntfy` (send-only), `email` (send-only).

### Adding a New Component

When adding a new LLM backend, messaging backend, or feature:

1. **Implement the interface** — `llm.Backend` or `messaging.Backend`
2. **Register** in `cmd/datawatch/main.go` via `llm.Register()` or messaging registry
3. **Add config** — struct in `config.go`, fields in `DefaultConfig()`, template in `template.go`
4. **Add setup wizard** — `datawatch setup <name>` CLI command
5. **Expose in web UI** — `BACKEND_FIELDS` or `LLM_FIELDS` in `app.js`, API GET/PUT handlers
6. **Document** — update `docs/messaging-backends.md` or `docs/llm-backends.md`, `docs/backends.md` table, architecture diagram, `docs/config-reference.yaml`
7. **Test and document results** — add test procedures to `docs/testing.md` with API test commands, expected results, and user validation steps per `AGENT.md` rules
8. **Update CHANGELOG.md** — under `[Unreleased]` or the current version

**Minimum documentation for any new component:**
- Config reference entry with all fields and defaults
- Setup wizard or web UI config instructions
- Architecture diagram updated if it adds a new connection type
- Test evidence in `docs/testing.md`
- User-facing test plan in `docs/testing.md`

---

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup, code style, how to add
new backends, and the PR process.

---

## Security

See [SECURITY.md](SECURITY.md) for the vulnerability reporting process and a discussion
of the security model.

---

## License

[Polyform Noncommercial License 1.0.0](LICENSE)

Free for personal, educational, and open-source use.
Commercial use requires explicit written permission.

---

*Built for the home lab community.*

---

## Acknowledgements

Special thanks to **[Daniel Keys Moran](https://en.wikipedia.org/wiki/Daniel_Keys_Moran)** and his novel
**[The Long Run](https://www.amazon.com/Long-Run-Daniel-Keys-Moran/dp/1939888336)** — the story of Trent
the Uncatchable, a thief operating under the eye of an all-seeing AI surveillance network, sparked a
decades-long obsession with the intersection of technology, autonomy, and the systems that watch over us.
That spirit lives somewhere in this project.

> *"The DataWatch sees everything."*

If you haven't read it: [buy it on Amazon](https://www.amazon.com/Long-Run-Daniel-Keys-Moran/dp/1939888336)
(Kindle edition also available), or borrow it from the
[Internet Archive](https://archive.org/details/longruntaleofcon0000mora).
Daniel has also historically offered copies by email request via his
[blog](https://danielkeysmoran.blogspot.com).
