# Plan: LLM Enhancements — Role-Aware Scheduling, Federated Capacity, Council-as-Reusable-Definition, Usage/Cost Tracking (BL405)

- **Date**: 2026-10-10
- **Version at planning**: v9.0.10
- **Status**: Audited 2026-10-23: see Phase status

## Phase status

| Phase | Status | Evidence |
|---|---|---|
| 0 | not started | no roles field on LLM entries in `internal/llm/registry.go`; `llm_add` / `llm_update` only extend the budget object (`internal/mcp/budget.go`); no "Workload Roles" card in `internal/server/web/app.js` or `internal/server/web/locales/en.json`; no contention-check code in `internal/llm` or `cmd/datawatch` |
| 1 | not started | no `Priority` field on `Session` (`internal/session/store.go`); admission requests in `cmd/datawatch/capacity_wiring.go` (e.g. line 456) never set `Priority`, while the autonomous path does set `prd.Priority` (`internal/autonomous/capacity.go:213`) |
| 2 | done | rate-ceiling dimension `internal/capacity/rate_ceiling.go`; `Decision` enum and non-blocking `Ledger.Check` in `internal/capacity/decision.go`; REST `GET /api/capacity/admission` and the rate fields in `internal/server/capacity.go`; MCP `capacity_admission` (`internal/mcp/bl221_types.go`; the plan's `capacity_admission.go` name was not used); CLI `datawatch capacity admission` in `cmd/datawatch/cli_capacity.go`; the windowed dollar budget landed earlier (`internal/autonomous/budget.go`). Next patch above main (unreleased development build) |
| 3 | not started | `KnownStates()` in `internal/session/store.go` has no preempted state; `internal/capacity/ledger.go` has no preemption path; `internal/autonomous/manager.go` `Pause`/`Resume` remain soft pauses, no checkpoint-driven preemption resume |
| 4 | not started | no per-call usage record store in `internal/session`, `internal/llm`, or `internal/stats` (no `ttft` / `queue_wait` / `usageRecord`); the ollama `usageFn` still feeds only the session aggregate (`internal/llm/backends/ollama/conversation.go:239-240`) |
| 5 | partly done | flat per-hour node field exists: `internal/compute/node.go:221` (`cost_per_hour`), `internal/config/config.go:549`; but local backends are still zero-cost in `DefaultCostRates()` (`internal/session/cost.go`) and `CostPerHour` is copied, never multiplied into a call cost |

| 6 | not started | no `internal/server/scheduler.go` and no `/api/scheduler*` routes in `internal/server`; no `scheduler_*` tools in `internal/mcp`; no `datawatch scheduler` subcommand in `cmd/datawatch/main.go`; no top-level Scheduler nav entry or session-creation play-well dialog in `internal/server/web/app.js` (only capacity UI is the per-task `waiting_capacity` badge near `internal/server/web/app.js:13131`); the budget work (BL428) landed as Phase 2 surfaces (`internal/server/budget.go`, `internal/mcp/budget.go`, `cmd/datawatch/cli_budget.go`), not a Scheduler surface; BL431 automation (`internal/autonomous/automation_*.go`) delivered PRD automation only, no scheduler item under another name |
| 7 | not started | no `CapCapacity` constant anywhere in `internal/federation/capabilities.go` and none in `allCaps`; `internal/federation/mcp_tool_caps.go` (line 137) maps only the pre-existing `capacity_status` tool to `CapAutonomousRead`; `internal/capacity/ledger.go` has no peer-pool representation and no hop-chain integration; the only peer-facing capacity-adjacent item is the opt-in BL428 per-peer spend count in `internal/autonomous/peer_budget.go`, which is budget accounting, not pool sharing |
| 8 | not started | no `type CouncilProfile` anywhere in `internal/` or `cmd/` — only a placeholder comment at `internal/autonomous/models.go:318` and the fallback log at `internal/autonomous/guided_mode.go:43` ("BL405 Phase 8 not yet shipped", falling back to operator approval); `internal/mcp/council.go` exposes only personas/run/draft tools (`toolCouncilRun` et al.), no `council_profile_*`; no `EngagementMode`/`EngagementChain` in `internal/council/council.go`; `alert_triage`/`anomaly_analysis`/`morning_briefing` remain the disabled triggers in `internal/mcp/triggers.go` |
| 9 | not started | `internal/alertrules/types.go` still documents `Condition.Metric` as resource telemetry only (cpu/mem/gpu/rss, lines 16-18) and `Action.Kind` as alert/scale_up/scale_down only (lines 29-30); no `queue_wait_seconds`/`cost_burn_rate`/`token_budget_remaining`/`model_error_rate` metric and no `switch_model`/`escalate_priority` action anywhere in `internal/` or `cmd/` |
| 10 | not started | `docs/plans/2026-10-09-plugin-extension-surfaces.md` has no cross-reference section to this plan (no bl405/scorecard/LLM-enhancements matches in it); no first-party widget-`Kind` dashboards exist yet since Phases 6-9 ship nothing to reference |

## Context

Source: `llm-research/docs/07-datawatch-improvements.md` (outside this repo)
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
   `datawatch`/`datawatch-openai` = 90, `local-node` = 70) as the
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

### Phase 0 — Role tags on LLM registry entries (Decision 1) [done]
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

**Audit evidence** (audited against the current tree):
- `Roles []string` registry field + `llm_add`/`llm_update`/REST/CLI/comm role param — **done**: `Roles` on the entry in `internal/inference/llm.go`, normalised by `NormalizeRoles` in `internal/inference/role_resolve.go`; `roles` on `llm_add`/`llm_update` in `internal/mcp/roles.go`, on `POST /api/llms` and `PUT /api/llms/{name}` in `internal/server/inference.go`, `--role` in `cmd/datawatch/cli_llm.go`, `roles=` in `internal/router/inference.go`. Omitting keeps, an empty list clears.
- Resolution order (explicit per-call-site override wins over role lookup) — **done**: `ResolveRole` in `internal/inference/role_resolve.go`; an explicit backend or model skips the lookup, several enabled claimants pick the first by name and return an ambiguity warning.
- Contention check (periodic reactive scan + config-write trigger) — **done**: `CheckRoleContention` and `RoleContentionMonitor` in `internal/inference/role_contention.go`; `autonomous.role_scan_interval_seconds` (default 300) drives `startRoleContentionScan` in `cmd/datawatch/main.go`; every llms write runs `Server.CheckRoleContention`, which audits each warning. `GET /api/llm-roles` returns the table and warnings.
- PWA "Workload Roles" card under Settings → Automate — **done**: the card and the roles editor in `internal/server/web/app.js`, strings in all five locale files; tests `internal/server/web/app-llm-roles.test.js`, `internal/server/inference_roles_table_test.go`.

### Phase 1 — Session priority wiring (Decision 5, mechanics only) [done]
- `session.Session` struct: new `Priority int` field (mirrors
  `autonomous.PRD.Priority`'s shape exactly).
- Wire into the 4 untouched admission call sites in
  `cmd/datawatch/capacity_wiring.go` (`ledger.Request{Priority:
  sess.Priority}`), closing the "independent sessions invisible to
  fairness" gap.
- Default value and the active "play well" computation are Phase 6 (needs
  the Scheduler UI to surface the operator warning/choice).

**Audit evidence** (audited against the current tree):
- `Session.Priority int` field — **done**: `Priority` on the session record in `internal/session/store.go`, validated by `ParsePriority` in `internal/session/priority.go` and set through `Manager.SetPriority`. Start accepts `priority`; `POST /api/sessions/set_priority` changes it; MCP (`set_session_priority`), CLI (`session new --priority`, `session priority`) and the comm channel carry it too, and the PWA session card shows a non-zero value.
- Wire session priority into the admission call sites — **done**: the `ledger.Request` values in `cmd/datawatch/capacity_wiring.go` and the interactive start in `internal/server/api.go` now set `Priority` from the session; test `cmd/datawatch/capacity_priority_test.go`.
- Default value + active "play well" computation — **remaining** (owned by Phase 6): no priority default, load-based computation, or operator warning exists in `internal/session` or `internal/server`.

### Phase 2 — Ledger: rate-ceiling pools + informative admission (Decisions 2, 7) [done]

> **Done 2026-10-23:** the rate-ceiling dimension, the `Decision` shape and the
> non-blocking query are implemented on all surfaces (next patch above main,
> unreleased development build). The earlier dollar-window budget landed first
> (BL428 Phase 5). Live contended-queue validation is still owed.

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

**Audit evidence** (audited against the current tree):
- Rate-ceiling dimension per pool — **done**: requests/tokens over a rolling window per pool in `internal/capacity/rate_ceiling.go` (`SetRate`, `RecordTokens`, `RateResetAt`), checked beside the concurrency `Limit`; unset keeps concurrency-only behavior. Config keys `capacity_rate_requests`, `capacity_rate_tokens`, `capacity_rate_window_seconds`; the wait carries cause `rate_ceiling` and a reset time through `internal/server/capacity.go`. The dollar-window budget (`internal/autonomous/budget.go`) landed earlier and stays separate.
- `Decision` enum (`admit`/`queued`/`alternate_available`) + `QueuePosition` + `Alternates` — **done**: `internal/capacity/decision.go` (`Decision`, `Alternate`, `FormatDecision`); queue position reuses the existing waiter ordering.
- Non-blocking informative admission query beside blocking `Acquire` — **done**: `Ledger.Check(req) Decision` in `internal/capacity/decision.go`; REST `GET /api/capacity/admission` (`internal/server/capacity_admission.go`, wired with `internal/server/capacity.go`), MCP `capacity_admission` (defined in `internal/mcp/bl221_types.go`), CLI `datawatch capacity admission` (`cmd/datawatch/cli_capacity.go`), comm `capacity admission` (`internal/router/commands.go`).

### Phase 3 — Preemption + resume (Decision 4) [not started]
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

**Audit evidence** (audited against the current tree):
- `StatePreempted` session state (same shape as `StateRateLimited`) — **remaining**: `KnownStates()` in `internal/session/store.go` lists only `StateRunning`, `StateWaitingInput`, `StateComplete`, `StateFailed`, `StateKilled`, `StateRateLimited`; `grep -ri preempt internal/` yields only unrelated kernel headers under `internal/observer/ebpf` and the native-bridge test `internal/channel/probe_test.go`.
- Reactive preemption (fairness ordering decides who yields) — **remaining**: `internal/capacity/ledger.go` only ever grants or blocks; release paths (`Release`, `Reap`) never displace a live lease for a higher-priority waiter.
- Active preemption with operator confirmation/notification — **remaining**: no operator-triggered preemption path or confirmation flow in `internal/autonomous`, `internal/session`, `internal/server`, or `internal/mcp`.
- Autonomous-task resume via the checkpoint precedent — **remaining**: `Pause`/`Resume` in `internal/autonomous/manager.go` remain soft pauses (no in-flight task is yielded); nothing reads `CHECKPOINT.md` into a next-attempt retry hint on preemption.

### Phase 4 — Per-call usage record store (B1, richer record) [not started]
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

**Audit evidence** (audited against the current tree):
- Rich per-call record shape (role, node, model, backend, ttft_ms, decode_tokens_per_sec, queue_wait_ms, success/retry, tokens in/out, est. cost) — **remaining**: no per-call usage record type or store exists in `internal/session`, `internal/llm`, or `internal/stats` (no `usageRecord`, `ttft`, or `queue_wait` anywhere); the only token data that persists is the session-level aggregate (`TokensIn`/`TokensOut`/`EstCostUSD`) in `internal/session/store.go` / `internal/session/cost.go`, plus the per-backend parser helpers (`internal/session/claude_usage.go`, `internal/session/aider_usage.go`).
- Wire the existing `usageFn` token callbacks into the record for every backend — **remaining**: the ollama backend still reports per-turn `prompt_eval_count`/`eval_count` straight to the session aggregate via `usageFn` (`internal/llm/backends/ollama/conversation.go:239-240`); the openwebui (`internal/llm/backends/openwebui/conversation.go`) and gemini backends have the same shape, and no record store consumes any of them.
- Shared data feed for the Scheduler dashboards, local-compute cost, B5 scorecard, and alert metrics — **remaining**: all four consumers are unbuilt, so there is nothing to feed them (depends on the two items above).

### Phase 5 — Local-compute cost model (B2) [partly done]
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

**Audit evidence** (audited against the current tree):
- Flat per-hour operator-set compute-node field — **done**: `CostPerHour float64` (`yaml:"cost_per_hour,omitempty"`) exists on the compute node at `internal/compute/node.go:221` and in the config type at `internal/config/config.go:549`. A partial item, not a full phase win — see the remaining two bullets.
- Local-call cost = node rate × wall-clock duration — **remaining**: `CostPerHour` is only copied from config into the node (`cmd/datawatch/main.go:3325`); no code multiplies it by call duration, and local/open backends are still hardwired to zero in `DefaultCostRates()` at `internal/session/cost.go` (`ollama`, `shell`, `openwebui` → `{InPerK: 0, OutPerK: 0}`).
- Side-by-side $ cost and token-throughput view for local vs. cloud — **remaining**: no such view in `internal/server`, `internal/mcp`, or `internal/server/web`; `internal/session/cost.go`'s `CostSummary` is tokens/dollars from `$`/1K-token rates only.

### Phase 6 — New top-level Scheduler surface (Decision 7, UI half) [not started]
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

**Audit evidence** (audited against the current tree):
- Top-level Scheduler nav surface (own entry, live pool view) — **remaining**: no `internal/server/scheduler.go` file, no `/api/scheduler` route register in `internal/server`, and no Scheduler nav entry in `internal/server/web/app.js` or `internal/server/web/locales/en.json`; the only capacity UI is the buried per-task `waiting_capacity` badge (`internal/server/web/app.js:13131`).
- Live queue with computed positions and rate-ceiling state (Phase 2's `Decision`) — **remaining**: the upstream data now exists (`Ledger.Check`, `GET /api/capacity/admission`, the rate fields on `GET /api/capacity`); the Scheduler view that renders it is not built.
- Session-creation "play well" warning/choice (Decision 5c) — **remaining**: no priority default, load check, or warning/choice dialog anywhere in `internal/session`, `internal/server`, or `internal/server/web/app.js` (the only `priority` UI in `app.js` is the compute-node `scheduling_priority` field and the PRD wizard's `prd.priority`).
- Full parity set (REST/MCP/CLI/comm/YAML/PWA) — **remaining**: none of the Parity-surface-table Phase 6 cells exist; the closest shipped work is the BL428 budget surfaces (`internal/server/budget.go`, `internal/mcp/budget.go`, `cmd/datawatch/cli_budget.go`), which are Phase 2 items, and the BL431 automation files (`internal/autonomous/automation_advance.go`, `internal/autonomous/automation_control.go`, `internal/autonomous/automation_insert.go`) contain no scheduler/priority/session-creation surface under another name.

### Phase 7 — Federation-aware capacity (Decision 6) [not started]
- New `CapCapacity` (read) federation capability in
  `internal/federation/capabilities.go` + `allCaps` + `mcp_tool_caps.go`
  entries for any new capacity/scheduler MCP tools — same least-privilege
  default-off precedent BL403 used for `CapPluginRead`/`CapPluginWrite`
  (not added to any `BuiltinGroups` preset by default).
- A federated peer's reported pools surface in Phase 6's Scheduler as
  "capacity on host X (federation)", carried through the existing
  hop-chain — no new hop/nesting mechanism, per Decision 6.

**Audit evidence** (audited against the current tree):
- `CapCapacity` (read) capability + `allCaps` entry — **remaining**: no `CapCapacity` constant in `internal/federation/capabilities.go` and no capacity entry in `allCaps` (list at line 167); capabilities there cover llms/compute/autonomous/etc. but not the capacity/scheduler resource.
- `mcp_tool_caps.go` entries for new capacity/scheduler MCP tools — **remaining**: `internal/federation/mcp_tool_caps.go` line 137 maps only the pre-existing `capacity_status` tool to `CapAutonomousRead`; no scheduler/capacity-read tool exists to gate.
- Peer pools surfaced via the hop-chain in the Phase 6 Scheduler — **remaining**: `internal/capacity/ledger.go` is purely local-instance-scoped (no peer/pool-from-host representation, no `internal/federation/hopchain.go` integration on the capacity path); the nearest peer-facing thing is the opt-in BL428 per-peer spend count in `internal/autonomous/peer_budget.go`, which is dollar accounting, not reported pools.

### Phase 8 — Council Profiles: implement BL390 Phase 2/3, then extend (Decision 8) [not started]
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

**Audit evidence** (audited against the current tree):
- 8a: `CouncilProfile` CRUD + `PRD.CouncilProfile`/`VerificationCouncilProfile` wiring — **remaining**: no `type CouncilProfile` anywhere in `internal/` or `cmd/` (only placeholder comments at `internal/autonomous/models.go:318`, `internal/autonomous/manager.go:677-681`, and `internal/autonomous/guided_mode.go:6`); no `/api/council/profiles*` route in `internal/server`; `internal/mcp/council.go` exposes only the shipped personas/run/draft tools (`toolCouncilPersonas`, `toolCouncilRun`, `toolCouncilPersonaOneShot`, draft tools), no `council_profile_list`/`get`/`create`/`update`/`delete`; no `CouncilConfig.Profiles` in `internal/config/config.go`.
- 8b: sequential pipeline-handoff `EngagementMode` (e.g. `EngagementChain`) — **remaining**: no `EngagementMode` or chaining implementation in `internal/council/council.go`; the only "RoundRobin" hit in `internal/` is the unrelated ledger fairness test `TestFairRoundRobinAcrossPRDs` (`internal/capacity/ledger_test.go:76`).
- 8c: extended selection points (verification council, sampling triggers, B5 scorecard, session-with-council mode) — **remaining**: `alert_triage`/`anomaly_analysis`/`morning_briefing` remain the currently-disabled triggers in `internal/mcp/triggers.go`; no evaluation fan-out across backends wired to `internal/evals`; the "council" guided session mode in `internal/autonomous/guided_mode.go` is a stub that logs at line 43 that "BL405 Phase 8 [is] not yet shipped" and falls back to operator approval. Per the plan, 8b/8c additionally need explicit operator go-ahead before implementation.

### Phase 9 — Alerting hooks (B4) [not started]
- `internal/alertrules/types.go`: new `Condition.Metric` values
  (`queue_wait_seconds`, `cost_burn_rate`, `token_budget_remaining`,
  `model_error_rate`), fed by Phase 4's per-call records and Phase 6's
  Scheduler state.
- New `Action.Kind` values (`switch_model`, `escalate_priority`) so a
  firing alert can act on the scheduler (e.g. auto-failover via Decision
  3's ordering, or bump a PRD's priority) rather than only notify.

**Audit evidence** (audited against the current tree):
- New `Condition.Metric` values (`queue_wait_seconds`/`cost_burn_rate`/`token_budget_remaining`/`model_error_rate`) — **remaining**: `internal/alertrules/types.go` still documents `Metric` (lines 16-18) as resource telemetry only (cpu/mem/gpu/rss et al.); grep for the four new metric names across `internal/` and `cmd/` finds nothing, and their upstream feeds (Phase 4 records, Phase 6 Scheduler state) are themselves unbuilt.
- New `Action.Kind` values (`switch_model`/`escalate_priority`) — **remaining**: `internal/alertrules/types.go` lines 29-30 still document `Kind` as alert/scale_up/scale_down only, with the action set wired to `alerts.Store` and the `ScaleNode` stub (package doc, lines 6-8); no scheduler-acting handler exists in `internal/alertrules`.

### Phase 10 — Dashboard-artifact cross-reference into BL403 [not started]
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

**Audit evidence** (audited against the current tree):
- First-party dashboard cards built on the BL403 widget `Kind` taxonomy — **remaining**: no such cards exist because the Phase 6-9 surfaces that would ship them (Scheduler queue view, cost/scorecard tiles) are all unbuilt (see the Phase 6-9 rows above); the only `scheduler`-named artifact in `internal/server/web` is the unrelated plugin pull scheduler referenced in `docs/plans/2026-10-09-plugin-extension-surfaces.md:236`.
- Cross-reference section in `docs/plans/2026-10-09-plugin-extension-surfaces.md` — **remaining**: that file has no match for bl405, llm-enhancements, scorecard, or any first-party `Kind` usage from this plan; its widget `Kind` taxonomy example list was not extended.

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

- **Every phase additionally carries AGENT.md's Phase Completion
  Checklist** (Planning Rules §5, added 2026-10-10 after BL406 Phase 0
  shipped missing several items on its first pass): CHANGELOG entry,
  `docs/config-reference.yaml` for any new field, `docs/operations.md`/
  `README.md`/doc index if applicable (or marked N/A with a reason),
  `docs/testing-tracker.md` entry, no leaked tracker IDs, Mobile-Parity
  Rule check (this plan adds real PWA affordances — the Scheduler
  surface, Workload Roles card, Council Profiles CRUD, the play-well
  dialog — each should expect a `datawatch-app` issue, not just the
  11 phases' functional work), Localization Rule for any new string,
  version bump if the phase ships as its own unit. Apply it when each
  phase actually lands.
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
