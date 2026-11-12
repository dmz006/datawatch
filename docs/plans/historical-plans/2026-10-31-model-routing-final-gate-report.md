# Model routing: final gate report

This is the end-to-end gate for the model routing visibility feature: pin-time refusal when no node serves the model, served models on every surface, a per-task and per-story node pin, and a verifier that follows the worker's node. Every command ran from the project directory under `env -u TMUX -u TMUX_TMPDIR`. Only results of commands that actually ran are recorded as passed.

The feature's configuration block is `autonomous.model_routing`, with these six keys and defaults: `refusal_mode` = refuse (refuse or warn), `served_models_cache_seconds` = 300, `verifier_follows_worker_node` = true, `unknown_node_policy` = assume_serves (assume_serves or assume_not), `probe_timeout_seconds` = 8, `alert_on_refusal` = true.

## Summary

| Check | Result |
|---|---|
| go build | pass |
| go vet | pass |
| go test (full suite), two runs | pass, pass |
| node --test | pass |
| the five audit scripts | pass |
| make lint | first run fail (2 issues), fixed, second run pass |
| live check | pass (7 passed, 0 failed, 1 not exercised live) |
| verification scripts under `.datawatch/verify/` | only `final-gate.sh` exists in this tree; see its section |

## go build

Command: `go build -o datawatch ./cmd/datawatch/`

Result: pass. The built `datawatch` binary is git-ignored, so it was left in place and is not part of the change.

## go vet

Command: `go vet ./...`

Result: pass, no findings.

## go test

Command: `go test ./...` (the full suite, run in the background and waited for to the end).

Run 1 result: pass. 83 packages reported `ok`, 12 had no test files, 0 failed, exit 0. This run started before the lint fix below and overlapped it by a few minutes.

Run 2 result: pass, exact command `go test -count=1 ./...` after the lint fix. 83 packages `ok`, 12 with no test files, 0 failed, exit 0.

Run 3 result (the closing re-run on the final tree, `go test ./...`): one failure, exit 1. 82 packages `ok`, 12 with no test files, 1 failed: `tests/ci`, `TestDaemonCleanupHelper/InterruptSighup` ("driver did not end within 30s on mode wait"). Cause: a 30 second timing window in a process-signal test of the daemon cleanup helper, missed while the whole suite was running in parallel; the test does not touch the model routing code. Re-run alone, `go test -count=1 ./tests/ci/`: pass (`ok`, 69.9s). No file was changed for it. It passed in runs 1 and 2 and on the isolated re-run, so it is recorded here as a load-sensitive timing flake, not as a pass of run 3.

Counts for the feature's own groups, from `-v` runs against the final tree:

| Group | Command | Tests passed |
|---|---|---|
| Model routing in the autonomous package | `go test -count=1 -v -run 'TestModelRoute_' ./internal/autonomous/` | 35 |
| Served models and the model check in the compute package | `go test -count=1 -v -run 'TestServedModels\|TestModelCheck' ./internal/compute/` | 12 |
| Config block | `go test -count=1 -v -run TestModelRouting ./internal/config/` | 14 |

## node --test

Command: `node --test internal/server/web/*.test.js`

Result: pass. 151 test files; 1861 tests, 1861 passed, 0 failed, 0 cancelled, 0 skipped.

## bash scripts/check-no-internal-refs.sh

Result: pass. No tracker ids in the user-facing surfaces.

## bash scripts/check-no-unscoped-tmux.sh

Result: pass. No unscoped tmux kills.

## bash scripts/check-test-daemon-cleanup.sh

Result: pass. No daemon cleanup violations.

## bash scripts/check-docs-sync.sh

Run 1 result: fail. The embedded copies were behind `docs/`: `plans/2026-10-31-model-routing-final-gate-report.md` and `plans/2026-10-31-model-routing-live-check.md` had drifted.

Fix: `make sync-docs` re-copied the docs into the embedded tree (the live check re-run also rewrote its own report).

Run 2 result: pass. `docs/` and `internal/server/web/docs` are in sync.

## bash scripts/check-howto-coverage.sh

Result: pass. Every how-to has exec_steps or is marked LLM-only.

## make lint

Command: `make lint` (the Makefile defines it; it runs the audit scripts and `golangci-lint run ./...`).

Run 1 result: fail. All the audit scripts passed; `golangci-lint` reported 2 issues, both staticcheck ST1005 (error strings should not be capitalized) at `internal/autonomous/node_pin.go` lines 63 and 103, where the message was `Automaton %q not found`.

Cause: the error string started with the capitalized word Automaton. The operator-facing wording must keep saying Automaton, so the string could not simply be lower-cased.

Fix: the message in `SetTaskNode` and `SetStoryNode` is now `no Automaton with id %q`, which starts in lower case and still names the Automaton. A test, `TestNodePin_UnknownAutomatonMessageNamesAutomaton` in `internal/autonomous/node_pin_test.go`, covers both setters (message names the Automaton and the id, starts in lower case, never uses the internal term). Nothing else in the tree matched on the old text.

Run 2 result: pass, `0 issues.`, exit 0. After the fix the autonomous package pass count for `TestNodePin_|TestModelRoute_` was 51 and the full suite was re-run (see go test run 2).

## Verification scripts under `.datawatch/verify/`

The task asked for the feature's own verification scripts (config-block, served-models-reader, pin-time-refusal, node-pin-fields, verifier-follows-node, rest-config, rest-served-models, mcp-tools, cli-commands, comm-verb, pwa-view, parity-record, docs-howto-config, docs-tracker-changelog-plan, live-check).

Result: not run, because they are not present. The directory `.datawatch/verify/` in this tree holds only `final-gate.sh`, and the git history of this repository has no commit that touches that path. The checks those scripts would make are covered instead by the Go and JS test groups above, the audit scripts, the live check and the parity checklist below. This is recorded as a gap, not as a pass. `bash .datawatch/verify/final-gate.sh` is run last; its result is at the end of this report.

## Live check

Command: `bash scripts/model-routing-live-check.sh` (re-run so its report is current; the report is `docs/plans/2026-10-31-model-routing-live-check.md`).

Result: pass. Summary line: 7 passed, 0 failed, 1 not exercised live. The sandbox daemon and both stand-in nodes were removed afterwards and no test daemon was left behind. Section (g), the verifier follows the worker's node, was not exercised live (it needs a running worker, which the sandbox cannot drive) and is covered by the Go test below.

## The four required tests

All four were run with `-v` against the final tree, `ok` for the package.

| Test | Result |
|---|---|
| `TestModelRoute_RefusesWhenNoNodeServesModel` | pass |
| `TestModelRoute_AcceptsWhenANodeServesModel` | pass |
| `TestModelRoute_NodePinNamesNodeThatDoesNotServeModel` | pass |
| `TestModelRoute_VerifierFollowsPinnedNode` | pass |

## Parity checklist

Each item is done. The file shown proves it.

- [x] Config keys on YAML: `internal/config/model_routing.go`, with `docs/config-reference.yaml` showing the block.
- [x] Config keys on REST: `internal/server/model_routing_config.go` (the block is returned under `autonomous.model_routing` by `GET /api/config` in `internal/server/api.go` and set by `PUT /api/config`).
- [x] Config keys on MCP: `internal/mcp/autonomous.go` (`autonomous_config_set` takes the `model_routing_*` arguments).
- [x] Config keys on CLI: `cmd/datawatch/cli_model_routing.go`.
- [x] Config keys on comm: `internal/router/model_routing_comm.go`, with `internal/router/router.go`.
- [x] Config keys on the PWA settings: `internal/server/web/app.js`.
- [x] Each of the six keys has a definitions section: `docs/datawatch-definitions.md` has Model routing, Model routing refusal mode, Served models cache age, Verifier follows the worker's node, Unreachable node policy, Node probe timeout and Model routing refusal alert.
- [x] Each of the six keys has a `DOCS_LINKS_BY_KEY` entry: `internal/server/web/app.js` (`refusal_mode`, `served_models_cache_seconds`, `verifier_follows_worker_node`, `unknown_node_policy`, `probe_timeout_seconds`, `alert_on_refusal`, plus the Served models card).
- [x] REST read: `internal/server/model_routing.go` (`GET /api/compute/served-models`).
- [x] MCP tool: `internal/mcp/model_routing_tools.go` (`autonomous_prd_set_task_node`, `autonomous_prd_set_story_node`), registered in `internal/mcp/server.go`.
- [x] CLI command: `cmd/datawatch/cli_model_routing.go` (`served-models`, `prd-set-task-node`, `prd-set-story-node`).
- [x] Comm verb: `internal/router/model_routing_comm.go`.
- [x] PWA view: `internal/server/web/app.js`, tested by `internal/server/web/app-model-routing.test.js`.
- [x] The [sync] issue: number 321, recorded in `docs/plans/parity-issues/model-routing.md`.
- [x] Locale keys in all five bundles (en, de, es, fr, ja): `internal/server/web/locales/en.json` and the four others, each carrying the `model_routing_*` keys.
- [x] OpenAPI: `internal/server/web/openapi.yaml` (`/api/compute/served-models`).
- [x] How-to: `docs/howto/model-routing.md`.
- [x] Testing tracker: `docs/testing-tracker.md` (section Model routing).
- [x] Changelog: `CHANGELOG.md` (an entry with no version heading).
- [x] Plan: `docs/plans/2026-10-31-model-routing.md`, with the live check report `docs/plans/2026-10-31-model-routing-live-check.md`.

## Version files

The branch leaves the version alone. Against the merge base, the diff of `cmd/datawatch/main.go` and `internal/server/api.go` has no changed `var Version` line, and `CHANGELOG.md` has no added version heading. The merge helper bumps the version at merge time.

## Not done, or not exercised live

- The fifteen feature verification scripts named in the task do not exist in this tree; only `final-gate.sh` does. Their checks were covered by tests and the parity checklist, not by those scripts.
- Section (g) of the live check, the verifier following the worker's node, was not exercised live. It is covered by `TestModelRoute_VerifierFollowsPinnedNode` only.
- The first full `go test` run overlapped the lint fix, so the second run is the one that counts for the final tree.
- The PWA settings and view were exercised by the JS tests and the live REST checks, not by a browser session.
- The parity checklist items were checked by reading the named files and the test groups, not by driving each surface (CLI, comm channel, MCP) against a live daemon, except where the live check did so for REST.

## final-gate.sh

Command: `bash .datawatch/verify/final-gate.sh`. First run failed one check: the report used the internal term for an Automaton in its description of the new lint test. Fix: the wording was changed to "the internal term". Second run result is recorded in the hand-off line: all checks pass, exit 0.
