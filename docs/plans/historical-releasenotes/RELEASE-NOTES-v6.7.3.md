# Release Notes — v6.7.3 (BL247-followup direction correction)

Released: 2026-05-04
GH release: https://github.com/dmz006/datawatch/releases/tag/v6.7.3
Smoke: 95/0/6

## Summary

Patch correcting the Observer↔Monitor direction. v6.7.2 implemented the BL247 Observer→Monitor unification half by **folding the standalone Observer view into a card inside Settings → Monitor** — the wrong direction. The original BL247 wording ("refactor to a unified Monitor tab that includes the existing monitor/stats content; move current Observer details … into a card at the bottom of the new Monitor tab") was ambiguous; operator clarification confirmed the intent was the inverse: keep the Observer top-level nav, fold the Settings → Monitor sub-tab content into it, and the Federated Peers content becomes a card at the bottom of the new Observer view.

This patch inverts.

## Changed

- **`renderObserverView()`** — restored as a real top-level view. Now renders 10 cards in this order: System Statistics (with eBPF, Plugins, Federated peers, Cluster nodes, MCP channel bridge sub-blocks), Memory Browser, Memory Maintenance, Scheduled Events, Global Cooldown, Session Analytics, Audit Log, Knowledge Graph, Daemon Log, **Federated Peers** (the original Observer-specific content) at the bottom. Uses the same `settings-section` + `settingsSectionHeader` shape so collapse-state and docs links work identically. Card loaders (`loadStatsPanel`, `listMemories`, `loadSchedulesList`, `loadCooldownStatus`, `loadAnalyticsPanel`, `loadAuditPanel`, `loadKgPanel`, `renderObserverPeersCard`) fire after innerHTML assignment.
- **`renderSettingsView()`** — removed all 10 `data-group="monitor"` settings-section blocks. Removed `monitor` from the `tabBtns` array. Settings tab bar now: **General · Comms · LLM · Plugins · Automata · About** (6 tabs, was 7). Card loaders for the moved cards removed from the post-render batch since those targets no longer exist in Settings.
- **`navigate('observer')`** — restored to render the Observer view directly (no longer redirects to Settings).
- **Startup localStorage migration** — `cs_settings_tab='monitor'` (left over from any pre-v6.7.3 state) now sets `cs_active_view='observer'` and clears the sub-tab. Reverses v6.7.2's wrong-direction migration.
- **`index.html`** — restored Observer top-level nav button. Always visible (not gated on `/api/observer/stats` the way the BL220-G1 era gated it).

## What didn't change

- Backend API surface (`/api/observer/*`) untouched.
- The card render functions themselves (`loadStatsPanel`, `loadAuditPanel`, etc.) untouched — they paint into the same target div ids; the divs just live in a different container now.
- All locale keys still present; `settings_tab_monitor` kept in all 5 bundles for mobile-parity transition.

## Process note

This is the third instance in a single session where I shipped my interpretation of an ambiguous backlog item without surfacing the ambiguity first. Saved as a durable rule in operator memory (`feedback_backlog_is_spec.md`): backlog text is the spec; ambiguity → ask, deviation → propose, never decide-then-ship.

## See also

CHANGELOG.md `[6.7.3]` entry.
