# Changelog

All notable changes to datawatch will be documented here.
Format based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).

## [Unreleased]

### Planned
- Native Go Signal backend (libsignal-ffi) — see `docs/plans/2026-03-29-libsignal.md`
- Container images and Helm chart
- IPv6 listener support
- Intelligence features — see `docs/plans/2026-04-06-intelligence.md`

## [2.0.1] - 2026-04-10

### Fixed
- **B28: Ollama test before enabling memory** — `POST /api/memory/test` performs full functional test (connect + embed + validate vector). Web UI "Test" button next to memory toggle. If test fails, toggle reverts with error message.
- **B29: Memory encryption docs** — `docs/encryption.md` now includes full Memory Content Encryption section (hybrid encryption, what's encrypted/visible, key management, configuration table).
- **Monitor tab scroll jump** — real-time stats updates now preserve scroll position (saves/restores `scrollTop` and `window.scrollY` around DOM rebuild).
- **Comms tab toggle switches** — Web Server and MCP Server settings now use proper toggle switches instead of On/Off buttons. All settings tabs use consistent toggle switch controls.

### Added
- **AGENT.md configuration accessibility rule** — every feature must have config in YAML, web UI, API, comm channel, MCP, and CLI. Must verify round-trip before marking complete.
- **`/api/memory/test` endpoint** — tests Ollama connectivity AND embedding capability (sends test phrase, validates non-zero vector response with dimension count).

## [2.0.0] - 2026-04-09

### Major Release — Memory Complete, Pipelines, Intelligence Infrastructure

This release completes the entire episodic memory system (25 BL items), adds session
chaining with DAG execution, quality gates, Ollama server monitoring, rich chat UI,
and conversation mining. **All memory backlog items (BL43-BL67) are now implemented.**

### Added — Session Chaining & Intelligence
- **F15: Session chaining** — `pipeline: task1 -> task2 -> task3` chains sessions in a DAG with parallel execution, dependency tracking, and cycle detection. Pipeline status/cancel commands. Fully integrated with session manager.
- **BL39: Cycle detection** — Kahn's algorithm validates DAG before execution, reports cycle path
- **BL28: Quality gates** — run test command before/after sessions, detect regressions vs preexisting failures, block on new failures. `CompareResults()` with REGRESSION/IMPROVED/STABLE classification
- **BL24: Task decomposition** — pipeline infrastructure supports LLM-driven decomposition (prompt tuning deferred)
- **BL25: Independent verification** — verification hook point in pipeline executor (prompt tuning deferred)

### Added — Monitoring & UI
- **BL71: Remote Ollama server stats** — real-time GPU/VRAM/model stats from Ollama API in Monitor dashboard. Models, running inference, disk usage. `/api/ollama/stats` endpoint. `ollama_stats` MCP tool
- **BL73: Rich chat UI** — markdown rendering (code blocks, bold, italic), typing indicator animation, streaming status, improved chat bubble styling for OpenWebUI/Ollama sessions

### Added — Documentation & Architecture
- **Root README** — complete rewrite with Memory & Intelligence section, updated architecture diagram (pipelines, quality gates, Ollama stats), 31+ MCP tools
- **Security** — gosec pre-release scan with `.gosec-exclude`, Slowloris protection (ReadHeaderTimeout)

### Tests
- 198 tests across 40 packages — all passing (new: 11 pipeline tests)

## [1.7.0] - 2026-04-09

### Added — Memory Tier 4 (9 features, all memory BL items complete)
- **BL47: Retention policies** — per-role TTL pruning (`PruneByRole`, `ApplyRetention`). Configurable `retention_session_days`, `retention_chunk_days`. Manual and learning memories kept forever by default.
- **BL51: Batch reindex** — `memories reindex` re-embeds all memories after model change. `Reindex()` method on retriever. MCP `memory_reindex` tool. Async execution with progress logging.
- **BL53: Learning quality scoring** — `SetScore()` on store for rating learnings 1-5. Score stored alongside summary.
- **BL49: Cross-project search** — `recall` already searches all projects via `RecallAll`. Documented as default behavior.
- **BL64: Cross-project tunnels** — `memories tunnels` shows rooms shared across multiple wings. `FindTunnels()` query groups by room with distinct wing count > 1.
- **BL59: Conversation mining** — `MineConversation()` ingests Claude JSONL, ChatGPT JSON, and generic JSON conversation exports. Normalizes, pairs user-assistant exchanges, stores as memories.
- **BL65: Claude Code save hook** — `hooks/datawatch_save_hook.sh` auto-saves to memory every N exchanges (default 15). Parses Claude Code transcript JSONL, extracts last exchanges, POSTs to `/api/test/message`.
- **BL66: Pre-compact hook** — `hooks/datawatch_precompact_hook.sh` saves topic summary before context window compression.
- **BL67: Mempalace import** — conversation mining supports generic JSON format compatible with mempalace exports.

### Fixed
- **Nil embedder crash** — `Remember()` now handles nil embedder gracefully (saves without vector) instead of panicking.

### Tests
- 187 tests across 39 packages — all passing (8 new Tier 4 tests)
- gosec scan: 185 findings (all expected, G104 excluded via `.gosec-exclude`)

## [1.6.1] - 2026-04-09

### Fixed
- **Memory config in API response** — `GET /api/config` now includes full `memory` and `proxy` sections with all fields. Previously these sections were missing from the manually-built response map, causing the web UI to show incorrect toggle states.
- **Memory toggle switches** — LLM tab now uses proper toggle switches (same as LLM backend checkboxes) instead of "On/Off" buttons. Boolean values correctly read from config (false no longer shows as "Off" when enabled).
- **Embedder host default** — shows the actual configured `ollama.host` value instead of a placeholder. Empty field means "using ollama.host" and displays the resolved value.
- **G112 Slowloris protection** — added `ReadHeaderTimeout` to all HTTP servers (main server: 10s, redirect server: 5s).

### Added
- **AGENT.md gosec rule** — pre-release security scan with `gosec ./...` required before every release. Documented expected suppressions (subprocess, SSRF, file inclusion) and must-fix categories.

## [1.6.0] - 2026-04-09

### Added — Memory Tier 3 (Enterprise & Integration)
- **BL61: MCP KG tools** — 5 new MCP tools: `kg_query`, `kg_add`, `kg_invalidate`, `kg_timeline`, `kg_stats`. Accessible via stdio (Claude Code, Cursor) and SSE (network LLMs).
- **BL54: KG REST API** — `GET /api/memory/kg/query?entity=`, `POST /api/memory/kg/add`, `POST /api/memory/kg/invalidate`, `GET /api/memory/kg/timeline?entity=`, `GET /api/memory/kg/stats`. Wing/room filter params on `/api/memory/list`.
- **`get_prompt` MCP tool** — get the last user prompt for a session (mirrors `copy_response` for prompts).

### Fixed
- **Memory config visibility** — `MemoryConfig` JSON serialization no longer uses `omitempty` on parent struct, so the entire memory section always appears in `/api/config` response. Toggle now correctly reflects enabled state.
- **Fallback chain in claude-code setup** — profiles and fallback chain field moved into the claude-code LLM backend config popup.

### Changed
- **Settings tab reorganization**:
  - **LLM tab**: LLM backends, Episodic Memory config, RTK, Detection/Output Filters, Saved Commands
  - **Comms tab**: Servers, Web Server, MCP Server, Proxy Resilience, Communication Config
  - **General tab**: Datawatch core, Auto-Update, Session, Notifications
- **Root README**: Updated architecture diagram with memory system, KG, response capture, 30 MCP tools. Updated feature list, doc index, Go version badge.
- **MCP tool count**: 30 tools (was 17) — session management (17) + memory (8) + KG (5)

### Tests
- 179 tests across 39 packages — all passing

## [1.5.2] - 2026-04-09

### Fixed
- **B27: Alerts now include user prompt** — alert body shows "Prompt: {what user asked}\n---\n{LLM response}" instead of just the response. All input paths (web UI, comm channel, MCP, direct tmux) capture the prompt via LastInput.
- **Memory stats show enabled/disabled** — Monitor card always shows memory status badge (enabled/disabled), encryption status with key fingerprint when encrypted. Previously only showed when enabled.

### Added
- **`prompt` command** — `prompt` or `prompt <id>` returns the last user input for a session. Works from all comm channels, API (`GET /api/sessions/prompt?id=`), and MCP (`get_prompt` tool).
- **Settings UI reorganization**:
  - Episodic Memory config moved from General → **LLM tab** (alongside RTK, detection filters)
  - Web Server, MCP Server, Proxy Resilience moved from General → **Comms tab** (after servers, before comm config)
  - Profiles & Fallback removed from General (belongs in claude-code backend setup)

### Changed
- Alert body format: `Prompt: {input}\n---\n{response}` when both are available

## [1.5.1] - 2026-04-09

### Added — Memory Encryption (BL68, BL70)
- **Hybrid content encryption**: XChaCha20-Poly1305 encrypts `content` and `summary` fields at rest while keeping embeddings and metadata (role, wing, room, timestamps) searchable. Enabled automatically when `--secure` mode is active or a `memory.key` file exists.
- **Key management**: `KeyManager` with Generate, Load, Fingerprint. Auto-detects key from `--secure` encKey or `{data_dir}/memory.key`. Key rotation via `RotateKey()` re-encrypts all content. Migration from plaintext via `MigrateToEncrypted()`.
- **Stats show encryption**: `memory_stats` and Monitor card display `encrypted: true/false` and `key_fingerprint`.
- **Config**: `memory.storage_mode` (summary/verbatim), `memory.entity_detection` toggle added to API config handler.

### Tests
- 179 tests across 39 packages (9 new encryption tests: roundtrip, wrong key, encrypted save/read/search, unencrypted preserved, key rotation, migration, fingerprint, key manager)

## [1.5.0] - 2026-04-09

### Added — Memory Tier 2 (5 features)
- **BL55: Spatial organization** — wing/room/hall columns for hierarchical memory structure. Auto-derive wing from project path, hall from role. `SearchFiltered()` with metadata filtering for +34pp retrieval improvement. `ListWings()`, `ListRooms()` taxonomy queries.
- **BL56: 4-layer wake-up stack** — L0 identity from `identity.txt`, L1 auto-generated critical facts from top learnings+manual, L2 topic-triggered room context, L3 deep search (existing recall). `WakeUpContext()` auto-loaded on session start alongside task-specific retrieval.
- **BL57: Temporal knowledge graph** — SQLite-backed entity-relationship triples with validity windows. `kg add/query/timeline/stats` commands from all comm channels. Point-in-time queries, invalidation support. Auto entity creation. `KnowledgeGraph` struct with full CRUD.
- **BL58: Verbatim storage mode** — `memory.storage_mode: verbatim` stores full prompt+response text instead of summaries. Higher retrieval accuracy at cost of storage.
- **BL60: Entity detection** — lightweight regex-based extraction of people (capitalized multi-word names), tools (Go/Docker/PostgreSQL/etc), and projects from text. `PopulateKG()` auto-adds detected entities to knowledge graph.

### Added — Plans & Governance
- **BL68-70: Memory encryption plan** — hybrid content encryption using XChaCha20-Poly1305. Key management (generate, rotate, import/export with key). Plan document created.
- **BL69: Splash screen enhancements** added to backlog
- **AGENT.md pre-release dependency audit** rule (72-hour stability window for upgrades)
- **Merged testing docs** — testing.md and testing-tracker.md combined into single organized document

### Tests
- 170 tests across 39 packages (12 new Tier 2 tests: spatial, KG, layers, entity detection)

## [1.4.0] - 2026-04-09

### Added — Memory Tier 1 (7 features)
- **BL63: Deduplication** — content hash (SHA-256) prevents storing identical memories. `Save()` returns existing ID on duplicate. New `content_hash` column with index.
- **BL62: Write-ahead log** — JSONL audit trail at `{data_dir}/memory-wal.jsonl` for all Save/Delete operations. `memories wal` command, `/api/memory/wal` endpoint, `memory_wal` MCP tool.
- **BL50: Embedding cache** — LRU cache wrapping the embedder avoids re-computing identical vectors. 1000 entry default, tracks hit rate in stats.
- **BL44: Auto-retrieve on session start** — when memory is enabled, new sessions automatically search for relevant past context and display it as a preamble in tmux. Filters to memories with >30% similarity.
- **BL52: Session output auto-index** — on session completion, output is chunked into ~500 word segments, embedded, and stored for granular semantic search via `recall`.
- **BL48: Memory browser enhancements** — role filter (manual/session/learning/chunk), date range filter (7d/30d/90d), export button. API: `/api/memory/list` supports `role`, `since`, `project` query params.
- **BL46: Export/import** — `GET /api/memory/export` downloads JSON backup, `POST /api/memory/import` restores. Dedup-aware import skips existing memories. WAL logs import operations.

### Added — API & MCP
- `/api/memory/export` GET — download all memories as JSON
- `/api/memory/import` POST — upload JSON backup, dedup-aware
- `/api/memory/wal` GET — view write-ahead log entries
- `/api/memory/list` supports `role`, `since`, `project` filter params
- `ListFiltered()`, `Export()`, `Import()`, `WALRecent()` on MemoryAPI and MemoryMCP interfaces

### Tests
- 158 tests across 39 packages (10 new memory Tier 1 tests)

## [1.3.1] - 2026-04-09

### Added
- **Memory statistics in Monitor tab**: real-time memory metrics card (total, manual, session, learning, chunk counts, DB size) in the Monitor dashboard with live WS updates
- **Memory browser in Monitor tab**: memory browser with search, list, and delete in the Monitor tab under Session Statistics. Memory stats card in Monitor dashboard with real-time updates
- **Memory REST API**: `GET /api/memory/stats`, `GET /api/memory/list`, `GET /api/memory/search?q=`, `POST /api/memory/delete` endpoints
- **MCP memory tools**: `memory_remember`, `memory_recall`, `memory_list`, `memory_forget`, `memory_stats`, `copy_response` — 6 new MCP tools for IDE integration
- **Rich text copy output**: `copy` command uses markdown formatting (bold header + code block) for Slack, Discord, Telegram backends that support RichSender
- **Splash screen 24h throttle**: startup splash only shows once per 24 hours unless version changed; shows "Updated" badge on new version
- **Ctrl-b tmux prefix**: system saved command for tmux prefix key in both session detail and card quick commands
- **AGENT.md monitoring rule**: all new features must include stats metrics, API endpoint, MCP tool, web UI card, comm channel access, and Prometheus metrics

### Changed
- Memory stats callback wired into stats collector for real-time broadcasting
- Remote alert bundler prefers captured response over screen scraping for all backends
- Memory browser and stats card in Monitor tab (stats card only visible when memory enabled)

## [1.3.0] - 2026-04-09

### Added
- **Episodic memory system** (BL23/BL32/BL36): vector-indexed project knowledge with semantic search. Pure-Go SQLite backend (no cgo, no root), Ollama or OpenAI embeddings, configurable via YAML/web UI/API/comm channels. Enterprise PostgreSQL+pgvector backend option. New `internal/memory` package with store, embeddings, retriever, and chunker.
- **Memory commands**: `remember:`, `recall:`, `memories`, `forget`, `learnings` — accessible from Signal, Telegram, Slack, Discord, web UI, and all comm channels
- **Memory settings card**: Settings -> General -> Episodic Memory with backend selector (SQLite/PostgreSQL), embedder selector (Ollama/OpenAI), model, host, top-K, retention, auto-save, learnings toggles
- **Response capture system**: captures LLM's last response on every running->waiting_input transition from `/tmp/claude/response.md` (Claude Code) or tmux fallback. Stored on session, broadcast via WS, used in alerts and memory
- **`copy` command**: `copy` or `copy <id>` returns last LLM response from any comm channel or web UI
- **Response viewer modal**: clickable response icon on session cards and session detail opens scrollable modal with rich-formatted response content, copy-to-clipboard button
- **OpenWebUI chat UI** (B26): structured chat bubbles for OpenWebUI interactive sessions with WS streaming. New `chat` output mode, `chat_message` WS type, CSS chat bubble styles
- **Proxy mode phases 4-5** (F16): PWA reverse proxy (`/remote/{server}/`), HTTP client pool with circuit breaker, offline command queue with auto-replay, `/api/servers/health` endpoint, ProxyConfig UI
- **Memory documentation**: `docs/memory.md` with architecture, flow diagrams, configuration, usage
- **13 new memory backlog items** (BL43-BL54): spatial organization, wake-up stack, knowledge graph, verbatim storage, mining, entity detection, MCP tools, WAL, deduplication, tunnels, hooks
- **13 new mempalace-inspired backlog items** (BL55-BL67): import, cross-project search, embedding cache, batch reindex

### Fixed
- **Alert body uses response content**: alerts now show the actual LLM response instead of raw screen-scraped terminal output with ANSI artifacts
- **Terminal scroll mess on new commands**: `\x1b[3J` clears scrollback buffer on each pane capture frame, preventing scroll accumulation
- **Session exit flashes shell prompt**: pane capture display frozen once session state is complete/failed/killed; frames containing `DATAWATCH_COMPLETE:` suppressed
- **Channel ready re-render resets terminal**: `handleChannelReadyEvent` now dismisses banner in-place without full `renderSessionDetail` re-render

### Changed
- OpenWebUI interactive sessions default to `output_mode: chat` instead of `terminal`
- `modernc.org/sqlite` added as pure-Go dependency (no cgo required)

## [1.2.2] - 2026-04-02

### Fixed
- **Session restart resumes in-place**: restarting a completed/failed/killed session now reuses the same session ID, tmux session, and tracking directory instead of creating a duplicate entry. New `POST /api/sessions/restart` endpoint and `Manager.Restart()` method handle the full lifecycle (kill tmux, reset state, relaunch with resume).
- **Session resume "No conversation found"**: `Launch()` now sets `--session-id` with a deterministic UUID derived from the datawatch session ID, so `--resume` can find the conversation later. Previously Claude generated a random UUID that the resume logic couldn't predict.
- **Backend loading blocks page**: `/api/backends` now always returns cached data immediately when the cache is stale, refreshing version checks in the background. Previously a stale cache (>5 min) blocked the response while running `--version` on every backend binary.
- **"Needs input" notification spam**: added deduplication checks to the capture-pane and idle-timeout prompt detection paths. Previously these paths fired `onNeedsInput` every 2 seconds for the same prompt, spamming Signal/Telegram/WebSocket notifications. The structured channel path already had the check — now all three paths are consistent.

### Added
- **`POST /api/sessions/restart` endpoint**: restarts a terminal-state session in-place with LLM conversation resume support
- **Claude-code backend tests**: unit tests for `deriveSessionUUID` determinism, `isUUID` validation, and launch/resume session ID consistency

## [1.2.1] - 2026-04-02

### Fixed
- **Session detail loading splash**: when opening a session, the terminal area now shows a "Connecting to session…" splash with the datawatch logo while waiting for the first terminal capture. Previously the terminal was blank/black during the delay. Includes retry logic (re-subscribes after 5s, up to 3 attempts) and error indicator with manual retry and dismiss buttons if connection fails.

## [1.2.0] - 2026-04-02

### Added
- **Voice input via Whisper transcription**: voice messages sent via Telegram or Signal are automatically transcribed to text and routed as commands. Uses OpenAI Whisper from a Python venv (CPU-only). Configurable model size (tiny/base/small/medium/large) and language (99 languages supported via ISO 639-1 codes, or `auto` for detection). Per-user language preferences deferred to multi-user access control feature.
- **Whisper settings card in web UI**: Settings → Voice Input (Whisper) with model dropdown, language field, venv path, and enable toggle
- **Whisper config in REST API**: `GET /api/config` includes `whisper` section; `PUT /api/config` supports `whisper.enabled`, `whisper.model`, `whisper.language`, `whisper.venv_path`
- **`messaging.Attachment` type**: messages from backends can now carry file attachments with MIME type and local path
- **Telegram voice/audio download**: Telegram backend detects `Voice` and `Audio` messages, downloads via Bot API, and attaches to the message for transcription
- **Signal attachment parsing**: Signal backend now parses `attachments` from signal-cli envelopes and propagates them through the messaging pipeline
- **Transcription echo**: when a voice message is transcribed, the router echoes `Voice: <text>` back to the channel before processing the command
- **RTK Token Savings card in Monitor**: stats dashboard now renders RTK version, hooks status, tokens saved, average savings %, and command count when RTK is installed
- **`POST /api/test/message` endpoint**: simulates incoming messaging backend messages through the router, returning the responses that would be sent back. Enables testing all comm channel commands (help, list, version, stats, new, send, kill, configure, schedule, alerts) without actual Signal/Telegram connections
- **Proxy mode**: single datawatch instance relays commands and session output to/from multiple remote instances. Enables multi-machine management from one Signal/Telegram group or one PWA.
  - **WebSocket proxy relay**: `/api/proxy/{server}/ws` — bidirectional WS relay between browser and remote instance with token injection
  - **Aggregated sessions**: `GET /api/sessions/aggregated` — parallel fetch from all remotes + local, sessions tagged with `server` field
  - **Remote command routing**: `status`, `send`, `kill`, `tail` auto-fallback to remote when session not found locally
  - **`new: @server: task` syntax**: route session creation to a specific remote server from any comm channel
  - **Aggregated `list` command**: shows sessions from all configured servers in one response
  - **Server badges**: session cards in web UI show server name when viewing aggregated sessions
  - **Server picker**: selecting a remote server in Settings reconnects WS and routes all API calls through proxy
  - **Web-only daemon keepalive**: daemon stays alive with just HTTP server (no messaging backends required) for proxy-only deployments
- **`proxy` package** (`internal/proxy`): `RemoteDispatcher` with session discovery cache (30s TTL), `ForwardCommand`, `ListAllSessions`, `ForwardHTTP`, token auth injection

### Fixed
- **MCP channel reconnect delay**: navigating to an already-established claude session showed "Waiting for MCP channel…" for a long time. Root cause: the web UI did not populate its `channelReady` cache from session data on initial WS sync. Fixed with three sync points: initial sessions message, session state updates, and direct session object check in the detail view
- **Configure command broken on all comm channels**: the `configure` chat command used `http.Post` to call `/api/config`, but that endpoint only accepts PUT. Changed to `http.NewRequest(PUT)`. This affected Signal, Telegram, Discord, Slack, and all other messaging backends
- **`server.Version` hardcoded**: the HTTP server's `/api/health` and `/api/info` endpoints always returned version `"1.0.0"` instead of the actual build version. Fixed by wiring `server.Version = Version` from main.go

### Documentation
- **messaging-backends.md**: added Voice Input section with setup, model guide, supported languages; added feature parity matrix (threading, markdown, buttons, file upload, voice by backend)
- **config-reference.yaml**: added `whisper` config section with all fields documented
- **setup.md**: full Whisper setup section (venv, ffmpeg, config, model guide, 99 languages); full RTK setup section; profiles and fallback chains section; encryption clarified — can enable at any time, not just at init
- **operations.md**: `/healthz` + `/readyz` Kubernetes probe docs; Prometheus `/metrics` endpoint with metric table; profiles/fallback section; voice input section; test message endpoint documentation

## [1.0.2] - 2026-04-01

### Added
- **OpenWebUI interactive mode**: replaced curl/python3 single-shot backend with a native Go conversation manager. Maintains message history for multi-turn follow-ups, streams SSE responses directly, and routes input through the Go HTTP client instead of tmux send-keys. No external dependencies (curl, python3) needed.
- **RTK integration**: detects RTK installation, shows version/hooks status in stats API, collects token savings metrics (total_saved, avg_savings_pct, total_commands). Auto-runs `rtk init -g` if hooks not installed and `auto_init: true`. Only activates for RTK-supported LLM backends (claude-code, gemini, aider).
- **Channel feature parity review**: comprehensive audit of all 11 communication backends. Identified gaps in threading, rich formatting, interactive components, and file handling. Prioritized plan created.
- **Threaded conversations**: session alerts are now threaded per-session on Slack (via `thread_ts`), Discord (via `MessageThreadStart`), and Telegram (via `reply_to_message_id`). Thread IDs stored on Session for follow-up replies. Backends without threading fall back to flat messages.
- **Rich markdown formatting**: `RichSender` interface for platforms supporting formatted text. Alert headers in bold, tasks in italics, output context in code blocks. Implemented for Slack (mrkdwn), Discord (native markdown), and Telegram (Markdown parse_mode).
- **RTK setup wizard**: `datawatch setup rtk` CLI command — detects RTK, installs hooks, enables integration. `--disable` flag to turn off.
- **Schedule management improvements**: edit/delete buttons on all scheduled events, multi-select with checkboxes + bulk delete, "on input" time parsing fixed, improved preset buttons (5/15/30 min, 1/2 hr, "On next prompt")
- **Compact LLM filter badges**: short backend names with session count badges, replacing full-name buttons that overflowed horizontally
- **Interactive buttons on alerts**: Slack Block Kit buttons ([Approve] [Reject] [Enter]) and Discord component buttons on waiting_input alerts. `ButtonSender` interface. Falls back to text-only for backends without button support.
- **File upload on completion**: session output log uploaded to Slack/Discord thread when session completes. `FileSender` interface.
- **Profile CRUD API**: `/api/profiles` GET/POST/DELETE endpoints. Profile dropdown in New Session form. Profile field in session start API.
- **Multi-profile fallback chains**: named profiles with different accounts/API keys per backend. `session.fallback_chain` config auto-switches to the next profile on rate limit. Profile env vars applied to tmux session on launch. Configurable via YAML, web UI (Settings → Profiles & Fallback), and REST API.
- **`channel_ready` + `channel_port` session fields**: set automatically when the per-session MCP channel calls `/api/channel/ready`; exposed in REST API `/api/sessions`. Used for debounced state detection and per-session channel routing
- **Prompt context capture**: prompt alerts now include up to 10 surrounding screen lines (`prompt_context` field on Session) giving meaningful context about what is being asked, instead of a single matched line or noisy fallback
- **Alert title format**: all alert titles now use `hostname: name [id]: event` (e.g. `ralfthewise: myproject [a1b2]: running → waiting_input`) for consistent identification across local and remote channels
- **Rate-limit auto-continue**: when a rate limit is detected with a reset time, datawatch creates a persisted scheduled command to auto-resume the session after the limit resets — survives daemon restarts (replaces previous in-memory timer that was lost on reboot)
- **`ScheduleStore.CancelBySession`**: new method to cancel all pending scheduled commands for a session; called automatically on session kill and delete to prevent orphan entries
- **Channel tab bidirectional**: channel tab now shows outgoing sends (blue `→`), incoming replies (amber `←`), and notifications (purple `⚡`) — previously only showed Claude's rare `reply` tool calls
- **Per-session channel port routing**: `/api/channel/send` uses the session's actual channel port (stored on channel_ready) instead of a global fallback, enabling correct routing to per-session MCP servers on random ports
- **Quick command dropdown redesign**: commands dropdown now has System/Saved `<optgroup>` sections with visual divider and a "Custom..." option that reveals an inline text input for freeform prompts. Hardcoded quick buttons (y/n/Enter/Up/Down/Esc) removed from both session list and session detail — all consolidated into the dropdown

### Changed
- **Toast notifications**: title-only (truncated to 60 chars), no body text in toasts — full details remain in the Alerts view
- **Toast styling**: smaller font (10px), right-aligned text, subtle left accent border colored by level, lower-contrast background
- **MCP connection banner**: simplified to "Waiting for MCP channel…" — removed the prompt-specific "Accept prompt below to continue" text that was incorrect on reconnect/refresh

### Fixed
- **Spurious alerts on web connect/refresh**: `StartScreenCapture` now skips state detection on the first tick (baseline capture only), eliminating the flood of prompt-detection alerts when opening or refreshing a session in the web UI
- **Claude state detection accuracy**: active-processing check now scans lines above the `❯` prompt for spinner (`✢ Verb…`) and tool execution (`⎿ Running…`) indicators, skipping separators and status bars. Removed "esc to interrupt" from `activeIndicators` (it's Claude's permanent status bar, not an active-work signal). For `channel_ready` sessions, prompt must persist for 3 seconds (15 captures) before transitioning to `waiting_input`, preventing false triggers between tool calls
- **Prompt context noise**: `extractPromptContext` filters separator lines, shell launch commands, Claude startup warnings, spinners, and status bar fragments from the context shown in alerts
- **Rate-limit resume lost on reboot**: replaced in-memory `time.After` goroutine with persisted `ScheduleStore` entry — sessions in `rate_limited` state now auto-resume even after daemon restart
- **Orphan scheduled commands on session delete**: `Manager.Kill` and `Manager.Delete` now call `CancelBySession` to clean up pending schedules
- **Session delete data cleanup**: `Delete()` now cleans `mcpRetryCounts` and `rawInputBuf` maps; falls back to `sess.TrackingDir` when in-memory tracker is unavailable
- **`SendInput` from `rate_limited` state**: now clears `RateLimitResetAt` when transitioning to running
- **Web UI session detail lost on daemon restart**: `ws.onopen` handler now re-renders session-detail view when active, re-sending the `subscribe` message and restoring xterm.js, screen capture, and saved commands
- **False session completion on startup**: capture-pane completion detection used `Contains` which matched `DATAWATCH_COMPLETE` in the shell command echo; changed to `HasPrefix` per-line to only match when the pattern is at line start — fixes sessions being falsely marked complete immediately after creation
- **WS send-on-closed-channel panic**: protected `c.send` channel writes in subscribe handler with `select`/`default` to prevent panic when WS client disconnects during subscribe processing
- **Session naming for Claude**: `--name <session-name>` is now passed to `claude` CLI when the datawatch session has a name, tagging the Claude conversation for visibility in `/resume`
- **MCP channel port race**: channel.js now awaits `httpServer.listen()` before reporting port to datawatch, ensuring the actual random port is sent instead of the fallback. Stale MCP registrations from deleted sessions are cleaned up on daemon startup
- **Browser auto-refresh on daemon update**: daemon version is included in WS `sessions` message; client auto-reloads when version changes after reconnect
- **Completion summary for comm channels**: remote channel alerts now include context lines for completion/failed/killed events (2x the configured alert_context_lines), not just waiting_input events
- **Claude spinner detection range**: increased from 3 to 8 content lines above prompt to account for task list display between spinner and prompt

### Documentation
- **setup.md**: added messaging backends table with all 10+ channels and `datawatch setup` commands; restructured Step 3 with interactive wizard as primary option
- **operations.md**: added Configuration Methods table (YAML, CLI wizard, web UI, API, chat); added click-to-type terminal note; added tmux web terminal known issues
- **llm-backends.md**: added CLI/web setup options to backend selection; fixed opencode section to point to ACP mode for interactive use
- **messaging-backends.md**: added note about all configuration methods
- **encryption.md**: added "Enable at Any Time" and "Daemon/Background Mode" sections
- **claude-channel.md**: added "State detection: console vs channel" section documenting the `channel_ready` behavior

## [1.0.1] - 2026-03-31

### Added
- **Alert context lines**: prompt alerts now include the last N non-empty terminal output lines (default 10) instead of just the prompt line, giving full context when responding from messaging apps
- **`session.alert_context_lines`**: new config field to control how many non-empty lines are included in prompt alerts — configurable via YAML, web UI settings, and REST API
- **18 backlog items**: future feature ideas added to `docs/plans/README.md` (session chaining, cost tracking, multi-user ACL, Prometheus metrics, voice input, and more)

## [1.0.0] - 2026-03-31

### Major Release
First stable release. All 7 LLM backends tested end-to-end, terminal rendering stable, communication channel integration complete.

### Added — Per-LLM Configuration Split
- **Separate config sections** for opencode, opencode_acp, opencode_prompt — each with own enabled, binary, console size, output_mode, input_mode
- **output_mode** per backend: `terminal` (xterm.js capture-pane) or `log` (formatted log viewer for ACP/headless)
- **input_mode** per backend: `tmux` (input bar + quick buttons) or `none` (TUI handles own input)
- **Config API**: full GET/PUT support for all per-backend fields including output_mode, input_mode
- **Web UI**: LLM config popup renders output_mode/input_mode as dropdown selects

### Added — Terminal Rendering
- **Flicker-free display**: cursor-home overwrite (ESC[2J+ESC[H) instead of term.reset() — no visible flash
- **Single display source**: only pane_capture writes to xterm.js; file monitor raw output disabled for terminal-mode
- **CapturePaneVisible**: captures visible pane only (no scrollback) for live display
- **Hidden scrollbar**: CSS scrollbar-width:none on xterm-viewport
- **Log viewer**: ACP sessions show formatted color-coded event stream instead of xterm

### Added — Session Management
- **Batch delete**: select mode with checkboxes on inactive sessions, Select All / Delete buttons
- **Shell PS1**: `datawatch:\w$` prompt for reliable detection
- **opencode exit detection**: shell prompt (`$`) after opencode TUI exits triggers session complete
- **Ollama interactive**: starts in chat mode, detects `>>>` prompt for follow-ups
- **opencode-prompt JSON output**: `--format json` with Python formatter for visible response text

### Added — CLI Feature Parity
- **`datawatch stats`**: full system statistics (CPU, memory, disk, GPU, sessions, channels, eBPF)
- **`datawatch alerts`**: list alerts with `--mark-read <id>` and `--mark-all-read`

### Added — Communication Channels
- **Alert bundling**: per-session goroutine collects events for 5s quiet time, sends one bundled message to remote channels. Web/local alerts fire immediately.
- **Saved command expansion**: `!approve` or `/enter` from messaging channels expands from saved command library
- **Input logging**: all input paths tracked (terminal typing, quick buttons, saved commands, input bar)
- **Prompt acceptance logging**: shows what prompt was accepted when user presses Enter
- **Alert format**: `Name [id]: event` with prompt text and quick reply hints only on final waiting_input

### Added — UI Polish
- **Watermark**: datawatch eye logo on sessions tab (centered, 85% width, 4.5% opacity)
- **Back button**: CSS-drawn iOS-style chevron, 44x44px mobile touch target
- **Toast notifications**: narrow (210px), right-justified within app border
- **Settings restructured**: General tab has Datawatch, Auto-Update, Web Server, MCP Server, Session, Notifications cards
- **Detection filter defaults**: shows built-in patterns (greyed) when no custom set
- **Daemon log viewer**: pageable, color-coded in Monitor tab

### Fixed
- Terminal garbled display from dual output sources (file monitor + capture-pane)
- Completion false positive from command echo (HasPrefix instead of Contains)
- Prompt detection: suffix matching without trailing space, `matchPromptInLines(10)`
- View persistence across browser refresh
- Comms server connection status (live WS updates)
- Session start alert restored (SetOnSessionStart was overwritten)
- Saved command `\n` sends Enter correctly (normalized to empty string)

### Security
- **Prompt logging documentation**: operations.md warns never to type passwords in AI prompts

## [0.18.1] - 2026-03-30

### Changed
- **Docs:** Removed all references to legacy `enc.salt` file — salt is embedded in config header since v0.7.2
- **Docs:** Updated encryption docs to reflect XChaCha20-Poly1305 (v2) as primary cipher, AES-256-GCM (v1) as legacy read-only
- **Docs:** Updated setup.md with auto-migration, FIFO streaming, env variable support
- **Docs:** Fixed encryption table to show session.json (tracking) as encrypted, daemon.log as plaintext by design

### Fixed
- **Cross-compile:** eBPF stub types in `ebpf_other.go` for darwin/windows builds (was in v0.18.0 tag but noting here)

## [0.18.0] - 2026-03-30

### Completed Plans
- **Dashboard Redesign** — expandable sessions, channel stats, LLM analytics, progress bars, donut charts, eBPF status, infrastructure card, daemon log viewer
- **Encryption at Rest** — plaintext→encrypted migration, tracker file encryption, FIFO log encryption, export command. 4 unit tests.
- **DNS Channel** — HMAC-SHA256 protocol, nonce replay protection, server/client modes, setup wizard. 13 tests.

### Added
- `internal/secfile/migrate.go` + `migrate_test.go` — 4 tests (log_only, full, skip-encrypted, empty-dir)
- `internal/stats/channels.go` — `ChannelTracker` with atomic per-channel counters
- Per-channel message tracking for all 13 messaging backends
- MCP tool call tracking (17 handlers wrapped), Web/PWA broadcast tracking
- LLM backend stats: total/active sessions, avg duration, avg prompts; active badge
- Expandable Chat Channels + LLM Backends in Monitor, sorted alphabetically
- Per-process network via eBPF, Infrastructure card, Session short UID `(#abcd)`
- Daemon log viewer in Monitor — pageable, color-coded, `/api/logs` endpoint, auto-refresh 10s
- Settings/General restructured: Datawatch, Auto-Update, Web Server, MCP Server, Session, Notifications cards
- Settings/Comms: Authentication card (browser + server + MCP tokens)
- Detection filter defaults shown in LLM tab when no custom patterns set
- Config defaults: `log_level=info`, `root_path=CWD`, console size placeholders
- `docs/plans/README.md` — consolidated project tracker (bugs, plans, backlog)
- `docs/testing.md` — merged test document (37 tests, all PASS)

### Changed
- `docs/testing.md` replaces `bug-testing.md` + `bug-test-plan.md`
- `docs/plans/README.md` replaces `BACKLOG.md`
- Interface binding: warn-only, connected detection, localhost forced on

### Fixed
- View persistence, comms server status, session network display, session sort, binary install path

## [0.17.4] - 2026-03-30

### Added
- **LLM backend active session badge** — each LLM backend row in Monitor shows a green badge with active session count
- **LLM backend total count** — collapsed LLM row shows "N total" for quick reference

### Changed
- **Testing docs merged** — `bug-testing.md` and `bug-test-plan.md` consolidated into single `docs/testing.md` with 28 test procedures and results
- **Interface binding** — no longer blocks save when connected interface not selected; warns instead. Connected interface shows "(connected)" badge. Tailscale hostname resolution for interface matching.
- **AGENT.md** — updated testing doc references to `docs/testing.md`

### Fixed
- **eBPF caps after build** — documented that `go build` can strip caps; build→setcap→start flow required

## [0.17.3] - 2026-03-30

### Added — Communication Channel Stats
- **Per-channel message tracking** — atomic counters for every messaging backend (Signal, Telegram, Discord, Slack, Matrix, Twilio, ntfy, Email, GitHub WH, Webhook, DNS)
- **ChannelTracker** (`internal/stats/channels.go`) — thread-safe per-channel counters: msg_sent, msg_recv, errors, bytes_in, bytes_out, last_activity
- **MCP tool call tracking** — every MCP tool invocation tracked with request/response size
- **Web/PWA broadcast tracking** — WS message sends and incoming WS messages tracked
- **LLM backend stats** — per-backend: total sessions, active sessions, avg duration, avg prompts/session
- **Session InputCount** — tracks prompts sent per session for LLM analytics
- **Expandable Communication Channels** in Monitor dashboard — split into Chat Channels and LLM Backends sections, sorted alphabetically
- **Detailed channel stats** — expand any channel to see: endpoint, requests in/out, data in/out, errors, last activity, connections (for infra channels), or session stats (for LLM backends)
- **Per-process network stats** — when eBPF active, Network card shows datawatch process traffic only (via ReadPIDTreeBytes)
- **Infrastructure card** — shows Web server URL/port, MCP SSE endpoint, TLS status, tmux session count
- **Session short UID** — monitor sessions list shows `(#abcd)` next to session name
- **Hub.ClientCount()** — WebSocket client count exposed for Web/PWA connection stats

### Fixed
- **View persistence** — page refresh now correctly restores saved view/tab instead of always resetting to sessions
- **Comms server status** — connection indicator updates dynamically on WS connect/disconnect
- **Session network display** — expanded session view uses plain text for network counts instead of bars
- **Daemon network removed** — daemon row no longer shows misleading system-wide network totals
- **Session sort** — monitor session list sorted alphabetically by name
- **Binary install path** — builds go to `~/.local/bin/datawatch`, no stale binaries in repo

## [0.17.2] - 2026-03-30

### Added — Dashboard Improvements
- **Daemon stats per-line** — Memory, Goroutines, File descriptors, Uptime each on own line
- **Network card per-line** — Download and Upload on separate rows
- **Session donut** — shows active out of max_sessions (from config)
- **Sessions in store link** — clickable link navigates to sessions page with history enabled
- **Communication Channels card** — shows all 13 channels with enabled/disabled status

## [0.13.0] - 2026-03-29

### Added — ANSI Console (Plan 3)
- **xterm.js integration** — session output rendered in a real terminal emulator (xterm.js v5.5) with full ANSI/256-color support
- **Terminal theme** — dark theme matching datawatch UI (purple cursor, matching background/foreground colors)
- **Auto-fit** — terminal auto-resizes to container via FitAddon + ResizeObserver
- **5000-line scrollback** — configurable scrollback buffer with native xterm.js scroll
- **Fallback rendering** — gracefully degrades to plain-text div rendering if xterm.js fails to load
- **Terminal cleanup** — terminal disposed on navigation away from session detail

### Changed
- Output area CSS updated: removed padding for xterm.js, added min-height for container stability

## [0.12.0] - 2026-03-29

### Added — System Statistics (Plan 4)
- **Stats collector** — `internal/stats/collector.go` samples CPU load, memory, disk, GPU (nvidia-smi), process metrics every 5s
- **Ring buffer** — 720-entry in-memory ring (1 hour history at 5s intervals), no persistence
- **`GET /api/stats`** — returns latest system snapshot (CPU, memory, disk, GPU, goroutines, sessions)
- **`GET /api/stats?history=N`** — returns last N minutes of metric history for time-series display
- **GPU detection** — optional nvidia-smi integration (hidden when no GPU available)
- **Platform support** — Linux proc filesystem for CPU/memory/disk; stub for other platforms

## [0.11.0] - 2026-03-29

### Added — Flexible Detection Filters (Plan 2)
- **`DetectionConfig` struct** — configurable prompt, completion, rate-limit, and input-needed patterns in `detection` config section
- **`DefaultDetection()`** — built-in patterns extracted from previously hardcoded vars; serve as fallback when config patterns are empty
- **Per-LLM pattern merge** — `GetDetection(backend)` merges global patterns with per-LLM overrides
- **API support** — `detection.*` fields in GET/PUT config for all four pattern arrays
- **Manager uses config** — all pattern matching now reads from `m.detection` (set from config) with automatic fallback to hardcoded defaults

### Changed
- Hardcoded `promptPatterns`, `rateLimitPatterns`, `completionPatterns`, `inputNeededPatterns` in manager.go are now fallback-only; config takes priority

## [0.10.0] - 2026-03-29

### Added — Config Restructure (Plan 1)
- **Annotated config template** — `datawatch config generate` outputs fully commented YAML with all fields, defaults, and section documentation
- **TLS dual-interface** — new `server.tls_port` field; when set, main port stays HTTP (with redirect) and TLS runs on separate port
- **HTTP→HTTPS redirect** — when dual-interface TLS is enabled, HTTP requests auto-redirect to HTTPS port

### Fixed
- **TLS cert details in config** — all TLS fields (cert, key, auto_generate, tls_port) now exposed in config, API, and Web UI
- **TLS dual-port option** — enables running both HTTP and HTTPS simultaneously on different ports

## [0.9.0] - 2026-03-29

### Added — Scheduled Prompts (Plan 5)
- **Natural language time parser** — "in 30 minutes", "at 14:00", "tomorrow at 9am", "next wednesday at 2pm", raw durations ("2h30m"), absolute datetimes
- **Deferred session creation** — schedule a new session to start at a future time with task, backend, project dir, and name
- **Timer engine** — background goroutine checks due items every 30s; auto-sends commands, starts deferred sessions, processes waiting_input triggers
- **`/api/schedules` CRUD** — GET (filter by session/state), POST (natural language time), PUT (edit), DELETE (cancel)
- **Router upgrade** — `schedule` command now accepts natural language time; added `schedule list` to view all pending
- **Session detail schedules** — pending scheduled items shown inline in session view (non-obtrusive, cancellable)
- **Sessions page badge** — pending schedule count badge with dropdown showing all queued items
- **Settings schedule section** — collapsible, paginated list of all scheduled events (editable, cancellable)

## [0.8.3] - 2026-03-29

### Fixed
- **ACP status → instant state transitions** — `[opencode-acp] awaiting input/ready` immediately sets `waiting_input`; `processing/thinking` immediately sets `running` (no idle timeout wait)
- **Rate-limit auto-accept** — when Claude shows rate limit menu, automatically sends "1" to accept "Stop and wait for limit to reset" after 2s delay
- **Rate-limit state stability** — `rate_limited` state is sticky; output after accepting wait does not flicker state back to `running`
- **Session name in channel messages** — router notifications now include session name alongside ID (e.g. `[host][abc1 MyProject] State: running → waiting_input`)
- **"Restart now" conditional** — restart prompt only appears after changing fields that require restart (host, port, TLS, binds); other saves just show "Saved"
- **Settings in config file** — `auto_restart_on_config` and `suppress_active_toasts` moved from localStorage to `server` config section (persists across browsers/devices)
- **Config save notice** — all settings changes show "Saved" toast confirmation
- **Seed honors --secure** — `datawatch seed` now uses encrypted stores when config is encrypted
- **Orphaned tmux sessions** — cleaned 2 dead tmux sessions (6a11, ef31) with no store entries

### Added
- **Channel tab help** — expanded help popup with what you can send, Claude slash commands, and what LLM can send back
- **Config option tooltips** — settings toggles include title attributes explaining their purpose
- **`server.auto_restart_on_config`** — server-side config field (was localStorage-only)
- **`server.suppress_active_toasts`** — server-side config field (was localStorage-only)

## [0.8.2] - 2026-03-29

### Added
- **Full config exposure** — all config struct fields now accessible via API GET/PUT, Web UI settings, and CLI setup
- **`datawatch setup dns`** — interactive CLI wizard for DNS channel configuration (mode, domain, listen, secret, rate limit, TTL)
- **API fields added** — server.token/tls_*/channel_port, mcp.sse_enabled/token/tls_*, opencode.acp_*/prompt_enabled, auto_manage_* for Discord/Slack/Telegram/Matrix, dns.rate_limit, session.log_level, signal.config_dir/device_name
- **Web UI fields added** — server token/TLS/channel_port, MCP SSE enabled/token/TLS, session log level, opencode-acp timeouts, messaging auto_manage toggles, DNS rate_limit, Signal group_id/config_dir/device_name

## [0.8.1] - 2026-03-29

### Added
- **Interface multi-select** — bind host fields in settings now show a checkbox list of all system network interfaces (0.0.0.0 first, 127.0.0.1 second, then others). Supports binding to one or multiple interfaces.
- **`/api/interfaces`** — new API endpoint returning available IPv4 network interfaces
- **Multi-bind support** — HTTP server and MCP SSE server accept comma-separated host values to listen on multiple interfaces simultaneously

## [0.8.0] - 2026-03-29

### Added
- **DNS server hardening** — per-IP rate limiting (default 30/min), catch-all REFUSED handler for non-domain queries, uniform error responses to prevent oracle attacks, no-recursion flag
- **Security documentation** — comprehensive network security section in operations.md covering all listeners, firewall rules, Tailscale, TLS, and recommended deployment patterns
- **IPv6 backlog item** — planned for future dual-stack listener support

### Changed
- **Default bind interfaces** — MCP SSE server now defaults to `127.0.0.1` (was `0.0.0.0`); webhook listeners (GitHub, generic, Twilio) default to `127.0.0.1:900x` (were `:900x` on all interfaces)
- **DNS config** — added `rate_limit` field (queries per IP per minute, default 30)

### Security
- All non-DNS services should be bound to localhost or behind a VPN when DNS is public-facing
- DNS server returns identical REFUSED for all failure cases (bad HMAC, replay, wrong domain, non-TXT, rate exceeded) — no information leakage
- Non-datawatch DNS queries are refused silently via catch-all handler

## [0.7.8] - 2026-03-29

### Fixed
- **Bash/shell prompt detection** — partial-line drain for interactive shell prompts (no trailing newline); sessions now correctly transition to `waiting_input`
- **Resume ID field** — hidden for backends that don't support resume (only claude, opencode, opencode-acp show it)
- **Git checkboxes layout** — auto git init and auto git commit checkboxes now reliably on same line
- **Session detail badges** — all status badges (backend, mode, state, stop, restart) use consistent pill sizing
- **LLM type filter badges** — clickable backend-type badges between filter input and show history for quick filtering
- **DNS Channel documentation** — added to messaging-backends, backends table, architecture diagram, data-flow, operations, implementation, commands

### Added
- **`supports_resume`** API field on `/api/backends` response for per-backend resume capability detection
- **`encrypted`/`has_env_password`** fields on `/api/health` for encryption-aware auto-restart

## [0.7.7] - 2026-03-29

### Fixed
- **Session visibility** — recently completed sessions (within 5 min) now visible in active list, fixing bash/openwebui/ollama sessions disappearing instantly
- **Auto-restart on config save** — general config changes now trigger auto-restart when enabled; warns if encrypted without `DATAWATCH_SECURE_PASSWORD` env variable
- **Nav bar highlight** — active tab now has a top accent bar and subtle background for better visibility
- **Header gap** — reduced unnecessary spacing under header on all pages
- **Settings file browser** — default project dir and root path fields now use the directory browser instead of plain text input

## [0.7.6] - 2026-03-29

### Fixed
- **Claude `enabled` flag** — claude-code backend now respects the per-backend enabled flag correctly
- **ACP configurable timeouts** — opencode-acp startup, health check, and message timeouts now configurable via config
- **Alert cards** — alert detail view cards now render with correct formatting and timestamps

## [0.7.5] - 2026-03-29

### Fixed
- **False rate_limited detection** — code output containing "rate limit" text no longer triggers false rate limit state (200-char line length check)
- **SendInput for rate_limited** — sessions in `rate_limited` state can now receive input (auto-wait confirmation)
- **Prompt patterns** — 15+ new patterns for claude-code trust prompts, numbered menus, rate limit recovery
- **Restart prompt field** — resume prompt field correctly cleared between session starts
- **Stale prompt** — `last_prompt` cleared on state transition back to running
- **Rate limited badge** — CSS for `rate_limited` state badge in session cards

### Added
- **Session safety rule** — AGENT.md rule preventing automated tools from stopping user sessions
- **Backlog processing checklist** — AGENT.md rules for systematic bug triage

## [0.7.4] - 2026-03-29

### Fixed
- **MCP reconciler** — session reconciler correctly cancels stale monitors before resuming
- **MCP retry validation** — retry counter reset on successful channel connection
- **Session safety** — reconciler no longer accidentally kills sessions with active MCP channels

## [0.7.3] - 2026-03-29

### Added
- **`datawatch logs` CLI** — tail any log (session or daemon), encrypted or plaintext, with `-n` and `-f` support
- **README architecture diagram** — reflects all 10 LLM backends, 10 messaging backends, MCP, DNS, fsnotify
- **README docs index** — encryption, claude-channel, covert-channels, testing-tracker links added
- **AGENT.md rule** — architecture diagram and docs index must be updated when adding features

## [0.7.2] - 2026-03-29

### Changed
- **XChaCha20-Poly1305** — all encryption switched from AES-256-GCM (24-byte nonce, post-quantum safe)
- **Salt embedded in config** — no separate `enc.salt` file needed; salt extracted from DWATCH2 header
- Config format upgraded to `DWATCH2`, data stores to `DWDAT2`
- Backward compat: reads v1 (DWATCH1/DWDAT1, AES-256-GCM) transparently

## [0.7.1] - 2026-03-29

### Added
- **Encrypted log writer** — `DWLOG1` format, 4KB blocks, XChaCha20-Poly1305 per block
- **EncryptingFIFO** — named pipe for tmux pipe-pane → encrypted output
- **`datawatch export`** — decrypt and export config, logs, data stores (`--all`, `--export-config`, `--log`)
- **`DATAWATCH_SECURE_PASSWORD`** — env variable for non-interactive encryption
- **Auto-detect encrypted config** — no `--secure` flag needed if config has DWATCH header
- **Auto-encrypt migration** — plaintext config encrypted on first `--secure` start
- **Windows cross-compile** — FIFO stub for Windows builds

## [0.7.0] - 2026-03-29

### Added
- **DNS channel communication backend** — covert C2 via DNS TXT queries
  - Server mode: authoritative DNS server (miekg/dns, UDP+TCP)
  - Client mode: encode commands as DNS queries via resolver
  - HMAC-SHA256 authentication, nonce replay protection (bounded LRU)
  - Query format: `<nonce>.<hmac>.<b64-labels>.cmd.<domain>`
  - Response: fragmented TXT records with sequence indexing
  - 15 tests, 86% coverage
- **Session reconciler** — periodic (30s) check prevents false stopped/completed states
- **MCP banner dismiss** — X button to skip MCP and use tmux only

## [0.6.35] - 2026-03-29

### Fixed
- Independent opencode/opencode-acp/opencode-prompt enabled flags (were shared)
- Textarea full width matching session name input
- opencode-prompt: --print-logs for status output
- Hidden input bar for single-prompt sessions
- Auto git init before commit ordering
- OpenWebUI: PromptRequired + empty task validation
- All communication backend PUT fields fully wired

## [0.6.14] - 2026-03-29

### Added
- `list --active|--inactive|--all` command filters
- Alerts: HR separators, session ID labels
- Session cards: stop button styled, LLM backend name shown

## [0.6.13] - 2026-03-29

### Added
- **Interrupt-driven output monitoring** — replaced 50ms polling loop with `fsnotify` file watcher. Output lines are processed instantly on file write events with polling fallback if inotify is unavailable. Reduces CPU usage and latency.
- **Fast prompt detection** — prompt patterns matched on each output line trigger a 1-second debounce instead of the full `input_idle_timeout` (default 10s). Consent prompts detected in ~2-4 seconds.
- **Per-session MCP channel servers** — each claude-code session registers its own MCP server (`datawatch-{sessionID}`) with unique `CLAUDE_SESSION_ID` env var and random port (`DATAWATCH_CHANNEL_PORT=0`). Enables true multi-session channel support.
- **MCP auto-retry** — detects "MCP server failed" in output and sends `/mcp` + Enter to reconnect. Configurable limit: `mcp_max_retries` (default 5). Available in Settings.
- **Connection status banner** — channel/ACP sessions show a spinner banner until the MCP channel or ACP server confirms ready. Input is disabled until connected (but enabled if session needs input for consent prompts).
- **Channel ready WebSocket event** — `channel_ready` broadcast from `/api/channel/ready` dismisses the banner and enables input. Now fires for all sessions, not just those with tasks.
- **Channel tab visibility** — Channel tab and send button only appear when the channel is actually connected. Help icon (`?`) shows available channel commands.
- **Alert state tracking** — all state transitions (not just `waiting_input`) now create alerts in the alert store. Kill/fail states get `LevelWarn`.
- **Alerts UI tabs** — Active/Inactive tabs at top; active sessions have sub-tabs per session. Inactive sessions collapsed by default. System alerts under collapsible group.
- **Session list enhancements** — action icons (stop/restart/delete) inline on each card. Waiting-input indicator with saved commands popup. Entire card clickable. Drag handle changed to familiar vertical dots.
- **Quick input buttons** — added Up arrow, Down arrow, and Escape to the quick-input row (y/n/Enter/Up/Down/Esc/Ctrl-C).
- **`sendkey` command** — sends raw tmux key names (Up/Down/Escape) without appending Enter.
- **General Configuration card** — new collapsible Settings section with toggle switches and inline inputs for session, server, MCP, and auto-update config fields.
- **LLM backend activation** — "Activate" button per backend in Settings LLM section. Version cache pre-warmed at startup and invalidated on config change.
- **PID lock** — prevents multiple daemon instances. Check in `daemonize()` before spawning child; foreground path skips own PID.
- **OpenCode binary detection** — resolves binary from `~/.opencode/bin/`, `~/.local/bin/`, `/usr/local/bin/` when not in PATH.
- **Channel MCP npm auto-install** — `EnsureExtracted` writes `package.json` and runs `npm install` (finds npm via PATH, nvm, corepack shim).
- **`--surface` CSS variable** — defined in `:root` (was undefined, causing transparent popup backgrounds).
- **PII masking in `datawatch test`** — email, Matrix IDs, URLs masked in test output.
- **AGENT.md rules** — work tracking checklist, decision-making rule, configuration rule, release discipline.

### Changed
- **CLAUDE.md → AGENT.md** — non-claude sessions get `AGENT.md` instead of `SESSION.md`. CLAUDE.md only created for `claude-code` backend. Skipped in project dir when `AGENT.md` already exists.
- **Signal backend resilience** — `call()` has 30-second timeout and `b.done` channel select to prevent permanent hang. Router `send()` runs async in goroutine (all messaging backends).
- **Toast position** — overlays the header bar (z-index 300, top: 0).
- **Output polling reduced** — fallback polling at 50ms (from 500ms) when fsnotify unavailable.
- **Install script** — tries GoReleaser tar.gz archives then falls back to raw binaries (`datawatch-linux-amd64` format).
- **Saved command Enter fix** — `sendSavedCmd` uses `send_input` with empty string for `\n` commands. JSON-escaped to prevent HTML attribute breakage.
- **Alert toast suppression** — toasts suppressed when actively viewing the session the alert belongs to.
- **Back navigation** — single press (removed double-back-press guard).
- **Alerts fetch fresh sessions** — alerts view fetches `/api/sessions` alongside alerts to ensure accurate active/inactive classification.
- **Global MCP removed** — per-session MCP servers replace the global `datawatch` registration. Stale globals cleaned up on startup.
- **`skip_permissions` default** — `true` for new installs.
- **OpenCode empty task** — launches interactive TUI instead of `opencode -p ''` (which showed help and exited).

### Fixed
- **PID lock self-block** — `daemonize()` writes child PID before child starts; child was finding its own PID and refusing to start. Fixed by checking in `daemonize()` before spawning.
- **MCP channel port conflict** — multiple sessions all tried port 7433. Per-session MCP servers now use random ports.
- **Channel ready not broadcast** — was gated on `sess.Task != ""`, preventing broadcast for sessions with no task.
- **Channel readiness lost on navigation** — `state.channelReady[sessionId]` caches readiness across view changes.
- **Saved command `\n` breaking HTML** — `JSON.stringify("\n")` produced literal newline in onclick attribute. Now HTML-escaped.
- **LLM backend list slow** — version checks ran sequentially (~5s). Now parallel with 60-second cache and startup pre-warm.

## [0.6.6] - 2026-03-28

### Fixed
- **Session output tabs** — when a session uses the channel backend (claude-code), the session detail view now shows two tabs: **Tmux** (raw terminal output) and **Channel** (structured channel replies). Tabs are hidden when the session only has tmux output. Live-appended lines now route to the correct area: tmux output → Tmux tab, channel replies → Channel tab.
- **Stop button stays after kill** — after stopping a session, the action buttons (Stop / Restart / Delete) and state badge now update immediately without requiring a manual refresh. The kill confirms optimistically then waits for the WebSocket `state_change` event to confirm.
- **`updateSessionDetailButtons`** — new helper that patches action buttons and state badge in-place; called from both `killSession` and `onSessionsUpdated` so WebSocket state changes also refresh the buttons.

## [0.6.5] - 2026-03-28

### Changed
- **Claude-specific config fields renamed** — `SkipPermissions` → `ClaudeSkipPermissions`, `ChannelEnabled` → `ClaudeChannelEnabled`, `ClaudeCodeBin` → `ClaudeBin` in the Go struct. YAML keys (`skip_permissions`, `channel_enabled`, `claude_code_bin`) are unchanged for backward compatibility with existing configs.
- **`Manager` struct field rename** — `claudeBin` → `llmBin`; `NewManager` parameter renamed accordingly. The legacy fallback path comment updated to reflect it works for any LLM binary, not just claude.
- **`WriteCLAUDEMD` → `WriteSessionGuardrails`** — method renamed and made backend-aware: claude-code sessions write `CLAUDE.md` (the file claude-code reads for project instructions); all other backends write `SESSION.md` instead. Project-directory write now only applies to claude-code sessions.
- **Template path updated** — session guardrails template looked up as `session-guardrails.md` first (with fallback to legacy `session-CLAUDE.md` name).
- **LLM backend fallback removed** — daemon startup no longer silently falls back to `claude-code` when `llm_backend` names an unregistered backend; instead returns an error with a hint to run `datawatch backend list`.
- **Manager doc comment** — "manages claude-code sessions" → "manages LLM coding sessions".

## [0.6.4] - 2026-03-28

### Added
- **MCP 1:1 feature parity** — MCP server now exposes 17 tools (up from 5), covering all major messaging commands:
  - `session_timeline` — structured event timeline (state changes, inputs with source attribution)
  - `rename_session` — set a human-readable session name
  - `stop_all_sessions` — kill all running/waiting sessions
  - `get_alerts` — list alerts, optionally filtered by session
  - `mark_alert_read` — mark alert(s) as read
  - `restart_daemon` — restart the daemon in-place (active sessions preserved)
  - `get_version` — current version + latest available check
  - `list_saved_commands` — view the saved command library
  - `send_saved_command` — send a named command (e.g. `approve`) to a session
  - `schedule_add` / `schedule_list` / `schedule_cancel` — full schedule management
- **Session audit source tracking** — `conversation.md` and `timeline.md` now record the source of each input (e.g. `(via signal)`, `(via web)`, `(via mcp)`, `(via filter)`, `(via schedule)`). All `SendInput` call sites pass origin context.
- **`/api/sessions/timeline` endpoint** — returns the structured timeline events for a session as JSON (`{session_id, lines[]}`).
- **`datawatch session timeline <id>` CLI command** — prints timeline events for a session; tries HTTP API first, falls back to reading `timeline.md` directly.
- **Session detail Timeline panel (Web UI)** — a ⏱ Timeline button in the session header loads and displays the structured timeline inline below the output area; click again to dismiss.
- **Alerts grouped by session (Web UI)** — alerts view now groups alerts under their session (with session name/state and a link to the session detail), instead of a flat list. Sessions in `waiting_input` state are highlighted with a ⚠ indicator.
- **Quick-reply command buttons in alerts (Web UI)** — when a session is in `waiting_input` state, its alert group shows saved commands (approve, reject, etc.) as one-click reply buttons.
- **Alert quick-reply hints in messaging** — when a `waiting_input` alert is broadcast to messaging backends, the message now appends `Quick reply: send <id>: <cmd>  options: approve | reject | ...` when saved commands exist.
- **`Router.SetCmdLibrary()`** — wires the saved command library into the router for alert quick-reply generation.

## [0.6.3] - 2026-03-28

### Added
- **`datawatch restart` command** — stops the running daemon (SIGTERM) and starts a fresh one; tmux sessions are preserved.
- **`POST /api/restart` endpoint** — web UI and API clients can trigger a daemon in-place restart via `syscall.Exec`; responds immediately, restarts after 500ms.
- **`restart` messaging command** — all backends (Signal, Telegram, Discord, Slack, Matrix, etc.) now accept `restart` to restart the daemon remotely.
- **Inline filter editing** — filter list rows now have a ✎ edit button; clicking expands an inline form to change pattern, action, and value. Backend: `PATCH /api/filters` now accepts `pattern`/`action`/`value` fields for full updates (not just enabled toggle).
- **Inline command editing** — command rows now have a ✎ edit button for in-place rename/value change. Backend: new `PUT /api/commands` endpoint accepts `{old_name, name, command}`.
- **Backend section UX redesign** — messaging backends in Settings now show "⚙ Configure" (unconfigured) or "✎ Edit" + "▶ Enable / ⏹ Disable" buttons instead of a misleading ▶/⏸ play-pause toggle. A "Restart now" link appears after any change.
- **`FilterStore.Update()`** — new method to replace pattern/action/value on an existing filter.
- **`CmdLibrary.Update()`** — new method to rename or change the command of an existing entry.

### Fixed
- **Settings page layout** — filter/command/backend rows now use consistent `.settings-list-row` classes with proper 16px left padding; no more content flush against the left edge. Delete buttons styled red for clarity.

## [0.6.2] - 2026-03-28

### Fixed
- **Web UI version mismatch** — Makefile ldflags now set both `main.Version` and `github.com/dmz006/datawatch/internal/server.Version` via a shared `LDFLAGS` variable. Previously only `main.Version` was set by ldflags; `server.Version` was hardcoded in source and could drift, causing `/api/health` to return a stale version string.

## [0.6.1] - 2026-03-28

### Added
- **`/api/update` endpoint** — `POST /api/update` downloads the latest prebuilt binary and restarts the daemon in-place via `syscall.Exec`. Web UI "Update" button now calls this endpoint instead of the check-only command.
- **Self-restart after CLI update** — `datawatch update` calls `syscall.Exec` after successful prebuilt binary install to restart the running daemon in-place.
- **Tmux send button in channel mode** — session detail input bar now shows both a "ch" (MCP channel) and a "tmux" send button when a session is in channel mode, making it easy to send to the terminal directly (e.g. for trust prompts during an active session).
- **opencode-acp: ACP vs tmux interaction table** — `docs/llm-backends.md` now has an explicit table documenting which paths go through ACP vs tmux for opencode-acp sessions.
- **CHANGELOG reconstructed** — v0.5.13 through v0.5.20 entries added (were missing).

### Fixed
- **`channel_enabled` defaults to `true`** — channel server is now self-contained (embedded in binary); enabling by default requires no extra setup.
- **Mobile keyboard popup on session open** — input field auto-focus is skipped on touch devices (`pointer:coarse`), preventing the soft keyboard from opening when navigating to a session.
- **Uninstalled backends selectable** — new session backend dropdown now marks unavailable backends as `disabled`; they show `(not installed)` but cannot be selected.
- **Android Chrome "permission denied"** — notification permission denied toast now includes actionable instructions (lock icon → Site settings → Notifications → Allow).
- **Backlog cleanup** — removed 8 completed items: docs opencode-ACP table, `channel_enabled` default, session auto-focus, uninstalled LLM filter, CHANGELOG missing entries, update+restart, Android notification, send channel/tmux toggle.

## [0.6.0] - 2026-03-28

### Added
- **`datawatch status` command** — top-level command showing daemon state (PID) and all active sessions in a table. Sessions in `waiting_input` state are highlighted `⚠`. Falls back to local session store if daemon API is unreachable.
- **opencode ACP channel replies** — `message.part.updated` SSE text events from opencode-acp sessions are broadcast as `channel_reply` WebSocket messages; the web UI renders them as amber channel-reply lines, matching claude MCP channel visual treatment.
- **Self-contained MCP channel server** — `channel/dist/index.js` embedded in the binary via `//go:embed`. On startup with `channel_enabled: true`, auto-extracted to `~/.datawatch/channel/channel.js` and registered with `claude mcp add --scope user`. No manual npm/mcp setup required.
- **`/api/channel/ready` endpoint** — channel server calls this after connecting to Claude; datawatch finds the active session's task and forwards it. Replaces broken log-line detection.
- **`apiFetch()` helper in app.js** — centralised fetch with auth header + JSON parse.
- **`make channel-build` target** — rebuild channel TypeScript and sync to embed path.
- **Planning Rules in AGENT.md** — large plans require `docs/plans/YYYY-MM-DD-<slug>.md` with date, version, scope, phases, and status.

### Fixed
- **Session create: name/task not shown ("no task")** — `submitNewSession()` now uses `POST /api/sessions/start` (REST) and navigates directly to the new session detail via the returned `full_id`, eliminating the 500 ms WS race.
- **Ollama "not active" for remote servers** — `Version()` probes `GET /api/tags` over HTTP when `host` is set, instead of running `ollama --version`. Remote Ollama servers without a local CLI binary now show as available.
- **Web UI version stuck at v0.5.8** — `var Version` in `internal/server/api.go` was not kept in sync with `main.go`; synced to current version.
- **Node.js check for channel mode** — `setupChannelMCP()` verifies `node` ≥18 in PATH before proceeding; emits actionable warning with install instructions and disable hint if missing.
- **Channel task not sent automatically** — removed broken `channelTaskSent` sync.Map output-handler; replaced by `/api/channel/ready` callback from channel server.
- **`/api/channel/ready` route missing** — handler existed in `api.go` but route was not registered in `server.go`.

### Internal
- `session.Manager.SetOnSessionStart` callback.
- `server.HTTPServer.BroadcastChannelReply` forwarding method.
- `opencode.OnChannelReply` package-level callback + `acpFullIDs` pending map.
- `opencode.SetACPFullID()` timing-safe association for ACP full IDs.

## [0.5.20] - 2026-03-28

### Added
- **`datawatch status` command** — shows daemon PID state and all active sessions; `WAITING INPUT ⚠` highlights sessions needing input.
- **opencode-acp channel replies** — SSE `message.part.updated` events broadcast as `channel_reply` WS messages for amber rendering in web UI.
- **AGENT.md planning rules** — large implementation plans must be saved to `docs/plans/YYYY-MM-DD-<slug>.md`.

### Fixed
- **`internal/server/api.go` version stuck at 0.5.8** — `var Version` was not updated alongside `main.go`; synced.
- **Backlog cleanup**: removed completed items (docs Node.js, agent rules, opencode ACP, status cmd, session name, ollama status, web UI version).

---

## [0.5.19] - 2026-03-28

### Added
- **Self-contained MCP channel server** — `channel/dist/index.js` embedded in binary via `//go:embed`; auto-extracted and registered on startup with `channel_enabled: true`.
- **`/api/channel/ready` endpoint** — channel server calls this after connecting to Claude; datawatch forwards the session task automatically.

### Fixed
- **Broken log-line detection** — removed `channelTaskSent` sync.Map and ANSI-poisoned log matching; replaced by `/api/channel/ready` callback.
- **`/api/channel/ready` route unregistered** — handler existed but was missing from `server.go` route table.

---

## [0.5.17] - 2026-03-28

### Added
- **Filter-based prompt detection** — `detect_prompt` filter action marks sessions as `waiting_input` immediately on pattern match, without idle timeout. Seeded by `datawatch seed`.
- **Backend setup hints in web UI** — selecting an uninstalled backend shows setup instructions (links to docs, CLI wizard command).

### Fixed
- Various web UI layout fixes.

---

## [0.5.16] - 2026-03-28

### Internal
- Version bump; no functional changes.

---

## [0.5.15] - 2026-03-28

### Added
- **MCP channel server for claude-code** — `channel/index.ts` TypeScript server implementing MCP stdio protocol; enables bidirectional tool-use notifications and reply routing without tmux.
- **Web UI dual-mode sessions** — session cards show `tmux`, `ch` (MCP channel), or `acp` (opencode ACP) mode badge.
- **Ollama setup wizard** — queries available models from the Ollama server (`GET /api/tags`) instead of requiring manual model name entry.
- **Dev channels consent prompt detection** — idle detector recognises `I am using this for local development` pattern and marks session as `waiting_input`.

### Fixed
- Channel flags and MCP server registration for `--dangerously-load-development-channels`.
- Registered Ollama and OpenWebUI backends in the LLM registry.

---

## [0.5.14] - 2026-03-28

### Added
- **opencode-acp backend** — starts opencode as an HTTP server (`opencode serve`); communicates via REST + SSE API for richer bidirectional interaction than `-p` flag mode.
- **ACP input routing** — `send <id>: <msg>` routes to opencode via `POST /session/<id>/message` instead of `tmux send-keys`.

---

## [0.5.13] - 2026-03-28

### Fixed
- **tmux interactive sessions** — fixed session launch for backends requiring PTY allocation; added debug logging for tmux session creation.
- **Update binary extraction** — archive contains plain `datawatch` binary (not versioned name); extraction now matches correctly.

---

## [0.5.12] - 2026-03-27

### Fixed
- **Session name shown in list**: session cards now display `name` when set, falling back to `task` text. Previously named sessions always showed "(no task)".
- **About section**: added link to `github.com/dmz006/datawatch` project page.

---

## [0.5.11] - 2026-03-27

### Added
- **ANSI color stripping** in web UI output — terminal escape sequences are stripped before display so raw color codes no longer appear in the session output panel.
- **Android back button support** — `navigate()` now pushes `history.pushState` entries; the `popstate` event intercepts Android Chrome's back gesture and applies the double-press guard for active sessions.
- **Settings About: version + update check** — About section now fetches version from `/api/health` and provides a "Check now" button that queries the GitHub releases API and shows an "Update" button when a newer release is available.
- **Drag-and-drop session reordering** — session cards now have a ⠿ drag handle; dragging between cards reorders the list (replaces ↑↓ buttons).
- **Numbered-menu prompt patterns** — idle detector now recognises claude-code's folder-trust numbered menu (lines containing "Yes, I trust", "Quick safety check", "Is this a project", "❯ 1.", etc.) as waiting-for-input prompts.

### Fixed
- **Signal not receiving user commands** — when signal-cli is linked to the user's own phone number, messages sent from their phone arrive as `syncMessage.sentMessage` (not `dataMessage`). The receive loop now parses these and dispatches them as commands. This was the root cause of "datawatch can send but cannot receive" on linked-device setups.
- **Shell/custom backend launches claude-code** — `manager.Start()` now looks up the requested backend by name in the llm registry when `opt.Backend` is set, wiring the correct launch function. Previously `backendName` was updated but `launchFn` remained as claude-code.
- **ANSI codes break prompt pattern detection** — `monitorOutput` now calls `StripANSI()` on the last output line before pattern matching, so TUI-style prompts with color codes trigger `waiting_input` correctly.
- **Empty project dir → "Please provide a directory path"** — `handleStartSession` (REST) and `MsgNewSession` (WS) now default `project_dir` to the user's home directory when not supplied.
- **Install script can't find binary in archive** — `install.sh` now searches the extracted archive directory for any file matching `datawatch*` when the exact `datawatch` name isn't present (handles both GoReleaser and manually packaged archives).
- **Task description required** — removed the 400-error check on empty task in `handleStartSession`; empty task starts an interactive session.
- **`moveSession` ↑↓ buttons removed** — replaced by drag-and-drop (old buttons caused layout issues and were redundant).

---

## [0.5.10] - 2026-03-27

### Added
- **Session resume documentation** added to `docs/llm-backends.md` for claude-code (`--resume`) and opencode (`-s`).
- **`root_path` and `update` config blocks** documented in `docs/operations.md` and `README.md`.

### Fixed
- **`datawatch update` now uses prebuilt binaries** instead of `go install`. Downloads the platform-specific archive from GitHub releases (with progress output), falls back to `go install` if the prebuilt download fails.
- **Directory browser navigation**: replaced inline `onclick` attributes with event delegation on `data-path` attributes, fixing navigation failures caused by special characters in path strings.
- **Task description is now optional** in the New Session form. Shell sessions and interactive backends can be started without a task.
- **`BACKLOG.md`** updated with new bugs (optional task description, dir browser navigation). Removed four previously resolved bugs.

---

## [0.5.9] - 2026-03-27

### Added
- **Delete button** in session detail view for finished sessions (complete / failed / killed). Prompts for confirmation, then calls `POST /api/sessions/delete` with `delete_data: true` to remove the session and all tracking data. Navigates back to the sessions list on success.
- **Saved commands quick-send panel** in session detail view for active and waiting sessions. Fetches `GET /api/commands` and renders clickable buttons above the input bar so saved commands can be dispatched in one click without typing.
- **Update progress output**: `installPrebuiltBinary` now prints download progress (percentage and KB at every 10% increment when `Content-Length` is known, or every 512 KB otherwise), plus extract and install step markers, so long updates give visible feedback.

### Changed
- **`AGENT.md`**: added "BACKLOG.md Discipline" rule — completed bugs/backlog items must be removed from `BACKLOG.md` after implementation; partially fixed items should be updated in place.

### Fixed
- **`BACKLOG.md`**: removed four resolved bugs (Signal already-linked detection, session delete UI, remote servers local default, needs-input alert + saved commands quick-send + SendInput accepting running state). Updated remaining bug to note partial status.

---

## [0.5.8] - 2026-03-27

### Added
- **Session history toggle**: sessions list hides stopped sessions by default; "Show history (N)" toolbar button reveals them. Active sessions are always shown.
- **Double-back-press guard**: pressing Back from an active session requires a second press within 2.5 s; a toast warning appears on the first press to prevent accidental navigation.
- **Saved sessions clickable link**: the "X saved sessions" count on the Settings page is now a clickable link that navigates to the sessions list filtered to history (completed/failed/killed).
- **Saved commands management in web UI**: Settings page now has a collapsible "Add Command" form (name + command + description fields) posting to `POST /api/commands`. Existing commands have a Delete button.
- **Output filters management in web UI**: Settings page has a collapsible "Add Filter" form (pattern + action + description) posting to `POST /api/filters`. Existing filters have a Delete button.
- **Directory browser navigation**: file browser in New Session form now has separate navigate-into and select actions. Clicking a folder navigates into it; a "✓ Use This Folder" button selects the current directory. `..` entry navigates up.
- **`session.root_path` config field**: restricts the file browser to a subtree — users cannot navigate above this path. At the root boundary, `..` is hidden and the path is clamped silently.
- **Session resume**: New Session form has an optional "Resume session ID" field. When set, claude-code launches with `--resume <ID>` and opencode with `-s <ID>`. Restart button pre-fills the resume ID from `sess.llm_session_id`.
- **Auto-update daemon**: new `update` config section with `enabled`, `schedule` (hourly/daily/weekly), and `time_of_day` (HH:MM). On schedule, downloads a prebuilt binary from GitHub releases and hot-swaps the running executable.
- **Backend availability in `/api/backends`**: each entry now includes `available` (bool) and `version` string. Web UI shows "(not installed)" for unavailable backends and a warning div when an unavailable backend is selected.

### Fixed
- **Bug: Signal already-linked detection** (`runLink`): detects existing `accounts.json` and `data/<number>/` directory; prints removal instructions instead of overwriting a working setup.
- **Bug: Session delete endpoint** (`DELETE /api/sessions/delete`): `Manager.Delete()` added; kills active session, removes from store, optionally removes tracking directory on disk.
- **Bug: Remote servers "local" default**: local server row now shows as active/connected when `state.activeServer === null`.
- **Bug: needs-input alert**: `NeedsInputHandler` now calls `alertStore.Add` so a Level-Info alert fires whenever a session enters waiting-for-input state.
- **Bug: `SendInput` accepts `running` state**: previously rejected sessions not in `waiting_input`; now also accepts `running`. State transition back to `running` only occurs if session was `waiting_input`.

---

## [0.5.7] - 2026-03-27

### Added
- **Stop button** in session detail view for active sessions (running / waiting_input / rate_limited). Sends a kill request and updates state immediately.
- **Restart button** in session detail view for finished sessions (complete / failed / killed). Pre-fills the New Session form with the original task, backend, and project directory.
- **Session backlog panel** in the New Session view: lists the last 20 completed/failed/killed sessions with one-click Restart to resume any prior task.
- **`POST /api/sessions/kill`** REST endpoint — previously session termination was only accessible via the command parser; now has a dedicated authenticated endpoint.
- **Symlink support in file browser**: `GET /api/files` now follows symlinks to directories so symlinked project folders appear as navigable directories (shown with 🔗 icon).

### Fixed
- **`make install`** now passes `-ldflags "-X main.Version=..."` so the installed binary reports the correct version string instead of the default `0.5.x`.

---

## [0.5.6] - 2026-03-27

### Fixed
- **install.sh**: `--version X.Y.Z` argument parsing was broken (`${!@}` syntax error under `set -euo pipefail`), causing the script to exit immediately with no output. Replaced with a portable `while` loop over a bash array.

---

## [0.5.5] - 2026-03-27

### Fixed
- **Windows cross-compile**: moved `daemonize()` to `daemon_unix.go` (`//go:build !windows`) and `daemon_windows.go` so the Windows binary builds without the Unix-only `syscall.SysProcAttr.Setsid` field. Pre-built Windows/amd64 binary now included in releases.

---

## [0.5.4] - 2026-03-27

### Added
- **`datawatch diagnose [signal|telegram|discord|slack|all]`**: connectivity diagnostic command for all messaging backends. Signal diagnose lists all known groups and validates the configured `group_id`; Telegram, Discord, Slack check live API connectivity. `--send-test` flag sends a test message to verify outbound delivery.
- **`datawatch diagnose --send-test`**: sends a test message to the configured group/channel to verify end-to-end delivery.
- **Signal backend stderr capture**: signal-cli's stderr is now always piped and logged (`[signal-cli stderr] ...`). Java startup errors, auth failures, and exceptions are now visible in daemon.log.
- **Signal backend verbose logging**: `datawatch start -v` now logs every raw JSON-RPC line sent/received from signal-cli, and full pretty-printed notifications for debugging.
- **Signal 4MB scanner buffer**: increased from 64KB default to 4MB to handle large group messages without silent truncation.
- **Signal self-filter fix**: normalises both sides of the number comparison with `strings.TrimPrefix("+")` to avoid format mismatch false-positives/negatives; uses `EffectiveSource()` (preferring `sourceNumber` in signal-cli v0.11+ responses).
- **Signal `EffectiveSource()`**: `Envelope.EffectiveSource()` returns `sourceNumber` (populated in signal-cli v0.11+) if set, falling back to `source`.
- **Signal scanner error logging**: `readLoop` now logs scanner errors explicitly instead of silently exiting.
- **Installer `--version X.Y.Z`**: install script now accepts `--version X.Y.Z` to install a specific release instead of the latest.

### Changed
- **Signal notification dispatch**: non-data envelopes (receipts, typing, sync) are logged in verbose mode and silently skipped; previously they caused confusing empty-message drops.
- **AGENT.md**: added "Supported Commands / Notification Events" requirement to the "New messaging/communication interface" rule; added interactive input, filter, and saved command documentation requirements to the "New LLM backend" rule.
- **`docs/messaging-backends.md`**: added "Command Support by Backend" comparison table; added "Supported Commands" section to Signal, Telegram, Discord, Slack, Matrix, and Twilio; added "Notification Events" table to ntfy and Email.
- **`docs/llm-backends.md`**: added "Command and Filter Support" section covering saved commands, output filters, and interactive input support by backend.

---

## [0.5.3] - 2026-03-27

### Changed
- **AGENT.md**: Added "Functional Change Checklist" under Release Discipline — after any functional change, bump version, run `make release-snapshot` to verify build, verify `datawatch update --check` reports the new version, and confirm install script downloads the prebuilt binary. Documents that `datawatch update --check` (CLI) and `update check` (messaging) are the canonical ways to check for available upgrades.

---

## [0.5.2] - 2026-03-27

### Fixed
- **install.sh**: prebuilt binary is now tried first; Go source build is only used as a fallback when the release archive download fails. Previously, if Go was installed on the host, the installer skipped the prebuilt download entirely and built from source (downloading Go if it wasn't new enough), which was slow and unnecessary.

---

## [0.5.1] - 2026-03-27

### Fixed
- **install.sh**: version is now resolved dynamically from the GitHub releases API at install time (was hardcoded as `"0.1.0"`); falls back to `"0.5.0"` if the API is unreachable.
- **install.sh**: Go fallback installer no longer fails with `mv: cannot overwrite` when the Go versioned directory already exists (`~/.local/go-X.Y.Z` is removed and re-extracted cleanly).
- **install.sh**: prebuilt binary download now uses GoReleaser archive format (`datawatch_VERSION_linux_ARCH.tar.gz`) instead of the old bare-binary URL that was never published.

---

## [0.5.0] - 2026-03-27

### Added
- **`alerts` messaging command**: send `alerts [n]` to any messaging backend to view recent alert history; alerts are also broadcast proactively to all active messaging backends when they fire.
- **`setup llm <backend>`**: CLI and messaging-channel setup wizards for all LLM backends — `claude-code`, `aider`, `goose`, `gemini`, `opencode`, `ollama`, `openwebui`, `shell`.
- **`setup session`**: wizard to configure session defaults (LLM backend, max sessions, idle timeout, tail lines, project dir, skip permissions).
- **`setup mcp`**: wizard to configure the MCP server (enable, SSE, host, port, TLS, token).
- **`datawatch test [--pr]`**: collects non-sensitive interface status (endpoints, binary paths, model names) for all enabled interfaces and optionally opens a GitHub PR updating `docs/testing-tracker.md`.
- **GoReleaser integration**: `.goreleaser.yaml` added; `make release` creates GitHub releases with pre-built binaries for linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, windows/amd64.
- **AGENT.md rules**: added rules for new communication interfaces (testing tracker, data flow docs, config field documentation, security options), and for release discipline (GoReleaser, PR check before commit/release).
- **DNS channel design**: expanded `docs/covert-channels.md` with command subset, query format specification, Mermaid sequence diagram, config block, security model, and threat model table.
- **`docs/testing-tracker.md`**: expanded with LLM backend rows, Web/API section, validation checklists per interface type, and DNS channel row.

### Changed
- `BACKLOG.md`: removed implemented `#future` items (LLM setup wizards, alerts command). Remaining backlog: DNS channel implementation.
- `Makefile`: updated VERSION to 0.5.0; added `release` and `release-snapshot` targets; added `windows/amd64` to `cross` target.

---

## [0.4.1] - 2026-03-27

### Added
- **Upgrade guide** in `docs/operations.md` (new section 2): covers `datawatch update`, in-place upgrade procedure, data compatibility across versions, encrypted store stability, and rollback.
- **`docs/covert-channels.md`**: research notes on DNS tunneling, ICMP, NTP, HTTPS, and steganographic channels for constrained network environments.

### Changed
- `docs/operations.md`: section numbering shifted by one (new section 2 = Upgrading; former sections 2–8 are now 3–9).
- `BACKLOG.md`: removed completed `#today` and `#next` sections; retained `#future` and `#backlog`.

---

## [0.4.0] - 2026-03-27

### Added
- **`--secure` now encrypts all data stores**: sessions.json, schedule.json, commands.json, filters.json, alerts.json are all encrypted with AES-256-GCM when `--secure` is set. A 32-byte symmetric key is derived once at startup via Argon2id and a persistent salt at `~/.datawatch/enc.salt`. Per-write operations use a fresh nonce with no KDF overhead.
- **`internal/secfile` package**: `Encrypt`/`Decrypt`/`ReadFile`/`WriteFile` helpers for AES-256-GCM store encryption without re-running the KDF. All stores have `*Encrypted` constructor variants.
- **`config.DeriveKey` + `config.LoadOrGenerateSalt`**: key derivation and salt persistence for the data encryption layer.
- **Command library** (`internal/session/cmdlib.go`): named reusable command strings backed by `~/.datawatch/commands.json`
- **`datawatch cmd add/list/delete`**: CLI commands for managing the command library
- **`datawatch seed`**: pre-populates the command library and filter store with useful defaults for common AI session interactions
- **Session output filters** (`internal/session/filter.go`): regex-based rules that fire `send_input`, `alert`, or `schedule` actions when output lines match
- **`FilterStore` + `FilterEngine`**: persistent filter store; engine processes each output line against enabled filters via `onOutput` callback on session Manager
- **`Manager.SetOutputHandler`**: new callback on the session manager, called for each output line; used by the filter engine
- **System alert channel** (`internal/alerts/store.go`): persistent alert store at `~/.datawatch/alerts.json`; listener pattern for WebSocket broadcast on new alerts
- **`GET /api/commands`**, **`POST /api/commands`**, **`DELETE /api/commands`**: REST endpoints for command library management
- **`GET /api/filters`**, **`POST /api/filters`**, **`PATCH /api/filters`**, **`DELETE /api/filters`**: REST endpoints for filter management
- **`GET /api/alerts`**, **`POST /api/alerts`** (mark read): REST endpoints for alert history
- **`MsgAlert` WebSocket type**: server pushes new alerts to all connected Web UI clients in real time
- **Alerts view in Web UI**: dedicated Alerts nav tab with unread badge counter; shows full alert history; marks all as read on open
- **Saved Commands section in Web UI Settings**: lists saved commands, allows deletion
- **Output Filters section in Web UI Settings**: lists filters with enable/disable toggle and deletion
- **Scheduler NeedsInputHandler bug fix**: `runScheduler` previously overwrote the combined NeedsInputHandler set in `runStart`. Fixed by extracting `fireInputSchedules` as a standalone helper called from the combined handler instead.

### Changed
- `--secure` mode previously only encrypted `config.yaml`; now all data stores are encrypted when the flag is set
- `session.NewManager` accepts an optional `encKey []byte` variadic parameter for encrypted session store
- `ScheduleStore`, `CmdLibrary`, `FilterStore`, `alerts.Store` all expose `*Encrypted` constructors
- Version bumped to 0.4.0

## [0.3.0] - 2026-03-27

### Added
- **`datawatch update [--check]`**: check for and install updates via `go install` from GitHub releases
- **`datawatch setup server`**: CLI wizard to add/edit remote datawatch server connections with connectivity test
- **`--server <name>` global flag**: target any CLI command at a configured remote server
- **`datawatch session schedule` subcommands**: `add`, `list`, `cancel` for scheduling commands to sessions
- **Schedule daemon goroutine**: fires time-based scheduled commands every 10s; on-input commands fire when sessions enter `waiting_input` state
- **`version` messaging command**: reply with current daemon version
- **`update check` messaging command**: reply with version + update availability
- **`schedule <id>: <when> <cmd>` messaging command**: schedule a command from any messaging backend
- **`GET /api/servers`**: list configured remote servers (tokens masked)
- **`GET/POST/DELETE /api/schedule`**: REST endpoints for schedule management
- **`GET|POST /api/proxy/{serverName}/{path}`**: proxy endpoint for Web UI to reach remote servers
- **Remote Server setup wizard** over all messaging channels (`setup server`)
- **Session ordering in Web UI**: up/down buttons on session cards; order persisted in localStorage
- **Remote server selector in Web UI Settings**: lists configured servers with reachability status; click to select active server
- **`RemoteServerConfig` + `Servers []RemoteServerConfig`** config fields
- **`internal/session/ScheduleStore`**: persistent schedule store at `~/.datawatch/schedule.json`
- **MCP daemon compatibility note** in `docs/mcp.md`

### Changed
- Router `handleSetup` now lists `server` as an available service
- `newRouter` helper in `runStart` wires schedule store, version, and update checker into every router
- Version bumped to 0.3.0 (new features, non-breaking additions)

## [0.2.0] - 2026-03-27

### Added
- **Daemon mode**: `datawatch start` now daemonizes by default (PID file at `~/.datawatch/daemon.pid`, logs to `~/.datawatch/daemon.log`). Use `--foreground` to run in terminal.
- **`datawatch stop` command**: sends SIGTERM to the running daemon; `--sessions` flag also kills all active AI sessions.
- **`datawatch setup <service>` command**: interactive CLI wizards for all 11 backends (signal, telegram, discord, slack, matrix, twilio, ntfy, email, webhook, github, web). Telegram/Discord/Slack/Matrix wizards auto-discover channels/rooms via API.
- **Setup wizards over messaging channels**: `setup <service>` command available via every messaging backend (Signal, Telegram, Discord, Slack, Matrix, etc.) — stateful multi-turn conversation engine in `internal/wizard/`.
- **Config encryption**: `--secure` flag enables AES-256-GCM config file encryption with Argon2id key derivation. Use with `datawatch --secure config init` and `datawatch --secure start --foreground`.
- **`GET/PUT /api/config` REST endpoint**: read and patch backend enable/disable status and server settings. Sensitive fields (tokens, passwords) are masked in GET responses.
- **Web UI backend status**: Settings view now shows enable/disable status for all backends with toggle buttons (calls `/api/config`).
- **Quick-input buttons in Web UI**: when a session is waiting for input, y/n/Enter/Ctrl-C buttons appear above the text input for one-click responses.
- **Web server setup wizard** (`setup web`): enable/disable the web UI and configure host/port/bearer token/TLS from CLI or any messaging backend.
- `server.tls_enabled` and `server.tls_auto_generate` config fields for TLS configuration.
- `docs/testing-tracker.md`: interface validation status tracker for all 14 backends.
- `internal/wizard/` package: `Manager`, `Session`, `Step`, `Def` types for cross-channel stateful wizards.

### Changed
- `datawatch start` now defaults to daemon mode. Existing `--foreground` flag keeps the old behavior.
- `datawatch config init` wizard is now service-agnostic (Signal section is optional).
- "No backends enabled" error message now points at `datawatch setup <service>`.
- `datawatch link` auto-creates the config file if it does not exist.
- `router.NewRouter()` signature updated to accept an optional `*wizard.Manager`.
- `server.New()` signature updated to accept `fullCfg` and `cfgPath` for `/api/config`.
- Version bumped to 0.2.0 (new features, non-breaking additions).

### Dependencies
- `golang.org/x/crypto` promoted from indirect to direct (Argon2id for config encryption)
- `golang.org/x/term` promoted from indirect to direct (password prompts for `--secure`)

## [0.1.4] - 2026-03-26

### Added
- Session naming: set a human-readable name at creation (`--name` flag or PWA) and rename at any time (`session rename`, `/api/sessions/rename`)
- Per-session LLM backend override: `--backend` flag on `session new`, backend selector in PWA
- `session stop-all` CLI command: kill all running sessions on this host
- `backend list` CLI command: list registered LLM backends with active marker
- `completion` CLI command: shell completion for bash, zsh, fish, powershell
- New REST endpoints: `/api/backends`, `/api/files`, `/api/sessions/start`, `/api/sessions/rename`
- Directory browser in PWA: navigate the filesystem to select a project directory when starting a session
- `skip_permissions` config field: pass `--dangerously-skip-permissions` to claude-code
- `kill_sessions_on_exit` config field: kill all active sessions when the daemon exits
- `--llm-backend`, `--host`, `--port`, `--no-server`, `--no-mcp` flags on `datawatch start`
- ANSI escape code stripping from session log output sent via messaging backends
- `NO_COLOR=1` set when launching claude-code to reduce color noise
- Extended claude-code permission dialog patterns for `waiting_input` detection
- Debug logging for Signal group ID mismatches to aid troubleshooting
- Session list now shows `NAME/TASK` and `BACKEND` columns
- AGENT.md versioning rule: every push requires a patch version bump
- AGENT.md docs rule: every commit must include documentation updates

### Changed
- `session new` now uses `/api/sessions/start` REST endpoint when daemon is running
- Config `show` subcommand displays all sections including messaging and LLM backends
- Signal self-filter is now lenient for phone number format variations

## [0.1.0] - 2026-03-26

### Added
- Signal group integration via signal-cli JSON-RPC daemon mode
- QR code device linking (terminal and PWA)
- claude-code session management via tmux
- Session state machine: running, waiting_input, complete, failed, killed
- Async Signal notifications for state changes and input prompts
- Session persistence across daemon restarts (JSON file store)
- Multi-machine support with hostname-prefixed session IDs
- Progressive Web App with real-time WebSocket interface
- Mobile-first dark theme PWA installable on Android home screen
- Browser push notifications for session input requests
- OpenAPI 3.0 specification with Swagger UI at /api/docs
- REST API: sessions, output, command, health, info, link endpoints
- Server-Sent Events for QR linking flow in PWA
- Pluggable LLM backend interface (internal/llm)
- Pluggable messaging backend interface (internal/messaging)
- claude-code directory constraints via --add-dir flag
- Automatic git tracking: pre/post session commits
- CLI session subcommands (no daemon required for local ops)
- Universal Linux installer (root and non-root modes)
- Systemd service with security hardening
- macOS LaunchAgent support
- Windows/WSL2 installation instructions
- Debian .deb packaging configuration
- RPM .spec for RHEL/CentOS/Fedora
- Arch Linux PKGBUILD
- Comprehensive documentation suite
