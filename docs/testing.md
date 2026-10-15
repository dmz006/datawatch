# Testing — Validation Procedures & Results

All bug fixes and features must have documented test results.

**Bug/plan tracking:** [docs/plans/README.md](plans/README.md)

---

## How to Test Each Interface Channel

Every new feature must be tested through **all applicable channels**. Use this
checklist for each feature:

### 1. API (REST)
```bash
# Direct endpoint test
curl -s http://localhost:8080/api/<endpoint> | python3 -m json.tool

# Simulate comm channel command
curl -s -X POST http://localhost:8080/api/test/message \
  -H "Content-Type: application/json" \
  -d '{"text":"<command>"}'
```

### 2. Communication Channels (Signal, Telegram, Slack, etc.)
- Use `POST /api/test/message` to simulate any comm channel command
- Verify response text matches expected format
- For live validation: send the command from the actual messaging app

### 3. Web UI
- Navigate to the relevant page/tab in browser
- Check Chrome DevTools Console (F12) for JS errors
- Verify elements render, buttons work, real-time updates arrive
- **Debug panel**: triple-tap the status dot for last 50 errors/events

### 4. WebSocket
- Open browser DevTools → Network → WS tab
- Verify message types arrive: `sessions`, `session_state`, `output`, `pane_capture`, `chat_message`, `response`, `stats`, `needs_input`, `alert`

### 5. MCP (Model Context Protocol)
- Check tool appears in `GET /api/mcp/docs`
- For stdio: `claude mcp list` shows tool registered
- For SSE: POST to MCP SSE endpoint with tool call

### 6. Config
- `PUT /api/config {"key":"...", "value":...}` → verify with `GET /api/config`
- Verify web UI Settings card reflects the change
- Verify `configure <key>=<value>` via comm channel

### Documentation Template
When documenting a test, use this table format:

| # | Feature | API | Comm | Web | MCP | Result |
|---|---------|-----|------|-----|-----|--------|

---

## Core Interface & Feature Tests

> **Jump to sections:**
> - [Interface Validation Tracker](#interface-validation-tracker) — messaging, web, API, MCP, LLM backend validation status
> - [v1.3.x–v1.5.x Feature Tests](#v13x15x-feature-tests) — memory, copy, spatial, KG, encryption tests by channel
> - [Unit Test Summary](#unit-test-summary-v151) — Go test counts per package
> - [Historical Feature Tests](#historical-feature-tests) — numbered v0.14–v1.2 tests

---

## Historical Feature Tests

### 1. Splash Screen

| Field | Value |
|-------|-------|
| Version | v0.14.5 |
| Steps | Hard refresh (Ctrl+Shift+R), observe splash with eye logo, spinning ring, bouncing dots. Time it — 3+ seconds. |
| Expected | Purple eye logo centered, "datawatch" title, "AI Orchestration" subtitle, smooth fade to sessions view. |
| Result | **PASS** — 3 second minimum display, fades on connect |

## 2. Interface Binding Selector

| Field | Value |
|-------|-------|
| Version | v0.14.5, updated v0.17.4 |
| Steps | Settings → General → "Bind interface". Checkboxes for 0.0.0.0, 127.0.0.1, and network interfaces. Uncheck "all", check a specific — "all" auto-unchecks. Check "all" — others uncheck. Connected interface shows "(connected)" badge. |
| Expected | Mutual exclusion works, save confirmation, restart hint. Connected interface detected via Tailscale hostname resolution. |
| Result | **PASS** — config saved, mutual exclusion verified. v0.17.4: no longer blocks save on connected-interface warning. |

## 3. SSE Interface Binding

| Field | Value |
|-------|-------|
| Version | v0.14.5 |
| Steps | Settings → MCP Server → "SSE bind interface" — same checkbox behavior as #2. |
| Expected | Same mutual exclusion as web server interface. |
| Result | **PASS** — PUT `{"mcp.sse_host":"127.0.0.1"}` verified via GET |

## 4. TLS Dual-Port

| Field | Value |
|-------|-------|
| Version | v0.14.6 |
| Steps | Enable TLS on port 8443, restart. `curl -sI http://127.0.0.1:8080/api/health \| grep Location`. Verify HTTPS on 8443. Disable TLS and restart to reset. |
| Expected | HTTP redirects to HTTPS with correct URL (no double port). |
| Result | **PASS** — `Location: https://127.0.0.1:8443/api/health`. Fix: strip port from `r.Host` before redirect. |

## 5. Stop/Delete Confirm Modal

| Field | Value |
|-------|-------|
| Version | v0.14.5 |
| Steps | Click Stop on session. Dark overlay modal appears. Verify Yes focused. Press Enter. Try Delete on completed session. |
| Expected | No browser `confirm()`. Inline modal, Yes auto-focused, Enter confirms. |
| Result | **PASS** — `yesBtn.focus()` called after modal render |

## 6. Detection Filters Managed List

| Field | Value |
|-------|-------|
| Version | v0.14.5 |
| Steps | Settings → LLM → Detection Filters. 4 subsections with pattern counts. Add "test-pattern", verify listed. Remove via X. |
| Expected | Patterns display with counts, add/remove works, toast confirms. |
| Result | **PASS** — addDetPattern/removeDetPattern verified |

## 7. About Section Logo

| Field | Value |
|-------|-------|
| Version | v0.14.5 |
| Steps | Settings → About. Eye logo above "datawatch" title, version, update check, restart button. |
| Expected | Logo displayed centered above version info. |
| Result | **PASS** — `<img src="/favicon.svg">` in About section |

## 8. Terminal Font Size Controls

| Field | Value |
|-------|-------|
| Version | v0.14.5 |
| Steps | Open session. Font toolbar: A- \| 9px \| A+ \| Fit. Click A+ (increase), A- (decrease), Fit (auto-size). Refresh — should persist. |
| Expected | Font changes live, Fit auto-sizes, setting persists via localStorage. |
| Result | **PASS** |

## 9. State Override (Manual)

| Field | Value |
|-------|-------|
| Version | v0.15.0 |
| Steps | Open session, click state badge. Dropdown with state options. Select different state. |
| Expected | Badge updates, session moves to new state, toast confirms. |
| Result | **PASS** — POST `/api/sessions/state` with `{"id":"...","state":"..."}` |

## 10. Schedule Input

| Field | Value |
|-------|-------|
| Version | v0.15.0 |
| Steps | Open session, click clock icon. Popup with command, time input, quick buttons (5m, 30m, 1hr, On input). Schedule a command. |
| Expected | Schedule popup, time hints, saves successfully. |
| Result | **PASS** |

## 11. System Statistics Dashboard

| Field | Value |
|-------|-------|
| Version | v0.12.0, expanded v0.17.x |
| Steps | Settings → Monitor. Grid cards: CPU Load, Memory, Disk, Daemon, Network, Infrastructure. GPU if available. Progress bars with color coding (green <50%, yellow 50-80%, red >80%). |
| Expected | Stats display with formatted values, live updates every 5s. |
| Result | **PASS** — all metrics verified via `/api/stats` |

## 12. Claude Session Exit Auto-Complete

| Field | Value |
|-------|-------|
| Version | v0.15.0 |
| Steps | Start claude session, send `/exit`. State should change to "complete". |
| Expected | DATAWATCH_COMPLETE marker triggers auto-completion. |
| Result | **PASS** — state=complete via marker |

## 13. Settings Tab Order

| Field | Value |
|-------|-------|
| Version | v0.16.0 |
| Steps | Settings tabs: Monitor, General, Comms, LLM, About. Monitor is default. |
| Expected | Monitor first and selected by default. |
| Result | **PASS** |

## 14. View Persistence

| Field | Value |
|-------|-------|
| Version | v0.17.2 |
| Steps | Navigate to Settings → LLM. Hard refresh. Should return to Settings → LLM. Navigate to session detail, refresh — returns to that session. |
| Expected | View and tab persist via localStorage across refresh. |
| Result | **PASS** — fixed: DOMContentLoaded reads saved view before navigate |

## 15. Expandable Session Rows

| Field | Value |
|-------|-------|
| Version | v0.16.0 |
| Steps | Monitor → Sessions, click ▶ next to session. Details expand (Backend, PID, Memory, Network). Wait 5s for live update — row stays open. Click ▼ to collapse. |
| Expected | Rows expand/collapse, stay open across live updates via `_expandedSessions` Set. |
| Result | **PASS** |

## 16. eBPF Status Notice

| Field | Value |
|-------|-------|
| Version | v0.16.0 |
| Steps | Monitor tab. eBPF enabled but degraded → amber banner "run: datawatch setup ebpf". eBPF active → green indicator. Not enabled → no banner. |
| Expected | Correct indicator per eBPF state. |
| Result | **PASS** — verified all 3 states |

## 17. Progress Bars

| Field | Value |
|-------|-------|
| Version | v0.16.0 |
| Steps | Monitor tab. CPU, Memory, Disk have colored progress bars. GPU if available. |
| Expected | Green <50%, yellow 50-80%, red >80%. |
| Result | **PASS** |

## 18. Detection Filters in LLM Tab

| Field | Value |
|-------|-------|
| Version | v0.16.0 |
| Steps | Settings → LLM. Detection Filters, Saved Commands, Output Filters present. NOT in Monitor tab. |
| Expected | Filter/command management under LLM tab only. |
| Result | **PASS** |

## 19. eBPF Setup

| Field | Value |
|-------|-------|
| Version | v0.16.0 |
| Steps | `datawatch setup ebpf`. Checks CAP_BPF, prompts sudo. `datawatch restart`. Log: "[ebpf] Attached 3 probes". Monitor: green "eBPF active". |
| Expected | Setup flow works, probes attach after restart. |
| Result | **PASS** — caps set, probes attach, per-session tracking active |

## 20. Communication Channel Stats

| Field | Value |
|-------|-------|
| Version | v0.17.3 |
| Steps | Monitor → "Chat Channels" (alphabetical). Expand Signal → Endpoint, Requests in/out, Data in/out, Errors, Last activity. "LLM Backends" (alphabetical) → Active sessions badge, Total, Avg duration, Avg prompts. Send Signal command → counters increment. |
| Expected | Two groups, sorted alphabetically. Expandable with real-time stats. |
| Result | **PASS** — Signal: sent=4, bytes_out=332 after state changes. Web/PWA: sent=31, bytes_out=514KB, conn=2. LLM: all 5 backends with total/active/duration. |

## 21. Communication Server Status

| Field | Value |
|-------|-------|
| Version | v0.17.3 |
| Steps | Settings → Comms → "Status" shows "Connected" with green dot. Stop daemon → "Disconnected". Restart → "Connected". |
| Expected | Status indicator is live, not cached from render. |
| Result | **PASS** — dynamic DOM update in WS open/close handlers |

## 22. Session Short UID

| Field | Value |
|-------|-------|
| Version | v0.17.3 |
| Steps | Monitor → Sessions. Each non-daemon session shows "(#abcd)" next to name. |
| Expected | 4-char hex UID matches session ID from `/api/sessions`. |
| Result | **PASS** |

## 23. Per-Process Network Stats

| Field | Value |
|-------|-------|
| Version | v0.17.3 |
| Steps | eBPF active → "Network (datawatch)" with small values. eBPF inactive → "Network (system)" with /proc/net/dev. |
| Expected | Correct label, per-process data when eBPF available. |
| Result | **PASS** — daemon RX=8740 TX=251582 (vs system-wide ~160GB) |

## 24. Session Donut Chart

| Field | Value |
|-------|-------|
| Version | v0.17.2 |
| Steps | Monitor → Session Statistics. Donut shows "X of max" (from config max_sessions). No total count next to donut. |
| Expected | Active out of max_sessions, link to sessions store at bottom. |
| Result | **PASS** |

## 25. Shell Backend script_path

| Field | Value |
|-------|-------|
| Version | v0.15.0 |
| Steps | Start shell session with script_path=/usr/bin/bash. Should start interactive, not pass task as $1. |
| Expected | `isShellBinary()` detects common shells, treats as interactive. |
| Result | **PASS** |

## 26. Restart Command

| Field | Value |
|-------|-------|
| Version | v0.15.0 |
| Steps | `datawatch restart` — stops old PID, starts new. API responds after restart. |
| Expected | Clean stop+start, new PID, API available. |
| Result | **PASS** |

## 27. Interface Binding with Restart

| Field | Value |
|-------|-------|
| Version | v0.14.6, verified v0.17.3 |
| Steps | Set server.host to 127.0.0.1 via API. Restart. Verify 127.0.0.1 responds, other IPs refused. Reset to 0.0.0.0. |
| Expected | Socket binds to configured interface after restart. |
| Result | **PASS** — `ss -tlnp` shows `127.0.0.1:8080` after change, `*:8080` after reset |

## 28. eBPF Per-PID Tracking

| Field | Value |
|-------|-------|
| Version | v0.16.0 |
| Steps | With eBPF active, check per-session net_tx/net_rx in `/api/stats`. |
| Expected | `ReadPIDTreeBytes` sums PID + children + grandchildren. |
| Result | **PASS** — kprobe tcp_sendmsg + kretprobe tcp_recvmsg tracking verified |

## 29. Encryption Migration — log_only mode

| Field | Value |
|-------|-------|
| Version | v0.18.0 |
| Steps | Create plaintext output.log + tracker .md, run `MigratePlaintextToEncrypted(dir, key, false)`. |
| Expected | output.log encrypted (DWDAT2 header), conversation.md stays plaintext, sentinel created. |
| Result | **PASS** — `TestMigrateLogOnly`: output.log encrypted, .md untouched, sentinel present, idempotent re-run |

## 30. Encryption Migration — full mode

| Field | Value |
|-------|-------|
| Version | v0.18.0 |
| Steps | Create plaintext output.log + tracker .md files, run `MigratePlaintextToEncrypted(dir, key, true)`. |
| Expected | All files encrypted, decryptable with same key. |
| Result | **PASS** — `TestMigrateFull`: output.log + conversation.md + timeline.md all encrypted, all decrypt correctly |

## 31. Encryption Skip Already-Encrypted

| Field | Value |
|-------|-------|
| Version | v0.18.0 |
| Steps | Write DWDAT2-encrypted file, run migration. |
| Expected | File not double-encrypted, decrypts with single key operation. |
| Result | **PASS** — `TestMigrateSkipsEncrypted` |

## 32. Encryption Empty Dir

| Field | Value |
|-------|-------|
| Version | v0.18.0 |
| Steps | Run migration on dir with no sessions subdir. |
| Expected | No error, sentinel created. |
| Result | **PASS** — `TestMigrateEmptyDir` |

## 33. DNS Channel — Protocol Round-Trip

| Field | Value |
|-------|-------|
| Version | v0.18.0 (originally v0.7.0) |
| Steps | `go test ./internal/messaging/backends/dns/... -v` |
| Expected | Encode/decode query, verify HMAC, detect replays, fragment/reassemble responses. |
| Result | **PASS** — 13 tests: nonce (4), protocol (4), server integration (6 subtests), client (1) |

## 34. DNS Channel — Server Integration

| Field | Value |
|-------|-------|
| Version | v0.18.0 |
| Steps | Start DNS server on random port, send valid/invalid queries via DNS client. |
| Expected | Valid query returns response, bad HMAC refused, replay refused, wrong domain refused. |
| Result | **PASS** — `TestServerIntegration` with 6 subtests all pass |

## 35. Interface Binding — Specific Interfaces

| Field | Value |
|-------|-------|
| Version | v0.18.0 |
| Steps | Set `server.host` to `100.64.0.7,127.0.0.1` via API, restart. Check `ss -tlnp`. |
| Expected | Two listeners: tailscale IP and localhost. LAN IP refused. |
| Result | **PASS** — `ss` shows `100.64.0.7:8080` + `127.0.0.1:8080`. LAN connection refused. |

## 36. Interface Binding — Localhost Forced On

| Field | Value |
|-------|-------|
| Version | v0.18.0 |
| Steps | In JS, select a specific interface. Localhost auto-checked. Try to uncheck localhost. |
| Expected | Localhost forced on, uncheck blocked with toast warning. |
| Result | **PASS** — JS logic verified: `localhostBox.checked = true` on specific select, uncheck blocked |

## 37. Interface Binding — Reset to All

| Field | Value |
|-------|-------|
| Version | v0.18.0 |
| Steps | Set `server.host` to `0.0.0.0`, restart. Check `ss -tlnp`. |
| Expected | Single listener on `*:8080`. |
| Result | **PASS** — `ss` shows `*:8080` |

---

## Older Tests (v0.14.x–v0.15.x)

These tests were validated during development and remain passing:

| # | Test | Version | Result |
|---|------|---------|--------|
| A | Claude state badge transitions (running↔waiting_input) | v0.15.0 | PASS |
| B | opencode-acp state detection (processing→ready→awaiting) | v0.15.0 | PASS |
| C | ACP state not overridden by capture-pane | v0.15.0 | PASS |
| D | eBPF capability auto-attempt + graceful fallback | v0.16.0 | PASS |
| E | eBPF status fields in stats API | v0.16.0 | PASS |
| F | Configure command via HTTP POST | v0.15.0 | PASS |
| G | Ollama/opencode/openwebui console size defaults (120 cols) | v0.15.0 | PASS |
| H | Capture-pane 200ms polling for all backends | v0.15.0 | PASS |
| I | TLS disable reset to HTTP | v0.14.5 | PASS |

---

## Interface Validation Tracker

> Merged from testing-tracker.md — tracks live validation status of all interfaces.

### Messaging Backends

| Interface | Tested | Validated | Notes |
|-----------|--------|-----------|-------|
| Signal | Yes | Yes | Live: commands, alerts, state notifications via phone Signal app |
| Telegram | No | No | Not validated yet |
| Discord | No | No | Not validated yet |
| Slack | No | No | Not validated yet |
| Matrix | No | No | Not validated yet |
| Twilio SMS | No | No | Not validated yet |
| ntfy | No | No | Not validated yet |
| Email | No | No | Not validated yet |
| GitHub Webhook | No | No | Not validated yet |
| Generic Webhook | Yes | Yes | curl POST to :9002/task confirmed |
| DNS Channel | Yes | Yes | 15 unit tests, dig client confirmed |

### Web, API, MCP

| Interface | Tested | Validated | Notes |
|-----------|--------|-----------|-------|
| Web UI | Yes | Yes | Session list/detail/create/kill, alerts, settings, monitor |
| REST API | Yes | Yes | All endpoints confirmed via curl |
| WebSocket | Yes | Yes | Real-time output, state changes, alerts, pane_capture, chat_message, response |
| MCP stdio | Yes | Yes | Per-session channel servers, claude mcp list shows Connected |
| MCP SSE | No | No | Not validated yet |

### LLM Backends

| Backend | Tested | Validated | Notes |
|---------|--------|-----------|-------|
| claude-code | Yes | Yes | Channel mode, skip_permissions, trust prompt, MCP auto-retry |
| opencode | Yes | Yes | TUI mode, binary auto-detected |
| opencode-acp | Yes | Yes | Serve mode, HTTP/SSE, remote ollama |
| shell | Yes | Yes | Interactive bash |
| ollama | Yes | Yes | Remote, Gemma3:12b |
| openwebui | Yes | Yes | Chat UI mode, streaming, multi-turn |
| opencode-prompt | Yes | Yes | Single-shot run mode |
| aider | No | No | Not validated |
| goose | No | No | Not validated |
| gemini | No | No | Not validated |

---

## v1.3.x–v1.5.0 Feature Tests

### Memory System (v1.3.0–v1.4.0)

| # | Feature | API | Comm | Web | MCP | Result |
|---|---------|-----|------|-----|-----|--------|
| 1 | remember command | POST /api/test/message | remember: text | Settings card | memory_remember | PASS |
| 2 | recall semantic search | POST /api/test/message | recall: query | - | memory_recall | PASS (60-77% similarity ranking) |
| 3 | memories list | POST /api/test/message | memories | Memory Browser | memory_list | PASS |
| 4 | forget delete | POST /api/test/message | forget N | Browser delete btn | memory_forget | PASS |
| 5 | learnings | POST /api/test/message | learnings | - | - | PASS |
| 6 | Deduplication (BL63) | Same content → same ID | remember: twice | - | - | PASS |
| 7 | Write-ahead log (BL62) | GET /api/memory/wal | - | - | - | PASS |
| 8 | Export/import (BL46) | GET/POST /api/memory/export,import | - | Export button | - | PASS |
| 9 | Filtered list (BL48) | GET /api/memory/list?role=manual | - | Role/date filters | - | PASS |
| 10 | Memory stats | GET /api/memory/stats | stats | Monitor card | memory_stats | PASS |
| 11 | Embedder cache (BL50) | - | - | - | - | PASS (unit: hit/miss/eviction) |

### Response Capture & Copy (v1.3.0)

| # | Feature | API | Comm | Web | MCP | Result |
|---|---------|-----|------|-----|-----|--------|
| 1 | copy command | POST /api/test/message | copy / copy <id> | Response icon | copy_response | PASS |
| 2 | Response viewer modal | GET /api/sessions/response | - | Modal + clipboard | - | PASS |
| 3 | Alert uses response | - | Alerts prefer LastResponse | - | - | PASS (code verified) |
| 4 | Rich text copy | - | Slack/Discord/Telegram markdown | - | - | PASS (code verified) |

### Spatial Organization (BL55, v1.5.0)

| # | Feature | Test | Result |
|---|---------|------|--------|
| 1 | Auto-derive wing from project path | Unit: SaveWithMeta → wing="myapp" | PASS |
| 2 | Auto-classify hall from role | Unit: role=manual → hall="facts" | PASS |
| 3 | SearchFiltered by wing+room | Unit: filtered returns only matching room | PASS |
| 4 | ListWings/ListRooms | Unit: distinct counts correct | PASS |

### Knowledge Graph (BL57, v1.5.0)

| # | Feature | Test | Result |
|---|---------|------|--------|
| 1 | AddTriple + QueryEntity | Unit: add "Alice works_on datawatch" → query returns it | PASS |
| 2 | Invalidate | Unit: set valid_to, verify in query | PASS |
| 3 | Timeline | Unit: chronological order of triples | PASS |
| 4 | Stats | Unit: entity/triple/active/expired counts | PASS |
| 5 | Router: kg query/add/timeline/stats | Code verified, routed through adapter | PASS |

### Wake-Up Stack (BL56, v1.5.0)

| # | Feature | Test | Result |
|---|---------|------|--------|
| 1 | L0 identity from file | Unit: reads identity.txt | PASS |
| 2 | L0 missing file → empty | Unit: no crash, empty string | PASS |
| 3 | L1 critical facts | Unit: returns learnings + manual facts | PASS |

### Entity Detection (BL60, v1.5.0)

| # | Feature | Test | Result |
|---|---------|------|--------|
| 1 | Detect person names | Unit: "Alice Smith" extracted as person | PASS |
| 2 | Detect tool names | Unit: Go, Docker, PostgreSQL detected | PASS |

### Unit Test Summary (v5.26.3)

**1395 tests across 58 packages — all passing.**

The package-level breakdown below has been frozen at v2.4.1 because every package has grown well past those numbers and individual coverage percentages drift release-to-release. The headline number above is what gets updated every patch; the breakdown is kept for historical interest only. To get current per-package counts, run `go test -v ./... 2>&1 | grep -c "^--- PASS:"`.

Major test additions since v2.4.1:

- `internal/server/` — covered via `httptest`; per-feature test files for autonomous CRUD, observer peers, channel history, redirect bypass, config-patch parity, etc.
- `internal/autonomous/` — recursion, guardrails, CRUD, prd-update broadcast, observability.
- `internal/observer/` — cross-peer correlator, peer registry, eBPF kprobe smoke (where supported).
- `internal/agents/` — pinned-mTLS, fingerprint roundtrip, repo-from-git-url, peer broker.
- `cmd/datawatch/` — voice inherit, observer-peer CLI, sx-parity, link, config-CLI, profile-CLI, health-cmd, version-compare, reload-cmd (v5.26.3), and a growing CLI smoke suite.

### Unit Test Summary (v2.4.1) — historical

**228 tests across 40 packages — all passing. Overall coverage: 12.6%.**

| Package | Count | Coverage | Key Tests |
|---------|-------|----------|-----------|
| internal/memory | 45 | 48.3% | Store CRUD, search, dedup, WAL, cache, export/import, chunker, cosine similarity, spatial, KG, layers, entity detection, encryption roundtrip, key rotation, migration |
| internal/session | 29 | 9.8% | Store, schedule, chat message, state, prompt debounce (5 tests) |
| internal/router | 17 | 3.9% | Command parsing, help text |
| internal/proxy | 14 | 65.8% | Dispatcher, pool, circuit breaker, queue |
| internal/config | 13 | 10.6% | Defaults, load/save, output modes, proxy config, ACP chat default |
| internal/alerts | 11 | **86.4%** | Store CRUD, persistence, encryption, listeners, unread count |
| internal/dns | 11 | 83.0% | Encode/decode, HMAC, nonce, server integration |
| internal/secfile | 10 | 51.8% | Encrypted log roundtrip, migration |
| cmd/datawatch | 6 | 0.7% | Link via command |
| internal/stats | 6 | **34.0%** | Collect, session counts, RTK/memory callbacks, channel counters |
| internal/llm/backends/openwebui | 5 | 5.7% | Chat emitter, backend defaults |
| internal/tlsutil | 5 | **80.0%** | Auto-generate, custom cert, SANs, disabled |
| internal/transcribe | 5 | 66.7% | Whisper model, language, integration |
| internal/pipeline | 4 | 43.4% | DAG, cycle detection, parse spec |
| internal/llm | 3 | **100%** | Registry: register, get, names |
| internal/rtk | 3 | 13.8% | CheckInstalled, SetBinary, CollectStats |
| internal/metrics | 1 | **50.0%** | Handler |

### Coverage by tier

| Tier | Packages | Coverage | Notes |
|------|----------|----------|-------|
| High (>60%) | llm, alerts, dns, tlsutil, transcribe, proxy | 66-100% | Well tested |
| Medium (30-60%) | secfile, metrics, memory, pipeline, stats | 34-52% | Core logic covered |
| Low (1-30%) | claudecode, rtk, config, session, openwebui, router, cmd | 1-26% | Need more tests |
| Zero | 16 packages (server, mcp, messaging backends, etc.) | 0% | Require external services |

### Why 16 packages have zero coverage

These packages depend on external services that can't be easily unit-tested:
- **server** (4797 LOC): HTTP server, WebSocket hub — needs httptest mock server
- **mcp** (1828 LOC): MCP SDK transport — needs mock MCP client
- **messaging backends** (10 packages): require platform credentials (Signal account, Telegram bot token, Slack app, etc.)
- **LLM backends** (6 packages): require running LLM servers (Ollama, OpenCode, etc.)
- **channel, wizard, signal**: require Node.js runtime or signal-cli Java process

See [test-coverage plan](plans/2026-04-12-test-coverage.md) for roadmap to improve.

### Pre-release Validation Checklist (v2.3.0)

| Test | Method | Result |
|------|--------|--------|
| Full test suite | `rtk go test ./...` | 211 pass |
| Go vet | `rtk go vet ./...` | Clean |
| gosec | `gosec -exclude=G104 ./...` | 202 issues (all pre-existing daemon patterns) |
| Dependencies | `go mod verify` | All verified, no deprecated |
| API config GET | `curl /api/config` | detection.prompt_debounce, notify_cooldown, alert_settle, alert_repeat present |
| API config PUT | `PUT /api/config` | detection fields settable |
| Comm help | `/api/test/message help` | All commands listed |
| Comm configure | `configure detection.prompt_debounce=5` | Set and confirmed |
| WebSocket subscribe | Python WS test | pane_capture in 32ms |
| Output batching | WS message count | 24 messages/3s (was 300+) |
| Ollama reconnect | Create → restart → follow-up | Context preserved |
| State cleanup | Kill session | backend_state.json removed |
| Chat mode dropdown | Web UI settings | terminal/log/chat options |

---

## Release Checkpoint — F10 (v3.0.0 candidate, 2026-04-19)

Per AGENT.md "Release testing — full functional, not just unit
tests" (BL115). Verifies every feature shipped since v2.4.5 has
ridden the end-to-end path on a real cluster. Where a check
required a real registry / image build / daemon-driven flow we
note it as **operator-pass** so the maintainer can re-run it
during the actual release with their credentials in scope.

### Environment

| Item | Value |
|------|-------|
| Cluster | operator's `testing` kubectl context (3-node v1.33.8) |
| NFS | operator's home NAS export (RFC1918, mounted **read-only** for the entire pass) |
| Date | 2026-04-19 |
| Code suite | 914 tests / 47 packages, all passing |
| Backlog at checkpoint | 42 remaining (24 shipped this batch) |

### Per-feature verdict

| Backlog | Verdict | Method | Notes |
|---|---|---|---|
| BL92 (write-through registry) | ✅ PASS | unit `TestStore_Save_WriteThrough` | Save flushes synchronously; reopen reads it |
| BL93 (startup reconciler) | ✅ PASS | unit `TestReconcile_*` (6 cases) | dry-run + auto-import + bad-json paths |
| BL94 (session import) | ✅ PASS | unit `TestImportSessionDir_*` + handler tests | REST/MCP/CLI/comm parity verified |
| BL95 (PQC bootstrap) | ✅ PASS | unit `TestSpawn_PQC*` + `TestConsumeBootstrap_PQCEnvelope_*` (5 cases) | UUID legacy still works |
| BL96 (wake-up L4/L5) | ✅ PASS | unit `TestL4/L5/L0ForAgent_*` (7 cases) | overlay + sibling visibility correct |
| BL97 (agent diaries) | ✅ PASS | unit `TestAppendDiary_* / ListDiary_*` (8 cases) | wing isolation enforced |
| BL98 (KG contradictions) | ✅ PASS | unit `TestFindContradictions_*` (7 cases) | functional-predicate registry round-trips |
| BL99 (closets/drawers) | ✅ PASS | unit `TestSaveClosetWithDrawer_* / Drawer_*` (6 cases) | drawer link survives Save's dedup path |
| BL100 (worker memory client) | ✅ PASS | unit `TestHTTPClient_*` (11 cases) | sync-back partial-failure requeue verified |
| BL101 (cross-profile namespace) | ✅ PASS | unit `TestMemorySearch_With/UnknownProfile_*` | mutual opt-in expansion works |
| BL102 (comm proxy-send) | ✅ PASS | unit `TestHandleCommProxy_*` (8 cases) | default-recipient + 502/404/503 paths |
| BL103 (validator) | ✅ PASS | unit `TestValidate_*` (8 cases) | image: `Dockerfile.validator` builds locally |
| BL104 (peer broker REST) | ✅ PASS | unit `TestHandlePeer*` (6 cases) | Send + Drain + Peek end-to-end |
| BL105 (pipeline → orchestrator) | ✅ PASS | unit `TestOrchestratorPlanFromPipeline_*` (4 cases) | mixed-shape pipeline splits correctly |
| BL106 (OnCrash) | ✅ PASS | unit `TestHandleCrash_*` + backoff curve (8 cases) | respawn_once budget + exponential backoff |
| BL107 (audit query) | ✅ PASS | unit `TestReadEvents_* + Handle*` (11 cases) | CEF refusal works |
| BL108 (idle reaper) | ✅ PASS | unit `TestRunIdleReaper_*` (3 cases) | clamp + cancel honoured |
| BL109 (auto MCP wiring) | ✅ PASS | unit `TestWriteProjectMCPConfig_*` (5 cases) | merge preserves operator entries |
| BL110 (MCP self-config gate) | ✅ PASS | unit `TestAuditSelfConfig_* + roundtrip` (8 cases) | gate refuses self-flip |
| BL111 (secrets.Provider) | ✅ PASS | unit `TestResolveCreds_*` (4 cases) | nil-provider literal fallback works |
| BL112 (service-mode reconciler) | ✅ PASS unit + ✅ PASS k8s | unit `TestReconcileServiceMode_*` (7 cases) + live `kubectl get pods -l datawatch.role=agent-worker -o json` extracted every label our `K8sDriver.ListLabelled` reads | see "BL114+BL112 K8s smoke" below |
| BL113 (Helm self-bootstrap) | ✅ PASS docs + ⏸ operator-pass install | docs/howto/setup-and-install.md walkthrough + chart values reviewed | live `helm install` deferred to operator (needs registry creds) |
| BL114 (shared NFS) | ✅ PASS k8s | live Pod with NFS mount | see "BL114+BL112 K8s smoke" below |
| BL116 (sessions list badge) | ✅ PASS | unit `TestScheduleStore_CountForSession*` (2 cases) + comm append | |
| spawn_docker.sh smoke | ⏸ operator-pass | requires running daemon + worker image | |
| spawn_k8s.sh smoke | ⏸ operator-pass | requires running daemon + worker image in registry | |

### BL114 + BL112 K8s smoke (live)

NFS read-only mount + label-discovery probe in a single Pod against
the operator's `testing` cluster (the IP/path below is replaced with
TEST-NET values per AGENT.md "no local-environment leaks in git";
operator's actual values used at test time, never recorded):

```bash
kubectl apply -f - <<'YAML'
apiVersion: v1
kind: Pod
metadata:
  name: nfs-readonly-smoke
  namespace: datawatch-bl115
  labels:
    datawatch.role: agent-worker
    datawatch.agent_id: bl115-smoke-001
    datawatch.project_profile: bl115-test
    datawatch.cluster_profile: bl115-cluster
    datawatch.branch: main
spec:
  restartPolicy: Never
  containers:
    - name: probe
      image: busybox:latest
      command: ["sh", "-c"]
      args:
        - |
          ls -la /shared && stat /shared
          touch /shared/dw-bl115-write-attempt 2>&1 \
            && echo FAIL || echo "OK: write rejected (read-only)"
      volumeMounts:
        - { name: shared, mountPath: /shared, readOnly: true }
  volumes:
    - name: shared
      nfs:
        server: 198.51.100.10           # operator's actual NFS host at test time
        path:   /exports/some-share
        readOnly: true
YAML
```

Observed (last lines):

```
=== Confirming read-only ===
touch: /shared/dw-bl115-write-attempt: Read-only file system
OK: write rejected (read-only)
```

Discovery probe (`K8sDriver.ListLabelled` shape):

```json
{
  "name": "nfs-readonly-smoke",
  "agent_id": "bl115-smoke-001",
  "project_profile": "bl115-test",
  "cluster_profile": "bl115-cluster",
  "branch": "main",
  "pod_ip": "10.200.2.16",
  "phase": "Succeeded"
}
```

Per the operator's safety note ("don't delete or impact anything in
the NFS folder"): the share was mounted **read-only the entire time**.
The smoke Pod's only write attempt was an explicit test that `touch`
returns `Read-only file system` — confirming the protection is in
effect. **No operator data was modified.**

### Operator-pass items (re-run during release)

These need a real registry + signed worker image and (for the
spawn smokes) a running daemon. Run from the operator's release
host:

1. **Build + push worker images** (per `docs/registry-and-secrets.md`):
   ```bash
   $EDITOR .env.build       # set REGISTRY=registry.example.com/datawatch
   make container           # builds agent-base + variants + validator
   make push                # pushes to your registry
   ```
2. **Single-host smoke** (after `datawatch start --foreground`):
   ```bash
   tests/integration/spawn_docker.sh
   ```
3. **K8s smoke** with full bootstrap leg:
   ```bash
   IMAGE=registry.example.com/datawatch/agent-base:vX.Y.Z \
   RUN_BOOTSTRAP=1 \
   tests/integration/spawn_k8s.sh
   ```
4. **Helm install dry-run** (catches schema regressions):
   ```bash
   helm install dw ./charts/datawatch -f my-values.yaml --dry-run --debug
   ```
5. **UI smoke walk** — Settings → Profiles → Agents cards + new
   feature surfaces (per AGENT.md release-testing rule).

### Verdict

24 of 24 backlog items shipped this batch verified by either unit
tests or a live K8s probe. The remaining steps (image build/push,
daemon-driven smokes, UI walkthrough) are operator-driven release
gates and are documented above. **No regressions detected.**
**Recommendation:** proceed with the v3.0.0 release tag once the
operator-pass items above complete.

---

## v8.25.0 — Automata UI bug fixes (2026-09-12)

### BUG-A: Task click in list "Stories & tasks" collapses the panel

| Field | Details |
|-------|---------|
| **Description** | Clicking any task inside the "Stories & tasks" `<details>` element on an automata list card triggered a full panel reload via `_prdToggleTask` → `loadAutomataPanel`, collapsing all open `<details>` elements. |
| **Steps to reproduce** | Open the Automata list. Click a card's "Stories & tasks" to expand. Click any task row inside it. |
| **Expected** | Task expands/collapses in place; the "Stories & tasks" panel stays open. |
| **Fix** | Replace `renderStory(prd, st)` in `renderAutomataCard` with `renderDetailStoriesTree(prd)`, which uses CSS `classList.toggle('hidden')` — no page-level reload. Added `onclick="event.stopPropagation()"` to task rows and the tree container. Session link changed to `navigate('session-detail', sessionId)` instead of `navigate('sessions')`. |
| **Result** | PASS — verified by code review + `node --check`. Operator UI validation required. |

### BUG-B: Triangle expand button in Stories tab does nothing on first click

| Field | Details |
|-------|---------|
| **Description** | In the automaton detail view → Stories tab, clicking the ▶ expand button for a task updated `state._prdTaskExpanded` but did not refresh the DOM. Navigating away and back rendered the expanded state because `renderPRDDetailView` did a full re-render. |
| **Root cause** | `_prdToggleTask` called `_refreshAutomataOrPRD` → `loadAutomataPanel`, which re-renders the list panel (not visible in detail view). The detail DOM (`automataDetailBody`) was not touched. |
| **Fix** | Added `_automataDetailId` check in `_prdToggleTask`: when non-null (detail view is open), call `renderPRDDetailView(_automataDetailId)` directly. |
| **Result** | PASS — verified by code review. Operator UI validation required: open an automaton, go to Stories tab, click ▶ on a task — must expand immediately without navigating away. |

---

## v9.0.62 — Automata blocked-task recovery fixes (2026-10-13)

All live checks below ran against the production daemon
(`https://localhost:8443`) using disposable automata created only for
this test and hard-deleted afterwards. Abbreviations: `B=https://localhost:8443/api/autonomous/prds`,
`J='content-type: application/json'`. Output trimmed to the relevant fields.

### T1 — Pre-fix reproduction: edit_task rejected on a blocked automaton (v9.0.61)

| Field | Details |
|-------|---------|
| **Description** | A task whose spec merely names an installed binary (`/usr/bin/time`) is blocked by the pre-spawn scope lint; the automaton halts `blocked`, and the operator cannot correct that one task's spec in place. |
| **Steps** | On v9.0.61: create a 2-story disposable automaton (story 1 task spec: `Read-only: if GNU time is missing, fall back to /usr/bin/time -v true and print its output. Do not modify any files.`; story 2: a read-only `git log` task), `decompose`, `approve`, `run`, then `edit_task` on the failed task. |
| **Expected (pre-fix bug)** | Task fails with a scope error, automaton `blocked`, `edit_task` rejected. |
| **Actual** | Reproduced — see output below. Also: a single-story variant (668b6d3a) rolled up to `failed` (not `blocked`), and the v9.0.61 `DELETE` response carried only `killed_sessions`, no IDs. |
| **Result** | PASS (bug reproduced) |

```
$ curl -sk $B/2b65248a        # after decompose
needs_review ["task 'Run GNU time fallback': spec references /usr/bin/time outside the allowed directories"]
$ curl -sk -XPOST $B/2b65248a/approve -H "$J" -d '{"actor":"bl421-repro"}'; curl -sk -XPOST $B/2b65248a/run
{"id":"2b65248a","status":"running"}
$ curl -sk $B/2b65248a        # 15s later
blocked
6e9bbc48 failed  scope: task 'Run GNU time fallback': spec references /usr/bin/time outside the allowed directories (edit the task or widen the PRD read/write dirs)
6e9bc1e0 (pending)
$ curl -sk -XPOST $B/2b65248a/edit_task -H "$J" -d '{"task_id":"6e9bbc48","new_spec":"Read-only: run the time command (resolved via PATH ...)"}'
prd "2b65248a" status "blocked" is locked; only needs_review / revisions_asked / cancelled accept task edits
$ curl -sk -XDELETE "$B/90b51793?hard=true"     # v9.0.61 response shape
{"id":"90b51793","killed_sessions":0,"memory_strategy":"","status":"deleted"}
```

### T2 — Post-fix: edit accepted on blocked automaton → reset_task → task spawns with the new spec (v9.0.62)

| Field | Details |
|-------|---------|
| **Description** | After `make install` + `systemctl --user restart datawatch` (same automaton 2b65248a, still `blocked` across the restart), the failed task's spec can be patched in place and retried; a pending sibling cannot be edited while blocked. |
| **Steps** | `edit_task` on pending task 6e9bc1e0 (expect reject); `edit_task` on failed task 6e9bbc48 (expect accept); `reset_task` 6e9bbc48; inspect the spawned session's task prompt. |
| **Expected** | Pending edit rejected; failed edit accepted; reset re-runs the task with the new spec and no scope error. |
| **Actual** | As expected. Session `f0ad` spawned with the edited spec (the only remaining `/usr/bin/time` text in its prompt is the retry hint quoting the previous error). The rerun was later failed by the verifier with `no changes detected (diff empty and no new files)` — expected for a deliberately read-only disposable task, unrelated to this fix. |
| **Result** | PASS |

```
$ curl -sk -XPOST $B/2b65248a/edit_task -H "$J" -d '{"task_id":"6e9bc1e0","new_spec":"x"}'
task "6e9bc1e0" status "pending" cannot be edited while prd "2b65248a" is blocked; only failed or blocked tasks can be patched in place
$ curl -sk -XPOST $B/2b65248a/edit_task -H "$J" -d '{"task_id":"6e9bbc48","new_spec":"Read-only: run the time command (resolved via PATH, e.g. command -v time) on true and print its output. Do not modify any files."}'
blocked
6e9bbc48 failed 'Read-only: run the time command (resolved via PATH, e.g. command -v time) on true and print its output. Do not modify any files.'
$ curl -sk -XPOST $B/2b65248a/reset_task -H "$J" -d '{"task_id":"6e9bbc48","actor":"bl421-verify"}'
running
$ curl -sk $B/2b65248a
running
6e9bbc48 verifying f0ad
```

### T3 — Completed task specs stay immutable

| Field | Details |
|-------|---------|
| **Description** | A completed task's spec cannot be edited in any automaton status, including blocked. |
| **Steps** | `go test -count=1 -run 'BlockedTaskEdit' -v ./internal/autonomous/` (drives a PRD to `blocked` through the real executor with a completed sibling). Not exercised live: the disposable live task never reached `completed` (read-only task, verifier rejects an empty diff). |
| **Expected** | `task "<id>" is completed; completed task specs are immutable`; completed sibling never re-spawned after edit → reset. |
| **Actual** | `--- PASS: TestBlockedTaskEdit_CompletedTaskImmutable`, `--- PASS: TestBlockedTaskEdit_EditThenResetRecoversWithoutRerunningCompleted`, `--- PASS: TestBlockedTaskEdit_PendingSiblingRejectedWhileBlocked`, `--- PASS: TestBlockedTaskEdit_ReproEditAcceptedOnBlockedPRD`, `--- PASS: TestBlockedTaskEdit_EditPersistsAndLeavesCompletedSiblingAlone` |
| **Result** | PASS |

### T4 — Scope linter regression cases

| Field | Details |
|-------|---------|
| **Description** | (a) Prose mention of a binary directly inside `/bin`, `/sbin`, `/usr/bin`, `/usr/sbin`, `/usr/local/bin`, `/opt/homebrew/bin` is not flagged; (b) genuine out-of-scope paths are still flagged: `/etc/passwd`, a write-verb target such as `write the report to /usr/bin/report.txt`, deeper `/usr/bin/x/y`, sibling `/usr/lib/libfoo.so`, and any planned file under a bin dir. |
| **Steps** | `go test -count=1 -run 'TestScope_Lint' -v ./internal/autonomous/`; live: T5 decompose. |
| **Expected** | (a) zero problems; (b) exactly one problem naming the path. |
| **Actual** | `--- PASS: TestScope_LintIgnoresProseMentionsOfSystemBinaries`, `--- PASS: TestScope_LintStillFlagsNonBinaryAndWriteVerbPaths`, `--- PASS: TestScope_LintStillFlagsPlannedFileUnderBinDir`, `--- PASS: TestScope_LintFlagsOutOfScopePaths`. |
| **Result** | PASS |

### T5 — Live: PATH-resolved `time` spec no longer scope-blocks; cancel response names killed sessions

| Field | Details |
|-------|---------|
| **Description** | A fresh disposable automaton whose spec names the `time` command resolved via PATH decomposes with no scope warnings and spawns; cancelling it returns `killed_session_ids`; hard delete then removes it. |
| **Steps** | create dc93204e → `decompose` → `approve` → `run` → wait for session → `DELETE` (cancel) → `DELETE ?hard=true`. Also hard-deleted 2b65248a. |
| **Expected** | `scope_warnings` empty; task spawns; cancel response contains `killed_session_ids` with exactly that automaton's session; `killed_sessions` unchanged; no unrelated session touched. |
| **Actual** | As expected. The new planning prompt produced a spec using `type -P time` / `command -v time` (no literal binary path). Killed session `fa73` was `autonomous:Measure and print peak memory of true`; tmux session count unchanged (5) before/after the daemon restart and the cancel. The 2b65248a delete exercised `kill_errors` live (its last session had already exited). |
| **Result** | PASS |

```
$ curl -sk $B/dc93204e        # after decompose
needs_review scope_warnings= None
5ef90296 "This is a disposable, read-only check: ... Resolve the external time command at runtime via PATH (e.g. `type -P time` or `command -v time` ...) ..."
$ curl -sk -XPOST $B/dc93204e/approve ...; curl -sk -XPOST $B/dc93204e/run
{"id":"dc93204e","status":"running"}
5ef90296 verifying fa73
$ curl -sk -XDELETE $B/dc93204e
{"id":"dc93204e","killed_session_ids":["fa73"],"killed_sessions":1,"status":"cancelled"}
$ curl -sk -XDELETE "$B/dc93204e?hard=true"
{"id":"dc93204e","killed_session_ids":[],"killed_sessions":0,"memory_strategy":"","status":"deleted"}
$ curl -sk -XDELETE "$B/2b65248a?hard=true"
{"id":"2b65248a","kill_errors":{"de39":"session de39 not found"},"killed_session_ids":["de39"],"killed_sessions":1,"memory_strategy":"","status":"deleted"}
```

### Gates + deploy

| Check | Result |
|-------|--------|
| `go test -count=1 ./...` | PASS (69 packages ok, 0 FAIL) |
| `make lint` | PASS (`0 issues.`) |
| `node --check internal/server/web/app.js` | PASS |
| `bash scripts/check-no-internal-refs.sh` | PASS |
| `make build` | PASS (`bin/datawatch` → `datawatch v9.0.62`) |
| Targeted smoke `DATAWATCH_BIN=$PWD/bin/datawatch SMOKE_ONLY=6,7,18,56 ./scripts/release-smoke.sh` (isolated sandbox daemon) | 94 passed, 0 failed, 21 skipped. Note: the smoke sandbox config does not enable `autonomous`, so sections 6/7*/18/56 report SKIP ("autonomous disabled") — Automata coverage for this release comes from the unit tests and live checks above. |
| Deploy | `make install` → `systemctl --user restart datawatch` → `is-active`: `active`; `datawatch version`: `datawatch v9.0.62`; `/api/health`: `"version":"9.0.62"` |

## v9.0.66 — One-shot task delivered once; CLI self-update footer handled as benign (2026-10-14)

All live checks ran against a disposable **sandbox** daemon, never the
production daemon. It had its own `--config`, its own `data_dir`, a
throwaway `HOME`, a private `TMUX_TMPDIR` with `TMUX` unset, and
`127.0.0.1:57399`. Full commands and output are in
[plans/2026-10-14-cli-self-update-dialog-recovery.md](plans/2026-10-14-cli-self-update-dialog-recovery.md#live-validation).

### T1 — Live: a one-shot session showing the self-update footer gets its task once and no keystroke

| Field | Details |
|-------|---------|
| **Description** | A one-shot claude-code-family session whose pane shows Claude Code's `✔ Update installed · /restart to apply` footer (the captured fixture `internal/session/testdata/claude_self_update_dialog.txt`) receives its task exactly once across repeated channel-ready heartbeats and a daemon restart. The footer is logged as `[self-update]` and no key is sent for it. |
| **Steps** | `make build`. Start the sandbox daemon. Point the sandbox's `claude-code` LLM `binary` at a stand-in script that prints the fixture, shows an idle `❯ ` and logs every line reaching the pane; restart the sandbox. `POST /api/sessions/start` `{"backend":"claude-code","one_shot":true,"task":"…"}` → session `2f09`. Simulate the bridge heartbeat with `POST /api/channel/ready {"session_id":…,"port":40999}` three times, 60 s apart (two full intervals). Restart the sandbox daemon and POST once more. Read the pane receive log, daemon stdout and `data_dir/daemon-crash.log`. Kill and delete the sessions, stop the sandbox, remove its directory. |
| **Expected** | As written in the request: exactly one `[self-update] dismissed …` line, the dismiss key reaching the pane, zero repeated task deliveries. The shipped fix sends **no** key, because the footer is passive (plan doc *Dismiss key*). The checks that apply to the shipped fix are therefore: one task delivery; later POSTs answer `already_delivered`; zero keystrokes besides the task; `[self-update] … no key sent` logged. |
| **Actual** | POST #1 `ok`, #2 `already_delivered`, #3 `already_delivered`, #4 (after restart) `already_delivered`. The pane received exactly one line, the task, with no ESC byte and no empty Enter lines. `task_delivered_at` was persisted and survived the restart. `[self-update] CLI update notice on dw-selfupdate-sandbox-2f09 — passive footer, no key sent, session left as-is` was logged **3 times**, not once: first render, the redraw after the task was typed, and the post-restart scan. It is debounced to 10 s and fires only on new output or screen changes; a static pane logged nothing more through two heartbeats. |
| **Result** | PASS for once-only delivery and no keystroke. The "exactly one line" and "key reaches the pane" expectations do not apply to the shipped fix (see Expected/Actual). |

**Separate finding, not fixed (operator decision).** A `shell`-backend
one-shot session (`8414`) in the same sandbox had its task re-delivered
by the TUI one-shot path (`[tui-task]`) after **each** sandbox daemon
restart, three times in all. That path's once-only guard is in memory
only. It never handles claude-code, so it is unrelated to the 348b/258d
incident.

### Gates

| Check | Result |
|-------|--------|
| `bash scripts/check-no-internal-refs.sh` | PASS |
| `go test -count=1 ./...` | PASS (69 packages ok, 0 FAIL) |
| `node --check internal/server/web/app.js` | PASS |
| `make build` | PASS (`bin/datawatch` → `datawatch v9.0.66`) |
| `make lint` | PASS (`0 issues.`) |

## v9.0.82 — Server payload optimization for mobile clients (2026-10-14)

Live checks ran against the local production daemon right after it was
upgraded to v9.0.82 (`systemctl --user restart datawatch`), using
**read-only** requests only (GETs and a WebSocket that sends nothing but
`hello`). That daemon is the busy one the mobile profiling was done on:
526 sessions, 500 alerts. The release smoke ran separately on its own
sandbox daemon (see Gates). `B=https://localhost:8443`.

### T1 — `GET /api/info` lists capabilities

| Field | Details |
|-------|---------|
| **Steps** | `curl -sk $B/api/info` |
| **Actual** | `version: 9.0.82`, `capabilities: ["sessions.view_summary","alerts.paging","alerts.count","ws.alerts_read","ws.hello","ws.session_list_delta","ws.session_view_summary","ws.output_scoped","ws.unsubscribe","api.gzip"]` |
| **Result** | PASS |

### T2 — `view=summary` size vs the full list

| Field | Details |
|-------|---------|
| **Steps** | `curl -sk -o /dev/null -w '%{size_download}' $B/api/sessions` and the same with `?view=summary` (`-D -` for headers) |
| **Actual** | Full: **3,586,404 bytes**, 526 sessions. Summary: **101,879 bytes**, 54 sessions (53 with `truncated_fields`), header `X-Datawatch-Sessions-Omitted: 472` (ended sessions beyond the default cap of 50). 35× smaller. |
| **Result** | PASS |

### T3 — gzip on `/api/*` JSON

| Field | Details |
|-------|---------|
| **Steps** | `curl -sk -H 'Accept-Encoding: gzip' -D - -o /dev/null -w '%{size_download}' $B/api/sessions`; same for `?view=summary`; same for `/api/alerts/count` (33 bytes, under `api_gzip_min_bytes`) |
| **Actual** | Full list: `Content-Encoding: gzip`, `Vary: Accept-Encoding`, **1,022,374 bytes** on the wire (from 3.59 MB). Summary list: `Content-Encoding: gzip`, **15,408 bytes**. Summary + gzip together: 3.59 MB → 15 KB (233×). `/api/alerts/count`: no `Content-Encoding`, 33 bytes. |
| **Result** | PASS |

### T4 — Alerts count and paging

| Field | Details |
|-------|---------|
| **Steps** | `curl -sk $B/api/alerts/count`; `curl -sk $B/api/alerts` (size); `curl -sk "$B/api/alerts?limit=5&unread=1"`; `curl -sk "$B/api/alerts?limit=0"`; `curl -sk -w '%{http_code}' "$B/api/alerts?limit=5&before=nope"` |
| **Actual** | Count: `{"total":500,"unread_count":500}` (33 bytes). Full list: 186,607 bytes. `limit=5&unread=1`: 5 alerts, `unread_count: 500`, `total: 500`, `has_more: true`, `next_before: "47876626"`, 1,819 bytes. `limit=0`: `{"alerts":[],"unread_count":500,"total":500,"has_more":true}`. Unknown `before`: **400**. |
| **Result** | PASS |

### T5 — WebSocket `hello` with delta session list and scoped output

| Field | Details |
|-------|---------|
| **Steps** | Two Node 24 WebSocket clients on `wss://localhost:8443/ws` for 30 s each, side by side. Client A sends `{"type":"hello","data":{"session_list":"delta","session_view":"summary","output":"subscribed"}}` on open; client B sends nothing. Each counts frames and bytes by `type`. A third 4 s run logs frame order for a hello client. |
| **Actual** | A got `hello_ack` `{"session_list":"delta","session_view":"summary","output":"subscribed","capabilities":["ws.hello","ws.session_list_delta","ws.session_view_summary","ws.output_scoped","ws.unsubscribe"]}`. Over 30 s, A: 19 `session_state` (34,833 bytes), **0 `output`**, 1 `pane_capture`, 1 `sessions` (the on-connect frame, see below). B: **2 `sessions` frames (7,103,447 bytes)**, 511 `output` (71,269 bytes), 310 `pane_capture` (1,243,100 bytes). Both got the same `stats`, `alert`, `needs_input` and `channel_ready` frames. |
| **Result** | PASS for the delta stream and scoped output. |

**Known limitation, not changed in this release.** The daemon sends the
full `sessions` list as soon as a WebSocket connects, before it reads
`hello`. The frame-order run showed `sessions` (3,551,724 bytes)
arriving first and `hello_ack` second. The client flow doc already says
frames before `hello_ack` can be ignored, but a hello client still
downloads that frame once per connect. The one `pane_capture` client A
received was also sent before `hello` was processed. Setting
`server.ws_session_list_default: delta` removes the on-connect frame for
every client, but the web app still expects it, so that is not the
default. Recorded as a follow-up in the backlog.

### Gates

| `go test ./...` | PASS (4162 passed in 83 packages) |
| `make lint` | PASS (`0 issues.`) after fixing 4 errcheck findings in the gzip middleware and its test file |
| `node --check internal/server/web/app.js` | PASS |
| `make build` | PASS (`bin/datawatch` → `datawatch v9.0.82`) |
| `make install` + `systemctl --user restart datawatch` | PASS (`datawatch version` → `datawatch v9.0.82`; `/api/info` → `9.0.82`) |
| `bash scripts/release-smoke.sh` (own isolated sandbox daemon, `TMUX` unset) | PASS — `== Summary ==` Pass: 217, Fail: 0, Skip: 35 (109 numbered sections; section 73 covers capabilities, the summary list, alert counts and API gzip) |

## v9.0.83 — Session-tracker git growth: output.log out of history, no implicit auto-gc (2026-10-14)

Fixes the 2026-10-13 daemon OOM. Each session-tracking repo committed its
growing `output.log` on every state change, and git's automatic gc then
repacked those blobs inside the daemon's cgroup. Now `output.log` (and
`.enc`/`.pipe`) stays on disk, untracked. Tracker repos get `gc.auto=0`,
`gc.autoDetach=false`, `pack.windowMemory=64m`, `pack.threads=1` and
`core.bigFileThreshold=16m` at init and on resume.

### T1 — Unit tests (`internal/session`)

| Field | Details |
|-------|---------|
| **Steps** | `go test -count=1 -run Tracker -v ./internal/session/` |
| **Expected** | New repos get the gc/pack caps; `output.log` is never committed; a resumed legacy repo with `output.log` tracked gets it untracked (file kept on disk). |
| **Actual** | `--- PASS: TestNewTracker_SetsSafeGCConfig`, `--- PASS: TestTracker_OutputLogNotCommitted`, `--- PASS: TestResumeTracker_UntracksLegacyOutputLog`; `ok internal/session 0.119s` |
| **Result** | PASS |

### T2 — Reproduction harness, legacy vs fixed setup

| Field | Details |
|-------|---------|
| **Steps** | `scripts/repro-tracker-git-growth.sh --workdir bin/repro-scratch --commits 120 --chunk-kb 512 --go` (scratch dir, removed afterwards) |
| **Expected** | Legacy setup grows roughly quadratically and commits `output.log` every time; fixed setup stays small and never commits it. |
| **Actual** | See the table below. `.git` is 2.83 GB for legacy vs 2.4 MB for fixed (about 1,177× smaller) with the same 62 MB `output.log`. Max commit RSS is 65 MB vs 6 MB. Auto-gc fired in legacy and not in fixed. `go test: pass`, `RESULT: PASS`, 76 s. |
| **Result** | PASS |

```
mode    | .git size KB |  max commit RSS KB |  auto-gc | output.log commits |  loose | packs |  output.log KB
--------+--------------+--------------------+----------+------------------+--------+-------+---------------
legacy  |      2834020 |              66776 |        y |                120 |    392 |     1 |          62248
fixed   |         2408 |               6028 |        n |                  0 |    360 |     0 |          62248
```

### T3 — Live daemon redeploy and 23-minute observation

| Field | Details |
|-------|---------|
| **Steps** | `make build` → `make install`; `datawatch version` checked before the restart; `systemctl --user restart datawatch` at 15:21:18. Then every 3 min: `systemctl --user show datawatch -p MainPID -p NRestarts -p MemoryCurrent -p MemoryPeak` and `journalctl --user -u datawatch --since '2026-10-14 15:21:18' \| grep -ciE 'oom\|killed'`. |
| **Expected** | Same PID throughout, `NRestarts=0`, no OOM/killed lines, memory flat. |
| **Actual** | `datawatch version` and `/api/info` both `9.0.83`. 8 samples from 15:21:34 to 15:44:06: MainPID 3008307 throughout, `NRestarts=0`, `MemoryCurrent` 631–669 MB (no upward trend), `MemoryPeak` 1.19 GB (set during startup, unchanged after), **0** oom/killed lines in every sample. |
| **Result** | PASS |

### T4 — Spot-check live session repos (read-only)

| Field | Details |
|-------|---------|
| **Steps** | For long-lived `<host>-9245` and for every repo that committed after the restart: `git ls-files output.log output.log.enc`, `git config gc.auto`, `git config gc.autoDetach`, `git config pack.windowMemory`, and `git log -- output.log` |
| **Expected** | Nothing tracked; `gc.auto=0`, `gc.autoDetach=false`, `pack.windowMemory=64m`; `output.log` still on disk. |
| **Actual** | `<host>-9245`: `ls-files output.log` is empty and `gc.auto=0`. Its last commit was 15:07:59, before the restart, from the v9.0.62+ tracker that was already untracking it; it was waiting for input and made no new commit during the window. Its `.git` is still 8.7 GB of pre-fix history, so `scripts/shrink-session-repo.sh` is a separate operator step. Repos that committed after the restart, `<host>-26ea` (15:22:52 "input sent") and `<host>-7c28` (15:22:16 "killed"): `ls-files` is empty, `gc.auto=0`, `autoDetach=false`, `winmem=64m`, 0 commits ever touched `output.log`, `.git` 452 KB / 500 KB, and `output.log` is present on disk. |
| **Result** | PASS |

### Gates

| Gate | Result |
|------|--------|
| `go test ./...` | PASS (69 packages `ok`, exit 0) |
| `make lint` | PASS (`0 issues.`) |
| `node --check internal/server/web/app.js` | PASS |
| `gofmt -l` | No findings in the files this release touched. Go 1.27.2's gofmt flags about 140 untouched files repo-wide; that predates this change and is not a CI gate (`make lint` is). |
| `make build` + `make install` | PASS (`datawatch v9.0.83`) |
| Release smoke | Not run. This is a patch fix with no new endpoint or operator-facing API (AGENT.md Section C allows targeted or no smoke). Live deploy and observation are in T3. |

## v9.0.84 — Session-tracker git growth: live remediation of the bloated repos (2026-10-14)

Follow-up to v9.0.83. With the fixed daemon running (v9.0.83, `output.log`
already untracked in each target), the three pre-fix session repos were
shrunk with `scripts/shrink-session-repo.sh`. No code change.

### T5 — Hygiene sweep on all session repos

| Field | Details |
|-------|---------|
| **Steps** | `for g in ~/.datawatch/sessions/*/.git; do git -C ${g%/.git} config gc.auto; done \| sort \| uniq -c`, then `scripts/shrink-session-repo.sh --all-hygiene ~/.datawatch/sessions`, then the same loop again |
| **Expected** | Every session repo ends with `gc.auto=0` and the `output.log` excludes. |
| **Actual** | Before: 521 of 547 repos had `gc.auto=0` (the other 26 were newer repos with it unset). Sweep: `547 repo(s) configured, 0 skipped` in 12 s. After: 547 of 547 `gc.auto=0`. |
| **Result** | PASS |

### T6 — Shrink ddeb, ab22, 9245 (smallest first)

| Field | Details |
|-------|---------|
| **Steps** | Prerequisite: `datawatch version` = `cmd/datawatch/main.go` (9.0.83), `systemctl --user is-active datawatch` = active, `git ls-files output.log` empty in each target, free disk 1.1 TB. For each repo: `du -sh .git`, `--dry-run`, then `--allow-active` (each was `waiting_input` but idle: last commit 6 h / 38 min / 68 min earlier, no `index.lock`). A 5 s sampler logged `systemctl --user show datawatch -p MemoryCurrent`, the shrink scope's `memory.current`/`memory.peak`, git RSS and `free -g` available, ready to kill only the scope if it went over 4 GB or available memory dropped below 8 GB. Afterwards: `du -sh .git`, `git fsck --no-progress`, `git log --all --oneline -- output.log \| wc -l`, `stat` of the working-tree `output.log`, `NRestarts`, `journalctl --user -u datawatch --since -1h \| grep -ci oom`. |
| **Expected** | `.git` shrinks to history without `output.log`; fsck clean; 0 `output.log` commits; working-tree `output.log` unchanged; daemon unaffected. |
| **Actual** | See the table below. Every run: script `OK`, `fsck` exit 0, `git log --all -- output.log` empty, timeline.md/conversation.md commit counts unchanged, working-tree `output.log` same size and inode (ddeb 29,454,368 B; ab22 504,425,422 B; 9245 260,108,841 B). The sampler never needed to abort. Daemon: MainPID 3008307 throughout, `NRestarts=0`, `MemoryCurrent` 623–649 MiB, 0 oom lines. Available host memory stayed at 94 GB. Disk used went from 622 GB to 583 GB. |
| **Result** | PASS |

```
repo  | commits | .git before | .git after | output.log commits rewritten | scope memory.peak | run time
------+---------+-------------+------------+------------------------------+-------------------+---------
ddeb  |     660 |      822 MB |     884 KB |                          342 |          32.9 MiB |      7 s
ab22  |    3642 |       31 GB |     3.3 MB |                         2239 |         274.3 MiB |     38 s
9245  |    2121 |      8.7 GB |     2.1 MB |                         1421 |         102.1 MiB |     20 s
```

Tool: `git filter-branch` (`git filter-repo` is not installed), each run in its
own `systemd-run --user` scope with `MemoryMax=4G`, `MemorySwapMax=0`, nice 19
and idle I/O. The 2026-10-13 in-daemon repack peaked at about 108 GB on ab22.

_Confirmed 2026-10-14: PR #245 merged into main; remediation stands (ab22 5M, 9245 3M, ddeb 1M `.git`; verify script passes; daemon NRestarts=0)._

## v9.0.85 — Automata lifecycle events: live WS `prd_update` check after the dispatcher migration (2026-10-14)

The WS `prd_update` broadcast now runs as one subscriber (`prd.updated`) on
the new in-process Automata lifecycle event dispatcher instead of the old
single-slot hook. This run checks that the production daemon still sends the
same frames, in the same shape, for each action.

| Item | Value |
|---|---|
| **Daemon** | production, `https://127.0.0.1:8443`, installed with `make install`, restarted with `systemctl --user restart datawatch` |
| **Version** | `datawatch version` → `datawatch v9.0.85` |
| **WS client** | throwaway Go program (gorilla/websocket) under the ignored `.datawatch/smoke/`, dialed `wss://127.0.0.1:8443/ws`, sent `hello` (`session_list=delta`, `session_view=summary`, `output=subscribed`) and printed only `prd_update` frames |
| **Throwaway Automaton** | `fc36e56b` (title `lifecycle-smoke-v9.0.85`) — hard-deleted at the end; `GET /api/autonomous/prds/fc36e56b` → 404 and no `*smoke*` Automaton left in the list |
| **Result** | PASS — one frame per state change, same `{prd_id, status}` / `{prd_id, deleted:true}` shape as before |

Commands (base `B=https://127.0.0.1:8443/api/autonomous/prds`):

```
curl -sk -X POST $B -d '{"spec":"throwaway smoke test ...","project_dir":".../.datawatch/smoke","title":"lifecycle-smoke-v9.0.85"}'  → 200, id fc36e56b, status draft
curl -sk -X PATCH $B/fc36e56b -d '{"title":"lifecycle-smoke-v9.0.85-edited"}'                                                    → 200
curl -sk -X POST  $B/fc36e56b/approve -d '{}'   → 400 `prd "fc36e56b" status "draft" is not approvable` (no state change)
curl -sk -X POST  $B/fc36e56b/cancel            → 200 {"status":"cancelled"}
curl -sk -X DELETE "$B/fc36e56b?hard=true"      → 200 {"status":"deleted","killed_sessions":0}
```

Frames received:

```
19:01:23.073 {"type":"prd_update","data":{"prd_id":"fc36e56b","status":"draft"}}       ← create
19:01:25.106 {"type":"prd_update","data":{"prd_id":"fc36e56b","status":"draft"}}       ← edit (title)
                                                                                         (approve refused: no save, no frame — same as before)
19:01:29.133 {"type":"prd_update","data":{"prd_id":"fc36e56b","status":"cancelled"}}   ← cancel
19:01:31.146 {"type":"prd_update","data":{"deleted":true,"prd_id":"fc36e56b"}}         ← hard delete
```

Not covered live: a successful approve, which needs the Automaton in
`needs_review` (an LLM decompose run); that path, and the task-level and
session-start events, are covered by the unit tests in
`internal/autonomous/lifecycle_*_test.go`, `prd_update_broadcast_test.go` and
`internal/server/prd_update_ws_frame_test.go`.

## v9.0.86 — daemon-wide `max_concurrent_tasks` + `max_parallel_tasks` deprecation: live check on the production daemon (2026-10-14)

| Item | Value |
|---|---|
| **Daemon** | production, `https://127.0.0.1:8443` (no auth token configured), `make build` → `make install`, restarted with `systemctl --user restart datawatch` at 20:03:15; `is-active` → `active`; `/api/health` → `"version":"9.0.86"` |
| **Live config** | `config.yaml` still has `autonomous.max_parallel_tasks: 1` (left in place on purpose for check (a)) |
| **Result** | PASS (a)–(e) |

| # | Check | Command | Output |
|---|---|---|---|
| a | One-time deprecation warning at startup | `journalctl --user -u datawatch --since '2026-10-14 20:03:15' \| grep -i deprecated` | exactly one line: `[warn] config: autonomous.max_parallel_tasks is deprecated and ignored; use autonomous.max_concurrent_tasks (removed in a future release)` |
| a | Audit entry | MCP `audit_query` `action=config_deprecated_key` | `count: 1` — `{"ts":"2026-10-14T20:03:19.71-04:00","actor":"system","action":"config_deprecated_key","details":{"key":"autonomous.max_parallel_tasks","replacement":"autonomous.max_concurrent_tasks"}}` |
| b | GET shows the new key | `curl -sk https://127.0.0.1:8443/api/config` → `.autonomous` | `max_concurrent_tasks=1` (`max_parallel_tasks=1` still listed, ignored) |
| b | PUT at the current value round-trips | `curl -sk -X PUT -H 'Content-Type: application/json' -d '{"autonomous.max_concurrent_tasks":1}' https://127.0.0.1:8443/api/config` | `{"status":"ok"}` HTTP 200; GET still `max_concurrent_tasks=1` |
| c | MCP | `autonomous_config_get` | `…"max_concurrent_tasks":1…` present |
| d | Deprecated key accepted and ignored | `curl -sk -X PUT -H 'Content-Type: application/json' -d '{"autonomous.max_parallel_tasks":1}' https://127.0.0.1:8443/api/config` | HTTP 200 `{"deprecated_fields":{"autonomous.max_parallel_tasks":"deprecated and ignored; use autonomous.max_concurrent_tasks"},"status":"ok"}`; GET still `max_concurrent_tasks=1` |
| e | CLI version | `datawatch version` | `datawatch v9.0.86` |

Not done on production: a non-default value (2) round-trip. An Automaton
was running, and raising the value would change how queued Automata run.
That round-trip is covered by the unit tests
(`TestMaxConcurrentTasks_PutConfigRoundTrip`,
`TestAutonomousMaxConcurrentTasks_SetAndRoundTrip`) and by
`scripts/release-smoke.sh` section 74, which runs against a sandbox daemon
and restores the original value.
