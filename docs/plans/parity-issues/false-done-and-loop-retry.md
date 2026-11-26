# docs(mobile): undelivered Automaton workers, interactive sessions and the supervisor loop's one retry

Draft for the datawatch-app repository. It is filed at merge time, not by the task that wrote it.

## What changed in datawatch

No new setting, endpoint, action or user-facing string. Three behaviours changed:

- An Automaton worker whose task datawatch types is retried as a launch failure, then held,
  unless delivery is proven for its current start. It is never shown as done without it.
- An interactive session whose agent reports its work done reads `waiting_input`. It no
  longer switches between `complete` and `running`.
- The supervisor loop's retry of a task held after infrastructure retries now starts the
  run, and happens once for each task until the operator retries or resets it.

Two read-only fields were added to API records:

| Record | Field | Meaning |
|---|---|---|
| Automaton task | `loop_retried_infra_hold` (bool, omitted when false) | The supervisor loop has used its one retry of this task's hold |
| Session | `launch_log_start` (integer, omitted when 0) | Output log size at the last restart |

## What to check in the mobile app

- Decoding of task and session records ignores or accepts the two new fields.
- No screen needs a change.
