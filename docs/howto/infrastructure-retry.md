---
docs:
  index: true
  topics: [infrastructure, retry, back-off, transient, held, quota, restart, automata, autonomous]
exec_params:
  - {name: automaton_id, required: false, description: "Automaton ID to inspect; leave empty to read every Automaton with retried or held tasks"}
exec_steps:
  - tool: autonomous_config_get
    description: Read the daemon-wide Automata settings
    args: {}
    read_only: true
  - tool: autonomous_infra_retries
    description: Read the infrastructure retry policy in effect (all seven values, defaults filled in) and the tasks that were retried or are held, newest first
    args: {automaton: "{{params.automaton_id}}", limit: 50}
    read_only: true
  - tool: autonomous_prd_get
    description: Read one Automaton, with each task's infra_retries history, infra_retry_count and failure_reason (a held task has the source infra-retry-hold)
    args: {id: "{{params.automaton_id}}"}
    read_only: true
---
# How-to: Infrastructure retry

A task can fail for a reason that has nothing to do with the task. The disk the
worker writes to is full, the worker session was killed before the task was
delivered, or the daemon restarted while the task was running. Counting that
against the task, and moving it up the worker ladder, punishes the task for a
fault it did not cause.

Infrastructure retry handles these failures on their own track. The Automaton
waits, then runs the same task again, without moving it up the worker ladder. Nothing is counted
against the task's failure budget while it does. If the fault outlasts the
retries, the task is held with the cause written on it, and you decide.

## Why it exists

Three situations account for nearly all of these failures:

- **The temp-directory quota is full.** A worker, or the daemon on its behalf,
  cannot write a file because the disk, the quota or the file system will not
  take it. The task did not get a fair attempt.
- **The worker session was killed before the task arrived.** The session ended
  after it started but before the task text was delivered to it. No work was
  done.
- **The daemon restarted.** The worker went away with the daemon, mid-task.

Without this feature each of these used up a failed attempt, so a task could be
escalated, or held for review as if it were a bad task, after a few minutes of
trouble that fixed itself.

## What counts as transient

A failure is retried only when it is one of four classes. These are the values
you can list in `autonomous.infra_retry.categories`.

| Class | What it means |
|---|---|
| `spawn_error` | The worker could not be started at all. |
| `storage_write` | A write failed because the device is full, the quota is used up, or the file system is read-only. |
| `session_killed_before_delivery` | The worker session ended before the task was delivered to it. |
| `daemon_restart` | The daemon restarted or shut down while the task was running. |

Everything else is not an infrastructure failure and takes the usual path: a
verifier failure, a task that failed on its content or its file scope, a worker
that stalled after it had started, and a usage or rate limit. The classifier
looks only at what kind of error it is, never at what the task was doing.

A session that never started is not on this list either. It has its own start
budget (see [Limits](#limits)).

## What happens

When an attempt ends with one of the four classes, and infrastructure retry is
on, the Automaton:

1. Records the retry on the task (class, reason, time, delay) and in the
   Automaton's decision log.
2. Shows the task as waiting, with the time of the next try, for example
   `infrastructure retry 2 of 3 at 14:05:09 (storage_write)`.
3. Waits out the back-off delay, then runs the task again.

By default a task gets **3 tries**, with delays of **1, 5 and 9 minutes**
(`60,300,540` seconds), which is 15 minutes in all. If there are more tries than
delays, the last delay repeats.

**Nothing is counted against the failure budget.** These retries are not failed
attempts, so the failure-escalation ladder does not move and the limit in
[failure-escalation.md](failure-escalation.md) is not touched. A task that has
been retried three times for a full disk still has all its normal attempts left.

A retry that works ends the incident. The consecutive count goes back to zero
and the records stay on the task as history.

## What you see

- **The task's retry list.** Each retried task keeps a list of its retries, with
  the number, the class, the reason (cut to a reasonable length), when it
  happened and when the next try is due. `datawatch autonomous infra-retries`
  and the `autonomous_infra_retries` tool print it. `autonomous_prd_get` returns
  it as `infra_retries` on the task, with `infra_retry_count` for the current
  run of retries.
- **The "Retry n of max" badge.** In the web app, a task that is being retried
  carries a badge such as `Retry 2 of 3` on its row in the Automaton detail. A
  held task carries a "Held for the operator" badge beside it, and its expanded
  body shows the cause.
- **The Infrastructure retries card.** On the Automation view, a card shows
  whether automatic retry is on, how many tries a task gets, and what happens
  when they run out, then one block per task that was retried or is held. Each
  block lists the latest retries with class, reason and time. A held task shows
  its cause and, when the daemon raised one, a button that opens the question.
  The card refreshes by itself.
- **The decision log.** Every scheduled retry adds an `infra_retry` entry to the
  Automaton's decisions.

## When the retries run out

When a task has used all its tries and the next failure is again a retried
class, what happens depends on `autonomous.infra_retry.exhausted_action`.

**`hold` (the default).** The task is held for you, in the same
`review_needed` state used by failure escalation:

- The failed attempt is recorded, but it is **not counted**, so the failure
  budget is untouched.
- The task carries the cause: the class and the original error text. The review
  report lists each retry, the time they took and every recorded attempt.
- **One error-level alert** is raised, titled "Automaton task held after
  infrastructure retries". It names the Automaton, the task, the class, the
  cause and how many retries were used.
- **One operator question** is raised, when `autonomous.infra_retry.raise_question`
  is on and the operator question channel (`autonomous.questions.enabled`) is
  on. It asks whether to retry the task now or cancel it. The default answer is
  retry. If the question channel is off, the alert and the report carry the
  cause on their own.
- Other tasks that do not depend on the held one keep running. Tasks that depend
  on it wait.

**You answer in one of two ways: retry or cancel.** Answer the operator question,
or resolve the held task the way you resolve any held task:

```sh
datawatch autonomous resolve-review <automaton-id> --task <task-id> --action retry
datawatch autonomous resolve-review <automaton-id> --task <task-id> --action cancel
```

Retry starts the task again with a **fresh set of infrastructure retries**, so
if the cause is still there the task is held again after another round. Cancel
cancels the task. An answered question and a direct resolve do the same thing,
and resolving the task directly withdraws the open question so no stale
question is left waiting.

The full set of review actions (edit spec, change worker, narrow scope) is in
[failure-escalation.md](failure-escalation.md#the-five-operator-actions).

**`count`.** The failure is booked as one ordinary failed attempt, labelled with
its class, and the task takes the usual failure path (the ladder moves, and the
task is held for review once the failure limit is reached). Nothing is raised
beyond what failure escalation already does. Use this when you would rather the
daemon never stop on infrastructure trouble.

## Dependents that failed behind it recover on their own

Sometimes a task's dependency fails for an infrastructure reason and the tasks
that depend on it are failed behind it. Those dependents did nothing wrong, so
they are marked with the infrastructure cause and the task where the chain began
(the root).

When the root is no longer failed, cancelled, blocked or held, for example
because you answered "retry" on the held root, the dependents marked behind it
go back to pending on their own. You do not reset them one by one:

- Only tasks failed because of an infrastructure dependency are reset. A task
  that failed for any other reason, one whose root is still failed, cancelled or
  held, and one with another unmet dependency, is left alone.
- A reset task keeps its attempts and retry history, and starts with a retry
  count of zero.
- If the Automaton had ended failed or blocked and no failed, blocked or held
  task remains, it returns to running.
- One `infra_dependents_recovered` decision records each root and the tasks that
  were reset.

A dependency that was cancelled, or that failed on its content or scope, marks
nothing: its dependents fail as they always did. Turn the whole behaviour off
with `autonomous.infra_retry.retry_dependents: false`.

## Configuration

Every key lives under `autonomous.infra_retry` and is read each time a failure
is decided, so a change applies at once with no restart. An unset key reads its
default.

| Key | Default | Allowed values | Effect |
|---|---|---|---|
| `autonomous.infra_retry.enabled` | `true` | `true`, `false` | Turns infrastructure retry on. When off, an infrastructure failure is an ordinary failure. |
| `autonomous.infra_retry.max_tries` | `3` | whole number from 1 to 1000 | How many infrastructure retries a task gets in a row before the exhausted action applies. |
| `autonomous.infra_retry.backoff_seconds` | `[60, 300, 540]` | list of whole seconds, 0 to 604800 each, at most 1000 entries | The delay before each retry. The last entry repeats when there are more tries than entries. An explicit empty list means retry with no delay. |
| `autonomous.infra_retry.categories` | all four classes | any of `spawn_error`, `storage_write`, `session_killed_before_delivery`, `daemon_restart` | The classes treated as transient. A class left out takes the ordinary failure path. An explicit empty list retries none. |
| `autonomous.infra_retry.exhausted_action` | `hold` | `hold`, `count` | What happens when the tries are used up: hold the task for you, or count it as an ordinary failure. |
| `autonomous.infra_retry.retry_dependents` | `true` | `true`, `false` | Mark dependents failed behind an infrastructure failure, and return them to pending once the root recovers. |
| `autonomous.infra_retry.raise_question` | `true` | `true`, `false` | Raise an operator question when a task is held. Needs `autonomous.questions.enabled`. |

A bad value is refused and nothing is written: the refusal names the key and the
valid values. Both lists accept a JSON array or a comma list, for example
`60,300,540`. A value of `[]` or an empty string is kept as an explicit empty
list, which is different from leaving the key unset.

## Surfaces

Each surface reads the same policy, and each takes the same seven keys.

| Surface | Read the retries | Set the policy |
|---|---|---|
| **YAML** | not applicable | `autonomous.infra_retry:` in `config.yaml` with any of the seven keys |
| **REST** | `GET /api/autonomous/infra-retries` with optional `automaton` and `limit` (1 to 1000, default 50); the policy is also under `autonomous.infra_retry` in `GET /api/config` | `PUT /api/config` with `autonomous.infra_retry.<key>` |
| **MCP** | `autonomous_infra_retries` (read-only; `automaton`, `limit`) | `autonomous_config_set` with `infra_retry_enabled`, `infra_retry_max_tries`, `infra_retry_backoff_seconds`, `infra_retry_categories`, `infra_retry_exhausted_action`, `infra_retry_retry_dependents`, `infra_retry_raise_question`; or `config_set` with the dotted keys |
| **CLI** | `datawatch autonomous infra-retries [automaton-id] [--limit N] [--json]` | `datawatch config set autonomous.infra_retry.max_tries 5` |
| **Comm channel** | `autonomous infra-retries [automaton-id] [limit]` | `configure autonomous.infra_retry.max_tries=5` |
| **PWA** | the Infrastructure retries card on the Automation view; the retry badges on the Automaton detail | Settings → General → Autonomous |

Examples:

```sh
# Read the policy and the retried or held tasks
datawatch autonomous infra-retries

# Five tries, with a longer wait before the last
datawatch config set autonomous.infra_retry.max_tries 5
datawatch config set autonomous.infra_retry.backoff_seconds 30,120,300,600,900

# Retry only the full-disk and restart classes
datawatch config set autonomous.infra_retry.categories storage_write,daemon_restart

# Count instead of hold
datawatch config set autonomous.infra_retry.exhausted_action count
```

```yaml
autonomous:
  infra_retry:
    enabled: true
    max_tries: 3
    backoff_seconds: [60, 300, 540]
    categories: [spawn_error, storage_write, session_killed_before_delivery, daemon_restart]
    exhausted_action: hold
    retry_dependents: true
    raise_question: true
```

```sh
# REST
curl -sk https://localhost:8443/api/autonomous/infra-retries?limit=20
curl -sk -X PUT -H "Content-Type: application/json" \
  -d '{"autonomous.infra_retry.max_tries": 5}' \
  https://localhost:8443/api/config
```

```
You: autonomous infra-retries 5
Bot: Infrastructure retry: on, 3 tries, back-off 60s/300s/540s, hold when used up
     Automaton <automaton-id> "<title>", task <task-id> "<title>": pending, retry 2 of 3
       retry 1: storage_write: no space left on device (2026-10-31 14:00:09)
```

The comm channel reply is cut to fit one chat message. It drops the oldest tasks
first, always keeps the newest, and says how many it left out; add an Automaton
id or a smaller limit to narrow the list.

## Limits

- **A restart-caused retry does not wait longer than the first delay.** When the
  daemon comes back and finds a task whose worker is gone, that is recorded as a
  `daemon_restart` retry, and the wait before the replacement starts is capped
  at the first configured delay (60 seconds by default), even if the retry
  number would normally use a longer one. The restart already cost time, and
  the worker's own disk is rarely the cause.
- **A session that never started keeps its own start budget.** A worker session
  that never started is not classified as an infrastructure failure, whatever
  else the error also says. It follows the session-start settings, not
  `autonomous.infra_retry`.
- **Retries do not climb the ladder.** An infrastructure retry is recorded as a
  re-queued attempt. It does not count toward the failure limit, so it never
  moves the task to the next worker. Worker changes belong to failure
  escalation.
- **Only consecutive failures count.** The tries counter belongs to one run of
  infrastructure failures on a task. A retry that works, or a reset of a
  dependent, ends it and starts the next incident from zero. The history of
  retries on the task stays, up to the newest 50.
- **Throttling is not an infrastructure failure.** Usage and rate limits have
  their own handling and are never retried by this feature.

## See also

- [failure-escalation.md](failure-escalation.md): the worker ladder, the failure
  budget, and the review actions a held task uses
- [retry-policy.md](retry-policy.md): failure categories and the other retry
  budgets
- [automation-lane-fill.md](automation-lane-fill.md): the Automation view, which
  carries the Infrastructure retries card
- [../api/autonomous.md](../api/autonomous.md): REST reference
