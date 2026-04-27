# Design Document — datawatch

> **Doc-alignment audit:** last refreshed for **v5.26.3** (2026-04-27). The original problem statement and v3.x design rationale below are kept as-is. The **v4 → v5 design evolution** appendix at the end of this page covers the autonomous, orchestrator, observer, plugin-framework, agent, and federation additions.

## 1. Problem Statement

Managing long-running AI coding sessions is hard when you are away from your keyboard. A `claude-code` task might take 20–40 minutes, require occasional yes/no confirmations, and produce output you want to review without sitting at a terminal. There is no good way to:

- Start a task on a remote machine from your phone
- Receive a notification when the AI pauses and needs a decision
- Respond to that prompt without opening a laptop or SSH client
- Monitor multiple sessions across multiple machines from one place

You need a way to **delegate tasks, receive async updates, and respond to prompts from anywhere** — including your phone, while offline or commuting — without installing heavyweight tools on the device in your pocket.

---

## 2. Goals

- **Async task delegation via any messaging backend** — send a task from your phone via Signal, Telegram, Matrix, Discord, Slack, SMS, or a webhook; receive a reply when it starts, get a notification when it needs input or completes
- **Multi-session management across multiple machines** — each machine runs its own daemon, all connected to a shared control channel; sessions are prefixed by hostname so you always know which machine is replying
- **Mobile-friendly real-time interface via PWA and Tailscale** — a Progressive Web App accessible over Tailscale, installable to the home screen, with live output streaming and browser notifications
- **MCP server for IDE integration** — Cursor, Claude Desktop, and VS Code can list, start, and interact with sessions directly via the Model Context Protocol
- **Session persistence across daemon restarts** — sessions are stored in a flat JSON file; monitors resume on startup so a daemon restart does not lose session state
- **Single binary, minimal dependencies** — one `datawatch` binary; only `tmux` is always required; messaging backends and LLM tools are optional dependencies enabled per-config
- **Pluggable LLM backends** — claude-code, aider, goose, gemini, opencode, ollama, openwebui, or a custom shell script

---

## 3. Non-Goals

- **Not a general-purpose messaging bot** — datawatch is purpose-built for AI coding session management; it is not a framework for building arbitrary bots
- **Not a cloud service** — all processing, state, and logs stay on the machines you own; there is no SaaS component
- **Not a GUI IDE replacement** — datawatch provides async control, not a full development environment; you still use your editor and terminal for primary work
- **No telemetry or data collection** — nothing is phoned home; the binary makes no network connections beyond configured messaging backends and LLM tools

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
    Launch(ctx context.Context, task, tmuxSession, projectDir, logFile string) error
    SupportsInteractiveInput() bool
    Version() string
}
```
The session manager calls `Launch()` to start the AI assistant inside the pre-created tmux session. `projectDir` is passed as the working directory; for claude-code this is also the `--add-dir` constraint. Swapping `claude-code` for any other backend requires implementing this interface and setting `session.llm_backend` in config.

Available implementations: `claude-code`, `aider`, `goose`, `gemini`, `opencode`, `ollama`, `openwebui`, `shell`.

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
The router receives `messaging.Message` values and calls `Send()` for replies. Multiple backends can run simultaneously — the router fans out notifications to all active backends. Adding a new backend requires implementing this interface — the router and session manager are unchanged.

Available implementations: `signal`, `telegram`, `matrix`, `discord`, `slack`, `twilio`, `ntfy`, `email`, `github` (webhook), `webhook` (generic).

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

## 9. Roadmap

### Completed

**Phase 1 — Core bridge** (Signal → tmux → claude-code loop, session lifecycle, persistence, QR linking)

**Phase 2 — PWA and WebSocket interface** (embedded HTTP server, real-time streaming, installable PWA, REST API, Swagger UI)

**Phase 3 — Documentation, install scripts, CLI, modularity**
Comprehensive documentation suite, `install/install.sh`, systemd service files, `datawatch session` subcommands, `llm.Backend` / `messaging.Backend` interface definitions and registries.

**Phase 5 — Additional messaging backends** (all shipped)
Signal, Telegram, Matrix, Discord, Slack, Twilio SMS, ntfy, email, GitHub webhook, generic webhook.

**Phase 6 — Additional LLM backends** (all shipped)
claude-code, aider, goose, gemini, opencode, ollama, openwebui, shell.

**MCP server** — Cursor, Claude Desktop, VS Code, and remote AI agent integration via Model Context Protocol (stdio and HTTP/SSE transports).

---

### Planned

**Phase 4 — Native Go Signal backend (libsignal-ffi via CGO)**
Replace `signal-cli` with a native Go implementation using the Rust libsignal-ffi C ABI called via CGO. Eliminates the Java dependency. Improves startup time and reliability. See `docs/future-native-signal.md` for design notes.

**Phase 7 — Container images and Kubernetes** *(largely shipped — see v4 → v5 appendix)*
- Docker and Podman images (`docker pull ghcr.io/dmz006/datawatch`)
- Helm chart for Kubernetes deployment
- Distroless base image for minimal attack surface

---

## v4 → v5 design evolution (2026-04-27 audit)

The v3.x design above was a single-host control plane: bridge messaging-channel inbound to LLM-CLI tmux sessions, persist state, and expose stats. The v4 → v5 stretch added five distinct subsystems on top of that substrate. Each stayed faithful to two design constraints from the original:

1. **Configuration-accessibility rule:** every feature must be reachable from YAML + REST + MCP + CLI + comm channel + PWA + (where applicable) mobile companion. No subsystem is "REST-only" or "MCP-only".
2. **Hot-reloadable subset must stay hot-reloadable:** new subsystems wire their own config block into `applyConfigPatch` so `datawatch reload` (and the equivalent SIGHUP / `POST /api/reload` / MCP `reload` tool) re-applies them without daemon restart.

### 5.1 Autonomous PRD substrate (BL24+BL25 → BL191 → BL202)

A PRD ("product requirement document") is a free-form spec that decomposes into a graph of stories and tasks. The autonomous engine runs each task as a real worker session and an independent verifier attests the result before the next step starts.

Key design decisions:

- **PRD lifecycle** (`draft → needs_review → approved → running → completed | blocked | cancelled`) — the `needs_review` state (BL191 Q1, v5.2.0) gates execution on operator approval, with `request_revision` returning to draft.
- **Recursion via `Task.SpawnPRD`** (BL191 Q4, v5.9.0) — a task whose spec is itself a PRD spec gets `Decompose+Approve+Run` automatically. Depth tracked via `PRD.Depth`; cycle prevention via `PRD.ParentPRDID`. `MaxRecursionDepth` config caps runaway nesting.
- **Guardrails-at-all-levels** (BL191 Q5/Q6, v5.10.0) — per-task and per-story `GuardrailVerdict` (`pass`/`warn`/`block`); one `block` paints the parent PRD blocked. Every level (orchestrator graph, PRD, story, task) carries verdicts independently.
- **LLM-override at PRD + Task scope** (BL203, v5.4.0) — `backend` / `effort` / `model` overridable at any level; falls back to inherit when empty.
- **Persisted as JSONL** under `~/.datawatch/autonomous/`. Hard-delete (v5.19.0) walks the parent→child graph and removes every descendant.

### 5.2 PRD-DAG orchestrator (BL117, v4.0.0)

Composes multiple PRDs into a DAG: edges declare `before` / `after` constraints. After each PRD finishes, a set of guardrails (rules-compliance, security-review, release-readiness, docs-integrity) each returns a verdict. One `block` halts the graph and waits for operator intervention. Each guardrail is its own LLM session with a focused prompt — independent and cross-backend-capable.

### 5.3 Observer + federation (BL171/BL172/BL180, v4.1 → v5.13)

A unified per-process observer that rolls up CPU / memory / disk / GPU / sub-process trees, with three "shapes" of peer:

- **Shape A** — standalone datawatch daemon registered as a peer of a parent.
- **Shape B** — datawatch agent worker auto-peering with its spawning parent (every ephemeral worker becomes an observer node for free).
- **Shape C** — node-level DaemonSet on a Kubernetes cluster, posting per-pod metrics back to a parent.

Cross-host envelope correlation (BL180, v5.12.0) walks each per-host envelope tree and surfaces caller chains that cross host boundaries as `<peer>:<envelope-id>` rows. eBPF kprobes (v5.13.0) on `tcp_connect` + `inet_csk_accept` produce LRU-hashed `conn_attribution` data joined with the procfs sub-process tree.

### 5.4 Plugin framework (BL33, v3.11.0)

Drop an executable plus a small manifest under `~/.datawatch/plugins/` and the daemon picks it up on the next save. Plugins get hooks for session start, session output, session completion, and alerts. The daemon hot-reloads the directory on the fly. Native plugins (built-in subsystems that look like plugins to the operator — observer, future native bridges) surface under `/api/plugins` via `RegisterNativePlugin` for parity with subprocess plugins.

### 5.5 Ephemeral worker agents (F10, v3.6 → v5.x)

Project + cluster profiles describe (1) the project to clone and (2) the cluster + namespace + image to spawn into. `datawatch agent spawn --project-profile X --cluster-profile Y` brings up a Pod (or Docker container, or remote-context spawn) with a pinned-mTLS bootstrap token. The worker calls `/api/agents/bootstrap` with the token — the parent verifies and returns the worker's full credentials. Workers terminate themselves on idle (BL96), can peer-to-peer message via the parent's `PeerBroker` (BL104), and resolve git tokens via the parent's `TokenBroker` (BL113).

### 5.6 Federated observer (S14a, v4.8.0)

A primary datawatch can register itself as a peer of a *root* primary, enabling cross-cluster observer roll-ups. Push-with-chain loop prevention via `federationSelfName`; per-envelope source attribution; opt-in via one config key.

### 5.7 Helm chart (charts/datawatch, v4.7+)

Ships the parent daemon as a Deployment with a ConfigMap-rendered config, an optional PVC for `~/.datawatch`, in-namespace RBAC for spawning ephemeral worker Pods, and a Service. Every secret (API token, TLS cert/key, Postgres URL, git token, kubeconfig for cross-cluster spawns) supports a *dual-supply* pattern (inline for dev or `existingSecret:` reference for prod with SealedSecret/ExternalSecret/Vault). NFS-backed persistence + cross-cluster kubeconfig + Shape-C observer DaemonSet documented in [`howto/setup-and-install.md`](howto/setup-and-install.md) (v5.26.2).

### 5.8 Channel transport (claude MCP / opencode-acp, v4.x → v5.26.1)

Per-session channel server (Node.js side-car for claude MCP, Go HTTP for opencode-acp) handles bidirectional message flow between operator and LLM CLI. Daemon brokers messages over WS to the PWA + mobile companion. Loopback POSTs to `/api/channel/*` from the channel server bypass the HTTP→HTTPS redirect (v5.18.0). Per-session ring buffer + `GET /api/channel/history` (v5.26.1) seeds the PWA Channel tab on session-detail open so a long-running channel doesn't look empty after a fresh page load.

### 5.9 Configuration parity backbone

Every new feature went through the same playbook: REST handler → MCP tool → CLI subcommand → PWA card → applyConfigPatch case → comm-channel verb (where applicable) → mobile companion surface. Two parity sweeps (v5.17.0 autonomous, v5.21.0 observer + whisper) closed gaps where the runtime feature shipped but the operator-facing surface silently no-op'd. Going forward, parity is verified at design time, not after the fact.
