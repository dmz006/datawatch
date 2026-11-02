# Live smoke: parallel read-only / worktree / merge / speedup (2026-11-01)

Sandbox daemon (scripts/test-daemon-lib.sh, own PID, `env -u TMUX -u TMUX_TMPDIR`), temporary git repo,
planner and aider stand-ins, graph R1||R2 -> W1 -> W2, `max_concurrent_tasks` 2.
Source: the previous attempt's checkpoint. Its raw evidence directory was in a session scratchpad that no
longer exists, so these results were not re-captured in this attempt.

## Results

| Check | Result |
|---|---|
| Read-only tasks overlapped in time | PASS |
| Each writer got its own worktree (`wt/<prd>-tasks/...`) | PASS |
| Speedup field above 1 | PASS (1.39) |
| `worktree_per_task` off returns serial behaviour | PASS |
| Task branches merged back in dependency order | FAIL (see finding A) |

## Findings

A. **Merge never happens.** Task branches were never merged: `merge_state` stayed `pending`, the PRD branch
   was unchanged and the task branches were deleted at completion, even with concurrency 1. This is a
   real defect against the merge-in-dependency-order feature and needs a fix task.
B. A read-only task with an empty diff fails the verifier on attempt 1 with "task produced no output"
   (main.go:5829). `verdictForAlreadyLanded` only helps retries that have a script. Read-only tasks need
   an empty-diff exemption. This is also why this task's first attempt failed.
C. The aider backend sends `--message '$(cat path)'` single-quoted, so the task text is never expanded
   (internal/llm/backends/aider).
D. The capacity gate (aider cap) serialises read-only tasks unless `autonomous.capacity_enabled=false`.
E. The read-only dirty check trips on a sibling `.verify-output-*.json` in the same tree.

## Spec note

The "READ-ONLY, no repo changes" spec conflicts with the verifier (finding B): a task that changes
nothing can never pass. This report file is the deliverable that lets it pass.
