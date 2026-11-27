# datawatch-app parity: shadow of a hand merge and the turn-on report (draft, not filed)

## What the daemon has now
- `GET /api/autonomous/merge/shadow` carries `turn_on` when a merge repository is
  configured: `project`, `merging_on`, `target`, `met`, `streak` (`length`,
  `total`, `bugs`, `features`, `with_conflict`, `asked`, `stopped_by`),
  `merges[]` (`automaton_id`, `name`, `hand_commit`, `at`, `verdict`, `rows[]`
  with `number`, `name`, `result`, `detail`), `eligible[]`, `excluded[]`,
  `next_push[]`, `actions[]`, `still_to_apply[]`, `planted_faults[]`,
  `turn_off`, `problems[]` and `lines[]` (the whole report as text).
- `POST /api/autonomous/prds/{id}/shipped` takes an optional `hand_checks` map
  (`pass`, `fail`, `skip`) with source `merge_step`.
- A merge pipeline definition has an optional `shadow` block (`private_files`,
  `check_files`, `release_text`).

- `streak.length` is 0 when `main` holds anything after the newest shadowed
  merge, and the run ends at any gap between two shadowed merges;
  `stopped_by` says which. `lines[]` also states what the count covers and the
  memory limit a shadow's commands run under.

## What the app should show
- On the merge shadow screen: the matched run against the target, whether merging
  is on, and the report text (`lines`). Show it once `merges` is not empty, also
  when there is no earlier comparison.
- Strings in the app's locales: "Matched hand merges in a row: N of T",
  "Merging by datawatch is off/on", "Turn-on report".
- No write is needed from the app.
