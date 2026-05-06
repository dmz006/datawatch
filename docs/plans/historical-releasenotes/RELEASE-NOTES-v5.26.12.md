# datawatch v5.26.12 — release notes

**Date:** 2026-04-27
**Spans:** v5.26.11 → v5.26.12
**Patch release** (no binaries — operator directive).
**Closed:** Children load eagerly with the rest of the PRD list.

## What's new

### PRD children no longer lazy-loaded

Operator: *"Children for prd should not be lazy load, they should load with everything else."*

Pre-v5.26.12 every PRD row had a "Children (lazy)" disclosure with a `Load` button that fired `GET /api/autonomous/prds/{id}/children` on click. That was an N+1 design — useful when `ListPRDs` only returned the top-level PRDs, but `ListPRDs` actually returns every PRD (parents AND children) flat with `parent_prd_id` pointers. v5.26.12:

- `loadPRDPanel` builds an O(N) `parent_id → [children]` index once from the flat list (`state._prdChildIndex`).
- `renderPRDRow` reads from the index and renders child rows inline. The `<details open>` attribute means rows are visible without an extra click, but the disclosure summary `Children (N)` still shows the count and lets the operator collapse if needed.
- The lazy-load `loadPRDChildren` + `Load` button + per-row `GET /children` fan-out are all gone.
- Same row shape as the v5.26.6 polish: clickable child IDs `scrollToPRD`, stories/tasks counts inline, verdict-count badges.

### Operator question: does autonomous mode auto-accept claude's trust-dir prompt?

**Yes** — already handled. The operator's config has `session.claude.skip_permissions: true` (default since v3.0), which makes `internal/llm/claudecode/backend.go` append `--dangerously-skip-permissions` to the claude-code launch command. That single flag bypasses both the "Do you trust this folder?" dialog AND every per-tool permission prompt for the lifetime of the session.

Autonomous PRDs spawn worker sessions through the same `Manager.Launch` code path that interactive sessions use, so they inherit `--dangerously-skip-permissions` automatically. No autonomous-specific code is needed.

For operators who *deliberately* leave `skip_permissions: false` (e.g. a multi-tenant deployment where each spawn should re-prompt), the existing `promptPatterns` list in `internal/session/manager.go:120` detects the trust dialog and flips the session to `waiting_input`. That mode is incompatible with autonomous run since the executor expects sessions to make progress on their own.

## Configuration parity

No new config knob.

## Tests

1397 passing. PWA-only change; no Go regression risk.

## Known follow-ups

Same as v5.26.11.

## Upgrade path

```bash
git pull
# Hard-refresh PWA tab once for the new SW cache.
# Open Autonomous tab → child PRDs (if you have any from a SpawnPRD
# task) now render inline under their parent, no Load button.
```
