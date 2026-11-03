# Flaky final-gate verification report

Date: 2026-10-23 (re-verified 01:36–01:41 UTC local)
Branch worktree: automaton/dbd50023
Task: End-to-end verification that the launch-failure + sibling-poller flakes are fixed. No source changes required.

## Result

| Run | Command | Result | Wall time |
|-----|---------|--------|-----------|
| 1 | `bash .datawatch/verify/flaky-final-gate.sh` (STRESS_COUNT=30) | VERIFY OK, exit 0 | 237s |
| 2 | `STRESS_COUNT=60 bash .datawatch/verify/launch-failure-flaky-fix.sh` | VERIFY OK, exit 0 | 24s |

Both runs completed pass 1 (serial, `-race`) and pass 2 (contention: rest of package in parallel + 24 CPU burners, GOMAXPROCS=2) with zero flakes, and zero background-runner failures.

## Checks inside flaky-final-gate.sh (both runs)

- `go vet ./internal/autonomous` — clean
- `go build ./...` — clean
- `go test -race -count=1 ./internal/autonomous` — ok (~14.9s)
- `golangci-lint run ./internal/autonomous/...` — clean
- Selected set: every test in files matching `pollUntil|pollPRDTerminal|runToTerminal|waitForPRDStatus`, run 30× (final-gate) under both passes
- launch-failure script guards:
  - `launch_failure_test.go` uses `t.TempDir()`, no hard-coded `"/w/proj"` — PASS
  - `runToTerminal` has no fixed 5s status-poll fallback — PASS
  - `TestLaunchFailure*` run 60× under both passes (incl. 20-iteration `..._NeverBlocked` loop → 1200 fixture runs per pass)

## Root cause

None found in this run — the fix already landed in a prior commit on this branch
(`47ddfeec` "Audit and fix sibling pollUntil tests that depend on real timers").
This task's scope was end-to-end verification; both gate scripts exit 0 with no
source changes, which is the statistical proof (30× and 60× reps, serial and
under CPU contention, race detector on throughout) that no leftover real-timer
dependency, shared-helper race, or unlocked field write remains among the
audited siblings.

## Audited sibling list (all exercised by flaky-final-gate.sh SEL, both passes)

- `automation_budget_admit_test.go` — TestAdmit* (wait, cancel, ignore, pacing, rollover, resumes, reason)
- `blocked_task_edit_test.go` — TestBlockedPRD_* (unblock/rerun, verification summary, retry hint, waitForPRDStatus)
- `ci_test.go` — TestCIWatch_* (disabled, green, failing policy/content, timeout)
- `drain_helper_test.go` — hosts `pollUntil`, `pollPRDTerminal`, `drainOnCleanup`
- `decompose_admission_test.go` — TestDecomposeAdmit*
- `executor_lease_test.go`, `executor_retry_test.go` — lease hold on verify, retry re-admit, pool fallback, worker-stall retry exhaustion, scope drift
- `launch_failure_test.go` — TestLaunchFailureError_WrapsWorkerStalled, *Retry_RetriesThenRecovers, *Retry_BoundedAndFailsInfra (… and _NeverBlocked iter 0–19)
- `scope_test.go` — TestScope_ExecutorBlocksOutOfScopeTaskWithoutSpawning; hosts `runToTerminal`
- `exit_silence_retry_test.go` — TestExitSilenceRetry_*
- `adopt_live_test.go` — TestAdoptLive_*, TestAdoptClaudeCode_*
- `await_external_test.go` — TestAwaitExternal_TreeLock*
- `resume_parked_test.go` — TestResumeParked_*
- `boot_resume_test.go` — TestBootResume* / autonomous-boot-resume*
- `story_halt_test.go` — TestHalt*; hosts `settlePRD`, `runToTerminalOrBlocked`

## Per-file changes in this task

None. Working tree was clean before and after; verification-only task and both
scripts exited zero. No other product defects found.
