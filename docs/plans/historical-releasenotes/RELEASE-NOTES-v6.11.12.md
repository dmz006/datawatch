# Release Notes — v6.11.12 (BL263 follow-up: stale-state gate + DOM-check)

Released: 2026-05-05
GH release: https://github.com/dmz006/datawatch/releases/tag/v6.11.12

## Summary

Two more BL263 follow-ups, this time root-causing both remaining symptoms after v6.11.11.

## Operator reports

1. "All no tmux at bottom on restart, but session is connected."
2. "If I exit the session and go back it doesn't display the session is blank but tmux is back."
3. "Sending commands again activated the display."

## Root cause + fix — Bug A (terminal blank after exit + re-enter)

The `pane_capture` WS handler in `internal/server/web/app.js` (around line 547) had a freeze-on-terminal-state gate driven by the `state.sessions` cache. Original intent: prevent flickering a shell prompt during the brief LLM-exited / tmux-not-yet-cleaned window. But during a daemon-restart / WS-disconnect window, the cache is stale — if it happened to hold a `complete` / `failed` / `killed` value, post-restart pane_capture frames were silently skipped, leaving the terminal blank until the cache was refreshed by an unrelated event (operator's first command).

Daemon-side `StartScreenCapture` already filters out terminal-state sessions (`manager.go:1515`). By the time a frame arrives over WS, the daemon considers the session active. The PWA gate was redundant belt-and-suspenders that hurt during the stale-cache window.

**Fix**: gate now only fires when the cached terminal-state record is fresh (< 10 seconds old). Stale records (typical for the post-restart window) fall through and draw the frame.

## Root cause + fix — Bug B (input bar missing on restart)

The `WS open` reconnect path's optimized "same session is alive" branch checked only `state.terminal && _termSessionId && _termHasContent`. It did NOT verify the input bar was still in the DOM. If a render during the disconnect window had dropped the bar, the optimized path skipped the full re-render and the bar stayed missing.

**Fix**: in the predicate, also require `document.getElementById('inputBar')` to be present. If the bar is missing, fall through to the full `renderSessionDetail()` path which recreates everything. DOM check is reliable; it doesn't depend on the (potentially stale) cache.

## Why earlier attempts didn't work

| Release | Approach | Why it didn't fix this |
|---|---|---|
| v6.11.6 | `inputBarMissing` check in WS open against cached state | Cache was stale at WS-open time (before /api/sessions refetched); fired wrong, broke reconnect entirely; reverted in v6.11.7 |
| v6.11.10 | Same DOM check inside `updateSession()` (after fetch resolves) | Helps for the cold-path on subsequent state updates; misses the case where the bar was missing at WS-open and the optimized path skipped re-render entirely |
| v6.11.11 | Drop `input-disabled` class + fitAddon refit | Helps with cosmetic input-disabled lingering, but doesn't restore a bar that was never re-rendered into the DOM |
| v6.11.12 | DOM check in WS-open predicate itself + relaxed pane_capture stale-state gate | Both root-cause fixes |

## Tests

1767 pass.

## Mobile parity

[`datawatch-app#66`](https://github.com/dmz006/datawatch-app/issues/66) — same gate-relaxation + DOM-check on the Compose Multiplatform app's pane-capture handler and reconnect flow.

## See also

- CHANGELOG.md `[6.11.12]`
