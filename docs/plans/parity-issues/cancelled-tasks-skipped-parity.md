Parent release: datawatch v9.162.1

## What changed in datawatch

A cancelled task is now a skipped, final outcome in a running Automaton. When every task is completed or cancelled and at least one is completed, the completion review runs and the decision log names the skipped tasks. A story of only cancelled tasks is skipped. The stuck-running check advances an Automaton blocked only by cancelled tasks after its grace period (`autonomous.stuck_running_seconds`). A task that is not running and not queued (cancelled, completed, failed, blocked, or pending without a session) can be removed while the Automaton is running, on REST, MCP, CLI and the chat verb.

## What the mobile app needs

- Show the remove-task control on a running Automaton for a task that is cancelled, completed, failed, blocked, or pending with no session; keep it hidden for a running or queued task. The web page added the string `prd_remove_task_running_hint` in all five locales.
- Treat a cancelled task as skipped, not blocking, in any story or Automaton progress rollup the app computes on its own.
- The help text of `autonomous.stuck_running_seconds` changed in all five locales (`settings_stuck_running_seconds_help`): if only cancelled tasks remain besides completed ones, the Automaton advances to the completion review.
