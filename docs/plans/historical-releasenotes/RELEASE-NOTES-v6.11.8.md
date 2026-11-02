# Release Notes — v6.11.8 (scroll-mode symmetry + reconnect reload timing)

Released: 2026-05-05
GH release: https://github.com/dmz006/datawatch/releases/tag/v6.11.8

## Summary

Three operator-reported bugs:

1. Scroll-mode page-up scrolls a different amount than page-down.
2. Scroll-mode page button sometimes takes 2 hits to actually scroll.
3. The first command sent after a daemon restart causes a full browser refresh — but only on the command, not on the restart itself.

## Fixed

### Bug 1 — page size asymmetry

The PWA's scroll-mode buttons sent raw `PPage` / `NPage` keysyms via the generic `sendkey` daemon command. Tmux's keysym resolution in copy-mode goes through the active key-table (vi vs emacs binding choice + table state), and that path was producing different scroll amounts for up vs down depending on what tmux thought the active window was at that moment.

**Fix**: added `tmux-page-up` / `tmux-page-down` daemon commands that invoke `tmux send-keys -X page-up` / `page-down` directly — `-X` bypasses keysym resolution and runs the copy-mode commands directly. Both move the cursor by `window-height-2` so they're guaranteed-symmetric.

`internal/server/api.go` — new commands. `internal/server/web/app.js` `scrollPage()` — switched to the new commands.

### Bug 2 — first click sometimes does nothing

The pane-capture refresh flag was a boolean. On scroll click, PWA set `_scrollPendingRefresh = true` and sent the page key. While waiting for the post-scroll frame, claude-code's status-line timer fired a routine pane_capture frame; that frame consumed the flag (set it back to `false`) and drew pre-scroll content. Then the actual post-scroll frame arrived with the flag reset and was skipped.

**Fix**: switched flag from boolean to deadline timestamp (`Date.now() + 700`). Every pane_capture frame within 700 ms of an operator scroll action forces a redraw, regardless of arrival order. The 700 ms window naturally expires so live updates resume normally afterward.

### Bug 3 — surprise full-browser refresh on first command after restart

The version-mismatch auto-reload was wired into the `sessions` WS message handler. The daemon doesn't broadcast `sessions` immediately on WS reconnect — it does so when something changes, which often coincided with the operator's first post-restart command. Result: operator sends a command, page refreshes, command appears to be lost.

**Fix**: added an eager `/api/health` probe in the WS open handler. Health endpoint returns the running daemon version cheaply. If it differs from the cached `_daemonVersion`, the auto-reload fires NOW (during the visible reconnect transition while the splash/disconnect indicator is up), not later when the operator is mid-action.

## What's still open per operator's report

> "All but capturing session ending"

Session-end natural-language detection is intentionally not in the global default `completionPatterns` — v6.11.6's attempt to add `Done!` / `All done` etc. broke reconnect because pane-buffer replay false-fired the matchers (see v6.11.7 notes). The right path is operator-controlled per-deployment patterns via `cfg.Detection.CompletionPatterns` in `~/.datawatch/datawatch.yaml`. Filed for separate operator decision.

## Tests

1765 pass.

## Mobile parity

[`datawatch-app#63`](https://github.com/dmz006/datawatch-app/issues/63) — same scroll-mode + version-reload improvements for the Compose Multiplatform app.

## See also

- CHANGELOG.md `[6.11.8]`
