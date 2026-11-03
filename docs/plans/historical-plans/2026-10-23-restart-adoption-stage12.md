# Staged restart validation, stage 12 (2026-10-23 20:55) — task reset, not adopted

Restart onto the build with flexible verifier routing, with one PRD mid-run and its task verifying. Snapshots: `/tmp/sv/restart-test/stage12-before.json` and `stage12-after.json`.

| PRD | Task | Session before | Session after | Outcome |
|---|---|---|---|---|
| 2d950001 (claude-code) | verifying (daemon-run verification script) | ed66 | none | task reset to pending; ended live session local-node-0907 ("task reset on daemon restart") |

- Result: **not adopted.** The task went from verifying to pending with an empty session link, so the worker will redo work it had already finished.
- Cause (from the boot reconcile code and journal): the task was in verification that the daemon runs itself as a script. Its worker session was finished and idle, so boot reconcile saw no live verifier session, found no recorded verdict, and reset the task to pending, which ended the idle worker session. The code path is `resetInProgressTasksForResume`, case `TaskVerifying`.
- Stages 10 and 11 kept their verifying tasks because those had a live session. This is the case they did not cover: a verifying task whose worker finished and whose verification is a daemon-run script.
- Wanted behaviour: when a verifying task has a finished worker and a recorded worker result, boot reconcile re-runs only the verification and keeps the worker's work, instead of resetting the task and ending the session.
- The shutdown-cause recording worked: the journal names the sender of the stop (a shell command from this supervisor session, found by process scan).
- tmux was untouched (NRestarts 3). The capacity API answered in about 4 ms after the restart.
- Filed as a defect row and a draft PRD (see the registry).

## Fix

Boot reconcile now keeps a verifying task whose worker finished and which has no recorded verdict. It re-runs only that task's verification, does not end the worker session and does not count a retry. A verifying task whose worker has not finished still resets to pending. The live re-validation of this fix is the next restart stage.
