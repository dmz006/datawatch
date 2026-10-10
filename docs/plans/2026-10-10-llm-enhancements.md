# Plan: LLM Enhancements — Role-Aware Scheduling, Federated Capacity, Council-as-Reusable-Definition, Usage/Cost Tracking (BL405)

- **Date**: 2026-10-10
- **Version at planning**: v9.0.10
- **Status**: Planned — not started

## Context

Source: `/home/dmz/workspace/llm-research/docs/07-datawatch-improvements.md`
(dated 2026-09-30, pre-v9.0.0) — Part A (7 routing/tiering/compute-balancing
proposals) and Part B (5 usage/cost/performance-tracking proposals). The
source doc is stale against the live system in both directions: some of
what it asked for is already built, and some of what it assumed was simple
turned out to need real design. Every finding below is re-verified against
the live v9.0.10 codebase (2026-10-10), not restated from the doc.

This plan was built through live, iterative design discussion with the
operator rather than from the doc's own framing. Scope grew well past the
original 12 items because several of them turned out to be the same piece
of work once investigated:

- Part A's "contention check" (Proposal 1) expanded into a full
  priority/preemption/resume/federation scheduling design once the
  existing capacity ledger turned out to be far more capable than the doc
  credited.
- Part B's B3 ("node/model dashboards") and the Part A scheduling work
  converged into one thing: a new top-level Scheduler surface.
- Part B's B1 ("per-call usage records") and B5 ("model scorecard") both
  turned out to depend on data/mechanisms this plan builds anyway (B1 is
  the Scheduler/cost dashboards' data feed; B5 is implementable directly
  on top of the Council Profiles this plan builds on from BL390).
- Proposal 6 (council configurable options — the operator's own unresolved
  note in the source doc) turned out to be a much larger, genuinely useful
  extension once the existing council/persona/backend machinery was
  checked: most of the hard part (per-persona LLM pinning) already shipped
  in BL390, and what's missing is a well-precedented "named reusable
  definition" pattern this codebase already uses three other ways.

Two bugs were found and fixed mid-iteration while live-verifying Part B's
premises (a usage-tracker restart-amplification bug, B109/v9.0.8, and an
imap_mcp outbound-bounce bug, B110/v9.0.10). Both are already shipped and
are **not** part of this plan. A related gap (imap_mcp has zero
REST/MCP/CLI/comm/PWA config exposure) was filed separately as **BL404**
and is also not part of this plan.

## Confirmed current-state findings (live-verified 2026-10-10)

- **Capacity ledger** (`internal/capacity/ledger.go`) already implements
  named pools, atomic multi-pool leases, and a fair wait queue ordered by
  priority → aging → model-affinity → least-recently-served PRD → FIFO
  (`orderLocked`). `PRD.Priority` is fully wired end-to-end
  (`internal/autonomous/capacity.go:123`'s `admit()` passes it into the
  ledger). The 4 admission call sites in `cmd/datawatch/capacity_wiring.go`
  for plain/interactive sessions never set `Priority` or `PRDID` —
  independent sessions are invisible to fairness today, always
  priority-zero.
- **No preemption exists anywhere** (grep for `preempt`/`evict`/`Interrupt`
  = zero hits). `autonomous.Manager.Pause`/`Resume`
  (`internal/autonomous/manager.go:2115-2145`) is "soft" — `Pause` only
  stops the executor from launching *new* tasks for that PRD; it does
  nothing to a task already mid-flight. The only primitives that touch an
  in-flight task today are `cancel_task` (destructive, no resume) and
  `reset_task`/`restart_session` (restart from scratch).
- **Pools are concurrency-slot-based only.**
  `cmd/datawatch/capacity_wiring.go:184`: `led.SetLimit("llm:"+l.Name,
  l.MaxInflight)` — a concurrent-call-count cap, same mechanism for every
  backend. A cloud LLM registry entry (e.g. `claude-code`) has no
  `ComputeNodes`, so `keys()` (capacity_wiring.go:84-94) only gives it
  `["host", "llm:<name>"]` — no `node:` pool. There is no token/request-rate
  pool type anywhere, cloud or local.
- **Cloud rate-limit handling is 100% reactive.** `rateLimitPatterns`
  (`internal/session/manager.go:131`) regex-matches claude-code's own CLI
  dialog text (*"5-hour limit reached"*, *"weekly usage limit"*, etc.)
  *after* the limit is hit, and triggers the existing `StateRateLimited`
  auto-resume flow. Nothing proactively prevents a call from being admitted
  when a cloud provider's budget is nearly exhausted.
- **Federation plumbing is more capable than assumed.**
  `internal/federation/capabilities.go` already has `CapLLMsList/Read/Write`
  and `CapComputeList/Read/Write` as grantable CBAC capabilities.
  `internal/federation/hopchain.go` (GH#201 Phase 2, landed 2026-10-09) is a
  real, already-nesting-aware multi-hop mechanism — an HMAC-signed, ordered
  hop list, explicitly designed and commented around a 3-hop example,
  already used by `ProxyRouter`'s LLM-call proxying for origin-actor
  attribution across daemon-to-daemon hops. **This narrows, but doesn't
  fully overturn, the session's earlier "multi-hop deferred" framing** —
  that framing was about a broader PWA/all-surfaces peer-list
  architecture pass (still genuinely deferred), not about whether any
  multi-hop-capable plumbing exists at all; clarified in memory as part
  of this plan (see Out of scope). What's missing for *this plan's*
  purposes is not nesting itself but giving the *capacity/scheduling
  resource* a federation presence at all: no `CapCapacity` exists, and
  nothing in the ledger or `/api/capacity` has any notion of a federated
  peer's pools — capacity is purely local-instance-scoped today.
- **`/api/capacity` has no informative decision shape.**
  `Waiter{Holder,PRDID,Pools,Since,Reason}` (`ledger.go:41-47`) has no
  computed queue position, no ETA, no alternate-capacity signal. The only
  UI surface is a small per-PRD card buried in the Automaton detail view
  (`app.js` ~20326, `prd-capacity-card`) — no standalone, cross-cutting
  scheduler-monitoring surface exists anywhere.
- **Council: BL390 already has a planned Phase 2/3 covering most of this
  — found while filing this plan's backlog entry, after this plan's own
  Phase 8 was already drafted as a parallel "Council Definition" concept.
  Corrected below rather than shipped as a duplicate.** BL390 Phase 1
  (`Persona.Backend`/`Model`, `internal/council/council.go`) is done,
  shipped v8.38.0 — per-persona LLM pinning already exists. BL390 Phase 2
  (Planned, not started; `docs/plans/historical-plans/
  2026-09-30-council-llm-model-assignment.md`) already designs exactly
  the named-reusable-definition object this plan needs: `CouncilProfile
  {Name, Personas, Backends, DefaultBackend, Engagement, Mode}`, with an
  `EngagementMode` enum (`Parallel` = today's semaphore-gated behavior,
  `RoundRobin` = new, `maxPar` forced to 1, personas processed in slice
  order). BL390 Phase 3 (also Planned) wires a named profile into the PRD
  verify-gate specifically, with `PRD.CouncilProfile` + a global
  `VerificationCouncilProfile` default — and **explicitly scopes out**
  "council as a PRD's *execution* backend" (not just its verify-gate) as
  a deliberate, not-yet-operator-confirmed decision ("council produces a
  text consensus, not an interactive coding session... flagged here
  explicitly rather than silently built or silently dropped").
  `CouncilConfig` (`internal/config/config.go:539`) is still the
  pre-BL390-Phase-2 single global config today — Phase 2/3 are designed
  but unimplemented. What BL390 does **not** cover: `RoundRobin` is serial
  *independent* answering, not true output-chaining (persona N's output
  becoming persona N+1's literal input) — a real pipeline-handoff
  topology doesn't exist in either the shipped code or BL390's own plan.
- **No local-compute cost model.** `session.DefaultCostRates()`
  (`internal/session/cost.go`) hardcodes `ollama`/`shell`/local backends to
  `{InPerK: 0, OutPerK: 0}` — genuinely free in the model, no
  electricity/amortized-hardware dimension.
- **Alert rules have no LLM/scheduler awareness.**
  `alertrules.Condition.Metric` (`internal/alertrules/types.go`) is pure
  resource telemetry (`cpu_pct | mem_pct | gpu_pct | rss_bytes | ...`).
  `Action.Kind` is `alert | scale_up | scale_down` only — nothing LLM-,
  queue-, or budget-aware, and no action type that can act on the
  scheduler itself.
- **Eval harness has no multi-model fan-out.** `internal/evals/evals.go`'s
  `Suite`/`Case`/`Run`/`Score` already does pass/fail grading per case, but
  a `Run` targets one backend at a time — no built-in comparison dimension
  across models.
- **B1/B3 premise correction.** B109 (v9.0.8, already shipped) fixed the
  session-level usage *aggregate* (it was being corrupted by restart
  amplification, not "all zero" as the source doc assumed). The richer
  per-call-grain record B1 actually asked for (role, node, ttft_ms,
  decode_tokens_per_sec, queue_wait_ms, success/retry) is still unbuilt —
  only the session-level aggregate exists. B3's "node/model dashboards" is
  not separate work; it is this plan's new Scheduler surface (Decision 7).

## Decisions

Made by the operator through iterative discussion, 2026-10-10. This is the
authoritative record; phases below implement these as stated, not as
re-derived from the source doc.

1. **Role→LLM mapping** (Proposal 1): tag roles onto existing LLM registry
   entries (a role-tag field on each call-site/registry field), not a new
   mapping table. All 9 roles in scope from day one. Resolution order:
   explicit per-call-site key always wins over a role default. Roles are
   an **open set** (any string is valid, not a fixed enum). Contention
   check fires both reactively (periodic) and actively (triggered).
   PWA: one consolidated **"Workload Roles"** card under
   **Settings → Automate** (not scattered per-subsystem).
2. **Capacity/rate model** (Proposals 2/7 + B1 overlap): generalize the
   ledger so **any** pool can optionally carry a token/request-rate
   ceiling (rolling window) alongside its existing concurrency ceiling.
   One pool type, applied uniformly to cloud and local/open backends — not
   a cloud-only special case. Rationale (operator): useful for local
   models too, and positions the system for when cloud access moves from
   today's subscription/TUI-only `claude-code` backend to real API-key
   usage, where token economics matter.
3. **Node selection / failover ordering** (Proposal 4/7): reuse compute
   node `scheduling_priority` (already exists; confirmed live:
   `datawatch`/`datawatch-openai` = 90, `johnnyjohnny` = 70) as the
   ordering signal among same-role-tagged LLM entries. No new priority
   concept invented.
4. **Priority, preemption, resume** (new cross-cutting scope): "interrupt"
   means let the current call/turn finish, then release the lease and
   hold the session — never a mid-generation kill. "Resume" means just
   re-admit later; nothing was destroyed. Autonomous tasks reuse the
   existing B97 checkpoint-protocol precedent for resume context rather
   than a new mechanism. New `StatePreempted` session state, same shape as
   the existing `StateRateLimited` (externally-told-to-wait vs.
   internally-told-to-wait, same trigger/resume plumbing). Preemption is
   **both** reactive (default, fairness-ordering driven) and active
   (operator/urgent-priority triggered); active preemption needs its own
   trigger mechanism and a louder confirmation/notification path, since it
   can forcibly interrupt someone else's work — possibly a human's
   interactive session.
5. **Session-level priority** (new cross-cutting scope): new
   `Session.Priority int`, mirroring `PRD.Priority`'s existing shape,
   wired into the 4 admission call sites in `capacity_wiring.go` that
   currently never set it. "Play well" behavior is **fully mature, all
   three layers together**, not a single choice:
   (a) passive fairness ordering always underlies everything;
   (b) session creation actively computes an informed default by checking
       current PRD/session load;
   (c) that default and the reasoning behind it is surfaced to the
       operator as a warning/choice at creation time, not applied
       silently.
6. **Federation-aware capacity** (new cross-cutting scope): no new nesting
   mechanism — reuse the existing hop-chain (`hopchain.go`) and CBAC
   capability-grant system, both already nesting-aware. The real new work
   is giving the capacity/scheduling resource itself a federation
   presence: a new `CapCapacity` capability, and a pool representation for
   a remote peer's reported capacity.
7. **Never silently block** (new cross-cutting scope, absorbs B3):
   admission becomes an informative decision, not a blind wait — one of
   *admit-now*, *queued-at-position-N-behind-\[holders\]*, or
   *here's-idle-capacity-on-\[host/peer/model\]-instead*. This elevates
   `/api/capacity` and its buried per-PRD card into a **new top-level
   Scheduler surface** (own nav entry; full REST/MCP/CLI/comm/PWA parity
   set) rather than growing the existing card in place.
8. **Council as a reusable, cross-surface definition** (Proposal 6,
   substantially expanded — **builds on BL390 Phase 2/3, does not
   duplicate them**): implement BL390 Phase 2's `CouncilProfile` type and
   `EngagementMode` enum as designed (named, reusable, following the same
   CRUD pattern as session profiles/guardrail profiles/autonomous
   templates — already BL390's own framing, not a new pattern this plan
   introduces) and BL390 Phase 3's PRD-verify-gate wiring as designed.
   This plan adds three things on top that BL390 didn't cover: (a) a true
   sequential pipeline-handoff `EngagementMode` (persona N's output
   becomes persona N+1's literal input — distinct from `RoundRobin`'s
   serial-but-independent answering); (b) extending a `CouncilProfile`'s
   selection points beyond the PRD verify-gate to
   `AutonomousConfig.VerificationBackends` (a "verification council" — N
   verifier personas vote instead of falling through a candidate list),
   the already-defined-but-disabled MCP sampling triggers
   (`alert_triage`/`anomaly_analysis`/`morning_briefing`), and B5's model
   scorecard (a profile whose personas are the same prompt pinned to
   different backends, fed through the existing eval grader); (c) a new
   "start session with a council" interactive mode. (b) and (c) **revisit
   BL390 Phase 3's explicit "not in scope" call** (council as something
   beyond a verify-gate producing one-shot text) — that was a deliberate,
   not-yet-operator-confirmed scope line in BL390's own plan, not a
   sequencing deferral, so it needs an explicit go-ahead here rather than
   being treated as already settled by this plan's existence.
9. **Local-compute cost unit** (B2): flat operator-set **$/GPU-hour per
   compute node**. No metering dependency; one new config field per node.
10. **B1/B3/B4/B5 disposition**: B3 is Decision 7 (not separate work). B1's
    session-level aggregate is already fixed (B109); this plan builds the
    richer per-call-grain record (role, node, ttft_ms,
    decode_tokens_per_sec, queue_wait_ms, success/retry) as the shared data
    feed for the Scheduler, cost dashboards, and B5. B4 needs new
    `alertrules.Condition.Metric` values (`queue_wait_seconds`,
    `cost_burn_rate`, `token_budget_remaining`, `model_error_rate`) and a
    new `Action.Kind` (`switch_model` / `escalate_priority`) so an alert
    can act on the scheduler, not just notify. B5 is covered by Decision 8.

## Phases

Ordered so later phases can build on earlier ones' data/mechanics rather
than guessing at them (per Decision dependencies: role tags before
failover ordering; the rate-ceiling pool type and informative-admission
API before any UI can show it; per-call usage records before any
dashboard/alert/scorecard has real data to show).

### Phase 0 — Role tags on LLM registry entries (Decision 1)
- `internal/llm` registry entry struct: new open-string `Roles []string`
  field (or per-call-site role key — see Decision 1's "tag existing
  entries" framing). `llm_add`/`llm_update` MCP tools + REST + CLI gain a
  `--role`/`roles` param.
- Resolution: explicit per-call-site backend/model always wins; role
  lookup only applies when a call site has no explicit override.
- Contention check: periodic reactive scan (did two roles silently
  converge on the same entry since last check) + an active check fired at
  config-write time.
- PWA: new "Workload Roles" card, Settings → Automate.

### Phase 1 — Session priority wiring (Decision 5, mechanics only)
- `session.Session` struct: new `Priority int` field (mirrors
  `autonomous.PRD.Priority`'s shape exactly).
- Wire into the 4 untouched admission call sites in
  `cmd/datawatch/capacity_wiring.go` (`ledger.Request{Priority:
  sess.Priority}`), closing the "independent sessions invisible to
  fairness" gap.
- Default value and the active "play well" computation are Phase 6 (needs
  the Scheduler UI to surface the operator warning/choice).

### Phase 2 — Ledger: rate-ceiling pools + informative admission (Decisions 2, 7)
- `internal/capacity/ledger.go`: new optional rate-ceiling dimension per
  pool (requests/tokens over a rolling window), alongside the existing
  concurrency `Limit`. Admission checks both dimensions when a ceiling is
  set; unset = today's concurrency-only behavior, no regression for
  existing pools.
- New admission result shape replacing "block or timeout": `Decision`
  enum (`admit`, `queued`, `alternate_available`), with `QueuePosition int`
  (computed from the waiter's index after `orderLocked` sorts it — no new
  sort logic, just reporting the existing one) and `Alternates
  []{Pool,Node,Model}` for idle capacity found elsewhere.
- `Ledger.Acquire` keeps its blocking contract for existing callers;
  informative callers (Phase 6's Scheduler) use a new non-blocking
  `Status(req Request) Decision`-style query, in the same spirit as the
  existing `verifierCapacityTryAdmit` precedent (v8.36.9) but returning the
  richer `Decision` instead of a bare bool.

### Phase 3 — Preemption + resume (Decision 4)
- New `StatePreempted` session state, same trigger/resume shape as
  `StateRateLimited` (`schedStore`-driven auto-resume).
- Reactive preemption: fairness ordering alone decides who yields when a
  higher-priority request needs the same pool and the ledger is full.
- Active preemption: new operator/urgent-priority-triggered path with its
  own confirmation/notification step before interrupting another
  session's or PRD's in-flight work.
- Autonomous task resume: reuse the B97 checkpoint-protocol precedent
  (read `CHECKPOINT.md`, relocate, fold into the next attempt's retry hint)
  rather than inventing a new resume primitive.

### Phase 4 — Per-call usage record store (B1, richer record)
- New record shape (role, node, model, backend, ttft_ms,
  decode_tokens_per_sec, queue_wait_ms, success/retry, tokens in/out, est.
  cost) appended per LLM call, distinct from the existing session-level
  aggregate (`TokensIn`/`TokensOut`/`EstCostUSD` + the B109 checkpoint
  fields) which stays as-is.
- **Local/open models already surface real token counts per call, not
  just duration** — confirmed live: `internal/llm/backends/ollama/
  conversation.go:217-229` already parses `prompt_eval_count`/
  `eval_count` from the streaming response and plumbs them through a
  `usageFn` callback; `openwebui` and `gemini` backends have the same
  shape. None of this is captured into any per-call record today — Phase
  4 wires the existing `usageFn` callbacks into the new record for every
  backend that has them (local and cloud alike), so local-compute calls
  get the same tokens-in/tokens-out/decode_tokens_per_sec fields as cloud
  calls, not a duration-only record.
- This is the shared data feed for Phase 6 (Scheduler dashboards), Phase 5
  (local-compute cost), Phase 8 (B5 scorecard), and Phase 9 (alert
  metrics) — built once, consumed by all four.

### Phase 5 — Local-compute cost model (B2)
- New compute-node field: flat `$/GPU-hour` rate, operator-set.
- Cost computation for a local LLM call = node rate × wall-clock duration
  of the call (from Phase 4's record), joined the same way cloud $/token
  costs already compute from `CostRate`.
- Because Phase 4 captures real token counts for local/open models too
  (not just duration), the local-compute view reports **both** the
  dollar cost (time × GPU-hour rate) **and** token throughput
  (tokens/sec, tokens per call) side by side — letting the operator
  compare local vs. cloud on a $/1K-token-equivalent basis despite the
  underlying local cost unit being time-based, and giving Phase 8's B5
  scorecard real throughput numbers for local models, not just cloud
  ones.

### Phase 6 — New top-level Scheduler surface (Decision 7, UI half)
- New top-level nav surface (own entry, not nested under Automaton or
  Settings): live view across every pool — local compute nodes, every
  LLM-registry entry/model, current priorities, rate-ceiling state (Phase
  2), and the live queue with computed positions (Phase 2's `Decision`
  shape).
- Session-creation "play well" warning/choice UI (Decision 5c): on new
  session start, show the computed default priority and why, let the
  operator accept or override.
- Full parity set: REST, MCP, CLI, comm, YAML, PWA (see Parity surface
  below).

### Phase 7 — Federation-aware capacity (Decision 6)
- New `CapCapacity` (read) federation capability in
  `internal/federation/capabilities.go` + `allCaps` + `mcp_tool_caps.go`
  entries for any new capacity/scheduler MCP tools — same least-privilege
  default-off precedent BL403 used for `CapPluginRead`/`CapPluginWrite`
  (not added to any `BuiltinGroups` preset by default).
- A federated peer's reported pools surface in Phase 6's Scheduler as
  "capacity on host X (federation)", carried through the existing
  hop-chain — no new hop/nesting mechanism, per Decision 6.

### Phase 8 — Council Profiles: implement BL390 Phase 2/3, then extend (Decision 8)
- **Sub-phase 8a** — implement BL390 Phase 2 and Phase 3 exactly as
  already designed in `docs/plans/historical-plans/
  2026-09-30-council-llm-model-assignment.md` (status: Planned, never
  started): `CouncilProfile{Name, Personas, Backends, DefaultBackend,
  Engagement, Mode}`, `EngagementMode{Parallel, RoundRobin}`, CRUD surface
  (`council_profile_list/get/create/update/delete` + `/api/council/
  profiles*`), `PRD.CouncilProfile` + `VerificationCouncilProfile`
  wiring into the verify-gate. This is prerequisite plumbing, not new
  design — do not re-decide anything BL390 already decided.
- **Sub-phase 8b** — add a true sequential pipeline-handoff
  `EngagementMode` (e.g. `EngagementChain`): persona N's output becomes
  persona N+1's literal input, distinct from `RoundRobin`'s
  serial-but-independent answering. New, not in BL390.
- **Sub-phase 8c** — extend a `CouncilProfile`'s selection points beyond
  the PRD verify-gate: `AutonomousConfig.VerificationBackends` (a
  "verification council" — N verifier personas vote instead of falling
  through a candidate list); the currently-disabled `alert_triage`/
  `anomaly_analysis`/`morning_briefing` MCP sampling triggers; B5's model
  scorecard (a profile fanning the same prompt across personas pinned to
  different backends, graded via the existing `internal/evals` grader);
  a new "start session with a council" interactive mode. **Needs explicit
  operator go-ahead before implementation** — this revisits BL390 Phase
  3's deliberate "not in scope: council as a PRD's execution backend"
  line, not just a sequencing continuation of it.

### Phase 9 — Alerting hooks (B4)
- `internal/alertrules/types.go`: new `Condition.Metric` values
  (`queue_wait_seconds`, `cost_burn_rate`, `token_budget_remaining`,
  `model_error_rate`), fed by Phase 4's per-call records and Phase 6's
  Scheduler state.
- New `Action.Kind` values (`switch_model`, `escalate_priority`) so a
  firing alert can act on the scheduler (e.g. auto-failover via Decision
  3's ordering, or bump a PRD's priority) rather than only notify.

### Phase 10 — Dashboard-artifact cross-reference into BL403
- Any new dashboard/observer card from Phases 6-9 (Scheduler queue view,
  node/model health, cost/scorecard tiles) is built using BL403's existing
  generic widget `Kind` taxonomy (`stat_tile | table | list | sparkline |
  status_badge | markdown`) wherever the shape fits, rather than bespoke
  one-off card HTML.
- Add a short cross-reference section to
  `docs/plans/2026-10-09-plugin-extension-surfaces.md` listing each new
  first-party `Kind` usage this plan ships as a concrete example — keeps
  that plan the living index of what the widget surface can do, useful to
  a future plugin author considering a similar card (e.g. a plugin-
  contributed scorecard-style table next to the first-party one).

## Parity surface

Per the Mobile-Parity Rule's full surface set (`REST`, `MCP`, `CLI`,
`comm channel`, `YAML/config`, `PWA`, `Android`, `iPhone/iOS`):

| Capability | REST | MCP | CLI | Comm | YAML | PWA | Android/iOS |
|---|---|---|---|---|---|---|---|
| Role tags (Phase 0) | `llm_update` extension | `llm_add`/`llm_update` param | `--role` flag | n/a (config-time, not a chat action) | `llms[].roles` | Workload Roles card | Settings-only parity, same as other LLM-registry fields today |
| Session priority (Phase 1/6) | session-create body field | `start_session` param | `--priority` flag | n/a (set at creation, not a chat verb) | default via `session.*` | play-well warning/choice dialog | same field, native form control |
| Rate-ceiling pools (Phase 2) | `/api/capacity` extension | `capacity_status` extension | n/a (reporting only) | `capacity` command extension | `llms[].rate_limit`/node equivalent | Scheduler surface | reporting-only, same as existing capacity card |
| Preemption/resume (Phase 3) | new cancel-preempt endpoint | new MCP tool(s) | n/a | active-preemption notification message | n/a | confirmation dialog for active preemption | push notification for an operator-visible active preemption |
| Scheduler top-level surface (Phase 6) | new `/api/scheduler*` | new `scheduler_*` tools | new `datawatch scheduler` subcommand | `schedule status`-style query | n/a (reporting) | new top-level nav entry | new nav entry, same data |
| Federated capacity (Phase 7) | `/api/scheduler` extension, `fedCap`-guarded | `CapCapacity`-gated tools | n/a | n/a | peer capability grant (existing federation YAML) | peer capacity shown in Scheduler | reporting-only |
| Council Profiles (Phase 8, BL390 P2/3 + extensions) | `/api/council/profiles*` (BL390 P2) | `council_profile_*` tools (BL390 P2) | n/a (complex object, PWA/REST only, per BL390's own precedent) | `council run <profile-name>` (BL390-style verb) | n/a (runtime store, like profiles/templates) | Profile CRUD (BL390 P2) + PRD picker (BL390 P3) + 8c's session-mode picker | session-mode picker parity (8c only) |
| Local-compute cost (Phase 5) | compute-node field extension | `compute_node_update` param | n/a | n/a | `compute_nodes[].gpu_hour_rate` | compute node edit form | reporting-only |
| Alerting hooks (Phase 9) | `alert_rule_create`/`update` extension | same MCP tools, extended | n/a | n/a | n/a (runtime store) | alert rule builder, new metric/action options | reporting-only |

Each excluded cell above is excluded because the surface doesn't have an
analogous action today (e.g. CLI has no chat-style "play well" prompt
because CLI invocations are already explicit/scripted) — not an oversight;
confirm at review if any of these reads wrong.

## Reuse-and-Expand audit

What each new piece reuses rather than invents, per decision:

- Role tags → existing LLM registry entries, not a new mapping table.
- Failover ordering → existing compute-node `scheduling_priority`, not a
  new priority concept.
- Session priority → existing `PRD.Priority`'s exact shape.
- Preempt/resume → existing `StateRateLimited` shape + B97's
  checkpoint-protocol precedent, not a new state machine.
- Rate-ceiling pools → extends the existing `Ledger`/`PoolStatus` types,
  not a parallel rate-limiting subsystem.
- Non-blocking admission → extends the existing `verifierCapacityTryAdmit`
  precedent (v8.36.9), not a new query path.
- Federation nesting → existing `hopchain.go` + CBAC capability grants, not
  a new multi-hop mechanism.
- Council Profiles → **BL390 Phase 2/3's own already-designed
  `CouncilProfile`/`EngagementMode`**, not a new "Council Definition"
  shape (this plan's own first draft duplicated BL390 before the existing
  plan was found — corrected). Per-persona backend pinning → existing
  `Persona.Backend`/`Model` (BL390 Phase 1, shipped).
- B5 model scorecard → BL390's `CouncilProfile` + the existing
  `internal/evals` grader, not a new comparison harness.
- New dashboard/observer cards → BL403's existing generic widget `Kind`
  taxonomy, not bespoke card HTML.
- Local/open-model token capture (Phase 4/5) → existing `usageFn`
  callback plumbing in the ollama/openwebui/gemini backends, not new
  token-parsing logic.

## Out of scope / deferred

- **True mid-generation interrupt** (killing an LLM call mid-token-stream)
  is explicitly out of scope — "interrupt" always means
  let-current-call-finish-then-release, per Decision 4.
- **Token-metered local-compute cost** (vs. flat-rate) — deferred; flat
  $/GPU-hour (Decision 9) is v1.
- **Full multi-hop capacity propagation beyond one federated peer** —
  Phase 7 gives capacity a federation presence and rides the existing
  nesting-aware hop-chain, but this plan does not itself build or test a
  3+-hop capacity-sharing scenario; that's a natural follow-up once Phase
  7 ships and a real multi-peer topology exists to test against.
- **Memory clarification** (process note, not a plan task): the prior
  `project_multihop_federation_goal` memory recorded a broader
  PWA/all-surfaces multi-hop architecture pass as deferred/unscheduled —
  that remains accurate and is *not* part of this plan. What's been
  clarified in memory is narrower: `hopchain.go`'s multi-hop-capable
  attribution mechanism already exists for LLM-proxy calls specifically,
  so Phase 7 here rides existing plumbing rather than needing to assume
  zero multi-hop support exists anywhere in the codebase.

## Testing / verification

- Go unit tests per phase, matching this codebase's existing style
  (ledger tests for the new rate-ceiling dimension and `Decision` shape;
  council tests for the sequential-topology mode; cost tests for the flat
  GPU-hour computation).
- `node --test internal/server/web/*.test.js` for every new PWA
  surface (Scheduler, Workload Roles card, Council Profiles CRUD,
  play-well dialog) — existing escaping/XSS-guard pattern applies to any
  new `innerHTML` assembly.
- Federation-Parity Rule checklist: every new `fedCap`-guarded endpoint
  (Phase 7) gets its capability check immediately after the method guard,
  and `TestEveryUnconditionallyRegisteredToolHasAnMCPToolCapEntry` must
  pass for every new MCP tool.
- Mobile-Parity Rule audit: file Android/iOS parity issues for any PWA-only
  gap this plan opens (expected minimal, per the Parity surface table
  above — most new surfaces are reporting-only on mobile).
- Localization Rule: 5 locale bundles + `mustHave` update for any new
  high-visibility string (Scheduler nav label, Workload Roles card,
  play-well dialog text, Council Profiles UI).
- Testing Tracker entries for every new REST endpoint (`/api/scheduler*`,
  `/api/council/profiles*`, capacity/ledger extensions).
- Live-verification requirement (this session's own established standard
  for capability/security-adjacent surfaces): do not mark Scheduler,
  federated capacity, or preemption "done" on unit tests alone — smoke
  each against the running daemon before calling it shipped, same as the
  B109/B110 fixes earlier this session.
- Before tagging any release including Phase 7: confirm the federation
  capability-mapping table in AGENT.md's Federation-Parity Rule section
  gets a `CapCapacity` row, mirroring the existing `CapAutonomous*` rows.

## Files (representative, not exhaustive)

- `internal/capacity/ledger.go`, `cmd/datawatch/capacity_wiring.go` —
  Phases 1, 2, 3.
- `internal/session/manager.go`, `internal/session/store.go` — Phase 1
  (`Session.Priority`), Phase 3 (`StatePreempted`).
- `internal/session/cost.go`, `internal/session/*_usage.go` — Phase 4.
- `internal/config/config.go` (compute node GPU-hour rate) — Phase 5.
- `internal/server/capacity.go`, new `internal/server/scheduler.go`,
  `internal/server/web/app.js` — Phase 6.
- `internal/federation/capabilities.go`, `internal/federation/hopchain.go`
  (read, not modified), `internal/federation/mcp_tool_caps.go` — Phase 7.
- `internal/council/council.go`, `internal/config/config.go`
  (`CouncilConfig.Profiles` per BL390 Phase 2), `internal/autonomous/*`
  (`PRD.CouncilProfile` per BL390 Phase 3, plus 8c's extensions) — Phase
  8; see also `docs/plans/historical-plans/
  2026-09-30-council-llm-model-assignment.md`.
- `internal/alertrules/types.go` — Phase 9.
- `docs/plans/2026-10-09-plugin-extension-surfaces.md` (cross-reference
  only) — Phase 10.
- `docs/config-reference.yaml`, `docs/plans/README.md`,
  `docs/parity-status.md` — updated as phases ship, not written upfront.
