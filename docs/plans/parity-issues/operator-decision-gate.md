# [sync] Automaton operator decision line and design-point gate

Parity issue for dmz006/datawatch-app. It covers Android and iPhone/iOS together, so no platform tag is used. The text between the two body markers below is the issue body; the sections after the end marker record the filed issue.

<!-- body-start -->
## Daemon version

Lands in the next minor release above main at the time the change merges. The mobile apps should gate the feature on the daemon reporting that release or later.

## What changed in datawatch

An Automaton task whose spec names an operator decision id (a token of the form `q-` followed by at least 20 lowercase hex characters) now waits until that decision is answered. The daemon exposes one read of all operator decisions, a new task status for a task held on one, and two config keys. The PWA already renders all of this; the apps need to match it.

## REST contract

### New read: `GET /api/autonomous/decisions`

Read-only, newest first. Requires the `autonomous:read` federation capability (a peer without it gets 403). Returns 503 when the Automaton engine or the question store is not available.

| Parameter | Type | Meaning |
|---|---|---|
| `prd_id` | string, optional | Only the decisions linked to this Automaton. |
| `state` | string, optional | One of `open`, `answered`, `withdrawn`, `expired`. Any other value is a 400. |
| `limit` | integer, optional | Positive whole number; default 100, maximum 1000. Zero, negative or non-numeric is a 400. |

A filter that matches nothing returns an empty array, never a 404.

Response:

```json
{
  "decisions": [
    {
      "id": "q-<hex>",
      "question": "...",
      "label": "...",
      "state": "open",
      "asked_by": "...",
      "asked_at": "2026-10-29T12:00:00Z",
      "prd_id": "...",
      "task_ids": ["..."],
      "waiting_task_ids": ["..."],
      "waiting_tasks": [ { "task_id": "...", "wait_reason": "..." } ],
      "answer": null
    }
  ]
}
```

| Field | Notes |
|---|---|
| `id` | Decision id. |
| `question` | The question text. |
| `label` | Short label; may be empty. |
| `state` | `open`, `answered`, `withdrawn` or `expired`. |
| `asked_by`, `asked_at` | Who raised it and when. |
| `prd_id` | The Automaton the decision belongs to; may be empty. |
| `task_ids` | Tasks of that Automaton whose spec names the decision. Always an array. |
| `waiting_task_ids` | The subset of those tasks currently held on it. Always an array. |
| `waiting_tasks` | The same held tasks with their `wait_reason`. Always an array. |
| `answer` | `null` until answered; then the current answer: `id`, `quote`, `quote_sha256`, `actor`, `surface`, `answered_at`. |

`answer.quote` is the operator's words exactly as stored. Do not trim, cut, re-wrap or re-case it.

The same data is on MCP as the `autonomous_decisions` tool, on the CLI, and on the comm channel; the apps only need the REST read.

### Decision log fields

Each entry on the Automaton decision log (`GET /api/autonomous/prds/{id}/decisions`) can now carry three optional fields that link it to an operator decision:

| Field | Notes |
|---|---|
| `question_id` | The decision the entry records. |
| `answer_id` | The answer the entry records (answer entries only). |
| `decision_state` | The decision state the entry stands for: `open`, `answered`, `withdrawn` or `expired`. |

New entry kinds: `operator_decision`, `operator_answer`, `decision_gate_hold` and `decision_gate_shadow`. The fields are omitted when empty. A client that shows the log should render an unknown kind as plain text rather than failing.

### Task status `waiting_decision`

A task held on an unanswered decision has `status` set to `waiting_decision`, and its `wait_reason` is a plain sentence naming the decision and, for an open one, the first 160 characters of the question (for example "operator decision q-... is waiting for an answer: ..."). When the decision is answered the task returns to `pending` on its own and `wait_reason` clears. `wait_reason` is the same field already used for `waiting_capacity`. Treat `waiting_decision` as an active, non-terminal status. The PWA shows a question-mark glyph, the label "Waiting for a decision", and the wait reason under the task.

### Config keys

Both sit under `autonomous.questions` in `GET`/`PUT /api/config`.

| Key | Type | Default | Meaning |
|---|---|---|---|
| `autonomous.questions.decision_gate` | boolean | on when the key is absent | Whether a task that names an unanswered decision is held. |
| `autonomous.questions.decision_gate_mode` | string | `shadow` when empty | `shadow` records what would be held and holds nothing; `on` holds the task. Any other value is refused by `PUT /api/config`. |

Locale keys the PWA added (English values; the other locales carry placeholders): `prd_task_waiting_decision`, `decision_line_title`, `decision_line_empty`, `decision_state_open`, `decision_state_answered`, `decision_state_withdrawn`, `decision_state_expired`, `decision_answer_id`, `decision_answer_actor`, `settings_automata_questions_decision_gate`, `settings_automata_questions_decision_gate_mode`.

## Decision line the apps should render

On the Automaton detail screen, a read-only card titled "Decisions" fed by `GET /api/autonomous/decisions?prd_id=<id>`. One row per decision, in the order returned:

1. The decision id and a state chip (open, answered, withdrawn or expired). Open uses a warning colour, answered a success colour, the rest a neutral colour.
2. The question text.
3. For an answered decision: "Answer <answer id>", "by <actor>", the answered time, and the quote verbatim in a block quote, preserving line breaks.
4. For each task held on the decision: the task id and its wait reason.

An empty list shows "No decisions yet". A failed fetch leaves the card empty with no error block; the card is a convenience read. Ignore a response that arrives after the screen moved to another Automaton. Every value must be escaped or treated as plain text, and long ids and questions must wrap at phone width.

Task rows with status `waiting_decision` show the "Waiting for a decision" label and the task's `wait_reason`.

Settings: add the two config keys to the Automata settings card, next to the other operator question toggles.

## Acceptance criteria

### Android

- [ ] The Automaton detail shows the Decisions card from `GET /api/autonomous/decisions?prd_id=<id>` with one row per decision as described above.
- [ ] State chip, question, answer id, actor, answered time and the verbatim quote render for an answered decision; the quote is not trimmed or shortened.
- [ ] A task with status `waiting_decision` renders with its own label, glyph and `wait_reason`, and counts as active, not finished.
- [ ] An unknown task status or decision-log kind does not crash the screen.
- [ ] The Automata settings card has a toggle for `autonomous.questions.decision_gate` (absent reads as on) and a shadow/on choice for `autonomous.questions.decision_gate_mode` (empty reads as shadow), saved through `PUT /api/config`.
- [ ] A 400 on save for an invalid mode shows the server message.
- [ ] The new locale keys exist in all five locales.
- [ ] Layout holds at 360 dp width with a 20+ character id and a long question.

### iPhone/iOS

- [ ] Same list as Android, checked at the narrowest supported iPhone width.
- [ ] Dynamic Type at the largest size keeps the chip, id and quote readable without horizontal scroll.
- [ ] VoiceOver reads each row as state, question, then answer.

## Ships in shadow mode

The gate ships with `decision_gate` on and `decision_gate_mode` set to `shadow`. In shadow mode no task is ever held: the task runs, and the Automaton decision log records once, as a `decision_gate_shadow` entry, what would have been held. So an app will not see `waiting_decision` on any task until an operator switches the mode to `on`. Build and test the status handling against a daemon set to `on`, and show the mode setting so an operator can see why nothing is being held.
<!-- body-end -->

## Filed issue

Issue number: 302

URL: https://github.com/dmz006/datawatch-app/issues/302

Title: [sync] Automaton operator decision line and design-point gate

## Filed body text

The exact text filed as the issue body:

~~~~markdown
## Daemon version

Lands in the next minor release above main at the time the change merges. The mobile apps should gate the feature on the daemon reporting that release or later.

## What changed in datawatch

An Automaton task whose spec names an operator decision id (a token of the form `q-` followed by at least 20 lowercase hex characters) now waits until that decision is answered. The daemon exposes one read of all operator decisions, a new task status for a task held on one, and two config keys. The PWA already renders all of this; the apps need to match it.

## REST contract

### New read: `GET /api/autonomous/decisions`

Read-only, newest first. Requires the `autonomous:read` federation capability (a peer without it gets 403). Returns 503 when the Automaton engine or the question store is not available.

| Parameter | Type | Meaning |
|---|---|---|
| `prd_id` | string, optional | Only the decisions linked to this Automaton. |
| `state` | string, optional | One of `open`, `answered`, `withdrawn`, `expired`. Any other value is a 400. |
| `limit` | integer, optional | Positive whole number; default 100, maximum 1000. Zero, negative or non-numeric is a 400. |

A filter that matches nothing returns an empty array, never a 404.

Response:

```json
{
  "decisions": [
    {
      "id": "q-<hex>",
      "question": "...",
      "label": "...",
      "state": "open",
      "asked_by": "...",
      "asked_at": "2026-10-29T12:00:00Z",
      "prd_id": "...",
      "task_ids": ["..."],
      "waiting_task_ids": ["..."],
      "waiting_tasks": [ { "task_id": "...", "wait_reason": "..." } ],
      "answer": null
    }
  ]
}
```

| Field | Notes |
|---|---|
| `id` | Decision id. |
| `question` | The question text. |
| `label` | Short label; may be empty. |
| `state` | `open`, `answered`, `withdrawn` or `expired`. |
| `asked_by`, `asked_at` | Who raised it and when. |
| `prd_id` | The Automaton the decision belongs to; may be empty. |
| `task_ids` | Tasks of that Automaton whose spec names the decision. Always an array. |
| `waiting_task_ids` | The subset of those tasks currently held on it. Always an array. |
| `waiting_tasks` | The same held tasks with their `wait_reason`. Always an array. |
| `answer` | `null` until answered; then the current answer: `id`, `quote`, `quote_sha256`, `actor`, `surface`, `answered_at`. |

`answer.quote` is the operator's words exactly as stored. Do not trim, cut, re-wrap or re-case it.

The same data is on MCP as the `autonomous_decisions` tool, on the CLI, and on the comm channel; the apps only need the REST read.

### Decision log fields

Each entry on the Automaton decision log (`GET /api/autonomous/prds/{id}/decisions`) can now carry three optional fields that link it to an operator decision:

| Field | Notes |
|---|---|
| `question_id` | The decision the entry records. |
| `answer_id` | The answer the entry records (answer entries only). |
| `decision_state` | The decision state the entry stands for: `open`, `answered`, `withdrawn` or `expired`. |

New entry kinds: `operator_decision`, `operator_answer`, `decision_gate_hold` and `decision_gate_shadow`. The fields are omitted when empty. A client that shows the log should render an unknown kind as plain text rather than failing.

### Task status `waiting_decision`

A task held on an unanswered decision has `status` set to `waiting_decision`, and its `wait_reason` is a plain sentence naming the decision and, for an open one, the first 160 characters of the question (for example "operator decision q-... is waiting for an answer: ..."). When the decision is answered the task returns to `pending` on its own and `wait_reason` clears. `wait_reason` is the same field already used for `waiting_capacity`. Treat `waiting_decision` as an active, non-terminal status. The PWA shows a question-mark glyph, the label "Waiting for a decision", and the wait reason under the task.

### Config keys

Both sit under `autonomous.questions` in `GET`/`PUT /api/config`.

| Key | Type | Default | Meaning |
|---|---|---|---|
| `autonomous.questions.decision_gate` | boolean | on when the key is absent | Whether a task that names an unanswered decision is held. |
| `autonomous.questions.decision_gate_mode` | string | `shadow` when empty | `shadow` records what would be held and holds nothing; `on` holds the task. Any other value is refused by `PUT /api/config`. |

Locale keys the PWA added (English values; the other locales carry placeholders): `prd_task_waiting_decision`, `decision_line_title`, `decision_line_empty`, `decision_state_open`, `decision_state_answered`, `decision_state_withdrawn`, `decision_state_expired`, `decision_answer_id`, `decision_answer_actor`, `settings_automata_questions_decision_gate`, `settings_automata_questions_decision_gate_mode`.

## Decision line the apps should render

On the Automaton detail screen, a read-only card titled "Decisions" fed by `GET /api/autonomous/decisions?prd_id=<id>`. One row per decision, in the order returned:

1. The decision id and a state chip (open, answered, withdrawn or expired). Open uses a warning colour, answered a success colour, the rest a neutral colour.
2. The question text.
3. For an answered decision: "Answer <answer id>", "by <actor>", the answered time, and the quote verbatim in a block quote, preserving line breaks.
4. For each task held on the decision: the task id and its wait reason.

An empty list shows "No decisions yet". A failed fetch leaves the card empty with no error block; the card is a convenience read. Ignore a response that arrives after the screen moved to another Automaton. Every value must be escaped or treated as plain text, and long ids and questions must wrap at phone width.

Task rows with status `waiting_decision` show the "Waiting for a decision" label and the task's `wait_reason`.

Settings: add the two config keys to the Automata settings card, next to the other operator question toggles.

## Acceptance criteria

### Android

- [ ] The Automaton detail shows the Decisions card from `GET /api/autonomous/decisions?prd_id=<id>` with one row per decision as described above.
- [ ] State chip, question, answer id, actor, answered time and the verbatim quote render for an answered decision; the quote is not trimmed or shortened.
- [ ] A task with status `waiting_decision` renders with its own label, glyph and `wait_reason`, and counts as active, not finished.
- [ ] An unknown task status or decision-log kind does not crash the screen.
- [ ] The Automata settings card has a toggle for `autonomous.questions.decision_gate` (absent reads as on) and a shadow/on choice for `autonomous.questions.decision_gate_mode` (empty reads as shadow), saved through `PUT /api/config`.
- [ ] A 400 on save for an invalid mode shows the server message.
- [ ] The new locale keys exist in all five locales.
- [ ] Layout holds at 360 dp width with a 20+ character id and a long question.

### iPhone/iOS

- [ ] Same list as Android, checked at the narrowest supported iPhone width.
- [ ] Dynamic Type at the largest size keeps the chip, id and quote readable without horizontal scroll.
- [ ] VoiceOver reads each row as state, question, then answer.

## Ships in shadow mode

The gate ships with `decision_gate` on and `decision_gate_mode` set to `shadow`. In shadow mode no task is ever held: the task runs, and the Automaton decision log records once, as a `decision_gate_shadow` entry, what would have been held. So an app will not see `waiting_decision` on any task until an operator switches the mode to `on`. Build and test the status handling against a daemon set to `on`, and show the mode setting so an operator can see why nothing is being held.
~~~~
