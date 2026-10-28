# Stuck-running watchdog: end-to-end gate

Date: 2026-10-28

## Commands

| # | Command | Exit status |
|---|---------|-------------|
| 1 | `go build -o /dev/null ./cmd/datawatch/` | 0 |
| 2 | `go vet ./internal/autonomous/ ./internal/config/` | 0 |
| 3 | `env -u TMUX -u TMUX_TMPDIR go test ./internal/autonomous/ ./internal/config/ ./internal/server/ -count=1` | 0 |
| 4 | `bash .datawatch/verify/watchdog-core.sh` | 0 |
| 5 | `bash .datawatch/verify/watchdog-surfaces.sh` | 0 |
| 6 | `bash .datawatch/verify/watchdog-release.sh` | 0 (after fixes, below) |

## Fixes made while running the gate

- `.datawatch/verify/watchdog-release.sh` first exited 141: under `set -o pipefail`, `awk | head -200`, `echo | grep -q` and `grep | grep -q` died of SIGPIPE when the reader closed early (the Unreleased changelog section is over 200 lines). The pipes were replaced with a capped awk, here-strings and a captured variable. The tracker-ID check now diffs `main...` instead of `HEAD`, so it inspects the whole branch addition. The `.datawatch` directory is git-ignored, so this change is not part of the commit.
- The script then exposed real drift: the embedded PWA docs copy no longer matched `docs/`. `make sync-docs` fixed it. The embedded copy is generated and ignored, so it adds no tracked change.

## Confirmations

- PASS: stale running PRD is acted on. `TestStuckRunningWatchdog_RefiresOnceThenBlocks` in `internal/autonomous/stuck_running_watchdog_test.go` re-fires once, then blocks.
- PASS: PRD with active work is left alone. `TestStuckRunningWatchdog_LeavesActiveWorkAlone` in the same file.
- PASS: commit message states the choice. Commit a8a29960c reads "re-fire the completion rollup once, then block".
- PASS: both Version vars match. `internal/server/api.go` and `cmd/datawatch/main.go` are both 9.29.2.
- PASS: CHANGELOG entry has no tracker IDs. The lines added versus `main` match no `BL<number>` pattern.
