# How-to: Cross-agent memory

Use the spatial memory structures (wings, rooms, halls, shelves) +
the temporal knowledge graph to share context across sessions, agents,
and federated peers. Sessions remember what other sessions discovered
last week; agents on different hosts see the same memory pool.

## What it is

Two coupled subsystems:

- **Episodic memory** — vector-indexed text chunks. Saved with
  metadata (source session, timestamp, tags). Retrieval via semantic
  search.
- **Knowledge graph** — entity-relationship triples
  `(subject, predicate, object, validity_window)`. Used for facts
  that have shape (who reports to whom; which service depends on
  which database).

Both share the spatial schema (floor / wing / room / hall / shelf /
box) inherited from mempalace, which improves recall by ~34pp over
flat indexes.

## Base requirements

- `datawatch start` — daemon up.
- Memory backend configured:
  - **SQLite** (default, pure-Go, zero setup). Fine up to ~100k
    entries.
  - **PostgreSQL + pgvector** (recommended >100k or for multi-host
    federation). Needs Postgres 14+ + pgvector extension.
- Embedding backend:
  - **Ollama** with `nomic-embed-text` or `mxbai-embed-large` (free,
    local).
  - **OpenAI** `text-embedding-3-small` (paid, low cost).

## Setup

### SQLite + Ollama (default, free)

```sh
ollama pull nomic-embed-text

datawatch config set memory.enabled true
datawatch config set memory.backend sqlite
datawatch config set memory.encryption.enabled true       # XChaCha20-Poly1305
datawatch config set embedder.backend ollama
datawatch config set embedder.model nomic-embed-text
datawatch reload
```

### PostgreSQL + pgvector (multi-host)

```sh
# Assume Postgres reachable at $PGURL with pgvector installed.
datawatch config set memory.backend postgres
datawatch config set memory.postgres_url '${secret:PG_URL}'
datawatch reload
```

Federated peers must point at the same memory backend (or use
[`federated-observer.md`](federated-observer.md) to push summaries between separate
backends).

## Two happy paths

### 4a. Happy path — CLI

```sh
# 1. Save a fact.
datawatch memory save \
  --content "We use 'eq' as a verb for kubectl in this team" \
  --tags vocabulary,k8s \
  --room operator-context

# 2. Search.
datawatch memory search "what do we call kubectl"
#  → 0.84 │ We use 'eq' as a verb for kubectl in this team
#         │   tags: vocabulary, k8s
#         │   room: operator-context

# 3. Recall — search + return only the top match formatted for an LLM.
datawatch memory recall "kubectl vocabulary"
#  → "We use 'eq' as a verb for kubectl in this team"

# 4. Knowledge graph — add a triple.
datawatch kg add operator owns datawatch-host-kona

# 5. Query.
datawatch kg query --subject operator
#  → operator owns datawatch-host-kona
#    operator owns lab-east-cluster

# 6. List rooms (spatial schema).
datawatch memory rooms
#  → operator-context     78 entries
#    project-datawatch    412 entries
#    incident-2026-04-30  23 entries
```

### 4b. Happy path — PWA

1. Bottom nav → **Observer** → scroll to **Knowledge Graph** card.
2. The card lists recent triples; filter by subject / predicate.
3. To add a triple: **+ Add triple** → modal with subject /
   predicate / object inputs. **Save**.
4. For episodic memory: inside any session detail, type inline:
   - `remember: <fact>` — saves the surrounding context as memory.
   - `recall: <topic>` — searches + injects the top match into the
     LLM's context.
   - `kg add (subject, predicate, object)` — adds a triple.
5. The session's chat tab shows these inline commands as system
   bubbles with the result.

## Other channels

### 5a. Mobile (Compose Multiplatform)

Observer → Knowledge Graph card with the same add / filter flow.
In-session inline `remember:` / `recall:` / `kg add` work identically.

### 5b. REST

```sh
# Save.
curl -sk -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"content":"...","tags":["..."],"room":"..."}' \
  $BASE/api/memory/save

# Search.
curl -sk -H "Authorization: Bearer $TOKEN" \
  "$BASE/api/memory/search?q=kubectl%20vocabulary&limit=5"

# Recall (top match formatted).
curl -sk -H "Authorization: Bearer $TOKEN" \
  "$BASE/api/memory/recall?q=kubectl"

# KG triples.
curl -sk -H "Authorization: Bearer $TOKEN" \
  "$BASE/api/kg/triples?subject=operator"
curl -sk -X POST -H "Authorization: Bearer $TOKEN" \
  -d '{"subject":"operator","predicate":"owns","object":"datawatch-host-kona"}' \
  $BASE/api/kg/add
```

### 5c. MCP

Tools: `memory_remember`, `memory_recall`, `memory_search`,
`memory_rooms`, `kg_query`, `kg_add`, `research_sessions` (deep
cross-session search).

This is one of the highest-value MCP surfaces — every coordinator LLM
should call `memory_recall` before planning, `memory_remember` after
deciding.

### 5d. Comm channel

| Verb | Example |
|---|---|
| `remember <fact>` | Saves to operator-context room. |
| `recall <query>` | Returns the top match. |
| `kg query subject=<x>` | Returns matching triples. |

In-session inline commands (`remember:`, `recall:`, `kg add`) work in
chat sessions exactly as they do in the PWA.

### 5e. YAML

```yaml
memory:
  enabled: true
  backend: sqlite                     # or postgres
  postgres_url: ${secret:PG_URL}
  encryption:
    enabled: true                      # XChaCha20-Poly1305
    key: ${secret:MEMORY_KEY}
    rotate_after_days: 90

embedder:
  backend: ollama                      # or openai / openai_compat
  model: nomic-embed-text
  ollama_url: http://localhost:11434

kg:
  enabled: true
  validity_window_default: 90d
```

## Diagram

```
   Session A (claude-code)            Session B (ollama, different host)
        │                                       │
        │ remember: <fact>                     │ recall: <query>
        ▼                                       │
  ┌──────────────────┐                          │
  │ Episodic memory  │ ◄────────────────────────┘
  │  (sqlite or pg)  │
  └──────────┬───────┘
             │ embed via Ollama / OpenAI
             ▼
        Vector index
             │
             ▼
        Cross-session retrieval

   Knowledge graph (triples) lives alongside; queried separately.
```

## Common pitfalls

- **Embedder slow.** Ollama embedding is fast on a GPU, painfully
  slow on CPU. For >1k chunks, use OpenAI embeddings (cheap) or
  pre-warm Ollama with `ollama run nomic-embed-text`.
- **Encryption key lost.** XChaCha20-Poly1305 with key rotation is
  default; if you lose the key, encrypted entries are unrecoverable.
  Back the key up via `datawatch secrets export`.
- **Federation across mismatched backends.** Two daemons on different
  memory backends won't share entries unless you configure observer
  push to forward summaries.
- **No room organization.** Saving everything to the default room
  becomes hard to recall. Use rooms / tags consistently from day one.
- **KG triples without validity window.** Default 90d; for permanent
  facts (org chart, infrastructure topology) set
  `--validity 999999d` or omit the validity check.

## Linked references

- See also: [`federated-observer.md`](federated-observer.md) — push
  memory summaries across hosts.
- See also: [`identity-and-telos.md`](identity-and-telos.md) — L0 layer
  is operator-fixed; memory provides L1-L3.
- See also: [`secrets-manager.md`](secrets-manager.md) — encryption key
  storage.
- Architecture: `../architecture-overview.md` § Memory + KG.

## Screenshots needed (operator weekend pass)

- [ ] Observer → Knowledge Graph card with sample triples
- [ ] Add Triple modal
- [ ] In-session `remember:` round-trip showing system bubble
- [ ] In-session `recall:` returning top match
- [ ] CLI `datawatch memory rooms` output
