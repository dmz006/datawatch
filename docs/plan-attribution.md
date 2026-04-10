# Plan Attribution Guide

When plans or features in datawatch are inspired by or derived from other projects,
the source must be credited in the plan document.

---

## Why

Transparency matters. Contributors and reviewers should know where design ideas
originate, especially when cross-pollinating between projects.

## How to credit

Add a **Source** field near the top of the plan metadata:

```markdown
# Feature Name

**Date:** 2026-04-10
**Source:** Inspired by [project-name](link) — brief description of what was borrowed
**Priority:** medium
**Effort:** 2-3 days
```

For multiple sources:

```markdown
**Source:**
- Memory architecture from [hackerdave](link) — spatial memory organization (wings/rooms/halls)
- Wake-up stack from [milla jovovich](link) — 4-layer context loading (L0–L3)
```

## What to include

- **Project name** — the originating project or repo
- **Link** — URL to the project, repo, or specific file/doc if applicable
- **What was borrowed** — brief description of the concept, pattern, or design
- **Adaptation notes** (optional) — how the idea was modified for datawatch

## Known source projects

| Project | Contributions to datawatch |
|---------|---------------------------|
| hackerdave | Memory system concepts, spatial organization, knowledge graph |
| milla jovovich | Wake-up stack, context layering patterns |

*Update this table as new source projects are referenced.*

---

## Feature map: what was included and what was built on top

### From hackerdave / mempalace

The episodic memory system was originally inspired by hackerdave's mempalace architecture.

#### Directly included (adapted for datawatch)

| Feature | BL# | Version | Description |
|---------|-----|---------|-------------|
| Spatial organization | BL55 | v1.5.0 | Wing/room/hall columns for hierarchical memory. Auto-derive wing from project path, hall from role. +34pp retrieval improvement per mempalace benchmarks. |
| Verbatim storage mode | BL58 | v1.5.0 | Store full prompt+response text instead of summaries. Higher retrieval accuracy (96.6% vs ~80%). |
| Entity detection | BL60 | v1.5.0 | Regex-based extraction of people, tools, and projects from text. Auto-populates knowledge graph. |
| Mempalace import | BL67 | v2.0.0 | Conversation mining supports generic JSON format compatible with mempalace exports. |
| Cross-project search | BL49 | v2.0.0 | Search memories across all projects via `RecallAll`. |

#### Built because it was inspired by the project

These features did not exist in the source project but were designed and built because working with the mempalace concepts revealed the need for them.

| Feature | BL# | Version | Description | Why it was built |
|---------|-----|---------|-------------|------------------|
| Temporal knowledge graph | BL57 | v1.5.0 | SQLite-backed entity-relationship triples with validity windows. `kg add/query/timeline/stats` commands. Point-in-time queries and invalidation. | Spatial memory needed relationship tracking between entities — mempalace had flat storage only. |
| Hybrid content encryption | BL68 | v1.6.0 | XChaCha20-Poly1305 on content/summary fields, embeddings stay searchable. | Storing verbatim text raised security concerns not addressed in mempalace. |
| Key rotation & management | BL70 | v1.6.0 | Generate, rotate, backup, fingerprint, import/export encryption keys. | Required by BL68 for production use. |
| PostgreSQL+pgvector backend | BL43 | v2.0.2 | Full memory store on PostgreSQL with vector search, spatial search, KG tables. | SQLite-only backend from mempalace doesn't scale for team deployments. |
| Deduplication | BL63 | v2.0.0 | Content-hash dedup prevents redundant storage. | High-volume auto-save created duplicates not handled in mempalace. |
| Write-ahead log | BL62 | v2.0.0 | Audit trail for all memory writes. | Needed for trust and debugging in multi-channel environment. |
| Embedding cache | BL50 | v2.0.0 | Cache embeddings to reduce API calls. | Cost optimization for Ollama/OpenAI embedders not present in mempalace. |
| Cross-project tunnels | BL64 | v2.0.0 | Share memories between projects via spatial tunnels. | Extended spatial organization beyond single-project scope. |
| Conversation mining | BL59 | v2.0.0 | Ingest Claude JSONL, ChatGPT JSON, generic exports. | Bulk import from AI conversation history — new use case beyond mempalace. |
| Claude Code auto-save hooks | BL65-66 | v2.0.0 | Shell hooks auto-save to memory every N exchanges, plus pre-compact saves. | Integration with Claude Code workflow not applicable to mempalace. |
| Session output auto-index | BL52 | v2.0.0 | Automatically index completed session output as searchable memories. | Bridges session management (datawatch-specific) with memory system. |
| Batch reindexing | BL51 | v2.0.0 | `memories reindex` after embedding model change. | Operational need when switching between Ollama models. |
| Retention policies | BL47 | v2.0.0 | Per-role TTLs for automatic memory expiration. | Enterprise requirement not present in personal mempalace. |
| Learning quality scoring | BL53 | v2.0.0 | Score and rank task learnings by relevance. | Built on top of BL36 learnings system, unique to datawatch. |
| Memory export/import | BL46 | v2.0.0 | JSON serialization for backup and migration. | Portability between SQLite and PostgreSQL backends. |

### From milla jovovich

The context layering and wake-up patterns were inspired by milla jovovich's approach to AI context management.

#### Directly included (adapted for datawatch)

| Feature | BL# | Version | Description |
|---------|-----|---------|-------------|
| 4-layer wake-up stack | BL56 | v1.5.0 | L0 identity (`identity.txt`), L1 critical facts (top learnings), L2 room context (topic-triggered), L3 deep search (existing recall). ~600 token auto-context on every session start. |

#### Built because it was inspired by the project

| Feature | BL# | Version | Description | Why it was built |
|---------|-----|---------|-------------|------------------|
| Auto-retrieve on session start | BL44 | v1.3.0 | Embed the task, search memory, inject relevant context as preamble. | Wake-up stack revealed that cold-start sessions needed automatic context, not just manual recall. |
| Per-session Claude Code hooks | v2.0.1 | — | Auto-hooks that save session context to memory at configurable intervals. | Extended the wake-up concept to continuous saving, not just loading. |
| Session awareness & broadcast | v2.0.1 | — | Memory config for `session_awareness` and `session_broadcast` to share context across concurrent sessions. | Multi-session coordination inspired by the layered context model. |

---

## Scope

This applies to:
- Plan documents in `docs/plans/`
- Architecture docs that reference external designs
- Backlog items that originate from other project explorations

It does **not** apply to:
- Standard library or framework usage
- Common design patterns (MVC, pub/sub, etc.)
- Bug fixes or refactors with no external inspiration
