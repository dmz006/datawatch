# Federated peer placement — design

- Date: 2026-11-03
- Version: 9.67.0
- Status: Proposing (design only — no implementation in this doc)

## Scope and non-goals

This proposal defines how a registered federation peer advertises its local
LLM-serving compute nodes so that another datawatch instance (the
"coordinator") can place work — LLM calls and autonomous task sessions — on
them, and how an LLM entry (or an Automaton's LLM selection) names and
resolves a peer-scoped node.

In scope:

- An advertisement contract: what a peer publishes about each placeable
  node (identity, node kind, declared capacity, pinned model, load, health,
  the peer's datawatch version), at what cadence, and where the coordinator
  stores and expires it.
- A single identity token for a peer-scoped node used uniformly in LLM
  entries, capacity pools, the ledger, leases, and status surfaces.
- Resolution semantics: how the dispatcher expands an LLM entry's placement
  set into candidate nodes (local and peer-scoped), in what order, and which
  existing gates (capability, permissions, ledger, health) apply.

Non-goals:

- No migration of sessions already running on a node to another node.
  Placement is decided at admission time only; a failing peer surfaces as an
  admission failure or dispatch refusal, not a live move.
- No changes to the existing peer token/auth model, the observer stats
  push, or the CBAC capability registry shape. New capabilities are additive.
- No new standalone control plane service, no cluster membership protocol,
  no automatic topology discovery. Topology stays operator-declared.
- No renaming of existing local nodes and no changes to the local-node
  placement path in `internal/compute` / `internal/inference` for entries
  that only reference local nodes.
- This doc does not specify CLI flags, PWA widgets, or migration steps; a
  later implementation plan may.

Main alternative for scope: treat peer placement as a general workload
scheduling subsystem (arbitrary job types, preemption, re-balancing).
Rejected — it re-opens the local-node design (v7.0.0 S1) with scheduler
semantics datawatch has deliberately not adopted; placement here is
"pick where a session/LLM call starts" and nothing more.

## Current state

What exists today, verified against the tree:

- `internal/observer/peer_registry.go` — the observer peer registry
  (`PeerEntry`): name, bcrypt token hash, Shape (B/C), `HostInfo`, peer
  `Version`, `RegisteredAt`, `LastPushAt`, and the latest `StatsResponse`
  snapshot. Peers register and periodically push stats; the registry
  persists to `<data_dir>/observer/peers.json`. This is a monitoring
  channel — it carries metrics about a peer, not a declaration that a peer
  *offers capacity for placement*.
- `internal/compute/node.go` — the local ComputeNode model: `Name`, `Kind`
  (`ollama`, `openai-compat`, plus deprecated kinds), `DeclaredCapacity`,
  `Hardware`, `MaxConcurrentSessions`, `SessionSlots`,
  `SchedulingPriority`, `Disabled`, `Permissions`, `NodePinnedModel`,
  `ObserverPeer` binding, and `Routing` modes (`direct`,
  `docker-network`, `datawatch-proxy`). The `datawatch-proxy` mode routes an
  *LLM* through one pre-named peer
  (`RoutingDatawatchProxy.Peer` + `RemoteLLMName`) — a hardcoded single
  peer, not a placement decision.
- `internal/inference/llm.go` — LLM entries carry `ComputeNodes`
  (a list of local node names) for failover across local nodes;
  `internal/inference/dispatcher.go` resolves LLM → node and dispatches,
  including the `datawatch-proxy` path.
- `internal/capacity/ledger.go` — the admission ledger: named pools
  including `node:<name>` per compute node, all-or-nothing `Pools`,
  `Request.NodeChoices` (`[]NodeChoice`) with the ledger picking the least
  loaded candidate at dispatch time, `ExcludeNodes`, `PreferNode`
  tie-breaking, `Lease.Node`, and role-ordered fair wait queues. Admission
  is purely local: "nodes" here are local registry names.
- `internal/federation/capabilities.go` — per-peer CBAC: `surface:action`
  capability strings expanded from groups, checked via `Resolve`/`Check`.
  Capability vocabulary already exists for reading peers' state
  (`observers:read`, `llms:read`, `compute:read`) but nothing expresses
  "this peer may *accept placed work for* a model".
- `internal/autonomous/models.go` — `PRD.OwnerPeer` (BL331) records which
  federation peer originated a PRD routed in via the channel surface; it is
  provenance, not placement.
- `docs/architecture.md` — lists the `federation` package (CBAC peer
  registry, `fedCap()` guards) and the `observer` package (Shape A/B/C
  peers, cross-host correlation).

Gap: there is no way to say "this model runs on node X of peer P" as a
*placement* choice. Today the only cross-peer LLM path is the
`datawatch-proxy` routing mode with one peer and one remote LLM name
hard-wired per node, and the capacity ledger cannot count, queue against, or
exclude a peer's capacity.

## Advertisement: how a peer declares placeable resources

Proposal.

A peer advertises its placeable nodes as a discrete, versioned
**placement advertisement**, distinct from — but pushed over the same
authenticated peer-push channel as — the observer stats snapshot:

1. **Unit of advertisement is a local ComputeNode, not the peer.** The peer
   selects its registry entries that are offerable: `Disabled` is false,
   not in `MaintenanceWindows`, `Kind` is a supported kind (`ollama` or
   `openai-compat`), and the node's `Permissions`
   (`AllowedConsumers`/`DeniedConsumers`) allow the `placement` consumer.
   Everything else (docker-network nodes, nodes whose pinned model is not
   served) is simply not advertised.

2. **What one advertisement line carries:** the local node name; node `Kind`;
   `DeclaredCapacity` and `Hardware` (so the coordinator can do weak
   capability checks without dialing the peer); `NodePinnedModel` /
   `AllowModelSwap`; `SchedulingPriority`; `MaxConcurrentSessions` and
   `SessionSlots`; the peer's `Version` (from the registry
   `PeerEntry.Version` mechanism); the current session count on that node
   (from the observer's in-process counts, same source as `StatsResponse`);
   and a monotonically increasing advertisement sequence number per node.

3. **Cadence and freshness.** Advertisements ride the existing periodic peer
   push (so no new channel to operate) and are also sent at registry
   change (node add/remove/edit) and at startup. The coordinator stores
   advertisements in `<data_dir>/federation/placement/<peer>.json`, marks a
   peer-scoped node **stale** when no advertisement with a newer sequence
   arrives within three push intervals, and removes
   the entry from all candidate sets the moment staleness trips. Stale is
   visible in status surfaces; it is never silently routed to.

4. **Identity token.** A peer-scoped node is addressed everywhere by
   **`<peer-name>/<local-node-name>`**. This is the single token used in
   LLM entries, in capacity pools (the ledger gains `node:<peer>/<node>`
   pools whose external occupancy is the advertised session count), in
   leases (`Lease.Node`), and in status/hold views. The slash separates
   peer from node because neither component of a ComputeNode name may
   contain one under `validNodeName`'s rules, so the token is unambiguous
   against local node names.

5. **Authorization.** Advertising is gated by a new capability
   (`placement:advertise`) and accepting placements by one
   (`placement:serve`), both following the existing `surface:action`
   naming and deliberately left out of the broad built-in groups
   (mirror: how `sessions:result_write` and `plugin:read`/`plugin:write`
   are kept out of preset groups). A peer that advertises but is not
   granted `placement:serve` is ignored at resolution time.

6. **Trust boundary.** Advertisement data is *operator-declared capacity
   claims*, not ground truth: the coordinator uses it for candidate
   selection only. Admission limits still come from the coordinator's own
   ledger pools; the actual work executes under the peer's own local
   enforcement. The coordinator never executes local dispatch against a
   peer token.

Main alternative: **pull-based discovery** — the coordinator dials each
registered peer's REST API (with a stored token) and reads
`/api/compute/nodes` + `/api/llms` on a schedule to build the candidate
set. Rejected because it inverts the trust and network topology datawatch
already uses: peers hold the auth tokens and initiate; a pull model
requires the coordinator to egress to every peer, work behind NAT, store
live credentials for outbound calls, and grow coordinator load linearly
with peer count — and it would surface peers' node lists to anyone holding
a read capability, wider than a per-peer explicit "I offer this" claim.

## Naming: how an LLM entry or Automaton selects a peer and node

Proposal.

Selection stays **operator-named inside the LLM entry** — datawatch's
placement has always been declarative (LLM entry → `ComputeNodes` list →
dispatcher failover → ledger admission). Extend that same list to a
**placement set** instead of adding a parallel field:

1. **One list, two namespaces.** An LLM entry's `ComputeNodes` may contain
   local node names and peer tokens (`<peer>/<node>`) in the same list, in
   the operator's declared preference order. No new LLM schema surface;
   local-only entries are byte-identical and behave exactly as today.
   Validation rejects peer tokens whose peer is not registered and
   whose advertisement is not currently fresh, so a misnamed peer fails
   at edit time, not at first dispatch.

2. **Resolution pipeline at dispatch time** (reuses, does not replace, the
   existing gates, in existing order):
   - expand the placement set to candidates, local tokens first in
     declared order, then peer tokens;
   - drop stale peer candidates (freshness from the advertisement store);
   - drop candidates whose peer lacks `placement:serve` (CBAC);
   - drop candidates whose node `Kind`, pinned-model state, or advertised
     model set cannot serve the requested model (the same weak checks
     local dispatch performs, fed from the advertisement instead of the
     local registry);
   - hand the survivors to the admission ledger as `NodeChoices`
     (`capacity.Request.NodeChoices` with `NodeChoice.Priority` derived
     from the entry's declared order and each node's
     `SchedulingPriority`), so the ledger's existing least-loaded-first
     rule, `ExcludeNodes`, `PreferNode` relaunch stickiness, role
     ordering, and fair wait queue all apply uniformly across local and
     peer candidates. A peer candidate occupies a
     `node:<peer>/<node>` pool; a local one occupies `node:<name>` —
     no change to local admission semantics.

3. **Automatons.** An Automaton selects its LLM via the existing
   LLM-entry selection paths; the design adds no Automaton-specific
   vocabulary. Where an operator wants a PRD's work tied to where it
   originated, the natural expression is an LLM entry whose placement set
   leads with that peer's node — and `PRD.OwnerPeer`
   (`internal/autonomous/models.go`) remains the provenance record of
   where a PRD came from, which the resolution log should carry alongside
   the chosen token for audit symmetry.

4. **Failure behavior.** A dispatched call to a peer that dies mid-flight
   fails the *call* with a refusal naming the peer token; the next attempt
   (dispatcher retry within the same request, per its existing failover
   loop over candidates) may fall through to the next surviving candidate
   in the placement set. No automatic migration, no lease rewriting.

Main alternative: **resolver-side picking** — the LLM entry names only the
model; the coordinator's resolver chooses the best peer/node at request
time from all advertisements (scored by load, hardware, version), with
operators only able to exclude. Rejected because it makes placement
implicit and unauditable: an operator reading the LLM entry could not say
where that model will run, it silently couples every LLM entry to global
state (a new peer appearing changes every entry's behavior), and it
conflicts with the pinned-model and permissions model that treats
"what runs where" as an operator decision. The ledger already provides the
deterministic *within-candidate-set* load balancing the proposal needs
(`NodeChoices` + least-loaded dispatch), so there is no load-based reason
to move the decision out of the operator's declared set.
