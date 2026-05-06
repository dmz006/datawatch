# datawatch v5.26.37 — release notes

**Date:** 2026-04-27
**Spans:** v5.26.36 → v5.26.37
**Patch release** (no binaries — operator directive).
**Closed:** PRD FAB size matches sessions FAB; alerts page no longer shows the new-session FAB.

## What's new

### FAB consistency

Operator: *"FAB on automations page is not the same size as FAB on sessions page; also FAB is not necessary on alerts page."*

#### PRD FAB now reuses the canonical `.new-session-fab` CSS class

v5.26.36 introduced the autonomous-tab FAB with inline styles (48×48, right/bottom 18px). The sessions FAB is canonical and lives in `style.css` (56×56, right 16px, bottom = `var(--nav-h) + 16px + env(safe-area-inset-bottom, 0)`, 28px font, accent2 fill, hover/active animations). v5.26.37 collapses the PRD button onto that class:

```html
<!-- Before (inline, 48×48, no nav clearance) -->
<button id="prdNewFab" class="btn-secondary"
        style="position:fixed;right:18px;bottom:18px;width:48px;height:48px;…">+</button>

<!-- After (canonical CSS, 56×56, nav-h + safe-area) -->
<button id="prdNewFab" class="new-session-fab"
        onclick="openPRDCreateModal()" title="New PRD" aria-label="New PRD">+</button>
```

Both FABs now have identical size, position relative to the bottom nav, hover/active transforms, and safe-area handling on notched devices.

The PRD FAB doesn't need the `.hidden`-toggle visibility logic that the sessions FAB uses (issue #22) — it lives inside `renderAutonomousView()`'s output, which gets replaced when the operator switches tabs, so the element naturally disappears.

#### Alerts page no longer shows the sessions FAB

The visibility rule was:

```js
const showFab = view === 'sessions' || view === 'alerts';
```

That `|| view === 'alerts'` was always wrong — the FAB invokes `openNewSessionModal()`, which is a session-creation flow. The alerts list has no equivalent "new alert" affordance, so the FAB on alerts was a misleading shortcut to "new session" while the operator was looking at alerts.

```js
// v5.26.37
const showFab = view === 'sessions';
```

If the alerts page wants its own creation flow later, that gets its own FAB pointed at the right modal — not the session one.

## Configuration parity

No new config knob.

## Tests

UI-only change. Go test suite unaffected (still 465 passing). Smoke unaffected (37/0/1).

## Known follow-ups

Same backlog as v5.26.36 — see `docs/plans/2026-04-27-v6-prep-backlog.md`.

## Upgrade path

```bash
git pull
datawatch restart
# Hard-reload the PWA (SW cache bumped to datawatch-v5-26-37).
# Open Autonomous tab — FAB now matches the Sessions tab's FAB
# size + position. Visit Alerts tab — no FAB.
```
