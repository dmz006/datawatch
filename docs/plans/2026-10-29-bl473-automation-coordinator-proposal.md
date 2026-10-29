# BL473 Automation coordinator built to the supervisor rules: design proposal

**Date:** 2026-10-29
**Version:** none (planning document; plan-document commits are exempt from the version bump)
**Tracker:** BL473

Confirmed: no — no option is chosen. This file records the problem and the options; nothing is decided and no implementation PRD is filed until a `Confirmed: yes` line replaces this one.

## Problem

Registry row 54 asks the Automation coordinator to do what the supervisor does by hand: validate plans, triage failures, check verifiers, admit work to a tree, run the after-done checklist, keep registry and README consistent, alert on silent state, keep lanes occupied, and push and tag each completed minor version while watching its CI run. The 10.0.0 arc draft (`2026-10-28-ten-oh-arc-automation-draft.md`) showed the gap: the Automation starts the next member but the supervisor still merges, ships, fills lanes and orders work. BL609 (`2026-10-25-automation-supervisor-duties-proposal.md`) already owns eight duties (steps 1 to 8) and the daemon-side watch, lane-fill eligibility and merge mechanics (steps 9 to 11). This proposal covers what BL473 adds beyond BL609 and how the two fit.

## What exists today (verified)

| Row 54 duty | State | Proof |
|---|---|---|
| Plan-validation gate | EXISTS in part | `CheckPlanCoverage`, `CheckPlanStructure` (`plan_coverage*.go`) run at decompose and gate Approve in "block" mode (`plan_coverage_gate.go`); `LintPlanScope` (`scope.go`); optional LLM `plan_review_mode` (`plan_review.go`, default off). MISSING: rules-doc section 4 items with no check I found: tests present per feature, no tracker IDs in names, release tasks without fixed version numbers, pin to open model. |
| Failure triage | EXISTS (BL609 step 1, v9.27.0) | `triage_pass.go`, `triage_record.go`, `Coordinator.triageFresh`; escalation ladder in `failure_escalation*.go`. MISSING: classification that picks retry, evidence-only or revision per rules section 5 (the record gathers facts, it does not choose the move); standalone failed Automata outside an Automation. |
| Verifier self-check | MISSING | No code runs a task's verification script before the worker starts; nothing checks the exec bit (only the decompose prompt text in `decompose.go` says "executable"). `supervisor-watch.py` does this by hand. |
| Tree admission | EXISTS | `refuse_dirty_tree_start`, `project_write_serialization` (config), `CheckProjectWorktree` (`worktree_integrity.go`). |
| After-done checklist | MISSING | No code tests version bump in both files, CHANGELOG entry, docs, parity issue or registry row at completion; `CheckpointSaysAllDone` (`checkpoint_done.go`) reads text only. |
| Registry versus README consistency | MISSING | No Go code or script parses `docs/plans/README.md`; only `check-merge-version.sh` exists for versions. |
| Silent-state alerting | EXISTS in part | `automation_idle.go` `checkIdle` (idle Automation alert), triage alert minutes, `lane_fill_alert.go`. MISSING: merge pending (only `progress_backlog.go` reads the suffix; nothing writes it), red CI on main and tags. |
| Lane occupancy | EXISTS in shadow | `PlanLaneFill` with `start_prd`, `decompose`, `decompose_ahead`, `report_merge`, `idle`, `blocked` actions (`lane_fill.go`); coordinator modes off, shadow (default), on (`lane_fill_coordinator.go`); hold reasons in `lane_fill_eligibility.go`. MISSING: eligibility rules of BL609 step 10, wait-on-verifier occupancy (step 3). |
| Push, tag and watch CI | EXISTS in part | `git_completion.go` pushes a branch; `ci_watch.go` watches the completed commit if the ci-watch guardrail is set. MISSING: any version tag push (no `git tag` in `internal/autonomous`), a main and tag CI watch (step 6), merge mechanics (step 11, `scripts/merge-branches-bump-version.sh` is the manual form). |

## Options

**A. Finish BL609 only; close BL473 as covered.** Steps 2 to 11 already cover triage, lane fill, merge pending, CI watch and merge mechanics. Cost: no new PRD, 10 existing steps (about 40 to 60 tasks in total, filed one at a time). Risk: the three MISSING rows above (plan-rule checks, verifier self-check, after-done checklist, registry consistency) stay manual because no BL609 step owns them.

**B. BL609 plus one "pre-flight and post-flight checks" PRD.** Adds the rows no BL609 step owns: a verifier pre-run (script exists, executable, fails before work and passes on a reference where possible), the rules-section-4 plan rules as extra findings in `CheckPlanStructure`, an after-done checklist record per Automaton, and a registry-versus-README consistency check, all reporting through the BL609 triage record. Cost: 1 PRD, about 12 to 16 tasks, then BL609 as in A. Risk: the checklist duplicates `check-merge-version.sh` unless it calls it; false positives on text rules.

**C. B plus an autonomous ship step.** Adds the push and tag of each completed minor version and its CI watch to BL609 step 11, under policy from BL483. Cost: 1 further PRD, about 8 to 12 tasks. Risk: the daemon pushes and tags by itself; the operator holds that decision today (the memory rule is to ask before every push, and step 11 lists it as open).

## Recommendation

Option B. It closes every row that has no owner while keeping all push and tag authority with the operator, and it reuses the triage record, so no new alert path is needed. Option C can follow when the operator answers the push question.

## Open questions for the operator

1. A, B or C?
2. Should the verifier pre-run (B) block a task start when the script is missing or not executable (yes), or only alert (no)?
3. Should the registry-versus-README check be report-only (yes) or able to rewrite the README row (no)?
4. May the daemon ever push and tag a completed minor version without asking (yes or no)? Required only for C.
5. Is the rules-section-4 plan list (tests per feature, no tracker IDs in names, no fixed version in release tasks, model pin) the full set of plan checks, or are items to be added or dropped?

## Dependencies and order

BL483 first for the policy keys the checks read (which checks are on, block versus warn). Then B's PRD in parallel with BL609 steps 2 to 8. B's alerts depend on BL609 step 1 (shipped). C depends on BL609 steps 9 to 11 and BL483. BL644 (pending-item member) is independent but decides whether BL473 itself can be an Automation member.

## Parity surface

| Surface | Touched by B |
|---|---|
| Config | yes, check-mode keys (from BL483) |
| REST | yes, read of the pre-flight and after-done records |
| MCP | yes, same read |
| CLI | yes, same read |
| Comm channel | yes, alerts through the triage record |
| PWA | yes, a findings card on the Automation view |
| Mobile | yes, one parity issue for the new REST record |
