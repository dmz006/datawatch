# Supervisor watch — sandbox live check (shadow, then on)

Sandbox daemon: 313725, port 18710, config path tmp-sandbox/svw/config.yaml
Production daemon: not touched (PID 3511620, /usr/local/bin/datawatch with --config /etc/datawatch/config.yaml, started Tue Oct 20 18:19:20 2026, still running at the end of the check; all API traffic in this check went only to 127.0.0.1:18710, and the only process stopped was the sandbox daemon, by `td_stop_one 313725` — its recorded PID, no kill by name, no tmux server command)
Alerts raised in shadow mode: 0 (alert list empty after every shadow poll, `would_alert` set on the finding)
Alerts raised in on mode: 1 for 1 finding (still 1 after a further poll)

## What was checked

A live pass of the daemon-side supervisor watch (`autonomous.automation.supervisor_watch.*`) against a sandbox
daemon only: set shadow mode with a small alert and silence window through the sandbox REST API, ran
several coordinator polls, read the watch back over REST, the MCP tool and the CLI, confirmed the finding is
stored with the would-alert mark and that the alert sink was called zero times in shadow, switched to on and
confirmed exactly one alert for the one finding, and compared the daemon's findings against an injected
script record (one agreement, then one deliberate disagreement). Everything created was removed, the config
was restored, and the sandbox daemon was stopped by its recorded PID.

## Honest limits of this check

- Only one finding kind was produced: `main_checkout_dirty`. The other thirteen kinds need state a bare
  sandbox does not have (registered compute nodes, a configured tmux baseline, running Automata, CI records,
  worktrees, lane-fill work), and none of them was induced. They are covered by the package tests, not by
  this live check.
- `verify_script_not_executable` could not be produced live. The check inspects running Automata only
  (`SupervisorWatchRunningUnits` skips every other status), and a task's verification script has no REST
  setter — only the decompose step fills it — while a draft Automaton refuses structural edits (`prd
  "810d0001" status "draft" is locked`). I created the draft Automaton and a non-executable
  `scripts/check.sh` in its sandbox project directory; the watch correctly raised nothing for them. Getting a
  running Automaton would have meant launching a real worker session, which a sandbox check must not do.
- Because the one finding is fleet-scoped (not tied to one Automaton), the pass wrote no triage-record note.
  The triage-record write path for Automaton-scoped findings was not exercised live.
- The first poll after the shadow config landed did not set the would-alert mark: the finding was 59.99 s old
  at the second poll against a one-minute alert window. The coordinator also skipped one tick (the pass's own
  one-minute guard saw 59.999 s since the previous pass, so that tick did nothing and the next poll ran two
  minutes later). Both are the documented guard behaviour, not failures; they only shifted the poll times
  below.
- The sandbox daemon logged `proxy sandbox listener: could not bind 127.0.0.1:8444 … address already in use`
  at start-up. That listener is unrelated to this check and the sandbox simply ran without it.

## Procedure and outputs

All writes stayed under tmp-sandbox/svw/ in this worktree (removed at the end). The sandbox project directory
held a nested repository of its own (tmp-sandbox/svw/maincheckout) so nothing could be committed into the
Automaton worktree. Daemon start and stop used scripts/test-daemon-lib.sh with no traps installed in the
starting shell (an EXIT trap would have ended the daemon when the call returned); TMUX and TMUX_TMPDIR were
unset throughout.

### 1. Build and start the sandbox daemon

```
[11:10:16] go build -o tmp-sandbox/datawatch ./cmd/datawatch/      (rc 0, 3 s)
source scripts/test-daemon-lib.sh        (TD_STATE_DIR=tmp-sandbox/svw/tdstate)
td_start ./tmp-sandbox/datawatch tmp-sandbox/svw/config.yaml tmp-sandbox/svw/daemon.log
td_start rc=0 pid=313725
GET /api/health → {"auth_required":true, … "hostname":"dw-svw-sandbox", "listeners":[rest … enabled …]}
```

Sandbox config (loopback only, no TLS, REST token set, MCP listener off, session backend `shell`):
`server.host 127.0.0.1`, `server.port 18710`, `data_dir` and `session.default_project_dir` under
tmp-sandbox/svw/, `autonomous.enabled true`.

### 2. Prior config (recorded for restore)

```
GET /api/config → autonomous.automation.supervisor_watch:
{"alert_minutes":30,"main_checkout":"","mode":"shadow","poll_seconds":60,"script_output_path":"",
 "silent_minutes":60,"tmux_restart_baseline":"","working_model":""}
```

(`mode` reads `shadow` with no value configured: an empty mode is shadow.)

### 3. Configure shadow mode, inject what the check needs

```
PUT /api/config {
  "autonomous.automation.supervisor_watch.mode": "shadow",
  "autonomous.automation.supervisor_watch.alert_minutes": 1,
  "autonomous.automation.supervisor_watch.silent_minutes": 1,
  "autonomous.automation.supervisor_watch.script_output_path": "tmp-sandbox/svw/script-record.json",
  "autonomous.automation.supervisor_watch.main_checkout": "tmp-sandbox/svw/maincheckout"
} → {"status":"ok"}
```

Injected inline:

- A tracked file modified in the nested repository tmp-sandbox/svw/maincheckout (`git status --short` → ` M tracked.txt`)
  so the daemon-side `main_checkout_dirty` check has a real finding to raise.
- A script-output file in the format scripts/supervisor-watch.py writes under `--record`
  (`{"at": <unix seconds>, "findings": [{"kind", "subject", "text"}]}`). For the agreement it lists exactly
  `main_checkout_dirty` / `tmp-sandbox/svw/maincheckout`. For the deliberate disagreement it was rewritten
  (fresh `at`) with one extra entry, `ci_red` / `injected-disagreement`, that the daemon cannot raise.
- A draft Automaton (`810d0001`, created with `POST /api/autonomous/prds`, project directory
  tmp-sandbox/svw/project/wt) and a non-executable tmp-sandbox/svw/project/wt/scripts/check.sh (mode 0644).
  See the limits above: no finding for it.

### 4. Polls in shadow mode

| poll | time | what happened |
|---|---|---|
| 1 | 11:11:36 | `main_checkout_dirty` stored, `would_alert` not set (finding just opened); comparison recorded: **agreed** |
| 2 | 11:12:36 | finding 59.99 s old, below the 1-minute window, no mark; comparison recorded with the extra script entry: **disagreed** |
| tick | 11:13:36 | skipped by the pass's one-minute guard (59.999 s since the previous pass) |
| 3 | 11:14:36 | `would_alert: true`, `would_alert_at: 2026-10-30T11:14:36`, `alerted_at` null; comparison: **disagreed** |

```
GET /api/alerts after poll 1 → {"alerts":[],"unread_count":0}
GET /api/alerts after poll 3 → {"alerts":[],"unread_count":0}     (WouldAlert set, alert sink called 0 times)
```

### 5. Read the findings back — REST, MCP tool, CLI (all three agree)

```
REST: GET /api/autonomous/supervisor-watch
{"mode":"shadow","last_poll_at":"2026-10-30T11:14:36.021109296-04:00",
 "findings":[{"kind":"main_checkout_dirty","subject":"tmp-sandbox/svw/maincheckout",
   "text":"the main checkout tmp-sandbox/svw/maincheckout has uncommitted tracked files: any Automaton ship will abort until they are committed",
   "first_seen_at":"2026-10-30T11:11:36.029659707-04:00","last_seen_at":"2026-10-30T11:14:36.030155652-04:00",
   "would_alert":true,"would_alert_at":"2026-10-30T11:14:36.021109296-04:00","resolved":false}],
 "comparison":{"last":{"at":"2026-10-30T11:14:36.021109296-04:00",
   "watch_kinds":["main_checkout_dirty/tmp-sandbox/svw/maincheckout"],
   "script_kinds":["ci_red/injected-disagreement","main_checkout_dirty/tmp-sandbox/svw/maincheckout"],
   "only_script":["ci_red/injected-disagreement"],"agreed":false},
  "agreed_days":0,"only_watch":[],"only_script":["ci_red/injected-disagreement"]}}

MCP:  POST /api/mcp/call {"tool":"supervisor_watch_list","params":{}}
      → content[0].text parses to the same body (mode shadow, 1 finding, would_alert true,
        comparison.last.agreed false, only_script ["ci_red/injected-disagreement"])

CLI:  DATAWATCH_TOKEN=*** ./tmp-sandbox/datawatch autonomous supervisor-watch -u http://127.0.0.1:18710
Supervisor watch mode: shadow
Last poll: 2026-10-30 11:14:36
Findings: 1
main_checkout_dirty  would alert  2026-10-30 11:14:36  the main checkout tmp-sandbox/svw/maincheckout has uncommitted tracked files: any Automaton ship will abort until they are committed
Comparison: differs at 2026-10-30 11:14:36 (days of full agreement: 0)
  only the script raised: ci_red/injected-disagreement
```

Stored: the finding record is in the watch store (`supervisor_watch.jsonl` under the sandbox data directory)
with the would-alert mark, and the daemon log shows no alert activity in shadow.

### 6. Switch to on — exactly one alert per finding

```
PUT /api/config {"autonomous.automation.supervisor_watch.mode":"on"} → {"status":"ok"}
[11:16:36] poll → GET /api/alerts:
{"alerts":[{"id":"6a0cbd2d","level":"warn",
  "title":"supervisor watch: main_checkout_dirty — tmp-sandbox/svw/maincheckout",
  "body":"The supervisor watch (mode on) has an open finding:\nthe main checkout tmp-sandbox/svw/maincheckout has uncommitted tracked files: any Automaton ship will abort until they are committed\nopen for 4 minute(s), first seen 2026-10-30 11:11:36.",
  "source":"system","read":false,"created_at":"2026-10-30T11:16:36.030488167-04:00"}],"unread_count":1}
finding: {"mode":"on", "would_alert":true, "alerted_at":"2026-10-30T11:16:36.021028239-04:00"}
[11:17:36] next poll → alerts still 1 (titles: ["supervisor watch: main_checkout_dirty — tmp-sandbox/svw/maincheckout"])
```

One finding, one alert, no repeat on the next poll (per-episode dedupe holds).

## Comparison

The comparison history the watch store persisted (`supervisor_watch_comparisons.json` in the sandbox data
directory, read before cleanup):

```
{"at":"2026-10-30T11:11:36","agreed":true,  "only_watch":null,"only_script":null}
{"at":"2026-10-30T11:12:36","agreed":false, "only_watch":null,"only_script":["ci_red/injected-disagreement"]}
{"at":"2026-10-30T11:14:36","agreed":false, "only_watch":null,"only_script":["ci_red/injected-disagreement"]}
days: {"2026-10-30":{"total":3,"disagreed":2}}
```

- **Injected agreement** (poll 1): watch and script both raised `main_checkout_dirty/tmp-sandbox/svw/maincheckout`;
  `agreed: true`, both one-sided differences empty.
- **Deliberate disagreement** (polls 2 and 3): the script record carried one extra entry,
  `ci_red/injected-disagreement`; the comparison shows it under `only_script`, `agreed: false`, and the read
  surfaces (REST, MCP, CLI) all show the same difference. `agreed_days` is 0 because the day holds
  disagreeing comparisons.
- A stale or missing script record records no comparison at all; the script record was rewritten with a fresh
  instant before each poll window so none of the above was dropped as stale (window = 3 × poll_seconds =
  180 s).

## Restore, stop, cleanup

```
DELETE /api/autonomous/prds/810d0001 → {"id":"810d0001","killed_session_ids":[],"killed_sessions":0,"status":"cancelled"}
PUT /api/config {"…mode":"shadow","…alert_minutes":30,"…silent_minutes":60,"…script_output_path":"","…main_checkout":""} → {"status":"ok"}
GET /api/config → {"alert_minutes":30,"main_checkout":"","mode":"shadow","poll_seconds":60,"script_output_path":"",
                   "silent_minutes":60,"tmux_restart_baseline":"","working_model":""}   (identical to the prior config)
TD_STATE_DIR=tmp-sandbox/svw/tdstate td_stop_one 313725 → rc 0, pid gone, port 18710 closed
daemon log: "Shutting down... (signal=terminated)"
```

Then `rm -rf tmp-sandbox/svw` and `rm -f tmp-sandbox/datawatch` (the built binary and every file this check
created); the files already in tmp-sandbox/ from an earlier check were not touched. Production daemon 3511620
was still running afterwards with the same start time.

## Commands run

All under `env -u TMUX -u TMUX_TMPDIR` (or with both unset), each wrapped with start and end timestamps:

- `go build -o tmp-sandbox/datawatch ./cmd/datawatch/`
- `source scripts/test-daemon-lib.sh; td_start ./tmp-sandbox/datawatch tmp-sandbox/svw/config.yaml tmp-sandbox/svw/daemon.log`
- `curl` against `http://127.0.0.1:18710`: `GET /api/health`, `GET /api/config`, `PUT /api/config`,
  `GET /api/autonomous/supervisor-watch`, `GET /api/alerts`, `POST /api/autonomous/prds`,
  `DELETE /api/autonomous/prds/810d0001`, `POST /api/mcp/call` (`supervisor_watch_list`)
- `./tmp-sandbox/datawatch autonomous supervisor-watch -u http://127.0.0.1:18710`
- `git init` / `git commit` / file edit inside tmp-sandbox/svw/maincheckout only
- `td_stop_one 313725`, `td_wait_port_closed 18710`

## Verification

- .datawatch/verify/supervisor-watch-live-check.sh
- `go test -run SupervisorWatch ./internal/autonomous/ ./internal/server/ ./internal/mcp/ ./internal/config/ ./internal/router/ ./cmd/datawatch/` → ok in all 6 packages

Result: the shadow-mode behaviour (store, would-alert mark, zero alerts), the on-mode behaviour (one alert per
finding, no repeat), the comparison record (agreement and deliberate disagreement) and all three read
surfaces check out for the one finding kind induced; `verify_script_not_executable` and the triage-record
write were not reproducible live (see limits above).
