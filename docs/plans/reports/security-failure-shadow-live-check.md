# Security failure report — sandbox live check (shadow mode, then on mode)

Run on 2026-10-30, 02:15–02:35 local, from the `f1f20052` worktree. A sandbox daemon, a throwaway
repository and a headless browser were used. Every command ran under `env -u TMUX -u TMUX_TMPDIR`; no
tmux command was run and the sandbox daemon started no session (0 sessions at the end).

**Verdict in one paragraph.** The feature does what the how-to says in both modes: in shadow it stores
the report, records the would-do stop and stops and alerts nothing; in `on` it blocks the linked
Automaton, copies the report into its triage record and raises exactly one alert; all five read surfaces
agree; and no security run on GitHub was rerun, cancelled or deleted. The check does **not** pass as
specified, for two reasons: once the daemon had saved its config and reloaded it, the unset
`security_workflows` default had become an explicit empty list, which silently switched the feature off
(the `on` phase only worked after an out-of-spec workaround, see Result), and the fixtures could not be
created through the API as the task asked (no such API exists, see Setup).

## Setup

### Sandbox daemon

| Item | Value |
|---|---|
| Binary | `go build -o tmp-sf/bin/datawatch ./cmd/datawatch/` from this worktree (v9.38.0) |
| Config | `testdata/datawatch.yaml` copied to `tmp-sf/config.yaml` and edited by script: `data_dir` under `tmp-sf/data`, loopback only, TLS on, every outward integration (ollama, memory, council, webhook, DNS, ntfy, web search, goose) disabled, `llms` and `compute_nodes` emptied, `server.token` empty (as in `testdata`) |
| Ports | TLS **18543**, plain 18580 (loopback). The standard sandbox TLS port **18443 was already held** by an unrelated listener (also 19090, 18080, 19080 and 19053), so it was left alone and a free port was used. The production ports 8443 and 8080 were never used. |
| Start/stop | `scripts/test-daemon-lib.sh` (`td_start`, `td_stop_one`, `td_wait_port_closed`) through `tmp-sf/dctl.sh` |
| PATH of the daemon | `tmp-sf/shimbin` first: a sandbox-only `gh` double (see below). Nothing else was changed in any environment. |
| Daemon PIDs (all recorded, each ended by that PID) | `3002665` (fixture creation, 02:20:38–02:21:26), `3011571` (02:21:35–02:23:26), `3030311` (02:23:30–02:34:36, the daemon of every result below) |

No `td_install_traps` was used: the stages were separate shell calls, and the lib's EXIT trap ends the
daemon when the starting shell exits. Every stop was explicit, by the recorded PID, and the last one is in
Cleanup. Had the session died mid-run nothing would have ended the daemon; it did not.

### Fixtures, and where the task could not be followed literally

The task asks for every fixture to be created through the API. Two of them cannot be:

* **Repo CI watch record.** The only route is `GET /api/autonomous/repo-ci-watch`; `DELETE` on it is
  `405`. Records are written only by the coordinator's poll (`git fetch`, `gh run list`).
* **An Automaton whose last post-verify SHA is the commit.** `POST /api/autonomous/prds` creates a draft,
  and a draft refuses stories and tasks: `prd "ed6e0001" status "draft" is locked; only needs_review /
  revisions_asked / cancelled accept structural edits`. Reaching `needs_review` needs a decompose (an
  LLM), and `post_verify_sha` is written only by the executor after a real task session, which would
  start tmux sessions, which this check must not do. No route sets it.

What was done instead, smallest departure first:

1. **Throwaway ref and commits** in a local repository `tmp-sf/repo` (created by `tmp-sf/setup.sh`): tag
   `v0.0.1` at commit `e00e3075…` (A), tag `v0.0.2` at commit `30b92ffb…` (B, created only for the `on`
   phase), an `origin/main` ref at a third commit. Its origin is `git@github.com:sandbox-throwaway/securityfailure.git`
   with `core.sshCommand=/bin/false`, so a fetch cannot leave the machine (it fails, which the poll
   tolerates). The repo carries a readable, empty `security/accepted-risks.yml`.
2. **Watch records** are created by the daemon's own poll of that repo. The repo CI watch was configured
   through the API (`PUT /api/config`: mode `shadow`, the repo, `poll_seconds` 60, `tag_count` 1).
3. **`gh` double** (`tmp-sf/shimbin/gh`, first on the sandbox daemon's PATH only). It answers exactly the
   three read calls the feature makes, from fixture files (a failing `CodeQL` run per throwaway commit,
   its failed log naming a dependency and a CVE, and the advisory), refuses every other call and logs all
   of them. The sandbox daemon therefore never reached GitHub. The runs, logs and advisories are
   made-up: `example.com/sandbox/vulnmod-a` with `CVE-2026-90042`, and `…/vulnmod-b` with `CVE-2026-90077`
   and `fixed version: 1.4.2`.
4. **Two Automata** were created through the API (`POST /api/autonomous/prds`, ids `ed6e0001` for A and
   `efb60001` for B, `project_dir` the throwaway repo). The daemon was then stopped and their stored
   files were patched with `jq` to `status: completed` with one completed task each carrying
   `post_verify_sha` A or B, and the daemon was started again. **This is the one out-of-band write.**
5. **Two records, not one.** The pass assesses each run once, shadow included (a record that carries a
   report is skipped, per the how-to and `security_failure_pass.go`), so the same record cannot be
   assessed again after the switch to `on`. Shadow therefore used run 9000001 / Automaton A and `on` used
   run 9000002 / Automaton B.

### Baseline (daemon 3011571, before the first poll)

```
automaton A: completed        automaton B: completed
alerts: 0                     security-failures: []
GET /api/config → autonomous.automation.security_failure = {"mode":"shadow"}   (the unset default)
repo_ci_watch security_workflows (effective) = [CodeQL, dependency-review, owasp-zap, secret-scan, security-scan]
```

### GitHub run list, before

```
env -u TMUX -u TMUX_TMPDIR gh run list --repo dmz006/datawatch --limit 100 --json databaseId,name,workflowName,status,conclusion,headSha,headBranch,event,createdAt,updatedAt
→ 100 runs, 9 with conclusion failure   (taken 02:20:26)
```

## Shadow mode

Mode `shadow` (the unset default, confirmed effective). First poll at 02:22:34, 49 s after the watch was
configured (02:21:45).

```
GET /api/autonomous/security-failures        (full body, abridged to the fields that matter)
[{ "repo":"sandbox-throwaway/securityfailure","ref_kind":"tag","ref_name":"v0.0.1","workflow":"CodeQL",
   "run_id":"9000001","status":"failing",
   "report":{ "dependency":"example.com/sandbox/vulnmod-a","advisory_ids":["CVE-2026-90042"],
     "sha":"e00e307597bea03949189be1e3333b91f3986818","published_at":"2026-10-27T09:00:00Z",
     "fixed_version":"","registry_found":false,"registry_error":"","lookup_error":"",
     "recommendation":"keep_no_entry","mode":"shadow","action":"would_stop" } }]
```

The would-do line stored on the watch record (`GET /api/autonomous/repo-ci-watch`, record for run 9000001):

```
"would_do":[{"action":"stop","run_id":"9000001",
  "reason":"security failure: would stop Automaton ed6e0001 (security failure in example.com/sandbox/vulnmod-a (CodeQL): advisory CVE-2026-90042, recommendation keep_no_entry, run https://github.com/sandbox-throwaway/securityfailure/actions/runs/9000001)"}],
 "rerun_log":null, "alerted_at":null
```

**Supervisor's own action for the same failure: none recorded.** The fixture Automaton `ed6e0001` has
0 decisions, no triage record and no notes, and the sandbox runs no supervisor.

| Check in shadow | Observed |
|---|---|
| Would-do stop recorded | yes, one `stop` entry naming Automaton `ed6e0001`, above |
| An Automaton blocked | **no**: `ed6e0001` and `efb60001` both `completed`, `blocked_reason` null |
| An alert raised | **no**: `GET /api/alerts` → 0 |
| A triage record written | no: `GET /api/autonomous/triage` → `{"records":[],"alert_minutes":30}` |
| Assessed once | across a daemon restart and every later poll (the `gh` double logged a poll every minute or two) the failed log was read once and the advisory once; the would-do list still had one entry |
| Report survives a restart | yes: after the 02:23 restart the same report was returned and the `gh` double saw no new log or advisory read |

## On mode

Only the sandbox daemon's mode was changed, at 02:24:39 through the API: tag `v0.0.2` created at commit B
in the throwaway repo, then

```
PUT /api/config {"autonomous.automation.security_failure.mode":"on"}   → {"status":"ok"}
GET /api/config → "security_failure":{"mode":"on"}
```

**The expected assessment of run 9000002 did not happen for the next 6 minutes** (02:24:39–02:30:35):
the first poll after the tag existed (02:25:30, the `gh` double logged a run list for commit B) created the
record, but `GET /api/autonomous/security-failures` still returned only the shadow report, Automaton B
stayed `completed`, alerts stayed 0. Read back at 02:30, both records were classified `code_or_test`, not
`security` (even run 9000001's record, which had been `security` at 02:22). The cause is in the Result
section. `PUT /api/config {"autonomous.automation.repo_ci_watch.security_workflows":
["CodeQL","dependency-review","owasp-zap","secret-scan","security-scan"]}` at 02:31:18 (the documented
default, set explicitly, sandbox only) made the next tick assess it at 02:31:30.

Observed after that, and unchanged through two more polls (02:32:30, 02:33:30):

| Check in on mode | Observed |
|---|---|
| Linked Automaton blocked | yes: `efb60001` → `status: blocked`, `blocked_reason: "security failure in example.com/sandbox/vulnmod-b (CodeQL): advisory CVE-2026-90077, recommendation upgrade, run https://github.com/sandbox-throwaway/securityfailure/actions/runs/9000002"` |
| Report action | `stopped`, mode `on`, recommendation `upgrade`, `fixed_version` `1.4.2` |
| Report copied into the triage record | yes: `GET /api/autonomous/triage` → one record, `prd_id` `efb60001`, `status` `blocked`, `alerted_at` 02:31:30; its `security` object is **byte-identical** to the stored report of run 9000002 (compared with `jq -S`) |
| Exactly one report alert | yes: `GET /api/alerts` → 1 (level `warn`), still 1 after two further polls and after the mode was restored |
| The shadow-assessed run | not reassessed: Automaton `ed6e0001` stayed `completed`, its report stayed `would_stop`, no second alert |
| Triage alert duplicate | none (the triage record is stamped alerted, `triage_alert_minutes` did not alert again) |

The alert, in full:

```
title: Automaton "efb60001" stopped: security failure in example.com/sandbox/vulnmod-b
body:  Automaton "efb60001" was stopped (blocked) because a security run failed on its commit 30b92ffb394b9e15b9b11b4dc0c553a8df4e39e3.
       Workflow: CodeQL
       Run: https://github.com/sandbox-throwaway/securityfailure/actions/runs/9000002
       Dependency: example.com/sandbox/vulnmod-b
       Advisory: CVE-2026-90077
       Published: 2026-10-28T09:00:00Z
       Registry entry: none
       Fixed version: 1.4.2
       Recommendation: upgrade
```

Supervisor's own action for run 9000002: **none recorded** (`efb60001` carried no decisions before the stop).

Mode restored at 02:33:45, sandbox only:

```
PUT /api/config {"autonomous.automation.security_failure.mode":"shadow"} → {"status":"ok"}
GET /api/config → "security_failure":{"mode":"shadow"}
```

### The GitHub run list, before and after

The sandbox daemon itself could not touch GitHub (the `gh` double). The `gh` double's log is the direct
evidence for the daemon; the real list below is the check the task asked for.

```
gh double, whole run: 22 calls — 18 `run list`, 2 `run view … --log-failed`, 2 `api advisories?cve_id=…`
                      refused: 0; rerun / cancel / delete / any non-GET: 0; only repo named: sandbox-throwaway/securityfailure

gh run list --repo dmz006/datawatch --limit 100 …   taken 02:20:26 (before) and 02:34:01 (after)
before: 100 runs        after: 100 runs        new runs since before: 0
runs missing from the after list: 0 (none newer than the window's oldest run, none pushed out)
runs in a security workflow (CodeQL, dependency-review, owasp-zap, secret-scan, security-scan): 37
  of those, status or conclusion changed: 0       any run that became "cancelled": 0
```

No security run was rerun, cancelled or deleted.

## Surfaces

All five read the same stored reports; none writes. Shadow (daemon 3030311, 02:23:39) first, then on
(02:31:51). The sandbox daemon has no server token (as in `testdata`); the comm verb needs that, see Result.

### REST

```
curl -sk https://127.0.0.1:18543/api/autonomous/security-failures
```
Shadow: the report above (`status` failing, `report.mode` shadow, `action` would_stop).
On: two entries, newest first —
`{"run_id":"9000002","ref":"v0.0.2","status":"failing","mode":"on","action":"stopped","rec":"upgrade","fixed":"1.4.2"}`,
`{"run_id":"9000001","ref":"v0.0.1","status":"failing","mode":"shadow","action":"would_stop","rec":"keep_no_entry","fixed":""}`.
Observed: matches the report, the watch record and the triage copy.

### MCP

```
curl -sk -X POST https://127.0.0.1:18543/api/mcp/call -d '{"tool":"security_failure_list","params":{}}'
```
Shadow: `{"content":[{"text":"[{\"repo\":\"sandbox-throwaway/securityfailure\",\"ref_kind\":\"tag\",\"ref_name\":\"v0.0.1\", … \"report\":{… \"recommendation\":\"keep_no_entry\",\"mode\":\"shadow\",\"action\":\"would_stop\" …}}]","type":"text"}]}`
On: the same list as REST (`9000002 stopped`, `9000001 would_stop`).
Observed: the tool's text parses to exactly the REST body.

### CLI

```
DW_BASE_INSECURE=1 tmp-sf/bin/datawatch autonomous security-failure -u https://127.0.0.1:18543
```
Shadow:
```
REPOSITORY                         REF         WORKFLOW  RUN      STATUS   DEPENDENCY                     ADVISORY        RECOMMENDATION  ACTION
sandbox-throwaway/securityfailure  tag/v0.0.1  CodeQL    9000001  failing  example.com/sandbox/vulnmod-a  CVE-2026-90042  keep_no_entry   would_stop
```
On: the same header, then `tag/v0.0.2 … 9000002 … CVE-2026-90077  upgrade  stopped`, then the `tag/v0.0.1 … would_stop` row.
Observed: rows agree with REST; `--json` printed the raw body (`[{"run_id":"9000001","action":"would_stop"}]` for the fields compared).

### Comm verb

```
curl -sk -X POST https://127.0.0.1:18543/api/test/message -d '{"text":"autonomous security-failure 9000001"}'
```
Shadow, with a run id:
```
[dw-sf-live-check] autonomous security-failure
Security failure report, run 9000001
Repository: sandbox-throwaway/securityfailure
Ref: tag/v0.0.1
Workflow: CodeQL
Run: https://github.com/sandbox-throwaway/securityfailure/actions/runs/9000001
Watch status: failing
Dependency: example.com/sandbox/vulnmod-a
Advisory: CVE-2026-90042
Published: 2026-10-27T09:00:00Z
Registry entry: none
Recommendation: keep_no_entry
Mode: shadow
Would have taken (shadow mode): stop the Automaton line
```
With no run id it replied with the same table as the CLI; `autonomous security_failure 123` (the alias, an
unknown run) replied `No security failure report found for run 123.` On, run 9000002 ended with `Fixed
version: 1.4.2` … `Mode: on` and `Action taken: stopped the Automaton line`.
Observed: correct in both modes. Note the verb answered `HTTP 401: unauthorized` while the sandbox had a
server token set (see Result), which is why the daemon was restarted without one.

### PWA card

Headless Google Chrome (profile under `tmp-sf/chrome`, remote-debugging 19333) loaded
`https://127.0.0.1:18543/`, called the app's own `navigate('automation')` (Automata → Automation → Chains)
and read `#automationSecurityFailureCard` (state: view `autonomous`, tab `automation`, subtab `chains`).
The card holds 0 buttons, inputs, forms or handlers. Shadow (screenshot taken and read):
```
Security failure
sandbox-throwaway/securityfailure · tag/v0.0.1 · CodeQL · Run 9000001
Dependency: example.com/sandbox/vulnmod-a
Advisory: CVE-2026-90042
Published: 2026-10-27T09:00:00Z
Registry entry: not in the registry
Recommendation: keep, but add a registry entry
Mode: shadow
Would have taken (shadow mode): stop the Automaton line
```
On: two cards. The first is run 9000002 with `Fixed version: 1.4.2`, `Recommendation: upgrade to the fixed
version`, `Mode: on`, `Action taken: stopped the Automaton line`; the second is the shadow card above.
Observed: the card matches the REST body in both modes and is read-only. The mobile app's render, and a
second locale, were not run.

## Cleanup

Everything the check created was removed, in this order, 02:34:13–02:35:30:

| Fixture | How removed | Result |
|---|---|---|
| Automata `ed6e0001`, `efb60001` | `DELETE /api/autonomous/prds/<id>?hard=true` (a bare `DELETE` only sets `cancelled`) | `status: deleted`, `killed_session_ids: []`; list empty; the stopped Automaton's triage record went with it (0 left) |
| Watch records for runs 9000001 and 9000002 | **no API removes them** (`DELETE /api/autonomous/repo-ci-watch` → 405); they lived in the sandbox data directory `tmp-sf/data/autonomous/` | removed with the directory |
| Refs: tags `v0.0.1`, `v0.0.2`, commits A, B | local refs in the throwaway repository `tmp-sf/repo` | removed with the repository |
| The one alert, the stored reports, the sandbox config | sandbox data directory | removed with `tmp-sf/` |
| Headless Chrome | stopped by its recorded PID `3038258` (its `--user-data-dir` checked first) | down |
| Sandbox daemon | `td_stop_one 3030311` (the recorded PID) then `td_wait_port_closed 18543` | `pid 3030311 is down; port 18543 closed` |

After the stops: no listener on 18543, 18580 or 19333; `kill -0 3030311` fails. Unrelated daemons that
were running at the start were all still running (550763, 2808557, 2862890, 2939542, 2977150, 2982703,
3008689, 3580116, 3816590). The production daemon `3511620` (`/usr/local/bin/datawatch --config
/etc/datawatch/config.yaml`, started Tue Oct 20 18:19:20) is still running with the same start time; a
`kill -0` probe first reported it missing only because it is root-owned (permission denied), and `ps`
confirmed it. The only processes ended by this check are the three recorded sandbox daemon PIDs and
Chrome `3038258`. Production config and every other daemon were not touched, and no `tmux` command was
run. The scratch directory `tmp-sf/` was deleted last, after this report was written.

## Result

### What failed, and the failing command

**Failing command:** `GET /api/autonomous/security-failures`, waited on (expecting the report of run
9000002) after `PUT /api/config {"autonomous.automation.security_failure.mode":"on"}`. It returned only
the shadow report for 6 minutes (02:24:39–02:30:35) and the linked Automaton was not stopped, until
`autonomous.automation.repo_ci_watch.security_workflows` was set explicitly by `PUT /api/config`.

**Cause, confirmed.** `security_workflows` is declared `yaml:"security_workflows"` with no `omitempty`
(`internal/config/config.go:3132`, `internal/autonomous/repo_ci_watch_config.go:92`), and the default
applies only to a `nil` list. A round trip with the repository's own types:

```
before save: SecurityWorkflows nil=true  effective=[CodeQL dependency-review owasp-zap secret-scan security-scan]
saved yaml:  … security_workflows: []
after load:  SecurityWorkflows nil=false effective=[]
```

In the sandbox, by 02:30 the daemon had written `security_workflows: []` into `config.yaml` itself (line
331; the check never wrote that key, and its only edit of the file was loading and dumping it to clear
the token fields) and was running on it: no run was classified `security` any more (both records read
`code_or_test`), so the repo CI watch stopped feeding the security failure pass, silently, in every mode.
The exact save that wrote it was not pinned down: the daemon was restarted once at 02:23, between the
first poll (still `security`) and the first time the problem was seen. What is shown is how a saved
default becomes "none" (the round trip above) and that it happened here. A daemon that saves its config
and is then restarted can lose the feature the same way, until an operator sets the list by hand. This
sits in the repo CI watch's config handling, which this feature depends on; it was found, not fixed.

### Other findings (not fixed, reported)

1. **Comm verbs cannot call the daemon when a server token is set.** `commGet` (`internal/router/sx2_parity.go:45`)
   sends no `Authorization` header, so `autonomous security-failure`, and the older `autonomous triage`,
   `repo-ci-watch` and `status`, all answer `HTTP 401: unauthorized` on a token-protected daemon. The live
   check restarted the sandbox with an empty token (as `testdata/datawatch.yaml` has) for this reason.
2. **A run assessed in shadow is never assessed in `on`** (documented, but it means switching modes
   does not stop an Automaton for a failure already reported in shadow; only new failing runs are acted on).
3. **No API to create the fixtures the task names** (see Setup); a repeatable live check needs either a
   record and post-verify seam for tests or a decompose-free path to a completed task.
4. The port the task names, 18443, was occupied by an unrelated process during the check.

### What passed

* Shadow: report stored, `would_stop` would-do line naming the Automaton, no Automaton blocked, no alert,
  no triage record, assessed once, survives a restart.
* `on` (after the workaround): the linked Automaton blocked with the one-line summary as its reason, the
  report byte-identical in its triage record, exactly one alert, no repeat over three polls, the mode
  restored to `shadow`.
* REST, MCP, CLI, comm verb and PWA card agree in both modes; the card is read-only.
* GitHub: 100 runs before and after, no new, missing, rerun, cancelled or deleted run, 37 security-workflow
  runs unchanged; the sandbox's `gh` double saw 22 calls, none a write.
* Cleanup: Automata deleted through the API, the sandbox daemon ended by its recorded PID and its port
  closed, other daemons and production untouched.

The check is recorded as a failure because the feature did not work after a restart without a manual
config change, and because the fixture steps the task specified could not be followed. The behaviour
when the feature is configured correctly is confirmed above.

Result: FAIL
