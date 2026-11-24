---
docs:
  index: true
  topics: [attempt, cap, back-off, retry, hold, loop, force reset, operator, automata, autonomous]
exec_params:
  - {name: automaton_id, required: false, description: "Automaton ID to inspect; leave empty to read the daemon-wide settings only"}
exec_steps:
  - tool: autonomous_config_get
    description: Read the daemon-wide Automata settings, including the attempt cap block (all four values, defaults filled in)
    args: {}
    read_only: true
  - tool: autonomous_prd_get
    description: Read one Automaton; each task carries attempts_total, attempt_cap_held and attempt_not_before, and a task held at the cap has the failure source attempt-cap-hold
    args: {id: "{{params.automaton_id}}"}
    read_only: true
---
# How-to: Attempt cap

A task that keeps failing is retried on a ladder of workers and then held for
you. That ladder starts over every time the task is reset, retried after
review, or cancelled and put back in the queue, so a model that cancels and
retries a task could loop for as long as it liked.

The attempt cap closes that. The daemon counts every attempt a task is started
for, in all, and keeps the count in the task. No reset, cancel, retry or daemon
restart clears it. When the count reaches the cap, the task is held, you get an
alert and a question, and nothing starts the task again until you do.

## What counts

- **One attempt** is one start of a worker for the task: a new session, or a
  retry that continues the earlier session with the failure report.
- **Not counted:** a start that failed because of the infrastructure and is
  retried (see [infrastructure retry](infrastructure-retry.md)), a session that
  never started, a daemon restart that cut an attempt short, and a session the
  daemon adopted again after a restart. These cost the task no attempt.
- **Counted across:** failed attempts, stalled attempts, cancelled attempts,
  resets, review retries and restarts. The failure ladder
  (`autonomous.failure_escalation`) still counts failed attempts since the last
  operator action; the attempt count is a second, separate number.

A task that already ran before the cap existed starts counting from zero.

## The wait between attempts

The first `free_attempts` attempts start at once (default 3, the length of the
failure ladder). Each attempt after them waits the next delay in
`backoff_minutes` (default 5, 15, 45; the last delay repeats), counted from the
end of the previous attempt. A cancel by you or by a session counts as the end
of an attempt, so cancel-and-retry waits too. The wait is stored on the task as
`attempt_not_before`, so a daemon restart does not shorten it, and the task
shows "Next attempt not before ..." while it waits.

With the defaults a task can run six attempts: three at once, then one after 5
minutes, one after 15, one after 45. The numbers are the failure ladder run
twice (the second run is the retry a supervisor or you may ask for), with the
wait growing by a factor of three so a fault nobody has fixed gets a little
more time each round, 65 minutes in all.

## At the cap

When the count reaches `max_total` (default 6), the daemon:

1. holds the task in `review_needed` with the failure source `attempt-cap-hold`
   and a report of every attempt;
2. writes an `attempt_cap_hold` decision on the Automaton;
3. raises one alert, "Automaton task reached its attempt cap";
4. when the operator question channel (`autonomous.questions.enabled`) is on,
   raises one question: **Clear the count and run it again**, or **Cancel the
   task**. The question's default is to leave the task held.

From then on a retry from review, a reset, or a force reset made by a session
does not start the task. The review retry says that the task is held at the
cap. The task leaves the hold in two ways only: you cancel it, or you
force-reset it.

## The force reset

Only the operator's force reset clears the count. It also clears the hold, sets
the task back to pending, and writes an `attempt_cap_cleared` decision naming
who did it and how many attempts were cleared. The REST audit entry for the
reset says `clears_attempt_count`.

A reset made by a doer, a supervisor session, a housekeeping repair or the gate
repair does not clear the count. The daemon decides who is calling from the
credential, not from what the call claims: a session's scoped credential names
the session, so a session cannot pass as the operator. A force reset made by a
session still requeues a completed or cancelled task, but it keeps the count.

A force reset that clears the count and does not come from the web app or from a
session credential raises one alert to you and writes a `safeguard_action` audit
entry. A plain REST call or `datawatch autonomous prd-reset-task --force` with no
session token is exactly that case: today the local API needs no token, so the
alert is how you learn that something other than you cleared a count. The alert
names the Automaton and task, the reason given (`--reason`, the `reason` field
of REST and MCP, or `reason <text>` in chat; "no reason given" otherwise), the
caller's address, user agent and claimed actor label. The web app sends an
`X-Datawatch-Client: pwa` header, so its **Force reset** and **Re-queue** buttons
raise no alert but are still audited with the marker. The header is not a
secret and only ever hides the alert. A force reset by a session never clears the
count, so it raises nothing. `audit.safeguard_alert: false` turns the alert off;
see [pause-new-work.md](pause-new-work.md#an-alert-when-a-script-resumes).

If you answer the cap question with the force-reset option, it is the same as
the force reset. A session that answers it clears nothing; the daemon writes an
`attempt_cap_answer_ignored` decision.

## Settings

| Key | Default | Accepted | What it does |
|---|---|---|---|
| `autonomous.attempt_cap.enabled` | `true` | `true`, `false` | Turn the cap and the wait on or off. Off restores the earlier behaviour: only the failure ladder limits a task. |
| `autonomous.attempt_cap.max_total` | `6` | whole number 1 to 1000 | Attempts a task may be started for in all. |
| `autonomous.attempt_cap.free_attempts` | `3` | whole number 1 to 1000 | Attempts that start at once. |
| `autonomous.attempt_cap.backoff_minutes` | `[5, 15, 45]` | list of whole minutes, 0 to 10080 | Wait before each later attempt; the last repeats; `[]` means no wait. |
| project policy `attempt_cap` | unset | whole number 1 or more | Replaces `max_total` for one project's tasks. |

A change applies to the next attempt with no restart. A bad value refuses the
whole change and names the key.

## Where to find it

| Surface | How |
|---|---|
| **YAML** | `config.yaml` -> `autonomous.attempt_cap:` with `enabled`, `max_total`, `free_attempts`, `backoff_minutes`; a project's `policy:` block takes `attempt_cap` |
| **REST** | `GET /api/config` shows the block under `autonomous.attempt_cap`; `PUT /api/config` with the dotted keys; every task of `GET /api/autonomous/prds/{id}` and `GET /api/autonomous/prds/{id}/tasks` carries `attempts_total`, `attempt_cap_held` and `attempt_not_before`; `POST /api/autonomous/prds/{id}/reset_task` with `force` is the force reset |
| **MCP** | `autonomous_config_set` with `attempt_cap_enabled`, `attempt_cap_max_total`, `attempt_cap_free_attempts`, `attempt_cap_backoff_minutes`; `autonomous_prd_get` and `autonomous_prd_tasks` for the count; `autonomous_prd_reset_task` with `force` |
| **CLI** | `datawatch config set autonomous.attempt_cap.max_total 8`; `datawatch autonomous tasks <id>` for the count; `datawatch autonomous prd-reset-task <id> <task> --force --reason "why"` |
| **Chat** | `configure autonomous.attempt_cap.max_total=8`; `autonomous reset-task <id> <task> force reason <text>` |
| **Web app** | Settings -> Automata -> Attempt cap; the project policy editor field **Attempt cap**; each task row shows the count, the hold and the wait, and a held task has a **Force reset** button |

## Common situations

- **A task is held at the cap and you want it to run again.** Look at the
  attempts in its report first: six attempts that fail the same way will fail
  a seventh time. Change the spec or the worker if you can, then force-reset.
- **The cap is too tight for one project.** Set `attempt_cap` in that
  project's policy rather than raising it for everything.
- **You want no wait.** Set `backoff_minutes` to an empty list.
- **You want the earlier behaviour.** Set `enabled` to `false`.
