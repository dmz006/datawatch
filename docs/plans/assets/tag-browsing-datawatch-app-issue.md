# DRAFT: issue for dmz006/datawatch-app (not filed)

> **Draft. Do not file yet.** The operator or supervisor files this issue in
> `dmz006/datawatch-app` once the PWA design is approved. Implementation is on
> hold until the server-side tag registry ships.
>
> Source of truth: `docs/plans/2026-10-20-tag-browsing-tile-design.md`
> (sections 1 to 7 and the Parity surface table).

---

**Title:** feat: tag-based browsing and PRD tile redesign (design pending)

---

## Summary

The datawatch PWA is getting a browse-by-tag view for Automata and a redesigned
PRD tile. The Android and iOS apps need the same scope so the three clients stay
at parity. The PWA design is documented in
`docs/plans/2026-10-20-tag-browsing-tile-design.md` in the `dmz006/datawatch`
repository. This issue records the client scope and the REST contract the
clients will parse. It is for planning. Nothing here is ready to build.

## Status

| Surface | Status |
|---|---|
| PWA | Design only. Not started. |
| Server tag registry (prerequisite) | Not shipped. The tag tile cannot show anything meaningful until it ships. |
| Android | Not started. |
| iOS (iPhone and iPad) | Not started. |

Implementation is on hold until the tag registry ships.

## Scope: Android and iOS (iPhone and iPad)

Both platforms get the same scope. The items are listed in build order.

1. **Tag drill-down.** The top level lists root tags. Each row shows the tag
   name, a subtree count (distinct items under the tag), and a chevron if the tag
   has children. An `Untagged` row sits last so no item is hidden. Opening a tag
   shows its child tags as chips with counts, then the items under it, including
   descendants. A "direct only" toggle narrows the list. The breadcrumb reads
   `Tags › Work › Mobile App › iOS`, and each crumb is tappable. The first crumb
   returns to the top level. An item carrying several tags appears under each of
   them, but only once inside any one list. Each row shows an `also in:` line with
   its other tags, and tapping one navigates there.

2. **PRD tiles.** Each PRD becomes a tile built on the existing session-card
   look: the status colour comes from the existing status styling, and the left
   border carries it. The tile shows:
   - title, clamped to two lines;
   - short ID;
   - backend and model line;
   - status chip;
   - progress as `done / total` with a thin bar;
   - up to three tags, with a `+N` chip that opens the full list;
   - last activity as a relative time (for example `updated 4m ago`);
   - actions: `Open` is always shown, then the actions for the current status,
     and an overflow menu holds the rest.

   **Phone layout:** below 720 px the tile collapses to a two-line row. Line one
   is title, status chip and progress count. Line two is backend, tag count and
   last activity. Actions move into the overflow menu. Below 420 px the row is one
   line: title, status dot and `4/9`. The activity time is dropped, and the status
   border stays.

3. **Attention-first section.** A pinned area sits above the tag browser.
   - **Needs attention** (`blocked`, `needs_review`) is expanded. Each row shows
     the blocked reason where one exists.
   - **Running** (`running`) is collapsed to a strip: one line per Automaton, up to
     five, then `+N more`. It shows progress and tag count.
   - Empty groups take no height. When both are empty, the pinned area is hidden.
   - The header shows `Attention (N)` so the switch is visible.
   - **Landing view:** attention when N is at least 1, otherwise the tag browser.
     The choice is made once, at launch, so the view does not change under the
     user. A per-user override lets them pin one view as the default.
   - `paused` is not pinned in v1 (see open questions).
   - An Automaton in the pinned area still appears in its own tag section. The
     pinned area is a view, not a move.

4. **Mobile single-section drill-down.** On phones, only one section is on screen
   at a time. Opening a tag replaces the list with that tag's view, and a back row
   returns to the parent. The tree, counts and breadcrumb come from the same data
   source as the tablet layout. Only the presentation differs.

   **Tablets (iPad and Android tablets):** use the PWA tablet layout. Tag sections
   are collapsible and inline, with nested sub-sections, in a 2-column tile grid.
   When a filter is active, the sections give way to one flat grid. Expansion
   state is kept per user.

5. **Saved filter combos.** A combo is a named set of tag selection with its
   match mode, the status set and the backend set. It applies in one tap from a
   chip row above the filter bar. Combos are stored on the server and synced to
   every client. The client refreshes its chip row when a combo-changed event
   arrives. A combo that references a tag which was renamed or deleted shows a
   "tag no longer exists" state. It is not silently dropped.

   The filter facets (tags, status and backend, with an ANY or ALL match toggle)
   are part of this item. The design allows them to lag one release behind the
   rest. The ANY or ALL toggle needs a visible label, not a hidden default.

**Push:** attention pushes use the existing mobile push topic. No tag-specific
delivery is needed.

## REST contract the clients will parse

These endpoints are proposed. None are implemented yet. Paths and fields come
from the design's Parity surface table and may change before the server ships.

| Method | Path | Purpose |
|---|---|---|
| GET | `/api/autonomous/tags` | Tag tree. Add `?counts=true` for subtree and direct counts. |
| GET | `/api/autonomous/views` | List saved filter combos. |
| POST | `/api/autonomous/views` | Create a combo. |
| PUT | `/api/autonomous/views/{id}` | Update a combo. |
| DELETE | `/api/autonomous/views/{id}` | Delete a combo. |

Notes for the client implementation:

- **Tag tree.** Each tag node carries its name, full path, parent and children.
  With `counts=true` it also carries a subtree count and a direct count. The tree
  is only served where the tag registry exists. If the endpoint is absent, the
  client hides the tag browser and shows the flat list.
- **Automata list items.** Each item gains a `tags` field. The tile also needs
  task counts (`done / total`) on each item. The design expects them in the list
  payload so the client does not fetch progress per card. This is not yet
  confirmed against the live API. If the counts are not in the list payload, the
  design needs a field or a second request. Clients should not assume either.
- **Saved combos.** The fields named in the design are: tag selection with its
  match mode, status set, backend set, and `updated_at`. The design gives the
  conflict behaviour as `409` on version mismatch, and also as last-write-wins
  keyed by `updated_at`. These two conflict. Clients must handle `409` in any case.
  The contract needs to pick one (see open questions).
- **Events.** A save or delete emits an event on the existing event stream. The
  event name is not fixed yet.

## Open questions

These come from the design doc. Each needs an answer before the client work is
planned.

1. **Session tags.** Session cards carry no tag field today. Should sessions
   inherit tags from the Automaton that spawned them, or carry their own? The
   design leaves sessions out of v1.
2. **Counts.** Are subtree counts (distinct items under the tag) wanted, direct
   counts, or both? Both doubles the vocabulary on a phone.
3. **Attention statuses.** Is `needs_review` the only "awaiting a human" status?
   Should `paused` join the attention group?
4. **List payload.** Does the Automata list return task counts and tags on each
   item? Not checked against the live API yet.
5. **Placement.** Does the tag browser live in the existing Automata tab, or in
   the planned Automation tab? The second depends on that tab's own work.
6. **Multi-tag display.** Is the duplicate-across-branches display acceptable, or
   should there be a single primary tag for sorting only?
7. **Live counts.** Do counts refresh on the event stream when a tag changes, or
   only on reload?
8. **Federation.** Tags are local to one node. Renaming a tag on one peer does not
   reach another. Tags must not silently collide in a multi-server view.
9. **Default match mode.** ANY is the default for tag facets. Should ALL be the
   default for multi-tag queries?

---

<!--
OPERATOR NOTES (delete this block before filing)

- Deliberately no tracker IDs (BL numbers, GH numbers) in this draft. The design
  doc holds them. The public issue should read as standalone.
- Design conflict to resolve before filing: section 6 says last-write-wins keyed
  by `updated_at`; the Parity surface REST row says `409` on version mismatch.
  The draft leaves both visible.
- Unverified in the design: the tag-registry source. Its MCP tools
  (`autonomous_tag_*`) have no Go implementation in this worktree (design open
  question 10). Confirm the registry exists in a shipped build before filing.
- Android and iOS parity issues are filed by the implementation PRD, per the
  design's Parity surface table. This draft is the planning version only.
-->
