---
docs:
  index: true
  topics: [autonomous, operator-questions, answering, pwa, cli, mcp, comm, rest, tradeoff, quality-check]
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
    description: Raise a question of your own (with at least two options, and a trade-off for each) when you want a decision recorded the same way
    args: {}
    read_only: false
  - tool: question_quality
    description: Read the question quality check policy (mode, minimum length, tracker id pattern, queue preview lines) before raising a question
    args: {}
    read_only: true
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

1. **Open the queue.** Every open question appears as a row: its label
   (when it has one) with the question id as smaller text beside it, a
   short preview of the text, who asked, how long it has been waiting, and
   a **High** marker for high-priority questions. See
   [The queue row preview](#the-queue-row-preview). The card refreshes on its own (it polls every
   five seconds) and updates the moment you answer or a worker raises —
   no reload needed.
2. **Find the question.** Type in the search box; the search commits after a
   short pause (or immediately when you press Enter) and re-requests the
   list narrowed to matching questions — then answer from the row.
3. **Open a question.** Click **Answer** on a row. The dialog is covered
   in [Reading the answer dialog](#reading-the-answer-dialog) below.
   If the question already has an answer, the dialog shows the current
   answer and notes that submitting again **supersedes** it.
4. **Submit.** Pick a radio option and/or type your own words in the
   answer box, then click **Answer**. Both the option and your words are
   recorded (either one is enough).
5. **Changed your mind?** Answer again. Nothing is edited in place — the
   earlier answer stays in the log, marked *Superseded by* the new one,
   and the sheet for the question now shows the new current answer.
6. **Read the log.** Click **History** on the card. Answered and
   withdrawn questions list there; each opens with its full answer log —
   every answer with the chosen option, the actor, the surface
   (`pwa`, `cli`, `rest`, `mcp`, `comm:<channel>`), the time, and the
   **Current** / **Superseded** markers.
7. If the channel is off, the card says so and points at the
   `autonomous.questions.enabled` config key.

### Reading the answer dialog

- **Title and id line.** The title is the question's label, or the question
  id when it has no label. Under the title a small line reads
  *Question <id>*, so the id is always visible, ready to copy into the CLI
  or a chat reply.
- **Context link.** When the question is about something specific, a line
  *This question is about: …* sits under the question text. For a proposal
  or document it carries an **Open** link that opens the proposal in the
  documents viewer in a new tab, so you can read it before deciding. For an
  Automaton it carries an **Open** button that closes the dialog and opens
  that Automaton; for a task or a registry row it names the item as text.
  A question about nothing in particular shows no line. See
  *Question context link* in the
  [definitions](../datawatch-definitions.md).
- **Trade-off line.** Each option shows a small line under it saying what
  that choice gains and what it gives up. The asker is expected to write
  one for every option; an option without one is flagged by the
  [quality check](#the-question-quality-check).
- **Recommendation line.** When the asker has a preferred option, the
  dialog says *The asker recommends <option> because: <reason>* (or just
  *The asker recommends <option>.* when no reason was given).
- **Forced default.** Every question carries a default, and it can never be
  empty: it is either one of the options or the word **hold**. The dialog
  says *If nobody answers, <option> is used.* or *If nobody answers, the
  question stays on hold.* — so you always know what a silence means.
- **Answer box.** The box is labelled *Your answer in your own words* and
  is optional when you pick an option. Whatever you type is **saved exactly
  as written**, together with your choice, as the record of your decision,
  and the asker reads it as your answer. Nothing is trimmed, summarised or
  reworded on the way.

### The queue row preview

Each row in the queue shows only the first few lines of the question text.
A question longer than that gets a **Show more** control that expands the
row in place, and **Show less** folds it back; the row stays expanded while
the card refreshes itself. How many lines the preview shows is the
`autonomous.questions.queue_preview_lines` setting (default 3, a whole
number of 1 or more; the PWA re-reads it about once a minute).

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

# Raise your own decision point the same way. --tradeoff id=text says
# what each option gains and gives up (repeat it, one per option id).
datawatch question raise --text "Which backend for the export job? The nightly export has doubled in size and the current backend is close to its rate limit." \
  --option claude=Use the Claude backend --option openai=Use the OpenAI backend \
  --tradeoff claude="best results, but it counts against the weekly budget" \
  --tradeoff openai="cheaper per run, but the output needs a second look" \
  --default claude

# Read the quality policy the daemon will check the question against
# (chat: "question quality")
datawatch question quality
```

`--tradeoff` splits on the first `=`, so the trade-off text may itself
contain an `=`; an id that is not one of the `--option` ids is rejected.
In `warn` mode the raise succeeds and each finding is printed as a
`warning:` line on standard error; in `refuse` mode the question is not
raised, the findings are printed, and you send a better one.

## MCP

The tools, one per operation (`/api/questions` is proxied on
loopback, the same as the other comm-parity surfaces):

| Tool | Does |
|------|------|
| `question_raise` | Raises a question — text, options (at least two, each with an optional trade-off, or a `tradeoffs` object mapping option ids to text), an optional `default` option id, and priority `normal`/`high` (default `normal`); the daemon returns the stored question with its id |
| `question_list` | The queue — optional `state` (`open`, `answered`, `withdrawn`, `expired`, or `all`; omitted = all states), `context`, `asker`, `text` and `limit` (the maximum to return) — returns `{count, questions}` |
| `question_get` | One question with `answer_count`, `current_answer_id`, and the current answer's summary if it exists |
| `question_answer` | Records the answer — `chosen_option_id` and/or `quote` (at least one), plus the optional `supersedes` id when answering a question that already has a current answer; the daemon returns the stored answer with the question's new state |
| `question_quality` | Reads the quality check policy — no parameters; returns `mode`, `min_text_chars`, `tracker_id_pattern` and `queue_preview_lines` |
| `question_withdraw` | Withdraws an open question — `id` and `actor` (required), plus an optional `reason`; the daemon returns the question marked `withdrawn` |

## Chat channel (comm)

The `question` verb exists on every chat channel, mirroring the REST
surface:

```
question list                              — the open queue
question show <id>                         — one question: text, options (numbered), current answer if any
question answer <id> <N|option id> [quote…] — records the answer
question quality                           — the quality check policy
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
| `GET /api/questions` | — list. Optional filters: `state` (`open`\|`answered`\|`withdrawn`\|`expired`\|`all`; omit for all states), `context`, `asker`, `text`, `limit` (a positive integer — `0` and below are a 400; omitted means the daemon default of 100, capped at 1000) |
| `GET /api/questions/{id}` | — the question plus `answer_count`, `current_answer_id`, and the current answer's summary if one exists |
| `POST /api/questions` | raise — `text`, `options` (at least one; each may carry `tradeoffs` text, or send a top-level `tradeoffs` object mapping option ids to text), `default` (the option id chosen when you submit without picking), `priority` (`normal` or `high`), and `context` must name a kind — `none`, `prd`, `task`, `proposal` or `registry_row` — with its matching link (`prd_id`, `task_id`, `proposal_path`, `registry_row_key`): `{"text": "Which model for the retry?", "options": [{"id": "fast", "label": "The fast model"}, {"id": "big", "label": "The large model"}], "default": "big", "priority": "high", "context": "none"}` |
| `POST /api/questions/{id}/answer` | answer: `{"chosen_option_id": "fast", "quote": "cost matters more here today", "surface": "cli"}` (at least one of `chosen_option_id` or `quote`; `supersedes` when replacing a current answer) |
| `POST /api/questions/{id}/withdraw` | withdraw: `{"reason": "answered in the session instead"}` |
| `GET /api/questions/quality` | — the quality check policy: `mode`, `min_text_chars`, `tracker_id_pattern`, `queue_preview_lines` |
| `GET /api/questions/{id}/answers` | — the full answer log, oldest first (each entry has option, quote, surface, actor, and its derived `superseded` / `superseded_by` state) |

```bash
curl -s -X POST http://localhost:8443/api/questions/q12/answer \
  -H 'content-type: application/json' \
  -d '{"chosen_option_id":"proceed","quote":"go, the budget covers it"}'
```

## The question quality check

A question is only as useful as it is readable. When a question is raised,
the daemon checks it and looks for three problems: an option with no
trade-off written, question text shorter than the minimum length, and an
internal tracker identifier the operator cannot look up. The
`autonomous.questions.quality_mode` setting decides what happens next:

| `quality_mode` | What the daemon does |
|----------------|----------------------|
| `off` | No check. Every question is raised as sent. |
| `warn` (default) | The question is raised anyway and the findings come back to the asker — a `warnings` list on REST and MCP, `warning:` lines on the CLI. |
| `refuse` | The question is **not** raised. REST answers `422` with the findings, the CLI prints them, and the asker has to send a better question. |

Four keys under `autonomous.questions` tune it (all editable in
Settings → Automata, or with `datawatch config set`):

| Key | Meaning | Default |
|-----|---------|---------|
| `autonomous.questions.quality_mode` | `off`, `warn` or `refuse` | `warn` |
| `autonomous.questions.quality_min_text_chars` | The fewest characters the question text may have; `0` turns the length check off | `60` |
| `autonomous.questions.quality_tracker_id_pattern` | A regular expression for what counts as a tracker identifier; empty uses the built-in pattern (a run of one to three capital letters followed by two to four digits) | empty (built-in) |
| `autonomous.questions.queue_preview_lines` | How many lines of each question the queue row shows before **Show more** | `3` |

Setting a pattern that is not a valid regular expression is refused when
you save it. The check only runs while the question channel is on
(`autonomous.questions.enabled`). See *Question quality check* in the
[definitions](../datawatch-definitions.md).

## What your answer means

- **The answer is the record.** What you type is saved exactly as written,
  with your choice, and the asker reads it as your answer. It cannot be edited or deleted. Changed
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
