# claude-signal

**Control claude-code sessions from your phone via Signal.**

[![License: Polyform NC](https://img.shields.io/badge/license-Polyform%20NC%201.0-blue)](LICENSE)
[![Go version](https://img.shields.io/badge/go-1.22%2B-00ADD8)](https://go.dev)
[![Platform](https://img.shields.io/badge/platform-Linux%20%7C%20macOS%20%7C%20WSL2-lightgrey)](docs/setup.md)

`claude-signal` is a daemon that bridges a Signal group chat to `claude-code` tmux sessions. Send a task from your phone, go offline, and check back for results — all without SSH. It also ships a mobile-first Progressive Web App accessible over Tailscale.

---

## What it does

- Start a `claude-code` session by sending `new: <task>` in a Signal group
- Receive automatic notifications when sessions complete or need your input
- Reply to claude-code prompts directly from Signal without SSH
- Monitor and manage multiple sessions across multiple machines from one group thread
- Stream live session output in a browser PWA over Tailscale
- Install the PWA to your Android/iOS home screen for one-tap access
- Persist sessions across daemon restarts with a JSON file store
- Pluggable LLM backend (implement `llm.Backend` to add aider, Codex, etc.)
- Pluggable messaging backend (implement `messaging.Backend` for Slack, Discord, etc.)
- Optional automatic git commits before and after each session
- Constrain claude-code to a project directory via `--add-dir`

---

## Quick Demo

```
You (Signal group):
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
Signal Group (phone)                  Browser / PWA
       |                                    |
       v                                    v
  signal-cli                     HTTP/WebSocket :8080
  (JSON-RPC daemon)              (over Tailscale)
       |                                    |
       v                                    |
  SignalCLI Backend ─────────────────────── |
       |                                    |
       v                                    |
  Router (command parser)      WebSocket Hub (broadcast)
       |                                    |
       +────────────────────────────────────+
                        |
                 Session Manager
                        |
           +────────────+────────────+
           v                         v
      tmux sessions            sessions.json
           |                   (persistence)
           v
      claude-code
      (--add-dir <projectDir>)
           |
           v
   ~/.claude-signal/logs/
```

---

## Documentation Index

| Document | Description |
|---|---|
| [docs/design.md](docs/design.md) | Design goals and philosophy |
| [docs/planning.md](docs/planning.md) | Roadmap and feature planning |
| [docs/implementation.md](docs/implementation.md) | Implementation notes and internals |
| [docs/data-flow.md](docs/data-flow.md) | Message and data flow diagrams |
| [docs/app-flow.md](docs/app-flow.md) | Application state machine |
| [docs/operations.md](docs/operations.md) | Day-to-day operations guide |
| [docs/api/openapi.yaml](docs/api/openapi.yaml) | OpenAPI 3.0 specification |
| [docs/commands.md](docs/commands.md) | Complete Signal command reference |
| [docs/setup.md](docs/setup.md) | Installation and setup guide |
| [docs/pwa-setup.md](docs/pwa-setup.md) | PWA setup with Tailscale |
| [docs/multi-session.md](docs/multi-session.md) | Multi-machine configuration |
| [docs/architecture.md](docs/architecture.md) | Architecture deep dive |
| [docs/future-native-signal.md](docs/future-native-signal.md) | Native Go Signal backend roadmap |
| [install/](install/) | Platform-specific installers |

---

## Prerequisites

| Dependency | Version | Notes |
|---|---|---|
| [signal-cli](https://github.com/AsamK/signal-cli) | >= 0.13 | Signal protocol bridge |
| Java | >= 17 | Required by signal-cli |
| [tmux](https://github.com/tmux/tmux) | Any recent | Session management |
| [claude CLI](https://docs.anthropic.com/en/docs/claude-code) | Latest | The `claude` binary |
| [Tailscale](https://tailscale.com) | Any | Optional — for PWA access |

---

## Installation

### Linux (one-liner)

```bash
curl -fsSL https://raw.githubusercontent.com/dmz006/claude-signal/main/install/install.sh | bash
```

Installs to `~/.local/bin` for non-root users, `/usr/local/bin` for root. Includes systemd service.

### macOS

```bash
curl -fsSL https://raw.githubusercontent.com/dmz006/claude-signal/main/install/install-macos.sh | bash
```

Installs as a LaunchAgent.

### From source

```bash
git clone https://github.com/dmz006/claude-signal
cd claude-signal
go build -o bin/claude-signal ./cmd/claude-signal
sudo mv bin/claude-signal /usr/local/bin/
```

---

## Quick Start

**1. Link your Signal device**

```bash
claude-signal link
```

Scan the QR code with your Signal app: Settings > Linked Devices > Link New Device.

**2. Find your group ID**

```bash
signal-cli -u +12125551234 listGroups
```

Copy the base64 group ID of the group you want to use as the control channel.

**3. Initialize config**

```bash
claude-signal config init
```

You will be prompted for your Signal number, group ID, and machine hostname.

**4. Start the daemon**

```bash
claude-signal start
```

**5. Verify it works**

Send `help` in your Signal group. You should receive the command reference.

---

## Signal Commands

All commands are sent as plain text messages in the configured Signal group.

| Command | Description | Example |
|---|---|---|
| `new: <task>` | Start a new claude-code session | `new: add error handling to api.go` |
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

## claude-code Directory Constraints

Each session runs inside a configured project directory. claude-code receives the
`--add-dir` flag pointing to that directory, limiting its file system access to that
tree. This prevents accidental edits outside your project.

**How project directory is resolved:**

| Context | Directory used |
|---|---|
| Signal / PWA `new:` command | `session.default_project_dir` in config |
| `claude-signal session new` CLI | Current working directory (`$PWD`) |
| Explicit `--dir` flag (CLI) | The specified path |

**Automatic git tracking:**

When `session.auto_git_commit: true` (default), the daemon:
1. Creates a pre-session snapshot commit before launching claude-code.
2. Creates a post-session commit after the session completes.

This gives you a clean audit trail of changes made by each session.

```yaml
session:
  auto_git_commit: true   # commit before and after each session
  auto_git_init: false    # auto-init a repo if none exists
  default_project_dir: ~/projects/my-app
```

---

## CLI Session Management

The `session` subcommand provides local session management without Signal:

```bash
# Start a new session in the current directory
claude-signal session new "refactor the database layer"

# List all sessions
claude-signal session list

# Show session status and recent output
claude-signal session status <id>

# Tail session output
claude-signal session tail <id> [n]

# Send input to a waiting session
claude-signal session send <id> "yes"

# Kill a session
claude-signal session kill <id>

# Get tmux attach command
claude-signal session attach <id>
```

---

## PWA

The built-in web server serves a mobile-first Progressive Web App for real-time
session management from any browser on your Tailscale network.

**URL:** `http://<tailscale-ip>:8080`

**Swagger UI:** `http://<tailscale-ip>:8080/api/docs`

**Install on Android:** Chrome > three-dot menu > Add to Home Screen

Features:
- Live-updating session list with state indicators
- Real-time output streaming for running sessions
- Browser push notifications when a session needs input
- Input bar for responding to prompts
- QR code linking flow (no terminal needed)

See [docs/pwa-setup.md](docs/pwa-setup.md) for Tailscale configuration, token auth,
and troubleshooting.

---

## Multi-Machine

Run `claude-signal` on multiple machines, all connected to the same Signal group.
Each machine's messages are prefixed with `[hostname]` so you always know which
machine is replying.

```
You: list

[laptop] Sessions:
  [a3f2] running        14:32:01
    Task: refactor database layer

[desktop] Sessions:
  [b7c1] waiting_input  14:45:22
    Task: deploy to staging
```

Session IDs are prefixed with the hostname (e.g. `laptop-a3f2`) to avoid collisions.

See [docs/multi-session.md](docs/multi-session.md) for detailed configuration.

---

## Configuration

Config file: `~/.claude-signal/config.yaml`

```yaml
# Identifies this machine in Signal messages and session IDs
hostname: my-server

# Where sessions, logs, and state are stored
data_dir: ~/.claude-signal

signal:
  account_number: +12125551234         # Your Signal phone number
  group_id: <base64>                   # Signal group ID (from listGroups)
  config_dir: ~/.local/share/signal-cli
  device_name: my-server               # Shown in Signal's linked devices list

session:
  max_sessions: 10                     # Max concurrent sessions per machine
  input_idle_timeout: 10               # Seconds idle before marking waiting_input
  tail_lines: 20                       # Default lines for tail/status commands
  claude_code_bin: claude              # Path to claude binary
  llm_backend: claude-code             # LLM backend to use (default: claude-code)
  default_project_dir: ~/projects      # Default working directory for new sessions
  auto_git_commit: true                # Git commit before/after each session
  auto_git_init: false                 # Auto-init git repo if none exists

server:
  enabled: true                        # Enable the PWA/WebSocket server
  host: 0.0.0.0                        # Bind address (0.0.0.0 = all interfaces)
  port: 8080                           # Listen port
  token: ""                            # Optional bearer token (empty = no auth)
  tls_cert: ""                         # Optional TLS certificate path
  tls_key: ""                          # Optional TLS key path
```

---

## Extensibility

claude-signal is designed for modularity. Both the LLM assistant and messaging
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

Current: `claude-code`. Planned: `aider`, OpenAI Codex CLI, custom scripts.

To add a backend: implement the interface, call `llm.Register(b)` in `init()`,
and blank-import your package in `cmd/claude-signal/main.go`.

See [CONTRIBUTING.md](CONTRIBUTING.md) for a full walkthrough.

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

Current: `signal` (via signal-cli JSON-RPC). Planned: Slack, Discord, Telegram, Matrix.

---

## API

The REST API is documented as an OpenAPI 3.0 spec at [docs/api/openapi.yaml](docs/api/openapi.yaml).
Browse it interactively at `http://<tailscale-ip>:8080/api/docs` (Swagger UI).

Key endpoints: `GET /api/sessions`, `POST /api/sessions`, `GET /api/sessions/{id}/output`,
`POST /api/sessions/{id}/command`, `GET /api/health`, `POST /api/link`.

---

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup, code style, how to add
new backends, and the PR process.

---

## Security

See [SECURITY.md](SECURITY.md) for the vulnerability reporting process and a discussion
of the security model (Signal account access, network exposure, file system scope,
and systemd hardening).

---

## License

[Polyform Noncommercial License 1.0.0](LICENSE)

Free for personal, educational, and open-source use.
Commercial use requires explicit written permission.

---

*Built for the home lab community.*
