# datawatch

<p align="center"><img src="internal/server/web/icon-512.svg" width="180" alt="datawatch logo"/></p>

**Control AI coding sessions from your phone — via Signal, Telegram, Matrix, webhooks, and more.**

[![License: Polyform NC](https://img.shields.io/badge/license-Polyform%20NC%201.0-blue)](LICENSE)
[![Go version](https://img.shields.io/badge/go-1.22%2B-00ADD8)](https://go.dev)
[![Platform](https://img.shields.io/badge/platform-Linux%20%7C%20macOS%20%7C%20WSL2-lightgrey)](docs/setup.md)

`datawatch` is a daemon that bridges messaging platforms to AI coding sessions running in tmux. Send a task from your phone, go offline, and check back for results — all without SSH. It also ships a mobile-first Progressive Web App accessible over Tailscale.

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
- **Proxy mode** — relay commands and sessions across multiple machines from one group; aggregated session list, WS relay, `new: @server: task` routing
- **Test message endpoint** — `POST /api/test/message` simulates comm channel commands for testing without Signal/Telegram
- MCP (Model Context Protocol) server — 17 tools for IDE integration (Cursor, Claude Desktop, VS Code)
- Named sessions with resume — Claude sessions tagged with `--name` for easy identification and `/resume`
- Optional push notifications via ntfy and email
- Optional automatic git commits before and after each session

---

## Quick Demo

```
You (Signal/Telegram group):
  new: write unit tests for the auth package

[laptop] Session a3f2 started: write unit tests for the auth package

... 3 minutes later ...

[laptop] Session a3f2 waiting for input:
  Found 3 files to modify. Proceed? [y/N]

You:
  send a3f2: y

[laptop] Session a3f2 resumed.

... 2 minutes later ...

[laptop] Session a3f2 complete.
  Tests written: auth_test.go (14 tests, all passing)
```

---

## Architecture

```
 Messaging Backends              Browser / PWA           MCP Clients
   Signal (signal-cli)                |                  Cursor / Claude Desktop
   Telegram Bot                       v                  VS Code / Remote AI
   Matrix Room                  HTTP/WS :8080                |
   Discord Bot                        |              MCP stdio / SSE :8081
   Slack Bot                          |                      |
   Twilio SMS                         |                      |
   ntfy / Email (outbound)            |                      v
   GitHub Webhooks                    |              MCP Server (17 tools)
   Generic Webhooks                   |                      |
   DNS Channel (TXT queries)          |                      |
         |                            |                      |
         v                            v                      |
   Router (command parser) ◄── WebSocket Hub ◄───────────────+
         |                       (broadcast)
         |
         +──── Alert Store ──── Filter Engine
         |
    Session Manager ──── Session Reconciler (30s)
         |
    +────+────+────────────+
    v         v            v
  tmux    sessions.json  output.log(.enc)
  sessions  (encrypted)  (FIFO → XChaCha20)
    |
    v
  LLM Backends
    claude-code (MCP channel per session)
    opencode (interactive TUI)
    opencode-acp (HTTP/SSE serve mode)
    opencode-prompt (single-shot run)
    ollama (interactive / remote)
    openwebui (OpenAI-compatible API)
    aider / goose / gemini
    shell (interactive $SHELL)
    |
    v
  fsnotify ──► Output Monitor ──► Prompt Detection (1s fast path)
```

---

## Documentation Index

Full documentation lives in [docs/](docs/) — see [docs/README.md](docs/README.md) for a complete index.

| Document | Description |
|---|---|
| [docs/setup.md](docs/setup.md) | Installation and setup guide |
| [docs/commands.md](docs/commands.md) | Complete command reference |
| [docs/llm-backends.md](docs/llm-backends.md) | All LLM backends (claude-code, aider, goose, gemini, opencode, ollama, openwebui, shell) |
| [docs/messaging-backends.md](docs/messaging-backends.md) | All messaging backends (Signal, Telegram, Matrix, Discord, Slack, Twilio, ntfy, email, webhooks, DNS) |
| [docs/encryption.md](docs/encryption.md) | Encryption at rest — XChaCha20-Poly1305, export command, env variable |
| [docs/mcp.md](docs/mcp.md) | MCP server — Cursor, Claude Desktop, VS Code, remote AI agents |
| [docs/claude-channel.md](docs/claude-channel.md) | MCP channel server for Claude Code (per-session channels) |
| [docs/pwa-setup.md](docs/pwa-setup.md) | PWA setup with Tailscale |
| [docs/operations.md](docs/operations.md) | Day-to-day operations guide |
| [docs/multi-session.md](docs/multi-session.md) | Multi-machine configuration |
| [docs/architecture.md](docs/architecture.md) | Architecture deep dive |
| [docs/covert-channels.md](docs/covert-channels.md) | DNS tunneling and covert channel design |
| [docs/rtk-integration.md](docs/rtk-integration.md) | RTK token savings integration — setup, config, stats, supported backends |
| [docs/channel-testing.md](docs/channel-testing.md) | MCP channel testing guide — manual test procedures |
| [docs/testing-tracker.md](docs/testing-tracker.md) | Interface validation status |
| [docs/uninstall.md](docs/uninstall.md) | Manual uninstall for all installation methods |
| [docs/api/openapi.yaml](docs/api/openapi.yaml) | OpenAPI 3.0 specification |
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
    url: http://192.168.1.10:8080
    token: "bearer-token-for-workstation"
    enabled: true
  - name: pi
    url: http://192.168.1.50:8080
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

The MCP server exposes tools for AI agents to manage sessions programmatically:

| Tool | Description |
|------|-------------|
| `datawatch-session-list` | List all sessions with state, backend, timestamps |
| `datawatch-session-start` | Start a new session with task, backend, project dir |
| `datawatch-session-send` | Send input/command to a running session |
| `datawatch-session-status` | Get session state, last output, prompt info |
| `datawatch-session-kill` | Terminate a session |
| `datawatch-session-reply` | Send a channel reply back to the monitoring system |

Connect via stdio (Cursor, Claude Desktop, VS Code) or HTTP SSE (remote agents). See [docs/cursor-mcp.md](docs/cursor-mcp.md) for IDE setup.

### REST API

OpenAPI 3.0 spec: [docs/api/openapi.yaml](docs/api/openapi.yaml) — browse at `http://<host>:8080/api/docs`

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/sessions` | GET | List all sessions |
| `/api/sessions/start` | POST | Start a new session |
| `/api/sessions/kill` | POST | Stop a session |
| `/api/sessions/state` | POST | Override session state |
| `/api/config` | GET | Read config (secrets masked) |
| `/api/config` | PUT | Patch config (dot-path keys) |
| `/api/stats` | GET | System metrics (CPU, memory, disk, GPU, tmux, uptime) |
| `/api/stats/kill-orphans` | POST | Kill orphaned tmux sessions |
| `/api/schedules` | GET/POST/PUT/DELETE | Manage scheduled events |
| `/api/interfaces` | GET | List available network interfaces |
| `/api/backends` | GET | List LLM backends with availability |
| `/api/health` | GET | Daemon health check (no auth) |
| `/ws` | WebSocket | Real-time output, state changes, terminal streaming |

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
