# S13 — Agent / recursive observer drill-downs

**Status:** Design — implementation pending
**Filed:** 2026-04-25
**Predecessors:**
  [BL171 / S9 v4.1.0 — observer substrate + Shape A](2026-04-22-bl171-datawatch-observer.md) ·
  [BL172 / S11 v4.4.0 — Shape B standalone daemon](2026-04-25-bl172-shape-b-standalone-daemon.md) ·
  [BL173 / S12 v4.5.0 — Shape C cluster container](2026-04-25-bl173-shape-c-cluster-container.md) ·
  [F10 ephemeral agents](2026-04-17-ephemeral-agents.md) ·
  BL117 PRD-DAG orchestrator
**Target release:** v4.7.0
**GitHub:** [#20](https://github.com/dmz006/datawatch/issues/20) (umbrella) — will spawn a follow-up issue for S13 specifically.

## Context

S9 → S12 shipped the three-shape observer (in-process / standalone /
cluster). What's missing is the *recursion*: an F10 ephemeral agent
worker — spawned by the parent into a Pod or container — has its own
`/proc` tree, its own claude / opencode / aider sub-process, and its
own session running inside it. Today the parent has no observability
into what's happening *inside* a worker beyond the bootstrap-channel
log lines the worker chooses to push.

S13 makes every worker a peer in the observer federation. The parent
sees the worker's envelopes as just another row in `/api/observer/peers`
+ the PWA Federated peers card; clicking through opens the same
process-tree drill-down modal v4.6.0 shipped (BL173 task 6).

This is the umbrella that joins:

- **F10 ephemeral agents** — already mints worker tokens at spawn,
  bootstraps via `/api/agents/bootstrap`, has a per-agent audit log.
- **BL117 orchestrator** — graphs of PRDs each running on workers;
  each node in the graph maps to a worker → maps to a peer → maps to
  envelopes.
- **Observer federation** — the `/api/observer/peers/*` registry
  shipped in v4.4.0; the `cluster.nodes` wire shape; the PWA modal
  shipped in v4.6.0.

## Scope

In scope:
- Worker-side: every spawned agent runs an in-process Shape A
  observer collector and pushes `StatsResponse v2` to the parent on
  the same bootstrap channel that already carries logs.
- Parent-side: `Manager.Spawn` mints an observer peer token alongside
  the bootstrap token and injects both into the worker env. On
  `Terminate`, the peer is dropped from the registry.
- Wire: reuse `/api/observer/peers/{name}/stats` exactly as Shape B / C
  use it. `peer_name` = agent ID (e.g. `agt_1234`). `shape` = "A".
- PWA: Federated peers card already lists peers; add an "Agents"
  filter pill so the operator can scope the list to F10 workers.
  Each agent envelope drills into the v4.6.0 process-tree modal.
- Orchestrator: `/api/orchestrator/graphs/{id}` payload gains an
  `observer_summary` field per-node (cpu / mem / status from the
  underlying agent's envelope), so the graph view shows live
  resource use without a second roundtrip.
- MCP / CLI / chat parity: `agent_observer_stats <agent_id>` etc.
- Recursive case: when an orchestrator node spawns CHILDREN (BL117
  fan-out), the children also peer-up and the parent's PWA shows the
  hierarchy. The peer registry is flat (no nesting); the hierarchy is
  reconstructed at render time from `agent.parent_id`.

Out of scope (defer to S14+):
- Cross-cluster federation tree (multi-parent topology).
- Per-pod alert routing.
- Observer-driven autoscaling decisions on the orchestrator.

## Wire contract

Reuses `StatsResponse v2` unchanged. Worker push body:

```json
{
  "shape": "A",
  "peer_name": "agt_a1b2c3",
  "snapshot": {
    "v": 2,
    "host": {"name": "agt_a1b2c3", "shape": "agent", …},
    "envelopes": [
      {"kind": "session", "id": "<session_id>", "cpu_pct": 23.4, "rss_bytes": 412334, …}
    ],
    …
  }
}
```

Note: `host.shape` becomes `"agent"` for worker peers (vs `"plugin"`,
`"daemon"`, `"cluster"` for the existing three shapes). Used by the
PWA to render the right icon / filter pill.

## Deliverables

### 1. Worker-side observer (cmd/datawatch-agent or slim daemon)

Two implementation paths — pick one:

**Path A** — extend `cmd/datawatch-agent` with the observer collector.
Pros: same binary already ships in the agent containers; minimal
deploy change. Cons: agent binary grows by the observer's size
(small — `internal/observer` is ~3 KB compiled).

**Path B** — every agent container ships `datawatch-stats` as a
sidecar process started by the entrypoint. Pros: no agent code
change. Cons: extra process per agent.

Pick A. The agent already initialises its own datawatch state on
boot; adding `observer.NewCollector(observer.DefaultConfig())` +
a peer push loop is ~50 LOC.

### 2. Parent-side spawn / terminate hooks

Two new lines in `Manager.Spawn`:

```go
peerToken, err := m.peerRegistry.Register(agt.ID, "A", agt.Version, hostInfo(agt))
if err != nil { /* warn-only; spawn doesn't fail on observer */ }
agt.Env["DATAWATCH_OBSERVER_PEER_TOKEN"] = peerToken
agt.Env["DATAWATCH_OBSERVER_PEER_NAME"]  = agt.ID
```

Two new lines in `Manager.Terminate`:

```go
_ = m.peerRegistry.Delete(agt.ID)
```

Token + peer name reach the worker through the existing env-injection
path (the bootstrap token uses the same path today). Worker reads them
on boot.

### 3. Worker push loop

```go
// inside cmd/datawatch-agent/main.go after collector start
peerName := os.Getenv("DATAWATCH_OBSERVER_PEER_NAME")
parentURL := os.Getenv("DATAWATCH_PARENT_URL")  // already injected today
token := os.Getenv("DATAWATCH_OBSERVER_PEER_TOKEN")
if peerName != "" && parentURL != "" && token != "" {
  pc, _ := newPeerClient(parentURL, peerName, "", true /* insecure-tls */)
  pc.setToken(token) // skip register — parent minted it on spawn
  go pushLoop(ctx, pc, col, 5*time.Second)
}
```

Reuses the `peerClient` type from `cmd/datawatch-stats/peer.go`.
Refactor: hoist `peerClient` into `internal/observerpeer/` so both
`cmd/datawatch-agent` and `cmd/datawatch-stats` can share it.

### 4. PWA filter + icon

`internal/server/web/app.js` → `loadObserverPeers()`:

- Tag rows by `host.shape`: `agent` / `daemon` / `cluster` (existing
  card already shows `shape` badge — extend the badge to use the
  inner host.shape for finer-grained labelling).
- Add a small filter pill row above the peer list:
  `All · Standalone · Cluster · Agents` — clicking filters the
  rendered list. State persists in localStorage.
- Click an agent peer → existing v4.6.0 process-tree modal.
  No new UI surface; the modal already handles this shape.

### 5. Orchestrator graph integration

`internal/server/orchestrator.go` →
`/api/orchestrator/graphs/{id}` response gains:

```json
{
  …,
  "nodes": [
    {
      "id": "n1",
      "agent_id": "agt_a1b2",
      "observer_summary": {
        "cpu_pct": 23.4,
        "rss_mb": 412,
        "envelope_count": 3,
        "last_push_at": "2026-04-25T..."
      }
    }
  ]
}
```

Looked up by joining `nodes[*].agent_id` against the peer registry.
No new endpoint.

### 6. MCP / CLI / chat parity

| Surface | Form |
|---|---|
| MCP | `observer_agent_stats { agent_id }` (alias of `observer_peer_stats` with the agent_id pre-resolved to peer name) |
| CLI | `datawatch observer agent stats <agent_id>` |
| Chat | `peers agt_a1b2 stats` (already works — the v4.5.1 `peers` command takes any peer name) |

### 7. Tests

- Unit: `Manager.Spawn` mints + injects env; `Manager.Terminate`
  drops the peer. Extends existing `internal/agents/spawn_test.go`.
- Integration: spin up a fake agent that pushes to an httptest parent;
  assert the parent's `peers/{agent_id}/stats` returns the pushed
  payload.
- PWA: snapshot test of the filter pill rendering; manual smoke for
  the modal (already covered by BL173 task 6).

## Open questions

1. **Token lifecycle on spawn failure** — when `Driver.Spawn` returns
   an error after `peerRegistry.Register` succeeded, the registry
   has an orphan peer. Add a cleanup in the `defer` of `Spawn`?
   Yes — same pattern as the bootstrap-token cleanup that already
   runs on spawn failure.
2. **Push interval** — workers can be very short-lived (some F10
   agents complete in <30s). Default 5s push interval means the
   parent might never see a non-zero envelope. Two options: bump
   the interval down to 1s for agents, or push immediately at
   collector start regardless of interval. Pick option 2 — push
   one snapshot at boot then settle to the configured interval.
3. **Network reach** — the agent already reaches the parent at
   `$DATAWATCH_PARENT_URL` for log push, so the observer push uses
   the same endpoint. No new firewall rules.
4. **Resource overhead on the worker** — observer collector tick
   adds ~5 ms per second on a 50-process worker. Acceptable. If a
   workload is sensitive, operator can disable per-agent via a
   `disable_observer: true` field on the project profile.
5. **Recursive spawn** — when agent A spawns child agent B (BL117
   fan-out), B registers with its OWN peer slot using its OWN agent
   ID. The hierarchy is rendered at the PWA level by joining
   `agent.parent_id` against `peer.name`. No nesting in the registry.
6. **TLS** — workers currently connect to the parent over the
   already-trusted bootstrap channel; observer push reuses the same
   client transport with `--insecure-tls` defaulted to `true` (matches
   current bootstrap behavior). Operator can override.

## Sprint plan (5 tasks, ~4 days)

| # | Task | Notes |
|---|---|---|
| 1 | Hoist `peerClient` from `cmd/datawatch-stats/peer.go` into `internal/observerpeer/` | Refactor; shared between `datawatch-stats` and `datawatch-agent`. Existing tests move with it. |
| 2 | Wire `Manager.Spawn` + `Manager.Terminate` to mint + drop the observer peer | Token injected via env; cleanup on spawn failure. |
| 3 | `cmd/datawatch-agent` boots an in-process observer + push loop | Reuses internal/observer.Collector; reads peer name + token from env; one immediate push then settle to 5s interval. |
| 4 | PWA Agents filter pill on the Federated peers card; orchestrator graph `observer_summary` field | Small JS + one server-side join. |
| 5 | MCP `observer_agent_stats`; CLI `datawatch observer agent stats <id>` | Thin aliases over `observer_peer_stats` with agent_id → peer_name resolve. Tests + openapi entry. |

## Acceptance criteria

- [ ] `datawatch agent spawn p c "echo hi"` produces a peer in
      `/api/observer/peers` within 10 s of the worker becoming
      Ready, with a non-empty envelope set.
- [ ] `datawatch agent terminate <id>` drops the peer from the
      registry within 5 s of the agent moving to Terminated.
- [ ] PWA Federated peers card shows the agent under the Agents
      filter; clicking opens the v4.6.0 process-tree modal with
      live data from the worker.
- [ ] BL117 orchestrator graph shows `observer_summary` per node when
      the underlying agent has registered (operators can sanity-check
      a fan-out by watching CPU spread across nodes).
- [ ] Spawn failure cleans up the orphan peer (no entry in
      `/api/observer/peers` for an agent that never started).
- [ ] All BL170 / 172 / 173 / 174 tests still pass; new tests for
      spawn/terminate hooks + push-loop happy path.

## Migration

None — additive. Operators with existing agent fleets see new rows
appear in the PWA Federated peers card on the next spawn after
upgrade. Old running agents do NOT retroactively peer-up; they
keep working as before until the next spawn.

## Sequencing

Land in this order:

1. Refactor: `peerClient` → `internal/observerpeer/` (1 commit; no
   behaviour change). Cuts a clean baseline.
2. Spawn + Terminate hooks (1 commit; tested in isolation against a
   fake registry).
3. Worker push loop (1 commit; integration test against an httptest
   parent).
4. PWA filter pill + orchestrator summary (1 commit; UI + small
   server-side join).
5. MCP / CLI parity (1 commit; trivial; tests).

Each commit ships independently; v4.7.0 ties the bow.

## What this unlocks

- **Per-PRD resource accounting**: BL117's PRD-DAG orchestrator gains
  per-node CPU / mem visibility, so operators can spot a runaway PRD
  without `kubectl exec`.
- **Mobile per-agent view**: datawatch-app gets the same data the PWA
  shows — useful when operators are away from a workstation but
  spawning agents over Signal.
- **Capacity planning**: the parent's federated view now reflects the
  full real cost of an agent fleet (workers + their LLM subprocesses
  + their docker children) rather than just what the parent's local
  collector sees.
