# Federated peer placement — design

**Date:** 2026-11-03

**Version:** 9.67.0

**Status:** Design proposal, awaiting operator confirmation

## Scope and non-goals

This document is docs only: it proposes how a registered federation peer
advertises its local LLM-serving compute nodes so another datawatch instance
(the "coordinator") can place work — LLM calls and autonomous task sessions —
on them, and how an LLM entry or an Automaton names and resolves a peer-scoped
node. **No implementation happens for this doc.**

In scope as proposal:

- The advertisement contract: what a peer publishes about each placeable
  node (identity, node kind, declared capacity, pinned model, load, health,
  the peer's datawatch version), at what cadence, and where the coordinator
  stores and expires it.
- A single identity token for a peer-scoped node used uniformly in LLM
  entries, capacity pools, the ledger, leases, and status surfaces.
- Resolution semantics: how an LLM entry or Automaton placement set expands
  into candidate nodes (local and peer-scoped), in what order, and which
  existing gates (capability, permissions, ledger, health) apply.
- Naming: the field shape on an LLM registry entry and on an Automaton,
  the precedence when both are set, and the mapping onto the capacity
  request.

Non-goals:

- No code, no migration of sessions already running on a node, no changes
  to the peer token/auth model, no new control plane service, no cluster
  membership protocol, no automatic topology discovery. Topology stays
  operator-declared.
- **No change to the existing concurrency/budget design doc**
  [docs/plans/2026-10-14-concurrency-and-budget-design.md](2026-10-14-concurrency-and-budget-design.md).
  That doc owns local admission and budgets; this proposal only adds
  *candidates* for its existing machinery to choose among. If the two
  conflict, the concurrency doc wins and this proposal is amended.
- This doc does not specify CLI flags, PWA widgets, or migration steps;
  the open operator decisions and deferred surfaces are handled by later
  sections of the plan series.

**How this becomes a build:** the implementation of this proposal is a
follow-up Automaton that starts **after operator confirmation** of this
design. Until that confirmation, nothing here is scheduled, decomposed, or
built.

Main alternative for scope: treat peer placement as a general workload
scheduling subsystem (arbitrary job types, preemption, re-balancing).
Rejected — it re-opens the local-node design (v7.0.0 S1) with scheduler
semantics datawatch has deliberately not adopted; placement here is
"pick where a session/LLM call starts" and nothing more, and it would make
this doc depend on the concurrency doc's budgets in both directions instead
of being additive.

## Current state

What exists today, verified against the tree:

- `internal/observer/peer_registry.go` — the observer peer registry
  (`PeerEntry`: name, token hash, shape, `HostInfo`, peer `Version`,
  `RegisteredAt`, `LastPushAt`): `PeerRegistry.Register` issues and stores
  the peer token, `RecordPush` stores the latest `StatsResponse` snapshot
  and bumps `LastPushAt` (the periodic stats push path from the peer's
  `internal/observerpeer` client), `List`/`Get` expose the registry, and
  the registry persists to `<data_dir>/observer/peers.json`. This is a
  monitoring channel — it carries *metrics about* a peer, not a declaration
  that a peer *offers capacity for placement*.
- `internal/compute/node.go` — the local ComputeNode model: `Kind`
  (`ollama`, `openai-compat`), `RoutingMode` (`direct`,
  `docker-network`, `datawatch-proxy`), `RoutingDatawatchProxyConfig`
  (a pre-named `Peer` + `RemoteLLMName` — one hard-wired peer per node,
  not a placement decision), `DeclaredCapacity`, `Permissions`
  (`AllowedConsumers`/`DeniedConsumers` and `AllowsConsumer`),
  `NodePinnedModel` + `AllowModelSwap`, `MaxConcurrentSessions`,
  `SessionSlots`, `SchedulingPriority`, `MaintenanceWindows`, `Disabled`,
  `ObserverPeer` binding, and the `validNodeName` rule (no `/` allowed in
  a node name).
- `internal/capacity/ledger.go` — the admission ledger: `Ledger`,
  `Request`, `NodeChoice` (with `Priority`), `ExcludeNodes`, `PreferNode`
  as a re-launch tie-breaker, `Lease` (`Node` binding), and `PoolStatus`.
  Named pools include `node:<name>` per compute node; when
  `Request.NodeChoices` is set the ledger picks the least loaded candidate
  at dispatch time. Admission is purely local: "nodes" here are local
  registry names.
- `internal/federation/capabilities.go` — per-peer CBAC: `surface:action`
  capability strings, `CapabilityGroup` presets in `BuiltinGroups`, checked
  via `Resolve`/`Check`. Vocabulary already exists for reading peers' state
  (`observers:read`, `llms:read`, `compute:read`) but nothing expresses
  "this peer may *accept placed work for* a model".
- `internal/inference/proxy_router.go` — `ProxyRouter.Infer` POSTs an
  inference request to `<PeerURL>/api/proxy/llm/<remoteLLM>`: the only
  existing call-into-a-peer path, driven by a node's `datawatch-proxy`
  routing mode with the peer and remote LLM name hard-wired per node.
  The LLM registry (`internal/inference/llm.go`) carries
  `ComputeNodes` — an operator-ordered failover list of *local* node
  names — which the dispatcher (`internal/inference/dispatcher.go`)
  resolves at dispatch time.
- `internal/autonomous/models.go` — `PRD.OwnerPeer` (BL331) records which
  federation peer originated a PRD routed in via the channel surface; it
  is provenance, not placement.
- `docs/architecture.md` — lists the `federation` package (CBAC peer
  registry, `fedCap()` guards), the `observer` package (Shape A/B/C peers,
  cross-host correlation), and the `observerpeer` outbound push client.

Gap: there is no way to say "this model runs on node X of peer P" as a
*placement* choice. Today the only cross-peer LLM path is
`ProxyRouter.Infer` with one peer and one remote LLM name hard-wired per
node, `datawatch-proxy` style, and the capacity ledger cannot count, queue
against, or exclude a peer's capacity.

Alternative framing rejected for this section: a pure, neutral code audit
with no gap statement. Rejected because the audit would read as a
description of the world rather than a justification for the sections that
follow; the placement gap is the reason this proposal exists, and
stating it here keeps the proposal and the current state in one
document.

## Advertisement: how a peer declares placeable resources

Proposal.

A peer advertises its placeable nodes as a discrete, versioned
**placement advertisement**, distinct from — but pushed over the same
authenticated peer-push channel as — the observer stats snapshot
(`PeerRegistry.RecordPush`'s path):

1. **Unit of advertisement is a local ComputeNode, not the peer.** The
   peer selects its registry entries that are offerable: `Disabled` is
   false, not in `MaintenanceWindows`, `Kind` is LLM-API-capable
   (`ollama` or `openai-compat`), and the node's `Permissions`
   (`AllowsConsumer`) allow a `placement` consumer. Everything else is
   simply not advertised.

2. **What one advertisement line carries:** the local node name; node
   `Kind`; `DeclaredCapacity` and hardware (so the coordinator can do
   weak capability checks without dialing the peer); `NodePinnedModel`
   / `AllowModelSwap`; `SchedulingPriority`; `MaxConcurrentSessions` and
   `SessionSlots`; the peer's datawatch `Version` (already in
   `PeerEntry`); the current session count on that node (from the
   observer's in-process counts, same source as `StatsResponse`); node
   health; and a monotonically increasing advertisement sequence number
   per node. These are the privacy and trust limits of the surface: a
   peer advertises exactly its own node facts and nothing more, and the
   coordinator treats them as *claims*, verified only as far as the
   existing capability grants allow.

3. **What is never advertised:** **secrets** (no peer token, no API keys,
   no credentials of any kind) and **prompt contents** (no request or
   response payload ever leaves a peer in an advertisement or in a
   stats snapshot). Advertisements carry capacity and identity metadata
   only; the operator configures what a node offers, so the surface can
   stay constant and auditable. If the operator later asks for model
   *weights*, quantization, or workload history to be advertised, that is
   a new field with its own review — not an extension of this surface.

4. **Cadence and freshness.** Advertisements ride the existing periodic
   peer push (no new channel to operate) and are also sent at registry
   change (node add/remove/edit) and at startup. The coordinator caches
   them under `<data_dir>/federation/placement/<peer>.json`, marks a
   peer-scoped node **stale** when a TTL expires — proposed at three peer
   push intervals since the last advertisement with a newer sequence —
   and removes the entry from every candidate set the moment staleness
   trips. TTL and staleness are proposed here and finalized in the
   failure-mode section of this plan series; the proposal is that stale
   is always visible in status surfaces and is never silently routed to.

5. **Identity token.** A peer-scoped node is addressed everywhere by
   **`<peer-name>/<local-node-name>`**. This is the single token used in
   LLM entries, in capacity pools (the ledger gains `node:<peer>/<node>`
   pools whose external occupancy is the advertised session count), in
   leases (`Lease.Node`), and in status/hold views. The slash separates
   peer from node because neither component may contain one under
   `validNodeName`'s rules, so the token is unambiguous against local
   node names.

6. **Authorization.** Advertising is gated by a new capability
   (`placement:advertise`) and accepting placements by one
   (`placement:serve`), both following the existing `surface:action`
   naming and deliberately left out of the broad built-in groups (mirror:
   how `sessions:result_write` and `plugin:read`/`plugin:write` are kept
   out of the preset groups in
   `internal/federation/capabilities.go`). A peer that advertises but is
   not granted `placement:serve` is ignored at resolution time.

7. **Trust boundary.** Advertisement data is *operator-declared capacity
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
requires the coordinator to egress to every peer, to work behind NAT, and
to store live credentials for outbound calls — and it would let anyone
holding a read capability enumerate peers' node lists, wider than a
per-peer explicit "I offer this" claim.

## Naming: how an LLM entry or Automaton selects a peer and node

Proposal.

Selection stays operator-declared — datawatch's placement has always been
declarative (LLM entry → `ComputeNodes` list → dispatcher failover →
ledger admission). The proposal defines the concrete field shape on both
surfaces, the precedence rule, the unset behavior, and the mapping onto
the capacity machinery:

1. **Field shape on the LLM registry entry.** The existing
   `ComputeNodes` list in `internal/inference/llm.go` (*operator-ordered*
   local node names) becomes a **placement set**: the same list, still
   operator-ordered, whose entries may be either a local node name or a
   `<peer-name>/<local-node-name>` token. No new field on the LLM entry;
   a local-only entry is byte-identical to today's and behaves exactly as
   today. Validation rejects a peer token whose peer is not registered or
   whose advertisement is not currently fresh, so a misnamed peer fails
   at edit time, not at first dispatch. (The final surface name and any
   operator-visible labeling — "node", "endpoint", "placement" — is an
   operator-owned decision; **see Open questions**.)

2. **Field shape on the Automaton.** An Automaton's existing *preferred
   LLM entry* remains its primary selector, exactly as today. To a
   specific PRD task, the proposal adds one optional field on the
   Automaton's task spec: **`PlacementOverride`** — the same list-type as
   the LLM entry's placement set (local names and/or `<peer>/<node>`
   tokens), empty by default. It is the only new Automaton-side field in
   this design. (The JSON/field name is an operator-owned decision;
   **see Open questions** — the shape and semantics proposed here are
   fixed.)

3. **Precedence.** When both the LLM entry's placement set and the
   task-level `PlacementOverride` are set, **`PlacementOverride` wins
   entirely** (it replaces the entry's set; it does not merge or
   intersect). The entry's set is the default for everything the
   Automaton runs; the override is the narrow, explicit choice for one
   task. The entry still gates what models and consumers are allowed —
   the override selects among candidates, it does not grant anything the
   entry's permission matrix would refuse.

4. **Behavior when unset.** When `PlacementOverride` is unset and the LLM
   entry's placement set is local-only or empty, placement is **local only,
   unchanged**: the dispatcher resolves local nodes exactly as today,
   peer advertisements are simply absent from the candidate set, and no
   peer token is ever constructed or dialed. A peer that has never
   advertised is indistinguishable from no peer at all.

5. **Mapping onto the capacity request.** At dispatch time the surviving
   candidates (after the existing gates below) are handed to the admission
   ledger as **`Request.NodeChoices`** (`internal/capacity/ledger.go`),
   one `NodeChoice` per candidate with `Priority` derived from the
   operator's declared order and each node's `SchedulingPriority`. The
   ledger's existing least-loaded-first rule, `ExcludeNodes`, `PreferNode`
   re-launch stickiness, and fair wait queue all apply uniformly across
   local and peer candidates — a peer candidate occupies a
   `node:<peer>/<node>` pool whose occupancy is the advertised session
   count plus the coordinator's own in-flight leases for that token; a
   local one occupies `node:<name>`. The resolution log records the chosen
   token and, for audit symmetry, the relevant `PRD.OwnerPeer`
   (`internal/autonomous/models.go`) when the PRD originated on a peer.

6. **Gates, in existing order.** The resolution pipeline reuses, does not
   replace, the current gates, in the current order: expand the placement
   set (override if set, else entry) → drop stale peer candidates →
   drop candidates whose peer lacks `placement:serve` (CBAC) → drop
   candidates whose node `Kind`, pinned-model state, or advertised model
   set cannot serve the requested model (the same weak checks local
   dispatch performs, fed from the advertisement instead of the local
   registry) → hand the survivors to the ledger as `NodeChoices`.

7. **Failure behavior.** A dispatched call to a peer that dies mid-flight
   fails the *call* with a refusal naming the peer token; the next
   attempt (dispatcher retry within the same request, per its existing
   failover loop over candidates) may fall through to the next surviving
   candidate in the placement set. No automatic migration, no lease
   rewriting.

Main alternative: **resolver-side picking** — the LLM entry and the
Automaton name only the model or an Automaton-level "any peer" hint, and
the coordinator's resolver chooses the best peer/node at request time from
all advertisements (scored by load, hardware, version), with operators
only able to exclude. Rejected because it makes placement implicit and
unauditable: an operator reading the LLM entry or the Automaton task spec
could not say where that model will run, it silently couples every LLM
entry and every task to global state (a new peer appearing changes every
entry's behavior), and it conflicts with the pinned-model and permissions
model that treats "what runs where" as an operator decision. The ledger
already provides the deterministic *within-candidate-set* load balancing
the proposal needs (`NodeChoices` + least-loaded dispatch), so there is no
load-based reason to move the decision out of the operator's declared set.
