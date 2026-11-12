# Boot-resume of verification script runs from the durable run record

- **Date:** 2026-11-09
- **Version at planning:** 9.112.10

## Goal

A verification or final gate script that was running when the daemon restarted
must not be started again blindly. The daemon resumes it from its durable run
record:

- record's process alive (same pid and start time) → attach a waiter and take the
  result when the run ends;
- record has an exit code and was not yet delivered → consume it directly, once;
- missing or unreadable record, stale pid, or already-delivered record → run the
  script from the start, exactly once.

A recorded pid is trusted only when its live `/proc` start time matches the
record's `StartedAt`, which guards against pid reuse.

## Scope

- `internal/autonomous/verification_script_record.go`: `Delivered` flag on the
  run record; per-script run directory `verifyRunDirFor` (keyed by a hash of the
  absolute script path, so boot can find a run it did not start).
- `internal/autonomous/verification_script_record_unix.go`: fresh runs clear their
  script's directory; in-process results are marked delivered; `killVerifyGroup`.
- `internal/autonomous/verification_script_record_windows.go`: `killVerifyGroup`
  no-op (no durable record is written on Windows).
- `internal/autonomous/verification_script_resume.go` (new): liveness check with
  `/proc` start time, record reader (exit-file fallback), resume decision, waiter
  with the run's timeout, consume-once, retire helper, boot hold helper. Test seams:
  `verifyRecordReadFn`, `verifyPIDLiveFn`, `verifyKillGroupFn`.
- `internal/autonomous/verification_script_exec.go`: `runVerificationScriptCapped`
  consults the record before it runs the script.
- `internal/autonomous/manager.go`: `resetInProgressTasksForResume` keeps a
  verifying task with no verdict verifying when its script run is alive, instead
  of resetting the worker. The second resume switch (`reconcileStuckTasks`) is
  unchanged.
- Tests: `internal/autonomous/verification_script_resume_test.go`, all named
  `TestVerificationScript*`, with fake record reader and fake liveness.

## Phases

1. **Record and run directory** — Status: Done. `Delivered` flag, deterministic
   per-script run directory, delivered marking on in-process results.
2. **Resume decision and waiter** — Status: Done. `resumeVerificationScriptRun`
   wired into `runVerificationScriptCapped`; waiter enforces the run timeout from
   `StartedAt`.
3. **Boot-resume hold** — Status: Done. `holdVerifyingForScriptRun` in
   `resetInProgressTasksForResume`; an undelivered record of a reset task is
   retired so a later attempt cannot consume it.
4. **Tests** — Status: Done. Alive waited and not re-run; finished consumed with
   no run; consumed once then fresh; stale pid re-run once; missing record runs;
   pid reuse rejected; boot hold with live and gone runs.

## Limits

- A run started under the memory watchdog (`resources.Watch`) records no pid, so
  it is never trusted after a restart and its script runs again. The finished case
  is still consumed.
- Boot-resume holds task-level scripts only. A story-level gate's undelivered
  record is consumed when the gate next runs, not retired at boot.
- Liveness uses `/proc`; on hosts without it, records are never trusted and the
  script runs again (the previous behaviour).

## Status

Shipped in the working branch; releases are cut by the supervisor under the
release-numbering rules in AGENT.md.
