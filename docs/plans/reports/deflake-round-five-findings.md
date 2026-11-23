# Deflake round five — findings (unawaited raise-notify alert write)

- Date: 2026-11-02
- Package: `internal/server` (plus `cmd/datawatch` wiring) and `internal/autonomous` (same defect class, covered below)
- Tests covered: `TestQuestionStep2Restarts_AnsweredEventExactlyOnce`, `TestQuestionStep2HappyPath_FullFlow`, `TestQuestionNotifyWaitsForAlertWrite` (new regression test), and — autonomous package — `TestAutomationIdleStartStop` + `TestCoordinatorStopWaitsForIdleLoop` (new regression test)
- CI signature hunted: `TempDir RemoveAll cleanup: … directory not empty` on the step-2 suite (and, on the same release run, the autonomous-package sibling `TestAutomationIdleStartStop`)
- Evidence: `.datawatch/deflake5/` (scratch, gitignored — `repro-summary.txt`, `after-summary.txt`, `regression-stub-run.txt`, `other-stores.txt`, `background-writers.txt`, plus the 2026-11-02 300-run outputs `before-question-300.txt`, `after-question-300.txt`, `before-idle-300.txt`, `after-idle-300.txt`)

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
`idleDone` signal exists in `internal/autonomous` (state at investigation time; it is resolved on this branch — see the next bullet). The round-5
`WaitQuestionNotifies` therefore **cannot** cover it; it is a distinct
incident of "unjoined writer races cleanup" (detailed in
`docs/plans/reports/idle-stop-shared-cause-investigation.md`). The other
unjoined launch sites on the question/idle paths
(`question_return_path.go:133`, `question_tick.go:233`, the
`automation_idle.go:24` ctx-cancel bridge) write no test TempDir files
and were not touched.

- **Resolved on this branch.** `Coordinator.Start` now creates an
  `idleDone` channel and passes it to the idle loop, and `Stop` blocks
  on it after closing the stop channel
  (`internal/autonomous/automation_coordinator.go`), so a stop returns
  only once the loop goroutine — including its in-flight `triagePass`
  (triage → retro → cleanup sweep) — has fully finished; the pre-fix
  `Stop` returned immediately while a pass could still be writing the
  Manager's root, which in the test is rooted in `t.TempDir()`. A
  test-only seam (`idleTickHook`, nil on the production path) lets a
  test occupy the pass window, and the regression test
  `TestCoordinatorStopWaitsForIdleLoop`
  (`internal/autonomous/automation_idle_stop_test.go`) starts the
  coordinator on a one-millisecond tick, blocks the first pass on the
  hook, and asserts `Stop` returns only after the pass completed. This
  is the direct analogue of the question-side
  `TestQuestionNotifyWaitsForAlertWrite`.

## 300-iteration under-load evidence (both halves, this branch, 2026-11-02)

`GOMAXPROCS=1`, `-race`, `-count=300`, `TMUX`/`TMUX_TMPDIR` unset, 8
busy-loop load generators on a 48-core host. Pre-fix runs in a
throwaway worktree checked out at the pre-fix commit; post-fix runs on
this branch. The question triad = the three `internal/server` tests
above; the autonomous run adds `TestCoordinatorStopWaitsForIdleLoop`
(post-fix only — pre-fix has no seam to drive it, which is the point
in itself).

| Run | Iterations | Failures | Flake signatures | Wall time |
|---|---|---|---|---|
| pre-fix, question triad | 300 | 0 | 0 | 291 s |
| post-fix, question triad | 300 | 0 | 0 | 289 s |
| pre-fix, `TestAutomationIdleStartStop` | 300 | 0 | 0 | 101 s |
| post-fix, both autonomous tests | 300 | 0 | 0 | 634 s |

Notes for the record:

- **The flake did not reproduce on this host at any iteration count
  (100, 200, 300), before or after, loaded or not** — the same
  zero-flake result the earlier iterations recorded. The escape window
  (a few milliseconds, scheduler-timing dependent; it hit the Release
  run, not this host) is therefore pinned where it is deterministic:
  with the wait stubbed to no-op semantics the question regression test
  fails 100% of the time (see Regression check), and the autonomous
  regression test proves the join — pre-fix `Stop` returned immediately
  while the pass was still in flight (the crash), post-fix it cannot.
  The 12% flake rate for `TestAutomationIdleStartStop` (and the
  before-fail / after-0 split) was seen on the sibling host's runs,
  recorded in
  `docs/plans/reports/idle-stop-shared-cause-investigation.md`.
- **The post-fix autonomous run is slower than the pre-fix one by
  design, not by regression:** because `Stop` now joins the idle loop,
  each of the 300 iterations waits for the in-flight pass to finish —
  and that pass includes the `datawatch-*` temp sweep over the host's
  temp root, which at run time carried tens of thousands of leftover
  entries from earlier test campaigns. Under a single core under load
  each such pass is slow, so the post-fix run completed in 634 s where
  the pre-fix (unjoined, never waiting for anything) run took 101 s.
  Joining is the fix; the sweep's cost was pre-existing, and the
  post-fix run simply pays for it at test time instead of hiding it.
- **No root-level result file:** the plan to carry the result in a
  tracked file at the repository root would have tripped
  `tests/ci/repo_root_files_test.go`. The evidence lives in this
  report, and the raw per-run output in the gitignored scratch dir
  `.datawatch/deflake5/` (named above).

## Versioning

The fix is a **production change** (a new public join point plus daemon
shutdown wiring), not test-only:

- **Patch bump through the merge helper at merge time** — no version
  edit is made on this branch.
- **CHANGELOG entry is on this branch, under `[Unreleased]`** (the
  earlier "entry added at merge" phrasing is superseded): it records
  both the raise-notify fan-out join (daemon shutdown awaits in-flight
  alert writes, and can no longer panic with a WaitGroup misuse) and
  the autonomous coordinator `Stop` join, both regression tests, and
  this report.
- The report file itself carries no version number.
