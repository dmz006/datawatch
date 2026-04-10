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

- **Requirements**: PostgreSQL 14+ with pgvector extension
- **Best for**: Team deployments, shared knowledge bases, existing Postgres infrastructure
- **Storage**: Dedicated database table
- **Performance**: Native vector similarity search with indexing
- **Config**: Set `backend: postgres` and `postgres_url: postgres://user:pass@host/db`

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
