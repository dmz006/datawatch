---
docs:
  index: true
  topics: [autonomous, automaton, branch reaper, branch-reap, branch-reap-run, branch-reap-restore, task sub-branch, contained branch, landed branch, backup ref, restore reaped branch, reaped_branch_backup_days, retention]
exec_steps:
  - tool: autonomous_branch_reap
    description: Read the branch reaper's report — what a pass would reap, what is held and why, and every backup ref (deletes nothing)
    args: {}
    read_only: true
  - tool: autonomous_branch_reap_run
    description: Perform one reap pass — each listed branch is backed up first, then deleted
    args: {}
    read_only: false
  - tool: autonomous_branch_reap_restore
    description: Recreate one reaped task sub-branch from its newest backup ref
    args:
      branch: "{{params.branch}}"
    read_only: false
  - tool: config_set
    description: Set the whole days a reaped branch's backup ref is kept (0 or unset = 30)
    args:
      key: autonomous.reaped_branch_backup_days
      value: "{{params.days}}"
    read_only: false
---

# Reap contained task branches safely

Every task an Automaton runs in parallel mode leaves its own branch in the
repository. Once the Automaton's work has landed (or its work branch was
dropped), those task branches are leftovers. The **branch reaper** removes
them — but only when nothing on them can be lost, and only after writing a
backup you can restore from.

This page is the short operator guide. The full reference, including how the
reaper fits beside the worktree cleanup pass and the open review items, is
[Automaton worktree cleanup → The task sub-branch reaper](automaton-worktree-cleanup.md#the-task-sub-branch-reaper-backup-first-then-remove).
The read-only check for `automaton/<id>` work branches and worktrees is the
safe-to-delete report in the same guide; this reaper does not replace it.

## What is reaped

- **Reaped:** task sub-branches (the per-task `worktree_branch`) of Automata
  whose work has settled:
  - **landed** — the `automaton/<id>` work branch exists and every commit of
    it is already on the base branch by content (a cherry-picked copy counts);
  - **dropped** — the work branch is gone, so only the base branch can hold
    its work.
- **Never reaped:** the `automaton/<id>` work branch itself and its worktree
  (that is the worktree cleanup pass), and any branch of an Automaton that is
  still **pending**.

## The safety rules

1. **Contained only.** A task branch is removed only when every commit on it
   is in the Automaton's work branch or in the base branch. One uncontained
   commit keeps the branch, with a plain reason in the report. If git cannot
   compare, the branch is kept — never a guess.
2. **Never pending.** An Automaton whose work branch still carries commits not
   on the base is skipped whole. An unknown landing state counts as pending.
3. **Never during a merge.** If a merge is in progress in any of the Automata's
   working directories, the pass stops at once: no backup is written and
   nothing is deleted.
4. **Backup before delete.** See below — no backup, no delete.
5. **One pass at a time.** A second pass while one is running is refused with
   a conflict, not skipped silently.

## The backup ref and restore

Before a branch is deleted the reaper writes
`refs/datawatch/reap-backup/<branch>/<Unix seconds>` (characters that are not
ref-safe become dashes, so `automaton/task-1` is stored under
`automaton-task-1`). Only then is the branch deleted. If the backup cannot be
written, the branch stays. If the delete fails after the backup, the backup
still stands.

To bring a branch back, restore it by name: the branch is recreated at the
**newest** backup ref for that name. A name that was never reaped is refused
(404 over REST) — nothing is created. A value that is not a valid git branch
name (for example one that starts with `-`) is refused (400 over REST) before
git runs; the same rule applies to the remote reap restore. You can also list backups by hand with
`git for-each-ref refs/datawatch/reap-backup/`.

## Retention: `autonomous.reaped_branch_backup_days`

Whole days a backup ref is kept before it may be pruned. **Default 30** (unset
or 0 reads 30); a negative value is refused. Pruning touches only refs under
`refs/datawatch/reap-backup/` with a numeric timestamp older than the
retention, and closes the restore window for that branch. It is read live; no
restart is needed.

| Surface | How |
|---|---|
| YAML | `~/.datawatch/config.yaml` → `autonomous.reaped_branch_backup_days` |
| CLI | `datawatch config set autonomous.reaped_branch_backup_days 45` |
| REST | `PUT /api/config` or `PUT /api/autonomous/config` with `{"autonomous.reaped_branch_backup_days": 45}` |
| MCP | `config_set` with that key |
| Messaging channel | `configure autonomous.reaped_branch_backup_days=45` |
| Web app | Settings → General → "Reaped Automaton branch backup kept (days; 0 or unset = 30)" |

Note: nothing runs a pass or the backup expiry on a timer yet; passes are
operator-triggered and refs accumulate until pruned.

## The report, the pass and restore on every surface

The report lists `reaped` (what a pass would remove, each with the Automaton,
containment, commit count, backup ref and a plain reason), `waiting` (what is
held and why), `backups` (every backup ref), `backup_days` and `computed_at`.
The report itself never deletes anything.

| Surface | Report | Pass | Restore |
|---|---|---|---|
| REST | `GET /api/autonomous/branch-reap` | `POST /api/autonomous/branch-reap/run` (no body; 409 if one is running) | `POST /api/autonomous/branch-reap/restore` `{"branch": "..."}` |
| CLI | `datawatch autonomous branch-reap [--json]` | `datawatch autonomous branch-reap-run [--json]` | `datawatch autonomous branch-reap-restore <branch> [--json]` |
| MCP | `autonomous_branch_reap` (read-only) | `autonomous_branch_reap_run` | `autonomous_branch_reap_restore` (`branch` required) |
| Messaging channel | `autonomous branch-reap` | — (not offered) | `autonomous branch-reap-restore <branch>` |
| Web app | Automata → **Events** → **Branch reaper** card | — (no run button) | **Restore** button per branch on the card |

All REST routes answer 503 while `autonomous.worktree_base_repo` is unset; the
two `POST`s need the autonomous write capability from a federated peer.

**On a live daemon.** Found 2026-11-18 and fixed: before the fix, a running
daemon answered 503 — `the autonomous manager does not expose the branch
reaper` — on *all three* routes **even when** the base repo was configured,
because the daemon's API wrapper did not carry the reaper's methods. The CLI,
MCP, messaging-channel and web-app surfaces failed with it too, which is why
finished Automata kept piling up in the repository while cleanup lagged. The
daemon now wires the reaper in with the routes: with a base repo set, the
report, the pass and the restore work on every surface above; with it unset
they still answer 503. If you were seeing that 503 with a base repo configured,
you were hitting that gap — on a daemon with the fix, read the report first
(there is a backlog to look at; see below), then run the pass.

## Walkthrough

1. `datawatch autonomous branch-reap` — read what would go and what is held.
2. `datawatch autonomous branch-reap-run` — each listed branch is backed up,
   then deleted; the reply names each backup ref.
3. Changed your mind? `datawatch autonomous branch-reap-restore <branch>`.

## What the operator must still review

- **No timer.** Nothing runs a reap pass on a schedule, and the backup-ref
  expiry has no caller yet. Backup refs under `refs/datawatch/reap-backup/`
  accumulate until something prunes them; check their count with
  `git for-each-ref refs/datawatch/reap-backup/ | wc -l` now and then.
- **No pass from chat or the web app.** The messaging channel and the web app
  offer the report and restore only; run a pass from REST, CLI or MCP.
- **Keep does not protect a branch.** A **Keep** record from the unlanded work
  review does not stop the reaper; a branch whose commits are all contained is
  still reaped (and stays restorable until its backup expires).
- **Restore window.** Lowering `autonomous.reaped_branch_backup_days` shortens
  the window for every existing backup once pruning runs.
- **The backlog built while the daemon's wiring was broken (fixed 2026-11-18).**
  Until the fix, every read of the report on a live daemon failed with the 503
  above, so neither the report nor the pass could be run from any surface and
  finished Automata kept piling up. On a daemon that has the fix, the first
  useful move is to read the report and see what accumulated during that
  window, then run the pass once the list reads as expected — the reaper does
  not catch up on its own (there is no timer; see the bullet above).

See also: [definitions → Task-subbranch reap](../datawatch-definitions.md),
[Automaton worktree cleanup](automaton-worktree-cleanup.md).
