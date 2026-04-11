# B1: xterm.js Stability and Faster Load

**Date:** 2026-04-11
**Priority:** high
**Effort:** 2-3 days
**Category:** web UI / terminal

---

## Problem

Two issues with the xterm.js terminal in the web UI:

### 1. Terminal crashes
The xterm.js terminal crashes intermittently, requiring the user to navigate away from the session detail view and back in to recover. The terminal becomes unresponsive — no cursor, no output updates, no input.

**Likely causes:**
- WebSocket disconnect during output streaming — xterm.js write buffer overflows or receives malformed data after reconnect
- Race condition between `StartScreenCapture` (200ms ticks) and xterm.js render cycle — multiple rapid writes can corrupt the terminal state
- Memory leak from accumulated terminal buffer — long-running sessions with heavy output can exhaust xterm.js internal buffers
- ANSI escape sequence corruption — partial sequences split across WebSocket messages can leave the terminal in a bad state

### 2. Slow initial load (~20 seconds)
When navigating to a session, it takes up to 20 seconds before the terminal shows content. The user sees "Connecting to session..." splash for an extended period.

**Likely causes:**
- `capture-pane` called on first tick but xterm.js not yet initialized — data arrives before terminal is ready
- WebSocket subscription + first `pane_capture` message has latency from server-side capture scheduling
- xterm.js `fit()` addon called before container has final dimensions — causes reflow after content arrives
- Large initial pane capture (full 40-row terminal) takes time to render with ANSI processing

---

## Investigation plan

### Phase 1: Diagnose crashes (1 day)

1. **Add error boundaries to xterm.js**
   - Wrap `terminal.write()` calls in try/catch
   - Log caught errors to console with stack traces
   - On error: attempt `terminal.reset()` before declaring crash
   
2. **Monitor WebSocket health**
   - Track `ws.readyState` before every write
   - If socket is CLOSING/CLOSED, queue data for reconnect instead of writing
   - Add reconnect backoff with state preservation

3. **Buffer management**
   - Check `terminal.buffer.active.length` periodically
   - If buffer exceeds threshold (e.g., 50k lines), trim oldest lines
   - Test with `scrollback` option set (currently may be unlimited)

4. **ANSI safety**
   - Validate incoming data for incomplete escape sequences
   - Buffer partial sequences until complete before writing to xterm

### Phase 2: Faster initial load (1 day)

1. **Eager terminal initialization**
   - Create xterm.js Terminal instance immediately on session-detail navigation
   - Call `terminal.open(container)` before WebSocket subscription
   - Show cursor/blank terminal immediately (not splash screen)

2. **Pre-fetch pane content via REST**
   - `GET /api/sessions/{id}/capture` returns current pane content
   - Write to terminal immediately without waiting for WebSocket
   - WebSocket updates overlay subsequent changes

3. **Reduce capture-pane latency**
   - Server: capture pane on session-detail WS subscribe (not on next tick)
   - Send first `pane_capture` within 100ms of subscription
   - Use `tmux capture-pane -e` for ANSI (already done) — verify no double-processing

4. **Optimize fit/resize**
   - Defer `fit()` until after first write (not before)
   - Use `ResizeObserver` instead of `setTimeout` for container dimension detection
   - Send `resize_term` to server after fit, not before

### Phase 3: Resilience (0.5 day)

1. **Auto-recovery on crash**
   - Detect unresponsive terminal (no cursor blink, no response to input)
   - Auto-dispose and recreate xterm.js instance
   - Re-subscribe to WebSocket and re-fetch pane content

2. **Graceful WebSocket reconnect**
   - On disconnect: keep terminal visible with last content
   - Show "Reconnecting..." overlay (not full splash replacement)
   - On reconnect: fetch full pane capture and `terminal.reset()` + write

3. **Performance guard**
   - Throttle `terminal.write()` to max 60 calls/second (batch at 16ms)
   - Coalesce rapid `pane_capture` messages (skip intermediate frames)

---

## Files to investigate

| File | What to check |
|------|--------------|
| `internal/server/web/app.js` | xterm.js initialization, WebSocket handlers, `renderSessionDetail()`, `destroyXterm()` |
| `internal/server/api.go` | `pane_capture` WebSocket message handling, screen capture endpoint |
| `internal/session/manager.go` | `StartScreenCapture()` interval, capture-pane calls |
| `internal/server/web/style.css` | Terminal container sizing, overflow rules |

## Success criteria

- Terminal loads content within 2 seconds of navigation
- No terminal crashes during normal use (1-hour session)
- Graceful recovery if crash occurs (auto-reset within 1 second)
- WebSocket disconnect shows overlay, not blank screen
