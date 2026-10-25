# Tag-browsing tile — design

**Date:** 2026-10-20
**Version:** v9.5.28 (read from `var Version` in `internal/server/api.go`)
**Status:** Design only. On hold until the tag registry ships (BL393, see Dependencies).

This note records how the PWA lists sessions, PRDs and Automata today, and what a
browse-by-tag tile would need from the tag registry. It does not design the
registry itself.

## Current state

The PWA has no tag-based grouping or browsing. The three list renderers that a tag
tile would sit beside group by something else.

### `renderSessionsAsTree(sessions)` — `internal/server/web/app.js:2649`

- Groups sessions by the **spawn relationship** (`parent_id`), not by tags.
- Builds `byId` keyed on `full_id`, a `childrenOf` map from `parent_id`, and a
  `roots` list of sessions whose parent is absent or unknown.
- Renders recursively. Each nesting level adds `margin-left: depth * 18px` and a
  left border. A child whose `parent_id` points at a missing session gets an
  "orphaned" warning badge.
- Each node is rendered by `sessionCard`.
- Called at `app.js:2502` when `state.sessionTreeView` is on (BL348). Otherwise
  `renderSessionsView` maps `sessionCard` over the flat `visible` list (`app.js:2503`).

### `sessionCard(sess, idx, total)` — `internal/server/web/app.js:2829`

- Renders one session as a card. It shows the name or task (truncated to 80
  characters), the short ID, the backend badge and the state badge.
- Action buttons depend on state. Active sessions get Stop, plus Quick commands
  when waiting for input. Done sessions get Restart and Delete. Summary is shown
  when the summarizer is enabled. A maximize button opens the session in dashboard
  expand mode.
- Shows a parent badge when `sess.parent_id` is set (`app.js:2963`).
- Reads no tag field. A grep of `app.js` for `tags` finds only notification,
  compute-node, LLM and attachment hits, none on sessions. A session card therefore
  has no tag display today.

### `renderPRDRow(prd)` — `internal/server/web/app.js:11674`

- Renders one PRD as a **card** (`div.prd-row.prd-card` with a
  `prd-card-status-<status>` class), not a table row. The brief called it a flat
  table row; the body shows it is a card.
- Computes a file-conflict map across stories and marks stories that plan the same
  file (`app.js:11681-11694`).
- Shows badges for template, template origin, LLM backend/effort/model, parent PRD
  link and recursion depth. It also shows a server badge in aggregated
  multi-server mode.
- Called as `prds.map(renderPRDRow)` at `app.js:11544`. The list is flat. The
  function does no grouping and renders no tags. Child sections are handled
  elsewhere (`app.js:19619` builds a child index for them).

### Status filtering

- **Automata list:** `_automataState.statusFilter` is a `Set` (`app.js:19034`).
  The list render keeps items whose `status` is in the set (`app.js:19149-19152`).
  `toggleAutomataStatusFilter` (`app.js:20026`) toggles membership.
- **`renderAutonomousView()`** (`app.js:22096`) draws two status badge rows. Active
  statuses are `draft, planning, needs_review, approved, running, paused, blocked,
  completed`. History statuses are `cancelled, rejected, archived`. It also draws
  type badges (`software, research, operational, personal`) backed by
  `_automataState.typeFilter`. There is no tag filter.

### What the tag registry is expected to provide

Taken from the BL393 notes (`docs/plans/historical-plans/2026-10-03-bl393-automata-tags.md`,
decisions in its §1 and §2 items 3–5) and the README row for BL393 (row 35, "not started"):

- Nested tags from the start, as a tree (for example `Work` → `Mobile App` → `iOS`).
- Multiple tags per Automaton, and an Automaton appears under each of its tags when
  browsing by tag.
- Renaming or re-parenting a tag updates every reference.
- Browse-by-tag as true sectioning, with a left-rail or top tree selector.
- Bulk tag assignment in the batch bar.

Per-tag counts are **not** stated in the README row. The BL393 sections read for
this note (lines 36–60 and 200–215) do not mention them either. The evaluation
section must confirm whether counts are wanted.

Existing tag tooling: the MCP tool catalog lists `autonomous_tag_create`,
`autonomous_tag_list`, `autonomous_tag_rename`, `autonomous_tag_set_parent` and
related tools. This note did not verify their implementation. A grep of `app.js` for
`autonomous/tags` found no PWA caller.

## Dependencies

1. **BL393 tag registry** (README row 35, not started). The tile cannot show
   anything meaningful until it has nested tags, multi-tag assignment and a stable
   read API for tag lists and membership.
2. **Tag fields on list items.** Sessions and PRDs carry no tag field the PWA reads
   today (see the `sessionCard` and `renderPRDRow` notes above). Automata may need
   a tag field in their list payload. Each must be confirmed against the API
   before the tile is designed.
3. **Host surface.** The Automation PWA list (2026-10-14 design, `## PWA`, BL431
   Phase 6, README row 20, not started) reuses the `renderAutonomousView`
   conventions. If the tile lives there, it depends on Phase 6. If it lives on the
   Sessions or PRD views, it does not.

## Out of scope

- No implementation. No source file changes, including `app.js`, `style.css` and
  locale bundles.
- No tag data model. The registry shape, storage and API belong to BL393.
- No Automation-specific view. The tile is not designed as part of the Automation
  tab.

## Evaluation

Each subsection below is a candidate design. Items 3 to 6 each end with a verdict.
Section 8 picks one design per surface.

**Terminology.** Mockups use the user-facing words: "Automata" for the tab and
"Automaton" for each item. "PRD" stays internal. Tile labels follow the same rule.

### 1. Tag-tree drill-down

**Top level.** The top level lists root tags. Each row shows the tag name, a
count, and a chevron if the tag has children.

- The count is the number of **distinct** items in the tag's subtree. An item
  tagged `Work/iOS` and `Work/Shared` counts once toward `Work`.
- Subtree counts answer "how many items would I see if I opened this tag". Direct
  counts (items tagged exactly this node) are shown only as a tooltip. This is
  open question 2.
- Untagged items get a final `Untagged` row so nothing is hidden.

**Opening a tag.** Opening a tag shows:

1. the child tags, as a row of chips with counts;
2. the items under this tag, including its descendants. A "direct only" toggle
   narrows the list.

**Cascade.** Nested tags cascade. `Work` lists items tagged `Work`,
`Work/Mobile App`, and `Work/Mobile App/iOS`. The depth of the match is not shown
in the list. Each item shows its own full tag path under its title, so the
operator can see where it sits.

**Breadcrumb.** The breadcrumb is `Tags › Work › Mobile App › iOS`. Every crumb
is clickable. The first crumb returns to the top level. The breadcrumb is also the
URL hash (for example `#automata/tags/work/mobile-app/ios`), so the browser back
button and deep links both work. On a phone, a breadcrumb deeper than three levels
collapses its middle crumbs to `…`.

**An item with several tags.** The item appears under every tag it carries. Within
one open tag it appears once, even when two of its descendant tags match. It is
never duplicated inside one list. Each row shows a `also in:` line with its other
tags. Tapping one of them navigates there. A "primary tag" was considered and
rejected. It hides the other memberships, and it adds a field that BL393 does not
name.

**Web/tablet mockup** (drill-down into `Work › Mobile App`):

```
+----------------------------------------------------------------------+
| Automata                                        [List] [Tags]  [+ New]|
+----------------------------------------------------------------------+
| Tags › Work › Mobile App                              breadcrumb      |
|                                                                      |
| Sub-tags:  [ iOS 4 > ]   [ Android 3 > ]   [ Shared 2 > ]            |
|                                                                      |
| Items under Mobile App (incl. sub-tags)                    9         |
| [ ] direct only                                                      |
|  +---------------------------+  +---------------------------+        |
|  | Release pipeline  running |  | Docs refresh   needs_rev. |        |
|  | #work #ios  also: Shared  |  | #work #android            |        |
|  +---------------------------+  +---------------------------+        |
+----------------------------------------------------------------------+
```

**Mobile mockup** (same view, one section at a time):

```
+--------------------------------------+
| <  Tags                              |
| Work › Mobile App › iOS              |
| Sub-tags:  iOS (4)  >                |
| Items (incl. sub-tags)          4    |
| --------------------------------------
| Release pipeline   running   4/9  [...]
| #work #ios  also: Shared             |
| --------------------------------------
| Beta build         needs_review      |
| #work #ios                           |
+--------------------------------------+
```

### 2. PRD tile redesign

**Reuse the session-card language.** The tile is built on the existing `.session-card`
base: `bg2` background, `--radius`, a 4px left border, and 12px/14px padding. The
status colour comes from the existing `.prd-card-status-*` modifiers, which
`renderPRDRow` already uses. No new palette, border, or card shell is introduced.
The header reuses `.session-card-header` and the `.state-badge-*` pattern for the
status chip. The mapping to `sessionCard` is:

| `sessionCard` today | Tile field |
|---|---|
| name or task, truncated to 80 chars | title (two-line clamp) |
| short ID | short ID |
| backend badge | backend and model line |
| state badge | status chip |
| action buttons by state | actions |

**Fields added for the tile.** These are not on `sessionCard` today.

- **Progress.** `done / total` tasks plus a thin bar. The list payload must carry
  the counts. Fetching `autonomous_progress` for each card would be an N+1 request.
  This is dependency item 4 below.
- **Tags.** Up to three chips. A `+N` chip opens the tag list. Tags come from the
  registry, not from a PRD field, since none exists yet.
- **Last activity.** A relative time from the most recent update. The idle duration
  reuses the Automation design's idle-for string.
- **Actions.** `Open` is always shown. Then the actions for the current status,
  using the same rules as the existing PRD card. An overflow menu holds the rest.

**Anatomy:**

```
+------------------------------------------+
| Release pipeline               [running] |   header: .session-card-header
| a1b2c3d4 · claude-code · sonnet          |   backend and model
| ███████░░░░░  4 / 9 tasks                |   progress bar
| #work  #ios  +1                          |   tags
| updated 4m ago        [Open]  [Stop] [⋯] |   activity and actions
+------------------------------------------+
   ^ left border = status colour (.prd-card-status-running)
```

**Degradation to a row.** The tile collapses to a row at two breakpoints.

- Below 720px the grid becomes a single column of two-line rows. Line one is the
  title, the status chip, and the progress count. Line two is the backend, the tag
  count, and the last activity. Actions move into `[⋯]`.
- Below 420px the row is one line. Title, status dot, and `4/9` are shown, and the
  activity time is dropped. The row still carries the status-coloured left border,
  so the list scans the same way as the tile grid.

```
+----------------------------------------+
| ▌Release pipeline         running 4/9  [⋯]
|  claude-code · #work +1 · 4m           |
+----------------------------------------+
```

### 3. Faceted multi-select tag filter over a tile grid

**Facets.** Three facets sit above the grid: tags, status, and backend. Each facet is a
multi-select chip group.

**Semantics.** These rules are fixed. They are not per-view options.

- **Across facets: AND.** A tile must pass every non-empty facet.
- **Within status and backend: OR.** Selecting `running` and `blocked` matches
  either.
- **Within tags: ANY (OR) by default**, with an `ALL (AND)` toggle. In ANY mode a tile
  matches if it carries any selected tag. In ALL mode it must match every selected
  tag.
- **Hierarchy.** A selected tag matches itself and every descendant, in both modes.
  Selecting `Work` in ALL mode with `iOS` also selected means: the tile must have
  some tag under `Work`, and some tag under `iOS`.

**Faceted counts.** Each option shows how many tiles would remain if it were picked,
given the other facets. These counts are computed from the loaded list on the
client. The cost is O(n) per facet and n is small (tens to low hundreds of
Automata). This needs no new endpoint.

**Empty result.** The grid shows "No matches. Clear 2 filters", and the count reads
"Showing 0 of 23".

**Web/tablet mockup** (filter bar above the grid, 2-column grid at tablet width):

```
+----------------------------------------------------------------------+
| Tags: [work x] [ios x]  match [ANY v]                                |
| Status: [running x] [+]        Backend: [any v]                      |
|                                              Showing 4 of 23  [Clear]|
+----------------------------------------------------------------------+
|  +---------------------------+  +---------------------------+        |
|  | tile                      |  | tile                      |        |
|  +---------------------------+  +---------------------------+        |
|  +---------------------------+  +---------------------------+        |
|  | tile                      |  | tile                      |        |
|  +---------------------------+  +---------------------------+        |
+----------------------------------------------------------------------+
```

On a phone the facet bar becomes one `Filters (2)` button that opens a bottom sheet
with the same facets.

**Pros.**
- It answers "running work tagged iOS" in one step.
- Faceted counts show a dead end before the operator clicks it.
- It reuses the `statusFilter` Set pattern (`app.js:19034`) for status, so the code
  change is small.

**Cons.**
- Three facets with AND across them can produce an empty grid quickly. The
  "Clear" affordance and the empty-state copy must carry that load.
- ANY versus ALL is a concept that most operators have not met. It needs a visible
  label, not a hidden default.
- Client-side counts assume the whole list is loaded. Paginated lists break that
  assumption.

**Verdict: adopt.** Status and backend facets are cheap and useful. Tag facets are
the main case, and the ANY/ALL toggle is needed for multi-tag items. The
faceted counts are computed on the client when the list is complete. If the list is
paginated, the counts move server-side (section 9).

### 4. Attention-first section

**What is pinned.** Two groups are pinned above the tag browser.

- **Needs attention** (`blocked`, `needs_review`). Expanded. Each row shows the
  blocked reason where one exists.
- **Running** (`running`). Collapsed to a compact strip: one line per Automaton, up
  to five, then `+N more`. The strip shows progress and the tag count. The section
  is pinned so the operator can see live work, but it does not push tag browsing
  below the fold.

`paused` is not pinned. A paused Automaton is a choice the operator made, not an
open question. This is open question 3.

**Duplication is intended.** A pinned item still appears in its own tag section. The
pinned area is a view, not a move, so the tag tree stays complete.

**Sections collapse when empty.** An empty attention group takes no height. When both
are empty, the pinned area is hidden.

**Default view.** The landing view on page load is:

- the attention-first view, when the attention count is at least one;
- the tag browser otherwise.

The choice is made once, at page load. It does not switch while the page is open,
since a view that changes under the operator is confusing. A per-viewer
override (remembered in `localStorage`, with try/catch) lets the operator pin one
view as their default. The header always shows `Attention (N)` so the switch is
visible.

**Mockup (web):**

```
+----------------------------------------------------------------------+
| Automata                                        [List] [Tags]  [+ New]|
+----------------------------------------------------------------------+
| NEEDS ATTENTION (2)                                         [hide]   |
|  > Release pipeline   BLOCKED       "waiting on CI"        #work     |
|  > Docs refresh       NEEDS REVIEW                         #work     |
| RUNNING (3)                                                 [show]   |
|  > Migration task   4/9   #ios   4m     +2 more                      |
+----------------------------------------------------------------------+
| TAGS                                                                 |
|  > Work (9)                                                          |
|  > Personal (4)                                                      |
+----------------------------------------------------------------------+
```

**Pros.**
- The operator sees what needs them before anything else.
- Tag browsing, which is a browsing mode, keeps its full value.
- The landing rule adapts to the state of the system without flipping mid-session.

**Cons.**
- Two landing rules are harder to explain than one.
- A permanently visible pinned area takes vertical space on phones. The mobile layout
  has to keep it short.
- Automata sit in the same two groups as the new Automaton tile. The lists must not
  diverge.

**Verdict: adopt the pinned area on every surface, collapse the running group by
default on mobile, and use the conditional landing rule.** The pinned area is cheap
and always useful. The conditional landing rule is the main change of behaviour, so
it ships behind a per-viewer override.

### 5. Collapsible inline tag sections (web/tablet) versus single-section drill-down (mobile)

**Web and tablet: collapsible inline sections.** Each top-level tag is a section with a
header (name, count, chevron). Expanding it shows that tag's tile grid. Nested tags
appear as sub-sections inside the parent. A top-level `Untagged` section sits last.

- Expansion state is kept per viewer in `localStorage`, under try/catch.
- Sections are collapsed by default when there are more than eight top-level tags.
  A "show all" control expands the rest.
- Sections are replaced by a single flat grid while any filter (section 3) is
  active. Sections return when the filter is cleared.

```
+----------------------------------------------------------------------+
| v Work (9)                                                           |
|     v Mobile App (5)                                                 |
|          [tile]  [tile]  [tile]                                      |
|          [tile]  [tile]                                              |
|     > Shared (2)                                                     |
| > Personal (4)                                                       |
| > Untagged (3)                                                       |
+----------------------------------------------------------------------+
```

**Mobile: single-section drill-down.** Only one section is on screen at a time. Opening a tag
replaces the list with that tag's view. A back row returns to the parent. This is the
drill-down from section 1, using the mobile mockup there.

**Pros of inline sections.**
- The whole tag tree is visible at a glance, with counts.
- It needs no navigation state beyond expansion flags.

**Cons of inline sections.**
- On a phone, several expanded sections make a very long scroll.
- Every section mounts its grid. Many tags with many tiles produce a heavy DOM.
- Counts for all tags must be known at load time, which pushes toward a counts
  endpoint (section 9).

**Pros of drill-down.**
- One screen, one list, one back action. It fits a narrow viewport.
- It scales to many tags without a heavy DOM.

**Cons of drill-down.**
- The tree is hidden behind taps, so the operator sees less context at once.
- Navigation state (breadcrumb, hash) must be correct, or back-button behaviour
  breaks.

**Verdict: inline sections on web and tablet, drill-down on mobile.** The two
layouts share one tree and one count source. Only the presentation differs.

### 6. Saved filter combos

A saved combo is a named set of facet selections: tags with the match mode, the status
set, and the backend set. It is applied in one tap from a chip row above the filter
bar.

**Where it is stored.** Three options were weighed.

- **Browser `localStorage`.** Simplest. Each browser keeps its own combos. It does
  not sync and it is lost on a cleared browser.
- **Server-side store, per operator, behind REST.** The daemon is a single-operator
  system, so "per operator" means one store. The combos survive a browser change and
  show up on every client.
- **Stored as a "smart view" inside the tag registry.** Couples the view to BL393's
  data model. Views would then depend on the registry's schema.

**Recommendation: server-side store.** `localStorage` is used only as a cache for an
unsent draft, under try/catch.

**Sync across clients.** A client that loads the list fetches the combos. A save or
delete emits an event on the existing event stream, so open clients refresh their
chip row. On conflicting edits, the last write wins, keyed by the combo's
`updated_at`. Combos are small, so this is safe.

**Pros.**
- One-tap access to the operator's common queries, such as "blocked mobile work" or
  "running on claude-code".
- The store is small and independent of tag internals.
- It sits beside the existing filter state, so the code change stays small.

**Cons.**
- A new endpoint set (section 9) and a new store to maintain.
- Combos may reference tags that have been renamed or deleted. The combo needs a
  "tag no longer exists" state, not a silent drop.
- It adds state that must be reviewed when the registry changes.

**Verdict: adopt in a later phase, after the filter exists.** Saved combos depend on the
filter (section 3) and the registry. Build them last, server-side, with the
missing-tag state from the start.

### 7. Automation PWA cross-reference

The Automation PWA phase in `2026-10-14-automation-orchestration-design.md` (section
`## PWA`, BL431 Phase 6, README row 20) owns the new Automation tab, its pipeline
strip, and its create/edit flow. This section does not repeat any of that scope.

**Shared components.** The tag tile reuses these pieces from the Automation design:

- the tile markup, using `prd-row prd-card prd-card-status-<status>`, so the
  left-border colour works unchanged;
- the status filter badge row (`automata-filter-badge`), driven by the
  `_automataState.statusFilter` Set;
- the attention pattern from section 4, which uses the Automation design's
  blocked-reason and idle-duration strings;
- the locale-key rules: no BL ids in any user-facing string, and the Automaton and
  Automata terminology table in AGENT.md.

**Not shared, and not duplicated here.** The pipeline strip, the Automation
create/edit flow, the BudgetPolicy fields, and the Automation tab itself stay in the
Automation design. The tag browser does not add Automation-level grouping.

**Placement dependency.** The tag browser can be built in the existing Automata tab
(`renderAutonomousView`) with no dependency on Phase 6. If it lands in the new
Automation tab instead, it depends on Phase 6. The recommendation (section 8) builds
it in the Automata tab first. The Automation tab adopts the shared tile later.

## Recommendation

### 8. Recommendation: one design per surface

| Surface | Chosen design |
|---|---|
| **Web (1100px and up)** | Attention-first pinned area on top. The faceted filter bar (section 3) above the content. Inline collapsible tag sections in a 3-column tile grid, with nested sub-sections. When a filter is active, sections give way to one flat grid. |
| **Tablet (720px to 1099px)** | The same as web, with a 2-column grid. The filter bar wraps to two rows and each facet opens a popover. |
| **Mobile (under 720px)** | Attention section as a short row list, with the running group collapsed. Single-section drill-down (section 1) with breadcrumb and back row. Tiles degrade to rows (section 2). Filters in a bottom sheet. Saved combos, once they exist, are a horizontal chip row. |

**Landing rule** (all surfaces): attention view when the attention count is at least
one, tag browser otherwise, decided once at page load, with a per-viewer override.

**Status: not started.** Every phase below is planned and nothing is implemented. The
design is also on hold until BL393 ships the tag registry.

**Phased implementation outline.** Sizing is S (under a day), M (a few days), or L (a
week or more), and is a rough estimate.

| Phase | Scope | Size | Status |
|---|---|---|---|
| 0 | BL393 tag registry: nested tags, multi-tag membership, rename and re-parent, read API. Not this design's work. | owned by BL393 | not started |
| 1 | Read API: tag tree with subtree and direct counts (`counts=true`), and a `tags` field on list items for Automata. | S to M | not started |
| 2 | Tile component and row degradation. Shared with the Automation tab. | M | not started |
| 3 | Attention pinned area, running strip, landing rule, per-viewer override. | S | not started |
| 4 | Tag browser: inline sections on web and tablet, drill-down with breadcrumb and hash on mobile. | M to L | not started |
| 5 | Faceted filter bar and bottom sheet, with faceted counts and ANY/ALL. | M | not started |
| 6 | Saved combos: server-side store, REST, MCP and CLI mirror, client chip row, missing-tag state. | M | not started |
| 7 | Parity follow-ups for Android and iPhone (see section 9). | S each, in the companion repo | not started |

Phases 2 and 3 can run in parallel once phase 1 is done. Phase 5 depends on phase 2
for the tile. Phase 6 depends on phase 5.

## Parity surface

Every row is either a change or N/A with a reason. Mirror names follow the existing
autonomous routes and the Automation design's parity table.

| Surface | Included? | Concrete design |
|---|---|---|
| **REST** | Yes | `GET /api/autonomous/tags?counts=true` returns the tree with subtree and direct counts. The list endpoint gains a `tags` field on each item, and counts stay correct at any page size. Saved combos: `GET/POST /api/autonomous/views`, `PUT/DELETE /api/autonomous/views/{id}`. `409` on version mismatch. The tree is only served where the registry exists (BL393). |
| **MCP** | Yes | `autonomous_tag_list` gains a `counts` flag. New `autonomous_view_list`, `autonomous_view_create`, `autonomous_view_update`, `autonomous_view_delete`. Each is added to the MCP capability map, as the Automation design does. |
| **CLI** | Yes | `datawatch autonomous tags [--counts]`; `datawatch autonomous views list\|create\|apply\|delete`. The `autonomous` command group already exists (`cmd/datawatch/cli_autonomous.go`). |
| **Comm channel** | Yes, read-only | `automata tags` (top-level counts) and `automata view <name>` (run a saved combo as a list). `automata attention` lists blocked and needs_review items. Create, rename and combo edits are excluded, since they do not fit one line. |
| **YAML/config** | Yes | Provisional keys under `autonomous.tags`: `default_view` (`attention`\|`tags`), `attention_statuses` (default `[blocked, needs_review]`), and `direct_counts_visible` (bool). Each goes in `handleGetConfig` / `handlePutConfig`, `docs/config-reference.yaml`, and `configure <key>=<value>`. Saved combos are runtime data, not YAML, the same as Automations. |
| **PWA** | Yes | Attention pinned area, tile grid, inline sections (web and tablet), drill-down (mobile), faceted filter bar, saved-combo chip row. Locale keys go in all five bundles under `internal/server/web/locales/`. |
| **Android** | Yes, via a parity issue | A `datawatch-app` issue for the tile row, attention list, and drill-down. The filter and saved combos may lag one release. Filed in the companion app repo by the implementation PRD, not here. |
| **iPhone/iOS** | Yes, via a parity issue | Same scope as Android, in the companion app's iOS track. Attention pushes ride on BL427's mobile topic sink, so no tag-specific delivery is needed. |

**Sessions:** N/A in v1. Session cards carry no tag field today (see Current state),
and the tile design covers Automata only. Open question 1 asks whether sessions should
inherit tags from their parent Automaton.

## Open questions

1. **Session tags.** Should a session carry its own tags, or inherit the tags of the
   Automaton that spawned it? Inheritance is read-only and needs no new session data.
   Direct tagging would need a session field the API does not have.
2. **Counts.** Are subtree counts (distinct items under the tag) wanted, or direct
   counts? BL393 does not say. Showing both is possible, but it doubles the
   vocabulary.
3. **Attention statuses.** Is `needs_review` the only "awaiting a human" status? Should
   `paused` join the attention group?
4. **List payload.** Does the Automata list endpoint return task counts (for progress)
   and tags on each item? If not, the list needs a field, or the tile needs a second
   request. Neither has been checked against the live API.
5. **Placement.** Does the tag browser live in the existing Automata tab, which needs
   no Phase 6, or in the new Automation tab?
6. **Multi-tag display.** Is the duplicate-across-branches display (section 1) acceptable,
   or should the operator choose a single primary tag for sorting only?
7. **Live counts.** Do counts refresh on the event stream when a tag changes, or only on
   reload? Rename and re-parent must update every reference (BL393).
8. **Federation.** Are tags local to one node? Renaming a tag on one peer will not
   reach another. Tags must not silently collide across a multi-server view, which
   already shows a server badge.
9. **Default match mode.** Section 3 sets the tag facet to ANY by default. Does the
   operator want ALL as the default for multi-tag queries?
10. **Registry source.** The MCP catalog lists `autonomous_tag_*` and
    `autonomous_prd_set_tags`, but no Go source in this worktree defines them, and the
    PRD model has no tags field (only `Template.Tags`, `internal/autonomous/models.go`).
    Which build serves those tools, and is the registry in this repo's HEAD at all?
