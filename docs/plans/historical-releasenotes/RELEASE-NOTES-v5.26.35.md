# datawatch v5.26.35 — release notes

**Date:** 2026-04-27
**Spans:** v5.26.34 → v5.26.35
**Patch release** (no binaries — operator directive).
**Closed:** Tmux toolbar + screen format survive daemon restarts (operator-reported regression).

## What's new

### Daemon restart no longer breaks the open session view

Operator: *"when service restarts, if i'm in a session and it refreshes the tmux bar goes away and the screen format is messed up, i have to exit the session and go back in to reset."*

What was happening: when the daemon restarted, the WS reconnect handler called `renderSessionDetail(state.activeSession)` unconditionally. That function rebuilds the entire session-detail subtree — including the tmux toolbar HTML and the xterm.js mount point. When the new DOM landed, the old `state.terminal` object was orphaned (xterm.js still held a reference but its DOM node was gone), and subsequent `pane_capture` frames wrote to a detached element. The toolbar was simply not in the new DOM. Operator saw a broken layout and had to leave the session view + come back to force a fresh render.

The fix:

```js
// On WS reconnect while in session-detail:
if (terminal_alive_for_this_session) {
    // Re-subscribe + flag a one-shot redraw on the next pane_capture.
    send('subscribe', { session_id });
    state._pendingPaneCaptureRefresh = true;
} else {
    // First visit / view switch / output_mode change — full re-render.
    renderSessionDetail(sessionId);
}
```

The pane_capture handler honors the flag: if set, it does a `terminal.reset() + write(lines)` on the next frame (same path as the very first frame), which heals any drift from output that landed during the disconnect window. Crucially, it does NOT touch the toolbar HTML — the existing DOM stays put, so the scroll-mode toggle, ESC key, send-keys quick row, and every other affordance survive the reconnect.

### When the full re-render still happens

The conditional preserves the existing DOM only when ALL of:

- `state.terminal` is non-null (xterm.js was successfully initialized)
- `state._termSessionId === activeSession` (we're still on the same session, not a navigation)
- `state._termHasContent === true` (we've drawn at least one frame)

If any of those fail (first visit, output_mode switch, terminal init failure), the path falls through to the original full re-render — same behavior as before.

### What didn't change

- Daemon-side WS handling, pane_capture protocol, session-state machine — all unchanged.
- Other WS reconnect handlers (`sessions` / `settings` view re-render) — still call their respective render functions; this fix only affects session-detail.
- xterm.js scroll mode (v5.26.14), response-noise filter (v5.26.31), all session lifecycle invariants — unchanged.

## Configuration parity

No new config knob.

## Tests

UI-only change. Manually validated by:

```bash
# 1. Open session in PWA
# 2. datawatch restart
# 3. Wait for reconnect toast
# 4. Verify: tmux toolbar still visible, terminal content still present,
#    scroll mode + send-keys still work without leaving + re-entering.
```

Go test suite unaffected (still 465 passing). Smoke unaffected (37/0/1).

## Known follow-ups

Backlog refactor (operator: *"refactor backlog. make sure active work and other areas that things are done are refactored into the correct closed sections"*) — pending.

PRD panel UX polish (operator: *"new prd should be a FAB (+) and not the new prd button at top. There should be a filter icon like sessions list to hide/show the filter and sort options, with it hidden by default"*) — pending; tracked alongside phase 6.

## Upgrade path

```bash
git pull
datawatch restart
# Hard-reload the PWA (SW cache bumped to datawatch-v5-26-35).
# Open a session, datawatch restart, watch the reconnect toast —
# the tmux toolbar + terminal layout should now survive without
# requiring a manual exit + re-entry.
```
