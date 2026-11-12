# Task failure escalation policy

**Date:** 2026-10-23
**Version:** next patch above main (the version is taken at merge, not on this branch)
**Status:** Engine, REST, config, MCP, CLI, comm channel and PWA are implemented on the working branch. Operator docs, reference docs and the testing tracker are in progress (Phase 7). The live run against a sandbox daemon is not done (Phase 8).
**Backlog:** the repeated task failure escalation entry in `docs/plans/README.md`, which links here.

---

## Problem

A task that keeps failing used to burn its retries on the same worker and then be marked failed. The retry count was `autonomous.auto_fix_retries` (default 1), so two attempts in all, and the second attempt saw only a one-line hint. Two things were missing:

- **A different worker for later attempts.** Local models are cheap, and a stronger worker with the earlier failure reports in its prompt often gets past the problem. Nothing tried one.
- **A stop that asks the operator.** A task that fails its attempts should be held with a report of every attempt, so the operator decides: retry it as is, retry it with a changed spec, worker or file scope, or cancel it. The old path marked it failed and moved on, with no decision recorded.

## Outcome

- Each task has a ladder of worker steps. After each failed attempt the next attempt runs on the step that the failed-attempt count selects. Step `i` covers its `attempts` consecutive failed attempts, and the last step covers the rest.
- After `max_failures` failed attempts (default 3, settable to 4 or more, minimum 1) the task is held in `review_needed`. It keeps its attempts, gets a plain-text report, raises one error alert, and blocks its Automaton until the operator acts. Independent tasks keep running.
- The policy is set at three levels and resolved per field: task override, Automaton override, daemon default. `enabled: false` restores the old retry-then-fail behaviour.
- The operator acts with retry (optionally with a changed spec, worker or file list), or cancel. A retry from review resets the failed-attempt count.

## Design contract

These names are shared by every part of the feature.

- Package `internal/autonomous`: `FailureEscalationPolicy{Enabled *bool; MaxFailures int; Steps []FailureEscalationStep}` and `FailureEscalationStep{Backend, Model string; Effort Effort; Attempts int}`, with JSON keys `enabled`, `max_failures`, `steps`, `backend`, `model`, `effort`, `attempts` (all `omitempty`).
- Task fields: `Attempts []TaskAttempt`, `FailureEscalation *FailureEscalationPolicy`, `EscalationFailures int`, `ReviewSummary *ReviewSummary`. Automaton field: `FailureEscalation *FailureEscalationPolicy`.
- Task status `TaskReviewNeeded` (`review_needed`): not terminal and not a failure.
- Resolver `(*Manager).resolveFailureEscalation(prd, task) EffectiveFailureEscalation`, with `EscalationSources{Enabled, MaxFailures, Steps}` holding `task`, `prd` or `global`.
- Operator actions: `(*Manager).ResolveTaskReview(prdID, taskID, ReviewResolution{Action, Spec, Backend, Model, Effort, Files, Note}, actor)` and `(*Manager).SetFailureEscalation(prdID, taskID, policy, actor)`. Sentinel `ErrTaskNotInReview`.
- Config: YAML `autonomous.failure_escalation.{enabled,max_failures,steps}`; `PUT /api/config` keys of the same names, with `steps` taking a JSON array or `backend:model[@attempts],...`.
- REST: `set_failure_escalation` and `resolve_task_review` under `/api/autonomous/prds/{id}/`, both behind the autonomous-write capability and audited.
- MCP: `autonomous_prd_set_failure_escalation`, `autonomous_prd_resolve_task_review`. CLI: `datawatch autonomous failure-escalation show|set|clear|global` and `datawatch autonomous resolve-review`. Comm: `configure autonomous.failure_escalation.<key>=<value>`, `set-failure-escalation <automaton> [task]`, `review-task <automaton> <task> retry|cancel`.

## Scope

- `internal/autonomous/`: `failure_escalation.go` (types, resolver, validation, compact text), `failure_escalation_select.go` (step choice), `failure_escalation_attempts.go` (attempt records), `failure_escalation_prompt.go` (retry prompt), `failure_escalation_hold.go` (hold, alert, learning), `failure_escalation_review.go` (operator actions, overrides), `failure_escalation_views.go` (held-task views); `executor.go` (attempt budget, step selection, hold); `model_backend_compat.go` (step pairs checked at start); `manager.go` (global default, status).
- `internal/config/failure_escalation.go`: the YAML block and its validation.
- `internal/server/`: `autonomous_failure_escalation.go` (REST), `failure_escalation_config.go` (config keys), routing in `autonomous.go`.
- `internal/mcp/autonomous_failure_escalation.go`, `internal/federation/mcp_tool_caps.go`.
- `internal/router/autonomous_failure_escalation.go` and `sx2_parity.go` (comm verbs and status lines).
- `cmd/datawatch/cli_autonomous_failure_escalation.go` (CLI).
- `internal/server/web/app.js`, `style.css`, `locales/{en,de,es,fr,ja}.json` (PWA).
- Docs: `docs/howto/failure-escalation.md`, `docs/operations.md`, `docs/api/autonomous.md`, `docs/mcp.md`, `docs/cursor-mcp.md`, `docs/api-mcp-mapping.md`, `docs/parity-status.md`, `docs/datawatch-definitions.md`, `docs/testing-tracker.md`, `docs/config-reference.yaml`, `docs/implementation.md`, `docs/howto/README.md`, `docs/README.md`, `CHANGELOG.md`, `docs/plans/README.md`.

## Phases

| # | Phase | Status | Commits |
|---|---|---|---|
| 1 | Policy model, global config block and per-field override resolution | Done | `2e067e36` |
| 2 | Attempt records, the retry prompt from earlier attempts, and the step ladder that changes the worker | Done | `c18e5572`, `58036d36` |
| 3 | Hold for review at the failure count, the alert, the operator actions and the override setters | Done | `d59d29ac`, `ab5318c9` |
| 4 | REST routes, config round trip, Automaton and Automation views | Done | `6ec2fc24` |
| 5 | MCP tools, CLI subcommands, comm verbs, status output | Done | `50da359d` |
| 6 | PWA: Settings fields, Automaton and task override editors, held-task actions, counts, five locales | Done | `31c4cc1f` |
| 7 | Operator how-to, reference docs, testing tracker section, this plan, backlog link, changelog entry; verify script passes | In progress | this change |
| 8 | Live run against a sandbox daemon and the opt-in smoke test (added by a separate task). Every tracker row stays Validated: No until this passes. | Planned | — |
| 9 | Companion-app issues for Android and iPhone (capability parity) | Planned: filed at merge | — |

Phase 7 completion checklist (AGENT.md general documentation checklist):

- [x] `CHANGELOG.md` entry under `[Unreleased]`, no version heading.
- [x] `docs/config-reference.yaml` has the annotated `autonomous.failure_escalation` block with a steps example.
- [x] `docs/operations.md` covers behaviour, defaults, the changed default retry count, and how to restore the old behaviour.
- [x] `README.md`: no change. Its docs index does not list howtos (the howto index is `docs/README.md` and `docs/howto/README.md`).
- [x] Documentation index in `docs/README.md` and `docs/howto/README.md`.
- [x] `docs/testing-tracker.md` section with a row for each interface, Validated: No.
- [x] No tracker IDs in operator-facing text, howto or test names added by this phase (see Open items for the existing names that do not follow the rule).
- [x] `## Parity surface` section below.
- [x] Conditional: `docs/datawatch-definitions.md` glossary entries (new operator concept). `docs/flow/*.md` not changed: no new data flow beyond the existing executor path.
- [x] Conditional: `docs/howto/failure-escalation.md` (new user-followable workflow).
- [ ] Mobile-parity rule: the companion-app issues are not filed yet; they are filed at merge (Phase 9). `docs/parity-status.md` has the row with Android and iOS pending.
- [x] `go test ./...` green (full run, exit 0, on 2026-10-24) and `node --test internal/server/web/*.test.js` green (841 of 841).
- [x] Localization rule: the escalation keys are in all five bundles, checked by the locale guard and the node tests.
- [ ] Live/smoke test against a sandbox daemon (Phase 8, separate task).
- [x] Version: not bumped on this branch. The merge takes the next patch above main, in both `cmd/datawatch/main.go` and `internal/server/api.go`.

## Verification

Run on the working branch with `env -u TMUX -u TMUX_TMPDIR`:

- `go test ./internal/autonomous/ -run 'TestFailureEscalation|TestReviewNeeded'`: passes.
- `go test ./internal/config/ ./internal/mcp/ ./internal/router/ ./internal/server/ -run 'TestFailureEscalation|TestReviewNeeded'`: passes.
- `go test ./cmd/datawatch/ -run TestFailureEscalation`: passes.
- `node --test internal/server/web/app-failure-escalation.test.js`: 18 pass.
- `bash .datawatch/verify/escalation-docs-howto-tracker.sh`: the docs gate for this phase.

No daemon was started or stopped for this phase, and no test touches the production service.

## Parity surface

Every channel the feature touches, and what is excluded or delegated.

| Surface | Intent | Notes |
|---|---|---|
| REST | **Include.** `set_failure_escalation` and `resolve_task_review` routes; `PUT`/`GET /api/config` keys; `review_needed` in the status, Automaton and task views. | Both routes need the autonomous-write capability and write an audit entry. Errors: 400, 403, 404, 405, 409 (not held), 501. |
| MCP | **Include.** `autonomous_prd_set_failure_escalation`, `autonomous_prd_resolve_task_review`; `config_set` keys; `autonomous_status` `review_needed`. | Both tools are in the federation capability map. |
| CLI | **Include.** `datawatch autonomous failure-escalation show|set|clear|global` and `datawatch autonomous resolve-review`; status lists held tasks. | The steps flag takes the compact text, parsed before any request. |
| Comm channel | **Include.** `autonomous set-failure-escalation`, `autonomous review-task`, `configure autonomous.failure_escalation.*`; the status reply lists each held task, then one line naming the decide command. | |
| YAML / config | **Include.** `autonomous.failure_escalation` block with `enabled`, `max_failures`, `steps`; unknown keys inside the block are rejected. | Defaults: enabled, 3, no steps. |
| PWA | **Include.** Settings fields for the daemon default; Automaton and task override editors; the held-task badge, attempts, report and the five actions; held counts on headers, rows and the Automation strip; locales in en, de, es, fr and ja. | Browser run pending (Phase 8). |
| Android | **Include: capability parity required.** The companion app must show the held state, the report, the five actions and the override, using the REST routes above. Implementation is by the companion app. | Companion-app issue filed at merge (Phase 9). Not implemented in this repository. |
| iPhone / iOS | **Include: capability parity required.** Same capability set as Android. Implementation is by the companion app. | Companion-app issue filed at merge (Phase 9). Not implemented in this repository. |

Nothing in the feature is excluded from a surface.

## Open items

- **Test names.** Most test names start with `TestFailureEscalation`, as the feature rule requires. The status and view tests in `internal/autonomous/failure_escalation_status_test.go` and `failure_escalation_views_test.go` are named `TestReviewNeeded*` (seven tests). They pass. Renaming them is a follow-up, not part of this docs change.
- **Live run.** No sandbox daemon run has been done. Phase 8 owns it. Until it passes, every tracker row stays Validated: No.
- **Companion apps.** Android and iPhone capability parity is required. The issues are filed at merge.
- **Behaviour change.** The default attempt count rose from two to three for every Automaton without its own setting. The operator guide, the operations reference and the changelog say so, with the way back.
