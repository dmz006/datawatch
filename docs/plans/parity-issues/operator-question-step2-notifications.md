# Operator-question Step 2: notifications — parity comment for datawatch-app #283

The switch for the channel (`autonomous.questions.enabled`) is Step 1 and already ships
off. This note documents Step 2 — the notification surfaces the app must handle and the
three toggles that gate them. All of it is gated on that one switch: with it off, none
of this fires (no WS message, no alert, no session line).

## Comment body (posted verbatim to datawatch-app #283)

### [2026-10-29] Operator-question Step 2: notifications surfaced for the operator

datawatch now notifies on the operator-question channel: when the Automaton raises a
question, when it is answered, and when it is withdrawn. Everything below shares one
switch that ships **off** — `autonomous.questions.enabled` — and with that switch off none
of it fires. The three notification toggles sit beside it and each defaults **on** when
the key is absent.

### New event kinds

Two lifecycle event kinds ride the existing lifecycle channel (the same bus the
`task.looping` event uses; one event per answer and one per withdrawal, replayed and
reconciled at daemon start):

- `question.answered` — an open question gained an answer (first or a superseding one).
  It names the question and the answer record it names. A question never expires by a
  clock, so there is no `question.expired` kind and none will ever be added.
- `question.withdrawn` — an open question was withdrawn before an answer. It names the
  question; the answer field is empty.

Treat both as "a question you may be following just changed state" — refresh any
badge/summary that tracks open questions. Neither changes a task status.

### New WebSocket message type: `question_raised`

Sent to all connected clients exactly once per successful raise, best-effort and off the
request path (a slow or failing part cannot delay or fail the raise). Envelope:

```json
{
  "type": "question_raised",
  "data": {
    "id": "...",
    "label": "...",
    "text": "...",
    "options": [ { "id": "...", "label": "...", "tradeoffs": "..." } ],
    "priority": "normal",
    "context": "..."
  }
}
```

`id` and `text` are always present; `label`, `options`, `priority`, and `context` are
omitted when empty. This is the real-time return path for a raised question — it removes
the need to wait on the polled GET below, but both are available at once.

### Polled GET (answer surface)

`GET /api/questions/{id}` returns:

```json
{
  "question": {
    "id": "...", "prd_id": "...", "task_id": "...", "session_id": "...",
    "state": "open", "asker_kind": "...", "context_kind": "...", "risk": "...",
    "priority": "normal", "return_path": true,
    "text": "...", "label": "...", "context": "...",
    "options": [ { "id": "...", "label": "...", "tradeoffs": "..." } ],
    "created_at": "...", "updated_at": "..."
  },
  "answer_count": 1,
  "current_answer_id": "...",
  "answer": {
    "id": "...", "chosen_option_id": "...", "quote": "...",
    "actor": "...", "surface": "...", "created_at": "..."
  }
}
```

`answer` is `null` while the question is still open (or was withdrawn). `answer_count` is
the number of answers recorded; the last one is the current one, and an answer
supersedes the previous one.

### Three new config keys

Beside the existing `autonomous.questions.enabled` switch (off by default), the config
now exposes three notification toggles under the same `autonomous.questions` object. All
three default **on** when the key is absent; an explicit `false` survives a round trip and
a config write (none of the fields has `omitempty`); and they are read live, so a change
takes effect on the next raise or answer. In `GET /api/config` the block reports
`enabled` plus each toggle's **effective** value:

```json
{ "autonomous": { "questions": {
  "enabled": false,
  "notify_messaging": true,
  "notify_alert": true,
  "return_path": true
} } }
```

| Key | Type | Default (absent) | What it controls |
|-----|------|------------------|------------------|
| `autonomous.questions.notify_messaging` | bool | on | notify the operator (messaging group) over a raised question; off = no messaging notification |
| `autonomous.questions.notify_alert` | bool | on | fire an alert for a **high-priority** question; off = high priority no longer alerts (the `question_raised` WS message and the polled GET still carry it) |
| `autonomous.questions.return_path` | bool | on | permit the per-question opt-in session line (below); the default return path — the event plus the polled GET — is always there regardless |

Add the three as boolean settings beside the existing `enabled` switch.

### The `DATAWATCH_ANSWER:` session line

When a question was raised **with** `return_path: true` and both switches are on, an
answer recorded against it is additionally delivered into the asking session as exactly
one line of compact JSON with a fixed marker:

```
DATAWATCH_ANSWER: {"question_id":"...","answer_id":"...","chosen_option":"...","quote":"...","actor":"...","at":"..."}
```

All values are JSON strings; `at` is RFC 3339 UTC.

- `question_id` — the question the answer is against
- `answer_id` — the answer record
- `chosen_option` — the chosen option id (empty when the operator answered in free text)
- `quote` — the operator's text exactly as recorded (control bytes are JSON-escaped, so
  the line is always a single physical line)
- `actor` — who answered
- `at` — when, RFC 3339 UTC

The marker is distinct from, and is never read as, the existing `DATAWATCH_COMPLETE:` and
`DATAWATCH_NEEDS_INPUT:` markers. Delivery is best-effort and can never block or fail the
answer — the event and the polled GET still carry the answer either way. If the app
renders the asking session's output it may see this line; treat it as an informational
marker, not a session completion and not an input request.

### Acceptance

- With the switch on and default toggles: raising a high-priority question surfaces one
  `question_raised` WS message; recording an answer (or a superseding one) emits one
  `question.answered`; withdrawing an open question emits one `question.withdrawn`.
- With the switch off: none of the above fire.
- Setting `notify_alert` off stops the high-priority alert while the `question_raised`
  WS message and the polled GET still carry the question.
- The three toggles show their effective value under `autonomous.questions` in
  `GET /api/config`; an explicit `false` comes back out verbatim.

## Filing record

- Comment URL: https://github.com/dmz006/datawatch-app/issues/283#issuecomment-6494958004
- Posted: 2026-10-29 11:05 UTC (datawatch-app issue 283)
- Via: `gh issue comment 283 -R dmz006/datawatch-app --body-file /tmp/dw-qstep2-body.md`
- Note: the capture+display double-run briefly created a duplicate
  (comment 6494958355), which was deleted; exactly one comment remains and its body is
  byte-identical to the section above (verified via the public comments API).
