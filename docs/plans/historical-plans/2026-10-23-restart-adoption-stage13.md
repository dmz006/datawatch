# Staged restart validation, stage 13 (2026-10-23 21:38)

Restart onto the build that keeps a finished worker and re-verifies only, with one PRD mid-run. Snapshots: `/tmp/sv/restart-test/stage13-before.json` and `stage13-after.json`.

| PRD | Task | Session before | Session after |
|---|---|---|---|
| 2d950001 (claude-code) | verifying, worker session still live | 03cd | 03cd |

- Result: the in-flight session was kept, with no duplicate session and no task reset. The PRD resumed because its worker session was alive.
- The tmux server was untouched (NRestarts 3). The capacity API answered in about 5 ms.
- The journal again named the sender of the stop from the shutdown-cause record.
- Not yet covered live: the stage 12 case (a verifying task whose worker had already finished). That case is covered by the table test and the fake restart test only. It needs a restart at a moment when a task is verifying with a finished worker.
