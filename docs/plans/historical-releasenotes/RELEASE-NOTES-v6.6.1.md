# Release Notes — v6.6.1 (BL246-followup polish)

Released: 2026-05-04
GH release: https://github.com/dmz006/datawatch/releases/tag/v6.6.1
Smoke: 91/0/6

## Summary

Patch release fixing two operator-reported polish bugs from the v6.6.0 cut:

1. **Automata batch action bar position** — was rendering inline inside the panel; now a fixed bottom popup above the nav, matching the Sessions select-mode bar.
2. **Redundant header Launch button** — removed; the floating ⚡ FAB shipped in v6.5.1 already covers it.

## Fixed

- **BL246-followup** (`internal/server/web/app.js` `_automataRenderBatchBar`) — Automata batch action bar (All / Run / Approve / Cancel / Archive / Delete / Cancel) now renders as a fixed bottom popup above the nav using the same `select-bar-fixed` CSS class as the Sessions select-mode bar. Created lazily when items are selected; removed when selection clears or when leaving the autonomous view. Was previously an inline `position: sticky` div inside the panel content, which scrolled with the list and obscured the cards being acted on.
- **BL246-followup** (`internal/server/web/app.js` `renderAutonomousView` + `switchAutomataTab`) — removed the redundant `automataLaunchBtn` (`⚡ Launch Automation`) from the Automata tab header; the floating ⚡ FAB shipped in v6.5.1 already covers it.

## Changed

- `navigate()` cleanup path — when leaving the `autonomous` view, the Automata batch bar is removed and `_automataState.selectMode` / `selected` are reset (mirrors the existing Sessions select-mode cleanup).

## See also

CHANGELOG.md `[6.6.1]` entry.
