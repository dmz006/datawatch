# BL462 — restart audit findings: what happens to a running PRD task when the daemon restarts

Status: live audit complete (sandbox). No product code was changed. Every finding below is backed either by timestamped live evidence or by a code location, and the difference is marked on each one.

## Method and safety

**Production was not touched.** The operator's production daemon (`datawatch` service, MainPID 1742603, ActiveEnterTimestamp 18:37:19, healthz on 8080 responding) was left running and was never restarted or signalled. `systemctl --user restart datawatch` and the `datawatch restart` CLI were **not** run against it. The audit's own session also runs inside a production-managed daemon, so restarting production from here would have killed the audit itself.

**Sandbox pattern used.** Every restart experiment ran against a sandbox daemon started by this audit:

- Config: `.bl462-sandbox/config.yaml`. Its own `data_dir` (`.bl462-sandbox/data`), HTTP port 18090 (`127.0.0.1`), TLS off, MCP SSE port 18091, proxy/channel ports 0, and no messaging channels configured.
- Started with `datawatch start --foreground --config <sandbox config> --port 18090 --host 127.0.0.1 --no-mcp`, with `TMUX` unset and `TMUX_TMPDIR=.bl462-sandbox/tmux` (its own tmux server socket). Production's tmux socket is `~/.datawatch/tmux`.
- Restart = `kill -TERM <sandbox pid>` (the same signal systemd sends on stop), then relaunch with the same command line. Two restarts were done (18:47:14 and 18:48:39 UTC-4).
- Sandbox PRDs and their project dirs (`projects/cc`, `projects/oc`, `projects/sub`) are under `.bl462-sandbox/` as well.

**Deviation from the brief, stated plainly.** The brief asked for the sandbox config, data and project dirs to live in a temp dir outside the repo. The scope rule in the same brief limits writes to `/home/user/workspace/datawatch`. These two rules conflict. The sandbox was kept inside the repo at `.bl462-sandbox/`, which is listed in `.git/info/exclude` so nothing there is committed. Its project dirs are nested git repos, so the sandbox's own auto-commit cannot touch the outer repo. The directory should be deleted once the audit is closed.

**Sandbox deviation 2.** The sandbox shares the host's `~/.claude` and `~/.config/opencode`, and the sandbox's startup log printed `cleaned up 1 stale MCP registration(s)`. I did not verify whether that cleanup touched a production MCP registration. Treat that as an open item.

**Restart method note.** The documented operator command is `systemctl --user restart datawatch` (docs/operations.md). The live test did not use it because it would have restarted production. Restart-equivalence is therefore shown for a SIGTERM-then-relaunch of a daemon with the same binary, config shape and `TMUX_TMPDIR` pattern, not for the systemd unit itself. The systemd path was not exercised.

**Workload.** Two autonomous PRDs, one per backend, each with a single task that runs a bash loop of 240 ticks at 2 s intervals (about 8 minutes):

| PRD | Backend | Task | Session at start |
|---|---|---|---|
| `fad30001` | claude-code | `0d320001` (Timed loop task) | `b755`, tmux `cs-bl462-sandbox-b755` |
| `99a40001` | opencode | `48d60002` (Timed loop task) | `4cb7`, tmux `cs-bl462-sandbox-4cb7` |

Both PRDs were approved and run at 18:46:01. Both tasks sat in `verifying` with their session live, which is the status that the boot-resume path has to handle.

A third, subprocess-mode session `9c87` (`bl462-subproc`, `subprocess:true`, `sleep 420`) was started at 18:48:33 so that it would be running at the second restart.

Timestamps are local (EDT, UTC-4). Log file paths are relative to `.bl462-sandbox/`.

---

## Baseline (before restart #1, 18:47:05)

- Sandbox tmux (`TMUX_TMPDIR=.bl462-sandbox/tmux`): `cs-bl462-sandbox-b755` (pane pid 1772975, cmd `claude`), `cs-bl462-sandbox-4cb7` (pane pid 1772990, cmd `opencode`). Both panes alive.
- Daemon sessions: `b755` = `running`, `4cb7` = `waiting_input`. The `waiting_input` on the opencode session is how the daemon sees an opencode `run` whose TUI prints nothing while a tool call is in flight, not an actual prompt.
- Capacity ledger (`GET /api/capacity`): pool `host` held 2 (holders `0d320001`, `48d60002`), `llm:claude-code` held 1 (`0d320001`, session `b755`), `llm:opencode` held 1 (`48d60002`, session `4cb7`). Both leases bound to their session IDs.
- Timed loops: claude-code at tick 13, opencode at tick 17.

## Restart #1 (SIGTERM 18:47:14.065, old pid gone 18:47:14.574, new pid 1777035 ready 18:47:22)

Boot log (`.bl462-sandbox/daemon2.log`), excerpt:

```
[autonomous] boot-resume: scanning prd=99a40001 status=running
[autonomous] boot-resume: re-launched executor for prd=99a40001
[autonomous] boot-resume: scanning prd=fad30001 status=running
[autonomous] boot-resume: re-launched executor for prd=fad30001
[recovery] starting screen capture for one-shot session bl462-sandbox-4cb7 (state=waiting_input)
[recovery] starting screen capture for one-shot session bl462-sandbox-b755 (state=running)
```

No `boot-resume: reset task` and no `kill orphaned session` line was written for either PRD. Neither task was reset or killed.

### Evidence at 18:47:25 (immediately after restart #1)

**(a) tmux pane and process.** Both tmux sessions still exist and both panes are alive. The pane shell pids are unchanged (1772975, 1772990). The LLM CLI processes are children of those shells and are still running:

- `1773499 claude --dangerously-load-development-channels server:datawatch-bl462-sandbox-b755 ...` under pane 1772975.
- `1773451 opencode run "You are running non-interactively as part of an automated pipeline..."` under pane 1772990.

**(b) daemon tracking and log re-pipe.** Session states after the restart: `b755` = `running`, `4cb7` = `waiting_input`. `pane_pipe` is `1` for both panes, so the pipe-pane bridge is attached. The claude session's `output.log` keeps growing: 38084 bytes at 18:47:51 and 47207 bytes 20 s later. The opencode session's `output.log` stays at 936 bytes over the same window. That is consistent with `opencode run` printing nothing while a bash tool call runs inside the loop, and it is **not** yet proof that the re-pipe is broken for opencode. The timed log for the opencode task kept advancing (tick 35 → tick 38 within the same interval), so the work itself is running. The definitive check is whether `output.log` grows again once the loop returns (see section (d) below).

**(c) capacity lease.** Immediately after the restart `GET /api/capacity` returned:

```
{"pools":[{"name":"host","limit":7,"external":0,"held":0,"holders":null}],"leases":[],"waiting":[]}
```

Both live tasks have **no lease**. The ledger was empty while two LLM tasks were running. See the "Capacity ledger lease on restart" section.

**(d) task completion.** Pending at this point, see the completion section.

## Restart #2 (SIGTERM 18:48:39.537, old pid gone 18:48:40.044, new pid 1780913 ready 18:48:50)

Taken with the subprocess session `9c87` running. Pre-restart state at 18:48:39:

- Timed logs: claude tick 59, opencode tick 62.
- Subprocess children: `1780019 bash -c echo bl462-subproc-start; sleep 420; ...` and `1780020 sleep 420`.
- Ledger: `host` held 0, `external` 1. The subprocess session is counted as an external slot in the host pool.

Boot log after restart #2 (`daemon3.log`): the same two `boot-resume: ... re-launched executor` lines and the same two `[recovery] starting screen capture` lines. No reset, no kill.

### Evidence at 18:49:09 (immediately after restart #2)

- Session `9c87` = `failed`, `subprocess=True`, `tmux_session=""`. See "Subprocess-mode sessions".
- Its real OS processes are still alive: `1780019` (`bash -c ...`) and `1780020` (`sleep 420`). The daemon that owned them is gone; the children were not terminated by the SIGTERM.
- Claude output grows (85147 → 96153 bytes between 18:49:43 and 18:50:08). Its timed log reached tick 157 by 18:51:57.
- Ledger after restart #2: `host` held 0, `leases` empty (not re-acquired).
- Tasks `0d320001` and `48d60002` are still `verifying` on their live sessions `b755` and `4cb7`. Both PRDs are `running`.

No `resuming verify for live session` log line appeared for either task (see Findings F3).

---

## Capacity ledger lease on restart

**Verdict: LOST. Not rebuilt. Not double-counted.** Under the observed load, a task that is still running on a live session after a restart holds no capacity lease until it finishes.

Code path that decides this:

1. **The ledger has no persistence.** `internal/capacity/ledger.go` keeps `leases` and `waiters` in memory only. `capacity.New` builds an empty ledger. There is no save, load or rebuild method. A restart therefore starts with zero leases.
2. **The boot path does not rebuild leases from in-progress tasks.** Nothing in `cmd/datawatch/capacity_wiring.go` or `cmd/datawatch/main.go` walks persisted PRD tasks and calls `Bind` or `Acquire` for them. The only `Bind` call site is `internal/autonomous/executor.go:1044`, which runs after a fresh spawn.
3. **The resumed executor skips admission for a live session.** `internal/autonomous/executor.go` in `executeOne`:
   - Lines ~917–926: if `attempt == 0 && t.Status == TaskVerifying && t.SessionID != ""` and `sessionAliveFn` returns true, `skipSpawn = true`.
   - Lines ~928–: `if !skipSpawn { ... }` contains the kill-before-respawn block and the `m.admit(ctx, prd, t, backend, model)` call (~line 992). So a resumed live session never reaches `admit`, and no lease is taken.
4. **Release is a no-op.** `executeOne` defers `m.releaseCapacity(t.ID)` (~line 882). `Ledger.Release` is a no-op when the holder has no lease. Nothing is double-freed, but nothing is restored either.
5. **The reaper cannot save the lease.** `cmd/datawatch/capacity_wiring.go` runs `led.Reap(live)` every sync tick. It only deletes leases that exist. Since none were rebuilt, there is nothing to reap.

What this means under load:

- **Lost, live proof:** host held 0 with 2 live LLM tasks (restart #1 and #2), and llm pool holders empty.
- **Starvation:** not observed. The host pool limit was 7 and the sandbox had no other load. The lease gap produces **over-admission**, not starvation. Once the host pool is full, new admissions would proceed while the restored tasks still consume real slots.
- **Double-counting:** not observed. No second lease appeared for the resumed tasks.
- **External counting partially compensates for one class of session only.** `cmd/datawatch/capacity_wiring.go` `external()` counts non-`autonomous:` running or waiting sessions on the host pool. The restored PRD tasks are named `autonomous:...`, so they are **excluded** from `external` as well. The subprocess session is not named `autonomous:` and was counted (`external: 1`). So the PRD tasks that restart is meant to protect are the ones the host-pool accounting drops.

---

## Worktree-mode PRD notes

**Live worktree run: not performed.** Creating a worktree-mode PRD needs `WorktreeBaseRepo` configured and a PRD with no explicit `ProjectDir`, `ProjectProfile` or `ClusterProfile`. Then one more restart would be needed. That was not cheap to add to this audit, so it was not run. The findings below are from reading the code.

Code facts (`internal/autonomous/worktree.go`):

- The per-PRD worktree path is `<WorktreeDir>/<prd.ID>`. `WorktreeDir` defaults to `~/.datawatch/prd-worktrees` (or `.datawatch/prd-worktrees` if there is no home dir). It is not a literal `prd-worktrees/<id>/` under the repo.
- `EnsureWorktree` is idempotent on resume: if `prd.Git.Branch` is set and `<path>/.git` exists, it returns the existing path and branch. This is the only restart-aware branch.
- `ensureWorktreeIfConfigured` is called from `executor.go:183`, `manager.go:1040`, and `manager.go:1216`. It is skipped entirely when `prd.ProjectDir` is set (true for both sandbox PRDs, which is why they were not worktree-mode).
- After a restart, the lease gap, the in-progress reset, and the live-session skip are **the same code** as main-tree PRDs. Nothing in `worktree.go` changes behavior on boot.

Implication: a worktree-mode PRD whose session survives a restart would behave exactly like the claude-code and opencode cases above (lease lost, session kept), **but this is inferred from the code and not observed live.**

---

## Subprocess-mode sessions

**Verdict: the code comment in `ResumeMonitors` is partly right. Live, the record is marked `failed` by design, but the OS process is not gone.**

Live evidence for session `9c87` (`subprocess=True`, `tmux_session=""`):

- Started 18:48:33 with `echo bl462-subproc-start; sleep 420; echo bl462-subproc-end`.
- Restart #2 at 18:48:39.5. The old daemon exited. The `bash -c` (pid 1780019) and `sleep 420` (pid 1780020) **survived** the SIGTERM and kept running.
- On the new boot, `ResumeMonitors` (`internal/session/manager.go`) saw `TmuxSession == ""`, set `State = StateFailed`, saved it, and fired `onStateChange` and `onSessionEnd`. The record reads `failed` at 18:49:09, while the child processes are still running.
- After the child finished, `output.log` held the end line `bl462-subproc-end` (18:55:33). The child process was gone by 19:00:03 (`pgrep` count 0). The session record stayed `failed` the whole time, so the daemon never recorded a clean exit.

Characterization:

- Correct: the daemon's goroutine that would have called `subprocessFinish` is gone, so the daemon cannot observe exit. Marking the record `failed` is the only way the scheduler's overlap guard is unblocked.
- Incorrect or at least overstated: "a subprocess has no independent existence a new daemon generation can re-attach to." The process does have independent existence. It survives the daemon. It cannot be re-attached through tmux, but it can be found by pid or by its tracking dir. So the record says the session has ended while a real process is still running and writing. A recurring schedule can then start a second copy of the same job alongside the orphan.

---

## Completion evidence (end of run)

Timeline after restart #2 (times EDT):

- **18:54:46** — the claude-code execution loop reached tick 240. The execution session `b755` printed its last tick and the executor moved task `0d320001` to the verifier. The verifier session `2b05` (`pi`, `autonomous:verify:0d320001`) spawned, and a lease `verify:0d320001` (model `ollama/qwen3.8-64k:27b`, pool `host`) was taken at 18:54:46.674.
- **18:54:46 (approximately)** — `b755` was recorded as `killed`, and its tmux server pane `cs-bl462-sandbox-b755` disappeared. The daemon logs contain no explicit line for this kill. It happened after the loop had ended and during normal operation, after restart #2, so it is **not** attributed to the restart. Noted as an observation (see F10).
- **~18:55:26** — the opencode loop reached tick 240. Its `output.log` grew from 936 bytes to 4221 bytes after the loop ended, so the pipe-pane bridge re-attached after restart #1 and kept delivering output (F6 confirmed).
- **18:55:33** — the subprocess child `sleep 420` exited. The exact end line `bl462-subproc-end` appeared in `data/sessions/bl462-sandbox-9c87/output.log`. The child was still alive at 18:55:35 and gone by 19:00:03 (`pgrep` count 0). The session record stayed `failed` the whole time (F4 confirmed).
- **18:57:31** — opencode PRD `99a40001` reached `completed`. Its task `48d60002` completed with verifier `a10f` (pi, `waiting_input`, output file ready, then killed by the executor). The executor's normal path worked after the restart.
- **~18:59:43** — claude-code PRD `fad30001` reached `completed`. Task `0d320001` completed with verification.
- **Final ledger (18:59:43):** `host` held 0, `leases []`, `waiting []`. No lease leaked and no double-count remained. Final session states: `9c87` failed (subprocess), `2b05` killed (verifier), `a10f` killed (verifier).

(d) per backend, final:

- **claude-code** (`fad30001`): loop completed, verification ran, PRD `completed`.
- **opencode** (`99a40001`): loop completed, verification ran, PRD `completed`.

So both tasks reached their normal terminal state after two daemon restarts, and the PRD rollup advanced. The restart therefore did not break the task outcome in this run. It did break the observability and capacity accounting described above.

Lease-at-verify observation: capacity leases came back for both tasks, but only when the verifier started, as `verify:<taskID>` holders. They were not re-acquired when the restored execution session resumed (F1 remains valid for the execution phase).

---

## Findings

Each finding states BROKEN or CONFIRMED-WORKING, the reproduction, the code location, and (for BROKEN) a fix direction. **No fix was implemented.**

### F1. Capacity leases for in-progress tasks are lost on restart — BROKEN

- **Reproduction:** start a PRD task whose session is live. Restart the daemon (SIGTERM, relaunch). `GET /api/capacity` immediately after returns `host held 0`, `leases []`, while the task and its session still run. Observed twice (restart #1 and #2).
- **Code:** `internal/capacity/ledger.go` (in-memory only); `internal/autonomous/executor.go` `executeOne`, `skipSpawn` path (~917–926) bypasses `m.admit` (~992).
- **Fix direction:** on resume, `Bind` a lease for each live `TaskVerifying`/`TaskInProgress` task at the point where the skip-spawn path is taken. Use the task's `ProjectDir`, backend and model to compute pools (same logic as `keys()` in `capacity_wiring.go`). Alternatively persist leases to the store and rebuild them in boot-resume before any admission. Add a test that restarts with one live session and asserts `held` matches the running task count.

### F2. Host-pool external count drops autonomous sessions — BROKEN (related to F1)

- **Reproduction:** with two live PRD tasks named `autonomous:...`, restart. `external` reports `0` and `held` reports `0`, so the host pool sees no load from either.
- **Code:** `cmd/datawatch/capacity_wiring.go` `external()` excludes sessions whose name starts with `autonomous:`, on the assumption that they are counted through leases.
- **Fix direction:** this exclusion is only correct if F1 is fixed. Fix F1 first, then count live autonomous sessions either through leases or in `external`, not both.

### F3. The resumed executor's "resuming verify for live session" branch was never logged — CONFIRMED-WORKING for the outcome, BROKEN for the observability

- **Reproduction:** restart with a task at `verifying` and a live session. Boot-resume re-launches the executor. The expected line `executeOne: task %s resuming verify for live session %s (daemon restart)` (`internal/autonomous/executor.go` ~924) never appears in `daemon2.log` or `daemon3.log`, in either restart.
- **Outcome:** both tasks still reached `completed` after their sessions ended and verification ran (see completion evidence). So the executor did pick the work back up. Whichever path it took, the restored task was not lost.
- **What is broken:** the log branch that is supposed to mark a resumed live verify is not reached in this run, so an operator cannot tell from the logs that a restored task was handled. The skip-spawn path (`skipSpawn`, executor.go ~917–926) is therefore unverified by evidence.
- **Fix direction:** add an unconditional log line when boot-resume re-launches a PRD, listing each task it found in `verifying`/`in_progress` and what it did with it (re-attached, reset, or left to the executor). Then add a test that exercises the skip-spawn branch.

### F4. Subprocess-mode sessions are marked failed while their processes keep running — BROKEN

- **Reproduction:** start a `subprocess:true` spawn with a long-running command. Restart the daemon. The session reads `failed` at once. `pgrep -af 'sleep 420'` still shows the child. The child finishes normally (end line written to `output.log` at 18:55:33), but the session record stays `failed` for the rest of the run, because no daemon owns it to record the exit.
- **Code:** `internal/session/manager.go` `ResumeMonitors`, the `TmuxSession == ""` branch (~4612–4660).
- **Fix direction:** on boot, look for the child pid (persist it on start, e.g. in the tracking dir). If it is alive, keep the session `running` and re-attach a waiter that polls the pid and reads its output file, then finish it normally when the pid exits. Only mark `failed` when the pid is gone and no exit code was recorded.

### F5. Live claude-code tmux sessions survive a daemon restart and keep being monitored — CONFIRMED-WORKING

- **Reproduction:** two restarts with `b755` live. The tmux pane and `claude` process survive, `pane_pipe=1`, `output.log` grows after each restart, and the PRD task keeps its session.
- **Code:** `internal/session/manager.go` `ResumeMonitors` (re-pipe at ~4700, `RepipeOutput`), `ReconnectBackends`, and boot-resume's `recovery` screen capture.
- **Note:** this is the claim docs/operations.md ~156 makes. It is true for the tmux pane and the output stream. It is **not** true for the autonomous lease (F1).

### F6. Live opencode tmux sessions survive a restart and their output keeps reaching the log — CONFIRMED-WORKING

- **Reproduction:** `4cb7`, opencode, survives both restarts. The `opencode run` child process is alive, its timed log advances, and the pane pipe is attached (`pane_pipe=1`). Its `output.log` stayed at 936 bytes while the loop ran inside a single tool call, which matches opencode printing nothing during a tool call. Once the loop returned, `output.log` grew to 4221 bytes, so the re-attached pipe delivered output after both restarts.
- **Code:** same as F5 (`ResumeMonitors` re-pipe, `recovery` screen capture).

### F7. `ReconcileLiveSessions` has no caller, and `reconcile_on_startup` is never read — BROKEN (dead wiring)

- **Reproduction:** `grep -rn ReconcileLiveSessions --include=*.go` finds only the definition. `grep -rn ReconcileOnStartup` finds the config field and a comment in `cmd/datawatch/main.go:1295`, with no read.
- **Code:** `internal/session/reconcile.go:94–107` says "Wired into startup unconditionally". That wiring does not exist.
- **Impact:** orphaned session dirs that are not in the store are not recovered at boot. Not exercised in this audit, because every session here was already in the store.
- **Fix direction:** either wire `ReconcileLiveSessions` into boot (before `ResumeMonitors`), or correct the comment and remove the dead `reconcile_on_startup` key.

### F8. The worktree restart path is identical to main-tree, and reuse on resume is correct — CONFIRMED-WORKING (code reading only)

- **Code:** `internal/autonomous/worktree.go` `EnsureWorktree` reuses `<WorktreeDir>/<id>` when `.git` exists and `Git.Branch` is set.
- **Not live-tested.** It inherits F1 and F3.

### F10. A claude-code execution session was marked `killed` after its loop ended, with no log line explaining it — OPEN (not attributed to the restart)

- **Reproduction:** claude-code task `0d320001`. Its loop finished at ~18:54:46, the verifier `2b05` spawned at the same moment, and session `b755` then read `killed` and its tmux pane disappeared. The daemon logs contain no kill line for `b755`.
- **Why it matters:** the task still completed, so the outcome was fine. But the execution session was killed by something that is not logged. It is not attributed to either restart, since both restarts had happened earlier and this happened during normal operation.
- **Fix direction:** log the caller and reason whenever a session transitions to `killed`, so that a silent kill is distinguishable from an operator or executor kill.

### F9. docs/operations.md restart guidance is correct for tmux sessions and silent about autonomous leases — PARTIALLY ACCURATE

- **Claim (~156):** "Active tmux sessions survive the daemon restart... Sessions in running or waiting_input state continue uninterrupted." Confirmed by F5 and F6 for the tmux and process layer.
- **Claim (~631–640):** "systemctl restart datawatch preserves tmux sessions." Consistent with the SIGTERM-and-relaunch test, but the systemd unit itself was not exercised.
- **Gap:** neither section says that PRD task capacity is lost on restart (F1), that a subprocess session is marked failed (F4), or that the executor may not verify a resumed task (F3). Doc fix should follow the code fixes.

---

## Open items

- Systemd unit path (`systemctl --user restart datawatch`) not exercised, to protect production.
- Worktree-mode PRD not run live (code reading only).
- Multi-ComputeNode parallel GPU streams not exercised (single node).
- Long-running open-model task beyond the 8-minute loop not exercised.
- Whether the sandbox's startup `cleaned up 1 stale MCP registration(s)` touched a production MCP registration is not verified.
- The sandbox directory `.bl462-sandbox/` should be deleted once the audit is closed.

---

## Fix status (2026-10-18)

Code fixes for the BROKEN findings. Unit tests cover each fix's scenario. Only the live restart gate can confirm the systemd and live-daemon behaviour, and that gate has not been run.

- **F1 — Fix status:** code fix. `capacity.Ledger.Adopt` (internal/capacity/ledger.go) records a non-waiting, idempotent lease for a live task. `Manager.adoptCapacity` (internal/autonomous/capacity.go) calls it from `resetInProgressTasksForResume`'s live-session branch (boot pass) and from `executeOne`'s skip-spawn branch. Covered by `TestAdoptRestoresLeaseOverLimitWithoutWaiting`, `TestAdoptIsIdempotent` and `TestCapacity_BootResumeReadoptsLiveTaskLease`. Live gate still needed: confirm `GET /api/capacity` shows `held 1` per live task right after a restart. Note that the audit's live tasks were in `verifying`, the only live state that survives boot. `in_progress` tasks are always reset at boot, so they hold no session to re-adopt.
- **F2 — Fix status:** no separate code change. `external()` in cmd/datawatch/capacity_wiring.go still excludes `autonomous:` sessions. With F1 fixed, those tasks are counted once, through their leases, so no double count is introduced. Covered by `TestCapacity_BootResumeReadoptsLiveTaskLease`, which asserts a single lease and that a sibling is not admitted over it.
- **F3 — Fix status:** the boot pass now logs unconditionally for each live `verifying`/`running_tests` task it keeps (`boot-resume: prd=... task ... kept (was ...) on live session ..., capacity lease re-adopted`). The audit's missing `resuming verify for live session` line is not explained by code reading, so the live gate must confirm which path a restored task takes.
- **F4 — Fix status:** code fix. A subprocess session now writes `subprocess.pid` (wrapper pid) and `subprocess.rc` (exit status) in its session directory, in internal/session/subprocess_resume.go. On boot, `ResumeMonitors` re-attaches a session whose task is still running (matched by pid and command line) and keeps it `running`. It finishes the session from `subprocess.rc` when the task ends. It marks the session `failed` only when the task is gone with no recorded status. Covered by `TestSubprocessSurvivesDaemonRestart`, `TestSubprocessEndedWhileDaemonDownUsesRecordedStatus` and `TestSubprocessGoneWithoutStatusIsNotReattached`. Caveat for the live gate: systemd's default `KillMode=control-group` kills the child on stop, so this helps only when children survive the daemon.
- **F7 — Fix status:** comment fix, not wiring. Boot already runs `ReconcileSessions(true)` (cmd/datawatch/main.go), which imports every orphan dir, live or stopped. The `ReconcileLiveSessions` comment and the `reconcile_on_startup` comment were wrong and now say so. The config key stays for compatibility and is not read. Removing the key would change the public config surface, which is out of scope.
- **F5, F6, F8 — Fix status:** CONFIRMED-WORKING. No code change.
- **F9 — Fix status:** doc follow-up, not done in this change. Operations docs should mention the lease and subprocess caveats once the live gate passes.
- **F10 — Fix status:** OPEN, not BROKEN. No code change in this task. Logging the caller on `killed` transitions is a separate change.
