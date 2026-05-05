# Release Notes — v6.11.10 (BL263 follow-up — restore input bar after restart)

Released: 2026-05-05
GH release: https://github.com/dmz006/datawatch/releases/tag/v6.11.10

## Summary

BL263 follow-up — restore the tmux input bar at the bottom of the session detail view after a daemon restart. v6.11.9 fixed the session itself (re-piping tmux output) but the input bar could still be missing from the PWA DOM if it had been dropped during the disconnect window.

## Operator report

> "Restart seemed better but still had the tmux command window at bottom not return after refresh."

## Fixed

- **`internal/server/web/app.js` `updateSession()`** — when a `session_state` update arrives for the currently-viewed session, now also checks: does the input bar need to be present (session active + input_mode != 'none') but it's missing from the DOM? If so, force `renderSessionDetail()` to recreate it.

The check runs inside `updateSession()` — which is called after the live `/api/sessions` fetch resolves on reconnect, so the session state is guaranteed fresh. This is the difference from v6.11.6's failed attempt, which checked DOM state at WS-open time using stale cached state.

The optimized "same session is alive" reconnect path keeps the bar in the DOM for the common case. This patch catches the cold path where the disconnect window briefly saw an inactive state and a render dropped the bar.

## Tests

1767 pass.

## Mobile parity

[`datawatch-app#64`](https://github.com/dmz006/datawatch-app/issues/64) — same input-bar-restore in the Compose Multiplatform app's session-state-update handler.

## See also

- CHANGELOG.md `[6.11.10]`
