# Discovery notes — requeue a queued planning run / queued worker on a new pool

Discovery only. No source file was changed. Every `path:line` below was read in this
worktree at HEAD `df86c6434` (v9.75.1). Line numbers drift; grep the quoted symbol
before editing. All paths are repo-relative.

Vocabulary: "Automaton" = PRD in code (`autonomous.PRD`). "Automation" = group of
Automata (`autonomous.Automation`). Planning = decompose.

**Spec discrepancy (read first).** The task spec says the worker-requeue part (item 5
and the worker setters) was already implemented by an earlier task. At HEAD `df86c6434`
it is not: `SetTaskLLM` (`internal/autonomous/manager.go:4612`) only rewrites the task
fields and clears `AdmittedBackend/AdmittedModel`; nothing re-queues a task already in
`waiting_capacity` (no ledger call, no supersede check, no hook; `grep -rn requeue
internal/autonomous` finds nothing relevant). Sections 4-5 map the worker side as it
actually stands, and section 10 treats the worker requeue as still to be built on the
same shared helper as planning. If the earlier task's branch exists elsewhere, it has
not been merged into this tree.

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
| Global fields (manager copy) | `internal/autonomous/manager.go:133` `Config.PlanningBackend` (+ effort `:139`, model `:144`); bridged from config at `cmd/datawatch/main.go` (~`:4429-4451` block building `autonomous.Config`) |
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
- `internal/autonomous/manager.go:2639` `Manager.Decompose` (sync twin; code comment says keep both in sync). It reads `effectivePlanning` at `:2667`, `backend := prd.DecompositionProfile` at `:2667-2669`, sets `PRDPlanning` at `:2682-2690`, `planModel := resolvePlannedModel` at `:2708`, `RequirePinnedModel` at `:2713`, then the retry loop calls `decomposeAdmitted` at `:2735`.
- Retry/failure helpers: `retryDecomposeFailure`, `failDecomposeAttempt` (`internal/autonomous/retry_policy.go:239`), `validateAndStorePlan` (`manager.go:2426`), `finalizeDecomposedPlan`.

Admission: `internal/autonomous/capacity.go:532` `Manager.decomposeAdmitted(prd, backend, model, req)`.
1. `capacityOn()` false (`:256`) or no pools/choices -> runs `m.decompose(req)` directly (`:534-546`).
2. Pools: `keysFn(backend, model)` then `decomposePools` (`:574`, strips `"host"`); node choices `nodeChoicesFor` (`:209`).
3. Holder is `"plan:" + prd.ID` (`:548`); request built at `:549-551` with `Role: capacity.RolePlanner`, `Consumer: consumerSessionSpawn` (`:39`), `Model: model`.
4. `lreq.Refresh = m.placementRefresh(lreq, backend, model, decomposePools)` (`:552`). **The Refresh closure captures the ORIGINAL backend/model**, so it only re-reads pool keys/nodes for the same LLM.
5. `led.Acquire(context.Background(), lreq, m.capacityWait(), cancelled=plannerCancelled, onWait=setPRDWaitReason)` (`:553-555`). Context is `Background` (not cancellable by the caller).
6. On error: `clearPRDWaitReason`; if `plannerCancelled` (`:590`, status cancelled/rejected) return `errDecomposeCancelled` (`:48`) else `fmt.Errorf("capacity: %w", err)` (`:556-566`).
7. On success: `defer led.Release(holder)`, `clearPRDWaitReason`, then `m.decompose(req)` with the already-frozen `req` (`:567-570`).

Wait reason text on the Automaton: `PRD.WaitReason` (`models.go:321`, json `wait_reason`), written by `setPRDWaitReason` (`capacity.go:605`, under `store.MutatePRD`, then `EmitPRDUpdate`) and cleared by `clearPRDWaitReason` (`:624`). The text is the ledger's own reason (e.g. `llm:ollama at 1/1 (1 waiting)`, "waiting for node X: ..."; built in `internal/capacity/ledger.go:657` `blockReasonLocked`, `:750` `resolveLocked`). The planner path passes the raw ledger reason. The worker path additionally calls `nameRequestedPool` (`internal/autonomous/wait_reason.go:476`) and `ClassifyWaitReason` (`wait_reason.go`) so a task wait reason names the pool it asked for; the planner path does not, so "the pool of the current profile" is not named in `PRD.WaitReason` today.

Ledger behavior (`internal/capacity/ledger.go`): roles ranked `:40` verifier < worker < planner (planner last); `Request.Refresh` doc `:93-102`; `dispatchLocked` `:904` applies Refresh to Pools/Node/NodeChoices/ExcludeNodes only (Holder, PRDID, Priority, Role, Model, Interactive are kept); `Replan` `:933`; `Acquire` `:967` (grant / ctx.Done / timeout `ErrWaitTimeout` / `cancelled()` true -> `context.Canceled`, all via `remove(w)` `:1053`); `Release` `:1180`; `Handoff` `:1218`; `Reap` `:1239`. `cancelled()` is polled only on the poll tick (`pollWaitLocked` `:1041`, `Options.PollInterval`).

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
- Enforcement: `manager.go:5226` `Manager.taskOverrideLocked(prd, prdID)`; error text `prd %q status %q is locked; only %s accept LLM overrides (autonomous.task_override_statuses)`. Called by SetStoryLLM `:4575`, SetTaskLLM `:4620`, SetTaskGuardrailLLM and the other task/story setters in the table.
- Validation on write: `internal/autonomous/api.go:395-410` (`API.SetConfig` probe + `ValidateTaskOverrideStatuses`); `internal/server/api.go:6606-6620` (PUT /api/config).
- Consequence for the fix: a worker queued in `waiting_capacity` lives in a `running` PRD. `running` is NOT in the default list, so `set_task_llm` on a queued worker is refused by default. The worker half of the requeue helper only matters when the operator adds `running` to the list, or for paths that change the effective backend without a setter (retry/resolve-review with `--backend`, finding 6). Do not change the default; the gate is the operator's.
- Doc/code mismatch to note (do not fix here): `docs/operations.md:~2727-2729` says Automaton-level overrides stay "locked to the 3 pre-Run statuses", but `SetPRDLLM` actually allows everything except running/completed (`manager.go:5049`).

## 5. A task's queued capacity request: create, cancel, release

Worker path, `internal/autonomous/executor.go` -> `internal/autonomous/capacity.go`:
- Resolve once: `resolveTaskBackendModel` `executor.go:3061` (task -> story -> PRD -> Automation -> global), `resolveTaskEffort` `:3162`, `admittedWorker` `:3052`; the executor resolves around `:1520`, records `recordAdmittedWorker` (`executor.go:1595`, defined `:3036`; sets `Task.AdmittedBackend/AdmittedModel`, `models.go:1144-1145`), then admits at `:1620-1621`: `m.resetCapacityWait(t.ID)` then `m.admitPreferring(ctx, prd, t, backend, model, capacity.RoleWorker, prevNode)`. `errCancelledWhileWaiting` (`capacity.go:28`) at `:1622` ends the attempt quietly. Later failover/node-load: `internal/autonomous/node_load.go:~53-63`, `capacity.go:497` `replaceStaleLease` (release `:501`, re-acquire `:502`).
- Create: `capacity.go:347` `admit` -> `:355` `admitPreferring` (budget wait `waitAutomationBudget` for workers, `automation_budget_admit.go:~84`) -> `:368` `acquireLease`. It builds `capacity.Request{Holder: t.ID, PRDID, Priority, Pools, Node, NodeChoices, ExcludeNodes, PreferNode, Model, Role, Consumer}` (`:~395-397`), `req.Refresh = m.placementRefresh(req, backend, model, nil)` (`:~398`, again bound to the original backend/model), optional fall-through `pickFallthrough` (`:1031`) for workers, bound = `capacityWaitRemaining` (`:264`) less time already spent, then `led.Acquire(ctx, req, bound, taskCancelled, onWait)` (`:~425`). `onWait` calls `nameRequestedPool`, audit `rec.AdmissionWait`, and `setWaiting` (`:316`, status `TaskWaitingCapacity`, `WaitReason`). On grant: `clearWaiting` (`:329`) and audit `AdmissionAdmitted`.
- Cancel: `taskCancelled` `capacity.go:309` (task status `TaskCancelled`); `Manager.CancelTask` `manager.go:3770` (sets `TaskCancelled`, then `m.releaseCapacity(taskID)` at `:3827`); the ledger drops the waiter on the next poll tick and `acquireLease` returns `errCancelledWhileWaiting` (`:443`).
- Release: `capacity.go:762` `releaseCapacity(taskID)` -> `Ledger.Release`; `:743` `releaseForRelaunch` (worker + `VerifyHolder(taskID)` = `"verify:"+id`, `:775`); `HandoffVerifyLease` `:795`, `judgeHandoff` `:817`, `JudgeHolder` `:785`; executor defers release at `executor.go:1256`; many other call sites (`grep -n "releaseCapacity(" internal/autonomous/*.go`).
- Adoption after restart: `adoptCapacity` `capacity.go:652` (`led.Adopt`), `markCapacityAdoptionComplete` `:686`; a `waiting_capacity` task at boot is requeued by the restart path at `manager.go:1976-1990`.
- Bound/timeout: `capacityWait()` `:289` (config `capacity_wait_timeout_seconds`, default `defaultCapacityWait` 4h `:58`; `ErrWaitTimeout` from the ledger).
- A reset also drops admitted pair: `manager.go:3564`; `failure_escalation_review.go:73`.

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
- Design rule: reuse `capacity_wait_timeout_seconds` for the requeued request; add NO new config key unless the operator asks. If one is ever added it must be declared in config.go, mirrored in the manager Config, bridged in main.go, exposed in handleGetConfig/handlePutConfig, MCP, CLI/comm and PWA, and documented in config-reference.yaml, operations.md and docs/mcp.md (parity across all surfaces).

## 9. Operator docs

- `docs/howto/automata-capacity.md`: line 59-64 "Planning waits too" (decompose takes the same `llm:`/`node:` pools minus host, PRD parks in `planning` with a visible wait reason); `:321-331` planning-call gating (wait reason example, planner ranks after workers and verifiers). Update both when the requeue ships.
- `docs/howto/autonomous-planning.md` (planning flow; capacity mention near `:416`), `docs/howto/autonomous-review-approve.md`, `docs/howto/autonomous.md`.
- `docs/api/autonomous.md` (REST surface), `docs/mcp.md`, `docs/operations.md`, `docs/config-reference.yaml`, `docs/api/openapi.yaml`.
- `docs/plans/2026-10-29-automation-dogfood-findings.md` (61 lines; findings 1, 6, 7 above). `docs/plans/README.md` holds the plan registry. Plan/prose-only edits get no version bump.

## 10. Design choices the later tasks MUST follow

1. **One shared "requeue on new pool" helper, used by planning and by workers.** Put it in `internal/autonomous/capacity.go` (or a new `capacity_requeue.go` in the same package). Do not write a planner-only and a worker-only copy. Suggested shape: `m.admitRequeuing(ctx, holderKind, resolve func() admissionTarget, request builder, role, onWait)` where `admissionTarget{Backend, Model, Pools, Node, Choices}` is re-derived from CURRENT state (PRD.DecompositionProfile/Model, Automation planning fields, global config for the planner; `resolveTaskBackendModel` for a worker). Loop: resolve -> build `capacity.Request` -> `led.Acquire` with a `cancelled` callback that returns true when EITHER the original cancel condition holds OR the resolved target no longer matches the target this attempt queued on -> on the supersede case clear the wait reason, `led.Release`/drop is automatic via `remove(w)`, loop again with the new target; on a genuine cancel return the existing sentinel (`errDecomposeCancelled` / `errCancelledWhileWaiting`).
2. **Why `Request.Refresh` alone is not enough:** `Refresh` may only replace Pools/Node/NodeChoices/ExcludeNodes (`ledger.go:93-102`, `:911-915`); Model and Role stay, and the post-grant code still runs the old frozen `DecomposeRequest`/spawn. The helper must rebuild the whole request and the work that runs after the grant (the `DecomposeRequest` in `decomposeAdmitted`, the backend/model passed to the spawn in the executor). Keep `placementRefresh` for node-list edits; add the supersede check on top.
3. **Detection latency and prompt wake-up:** `cancelled()` runs only on the ledger poll tick (`ledger.go:1041`). To requeue immediately, have `SetPRDLLM`/`SetAutomationLLM` (and `SetTaskLLM` when it is accepted for a waiting worker) call a small manager hook (e.g. `m.notifyAdmissionChanged(holder)`) that cancels the attempt's per-holder context (replace `context.Background()` at `capacity.go:553` with a cancellable context kept in a manager map keyed by holder, cleaned up on exit) or calls `led.Replan()` (`ledger.go:933`). Keep the poll-tick check as the safety net.
4. **Only requeue when the effective pool changed.** Compare the resolved `(backend, model)` (or the pool key set) with the one queued; ignore no-op writes and writes that do not change the effective target. A different backend with an equal pool set still counts as a changed target because the work that runs after the grant uses the new backend.
5. **Keep the wait budget honest.** A requeue is a new admission for planning (fresh `capacityWait()` bound); for workers it follows the existing rule: `resetCapacityWait` is called per admission (`executor.go:1620`), `capacityWaitSpent` accumulates across failover re-entry (`capacity.go:264-287`). Decide explicitly which applies and document it; do not silently reset a worker's bound.
6. **Wait reason shows the current pool.** After a requeue, `PRD.WaitReason` must be rewritten from the new queue (the helper's `onWait` fires with the new reason). Apply `nameRequestedPool` (`wait_reason.go:476`) to the planner reason too so the text names the requested pool, and clear the reason on every exit (existing `clearPRDWaitReason` discipline, `capacity.go:624`).
7. **Do not touch role ranking or host exclusion.** The planner keeps `RolePlanner` (last) and `decomposePools` (no host); the worker keeps `RoleWorker`. A requeued planner must not jump ahead of verifiers/workers.
8. **Status gates stay as they are.** Do not change `autonomous.task_override_statuses` defaults, `prdLLMOverrideAllowed`, or the `ResetToDraft` refusal while planning (the requeue replaces the need to reset). Never edit `.datawatch/verify/*` scripts.
9. **Cancel and reject stay terminal.** A superseded attempt must not consume decompose retry budget or roll the PRD back to draft (same contract as `errDecomposeCancelled`, comment at `manager.go:2738-2742`); use an internal sentinel such as `errAdmissionSuperseded` that both retry loops (`manager.go:2735` and `:2898`) treat as "loop again", not as failure.
10. **Surfaces and parity.** If a new field/endpoint is added, wire REST + MCP + CLI + comm channel + PWA together, add the PWA rendering for `PRD.WaitReason` (currently absent), update openapi/docs, and file the datawatch-app parity issue at merge time. Prefer no new surface: the requeue is automatic on `set_llm`/`automation set_llm`.
11. **Tests (no real sleeps):** use the fake-clock fixtures in section 7; cover (a) queued planner moves to the new pool on `SetPRDLLM`, (b) on `SetAutomationLLM` planning change, (c) cancel while queued still returns `errDecomposeCancelled`, (d) no-op write does not requeue, (e) wait reason text names the new pool and is cleared, (f) worker analogue with `running` in `task_override_statuses`, (g) lease is released on every exit path.

## 11. Corrections to earlier attempts' notes (anchors that were wrong or missing)

- Decompose model resolution is called at `manager.go:2708` (sync) and `:2878` (async), not `:2711`.
- `Approve` (`manager.go:2954`) is a review gate, not a decompose path.
- Restart-decompose and reset-to-draft-while-planning are covered in section 3.
- Manager-side cancel of queued capacity requests: `errCancelledWhileWaiting` `capacity.go:28`, `errDecomposeCancelled` `capacity.go:48` (section 5).
- How `autonomous.*` settings reach the config API: section 8 (`handleGetConfig` `:5789`, `handlePutConfig` `:6589`, apply switch `:8498+`).
- Comm-channel surfaces do exist for every setter (section 6); the PWA has no PRD-level wait-reason render.
