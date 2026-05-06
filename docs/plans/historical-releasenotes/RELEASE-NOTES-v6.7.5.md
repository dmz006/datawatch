# Release Notes — v6.7.5 (Layout polish: bottom nav + wizard + settings modal)

Released: 2026-05-05
GH release: https://github.com/dmz006/datawatch/releases/tag/v6.7.5
Smoke: 95/0/6

## Summary

Layout polish patch. Three operator-reported issues:

1. **Bottom nav buttons left-huddled** on viewports under 600px — the BL239 `space-around` + `flex: 1` rule only applied at `min-width: 600px`. Default was `flex-start`.
2. **Launch Automaton wizard** had excessive vertical spacing (section paddings + section-title margins + per-checkbox margins all on the loose end of the project's standard).
3. **PRD Edit Settings modal** had `gap:10px` between fields making it feel airy, no visual separation between fields and the footer action row.

## Fixed

- **Bottom nav** (`internal/server/web/style.css` `.nav`) — promoted `justify-content: space-around` to the default rule (was only applied at `≥600px`). Buttons spread evenly across the bar at every viewport width. The existing `overflow-x: auto` + `scroll-snap-type: x proximity` still handles narrow viewports where cumulative button width would exceed the bar.

## Changed

- **Launch Automaton wizard CSS** (`internal/server/web/style.css`) — `.wizard-section` padding `12px 0 4px` → `8px 0 2px`; `.wizard-section-title` `margin-bottom: 8px` → `4px`; `.wizard-type-grid` `margin: 6px 0` → `4px 0`; `.wizard-advanced-body` padding `8px 0 4px` → `4px 0 2px`.
- **Launch Automaton wizard inline styles** (`internal/server/web/app.js` `openLaunchAutomatonWizard`):
  - Intent textarea `rows="4"` → `rows="3"`
  - Intent label margin-bottom `6px` → `4px`
  - Title input margin-top `6px` → `4px`
  - Workspace dirRow margin-top `4px` → `2px`
  - Execution grid gap `8px` → `6px`
  - Advanced-body per-checkbox margin-bottom `6px` → `3px`
  - Skills hint adds `margin-top:2px`
  - Footer margin-top `12px` → `8px`; gap `8px` → `6px`
- **PRD Edit Settings modal** (`internal/server/web/app.js` `openPRDSettingsModal`):
  - Form `gap:10px` → `gap:6px`
  - Every label gets `display:block;margin-bottom:2px` for consistent label-to-input spacing
  - Skills-hint gains `line-height:1.3` so wrapped lines stay readable
  - Guided-mode checkbox row `margin-top:2px`
  - Footer separated by `margin-top:6px;padding-top:8px;border-top:1px solid var(--border)` so it reads as a clear action row instead of another field

## What didn't change

- No backend changes. No locale changes. No new functionality. Pure CSS + inline-style cleanup.

## Smoke

`scripts/release-smoke.sh`: **95 pass / 0 fail / 6 skip**.

## Install

```
datawatch update
datawatch restart
```

## See also

CHANGELOG.md `[6.7.5]` entry.
