# Supervisor watch — sandbox live check (shadow, then on)

Sandbox daemon: 484180, port 18710, config path tmp-sandbox/svw/config.yaml
Production daemon: not touched (PID 3511620, started Tue Oct 20 18:19:20 2026, still running at the end of the check; all API traffic in this check went only to 127.0.0.1:18710, and the only process stopped was the sandbox daemon, by `td_stop_one 484180` — its recorded PID, no kill by name, no tmux server command)
Alerts raised in shadow mode: 0 (alert list empty after both shadow polls; `would_alert` set on all three findings)
Alerts raised in on mode: 3 for 3 findings (still 3 after a further poll)
Finding kinds raised live: 3 of 14 — `verify_script_not_executable`, `task_failed`, `main_checkout_dirty`
Triage-record write: exercised live (one note, source `supervisor_watch`, on a failed Automaton's record)

## What was checked

A live pass of the daemon-side supervisor watch (`autonomous.automation.supervisor_watch.*`) against a sandbox
daemon only. Shadow mode, a one-minute alert window and a one-minute silence window were set through the sandbox
REST API. Two shadow polls and two on-mode polls ran. The watch was read back over REST, the MCP tool and the CLI.
The check confirmed that the findings are stored, that `would_alert` is set, that the alert sink was called zero
times in shadow, that switching to on produced exactly one alert per finding with no repeat, that the first
sighting of an Automaton-scoped finding landed on the triage record, and that the comparison record shows an
injected agreement and a deliberate disagreement. Config was restored, the Automata were cancelled, the sandbox
daemon was stopped by its recorded PID, and the whole sandbox directory was deleted.

## Departures from the specification and honest limits

- **The specified draft Automaton cannot produce `verify_script_not_executable`.** The check inspects running
  Automata only (`SupervisorWatchRunningUnits` skips every other status), so a draft is never flagged. Separately,
  no REST, MCP or CLI call sets a task's verification script path — only the decompose step fills it — and a draft
  Automaton refuses structural edits. The first attempt at this check created the draft and correctly saw nothing.
  This attempt therefore created the state differently, described next.
- **How the Automata were created (the one step not done through REST).** Before the daemon started, a throwaway
  program under tmp-sandbox/svw/seed wrote two Automaton records into the sandbox data directory through the
  daemon's own record store writer, then was deleted with the rest of tmp-sandbox/svw:
  - a running Automaton whose only task is held at awaiting-approval (the executor never starts a task in that
    state, so no worker session is launched) and whose task names `scripts/check.sh` as its verification script;
    that file exists inside the Automaton's worktree with mode 0644, no exec bit;
  - a failed Automaton with one completed task and one failed task (a completed task makes boot recovery skip it, so
    it stays failed). It was seeded with no triage record: the daemon's own triage pass built the record on its first
    idle tick, just before the watch pass ran.
  Everything else — all config, all reads, the mode switch — went through the sandbox REST API (the read-backs also
  through the MCP tool and the CLI).
- Only three of the fourteen kinds were induced. The other eleven (`tmux_restart`, `daemon_inactive`,
  `model_not_resident`, `task_review_needed`, `automaton_not_merged`, `idle_lane_eligible_work`,
  `worktree_no_gomod`, `worktree_not_registered`, `worktree_silent`, `ci_red`, `ci_stuck`) need state a bare sandbox
  does not have (registered compute nodes, a tmux baseline, review-held tasks, merge-pending records, lane-fill work,
  registered worktrees and session logs, repo CI records). They are covered by the package tests, not by this live
  check. `ci_red` appears below only as the deliberate script-side extra in the comparison, not as a daemon finding.
- The triage-record write was seen for the Automaton-scoped finding on the failed Automaton. The running
  Automaton got no note, by design (a running Automaton has no triage record; its finding store record is its
  record), and the fleet-scoped `main_checkout_dirty` writes no note either. The note path that mints a fresh record
  for a failed Automaton that has none was not exercised: the triage pass had already filed the record.
- **Polls run every second minute.** The pass's own one-minute guard skips the tick that lands 59.999 s after the
  previous pass, so passes ran at 11:25:09, 11:27:09, 11:29:09 and 11:31:09. That is the documented guard, not a
  failure. It is why `would_alert` was not set at the first poll (the findings were seconds old against a one-minute
  window) and was set at the second.
- **tmux socket.** The sandbox daemon logged `TMUX_TMPDIR was unset — using dedicated socket` at start-up: with the
  variable unset, as the rules require, it falls back to the account's dedicated tmux socket. I issued no tmux
  command, and the sandbox listed zero sessions at each of its three reads (11:24:17, 11:25:14, 11:31:22: `GET /api/sessions` → `[]`), so the shutdown line
  `Killing active sessions...` had no sessions of its own to end. I did not look inside the tmux server to prove
  nothing else was touched.
- **Cancel, not purge, through the API.** The two Automata were cancelled through the Automaton cancel endpoint
  (status `cancelled`, 0 sessions killed). That does not remove the record; the records were removed when the sandbox
  directory was deleted after the daemon stopped. `git status` was empty afterwards.

## Procedure and outputs

All writes stayed under tmp-sandbox/svw/ in this worktree (removed at the end) plus the binary tmp-sandbox/datawatch
(removed at the end). The files already in tmp-sandbox/ from earlier checks were not touched. TMUX and TMUX_TMPDIR were
unset for every command. Daemon start and stop used scripts/test-daemon-lib.sh with no traps installed in the
starting shell (an EXIT trap would have ended the daemon when the call returned), with the state directory under
tmp-sandbox/svw/tdstate.

### 1. Build, seed and start the sandbox daemon

```
[11:23:38] start  mkdir sandbox dirs; write config, nested repo, non-executable script      [11:23:38] done
[11:23:53] start  go build -o tmp-sandbox/datawatch ./cmd/datawatch/                         [11:23:57] done (4 s)
[11:24:05] start  go run ./tmp-sandbox/svw/seed tmp-sandbox/svw/data tmp-sandbox/svw/project/wt   [11:24:05] done rc=0
                  seeded 5a0e0001 running / seeded 5a0e0002 failed
[11:24:05] start  td_start ./tmp-sandbox/datawatch tmp-sandbox/svw/config.yaml tmp-sandbox/svw/daemon.log   [11:24:05] done rc=0, pid 484180
[11:24:11] start  GET /api/health → 200, hostname dw-svw-sandbox, rest listener enabled, loopback   [11:24:11] done
```

Sandbox config: loopback only, no TLS, REST token set, MCP listener off, session backend `shell`, `data_dir` and
`session.default_project_dir` under tmp-sandbox/svw/, `autonomous.enabled true`.

Boot-resume decisions in the daemon log (the two Automata behaved as designed, no worker launched): the running
Automaton `5a0e0001` was resumed (age 4 s, within the resume limit) and its executor re-launched; the failed
Automaton `5a0e0002` was scanned and recovery was skipped because its tasks have results; the summary read
"resumed 1, parked 0".

State read back at 11:24:17: `5a0e0001` running, its task `awaiting_approval`, verification script `scripts/check.sh`;
`5a0e0002` failed, tasks `completed` and `failed`; no triage record on either yet; `GET /api/sessions` → `[]`.

### 2. Prior config (recorded for restore)

```
[11:24:11] GET /api/config → autonomous.automation.supervisor_watch:
{"alert_minutes":30,"main_checkout":"","mode":"shadow","poll_seconds":60,"script_output_path":"",
 "silent_minutes":60,"tmux_restart_baseline":"","working_model":""}
```

(`mode` reads `shadow` with no value configured: an empty mode is shadow.)

### 3. Configure shadow mode, inject what the check needs

```
[11:24:17] start  PUT /api/config {
  "autonomous.automation.supervisor_watch.mode": "shadow",
  "autonomous.automation.supervisor_watch.alert_minutes": 1,
  "autonomous.automation.supervisor_watch.silent_minutes": 1,
  "autonomous.automation.supervisor_watch.script_output_path": "tmp-sandbox/svw/script-record.json",
  "autonomous.automation.supervisor_watch.main_checkout": "tmp-sandbox/svw/maincheckout"
} → {"status":"ok"}                                                                         [11:24:17] done
```

Injected inline (no daemon call involved):

- A tracked file modified in the nested repository tmp-sandbox/svw/maincheckout (`git status --short` → ` M tracked.txt`)
  so `main_checkout_dirty` has a real finding.
- The two Automata above (`5a0e0001` running with the non-executable script, `5a0e0002` failed).
- A script-output file in the format scripts/supervisor-watch.py writes under `--record`
  (`{"at": <unix seconds>, "findings": [{"kind", "subject", "text"}]}`), rewritten with a fresh `at` before each
  comparison window. For the agreement it lists exactly the three findings the daemon raises
  (`main_checkout_dirty`, `task_failed`, `verify_script_not_executable`). At 11:25:21 it was rewritten with one extra
  entry, `ci_red` / `injected-disagreement`, that the daemon cannot raise.

### 4. Polls in shadow mode

| poll | time | what happened |
|---|---|---|
| 1 | 11:25:09 | three findings stored, none marked `would_alert` (just opened); triage note written on `5a0e0002`; comparison: **agreed** |
| — | 11:26:09 | skipped by the pass's one-minute guard |
| 2 | 11:27:09 | all three findings `would_alert: true`, `alerted_at` null; comparison: **disagreed** (script-only `ci_red/injected-disagreement`) |

```
[11:25:10] GET /api/alerts after poll 1 → {"alerts":[],"unread_count":0}
[11:27:10] GET /api/alerts after poll 2 → {"alerts":[],"unread_count":0}     (would_alert set, alert sink called 0 times)
[11:27:24] GET /api/alerts             → {"alerts":[],"unread_count":0}
```

Finding texts as raised (the same wording on every surface):

```
verify_script_not_executable  5a0e0001  the verify script of Automaton 5a0e0001 is not executable: run its check manually
task_failed                   5a0e0002  a task in Automaton 5a0e0002 failed: read the verifier verdict before retrying
main_checkout_dirty           tmp-sandbox/svw/maincheckout  the main checkout tmp-sandbox/svw/maincheckout has uncommitted tracked files: any Automaton ship will abort until they are committed
```

### 5. Read the findings back — REST, MCP tool, CLI (all three agree)

```
[11:27:19] REST: GET /api/autonomous/supervisor-watch
{"mode":"shadow","last_poll_at":"2026-10-30T11:27:09.581334094-04:00",
 "findings":[
  {"kind":"main_checkout_dirty","subject":"tmp-sandbox/svw/maincheckout","would_alert":true,"would_alert_at":"2026-10-30T11:27:09.58…","resolved":false,…},
  {"kind":"task_failed","subject":"5a0e0002","would_alert":true,"would_alert_at":"2026-10-30T11:27:09.58…","resolved":false,…},
  {"kind":"verify_script_not_executable","subject":"5a0e0001","would_alert":true,"would_alert_at":"2026-10-30T11:27:09.58…","resolved":false,…}],
 "comparison":{"last":{"at":"2026-10-30T11:27:09.58…","watch_kinds":[3 entries],"script_kinds":[4 entries],
   "only_script":["ci_red/injected-disagreement"],"agreed":false},
  "agreed_days":0,"only_watch":[],"only_script":["ci_red/injected-disagreement"]}}

[11:27:19] MCP:  POST /api/mcp/call {"tool":"supervisor_watch_list","params":{}}
      → content[0].text parses to the same body (mode shadow, 3 findings, all would_alert true,
        comparison.last.agreed false, only_script ["ci_red/injected-disagreement"]);
        the (kind, subject) list is identical to the REST list

[11:27:20] CLI:  DATAWATCH_TOKEN=*** ./tmp-sandbox/datawatch autonomous supervisor-watch -u http://127.0.0.1:18710   [11:27:20] done rc=0
Supervisor watch mode: shadow
Last poll: 2026-10-30 11:27:09
Findings: 3
main_checkout_dirty           would alert  2026-10-30 11:27:09  the main checkout tmp-sandbox/svw/maincheckout has uncommitted tracked files: …
task_failed                   would alert  2026-10-30 11:27:09  a task in Automaton 5a0e0002 failed: read the verifier verdict before retrying
verify_script_not_executable  would alert  2026-10-30 11:27:09  the verify script of Automaton 5a0e0001 is not executable: run its check manually
Comparison: differs at 2026-10-30 11:27:09 (days of full agreement: 0)
  only the script raised: ci_red/injected-disagreement
```

Stored: the finding records are in the watch store (`supervisor_watch.jsonl` under the sandbox data directory), read
before cleanup: three open (`resolved` false) records with `would_alert` true; after the on-mode poll all three carried
`alerted_at`.

### 6. Triage record — the write path for an Automaton-scoped finding

Read at 11:25:14 (right after poll 1), over REST, and again at 11:27:20 over the MCP tool `autonomous_triage` and the
CLI `autonomous triage`:

```
5a0e0002 (failed): task_id 5a0e0002-t2, error "sandbox seeded failure",
  first_seen_at 2026-10-30T11:25:09.581…  (the daemon's triage pass, just before the watch pass)
  notes: [{"at":"2026-10-30T11:25:09.596…","source":"supervisor_watch",
           "text":"a task in Automaton 5a0e0002 failed: read the verifier verdict before retrying"}]
5a0e0001 (running): triage record absent (null) — no note, by design

CLI: Triage: 1 Automaton waiting for the operator (alert threshold: 30 min), newest first:
  Automaton "Sandbox failed Automaton" (id 5a0e0002) is failed and waiting for operator triage. …
  Notes:
    2026-10-30 11:25 supervisor_watch: a task in Automaton 5a0e0002 failed: read the verifier verdict before retrying
```

The note landed once, at first sighting; the read at 11:27:24, after the second poll, still showed exactly one note.

### 7. Switch to on — exactly one alert per finding

```
[11:27:33] start  PUT /api/config {"autonomous.automation.supervisor_watch.mode":"on"} → {"status":"ok"}   [11:27:33] done
[11:29:09] poll → GET /api/alerts (read 11:29:13): unread 3, count 3
  warn system  supervisor watch: main_checkout_dirty — tmp-sandbox/svw/maincheckout
  warn system  supervisor watch: task_failed — 5a0e0002
  warn system  supervisor watch: verify_script_not_executable — 5a0e0001
  body (each): "The supervisor watch (mode on) has an open finding:\n<finding text>\nopen for 3 minute(s), first seen 2026-10-30 11:25:09."
findings: all three  alerted_at 2026-10-30T11:29:09.581…
[11:31:09] next poll → alerts still 3 (same three titles), alerted_at unchanged
```

Three findings, three alerts, no repeat on the next poll (per-episode dedupe holds).

## Comparison

The comparison history the watch store persisted (`supervisor_watch_comparisons.json` in the sandbox data directory,
read at 11:27:24, before cleanup):

```
{"at":"2026-10-30T11:25:09","agreed":true,
 "watch_kinds":["main_checkout_dirty/tmp-sandbox/svw/maincheckout","task_failed/5a0e0002","verify_script_not_executable/5a0e0001"],
 "script_kinds":[the same three]}
{"at":"2026-10-30T11:27:09","agreed":false,
 "watch_kinds":[the same three],
 "script_kinds":["ci_red/injected-disagreement", …the same three],
 "only_script":["ci_red/injected-disagreement"]}
days: {"2026-10-30":{"total":2,"disagreed":1}}
```

- **Injected agreement** (11:25:09): watch and script both raised the same three kind/subject pairs, including
  `verify_script_not_executable/5a0e0001`; `agreed: true`, both one-sided differences empty, `agreed_days` 1.
- **Deliberate disagreement** (11:27:09): the script record carried one extra entry, `ci_red/injected-disagreement`;
  the comparison shows it under `only_script`, `agreed: false`, and REST, MCP and CLI all show the same difference.
  `agreed_days` fell to 0 because the day now holds a disagreeing comparison.
- No comparison was recorded for the skipped 11:26:09 tick (no pass ran). A stale or missing script record records no
  comparison at all; the record was rewritten with a fresh instant before each window so none was dropped as stale
  (window = 3 × poll_seconds = 180 s).

## Restore, stop, cleanup

```
[11:31:30] PUT /api/config {"…mode":"shadow","…alert_minutes":30,"…silent_minutes":60,"…script_output_path":"","…main_checkout":""} → {"status":"ok"}
[11:31:30] GET /api/config → {"alert_minutes":30,"main_checkout":"","mode":"shadow","poll_seconds":60,"script_output_path":"",
                              "silent_minutes":60,"tmux_restart_baseline":"","working_model":""}   (identical to the prior config)
[11:31:30] cancel 5a0e0001 → {"killed_session_ids":[],"killed_sessions":0,"status":"cancelled"}
[11:31:30] cancel 5a0e0002 → {"killed_session_ids":[],"killed_sessions":0,"status":"cancelled"}
[11:31:37] start  TD_STATE_DIR=tmp-sandbox/svw/tdstate td_stop_one 484180   [11:31:37] done rc=0
[11:31:37] start  td_wait_port_closed 18710 20                              [11:31:37] done rc=0   (pid 484180 gone, port 18710 closed)
daemon log: "Shutting down... (signal=terminated)"
[11:31:44] rm -rf tmp-sandbox/svw ; rm -f tmp-sandbox/datawatch
```

After the removal: tmp-sandbox/ held only the files that were there before; `git status --short` was empty; nothing
listened on port 18710. Production daemon 3511620 was still running afterwards with the same start time.

## Commands run

All under `env -u TMUX -u TMUX_TMPDIR`. Every call was logged by a wrapper that wrote a start and a done stamp for
the command (`rc` shown on the done line); the stamps quoted above come from that log. The setup, build and teardown
commands are stamped in the sections above; the read-only polling loops (`GET /api/autonomous/supervisor-watch`
every 3–4 s while waiting for a pass) each have their own start/done pair in the log and are not listed one by one.

- `go build -o tmp-sandbox/datawatch ./cmd/datawatch/`
- `go run ./tmp-sandbox/svw/seed tmp-sandbox/svw/data tmp-sandbox/svw/project/wt` (the seeding program, deleted afterwards)
- `source scripts/test-daemon-lib.sh; td_start ./tmp-sandbox/datawatch tmp-sandbox/svw/config.yaml tmp-sandbox/svw/daemon.log`
- `curl` against `http://127.0.0.1:18710`: `GET /api/health`, `GET /api/config`, `PUT /api/config`, `GET /api/sessions`,
  `GET /api/autonomous/supervisor-watch`, `GET /api/autonomous/triage`, `GET /api/alerts`, the Automaton detail read and
  cancel calls for `5a0e0001` and `5a0e0002`, `POST /api/mcp/call` (`supervisor_watch_list`, `autonomous_triage`)
- `./tmp-sandbox/datawatch autonomous supervisor-watch -u http://127.0.0.1:18710`
- `./tmp-sandbox/datawatch autonomous triage -u http://127.0.0.1:18710`
- `git init` / `git commit` / file edit inside tmp-sandbox/svw/maincheckout only
- `td_stop_one 484180`, `td_wait_port_closed 18710 20`
- `rm -rf tmp-sandbox/svw`, `rm -f tmp-sandbox/datawatch`
- `go test -count=1 -run SupervisorWatch ./internal/autonomous/ ./internal/server/ ./internal/mcp/ ./internal/config/ ./internal/router/ ./cmd/datawatch/` (11:32:14 → 11:32:17)

## Verification

- .datawatch/verify/supervisor-watch-live-check.sh
- `go test -count=1 -run SupervisorWatch ./internal/autonomous/ ./internal/server/ ./internal/mcp/ ./internal/config/ ./internal/router/ ./cmd/datawatch/` → ok in all 6 packages

Result: the shadow-mode behaviour (store, would-alert mark, zero alerts), the on-mode behaviour (one alert per
finding, no repeat), the comparison record (agreement and deliberate disagreement), the triage-record write for an
Automaton-scoped finding, and all three read surfaces check out. Three of the fourteen finding kinds were induced
live, including `verify_script_not_executable`; the other eleven rest on the package tests. The specification's
"draft Automaton" cannot raise `verify_script_not_executable` (the check reads running Automata only), so running and
failed Automata were seeded into the sandbox store instead; that seeding is the one step not made through REST.
