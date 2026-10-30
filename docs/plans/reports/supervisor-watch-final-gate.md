# Supervisor watch — final gate

Date: 2026-10-30 (branch `automaton/85720001`), re-run after the rules-review fixes. All commands were run from the repository root under `env -u TMUX -u TMUX_TMPDIR` with Go 1.27.2 (linux/amd64).

Result: **PASS** for everything that could be run in this iteration; two items were **not run** and are named under "Not run".

## Commands run

| # | Command | Result |
|---|---|---|
| 1 | `go build ./...` | exit 0 |
| 2 | `go vet ./internal/autonomous ./internal/config ./internal/federation ./internal/mcp ./internal/router ./internal/server ./internal/stats ./internal/metrics ./cmd/datawatch` | exit 0 |
| 3 | `go test -count=1 ./internal/autonomous ./internal/server ./internal/mcp ./internal/router ./internal/config ./internal/federation ./internal/stats ./internal/metrics ./cmd/datawatch` | all nine packages `ok` (autonomous 32.9 s, server 68.0 s, router 108.6 s, cmd/datawatch 114.5 s) |
| 4 | `make lint` (the nine audit scripts, then `golangci-lint run ./...`) | `0 issues.`, exit 0 |
| 5 | `node --test internal/server/web/app-supervisor-watch.test.js internal/server/web/app-supervisor-watch-stats.test.js` | 13 pass, 0 fail |
| 6 | `bash scripts/check-docs-sync.sh` (after `make sync-docs`) | `docs/ and internal/server/web/docs in sync.` |
| 7 | `bash scripts/check-no-internal-refs.sh`, `check-curated-howtos.sh`, `check-howto-coverage.sh` | all PASS |
| 8 | absolute-home-path scan of the new test files, reports and docs | none found |
| 9 | `bash -n scripts/release-smoke.sh`, `node --check` on the new story | syntax ok |

## What the review found, and what was done

- **Lint** (six errcheck, three unused): fixed; `make lint` is clean.
- **Absolute home paths in test fixtures**: replaced with repository-relative synthetic values (`checkouts/main`).
- **Gate FAIL items**: the docs mirror was synced; the fixtures were fixed; `TestAttemptSnapshotCleansUpTempDir` (a shared-temp-directory flake, outside this branch's files) now waits up to three seconds for a snapshot another test still has in flight before calling its directory left behind. It passed in the full package run above.
- **Docs**: `docs/implementation.md` section, `docs/cursor-mcp.md` rows for `supervisor_watch_list` and `supervisor_watch_stats`, `docs/mcp.md`, README paragraph, operations, testing tracker rows, parity status rows, CHANGELOG (Added, Fixed, corrected "same fourteen checks" wording).
- **Smoke**: `scripts/release-smoke.sh` section 90 (REST read, stats read, POST is 405, comm verb, usage line).
- **Browser story**: `TS-840` (`pwa/supervisor-watch-card.mjs`).
- **Observability**: `GET /api/autonomous/supervisor-watch/stats`, MCP `supervisor_watch_stats`, `supervisor_watch_*` fields on `SystemStats`, a Monitor card, three Prometheus gauges, openapi and locale keys in all five bundles; the high-visibility keys are in `mustHave`.
- **Locale guard**: four supervisor watch keys added to `mustHave` in `v5280_locales_test.go`.
- **Script/watch gap**: documented in the how-to ("Where the watch and the script differ"), the glossary, operations and the CHANGELOG. The script's `MODEL host … unreachable` line is no longer recorded as `model_not_resident`.
- **`task_review_needed`**: now reads the tasks of running or blocked Automata instead of a triage record that never names a task held for review; the unit test uses the escalated shape the live run produced.
- **Adapter coverage**: new tests for the real host probe (`runCmd`, `ServiceRestartCount`, `ActiveSince`, `DaemonActive`, `ResidentModels`, `GitStatus`, `FileMode`), the real worktree probe, `RenderSupervisorWatch`, `SupervisorWatchStatus`, the store failure paths and the wiring seams. The watch's own files now average 96.7 percent over 127 functions; the `internal/autonomous` package as a whole is 85.5 percent (it holds a great deal of code this branch did not write).
- **Live-check set-up faults** (no trap machinery, production proxy port 8444): recorded as corrections for the next run in `supervisor-watch-shadow-live-check.md`.
- **Triage note in shadow**: now stated in the how-to and operations.

## Not run

- **`scripts/run-tests.sh --stories=TS-840` and `scripts/release-smoke.sh` section 90** against a daemon: the harness creates its working directory outside the repository, which this task's scope does not allow. They are syntax-checked only; the tracker marks them Tested "syntax" / Validated No.
- **A new sandbox live check of `task_review_needed`**: the fix is unit-tested only.

## Follow-ups outside this branch

- A comment on the datawatch-app parity issue (`dmz006/datawatch-app#307`) for the stats route and the Monitor card; the new section is already in `docs/plans/parity-issues/supervisor-watch.md`. Nothing was posted to GitHub from here.
