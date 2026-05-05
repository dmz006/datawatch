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
| milla-jovovich / mempalace | [github.com/milla-jovovich/mempalace](https://github.com/milla-jovovich/mempalace) | Wake-up stack, context layering patterns. **v5.26.70 quick-win bundle** — `room_detector.go` (room_detector_local.py), memory pinning, `conversation_window.go` stitching (convo_miner.py), `query_sanitizer.go` (query_sanitizer.py), `repair.go` self-check (repair.py). **v5.26.72 extension** — full spatial schema (floor/shelf/box from palace_graph.py), `normalize.go` (dialect.py + normalize.py), `sweeper.go` similarity-stale eviction (sweeper.py), `refine_sweep.go` periodic re-summarize (llm_refine.py), `general_extractor.go` schema-free fact extraction (general_extractor.py), `spellcheck.go` (spellcheck.py), `convo_miner.go` Slack/IRC/email parsers (convo_miner.py), `migrate.go` schema_version table (migrate.py), `corpus_origin` Source field population. |
| danielmiessler / PAI | [github.com/danielmiessler/Personal_AI_Infrastructure](https://github.com/danielmiessler/Personal_AI_Infrastructure) | **v6.0+ platform design and v6.2 Automata redesign** — Identity/Telos layer (including interview-style initialization workflow), Guided Mode (7-phase Algorithm), Skills layer, Evals framework, Council mode, ISA generalization (→ Automata type system). Full comparative analysis: `docs/plans/2026-05-02-pai-comparison-analysis.md`. Design: `docs/plans/2026-05-02-unified-ai-platform-design.md`. Automata: `docs/plans/2026-05-02-bl221-autonomous-task-redesign.md`. |

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

### From [danielmiessler/Personal_AI_Infrastructure (PAI)](https://github.com/danielmiessler/Personal_AI_Infrastructure)

**What PAI is:** A personal productivity OS built on top of Claude Code. PAI is a framework of 45+ skills, workflows, and life management tools. Core thesis: "make Claude Code a personal life operating system." Every design decision traces back to operator identity and structured thinking. PAI uses filesystem-based markdown memory (no database), Bun/TypeScript runtime, and is entirely local-first.

**PAI's architecture:** The 7-phase Algorithm (Observe → Orient → Decide → Act → Measure → Learn → Improve) is the required harness around complex work. Identity is expressed through Telos — a structured document (principal identity, north-star goals, current projects, values). Skills are named, self-contained workflows in `~/.config/pai/Skills/`. The Evals pack runs structured LLM-based quality checks. Council spawns 4–6 specialized debater agents with structured rounds.

**Comparative analysis:** `docs/plans/2026-05-02-pai-comparison-analysis.md`

#### How datawatch compares to PAI

| Aspect | PAI | datawatch | Notes |
|--------|-----|-----------|-------|
| **Primary purpose** | Personal life OS on top of Claude Code | Distributed AI session control plane | PAI gives the operator identity; datawatch gives infrastructure |
| **Language** | TypeScript/Bun | Go | Datawatch: single binary, no runtime deps |
| **Memory** | Markdown filesystem | SQLite/pgvector + spatial mempalace + temporal KG | Datawatch strictly more capable for structured retrieval |
| **Multi-machine** | None | Proxy mode, cross-cluster federation, peer registry | Datawatch is distributed; PAI is local-only |
| **Messaging** | None | 12 bidirectional backends (Signal/Telegram/Discord/Slack/Matrix/etc.) | PAI has no messaging layer |
| **Container execution** | None | Docker/K8s spawn, PQC bootstrap, distroless images | PAI runs in Claude Code only |
| **Observability** | Pulse dashboard | Prometheus, eBPF, audit trail, CEF | Datawatch has production-grade observability |
| **Extensibility** | 45 public packs (install manually) | Plugin framework (hot-reload, manifest-driven, sandboxed) | Different models; PAI pack → datawatch skill |
| **Configuration** | Filesystem only | YAML + REST + MCP + CLI + Comm + PWA + Mobile (7 surfaces) | Datawatch enforces full config parity |
| **Mobile** | None | Full Android + Wear OS app with WebSocket streaming | PAI has no mobile client |

#### Directly included from PAI (adapted for datawatch)

These concepts from PAI were analyzed, adopted in principle, and implemented natively in the Go binary to meet datawatch's 7-surface parity requirement. PAI's TypeScript runtime was not adopted; the concepts were re-implemented.

| Feature | BL# | Version | What PAI had | How datawatch adapted it |
|---------|-----|---------|--------------|--------------------------|
| Skills layer | **BL255** | ✅ **v6.7.0** | PAI packs — self-contained skill manifests with `INSTALL.md`, `SKILL.md`, `VERIFY.md`, executables | `~/.datawatch/skills/<name>/skill.yaml` — named, MCP-registered, invokable from all 7 surfaces. Session type `skill`. Hot-reload via fsnotify. Different from hooks (plugins react; skills are intentionally invoked). Shipped via Skill Registries with PAI default registry, connect→browse→sync flow, JSON-on-disk store. |
| Identity layer (Telos) | **BL257** | ⏳ **Open — retro-filed 2026-05-05** | Telos: principal identity, north-star goals, current projects, values — injected into every LLM interaction | `~/.datawatch/identity.yaml` — auto-injects into wake-up L0 layer; `GET/PUT /api/identity`; MCP `get_identity`/`set_identity`; personal memory namespace for life context. **Originally claimed `v6.0.3 (target)` here but never shipped — sub-feature of BL221 silently dropped at v6.2.0 closure. Retro-filed as BL257.** |
| Identity interview workflow | **BL257** | ⏳ **Open — retro-filed 2026-05-05** | PAI's Telos initialization interview — LLM-guided Q&A that builds the identity document through structured phases; operator answers questions, LLM captures and writes the result | `interview` skill type (new manifest type: `type: interview`, `phases:`, `output_file:`, `update_mode: merge`); `interview-identity` built-in skill (phases: role → goals → values → current_focus → context_notes); Identity Automaton entry point (robot icon in PWA header → automaton lookup → create modal → runs interview as `personal` type automaton); `identity configure` command on all 7 surfaces. Configure path always goes through automaton run — GET/SET REST are convenience endpoints only. **Originally claimed `v6.2.0 (target)` here but never shipped — folded into BL257.** |
| Guided Mode (PRD subset) | BL221-native | ✅ **v6.2.0 (partial)** | PAI's 7-phase Algorithm — required harness for complex work: Observe→Orient→Decide→Act→Measure→Learn→Improve | **Renamed "Guided Mode"** — 5-phase subset (Observe→Orient→Decide→Act→Summarize) for PRD sessions only. Implemented as session template injection; gates detected by channel bridge; operator confirms at each gate via `WaitingInput` state. **Measure/Learn/Improve phases + general (non-PRD) per-session applicability not shipped — see BL258.** |
| Algorithm Mode (general 7-phase per session) | **BL258** | ⏳ **Open — retro-filed 2026-05-05** | PAI's full 7-phase Algorithm as a generic session-level mode applicable to any session, not just PRD | `algorithm_mode` flag on any session; channel bridge handles all 7 phase gates; operator confirms at each gate. Strict superset of the partial Guided Mode shipped in v6.2.0. **Originally implied by "v6.1.0 (target)" claim above — only the PRD subset shipped. Retro-filed as BL258.** |
| Evals framework | **BL259** | ⏳ **Open — retro-filed 2026-05-05** | PAI Evals pack — code graders (string_match/regex), model graders (llm_rubric), human graders; capability vs regression modes | `internal/evals/` — 4 grader types (string_match, regex_match, llm_rubric, binary_test); YAML eval definitions; integrated with session completion + orchestrator DAG; BL221 rules check and security scan use `llm_rubric` + `binary_test` graders. **Originally claimed `v6.1.1 (target)` here but never shipped — current verifier remains binary yes/no. Retro-filed as BL259.** |
| Council / multi-agent debate | **BL260** | ⏳ **Open — retro-filed 2026-05-05** | PAI Council pack — 4–6 specialized agents, DEBATE mode (3 rounds) vs QUICK mode; 4–6 well-composed agents outperform 12 generic | `CouncilOrchestrator` — N parallel `council_reviewer` sessions (one per persona), structured rounds, synthesizer session; orchestrator guardrail `type: council`. **Originally claimed `v6.1.2 (target)` here but never shipped. Retro-filed as BL260.** |
| ISA / generalized task types | BL221 | ✅ **v6.2.0** | PAI ISA (Ideal State Artifact) — PRD-like planning doc applicable to any task (software/research/creative/operational) | Automata `type` field — software/research/operational/personal + plugin-extensible registry; each type gets type-specific decomposition prompt + display aliases (Stories→Phases for research, etc.). The ISA concept generalized the PRD into a multi-domain planning unit. |
| Skill verification | BL255 | ✅ **v6.7.0** | PAI `VERIFY.md` per skill — post-skill check that output meets expectations | `skill.yaml` `verify:` section — optional `entry: ./verify.sh` with `pass_threshold`. BL221 scan framework extends this to per-task scanners. Shipped as part of Skill Registries. |
| BeCreative / diversity ideation | _deferred_ | ⏳ **Deferred — low value flagged 2026-05-05** | Verbalized sampling for 1.6–2.1x diversity. Multi-candidate generation before locking | Automata "explore alternatives" concept: before locking planning, generate 3 structurally different approaches for operator selection. **Originally claimed `v6.2.0 (target)` here but never shipped. Per PAI analysis "Low value" classification, not retro-filed as BL — operator confirmed deferred 2026-05-05. Will file if future need surfaces.** |
| Daemon / public profile | _frozen_ | ❄️ **Frozen 2026-05-05** | PAI Daemon pack — VitePress + Cloudflare Pages public profile aggregation with PII filter | Out of scope per PAI analysis "Don't translate" section. Operator confirmed frozen 2026-05-05. |

#### Built because PAI revealed the need

These features did not exist in PAI but were designed because the PAI analysis surfaced the gap.

| Feature | BL# | Version | Description | Why it was built |
|---------|-----|---------|-------------|------------------|
| Plugin-extensible type system | BL221 Section 3.3 | v6.2.0 | Automata types registered by plugins via manifest (`automaton_types:` key), not hardcoded | PAI's 45 public packs cover domains far beyond the 4 built-in types — a fixed 4-type taxonomy would block PAI pack → datawatch skill migrations |
| LLM-assisted fix loop | BL221 Section 8.8 | v6.2.0 | On task rejection/block → LLM fix analysis mini-session → structured proposal → operator approves → retry → verify | PAI's quality gates verify but don't diagnose failures. Datawatch needed a structured human-in-the-loop fix cycle that PAI's single-user local model makes unnecessary |
| LLM rule editor | BL221 Section 8.7 | v6.2.0 | Rules check violation → LLM proposes 2–3 AGENT.md diffs → operator approves → LLM inserts | PAI's `.pai-protected.json` is a prevention mechanism. Datawatch's post-hoc rule editor addresses the case where rules need updating based on new patterns discovered during execution |
| Secrets scanner with git history | BL221 Section 8.6 | v6.2.0 | `gitleaks` always-on for `.git` repos; scans full commit history; blocks on any finding | PAI has a protected-commit hook for new commits. Datawatch's scanner catches secrets already in history — a broader scope driven by the multi-operator deployment use case |
| Settings → Automata consolidated tab | BL221 Section 8b | v6.2.0 | All Automata config in a dedicated PWA Settings tab with 4 sections | PAI is filesystem-config only. Datawatch's 7-surface rule requires a UI surface for every config key; BL221 scope made a dedicated tab necessary for discoverability |
| Contextual multi-select batch actions | BL221 Section 4.5 | v6.2.0 | Batch actions shown only when valid for ALL selected automata simultaneously | PAI has no list UI. Datawatch's CRUD list view with batch operations needed a principled intersection logic to avoid user error |

#### PAI concepts that were researched but not adopted

| PAI Concept | Why Not Adopted | Alternative |
|-------------|----------------|-------------|
| Filesystem-as-database | PAI intentionally avoids databases for portability. Datawatch's memory system (SQLite/pgvector, temporal KG, spatial indexing) is strictly more capable for structured retrieval at scale | Datawatch continues with SQLite/pgvector |
| AAAK compression dialect | Reduces tokens 85% but regresses accuracy to 84.2% (vs 96.6% verbatim). Unacceptable quality trade-off for autonomous tasks | Plain text with spatial mempalace for retrieval efficiency |
| Bun/TypeScript runtime | PAI is Bun-native. Datawatch is Go-native. Introducing Bun would create a runtime dependency incompatible with single-binary deployment | PAI packs run via the `datawatch-pai` plugin container (isolated) |
| VitePress public profile | PAI's Daemon pack publishes an operator profile to Cloudflare Pages. Datawatch's public surface (if any) would be an operational status page, not a portfolio site | Deferred to v6.3+ as opt-in status page |
| Full life-goal integration (health, relationships, finances) | PAI integrates personal life goals into every AI interaction. Datawatch is a technical tool; this integration is out of scope for the platform itself | Accessible via `personal` session type + personal memory namespace |

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
