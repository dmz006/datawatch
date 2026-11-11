# Parity issue draft — Per-Automaton cost completeness

Draft only. No issue has been filed on any tracker; the operator or supervisor files it.

- **Suggested title:** feat(parity): cost report window notes, unknown-spend flag and attribution check
- **Suggested repo:** dmz006/datawatch-app

## Affected area

The cost report: `internal/costreport/report.go`, `internal/server/cost_report.go` (the report source now reads the spend ledger), and the attribution check in `internal/costreport/attribution.go`. The window and unknown-spend notes are produced by the report engine in `report.go`. The mobile app's cost views read this report and the per-Automaton grouping.

## Current behavior

The server report now covers the tokens, time, sessions and tool calls of every Automaton-launched session, not only worker sessions, by reading the spend ledger. New optional fields appear in the response:

- Per row: `window_note` and `spend_unknown` / `spend_unknown_note`, set when a row began before the requested window or the window reaches before the ledger horizon.
- Top level: `retention_note` (always present), `window_note`, `unknown_spend_note` and `unknown_before` (the ledger horizon).
- An attribution check result: `automaton_launched`, `unattributed`, `session_ids`, `ok` and `note`. It counts ledger sessions launched by Automaton machinery that carry no automaton id.

The mobile app ignores all of these fields. Unknown spend before the horizon can therefore look like zero spend, and unattributed Automaton sessions show only as a generic "unattributed" group with no explanation.

## Expected behavior

The mobile cost views show the same truth as the PWA:

- The retention note is visible on the cost view.
- A partial-window row shows its `window_note`.
- A row or total with `spend_unknown` shows "unknown", never zero, with its note and the horizon date.
- When the attribution check is not `ok`, the view shows its note and a way to list the unattributed session ids.

All fields are optional. An older server that omits them must render exactly as today.

## Suggested next step

File one parity issue in the mobile repo, linking this draft. Have the mobile cost screen model decode the new optional fields. Add the retention note, the per-row and top-level notes, and the unknown-spend state to the existing cost layouts. Add the attribution banner. Then record the contract in the mobile parity matrix. Ask the operator where the banner and the session id list should sit before building, since placement is a design question.
