# Deflake round five — findings (unawaited raise-notify alert write)

- Date: 2026-11-03
- Package: `internal/server` (plus `cmd/datawatch` wiring)
- Tests covered: `TestQuestionStep2Restarts_AnsweredEventExactlyOnce`, `TestQuestionStep2HappyPath_FullFlow`, `TestQuestionNotifyWaitsForAlertWrite` (new regression test)
- CI signature hunted: `TempDir RemoveAll cleanup: … directory not empty` on the step-2 suite
- Evidence: `.datawatch/deflake5/` (scratch, gitignored — `repro-summary.txt`, `after-summary.txt`, `regression-stub-run.txt`, `other-stores.txt`, `background-writers.txt`)

## Cause

The unawaited goroutine in `internal/server`'s `notifyQuestionRaised`
(`internal/server/question_raise_dispatch.go`). On a successful raise the
server fans out in its own goroutine — WebSocket broadcast, messaging
router, and — for high-priority questions — the alert store's `Add`,
which rewrites the alerts JSON file. **Nothing in the raise path waited
for that goroutine**: the 201 was already gone when its last leg ran. So
the write could land in the TempDir a finished test (or a shutting-down
daemon) had already removed — the "TempDir RemoveAll cleanup: directory
not empty" flake on the step-2 suite, and the same shape of hazard at
process exit in production.

That is production code that started a write nothing waited for, so the
fix has two parts and the store itself is unchanged:

1. **`Server.WaitQuestionNotifies`** (the fan-out goroutine counts
   itself on a `sync.WaitGroup`; the wait blocks until every in-flight
   fan-out — including the alert-store write and its listeners — has
   finished), wired into daemon shutdown at `cmd/datawatch/main.go:9627`.
2. **Test cleanup order**: `TestQuestionStep2HappyPath_FullFlow`
   registers `s.WaitQuestionNotifies` in `t.Cleanup` after the fixture is
   built (`internal/server/question_step2_switch_off_test.go:77`), and
   `TestQuestionStep2Restarts_AnsweredEventExactlyOnce` registers its
   subscription cancels before the fixture's own cleanups so they run
   first (in `t.Cleanup`'s last-in-first-out order)
   (`internal/server/question_step2_restart_test.go:63-73`).
3. **Regression test** `TestQuestionNotifyWaitsForAlertWrite`
   (`internal/server/question_notify_wait_test.go`): an alert-store
   listener that lingers 300 ms makes the leg's completion observable
   from the outside; stub the wait out and the test **fails**, with the
   real wait it **passes** (see Regression check).

The alert store (`internal/alerts/store.go`) is write-through and was
not changed.

## Failure rate

Measured on the **before-fix tree** (fix-commit ancestors of
`internal/server/api.go`, `internal/server/question_raise_dispatch.go`,
`cmd/datawatch/main.go` and both step-2 tests; `-race`, `GOMAXPROCS=1`):

| Run | Result |
|---|---|
| 100 iterations, unloaded | 100/100 PASS |
| 100 iterations, single-core pinned + 8 busy-loop load generators | 100/100 PASS |

So the local probability that the window fires is below ≈1.5% (95%
upper bound on 0/200) on this host — the escape window between "test
finishes" and "the unawaited leg's file write lands" is a few
milliseconds and scheduler-timing dependent (that is why it hit the
Release run and did not reliably reproduce here). The defect itself is
**not** probabilistic to prove: with the wait stubbed to a no-op
(before-fix semantics) the regression test fails 100% of the time —
the unwaited write demonstrably outruns whatever proceeds after the
raise (see Regression check). The release-run flake plus the
deterministic stub failure settle the cause without needing a local
probability estimate.

## After the fix

Same three tests, round-5 fix applied, 200 iterations, race on,
single core with load pinning (command in `after-summary.txt`):

```
ok  	github.com/dmz006/datawatch/internal/server	165.201s
exit code 0
```

**200/200 PASS** — 0 failures, 0 flake signatures. The alert write is
now barred by the wait, so it cannot land in a deleted TempDir.

## Regression check

`TestQuestionNotifyWaitsForAlertWrite` must fail before the fix and
pass after. Verified both directions, `-race -count=1`
(`regression-stub-run.txt`):

```
=== wait stubbed to a no-op (before-fix semantics) ===
--- FAIL: TestQuestionNotifyWaitsForAlertWrite (0.00s)
    question_notify_wait_test.go:57: alert listener fired 0 times after
    WaitQuestionNotifies, want 1 (wait did not bar the in-flight raise fan-out)
    exit code 1

=== real WaitQuestionNotifies restored ===
ok  	github.com/dmz006/datawatch/internal/server	1.330s
exit code 0
```

The 300 ms listener linger in the fake is what makes the missing wait
observable: without it, the assertion can win the coin toss; with the
wait, completion is guaranteed before the assertion runs.

## Other store tests

Stores the same tests construct, inventory in `other-stores.txt`:

- **Alerts store** (`internal/alerts/store.go`) — sync writer, no
  background goroutine, and no `Close` method at all: **never closed,
  unchanged**. Write-through, so it cannot itself leak; the round-5
  write it performed was the unawaited one, fixed by the fan-out join,
  not by any store-side change.
- **Question store** (`internal/autonomous/questions_store.go`) —
  append-only JSONL, inline writes under the store lock, no `Close`
  method: **never closed, unchanged**. Reopening over the same file is
  the explicit restart scenario of the step-2 restart test.
- (For contrast: the **session store**'s deferred flush writer was the
  fixed sibling earlier in this line of work —
  `internal/session/store_leak_guard.go` + Close in `t.Cleanup` — and is
  closed by test and production alike.)

Conclusion: both never-closed stores are write-through and were left
byte-for-byte unchanged by the round-5 fix.

## Autonomous package check

Finding in `background-writers.txt`: the other Release-run flake,
`TestAutomationIdleStartStop … TempDir RemoveAll cleanup: directory not
empty`, is the **same defect class with a different writer** — the
`Coordinator.idleLoop` tick body
(`internal/autonomous/automation_idle.go:15`, launched unjoined at
`internal/autonomous/automation_coordinator.go:157`) commits state
through a Manager rooted in the test's `t.TempDir()`
(`internal/autonomous/automation_coordinator_test.go:57`) while
`Coordinator.Stop` joins only the lane-fill loop's done channel; no
`idleDone` signal exists in `internal/autonomous`. The round-5
`WaitQuestionNotifies` therefore **cannot** cover it; it is a distinct
incident of "unjoined writer races cleanup" (detailed in
`docs/plans/reports/idle-stop-shared-cause-investigation.md`). The other
unjoined launch sites on the question/idle paths
(`question_return_path.go:133`, `question_tick.go:233`, the
`automation_idle.go:24` ctx-cancel bridge) write no test TempDir files
and were not touched.

## Versioning

The fix is a **production change** (a new public join point plus daemon
shutdown wiring), not test-only:

- **Patch bump through the merge helper at merge time** — no version
  edit is made on this branch.
- **CHANGELOG entry at merge** describing the fix (raise-notify fan-out
  now joined; daemon shutdown waits for in-flight alert writes).
- The report file itself carries no version number.
