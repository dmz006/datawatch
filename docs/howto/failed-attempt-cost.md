---
docs:
  index: true
  topics: [attempt cost, first attempt, failed attempt, retry, resume, escalation, no retry, attempt record, automata]
exec_params:
  - {name: automaton_id, required: true, description: "Automaton ID whose task attempts to read back (leave the attempt rows for the task review)"}
exec_steps:
  - tool: autonomous_config_get
    description: Read the attempt-cost keys (first_attempt, first_attempt_escalate_after_failures, retry_resume_max_context_tokens) from the daemon's autonomous config
    args: {}
    read_only: true
  - tool: autonomous_prd_get
    description: Read an Automaton's tasks, including each task's attempts (backend, backend_reason, resumed, resume_reason, no_retry_reason) and its review entry
    args: {id: "{{params.automaton_id}}"}
    read_only: true
---

# How-to: Failed-attempt cost

A task that fails does not have to cost three full starts. Each worker session
begins with a large fixed context, so every failed attempt that starts a fresh
session pays that context again. Attempt cost is the set of daemon-wide choices
that keep this down: cheap work is tried on a cheap worker first, the strong
worker starts only once a task has actually failed, and a retry continues the
failed session when it can.

This page explains what a failed attempt costs and why, which worker a first
attempt uses, when a retry resumes and when it starts fresh, which failures go
to review at once, how to read the attempt record, and every setting on each of
the six surfaces that accept it.

## What a failed attempt costs now, and why

A worker session starts with a fixed context before it does any work. On the
sessions this daemon runs, a task session starts at about 85,000 input tokens,
and the MCP tool definitions alone make up about 36,000 of those.

Before attempt cost, every retry and every hop to the next worker on the ladder
spawned a fresh session. A task that failed three times on the strong worker
therefore paid the full starting context three times, about 255,000 tokens, for
work that had already shown it would not succeed.

With attempt cost in place, the same failing task is priced differently:

- **The first attempt of a cheap kind runs on a cheap worker.** A `docs` or
  `note` task that fails has spent one cheap start, not three strong ones.
- **A retry can resume the failed session.** The retry then costs one summary line
  and the continuation, not another starting context
  ([Retrying in the same session](#retrying-in-the-same-session-or-starting-a-new-one)).
- **Nothing else changes.** The attempt order, the failure ladder and the review
  hold work as they did. A first-try success runs on the worker its kind resolves
  to, which is unchanged if you set no first-attempt worker.

The cost is still paid per attempt. Attempts of one task can run on different
workers, and each is charged for the context it carries. To see real figures,
read a few failed attempts in the cost report rather than multiplying the first
attempt by the attempt count.

## The first attempt, by task kind

Every task falls into one of four kinds, decided from what its attempt produces.
The rules are checked in this order, and the first match wins:

| Kind | Meaning |
|---|---|
| `final_gate` | The Automaton's final gate task |
| `note` | A read-only task, or a task with no listed files |
| `docs` | A task whose every listed file is documentation (a `.md` file, or a file under a `docs` directory) |
| `implementation` | Everything else |

`autonomous.first_attempt.<kind>.backend` and `.model` name the worker that
kind's first attempt runs on. The example below sends the two cheap kinds to a
local model, with no hosted worker involved:

```yaml
autonomous:
  first_attempt:
    docs:
      backend: pi-prompt
      model: ollama/<local-model>
    note:
      backend: pi-prompt
      model: ollama/<local-model>
```

How the choice is made:

- **Empty means today's choice.** A kind with no backend, and every kind you did
  not name, resolves exactly as before: task, story, Automaton, then the
  daemon default.
- **An explicit choice always wins.** If a worker or LLM was set explicitly on
  the task, on its story, on the Automaton, or by a burn-guard route, the
  first-attempt pair does not apply. Set the LLM on a task with
  `autonomous_prd_set_task_llm` or on an Automaton with `autonomous_prd_set_llm`.
- **A model is required for some backends.** `pi`, `pi-prompt` and `ollama`
  refuse to start without a model. Set the model before, or with, the backend.
- **The pair is daemon-wide.** There is no per-Automaton or per-task map. For one
  task that needs a specific worker, set the worker on that task, or put a
  ladder step on it ([Failure escalation](failure-escalation.md)).

## Escalating after n failures

`autonomous.first_attempt_escalate_after_failures` says how many failed attempts
it takes before the first-attempt pair stops applying. The default is `1`, so the
first failure escalates. The range is `0` to `20`, and `0` behaves like `1`.

Once the task has failed that many times, the attempt resolves the normal way
again, including the failure-escalation ladder. With the pair on `docs` tasks,
the default escalation count, and a ladder of a local model for two attempts
then a hosted model:

| Attempt | Failed attempts so far | Worker |
|---|---|---|
| 1 | 0 | `pi-prompt` / `ollama/<local-model>` (the first-attempt pair) |
| 2 | 1 | The ladder's step 1, `pi-prompt` / `ollama/<local-model>` |
| 3 | 2 | The ladder's step 2, `claude-code` / `claude-sonnet-5-5` (the paid backend) |
| — | 3 | Held for review |

The ladder is set with `autonomous.failure_escalation.steps`, and its failure
count with `max_failures` (default 3). See
[Failure escalation](failure-escalation.md) for the ladder and the review hold.

<a id="resuming-a-retry"></a>

## Retrying in the same session, or starting a new one

When a task attempt fails verification, the next attempt either resumes the
failed session or starts a new one. The retry resumes only when every check
below passes. The first check that fails sends the attempt to a fresh session,
and the record says which one did it.

1. **The attempt failed verification.** Stalls, start failures and infra failures
   never resume.
2. **The session is still open**, and the task still points at it. A session
   that has ended refuses the resume.
3. **The attempt's backend can resume a session.** Today that is `claude-code`
   only.
4. **`autonomous.retry_resume_max_context_tokens` is above `0`.** The default is
   **`150000`** tokens. `0` means every retry starts a fresh session.
5. **The session's context is at or under that limit.** A context size the daemon
   cannot measure passes this check.

When a retry resumes, nothing new is started. The attempt waits on the same
session, and the worktree is whatever the failed attempt left, so the worker
fixes the work in place. The daemon sends one line into the session: a marker
that this is an Automaton retry resumed with its context, then the failure summary
flattened to one line and cut at 4,000 characters. If the send path is missing or
the send fails, the attempt falls back to a fresh session.

A resumed retry that still fails counts as a failed attempt in every other
respect. The failure count, the ladder, the review hold and the alert are
unchanged. Only the cost of the pass changes.

## Failures that go to review at once

Some failures have no route to a fix through another attempt. When the verifier
rejects an attempt for one of these reasons, the task is held for review at once,
with the reason, and no further attempt is started. This applies while failure
escalation is on.

| Failure | Why another attempt will not help | What to change |
|---|---|---|
| **No files changed**, and the task writes nothing (read-only, or no planned files) | A re-run of a task that writes nothing has no new work to verify. | Give the task files to write, so that a change can be made and checked. |
| **Verification script missing** from the worktree | The same missing script fails the re-run the same way. | Put the verification script in the worktree, or change the spec so that it creates the script. |
| **Same failure summary failed twice** (case and surrounding whitespace ignored) | A third identical failure means the retry is not fixing anything. | Replace the spec, or change the worker, the files or the scope. An unchanged retry re-enters the same hold. |

Each hold raises one error-level alert titled *Automaton task needs review (no
retry)*, which names the reason. The actions are the same five as any review hold
(retry, edit spec and retry, change worker, narrow scope, cancel). For these
holds, use the change that alters the basis: the files, the verification script or
the spec.

## Reading the attempt record

Every failed attempt of a task is kept in the task's attempt list. It is never
removed, and its `number` keeps counting up across operator actions. The fields
that matter for cost and for the next decision are:

| Field | Meaning |
|---|---|
| `number` | The attempt's place in the task's history |
| `backend`, `model`, `effort` | The worker the attempt ran on |
| `backend_reason` | Why that worker: `first attempt for <kind> tasks`, `escalated after <n> failures`, `escalation ladder step`, `operator pinned` or `default choice` |
| `escalated` | `true` when the attempt ran on a ladder step rather than the normal worker |
| `outcome` | `failed`, `stalled` or `cancelled` |
| `failure_category`, `failure_summary` | The failure class and the one-line summary the next attempt is given |
| `verifier_summary`, `verifier_issues`, `changed_files` | What the verifier said and which files the attempt changed |
| `resumed` | `true` when the attempt continued the previous session with the failure summary |
| `resume_reason` | The retry decision: `resumed previous session`, or the refusal, such as `session ended; starting a fresh attempt`, `backend cannot resume; starting a fresh attempt`, `resume disabled by setting; starting a fresh attempt`, `context above the limit; starting a fresh attempt`, `no send-input hook; starting a fresh attempt` or `send into session failed; starting a fresh attempt` |
| `no_retry_reason` | Set only on the attempt that held the task for review without another attempt, with the reason |

The first attempt makes no retry decision, so `resumed` and `resume_reason` are
empty on it. A task held for review shows the same reasons for its last attempt in
its review entry, as `last_resumed`, `last_resume_reason` and `no_retry_reason`.

To read the record:

- **Status output.** `datawatch autonomous status` prints the daemon's status JSON
  (`/api/autonomous/status`). Each `review_needed` row carries `last_backend`,
  `last_backend_reason`, `last_resumed`, `last_resume_reason` and `no_retry_reason`,
  and the fields are omitted when not recorded.
- **The task.** Read the Automaton with `autonomous_prd_get`, or open the task row
  in the Automaton's review panel. The panel shows the attempt list with the
  worker reason and the resumed or fresh session, and a **No retry** line where
  one applies.

## The settings, on every surface

There are four keys. The first is a map, one entry for each of the four kinds.

| Config key | Default | Range | Meaning |
|---|---|---|---|
| `autonomous.first_attempt.<kind>.backend` | empty | a backend name | The first-attempt backend for `implementation`, `docs`, `note` or `final_gate` |
| `autonomous.first_attempt.<kind>.model` | empty | a model name | The model for that backend. Required by `pi`, `pi-prompt` and `ollama` |
| `autonomous.first_attempt_escalate_after_failures` | `1` | whole number, 0 to 20 | Failed attempts before the pair stops applying |
| `autonomous.retry_resume_max_context_tokens` | `150000` | whole number, 0 to 2000000 | Largest prior context a retry may resume. `0` = always start fresh |

Every write goes through one shared check. It is run on the stored values with
your change applied, before anything is saved. A refused write saves nothing, and
the refusal names the key that is wrong. The refusal messages below are the
daemon's own text.

A backend and its model are checked together. Setting a backend that needs a
model fails while no model is stored, so set the model first and then the
backend.

### 1. config.yaml

The file is checked when the daemon loads it. Use the `autonomous` block:

```yaml
autonomous:
  first_attempt:
    docs:
      backend: pi-prompt
      model: ollama/<local-model>
  first_attempt_escalate_after_failures: 1
  retry_resume_max_context_tokens: 150000
```

A bad value stops the configuration from loading, and the error names the key,
for example:

```
autonomous.retry_resume_max_context_tokens: 2500000 is invalid; use a whole number from 0 to 2000000 (0 = always start a fresh session)
```

### 2. Web Settings

Open **Settings → Automata**, then the **Autonomous** section. The attempt-cost
fields are:

- *First attempt backend* and *First attempt model*, one pair for each of
  implementation, docs, note and final gate tasks
- *Failed attempts before the strong worker takes over* (0 to 20, default 1)
- *Resume context limit for retries, in tokens* (0 to 2000000, default 150000;
  0 = always start fresh)

Each field saves when you leave it. A refused value shows its message in a red
line under that field, and the value stays in the box for correction. Nothing is
saved on a refusal.

### 3. REST: GET and PUT /api/config

`GET` returns the three values in the `autonomous` block, with `first_attempt` as
a map of kind to `{backend, model}`:

```sh
curl -sk https://localhost:8443/api/config
```

`PUT` takes JSON. A dotted key changes one field and keeps the others. A
`first_attempt` object replaces the whole map. Send the dotted form when you
change one kind:

```sh
curl -sk -X PUT -H "Content-Type: application/json" \
  -d '{"autonomous.first_attempt.docs.model":"ollama/<local-model>"}' \
  https://localhost:8443/api/config

curl -sk -X PUT -H "Content-Type: application/json" \
  -d '{"autonomous.first_attempt.docs.backend":"pi-prompt"}' \
  https://localhost:8443/api/config
```

A refused write returns HTTP `400` with the message as plain text, and nothing in
the request is applied. Setting the backend without a stored model returns:

```
autonomous.first_attempt.docs.model: backend "pi-prompt" needs a model; set a model or clear the backend
```

### 4. Comm channel: configure

```
configure autonomous.first_attempt.docs.model=ollama/<local-model>
configure autonomous.first_attempt.docs.backend=pi-prompt
configure autonomous.first_attempt_escalate_after_failures=2
configure autonomous.retry_resume_max_context_tokens=0
```

A successful write replies `[<host>] Set <key> = <value>. Restart may be required.`
A refused one replies
`Refused:` followed by the same validator message, and nothing is written:

```
[<host>] Refused: autonomous.first_attempt_escalate_after_failures: 25 is invalid; use a whole number from 0 to 20
```

### 5. MCP: autonomous_config_get and autonomous_config_set

`autonomous_config_get` reads the three keys back. `autonomous_config_set` takes
them as arguments. Send the whole `first_attempt` object when you set it:

```json
{
  "first_attempt": {
    "docs": {"backend": "pi-prompt", "model": "ollama/<local-model>"},
    "note": {"backend": "pi-prompt", "model": "ollama/<local-model>"}
  },
  "first_attempt_escalate_after_failures": 1,
  "retry_resume_max_context_tokens": 150000
}
```

A refused call returns a tool error with the validator's message, and nothing is
written:

```
autonomous.retry_resume_max_context_tokens: -1 is invalid; use a whole number from 0 to 2000000 (0 = always start a fresh session)
```

Read the result back with `autonomous_config_get` to confirm the change.

### 6. datawatch config set

```sh
datawatch config set autonomous.first_attempt.docs.model ollama/<local-model>
datawatch config set autonomous.first_attempt.docs.backend pi-prompt
datawatch config set autonomous.first_attempt_escalate_after_failures 2
datawatch config set autonomous.retry_resume_max_context_tokens 0
```

A successful write prints `Set <key> = <value>`. A refused write exits with an
error that starts `config set:` and carries the daemon's message, and the stored
value does not change:

```
config set: autonomous.first_attempt_escalate_after_failures: 25 is invalid; use a whole number from 0 to 20
```

## See also

- [Failure escalation](failure-escalation.md): the worker ladder, the failure count,
  the review hold, the five operator actions and the override order
- [Retry policy](retry-policy.md): the failure categories and the retry budgets
  that apply before and after this policy
- [../api/autonomous.md](../api/autonomous.md): REST reference for the autonomous
  config
