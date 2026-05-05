# Release Notes — v6.5.6 (BL252 Phases 3+4 — PRD management + Stats/Alerts)

Released: 2026-05-04
GH release: https://github.com/dmz006/datawatch/releases/tag/v6.5.6
Smoke: 87/0/6

## Summary

Patch release delivering **BL252 Phases 3+4** — PWA i18n coverage for Automata/PRD management and Stats/Alerts panels.

## Added

- **BL252-P3** (`internal/server/web/app.js`) — i18n: lifecycle strip buttons (Instantiate/Plan/Review/Approve/Reject/Request-revision/Run/Cancel/Done/Completed/Rejected/Cancelled/More-actions); PRD create modal (title, labels, placeholders); edit-story-files, edit-task-files, set-story-profile modals; edit-task, edit-story, set-LLM modals; Stories & tasks collapsible; No PRDs / No stories / No tasks empty states.
- **BL252-P3** (locales) — 52 new keys in all 5 bundles including shared `btn_cancel`, `btn_save`, `btn_close`, `btn_create` plus `prd_*` keys.
- **BL252-P4** (`internal/server/web/app.js`) — i18n: stats section headings (Daemon, Infrastructure, RTK Token Savings, Episodic Memory, Ollama Server, Session Statistics, Sessions, Chat Channels, LLM Backends), stats unavailable/not-available errors, alerts empty states (active/inactive/system), refresh button, load error.
- **BL252-P4** (locales) — 18 new keys in all 5 bundles: `stats_*` (9 keys) + `alerts_*` (6 keys) + `stats_unavailable`/`stats_not_available`.

## See also

CHANGELOG.md `[6.5.6]` entry.
