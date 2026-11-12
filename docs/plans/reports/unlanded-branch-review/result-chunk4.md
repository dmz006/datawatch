# Review of archived Automaton branches, chunk 4 (read-only, 2026-11-06)

"main-ahead" is the number of commits main gained since the branch's merge-base. A large number means the branch needs a rebase or port, not a plain merge. Several branches are stacked task branches of one Automaton: the later one contains the earlier one.

| id | what it was for | class | evidence | recommendation |
|---|---|---|---|---|
| 3b518952 | BL500 worktree/tree-serialization smoke PRD; adds `W1.txt` | JUNK | One commit, one line in `W1.txt`. Registry row 95 says the restart revived stale smoke PRDs 3b518952 and 69e8421b, both cancelled. | delete worktree and branch |
| e6443290 | Smoke "WT1"; adds `SMOKE-WT1.txt` | JUNK | One commit, one line, no code. | delete worktree and branch |
| 6ac20001 | Design analysis doc `docs/plans/2026-10-21-undeclared-edit-vs-file-drift.md` (124 lines) on undeclared edits vs the file-drift guardrail | SUPERSEDED | The feature it analyses (branch a8500001) shipped as PRD 99090001 in 9.5.54 (registry row 89, BL508). The analysis doc itself is not on main, but it only records the design. | delete (optionally keep the doc as a reference) |
| 4e190001 | Deflake round five: unawaited question-raise alert write, idle-loop Stop race, test cleanup order | USEFUL-UNLANDED (partial) | Idle-loop part is on main: `idleLoop(..., done)` and `idleTickHook` in `internal/autonomous/automation_idle.go`, with report `docs/plans/reports/archive/2026-11/2026-11-02-idle-loop-stop-deflake.md`. The question-raise part is not: main has no `WaitQuestionNotifies` or `questionNotifyGate`, and `notifyQuestionRaised` still starts a bare goroutine. Registry row 313 says the Automaton was "discarded (never merged)". | keep as reference, port the question part (see below) |
| 61c60001...ff170005 | Defect 61c60001 (registry row 363): archive grace period, keep cancelled Automata whose branch holds unmerged commits | USEFUL-UNLANDED | Not on main: no `ArchiveGrace` or `archive_grace` anywhere in `internal`. Row 363 is still draft. Branch is an ancestor of ff170008. | delete after the ff170008 branch is kept |
| 61c60001...ff170008 | Same Automaton, tip: adds the grace setting to the PWA in five locales and tests on REST, CLI, MCP and comm | USEFUL-UNLANDED | Tip of the 61c60001 chain, 6 commits, 24 files, +843. main-ahead 124. | keep and land |
| 6aca0001...9c9a0007 | Protected-paths hold record, operator question and task view | USEFUL-UNLANDED | Not on main: no `protected_paths*` or `ProtectedPaths` in `internal` or `cmd`. Registry row 260 lists `6aca0001` as the pending rules-file protection feature. Ancestor of 9c9a0010. | delete after the 9c9a0010 branch is kept |
| 6aca0001...9c9a0010 | Protected-paths feature, tip: base-commit resolver, rules-review judge reads the rules from the base commit, matcher, gate, answer paths, REST, MCP, CLI, comm, PWA, how-to, parity issue file | USEFUL-UNLANDED | 15 commits, 62 files, +10.9k lines. main-ahead 1709, so it needs a port. | keep and land (port) |
| 718a0001...22bc0003 | Measured headroom check for a verifier cap above 1: idle-only probe | USEFUL-UNLANDED | Ancestor of 22bc0007. Not on main: no `HeadroomProbe`, `MeasureHeadroom` or `verifier_headroom`. Registry row 364. | delete after the 22bc0007 branch is kept |
| 718a0001...22bc0007 | Headroom feature, tip: probe, store, two settings wired into the write gate and admission, on-demand measure on REST, MCP, CLI and comm, node-detail display | USEFUL-UNLANDED | 8 commits, 53 files, +4.5k. main-ahead 94. It carries the five findings of the second review of b20f0001. It has a stray `DATAWATCH_COMPLETE` file at the repo root. Row 367 says its final gate task 22bc0009 is wedged on a read-only task, which c4880001 fixes. | keep and land (drop the stray `DATAWATCH_COMPLETE` file first) |
| a12f0001...d4f40007 | Sessions tiles and grouping, five-locale strings | USEFUL-UNLANDED | Ancestor of d4f40009. Not on main: no `tag_prefix`, `SessionGroup`, `session_group.go`. | delete after the d4f40009 branch is kept |
| a12f0001...d4f40009 | Sessions tiles, grouping, saved views, tip: REST `group_by` and `tag_prefix`, MCP, CLI, comm, web tile mode, locales, how-to `docs/howto/sessions-tiles-and-grouping.md` | USEFUL-UNLANDED | 8 commits, 33 files, +4.6k. main-ahead 1044, and `app.js` is heavily touched. Row 366: Automaton 68b70001 is held until this one merges. | keep and land (port) |
| c4880001...8a8e0003 | Lint rule and plan-coverage finding for unverifiable read-only tasks, task templates with a report path | USEFUL-UNLANDED | Ancestor of 8a8e0008. Not on main: no `readonly_unverifiable` or `verifier_report_path`. Addresses the defect in registry row 367. | delete after the 8a8e0008 branch is kept |
| c4880001...8a8e0008 | Read-only task recovery, tip: verifier accepts a declared report file, review edit and retry accept a held read-only task, operator cancel with a reason counts for the rollup, operations doc | USEFUL-UNLANDED | 7 commits, 42 files, +1.9k. main-ahead 74, so it should apply easily. | keep and land |
| ec310001...c39a0009 | Observer pull protocol: pull config and status over REST and MCP | USEFUL-UNLANDED | Ancestor of c39a000b and c39a000c. Not on main: no `observer_pull.go`, `pull_collector.go`, `ObserverPull`. | delete after the c39a000c branch is kept |
| ec310001...c39a000b | Same, plus the PWA source marker, pull controls and locales | USEFUL-UNLANDED | Ancestor of c39a000c. | delete after the c39a000c branch is kept |
| ec310001...c39a000c | Observer pull, tip: pull collector, REST and MCP, CLI, comm, PWA, five locales, push-vs-pull docs `docs/howto/federated-observer.md` | USEFUL-UNLANDED | 9 commits, 46 files, +4.9k. main-ahead 1186. No registry row mentions "observer pull" (`grep` of the registry and CHANGELOG found only unrelated pull-model hits). | keep and land (port); confirm the registry row |
| ee660001...22ee000e | Decision records: config keys, store, REST routes, MCP tools, question integration | USEFUL-UNLANDED | Not on main: no `decision-records` route or `DecisionRecord` store in `internal`. Row 313(c) lists ee660001 as one of the Automata being reset over the week. This branch has the MCP tools but not the how-to. | keep and land (port) |
| ee660001...22ee0016 | Decision records with the how-to `docs/howto/automaton-decision-records.md` | USEFUL-UNLANDED | 16 commits, 59 files, +11.3k. It has the how-to but not `internal/mcp/decision_record_tools.go` or its tests, and it is not an ancestor of 22ee000e. Together the two make the full feature. main-ahead 1756. | keep both; the combined result is the unit to land |

## Counts

- JUNK: 2
- SUPERSEDED: 1
- DROPPED: 0
- USEFUL-UNLANDED: 16 branches, forming 8 features: 4e190001 (partial), 61c60001, 6aca0001, 718a0001, a12f0001, c4880001, ec310001 and ee660001. Of the 16, 7 are ancestors of a tip branch and can be deleted once the tip is kept (61c60001 ff170005, 6aca0001 9c9a0007, 718a0001 22bc0003, a12f0001 d4f40007, c4880001 8a8e0003, ec310001 c39a0009 and c39a000b).
- UNCERTAIN: 0

## USEFUL-UNLANDED detail

### 4e190001 (deflake round five, question-raise part only)

- Missing from main: `questionNotifyGate` and `WaitQuestionNotifies` in `internal/server/question_raise_dispatch.go`, about 130 lines. They wait for the in-flight question-raise fan-out goroutine, including the alert-store write, before shutdown or reading the alerts file.
- Also missing: `internal/server/question_notify_wait_test.go` (62 lines), wiring in `cmd/datawatch/main.go` (17 lines), and test cleanup ordering in `question_step2_*_test.go`.
- It would apply to current main as a small cherry-pick of the `internal/server` files, though main is 1333 commits ahead, so expect small conflicts.
- Skip the idle-loop changes: main already has them in another shape.
- Related roadmap item: deflake rounds (`docs/plans/reports/deflake-round-*.md`) and shutdown races.
- Reports `docs/plans/reports/deflake-round-five-findings.md` (222 lines) and `idle-stop-shared-cause-investigation.md` (169 lines) exist only on this branch. Keep them as a reference.

### 61c60001 chain (ff170008 is the tip)

- Files: `internal/autonomous/supervisor_loop_archive_branch.go` (61 lines) plus tests, `supervisor_loop_steps.go` (+62), `internal/supervisorloop/settings.go` (+41) with tests, and the actor change in `Manager.Archive`.
- It also has REST, CLI, MCP and comm tests for the grace setting and PWA setting strings in five locales.
- Roadmap: registry row 363, the defect where the loop archives a cancelled Automaton within seconds. Memory note: "Loop archives cancelled Automata".
- Applies almost cleanly (main-ahead 124).
- It is the fix for the defect that made these very worktrees disappear, so it has priority.

### 6aca0001 chain (9c9a0010 is the tip)

- About 10.9k lines in 62 files.
- Core Go: `base_commit.go`, `protected_paths*.go` (gate, hold, answer, status), `rules_review.go` (+132), `config.go` (+146).
- Surfaces: REST, MCP (`protected_paths_tools.go`), CLI (`cli_protected_paths.go`), comm, PWA.
- Docs: `docs/howto/automaton-protected-paths.md` (444 lines) and the parity issue file `docs/plans/parity-issues/2026-10-31-automaton-protected-paths-parity.md`.
- Roadmap: registry row 260, the rules-files protection from the 2026-10-30 queue answers.
- main-ahead 1709, and it touches `openapi.yaml` (+1150), so it needs a port. The tip's commit list is the guide for the order.

### 718a0001 chain (22bc0007 is the tip)

- `internal/compute/headroom.go`, `headroom_probe.go` and tests (about 1.2k lines), `cmd/datawatch/verifier_headroom_measure.go`, `headroom_wiring.go` and the cap gate.
- Roadmap: registry rows 364 and 367, measured headroom for a verifier cap above 1.
- main-ahead 94. The old Automaton b20f0001 conflicted with main's slot model; this one is already ported onto it.
- Remove the stray `DATAWATCH_COMPLETE` file at the repo root before landing.
- Its last gate task (22bc0009) never ran, so the unit has not passed its final gate.

### a12f0001 chain (d4f40009 is the tip)

- `internal/server/session_group.go` (314 lines) and tests (589), `app.js` (+823), `style.css` (+162), three JS test files (about 1.4k lines).
- Surfaces: MCP, CLI (`cli_session_group.go`), comm (`session_group_cmd.go`), locales, and the how-to `docs/howto/sessions-tiles-and-grouping.md`.
- Roadmap: registry row 366 and the tag-browsing tile design `2026-10-20-tag-browsing-tile-design.md`. Automaton 68b70001, which covers the Automata and Automation lists, depends on this one.
- main-ahead 1044, and `app.js` has probably moved, so it needs a port or a careful rebase.

### c4880001 chain (8a8e0008 is the tip)

- `cmd/datawatch/verifier_report_path.go`, `decompose_lint_rules.go`, `plan_coverage_findings.go`, `failure_escalation_review.go` (+88), `manager.go` (+76), `task_templates*.go`.
- Tests: `readonly_unverifiable_test.go` (383 lines), `cancel_reopen_test.go` (245), `held_task_edit_test.go` (206).
- Docs: operations doc (+99).
- Roadmap: registry row 367, the defect where a read-only task with no verification script or report path wedges an Automaton.
- main-ahead 74, so it applies easily. It is the recovery for the 718a0001 gate wedge, so land it before or with that one.

### ec310001 chain (c39a000c is the tip)

- `internal/observer/pull_collector.go` (327 lines), `internal/server/observer_pull.go` (290), `observer_peers.go` (+310), `config.go` (+65), `cli_observer.go` (+204), `federation_meta_peers.go`, MCP, comm, PWA.
- Docs: `docs/howto/federated-observer.md` (+86).
- Roadmap: federated observer pull mode. I found no registry row; confirm with the operator.
- main-ahead 1186, so it needs a port.

### ee660001 pair (22ee000e and 22ee0016 are parallel, not nested)

- The store is `internal/autonomous/decision_record*.go`, about 3.1k lines in 9 files, plus `questions_store.go` (+227), `config.go` (+391), audit events and CEF signatures, and federation tool caps (+216).
- 22ee000e adds MCP tools (`decision_record_tools.go`, 232 lines, and tests of 320).
- 22ee0016 adds the how-to (368 lines) and operations and parity doc edits.
- Roadmap: registry row 262 (decisions raise their own questions) and the decision-record design.
- main-ahead 1756. The biggest port in the set, and the two branches must be merged together.
