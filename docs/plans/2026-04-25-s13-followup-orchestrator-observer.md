# S13 follow-up — orchestrator graph `observer_summary`

**Status:** Design — implementation pending
**Filed:** 2026-04-25
**Predecessor:** [S13 v4.7.0 — agent observer peers](2026-04-25-s13-agent-recursive-drilldowns.md)
**Target release:** v4.7.x patch (small) or v4.8.0 alongside S14a
**GitHub:** [#20](https://github.com/dmz006/datawatch/issues/20)

## What

The S13 design called for `/api/orchestrator/graphs/{id}` to gain a
per-node `observer_summary` field (live cpu / rss / envelope_count
joined from the peer registry). It got deferred from the v4.7.0 cut
because the BL117 orchestrator's `Node` struct doesn't carry an
`agent_id` — there's no key to join against.

This doc covers the smallest change that closes the gap.

## Scope

**One field** added to `internal/orchestrator.Node`:

```go
// AgentID — populated by the executor when it spawns an F10 worker
// to run this node's PRD. Used by /api/orchestrator/graphs/{id} to
// join against the observer peer registry for live envelope summary.
// Empty for guardrail nodes (they run inline in the parent).
AgentID string `json:"agent_id,omitempty"`
```

…and one wiring change in the executor: when calling
`Manager.Spawn` for a PRD, capture the returned agent ID into the
node.

## Deliverables

### 1. orchestrator.Node.AgentID

Single struct field. JSON-omittable so existing snapshots stay
compatible.

### 2. Executor wires it

`internal/orchestrator/executor.go` (or wherever `SpawnFn` is
invoked per node) — capture the agent ID from `SpawnFn`'s return
into `node.AgentID` and persist via the existing graph store
update path.

### 3. Server-side join

`internal/server/orchestrator.go` → `handleOrchestratorGraphs(id)`:
when the graph payload is built, walk `nodes`, look up each
`AgentID` in the peer registry, attach a fresh `observer_summary`:

```json
{
  "cpu_pct": 23.4,
  "rss_mb": 412,
  "envelope_count": 3,
  "last_push_at": "2026-04-25T..."
}
```

Cheap — the peer registry is in-memory; one map lookup + one
extraction per node.

### 4. PWA orchestrator graph view

If/when the PWA grows an orchestrator graph view (currently CLI /
MCP only). The data is already there for any client to consume.

### 5. Mobile parity

Already filed: [datawatch-app#7](https://github.com/dmz006/datawatch-app/issues/7).

## Tests

- `internal/orchestrator/executor_test.go` — new fake `SpawnFn`
  returns a fixed agent ID; assert it lands on `node.AgentID`.
- `internal/server/orchestrator_test.go` — fake peer registry +
  graph with one node carrying `AgentID`; assert response includes
  `observer_summary`.

## Acceptance criteria

- [ ] `GET /api/orchestrator/graphs/{id}` includes
      `nodes[*].observer_summary` for any node whose `agent_id` matches
      a live peer.
- [ ] Old graphs (no `agent_id` populated yet) render unchanged —
      `observer_summary` is omitted, no breakage.
- [ ] Mobile + PWA can render the new field without a server-version
      bump (additive).

## Why now (or why later)

**Now** if it ships in a v4.7.x patch alongside S14a — operators
running the orchestrator get observer attribution per fan-out.

**Later** if S14a (cross-cluster federation) lands first — then the
observer_summary join also needs to walk the federated tree, which
is a slightly larger change.

Either ordering works. The struct field + executor wire are the same
either way.
