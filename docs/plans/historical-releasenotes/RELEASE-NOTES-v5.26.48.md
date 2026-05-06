# datawatch v5.26.48 — release notes

**Date:** 2026-04-27
**Spans:** v5.26.47 → v5.26.48
**Patch release** (no binaries — operator directive).
**Closed:** MCP tool surface smoke probe (service-function audit).

## What's new

### Smoke now verifies the MCP tool surface is registered

The MCP server exposes 39 operator-facing tools today (sessions, agents, schedules, profiles, alerts, pipelines, memory). All of them get registered through the same code path; if a release accidentally drops one (rename without registering, missing import after a refactor), MCP clients silently lose that capability with no immediate signal. v5.26.48 adds §7g to catch the regression class:

```
== 7g. MCP tool surface ==
  PASS  /api/mcp/docs returns the canonical MCP tool surface (>=30 tools, foundational subset present)
```

Two assertions:

- **Tool count floor: ≥30.** Current count is 39. The check is defensive — releases that strip tooling can still pass as long as the floor holds.
- **Foundational subset registered:** `list_sessions`, `start_session`, `send_input`, `schedule_add`, `profile_list`, `agent_list`. These are the spine of the MCP surface — every operator workflow ultimately calls one of these. If any are missing, the registration path itself is broken.

The check uses `/api/mcp/docs` which returns the canonical JSON tool list with `name` / `description` / `parameters`. No MCP client wrapper needed; pure REST shape verification.

### What's still uncovered (MCP-side)

`memory_recall`, `kg_query`, `research_sessions`, `copy_response`, `get_prompt` are mentioned in `CLAUDE.md` but not currently in the daemon's `/api/mcp/docs` listing — they live behind a different MCP namespace (the `mcp__datawatch-*` server-mode tools that operate over stdio, not the REST-mirrored set). Smoke for those needs an actual MCP client connection. Tracked.

### Smoke total

40 → 45 → 46. Was 37 at the start of this session's audit additions; the four new sections (§7d persistent fixtures, §7e filter CRUD, §7f memory + KG, §7g MCP) collectively add 9 PASS entries.

## Configuration parity

No new config knob.

## Tests

Smoke against the dev daemon: 46 pass / 0 fail / 1 skip. Go test suite unaffected: 465 passing.

## Known follow-ups

Service-function smoke residuals (per `docs/plans/2026-04-27-v6-prep-backlog.md`):

- Schedule store CRUD (deferred-execution timing constraint).
- Wake-up stack layer probes (need agent fixture).
- Stdio-mode MCP tools (need MCP client wrapper).
- F10 ephemeral agent lifecycle.
- Channel send round-trip.

PRD-flow phase 3 + phase 4 design, mempalace alignment audit, screenshots refresh, datawatch-app PWA mirror — all unchanged.

## Upgrade path

```bash
git pull
# No daemon restart needed — script-only change. Run
# scripts/release-smoke.sh; section 7g fires inline.
```
