# Design Document — datawatch

## 1. Problem Statement

Managing long-running AI coding sessions is hard when you are away from your keyboard. A `claude-code` task might take 20–40 minutes, require occasional yes/no confirmations, and produce output you want to review without sitting at a terminal. There is no good way to:

- Start a task on a remote machine from your phone
- Receive a notification when the AI pauses and needs a decision
- Respond to that prompt without opening a laptop or SSH client
- Monitor multiple sessions across multiple machines from one place

You need a way to **delegate tasks, receive async updates, and respond to prompts from anywhere** — including your phone, while offline or commuting — without installing heavyweight tools on the device in your pocket.

---

## 2. Goals

- **Async task delegation via Signal messenger** — send a task from your phone, receive a reply when it starts, get a notification when it needs input or completes
- **Multi-session management across multiple machines** — each machine runs its own daemon, all connected to one Signal group; sessions are prefixed by hostname so you always know which machine is replying
- **Mobile-friendly real-time interface via PWA and Tailscale** — a Progressive Web App accessible over Tailscale, installable to the home screen, with live output streaming and browser notifications
- **Session persistence across daemon restarts** — sessions are stored in a flat JSON file; monitors resume on startup so a daemon restart does not lose session state
- **Single binary, minimal dependencies** — one `datawatch` binary; the only required external tools are `signal-cli`, `tmux`, `claude` (the claude-code CLI), and Java (for signal-cli)

---

## 3. Non-Goals

- **Not a general-purpose Signal bot** — datawatch is purpose-built for AI coding session management; it is not a framework for building arbitrary Signal bots
- **Not a cloud service** — all processing, state, and logs stay on the machines you own; there is no SaaS component and no accounts beyond Signal itself
- **Not a GUI IDE replacement** — datawatch provides async control, not a full development environment; you still use your editor and terminal for primary work
- **No telemetry or data collection** — nothing is phoned home; the binary makes no network connections except to signal-cli and Tailscale

---

## 4. Design Principles

### Simplicity
A single Go binary with no embedded database. State is stored in flat JSON files that are human-readable, easily inspectable with `cat`, and trivially backed up. The configuration is one YAML file. The PWA is plain HTML/CSS/JS with no build step.

### Reliability
Sessions persist in `sessions.json`. When the daemon restarts, it reads the session file and resumes monitoring every session that was in `running` or `waiting_input` state. If `signal-cli` crashes, a supervisor (systemd) restarts it. If a tmux session has gone missing at resume time, the session is marked `failed` rather than silently dropped.

### Security
Signal provides end-to-end encryption for all task content and command messages. Tailscale provides an encrypted WireGuard overlay for PWA access — no certificate management required. An optional bearer token provides an additional authentication layer for shared Tailscale networks. The daemon runs as a non-privileged user. No data leaves the local machine.

### Extensibility
The messaging protocol and LLM backend are expressed as Go interfaces. Swapping Signal for Slack, or `claude-code` for `aider`, requires implementing an interface and wiring it into `main.go` — no changes to the router, session manager, or HTTP server.

---

## 5. Key Design Decisions

### Signal via signal-cli (JSON-RPC daemon mode)

**Decision:** Use `signal-cli` as a subprocess in `jsonRpc` mode, communicating over stdin/stdout with newline-delimited JSON-RPC 2.0.

**Rationale:** There is no mature, production-ready native Go Signal library. `signal-cli` is the most battle-tested open-source Signal client, used by thousands of deployments. The JSON-RPC daemon mode gives a stable, long-running process with a clean request/response + notification model. The cost is a Java dependency (~200 MB), which is acceptable for server-side use and is the primary motivation for Phase 3 (native Go Signal backend).

### tmux for session management

**Decision:** Each `claude-code` session runs inside a named tmux session (`cs-{hostname}-{id}`).

**Rationale:** tmux is the most reliable way to run long-lived interactive processes that can be attached to manually, detached without killing the process, and piped for output capture. The `pipe-pane` feature writes all terminal output to a log file, giving a reliable audit trail. tmux has been stable for over a decade and is available in every Linux/macOS package manager. A user can always `tmux attach -t cs-myhost-a3f2` to interact directly, bypassing the Signal interface entirely.

### Group messaging vs. 1:1

**Decision:** Commands are sent to a Signal group, not as direct messages to the daemon's phone number.

**Rationale:** A group channel enables multi-participant control (you can add a colleague), provides a shared audit log of all commands and replies visible to all group members, and enables the multi-machine pattern where multiple daemons all reply to the same group. Group messages are also easier to route unambiguously — each daemon filters on its own configured `group_id`.

### Go for implementation

**Decision:** The daemon is written in Go.

**Rationale:** Go compiles to a single static binary with no runtime dependencies. Its goroutine model maps naturally to the concurrent workload: one goroutine per output monitor, one for the signal-cli read loop, one for each WebSocket client write pump, and one for the HTTP server. Cross-compilation to arm64/amd64 Linux and macOS is a single `make cross` command. The standard library covers HTTP, JSON, YAML parsing (via a single external dependency), and file I/O without additional frameworks.

### JSON file persistence

**Decision:** Session state is stored in `~/.datawatch/sessions.json` as a JSON array.

**Rationale:** No database dependency. The file is human-readable and editable with any text editor. It is easy to back up, copy between machines, and inspect when debugging. The session count is small (bounded by `max_sessions`, default 10) so there is no performance concern with full-file rewrites on each state change. If the file is corrupted, the daemon starts with an empty session list rather than failing.

### Hostname-prefixed session IDs for multi-machine disambiguation

**Decision:** Session IDs are stored as `{hostname}-{4-char-hex}` (e.g. `myhost-a3f2`). All Signal replies are prefixed with `[hostname]`.

**Rationale:** When multiple machines share a Signal group, short IDs like `a3f2` would be ambiguous — two different machines could independently generate the same 4-character ID. Hostname prefixes make every session globally unique in the group. The short 4-character ID is still accepted for commands when the session is on the local machine, since each daemon only operates on its own sessions.

### Pluggable backend interfaces for future Signal replacement with native Go

**Decision:** Both the messaging layer (`messaging.Backend`) and the LLM layer (`llm.Backend`) are expressed as Go interfaces. The `signal.SignalBackend` interface exists within the `signal` package as a more specific variant.

**Rationale:** `signal-cli` requires Java, which adds ~200 MB to the system requirements. The libsignal-ffi library (Rust) provides a C ABI that Go can call via CGO. A future native Go backend would eliminate the Java dependency entirely and improve startup time and reliability. Defining the interface now means the rest of the application does not need to change when the backend is swapped.

---

## 6. Security Architecture

### Signal E2E encryption
All command messages and task content are sent through Signal, which provides end-to-end encryption using the Signal Protocol (Double Ratchet + X3DH). Only devices registered to the Signal group can read messages. The daemon runs as a linked device to a Signal account.

### Tailscale overlay network
The PWA is served over HTTP. Plain HTTP is secure on Tailscale because Tailscale provides WireGuard-based encryption at the network layer (ChaCha20-Poly1305). Only devices authenticated to your Tailscale account can reach the machine. The PWA does not need its own TLS in typical deployments.

### Optional bearer token
For shared Tailscale networks, an optional bearer token can be set in `config.yaml`. The token is sent as a WebSocket query parameter and as an `Authorization: Bearer` header for REST API calls. The PWA stores it in `localStorage`.

### Non-privileged daemon
The daemon runs as the user who installed it. It requires no root privileges. The systemd user service variant requires no root at all. The system service variant runs as a dedicated user created by the install script.

### No cloud dependencies
Beyond Signal infrastructure (which is federated and open source), datawatch makes no external network connections. All data — sessions, logs, config — is stored locally.

---

## 7. Modularity Design

### `llm.Backend` interface
```go
type Backend interface {
    Name() string
    Launch(ctx context.Context, task, tmuxSession, logFile string) error
    SupportsInteractiveInput() bool
}
```
The session manager calls `Launch()` to start the AI assistant inside the pre-created tmux session. Swapping `claude-code` for `aider` or `continue.dev` requires implementing this interface and adding a config option for `llm_backend`.

### `messaging.Backend` interface
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
The router receives `messaging.Message` values and calls `Send()` for replies. Adding Slack, Discord, or Telegram is a matter of implementing this interface — the router and session manager are unchanged.

### HTTP server is protocol-agnostic
The PWA API and WebSocket server are independent of the Signal backend. Commands flow through the same router regardless of whether they arrive via Signal or via WebSocket. State change callbacks (`onStateChange`, `onNeedsInput`) are composed in `main.go` so both the Signal router and the HTTP server are notified on every transition.

---

## 8. Failure Modes and Mitigations

| Failure | Detection | Mitigation |
|---|---|---|
| signal-cli crash | Read loop goroutine closes `done` channel | Supervisor (systemd) restarts the daemon; sessions.json preserves state |
| tmux session death | Monitor checks `tmux has-session` periodically | Session marked `failed`; Signal + PWA notification sent |
| Network loss (Signal) | signal-cli handles reconnection internally | Signal queues messages; they are delivered when connectivity resumes |
| Network loss (Tailscale) | WebSocket disconnect; client reconnects | PWA auto-reconnects with exponential backoff |
| Daemon restart | Startup reads sessions.json | `ResumeMonitors()` re-attaches goroutines to all active sessions |
| sessions.json corruption | JSON unmarshal error on startup | Daemon starts with empty session list and logs an error |
| claude-code not in PATH | tmux session exits immediately | Session transitions to `failed`; notification sent |
| Java not installed | signal-cli subprocess fails to start | Daemon reports error on startup; `datawatch link` also fails |

---

## 9. Future Roadmap

### Phase 3 — Extensive documentation, install scripts, CLI local commands, modularity interfaces
Comprehensive docs (this document and siblings), `make install` targets, package configs for apt/rpm/brew, `datawatch session` subcommands for local use without Signal, and the `llm.Backend` / `messaging.Backend` interface definitions.

### Phase 4 — Native Go Signal backend (libsignal-ffi via CGO)
Replace `signal-cli` with a native Go implementation using the Rust libsignal-ffi C ABI called via CGO. Eliminates the Java dependency. Improves startup time and reliability. See `docs/future-native-signal.md` for design notes.

### Phase 5 — Additional messaging backends
Implement `messaging.Backend` for:
- **Slack** — using the Slack Web API and Events API (webhook or Socket Mode)
- **Discord** — using the Discord bot API
- **Telegram** — using the Telegram Bot API

### Phase 6 — Additional LLM backends
Implement `llm.Backend` for:
- **aider** — open-source AI coding assistant (`aider --yes --message "..."`)
- **GPT-4 via API** — a thin wrapper that calls the OpenAI API and streams output to a tmux session

### Phase 7 — Container images and Kubernetes
- Docker and Podman images (`docker pull ghcr.io/dmz006/datawatch`)
- Helm chart for Kubernetes deployment
- Distroless base image for minimal attack surface
