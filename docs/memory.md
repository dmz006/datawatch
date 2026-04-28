# Episodic Memory System

> Added in v1.3.0

Datawatch's episodic memory provides vector-indexed project knowledge with
semantic search and automatic task learnings extraction. Sessions build
institutional knowledge over time — every completed task, manual note, and
extracted learning becomes searchable context for future work.

## Design

### Architecture

```
┌─────────────────────────────────────────────────┐
│                  Datawatch Daemon                │
│                                                  │
│  ┌──────────┐  ┌───────────┐  ┌──────────────┐  │
│  │ Embedder │  │ Retriever │  │   Chunker    │  │
│  │(Ollama/  │←─│ (search + │←─│ (split long  │  │
│  │ OpenAI)  │  │  save)    │  │  output)     │  │
│  └──────────┘  └─────┬─────┘  └──────────────┘  │
│                      │                           │
│  ┌───────────────────┴───────────────────────┐   │
│  │              Vector Store                  │   │
│  │  SQLite (default) or PostgreSQL+pgvector   │   │
│  └────────────────────────────────────────────┘   │
└──────────────────────────────────────────────────┘
```

### Memory Flow

```
┌─────────────────────────────────────────────────────────────┐
│                     Session Lifecycle                        │
│                                                              │
│  1. Session starts                                           │
│     └─→ (future: auto-retrieve relevant past context)        │
│                                                              │
│  2. LLM processes prompt (running state)                     │
│     └─→ Response generated                                   │
│                                                              │
│  3. running → waiting_input transition                       │
│     ├─→ Capture response (/tmp/claude/response.md or tmux)   │
│     ├─→ Store as session.LastResponse                        │
│     ├─→ Broadcast via WS (response message type)             │
│     ├─→ Update alert body with response content              │
│     └─→ If memory enabled: embed & save prompt+response      │
│                                                              │
│  4. Session completes                                        │
│     ├─→ If auto_save: embed & save session summary           │
│     └─→ If learnings_enabled: extract & save learnings       │
│                                                              │
│  5. User queries (any time, any channel)                     │
│     ├─→ recall: semantic vector search                       │
│     ├─→ memories: list recent                                │
│     ├─→ learnings: list/search learnings                     │
│     ├─→ copy: get last response                              │
│     └─→ remember/forget: manual CRUD                         │
└─────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────┐
│                    Embedding Pipeline                        │
│                                                              │
│  Text ──→ Embedder (Ollama/OpenAI) ──→ float32[] vector     │
│                                            │                 │
│                                            ▼                 │
│                                    ┌──────────────┐          │
│                                    │ Vector Store  │          │
│                                    │ (SQLite or    │          │
│                                    │  PostgreSQL)  │          │
│                                    └──────┬───────┘          │
│                                           │                  │
│  Query ──→ Embed ──→ Cosine Similarity ──→ Top-K results    │
└─────────────────────────────────────────────────────────────┘
```

### Components

- **Embedder**: Converts text into vector embeddings. Supports Ollama (free, local,
  default) and OpenAI (better quality, paid). Uses cosine similarity for matching.

- **Store**: Persists memories with their embeddings. SQLite backend uses pure Go
  (`modernc.org/sqlite`) — no cgo, no root privileges needed. PostgreSQL+pgvector
  backend available for enterprise deployments with existing database infrastructure.

- **Retriever**: Orchestrates embedding + storage. Handles auto-save on session
  completion, semantic search, and learning extraction.

- **Chunker**: Splits long session outputs into overlapping ~500-word segments for
  granular retrieval. Ensures that relevant sections of large outputs are findable
  even when the full output is thousands of lines.

### Memory Types

| Role | Source | Description |
|------|--------|-------------|
| `session` | Auto-save on completion | Task + summary of what was accomplished |
| `manual` | `remember:` command | User-created notes and context |
| `learning` | Auto-extracted on completion | Key decisions, patterns, and gotchas |
| `output_chunk` | Auto-indexed on completion | Searchable segments of session output |

### How It Works

1. **On session completion** (when `auto_save` is enabled):
   - The task description and a summary are embedded and stored
   - Session output is chunked and each chunk is embedded for granular search
   - If `learnings_enabled`, key decisions are extracted and stored separately

2. **On session start** (future enhancement):
   - The new task text is embedded and searched against existing memories
   - Top-K relevant memories are injected as context for the LLM

3. **On demand** via commands:
   - `remember:` saves manual notes with embeddings
   - `recall:` performs semantic search across all memory types
   - `memories` lists recent entries
   - `learnings` shows extracted task learnings

### v5.26.70 Mempalace quick-win ports (Go-native)

| Feature | Module | Behaviour |
|---------|--------|-----------|
| Auto-tagging on save (QW#1) | `internal/memory/room_detector.go` | On every `SaveWithNamespace` call, derive `wing` from project basename and classify `hall` (preferences/advice/events/discoveries/facts) + `room` (auth/deploy/testing/perf/db/ui/api/docs/security) from content keywords. Operator-supplied values are preserved. |
| Memory pinning (QW#2) | `Memory.Pinned` column | Operator-marked rows always surface in L1 critical-facts even when vector-similarity rank is low. SQLite migration adds `pinned INTEGER DEFAULT 0` + index. |
| Query sanitization (QW#4) | `internal/memory/query_sanitizer.go` | Recall queries pass through `SanitizeQuery` before reaching the embedder. Strips 10 OWASP-LLM01 prompt-injection patterns (`ignore previous instructions`, `system: you are`, `jailbreak`, etc.) and replaces with `[redacted]`. Defense-in-depth — embedder treats input as opaque text, but sanitization reduces attack surface for downstream LLM consumers (memory_recall MCP tool, auto-loaded L1 facts). |

All three are pure Go ports of [Mempalace](https://github.com/dmz006/MemPalace) Python modules — no Python runtime dependency. See `docs/plans/2026-04-27-mempalace-alignment-audit.md` for the full module-by-module port plan.

## Configuration

### YAML Config

```yaml
memory:
  enabled: true                    # Enable the memory system
  backend: sqlite                  # "sqlite" (default) or "postgres"
  db_path: ""                      # SQLite path (default: ~/.datawatch/memory.db)
  postgres_url: ""                 # PostgreSQL URL (enterprise only)
  embedder: ollama                 # "ollama" (default) or "openai"
  embedder_model: nomic-embed-text # Embedding model name
  embedder_host: ""                # Embedder API URL (default: same as ollama.host)
  openai_key: ""                   # OpenAI API key (only for embedder=openai)
  dimensions: 0                    # Vector dimensions (0 = auto-detect)
  top_k: 5                         # Results per search query
  auto_save: true                  # Save summaries on session completion
  learnings_enabled: true          # Extract task learnings on completion
  retention_days: 0                # Auto-prune after N days (0 = keep forever)
```

### Access Methods

| Method | How |
|--------|-----|
| **YAML** | `memory:` section in `config.yaml` |
| **Web UI** | Settings → General → Episodic Memory |
| **CLI** | `configure memory.enabled=true` |
| **Comm channel** | `configure memory.enabled=true` from Signal/Telegram/etc. |
| **REST API** | `PUT /api/config` with key `memory.enabled` |

### Backend Options

#### SQLite (Default)

- **Requirements**: None — pure Go implementation, no cgo, no root
- **Best for**: Single-machine deployments, personal use, development
- **Storage**: Single file at `~/.datawatch/memory.db`
- **Performance**: Handles thousands of memories easily; search is in-memory cosine similarity

#### PostgreSQL + pgvector (Enterprise)

- **Requirements**: PostgreSQL 14+ (pgvector extension recommended but optional)
- **Best for**: Team deployments, shared knowledge bases, existing Postgres infrastructure
- **Storage**: Dedicated database with `memories`, `kg_entities`, `kg_triples` tables
- **Performance**: Application-side cosine similarity (pgvector native search planned)
- **Encryption**: XChaCha20-Poly1305 content encryption works identically to SQLite

##### PostgreSQL Setup

```bash
# 1. Install PostgreSQL (if not already running)
sudo apt install postgresql-17

# 2. Install pgvector extension (recommended)
sudo apt install postgresql-17-pgvector

# 3. Create database and user
sudo -u postgres psql <<SQL
CREATE USER datawatch WITH PASSWORD 'datawatch';
CREATE DATABASE datawatch OWNER datawatch;
\c datawatch
CREATE EXTENSION IF NOT EXISTS vector;
GRANT ALL ON ALL TABLES IN SCHEMA public TO datawatch;
SQL

# 4. Verify connection
PGPASSWORD=datawatch psql -U datawatch -h 127.0.0.1 -d datawatch -c "SELECT 1;"

# 5. Verify pgvector
PGPASSWORD=datawatch psql -U datawatch -h 127.0.0.1 -d datawatch -c "SELECT * FROM pg_extension WHERE extname = 'vector';"
```

##### PostgreSQL Configuration

```yaml
memory:
  enabled: true
  backend: postgres
  postgres_url: "postgres://datawatch:datawatch@127.0.0.1:5432/datawatch"
  embedder_model: nomic-embed-text
```

All access methods work with PostgreSQL:

| Method | Command |
|--------|---------|
| **Web UI** | Settings → LLM → Episodic Memory → Backend: postgres |
| **Comm channel** | `configure memory.backend=postgres` then `configure memory.postgres_url=postgres://...` |
| **API** | `PUT /api/config {"key":"memory.backend","value":"postgres"}` |

##### What pgvector provides

Without pgvector, datawatch uses application-side cosine similarity (loads all
embeddings into memory, computes similarity in Go). This works well for thousands
of memories.

With pgvector, future versions will use native `ORDER BY embedding <=> $1`
queries for better performance at scale (millions of memories).

##### Migration between backends

To migrate from SQLite to PostgreSQL:
1. Export: `GET /api/memory/export` → saves JSON
2. Switch config to `backend: postgres` with `postgres_url`
3. Restart daemon
4. Import: `POST /api/memory/import` with the JSON file

### Embedding Providers

#### Ollama (Default)

- **Cost**: Free (runs locally)
- **Model**: `nomic-embed-text` (768 dimensions, good general purpose)
- **Latency**: ~50-200ms per embedding on GPU, ~500ms on CPU
- **Requirements**: Ollama running (same host as configured in `ollama.host`)

#### OpenAI

- **Cost**: ~$0.02 per million tokens
- **Model**: `text-embedding-3-small` (1536 dimensions, higher quality)
- **Latency**: ~100-300ms per embedding (network dependent)
- **Requirements**: OpenAI API key in `memory.openai_key`

## Usage

### Response Capture (copy command)

Every time a prompt finishes (running to waiting_input), datawatch captures the
LLM's response from `/tmp/claude/response.md` (Claude Code native output) or
falls back to the tmux pane content. This captured response is:

- Stored on the session as `last_response`
- Broadcast to all WS clients via a `response` message
- Used as the alert body instead of raw screen scraping
- Saved to memory (if enabled) with the prompt for future recall
- Available via the `copy` command from any comm channel

### Commands (from any comm channel or web UI)

```
copy                            get last LLM response (most recent session)
copy <id>                       get last response for a specific session
remember: always use --no-verify for this repo's pre-commit hooks
remember: the CI pipeline requires Go 1.22+
remember: prod database credentials are in Vault, not env vars

recall: pre-commit hooks
recall: CI requirements
recall: database credentials

memories          # list last 10 memories
memories 20       # list last 20 memories

forget 42         # delete memory #42

learnings         # list recent task learnings
learnings search: race condition   # search learnings semantically
```

### REST API

```bash
# Save a memory
POST /api/test/message
{"text": "remember: always run migrations before deploy"}

# Search memories
POST /api/test/message
{"text": "recall: deployment process"}

# List memories
POST /api/test/message
{"text": "memories"}

# Configure via API
PUT /api/config
{"key": "memory.enabled", "value": true}
```

### Web UI

In **Settings → General → Episodic Memory**:
- Toggle memory on/off
- Select storage backend (SQLite/PostgreSQL)
- Choose embedding provider (Ollama/OpenAI)
- Configure model, host, top-K, retention
- All changes save immediately via the API

## Data Storage

### SQLite Schema

```sql
CREATE TABLE memories (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id TEXT DEFAULT '',
  project_dir TEXT NOT NULL,
  content TEXT NOT NULL,
  summary TEXT DEFAULT '',
  role TEXT DEFAULT 'session',
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  embedding BLOB  -- float32[] serialized as little-endian bytes
);
```

### Retention

Set `retention_days` to automatically prune old memories. Setting it to `0`
(default) keeps memories forever. Pruning runs on daemon startup.

## Files

| File | Purpose |
|------|---------|
| `internal/memory/embeddings.go` | Embedder interface, Ollama + OpenAI implementations, cosine similarity |
| `internal/memory/store.go` | SQLite vector store — CRUD, search, prune |
| `internal/memory/retriever.go` | High-level orchestration — remember, recall, auto-save |
| `internal/memory/chunker.go` | Text/line chunking for granular search |
| `internal/memory/adapter.go` | Bridge between memory package and router interface |
| `internal/config/config.go` | MemoryConfig struct and helpers |
| `internal/router/commands.go` | Command parsing for remember/recall/memories/forget/learnings |
| `internal/router/router.go` | Command handlers and MemoryRetriever interface |
| `internal/memory/api_adapter.go` | Bridge for REST API and MCP server interfaces |
| `internal/mcp/memory_tools.go` | MCP tools: memory_remember, memory_recall, memory_list, memory_forget, memory_stats, copy_response |
| `internal/stats/collector.go` | Memory metrics in SystemStats for real-time monitoring |

## MCP Tools

When memory is enabled, 6 MCP tools are registered:

| Tool | Description |
|------|-------------|
| `memory_remember` | Store a memory with vector embedding. Params: `text` (required), `project_dir` |
| `memory_recall` | Semantic search across all memories. Params: `query` (required) |
| `memory_list` | List recent memories. Params: `project_dir`, `n` (default 20) |
| `memory_forget` | Delete a memory by ID. Params: `id` (required) |
| `memory_stats` | Get memory system statistics (counts, DB size) |
| `copy_response` | Get last captured LLM response. Params: `session_id` (optional) |

## Monitoring

Memory metrics are included in the real-time stats broadcast (every 5s):

| Metric | Field | Description |
|--------|-------|-------------|
| Enabled | `memory_enabled` | Whether memory system is active |
| Backend | `memory_backend` | Storage backend (sqlite/postgres) |
| Embedder | `memory_embedder` | Embedding provider and model |
| Total | `memory_total_count` | Total memories across all projects |
| Manual | `memory_manual_count` | User-created memories |
| Session | `memory_session_count` | Auto-saved session summaries |
| Learning | `memory_learning_count` | Extracted task learnings |
| Chunks | `memory_chunk_count` | Output search chunks |
| DB Size | `memory_db_size_bytes` | Database file size |

These appear in:
- **Monitor tab** → Episodic Memory card (real-time)
- **Monitor tab** → Episodic Memory card + Memory Browser section
- **`/api/stats`** → `memory_*` fields in JSON response
- **`/api/memory/stats`** → dedicated stats endpoint
- **MCP** → `memory_stats` tool
- **Comm channel** → `stats` command includes memory info

## REST API

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/memory/stats` | GET | Memory system statistics |
| `/api/memory/list?project=&n=50` | GET | List recent memories |
| `/api/memory/search?q=<query>` | GET | Semantic search |
| `/api/memory/delete` | POST | Delete memory `{"id": N}` |
| `/api/sessions/response?id=<id>` | GET | Get last captured response |
| `/api/sessions/prompt?id=<id>` | GET | Get last user prompt |
| `/api/memory/export` | GET | Download all memories as JSON |
| `/api/memory/import` | POST | Upload JSON backup (dedup-aware) |
| `/api/memory/wal?n=50` | GET | View write-ahead log entries |
| `/api/memory/test` | GET | Test Ollama embedding connectivity |
| `/api/memory/kg/query?entity=` | GET | Query entity relationships |
| `/api/memory/kg/add` | POST | Add relationship triple |
| `/api/memory/kg/invalidate` | POST | End relationship validity |
| `/api/memory/kg/timeline?entity=` | GET | Entity timeline |
| `/api/memory/kg/stats` | GET | Knowledge graph statistics |
| `/api/ollama/stats` | GET | Remote Ollama server statistics |

## Advanced Features

### Retention Policies (BL47)

Per-role TTL pruning runs on daemon startup. Configure retention days globally
or per-role:

```yaml
memory:
  retention_days: 0               # Global (0 = forever)
  retention_session_days: 90      # Override for session summaries
  retention_chunk_days: 30        # Override for output chunks
  # Manual and learning memories kept forever by default
```

### Batch Reindex (BL51)

After changing the embedding model, re-embed all memories:

```
memories reindex
```

This runs asynchronously. Progress logged to daemon log. Available via MCP
`memory_reindex` tool.

### Conversation Mining (BL59)

Import conversation exports from other AI tools into memory:

- **Claude Code** — JSONL transcript format (auto-detected)
- **ChatGPT** — JSON export from settings
- **Generic** — JSON array of `[{"role":"user","content":"..."},...]`

Via API: `POST /api/memory/import` with JSON body.

### Claude Code Auto-Hooks (BL65/BL66)

When `memory.auto_hooks` is true (default when memory enabled), datawatch
automatically writes `.claude/settings.local.json` in the project dir before
launching Claude Code. This installs:

- **Save hook**: Every 15 messages, extracts recent exchanges and saves to memory
- **Pre-compact hook**: Before context compression, saves topic summary

Configure:
```yaml
memory:
  auto_hooks: true            # Auto-install hooks per session
  hook_save_interval: 15      # Messages between saves
```

Hooks are per-project (not global). Existing user config is preserved.

### OpenWebUI Chat Memory Commands

In OpenWebUI/Ollama chat sessions, type memory commands directly in the chat:

```
remember: always use --no-verify on this repo
recall: pre-commit hooks
memories
kg add Alice works_on myproject
```

Commands are intercepted before reaching the LLM. Results appear as system
messages in the chat bubbles.

### Ollama Server Monitoring (BL71)

When `ollama.host` is configured, the Monitor tab shows live stats:
- Models installed (count + total disk)
- Running models with VRAM usage per model
- Server availability status

Available via `/api/ollama/stats` and MCP `ollama_stats` tool.

### Rich Chat UI (BL73)

OpenWebUI/Ollama chat sessions feature:
- Markdown rendering (code blocks, bold, italic, bullet lists)
- Typing indicator animation during streaming
- Code block syntax styling with monospace font

### Session Chaining / Pipelines (F15)

Chain tasks in a dependency DAG:

```
pipeline: analyze code -> write tests -> update docs
pipeline status
pipeline cancel pipe-12345
```

Tasks execute in dependency order. Independent tasks run in parallel (up to
`max_parallel` workers, default 3). Cycle detection prevents deadlocks.

### Quality Gates (BL28)

Run tests before and after sessions to detect regressions:

- **REGRESSION**: New test failures found → blocks completion
- **PREEXISTING**: Same failures as baseline → warns but allows
- **IMPROVED**: Fewer failures → reports improvement
