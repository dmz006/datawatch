# BL620: protect the rules files from workers and from the daemon

**Date:** 2026-10-29
**Version:** none (planning document; plan-document commits are exempt from the version bump)
**Tracker:** BL620, registry row 207 (external report GH#207 follow-up; earlier draft in `2026-10-26-response-to-issues-207-210.md`)

Confirmed: yes — option B first (the judge reads the rules from the base commit, plus a protected-paths gate) with the proposal's default answers, every setting configurable. Operator, 2026-10-30 13:42 EDT, in the question queue (PWA), question q-18e348cb04df2657cbce, option confirm; words: "These questions should be configurable,  other datawatch users may want their own settings, my selection is default".

## Problem

Nothing stops a worker, or the daemon's own injection, from changing the rules files (`AGENT.md`, the supervisor rules, `CODEOWNERS`, the workflows), and the review that should catch it reads the rules from the same tree the worker changed. Every commit carries the same identity, so history cannot say who changed a rules file. The row lists five parts: judge reads rules from the base commit, a protected-paths gate, distinct git identities, branch protection with code-owner review, and (also in the row) judge timeout holds the merge plus a memory destructive-operation guard and a demoted state.

## What exists today (verified)

| Part | State | Proof |
|---|---|---|
| Rules-review judge | EXISTS, opt-in (runs only when `rules-review` is in the resolved PRD guardrail list). It tells the judge to read the rules file at the root of its working directory and compare against its own git diff, so the rules come from the worker's tree | `internal/autonomous/rules_review.go` (`rulesReviewSuffix`, `presentRulesContextFiles` tests `fileExists` in the project dir) |
| Judge reads rules from the base commit | MISSING | same file; no `git show <base>:` use |
| Judge timeout holds the merge | EXISTS. A judge that times out on every backend gives category `judge-timeout`, the PRD goes to `needs_review`, lane fill will not run it, only the operator moves it on | `internal/autonomous/judge_hold.go`; `judge_policy` in `internal/config/config.go` |
| Supervisor rule that bypassed the judge removed | NOT VERIFIED in this pass (the supervisor rules file was not re-read for the old lesson) | row 207 text |
| Per-Automaton read and write directory scope | EXISTS. Default write scope is the project dir, so `AGENT.md` is inside it | `internal/autonomous/scope.go` (`EffectiveWriteDirs`) |
| Scope lint at plan time (off, warn, enforce) | EXISTS, checks declared task files against scope, not path names | `scope.go`, `scope_lint` in config |
| Foreign file drift and undeclared-edit checks (off, warn, block) | EXISTS but SKIPPED for worktree-mode PRDs (branch set), empty task dir or empty pre-task SHA. They compare against the task's declared files, with no notion of a protected path | `internal/autonomous/file_drift.go` (`checkForeignFileDrift`, `checkUndeclaredEdits`) |
| Protected-paths gate (hold a task commit touching named paths) | MISSING. No protected-path list in config or code | search of `internal/autonomous`, `config.go` |
| Workers cannot commit; the harness commits after validation | EXISTS for session commits: a commit guard hooks directory refuses commits and ref updates without a per-session token | `internal/gitguard/commitguard.go`, `internal/session/commit_guard.go`. Whether the autonomous worktree path uses it for task commits was not traced (unverified) |
| Distinct git identities | MISSING. Daemon commits use the fixed identity `datawatch`; the operator, supervisor and workers otherwise share one. The identity is set in the session git helper, the tracker, the verifier diff helper and attempt snapshots | `internal/session/git.go`, `tracker.go`, `cmd/datawatch/verifier_diff.go`, `internal/autonomous/attempt_snapshot.go` |
| Daemon injection into `AGENT.md` | EXISTS. The session tracker writes the cross-session rule block under a marker when messaging is enabled | `internal/session/tracker.go` |
| `CODEOWNERS` | EXISTS at the repo root: default owner plus `AGENT.md`, `SECURITY.md`, `LICENSE`, the security review and `.github/workflows/` | `CODEOWNERS` |
| `CODEOWNERS` covering the supervisor rules file and itself | MISSING. Neither the supervisor rules doc nor `CODEOWNERS` is listed | `CODEOWNERS` |
| Branch protection with code-owner review | MISSING per the 2026-10-26 draft (main unprotected, direct pushes). This is a GitHub setting and cannot be confirmed from the repo (unverified here) | draft doc |
| Merge helper checks protected paths | MISSING. It checks version consistency and internal refs only | `scripts/merge-branches-bump-version.sh` |
| Memory destructive-operation guard and a demoted state | MISSING per the 2026-10-26 draft; not re-checked in code in this pass | draft doc |

## Options

**A. Judge reads rules from the base commit only.** Builds: change the judge prompt and its project-dir check to read the rules through git from the base branch instead of the worktree file. Cost: 1 PRD, about 3 tasks (resolve base ref, read via `git show`, tests with an edited-rules fixture). Risk: low. It does not stop the edit, only stops the edit from changing the rubric.

**B. A plus a protected-paths gate.** Builds A, plus a configurable list of protected paths (defaults: `AGENT.md`, the supervisor rules, `CODEOWNERS`, the workflows) checked on every task commit including worktree mode; a hit holds the task for the operator unless that Automaton is authorised for the path. Cost: 1 to 2 PRDs, about 8 tasks (config, check in the commit path, hold state and event, authorisation field, PWA/MCP/REST/CLI, tests, howto). Risk: medium; false holds when a task legitimately edits docs, and it overlaps the existing drift checks.

**C. B plus identities and branch protection.** Adds distinct git identities for operator, supervisor and workers (per-role author name and email on harness commits), and branch protection with code-owner review once the loop raises pull requests, plus adding the supervisor rules file and `CODEOWNERS` to `CODEOWNERS`. Cost: 2 to 3 PRDs, about 12 tasks; branch protection is operator-side GitHub configuration. Risk: branch protection blocks the current direct-push release flow, so it needs a replacement flow.

**D. B plus stop the daemon writing `AGENT.md`.** Moves the cross-session block out of the rules file or behind an operator-approved path. Cost: 1 PRD, about 3 tasks. Risk: changes how cross-session messaging is advertised to workers.

## Recommendation

Option B first, then C's identities and CODEOWNERS additions as a second slice, keeping branch protection as a separate operator decision tied to a pull-request flow. A removes the self-grading problem cheaply and B closes the write path; identities make later history answerable. Branch protection should wait until the loop can raise pull requests.

## Open questions for the operator

1. Which option: A, B, C or D (or A now and the rest later)?
2. Is the protected list exactly `AGENT.md`, the supervisor rules, `CODEOWNERS` and the workflows, or add more (security review, changelog), yes to the four or name the additions?
3. When a task touches a protected path, hold for the operator (proposed) or only warn, hold or warn?
4. May the operator authorise a path per Automaton so a rules-editing Automaton can run, yes or no?
5. Should the daemon stop writing the cross-session block into `AGENT.md` (option D), yes or no?

## Dependencies and order

Order: A, then the protected-paths gate, then identities, then branch protection. Branch protection depends on a pull-request flow for the loop, which does not exist today. The judge-timeout hold already exists, so that part of the row needs only the supervisor-rule cleanup (unverified). The memory guard and demoted state are separate work and should be split into their own row. The gate should reuse the drift checks' skipped-for-worktree gap rather than add a third check.

## Parity surface

| Surface | Change a build would make |
|---|---|
| Config | protected-path list; per-Automaton authorised paths; per-role git identity fields |
| REST | read and write the list and the per-Automaton authorisation; hold state visible on the task |
| MCP | matching tools and a set-authorisation verb |
| CLI | matching commands |
| Comm channel | a held-for-protected-path notification and an approve verb |
| PWA | settings for the list; held-task banner and approve control |
| Mobile | parity issue on the datawatch-app repo for the banner and approve control |
