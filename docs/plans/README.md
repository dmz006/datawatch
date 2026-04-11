# Plans, Bugs & Backlog

Single source of truth for all datawatch project tracking.

---

# Bug and Feature rules
## When plans are inspired by other projects (hackerdave, milla jovovich, etc.), credit the source — see [Plan Attribution Guide](../plan-attribution.md)
## make sure all implementation of bugs or features have 100% (or close) code test coverage and that the fixes or functionality is actually tested through web, api, or any means you have access to validate the code works as requested
## if testing involves creating testing sessions be sure to stop and delete those sessions when done
## Unclassified bugs
(none)

## Unclassified features
- in the director selector in new session and settings, need to be able to create a folder if it doesn't exist.
- review https://github.com/AndyMik90/Aperant?tab=readme-ov-file and see what can be done for integration and using datawatch as a session service for it

## Open Bugs

| # | Description | Priority | Notes |
|---|-------------|----------|-------|
| B1 | ~~xterm.js crashes and slow load~~ — **FIXED v2.3.0**: 32ms load (was 20s), crash guard, output batching, ResizeObserver leak | ~~high~~ | Plan: [xterm-stability](2026-04-11-xterm-stability.md) |
| B2 | ~~Claude Code prompt detection false positives~~ — **FIXED v2.3.0**: status bar check, expanded indicators, velocity check, oscillation backoff. Zero false positives in 60s live test. | ~~high~~ | Plan: [claude-prompt-detection](2026-04-11-claude-prompt-detection.md) |
| B3 | ~~LLM session reconnect on daemon restart~~ — **FIXED v2.2.9**: backend_state.json persists connection state, auto-reconnect on startup | ~~medium~~ | Plan: [session-reconnect](2026-04-11-session-reconnect.md) |
| B4 | Input bar sometimes disappears in session detail — requires navigating away and back to restore | medium | Likely caused by re-render during state change wiping DOM |


## Open Features

| # | Description | Priority | Effort | Notes |
|---|-------------|----------|--------|-------|
| F7 | libsignal — replace signal-cli with native Go | low | 3-6 months | Plan: [libsignal](2026-03-29-libsignal.md) |
| F10 | Container images, Helm chart | medium | 1-2 days | Plan: [backlog-plans](2026-04-01-backlog-plans.md#bl3-container-images-and-helm-chart) |
| F13 | Copilot/Cline/Windsurf backends | low | 1-2hr each | Plan: [backlog-plans](2026-04-01-backlog-plans.md#bl19-copilotclinewindsurf-backends) |
| F14 | Live cell DOM diffing | low | 3-4hr | Plan: [backlog-plans](2026-04-01-backlog-plans.md#bl2-live-cell-dom-diffing) |
| F15 | Session chaining — pipelines with DAG | low | 1-2 days | Plan: [backlog-plans](2026-04-01-backlog-plans.md#bl4-session-chaining) |

## Backlog — All Remaining Items

All items have plans. Quick wins marked with ⚡.

### Sessions (10 items)

| ID | Item | Effort | Notes |
|----|------|--------|-------|
| BL5 | Session templates | 1 day | [plan](2026-04-11-backlog-plans.md#bl5-session-templates) |
| BL6 | Cost tracking | 2-3 days | [plan](2026-04-11-backlog-plans.md#bl6-cost-tracking) |
| BL26 | Cron-style schedules | 1-2 days | [plan](2026-04-11-backlog-plans.md#bl26-scheduled-prompts-cron-style) |
| BL27 | Project management | 3-4hr | [plan](2026-04-11-backlog-plans.md#bl27-project-management) |
| BL29 | Git checkpoints + rollback | 1 day | [plan](2026-04-11-backlog-plans.md#bl29-git-checkpoints) |
| BL30 | Rate limit cooldown | 3-4hr | [plan](2026-04-11-backlog-plans.md#bl30-rate-limit-cooldown-system) |
| ⚡BL34 | Read-only ask mode | 2-3hr | [plan](2026-04-11-backlog-plans.md#bl34-read-only-ask-mode) |
| ⚡BL35 | Project summary command | 2-3hr | [plan](2026-04-11-backlog-plans.md#bl35-project-summary-command) |
| BL40 | Stale task recovery | 3-4hr | [plan](2026-04-11-backlog-plans.md#bl40-stale-task-recovery) |
| ⚡BL41 | Effort levels per task | 1-2hr | [plan](2026-04-11-backlog-plans.md#bl41-effort-levels-per-task) |

### Intelligence (4 items — all depend on F15 pipelines)

| ID | Item | Effort | Notes |
|----|------|--------|-------|
| BL24 | Autonomous task decomposition | 1-2 weeks | [plan](2026-04-11-backlog-plans.md#bl24-autonomous-task-decomposition). Depends on F15 |
| BL25 | Independent verification | 2-3 days | [plan](2026-04-11-backlog-plans.md#bl25-independent-verification). Depends on BL24 |
| BL28 | Quality gates | 2-3 days | [plan](2026-04-11-backlog-plans.md#bl28-quality-gates). Depends on BL24 |
| ⚡BL39 | Circular dep detection | 2-3hr | [plan](2026-04-11-backlog-plans.md#bl39-circular-dependency-detection). Depends on BL24 |

### Observability (4 items)

| ID | Item | Effort | Notes |
|----|------|--------|-------|
| ⚡BL10 | Session diffing (git diff in alerts) | 2-3hr | [plan](2026-04-11-backlog-plans.md#bl10-session-diffing) |
| BL11 | Anomaly detection | 1-2 days | [plan](2026-04-11-backlog-plans.md#bl11-anomaly-detection) |
| BL12 | Historical analytics + charts | 2-3 days | [plan](2026-04-11-backlog-plans.md#bl12-historical-analytics) |
| BL86 | Remote GPU/system stats agent | 1-2 days | [plan](2026-04-11-backlog-plans.md#bl86-remote-gpu-stats-agent) |

### Operations (4 items)

| ID | Item | Effort | Notes |
|----|------|--------|-------|
| BL17 | Hot config reload (SIGHUP) | 3-4hr | [plan](2026-04-11-backlog-plans.md#bl17-hot-config-reload) |
| ⚡BL22 | RTK auto-install | 1-2hr | [plan](2026-04-11-backlog-plans.md#bl22-rtk-auto-install) |
| ⚡BL37 | System diagnostics command | 2-3hr | [plan](2026-04-11-backlog-plans.md#bl37-system-diagnostics) |
| ~~BL85~~ | ~~RTK auto-update check~~ | ~~Done v2.3.5~~ | GitHub version check, auto-update, stats |

### Collaboration (3 items)

| ID | Item | Effort | Notes |
|----|------|--------|-------|
| BL7 | Multi-user access control | 1-2 weeks | [plan](2026-04-11-backlog-plans.md#bl7-multi-user-access-control) |
| BL8 | Session sharing (time-limited links) | 1 day | [plan](2026-04-11-backlog-plans.md#bl8-session-sharing) |
| BL9 | Audit log | 3-4hr | [plan](2026-04-11-backlog-plans.md#bl9-audit-log) |

### Messaging (2 items)

| ID | Item | Effort | Notes |
|----|------|--------|-------|
| BL15 | Rich previews in alerts | 1 day | [plan](2026-04-11-backlog-plans.md#bl15-rich-previews) |
| BL31 | Device targeting (@device routing) | 1 day | [plan](2026-04-11-backlog-plans.md#bl31-device-targeting) |

### Backends & UI (5 items)

| ID | Item | Effort | Notes |
|----|------|--------|-------|
| BL20 | Backend auto-selection (routing rules) | 1 day | [plan](2026-04-11-backlog-plans.md#bl20-backend-auto-selection) |
| BL42 | Quick-response assistant | 3-4hr | [plan](2026-04-11-backlog-plans.md#bl42-quick-response-assistant) |
| BL69 | Splash screen — custom logo support | 2-3hr | Partially done v1.3.1 |
| BL78 | Chat UI: Gemini chat mode | 3-4hr | Extends BL73 |
| BL79 | Chat UI: Aider/Goose chat mode | 1 day | Extends BL73 |

### Memory & Security (4 items)

| ID | Item | Effort | Notes |
|----|------|--------|-------|
| BL45 | ChromaDB/Pinecone/Weaviate backends | 1-2 days each | [plan](2026-04-09-memory-backlog.md) Tier 3 |
| BL72 | OpenCode memory hooks | 3-4hr | Extends BL65 to opencode |
| ⚡BL38 | Message content privacy | 2-3hr | [plan](2026-04-11-backlog-plans.md#bl38-message-content-privacy) |
| ⚡BL1 | IPv6 listener support | 1-2hr | [plan](2026-04-11-backlog-plans.md#bl1-ipv6-listener-support) |

### Extensibility (1 item)

| ID | Item | Effort | Notes |
|----|------|--------|-------|
| BL33 | Plugin framework | 2-3 days | [plan](2026-04-11-backlog-plans.md#bl33-plugin-framework) |
| ~~BL70~~ | ~~Memory: key rotation and management~~ | ~~memory~~ | ~~Done v1.5.1~~ |

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

## Testing Results (v1.3.0)

**179 tests pass across 39 packages.** (v1.6.0)

### Go Unit Tests — All Packages

| Package | Status | Count | Tests |
|---------|--------|-------|-------|
| `cmd/datawatch` | PASS | 6 | LinkViaCommand (StderrURI, StdoutURI, Failure, CalledOnceOnly, QRCodeGeneration, NoURINoCallback) |
| `internal/config` | PASS | 13 | DefaultConfig, Load (NonExistent, InvalidYAML, Partial, ZeroFieldsGetDefaults), Save (RoundTrip, FilePermissions, CreatesParentDirs), ConfigPath, GetOutputMode_OpenWebUIDefaultsToChat, GetOutputMode_OpenWebUIExplicitOverride, GetOutputMode_OtherBackendsDefaultToTerminal, ProxyConfig_Defaults |
| `internal/messaging/backends/dns` | PASS | 11 | NonceReplay, NonceTTL, NonceLRU, NonceEmpty, EncodeDecodeQueryRoundTrip, DecodeQuery (BadHMAC, DomainMismatch), EncodeDecodeResponseRoundTrip, EncodeResponseFragmentation, ServerIntegration (6 sub-tests), ClientExecute |
| `internal/proxy` | PASS | 14 | NewRemoteDispatcher, HasServers, FetchSessions (mock HTTP), FindSession (short + full ID), ForwardCommand (mock), ListAllSessions (2 servers parallel), AuthToken (Bearer injection), NewPool, Pool_CircuitBreaker, Pool_RecordSuccess_ClearsErrors, OfflineQueue_Enqueue, OfflineQueue_PendingAll, OfflineQueue_Replay |
| `internal/router` | PASS | 17 | Parse (New, NewWithProjectDir, NewAtServer, NewCaseInsensitive, NewStripsWhitespace, NewNoTask, List, Status, Send, SendMissingColon, SendColonInMessage, Kill, Tail_DefaultN, Tail_WithN, Attach, History, Help, Unknown), HelpText, Truncate |
| `internal/rtk` | PASS | 3 | CheckInstalled, SetBinary, CollectStats |
| `internal/secfile` | PASS | 10 | EncryptedLog (RoundTrip, LargeData, WrongKey, MultipleWrites, Flush, EmptyFile), Migrate (LogOnly, Full, SkipsEncrypted, EmptyDir) |
| `internal/session` | PASS | 24 | CancelBySession, CancelBySessionShortID, Delete, Store (NewEmpty, NewFromMissingFile, Save_Get, GetMissing, GetByShortID, GetByShortID_CaseInsensitive, GetByShortID_Missing, List, ListEmpty, Update, Delete, DeleteMissing, Persistence, PersistAfterDelete, MultipleSavesSameID), StateConstants, ParseScheduleTime (valid, NextWeekday, Errors), EmitChatMessage_NoCallback, EmitChatMessage_WithCallback, SetOnChatMessage_Replaces |
| `internal/memory` | PASS | 14 | NewStore, SaveAndListRecent, ListByRole, Delete, Count, SearchWithEmbeddings, Prune, EncodeDecodeVector, ChunkText (Short, Empty, Long), ChunkLines (Simple, Split), CosineSimilarity |
| `internal/llm/backends/openwebui` | PASS | 5 | SetChatEmitter, EmitChat_NoEmitter, EmitChat_StripsCsPrefix, InteractiveBackend_Name, NewInteractive_Defaults |
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

**OpenWebUI chat UI (B26, v1.2.x):**
- Session create with `backend=openwebui` returns `output_mode: chat`: PASS
- WS `chat_message` events fire: user message + streaming assistant chunks + final: PASS (4 events verified via Python WebSocket client)
- Multi-turn conversation: "say hello" → "Hello! 😊", "2+2" → "4", "5+5" → "10": PASS
- Comm channel list/status/send/kill all route correctly for chat-mode sessions: PASS
- Deployed `style.css` contains 4 chat CSS rules, `app.js` contains 21 chat references: PASS
- Test sessions cleaned up after validation: PASS

**Proxy mode Phases 4-5 (F16, v1.2.x):**
- `/remote/{server}/` PWA reverse proxy route registered behind auth: PASS (code verified)
- `GET /api/servers/health` returns health status with breaker state and queued counts: PASS (code verified)
- Settings → Servers shows health badges (healthy/degraded/down) and queued count: PASS (code verified)
- Settings → Proxy Resilience card with editable config fields: PASS (code verified)
- ProxyConfig persists via `PUT /api/config` with proxy.* keys: PASS (code verified)

**Episodic memory (BL23/BL32/BL36, v1.2.x):**
- Config: `PUT /api/config` with `memory.enabled=true` persists and activates: PASS
- `remember:` saves memories with Ollama embeddings (nomic-embed-text): PASS
- `memories` lists all stored memories with count: PASS
- `recall:` semantic search ranks correctly: "CI requirements" → CI memory top (60%), "deploy" → deploy memory top (77%), "database" → db memory top (59%): PASS
- `forget <id>` deletes specific memory: PASS
- `learnings` lists learnings (empty when none): PASS
- `help` includes all 5 memory commands: PASS
- Settings → General → Episodic Memory card renders in web UI: PASS (code verified)
- Test memories cleaned up after validation: PASS

**Terminal display fixes (v1.2.x):**
- Session exit no longer flashes shell prompt — pane_capture frozen on completion: PASS (code verified)
- Terminal scrollback no longer accumulates — `\x1b[3J` clears scrollback each frame: PASS (code verified)
- Re-renders of same session preserve terminal state (no reset on channel_ready): PASS (code verified)

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
| B27 | Alerts/comm channels missing user prompt text | v1.5.2: alert body now includes "Prompt: {input}\n---\n{response}". New `prompt` command + API endpoint + MCP `get_prompt` tool. All 3 input paths (web/comm/tmux) capture LastInput. |
| B26 | OpenWebUI interactive chat UI — raw shell echo/printf replaced with structured chat bubbles | v1.2.x: chat output_mode with WS chat_message events, streaming support, CSS chat bubbles. Tested: unit tests (18 new across 4 packages), API session create output_mode=chat, WS chat_message events (user + streaming assistant + final), multi-turn conversation (2+2=4, 5+5=10), comm channel list/status/send/kill, deployed CSS/JS verified |
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
| F16 | Proxy mode (all phases) | v1.1.0: Phases 1-3 — WS proxy relay, aggregated sessions API, remote command routing via comm channels, `new: @server:` syntax, server badges in UI, web-only daemon keepalive fix. v1.2.x: Phases 4-5 — PWA reverse proxy (`/remote/{server}/` with content rewriting), HTTP client pool with connection reuse, circuit breaker (configurable threshold/reset), offline command queue with auto-replay, `/api/servers/health` endpoint, ProxyConfig in YAML/API/web UI, health badges on server list. |
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
| RTK Integration | v1.0.2 | 2026-03-30 | [rtk-integration](2026-03-30-rtk-integration.md) |
| Backlog Detailed Plans | — | 2026-04-01 | [backlog-plans](2026-04-01-backlog-plans.md) |
| Channel Parity Review | v1.0.2 | 2026-04-01 | [channel-parity-review](2026-04-01-channel-parity-review.md) |
| Proxy Mode (all phases) | v1.2.x | 2026-04-02 | [proxy-mode](2026-04-02-proxy-mode.md) |
| OpenWebUI Chat UI (B26) | v1.2.x | 2026-04-06 | [openwebui-chat-ui](2026-04-06-openwebui-chat-ui.md) |
| Episodic Memory (BL23/32/36) | v1.3.0 | 2026-04-09 | [intelligence](2026-04-06-intelligence.md) Phases 1-3 |

### Open Plans (not yet implemented)

| Plan | Date | File |
|------|------|------|
| libsignal | 2026-03-29 | [libsignal](2026-03-29-libsignal.md) |
| Intelligence Features | 2026-04-06 | [intelligence](2026-04-06-intelligence.md) |
| Memory Backlog (BL43-67) | 2026-04-09 | [memory-backlog](2026-04-09-memory-backlog.md) |
| Memory Encryption (BL68-70) | 2026-04-09 | [memory-encryption](2026-04-09-memory-encryption.md) |
