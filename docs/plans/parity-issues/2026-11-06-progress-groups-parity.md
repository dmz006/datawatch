Filed: https://github.com/dmz006/datawatch-app/issues/386

## What changed in datawatch

The Progress card on the PWA Automation sub-tab (*Progress and cost*) now answers three more questions: how the work splits across projects, Automations and Automata, what the estimate rests on, and what the card is for.

- **Group selector.** A row of four buttons, labelled *Group the totals*: *Overall*, *By project*, *By Automation*, *By Automaton*. *Overall* is the default and shows the existing feature, bug and other table. The other three replace that table with one row per project, Automation or Automaton, each row carrying the same figures as the overall table. The choice lives only in the card state and is not saved.
- **(none) group.** Records with no project (project axis) or no Automation (Automation axis) are shown together in one row named *(none)*, always sorted last, so the rows add up to the overall total.
- **Estimate basis.** Under the table the card says "Estimate based on N finished records and M completed tasks." It is shown only when the reply carries both sample counts, and the line is omitted otherwise.
- **Purpose line.** A line above the selector says what the card is for, its state (work is running, nothing is running, or numbers are out of date), why, and the next step. A help link opens the progress how-to.
- **What counts as done.** A cancelled or archived Automaton whose tasks all completed now counts as done in every total and in the estimate sample. Cancelled, archived or rejected Automata with unfinished or no tasks stay out.

## REST fields the mobile app needs

`GET /api/autonomous/progress` (autonomous read capability):

| Item | Value |
|---|---|
| Query `group` | `project`, `automation` or `automaton`; any other value is a 400 naming it. Adds `group` and `group_totals` (row name to totals) to the reply. |
| `grouped.by_project`, `grouped.by_automation`, `grouped.by_automaton` | Always present in the reply, one map per axis (row name to totals, `(none)` included). The PWA card reads these, so the app can switch axes without another request. |
| `completion.sample_records` | Finished Automata the estimate rests on, archived finished work included. |
| `completion.sample_tasks` | Completed tasks the estimate rests on. |
| Row figures | Automata, done, running, waiting, blocked, tasks, tasks done and left, worker hours, median and mean times. |

`GET /api/autonomous/progress/trend`: with `project`, the reply adds `project_points` (date, reason, prds, done, running, waiting, blocked, tasks, tasks_done, tasks_left, work_hours), oldest first; `(none)` for records with no project. The route does not read `group`. The progress route does not read `project`; it is honoured only on the trend route, so the app should take one project's row from `grouped.by_project`.

## Locale keys added

All five locales (EN, DE, ES, FR, JA) carry these keys; the PWA falls back to the English text when a key is missing.

| Key | EN |
|---|---|
| `progress_group_label` | Group the totals |
| `progress_group_overall` | Overall |
| `progress_group_by_project` | By project |
| `progress_group_by_automation` | By Automation |
| `progress_group_by_automaton` | By Automaton |
| `progress_group_none` | (none) |
| `progress_state_stale` | Numbers are out of date |
| `progress_state_running` | Work is running |
| `progress_state_idle` | Nothing is running |
| `progress_why_stale` | The last refresh failed or is older than five minutes. |
| `progress_why_counts` | %1$s Automata, %2$s tasks left. |
| `progress_next_stale` | Reload the card to refresh it. |
| `progress_next_pick` | Pick a grouping to see where the work sits. |
| `progress_purpose` | How far along the Automata are and roughly when the rest will be done. |
| `progress_estimate_basis` | Estimate based on %1$s finished records and %2$s completed tasks. |
| `progress_help_tip` | Open the progress howto |
| `progress_help_label` | Help |

Please send translations through the Compose Multiplatform pipeline, using the app's own word for "Automaton", so the PWA bundles can mirror them.

## Acceptance

- The app's progress view offers the four groupings, defaulting to overall, and shows a *(none)* row last on the project and Automation axes.
- The estimate basis line shows the two sample counts and is hidden when either is missing.
- The app reads `group`, `grouped.*` and `completion.sample_*` from the progress route and `project_points` from the trend route; no new status value is added.
- All labels are localized in EN, DE, ES, FR and JA.

## Filing record

Not yet filed: file as a datawatch-app issue with `gh issue create --repo dmz006/datawatch-app --body-file` and record the URL and date here.
