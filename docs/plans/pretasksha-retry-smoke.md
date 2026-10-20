# pre_task_sha on retry — live smoke (2026-10-20)

Goal: on an isolated sandbox daemon, prove that a task which fails its first
attempt and is retried commits real work, that the retry's `pre_task_sha` is the
HEAD captured immediately before the retry launched, that the verifier does not
report a no-output failure, and that the task ends `completed`.

Result: **PASS** for the retry path (run 4). Three observations are recorded below
as remaining questions/defects; none blocks the `pre_task_sha` contract.

## Sandbox

- Binary: `./datawatch` built from this worktree (`go build -o datawatch ./cmd/datawatch/`), version 9.5.33.
- Location: `.smoke/` inside the worktree (deleted at the end). The task brief asked for
  writes only under the worktree, so the `mktemp -d` suggestion was not used.
- Daemon: `datawatch start --foreground --config .smoke/config.yaml`, started with
  `env -u TMUX TMUX_TMPDIR=.smoke/tmux`, bound to `127.0.0.1:18791`, TLS off, empty token.
  Production daemon (`~/.datawatch`) was not touched.
- Config (`.smoke/config.yaml`):
  ```yaml
  data_dir: <worktree>/.smoke/data
  hostname: dw-retry-smoke
  server: {enabled: true, host: 127.0.0.1, port: 18791, tls_enabled: false, token: ""}
  autonomous: {enabled: true}
  shell_backend: {enabled: true, script_path: <worktree>/.smoke/worker.sh}
  ollama: {enabled: true, host: http://127.0.0.1:11434, model: qwen3.8-64k:27b}
  ```
  Autonomous config was then set with `POST /api/autonomous/config {"planning_backend":"shell"}`
  so decompose ran through the deterministic worker (the Ollama path failed with
  "no reachable ComputeNode"). Other runtime values were the defaults.
- Project: `.smoke/proj`, its own git repo with seed commit (`git init`, `user.name smoke`).
- Worker `.smoke/worker.sh` (deterministic, dispatches on prompt):
  - decompose prompt → writes a one-story/one-task plan JSON to the `.decompose-output-*.json` path it is given;
  - verify prompt → writes `{"ok": …}` to the `.verify-output-*.json` path: ok only if `retry-output.txt` exists;
  - task prompt → attempt 1 makes no change and exits 0; attempt 2+ writes and commits `retry-output.txt`.

## Commands (abridged)

```sh
# health
curl -s http://127.0.0.1:18791/api/health
# create + decompose + approve + run
curl -s -X POST -d '{"spec":"…retry-output.txt…","project_dir":".smoke/proj","backend":"shell","effort":"low"}' \
  http://127.0.0.1:18791/api/autonomous/prds                       # -> PRD 91b90001
curl -s -X POST -d '{}' http://127.0.0.1:18791/api/autonomous/prds/91b90001/decompose   # -> needs_review
curl -s -X POST -d '{}' http://127.0.0.1:18791/api/autonomous/prds/91b90001/approve
curl -s -X POST -d '{}' http://127.0.0.1:18791/api/autonomous/prds/91b90001/run
# attempt 1 failed verification -> task failed (category content)
curl -s -X POST -d '{"task_id":"4ab00002","actor":"smoke"}' \
  http://127.0.0.1:18791/api/autonomous/prds/91b90001/reset_task  # retry
curl -s http://127.0.0.1:18791/api/autonomous/prds/91b90001      # observe
```

## Observed (run 4, PRD `91b90001`, task `4ab00002`)

Timeline (sandbox git log, committer times, local):

| time | commit | subject (truncated) | note |
|---|---|---|---|
| 18:44:34 | b1e398d | seed commit | — |
| 18:44:35 | af6a99e | task: You are decomposing… | decompose auto-commit (base_sha) |
| 18:44:38 | 1e308eb | pre-session[]: … | snapshot of dirty drift files at attempt-1 launch |
| 18:44:38 | f4502fb | task: You are running… | attempt-1 session end |
| 18:44:42 | d3195df | task: SECURITY NOTE… | verifier 1 end |
| 18:44:45 | 4633451 | task: You are running… | next spawn (see observation 3) |
| 18:44:48 | 36fce9a | task: SECURITY NOTE… | last commit before the retry |
| 18:44:50 | — | — | verifier verdict 1: `ok:false`, "retry-output.txt missing" |
| 18:44:57.37 | — | — | `reset_task` (actor smoke): "reset to pending for retry" |
| 18:44:57.43 | — | — | `run` (retry launched) |
| 18:44:57 | 701e79d | pre-session[]: … | snapshot of drift files, after pre_task_sha was captured |
| 18:44:57 | b035b80 | retry commit (attempt 2) | worker's real work; parent = 701e79d |
| 18:44:57 | 216658f | task: You are running… | retry session end |
| 18:45:01 | 16830d3 | task: SECURITY NOTE… | verifier 2 end |

Task state after retry (`GET /api/autonomous/prds/91b90001`):

- PRD status: `completed`; task status: `completed`; `error`: null.
- `pre_task_sha` = `36fce9a1300f779cd723a1adb6b97f37ddcc547b`
- `base_sha` = `36fce9a1300f779cd723a1adb6b97f37ddcc547b`
- `attempt_commit_shas` = `["216658f90980407516f2b84f501120e8486d2c6a"]`
- `verification` = `{"ok": true, "severity": "info", "summary": "retry-output.txt present", "verifier_backend": "shell"}`
- `prior_attempt_count` = 2; `retry_count` cleared by the manual reset.
- Decisions: `decompose` (autonomous), `approve` (operator), `run`, `file_drift_detected`,
  `reset_task` ("reset to pending for retry", actor smoke), `run`, `file_drift_detected`.

Before the reset, the failed state was: `status failed`, `pre_task_sha d3195df…`,
`base_sha af6a99e…`, `verification.ok false`, `summary "retry-output.txt missing: no change was made"`,
`failure_reason.category content`.

### Is the retry's pre_task_sha the correct pre-attempt HEAD?

Yes. `pre_task_sha` is captured by `capturePreTask` in `cmd/datawatch/main.go` (`git rev-parse HEAD`)
before the session starts. The `pre-session[]` snapshot commit (`internal/session/git.go`,
`PreSessionCommit`) happens after that capture, so it is not part of `pre_task_sha`. For the
retry, the last commit before the reset-run launched was `36fce9a` (18:44:48); no commit landed
between it and the 18:44:57 launch. `pre_task_sha` = `36fce9a` matches that. The retry's own
commit `b035b80` has parent `701e79d` (the `pre-session` snapshot), which descends from `36fce9a`.

`base_sha` was re-anchored to `36fce9a` because a human `reset_task` clears
`BaseSHA` on purpose ("starting over", `internal/autonomous/manager.go`). This is by design,
not the automatic-retry path, which keeps the first attempt's anchor.

### Verifier

- Attempt-1 verdict: `ok:false`, category `content`, summary about a missing file. That is a
  real content failure, not a no-output failure.
- Attempt-2 verdict: `ok:true`. No "completed but output file not found" or no-output error.

## Remaining defects / questions (recorded, not fixed)

1. **Verifier scratch files are left in the project dir and get committed.**
   `.verify-output-*.json` files stay in the project repo after their verdict is read. Decompose
   output is relocated under the PRD scratch dir; verify output is not. Evidence:
   - Retry `file_drift_detected` note lists `.verify-output-1bc2f79b3e6ac2e8.json` as
     "pre-existing uncommitted file" (18:45:03).
   - Task `files_touched` contains `.verify-output-1bc2f79b3e6ac2e8.json`.
   - Final sandbox tree still holds `.verify-output-c93213352f8f8592.json` and an
     `ca21e34597b01360` file.
   In run 2 this changed the verifier prompt (the scratch file entered the diff).
2. **Extra verifier spawn killed without output.** For task `4ab00002`, verify session `14fd`
   was spawned and killed with no output file (daemon log, `[autonomous:verify:4ab00002]`), and
   no failure surfaced. Verdicts came from `2d3d` (false) and `31d1` (true). Not investigated.
3. **Unexplained spawn before the reset.** A session at 18:44:45 (commit `4633451`,
   `pre_task_sha d3195df`) produced no worker commit and no failure record, and its cause is
   unknown. It may be the content auto-fix path (`content_auto_fix_enabled: true`); not confirmed.
4. **`attempt_commit_shas` holds session-end auto-commits.** It contains `216658f` (the
   `task: You are running…` commit closing attempt 2), not the worker's own commit `b035b80`, which
   is its parent. Whether this is intended is a question for the maintainers.

Harness notes (not product defects):

- Run 2 failed with "output file … not found" because the worker grepped the first
  `.verify-output-*` path it saw, which appeared inside the quoted diff. Fixed with `tail -1`.
  The executor classified it correctly as infra.
- Sandbox daemon logs `proxy sandbox listener: could not bind 127.0.0.1:8444` because the production
  daemon holds that port. `/remote/` is unavailable in the sandbox; the smoke does not use it.
- Ollama planning requires a registered ComputeNode (`compute node "datawatch-ollama" not found`).
  The smoke used `planning_backend: shell` to avoid this.

## Cleanup

- Sandbox daemon (pid recorded in `.smoke/daemon.pid`) stopped with `kill` and confirmed gone.
- `.smoke/` (config, data dir, project repo, tmux dir, logs) removed.
- `CHECKPOINT.md` removed.
- The worktree `datawatch` binary is ignored by git (checked with `git status`).
- No other sessions, PRDs or processes were touched.
