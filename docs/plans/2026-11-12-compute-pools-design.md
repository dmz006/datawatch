# Compute pools — design

**Date:** 2026-11-12

**Status:** Design

**Version:** design only; any release that builds it is the next patch above main (feature work may call for the next minor; the operator decides in the Operator decisions section)

**Scope:** a first-class, named, operator-managed **compute pool** object that groups compute nodes (local and federated) for lane counting, placement and observability. Touches `internal/compute/` (registry and node record), `internal/autonomous/` (lane fill, executor LLM and node resolution), `internal/capacity/` (read-only: pool views over existing node counts), `internal/config/` (the new block and project policy), `internal/federation/` (capability checks), and the six operator surfaces (config.yaml, REST, CLI, MCP, chat verbs, web). Documentation only in this change: no code, no settings, no version fields, no changelog entry.

**Related plans:**

- [Federated peer placement](2026-11-03-federated-peer-placement.md) — how a peer advertises nodes and how placement through a peer is gated. This design reuses its capability model and its `<peer>/<node>` token.
- [Lane fill, shadow mode first](2026-10-24-lane-fill-shadow-mode.md) — the planner that today counts one GPU pool.
- [Usage auto-lanes](2026-11-02-usage-auto-lanes.md) — the lane-count source that already derives counts from caps.
- [Automata pages redesign](2026-11-06-automata-pages-redesign.md) — where the web views would sit.
- [Placement auto-tags parity](2026-11-03-placement-auto-tags-parity.md) — the tag-based placement filter pools must coexist with.

**The operator's request** (quoted as given):

> "pool seem like a datawatch construct, we may need multiple of we had enough gpu for different projects or automating, seems it should be a bit more configurable than a single pool"

> "nodes can be members of none, one or multiple, throughout datawatch and federated peers, make it rhobust any other suggestions based on datawatch design"

Today there is one pool, and it holds two nodes (node A and node B). It is not an object: it is whatever the pool host lister returns. This document refers to nodes generically (node A, node B, node C) and to a peer's nodes as `<peer>/<node>`; no real host name or address appears.

## Findings from the investigation

No investigation report existed at `.datawatch/reports/pools-investigation.md`, so the investigation was done directly against the tree. Everything below is read from the files cited; anything not confirmed is marked **unconfirmed**.

1. **The "pool" today is an implicit list, not an object.** `computePoolHosts.PoolHosts` in `cmd/datawatch/lane_fill_wiring.go` returns every enabled node whose kind is Ollama, de-duplicated by endpoint, plus the local Ollama host. There is no name, no membership record, no owner. `internal/autonomous/lane_fill_evidence.go` (`PoolHost`, `HostLister`, `NewPoolEvidenceSource`) reads that list to decide whether the GPU lane is busy. Changing the pool today means adding or disabling nodes.
2. **Lane fill knows exactly two lane kinds, each a single global count.** `LaneFillConfig` in `internal/autonomous/lane_fill_config.go` has `GPULanes`, `ClaudeLanes`, `GPUBackend`, `GPUModel`, with `GPUMode`/`ClaudeMode` per kind. `internal/autonomous/lane_fill_caps.go` derives the count from LLM caps (`deriveLaneCount`, `LaneCountSourceCaps`). There is no way to say "these nodes serve project X".
3. **Busy-ness is session counting against the pool, not node health.** `poolEvidenceSource` counts running sessions on the configured `gpu_backend`, plus sessions of other backends whose record names a pool host (`PoolSession.Node`, from `ComputeNodeRef` in `internal/session/manager.go`). When one host cannot be read it returns partial evidence and an error naming the host (`PoolHostFault`), so the planner never calls the pool idle on a guess. The shape of "one unreadable host degrades only that host" already exists; pools reuse it.
4. **Slots and leases are already counted once, at the node.** `NodeSlots` in `internal/compute/slots.go` derives a node's slot count from observer GPU data, an operator `session_slots`, and the `max_concurrent_sessions` ceiling. The capacity ledger (`internal/capacity/ledger.go`) holds leases against named pools `node:<name>`, `llm:<name>` and `node-verify:<name>`; a lease carries all its pools or none. **Naming collision:** the ledger already calls these "pools". The new object must not reuse that word for ledger keys (see section 3).
5. **A per-LLM node list already exists.** `LLM.ComputeNodes` in `internal/inference/llm.go` is an operator-ordered failover list, and `OrderedPoolNodes` in `internal/inference/pool_nodes.go` orders it by each node's `SchedulingPriority`. This is the closest existing thing to a pool, and it is per LLM entry, not per project. A compute pool sits above it as a candidate filter, not a replacement.
6. **Per-LLM caps already bound concurrency.** `EffectiveMaxInflight` in `internal/inference/llm.go` (explicit `MaxInflight`, then plan tier table, then kind default: subscription 1, local and API unlimited) is the one source for `llm:<name>` limits. `llmLaneCapSource` in `cmd/datawatch/lane_fill_wiring.go` feeds the same function to lane counts, so lane counts and admission agree. A pool must not introduce a second cap source.
7. **The node record already carries almost every per-member control.** `Node` in `internal/compute/node.go` has `Permissions` (allowed and denied consumers, deny wins), `Drained`/`DrainUntil`/`DrainReason`, `MaintenanceWindows`, `Disabled`, `SchedulingPriority`, `Tags`, `ObserverPeer`, `Routing` (including `datawatch-proxy` with `RoutingDatawatchProxyConfig.Peer`), and `LastDispatchError`. The file header records an operator decision (2026-05-08, BL295) that the object is named "ComputeNode, not Pool". A pool is therefore a **grouping above nodes**, not a rename; no per-node field moves.
8. **Placement is pure and node-scoped.** `PlaceMissingModel` in `internal/compute/placement.go` takes a caller-shortlisted candidate list and a task pin `PinnedTo` (exactly one node, "a pin is not a bypass"). The policy block `ComputePlacementConfig` (`missing_model`, `mem_headroom_pct`, `disk_headroom_gb`) lives in `internal/config/config.go` and is read live on every decision. A pool is the natural source of the caller's shortlist.
9. **The LLM override precedence is explicit in code.** `resolveTaskExecutionLLM` in `internal/autonomous/executor.go` resolves task, then story, then Automaton (the PRD record), then Automation, then the global fallback, and reports the winning level (`ExecutorSource*`). `ResolveVerificationLLM` in `internal/autonomous/verification_llm.go` uses the same order. Tasks and stories can also pin a node (`PinnedNode` in `internal/autonomous/models.go`). Pool assignment should follow this chain so there is one precedence to learn.
10. **A project tier already exists between the daemon and the Automaton.** `ProjectPolicy` in `internal/config/project_policy.go` resolves automaton, then project, then daemon, with `PolicySource` naming the winner, and already carries `LaneFillGPULanes`/`LaneFillClaudeLanes`. A project can already say "my GPU lane count"; it cannot say "on which nodes".
11. **Federation sessions already come back with their node and host.** `federationSessionLister.RunningSessions` in `cmd/datawatch/lane_fill_wiring.go` merges this host's sessions with `resp.Proxied` per peer and reports per-peer faults from `resp.Errors`. A peer's nodes are reachable only through `RoutingDatawatchProxy` (`internal/inference/proxy_router.go`, `internal/server/bl320_proxy_llm.go`, which requires the `sessions:input` capability), never by calling the peer's Ollama port.
12. **The federation capability model is deny-by-default, but placement capabilities are not shipped.** `internal/federation/capabilities.go` holds `allCaps` and `BuiltinGroups`; the 2026-11-03 federated placement plan proposes `placement:advertise` and `placement:submit` plus a `placement-partner` group. At the time of writing no `placement:` capability appears in `capabilities.go` (**unconfirmed** whether a build is in flight elsewhere). This design depends on those two capabilities and does not define new ones.
13. **There are existing "explain" calls to copy.** `POST /api/autonomous/planning-routing/test` (`handleAutonomousPlanningRoutingTest` in `internal/server/autonomous.go`) routes one input with no LLM call; the CLI counterpart is `newPlanningRoutingTestCmd` in `cmd/datawatch/cli_planning_routing.go`, the MCP tool is `toolPlanningRoutingTest` in `internal/mcp/planning_routing.go`, and `routing_rules_test` exists beside it. The "where would this run and why" call copies this shape.
14. **Surfaces for nodes are already complete and a model to follow.** Nodes are exposed at `/api/compute/nodes` (`internal/server/compute.go`), by MCP tools in `internal/mcp/compute.go`, by `datawatch compute node ...`, and by the chat verbs `compute node list|get|add|update|delete|health|detail|drain|undrain` documented at the top of `internal/router/compute.go`. A pool gets the same six-surface set.
15. **Not confirmed:** how long a federated peer may be silent before the existing code treats its observer data as stale. The observer peer registry under `internal/observer/` records last-seen data, but no single "down after N seconds" constant was found. Section 5 therefore proposes a value and marks the current behaviour **unconfirmed**.
16. **Not confirmed:** whether the lane utilization view reads pool hosts directly. The progress card in `internal/server/web/app.js` (`autonomous.progress.lanes`, the utilization section) and `GET /api/autonomous/lane-fill` (registered in `internal/server/server.go`) are the two places a pool view would attach; their exact data contract was not traced end to end.

The shape of the answer follows from these findings: the pool is a thin named set of node references, everything it needs per member already lives on the node record, the ledger stays the single place slots are counted, and the work is mostly resolution, counting views and surfaces.

## 1. The pool object

A **compute pool** is a named, operator-managed set of compute nodes. It carries policy about *which work may use those nodes*; it carries no capacity of its own.

### Fields

| Field | Type | Meaning |
|---|---|---|
| `name` | string, unique, `[a-z0-9-]`, 1–40 | Identity. Immutable after create (rename is delete plus add, so audit trails stay unambiguous). |
| `description` | string | Free text for the operator. |
| `members` | list of member refs | Each is `{node: "<name>"}` for a local node, or `{peer: "<peer>", node: "<name>"}` for one named node of a peer, or `{peer: "<peer>", scope: "all"}` for the peer's whole datawatch capacity. |
| `lane_count` | int or `from_caps` | Lanes this pool offers lane fill. `from_caps` (default) derives the count from the enabled LLM caps exactly as `deriveLaneCount` does; an integer overrides it. |
| `lane_kind` | `gpu` or `claude` | Which lane kind the pool feeds. Default `gpu`. A pool of local Ollama-style nodes is `gpu`; Claude lanes stay global and are not pooled in this design. |
| `backend`, `model` | string | The backend and model the pool's lanes run (same meaning as `gpu_backend`/`gpu_model`). Empty inherits the global value. |
| `mode` | `inherit`, `off`, `shadow`, `on` | Per-pool lane fill mode, same vocabulary as `gpu_mode`. Default `inherit`. |
| `spill_over` | list of pool names | Pools this one may borrow from when full. Default empty (never). See section 6. |
| `tags` | list of strings | Operator labels; also the hook for tag-based assignment later. |
| `disabled` | bool | A disabled pool offers no lanes and accepts no assignment; members are untouched. |
| `created_at`, `updated_at` | time | Bookkeeping, same as `Node`. |

Per-member health, drain, permissions and priority are **not** pool fields: they stay on the node record (`Node.Drained`, `Node.Permissions`, `Node.SchedulingPriority`). A pool may add one per-membership attribute only: `weight` (default 1), the relative share when two members are otherwise tied.

### Default pool created at upgrade

At the first start after upgrade, the daemon creates one pool named **`default`**, whose members are exactly what `computePoolHosts.PoolHosts` returns today (every enabled Ollama-kind node, plus the local host as the implicit local member), with `lane_count: from_caps`, `mode: inherit`, `spill_over: []`. Lane counts, placement and the evidence source therefore produce the same answers as before.

Reason for the name: the operator today has one unnamed pool, and `default` is the name that says "this is what existed before pools"; it is also the fallback in the assignment precedence (section 4), so the word in the precedence chain and the word on the object are the same. It is a reserved name that cannot be deleted while any assignment resolves to it, and it can be edited like any other pool. Alternatives considered: `main` (collides in speech with the main branch the operator talks about daily), `all-nodes` (wrong once nodes can be members of none).

### The six surfaces

**config.yaml.** A top-level `compute_pools` list, read live like `compute_placement`, with a commented template entry in `docs/config-reference.yaml`:

```yaml
compute_pools:
  - name: default
    description: "everything that existed before pools"
    lane_kind: gpu
    lane_count: from_caps
    mode: inherit
    members:
      - node: node-a
      - node: node-b
  - name: research
    lane_kind: gpu
    lane_count: 1
    members:
      - node: node-b
      - peer: peer-1
        node: node-c
    spill_over: [default]
```

**REST.** Under `/api/compute/pools`, matching the `/api/compute/nodes` handler style in `internal/server/compute.go`:

| Call | Does |
|---|---|
| `GET /api/compute/pools` | list pools with live member state |
| `GET /api/compute/pools/<name>` | one pool: record, members, counts |
| `POST /api/compute/pools` | create |
| `PUT /api/compute/pools/<name>` | replace (fields above, not `name`) |
| `DELETE /api/compute/pools/<name>` | delete; refused with the list of assignments that still resolve to it |
| `POST /api/compute/pools/<name>/members` | add one member ref |
| `DELETE /api/compute/pools/<name>/members/<member>` | remove one member |
| `POST /api/compute/pools/<name>/drain` and `/undrain` | drain or restore the whole pool (members each get a pool-scoped drain, section 5) |
| `POST /api/compute/pools/explain` | the "where would this run and why" call (section 8) |

**CLI.** `datawatch compute pool list|get|add|update|delete`, `datawatch compute pool member add|remove <pool> <member>`, `datawatch compute pool drain|undrain <pool> [--until RFC3339] [--reason text]`, `datawatch compute pool explain [--project p] [--automaton id] [--task id] [--llm name]`. Registered beside `compute node` in `cmd/datawatch/`.

**MCP.** `compute_pool_list`, `compute_pool_get`, `compute_pool_add`, `compute_pool_update`, `compute_pool_delete`, `compute_pool_member_add`, `compute_pool_member_remove`, `compute_pool_drain`, `compute_pool_undrain`, `compute_pool_explain`, in `internal/mcp/compute.go` next to the `compute_node_*` tools. Each tool is added to the `MCPToolCap` map in `internal/federation/mcp_tool_caps.go` so the capability completeness test keeps passing.

**Chat verbs.** Added to the `compute` verb in `internal/router/compute.go`:

```
compute pool                         list pools
compute pool get <name>              fetch one
compute pool add <name> lane_kind=gpu lane_count=2 [k=v ...]
compute pool update <name> [k=v ...]
compute pool delete <name>
compute pool join <pool> <node|peer/node|peer/*>
compute pool leave <pool> <member>
compute pool drain <name> [until=<RFC3339>] [reason=<text>]
compute pool undrain <name>
compute pool explain [project=<p>] [automaton=<id>] [llm=<name>]
```

**Web.** A **Pools** card on the Compute page (create, edit, join and leave with a node picker, drain, delete with the blocking list) and a pool selector on the project and Automaton editors (section 4). Every string is localized per the Localization Rule and carries no tracker IDs.

### Alternatives for the object's shape

| Option | What it is | Verdict |
|---|---|---|
| A. Named pool object with member refs | Described above | Matches every datawatch registry (nodes, LLMs, profiles, federation groups). |
| B. Pool as a tag on nodes | `Node.Tags` carry `pool:research`; no new object | Cheap, but there is no place for lane count, spill-over, mode or description, and tags are free text (typos silently make a new pool). `AutoTags` are daemon-internal. |
| C. Pool as a field on `LLM.ComputeNodes` | Extend the per-LLM failover list | Per-LLM, not per-project; cannot express "this project's nodes". |
| D. Pool as a federation group | Reuse `federation_group_*` | Federation groups are capability role presets for peers (`internal/federation/group_store.go`), a different concept; reusing the noun would confuse operators. |

Recommendation: **A, a named pool object with member references.** It is the only option that holds lane count, mode and spill-over in one place, it follows the registry pattern operators already know from nodes and LLMs, and tags (B) remain available as an optional label without carrying meaning.

## 2. Membership

A node may be in **no pool, one pool, or several pools**, and the same holds for a peer's nodes. Membership is a reference stored on the pool, never a copy of the node: deleting a node removes its references (with an audit entry); deleting a pool never touches a node.

### Local nodes

A local node joins by `{node: "<name>"}`. The node record is unchanged. `Node.Permissions` still decides which consumers may use it; being in a pool never widens a permission, and a denied consumer is refused on a pool member exactly as it is refused today.

### A node in no pool

A node that is in no pool **stays usable**: placement and the LLM dispatcher still choose it per its `Permissions`, `Disabled`, drain and maintenance state, and an explicit task pin (`PinnedNode`) or `LLM.ComputeNodes` entry still reaches it. It simply **counts for no lane**: lane fill's busy-ness and lane count read pools, so a node in no pool is not part of any lane and a session running on it is not counted against one. The pool view shows these as "unpooled nodes" so the operator can see capacity that no lane uses. This is deliberate: it lets an operator reserve a node for ad-hoc `ask` calls or a pinned job without any lane taking it over.

### Peers: whole capacity versus named nodes

| Form | Means | Resolves through |
|---|---|---|
| `{peer: "p", node: "n"}` | One named node of peer `p` | The peer's datawatch, at `<peer>/<node>` (token from the federated placement plan) |
| `{peer: "p", scope: "all"}` | Whatever the peer advertises as placeable now | The peer's datawatch; the member list is expanded from the peer's advertisement at each decision, never stored |

In both forms dispatch goes **through that peer's datawatch** using the existing `RoutingDatawatchProxy` path and the peer's own admission (`internal/inference/proxy_router.go`, `internal/server/bl320_proxy_llm.go`). It never goes to the peer node's Ollama port directly: the peer decides what it accepts, the coordinator never needs the peer node's address or TLS settings, and a peer with no grant is simply unreachable. A named node that the peer stops advertising becomes a **missing member** (shown, counted as down, never silently dropped).

### Duplicate and overlapping membership

The same node in two pools is legal and expected (section 3 for counting). The same node listed twice in one pool is rejected at write time. A whole-peer member and a named node of the same peer in one pool are rejected as overlapping.

### Alternatives for membership

| Option | Behavior | Verdict |
|---|---|---|
| A. Exclusive: a node is in exactly one pool | Simple counting | Rejects the operator's stated requirement ("none, one or multiple"). |
| B. Many-to-many references; unpooled nodes remain usable | Described above | Matches the request; counting stays safe if slots are counted at the node (section 3). |
| C. Many-to-many, but an unpooled node is unusable | Pools become an allow-list | Breaks every existing install that has a node outside the implicit pool (a disabled-kind node, an `ask`-only node); forces a migration decision per node. |
| D. Peer capacity only as a whole | One entry per peer | Cannot reserve one peer node for one project, and the peer's operator may not want every node offered. |

Recommendation: **B, many-to-many references with unpooled nodes staying usable, and peers joinable either whole or by named node.** It is the only option that satisfies "none, one or multiple" without breaking existing placement, and the whole-versus-named choice mirrors the grant granularity already proposed for peers.

## 3. Capacity accounting

**The rule: slots and leases are counted once, at the node, never per pool.** The ledger keeps `node:<name>` as the single place a node's slot count and held leases live (`internal/capacity/ledger.go`, `internal/compute/slots.go`). A pool never creates a ledger pool, never holds a lease, and never stores a limit. A pool's numbers are **views computed from node counts**:

- `pool.slots` = sum over members of each member's effective `NodeSlots` (for a peer node, the peer's advertised slot count; for a whole-peer member, the sum of its advertised nodes).
- `pool.held` = sum over members of each member's held leases on `node:<name>`.
- `pool.free` = `pool.slots - pool.held`, floored at 0.

Because the ledger is the only place a lease is recorded, a node in two pools cannot be double-booked: a lease taken through pool X raises the node's held count, and pool Y sees the same node as one slot fuller.

### How a pool's lane count and the per-LLM caps interact

There are three numbers and they only ever **take the minimum**:

1. The node's slots (`NodeSlots`, with `max_concurrent_sessions` as ceiling) — what the hardware takes.
2. The per-LLM cap (`EffectiveMaxInflight` → `llm:<name>`) — how many sessions of that backend run at once.
3. The pool's `lane_count` — how many lanes lane fill offers for that pool.

Lane count is a **planning ceiling**, not a reservation: lane fill may start work in a pool only while the pool has fewer running sessions than `min(lane_count, pool.free)`, and every start still passes the ledger, which enforces 1 and 2. When `lane_count` is `from_caps`, it is the sum of the enabled caps for the pool's backend, exactly `deriveLaneCount`; an unlimited entry makes the derived count fall back to the configured value, as that function already reports with its `Reason`. Raising a pool's `lane_count` above what caps and slots allow changes nothing the ledger would refuse; lowering it holds work back earlier.

### Worked example: a node in two pools

Node A: 2 GPUs, derived `NodeSlots` 2. Node B: 1 GPU, 1 slot. Local LLM entry `local-qwen` has `MaxInflight` 3 (explicit).

- Pool `default`: members node A and node B, `lane_count: from_caps` → derived 3 (the entry cap), so lanes offered = `min(3, free 3)` = 3.
- Pool `research`: members node B only, `lane_count: 1`.

State: one `research` Automaton is running on node B (one lease, `node:node-b` held 1 of 1).

| View | slots | held | free | lanes offered |
|---|---|---|---|---|
| node A | 2 | 0 | 2 | — |
| node B | 1 | 1 | 0 | — |
| pool `default` (A + B) | 3 | 1 | 2 | `min(3, 2)` = 2 |
| pool `research` (B) | 1 | 1 | 0 | `min(1, 0)` = 0 |

The one lease is counted once (node B's `held` is 1), and both pools show it. `default` correctly offers 2 lanes, not 3: node B is full whether the work came through `research` or `default`. If node B were instead counted per pool, `default` would show 3 free and a second job could be admitted onto a full node; the ledger would refuse it, but only after lane fill had already reported a lane idle and fired an idle alert for work that cannot start. Counting once at the node removes that false alert.

### Alternatives for accounting

| Option | Behavior | Verdict |
|---|---|---|
| A. Count at the node; pool counts are views | Described above | One source of truth, matches the ledger. |
| B. Per-pool ledger pools (`pool:<name>` with limits) | A lease carries `pool:<name>` and `node:<node>` | Reuses the ledger's all-or-nothing leases but needs every pool limit hand-kept equal to the sum of its nodes, and a node in two pools needs a lease on both, which makes one session consume a slot in each. |
| C. Split a shared node's slots between pools by weight | Node B's slot is 50/50 | A single-slot node cannot split; fractional slots invite starvation. |
| D. Reservation per pool | Pool holds slots in advance | Idle reserved capacity is exactly the idle-lane problem lane fill exists to remove. |

Recommendation: **A, count at the node and derive every pool number as a view.** Slot counts already come from observer data and the node ceiling, and the ledger is already the only admission authority; pools that only read it cannot disagree with it, and a node in several pools cannot be double-booked.

## 4. Assignment and precedence

Assignment answers "which pool's nodes may this work use?". It is a **candidate filter**: the chosen pool's members become the shortlist handed to placement (`PlaceMissingModel` candidates, `OrderedPoolNodes` for an LLM entry), which then applies every existing gate (permissions, drain, maintenance, headroom, task pin).

| Level | Where the assignment lives | Notes |
|---|---|---|
| Task | A `pool` field on the task, set like `set_task_llm` / `set_task_node` | Most specific. An explicit task `PinnedNode` still wins if it is a member of the resolved pool; if it is not, the pin is honored and the audit entry says the pin overrode the pool (a pin "is not a bypass" of node gates but is not subject to pool membership either). |
| Story | A `pool` field on the story | Applies to its tasks. |
| Automaton | A `pool` field on the Automaton (the `PRD` record) | Operator-facing name Automaton; internal type `PRD`. |
| Automation | A `pool` field on the Automation (the chain an Automaton is a member of) | Applies to member Automata that set nothing. |
| Project | `pool` in the project's `policy` block (`ProjectPolicy` in `internal/config/project_policy.go`) | The "different pools for different projects" case. |
| Global | Pool named `default` (or the operator's chosen default pool) | Fallback; same as today. |

### Precedence

**task → story → Automaton → Automation → project → global default.** This is the order `resolveTaskExecutionLLM` in `internal/autonomous/executor.go` uses for the worker LLM (task, story, PRD, Automation, global), with the project tier inserted where `ProjectPolicy` sits relative to the daemon (`internal/config/project_policy.go`: automaton, then project, then daemon). The most specific level that sets a pool wins and **does not cascade a less specific level's pool past it**; as with the LLM model rule (a model is taken from the same level its backend was found at), there is no mixing of levels. The resolver reports the winning level (`PoolSource*`, same idea as `ExecutorSource*` and `PolicySource`) so every surface can show "pool research, from the project".

An empty value at a level means "inherit". An explicit pool name is validated at write time (the pool must exist and not be disabled) and revalidated at dispatch (a pool deleted since falls through to the next level and raises an alert, never silently to `default` without a record).

Lane fill uses the same resolution to attach a queued Automaton to a pool's lane; an Automaton whose resolved pool has no free lane waits, and the wait reason names the pool (extending `internal/autonomous/wait_reason.go`).

### Alternatives for assignment

| Option | Behavior | Verdict |
|---|---|---|
| A. Layered fields with the LLM precedence chain | Described above | One precedence for operators to learn; per-task pinning keeps working. |
| B. Project only | Pool lives on the project; Automata inherit | Cannot send one expensive task to a bigger pool; cannot give an Automation its own lane. |
| C. Tag matching (pool matches work tags) | Work tagged `gpu-large` picks the pool tagged the same | Implicit; two matches need a tie rule; makes "why did this run there" hard to answer. Compatible later as a way to set the project or Automaton field. |
| D. Rule engine (match expressions) | Ordered match rules → pool | Powerful but a second rule language beside `routing_rules` and planning routing. |

Recommendation: **A, layered fields resolved in the existing override order, with the project tier included.** It repeats a precedence operators already rely on for LLMs and verifiers, the winning level is reportable, and tags (C) can later be a *way to fill* these fields without being a second resolution path.

## 5. Health

Health is **per member**, never per pool, and it is derived from data the node record and observer already hold. A pool's health is the summary of its members.

### Member states

| State | Meaning | Derived from |
|---|---|---|
| **up** | Member is eligible and fresh | Not `Disabled`, drain not active, not in a maintenance window (`MaintenanceWindows`), `LastDispatchError` empty, observer or peer data fresh |
| **degraded** | Member is eligible but impaired | A recent dispatch error, observer data stale but not yet down, partial read (`PoolHostFault` for that host), a peer member whose advertisement is stale |
| **down** | Member is not usable now | Disabled, drain active, in a maintenance window, unreachable past the silent-member limit, or a named peer node no longer advertised |

`Node.Drained`/`DrainUntil` are evaluated the way `DrainActive` already does: an expired drain reads as not active without mutating the node. The state is computed on read; nothing is written for health.

### The time before a silent member counts as down

A member that has produced no successful read for **`down_after_seconds`** is **down**; between **`degraded_after_seconds`** and `down_after_seconds` it is **degraded**. Proposed defaults: degraded after **90 s**, down after **300 s**, as `compute_pools.health` settings (config.yaml block, same six surfaces as every setting, read live). Observer-down is not node-down in the existing design (`Node.ObserverPeer` comment: "observer going offline does NOT clear this"), so a silent *observer* alone only degrades its node; a silent *dispatch path* (probe failure or peer advertisement stale) is what leads to down. The current stale limit for peer data is **unconfirmed** (finding 15), so the numbers above are proposals to be checked against the observer peer registry in the build.

### Drain and maintenance per member

The node-level drain and maintenance windows stay the only *global* controls: they apply to the node in every pool it belongs to, which is correct (the hardware is the same). A pool adds one **pool-scoped drain** per membership: `compute pool drain <pool> [member]` marks a member drained **in that pool only** (with `until` and `reason`, as for nodes), so an operator can pull node B out of the `research` pool while it keeps serving `default`. Draining the whole pool applies a pool-scoped drain to each member. A pool-scoped drain is stored on the pool's member entry, not the node record, so node-level drain remains the single source for "this machine is out".

### An unreadable member degrades only that member

When a member cannot be read (a host that fails its model fetch, a peer that does not answer, an observer that has not pushed) the result is that member's state — degraded, then down — and the pool continues with the remaining members. This is the contract `poolEvidenceSource` already has (partial evidence plus an error naming the host, in `internal/autonomous/lane_fill_evidence.go`) and `RunningSessions` already has for peers (per-peer fault, other sessions kept). Lane fill must never report a pool idle while any of its members is degraded and unread (the "never calls the pool idle on a guess" rule), and must not report a pool as down because one member is.

### Alternatives for health

| Option | Behavior | Verdict |
|---|---|---|
| A. Per-member state, pool summary | Described above | Matches how evidence faults already work. |
| B. Pool-level up/down only | One flag per pool | Hides the one bad member; an unreadable node would flip the whole pool. |
| C. Active probe loop per pool | New poller for members | Duplicates the probe in `internal/compute/probe.go` and the observer push. |
| D. Require N consecutive failures | Flap damping | Useful but additive: the two time thresholds already damp flapping without a counter. |

Recommendation: **A, per-member up, degraded and down computed on read from existing node and observer data, with the 90 s and 300 s proposed limits as live settings.** It reuses the fault model lane fill already has, never lets one unreadable member take down a pool, and adds no new poller.

## 6. Spill-over

**Spill-over** is using another pool's nodes when the assigned pool is full or has no healthy member. It is **off by default and explicit**: a pool spills only into pools named in its own `spill_over` list.

### The rule

1. Spill-over is considered only when the assigned pool offers no lane (`min(lane_count, free) == 0`) or has no `up` member, **and** the pool's `spill_over` list is non-empty.
2. Targets are tried in list order; the first with a free lane and an `up` member that is **permitted for this consumer** is used. Node permissions (`Node.Permissions`) are not relaxed by spill-over.
3. Spill-over never crosses a federation boundary unless the target pool is itself the one the operator listed and the federation checks in section 7 pass.
4. The spill target must not itself spill (depth 1), so a cycle is impossible.
5. Spill is per decision: the next decision starts again from the assigned pool.
6. An Automaton may opt out (`spill_over: none`) at its own level; it may not opt in to a pool its assigned pool did not list.

### What is audited

Every spill decision writes an audit entry (event type added in `internal/audit/events.go` beside the existing capacity events) with: the work id (Automaton, story, task), the assigned pool and why it was full (`lane_count`, `free`, member states), the pool spilled to and the node chosen, whether the node is local or a peer (peer name only), and the winning precedence level. A refused spill (target full, target unhealthy, permission denied, federation check failed) is also audited with the reason, so "why did it wait instead of spilling" is answerable. Shadow mode (section 9) records what would have spilled without starting anything.

### Alternatives for spill-over

| Option | Behavior | Verdict |
|---|---|---|
| A. Explicit opt-in list per pool, depth 1 | Described above | Predictable, auditable, cannot loop. |
| B. Always spill into `default` | Implicit fallback | A project's reserved nodes get used by anything that overflows into `default`, and the reverse; defeats isolation. |
| C. Never spill | Strict isolation | Simple, but a full pool idles while another has room; the thing lane fill exists to prevent. |
| D. Priority-weighted automatic spill | Spill by node priority across all pools | Crosses reservations silently; hard to audit. |

Recommendation: **A, explicit per-pool opt-in with depth 1, full audit including refusals.** It keeps isolation the default (the operator asked for pools *for different projects*), lets the operator trade isolation for utilization one pool at a time, and the depth limit removes loop analysis.

## 7. Federation safety

A pool that contains peer members lets local work run on someone else's machine, so it follows the same deny-by-default rules as the federated placement plan; it does not define a second control plane.

### The peer capability a placement through a peer needs

Placing work on a peer member requires the capability **`placement:submit`** on the grant held for that peer, and a peer's nodes may enter a pool's candidate set only while the peer holds **`placement:advertise`** (both from [the federated placement plan](2026-11-03-federated-peer-placement.md), checked through the existing `Resolve()`/`Check()` chokepoint in `internal/federation/capabilities.go`; the `placement-partner` builtin group holds exactly these two). The checks are:

- At pool write time: adding a `{peer: ...}` member is refused (or accepted but marked `inactive`, operator's choice at write time) when the peer lacks `placement:advertise`.
- At decision time: a peer member without both capabilities is treated as **down** for placement with reason `capability`, and shows as such in the pool view.
- The peer enforces independently when the submit arrives; a coordinator-side grant never forces the peer to accept.

If those capabilities are not yet in a build (finding 12), peer members cannot be activated: they can be written to a pool in `inactive` state and are skipped. This design adds no capability of its own.

### Audit entries

Every pool-related federation act is audited: peer member added or removed (who, pool, peer), placement through a peer (work id, pool, peer, node, capability checked), refusal (capability missing, peer stale, peer refused), spill that crossed into a peer pool member, and a pool delete that removed peer members. Entries name peers and nodes by their registered names only; no address, token or fingerprint is written.

### What a peer may learn about local pools

A peer learns **nothing about pool structure**. It sees only what it sees today for a placed task: the work arrives as a placement request naming its own node, with the coordinator's identity (`OwnerPeer`) and no pool name, no other members, no lane counts, no spill-over list. Pool names are local labels and never leave the coordinator. A peer's own `pools` are likewise not advertised: a peer advertises **nodes** (the placement plan's advertisement contract), and the coordinator groups them into its own pools. `GET /api/compute/pools` is therefore a local read, subject to the caller's normal capability, and is not exposed to peer tokens unless an operator grants a read capability for it.

### Alternatives for federation safety

| Option | Behavior | Verdict |
|---|---|---|
| A. Reuse `placement:advertise` and `placement:submit`; local-only pool names | Described above | One capability model; nothing new to leak. |
| B. New `pool:*` capabilities | Peer-visible pool grants | Second vocabulary for the same act; peer would learn pool structure. |
| C. Treat peer members like local nodes | No capability check | Violates deny-by-default; a registered peer would be placeable without a grant. |
| D. Peers advertise their own pools | Coordinator imports peer pools | Imports another operator's grouping decisions and names; breaks the "peer decides what it offers" boundary. |

Recommendation: **A, reuse the two placement capabilities and keep pool names local.** It adds no second permission system, matches how the federated placement plan already scopes trust, and a peer cannot learn the coordinator's pool layout because it is never sent.

## 8. Observability

### Pool view in the lane utilization page

The lane utilization section of the progress card (`internal/server/web/app.js`, utilization section; settings key `autonomous.progress.lanes`) and `GET /api/autonomous/lane-fill` gain a **by pool** breakdown: for each pool its lane count and where that count came from (the existing `LaneCountSourceCaps`/`LaneCountSourceConfig` vocabulary), lanes busy and free, members with state (up, degraded, down) and the reason, and the unpooled nodes line. A queued Automaton's wait reason names its pool.

### Stats

Per-pool gauges are exposed through the existing stats path (`internal/metrics/prometheus.go` and the stats REST/MCP calls): `pool_slots`, `pool_held`, `pool_free`, `pool_lanes_offered`, `pool_members{state=...}`. They are computed views (section 3) so they are read-only and never stored.

### Alerts

Two new alert kinds on the existing alert path (`internal/autonomous/lane_fill_alert.go` is the model, which already raises an idle-lane alert after `idle_alert_minutes`):

- **Empty pool:** a pool has no `up` member (all down, or no members). Raised once per transition and cleared when a member comes up.
- **Exhausted pool:** a pool has queued eligible work and `free == 0` for `exhausted_alert_minutes` (default 30, matching `idle_alert_minutes`, 0 = off), naming whether spill-over was possible and, if not, why.

Both alerts name the pool by name and list members and states; neither includes an address.

### "Where would this run and why" — the explain call

Modelled on the planning routing test (`POST /api/autonomous/planning-routing/test`, "route one input, no LLM call"), the explain call **starts nothing and calls no LLM**. Input: a project, an Automaton, a task, or an LLM name, optionally a consumer. Output: the resolved pool and the precedence level that supplied it, the members considered with each member's state and the gate that excluded it (permission, drain, maintenance, headroom, pin, capability), the spill-over target if the pool is full, the chosen node (or "none, would wait: reason"), and the lane count arithmetic from section 3.

| Surface | Shape |
|---|---|
| config.yaml | None (a read, not a setting). The settings it explains (`compute_pools`, `compute_pools.health`) are all there. |
| REST | `POST /api/compute/pools/explain` with `{project, automaton, task, llm, consumer}` |
| CLI | `datawatch compute pool explain [--project p] [--automaton id] [--task id] [--llm name] [--consumer c]` |
| MCP | `compute_pool_explain` with the same fields |
| Chat verb | `compute pool explain project=<p> automaton=<id> llm=<name>` |
| Web | An "Explain" action on the project, Automaton and pool views that shows the same text |

### Alternatives for observability

| Option | Behavior | Verdict |
|---|---|---|
| A. Pool breakdown in existing pages, two alert kinds, explain on all surfaces | Described above | Extends what operators already look at; explain makes precedence debuggable. |
| B. A separate pools dashboard only | New page | A second place to look; lane fill's own status would not mention pools. |
| C. Alerts only | No view | Operators cannot see why an alert fired. |
| D. Explain only on the CLI | One surface | Violates the parity rule; the operator is often on chat or web. |

Recommendation: **A, extend the lane utilization view and lane-fill status, add the two alerts, and ship explain on all six surfaces.** It adds no new page to learn, reuses the alert path that already exists for idle lanes, and explain is the one tool that makes a six-level precedence debuggable.

## 9. Migration and rollout

### Shadow mode first, then on

A new setting `compute_pools.mode` takes **`off`**, **`shadow`** (default after upgrade) and **`on`**, the same vocabulary as `autonomous.lane_fill.mode` (`LaneFillConfig.Mode` in `internal/autonomous/lane_fill_config.go`).

1. **Upgrade:** the daemon creates pool `default` from the pool host lister and the empty assignment chain. Mode is `shadow`.
2. **Shadow:** lane fill and placement continue to use the existing path and answers. In parallel the pool resolver computes what it would have decided (assigned pool, members, chosen node, spill) and writes it to the audit log and the explain call, flagged `shadow`. Divergence between the pool answer and the live answer is counted and shown. With only pool `default` the two must match; any mismatch is a defect to fix before `on`.
3. **On:** the pool resolver becomes the source for lane counts and candidate shortlists. Flipping is one setting; flipping back to `shadow` restores the old path immediately because it was never removed.
4. Operators create additional pools in `shadow` and watch the divergence report before turning the mode on; the mode is global but each pool has its own `mode` (`inherit`, `off`, `shadow`, `on`) so one new pool can go live while others still shadow.

### What a downgrade leaves behind

A downgrade to a build without pools leaves: the `compute_pools` block in config.yaml (unknown keys are ignored by the older loader; **unconfirmed**, to be proven by a downgrade test in the build), `pool` fields on task, story, Automaton, Automation and project records (ignored by the older code, preserved on write only if the record round-trips unknown fields, which is **unconfirmed**), and audit entries and alerts that name pools. Nothing is written to node records, so nodes, slots, leases and the ledger are unchanged; the older code runs lane fill from the pool host lister exactly as before, which is why pool `default` is created from that lister. The build must include a downgrade test that proves the older loader starts and lane fill runs.

### Alternatives for rollout

| Option | Behavior | Verdict |
|---|---|---|
| A. Shadow then on, default pool from the host lister | Described above | Same pattern lane fill used; zero behavior change at upgrade. |
| B. On immediately with default pool | Faster | A resolver defect would stall lanes with no comparison available. |
| C. Opt-in off by default | Nothing changes until the operator creates a pool | Pools never get exercised on the common single-pool install; shadow comparison is lost. |
| D. Per-project rollout flag | Pools on for some projects | A second rollout dimension beside pool `mode`; not needed. |

Recommendation: **A, upgrade into shadow with a `default` pool that reproduces today's pool, then flip to on after a clean divergence report.** It repeats the rollout already proven for lane fill, changes no behavior at upgrade, and the old path stays available until the operator chooses to drop it.

## 10. Phased build plan

Each phase below is an Automaton (or a few) executed by the normal Automata flow; counts are estimates, one Automaton being a normal-sized, independently verifiable change. Every phase's own checklist carries the Phase Completion Checklist from `AGENT.md` (docs, `docs/config-reference.yaml`, locale strings, testing tracker, parity issue), and each phase is verified by tests and, where it touches the daemon, by a live check on a sandbox daemon, not the operator's.

### Phases with an estimate of Automata per phase

| Phase | Content | Status | Est. Automata |
|---|---|---|---|
| 1 | Pool record, registry store and validation in `internal/compute/` (members, ref forms, reserved `default`); `compute_pools` config block; default pool created at upgrade from the host lister | Planned | 2 |
| 2 | Pool resolver: the assignment chain (task → story → Automaton → Automation → project → global), `PoolSource` reporting, project policy `pool` field | Planned | 2 |
| 3 | Counting views: pool slots, held, free, lanes offered from node counts and caps; lane fill reads pools; shadow mode and divergence report | Planned | 3 |
| 4 | Health: per-member state, `compute_pools.health` settings, pool-scoped drain, fault handling reusing `PoolHostFault` | Planned | 2 |
| 5 | Surfaces: REST, CLI, MCP (with `MCPToolCap` entries), chat verbs, config template, openapi, localization | Planned | 4 |
| 6 | Web: Pools card on the Compute page, selectors on project and Automaton editors, pool breakdown in lane utilization | Planned | 3 |
| 7 | Spill-over and its audit entries; wait reasons that name the pool | Planned | 2 |
| 8 | Peer members: capability checks, `<peer>/<node>` and whole-peer expansion, federation audit entries (blocked on the placement capabilities existing) | Planned | 3 |
| 9 | Observability: stats gauges, empty and exhausted alerts, explain call on all six surfaces | Planned | 3 |
| 10 | Go-live: divergence review, switch to `on`, downgrade test, docs and howto, how-to for splitting a pool | Planned | 2 |
| 11 | Mobile parity tracking in the companion app (issue filed per phase that changes the web, tracked there; not built here) | Planned | 0 here |

Estimated total: about **26 Automata** (range 22–32), run sequentially by phase except that phases 5 and 6 can overlap once phase 3 is done and phase 8 waits for the placement capabilities.

### Risks

- **Word collision with ledger "pools".** The ledger already uses `node:`, `llm:`, `node-verify:` pools (`internal/capacity/ledger.go`). *Mitigation:* the user-facing noun is "compute pool" everywhere; the ledger is never given a `pool:` key; docs and code comments say "ledger pool" for the old meaning.
- **Double counting a node in two pools.** *Mitigation:* section 3's rule, a test that a node in two pools reports one held lease, and the divergence report in shadow.
- **A pool resolver defect stalls lanes.** *Mitigation:* shadow first, the old path stays until `on`, and flipping back is one setting.
- **Federation leak or silent peer use.** *Mitigation:* peer members need both placement capabilities, are `inactive` without them, the peer never learns pool names, and every peer act is audited.
- **Spill-over breaks isolation.** *Mitigation:* explicit opt-in only, depth 1, never relaxes node permissions, refusals audited.
- **Stale peer data read as healthy.** *Mitigation:* the two time limits and the rule that an unread member is never reported idle; the real stale limit is confirmed against the observer registry in phase 4 (finding 15).
- **Deleting a pool that work still resolves to.** *Mitigation:* delete is refused with the list of assignments, and a revalidation at dispatch falls through with an alert instead of silently using `default`.
- **Downgrade surprises.** *Mitigation:* nothing is written to node records, and a downgrade test is a phase 10 exit criterion (both unconfirmed claims in section 9 must be proven there).
- **Scope creep into a scheduler.** *Mitigation:* pools filter candidates and count lanes; placement, admission and slots remain where they are (matches the non-goal in the federated placement plan).
- **Operator interface sprawl.** *Mitigation:* the six surfaces are generated from one table of fields per the Configuration Accessibility Rule, and the web design questions (where a card goes) are asked before building.

## Operator decisions needed

1. **Should the default pool be named `default`?** (It is created from `computePoolHosts` in `cmd/datawatch/lane_fill_wiring.go`.) Options: (a) `default` — the fallback word in the precedence chain; (b) `main`; (c) a name the operator picks at upgrade. *Recommendation: (a).* It needs no operator action at upgrade and matches the precedence wording.
2. **Should the first build, reading `Node.Permissions` in `internal/compute/node.go`, make an unpooled node usable by placement but outside every lane (section 2), or require every node to be in a pool?** Options: (a) usable but laneless; (b) unusable until pooled. *Recommendation: (a).* It breaks no existing install and lets a node be reserved for `ask` or a pinned job.
3. **Should spill-over be allowed to cross into a peer's nodes in the first release?** Options: (a) local-only spill in the first release, peers later; (b) allow from the start; (c) never. *Recommendation: (a).* It proves isolation and audit on local nodes before any work crosses a trust boundary, and phase 8 can lift it.
4. **What should the health limits be?** Options: (a) 90 s degraded and 300 s down; (b) 60 s and 180 s; (c) 180 s and 600 s. *Recommendation: (a).* It is slower than a probe round but faster than a drain decision, and the numbers are live settings so the operator can change them without a restart.
5. **Where should the pool UI go?** Options: (a) a Pools card on the Compute page plus selectors on the project and Automaton editors; (b) a new sub-tab under Compute; (c) under Settings. *Recommendation: (a).* Pools are about nodes, so the Compute page is where the operator looks, and the selectors sit where the assignment is made. Per the standing rule, the placement is confirmed with the operator before the web phase starts.
6. **Should Claude lanes be poolable in this design?** Options: (a) GPU lanes only (Claude lanes stay global); (b) both lane kinds; (c) GPU now, Claude after usage-based lane counts settle. *Recommendation: (a).* Claude lanes are bounded by the plan and usage calculator rather than by nodes, so a node pool does not describe them, and the lane kind field leaves room for (c).
7. **Should a whole-peer member be allowed, or only named peer nodes?** Options: (a) both; (b) named nodes only; (c) whole peer only. *Recommendation: (a).* A whole-peer member is convenient for a trusted partner, a named node is the safe way to share one machine, and both go through the same capability checks.
8. **What release size should this carry?** Options: (a) next minor above main, because it adds an object and six surfaces; (b) next patch above main. *Recommendation: (a).* It is new operator-visible feature surface, which the release numbering rule treats as a minor; the operator sets the final number at merge.
9. **Should the pool be exposed to peers for read at all?** Options: (a) never (local only); (b) read-only pool list behind an explicit grant; (c) full read. *Recommendation: (a).* A peer needs the placement capabilities, not the coordinator's layout, and (b) can be added later without breaking (a).

## Documentation impact

This change is design only, so no user-facing document describes pools yet and none may claim they exist. When the build phases land, each document below is updated in the same change as the phase that ships the behavior (the Phase Completion Checklist in `AGENT.md`). Every existing link and anchor into these files stays reachable: pool material is added as new sections or new rows, and no heading is renamed or removed.

| Document | Entry to update after the build | Phase |
|---|---|---|
| `docs/howto/compute-nodes.md` | New section "Grouping nodes into pools": a node in none, one or several pools, unpooled nodes staying usable, peer members by name or whole, pool-scoped drain. Cross-link to the existing `Node.Permissions` and drain sections. | 5 |
| `docs/howto/automation-lane-fill.md` | Extend "The lane counts rule" and "Pool evidence and a node with a private certificate" with the pool as the unit of lane counting, the `min(lane_count, free)` arithmetic, and the shadow to on switch for `compute_pools.mode`. Keep the term "ledger pool" for the `node:` and `llm:` keys. | 3 |
| `docs/howto/federation-cbac.md` and `docs/howto/compute-routing.md` | Peer members need `placement:advertise` and `placement:submit`; a peer never learns pool names. | 8 |
| `docs/operations.md` | Operator procedures: split a pool, drain one member in one pool, read the empty-pool and exhausted-pool alerts, roll back from `on` to `shadow`, downgrade steps. | 10 |
| `docs/datawatch-definitions.md` | New definitions: compute pool, member, unpooled node, spill-over, pool source (winning precedence level), explain. Clarify that "ledger pool" is a different, older meaning. Add the Pools card under Settings, Compute. | 6 |
| `docs/config-reference.yaml` | Commented `compute_pools`, `compute_pools.health` and `compute_pools.mode` entries. | 5 |
| `docs/plans/README.md` | The design-docs link and registry row 501 exist now; the row moves from design to done as phases ship. | each |

## Operator review before this becomes a build

These need the operator's eyes; none is decided here beyond the recommendation in the Operator decisions section.

- The nine decisions above, in particular decision 2 (unpooled nodes stay usable but count for no lane) and decision 8 (release size).
- **Unconfirmed claims** that a build must prove first: finding 12 (the `placement:` capabilities are not yet in `capabilities.go`, so phase 8 is blocked), finding 15 (the real stale limit for peer data, so 90 s and 300 s are proposals), finding 16 (the exact data contract of the lane utilization view), and the two downgrade claims in section 9.
- **Web placement** of the Pools card and the editor selectors is a design question for the operator before phase 6.
- **Mobile parity:** a `datawatch-app` issue is due at each web-changing phase; none is filed by this documentation change.
- The word "pool" is already used by the capacity ledger; confirm that user-facing text says "compute pool" throughout.

## Parity surface

Per the parity-surface rule in `AGENT.md`, this lists every channel the pool feature touches. All surfaces are included except the two mobile clients, which are tracked in the companion app and not built here.

| Surface | Include / Exclude | What the surface gets (or why excluded) |
|---|---|---|
| REST | Include | `/api/compute/pools` list, get, create, replace, delete; member add and remove; drain and undrain; `POST /api/compute/pools/explain`; pool fields on task, story, Automaton, Automation and project records; pool breakdown added to `GET /api/autonomous/lane-fill`. Guarded by `fedCap` like the node endpoints. |
| MCP | Include | `compute_pool_*` tools (list, get, add, update, delete, member add and remove, drain, undrain, explain) with `MCPToolCap` entries, plus a `pool` argument on the existing set-LLM and set-node style tools for task, story, Automaton and Automation. |
| CLI | Include | `datawatch compute pool ...` (list, get, add, update, delete, member, drain, undrain, explain) and `datawatch config set compute_pools...` round trips. |
| comm channel | Include | `compute pool ...` verbs (list, get, add, update, delete, join, leave, drain, undrain, explain) in `internal/router/compute.go`, and `configure compute_pools...`. |
| YAML/config | Include | `compute_pools` list and `compute_pools.health` / `compute_pools.mode` settings, each with a commented entry in `docs/config-reference.yaml`; `pool` in a project's `policy` block; read live without a restart. |
| Web (PWA) | Include | Pools card on the Compute page, pool selectors on project and Automaton editors, by-pool breakdown in the lane utilization view, explain action; localized, no tracker IDs. Placement of each element is confirmed with the operator first. |
| Android | Exclude here, tracked in the companion app | Parity is required (PWA, Android and iPhone must match) but delivery is native in the companion app; a `datawatch-app` issue is filed at each web-changing phase and the work is tracked there, **not built in this repository**. |
| iPhone/iOS | Exclude here, tracked in the companion app | Same as Android: capability parity through the shared REST contract, native delivery, tracked in the companion app and not built here. |
