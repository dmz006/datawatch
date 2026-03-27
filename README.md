# datawatch

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
Messaging Backends               Browser / PWA
  Signal (signal-cli)                  |
  Telegram Bot                         v
  Matrix Room          ─────► HTTP/WebSocket :8080
  GitHub Webhooks                      |
  Generic Webhooks                     |
       |                               |
       v                               |
  Router (command parser)   WebSocket Hub (broadcast)
       |                               |
       +───────────────────────────────+
                       |
                Session Manager
                       |
          +────────────+────────────+
          v                         v
     tmux sessions            sessions.json
          |                   (persistence)
          v
     LLM Backend
       claude-code / aider / goose / gemini / opencode / shell
          |
          v
  ~/.datawatch/logs/
```

---

## Documentation Index

Full documentation lives in [docs/](docs/) — see [docs/README.md](docs/README.md) for a complete index.

| Document | Description |
|---|---|
| [docs/setup.md](docs/setup.md) | Installation and setup guide |
| [docs/commands.md](docs/commands.md) | Complete command reference |
| [docs/llm-backends.md](docs/llm-backends.md) | All LLM backends (claude-code, aider, goose, gemini, opencode, ollama, openwebui, shell) |
| [docs/messaging-backends.md](docs/messaging-backends.md) | All messaging backends (Signal, Telegram, Matrix, Discord, Slack, Twilio, ntfy, email, webhooks) |
| [docs/mcp.md](docs/mcp.md) | MCP server — Cursor, Claude Desktop, VS Code, remote AI agents |
| [docs/pwa-setup.md](docs/pwa-setup.md) | PWA setup with Tailscale |
| [docs/operations.md](docs/operations.md) | Day-to-day operations guide |
| [docs/multi-session.md](docs/multi-session.md) | Multi-machine configuration |
| [docs/architecture.md](docs/architecture.md) | Architecture deep dive |
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

**1. Link your Signal device (optional — only needed for Signal backend)**

```bash
datawatch link
```

Scan the QR code with your Signal app: Settings > Linked Devices > Link New Device.

**2. Find your group ID**

```bash
signal-cli -u +12125551234 listGroups
```

Copy the base64 group ID of the group you want to use as the control channel.

**3. Initialize config**

```bash
datawatch config init
```

You will be prompted for your Signal number, group ID, and machine hostname.

**4. Start the daemon**

```bash
datawatch start
```

**5. Verify it works**

Send `help` in your configured group. You should receive the command reference.

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

# List all sessions
datawatch session list

# Show session status and recent output
datawatch session status <id>

# Tail session output
datawatch session tail <id> [n]

# Send input to a waiting session
datawatch session send <id> "yes"

# Kill a session
datawatch session kill <id>

# Get tmux attach command
datawatch session attach <id>
```

---

## PWA

The built-in web server serves a mobile-first Progressive Web App for real-time
session management from any browser on your Tailscale network.

**URL:** `http://<tailscale-ip>:8080`

**Swagger UI:** `http://<tailscale-ip>:8080/api/docs`

**Install on Android:** Chrome > three-dot menu > Add to Home Screen

---

## Multi-Machine

Run `datawatch` on multiple machines, all connected to the same group.
Each machine's messages are prefixed with `[hostname]` so you always know which
machine is replying.

---

## Configuration

Config file: `~/.datawatch/config.yaml`

```yaml
# Identifies this machine in messages and session IDs
hostname: my-server

# Where sessions, logs, and state are stored
data_dir: ~/.datawatch

signal:
  account_number: +12125551234         # Your Signal phone number
  group_id: <base64>                   # Signal group ID (from listGroups)
  config_dir: ~/.local/share/signal-cli
  device_name: my-server               # Shown in Signal's linked devices list

telegram:
  enabled: false
  token: ""                            # Bot token from @BotFather
  chat_id: 0                           # Chat/group ID

matrix:
  enabled: false
  homeserver: https://matrix.org
  user_id: "@bot:matrix.org"
  access_token: ""
  room_id: "!roomid:matrix.org"

ntfy:
  enabled: false
  server_url: https://ntfy.sh
  topic: ""
  token: ""

email:
  enabled: false
  host: smtp.example.com
  port: 587
  username: ""
  password: ""
  from: datawatch@example.com
  to: you@example.com

github_webhook:
  enabled: false
  addr: :9001
  secret: ""

webhook:
  enabled: false
  addr: :9002
  token: ""

session:
  max_sessions: 10                     # Max concurrent sessions per machine
  input_idle_timeout: 10               # Seconds idle before marking waiting_input
  tail_lines: 20                       # Default lines for tail/status commands
  claude_code_bin: claude              # Path to claude binary
  llm_backend: claude-code             # LLM backend to use
  default_project_dir: ~/projects      # Default working directory for new sessions
  auto_git_commit: true                # Git commit before/after each session
  auto_git_init: false                 # Auto-init git repo if none exists

aider:
  enabled: false
  binary: aider

goose:
  enabled: false
  binary: goose

gemini:
  enabled: false
  binary: gemini

opencode:
  enabled: false
  binary: opencode

shell_backend:
  enabled: false
  script_path: ""                      # Path to your shell script

server:
  enabled: true                        # Enable the PWA/WebSocket server
  host: 0.0.0.0                        # Bind address
  port: 8080                           # Listen port
  token: ""                            # Optional bearer token
```

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
