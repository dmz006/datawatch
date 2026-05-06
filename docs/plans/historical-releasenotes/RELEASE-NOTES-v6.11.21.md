# Release Notes — v6.11.21

Released: 2026-05-05
GH release: https://github.com/dmz006/datawatch/releases/tag/v6.11.21

### Summary

Two PWA session-detail enhancements based on operator feedback:

1. **Stats tab** — the mobile app's Sessions detail has a Stats panel (CPU ring, RSS, Threads, FDs, Net Rx/Tx, GPU, PID); the PWA didn't. Added.
2. **Font controls dropdown** — the four terminal-font buttons (A−, size, A+, Fit) now collapse into a single `Aa ▾` dropdown to declutter the toolbar.

### Added

- **`internal/server/web/app.js`** — third `Stats` output tab in session detail (alongside Tmux/Channel) with `renderSessionStats(sessionId)` consuming `state.statsData` from the existing `stats` WS frame; CPU usage rendered as an SVG ring, plus RSS / Threads / FDs / Net Rx-Tx / GPU / PID rows mirroring `SessionStatsPanel.kt`.
- **`internal/server/web/app.js`** — `term-font-dropdown` wrapping a `Aa ▾` button + collapsible menu containing the existing A−, size, A+, Fit controls; `toggleTermFontDropdown` / `closeTermFontDropdown` with outside-click dismissal.
- **`internal/server/web/style.css`** — `.term-font-dropdown`, `.term-font-menu`, `.term-font-size` rules.

### Mobile parity

The mobile app already has the Stats panel and a single font-control affordance, so this release is the PWA catching up. No new datawatch-app issue required.
