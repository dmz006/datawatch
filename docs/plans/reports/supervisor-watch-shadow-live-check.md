# Supervisor watch — sandbox live check (shadow, then on)

Sandbox daemon: 3267028, port 18781, config path tmp-sandbox/svw8/dw/config.yaml
Production daemon: not touched (no request, signal, service command or file read was sent to it; the sandbox daemon ran with `TMUX` and `TMUX_TMPDIR` unset and with `HOME` pointed at a scratch directory, so it could not adopt the dedicated tmux socket the production daemon uses — see Observations 1 for what this does and does not prove; the one contact is that the sandbox daemon tried to bind port 8444, which the production proxy holds, and the bind failed)
Alerts raised in shadow mode: 0 supervisor-watch alerts (reads of the alert list while in shadow at 14:34:46, 14:36:27 and 14:41:51, and again at 14:42:25 after the switch but before the next pass; 24 alerts in total on the first three reads, all of them session events or the review-hold alert, none from the watch; `would_alert` set on the 10 open findings)
Alerts raised in on mode: 10 for 10 open findings, all at one pass (14:42:35), none repeated at the next pass (14:43:35)
Finding kinds raised live: 9 of 14 — `main_checkout_dirty`, `verify_script_not_executable`, `task_failed`, `idle_lane_eligible_work`, `model_not_resident`, `automaton_not_merged`, `worktree_no_gomod`, `worktree_not_registered`, `worktree_silent`
Finding kinds not raised live: 5 of 14 — `task_review_needed` (tried live, cannot hold on the real data: see below), `tmux_restart`, `daemon_inactive`, `ci_red`, `ci_stuck` (not producible in a sandbox); each has a row naming its unit test
Triage-record write: exercised live twice — a first-sighting note on the failed Automaton's record (`task_failed`) and on the merge-pending Automaton's record (`automaton_not_merged`)
Code changed by this check: none (only this report was written)

## What was checked

The daemon-side supervisor watch (`autonomous.automation.supervisor_watch.*`) was run against a sandbox daemon only. The
check proves the watch end to end for every finding kind a sandbox daemon can raise from state created through its own
REST API, and for every other kind says which unit test covers it or why it cannot be produced. Shadow mode with a
one-minute alert window and a one-minute silence window was set over the sandbox REST API. The findings were read back
over REST, the MCP tool (through the sandbox's `POST /api/mcp/call` bridge) and the CLI. The check confirmed that findings
are stored, that `would_alert` is set, that the alert sink was not called in shadow, that switching to on produced exactly
one alert per open finding with no repeat, that the comparison record shows an injected agreement and a deliberate
disagreement, and that the triage-record note path works.

## How the state was created

All Automaton, compute-node and config state was created through the sandbox REST API. Nothing was written into the
sandbox daemon's data directory.

- **Config:** `PUT /api/config` for `autonomous.automation.supervisor_watch.{mode,alert_minutes,silent_minutes,script_output_path,main_checkout,working_model}`,
  `autonomous.worktree_base_repo`, `autonomous.worktree_dir`, `autonomous.automation.merge_pending_mode` (on, so a finished
  Automaton gets its merge-pending record) and `autonomous.automation.merge_pending_alert_minutes` (1).
- **Compute node:** `POST /api/compute/nodes` registered `sbx-node` (kind ollama) pointing at the fixture server; the watch
  reads node addresses at daemon start, so the sandbox daemon was restarted once after the node was added.
- **Automata:** `POST /api/autonomous/prds`, then `…/plan`, `…/plan_coverage/acknowledge`, `…/set_verification_llm`,
  `…/set_failure_escalation`, `…/approve`, `…/run`, `…/cancel` and `DELETE …?hard=true`. No REST, MCP or CLI call sets a
  task's verification script path (only the planning step writes it) and a draft Automaton has no tasks, so the planning step was
  answered by a **fixture server**, a small local HTTP server (Python standard library, kept outside the repository, started and
  stopped by recorded PID) on 127.0.0.1:18782 that stands in for a model node. It answers `GET /api/tags`, `GET /api/ps`
  and `POST /api/generate`: a plan with one story and one task chosen by a marker word in the Automaton spec, and, for the
  verifier, a verdict. The daemon's own planning and verification code parsed those replies. It is a model stand-in, not a
  stand-in for any host command, and it wrote nothing into the daemon. Worker sessions were real shell sessions started by the
  daemon.
  - `MARK-NOTMERGED`: one task that commits a file on the Automaton's worktree branch, with verification script `ok.sh` (exit 0)
  - `MARK-REVIEW` and `MARK-FAILED`: one task that finishes, with verification script `fail.sh` (exit 1); the fixture verdict
    is "not passed" for a task spec carrying the word `fails-check`
  - `MARK-SILENT`: one task that runs `sleep 900`, with verification script `check.sh`
  - no marker: one task, never run
- **Fixtures the daemon only reads:** a small git repository with a `go.mod` and two executable scripts (the
  `worktree_base_repo`, in a scratch directory outside the repository); a second small git repository with one modified
  tracked file at tmp-sandbox/svw8/maincheckout (the `main_checkout` value); the script-output file
  tmp-sandbox/svw8/script-record.json; a project directory for the silent Automaton holding `check.sh` with mode 0644. The
  project, worktree and base-repository directories are in a scratch directory **outside the repository**, listed in
  `server.allowed_project_roots` of the bootstrap config.
- **Bootstrap config:** tmp-sandbox/svw8/dw/config.yaml was written by hand (a daemon needs one to start): hostname, an
  absolute data directory, the shell session backend, a loopback listener with a token, no TLS, MCP listener off, memory
  off, and the planner pointed at the fixture server. Because the planner settings are part of the bootstrap, they are part
  of the prior config the restore compares against.
- **Not done, stated plainly:** no stand-in for `systemctl`, `gh` or any other host command was used; `PATH` was not changed;
  no tmux command was run by this check.

### Kind by kind

| kind | live | covered by / reason |
|---|---|---|
| `main_checkout_dirty` | **yes** (seen at the 14:21:37 pass) | `main_checkout` set by PUT to tmp-sandbox/svw8/maincheckout, which holds a modified tracked file. Also `TestSupervisorWatchHostMainCheckoutDirty` |
| `verify_script_not_executable` | **yes** (14:31:35) | Automaton `e91a0001` (silent case), project directory with `check.sh` mode 0644, running. Also `TestSupervisorWatchHostVerifyScriptNotExecutable` |
| `task_failed` | **yes** (14:33:35) | Automaton `69ff0001`: escalation disabled for it, the task's worker finished, the fixture verdict said "not passed", the task failed after 2 attempts ("verification failed after retries"), the Automaton failed, its triage record named the task. Also `TestSupervisorWatchWorkTaskFailed` |
| `idle_lane_eligible_work` | **yes** (gpu-1, gpu-2 seen at the 14:33:35 pass and open until the end; claude-1, claude-2, claude-3 opened and resolved while workers came and went) | Automaton `2a6c0001`, `lane_preference` gpu, approved and left unrun. Also `TestSupervisorWatchWorkIdleLaneEligibleWork` |
| `model_not_resident` | **yes** (seen at the 14:21:37 pass) | compute node `sbx-node` registered over REST with the fixture server's address; `working_model` set to `sbx-absent-model`; the fixture's `GET /api/ps` lists only `stub-planner`. Also `TestSupervisorWatchHostModelNotResident` |
| `automaton_not_merged` | **yes** (14:27:35) | Automaton `e8d30001`, worktree mode (no project directory given, so the daemon made a registered worktree and branch from the base repository): the worker committed one file to the branch, the verification script `ok.sh` passed, the Automaton completed, its triage record went to `merge_pending` (3 commits ahead of main — the daemon's own snapshot commits plus the worker's). Also `TestSupervisorWatchWorkAutomatonNotMerged` |
| `worktree_no_gomod` | **yes** (seen by the 14:33:35 pass) | Automaton `e91a0001` runs in an explicit project directory without a `go.mod` while the registered base repository has one. Also `TestSupervisorWatchWorktreeNoGomod` |
| `worktree_not_registered` | **yes** (seen by the 14:33:35 pass) | the same project directory is not in the base repository's worktree list. Also `TestSupervisorWatchWorktreeNotRegistered` |
| `worktree_silent` | **yes** (e91a0001 seen at the 14:33:35 pass; also an earlier Automaton `78fb0001`, first sighting 14:23:37, resolved after it was deleted) | the silent Automaton's task is in progress with a shell worker running `sleep 900`; neither the project directory nor the session log changed for over the one-minute window. Also `TestSupervisorWatchWorktreeSilent` |
| `task_review_needed` | **no — tried live, cannot hold** | Automaton `28070001` (escalation `max_failures` 1): its task reached `review_needed` after one failed attempt and the Automaton went `blocked` ("task 2a0b0002 needs operator review"), but its triage record carries only the blocked reason and **no task id**, so the check's condition (the record names a task that is in the review state) never held, at three passes. The record builder (`triageFailureTask`) names only failed or blocked tasks, never a task held for review. Covered by `TestSupervisorWatchWorkTaskReviewNeeded`, which builds the record with the task id by hand. Reported for the operator as a probable gap between the check and the record it reads |
| `tmux_restart` | no | covered by `TestSupervisorWatchHostTmuxRestartMismatch`, `TestSupervisorWatchHostTmuxRestartFirstSight`, `TestSupervisorWatchTmuxBaselinePersistsAcrossPolls`. Not producible in a sandbox: the finding reads the user service manager's restart count for the dedicated tmux unit, and a sandbox must not restart that unit |
| `daemon_inactive` | no | covered by `TestSupervisorWatchHostDaemonInactive`, `TestShellProbeDaemonActiveReadsNonZeroExitStates`. Not producible in a sandbox: it reads the daemon's own service unit state, which for a sandbox is the production unit, and this check does not touch the production service (not even a read) |
| `ci_red` | no | covered by `TestSupervisorWatchWorkCIRed`. Not producible in a sandbox: the repo CI watch records it reads come from `gh` against GitHub using the operator's credentials, and no fake CI source is configurable |
| `ci_stuck` | no | covered by `TestSupervisorWatchWorkCISTuck`; same reason as `ci_red` |

All 17 tests named in the table were run together at 14:44:52 and passed (see Verification).

## Observations

1. **Where the sandbox's worker sessions ran.** Worker and verifier sessions were real shell sessions, so the sandbox daemon
   used tmux. The daemon, if `TMUX_TMPDIR` is unset, adopts the account's dedicated socket (the production daemon's) when that
   socket exists under `$HOME`; to avoid that, `HOME` was set to a scratch directory for the daemon (the first start had
   `HOME` pointing at a path that does not exist, by a shell mistake; it was stopped at once and restarted). None of the
   four sandbox daemon logs had the "using dedicated socket" boot line (count 0 in each, checked before the logs were
   deleted), so the daemon did not adopt the production socket. What this does **not** show: which socket path tmux
   actually used. With `TMUX_TMPDIR` unset it is tmux's default socket under the temporary directory, which any other tmux
   use on the account shares; this check ran no tmux command and cannot show the path. Sessions carry unique names and the
   daemon ended only its own (`kill_sessions_on_exit` is on).
2. **The daemon boot line "proxy sandbox listener: could not bind 127.0.0.1:8444 … address already in use".** The port is the
   production proxy's. The bind failed and nothing was sent to it; the sandbox's `/remote/` view is simply unavailable.
3. **The first worker's completion was never detected.** Automaton `78fb0001`'s session log stayed empty because the first
   bootstrap config used a relative data directory (tmux resolves a relative pipe-pane path against its own directory), so the
   daemon never saw the marker; the marker also printed within seconds of the task echo, which the daemon suppresses as an
   echo. Fixed in the check (absolute data directory, a `sleep 8` before the marker); the Automaton was cancelled and deleted.
   This is a property of the check's fixture, not a finding about the watch. That stuck worker is also what gave the early
   `worktree_silent` sighting for `78fb0001`; the finding resolved at the next pass after the Automaton was deleted.
4. **A shell verifier cannot give a verdict.** With the default verifier a shell worker's task sat in `verifying` until the
   verifier session was killed; the later Automata were pointed at the fixture node for verification (`set_verification_llm`).
   Two Automata (`f4e30001`, `a6f70001`) completed instead of failing because the first fixture rule keyed on text the
   verifier prompt does not carry; both were deleted and redone with a rule keyed on the task spec.
5. **Clearing config over REST.** Every key set by this check read back to its prior value after restore, including the
   string keys. The full config compared key by key (506 keys): 2 differences, both a key that was `null` before and `[]`
   after (`acme.domains`, `autonomous.verification_backends`), which is how the daemon renders an empty list after a config
   write, not a value this check set.
6. **Passes slip.** Polls ran at 14:33:35, 14:35:35, 14:36:35, 14:38:35, 14:41:35, 14:42:35 and 14:43:35 (the idle tick lands
   slightly before the one-minute guard and a poll slips a minute). Not changed.
7. **A re-sighting needs the alert window.** The three claude-lane findings opened and resolved inside `alert_minutes` (1),
   so they never got `would_alert` and never alerted.
8. **A deleted Automaton leaves its finding until the next pass.** `worktree_silent` for `78fb0001` stayed open for one pass
   after the Automaton was deleted and then resolved.

## Procedure and outputs

All writes stayed under tmp-sandbox/svw8 (removed at the end), the binary tmp-sandbox/datawatch (removed at the end), a
scratch directory outside the repository (removed), and this report. The files already tracked in tmp-sandbox were not
touched. Daemon start and stop used scripts/test-daemon-lib.sh (`td_start`, `td_stop_one`, `td_wait_port_closed`) with no
traps installed in the starting shell and its state directory at tmp-sandbox/svw8/tdstate. Commands ran under
`env -u TMUX -u TMUX_TMPDIR` for the build, the tests and the daemon start/stop (the daemon start changed only `HOME`). The API calls were plain `curl`.

### 1. Build and start

```
[14:18:32] start  go build -o tmp-sandbox/datawatch ./cmd/datawatch/   [14:18:36] done (4s)
[14:18:48] td_start → PID 3151165 (HOME mistake, see Observations 1)        [14:19:14] td_stop_one 3151165 → rc 0
[14:19:14] td_start → PID 3158285                                            [14:19:36] td_stop_one 3158285 → rc 0 (to load the compute node)
[14:19:40] td_start → PID 3164447                                            [14:24:19] td_stop_one 3164447 → rc 0 (absolute data directory)
[14:24:38] td_start → PID 3267028   (the daemon the check ran against)       [14:44:40] td_stop_one 3267028 → rc 0; td_wait_port_closed 18781 → closed
[14:18:53] GET /api/health → hostname dw-svw8-sandbox, rest listener enabled, loopback, token required
```

Fixture server instances (each started and stopped by its recorded PID, all five stopped): 3150937, 3266818, 3317221, 3395566,
3408594 (the last stopped at 14:44:40, port 18782 closed).

### 2. Prior config (recorded for restore)

```
[14:19:23] GET /api/config (506 keys) → autonomous.automation.supervisor_watch:
  {"alert_minutes":30,"main_checkout":"","mode":"shadow","poll_seconds":60,"script_output_path":"",
   "silent_minutes":60,"tmux_restart_baseline":"","working_model":""}
  autonomous.worktree_base_repo "", worktree_dir "", merge_pending_mode "off", merge_pending_alert_minutes 30
[14:20:41] GET /api/llms/ollama → compute_nodes ["datawatch-ollama"]  (re-pointed at sbx-node for planning, put back at restore)
```

(`mode` reads `shadow` with no value configured: an empty mode is shadow.)

### 3. Configure and create

```
[14:19:30] POST /api/compute/nodes {"name":"sbx-node","kind":"ollama","address":<fixture server>} → ok
[14:19:30] PUT /api/config {supervisor_watch.mode shadow, alert_minutes 1, silent_minutes 1,
           script_output_path "tmp-sandbox/svw8/script-record.json", main_checkout "tmp-sandbox/svw8/maincheckout",
           working_model "sbx-absent-model", worktree_base_repo <base repo>, worktree_dir <worktree dir>,
           merge_pending_mode on, merge_pending_alert_minutes 1} → {"status":"ok"}
[14:20:41] PUT /api/llms/ollama {compute_nodes ["sbx-node"], …} → ok       (the planner's node entry pointed at a node the daemon sweeps)
[14:27:12] e8d30001  plan → needs_review; set_verification_llm; ack + approve; run 14:27:2x → task completed, Automaton completed 14:27:3x
[14:30:50] 28070001 (MARK-REVIEW, escalation max_failures 1) run → task review_needed, Automaton blocked ~14:33
[14:32:30] 69ff0001 (MARK-FAILED, escalation off) run → task failed after 2 attempts, Automaton failed ~14:33
[14:30:53] e91a0001 (MARK-SILENT, explicit project directory) run → task in progress/verifying, shell worker `sleep 900`
[14:30:53] 2a6c0001 (no marker, lane_preference gpu) planned and approved, not run
```

(The plan-coverage gate treats the fixture's marker word in a spec as an unmatched requirement; each acknowledgement went
through `plan_coverage/acknowledge` with an actor and a note saying so.)

### 4. Shadow mode: findings stored, `would_alert` set, zero alerts

```
[14:34:46] GET /api/alerts → 24 total, 0 supervisor watch   [14:36:27] same   [14:41:51] same
[14:35:45] GET /api/autonomous/supervisor-watch (pass 14:35:35) → mode shadow, 14 findings (10 open, 4 resolved), would_alert true on all 10 open:
  task_failed 69ff0001, worktree_silent e91a0001, worktree_not_registered e91a0001, worktree_no_gomod e91a0001,
  verify_script_not_executable e91a0001, model_not_resident sbx-node, main_checkout_dirty tmp-sandbox/svw8/maincheckout,
  idle_lane_eligible_work gpu-1, idle_lane_eligible_work gpu-2, automaton_not_merged e8d30001
  resolved: idle_lane_eligible_work claude-1, claude-2, claude-3; worktree_silent 78fb0001
```

### 5. Read the findings back — REST, MCP tool, CLI

```
[14:36:27] REST: GET /api/autonomous/supervisor-watch → mode shadow, last_poll_at 14:35:35, 14 findings
[14:36:38] MCP:  POST /api/mcp/call {"tool":"supervisor_watch_list","params":{}} → mode shadow, 14 findings; the sorted kind/subject/resolved
           list is identical to the REST list (compared from two saved responses)
[14:36:27] CLI:  DATAWATCH_TOKEN=*** ./tmp-sandbox/datawatch autonomous supervisor-watch -u http://127.0.0.1:18781
Supervisor watch mode: shadow
Last poll: 2026-10-30 14:35:35
Findings: 14
task_failed                   would alert  2026-10-30 14:35:35  a task in Automaton 69ff0001 failed: read the verifier verdict before retrying
worktree_silent               would alert  2026-10-30 14:35:35  Automaton e91a0001 went silent while its worker was expected to be active: check it before re-dispatching
worktree_not_registered       would alert  2026-10-30 14:35:35  the worktree of Automaton e91a0001 is not registered
worktree_no_gomod             would alert  2026-10-30 14:35:35  the worktree of Automaton e91a0001 has no go.mod: its project directory is broken
verify_script_not_executable  would alert  2026-10-30 14:35:35  the verify script of Automaton e91a0001 is not executable: run its check manually
model_not_resident            would alert  2026-10-30 14:35:35  the working model is not resident on sbx-node: reload it before routing more Automata
main_checkout_dirty           would alert  2026-10-30 14:35:35  the main checkout tmp-sandbox/svw8/maincheckout has uncommitted tracked files: any Automaton ship will abort …
idle_lane_eligible_work       would alert  2026-10-30 14:35:35  lane gpu-2 is idle while Automata work that is eligible to run is waiting
idle_lane_eligible_work       would alert  2026-10-30 14:35:35  lane gpu-1 is idle while Automata work that is eligible to run is waiting
automaton_not_merged          would alert  2026-10-30 14:35:35  Automaton e8d30001 finished all of its work but is not merged
(4 resolved rows)
Comparison: none recorded yet (days of full agreement: 0)
```

### 6. Triage record — the note-writing path, exercised

For an Automaton-scoped finding the pass writes a first-sighting note onto the owning Automaton's triage record. A running
Automaton has no triage record by design, so the findings for the silent Automaton wrote none. The failed and the
merge-pending Automata have records, and the notes landed on them (read over `GET /api/autonomous/triage`):

```
prd_id 69ff0001, status failed, task_id 6cba0002, task_title "Finish without passing",
  error "verification failed after retries\nsandbox fixture verdict: the task did not pass its check\nIssues:\n- the check script exited 1"
  first_seen_at 14:33:35
  notes: [{"at":"14:33:35","source":"supervisor_watch","text":"a task in Automaton 69ff0001 failed: read the verifier verdict before retrying"}]
prd_id e8d30001, status merge_pending, branch automaton/e8d30001, commits_ahead 3, first_seen_at 14:27:35, alerted_at 14:29:35
  notes: [{"at":"14:27:35","source":"supervisor_watch","text":"Automaton e8d30001 finished all of its work but is not merged"}]
prd_id 28070001, status blocked, blocked_reason "task 2a0b0002 needs operator review"   (no task_id, no watch note — see the task_review_needed row)
```

### 7. Switch to on — one alert per finding

```
[14:42:00] PUT /api/config {"autonomous.automation.supervisor_watch.mode":"on"} → {"status":"ok"}
[14:42:25] 0 supervisor-watch alerts (the 14:41:35 pass was before the switch)
[14:42:50] pass at 14:42:35 → 10 supervisor-watch alerts (34 alerts in all), level warn, 10 distinct titles, all created at 14:42:35;
           the 10 open findings carry alerted_at, the 4 resolved ones carry none
  supervisor watch: automaton_not_merged — e8d30001
  supervisor watch: idle_lane_eligible_work — gpu-1
  supervisor watch: idle_lane_eligible_work — gpu-2
  supervisor watch: main_checkout_dirty — tmp-sandbox/svw8/maincheckout
  supervisor watch: model_not_resident — sbx-node
  supervisor watch: task_failed — 69ff0001
  supervisor watch: verify_script_not_executable — e91a0001
  supervisor watch: worktree_no_gomod — e91a0001
  supervisor watch: worktree_not_registered — e91a0001
  supervisor watch: worktree_silent — e91a0001
  body (automaton_not_merged): "The supervisor watch (mode on) has an open finding:\nAutomaton e8d30001 finished all of its work but is not
                 merged\nopen for 14 minute(s), first seen 2026-10-30 14:27:35."
[14:43:57] after the 14:43:35 pass: still 10 supervisor-watch alerts, 10 distinct titles, one creation time (14:42:35), 10 alerted
```

Ten open findings, ten alerts, no repeat. The sandbox had no messaging channel configured, so nothing left the sandbox. The
separate merge-pending alert for `e8d30001` (from the merge-pending check, not the watch) is among the non-watch alerts.

## Comparison

The script record is the file the watch compares against (`script_output_path`). It was **injected**, because
scripts/supervisor-watch.py needs production hosts and was not run: a helper wrote `{"at": <unix seconds>, "findings":
[{"kind","subject","text"}]}` from the daemon's own open findings read over REST, so the agreeing record matches the
daemon exactly. The staleness window is three times `poll_seconds`, 180 s.

```
[14:36:38] record 1: 10 entries (the 10 open findings at that moment)
[14:38:35] pass → {"agreed": true, "watch_kinds": 10, "script_kinds": 10, "only_watch": [], "only_script": []}   agreed_days 1
           watch_kinds = script_kinds = [automaton_not_merged/e8d30001, idle_lane_eligible_work/gpu-1, idle_lane_eligible_work/gpu-2,
             main_checkout_dirty/tmp-sandbox/svw8/maincheckout, model_not_resident/sbx-node, task_failed/69ff0001,
             verify_script_not_executable/e91a0001, worktree_no_gomod/e91a0001, worktree_not_registered/e91a0001, worktree_silent/e91a0001]
[14:38:52] record 2: the same 10 plus {"kind":"ci_stuck","subject":"injected-disagreement"}
[14:41:35] pass → {"agreed": false, "only_script": ["ci_stuck/injected-disagreement"], "only_watch": []}   agreed_days 0
[14:41:51] MCP: comparison.last.agreed false, only_script ["ci_stuck/injected-disagreement"], only_watch []
[14:41:51] CLI: "Comparison: differs at 2026-10-30 14:41:35 (days of full agreement: 0)" / "only the script raised: ci_stuck/injected-disagreement"
```

- **Injected agreement** (14:38:35): both sides listed the same 10 kind/subject pairs, `agreed: true`.
- **Deliberate disagreement** (14:41:35): the extra entry appears under `only_script`, `agreed: false`, `only_watch` empty. REST, the MCP tool and the CLI showed the same difference.

## Restore, stop, cleanup

```
[14:44:17] POST …/cancel and DELETE …?hard=true for e8d30001, 28070001, e91a0001, 69ff0001, 2a6c0001 → deleted; GET /api/autonomous/prds → 0 left
[14:44:28] 6 sessions (0897, 3120, 37da, 830c, a7a1, e22f) deleted over the MCP bridge; GET /api/sessions → []
[14:44:28] PUT /api/config {supervisor_watch mode shadow, alert_minutes 30, silent_minutes 60, script_output_path "", main_checkout "",
           working_model "", worktree_base_repo "", worktree_dir "", merge_pending_mode off, merge_pending_alert_minutes 30} → ok
[14:44:28] DELETE /api/compute/nodes/sbx-node → ok (GET /api/compute/nodes → []); PUT /api/llms/ollama compute_nodes ["datawatch-ollama"] → ok
           GET /api/config compared key by key with the recorded prior config (506 keys): identical except 2 keys that were null and now read []
[14:44:40] td_stop_one 3267028 → rc 0; td_wait_port_closed 18781 → closed; fixture server 3408594 stopped; ports 18781 and 18782 both closed
[14:47:12] rm -rf tmp-sandbox/svw8 tmp-sandbox/datawatch; scratch directory (base repo, worktrees, project dir, scratch HOME, fixture) removed
```

After the removal `git status --short` listed nothing; the files that were already tracked in tmp-sandbox were untouched. No
process was signalled by name; every stop used a recorded PID.

## Commands run

- `go build -o tmp-sandbox/datawatch ./cmd/datawatch/`
- `source scripts/test-daemon-lib.sh; td_start ./tmp-sandbox/datawatch tmp-sandbox/svw8/dw/config.yaml tmp-sandbox/svw8/dw/daemon.log`
  (four starts), `td_stop_one <pid>` (four stops), `td_wait_port_closed 18781 30`
- `curl` against `http://127.0.0.1:18781`: `GET /api/health`, `GET|PUT /api/config`, `GET|PUT /api/llms/ollama`, `POST|DELETE|GET /api/compute/nodes`,
  `GET /api/sessions`, `POST /api/sessions/kill`, `GET /api/alerts`, `GET /api/autonomous/supervisor-watch`, `GET /api/autonomous/triage`,
  `POST /api/mcp/call` (`supervisor_watch_list`, `send_input`, `delete_session`), `POST /api/autonomous/prds` and its `plan`,
  `plan_coverage/acknowledge`, `set_verification_llm`, `set_failure_escalation`, `approve`, `run`, `cancel` actions,
  `GET|DELETE /api/autonomous/prds/<id>`
- `./tmp-sandbox/datawatch autonomous supervisor-watch -u http://127.0.0.1:18781`
- `git init`, `git add`, `git commit`, `git worktree remove|prune`, `git branch -D` inside the scratch repositories and
  tmp-sandbox/svw8/maincheckout only
- the fixture server and a small record-writer helper (Python standard library, outside the repository); `kill <recorded fixture PID>` (five)
- one `send_input` to the first Automaton's worker session (re-printing its completion marker), before the data-directory fix
- `go test -count=1 -v -run '<the 17 test names in the kind table>' ./internal/autonomous/` ([14:44:52] start, 0 s)

## Verification

- The 17 tests named in the kind table → all PASS in `internal/autonomous` (`go test` at 14:44:52, 0.17 s): `TestSupervisorWatchHostTmuxRestartMismatch`,
  `TestSupervisorWatchHostTmuxRestartFirstSight`, `TestSupervisorWatchTmuxBaselinePersistsAcrossPolls`,
  `TestSupervisorWatchHostDaemonInactive`, `TestShellProbeDaemonActiveReadsNonZeroExitStates`,
  `TestSupervisorWatchHostModelNotResident`, `TestSupervisorWatchWorkTaskFailed`, `TestSupervisorWatchWorkTaskReviewNeeded`,
  `TestSupervisorWatchWorkAutomatonNotMerged`, `TestSupervisorWatchWorkIdleLaneEligibleWork`,
  `TestSupervisorWatchWorktreeNoGomod`, `TestSupervisorWatchWorktreeNotRegistered`, `TestSupervisorWatchWorktreeSilent`,
  `TestSupervisorWatchHostVerifyScriptNotExecutable`, `TestSupervisorWatchHostMainCheckoutDirty`,
  `TestSupervisorWatchWorkCIRed`, `TestSupervisorWatchWorkCISTuck`.
- The project's local verify script checks only that this report exists, carries the three marker lines and is large enough;
  it does not check the live behaviour above.

Result: the shadow-mode behaviour (findings stored, `would_alert` set, zero alerts), the on-mode behaviour (one alert per
open finding per episode, no repeat), the comparison record (injected agreement, deliberate disagreement), the triage-record
note and the three read surfaces check out for the nine kinds raised live. The other five are not claimed live:
`tmux_restart`, `daemon_inactive`, `ci_red` and `ci_stuck` are not producible in a sandbox, and `task_review_needed` was tried
live and cannot hold with the data the daemon writes today. Three things go back to the operator: (1) `task_review_needed`
reads a triage record that never names a task held for review, so the check cannot fire on the normal escalation path
(a blocked-for-review Automaton's record has a blocked reason and no task id); (2) the planning step is the only way a task
gets a verification script path, so the live check needed a fixture node for planning and verdicts; (3) a daemon started
with `TMUX_TMPDIR` unset adopts the dedicated tmux socket when `$HOME` has one, so a sandbox that needs worker sessions has to
change `$HOME` to avoid the production socket, and even then the sessions share tmux's default socket.
