# Testing — Validation Procedures & Results

All bug fixes and features must have documented test results. This document combines
the test procedures (how to validate) with the test log (actual results).

**Bug/plan tracking:** [docs/plans/README.md](plans/README.md) — single source of truth for bugs, plans, and backlog.

---

## How to Test

### Browser Testing (Chrome DevTools F12)
1. **Console tab** — check for JS errors after each action
2. **Network tab** — verify API calls return 200
3. **Elements tab** — inspect DOM to verify CSS changes

### Debug Panel (built-in)
1. Triple-tap the status dot (green/red circle in header)
2. Panel shows last 50 JS errors, failed fetches, WS events
3. "Copy JSON" button copies full log for sharing
4. Interface binding debug: look for `IFACE` messages in console

### API Testing
```bash
# Stats overview
curl -s http://localhost:8080/api/stats | python3 -m json.tool

# Config check
curl -s http://localhost:8080/api/config | python3 -m json.tool

# Session list
curl -s http://localhost:8080/api/sessions | python3 -m json.tool
```

---

## 1. Splash Screen

| Field | Value |
|-------|-------|
| Version | v0.14.5 |
| Steps | Hard refresh (Ctrl+Shift+R), observe splash with eye logo, spinning ring, bouncing dots. Time it — 3+ seconds. |
| Expected | Purple eye logo centered, "datawatch" title, "AI Session Monitor" subtitle, smooth fade to sessions view. |
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
