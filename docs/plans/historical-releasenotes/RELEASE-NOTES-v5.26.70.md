# datawatch v5.26.70 — release notes

**Date:** 2026-04-28
**Patch.** Mempalace quick-win bundle + ZAP active scan + PWA spacing cleanup.

## What's new

### 1. Mempalace QW#1 — auto-tagging on save (`#61`)

`internal/memory/room_detector.go` ports mempalace's `room_detector_local.py`. On every `SaveWithNamespace` call:

- `wing` is derived from `filepath.Base(projectDir)` when not supplied.
- `hall` is classified via keyword anchors into `preferences` / `advice` / `events` / `discoveries` / `facts`.
- `room` is classified into `auth` / `deploy` / `testing` / `perf` / `db` / `ui` / `api` / `docs` / `security` from content keywords.

`AutoTag(projectDir, content, wing, room, hall)` preserves operator-supplied values; only blanks get filled. Pure Go, no Python runtime.

### 2. Mempalace QW#2 — memory pinning (`#62`)

Operator-marked memories that always surface in L1 critical-facts regardless of vector-similarity rank.

- Schema: `ALTER TABLE memories ADD COLUMN pinned INTEGER DEFAULT 0` + index. Idempotent migration (errcheck'd).
- `Memory.Pinned bool` field with `json:"pinned,omitempty"`.
- `Store.SetPinned(id, pinned)` + `Store.ListPinned(projectDir, n)` SQLite implementations.
- New `PinnableBackend` optional interface — Layers.L1 type-asserts so backends without pinning (PG path) degrade gracefully.
- `Layers.L1` surfaces pinned rows first with a `[pin/<role>]` prefix, then merges with the existing recency-ranked picks (dedup by ID).
- REST: `POST /api/memory/pin {id, pinned}`. Returns 501 when the backend doesn't support pinning.
- `MemoryAPI.SetPinned` interface + `ServerAdapter.SetPinned` implementation. Test fakes updated.

### 3. Mempalace QW#3 — conversation-window stitching (`#63`)

`internal/memory/conversation_window.go`. When semantic search returns one `output_chunk`, the operator usually wants the surrounding context too. `Store.StitchSessionWindow(hitID, before, after)` returns the hit chunk + N predecessors + N successors from the same `session_id`, ordered oldest-first. Caller decides how to render the window (PWA drill-down, channel quote, etc.).

### 4. Mempalace QW#4 — query sanitizer (`#64`)

`internal/memory/query_sanitizer.go` ports mempalace's `query_sanitizer.py`. `SanitizeQuery(q)` redacts 10 OWASP-LLM01 prompt-injection patterns (`ignore previous instructions`, `system: you are`, `[SYSTEM]:`, `jailbreak`, `DAN mode`, `act as unrestricted`, `reveal system prompt`, etc.) before the query reaches the embedder. Wired into `Retriever.Recall` / `RecallAll` / `RecallInNamespaces` — every recall path goes through it.

Defense in depth — the embedder treats inputs as opaque text already, but sanitization reduces the attack surface for downstream LLM consumers (`memory_recall` MCP tool, auto-loaded L1 facts).

### 5. Mempalace QW#5 — repair self-check (`#65`)

`internal/memory/repair.go`. `Store.RunRepair(ctx, opts)` scans the store and reports drift:

- rows with NULL/empty embedding column (re-embed candidates)
- rows with empty content (storage drift)
- closets with `drawer_id` pointing at a missing row
- duplicates within a project (grouped by content sha256)

Default `DryRun=true` — read-only. With `DryRun=false` and an embedder, the pass re-embeds missing-vector rows inline. Destructive cleanup (delete duplicates, detach closets) is left to the operator since dupes can be intentional snapshots.

### 6. ZAP active scan workflow (`#66`)

`.github/workflows/owasp-zap.yaml` extended with two new passes:

- **PWA full active scan** (`zaproxy/action-full-scan`) against the kind-deployed daemon root.
- **API full active scan** (`zaproxy/action-api-scan` with `-t`) against `/api/`, schema-constrained by `docs/api/openapi.yaml`.

Operator confirmed "zap didn't need auth" — the kind-deployed daemon runs without a bearer token in this CI flow, so active scans hit every endpoint directly. Active scans send real attack payloads (SQLi/XSS/cmd-injection probes) so they're only safe against an ephemeral test daemon, never a shared environment. `fail_action: false` until an active baseline is set.

The workflow now runs five passes in one kind setup:

1. PWA baseline (passive)
2. API baseline (OpenAPI-driven, passive)
3. diagrams.html baseline
4. PWA full active *(new)*
5. API full active *(new)*

### 7. PWA — PRD row spacing for hidden items

Operator-reported: "New PRD spacing of page lines needs to be cleaned up, especially with hidden items."

- `renderStory` filters empty segments (`desc`, `filesPlanned`, `rejected_reason`, `tasks`) with `[…].filter(Boolean).join('')` so missing fields don't carry margin/padding gaps.
- Story title row collapses the `✎ files` button inline when no files are planned (instead of emitting a blank "📝 ✎ files" line under the title).
- `renderTask` no longer emits an empty "📝 ✎" line for editable tasks with no planned files; the affordance moves inline next to the spec.
- Net effect: PRDs with sparse stories/tasks (no description, no files yet, no rejected reason) render compact instead of carrying multiple blank-padded lines.

### 8. Memory feature attestation

- `README.md` features list extended with auto-tagging, pinning, and query-sanitization bullets (each tagged `Mempalace QW#…, v5.26.70`).
- `docs/memory.md` got a new "v5.26.70 Mempalace quick-win ports (Go-native)" subsection with module / behaviour table.

## Configuration parity

`POST /api/memory/pin` — new operator surface for pinning. PWA wiring + comm-channel commands deferred to v5.26.71+ (REST + MCP exposed; CLI/PWA/mobile pending).

`AutoTag`, `SanitizeQuery`, `StitchSessionWindow`, `RunRepair` are pure library functions — exposed via `Store` / `Retriever` and reachable from MCP tools that already consume them.

## Tests

```
Go build: Success
Go test: 1454 passed in 58 packages
```

Existing test fakes (`fakeMemAPI`, `nsMemAPI`) updated with `SetPinned` stubs to satisfy the extended `MemoryAPI` interface.

## Backlog status

| Task | Status |
|------|--------|
| #61 Mempalace QW#1 auto-tag | ✅ this patch |
| #62 Mempalace QW#2 pinning | ✅ this patch |
| #63 Mempalace QW#3 stitching | ✅ this patch |
| #64 Mempalace QW#4 sanitizer | ✅ this patch |
| #65 Mempalace QW#5 repair | ✅ this patch |
| #66 ZAP active scan workflow | ✅ this patch |
| #67 stdio-MCP real client smoke | deferred to v6.0+ (prereq check shipped in v5.26.68) |
| #68 L4/L5 wake-up smoke | deferred to v6.0+ (prereq check shipped in v5.26.68) |
| #69 GHCR past-minor cleanup | operator action (PAT-gated) |

## Upgrade path

```bash
git pull
datawatch restart
# Hard-reload PWA. SQLite `pinned` column is added idempotently
# on first start; existing rows default to unpinned.
```

No data migration required. The pinned column is additive; reads from older datawatch releases still work (pinned defaults to false).
