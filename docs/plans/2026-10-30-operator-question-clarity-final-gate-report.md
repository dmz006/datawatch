# Operator question clarity — final gate report

Date: 2026-10-30

Scope: the change that makes operator questions easier to answer — a visible
trade-off line on every option, a context link in the answer dialog, a short
preview of each open question in the queue, and three quality modes (`off`,
`warn`, `refuse`) that check a question when it is raised.

This report was written by re-running every check from a clean working tree on the
branch (all earlier work already committed). Every shell command ran under
`env -u TMUX -u TMUX_TMPDIR`. Nothing was fixed in this task. `PASS` below means the
command exited 0 and printed no failure; `FAIL` means it did not.

## Overall result

FAIL — 15 of the 16 `.datawatch/verify/qc-*.sh` scripts exit 1 on this clean tree. Every
build, test, repository check and content check passes; each qc failure comes from the same
single check, explained under "Why the qc scripts fail" below. Per the task rules the
failure is recorded here and nothing was changed to hide it.

## Build

| Command | Result |
|---|---|
| `go build -o /dev/null ./cmd/datawatch/` | PASS |

## Go test packages (full package, `go test -count=1 -v`)

| Package | Result | Top-level tests passed | Failed | Skipped | Passed including subtests |
|---|---|---|---|---|---|
| `internal/autonomous` | PASS | 2478 | 0 | 8 | 4309 |
| `internal/server` | PASS | 1538 | 0 | 2 | 2100 |
| `internal/mcp` | PASS | 511 | 0 | 0 | 594 |
| `internal/router` | PASS | 504 | 0 | 0 | 650 |
| `internal/config` | PASS | 266 | 0 | 0 | 334 |
| `cmd/datawatch` | PASS | 973 | 0 | 1 | 1526 |
| Total | PASS | 6270 | 0 | 11 | 9513 |

## Node tests (`node --test`, one file at a time)

| File | Result | Tests | Passed | Failed |
|---|---|---|---|---|
| `internal/server/web/app-questions-answer.test.js` | PASS | 12 | 12 | 0 |
| `internal/server/web/app-questions-clarity.test.js` | PASS | 9 | 9 | 0 |
| `internal/server/web/app-questions-context-link.test.js` | PASS | 10 | 10 | 0 |
| `internal/server/web/app-questions-history.test.js` | PASS | 12 | 12 | 0 |
| `internal/server/web/app-questions-live.test.js` | PASS | 13 | 13 | 0 |
| `internal/server/web/app-questions-queue-preview.test.js` | PASS | 11 | 11 | 0 |
| `internal/server/web/app-questions-queue.test.js` | PASS | 29 | 29 | 0 |
| Total | PASS | 96 | 96 | 0 |

## Repository checks

| Command | Result |
|---|---|
| `scripts/check-no-internal-refs.sh` | PASS |
| `scripts/check-docs-sync.sh` | PASS |

## Confirmations

| Confirmation | Result | Evidence |
|---|---|---|
| The live-check report says PASS overall | PASS | `docs/plans/2026-10-30-operator-question-clarity-live-check.md`: "Checks: PASS" and "Overall: PASS for the feature under test". Its cleanup line says PARTIAL against the literal wording (the one answered question cannot be deleted through the API, so the sandbox data directory was removed instead); that deviation is recorded in that report. |
| The parity issue file records an issue number | PASS | `docs/plans/parity-issues/operator-question-clarity.md` records `dmz006/datawatch-app#309`, open, filed 2026-10-30. |
| The CHANGELOG entry has no tracker identifiers | PASS | The three lines added to `CHANGELOG.md` since the branch point match neither the `BL` nor the letters-and-digits identifier pattern. |
| The `Version` variables are unchanged against main | PASS | `internal/server/api.go` and `cmd/datawatch/main.go` both hold `9.42.2`, the same as at the branch point; no `Version` line differs between the branch point and the branch head. Main has since moved on to a later version, which is the merge helper's bump and not a change on this branch. |

## qc scripts (`.datawatch/verify/qc-*.sh`, all but `qc-final-gate.sh`)

Each script was run with `env -u TMUX -u TMUX_TMPDIR bash <script>`. All 15 exit 1.
"Checks ok" counts the `ok:` lines each script printed; "Checks failed" counts its
`FAIL:` lines. Every `FAIL:` line is the same message, "expected change not found",
and no other kind of failure was printed by any script.

| Script | Result | Checks ok | Checks failed | Files named in the failing lines |
|---|---|---|---|---|
| `qc-cli.sh` | FAIL | 5 | 1 | `cmd/datawatch/cli_questions.go` |
| `qc-comm.sh` | FAIL | 3 | 1 | `internal/router/questions_comm.go` |
| `qc-config-keys.sh` | FAIL | 7 | 3 | `internal/config/config.go`, `internal/server/api.go`, `docs/config-reference.yaml` |
| `qc-context-link.sh` | FAIL | 10 | 7 | `internal/server/web/app.js`, `internal/server/web/app-questions-context-link.test.js`, the five `internal/server/web/locales/*.json` bundles |
| `qc-context-ref.sh` | FAIL | 4 | 3 | `internal/autonomous/question_context_ref.go`, `internal/autonomous/question_context_ref_test.go`, `internal/server/questions.go` |
| `qc-dialog.sh` | FAIL | 9 | 7 | `internal/server/web/app.js`, `internal/server/web/app-questions-clarity.test.js`, the five locale bundles |
| `qc-docs-howto.sh` | FAIL | 5 | 2 | `docs/howto/operator-questions-answering.md`, `docs/datawatch-definitions.md` |
| `qc-docs-tracker.sh` | FAIL | 5 | 3 | `docs/testing-tracker.md`, `CHANGELOG.md`, `docs/parity-status.md` |
| `qc-live-check.sh` | FAIL | 8 | 1 | `docs/plans/2026-10-30-operator-question-clarity-live-check.md` |
| `qc-mcp.sh` | FAIL | 6 | 2 | `internal/mcp/questions_tools.go`, `docs/mcp.md` |
| `qc-parity-issue.sh` | FAIL | 5 | 1 | `docs/plans/parity-issues/operator-question-clarity.md` |
| `qc-quality-check.sh` | FAIL | 3 | 2 | `internal/autonomous/question_quality.go`, `internal/autonomous/question_quality_test.go` |
| `qc-queue-row.sh` | FAIL | 10 | 7 | `internal/server/web/app.js`, `internal/server/web/app-questions-queue-preview.test.js`, the five locale bundles |
| `qc-raise-route.sh` | FAIL | 6 | 2 | `internal/server/questions.go`, `internal/server/web/openapi.yaml` |
| `qc-settings-help.sh` | FAIL | 9 | 8 | `internal/server/web/app.js`, `internal/server/questions_quality_settings_test.go`, `docs/datawatch-definitions.md`, the five locale bundles |

Failing output, one line per file, as printed (for example from `qc-parity-issue.sh`):

```text
FAIL: expected change not found: docs/plans/parity-issues/operator-question-clarity.md
verification failed: 1 problem(s)
```

### Why the qc scripts fail

The shared helper `need_changed` in `.datawatch/verify/_lib.sh` treats a file as changed
only if it differs from `HEAD` or is untracked. These scripts were written to run
inside the task that edits the files. By the time of this gate every one of those files
was already committed by an earlier task, so on a clean tree the working copy equals
`HEAD` and the check reports "expected change not found" for each one. The other checks
in the same scripts (tests that exist and run, expected wording present, locale keys in
all five bundles, no tracker identifiers, no version edit, sandbox directory gone and
port closed) all printed `ok:`. This is recorded as FAIL because the scripts exit 1; it
was not worked around and the scripts and the files they name were not touched.

## Not run by this gate

- `qc-final-gate.sh` is the script that checks this report, so it is not part of the
  list above; its own result is reported separately by the task that runs it.
