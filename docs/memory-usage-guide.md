# Memory Usage Guide — Practical Examples

This guide shows how to use datawatch's episodic memory system in real
development workflows. All examples are tested and verified.

## Quick Start

### Enable memory
```bash
# Via comm channel (Signal/Telegram/Slack)
configure memory.enabled=true

# Via web UI
Settings → LLM → Episodic Memory → toggle on

# Via API
curl -X PUT http://localhost:8080/api/config \
  -H "Content-Type: application/json" \
  -d '{"key":"memory.enabled","value":true}'
```

### Test it works
```bash
# Web UI: Settings → LLM → Episodic Memory → click "Test" button
# API:
curl http://localhost:8080/api/memory/test
# Expected: {"success":true,"embedder":"ollama","model":"nomic-embed-text","dimensions":768}
```

---

## Saving Memories

### Manual notes — things you want to remember
```
remember: the CI pipeline requires Go 1.24 and golangci-lint must pass
remember: production deploys must go through staging first
remember: the auth module was refactored to use JWT in April 2026
remember: never use sync.Map for the session store — caused race conditions
```

**Tested result:**
```
[myserver] Saved memory #4
```

### Auto-save on session completion
When `memory.auto_save` is true (default), every completed session automatically
saves its task + summary + output chunks to memory. No action needed.

### Claude Code hooks — continuous saving
When `memory.auto_hooks` is true (default), every Claude Code session gets
save hooks installed that capture conversation context every 15 messages.

---

## Searching Memories

### Semantic recall — find by meaning, not keywords
```
recall: how to deploy to production
```

**Tested result:**
```
[myserver] Recall results:
  #32 [59%] manual: datawatch release discipline: version in main.go + api.go,
    gosec scan with .gosec-exclude, dep audit 72h rule, cross-compile 5 platforms
  #27 [47%] manual: datawatch is a Go daemon that bridges messaging platforms...
```

The `[59%]` is the similarity score — higher means more relevant.

### Cross-session research — search everything
```
research: PostgreSQL configuration
```

**Tested result:**
```
[myserver] Research: PostgreSQL configuration
Memories:
  [49%] manual: datawatch v2.1.2 has auto-merge guardrails...
  [46%] session: Task: Summary: Prompt: Response: ...
```

Research searches memories + KG + session outputs simultaneously.

---

## Knowledge Graph

### Record relationships
```
kg add datawatch uses PostgreSQL
kg add Alice works_on datawatch
kg add datawatch has_feature episodic_memory
```

**Tested result:**
```
[myserver] Added triple #4: datawatch uses PostgreSQL
```

### Query an entity
```
kg query datawatch
```

**Tested result:**
```
[myserver] KG: datawatch
  #4 datawatch uses PostgreSQL (from 2026-04-10)
  #5 datawatch written_in Go (from 2026-04-10)
  #6 datawatch uses Ollama (from 2026-04-10)
  #12 datawatch deployed_on ralfthewise (from 2026-04-10)
```

### View timeline
```
kg timeline datawatch
```

Shows chronological history of all relationships.

### Invalidate (end a relationship)
```
kg add Bob works_on project-alpha
# Later when Bob moves on:
kg invalidate Bob works_on project-alpha
```

The original triple is preserved with a `valid_to` date — history is never deleted.

---

## Using Memory Inside Claude Code Sessions

When memory is enabled, Claude Code sessions get memory instructions in their
CLAUDE.md/AGENT.md. Claude can use MCP tools directly:

### Claude uses recall before answering
```
Human: How do we deploy to production?

Claude: Let me check memory for deployment procedures...
[calls memory_recall with query "deployment process"]
Based on memory: "production deploys must go through staging first"
and "use kubectl apply -f deploy/"...
```

### Claude saves decisions during work
```
Claude: I've decided to use JWT tokens instead of session cookies for the auth refactor.
[calls memory_remember with "auth refactor: using JWT tokens instead of session cookies — better for microservices, stateless"]
```

### Claude researches across sessions
```
Human: What did we do about the race condition last week?

Claude: Let me search across all sessions...
[calls research_sessions with query "race condition"]
Found in session [a3f2]: "Fixed race condition in session store by switching from sync.Map to sync.RWMutex..."
```

---

## Using Memory in the Rich Chat UI

Any session with `output_mode: chat` (OpenWebUI by default, configurable for Ollama
and others) gets the rich chat interface with built-in memory features:

### Memory command quick bar
At the bottom of the chat area, quick buttons provide one-click access:
- **memories** — list recent memories
- **recall** — pre-fills the input with `recall: ` for semantic search
- **kg query** — pre-fills for knowledge graph entity lookup
- **research** — pre-fills for cross-session deep search

### Hover actions on messages
Hover over any assistant message to reveal:
- **Copy** — copies the message text to clipboard
- **Remember** — saves the assistant's response directly to memory

### Type memory commands directly
```
You: remember: this project uses React 18 with TypeScript
System: [myserver] Saved memory #15

You: recall: what framework are we using
System: [myserver] Recall results:
  #15 [72%] manual: this project uses React 18 with TypeScript

You: kg add frontend uses React
System: [myserver] Added triple #1: frontend uses React

You: research: authentication patterns
System: [myserver] Research: authentication patterns
  Memories: [52%] manual: auth module refactored to use JWT...
```

### Enable chat UI for other backends
```yaml
ollama:
  output_mode: chat    # enables rich chat for Ollama sessions
```

---

## Memory in the Web UI

### Monitor tab
- **Episodic Memory card**: shows enabled/disabled, backend, embedder, encryption status,
  counts (total, manual, session, learning, chunk), DB size
- **Memory Browser**: search, list, filter by role/date, delete, export

### Settings → LLM tab
- Enable/disable memory
- Choose backend (SQLite/PostgreSQL)
- Configure embedder (Ollama/OpenAI)
- Set model, top-K, retention, auto-save, auto-hooks
- Test button for Ollama connectivity
- Session awareness and broadcast toggles

---

## Memory via API

### Full API reference

| Endpoint | Method | Example |
|----------|--------|---------|
| `/api/memory/stats` | GET | `curl http://localhost:8080/api/memory/stats` |
| `/api/memory/list?n=10` | GET | List recent memories |
| `/api/memory/list?role=learning` | GET | Filter by role |
| `/api/memory/search?q=deploy` | GET | Semantic search |
| `/api/memory/delete` | POST | `{"id": 42}` |
| `/api/memory/export` | GET | Download JSON backup |
| `/api/memory/import` | POST | Upload JSON backup |
| `/api/memory/test` | GET | Test Ollama connectivity |
| `/api/memory/wal?n=20` | GET | View write-ahead log |
| `/api/memory/kg/query?entity=Alice` | GET | KG entity query |
| `/api/memory/kg/add` | POST | `{"subject":"A","predicate":"uses","object":"B"}` |
| `/api/memory/kg/timeline?entity=Alice` | GET | Entity timeline |
| `/api/memory/kg/stats` | GET | KG statistics |

---

## Memory via MCP Tools

Available from Claude Code, Cursor, VS Code, or any MCP client:

| Tool | Parameters | Description |
|------|-----------|-------------|
| `memory_recall` | `query` | Semantic search |
| `memory_remember` | `text`, `project_dir` | Store memory |
| `memory_list` | `project_dir`, `n` | List recent |
| `memory_forget` | `id` | Delete by ID |
| `memory_stats` | — | System statistics |
| `memory_reindex` | — | Re-embed after model change |
| `research_sessions` | `query`, `max_results` | Cross-session search |
| `copy_response` | `session_id` | Last LLM response |
| `get_prompt` | `session_id` | Last user prompt |
| `kg_query` | `entity`, `as_of` | Entity relationships |
| `kg_add` | `subject`, `predicate`, `object` | Add triple |
| `kg_invalidate` | `subject`, `predicate`, `object` | End relationship |
| `kg_timeline` | `entity` | Chronological history |
| `kg_stats` | — | KG statistics |

---

## PostgreSQL Backend

### Switch from SQLite to PostgreSQL
```bash
# 1. Export from SQLite
curl http://localhost:8080/api/memory/export > backup.json

# 2. Configure PostgreSQL
configure memory.backend=postgres
configure memory.postgres_url=postgres://datawatch:datawatch@127.0.0.1/datawatch

# 3. Restart daemon
datawatch restart

# 4. Import into PostgreSQL
curl -X POST http://localhost:8080/api/memory/import -d @backup.json
```

### Verified test against PostgreSQL 17 + pgvector:
```
=== Memories in PostgreSQL ===
 id |  role   | content
----+---------+--------
 27 | manual  | datawatch is a Go daemon...
 28 | manual  | datawatch v2.1.2 architecture: 205 tests...
 34 | manual  | PostgreSQL backend config...
(10 rows)

=== KG Triples ===
 id | subject   | predicate   | object
----+-----------+-------------+---------
  4 | datawatch | uses        | PostgreSQL
  5 | datawatch | written_in  | Go
  6 | datawatch | uses        | Ollama
 12 | datawatch | deployed_on | ralfthewise
(9 rows)
```

---

## Encryption

Memory content is automatically encrypted when using `--secure` mode:

```bash
export DATAWATCH_SECURE_PASSWORD="your-password"
datawatch --secure start
```

Works identically on both SQLite and PostgreSQL. Content and summary fields
encrypted with XChaCha20-Poly1305. Embeddings remain searchable.

Check status: `curl http://localhost:8080/api/memory/stats` → `"encrypted": true`
