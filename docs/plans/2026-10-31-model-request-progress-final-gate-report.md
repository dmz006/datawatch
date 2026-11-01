# Automaton model request watch: final gate report

**Date:** 2026-11-01 (run 15:31 to 15:37, local time)
**Branch:** `automaton/8f040001`, at the base it was cut from (`var Version = "9.51.0"` in both files)
**Plan:** `docs/plans/2026-10-31-model-request-progress.md`
**Live check:** `docs/plans/2026-10-31-model-request-progress-live-check.md`

Every command ran from the repository root under `env -u TMUX -u TMUX_TMPDIR`, with output and the
build artifact in a throwaway folder (`.gate-tmp/`, removed at the end, so no
binary is left in the repository). No multiplexer server command was run and no process was ended
by name.

**Overall:** PASS

## Checks

| # | Check | Command | Result |
|---|---|---|---|
| 1 | go build | `go build -o .gate-tmp/datawatch ./cmd/datawatch/` | Passed, exit 0 |
| 2 | go vet | `go vet ./...` | Passed, exit 0, no output |
| 3 | go test, whole repository | `go test ./...` (background, log in the throwaway folder) | Passed, exit 0; 84 packages ok, none reported a failing test |
| 4 | node tests | `node --test internal/server/web/*.test.js` | Passed: 1859 tests, 1859 pass, 0 failed |
| 5 | locale parity and key coverage | `go test ./internal/server/ -run 'Locale' -v` and `-run 'RequestWatch'` | Passed. The 43 new keys (`settings_request_watch_*`, `request_watch_*`) are present in all five bundles (en, de, es, fr, ja); `TestRequestWatchDocsEverySettingsFieldCarriesTheDocsKey` and the locale parity tests pass |
| 6 | request watch package tests | `go test ./internal/requestwatch/ ./internal/config/ -run 'RequestWatch\|Tracker\|Watcher\|Source\|Text\|Status'` | Passed |
| 7 | internal-refs | `bash scripts/check-no-internal-refs.sh` | Passed |
| 8 | unscoped tmux | `bash scripts/check-no-unscoped-tmux.sh` | Passed |
| 9 | test daemon cleanup | `bash scripts/check-test-daemon-cleanup.sh` | Passed |
| 10 | leftover test daemons | `bash scripts/check-no-leftover-test-daemons.sh` | Passed (run after the whole test run, so nothing was left behind) |
| 11 | howto coverage | `bash scripts/check-howto-coverage.sh` | Passed |
| 12 | curated howtos | `bash scripts/check-curated-howtos.sh` | Passed (22 howtos) |
| 13 | lint | `make lint` (golangci-lint is on the PATH) | Passed on the second run, 0 issues; see the note below |
| 14 | config round trip of the eleven keys | cited, not rerun | Covered by check 1 of the live check report (`docs/plans/2026-10-31-model-request-progress-live-check.md`): defaults read on a fresh config, all eleven keys round-tripped on `PUT` then `GET /api/config` and on the Automata settings route, an explicit empty list kept apart from unset, eight bad values refused naming the key. The same round trip is a unit test (`TestRequestWatchConfigPutGetRoundTripEveryKey`, passed in check 5) |
| 15 | no model name, node name or the word PRD | `grep -rniwE` over `internal/requestwatch/`, `internal/config/request_watch.go` (source and tests) and the 43 new locale values in all five bundles | Passed: no match. Model family names, the sandbox node and LLM names and the word PRD do not appear |
| 16 | version lines unchanged | compared `var Version =` in `cmd/datawatch/main.go` and `internal/server/api.go` with the merge base; also `git diff` of `CHANGELOG.md` | Passed: both lines identical to the merge base (`9.51.0`); the `CHANGELOG.md` diff adds no heading of any kind, only three body lines |
| 17 | parity issue number recorded | `docs/plans/parity-issues/model-request-progress.md` | Passed: the file records `Number: 324` and the issue address `https://github.com/dmz006/datawatch-app/issues/324`, filed once |
| 18 | gate script | `bash .datawatch/verify/request-watch-final-gate.sh` | Passed (see the end of the run) |

## Notes

- **A lint finding in this feature was fixed.** The first `make lint` run reported one staticcheck
  finding, QF1008 (a redundant embedded field selector) at `internal/server/request_watch_views_test.go:100`, in a test file this
  feature added. The smallest fix was `v.Session.ID` to `v.ID`. After it the request watch tests in
  `internal/server` passed again and `make lint` reported 0 issues.
- **Version lines and `main`.** `main` has moved on to `9.54.0` since this branch was cut at
  `9.51.0`. The check is against the merge base, which is what "no version change on the branch"
  means; the merge helper takes the next patch above `main` and bumps a minor version at merge
  time, so the lines are not touched here.
- **Backend family in file names.** The signal source file is named for the node kind it reads
  (`internal/requestwatch/source_ollama.go`), as the plan's scope says; the model and node
  names are never hard-coded. The check 15 pattern deliberately leaves out that node kind.
- **Pre-existing failures.** None found. Every check passed on this branch, so no comparison run
  against `main` was needed.
- **Skipped.** Nothing was skipped. The live sandbox run itself was not repeated; the live check
  report is cited for the config round trip and the behaviour on a running daemon, as the task
  directs.
- **Not shown by any check here.** Same as in the live check report: the web page rendering in a
  browser, the GPU signal and the YAML file round trip against a running daemon are covered by
  unit and component tests only.
