# Stuck-running watchdog: end-to-end gate

Date: 2026-10-28
Plan: [2026-10-28-stuck-running-watchdog.md](../2026-10-28-stuck-running-watchdog.md)

Every command below runs from the repository root and can be repeated from the
committed tree. No check depends on a git-ignored script.

## Commands

| # | Command | Result |
|---|---------|--------|
| 1 | `make build` | exit 0 |
| 2 | `make lint` | exit 0, 0 issues |
| 3 | `env -u TMUX -u TMUX_TMPDIR go test ./... -count=1` | First run: every package ok except `internal/server` (the Automaton terminology guard), `tests/ci` (smoke section count in `docs/testing.md`) and `internal/session` (the tmp-leak guard, which runs the server suite). Fixed, then those three packages re-run: all ok |
| 4 | `node --check internal/server/web/app.js` and `node --test internal/server/web/*.test.js` | ok; 1421 tests, 0 fail |
| 5 | `make check-internal-refs` | pass |
| 6 | `env -u TMUX -u TMUX_TMPDIR go test ./internal/autonomous/ ./internal/audit/ ./internal/config/ ./internal/server/ -run 'StuckRunning\|Locales' -count=1` | exit 0 |
| 3b | `go test ./...` re-run on the final tree (after the last fix commit) | every package ok, 0 FAIL, about 160 s |
| 3c | `golangci-lint run ./...` on the final tree | 0 issues |
| 7 | Smoke section 86 body against a sandbox daemon (see the tracker section) | 5 of 5 pass |

## Confirmations

- A stuck running Automaton is acted on: `TestStuckRunningWatchdog_RefiresOnceThenBlocks` in `internal/autonomous/stuck_running_watchdog_test.go` re-fires once, then blocks.
- An Automaton with active work is left alone: `TestStuckRunningWatchdog_LeavesActiveWorkAlone` in the same file.
- The audit action has a JSON-lines round-trip test and a CEF signature and escaping test in `internal/audit/stuck_running_events_test.go`.
- Both `Version` variables read 9.29.2 (`cmd/datawatch/main.go`, `internal/server/api.go`). The plan records the open question about bumping on a feature branch.
- The CHANGELOG entry and the README line carry no tracker identifiers.

## Not verified here

- The PWA story TS-829 was written and syntax-checked but not run to completion: the runner fails the existing story TS-827 immediately on this host too.
- A full `release-smoke.sh` run is not usable on this host: the plans-folder tidy gate fails on 48 older plan files unrelated to this change, and with it skipped the sandbox daemon is no longer reachable by section 85.
