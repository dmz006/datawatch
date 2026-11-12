# Supervisor watch — final gate

Date: 2026-10-30 (branch `automaton/85720001`), re-run after the rules-review fixes. All commands were run from the repository root under `env -u TMUX -u TMUX_TMPDIR` with Go 1.27.2 (linux/amd64).

Result: **PASS** for everything that could be run in this iteration; two items were **not run** and are named under "Not run".

## Commands run

| # | Command | Result |
|---|---|---|
| 1 | `go build ./...` | exit 0 |
| 2 | `go vet ./internal/autonomous ./internal/config ./internal/federation ./internal/mcp ./internal/router ./internal/server ./internal/stats ./internal/metrics ./cmd/datawatch` | exit 0 |
| 3 | `go test -count=1 ./internal/autonomous ./internal/server ./internal/mcp ./internal/router ./internal/config ./internal/federation ./internal/stats ./internal/metrics ./cmd/datawatch` | all nine packages `ok` (autonomous 32.7 s, server 67.6 s, router 108.6 s, cmd/datawatch 113.8 s) |
| 4 | `make lint` (the nine audit scripts, then `golangci-lint run ./...`) | `0 issues.`, exit 0 |
| 5 | `node --test internal/server/web/app-supervisor-watch.test.js internal/server/web/app-supervisor-watch-stats.test.js` | 13 pass, 0 fail |
| 6 | `bash scripts/check-docs-sync.sh` (after `make sync-docs`) | `docs/ and internal/server/web/docs in sync.` |
| 7 | `bash scripts/check-no-internal-refs.sh`, `check-curated-howtos.sh`, `check-howto-coverage.sh` | all PASS |
| 8 | absolute-home-path scan of the new test files, reports and docs | none found |
| 9 | `bash -n scripts/release-smoke.sh`, `bash -n scripts/supervisor-watch-sandbox-check.sh`, `node --check` on the new story | syntax ok |
| 10 | `bash scripts/supervisor-watch-sandbox-check.sh` (builds this tree, starts a throwaway daemon) | `ALL PASS`: traps installed, proxy listener disabled with no 8444 bind, watch endpoint 200 in shadow, `main_checkout_dirty` raised live without an alert, config restored |

## What the review found, and what was done

- **Lint** (six errcheck, three unused): fixed; `make lint` is clean.
- **Absolute home paths in test fixtures**: replaced with repository-relative synthetic values (`checkouts/main`).
- **Gate FAIL items**: the docs mirror was synced; the fixtures were fixed; `TestAttemptSnapshotCleansUpTempDir` failed because it scanned the system temp directory, which every package binary `go test ./...` runs at the same time shares (the daemon's attempt-preserve path snapshots there too), and counted another process's in-flight snapshot as a leak; the test now sets `TMPDIR` to its own directory, with no wait. It passed in the full run above and 20 times in a row alone. It passed in the full package run above.
- **Docs**: `docs/implementation.md` section, `docs/cursor-mcp.md` rows for `supervisor_watch_list` and `supervisor_watch_stats`, `docs/mcp.md`, README paragraph, operations, testing tracker rows, parity status rows, CHANGELOG (Added, Fixed, corrected "same fourteen checks" wording).
- **Smoke**: `scripts/release-smoke.sh` section 90 (REST read, stats read, POST is 405, comm verb, usage line).
- **Browser story**: `TS-840` (`pwa/supervisor-watch-card.mjs`).
- **Observability**: `GET /api/autonomous/supervisor-watch/stats`, MCP `supervisor_watch_stats`, `supervisor_watch_*` fields on `SystemStats`, a Monitor card, three Prometheus gauges, openapi and locale keys in all five bundles; the high-visibility keys are in `mustHave`.
- **Locale guard**: four supervisor watch keys added to `mustHave` in `v5280_locales_test.go`.
- **Script/watch gap**: documented in the how-to ("Where the watch and the script differ"), the glossary, operations and the CHANGELOG. The script's `MODEL host … unreachable` line is no longer recorded as `model_not_resident`.
- **`task_review_needed`**: now reads the tasks of running or blocked Automata instead of a triage record that never names a task held for review; the unit test uses the escalated shape the live run produced.
- **Coverage**: every statement in the watch's own files (`internal/autonomous/supervisor_watch*.go`, 1160 statements) is now covered: 0 uncovered, measured with `-coverprofile`. That took tests for the real host and worktree probe adapters, the store failure paths, the render and status functions, the poll's save-failure and alert-dedupe paths and every work-check suppression, plus removing defensive branches that could not be reached (a nil check on the address of a slice element, a marshal error on a plain struct, a cloned record that cannot disappear under the lock). The `internal/autonomous` package as a whole measures 85.8 percent: the other 3,500 or so uncovered statements are code this branch did not add or change, and raising them is outside this change.
- **Live-check set-up faults** (no trap machinery, production proxy port 8444): corrected in a new script, `scripts/supervisor-watch-sandbox-check.sh`, which installs `td_install_traps` first, records the daemon, sets `server.proxy_sandbox_port: 0` and fails on any 8444 bind attempt. It was run (row 10 above); the report `supervisor-watch-shadow-live-check.md` records the run, and one fault of the script's own first draft (a `sed` that broke the bootstrap file so the daemon ran on default ports; its binds failed because the production daemon holds them, and nothing was sent to it), which the script now guards against.
- **Watch-only kinds**: the how-to now calls `worktree_not_registered` and `verify_script_not_executable` "watch-only" (the script never prints a line for either, so the comparison lists them under only the watch).
- **Triage note in shadow**: now stated in the how-to and operations.

## Not run

- **`scripts/run-tests.sh --stories=TS-840` and `scripts/release-smoke.sh` section 90** against a daemon: the harness creates its working directory outside the repository, which this task's scope does not allow. They are syntax-checked only; the tracker marks them Tested "syntax" / Validated No.
- **A new sandbox live check of `task_review_needed`**: the fix is unit-tested only.

## Follow-ups outside this branch

- A comment on the datawatch-app parity issue (`dmz006/datawatch-app#307`) for the stats route and the Monitor card; the new section is already in `docs/plans/parity-issues/supervisor-watch.md`. Nothing was posted to GitHub from here.

## Commit message tokens (AGENT.md per-sprint audit)

The commit subject is the conventional `type(scope)` form and carries the audit tokens:

`rules: Testing Tracker Rule, Localization Rule, Mobile-Parity Rule, Monitoring Rule, Federation-Parity Rule` · `tests: ok` · `lint: clean` · `tracker: Supervisor watch — monitoring, the real probe adapters, the smoke section and the browser story` · `locale-guard: added supervisor_watch_title, supervisor_watch_no_findings, settings_supervisor_watch_mode, stats_supervisor_watch` · `mobile-parity: datawatch-app#307` · `config-parity: verified` · `fed-parity: verified` · `observability: added` · `smoke-extended: 90. Supervisor watch` · `changelog: added`
