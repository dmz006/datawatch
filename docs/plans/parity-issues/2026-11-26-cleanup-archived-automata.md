# Mobile parity: cleanup of archived Automata

Draft for the datawatch-app client. Not filed.

## What changed in the daemon
- New setting `autonomous.automation.cleanup_archived` (boolean, default true, unset reads true). With it on, the cleanup treats an archived Automaton as finished, under the same keep-work rules as a completed or cancelled one. `cleanup_mode` still decides whether anything is removed.
- Two new values in `task_worktree_cleanup.reasons` and its `kept` entries: `needed_by_record` (an Automaton that has not finished still points at the worktree or branch) and `over_limit` (the periodic pass reached its removal limit).
- The cleanup skip list has a new reason, `needed_by_record`.
- Archiving releases nothing more: only the periodic cleanup releases archived work. Removing a branch writes no landing record.
- The settings help text says so and names what a folder removal can delete.
- An Automaton's own removed worktree is created again at its next plan or run; the record gets a `worktree_recreated` decision.

## Client work
| Area | Change |
|---|---|
| Settings, Automata | A switch for `autonomous.automation.cleanup_archived`, default on. Label: "Clean up archived Automata like other finished ones (default on)" |
| Cleanup status and kept notices | Show text for `needed_by_record` and `over_limit`; unknown reasons must still be shown as they are |
| Localisation | en, de, es, fr, ja (same strings as the web bundles: `settings_automation_cleanup_archived`, `settings_automation_cleanup_archived_help`, `task_keep_reason_needed_by_record`, `task_keep_reason_over_limit`, `cleanup_skip_reason_needed_by_record`) |

## API
`GET /api/config` shows the value in effect; `PUT /api/config` accepts `autonomous.automation.cleanup_archived` (true or false, anything else is refused with 400).
