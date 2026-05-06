# datawatch v5.27.0 — release notes

**Date:** 2026-04-28
**Minor release.** Pre-6.0 testing window — every mempalace alignment item now has full configuration parity (REST + MCP + CLI + comm channels + PWA). Operator policy: only debugging until the v6.0 release date; no more feature work in this branch.

## What's new since v5.26.71

### Memory + intelligence — full mempalace alignment

**Schema (additive on both SQLite + PG; `IF NOT EXISTS`):**
- `floor` / `shelf` / `box` columns + indexes — full 6-axis spatial schema
- `source` column + index (corpus_origin)
- `last_hit_at` column + index (sweeper.py similarity-stale eviction)
- `schema_version` table (migrate.py)

**New Go-native modules (no Python deps):**

| Module | Purpose |
|---|---|
| `internal/memory/normalize.go` | Pre-save NFC + whitespace + fancy-quote folding |
| `internal/memory/sweeper.go` | `SweepStale` + `MarkHit` + `SchemaVersion` |
| `internal/memory/refine_sweep.go` | Periodic LLM-compress sweep (llm_refine.py) |
| `internal/memory/general_extractor.go` | Heuristic + LLM SVO triple extraction |
| `internal/memory/spellcheck.go` | Levenshtein-based suggestions (spellcheck.py) |
| `internal/memory/convo_miner.go` | Slack JSON / IRC / mbox parsers (extra sources) |
| `internal/memory/v6_features_test.go` | 10 unit tests for the bundle |

**Extensions:**
- `room_detector.go` `AutoTagFull` adds floor/shelf/box derivation
- `store.go` new `SaveWithNamespaceAndSource` canonical write path with normalize hook + corpus_origin Source field
- `pg_store.go` full PG migration + `SweepStale` + `MarkHit` + `SchemaVersion`
- `conversation_window.go` id-based ordering for same-second saves
- `Memory` struct gains `Floor` / `Shelf` / `Box` / `Source` / `LastHitAt` fields with `omitempty`
- `Search` / `SearchAll` / `SearchFiltered` / `SearchInNamespaces` wire `MarkHit` on every top-K hit

### Configuration parity (the big one)

Every new feature is reachable from **REST + MCP + CLI + comm channels + PWA** per the project rule:

| Feature | REST | MCP tool | CLI | Comm channel | PWA |
|---|---|---|---|---|---|
| Pin/unpin a memory | `POST /api/memory/pin` | `memory_pin` | `datawatch memory pin <id> [on\|off]` | `memory pin <id> [on\|off]` | Memory Maintenance section + 📌 inline button (per-row, follow-up) |
| Similarity-stale eviction | `POST /api/memory/sweep_stale` | `memory_sweep_stale` | `datawatch memory sweep` / `sweep-apply [days]` | `memory sweep [apply [days]]` | Memory Maintenance — Dry-run / Apply buttons |
| Spellcheck | `POST /api/memory/spellcheck` | `memory_spellcheck` | `datawatch memory spellcheck <text>` | `memory spellcheck <text>` | Memory Maintenance — textarea + Run button |
| Extract facts (SVO triples) | `POST /api/memory/extract_facts` | `memory_extract_facts` | `datawatch memory extract <text>` | `memory extract <text>` | Memory Maintenance — textarea + Extract button |
| Schema version | `GET /api/memory/stats.schema_version` | `memory_schema_version` | `datawatch memory schema` | `memory schema` | Memory Maintenance — Check schema button |
| Wake-up bundle composer | `GET /api/memory/wakeup` | (read-only — operator inspection) | (planned) | (planned) | Inline link in Memory Maintenance footer |

The REST handler interface (`server.MemoryAPI`) gained: `SetPinned`, `WakeUpBundle`, `SweepStale`, `SpellCheckText`, `ExtractFactsText`, `SchemaVersion`. Test fakes (`fakeMemAPI`, `nsMemAPI`) updated. The router's `MemoryStore` gained `SetPinned` / `SweepStaleSummary` / `SpellCheckSummary` / `ExtractFactsSummary` / `SchemaVersionString` so chat-channel handlers don't have to reach into the memory package directly.

### MCP / stdio
- Stdio MCP nil-reader segfault fixed in v5.26.71 carried forward.
- `memory_recall` / `memory_remember` / `memory_list` / `memory_forget` / `memory_stats` registered always-on so `datawatch mcp` (subprocess for IDEs) surfaces them in `tools/list` regardless of `SetMemoryAPI` wiring.
- 5 new tools added in v5.27.0: `memory_pin`, `memory_sweep_stale`, `memory_spellcheck`, `memory_extract_facts`, `memory_schema_version`.

### CI + supply chain
- **OWASP ZAP active scans** (v5.26.70 carry-forward) — 2 new passes (PWA full + API full with `-t`) on top of 3 baselines.
- **GHCR cleanup workflow** (v5.26.71 carry-forward) — weekly + workflow_dispatch, dry-run by default.

### PWA + UX
- New **Memory Maintenance** settings section under Monitor → Memory. Surfaces all v5.27.0 mempalace tools with inline result rendering. Each panel links to `/docs/memory.md` and the v5.27.0 release notes for behaviour reference.
- `/api/memory/stats` now reports `schema_version` so the PWA can show migration state.
- v5.26.70 PRD spacing for hidden items carried forward.

### Tests
- **+5 router parsing tests** (`memory pin`, `memory sweep`, `memory spellcheck`, `memory extract`, `memory schema`).
- **+10 memory unit tests** (carried forward from v5.26.72/6.0.0 work) covering normalize, spellcheck, extract facts, sanitize query, AutoTagFull, sweep stale, conversation window, and the convo miners.

### Smoke
- **§7t** new section probes `/api/memory/sweep_stale`, `/spellcheck`, `/extract_facts`.
- §7r/§7s wired to the v5.26.71 stdio MCP + L4/L5 wake-up wrappers.

## datawatch-app sync

Filed under datawatch-app issue umbrella for mobile-companion mirror:
- Memory Maintenance section parity (sweep / spellcheck / extract / schema / pin button per row)
- New `/api/memory/sweep_stale|spellcheck|extract_facts` endpoint binding
- Wake-up bundle viewer

(Operator updates issue tracker after release lands.)

## Tests + smoke

```
Go build:  Success
Go test:   1469 passed in 58 packages (+5 router parsing, +10 memory)
Smoke:     72 pass / 0 fail / 4 skip (full release-smoke against dev daemon)
```

## Backwards compatibility

- All schema changes additive (`ALTER TABLE … ADD COLUMN IF NOT EXISTS`). Pre-v5.27 rows have empty values for the new columns; queries without filters on them return the same rows they did before.
- `SaveWithMeta` / `SaveWithNamespace` continue to work; both funnel through `SaveWithNamespaceAndSource` with empty `Source`.
- The `Backend` interface gained no required methods. New capabilities exposed via optional capability interfaces (`PinnableBackend`, ad-hoc type assertions) so the PG path keeps compiling without forcing every implementation to learn every new feature simultaneously.
- The `router.MemoryStore` interface DID gain methods — every MemoryStore implementation must add the 5 new methods (`SetPinned`, `SweepStaleSummary`, `SpellCheckSummary`, `ExtractFactsSummary`, `SchemaVersionString`). The in-tree `storeAdapter` is updated; downstream forks need to mirror.

## Upgrade path

```bash
git pull
datawatch update && datawatch restart
# Hard-reload PWA. SQLite + PG migrations run idempotently
# on first start. Existing rows default to '' for the new
# spatial axes and 0 for last_hit_at.
```

No data migration required.

## Why this is a minor not a major

Operator policy: pre-6.0 testing window. v6.0 ships at the operator-defined release date with the cumulative narrative; everything before that point lands as patch or minor releases under the v5.x umbrella. The earlier draft tagged this work as v6.0.0 — backed out before publish in favor of v5.27.0 so the v6.0 cut moment stays under operator control.

## What's left until v6.0

**Debugging only.** The mempalace alignment audit is closed, the PRD-flow phases are locked, the spatial schema is at parity, and every v6.1+/v6.2+/deferred candidate the operator named has shipped. Bug reports + smoke regressions only between now and v6.0.
