# Memory Backlog — Comprehensive Plan

**Date:** 2026-04-09
**Covers:** BL43–BL67 (25 items)
**Total effort:** 8–12 weeks
**Prerequisite:** BL23/BL32/BL36 (episodic memory, semantic search, learnings) — DONE in v1.3.0

---

## Current State (v1.3.1)

Implemented:
- SQLite vector store (pure Go, no cgo)
- Ollama + OpenAI embedding providers
- Cosine similarity search
- Session auto-save on completion
- `remember/recall/memories/forget/learnings` commands (all channels)
- Memory REST API (`/api/memory/*`)
- 6 MCP tools (`memory_remember/recall/list/forget/stats`, `copy_response`)
- Real-time stats in Monitor tab
- Memory Browser (search, list, delete)
- Web UI settings card for all config options
- Rich text copy output for RichSender backends

---

## Priority Tiers & Implementation Order

### Tier 1: High Impact, Low Risk (implement first)

These items have the highest value-to-effort ratio and no external dependencies.

| # | Item | Effort | Why first |
|---|------|--------|-----------|
| BL44 | Auto-retrieve on session start | 1-2 days | Eliminates cold-start — every session starts with context |
| BL48 | Web UI memory browser enhancements | 1 day | Already partially done (basic browser exists) — add filters |
| BL52 | Session output auto-index | 1-2 days | Makes `recall` actually useful for finding past work |
| BL63 | Deduplication | 2-3 hours | Prevents redundant storage, trivial to implement |
| BL62 | Write-ahead log | 3-4 hours | Audit trail for all writes, essential for trust |
| BL50 | Embedding cache | 3-4 hours | Reduces API calls, speeds up search |
| BL46 | Export/import | 3-4 hours | Backup and migration, simple JSON serialization |

### Tier 2: High Impact, Medium Effort

These are the mempalace-inspired features that dramatically improve retrieval.

| # | Item | Effort | Why important |
|---|------|--------|---------------|
| BL55 | Spatial organization (wings/rooms/halls) | 3-5 days | +34pp retrieval improvement per mempalace benchmarks |
| BL56 | 4-layer wake-up stack | 2-3 days | ~600 token auto-context on every session, zero-cost awareness |
| BL57 | Temporal knowledge graph | 3-5 days | Entity-relationship tracking with time validity |
| BL58 | Verbatim storage mode | 1 day | Higher retrieval accuracy (96.6% vs ~80% with summaries) |
| BL60 | Entity detection | 2-3 days | Auto-extract people/projects/tools for KG population |
| BL68 | Hybrid content encryption | 2-3 days | XChaCha20-Poly1305 on content, embeddings stay searchable |
| BL70 | Key rotation and management | 1 day | Generate, rotate, backup, fingerprint, import/export with key |

### Tier 3: Enterprise & Integration

These serve team deployments and external integrations.

| # | Item | Effort | Why |
|---|------|--------|-----|
| BL43 | PostgreSQL+pgvector backend | 2-3 days | Enterprise teams with existing Postgres infrastructure |
| BL45 | ChromaDB/Pinecone/Weaviate backends | 1-2 days each | Cloud-native vector DBs for scale |
| BL54 | REST API enhancements | 1 day | Partially done — expand with filters, pagination |
| BL61 | MCP server tool enhancements | 1 day | Partially done — add KG tools when BL57 implemented |

### Tier 4: Advanced Features

These build on Tier 2 foundations.

| # | Item | Effort | Dependencies |
|---|------|--------|-------------|
| BL64 | Cross-project tunnels | 1-2 days | Depends on BL55 (spatial organization) |
| BL49 | Cross-project search | 2-3 hours | Already possible via `RecallAll` — just needs better UI |
| BL51 | Batch reindexing | 3-4 hours | `memories reindex` after model change |
| BL53 | Learning quality scoring | 1 day | Depends on BL36 (learnings, done) |
| BL47 | Retention policies | 3-4 hours | Per-role TTLs, simple cron job |
| BL59 | Conversation mining | 2-3 days | Ingest Claude/ChatGPT/Slack exports |
| BL65 | Claude Code auto-save hook | 1 day | Shell hook script + instructions |
| BL66 | Pre-compact hook | 2-3 hours | Depends on BL65 |
| BL67 | Mempalace import | 1-2 days | ChromaDB → SQLite migration tool |

---

## Detailed Plans

### BL44: Auto-Retrieve on Session Start

**Goal:** When a new session starts, automatically search memory for relevant
context and inject it as a preamble in the tmux session.

**Changes:**
- `internal/memory/retriever.go`: Add `RetrieveContext(projectDir, task string, topK int) string`
  that embeds the task, searches, and formats results as a context block
- `internal/session/manager.go`: In `LaunchSession()`, after backend launch, if memory
  enabled, call `RetrieveContext` and display in tmux via send-keys
- For claude-code: inject via `--system-prompt` flag or prepend to task text
- For openwebui: add as system message in conversation history

**Config:**
```yaml
memory:
  auto_retrieve: true          # Enable auto-retrieve (default true when memory enabled)
  auto_retrieve_top_k: 3       # Number of memories to inject
  auto_retrieve_max_tokens: 500 # Max tokens for context injection
```

**Testing:**
- Unit: `RetrieveContext` returns formatted string with top-K results
- API: Create session with memory enabled, verify context appears in output
- MCP: `start_session` tool result mentions injected context count
- Comm: `new: <task>` shows "Injected N memories as context" in session start alert

**MCP integration:** `start_session` tool auto-injects memory context when enabled.
No new MCP tool needed — behavior is automatic.

---

### BL48: Web UI Memory Browser Enhancements

**Goal:** Add filtering by role, project, date range to the existing browser.
Add inline editing and bulk delete.

**Changes:**
- `app.js`: Add filter dropdowns (role: all/manual/session/learning/chunk,
  project dropdown populated from distinct project_dirs)
- `app.js`: Add date range picker (last 7d/30d/90d/all)
- `app.js`: Add "Select all" + "Delete selected" for bulk operations
- `/api/memory/list`: Add `role`, `since`, `project` query parameters

**Testing:**
- Web: Verify filters reduce results correctly
- API: `GET /api/memory/list?role=learning&since=2026-04-01` returns filtered results

---

### BL52: Session Output Auto-Index

**Goal:** On session completion, chunk the session output and embed each chunk
for granular semantic search.

**Changes:**
- `cmd/datawatch/main.go`: In `SetOnSessionEnd` callback, read output.log,
  run through `ChunkLines()`, call `SaveOutputChunks()`
- `internal/memory/retriever.go`: `SaveOutputChunks` already exists — wire it
- Config: `memory.auto_index_output: true` (default true)

**Testing:**
- Unit: `ChunkLines` produces expected chunks from sample output
- API: Complete a session, verify chunks appear in `GET /api/memory/list?role=output_chunk`
- Comm: `recall: <specific term from past output>` finds the chunk

**MCP integration:** No new tool — chunks are searchable via existing `memory_recall`.

---

### BL63: Deduplication

**Goal:** Before saving any memory, check if identical or near-identical content
already exists. Prevent duplicates.

**Changes:**
- `internal/memory/store.go`: Add `FindDuplicate(projectDir, content string) *Memory`
  using content hash (SHA-256 of normalized text)
- Add `content_hash TEXT` column to schema, index it
- `retriever.go`: Check `FindDuplicate` before `Save`, skip if exists
- Return existing ID instead of creating new row

**Testing:**
- Unit: `Remember("same text")` twice returns same ID
- Comm: `remember: X` twice, `memories` shows only one entry

---

### BL62: Write-Ahead Log

**Goal:** JSONL audit trail for all memory write operations.

**Changes:**
- `internal/memory/wal.go`: `WriteAheadLog` struct with `Log(operation, params, result)`
- Appends to `{data_dir}/memory-wal.jsonl`
- Every `Save`, `Delete` call logs before executing
- `memories wal [n]` command to view recent writes
- `/api/memory/wal` endpoint

**Testing:**
- Unit: After `Save`, WAL file contains the entry
- Comm: `memories wal` shows recent operations with timestamps

**MCP integration:** Add `memory_wal` tool to view audit log.

---

### BL50: Embedding Cache

**Goal:** LRU cache for embeddings to avoid re-computing identical vectors.

**Changes:**
- `internal/memory/embeddings.go`: Wrap embedders with `CachedEmbedder`
  using `sync.Map` or `lru.Cache` (key: SHA-256 of text, value: []float32)
- Config: `memory.embedding_cache_size: 1000` (default)
- Cache hit rate tracked in memory stats

**Testing:**
- Unit: Same text embedded twice, second call returns cached (no HTTP call)
- Stats: `memory_stats` shows cache hit rate

---

### BL46: Export/Import

**Goal:** `memories export` produces JSON dump, `memories import` restores.

**Changes:**
- `internal/memory/store.go`: `Export(w io.Writer)` writes all memories as JSON array
- `internal/memory/store.go`: `Import(r io.Reader)` reads JSON and inserts
- Router: `memories export` and `memories import` commands
- `/api/memory/export` GET (download JSON) and `/api/memory/import` POST (upload JSON)
- CLI: `datawatch memory export > backup.json`, `datawatch memory import < backup.json`

**Testing:**
- Unit: Export → Import roundtrip preserves all fields
- API: GET export, POST import, verify count matches
- Comm: `memories export` returns JSON or download link

**MCP integration:** `memory_export` and `memory_import` tools.

---

### BL55: Spatial Organization (Wings/Rooms/Halls)

**Goal:** Hierarchical palace structure with metadata-filtered search.

**Recommendation:** This is the single highest-impact improvement. Mempalace shows
+34 percentage point retrieval improvement with metadata filtering.

**Changes:**
- Schema: Add `wing TEXT`, `room TEXT`, `hall TEXT` columns to memories table
- `internal/memory/classifier.go`: Auto-classify memories into wings (project name)
  and rooms (topic detection from content)
- `store.go`: `SearchFiltered(wing, room, queryVec, topK)` — filter before cosine
- `retriever.go`: Auto-set wing from project_dir, detect room from content
- Router: `recall: @project room:auth <query>` syntax for filtered search
- Web: Room/wing filter dropdowns in browser

**Halls (standardized types):**
- `facts` — locked decisions
- `events` — sessions and milestones
- `discoveries` — breakthroughs
- `preferences` — habits and opinions
- `advice` — recommendations

**Testing:**
- Unit: Filtered search returns only matching wing/room memories
- Benchmark: Compare retrieval accuracy with/without filtering (measure R@5)
- Comm: `recall: @myproject room:auth login bug` finds auth-related memories
- MCP: `memory_recall` tool gets `wing` and `room` optional params

**MCP integration:** Add `wing` and `room` params to `memory_recall`, `memory_list`.
Add `memory_list_rooms` tool to show room taxonomy.

---

### BL56: 4-Layer Wake-Up Stack

**Goal:** Auto-load ~600 tokens of context on every session start.

**Changes:**
- `internal/memory/layers.go`:
  - L0: Identity text from `{data_dir}/identity.txt` (~100 tokens, user-written)
  - L1: Auto-generated from top-K most important memories (~500 tokens)
  - L2: Topic-triggered, loaded when room is detected (variable)
  - L3: Deep search (existing `recall` functionality)
- `retriever.go`: `WakeUpContext() string` returns L0+L1 concatenated
- Session launch: inject wake-up context automatically

**Depends on:** BL44 (auto-retrieve), BL55 (spatial organization for L2 rooms)

**Testing:**
- Unit: `WakeUpContext()` returns identity + critical facts within token budget
- API: New session output shows injected context
- MCP: `memory_stats` shows wake-up token count

---

### BL57: Temporal Knowledge Graph

**Goal:** Entity-relationship triples with time validity windows.

**Changes:**
- `internal/memory/knowledge_graph.go`:
  - SQLite tables: `entities (id, name, type)`, `triples (subject, predicate, object, valid_from, valid_to)`
  - `AddTriple(entity1, relation, entity2, validFrom)` — time-bounded facts
  - `Invalidate(entity1, relation, entity2, ended)` — end validity
  - `QueryEntity(name, asOf)` — point-in-time entity query
  - `Timeline(entity)` — chronological story
- Router: `kg query <entity>`, `kg add <s> <p> <o>`, `kg timeline <entity>`
- API: `/api/memory/kg/query`, `/api/memory/kg/add`, `/api/memory/kg/timeline`

**Testing:**
- Unit: Add triple, query, invalidate, verify time filtering
- Comm: `kg add Max works_on datawatch`, `kg query Max` returns relationship
- MCP: `kg_query`, `kg_add`, `kg_invalidate`, `kg_timeline` tools

**MCP integration:** 4 new MCP tools for KG operations.

---

### BL43: PostgreSQL+pgvector Backend

**Goal:** Enterprise-grade vector search for team deployments.

**Changes:**
- `internal/memory/pg_store.go`: Implement `Store` interface using `pgx` driver
  with pgvector extension for native vector similarity
- `internal/memory/store.go`: Add `Backend` interface, refactor SQLite store to implement it
- Config: `memory.backend: postgres`, `memory.postgres_url: postgres://...`
- Schema: `CREATE EXTENSION vector; CREATE TABLE memories (... embedding vector(768))`
- Search: `ORDER BY embedding <=> $1 LIMIT $2` (native pgvector operator)

**Testing:**
- Unit: Mock pgvector (or use testcontainers with real Postgres)
- API: All `/api/memory/*` endpoints work identically with Postgres backend
- MCP: All memory tools work identically

---

### BL58: Verbatim Storage Mode

**Goal:** Store raw session exchanges without summarization.

**Changes:**
- Config: `memory.storage_mode: verbatim` (default: `summary`)
- `retriever.go`: In verbatim mode, save full prompt+response text instead of summary
- Chunking: Use `ChunkLines` to split large exchanges into searchable chunks

**Testing:**
- Unit: Verbatim mode stores full text, summary mode stores summary
- Recall: Search finds exact phrases from verbatim content

---

### BL65 + BL66: Claude Code Hooks

**Goal:** Auto-save to memory every N messages and before context compaction.

**Changes:**
- `hooks/mempal_save_hook.sh`: Adapted for datawatch's memory system
  - Reads Claude Code transcript JSONL
  - Counts human messages
  - Every N messages, blocks and instructs Claude to save structured data
- `hooks/mempal_precompact_hook.sh`: Fires before context window shrink
- `datawatch setup hooks` command to install hooks
- Config: `memory.hook_save_interval: 15` (messages between saves)

**Testing:**
- Unit: Hook script parses sample transcript correctly
- Integration: Claude Code session with hook installed, verify memories saved
- Comm: `memories` shows auto-saved entries after hook fires

---

## MCP Integration Requirements

**Every memory feature MUST expose its functionality through MCP tools.**
This ensures IDE clients (Claude Code, Cursor, VS Code, JetBrains) and
network-connected LLMs can use all memory features.

### Current MCP tools (v1.3.1):
| Tool | Purpose |
|------|---------|
| `memory_remember` | Store a memory |
| `memory_recall` | Semantic search |
| `memory_list` | List recent |
| `memory_forget` | Delete by ID |
| `memory_stats` | Statistics |
| `copy_response` | Last LLM response |

### MCP tools to add per feature:

| Feature | New MCP Tools |
|---------|--------------|
| BL48 (browser) | `memory_list` gets `role`, `since`, `project` params |
| BL55 (spatial) | `memory_list_rooms`, `memory_recall` gets `wing`/`room` params |
| BL57 (KG) | `kg_query`, `kg_add`, `kg_invalidate`, `kg_timeline`, `kg_stats` |
| BL46 (export) | `memory_export`, `memory_import` |
| BL51 (reindex) | `memory_reindex` |
| BL62 (WAL) | `memory_wal` |

### MCP SSE for network access

All MCP tools are accessible over HTTP/SSE when `mcp.sse_enabled: true`. This
allows any LLM or automation system on the network to use memory features:

```bash
# Remote LLM queries datawatch memory
curl -X POST https://datawatch:8443/mcp \
  -H "Authorization: Bearer <token>" \
  -d '{"tool":"memory_recall","arguments":{"query":"deployment process"}}'
```

---

## Testing Strategy

### Unit Tests (per feature)

Every feature must have:
- Store-level tests (CRUD, search, filter)
- Retriever-level tests (embedding mock, search orchestration)
- Router command tests (parse + handle)
- MCP tool tests (request → response)

### Integration Tests (per feature)

- **API**: curl commands against running daemon, verify JSON responses
- **Comm channel**: `POST /api/test/message` with each new command
- **Web UI**: Verify cards render, buttons work, real-time updates
- **MCP**: Verify tool appears in `/api/mcp/docs`, test via MCP client
- **Config**: `PUT /api/config` → `GET /api/config` roundtrip

### Cross-Backend Tests (for BL43, BL45)

- Run full test suite against SQLite backend
- Run same suite against PostgreSQL+pgvector backend
- Verify identical results for search, CRUD, stats

### Benchmark Tests (for BL55)

- Create 1000 memories across 5 projects and 10 rooms
- Measure R@5 and R@10 with and without wing/room filtering
- Compare to unfiltered baseline
- Target: +20pp improvement minimum

### Regression Tests

- After each memory feature, re-run all existing memory tests
- Verify `remember`, `recall`, `memories`, `forget`, `learnings` still work
- Verify Monitor tab stats card still renders
- Verify MCP tools still respond

---

## Documentation Requirements

Each implemented feature must update:

1. `docs/memory.md` — architecture, usage, configuration
2. `docs/plans/README.md` — move from backlog to completed, update test counts
3. `CHANGELOG.md` — under `[Unreleased]` or version section
4. `internal/server/web/openapi.yaml` — new API endpoints
5. `docs/mcp.md` — new MCP tools with parameter tables and examples
6. `AGENT.md` — if new governance rules apply

---

## Recommendations

### Do first (immediate high value):
1. **BL63 (dedup)** — 2 hours, prevents data quality issues now
2. **BL62 (WAL)** — 3 hours, audit trail before more data accumulates
3. **BL50 (cache)** — 3 hours, reduces embedding API calls immediately
4. **BL44 (auto-retrieve)** — 1-2 days, the single most impactful UX improvement
5. **BL52 (output index)** — 1-2 days, makes recall actually find past work

### Do next (structural improvements):
6. **BL55 (spatial)** — 3-5 days, biggest retrieval accuracy gain
7. **BL56 (wake-up stack)** — 2-3 days, depends on BL44+BL55
8. **BL57 (KG)** — 3-5 days, enables temporal reasoning

### Do later (polish & enterprise):
9. **BL46 (export)** — 3 hours, backup/migration
10. **BL43 (postgres)** — 2-3 days, enterprise teams
11. **BL58 (verbatim)** — 1 day, higher accuracy
12. **BL65+66 (hooks)** — 1 day, continuous saving

### Skip or defer:
- **BL45 (ChromaDB/Pinecone/Weaviate)** — low demand until enterprise adoption grows
- **BL59 (conversation mining)** — nice-to-have, not critical path
- **BL67 (mempalace import)** — only relevant for users migrating from mempalace
- **BL64 (cross-project tunnels)** — depends on BL55, advanced feature
