# Release Notes — v6.6.0 (BL252 close + BL246 close)

Released: 2026-05-04
GH release: https://github.com/dmz006/datawatch/releases/tag/v6.6.0
Smoke: 91/0/6

## Summary

Minor release closing **two operator-priority backlog items** plus collecting v6.5.x patch closures:

1. **BL252 — PWA i18n full coverage (closes GH#32)** — 7 phases shipped over the v6.5.5 → v6.6.0 series totaling ~190 new locale keys across all 5 bundles (en/de/es/fr/ja) with inline translations.
2. **BL246 — Automata UX overhaul** — items 1, 5, 6 shipped this release (items 2/3/4/7 closed v6.5.1).

## BL252 — PWA i18n full coverage (closes GH#32)

7 phases across the v6.5.x series, totaling **~190 new locale keys** across all 5 bundles:

- **Phase 1+2** (v6.5.5) — sessions list, session detail toolbar, chat role labels, Mermaid renderer, schedule-input popup, timeline panel, new-session form, channel help (53 keys)
- **Phase 3+4** (v6.5.6) — PRD lifecycle strip + CRUD modals + stories/tasks tree; Stats card section headings; Alerts empty states (70 keys)
- **Phase 5** (v6.5.7) — Settings panel: auth, servers, communications, About, dynamic update strings (24 keys)
- **Phase 6** (v6.6.0) — header nav titles, FAB titles, session detail buttons, input placeholders, terminal connection states, voice input states (26 keys)
- **Phase 7** (v6.6.0) — final sweep across status indicators, update progress, Start Session, server picker, LLM/log/config/memory unavailable states, memory tools, audit + analytics empty states, Signal device link states, KG queries, toast messages (43 keys)

## BL246 — Automata UX overhaul (fully closed v6.6.0)

**Items 1, 5, 6 shipped this release** (items 2, 3, 4, 7 closed v6.5.1):

- **Item 1 — tabbed detail view.** Detail page is now a 4-tab surface (**Overview** / **Stories** / **Decisions** / **Scan**) with a persistent header toolbar that stays visible across all tabs.
- **Item 5 — select-mode toggle.** Per-card checkboxes are hidden by default; the new ✓ **Select** toolbar button reveals them and the batch-action bar (matches the Sessions pattern).
- **Item 6 — every PRD API verb has a visible PWA affordance:**
  - Header toolbar: ✎ Edit Spec · ⚙ Settings · ↺ Request Revision · ⌗ Clone to Template · 🗑 Delete
  - New Settings modal — type / backend / effort / model / skills / guided mode in one form; only PATCHes the fields that changed
  - Stories tab switched to the rich `renderStory()` so per-story (Edit / Profile / Files / Approve / Reject) and per-task (Edit / LLM / Files) buttons are visible inline
  - Scan tab carries a help block describing what Run Scan does (SAST · secrets · dependencies · LLM grader)
  - Decisions tab rows expand to show the raw decision details payload

## Other PWA work bundled

- **BL247** (v6.5.1) — Settings tab + card reorganization (Routing→Comms, Orchestrator→Automata, etc.)
- **BL249** (v6.5.1) — Session auto-reconnect after daemon restart
- **BL250** (v6.5.1) — Session state refresh after Input Required popup dismiss

## Mobile parity

datawatch-app issues #46 (BL252 i18n keys), #47 (BL246 layout), #48 (BL247), #49 (BL249/BL250) filed.

## See also

CHANGELOG.md `[6.6.0]` entry.
