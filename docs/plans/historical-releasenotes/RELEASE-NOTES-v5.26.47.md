# datawatch v5.26.47 — release notes

**Date:** 2026-04-27
**Spans:** v5.26.46 → v5.26.47
**Patch release** (no binaries — operator directive).
**Closed:** Memory + KG round-trip smoke probe (service-function audit, expanded).

## What's new

### Smoke now exercises memory save + spatial dims + KG stats

The §9 memory check from v5.26.28 only hits `/api/memory/search`. Operator directive (service-function audit): every operator-facing surface should round-trip in smoke. v5.26.47 adds §7f that covers the *write* side:

```
== 7f. Memory + KG round-trip ==
  PASS  /api/memory/stats reports enabled=true
  PASS  /api/memory/kg/stats returns the canonical 4-counter shape
  PASS  memory save with wing/room/hall returned id=793
  PASS  memory search round-trips id=793
  PASS  memory probe id=793 deleted
```

Five checks:

1. **`GET /api/memory/stats`** — health gate; sets the `MEM_OK=yes` flag that gates the rest of the section. Skip cleanly when memory isn't enabled.
2. **`GET /api/memory/kg/stats`** — verify the four counter keys (`entity_count`, `triple_count`, `active_count`, `expired_count`) all present. Catches schema regressions in the KG stats handler.
3. **`POST /api/memory/save`** with spatial dimensions (`wing: smoke`, `room: probe`, `hall: facts` — the nightwire BL55 columns) — capture the returned id.
4. **`GET /api/memory/search?q=v5.26.47`** — verify the saved id appears in the results. Round-trips the write through the embedder + the search index.
5. **`DELETE /api/memory/delete?id=<id>`** — cleanup the probe so smoke runs don't leak memory rows.

The probe content includes a unique timestamp so collisions with prior runs are impossible. The probe row is fully ephemeral — saved → searched → deleted in <1s.

### What's still uncovered

- **Wake-up stack (L0/L1/L2/L3/L4/L5)** — the layer.go composers don't have direct REST endpoints; they're invoked by the agent bootstrap path. Smoke would need to spawn an agent + read its wake-up bundle. Tracked in the audit gap list.
- **Agent diaries** (BL97) — same constraint: written by spawned workers, not directly by REST.
- **KG contradiction detection** (BL98) — fires on KG add; smoke could test it but needs a deliberate contradicting-fact pair to exercise the path.
- **Closets/drawers** (BL99) — verbatim-to-summary chain; needs multiple writes to trigger.

§7f closes the simplest CRUD-style row of the audit. The remaining items need richer fixtures.

### Smoke total

40 → 45 pass. Was 37 before this session's §7d (persistent fixtures) + §7e (filter CRUD) + §7f (memory + KG) additions; now 45/0/1.

## Configuration parity

No new config knob.

## Tests

Smoke against the dev daemon: 45 pass / 0 fail / 1 skip (the `1 skip` is orchestrator, which is genuinely disabled here). Go test suite unaffected: 465 passing.

## Known follow-ups

Service-function smoke audit residuals (per `docs/plans/2026-04-27-v6-prep-backlog.md`):

- Schedule store CRUD (deferred-execution timing constraint).
- Wake-up stack layer probes (need agent fixture).
- MCP tools surface (need MCP client wrapper).
- F10 ephemeral agent lifecycle.
- Channel send round-trip.

PRD-flow phase 3 + phase 4 design, mempalace alignment audit, screenshots refresh, datawatch-app PWA mirror — all unchanged.

## Upgrade path

```bash
git pull
# No daemon restart needed — script-only change. Run
# scripts/release-smoke.sh; section 7f fires inline.
```
