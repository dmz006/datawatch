# Release Notes — v6.11.4 (BL257 P2 follow-up: scope 🤖 icon to Automata page)

Released: 2026-05-05
GH release: https://github.com/dmz006/datawatch/releases/tag/v6.11.4

## Summary

Operator-reported: the 🤖 Identity Wizard icon in the PWA header should only appear on the Automata page, not on every view.

## Changed

- **`internal/server/web/index.html`** — `headerIdentityBtn` defaults to `display:none`.
- **`internal/server/web/app.js`** `navigate()` — shows the button when `view === 'autonomous'`, hides it otherwise. Same conditional pattern as `headerSearchBtn` (sessions + autonomous only).

## What didn't change

- The Identity card itself (Settings → Automata → Identity, BL257 P1 + v6.11.1 tab placement) is available everywhere via the Settings tab.
- Wizard logic, locale keys, REST/MCP/CLI/comm surfaces unchanged.
- No backend changes.

## Mobile parity

[`datawatch-app#60`](https://github.com/dmz006/datawatch-app/issues/60) — same Automata-page-only scoping for the robot icon on the Compose Multiplatform app.

## See also

- CHANGELOG.md `[6.11.4]`
