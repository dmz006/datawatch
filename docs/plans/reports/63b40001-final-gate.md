# Final gate — 63b40001 (counts-only report)

Date: 2026-11-01. Run without tmux (`TMUX`, `TMUX_TMPDIR` unset). No daemon started, no
package manager invoked, no version variable changed.

## Result: PASS

## Checks (pass/fail only)

| Check | Result |
|---|---|
| 1. `go build -o /dev/null ./cmd/datawatch/` | pass |
| 2a. `go test` — packages passed / failed | 5 / 0 |
| 2b. `go test` — tests passed / failed (top level) | 7673 / 0 (18 skipped) |
| 3. Node test suite — tests passed / failed | 5 / 0 (0 skipped) |
| 4. `bash scripts/check-howto-coverage.sh` | pass |
| 5. `var Version` untouched in the two entry-point files (diff clean) | pass (no diff) |

## Skipped-paths fixture, per-reason counts seen in the new test

| Reason | Skipped-path entries |
|---|---|
| never-stage | 1 |
| ignored | 2 |
| (no reason supplied) | 1 |

Total skipped-path entries observed across the test cases: 4.
