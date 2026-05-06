# Release Notes — v6.12.2

Released: 2026-05-06
GH release: https://github.com/dmz006/datawatch/releases/tag/v6.12.2

### Summary

Four operator-reported regressions / UX gaps:

1. **Daemon restart marks live sessions as stopped/waiting; list never refreshes back to Running.** BL266 gap watcher loaded sessions from disk with stale `LastChannelEventAt` timestamps and immediately flipped Running → WaitingInput on the first watcher tick. Then `monitorOutput`'s output-arrival path only bumped LCE on the WaitingInput → Running transition — sessions already in Running never had their LCE refreshed from this path, so the watcher could re-flip them indefinitely.
2. **Yellow "needs input" popup fires even while the operator is actively in the session.** With "Suppress toasts for active session" enabled, the in-session popup duplicates what the operator is already watching live. They want the popup only when they were elsewhere; on next session-detail entry, replay it once so the operator sees what happened.
3. **Automata Select-All ticks rows that can't be deleted yet** (need cancel first). Operator wants Select-All to only tick terminal-state rows; active ones must be ticked individually.
4. **Automata batch bar didn't fire reliably on first checkbox tick** — added `onclick="event.stopPropagation()"` and `data-prd-id` so my new select-all eligibility filter works.

### Fixed

- **`internal/session/manager.go` `ResumeMonitors`** — for every session whose tmux pane is confirmed alive after restart, reset `LastChannelEventAt = time.Now()`. Tmux being alive is positive evidence; the old pre-restart timestamp must not survive into the watcher.
- **`internal/session/manager.go` output-arrival path** — `LastChannelEventAt` now ALWAYS bumps when output arrives, not just on WaitingInput → Running. Long-running sessions stay correctly Running with the watcher seeing fresh activity.
- **`internal/session/state_engine.go` `StartChannelStateWatcher`** — added a 30 s warm-up grace period after start before the first transition. Defensive against any edge case where ResumeMonitors didn't reset LCE for a session (e.g. transient tmux SessionExists failures).
- **`internal/server/web/app.js` `handleNeedsInput`** — when in active session AND `suppressActiveToasts` is on, drop the toast + browser notification (still highlights input bar visually). Stash the prompt as `state.pendingNeedsInputPopup[sessionId]`; `renderSessionDetail` calls `maybeReplayPendingNeedsInputPopup` 200 ms after subscribe so the operator sees the popup once when they return to the session.
- **`internal/server/web/app.js` `toggleAutomataSelectAll`** — now only ticks rows in terminal status (completed / rejected / cancelled / archived). Active items must be ticked individually if you want them in the same batch.
- **`internal/server/web/app.js` automata card checkbox** — added `data-prd-id` so the new select-all eligibility filter can identify rows; added `onclick="event.stopPropagation()"` so the checkbox tap doesn't bubble into card-click handlers that could mask the bar render.

### Tests

- 1804 go tests pass.
- Smoke 106 / 0 / 9.
