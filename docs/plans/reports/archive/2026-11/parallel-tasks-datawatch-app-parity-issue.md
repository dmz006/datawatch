# datawatch-app parity issue: parallel tasks inside one Automaton (v9.57.0)

Ready for the merge step to file against `dmz006/datawatch-app`. Not filed from this branch.

**Title:** Parity for datawatch v9.57.0 — read-only task toggle, task worktree/merge badges, parallel settings, parallel execution card

## What changed in the PWA

Tasks of one Automaton can now run at the same time. A task flagged read only skips the
project write lock; other parallel tasks each get a worktree and branch, merged back in
dependency order under a conflict policy. The PWA gained the following, and the Android
client needs the same on each surface it has.

1. **Read-only toggle on a task.** A "Read-only" checkbox in the task editor
   (`prd_task_read_only_label`, `prd_task_read_only_hint`) that sets the task's `read_only`
   field through the existing task edit call, and a `read-only` badge on the task row
   (`prd_task_read_only_badge`, `prd_task_read_only_badge_tip`).
2. **Worktree and merge-state badge on each task.** Shows the task merge state
   `pending`, `merged`, `conflict` or `held` with a tooltip, and the merge commit when
   there is one (`task_merge_state_pending|merged|conflict|held`, `task_merge_badge_tip`,
   `task_merge_badge_tip_commit`). The state comes from the task object in
   `GET /api/autonomous/prds/{id}` (see `docs/api/openapi.yaml`).
3. **Four settings under `autonomous.parallel`**, in Settings > Automata, group
   "Parallel task worktrees" (`settings_parallel_task_group`):
   - `read_only_tasks` (bool, default true) — `settings_parallel_read_only_tasks`
   - `worktree_per_task` (bool, default true) — `settings_parallel_worktree_per_task`
   - `merge_conflict_policy` (`additive_then_model` | `block` | `triage`, default
     `additive_then_model`) — `settings_parallel_merge_conflict_policy`, option labels
     `parallel_policy_additive_then_model|block|triage`
   - `merge_conflict_model` (string, empty = strongest planning model) —
     `settings_parallel_merge_conflict_model`

   Each setting has a help link to `docs/howto/automaton-parallel-tasks.md`, and the
   definitions are in `docs/datawatch-definitions.md`.
4. **Parallel execution card (speedup)** on a completed Automaton: total tasks, read-only
   tasks, and speedup, or "not measured" when none was recorded
   (`parallel_metrics_card_title`, `parallel_metrics_help_link`, `parallel_metrics_total_tasks`,
   `parallel_metrics_read_only_tasks`, `parallel_metrics_speedup`,
   `parallel_metrics_speedup_none`).

## New locale keys

All added to `internal/server/web/locales/{en,de,es,fr,ja}.json`: the `prd_task_read_only_*`,
`task_merge_*`, `settings_parallel_*`, `parallel_policy_*` and `parallel_metrics_*` keys
listed above, plus `request_watch_more`. The Android client should add the same keys to
its five locales.

## Other surfaces already done (for reference, no client work)

REST, MCP (`autonomous_config_set` arguments `parallel_*`, task read-only on task edit),
CLI, chat `configure`, and YAML.

## Acceptance

- A task can be flagged and unflagged read only and the badge follows.
- Each task shows its merge state and tooltip; a conflict is visibly distinct.
- The four settings read and write through `PUT /api/config` and show their defaults.
- The parallel execution card shows on a completed Automaton and handles "not measured".
- Strings are translated in all five locales, with no hard-coded text.
