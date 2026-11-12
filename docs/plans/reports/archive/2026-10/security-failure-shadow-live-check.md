# Security failure report — sandbox live check (shadow mode, then on mode)

Run on 2026-10-30 from the `automaton/f1f20052` worktree, after the fixes for B127, B128 and B129
(see `docs/plans/README.md`). A sandbox daemon, a throwaway repository, a `gh` double and a headless
browser were used. Every command ran under `env -u TMUX -u TMUX_TMPDIR`. No tmux command was run and the
sandbox daemon started no session. Placeholders stand for local values: `<worktree>` is the checkout,
`<sandbox-token>` the sandbox bearer token, `<tls-port>` and `<plain-port>` the sandbox listeners, and
`<sandbox-pid>` the sandbox daemon's process id. The production daemon was not touched.

**Verdict in one paragraph.** The feature behaves as the how-to says in both modes, and the three
defects found in the first run are fixed and were confirmed on this run. In shadow it stores the report,
records a would-do stop and stops and alerts nothing. In `on` it blocks the linked Automaton, copies the
report into its triage record and raises exactly one alert. The REST route, MCP tool, CLI, comm verb and
PWA card agree, the comm verb works against a token-protected daemon, the saved `security_workflows` list
survives a save and a restart, and no security run on GitHub was rerun, cancelled or deleted. The check
passes with one disclosed deviation from the task: the fixture Automata could not be driven to a completed
state through the API (no route sets `post_verify_sha`), so two stored status fields were patched while the
daemon was stopped (see Setup, step 4).

## Setup

### Sandbox daemon

| Item | Value |
|---|---|
| Binary | `go build -o tmp-sf/bin/datawatch ./cmd/datawatch/` from `<worktree>` (v9.38.0) |
| Config | `testdata/datawatch.yaml` copied and edited by script: data directory under `tmp-sf/data`, loopback only, TLS on, outward integrations disabled, `llms` and `compute_nodes` emptied |
| Server token | **set**: `<sandbox-token>` (the first run had none; see Result, finding 1) |
| Ports | TLS `<tls-port>`, plain `<plain-port>`, loopback only. The production ports were never used. |
| Start and stop | `tmp-sf/start.sh` records the PID in `tmp-sf/daemon.pid`; stopped with `SIGTERM` to that PID, then the port was checked closed |
| PATH of the daemon | `tmp-sf/shimbin` first: a sandbox-only `gh` double (see step 3). Nothing else in any environment was changed. |
| Daemon runs | `<sandbox-pid>` for the shadow and `on` checks; a second start of the same binary for the restart check and the PWA story rerun. Each was ended by its recorded PID. |

### Fixtures, and the one deviation

The task asks for every fixture to be created through the API. Most can be, but one step cannot:

1. **Throwaway repository and refs.** `tmp-sf/setup.sh` makes a local repository with tag `v0.0.1` at
   commit A, tag `v0.0.2` at commit B (created only for the `on` phase) and an `origin/main` ref. Its
   origin is a non-resolving SSH address with `core.sshCommand=/bin/false`, so a fetch cannot leave the
   machine; the poll tolerates the failed fetch. The repository carries an empty
   `security/accepted-risks.yml`.
2. **Watch records** come from the daemon's own poll of that repository. The repo CI watch was set through
   the API (`PUT /api/config`: mode `shadow`, the repository, a one-minute poll, `tag_count` 1).
3. **`gh` double** (`tmp-sf/shimbin/gh`, first on the daemon's PATH only). It answers the three read calls
   the feature makes (`run list`, `run view --log-failed`, `api advisories?cve_id=…`) from fixture files and
   refuses every other call. Its runs, logs and advisories are made up: `example.com/sandbox/vulnmod-a`
   with `CVE-2026-90042`, and `example.com/sandbox/vulnmod-b` with `CVE-2026-90077` and fixed version
   `1.4.2`.
4. **Two Automata**, created through the API (`POST /api/autonomous/prds` with a `spec`, a `title` and the
   throwaway repository as `project_dir`): `bd430001` for commit A and `bea80001` for commit B. The
   status of each was then set to `completed`, with one completed task carrying `post_verify_sha` A or B,
   by editing the stored JSON while the daemon was stopped, and the daemon was started again.
   **This is the deviation.** No route sets `post_verify_sha` or moves a draft to completed without a real
   task session, and a real session would start tmux sessions, which this check must not do.
5. **Two records, not one.** The pass assesses each run once, shadow included, so run 9000001 (commit A,
   shadow) and run 9000002 (commit B, `on`) are separate records.

## Shadow mode

Mode `shadow` (the default). The first poll read run 9000001 and stored its report.

```
GET /api/autonomous/security-failures
[{ "repo":"sandbox-throwaway/securityfailure","ref_kind":"tag","ref_name":"v0.0.1","workflow":"CodeQL",
   "run_id":"9000001","status":"failing",
   "report":{ "dependency":"example.com/sandbox/vulnmod-a","advisory_ids":["CVE-2026-90042"],
     "fixed_version":"","registry_found":false,"recommendation":"keep_no_entry",
     "mode":"shadow","action":"would_stop" } }]
```

| Check in shadow | Observed |
|---|---|
| Would-do stop recorded | yes: one `stop` entry naming Automaton `bd430001` |
| An Automaton blocked | no: `bd430001` stays `completed`, `blocked_reason` empty |
| An alert raised | no |
| A triage record written | no |
| Assessed once | yes: the `gh` double answered one failed-log read and one advisory read for run 9000001 across the polls that followed |
| Report survives a restart | yes: after the restart the same report was returned and no new log or advisory read was made |

## On mode

Only the sandbox daemon's mode was changed, through the API: tag `v0.0.2` created at commit B, then
`PUT /api/config {"autonomous.automation.security_failure.mode":"on"}`, which returned `{"status":"ok"}`.
The next poll assessed run 9000002.

| Check in on mode | Observed |
|---|---|
| Linked Automaton blocked | yes: `bea80001` is `blocked`, with reason `security failure in example.com/sandbox/vulnmod-b (CodeQL): advisory CVE-2026-90077, recommendation upgrade, run https://github.com/sandbox-throwaway/securityfailure/actions/runs/9000002` |
| Report action | `stopped`, mode `on`, recommendation `upgrade`, fixed version `1.4.2` |
| Report copied into the triage record | yes: the triage record's `security` object is byte-identical to the stored report of run 9000002 (compared with `jq -S`) |
| Exactly one report alert | yes: `GET /api/alerts` returns 1 after the poll, and still 1 after later polls and after the mode was restored |
| The shadow-assessed run | not reassessed: `bd430001` stayed `completed` and run 9000001's report stayed `would_stop` |

The alert:

```
title: Automaton "bea80001" stopped: security failure in example.com/sandbox/vulnmod-b
body:  Automaton "bea80001" was stopped (blocked) because a security run failed on its commit 30b92ffb394b9e15b9b11b4dc0c553a8df4e39e3.
       Workflow: CodeQL
       Run: https://github.com/sandbox-throwaway/securityfailure/actions/runs/9000002
       Dependency: example.com/sandbox/vulnmod-b
       Advisory: CVE-2026-90077
       Published: 2026-10-28T09:00:00Z
       Registry entry: none
       Fixed version: 1.4.2
       Recommendation: upgrade
```

The mode was then restored to `shadow` through the API.

### Restart and saved configuration

After the `on` check the daemon was stopped and started again from the same config and data directory.

```
GET /api/config → autonomous.automation.security_failure = {"mode":"shadow"}
GET /api/config → autonomous.automation.repo_ci_watch.security_workflows =
    ["CodeQL","dependency-review","owasp-zap","secret-scan","security-scan"]
GET /api/autonomous/security-failures → run 9000002 stopped/on, run 9000001 would_stop/shadow
GET /api/alerts → 1
bea80001 → blocked
```

The list was saved as written and read back intact after the restart; this is the B127 check. What this run
did not separately check is whether the boot bridge (B129) put the watch block into the Manager before the
first post-restart poll: the mode and list were read back from the config, and the stored reports were read
back from the daemon, but no poll after the restart was observed. B129 is covered by its unit test
(`cmd/datawatch/repo_ci_watch_boot_test.go`).

### GitHub run list

The sandbox daemon could not reach GitHub: the `gh` double answered its reads from fixtures. The double's
log is the direct evidence for the daemon. The real list was checked separately, before and after.

```
gh double, whole run: 30 logged calls
  24 run list, 2 run view --log-failed, 3 api advisories (one for CVE-2026-90042, two for CVE-2026-90077)
  1 run delete, refused (see Result, note on the log)
  repository named in every call: sandbox-throwaway/securityfailure

gh run list --repo dmz006/datawatch --limit 100   before the check and after it
  before: 100 runs, after: 100 runs, new runs since before: 0
  runs in a security workflow (CodeQL, dependency-review, owasp-zap, secret-scan, security-scan): unchanged
  any run that became cancelled, rerun or deleted: 0
```

No security run was rerun, cancelled or deleted.

## Surfaces

All five read the same stored reports and none writes. The check was made in shadow and in `on`, with the
token set.

### REST

`GET /api/autonomous/security-failures`. Shadow: the report above. On: two entries, newest first, run
9000002 (`stopped`, `on`, `upgrade`, fixed `1.4.2`) and run 9000001 (`would_stop`, `shadow`).
The body matches the watch record and the triage copy.

### MCP

The tool `security_failure_list`, called through the daemon's MCP surface, returns the same list as REST
in both modes.

### CLI

`datawatch autonomous security-failure` against the sandbox. Shadow: one row for run 9000001 with
`would_stop`. On: the row for run 9000002 with `upgrade` and `stopped` above it. The rows agree with REST.

### Comm verb (token-protected daemon)

`POST /api/test/message` with `autonomous security-failure` answers with the same table as the CLI, and
`autonomous security-failure <run-id>` answers with the run's report. An unknown run id answers
`No security failure report found for run <id>.` With the token set the verb answers in both modes, which
is the B128 fix. Before the fix it answered `HTTP 401` on the same daemon.

### PWA card

Headless Chrome loaded the PWA from the sandbox, navigated to Automata → Automation → Chains, and read the
security failure card. The card holds no buttons, inputs, forms or handlers, and it matches the REST body.

## PWA story (TS-839)

`scripts/test-stories/TS-839.sh` runs `scripts/test-stories/pwa/TS-839.mjs` against the sandbox. The story
fulfils the report route with three entries (shadow, on, hostile), then checks the card count, the text of
each card, that a hostile value stays inert text, that the card has no controls, that it fits at phone
width, and the empty state. It passed with exit code 0 on the final run and wrote its screenshots to the
evidence directory.

The first attempt of the story had failed (a wait timed out) and left `error.txt` and `failure.png` in the
evidence directory. Those were deleted before the final run, so the directory holds the final run's
screenshots only.

## Cleanup

| Item | How removed | Result |
|---|---|---|
| Automata `bd430001`, `bea80001` | removed with the sandbox data directory; the API hard delete was not called | gone with the directory |
| Watch records and triage records | sandbox data directory | removed with the directory |
| Refs and commits | throwaway repository under `<worktree>/tmp-sf/repo` | removed with the directory |
| Stored reports, alert, sandbox config | sandbox data directory | removed with `tmp-sf/` |
| Sandbox daemon | `SIGTERM` to its recorded PID, then port check | exited; both listeners closed |
| Headless Chrome | run by the story; none left running after it | none running |

The production daemon, which runs as another user, was not signalled and was still running at the end.
Only the sandbox daemon PIDs recorded by this check were ended. No tmux command was run.

## Result

### Findings

1. **Comm verbs failed on a token-protected daemon (B128, fixed).** `commGet` and `commJSON` sent no
   `Authorization` header, so every comm verb answered `HTTP 401`. Fixed by `SetWebToken` and `commDo`,
   wired into both the router and the test router. Confirmed on this run with the token set.
2. **Saved `security_workflows` became an empty list (B127, fixed).** A nil list was written as `[]` and read
   back as "no security workflows", which switched the pass off in every mode. Fixed by populating the
   default in `config.DefaultConfig`. Confirmed on this run: the five names survive a save and a restart.
3. **Boot path did not carry the repo CI watch block (B129, fixed).** After a restart the watch read
   `mode: off` until it was saved again. Fixed by `repoCIWatchFromYAML` in the boot bridge, with a unit
   test. This run read the mode and list back after the restart; a post-restart poll was not observed.
4. **Shadow-assessed runs are not reassessed in `on` (documented behaviour, not a defect).** Switching
   modes acts on new failing runs only. The how-to says so.
5. **Fixtures.** The Automata could not be created in the completed state through the API (see Setup,
   step 4). A repeatable live check would need a test seam for `post_verify_sha`, or a decompose-free route
   to a completed task. Not fixed here; it is a test-harness limit.

### Note on the `gh` log

The double logged one `run delete` call. The double refused it, and the daemon only issues read calls. That
line's timestamp is before the sandbox daemon's last start, so it is most likely a manual test of the
double made during setup. That attribution is not confirmed, and the report does not rely on it.

### Monitoring & Observability Rule

Met for the parts that apply: an MCP tool (`security_failure_list`) and a comm verb (finding 1). The REST
route is in `openapi.yaml`. Declared N/A with reasons for the rest: no new subsystem was added (the pass runs
inside the existing repo CI watch poll), so there are no new `SystemStats` fields, no new `/stats`
endpoint, no Monitor-tab card and no counters or gauges for Prometheus. The PWA card is on the Automation
tab, not the Monitor tab, and it reads the same stored reports as the REST route.

Result: PASS
