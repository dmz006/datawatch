# Supervisor watch — sandbox live check (shadow, then on)

Sandbox daemon: 2263494, port 18741, config path tmp-sandbox/svw6/dw/config.yaml
Production daemon: not touched (PID 3511620, started Tue Oct 20 18:19:20 2026, same PID and start time at the start and at the end of the check; all API traffic went to 127.0.0.1:18741 and the fixture model server on 127.0.0.1:18742; no `systemctl`, no `gh` and no tmux command was run by this check against the host, and every process this check stopped was stopped by an exact PID)
Sandbox daemon PIDs in this run, in order: 1748007 (first start), 1753024 (restart after the nodes were registered), 2073080 (restart onto a rebuilt binary), 2263494 (restart onto the final binary; the one named above)
Alerts raised in shadow mode: 0 supervisor-watch alerts (three reads of the alert list at 13:09:50, 13:09:53, 13:09:56; `would_alert` set on all 19 findings by 13:11:00)
Alerts raised in on mode: 17 for 17 open findings, all at one pass (13:16:00), none repeated at the next pass; 22 alerts for 21 distinct findings by the end of the run (details in section 7)
Finding kinds raised live: 13 of 14 — `tmux_restart`, `daemon_inactive`, `model_not_resident`, `task_failed`, `automaton_not_merged`, `idle_lane_eligible_work`, `worktree_no_gomod`, `worktree_not_registered`, `worktree_silent`, `verify_script_not_executable`, `main_checkout_dirty`, `ci_red`, `ci_stuck`
Kind NOT covered: 1 of 14 — `task_review_needed` (reason, with the live evidence, in the kind table)
Triage-record write: exercised live — the watch wrote its first-sighting note onto three Automata's triage records (section 6)
Code defects found by this check and fixed in the same change: four (section 9)

## What was checked

A live pass of the daemon-side supervisor watch (`autonomous.automation.supervisor_watch.*`) against a sandbox daemon
only. Shadow mode, a one-minute alert window and a one-minute silence window were set through the sandbox REST API.
Automata were created and driven through the sandbox REST API. The watch ran about thirty passes. The findings were read
back over REST, the MCP tool and the CLI. The check confirmed that findings are stored, that `would_alert` is set, that
the alert sink was called zero times in shadow, that switching to on produced one alert per finding with no repeat, that
the comparison record shows an injected agreement and a deliberate disagreement, that the triage-record note path
works, and that clearing a key over REST reaches the live watch without a restart. The Automata, nodes and config were
restored through the API, the sandbox daemon and the fixture model server were stopped by recorded PID, and every file
the check created was deleted.

## How the state was created

Created through the sandbox REST API (and, for the two worker/verifier steps, the session-input tool of the MCP route
`POST /api/mcp/call`, which is also an API call on the sandbox daemon):

- **Nodes:** `POST /api/compute/nodes` — `dw-svw6-sandbox` (the sandbox hostname) and `datawatch-ollama` (the node the
  planner path routes through), both pointing at the fixture model server.
- **Config:** `PUT /api/config` for every `autonomous.automation.supervisor_watch.*` key, for
  `autonomous.automation.repo_ci_watch.*`, `autonomous.automation.merge_pending_*` and `autonomous.max_recursion_depth`.
- **Automata:** `POST /api/autonomous/prds`, then `…/plan`, `…/approve`, `…/run`, `…/approve_task`,
  `…/set_lane_preference`, `…/cancel` and `DELETE …?hard=true`. This is the Automaton REST surface.
- **A task's verification script path can only come from the planning step** (no REST, MCP or CLI call sets it), so the
  planner's model host (`ollama.host`) pointed at a small fixture HTTP server that answers the model API shapes
  (`GET /api/tags`, `GET /api/ps`, `POST /api/generate`) and returns a one-story, one-task plan chosen by a marker word in
  the spec. The daemon's own planning code produced every stored task from those replies.
- **Worker and verifier steps:** the shell backend's worker and verifier sessions were answered through the
  session-input API (a worker completion line, then a verifier verdict file command), so the daemon's own
  executor, verification and triage code ran end to end. Nothing was written into the daemon's data directory.

Not done through the API, stated plainly:

- The sandbox bootstrap config file (tmp-sandbox/svw6/dw/config.yaml) was written by hand — a daemon needs one to start.
  It holds the listener port and token, data directory, hostname, session defaults, the planner model host, and the
  worktree base and worktree directory. All watch, CI watch and merge-pending values went through the API.
- **Fixture directories.** The Automata's project directories, the worktree base repository (with a `go.mod`), the
  worktree directory and the CI fixture repository sit in a session scratch directory **outside the repository**, listed in
  `server.allowed_project_roots` of the bootstrap config; the instruction that the sandbox project directory must not be
  inside the repository is followed. Only the "main checkout" fixture (a small repository with one modified tracked
  file, tmp-sandbox/svw6/maincheckout), the daemon data, the script-output file and the helper scripts are under
  tmp-sandbox/svw6, because the watch reports the main checkout's path in a finding.
- **Two command stand-ins on PATH for the sandbox daemon only** (tmp-sandbox/svw6/shim): a `systemctl` that answers
  the three read queries the watch makes (`show -p NRestarts datawatch-tmux`, `show -p ActiveEnterTimestamp
  datawatch-tmux`, `is-active datawatch`) from small state files, and a `gh` that answers `run list` from a state file
  holding two runs (one in progress for three hours, one failed). They exist so that `tmux_restart`, `daemon_inactive`,
  `ci_red` and `ci_stuck` can be produced without touching the real user service manager or GitHub. The queries the
  daemon made are the production ones; only the answers are fixtures.
- **A private tmux directory for the sandbox daemon (a deliberate deviation from "unset TMUX_TMPDIR").** With
  `TMUX_TMPDIR` unset the daemon falls back to the account's dedicated tmux socket, which the production daemon also
  uses (the daemon prints that fallback at boot). Worker sessions are needed for `automaton_not_merged` and
  `worktree_silent`, so the sandbox daemon alone was started with `TMUX_TMPDIR` pointing at tmp-sandbox/svw6/tmux. The Go build, vet and test commands ran with `TMUX` and `TMUX_TMPDIR` unset, and the daemon start with `TMUX` unset; the API
  calls were plain `curl`. This check ran no tmux command at all; the daemon started
  its own tmux server on the private socket, and that server was gone after the daemon stopped (no tmux process of
  the sandbox remained; the production keepalive session was untouched).
- **A leftover from an earlier attempt at this check** was found holding ports 18741 and 18742 when this run started: a
  sandbox daemon (PID 1262247, started 12:23:58, command line naming tmp-sandbox/svw5/dw/config.yaml, working
  directory this worktree) and its fixture model server (PID 1275295). Both were verified by command line as sandbox
  processes of this worktree and stopped at 12:53:36 with a signal to those exact PIDs; their directory tmp-sandbox/svw5
  was deleted at the end. They were not started by this run, so they were not "recorded" PIDs.

### Kind by kind

| kind | raised live | how it was produced, or why it was not |
|---|---|---|
| `main_checkout_dirty` | yes (12:57:00) | `main_checkout` set by PUT to tmp-sandbox/svw6/maincheckout, which holds a modified tracked file |
| `model_not_resident` | yes (12:57:00, both nodes) | `working_model` set by PUT to a model the fixture server's `/api/ps` does not list |
| `tmux_restart` | yes, twice: (1) 12:57:00 with a deliberately wrong configured baseline (`7`); (2) 13:31:58 as a genuine restart-count change | (2): no baseline configured; the first pass after boot stored the baseline (13:29:58), the stand-in `NRestarts` was raised from 9 to 10 at 13:29:59, the pass at 13:31:58 opened the finding. Before the fix in section 9 this second case never fired (NRestarts 7→8 at 13:22:21 and 8→9 at 13:25:19 raised nothing) |
| `daemon_inactive` | yes (13:18:49) | the `systemctl` stand-in answered `inactive` with exit status 3, as the real command does; the finding opened for node `dw-svw6-sandbox` and resolved at 13:24:49 once the stand-in said `active` again. Before the fix in section 9 an inactive unit read as "unknown, no finding" and this kind could never fire |
| `verify_script_not_executable` | yes (12:59:00) | a draft Automaton `a7720001` was created first (no tasks, no finding at the 12:57:00 pass), then planned (`needs_review`, task `1ac20002` with verification script `scripts/check.sh`, mode 0644 in its project directory), then approved and run in guided mode; the finding appeared once it was running. The watch checks running Automata only, so a draft or a planned-but-not-running Automaton raises nothing, and the finding needs a running one |
| `worktree_no_gomod`, `worktree_not_registered` | yes (12:59:00) | the same running Automaton: its project directory has no `go.mod` and is not in the worktree base repository's worktree list |
| `task_failed` | yes (12:59:00; two Automata) | a task of type "spawn a child Automaton" with `autonomous.max_recursion_depth` set to 1 through REST: the child hit the depth limit and failed, then the parent task failed. No worker session started. The Automaton's triage record names the failed task |
| `idle_lane_eligible_work` | yes (13:05:00: gpu-1, gpu-2; 13:20:49: claude-1, claude-2, claude-3) | an Automaton given `lane_preference` gpu by `…/set_lane_preference` and left approved counted as one eligible gpu item while both gpu lanes were idle; the claude lanes followed after the restart |
| `automaton_not_merged` | yes (13:06:00) | Automaton `d0bf0001` (worktree mode: branch `automaton/d0bf0001`, project directory created by the daemon under the worktree directory), shell worker answered through the session-input API, verifier verdict `ok: true`; it completed at 13:05:55 and the daemon's own merge-pending pass wrote its triage record `merge_pending` at 13:06:00 |
| `worktree_silent` | yes (13:08:00) | Automaton `9f110001`: a shell worker session left idle in a task that was in progress/verifying for longer than `silent_minutes` (1) |
| `ci_red`, `ci_stuck` | yes (13:08:00) | `repo_ci_watch` set by PUT to shadow for the fixture repository; the `gh` stand-in returned one failed run (900000002) and one run in progress for three hours (900000001); the CI watch recorded them as `failing` and `hung` at 13:08:00 |
| `task_review_needed` | **no** | Produced the situation the kind describes and it is not raised: Automaton `d05f0001` (shell worker answered "done", verifier verdict `ok: false` three times) ended with its task `d2180002` in `review_needed` and the Automaton `blocked` at 13:04:10. Its triage record (read at 13:09:33 and 13:05:17) carries `blocked_reason: task d2180002 needs operator review` and **no `task_id`**, so the check, which needs a record that names a task now sitting in review, found nothing. From the code: a triage record names a task only through `triageFailureTask`, which looks at failed and blocked tasks, never at review ones, and the triage pass rewrites the record on every coordinator tick; so the record cannot name a task that has moved to review. Not produced, and likely not producible by a real flow without a change to the check or the record (reported, not changed) |

## Observations about the watch

- **Fixed in this change, with live evidence** — section 9: `daemon_inactive` unreachable with the real command's exit
  status; `tmux_restart` unreachable without a configured baseline; clearing a string key over REST not reaching the
  live watch; `poll_seconds: 0` refused by REST but treated as unset everywhere else.
- **A pass is skipped when the idle tick arrives a few milliseconds early.** The guard
  (`supervisorWatchMinPollInterval`, one minute) equals the tick, so a tick that lands at 59.9 s after the previous pass
  is skipped and the poll slips a whole minute. Seen at 13:15:00 (the first pass in on mode did not alert, the next one
  at 13:16:00 did), 13:19:49, 13:22:49 and 13:25:49. The script-record staleness window (three polls) tolerates it.
  Not changed: it has the same shape as the repo CI watch guard.
- **Node addresses and the worktree base repository are read once at boot.** Nodes registered after start are not seen
  by `model_not_resident` or `daemon_inactive` until a restart; that is why the sandbox daemon was restarted once after
  the nodes were registered.
- **`ci_red` and `ci_stuck` outlive the repo CI watch.** With `repo_ci_watch` back to off the stored records still held
  the failing and hung runs, so both stayed open after cleanup (13:34:58). They read stored records by design.
- **A restart ends the worker sessions** (`session.kill_sessions_on_exit` is on in the sandbox), so an open
  `worktree_silent` closes and, when the Automaton resumes and goes silent again, opens as a new episode that alerts again.
- **Resolved findings stay in the read-back list** with a `resolved` mark (the CLI prints them as `resolved`); a script
  record built from the whole list therefore disagrees with the daemon until it is built from the open findings only.
  The first agreement attempt at 13:10 hit exactly that (see Comparison).

## Procedure and outputs

All writes stayed under tmp-sandbox/svw6 (removed at the end), the binary tmp-sandbox/datawatch (removed at the end),
tmp-sandbox/svw5 (the earlier attempt's leftover, removed), the session scratch directory outside the repository, and
the files named in the change list. The files that were already tracked in tmp-sandbox/ were not touched. Daemon start
and stop used scripts/test-daemon-lib.sh (`td_start`, `td_stop_one`, `td_wait_port_closed`) with no traps installed in
the starting shell and its state directory under tmp-sandbox/svw6/tdstate. Each command printed a start and a done
stamp, quoted below.

### 1. Build and start

```
[12:51:41] start  go build -o tmp-sandbox/datawatch ./cmd/datawatch/   [12:51:49] done
[12:55:33] start  fixture model server on 127.0.0.1:18742 (PID 1747760, recorded); td_start ./tmp-sandbox/datawatch tmp-sandbox/svw6/dw/config.yaml tmp-sandbox/svw6/dw/daemon.log  → pid 1748007   [12:55:38] done
GET /api/health → hostname dw-svw6-sandbox, rest listener enabled, loopback, token required
```

### 2. Prior config (recorded for restore)

```
[12:55:48] GET /api/config → autonomous.automation.supervisor_watch:
  {"alert_minutes":30,"main_checkout":"","mode":"shadow","poll_seconds":60,"script_output_path":"",
   "silent_minutes":60,"tmux_restart_baseline":"","working_model":""}
 autonomous.automation.repo_ci_watch: {"alert_minutes":30,"delete_superseded":"report","hung_after_minutes":60,"mode":"off",
   "poll_seconds":300,"repos":[],"security_workflows":[...5 names...],"tag_count":5}
 merge_pending_mode "off", merge_pending_alert_minutes 30, max_recursion_depth absent
```

(`mode` reads `shadow` with no value configured: an empty mode is shadow.)

### 3. Configure through the API, then restart once

```
[12:55:48] POST /api/compute/nodes {"name":"dw-svw6-sandbox","kind":"ollama","address":"http://127.0.0.1:18742"} → ok   (and datawatch-ollama)
[12:55:57] PUT /api/config {
  "autonomous.automation.supervisor_watch.mode": "shadow",
  "autonomous.automation.supervisor_watch.alert_minutes": 1,
  "autonomous.automation.supervisor_watch.silent_minutes": 1,
  "autonomous.automation.supervisor_watch.script_output_path": "tmp-sandbox/svw6/script-record.json",
  "autonomous.automation.supervisor_watch.main_checkout": "tmp-sandbox/svw6/maincheckout",
  "autonomous.automation.supervisor_watch.working_model": "qwen-stub-working",
  "autonomous.automation.supervisor_watch.tmux_restart_baseline": "7",
  "autonomous.automation.repo_ci_watch.mode": "shadow", ".repos": [<fixture repository, absolute path required>],
  ".tag_count": 1, ".hung_after_minutes": 60,
  "autonomous.automation.merge_pending_mode": "on", "autonomous.automation.merge_pending_alert_minutes": 1} → {"status":"ok"}
  (a first PUT with a repository-relative CI repo path was refused: "must be an absolute path")
[12:55:57] td_stop_one 1748007 → rc 0, port 18741 closed; td_start again → pid 1753024   [12:56:00] done
```

### 4. The Automata and the polls in shadow mode

```
[12:56:21] POST /api/autonomous/prds (draft, spec marker PLAN-RUN, shell backend, guided_mode true) → a7720001, status draft, 0 stories
[12:57:23] GET /api/autonomous/supervisor-watch (pass 12:57:00, draft only) → tmux_restart, model_not_resident ×2, main_checkout_dirty
           — no verify_script_not_executable, no worktree finding for the draft
[12:57:23] …/a7720001/plan → needs_review; task 1ac20002, verification_script scripts/check.sh
[12:57:35] …/approve, …/run → running (guided: the task waits for approval, no session)
[12:57:35] PUT autonomous.max_recursion_depth 1; create f8b30001 (marker PLAN-SPAWN, spawn-a-child task), plan, approve, run
[12:57:49] f8b30001 failed, child ebc40001 failed (recursion depth 2 would exceed max_recursion_depth=1); GET /api/sessions → []
[12:58:22] create d05f0001 (marker PLAN-REVIEW, shell worker, guided); plan, approve, run; approve_task at 12:58:34;
           the task started only after a second …/run at 13:01:48 (the first run had returned with the task awaiting approval)
[13:02:07] driver answers: worker complete → verifier verdict false, three attempts → 13:04:10 task review_needed, Automaton blocked
[13:02:51] create 81560001 (marker PLAN-RUN, project directory with an executable script), plan, set_lane_preference gpu, approve (13:03:00)
           → lane fill queued_eligible {"gpu":1,"claude":0}, gpu-1 and gpu-2 idle
[13:04:53] create d0bf0001 (marker PLAN-MERGE, no project directory → worktree mode, branch automaton/d0bf0001); run, approve_task,
           run, driver: worker complete, verdict true → completed 13:05:55; merge_pending triage record 13:06:00
[13:06:22] create 9f110001 (marker PLAN-SILENT, shell worker, guided); worker session idle from 13:06:41
[13:07:24] CI fixture repository given a commit and a remote-tracking ref; CI watch poll 13:08:00 → 900000002 failing, 900000001 hung
```

The findings after the 13:08:00 pass (read at 13:08:29, 19 stored, 17 open; `would_alert` shown from the 13:10:00 pass):

```
automaton_not_merged          d0bf0001   Automaton d0bf0001 finished all of its work but is not merged
ci_red                        900000002  CI run 900000002 is red: triage the failure before shipping more Automata
ci_stuck                      900000001  CI run 900000001 has been in progress abnormally long: the Automata release pipeline is stalled …
idle_lane_eligible_work       gpu-1      lane gpu-1 is idle while Automata work that is eligible to run is waiting
idle_lane_eligible_work       gpu-2      lane gpu-2 is idle while Automata work that is eligible to run is waiting
main_checkout_dirty           tmp-sandbox/svw6/maincheckout  the main checkout tmp-sandbox/svw6/maincheckout has uncommitted tracked files: any Automaton ship will abort …
model_not_resident            datawatch-ollama   the working model is not resident on datawatch-ollama: reload it before routing more Automata
model_not_resident            dw-svw6-sandbox    the working model is not resident on dw-svw6-sandbox: reload it before routing more Automata
task_failed                   ebc40001, f8b30001  a task in Automaton … failed: read the verifier verdict before retrying
tmux_restart                  datawatch-tmux     tmux restarted on datawatch-tmux: audit every non-terminal Automaton there
verify_script_not_executable  a7720001   the verify script of Automaton a7720001 is not executable: run its check manually
worktree_no_gomod             9f110001, a7720001   the worktree of Automaton … has no go.mod: its project directory is broken
worktree_not_registered       9f110001, a7720001   the worktree of Automaton … is not registered
worktree_silent               9f110001   Automaton 9f110001 went silent while its worker was expected to be active: check it before re-dispatching
```

plus `worktree_no_gomod` / `worktree_not_registered` for d05f0001, which closed once that Automaton was blocked (resolved, still listed).
The 17 open findings are the lines above counted per subject.

Alerts in shadow:

```
[13:09:50] GET /api/alerts → 19 alerts in total (session events, one merge-pending alert), 0 whose title starts "supervisor watch"
[13:09:53] same     [13:09:56] same
[13:11:00] findings: 19 stored, 19 would_alert true, 0 alerted_at
```

### 5. Read the findings back — REST, MCP tool, CLI (all three agree)

```
[13:13:42] REST: GET /api/autonomous/supervisor-watch
           → mode shadow, last_poll_at 13:14:00, 19 findings (17 open, 2 resolved), comparison.last.agreed false,
             only_script ["ci_stuck/injected-disagreement"], only_watch []
[13:13:42] MCP:  POST /api/mcp/call {"tool":"supervisor_watch_list","params":{}}
           → same body; the sorted kind/subject list is identical to REST (19 entries); only_script the same
[13:13:42] CLI:  DATAWATCH_TOKEN=*** ./tmp-sandbox/datawatch autonomous supervisor-watch -u http://127.0.0.1:18741
Supervisor watch mode: shadow
Last poll: 2026-10-30 13:14:00
Findings: 19
worktree_silent               would alert  2026-10-30 13:14:00  Automaton 9f110001 went silent while its worker was expected to be active: check it before re-dispatching
worktree_not_registered       would alert  2026-10-30 13:14:00  the worktree of Automaton a7720001 is not registered
  … (14 more `would alert` lines, one per open finding, same wording as above)
worktree_not_registered       resolved     2026-10-30 13:14:00  the worktree of Automaton d05f0001 is not registered
worktree_no_gomod             resolved     2026-10-30 13:14:00  the worktree of Automaton d05f0001 has no go.mod: its project directory is broken
Comparison: differs at 2026-10-30 13:14:00 (days of full agreement: 0)
  only the script raised: ci_stuck/injected-disagreement
```

### 6. Triage record — the note-writing path, exercised

The pass writes a first-sighting note onto the triage record of the owning Automaton for an Automaton-scoped finding; a
running Automaton has no record by design, so the three findings raised for the running Automaton wrote none. For the
Automata that the daemon's own triage and merge-pending passes had put in a failed, blocked or merge-pending state the
notes were written (read at 13:09:33 over `GET /api/autonomous/triage`):

```
d0bf0001  merge_pending  note supervisor_watch 13:06:00  Automaton d0bf0001 finished all of its work but is not merged
ebc40001  failed (task ecc10002, "recursion depth 2 would exceed autonomous.max_recursion_depth=1")
                         note supervisor_watch 12:59:00  a task in Automaton ebc40001 failed: read the verifier verdict before retrying
f8b30001  failed (task fa420002, "child PRD ebc40001 ended with status failed")
                         note supervisor_watch 12:59:00  a task in Automaton f8b30001 failed: read the verifier verdict before retrying
d05f0001  blocked ("task d2180002 needs operator review"), no task id, no note (no Automaton-scoped finding was first seen after it was blocked)
```

(The word `PRD` in the quoted child-Automaton error is an existing daemon message, quoted as it came, not a string this change adds.)

### 7. Switch to on — one alert per finding

```
[13:14:28] PUT /api/config {"autonomous.automation.supervisor_watch.mode":"on"} → {"status":"ok"}
[13:15:18] pass at 13:15:00 was skipped by the poll guard (see Observations); alerts with a supervisor-watch title: 0
[13:16:00] pass → 17 alerts, level warn, 17 distinct titles, all created at 13:16:00; 17 of 17 open findings carry alerted_at
  supervisor watch: automaton_not_merged — d0bf0001            supervisor watch: tmux_restart — datawatch-tmux
  supervisor watch: ci_red — 900000002                         supervisor watch: verify_script_not_executable — a7720001
  supervisor watch: ci_stuck — 900000001                       supervisor watch: worktree_no_gomod — 9f110001
  supervisor watch: idle_lane_eligible_work — gpu-1            supervisor watch: worktree_no_gomod — a7720001
  supervisor watch: idle_lane_eligible_work — gpu-2            supervisor watch: worktree_not_registered — 9f110001
  supervisor watch: main_checkout_dirty — tmp-sandbox/svw6/maincheckout   supervisor watch: worktree_not_registered — a7720001
  supervisor watch: model_not_resident — datawatch-ollama      supervisor watch: worktree_silent — 9f110001
  supervisor watch: model_not_resident — dw-svw6-sandbox       supervisor watch: task_failed — ebc40001 / f8b30001
  body (automaton_not_merged): "The supervisor watch (mode on) has an open finding:\nAutomaton d0bf0001 finished all of its work but is
                   not merged\nopen for 9 minute(s), first seen 2026-10-30 13:06:00."
[13:17:36] after the 13:17:00 pass: still 17 alerts, 17 distinct titles, alerted_at unchanged (per-episode dedupe holds)
```

Seventeen findings, seventeen alerts, no repeat. After the restart of 13:17:45 (pid 2073080, stand-in `systemctl` answering
`inactive`), the pass at 13:18:49 opened `daemon_inactive` for `dw-svw6-sandbox` and, with the boot-resumed work waiting,
`idle_lane_eligible_work` for claude-1, claude-2 and claude-3; at 13:20:49 those four were alerted: 21 alerts. The 22nd
is `worktree_silent` for `9f110001`: the restart ended its worker session, the finding closed, and when the Automaton
resumed and went silent again it opened as a new episode (same first-seen time) and alerted once more, as designed
("a re-sighting after a resolve is a new episode that may alert again"). The sandbox had no messaging channel
configured, so nothing left the sandbox.

## Comparison

The script record is the file the watch compares against (`script_output_path`). It was **injected**, because
scripts/supervisor-watch.py needs production hosts and was not run: a small helper wrote
`{"at": <unix seconds>, "findings": [{"kind","subject","text"}]}` built from the daemon's own open findings read over
REST, so the agreeing record matches the daemon exactly. The staleness window is three times `poll_seconds`, 180 s.

```
[13:10:00] record 1: 19 entries (all stored findings, resolved ones included)
[13:11:00] pass → comparison disagreed: only_script ["worktree_no_gomod/d05f0001","worktree_not_registered/d05f0001"]
           (those two had resolved when d05f0001 was blocked; the record was built from the whole list — unplanned disagreement, see Observations)
[13:12:38] record 2: 17 entries (open findings only)
[13:13:00] pass → {"agreed": true, "watch_kinds": 17, "script_kinds": 17, "only_watch": [], "only_script": []}
[13:13:30] record 3: the same 17 plus {"kind":"ci_stuck","subject":"injected-disagreement"}
[13:14:00] pass → {"agreed": false, "only_script": ["ci_stuck/injected-disagreement"], "only_watch": []}   agreed_days 0
```

The comparison history persisted by the watch (`supervisor_watch_comparisons.json` in the sandbox data directory, read
at 13:32:12 before cleanup) holds the last record and the day counters; REST, MCP and CLI showed the same difference
(section 5). Passes with no fresh record (and every pass after 13:14 once the record aged out) recorded no comparison,
by design: a missing or stale script record is "unavailable", not a disagreement.

- **Injected agreement** (13:13:00): both sides listed the same 17 kind/subject pairs, `agreed: true`.
- **Deliberate disagreement** (13:14:00): the extra entry appears under `only_script`, `agreed: false`, `only_watch` empty.

## 9. Defects found by this check, fixed in the same change

Each has a test; each was confirmed live in the sandbox after the fix.

1. **`daemon_inactive` could never be raised with the real command.** `DaemonActive` (internal/autonomous/supervisor_watch_host.go)
   ran `systemctl --user is-active datawatch` through a helper that turns any non-zero exit into an error, and
   `systemctl is-active` exits 3 for an inactive unit, so an inactive daemon read as "unknown, no finding". It now reads the
   state word from the output of a non-zero exit (`inactive`, `failed`, `activating`, `deactivating`, `reloading`,
   `maintenance` → not active; anything else, such as an unreachable user manager → unknown). Tests with a stand-in
   `systemctl` on PATH: internal/autonomous/supervisor_watch_host_systemctl_test.go. Live: finding opened 13:18:49.
2. **`tmux_restart` could not fire without a configured baseline.** The pass built a new host every poll
   (supervisor_watch_pass.go), so the baseline stored at "first sight" was dropped after each poll and every poll was a
   first sight; with a configured baseline it fired on every poll forever. The baseline now lives on the Manager
   (`supervisorWatchTmuxSeed`/`supervisorWatchTmuxStore`), is seeded from the configured value on the first poll or when
   the configured value changes, and moves on after each poll. Tests: internal/autonomous/supervisor_watch_tmux_baseline_test.go.
   Live: finding opened 13:31:58 after the stand-in restart count went 9→10 (before the fix 7→8 and 8→9 raised nothing).
3. **Clearing a string key over REST did not reach the live watch.** The live push to the manager sends the touched
   top-level block, and the empty strings and the zero `poll_seconds` were dropped from it (`omitempty`), so the manager
   kept the old value until a restart; `main_checkout_dirty` and `tmux_restart` stayed open after the restore. The push now
   carries the watch's string keys and `poll_seconds` explicitly when the PUT touched any watch key
   (`withSupervisorWatchClears` in internal/server/api.go). Tests: internal/server/supervisor_watch_config_push_test.go.
   Live: after the restore PUT at 13:21:28 the very next pass (13:21:49) resolved `main_checkout_dirty`,
   `model_not_resident` (both nodes) and `tmux_restart` with no restart.
4. **`poll_seconds: 0` was refused by REST but is "unset" for the config validator and accessor.** REST now accepts 0
   (stored as unset, reads the 60-second default) and still refuses negative values and values below 60. Test in the
   same file. Live: `PUT poll_seconds 0` → ok, `GET` shows 60; `PUT poll_seconds 30` → refused with "30 must be 0 (unset) or a
   whole number >= 60".

Also: internal/autonomous/supervisor_watch_host.go was not gofmt-clean (struct literal alignment in
`NewSupervisorWatchHost`); `gofmt -l` is now empty for every file this change touches.

## Restore, stop, cleanup

```
[13:21:28] PUT /api/config {supervisor_watch mode shadow, alert_minutes 30, silent_minutes 60, poll_seconds 0,
           script_output_path "", main_checkout "", working_model "", tmux_restart_baseline ""} → ok
[13:32:25] for each of the 7 Automata: POST …/cancel; DELETE …?hard=true → {"status":"deleted"}  (3 worker sessions killed by the delete)
[13:32:25] DELETE /api/compute/nodes/datawatch-ollama and /dw-svw6-sandbox → ok
[13:32:25] PUT /api/config {repo_ci_watch mode off, repos [], tag_count 5; merge_pending_mode off, merge_pending_alert_minutes 30;
           max_recursion_depth 0} → ok
           GET /api/config compared with the recorded prior config: supervisor_watch, repo_ci_watch, merge_pending_mode,
           merge_pending_alert_minutes, max_recursion_depth identical
[13:32:58] pass: all Automaton findings resolved without a restart; idle-lane findings resolved at 13:34:58 (lane fill lag);
           ci_red and ci_stuck remain (stored CI records, see Observations)
[13:36:03] td_stop_one 2263494 → rc 0, port 18741 closed; fixture server PID 1747760 (command line checked) stopped, port 18742 closed
[13:36:12] rm -rf tmp-sandbox/svw6 tmp-sandbox/svw5 and the scratch fixture directory; rm -f tmp-sandbox/datawatch
```

After the removal `git status --short` listed only this change's source and test files and the report; the files that
were tracked in tmp-sandbox/ before were untouched. Production daemon 3511620 was running with the same start time. No
sandbox tmux server remained.

## Commands run

All Go commands under `env -u TMUX -u TMUX_TMPDIR`; the daemon start under `env -u TMUX` with the private tmux directory
(see above); each printed a start and a done stamp (those quoted above).

- `go build -o tmp-sandbox/datawatch ./cmd/datawatch/` (three times: 12:51, 13:07 after the daemon-state fix, 13:28 after the baseline fix)
- `source scripts/test-daemon-lib.sh; td_start ./tmp-sandbox/datawatch tmp-sandbox/svw6/dw/config.yaml tmp-sandbox/svw6/dw/daemon.log` (four times),
  `td_stop_one <recorded pid>`, `td_wait_port_closed 18741 30`
- `curl` against `http://127.0.0.1:18741`: `GET /api/health`, `GET|PUT /api/config`, `GET /api/sessions`, `GET /api/alerts`,
  `GET /api/capacity`, `GET|POST|DELETE /api/compute/nodes`, `POST /api/autonomous/prds` and its `plan`, `approve`, `run`,
  `approve_task`, `set_lane_preference`, `cancel` actions, `GET|DELETE /api/autonomous/prds/<id>`, `GET /api/autonomous/status`,
  `GET /api/autonomous/lane-fill`, `GET /api/autonomous/repo-ci-watch`, `GET /api/autonomous/supervisor-watch`,
  `GET /api/autonomous/triage`, `POST /api/mcp/call` (`supervisor_watch_list`, and `send_input` for the worker and verifier steps)
- `./tmp-sandbox/datawatch autonomous supervisor-watch -u http://127.0.0.1:18741`
- `git init` / `git commit` / `git update-ref` / one file edit inside the fixture repositories only
- `rm -rf tmp-sandbox/svw6 tmp-sandbox/svw5`, `rm -f tmp-sandbox/datawatch`
- `gofmt -l`, `go vet ./internal/autonomous/ ./internal/server/`
- `go test -count=1 -run 'SupervisorWatch|ShellProbe|WithSupervisorWatch|ValidateSupervisorWatchPatch|ApplySupervisorWatchValue' ./internal/autonomous/ ./internal/server/ ./internal/mcp/ ./internal/config/ ./internal/router/ ./cmd/datawatch/` ([13:32:36] start, [13:32:45] done)
- `go test -count=1 ./internal/autonomous/ ./internal/server/ ./internal/config/ ./internal/mcp/` ([13:32:51] start, [13:35:53] done)

## Verification

- The six targeted test sets above → ok in all 6 packages; the four full package test runs → ok (autonomous 32.5 s, server 66.9 s, config, mcp).
- `go vet` on the two changed packages → clean; `gofmt -l` on every touched Go file → empty.
- The report path, the three marker lines (`Sandbox daemon`, `Production daemon: not touched`, `Comparison`) and its size are checked by the project's local verify script, which is not tracked by git and is therefore not part of this change.

Result: the shadow-mode behaviour (store, would-alert mark, zero alerts), the on-mode behaviour (one alert per finding per
episode, no repeat), the comparison record (injected agreement, deliberate disagreement), the triage-record notes and the
three read surfaces check out. Thirteen of the fourteen finding kinds were raised live from state created through the
sandbox REST API (plus the fixtures and stand-ins named above); `task_review_needed` was not produced, with the live
evidence and the code reason above. Four code defects the check exposed were fixed and re-confirmed live; two observations
(the poll guard equal to the tick, and `task_review_needed` being unreachable through a real flow) are reported and left
unchanged.
