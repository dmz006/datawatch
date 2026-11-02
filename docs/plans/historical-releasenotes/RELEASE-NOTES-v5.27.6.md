# datawatch v5.27.6 — release notes

**Date:** 2026-04-30
**Hotfix.** Two operator-reported bugs affecting active-session workflow.

## What's fixed

### BL211 — Scrollback mode broke session state detection

Operator scenario (2026-04-30): operator was scrolled up in the tmux pane when claude finished its turn. claude's pane bottom showed `✻ Crunched for Xm` (turn complete, awaiting next prompt). datawatch state stayed at `running`. Next operator input got sent to a session that had already finished.

**Root cause** — two-bug stack:

1. `internal/session/tmux.go:CapturePaneVisible` deliberately captures the scrolled view in tmux copy-mode (correct for the PWA xterm display so the operator sees what they're scrolling through). But the state-detection loop at `manager.go:1489` used the **same method**, so when copy-mode was active the daemon's prompt + completion checks ran on stale content.
2. `manager.go:2572` lists `Crunched for` / `Thought for` / `Formed for` (past-tense with timing) in `activeIndicators`. Originally meant to catch the indicator when it appears mid-stream between tool calls. Side effect: when claude finishes its turn and the past-tense indicator persists as the LAST visible line, the daemon still treats the session as actively processing.

**Fix:**

- New `TmuxAPI.CapturePaneLiveTail(session)` method that always reads the live pane bottom regardless of copy-mode.
- State-detection loop in `Manager.monitorOutput` (and the screen-capture goroutine at `manager.go:1483+`) switched onto the live-tail capture. The display channel (`onScreenCapture` callback feeding the PWA xterm) keeps using `CapturePaneVisible` so operator scroll behaviour is unchanged.
- Past-tense indicator handling unchanged — but the live-tail capture means the indicator now appears on lines we actually look at, not on a frozen scrollback frame.

### BL215 — Rate-limit detection's per-line length gate was too tight

Operator scenario (2026-04-30): claude printed a rate-limit message; datawatch missed it. Operator also noted the message lacked a parseable reset time.

**Root cause:** `manager.go:3791` had `} else if len(line) < 200 {` — a per-line cap meant to stop false positives from giant log dumps containing words like "limit". Modern claude-code rate-limit dialogs are paragraph-length with context (`"5-hour limit reached. Use the limit reset time to plan... Resets at 2pm."` runs ~600 chars on one line). The gate dropped the line on the floor before any pattern match ran.

**Fix:** raise the gate to 1024 chars. Still keeps the false-positive guard (a megabyte stack trace mentioning "limit" won't trip), but accommodates realistic rate-limit message sizes.

The "no reset time" follow-up the operator noted was already handled correctly by the `+60min` fallback at `manager.go:3837-3840` — when `parseRateLimitResetTime` returns zero (no parseable marker), the auto-resume scheduler falls through to "now + 60 minutes" so the session resumes within an hour even if claude didn't tell us when. Verified by the new `TestBL215_FallbackResumeAt60min` test. Only the upstream miss in `isRateLimit` needed fixing — once that fires, the rest of the pipeline works.

## Tests

```
Go build:  Success (via `make build` + `make cross`)
Go test:   1508 passed in 58 packages (+8 new)
Smoke:     run after install
```

New tests in `internal/session/v5276_scrollback_ratelimit_test.go`:

- BL211 — `TmuxAPI` interface contract includes `CapturePaneLiveTail`; `FakeTmux` implements it; recorded under its own op so tests can assert which capture method production code took.
- BL215 — long rate-limit lines (600+ chars) now match; lines past the 1024 ceiling still get rejected; no-reset-time lines still parse as zero (and the fallback still produces ~60 min).

## Backwards compatibility

- `TmuxAPI` interface gained one method. Production `TmuxManager` and the in-tree `FakeTmux` both implement it. Downstream forks with custom `TmuxAPI` implementations need to add the method.
- Per-line rate-limit gate raised — purely additive. No behaviour change for shorter lines.
- PWA xterm scroll behaviour unchanged.

## Upgrade path

```bash
git pull
datawatch update && datawatch restart
```

No data migration. No new schema. Hard-reload PWA picks up the v5-27-6 cache name.
