---
docs:
  index: true
  topics: [autonomous, operator-questions, worker-marker, blocking, decisions, pwa, cli, mcp, comm, rest]
exec_params: []
exec_steps:
  - tool: get_config
    description: Read the question channel switch and the worker marker switch (the channel is off and the marker switch is off when the key is absent)
    args: {section: "autonomous"}
    read_only: true
  - tool: question_list
    description: List the open questions - the queue a worker's question lands in (the same read the coordinator tick makes)
    args: {state: open}
    read_only: true
---
# How-to: a worker asks you a question, and the task waits for your answer

A worker session of an Automaton can ask you a design question while it
works. It does that by printing **one line** in its output, the *worker
question marker*. The daemon notices the line, stores it as an ordinary
operator question with a link to the task, announces it the way every other
question is announced, and — when the worker says it cannot go on without the
answer — **parks the task** on it. The task gives back its capacity slot and
waits. When you answer, a later pass of the coordinator tick resumes the task
and hands the worker your decision on one line.

Nothing here changes how you answer a question. It only adds a new way for a
question to arrive, and a way for a task to wait for it.

> **Two switches, both off by default.** The marker is acted on only when
> `autonomous.questions.enabled` **and** `autonomous.questions.worker_marker`
> are both `true`. With either one off, the marker line is ignored like any
> other output. Both are read live: no restart.

## Base requirements

- A running Automaton engine (`autonomous.enabled`).
- The operator question channel on (`autonomous.questions.enabled: true`). See
  [getting notified when an Automaton asks you a question](operator-questions-notifications.md)
  for the channel and its announcements.
- Worker sessions that run in a terminal (the interactive backends). See
  "What this step does not cover yet" below.

## Setup: turn on the channel and the worker marker

| Key | Default (key absent) | Controls |
|---|---|---|
| `autonomous.questions.enabled` | **off** | the whole question channel |
| `autonomous.questions.worker_marker` | **off** | whether a question marker in a worker's output is raised as a question. It does nothing while the channel is off |

YAML, under the `autonomous:` section of the daemon config:

```yaml
autonomous:
  questions:
    enabled: true
    worker_marker: true
```

| Surface | How to turn the worker marker on |
|---|---|
| YAML | as above |
| REST | `curl -sk -X PUT -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" -d '{"autonomous.questions.worker_marker": true}' https://localhost:8443/api/config` |
| MCP | `config_set(key="autonomous.questions.worker_marker", value=true)`; read it back with `get_config(section="autonomous")` |
| CLI | `datawatch config set autonomous.questions.worker_marker true` |
| Comm channel | `configure autonomous.questions.worker_marker=true` |
| PWA | **Settings → Autonomous Automata planning** → *Let a worker raise a question with a marker in its output (off by default)* |

Every worker session carries a short instruction describing the marker, so a
worker knows it exists. The daemon acts on a marker line only while the switch
is on.

## The marker line

The worker prints the text `DATAWATCH_QUESTION:` followed by **one line of
JSON**, all on a single line:

```
DATAWATCH_QUESTION: {"question":"Cap the retry backoff at five minutes, or leave it unbounded?","options":[{"id":"cap","label":"Cap at five minutes","tradeoffs":"bounded latency, may drop slow jobs"},{"id":"none","label":"Leave it unbounded"}],"recommendation":{"option_id":"cap","rationale":"unbounded backoff wedged a worker before"},"default":"hold","blocked":true}
```

The line has to start with the marker (a leading terminal glyph or colour code
is fine). A marker in the middle of a sentence, or followed by a placeholder
such as `<json>`, is not a question and is dropped silently.

| Field | Required | Rule |
|---|---|---|
| `question` | yes | The decision in plain words. Up to 8192 bytes; not blank. |
| `options` | yes | 2 to 20 options, each `{id, label, tradeoffs}`. `id` and `label` must be non-empty; ids are unique; `label` up to 200 bytes; `tradeoffs` (optional) up to 1000 bytes. |
| `recommendation` | no | `{option_id, rationale}`. `option_id` must be one of the options; `rationale` up to 1000 bytes. |
| `default` | no | An option id, or `hold`. Absent means `hold`: nothing is chosen for you. |
| `blocked` | no | `true` when the task cannot go on without the answer. Absent means `false`. |
| `label` | no | `decision` (the default), `design review`, `approval` or `information`. |
| `risk` | no | `normal` (the default) or `low`. |

The whole line is capped at 16384 bytes, and a key that is not in this table
makes the line malformed.

A line that fails any rule is **refused**: no question is stored and nothing is
announced. The refusal is written to the audit log as `question_marker_refused`
with the session id, a reason (`malformed`, `too_long`, `empty_question`,
`too_few_options`, `duplicate_option`, `invalid_label`, `invalid_risk`,
`invalid_default` or `invalid_recommendation`) and the SHA-256 of the line —
never the line text. The daemon log carries the same three facts once.

### What the stored question looks like

The question goes through exactly the path a `POST /api/questions` raise takes,
so it has the same audit entry, decision-log entry and announcements (the
WebSocket message, the messaging line, and the alert for a high-priority one).
Its fields:

- `asked_by` is `worker-` plus the short session id; `asker_kind` is
  `session`; `session_id` is the worker's session.
- It is linked to the task (`context: task`). If the session has no task but
  has an Automaton, it links to the Automaton (`context: prd`); with neither,
  the context is `none`.
- `priority` is `normal` and `return_path` is `false`.
- `source` is `marker`, and `source_line_sha256` is the hash of the line, so the
  same line read twice raises **one** question while the first is still open.
- `blocking` is the line's `blocked` value.

## When a worker should block, and when it should continue

| The worker... | Sets | What happens |
|---|---|---|
| cannot make progress until you decide (it would be guessing, or it would redo work) | `"blocked": true` | The task is **parked** on the question until it is decided. |
| can finish the rest of the task and the choice only shapes a follow-up | `"blocked": false` (or leave it out) | The question waits in your queue. The task keeps running and nothing is parked. |

A worker should ask only about a choice that the task and the code cannot
settle and that only you can make. For a plain request for input that is not a
design choice, it keeps using the needs-input line.

A non-blocking question's answer is stored and readable like any other, but it
is not sent back into the worker's session: the worker did not stop for it.

## How a blocked task waits

When a task has an open blocking question, the daemon:

1. Shows the task as **waiting** with the wait reason
   `await_decision: <question id>` (cause `await_decision`).
2. Gives back the task's capacity slot, so a waiting task does not hold a lane.
3. Releases the project-tree lock, unless the tree has uncommitted changes or
   commits that are not pushed.
4. Counts **no retry** and records **no failure**. The wait is not a fault.
5. Leaves the worker session running and suppresses the stall detectors for it
   while the question is open: the worker is waiting on you by design.

It parks the task once per question.

## How the coordinator tick reads and resumes

The task does not resume itself. The coordinator tick does, in a pass of its
own:

1. It reads the **answered** and **withdrawn** questions through the same
   question read that `GET /api/questions` serves, with a `state` filter. It
   makes no HTTP call and never reads a terminal.
2. For each parked task whose question has been decided it starts one resume,
   in its own goroutine and guarded so a task is never resumed twice at once.
   An open question leaves its task parked.
3. A resume re-admits the task through the capacity ledger (it may queue behind
   other work), re-claims the project-tree lock if the park released it, and
   then sends the worker **one line**:

```
[automaton decision] question=q-… option="cap" label="Cap at five minutes" quote="Cap it - unbounded wedged prod" answer=a-… actor="operator" — the operator has decided; continue the task from where you left off using this decision.
```

The quote and label are JSON-encoded, so a line break in your words cannot
break the line. If the question was **withdrawn**, the line says so: no default
was applied and no decision was made, the worker continues only with work that
does not depend on it, and raises a new question if it is still blocked.

The task records which answer it resumed for. If you later *supersede* your
answer, that becomes the new current answer on the question, and the task is not
resumed a second time.

## Answering from each surface

You answer a worker's question exactly as you answer any question. The
Automaton's decision log also gets a line for it, and in the PWA the answer id
on that line is a link that opens a small panel with your quote, the chosen
option, who answered and when.

| Surface | How |
|---|---|
| PWA | **Automation** view → *Operator questions* card → **Answer** on the row → pick an option or type your words → **Answer**. On the Automaton's Decisions tab, the answer id on an answer entry opens the quote panel |
| CLI | Read only: `datawatch autonomous decisions --state open` lists the open questions with their ids. There is no CLI verb to answer; answer from the PWA, REST or MCP |
| REST | `POST /api/questions/{id}/answer` (example below) |
| MCP | `question_list` with `state: open`, then `question_answer` with `id`, `chosen_option_id` and/or `quote` |
| Comm channel | Read only: `autonomous decisions` shows the newest decisions, open questions included. The announcement names the question id. There is no chat verb to answer; answer from the PWA, REST or MCP |

An answer needs an option, your own words, or both. Your words are stored
exactly as written, and an answer that looks like a credential is refused.

```bash
# See what is waiting
curl -sk -H "Authorization: Bearer $TOKEN" \
  "https://localhost:8443/api/questions?state=open"

# Answer one
curl -sk -X POST -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  "https://localhost:8443/api/questions/q-0123456789abcdef0123/answer" \
  -d '{"chosen_option_id": "cap", "quote": "Cap it - unbounded wedged prod"}'
```

The question JSON gains three fields for this step: `blocking`, `source`
(`marker` or `rest`) and `source_line_sha256`. They are left out when empty, so
an older record reads as before.

## Walkthrough

1. Turn both switches on (see Setup).
2. Start an Automaton. A worker reaches a choice it cannot settle and prints the
   marker line with `"blocked": true`.
3. Read the queue. `GET /api/questions?state=open` shows a question asked by
   `worker-<id>` with `blocking: true`, `source: "marker"` and the task id.
4. Read the task on `GET /api/autonomous/prds/{id}`: it is waiting with the
   reason `await_decision: q-…`, and its lane is free for other work.
5. Answer from any surface above.
6. On its next pass the coordinator tick resumes the task and the worker gets
   the `[automaton decision]` line. The task continues; the decision log shows
   the question, your answer and the link to it.

If the worker's line was refused, no question appears. Look for
`question_marker_refused` in the audit log (`GET /api/audit?action=question_marker_refused`)
and compare the line hash with the worker's output.

## What this step does not cover yet

- **One-shot print-mode backends are not asked on their behalf.** A worker that
  runs as a single non-interactive print-mode call prints its result and exits;
  it cannot be paused and then prompted with your decision. This step adds no
  handling for those backends. Such a run can still raise a question itself with
  `POST /api/questions` (see the notifications guide), or ask through the
  needs-input line.
- A non-blocking question's answer is not sent into the worker's session.
- The marker is read from the worker's own output. It is not a way for the
  daemon to ask a worker anything.

## See also

- [Getting notified when an Automaton asks you a question](operator-questions-notifications.md)
- [Operator decisions on an Automaton](automaton-operator-decisions.md)
- API reference: [Operator question channel](../api/autonomous.md#operator-question-channel)
- Operator guide: [Operator questions](../operations.md#operator-questions)
