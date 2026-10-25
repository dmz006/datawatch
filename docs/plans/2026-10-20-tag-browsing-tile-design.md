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

_To be written in the next task._

## Recommendation

_To be written in the next task._
