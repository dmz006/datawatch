# Release Notes — v6.11.6 (restart-recovery + session-end detection)

Released: 2026-05-05
GH release: https://github.com/dmz006/datawatch/releases/tag/v6.11.6

## Summary

Two operator-reported bugs fixed:

1. **Restart breaks session view** — when the daemon restarts and the operator is in a session, the terminal is not resized correctly and the tmux input bar disappears, requiring exit + re-enter.
2. **Session-end not captured** — `DATAWATCH_COMPLETE:` was the only completion marker; natural claude-code task-completion phrases never transitioned the session out of running/waiting_input.

## Bug 1 — restart breaks session view

### Root cause

The optimized "same session is alive — skip full re-render" reconnect path:
- Read `t.cols` / `t.rows` from xterm BEFORE refitting → if the browser resized during the disconnect, dims were stale and the `resize_term` sent to the daemon told tmux the wrong size.
- Did not check whether the input bar (`#inputBar`) had been removed from the DOM (e.g., scroll-mode race) → operator saw a session view with no input bar.

### Fix (`internal/server/web/app.js` `connect()` open handler)

Three changes:

1. Call `state.termFitAddon.fit()` BEFORE reading `t.cols`/`t.rows`. The fit-addon recomputes dimensions for the current container width and updates the xterm instance; the resize_term then carries accurate values and tmux reshapes the pane to match.
2. Detect "input bar should exist but doesn't" and force a full re-render instead of taking the optimized path. This covers operator-reported cases where the input bar disappeared during the disconnect (scroll-mode + restart race, or a stale `display:none` from elsewhere).
3. Defensive: drop the `input-disabled` class on reconnect even when the optimized path runs (covers sticky-class cases).

## Bug 2 — session-end not captured

### Root cause

`internal/session/manager.go` `completionPatterns` had only `DATAWATCH_COMPLETE:`. That marker is only emitted when the operator's session-init prompt asks the model to print it explicitly. claude-code finishing a task naturally — saying things like "Task complete" or "All done" — never matched.

Additionally the reconcile loop (which detects tmux-pane-exit when claude-code is `/exit`'d) ran every 30 seconds.

### Fix

#### `internal/session/manager.go` `completionPatterns` — add 12 natural-language end-of-task phrases

```
DATAWATCH_COMPLETE:
Task complete
Task completed
Task is complete
Successfully completed
All tasks complete
All tasks completed
I've completed the task
I have completed the task
The task is now complete
The work is complete
All done
Done!
```

All matched via `HasPrefix` on the trimmed line — paragraph mid-sentence text containing these phrases won't false-fire.

#### `internal/session/manager.go` `StartReconciler` — interval 30s → 10s

Faster pickup of tmux-session-gone (claude `/exit`, process death). Trade-off: 3× more frequent `tmux list-sessions` calls (negligible cost). Net latency from tmux exit → operator visible state-change goes from average 15s → average 5s.

## Mobile parity

[`datawatch-app#62`](https://github.com/dmz006/datawatch-app/issues/62) — same restart-recovery + session-end detection improvements on the Compose Multiplatform app side. The patterns added here are emitted via the existing `session_state` WS message, so the mobile app picks them up automatically once the daemon is upgraded.

## Tests

1765 pass (unchanged — pure detection-pattern + reconcile-tick changes; existing tests cover the matching/transition logic).

## See also

- CHANGELOG.md `[6.11.6]`
