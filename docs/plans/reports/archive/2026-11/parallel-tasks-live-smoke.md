# Live smoke: parallel tasks + per-task worktrees

**Date:** 2026-11-01 (evening UTC−5)
**Binary:** `datawatch 9.57.0` (built in session, scratchpad, outside the repo)
**Daemon:** sandbox instance via `scripts/test-daemon-lib.sh` (`td_start`/`td_stop`), its own PID, private tmux socket; stopped by recorded PID at the end. No other daemons touched.

## Setup (all outside the repo)

- Scratchpad: `/home/<user>/workspace/.smoke-3ebd/` — driver script, built binary, config, throwaway target repo (`run/projects/throwaway`, git init'd with README + .gitignore), planner HTTP stub (python3), and binary stand-ins for the tmux client and the worker/verifier CLI (`aider`).
- Config: from `testdata/datawatch.yaml`, rewritten to free local ports and scratchpad paths; `autonomous.max_concurrent_tasks: 2`, `capacity_enabled: false`, `worktree_base_repo: <scratch>/run/projects/throwaway`, `worktree_dir: <scratch>/run/wt`. No operator-supplied `project_dir` on the PRD (that is what switches the PRD into worktree mode: daemon created `run/wt/<prdid>` for branch `automaton/<prdid>` per PRD, verified via API after decompose: `{"git":{"branch":"automaton/d4540001"},"project_dir":"…/run/wt/d4540001"}`).
- Task graph (both cases): two read-only tasks with no dependencies, `R1 sleep=25s`, `R2 sleep=35s`; two writing tasks `W1 sleep=8s` (depends on R1+R2), `W2 sleep=8s` (depends on W1). `concurrent_tasks: 2` throughout.
- Case 1 (`per_task=on`): `autonomous.parallel.worktree_per_task = true` (default). PRD `d4540001`.
- Case 2 (`per_task=off`): flipped live via `PUT /api/config` to `{"autonomous.parallel.worktree_per_task":false}` (API confirmed the switch), then a new PRD `8aee0001` with the identical task graph.
- All worker/verifier "LLM" turns were answered by a stand-in binary (deterministic name/sleep/write markers per task); verifier turns wrote the ok verdict JSON. Evidence = daemon API state + stand-in event log (epoch-ns timestamps) + a 1 Hz `git worktree list` sampler + the smoke-repo git log.

## Results

| # | Check | Result |
|---|-------|--------|
| 1 | Read-only tasks overlapped in time (concurrency 2) | **PASS** both cases |
| 2 | Each writing task got its own worktree + branch (case 1) | **PASS** |
| 3 | Merges happened in dependency order (R1+R2 → W1 → W2) | **PASS** both cases |
| 4 | Speedup vs serial > 1 | **PASS** both cases (1.47) |
| 5 | `worktree_per_task` off restores the shared-checkout (serial) model | **PASS** |

## Evidence (live, fresh run 2026-11-01 22:40–22:42)

### 1 — read-only overlap

Stand-in event log, case 1 (PRD `d4540001`):

```
1793590833.299658552 R1 start cwd=…/run/wt/d4540001 branch=automaton/d4540001
1793590833.301286134 R2 start cwd=…/run/wt/d4540001 branch=automaton/d4540001
1793590858.315615221 R1 end            (t+25.0s)
1793590868.317303963 R2 end            (t+35.0s)
```

R1 and R2 were alive simultaneously (t+0.00s → t+25.0s), i.e. 25 s of overlap out of 35 s of wall time. Case 2, PRD `8aee0001`: `R2 start … .813068915` / `R1 start … .813338610` — same overlap. API: both tasks `read_only: true`, both `status: completed`.

Note: read-only tasks keep the PRD checkout by design (`readOnlyTaskSkipsLock`: they write nothing to isolate), so their overlap is preserved in **both** cases — including case 2, which is why case 2's speedup is also > 1.

### 2 — per-task worktree + branch (case 1)

Stand-in start lines (case 1):

```
W1 start cwd=…/run/wt/d4540001-tasks/w1-189f0001  branch=automaton/d4540001--tasks-w1-189f0001
W2 start cwd=…/run/wt/d4540001-tasks/w2-19c30001  branch=automaton/d4540001--tasks-w2-19c30001
```

1 Hz `git worktree list` sampler, while W1 was inside its window (t+39.5s…t+47.5s; 8+ consecutive samples):

```
projects/throwaway   822f1f8 [main]
wt/d4540001          ffba7e5 [automaton/d4540001]
wt/d4540001-tasks/w1-189f0001  ffba7e5 [automaton/d4540001--tasks-w1-189f0001]
```

The sampler never showed a `…-tasks/w2-*` entry while W1's was present, and W2's tree existed only during W2's window — one live task worktree at a time, each task in its own tree/branch.

### 3 — merge order = dependency order

PRD branch `automaton/d4540001` (case 1), from the smoke repo's git log:

```
* a391bfd (automaton/d4540001) Merge task 19c30001: W2
|\
| * ea553ed W2 writes w2.txt
* c6a26a0 Merge task 189f0001: W1
|\
| * b792274 W1 writes w1.txt
* ffba7e5 R2 writes notes_r2.txt
* a307697 R1 writes notes_r1.txt
```

Order: R1, R2 merged first; then `Merge … W1` (only after both reads); then `Merge … W2` (only after W1). API task records agree: W1 `merge_state: "merged", merge_commit: c6a26a07…`, W2 `merge_state: "merged", merge_commit: a391bfd2…`; both read-only tasks `merge_state: null`.

Case 2 (`8aee0001`), same order on the shared branch: `R1 → R2 → W1 → W2` (`060c2c2`/`d28e754`/`8df6ec7`/`3649580`), no merge commits because there was nothing to merge — the writers committed onto the shared PRD branch directly.

### 4 — speedup > 1

Daemon-computed per PRD (from the API):

```
case 1 (per_task=on ): wall 64.37 s, serial 94.73 s, speedup 1.472
case 2 (per_task=off): wall 64.32 s, serial 94.71 s, speedup 1.472
```

Both > 1 (the read-only pair overlaps in both models; see note in §1).

### 5 — `worktree_per_task` off ⇒ shared checkout (serial) model

Case 2 events: every task, read-only and writing alike, ran in the **same** checkout and branch — no `-tasks/` worktrees were ever created:

```
R1 start cwd=…/run/wt/8aee0001 branch=automaton/8aee0001
W1 start cwd=…/run/wt/8aee0001 branch=automaton/8aee0001   ← same tree as R1/R2
W2 start cwd=…/run/wt/8aee0001 branch=automaton/8aee0001
```

Writer tasks were serialized (W1 started at t+79.7s after R2's end; W2 after W1 — the per-checkout write lock applies when there is one shared checkout), and there were no per-task branches/merges. `git worktree list` during case 2 showed only the base repo + the single PRD worktree.

## Environment / lifecycle notes

- Build: `go build -o <scratch>/datawatch ./cmd/datawatch/` (scratchpad). Version banner `9.57.0`.
- Daemon started with `td_start` (recorded PID 2520192), `td_set_api`, private tmux socket via shim; killed at the end with `td_stop` by PID. `pgrep` afterwards: no residual `datawatch`, `aider`-stand-in, or tmux-server processes.
- PRD lifecycle: `decompose` (stubbed planner) → `add_story`/`add_task` → `plan_coverage/acknowledge` → `approve` → `run` → `completed` (66 s each case).
- No repo file changes except this report; driver, stand-ins, binary, config, throwaway git repo, and evidence files all live under the scratchpad.
