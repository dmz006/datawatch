# Operator question channel — Step 4 final gate report (worker marker)

Date: 2026-10-29 · Branch: `automaton/f1f200be`

Scope: the final gate for the worker marker. A worker session prints one line that starts with `DATAWATCH_QUESTION:` followed by one line of JSON. When `autonomous.questions.worker_marker` is on (and the question channel is on), the daemon raises it as an operator question, parks the task on it if the worker said it is blocked, and resumes the task from the coordinator tick once the question is answered or withdrawn. No code was changed by this task; it only ran checks and wrote this report.

Every command ran under `env -u TMUX -u TMUX_TMPDIR`, from the worktree root.

## Commands run

| # | Command | Exit | Result |
|---|---------|------|--------|
| 1 | `gofmt -l` on the 38 Go files this branch changed (file list from `git diff --name-only main...HEAD`, never a package) | 0 | Printed nothing: every file is formatted |
| 2 | `go build ./internal/... ./cmd/...` | 0 | No output |
| 3 | `go vet` on the 8 touched packages: `cmd/datawatch`, `internal/audit`, `internal/autonomous`, `internal/config`, `internal/mcp`, `internal/router`, `internal/server`, `internal/session` | 0 | No output |
| 4 | `go test ./...` (the full suite) | 0 | 82 packages `ok`, no `FAIL`, 170 s |
| 5 | `make lint` | 0 | Service-control, toolchain-pin and internal-ref audits all PASS; `golangci-lint run ./...` printed `0 issues.` |
| 6 | `bash .datawatch/verify/worker-question-marker-story.sh` | 0 | `result: PASS` (6 packages `ok` on `WorkerQuestion\|WorkerMarker`, then `go build`) |
| 7 | `bash .datawatch/verify/worker-question-decisions-story.sh` | 0 | `result: PASS` |
| 8 | `bash .datawatch/verify/operator-question-surfaces-story.sh` | 0 | `result: PASS` (locale guard `TestLocales`, `node --check` of `app.js`) |
| 9 | `bash .datawatch/verify/worker-question-marker-parser.sh` | 0 | `result: PASS` |
| 10 | `bash .datawatch/verify/worker-question-marker-scanner.sh` | 0 | `result: PASS` |
| 11 | `bash .datawatch/verify/worker-marker-config-switch.sh` | 0 | `result: PASS` |
| 12 | `bash .datawatch/verify/worker-question-guardrail-text.sh` | 0 | `result: PASS` |
| 13 | `bash .datawatch/verify/worker-question-raise-audit.sh` | 0 | `result: PASS` |
| 14 | `bash .datawatch/verify/await-decision-park.sh` | 0 | `result: PASS` |
| 15 | `bash .datawatch/verify/question-tick-resume.sh` | 0 | `result: PASS` |
| 16 | `bash .datawatch/verify/pwa-question-decision-link.sh` | 0 | `result: PASS` (includes `node --check` and `node --test`) |
| 17 | `bash .datawatch/verify/question-step4-docs.sh` | 0 | `result: PASS` |
| 18 | `bash .datawatch/verify/question-step4-live-check.sh` | 0 | `result: PASS` |
| 19 | `make sync-docs` | 0 | No output (regenerates the gitignored embedded docs copy) |
| 20 | `bash scripts/check-docs-sync.sh` | 0 | `docs/ and internal/server/web/docs in sync.` |
| 21 | `node --check internal/server/web/app.js` | 0 | No output |
| 22 | `node --check internal/server/web/app-question-decision-link.test.js` | 0 | No output |
| 23 | `node --check scripts/test-stories/pwa/question-decision-link.mjs` | 0 | No output |
| 24 | `node --test internal/server/web/app-question-decision-link.test.js` | 0 | 18 tests, 18 pass, 0 fail, 0 skipped |
| 25 | Live check report `docs/plans/2026-10-29-operator-question-step4-live-check.md`: read, steps counted | n/a | Exists, has a `## Verdict` section, 10 verdict rows each `**PASS**`, no `FAIL`, no production port named (see "Live check" below) |
| 26 | `bash .datawatch/verify/question-step4-final-gate.sh` (this task's own script, run once this report existed) | 0 | `result: PASS` (206 s; see "Final-gate script" below) |

Every `.datawatch/verify/` script in the directory is a task of this PRD (rows 6 to 18, plus 26). No other script is in that directory.

### Final-gate script

`.datawatch/verify/question-step4-final-gate.sh` exited 0 with `result: PASS`. It found the report with its `## Parity surface` and `## Verdict` sections, then re-ran, each exit 0: `go build ./internal/... ./cmd/...`, `go test ./...` (a second full-suite pass, about 170 s, no failure), `make lint`, `make sync-docs` and `scripts/check-docs-sync.sh` (`docs/ and internal/server/web/docs in sync.`). The report is the only uncommitted file it checks, and it has no Go file, so its gofmt/vet step had nothing to do.

### Live check

The live check report was not re-run here (it needs a throwaway daemon and this task changes no code). It was read in full, and its claims were checked against the rest of this gate:

- It reports PASS on all 10 steps it claims: both switches set and read back, a valid line raising one open question (source `marker`, not blocking, default `hold`), an instruction sentence naming the marker raising nothing, a malformed line refused and audited with a reason and a hash and never its text, a placeholder line ignored, `question_raised` and `question_marker_refused` in the audit log, a quote with a newline and a double quote stored and returned byte for byte, marker-off raising nothing, a restart with no duplicate and the config kept, and the daemon stopped by exact PID with the sandbox directory deleted.
- It names what it did **not** run live, and this gate does not claim those either: the park path of a blocking question (covered by the Go tests `TestAwaitDecision_*`, `TestQuestionTick_*` and `TestWaitReason_AwaitDecisionTextAndPredicate`, not validated live), a worker of a real Automaton with a task id, the raise notifications for a marker question, the PWA toggle, the MCP/CLI/comm config surfaces (only REST was driven live; the others are covered by the Go tests in the checklist below), the restart-grace and task-echo windows live, the size caps and other refusal reasons live, and the same-line de-duplication of a pane read twice (Go tests only).

## Scope checklist

Each requirement, mapped to the file that implements it and the test that proves it. Every test named here is inside the full suite of row 4 and the focused runs of rows 6 to 18.

| Requirement | Implemented in | Proven by |
|---|---|---|
| **The marker** — a line starting with `DATAWATCH_QUESTION:` plus one line of JSON is recognised; instruction sentences, placeholders and malformed lines are not raised; size caps and field rules enforced | `internal/session/question_marker.go` (`classifyWorkerQuestion`, `parseWorkerQuestionPayload`); routed from the output scanner at `internal/session/manager.go:7836` (`routeWorkerQuestion`); raised into the store by `RaiseWorkerQuestion` in `internal/autonomous/worker_question.go` | `internal/session/question_marker_test.go`: `TestWorkerQuestionValidLineFillsEveryField`, `TestWorkerQuestionNotRealLines`, `TestWorkerQuestionRecognitionForms`, `TestWorkerQuestionRefusals`, `TestWorkerQuestionCapsRefused`, `TestWorkerQuestionCapsAcceptedAtLimit`, `TestWorkerQuestionReasonIsNeverLineText`, `TestWorkerQuestionLineHashIsOfExactBytes`. Scanner: `internal/session/manager_worker_question_test.go` (`TestWorkerQuestion_GateOffNoHookCall`, `_RestartGraceDropsIt`, `_TaskEchoWindowDropsIt`, `_InstructionAndPlainLinesAreIgnored`, `_RefusedLineCallsHookWithReasonOnly`, `_NeedsInputStillWaitsWithGateOn`). Store: `internal/autonomous/worker_question_test.go` (`TestWorkerQuestionRaisesRecordFromEvent`, `_ContextAndDefaults`, `_InvalidDefaultIsRefused`, `_RefusalAuditsHashNeverTheLine`, `_DedupesOpenQuestionBySessionAndLine`, `_DedupeHoldsUnderConcurrency`). Audit: `internal/audit/question_marker_refused_test.go`. Live: live check rows 7 to 10 |
| **The guardrail text** — the session guardrail tells a worker how and when to print the marker, next to the unchanged needs-input text | `internal/session/tracker.go` (the guardrail text) and `templates/session-CLAUDE.md` (the same bullet in the template) | `TestWorkerQuestionGuardrail` in `internal/session/tracker_test.go` (needs-input marker kept, the marker only inline, no repo-only file names in the text); `worker-question-guardrail-text.sh` |
| **Park or continue** — `blocked: true` parks the task on the open question without a retry or failure and gives its slot back; `blocked: false` keeps the worker working | `internal/autonomous/await_decision.go` (`OpenBlockingQuestionForTask`, `ParkForDecision`), `internal/autonomous/await_external.go`, `internal/autonomous/wait_reason.go`, `internal/autonomous/task_visibility.go` | `internal/autonomous/await_decision_test.go`: `TestAwaitDecision_ParksWithReasonAndReleasesLease`, `_RetryCountUnchangedAndNoFailure`, `_ParksWithoutCapacityLedger`, `_TreeLockKeptWhileUnpushed`, `_TreeLockReleasedWhenClean`, `_SecondParkOfSameQuestionChangesNothing`, `_NeverParksWithoutTaskOrQuestion`, `_OnlyOpenBlockingQuestionOfTheTaskMatches`. Visibility: `TestWaitReason_AwaitDecisionTextAndPredicate`, `TestTaskVisibility_EveryWaitCauseHasText`. Live: not validated live (named in the live check) |
| **The tick read** — the coordinator tick reads open and answered questions, resumes a parked task exactly once on an answer or a withdrawal, never reads session output | `internal/autonomous/question_tick.go` (`ReadQuestionsForTick`, `QuestionTick`, `questionTickTask`, `resumeForQuestion`, `BuildDecisionResumeLine`); called from `internal/autonomous/automation_idle.go` and `internal/autonomous/manager.go` | `internal/autonomous/question_tick_test.go`: `TestQuestionTick_OpenQuestionLeavesTaskParked`, `_AnsweredResumesExactlyOnceAcrossTwoTicks`, `_WithdrawnResumesWithTheWithdrawnNote`, `_NeverReadsSessionOutput`, `_SupersedingAnswerDoesNotResumeTwice`, `_TwoAnswersBeforeATickResumeOnceWithTheNewest`, `_InFlightGuardHoldsAndTickerDoesNotWait`, `_FailedReAdmissionIsRetriedByALaterTick`, `_NoStoreIsANoOp`, `_AwaitDecisionQuestionID`, `_ResumeLineStaysOneLine`; list read in `internal/server/questions_tick_list_test.go` (`TestQuestionTick`); `question-tick-resume.sh` |
| **The decision link** — an `operator_answer` entry in the Automaton decision log carries the question and answer ids; the PWA shows the answer id as a link that opens the quote panel | `internal/autonomous/decision_log.go` (the `operator_answer` entry with `question_id` and `answer_id`, from an earlier step), `internal/autonomous/worker_question.go` (a worker question also gets an `operator_decision` entry when raised), `internal/server/web/app.js` (answer link at line 25352 on, `openDecisionAnswerPanel`) | `TestWorkerQuestionGainsOperatorDecisionEntry` (`internal/autonomous/worker_question_test.go`); `internal/server/web/app-question-decision-link.test.js` (link carries both ids, no link on other entries, hostile ids escaped, quote verbatim, not-on-record and failed-fetch lines, request path encoded); browser story `scripts/test-stories/pwa/question-decision-link.mjs` |
| **The switch defaults off** — `autonomous.questions.worker_marker` is off when the key is absent, does nothing while the channel is off, and is read live | `internal/config/config.go:3178` (`WorkerMarker *bool`) and `:3221` (`EffectiveWorkerMarker`); gate wired in `cmd/datawatch/main.go:6694` as `Enabled && EffectiveWorkerMarker()`; REST read/write in `internal/server/api.go` (`:6270`, `:6692`, `:8189`) | `internal/config/questions_worker_marker_test.go`: `TestQuestionsWorkerMarkerDefaultOff`, `_EffectiveHelper`, `_YAMLRoundTripKeepsExplicitValues`, `_JSONRoundTripKeepsExplicitValues`; `internal/server/questions_worker_marker_config_test.go`: `TestQuestionsWorkerMarkerConfig_RESTGetDefaultsOff`, `_RESTPutThenGetRoundTrip`, `_RESTNonBooleanRejected`; `TestWorkerQuestion_GateOffNoHookCall`; live check rows 3, 4, 13, 15, 16, 19, 20 |
| **The PWA link** — a Settings toggle for the key and the answer link (see the decision link row), with strings in all five locales | `internal/server/web/app.js:16396` (toggle), `internal/server/web/locales/{en,de,es,fr,ja}.json` | `internal/server/web/app-question-decision-link.test.js` ("the worker marker switch is a toggle in the Automata settings, with help text", and one per-locale test for the ten new keys); `internal/server/v5280_locales_test.go` (new keys in the locale guard); `pwa-question-decision-link.sh`, `operator-question-surfaces-story.sh` |
| **Parity** — the new key and fields reach every surface that can reach them, and the mobile apps are tracked | See "Parity surface" below | Config on MCP: `internal/mcp/question_worker_marker_config_test.go` (`TestQuestionWorkerMarkerConfig_MCPSetSwitch`); CLI: `cmd/datawatch/question_worker_marker_config_test.go` (`_CLISetSwitch`, `_CLIGetReadsSwitch`); comm channel: `internal/router/question_worker_marker_config_test.go` (`_ConfigureApplyPath`, `_MentionedInConfigureHelpAndList`); REST and YAML as in the switch row; parity record `docs/plans/parity-issues/operator-question-step4-worker-marker.md` |
| **Tests** — unit, surface, PWA and story tests for each requirement above | All the test files named in this table | Rows 4 and 6 to 18 of the command table: full suite exit 0, 82 packages ok, every story script PASS; 18 of 18 node tests pass |
| **Docs** — how-to, API, config reference, operations, testing, changelog, parity record | `docs/howto/operator-questions-workers.md` (new), `docs/howto/README.md`, `docs/api/autonomous.md`, `docs/api/openapi.yaml`, `docs/config-reference.yaml`, `docs/datawatch-definitions.md`, `docs/implementation.md`, `docs/operations.md`, `docs/testing.md`, `docs/testing-tracker.md`, `CHANGELOG.md` (Unreleased), `docs/plans/parity-issues/operator-question-step4-worker-marker.md` | `question-step4-docs.sh` PASS; `scripts/check-docs-sync.sh` exit 0 (row 20); `make lint` internal-ref audit PASS (row 5) |
| **The live check** — a sandbox daemon run of the marker | `docs/plans/2026-10-29-operator-question-step4-live-check.md` | Row 25: exists, 10 of 10 rows PASS, names the steps it did not run; `question-step4-live-check.sh` PASS |

## Parity surface

| Surface | Changed? | Reason |
|---|---|---|
| REST | Yes | Three new fields on the question record (`blocking`, `source`, `source_line_sha256`), the `await_decision` value for the waiting-task `cause` in the capacity status, and the new key on `GET`/`PUT /api/config`. No new route: a worker question is read and answered on the existing `/api/questions` routes. Tests: the REST tests in the switch row, `TestWorkerQuestionFieldsPersistAndOldRecordsReplay`. |
| MCP | Config only | No new tool. `question_list` and `question_get` return the record, so the new fields appear there; the key is set with `config_set` and read with `get_config`. A marker is raised from a session's output, so a `question_raise` call is not part of it. Test: `TestQuestionWorkerMarkerConfig_MCPSetSwitch`. |
| CLI | Config only | No new verb or flag. `datawatch config set autonomous.questions.worker_marker true` and `config get` reach the key. The CLI has no question-answer verb and this step adds none. Tests: `TestQuestionWorkerMarkerConfig_CLISetSwitch`, `_CLIGetReadsSwitch`. |
| Comm channel | Config only | No new command. `configure autonomous.questions.worker_marker=true` sets it, and the configure help and list name it. Raising is done by the worker's marker, not by a chat verb. Tests: `TestQuestionWorkerMarkerConfig_ConfigureApplyPath`, `_MentionedInConfigureHelpAndList`. |
| YAML/config | Yes | New key `autonomous.questions.worker_marker` (boolean, off when absent, read live), in `docs/config-reference.yaml`. Tests: the YAML and JSON round-trip tests in `internal/config/questions_worker_marker_test.go`. |
| PWA | Yes | A Settings toggle for the key and the answer link with its quote panel on `operator_answer` entries, with strings in all five locale bundles. Tests: `app-question-decision-link.test.js`, `TestLocales*`, `scripts/test-stories/pwa/question-decision-link.mjs`. |
| Android | Yes, not built | Needs the settings toggle, the waiting-for-decision display and the answer link. Tracked in `docs/plans/parity-issues/operator-question-step4-worker-marker.md` with acceptance criteria. The issue is **not filed**: it is filed only when the operator says so. |
| iPhone | Yes, not built | The same as Android, with the Dynamic Type and VoiceOver criteria. Tracked in the same record; **not filed** until the operator says so. |

## Constraint compliance

- **No tmux server commands.** None was run in this task. Every command above ran under `env -u TMUX -u TMUX_TMPDIR`. The live check report records its own tmux note (the sandbox daemon opened panes on the default socket because `TMUX_TMPDIR` was unset by rule; no tmux command was run by hand).
- **No kill by name.** No `kill`, `pkill` or `killall` was run in this task. The live check stopped its sandbox daemons by exact PID and confirmed both gone; the production daemon and its port were not touched. A search of the lines this branch added found no `pkill`, `killall` or `tmux kill` anywhere.
- **No edits to `AGENT.md` or to a project rules file.** `AGENT.md` and the repository root `CLAUDE.md` are not in the branch diff. The branch does change `templates/session-CLAUDE.md`; that is the template of the session guardrail text, which is the deliverable of the "guardrail text" requirement, not a project rules file, and it carries the same bullet as `internal/session/tracker.go`.
- **No version number on the branch.** `var Version` in `cmd/datawatch/main.go` and in `internal/server/api.go` is not in the branch diff (both read `9.35.0` here, the value at the branch point). The changelog entries sit under Unreleased, with no new version heading. Note: `main` has moved on since the branch point (its `main.go` reads `9.37.0`), so the merge must take the version from `main`; nothing on this branch pins one.
- **No tracker IDs in user-facing strings.** A search of the non-test Go lines, the template, the five locale files and the added doc lines for tracker IDs (BL, B, F, S plus digits) and for the word PRD found none; `make lint` ran the internal-ref audit (`no internal refs in user-facing surfaces`) and the locale guard ran in the full suite.
- **Files written by this task:** this report only (plus `CHECKPOINT.md`, scratch state, not part of the deliverable). `make sync-docs` regenerated the gitignored embedded docs copy. No code was changed.

## Verdict

**PASS.** All 26 rows of the command table ended with exit 0 or the stated result: gofmt clean on all 38 changed Go files, build 0, vet 0 on the 8 touched packages, the full `go test ./...` exit 0 twice (82 packages ok), `make lint` 0 issues, all 13 task and story scripts PASS plus the final-gate script, docs sync in sync, `node --check` clean on the three PWA files, `node --test` 18 of 18, and the live check report shows 10 of 10 PASS. Every operator requirement in the scope checklist maps to a file and a passing test.

What this verdict does not claim, so the next reader does not assume it:

- The park path for a blocking question and the raise notifications for a marker question are proven by Go tests only, not by a live run (the live check says so).
- The Android and iPhone work is not built. The parity record is filed as two comments on `dmz006/datawatch-app#283` on 2026-10-29 (the feature, then the marker counters and Monitor card); the apps have not acted on it.
- The branch is behind `main` on the version number (`9.35.0` at the branch point, `9.37.0` on `main`). The merge takes the version from `main`.

## Later round: rules-review fixes (2026-10-29)

A rules-review round found that the first pass left several rules unmet. What was done, with the result:

| # | Item | Result |
|---|------|--------|
| 1 | Mobile App Sync and Mobile-Parity: file the app issue | Comments filed on datawatch-app#283 (the feature, then an addendum for the stats read, Monitor card and locale keys); `docs/parity-status.md` has two new rows (worker marker toggle, decision answer link) pointing at them |
| 2 | Observability | Five counters, `GET /api/questions/stats`, MCP `question_stats`, the Monitor tab *Worker Questions* card (six locale keys in five bundles) and the `datawatch_worker_questions_total` metric, each with tests |
| 3 | Release smoke | Section 88 added (marker switch, stats read, REST source, answer log). Run alone on a fresh sandbox daemon: 6 PASS, 0 FAIL, 0 SKIP. The whole script could not be used here; the reason is in the testing guide |
| 4 | PWA e2e story | `scripts/test-stories/TS-838.sh` wraps the story; `scripts/run-tests.sh --stories=TS-838` passed against the sandbox daemon (1 passed, 0 failed, no leftover test daemons) |
| 5 | Test daemon cleanup and tmux | The sandbox runs used `scripts/test-daemon-lib.sh` (recorded PID, `/proc/<pid>/cmdline` marker check, traps) and a private `TMUX_TMPDIR`; the live check was repeated with the tmux server inspected: one session on the private server, none after delete, and no server after the daemon stopped |
| 6 | Docs | README, `docs/README.md` index, the plans README row, the proposal status, the CHANGELOG monitoring and reuse-audit paragraphs for this step, the live-check `## Parity surface` section, OpenAPI (both copies), API reference, MCP mapping, `docs/mcp.md`, operations, the how-to, the tracker and the testing guide |
| 7 | Coverage | `questionPass` 100%, `resumeForQuestion` 91%, `RaiseWorkerQuestion` 100%, `workerLineHash` 100% (was 0, 71, 81 and 40) |
| 8 | Detection Pattern Governance | Recorded as a low-severity divergence: the marker is a protocol constant like `DATAWATCH_COMPLETE`, not an editable pattern (CHANGELOG reuse audit and the proposal) |

Verification: `make build` clean, `make lint` 0 issues (includes the test-daemon cleanup and unscoped-tmux audits), `go test ./...` passed after one documentation count (the smoke section count in the testing guide) was brought to 89, `node --check internal/server/web/app.js` ok, `node --test internal/server/web/*.test.js` 1517 of 1517.

### Parity surface in this round

Unchanged from the table above, plus: REST gains `GET /api/questions/stats`; MCP gains `question_stats`; the PWA gains the Monitor *Worker Questions* card; the Android and iPhone rows point at the two comments on datawatch-app#283.
