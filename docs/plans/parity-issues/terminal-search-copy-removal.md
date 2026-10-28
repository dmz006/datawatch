# [sync] terminal search and copy removed from the PWA (datawatch next release)

## Body sent with the issue

The session toolbar **search** and **copy** buttons that exist in the datawatch PWA do not need counterparts in the datawatch app, so they are being recorded here as a no-op parity item rather than a feature to build.

### What happened

The PWA session toolbar search and copy buttons were removed in the next release of datawatch. For the record: they were added in datawatch 8.51.0 as parity item **D69** (from issue #172 on dmz006/datawatch). They were browser-only UI — they read the rendered terminal session content in the browser and had **no server API, config, MCP, CLI, or OpenAPI surface** behind them.

### Consequence for the app

- There is no daemon-side API, setting, MCP tool, CLI flag, or OpenAPI endpoint to consume for these controls; the app cannot build them against a daemon contract.
- The PWA itself no longer ships them, so the parity gap they represented is closed on the datawatch side.
- **The app does not need to build terminal search and copy.** No code, locale keys, settings, or OpenAPI work is required. This issue exists purely so the removal is on record and the old D69 parity reference can be resolved as dropped rather than deferred.

### Acceptance

- None required. If any D69 references to terminal search/copy remain in app planning docs or issue trackers, close or annotate them as dropped.

## Filing record

Filed: https://github.com/dmz006/datawatch-app/issues/295 (2026-10-28, via `gh issue create --repo dmz006/datawatch-app --title "[sync] terminal search and copy removed from the PWA (datawatch next release)"`).
