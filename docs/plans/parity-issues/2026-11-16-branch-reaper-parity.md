# feat(i18n): add the branch reaper card, setting and keys for parity (datawatch v9.180.0)

Parent release: datawatch v9.180.0

## What changed

The daemon now reaps task sub-branches that are contained in their Automaton branch or landed. Each reaped branch is backed up to a ref under `refs/datawatch/reap-backup/` first and only then deleted, and the backup is kept for a number of whole days. The web app gains a Branch reaper card in the maintenance area: a preview report of what a pass would reap and what it holds, the backup refs, and a Restore button that recreates one reaped branch from its newest backup ref. The CLI gains `branch-reap`, `branch-reap-run` and `branch-reap-restore`, and the chat gains the same.

## Settings to add to the mobile app

- `autonomous.reaped_branch_backup_days` — whole days a reaped branch's backup ref is kept; 0 or unset means 30.

## REST contract

- `GET /api/autonomous/branch-reap` — the report: branches that would be reaped, branches held and why, backup refs and the retention.
- `POST /api/autonomous/branch-reap/run` — perform one reap pass (back up, then delete).
- `POST /api/autonomous/branch-reap/restore` — recreate one reaped branch from its newest backup ref; the body names the branch.

## New locale keys with English values

| Key | English |
|---|---|
| `settings_reaped_branch_backup_days` | Reaped Automaton branch backup kept (days; 0 or unset = 30) |
| `branch_reap_card_title` | Branch reaper |
| `branch_reap_preview_note` | Preview only — the report itself deletes nothing; a reap pass backs up and then deletes the listed branches. |
| `branch_reap_computed_at` | Report computed at %1$s |
| `branch_reap_retention_days` | Backup ref retention: %1$d whole days |
| `branch_reap_retention_default` | Backup ref retention: default (unset) |
| `branch_reap_reaped_heading` | Would be reaped (%1$d) |
| `branch_reap_reaped_empty` | No branch would be reaped |
| `branch_reap_waiting_heading` | Held — would be kept (%1$d) |
| `branch_reap_waiting_empty` | No branch is held |
| `branch_reap_empty` | Nothing would be reaped and nothing is held. |
| `branch_reap_automaton` | Automaton %1$s |
| `branch_reap_contained` | Contained in %1$s |
| `branch_reap_commits` | %1$d commit(s) |
| `branch_reap_backup` | Backup ref %1$s |
| `branch_reap_backups_heading` | Backup refs |
| `branch_reap_backups_none` | No backup refs exist |
| `branch_reap_restore_btn` | Restore |
| `branch_reap_restoring` | Restoring… |
| `branch_reap_restore_ok` | Branch %1$s restored from its newest backup ref. |
| `branch_reap_restore_err` | Could not restore %1$s: %2$s |
| `branch_reap_failed` | Could not read the branch reap report: %1$s |

## Screens

- Automata maintenance: a Branch reaper card with the preview report (would be reaped, held), the backup refs list and a Restore button per reaped branch.
- Settings: the backup-days field.
