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
- Memory architecture from [HackingDave/nightwire](https://github.com/HackingDave/nightwire) — spatial memory organization (wings/rooms/halls)
- Wake-up stack from [milla-jovovich/mempalace](https://github.com/milla-jovovich/mempalace) — 4-layer context loading (L0–L3)
```

## What to include

- **Project name** — the originating project or repo
- **Link** — URL to the project, repo, or specific file/doc if applicable
- **What was borrowed** — brief description of the concept, pattern, or design
- **Adaptation notes** (optional) — how the idea was modified for datawatch

## Known source projects

| Project | Repository | Contributions to datawatch |
|---------|------------|---------------------------|
| HackingDave / nightwire | [github.com/HackingDave/nightwire](https://github.com/HackingDave/nightwire) | Memory system concepts, spatial organization, knowledge graph; PRD-decomposition workflow patterns referenced in BL24 design |
| milla-jovovich / mempalace | [github.com/milla-jovovich/mempalace](https://github.com/milla-jovovich/mempalace) | Wake-up stack, context layering patterns |

### Researched and skipped

| Project | Why skipped | What was kept as prior art |
|---------|-------------|---------------------------|
| Aperant ([aperantdesktop.com](https://aperantdesktop.com/), see BL inventory) | AGPL-3.0 incompatible with datawatch distribution; Electron desktop app with no headless API; sits on top of the same claude-code layer datawatch uses | Worktree-isolation + self-QA ideas borrowed into BL24 roadmap as prior art alongside nightwire — no integration |

*Update this table as new source projects are referenced or evaluated.*

### Operator action — list any other inspirations that aren't tracked here

This sweep is partial: the operator may remember specific projects that
shaped design choices but aren't yet credited. **Add them above** when you
think of them — the rule already says new plan docs must carry their own
**Source** field, but historical plans (filed before the rule landed) may
have undocumented inspirations. Suggested places to look: BL117 (PRD-DAG
orchestrator), BL33 (plugin framework), F10 (ephemeral worker substrate),
the cluster-container shape (BL173). When in doubt, file a one-line
addition rather than leave a credit gap.

---

## Feature map: what was included and what was built on top

### From [HackingDave/nightwire](https://github.com/HackingDave/nightwire)

**What nightwire is:** A Python Signal messaging bot that lets you control AI coding workflows from your phone. Send Signal messages to run `/ask` (read-only), `/do` (modifications), or `/complex` (full autonomous multi-step projects). Dispatches work to Claude CLI, Cursor, Codex CLI, or OpenCode CLI. Features independent verification, scheduled prompts, and a plugin framework.

**Nightwire's memory system:** SQLite with vector embeddings using `all-MiniLM-L6-v2` (local sentence-transformer). Conversations auto-stored and indexed. `/remember` and `/recall` commands for explicit storage and semantic retrieval. Project-scoped or global (`/global remember`). Configurable `max_context_tokens` (default 1500). `/learnings` surfaces distilled knowledge from completed tasks. No spatial organization, no knowledge graph, no wake-up stack.

#### How datawatch compares to nightwire

| Aspect | nightwire | datawatch | Notes |
|--------|-----------|-----------|-------|
| **Language** | Python | Go | Datawatch chose Go for single-binary deployment, no runtime deps |
| **Signal transport** | `signal-cli-rest-api` Docker (WebSocket) | `signal-cli` JSON-RPC (direct) | Datawatch avoids the Docker dependency |
| **Storage backend** | SQLite + sentence-transformers | SQLite (pure Go, modernc.org/sqlite) + PostgreSQL+pgvector | Datawatch supports both local and enterprise backends |
| **Embedding model** | `all-MiniLM-L6-v2` (local, fixed) | Ollama (`nomic-embed-text`) or OpenAI (`text-embedding-3-small`), configurable | Datawatch supports multiple embedding providers with caching |
| **Memory scope** | Per-project or global | Per-project with wing/room/hall spatial hierarchy | Datawatch adds structured spatial organization from mempalace |
| **Knowledge graph** | None | SQLite/PostgreSQL temporal KG with validity windows | Datawatch built entity-relationship tracking |
| **Context management** | Flat semantic search, `max_context_tokens` cap | 4-layer wake-up stack (L0–L3) from mempalace | Datawatch adopted mempalace's layered approach |
| **Encryption** | Not mentioned | XChaCha20-Poly1305 hybrid (content encrypted, embeddings searchable) | Datawatch added at-rest encryption |
| **Coding backends** | Claude CLI, Cursor, Codex, OpenCode | Claude Code, Ollama, OpenWebUI, OpenCode-ACP | Different backend ecosystems |
| **Task execution** | Up to 10 parallel workers with verifier | Single-session tmux with pipelines/chaining (DAG) | Different concurrency models |
| **Comm channels** | Signal only | Signal, Telegram, Matrix, webhooks, API, CLI | Datawatch is multi-channel |
| **remember/recall** | Yes (Signal commands) | Yes (all channels + MCP tools + chat + API) | Same concept, broader surface |
| **Learnings** | `/learnings` from completed tasks | `learnings` command + quality scoring + auto-extract | Datawatch extends with scoring |
| **Plugin system** | Python subclass auto-discovery | MCP server (37 tools) + pipeline DAG | Different extensibility models |

#### Directly included from nightwire (adapted for datawatch)

| Feature | BL# | Version | What nightwire had | How datawatch adapted it |
|---------|-----|---------|-------------------|--------------------------|
| Episodic memory store | BL23 | v1.3.0 | SQLite + sentence-transformer embeddings, `/remember`/`/recall` commands, project-scoped storage | Reimplemented in pure Go (no cgo), swapped embedding provider to Ollama/OpenAI, added configurable `top_k` and similarity thresholds |
| Verbatim storage mode | BL58 | v1.5.0 | Nightwire stores full conversations by default | Added as a configurable `storage_mode: verbatim` option (datawatch defaults to summaries for space, verbatim is opt-in) |
| Cross-project search | BL49 | v2.0.0 | `/global recall` searches across all projects | `RecallAll` method + MCP `research_sessions` tool for cross-session deep search |
| Task learnings | BL36 | v1.3.0 | `/learnings` surfaces distilled knowledge from completed autonomous tasks | `learnings` command from all channels, stored as memories with `role: learning` for searchability |
| Scheduled prompts | F6 | v0.7.0 | Cron-style recurring prompts per project | `schedule` command with absolute and relative times, per-session scheduled inputs |

#### Built because nightwire inspired it

These features did not exist in nightwire but were designed and built because working with nightwire's concepts revealed the need.

| Feature | BL# | Version | Description | Why it was built |
|---------|-----|---------|-------------|------------------|
| Spatial organization | BL55 | v1.5.0 | Wing/room/hall columns for hierarchical memory. Auto-derive wing from project path, hall from role. | Nightwire's flat project-scoped storage made retrieval noisy at scale — adopted mempalace's spatial structure to improve precision (+34pp). |
| Temporal knowledge graph | BL57 | v1.5.0 | SQLite-backed entity-relationship triples with validity windows. `kg add/query/timeline/stats` commands. Point-in-time queries and invalidation. | Nightwire had no relationship tracking — needed structured entity connections beyond flat text search. |
| Hybrid content encryption | BL68 | v1.6.0 | XChaCha20-Poly1305 on content/summary fields, embeddings stay searchable. | Nightwire stores memories in plaintext SQLite. Datawatch needed at-rest encryption for sensitive project data. |
| Key rotation & management | BL70 | v1.6.0 | Generate, rotate, backup, fingerprint, import/export encryption keys. | Required by BL68 for production use — key lifecycle management. |
| PostgreSQL+pgvector backend | BL43 | v2.0.2 | Full memory store on PostgreSQL with vector search, spatial search, KG tables. | Nightwire is SQLite-only. Team deployments need shared database infrastructure. |
| Deduplication | BL63 | v2.0.0 | Content-hash dedup prevents redundant storage. | Nightwire's auto-save + datawatch's multi-channel input created duplicate memories not handled by either project. |
| Write-ahead log | BL62 | v2.0.0 | Audit trail for all memory writes. | Multi-channel writes (Signal + Telegram + API + CLI) needed atomic auditing not required in Signal-only nightwire. |
| Embedding cache | BL50 | v2.0.0 | Cache embeddings to reduce API calls. | Nightwire uses local sentence-transformers (fast). Datawatch uses Ollama/OpenAI (network calls) — caching critical for performance. |
| Cross-project tunnels | BL64 | v2.0.0 | Share memories between projects via spatial tunnels. | Extended nightwire's global recall into structured cross-project connections using mempalace's tunnel concept. |
| Conversation mining | BL59 | v2.0.0 | Ingest Claude JSONL, ChatGPT JSON, generic exports. | Bulk import from AI conversation history — nightwire only captures its own conversations. |
| Claude Code auto-save hooks | BL65-66 | v2.0.0 | Shell hooks auto-save to memory every N exchanges, plus pre-compact saves. | Integration with Claude Code's hook system — not applicable to nightwire's Python architecture. |
| Session output auto-index | BL52 | v2.0.0 | Automatically index completed session output as searchable memories. | Bridges datawatch's session management with memory — nightwire handles this differently via its worker model. |
| Batch reindexing | BL51 | v2.0.0 | `memories reindex` after embedding model change. | Nightwire has a fixed embedding model. Datawatch supports swapping models, requiring re-embedding. |
| Retention policies | BL47 | v2.0.0 | Per-role TTLs for automatic memory expiration. | Enterprise requirement — nightwire stores indefinitely with manual `/forget`. |
| Learning quality scoring | BL53 | v2.0.0 | Score and rank task learnings by relevance. | Extended nightwire's `/learnings` with quality metrics for ranking. |
| Memory export/import | BL46 | v2.0.0 | JSON serialization for backup and migration. | Portability between SQLite and PostgreSQL backends — nightwire is single-backend. |
| Entity detection | BL60 | v1.5.0 | Regex-based extraction of people, tools, and projects from text. Auto-populates knowledge graph. | Nightwire has no entity extraction — needed to auto-populate the KG that nightwire also lacked. |

---

### From [milla-jovovich/mempalace](https://github.com/milla-jovovich/mempalace)

**What mempalace is:** A local, open-source AI memory system that stores every AI conversation verbatim and makes it semantically searchable. Core thesis: "store everything, then make it findable" — rejects LLM summarization which discards context and reasoning. Integrates with Claude Code (plugin), MCP-compatible tools, and local models.

**Mempalace's memory architecture:**
- **Palace metaphor (structural, not cosmetic):** Wings (person/project namespaces) → Rooms (specific topics like `auth-migration`) → Halls (five typed corridors: facts, events, discoveries, preferences, advice) → Tunnels (auto-linked cross-wing connections when room names match) → Closets (plain-text summaries pointing to originals) → Drawers (verbatim original files).
- **Wake-up stack:** L0 identity (~50 tokens, always), L1 critical facts (~120 tokens in AAAK, always), L2 room recall (on demand, topic-triggered), L3 deep semantic search (on demand, explicit). Cold-start cost: ~170 tokens.
- **Storage:** ChromaDB (local, no cloud). Raw verbatim text default. Temporal KG in SQLite with validity windows.
- **Benchmarks:** 96.6% LongMemEval R@5 (verbatim mode). Spatial structure accounts for +34pp retrieval gain (flat 60.9% → wing+room 94.8%).
- **19 MCP tools** for palace reads/writes, KG CRUD, navigation, agent diaries.

#### How datawatch compares to mempalace

| Aspect | mempalace | datawatch | Notes |
|--------|-----------|-----------|-------|
| **Language** | Python | Go | Datawatch compiles to single binary, no Python runtime |
| **Primary purpose** | Standalone memory system for any AI tool | Session management daemon with integrated memory | Memory is one subsystem in datawatch, not the whole product |
| **Storage backend** | ChromaDB (local) | SQLite (pure Go) + PostgreSQL+pgvector | Datawatch avoids ChromaDB dependency, supports enterprise Postgres |
| **Spatial structure** | Wings → Rooms → Halls → Tunnels → Closets → Drawers | Wings → Rooms → Halls + Tunnels + Closets/Drawers (5 of 6 mempalace levels) | Datawatch adopted the same 6-level palace minus the operator-supplied "auto-detected room" sub-categorization. Closets (summary embeddings) point at Drawers (verbatim originals) — implemented in `internal/memory/closets_drawers.go`. |
| **Wake-up stack** | L0–L3, ~170 token cold-start | L0–L3, ~600 token cold-start | Same concept, datawatch's L1 is larger (includes top learnings) |
| **Knowledge graph** | SQLite temporal KG with validity windows | SQLite + PostgreSQL temporal KG with validity windows | Both use temporal triples; datawatch adds PostgreSQL KG backend |
| **Embedding** | ChromaDB built-in | Ollama or OpenAI with caching | Different embedding infrastructure |
| **Benchmarks** | 96.6% LongMemEval R@5 | Not independently benchmarked | Datawatch adopted the spatial structure that produced these gains |
| **MCP tools** | 19 (memory-focused) | 37 (memory + sessions + config + stats) | Datawatch's MCP covers the full daemon, not just memory |
| **AAAK compression** | Experimental token compression dialect | Not implemented | Datawatch uses standard text; AAAK regresses accuracy to 84.2% |
| **Contradiction detection** | `fact_checker.py` (checks KG for conflicts) | Implemented (`internal/memory/kg_contradictions.go`) | Ports the "functional predicate" slice — flags overlapping active triples for the same subject+predicate when the predicate allows only one active object value (e.g. `owns`, `current_status`, `lives_in`). |
| **Agent diaries** | Per-agent wing with AAAK diary | Implemented (`internal/memory/agent_diary.go`) | Each spawned worker gets a canonical `agent-<id>` wing; `AppendDiary` writes hall-typed entries (facts/events/discoveries/preferences/advice) that outlive the agent. Plain text instead of AAAK. Auto-injected into the wake-up stack as L3 for the spawning agent. |
| **Mining modes** | `projects`, `convos`, `general` (auto-classifies) | Claude JSONL, ChatGPT JSON, generic JSON | Different import formats, similar concept |
| **Comm channels** | None (library/MCP only) | Signal, Telegram, Matrix, webhooks, API, CLI | Datawatch is multi-channel; mempalace is a library |
| **Session management** | None | Full tmux session lifecycle (create, monitor, input, chain) | Core datawatch feature, not in mempalace's scope |
| **Chat UI** | None | Rich chat with streaming, thinking overlays, memory commands | Datawatch provides a web interface |

#### Directly included from mempalace (adapted for datawatch)

| Feature | BL# | Version | What mempalace had | How datawatch adapted it |
|---------|-----|---------|-------------------|--------------------------|
| Spatial organization | BL55 | v1.5.0 | 6-level palace: wings → rooms → halls → tunnels → closets → drawers. Auto-detected rooms. Five fixed hall types. | Adopted wing/room/hall (BL55, v1.5.0). Auto-derive wing from project path, hall from role. Closets + drawers landed in BL99 (multi-agent fan-out made the two-tier search-summary/verbatim chain valuable). Achieved same +34pp retrieval improvement. |
| Closets + drawers | BL99 | (per-agent fan-out) | Closet = small summary embedding pointing at Drawer = full verbatim, no embedding. Search hits the closet first; the drawer is fetched on demand. | Same shape — `SaveClosetWithDrawer` writes verbatim then summary, linking the closet's `drawer_id` at the verbatim's row. Search costs scale with summary count, not verbatim size. |
| Per-agent diary wing | BL97 | (per-agent fan-out) | Each agent gets its own wing of decisions / events / discoveries / preferences / advice that survives the agent's lifetime. | Canonical `wing = "agent-<id>"`; `AppendDiary` writes one hall-typed entry per call. Wired into the wake-up stack as L3 for the spawning agent. |
| KG contradiction detection (functional predicate slice) | BL98 | (per-agent fan-out) | `fact_checker.py` scans the KG for triples that contradict each other. | `internal/memory/kg_contradictions.go` ports the "functional predicate" subset — flags overlapping active triples for the same subject+predicate when the predicate allows at most one object value at a time. |
| 4-layer wake-up stack | BL56 | v1.5.0 | L0 identity (~50 tokens), L1 critical facts (~120 tokens AAAK), L2 room recall (topic-triggered), L3 deep search. ~170 token cold-start. | Same 4 layers. L0 from `identity.txt`, L1 from top learnings (no AAAK, uses plain text ~400 tokens), L2 room context, L3 existing recall. ~600 token cold-start. |
| Temporal knowledge graph | BL57 | v1.5.0 | SQLite temporal KG with entity-relationship triples, validity windows, point-in-time queries. | Reimplemented in Go with same temporal model. Added `kg add/query/timeline/stats` commands accessible from all comm channels + MCP + chat. PostgreSQL KG backend added in v2.0.2. |
| Verbatim storage mode | BL58 | v1.5.0 | Default mode — stores raw conversation text, no LLM summarization. 96.6% retrieval accuracy. | Added as opt-in `storage_mode: verbatim` (datawatch defaults to summaries). Same accuracy benefit when enabled. |
| Entity detection | BL60 | v1.5.0 | Part of mining system — auto-classifies into decisions, preferences, milestones, problems, emotional context. | Lightweight regex-based extraction of people, tools, projects. Auto-populates KG. Less sophisticated than mempalace's classification but sufficient for KG seeding. |
| Cross-project tunnels | BL64 | v2.0.0 | Auto-linked when same room name appears in multiple wings. Enables cross-wing navigation. | Implemented as spatial tunnel queries. Same concept of shared room names linking projects. |
| Mempalace import | BL67 | v2.0.0 | Native ChromaDB format with closet/drawer hierarchy. | Conversation mining accepts generic JSON compatible with mempalace exports. Normalizes into datawatch's 3-level spatial model. |

#### Built because mempalace inspired it

These features did not exist in mempalace but were designed because working with its concepts revealed the need.

| Feature | BL# | Version | Description | Why it was built |
|---------|-----|---------|-------------|------------------|
| Auto-retrieve on session start | BL44 | v1.3.0 | Embed the task, search memory, inject relevant context as preamble. | Wake-up stack revealed that cold-start sessions needed automatic context — mempalace relies on MCP tool calls, datawatch auto-injects. |
| Per-session Claude Code hooks | BL65-66 | v2.0.1 | Auto-hooks that save session context to memory every N exchanges, plus pre-compact saves. | Extended the wake-up concept to continuous saving — mempalace only loads, datawatch also auto-saves during sessions. |
| Session awareness & broadcast | — | v2.0.1 | Memory config for `session_awareness` and `session_broadcast` to share context across concurrent sessions. | Multi-session coordination inspired by mempalace's layered context — but mempalace doesn't manage sessions. |
| Hybrid content encryption | BL68 | v1.6.0 | XChaCha20-Poly1305 on content/summary fields, embeddings stay searchable. | Mempalace stores in plaintext ChromaDB. Datawatch needed at-rest encryption for verbatim content containing sensitive code/conversations. |
| Key rotation & management | BL70 | v1.6.0 | Generate, rotate, backup, fingerprint, import/export encryption keys. | Required by BL68 — mempalace has no encryption lifecycle. |
| PostgreSQL+pgvector backend | BL43 | v2.0.2 | Full memory store on PostgreSQL with vector search, spatial search, KG tables. | Mempalace uses ChromaDB only. Enterprise/team deployments need shared Postgres infrastructure. |
| Multi-channel memory commands | — | v1.3.0+ | `remember/recall/forget/learnings/kg` from Signal, Telegram, Matrix, webhooks, API, CLI, chat UI. | Mempalace is MCP-only (19 tools). Datawatch exposes memory through every communication channel. |
| Deduplication | BL63 | v2.0.0 | Content-hash dedup prevents redundant storage. | Multi-channel auto-save creates duplicates — mempalace's single-source input doesn't have this problem. |
| Embedding cache | BL50 | v2.0.0 | Cache embeddings to reduce API calls. | Mempalace uses ChromaDB's built-in embeddings. Datawatch calls external Ollama/OpenAI — caching needed. |
| Retention policies | BL47 | v2.0.0 | Per-role TTLs for automatic memory expiration. | Mempalace stores everything forever. Datawatch needs lifecycle management for enterprise compliance. |
| Memory export/import | BL46 | v2.0.0 | JSON serialization for backup and migration. | Portability between SQLite and PostgreSQL — mempalace is ChromaDB-only. |
| Write-ahead log | BL62 | v2.0.0 | Audit trail for all memory writes. | Multi-channel writes need atomic auditing — not needed for mempalace's single MCP interface. |
| Batch reindexing | BL51 | v2.0.0 | `memories reindex` after embedding model change. | Mempalace uses ChromaDB's fixed embedder. Datawatch supports swapping Ollama models. |
| Learning quality scoring | BL53 | v2.0.0 | Score and rank task learnings by relevance. | Unique to datawatch's learnings system — mempalace classifies but doesn't score. |

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
