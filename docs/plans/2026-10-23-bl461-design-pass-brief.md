# BL461 tag-based browsing and tile redesign: what the PRD will do (for operator review)

Written 2026-10-23 so the operator can review the scope before the PRD runs. PRD `18db0001` is held until BL587 and BL589 ship (operator decision 2026-10-23), then it is re-decomposed against the new list code. There is **no design document yet**: this PRD is a design pass, and its job is to produce one.

## The PRD spec (verbatim)

BL461 — Tag-based browsing + tile redesign for the PRD and session lists at scale (PWA + mobile/tablet parity apps). DESIGN PASS ONLY -- no implementation yet. Depends on BL393 (nested tags) shipping first; cannot be built until the Tag registry exists.

WHY: operator-raised 2026-10-15, directly after BL393. With dozens of sessions and (soon) hundreds of PRDs/Automata, today's flat lists don't scale: sessions already use a card layout (sessionCard()) with one existing tree-rendering precedent (renderSessionsAsTree(), which groups by parent/child spawn relationship, NOT tags -- a reusable pattern, not the feature itself); PRDs are flat table rows (renderPRDRow()) with zero grouping or card/tile layout at all today. Confirmed live by reading internal/server/web/app.js directly before filing this -- do not re-derive, this audit is already done.

SCOPE FOR THE DESIGN PASS (when picked up, after BL393 ships):
1. Tag-tree drill-down: top level shows the tag list with per-tag counts; opening a tag shows its PRDs/sessions; nested tags cascade (matches BL393's own "nested from the start" decision).
2. Tile/card redesign for the PRD list specifically (sessions already have this pattern -- extend it, don't reinvent).
3. Evaluate, don't just pick blind: (a) faceted multi-select tag filter over a tile grid (matches BL393's "multiple tags per Automaton" decision) -- likely best on tablet/web's extra width; (b) a recency/attention-first section (blocked/needs_review/running) pinned above pure tag browsing, since at this scale "what needs me now" often beats category browsing as the default view; (c) collapsible inline tag sections (see several at once) for web/tablet vs. a single-section drill-down for mobile's narrower screen; (d) saved/pinned custom filter combos (tag + status + backend) as one-tap shortcuts.
4. Coordinate with BL431 Phase 6 (Automation PWA) -- Automations will face the exact same at-scale browsing problem once there are many of them; share tile/tag UI patterns rather than building two divergent designs. Cross-reference, do not duplicate BL431 Phase 6's own scope.
5. Full Mobile-Parity Rule compliance: file the datawatch-app tracking issue(s) once the PWA design is settled, same pattern as every other PWA feature in this backlog.

OUT OF SCOPE for the design pass: any implementation (this is a design doc only, same treatment as BL412/413); BL393's own tag data model (that's BL393's scope, not this one's); BL431 Phase 6's own Automation-specific view (cross-referenced, not absorbed).

Status: HOLD. File only, no implementation. Sequenced immediately after BL393 in the v10.0.0 queue -- cannot start meaningfully before BL393 ships the tag registry.

## The tasks in the current plan (will be re-planned)

### Story: Write the tag-browsing and tile design document

- **b6ef0002 Audit current PRD and session list rendering**
  Read-only exploration, then start the design doc. In internal/server/web/app.js read renderSessionsView() (around line 2276), renderSessionsAsTree() (around line 2649, groups by parent_id spawn relationship, not tags), sessionCard() (around line 2829), renderPRDRow() (around line 11674, flat table row with no grouping) and renderAutonomousView() (around line 22096), plus any status filter in the PRD list. Also read docs/plans/2026-10-14-automation-orchestration-design.md (Automation PWA phase) and the nested-tags row in docs/plans/README.md to learn what the tag registry is expected to provide (multiple tags per Automaton, nested tags, per-tag counts). Create docs/plans/2026-10-20-tag-browsing-tile-design.md following AGENT.md Planning Rules: title, date 2026-10-20, a Version line (read the current version from internal/server/api.go), Status (Design only, on hold until the tag registry 

- **b6ef0003 Write the option evaluation, recommendation and parity surface**
  Edit docs/plans/2026-10-20-tag-browsing-tile-design.md (created by the previous task; if absent, create it with the sections listed there). Fill in these sections with concrete, reasoned content, including ASCII mockups for web/tablet and mobile widths: (1) 'Tag-tree drill-down': top level lists tags with per-tag counts, opening a tag shows its PRDs/sessions, nested tags cascade, define breadcrumb and how an item with several tags appears. (2) 'PRD tile' redesign: fields on a tile (title, status chip, backend/model, progress, tags, last activity, actions), reusing the sessionCard() visual language instead of inventing a new one, and how it degrades to a row. (3) 'Faceted multi-select tag filter' over a tile grid (AND/OR semantics, fits tablet/web width). (4) 'Attention-first section': blocked/needs_review/running pinned above tag browsing, and when it should be the default view. (5) 'Col

### Story: Register the design, file the mobile-parity issue draft and update the tracker

- **b6ef0005 Draft the datawatch-app issue and parity-status row**
  Create docs/plans/assets/tag-browsing-datawatch-app-issue.md: a ready-to-file issue for the dmz006/datawatch-app repo, titled in the style 'feat: tag-based browsing and PRD tile redesign (design pending)', stating that the PWA design is documented in docs/plans/2026-10-20-tag-browsing-tile-design.md, that implementation is on hold until the tag registry ships, and listing separately the Android and iOS (iPhone/iPad) work: tag drill-down, PRD tiles, attention-first section, mobile single-section drill-down, saved filter combos, plus the REST contract the clients will parse (summarise from the design doc's parity table). Mark it clearly as a draft to be filed by the operator or supervisor once the design is approved. Then add a 'Tag browsing and PRD tiles' row to docs/parity-status.md in the existing table format (read the file first to copy its column layout and legend), with PWA status '

- **b6ef0006 Link the design doc in the planning tracker**
  In docs/plans/README.md find the table row for the tag-based browsing and tile redesign item (search for BL461). Change its title cell to link docs/plans/2026-10-20-tag-browsing-tile-design.md (relative link, same style as other design-doc rows such as the automation orchestration design) and update its status text to 'design doc written (design only, implementation on hold until nested tags ship); datawatch-app issue draft in docs/plans/assets/tag-browsing-datawatch-app-issue.md'. Keep its dependency column unchanged. Do not touch other rows. Then run `sh scripts/check-docs-sync.sh` and `bash scripts/check-no-internal-refs.sh`, and run `make sync-docs` only if the docs-sync check says it is needed. No version bump and no versioned CHANGELOG entry (docs-only change under docs/plans and parity docs); do not run git add or git commit.

## How to give feedback

Tell the supervisor what to change (or add a note to this file). If nothing is said, the PRD is re-decomposed and run after BL587 and BL589 ship.
