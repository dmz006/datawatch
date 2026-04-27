# datawatch v5.20.0 — release notes

**Date:** 2026-04-26
**Spans:** v5.19.0 → v5.20.0
**Closed:** Documentation alignment sweep — items 1+2 from the audit doc

## What's new

Pure documentation release. Closes the operator's audit finding that several MCP tools, REST endpoints, and CLI commands shipped in v5.7.0 → v5.19.0 without updating the doc surfaces.

### docs/mcp.md

- Updated tool count from "41 tools" (stale) to "100+ tools" with a pointer to `GET /api/mcp/docs` as the authoritative live list.
- Added a tool-families table covering Sessions / Autonomous PRDs / Orchestrator / Pipelines / Memory+KG / Observer / Agents / Plugins / Profiles+Projects / Templates+Scheduling / Devices+Routing / Cost+Audit+Config / Saved-commands / Ask+Assist / Voice.
- Called out the v5.9 → v5.19 additions: `autonomous_prd_children`, `observer_envelopes_all_peers`.

### docs/cursor-mcp.md

- Tools table extended beyond the original five session-management tools to include the autonomous PRD lifecycle, observer cross-host view, memory + KG, orchestrator, and pipeline tool families.
- Pointer to docs/mcp.md for the family breakdown.

### docs/api/autonomous.md

- Documented every REST endpoint added since v5.2.0:
  - `POST /api/autonomous/prds/{id}/{approve,reject,request_revision,edit_task}` (BL191 Q1)
  - `POST /api/autonomous/prds/{id}/instantiate` (BL191 Q2)
  - `POST /api/autonomous/prds/{id}/{set_llm,set_task_llm}` (BL203)
  - `GET /api/autonomous/prds/{id}/children` (BL191 Q4)
  - `PATCH /api/autonomous/prds/{id}` (v5.19.0 edit)
  - `DELETE /api/autonomous/prds/{id}?hard=true` (v5.19.0 hard-delete)

### docs/api/observer.md

- Documented `GET /api/observer/envelopes/all-peers` (BL180 cross-host, v5.12.0) with the `{by_peer: …}` response shape.
- Added every observer MCP tool name to the `MCP tools` bullet (was just listing 5; now lists the full surface incl. `observer_envelopes_all_peers`, `observer_peers_*`, `observer_agent_*`, `ollama_stats`).
- Added every observer CLI subcommand to the `CLI` bullet.

### internal/server/web/openapi.yaml

- Added `/api/autonomous/prds/{id}/children` GET path.
- Added `PATCH /api/autonomous/prds/{id}` operation with body schema (title/spec/actor).
- Documented the `?hard=true` query parameter on the existing DELETE.
- Added `/api/observer/envelopes/all-peers` GET path with description + response shape.
- Sync'd `docs/operations.md`-adjacent text via `make sync-docs` so the embedded PWA docs viewer carries the updated specs.

## Tests

No code changes; 1376 still passing. This release is a docs-only patch that touches openapi.yaml + four `.md` files.

## Known follow-ups

Per the audit doc:

- **v5.21.0** — observer + whisper config-parity sweep (`applyConfigPatch` cases for the observer subsystem + missing whisper.backend / endpoint / api_key keys).
- **v5.22.0** — observability fill-in (stats metrics + Prom metrics for BL191 Q4/Q6 + BL180 cross-host).
- **v5.22.x patches** — datawatch-app#10 catch-up issue; container parent-full retag; gosec HIGH-severity review.

## Upgrade path

Pure docs change — no behavior shift. `datawatch update && datawatch restart` picks up the embedded-docs refresh so the PWA's `/diagrams.html` + Settings → About → MCP tools links surface the new entries.
