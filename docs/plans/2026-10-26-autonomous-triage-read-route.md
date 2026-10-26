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

## Parity surface

The feature touches the operator-interface surfaces. Per the parity rule,
the full surface set is named here — `REST`, `MCP`, `CLI`, `comm channel`,
`YAML/config`, `PWA`, `Android`, `iPhone/iOS` — with each excluded surface
carrying a reason.

| Surface | This slice | Reason |
|---|---|---|
| **REST** | Shipped — `GET /api/autonomous/triage`. | The read the feature is about: filters `automation_id`, `prd_id`, `status`; a wrong `status` is a 400; no match lists empty, never 404; the autonomous read capability, exactly like the sibling reads. |
| **MCP** | Shipped — `autonomous_triage`. | The operator-facing tool surface of the read: read-only hint, optional `automation_id`, `prd_id`, `status`, mapping to the autonomous read capability and proxying the route. |
| **CLI** | Excluded. | The record already appears on the Automaton itself, in the reads an operator already has (including `datawatch autonomous get`). A `datawatch autonomous triage` verb is one part of a later operator-surface pass (CLI + comm + client cards together), so it is not added alone here. Pinned: `TestTriageParity` fails if a `triage` CLI subcommand appears before this section is updated. |
| **comm channel** | Excluded. | Same deliberate deferral as the CLI: the chat verb ships with the operator-surface pass, not with the read slice. Pinned: `TestTriageCommVerbIsExcluded` — `autonomous triage` is still a non-verb, answered with the autonomous usage without reaching the daemon. |
| **YAML/config** | No new item. | The read reports the existing `autonomous.automation.triage_alert_minutes` setting (added by the settings task in this story): no new key, no new value, nothing to configure. |
| **PWA** | Excluded. | A triage card on the Automation sub-tab is an operator-client feature. Client parity (PWA == Android == iPhone) ships as one client slice in the later pass, so the phones are not left behind the PWA. The record is already visible on the Automaton itself, so no client surface is hiding a fact that was not already reachable. |
| **Android** | Excluded. | Same reason as the PWA: client surfaces ship together, later. |
| **iPhone/iOS** | Excluded. | Same reason as the PWA: client surfaces ship together, later. |

The exclusions are deliberate, not an oversight: the two surfaces that are
shipped (REST, MCP) are fully built and tested, and the rest are named
with a stated reason; the two operator-facing exclusions (CLI, comm) are
pinned by tests so the exclusion cannot drift silently, and each pin
changes with this section when its slice lands.

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
