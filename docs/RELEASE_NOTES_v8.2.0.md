# Release Notes — v8.2.0

**Date**: 2026-05-19  
**Sprint**: T40 — Android 1.0.0 Blockers + Settings UX (BL327–BL330)  
**Stories**: 46 E2E stories (TS-637–TS-682), 43 pass, 3 skipped (cap enforcement in no-auth test env)

---

## What's new

### BL327 — Badge/chip multi-select

All comma-separated settings fields now use badge-input components. Enter or comma key creates a badge; × removes it. Known-set fields (federation capabilities, LLM names) show a filtered dropdown. Drag-to-reorder for LLM fallback chain order.

Affected fields: secrets tags, federation peer capabilities, LLM fallback chain, compute node tags, profile `memory_shared_with`, profile `skills`.

REST payloads are **unchanged** — values are still comma-separated strings. This is a pure PWA UX change.

### BL328 — Async PRD decompose (SSE streaming)

`POST /api/autonomous/prds/<id>/decompose` returns immediately with `{task_id, stream_url}` instead of blocking. Stories stream as Server-Sent Events. `Last-Event-ID` header enables mid-stream reconnect replay.

Second POST for an in-flight job returns the same `task_id` (idempotent). Status endpoint: `GET .../decompose/status`.

CLI: `datawatch autonomous prd decompose <id>`  
MCP: `autonomous_prd_decompose`  
PWA: inline progress panel with reconnecting state indicator.

### BL329 — Identity POST alias

`POST /api/identity` now behaves identically to `PATCH` (partial update, merges non-empty fields). Added for Android mobile compatibility where POST is more natural than PATCH.

All four methods — GET, PUT, PATCH, POST — share a single handler.

### BL330 — UnifiedPush

Full push notification surface compatible with the UnifiedPush standard:

- `GET /.well-known/unifiedpush` — discovery endpoint
- `POST /api/push/register` — register a push endpoint
- `GET /api/push/register` — list registrations
- `DELETE /api/push/unregister` — unregister by id or endpoint URL
- `POST /api/push/notify` — send to all or one targeted endpoint

PWA: Settings → Comms → Push Notifications card.

---

## Security

- gosec baseline updated from 42 to 60; previously blocking all releases since v5.26.40.
- `-exclude-dir=.claude` added to all gosec invocations to prevent scanning agent worktrees.
- SARIF uploads and release artifact attachment added to the release pipeline.
