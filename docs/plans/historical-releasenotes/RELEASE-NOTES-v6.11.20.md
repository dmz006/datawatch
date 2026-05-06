# Release Notes — v6.11.20 (BL265 tightening + scroll-button sizing)

Released: 2026-05-05
GH release: https://github.com/dmz006/datawatch/releases/tag/v6.11.20

## Operator reports

> "Still getting blank screen when going into session that starts when I send a command"
> "Scroll mode page up is not the same size as page down, make all 3 buttons the same size"

## Bug 1 — blank screen on session entry

### Root cause

v6.11.19 (BL265) added natural-language channel-state detection but matched on **any-substring**. Phrases like `task complete` appearing mid-message in normal claude-code output ("OK, the auth task is complete, now starting on routing") were prematurely transitioning sessions to `Complete`. The PWA's `pane_capture` handler at line 547 then skipped frames for sessions in terminal state → blank screen until the operator's next command bumped the cache.

### Fix

`internal/session/manager.go` `detectChannelStateSignal` — tightened to **end-of-message match** (with optional trailing `.` / `!`). Mid-message occurrences are now ignored.

Also constrained the trailing-`?` heuristic for input to messages ≤ 200 chars (long messages with rhetorical `?` are usually narration, not actual asks).

## Bug 2 — scroll-mode buttons different sizes

### Fix

`internal/server/web/style.css` `.scroll-bar-btn`:
- Removed `max-width: 200px` cap.
- `flex: 1 1 0` + `min-width: 0` + `flex-basis: 33%` so all three buttons get exactly 1/3 of the row.
- Added `box-sizing: border-box` + `white-space: nowrap` + `text-overflow: ellipsis` to handle long labels gracefully.

## Tests

1790 pass (1788 + 2 new BL265 cases):

- `TestBL265_DetectChannelStateSignal_NoFalsePositiveMidMessage` — verifies mid-message completion phrases (e.g., "OK, the auth task is complete, now starting routing") return empty signal.
- `TestBL265_DetectChannelStateSignal_LongTrailingQuestionIgnored` — verifies long messages with rhetorical trailing `?` don't return input signal.

Existing tests updated to match the new end-of-message behavior (test fixtures changed from "task completed successfully" → "Task complete." etc.).

## Mobile parity

[`datawatch-app#70`](https://github.com/dmz006/datawatch-app/issues/70) — same classifier tightening recommended for the Compose Multiplatform side.

## See also

- CHANGELOG.md `[6.11.20]`
