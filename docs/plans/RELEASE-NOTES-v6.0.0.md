# datawatch v6.0.0 — release notes

**Date:** 2026-04-28
**Major release.** First major version since v5.0.0 (2026-04-26). Closes the entire mempalace alignment audit, ships the full spatial-memory schema, finishes the PRD-flow phases, and lands every v6.1+ / v6.2+ candidate the operator wanted in this cut. Going forward, only debugging until the v6.0 release date.

## Headline changes since v5.0.0

### Memory + Intelligence
- **Full mempalace spatial schema (6 axes).** Floor / wing / room / hall + new shelf / box columns, all auto-derived at save time via `AutoTagFull` (`internal/memory/room_detector.go`). Backwards-compatible — pre-v6.0 rows carry empty values for the new axes; queries without filters on them return the same rows.
- **Memory pinning.** Operator-marked rows always surface in L0+L1 wake-up regardless of vector rank. Schema additive (`pinned BOOLEAN`); `POST /api/memory/pin`; pinned rows surface with `[pin/role]` prefix in L1.
- **Conversation-window stitching.** `Store.StitchSessionWindow(hitID, before, after)` returns N predecessors + the hit + N successors from the same session. Order via row ID so same-second saves still work.
- **Query sanitizer.** 10 OWASP-LLM01 prompt-injection patterns redacted before queries reach the embedder. Wired into every `Recall` path.
- **Repair self-check.** `Store.RunRepair(ctx, opts)` reports missing embeddings, empty content, detached closets, and content-sha duplicates. Optional inline re-embed.
- **Pre-save normalization.** NFC + whitespace collapse + fancy-quote folding so writes that differ only in invisible drift dedup correctly.
- **Similarity-stale eviction.** `last_hit_at` column + `Store.SweepStale`. Rows that never surface in any query and are older than the cutoff become eviction candidates. Manual + pinned rows exempt.
- **Periodic refine sweep.** `Store.RefineSweep` walks old session/output_chunk rows, asks the supplied LLM to compress, replaces in place. Free row count back without losing semantic coverage.
- **Schema-free fact extraction.** `ExtractFacts(ctx, text, llm)` returns subject-predicate-object triples via heuristics first, optional LLM fallback when confidence is low.
- **Spellcheck.** Conservative Levenshtein-based suggestions on top of an embedded English + datawatch-domain dictionary. Never rewrites — returns suggestions only.
- **Convo miners — 3 new sources.** Slack channel JSON exports, IRC log files (weechat/irssi/hexchat), and mbox email archives. Each round-trips through `Store.SaveImportedMessages` with `Source: "channel:slack/irc/email"`.
- **Corpus origin (Source field).** Every memory row now carries a Source string ("operator", "session", "channel:slack", "mcp:remember", …) populated automatically when not explicit. `Memory.Source` exposed in REST and MCP payloads.
- **Auto-tagging on save.** Heuristic classifier fills empty wing/hall/room/floor/shelf/box from project_dir + content keywords + role. Operator overrides always win.
- **Wake-up bundle composer (REST).** `GET /api/memory/wakeup` returns the L0+L1+L4+L5 bundle on demand. Smoke + operator tooling probe what an agent would receive at bootstrap.
- **Schema_version tracking.** Idempotent `schema_version` table per backend; version surfaces via `MemoryAPI.SchemaVersion()`.

### MCP / stdio
- **Stdio MCP nil-reader segfault fixed.** `ServeStdio` now passes `os.Stdin` / `os.Stdout` explicitly so the subprocess handles its first message instead of panicking on EOF.
- **memory_recall, memory_remember, memory_list, memory_forget, memory_stats** now registered always-on in `mcp.New()` so `datawatch mcp` (subprocess for IDEs) surfaces them in `tools/list` regardless of whether `SetMemoryAPI` was called. Closes mempalace-audit partial.

### CI + supply chain
- **OWASP ZAP active scans.** Two new passes (PWA full active + API full active with `-t`) on top of the three baseline passes. Operator confirmed no auth needed against the kind-deployed daemon.
- **GHCR cleanup workflow.** Weekly + workflow_dispatch. Deletes container versions from closed minor lines; keeps latest patch of each closed minor + `latest`. Uses `GITHUB_TOKEN` with `packages: write` — no PAT.

### PWA + UX
- **PRD row spacing for hidden items.** `renderStory` / `renderTask` filter empty segments; the `✎ files` button folds inline into the title row when no files are planned. PRDs with sparse stories no longer carry blank-padded lines.

### Smoke + tests
- **Real stdio MCP smoke wrapper** — `scripts/release-smoke-stdio-mcp.sh` spawns `datawatch mcp`, sends JSON-RPC initialize + tools/list + tools/call(memory_recall), validates each response.
- **Real L4/L5 wake-up smoke wrapper** — `scripts/release-smoke-wakeup.sh` probes the new `/api/memory/wakeup` REST endpoint with three argument shapes (L0+L1, +parent, +self+parent).
- **§7r and §7s** in `scripts/release-smoke.sh` wired to the new wrappers; replaces v5.26.68 prereq-only stubs.
- **10 new unit tests** in `internal/memory/v6_features_test.go` covering normalize, spellcheck, extract facts, sanitize query, AutoTagFull, sweep stale, conversation window, and the convo miners.

## Configuration parity

Every new feature has at least one of: REST endpoint, MCP tool, MemoryAPI interface entry, or operator-runnable script. Headline new REST surface:

| Endpoint | Purpose |
|---|---|
| `POST /api/memory/pin` | Toggle pinned flag (Mempalace QW#2) |
| `GET  /api/memory/wakeup` | Composed L0+L1+L4+L5 bundle |
| `POST /api/memory/sweep_stale` | Similarity-stale eviction (sweeper.py) |
| `POST /api/memory/spellcheck` | Conservative spellcheck (spellcheck.py) |
| `POST /api/memory/extract_facts` | Heuristic SVO extraction (general_extractor.py) |

## Tests

```
Go build: Success
Go test:  1464 passed in 58 packages
Smoke:    68/0/5 (full release-smoke against dev daemon)
```

## Backwards compatibility

- All schema changes are additive (`ALTER TABLE … ADD COLUMN IF NOT EXISTS`). Pre-v6.0 rows carry empty values for the new columns; reads from older datawatch releases still work.
- `SaveWithMeta` / `SaveWithNamespace` continue to work; both funnel through `SaveWithNamespaceAndSource` which accepts `Source: ""` and derives a sensible default.
- The `Backend` interface gained no required methods. New capabilities (`SweepStale`, `MarkHit`, `SchemaVersion`, `SetPinned`, `ListPinned`) are exposed via optional capability interfaces (`PinnableBackend`, ad-hoc type assertions in `ServerAdapter`) so the PG path keeps compiling without forcing every implementation to learn every new feature at the same time.
- JSON-tag rename `files_planned` → `files` already shipped in v5.26.67; v6.0 carries it forward.

## Cumulative changelog highlights since v5.0.0

- **v5.26.0–.69** — operator UX + PRD-flow Phases 1–6, container workers F10, comm fabric BL101–104, ZAP per-interface scans, 33→68 smoke sections, autonomous howto + screenshots, mempalace audit, encryption smoke runner, file association.
- **v5.26.70** — mempalace QW bundle (auto-tag, pinning, stitching, sanitizer, repair) + ZAP active scan + PRD spacing.
- **v5.26.71** — stdio MCP real probe, L4/L5 wake-up real probe (REST), GHCR cleanup workflow, follow-up plan.
- **v6.0.0** (this release) — full spatial schema, normalize, sweeper, refine sweep, fact extractor, spellcheck, Slack/IRC/email convo miners, corpus_origin Source field, schema_version tracking, plus everything above carried into a single major.

## Upgrade path

```bash
git pull
datawatch update && datawatch restart
# Hard-reload PWA. SQLite + PG migrations run idempotently
# on first start; existing rows default to '' for the new
# spatial axes and 0 for last_hit_at.
```

No data migration required. The new columns are additive; rolling back to v5.26.71 still reads them as if they were '' (SQLite COALESCE handles the missing column case in v5.26.71 too).

## What's next

Only debugging until the v6.0 release date. No more feature work in this branch — the audit is closed, the PRD-flow is locked, and the spatial schema is at parity with mempalace. Bug reports + smoke regressions only.

The follow-up plan doc (`docs/plans/2026-04-28-mempalace-followups-and-spatial-dims.md`) marked everything pre-v6.0; this release ships all of it.
