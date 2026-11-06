# [sync] Decision records: the daemon builds the operator question from a structured record

Parity issue for dmz006/datawatch-app. It covers Android and iPhone/iOS together, so no platform tag is used. The text between the two body markers below is the issue body; the sections after the end marker record the filed issue.

<!-- body-start -->
## Daemon version

Lands in the next patch release above main at the time the change merges, released as a minor bump (main is at 9.101.1 as this is written, so the release number is whatever the merge helper takes at merge time). The mobile apps should gate the feature on the daemon reporting that release or later.

## What changed in datawatch

An operator decision can now be written once as a structured **decision record** (label, title, situation, what is being decided, options with their trade-offs, a recommendation and a default). The daemon builds the operator question from the record, so every question has the same shape. Nobody types the question text by hand. The record is stored by the daemon, raises exactly one question, follows edits and withdrawals of the record, and keeps the operator's answer, the result of the document change and the follow-up outcome. The PWA has a new Decision records card in the Automata area; the apps need to match it.

The questions API also changed in two small ways: a question can carry the id of the record it asks about, and an answer can name a follow-up to run.

## REST contract

Records are created from REST, MCP, the CLI, chat and a watched folder. The apps only need the REST routes below. The same data is on MCP as `autonomous_decision_record_list` and `autonomous_decision_record_get`, on the CLI and on the comm channel.

All routes need a federation capability: reads need `autonomous:read`, every change needs `autonomous:write` (a peer without it gets 403). Every route returns 503 when the decision record service is not available, or when a create or update is made while `autonomous.decisions.enabled` or `autonomous.questions.enabled` is off. A path below `/api/autonomous/decision-records` other than the ones listed is a 404. A method not listed for a route is a 405 with an `Allow` header.

| Method and route | Capability | Purpose |
|---|---|---|
| `GET /api/autonomous/decision-records` | read | List records, newest first, with the folder watch status |
| `POST /api/autonomous/decision-records` | write | Create a record |
| `GET /api/autonomous/decision-records/{id}` | read | One record |
| `PUT /api/autonomous/decision-records/{id}` | write | Replace the content of an open record |
| `POST /api/autonomous/decision-records/{id}/withdraw` | write | Withdraw a record and its question |

### List: `GET /api/autonomous/decision-records`

| Parameter | Type | Meaning |
|---|---|---|
| `prd_id` | string, optional | Only the records whose Automaton link is this id. |
| `state` | string, optional | One of `open`, `answered`, `withdrawn`. Any other value is a 400. |
| `limit` | integer, optional | Positive whole number; default 100, maximum 1000. Zero, negative or non-numeric is a 400. |

A filter that matches nothing returns an empty array, never a 404. Response:

```json
{
  "decision_records": [ { "...": "a record, see below" } ],
  "folder": {
    "enabled": false,
    "path": "",
    "files_seen": 0,
    "errors": [],
    "last_scan": null
  }
}
```

`folder` is the folder watch status. `errors` is always an array (it holds the plain messages for files that could not be read), and `last_scan` is a timestamp or `null`.

### Create, update, read and withdraw

Create (`POST`) and update (`PUT`) take only the content fields, as one JSON object of at most 64 KiB:

| Field | Type | Notes |
|---|---|---|
| `label` | string | Short human label. Default `decision`. |
| `title` | string | Plain-language title. |
| `situation` | string | What is going on. |
| `deciding` | string | What exactly is being decided. |
| `options` | array | Each `{id, label, tradeoff}`. |
| `recommendation` | object, optional | `{option_id, reason}`. |
| `default` | string | An option id, or the word `hold`. |
| `links` | object, optional | `doc` (repo-relative path), `automaton` (Automaton id), `registry_row` (a registry row key). |

A field outside this set, invalid JSON, or anything after the object is a 400. A caller can never set the state, the source, the question link or the answer. Withdraw takes an optional body `{"reason": "..."}`; an empty body is accepted.

Create, update, read and withdraw all answer with the same envelope:

```json
{
  "record": { "...": "a record, see below" },
  "question_id": "q-<hex>",
  "warnings": [ { "code": "...", "message": "..." } ]
}
```

`warnings` is present on create and update (always an array there, empty when there are none) and absent on read and withdraw. A warning with code `question_not_in_step` means the record is stored but its question could not be brought in step yet; the daemon retries on its next poll.

| Status | When |
|---|---|
| 201 | Created (also when the record is stored but the question is not in step yet, with a warning). |
| 200 | Read, updated or withdrawn. |
| 400 | Invalid body, unknown field, bad `state` or `limit`, or an invalid record. |
| 403 | Missing federation capability. |
| 404 | Unknown record id, or an unknown path below the prefix. |
| 405 | Method not allowed for the route. |
| 409 | The record is already answered or withdrawn (cannot be edited or withdrawn again), or it duplicates an existing record. |
| 413 | Body over 64 KiB. |
| 422 | Create or update refused by the clarity rules (`autonomous.decisions.clarity_mode` is `refuse`). The body is `{"error": "...", "findings": [ {code, message}, ... ]}`. |
| 503 | Service unavailable, or a required switch is off. |

### The record

| Field | Notes |
|---|---|
| `id` | `dr-` followed by 20 lowercase hex characters. |
| `label`, `title`, `situation`, `deciding` | As sent. |
| `options` | Always an array of `{id, label, tradeoff}`. |
| `recommendation` | `{option_id, reason}`; omitted when none. |
| `default` | An option id or `hold`. |
| `links` | `{doc, automaton, registry_row}`; each part omitted when empty. |
| `state` | `open`, `answered` or `withdrawn`. |
| `question_id` | The queue question the daemon raised from the record. |
| `question_state` | The state of that question; omitted when unknown. |
| `source`, `source_ref` | `rest`, `mcp`, `cli`, `comm` or `file`; for a file, its name inside the folder. |
| `content_hash` | SHA-256 of the content fields. |
| `created_at`, `updated_at` | Timestamps. |
| `answer` | Absent until answered; see below. |
| `doc_change` | Absent until the answer has been applied; see below. |
| `follow_up` | Absent unless a follow-up was requested; see below. |

**Answer.** `answer_id`, `chosen_option_id`, `quote`, `actor`, `surface`, `answered_at`. `answer.quote` is the operator's words exactly as stored, and `actor` and `surface` say who answered and from where. Do not trim, cut, re-wrap or re-case the quote. Until the daemon applies the answer to the record, the response fills `answer` from the answered question (quote, actor, surface and time, without `answer_id`).

**Document change report.** When the record links a document, the answer can update a status line in it. `doc_change` reports what happened: `mode` (`off`, `report` or `on`), `path`, `line`, `before`, `after`, `diff`, `at` and `status`. `status` is one of `reported` (nothing written, the change is only shown), `applied`, `unchanged`, `no_match` (no line matched the pattern), `ambiguous` (more than one line matched, nothing changed) or `error`. Show unknown statuses as plain text.

**Follow-up outcome.** `follow_up` has `requested` (boolean), `name` (the configured follow-up the operator named), `kind` (`start_template` or `open_automaton`), `target`, `status` (`done`, `skipped` or `error`), `result` (one plain line) and `at`. Only `requested` and `name` are present until the follow-up has run.

## New question and answer body fields

- **Question: `decision_record_id`.** Every question object returned by `/api/questions` (and the single-question read) can now carry `decision_record_id`: empty or omitted for an ordinary question, or the `dr-` id of the record the question asks about. When present, the question was built by the daemon from that record and the app can link to the record.
- **Answer body: `follow_up`.** `POST /api/questions/{id}/answer` accepts an optional string `follow_up`: the `name` of one entry of `autonomous.decisions.follow_ups`. It needs a question that belongs to a decision record, and a name that is not in the configured list is refused with the server message. Leave it out to run nothing. Nothing runs unless an operator has configured follow-ups.
- **Decision line: `record_id`.** Each entry from `GET /api/autonomous/decisions` now carries `record_id`: the id of the record the decision was raised from, or an empty string for a decision that has no record.

## Config keys

All sit under `autonomous.decisions` in `GET`/`PUT /api/config`. The daemon reads them at the moment they are used, so no restart is needed.

| Key | Default |
|---|---|
| `enabled` | `true` |
| `folder` | empty (folder watch off) |
| `folder_glob` | `*.md` |
| `front_matter_delimiter` | `---` |
| `front_matter_separator` | `:` |
| `front_matter_field_separator` | `\|` |
| `front_matter_keys` | empty (canonical names) |
| `folder_withdraw_on_delete` | `true` |
| `clarity_mode` | `warn` (`off`, `warn` or `refuse`) |
| `clarity_min_text_chars` | `60` |
| `clarity_require_tradeoffs` | `true` |
| `clarity_require_label` | `true` |
| `clarity_tracker_id_pattern` | empty (built-in pattern) |
| `doc_root` | empty (the daemon's working directory) |
| `doc_status_mode` | `report` (`off`, `report` or `on`) |
| `doc_status_pattern` | `^Confirmed:.*$` |
| `doc_status_template` | `Confirmed: {choice} - "{quote}" ({actor}, {date})` |
| `follow_ups` | empty list; each entry is `{name, kind, target}` |

## What the mobile screens should show

**Decision records card.** In the Automata area, a card titled "Decision records" fed by `GET /api/autonomous/decision-records`. On an Automaton detail screen, filter it with `prd_id=<id>`. One row per record, in the order returned, showing the title, the label, the state (open uses a warning colour, answered a success colour, withdrawn a neutral colour) and the record id.

**Record detail.** Opening a row reads `GET /api/autonomous/decision-records/{id}` and shows, as plain text:

- The situation, what is being decided, and each option with its trade-off. The recommended option is marked and its reason shown, and the default is shown (an option, or "hold").
- For an answered record: the answer quote verbatim with line breaks kept, the option chosen, the actor, the surface and the time.
- The document change report when present: status, path, line, and the before and after lines (the diff may be shown as secondary detail).
- The follow-up outcome when present: name, status and result.
- For an open record, a Withdraw action (`POST .../withdraw`, with an optional reason). A 409 shows the server message.

The question text in the answer sheet comes from the daemon, so the apps never build it. When a question has a `decision_record_id`, the answer sheet may offer a follow-up choice built from `autonomous.decisions.follow_ups`, and sends the chosen name as `follow_up`. With no follow-ups configured, show no choice.

Creating and editing records from a phone is not required in this issue; the apps read, show and withdraw.

An empty list shows "No decision records". A failed fetch leaves the card empty with no error block. Ignore a response that arrives after the screen moved to another Automaton. Every value is plain text, never markup, and long ids, titles and quotes must wrap at phone width. An unknown state, doc change status or follow-up status must not crash the screen.

Settings: add the `autonomous.decisions` keys that an operator is likely to change to the Automata settings card (`enabled`, `clarity_mode`, `doc_status_mode`), saved through `PUT /api/config`. A 400 on save shows the server message.

## Acceptance criteria

### Android

- [ ] The Decision records card lists records from `GET /api/autonomous/decision-records`, filtered by `prd_id` on an Automaton detail screen.
- [ ] The record detail shows the situation, deciding line, options with trade-offs, recommendation and default.
- [ ] An answered record shows the answer quote verbatim (not trimmed or shortened), the chosen option, the actor, the surface and the time.
- [ ] The document change report renders with its status; `reported`, `applied`, `unchanged`, `no_match`, `ambiguous` and `error` all display, and an unknown status shows as plain text.
- [ ] The follow-up outcome renders (name, status, result).
- [ ] Withdraw works on an open record, and a 409 shows the server message.
- [ ] A question with `decision_record_id` links to its record, and the answer sheet sends `follow_up` when the operator picks one.
- [ ] The `autonomous.decisions` settings save through `PUT /api/config`.
- [ ] The empty list, a failed fetch and a stale response after navigating away behave as described above.
- [ ] Layout holds at 360 dp width with a 20+ character id, a long title and a long quote.

### iPhone/iOS

- [ ] Same list as Android, checked at the narrowest supported iPhone width.
- [ ] Dynamic Type at the largest size keeps the state, title and quote readable without horizontal scroll.
- [ ] VoiceOver reads each row as state, then title, then the answer quote when there is one.
<!-- body-end -->

## Parity surface

| Surface | Status | Notes |
|---|---|---|
| REST `/api/autonomous/decision-records` (list, create, read, update, withdraw) | Shipped | Capabilities `autonomous:read` and `autonomous:write`. |
| MCP | Shipped | `autonomous_decision_record_list`, `_get`, `_create`, `_update`, `_withdraw`. |
| CLI | Shipped | `datawatch autonomous decision-records` with `list`, `show`, `create`, `update`, `withdraw`. |
| Comm channel | Shipped | `autonomous decision-records`, `decision-record <id>`, `decision-record create`, `decision-record withdraw`. |
| Folder watch | Shipped | `autonomous.decisions.folder`; status on the list route. |
| PWA | Shipped | Decision records card in the Automata area. |
| Question `decision_record_id`, answer `follow_up`, decision line `record_id` | Shipped | Same routes as before, new fields. |
| Android (datawatch-app) | Open | This issue. |
| iPhone/iOS (datawatch-app) | Open | This issue. |

## Filed issue

- Issue: [dmz006/datawatch-app#382](https://github.com/dmz006/datawatch-app/issues/382), "feat(i18n): add 39 decision record keys (decision_record_*, settings_automata_decisions_*) for decision records parity (datawatch v9.102.0)", open.
- Filed: 2026-11-06.
- No open issue covered this contract (#302 covers only the decision line and gate), so a new issue was created.
- At merge (2026-11-06, datawatch v9.102.0) the same issue was retitled `feat(i18n): add 39 decision record keys (decision_record_*, settings_automata_decisions_*) for decision records parity (datawatch v9.102.0)` and the parent release plus the 39 locale keys with their EN values were appended to its body.
