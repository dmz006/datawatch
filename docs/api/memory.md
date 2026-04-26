# Memory — operator reference

Vector-indexed project memory + temporal knowledge graph + a 4-layer
context wake-up stack + spatial structures (wing / room / hall /
closet / drawer) borrowed from mempalace and extended for
multi-agent fan-out.

The architectural overview lives in [`docs/memory.md`](../memory.md).
This page is the operator reference: every REST endpoint, MCP tool,
and CLI subcommand, plus a walkthrough of the spatial vocabulary.

## Surface

### REST

| Endpoint | Purpose |
|---|---|
| `POST /api/memory/save` | Save one memory. Optional `wing`/`room`/`hall` metadata + namespace + role. |
| `GET /api/memory/search` | Vector + namespace search. Optional `wing`/`room` filters. |
| `GET /api/memory/list` | Paginated list. |
| `POST /api/memory/delete` | Soft delete by ID. |
| `POST /api/memory/reindex` | Recompute embeddings for the active namespace. |
| `GET /api/memory/learnings` | Distilled learnings rolled up across sessions. |
| `GET /api/memory/research` | Deep cross-session research query. |
| `GET /api/memory/export` | Export the namespace as JSONL. |
| `POST /api/memory/import` | Import previously-exported JSONL. |
| `GET /api/memory/stats` | Counts by namespace, wing, hall. |

### MCP tools

`memory_remember`, `memory_recall`, `memory_research`,
`memory_export`, `memory_import` — see
[`docs/api-mcp-mapping.md`](../api-mcp-mapping.md) for the full
mapping table.

### CLI

```bash
datawatch memory save     "<content>" --namespace <ns> [--wing <w> --room <r> --hall <h>]
datawatch memory recall   "<query>"   --namespace <ns> [--wing <w> --room <r>]
datawatch memory list                  --namespace <ns> [--wing <w>]
datawatch memory stats
```

### Chat / messaging

`memory_remember <content>` and `memory_recall <query>` work
verbatim from any bidirectional channel (Signal, Telegram, Discord,
Slack, Matrix, Twilio).

---

## Spatial structures

Three of mempalace's six levels are the foundation; closets/drawers
add the fourth tier the agent fan-out needed.

| Level | Field | Purpose | Example |
|-------|-------|---------|---------|
| **Wing** | `wing` | Top-level partition. Per-agent diaries live in `agent-<id>` wings; project memory lives in `project-<name>` wings. | `agent-abc123`, `project-datawatch` |
| **Room** | `room` | Operator-supplied topic inside a wing. | `decisions`, `edits`, `incidents`, `auth-flow` |
| **Hall** | `hall` | Category of an entry inside a room. | `facts`, `events`, `discoveries`, `preferences`, `advice` |
| **Closet** | (synthetic) | A small summary embedding pointing at a verbatim drawer. Search hits these first; cheap. | `"User preferred Postgres pgvector over sqlite-vss"` |
| **Drawer** | (synthetic) | The verbatim original, no embedding (cheap to store, expensive to search). | the full multi-paragraph decision document |

**Why two-tier closet/drawer:** with multiple agents writing memory
in parallel, search costs scale with embedding count. Closets keep
the search index small; the drawer is fetched on demand only when
the operator (or another agent) drills into a hit.

### Tunnels

When the same `room` appears across multiple `wings`, that room is
a *tunnel* — a shared topic across agents/projects. Surfaced via
the `FindTunnels()` helper. Useful for "where did I see this
discussed across the agents I've spawned?"

---

## 4-layer wake-up stack

Auto-injected at session start (~600–900 tokens, zero operator
effort). Layers (in load order):

- **L0 — identity.** Operator's name + role + currently-active
  project. Always present.
- **L1 — critical facts.** Items tagged `critical` or
  `hall=preferences`. Always present.
- **L2 — room context.** Loaded only when the topic of the
  starting session matches a `room` in any wing.
- **L3 — recent diary.** Last N agent-diary entries from the
  spawning agent's `agent-<id>` wing.

Disable per-session with `--no-wake-up` or globally with
`memory.wake_up_stack: false`.

---

## Namespace + sharing

Per-project namespaces are the default isolation boundary. Agents
inherit their parent's namespace; cross-namespace reads require
**mutual opt-in** via the project profile's `memory.shared_with`
field. See the inline help for the field
([Settings → Profiles → Memory shared_with]) — the reciprocity
requirement means *both* profiles must list the other before reads
flow. Useful for federated builds where two project profiles need
to share a `room` of common decisions.

---

## See also

- [`docs/memory.md`](../memory.md) — concept + architecture
- [`docs/flow/memory-recall-flow.md`](../flow/memory-recall-flow.md) —
  embedder → pgvector → namespace expansion → KG enrichment
- [`docs/howto/cross-agent-memory.md`](../howto/cross-agent-memory.md) —
  end-to-end walkthrough using diaries + tunnels
- [`docs/api-mcp-mapping.md`](../api-mcp-mapping.md) — REST ↔ MCP table
