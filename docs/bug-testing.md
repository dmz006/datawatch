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

## Pending Validation (require browser testing)

- Interface checkbox mutual exclusion in browser (code fix verified)
- Detection filter add/remove in browser (code verified)
- Claude session state badges during active work
- Bash terminal rendering (tmux size 80x24 verified, capture-pane working)
- opencode-acp session startup
- ollama terminal wrapping
