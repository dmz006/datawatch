# Memory + KG recall flow (BL23 episodic + BL57 KG, BL101 namespaces)

How a single call to `memory_recall` (or `GET /api/memory/search`)
fans out across the embedder, the vector store, the knowledge graph,
and the namespace expansion that lets workers see peer-profile data.

```
   ┌─── caller (MCP tool / REST / PWA / Signal) ──────────────────────┐
   │                                                                  │
   │  memory_recall(q, profile=auth-team)                             │
   │      │                                                           │
   │      ▼                                                           │
   │  GET /api/memory/search?q=…&profile=auth-team                    │
   │                                                                  │
   └────────────────────────────┬─────────────────────────────────────┘
                                │ HTTPS bearer
                                ▼
   ┌─── parent daemon ────────────────────────────────────────────────┐
   │                                                                  │
   │  503 ──→ if memoryAPI == nil (memory not enabled)                │
   │                                                                  │
   │  BL101 — namespace expansion                                     │
   │      profile name ─→ profile.ProjectStore.EffectiveNamespacesFor │
   │                       (own + mutual-opt-in peers)                │
   │      agent_id    ─→ agentMgr.Get(id).ProjectProfile  (worker)    │
   │      explicit    ─→ ?namespace= overrides                        │
   │                                                                  │
   │      effectiveNamespaces []string                                │
   │      │                                                           │
   │      ▼                                                           │
   │  internal/memory.SearchInNamespaces(q, ns, limit)                │
   │      │                                                           │
   │      ├─→ embedder.Embed(q)                                       │
   │      │   (ollama nomic-embed-text by default; cached per-q)      │
   │      │                                                           │
   │      ├─→ pgvector cosine search across ns set                    │
   │      │   (postgres backend; sqlite when single-node)             │
   │      │                                                           │
   │      └─→ optional KG expansion                                   │
   │          (top-k results' subjects ─→ kg.Query for related        │
   │           triples; merged into the result envelope)              │
   │                                                                  │
   │  ◀── 200 OK [{id, content, score, namespace, kind, ts}, …]       │
   │                                                                  │
   └──────────────────────────────────────────────────────────────────┘
```

## Write path (for context)

```
   memory_remember(content, kind, namespace, tags)
       │
       ▼
   POST /api/memory/save
       │
       ▼
   internal/memory.Save:
       • write WAL row (BL23 audit) — survives crash before commit
       • embedder.Embed(content)
       • insert into pgvector + tags index
       • kg.AddFromContent(content) — optional NLP extract
       • emit notify on memory_changed channel
```

## Failure modes

| Symptom | Likely cause | Fix |
|---|---|---|
| 503 "memory not enabled" | `cfg.Memory.Enabled = false` | flip in PWA Settings → Memory |
| Empty results despite known data | Wrong namespace or profile | check `EffectiveNamespacesFor` output via `/api/projects/<name>` |
| Slow first call | Embedder cold start | ollama keeps the model warm after first hit; consider preloading |
| 502 from embedder | Ollama down or model missing | `ollama pull nomic-embed-text` |
| Stale results | Index out of sync | `POST /api/memory/reindex` (per-namespace optional) |

## Related

- Endpoint specs: [`docs/api/openapi.yaml` → `/api/memory/*`](../api/openapi.yaml)
- MCP tools: see `api-mcp-mapping.md` — every endpoint has a tool
- Operator doc: [`docs/memory.md`](../memory.md)
- Implementation: `internal/memory/`
