# How-to: Cross-agent memory

Use the spatial memory structures (wings, rooms, halls,
closets/drawers) to share context between agents on the same host
and across federated peers.

## Base requirements

- A datawatch daemon with memory enabled (default on).
- An embedder configured (`memory.embedder`). Default:
  `ollama:nomic-embed-text` if you have ollama running locally.
- For cross-namespace sharing: project profiles with mutual
  `memory.shared_with` lists.

## Setup

```bash
# Confirm memory is reachable.
datawatch memory stats
#  → namespaces: 3, wings: 12, total memories: 421, …

# Pick or create a project namespace.
datawatch config set session.default_project_dir /home/me/work/auth-service
#  (the namespace "project-auth-service" is auto-derived from the dir name)
```

## Walkthrough — spawn two agents, share decisions through a tunnel room

Goal: an `auth-redesign` agent makes architectural decisions; a
`migration-worker` agent reads those decisions before running its
SQL migration.

### 1. Agent A writes decisions to its own diary

When `auth-redesign` runs, it writes decisions into its
`agent-<id>` wing under `room=decisions`, `hall=facts`:

```bash
# From inside the agent (or via REST loopback):
datawatch memory save \
  "Chose row-level security over policy-table for tenant isolation. \
   Reasoning: simpler audit trail; rejected because of cross-tenant \
   joins becoming a perf cliff." \
  --wing agent-$AGENT_ID --room decisions --hall facts
```

### 2. Agent A also writes a closet+drawer for the long-form rationale

```bash
# Long verbatim → drawer; short summary → closet that points at it.
# Done via /api/memory/save with closet_drawer=true (or memory_remember MCP).
datawatch memory save \
  --closet-summary  "Tenant isolation = RLS not policy-table" \
  --drawer-verbatim "$(cat /tmp/auth-decision-doc.md)" \
  --room decisions --hall facts
```

### 3. Agent B reads the shared room

The `migration-worker` profile listed `auth-redesign` in
`memory.shared_with`, and `auth-redesign` reciprocates. So
`migration-worker` can recall from `auth-redesign`'s diary:

```bash
datawatch memory recall "tenant isolation policy" \
  --wing agent-auth-redesign-* --room decisions
#  → 2 hits:
#    1. (closet) "Tenant isolation = RLS not policy-table" → drawer #4137
#    2. (fact)   "Chose row-level security over policy-table …"
```

Drilling into the drawer for the full text:

```bash
datawatch memory get 4137
#  → full multi-paragraph decision document
```

### 4. Find tunnels — rooms shared across wings

When several agents have written into the same `room` name, that
room is a *tunnel*:

```bash
datawatch memory tunnels
#  → "decisions"   → [agent-auth-redesign, agent-migration-worker]
#    "incidents"   → [agent-on-call, project-prod]
#    "preferences" → [project-datawatch, project-mcp-bridge]
```

Useful for "where did this topic come up across all my agents?"

### 5. Use the agent diary at session start

Every spawned worker auto-injects layer L3 of its parent agent's
recent diary on first prompt. Operators don't have to do anything;
the wake-up stack reads
`agent-<spawning-id>` wing → last N entries by `created_at desc`.

Disable per-session if you want a clean context:

```bash
datawatch session start --task "regress test only" --no-wake-up
```

Or globally:

```bash
datawatch config set memory.wake_up_stack false
```

The Settings → Monitor card surfaces episodic-memory health
(backend, embedder, encryption mode, total / manual / session /
learning row counts, DB size on disk):

![Settings → Monitor — Episodic Memory panel](screenshots/settings-monitor.png)

## Federated peers (cross-host memory)

When the daemon is configured as a peer of a root primary
(`observer.federation.parent_url`), memory writes stay local — but
operators can opt in to selective replication via the
`memory.federation` config key (same per-namespace allow-list as
`shared_with`, just across hosts). Today's federation is observer-
only; cross-host memory replication is on the roadmap (see
[BL189](../plans/README.md) and the related cluster-federation plan).

## See also

- [`docs/api/memory.md`](../api/memory.md) — full REST + MCP reference
- [`docs/memory.md`](../memory.md) — architecture
- [`docs/flow/memory-recall-flow.md`](../flow/memory-recall-flow.md) — recall pipeline
- [How-to: Container workers](container-workers.md) — when each agent gets its own diary
- [How-to: Autonomous planning](autonomous-planning.md) — PRDs and stories that share decisions through a tunnel
