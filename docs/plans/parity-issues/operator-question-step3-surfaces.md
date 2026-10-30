# Operator-question Step 3: every surface can answer, and the PWA shows the queue — parity comment for datawatch-app #283

## Parity surface

Step 3 (this workstream) makes the operator-question channel answerable from
every route an operator is likely to be at — the CLI, MCP, the chat channel,
and the REST endpoint — and puts a live, self-refreshing queue, an answer form
and an answer log in the PWA's Automation view. Steps 1 and 2 already defined
the record and its API (raise, store, answer, withdraw, the events, the polled
GET, the `DATAWATCH_ANSWER:` line); the API contract is restated below for the
mobile app so parity is unambiguous. Nothing in this repository ships a mobile
build; the parity of these surfaces is the app's work. The datawatch-app issue
is the record of request.

## Comment body (posted verbatim to datawatch-app #283)

### [2026-10-29] Operator-question Step 3: every surface can answer, and the PWA shows the queue

Step 3 gives the operator-question channel a full answering surface on every
route an operator is likely to be at, and puts a live queue in the PWA. This
message records what Step 3 adds on the server and the PWA, restates the API
contract Steps 1 and 2 already defined (routes, bodies, states, errors, the
`question_raised` WebSocket message, the lifecycle events, the polled single
question reading, and the `DATAWATCH_ANSWER:` session line), and lists the new
PWA locale keys — which this repository already ships with DE, ES, FR and JA
values — that the app's native surfaces must match.

Everything stays behind the one switch `autonomous.questions.enabled` (off by
default). While that switch is off, none of the surfaces below can reach the
channel, the CLI/MCP/comm verbs have nothing to do, and every REST route
`503`s. With it on, raising a decision, seeing it in the queue, and answering
it are the loop.

#### What Step 3 adds

Server:

- **CLI** — five verbs under `datawatch question`, one per operation:
  - `raise` — raise a decision to yourself or a session
    (`--text`, one or more `--option id=label`, `--label`, `--context`,
    `--default`, `--recommendation-id`, `--risk`, `--priority`, `--return-path`,
    `--actor`);
  - `list` — filter the log by `--state`, `--context`, `--asker`, `--text`,
    `--limit`, `--json` for a machine-readable list;
  - `show <id>` — the question, its options, the worker's recommendation, and,
    if any, the current answer and the answer log;
  - `answer <id>` — record an answer: `--option <option id>` and/or
    `--text "…"` (at least one), `--supersedes <answer id>` when changing a
    mind, `--actor`;
  - `withdraw <id>` — withdraw an open one, `--reason`, `--actor`.
- **MCP** — five tools, one per operation, proxying it on loopback:
  `question_raise`, `question_list`, `question_get`, `question_answer`,
  `question_withdraw`. These cover five of the six REST routes; the answer
  log read (`GET /api/questions/{id}/answers`) is REST-only.
- **Chat channel (comm)** — three verbs on every channel, plus the bare-reply
  rule. A raise arrives in the channel as a numbered list of options, so
  `question answer <id> 2 "because of the deadline"` is all you type:
  - `question list` — the open ones, numbered;
  - `question show <id>` — one question with its options, recommendation and
    current answer;
  - `question answer <id> <option number or id> [quote…]`.

  The **bare-reply rule**: with exactly one open question a plain message
  answers it — the first word is an option number (the words after it are
  recorded as the quote) or the whole message is an exact option label of it.
  With two or more open questions a bare reply is refused and the open ones are
  listed back, so answer by id. When none is open, a plain message is just a
  chat message.

PWA (Automation view):

- **Queue card** — an *Open questions* card in the Automation tab, below the
  capacity and triage cards. Each open question is a row: its text (truncated),
  who asked, how long it has waited (relativized, updated while shown), a
  *high* marker, and an **Answer** button. A search box narrows the list (also
  narrowable to open only).
- **Answer form** — **Answer** opens a sheet that carries the question text,
  the numbered options, the worker's recommendation (a *Recommended: …* badge),
  and the default option (`Default: …`, the one picked if you submit without
  choosing). You pick an option and/or type words and press **Answer**.
  Submitting asks you to pick an option or type an answer first; if the question
  is already answered, the sheet shows the *Current answer* and notes that
  submitting again **supersedes** it.
- **History** — **History** opens the answered and withdrawn questions, each
  carrying its full answer log. Every answer line shows the chosen option
  (`Chosen: …`), who answered, the surface (`pwa`, `cli`, `rest`, `mcp`,
  `comm:…`), the time, and a **Current** or **Superseded by …** marker.
- **Live refresh** — on the `question_raised` message the queue updates
  immediately and a *New question* notice appears, no reload.
- **Switch off** — the card says the channel is disabled and points at
  `autonomous.questions.enabled` instead.

#### API contract (Steps 1 and 2)

Six REST routes, newest-first on the read; the channel is off by default and
**every route `503`s while the switch is off**:

| Route | Success body |
|---|---|
| `POST /api/questions` | `201` — the raised question |
| `GET /api/questions` | `200` — `{count, questions[]}`, newest first |
| `GET /api/questions/{id}` | `200` — one question plus the answer metadata |
| `POST /api/questions/{id}/answer` | `201` — the stored answer plus the question's new state |
| `POST /api/questions/{id}/withdraw` | `200` — the question in its new state |
| `GET /api/questions/{id}/answers` | `200` — the answer log, oldest first, plus the in-force answer id |

**States.** A question moves `open` → `answered` or `open` → `withdrawn`;
`expired` is the fourth terminal state (a valid value but never produced — a
question has no deadline and never expires by a clock). `open` and `answered`
are the only states that accept an answer; `withdrawn` accepts none. A changed
mind is not an edit: it is a new answer that names the one it replaces through
`supersedes` — the old line stays in the log, derived as superseded, and nothing
is ever edited or deleted.

**Question body** (raise) and **question record** (reads) — the fields an
operator or an app may send or render: `id`, `state`, `label`, `text`,
`options[]` (each `id`, `label`, `tradeoffs`), `default` (an option id of this
question, or `hold`), `recommendation` (`option_id`, `rationale`), `priority`
(`normal` or `high`), `risk` (`normal` or `low`), `return_path` (bool),
`asker_kind` (`session`, `supervisor`, `operator`, `system`), `asked_by`,
`session_id`, `context` (`none`, `prd`, `task`, `proposal`, `registry_row`)
with its matching link, `created_at`. The worker's recommendation sits on the
record, not in a separate read.

**Answer body** (answering) — `chosen_option_id` (must name an option of the
question), `quote` (the operator's own words; at least one of the option or a
non-blank `quote` is required), `supersedes` (the id of the answer being
replaced, when changing a mind), `surface` (`pwa`, `android`, `ios`, `cli`,
`rest`, `mcp`, or `comm:<channel>`), `actor`. A stored answer also carries
`answer_id` and, on the single-question reading, `quote_sha256` so any later
copy of a stored quote can be checked against the record. An answer whose text
looks like a secret is refused before anything is stored and the response never
echoes any part of it.

**Withdraw body** — `reason` (free text) and `actor`, both optional.

**List query** (read) — `state` (`open`, `answered`, `withdrawn`, `expired`),
`context` (`none`, `prd`, `task`, `proposal`, `registry_row`), `asker` (exact
`asked_by`), `text` (a case-insensitive substring over the question text,
option labels and tradeoffs, and answer quotes), `limit` (positive integer,
capped at 1000). An unknown `state` or `context` is a `400`.

**Errors** — the routes return: `400` for a bad body or a missing `text` with no
`options` (the failing fields are named); `403` for a capability the caller
does not carry; `404` for an unknown id or a path below one that is not an
answer/withdraw sub-path; `405` for a method the route does not take; `409` for
an answer in a state that no longer accepts one, or a withdrawal after the
question has moved on; `422` for an answer that looks like a secret (refused,
and nothing of it echoed); and, while the switch is off, **`503` for every
route**, naming `autonomous.questions.enabled`.

**`question_raised` WebSocket message** — sent to every connected client on a
successful raise, best-effort and off the request path:

```json
{ "type": "question_raised",
  "data": { "id": "…", "label": "…", "text": "…",
            "options": [ { "id": "…", "label": "…" } ],
            "priority": "…", "context": "…" } }
```

`id` and `text` always appear; `label`, `options`, `priority` and `context` are
omitted when empty. This is the PWA's live feed (see *Live refresh* above).

**Lifecycle events — `question.answered` and `question.withdrawn`.** Ridden on
the ordinary lifecycle event rails; the kinds are in the closed
`KnownEventKinds` set, so a subscription's `filter.kinds` may name them, and
exactly-once is guaranteed by the event sink (one per answer, one per
withdrawal, replayed at daemon start). `question.answered` carries the
question, the answer, the chosen option, the asking session/automaton ids when
named, and one line of details; `question.withdrawn` carries the question, the
actor and, when given, the reason. There is no `question.expired` kind, and one
is not planned.

**Polled single-question reading** — `GET /api/questions/{id}` is the default
return path for an asker that cannot subscribe: the question plus
`answer_count`, `current_answer_id`, and (while it has no answer) `answer:
null`; once it has one, the in-force answer's `chosen_option_id`, `quote` (as
stored), `actor`, `surface` and `created_at`.

**The `DATAWATCH_ANSWER:` session line** — when a question was raised with
`return_path: true` and the `autonomous.questions.return_path` toggle is on,
an answer against it is additionally delivered into the asking session as
exactly one line of compact JSON:

```
DATAWATCH_ANSWER: {"question_id":"…","answer_id":"…","chosen_option":"…","quote":"…","actor":"…","at":"…"}
```

All values are JSON strings; `at` is in RFC 3339 UTC. `chosen_option` is empty
when the operator answered in free text, `quote` is the operator's words exactly
as stored (control bytes JSON-escaped, so it stays a single physical line), and
`actor` is the answering party of record. The marker is distinct from, and
never read as, `DATAWATCH_COMPLETE:` and `DATAWATCH_NEEDS_INPUT:` — one session
does not treat a question's answer as its completion signal. The line is
best-effort and can never block or fail the answer: the event and the polled
reading carry the answer either way, and the session keeps running when it does
not arrive.

#### New locale keys

Thirty-one new PWA keys, all in the `automation_questions_` family. The English
reference values below are the translation source — keep the `%1$s`
placeholders exactly where they are, and preserve their surrounding
punctuation:

| Key | English (reference) |
|---|---|
| `automation_questions_title` | Open questions |
| `automation_questions_empty` | No open questions |
| `automation_questions_disabled` | Operator questions are currently disabled: the question switch is off. |
| `automation_questions_search` | Search open questions |
| `automation_questions_open` | Answer |
| `automation_questions_high` | high |
| `automation_questions_by` | by |
| `automation_questions_age` | asked %1$s ago |
| `automation_questions_failed` | Open questions failed: %1$s |
| `automation_questions_raised` | New question: %1$s |
| `automation_questions_answer_title` | Answer question %1$s |
| `automation_questions_answer_current` | Current answer |
| `automation_questions_answer_default` | Default: %1$s |
| `automation_questions_answer_rec` | Recommended: %1$s |
| `automation_questions_answer_quote_label` | Your answer text (optional when you pick an option) |
| `automation_questions_answer_required` | Pick an option or type an answer before submitting. |
| `automation_questions_answer_supersedes_note` | This answer replaces the current one. |
| `automation_questions_answer_submit` | Answer |
| `automation_questions_history` | History |
| `automation_questions_history_title` | Question %1$s — answer log |
| `automation_questions_history_list_empty` | No answered or withdrawn questions |
| `automation_questions_history_no_answers` | No answers recorded for this question. |
| `automation_questions_history_actor` | by %1$s |
| `automation_questions_history_answered` | answered |
| `automation_questions_history_chosen` | Chosen: %1$s |
| `automation_questions_history_current` | Current |
| `automation_questions_history_date` | on %1$s |
| `automation_questions_history_surface` | via %1$s |
| `automation_questions_history_time` | at %1$s |
| `automation_questions_history_superseded` | Superseded by %1$s |
| `automation_questions_history_withdrawn` | withdrawn |

All five bundles in this repository already carry values for these keys:
EN is the reference, and DE, ES, FR and JA hold the first-pass translations
shipped with this step's UI (for example `automation_questions_high` is
"Hoch" in DE, "Alta" in ES, "Haute" in FR and "高優先" in JA — none of them
is an English placeholder). Since the Localization Rule keeps this repository's
bundles 1:1 with the app's Compose Multiplatform client, add these keys to
the app's locale resources with the same values when you build the queue,
answer form and history; where the app's final DE, ES, FR or JA wording
differs, send the final strings back so this repository can mirror them and
the two surfaces stay identical.

**Nothing is built in this repository for mobile.** datawatch holds the daemon
and the PWA; the app holds the native surfaces. The parity of this Step 3 is on
the app's side — the same queue, answer form, history, live update, and the
same `question_*` tools and `question …` chat verbs the operator is used to on
this side.

## Filing record

- Posted: 2026-10-29 (datawatch-app issue 283) — https://github.com/dmz006/datawatch-app/issues/283#issuecomment-6508901768
- Via: `gh issue comment 283 -R dmz006/datawatch-app --body-file <comment-body-file>`
- Note: on 2026-10-29 the posted body was corrected in place (edited via the
  comments API, same comment, no duplicate left on the issue): the locale
  paragraph now states what the bundles actually contain (translated DE, ES,
  FR and JA values added with this step, no English placeholders) and the MCP
  bullet states that five of the six REST routes have a matching tool, the
  answer log read being REST-only.
