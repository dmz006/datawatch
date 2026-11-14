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

## Failure modes

Proposal. Each mode below states how the condition is detected, what this
daemon (the coordinator's datawatch) does about it, and what the operator
sees. The peer's own daemon behaves as an ordinary local datawatch in all
cases; only the coordinator-side behavior is specified here. All modes are
handled with the same three mechanisms as the rest of this doc — the
freshness TTL from the advertisement section, the `OwnerPeer` single-
pointer from the task-lifecycle section, and the shared `Ledger` — and no
new protocol is introduced for any of them.

1. **Peer down.**
   *Detection:* the peer's pushes stop — `PeerEntry.LastPushAt`
   (`internal/observer/peer_registry.go`, bumped by `RecordPush`) stops
   advancing past the TTL, and the advertisement sequence numbers of its
   nodes stop increasing. The two signals are the same event from two
   views of one channel.
   *What this daemon does:* marks the peer and every peer-scoped node of
   it **stale** and removes them from every candidate set immediately
   (no dispatch to a stale node, per the advertisement section); in-flight
   LLM calls to the peer fail with a refusal that names the peer token;
   the dispatcher's existing failover loop continues with the next
   surviving candidate in the placement set, or the request queues as a
   `Waiter`; leases for work already submitted to the peer are returned to
   the ledger at their own normal timeout/failure path, not by any
   special reclamation; a task placed on the peer keeps its record with
   `OwnerPeer` set and its last-known state, and if the peer returns and
   its terminal write conflicts with a coordinator-side decision
   (e.g. a cancel), the already-terminal coordinator state wins, per mode
   5 below.
   *What the operator sees:* the peer shows as stale/down in the status
   surfaces (observer status and any placement view), the failed call or
   dispatch names the `<peer>/<node>` token in its refusal, and any task
   affected carries its last-known status — nothing silently disappears
   and nothing is auto-retried against a dead peer.

2. **Peer version below the minimum requirement.**
   *Detection:* the peer's datawatch `Version` — already carried in
   `PeerEntry` from registration and re-reported with each advertisement
   (advertisement section, item 2) — compared against the minimum version
   the placement protocol requires (a value proposed in Open questions,
   not fixed here).
   *What this daemon does:* treats the peer as **registered but
   placement-ineligible**: observer stats continue to be accepted and
   shown (that channel and its minimum are a separate decision, below),
   but the peer's nodes are never fresh in the placement sense — they are
   dropped at the "drop stale peer candidates" gate in the naming
   section's pipeline, and validation of an LLM entry that names such a
   peer token is refused at edit time ("fresh advertisement required")
   rather than failing at first dispatch.
   *What the operator sees:* the peer's status row carries an explicit
   "below placement minimum" marker with the peer's actual version, and
   an edit error that names both the peer and the minimum, so an operator
   who sees a refused placement can tell "version too old" from "peer
   down" or "capability not granted" at a glance.

3. **Partial results (a task ends with work done but not fully
   finished).**
   *Detection:* the peer reports a terminal `TaskStatus`
   (`internal/autonomous/models.go`) whose payload is partial by the same
   axes a local task uses — a changed-file list that is small, empty, or
   incomplete in the same field, a verdict that is missing or failed in
   `Task.Verification` (task-lifecycle section, item 8). There is no
   separate "remote partial" signal; partialness is the existing
   terminal-status + verdict + changed-files combination.
   *What this daemon does:* records the result exactly as it records a
   local task's result — the lease is already returned at completion, the
   record carries the partial changed-file list and the absent/failed
   verdict under the task's local copy, and the operator's existing
   review discipline (`Task.Verification` before accepting) applies
   unchanged. A completed verdict from a peer is a trust boundary, not a
   proof, for the same reason as locally.
   *What the operator sees:* the identical shape to a local partial task —
   a terminal status with a partial/empty changed-file list and a
   missing or failed verdict, plus `OwnerPeer` showing the work happened
   remotely. No new status vocabulary to learn.

4. **Stale advertisement (peer alive, node facts outdated).**
   *Detection:* no advertisement for a node carries a newer sequence
   number than the last received and the TTL (freshness window from the
   advertisement section, item 4) elapses, or an advertisement arrives
   with a *regressed* sequence number — both mean the cached facts are no
   longer trusted, even though the peer's other pushes may still be
   flowing (a node edited/removed on the peer without the push reaching
   the coordinator is indistinguishable from a node that changed and
   stopped being offered).
   *What this daemon does:* marks that one peer-scoped node stale — not
   the whole peer — drops it from candidate sets the moment staleness
   trips, and keeps it visible as stale in status. In-flight leases on
   the node are not force-revoked (the work is remote and running; the
   coordinator has no local handle on it); they end by their normal
   completion/timeout path. A newer advertisement with a higher sequence
   on the same node re-freshes it.
   *What the operator sees:* the status surface names the specific node
   as stale (distinct from the peer being down), with the last-received
   sequence and its local-clock receipt time, and dispatch refusals for
   that token say it is stale.

5. **Cancel racing completion.**
   *Detection:* an ordering race between two independent writes — the
   coordinator's cancel reaching the peer after the peer has already
   started its terminal state, or the peer's result write reaching the
   coordinator after the coordinator already recorded the cancel. Neither
   side can see the other's intent; the race is observable only as "two
   terminal events for one task."
   *What this daemon does:* keeps single-writer state on each side, per
   the task-lifecycle section (item 7): whichever event the coordinator
   records first makes its local task copy terminal; the later of the two
   is rejected against that already-terminal state (a result write after
   a recorded cancel is refused and logged, a cancel after a recorded
   completion likewise). The lease is released exactly once — by the
   event that is recorded first — so the race cannot double-release or
   double-occupy the pool.
   *What the operator sees:* one terminal status for the task, with a
   record of which event was recorded first and that the other arrived
   late and was rejected (the rejection is in the coordinator's own log,
   and the status is unambiguous in any case: the task is not in two
   states).

6. **Duplicate submission on retry (submit timed out, response lost).**
   *Detection:* the coordinator's submit (create + start, task-lifecycle
   section item 2) times out or its response is lost, so the coordinator
   cannot tell whether the peer created the task — and a blind retry
   could create a second task for the same logical work.
   *What this daemon does:* makes the submit idempotent by identity: the
   coordinator generates one task id for the placement attempt and sends
   it with the create; the peer stores that id as the task's identity and
   a retried create carrying the same id returns the existing task rather
   than creating a new one. The coordinator's lease accounting is
   unaffected — one lease per attempt, returned on rollback — so even
   without the peer-side dedup a double submit cannot occupy two ledger
   slots, and with it, exactly one task exists for the attempt.
   *What the operator sees:* one task in the coordinator's list and one
   on the peer side, for one attempt; the operator never sees two tasks
   that are "the same work."

7. **Network partition mid-task.**
   *Detection:* identical signal to the peer-down case (pushes stop,
   status polls stop answering) occurring while a task is running — the
   coordinator cannot distinguish "partitioned" from "peer crashed" and
   deliberately does not need to: both mean "I cannot talk to the
   owning machine."
   *What this daemon does:* stops polling with a bounded backoff (no
   request storm against an unreachable peer), keeps the task record
   intact with `OwnerPeer` set and its last-known state, and takes no
   ownership of the peer's working state — the work runs (or dies) on the
   peer under the peer's own daemon, independent of the partition. When
   connectivity returns, the peer's later terminal write and the
   coordinator's record converge by the same already-terminal rule as
   mode 5; if the peer and its task died, the coordinator's task stays
   at last-known state (no invented completion is recorded by the
   coordinator).
   *What the operator sees:* the task sits at its last-known status with
   the peer shown unreachable; there is no fabricated verdict either way,
   and the operator can cancel (recorded optimistically per the cancel
   semantics) or wait for the peer's real terminal state to arrive.

8. **Clock skew between daemons.**
   *Detection:* not detected as a fault per se — the design makes wall-
   clock disagreement irrelevant. Timestamps on advertisements may arrive
   ordered differently than they were produced, and the two machines'
   clocks may differ arbitrarily; the detection that *would* expose
   skew is an advertisement arriving with a regressed sequence number
   (handled under mode 4) or a staleness decision landing early/late by
   the skew amount (visible in status, below).
   *What this daemon does:* never uses a peer-reported wall clock for any
   admission, freshness, or ordering decision: staleness and TTL are
   computed against the *receiving* coordinator's local clock at receipt
   time, and ordering between advertisements of the same node is settled
   by the monotonic per-node sequence number (advertisement section,
   item 2), not by timestamps. A peer clock that is wrong can only make
   its own stats look odd, not change who is admitted where.
   *What the operator sees:* nothing directly attributable to clock skew;
   the status surface shows receipt times in the coordinator's local
   clock (which are the times that ever matter for placement), and
   worst-case skew only bounds how early or late a stale node is
   evicted.

## Path to a fully federated resource peer

Proposal. The route for a second machine (the peer) to share its
resources with the coordinator is **configuration-only at every stage**:
no rebuild on the peer, no new service, no cluster protocol — each stage
is the existing peer registration from `internal/observer/peer_registry.go`
(`PeerRegistry.Register`: operator-initiated, token minted and given to
the peer once, persisted to `<data_dir>/observer/peers.json`, stats
refreshed by the peer's `internal/observerpeer` push through
`RecordPush`) plus one more explicit grant or configuration. Each stage is
gated on the previous one and independently reversible: removing a grant
stops that stage's traffic and leaves every earlier stage intact.

1. **Stage 0 — register the peer (already exists).** Operator registers
   the peer on the coordinator and the peer is configured with the
   coordinator URL and its minted token. *Gate:* first successful push —
   `RecordPush` has stamped a fresh `LastPushAt`. *Result:* the peer
   appears in observer status with its `HostInfo` and `Version`; nothing
   is placed on it yet — this stage carries metrics *about* the peer, not
   an offer of capacity, which is exactly the boundary the current-state
   section draws.
2. **Stage 1 — advertise local nodes.** On the peer, the operator marks
   which of its local `ComputeNode` entries are offerable (not
   `Disabled`, outside `MaintenanceWindows`, `AllowsConsumer` for the
   placement consumer) — pure local config on the peer. On the
   coordinator, the operator grants `placement:advertise` to this peer
   in the federation capability set. *Gate:* the peer's version meets the
   placement minimum and at least one node's advertisement is fresh
   (sequence advancing within the TTL). *Result:* the peer's nodes appear
   in the shared `Ledger` as `node:<peer>/<node>` pools with their
   advertised limits and occupancy — the peer is now *countable*.
3. **Stage 2 — place admitted LLM calls.** The operator extends LLM
   entries' placement sets (naming section) to include the peer's node
   tokens and grants `placement:serve` to the peer. *Gate:* the
   peer/node passes the weak checks of the resolution pipeline (kind,
   pinned-model state, advertised model set can serve the requested
   model) and the ledger has room in the pool. *Result:* LLM calls may
   dispatch to the peer's node with a real lease against its advertised
   slots — the peer's model is now usable *with admission*, not just as a
   fixed `datawatch-proxy` route.
4. **Stage 3 — place autonomous task sessions.** The operator grants the
   coordinator the autonomous capability set on the peer
   (`CapAutonomousRead`, `CapAutonomousList`, `CapAutonomousWrite`,
   `CapAutonomousRun`), and the peer side is configured for autonomous
   work the way a local instance runs it today: repo available for
   `EnsureWorktree`, verification scripts runnable, workers configured.
   *Gate:* submit + track proven in a guided run (one task successfully
   submitted, tracked to a terminal state with a recorded verdict —
   i.e. the whole task-lifecycle table exercised end to end at least
   once). *Result:* the machine is a **fully federated resource peer** —
   its compute is counted, its models serve admitted calls, and whole
   task sessions execute on it while the coordinator keeps state. Every
   stage before this one remains available and independent of it.

## Parity surface

Per the parity-surface rule (AGENT.md), this proposal enumerates all
channels it touches from the full set. The design is a proposal (no
implementation yet), so "Include" means the surface gets the feature
when it is built; excluded surfaces state the reason.

| Surface        | Include / Exclude | What the surface gets (or why excluded) |
|---              |---                |---|
| REST           | Include           | The placement surfaces: placement-set editing on LLM entries, `node:<peer>/<node>` pool/status reads, peer registration and capability grants (`fedCap`-guarded per the Federation-Parity rule), submit/track/cancel/result for placed tasks. Mobile clients and scripts parse exactly this contract. |
| MCP            | Include           | The status and configuration counterparts of the REST surfaces (placement pools, peer freshness, per-token status), matching how existing autonomous and compute status is exposed via MCP; the capability-accessibility rule treats MCP as a first-class config/status channel. |
| CLI            | Include           | `datawatch config set` round-trips for placement-set and capability fields (Configuration Accessibility Rule), plus peer register/list/status for operators on a shell — the only channel that can do the one-time token handoff without a browser. |
| comm channel   | Include           | `configure <key>=<value>` for the same fields and a status query for peer placement, so an operator can check "is that peer's node still fresh?" from Signal/Telegram without opening the PWA. |
| YAML/config    | Include           | Placement sets on LLM entries, `PlacementOverride` on task specs, and per-peer capability grants, each with a commented field in the config template per the Configuration Accessibility Rule. |
| PWA            | Include           | LLM entry editor accepts local names and `<peer>/<node>` tokens with edit-time validation, placement status shows each pool (local and peer-scoped) with freshness, and placed tasks render like local ones with `OwnerPeer` visible. Any of these operator-visible changes trigger the Mobile-Parity Rule at implementation time. |
| Android        | Include           | Capability parity via the shared REST/WS contracts above; idiomatic delivery (no web-specific affordances assumed). **A `datawatch-app` issue is required when the feature is built** (Mobile-Parity Rule): the new placement-set editor affordances, status chips, and any new strings get filed against the Compose Multiplatform client in the same release that ships them on the PWA. |
| iPhone/iOS     | Include           | Same parity clause as Android: parity standard is PWA == Android == iPhone, capability parity required with native (SwiftUI) delivery. **A `datawatch-app` issue is required when the feature is built** (Mobile-Parity Rule) so the iOS client tracks the same API contract and affordances; excluded from *this doc* only because this doc implements nothing. |

## Federation rules and controls

Placement moves *operator* work onto a *peer* machine across an untrusted
boundary, so the capability model, the policy, and the audit trail are one
control plane: no code path may place, refuse, shadow, or cancel
differently from what this section describes. Everything here is operator
config — the Configuration Accessibility Rule applies (every key readable
and writable on REST, MCP, CLI, the comm channel, in the YAML/config
template, and in the web UI) — is deny-by-default (every default below
reads "nothing is placed, anywhere"), and is shadow-first (the first
*live* decision any peer ever executes follows shadow entries an operator
has actually watched).

### New placement capabilities

Two capabilities join the existing `surface:action` vocabulary in
`internal/federation/capabilities.go` (whose `Resolve()`/`Check()`
chokepoint every other surface already funnels through):

| Capability | Gates |
|---|---|
| `placement:advertise` | The peer may *contribute* placement material: its nodes may enter this coordinator's candidate sets. Inbound direction. A peer without the grant is unaffected in everything else — its session tokens, its reads, its own work all keep running — only its nodes stay invisible to placement, and the dropped advertisement is an audited decision, not a silence. |
| `placement:submit` | The coordinator may *submit* work to the peer: the session-start write to a `<peer>/<node>` node, and the cancel write back to the same peer (one who may start may also stop, per the task-lifecycle section). Outbound direction; the reciprocal grant lives in the *other* daemon's peer list for *this* one — per peer, per direction, two independent decisions. |

Both are checked on the coordinator side at decision time against the
acting credential (admin bypasses; session tokens and peer tokens go
through the same `Check()`), and independently re-checked on the peer side
when the submit write arrives (peer-side enforcement below) — a grant here
authorizes the coordinator's act, it does not force the peer's compliance.

**Built-in group placement** (following the file's own least-privilege
precedent — `federation:fingerprint`, `plugin:read`/`plugin:write`, and
`alerts:write` are each deliberately in `allCaps`/`full-control` *only*):

- `full-control`: both capabilities added to `allCaps`.
- New builtin group **`placement-partner`**: exactly `placement:submit` +
  `placement:advertise` — the convenience grant for a peer that both
  advertises and receives.
- **No other builtin group** (not `federation-peer`, which keeps its v9.0.0
  SEC-009 default of health + own-entry only) gains either capability by
   default. A newly registered peer is placement-invisible in both directions
   until an operator grants — open question 8's recommendation, now enforced
   in the capability layer instead of just intended. Advertise-only or
   submit-only postures stay available as single-capability grants, so the
   builtins can grant each direction independently ("per direction" is a
   grant-level property, not a config afterthought).

### Tool-to-capability table

`internal/federation/mcp_tool_caps.go` (the `MCPToolCap` map, enforced at
the `POST /api/mcp/call` chokepoint, with the `internal/server/
route_caps_a3_test.go` completeness test) gains the placement control
surface, mirroring the existing `alert_rule_*` convention of gating a
control plane on `config:read`/`config:write` rather than minting another
read capability:

| MCP tool | Capability | Note |
|---|---|---|
| `placement_status` | `config:read` | Switches, shadow entries, approvals, per-peer effective policy, candidate-set freshness |
| `placement_policy_get` | `config:read` | The per-peer allow/deny + limits set |
| `placement_policy_set` | `config:write` | Allow/deny lists, limits, per-peer switch |
| `placement_master_set` | `config:write` | The master switch (off by default) |
| `placement_shadow_set` | `config:write` | Flip a *specific peer* into or out of shadow — per peer, deliberately no global version |
| `placement_approve` | `config:write` | Record the first-live-placement approval for a peer |
| `placement_cancel` | `placement:submit` | The only placement *action* in the table — canceling a placed task is a submission-side act, the capability of the thing it undoes (the same pairing as `sessions:kill` on a session its holder could have started) |

REST, CLI, comm-channel, and MCP all resolve the *same* capability for
the same logical act (the comm-channel `placement cancel` and the REST
cancel route are one guard), which is the Federation-Parity rule's
"every remote write/run operation maps to a capability with a guard
requirement" satisfied with one chokepoint in `internal/federation` and no
surface re-implementing policy.

### Per-peer allow/deny and limits

```yaml
federation:
  placement:
    enabled: false          # master switch; default off — nothing placed, anywhere
    shadow: true            # shadow mode while enabled: log would-be placements, place nothing
    peers:
      <peer-id>:
        enabled: false      # per-peer switch; default off even with the master on
        approved: false     # first-live-placement approval recorded? an audited act, not a hand-edit
        nodes:    { allow: [], deny: [] }   # node names from the peer's advertisement
        models:   { allow: [], deny: [] }   # LLM ids, not raw token pools
        automata: { allow: [], deny: [] }   # Automaton names, or `kind:<kind>` tokens for whole kinds
        projects: { allow: [], deny: [] }   # project slugs
        limits:
          max_concurrent_tasks: 2   # simultaneous live-placed sessions on this peer; default 2
          max_slot_share: 0.25      # max fraction of that peer's advertised concurrency any one coordinator may claim; default 0.25
          max_task_hours: 8         # per-task runtime ceiling before the task is a hung-candidate (task-lifecycle section); default 8
          cost_class: high          # admit task models whose cost tier is low, mid, or <= high (default: no cost gate until an operator tightens it)
```

**Allow/deny semantics** (identical across the four axes): a non-empty
`allow` list restricts to its entries; a non-empty `deny` list always wins
over `allow` for the entries it names; *both empty means no restriction on
this axis* — denial-by-default lives at the peer-entry level (`enabled:
false`) and in the switches, and the per-axis defaults must not silently
re-deney a peer the operator already enabled, which would make "allow
anything but `kind:billing`" read as "allow nothing". Operators start the
first entry with one axis populated (usually `models.deny`) and widen
deliberately, one axis at a time, with each change a `config:write`-guarded,
audit-logged policy write.

**Limits** are evaluated at decision time against the peer's *current*
advertised concurrency and the task's projected cost, and a limit refusal
names which limit (concurrent, share, hours, or cost) in the audit entry
— an operator reading the feed cannot tell at a glance whose gate closed
unless the refusal says. `max_slot_share` and `max_concurrent_tasks` are
deliberately finite by default: a peer that silently becomes a 50-task
destination is an operator surprise, and the override is one policy write
with an audit line, so the default costs nothing to raise when a dedicated
peer (an operator's own second instance, say) should take everything.

### The decision gate (order is the audit)

A candidate (task, model, `<peer>/<node>`) is placed on peer P **iff every**
step passes, in this order — the *first failing* step is the audit
entry's `deciding_rule`, and the order is part of the contract so that
refusals read the same in the feed, the PWA, the CLI, and the comm channel:

1. **Master switch** on (`federation.placement.enabled`).
2. **Per-peer switch** on (`peers.<P>.enabled`).
3. **Capability**: this daemon holds `placement:submit` for P, and P held
   `placement:advertise` when its advertisement was accepted (also
   re-checked at decision time — a revocation pulls the peer's nodes out
   of candidate sets within one advertisement TTL, the same freshness
   window as a dead peer, rather than waiting for a manual purge).
4. **Allow/deny** on all four axes (the semantics above), per axis.
5. **Limits**: concurrent, share, task-hours, cost class.
6. **Approval**: if this would be the peer's first-ever *live* submission,
   the operator's prior approval act must exist (shadow submissions never
   require it).

### Shadow mode (default on while placement is newly enabled)

With the master on and `shadow: true` — the posture an operator adopts on
first enable, and the default for any peer whose per-entry `enabled` flips
`false → true` without an explicit `placement_shadow_set … off` act — the
coordinator runs the full decision gate above on every candidate as if it
were live, then **logs the would-be decision and submits nothing**: the
audit entry records the candidate, the passing path or the first failing
rule, and a `placement:shadow` marker; no session starts, no token is
spent on placement transport, and the peer never sees the task. The shadow
entries are the review surface: the PWA card, the
`datawatch placement status` CLI view, and the comm-channel
`placement status` query list them per peer with a deciding-rule breakdown
("14 would-be placements to `peer-2` over the last 7 days: 11 would pass
to `node-b`, 3 refused on `automata.deny: kind:billing`"), which is the
evidence the operator needs before the two acts that follow. Flipping a
specific peer out of shadow is `placement_shadow_set <peer> off` — a
per-peer, audit-logged act with the peer's name in it — and there is
deliberately **no global shadow-off**, because "I watched `peer-2` for a
week, now turn everything on" is two different decisions bundled into one
click. Peers go live one at a time; enabling one changes nothing about
the others' shadow posture.

### First-live-placement approval

The *first live submission to a given peer* requires an explicit operator
act, separate from any switch: `datawatch placement approve <peer>
(config:write)`, the MCP `placement_approve`, the REST
`POST /api/federation/placement/<peer>/approved`, and the comm
`placement approve <peer>` channel are all the same act on the same guard,
and each is itself an audit-logged write that sets
`peers.<P>.approved: true` with the actor and timestamp. The act is
deliberately separate from the per-peer switch and from shadow-off:
operators who enable a peer, watch its shadow entries, and then approve it
are doing three auditable decisions instead of one, and a peer can sit in
shadow indefinitely — approved or not — without ever having placed a
single live task, which is the posture a cautious operator should be able
to hold forever without a config contradiction. Approval is per **peer**
(not per task, not per Automaton — open decision 4 below weighs the
granularity); revoking a peer's approval is a policy write that takes
effect on the peer's next *candidate*, and already-running tasks keep
cancel rights per the task-lifecycle section's single best-effort write.

### Audit entries (every placement, refusal, cancellation)

Every placement *attempt* — placed, refused by any gate step, shadowed,
withdrawn as stale, canceled, or rejected on the peer side — is one audit
entry, in the formats the Audit Logging Rule (`AGENT.md`) requires:
JSON-lines by default and CEF for SIEM forwarding (the escape rules and
severity assignments in that rule apply, and the CEF mapping is per-event
as it is for every other federation write-class event):

```
{"event":"placement:refused", "peer":"peer-2", "node":"node-b",
 "run":"task-17", "model":"claude-opus-4-8", "deciding_rule":"automata.deny",
 "failed_entry":"kind:billing", "actor":"operator@…",
 "ts":"2026-…"}
{"event":"placement:shadow", "peer":"peer-2", "node":"node-b",
 "run":"task-18", "deciding_rule":"gate_passed", "would_place":true, "ts":"…"}
{"event":"placement:submitted", "peer":"peer-2", "node":"node-b",
 "run":"task-19", "task_id":"…", "deciding_rule":"none", "ts":"…"}
{"event":"placement:rejected_by_peer", "peer":"peer-2", "node":"node-b",
 "run":"task-19", "peer_reason":"local-concurrency", "ts":"…"}
{"event":"placement:cancelled", "peer":"peer-2", "node":"node-b",
 "run":"task-19", "remote_confirmed":true|false, "ts":"…"}
```

The `deciding_rule` field is the point of the audit requirement: a refusal
is never just "denied" — it names *which control* denied, so an operator
reading the feed can distinguish a capability gap, a deny-list hit,
a limit hit, an approval-pending refusal, and a peer's own local denial
without reading the daemon's source. It is the same field the PWA, the
CLI, and the comm channel surface when an operator asks *why* a candidate
was or wasn't placed, and it is what makes shadow mode a reviewable record
rather than a log dump.

### Control surfaces (parity, same act, same guard)

| Surface | Entry point | Capability (same act, same guard as the MCP table) |
|---|---|---|
| REST | `GET /api/federation/placement/status`; `GET/PUT …/policy`; `PUT …/master`; `PUT …/shadow/<peer>`; `POST …/approve/<peer>`; `POST …/cancel/<run>` | `config:read` for the reads, `config:write` for the settings/switch/approval writes, `placement:submit` for cancel (the table's `placement_cancel`) |
| MCP | The seven tools in the table above | per tool, as the table says |
| CLI | `datawatch placement status \| policy <set\|get> \| master <on\|off> \| shadow <peer> <on\|off> \| approve <peer> \| cancel <run>` | same mapping: status/policy → `config:read`; the rest by their tool's capability |
| comm channel | `placement status`, `placement policy set …`, `placement master off`, `placement shadow <peer> off`, `placement approve <peer>`, `placement cancel <run>` — enough to run the whole control plane over Signal/Telegram without opening the PWA | same mapping |
| YAML/config | The `federation.placement.*` block above, every field present-and-commented in the template (Configuration Accessibility Rule); `approved` is the same field whether hand-edited or set via the audited act — hand-editing it *is* the act, and the audit line for it says "policy write" rather than "approve" so the two entry paths are distinguishable in the feed | n/a |
| PWA | Federation → Placement card: per-peer switch, shadow entry list with deciding-rule breakdown, approval state and actor, candidate-set freshness per peer; mobile parity issue required at build time (Mobile-Parity Rule in the Parity surface above) | n/a |

Parity here is the requirement, not the aspiration: the *same logical act*
resolved on any surface holds the *same capability* (`MCPToolCap` /
`internal/federation` chokepoint), so a surface that forgets a guard is
caught by the route-completeness test, not discovered by an operator in
production at 2 a.m. reading audit lines that don't match what they typed
on the PWA.

### What the peer enforces independently

The coordinator's gate is necessary, not sufficient — the peer is the
machine that receives the work and remains the single writer for its own
bytes (the worktree decision's premise). On the peer side of the same
submit write, independently of anything the coordinator decided:

- **Token and registry check**: the session-start write arrives under the
  peer's own peer-scoped node token (the token section's model), and the
  peer's local registry is authoritative for whether that token is still
  valid, which node it binds, and whether that node is still *enabled on its
  own side* — a grant the coordinator believes exists is no more binding
  than the peer's own registry says at the moment the write arrives, and
  the peer's refusal is a `placement:rejected_by_peer` the coordinator
  records with the peer's stated reason.
- **Local concurrency and budget**: the peer's own lane caps, its own
  autonomous-concurrency ceiling (open question 3's answer — peer sessions
  count toward the *global* cap, not local-lane caps), its own model token
  budget, and its own cost policy all apply to a received task exactly as
  they apply to any local task on that machine; a peer at capacity simply
  refuses, and the coordinator's ledger records the refusal rather than a
  silent retry elsewhere (a retry to a *different* peer is a new candidate
  evaluation, a new decision gate run, a new audit entry — not a re-use of
  the first one).
- **Its own trust posture**: a peer that is also a coordinator for its own
  fleet enforces *this same section* on its own nodes with its own peer
  list, its own switches, its own approvals, its own audit trail — there
  is no "a coordinator already said yes, so skip the gate" fast path
  anywhere, which is the Federation-Parity rule's untrusted-caller premise
  holding on both sides of the boundary in both directions, and the reason
  a peer with `placement:submit` granted *by* the coordinator is a
  different, independently-gated relationship than a peer with that
  capability *on* the coordinator's own peer list.

Deny-by-default is thus **structural**, not just a config default: a
submission the coordinator's six-step gate passes can still fail the
peer's own checks, and neither side's audit trail needs to trust the
other's to explain why the task isn't running — the operator reads two
lines (one `placement:submitted`, one
`placement:rejected_by_peer: local-concurrency`) and knows exactly which
machine said no and on which rule, without a second support pass.

### Open decisions (each with a recommendation)

Per this document's convention for operator-owned decisions, each is
stated with its recommendation so the design can be built as-written if
the operator agrees with all of them; open question 9 below is the
index entry for this list.

1. **What `max_concurrent_tasks` should default to per peer.**
   **Recommendation:** 2. Two is enough to prove the full pipeline
   (placement, worktree, run, verify, result, cancel) works across the
   boundary without the peer becoming a de-facto fleet, and the override
   is one audited `placement_policy_set` write — so the default's
   conservatism costs nothing to raise when a dedicated peer should take
   everything, and it costs an operator's own surprise to have skipped it.
2. **What `max_slot_share` should default to per peer.**
   **Recommendation:** 0.25. A peer's advertised concurrency was declared
   *by that peer's operator* as theirs; 25% is a visible, overridable
   fraction that keeps any one coordinator from monopolizing a shared
   peer's slots by default, and a dedicated peer can be set to 1.0
   explicitly — the honest statement of "this machine is fully mine" —
   rather than left implicit in a default that reads as shared.
3. **What `cost_class` means as a value and where it comes from.**
   **Recommendation:** `cost_class` is three values ordered low < mid <
   high, with `high` as the admission default so an untouched peer gate
   is the open end and tightening is always a deliberate act. The source
   of a model's class is the per-backend rate the daemon already prices
   from — `session.CostRate` (USD per 1K input/output tokens per backend,
   the table the cost report and `AddUsage` already read, already
   operator-overridable) — so a model's class is *"its rate is at or
   below this class's ceiling"*: the operator sets the two class
   ceilings once in the placement config (present-and-commented in the
   template, per the Configuration Accessibility Rule), and no new
   per-entry cost field has to be kept in sync with the registry, the
   PWA entry editor, or the parity surfaces — that sync burden is the
   reason this design does not mint a separate class field per LLM
   entry, even though a `cost_hint`-style field on the entry is the more
   obvious shape at first glance. A peer whose operator wants every
   class admitted leaves `cost_class: high`; a peer that should only
   take low-class (local) work sets `cost_class: low`. This is a
   design-level constraint on admission, not a budget on execution —
   the runtime token budget stays where it is (the cost-report and
   burn-guard territory); what changes is that a peer's cost posture
   is an auditable part of its placement trust surface, not an afterthought read out of the PWA.
   **Fallback if the operator wants per-model control instead:** the
   placement config also accepts an explicit `cost_class_overrides`
   map of LLM-id → class (low/mid/high) that wins over the ceiling
   derivation, so a model whose rate looks mid but whose traffic is
   actually low (a self-hosted copy, say) can be admitted to a
   low-class peer without changing its rate.
4. **Approval granularity: per peer vs. per (peer, Automaton kind).**
   **Recommendation:** per peer, one act. Approval is a trust decision
   about *the machine and its operator*; the *work* is already split by
   the allow/deny axes (which include `kind:<kind>` tokens). Two axes
   doing different jobs beats one axis doing both, and per-kind approval
   is available later as a filter on the `approved` record without
   changing the gate's six-step shape — it is not needed to make the
   first deployment safe, only to make the second one more granular, and
   that granularity should earn its complexity by being asked for rather
   than assumed.
5. **Whether shadow-off is per peer or also global.**
   **Recommendation:** per peer only — the one-button "watched it, now
   on for all" is the exact path an operator takes at 2 a.m. under incident
   pressure, and the per-peer `placement_shadow_set <peer> off` act with
   the peer's name in it is the same decision with a name the audit trail
   can hold the operator to. A global off could be added, but that
   addition is the thing that makes a peer go live on the strength of a
   click, and this section's whole premise is that peers go live on the
   strength of a *named, per-peer, audited* act.

## Open questions

Each is an operator-owned decision. The recommended answer is stated with
its reason so the design can be built as-written if the operator agrees
with every recommendation.

1. What advertisement cadence and TTL should the coordinator use —
   should advertisements ride the existing peer-push cadence and a peer
   node go stale after three missed pushes, or should placement get its
   own faster, dedicated ping?
   **Recommendation:** ride the existing push cadence with a TTL of three
   missed pushes (the advertisement section's proposal). The peer already
   operates one push channel; a second faster one is a second thing to
   operate and to secure, while a three-push TTL is short enough that a
   dead peer leaves every candidate set within minutes, not hours.
2. What naming syntax should a peer-scoped node use — a single dotted or
   slash-joined `peer.node` token inside the existing placement-set list,
   or separate `peer` and `node` fields on the LLM entry and task spec?
   **Recommendation:** keep one placement token (the proposed
   `<peer>/<node>`; dot is an acceptable spelling if the operator
   prefers it) inside the existing operator-ordered list. A token keeps
   entries byte-identical to today when local-only, stays unambiguous
   against local node names (neither component may contain the
   separator under `validNodeName`), and composes directly with the
   ledger's `node:<…>` pool names; two fields would double every
   validation, precedence, and serialization surface for zero new
   expressiveness.
3. Should peer slots count toward the existing lane caps (the
   concurrency limits that already bound how many autonomous tasks run
   locally), or should they sit outside them?
   **Recommendation:** peer slots should count toward the *global*
   autonomous-concurrency cap but not toward per-local-lane limits. The
   local lanes bound local workers — a peer session occupies none of
   them — but the coordinator's own state (records, polling, leases,
   audits) is real cost per placed task, so a pure local exemption would
   let an operator's fleet grow unbounded coordinator-side load. Counting
   peer sessions in the global cap keeps that bounded while lane
   semantics stay about local capacity.
4. Where should the placed task's worktree live — on the peer, or created
   on the control plane from a copy of the peer's tree?
   **Recommendation:** on the peer, exactly as the task-lifecycle section
   proposes (`EnsureWorktree` runs on the peer; the coordinator only
   records `OwnerPeer`). A worktree is a view of a repo on a machine; a
   "copy of the tree" on the coordinator means the same content living
   on two machines with a sync question that has no honest answer over an
   untrusted boundary — keeping the worktree on the peer removes the
   question entirely and leaves each side the single writer for its own
   bytes.
5. What cancel semantics should cross the boundary — a single best-effort
   cancel write (coordinator records the cancel as authoritative for its
   operator whether or not the peer acks), or a two-phase cancel with
   ack/timeout before the coordinator's record is allowed to go
   terminal?
   **Recommendation:** the single best-effort write with
   cancelled-with-remote-unconfirmed when the peer is unreachable (the
   task-lifecycle section's proposal). The peer is the only machine that
   can actually stop the work, so a two-phase protocol buys a
   confirmation while hiding that the work may keep running — worse than
   an honest "cancelled here, unconfirmed there" record; single-writer
   state on each side keeps the ledger consistent in all four orderings
   without a protocol.
6. Where should verification run — on the owning peer, against the
   worktree it created, or on the coordinator against a fetched copy of
   the peer's changes?
   **Recommendation:** on the owning peer, in its own tree (the
   task-lifecycle section's proposal). The verification scripts are
   written against the tree and environment the task ran in; re-running
   them elsewhere requires shipping the tree (defeating the worktree
   decision above), and the coordinator has no local state to check a
   peer's verdict against in the first place. The trust model stays
   "the peer reports faithfully," which is the same trust model as
   trusting a local task's scripts — applied to the machine where the
   work actually happened.
7. What minimum peer version must a peer meet to be placement-eligible —
   the version at feature release plus one semver level, the exact same
   minor version, or whatever the operator declares per peer?
   **Recommendation:** the version at feature release plus one semver
   level (strictly newer than or equal to the release tag the feature
   ships in), read from `PeerEntry.Version` which is already captured at
   registration and re-reported each advertisement. The placement wire
   contract (advertisement fields, result write, capability strings)
   evolves with releases, so "any version" would admit peers that speak
   an older contract; per-peer operator override is already available in
   the general capability grant and is not needed as a special
   exception to the version floor.
8. What should be the default trust grant for a newly registered peer —
   no placement capabilities at all, or a conservative default set
   (e.g. advertise-only) that the operator can then widen or revoke?
   **Recommendation:** no placement grant by default. The Federation-
   Parity rule treats peers as untrusted callers by default, and the
   existing built-in capability groups already keep the narrow
   `*-serve`/`advertise`-style capabilities out of the presets on
   purpose — a new peer should be invisible to placement until an
   operator explicitly decides that *this* peer may be placed on, and
   deny-by-default makes the audit story one sentence instead of a diff
   against implicit grants. The cost is one extra grant step at
   registration, which is the point. (The Federation rules and controls
   section makes this structural: only `full-control` and the new narrow
   `placement-partner` builtin carry either `placement:*` capability, so
   a `federation-peer` default grant cannot silently open either
   direction.)
9. What defaults should ship for the placement control plane — the
   Federation rules and controls section's five open decisions
   (`max_concurrent_tasks` default, `max_slot_share` default, the
   `cost_class` value and its source, approval granularity, and
   per-peer-only shadow-off), each of which carries its own
   Recommendation in that section; this entry is the index so this list
   remains the single place operator decisions are enumerated. (Per the
   section's conventions, each is already recommended in place — the
   recommendation is not deferred to this list.)
