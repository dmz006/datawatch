# Usage calculator auto lanes

Date: 2026-11-02
Version at planning time: 9.67.0 (read from the `var Version` line in `cmd/datawatch/main.go`; recorded, not bumped — the release version is taken at merge time)

## Scope

The daemon applies the usage calculator's lane advice to the Claude lanes it runs, on its own: clamped to at least one lane, never above the operator maximum, and logged. This plan records the five surfaces that expose it: the status read (REST), the MCP status tool, the CLI `usage auto-lanes` verbs, the comm-channel and config settings, and the PWA controls and status line. No separate design document exists; this plan is the design record.

Out of scope: any change to the advice maths, and the mobile apps (tracked in the parity issue below).

## Phases

| Phase | Work | Status |
|---|---|---|
| 1 | Lane rule and hourly loop (`internal/usagecalc/autolanes.go`), decision log | Done |
| 2 | Settings `usage.calculator.auto_lanes`, `auto_lanes_max`, `auto_lanes_special_priority` | Done |
| 3 | `GET /api/usage/autolanes` status read and OpenAPI | Done |
| 4 | CLI `datawatch usage auto-lanes` and MCP `usage_autolanes_status` | Done |
| 5 | PWA controls and status line, five locale bundles | Done |
| 6 | Howto, operations guide and README | Done |
| 7 | Plan, parity status, parity issue, changelog, testing tracker | Done |
| 8 | Android and iPhone/iOS parity (issue text drafted in the parity issue file, not yet filed; the supervisor files it at merge; app work pending) | Planned |

## Parity surface

| Surface | Touched or excluded | Detail |
|---|---|---|
| REST | Touched | `GET /api/usage/autolanes` (enabled, policy, operator maximum, current cap, pending decision, recent decisions); both OpenAPI copies |
| MCP | Touched | Read-only `usage_autolanes_status`, capability `usage:read` |
| CLI | Touched | `datawatch usage auto-lanes status` and `set --on/--off --max N` |
| comm channel | Touched | The settings are reachable through the existing config verbs; no new verb |
| YAML/config | Touched | `usage.calculator.auto_lanes`, `auto_lanes_max`, `auto_lanes_special_priority` |
| PWA | Touched | Settings toggle and maximum, and the auto-lanes status line; locale keys in de, en, es, fr, ja |
| Android | Excluded from this change | The companion app is a separate repository; its work is drafted in [parity-issues/2026-11-02-usage-auto-lanes.md](parity-issues/2026-11-02-usage-auto-lanes.md), not yet filed |
| iPhone/iOS | Excluded from this change | Same reason; the iPhone app has no calculator screen yet, so the drafted text in [parity-issues/2026-11-02-usage-auto-lanes.md](parity-issues/2026-11-02-usage-auto-lanes.md) is not yet filed |
