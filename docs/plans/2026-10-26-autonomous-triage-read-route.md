# Autonomous triage records — readable through one endpoint

Date: 2026-10-26. Status: implemented and tested (unit level; no live check).

## What was wanted

The triage records for failed and blocked Automata were only readable as files on the daemon's host. An operator (or an operator-facing tool) should be able to read them the same way the other autonomous reads work, through the daemon's own surfaces.

## What was decided

- **Route:** `GET /api/autonomous/triage` — a sibling of `GET /api/autonomous/lane-fill`.
- **Response:** the records newest first, with the alert threshold in the same body:

  ```json
  {
    "records": [ {...} ],
    "alert_minutes": 30
  }
  ```

  Each record is the stored triage record: `prd_id`, `prd_title`, `status`,
  `automation_id`, `automation_name`, `task_id`, `task_title`, `error`,
  `blocked_reason`, `session_end_reason`, `retry_budget`, `first_seen_at`,
  `updated_at`, `alerted_at`, `notes`.
- **Filters, all optional:** `automation_id`, `prd_id`, and `status` (where
  `status` may only be `failed` or `blocked`). A filter that matches nothing
  lists nothing (`"records":[]`), never a 404.
- **A wrong `status` is refused** with a 400 that says which two values are
  allowed. The daemon is not asked to guess the intended one.
- **`alert_minutes`** is the daemon's effective triage alert threshold for
  the Automation — the existing `autonomous.automation.triage_alert_minutes` setting
  (default 30; an explicit 0 means the alert is off and 0 is reported).
  Nothing new is configured.
- **Capability:** the read capability already used by the other autonomous
  reads, exactly like every sibling read. The tool list check and the
  route gate check were extended by the code, not bypassed.
- **MCP tool:** `autonomous_triage`, same three parameters, proxy to the
  REST route exactly like the rest of the autonomous tools.
- No writes. The route touches nothing but the record list.

## What changed

- `internal/server/triage.go` (new) — the route and the filter and sort;
  registered in `internal/server/server.go`.
- `internal/autonomous/triage_record.go` — `ListTriageRecords` (the records
  for every failed or blocked Automaton, as copies) and
  `TriageAlertMinutes` (the effective threshold).
- `internal/mcp/triage_tools.go` (new) — the tool and the proxy; registered
  in `internal/mcp/server.go`.
- `internal/federation/mcp_tool_caps.go` — the tool's capability entry.
- Tests: `internal/server/triage_rest_test.go` (new) and
  `internal/mcp/triage_tools_test.go` (new).

## Checks that passed (2026-10-26, Go 1.27.2, `env -u TMUX -u TMUX_TMPDIR`)

- `go test ./internal/server/ -run 'Triage' -count=1` — all pass.
- `go test ./internal/mcp/ -run 'Triage' -count=1` — all pass.
- `go test ./internal/federation/ -count=1` — pass.
- `TestA2_EveryRegisteredRouteHandlerIsCapabilityGated` — pass.
- `TestEveryUnconditionallyRegisteredToolHasAnMCPToolCapEntry` — pass.
- `go test ./...` (whole repository) — pass, no failures.

## Not done here

- No live daemon check against a real store (tracker row says
  Validated=No).
- The operator how-to and the config reference are left to the same
  documentation pass as the rest of the autonomous reads.
