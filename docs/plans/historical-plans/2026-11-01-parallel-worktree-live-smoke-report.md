# Live smoke: parallel read-only / worktree / merge / speedup (2026-11-01)

This report replaces the earlier version, which carried results from an attempt whose raw evidence was
gone. Every row below was captured in this attempt, from a daemon built from this tree
(`go build -o datawatch ./cmd/datawatch/`, daemon version 9.57.0), and the raw excerpts are inline.

## Setup

- Sandbox daemon started with `scripts/test-daemon-lib.sh` (`td_start`, own PID, `td_install_traps`), run as
  `env -u TMUX -u TMUX_TMPDIR bash smoke.sh`. Ended on every exit path by recorded PID and marker only; no
  `pkill`, `killall` or `pgrep -f`. Loopback ports probed free, own data dir, own private tmux socket
  (`tmux -S` shim, kept short because the socket path limit is 107 bytes). Afterwards: no daemon process, no
  planner process, no scratch dir left.
- Throwaway git repo (default branch `main`) with `autonomous.worktree_base_repo` pointing at it, so each
  Automaton runs in its own PRD worktree (`wt/<prd>`). Per-task worktrees only exist in that mode.
- `autonomous.max_concurrent_tasks: 2`, `autonomous.capacity_enabled: false`.
- Stand-ins: a planner HTTP stub (so decompose runs without a model) and an `aider` stand-in that reads the
  task text, sleeps, commits a file, and also acts as the verifier by writing an `ok` verdict.
- Graph: `R1` and `R2` (read-only, no dependencies), `W1` (needs R1 and R2, writes `w1.txt`), `W2` (needs W1,
  writes `w2.txt`). The driver is kept next to this report as
  `2026-11-01-parallel-worktree-live-smoke-driver.sh.txt`.

## Results

| Check | Result | Evidence |
|---|---|---|
| Read-only tasks overlapped in time | PASS | R1 and R2 started 0.2 ms apart, R1 ran 10 s, R2 13 s, so they overlapped for 10 s (`concurrent_tasks: 2`) |
| Each writer had its own worktree | PASS | W1 ran in `wt/754e0001-tasks/w1-787e0001` on branch `automaton/754e0001--tasks-w1-787e0001`, W2 in `wt/754e0001-tasks/w2-78ec0001` on its own branch; the 1 s `git worktree list` sampler saw the task worktree appear |
| Merges happened in dependency order | PASS | PRD branch history: `Merge task 787e0001: W1` (e7ab78c) before `Merge task 78ec0001: W2` (47eaf85); both tasks `merge_state: merged` with those merge commits |
| Speedup field above 1 | PASS | `total_tasks 4, read_only_tasks 2, concurrent_tasks 2, wall_clock_seconds 37.2, serial_seconds 52.5, speedup 1.412` |
| `worktree_per_task` off returns old serial behaviour | PASS | Set with `PUT /api/config {"autonomous.parallel.worktree_per_task":false}` and read back `false`. Every task ran in the single PRD worktree (`wt/99f60001`, branch `automaton/99f60001`), no `-tasks/` worktree appeared, no `merge_state` was set, history is linear (no merge commits), W1 finished before W2 started |

Run 1 (per-task worktrees on), from the stand-in event log (epoch seconds, `…` = sandbox path):

```
1793581613.5505 R2 start cwd=…/wt/754e0001 branch=automaton/754e0001
1793581613.5507 R1 start cwd=…/wt/754e0001 branch=automaton/754e0001
1793581623.5584 R1 end
1793581626.5588 R2 end
1793581632.0679 W1 start cwd=…/wt/754e0001-tasks/w1-787e0001 branch=automaton/754e0001--tasks-w1-787e0001
1793581641.3908 W2 start cwd=…/wt/754e0001-tasks/w2-78ec0001 branch=automaton/754e0001--tasks-w2-78ec0001
```

Run 2 (`worktree_per_task` off): R1 and R2 again started 8 ms apart and overlapped; W1 and W2 both ran at
`cwd=…/wt/99f60001`, W1 ending at 1793581674.4 before W2 started at 1793581680.7. Its speedup also read
1.412, because read-only overlap is controlled by `read_only_tasks`, not by `worktree_per_task`. The off
setting turns off the per-writer worktrees and merges, not the read-only overlap.

## Findings

A. **Retracted: "merge never happens".** The earlier report said task branches were never merged. That came
   from running without worktree mode (no `worktree_base_repo`), where per-task worktrees are skipped by
   design (`taskWorktreeEnabled` needs `prd.Git.Branch`). In worktree mode the merges happen, in order.
   A user who sets `worktree_per_task` on a project that is not in worktree mode gets no worktrees and no
   merges, with no message saying why. Worth a log line or a status field.
B. **Confirmed live: a read-only task can never pass the verifier.** With an unchanged tree the verifier
   returns "no changes detected (diff empty and no new files); task produced no output"
   (`cmd/datawatch/main.go:5829`). R1 and R2 each ran 3 times, went to `review_needed` ("held for operator
   review after 3 failed attempts") and the Automaton went `blocked`. `resolve_task_review` accepts only
   `retry` or `cancel`, so there is no way to accept such a task. Read-only tasks need an empty-diff
   exemption. For this smoke the stand-ins committed a small note file so the verifier had a diff.
C. **Confirmed live: aider task text arrives as literal text.** The backend builds
   `--message '$(cat '/path')'`; the shell removes the inner quotes, so the worker receives the string
   `$(cat /path)` and never the task (`internal/llm/backends/aider/backend.go:80`). The stand-in had to read
   the path out of that string.
D. Not re-tested: with the capacity gate on, the aider cap may serialise read-only tasks. This run set
   `capacity_enabled: false`.
E. **Confirmed live: first-run `.gitignore` edits fail read-only tasks.** At the first run the daemon adds
   ignore entries (`.decompose-output-*.json`, `CHECKPOINT.md`, `.goose/`, `.gemini/` and others). In a repo
   whose `.gitignore` lacks them, a read-only task fails with "read-only task changed the tree: git status
   is dirty after it finished (?? .gitignore)" (and `M .gitignore` when the file existed). The smoke avoided
   it by committing those entries first.

## Spec note

The brief says READ-ONLY with no repo files changed, but a task that changes nothing cannot be recorded as
a delivered change, and this report is the one repo file added. No source file was changed.
