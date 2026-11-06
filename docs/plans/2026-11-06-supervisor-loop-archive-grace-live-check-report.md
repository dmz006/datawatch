# Supervisor-loop archive grace — live check on a sandbox daemon

**Date:** 2026-11-06 (run started 16:18:47, local time)
**Version under test:** datawatch v9.98.1 (binary built from this worktree)
**Script:** `scripts/supervisor-loop-archive-live-check.sh` (run as `env -u TMUX -u TMUX_TMPDIR bash scripts/supervisor-loop-archive-live-check.sh`)
**Scope:** the `autonomous.supervisor_loop.archive_cancelled_after_minutes` setting (round-trip and refusal), the grace holding a cancelled Automaton whose branch carries unmerged work, the immediate archive of a cancelled Automaton whose branch carries no work — with the `archive` decision recorded under the actor `supervisor-loop` — against a throwaway project repository whose default branch is `trunk.

## Result

**Overall:** PASS

| # | Check | Result | Assertions |
|---|---|---|---|
| 1 | Config: the grace setting round-trips | PASS | 4 of 4 passed |
| 2 | Grace 60: a cancelled Automaton with unmerged branch work survives the pass | PASS | 7 of 7 passed |
| 3 | Grace 0: a cancelled Automaton with no branch work is archived by the pass as supervisor-loop | PASS | 8 of 8 passed |
| 4 | Cleanup | PASS | 2 of 2 passed |

## Setup

- **Sandbox daemon only.** Started with `td_start` from `scripts/test-daemon-lib.sh` from a config generated from `testdata/datawatch.yaml` by the substitutions the test runner uses (data folder, ports, loopback host, test token). Added: `autonomous.worktree_base_repo` and `autonomous.worktree_dir` (the throwaway project and its worktree root), `server.allowed_project_roots`, and `shell_backend.script_path` (the stand-in worker). The production daemon, its service and its data were not touched, and nothing was ended by name.
- **Ports.** HTTP 47427, TLS 51979, MCP 48465, DNS 41993, webhook 34233, notify 52769, each probed free just before use.
- **Session sockets.** The daemon ran with its multiplexer socket directory under the scratch folder, so no session of the operator was reachable and none was touched. The script itself strips its own multiplexer variables and issues no multiplexer command.
- **Everything** lives under one scratch folder inside the project's `.datawatch` folder, removed on every exit path.
- **Throwaway project.** A bare repository whose default branch is `trunk` and which has no `main`; `refs/remotes/origin/HEAD` records it — the base the merge compare and the branch guard measure against.

```
refs/remotes/origin/trunk; default branch: trunk (deliberately not main)
```

- **Worker.** The daemon's shell backend ran a generated script as the worker. The work-carrying Automaton's task declares the write directory `src/` and the worker writes one file there, which the session's own commit stages and commits — that is the one commit the branch carries. The no-work Automaton's task declares the same write directory but the worker writes nothing, and a session with nothing to stage never commits, so its branch stays in step with `trunk. Every task carries a deterministic verification script and the daemon's verification is answered by the same script with a fixed pass, so no model is involved anywhere.

| Automaton | Worker mode | Used in |
|---|---|---|
| `e2a10001` archive-work-branch | writes its file under `src/` (one commit the branch keeps) | checks 2, 3 (kept at both grace values) |
| `e2a10002` archive-no-work-branch | writes nothing (branch carries no work) | check 3 (archived at grace 0) |


## 1. Config: the grace setting round-trips

- **PASS** — PUT autonomous.supervisor_loop.archive_cancelled_after_minutes = 60 is accepted (200)
  - response `
200`

- **PASS** — the round-trip: GET reads the value back as 60
  - stored value after the write: `60`; before the write: `60`

- **PASS** — PUT autonomous.supervisor_loop.archive_cancelled_after_minutes = -1 is refused (400)
  - response code `400`, body: `
400`

- **PASS** — the refused write saved nothing
  - stored value after the refused write: `60`

## 2. Grace 60: a cancelled Automaton with unmerged branch work survives the pass

- **PASS** — approve + run the work-carrying Automaton through the REST API
  - Automaton `running`

- **PASS** — the work-carrying Automaton completes
  - Automaton `completed`, task `{"status":"completed"}`; worker runs: 1

- **PASS** — its branch carries a commit the default branch has not merged
  - `automaton/e2a10001-archive-work-branch` is 2 commit(s) ahead of `trunk`; worktree commit: 65e993f Merge task e2a10001t1: Sample task for archive-work-branch

- **PASS** — the cancel returns 200 and leaves the Automaton cancelled
  - status route `200`; status `cancelled`; cancel decision actor: `operator` (operator)

- **PASS** — the close_finished_records pass runs and is recorded
  - run: ok=`true` result=`closed 0 finished records (held 1 cancelled within the 60-minute grace)`; run records for the job before/after: 0/1

- **PASS** — the cancelled Automaton survives the pass (not archived)
  - status after the pass: `cancelled`; the pass reported it as held within the grace: `closed 0 finished records (held 1 cancelled within the 60-minute grace)`

- **PASS** — the branch work is untouched (still 2 commit(s) ahead)
  - `automaton/e2a10001-archive-work-branch` is 2 commit(s) ahead of `trunk`; worktree `<sandbox>/worktrees/e2a10001/` still present: yes

The run record of the pass (the newest one):
```
{"job":"close_finished_records","outcome":"ok","result":"closed 0 finished records (held 1 cancelled within the 60-minute grace)"}

```

## 3. Grace 0: a cancelled Automaton with no branch work is archived by the pass as supervisor-loop

- **PASS** — setting the grace to 0 round-trips (PUT 200, GET reads 0)
  - response code `200`, body: `
200`; stored value: `0`

- **PASS** — approve + run the no-work Automaton through the REST API
  - Automaton `running`

- **PASS** — the no-work Automaton completes
  - Automaton `completed`; worker runs: 1 (the nop mode wrote no file)

- **PASS** — its branch carries no work of its own
  - branch `automaton/e2a10002-archive-no-work-branch`: ahead-of-default = `0` (ref-gone means the daemon already released it, which is the same evidence)

- **PASS** — the cancel returns 200 and leaves the Automaton cancelled
  - status route `200`; status `cancelled`; cancel decision actor: `operator` (operator)

- **PASS** — the pass archives it immediately (grace 0, no branch work)
  - run: ok=`true` result=`closed 1 finished records (kept 1 cancelled with unmerged branch work)`; status after the pass: `archived`

- **PASS** — the archive decision is recorded under the supervisor-loop actor
  - latest archive decision actor: `supervisor-loop`; all decisions: `approve:operator run:autonomous git_pushed: git_pushed: cancel:operator archive:supervisor-loop`

- **PASS** — the branch-carrying Automaton is still kept even at grace 0
  - status of the first Automaton: `cancelled`; its branch is still 2 commit(s) ahead; the pass reported: `closed 1 finished records (kept 1 cancelled with unmerged branch work)`

The run record of the pass (the newest one):
```
{"job":"close_finished_records","outcome":"ok","result":"closed 1 finished records (kept 1 cancelled with unmerged branch work)"}

```

## 4. Cleanup

- **PASS** — the sandbox daemon and its sessions are gone and no test daemon was left behind
  - daemon gone: yes (pid 3980484); port 51979 closed: yes; leftover check exit 0 (OK: no leftover test daemons)

- **PASS** — the kept branch work survived the daemon ending (still readable in the scratch project)
  - worktree `<sandbox>/worktrees/e2a10001`; branch ahead: ref-gone (ref-gone is not expected here — the work was never merged)

- The daemon was ended by `td_stop_all` from `scripts/test-daemon-lib.sh` (the recorded pid, config path as marker); nothing was ended by name.
- The scratch area (project, remote, worktrees, data, config, log, records, worker, state) is removed by the exit handler right after this report is written.

## Daemon log, the supervisor-loop lines

```
2026/11/06 16:18:46 [fingerprint] wired: store=<sandbox>/data/autonomous/fingerprint/matches.json interval=15m0s peers=0
2026/11/06 16:18:47 [autonomous] prd=e2a10001 worktree created at <sandbox>/worktrees/e2a10001 (branch=automaton/e2a10001-arch
2026/11/06 16:18:47 tree lock: acquired <sandbox>/worktrees/e2a10001-tasks/sample-task-for-archive-work-branch-e2a10001t1/.dat
2026/11/06 16:18:53 autonomous: prd e2a10001 push-on-complete (branch): pushing automaton/e2a10001-archive-work-branch to remote origin
2026/11/06 16:18:53 autonomous: prd e2a10001 push-on-complete (branch): pushing automaton/e2a10001-archive-work-branch to remote origin
2026/11/06 16:18:53 [autonomous] prd=e2a10001 ship step failed: ship: read release version: open <sandbox>/worktrees/e2a10001/
2026/11/06 16:18:55 [autonomous] prd=e2a10002 worktree created at <sandbox>/worktrees/e2a10002 (branch=automaton/e2a10002-arch
2026/11/06 16:18:55 tree lock: acquired <sandbox>/worktrees/e2a10002-tasks/sample-task-for-archive-no-work-branch-e2a10002t1/.
2026/11/06 16:19:02 autonomous: prd e2a10002 push-on-complete (branch): pushing automaton/e2a10002-archive-no-work-branch to remote origin
2026/11/06 16:19:02 autonomous: prd e2a10002 push-on-complete (branch): pushing automaton/e2a10002-archive-no-work-branch to remote origin
2026/11/06 16:19:02 [autonomous] prd=e2a10002 ship step failed: ship: read release version: open <sandbox>/worktrees/e2a10002/
2026/11/06 16:19:03 [autonomous] prd=e2a10002 orphaned worktree removed (<sandbox>/worktrees/e2a10002)
```


## Notes and what is not shown live

- **How the two states were made.** The Automata were written into the sandbox store before its first start (the create route only makes drafts and a plan needs a planner model, which a sandbox does not have). Approve, run, both cancels, both settings and both manual job runs went through the REST API of the running daemon; the worktrees and branches were made by the daemon itself. The cancels are therefore recorded as the operator's, and only the archive was recorded as `supervisor-loop` — the division the check is named after.
- **The grace is a time floor, and the branch guard is absolute.** The pass archives a cancelled Automaton only when it has been cancelled for at least the grace AND its branch carries no unmerged work: unmerged branch work keeps the Automaton at any grace value, which is why the first Automaton is still `cancelled` after the grace-0 pass in check 3 while the second is `archived` in the same one.
- **Not shown live:** the PWA and CLI reads of the setting and the run records (the REST reads are the ones this feature writes through), the idle-tick driven pass (the manual job run drives the identical step), and an archive after the grace elapsed naturally instead of being set to zero — the arithmetic is the same branch in the step, covered by its unit tests.

Result: PASS
