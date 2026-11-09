# [sync] Sessions page: summary rows now always send `system_tags`, with an empty array meaning "no placement tags now" — mobile parity (datawatch app)

**Status: draft, not filed.** This is the tracked draft for the app-side half of
the Sessions summary-row work. It is a file write only: it never runs any
hosting CLI (no `gh`, no issue creation on a tracker) and creates nothing on
any host. Filing it — as a new issue or as a follow-up comment on the issues
of the same family — is the operator's call.

**Found 2026-11-08** by the merge, in the docs set of the work "Make the
Sessions summary row and its documentation agree about an empty system tag
list". Earlier drafts and docs of this family described an old per-shape
split — summary rows omit an empty `system_tags`, full records alone send
`[]` — and framed "keep omitting vs. send empty" as an open operator
decision. That decision is now settled in the code and the shipped docs:
every row shape the server sends carries `system_tags`, and the open
question this draft used to pose is no longer open. This draft restates the
settled contract for the clients that still read it and records the one
parity gap the work leaves: the app-side clients.

## Affected area

One field, `system_tags` (the machine-derived placement tags such as
`compute:<node>`, `peer:<name>`, `backend:<llm>`, `lane:<lane>`,
`role:<role>`, `project:<name>`), on every session row shape the server
sends, and on each operator-facing client that reads those rows:

- **Server row shapes.** The compact summary row of
  `GET /api/sessions?view=summary` (`sessionSummaryRow`,
  `internal/server/session_list_page.go`) now declares `system_tags`
  without `omitempty`, so a session with no placement tags serialises as
  `"system_tags": []` — never as a missing key and never as null. The
  windowed session frames and `session_state` pushes built from the same
  shape (`internal/server/ws_session_stream.go`), the list record and
  bounded record paths (`internal/server/session_list_view.go`, whose
  `MarshalJSON` always emits the field), and the full record, session
  detail and tag-update responses all agree on the same rule.
- **Tests that pin the contract.**
  `internal/server/session_summary_row_empty_system_tags_test.go` asserts
  the summary row, the `Hub.summaryRow` window frame and the list records
  all send `system_tags` as `[]` (key present, not null) for nil and empty
  tag sets, and still in full when non-empty. On the PWA,
  `internal/server/web/app-session-tile-last-placement-tag.test.js` asserts
  a tile that loses its last placement tag drops the chip on the update
  that reports it gone.
- **Documentation.** `docs/howto/automata-tags.md` (the Sessions chip
  bullets and the placement section), `docs/api/sessions.md` (compact view
  section), the *Placement axes* entry in
  `docs/datawatch-definitions.md`, and the session-list section of
  `docs/operations.md` all state the same settled rule, and each notes
  that a page reload always agrees with the server.
- **The app-side clients (the gap this draft covers).** Android and
  iPhone/iOS read the same REST contract. The earlier drafts of this same
  family — the first-load data path
  ([2026-11-08-sessions-system-tags-chips-first-load-parity.md](2026-11-08-sessions-system-tags-chips-first-load-parity.md))
  and the drawing discipline
  ([2026-11-08-sessions-tag-chips-drawing-discipline-parity.md](2026-11-08-sessions-tag-chips-drawing-discipline-parity.md))
  — were tracked against the app tracker in the v9.112.8 window; this
  draft is the follow-up for the empty-list half of the contract.

## Current behavior

On the datawatch side, the gap this work set out to close is closed:

- The server sends `system_tags` in every session row shape. A session with
  no placement tags is sent as `"system_tags": []`; the field is never
  absent and never null on any shape — summary row, windowed frame,
  `session_state` push, list record or full record.
- The docs agree with the code, word for word where they state the rule:
  "always present … an empty list sent as `system_tags: []` (never a
  missing key and never null)".
- The PWA's merge rule matches: a row that carries the key wins even when
  it is empty, so a session that loses its last placement tag drops the
  chip on that update; a row that omits the key — a shape the current
  server does not send, e.g. an older deployed daemon — is treated as "no
  update" and keeps the chips the page already holds.

What the work leaves is app-side:

- The earlier drafts of this family were written before the summary row
  started always carrying the field. The removal-half draft
  ([2026-11-08-sessions-empty-system-tags-removal-parity.md](2026-11-08-sessions-empty-system-tags-removal-parity.md))
  still stated the old per-shape split — "a session with no placement
  tags has no key at all in those rows. An empty array never arrives in a
  summary row" — a sentence that is false against v9.112.8 and later, so
  it was not filed; it has since been retired in favour of a corrected
  draft at the same path that carries the settled rule this draft states.
  The first-load data-path draft likewise still describes the field as
  `omitempty` on summary rows and needs the same correction before filing.
- Any app client that was built or reviewed against the old contract —
  summary rows omitting the key and only full records sending `[]` — has
  acceptance criteria that no longer match the wire. The chip-lifecycle
  rule itself was easy to implement correctly, but a client that special-cased
  "summary rows never need empty-list handling" is now relying on a
  contract that has a sharper, easier form to meet.

## Expected behavior

One rule, stated once, that the server, the docs and every client already
agree on (or are asked to agree on):

- `system_tags` is always present in every session payload that carries
  placement tags — the summary row, the windowed and `session_state`
  frames, the list record and the full record. A session with no placement
  tags is sent as `"system_tags": []`, never as a missing key and never
  as null.
- An empty array means "no placement tags now": the row that reports it
  removes the drawn placement chips for that session, on that update — no
  reload needed, on the PWA or required to be possible on the app clients.
- A row that omits the key entirely means "no update" and keeps the chips
  the page already holds. Only an older server produces that shape; it is
  a deliberate tolerance, not a contract the clients may depend on.
- A page reload always agrees with the server.

Capability parity is required; implementation parity is not. Each client
may keep its own row-merge strategy as long as chips are present at first
draw, an empty `system_tags` on any row shape it reads clears them, and a
key-less row (older server) never adds chips.

## Suggested next step

Nothing on the daemon side is open: the behavior is implemented, pinned by
the Go and PWA tests above, documented in the four docs listed in *Affected
area*, and recorded in `CHANGELOG.md` under v9.112.8. There is no setting
and no migration to make.

On the app side the operator files one follow-up, not one new feature:

- **File this draft as a datawatch-app issue or comment** in the same
  family as the two already-tracked drafts (the first-load data path and
  the drawing discipline, tracked in the v9.112.8 window against the
  placement-tags parent issue). The comment quotes the settled rule above —
  always present, `[]` means "no placement tags now" and clears chips,
  omitted key means "no update" and is only produced by older servers — and
  asks for the Android and iPhone sessions-list acceptance criteria to be
  re-stated against it, including the case of a tile that loses its last
  placement tag.
- **The conflicting wire description no longer exists in the tracked
  drafts.** The removal-half draft that asserted the per-shape split v9.112.8
  retired has been retired in favour of a corrected draft at
  [2026-11-08-sessions-empty-system-tags-removal-parity.md](2026-11-08-sessions-empty-system-tags-removal-parity.md)
  carrying the same per-rule wording this draft has, so this file and that
  file no longer conflict. The first-load data-path draft still describes
  the summary-row field as `omitempty` and should get the same correction
  before filing; `docs/operations.md`'s *Operator review still owed* note
  already flags the family's pre-filing corrections.
- No further datawatch changes are needed for this gap. If the operator
  later wants a client-visible affordance for the empty state (for example a
  dimmed placeholder where chips would be), that is a separate app-side
  question and does not change the contract in this draft.

## Parity surface

| Surface | Decision | Why |
|---------|----------|-----|
| REST | included | The contract is the subject and it is settled: every row shape — summary, windowed, `session_state`, list record, full record — always sends `system_tags`, as `[]` when empty. No endpoint changed by this draft; the field's presence is pinned by `session_summary_row_empty_system_tags_test.go`. |
| MCP | included, unchanged | No MCP tool wraps the sessions summary list and nothing this work exposes. An MCP session read that surfaces `system_tags` sees the always-present field of its row shape; this draft changes nothing there. |
| CLI | included, unchanged | `datawatch sessions` reads the same endpoints and sees the same always-present field. Printing an empty placement list is not part of this draft and not changed by it. |
| comm channel | included, unchanged | Session listing verbs go through the same endpoints. No comm-side change is needed for the contract statement. |
| YAML/config | not a target | No configuration is involved. Placement tags stay on by default; the always-present field is a wire decision already made and shipped, not a setting. |
| PWA | included, already conforming | The merge rule in `app.js` treats a key-carrying row as winning even when empty and a key-less row as a keep; `app-session-tile-last-placement-tag.test.js` pins the clear-on-empty direction. Listed so the table names the reference implementation the app acceptance criteria are measured against. |
| Android | excluded (no code in this repo) | The gap lives here: capability parity is required (chips on first draw, clear on `[]`, keep on a key-less older-server row), implementation parity is not. Tracked by the comment in *Suggested next step*. |
| iPhone/iOS | excluded (no code in this repo) | Same as Android: tracked by the same comment; the native client keeps its own merge rule as long as it meets the three client rules above. |
