# Load Proof: idle Stop (612b0009) after the fix

Verified in this worktree on branch
`automaton/64580001--tasks-run-the-load-proof-on-the-fixed-stop-612b0009`.

## Result

**PASS — the fixed Stop waits for the idle loop under load, with no flakiness.**

```
runs=300
failures=0
busy_loops=3
gomaxprocs=1
```

## What was run

Command (from the project root, as specified):

```
bash .datawatch/verify/idle-stop-load-run.sh \
  prove \
  .datawatch/notes/idle-stop-after-fix.txt \
  '^(TestAutomationIdleStartStop|TestCoordinatorStopWaitsForIdleLoop)$'
```

The helper:
1. Builds the race test binary for `./internal/autonomous` (`go test -race -c`).
2. Starts **3 busy loops** (independent `bash` child PIDs, recorded).
3. Runs both named tests **300 times each** under `-race` with
   `GOMAXPROCS=1` (single-scheduler, so scheduler-timing races surface).
4. **Stops the busy loops by the PIDs it recorded** (never by name).
5. Writes the notes file and exits non-zero on any failure.

Then:

```
bash .datawatch/verify/idle-stop-load-proof.sh   # exit 0
```

which re-runs the proof and asserts the notes record
`runs=300`, `failures=0`, `busy_loops=3` → **`ok: runs=300 failures=0 busy_loops=3`**.

## Tests exercised (unchanged)

| Test | Location |
|------|----------|
| `TestAutomationIdleStartStop` | `internal/autonomous/automation_status_test.go:270` |
| `TestCoordinatorStopWaitsForIdleLoop` | `internal/autonomous/automation_idle_stop_test.go:16` |

Relevant fix: the idle loop runs as
`go func(stop <-chan struct{}, done chan<- struct{}) { defer close(done) }…`
(`internal/autonomous/automation_coordinator.go:166`), and `Stop` waits on the
`done` channel the idle loop closes (`internal/autonomous/automation_idle.go:17`),
so `Stop` no longer returns before the idle loop has actually exited.

## Timing

- Proof run: **14:30:13 → 14:44:36 local (863 s)**, 300 runs, 0 failures.
- Go toolchain: `go1.27.2 linux/amd64` (`-race`).

## Constraints honored

- **No tracked files modified**; the two tests and production code were not
  edited. `git status` shows only gitignored `.datawatch/` artefacts (plus this
  report) as new/changed.
- Busy loops were terminated **by the PIDs the helper recorded**
  (`652522 652523 652524`, all confirmed dead afterwards); no `tmux kill`,
  no kill-by-name, no stray loops left running.
- All shell commands wrapped in start/end timestamps and run under
  `env -u TMUX -u TMUX_TMPDIR`.
- Notes file `.datawatch/notes/idle-stop-after-fix.txt` records
  `runs=300`, `failures=0`, `busy_loops=3`.

## Evidence

- Notes file (fresh): `.datawatch/notes/idle-stop-after-fix.txt`
  (`timestamp=2026-11-02 14:44:36`)
- Per-run logs: `.datawatch/tmp/load-run/run.<1..300>.log`
