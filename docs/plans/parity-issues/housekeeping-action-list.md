# Automata Housekeeping action list: Unlanded work card, Commit button, permission fix and the Events move

## Body of the issue

The PWA Housekeeping sub-tab (Automata tab → Automation view) changed from a set of read-only reports into an action list. The app should add the same card, buttons and flows, and drop the reports that moved to the Events sub-tab. Design record: `docs/plans/2026-11-06-automata-pages-redesign.md` (the Housekeeping finding and decision 6). Operator-facing help: `docs/howto/automaton-worktree-cleanup.md` and `docs/howto/automation-tab.md`.

Why it exists: the operator found many worktrees with commits not on the default branch, and cards that showed text with no action, no request and no explanation. Each card now says what it is for, what state it is in, why, and what the operator can do next.

### Unlanded work card

A card listing archived Automata whose branch has commits that never reached the default branch. It is hidden until the list has a row and refreshes every 30 seconds. Running Automata are never listed. The comparison is by commit content, so a merge that rewrote commits still counts as landed.

`GET /api/autonomous/unlanded-work` (read-only; a federated peer needs the autonomous read capability; 503 when Autonomous is disabled; 405 for any method but GET). Response: `{"branches": [...]}`, an empty array when nothing is unlanded. Each entry:

| Field | Meaning |
|---|---|
| `automaton_id` | The Automaton the branch belonged to |
| `branch` | The branch name; the key every action takes |
| `title` | The Automaton title, shown as the row heading |
| `commits` | How many commits are not on the default branch |
| `last_activity` | Time of the newest activity |
| `recommendation` | `landed`, `superseded` or `needs_decision` |
| `reason` | One plain line explaining the recommendation |

Order is least settled first (`needs_decision`, `superseded`, `landed`), then newest activity first. Show the title, the commit count, the last activity, the recommendation and the reason on each row.

| Recommendation | Meaning |
|---|---|
| `landed` | Every commit is already on the default branch by content: the branch is a leftover |
| `superseded` | Some commits are not on the default branch, but later work changed every file they touch |
| `needs_decision` | Nothing shows the work was replaced: the operator decides |

### Row actions

All three are `POST` with a JSON body naming the branch; they need the autonomous write capability. An unknown or no-longer-listed branch is 404; a missing base repository is 400; any other refusal is 409 with a plain message to show unchanged.

| Button | Route | Body | Effect |
|---|---|---|---|
| Keep | `POST /api/autonomous/unlanded-work/keep` | `branch`, optional `note` | Records that the branch is left alone. Not destructive. Keeping twice updates the note |
| Archive branch | `POST /api/autonomous/unlanded-work/archive-branch` | `branch` | Moves the branch under `refs/archive/<branch>`; the commits stay reachable. Reversible |
| Delete worktree | `POST /api/autonomous/unlanded-work/delete-worktree` | `branch`, `confirm: true` | Writes and verifies a git bundle backup first, then removes the worktree and the branch. The only destructive action. Without `confirm: true` it is refused |

Delete must ask the operator to confirm before sending `confirm: true`. If the backup cannot be written, nothing is removed and the error is shown.

### Commit button on the uncommitted work card

Each held worktree on the existing Uncommitted work card has a Commit button that opens a dialog with the diff and a message box.

- `GET /api/autonomous/uncommitted-work/diff?worktree=<path>` returns `{"worktree", "diff", "clean"}`. `clean` is true when there is nothing to commit; show "Nothing to commit" and disable the submit button.
- `POST /api/autonomous/uncommitted-work/commit` with `{"worktree", "message"}` returns `{"worktree", "commit"}` (the new commit id). An empty message, a worktree outside the worktree base, or nothing to commit is 400; other failures are 409. Show the message text on failure.

### Permission fix

A build or test can leave read-only files that stop cleanup from deleting a worktree. The cleanup status now carries one `permission_issue` record (`count`, `paths`, `why`, `fixable`, `hidden`) instead of raw errors, and `skipped_by_reason` counts what cleanup left in place by reason. Show the one-line `why`; show a **Fix permissions** button only when `fixable` is true; show nothing when `hidden` is true.

`POST /api/autonomous/cleanup/fix-permissions` with `{"paths": [...]}` (the `paths` from the record) returns `{"changed": N}`. It restores owner write permission on paths inside the worktree base only, never follows links, and removes nothing. Show "Fixed permissions on N folder(s)" on success.

### Moved to the Events sub-tab

The safe-to-delete report and the cleanup history are no longer Housekeeping cards. They are Events entries and keep a help line on the Events sub-tab. The Cleanup card, which holds the permission fix, lives there. Housekeeping keeps the Unlanded work card, the Uncommitted work card (with Commit) and protected paths. The events themselves are described in `docs/plans/parity-issues/85430001.md`.

### Other surfaces added (no app work needed, listed for completeness)

- CLI: `datawatch autonomous unlanded-work [--json]` with `keep <branch> [--note ...]`, `archive-branch <branch>` and `delete-worktree <branch> --confirm`.
- Comm channel: `autonomous unlanded-work` with `keep|archive-branch|delete-worktree <branch>` (delete needs the word `confirm`).
- MCP: `autonomous_unlanded_work`, read-only; the three actions are not MCP tools.
- The Commit and the permission fix are REST and PWA only. There is no CLI, comm or MCP verb for them.
- `scripts/branch-audit.py` prints the same branches as `UNLANDED` and exits 1 while any exist.

### New locale keys with English values

| Key | English |
|---|---|
| `unlanded_work_card_title` | Unlanded work |
| `unlanded_work_failed` | Could not read the unlanded work list: %1$s |
| `unlanded_work_purpose` | Purpose: %1$s |
| `unlanded_work_commits` | %1$d commit(s) not on the default branch |
| `unlanded_work_last_activity` | Last activity: %1$s |
| `unlanded_work_recommendation` | Recommendation: %1$s |
| `unlanded_work_rec_decide` | needs a decision |
| `unlanded_work_rec_superseded` | superseded |
| `unlanded_work_rec_landed` | already landed |
| `unlanded_work_keep` | Keep |
| `unlanded_work_archive` | Archive branch |
| `unlanded_work_delete` | Delete worktree |
| `unlanded_work_delete_confirm` | Delete the worktree and branch %1$s? The work is backed up first. |
| `unlanded_work_action_failed` | Action failed: %1$s |
| `unlanded_work_help_link` | About the unlanded work card |
| `uncommitted_work_commit` | Commit |
| `uncommitted_work_commit_title` | Commit uncommitted work |
| `uncommitted_work_commit_message` | Commit message |
| `uncommitted_work_commit_diff_loading` | Loading the changes… |
| `uncommitted_work_commit_clean` | Nothing to commit. |
| `uncommitted_work_commit_submit` | Commit changes |
| `uncommitted_work_commit_cancel` | Cancel |
| `uncommitted_work_commit_failed` | Commit failed: %1$s |
| `uncommitted_work_commit_help_link` | About committing uncommitted work |
| `cleanup_permission_fix` | Fix |
| `cleanup_permission_fixed` | Fixed permissions on %1$d folder(s). |
| `cleanup_permission_fix_failed` | Could not fix permissions: %1$s |
| `cleanup_permission_help_link` | About the permission fix |
| `cleanup_skip_reason_not_finished` | The Automaton is still running |
| `cleanup_skip_reason_live_session` | A live session is using the worktree |
| `cleanup_skip_reason_live_lease` | A live lease holds the worktree |
| `cleanup_skip_reason_retained` | Kept until the retention time ends |
| `cleanup_skip_reason_primary` | Main checkout, never removed |
| `cleanup_skip_reason_dirty` | Has uncommitted changes |
| `cleanup_skip_reason_commits_not_on_base` | Has commits not on the base branch |
| `cleanup_skip_reason_no_base_branch` | The base branch could not be found |
| `cleanup_skip_reason_held` | Held by the safe-to-delete check |
| `cleanup_skip_reason_over_limit` | Per-pass removal limit reached |
| `cleanup_skip_reason_permission_denied` | Permission denied |

Settings to add to the mobile app: none.

Open question for the operator: whether a kept branch should also suppress the safe-to-delete report. Until it is answered, the app should not assume either behaviour.

How-to: `docs/howto/automaton-worktree-cleanup.md`.

## Filing record

Not yet filed. File as a datawatch-app issue; record the URL and date here when filed.
