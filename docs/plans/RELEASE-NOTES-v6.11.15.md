# Release Notes — v6.11.15 (splash-stuck-on-restart timing fix)

Released: 2026-05-05
GH release: https://github.com/dmz006/datawatch/releases/tag/v6.11.15

## Operator report

> "Getting no screen on reboot, just the connecting to session window, I am guessing it will start working once I send this command. ... It did start working after the command was sent."

## Root cause

In v6.11.13 the WS-open reconnect path cleared `state._lastPaneFrame` BEFORE the async `/api/sessions` fetch. Any pane_capture frames arriving during the ~50 ms fetch window re-populated the cache before `renderSessionDetail()` ran. The dedupe at line 562 (`if (frameKey === state._lastPaneFrame) break;`) then skipped the first post-render frame because the cached frameKey matched it.

Effect: the splash placeholder placed by `renderSessionDetail` was never dismissed. The dismiss path runs inside the pane_capture handler — only fires when `_termHasContent` is false AND a frame is drawn. The dedupe skip prevented the draw.

The operator's first command triggered fresh tmux content → daemon sent a different pane_capture frame → dedupe miss → drew → splash dismissed → display kicked in.

## Fixed

`internal/server/web/app.js` WS-open reconnect path:

- Moved the `state._lastPaneFrame = null` reset INSIDE the `.then` callback, immediately before `renderSessionDetail()`. Closes the race window where in-flight frames could re-populate the cache.
- Also reset `state._termHasContent = false` at the same point so the splash placement + initXterm path runs cleanly and the first frame goes through the "first-frame" branch (which both dismisses the splash and writes content).

## Tests

1767 pass.

## Mobile parity

Not needed — daemon-internal data flow unchanged; pure PWA timing fix.

## See also

- CHANGELOG.md `[6.11.15]`
