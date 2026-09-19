# 2026-09-12 — BL373 Automata UI Improvements

**Date:** 2026-09-12  
**Version at planning:** v8.24.2  
**Ships in:** v8.25.0  

## Scope

Files affected:
- `internal/server/web/app.js` — all UI changes
- `internal/server/web/locales/en.json` (+ de/es/fr/ja) — new strings
- `internal/server/v5280_locales_test.go` — guard new high-visibility keys
- `docs/plans/README.md` — BL373 entry
- `CHANGELOG.md` — v8.25.0 entry
- `cmd/datawatch/main.go` + `internal/server/api.go` — version bump

## Bug Fixes

### BUG-A: Task click in automata list "Stories & tasks" closes the panel
Root cause: `renderAutomataCard` line ~15589 uses `renderStory(prd, st)` which calls
`renderTask` → `_prdToggleTask` → `_refreshAutomataOrPRD` → `loadAutomataPanel`.
Full panel reload collapses all `<details>` elements, including the open "Stories & tasks" one.

Fix: Replace `renderStory(prd, st)` calls with `renderDetailStoriesTree(prd)` in the list card
(one call for all stories, not per-story). `renderDetailStoriesTree` uses CSS `classList.toggle('hidden')`
for story expansion, and its task rows have no expand/collapse — so no `_prdToggleTask` call, no reload.

Also fix: session link in `renderDetailStoriesTree` task rows calls `navigate('sessions')` which
navigates to sessions list. Change to `navigate('session-detail', sessionId)` + stopPropagation.

### BUG-B: Triangle expand button in Stories tab does nothing until navigating away and back
Root cause: `_prdToggleTask` calls `_refreshAutomataOrPRD()` → `loadAutomataPanel()` when
`state.activeView === 'autonomous'`. But in the detail view, `automataPanel` is the list panel
(not shown), not `automataDetailBody`. So state IS updated but DOM is not refreshed.

Fix: in `_prdToggleTask`, check if `_automataDetailId !== null` and call
`renderPRDDetailView(_automataDetailId)` instead.

## UI Features

### UI-1: Spec truncation in detail header
Current: spec truncated to 280 chars in `_renderDetailHeader` line ~16892.
Fix: Remove hard truncation. Use expandable pattern: first 280 chars + "…show full" link
that inline-expands the rest. Already has full spec in Overview tab `<details>` — just fix header.

### UI-2: Running bar status graphs
Inline status card between action toolbar and tab strip (per operator choice).
Shows when `status === 'running'` or `status === 'decomposing'`:
- ✓/✗ Decomposed indicator
- N stories · M tasks total
- Per-story: mini progress bar with X/Y tasks + % fill

Implemented as `_renderStatusGraphs(prd)` (synchronous shell) +
`_loadStatusGraphsCompute(prd, slotEl)` (async, adds CPU/RSS).

### UI-3: GPU/CPU/memory for active sessions per story
Async: after rendering status graphs, fetch `/api/observer/envelopes` and match
task session IDs to envelopes. Add CPU%/RSS MB row under each story bar.

### UI-4: Files as links (deferred to BL374)
Needs new API endpoint `GET /api/autonomous/prds/{id}/file?path=...`.
Too large for this sprint. Filed as BL374 in backlog.

### UI-5: Action bar dropdown for running state
When `status === 'running'`: current toolbar shows no buttons (cancel is only in prdActiveSessionCard).
Add: explicit Cancel button + "⋯ Actions" dropdown with Clone to Template, View Sessions.

## Phases

| Phase | Description | Status |
|-------|-------------|--------|
| 1 | Create plan doc (this file) | Done |
| 2 | Bug fixes: BUG-A + BUG-B | Done |
| 3 | UI-1: spec expand in header | Done |
| 4 | UI-2+3: status graphs + compute stats | Done |
| 5 | UI-5: running action bar improvements | Done |
| 6 | Locale strings (5 bundles) + locale-guard test | Done |
| 7 | datawatch-app GitHub issues | Pending (Mobile-Parity Rule — file after commit) |
| 8 | Version bump, changelog, docs, commit | Done |
