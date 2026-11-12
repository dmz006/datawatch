---
docs:
  index: true
  topics: [autonomous, operator-questions, decisions, gate, shadow-mode]
exec_params: []
exec_steps:
  - tool: get_config
    description: Read the question channel switch and the two decision gate keys (decision_gate is on when the key is absent, decision_gate_mode reads shadow when empty)
    args: {section: "autonomous"}
    read_only: true
  - tool: autonomous_decisions
    description: List the operator decisions, newest first, with their answers and the tasks held on them
    args: {state: "open"}
    read_only: true
---
# How-to: operator decisions on an Automaton, and the gate that waits for them

Some choices on an Automaton are yours to make: which design to use, whether
to go ahead with a risky change, what a name should be. An **operator
decision** is one of those choices written down as a structured question.
It has an **id** (`q-` followed by a long run of lowercase letters and
digits), the question text, optional numbered options, and a state: `open`,
`answered`, `withdrawn` or `expired`. Your answer is stored with your own
words quoted exactly as you wrote them.

The **decision gate** is the part that makes a decision matter. When the
spec of an Automaton task names a decision id, the task does not start
until that decision is answered. While it waits it shows the status
**Waiting for a decision** and a one-sentence reason. The moment you answer,
the task starts by itself.

This guide covers raising a decision, reading it on every surface, how the
gate holds a task, what the waiting reason says, the two config keys, and
shadow mode, which is how the gate ships.

## Base requirements

- The question channel on. Decisions are raised and answered through it, and
  it is off until `autonomous.questions.enabled` is `true`. See
  [getting notified when an Automaton asks you a question](operator-questions-notifications.md)
  for the channel itself.
- A running Automaton engine (`autonomous.enabled`).
- Nothing else. The gate needs no extra service.

Turn the channel on if it is not:

```bash
curl -sk -X PUT -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"autonomous.questions.enabled": true}' https://localhost:8443/api/config
```

Keep the channel on for as long as the gate is on. The gate only sees
decisions that were raised through the channel.

## What an operator decision is

A decision is a question with a purpose: other work waits on its answer.

| Part | What it is |
|---|---|
| **Id** | `q-` and at least 20 lowercase hex characters, for example `q-3f9a1c7e5b2d4a60c8e1`. This is what a task spec names. |
| **Question and label** | The text you are asked, and a short label such as `decision` or `approval`. |
| **State** | `open` until someone answers it; then `answered`. It can also be `withdrawn` by whoever raised it, or `expired`. |
| **Answer** | Your reply: the id of the answer, your words exactly as you wrote them (the quote), who answered and from which surface. A later answer replaces an earlier one without erasing it. |
| **Tasks** | The tasks of the Automaton whose spec names the decision, and the ones held on it right now. |

## How a decision is raised

A decision is raised on the question channel, with the Automaton it belongs
to in the `prd` context so the decision and the Automaton are linked:

```bash
curl -sk -X POST "$BASE/api/questions" -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{
    "label": "decision",
    "context": "prd",
    "prd_id": "d0442737d38c",
    "text": "Use the existing session table or add a new one?",
    "options": [
      {"id": "reuse", "label": "reuse the table", "tradeoffs": "no migration, mixes two kinds of row"},
      {"id": "new",   "label": "add a new table", "tradeoffs": "a migration, clean separation"}
    ],
    "default": "hold",
    "recommendation": {"option_id": "new", "rationale": "the two kinds of row have different lifetimes"},
    "asked_by": "operator",
    "asker_kind": "operator"
  }'
```

The reply carries the new id. An Automaton, one of its sessions or you can
raise one; the full list of fields is in the
[question channel reference](../api/autonomous.md#operator-question-channel).
Raising never blocks the asker.

To make a task wait for it, **write the id into the task's spec**, anywhere
in the text:

```
Add the sessions table migration. This follows operator decision
q-3f9a1c7e5b2d4a60c8e1 on which table to use.
```

The id has to stand alone as a word: `q-` then 20 or more lowercase hex
characters, with no letter or digit stuck to either end. A longer hex run is
read whole. One spec can name more than one decision, and the task waits
until every one of them is answered.

Answer it the way you answer any question: on the PWA, with
`POST /api/questions/{id}/answer`, or from chat. A quick REST answer:

```bash
curl -sk -X POST "$BASE/api/questions/q-3f9a1c7e5b2d4a60c8e1/answer" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"chosen_option_id": "new", "quote": "Add a new table.\nKeep the old one read-only.", "actor": "operator", "surface": "cli"}'
```

## Reading the decision line

The **decision line** is the read of every decision, newest first: id, state,
question, who asked and when, the Automaton, the tasks that name it, the tasks
held on it and the current answer. Every surface shows the same data, and
none of them writes anything.

| Surface | How to read it |
|---|---|
| **REST** | `GET /api/autonomous/decisions`, optionally with `?prd_id=<automaton-id>`, `?state=open`, `?limit=20`. Reply: `{"decisions": [...]}`. |
| **MCP** | The `autonomous_decisions` tool, with the same three optional arguments `prd_id`, `state` and `limit`. |
| **CLI** | `datawatch autonomous decisions [automaton-id] [--state open] [--limit 20] [--json]`. |
| **Comm channel** | `autonomous decisions [automaton-id]`. The reply is the newest ten, one per line. |
| **Web UI** | Open an Automaton from **Automata**; the **Decisions** card on its detail page lists that Automaton's decisions. |

REST example:

```bash
curl -sk -H "Authorization: Bearer $TOKEN" \
  "$BASE/api/autonomous/decisions?prd_id=d0442737d38c&state=open"
```

```json
{
  "decisions": [
    {
      "id": "q-3f9a1c7e5b2d4a60c8e1",
      "question": "Use the existing session table or add a new one?",
      "label": "decision",
      "state": "open",
      "asked_by": "operator",
      "asked_at": "2026-10-29T12:00:00Z",
      "prd_id": "d0442737d38c",
      "task_ids": ["task-4"],
      "waiting_task_ids": ["task-4"],
      "waiting_tasks": [
        {"task_id": "task-4",
         "wait_reason": "operator decision q-3f9a1c7e5b2d4a60c8e1 is waiting for an answer: Use the existing session table or add a new one?"}
      ],
      "answer": null
    }
  ]
}
```

Once the decision is answered, `answer` is no longer `null`:

```json
"answer": {
  "id": "a-0000019c2d4e5f6a1b2c",
  "quote": "Add a new table.\nKeep the old one read-only.",
  "quote_sha256": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
  "actor": "operator",
  "surface": "cli",
  "answered_at": "2026-10-29T12:05:00Z"
}
```

The `quote` is exactly what the operator wrote, line breaks and capitals
included. No surface trims or rewords it.

What each surface prints:

- **CLI and chat** show one line per decision: the id, the state, the
  question cut to 120 characters, and for an answered decision
  `| answer <answer id> by <actor>: "<quote>"`. A task held on the decision
  is added as `| waiting: task <id> (<reason>)`. An empty list reads
  `No decisions to show.`
- **Web UI** shows one entry per decision with a state chip (open in the
  warning colour, answered in the success colour, withdrawn and expired in a
  neutral one), the question, and for an answered decision the answer id,
  who answered and when, with the quote in full and its line breaks kept.
  A decision that holds a task shows the task's reason. A page with no
  decisions says **No decisions yet**.

A withdrawn or expired decision still shows, with that state. A filter that
matches nothing returns an empty list, not an error.

## How the gate holds a task

Before a pending or queued task starts, the daemon looks at its spec for
decision ids and checks each one:

| The decision is | The gate |
|---|---|
| `answered` | lets the task through |
| `open` | holds the task |
| `withdrawn` or `expired` | holds the task, because it has no answer |
| not found | holds the task, because the id names nothing |

A held task has the status **Waiting for a decision** (`waiting_decision`).
It has not started; it is not a failure and it is not a retry. Tasks that
depend on it wait behind it, and the Automaton keeps running and waits for
the answer instead of finishing. The daemon sends **one** warning alert when
the task first goes on hold, not one per check.

When the decision is answered, the task returns to pending on its own, the
reason clears, and it starts. You do not run anything or resume the
Automaton. If you turn the gate off or set it to shadow while a task is held,
the next check sends the task back to pending as well. A decision that was
withdrawn or expired keeps its task held until the spec is changed to name an
answered decision, or until you turn the gate off.

### What the waiting reason looks like

The reason is one plain sentence on the task. For an open decision it names
the id and quotes the first 160 characters of the question:

```
operator decision q-3f9a1c7e5b2d4a60c8e1 is waiting for an answer: Use the existing session table or add a new one?
```

For the other cases:

```
operator decision q-3f9a1c7e5b2d4a60c8e1 was withdrawn without an answer
operator decision q-3f9a1c7e5b2d4a60c8e1 was expired without an answer
operator decision q-3f9a1c7e5b2d4a60c8e1 was not found
```

When a spec names several decisions that are not answered, the sentences are
joined with `; `. The reason shows under the task in the web UI beside the
question-mark glyph, on `GET /api/autonomous/prds/{id}` as `wait_reason`, and
next to the task on the decision line.

## Shadow mode against on

The gate has two keys, both under `autonomous.questions`.

| Key | Default | Meaning |
|---|---|---|
| `autonomous.questions.decision_gate` | on when the key is absent | Whether the gate looks at task specs at all. Off means no task is ever held or checked. |
| `autonomous.questions.decision_gate_mode` | `shadow` when empty | `shadow` works out what it would hold and holds nothing. `on` holds the task. Any other value is refused. |

**The gate ships in shadow mode.** With `decision_gate` on and
`decision_gate_mode` at `shadow`, no task is ever held: the task runs. For
each task the gate would have held, the Automaton's decision log records one
entry of kind `decision_gate_shadow` with a note such as
`would hold: operator decision q-... is waiting for an answer: ... (task task-4)`.
The same task with the same reason is recorded once, not on every check. So
you can watch what the gate would do on your real Automata before it stops
anything.

To read what shadow mode recorded, open the Automaton and look at its
decision log (**Decisions** tab on the detail page), or read the
`decisions` array on `GET /api/autonomous/prds/{id}` and look for
`"kind": "decision_gate_shadow"`.

When the entries look right, switch the mode to `on`:

| Method | How |
|---|---|
| **YAML** | `~/.datawatch/config.yaml` → `autonomous:` → `questions:` → `decision_gate: true` and `decision_gate_mode: on` |
| **CLI** | `datawatch config set autonomous.questions.decision_gate_mode on` |
| **Web UI** | Settings → **Autonomous Automata planning** → *Decision gate mode (shadow only records, on holds the task, default shadow)* |
| **REST API** | `PUT /api/config` with the body below |
| **Comm channel** | `configure autonomous.questions.decision_gate_mode=on` |
| **MCP** | `config_set` with key `autonomous.questions.decision_gate_mode` and value `on` |

Exact `PUT /api/config` bodies:

```bash
# Hold tasks on unanswered decisions (the gate is already enabled by default)
curl -sk -X PUT -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"autonomous.questions.decision_gate_mode": "on"}' https://localhost:8443/api/config

# Go back to watching only
curl -sk -X PUT -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"autonomous.questions.decision_gate_mode": "shadow"}' https://localhost:8443/api/config

# Turn the gate off completely
curl -sk -X PUT -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"autonomous.questions.decision_gate": false}' https://localhost:8443/api/config

# Turn it back on
curl -sk -X PUT -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"autonomous.questions.decision_gate": true}' https://localhost:8443/api/config
```

A mode other than `shadow` or `on` is refused with a `400`, and nothing
changes. Both keys are read at the moment a task is about to start, so a
change takes effect at once with no restart. Setting the gate to `false`, or
the mode back to `shadow`, sends any task the gate is holding back to pending
on the next check.

The web UI shows both as controls on the Automata settings card, next to the
other question settings: a checkbox for `decision_gate` and a shadow/on
choice for `decision_gate_mode`.

## Where to see it

| Location | What you see |
|---|---|
| **Web UI** | The **Decisions** card on an Automaton's detail page; the **Waiting for a decision** label and reason under a held task; the alert in the alert list |
| **REST API** | `GET /api/autonomous/decisions`; `status` and `wait_reason` on tasks in `GET /api/autonomous/prds/{id}`; `decision_gate_shadow` entries in the same Automaton's `decisions` log |
| **MCP** | `autonomous_decisions` |
| **CLI** | `datawatch autonomous decisions` |
| **Comm channel** | `autonomous decisions [automaton-id]` |

## Troubleshooting

- **A task names a decision but runs anyway.** Check the mode. In `shadow`
  (the default) nothing is held; look for a `decision_gate_shadow` entry on
  the Automaton's log. Also check that `autonomous.questions.decision_gate`
  has not been set to `false`.
- **A task is held on a decision you never raised.** The id in the spec may
  have a typo. The reason says `was not found`. Fix the id in the spec so it names
  a decision that exists.
- **A task stays held after you answered.** Answer the decision named in the
  reason; one task can wait on several. The held task starts on the next
  check, and an answer wakes the Automaton at once.
- **The decision line is empty.** Nothing has been raised, the filter is
  too narrow, or the question channel was never turned on. A `503` means
  the Automaton engine is not running or the question store is not
  available.

## See also

- [Getting notified when an Automaton asks you a question](operator-questions-notifications.md)
- [Operator question channel reference](../api/autonomous.md#operator-question-channel)
- [Decision line reference](../api/autonomous.md#operator-decisions-get-apiautonomousdecisions)
- [Operations: decision gate](../operations.md#decision-gate)
