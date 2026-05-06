# Release Notes — v6.7.7 (BL261 — Settings → Automata tab card padding)

Released: 2026-05-05
GH release: https://github.com/dmz006/datawatch/releases/tag/v6.7.7
Smoke: 95/0/6

## Summary

BL261 — v6.7.6 padding-fix follow-up. Three more cards in Settings → Automata tab had the same bare-container root cause as the v6.7.6 templates/aliases fix and were missed in that sweep.

## Fixed

- **Settings → Automata tab card content padding** (`internal/server/web/app.js`):
  - `loadPipelinesPanel` / `pipelinesPanel` (~line 13123) — wrapped loading, populated, empty, and error states in `<div style="padding:6px 12px;">`.
  - `loadOrchestratorPanel` / `orchestratorPanelBody` (~line 12219) — same wrap.
  - `loadSkillsPanel` + `_renderSkillsRegistries` / `automataSettingsSkillsPanel` (~lines 12343 / 12359 / 12407) — same wrap on loading, empty, populated, and error states.

  All three now match the Stats / Audit / KG / Templates / Aliases card inset.

## What didn't change

- No backend changes. No new API endpoints. No locale keys (pure layout fix).
- No persisted-state migration.

## Mobile parity

[`datawatch-app#57`](https://github.com/dmz006/datawatch-app/issues/57) — same inset fix needed for the equivalent cards on the Compose Multiplatform side.

## See also

CHANGELOG.md `[6.7.7]` entry.
