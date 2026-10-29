---
docs:
  index: true
  topics: [autonomous, operator-questions, answering, pwa, cli, mcp, comm, rest]
exec_params: []
exec_steps:
  - tool: question_list
    description: List the open operator questions — the queue you answer from (the same list the PWA automation-view card shows)
    args: {state: open}
    read_only: true
  - tool: question_get
    description: Open one question: options, recommendation, and if it is already answered, the current answer
    args: {id: ""}
    read_only: true
  - tool: question_answer
    description: Record the answer — choose an option (or leave it unset) and attach your words, verbatim
    args: {}
    read_only: false
  - tool: question_withdraw
    description: Withdraw an open question (removes it from open; the entry stays, marked withdrawn)
    args: {}
    read_only: false
  - tool: question_raise
    description: Raise a question of your own (with at least two options) when you want a decision recorded the same way
    args: {}
    read_only: false
---

# Answering operator questions

An Automaton worker that needs an operator decision can leave an open
question — the task it was on stays blocked until the question is
answered (or withdrawn). Answering is the operator's job, and the answer
becomes the **record of your decision**: the chosen option or the words
you typed, verbatim, plus who answered and from which surface.

Answering works from every surface — the PWA queue, the CLI, the MCP
tools, a chat channel, or plain REST:

| Method | How |
|--------|-----|
| **YAML** | `~/.datawatch/config.yaml` → `autonomous.questions.enabled: true` (off by default) turns the channel on; the three notification toggles (`notify_messaging`, `notify_alert`, `return_path`) only change how raises and answers are announced, never how you answer |
| **CLI** | `datawatch question answer <id> --option <id> [--text "…"]` (see the full verbs below) |
| **Web UI** | Automation tab → *Operator questions* card → **Answer** on a question → pick an option or type your words → **Answer**; **History** for the answer log |
| **REST API** | `POST /api/questions/{id}/answer` with `{"chosen_option_id": "proceed", "quote": "…"}` (routes below) |
| **Comm channel** | `question answer <id> <option number or id> [quote…]` — or a bare reply when exactly one question is open |

Every surface writes the same one-line record and emits the same
`question.answered` lifecycle event.

## PWA — the queue in the Automation view

The queue sits as a card in the **Automation** tab, below the capacity
and triage cards:

1. **Open the queue.** Every open question appears as a row: the text,
   who asked, how long it has been waiting, and a **High** marker for
   high-priority questions. The card refreshes on its own (it polls every
   five seconds) and updates the moment you answer or a worker raises —
   no reload needed.
2. **Find the question.** Type in the search box; the list narrows as you
   type, and pressing Enter opens the first match.
3. **Open a question.** Click **Answer** on a row. The sheet shows the
   question, its numbered options, the worker's recommended option, and
   the default option (the one chosen if you submit without picking).
   If the question already has an answer, the sheet shows the current
   answer and notes that submitting again **supersedes** it.
4. **Submit.** Pick a radio option and/or type your own words in the
   quote field, then click **Answer**. Both the option and your words are
   recorded (either one is enough).
5. **Changed your mind?** Answer again. Nothing is edited in place — the
   earlier answer stays in the log, marked *Superseded by* the new one,
   and the sheet for the question now shows the new current answer.
6. **Read the log.** Click **History** on the card. Answered and
   withdrawn questions list there; each opens with its full answer log —
   every answer with the chosen option, the actor, the surface
   (`web`, `cli`, `mcp`, `comm:<channel>`), the time, and the
   **Current** / **Superseded** markers.
7. If the channel is off, the card says so and points at the
   `autonomous.questions.enabled` config key.

## CLI

All five verbs exist (raise, list, show, answer, withdraw):

```bash
# The open queue (optionally narrowed; --json for machines)
datawatch question list --state open --text "which model" --limit 20

# One question: text, options, recommendation, current answer if any
datawatch question show <id>

# Answer: choose the option (the option id printed by show/list) and,
# optionally, your words. Either the option or the words is enough.
datawatch question answer <id> --option proceed --text "use the fast model, budget is fine"

# Changed mind: answer again — the new answer supersedes the old one.
datawatch question answer <id> --option pause --text "hold, check the budget panel first"

# Remove an open question that is no longer relevant (marked withdrawn,
# the entry stays; the blocked task releases)
datawatch question withdraw <id> --reason "solved upstream"

# Raise your own decision point the same way
datawatch question raise --text "Which backend for the export job?" \
  --option claude=Use the Claude backend --option openai=Use the OpenAI backend \
  --default claude
```

## MCP

The five tools, one per operation (`/api/questions` is proxied on
loopback, the same as the other comm-parity surfaces):

| Tool | Does |
|------|------|
| `question_raise` | Raises a question — text, options (at least two), optional default, priority `low`/`normal`/`high`; the daemon returns the stored question with its id |
| `question_list` | The queue — optional `state` (`open`, `answered`, `withdrawn`, `expired`, or `all`, default open), `context`, `asker`, `text` and `limit` — returns `{count, questions}` |
| `question_get` | One question with `answer_count`, `current_answer_id`, and the current answer's summary if it exists |
| `question_answer` | Records the answer — `chosen_option_id` (or `null`) and/or `quote`, plus an optional `superseded_answer_id` to say which answer this replaces; the daemon returns the stored answer with the question's new state |
| `question_withdraw` | Withdraws an open question — `id` and a `reason`; the daemon returns the question marked `withdrawn` |

## Chat channel (comm)

The `question` verb exists on every chat channel, mirroring the REST
surface:

```
question list                              — the open queue
question show <id>                         — one question: text, options (numbered), current answer if any
question answer <id> <N|option id> [quote…] — records the answer
```

The notice a worker raises arrives as a **numbered option list**, so
`question answer <id> 2 "because of the deadline"` is all you need to
type — the number or the option id resolves to the right option, and the
words after it are recorded verbatim.

**Bare-reply rule.** With **exactly one** open question, you can answer
it with a plain message instead of the verb: the first word is an option
number (the words after it become your quote), or the whole message is
exactly an option label. With **two or more** open questions a bare
reply is refused and the open questions are listed, so answer by id with
`question answer <id> …` instead. With **none** open, a plain message is
just chat.

## REST

The routes (all return `503` naming the switch while the channel is off):

| Route | Body |
|-------|------|
| `GET /api/questions` | — list. Optional filters: `state` (`open`\|`answered`\|`withdrawn`\|`expired`\|`all`, default `open`), `context`, `asker`, `text`, `limit` (default 50; 0 = unbounded) |
| `GET /api/questions/{id}` | — the question plus `answer_count`, `current_answer_id`, and the current answer's summary if one exists |
| `POST /api/questions` | raise: `{"text": "Which model for the retry?", "options": [{"id": "fast", "label": "The fast model"}, {"id": "big", "label": "The large model"}], "default_option_id": "big", "priority": "high", "context": {"automaton_id": "a71"}}` |
| `POST /api/questions/{id}/answer` | answer: `{"chosen_option_id": "fast", "quote": "cost matters more here today", "surface": "cli"}` |
| `POST /api/questions/{id}/withdraw` | withdraw: `{"reason": "answered in the session instead"}` |
| `GET /api/questions/{id}/answers` | — the full answer log, newest first (each entry has option, quote, surface, actor, `createdAt`, and `supersededBy`) |

```bash
curl -s -X POST http://localhost:8443/api/questions/q12/answer \
  -H 'content-type: application/json' \
  -d '{"chosen_option_id":"proceed","quote":"go, the budget covers it"}'
```

## What your answer means

- **The answer is the record.** It cannot be edited or deleted. Changed
  your mind? Answer again — the new answer becomes the current one and
  the older one stays in the log, marked *Superseded by <id>*. That
  chain is the decision history.
- **Questions never expire.** An open question stays open until it is
  answered or withdrawn. There is no timeout and **no way to bypass it**
  — the blocked task keeps waiting, and that pressure is intentional: a
  silence is a non-decision, not a decision.
- **Secret-looking words are refused.** An answer that looks like a
  credential (a token, key, or long hex/base64 blob) is rejected on
  every surface, so the value never lands in the question record or the
  audit trail. The safe routes: keep the value in the **datawatch
  secrets store** and reference it by name (`${secret:name}`) so only a
  name reaches the record, or point the session at a **file** — the
  answer carries the path, and the session reads the value from the
  file directly.
- **Withdrawal removes an open question.** It releases the blocked task
  and marks the entry `withdrawn` with your reason. It works only while
  the question is still open; an answered question is closed forever
  (supersede it instead), and a withdrawn one cannot be re-opened —
  raise a new one if the decision is still needed.

## See also

- Notifications for this channel (how raises and answers are
  announced): [operator-questions-notifications](operator-questions-notifications.md)
- The channel's REST reference: [api/autonomous — Operator question channel](../api/autonomous.md#operator-question-channel)
- The five tool/endpoint mappings: [api-mcp-mapping — Operator questions](../api-mcp-mapping.md#operator-questions)
