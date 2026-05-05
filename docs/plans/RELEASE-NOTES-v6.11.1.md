# Release Notes — v6.11.1 (BL257-BL260 cards: Agents → Automata)

Released: 2026-05-05
GH release: https://github.com/dmz006/datawatch/releases/tag/v6.11.1
Smoke: 100/0/7 (no smoke regression — JS-only patch)

## Summary

Move the four BL257-BL260 cards from the Agents tab to the Automata tab per operator directive. These are automation primitives and belong with Pipeline Manager, PRD Orchestrator, Scan Framework, Skill Registries, and Autonomous Config — not with operator-self-description (Project Profiles, Cluster Profiles, Container Workers, Tailscale).

## Changed

- **`internal/server/web/app.js`** — flipped `data-group="agents"` → `data-group="automata"` on all four cards:
  - Identity (BL257 P1)
  - Algorithm Mode (BL258)
  - Evals (BL259 P1)
  - Council Mode (BL260)

The 🤖 robot icon in the PWA header (BL257 P2 wizard entry) is unchanged — header-level entry points are tab-independent.

## What didn't change

- Backend logic identical to v6.11.0.
- All REST/MCP/CLI/comm/locale surfaces unchanged.
- No new dependencies, no schema migration.

## Mobile parity

[`datawatch-app#58`](https://github.com/dmz006/datawatch-app/issues/58) filed: same Agents → Automata move on the Compose Multiplatform side.

## Why this happened

Original placement assumed the v6.7.6 Agents tab would carry both "who is the operator" (Identity) and "automation primitives" (Algorithm/Evals/Council). Operator clarified after the v6.11.0 ship that Automata is the canonical home for automation primitives — Identity rides along since it informs automation behavior.

## See also

- CHANGELOG.md `[6.11.1]`
