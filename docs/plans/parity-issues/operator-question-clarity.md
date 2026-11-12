# [sync] Clearer operator questions: trade-offs, quality policy and context link

Parity issue for dmz006/datawatch-app. It covers Android and iPhone/iOS together, so no platform tag is used. The text between the two body markers below is the issue body; the sections after the end marker record the filed issue.

<!-- body-start -->
## Daemon version

Lands in the next minor release above the one on main when the change merges. The mobile apps should gate the feature on the daemon reporting that release or later. This follows the operator question channel work already tracked in #283, and the Decisions card in #302.

## What changed in datawatch

An operator question now says what each choice costs, says what it is about, and is checked for being readable when it is raised. The PWA answer dialog and the queue rows were reworked to show this. The REST contract changed in five places: a new `tradeoffs` request field on the raise, `warnings` on the raise response, a `422` refusal, a new read `GET /api/questions/quality`, and a computed `context_ref` on the question reads. The apps need to match the PWA.

## REST contract

### Raise: `POST /api/questions`

New optional request field `tradeoffs`: an object mapping an option id to one plain line saying what the operator gains and gives up by choosing it. A trade-off can also be sent on the option itself as `options[].tradeoffs` (the option field already exists on the record); when both are sent the top-level object fills options that have none. Each trade-off is capped at 1000 bytes. An id in `tradeoffs` that is not among the options is a 400.

Response `201`:

```json
{
  "question": { "id": "q-…", "label": "…", "text": "…", "options": [ { "id": "fast", "label": "The fast model", "tradeoffs": "cheaper per run, but the output needs a second look" } ], "default": "fast" },
  "warnings": [ { "code": "no_tradeoffs", "message": "…" } ]
}
```

`warnings` is left out when there is nothing to report. It is present only when `autonomous.questions.quality_mode` is `warn`, and the question is stored either way.

### Refusal: HTTP 422

When `quality_mode` is `refuse` and the question has findings, nothing is stored and the answer is:

```json
{ "error": "question refused: it does not meet the question quality policy; fix the findings and send it again",
  "findings": [ { "code": "text_too_short", "message": "…" } ] }
```

The `code` values are stable: `no_tradeoffs` (an option with no trade-off line), `text_too_short` (the text is shorter than the minimum), `tracker_id` (the text names an internal tracker identifier the operator cannot look up). Treat an unknown `code` as plain text and show the `message`. This is a different 422 from the existing refusal of secret-looking wording; tell them apart by the presence of `findings`.

### New read: `GET /api/questions/quality`

Returns the effective policy the daemon checks every raised question against. It answers `503` while the question channel is off, like the other question routes.

| Field | Type | Meaning |
|---|---|---|
| `mode` | string | `off`, `warn` or `refuse`. Default `warn`. |
| `min_text_chars` | integer | The fewest characters the question text may have. `0` turns the length check off. Default 60. |
| `tracker_id_pattern` | string | The regular expression for what counts as a tracker identifier. |
| `queue_preview_lines` | integer | How many lines of a question the queue row shows before **Show more**. Default 3. |

### Computed `context_ref` on `GET /api/questions/{id}` and the list rows

Every question read now carries a computed `context_ref` beside the stored fields. It is built when the question is read and is never part of the stored record, so a client must not send it back.

| Field | Type | Notes |
|---|---|---|
| `kind` | string | `proposal`, `automaton`, `task`, `registry_row` or `none`. Treat an unknown value as `none`. |
| `title` | string | What to show: the file name for a proposal, the stored name for an Automaton or a task (the id when the name is not known), the key for a registry row. Empty for `none`. |
| `id` | string | The Automaton id, task id or registry row key. Empty for a proposal and for `none`. |
| `doc_path` | string | Repository-relative path of the proposal document. Empty otherwise. |
| `viewer_url` | string | For a proposal inside the served docs tree, the in-app document viewer link (`/diagrams.html#docs/<path>`). Empty when the file is not a served markdown document; then show the title without a link. |

`GET /api/questions/{id}` already returned `answer_count`, `current_answer_id` and the standing `answer`; those are unchanged.

### Config keys

Under `autonomous.questions` in `GET`/`PUT /api/config`. A bad value is refused with nothing changed; a good one applies at once with no restart.

| Key | Type | Default |
|---|---|---|
| `autonomous.questions.quality_mode` | `off` / `warn` / `refuse` | `warn` |
| `autonomous.questions.quality_min_text_chars` | integer | 60 |
| `autonomous.questions.quality_tracker_id_pattern` | string (regular expression; empty uses the built-in) | empty |
| `autonomous.questions.queue_preview_lines` | integer | 3 |

Locale keys the PWA added (English values in `internal/server/web/locales/en.json` of the datawatch repository; the other four locales carry them too): `automation_questions_show_more`, `automation_questions_show_less`, `automation_questions_answer_id_line`, `automation_questions_answer_text_label`, `automation_questions_answer_text_help`, `automation_questions_context_label`, `automation_questions_context_open`, `automation_questions_answer_rec_line`, `automation_questions_answer_rec_line_no_rationale`, `automation_questions_answer_default_line`, `automation_questions_answer_default_line_hold`, `settings_automata_questions_quality_mode`, `settings_automata_questions_quality_min_text_chars`, `settings_automata_questions_quality_tracker_id_pattern`, `settings_automata_questions_queue_preview_lines`.

## What the mobile dialog should show

The answer dialog and the queue are the same screens as in #283; this changes what they show.

- **Label as the title.** The dialog title is the question's `label`. The question id is the small secondary line under it, not the title. A queue row also leads with the label and shows the id as the small secondary line.
- **Answer-text helper line.** The free-text answer box is labelled "Your answer in your own words" and has one helper line under it: "Optional when you pick an option. Whatever you type is saved exactly as written, with your choice, as the record of your decision, and the asker reads it as your answer."
- **Trade-off lines.** Under each option, show its trade-off line (from `options[].tradeoffs`) in smaller text. An option with no trade-off shows no empty line.
- **Context link.** Under the question text, show "This question is about: …" with what it is about, from `context_ref`: a `proposal` with a `viewer_url` opens that document in the viewer in a new tab; an `automaton` shows its `title` and an **Open** button that goes to the Automaton; a `task` or `registry_row` shows its `title` and kind as plain text; `none` shows nothing. The link is text and a button, never markup taken from the field.
- **Recommendation line.** When the question has a `recommendation`, say "The asker recommends <option> because: <rationale>", or "The asker recommends <option>." when there is no rationale.
- **Default line.** Say "If nobody answers, <option> is used.", or "If nobody answers, the question stays on hold." when `default` is `hold`.
- **Queue preview.** A queue row shows only the first `queue_preview_lines` lines of the question text (read it from `GET /api/questions/quality`; if that read fails, use 3) with **Show more** / **Show less**. The expanded state is remembered per question across refreshes.

## Acceptance criteria

### Android

- [ ] The dialog title is the label and the id is the small second line; the same for queue rows.
- [ ] The answer box has the helper line under it.
- [ ] Each option shows its trade-off line when it has one and nothing when it has none.
- [ ] The context link renders for each `context_ref.kind`, including a proposal with an empty `viewer_url` (title only) and an unknown kind (nothing).
- [ ] The recommendation line and the default line render in plain words, including `default` of `hold`.
- [ ] Queue rows clip to `queue_preview_lines`, expand and collapse, and keep the state across a refresh; a failed policy read falls back to 3.
- [ ] A raise that comes back `201` with `warnings` and one that comes back `422` with `findings` both parse; an unknown finding `code` shows its `message`.
- [ ] The new locale keys exist in all five locales.
- [ ] Layout holds at 360 dp width with a long label and a long trade-off line.

### iPhone/iOS

- [ ] Same list as Android, checked at the narrowest supported iPhone width.
- [ ] Dynamic Type at the largest size keeps the label, the helper line, the trade-off lines and the **Show more** control readable without horizontal scroll.
- [ ] VoiceOver reads the context link as a link or button and the expand control as a button with its state.

## Ships behind the existing switch

All of this sits behind `autonomous.questions.enabled`, which is off by default. Build and test against a daemon with it on, and with `quality_mode` set to each of `off`, `warn` and `refuse`.
<!-- body-end -->

## Parity surface

Every surface the change could reach, and whether it changes. Where it does not change, the reason is given.

| Surface | Changes? | Detail or reason |
|---|---|---|
| REST | Yes | A `tradeoffs` request field on `POST /api/questions`, `warnings` on the `201` raise response, a `422` refusal with `findings` when `quality_mode` is `refuse`, the new read `GET /api/questions/quality`, and a computed `context_ref` on `GET /api/questions/{id}` and the list rows. |
| MCP | Yes | `question_raise` takes the `tradeoffs` object and returns `warnings`; the new read-only tool `question_quality` returns the policy. |
| CLI | Yes | `datawatch question raise --tradeoff <option>=<text>`, `datawatch question quality [--json]`, and `question show` prints the trade-offs and the context. |
| Comm channel | Yes | The verb `question quality`; `question show` carries the trade-offs. |
| YAML/config | Yes | Four keys under `autonomous.questions`: `quality_mode`, `quality_min_text_chars`, `quality_tracker_id_pattern`, `queue_preview_lines`. |
| PWA | Yes | The answer dialog (label title, answer-text helper line, trade-off lines, context link, recommendation and default lines), queue rows with a preview and **Show more** / **Show less**, and the four settings rows with help links. |
| Android | Yes, not built | Needs the dialog and queue changes above. Tracked in datawatch-app#309. |
| iPhone | Yes, not built | The same as Android. Tracked in datawatch-app#309. |

## Filed issue

- Issue: [dmz006/datawatch-app#309](https://github.com/dmz006/datawatch-app/issues/309), "[sync] Clearer operator questions: trade-offs, quality policy and context link", open.
- Filed on 2026-10-30 as a new issue. The only other open question issue, [datawatch-app#283](https://github.com/dmz006/datawatch-app/issues/283), is the umbrella for the operator question channel and carries a different contract, so it was not reused; the issue body points back to it.
- Issue body: the text between the two body markers above.
