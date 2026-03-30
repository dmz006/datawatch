# datawatch

<p align="center"><img src="internal/server/web/icon-512.svg" width="180" alt="datawatch logo"/></p>

**Control AI coding sessions from your phone — via Signal, Telegram, Matrix, webhooks, and more.**

[![License: Polyform NC](https://img.shields.io/badge/license-Polyform%20NC%201.0-blue)](LICENSE)
[![Go version](https://img.shields.io/badge/go-1.22%2B-00ADD8)](https://go.dev)
[![Platform](https://img.shields.io/badge/platform-Linux%20%7C%20macOS%20%7C%20WSL2-lightgrey)](docs/setup.md)

`datawatch` is a daemon that bridges messaging platforms to AI coding sessions running in tmux. Send a task from your phone, go offline, and check back for results — all without SSH. It also ships a mobile-first Progressive Web App accessible over Tailscale.

---

## What it does

- Start an AI coding session by sending `new: <task>` in any configured group
- Receive automatic notifications when sessions complete or need your input
- Reply to AI prompts directly from Signal, Telegram, Matrix, or any webhook
- Monitor and manage multiple sessions across multiple machines from one group thread
- Stream live session output in a browser PWA over Tailscale
- Install the PWA to your Android/iOS home screen for one-tap access
- Persist sessions across daemon restarts with a JSON file store
- Pluggable LLM backend: claude-code, aider, goose, gemini, opencode, or a custom shell script
- Pluggable messaging backend: Signal, Telegram, Matrix, GitHub webhooks, generic webhooks
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

The PWA includes an **Alerts** tab with an unread badge counter that pushes new alerts
in real time via WebSocket. The **Settings** panel includes **Saved Commands** and
**Output Filters** sections for managing the command library and filter rules.

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

All settings are editable in the web UI under Settings.

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

Available: `signal`, `telegram`, `matrix`, `github` (webhook), `webhook` (generic), `ntfy` (send-only), `email` (send-only).

---

## API

The REST API is documented as an OpenAPI 3.0 spec at [docs/api/openapi.yaml](docs/api/openapi.yaml).
Browse it interactively at `http://<tailscale-ip>:8080/api/docs` (Swagger UI).

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
