# Parity draft: reports and moved-aside files are never lost (datawatch-app)

Draft only. No issue has been filed.

## What changed in the daemon

- `POST /api/autonomous/unlanded-work/delete-worktree` copies the worktree's
  reports, moved-aside files and other untracked files in its work folders
  (including docs/plans/reports/) before it removes the worktree. The reply gains two optional fields:
  `files_backup_path` (string) and `backed_up_files` (list of paths). They are
  absent when there was nothing to copy. A failed copy answers 409 and removes
  nothing.
- A permanent Automaton delete keeps a folder that was moved aside from its
  worktree path. The reply is unchanged.
- New audit log actions: `autonomous_read_only_report_settled`,
  `autonomous_merge_files_moved_aside`, `autonomous_gate_report_kept_previous`,
  `autonomous_delete_kept_moved_aside`,
  `autonomous_unlanded_delete_files_backed_up`.

## For the mobile client

- If the app shows the reply of a worktree delete, show `files_backup_path`
  when present.
- If the app lists audit actions by name, add the five above.
- No new setting, screen or request field.
