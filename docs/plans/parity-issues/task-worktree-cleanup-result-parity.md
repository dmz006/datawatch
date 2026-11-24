# [sync] Cancel and archive results: kept task worktrees and branches

Draft only. No issue has been filed on any tracker.

## Affected area

The Automaton cancel and archive actions (single and multi-select) in the datawatch-app mobile clients (Android and iOS).

## What the daemon does

Closing an Automaton never removes a task worktree that has uncommitted or untracked files, and never deletes a task branch with commits that are not on the default branch. The Automaton record now carries `task_worktree_cleanup`, and the replies of `DELETE /api/autonomous/prds/{id}`, `POST …/{id}/cancel` and `POST …/{id}/archive` include it when the Automaton had task worktrees or branches:

- counts: `worktrees_removed`, `branches_deleted`, `worktrees_kept`, `branches_kept`
- `reasons`: a count per reason (`cleanup_off`, `report_mode`, `not_removed_by_archive`, `no_base_repo`, `live_session`, `uncommitted_work`, `detached_commits`, `unverified`, `remove_failed`, `unmerged_branch`, `no_default_branch`, `backup_failed`, `branch_delete_failed`)
- `message`, and `kept` (one entry per task; not in the compact view)

No new setting and no new endpoint.

## What the PWA does

After a cancel or an archive, when `worktrees_kept + branches_kept` is above zero, it shows a warning notice with both counts and each reason with its count. Locale keys `automata_task_worktrees_kept` and `task_keep_reason_<reason>`, present in en, de, es, fr and ja.

## Expected behavior in the apps

1. After a cancel or archive, show the same notice when something was kept; show nothing when nothing was kept or the field is absent.
2. Use the same strings in all five locales. Show an unknown reason as its code.
