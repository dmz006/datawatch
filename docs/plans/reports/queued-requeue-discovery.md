# Discovery notes — requeue a queued planning run / queued worker on a new pool

Discovery only. No source file was changed. Every `path:line` below was read in this
worktree at HEAD `a038e2e9b` (source identical to `df86c6434`, v9.75.1; HEAD adds only
this notes file). Line numbers drift; grep the quoted symbol before editing. All paths
are repo-relative.

Vocabulary: "Automaton" = PRD in code (`autonomous.PRD`). "Automation" = group of
Automata (`autonomous.Automation`). Planning = decompose.

**Two code states are mapped (read first).** The earlier task that implemented the
worker requeue is commit **`25df3e2d2`** ("task(9e4d0001): Implement queued-task
override and requeue while an Automaton is running"), on branch
`automaton/9e4d0001--tasks-implement-queued-task-override-and-reque-6cb90006` (local ref
and `origin/` ref, same SHA). It is a single commit whose parent is `df86c6434`; it is
**not an ancestor of HEAD** (`git merge-base --is-ancestor 25df3e2d2 HEAD` fails), so
the worker requeue is not in this worktree's files, but it is in the repository's refs
and is the worker-side implementation "as it stands". It touches 15 files (source +
tests, no docs). Convention in these notes:

- A bare `path:line` is HEAD (merged tree).
- `[branch]` marks an anchor read with `git show 25df3e2d2:<path>`; to inspect it use
  `git show 25df3e2d2:internal/autonomous/queued_task_override.go | cat -n`. Section 5b
  maps everything the branch adds. Because HEAD differs from the branch parent only by
  docs, the branch should merge without source conflicts; whoever merges it must run
  the full suite, since it changes the signatures of `Manager.SetStoryLLM` and
  `Manager.SetTaskLLM`.

**What the branch does NOT cover (so later tasks must still build it):** the planning
(decompose) queue. Nothing on the branch touches `decomposeAdmitted`, `SetPRDLLM`,
`SetAutomationLLM`, `ResetToDraft` or the decompose in-flight guard. The worker side also
has a post-grant gap (section 5b, "Hazards"). Sections 4-5 describe the HEAD worker side;
section 10 states the design the planner and the worker must share, built on the
branch's `capacity.Ledger.Retarget` rather than a new helper.

## 0. The problem in one paragraph (docs/plans/2026-10-29-automation-dogfood-findings.md)

Finding 7 (line 61): a planning run already queued on the local pool cannot be
re-routed. Changing the Automaton's decomposition profile to the cloud backend,
restarting the decompose, and reset-to-draft (refused while planning) all left the
run waiting on the full local pool. Wanted fix: changing the planning backend of a
queued run cancels and re-queues it on the new pool, and the wait reason shows the
pool of the current profile. Finding 1 (line 55) is the related "planning follows
the global planning backend" gap; finding 6 (line 60) is the worker analogue (an
explicit retry backend still waited on the local pool). The docs file already exists
(`docs/plans/` exists; nothing to create).

Root cause, verified in code (section 2 and 5): backend/model are resolved ONCE, before
the capacity `Acquire`, and then frozen in the ledger request and in the
`DecomposeRequest`/spawn that runs after the grant. Nothing watches for the
profile changing while the request is queued.

## 1. Where the decomposition (planning) profile and model are stored and set

Resolution chain, most specific first: per-PRD, then per-Automation, then global.

| What | Where |
|---|---|
| Per-PRD profile/model fields | `internal/autonomous/models.go:253` `PRD.DecompositionProfile` (planning backend name), `:254` `PRD.DecompositionModel` |
| Per-Automation fields | `internal/autonomous/automation.go` `Automation.PlanningBackend` / `PlanningModel` (struct near `:94-95`; grep `PlanningBackend`) |
| Global fields (config) | `internal/config/config.go:2596` `PlanningBackend`, `:2612` `PlanningEffort`, `:2622` `PlanningModel`, `:2682` `PlanningTimeoutSeconds`; legacy `decomposition_*` keys migrated at `:6329-6336` |
| Global fields (manager copy) | `internal/autonomous/manager.go:133` `Config.PlanningBackend` (+ `PlanningEffort` `:140`, `PlanningModel` `:145`; the lines just above them are comments); bridged from config at `cmd/datawatch/main.go` (~`:4429-4451` block building `autonomous.Config`) |
| Read: Automation vs global | `internal/autonomous/automation_planning.go:14` `effectivePlanning(auto, cfg) (backend, model)` |
| Read: final planning model | `internal/autonomous/planning_model.go:32` `resolvePlannedModel(backend, prdDecompositionModel, prdBackend, prdModel, globalPlanningBackend, globalPlanningModel, globalVerificationModel)` |
| Pinned-model guard | `internal/autonomous/pinned_model.go:44` `RequirePinnedModel(backend, model, keys...)` (pi-family needs an explicit model) |
| Per-PRD setter (core) | `internal/autonomous/manager.go:5053` `Manager.SetPRDLLM(prdID, backend, effort, model, decompositionProfile, decompositionModel *string, actor)`; writes `DecompositionProfile`/`DecompositionModel` at `:5076-5079`; decision kind `set_prd_llm`. API wrapper `internal/autonomous/api.go:1242` |
| Per-Automation setter (core) | `internal/autonomous/automation_set_llm.go:88` `Manager.SetAutomationLLM(id, backend, model, effort, planningBackend, planningModel *string, actor, expectedVersion)` via `setAutomationLLM` (`:~54-76`); API wrapper `internal/autonomous/api.go:1981` |

Key fact for the fix: `SetPRDLLM` is allowed in every status except running and
completed (`manager.go:5049` `prdLLMOverrideAllowed`; check at `:5059`). So changing
`decomposition_profile` while status is `planning` SUCCEEDS and writes the field, but
the in-flight queued decompose never sees it. `SetPRDLLM` does not touch the ledger
and does not emit a requeue signal. `SetAutomationLLM` is allowed for Automation status
draft/active/paused/blocked (`automation_set_llm.go:~62`, `checkControlStatus`
`automation_control.go:102`) and likewise does not touch member PRDs' queued planners.

## 2. Decompose / planning run lifecycle and how it requests capacity

Entry points:
- REST `POST /api/autonomous/prds/{id}/decompose` (alias `plan`): `internal/server/autonomous.go:924` -> `handleDecomposeAsync` `internal/server/autonomous_decompose.go:100`. It returns 202 and runs a goroutine calling `DecomposeStreaming`. **In-flight guard at `:109-121`**: if `s.decomposeJobs` (sync.Map, `internal/server/api.go:550`) holds a pending/in-progress job for the PRD it returns "decompose already in progress" and does NOT restart. This is why "restart decompose" does nothing for a queued run.
- `internal/autonomous/manager.go:2799` `DecomposeStreaming` -> `:2809` `decomposeStreamingCore` (the async path; resolves at `:2834` `backend := prd.DecompositionProfile`, `:2878` `planModel2 := resolvePlannedModel(...)`, admission call `:2898`).
- `internal/autonomous/manager.go:2639` `Manager.Decompose` (sync twin; code comment says keep both in sync). It reads `effectivePlanning` at `:2667`, `backend := prd.DecompositionProfile` at `:2667-2669`, sets `PRDPlanning` at `:2682-2690`, `planModel := resolvePlannedModel` at `:2708`, `RequirePinnedModel(` call at `:2714` (`:2713` is a comment; the async twin calls it at `:2882`), then the retry loop calls `decomposeAdmitted` at `:2735`.
- Retry/failure helpers: `retryDecomposeFailure`, `failDecomposeAttempt` (`internal/autonomous/retry_policy.go:239`), `validateAndStorePlan` (`manager.go:2426`), `finalizeDecomposedPlan`.

Admission: `internal/autonomous/capacity.go:532` `Manager.decomposeAdmitted(prd, backend, model, req)`.
1. `capacityOn()` false (`:256`) or no pools/choices -> runs `m.decompose(req)` directly (`:533-535` and `:542-544`).
2. Pools: `keysFn(backend, model)` then `decomposePools` (`:574`, strips `"host"`); node choices `nodeChoicesFor` (`:209`).
3. Holder is `"plan:" + prd.ID` (`:545`); request literal at `:546-548` (`lreq := capacity.Request{...}`) with `Role: capacity.RolePlanner`, `Consumer: consumerSessionSpawn` (`:39`), `Model: model`.
4. `lreq.Refresh = m.placementRefresh(lreq, backend, model, decomposePools)` (`:549`). **The Refresh closure captures the ORIGINAL backend/model**, so it only re-reads pool keys/nodes for the same LLM.
5. `led.Acquire(context.Background(), lreq, m.capacityWait(), cancelled=plannerCancelled, onWait=setPRDWaitReason)` (`:550-552`; `Acquire(` opens on `:550`, the `setPRDWaitReason` callback is `:551`). Context is `Background` (not cancellable by the caller).
6. On error: `clearPRDWaitReason`; if `plannerCancelled` (`:590`, status cancelled/rejected) return `errDecomposeCancelled` (`:48`) else `fmt.Errorf("capacity: %w", err)` (`:553-563`).
7. On success: `defer led.Release(holder)` (`:565`), `clearPRDWaitReason` (`:566`), then `m.decompose(req)` with the already-frozen `req` (`:567`). So even if a requeue moved the lease to another pool, the decompose that runs would still use the old `DecomposeRequest.Backend/Model`.

Wait reason text on the Automaton: `PRD.WaitReason` (`models.go:321`, json `wait_reason`), written by `setPRDWaitReason` (`capacity.go:605`, under `store.MutatePRD`, then `EmitPRDUpdate`) and cleared by `clearPRDWaitReason` (`:624`). The text is the ledger's own reason (e.g. `llm:ollama at 1/1 (1 waiting)`, "waiting for node X: ..."; built in `internal/capacity/ledger.go:657` `blockReasonLocked`, `:750` `resolveLocked`). The planner path passes the raw ledger reason. The worker path additionally calls `nameRequestedPool` (`internal/autonomous/wait_reason.go:476`) and `ClassifyWaitReason` (`wait_reason.go`) so a task wait reason names the pool it asked for; the planner path does not, so "the pool of the current profile" is not named in `PRD.WaitReason` today.

Ledger behavior (`internal/capacity/ledger.go`): roles ranked `:40` verifier < worker < planner (planner last); `Request.Refresh` doc `:93-102`; `dispatchLocked` `:904` applies Refresh to Pools/Node/NodeChoices/ExcludeNodes only (Holder, PRDID, Priority, Role, Model, Interactive are kept); `Replan` `:933` (has one production caller: `replanOnPlacementChange` `cmd/datawatch/capacity_wiring.go:203`, wired at `:903` through the inference registry's `SetChangeFn`, so it fires on an LLM-entry or compute-node placement change and re-reads each waiter's `Refresh`; it does NOT fire on `SetPRDLLM`, `SetAutomationLLM` or `SetTaskLLM`, and `Refresh` still closes over the old backend/model, so `Replan` cannot move a waiter to a different backend); `Retarget` **[branch]** `ledger.go:1101` (section 5b); `Acquire` `:967` (grant / ctx.Done / timeout `ErrWaitTimeout` / `cancelled()` true -> `context.Canceled`, all via `remove(w)` `:1053`); `Release` `:1180`; `Handoff` `:1218`; `Reap` `:1239`. `cancelled()` is polled only on the poll tick (`pollWaitLocked` `:1041`, `Options.PollInterval`).

## 3. Restart-decompose and reset-to-draft paths

- There is no separate "restart decompose" function. Restarting = another `POST .../decompose`; blocked by the in-flight job guard (section 2) while a job is pending/in progress. After a completed/errored job it starts a new `Decompose*`, which sets status `planning` again.
- `internal/autonomous/manager.go:3096` `Manager.ResetToDraft(id, actor)`: **refuses `PRDRunning`, `PRDActive`, `PRDPlanning`** (`:3102-3103`, error `"prd %q is %s; stop it before resetting to draft"`) and `PRDArchived` (`:3104-3105`). Otherwise sets draft, clears `Story`, approval fields, `DecomposeSessionFailures`, a `decompose-session` `LastDecomposeFailure` (`:3112-3118`), records decision `reset_to_draft`, `emitPRDStatus`. It does not release a ledger waiter.
- Surfaces: REST `case "reset_to_draft"` `internal/server/autonomous.go:1114`; `internal/autonomous/api.go:852` `API.ResetToDraft`; MCP `autonomous_prd_reset_to_draft` tool `internal/mcp/autonomous.go:930`, handler `:935`; comm `internal/router/sx2_parity.go:1669`; PWA button `internal/server/web/app.js:29167` (`prdAction(..., 'reset_to_draft', ...)`). **No CLI command** for reset-to-draft exists (grep of `cmd/datawatch` for `reset_to_draft`/`reset-to-draft` finds nothing). Automation-level: MCP `automation_return_to_draft` `internal/mcp/automation_tools.go:229`; comm `automation_cmd.go` verb `return-to-draft`.
- Cancel path used to drop a queued planner: `internal/autonomous/api.go:825` `API.Cancel` (cancelRun `:813`, kill sessions, status `PRDCancelled`, decision `cancel`). A queued planner notices on the next poll tick via `plannerCancelled` (`capacity.go:590`). `Reject` `manager.go:3039` also counts.
- Gap to close: nothing in `ResetToDraft`, `SetPRDLLM`, `SetAutomationLLM` or the in-flight guard re-runs admission for a queued planner.

## 4. Automaton-level worker backend/model/effort setters, per-task overrides, and the status gate

Setters (all in `internal/autonomous/manager.go` unless noted; API wrappers in `internal/autonomous/api.go`):

| Setter | Manager | API wrapper | Status gate |
|---|---|---|---|
| Automaton worker LLM (backend/effort/model + decomposition) | `:5053` `SetPRDLLM` | `api.go:1242` | `prdLLMOverrideAllowed` `:5049`: anything except running/completed |
| Automaton guardrail LLM | `:5117` `SetPRDGuardrailLLM` | `api.go` (grep) | same as SetPRDLLM (inline check running/completed) |
| Automaton verification LLM | `:5400` `SetPRDVerificationLLM` | `api.go` | `verificationLLMLocked` `:5159` |
| Story worker LLM | `:4567` `SetStoryLLM` | `api.go:1130` | `taskOverrideLocked` |
| Task worker LLM | `:4612` `SetTaskLLM` | `api.go:1139` | `taskOverrideLocked`; also clears `AdmittedBackend/AdmittedModel` (`:4633`) |
| Task guardrail LLM | `:4652` `SetTaskGuardrailLLM` | (grep api.go) | `taskOverrideLocked` |
| Task/Story thinking level | `:4857` `SetTaskThinkingLevel`, `:4818` `SetStoryThinkingLevel` | `api.go:1353` (task) | `taskOverrideLocked` |
| Task/Story verification LLM | `:5308` `SetTaskVerificationLLM`, `:5357` `SetStoryVerificationLLM` | `api.go:1267`, `:1274` | `taskOverrideLocked` |
| Task verifier policy | `internal/autonomous/verifier_policy_set.go:92` | `api.go:1290` | `taskOverrideLocked` |
| Task effort ceiling | `internal/autonomous/effort_ceiling.go:230` | `api.go:1367` | see file |
| Task project dir | `internal/autonomous/task_project_dir.go:41` | `api.go:1231` | see file |
| Automation default LLM | `internal/autonomous/automation_set_llm.go:88` | `api.go:1981` | Automation draft/active/paused/blocked |
| Automation verification LLM | `automation_set_llm.go:126` `SetAutomationVerificationLLM` | `api.go` | same |

Pair checks before any write: `checkResolvedLLMPair` `manager.go:5282`, `checkLevelPair` `:5268`, `checkAutomationLLMPair` `automation_set_llm.go:43`.

The status gate (`autonomous.task_override_statuses`):
- Config field `internal/config/config.go:2678` `TaskOverrideStatuses` (doc comment `:2671`); manager copy `manager.go:328` (json `task_override_statuses`, no omitempty).
- Default (empty list means default): `manager.go:5196` `TaskOverrideStatusesDefault()` = needs_review, revisions_asked, cancelled, blocked; `:5203` `EffectiveTaskOverrideStatuses`; known set (14 names) `:5167-5173`, `:5177` `TaskOverrideStatusesKnown`, `:5182` `TaskOverrideStatusValid`; `:5213` `ValidateTaskOverrideStatuses`.
- Enforcement: `manager.go:5226` `Manager.taskOverrideLocked(prd, prdID)`; error text `prd %q status %q is locked; only %s accept LLM overrides (autonomous.task_override_statuses)`. Called by SetStoryLLM `:4572`, SetTaskLLM `:4617`, SetTaskGuardrailLLM `:4657`, SetStoryThinkingLevel `:4826`, SetTaskThinkingLevel `:4865`, SetTaskVerificationLLM `:5316`, SetStoryVerificationLLM `:5365` (grep `taskOverrideLocked(prd, prdID)`). On the branch only SetStoryLLM and SetTaskLLM gained a fall-back when this gate refuses (section 5b); the other callers still refuse.
- Validation on write: `internal/autonomous/api.go:395-410` (`API.SetConfig` probe + `ValidateTaskOverrideStatuses`); `internal/server/api.go:6606-6620` (PUT /api/config).
- Consequence at HEAD: a worker queued in `waiting_capacity` lives in a `running` PRD. `running` is NOT in the default list, so `set_task_llm` on a queued worker is refused by default.
- Consequence on the branch **[branch]**: the gate itself is unchanged (`taskOverrideLocked`, same list, same defaults), but `SetStoryLLM`/`SetTaskLLM` no longer return its error when the new `autonomous.override_queued_tasks` is on, and that setting defaults to ON (nil reads true, `EffectiveOverrideQueuedTasks` `manager.go:5243` [branch]). So on the branch an unstarted task in a running Automaton accepts the override by default. That is a behavior change to a documented operator gate (`docs/operations.md:~2715-2730` still says the opposite); it needs the operator's explicit confirmation before the branch merges. The explicit `false` restores the refusal. Do not change the `task_override_statuses` default; the gate is the operator's.
- Paths that change the effective backend without a setter (retry/resolve-review with `--backend`, dogfood finding 6) are not covered by the branch either.
- Doc/code mismatch to note (do not fix here): `docs/operations.md:~2727-2729` says Automaton-level overrides stay "locked to the 3 pre-Run statuses", but `SetPRDLLM` actually allows everything except running/completed (`manager.go:5049`).

## 5. A task's queued capacity request: create, cancel, release

Worker path, `internal/autonomous/executor.go` -> `internal/autonomous/capacity.go`:
- Resolve once: `resolveTaskBackendModel` `executor.go:3061` (task -> story -> PRD -> Automation -> global), `resolveTaskEffort` `:3162`, `admittedWorker` `:3052`; the executor resolves around `:1520`, records `recordAdmittedWorker` (`executor.go:1595`, defined `:3036`; sets `Task.AdmittedBackend/AdmittedModel`, `models.go:1144-1145`), then admits at `:1620-1621`: `m.resetCapacityWait(t.ID)` then `m.admitPreferring(ctx, prd, t, backend, model, capacity.RoleWorker, prevNode)`. `errCancelledWhileWaiting` (`capacity.go:28`) at `:1622` ends the attempt quietly. Later failover/node-load: `internal/autonomous/node_load.go:~53-63`, `capacity.go:497` `replaceStaleLease` (release `:501`, re-acquire `:502`).
- Create: `capacity.go:347` `admit` -> `:355` `admitPreferring` (budget wait `waitAutomationBudget` for workers, `automation_budget_admit.go:~84`) -> `:368` `acquireLease`. It builds `capacity.Request{Holder: t.ID, PRDID, Priority, Pools, Node, NodeChoices, ExcludeNodes, PreferNode, Model, Role, Consumer}` (`:384-386`), `req.Refresh = m.placementRefresh(req, backend, model, nil)` (`:387`, again bound to the original backend/model), optional fall-through `pickFallthrough` (`:1031`, called at `:395`) for workers, bound = `capacityWaitRemaining` (`:264`) less time already spent (`:406-413`), then `led.Acquire(ctx, req, bound, taskCancelled, onWait)` (`:415`). `onWait` calls `nameRequestedPool`, audit `rec.AdmissionWait`, and `setWaiting` (`:316`, status `TaskWaitingCapacity`, `WaitReason`). On grant: `clearWaiting` (`:329`) and audit `AdmissionAdmitted`.
- Cancel: `taskCancelled` `capacity.go:309` (task status `TaskCancelled`); `Manager.CancelTask` `manager.go:3770` (sets `TaskCancelled`, then `m.releaseCapacity(taskID)` at `:3827`); the ledger drops the waiter on the next poll tick and `acquireLease` returns `errCancelledWhileWaiting` (`:443`).
- Release: `capacity.go:762` `releaseCapacity(taskID)` -> `Ledger.Release`; `:743` `releaseForRelaunch` (worker + `VerifyHolder(taskID)` = `"verify:"+id`, `:775`); `HandoffVerifyLease` `:795`, `judgeHandoff` `:817`, `JudgeHolder` `:785`; executor defers release at `executor.go:1256`; many other call sites (`grep -n "releaseCapacity(" internal/autonomous/*.go`).
- Adoption after restart: `adoptCapacity` `capacity.go:652` (`led.Adopt`), `markCapacityAdoptionComplete` `:686`; a `waiting_capacity` task at boot is requeued by the restart path at `manager.go:1976-1990`.
- Bound/timeout: `capacityWait()` `:289` (config `capacity_wait_timeout_seconds`, default `defaultCapacityWait` 4h `:58`; `ErrWaitTimeout` from the ledger).
- A reset also drops admitted pair: `manager.go:3564`; `failure_escalation_review.go:73`.

## 5b. The worker requeue as implemented on the branch (commit `25df3e2d2`)

All anchors are **[branch]** (`git show 25df3e2d2:<path>`). 15 files: `cmd/datawatch/main.go`
(+1), `internal/autonomous/{api.go,manager.go,prd_view.go,queued_task_override.go}`,
`internal/capacity/ledger.go`, `internal/config/config.go`,
`internal/server/{api.go,autonomous.go,autonomous_view.go}`, and 6 test files. No docs,
no MCP/CLI/comm/web-UI file is touched.

Shared helper (the piece the planner must reuse):
- `internal/capacity/ledger.go:1101` `func (l *Ledger) Retarget(holder string, req Request) bool`. Under the ledger lock: a holder with a live lease is left alone (returns false); a holder with no queued waiter returns false; otherwise it applies `req.Refresh()` (Pools/Node/NodeChoices/ExcludeNodes), strips a stale node (`withoutStaleNode`), returns false if `samePlacement` says nothing moved, else **replaces the whole `w.req`** (so `Model`, `Role`, `Refresh`, `Priority` come from the new request) while keeping the waiter's `seq`, `since` and grant channel, then `signalChangedLocked()` and `dispatchLocked()` (a free pool grants in the same call). Returns true only on a real move. One holder can therefore never have two live requests. Helpers: `samePlacement` `:1136` (pool set order-insensitive, Node, ExcludeNodes, NodeChoices), `slicesEqualStrings` `:1166`. Tests: `internal/capacity/ledger_retarg_test.go` `TestRetarget_MovesQueueEntryToNewPool` `:13`, `TestRetarget_SamePlacementIsNoop` `:81`, `TestRetarget_RefusesUnknownAndHeld` `:111`.
- `internal/autonomous/queued_task_override.go:222` `func (m *Manager) requeueQueuedCapacity(prd *PRD, t *Task, backend, model string) bool`: returns false if no ledger / `!capacityOn()` / no pools; resolves pools via `m.capacityKeys` and `nodeChoicesFor`; builds a `capacity.Request{Holder: t.ID, PRDID, Priority, Pools, Node, NodeChoices, Model, Role: RoleWorker, Consumer: consumerSessionSpawn}` with `Refresh = m.placementRefresh(req, backend, model, nil)`, then `led.Retarget(t.ID, req)`. It is worker-only (hard-coded `Holder: t.ID`, `RoleWorker`, no `decomposePools`); the planner needs the same call with `Holder: "plan:"+prd.ID`, `RolePlanner`, `decomposePools`.

Setters and gate fall-back:
- `internal/autonomous/manager.go:4582` `SetStoryLLM` and `:4636` `SetTaskLLM` now return `(*PRD, *LLMOverrideResult, error)`. When `taskOverrideLocked` (`:4587`, `:4641`) refuses and `EffectiveOverrideQueuedTasks(m.cfg.OverrideQueuedTasks)` (`:4588`, `:4642`) is true they call `applyQueuedStoryLLM` / `applyQueuedTaskLLM` instead of returning the refusal. Classic-accepted and refused calls return a nil result. `EffectiveOverrideQueuedTasks` is at `manager.go:5243`, config mirror `Config.OverrideQueuedTasks *bool` `manager.go:333`. Every other setter (`SetTaskGuardrailLLM`, thinking levels, verification LLMs, `SetPRDLLM`) is unchanged.
- `internal/autonomous/queued_task_override.go`: `LLMOverrideOutcome` `:45` (`task_id`, `reason`, `capacity_moved`), `LLMOverrideResult` `:61` (`changed`, `left_alone`), `unstartedTask` `:76` (pending, queued, waiting_capacity, waiting_decision, awaiting_approval), `taskLeftAloneReason` `:86`, `applyQueuedStoryLLM` `:109`, `applyQueuedTaskLLM` `:170`.
  - Story path writes story `Backend/Effort/Model`, appends a `set_story_llm` Decision, saves, then for each task: not unstarted -> `left_alone`; unstarted -> `changed`, and if `waiting_capacity` and backend non-empty it re-resolves with `resolveTaskBackendModel` and calls `requeueQueuedCapacity` (`:152-160`).
  - Task path writes `Backend/Effort/Model`, **clears `AdmittedBackend/AdmittedModel`** (`:190`), appends a `set_task_llm` Decision, saves, then requeues only when `oldStatus == TaskWaitingCapacity && backend != "" && oldBackend != backend` (`:202`). Consequence: a model-only change, or clearing the backend to inherit, does not requeue; the story path has no such comparison.
  - The pair check `checkResolvedLLMPair` runs before any write in both paths.
- Reply plumbing: `internal/autonomous/api.go:1143` `API.SetStoryLLM` and `:1155` `API.SetTaskLLM` return `SetLLMOut{PRD, Override}` (`:1171`) only in queued mode, else the bare PRD; `SplitSetLLMOut` `:1180` unwraps. `internal/server/autonomous.go:1986-1987` and `:2053-2054` call it, then `writeAutonomousResultOverride` (`internal/server/autonomous_view.go:44`): full view becomes `{"prd":…,"override":…}` (shape change vs the bare PRD, only in queued mode), compact view puts it in `WriteView.Override` (`internal/autonomous/prd_view.go:18`).
- Config: `internal/config/config.go:2698` `OverrideQueuedTasks *bool` (yaml/json `override_queued_tasks`, doc comment `:2679-2697`); bridge `cmd/datawatch/main.go:4430`; read `internal/server/api.go:6236` (effective value in `GET /api/config`); write validation `internal/server/api.go:6626-6642` (bool or ParseBool-able string, else 400) and `internal/autonomous/api.go:423-433` (`API.SetConfig`, JSON boolean required); apply `internal/server/api.go:8547` (`applyConfigPatch` case `autonomous.override_queued_tasks`).
- Tests: `internal/autonomous/queued_task_override_test.go` `TestQueuedTaskOverride_TaskLevelDefaultOn` `:24`, `_TaskLevelDisabledRefuses` `:48`, `_TaskLevelCustomListStaysLocked` `:71`, `_ClassicPathNilResult` `:91`, `TestQueuedStoryOverride_SplitsChangedAndLeftAlone` `:104`, `TestQueuedTaskOverride_RetargetsWaitingCapacity` `:153`, `_RetargetTwiceKeepsOneLiveRequest` `:211`; existing tests updated for the 3-value return and pinned to the old refusal with `OverrideQueuedTasks=&false` (`task_override_statuses_test.go`, `model_backend_refusal_test.go`, `bl416_guardrail_llm_setter_test.go`). The branch also contains three gofmt-only alignment hunks in `manager.go` (around `:274`, `:287`, `:892`) that are noise.
- Added by the worker-requeue-tests task (this worktree, `automaton/f1f20051`):
  - `internal/autonomous/worker_requeue_test.go` — the 7 requirement tests: `TestWorkerRequeue_RunningStory_ChangesAllAndRequeuesWaiting` (pending + waiting → both changed, the waiting one requeues on the new pool and gets its lease there once the pool frees), `TestWorkerRequeue_RunningStory_StartedAndFinishedLeftAlone` (both left alone with reasons; task records and capacity state untouched), `TestWorkerRequeue_TaskLevel_WaitingTaskRequeues` (per-task override requeues; on release the single live request lands as a lease on `node:fastb`), `TestWorkerRequeue_Disabled_RestoresClassicRefusal` (explicit false → classic `is locked` refusal, nil result, nothing written), `TestWorkerRequeue_StatusGate_GovernsClassicVersusQueued` (default list AND a custom list decide classic (nil result) vs queued-mode (non-nil result)), `TestWorkerRequeue_TwoChanges_SingleLiveRequestOnLatestPool` (exactly ONE waiter, now on `node:quick`; busy:solo/fastb/quick still hold their slots — no capacity leak), `TestWorkerRequeue_SecondChangeAfterStart_LeavesStartedTaskAlone` (first change requeues, task admits and starts; second change → changed empty / left_alone with reason, record and capacity untouched). All on `fakeCapClock`; observations via `pollUntil` + `Ledger.Snapshot()`; no test sleeps.
  - `internal/autonomous/queued_task_override.go` (task-level path, required for requirement 7 to hold): the started-task guard now also applies in `applyQueuedTaskLLM` — a task that is running or already finished (any non-unstarted status) is returned in `left_alone` with the reason and its record is left exactly as it was, matching the `SetTaskLLM` contract documented in the function comment; and `unstartedTask` now treats the empty (never-scheduled) status as unstarted so freshly created tasks stay overridable in both paths (keeps `TestQueuedTaskOverride_TaskLevelDefaultOn` / `_TaskLevelCustomListStaysLocked` green).

Hazards a later task must know before building on the branch (verified against HEAD code; the branch does not address them and its tests do not exercise them):
1. **Frozen worker pair after the grant.** The executor resolves `backend, model` once at `internal/autonomous/executor.go:1519`, calls `m.admitPreferring(ctx, prd, t, backend, model, ...)` at `:1621`, and later spawns with those same locals (`Backend: backend` `:1706`, `Model: model` `:1708`). `Retarget` swaps the ledger request, but the blocked `acquireLease` call and the executor frame still hold the OLD backend/model, so the worker would spawn on the old backend holding a lease booked on the new pool, until the next attempt re-resolves. `queued_task_override_test.go:153` retargets a test-created waiter (`queueWaiter`), not the executor's own `Acquire`, so it never sees this. The fix must make the post-grant code re-resolve (or have `acquireLease` return the superseded target) rather than rely on the ledger move alone.
2. **Wait reason still names the old pool.** The original `onWait` closure (`capacity.go:415`, the `led.Acquire` call) keeps calling `nameRequestedPool(reason, requested)` (`:419`) and `rec.AdmissionWait(..., pools)` (`:423`) with the pools captured at entry (`requested := pools`, `capacity.go:390`). After `Retarget` the ledger reason text is for the new queue but the name substituted into it is the old one. `Task.WaitReason`/`PRD.WaitReason` therefore need the requested-pool name to be re-derived, not captured.
3. **Wait budget.** `Retarget` does not change the in-flight `Acquire` timeout; the worker bound is the one computed at `capacity.go:406-413` (`capacityWaitRemaining` `:408`, passed to `led.Acquire` `:415`), so a requeue does not reset it.
4. `requeueQueuedCapacity` ignores `PreferNode`, `ExcludeNodes` and the worker fall-through (`pickFallthrough`), so a task that was queued with an exclusion list or on an alternate pool is retargeted without them.
5. The default-ON behavior change and the `{prd, override}` reply-shape change (see section 4 and above) are operator-visible and undocumented: no `docs/*`, no `docs/config-reference.yaml`, no MCP `autonomous_config_set` text, no PWA settings field, no locale strings, no openapi change on the branch (section 8).

## 6. Every surface that sets these values

Planning/decompose profile and Automaton worker LLM (PRD level):
- REST: `POST /api/autonomous/prds/{id}/set_llm` `internal/server/autonomous.go:1862` (body `backend, effort, model, decomposition_profile, decomposition_model, guardrail_*`; validates names against `inferenceReg` `:1895-1909` (`:1897`, `:1905`); calls `SetPRDLLM` `:1919`).
- REST story/task: `set_story_llm` `:1948`, `set_task_llm` `:1989`; verification: `set_task_verification_llm`/`set_story_verification_llm`/`set_verification_llm` `:2114`.
- REST decompose: `:924` (+ stream/status `:929-933`) -> `internal/server/autonomous_decompose.go:100`, `:205` `handleDecomposeStream`, `:285` `handleDecomposeStatus`.
- REST Automation: `handleAutomationControl` `internal/server/autonomous.go:4258`; `case "set_llm", "set_verification_llm"` `:4332` (planning keys rejected for the verification op `:4336`); op table `internal/server/automation_ops.go:32`.
- MCP (`internal/mcp/autonomous.go`): `autonomous_prd_decompose` `:883` (handler `:889`), `autonomous_prd_reset_to_draft` `:930`, `autonomous_prd_set_llm` `:1384` (handler `:1395`), `autonomous_prd_set_story_llm` `:1426` (handler `:1435`), `autonomous_prd_set_task_llm` `:1449` (handler `:1460`), `autonomous_prd_set_task_verification_llm` `:1605`, `autonomous_prd_set_verification_llm` `:1635`; registrations `internal/mcp/server.go:624-626`. Config tool `autonomous_config_set` (mentions `task_override_statuses` `:85`, `:98`, `:271`). Verifier policy tool `internal/mcp/verifier_policy_tools.go:33`.
- MCP Automation: `automation_set_llm` tool `internal/mcp/automation_tools.go:533` (handler `:536`, fields `automationLLMFields` `:521`, tool builder `:525`), `automation_set_verification_llm` `:546`; registered `internal/mcp/server.go:1466-1467`.
- CLI: `cmd/datawatch/cli_autonomous.go:917` `prd-plan` (alias `prd-decompose`), `:1338` `prd-set-llm`, `:1357` `prd-set-story-llm`, `:1376` `prd-set-task-llm`, `:1468` `prd-set-task-verification-llm`, `:1485` `...story...`, `:1501` `prd-set-verification-llm`; Automation: `cmd/datawatch/cli_automation.go:792` `automation set-llm <id>` (flags `--planning-backend/--planning-model`, test `automation_set_llm_cli_test.go`). No reset-to-draft CLI.
- Comm channel (`internal/router/sx2_parity.go`): `autonomous decompose` `:750`, `set-llm` `:1034`, `set-story-llm` `:1113`, `set-task-llm` `:1132`, `reset-to-draft` `:1669`; usage line `:641`. Automation verbs `internal/router/automation_cmd.go:216` (`set-llm`), handler `:299` `automationSetLLM`, usage `:48`.
- Web UI (`internal/server/web/app.js`): decompose stream `:15476` `_startDecomposeStream`; `_prdLLMCurrent` `:15233`; per-task set_task_llm `:16227`; per-story `:16402`; Automaton LLM modal title `:16432`, POST `set_llm` `:16469` (also `:15939`, `:16752`, `:24691`); reset-to-draft button `:29167`; Automation planning pair POST `:33977` (`_automationFormSavePlanning` `:33970`); status card task wait reason `:6340`, `:14953`, `:15011`. Tests: `app-llm-roles.test.js`, `app-llm-budget.test.js`, `app-decompose-error.test.js`, `app-progress-card.test.js`, `app-autonomous-settings-fields.test.js:91`.
- **PWA gap:** `grep -n wait_reason internal/server/web/app.js` hits only task-level lines (6340, 14953, 15011, 25533-25536, 26792). `PRD.WaitReason` (planner) is not rendered anywhere in the web UI. Parity rule: any UI surfacing here needs a datawatch-app parity issue.

Effect of the branch on these surfaces **[branch]**: only the REST handlers changed (`internal/server/autonomous.go:1986-1987` `set_story_llm`, `:2053-2054` `set_task_llm`). MCP (`handleAutonomousPRDSetTaskLLM` -> `llmSetterResult`, `internal/mcp/autonomous.go:828`, passes the REST body through as text), the CLI (`prd-set-task-llm` -> `daemonJSON`), the comm channel (`internal/router/sx2_parity.go:1132`, `prettyJSON(out)`) all forward the REST JSON unchanged, so the new `override` object reaches them as raw JSON with no tool description, flag, help text or formatting. The PWA task edit (`app.js:16227`) POSTs and ignores the body, so `capacity_moved`/`left_alone` are not shown to the operator. None of the four surfaces documents the new behavior or the `override_queued_tasks` setting. Planning surfaces (`set_llm`, `decompose`, `reset_to_draft`, `automation set_llm`) are untouched.

## 7. Existing test fakes and fixed-clock helpers

- Ledger clock interface: `capacity.Clock` (`internal/capacity/ledger.go:291`, `Now`, `After`); `Options{PollInterval, Clock, AutonomousBackpressure, NodeGate, ...}` (`ledger.go:313`).
- `internal/capacity/fake_clock_test.go:13` `fakeClock` (`newFakeClock` `:24`, `Advance` `:51`, `FireAll` `:68`, `Pending` `:78`, `waitPending` `:86`); package-private, usable only inside `internal/capacity`. Replan tests: `internal/capacity/replan_test.go` (`TestReplanMovesWaitersOffNodeTheListLoses` `:111`, etc.).
- `internal/autonomous/capacity_test.go:489` `fakeCapClock`, `newFakeCapClock` `:494`; `capFixture` `:14`, `capFixtureLocalEngine` `:405`, `capFixtureFakeClock` `:510`, helpers `capWaitUntil`, `capNodeReq`, `admitQueued`, `awaitAdmitted`, `newPRDWithTasks` `:~26`.
- `internal/autonomous/backoff_admission_test.go:24` `stepClock` (`newStepClock` `:35`, `waitTimers`), `backoffFixture` `:88`; `internal/autonomous/backoff_test.go:13` `fakeClock{t}` with `Advance` (for `Backoff.Clock`).
- `Manager.SetWaitClock(c capacity.Clock)` `capacity.go:146` injects the spawn-wait-loop clock (separate from the ledger clock).
- Decompose admission fixtures: `internal/autonomous/decompose_admission_test.go:25` `decomposeCapFixture` (host 4, `llm:ollama` 1, `node:n1` 2), helpers `prdWaitReason` `:40`, `heldBy` `:49`, `plannerQueued` `:58`; tests `:67` `TestDecompose_WaitsForLLMPoolAndProceeds`, `:135`, `:154` timeout clears wait reason, `:191` cancel while waiting, `:250`, `:295` host excluded, `:354`.
- Base fixture `internal/autonomous/executor_resume_test.go:29` `apiFixture(t)` (returns manager, API, fake spawn, fake verify; fake decompose fn returns a one-task plan); `fakeSpawn` `recursion_test.go:19`, `fakeSpawnCapture` `task_project_dir_executor_test.go:48`; `drainOnCleanup`.
- Status-gate tests: `internal/autonomous/task_override_statuses_test.go`, `task_override_statuses_config_test.go`. Wait-reason tests: `wait_reason_test.go`. Node-load failover: `node_load_test.go`.
- Pattern for the new tests: build with `decomposeCapFixture` but give the ledger `Clock: newFakeCapClock()`; fill `llm:ollama` with a holder; start `Decompose` in a goroutine; assert `plannerQueued` and `prdWaitReason`; call `SetPRDLLM(..., decompositionProfile="claude-code")` with the keys function returning a different pool for that backend; advance the fake clock; assert the old waiter is gone, the new pool is queued/held, and `PRD.WaitReason` names the new pool.

## 8. `autonomous.*` config: declared, defaulted, documented, exposed

- Declared: `internal/config/config.go` struct fields: `PlanningBackend` `:2596`, `PlanningEffort` `:2612`, `PlanningModel` `:2622`, `PlanningTimeoutSeconds` `:2682`, `TaskOverrideStatuses` `:2678`, `CapacityEnabled` `:2823`, `CapacityWaitTimeoutSeconds` `:2837` (default 14400 per comment). Manager mirror `internal/autonomous/manager.go` `Config` (`:133`, `:328`, `:568`); defaults `manager.go:877` `DefaultConfig`; bridge in `cmd/datawatch/main.go` (`:4429`, `:4451`).
- Exposed (read): `internal/server/api.go:6222` planning_backend (+ legacy key), `:6232` `task_override_statuses` (effective list), `:6287` `capacity_wait_timeout_seconds` in the `GET /api/config` autonomous block (`handleGetConfig` `:5789`).
- Exposed (write): `handlePutConfig` `internal/server/api.go:6589`; pre-validation `:6606-6620`; apply switch `:8498` (planning_backend), `:8520-8530` (task_override_statuses, array or CSV), `:9086` (capacity_wait_timeout_seconds). Dedicated REST `PUT /api/autonomous/config` via `API.SetConfig` `internal/autonomous/api.go:326` `API.SetConfig` (task_override_statuses probe `:395-410`). MCP `autonomous_config_get/set` `internal/mcp/autonomous.go:~74-98`. Comm `configure autonomous.<key>=<v>`. Web settings field `internal/server/web/app.js:17256` (`autonomous.task_override_statuses`).
- Documented: `docs/config-reference.yaml:2321` (`capacity_wait_timeout_seconds: 14400`), `:2459` (`planning_backend`), `:2740` (`task_override_statuses`); `docs/operations.md:~2715-2730`; `docs/mcp.md:293`, `:296`, `:855`; `docs/api/autonomous.md:106`, `:1358`; `docs/api/openapi.yaml` (grep `task_override_statuses`).
- **The branch's new key `autonomous.override_queued_tasks` [branch]** (`*bool`, nil reads true): declared `internal/config/config.go:2698`; manager mirror `internal/autonomous/manager.go:333`; bridged `cmd/datawatch/main.go:4430`; defaulted by `EffectiveOverrideQueuedTasks` `manager.go:5243` (no literal default in `DefaultConfig`); read in `GET /api/config` `internal/server/api.go:6236`; write-validated in `handlePutConfig` `internal/server/api.go:6626-6642` and in `API.SetConfig` `internal/autonomous/api.go:423-433`; applied by `applyConfigPatch` `internal/server/api.go:8547`. Not done on the branch: `docs/config-reference.yaml` entry, `docs/operations.md` text (it still describes the old refusal), `docs/mcp.md` / `autonomous_config_set` description, `docs/api/autonomous.md` / openapi (`docs/api/openapi.yaml`, `internal/server/web/openapi.yaml`) for the `override` reply object, the PWA settings field (compare `task_override_statuses` at `internal/server/web/app.js:17256` and `app-autonomous-settings-fields.test.js:91`), the locale files (`internal/server/web/locales/{en,de,es,fr,ja}.json`), CLI/comm help. Per the parity rule, adding it to the PWA needs a datawatch-app parity issue at merge.
- Design rule: reuse `capacity_wait_timeout_seconds` for the requeued request; add NO new config key unless the operator asks. If one is ever added it must be declared in config.go, mirrored in the manager Config, bridged in main.go, exposed in handleGetConfig/handlePutConfig, MCP, CLI/comm and PWA, and documented in config-reference.yaml, operations.md and docs/mcp.md (parity across all surfaces).

## 9. Operator docs

- `docs/howto/automata-capacity.md`: line 59-64 "Planning waits too" (decompose takes the same `llm:`/`node:` pools minus host, PRD parks in `planning` with a visible wait reason); `:321-331` planning-call gating (wait reason example, planner ranks after workers and verifiers). Update both when the requeue ships.
- `docs/howto/autonomous-planning.md` (planning flow; capacity mention near `:416`), `docs/howto/autonomous-review-approve.md`, `docs/howto/autonomous.md`.
- `docs/api/autonomous.md` (REST surface), `docs/mcp.md`, `docs/operations.md`, `docs/config-reference.yaml`, `docs/api/openapi.yaml`.
- `docs/plans/2026-10-29-automation-dogfood-findings.md` (61 lines; findings 1, 6, 7 above). `docs/plans/README.md` holds the plan registry. Plan/prose-only edits get no version bump.

## 10. Design choices the later tasks MUST follow

Shared rule: **planning and workers use ONE requeue primitive, and it already exists on the branch: `capacity.Ledger.Retarget` ([branch] `internal/capacity/ledger.go:1101`).** Do not write a second ledger-level helper, and do not build the planner path on `Replan`, a cancel-and-reacquire loop, or a supersede sentinel.

1. **Land the branch first.** Planner work depends on `Retarget`, so commit `25df3e2d2` must be on `main` (merge or cherry-pick) before the planner task starts. It is a child of `df86c6434` and HEAD differs from that only by docs, so it should apply cleanly. The merge needs the operator's go-ahead on the default-ON `override_queued_tasks` (section 4) and a full `go build` + `go test ./internal/capacity ./internal/autonomous ./internal/server` because `SetStoryLLM`/`SetTaskLLM` changed signature.
2. **One autonomous-package wrapper for both holders.** Generalise `requeueQueuedCapacity` ([branch] `queued_task_override.go:222`) into a holder-neutral function, e.g. `requeueQueued(holder, prd, role, backend, model, trim func([]string) []string) bool`, and make the worker call it with `t.ID`, `RoleWorker`, `nil` and the planner with `"plan:"+prd.ID`, `RolePlanner`, `decomposePools` (`capacity.go:574`). `Retarget` replaces the whole `w.req` including `Role` and `Model` (section 5b), so the planner call MUST set `Role: RolePlanner` and strip `host` through `trim`; leaving either out would promote the planner ahead of workers/verifiers or stack a host lease on the decompose session (see the comment above `decomposeAdmitted`).
3. **The ledger move is not enough; the code after the grant must follow it.** Hazards 1-2 of section 5b apply to the planner too (`decomposeAdmitted` runs `m.decompose(req)` with the `DecomposeRequest` built before the wait, `capacity.go:567`; both retry loops build it with `Backend: backend`, `manager.go:2735`, `:2898`). Required: record the target a holder is queued on (per holder, `{backend, model}`, set before `Acquire`, cleared on every exit), have the requeue hook update it, and have the code that runs after the grant read the CURRENT target (re-resolve with `effectivePlanning` + `resolvePlannedModel` + `RequirePinnedModel` for the planner; re-run `resolveTaskExecutionLLM` for the worker at `executor.go:1621` unless escalation or `OperatorWorkerSet` pinned the pair) and rebuild the request / spawn with it. Add the assertion the branch lacks: the decompose / spawn that actually runs carries the NEW backend and model.
4. **Trigger on a change of the effective target, not of a field.** Compare the resolved `{backend, model}` before and after the write (planner: `effectivePlanning` + `resolvePlannedModel`; worker: `resolveTaskBackendModel`, `executor.go:3061`). The branch's `oldBackend != backend` test (task path only) misses a model-only change and a cleared override that now inherits another pair. A different backend with an identical pool set still needs the post-grant re-resolve of choice 3 even though `Retarget` reports "no move" (`samePlacement`).
5. **Planner triggers:** `SetPRDLLM` when `decomposition_profile`/`decomposition_model` change while `prd.Status == PRDPlanning` (`manager.go:5053`, writes at `:5076-5079`; status gate `prdLLMOverrideAllowed` `:5049` already allows planning); `SetAutomationLLM` (`automation_set_llm.go:88`) for each member PRD whose effective planning pair changed and that is `PRDPlanning`; a `/api/config` change of `autonomous.planning_backend`/`planning_model` for PRDs that inherit the global. Do NOT add a trigger to `ResetToDraft` or the decompose in-flight guard (`autonomous_decompose.go:109`); the requeue replaces the need for them.
6. **Immediate effect.** `Retarget` runs synchronously under the ledger lock, grants at once on a free pool, and signals the waiter, so no poll-tick latency and no new context map is needed. `Replan` (`ledger.go:933`, wired only to registry changes, `capacity_wiring.go:903`) cannot change backend and is not the mechanism.
7. **Wait budget unchanged.** `Retarget` does not restart the in-flight `Acquire` timeout: planning keeps the `capacityWait()` bound taken at `capacity.go:550`, workers keep the `capacityWaitRemaining` bound (`capacity.go:406-415`). Say so in the docs; do not silently reset it.
8. **Wait reason shows the pool of the current profile.** Make the requested-pool name dynamic: the worker `onWait` captures `requested := pools` (`capacity.go:390`, used `:419`) and the audit uses `pools` (`:423`); the planner passes the raw ledger reason to `setPRDWaitReason` (`capacity.go:551`) without `nameRequestedPool` at all. Both must read the holder's current target pools, and `PRD.WaitReason` must be cleared on every exit (`clearPRDWaitReason` `:624`, existing discipline at `:556`, `:566`).
9. **Do not touch** role ranking (planner last), `decomposePools`, `autonomous.task_override_statuses` defaults, `prdLLMOverrideAllowed`, the `ResetToDraft` refusal while planning (`manager.go:3102-3103`) or `.datawatch/verify/*`. Cancel and reject stay terminal (`errDecomposeCancelled`, `errCancelledWhileWaiting`); an in-place `Retarget` starts no new attempt, so it neither consumes decompose retry budget nor rolls the PRD back to draft.
10. **Config is the operator's.** The branch defaults `override_queued_tasks` to ON for every deployment; whether that default stands is the operator's call (other users run DataWatch). The planner requeue itself should need no new config key (reuse `capacity_wait_timeout_seconds`); if a key is added it needs the full section 8 treatment.
11. **Surfaces, docs and parity (undone on the branch).** Document `override_queued_tasks` and the `override` reply object (`changed` / `left_alone` / `capacity_moved`) in `docs/config-reference.yaml`, `docs/operations.md` (replace the "locked to the 3 pre-Run statuses" text at `~2727-2729`), `docs/mcp.md`, `docs/api/autonomous.md`, both openapi files, `docs/howto/automata-capacity.md` (`:59-64`, `:321-331`); add the PWA settings field and locale strings, render `PRD.WaitReason` and the `override` outcome in the PWA, surface `override` in the MCP tool text, CLI and comm help; file the datawatch-app parity issue at merge time. Plan/prose-only edits get no version bump; code changes bump both `Version` vars.
12. **Tests (fake clocks, no real sleeps), using the fixtures in section 7:** (a) queued planner moves to the new pool on `SetPRDLLM`; (b) same on `SetAutomationLLM` and on a global planning change; (c) the decompose that runs after the grant uses the new backend/model; (d) cancel while queued still returns `errDecomposeCancelled`; (e) a no-op write and a write that leaves the effective pair unchanged do not requeue; (f) `PRD.WaitReason` names the new pool and is cleared on grant, timeout and cancel; (g) worker: executor-path (not a hand-queued waiter) spawns with the new pair after `SetTaskLLM` at a running Automaton, model-only change requeues, one live request after two changes; (h) `Retarget` keeps `Role: RolePlanner` and omits `host`; (i) lease released on every exit path.

## 11. Corrections to earlier attempts' notes and to the verifier findings

- The worker requeue exists: commit `25df3e2d2` on branch `automaton/9e4d0001--tasks-implement-queued-task-override-and-reque-6cb90006` (section 5b). Earlier notes said it was not implemented and proposed a new helper; the helper is `Ledger.Retarget`.
- Anchors corrected in this version: planner request literal `capacity.go:546-548`, `Refresh` `:549`, `led.Acquire` `:550-552`; `taskOverrideLocked` calls at `manager.go:4572` (SetStoryLLM) and `:4617` (SetTaskLLM); `RequirePinnedModel(` calls at `manager.go:2714` and `:2882`; `PlanningEffort`/`PlanningModel` at `manager.go:140`/`:145`; `resolvePlannedModel` calls at `:2708` and `:2878`.
- `Ledger.Replan` (`ledger.go:933`) is NOT unused in production: `replanOnPlacementChange` (`cmd/datawatch/capacity_wiring.go:203`, wired `:903`) calls it on registry/node placement changes. It cannot re-route to another backend and is not the mechanism for this feature.
- `Approve` (`manager.go:2954`) is a review gate, not a decompose path.
- Restart-decompose and reset-to-draft-while-planning: section 3. Manager-side cancel of queued capacity requests: `errCancelledWhileWaiting` `capacity.go:28`, `errDecomposeCancelled` `:48` (section 5). How `autonomous.*` settings reach the config API: section 8.
- Comm-channel surfaces exist for every setter (section 6); the PWA has no PRD-level wait-reason render.

## 12. Changed files — planning requeue implementation (this worktree)

Feature: changing the decomposition profile/model of a planning (or queued) Automaton now
retargets the queued planner on the ledger to the new pool (`requeued`), leaves an already
started run alone and stores the value for the next run (`deferred_to_next_run`), or
confirms nothing moved (`unchanged`). `SetPRDLLM` returns the outcome; the decompose
handler re-queues on restart while queued; the wait reason always names the current
profile's pool. Built on `capacity.Ledger.Retarget` per section 10 (one primitive, planner
holder `"plan:"+prd.ID`, `RolePlanner`, `decomposePools`).

New:
- `internal/autonomous/planning_requeue.go` — `PlanningOutcome` (`requeued` / `deferred_to_next_run` / `unchanged`), `PlanningRequeueResult` (old/new backend, model, pools + human reason), `plannerPairFor`, `plannerPoolsFor`, `requeuePlanningRun` (shared requeue path used by the worker story too), `planningRequestAtGrant` (the run that executes after the capacity grant re-resolves backend/model from the PRD's CURRENT record, so the execution carries the pair the queue entry was retargeted onto — falls back to the captured pair when the current pair is unresolvable or unpinned, the same admission rule `requeuePlanningRun` applies), `API.RestartPlanning` manager-side, `plannerWaitReason`, decision note. Ordering: a live lease (started run) is detected BEFORE the pool-math short-circuit, so a started run is always `deferred_to_next_run`; time comes from the ledger's injected `capacity.Clock` only.
- `internal/autonomous/planning_requeue_test.go` — `TestPlanningRequeue_QueuedRun_RetargetsToNewPool`, `_RetargetedRunExecutesWithNewPair` (the retargeted waiter, once its new pool frees up, runs the decompose with the NEW backend AND model — mutation-verified: without the admission-pair fix it executes with the pre-wait pair), `_TwoChanges_SingleQueueEntry` (exactly one live queue entry, no leaked capacity, and the admission executes the LATEST pair), `_NoMoveIsUnchanged`, `_StartedRunDeferred`, `_RestartWhileQueued`, `TestSplitSetLLMOut_PlanningRoundsTrip`. Fixtures inject `fakeCapClock` (`capacity.Clock` dependency); the ledger admits only on `Release`/`Retarget` dispatch, so nothing depends on wall-clock polls. Note `Ledger.SetLimit(pool, 0)` means UNLIMITED, so "busy pools" are simulated by holding each pool's single slot with busy consumers.
- `internal/autonomous/planning_requeue_table_test.go` — `TestPlanningRequeue_Table`: table-driven requirements coverage (all 7 cases: queued profile change retargets and requeues; queued model-only change retargets and requeues; started run deferred and the next run queued on the new pool; two queued changes leave exactly one live request on the latest pool with no leaked capacity; changes with a start in between requeue then defer; wait reason names the current profile's pool after each change; restart-decompose re-queues while queued and answers plainly while started). Reuses the capacity fakes — `fakeCapClock` (a `capacity.Clock` whose `After` channels never fire, so admission happens only on `Release`/dispatch, never on real time) plus a `runGate` that holds a started decompose so the test observes "the run is running"; the pool map is model-sensitive (profile `pb` and model `m2` both resolve to `llm:pb`) so both change kinds are observable. Every observation polls the ledger snapshot; no test sleeps for the system to catch up.
- `internal/server/planning_requeue_helper.go` — `writeAutonomousPlanningResult` (reply `{prd, planning}` / view `planning`, mirroring `writeAutonomousResultOverride`).

Changed (non-test):
- `internal/autonomous/capacity.go` — `decomposeAdmitted` executes `m.decompose(m.planningRequestAtGrant(prd, req))` at the admission grant: the decompose request that runs carries the PRD's CURRENT decomposition pair (a queued entry may have been retargeted onto this pool mid-wait), not the pair resolved before the wait.
- `internal/autonomous/manager.go` — `SetPRDLLM` now returns `(*PRD, *PlanningRequeueResult, error)` and calls `requeuePlanningRun` when the decomposition pair changes; `RestartPlanning` for the decompose/restart path.
- `internal/autonomous/api.go` — `SetLLMOut.Planning`, `API.SetPRDLLM` forwards the new value, `SplitSetLLMOut` returns `(any, *LLMOverrideResult, *PlanningRequeueResult)`, `API.RestartPlanning` + `AutonomousAPI` entry (MCP/CLI/comm unchanged: they see the 2-value API).
- `internal/autonomous/prd_view.go` — `WriteView.Planning any`.
- `internal/server/api.go` — wiring for the planning outcome.
- `internal/server/autonomous.go` — `set_llm` handler captures the third `SplitSetLLMOut` value (nil-safe type assertion, typed-nil guarded) and writes the planning object.
- `internal/server/autonomous_decompose.go` — decompose handler re-queues a queued planner on the CURRENT pool via `RestartPlanning` (requirement d) and surfaces the explanation for a started run.
- `internal/server/autonomous_view.go` — planning-result JSON writer plumbing.

Changed (tests, signature updates for 3-value `SetPRDLLM` / 3-value `SplitSetLLMOut`):
- `internal/autonomous/automation_planning_test.go`, `bl320_decomp_profile_test.go`, `bl321_decomp_priority_test.go`, `bl416_guardrail_llm_setter_test.go`, `decompose_streaming_model_test.go`, `lane_fill_coordinator_test.go`, `model_backend_refusal_test.go`, `set_prd_llm_fields_test.go`
- `internal/server/orchestrator_enrich_test.go`, `set_llm_fields_preserved_test.go`

Verification: `go vet ./...` clean; full `go test ./internal/... -count=1` green (11078 tests /
93 packages); targeted `go test ./internal/autonomous/ -run 'TestPlanningRequeue|TestSplitSetLLMOut'
-count=1` all PASS. No version bump, no `gh`, no daemon start.

## 13. Changed files — surface wiring (task 6cb90009, this worktree)

This task exposes the queued-requeue + per-task-override outcomes on every surface
(REST, MCP, CLI, comm-channel, web) and stops disabling the worker LLM/effort
controls for a running Automaton while tasks can still change. On top of the
section-12 planning-requeue plumbing, the surface wiring touched:

- `internal/autonomous/api.go` — `SplitSetLLMOut` returns the planning outcome; the
  set-llm write view carries both `.override` (changed / left_alone — started/finished
  targets the override applies to on their next run) and `.planning` (requeued /
  deferred_to_next_run / unchanged + old/new pools); `API.SetPRDLLM` forwards it.
- `internal/autonomous/manager.go` — `SetPRDLLM` runs the override + requeue and
  returns both outcomes; a PRD that is `running` stays settable when
  `override_queued_tasks` is on (per-task override path) — the running-state gate is
  split so worker-pair changes on a running PRD are only refused when the override
  can't reach any not-yet-started task.
- `internal/autonomous/queued_task_override.go` — `LLMOverrideResult.Changed` /
  `.LeftAlone` classification; a task is overridable only while it is genuinely
  unstarted (the `pending`/`queued`/`waiting_*`/`awaiting_approval` family) — failed /
  blocked / review_needed targets are reported as left-alone, never silently applied.
- `internal/autonomous/capacity.go` — `decomposeAdmitted` runs the decompose with the
  PRD's current planning pair (a queued entry may have been retargeted mid-wait);
  the wait reason names the CURRENT pool after a retarget.
- `internal/server/autonomous_view.go` — the write-view JSON writer emits
  `{...prd, override, planning}` on every set-llm / set-decompose write.
- `internal/mcp/automation_tools.go` + `internal/mcp/autonomous.go` — the config tool
  describes/exposes `override_queued_tasks` + the planner fields; the set-llm / decompose
  tool descriptions document the 2xx body's `override` + `planning` outcome shape (tools
  are thin proxies over the REST endpoints, so the payload flows through unchanged).
- `cmd/datawatch/` (unchanged) — the CLI already pretty-prints the full 2xx body, so
  the `override` + `planning` fields surface automatically in `autonomous set-llm` and
  `autonomous decompose` output without further code.
- `internal/router/automation_cmd.go` — comm `automation set-llm` prepends a one-line
  requeue/override summary above the JSON body; comm `config-set`/`config-get` are
  key-permeable so `override_queued_tasks` + planner fields pass through.
- `internal/server/web/app.js` — the PWA (a) stops disabling worker LLM/effort controls
  for a running/structural-edit Automaton while it still has an overridable (not-
  yet-started) task, disabling them per task only for started/finished tasks, and
  (b) renders the PRD wait reason and a set-llm outcome summary (changed / left-alone /
  requeued / deferred) inline + as a toast.

New tests: `internal/server/web/app-set-llm-outcome.test.js` (web outcome helpers),
`internal/autonomous/queued_task_override_test.go` (changed/left-alone classification,
failed/blocked/review_needed refused), `internal/server/autonomous_view_test.go` (write
view `.override` + `.planning` shape), plus signature updates in
`internal/autonomous/{automation_set_llm_test,bl320_decomp_profile_test}.go` and
`internal/router/automation_set_llm_cmd_test.go`.

## 14. Changed files — operator docs (this task)

Docs-only; no code, no version bump. Documents the queued-requeue + queued-task-
override behaviour for operators (findings 1 and 7 of
`docs/plans/2026-10-29-automation-dogfood-findings.md`, now marked addressed).

Changed:
- `docs/howto/autonomous-planning.md` — new section "Changing the planning
  backend while planning": queued run retargeted in place (`requeued`, wait
  reason names the new pool, no new attempt, retry budget untouched, wait
  budget not reset), started run left alone (`deferred_to_next_run`), no-move
  (`unchanged`), no-op re-target leaves one queue entry, restart-decompose via
  `decompose`/`plan`, the `planning` reply object with example, and
  CLI / MCP / REST / comm examples.
- `docs/howto/automata-capacity.md` — "Planning waits too" bullet now points
  at the retarget behaviour; new "Retargeting while queued" section (planner
  retarget under the same rules, worker retarget with `capacity_moved`, link
  out for the full report).
- `docs/operations.md` — LLM override section: "Accepted while" split into the
  classic gate (unchanged) and the new `autonomous.override_queued_tasks`
  queued mode (default on, unstarted tasks changed, started/finished/parked
  left alone with the reason, `waiting_capacity` retargeted, `override`
  reply object with example, how to turn it off, CLI / MCP / REST / comm
  examples); new "Planning requeue" subsection after the per-PRD planning
  settings with the `planning` object, the three outcomes and the
  restart-decompose path.
- `docs/config-reference.yaml` — `autonomous.override_queued_tasks` entry
  (nil reads true; explicit false restores the classic refusal; all config
  surfaces listed), next to `task_override_statuses`.
- `docs/mcp.md` — `autonomous_prd_set_llm` and `autonomous_prd_set_task_llm`
  bullets document the `planning` / `override` reply objects and the queued
  mode; the config-tool bullet gains `override_queued_tasks` beside
  `task_override_statuses`.
- `docs/api/autonomous.md` — new "Override outcomes on a running or queued
  Automaton" section (queued mode, `override` object, `capacity_moved`,
  classic mode, config surfaces) and new "Planning requeue" section (queued /
  started / no-move, `planning` object, outcome table, no-op and cancel
  semantics, links to the howtos).
- `docs/plans/2026-10-29-automation-dogfood-findings.md` — findings 1 and 7
  of the 2026-11-03 set marked addressed with pointers to the docs above
  (finding 6 remains open: the retry/resolve-review admission path is not
  covered by this work, per section 5b of this report).

Not changed (out of scope here): the two openapi files' `override` /
`planning` response schemas, the PWA settings field and locale strings for
`override_queued_tasks`, and CLI/comm help text — the CLI already
pretty-prints the full 2xx body and the comm channel passes the outcome
through, so both surfaces work today; the PWA + openapi parity items stay
with the merge per the parity rule (section 8).

Corrections after final source cross-check (manager.go `SetPRDLLM`,
`applyQueuedTaskLLM` / `applyQueuedStoryLLM` in queued_task_override.go):

1. A task-level override call aimed at a task that already started or
   finished is **not refused** — it answers 2xx with the task reported in
   `left_alone` (record untouched). The docs no longer say it is refused
   with the classic lock message (operations.md, api/autonomous.md,
   mcp.md, config-reference.yaml).
2. The `planning` object is present on `set_llm` whenever a planning field
   (`decomposition_profile` / `decomposition_model`) is part of the call —
   reading `unchanged` when nothing moves (effective pair unchanged,
   no live planning run, or queue entry already on the target pool). The
   outcome descriptions in autonomous-planning.md, operations.md and
   api/autonomous.md now say so.
