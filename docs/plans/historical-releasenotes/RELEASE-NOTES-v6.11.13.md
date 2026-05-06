# Release Notes — v6.11.13 (drop optimized reconnect path; always full re-render)

Released: 2026-05-05
GH release: https://github.com/dmz006/datawatch/releases/tag/v6.11.13

## Summary

Drop the optimized "same session alive" reconnect path entirely. After 6 patch releases (v6.11.6 → v6.11.12) trying to make the optimization work cleanly, the conclusion is: it can't, and the cure is the diagnosis — always do a full `renderSessionDetail()` on reconnect.

## Iteration history (why this took 6 releases)

| Release | Attempted | Why it didn't fully fix it |
|---|---|---|
| v6.11.6 | Cached-state-based input-bar-missing detection | Cache was stale at WS-open; fired wrong; broke reconnect; reverted |
| v6.11.7 | Reverted v6.11.6 | Restored stability; underlying problem (input bar missing) untouched |
| v6.11.8 | Eager version-reload + scroll-mode fixes | Different bugs; no effect |
| v6.11.9 (BL263) | Daemon `RepipeOutput` in `ResumeMonitors` | Fixed the actual frozen-session symptom; PWA-side cosmetics still broken |
| v6.11.10 | DOM check inside `updateSession()` after fetch | Helps subsequent state updates; misses WS-open case where optimized path skipped re-render |
| v6.11.11 | Drop `input-disabled` class + fitAddon refit | Cosmetic; doesn't restore a bar that was never rendered |
| v6.11.12 | DOM check in WS-open predicate + relaxed pane_capture stale-state gate | Closer; still missing the dedupe `_lastPaneFrame` clear and a fresh state fetch before render |
| **v6.11.13** | **Drop the optimized path; always full re-render with fresh state** | **The simple right answer** |

## Operator reports across the arc

> "Restart seemed better but still had the tmux command window at bottom not return after refresh." (post-v6.11.10)
> "Tmux command panel still not displaying after restart, screen size is compacted to Window size and not the full size going wider than the screen so lines were wrapping around" (post-v6.11.11)
> "All no tmux at bottom on restart, but session is connected. If I exit the session and go back it doesn't display the session is blank but tmux is back." (post-v6.11.12)
> "Still same problem, screen there after reboot but no tmux and screen size was shrunk and was wrapping, when exit and return blank screen until I send a command" (post-v6.11.13 attempt — this release)

Each iteration patched a sub-problem in the optimized path while introducing new ones. v6.11.13 takes the pragmatic exit.

## Fixed

`internal/server/web/app.js` WS open handler — when on session-detail view, now ALWAYS does:

1. **Drop `state._lastPaneFrame`** — clears the pane-capture dedupe cache so the post-reentry frame draws even if its content is identical to what xterm last showed.
2. **Fetch `/api/sessions`** — guarantees `state.sessions` is fresh before rendering decides what to show.
3. **Apply each session via `updateSession()`** — patches the in-memory cache.
4. **Call `renderSessionDetail()`** — fully rebuild the view: rebuild the input bar based on fresh state, reinit xterm with a fresh container, fresh resize_term, fresh pane_capture subscribe.

No more conditional optimized path.

### What this fixes

- ✅ "no tmux at bottom on restart" — full re-render always emits the input bar
- ✅ "screen size shrunk and wrapping" — full re-render reinitializes xterm + sends fresh resize_term
- ✅ "blank screen on reentry until I send a command" — `_lastPaneFrame` cleared so post-reentry pane_capture isn't dedupe-skipped
- ✅ "tmux command panel still not displaying" — full re-render emits based on fresh state
- ✅ Stable behavior across all WS-disconnect / daemon-restart / manual-reentry scenarios

### Cost

~50–200 ms of DOM-rebuild flicker on reconnect. Acceptable; operators were already accepting this in the cold-path branch.

## Lesson for memory

The optimization was load-bearing in operators' minds for "tmux bar doesn't disappear on reconnect" but was actually causing every variant of "tmux bar disappears on reconnect" we've seen since BL263 fixed the daemon-side. When an optimization causes recurring symptoms across 6 patch releases, drop it.

## Tests

1767 pass.

## Mobile parity

[`datawatch-app#67`](https://github.com/dmz006/datawatch-app/issues/67) — same guidance: prefer full re-render on reconnect.

## See also

- CHANGELOG.md `[6.11.13]`
