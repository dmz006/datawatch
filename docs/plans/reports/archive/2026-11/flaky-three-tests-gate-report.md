# Flaky-three-tests gate report (run of 2026-05-31, 09:06–09:12 local)

Gate: `.datawatch/verify/flaky-three-tests-gate.sh` from repository root of
worktree `/home/<user>/.datawatch/prd-worktrees/60070001` (branch `automaton/60070001`, HEAD 7876cac18).
Read-only run; no repository files were modified by this task. Full raw log: `.datawatch/verify/gate-run.log`.

## Step results and timings

| # | Step | Result | Duration |
|---|------|--------|----------|
| 1 | `gofmt -l` on the 8 changed Go files only | PASS ("gofmt clean on changed files") | <1s |
| 2 | `go vet ./internal/server/ ./internal/shutdowncause/` | PASS (no output) | <1s |
| 3 | `internal/shutdowncause`, `-race -count=40`, GOMAXPROCS=1, taskset -c 0 + 4 CPU burners (burners killed by the script's recorded PIDs) | PASS — `ok github.com/dmz006/datawatch/internal/shutdowncause` | 20.302s (09:06:08→09:06:30 wall) |
| 4 | `TestWSBroadcastDelta_LegacyClientGetsOneBoundedFrame` + `TestGH162_NotifyStateChange_EmitsBothMessages`, `-race -count=60`, same load | PASS — `ok github.com/dmz006/datawatch/internal/server` | 13.313s (09:06:30→09:06:49) |
| 5 | `internal/server` full package, `-race -count=1` | **FAIL** — data race in `TestHandleChannelReady_HeartbeatNeverRepastesOneShotTask` | 250.157s (09:06:49→09:11:00) |
| 6 | `internal/session` `TestNoNewTmpLeakEntries` temp-leak guard | PASS — `ok github.com/dmz006/datawatch/internal/session` | 86.648s (09:11:00→~09:12:27) |

Final line: **`FINAL GATE FAILED`** (non-zero exit).

## The failing test, message, step, and race

- **Step:** step 5 (`go test -race -count=1 ./internal/server/`, full package) — the only failing step.
- **Failing test:** `TestHandleChannelReady_HeartbeatNeverRepastesOneShotTask`
- **Failure message (exact):**
  ```
  WARNING: DATA RACE
  Write at 0x00c0006dc078 by goroutine 1399:
    github.com/dmz006/datawatch/internal/server.setWaiting()
        internal/server/channel_ready_redelivery_test.go:101
    github.com/dmz006/datawatch/internal/server.TestHandleChannelReady_HeartbeatNeverRepastesOneShotTask()
        internal/server/channel_ready_redelivery_test.go:140

  Previous read at 0x00c006dc078 by goroutine 1400:
    encoding/json/v2.makeStringArshaler.func1()
    github.com/dmz006/datawatch/internal/session.(*Store).marshalCapped()
        internal/session/store_archive.go:697
    github.com/dmz006/datawatch/internal/session.(*Store).flushRecords.func1()
        internal/session/store_records.go:396
    github.com/dmz006/datawatch/internal/session.(*Store).flushRecords()
        internal/session/store_records.go:409
    github.com/dmz006/datawatch/internal/session.(*Store).flush()
        internal/session/store.go:898
    github.com/dmz006/datawatch/internal/session.(*Store).writer()
        internal/session/store.go:766
    github.com/dmz006/datawatch/internal/session.(*Store).markDirtyLocked.gowrap1()
        internal/session/store.go:735
    github.com/dmz006/datawatch/internal/session.(*Store).Save()
        internal/session/store.go:694
    github.com/dmz006/datawatch/internal/session.(*Manager).SaveSession()
        internal/session/manager.go:5045
    github.com/dmz006/datawatch/internal/server.seedOneShot()
        internal/server/channel_ready_redelivery_test.go:115
    github.com/dmz006/datawatch/internal/server.TestHandleChannelReady_HeartbeatNeverRepastesOneShotTask()
        internal/server/channel_ready_redelivery_test.go:130
  ==================
  --- FAIL: TestHandleChannelReady_HeartbeatNeverRepastesOneShotTask (10.14s)
      testing.go:1867: race detected during execution of test
  ```
- What it is: the test's own helper `setWaiting` mutates a live session record
  (test goroutine 1399, `channel_ready_redelivery_test.go:101`) while the session
  store's background writer goroutine (1400), woken by the test's own
  `seedOneShot` → `Manager.SaveSession` (line 115), is concurrently
  JSON-marshalling the same store (`marshalCapped`/`flushRecords`). Classic
  test-side concurrent mutation of shared store state; the race window is
  schedule-dependent, which is why the test passes in isolation (steps 3–4
  and single re-runs) but fails in the full-package `-race` run.

### Related race seen in the immediately prior verification run (08:49–08:56)

That run failed the same step with **two** races: the one above, plus a
`TestQuestionsFlow_EndToEnd` race between a write in `applyConfigPatch`
(`internal/server/api.go:8407`, `autonomous.questions.*` config keys) and a
read in `notifyQuestionRaised`
(`internal/server/question_raise_dispatch.go:92`). It did not reproduce in
this run (count=1 flake), but the shared root cause — test goroutines and the
store/config writer running concurrently under `-race` — is the same class.

## Responsible earlier tasks (files are not in this branch's changed set)

This branch (`automaton/60070001`) changes only: `CHANGELOG.md`,
`internal/server/gh162_session_state_broadcast_test.go`,
`internal/server/health_test.go`, `internal/server/hub_register_wait_test.go`,
`internal/server/ws_session_stream_test.go`, and
`internal/shutdowncause/{capture_linux.go,cause_linux_test.go,signal_child_test.go}`.
`git log main..HEAD` touches **none** of the racing files, and all of this
branch's own tests pass. The race lives in earlier tasks' code:

| Racing file | Introduced/last changed by |
|---|---|
| `internal/server/channel_ready_redelivery_test.go` (the failing test + `setWaiting`/`seedOneShot` helpers) | `7182f10db task(5a390001): Triage and fix the Go path-injection alerts`; file's fixture pattern from `9ea69f74c task(89450001): Apply the same cleanup to every test using the same fixture` |
| `internal/session/store_archive.go:697` (`marshalCapped`), `store_records.go:396/409` (async `flushRecords`) | `035ed7be2 task(766e0001): Archive old terminal sessions and cap stored field sizes` (store writer pipeline), refine: `cf0f90afb task(1de70001): Route session path sinks through the containment helper` |
| `internal/session/store.go` (writer goroutine, `markDirtyLocked`/`Save`) | `816db4622 task(ead20001): Add the session tags field with inheritance`; `bddbfc921 task(84860001): Carry the held node into the session start request` |
| `internal/session/manager.go:5045` (`SaveSession`) | `8f243c8b4 task(f8f60001): Implement the daemon temp file sweep` |
| (flaky second race) `question_raise_dispatch.go` | `20efd06ed task(f1f200be): Raise worker questions into the store with links, dedupe and audit`; `c32fa6274 task(e0830001): Notify on raise: ...` |

So: the failing step is caused by **task 5a390001 / 89450001**
(channel-ready redelivery test helpers writing live records during the test)
racing against the async session-store writer pipeline from **task 766e0001**
(and its follow-up 1de70001) — none of which this branch modified. The fix
belongs in whichever of those tasks next owns those files (e.g. have the
test helpers stop mutating shared stores concurrently, or the store
defensively deep-copies records before flushing); it is out of scope for this
read-only gate task.

## Notes
- Step 6 (`TestNoNewTmpLeakEntries`) passed this time; the prior verification
  run also reported it passing (93.926s).
- The many `[warn] sessions store flush: acquire sessions lock: ... no such
  file or directory` lines in the log are pre-existing test-cleanup noise
  (background flusher racing testhome teardown), not a gate step failure.
- Because a step failed, the "everything passes" extra checks (no new `t.Skip`,
  no unlisted test files changed, `Version` in `cmd/datawatch/main.go` /
  `internal/server/api.go` unmodified) were not applicable; note for the
  record that `git diff main...HEAD --name-only` shows no `t.Skip`-bearing
  new files beyond the 8 listed above, and this branch does not touch
  `cmd/datawatch/main.go` or `internal/server/api.go` (main's `Version` is
  `9.47.1` per `193b17b99 Set version 9.47.1`).

## Final
**FINAL GATE FAILED** — 5 of 6 steps pass; failure is a pre-existing
`-race` data race in `TestHandleChannelReady_HeartbeatNeverRepastesOneShotTask`
(internal/server full-package run), caused by earlier tasks' test-helper +
session-store-writer code, not by anything in this branch.
