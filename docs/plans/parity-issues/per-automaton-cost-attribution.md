# Mobile parity draft — per-Automaton cost attribution

- **Status:** draft (file only; nothing filed on any tracker)
- **Target repo:** dmz006/datawatch-app
- **Suggested title:** feat(parity): per-Automaton cost report covers all spend (ledger-backed, attributed vs unattributed)

## Affected area

The cost report: the Automaton and group view of tokens, time, sessions and tool calls. It is served by the REST cost report in `internal/server/cost_report.go` and `internal/server/autonomous.go`, documented in `docs/api/openapi.yaml`, and exposed through the MCP tools in `internal/mcp/cost_report_tools.go`, the CLI in `cmd/datawatch/cli_cost_report.go`, and the comm-channel verb in `internal/router/commands.go`. The mobile app (datawatch-app) is the surface this draft is about.

## Current behavior

Before this work, the per-Automaton cost view counted only spend that could be traced from a session back to its owning Automaton. Spend from sessions the Automaton launched but that were not linked (verifier, guardrail and retry sessions, and similar) was missing, so the totals understated what an Automaton cost. The operator expects tokens, time, sessions and tool calls for every Automaton-launched session.

The report source now reads the spend ledger. Each ledger entry carries its own Automaton id, which is resolved to the owning automation and release through the PRD map. An entry with no Automaton id falls back to the session-to-owner lookup. `group_by=automaton` prices the same spend as the PRD view. An attribution check reports Automaton-launched sessions that have no owner. The same data is reachable over REST, MCP, CLI and the comm channel. The mobile app does not yet show it.

## Expected behavior

The mobile app shows the same per-Automaton cost as the other surfaces:

- Per-Automaton totals for tokens, time, sessions and tool calls, using the ledger-backed numbers from the cost report endpoint (including `group_by=automaton`).
- Spend that cannot be attributed to an Automaton is shown as its own "unattributed" line, not silently dropped or folded into another row.
- Numbers match what `datawatch cost-report`, the MCP cost report tool and the comm-channel cost verb return for the same period.
- Dollar figures are API-equivalent only, as on the other surfaces, because the operator is on a flat plan.

## Gaps this work leaves

- The mobile app has no Automaton cost screen or card.
- The mobile app has no view of the attribution check, so unattributed Automaton sessions are invisible there.
- The new request and response fields are in `docs/api/openapi.yaml` but not yet in the mobile client models.

## Suggested next step

Use the OpenAPI additions as the contract and add a per-Automaton cost view to the mobile app that calls the cost report endpoint with `group_by=automaton`. Add an unattributed row and a link to the attribution check output. Where this view lives in the app (a new card, a tab or a sub-tab) is a design question, so ask the operator before building it. File the parity issue on dmz006/datawatch-app when the change merges. Do not file it from this task.
