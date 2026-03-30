# Planning Document — datawatch

## Development Phases

### Phase 1 — Core Bridge (Complete)

**Scope:** Everything needed for the basic Signal → tmux → claude-code loop.

**Delivered:**
- `signal-cli` subprocess management (JSON-RPC daemon mode)
- `signal.SignalBackend` interface and `SignalCLIBackend` implementation
- tmux session creation, output piping via `pipe-pane`, and key sending
- Session lifecycle state machine: `running` → `waiting_input` → `complete` / `failed` / `killed`
- Output monitor goroutines with idle detection and prompt pattern matching
- Command parser (`router.Parse`) for: `new:`, `list`, `status`, `send`, `kill`, `tail`, `attach`, `help`
- Persistent session store (`sessions.json`) with mutex-protected in-memory cache
- `ResumeMonitors()` on daemon restart — re-attaches goroutines to all active sessions
- Hostname-prefixed session IDs and `[hostname]` message prefixes for multi-machine use
- `datawatch link` — QR code workflow for device linking
- `datawatch config init` — interactive config wizard
- `datawatch start` — daemon entry point
- Minimal `config.yaml` support with sensible defaults

---

### Phase 2 — PWA and WebSocket Interface (Complete)

**Scope:** Real-time browser interface, installable as a PWA.

**Delivered:**
- Embedded HTTP server (`internal/server`) with static file serving via `//go:embed`
- WebSocket hub with per-client goroutines and subscription model
- Typed WebSocket protocol: `sessions`, `session_state`, `output`, `needs_input`, `notification`, `command`, `new_session`, `send_input`, `subscribe`, `ping`
- REST API: `GET /api/sessions`, `GET /api/output`, `POST /api/command`, `GET /api/health`, `GET /api/info`
- PWA: mobile-first HTML/CSS/JS, no build step, service worker for offline support
- Browser notifications when sessions enter `waiting_input`
- Installable to Android (Chrome) and iOS (Safari) home screen
- QR linking flow via SSE: `POST /api/link/start` → `GET /api/link/stream` → `qr` event → `linked` event
- Bearer token authentication (optional)
- OpenAPI specification (`docs/api/`)
- Swagger UI at `/api/docs`
- Dual-callback wiring in `main.go`: both Signal and HTTP server notified on every state change

---

### Phase 3 — Documentation, Install, CLI, Modularity (Complete)

**Scope:** Make the project production-ready for self-hosted deployment.

**Delivered:**
- Comprehensive documentation suite: `design.md`, `planning.md`, `implementation.md`, `app-flow.md`, `operations.md`, `data-flow.md`, `llm-backends.md`, `messaging-backends.md`, `mcp.md`, `uninstall.md`, `docs/README.md`
- Install scripts: `install/install.sh` (supports user and root installs, multiple distros)
- Package configs: systemd unit files, Makefile `build`/`install`/`cross` targets, debian/rpm/arch packaging configs
- `datawatch session` subcommands — local CLI without messaging backend:
  - `datawatch session list`
  - `datawatch session new "<task>"`
  - `datawatch session status <id>`
  - `datawatch session send <id> "<text>"`
  - `datawatch session kill <id>`
  - `datawatch session tail <id> [--lines N]`
  - `datawatch session attach <id>`
- `llm.Backend` interface with registry; implementations: `claude-code`, `aider`, `goose`, `gemini`, `opencode`, `ollama`, `openwebui`, `shell`
- `messaging.Backend` interface with registry; implementations: Signal, Telegram, Matrix, Discord, Slack, Twilio, ntfy, email, GitHub webhook, generic webhook
- MCP server (`internal/mcp`) — stdio and HTTP/SSE transports for Cursor, Claude Desktop, VS Code, and remote AI agents

---

### Phase 4 — Native Go Signal Backend (Planned)

**Scope:** Eliminate the Java/signal-cli dependency.

**Approach:**
- Wrap libsignal-ffi (Rust) via CGO using the stable C ABI
- Implement `signal.SignalBackend` with the new backend
- Select backend via `signal.backend: native` config option
- Maintain `signal-cli` backend as fallback for compatibility

**Blockers:**
- libsignal-ffi CGO bindings are complex; requires Rust toolchain in CI
- Signal protocol registration requires phone number verification which cannot be automated in CI tests

**Acceptance criteria:**
- All existing Signal commands work with the native backend
- Java is not required when using the native backend
- Linking, send, receive, group listing all functional

---

### Phase 5 — Additional Messaging Backends (Complete)

**Scope:** Support multiple messaging platforms as control channels.

**Delivered:**
- **Signal** — signal-cli JSON-RPC subprocess (bidirectional)
- **Telegram** — Bot API long-polling (bidirectional)
- **Matrix** — Client-Server API `/sync` (bidirectional)
- **Discord** — Gateway API bot (bidirectional)
- **Slack** — RTM API bot (bidirectional)
- **Twilio** — SMS via webhook + API (bidirectional)
- **ntfy** — push notifications via ntfy.sh or self-hosted (outbound only)
- **Email** — SMTP notifications (outbound only)
- **GitHub webhook** — triggers sessions from issue/PR comments and workflow dispatches (inbound only)
- **Generic webhook** — HTTP POST to start sessions from any system (inbound only)

All backends implement `messaging.Backend` and are registered in `internal/messaging/registry.go`. Multiple backends can be active simultaneously; notifications are fanned out to all enabled backends.

---

### Phase 6 — Additional LLM Backends (Complete)

**Scope:** Support multiple AI coding tools as alternatives to `claude-code`.

**Delivered:**
- **claude-code** — Anthropic Claude Code CLI (default; supports interactive input)
- **aider** — `aider --yes --message "<task>"` in tmux
- **goose** — `goose run --text "<task>"` in tmux
- **gemini** — `gemini -p "<task>"` in tmux
- **opencode** — `opencode -p "<task>"` in tmux
- **ollama** — `ollama run <model> "<task>"` in tmux (fully local, no API key)
- **openwebui** — OpenAI-compatible API via curl, streamed to tmux
- **shell** — custom shell script; receives task as `$1`, project dir as `$2`

All backends implement `llm.Backend` and are registered in `internal/llm/registry.go`. Selected via `session.llm_backend` in config.

---

### Phase 7 — Container Images and Kubernetes (Planned)

**Scope:** First-class container support for server deployments.

**Deliverables:**
- `Dockerfile` — multi-stage build, distroless runtime image
- `docker-compose.yml` — datawatch + signal-cli sidecar
- GitHub Actions workflow — build and push to `ghcr.io/dmz006/datawatch`
- Helm chart — Kubernetes deployment with persistent volume for data dir
- Documentation: container quickstart, Tailscale sidecar configuration

---

### Phase 8 — Encryption at Rest (Complete — v0.7.1-v0.7.2)

**Scope:** Full encryption at rest with export and post-quantum safe cipher.

**Delivered:**
- XChaCha20-Poly1305 encryption (DWATCH2/DWDAT2 formats, 24-byte nonce)
- Argon2id key derivation with salt embedded in encrypted config header
- Streaming encrypted log writer (DWLOG1 format, 4KB blocks, FIFO pipe)
- `datawatch export` CLI — decrypt and export config, logs, data stores
- `DATAWATCH_SECURE_PASSWORD` env variable for non-interactive operation
- Auto-detect encrypted config, auto-encrypt migration on first --secure start
- `datawatch logs <id> [-n N] [-f]` — tail encrypted or plaintext logs
- Backward compat: reads v1 AES-256-GCM files transparently

---

### Phase 9 — DNS Covert Channel (Complete — v0.7.0)

**Scope:** DNS tunneling communication backend for covert command & control.

**Delivered:**
- Server mode: authoritative DNS via miekg/dns (UDP+TCP)
- Client mode: DNS TXT queries to configured resolver
- HMAC-SHA256 authentication, nonce replay protection (bounded LRU)
- Full messaging.Backend implementation with config, API, web UI
- 15 tests, 86% coverage; live validated with external dig client

---

### Phase 10 — Native Go Signal Backend (Planned — see Plan 2)

**Scope:** Replace signal-cli (Java) with libsignal (Rust FFI) via CGo bindings.

**Status:** Research phase. Plan documented in `.claude/plans/`. Estimated 3-6 months.
- CGo bindings to libsignal_ffi
- Signal server HTTP/WebSocket transport
- Device linking, group messaging, key storage
- Build script for libsignal binary updates

---

### Future — ANSI Console, System Statistics, Flexible Filters (Planned)

**Scope:** See BACKLOG.md #toplan section.

---

## Known Technical Debt

| Item | Impact | Priority |
|---|---|---|
| signal-cli requires Java | Adds ~200 MB to system requirements; Java version mismatches cause failures | High — addressed in Phase 4 |
| QR code library loaded from CDN in PWA | PWA non-functional without internet on first link attempt | Medium — should be bundled in web/ |
| No test suite | Regressions are caught manually; CI cannot verify correctness | High — unit tests for router, store, config; integration tests for tmux manager |
| Output prompt detection uses simple heuristics | May miss novel claude-code prompt patterns; may false-positive | Medium — could use PTY for better accuracy |
| No rate limiting on Signal message handling | A busy group or looping message could exhaust resources | Low — Signal groups are controlled environments |
| `GetByShortID` does linear scan | O(n) lookup; acceptable at max_sessions=10 but not at scale | Low |
| WebSocket CheckOrigin always returns true | Relies entirely on Tailscale for origin security | Low — acceptable given network security model |
| `sessions.json` written on every state change | N writes per session lifecycle; fine at small scale | Low |

---

## Milestones

### v0.1.0 — Multi-Backend Release (current)
- Phases 1–3, 5, 6 complete
- Signal, Telegram, Matrix, Discord, Slack, Twilio, ntfy, email, GitHub webhook, generic webhook backends
- claude-code, aider, goose, gemini, opencode, ollama, openwebui, shell LLM backends
- MCP server for Cursor, Claude Desktop, VS Code, and remote AI agents
- PWA with WebSocket streaming and REST API
- Full install scripts (Linux user/root, macOS, Windows)
- Comprehensive documentation suite

### v0.2.0 — Native Go Signal Backend
- Phase 4 complete
- libsignal-ffi CGO wrapper
- Java-free deployment path
- CI pipeline with CGO build

### v0.3.0 — Test Coverage and Container Support
- Phase 7 complete
- Full unit and integration test suite
- Stable config and WebSocket API (semver guarantee)
- Docker image published to GHCR
- Helm chart published

### v1.0.0 — Stable API
- Stable semver guarantee on config, WebSocket API, MCP tools, and REST API
- CI with automated testing on every PR
- Package repository for apt/rpm/brew
