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

### Phase 3 — Documentation, Install, CLI, Modularity (In Progress)

**Scope:** Make the project production-ready for self-hosted deployment.

**In progress / planned:**
- Comprehensive documentation: `design.md`, `planning.md`, `implementation.md`, `app-flow.md`, `operations.md`, `data-flow.md`
- Install scripts: `install/install.sh` (system), `install/install-user.sh` (user service)
- Package configs: systemd unit files, `Makefile` targets for `make install`
- `datawatch session` subcommands — local CLI without Signal:
  - `datawatch session list`
  - `datawatch session new "<task>"`
  - `datawatch session status <id>`
  - `datawatch session send <id> "<text>"`
  - `datawatch session kill <id>`
  - `datawatch session tail <id> [--lines N]`
  - `datawatch session attach <id>`
- `llm.Backend` interface (defined, `claudecode` implementation complete)
- `messaging.Backend` interface (defined, Signal adapter complete)
- LLM and messaging backend registries

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

### Phase 5 — Additional Messaging Backends (Planned)

**Scope:** Support Slack, Discord, and Telegram as control channels.

**Backends:**
- **Slack** — bot token + Events API (Socket Mode for no-public-URL deployments)
- **Discord** — bot application + Gateway API
- **Telegram** — bot token + long-polling or webhook

**Each backend will:**
- Implement `messaging.Backend`
- Add a config section (e.g. `slack.bot_token`, `discord.bot_token`)
- Support the same command syntax as the Signal interface
- Be selected via `messaging.backend: slack` in config

---

### Phase 6 — Additional LLM Backends (Planned)

**Scope:** Support `aider` and GPT-4 as alternatives to `claude-code`.

**Backends:**
- **aider** — `aider --yes --message "<task>"` in tmux, output piped to log file
- **GPT-4** — thin Go process that calls OpenAI API and streams to tmux/log

**Each backend will:**
- Implement `llm.Backend`
- Be registered in `internal/llm/registry.go`
- Be selected via `session.llm_backend: aider` in config

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

### v0.1.0 — Initial Release (current)
- Phase 1 and Phase 2 complete
- Signal bridge fully functional
- PWA with WebSocket streaming
- QR linking via terminal and browser
- Systemd service files
- README and basic docs

### v0.2.0 — Native CLI and Package Builds
- Phase 3 complete
- `datawatch session` subcommands
- `make install` for system and user installs
- Full documentation suite
- `llm.Backend` and `messaging.Backend` interfaces published

### v0.3.0 — Native Go Signal Backend
- Phase 4 complete
- libsignal-ffi CGO wrapper
- Java-free deployment path
- CI pipeline with CGO build

### v1.0.0 — Stable API, Full Test Coverage, Container Support
- Phases 5–7 complete or in progress
- Full unit and integration test suite
- Stable config and WebSocket API (semver guarantee)
- Docker image published to GHCR
- Helm chart published
