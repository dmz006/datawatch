# datawatch-app parity issue — spec/description writes: truncation-marker refusal and full reads (FILED)

This is a ready-to-file draft for a sync issue on **datawatch-app**. It covers the
datawatch main behaviour that the Android and iOS clients must match when they read,
write or display Automaton specs, story descriptions and task specs: the capped-view
contract (which fields are always whole, which are capped, the marker, the `truncated`
array, the full-text reads), and the guardrail on every spec/description write that
refuses text carrying the truncation marker — a copy pasted from a capped view, even one
edited after the cut.

Do not run `gh` from here and issue filed: https://github.com/dmz006/datawatch-app/issues/347; was blank — the supervisor files it
at the merge and records the URL back into this file.

---

## Title

`feat(parity): spec and description writes — refusals and full reads`

## Body

A capped view of an Automaton ends its heavy text fields with the marker
`…[truncated: kept N of M bytes]`. Saving spec or description text copied from such a
view silently drops its tail, so datawatch main now refuses it before anything is
stored — over REST, over MCP, from the CLI and from the comm verbs. The uncapped text
is always one read away. The clients need the read contract, the refusal, and the
strings, so an operator editing a spec on a phone sees the same behaviour as on the
PWA/CLI: an obvious marker, a one-tap full read, and a hard refusal instead of a
silently truncated save.

### The fields and the cap

Always whole on every read (never capped): the Automaton `spec`, every story
`description`, every task `spec`.

Capped to `autonomous.detail_text_cap_bytes` (default 4096 bytes, 0 = default,
negative = off): the Automaton's `memory_report`, and per task `error`,
`prior_attempt_hint`, `start_failure_tail[]`, `attempt_refs[]`, verdict and verifier
text. Each capped field ends with the marker, is named in the record's `truncated`
array, and the array carries the URL of the full text for each entry.

### Full reads (the escape hatch)

- `GET /api/autonomous/prds/{id}?full=1` — the whole Automaton, uncapped.
- `GET /api/autonomous/prds/{id}/outputs` — the PRD's heavy output fields, uncapped.
- `GET /api/autonomous/prds/{id}/tasks/{task_id}/outputs` — one task's heavy output
  fields (error, `prior_attempt_hint`, verifier verdict text), uncapped. The task's own
  `spec` is whole in every read.

### Refusal contract (every spec/description write)

`POST /api/autonomous/prds` (create), `PATCH /api/autonomous/prds/{id}` (PRD edit),
`PATCH /api/autonomous/prds/{id}/tasks/{task_id}` (edit task `spec`),
`POST /api/autonomous/prds/{id}/edit_story`, `POST /api/autonomous/prds/{id}/add_story`,
`POST /api/autonomous/prds/{id}/add_task`, `POST /api/autonomous/prds/{id}/resolve_task_review`
(retry with a replaced `spec`), `POST /api/autonomous/templates` and
`PUT /api/autonomous/templates/{id}`.

Any `spec` / `description` / `new_spec` / `new_description` value containing the marker
`…[truncated: kept ` is refused with 400 before anything is stored. The message names
the field and says what to do instead:

`<field>: contains a truncation marker — it was copied from a capped view; read the full text and edit that`

Client behaviour:

1. Detect locally: before sending a spec/description write, refuse values containing
   the marker substring and show the same operator-facing message — do not send a
   request the daemon will only refuse.
2. Surface the daemon's 400 verbatim when the server rejects (older daemon, or a value
   the client did not catch) — no retry, no silent trim.
3. In editors and viewers, mark capped fields (marker present, or the field named in
   `truncated`) and offer a full read via `?full=1`, `/outputs` or the per-task
   `/outputs` route; a spec/description editing surface must load from a full read
   before saving, not from the capped view.

### English strings to add (wording fixed; key names follow datawatch-app convention)

- Refusal (per field): "contains a truncation marker — it was copied from a capped view;
  read the full text and edit that" (prefixed with the field name on display).
- Capped-field badge/label: "truncated" (the marker itself reads
  "…[truncated: kept N of M bytes]" and should be surfaced, not hidden).
- Full-text action: "Open full text" / "Show more".
- No-marker guard for the create/edit forms where the client detects early.

### Translations requested

Please add DE, ES, FR and JA translations of the English strings above for each client.
The refusal message is the priority: it is the operator-facing guard, and the PWA
surfaces it in English from the daemon's 400 body — the clients should be able to say
it natively when they refuse locally.
