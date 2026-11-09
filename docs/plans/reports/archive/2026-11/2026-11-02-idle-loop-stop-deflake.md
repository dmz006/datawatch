# Deflake report: idle check loop Stop now waits for the loop to exit

Evidence notes under `.datawatch/notes/`: `idle-stop-baseline.txt`, `idle-stop-before-fix.txt`, `idle-stop-after-fix.txt`, `start-stop-audit.txt`.

## Root cause

`Coordinator.Stop` returned before the idle check loop exited. The loop was started as
`go c.idleLoop(stop, tick)` (`internal/autonomous/automation_coordinator.go`), and `Stop`
only closed the `idleStop` channel and then waited for the *lane fill* loop's done channel —
nothing waited on the idle loop itself. Closing the channel only requests the exit; the loop
can still be inside a tick (select picks a ready channel at random, and the ticker's buffer
can hold pending ticks), so a pass could start or still be running after `Stop` had
returned, racing shutdown and writing into a directory while it was being cleaned up.

## Baseline

Recorded in `.datawatch/notes/idle-stop-baseline.txt` (ran on the pre-fix tree,
race-detector on):

```
test=TestCoordinatorStopWaitsForIdleLoop
runs=20
failures=20
exit=1
```

Every run reproduced the assertion failure — the defect was deterministic under the test
harness, i.e. a scheduler-timing race in production that the test pins deterministically.

## Regression test

Test name: `TestCoordinatorStopWaitsForIdleLoop`
(`internal/autonomous/automation_idle_stop_test.go`), added together with the `idleTickHook`
test seam in `internal/autonomous/automation_coordinator.go` / `internal/autonomous/automation_idle.go`.

The harness holds the loop blocked inside its first tick via the hook, calls `Stop` in a
goroutine, and asserts that `Stop` has not returned while the loop is still inside that
tick. Before the fix it failed 20 of 20 runs (`.datawatch/notes/idle-stop-before-fix.txt`,
`before_fix=fail`) on exactly this assertion:

> Stop returned while the idle loop was still blocked inside a tick; Stop must wait for
> the idle loop to exit before returning

(`internal/autonomous/automation_idle_stop_test.go:53`). After the fix the same test is
green under the load proof below.

## Fix

Committed by the fix task on this branch; touched
`internal/autonomous/automation_coordinator.go` and `internal/autonomous/automation_idle.go`:

1. **Done channel.** `Start` now creates `c.idleDone = make(chan struct{})` and launches
   `go c.idleLoop(c.idleStop, c.idleTick, c.idleDone)`. The loop begins with
   `defer close(done)`, so the channel closes only when the loop's goroutine is actually
   exiting. `Stop` reads `idleDone` under the same mutex it uses for `idleStop`, closes
   the stop channel, and — after cancelling the child context and the lane fill loop —
   blocks on `<-idleDone` before returning. `Stop` therefore returns only after the idle
   loop has exited.
2. **Lock rule.** `idleDone` is captured inside the `c.mu.Lock()`/`Unlock()` section of
   `Stop` alongside `cancel` and `laneFillDone`, and is deliberately left set on the
   coordinator: a second or concurrent `Stop` caller waits on the same channel, and once
   it is closed those callers return at once. No reader ever touches `idleDone` outside
   the lock.
3. **No-Stop-from-a-pass rule.** `idleLoop`'s doc comment now states that a pass run here
   must never call `Coordinator.Stop`: because `Stop` waits for this loop to exit, calling
   it from a pass would deadlock.
4. **No tick after Stop.** After the select in the loop body, a second
   `select { case <-stop: return; default: }` check runs *before* any pass, because a
   pending tick could have been queued in the ticker's buffer while the stop was already
   closed. This guarantees no further passes start after `Stop` has closed the stop
   channel, and the hook seam runs first so tests can hold the loop inside a tick.

## Load proof

Recorded in `.datawatch/notes/idle-stop-after-fix.txt` (fresh run on this tree, with 3
spin-occupying busy loop processes started before the test and killed by the exact PIDs
recorded afterwards — none left running):

```
tests=TestAutomationIdleStartStop+TestCoordinatorStopWaitsForIdleLoop
runs=30
failures=0
busy_loops=3
gomaxprocs=1
elapsed_seconds=85
exit=0
```

Under `GOMAXPROCS=1` (single-scheduler, so scheduler-timing races surface) with the
race detector, both the pre-existing `TestAutomationIdleStartStop`
(`internal/autonomous/automation_status_test.go`) and the new regression test ran 30
times each with zero failures. The heavier story-level proof for this story (300 runs,
`failures=0`, 3 busy loops, `gomaxprocs=1`, roughly 14 minutes elapsed) is recorded in
`LOAD-PROOF-REPORT.md` at the repo root.

## Start/Stop audit

Full audit in `.datawatch/notes/start-stop-audit.txt` (and committed as
`docs/plans/reports/archive/2026-11/start-stop-audit.md`); package `internal/autonomous`:

| Pair | File | Stop waits? |
|------|------|-------------|
| `Coordinator.Stop` | `internal/autonomous/automation_coordinator.go` | yes — waits on `idleDone` and `laneFillDone` |
| `Manager.Stop` | `internal/autonomous/manager.go` | no — only calls `m.cancel()` |
| `ProgressSnapshotRunner.Run` | `internal/autonomous/progress_snapshot_job.go` | no — ctx-cancelled, never joined |
| `LifecycleSubscriptionEngine.Close` | `internal/autonomous/lifecycle_session_push.go` | no — closes `e.stop`, no done channel |
| `CommFanoutSink.Close` | `internal/autonomous/comm_fanout.go` | no — no waitgroup for in-flight waiters |
| `Coordinator.laneFillDecompose` | `internal/autonomous/lane_fill_coordinator.go` | no — wg is test-only |
| `Manager.startQuestionResume` | `internal/autonomous/question_tick.go` | no — wg is test-only |
| `Manager.callAssistantModel` | `internal/autonomous/decision_assistant_engine.go` | no — bounded by the request context |
| `Manager.ciWatchPending` | `internal/autonomous/ci_watch.go` | no — fire-and-forget under a follow-up budget |

The audit was deliberately audit-only: all of the `waits=no` pairs above were **left
unchanged on purpose** — each is bounded by context cancellation, a request timeout, or a
follow-up budget rather than unbounded, so they were not widened on this branch. Only
`Coordinator.Stop` was changed, to also wait for the idle loop it starts.

## Versioning

No version number was changed on this branch: no `[Unreleased]`-to-number bump and no
version constant edits. The merge helper sets the next patch version above `main` at
merge time. The `CHANGELOG.md` `[Unreleased]` → `Fixed` entry describing the Stop wait
fix is in place on this branch.
