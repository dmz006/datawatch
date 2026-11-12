# BL451 — Stale completion-rollup overwrites concurrent reset

- **Date:** 2026-10-15
- **Version:** v9.1.1 (fix) → v9.1.2 (recovery path)
- **Status:** Phase 1 (PRD.Rev + guarded commit) — Done (v9.1.1). Phase 2 (ResetTask pending-task recovery) — Done (v9.1.2). Phase 3 (unstick BL449 live) — Planned (operator step).

## Incident timeline (2026-10-15, PRD 9e7bad8e / BL449)

- ~00:38 — last task reaches a terminal status; the executor starts the PRD-completion rollup, including the ci-watch check.
- 00:41:27 — operator calls `reset_task` with `force=true`: the task is flipped to pending and the PRD to `running`.
- 00:41:37 — the in-flight ci-watch rollup finishes and calls `SavePRD` with its stale in-memory PRD, writing `completed` over `running`.
- Result: PRD `completed` with one task `pending`. `reset_task` on that task errors (not failed/blocked/completed/cancelled), and approve/run refuse a completed PRD. Dead end.

## Root cause

A shared-pointer lost update in the `executor.go` rollup. The rollup held a PRD it had read before the reset. It decided "all tasks terminal → completed" from that snapshot and saved it unconditionally after the slow ci-watch step, so the concurrent reset was lost.

## Fix

- **Phase 1 (v9.1.1):** `PRD.Rev` is bumped on every `SavePRD`. All four terminal rollup sites (failed, the blocked paths, completed) commit through `Store.CommitPRDIfRev`. Under the store lock it checks that the revision is unchanged apart from the rollup's own saves, the status is still the one it saw, and every task is still terminal. A stale commit records `rollup_aborted_stale` and writes nothing else.
- **Phase 2 (v9.1.2):** `Manager.ResetTask` with `force=true` on an already-pending task in a terminal PRD (completed/failed/blocked/cancelled) is now a recovery. The task stays pending, a terminal containing story is reopened, the PRD returns to `running`, and a `recover_pending_task` decision is recorded with the actor and prior status. Without `force` it still errors, and the message now points at `force=true`. No other combination changes.

## Parity surface

- **REST / MCP / CLI / comm channel:** these surfaces all wrap `Manager.ResetTask`, so they get the new behaviour without code changes. Only the MCP `autonomous_prd_reset_task` description text was updated.
- **YAML/config:** excluded, because there is no new setting.
- **PWA / Android / iPhone:** excluded, because the existing reset action calls the same endpoint and no UI change is needed.

## Unsticking BL449 (9e7bad8e)

1. Deploy v9.1.2 or later, then restart the daemon with `systemctl --user restart datawatch`.
2. Call `autonomous_prd_reset_task` on 9e7bad8e's pending task with `force=true`.
3. Confirm with `autonomous_prd_get` that the status is `running` and the last decision is `recover_pending_task`.
4. Re-running the matrix is a separate step and is not part of this recovery.

This task did not perform the live operation.
