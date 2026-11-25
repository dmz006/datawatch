# [sync] Operator pause holds more engine work, an operator-ended planner stays stopped, attempt timing — parity draft for the datawatch-app native clients (Android, iPhone)

**Not filed yet.** This is the tracked draft; the GitHub issue is opened when
the operator files it. This draft is a file write only: it runs no hosting CLI
and creates nothing on any host.

**What changes in the daemon.** An operator pause now also holds the
stalled-merge retry, the merge pass after a task finishes, further planner
sessions of a plan the engine started, and the planning and running of child
and scan-fix Automata. An operator-started plan is no longer held by a usage
back-off. A planner session the operator ends is not started again. Task
attempt records carry two timings.

## Affected area

No new endpoint, setting, tool, verb or screen. The client-visible facts are
read-only:

| Fact | Where it shows | What the app should do |
|---|---|---|
| Task attempt fields `queue_wait_seconds` and `verify_seconds` (integers, omitted when 0) | `GET /api/autonomous/prds/{id}`, a task's `attempts[]` | Optional: show them on the attempt row; ignore if absent. `verify_seconds` currently includes the worker's run. |
| A parent task that spawns a child waits as `waiting_capacity` with `wait_reason` starting `paused by operator` | Task status and wait reason | Nothing new: show the reason as for any waiting task. |
| An engine-started plan waiting for the resume shows the pause as the Automaton's wait reason | Automaton wait reason | Show the text as given. |
| An Automaton in `failed` with `last_decompose_failure.detail` "the planner session was ended by the operator (...)" | Automaton status and planning failure | Show the detail as given; the existing reset to draft plans it again. |

## Surfaces

| Surface | Status |
|---|---|
| Config (yaml key) | Not touched: no new setting. |
| REST / openapi | Read-only attempt fields only. |
| MCP, CLI, comm, PWA | Not touched. |
| Federation | Not touched: each daemon holds its own work. |
| datawatch-app | This draft. |
