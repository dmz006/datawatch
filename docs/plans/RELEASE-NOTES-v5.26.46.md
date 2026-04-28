# datawatch v5.26.46 — release notes

**Date:** 2026-04-27
**Spans:** v5.26.45 → v5.26.46
**Patch release** (no binaries — operator directive).
**Closed:** Three operator-asked PRD/UX items: filter icon to header bar; PRD dir picker matches sessions; "+ New folder" affordance.

## What's new

Operator: *"on autonoous page, the filter icon should match the magnifying glass on sessions list and be on the header bar to the left of the server status indicator like on sessions page. also if making new prd and choose product directory the path should be a selector box like new session. also here and on new session need to be able to create a directory while browsing."*

Three changes in one patch — all UX cleanup, no schema changes.

### 1. Autonomous filter toggle moved to the header bar

Pre-v5.26.46 the autonomous tab carried its own `⛁` glyph button inside the panel header. Sessions tab uses the magnifying-glass (`#headerSearchBtn`) in the top header bar instead — the affordance is reachable without scrolling. v5.26.46 collapses both onto the same button:

- The header `#headerSearchBtn` (🔍) now shows on `view === 'sessions'` AND `view === 'autonomous'`.
- Clicking it on autonomous calls `_toggleAutonomousFilters()` (the same handler that ⛁ used).
- The in-panel ⛁ button is removed; the panel header is now just the "PRDs" label.
- Tooltip switches to "Toggle PRD filters" when on autonomous.

The button sits in the same place as the sessions-tab one — to the left of the server-status dot.

### 2. New PRD project-directory field is now a directory selector

Pre-v5.26.46 the New PRD modal had a plain `<input type="text">` for project_dir. Operator typed a path. New Session modal has used a click-to-browse dir picker for a while (`#selectedDirDisplay` + `#dirBrowser`). v5.26.46 brings the New PRD modal in line:

```html
<div id="prdNewDirRow">
  <label>Project directory</label>
  <div class="dir-picker">
    <span id="selectedDirDisplay" onclick="openDirBrowser()">~/</span>
  </div>
  <div id="dirBrowser" style="display:none">
    <div id="dirBrowserContent"></div>
  </div>
</div>
```

The IDs (`selectedDirDisplay`, `dirBrowser`) are shared with the New Session modal — both modals are mutually exclusive views (you're either in autonomous + PRD modal or sessions + new-session modal), so collision is fine. Submit handler reads `newSessionState.selectedDir` (set by `selectDir()`), with a fallback to the display span's textContent for resilience.

### 3. "+ New folder" while browsing

Both the New Session and the New PRD dir browsers now expose a `+ New folder` button next to the `✓ Use This Folder` button:

```
[current path: /home/dmz/projects]
[ ✓ Use This Folder ] [ + New folder ]
[ 📁 .. ]
[ 📁 datawatch ]
[ 📁 nightwire ]
…
```

Click → `prompt()` for a folder name → POSTs `{path: <parent>/<name>, action:"mkdir"}` to `/api/files` (the daemon-side mkdir endpoint added in `handleFilesMkdir` already exists; the UI just wasn't exposing it). Refuses path-separator characters client-side; refreshes the listing on success so the new folder appears in the next render.

## Configuration parity

No new config knob.

## Tests

UI-only changes. Manually validated by:

- Opening Autonomous → magnifying-glass appears in header → click toggles filter row.
- Opening New PRD → "Project directory" → click "~/" → dir browser appears → "+ New folder" prompt → folder creates → listing refreshes.
- Same flow on New Session — the "+ New folder" affordance works identically.

Go test suite unaffected (still 465 passing). Smoke unaffected (40/0/1).

## Known follow-ups

The `docs/plans/2026-04-27-v6-prep-backlog.md` Open list shrinks again; the operator's PRD-flow phase 1 items related to UX are now all closed (unified dropdown, FAB matching sessions FAB, filter toggle matching sessions filter, directory picker matching new-session, mkdir affordance).

Phase 3 + Phase 4 (per-story execution profile + file association) still need design.

## Upgrade path

```bash
git pull
datawatch restart
# Hard-reload the PWA (SW cache bumped to datawatch-v5-26-46).
# Open Autonomous → 🔍 in the top header now toggles the PRD
# filter row. Open New PRD → Project directory is the same
# click-to-browse picker as New Session, with a "+ New folder"
# button visible in the dir browser.
```
