# [sync] Sessions page: placement (system) tag chips drop when a session loses its last placement tag — mobile parity (datawatch app)

**Filed 2026-11-08 (release v9.112.8).** Original note: — this is the
tracked draft; the GitHub issue (or follow-up comment) is opened when the
operator files it. Filing the issue does not change a line of this repo.
This draft is a file write only: it never runs any hosting CLI (no `gh`,
no issue creation) and creates nothing on any host.

**Defect this work addresses** (found 2026-11-08 by the merge, in the docs
set of this release): the operator-facing docs still described the
pre-change rule — "a chip that was drawn stays on the tile until the page
is reloaded" and "the server never sends an empty `system_tags`" — while
the PWA already implements the per-key rule: a row for a session that
carries `system_tags` wins even when it is an empty array, and a row that
omits the key keeps the chip already held. The two rules disagree about a
tile that loses its last placement tag, and the docs now agree with the
code.

## Affected area

The removal half of the placement-chip lifecycle, in the PWA's row merge
(`_mergeSessionFields`, `internal/server/web/app.js`) and the chip draw on
the session card, with the server's per-row-shape contract underneath.
Two shapes define the wire:

- **Summary rows** — the compact rows of `GET /api/sessions?view=summary`,
  the windowed frames and the `session_state` pushes built from the same
  row shape (`sessionSummaryRow`, `internal/server/session_list_page.go`)
  — carry `system_tags` with `omitempty`, so a session with no placement
  tags has **no key at all** in those rows. An empty array never arrives
  in a summary row.
- **Full records** — the session detail, the full list record, the tag
  update response (`sessionTagView`, `internal/server/session_tags.go`) —
  carry `system_tags` **always**, and use `[]` for "no placement tags
  now".

Against those shapes the PWA applies one rule: **a key the row carries
wins, even empty; a key the row omits keeps the held value.**
`internal/server/web/app-session-tile-last-placement-tag.test.js` pins
both directions — a later row with an explicit empty `system_tags`
removes the placement chip from the tile, and a later row that omits the
key keeps it — and the sibling first-load work pins the same
no-key-loses / empty-array-wins merge on the sessions-frame path.

Operator guidance for the surrounding surface lives in
[docs/howto/automata-tags.md](../../howto/automata-tags.md) (the session
chip bullets under *7. Tag a session* and *Shown on the session card*),
the *Placement axes* entry in
[docs/datawatch-definitions.md](../../datawatch-definitions.md), the
session-list section of [docs/operations.md](../../operations.md) (which
also flags the still-unfiled drafts of this family) and
[docs/api/sessions.md](../../api/sessions.md) (the per-shape contract).

## Current behavior (what this work delivers on the PWA)

The PWA implements the per-key rule end to end:

1. **Hold on omission.** A later summary row, windowed frame or
   `session_state` push whose payload omits `system_tags` — which is what
   this server sends for a session with no placement tags, because the
   field is `omitempty` on that row shape — keeps the placement chips the
   page already holds for that session. Sibling sessions are untouched.
2. **Clear on an explicit empty list.** A row for the same session that
   carries `system_tags: []` — the shape full records use when the session
   has no placement tags — removes the placement chip from the tile on
   that update. No page reload is needed: a session that genuinely loses
   its placement (a re-stamp that rewrites the whole `system_tags` list, a
   tag deletion, a placement switch coming off) drops its chip the moment
   a row reporting the placement as gone arrives.
3. **Reload always agrees.** A page reload re-reads the server, so the
   chips on a freshly loaded page match the server's tags exactly.

Nothing on the wire changed with this behavior: the field, its
per-row-shape presence, and the REST/WS contracts are as shipped, and the
visible behavior comes from how the PWA merges a row over the record it
already holds.

The gap this leaves is on the operator-facing mobile clients: a sessions
screen that (a) treats a row that omits `system_tags` as "no placement
tags now" and clears chips the server never cleared, or (b) ignores an
explicit empty `system_tags` and keeps chips the server has cleared,
misrenders both directions of the rule. Neither direction needs a server
change to reproduce or to test, and neither is visible to the datawatch
server.

## Expected behavior

Once parity is met, a sessions screen on Android and iPhone applies the
same per-key rule as the PWA:

1. A live update for a session whose payload omits `system_tags` (the
   shape summary rows use when the session has no placement tags) keeps
   the placement chips that were drawn; other sessions are unaffected.
2. A live update for the same session that carries an explicit empty
   `system_tags` array removes the placement chips on that update — the
   session's full record always carries the field, so this is reachable
   with no server change.
3. At minimum, when the client cannot adopt (1) or (2) verbatim, it must
   not misrender: chips must not disappear on a row that merely omits the
   key, and a row carrying an empty `system_tags` must not leave chips the
   server has cleared.

Capability parity is required; implementation parity is not — each client
may reach "hold on omission, clear on explicit empty" with its own
list architecture and its own row-merge rule. Nothing here requires a new
endpoint, setting, or field: the field and the REST/WS contracts are
unchanged.

## Suggested next step

Confirm against the existing datawatch-app tracker first: the
placement-tag chip surface for the native clients is already tracked by
the sibling drafts of this family — the first-load row shape and the
drawing-discipline drafts, whose acceptance items already include
"an explicit empty `system_tags` array does remove them" — so per the
project's mobile-parity rule the default action is a **follow-up comment**
on that issue rather than a new one, adding this removal item: the rule
(a row that omits `system_tags` keeps the chips; a row that carries it
wins, even empty; full records always carry it, so a session that loses
its last placement tag drops its chips on that update), the pinned PWA
evidence (`internal/server/web/app-session-tile-last-placement-tag.test.js`),
and a pointer at this draft for wording. If that issue is closed, file one
`fix(sessions):`-prefixed issue carrying the same body and referencing the
sibling issue as the parent. If the native sessions screens already apply
both directions, the resolution is a one-line "already met" comment on the
issue, and this draft is closed with that comment as its evidence. Either
way the work on the app side is row-merge and draw logic — a
render-and-merge pass-through story, not an integration story, and there
is nothing in this draft that requires datawatch to ship anything further.

## Parity surface

| Surface | Decision | Why |
|---------|----------|-----|
| REST | included, unchanged | The contracts already express both directions: summary rows `omitempty` (key absent = no update) and full records always carrying the key (`[]` = placement gone). No endpoint, method, or field changed by this work. |
| MCP | included, unchanged | No tool is touched; the session tag inventory tool already exists from the placement-tags work. |
| CLI | included, unchanged | `datawatch sessions` reads the same rows; list output is unaffected by a client-side merge rule. |
| comm channel | included, unchanged | The comm session-listing verbs hit the same endpoints; nothing new flows over the channel. |
| YAML/config | not a target | No configuration is involved — the hold-on-omission and clear-on-explicit-empty rules are display behavior, not a setting; there is intentionally nothing to configure. |
| PWA | included | The Sessions page applies the per-key merge rule and pins both directions in `internal/server/web/app-session-tile-last-placement-tag.test.js` — this draft is the PWA-side record of the removal half of it. |
| Android | excluded (no code in this repo) | Capability parity is required and is tracked by this draft via the sibling issue: hold chips when a row omits the key, drop them when a row carries an explicit empty `system_tags` — implementation parity is not. |
| iPhone/iOS | excluded (no code in this repo) | Same as Android: the same merge-and-draw items are tracked here for the native client. |
