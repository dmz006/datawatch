# Repo CI watch — sandbox live check (shadow mode)

Sandbox daemon: 3400327, port 18600, config path /home/dmz/.datawatch/prd-worktrees/f1f20051/tmp-sandbox/dwdata/config.yaml
Production daemon: not touched (PID 3511620, /usr/local/bin/datawatch with --config /etc/datawatch/config.yaml, started Tue Oct 20 18:19:20 2026, still running at the end of the check; all API traffic in this check went only to 127.0.0.1:18600, and the only process termination issued was `td_stop_one 3400327` — the sandbox daemon's recorded PID)
GitHub writes: 0
Would-rerun entries: 2 (would_do action "rerun" for runs 40980847035 and 40982802560; supervisor action for each: none found)

## What was checked

A live shadow-mode pass of the repo CI watch against the sandbox daemon only:
`autonomous.automation.repo_ci_watch.{mode,repos,poll_seconds,tag_count}` set via the sandbox REST
API to `shadow` / `/home/dmz/.datawatch/prd-worktrees/f1f20051` / `60` / `2`, two real poll passes
run against the real GitHub repository `dmz006/datawatch`, the watch read back over all three
surfaces (REST, MCP tool, CLI), verified that every automatic action the watch took in shadow mode
is a would-do stand-in (rerun, cancel, delete — none executed), and confirmed the watch made zero
writable GitHub writes (`gh` rerun/cancel/delete). The sandbox config was restored to its prior
values and the sandbox daemon was stopped by its recorded PID. The production daemon was never
restarted, reconfigured or signaled; the only process terminated in this check is the sandbox
daemon 3400327.

## Procedure and outputs

All writes stayed under /home/dmz/.datawatch/prd-worktrees/f1f20051 (tmp-sandbox/). Daemon
start/stop via scripts/test-daemon-lib.sh (no traps installed in the starting shell — the EXIT
trap would have killed the daemon at call exit; stopped later with `td_stop_one <recorded PID>`).

### 1. Build + start sandbox daemon

```
[21:43:01] go build -o tmp-sandbox/datawatch ./cmd/datawatch/   (done, 3s)
env -u TMUX -u TMUX_TMPDIR td_start ./tmp-sandbox/datawatch tmp-sandbox/dwdata/config.yaml tmp-sandbox/daemon.log
STARTED pid=3400327
GET /api/health  → {"status":"ok", … "listeners":[rest 127.0.0.1:18600 up, …]}
```

(Note: a first start under `td_install_traps` was killed by the lib's own EXIT trap when the
starting shell exited — recorded PID 3400327 is the surviving daemon and the only daemon
terminated by this check.)

### 2. Prior config (recorded for restore)

```
GET /api/config → repo_ci_watch: mode=off, repos=[], poll_seconds=300, tag_count=5,
  alert_minutes=30, hung_after_minutes=60, delete_superseded=report,
  security_workflows=[CodeQL, dependency-review, owasp-zap, secret-scan, security-scan]
```

### 3. Configure shadow mode via API

```
PUT /api/config {
  "autonomous.automation.repo_ci_watch.mode": "shadow",
  "autonomous.automation.repo_ci_watch.repos": ["/home/dmz/.datawatch/prd-worktrees/f1f20051"],
  "autonomous.automation.repo_ci_watch.poll_seconds": 60,
  "autonomous.automation.repo_ci_watch.tag_count": 2
} → {"status":"ok"}

GET /api/autonomous/repo-ci-watch → {"mode":"shadow","alert_minutes":30,
  "repos":["/home/dmz/.datawatch/prd-worktrees/f1f20051"],"last_poll_at":"0001-01-01T00:00:00Z","records":[]}
```

### 4. GitHub state BEFORE the polls

```
[21:44:11] gh run list --limit 15 --json databaseId,name,headBranch,status,conclusion,createdAt,event,displayTitle
→ 15 runs, newest 40984027933 ("Release CI Summary", completed success, 2026-10-30T01:08:28Z)
  (full JSON: tmp-sandbox/gh-runs-before.json)
```

### 5. Let the polls run (≥2)

Idle tick 1 min, poll interval 60 s → two poll passes observed:

```
last_poll_at 2026-10-29T21:45:31.742  records: 29
last_poll_at 2026-10-29T21:47:31.741  records: 29
```

The 29 records: main/main runs (CodeQL green + failing/security, docs-sync, secret-scan,
security-scan, 5× Release CI Summary) and the two newest tags (tag_count=2 →
v9.36.0, v9.37.0) with their CI, CodeQL, Matrix Integration, Release,
Release CI Summary, docs-sync, secret-scan, security-scan runs. Record store file on
disk: tmp-sandbox/dwdata/autonomous/repo_ci_watch.jsonl.

### 6. Read the watch — three surfaces (all agree)

```
REST:  curl -H "Authorization: Bearer ***" http://127.0.0.1:18600/api/autonomous/repo-ci-watch
       → mode shadow, last_poll 21:47:31, 29 records  (tmp-sandbox/rest-after.json)

MCP:   curl -POST http://127.0.0.1:18600/api/mcp/call -d '{"tool":"autonomous_repo_ci_watch","params":{}}'
       → content[0].text parses to the same body: mode shadow, 29 records  (tmp-sandbox/mcp-after.json)

CLI:   DATAWATCH_TOKEN=dw-shadow-live-9917 ./tmp-sandbox/datawatch autonomous repo-ci-watch -u http://127.0.0.1:18600
       → table, 29 rows, e.g.:
         REPOSITORY        REF          WORKFLOW           STATUS   CLASSIFICATION  FIRST SEEN          ALERTED
         dmz006/datawatch  main/main    CodeQL             green    none            2026-10-29 21:44:31 no
         dmz006/datawatch  main/main    CodeQL             failing  security        2026-10-29 21:44:31 no
         … (tmp-sandbox/cli-after.txt)
```

No alerts fired (alert_minutes=30 not reached within the check window) — alerted column "no"
across all rows, as expected.

### 7. Would-do rows vs supervisor actions; GitHub write check

| would_do action | run id  | reason (shadow-mode explanation) | supervisor action for the same run |
|---|---|---|---|
| rerun | 40980847035 (tag/v9.36.0 CodeQL, infra) | "infra: the run ended by cancellation, timeout, a pre-start failure or staleness — retryable, not code" | none found |
| rerun | 40982802560 (tag/v9.37.0 CodeQL, infra) | "infra: the run ended by cancellation, timeout, a pre-start failure or staleness — retryable, not code" | none found |
| cancel | 40980849216 (tag/v9.36.0 Release, hung) | "hung: still hung past the hung_after_minutes threshold" | none found |
| delete | 40980847035 (as a superseded predecessor, seen under main/main CodeQL) | "superseded: a later green run of the same workflow on a descendant commit" | none found |
| delete | 40982802560 (as a superseded predecessor, seen under main/main CodeQL) | "superseded: a later green run of the same workflow on a descendant commit" | none found |
| delete | 40980847221 (as a superseded predecessor) | "superseded: a later green run of the same workflow on a descendant commit" | none found |
| delete | 40982802560 (as a superseded predecessor) | "superseded: a later green run of the same workflow on a descendant commit" | none found |

(Supervisor = the pipeline supervisor for the repo; the sandbox daemon ran a fresh data dir
with no Automata, PRDs or worker sessions — "boot-resume: resumed 0, parked 0" — so no
supervisor record for any of these runs exists in this sandbox. Would-do entries are
stand-ins, not writes.)

```
[21:48:23] gh run list --limit 15 → newest run still 40984027933; new runs after polls: NONE
gh run view 40980847035 → completed/cancelled (created 2026-10-30T00:37:29Z, updated unchanged — pre-existing)
gh run view 40982802560 → completed/cancelled (pre-existing, updated 2026-10-30T00:58:00Z)
gh run view 40980849216 → in_progress (still running — the shadow "cancel" was not executed)
gh run view 40980847221 → completed/failure (still present — the shadow "delete" was not executed)
rerun_log on all 29 records: [] (empty) — no automatic cancel or rerun was performed
daemon log (tmp-sandbox/daemon.log): no gh rerun/cancel/delete activity
```

`new runs after polls: NONE` + cancel target still `in_progress` + delete targets still listed +
empty `rerun_log` on every record = GitHub writes: 0.

### 8. Restore + stop

```
PUT /api/config {"…mode":"off","…repos":[],"…poll_seconds":300,"…tag_count":5} → {"status":"ok"}
GET /api/config → repo_ci_watch back to mode=off, repos=[], poll_seconds=300, tag_count=5,
  alert_minutes=30, hung_after_minutes=60, delete_superseded=report, security_workflows=<default list>
TD_STATE_DIR=tmp-sandbox/tdstate td_stop_one 3400327  → stopped; port 18600 closed (elapsed 1s)
```

No sandbox Automaton had to be created: the repo CI watch runs on the coordinator's idle tick
independent of any Automaton. Nothing in the sandbox was left running.

## Verification

- .datawatch/verify/repo-ci-watch-shadow-live-check.sh
- go test ./internal/autonomous/ ./internal/config/ → 4300 passed in 2 packages
- go test -run RepoCIWatch ./internal/server/ ./cmd/datawatch/ ./internal/mcp/ → 32 passed in 3 packages

Result: PASS
