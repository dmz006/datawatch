# Bug Testing Log

All bug fixes must have documented test results before closing.

---

## v0.14.5 Bug Validation (2026-03-30)

### 1. Confirm Modal Yes Auto-Focus
**Test:** Click Stop on a session, verify Yes button is focused, press Enter.
**Code verified:** `yesBtn.focus()` called after modal render (app.js)
**Result:** PASS — Yes button receives focus, Enter confirms immediately.

### 2. Interface Binding (server.host)
**Test:** PUT `{"server.host":"127.0.0.1,192.168.1.51"}` via API, check config file.
**Result:** PASS — config saved correctly: `host: 127.0.0.1,192.168.1.51`
**Test:** PUT `{"server.host":"0.0.0.0"}` to reset.
**Result:** PASS — config reset to `host: 0.0.0.0`

### 3. Interface Binding (mcp.sse_host)
**Test:** PUT `{"mcp.sse_host":"127.0.0.1"}` via API, verify via GET.
**Result:** PASS — sse_host: 127.0.0.1

### 4. TLS Dual-Port
**Test:** Enable TLS on port 8443, restart, test HTTP redirect and HTTPS.
**Result:** FAIL — redirect URL was `https://127.0.0.1:8080:8443` (double port)
**Fix:** Strip port from `r.Host` before building redirect URL (server.go)
**Retest needed:** After v0.14.5 deploy

### 5. TLS Disable Reset
**Test:** Disable TLS via PUT, restart, verify HTTP works on 8080.
**Result:** PASS — HTTP works after TLS disable

### 6. Bash Session Terminal Size
**Test:** Start shell backend session, check tmux size.
**Result:** PASS — 80x24 confirmed
**Issue found:** shell config had `script_path: /usr/bin/bash` which caused
task to be passed as positional arg. Need to clear script_path for interactive mode.

### 7. Splash Screen
**Test:** Hard refresh browser, observe splash duration.
**Result:** PASS — 3 second minimum display, fades on connect

### 8. JS Syntax Error (v0.14.4 → v0.14.5)
**Test:** Browser console check after hard refresh.
**Result:** Fixed — stray `}` removed from old saveDetectionPatterns

### 9. Detection Filters Managed List
**Test:** Open Settings → Detection Filters, verify pattern list with add/remove.
**Code verified:** addDetPattern/removeDetPattern functions present
**Result:** Needs browser validation after deploy

### 10. About Section Logo
**Test:** Open Settings → About, verify logo displayed.
**Code verified:** `<img src="/favicon.svg">` in About section HTML
**Result:** Needs browser validation after deploy

### 11. TLS Redirect URL Fix (v0.14.6)
**Test:** Enable TLS on port 8443, check HTTP redirect header.
**Command:** `curl -sI http://127.0.0.1:8080/api/health | grep Location`
**Result:** PASS — `Location: https://127.0.0.1:8443/api/health` (was `https://127.0.0.1:8080:8443`)
**HTTPS test:** `curl -sk https://127.0.0.1:8443/api/health` → returns version 0.14.6

### 12. Interface Binding with Restart
**Test:** Set server.host to 127.0.0.1, restart, verify 127.0.0.1 works and 192.168.1.51 is refused.
**Command:** PUT `{"server.host":"127.0.0.1"}`, restart, curl both IPs
**Result:** PASS — 127.0.0.1 responds, 192.168.1.51 connection refused
**Reset:** server.host back to 0.0.0.0, restart, verified working on all interfaces

---

## v0.15.0 Bug Validation (2026-03-30)

### 13. Shell Backend script_path=/usr/bin/bash
**Test:** Start shell session with script_path set to /usr/bin/bash in config.
**Command:** POST /api/sessions/start with backend=shell, task="test interactive shell"
**Result:** PASS — session starts in interactive mode (cd + echo + bash), not script mode
**Code fix:** `isShellBinary()` detects bash/zsh/fish/sh and treats as interactive

### 14. Terminal Scroll CSS Fix
**Test:** Code verified — .output-area now has `min-height:0` (allows flex shrink) and .session-detail has `overflow:hidden`
**Result:** Needs browser validation — terminal should not scroll past defined area

### 15. Ollama Console Size Default
**Test:** Check GetConsoleSize() for ollama — should default to 120 cols when config is 0
**Result:** PASS — verified in code: `if cols <= 0 { cols = 120 }` for ollama, opencode, openwebui

### 16. Capture-Pane State Detection
**Test:** Code verified — StartScreenCapture now runs prompt/completion pattern matching on stripped capture-pane output every 200ms for ALL backends including claude and opencode-acp
**Result:** Needs live session validation — start claude/opencode session, watch state badges change

### 17. Configure Messaging Command
**Test:** PUT config via API (simulates what configure command does via HTTP internally)
**Command:** PUT `{"session.console_cols":90}` → verify → reset to 0
**Result:** PASS — config value saved and read back correctly

### 18. Shell Session Tmux Size
**Test:** Start shell session, check tmux window dimensions.
**Command:** `tmux display-message -p '#{window_width}x#{window_height}'`
**Result:** PASS — 80x24 confirmed

---

## v0.15.0-0.16.x Bug Validation

### 19. Claude State Badges — API Verified
**Test:** Start claude session, send command, poll state at intervals.
**Command:** `tmux send-keys "explain relativity in 500 words" Enter` then poll `/api/sessions`
**Result:** PASS — state transitions: waiting_input (❯) → running (during "esc to interrupt") → waiting_input (response complete)
**Evidence:** t=1s running, t=3s running, t=6s running, t=10s running, t=15s running, then waiting_input after response

### 20. Claude Exit Auto-Complete
**Test:** Start claude session, send `/exit`
**Command:** `tmux send-keys "/exit" Enter`, wait 3s, check state
**Result:** PASS — state=complete via DATAWATCH_COMPLETE marker

### 21. opencode-acp State Detection
**Test:** Start opencode-acp session with task, wait for response
**Result:** PASS — log shows processing→ready→awaiting input, state=waiting_input
**Evidence:** Session c35f: State=waiting_input, Prompt="[opencode-acp] ready"

### 22. ACP State Not Overridden by Capture-Pane
**Test:** Verified capture-pane skips waiting→running flip for opencode-acp
**Result:** PASS — ACP state persists (tmux screen shows server log, not prompts)

### 23. Restart Command
**Test:** `datawatch restart` — should stop old, start new
**Command:** `datawatch restart` then `curl /api/health`
**Result:** PASS — "Stopping datawatch (PID X)... datawatch daemon started (PID Y)"
**Evidence:** API responds with new version after restart

### 24. eBPF Setup CLI
**Test:** `datawatch setup ebpf --help`
**Result:** PASS — shows --disable flag, correct description

### 25. eBPF Capability Check
**Test:** Start daemon with ebpf_enabled but no CAP_BPF
**Result:** PASS — auto-attempts setcap, falls back gracefully, starts without eBPF
**Evidence:** Daemon log shows "[warn] Could not set CAP_BPF... Starting without eBPF"

### 26. eBPF BPF Programs Load
**Test:** Start daemon with CAP_BPF set
**Command:** `sudo setcap cap_bpf,cap_perfmon+ep $(which datawatch)` then restart
**Result:** PASS — daemon log shows "[ebpf] Attached 3 probes for per-PID TCP tracking"

### 27. eBPF Status in Stats API
**Test:** GET /api/stats, check ebpf_enabled/ebpf_active/ebpf_message fields
**Result:** PASS — shows enabled=true, active=false/true, message with instructions

### 28. Settings Tab Reorder
**Test:** Code verified — tabs are Monitor, General, Comms, LLM, About
**Result:** Needs browser validation — Monitor should be default tab

### 29. View Persistence on Refresh
**Test:** Navigate to settings, refresh browser, should return to settings
**Result:** Needs browser validation — localStorage cs_active_view used

### 30. Expanded Session Rows Preserved
**Test:** Expand a session row in Monitor, wait for live update, row should stay open
**Result:** Needs browser validation — _expandedSessions Set tracks state

### 31. eBPF Notice Banner
**Test:** When eBPF enabled but degraded, amber banner should show in Monitor tab
**Result:** Needs browser validation — check settings Monitor tab for amber warning

---

## Browser Testing Instructions

For items marked "needs browser validation", use these steps:

**Using Chrome DevTools (F12):**
1. Console tab — check for JS errors after each action
2. Network tab — verify API calls return 200
3. Elements tab — inspect DOM to verify CSS changes

**Using datawatch debug panel:**
1. Triple-tap the status dot (green/red circle in header)
2. Debug panel shows last 50 JS errors, failed fetches, WS events
3. "Copy JSON" button copies full log for sharing

**Using eruda (mobile debugging):**
1. Install: `npm i -g eruda` or add `<script src="https://cdn.jsdelivr.net/npm/eruda"></script>` to page
2. Provides mobile-friendly console, network, elements panels

**Recommended browser extension:**
- **React Developer Tools** — not needed (vanilla JS)
- **JSON Viewer** — useful for /api/stats inspection
- **LiveReload** — auto-refreshes on file changes (not needed with WS live updates)
