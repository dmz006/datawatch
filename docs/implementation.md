# Implementation Guide — datawatch

This document is intended for developers who want to understand, modify, or extend `datawatch`.

---

## 1. Project Layout

```
datawatch/
├── cmd/
│   └── datawatch/          # CLI entry point (cobra commands)
│       ├── main.go             # Root command, start, link, config, session subcommands
│       └── ...
├── internal/
│   ├── alerts/
│   │   └── store.go            # Alert, Store — persistent system alert log
│   ├── config/
│   │   ├── config.go           # Config struct, Load(), Save(), LoadSecure(), SaveSecure(), applyDefaults()
│   │   └── encrypt.go          # AES-256-GCM encryption helpers (IsEncrypted, Encrypt, Decrypt, DeriveKey, LoadOrGenerateSalt)
│   ├── secfile/
│   │   └── secfile.go          # AES-256-GCM file encryption helpers (Encrypt/Decrypt/ReadFile/WriteFile)
│   ├── llm/
│   │   ├── backend.go          # llm.Backend interface definition
│   │   ├── registry.go         # Register() and Get() for named backends
│   │   └── claudecode/
│   │       └── backend.go      # claude-code implementation of llm.Backend
│   ├── messaging/
│   │   └── backend.go          # messaging.Backend interface + registry
│   ├── router/
│   │   └── commands.go         # Parse(), HelpText(), Command types (including CmdSetup)
│   ├── server/
│   │   ├── server.go           # HTTP server setup, mux routing, auth middleware
│   │   ├── api.go              # REST API handlers (/api/sessions, /api/config, etc.)
│   │   └── ws.go               # WebSocket hub, client pumps, message types
│   ├── wizard/
│   │   ├── wizard.go           # WizardManager, WizardSession, Step, Def — stateful multi-turn wizard engine
│   │   └── defs.go             # Wizard definitions for all 12 services (signal/telegram/.../web/server)
│   ├── session/
│   │   ├── store.go            # Session struct, Store (JSON persistence), state constants
│   │   ├── schedule.go         # ScheduledCommand, ScheduleStore — persistent command scheduler
│   │   ├── cmdlib.go           # SavedCommand, CmdLibrary — named reusable command library
│   │   └── filter.go           # FilterPattern, FilterStore, FilterEngine, ActionHandlers
│   └── signal/
│       ├── backend.go          # SignalBackend interface, Group, IncomingMessage types
│       ├── signalcli.go        # SignalCLIBackend — signal-cli subprocess management
│       └── types.go            # JSON-RPC types: Request, Response, Envelope, DataMessage
├── docs/
│   ├── api/                    # OpenAPI specification
│   ├── architecture.md         # Component overview and diagrams
│   ├── app-flow.md             # Application flow with Mermaid diagrams
│   ├── commands.md             # Command reference
│   ├── data-flow.md            # Data flow sequence diagrams
│   ├── design.md               # Design rationale and decisions
│   ├── future-native-signal.md # Roadmap for native Go Signal backend
│   ├── implementation.md       # This document
│   ├── multi-session.md        # Multi-machine setup guide
│   ├── operations.md           # Operations and troubleshooting
│   ├── planning.md             # Development phases and milestones
│   ├── pwa-setup.md            # PWA and Tailscale setup
│   └── setup.md                # Installation and quickstart
├── install/
│   ├── install.sh              # System-wide install script (requires root)
│   └── install-user.sh         # User service install script (no root)
├── web/                        # PWA static files (embedded into binary)
│   ├── index.html
│   ├── app.js
│   ├── style.css
│   └── manifest.json
├── go.mod
├── go.sum
├── Makefile
├── README.md
└── LICENSE
```

---

## 2. Core Concepts

### Session Lifecycle and States

A session moves through these states in its lifetime:

```
[created] → running → waiting_input ⇄ running
                    ↓
              complete | failed | killed
```

| State | Meaning |
|---|---|
| `running` | tmux session alive, claude-code executing, output being produced |
| `waiting_input` | Output has been idle for `input_idle_timeout` seconds and the last line matches a prompt pattern |
| `complete` | claude-code exited cleanly (tmux session gone, no error) |
| `failed` | tmux session gone unexpectedly, or claude-code exited non-zero |
| `killed` | User sent `kill <id>` command |

State transitions trigger the `onStateChange` callback set on the session manager, which both the Signal router and the HTTP WebSocket hub subscribe to.

### The Session Store

`internal/session/store.go` implements a simple in-memory cache backed by a flat JSON file.

- All sessions are kept in `map[string]*Session` (key: `FullID = hostname-shortid`)
- A `sync.RWMutex` protects concurrent access from multiple goroutines (monitor goroutines, HTTP handlers, router callbacks)
- Every mutating operation (Save, Delete) calls `persist()`, which atomically rewrites the full JSON file
- On startup, `NewStore()` reads and unmarshals the existing file; an empty or missing file produces an empty store

### The tmux Integration

Every session is backed by a named tmux session:

- **Naming convention:** `cs-{hostname}-{shortid}` (e.g. `cs-myhost-a3f2`)
  - `cs-` prefix namespaces datawatch sessions from user sessions
  - hostname is included to avoid collisions when multiple users share a tmux server
- **Output capture:** `tmux pipe-pane -o -t <session> 'cat >> <logfile>'` redirects all pane output to a log file
  - The `-o` flag means "only pipe stdout, not stdin"
  - The log file path is `{data_dir}/logs/{hostname}-{shortid}.log`
- **Input injection:** `tmux send-keys -t <session> "<text>" Enter` sends keystrokes to the running process

### Output Monitoring

Each active session has a dedicated `monitorOutput` goroutine that uses **fsnotify**
(interrupt-driven file watching) instead of polling. The goroutine:

1. Opens the session's log file (waits if not yet created)
2. Seeks to the end of the file (skips history on resume)
3. Watches the log file with fsnotify; reads new lines on write events
4. Maintains a line buffer and resets an idle timer on each new line
5. When the idle timer fires (no new output for `input_idle_timeout` seconds):
   - Checks if the tmux session still exists
   - If gone: marks the session `complete` or `failed`
   - If alive: checks whether the last buffered line matches a prompt pattern (ends with `?`, `>`, `[y/N]`, `: `, etc.)
   - If pattern matches: marks `waiting_input`, calls `onNeedsInput`
6. When new output arrives after `waiting_input`: marks `running` again
7. The goroutine exits when the session reaches a terminal state or the context is cancelled

### Message Routing

The router (`internal/router`) is stateless. It:

1. Receives an `IncomingMessage` (or `messaging.Message`) from the Signal backend
2. Filters by group ID — messages from other groups are ignored
3. Filters out messages from the daemon's own account (echo suppression)
4. Calls `Parse(text)` to produce a `Command`
5. Dispatches to the appropriate session manager method
6. Formats a reply and calls `backend.Send(groupID, reply)`

The router never holds state. Session state lives exclusively in the session store.

### DNS Channel Backend

The DNS channel (`internal/messaging/backends/dns/`) implements the `messaging.Backend` interface
using DNS TXT queries for covert command/response communication.

**Package structure:**
- `protocol.go` — `EncodeQuery`/`DecodeQuery` (HMAC-SHA256), `EncodeResponse`/`DecodeResponse` (fragmented TXT)
- `nonce.go` — `NonceStore` (bounded LRU, 10K entries, 5-minute TTL)
- `server.go` — `ServerBackend` implementing `messaging.Backend` (miekg/dns UDP+TCP)
- `client.go` — `Client.Execute()` for CLI DNS queries

**Config struct:** `DNSChannelConfig` in `internal/config/config.go`
- Fields: `enabled`, `mode`, `domain`, `listen`, `upstream`, `secret`, `ttl`, `max_response_size`, `poll_interval`

**Router exception:** DNS messages bypass the group ID filter since DNS has no concept of groups.
The router checks `msg.Backend == "dns"` and processes commands regardless of group ID.

---

## 3. Adding a New LLM Backend

To add support for a new AI coding assistant (e.g. `aider`):

### Step 1: Create the backend package

```
internal/llm/aider/backend.go
```

### Step 2: Implement `llm.Backend`

```go
package aider

import (
    "context"
    "fmt"
    "os/exec"

    "github.com/dmz006/datawatch/internal/llm"
)

// Backend implements llm.Backend for aider.
type Backend struct {
    bin string // path to aider binary
}

// New creates an aider backend.
func New(bin string) *Backend {
    if bin == "" {
        bin = "aider"
    }
    return &Backend{bin: bin}
}

// Name returns the backend name.
func (b *Backend) Name() string { return "aider" }

// SupportsInteractiveInput returns true — aider accepts yes/no on stdin.
func (b *Backend) SupportsInteractiveInput() bool { return true }

// Launch starts aider for the given task inside the pre-created tmux session.
// Output is captured to logFile via tmux pipe-pane (already set up by the session manager).
func (b *Backend) Launch(ctx context.Context, task, tmuxSession, logFile string) error {
    // Build the aider command: run non-interactively with an initial message
    aiderCmd := fmt.Sprintf("%s --yes --message %q", b.bin, task)

    // Send the command to the existing tmux session
    cmd := exec.CommandContext(ctx, "tmux", "send-keys", "-t", tmuxSession, aiderCmd, "Enter")
    if err := cmd.Run(); err != nil {
        return fmt.Errorf("send aider command to tmux: %w", err)
    }
    return nil
}

// Ensure Backend implements the interface at compile time.
var _ llm.Backend = (*Backend)(nil)
```

### Step 3: Register in `internal/llm/registry.go`

```go
import "github.com/dmz006/datawatch/internal/llm/aider"

func init() {
    Register(aider.New(""))
}
```

### Step 4: Add a config option

In `internal/config/config.go`, add to `SessionConfig`:

```go
// LLMBackend selects the LLM backend. One of: "claude-code", "aider".
// Defaults to "claude-code".
LLMBackend string `yaml:"llm_backend"`
```

### Step 5: Wire into the session manager

In the session manager's `Start()` method, look up the backend by name:

```go
backend, ok := llm.Get(cfg.Session.LLMBackend)
if !ok {
    backend, _ = llm.Get("claude-code") // fallback
}
if err := backend.Launch(ctx, task, tmuxSession, logFile); err != nil {
    return nil, fmt.Errorf("launch backend: %w", err)
}
```

Users then configure the backend in `config.yaml`:

```yaml
session:
  llm_backend: aider
```

---

## 4. Adding a New Messaging Backend

To add Slack as a control channel:

### Step 1: Create the backend package

```
internal/messaging/slack/backend.go
```

### Step 2: Implement `messaging.Backend`

```go
package slack

import (
    "context"
    "github.com/dmz006/datawatch/internal/messaging"
)

type Backend struct {
    botToken  string
    channelID string
    client    *slackAPIClient
}

func New(botToken, channelID string) *Backend {
    return &Backend{botToken: botToken, channelID: channelID}
}

func (b *Backend) Name() string { return "slack" }
func (b *Backend) SelfID() string { return b.client.BotUserID() }

func (b *Backend) Send(recipient, message string) error {
    return b.client.PostMessage(recipient, message)
}

func (b *Backend) Subscribe(ctx context.Context, handler func(messaging.Message)) error {
    // Use Slack Events API Socket Mode for no-public-URL deployment
    return b.client.RunSocketMode(ctx, func(event SlackEvent) {
        if event.Type != "message" || event.BotID != "" {
            return // ignore bot messages
        }
        handler(messaging.Message{
            GroupID:    event.Channel,
            Sender:     event.User,
            SenderName: event.UserName,
            Text:       event.Text,
            Backend:    b.Name(),
        })
    })
}

// Link is a no-op for Slack — bots authenticate with a bot token, no QR linking needed.
func (b *Backend) Link(deviceName string, onQR func(string)) error { return nil }

func (b *Backend) Close() error { return b.client.Close() }

var _ messaging.Backend = (*Backend)(nil)
```

### Step 3: Add a config section

In `internal/config/config.go`:

```go
type SlackConfig struct {
    BotToken  string `yaml:"bot_token"`
    ChannelID string `yaml:"channel_id"`
}

type Config struct {
    // ...existing fields...
    Slack SlackConfig `yaml:"slack"`
}
```

### Step 4: Wire into main.go

```go
if cfg.Slack.BotToken != "" {
    slackBackend := slack.New(cfg.Slack.BotToken, cfg.Slack.ChannelID)
    messaging.Register(slackBackend)
    go slackBackend.Subscribe(ctx, router.HandleMessage)
}
```

### Step 5: Configure

```yaml
slack:
  bot_token: xoxb-...
  channel_id: C01234567
```

---

## 5. signal-cli JSON-RPC Protocol

`signal-cli` in `jsonRpc` mode reads newline-delimited JSON-RPC 2.0 requests from stdin and writes responses and notifications to stdout.

### Subscribe to incoming messages

Request:
```json
{"jsonrpc":"2.0","method":"subscribeReceive","id":1}
```

Response:
```json
{"jsonrpc":"2.0","result":{},"id":1}
```

After subscribing, incoming messages arrive as notifications (no `id` field):
```json
{
  "jsonrpc": "2.0",
  "method": "receive",
  "params": {
    "envelope": {
      "source": "+12125551234",
      "sourceName": "Alice",
      "dataMessage": {
        "message": "new: write tests",
        "groupInfo": {
          "groupId": "base64groupid=="
        }
      }
    }
  }
}
```

### Send a message to a group

Request:
```json
{
  "jsonrpc": "2.0",
  "method": "send",
  "params": {
    "groupId": "base64groupid==",
    "message": "[myhost][a3f2] Started session for: write tests"
  },
  "id": 2
}
```

Response:
```json
{"jsonrpc":"2.0","result":{"timestamp":1711234567890},"id":2}
```

### List joined groups

Request:
```json
{"jsonrpc":"2.0","method":"listGroups","id":3}
```

Response:
```json
{
  "jsonrpc": "2.0",
  "result": [
    {"id": "base64groupid==", "name": "AI Control"},
    {"id": "anothergroup==", "name": "Family"}
  ],
  "id": 3
}
```

### Link a new device

Device linking is not a JSON-RPC call — it uses a separate `signal-cli link` subprocess invocation:

```bash
signal-cli --config ~/.local/share/signal-cli link -n myhost
```

stdout/stderr: `sgnl://linkdevice?uuid=...&pub_key=...`

The Go code captures this URI, converts it to a QR code, and displays it (in terminal or via SSE to the PWA).

---

## 6. WebSocket Protocol

All messages use a common envelope:

```json
{
  "type": "<message_type>",
  "data": { ... },
  "ts": "2026-03-25T14:32:01.123Z"
}
```

### Server → Client Messages

| Type | When Sent | `data` Payload |
|---|---|---|
| `sessions` | On connect; on any session state change | `{"sessions": [<Session>, ...]}` |
| `session_state` | When one session's state changes | `{"session": <Session>}` |
| `output` | New output lines from a monitored session | `{"session_id": "a3f2", "lines": ["line1", "line2"]}` |
| `needs_input` | Session enters `waiting_input` | `{"session_id": "a3f2", "prompt": "Do you want to continue? [y/N]"}` |
| `notification` | General informational message | `{"message": "Session a3f2 killed."}` |
| `error` | Command or protocol error | `{"message": "Session not found: zz99"}` |

**Example — sessions message on connect:**
```json
{
  "type": "sessions",
  "data": {
    "sessions": [
      {
        "id": "a3f2",
        "full_id": "myhost-a3f2",
        "task": "write tests for auth module",
        "tmux_session": "cs-myhost-a3f2",
        "state": "running",
        "created_at": "2026-03-25T14:00:00Z",
        "updated_at": "2026-03-25T14:01:30Z",
        "hostname": "myhost"
      }
    ]
  },
  "ts": "2026-03-25T14:32:01.000Z"
}
```

**Example — needs_input event:**
```json
{
  "type": "needs_input",
  "data": {
    "session_id": "a3f2",
    "prompt": "Do you want to overwrite auth_test.go? [y/N] "
  },
  "ts": "2026-03-25T14:35:22.000Z"
}
```

### Client → Server Messages

| Type | When Sent | `data` Payload |
|---|---|---|
| `command` | Raw command string (same syntax as Signal) | `{"text": "list"}` |
| `new_session` | Start a new session | `{"task": "build a REST API"}` |
| `send_input` | Send input to a waiting session | `{"session_id": "a3f2", "text": "y"}` |
| `subscribe` | Subscribe to output lines for a session | `{"session_id": "a3f2"}` |
| `ping` | Keepalive (server replies with pong) | `{}` |

**Example — subscribe to session output:**
```json
{
  "type": "subscribe",
  "data": {"session_id": "a3f2"},
  "ts": "2026-03-25T14:32:05.000Z"
}
```

After subscribing, the server sends `output` messages for that session as new lines appear.

---

## 7. Output Monitoring Algorithm

The `monitorOutput` goroutine implements the following algorithm:

```
1. Open log file at sess.LogFile
   - If file does not exist, wait with exponential backoff (up to 5s) then retry
2. Seek to EOF (skip historical output on resume after daemon restart)
3. Create an idle timer: time.NewTimer(cfg.Session.InputIdleTimeout * time.Second)
4. Enter read loop:
   a. Attempt to read the next line from the file
   b. If a new line is available:
      - Append to in-memory line buffer (capped at 100 lines)
      - Reset the idle timer
      - If current state is waiting_input: transition back to running
   c. If no new line (EOF):
      - Wait for the next fsnotify write event (interrupt-driven, no polling)
   d. If idle timer fires (no new output in InputIdleTimeout seconds):
      - Check if tmux session exists: `tmux has-session -t <name>`
      - If session is gone:
          * If last state was running/waiting_input: mark complete or failed
          * Call onStateChange(sess, newState)
          * Return (goroutine exits)
      - If session is alive and last line matches prompt pattern:
          * Mark waiting_input
          * Set sess.LastPrompt = last buffered line
          * Call onNeedsInput(sess, lastPrompt)
          * Call onStateChange(sess, waiting_input)
      - Reset idle timer and continue loop
5. On context cancellation: return (daemon shutting down)
```

**Prompt patterns** (heuristic — last line ends with one of):
- `? ` — generic question
- `[y/N] ` or `[Y/n] ` — yes/no prompt
- `> ` — interactive continuation prompt
- `: ` — input request
- `(Y/n)` — alternate format

These patterns are checked case-insensitively on the last non-empty line in the buffer.

---

## 8. Configuration Reference

All fields in `~/.datawatch/config.yaml`:

| Field | Type | Default | Description |
|---|---|---|---|
| `hostname` | string | OS hostname | Identifies this machine in Signal messages and session IDs |
| `data_dir` | string | `~/.datawatch` | Root directory for sessions.json, logs/, and config |
| `signal.account_number` | string | (required) | Signal phone number in E.164 format, e.g. `+12125551234` |
| `signal.group_id` | string | (required) | Signal group ID in base64 format (from `signal-cli listGroups`) |
| `signal.config_dir` | string | `~/.local/share/signal-cli` | signal-cli data directory containing account keys |
| `signal.device_name` | string | hostname | Name shown in Signal's Linked Devices list |
| `session.max_sessions` | int | `10` | Maximum number of concurrent claude-code sessions |
| `session.input_idle_timeout` | int | `10` | Seconds of idle output before declaring a session is waiting for input |
| `session.tail_lines` | int | `20` | Default number of lines returned by `tail` and `status` commands |
| `session.claude_code_bin` | string | `claude` | Path to the claude-code binary (can be absolute or a `PATH`-relative name) |
| `server.enabled` | bool | `true` | Whether to start the HTTP/WebSocket server |
| `server.host` | string | `0.0.0.0` | Bind address. Use `0.0.0.0` for all interfaces (including Tailscale) |
| `server.port` | int | `8080` | HTTP/WebSocket listen port |
| `server.token` | string | `""` | Optional bearer token for PWA authentication. Empty = no auth |
| `server.tls_cert` | string | `""` | Path to TLS certificate PEM file. Leave empty for plain HTTP |
| `server.tls_key` | string | `""` | Path to TLS key PEM file. Leave empty for plain HTTP |
| `server.tls_enabled` | bool | `false` | Enable TLS. Use with `tls_auto_generate=true` or explicit cert/key paths |
| `server.tls_auto_generate` | bool | `false` | Auto-generate a self-signed certificate at `~/.datawatch/tls/server/` on start |
| `servers[].name` | string | — | Short identifier for a remote datawatch server (used with `--server` flag) |
| `servers[].url` | string | — | Base URL of the remote server (e.g. `http://203.0.113.10:8080`) |
| `servers[].token` | string | — | Bearer token for the remote server |
| `servers[].enabled` | bool | `true` | Whether this remote server is active |
| `mcp.max_retries` | int | `3` | Number of automatic retries when a per-session MCP channel server fails to start or loses connection |

### Dependencies

- **fsnotify** — used for interrupt-driven log file monitoring (replaces polling). Each session's `monitorOutput` goroutine watches the log file via fsnotify and reads new lines on write events, reducing CPU usage and improving latency.

### Per-Session MCP Architecture

When `channel_enabled: true`, each session gets its own dedicated MCP channel server
on a random port. This replaces the previous global MCP channel and enables true
multi-session support — each claude-code instance communicates with its own channel
server independently. The per-session servers are started automatically when a session
launches and stopped when the session ends. The `mcp.max_retries` config field controls
how many times a failed channel server connection is retried before giving up.

### Data Files

All persistent data is stored in `~/.datawatch/` (or `data_dir` if overridden):

| File | Description |
|---|---|
| `config.yaml` | Daemon configuration |
| `sessions.json` | Session state and history |
| `schedule.json` | Persistent command scheduler entries |
| `commands.json` | Named reusable command library (SavedCommand records) |
| `filters.json` | Output filter rules (FilterPattern records) |
| `alerts.json` | Persistent system alert log |
| `logs/<hostname>-<id>.log` | Per-session output log |
| `tls/server/` | Auto-generated TLS certificate/key (when `tls_auto_generate: true`) |
| `daemon.pid` | PID file written in daemon mode |
| `daemon.log` | Daemon log in daemon mode |

---

## 9. Building

### Local binary

```bash
make build
# produces: ./bin/datawatch
```

Or directly with Go:

```bash
go build -o datawatch ./cmd/datawatch/
```

### Cross-compilation

```bash
make cross
# produces:
#   bin/datawatch-linux-amd64
#   bin/datawatch-linux-arm64
#   bin/datawatch-darwin-amd64
#   bin/datawatch-darwin-arm64
```

### Install to `$GOPATH/bin`

```bash
make install
# equivalent to: go install ./cmd/datawatch/
```

### Build tags

No build tags are currently required. CGO is disabled for the default build (`CGO_ENABLED=0`), which produces a fully static binary.

The future native Signal backend (Phase 4) will require `CGO_ENABLED=1` and a Rust toolchain for libsignal-ffi.

---

## 10. Local Development

### Running without a real Signal account

You can run the daemon and exercise all session management functionality without a Signal account by disabling the Signal backend and using the CLI or HTTP API directly.

**Option 1: Use `datawatch session` subcommands (no daemon needed)**

```bash
datawatch session list
datawatch session new "write a hello world program"
datawatch session tail a3f2 --lines 50
datawatch session send a3f2 "yes, continue"
```

**Option 2: Start the daemon with Signal disabled**

Remove (or comment out) the `signal.account_number` and `signal.group_id` from config. The daemon will start with only the HTTP server active:

```bash
datawatch start --config ./dev-config.yaml
```

Then drive commands via curl:

```bash
# List sessions
curl http://localhost:8080/api/sessions | jq .

# Start a new session
curl -X POST http://localhost:8080/api/command \
  -H 'Content-Type: application/json' \
  -d '{"text": "new: write hello world in Go"}'

# Get session output
curl "http://localhost:8080/api/output?id=a3f2&n=50"

# Send input to a waiting session
curl -X POST http://localhost:8080/api/command \
  -H 'Content-Type: application/json' \
  -d '{"text": "send a3f2: yes"}'
```

**Option 3: Mock signal-cli with a shell script**

Create a fake `signal-cli` that reads JSON-RPC from stdin and writes a subscription acknowledgement:

```bash
#!/bin/bash
# fake-signal-cli.sh — minimal signal-cli mock for development
while IFS= read -r line; do
  # Echo a subscribeReceive response
  echo '{"jsonrpc":"2.0","result":{},"id":1}'
done
```

Point the config at it via a wrapper that puts it on PATH before the real signal-cli.

### Using the `--config` flag

All daemon commands accept `--config` to point at a non-default config file:

```bash
datawatch start --config /tmp/dev-config.yaml
datawatch session list --config /tmp/dev-config.yaml
```

### Testing with a dedicated Signal number

The recommended approach for integration testing is to use a cheap VoIP number (e.g. Google Voice, Twilio) to register a dedicated Signal account for the daemon. Keep a test Signal group with that account and your personal number, then run the daemon against it in a tmux session on your development machine.
