# Staged restart validation, stage 11 (2026-10-23 18:58)

Restart onto the build with the per-session commit guard, the hermetic server tests and the earlier store change, with one PRD mid-run and its task verifying. Snapshots: `/tmp/sv/restart-test/stage11-before.json` and `stage11-after.json`.

| PRD | Task | Session before | Session after |
|---|---|---|---|
| 61130001 (claude-code) | verifying | 7f3e | 7f3e |

- Result: the in-flight session was kept, with no duplicate session and no task reset.
- The tmux server was untouched (NRestarts 3).
- The capacity API answered in about 4 ms after the restart.
- Open check: this is the first run with the commit guard live. The next task commit by the harness on a running PRD shows whether the guard lets harness commits through.
