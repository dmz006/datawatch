# Release Notes — v6.11.22

Released: 2026-05-05
GH release: https://github.com/dmz006/datawatch/releases/tag/v6.11.22

### Summary

Two operator-reported regressions from v6.11.21:

1. **"Session detail tab has incorrect word in tab"** — the new Stats tab and its empty-state placeholder rendered the literal locale keys (`session_detail_tab_stats`, `session_detail_stats_no_data`, `session_stats_process_title`) because they weren't added to any locale bundle. The `t(key) || 'fallback'` pattern doesn't fall through when `t()` returns the key itself (truthy).
2. **"Blank screen until I send a command"** — entering a session whose tmux pane was already cleaned up (operator wasn't in the session when it ended) showed a perpetual loading splash. `CapturePaneANSI` errored, `StartScreenCapture` returned immediately for terminal-state sessions, no `pane_capture` frame fired, splash stayed.

### Fixed

- **`internal/server/web/locales/{en,de,es,fr,ja}.json`** — added `session_detail_tab_stats`, `session_stats_process_title`, `session_detail_stats_no_data` to all 5 bundles.
- **`internal/server/api.go` subscribe handler** — when `CapturePaneANSI` errors or returns empty, fall back to the `TailOutput` lines we already fetched and send them as a synthesized `pane_capture` so the PWA dismisses the splash.

### Mobile parity

[`datawatch-app#71`](https://github.com/dmz006/datawatch-app/issues/71) — same locale parity requested for the Compose Multiplatform side (Stats panel string keys) per the localization rule.
