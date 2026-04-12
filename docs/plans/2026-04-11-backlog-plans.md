# Backlog Plans — Comprehensive

**Date:** 2026-04-11
**Covers:** All unplanned backlog items (27 items)

---

## Sessions

### BL84: Tmux History Scrolling
**Effort:** Done (v2.3.2) | **Priority:** medium

Browse tmux scrollback history from the web UI terminal toolbar.

**Implemented:**
- Scroll button in terminal toolbar — sends `Ctrl-b [` to enter tmux copy-mode
- When scroll mode active: PgUp/PgDn buttons and red ESC button appear
- ESC exits copy-mode (sends `q`) and restores normal toolbar
- ESC also added to system saved commands for phone keyboard access
- `sendkey` command updated to support multi-key sequences (`C-b [`)

---

### BL1: IPv6 Listener Support
**Effort:** 1-2 hours | **Priority:** low

Bind HTTP server to `[::]` for dual-stack IPv6+IPv4 support.

**Changes:**
- `internal/server/api.go`: change `net.Listen("tcp", addr)` to support `[::]` notation
- `internal/config/config.go`: validate `server.host` accepts IPv6 addresses
- Test with `curl -6 http://[::1]:8080/api/sessions`

**Risk:** Low. Most Go `net.Listen` already handles dual-stack by default.

---

### BL5: Session Templates
**Effort:** 1 day | **Priority:** medium

Reusable session configurations: project dir, backend, env vars, auto-git settings bundled into named templates.

**Changes:**
- `internal/config/config.go`: add `Templates []SessionTemplate` with fields: name, project_dir, backend, env, auto_git, console_cols/rows, output_mode
- `internal/session/manager.go`: `LaunchFromTemplate(name, task)` resolves template and applies overrides
- `internal/router/router.go`: `new: @template-name: task` syntax
- Web UI: template selector dropdown in new session view
- API: `POST /api/sessions/start` accepts `template` field
- CLI: `datawatch templates list/add/remove`

**Storage:** `~/.datawatch/config.yaml` templates section or separate `templates.yaml`.

---

### BL6: Cost Tracking
**Effort:** 2-3 days | **Priority:** low

Aggregate token usage and estimated cost per session and backend.

**Changes:**
- `internal/session/store.go`: add `TokensIn`, `TokensOut`, `EstCost` fields to Session
- `internal/session/manager.go`: parse token counts from LLM output (Claude shows tokens in completion)
- `internal/server/api.go`: `GET /api/sessions/{id}/cost`, aggregate in stats
- Web UI: cost column in session list, cost card in session detail
- Config: per-backend cost rates ($/1K input tokens, $/1K output tokens)
- MCP: `session_cost` tool

**Challenge:** Each LLM reports tokens differently. Need per-backend parsers.

---

### BL26: Scheduled Prompts (Cron-style)
**Effort:** 1-2 days | **Priority:** medium

Natural language time expressions and recurring schedules for sessions.

**Changes:**
- `internal/session/schedule.go`: add `Recurring bool`, `CronExpr string` fields to ScheduledItem
- Parse natural language: "every day at 9am", "every Monday", "in 2 hours"
- `processScheduledItems()`: check cron expressions, reschedule after execution
- Web UI: recurring toggle in schedule input popup
- Comm: `schedule <id> every 6h: run tests`

**Dependencies:** Consider using `robfig/cron` library for cron expression parsing.

---

### BL27: Project Management
**Effort:** 3-4 hours | **Priority:** medium

Register, select, and switch project directories from comm channels.

**Changes:**
- `internal/config/config.go`: `Projects []ProjectConfig` with name, dir, default_backend
- `internal/router/router.go`: `project list`, `project add <name> <dir>`, `project default <name>`
- `new:` command uses default project dir from active project
- Web UI: project picker in new session view, project list in settings
- API: `GET/POST /api/projects`

---

### BL29: Git Checkpoints
**Effort:** 1 day | **Priority:** medium

Atomic commit before and after every task with rollback on failure.

**Changes:**
- `internal/session/manager.go`: pre-session commit already exists (auto_git). Add:
  - Tag pre-session commit with `datawatch-pre-<session-id>`
  - On failure: `git reset --hard datawatch-pre-<session-id>` (with user confirmation)
  - On success: tag post-session commit with `datawatch-post-<session-id>`
- Comm: `rollback <id>` command to revert to pre-session state
- Web UI: rollback button on failed sessions

**Risk:** Medium. Rollback on dirty working tree needs careful handling.

---

### BL30: Rate Limit Cooldown System
**Effort:** 3-4 hours | **Priority:** medium

Pause all operations on subscription cap, auto-resume with notification.

**Changes:**
- Already partially implemented (StateRateLimited, auto-resume timer)
- Extend: global cooldown flag that pauses new session creation
- Notification to all channels: "Rate limited on backend X, resuming at HH:MM"
- Config: `session.rate_limit_global_pause: true` to stop all backends when one hits limit
- Fallback chain: try next backend profile on rate limit

---

### BL34: Read-only Ask Mode
**Effort:** 2-3 hours | **Priority:** low

Lightweight LLM question without creating a full tmux session.

**Changes:**
- `internal/router/router.go`: `ask: <question>` command
- Uses Ollama/OpenWebUI API directly (no tmux, no session)
- Returns response inline in the comm channel
- Web UI: "Quick Ask" button in header
- API: `POST /api/ask` with `{"question": "...", "backend": "ollama"}`

**Advantage:** Fast (~1s), no session overhead, no cleanup needed.

---

### BL35: Project Summary Command
**Effort:** 2-3 hours | **Priority:** low

Comprehensive project overview from comm channels.

**Changes:**
- `internal/router/router.go`: `summary [dir]` command
- Collects: git status, recent commits, open files, session history for project
- Queries memory for related memories and learnings
- Returns formatted summary to comm channel
- MCP: `project_summary` tool

---

### BL40: Stale Task Recovery
**Effort:** 3-4 hours | **Priority:** medium

Auto-resume or mark-failed sessions stuck in running state after daemon restart.

**Changes:**
- `internal/session/manager.go`: in reconciler, check age of running sessions
- If tmux alive but no output for >30 min: mark as stale, notify
- If tmux dead: mark complete (already done in ReconnectBackends)
- Config: `session.stale_timeout: 1800` (seconds)
- Comm: `stale` command to list stale sessions, `resume <id>` to retry

---

### BL41: Effort Levels per Task
**Effort:** 1-2 hours | **Priority:** low

Configurable effort/thoroughness per session type.

**Changes:**
- `internal/session/store.go`: add `Effort string` field ("quick", "normal", "thorough")
- Pass as flag to Claude Code: `--effort quick`
- Comm: `new: @thorough: complex refactoring task`
- Web UI: effort dropdown in new session view
- Config: per-template default effort level

---

## Intelligence

### BL24: Autonomous Task Decomposition
**Effort:** 1-2 weeks | **Priority:** low | **Depends on:** F15 (pipelines)

`complex:` command breaks tasks into DAG of subtasks with parallel workers.

**Changes:**
- LLM call to decompose task into subtasks with dependencies
- Create pipeline DAG from decomposed tasks
- Each subtask runs as independent session
- Aggregation step collects results
- Retry failed subtasks with context from sibling results

**Prerequisite:** F15 (session chaining/pipelines) must be complete first.

---

### BL25: Independent Verification
**Effort:** 2-3 days | **Priority:** low | **Depends on:** BL24

Separate LLM verifies each task output, fail-closed model.

**Changes:**
- After each subtask completes, spawn verifier session with different backend
- Verifier checks: code compiles, tests pass, output matches spec
- Fail-closed: task blocked until verifier approves
- Config: `verification.enabled`, `verification.backend`

---

### BL28: Quality Gates
**Effort:** 2-3 days | **Priority:** low | **Depends on:** BL24

Test baseline + regression detection, block completion on regression.

**Changes:**
- Pre-session: run test suite, store baseline results
- Post-session: run test suite again, compare
- If new failures: block completion, notify, offer rollback
- Config: `quality_gates.test_command`, `quality_gates.block_on_regression`

---

### BL39: Circular Dependency Detection
**Effort:** 2-3 hours | **Priority:** low | **Depends on:** BL24

Prevent deadlocks in task pipeline DAGs.

**Changes:**
- `internal/session/pipeline.go`: add cycle detection using DFS/topological sort
- Reject pipeline creation with cycles
- Error message: "Circular dependency: A → B → C → A"

---

## Collaboration

### BL7: Multi-user Access Control
**Effort:** 1-2 weeks | **Priority:** low

Role-based permissions, per-user channel bindings, per-user whisper language.

**Changes:**
- `internal/auth/`: new package for user management
- Users: admin (full access), operator (start/send/kill), viewer (list/status/tail)
- Per-user: phone number binding (Signal), language preference (whisper)
- Token-based auth for API (already have `server.token`)
- Web UI: user management in settings (admin only)

---

### BL8: Session Sharing
**Effort:** 1 day | **Priority:** low

Time-limited read-only or interactive links for teammates.

**Changes:**
- `internal/server/api.go`: `POST /api/sessions/{id}/share` generates token + expiry
- Shared URL: `/share/<token>` renders session detail (read-only or interactive)
- Auto-expire after configurable TTL
- Comm: `share <id> [duration]` returns link

---

### BL9: Audit Log
**Effort:** 3-4 hours | **Priority:** low

Append-only record of who started/killed/sent input, exportable.

**Changes:**
- `internal/audit/log.go`: append-only JSON log file
- Record: timestamp, user/channel, action (start/kill/send/configure), session_id, details
- API: `GET /api/audit?since=2026-04-01`
- Web UI: audit tab in settings
- Export: `datawatch audit export --format json|csv`

---

## Observability

### BL10: Session Diffing
**Effort:** 2-3 hours | **Priority:** low

Auto git diff summary in completion alerts (+47/-12, 3 files).

**Changes:**
- `internal/session/manager.go`: after post-session commit, run `git diff --stat HEAD~1`
- Include summary in completion alert: "3 files changed, +47 insertions, -12 deletions"
- Web UI: diff badge on completed session card
- Comm: include in completion message

---

### BL11: Anomaly Detection
**Effort:** 1-2 days | **Priority:** low

Flag stuck loops, unusual CPU/memory, long input-wait.

**Changes:**
- Monitor: output pattern repetition (same line N times = stuck loop)
- Monitor: session duration vs historical average (2x = anomaly)
- Monitor: input-wait duration > threshold (configurable)
- Alert: "Session X appears stuck (same output repeated 50 times)"
- Config: `anomaly.stuck_threshold: 50`, `anomaly.duration_multiplier: 2`

---

### BL12: Historical Analytics
**Effort:** 2-3 days | **Priority:** low

Trend charts in PWA (sessions/day, duration, failure rates).

**Changes:**
- `internal/stats/history.go`: aggregate session data by day/week
- Store: SQLite or JSON time-series
- API: `GET /api/analytics?range=30d`
- Web UI: Chart.js or sparklines in Monitor tab — sessions/day, avg duration, success rate
- Export: CSV download

---

## Messaging

### BL15: Rich Previews
**Effort:** 1 day | **Priority:** low

Syntax-highlighted code snippets or terminal screenshots in alerts.

**Changes:**
- Detect code blocks in LLM responses
- For Signal: send as monospace text (already works)
- For Telegram: send with Markdown code formatting
- For Slack: send as code block attachment
- Optional: capture terminal screenshot as PNG (tmux capture-pane + ANSI-to-image)

---

### BL31: Device Targeting
**Effort:** 1 day | **Priority:** low

`@device` prefix routing across multiple machines.

**Changes:**
- Config: `hostname` already exists. Add `device_aliases: ["prod", "dev"]`
- Router: `new: @prod: deploy task` routes to proxy server named "prod"
- Requires: proxy mode (already implemented) with server aliases
- Web UI: device selector in new session view

---

## Operations

### BL17: Hot Config Reload
**Effort:** 3-4 hours | **Priority:** medium

SIGHUP or API to reload config.yaml without restart.

**Changes:**
- Signal handler: `os.Signal` for SIGHUP
- `internal/config/config.go`: `Reload()` re-reads file, validates, applies
- Re-apply: detection patterns, backend toggles, memory settings
- NOT hot-reloadable: server.host/port, signal config (require restart)
- API: `POST /api/reload`
- Comm: `reload` command

---

### BL22: RTK Auto-install
**Effort:** 1-2 hours | **Priority:** low

`datawatch setup rtk` downloads and installs RTK binary.

**Changes:**
- `cmd/datawatch/main.go`: add `setup rtk` command
- Download latest release from GitHub API
- Install to `~/.local/bin/rtk`
- Run `rtk init --global` to add CLAUDE.md instructions
- Detect platform (linux/darwin, amd64/arm64)

---

### BL37: System Diagnostics
**Effort:** 2-3 hours | **Priority:** low

`diagnose` health checks from comm channels.

**Changes:**
- `internal/router/router.go`: `diagnose` command
- Checks: tmux available, LLM backends reachable, memory store connected, Signal/Telegram connected, disk space, session store readable
- Returns: pass/fail per check with details
- MCP: `diagnose` tool
- API: `GET /api/diagnose`

---

## Backends

### BL20: Backend Auto-selection
**Effort:** 1 day | **Priority:** low

Route to best backend based on task type, load, or rules.

**Changes:**
- `internal/config/config.go`: `routing_rules` section with patterns
- Rules: "if task contains 'test' → use claude-code", "if all sessions busy → use ollama"
- `internal/session/manager.go`: apply rules before backend selection
- Fallback: configured default backend
- Web UI: routing rules editor in settings

---

### BL42: Quick-response Assistant
**Effort:** 3-4 hours | **Priority:** low

Lightweight secondary LLM for general questions without sessions.

**Changes:**
- Similar to BL34 (ask mode) but with dedicated "assistant" backend
- Config: `assistant.backend: ollama`, `assistant.model: llama3`
- Comm: `assist: what is X?` for quick answers
- Could share conversation state for follow-ups (lightweight context)

---

## Memory

### BL45: ChromaDB/Pinecone/Weaviate Backends
**Effort:** 1-2 days each | **Priority:** low

Cloud-native vector databases for scale.

**Changes per backend:**
- `internal/memory/backends/<name>/store.go`: implement `memory.Backend` interface
- Methods: Save, Search, Delete, List, Stats
- Config: `memory.backend: chromadb`, `memory.chromadb_url: http://...`
- Test: same test suite as SQLite/PostgreSQL

**Order:** ChromaDB first (most popular), then Pinecone, then Weaviate.

---

## Security

### BL38: Message Content Privacy
**Effort:** 2-3 hours | **Priority:** medium

Disable logging of prompts/inputs in alerts.

**Changes:**
- Config: `privacy.mask_prompts: true`, `privacy.mask_responses: true`
- Alert handler: replace content with "[redacted]" when masked
- Session store: optionally omit `last_input`, `last_prompt`, `last_response`
- Log files: not affected (already encrypted in secure mode)
- Web UI: privacy toggle in settings

---

## Extensibility

### BL33: Plugin Framework
**Effort:** 2-3 days | **Priority:** low

Auto-discovered plugins in `plugins/` directory.

**Changes:**
- Plugin types: command handlers, output filters, notification hooks
- Discovery: scan `~/.datawatch/plugins/*.so` (Go plugins) or `plugins/*.py` (subprocess)
- Interface: `Plugin.Init(config)`, `Plugin.HandleCommand(cmd)`, `Plugin.FilterOutput(line)`
- Config: `plugins.enabled: true`, `plugins.dir: ~/.datawatch/plugins`
- Security: plugins run with same permissions as daemon — document risks

**Alternative:** Consider Lua/JS scripting via embedded interpreter instead of native plugins.

---

## Operations (continued)

### BL85: RTK Auto-update Check
**Effort:** 2-3 hours | **Priority:** medium | **Category:** operations

On daemon start, check GitHub for the latest RTK release. If a newer version is available:
1. If auto-update is enabled and binary is in a writable location, download and replace it
2. Otherwise, show a red indicator on the Monitor page with the new version info

**Changes:**

`internal/rtk/rtk.go`:
- `CheckLatestVersion()` — query `https://api.github.com/repos/rtk-ai/rtk/releases/latest`, parse `tag_name`, compare with installed version
- `UpdateBinary(targetPath string)` — download platform-specific binary from release assets, replace existing binary, verify with `--version`
- `VersionStatus` struct: `CurrentVersion`, `LatestVersion`, `UpdateAvailable`, `AutoUpdatable`, `LastChecked`

`internal/config/config.go`:
- Add to `RTKConfig`: `AutoUpdate bool` (yaml: `auto_update`), `UpdateCheckInterval int` (yaml: `update_check_interval`, default 86400 = daily)
- Expose via all config channels (API, web, comm, MCP)

`cmd/datawatch/main.go`:
- On startup after RTK detection: call `CheckLatestVersion()`, log result
- If `auto_update` enabled and update available: attempt `UpdateBinary()`
- Store version status in stats for Monitor page

`internal/server/web/app.js`:
- Monitor tab: RTK card shows version with green (up to date) or red (update available) indicator
- Red badge: "RTK v0.34.2 → v0.35.0 available" with optional "Update" button

`internal/server/api.go`:
- `GET /api/config` includes `rtk.auto_update` and `rtk.update_check_interval`
- `PUT /api/config` accepts `rtk.auto_update` and `rtk.update_check_interval`

**Platform detection for downloads:**
- `runtime.GOOS` + `runtime.GOARCH` → asset name (e.g., `rtk-linux-amd64`, `rtk-darwin-arm64`)
- Download from GitHub release asset URL
- `chmod +x` after download

**Config example:**
```yaml
rtk:
  enabled: true
  auto_update: true
  update_check_interval: 86400  # seconds (daily)
```

**Risk:** Binary replacement while running — RTK is a proxy, not a long-running daemon. Safe to replace between invocations.

---

### BL87: Config Edit Command
**Effort:** 3-4 hours | **Priority:** medium | **Category:** operations

`datawatch config edit` — safe interactive config editor, visudo-style.

**Behavior:**
1. Open config file in `$EDITOR` (falls back to `$VISUAL`, then `vim`, then `nano`)
2. After save+quit, validate the YAML syntax and config structure
3. If validation fails: show the error, ask to re-edit or exit without saving (like visudo)
4. If validation passes: write back to config, optionally reload daemon
5. Loop until valid or user chooses to exit without saving

**Encrypted config support (`--secure` mode):**
1. Decrypt `config.yaml.enc` to a temp file (`/dev/shm/` preferred for RAM-only)
2. Open temp file in editor
3. On save: validate, re-encrypt, write back to `config.yaml.enc`
4. **Secure wipe** the temp file with `wipe -f` (falls back to `shred -u`, then overwrite+delete)
5. Never leave decrypted config on disk

**Changes:**
- `cmd/datawatch/main.go`: add `config edit` subcommand
- Validation: unmarshal YAML into `config.Config` struct, check required fields
- Temp file: `os.CreateTemp("/dev/shm", "datawatch-config-*")` with `0600` perms
- Secure delete: try `wipe -f`, fall back to `shred -u`, fall back to manual overwrite+unlink
- After successful save: offer to send SIGHUP for hot reload (if BL17 implemented) or print "restart required"

**Config:**
- `EDITOR` / `VISUAL` env vars (standard Unix)
- No datawatch config needed — this is a CLI-only command

**Safety:**
- Validate before saving (syntax + structural checks)
- Never exit with invalid config written
- Encrypted temp file in RAM-backed filesystem
- Secure wipe with verification

---

### BL86: Remote GPU/System Stats Agent
**Effort:** 1-2 days | **Priority:** medium | **Category:** observability

Collect GPU utilization, temperature, VRAM, and system metrics from remote Ollama/GPU servers where `nvidia-smi` isn't available locally.

**Problem:** When Ollama runs on a different machine (`ollama.host: http://remote:11434`), the local `nvidia-smi` shows the local GPU, not the Ollama server's GPU. The Ollama API (`/api/ps`, `/api/tags`) provides model info and VRAM usage but NOT GPU utilization %, temperature, or system CPU/memory.

**Approaches (implement one):**

**Option A: Lightweight HTTP stats endpoint (recommended)**
- Small Go binary (`datawatch-agent`) that runs on the remote server
- Exposes `GET /stats` returning JSON: GPU util/temp/VRAM, CPU, memory, disk
- Datawatch polls it alongside Ollama API
- Config: `ollama.stats_agent: http://remote:9877`
- Install: single binary, systemd service, no deps

**Option B: SSH-based collection**
- Datawatch SSHs to remote host and runs `nvidia-smi` + `free` + `df`
- Config: `ollama.ssh_host`, `ollama.ssh_user`, `ollama.ssh_key`
- Pro: no agent to install. Con: SSH overhead, key management

**Option C: Prometheus/node_exporter scraping**
- If the remote host already runs `node_exporter` + `dcgm_exporter`
- Datawatch scrapes their `/metrics` endpoints
- Config: `ollama.node_exporter: http://remote:9100`, `ollama.dcgm_exporter: http://remote:9400`
- Pro: standard tooling. Con: requires separate install

**Data collected:**
| Metric | Source |
|--------|--------|
| GPU utilization % | nvidia-smi / agent |
| GPU temperature | nvidia-smi / agent |
| GPU VRAM used/total | nvidia-smi / agent + Ollama /api/ps |
| GPU power draw | nvidia-smi / agent |
| System CPU % | /proc/stat / agent |
| System memory used/total | /proc/meminfo / agent |
| Disk usage | df / agent |
| Ollama models loaded | Ollama /api/ps (already collected) |

**Changes:**
- `internal/stats/remote_agent.go`: HTTP client to poll remote stats agent
- `internal/stats/collector.go`: add `RemoteGPU` fields to SystemStats
- `internal/config/config.go`: `ollama.stats_agent_url` config field
- Web UI: Monitor page shows remote GPU stats alongside local GPU
- Config via all channels (API, web, comm, CLI, MCP)

---

## Testing Infrastructure

### BL89: Mock Session Manager
**Effort:** 1 day | **Priority:** medium | **Category:** testing

Create an interface-based mock session manager that router and server handlers can use without tmux.

**Changes:**
- `internal/session/mock_manager.go`: implements Manager interface with in-memory state
- No tmux, no log files, no tmux capture-pane — just state transitions
- Track: sessions created, inputs sent, kills, state changes
- Used by router handler tests and server API tests

**Expected coverage gain:** router 10% → 40%, session 15% → 25%

---

### BL90: httptest Server for API Tests
**Effort:** 1-2 days | **Priority:** medium | **Category:** testing

Test all 65 API endpoints using Go's httptest package with mock dependencies.

**Changes:**
- `internal/server/api_test.go`: httptest.NewServer with mock manager, memory, pipeline
- Test request/response contracts for every endpoint
- Verify status codes, JSON structure, error handling
- Cover: sessions CRUD, memory CRUD, pipeline CRUD, config GET/PUT, alerts, schedules

**Expected coverage gain:** server 0% → 30-40%, overall 14.5% → 25%+

---

### BL91: MCP Tool Handler Tests
**Effort:** 1 day | **Priority:** medium | **Category:** testing

Test all 44 MCP tool handlers by calling them directly without transport.

**Changes:**
- `internal/mcp/server_test.go`: create Server with mock manager/memory/KG
- Call each handler function directly with mock CallToolRequest
- Verify response text, error handling, nil checks
- Cover: session tools, memory tools, KG tools, pipeline tools, config tools

**Expected coverage gain:** mcp 0% → 40-50%, overall → 28%+
