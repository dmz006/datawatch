# Mempalace audit follow-ups + additional spatial dims

**Date:** 2026-04-28
**Owner:** operator (paced)
**Status:** plan; v6.1+ work after v6.0 cut

After v5.26.70 closed the mempalace audit's quick-win shortlist (QW#1–5),
this doc enumerates what's still open and the spatial-memory expansion
the operator asked about ("did we add the additional spatial memory
from the recent memory audit?").

## Did v5.26.70 add additional spatial dims?

**No** — v5.26.70 added auto-tagging for the *existing* wing/room/hall
schema (QW#1) but did not introduce new spatial axes. The audit's
2026-04-27 module-by-module table marked `palace.py` /
`palace_graph.py` (wing/hall/room) as ✅ ported and the operator's
"expand to full spatial memory like mempalace has" intent was carried
into the audit's open questions, not into the v5.26.70 patch.

Mempalace's full spatial schema includes more axes than wing/room/hall.
A quick read of `closets_drawers.go:5` already notes datawatch covers
"3 of 6 levels". The remaining 3 axes most worth porting:

### Proposed spatial axes (v6.1+)

| Axis | Mempalace term | Purpose | Effort |
|---|---|---|---|
| **floor** | `floor_id` | Outermost grouping above wing — separates *organisations* / *workspaces* when one operator has multiple orgs (e.g. work + personal). Currently datawatch uses one wing per project; operators with N projects under M orgs lose the org grouping. | 1 day (column + filter + UI) |
| **shelf** | `shelf_label` | Sub-room organisation within a single room. Useful for long-running rooms (`auth`, `db`) where `shelf` carves out subtopics (`auth/oauth`, `auth/sessions`, `auth/2fa`). Mempalace uses it for query-time facets. | 1 day (column + UI facets + L2 layer awareness) |
| **box** | `box_id` | Per-author or per-source bundling within a shelf. Distinguishes "operator wrote this" vs "agent X wrote this" without leaking through namespace, so cross-namespace queries can still group by author. | 4–6 hours (column + author tag) |

Each is a column add + UI filter exposure — schema-additive, no
breaking change. They compose multiplicatively with wing/room/hall:
floor → wing → shelf → room → hall → box.

**Open question:** does the operator want all three at once, or
prioritise floor (org grouping) since multi-org is the visible
gap from production use? Recommendation in the absence of an
answer: floor first, alone, in v6.1.0; shelf+box together in v6.1.1
once floor proves out.

## Remaining mempalace audit items

The 2026-04-27 audit table listed 24 modules. After v5.26.70 the
remaining gaps:

### Outright gaps (⏳)

| Module | Concept | v5.26.70 status | Plan |
|---|---|---|---|
| `dialect.py` + `normalize.py` | Pre-save unicode/whitespace/punctuation normalization | not in datawatch | New `internal/memory/normalize.go`. Pure func that NFC-normalizes, collapses whitespace, optionally maps fancy quotes. Hook into `SaveWithNamespace` after `AutoTag`. ~4 hours. |
| `general_extractor.py` | Schema-free fact extraction from free-form text | not in datawatch | LLM-backed `Extract(text) []Fact` that returns subject-predicate-object triples without a fixed schema. Belongs to KG path; complements `entity_detector.go`. Operator-paced — depends on which LLM backend should drive it. ~1 day. |
| `spellcheck.py` | Spellcheck on ingest | not in datawatch | Optional, low-priority. Mempalace uses it to fix transcribed input from voice. Datawatch's voice path goes through whisper which produces clean output already. **Recommendation: don't port unless an operator hits a recall failure traceable to typos.** |

### Partials (🟡)

| Module | What's there | What's missing | Plan |
|---|---|---|---|
| `convo_miner.py` + `convo_scanner.py` | `memory_import` MCP tool covers Claude Code / ChatGPT / generic JSON | Mempalace also mines Slack DMs, IRC logs, and email threads. | Decide per-source: Slack via `slack_export` package; email via mbox parser. Each source = ~4 hours. v6.1+ as operators ask. |
| `llm_refine.go` | Auto-summary on save (one-shot) | Mempalace re-summarizes periodically (daily sweep) to compress accumulated session content into denser learnings | New `RefineSweep` cron-style job — walk session memories older than 7d, re-summarize via LLM, replace content with summary. ~6 hours. |
| `corpus_origin.py` | `Memory.Source` field present | Population is partial — only manual `remember:` calls set it explicitly. Channel commands, MCP tool calls, KG adds don't tag source. | Add Source on every entry path. ~3 hours, mostly find-and-update. |
| `migrate.py` | pg_store auto-migrate runs `IF NOT EXISTS` | No version table → can't tell which migration ran | Add `schema_version` table + ordered migration list. ~4 hours. |
| `sweeper.py` | tier-3 retention policies (BL47) | Mempalace also evicts low-similarity rows (rows that never matched a query) | Track `last_hit_at` column + add eviction by similarity-stale rule. ~6 hours. |

### Already covered (✅) — no follow-up

`layers.py`, `palace.py`, `fact_checker.py`, `closet_llm.py` /
`diary_ingest.py`, `entity_detector.py`, `entity_registry.py`,
`dedup.py`, `embedding.py`, `searcher.py`, `onboarding.py`,
`split_mega_files.py`, `instructions/` — see audit doc for parity
notes.

## Other open items beyond the audit

| Item | Source | Plan |
|---|---|---|
| `memory_recall` / `kg_query` in stdio MCP surface | audit partial line 116 | **CLOSED in v5.26.71** — registered always-on in `mcp.New()` so `datawatch mcp` surfaces them in `tools/list`. Stdio panic on nil reader/writer also fixed. |
| Stdio MCP integration smoke | tracked as #67 | **CLOSED in v5.26.71** — `scripts/release-smoke-stdio-mcp.sh` spawns `datawatch mcp`, sends JSON-RPC, validates initialize/tools/list/tools/call. Wired into release-smoke §7r. |
| L4/L5 wake-up integration smoke | tracked as #68 | **CLOSED in v5.26.71** — new `GET /api/memory/wakeup` REST endpoint composes the bundle on demand; `scripts/release-smoke-wakeup.sh` probes 3 shapes (L0+L1, +L4, +L5). Wired into release-smoke §7s. |
| GHCR past-minor cleanup | tracked as #69 | **CLOSED in v5.26.71** — `.github/workflows/ghcr-cleanup.yaml` runs weekly + workflow_dispatch, deletes versions from closed minor lines, keeps latest patch of each closed minor + `latest`. Uses `GITHUB_TOKEN` / packages:write — no PAT needed. Operator validates dry-run output before flipping `dry_run: false`. |

## Cut sequence

v5.26.71 (this patch) closes #67/#68/#69 + the stdio-surface partial.
v6.0.0 ships as planned with no further mempalace work; everything
above becomes v6.1+.

**v6.1 candidate scope (operator decides):**
- floor spatial axis (proposed first)
- `normalize.go` (low effort, high signal)
- `corpus_origin` follow-through (Source population)

**v6.2+:**
- shelf + box axes
- `general_extractor` if LLM backend choice settles
- `migrate.py` schema_version table
- `llm_refine` periodic sweep
- `sweeper.py` similarity-stale eviction

Spellcheck and Slack/email convo miners stay deferred until an
operator hits a concrete recall failure traceable to either.
