# The shipped status — documentation pass

Date: 2026-11-01

The shipped state for Automata (PRDs) and Automations (a record's work
landed on the default branch, with the release and commit it landed on) is
implemented and released in the daemon. This document tracks the
documentation pass that brings every operator-facing surface up to it:
the how-to, the OpenAPI spec, the config reference, the definitions and
implementation docs, the API doc, the changelog, and the mobile-parity
issue.

Nothing here changes product behaviour: every surface already serves the
data; the work is documenting what is there and filing the one gap the
mobile apps have.

## What the surface is (one line per channel)

- **REST** — `POST /api/autonomous/prds/{id}/shipped` and
  `POST /api/automations/{id}/shipped` record the four shipped fields
  (`merged_in_release`, `merged_at`, `merge_commit`, `shipped_source`);
  `GET /api/autonomous/shipped/counts` reads the per-status buckets plus the
  in-force settings; `POST /api/autonomous/shipped/backfill` runs one
  recovery pass (`?dry_run=true` to not write); `?shipped=true|false` (or
  `shipped` inside `status`) filters `GET /api/autonomous/prds` and
  `GET /api/automations`.
- **MCP** — `autonomous_shipped_record`, `autonomous_shipped_backfill`,
  `autonomous_shipped_counts`; `autonomous_prd_list` and `automation_list`
  take `shipped: "true" | "false"`.
- **CLI** — `datawatch autonomous shipped record|backfill|counts`,
  `datawatch automation shipped <id>`, `datawatch autonomous list
  --shipped …`, `datawatch automation list --shipped …`.
- **Comm channel** — `autonomous shipped-record … [automation=true]`,
  `autonomous shipped-backfill [dry_run=true]`, `autonomous
  shipped-counts`.
- **Config** — the `autonomous.shipped` block: `enabled`, `show_chip`,
  `filter_default`, `count_separately`, `backfill_on_start`.
- **PWA** — the system **Shipped dashboard card** (server fills its live
  per-status counts and the in-force settings at read time; it cannot be
  deleted), plus the shipped fields on the list rows.
- **Merge helper** — `scripts/merge-branches-bump-version.sh` (sources
  `scripts/merge-shipped-record.sh`) records shipped state best-effort for
  every merged `automaton/<id>` branch after the release bump commit; a
  failed record never fails a merge that succeeded.

## Parity surface

| Surface | Touched by this pass | Detail |
|---|---|---|
| REST | Touched | OpenAPI gained the four shipped paths (`POST /api/autonomous/prds/{id}/shipped`, `POST /api/automations/{id}/shipped`, `GET /api/autonomous/shipped/counts`, `POST /api/autonomous/shipped/backfill`), the `shipped` query parameter on the PRD and Automation list routes, the four field properties on the PRD list item and Automation schemas, and the `ShippedRecordRequest`, `ShippedCountsResponse`, `ShippedBackfillReport` schemas. The routes themselves existed before this pass. |
| MCP | Touched (docs only) | The three `autonomous_shipped_*` tools and the `shipped` parameters on the two list tools existed before this pass; the how-to and definitions now describe them. |
| CLI | Touched (docs only) | `datawatch autonomous shipped …` and `datawatch automation shipped …` existed before this pass; the how-to now describes the exact flags and defaults. |
| Comm channel | Touched (docs only) | The `autonomous shipped-record / shipped-backfill / shipped-counts` verbs existed before this pass. |
| YAML/config | Touched | The `autonomous.shipped` block in `docs/config-reference.yaml` now carries the five keys with defaults and legal values. |
| PWA | Touched (docs only) | The Shipped dashboard card (system card, server-filled data) is documented; it already exists in the web app. |
| Android | Excluded from this change, tracked | The Android app has no shipped surface. Tracked by the parity issue below. No Android code lives in this repository. |
| iPhone | Excluded from this change, tracked | Same issue. No iPhone code lives in this repository. |

## Documentation changed in this pass

- [How-to: the shipped status](../howto/automata-shipped-status.md) — the operator
  walkthrough, all channels.
- [`docs/api/autonomous.md`](../api/autonomous.md) — the shipped paths and
  the `shipped` list parameter.
- [`openapi.yaml`](../../internal/server/web/openapi.yaml) — the shipped
  paths, parameters, fields and schemas.
- [`docs/config-reference.yaml`](../config-reference.yaml) — the
  `autonomous.shipped` block.
- [`docs/datawatch-definitions.md`](../datawatch-definitions.md) — the
  shipped entry.
- [`docs/implementation.md`](../implementation.md) — the shipped section.
- [`CHANGELOG.md`](../../CHANGELOG.md) — the user-facing entry.
- **Parity issue draft** —
  [`docs/plans/2026-11-01-shipped-status-parity-issue.md`](2026-11-01-shipped-status-parity-issue.md)
  — the datawatch-app issue for the Android/iPhone shipped surfaces.
