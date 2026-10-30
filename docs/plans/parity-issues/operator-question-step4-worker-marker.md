# [sync] Worker questions: blocking questions, the await_decision wait and the decision link

Parity issue for dmz006/datawatch-app. It covers Android and iPhone/iOS together, so no platform tag is used. The text between the two body markers below is the issue body; the sections after the end marker record the filed issue.

**The GitHub issue is not filed yet.** It is filed only when the operator says so. Until then this file is the tracked parity record, and the "Filed issue" section below stays empty.

<!-- body-start -->
## Daemon version

Lands in the next minor release above main at the time the change merges. The mobile apps should gate the feature on the daemon reporting that release or later.

## What changed in datawatch

A worker session of an Automaton can now ask the operator a design question by printing one `DATAWATCH_QUESTION:` line (followed by one line of JSON) in its output. The daemon stores it as an ordinary operator question, announces it the way every question is announced, and, when the worker says it is blocked, parks the task on it until the operator decides. The contract changes are four: three new fields on the question record, a new task wait reason, one new config key, and a link from an answer entry in the Automaton decision log to the answer. No route and no MCP tool is added.

## REST contract

### New question fields

On `GET /api/questions` and `GET /api/questions/{id}` (the question record). All three are left out when empty or false, so a record written before they existed reads as before.

| Field | Type | Notes |
|---|---|---|
| `blocking` | boolean | The asker cannot go on until the question is decided. A question raised with `POST /api/questions` is never blocking. |
| `source` | string | `marker` (a worker's marker line) or `rest` (`POST /api/questions`). Empty on an old record. Treat an unknown value as plain text. |
| `source_line_sha256` | string | Lowercase hex SHA-256 of the marker line. A dedupe key only; show it at most as secondary detail. It is never the line text. |

A worker question is asked by `worker-<short session id>` (`asker_kind` `session`), carries the worker's `session_id`, is linked to its task (`context` `task`), has `priority` `normal` and `return_path` `false`.

### Wait reason `await_decision`

A task parked on an open blocking question is a waiting task (the same waiting state as `waiting_capacity`) with `wait_reason` `await_decision: <question id>`. The `cause` on the waiting-task list of the capacity status (`GET /api/capacity`, `tasks.waiting[].cause`) has a new value, `await_decision`, beside `await_external`. The task returns to running by itself after the question is answered or withdrawn. Treat it as an active, non-terminal wait, and render an unknown `cause` as plain text.

### Config key

Under `autonomous.questions` in `GET`/`PUT /api/config`.

| Key | Type | Default | Meaning |
|---|---|---|---|
| `autonomous.questions.worker_marker` | boolean | off when the key is absent | Whether a marker line in a worker's output is raised as a question. It changes nothing while `autonomous.questions.enabled` is off. Read live; no restart. |

### Decision link

An entry of kind `operator_answer` in the Automaton decision log (the `decisions` array on `GET /api/autonomous/prds/{id}`) already carries `question_id` and `answer_id`. The PWA now shows the `answer_id` as a link. Opening it fetches `GET /api/questions/{question_id}/answers` and shows the answer whose `id` matches: the quote verbatim with its line breaks, the chosen option id, the actor and the time. If the answer is not in the list, or the fetch fails, the panel says so in one line.

### Audit action

`question_marker_refused` is written when a marker line fails validation and no question is stored. It carries the session id, a reason category and the SHA-256 of the line, never the line text. The apps do not need to render it.

Locale keys the PWA added (English values; the other locales carry placeholders): `settings_automata_questions_worker_marker`, `settings_automata_questions_worker_marker_help`, `decision_answer_link`, `decision_answer_panel_quote`, `decision_answer_panel_option`, `decision_answer_panel_actor`, `decision_answer_panel_time`, `decision_answer_panel_loading`, `decision_answer_panel_failed`, `decision_answer_panel_missing`.

## What the apps should render

- The Automata settings card gets a toggle for `autonomous.questions.worker_marker` next to the other operator question toggles. Absent reads as off. A tooltip or help line explains that a worker can ask a question with a marker in its output, the task waits for the answer without using a retry, and that with the toggle off such markers are ignored.
- A task whose `wait_reason` starts with `await_decision` shows as waiting for a decision, with the question id from the reason. The question itself is in the operator question queue.
- In the decision log of an Automaton, an `operator_answer` entry that has both `question_id` and `answer_id` shows the answer id as a tappable link that opens a small panel with the quote, the option, the actor and the time. The quote is text, never markup, and is not trimmed or cut.
- Where the question queue shows a question, `blocking: true` may be shown as a small marker (the question holds a task). `source` and `source_line_sha256` need no display.

## Acceptance criteria

### Android

- [ ] The Automata settings card has a toggle for `autonomous.questions.worker_marker` (absent reads as off), saved through `PUT /api/config`.
- [ ] A task waiting with `wait_reason` `await_decision: <id>` renders as a waiting task and not as finished or failed; an unknown capacity `cause` does not crash the screen.
- [ ] An `operator_answer` entry with both ids shows the answer link; tapping it opens the quote panel from `GET /api/questions/{id}/answers`; a failed fetch or a missing answer shows its own line.
- [ ] The quote renders verbatim with line breaks kept and long unbroken text wrapped.
- [ ] A question record with or without `blocking`, `source` and `source_line_sha256` parses.
- [ ] The new locale keys exist in all five locales.
- [ ] Layout holds at 360 dp width with a 20+ character id.

### iPhone/iOS

- [ ] Same list as Android, checked at the narrowest supported iPhone width.
- [ ] Dynamic Type at the largest size keeps the link and the quote readable without horizontal scroll.
- [ ] VoiceOver reads the answer link as a button and announces the panel when it opens.

## Ships off

The worker marker ships off, and does nothing while the question channel is off. So an app will not see a `source: "marker"` question, a `blocking: true` question or an `await_decision` wait until an operator turns both `autonomous.questions.enabled` and `autonomous.questions.worker_marker` on. Build and test against a daemon with both on.
<!-- body-end -->

## Parity surface

Every surface the change could reach, and whether it changes. Where it does not change, the reason is given.

| Surface | Changes? | Detail or reason |
|---|---|---|
| REST | Yes | Three new fields on the question record (`blocking`, `source`, `source_line_sha256`), the `await_decision` value for the capacity wait `cause`, and the new config key on `GET`/`PUT /api/config`. No new route: a worker question is read and answered on the existing `/api/questions` routes. |
| MCP | Config only | No new tool. `question_list` and `question_get` return the question record, so the new fields appear there as they do on REST, and the key is set through the generic `config_set` and read with `get_config`. A marker is raised from a session's output, so a `question_raise` tool call is not part of it. |
| CLI | Config only | No new verb or flag. The key is set with `datawatch config set autonomous.questions.worker_marker true`. `datawatch autonomous decisions --json` prints the daemon's response unchanged, so any new decision fields appear there; the CLI has no question-answer verb, and this step adds none. |
| Comm channel | Config only | No new command. The key is set with `configure autonomous.questions.worker_marker=true`. The comm channel has no question-answer verb or bare-reply rule, and this step adds none; raising is done by the worker's marker, not by a chat verb. |
| YAML/config | Yes | New key `autonomous.questions.worker_marker` (boolean, off when absent, read live), documented in the config reference. |
| PWA | Yes | A settings toggle for the key, and the answer link with its quote panel on `operator_answer` entries in the Automaton decision log. |
| Android | Yes, not built | Needs the settings toggle, the waiting-for-decision display and the answer link. Tracked in this record; the issue is filed when the operator says so. |
| iPhone | Yes, not built | The same as Android. Tracked in this record; the issue is filed when the operator says so. |

## Filed issue

Not filed. The GitHub issue for dmz006/datawatch-app is filed only when the operator says so. When it is filed, record here the issue number, the URL, the title and the exact filed body text, as the other parity records do, and update the Android and iPhone rows in `docs/parity-status.md`.
