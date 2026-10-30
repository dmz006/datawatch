# Supervisor watch — sandbox live check (shadow, then on)

Sandbox daemon: 2695905, port 18751, config path tmp-sandbox/svw7/dw/config.yaml
Production daemon: not touched (PID 3511620, started Tue Oct 20 18:19:20 2026, same PID and start time before and after the check; all API traffic went to the sandbox daemon on 127.0.0.1:18751 and the fixture server on 127.0.0.1:18752; this check ran no `systemctl`, no `gh` and no tmux command; the only processes it ended were the sandbox daemon and the two fixture-server instances, each by its recorded PID)
Alerts raised in shadow mode: 0 supervisor-watch alerts (four reads of the alert list: 14:00:37, 14:00:40, 14:00:43, 14:05:23; `would_alert` set on the 5 open findings)
Alerts raised in on mode: 5 for 5 open findings, all at one pass (14:06:14), none repeated at the next pass (14:08:14)
Finding kinds raised live: 4 of 14 — `main_checkout_dirty`, `verify_script_not_executable`, `task_failed`, `idle_lane_eligible_work`
Finding kinds not raised live: 10 of 14 — each has a row in the kind table naming the unit test that covers it, or saying it is not producible in a sandbox; none is claimed as live
Triage-record write: exercised live for `task_failed` — the watch wrote its first-sighting note onto the failed Automaton's triage record
Code changed by this check: none (only this report was written)

## What was checked

The daemon-side supervisor watch (`autonomous.automation.supervisor_watch.*`) was run against a sandbox daemon only. The
scope is the narrowed one: the check proves the watch end to end for the finding kinds a sandbox daemon can raise from
state created through its own REST API and CLI, and for every other kind says which unit test covers it or why it cannot
be produced. Shadow mode with a one-minute alert window and a one-minute silence window was set through the sandbox REST
API. The findings were read back over REST, the MCP tool (through the sandbox's `POST /api/mcp/call` bridge) and the CLI.
The check confirmed that findings are stored, that `would_alert` is set, that the alert sink was not called in shadow,
that switching to on produced exactly one alert per open finding with no repeat, that the comparison record shows an
injected agreement and a deliberate disagreement, and that the triage-record note path works.

## How the state was created

All Automaton and config state was created through the sandbox REST API. Nothing was written into the sandbox daemon's
data directory.

- **Config:** `PUT /api/config` for the five `autonomous.automation.supervisor_watch.*` keys used (`mode`, `alert_minutes`,
  `silent_minutes`, `script_output_path`, `main_checkout`), for `autonomous.max_recursion_depth` (set to -1, which the
  executor treats as recursion disabled), and for the planner host settings (`ollama.*`, `autonomous.planning_*`).
- **Automata:** `POST /api/autonomous/prds`, then `…/plan`, `…/plan_coverage/acknowledge`, `…/approve`, `…/run`,
  `…/cancel` and `DELETE …?hard=true`. A draft Automaton has no tasks and cannot be approved or run (`approve` answered
  `status "draft" is not approvable`), and no REST, MCP or CLI call sets a task's verification script path: only the
  planning step writes it. So the planning step was answered by a **fixture planner**: a 25-line local HTTP server on
  127.0.0.1:18752 (a Python standard-library script kept outside the repository, started and stopped by recorded PID)
  that answers `GET /api/tags`, `GET /api/ps` and `POST /api/generate` with a one-story, one-task plan, chosen by a marker
  word in the spec. The daemon's own planning code parsed that reply and stored every story and task. This is a model
  stand-in, not a stand-in for any host command, and it writes nothing into the daemon.
  - spec without a marker → task "Write the file" with `files: ["out.txt"]` and `verification_script: "check.sh"`
  - spec with `MARK-SPAWN` → task "Spawn a child Automaton" with `spawn_prd: true`
- **Fixtures the daemon only reads:** a small git repository with one modified tracked file at
  tmp-sandbox/svw7/maincheckout (the `main_checkout` value); the script-output file at
  tmp-sandbox/svw7/script-record.json (the file the script format would produce); and the Automata's project directories,
  one of which holds `check.sh` with mode 0644. The project directories are in a scratch directory **outside the
  repository** and listed in `server.allowed_project_roots` of the bootstrap config. The main-checkout fixture sits under
  tmp-sandbox because the watch puts the configured path into its finding; it is not an Automaton project directory.
- **Bootstrap config:** tmp-sandbox/svw7/dw/config.yaml was written by hand (a daemon needs one to start): hostname,
  data directory, shell session backend, a loopback listener with a token, no TLS, MCP listener off, memory off.
- **Not done, stated plainly:** no stand-in for `systemctl`, `gh` or any other host command was used; no sandbox daemon
  command wrapper changed `PATH`; no tmux directory was set aside.

### Kind by kind

| kind | live | covered by / reason |
|---|---|---|
| `main_checkout_dirty` | **yes** (first pass 13:56:14) | `main_checkout` set by PUT to tmp-sandbox/svw7/maincheckout, which holds a modified tracked file. Also `TestSupervisorWatchHostMainCheckoutDirty` |
| `verify_script_not_executable` | **yes** (13:59:14) | Automaton `51aa0001`, planned by the fixture planner with task `t1` and verification script `check.sh`, mode 0644 in its project directory; approved and run in guided mode (the task waits at `awaiting_approval`, no worker session). The watch checks running Automata only, so the draft Automaton raised nothing and the finding appeared once it was running. Also `TestSupervisorWatchHostVerifyScriptNotExecutable` |
| `task_failed` | **yes** (13:59:14) | Automaton `cf9b0001`, a `spawn_prd` task run with recursion disabled: the task failed at once with `recursion disabled (autonomous.max_recursion_depth=0); …`, no worker session started, and the daemon's triage pass wrote the Automaton's triage record naming the task. Also `TestSupervisorWatchWorkTaskFailed` |
| `idle_lane_eligible_work` | **yes** (gpu-1 and gpu-2 at 13:59:14; claude-1, claude-2, claude-3 opened at the same pass and resolved by 14:01:14) | Automaton `af510001` with `lane_preference` gpu, approved and left unrun, counted as eligible work while both gpu lanes were idle. Also `TestSupervisorWatchWorkIdleLaneEligibleWork` |
| `tmux_restart` | no | covered by unit tests `TestSupervisorWatchHostTmuxRestartMismatch`, `TestSupervisorWatchHostTmuxRestartFirstSight`, `TestSupervisorWatchTmuxBaselinePersistsAcrossPolls`. Not producible in a sandbox: the finding reads the user service manager's restart count for the dedicated tmux unit, and a sandbox must not restart that unit |
| `daemon_inactive` | no | covered by unit tests `TestSupervisorWatchHostDaemonInactive`, `TestShellProbeDaemonActiveReadsNonZeroExitStates`. Not producible in a sandbox: it needs the daemon's own service unit to be inactive, and this check does not touch the production service |
| `model_not_resident` | no | covered by unit test `TestSupervisorWatchHostModelNotResident`. Not raised: it needs a registered compute node whose resident-model list lacks the working model; the sandbox had no compute node and `working_model` was left unset |
| `task_review_needed` | no | covered by unit test `TestSupervisorWatchWorkTaskReviewNeeded`. Not producible without a worker: a task reaches the review state only after a worker session and verifier verdicts, and this check started no worker session |
| `automaton_not_merged` | no | covered by unit test `TestSupervisorWatchWorkAutomatonNotMerged`. Not producible without a worker: it needs an Automaton whose tasks all completed |
| `worktree_no_gomod` | no | covered by unit test `TestSupervisorWatchWorktreeNoGomod`. The running Automaton `51aa0001` raised neither worktree finding at any pass: the check applies to a registered base repository, and the sandbox config registers none |
| `worktree_not_registered` | no | covered by unit test `TestSupervisorWatchWorktreeNotRegistered`; same reason as the row above |
| `worktree_silent` | no | covered by unit test `TestSupervisorWatchWorktreeSilent`. Not producible without a worker: it needs a task in progress with a worker session gone quiet |
| `ci_red` | no | covered by unit test `TestSupervisorWatchWorkCIRed`. Not producible in a sandbox: it reads the repo CI watch's stored records, which come from `gh` against GitHub, and no fake CI source is configurable |
| `ci_stuck` | no | covered by unit test `TestSupervisorWatchWorkCISTuck`; same reason as `ci_red` |

All 17 tests named in the table were run together at 14:09:29 and passed (see Verification).

## Observations

- **A session started that this check did not intend.** The first Automaton for the spawn case (`ae780001`) was planned
  twice (the first plan's result was not persisted: `decompose output contained no stories`), then run. With the default
  recursion limit its task spawned a child Automaton (`bc330001`), and the child's task started a shell worker session
  (`0365`) in the sandbox daemon. The check cancelled the Automaton, cancelled the child, and the session was `killed`
  within about 25 seconds (13:57:52 start, 13:58:16 killed); both Automata were deleted. The sandbox daemon starts its tmux
  server from `TMUX_TMPDIR` unset, so it falls back to the account's dedicated socket (its boot line says so), which the
  production daemon also uses. This check issued no tmux command, and `kill_sessions_on_exit` is on. The two session
  events from that session are the only two entries in the alert list at the end (title "autonomous:Write the file [0365]").
  For the second spawn Automaton (`cf9b0001`) the limit was set to -1 first, so no child and no session appeared.
- **The daemon boot printed "cleaned up 2 stale MCP registration(s)".** That is the daemon's own startup step; this
  check did not look at which registrations it touched.
- **Some passes were skipped.** Passes ran at 13:56:14, 13:57:14, 13:58:14, 13:59:14, 14:01:14, 14:03:14, 14:05:14,
  14:06:14 and 14:08:14. The passes at 14:00, 14:02, 14:04 and 14:07 did not run: the idle tick lands slightly before the
  one-minute guard (`supervisorWatchMinPollInterval`) and the poll slips a minute. Not changed.
- **Clearing a string key over REST did not restore two planner settings.** `ollama.host` and `ollama.model` stayed at the
  fixture values after `PUT` with empty strings (flat keys and a nested object), both live and in the sandbox's config
  file; every other key read back identical to the prior config. The sandbox directory was deleted, so nothing remains.
  Reported, not changed.
- **A re-sighting needs the alert window.** The three claude-lane findings opened at 13:59:14 and resolved before
  `alert_minutes` (1) elapsed, so they never got `would_alert` and never alerted.

## Procedure and outputs

All writes stayed under tmp-sandbox/svw7 (removed at the end), the binary tmp-sandbox/datawatch (removed at the end), the
scratch directory outside the repository (removed), and this report. The files already tracked in tmp-sandbox were not
touched. Daemon start and stop used scripts/test-daemon-lib.sh (`td_start`, `td_stop_one`, `td_wait_port_closed`) with no
traps installed in the starting shell and its state directory at tmp-sandbox/svw7/tdstate. The Go build and test commands
and the daemon start ran under `env -u TMUX -u TMUX_TMPDIR`; the API calls were plain `curl`.

### 1. Build and start

```
[13:52:58] start  go build -o tmp-sandbox/datawatch ./cmd/datawatch/   [13:53:02] done (4s)
[13:53:10] start  td_start ./tmp-sandbox/datawatch tmp-sandbox/svw7/dw/config.yaml tmp-sandbox/svw7/dw/daemon.log → PID=2695905   [13:53:14] done
[13:53:19] GET /api/health → hostname dw-svw7-sandbox, rest listener enabled, loopback, token required
```

### 2. Prior config (recorded for restore)

```
[13:53:29] GET /api/config → autonomous.automation.supervisor_watch:
  {"alert_minutes":30,"main_checkout":"","mode":"shadow","poll_seconds":60,"script_output_path":"",
   "silent_minutes":60,"tmux_restart_baseline":"","working_model":""}   merge_pending_mode "off"
  ollama {enabled false, host "", model ""}   autonomous.planning_backend ""   max_recursion_depth absent
```

(`mode` reads `shadow` with no value configured: an empty mode is shadow.)

### 3. Configure, build the fixtures, create the Automata

```
[13:53:48] fixture: git repository tmp-sandbox/svw7/maincheckout, one commit, then one tracked file modified (git status: " M f.txt")
[13:53:48] PUT /api/config {supervisor_watch.mode shadow, alert_minutes 1, silent_minutes 1,
           script_output_path "tmp-sandbox/svw7/script-record.json", main_checkout "tmp-sandbox/svw7/maincheckout"} → {"status":"ok"}
[13:53:48] POST /api/autonomous/prds (spec "sandbox live check Automaton A", shell backend) → 51aa0001, draft
[13:53:56] …/51aa0001/add_story → "status draft is locked; only needs_review / revisions_asked / cancelled accept structural edits"
[13:54:26] …/approve → 400 "status draft is not approvable";  …/run → 400 "status draft is not runnable; call /approve first"
[13:54:02] …/plan with no planner configured → accepted, nothing happened (no planning backend)
[13:55:17] fixture planner started on 127.0.0.1:18752 (PID 2743666, recorded); restarted at 13:56:06 (PID 2757700, recorded) after a plan-field fix
[13:55:21] PUT /api/config {ollama.enabled true, ollama.host http://127.0.0.1:18752, ollama.model stub-planner,
           autonomous.planning_backend ollama, autonomous.planning_model stub-planner} → ok
[13:55:29] …/51aa0001/plan → needs_review, task t1 verification_script "check.sh"
[13:55:46] …/approve → refused by the plan-coverage gate (task "Write the file" listed no files); stub fixed; re-plan 13:56:06; …/approve → approved
[13:56:19] …/run → running; task t1 awaiting_approval (guided); GET /api/sessions → []
[13:56:41] af510001 (lane_preference gpu) planned; approved 13:57:25 (left unrun)
[13:57:46] ae780001 (spawn marker) acknowledged + approved + run → child bc330001 → session 0365 (see Observations); cancelled 13:58:12
[13:58:43] DELETE bc330001, ae780001 (hard); PUT autonomous.max_recursion_depth -1 → ok
[13:58:33] cf9b0001 (spawn marker) planned: task t1 spawn_prd true; [13:58:51] acknowledge + approve + run
[13:58:54] cf9b0001 → failed; t1 failed "recursion disabled (autonomous.max_recursion_depth=0); set spawn_prd=false or raise the limit"; GET /api/sessions → [0365 killed]
```

(The plan-coverage gate treats the fixture's marker word in a spec as a requirement; the acknowledgement route needs an
actor and a note, and the note says so.)

### 4. Shadow mode: findings stored, `would_alert` set, zero alerts

```
[13:59:49] GET /api/autonomous/supervisor-watch (pass 13:59:14) → mode shadow, 8 findings:
  idle_lane_eligible_work gpu-1, gpu-2 (would_alert true)    verify_script_not_executable 51aa0001 (true)
  main_checkout_dirty tmp-sandbox/svw7/maincheckout (true)    task_failed cf9b0001 (set at the next pass)
  idle_lane_eligible_work claude-1, claude-2, claude-3 (opened; resolved by the 14:01:14 pass)
[14:00:37] GET /api/alerts → 2 total, 0 supervisor watch     [14:00:40] same     [14:00:43] same
[14:01:20] GET /api/autonomous/supervisor-watch → pass 14:01:14: would_alert true on the 5 open findings
[14:05:23] GET /api/alerts → 2 total (both are the session events of 0365), 0 supervisor watch
```

### 5. Read the findings back — REST, MCP tool, CLI

```
[14:05:14] pass → REST GET /api/autonomous/supervisor-watch: mode shadow, last_poll_at 14:05:14, 8 findings (5 open, 3 resolved),
           comparison.last.agreed false, only_script ["ci_stuck/injected-disagreement"], only_watch []
[14:05:22] MCP:  POST /api/mcp/call {"tool":"supervisor_watch_list","params":{}} → mode shadow, 8 findings;
           the sorted kind/subject/resolved list is identical to the REST list; only_script the same
[14:05:22] CLI:  DATAWATCH_TOKEN=*** ./tmp-sandbox/datawatch autonomous supervisor-watch -u http://127.0.0.1:18751
Supervisor watch mode: shadow
Last poll: 2026-10-30 14:05:14
Findings: 8
verify_script_not_executable  would alert  2026-10-30 14:05:14  the verify script of Automaton 51aa0001 is not executable: run its check manually
task_failed                   would alert  2026-10-30 14:05:14  a task in Automaton cf9b0001 failed: read the verifier verdict before retrying
main_checkout_dirty           would alert  2026-10-30 14:05:14  the main checkout tmp-sandbox/svw7/maincheckout has uncommitted tracked files: any Automaton ship will abort until they are committed
idle_lane_eligible_work       would alert  2026-10-30 14:05:14  lane gpu-2 is idle while Automata work that is eligible to run is waiting
idle_lane_eligible_work       would alert  2026-10-30 14:05:14  lane gpu-1 is idle while Automata work that is eligible to run is waiting
idle_lane_eligible_work       resolved     2026-10-30 14:05:14  lane claude-2 is idle while Automata work that is eligible to run is waiting
idle_lane_eligible_work       resolved     2026-10-30 14:05:14  lane claude-1 is idle while Automata work that is eligible to run is waiting
idle_lane_eligible_work       resolved     2026-10-30 14:05:14  lane claude-3 is idle while Automata work that is eligible to run is waiting
Comparison: differs at 2026-10-30 14:05:14 (days of full agreement: 0)
  only the script raised: ci_stuck/injected-disagreement
```

### 6. Triage record — the note-writing path, exercised

For an Automaton-scoped finding the pass writes a first-sighting note onto the owning Automaton's triage record. A running
Automaton has no triage record by design, so the finding for `51aa0001` wrote none; `cf9b0001` had failed and the
daemon's triage pass had built its record, so the note landed on it (read at 14:00:29 over `GET /api/autonomous/triage`):

```
prd_id cf9b0001, status failed, task_id t1, task_title "Spawn a child Automaton",
error "recursion disabled (autonomous.max_recursion_depth=0); set spawn_prd=false or raise the limit"
first_seen_at 13:59:14
notes: [{"at":"13:59:14","source":"supervisor_watch","text":"a task in Automaton cf9b0001 failed: read the verifier verdict before retrying"}]
```

### 7. Switch to on — one alert per finding

```
[14:05:31] PUT /api/config {"autonomous.automation.supervisor_watch.mode":"on"} → {"status":"ok"}
[14:06:16] pass at 14:06:14 → 5 supervisor-watch alerts (7 alerts in all with the 2 session events), level warn, 5 distinct titles,
           all created at 14:06:14; the 5 open findings carry alerted_at 14:06:14; the 3 resolved ones carry none
  supervisor watch: idle_lane_eligible_work — gpu-1
  supervisor watch: idle_lane_eligible_work — gpu-2
  supervisor watch: main_checkout_dirty — tmp-sandbox/svw7/maincheckout
  supervisor watch: task_failed — cf9b0001
  supervisor watch: verify_script_not_executable — 51aa0001
  body (gpu-1): "The supervisor watch (mode on) has an open finding:\nlane gpu-1 is idle while Automata work that is eligible to
                 run is waiting\nopen for 7 minute(s), first seen 2026-10-30 13:58:14."
[14:08:14] after the 14:08:14 pass: still 5 supervisor-watch alerts, 5 distinct titles, alerted_at unchanged (14:06:14);
           the CLI reads "alerted" for the 5 open findings
```

Five open findings, five alerts, no repeat. The sandbox had no messaging channel configured, so nothing left the sandbox.

## Comparison

The script record is the file the watch compares against (`script_output_path`). It was **injected**, because
scripts/supervisor-watch.py needs production hosts and was not run: a helper wrote `{"at": <unix seconds>, "findings":
[{"kind","subject","text"}]}` from the daemon's own open findings read over REST, so the agreeing record matches the
daemon exactly. The staleness window is three times `poll_seconds`, 180 s.

```
[14:01:27] record 1: 5 entries (the 5 open findings at that moment)
[14:02:28] GET → comparison.last null (the 14:02:14 pass was skipped; no comparison recorded without a pass)
[14:03:14] pass → {"agreed": true, "watch_kinds": 5, "script_kinds": 5, "only_watch": [], "only_script": []}   agreed_days 1
           watch_kinds = script_kinds = [idle_lane_eligible_work/gpu-1, idle_lane_eligible_work/gpu-2,
             main_checkout_dirty/tmp-sandbox/svw7/maincheckout, task_failed/cf9b0001, verify_script_not_executable/51aa0001]
[14:03:21] record 2: the same 5 plus {"kind":"ci_stuck","subject":"injected-disagreement"}
[14:05:14] pass → {"agreed": false, "only_script": ["ci_stuck/injected-disagreement"], "only_watch": []}   agreed_days 0
```

- **Injected agreement** (14:03:14): both sides listed the same 5 kind/subject pairs, `agreed: true`.
- **Deliberate disagreement** (14:05:14): the extra entry appears under `only_script`, `agreed: false`, `only_watch` empty. REST, the MCP tool and the CLI showed the same difference (section 5).

## Restore, stop, cleanup

```
[14:08:41] POST …/cancel and DELETE …?hard=true for 51aa0001, cf9b0001, af510001 → deleted; GET /api/autonomous/prds → []
[14:08:41] PUT /api/config {supervisor_watch mode shadow, alert_minutes 30, silent_minutes 60, script_output_path "", main_checkout "",
           ollama.enabled false, planning_backend "", planning_model "", max_recursion_depth 0} → ok
           GET /api/config compared with the recorded prior config: identical except ollama.host (now the fixture URL) and
           ollama.model (now stub-planner), which an empty-string PUT did not clear (see Observations)
[14:08:52] td_stop_one 2695905 → rc 0; td_wait_port_closed 18751 → closed; fixture server PID 2757700 (command line checked) stopped;
           ports 18751 and 18752 both closed; production daemon 3511620 still running with the same start time
[14:09:02] rm -rf tmp-sandbox/svw7; rm -f tmp-sandbox/datawatch; scratch fixture directory removed
```

After the removal `git status --short` listed nothing for the sandbox; the files that were already tracked in tmp-sandbox
were untouched. The fixture server's first instance (PID 2743666, recorded when started) was stopped with a signal to
that PID at 13:56:06 to apply a one-line fix to its plan field. No process was signalled by name.

## Commands run

- `go build -o tmp-sandbox/datawatch ./cmd/datawatch/`
- `source scripts/test-daemon-lib.sh; td_start ./tmp-sandbox/datawatch tmp-sandbox/svw7/dw/config.yaml tmp-sandbox/svw7/dw/daemon.log`,
  `td_stop_one 2695905`, `td_wait_port_closed 18751 30`
- `curl` against `http://127.0.0.1:18751`: `GET /api/health`, `GET|PUT /api/config`, `GET /api/sessions`, `GET /api/alerts`,
  `GET /api/autonomous/lane-fill`, `GET /api/autonomous/supervisor-watch`, `GET /api/autonomous/triage`,
  `POST /api/mcp/call` (`supervisor_watch_list`), `POST /api/sessions/kill` (session `0365`), `POST /api/autonomous/prds`
  and its `plan`, `plan_coverage/acknowledge`, `approve`, `run`, `set_guided_mode`, `add_story` (refused), `cancel` actions,
  `GET|DELETE /api/autonomous/prds/<id>`
- `./tmp-sandbox/datawatch autonomous supervisor-watch -u http://127.0.0.1:18751`
- `git init`, `git add`, `git commit`, a one-line file edit inside tmp-sandbox/svw7/maincheckout only
- the fixture planner and a small record-writer helper (Python standard library, outside the repository)
- `kill 2743666`, `kill 2757700` (recorded PIDs); `rm -rf tmp-sandbox/svw7`, `rm -f tmp-sandbox/datawatch`
- read-only `ps` listings (the sandbox PIDs, the production PID, and a count of tmux processes: two, both with a parent
  that is not the sandbox daemon)
- `go test -count=1 -v -run '<the 17 test names in the kind table>' ./internal/autonomous/` ([14:09:29] start, [14:09:30] done)

## Verification

- The 17 tests named in the kind table → all PASS in `internal/autonomous` (`go test` at 14:09:29, 0.18 s): `TestSupervisorWatchHostTmuxRestartMismatch`,
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
note and the three read surfaces check out for the four kinds raised live. The other ten kinds are not claimed live: four
are not producible in a sandbox (the tmux restart, the daemon not active, red CI, stuck CI), three need a worker session
this check did not start (task in review, Automaton not merged, silent worktree), two need a registered base repository the
sandbox did not have (worktree without `go.mod`, worktree not registered), and one needs a registered compute node (model
not resident); each is tied to its passing unit test above. Two things go back to the operator: the planning step
is the only way a task gets a verification script path, so the live check needed a fixture planner, and an empty-string
`PUT` did not clear two planner settings.
