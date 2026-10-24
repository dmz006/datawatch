# Restart matrix findings

Date: 2026-10-24. Harness: `scripts/restart-matrix.sh` with `scripts/restart-matrix/`. Daemon: built from this tree to a path under the temp dir and run only as a sandbox (own config, own data dir, free ports, TLS off, own multiplexer socket dir). The production daemon, its ports and its data dir were never touched; the harness refuses to start when a target matches them.

The matrix puts one unit of autonomous work into each of twelve states, restarts the sandbox daemon, and records what became of the work. No real model is involved: planner, worker, verifier and guardrail judge are one fake shell backend that writes the same outputs a real role would, and the fake worker ends on the completion marker a real worker prints. Values below come from the record files the run wrote (`records/<state>.json`, plus before / immediately-after / settled snapshots).

## Result

Six of twelve rows survive a restart cleanly. Six do not.

| Survives | Rows |
|---|---|
| yes | worker running, waiting for capacity, waiting on an external condition, merge or git operation in progress (see its limit), scheduled job due during restart, open WebSocket clients |
| no | planning, verifying through a live verifier session, verifying through a daemon-run script, verifying with the script half done, final gate script running, Automation with active branches |

Two causes explain every failing row:

1. A task in `verifying` whose worker session has already ended is reset to pending at boot and its worker runs again. The boot path that would keep it and only verify (`ResolveVerifyingResume`, used by `resetInProgressTasksForResume` in `internal/autonomous/manager.go`) is never reached, because `reconcileStuckTasks` in the same file runs first and decides with `ResolveVerifyingTask`, which only knows "session live" or "ok verdict recorded". Rows: verifying through a live verifier session, both script rows, final gate, and the Automation branch whose worker had finished.
2. A planning PRD is reset to draft and its live decompose session is killed (`internal/autonomous/api.go` planning case and `killOrphanDecomposeSessions` in `internal/autonomous/manager.go`), because the goroutine that would read the plan is gone. The PRD then waits for the operator.

The daemon-run verification script adds a third problem on its own: it is a direct child of the daemon (`runVerificationScript` in `internal/autonomous/verification_script_exec.go`, `exec.Command("bash", …)`) with no durable record, so a restart either ends it (control group) or orphans it (process), and a re-run can overlap an orphan.

## The matrix

Measured with `RM_KILL_MODE=control-group` (the mode that matches the unit; see the next section). Task status is what the API reports: a task whose worker is still running or has just ended reports `verifying`, and a task whose worker has ended while its checks run can report `completed`. "Settled" is the state after the work stopped moving. Session ids and pids are from the run recorded for this document; they differ on every run.

| State | Survives | Task status: before → just after → settled | Session ids | Process ids | Retry count | Duplicates | Lost output | Work redone | Why it does or does not survive | Smallest fix |
|---|---|---|---|---|---|---|---|---|---|---|
| Decompose or planning running | **no** | PRD `planning` → `draft` → `draft` (no task exists yet) | decompose session 1bb8 live → 1bb8 ended, none live | daemon 2699990 → 2703967; planner 2701640 dead right after | 0 → 0 | 0 | 0 bytes (the planner had written nothing) | planning is not redone; the PRD stays `draft` until an operator plans again (planner starts 1 → 1) | The boot pass resets every `planning` PRD to `draft` and kills the live decompose session as an orphan. The consumer of its output was a goroutine inside the dead daemon. Log: `boot-resume: killed orphan decompose session … ` and `was planning on restart, reset to draft`. | **OPERATOR PICK** (A re-plan automatically, B adopt the live session, C leave for the operator); see below |
| Worker running | yes | `verifying` → `verifying` → `completed` | worker d227 → d227 | daemon 2761721 → 2768695; worker 2765008 alive; pane shell 2764755 unchanged | 0 → 0 | 0 | 0 bytes; counter file 45 of 45 lines | no (worker starts 1 → 1) | The worker lives in a multiplexer session outside the daemon's process tree. Boot-resume re-attaches (`resetInProgressTasksForResume` adopts the live session and re-takes its capacity lease) and the executor waits on it instead of spawning. | none needed |
| Worker finished, verifying through a live verifier session | **no** | `verifying` → `in_progress` → `failed` | verifier 9aea live → ended; worker 9955 (ended) → 1938 → af34 | daemon 2778936 → 2784265; verifier 2782149 dead right after; worker pane shell 2781901 → 2786693 | 0 → **1** | 0 live | 0 bytes | **yes: worker starts 1 → 3**; verifier never reached again | The verifier session is ended during the restart. The task, `verifying` with a finished worker and no verdict, is reset to pending by `reconcileStuckTasks` (`boot-reconcile: … status=verifying … → TaskPending (session gone on daemon restart)`), so the finished worker runs again and a retry is counted. The task then ended `failed`: the re-run worker wrote the same file, so the verifier saw no diff. That last step is partly a property of this harness's fake worker and a non-git project; the two redone workers and the extra retry are not. | In `reconcileStuckTasks`, resolve with `ResolveVerifyingResume` (given the session output function) and, for the re-verify outcome, leave the task `verifying`, re-take its lease, and continue, exactly as `resetInProgressTasksForResume` already does. Not applied in this pass. Re-running the verifier once is acceptable. |
| Verifying through a daemon-run script (script launched, not started) | **no** | `completed` → `verifying` → `completed` | worker 0a36 → db6c; a second verifier session ran | daemon 2852261 → 2859929; script child 2857207 ended by the harness; new worker pane shell 2861845 | 0 → 0 | 0 | 0 bytes | **yes: worker starts 1 → 2, verifier starts 1 → 2, script launched 2, finished 1** | Same reset as the row above (`boot-reconcile … → TaskPending`), so worker and verifier run again. The script is the daemon's own child with no record, so it is lost with the daemon and started again. In process mode the first script is orphaned, not ended, and keeps running: launched 2, started 2, finished 2, two scripts overlapping in one tree. | The same `reconcileStuckTasks` change removes the worker and verifier re-run; the script then runs once more from the start. **OPERATOR PICK** for where the script runs; see below |
| Verifying, script half done (start marker written) | **no** | `completed` → `verifying` → `completed` | worker 0940 → 9d2b; second verifier 568b | daemon 2868873 → 2875001; script child 2872383 ended by the harness | 0 → 0 | 0 | 0 bytes | **yes: worker 1 → 2, verifier 1 → 2, script started 2, finished 1** | As above, with the script already inside its work: that work is thrown away and redone from the start. Process mode: started 2, finished 2 (the orphan ran to completion beside the re-run). | As above. **OPERATOR PICK** |
| Waiting for capacity | yes | `waiting_capacity` → `waiting_capacity` → `completed` | holder worker fc69 unchanged; the waiting task has no session until admitted (c2ff) | daemon 2882607 → 2887262; holder pane shell 2884737 unchanged | 0 → 0 | 0 | 0 bytes | no (two workers in total, one per task) | The wait is a ledger entry rebuilt at boot from the live holder session, and the holder survived. Admission ran after the holder finished (`pool host full (1/1, held by …)` before, admitted afterwards). | none needed |
| Waiting on an external condition | yes (limit below) | `verifying` → `in_progress` → `completed` | worker a9fd → a9fd | daemon 2901111 → 2905815; parked worker 2902761 alive; pane shell 2902513 unchanged | 0 → 0 | 0 | 0 bytes | no (worker starts 1, finished once) | The parked worker's session survived and was adopted; the task was not failed or reset; after the condition was released the worker finished once. Limit: the scenario parks a single worker. The variant where the task releases its host slot while parked and waits for re-admission behind another task is not exercised (see unmeasured). | none needed for the measured variant |
| Final gate script running | **no** | `completed` → `verifying` → `completed` (kind `final_gate` kept) | worker 95ca → 4abb; second verifier ce8e | daemon 2911846 → 2916608; gate script child 2914348 ended by the harness; new pane shell 2918051 | 0 → 0 | 0 | 0 bytes | **yes: worker 1 → 2, verifier 1 → 2, gate script started 2, finished 1** | Identical to the script rows: a final-gate task has the same reset-and-redo path, and its script is the same unrecorded child. Process mode: finished 2. | As the script rows. **OPERATOR PICK** |
| Merge or git operation in progress | yes (limit below) | `in_progress` → `in_progress` → `completed` | none before (no session yet) → worker 63df after | daemon 3079585 → 3084736; the `git commit` child 3082416 ended by the harness | 0 → 0 | 0 | 0 bytes | no (first worker start is after the restart; 0 → 1) | The commit interrupted is the daemon's task-start snapshot commit, held inside a 20 s pre-commit hook. Git removed its own index lock on TERM (no stale `index.lock`), the hook finished, the task was reset to pending (no session yet), and the commit was made once more by the new run. One commit adds the result file, the tree ended clean. Limit: see unmeasured. | none needed for the measured commit |
| Automation with active branches | **no** | the Automation: `active` → `active` → `completed`; the two branch PRDs: one completed, one `failed` | kept branch worker 0dd5 → 0dd5; the other branch's worker 552a (ended) → redone twice: a829 (killed as stale before the next respawn), then f09b | daemon 2942375 → 2964007; worker panes 2961554 and 2951664 → 2961554 only | 0 → 0 (kept branch) | 0 | 0 bytes | **yes: worker starts 3 → 5** (the branch whose worker had finished ran twice more) | The Automation itself came back: both cursors (2 → 2), status `active`. The branch whose worker was still alive was kept and finished. The branch whose worker had finished while verifying hit the same reset as the verifier row, was redone, failed its verification (no diff), and blocked the join. | The `reconcileStuckTasks` change above. Nothing Automation-specific. |
| Scheduled job due during the restart | yes | none (no PRD): schedule `pending` → fired once → `done` | none → d7d0 (spawned after boot) | daemon 3031576 → 3036999, down 33 s across the due time | 0 → 0 | 0 | 0 bytes | no (the spawn ran once; fire count 1 after a 25 s grace) | The due time passed while the daemon was down; the scheduler fired the missed job once after boot and marked it done. | none needed |
| Open WebSocket clients | yes (limit below) | `verifying` → `verifying` → `verifying` (worker running throughout) | worker 46d5 → 46d5 | daemon 3041208 → 3045565; pane shell 3042607 unchanged | 0 → 0 | 0 | 0 bytes | no | Both clients saw the drop and reconnected 9.0 s after it (a 5 s outage, the boot, and the client's own retry), neither gave up, and messages flowed again (5 before, 5 after). The first message on reconnect is a full session snapshot, so state resyncs. Events emitted while the daemon was down are not replayed. | none needed; clients must not assume gap-free delivery |

## OPERATOR PICK

These rows have a fix that is a design choice. Nothing here is decided.

### Planning running (decompose)

Current behaviour: reset to `draft`, live decompose session killed, operator re-plans by hand.

| Option | What it is | Cost |
|---|---|---|
| A. Re-plan automatically | After the boot reset, start planning again once (bounded), instead of leaving `draft`. The old session is still killed. | Smallest. A planner run is spent twice; no new code path for reading a half-written plan. |
| B. Adopt the live session | Stop killing it. At boot, find the decompose session linked to the PRD, then read its output file when it appears (and handle a session that finished while the daemon was down). | Keeps the work and matches how workers are adopted. Needs a new consumer for the output file and care with a partly written plan. |
| C. Leave it, surface it | Keep today's reset, but mark the PRD as "planning interrupted" with a one-step re-plan. | No automatic action; still a manual step per restart. |

Recommendation: A now. Planning is the cheapest role to repeat and A needs no half-written-file handling. Revisit B only if planning runs get long enough that a repeat hurts.

### Where the daemon-run verification script runs (script, script half done, final gate)

Current behaviour: a direct child of the daemon with no record. Under a control-group stop it dies with the daemon and starts again from the beginning; under a process-only stop it is orphaned and can overlap its own re-run.

| Option | What it is | Cost |
|---|---|---|
| A. Run the script in a multiplexer session | Same machinery as workers: the session outlives the daemon, boot adopts it, a small wrapper writes the exit code and output next to the log. | Reuses adoption, liveness and logs. Needs the wrapper's exit-code file and a rule for a script that ended while the daemon was down. |
| B. Detached run plus a result file the daemon adopts at boot | Start the script outside the daemon's control group, have it write a result file (exit code, tail of output, timestamps) to a durable per-task path, and have boot adopt that file if the script ended or wait on its pid if it is still running. | Keeps scripts out of the multiplexer. Needs a way to leave the unit's control group, and its own pid and liveness tracking. |
| C. Keep it a daemon child and make the re-run cheap | After the `reconcileStuckTasks` fix, re-run only the script (not worker or verifier) on boot. Scripts must be safe to run twice. | Smallest, no new moving parts. A long script loses its progress on every restart, and an orphan can still overlap if a stop only signals the daemon. |

Recommendation: C immediately, since it comes with the shared fix and removes the worker and verifier re-runs, and A as the follow-up for any script that runs for minutes. A beats B because it adds no second adoption mechanism. Decide on A or B before any script is allowed to be long or have side effects that are not repeatable.

## Smallest fixes, in one place

Not applied in this pass (the task is measurement). Each names where the change goes.

1. `reconcileStuckTasks` in `internal/autonomous/manager.go`: use `ResolveVerifyingResume` and keep a finished-worker task `verifying`, as `resetInProgressTasksForResume` does. Clears the verifier row, the three script rows' worker and verifier re-runs, and the Automation branch row.
2. Planning PRDs on boot (`internal/autonomous/api.go`, `killOrphanDecomposeSessions` in `internal/autonomous/manager.go`): see the pick above.
3. Verification script placement: see the pick above.

## KillMode control-group: what depends on it and how the harness simulates it

The unit service uses KillMode `control-group`: on stop or restart the service manager sends TERM to every process in the unit's control group at once, not only to the daemon. Anything the daemon starts directly and keeps in its own group dies with it. Workers, verifier and planner sessions are not affected this way because they live in a multiplexer server that is not part of the daemon's tree; the harness relies on that and does not test the opposite.

Rows that depend on the kill mode (the others have no daemon-child process in play, and were run in `control-group` mode only):

| Row | Control group | Daemon pid only |
|---|---|---|
| Verifying through a daemon-run script | script child ended; re-run starts it again (launched 2, finished 1) | script child orphaned and keeps running; launched 2, started 2, finished 2 |
| Script half done | child ended mid-run; re-run completes (started 2, finished 1) | orphan finishes and the re-run finishes: started 2, finished 2 |
| Final gate script running | gate child ended; started 2, finished 1 | started 2, finished 2 |
| Merge or git operation | the `git commit` child ended; no stale lock; result committed once | git is left running and completes; result committed once |
| Planning and verifier session | no difference measured: the sessions are ended by the daemon's own boot and shutdown paths in both modes | same |

How the harness simulates it: `RM_KILL_MODE` in `scripts/restart-matrix/lib.sh`. The restart always sends TERM to the recorded sandbox daemon pid (after checking the pid is alive and its command line carries this sandbox's config). In `control-group` mode (the default) it first lists the daemon's direct children by pid and then sends TERM to each of those pids as well, right after the daemon's. In `process` mode only the daemon pid is signalled. The children it ended are written into each record (`restart.children_terminated`). Nothing is ever signalled by name, and no service manager, multiplexer-server kill or restart command is used.

Limits of the simulation, plainly:

- It is a stand-in. The sandbox daemon is not in a unit and has no control group; the harness cannot show what the service manager does. It ends direct children only, not grandchildren (a hook run by a `git commit` child kept running in both modes), and sends the signals one by one, not atomically.
- A multiplexer server started by the daemon detaches from its tree, so it is never ended here. In a real control group a multiplexer started inside the daemon's unit would be ended too. The production arrangement, where the multiplexer runs in its own unit, is assumed and not tested.
- Only TERM is covered. A crash, a kill signal, an out-of-memory kill, a host reboot, and the service manager's stop timeout followed by a hard kill are not simulated (every restart here exited on TERM within a few seconds, so the timeout path was never taken).
- The restart-time ordering of the real system (children ended in parallel with the daemon's own shutdown work) is not reproduced; a child that the daemon would have reaped in its shutdown handler might behave differently.

## What the harness could not measure

- **Merge or git operation in progress:** only the task-start snapshot commit was interrupted. The commit that takes a worker's output did not run the pre-commit hook in a trial that armed the hook after the worker started, so it could not be held open that way. A merge of a finished branch, a push, and an interrupted commit of worker output are therefore not measured. The row's "yes" covers the snapshot commit only.
- **Waiting on an external condition:** only the variant where the parked worker keeps its session alive. The variant where the task gives up its host slot while parked and waits for re-admission behind another task is not exercised, so how that wait is rebuilt after a restart is unmeasured.
- **All roles are fake.** No real model, no real terminal UI session, no real completion detection from a live TUI, no real MCP channel reconnection. The fake worker prints the completion marker on its last line; a real worker that stalls or ends without the marker is a different case.
- **Process mode was run for six rows only** (planning, verifier session, the two script rows, final gate, git operation): the rows with a daemon child in play, or planning. The other six have no direct child and were run in control-group mode only.
- **Task status is the API's view.** A running worker reports `verifying`; a task whose worker has ended while its checks run can report `completed` while the stored status is still `verifying`. The boot log (`boot-reconcile: … status=verifying`) is the stored value.
- **The failed end state of the verifier row** (`failed`, verifier saw no diff) is shaped by the fake worker writing an identical file in a project that is not a git repository. A real re-run would more often produce a diff; the redone workers and the extra retry do not depend on that.
- **Single Automation shape:** one entry member, a fork into two branches, a join. Other graphs are not measured.
- **Concurrency:** one state at a time against its own daemon. A restart with several PRDs in different states at once is not measured.
- **One daemon version, one machine, one run per cell** (git operation was run again after the harness change; its numbers are from that run). No repeat counts for timing-sensitive cells such as the WebSocket reconnect time.

## Changes made to the harness during this pass

- `RM_KILL_MODE` (above) was added; before it, the verification script in the test project ended itself when its parent died, which hid the process-only behaviour. That self-ending line is gone.
- The fake worker now prints the completion marker last. A first full run was made without it and gave the same failing rows, so the marker did not change any outcome.
- The "work redone" flag no longer counts a role's first start after the restart as a redo (this had marked the git row as redone when its worker had simply not started yet).
- The git row's description now says which commit it interrupts.

## Reproduce

```bash
sandbox_dir="$(mktemp -d)"   # keep the path short: the multiplexer socket path has a length limit
go build -o "$sandbox_dir/datawatch-sandbox" ./cmd/datawatch/
export DATAWATCH_BIN="$sandbox_dir/datawatch-sandbox" RM_TMP_BASE="$sandbox_dir"
RM_KILL_MODE=control-group scripts/restart-matrix.sh       # all twelve rows
RM_KILL_MODE=process scripts/restart-matrix.sh --only planning,verify-session,verify-script,verify-script-mid,final-gate,git-operation
scripts/restart-matrix.sh --list                           # state names
```

A full control-group run takes about 25 minutes. Records land under `<work root>/records` (one JSON per state, `summary.json`, and `snapshots/`). `--keep` keeps each state's sandbox dir (daemon log, `data/daemon-crash.log` which holds the structured `[autonomous]` lines quoted above). `--strict` exits 1 when any row fails.
