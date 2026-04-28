# datawatch v5.26.45 — release notes

**Date:** 2026-04-27
**Spans:** v5.26.44 → v5.26.45
**Patch release** (no binaries — operator directive).
**Closed:** Daemon-restart-in-session screen recovery (v5.26.35 follow-up — same operator complaint, deeper cause).

## What's new

### Reconnect now sends a fresh `resize_term` with live xterm dimensions

Operator-reported (repeat): *"when datawatch daemon restarts and i'm in a session the screen gets messed up, loses tmux chat and i have to exit and reenter session to get view back again."*

v5.26.35 fixed the DOM side of this — we stopped calling `renderSessionDetail()` on reconnect when the terminal was already alive, so the toolbar + xterm.js mount survived. But the operator was still seeing garbled output after restart. v5.26.45 closes the second half: a tmux/xterm dimension drift that v5.26.35 didn't address.

What was happening:

```
operator in session at 200×60
  ↓
daemon restart begins → WS disconnects
  ↓
during the disconnect window, the operator may resize the browser
OR the daemon's tmux pane geometry may drift on re-attach (default
size if tmux loses tracking). xterm-side stays at 200×60 (it
doesn't know).
  ↓
WS reconnect → v5.26.35 sends `subscribe` → daemon's first
pane_capture comes back at, say, 80×24 (tmux's restored size)
  ↓
xterm at 200×60 receives an 80×24 frame → garbled layout, wraps
in unexpected places, "loses tmux chat" because the pane no longer
includes the bottom rows the operator expected
```

v5.26.45 sends an explicit `resize_term` with the live xterm `cols`/`rows` immediately after re-subscribe. The daemon reshapes the underlying tmux pane to match the browser's view; the next `pane_capture` frame comes back at the correct geometry; the redraw is clean.

```js
if (sameSessionTermAlive) {
  send('subscribe', { session_id: sid });
  state._pendingPaneCaptureRefresh = true;

  // v5.26.45 addition
  const t = state.terminal;
  if (t && t.cols && t.rows) {
    send('resize_term', { session_id: sid, cols: t.cols, rows: t.rows });
  }
} else {
  renderSessionDetail(sid);
}
```

### Why the v5.26.35 fix wasn't enough

v5.26.35's reasoning was correct: the DOM was healthy and `renderSessionDetail()` was destroying it unnecessarily. But "DOM healthy" is not the same as "daemon and browser agree on terminal geometry." v5.26.35 fixed the visible-toolbar regression; v5.26.45 fixes the underlying-content garble.

Both halves of the operator's original complaint now hold:

- ✅ Tmux toolbar / scroll mode / send-keys row stay intact across daemon restart (v5.26.35).
- ✅ Pane content redraws cleanly at the operator's actual terminal size (v5.26.45).

## Configuration parity

No new config knob — UI fix.

## Tests

Manually validated by:

```bash
# 1. Open a session in PWA
# 2. Resize the browser window so xterm is at a non-default geometry
# 3. datawatch restart
# 4. Wait for the reconnect toast
# 5. Verify: terminal redraws at the resized geometry, tmux output
#    is readable, no need to navigate away + back.
```

Go test suite unaffected (still 465 passing). Smoke unaffected (40/0/1).

## Known follow-ups

Operator queue items still pending from this turn:

- **Autonomous filter icon should match sessions list magnifying glass** + move to header bar next to server-status indicator.
- **New PRD / project_directory should be a directory selector** like the New Session modal.
- **Directory creation while browsing** — both for New PRD and New Session.

These are tracked for the next pass.

Backlog otherwise unchanged — see `docs/plans/2026-04-27-v6-prep-backlog.md`.

## Upgrade path

```bash
git pull
datawatch restart
# Hard-reload the PWA (SW cache bumped to datawatch-v5-26-45).
# Open a session, resize the browser, restart the daemon — the
# session view should recover at the new geometry without
# requiring an exit + re-entry.
```
