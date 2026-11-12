# PRD compact replies and detail levels

**Date:** 2026-10-18
**Version:** 9.5.12

**Scope:** Packages and files touched:

- `internal/autonomous`: `prd_view.go` (write view), `prd_view_summary.go` (summary view), `prd_view_tasks.go` (tasks view), and their tests.
- `internal/server`: `autonomous.go` (PRD GET and write handlers), `autonomous_view.go` (`writeAutonomousResult`), and their tests.
- `internal/mcp`: `sx_parity.go` (`autonomous_prd_get` `detail` argument and the compact-view query on PRD write calls in `proxyJSON`), and `autonomous_view_test.go`.
- `cmd/datawatch`: version bump only (`main.go`). The CLI `prd-get --detail` flag is not yet implemented.
- `docs`: `mcp.md`, `api-mcp-mapping.md`, `testing-tracker.md`, `CHANGELOG.md`.

## Phases

Each phase is one story, in implementation order. A phase is Done when its gate passes (the unit tests named in the gate column pass).

| # | Phase | Status | Shipped | Gate |
|---|-------|--------|---------|------|
| 1 | Summary and tasks detail views (`BuildSummaryView`, `BuildTasksView`) | Done | 9.5.12 | `TestBuildSummaryView`, `TestBuildTasksView` pass |
| 2 | Compact write view (`BuildWriteView`, `writeAutonomousResult`) on all PRD write cases | Done | 9.5.12 | `TestBuildWriteView_*`, `TestWriteCasesCompact*`, `TestResetTaskCompactReply` pass |
| 3 | REST `GET ?detail=summary\|tasks\|full` and unchanged REST default | Done (live daemon check pending) | 9.5.12 | `TestGetPRDDetailLevels`, `TestPRDRESTDefaultUnchanged` pass |
| 4 | MCP: write tools send `view=compact`; `autonomous_prd_get` defaults to `summary` | Done (live daemon check pending) | 9.5.12 | `TestMCPWriteToolsRequestCompactView`, `TestMCPPRDGetDetailArgument` pass |
| 5 | Documentation of the detail levels and compact replies | Done | 9.5.12 | `docs/mcp.md`, `docs/api-mcp-mapping.md`, `docs/testing-tracker.md` updated |
| 6 | Release: CHANGELOG entry and version bump | Done | 9.5.12 | `CHANGELOG.md` v9.5.12 entry; `Version` = 9.5.12 in `main.go` and `api.go` |

Open items:

- A live daemon check of the summary, tasks and full GET shapes and the compact write view (tracker row "Live check") is pending.
- The CLI `prd-get --detail` flag (phase 4 parity row) is not implemented in `cmd/datawatch`. The tracker records this as a gap.

## Parity surface

| Surface | Status | Notes |
|---------|--------|-------|
| REST | Included | Additive query parameters only: `detail` on GET, `view=compact` on writes. Defaults unchanged: GET is `full`, writes return the full reply unless `view=compact` is sent. |
| MCP | Included | Behaviour change: write tools always send `view=compact`, and `autonomous_prd_get` returns the `summary` view when `detail` is omitted. Clients that need the full Automaton must pass `detail: "full"`. |
| CLI | Included | `prd-get --detail` flag. Not yet implemented; see open items. |
| Comm channel | Excluded | Comm commands render their own text from the REST reply and keep the full response. No change this release. |
| YAML/config | Excluded | No configuration keys were added. |
| PWA | Excluded | The PWA requests the default full REST reply, which is unchanged. |
| Android | Excluded | Existing REST requests are unchanged, so no client change is needed. |
| iPhone/iOS | Excluded | Same reason as Android: existing REST requests are unchanged. |
