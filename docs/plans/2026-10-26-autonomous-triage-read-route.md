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
- **CLI:** `datawatch autonomous triage [--automation ID] [--prd ID] [--status failed|blocked] [--json]` —
  the same read over the command line, same filters, same canonical status
  refusal, rendered through the shared formatter.
- **comm channel:** `autonomous triage [automation-id]` — the same read
  over the chat channel; the optional Automation id narrows the read.
- **One wording everywhere:** the CLI, the comm reply and the triage
  alert body all render `TriageRecord` through the shared formatter in
  `internal/autonomous/triage_text.go`, and `triageAlertBody` delegates
  to it — so a record shows the same standing wherever the operator
  reads it and on the alert itself.
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
- `internal/autonomous/triage_text.go` (new) — the shared formatter:
  `ParseTriageList`, `RenderTriageText` (the list and the threshold,
  with the "no records" text) and `RenderTriageRecord` (one record in
  full: identity, membership, the failed or blocked task with error and
  end reason, the block reason, the retry budget, the stamps, the notes).
- `cmd/datawatch/cli_triage.go` (new) — the `triage` subcommand of
  `datawatch autonomous`; registered in `cmd/datawatch/cli_autonomous.go`.
- `internal/router/triage_comm.go` (new) — the `triage` verb of the
  autonomous comm command; routed in `internal/router/sx2_parity.go`.
- `internal/autonomous/triage_pass.go` — `triageAlertBody` now delegates
  to the shared formatter, so the alert body is the record's text.
- Tests: `internal/server/triage_rest_test.go` (new) and
  `internal/mcp/triage_tools_test.go` (new).
- Tests: `internal/autonomous/triage_text_test.go` (the shared
  formatter; `TestRenderTriageRecord_MatchesTriageAlertBody` pins the
  alert-body contract), `cmd/datawatch/cli_triage_test.go` (filters, local
  status refusal, `--json` pass-through, daemon refusal, registration),
  `internal/router/triage_comm_test.go` (rendering, the Automation
  filter, argument refusal, daemon refusal),
  `internal/router/triage_parity_test.go` (`TestTriageCommVerbIsRegistered`
  — the verb parses and is listed in the autonomous usage) and
  `cmd/datawatch/lane_fill_parity_test.go` (`TestTriageParity` — all four
  surfaces listed together).

## Parity surface

The feature touches the operator-interface surfaces. Per the parity rule,
the full surface set is named here — `REST`, `MCP`, `CLI`, `comm channel`,
`YAML/config`, `PWA`, `Android`, `iPhone/iOS` — with each excluded surface
carrying a reason.

| Surface | This slice | Reason |
|---|---|---|
| **REST** | Shipped — `GET /api/autonomous/triage`. | The read the feature is about: filters `automation_id`, `prd_id`, `status`; a wrong `status` is a 400; no match lists empty, never 404; the autonomous read capability, exactly like the sibling reads. |
| **MCP** | Shipped — `autonomous_triage`. | The operator-facing tool surface of the read: read-only hint, optional `automation_id`, `prd_id`, `status`, mapping to the autonomous read capability and proxying the route. |
| **CLI** | Shipped — `datawatch autonomous triage [--automation ID] [--prd ID] [--status failed\|blocked] [--json]`. | The operator-facing CLI surface of the read: the same filters as the route, the status refused locally with the two allowed values (nothing reaches the daemon on a wrong one), `--json` prints the body untouched. The record's standing renders through the shared formatter. Pinned: `TestTriageParity` and `cmd/datawatch/cli_triage_test.go`. |
| **comm channel** | Shipped — `autonomous triage [automation-id]`. | The operator-facing chat surface of the read: one optional Automation id narrows the list; the reply is the shared formatter's text (the record's standing and the threshold), the same as the CLI and the alert. Pinned: `TestTriageParity`, `internal/router/triage_parity_test.go`, `internal/router/triage_comm_test.go`. |
| **YAML/config** | No new item. | The read reports the existing `autonomous.automation.triage_alert_minutes` setting (added by the settings task in this story): no new key, no new value, nothing to configure. |
| **PWA** | Excluded. | A triage card on the Automation sub-tab is an operator-client feature. Client parity (PWA == Android == iPhone) ships as one client slice in the later pass, so the phones are not left behind the PWA. The record is already visible on the Automaton itself, so no client surface is hiding a fact that was not already reachable. |
| **Android** | Excluded. | Same reason as the PWA: client surfaces ship together, later. |
| **iPhone/iOS** | Excluded. | Same reason as the PWA: client surfaces ship together, later. |

The exclusions are deliberate, not an oversight: the four surfaces that
are shipped (REST, MCP, CLI, comm) are fully built and tested, the rest
are named with a stated reason, and every shipped surface is pinned by
`TestTriageParity` (the cmd half) and `TestTriageCommVerbIsRegistered`
(the router half), so no surface can drift silently.

## Checks that passed (2026-10-26, Go 1.27.2, `env -u TMUX -u TMUX_TMPDIR`)

- `go test ./internal/server/ -run 'Triage' -count=1` — all pass.
- `go test ./internal/mcp/ -run 'Triage' -count=1` — all pass.
- `go test ./internal/federation/ -count=1` — pass.
- `TestA2_EveryRegisteredRouteHandlerIsCapabilityGated` — pass.
- `TestEveryUnconditionallyRegisteredToolHasAnMCPToolCapEntry` — pass.
- `go test ./...` (whole repository) — pass, no failures.
- CLI + comm slice (2026-11-04): `go test ./internal/autonomous/ ./internal/router/ ./cmd/datawatch/ -run 'Triage'` —
  all pass: the shared formatter, the alert-body contract
  (`TestRenderTriageRecord_MatchesTriageAlertBody`), the CLI filters,
  refusal and pass-through, the comm rendering and argument handling, and
  both halves of `TestTriageParity` / `TestTriageCommVerbIsRegistered`.

## Not done here

- No live daemon check against a real store (tracker row says
  Validated=No).
- The operator how-to and the config reference are left to the same
  documentation pass as the rest of the autonomous reads.
