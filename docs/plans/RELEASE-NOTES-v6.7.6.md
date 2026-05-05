# Release Notes — v6.7.6 (Settings tab reorg + new Agents tab + padding fix)

Released: 2026-05-05
GH release: https://github.com/dmz006/datawatch/releases/tag/v6.7.6
Smoke: 95/0/6

## Summary

Settings tab reorganization per operator request, plus a padding fix on Session Templates + Device Aliases cards.

## Changed

- **Settings tab order** — Plugins moved to position 2 (between General and Comms). New **Agents** tab added between LLM and Automata.

  | Was | Is |
  |---|---|
  | General · Comms · LLM · Plugins · Automata · About (6 tabs) | General · Plugins · Comms · LLM · **Agents** · Automata · About (7 tabs) |

- **Cards moved from General → Agents** (4 cards, 5 settings-sections — Tailscale Config + Mesh Status share an outer wrapper):
  - Project Profiles (`gc_projectprofiles`)
  - Cluster Profiles (`gc_clusterprofiles`)
  - Container Workers (`gc_agents`)
  - Tailscale Configuration + Mesh Status (`tailscale_config` + `tailscale_status`)

- **Locale** (5 bundles) — new key `settings_tab_agents`:
  - en: "Agents"
  - de: "Agenten"
  - es: "Agentes"
  - fr: "Agents"
  - ja: "エージェント"

## Fixed

- **Session Templates + Device Aliases card content had no padding.** Both `loadTemplatesPanel` and `loadDeviceAliasesPanel` rendered their content directly into bare `<div id="templatesList">` / `<div id="deviceAliasesList">` containers, leaving the rendered content flush against the card edge. Wrapped the rendered content (and the error-state HTML) in `<div style="padding:6px 12px;">` to match the project's standard card-content inset (same value as Stats / Audit / KG cards).

## What didn't change

- No backend changes. No new API endpoints. No new card content — just the cards' parent tab.
- No persisted-state migration needed: `cs_settings_tab` values that operators have stored (`general`, `comms`, `llm`, `plugins`, `automata`, `about`) all still resolve to existing tabs. New `agents` value gets created on first click.

## Mobile parity

Per the new Mobile-Parity Rule (added 2026-05-05): file an issue on `dmz006/datawatch-app` for the tab reorg + padding fix so mobile mirrors the new structure.

## See also

CHANGELOG.md `[6.7.6]` entry.
