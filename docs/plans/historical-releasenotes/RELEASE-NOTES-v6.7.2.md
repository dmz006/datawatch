# Release Notes — v6.7.2 (BL247-followup — Observer→Monitor unification)

Released: 2026-05-04
GH release: https://github.com/dmz006/datawatch/releases/tag/v6.7.2
Smoke: 95/0/6

## Summary

Patch closing the **missing half of BL247**. The original BL247 backlog entry (filed 2026-05-04, commit `2476e54`) listed two pieces of work:

1. **Observer→Monitor unification** — current Observer tab is unclear; refactor to a unified "Monitor" tab; move Observer details (peer list, envelope browser) into a card at the bottom of the new Monitor tab.
2. **Card migrations** — pipelines/autonomous/orchestrator → Automata, routing → Comms, secrets+tailscale → General.

Only #2 shipped in v6.5.1 (audited and verified clean: no orphaned cards, no duplicates). Item #1 was silently dropped — leaving the standalone Observer top-level nav view orphaned alongside Settings → Monitor.

This patch finishes BL247 by shipping the Observer→Monitor unification.

## Changed

- **`internal/server/web/app.js`** — `renderObserverView()` rewritten as a thin redirect that switches to Settings → Monitor and scrolls to the new Federated Peers card. The actual rendering moved to `renderObserverPeersCard(targetId)` so the same content lives inside the Monitor settings section. Loaded automatically when the Settings view paints.
- **`internal/server/web/app.js`** — `navigate('observer')` redirects to Settings → Monitor (matches the BL238 pattern used for plugins / routing / orchestrator).
- **`internal/server/web/app.js`** — startup hydration migrates `cs_active_view='observer'` → `'settings'` + `cs_settings_tab='monitor'` so operators with the old view persisted in localStorage land in the right place on first reload.
- **`internal/server/web/index.html`** — removed `navBtnObserver` (the standalone Observer nav button hidden-until-`/api/observer/stats`-responds gate from BL220-G1).
- **`internal/server/web/app.js`** — dropped the BL220-G1 visibility-check fetch on startup; the Federated Peers card is always present in Settings → Monitor and degrades gracefully when `/api/observer/stats` is unreachable.
- **Locale** (5 bundles) — new key `monitor_section_observer_peers` ("Federated Peers" / "Föderierte Peers" / "Pares federados" / "Pairs fédérés" / "連合ピア").

## What didn't change

- The peer list / stats / config rendering stayed identical — operators see the same content in a different home.
- Backend API surface is unchanged (`/api/observer/*` endpoints all still serve as before).
- No data migration; the move is purely PWA presentation.

## See also

CHANGELOG.md `[6.7.2]` entry.
