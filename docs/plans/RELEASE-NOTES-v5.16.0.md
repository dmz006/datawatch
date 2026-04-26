# datawatch v5.16.0 — release notes

**Date:** 2026-04-26
**Spans:** v5.15.0 → v5.16.0
**Closed:** PWA visualization gaps for v5.9.0 (BL191 Q4) + v5.10.0 (BL191 Q6) + v5.12.0 (BL180 cross-host)

## What's new

Three contained PWA additions that surface data the daemon was
already producing through `/api/autonomous/prds/...` and
`/api/observer/envelopes/all-peers`:

### BL191 Q4 — PRD genealogy badges + Children disclosure

Every PRD card on the Autonomous tab now shows:

- **`↗ parent <id>`** badge when `parent_prd_id` is set (PRD was
  spawned from a parent task's `SpawnPRD` shortcut)
- **`depth N`** badge when `depth > 0`
- New **Children (lazy)** disclosure under the Stories & tasks
  block — clicking "Load" calls `GET /api/autonomous/prds/{id}/children`
  and renders one row per child PRD with status pill + title + depth

Per-task affordances also gain:

- **`↳ spawn`** badge when `task.spawn_prd === true` (operator
  flagged this task to spawn a child PRD instead of running directly)
- **`→ child <id>`** link once the executor has spawned the child

### BL191 Q6 — Per-story / per-task verdict badges

`Story.Verdicts` and `Task.Verdicts` (populated when
`Config.PerStoryGuardrails` / `Config.PerTaskGuardrails` are
non-empty) now render inline next to the story title and task title
respectively:

```
[rules: pass]  [security: warn]  [release-readiness: block]
```

- Color-coded by outcome (green / amber / red)
- Hover tooltip surfaces severity, summary, and the first three
  issues
- Block outcomes pop visually so operators see them immediately

### BL180 cross-host — `↔ Cross-host view` modal

Added a "Cross-host view" button to the Federated peers filter
row on Settings → Monitor. Clicking it opens a modal that fetches
`/api/observer/envelopes/all-peers` and renders one collapsible
section per peer (local + every Shape A/B/C peer) with:

- Each envelope's listen addrs + outbound edges visible inline
- CallerAttribution rows with per-caller conn counts
- **`🔗 cross`** badge on caller rows whose ID matches the
  `<peer>:<envelope-id>` cross-host pattern (output of
  `CorrelateAcrossPeers`)

This is the operator-facing surface for the v5.12.0 cross-host
correlation work.

## Tests

No new tests — the changes are all in `internal/server/web/app.js`
(client-side rendering) and don't affect the Go test surface.
**1355 still passing in 58 packages**; daemon still starts.

## Known follow-ups (still open)

- **BL190 deeper density** — failure-path popup, mid-run progress,
  verdict drill-down panel. Iterative.

## Upgrade path

```bash
datawatch update                            # check + install
datawatch restart                           # apply the new binary

# After at least one PRD with SpawnPRD, the genealogy badges show.
# After PerTaskGuardrails / PerStoryGuardrails are configured + run,
# verdict badges show inline.
# After ≥2 federated peers are pushing, the Cross-host view modal
# populates with cross-peer attribution.
```
