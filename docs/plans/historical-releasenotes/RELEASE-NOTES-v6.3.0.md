# Release Notes — v6.3.0 (BL244 Plugin Manifest v2.1)

Released: 2026-05-03
GH release: https://github.com/dmz006/datawatch/releases/tag/v6.3.0

## Summary

Minor release delivering **BL244 — Plugin Manifest v2.1** and **BL245 — schedule date display fix**. Plugins can now declare comm-channel commands (auto-routed by the router via a new `PluginRegistry` interface), CLI subcommands, mobile endpoints, and session injection (context prepend). Backlog plugin manifests gain four new optional sections; existing v2.0 manifests parse cleanly.

## Added — BL244 Plugin Manifest v2.1

- **`comm_commands`** — plugins declare comm-channel command names + routes; `Router` auto-routes via new `PluginRegistry` interface (no daemon hardcoding).
- **`cli_subcommands`** — `datawatch plugins run <name> <sub>` looks up the route from the manifest and proxies it; `datawatch plugins mobile-issue <name>` prints a formatted `datawatch-app` issue body from manifest mobile endpoints.
- **`mobile`** (`MobileDecl` + `MobileEndpoint`) — declare REST endpoints for mobile clients; rendered in PWA plugin detail view; `DatawatchAppIssue` field tracks the corresponding app issue.
- **`session_injection`** (`types` + `context_prepend`) — autonomous executor calls `Manager.SetContextFn` at spawn time and forwards `SpawnRequest.ContextPrepend` to the worker.
- **MCP**: `plugin_run_subcommand` parity with `plugins run` CLI.
- **PWA**: plugin detail view shows `comm_commands`, `cli_subcommands`, `mobile`, `session_injection` sections when present.
- **Locale**: 4 new keys (`plugin_detail_*`) across all 5 bundles.

## Fixed — BL245

- Schedule "on next prompt" was being rendered as "12/31/1, 7:03:58 PM" in the PWA. Root cause: Go zero time `0001-01-01T00:00:00Z` is a truthy JS string and `new Date()` returns year 1 CE, bypassing the existing truthiness guard. Fix: `_fmtScheduleTime()` helper checks `getFullYear() < 2000`. (Originally shipped in v6.2.1; kept in v6.3.0 release notes for context.)

## See also

CHANGELOG.md `[6.3.0]` and `[6.3.1]` entries for the full detail.
