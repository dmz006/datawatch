# datawatch v5.26.36 — release notes

**Date:** 2026-04-27
**Spans:** v5.26.35 → v5.26.36
**Patch release** (no binaries — operator directive).
**Closed:** PRD panel UX polish — FAB for New PRD + collapsible filter row + backlog refactor.

## What's new

### PRD panel: FAB for New PRD, filter row collapsed by default

Operator: *"new prd should be a FAB (+) and not the new prd button at top. There should be a filter icon like sessions list to hide/show the filter and sort options, with it hidden by default."*

Old layout:

```
[+ New PRD] [Status filter ▼] [□ templates]
[ ... PRD rows ... ]
```

The "+ New PRD" button competed with the status filter for the same toolbar real estate, and the filter controls were always visible even when the operator wasn't using them.

New layout:

```
PRDs                                     [⛁]   ← click to toggle filter row
[ ... PRD rows ... ]                     [+]   ← floating action button
```

- **Floating Action Button** anchored bottom-right (`position:fixed`, `right:18px bottom:18px`). 48×48 circle, large `+` glyph, accent2 background, drop shadow. Click → `openPRDCreateModal()`. Lives only inside `renderAutonomousView()` so it doesn't bleed onto Sessions / Settings / other tabs (the parent view container's innerHTML gets replaced on tab switch, taking the fixed-position FAB with it).
- **Filter toggle (⛁)** in the header next to the "PRDs" label. Click toggles `prdFilterRow`'s display between `none` and `flex`. Hidden by default — operator only sees the filter dropdown + templates checkbox when they explicitly want it. Toggle button gets an `.active` class while open for visual feedback.

### Backlog refactor

Operator: *"on next pass refactor backlog. make sure active work and other areas that things are done are refactored into the correct closed sections."*

`docs/plans/2026-04-27-v6-prep-backlog.md` cleaned up:

- **Open** section now reflects only actual remaining work. `Per-session workspace reaper` (closed v5.26.26 + v5.26.27) and the `gh-actions audit + parent-full + concurrency guard` line items (closed v5.26.25) moved out of Open. The original `CI: parent-full + agent-goose containers` entry collapsed into a residual `CI follow-ups` section that lists only what's actually still pending (agent-goose Dockerfile, pinned action SHAs, kind-cluster smoke, gosec baseline-diff).
- **PRD-flow rework** consolidated into one entry showing all 6 phases with phases 1/2/5 marked ✅ and pointing at the release tags that closed them.
- **Closed in v5.26.x** section expanded from 9 to 30 entries — every patch from v5.26.6 → v5.26.35 has a one-line summary, newest at top.
- New top-of-Open entry: **PRD panel UX polish** (this release).

The intent: the Open section is a working list, the Closed section is institutional memory. After every release that ships from the backlog, items move down the page.

## Configuration parity

No new config knob.

## Tests

UI-only change. Go test suite unaffected (still 465 passing). Smoke unaffected (37/0/1).

## Known follow-ups

PRD-flow phase 3 (per-story execution profile + per-story approval gate) and phase 4 (file association) — design first, then implement. Phase 6 (howtos / screenshots / diagrams refresh) waits for the New PRD modal shape to fully settle (which v5.26.36's FAB rework hopefully marks as done) before screenshots get recaptured.

Mempalace alignment audit, service-function smoke completeness, datawatch-app PWA mirror, v6.0 cumulative release notes — unchanged. See `docs/plans/2026-04-27-v6-prep-backlog.md`.

## Upgrade path

```bash
git pull
datawatch restart
# Hard-reload the PWA (SW cache bumped to datawatch-v5-26-36).
# Open the Autonomous tab — the toolbar is now just a header
# label + filter toggle; the New PRD affordance is the round (+)
# button at bottom-right.
```
