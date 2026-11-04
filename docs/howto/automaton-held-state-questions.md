---
docs:
  index: true
  topics: [automation, autonomous, automaton, held state, held question, operator question, shadow mode, blocking, decision, review]
exec_steps:
  - tool: autonomous_held_questions
    description: Read the held-question report — the mode in effect, the held states covered, the open held questions to answer and the shadow entries logged instead
    args: {}
    read_only: true
  - tool: autonomous_config_get
    description: Read the autonomous settings, including the held-question mode and held states
    args: {}
    read_only: true
  - tool: config_set
    description: Set the held-question mode (off, shadow or on) on key autonomous.questions.held_mode; it takes effect on the next pass. The state list, if ever needed, is the same tool with key autonomous.questions.held_states and a JSON-array value (unset = all five, [] = none)
    args:
      key: autonomous.questions.held_mode
      value: "{{params.mode}}"
    read_only: false
  - tool: question_answer
    description: Answer an open held question with one of its options (hold is always offered and is always the default) or the operator's own words
    args:
      id: "{{params.question_id}}"
      chosen_option_id: "{{params.option}}"
    read_only: false
  - tool: autonomous_held_questions
    description: Confirm the answer was applied — the question is gone from open, and a decision entry with the applied action is on the Automaton
    args:
      prd_id: "{{params.automaton_id}}"
      state: "{{params.state}}"
    read_only: true
---

# Held-state questions

**Held-state questions** are the daemon's own operator questions, raised on
its own initiative when an Automaton sits in one of the five **held states**
and is waiting on a decision the daemon will not make for the operator.
Before, the held Automaton sat there and only you — if you happened to look
at the Automaton detail — could see it was waiting. Now the daemon raises a
question the moment the hold appears, with the real actions available for
that state, a recommendation and a reason, and it is visible on the PWA, the
MCP tools, the CLI, the chat channel and REST, exactly like a worker's
question but asked by the daemon itself.

You answer it with one of its options — **hold** is always one of them and
is always the default, so nothing can ever be decided silently on your
behalf — or with your own words. The answer is routed into the daemon's
existing action paths, and the Automaton moves once, or stays exactly where
it was if you chose hold.

## Base requirements

- A running daemon with `autonomous.enabled: true` and the operator-question
  channel on: `autonomous.questions.enabled: true`. Held questions live under
  that switch; while it is off the report endpoint answers 503 and names the
  switch, and the daemon raises nothing.
- The Automation coordinator running. It is the same loop that advances
  Automations, so it needs no separate service.
- Nothing else. No extra binaries, no extra config: the mode defaults to
  **shadow**, which is the mode you should run first.

## The five held states

The pass looks at the Automation line on every poll and asks, for each
Automaton, whether it is in one of these states:

| Held state id | What it means | The question offers (plus always **hold**) |
|---|---|---|
| `blocked` | The Automaton is blocked, waiting on a condition outside the task | release the hold (resume) · retry the task · change the task spec before retrying · cancel the task |
| `review_held` | A task is held for operator review after its failed attempts reached the escalation limit (the failure-escalation "held for review" state) | retry on the current worker · retry on a stronger available backend · change the task spec before retrying · cancel the task |
| `revision_requested` | A revision of the Automaton's spec was requested in review | change the spec (satisfies the request) · release the hold (your review is done) |
| `awaiting_decision` | A task is parked waiting on an unanswered operator decision | release the hold and let it continue · cancel the task |
| `pending_insert` | A member insert into an Automation chain is pending, waiting on operator approval | approve the insert · reject the insert |

Every option maps to a real action the daemon already implements: the
failure-escalation review resolution (retry, retry stronger, spec change,
cancel), the Automaton approve / resume paths, and the operator-only pending
insert approval. **Hold** means record the answer and leave the state exactly
as it is — nothing runs, and the question is answered, so the timeline shows
you saw it.

### Who asks, and what is in the question

The asker is the daemon itself (`autonomous`, system kind), the label is
`decision`, the question is **blocking** (so no worker session is left
waiting on it), and the record names the held Automaton with its title and
id, the owning Automation and member when the hold is one of them, the task
when there is one, the reason it is held and, when known, the failure
category (`infra`, `content`, `scope` or `policy`), the attempts used and the
available backends. The recommendation comes from a fixed table keyed on
(state, failure category) — never a model — and it is one sentence of
rationale.

### Auto-withdrawal and stale answers

- **The state clears → the question withdraws.** On the next pass, an open
  held question whose held state is no longer present is withdrawn by the
  daemon with a reason naming the cleared state — you do not have to tidy it
  up.
- **The state changed before you answered → the answer is refused.** Before
  applying any option the daemon re-checks that the item is still in the
  state the question was raised for. An answer that names a state the item
  already left is refused (one decision entry, `result: stale`, records the
  full answer for the timeline), the question is closed and nothing runs.
- **No double-asking.** A held (item, state) already covered by an open
  question is never raised a second time; for `awaiting_decision`, the task
  links to the decision already open instead of a new question.

## The three modes

| Mode (`autonomous.questions.held_mode`) | What the pass does | Raises anything? |
|---|---|---|
| `off` | Does not run. Detects nothing, writes nothing, withdraws nothing. | No |
| `shadow` (default) | Runs on every poll and computes each held Automaton, but records one append-only note per (item, state) on the Automaton's record. No question is raised. | No |
| `on` | Runs on every poll and raises one question per held (item, state), deduped by a stable key so a restart does not double-ask. | Yes |

**Why shadow comes first.** Shadow is the same detection with the actions
removed: for a day or so the report shows you exactly which holds the daemon
would have asked about and which option it would have recommended, against
what the Automaton actually did. Then you switch to `on`. Switching mode
takes effect on the next pass; no restart is needed.

## Configuration

Two keys, both under the existing operator-question block — the held-question
feature is fully described by them plus the channel switch:

```yaml
autonomous:
  questions:
    enabled: true                      # the operator-question channel (base requirement)
    held_mode: shadow                  # off | shadow (default) | on
    held_states: [blocked, review_held, revision_requested, awaiting_decision, pending_insert]
```

- `held_mode` — `off`, `shadow` or `on`. Empty (key absent) reads as
  `shadow`. Any other value is refused on write.
- `held_states` — the held state ids the pass covers. Absent (no list) means
  all five; an explicit empty list means none; an unknown state id is refused
  on write. Use it to ask only about the states you care about, for example
  `held_states: [review_held, pending_insert]`.

Set either with `PUT /api/config` (body `{"autonomous.questions.held_mode": "on"}`), from the PWA Settings (the Automation section's
held-question row), or through the MCP `config_set` tool (key
`autonomous.questions.held_mode`). There is
no per-Automaton override: both keys are daemon-wide.

## Reading the report

The report carries the same facts on every surface:

- the **mode in effect** (`off`, `shadow` or `on`) — the daemon's effective
  mode, so it reads `shadow` even when you never set the key;
- the **held states** covered by the pass;
- the **open held questions** (in `on` mode): the question id, the held
  state, the held Automaton's title and id, the task when there is one, the
  owning Automation, the reason it is held, the **recommended option** and
  the **default** (always `hold`), and when it was raised;
- the **shadow entries** (in `shadow` mode): each hold the daemon logged
  instead of raising — the Automaton, the held state, the option it would
  have recommended, and when.

| Surface | How |
|---|---|
| REST | `GET /api/autonomous/held-questions` — optional filters `?prd_id=<id>` (one Automaton only) and `?state=<held state id>` (one of the five; anything else is rejected). Read-only; 405 on non-GET. |
| MCP | `autonomous_held_questions`, with the same optional `prd_id` and `state` arguments. Read-only. Answer with the standard `question_answer` tool (`chosen_option_id`, or `quote`; `supersedes` for a changed mind). |
| CLI | `datawatch question held [--prd <id>] [--state <id>] [--json]` — the same report, human-readable or raw JSON. Answer with `datawatch question answer <id> --option <option id> [--text "…"]`. |
| Chat | `question held [prd <id> \| state <held state id>]` — read-only. `question answer <id> <option number or id> [quote…]` answers; a bare reply still answers the single open question, as with worker questions. |
| PWA | The **Held-question** card on the Automation view (Inbox sub-tab), directly beneath the open-questions card: the mode badge, every open held entry with its recommendation, the forced default line, a link that opens the Automaton detail, and the usual answer sheet. In shadow mode the would-be holds are listed beneath them. |

## Shadow dogfood, then switch to on

The pattern is the same as every other daemon check in datawatch (CI watch,
lane fill, merge-pending):

1. Leave the mode at **shadow** for a little while and read the report
   (`datawatch question held`, the PWA card, or `autonomous_held_questions`).
   You will see the shadow entries — the holds that would have been asked,
   with their recommendations — side by side with what the Automata actually
   did.
2. Check that the recommendations match what *you* would have done. If a
   state is noise for you, drop it from `held_states`.
3. Switch `held_mode` to `on`. The next pass raises the real questions —
   one per held (item, state), never a second for the same one while it is
   open.
4. Answer questions from any surface. Every answer lands once (idempotent by
   answer id): a decision entry on the Automaton with the operator's words,
   the actor, the surface, the chosen option and the result
   (`applied` / `recorded` / `refused` / `stale`).

## What it never does

- **Never acts on your behalf.** Every question is **blocking**: holding is
  the default on every one, and only your answer moves the Automaton (or
  deliberately leaves it alone).
- **Never double-asks.** One open question per held (item, state), stable
  across restarts; a linked decision question is never re-raised.
- **Never invents states or options.** The five states, the per-state
  options and the recommendations come from a fixed table; an unknown state
  gets the safe fallback (real actions only, hold last) and a hold
  recommendation.
- **Never erases history.** Answers are append-only, withdrawals carry the
  reason, and stale refusals keep the rejected answer in the decision log —
  the timeline always shows who answered what, on which surface, and what
  happened to it.
- **Never touches worker questions.** A worker asking a design question in
  its session is a different thing with the same channel; the pass only acts
  on held Automata, and the two never collide (held questions carry a
  distinct dedupe session id).

See also [operations.md](../operations.md) (*Held-state questions*),
[config-reference.yaml](../config-reference.yaml)
(`autonomous.questions.held_mode`, `autonomous.questions.held_states`) and
[operator question answering](operator-questions-answering.md) for the answer
record and every answer surface in detail.
