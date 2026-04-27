# datawatch v5.26.41 — release notes

**Date:** 2026-04-27
**Spans:** v5.26.40 → v5.26.41
**Patch release** (no binaries — operator directive).
**Closed:** Filter store CRUD smoke probe (service-function audit, partial).

## What's new

### Smoke now exercises filter store CRUD

Operator directive (service-function smoke audit): every store with REST CRUD should round-trip in smoke. v5.26.41 adds the simplest shape — filters — as section §7e:

```
== 7e. Filter store CRUD ==
  PASS  create filter: d2ea (pattern=smoke-probe-1777333656)
  PASS  filter d2ea round-trips through GET /api/filters
  PASS  delete filter d2ea
```

Three checks:

1. **Create** via `POST /api/filters` with `{pattern, action: "schedule", value: "yes"}`.
2. **Round-trip** via `GET /api/filters` — verify the new filter ID appears in the returned list.
3. **Delete** via `DELETE /api/filters?id=<id>` — verify the response carries the `"status": "deleted"` shape.

Smoke now reports `40 pass / 0 fail / 1 skip` (was `37 / 0 / 1`). Pattern uses `$(date +%s)` so each run gets a unique fixture; all three are ephemeral so no cleanup_log entry needed (the create's delete IS the cleanup).

### Why filters first, alerts and schedules later

The three stores have very different shapes:

| Store | Body shape | Smoke value |
|------|------|------|
| Filters | `{pattern, action, value}` — 3 simple strings | High (operator-creatable, simple) |
| Alerts | `{level, title, body, …}` plus system-generated metadata | Lower (alerts mostly system-fired, not operator-CRUD'd) |
| Schedules | `{session_id, command, when, …}` with deferred-execution semantics | Higher (operator-creatable) but more complex body |

Filter CRUD is the lowest-risk addition that closes one row of the audit gap. Schedule CRUD will follow once the body shape + the deferred-execution timing is wrapped in a smoke-friendly invariant (currently the schedule fires on its own clock — hard to assert against without time control). Alerts won't get a CRUD smoke because the operator surface is mostly read-only acks.

## Configuration parity

No new config knob.

## Tests

Smoke: 40 pass / 0 fail / 1 skip (was 37/0/1). Go test suite unaffected: 465 passing.

## Known follow-ups

Service-function smoke audit residuals (per `docs/plans/2026-04-27-v6-prep-backlog.md`):

- **Schedule store CRUD** — needs a smoke-friendly time invariant.
- **Memory layers L0–L5 + spatial filtering + agent diary + KG contradictions + closets/drawers** — biggest gap; needs one probe per layer / spatial dimension.
- **MCP tools surface** — every operator-facing MCP tool deserves a probe; needs an MCP client wrapper in smoke.
- **F10 agent lifecycle** — broker mint/revoke + cluster spawn + sibling visibility + parent-namespace import.
- **Channel send round-trip** — currently only `/api/channel/history` shape is checked.

## Upgrade path

```bash
git pull
# No daemon restart needed — script-only change. Run
# scripts/release-smoke.sh; section 7e fires inline.
```
