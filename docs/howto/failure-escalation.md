---
docs:
  index: true
  topics: [failure, escalation, retry, review, automata, autonomous]
exec_params:
  - {name: automaton_id, required: true, description: "Automaton ID whose failure escalation settings and held tasks to inspect"}
  - {name: task_id, required: false, description: "Task ID held for review (needed only for the review step; leave empty to skip it)"}
exec_steps:
  - tool: autonomous_config_get
    description: Read the daemon-wide failure escalation default (autonomous.failure_escalation)
    args: {}
    read_only: true
  - tool: autonomous_prd_get
    description: Read the Automaton's failure_escalation override, and each task's escalation_failures, attempts and review_summary
    args: {id: "{{params.automaton_id}}"}
    read_only: true
  - tool: autonomous_prd_set_failure_escalation
    description: Raise the Automaton's failed-attempt limit to 4 (edit the policy object to suit; this changes the live override)
    args:
      id: "{{params.automaton_id}}"
      policy: {max_failures: 4}
    read_only: false
  - tool: autonomous_prd_resolve_task_review
    description: Retry a task held for review with no changes (use the CLI, web UI or comm channel for a changed spec, worker, scope or cancel)
    args:
      id: "{{params.automaton_id}}"
      task_id: "{{params.task_id}}"
      action: retry
    read_only: false
---
# How-to: Failure escalation

A task that keeps failing should not burn retries on the same worker forever,
and it should not quietly fail either. Failure escalation does three things:

1. **Moves the task up a ladder of workers.** After each failed attempt the
   next attempt can run on a different worker, for example a local model first
   and then a stronger Claude model that reads the earlier failure reports.
2. **Counts the failures.** After a set number of failed attempts (3 by
   default, 4 or more if you want) the task stops.
3. **Asks you.** The task is held with the status `review_needed` and a report of
   every attempt. Nothing else is spent on it until you retry it, change it, or
   cancel it.

This is the decision the daemon used to make for you by failing the task. Now
you make it, with the evidence in front of you.

## Why this exists

A worker that fails the same task three times usually fails the fourth time
too, for the same reason. Retrying it unchanged wastes time and tokens. A
different worker, or the same worker with the earlier failures in its prompt,
has a real chance of getting past the problem. When even that does not work,
the honest answer is "this needs a human," and the daemon should say so once,
clearly, rather than keep trying or silently marking the task failed.

## The policy

The policy has three fields. Each can be set at three levels (see
[Override order](#override-order) below).

| Field | Meaning | Default |
|---|---|---|
| `enabled` | `true` turns escalation on. `false` restores the old behaviour: retry up to `autonomous.auto_fix_retries`, then mark the task failed. | `true` |
| `max_failures` | Failed attempts after which the task is held for review. Minimum 1; 4 or more is allowed. | `3` |
| `steps` | The ladder of workers, in order. Empty means every attempt uses the task's normally resolved worker. | empty |

Each step has these fields:

| Field | Meaning |
|---|---|
| `backend` | The worker backend, for example `pi-prompt` or `claude-code`. Empty means the task's normally resolved worker. |
| `model` | The model on that backend. Needs a backend. |
| `effort` | Effort level for this step only (JSON form only). Empty means the task's effort. |
| `attempts` | How many consecutive failed attempts this step covers. Default 1. |

### How the ladder is walked

Attempts are counted as failed attempts since the last operator action. Step 1
covers its `attempts` failed attempts, step 2 covers the next ones, and so on.
The **last step covers every remaining attempt**, so a one-step ladder means
"this worker for every attempt until the limit."

With `steps: pi-prompt:ollama/qwen3.8:27b@2,claude-code:claude-opus-5-5` and the
default `max_failures` of 3:

| Attempt | Failed attempts so far | Worker |
|---|---|---|
| 1 | 0 | `pi-prompt` / `ollama/qwen3.8:27b` |
| 2 | 1 | `pi-prompt` / `ollama/qwen3.8:27b` |
| 3 | 2 | `claude-code` / `claude-opus-5-5` |
| — | 3 | Held for review |

Raise `max_failures` to 4 and the Claude step gets two attempts instead of one.

Every time an attempt moves the task to a different worker, the Automaton's
decision log gets a `failure_escalation` entry naming the worker.

### Model names must fit the backend

The daemon checks each step before the Automaton starts. A step that cannot run
refuses the start and names the task and the step.

- `claude-code` takes bare Claude names such as `claude-opus-5-5` or
  `claude-sonnet-5-5`. It refuses provider-qualified names with a `/`.
- `pi-prompt` (and the other pi-based backends) takes provider-qualified names
  such as `ollama/qwen3.8:27b`. It refuses bare Claude names.

## Examples

### GPU only

Every step runs on local models on your own GPU. Two tries on the usual local
model, then a larger local model you have pulled:

```sh
datawatch autonomous failure-escalation global \
  --steps "pi-prompt:ollama/qwen3.8:27b@2,pi-prompt:ollama/<larger-local-model>"
```

### Claude only

Every step runs on Claude. Two tries on a Sonnet model, then one on Opus, with
a higher limit so Opus gets a turn:

```sh
datawatch autonomous failure-escalation global --max-failures 4 \
  --steps "claude-code:claude-sonnet-5-5@2,claude-code:claude-opus-5-5"
```

### Mixed: a local model, then a stronger Claude model

The usual setup. Local models do the cheap first tries. The Claude step gets the
earlier failure reports in its prompt, so it starts from what went wrong
instead of from nothing. Use this on one Automaton, not the whole daemon, if
you only want it for risky work:

```sh
datawatch autonomous failure-escalation set <automaton-id> --max-failures 4 \
  --steps "pi-prompt:ollama/qwen3.8:27b@2,claude-code:claude-opus-5-5"
```

For a single task that needs a stronger worker from the start, put the same
steps on that task with `--task <task-id>`.

## What the retry prompt contains

When a task has failed attempts on record, the next worker receives them before
it starts. The prompt begins with a line saying earlier attempts did not finish
successfully, then lists each attempt, oldest first:

- the attempt number and the worker it ran on
- how long it ran and how it ended (`failed`, `stalled` or `cancelled`)
- the verifier's summary and issues
- the files it changed

The prompt is capped at about 12,000 characters. When the reports would be
longer, the oldest attempts are shortened to one line each. If that is still
too long, the oldest are left out, and the prompt says which attempts were left
out. The worker is never silently missing a failure.

Any operator reset of the task, including a retry from review, starts the failure
count again from zero, so the ladder starts over at step 1. The exception is a
review retry that changes the worker: the worker you chose is kept for the
attempts after it, and the ladder does not replace it. A review retry without a
worker change lets the ladder apply again.

## When a task needs review

A task reaching `max_failures` is **held**, not failed. What you see:

- **Web UI.** The task row carries a "Held for review" badge. Expand the row for the attempt list, the full report and the
  action buttons. On the Automaton header, in the Automaton list rows and on the
  Automation strip, a count shows how many tasks are waiting.
- **Status output.** `datawatch autonomous status` lists each held task under
  `review_needed`, with the Automaton, the task, the failed-attempt count, the
  last worker and when it was held. The comm channel's `autonomous status`
  adds one line per held task and tells you the command to decide it. MCP
  `autonomous_status` and REST `GET /api/autonomous/status` carry the same
  `review_needed` array.
- **Alert.** One error-level alert, titled "Automaton task needs review", is raised
  when the task is held. Its body names the Automaton, the task, the failed-attempt
  count, the last worker and the ways to act. You get one alert per hold, not one
  per failed attempt.
- **The Automaton.** Other tasks that do not depend on the held one keep running.
  Tasks that depend on it wait. The Automaton shows as blocked until you act.
- **The report.** The task keeps every attempt, and its review report is a plain
  text summary of each one, with the total time spent.

## The five operator actions

Every action is available on every surface. The table gives the way in on each.

| Action | What it does | Web UI | CLI | REST (`resolve_task_review`) | MCP (`autonomous_prd_resolve_task_review`) | Comm channel |
|---|---|---|---|---|---|---|
| **Retry** | Requeues the task on its normally resolved worker, with the earlier reports in the prompt. | **Retry** button on the task | `datawatch autonomous resolve-review <automaton-id> --task <task-id> --action retry` | `{"task_id":"…","action":"retry"}` | `action=retry` | `autonomous review-task <automaton> <task> retry` |
| **Edit spec and retry** | Replaces the task spec, then retries. | **Edit spec and retry** (opens a form) | add `--spec "<new spec>"` to the retry | add `"spec":"…"` | `spec=…` | add `spec=…` to the retry |
| **Change worker** | Retries with a different backend, model and effort for this task. | **Change worker** (opens a form) | add `--backend`, `--model`, `--effort` | add `"backend"`, `"model"`, `"effort"` | `backend`, `model`, `effort` | add `backend=… model=… effort=…` |
| **Narrow scope** | Retries with the list of files the task may change cut down. | **Narrow scope** (opens a form) | add `--files a.go,b.go` | add `"files":["a.go","b.go"]` | `files` | add `files=a.go,b.go` |
| **Cancel** | Cancels the task. The Automaton can roll up without it. | **Cancel task** button on the task | `datawatch autonomous resolve-review <automaton-id> --task <task-id> --action cancel` | `{"task_id":"…","action":"cancel"}` | `action=cancel` | `autonomous review-task <automaton> <task> cancel` |

Notes on the actions:

- A retry can carry a spec, a worker and a file list in one go. Each change is
  checked before anything is written, so a refused change leaves the task held.
- A `--note` (REST and MCP `note`, comm `note=…`) is recorded on the decision log
  and given to the next attempt.
- Asking to act on a task that is not held answers **409** on REST and an error
  on the other surfaces. Nothing changes.

### Examples

```sh
# Retry with a changed worker and a narrowed file list, with a note
datawatch autonomous resolve-review <automaton-id> --task <task-id> --action retry \
  --backend claude-code --model claude-opus-5-5 --effort high \
  --files internal/parser/parse.go --note "the failure is in the parser only"

# Cancel
datawatch autonomous resolve-review <automaton-id> --task <task-id> --action cancel
```

```
You: autonomous review-task <automaton-id> <task-id> retry backend=claude-code model=claude-opus-5-5 note=narrowed to the parser
Bot: {"id":"<automaton-id>", ...}
```

```sh
# REST
curl -sk -X POST -H "Content-Type: application/json" \
  -d '{"task_id":"<task-id>","action":"retry","backend":"claude-code","model":"claude-opus-5-5","files":["internal/parser/parse.go"],"note":"narrowed to the parser"}' \
  https://localhost:8443/api/autonomous/prds/<automaton-id>/resolve_task_review
```

## Override order

Each field is resolved on its own, from the most specific level that sets it:

1. **Task** override (`failure_escalation` on the task)
2. **Automaton** override (`failure_escalation` on the Automaton)
3. **Daemon default** (`autonomous.failure_escalation` in the config)

So a task can set only `max_failures` and still inherit the Automaton's
`steps`. The CLI `show` command prints the override at the level you ask about
next to the daemon default, so you can see what the level above supplies.

To remove an override, clear it at that level. Clearing a task or Automaton
override lets it inherit again.

### Setting each level

**Daemon default**

| Surface | How |
|---|---|
| Web UI | Settings → Automata → Autonomous block: *Failure escalation enabled*, *Failed attempts before the task is held for review*, *Escalation steps* |
| CLI | `datawatch autonomous failure-escalation global --enabled true --max-failures 4 --steps "…"`; or `datawatch config set autonomous.failure_escalation.max_failures 4` |
| REST | `PUT /api/config` with `autonomous.failure_escalation.enabled`, `.max_failures` or `.steps` |
| MCP | `config_set` with `key` and `value`, using the same keys |
| Comm channel | `configure autonomous.failure_escalation.max_failures=4` |
| YAML | `autonomous.failure_escalation: {enabled: true, max_failures: 4, steps: [...]}` in `config.yaml` |

**Automaton override**

| Surface | How |
|---|---|
| Web UI | Automaton detail → Overview → *Failure escalation override*. Empty fields inherit. **Clear override** removes it. |
| CLI | `datawatch autonomous failure-escalation set <automaton-id> --max-failures 4`; `datawatch autonomous failure-escalation clear <automaton-id>` |
| REST | `POST /api/autonomous/prds/{id}/set_failure_escalation` with `{"policy":{"max_failures":4}}`; `"policy": null` clears |
| MCP | `autonomous_prd_set_failure_escalation` with `id` and `policy`; `clear=true` removes it |
| Comm channel | `autonomous set-failure-escalation <automaton-id> max_failures=4`; `autonomous set-failure-escalation <automaton-id> clear` |

**Task override**

| Surface | How |
|---|---|
| Web UI | Automaton detail → Stories → expand the task → the same editor, for that task |
| CLI | add `--task <task-id>` to `set`, `clear` or `show` |
| REST | the same `set_failure_escalation` call with `"task_id":"<task-id>"` |
| MCP | `autonomous_prd_set_failure_escalation` with `task_id` set |
| Comm channel | `autonomous set-failure-escalation <automaton-id> <task-id> max_failures=4` |

To see the effective value and where it comes from, run
`datawatch autonomous failure-escalation show <automaton-id> [--task <task-id>]`
or read the Automaton with `autonomous_prd_get` and the daemon default with
`autonomous_config_get`.

### Steps in the text form

Steps can be written as compact text on the CLI, in `PUT /api/config`, and in
the comm channel and the web UI field: `backend:model[@attempts]`, separated by
commas. Use `-` alone for a step on the task's normal worker. The text form has
no effort field, so set effort in the JSON form when you need it:

```json
[
  {"backend": "pi-prompt", "model": "ollama/qwen3.8:27b", "attempts": 2},
  {"backend": "claude-code", "model": "claude-opus-5-5", "effort": "high"}
]
```

## Upgrading from the old retry count

Before this policy, an attempt that failed was retried up to
`autonomous.auto_fix_retries` (default 1), which meant two attempts in all, and
then the task was marked failed.

With failure escalation on, which is the default, a task gets `max_failures`
attempts (default 3) and is then held for review instead of failed. An Automaton
with no settings of its own therefore gets one more attempt than before, and a
held task waits for you instead of ending as failed.

To keep the old behaviour:

```sh
datawatch config set autonomous.failure_escalation.enabled false
```

or, on one Automaton, `datawatch autonomous failure-escalation set <automaton-id> --enabled false`.
With escalation off, a task that fails its attempts is marked failed, and the
retry count is still `autonomous.auto_fix_retries`.

## See also

- [retry-policy.md](retry-policy.md): failure categories and the retry budgets
  that apply before and after this policy
- [autonomous-review-approve.md](autonomous-review-approve.md): the review gate
  before an Automaton runs
- [automata-orchestrator.md](automata-orchestrator.md): chaining Automata into an
  Automation
- [../api/autonomous.md](../api/autonomous.md): REST reference
