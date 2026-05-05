# Release Notes — v6.5.5 (BL252 Phases 1+2 — PWA i18n core sessions + new-session)

Released: 2026-05-04
GH release: https://github.com/dmz006/datawatch/releases/tag/v6.5.5
Smoke: 87/0/7

## Summary

Patch release delivering **BL252 Phases 1+2** — PWA i18n coverage for core session views and the session creation dialog. Wraps 53 hardcoded English strings across sessions list, session detail toolbar, chat role labels, Mermaid renderer, schedule-input popup, timeline panel, new-session form, and channel help. All 5 locale bundles extended.

## Added

- **BL252-P1** (`internal/server/web/app.js`) — i18n: session filter placeholder, select tooltip, empty-state heading, terminal toolbar tooltips (font ±, fit, scroll), tab labels (Tmux/Channel/Chat), channel help tooltip, send-via-channel tooltip, arrow-key tooltip, connecting splash text, chat empty-state hints, chat role labels (You/Assistant/System/Thinking), Mermaid diagram title and hint.
- **BL252-P1** (locales) — 21 new keys in all 5 bundles.
- **BL252-P2** (`internal/server/web/app.js`) — i18n: schedule-input popup (title, labels, when-placeholder, quick-button, submit), timeline panel (loading/empty/title/error), new-session form (title, description, name/task/profile/cluster/LLM/permission/model/effort/dir/resume fields, start button, backlog title, no-prev empty state), channel help heading.
- **BL252-P2** (locales) — 32 new keys in all 5 bundles.

## Changed

- **BL252-P1** (locales) — `session_detail_tab_tmux` and `session_detail_tab_channel` values updated to capitalised forms (Tmux/Channel) across all 5 bundles; now wired in app.js.

## Phases following in v6.5.x → v6.6.0

- v6.5.6 — Phases 3+4 (PRD + Stats/Alerts)
- v6.5.7 — Phase 5 (Settings)
- v6.6.0 — Phases 6+7 + BL252 close (~190 keys total across 7 phases)

## See also

CHANGELOG.md `[6.5.5]` entry.
