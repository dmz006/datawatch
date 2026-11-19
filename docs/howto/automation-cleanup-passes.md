---
docs:
  index: true
  topics: [autonomous, automation, automaton, cleanup, branch reaper, worktree cleanup, remote reap, temp sweep, report only, backed up, skip reasons, release action, remote_reap_enabled, remote_reap_retention_days, unreadable]
exec_steps:
  - tool: autonomous_branch_reap
    description: Read the branch reaper report (a report; it deletes nothing)
    args: {}
    read_only: true
  - tool: autonomous_cleanup_status
    description: Read what the worktree cleanup and temp sweep did last, with the skipped entries and their release actions
    args: {}
    read_only: true
  - tool: autonomous_remote_reap
    description: Read the remote reap report on origin (report only; it deletes nothing)
    args: {}
    read_only: true
---

# Automaton cleanup passes: branch reaper, worktree cleanup, remote reap and temp sweep

Finished Automata leave things behind: task branches, work branches,
worktrees, remote branches and scratch files. Four passes clean them up. This
guide says what each one does, how to read it before it acts, and the one
action that frees whatever it held back.

## Two guarantees

- **Nothing with unmerged work is ever deleted.** Every pass proves the work is
  already on the project's default branch, or leaves the entry alone and names
  the reason.
- **Every deleted branch is backed up first.** A branch is deleted only after a
  backup ref has been written (and, for the remote reap, verified to point at
  the branch tip). If the backup fails, the branch is kept. A backed up branch
  can be restored from its newest backup ref.

## Branch reaper

The **branch reaper** removes the task branches that parallel-mode tasks leave
in the repository, once the parent Automaton has settled.

- **Report versus run.** The report (`datawatch autonomous branch-reap`, MCP
  `autonomous_branch_reap`, `GET /api/autonomous/branch-reap`, comm
  `autonomous branch-reap`) lists what a pass would reap, what is held and
  every backup ref; it deletes nothing. The run (`datawatch autonomous
  branch-reap-run`, MCP `autonomous_branch_reap_run`, `POST
  /api/autonomous/branch-reap/run`) backs each listed branch up, then deletes
  it. There is no comm verb for the run. Restore one branch with
  `datawatch autonomous branch-reap-restore <branch>`.
- **The live-daemon fix.** Found 2026-11-18 and fixed: a running daemon
  answered 503 on all three routes even with the base repository set, so
  finished Automata piled up. The daemon now serves them; a 503 now means only
  that `autonomous.worktree_base_repo` is unset.
- **Surfaces.** REST, CLI, MCP, comm and the Branch reaper card on the
  Automata tab's Events sub-tab (report and Restore; no run button).

Details: [automaton-worktree-cleanup.md](automaton-worktree-cleanup.md).

## Worktree cleanup

The **worktree cleanup** pass removes a finished Automaton's work branch and
worktree. It runs in `autonomous.automation.cleanup_mode` `report` (the
default), `on` or `off`. Each entry it leaves is a skip, with a reason and,
where there is one, the single action that frees it.

| Skip reason | Meaning | Your one action |
|---|---|---|
| `dirty` | The worktree has uncommitted work the cleanup cannot name | Commit the work or discard the listed files, then let the next pass run |
| `held` | The safe-to-delete report holds the branch | Follow the release action on the entry (below) |
| `commits_not_on_base` | Some commits are not on the default branch | Merge the branch to the default branch |
| `no_base_branch` | The default branch could not be established | Set the base branch for the Automaton or the project, then re-run |
| `fresh` | Younger than the retention window | Nothing; wait |
| `live_session`, `live_lease` | A session or lease still uses it | Nothing; wait |
| `permission_denied` | The file system refused the removal | Use Fix permissions on the Cleanup card |
| `limit` / `over_limit` | The per-pass removal cap was reached | Nothing; the next pass continues |

**Generated and launcher-only uncommitted work does not hold the pass.**
Untracked files that are only the daemon's own harness or launcher files (for
example `.mcp.json`, the backend config files and the injected skills folder)
are removed with the worktree and listed on the removal as `resolved_files`.
A changed tracked file or an unrecognised untracked file still holds it.

**The release action.** A hold placed by the safe-to-delete report carries one
release action matching the report's state: if the work is already on the
default branch, re-run the worktree cleanup pass; if it has not landed, merge
it to the default branch and then re-run; if the state could not be read,
re-run so the merge check can resolve it. The status lists these as
`last_worktree_skips`.

## Remote reap

The **remote reap** removes the remote branch of an Automaton whose work is
fully merged. The local passes never touch the remote.

- **Report only by default.** `autonomous.remote_reap_enabled` is false or
  unset by default, which means **report only**: a pass evaluates everything
  and the report says `report_only`, but deletes nothing on origin.
- **Eligibility.** A branch is reaped only when it is merged into the
  project's default branch and its tip is older than
  `autonomous.remote_reap_retention_days` whole days (unset or 0 reads 30).
  Held branches are named with their reason (not merged, too new, no remote
  ref, or backup could not be verified).
- **Run action.** `datawatch autonomous remote-reap-run`, MCP
  `autonomous_remote_reap_run`, `POST /api/autonomous/remote-reap/run`. It runs
  once when you ask; no timer deletes remote branches. With the setting on,
  each branch is backed up to `refs/datawatch/remote-reap-backup/<branch>/<time>`,
  the backup is verified, then the branch is deleted on origin.
- **Read and restore.** `datawatch autonomous remote-reap` (also MCP
  `autonomous_remote_reap`, comm `autonomous remote-reap`) is the report;
  `remote-reap-restore <branch>` recreates a branch from its newest backup.

Turn deletion on with
`datawatch config set autonomous.remote_reap_enabled true` once the report
reads as expected.

## Temp sweep

The **temp sweep** removes the daemon's scratch files for ended sessions and
stale build and scan cache folders in the temporary directory. A path the file
system refuses to read or remove is **skipped, not failed**: it is counted
under skipped with reason `unreadable`, left in place, and removed on a later
pass once readable. A pass with zero failures can still be holding such a path,
so if the temp disk keeps growing, look for an `unreadable` skip and fix its
permissions or mount.

## Reading all of it

`datawatch autonomous cleanup` (MCP `autonomous_cleanup_status`, comm
`autonomous cleanup`, `GET /api/autonomous/cleanup`) shows each pass's last
result, the skips by reason and the release actions.

See also [automaton-worktree-cleanup.md](automaton-worktree-cleanup.md) and
[../operations.md](../operations.md).
