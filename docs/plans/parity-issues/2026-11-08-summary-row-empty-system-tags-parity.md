# [sync] Sessions page: placement (system) tag chips — the summary row never sends an empty `system_tags`, so the "explicit empty array removes the chip" rule in the app parity drafts is wrong (datawatch app)

**Status: draft, not filed.** This is the tracked draft for a follow-up comment on
the two datawatch-app issues already opened for the same Sessions chips work
(see *Suggested next step*). It is a file write only: it never runs any hosting
CLI (no `gh`, no issue creation) and creates nothing on any host. Filing the
comment is the operator's call.

**Found 2026-11-08** while closing the docs for the Sessions summary row: the
docs set in commit `73dc282f5` (merged as `1eb459f59`) states that the summary
row omits an empty `system_tags` and that a chip is cleared only by a page
reload. Re-reading the code for that change showed the statement holds for the
summary row, but the docs and the two earlier parity drafts generalise it to
every session payload, and the code does not support that.

## Affected area

Three things sit on the same field, `system_tags` (the machine-derived
placement tags such as `compute:<node>`, `peer:<name>`, `backend:<llm>`,
`lane:<lane>`, `role:<role>`, `project:<name>`):

1. **Summary rows** of `GET /api/sessions?view=summary`, and the windowed
   and `session_state` rows built from the same shape. The field is declared
   `omitempty` in `sessionSummaryRow` (`internal/server/session_list_page.go`,
   around line 63), so a session with no placement tags serialises with no
   key at all. An empty array is never sent on this path.
2. **Full session records.** `sessionTagView` (`internal/server/session_tags.go`,
   around line 91) declares `system_tags` *without* `omitempty` and its comment
   says the field is "always present". `sessionView` builds it for the session
   detail response (`internal/server/api.go`, around line 4229), the session
   list in `session_tags.go` (around line 110), and the session-tag update
   response (around line 175). So a session with no placement tags is sent as
   `"system_tags": []` on those responses. An empty array *is* sent on this
   path.
3. **The PWA merge rule** in `internal/server/web/app.js` (around lines
   3158–3173). For a full record (the `!cur || 'task' in row` branch) the new
   row replaces the held one, so an empty `system_tags` on that row replaces
   and clears the held chips. For a summary or partial row, a missing key keeps
   the held chips, and the code deletes `system_tags` from the incoming row so
   the held value survives. The only place an explicit empty array can win is
   the full-record branch, which is the one the summary-row wording never
   covered.

The documentation that describes this surface is
[docs/howto/automata-tags.md](../../howto/automata-tags.md) (the Sessions chip
bullets and the inherited-tag section, around lines 253–260 and 495–499),
[docs/api/sessions.md](../../api/sessions.md) (around line 75), the *Placement
axes* entry in [docs/datawatch-definitions.md](../../datawatch-definitions.md),
and the session-list section of [docs/operations.md](../../operations.md)
(around lines 1620–1650). The app-side drafts that repeat the wording are
[2026-11-08-sessions-system-tags-chips-first-load-parity.md](2026-11-08-sessions-system-tags-chips-first-load-parity.md)
and [2026-11-08-sessions-tag-chips-drawing-discipline-parity.md](2026-11-08-sessions-tag-chips-drawing-discipline-parity.md).

## Current behavior

On the wire, the summary row is correct as documented: an empty placement set
produces no `system_tags` key, and the docs now say so. The docs then go wrong
in three places:

- `docs/howto/automata-tags.md` and `docs/operations.md` say, in general terms,
  that the server never sends an empty `system_tags` and that an empty list is
  never sent. That is false for the session detail, session list and
  session-tag update responses, which send `"system_tags": []`.
- Both app parity drafts (linked above) still say that only an *explicit empty*
  `system_tags` array removes a placement chip. On the summary path that case
  cannot occur, so the rule they describe never fires there. On the
  full-record path it does fire, through the replace branch in the PWA merge,
  which the drafts do not mention.
- The docs say a drawn chip clears only on a page reload. That holds for summary
  and partial rows. It does not hold for a full record that carries an empty
  array, which clears the chip on that update.

The operations doc already records that this is an open decision and not
something the docs change makes: it says that clearing a chip live would mean
sending an empty array on the summary row, which changes the wire contract and
is a code change. That open decision is still open. The statements above
make the operator's options look more settled than they are, and the general
claim in `automata-tags.md` and `operations.md` needs narrowing before the
docs are relied on.

## Expected behavior

The docs, the server and the clients should agree on one rule, stated per
payload:

- The summary row (`view=summary`, and the windowed and `session_state` rows
  built on it) omits `system_tags` when the set is empty. A missing key keeps
  whatever placement chips the page already holds, and a chip that was drawn
  clears only on page reload or when a row carries a non-empty list.
- The full session record, the session detail response and the session-tag
  update response send `system_tags` always, including as `[]`. An empty array
  there clears the placement chips for that session.
- The app parity drafts say the same thing per payload: "explicit empty array
  removes the chip" is true for full records and never happens on summary rows.
- The mobile clients meet the display rule for each payload they actually read.
  A client that reads only summary rows keeps placement chips on partial pushes
  and needs no empty-array handling. A client that reads full records treats an
  empty `system_tags` as "no placement tags now".

Capability parity is required; implementation parity is not. Each client may
keep its own merge strategy as long as chips are present on first draw and
change only as the payload they read changes.

## Suggested next step

The operator decides, in one of two ways. Option A keeps `omitempty` on the
summary row, which leaves a chip cleared only by reload on that path. The docs
then need the per-payload wording above in `automata-tags.md`, `operations.md`,
`api/sessions.md` and the glossary, and the two app drafts need the same
correction. Option B sends an empty array on the summary row so a live update
can clear a chip. That is a wire-contract change and a code change, so it
needs an explicit go-ahead; it also means checking the windowed and
`session_state` paths and the PWA's partial-row rule (which currently deletes
the key when it is absent) together. Until the operator picks one, the docs
should keep the open-decision note and drop the general "never sends an empty
list" sentence in favour of the per-payload statement.

Whichever option is chosen, the datawatch-app side is a follow-up comment, not a
new issue, on the two issues already filed from the earlier drafts (#401 for the
first-load data path and #402 for the drawing discipline) and on the placement
tags parent issue #394 they reference. The comment should quote the corrected
rule per payload and link this draft. If the operator picks Option B, the
comment also asks the app to handle an empty summary `system_tags` as a clear.
The app drafts' acceptance criteria and their parity surface tables do not
change beyond that wording.

## Parity surface

| Surface | Decision | Why |
|---------|----------|-----|
| REST | included | The contract is the subject. Summary rows, windowed and `session_state` rows omit `system_tags` when empty (`omitempty`); the session detail, session list and session-tag update responses always send it, including `[]`. The docs must say which payload does which. No endpoint or field changed by this draft. |
| MCP | included, unchanged | No MCP tool wraps the sessions summary list, and nothing new is exposed. Whether an MCP session read shows the always-present `system_tags` of the full record is not checked in this draft and is not changed by it. |
| CLI | included, unchanged | `datawatch sessions` reads the same endpoints. Whether it prints an empty placement list is not part of this draft and is not changed. |
| comm channel | included, unchanged | Session listing verbs go through the same endpoints. No comm-side change is needed for the wording fix. |
| YAML/config | not a target | No configuration is involved. Placement tags stay on by default and the choice between Option A and Option B is a wire decision, not a setting. |
| PWA | included | The merge rule in `app.js` treats an empty array on a full record as a clear and a missing key on a partial row as a keep. The docs and the summary-path behaviour must match it. Option B would also change that rule. |
| Android | excluded (no code in this repo) | Capability parity is tracked by the existing issues #401, #402 and #394 and by the comment in *Suggested next step*. Implementation parity is not required. |
| iPhone/iOS | excluded (no code in this repo) | Same as Android: tracked by the same issues and comment; the native client picks its own merge rule. |
