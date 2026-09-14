# Plan: PRD Overview Session Resource Stats Card (v8.26.0)

**Status:** Completed 2026-09-13
**Release:** v8.26.0

## Problem

The PRD Overview tab showed no compute resource usage while tasks were actively running. Operators
had to navigate to the Observatory or the individual session card to see CPU/GPU/memory for
running task sessions. BL373 (v8.25.0) added a compute stats card to the session detail view;
BL375 (v8.25.4) added per-peer resource panels in Observatory — but the PRD overview itself
was blind to resource usage during execution.

## Approach

Added a lazy-loaded resource card to the PRD Overview tab body, displayed only when at least one
task has an active `session_id` in `running`, `verifying`, or `running_tests` state.

- `_renderDetailOverview` detects active-session tasks and injects a `prdSessionResources_{prd.id}`
  placeholder `<div>` above the progress bar when any are present.
- `_loadPRDSessionResources(prd)` (new async function) runs after the overview HTML is set:
  - For each active task's `session_id`, looks up `compute_node_ref` from `state.sessions`.
  - Fetches `/api/compute/nodes/{ref}/detail` (same endpoint as BL373's session card).
  - Falls back to local `/api/stats` when no compute node ref is set.
  - Renders compact bar cards (5px height, `tabular-nums`, label/value layout) for CPU%,
    RAM used/total, GPU util%, and GPU VRAM — same visual language as the BL379 Observer grid.
  - Updates `slot.innerHTML` on each refresh.
- Auto-refreshes every 5 s via `setInterval`; interval cleared when a new PRD detail renders.
- Called from `_renderDetailContent` whenever `tab === 'overview'`, after the BL373 block.

## Files Changed

- `internal/server/web/app.js` — three changes:
  1. `_renderDetailOverview`: active-task detection + placeholder slot injection
  2. `_renderDetailContent`: `_loadPRDSessionResources(prd)` call for overview tab
  3. New `window._loadPRDSessionResources` function (refresh loop + bar-card renderer)

## Design Notes

Reuses the existing compute node detail API rather than adding a new endpoint. Falls back
gracefully to local stats — the card is useful even on single-node setups with no registered
compute nodes. The slot pattern (placeholder injected by the synchronous renderer, filled
asynchronously) is the same pattern used by BL373's `_loadPRDActiveSessionCard`.
