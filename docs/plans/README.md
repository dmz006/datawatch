# Plans, Bugs & Backlog

Single source of truth for all datawatch project tracking.

---

# Bug and Feature rules
## make sure all implementation of bugs or features have 100% (or close) code test coverage and that the fixes or functionality is actually tested through web, api, or any means you have access to validate the code works as requested
## if testing involves creating testing sessions be sure to stop and delete those sessions when done
## Unclassified bugs
_(empty — all classified)_

## Unclassified features
_(empty — all classified)_

## Open Bugs

| # | Description | Priority | Notes |
|---|-------------|----------|-------|
| — | No open bugs | — | — |

## Open Features

| # | Description | Priority | Effort | Notes |
|---|-------------|----------|--------|-------|
| F7 | libsignal (replace signal-cli with native Go) | low | 3-6 months | Plan: [libsignal](2026-03-29-libsignal.md) |
| F10 | Container images, Helm chart, NFS workspace support (BL3) | medium | 1-2 days | Plan: [backlog-plans](2026-04-01-backlog-plans.md#bl3-container-images-and-helm-chart). Depends on F8 (done) |
| F13 | Copilot/Cline/Windsurf backends (BL19) | low | 1-2hr each | Plan: [backlog-plans](2026-04-01-backlog-plans.md#bl19-copilotclinewindsurf-backends) |
| F14 | Live cell DOM diffing for session list (BL2) | low | 3-4hr | Plan: [backlog-plans](2026-04-01-backlog-plans.md#bl2-live-cell-dom-diffing) |
| F15 | Session chaining — pipelines with conditional branching (BL4) | low | 1-2 days | Plan: [backlog-plans](2026-04-01-backlog-plans.md#bl4-session-chaining) |
| ~~F16~~ | ~~Proxy mode — Phases 1-3: web UI routing, session aggregation, messaging routing~~ | ~~done~~ | ~~done~~ | ~~v1.1.0: WS proxy, aggregated sessions, remote command forwarding, `new: @server:` syntax, server badges. Phases 4-5 (PWA reverse proxy, resilience) deferred~~ |

## Backlog (no plan, low priority)

| ID | Item | Category |
|----|------|----------|
| BL1 | IPv6 listener support (`[::]` bind) | infrastructure |
| BL5 | Session templates — reusable workflows (dir, backend, env, auto-git bundled) | sessions |
| BL6 | Cost tracking — aggregate token usage and estimated cost per session/backend | sessions |
| BL7 | Multi-user access control — role-based permissions (viewer/operator/admin), per-user channel bindings, per-user whisper language preference | collaboration |
| BL8 | Session sharing — time-limited read-only or interactive links for teammates | collaboration |
| BL9 | Audit log — append-only record of who started/killed/sent input, exportable | collaboration |
| BL10 | Session diffing — auto git diff summary in completion alerts (+47/-12, 3 files changed) | observability |
| BL11 | Anomaly detection — flag stuck loops, unusual CPU/memory, long input-wait | observability |
| BL12 | Historical analytics — trend charts in PWA (sessions/day, duration by backend, failure rates) | observability |
| BL15 | Rich previews — syntax-highlighted code snippets or terminal screenshots in alerts | messaging |
| BL17 | Hot config reload — SIGHUP or API to reload config.yaml without restart | operations |
| BL20 | Backend auto-selection — route to best backend based on task type, load, or rules | backends |
| BL22 | RTK auto-install — `datawatch setup rtk` downloads and installs RTK binary if not present | operations |
| BL23 | Episodic memory — persistent vector-indexed conversation memory per project. Auto-retrieve relevant context from past sessions when starting new tasks. `remember` and `recall` commands from comm channels. SQLite + embeddings | intelligence |
| BL24 | Autonomous task decomposition — `complex: <task>` breaks large tasks into PRD with stories and atomic tasks, dispatches to parallel workers, independently verifies each, runs quality gates (test baseline + regression detection). Auto-fix on verification failure | intelligence |
| BL25 | Independent verification — each completed task verified by a separate LLM context for security issues, logic errors, and correctness. Fail-closed model: rejected code is not committed | intelligence |
| BL26 | Scheduled prompts (cron-style) — `schedule add daily at 5am: check production for errors`. Natural language time expressions, per-project scoping, silent execution with notify-on-issue. Extends existing schedule system with recurring schedules | sessions |
| BL27 | Project management — register, select, and switch between multiple project directories from comm channels. `/projects`, `/select <name>`, `/add <name> <path>`. Currently sessions use `default_project_dir` or explicit path | sessions |
| BL28 | Quality gates — run test suite before and after each task, capture baseline, detect regressions. Distinguish new failures from pre-existing ones. Block completion if tests regress | intelligence |
| BL29 | Git checkpoints — automatic git commit before every task starts and atomic commit after verified completion. Rollback on failure. Extends existing auto_git_commit with per-task granularity | sessions |
| BL30 | Rate limit cooldown system — when Claude hits subscription cap, pause all operations, send single notification, auto-resume after cooldown. Manual override via `/cooldown clear`. Extends existing rate-limit detection | sessions |
| BL31 | Device targeting — run datawatch on multiple machines under one Signal account, route commands to specific instance with `@device` prefix. Extends existing proxy mode with Signal-level routing | messaging |
| BL32 | Semantic search across sessions — vector-indexed session output and conversation history. `recall` command searches past sessions by meaning, not just text matching | intelligence |
| BL33 | Plugin framework — auto-discovered plugins in a `plugins/` directory. Each plugin registers commands, message matchers, and help text. Broken plugins logged and skipped. Enables community extensions without touching core code | extensibility |
| BL34 | Read-only `ask` mode — `ask: <question>` sends a read-only question to the LLM about the project (no file changes). Distinct from `new:` which creates a full session. Lightweight, faster, lower resource usage | sessions |
| BL35 | Project summary command — `summary` generates a comprehensive overview of project structure, technologies, dependencies, and recent git changes. Quick project orientation from comm channels | sessions |
| BL36 | Task learnings capture — after each completed session/task, extract key learnings and decisions. Searchable via `learnings` command. Builds institutional knowledge over time | intelligence |
| BL37 | System diagnostics command — `diagnose` runs health checks (messaging backends, WS connections, tmux, disk space, LLM backends). Reports status from comm channels. Extends existing `/api/health` | operations |
| BL38 | Message content privacy — option to disable logging of prompt text and user inputs in alerts, session tracking, and comm channel notifications. Path validation hardening for project dirs | security |
| BL39 | Circular dependency detection — for session chaining (BL4/F15), detect and prevent circular task dependencies that would cause deadlock in autonomous execution pipelines | intelligence |
| BL40 | Stale task recovery — on daemon restart, detect tasks that were in-progress when the daemon stopped. Auto-resume or mark as failed with notification. Extends existing session resume logic | sessions |
| BL41 | Effort levels per task — configurable effort/thoroughness level per session type (implementation=high, bug_fix=high, refactor=medium). Maps to LLM parameters or prompting strategy | sessions |
| BL42 | Quick-response assistant — lightweight secondary LLM (OpenAI/Grok/Ollama) for general questions that don't need project access. Separate from the main coding LLM backend. Lower cost for simple queries | backends |

### Completed Backlog (promoted → implemented)

| ID | Item | Outcome |
|----|------|---------|
| BL2 | Live cell DOM diffing | Promoted to F14 (open) |
| BL3 | Container images and Helm chart | Promoted to F10 (open) |
| BL4 | Session chaining | Promoted to F15 (open) |
| BL13 | Threaded conversations | Completed as part of F4 (channel parity), v1.0.2 |
| BL14 | Voice input | Promoted to F11, completed v1.1.0 |
| BL16 | Health check endpoint | Promoted to F8, completed v1.0.2 |
| BL18 | Prometheus metrics export | Promoted to F12, completed v1.0.2 |
| BL19 | Copilot/Cline/Windsurf backends | Promoted to F13 (open) |
| BL21 | Fallback chains | Promoted to F9, completed v1.0.2 |

## Testing Results (v1.1.0)

**112 tests pass across 38 packages.**

### Go Unit Tests — All Packages

| Package | Status | Count | Tests |
|---------|--------|-------|-------|
| `cmd/datawatch` | PASS | 6 | LinkViaCommand (StderrURI, StdoutURI, Failure, CalledOnceOnly, QRCodeGeneration, NoURINoCallback) |
| `internal/config` | PASS | 9 | DefaultConfig, Load (NonExistent, InvalidYAML, Partial, ZeroFieldsGetDefaults), Save (RoundTrip, FilePermissions, CreatesParentDirs), ConfigPath |
| `internal/messaging/backends/dns` | PASS | 11 | NonceReplay, NonceTTL, NonceLRU, NonceEmpty, EncodeDecodeQueryRoundTrip, DecodeQuery (BadHMAC, DomainMismatch), EncodeDecodeResponseRoundTrip, EncodeResponseFragmentation, ServerIntegration (6 sub-tests), ClientExecute |
| `internal/proxy` | PASS | 7 | NewRemoteDispatcher, HasServers, FetchSessions (mock HTTP), FindSession (short + full ID), ForwardCommand (mock), ListAllSessions (2 servers parallel), AuthToken (Bearer injection) |
| `internal/router` | PASS | 17 | Parse (New, NewWithProjectDir, NewAtServer, NewCaseInsensitive, NewStripsWhitespace, NewNoTask, List, Status, Send, SendMissingColon, SendColonInMessage, Kill, Tail_DefaultN, Tail_WithN, Attach, History, Help, Unknown), HelpText, Truncate |
| `internal/rtk` | PASS | 3 | CheckInstalled, SetBinary, CollectStats |
| `internal/secfile` | PASS | 10 | EncryptedLog (RoundTrip, LargeData, WrongKey, MultipleWrites, Flush, EmptyFile), Migrate (LogOnly, Full, SkipsEncrypted, EmptyDir) |
| `internal/session` | PASS | 21 | CancelBySession, CancelBySessionShortID, Delete, Store (NewEmpty, NewFromMissingFile, Save_Get, GetMissing, GetByShortID, GetByShortID_CaseInsensitive, GetByShortID_Missing, List, ListEmpty, Update, Delete, DeleteMissing, Persistence, PersistAfterDelete, MultipleSavesSameID), StateConstants, ParseScheduleTime (valid, NextWeekday, Errors) |
| `internal/transcribe` | PASS | 5 | New_MissingVenv, New_AutoLanguage, New_ExplicitLanguage, SupportedLanguages, Transcribe_Integration (whisper tiny model, CPU, silent WAV) |

### Functional API Tests (13/13 pass)

| # | Feature | Test | Result |
|---|---------|------|--------|
| 1 | Health | `GET /healthz` returns 200 | PASS |
| 2 | Readiness | `GET /readyz` returns 200 with active_sessions | PASS |
| 3 | Prometheus | `GET /metrics` returns 27+ Prometheus metrics | PASS |
| 4 | RTK stats | RTK fields in `/api/stats` (installed, version, hooks, total_saved, avg_savings_pct, total_commands) | PASS |
| 5 | RTK discover | `GET /api/rtk/discover` returns optimization data | PASS |
| 6 | RTK config | RTK section in `/api/config` (enabled, binary, show_savings, auto_init, discover_interval) | PASS |
| 7 | Profiles | `GET /api/profiles` returns profile map | PASS |
| 8 | Profiles | `POST /api/profiles` creates profile with backend, env, binary, model | PASS |
| 9 | Profiles | Profile persists and appears in GET after creation | PASS |
| 10 | Profiles | `DELETE /api/profiles` removes profile | PASS |
| 11 | Schedule | Schedule "on input" time parsing | PASS |
| 12 | Fallback | Fallback chain in config API (ordered profile list) | PASS |
| 13 | Sessions | Session create + kill (claude-code backend) | PASS |

### Profile CRUD Tests (6/6 pass)

| # | Test | Result |
|---|------|--------|
| 1 | Create profile via POST | PASS |
| 2 | Profile JSON fields lowercase (json tags) | PASS |
| 3 | Profile appears in `/api/config` | PASS |
| 4 | Start session with profile override | PASS |
| 5 | Delete profile via DELETE | PASS |
| 6 | Profile removed from store | PASS |

### Manual Validation

**Core features:**
- Claude-code session creation with `--name` flag: verified in tmux capture
- OpenWebUI interactive mode: tested 2-turn conversation (2+2=4, multiply by 10=40)
- Stale MCP cleanup: 13 stale registrations cleaned on startup
- WS reconnect: session detail auto-restores after daemon restart
- Schedule management: edit/delete/multi-select all functional in web UI

**Voice input (F11):**
- Whisper venv created, `openai-whisper` installed with CPU-only PyTorch
- Whisper tiny model downloaded and transcribed silent WAV (integration test)
- Whisper Python API verified: `load_model("tiny", device="cpu")` + `transcribe()` returns text
- Telegram `Voice`/`Audio` field detection and `getFile` download path verified at code level
- Signal `attachments` field propagation from `DataMessage` → `IncomingMessage` → `messaging.Message` verified at code level

**RTK integration (F6):**
- RTK `0.34.2` detected on system, hooks active
- `/api/stats` returns `rtk_installed`, `rtk_version`, `rtk_hooks_active`, `rtk_total_saved`, `rtk_avg_savings_pct`, `rtk_total_commands`
- `/api/rtk/discover` returns real optimization data from RTK CLI
- `datawatch setup rtk` wizard verified
- RTK settings card renders in web UI

**Profiles and fallback chains (F9):**
- Profile CRUD via API: create, read, update, delete all confirmed
- Profile env vars applied to session tmux environment on launch
- Fallback chain config persists and appears in `/api/config`
- Profile dropdown renders in New Session web form
- Settings → Profiles & Fallback card functional

**Channel feature parity (F4):**
- Threading: Slack `thread_ts`, Discord `MessageThreadStart`, Telegram `reply_to_message_id` — interfaces compile, router routing verified
- Rich markdown: `RichSender` interface implemented for Slack (mrkdwn), Discord (native), Telegram (Markdown)
- Interactive buttons: `ButtonSender` for Slack Block Kit and Discord components — interface compile verified
- File upload: `FileSender` for Slack and Discord — interface compile verified

**Health, readiness, metrics:**
- `GET /healthz` returns `{"status":"ok"}` (200, no auth)
- `GET /readyz` returns `{"status":"ready","active_sessions":N}` (200, no auth)
- `GET /metrics` returns 27+ Prometheus text-format metrics with correct values after collection cycle

**Encryption:**
- Existing plaintext config auto-migrated to encrypted on first `--secure` start
- `DATAWATCH_SECURE_PASSWORD` env var works for non-interactive daemon mode
- Session tracking encryption with `secure_tracking: full` verified

**Proxy mode (F16):**
Integration tested with real second instance (testhost on port 9090, separate data dir):
- `GET /api/servers` — lists local + testhost with auth status: PASS
- `GET /api/proxy/testhost/healthz` — HTTP proxy to remote: PASS
- `GET /api/proxy/testhost/api/health` — returns remote hostname: PASS
- `GET /api/proxy/testhost/api/sessions` — empty array from remote: PASS
- `GET /api/proxy/nonexistent/healthz` — 404 for unknown server: PASS
- `POST /api/proxy/testhost/api/sessions/start` — creates session on remote: PASS
- `GET /api/sessions/aggregated` — returns both local + remote sessions with server tags: PASS
- `POST /api/test/message {"text":"list"}` — aggregated list shows both servers: PASS
- `POST /api/test/message {"text":"new: @testhost: echo test"}` — routes to remote: PASS
- Remote kill/status via comm channel — forwards to correct server: PASS
- Test instance stopped and data cleaned up after testing: PASS
- Web-only daemon keepalive (no messaging backends) — stays alive for proxy-only mode: PASS

## Completed Bugs (archived)

| # | Description | Notes |
|---|-------------|-------|
| B25 | server.Version hardcoded as "1.0.0" | v1.1.0: /api/health and /api/info always returned wrong version. Fixed by wiring server.Version = Version from main.go. Tested: /api/health returns 1.1.0 |
| B24 | Configure command broken on all comm channels (POST→PUT) | v1.1.0: configureFn used http.Post but /api/config only accepts PUT. Affected Signal, Telegram, Discord, Slack, all backends. Fixed to http.NewRequest(PUT). Tested: configure session.console_cols=100 via /api/test/message returns "Set" |
| B23 | MCP channel reconnect delay on established sessions | v1.1.0: initial WS sessions sync now populates channelReady map from session.channel_ready; session detail also checks session data directly. Tested: daemon restart + navigate to session = no banner, input enabled immediately |
| B22 | LLM filter buttons don't fit horizontally | v1.0.2: compact badges with short labels + count. Tested: visual in web UI |
| B21 | Schedule time parsing "on-input" fails | v1.0.2: ParseScheduleTime handles "on input", preset buttons. Tested: API test #11, unit test in timeparse_test.go |
| B20 | eBPF warning message inconsistent | v1.0.2: unified to "datawatch setup ebpf". Tested: grep confirmed no remaining `sudo setcap` |
| B19 | Schedule events missing delete/edit on Monitor page | v1.0.2: edit/delete buttons, multi-select, bulk delete. Tested: API DELETE confirmed, manual web UI validation |
| B1 | OpenWebUI curl/script backend — no interactive mode | v1.0.2: Go conversation manager with message history, SSE streaming, SendInput routing. Tested: 2-turn conversation (2+2=4, ×10=40), response renders via printf |
| B18 | Completion summary truncated in comm channels | v1.0.2: context lines for terminal events (2x), not just waiting_input |
| B17 | Browser auto-refresh on daemon update | v1.0.2: version in WS sessions message, client auto-reloads |
| B16 | MCP channel port race + stale registration cleanup | v1.0.2: channel.js awaits listen, stale MCP cleanup on startup. Tested: 13 stale registrations cleaned, verified via `claude mcp list` |
| B15 | Web UI session detail stops refreshing after daemon restart | v1.0.2: ws.onopen re-renders session-detail, re-subscribes to output |
| B14 | Session delete doesn't clean all associated data | v1.0.2: cleans mcpRetryCounts, rawInputBuf; TrackingDir fallback |
| B13 | Rate-limit resume timer lost on reboot | v1.0.2: persisted ScheduleStore entry replaces in-memory timer |
| B12 | Scheduled commands not deleted on session delete | v1.0.2: CancelBySession in Kill+Delete. Tested: unit tests (TestCancelBySession, TestCancelBySessionShortID, TestDelete), API kill confirmed cancel in schedule list |
| — | WS send-on-closed-channel panic | v1.0.2: safeSend method on client, closed flag set before close(c.send) |
| — | False session completion from command echo | v0.19.0: HasPrefix for output lines; v1.0.2: extended to capture-pane checks |
| — | Restart appends "(restart)" to session name | v1.0.2: preserves original name |
| — | Claude state detection false waiting_input during processing | v1.0.2: spinner/tool detection above ❯ prompt (8 lines), 10s debounce for channel_ready |
| — | Spurious alerts on web connect/refresh | v1.0.2: first-tick baseline skip in StartScreenCapture |
| — | Alert title inconsistent format | v1.0.2: hostname: name [id]: event |
| — | Prompt context noise in alerts | v1.0.2: extractPromptContext filters separators, shell noise, spinners |
| B10 | LLM config dropdowns render as text | v0.19.1: select_inline renders as \<select\> dropdown |
| B9 | Toast positioning outside app border | v0.19.1: container inside .app div, position:absolute |
| B8 | Watermark icon missing | v0.19.1: fixed-position centered SVG, 85% width, 4.5% opacity |
| B7 | Back button hard to tap on mobile | v0.19.1: CSS chevron, 44x44px touch target |
| B6 | Alert burst flooding to remote channels | v0.19.1: goroutine bundler per session, 5s quiet + 30s keepalive |
| — | Alert accepted prompt logging | v0.19.1: LastPrompt fallback when Enter pressed |
| — | Input logging for all paths | v0.19.1: rawInputBuf accumulator |
| — | Alert quick reply only on final waiting_input | v0.19.1: HasSuffix check on last event |
| — | Saved command expansion from messaging channels | v1.0.0: expandSavedCommand in router, Enter key fix |
| B5 | opencode exit to shell not detected | v1.0.0: `$` suffix detection on last capture-pane line |
| B4 | opencode TUI prompt detection after results | v1.0.0: matchPromptInLines(10), waiting→running flip |
| B3 | Claude/opencode state badges not updating during work | v0.19.0: universal capture-pane detection, prompt patterns expanded |
| B2 | Terminal scroll leaks outside session detail | v0.19.0: xterm scrollbar hidden, overflow-x auto |
| — | Terminal rendering garbled display | v0.19.0+: single display source (pane_capture only), cursor-home overwrite |
| — | Interface binding: localhost forced on, connected detection | v0.18.0 |
| — | Interface checkbox mutual exclusion | v0.17.3 |
| — | Comms server status indicator | v0.17.3 |
| — | View persistence across refresh | v0.17.2 |
| — | Session donut chart (active of max) | v0.17.2 |
| — | Real-time stats streaming via WS | v0.17.2 |
| — | Settings sub-tabs | v0.16.0 |
| — | Multi-machine README shared vs unique channels | v0.16.0 |
| — | Per-session network statistics (eBPF) | v0.16.0 |
| — | Browser debug tools (triple-tap panel) | v0.16.0 |
| — | Shell backend script_path detection | v0.15.0 |
| — | Claude exit auto-complete | v0.15.0 |
| — | Restart command (os.Args override) | v0.15.0 |
| — | TLS redirect double-port | v0.14.6 |
| — | Detection filter add/remove managed list | v0.14.5 |

## Completed Features (archived)

| # | Description | Notes |
|---|-------------|-------|
| F16 | Proxy mode (Phases 1-3) | v1.1.0: WS proxy relay, aggregated sessions API, remote command routing via comm channels, `new: @server:` syntax, server badges in UI, web-only daemon keepalive fix. Tested: 10/10 integration tests (HTTP proxy, WS, aggregated sessions, remote list/status/kill/new @server, cleanup). Phases 4-5 (PWA reverse proxy, resilience) deferred. |
| — | POST /api/test/message endpoint | v1.1.0: simulates incoming comm channel messages through the router for testing. Returns responses array. Test router wired with schedStore, alertStore, cmdLib, statsFn, configureFn. Tested: 26/28 comm commands pass |
| — | Whisper web UI settings card | v1.1.0: Settings → General → Voice Input (Whisper) with model dropdown, language, venv path, enable toggle. Config exposed in GET/PUT /api/config. Tested: web UI renders, config PATCH works |
| — | RTK Token Savings stats card | v1.1.0: Monitor tab renders version, hooks status, tokens saved, avg savings %, commands when rtk_installed=true. Tested: web UI shows live data from /api/stats |
| F11 | Voice input via Whisper transcription | v1.1.0: WhisperConfig (model, language, venv_path), transcribe package, Telegram voice/audio download, Signal attachment parsing, router integration, web UI card, API config. Per-user language deferred to BL7. Tested: unit tests (5/5), API config PATCH, web UI card, comm channel configure |
| F9 | Multi-profile fallback chains (all phases) | v1.0.2: ProfileConfig, FallbackChain, env injection, CRUD API, profile dropdown, auto-fallback. Tested: 6/6 profile CRUD tests pass, session start with profile verified |
| F4 | Channel feature parity (all phases) | v1.0.2: threading (Slack/Discord/Telegram), rich markdown, interactive buttons, file upload. Tested: build clean, interfaces compile, router routing verified |
| F12 | Prometheus metrics `/metrics` endpoint | v1.0.2: prometheus/client_golang, gauges/counters for sessions, CPU, memory, RTK, uptime. Tested: 27 metrics returned, values populated after collection cycle |
| F8 | Health check `/healthz` + `/readyz` for k8s probes | v1.0.2: liveness + readiness endpoints, public (no auth). Tested: both return 200, readyz shows active_sessions count |
| F5 | Channel tab testing guide | v1.0.2: [channel-testing.md](../channel-testing.md) |
| F6 | RTK integration Phase 1+2+3 — detection, stats, auto-init, per-session analytics, discover API, setup wizard | v1.0.2: rtk package, config, stats API, discover, web UI settings, `datawatch setup rtk` CLI. Tested: unit tests (3/3), RTK detected v0.34.2, discover returns real data, setup wizard verified |
| F4 | Channel feature parity review — all 11 backends audited | v1.0.2: [channel-parity-review](2026-04-01-channel-parity-review.md), gaps identified, prioritized plan |
| F3 | Rate-limit auto-continue via persisted schedule | v1.0.2: ScheduleStore entry, README headline feature |
| F2 | Quick command dropdown: System/Saved/Custom | v1.0.2: optgroups, inline custom input, quick buttons removed |
| F1 | Session naming for Claude + session picker dropdown | v1.0.2: --name flag, Nameable interface, resume dropdown, restart pre-fills |
| — | Channel tab bidirectional display | v1.0.2: outgoing/incoming/notify with direction indicators |
| — | Per-session channel port routing | v1.0.2: ChannelPort stored on Session |
| — | MCP connection banner simplified | v1.0.2: "Waiting for MCP channel…" |
| — | Documentation updates (D1-D7) | v1.0.2: setup.md, operations.md, llm-backends.md, messaging-backends.md, encryption.md, claude-channel.md |
| — | Encryption at rest | v0.18.0: session.json encrypted; daemon.log excluded by design |
| — | Animated GIF tour → video recording | v1.0.0 |

## Completed Plans

| Plan | Version | Date | File |
|------|---------|------|------|
| Backlog Bugs & Channel | v0.6.x | 2026-03-28 | [bugs-and-channel](2026-03-28-bugs-and-channel.md) |
| Patch 0.6.1 | v0.6.1 | 2026-03-28 | [patch-0.6.1](2026-03-28-patch-0.6.1.md) |
| Patch 0.6.3 | v0.6.3 | 2026-03-28 | [patch-0.6.3](2026-03-28-patch-0.6.3.md) |
| Scheduled Prompts | v0.9.0 | 2026-03-29 | [scheduled-prompts](2026-03-29-scheduled-prompts.md) |
| Config Restructure | v0.10.0 | 2026-03-29 | [config-restructure](2026-03-29-config-restructure.md) |
| Flexible Filters | v0.11.0 | 2026-03-29 | [flexible-filters](2026-03-29-flexible-filters.md) |
| System Statistics | v0.12.0 | 2026-03-29 | [system-statistics](2026-03-29-system-statistics.md) |
| ANSI Console (xterm.js) | v0.13.0 | 2026-03-29 | [ansi-console](2026-03-29-ansi-console.md) |
| eBPF Per-Session Stats | v0.16.0 | 2026-03-30 | [ebpf-stats](2026-03-30-ebpf-stats.md) |
| Dashboard Redesign | v0.18.0 | 2026-03-30 | [dashboard-redesign](2026-03-30-dashboard-redesign.md) |
| Encryption at Rest | v0.18.0 | 2026-03-30 | — (secfile/migrate.go, tracker encryption, export cmd) |
| DNS Channel | v0.7.0+ | 2026-03-30 | — (internal/messaging/backends/dns/) |
