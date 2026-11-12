# CI Watch Flake: TestCIWatch_TimeoutPending

Date: 2026-10-28
Scope: Why TestCIWatch_TimeoutPending (internal/autonomous/ci_watch_test.go) flakes
on CI with "pollUntil: follow-up recording a pass not reached within 30s".
Status: Done.

## Investigation

### Root cause (hypothesis confirmed)

`Store.loadRecords` (internal/autonomous/store_records.go) scans the records
directory (`<dataDir>/autonomous/prds/`) and deletes every file named
`.tmp-*` ("leftover of an interrupted write"). `atomicWriteFile` writes the
record in exactly that directory: `os.CreateTemp(dir, ".tmp-*")` → write →
sync → close → chmod → `os.Rename`. Any `NewStore(dir)` opened between
CreateTemp and Rename executes loadRecords and `os.Remove`s the in-flight
writer's temp file. The writer then fails at Chmod or Rename with
"no such file or directory", `SavePRD` returns that error, and
`ciWatchFollowUp` (internal/autonomous/ci_watch.go, line ~642) only does
`log.Printf` on the SavePRD error — so the recorded "pass" is lost for good,
and pollUntil times out.

The race is structural, not scheduling luck: the victim window spans the
whole write/fsync/chmod/rename sequence, and the test hammers that window
because TestCIWatch_TimeoutPending's `readBack` closure opens a fresh
`NewStore(dataDir)` on every poll while the follow-up goroutine calls
`SavePRD` concurrently.

### Probe (step 1)

`internal/autonomous/zz_probe_test.go` (created, then deleted): one Store
on `t.TempDir()`, `CreatePRD`, then for 3 s one goroutine looping
`NewStore(dir)` while the test goroutine mutates `p.Spec` and calls
`SavePRD(p)`, counting errors and keeping the first:

```
saves=150742 fails=31211
firstErr=chmod /tmp/TestZZProbe2260169623/001/autonomous/prds/.tmp-495180653: no such file or directory
```

~21% of SavePRD calls fail with a "no such file or directory" error naming
the `.tmp-*` temp file (first captured one hit the Chmod step; the same
removal can equally hit Rename — any syscall between creation and rename).

### Stress reproduction of the original test (step 3)

Built `go test -race -c ./internal/autonomous/`, ran
`-test.run TestCIWatch_TimeoutPending -test.count=30 -test.shuffle=on`
from internal/autonomous with 48 bounded `yes > /dev/null` burners
(one per core, 120 s timeout, stopped by recorded PID):

- `-test.parallel 1`: 0/30 failed (PASS, 35 s)
- `-test.parallel 16`: 0/30 failed (PASS, 35 s)

The test did not fail locally under this stress; the probe result above is
the evidence. Local timing (48 cores, fast tmpfs) simply did not land a
NewStore mid-write in the follow-up's window during these 60 runs.

### Ruling out the other candidates (step 4)

- `grep -c t.Parallel internal/autonomous/ci_watch_test.go` → 0, and no
  test file in the package uses `t.Parallel`, so there is no test-level
  concurrency other than the follow-up goroutine the test itself spawns.
- `wireCIWatch` (ci_watch_test.go:68-69) sets `m.ciPollInterval` and
  `m.ciFollowUpDelay` to 1 ms on the test's Manager, so polling cadence and
  follow-up delay are not the variable.
- The fake `runs` func blocks on
  `select { case <-release: case <-ctx.Done(): }`. A receive from a closed
  channel returns immediately and `close(release)` runs long before the
  follow-up could otherwise unblock, so the release cannot be missed.

Conclusion: the only unexplained, and now demonstrated, failure path is the
NewStore/loadRecords `.tmp-*` cleanup racing a concurrent SavePRD's
atomicWriteFile, with ciWatchFollowUp silently dropping the pass on the
resulting save error.

## Suggested fix direction (for the next tasks)

- Do not delete foreign temp files in loadRecords: only remove `.tmp-*`
  entries that are stale (e.g. age- or pid-based naming), or better, never
  sweep temp files at all and rely on per-writer cleanup
  (atomicWriteFile already removes its own temp file on error and
  Rename consumes it on success).
- Make `ciWatchFollowUp` retry/fail visibly on SavePRD errors instead of
  logging, so a lost pass is at least observable.

## Final verification

Ran 2026-10-28 on the fix branch (worktree e9930001), all outside tmux
(`env -u TMUX -u TMUX_TMPDIR`). No stray files left: `git status --short`
is clean after the runs.

### Stress gate (.datawatch/verify/ci-watch-stress-gate.sh)

One bounded CPU burner per core (`timeout 150 sh -c 'yes > /dev/null'`),
then TestCIWatch with -race -shuffle=on -count=15 at -parallel 1 and 16,
then TestPRDStore, all under load:

```
== TestCIWatch -race -shuffle=on -parallel 1 under CPU load ==
PASS
== TestCIWatch -race -shuffle=on -parallel 16 under CPU load ==
PASS
== TestPRDStore under CPU load ==
PASS
PASS: ci-watch-stress-gate
```

Exit code 0; no burners left alive after the gate (each ended by its own
timeout; the script kills only the exact PIDs it recorded).

### Full package, race detector (outside load)

```
$ go test -race -count=1 ./internal/autonomous/
ok  	github.com/dmz006/datawatch/internal/autonomous	44.758s
```

Exit code 0.

### Repository checks

```
$ bash scripts/check-no-unscoped-tmux.sh
OK: no unscoped tmux kills

$ bash scripts/check-no-internal-refs.sh
✓ PASS: no internal refs in user-facing surfaces.
```

Both exit code 0.

### Changed files (git diff --stat main...HEAD)

```
docs/plans/2026-10-28-ci-watch-flake.md            |  86 +++++++++++++
internal/autonomous/ci_watch_test.go               |  15 ++-
internal/autonomous/store_open_live_writer_test.go | 143 +++++++++++++++++++++
internal/autonomous/store_records.go               |  14 +-
internal/autonomous/store_records_test.go          |  11 ++
```

Exactly the expected set, plus the (untracked, gitignored)
.datawatch/verify gate scripts. `git status --short` shows no stray probe
tests or test binaries.
