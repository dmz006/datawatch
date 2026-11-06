# Review result, chunk 2 (19 branches)

Method: git log/diff vs merge-base, `git grep` and `git ls-tree` on main, docs/plans/README.md registry rows, CHANGELOG.md. Read-only. Daemon state of each Automaton was not queried.

Key structural finding: 14 of the 19 are per-task branches (`automaton/<prd>--tasks-...`) of a PRD whose parent branch `automaton/<prd>` still exists with a worktree under `~/.datawatch/prd-worktrees/`. `git merge-base --is-ancestor` shows each is already contained in its parent (except 42950001 which has no parent branch, and the two ee660001 branches, which are siblings off the parent's last commit). So the work is not on main, but the parent PRD branch is the thing to keep, and the task branches are redundant copies.

| id | what it was for | class | evidence | recommendation |
|---|---|---|---|---|
| 2713dbb9 | BL455: keep the PreTaskSHA retry baseline across auto-fix retries (fix plus about 1,000 lines of tests) | SUPERSEDED | Registry row 3: "done, shipped in 9.5.33 (PRD 2713dbb9 record cancelled; the fix shipped)". Main has `Task` retry-baseline field (internal/autonomous/models.go:1277 "Unlike PreTaskSHA (per-attempt...)") and `retry_pretask_capture_test.go`. Branch is 4,993 commits behind main. | delete worktree and branch |
| 42950001--...-f7900001 | Auto-fix attempt 1 for the burn-guard back-off display (9.71.2): zone fix in the rate-limit back-off path plus a JS test and a browser story | USEFUL-UNLANDED (small) | Parent feature shipped as 9.71.2 (row 302) and main's `budgetTime` already keeps the zone. But main's `cmd/datawatch/usage_backoff_wiring.go` has no `.In(time.Local)`, and main lacks `internal/server/web/app-capacity-backoff.test.js`, `scripts/test-stories/pwa/capacity-backoff-card.mjs` and the wiring test. Row 302's follow-up ("rate-limit back-off path still stores its reset time in the message's own zone") matches exactly what this commit fixes. | keep and land (cherry-pick the 14-line wiring fix and the three test files; drop the CHANGELOG, registry and gate-report edits) |
| 597a5120 | BL475-style harness-only commit guard: git shim (`internal/git/bin.go`, `internal/session/git_guard.go`), refuse commits outside a project and with an empty write scope | SUPERSEDED | Main shipped the same capability by a different design in 9.5.122 (row 56, PRD 72590001): per-session hooks, `internal/gitguard/{commitguard,projectdir,policy}.go`, and `stageChanges` stages only run-changed files when scope is empty (internal/session/git.go:213). Branch is 4,581 commits behind. | delete worktree and branch |
| 61c60001--...-ff170004 | Archive cancelled Automata only after a grace period (defect row 363) | USEFUL-UNLANDED | main has no `archive_grace`/`ArchiveGrace`. Subset (3 commits) of parent `automaton/61c60001` (14 commits). Only 124 commits behind main. | keep the parent and land it; delete this task branch after |
| 61c60001--...-ff170007 | Same PRD: grace setting tested on REST, CLI, MCP, comm; keep cancelled Automata whose branch has unmerged work | USEFUL-UNLANDED | Superset of ff170004 (5 commits), contained in parent `automaton/61c60001`. | keep the parent and land it; delete this task branch after |
| 6aca0001--...-9c9a0009 | BL620 protected-paths gate: config keys, base-commit resolver, rules-review judge reads base commit, hold record, gate, operator answers | USEFUL-UNLANDED | Registry rows 207 and 260 (draft, awaiting run); main has no `ProtectedPaths`, `protected_paths`, `base_commit`/`BaseCommit` code. Contained in parent `automaton/6aca0001` (23 commits). 1,709 commits behind main. | keep the parent; delete this task branch after |
| 6aca0001--...-9c9a000b | Same PRD plus REST surface (7,401 insertions, 35 files) | USEFUL-UNLANDED | Superset of 9c9a0009, contained in parent. | keep the parent; delete this task branch after |
| 718a0001 | Measured headroom check for a verifier cap above 1 (gate, store, MB conversion, idle probe, REST/MCP/CLI/comm, node detail, docs) | USEFUL-UNLANDED | Row 364 (re-filed port of b20f0001 onto main's verifier-slot model). Main has no `headroom_probe`/`HeadroomProbe`/`MeasureHeadroom`; it has only the older observer headroom gate (35070001). Parent `automaton/718a0001` has 16 commits through docs task 22bc0008; final gate 22bc0009 blocked by row 367. 94 commits behind main. | keep and land |
| 718a0001--...-22bc0004 | Early task branch of 718a0001 (settings, shared store, probe, gate) | USEFUL-UNLANDED (redundant) | Strict ancestor of `automaton/718a0001`. | delete worktree and branch once 718a0001 is kept |
| 85d00001 | BL431 Phase 3: Automation pause/resume/cancel/skip/insert/remove/reorder | DROPPED | Registry row 196: "dropped, not merged: main already has the whole stack". Main has `internal/autonomous/automation_control.go`, `automation_control_test.go`, REST/MCP/router/CLI tests; CHANGELOG v9.5.87. | delete worktree and branch |
| a12f0001--...-d4f40002 | Session grouping read (`group_by`, `tag_prefix`) on REST | USEFUL-UNLANDED | main has no `session_group.go`; `tag_prefix`/`group_by` on main only appear in cost report and locales. Row 366 shows a12f0001 (sessions tiles) running. Contained in parent `automaton/a12f0001` (14 commits). 1,044 behind. | keep the parent; delete this task branch after |
| b20f0001 | First attempt at the headroom check (verifier cap above 1) | DROPPED | Rows 361 and 364: not merged, built on a verifier-slot model main replaced with 35070001; "ported onto main's model with the old branch as a reference" as 718a0001. 28 commits, 53 files, 22 files conflict. | delete worktree and branch after 718a0001 lands (keep as reference until then) |
| c4880001--...-8a8e0005 | Verifier accepts a read-only task's declared report file; lint rule and plan-coverage finding for unverifiable read-only tasks (defect row 367) | USEFUL-UNLANDED | main has no `unverifiable`, no `cmd/datawatch/verifier_report_path.go`, no `readonly_unverifiable_test.go`. Contained in parent `automaton/c4880001` (11 commits). 74 commits behind, should apply cleanly. | keep the parent and land it; delete this task branch after |
| da660001 | Automatic lane fill (config, eligibility, auto-start, idle alert, config surfaces, docs) | SUPERSEDED | Rows 189 and 343: decided 2026-10-24 that BL514 builds lane fill in the Automation coordinator in shadow mode; row 343: "on main under rewritten commits ... the branch is stale and can be deleted". Main has `lane_fill.go`, `lane_fill_alert.go`, `lane_fill_eligibility.go`, `lane_fill_config.go`, and the CLI, MCP, router and REST files. | delete worktree and branch |
| ec310001--...-c39a000a | Observer pull: federation peer pull config, collector, fetcher, merged views, compute-node binding, REST/MCP/CLI/comm | USEFUL-UNLANDED | main has no `pull_collector`, `observer_pull`, `PullCollector`. No registry row names ec310001. Superset of the other two ec310001 branches; contained in parent `automaton/ec310001` (20 commits, counts-only gate done). 1,186 behind. | keep the parent; delete this task branch after |
| ec310001--...-c39a0007 | Same PRD up to compute-node binding | USEFUL-UNLANDED (redundant) | Ancestor of parent. | delete worktree and branch once parent is kept |
| ec310001--...-c39a0006 | Same PRD up to merged observer views | USEFUL-UNLANDED (redundant) | Ancestor of parent. | delete worktree and branch once parent is kept |
| ee660001--...-22ee0010 | Decision records: configuration keys, store, question builder, sync, answers, folder watch, REST, comm verbs | USEFUL-UNLANDED | Registry row 262 (draft PRD), row 313(c) (reset to run over the week). main has no `decision_record_store.go`, `decision_record_folder.go` or `decision_record_sync.go` (the `DecisionRecord` hits on main are the unrelated lane-fill decision log). Not an ancestor of the parent: sibling of 22ee000d off commit 274c0285e. 1,756 behind. | keep and land (via the parent `automaton/ee660001` plus this branch's extra commits) |
| ee660001--...-22ee000d | Same PRD, REST routes and OpenAPI entries variant | USEFUL-UNLANDED | Shares 12 commits with 22ee0010; differs by its own REST commit f158b64ca (openapi.yaml edit, 1,178 lines changed). | keep as reference; compare with 22ee0010 before deleting |

## Counts

SUPERSEDED 3, DROPPED 2, USEFUL-UNLANDED 14, JUNK 0, UNCERTAIN 0.

## USEFUL-UNLANDED details

Common caveat: the registry does not show whether these Automata are still live. Worktrees still exist for every one. Check the Automaton state (`autonomous_prd_get`) before deleting anything.

### 42950001--...-f7900001 (usage back-off zone fix)
- Files: `cmd/datawatch/usage_backoff_wiring.go` (+14, converts `resetAt` and rebuilt deadlines with `.In(time.Local)` in two places), `cmd/datawatch/usage_backoff_wiring_test.go` (+34), `internal/server/web/app-capacity-backoff.test.js` (+111), `scripts/test-stories/pwa/capacity-backoff-card.mjs` (+119).
- Applies to current main: yes for the wiring fix and tests (main's `budgetTime` already matches); drop the CHANGELOG/registry/gate-report edits, which conflict with 1,057 later commits.
- Roadmap: burn guard back-off display (9.71.2, row 302 follow-up).

### 61c60001 (archive grace) task branches ff170004 and ff170007
- Code: `internal/autonomous/supervisor_loop_steps.go`, `supervisor_loop_archive_branch.go`, `internal/supervisorloop/settings.go`, `internal/autonomous/manager.go` (actor on Archive), plus REST/CLI/MCP/comm tests. About 800 insertions in ff170007.
- Applies to main: very likely (124 commits behind). The parent `automaton/61c60001` has 14 commits and also covers the PWA setting, docs and the live-check script.
- Roadmap: defect row 363, the loop archiving cancelled Automata within seconds. Fixes a recurring operational problem.

### 6aca0001 (protected paths) task branches 9c9a0009 and 9c9a000b
- New files: `internal/autonomous/protected_paths_gate.go`, `protected_paths_answer.go`, `base_commit.go`, hold record, `rules_review` base-commit read, plus about 25 test files; REST surface and `internal/server/protected_paths_test.go` in 9c9a000b. 4,600 to 7,400 lines.
- Applies to main: no, 1,709 commits behind, 35 files with config, API and router overlap. Rebase of the parent `automaton/6aca0001` (23 commits, including MCP, CLI, PWA, docs, parity issue file, live-check) or a re-run.
- Roadmap: BL620, rules-file protection (rows 207, 260).

### 718a0001 and 22bc0004 (headroom measurement)
- New: `internal/compute/headroom.go`, `headroom_probe.go`, `internal/server/compute_headroom.go`, `cmd/datawatch/verifier_headroom_measure.go`, PWA node-detail code in `app.js` (+210), `tests`, docs. About 4,600 lines.
- Applies to main: yes, built on main's verifier-slot model, 94 commits behind. Final gate task 22bc0009 was blocked by the read-only-task defect (row 367), which `c4880001` fixes.
- Roadmap: measured headroom for a verifier cap above 1 (rows 361, 364).

### a12f0001 task d4f40002 (session grouping read)
- New: `internal/server/session_group.go` (314), `session_group_test.go` (589), small edits to `session_list_page.go` and `api.go`.
- Applies to main: probably with small conflicts (1,044 behind). Parent `automaton/a12f0001` has 14 commits (tiles, nesting, locales, docs).
- Roadmap: sessions tiles, grouping and saved views (row 366, and `2026-10-20-tag-browsing-tile-design.md`).

### c4880001 task 8a8e0005 (read-only task report file)
- New: `cmd/datawatch/verifier_report_path.go` (59) and test, `internal/autonomous/plan_coverage_findings.go` (+52), `decompose_lint` rule and config, `task_templates.go` (+70), `readonly_unverifiable_test.go` (383). About 900 lines.
- Applies to main: yes (74 behind). Parent `automaton/c4880001` has 11 commits (adds cancel-with-reason, review/edit/retry paths, docs).
- Roadmap: defect row 367. This unblocks 718a0001's final gate.

### ec310001 task branches c39a000a, c39a0007, c39a0006 (observer pull)
- New: `internal/observer/pull_collector.go` (327), `internal/server/observer_pull.go`, `observer_peers.go` changes, config, multiserver store, CLI and comm surfaces. About 4,300 lines in c39a000a.
- Applies to main: needs a rebase (1,186 behind, touches observer peers, config, compute node binding). Parent `automaton/ec310001` (20 commits) also has PWA, docs and the counts-only gate.
- Roadmap: observer pull versus push for federation peers; no registry row, so confirm the operator wants it before landing.

### ee660001 task branches 22ee0010 and 22ee000d (decision records)
- New: `internal/autonomous/decision_record.go`, `decision_record_store.go`, `decision_record_sync.go`, `decision_record_folder.go`, answer handling, REST routes, `internal/server/decision_records_test.go`, openapi.yaml, comm verbs (22ee0010). 11,000+ lines.
- Applies to main: no. 1,756 commits behind and openapi.yaml was rewritten. Use as the base for a rebase of the parent `automaton/ee660001` plus the sibling task branches (MCP, CLI, PWA, flow tests, how-to), or re-run.
- Roadmap: "decisions raise their own questions" (row 262, draft PRD).
