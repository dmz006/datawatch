# Supervisor watch — sandbox live check (shadow, then on)

Sandbox daemon: 1141928, port 18731, config path tmp-sandbox/svw3/config.yaml
Production daemon: not touched (PID 3511620, started Tue Oct 20 18:19:20 2026, still running at the end of the check; all API traffic went only to 127.0.0.1:18731 and the fixture model server on 127.0.0.1:18732; every process this check stopped was stopped by a recorded PID through `td_stop_one`, no kill by name, no tmux command)
Earlier sandbox daemon PIDs in this run: 939890 (first start), 947323 (second start, after the node registration needed a boot); the last one, 1141928, was started to test one observation (see Observations)
Alerts raised in shadow mode: 0 (alert list empty after each of the three shadow reads; `would_alert` set on all 9 open findings)
Alerts raised in on mode: 9 for 9 findings (still 9 after a further poll, no repeat)
Finding kinds raised live: 8 of 14 (9 findings; `idle_lane_eligible_work` fired for two lanes): `tmux_restart`, `model_not_resident`, `main_checkout_dirty`, `verify_script_not_executable`, `worktree_no_gomod`, `worktree_not_registered`, `idle_lane_eligible_work`, `ci_red`
Kinds NOT covered: 6 of 14 — `daemon_inactive`, `task_failed`, `task_review_needed`, `automaton_not_merged`, `worktree_silent`, `ci_stuck` (reasons in the table below)
Triage-record write: not exercised live (reason below); the triage reads over REST, MCP and CLI were done and show no record for the running Automaton, as designed

## What was checked

A live pass of the daemon-side supervisor watch (`autonomous.automation.supervisor_watch.*`) against a sandbox
daemon only. Shadow mode, a one-minute alert window and a one-minute silence window were set through the sandbox
REST API. Four shadow passes ran with findings present; the last two carried a script record, one agreeing and one
with a deliberate extra entry. The watch was read back over REST, the MCP tool and the CLI. The check confirmed that
findings are stored, that `would_alert` is set, that the alert sink was called zero times in shadow, that switching to
on produced exactly one alert per finding with no repeat, and that the comparison record shows the injected agreement
and the deliberate disagreement. The Automata and nodes were removed through the API, config was restored, the sandbox
daemon and the fixture model server were stopped by recorded PID, and the sandbox directory was deleted.

## How the state was created (REST only, with the exceptions listed)

Every Automaton, node and config value the check needed was created through the sandbox REST API:

- **Node registration:** `POST /api/compute/nodes` (two nodes: `dw-svw3-sandbox`, the sandbox hostname, and
  `datawatch-ollama`, the name the planner path routes through), both pointing at the fixture model server.
- **Config:** `PUT /api/config` for every `autonomous.automation.supervisor_watch.*` key, for
  `autonomous.automation.repo_ci_watch.*` and for `autonomous.worktree_base_repo`.
- **Automata:** `POST /api/autonomous/prds` (with `guided_mode` true), `.../plan`, `.../approve`, `.../run`; later
  `.../cancel` and `DELETE ...?hard=true`. This is the Automaton REST surface.
- **The only way to give a task a verification script is the planning step.** No REST, MCP or CLI call sets it. So the
  planner's model host (`ollama.host`) pointed at a small fixture HTTP server that answers the model API shapes
  (`GET /api/tags`, `GET /api/ps`, `POST /api/generate`) and returns a one-story, one-task plan whose task names
  `scripts/check.sh` as its verification script. The daemon's own planning code produced the stored task from that
  reply; nothing was written into the daemon's data directory.
- **Guided mode** makes every task wait for approval, so the running Automaton never started a worker session
  (`GET /api/sessions` → `[]` at every read).

Not done through REST, stated plainly:

- The sandbox bootstrap config file (tmp-sandbox/svw3/config.yaml) was written by hand — a daemon needs one to start. It holds only listener port, token, data directory, hostname, session
  defaults, `autonomous.enabled`/`planning_backend` and the `ollama.host`/`ollama.model` that point at the fixture
  model server. All other values went through the API.
- Fixture files on disk, outside the daemon's data directory: a base repository with a `go.mod`
  (tmp-sandbox/svw3/base), a "main checkout" repository with one modified tracked file (tmp-sandbox/svw3/maincheckout),
  the Automaton's project directory holding `scripts/check.sh` with mode 0644 (tmp-sandbox/svw3/proj), and the script-output
  file (tmp-sandbox/svw3/script-record.json). These are the things the watch inspects, not daemon state.
- The fixture model server (a short Python program under tmp-sandbox/svw3) is a separate process, not part of the daemon.
- The daemon was stopped and started once after the nodes and `autonomous.worktree_base_repo` were set through the API,
  because the watch reads the node addresses and the base repository once at boot.

### Kind by kind

| kind | raised live | how it was produced, or why it was not |
|---|---|---|
| `main_checkout_dirty` | yes | `main_checkout` set by PUT to a repository holding a modified tracked file |
| `verify_script_not_executable` | yes | running Automaton, task verification script `scripts/check.sh` present in its project directory with mode 0644 (set through the planning reply, see above) |
| `worktree_no_gomod` | yes | `autonomous.worktree_base_repo` set by PUT to a repository holding a `go.mod`; the running Automaton's project directory holds none |
| `worktree_not_registered` | yes | same Automaton; its project directory is not in the base repository's worktree list |
| `model_not_resident` | yes | `working_model` set by PUT to a model the fixture server's `/api/ps` does not list |
| `tmux_restart` | yes | `tmux_restart_baseline` set by PUT to a value that differs from the live tmux unit state, which the watch read with its own read-only unit query. No tmux restart happened and no tmux command was issued; this exercises the compare-and-raise path with a deliberately wrong baseline |
| `idle_lane_eligible_work` | yes (gpu-1, gpu-2) | a second Automaton planned through the API and left in review counted as one eligible gpu item while both gpu lanes were idle; lane fill stayed in shadow |
| `ci_red` | yes (one real run) | `repo_ci_watch` set by PUT to shadow for the repository checkout; its stored records held 2 failing and 28 green runs, one failing run id was raised |
| `ci_stuck` | **no** | needs a stored record with status hung; none of the 30 records was hung, and a hung run cannot be created on demand |
| `daemon_inactive` | **no** | needs the user service `datawatch` to be inactive on the node whose name equals the daemon hostname. The live unit was active (read-only `is-active` returned `active`); making it inactive would stop the production service, which this check must never touch. From reading the probe, a genuinely inactive unit makes `is-active` exit non-zero, which the probe treats as an error (unknown, no finding), so this kind may be unreachable through the production probe; not verified live |
| `task_failed` | **no** | needs a task that ran and failed, plus its triage record. Running a task starts a worker session, and on this host the sandbox daemon would put it on the account's dedicated tmux socket, shared with the production daemon (the sandbox daemon logged that it fell back to it because `TMUX_TMPDIR` must stay unset). No session was started |
| `task_review_needed` | **no** | same: needs a task that ran and reached review |
| `automaton_not_merged` | **no** | needs every task completed plus a merge-pending triage record; no task can be completed without a session |
| `worktree_silent` | **no** | needs a task in progress or verifying; same reason |

## Observations about the watch (reported, not changed)

- **Clearing a string key over REST does not reach the live watch.** After the restore, `GET /api/config` showed
  `main_checkout` and `tmux_restart_baseline` empty, but `main_checkout_dirty` and `tmux_restart` stayed open for two
  further passes (12:11:15 and 12:13:15). After a restart of the sandbox daemon (12:14:19) the next pass resolved both.
  The live push sends only the touched top-level block and the empty strings are omitted from it by their omitempty
  tags, so the manager keeps the old value. `mode` (non-empty) did apply live. This is the same defect shape as an
  earlier planning-model fix, and it is why the config-restore step is not enough to disable the checks until restart.
- **Node addresses and the base repository are read once at boot.** A node registered or a base repository set after
  start is not seen by `model_not_resident` or the worktree checks until the daemon restarts. That is why the daemon
  was restarted once during setup.
- **`ci_red` outlives the repo CI watch.** With `repo_ci_watch` back to off, the stored CI records still held failing
  runs, so `ci_red` stayed open after restart. It reads stored records only, by design.
- **The specification's "draft Automaton" cannot raise `verify_script_not_executable`:** the check reads running
  Automata only. The finding was raised from a running Automaton instead (see the table).
- **The specification's "sandbox project dir must not be inside the repository" conflicts with the write boundary** (all
  writes must stay inside this worktree). The sandbox lived at tmp-sandbox/svw3 with its own nested repositories; automatic
  repository initialisation and commits were off in the sandbox session config, and nothing was committed there.

## Procedure and outputs

All writes stayed under tmp-sandbox/svw3 (removed at the end) plus the binary tmp-sandbox/datawatch (removed at the end).
The files that were already tracked in tmp-sandbox/ from earlier checks were not touched. `TMUX` and `TMUX_TMPDIR` were
unset for every command. Daemon start and stop used scripts/test-daemon-lib.sh with no traps installed in the starting
shell and its state directory under tmp-sandbox/svw3/tdstate. Every command below was run with a start and a done stamp
(the stamps are those printed around the command).

### 1. Build and start

```
[11:57:15] start  create fixtures, fixture model server, bootstrap config; go build -o tmp-sandbox/datawatch ./cmd/datawatch/   [11:57:18] done
[11:57:28] start  fixture model server on 127.0.0.1:18732; td_start ./tmp-sandbox/datawatch tmp-sandbox/svw3/config.yaml tmp-sandbox/svw3/daemon.log   [11:57:35] done  pid 939890
GET /api/health → 200, hostname dw-svw3-sandbox, rest listener enabled, loopback, token required
```

### 2. Prior config (recorded for restore)

```
[11:57:41] GET /api/config → autonomous.automation.supervisor_watch:
  {"alert_minutes":30,"main_checkout":"","mode":"shadow","poll_seconds":60,"script_output_path":"",
   "silent_minutes":60,"tmux_restart_baseline":"","working_model":""}
 autonomous.automation.repo_ci_watch: {"alert_minutes":30,"delete_superseded":"report","hung_after_minutes":60,"mode":"off",
   "poll_seconds":300,"repos":[],"security_workflows":[...5 names...],"tag_count":5}
 autonomous.worktree_base_repo: ""      autonomous.lane_fill.mode: "shadow"
```

(`mode` reads `shadow` with no value configured: an empty mode is shadow.)

### 3. Configure through the API, then restart once

```
[11:57:50] POST /api/compute/nodes {"name":"dw-svw3-sandbox","kind":"ollama","address":"http://127.0.0.1:18732"} → ok
[11:57:50] PUT /api/config {
  "autonomous.worktree_base_repo": "tmp-sandbox/svw3/base",
  "autonomous.automation.supervisor_watch.mode": "shadow",
  "autonomous.automation.supervisor_watch.alert_minutes": 1,
  "autonomous.automation.supervisor_watch.silent_minutes": 1,
  "autonomous.automation.supervisor_watch.script_output_path": "tmp-sandbox/svw3/script-record.json",
  "autonomous.automation.supervisor_watch.main_checkout": "tmp-sandbox/svw3/maincheckout",
  "autonomous.automation.supervisor_watch.working_model": "qwen-stub-working",
  "autonomous.automation.supervisor_watch.tmux_restart_baseline": "0"} → {"status":"ok"}
[11:58:12] td_stop_one 939890 → rc 0, port 18731 closed; td_start again → pid 947323   [11:58:18] done
```

A first plan attempt failed because the planner path routes through a node named `datawatch-ollama` that did not
exist ("compute node not found", four automatic retries); registering that node at 11:59:38 fixed it.

```
[11:58:24] POST /api/autonomous/prds {"spec":"…","project_dir":"tmp-sandbox/svw3/proj","backend":"shell","guided_mode":true} → id 2ccf0001, draft
[11:59:38] POST …/2ccf0001/plan → needs_review; one story, one task, verification_script "scripts/check.sh"
[11:59:53] POST …/2ccf0001/approve → approved (task awaiting_approval); POST …/run → running
           task 7c940002: awaiting_approval, session_id null;  GET /api/sessions → []
[12:00:21] PUT /api/config repo_ci_watch.mode shadow, repos [<the repository checkout>], tag_count 1
[12:00:37] second Automaton 26030001 (lane_preference gpu): planned → needs_review; approve refused by the plan-coverage
           gate (kept in review); lane fill then reported queued_eligible {"gpu":1,"claude":0}, gpu-1 and gpu-2 idle
```

### 4. Polls in shadow mode

| pass | time | what happened |
|---|---|---|
| — | 11:59:15 | three findings stored (`tmux_restart`, `model_not_resident`, `main_checkout_dirty`); the Automaton was not running yet |
| 1 | 12:01:21 | all nine findings stored; no script record yet, so no comparison |
| 2 | 12:03:15 | no comparison (the script record was written at 12:03:37, after this pass) |
| 3 | 12:05:15 | all nine `would_alert: true`, `alerted_at` null; comparison **agreed** |
| 4 | 12:06:18 | all nine `would_alert: true`; comparison **disagreed** (script-only `ci_stuck/injected-disagreement`) |

```
[12:05:23] GET /api/alerts → {"count":0,"unread":0}          (after pass 3)
[12:06:21] GET /api/alerts → {"count":0}                     (after pass 4; would_alert set on 9, alerted_at on 0)
```

The nine findings, as raised (wording identical on every surface):

```
worktree_not_registered       2ccf0001  the worktree of Automaton 2ccf0001 is not registered
worktree_no_gomod             2ccf0001  the worktree of Automaton 2ccf0001 has no go.mod: its project directory is broken
verify_script_not_executable  2ccf0001  the verify script of Automaton 2ccf0001 is not executable: run its check manually
tmux_restart                  datawatch-tmux  tmux restarted on datawatch-tmux: audit every non-terminal Automaton there
model_not_resident            dw-svw3-sandbox  the working model is not resident on dw-svw3-sandbox: reload it before routing more Automata
main_checkout_dirty           tmp-sandbox/svw3/maincheckout  the main checkout tmp-sandbox/svw3/maincheckout has uncommitted tracked files: any Automaton ship will abort until they are committed
idle_lane_eligible_work       gpu-2  lane gpu-2 is idle while Automata work that is eligible to run is waiting
idle_lane_eligible_work       gpu-1  lane gpu-1 is idle while Automata work that is eligible to run is waiting
ci_red                        41051511556  CI run 41051511556 is red: triage the failure before shipping more Automata
```

Stored: `supervisor_watch.jsonl` in the sandbox data directory held 9 records (read at 12:06:53, before cleanup).

### 5. Read the findings back — REST, MCP tool, CLI (all three agree)

```
[12:06:44] REST: GET /api/autonomous/supervisor-watch
           → mode shadow, last_poll_at 12:06:18, 9 findings, comparison.last.agreed false,
             only_script ["ci_stuck/injected-disagreement"]
[12:06:44] MCP:  POST /api/mcp/call {"tool":"supervisor_watch_list","params":{}}
           → content[0].text parses to the same body; the sorted kind/subject list is identical to REST (9 entries)
[12:06:44] CLI:  DATAWATCH_TOKEN=*** ./tmp-sandbox/datawatch autonomous supervisor-watch -u http://127.0.0.1:18731
Supervisor watch mode: shadow
Last poll: 2026-10-30 12:06:18
Findings: 9
worktree_not_registered       would alert  2026-10-30 12:06:18  the worktree of Automaton 2ccf0001 is not registered
worktree_no_gomod             would alert  2026-10-30 12:06:18  the worktree of Automaton 2ccf0001 has no go.mod: its project directory is broken
verify_script_not_executable  would alert  2026-10-30 12:06:18  the verify script of Automaton 2ccf0001 is not executable: run its check manually
tmux_restart                  would alert  2026-10-30 12:06:18  tmux restarted on datawatch-tmux: audit every non-terminal Automaton there
model_not_resident            would alert  2026-10-30 12:06:18  the working model is not resident on dw-svw3-sandbox: reload it before routing more Automata
main_checkout_dirty           would alert  2026-10-30 12:06:18  the main checkout tmp-sandbox/svw3/maincheckout has uncommitted tracked files: …
idle_lane_eligible_work       would alert  2026-10-30 12:06:18  lane gpu-2 is idle while Automata work that is eligible to run is waiting
idle_lane_eligible_work       would alert  2026-10-30 12:06:18  lane gpu-1 is idle while Automata work that is eligible to run is waiting
ci_red                        would alert  2026-10-30 12:06:18  CI run 41051511556 is red: triage the failure before shipping more Automata
Comparison: differs at 2026-10-30 12:06:18 (days of full agreement: 0)
  only the script raised: ci_stuck/injected-disagreement
```

### 6. Triage record — what was and was not exercised

Read at 12:06:53 over REST, over the MCP tool `autonomous_triage` and over the CLI `autonomous triage`:

```
REST: {"records":[],"alert_minutes":30}
MCP:  {"records":[],"alert_minutes":30}
CLI:  No failed or blocked Automata to triage. (Triage alert threshold: 30 min)
GET …/prds/2ccf0001 → {"status":"running","triage":null}
```

The pass writes a first-sighting note only for an Automaton-scoped finding whose Automaton already has a triage
record, or mints one for a failed or blocked Automaton that has none. A running Automaton has no record, by design,
so the three Automaton-scoped findings raised here (`verify_script_not_executable`, `worktree_no_gomod`,
`worktree_not_registered`) correctly wrote no note. **The note-writing path itself was not exercised live.** It
needs a failed or blocked Automaton with a named task, which only `task_failed`, `task_review_needed` and
`automaton_not_merged` produce, and those need a task that ran (see the table). It is covered by the package tests,
not by this check.

### 7. Switch to on — exactly one alert per finding

```
[12:07:01] PUT /api/config {"autonomous.automation.supervisor_watch.mode":"on"} → {"status":"ok"}
[12:08:15] pass → GET /api/alerts (12:08:16): count 9, unread 9; all level warn, source system:
  supervisor watch: ci_red — 41051511556
  supervisor watch: idle_lane_eligible_work — gpu-1
  supervisor watch: idle_lane_eligible_work — gpu-2
  supervisor watch: main_checkout_dirty — tmp-sandbox/svw3/maincheckout
  supervisor watch: model_not_resident — dw-svw3-sandbox
  supervisor watch: tmux_restart — datawatch-tmux
  supervisor watch: verify_script_not_executable — 2ccf0001
  supervisor watch: worktree_no_gomod — 2ccf0001
  supervisor watch: worktree_not_registered — 2ccf0001
  body (ci_red): "The supervisor watch (mode on) has an open finding:\nCI run 41051511556 is red: triage the failure
                  before shipping more Automata\nopen for 6 minute(s), first seen 2026-10-30 12:01:21."
findings: 9 of 9 carry alerted_at
[12:09:15] next pass → alerts still 9, 9 distinct titles, alerted_at unchanged
```

Nine findings, nine alerts, no repeat on the next pass (per-episode dedupe holds). The sandbox had no messaging channel
configured, so nothing left the sandbox.

## Comparison

The script record is the file the watch compares against (`script_output_path`). It was **injected**: written inline
with a fresh `at` (unix seconds) and the oracle format `{"at":…,"findings":[{"kind","subject","text"}]}`, built from the
daemon's own open findings so that the first record agrees exactly; scripts/supervisor-watch.py itself needs production
hosts and was not run. Written at 12:03:37 (nine entries) and at 12:05:24 (the same nine plus one extra, `ci_stuck` /
`injected-disagreement`, which the daemon cannot raise in this sandbox). The staleness window is three times
`poll_seconds`, 180 s; each record was fresh at the pass that read it.

The comparison history the watch store persisted (`supervisor_watch_comparisons.json`, read at 12:09:18, before cleanup):

```
{"at":"2026-10-30T12:05:15","agreed":true,
 "watch_kinds":[9 kind/subject pairs, listed in section 4],
 "script_kinds":[the same 9]}
{"at":"2026-10-30T12:06:18","agreed":false,
 "watch_kinds":[the same 9],
 "script_kinds":[the same 9 plus "ci_stuck/injected-disagreement"],
 "only_script":["ci_stuck/injected-disagreement"]}
```

- **Injected agreement** (12:05:15): both sides listed the same nine kind/subject pairs, `agreed: true`, both one-sided
  differences empty, `agreed_days` 1.
- **Deliberate disagreement** (12:06:18): the extra entry shows under `only_script`, `agreed: false`, `only_watch` empty,
  `agreed_days` fell to 0; REST, MCP and CLI all show the same difference.
- Passes with no fresh record (12:01:21, 12:03:15 and every on-mode pass after 12:07) recorded no comparison, by design:
  a missing or stale script record is "unavailable", not a disagreement.

## Restore, stop, cleanup

```
[12:09:26] DELETE …/prds/26030001?hard=true → {"status":"deleted"}
[12:09:26] DELETE …/prds/2ccf0001?hard=true → refused: "is running; cancel before deleting"
[12:09:34] POST …/prds/2ccf0001/cancel → cancelled;  DELETE …/prds/2ccf0001?hard=true → {"status":"deleted","killed_sessions":0}
[12:09:26] DELETE /api/compute/nodes/datawatch-ollama and /dw-svw3-sandbox → ok (both)
[12:09:26] PUT /api/config {supervisor_watch mode shadow, alert_minutes 30, silent_minutes 60, script_output_path "",
           main_checkout "", working_model "", tmux_restart_baseline ""; repo_ci_watch mode off, repos [], tag_count 5;
           worktree_base_repo ""} → ok
           GET /api/config compared with the recorded prior config: supervisor_watch, repo_ci_watch and worktree_base_repo identical
[12:11:15] pass: Automaton gone → 6 findings resolved; 3 still open (tmux_restart, main_checkout_dirty, ci_red) — see Observations
[12:14:19] td_stop_one 947323, start again (pid 1141928) → at 12:15:23 the pass resolved tmux_restart and main_checkout_dirty; ci_red stays (stored records)
[12:15:32] td_stop_one 1141928 → rc 0;  td_stop_one on the fixture server's recorded wrapper PID (953288) → rc 0
[12:15:57] port 18732 still listening: the recorded PID was the setsid wrapper; the real server process (953291, command line
           naming the fixture server, owner of the socket on the sandbox port) was recorded with td_track_pid and stopped with
           td_stop_one → rc 0, port closed, no listener on 18731 or 18732
[12:16:27] rm -rf tmp-sandbox/svw3 ; rm -f tmp-sandbox/datawatch
```

After the removal: tmp-sandbox/ held only the files that were tracked there before, `git status --short` was empty, and
nothing listened on ports 18731 or 18732. Production daemon 3511620 was still running with the same start time. The
Automata were hard-deleted through the API (cancel first, because a running Automaton refuses deletion); the nodes were
deleted through the API; the nine sandbox alerts went with the sandbox data directory.

## Commands run

All under `env -u TMUX -u TMUX_TMPDIR`, each printed with a start and a done stamp (those quoted above).

- `go build -o tmp-sandbox/datawatch ./cmd/datawatch/`
- `source scripts/test-daemon-lib.sh; td_start ./tmp-sandbox/datawatch tmp-sandbox/svw3/config.yaml tmp-sandbox/svw3/daemon.log` (three times)
- `td_track_pid`, `td_stop_one <recorded pid>`, `td_wait_port_closed 18731 20`, `td_wait_port_closed 18732 20`
- `curl` against `http://127.0.0.1:18731`: `GET /api/health`, `GET|PUT /api/config`, `GET /api/sessions`, `GET /api/alerts`,
  `GET|POST|DELETE /api/compute/nodes`, `POST /api/autonomous/prds` and its `plan`, `approve`, `run`, `cancel` actions,
  `GET|DELETE /api/autonomous/prds/<id>`, `GET /api/autonomous/lane-fill`, `GET /api/autonomous/repo-ci-watch`,
  `GET /api/autonomous/supervisor-watch`, `GET /api/autonomous/triage`, `POST /api/mcp/call` (`supervisor_watch_list`,
  `autonomous_triage`)
- `./tmp-sandbox/datawatch autonomous supervisor-watch -u http://127.0.0.1:18731`, `./tmp-sandbox/datawatch autonomous triage -u http://127.0.0.1:18731`
- `git init` / `git commit` / one file edit inside tmp-sandbox/svw3/base and tmp-sandbox/svw3/maincheckout only
- a read-only `systemctl --user is-active datawatch` (to explain why `daemon_inactive` was not produced); the daemon's own
  probes ran their read-only unit queries themselves
- `rm -rf tmp-sandbox/svw3`, `rm -f tmp-sandbox/datawatch`
- `go test -count=1 -run SupervisorWatch ./internal/autonomous/ ./internal/server/ ./internal/mcp/ ./internal/config/ ./internal/router/ ./cmd/datawatch/` ([12:16:27] start, [12:16:30] done)

## Verification

- .datawatch/verify/supervisor-watch-live-check.sh
- `go test -count=1 -run SupervisorWatch ./internal/autonomous/ ./internal/server/ ./internal/mcp/ ./internal/config/ ./internal/router/ ./cmd/datawatch/` → ok in all 6 packages

Result: the shadow-mode behaviour (store, would-alert mark, zero alerts), the on-mode behaviour (one alert per finding,
no repeat), the comparison record (injected agreement and deliberate disagreement) and all three read surfaces check
out. Eight of the fourteen finding kinds were raised live, all from state created through the sandbox REST API (plus the
fixture files and fixture model server named above); six kinds were not produced, with the reason for each in the table,
and the triage-record write path was not exercised. Three observations about the watch itself are listed above and were
left unchanged.
