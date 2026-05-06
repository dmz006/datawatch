# Release Notes — v6.5.7 (BL252 Phase 5 — Settings panel i18n)

Released: 2026-05-04
GH release: https://github.com/dmz006/datawatch/releases/tag/v6.5.7
Smoke: 87/0/6

## Summary

Patch release delivering **BL252 Phase 5** — PWA i18n coverage for the Settings panel.

## Added

- **BL252-P5** (`internal/server/web/app.js`) — i18n: Settings auth section (browser token, save & reconnect, server bearer token, MCP SSE bearer token); servers section (status label, this server); backends/comms section (Signal Device, Checking…, Link Device); About section (title, language, auto default, version, update, check now, daemon, restart, sessions in store, project, mobile app, branding/splash, orphaned tmux sessions); dynamic `checkForUpdate()` strings (Checking… and Check failed).
- **BL252-P5** (locales) — 24 new keys in all 5 bundles: `settings_browser_token`, `settings_save_reconnect`, `settings_server_token`, `settings_mcp_token`, `settings_status_label`, `settings_this_server`, `settings_signal_device`, `settings_checking`, `settings_link_device`, `settings_about_title`, `settings_language`, `settings_lang_auto`, `settings_version`, `settings_update`, `settings_check_now`, `settings_check_failed`, `settings_daemon`, `settings_restart`, `settings_sessions`, `settings_sessions_in_store`, `settings_project`, `settings_mobile_app`, `settings_branding`, `settings_orphaned_tmux`.

## See also

CHANGELOG.md `[6.5.7]` entry.
