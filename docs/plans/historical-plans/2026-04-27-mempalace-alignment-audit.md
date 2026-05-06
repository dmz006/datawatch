# Mempalace alignment audit

**Status:** Audit / planning doc (no implementation in this doc).
**Owner:** —
**Tracks under:** Operator request 2026-04-27 — *"i meant against mempalace. add to backlog to do an audit against mempalace and latest features, look at expanding to full spatial memory like mempalace has and make a plan for additional memory updates based on latest features."*
**Author:** generated 2026-04-27 in v5.26.53 cycle.

---

## Current state — datawatch ↔ mempalace correspondences

Per `docs/plan-attribution.md` and `internal/memory/`:

| Mempalace concept | Datawatch implementation | Status |
|------|------|------|
| L0 identity | `Layers.L0()` from `<data_dir>/identity.txt` | ✅ shipped (BL96) |
| L1 critical facts | `Layers.L1(projectDir, maxChars)` — top memories | ✅ shipped |
| L2 room context | `Layers.L2(projectDir, topic, maxResults)` — wing/hall/room scoped | ✅ shipped (BL96) |
| L3 deep search | `/api/memory/search` (semantic) | ✅ shipped |
| L4 parent context (datawatch extension for F10) | `Layers.L4(parentNamespace, maxChars)` | ✅ shipped (BL96) |
| L5 sibling visibility (datawatch extension for F10) | `Layers.L5(selfID, parentAgentID)` | ✅ shipped (BL96) |
| Spatial dimensions | wing / hall / room columns in `pg_store.go` | ✅ shipped (BL55, v1.5.0) |
| Per-agent identity overlay | `Layers.L0ForAgent(agentID)` | ✅ shipped (BL96) |
| Agent diaries | `internal/memory/agent_diary.go` (BL97) | ✅ shipped |
| KG contradiction detection | `internal/memory/kg_contradictions.go` (BL98 — fact_checker port) | ✅ shipped |
| Verbatim → summary chain | `internal/memory/closets_drawers.go` (BL99) | ✅ shipped |

L0–L5 + spatial dims + the three named ports cover most of mempalace's headline architecture as documented in 2026-04-09 plans.

## How to do the audit

This document is the **starting frame** for the audit; it doesn't enumerate the gap because that requires pulling current mempalace upstream. The audit's deliverable is a follow-up doc that fills in section 4 below.

The audit proceeds in three steps. It's tractable in a few hours of focused time when an operator is ready to run it.

### Step 1 — pull current upstream

```bash
git clone https://github.com/milla-jovovich/mempalace /tmp/mp-audit
cd /tmp/mp-audit
git log --since=2026-04-09 --oneline > /tmp/mp-since-baseline.txt
```

The 2026-04-09 baseline is the date `docs/plans/2026-04-09-memory-backlog.md` was filed (BL43–BL67 covers the original port). Anything mempalace has shipped since is candidate gap.

Sanity check by reading `/tmp/mp-since-baseline.txt`'s top 30 commits — operators familiar with both repos can spot-check whether anything is conceptually new (vs. cleanups/refactors that don't change the feature surface).

### Step 2 — enumerate features by directory

Mempalace's typical layout (verify against actual upstream):

| Mempalace dir | Concept | Datawatch parallel? |
|------|------|------|
| `mempalace/wings/` | Per-project memory shards | wing column in `pg_store` ✓ |
| `mempalace/halls/` | Cross-project domain shards (facts, code, decisions) | hall column ✓ |
| `mempalace/rooms/` | Per-topic per-project shards | room column ✓ |
| `mempalace/closets/` | Verbatim quote storage | BL99 closets/drawers ✓ |
| `mempalace/drawers/` | Summary chain | BL99 closets/drawers ✓ |
| `mempalace/diaries/` | Per-agent journals | BL97 agent_diary ✓ |
| `mempalace/fact_checker/` | KG contradiction | BL98 kg_contradictions ✓ |
| `mempalace/corridors/` | (?) — confirm presence + role | Likely UNCOVERED |
| `mempalace/archives/` | (?) — confirm | Likely UNCOVERED |
| `mempalace/suites/` | (?) — confirm | Likely UNCOVERED |

The "Likely UNCOVERED" rows are the reason this doc is a placeholder for the real audit — without pulling upstream we can't enumerate them concretely. Operator's stated intent ("look at expanding to full spatial memory like mempalace has") implies *some* spatial constructs beyond wing/hall/room are absent.

### Step 3 — fill in the gap table

For each enumerated mempalace feature not in datawatch, complete this row:

```
| Feature | What it does | Single-host or composes with F10? | Effort (hrs) | Gain | Priority |
|------|------|------|------|------|------|
| corridors (TBD) | … | … | … | … | … |
```

Priority rubric:

- **High** — closes a user-visible recall failure mode the operator has reported.
- **Medium** — adds semantic precision; measurable retrieval gain.
- **Low** — niche or substantially overlaps with what we have.

## Quick-win shortlist (provisional — to be confirmed in audit)

Three candidates that are likely sub-day implementations IF mempalace upstream still has them in current form:

1. **Mempalace's auto-tagging on save.** Pre-classifies content into hall/room based on heuristics (e.g. file-path patterns, sentence shape). Datawatch derives wing from project_dir but doesn't auto-tag hall/room. ~3–4 hours.

2. **Memory pinning.** Mark a memory as "always-include in L1 even when relevance is low." Useful for project conventions / non-obvious gotchas. ~2 hours (add `pinned bool` column + filter in L1 ranking).

3. **Conversation-window stitching.** When closets/drawers fire, also stitch the surrounding chat-window context (10 messages before + after) into the summary. Datawatch's BL99 port may already do this — verify. ~variable.

These are placeholders; the actual audit may surface different / better candidates.

## Audit results — 2026-04-28

Pulled MemPalace/mempalace contents via `gh api`. Repo lives at
`github.com/MemPalace/mempalace` (note: the v1.5.0-era credit line in
`docs/plan-attribution.md` cites `milla-jovovich/mempalace` which redirects).

### Module-by-module gap table

| Mempalace module | Concept | Datawatch parallel | Status |
|---|---|---|---|
| `layers.py` | L0–L3 wake-up stack | `internal/memory/layers.go` (L0–L3) + `layers_recursive.go` (L4/L5 datawatch extension) | ✅ ported + extended |
| `palace.py` / `palace_graph.py` | Wing/hall/room schema | `pg_store.go` wing/hall/room columns (BL55) | ✅ ported |
| `fact_checker.py` | KG contradiction detection | `internal/memory/kg_contradictions.go` (BL98) | ✅ ported |
| `closet_llm.py` + `diary_ingest.py` | Closets/drawers verbatim → summary | `internal/memory/closets_drawers.go` (BL99) | ✅ ported |
| `entity_detector.py` + `entity_registry.py` | Entity detection (BL60) | `internal/memory/entities.go` (BL60) | ✅ ported |
| `dedup.py` | Deduplication | `pg_store.go` SHA-based dedup (BL63) | ✅ ported |
| `convo_miner.py` + `convo_scanner.py` | Conversation mining | datawatch session mining via `memory_import` MCP tool (partial) | 🟡 partial |
| `room_detector_local.py` | Heuristic room/wing classifier on save | NOT in datawatch — this is the **#1 quick win** in the audit's provisional shortlist | ⏳ gap |
| `embedding.py` | Embedder abstraction | `internal/memory/embedder.go` (Ollama / OpenAI) | ✅ ported |
| `llm_refine.py` | Post-save LLM re-summarization | `internal/memory/refine.go` (partial — auto-summary on save) | 🟡 partial |
| `searcher.py` | Multi-mode search | `/api/memory/search` (semantic) + `/list` (filtered) | ✅ ported |
| `mcp_server.py` | MCP-over-stdio for memory tools | datawatch MCP exposes memory_save/import/learnings (subset) | 🟡 partial — `memory_recall` / `kg_query` not in stdio surface |
| `corpus_origin.py` | Source attribution per memory row | datawatch has `Memory.Source` field; population is partial | 🟡 partial |
| `dialect.py` + `normalize.py` | Text normalization pre-save | NOT in datawatch (operator unicode-noise stays raw) | ⏳ gap |
| `general_extractor.py` | Schema-free fact extraction | NOT in datawatch (datawatch only does entity extraction) | ⏳ gap |
| `migrate.py` | Schema-version migration | datawatch uses pg_store auto-migrate; no explicit pass | 🟡 partial |
| `onboarding.py` | First-run wizard for memory setup | datawatch has `datawatch setup memory` wizard | ✅ ported (different shape) |
| `query_sanitizer.py` | Query string sanitization | NOT explicit in datawatch | ⏳ gap |
| `repair.py` | Self-repair (broken embeddings, orphan rows) | NOT in datawatch | ⏳ gap |
| `spellcheck.py` | Spellcheck on ingest | NOT in datawatch | ⏳ gap |
| `split_mega_files.py` | Chunking for large source files | datawatch chunks at session-output ingest (BL52) | ✅ ported |
| `sweeper.py` | Periodic stale-row eviction | datawatch has tier-3 retention policies (BL47) | 🟡 partial |
| `instructions/` | Bundled prompt instructions | NOT in datawatch (operator's CLAUDE.md serves the role) | ✅ functional parity |
| `convo_miner.py` (window stitching) | Conversation-window stitching for closets/drawers | NOT in datawatch BL99 — **#3 quick win** in shortlist | ⏳ gap |

### Quick-win shortlist (refined from provisional)

The audit-frame doc proposed three quick wins; pulling actual upstream
sharpens them:

1. **Auto-tagging on save (room_detector_local.py port).**
   Mempalace runs a heuristic classifier (file-path patterns, sentence
   shape, keyword anchors) at save time to assign `wing` / `hall` /
   `room` automatically. Datawatch derives `wing` from project_dir
   only; `hall` and `room` stay empty unless an operator passes them.
   **Effort:** 4–6 hours (port the classifier + add a save hook +
   unit tests). High-value: improves L2 scoping.

2. **Memory pinning** (NEW — wasn't in the provisional list, but more
   compelling after seeing mempalace's `repair.py` which has it).
   Add a `pinned bool` column + L1 ranking boost. ~2 hours.

3. **Conversation-window stitching for BL99 closets/drawers.**
   Verify whether datawatch's port already does this; mempalace's
   `convo_miner.py` stitches ±10 messages around the verbatim quote
   into the summary. ~3 hours if missing.

4. **Query sanitizer port** (`query_sanitizer.py`). Defensive — strips
   prompt-injection patterns from search queries before embedding.
   ~2 hours.

5. **Self-repair pass** (`repair.py`). Periodic check for broken
   embeddings + orphan rows. Could share infrastructure with the
   existing tier-3 retention sweeper. ~1 day.

### Implementation plan for the quick wins

Operator-paced. Each lands as its own patch with full configuration
parity (yaml + REST + MCP + CLI + comm) per the project rules.
None of these block v6.0 — they're additive v6.1+ improvements.

## Out of scope for the audit itself

- Implementation. Audit produces this filled-out plan + a quick-win shortlist; subsequent BLs implement.
- v6.0 cut. Mempalace alignment is a v6.1+ topic per the operator directive (v6.0 is feature-stable; this is additive).

## Open questions for the operator

1. Is there a specific recall failure mode that motivated the audit ask? (Knowing the symptom helps the priority rubric.)
2. Is there a feature in mempalace upstream's current docs/README the operator can name as a known target? (Saves a step of the audit.)
3. For multi-agent F10 contexts: should every imported mempalace feature compose cleanly with parent/sibling/diary already in place, or is single-host parity acceptable for v6.1?

## Hand-off

This doc lives at `docs/plans/2026-04-27-mempalace-alignment-audit.md`. When an operator (or a worker) is ready to run the audit:

1. Step 1 (pull upstream).
2. Fill in section §3 — Step 3 gap table.
3. Promote 1–3 quick wins to actual BL items.
4. Replace this provisional shortlist with the audit-confirmed one.
5. Cross-link the audit deliverable from `docs/plan-attribution.md` so attribution stays current.
