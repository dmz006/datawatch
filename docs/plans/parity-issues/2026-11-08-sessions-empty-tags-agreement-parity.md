# [sync] Sessions: summary row and docs now agree that an empty system tag list is a sent empty array — mobile parity (datawatch app)

**Status: draft, not filed.** This is the tracked draft for the work
"Make the Sessions summary row and its documentation agree about an empty
system tag list." It is a file write only: it never runs any hosting CLI
(no `gh`, no issue creation on a tracker) and creates nothing on any
host. Filing it — as a new issue or as a follow-up comment on the issues
of the same family — is the operator's call.

**Found 2026-11-08** by the merge, in the docs set of this release. Before
the work, the server's own docs described an old per-shape split — summary
rows omitting an empty `system_tags` while only full records sent `[]` —
and the PWA merge rule had to carry two different rules for the two shapes.
The work retired that split on the daemon and aligned every operator-facing
doc with the code that shipped; the one parity gap it leaves is the
app-side clients that still read the same rows.

## Affected area

One field — `system_tags`, the machine-derived placement tags such as
`compute:<node>`, `peer:<name>`, `backend:<llm>`, `lane:<lane>`,
`role:<role>`, `project:<name>` — on the compact Sessions summary row and
on every operator-facing doc that states its empty-list contract:

- **The summary row shape.** `sessionSummaryRow`
  (`internal/server/session_list_page.go`) now declares `SystemTags`
  without `omitempty`, so a summary row for a session with no placement
  tags serialises as `"system_tags": []` — the key is present and the
  value is an empty array, never a missing key and never null. The
  windowed session frames and `session_state` pushes built from the same
  shape, the list records (`internal/server/session_list_view.go`, whose
  `MarshalJSON` always emits the field) and the full record / detail /
  tag-update responses already carried the same rule, so all row shapes
  the server sends now agree with each other.
  `internal/server/session_summary_row_empty_system_tags_test.go` pins the
  always-present, empty-array-when-none form for the summary row and the
  list records.
- **The PWA merge rule.** `_mergeSessionFields`
  (`internal/server/web/app.js`) keeps one rule for both directions: a
  later row that omits the key (a shape the current server does not send,
  e.g. an older deployed daemon) keeps the placement chips already held;
  a later row that carries the key wins, even when it is empty, so a
  session that loses its last placement tag drops the chip on that update.
  `internal/server/web/app-session-tile-last-placement-tag.test.js` pins
  both directions — empty `system_tags` removes the chip while the own and
  inherited chips stay, and a key-less later row keeps it.
- **The operator-facing docs.** `docs/howto/automata-tags.md` (the session
  chip bullets and the placement section), the compact-view section of
  `docs/api/sessions.md`, the *Placement axes* entry in
  `docs/datawatch-definitions.md`, and the session-list section of
  `docs/operations.md` all state the same settled rule — always present,
  an empty list sent as `system_tags: []`, a row that carries the key
  wins even when empty, and a page reload always agrees with the server.
- **The app-side clients (the gap this draft covers).** Android and
  iPhone/iOS read the same REST contract. The sibling drafts of this
  family — the first-load data path
  ([2026-11-08-sessions-system-tags-chips-first-load-parity.md](2026-11-08-sessions-system-tags-chips-first-load-parity.md))
  and the drawing discipline
  ([2026-11-08-sessions-tag-chips-drawing-discipline-parity.md](2026-11-08-sessions-tag-chips-drawing-discipline-parity.md))
  — are tracked in this repo for the app; the removal half
  ([2026-11-08-sessions-empty-system-tags-removal-parity.md](2026-11-08-sessions-empty-system-tags-removal-parity.md))
  still asserts the per-shape split this work retired, and its
  companion draft
  ([2026-11-08-summary-row-empty-system-tags-parity.md](2026-11-08-summary-row-empty-system-tags-parity.md))
  restates the settled contract for exactly that correction.

## Current behavior

The gap this work set out to close is closed on the daemon and in its
docs:

- The summary row, the windowed and `session_state` frames, the list
  records and the full record all send `system_tags`; a session with no
  placement tags is sent as `"system_tags": []`, never as a missing key
  and never as null.
- The four operator-facing docs listed in *Affected area* state that rule
  word for word, so the docs no longer describe a split the server no
  longer makes, and each says a page reload always agrees with the
  server.
- The PWA behaves to the rule: chips are drawn at first load from the
  summary row itself, an empty `system_tags` on a later row removes the
  placement chip on that update, and a key-less row — reachable only from
  an older server — keeps the chips the page already holds.

What the work leaves is the app side, and one wording inconsistency inside
this repo's own tracked drafts:

- Any Android or iPhone sessions screen that was reviewed against the old
  contract — summary rows omitting the key, only full records sending
  `[]` — has acceptance criteria that no longer match the wire. A client
  that special-cased "summary rows never need empty-list handling" is now
  relying on a distinction the server has removed; the sharper, easier
  form is that every row it reads can carry `system_tags`, and an empty
  array on any of them means "no placement tags now".
- The removal-half draft above still says "a session with no placement
  tags has no key at all in those rows. An empty array never arrives in a
  summary row." That sentence is false against v9.112.8 and later. The
  draft's two directions are still the right client behavior, but its wire
  description of *which shape produces which direction* is the retired
  one, so it must not be filed or reused until its wording matches this
  draft.

## Expected behavior

Once parity is met, one rule stated once covers everything:

- `system_tags` is present in every session payload the server sends —
  summary row, windowed and `session_state` frames, list record and full
  record. A session with no placement tags is `"system_tags": []`, never
  a missing key and never null.
- An empty array means "no placement tags now": the row that reports it
  removes the drawn placement chips for that session, on that update — no
  reload needed on the PWA, and required to be possible on the app
  clients.
- A row that omits the key entirely means "no update" and keeps the chips
  the client already holds. Only an older server produces that shape; it
  is a deliberate tolerance, not a contract the clients may depend on.
- A page reload always agrees with the server.

Capability parity is required; implementation parity is not — each client
may reach the rule with its own list architecture and row-merge strategy.
Nothing here requires a new endpoint, setting, or field: the field and
the REST/WS contracts are as shipped.

## Suggested next step

Nothing on the daemon side is open: the behavior is implemented, pinned
by the Go and PWA tests named in *Affected area*, documented in the four
docs listed there, and recorded in `CHANGELOG.md` under v9.112.8. There
is no setting and no migration.

On the app side the operator files one follow-up, not a new feature:

- File this draft as a datawatch-app issue or, per the project's
  mobile-parity rule, as a **follow-up comment** on the same-family
  issues already tracked (the first-load data path and the
  drawing-discipline drafts, tracked against the placement-tags parent).
  The comment quotes the settled rule above — always present, `[]` means
  "no placement tags now" and clears the chips, an omitted key means "no
  update" and is only produced by an older server — names the pinned
  evidence (`internal/server/web/app-session-tile-last-placement-tag.test.js`,
  `internal/server/session_summary_row_empty_system_tags_test.go`), and
  asks for the Android and iPhone sessions-list acceptance criteria to be
  re-stated against it, including the case of a tile that loses its last
  placement tag.
- Before that comment goes out, correct the removal-half draft's wire
  description (or retire it in favour of this draft) so the repo's
  tracked drafts do not ship to the app tracker with two conflicting
  accounts of which row shape sends which direction. `docs/operations.md`
  already points at this correction in its *Operator review still owed*
  note.
- If the native sessions screens already apply the rule, the resolution
  is a one-line "already met" comment on the issue, and this draft is
  closed with that comment as its evidence. Either way there is nothing
  in this draft that requires datawatch to ship anything further.

## Parity surface

| Surface | Decision | Why |
|---------|----------|-----|
| REST | included, unchanged | The contract is the subject and it is settled: every row shape — summary, windowed, `session_state`, list record, full record — always sends `system_tags`, as `[]` when empty. No endpoint, method, or field changed by this draft; the summary-row form is pinned by `session_summary_row_empty_system_tags_test.go`. |
| MCP | included, unchanged | No MCP tool is touched; an MCP session read that surfaces `system_tags` sees the same always-present field of its row shape. |
| CLI | included, unchanged | `datawatch sessions` reads the same endpoints and sees the same always-present field; list printing is unaffected by a client-side merge rule. |
| comm channel | included, unchanged | The comm session-listing verbs hit the same endpoints; nothing new flows over the channel. |
| YAML/config | not a target | No configuration is involved — placement tags stay on by default and the always-present field is a wire decision already shipped, not a setting. |
| PWA | included, already conforming | `_mergeSessionFields` in `app.js` applies the one rule — key-carrying row wins even when empty, key-less row keeps — and `app-session-tile-last-placement-tag.test.js` pins both directions. Listed as the reference implementation the app acceptance criteria are measured against. |
| Android | excluded (no code in this repo) | The gap lives here: capability parity is required (chips at first draw, clear on `[]` from any row shape, keep on a key-less older-server row), implementation parity is not. Tracked by the comment in *Suggested next step*. |
| iPhone/iOS | excluded (no code in this repo) | Same as Android: tracked by the same comment; the native client keeps its own merge rule as long as it meets the client rules in *Expected behavior*. |
