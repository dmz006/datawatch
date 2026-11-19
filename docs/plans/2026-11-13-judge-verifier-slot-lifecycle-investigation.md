# Judge and verifier slot lifecycle: why a pool slot outlives its node lease

- **Date:** 2026-11-13
- **Version read:** v9.172.5 (tree at `def615f97`)
- **Kind:** read-only investigation. Nothing in the tree was changed except this report. The daemon was not run; every claim below is from reading the code, with the file and function named so it can be re-checked.
- **Scope read:** `internal/capacity/ledger.go`, `internal/capacity/verifier_pool.go`, `internal/autonomous/capacity.go`, `internal/autonomous/judge_capacity.go`, `internal/autonomous/verifier_run_cap_decision.go`, `internal/autonomous/judge_reconcile.go`, `internal/autonomous/verifier_reconcile.go`, `cmd/datawatch/capacity_reap.go`, `cmd/datawatch/capacity_wiring.go`, `cmd/datawatch/judge_capacity.go`, `cmd/datawatch/judge_reconcile_host.go`, `cmd/datawatch/verifier_reconcile_host.go`, `cmd/datawatch/verifier_policy_select.go`, `cmd/datawatch/verification_llm.go`, `cmd/datawatch/policy_retarget.go`, and the verifier and judge closures in `cmd/datawatch/main.go`.

## Short answer

1. A lease in the ledger is one object holding every pool it names (`host`, `llm:<entry>`, `node:<name>` or `node-verify:<name>`). Pool slots cannot be "held" apart from a lease. A slot that is still counted while "the node lease" is gone means either a lease that never carried a `node:` pool, or a `verify:<task>` / `judge:<unit>` lease that outlived the worker lease it replaced.
2. After a worker lease is handed to the verifier (`Ledger.Handoff`) or released in its favour (`AdmitJudge` on a node with a verifier pool), the slot is held under the holder `verify:<taskID>`. **Every task-level release in the Automaton code frees only the holder `<taskID>`** (`Manager.releaseCapacity(t.ID)`), so after the hand-off all of them are no-ops for the slot. Only the verifier closure's own release, `ReleaseVerifierRunCapLease`, `releaseForRelaunch` and the reaper free a `verify:` lease. Nothing at the Manager level ever frees a `judge:` lease.
3. There is one confirmed missed release: `cmd/datawatch/main.go`, verifier closure, the pi-family "no pinned model" setup return happens **after** a probe lease was taken and **before** the `defer release()` is registered. The lease and its registry entry leak, and the reaper cannot see them as dead because the registry says the verifier is active and no session was ever bound. The slot stays held until the daemon restarts, including after the Automaton merges.
4. The reaper (`leaseLiveVerdict`) judges a lease only by sessions and an in-memory "active" flag. It never looks at the Automaton, the task status, the lease age (once a flag is set) or the node. That is why a leaked flag keeps a lease live forever.

## 1. Every path that takes a slot, and every path that frees it

Holder names: `VerifyHolder(taskID)` = `verify:<taskID>`, `JudgeHolder(unitID)` = `judge:<unitID>` (`internal/autonomous/capacity.go`). The unit of a judge is a task, a story or an Automaton. The verifier-side lease is only ever for a task.

### 1.1 Taking or adopting a slot

| # | Where | What it does | Holder | Registry entry |
|---|---|---|---|---|
| A1 | `Manager.AdmitJudge` (`internal/autonomous/judge_capacity.go`), reached from `verifierCapacityAdmit` (`capacity_wiring.go`, blocking) | Frees a stale lease the holder still holds, then either `judgeHandoff` (worker lease re-keyed, no wait) or `led.Release(WorkerHolder)` followed by `led.Acquire` of the verifier's own pools | `verify:<task>` | `verifiers.begin(holder)` after `AdmitJudge` returns |
| A2 | `verifierCapacityTryAdmit` (`capacity_wiring.go`): `AdmitJudge` with a 150 ms wait. Used by the verifier policy probe (`verifierProbeLoad.TryAdmit`, `verifierProbeLoad.take`) and by `selectVerifierLLM` for a pool candidate | Same as A1. Releases the worker lease or hands it over **before** it knows it will get a slot | `verify:<task>` | `verifiers.begin(holder)` |
| A3 | `judgeNodeAdmitter` (`cmd/datawatch/judge_capacity.go`), reached from `runJudgeLeased` / `judgeAwaitLease` | `AdmitJudge` with no worker holder: plain `led.Acquire`, node chosen by the ledger when the entry has two or more nodes | `judge:<unit>` | `judges.begin(holder)` |
| A4 | `judgeCapacityTryAdmit` (`capacity_wiring.go`) | `AdmitJudge` with a 150 ms wait, released at once (routing probe) | `judge:<unit>` | none |
| A5 | `Manager.HandoffVerifyLease` (`internal/autonomous/capacity.go`) calling `judgeHandoff` | Re-keys `<task>` to `verify:<task>` through `Ledger.Handoff`. No production caller outside `AdmitJudge` was found; the function is the public form of the same step | `verify:<task>` | none |
| A6 | `Manager.adoptCapacity` (`internal/autonomous/capacity.go`) calling `Ledger.Adopt` | Boot and skip-spawn adoption of the **worker** lease `<task>`. It passes `VerifyHolder(t.ID)` as a successor so it adds nothing when a `verify:` lease already exists | `<task>` only | none |
| A7 | `judgeAdmitters` `admit` and `try` closures (`cmd/datawatch/judge_capacity.go`) | Plain `led.Acquire`. Only the `started` closure from this function is wired in production (`capacity_wiring.go`, `_, _, judgeStartedFn := judgeAdmitters(...)`); `admit` and `try` are test paths | `judge:<unit>` | `judges.begin` |

There is no `Adopt` for `verify:` or `judge:` holders. After a daemon restart the ledger is empty, a surviving verifier or judge session holds **no** lease, and `BindLease` on the reconcile hosts (`verifierReconcileHost.BindLease`, `judgeReconcileHost.BindLease`) calls `led.Bind`, which does nothing when no lease exists. That under-counts rather than leaks (see 2.6).

### 1.2 Binding a session to the lease

- `verifierSessionStarted` (`capacity_wiring.go`) and `judgeAdmitters.started` (`judge_capacity.go`): when the one-shot runner starts a session named `autonomous:verify:<task>` or `autonomous:guardrail:<unit>`, call `registry.bindSession(holder, id)` and `led.Bind(holder, id)`. Called from `runOneShotJSONSessionWithSpawnHookAttr` (`session_judgment.go`), also for an adopted live session.
- Restart reconcile: `Manager.ReconcileVerifiers` and `Manager.ReconcileJudges` call `host.BindLease`, which does the same two calls.
- A lease that came from `Handoff` keeps the worker's `SessionID` until `Bind` overwrites it, so for a while a `verify:` lease names the finished worker session.

### 1.3 Freeing a slot

| # | Where | Frees | Notes |
|---|---|---|---|
| R1 | The closure returned by A1/A2/A3: `end(); release()` where `release` is `led.Release(j.Holder)` (`placeJudge`) | the holder | By **holder name**, not by lease identity. Idempotent. A late call from an old closure frees a newer lease of the same holder |
| R2 | Verifier closure in `main.go`: `defer release()` | `verify:<task>` | Registered **late**: only after the model guard (see 2.1) |
| R3 | `runJudgeLeased` (`cmd/datawatch/judge_capacity.go`): `defer release()` | `judge:<unit>` | Covers success, error, cancel and panic once the lease is granted. A wait that moves backend holds no lease |
| R4 | `placeJudge` model-load failure branch: `led.Release(j.Holder)` then re-acquire | holder | Also on `ctx.Err()` |
| R5 | `AdmitJudge` entry: `led.Release(j.Holder)` when a lease is already held | holder | The only self-healing step for a leaked `verify:` or `judge:` lease, and it only runs if the same holder is admitted again |
| R6 | `Manager.ReleaseVerifierRunCapLease` / `HandleVerifierRunCap` (`verifier_run_cap_decision.go`), called from `verifierRunCapDecision` (`verifier_run_cap_retry.go`) | `verify:<task>` | Frees the ledger lease only. It does not clear `verifiers.active`. The retry on the next backend then runs with no lease |
| R7 | `Manager.releaseForRelaunch` (`internal/autonomous/capacity.go`) | `<task>` and `verify:<task>` | The only task-level path that frees `verify:`. It is a relaunch path, not a completion, cancel or merge path |
| R8 | `Manager.releaseCapacity(t.ID)`: `executeOne` `defer` (`executor.go`), `CancelTask`, `CancelStory`, `ConsumeVerifierResult`, boot-resume and boot-reconcile resets (`manager.go`), `node_load.go`, `adopt_session.go`, `await_decision.go`, `await_external.go`, `replaceStaleLease` | `<task>` only | **No-op once the slot is `verify:<task>`.** Nothing equivalent exists for `judge:<unit>` |
| R9 | The reaper: `syncPools` (`capacity_wiring.go`) calling `Ledger.Reap(live)` every 10 s with `leaseLiveVerdict` | any lease the verdict calls dead | See section 3 |
| R10 | `CapacityRelease` hook (`httpServer.SetCapacityRelease`) | an interactive holder | Not a judge or verifier path |

## 2. The exact paths on which a release is missed

### 2.1 Confirmed leak: the pi-family "no pinned model" return after a probe lease

`cmd/datawatch/main.go`, verifier closure:

1. `policyVerifierSelection(...)` or `selectVerifierLLM(...)` runs with `verifierTryAdmit` set. Both can return a selection whose `Release` is non-nil: `pickVerifierByPolicy` sets `sel.Release, sel.Node = probe.take(...)`, and `selectVerifierLLM` pool branch sets `Release: r` from `tryAdmit`. That is A2: the `verify:<task>` lease is already held, and `verifiers.active[verify:<task>]` is already true.
2. The guard `if useSession && isPiFamilyKind(vkind) && verifyModel == ""` calls `guardPiFamilyVerifyModel`. When it returns a setup verdict ("judge never ran — no pinned model for the pi-family verify session") the closure does `return *setup, nil`.
3. `release := vsel.Release` and the `defer release()` come **after** that return (`if release != nil { defer release() }`). So the early return skips the release.

Result: `verify:<task>` stays in the ledger holding `host`, `llm:<entry>` and the node pool (`node:<n>` or `node-verify:<n>`), and `verifiers.active[...]` stays true. No verifier session was spawned, so `verifiers.sessions[...]` is empty and `ls.SessionID` is the finished worker session. `executeOne`'s `defer m.releaseCapacity(t.ID)` frees `<task>`, which no longer exists (it was handed over or released in A2). A setup verdict does not by itself complete the task. When the operator later fixes the model and the task verifies through a path that does not re-admit `verify:<task>`, or resolves the review by hand, the Automaton can complete and merge while `verify:<task>` is still held.

A retry of the verification for the same task takes the R5 path and frees it. If the task never verifies again, nothing does.

### 2.2 Reaper treats the leaked lease as live (the second half of 2.1, and the general blind spot)

See section 3. In short: `leaseLiveVerdict` returns live for a `verify:` lease whenever `verifiers.isActive(holder)` is true and no session is bound, and for a `judge:` lease whenever `judges.isActive(holder)` is true and `ls.SessionID` is empty. The flag is cleared only by the `end()` of the closure that set it. A closure that never reaches `end()` leaves the flag, and the lease, forever.

### 2.3 Task-level releases never reach `verify:` or `judge:` holders

Every `releaseCapacity(t.ID)` call in 1.3/R8 names the worker holder. The code comment at the top of `adoptCapacity` already says a handed-over worker lease "holds its slot under VerifyHolder", so these calls are known to be no-ops after the hand-off; the safety net was supposed to be the verifier closure's own release. When that closure's release is missed (2.1) or never runs, no completion, cancel or merge step frees the slot.

`CancelTask` and `CancelStory` are the clearest cases: they release `<task>` only. Cancelling or merging does not cancel the context of a running verifier or judge either: the one-shot runner uses its own `context.WithTimeout(context.Background(), timeout)` (`runOneShotJSONSessionWithSpawnHookAttr`), so a verifier or judge session runs until it writes output, ends, or hits its timeout. The slot is correctly held while that happens. If the session is hung and the timeout is long or unset (`judgeRunContext` with a limit of 0 is unbounded), the slot stays held with the Automaton already terminal.

### 2.4 Judge slots: no Manager-level release at all

`judge:<unit>` is freed by R1, R3, R4, R5 and the reaper only. Nothing on cancel, completion or merge of the unit frees it. In normal flow R3's `defer` covers it, so there is no confirmed leak on the judge side. The exposure is the same as 2.3: a judge body that never returns keeps the lease, the reaper trusts `judges.isActive`, and no Automaton-state check ever disagrees. The startup `Manager.ReconcileJudges` plans only session actions (`PlanJudgeReconcile`: end stale judges, adopt the newest) and runs once; it never looks at ledger leases.

### 2.5 `end()` and `led.Release` are not tied to one lease

`verifierRegistry.begin` returns a closure that deletes `active[holder]` and `sessions[holder]` by holder name, and `Release` frees by holder name. If an old closure runs late after the same holder was admitted again (a retry), it frees the new lease and clears the new registry entry. The new lease is then judged by the generic rule: no bound session and `age < spawnGrace` (10 min) is live, after which it is reaped while the verifier is still running. This under-counts; it is not the stuck-slot symptom, but it is the same lack of lease identity, and any fix that adds a reconcile should use `Lease.Acquired` as the identity the way `Ledger.Reap` already does.

### 2.6 Restart: a surviving session with no lease, and a stale registry session

After a restart `ReconcileVerifiers` and `ReconcileJudges` call `registry.bindSession` without `begin`. The registry then holds a session id for a holder that has no lease and no `active` flag. If that holder is later admitted, `leaseLiveVerdict` consults `reg.sessionOf(holder)` first: if the old session is still running the new lease is live, and if it is not running the new lease is reaped as "verifier session X is not running" even though the verifier just started. Neither case leaks capacity beyond the session's life; both give wrong answers about a lease. `adoptCapacity` adopts only the worker holder, so a live verifier after a restart is not counted at all until it is re-admitted.

## 3. How the reaper decides, and why it keeps the lease

`cmd/datawatch/capacity_reap.go`, `leaseLiveVerdict(ls, reg, holderAlive, sessionAlive, now)`, called from the `live` closure in `syncPools` with `reg = verifiers`, `holderAlive = judges.isActive`, `sessionAlive = sessionRunning(mgr, id)`:

| Holder | Order of evidence | Live when |
|---|---|---|
| `verify:<task>` | 1. `verifiers.sessionOf(holder)` (the registry's verifier session). 2. `verifiers.isActive(holder)`. 3. generic rule | a registered verifier session is running; or no session registered and the holder is flagged active; or (generic) `ls.SessionID` empty and `age < spawnGrace`, or `ls.SessionID` names a running session |
| `judge:<unit>` | 1. `ls.SessionID` (bound by `led.Bind`). 2. `judges.isActive(holder)`. 3. generic rule | the bound session is running; or no session bound and the holder is flagged active; or generic rule |
| anything else | generic rule | `ls.SessionID` empty and `age < spawnGrace`, or the bound session is running |

What it never reads: the task or unit state, the Automaton status, the lease age once a flag is set, whether the node still exists or is drained, and which pools the lease holds. So:

- **A flagged lease with no session is live forever.** `isActive` is a bool with no timestamp. This is what keeps the 2.1 leak.
- **A `verify:` lease after hand-off can be kept live by the worker's session.** With no registry entry and no flag, the generic rule reads `ls.SessionID`, which is the worker session carried over by `Handoff` until the verifier session is bound. A worker session left in a state `sessionRunning` counts as running (anything but `complete`, `failed`, `killed`) keeps the verifier's lease live.
- **Order differs between the two kinds.** For `verify:` the registry session overrides `ls.SessionID`; for `judge:` `ls.SessionID` overrides the registry. After a restart (2.6) that asymmetry gives different answers for the same situation.
- **No node evidence.** A lease on a node that was removed from the compute registry, or a lease with no node pool at all, is judged exactly like any other.
- **A reaped holder's registry entry is not cleared.** The reaper loop in `syncPools` prints a line and increments `CapacityLeasesReaped` but never calls `end()`, so a reaped lease can leave `active[holder]` set; if that holder is admitted again before its own `end()`, the flag is stale.

## 4. Why the pool slot persists while the node lease is absent

The reading of "node lease" in the code is the node pool entry of a lease (`Lease.Pools` containing `node:<name>`, `Lease.Node`). A `verify:` or `judge:` lease can hold the `host` and `llm:` slots without it in these ways:

1. **The worker's lease was released, not handed over, and the verifier's lease then leaked.** `AdmitJudge` skips `judgeHandoff` and calls `led.Release(j.WorkerHolder)` when the worker's node has a verifier pool (`verifierPoolOnWorkerNode`, `Ledger.VerifierSlotsOn(node) > 0`). The node lease `<task>` is gone at that moment, and the verifier takes `node-verify:<node>` under `verify:<task>`. If the verifier closure then leaks (2.1), `node-verify:<node>`, `host` and `llm:` stay held with no `node:<node>` entry and no worker lease. A reader looking for the task's `node:` pool entry will not find it.
2. **The verifier pool is a different pool name.** `Request.withNodePool` and `Ledger.swapVerifierNodePoolLocked` put `node-verify:<node>` in the lease **instead of** `node:<node>`. `Snapshot` lists the lease under `node-verify:<node>`. The `node:<node>` pool shows nothing for it by design.
3. **The backend takes no node pool.** `capacityResolver` (`capacity_wiring.go`) returns only `host` and `llm:<entry>` for a backend that is not a hosted local engine, and `AdmitJudge`/`placeJudge` return node `""` for an entry with fewer than two nodes. Such a lease never had a node pool, so "the node lease is gone" is always true for it while its `host` and `llm:` slots stay held.
4. **The lease names a node outside the pool.** `Lease.NodeOverflow` (adopted onto a full node) and `Lease.NodeUnknown` (node expected, not found) keep `host` and `llm:` held and hold no `node:` pool entry. These only arise from `Adopt`, which production uses only for the worker holder, so they affect `<task>` leases, not `verify:` or `judge:` ones.
5. **Task-level releases target a holder that no longer exists.** Section 2.3: the slot lives under `verify:<task>`, and the code that runs at cancel, completion and merge frees `<task>`.
6. **No path re-checks a granted lease against the current node set.** Only the worker path does that (`replaceStaleLease`, `leaseNodeStale`). A verifier or judge lease granted on a node that later leaves the entry's `compute_nodes`, or the compute registry, keeps its pools; `syncPools` stops calling `SetLimit` for the removed node but leaves the old limit in the ledger.
7. **A merge needs no verifier or judge release.** Merging a task branch or an Automaton (`MergeTaskBranch`, `MergeCompletedTasks`) does not touch the ledger. The only reason a merged Automaton has no leaked slot is that its verifier closure released earlier. When it did not (2.1), nothing at merge time compensates.

## 5. Narrowest fix on each missed path

1. **Confirmed leak (2.1), `cmd/datawatch/main.go`.** Register the release as soon as the selection is made, before any early return:
   ```go
   release := vsel.Release
   defer func() { if release != nil { release() } }()
   ```
   placed right after `vsel` is chosen and before the pi-family guard, and delete the later `if release != nil { defer release() }`. The deferred closure reads `release` at exit, so the later `release = lease.Release` assignment (from `awaitVerifierLease`) is still the one freed. The `release()` closure is `end(); led.Release(holder)`, safe to call once. Add a test that drives the guard with a lease from the probe and asserts `led.Held(VerifyHolder(task))` is false afterwards.
2. **Task-level release (2.3), `internal/autonomous/executor.go` and `manager.go`.** `executeOne` runs spawn, verify and the retry loop synchronously, so when its `defer m.releaseCapacity(t.ID)` fires no verifier of that task can still be legitimately running. Replace that defer with a helper that frees `t.ID` and `VerifyHolder(t.ID)`, the same pair `releaseForRelaunch` frees. Use the same helper in `CancelTask`, `CancelStory` and `ConsumeVerifierResult`. A lease still held there is by definition leaked, and a running verifier on a cancelled task should lose its slot anyway. Do not add `JudgeHolder(t.ID)` blindly: a judge may be mid-run for a unit that is still live, so judge cleanup belongs in the reconcile (item 4).
3. **Reaper blind spot (3), `cmd/datawatch/capacity_reap.go`.** Two small changes:
   - Give `verifierRegistry.active` a start time instead of a bool, and treat a flagged lease with **no bound session** as live only while `now - started` is under a bound derived from the verifier or judge session timeout plus the capacity wait. Past it, fall through to the generic rule and then to "dead".
   - Make `leaseLiveVerdict` take a unit-state hook `unitLive func(holder string) (known, live bool)` and return dead when the unit is known and not live and no session is running. The reaper loop in `syncPools` should also call a `forget(holder)` on the right registry for every holder it reaps so a stale flag is not left behind.
4. **Judge slots (2.4) and the periodic reconcile.** Add a pass beside `syncPools` that decides each `verify:` and `judge:` lease from the data below instead of only from flags. Reuse `Manager.judgeUnitLookup()` (already returns `NeedsJudge` and the PRD) for judges and `Store.TaskSnapshot` for verifiers (the same lookup `ReconcileVerifiers` builds).
5. **Registry versus lease identity (2.5), `internal/capacity/ledger.go`.** Add `ReleaseIf(holder string, acquired time.Time) bool` (or return the `Acquired` from `Acquire`) so `release` frees only the lease it took, and have `begin` key its entry by the same pair. Narrower and optional; it removes the late-closure race.
6. **Restart (2.6), `verifier_reconcile.go` and `judge_reconcile.go`.** When the adopt action binds a session, also `led.Adopt` a lease for the holder with that session (successors named so no double count), and clear the registry `sessions` entry for a holder that has no lease. This is the only fix here that changes counting on boot; it is secondary to the leak.

## 6. Data a periodic reconcile needs

All of it is already in memory in the daemon; none of it needs I/O under the ledger lock (the LOCK RULE on `capacity.Options` applies to `Options` hooks only, and `Ledger.Reap` already runs its callback without the lock).

| Input | Source | Used for |
|---|---|---|
| Every lease: `Holder`, `PRDID`, `Pools`, `Node`, `NodeUnknown`, `NodeOverflow`, `SessionID`, `Acquired` | `Ledger.Leases()` or `Snapshot().Leases` | Pick `verify:` and `judge:` holders; read lease age and the identity pair (`Holder`, `Acquired`) |
| Running verifier sessions by task, running judge sessions by unit | `mgr.ListSessions()` with `verifierTaskFromSessionName`, `judgeUnitFromSessionName`, `sessionRunning` (the same filter `verifierReconcileHost.LiveVerifiers` and `judgeReconcileHost.LiveJudges` use) | A lease with a running session of its own kind is live, whatever `ls.SessionID` says |
| Registry state: `active` with start time, `sessions` | `verifiers` and `judges` in `wireCapacity` | Bound the flag; clear it for every holder reaped |
| Task state for `verify:<task>` | `Store.TaskSnapshot(taskID)`: `Status`, `PRDID`; plus the PRD status | Dead when the task is missing or its status is not `TaskVerifying` (or `TaskRunningTests`, which the boot reconcile also treats as verifying) |
| Unit state for `judge:<unit>` | `Manager.judgeUnitLookup()`: `JudgeUnitInfo.NeedsJudge`, `ok` | Dead when the unit is gone or `NeedsJudge` is false and no judge session runs |
| Compute node set | `httpServer.ComputeRegistry().List()` (already read in `syncPools`) | Flag a lease whose `Node` is not registered; report it, do not reap on it alone |
| Grace | `spawnGrace` (10 min) plus the verifier session timeout | Never reap a lease younger than a spawn in flight, as `leaseLiveVerdict` already does |

Decision rule for the pass, per `verify:` or `judge:` lease older than the grace:

1. A running session of its own kind exists for the holder: live.
2. Else the unit is missing, terminal or not awaiting this judgment: release it with `Ledger.Reap` semantics (compare `Acquired`), clear the registry entry, and write one journal line with the reason (as the existing reap line does).
3. Else the unit is still waiting on this judgment: leave it, and record an age over the bound as a warning, not a release, since the verifier or judge may be legitimately waiting on a model.

The same pass is the right place to log a `node-verify:<node>` lease whose worker lease no longer exists and whose task is not verifying, since that is the signature of the 2.1 leak.

## 7. What was and was not checked

- Checked by reading: all functions named above. The missed release in 2.1 and the reaper behaviour in section 3 are direct readings, not inference.
- Not checked: any live daemon state, any capacity snapshot from a running node, and whether a given deployment has pi-family verifier backends without a pinned model (the condition 2.1 needs). Whether the reaper keeps a specific stuck slot in a running daemon depends on whether it matches 2.1; that needs the ledger lease list and the `verifiers` registry from that node.
- Not changed: no source, test or setting. The fixes above are for a follow-up Automaton.
