# Does TestAutomationIdleStartStop share the question-step2 cause?

Date: 2026-11-02 (worktree 4e190001, branch `automaton/4e190001`)
Mode: read-only (no autonomous tests run; only the provided investigate script ran)

## Answer: same defect class, different writer — NOT the question-store leak

The Release-run failure
`TestAutomationIdleStartStop ... TempDir RemoveAll cleanup: unlinkat ... directory
not empty` is the classic **unjoined background writer racing `t.TempDir()`
cleanup** defect. The question-step2 failure was that same class, but its
writer was the session/question store's deferred flush writer (fixed in this
tree by `internal/session/store_leak_guard.go` + Close-in-`t.Cleanup` +
`WaitQuestionNotifies`). The autonomous-package failure has its own writer:

**Writer found: `(*Coordinator).idleLoop`** (`internal/autonomous/automation_idle.go:15`)

### Evidence (all in this tree)

1. **Test** — `internal/autonomous/automation_status_test.go:270`:
   `newCoordHarness(t, 1)`, then `h.c.idleTick = time.Millisecond`, then
   `Start / Start(no-op) / Sleep(5ms) / Stop / Stop / Start / Stop`.
   With a 1 ms tick there are ~5 tick iterations whose pass bodies
   (`checkIdle, checkBudget, triagePass, heldQuestionPass, questionPass,
   repoCIWatchPass, securityFailurePass, supervisorWatchPass`) execute around
   the two Stop/Start shutdowns.

2. **Harness writes into `t.TempDir()`** —
   `internal/autonomous/automation_coordinator_test.go:57`:
   `NewManager(t.TempDir(), ...)`. Every pass body that commits state goes
   through `m.store` / audit writers rooted in that TempDir, exactly the tree
   `t.Cleanup`'s `RemoveAll` later has to drain.

3. **Start joins nothing for the idle loop** —
   `internal/autonomous/automation_coordinator.go:156-157`:
   ```go
   c.idleStop = make(chan struct{})
   go c.idleLoop(c.idleStop, c.idleTick)
   ```
   Contrast the lane-fill loop in the same function, which is properly joined
   via a done channel: `laneFillDone ... defer close(done)` (lines 160-164).
   No `idleDone` channel exists anywhere in `internal/autonomous`
   (`grep -rn idleDone internal/autonomous/` → no hits in this tree).

4. **Stop signals but does not wait** — `automation_coordinator.go` Stop:
   ```go
   if c.idleStop != nil { close(c.idleStop); c.idleStop = nil }
   ...
   if laneFillDone != nil { <-laneFillDone }
   ```
   The only join is `<-laneFillDone`. A tick already inside the pass bodies
   when `close(idleStop)` fires keeps running after `Stop()` returns — and
   after the test returns — writing into the directory while Go's test
   cleanup runs `RemoveAll`. That is precisely the
   "directory not empty" signature.

5. **Class match, writer mismatch** — the question-side guard
   (`internal/session/store_leak_guard.go`) documents the identical failure
   mechanism for the store flush writer: "The writer fires up to one
   flushInterval later into the test's t.TempDir — which by then is
   deleted ... the 'TempDir RemoveAll cleanup ... directory not empty'
   failure gets blamed on whatever test happened to be running."
   Same race, different leaking writer:
   question-step2 = session/question store writer;
   TestAutomationIdleStartStop = Coordinator idle loop.

6. **Sibling-branch fix is NOT in this tree** (from prior checkpoint,
   read-only in worktree 64580001): PRD 64580001 "deflake round four" fixed
   exactly this test with `ec031667c` "Make Stop wait on a done channel that
   the idle loop closes" (+ regression test `027c9c58f`
   `TestCoordinatorStopWaitsForIdleLoop`), with before=fail / after=0
   evidence. `git branch -a --contains ec031667c` showed only
   `automaton/64580001*`; it is not on `automaton/4e190001` or main, and
   `grep idleDone` above confirms absence here. So the failure in the
   Release attempt of THIS branch is expected, un-fixed behavior.

7. Investigate-script artifact (required): `.datawatch/deflake5/background-writers.txt`
   (regenerated this run; lists the TestAutomationIdleStartStop site and all
   `go func` launch sites in the idle/question paths: `automation_idle.go:24`
   (the ctx-cancel bridge, itself unjoined), `question_return_path.go:133`,
   `question_tick.go:233`, `internal/server/question_raise_dispatch.go:74`).

## Conclusion

Same defect class as question-step2 — an unjoined background writer racing
`t.TempDir()` cleanup — but a **different writer**, so it is not "the
question-store race" and the question-side fix (leak guard / Close in
t.Cleanup / WaitQuestionNotifies) cannot cover it. The autonomous-side fix
exists on branch `automaton/64580001` and is not present here.

## Run that settles it

1. Repro (expect "directory not empty" flakes):
   ```
   env -u TMUX -u TMUX_TMPDIR GOMAXPROCS=1 \
     go test ./internal/autonomous/ -run TestAutomationIdleStartStop -race -count=300
   ```
   Expect some `TestAutomationIdleStartStop ... directory not empty` failures.
2. Fix + settle: cherry-pick `ec031667c` (plus the regression test `027c9c58f`, "no tick
   after Stop returns") onto this branch, rerun the same command → expect failures=0.
   That before/after delta isolates the idle loop as the writer and proves the
   defect-class link to question-step2 (same fix shape: join-before-return) while the two
   failures remain distinct incidents.

## Re-verification (retry run 2026-11-02 13:48 UTC)

Every citation above was re-checked against the live tree on 2026-11-02 and all still hold:

- Test body at `automation_status_test.go:270-278` (1 ms tick, `Start/Start/Sleep(5ms)/
  Stop/Stop/Start/Stop`) — confirmed verbatim.
- `Start`: `automation_coordinator.go:156-157` `go c.idleLoop(c.idleStop, c.idleTick)`;
  `laneFillDone` join pattern at lines 160-164 — confirmed verbatim.
- `Stop`: `automation_coordinator.go:174-204`; sole join is `<-laneFillDone` (line 198);
  no `idleDone` signal anywhere in `internal/autonomous` (grep re-run, zero hits).
- `idleLoop` pass list confirmed at `automation_idle.go:36-43` (`checkIdle, checkBudget,
  triagePass, heldQuestionPass, questionPass, repoCIWatchPass, securityFailurePass,
  supervisorWatchPass`) — all state commits flow through the Manager rooted at
  `NewManager(t.TempDir(), ...)` (`automation_coordinator_test.go:57`).
- `git branch -a --contains ec031667c` re-run: only `automaton/64580001*` branches
  (plus its task sub-branches) — the fix is still absent from
  `automaton/4e190001`/`main`.
- `.datawatch/verify/investigate-background-writers.sh` re-run this session (exit 0);
  `.datawatch/deflake5/background-writers.txt` output identical (10 lines) and matches
  the artifact listing in section 7 above.
- Clarification: `automation_idle.go:24` (the ctx-cancel bridge goroutine) is itself
  unjoined but writes no files; the file-writing surface is the `idleLoop` main goroutine's
  tick body, which is the one `Stop` never joins.

Companion evidence copy (scratch, gitignored): `.datawatch/deflake5/idle-stop-shared-cause.md`.

## Resolution (added 2026-11-02, after the fix landed on this branch)

The conclusions above are recorded as they stood at investigation time
(read-only; the fix was then only on `automaton/64580001`). They are
superseded on the current tree of THIS branch:

- **The autonomous-side fix is now present here.** `Start` creates an
  `idleDone` channel and the idle loop closes it; `Stop` blocks on
  `<-idleDone` after closing the stop channel
  (`internal/autonomous/automation_coordinator.go:161-162, 203-204`).
  The writer found in Evidence 3-4 (`*Coordinator.idleLoop`) is now
  joined before `Stop` returns, so a pass in flight can no longer write
  the `t.TempDir()` root after the test has cleaned it up.
- **The regression test is present here too**:
  `internal/autonomous/automation_idle_stop_test.go`
  (`TestCoordinatorStopWaitsForIdleLoop`) drives one tick through the
  `idleTickHook` test seam (nil on the production path) and asserts
  `Stop` does not return before that tick completes — the "no tick
  after Stop returns" guarantee from sibling commit `027c9c58f`.
- **"directory not empty" is gone in the under-load run on this
  branch**: `TestAutomationIdleStartStop` +
  `TestCoordinatorStopWaitsForIdleLoop`, `-race -count=300`,
  `GOMAXPROCS=1` under load — 300/300 PASS, 0 "directory not empty",
  wall 634 s (the join makes each iteration wait for the in-flight
  pass; the pass includes the `datawatch-*` temp sweep over the host
  temp root, whose tens of thousands of leftover entries from earlier
  test campaigns are the dominant runtime — pre-existing environmental
  load, not a regression; the pre-fix 300-iteration run of
  `TestAutomationIdleStartStop` alone completed in 101 s). See the
  evidence table in `docs/plans/reports/deflake-round-five-findings.md`
  (Scratch: `.datawatch/deflake5/after-idle-300.txt`).

The "Run that settles it" section above therefore describes the
historical before/after that was expected; the same before/after was
actually run this round (pre-fix and this branch, `-count=300`, both
green here; the 12% flake reproduction occurred on the sibling host
during its round). The defect-class link to question-step2 holds:
same fix shape (join the writer before `Stop`/return), distinct
writers, distinct incidents.
