# datawatch v5.26.14 — release notes

**Date:** 2026-04-27
**Spans:** v5.26.13 → v5.26.14
**Patch release** (no binaries — operator directive).
**Closed:** Scroll mode no longer leaks live updates from the running session.

## What's new

### Scroll mode preservation — third iteration

Operator: *"Scroll mode still getting live updates from running session."*

This is the third pass on the same problem. Recap:

| Version | Approach | What broke |
|---------|----------|------------|
| v5.24.0 | Skip redraws when xterm is scrolled up (`viewportY < baseY`) | xterm-side scroll worked, but pressing the **Scroll** button (which puts tmux in copy-mode) didn't preserve the view — operator's "scroll mode" is the tmux copy-mode flag (`state._scrollMode`), not xterm's own scroll-back. |
| v5.26.9 | Skip redraws unconditionally while `state._scrollMode === true` | Scrolling broke entirely — PageUp / PageDown moved tmux's position but xterm never re-rendered, so the operator saw a frozen frame. |
| v5.26.10 | Content-aware dedupe — skip only when the captured frame is byte-identical to the last write | Defeated by claude-style TUIs whose status timer (`(7s · timeout 1m)`) ticks every second, marking every frame as "different" and firing a redraw that pulled live content into the scroll view. |
| **v5.26.14** | Skip redraws in scroll mode UNLESS `state._scrollPendingRefresh` is set; the flag is set explicitly when the operator scrolls. | — |

How it works:

- `toggleScrollMode` (Scroll button click) sets `state._scrollPendingRefresh = true` so the FIRST redraw after entering scroll mode lands, showing the operator the scroll-back position. Subsequent idle ticks (status timer, live output) skip silently.
- `scrollPage('up' | 'down')` (Page-Up / Page-Down buttons) sets the flag right before sending the keystroke to tmux. The next pane_capture sees the new tmux scroll position and renders it. Then idle ticks resume skipping.
- `exitScrollMode` also clears `state._lastPaneFrame` so the first post-exit pane_capture is forced through (otherwise the cached scroll-view frame would be byte-identical to the new live frame's first write and skip-as-identical).

End result: while idle in scroll mode, the live status timer + new output don't bleed into the view; the scroll position is preserved. Each operator click on PageUp/PageDown produces exactly one redraw with the new position.

## Configuration parity

No new config knob.

## Tests

- 1397 Go unit tests passing (no Go-side changes).
- Functional smoke unaffected (does not exercise scroll mode).
- Manual browser verification: `Scroll` → idle for 30s, status timer ticks but pane stays put. `PageUp` shows new scroll-back. `PageDown` returns. `ESC — Exit Scroll` returns to live view.

## Known follow-ups

- Smoke step for scroll mode would require browser automation; deferred to v6.0 or a `playwright`-driven UI smoke alongside the existing REST smoke.
- Other operator-reported items closed across the v5.26.x stretch are unchanged.

## Upgrade path

```bash
git pull
datawatch restart
# Hard-refresh PWA tab once to pick up new SW cache + app.js.
# Open a session-detail with a running session, click Scroll →
# the pane freezes at the live position (entry point), then
# PageUp / PageDown navigate scroll-back. Live updates from the
# running session no longer bleed in.
```
