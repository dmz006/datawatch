# Stuck-running watchdog: end-to-end gate

Date: 2026-10-28
Plan: [2026-10-28-stuck-running-watchdog.md](../2026-10-28-stuck-running-watchdog.md)

Every command below runs from the repository root and can be repeated from the
committed tree. No check depends on a git-ignored script.

## Commands

Results are from the working tree as it stood when the last change to this file was made, before the automatic commit.

| # | Command | Result |
|---|---------|--------|
| 1 | `make build` | exit 0 |
| 2 | `make lint` | exit 0, `golangci-lint run ./...`: 0 issues |
| 3 | `env -u TMUX -u TMUX_TMPDIR go test ./... -count=1` | exit 0, every package ok, 0 FAIL, about 200 s together with rows 1 and 2 |
| 4 | `node --check internal/server/web/app.js` and `node --test internal/server/web/*.test.js` | ok; 1421 tests, 0 fail (one run of `app-notify-events.test.js` failed while `make` ran alongside; it passed alone and on re-run) |
| 5 | `make check-internal-refs` | pass |
| 6 | `env -u TMUX -u TMUX_TMPDIR go test ./internal/autonomous/ ./internal/audit/ ./internal/config/ ./internal/server/ -run 'StuckRunning\|Locales' -count=1` | exit 0 |
| 7 | Smoke section 86 body against a hand-started sandbox daemon | 5 of 5 pass in an earlier pass, recorded in the tracker section; not re-run in this pass (see Not verified here) |

## Confirmations

- A stuck running Automaton is acted on: `TestStuckRunningWatchdog_RefiresOnceThenBlocks` in `internal/autonomous/stuck_running_watchdog_test.go` re-fires once, then blocks.
- An Automaton with active work is left alone: `TestStuckRunningWatchdog_LeavesActiveWorkAlone` in the same file.
- The audit action has a JSON-lines round-trip test and a CEF signature and escaping test in `internal/audit/stuck_running_events_test.go`.
- Both `Version` variables read 9.29.2 (`cmd/datawatch/main.go`, `internal/server/api.go`). The plan records the open question about bumping on a feature branch.
- The CHANGELOG entry and the README line carry no tracker identifiers.

## Not verified here

- The PWA story TS-829 was written and syntax-checked but not run to completion: the runner fails the existing story TS-827 immediately on this host too.
- Smoke section 86 was added to `scripts/release-smoke.sh` and counted in `docs/testing.md` (the `tests/ci` guard passes), but the script was not run through in this pass; the body run recorded in the tracker is from an earlier pass.
- A full `release-smoke.sh` run is not usable on this host: the plans-folder tidy gate fails on 48 older plan files unrelated to this change, and with it skipped the sandbox daemon is no longer reachable by section 85.
