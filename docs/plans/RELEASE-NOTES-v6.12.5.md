# Release Notes — v6.12.5

Released: 2026-05-06
GH release: https://github.com/dmz006/datawatch/releases/tag/v6.12.5

### Fixed

- **In-session yellow "Input Required" banner flashed on every WS sessions broadcast** even with `suppressActiveToasts` on. v6.12.4's multi-tab BroadcastChannel suppression covered the toast (`handleNeedsInput`) but not the banner (`buildNeedsInputBannerHTML` → `refreshNeedsInputBanner`). The banner re-renders on every state oscillation so when state ping-pongs Running↔WaitingInput within seconds the banner appears + vanishes repeatedly — visible as a flash. Now: when `suppressActiveToasts` is on AND the operator has the session in active focus on this tab OR any sibling tab (via the BroadcastChannel presence map), `buildNeedsInputBannerHTML` returns empty. The xterm input-bar's `.needs-input` yellow border is kept as the visual cue.

### Tests

Per the patch-vs-minor rule: PWA-only change, no daemon code touched. JS syntax check (`node --check internal/server/web/app.js`) passes; full regression deferred to next minor (v6.13.0).
