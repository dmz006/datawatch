# BL556 Parallel tasks inside one Automaton: design proposal

**Date:** 2026-10-29

**Version:** none (planning document; plan-document commits are exempt from the version bump)

**Tracker:** BL556 (registry row 137)

Confirmed: no — no option is chosen. This file records the problem and the options; nothing is decided and no implementation PRD is filed until a `Confirmed: yes` line replaces this one.

## Problem

Task graphs inside one Automaton (PRD) allow parallel work, but tasks run one at a time. The registry row measured 46 PRDs since 2026-10-19: on average 5.5 tasks with a critical path of 3.2, so a perfect scheduler would finish up to 1.7x faster (5.5 / 3.2). The row also names the blocker: the project-tree write lock serializes writers on one worktree. This proposal separates what is already built from what is missing, and offers ways to close the gap.

## What exists today (verified)

EXISTS TODAY:

| Piece | Proof |
|---|---|
| A task fan-out pool with a semaphore (BL370). `Run` resolves `effectiveTaskConcurrency(prd override, global)`; a result above 1 takes the pool path, 1 or less takes the sequential path | `internal/autonomous/executor.go` (`Run`, `effectiveTaskConcurrency`) |
| Two knobs: daemon-wide `autonomous.max_concurrent_tasks` and a per-Automaton override. Both are settable on REST, MCP (`autonomous_config_set`), the comm channel `configure`, and per-Automaton through `SetPRDConcurrency` | `internal/config/config.go`, `internal/server/api.go`, `internal/server/autonomous.go`, `internal/mcp/autonomous.go` |
| The default is 1, so the pool is never used unless an operator raises it | `internal/config/max_concurrent_tasks_test.go`, `internal/autonomous/manager.go` (default config) |
| The per-tree advisory writer lock (BL428 Phase 2). Every task takes it before spawn in `executeOne` through `holdTree()` and holds it across spawn, verify and retries | `internal/autonomous/tree_lock.go` (`acquireTreeWritersLock`), `internal/autonomous/executor.go` (two `holdTree()` call sites) |
| The lock key is per worktree for a PRD with a git branch, and per shared base for a direct-mode PRD. All tasks of one worktree PRD therefore share one key | `internal/autonomous/tree_lock.go` (`ResolveTreeKey`) |
| A per-task project-directory override: a task with its own dir resolves to a different key and does not wait on its siblings | `ResolveTaskProjectDir` used by `ResolveTreeKey` |
| One git worktree per PRD, created once under the PRD worktree base, branch off the base branch | `internal/autonomous/worktree.go` (`EnsureWorktree`) |
| Scope fields: PRD-level `WriteDirs` and `ReadDirs`, and per-task `FilesPlanned` | `internal/autonomous/models.go` |
| An optional push-and-open-PR step at PRD completion (`Git.AutoPR`) | `internal/autonomous/models.go` (`GitWorkflow`), `internal/mcp/bl221_types.go` |

MISSING:

| Gap | Evidence |
|---|---|
| No notion of a read-only task. The lock is taken for every task, including research or review tasks that never write | Both `holdTree()` sites are unconditional; no `read_only` field on `Task` |
| No per-task worktree or branch. One PRD has exactly one worktree | `EnsureWorktree` is keyed by PRD id |
| No task-to-task merge step. I found no daemon code that merges a branch; the PRD branch is merged by the supervisor's helper outside the executor, or by the PR that `AutoPR` opens | grep of `internal/autonomous` for merge logic finds only PR and CI code |
| No path-scoped lock. The lock is whole-tree, so disjoint `files` lists do not help | `tree_lock.go` key is a tree, not a path |

Consequence worth knowing before any raise of the default: with the pool on, a second writer task takes a pool slot and then waits on the tree lock (bounded by `capacityWait`). It shows as a waiting task with a "tree lock" reason, so raising the knob alone gives no speedup and can hold slots idle. I did not run this live.

The design record (`docs/plans/2026-10-14-concurrency-and-budget-design.md`, section 3a) also lists why same-tree parallel writers are unsafe: both contend on the git index lock, and each task's `pre_task_sha..HEAD` diff range then includes the other's commits.

## Options

**A. Read-only task class.** Add a task flag (declared at decompose or by `edit_task`) for tasks that only read. Such tasks skip `holdTree()` and run in the existing pool; writers stay serial. Cost: 1 PRD, about 5 tasks (field, executor skip, decompose prompt, parity surfaces, tests). Risk: low for safety, but the gain is unmeasured because nobody has counted what share of tasks are read-only; a task that is wrongly flagged can edit files under a running writer. Needs a guard that fails a flagged task when the tree changes.

**B. Per-task worktrees plus a merge step.** Each task gets its own worktree and branch off the PRD branch; on task completion the executor merges it into the PRD branch in dependency order, under a short merge lock. Verifier diffs become cleaner (each task has its own base). Cost: 2 to 3 PRDs, about 12 to 18 tasks (worktree lifecycle and cleanup, merge and conflict handling, failure and retry semantics, verifier diff base, restart recovery, tests). Risk: highest build effort; a merge conflict between tasks needs a defined outcome (block for the operator or send to triage); build caches and the go:embed placeholder are per worktree; disk use grows with fan-out.

**C. Write-dir partitioning with a path-scoped lock.** Tasks whose planned files or dirs do not overlap run together in the one worktree; the lock becomes per path set. Cost: 1 to 2 PRDs, about 8 tasks. Risk: the design record already finds same-tree parallel commits unsafe (index lock, diff range, `git add -A` sweeping a sibling's files), and `FilesPlanned` is a plan, not a guarantee. Not recommended.

**D. Leave it serial.** Keep the knob, document that it only helps cross-tree work. Cost: none. The 1.7x ceiling stays unclaimed.

## Recommendation

Option A first. It is the smallest change that uses the pool already built, it carries no merge risk, and it produces the missing measurement (how many tasks are read-only, and what the real wall-clock gain is). Decide on B only after that figure exists, because B costs three to four times as much for a gain bounded at 1.7x.

## Open questions for the operator

1. Which option? A / B / C / D.
2. If A: may decompose set the read-only flag itself, or must the operator or supervisor confirm each flagged task? (decompose / confirm)
3. Should the daemon-wide default of `max_concurrent_tasks` stay at 1 until the build ships? (yes / no)
4. If B: on a merge conflict between two tasks, block the Automaton for the operator, or send it to the triage path? (block / triage)
5. Should a measurement of read-only share across existing PRDs run before any build is filed? (yes / no)

## Dependencies and order

Depends on BL428 Phase 2 (shipped) and BL370 (shipped). Option B also touches the lane-fill and capacity accounting (BL428 Phase 3, shipped) because more tasks hold sessions at once, and it would raise load on the shared inference pool (BL405). Order: A, measure, then B only if the measured gain justifies it.

## Parity surface

| Surface | A | B |
|---|---|---|
| Config | none new (reuses `max_concurrent_tasks`) | new keys for per-task worktree on/off and cleanup |
| REST | task field in create/edit task | worktree and merge status on the task |
| MCP | `autonomous_prd_edit_task`, `add_task` param | status fields only |
| CLI | task flag | status output |
| Comm channel | task edit command | status text |
| PWA | flag on the task card | merge state on the task card |
| Mobile | datawatch-app parity issue required (PWA changes) | datawatch-app parity issue required |
