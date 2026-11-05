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

## Capacity ledger: counting peer slots

Proposal.

The central requirement for this whole section is **one ledger and no
second source of truth**: every admission decision — a local or a peer
slot, an LLM call or an autonomous task session — goes through the single
`Ledger` in `internal/capacity/ledger.go`. Neither the coordinator nor
the peer is allowed a private occupancy table that another side must
consult or sync.

1. **How advertised slots enter the ledger.** From the advertisement
   section above, the coordinator takes each advertised peer node's
   `SessionSlots` / `MaxConcurrentSessions` (the declared slot cap) and
   its current session count (the occupancy), and represents that peer
   node in the existing ledger as a `node:<peer-name>/<local-node-name>`
   pool — the same `node:<name>` shape local nodes already occupy, with
   the peer identity prefixed in the token. The ledger's own snapshot
   machinery (`PoolStatus` per pool, `Waiter` for queued requests, `Lease`
   bindings for admitted work) then covers the peer slot exactly as it
   covers a local one: a `PoolStatus` for the pool shows the advertised
   limit and occupancy, a `Lease` on it is a coordinator-side admission,
   and a `Waiter` is a coordinator request queued behind it. No parallel
   "peer capacity" structure is added anywhere; peer slots appear in the
   same `Ledger.Snapshot` output as local slots.

2. **How inflight caps and node choices apply to peer nodes.** The
   existing request machinery applies unchanged: a request names pools
   ("all must have room", all-or-nothing), and `Request.NodeChoices`
   may include peer nodes as ordinary `NodeChoice` entries with
   `Priority`. The ledger's dispatch — pick the least loaded candidate
   whose `node:<name>` pool has room, apply `ExcludeNodes`, break ties
   with `PreferNode` — treats a peer node choice identically to a local
   one. Inflight caps still come from the node's declared concurrency
   (`MaxConcurrentSessions` / `SessionSlots`), which for a peer node is
   the advertised value; when the peer node's pool is full the request
   queues as a `Waiter` or, if a cheaper path exists in the placement
   set, the existing failover semantics move it — the ledger does not
   special-case "this candidate is remote".

3. **How local and peer slots compete for the same pools.** A placement
   set is one candidate list. When a request's pools include both a
   local `node:a` and a peer `node:<peer>/b`, the ledger sees two
   candidate node pools with the same claim on the request's admission;
   `NodeChoice.Priority` (derived from operator order plus
   `SchedulingPriority`, as the naming section defines) decides which is
   tried first, and the least-loaded rule decides the rest. A peer slot
   and a local slot are in the same queue, counted by the same
   `PoolStatus`, and released by the same lease-return path — there is
   nothing that reserves local slots for local requests or peer slots
   for peer-originated ones beyond the operator's own placement sets.

4. **The peer holds no counter the control plane must reconcile
   against.** This is stated deliberately: the peer's own datawatch keeps
   its own local admission state for its own operator, but from the
   coordinator's point of view **the peer holds no counter that the
   control plane reads, diffs, syncs, or corrects**. The coordinator's
   `Ledger` is the single source of truth for *coordination-time*
   admission; the peer's live occupancy is only ever advisory input in
   the form of its advertisement (an occupancy number, refreshed on its
   own cadence), never an authoritative state machine the coordinator
   must reconcile against. Consequences: no lease is ever issued to a
   peer asking permission — the peer's own local ledger governs its
   local sessions; a peer that reports "full" simply fails or delays
   the work on its side, and the coordinator learns that through the
   normal request outcome (timeout, refusal, or a later advertisement),
   not through a reconciliation protocol; and there is no coordinator
   mechanism to detect or repair "drift" between a peer's self-reported
   occupancy and the coordinator's `PoolStatus`, because the two were
   never supposed to agree — one is a coordination claim, one is ground
   truth on the other machine.

Main alternative rejected: a **two-sided capability** where each side
reserves the other's slots (coordinator leases on the peer, peer leases
on the coordinator) so both ledgers agree. Rejected because it creates
exactly the second source of truth this section forbids: two ledgers
whose only consistency mechanism is the wire between them, every failure
mode (peer down, coordinator reboot, split view) becomes a
reconciliation problem, and the operator now has two places to look to
answer "which slots are free".

## Task lifecycle across the trust boundary

Proposal. An autonomous task placed on a peer goes through the same
phases a local task does — submit, run, track, (cancel), verify, result
— but exactly one machine does the work: the executing peer runs the
task under its own datawatch, and the coordinator does state-keeping
and reporting. The division below is per-phase, and each remote
operation is mapped to the Federation-Parity capability
(`CapAutonomousRead`, `CapAutonomousWrite`, `CapAutonomousRun`,
`CapAutonomousList` in `internal/federation/capabilities.go`) it needs,
per the project rule that every new autonomous write/run endpoint is
`fedCap`-guarded.

1. **Who is the owner, and where the worktree lives.** The executing
   peer is the **worktree owner**: it creates the git worktree
   (`EnsureWorktree` in `internal/autonomous/worktree.go`) on its own
   machine, against its own repo clone, and the worktree never crosses
   the trust boundary — no worktree, no working tree, no branch object
   is shipped to the coordinator. The coordinator's record of the placed
   task keeps `OwnerPeer` (`internal/autonomous/models.go`, the
   peer-ownership field already used for provenance) **set to the
   owning peer's name**, so every later status, audit, and cancel
   decision the coordinator makes can see "this task's machine state
   belongs to peer X, not to me." That field is the single source the
   coordinator uses to know which side of the boundary to talk to.

2. **Submit.** The coordinator (having admitted the task against the
   peer's `node:<peer>/<node>` pool in the shared `Ledger`) creates the
   task on the peer through the peer's autonomous task surface and
   starts it. On the peer side that endpoint is a write and a run —
   creating/mutating an autonomous task is `CapAutonomousWrite` and
   launching/starting work is `CapAutonomousRun` in
   `internal/federation/capabilities.go`, and the Federation-Parity
   rule requires the `fedCap` guard on both the create and the start
   path before any business logic runs. A task the coordinator admits
   but cannot submit (peer lost, capability denied) is rolled back the
   same way a local dispatch failure is: the lease is returned to the
   ledger and the request falls through to the next candidate or
   queues as a `Waiter`.

3. **Track.** The coordinator polls the peer's task/PRD state to keep
   its own view current (the peer does not push a live stream of task
   state); read-only status is `CapAutonomousRead` and listing the
   peer's autonomous tasks/PRDs is `CapAutonomousList`, both
   `fedCap`-guarded on the peer side. The coordinator's local record is
   still updated through its own `Manager`/`API` layer (the same code
   path it uses for local tasks), so every local consumer — the PWA,
   `comm`, REST, alerting — sees a placed task exactly like a local
   one, with `OwnerPeer` telling an operator the work is remote.

4. **Verification.** Verification scripts and the verifier run **on the
   owning peer, in the peer's own tree**, not on the coordinator — the
   same phase ordering local tasks get (script evidence before the
   verifier, `VerificationScript` → verdict in `Task.Verification`),
   executed against the worktree that only the peer can see. The
   coordinator accepts the verdict and the bounded result payload
   (verdict, output excerpt, changed-file list) as the peer's report
   and records it under `Task.Verification` on its own copy. The
   coordinator never re-runs the script against a local tree to
   double-check a peer's verdict: there is no local tree to check it
   against, and re-running it would turn a trusted-verdict into a
   reconciliation problem this design does not have a mechanism for —
   the operator's trust boundary is "the peer's datawatch reports
   faithfully," full stop.

5. **Worktree.** Per (1): owned by and created on the peer by
   `EnsureWorktree`'s normal local path; branch naming, worktree
   integrity, and cleanup behavior are all the peer's local concerns
   (`internal/autonomous/worktree*.go` unchanged). The only cross-
   boundary fact about the worktree is its existence and owner, carried
   by `OwnerPeer` plus the peer's own status report. Commit, push, or
   merge decisions on the peer's changes are governed by whatever the
   operator configured on the peer — this section does not add a
   cross-boundary git path.

6. **Result and changed files.** A completed task's result (verdict
   plus the changed-file list, bounded the same way failure-escalation
   records already bound `ChangedFiles`) travels back to the coordinator
   as a **result write**: the peer calls the coordinator's
   task-result surface, which on the coordinator's side is a state
   mutation and therefore guarded by `CapAutonomousWrite` — the
   mirror of the local `SetTaskFiles` path in
   `internal/autonomous/api.go` where a caller mutates a task's
   recorded files/result. The coordinator records the result on its
   local task copy exactly as it records a local task's result, so the
   "what did this task change" answer is read-local on the coordinator
   from then on. The coordinator does not pull the worktree or diff it
   later — the changed-file list in the result payload is the
   coordination-level truth about "what did the peer touch."

7. **Cancel.** A cancel the operator issues at the coordinator (REST/
   MCP/comm, through the existing `CancelTask` surface in
   `internal/autonomous/manager.go`) reaches the owning peer because
   `OwnerPeer` names it: the coordinator posts the cancel to the peer's
   autonomous task-cancel endpoint (a state mutation, guarded by
   `CapAutonomousWrite` on the peer side, and — where the peer kills
   an in-flight worker — its existing run-path guard
   `CapAutonomousRun`), then marks its own task copy as cancelled on
   return and returns the lease to the shared ledger. If the cancel
   message does not reach the peer (peer down), the coordinator still
   records the task's state as cancelled-with-remote-unconfirmed and
   releases its lease; it does not keep retrying against a dead peer —
   eventual state converges when the peer's task either completes and
   its result write is rejected by the coordinator's already-terminal
   state, or the peer cancels itself later. There is no distributed
   cancel protocol; single-writer state on each side, and the
   coordinator's record is what its operator sees.

8. **Partial results.** A task that finishes with some work done but
   not fully verified passes represents partial state the same way the
   local task state machine already does — the peer reports a terminal
   status from the existing `TaskStatus` set (`TaskCompleted`,
   `TaskFailed`, `TaskCancelled`, `TaskReviewNeeded`,
   `TaskVerifying`, in
   `internal/autonomous/models.go`) with whatever result payload exists
   at that moment: a partial changed-file list is a normal
   small/empty/partial list in the same field, and a missing-or-failed
   verdict is a missing-or-failed `Verification` on the same task
   copy. The coordinator does not invent a new "remote partial"
   status: partialness is already an orthogonal axis (terminal status
   + verdict + changed files), and placed tasks use the identical
   vocabulary. When a `TaskCancelled` or `TaskFailed` record also has
   changed files, that **is** the partial result, recorded identically
   to a local task in the same state. A peer that reports "completed"
   is a trust boundary, not a proof — the operator's existing
   verification discipline (review `Task.Verification` before accepting
   a result) is unchanged and applies equally to local and placed
   tasks.

9. **Capability mapping per remote operation.** Consolidated, per the
   Federation-Parity rule (every new write/run endpoint is
   `fedCap`-guarded before decoding or business logic):

   | Coordinator action            | Peer-side endpoint          | Capability (internal/federation/capabilities.go) |
   |---                              |---                           |---|
   | Submit: create the task        | autonomous task create       | `CapAutonomousWrite` ("autonomous:write") |
   | Submit: start/run the task     | autonomous task run/start    | `CapAutonomousRun` ("autonomous:run") |
   | Track: read task/PRD state     | autonomous task/PRD read     | `CapAutonomousRead` ("autonomous:read") |
   | Track: list peer autonomous    | autonomous listing           | `CapAutonomousList` ("autonomous:list") |
   | Cancel: cancel the peer task   | autonomous task cancel       | `CapAutonomousWrite` ("autonomous:write") |
   | Result: peer writes result/files back | coordinator's task-result write | `CapAutonomousWrite` ("autonomous:write") |

   Reading back the recorded result locally afterwards is plain
   coordinator-local state; it needs no federation capability.

Main alternative rejected: a **push-based status stream** where the
peer pushes live task state (every status change, every log line) to
the coordinator over a persistent connection, and the coordinator
becomes the "source of truth" in near-real-time. Rejected because it
moves the trust boundary into the coordinator's event store (the
coordinator would now hold a live, authoritative view of peer
internals it is not authorized to hold), it needs a resumable-stream
protocol and backpressure on top of the existing authenticated peer-
channel model, and it still does not remove the polling the
coordinator needs for anything the peer does not push — so it adds a
second truth (the stream) without removing the first (the state the
peer keeps). Poll-with-terminal-writes keeps exactly one authority per
side and one authoritative record (the coordinator's task copy), which
is what the capability-gated model above buys.

## Datawatch-proxy routing mode versus peer placement

Comparison of the two mechanisms that both put work on another datawatch
instance's hardware; they are not alternatives to each other and an
operator should pick per-feature by the axis below.

- **datawatch-proxy** is *inference-forwarding*: a local compute node
  configured with `RoutingMode` `datawatch-proxy` and
  `RoutingDatawatchProxyConfig` (a named `Peer` and a named
  `RemoteLLMName`, optional `TimeoutSeconds`, in
  `internal/compute/node.go`) sends *that node's LLM traffic* to the
  peer's LLM endpoint, where `ProxyRouter.Infer` in
  `internal/inference/proxy_router.go` executes it on the peer. The
  work is one LLM call; the peer is a fixed, configured destination;
  there is no task, no worktree, no verification, and the peer's
capacity is not counted by the coordinator's ledger — it is one more
  reachable endpoint for the dispatcher's existing failover list.

- **peer placement** (this doc) is *task/LLM placement with admission*:
  a peer advertised its nodes, the coordinator admitted the task or
  LLM call against the peer's `node:<peer>/<node>` pool in the shared
  `Ledger`, and then either (a) a task session runs entirely on the
  peer with the coordinator as control plane, or (b) an LLM call is
  dispatched to the peer's node through its advertised slot. The peer
  is a dynamic, capability-gated candidate; its capacity is counted
  locally by the ledger; and task lifecycle state (submit, track,
  cancel, verify, result) travels under Federation-Parity capabilities.

| Axis                        | datawatch-proxy                          | peer placement (this doc)                              |
|---                            |---                                       |---|
| What runs on the peer         | Only an LLM inference call             | A whole task session, or an admitted LLM call            |
| Peer chosen by                | Fixed in node config at edit time      | Placement set + `placement:serve` + ledger at dispatch   |
| Peer capacity counted         | No (failover order only)               | Yes — `node:<peer>/<node>` pool in the shared `Ledger`  |
| State that crosses boundary   | Request/response payloads              | Task state, verdict, changed files; no worktree, no logs|
| Trust/capability model        | Existing per-peer token (endpoint-level)| Federation-Parity `CapAutonomous*` per operation        |
| Cancellation path             | Abort the in-flight HTTP call          | Cancel reaches the owning peer via `OwnerPeer`          |
| Result ownership             | Return value to the caller             | Terminal status + verdict + changed files, recorded     |

**Which one a given feature should use.** The rule is: *does the
feature need a peer's capacity counted, an admitted lease, and/or a
task lifecycle — or just its model serving?* If it only needs the
peer's model to answer one call and the operator is happy to name a
single destination peer up front, **datawatch-proxy is the right
mechanism** and peer placement is overkill (it would add a ledger pool
and a capability grant for what is really one fixed HTTP route). If a
feature needs to *choose* a peer at dispatch time from a placement
set, needs a peer's slots to count against a shared ledger, needs the
task's worktree/verification to run on the peer with the coordinator
keeping state, or needs cancel/result to be representable across the
boundary, **peer placement is the right mechanism** and
`datawatch-proxy` cannot express it (it has no pool, no task state, no
cancel path, and no way to say "any peer that runs this model is
fine"). A feature that mixes both — most likely: an LLM entry that may
resolve to a peer node, plus the peer's model also reachable as a
plain proxy route — should use peer placement for the *placement*
decision and datawatch-proxy only for a fixed, non-placement fallback
route, never as a substitute for admission.
