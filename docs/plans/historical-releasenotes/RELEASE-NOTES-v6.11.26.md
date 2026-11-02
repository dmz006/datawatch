# Release Notes — v6.11.26

Released: 2026-05-05
GH release: https://github.com/dmz006/datawatch/releases/tag/v6.11.26

### Summary — BL266 watcher fix (operator-debugged)

Operator started a fresh claude session and observed the bug live: gap watcher flipped Running→WaitingInput exactly once at lce_age 64 s, then state bounced back to Running and stuck. **Root cause**: the legacy "no prompt → revert to Running" reverters in `manager.go:1615/3928/4094/4274/4361` fire every ~2 s on the structured-channel monitor tick, and they bump `UpdatedAt`. The watcher's `max(LCE, UpdatedAt)` fallback then turned every reverter tick into a permanent watcher bypass.

### Fixed

- **`internal/session/state_engine.go` `runChannelStateWatcherTick`** — DROPPED the `UpdatedAt` fallback. Only `LastChannelEventAt` matters for gap detection. Operator input + pane-content changes already bump LCE explicitly, so we never depended on UpdatedAt for real activity — only internal housekeeping was leaking through.
- **`internal/session/manager.go:1615`** — guarded the StartScreenCapture "no prompt → Running" reverter with `lceFresh` (LCE within the watcher's gap window). Stops the reverter from undoing watcher transitions when there's no real recent activity.
- **`internal/session/manager.go:3928`** — same guard on the structured-channel monitor's revert branch (this was the main culprit — runs every 2 s for MCP/ACP backends).
- **`internal/session/manager.go:4357`** — output-arrival "WaitingInput → Running" path now also bumps `LastChannelEventAt`. Output IS real evidence of activity, and the watcher will now see it.

### Added

- **`scripts/release-smoke.sh` check 17 — BL266 gap watcher**: starts a fresh `smoke-state-engine` claude-code session, waits 22 s, asserts state flipped to `waiting_input`. Catches any future regression of the watcher / reverter-guard interaction.
- **`scripts/release-smoke.sh` `cleanup_all` `sess)` kind**: tracked sessions get killed via `/api/sessions/kill` on EXIT (success or failure).
- **`scripts/release-smoke.sh` orphan sweep extended**: now catches BOTH `autonomous:*` race-survivors AND any session named `smoke-*` (any state). Sweeps anything not in the pre-smoke baseline.

### Tests

- **`internal/session/bl266_state_engine_test.go`** — `TestBL266Followup_v6_11_26_WatcherIgnoresFreshUpdatedAt` replaces the previous `FreshUpdatedAtKeepsRunning` (inverted assertion). LCE-only is the new contract.
- 525 session+server tests pass.

### Memory rule

Saved to project memory: every smoke check that creates daemon resources MUST use a `smoke-*` name and `add_cleanup <kind> <id>` tracking. The `cleanup_all` sweep is a safety net, not a substitute.
