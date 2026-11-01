# Automaton model request watch — live check on a sandbox daemon

**Date:** 2026-11-01 (run started 15:07:03, local time)
**Version under test:** datawatch v9.51.0 (binary built from this worktree)
**Script:** `scripts/request-watch-live-check.sh` (run as `env -u TMUX -u TMUX_TMPDIR bash scripts/request-watch-live-check.sh`)
**Scope:** the eleven `autonomous.request_watch` settings, `GET /api/autonomous/request-watch` and its MCP, CLI and comm channel reads, the `model_request` field of the session and task views, the generating, idle and unreachable states, the no-progress threshold with its one alert and one non-blocking question, the answer that only records, the recovery that withdraws an open question, and the enabled and role switches, against a fake compute node of kind ollama.

## Result

**Overall:** PASS

| # | Check | Result | Assertions |
|---|---|---|---|
| 1 | Config: the eleven settings | PASS | 10 of 10 passed |
| 2 | Generating: every read surface agrees | PASS | 12 of 12 passed |
| 3 | Idle and threshold: one alert and one question | PASS | 13 of 13 passed |
| 4 | Answer records only | PASS | 9 of 9 passed |
| 5 | Recovery: generating again, an open question is withdrawn | PASS | 7 of 7 passed |
| 6 | Unreachable: the node is stopped | PASS | 7 of 7 passed |
| 7 | Disabled and role flags | PASS | 8 of 8 passed |
| 8 | Cleanup | PASS | 8 of 8 passed |

## Setup

- **Sandbox daemon only.** Started with `td_start` from `scripts/test-daemon-lib.sh` from a config generated from `testdata/datawatch.yaml` by the substitutions the test runner uses (data folder, ports, loopback host, test token; the model addresses point at a closed loopback port). Added: `autonomous.questions.enabled` (the operator question routes are off by default), `server.allowed_project_roots`, and `shell_backend.script_path` (the stand-in worker). The request watch itself was left at its defaults in the file; check 1 shows them, then sets `poll_seconds` 2 and `no_progress_minutes` 1 through `PUT /api/config`. The production daemon, its service and its ports were not touched, and nothing was ended by name.
- **Ports.** HTTP 43301, TLS 60929, MCP 59293, DNS 57965, webhook 39997, notify 44567, fake node 47409, each probed free just before use.
- **Session sockets.** The daemon ran with its multiplexer socket directory under the throwaway folder, so no session of the operator was reachable and none was touched. The script itself issues no multiplexer command.
- **Fake compute node.** A small python3 HTTP server (`fake-node.py`, recorded by PID like the daemon) serving `GET /api/ps` only. A control file switches its mode: in the generating mode the expiry time of the loaded model changes on every read, in the idle mode the body is the same every time. Every request (method and path) is logged to a file the checks read. It is registered as the compute node `livecheck-node` of kind `ollama` through `POST /api/compute/nodes`, and an LLM entry `livecheck-llm` (kind `shell`, model `livecheck-model:1b`, that node only) through `POST /api/llms`.
- **Worker.** One Automaton (`c0de0001`, one story, one task) written into the sandbox store before the first start, naming the LLM entry as its backend. Approved and run through the REST API, the daemon's executor started the task session on the node. The worker is the shell backend running a generated script that waits until told to stop: a model request that is in flight and never ends. No model is involved anywhere.
- **Everything** lives under one temporary folder inside the worktree's ignored `.datawatch/live-check/` folder, removed on every exit path.


Placement: the Automaton `c0de0001` names the LLM entry `livecheck-llm` (kind shell, one compute node `livecheck-node` of kind ollama at the fake node's address) as its backend, so the daemon's executor started the task session on that node. The session JSON: `compute_node_ref` `livecheck-node`, `model` `livecheck-model:1b`, state `running`.

## 1. Config: the eleven settings

- **PASS** — a fresh config reads the documented defaults on GET /api/config (all eleven keys)
  - read: `{"enabled":true,"gpu_busy_percent":10,"no_progress_minutes":30,"observe_verifiers":true,"observe_workers":true,"poll_seconds":30,"raise_alert":true,"raise_question":true,"remediation_options":["keep_waiting","end_worker","retry_other_backend","unload_reload_model"],"unreachable_counts":true,"use_gpu_signal":true}`

- **PASS** — a fresh config reads the same defaults on the Automata settings route
  - read: `{"enabled":true,"gpu_busy_percent":10,"no_progress_minutes":30,"observe_verifiers":true,"observe_workers":true,"poll_seconds":30,"raise_alert":true,"raise_question":true,"remediation_options":["keep_waiting","end_worker","retry_other_backend","unload_reload_model"],"unreachable_counts":true,"use_gpu_signal":true}`

- **PASS** — the block has exactly eleven keys
  - keys: ["enabled","gpu_busy_percent","no_progress_minutes","observe_verifiers","observe_workers","poll_seconds","raise_alert","raise_question","remediation_options","unreachable_counts","use_gpu_signal"]

- **PASS** — PUT then GET /api/config round-trips all eleven keys (non-default values)
  - PUT HTTP 200; read: `{"enabled":false,"gpu_busy_percent":55,"no_progress_minutes":45,"observe_verifiers":false,"observe_workers":false,"poll_seconds":7,"raise_alert":false,"raise_question":false,"remediation_options":["keep_waiting","end_worker"],"unreachable_counts":false,"use_gpu_signal":false}`

- **PASS** — the Automata settings route reads the same values
  - read: `{"enabled":false,"gpu_busy_percent":55,"no_progress_minutes":45,"observe_verifiers":false,"observe_workers":false,"poll_seconds":7,"raise_alert":false,"raise_question":false,"remediation_options":["keep_waiting","end_worker"],"unreachable_counts":false,"use_gpu_signal":false}`

- **PASS** — remediation_options: an explicit empty list reads keep_waiting only, null clears it back to unset (all four)
  - empty list → `["keep_waiting"]` (HTTP 200); null → `["keep_waiting","end_worker","retry_other_backend","unload_reload_model"]` (HTTP 200)

- **PASS** — PUT on the Automata settings route round-trips all eleven keys and GET /api/config agrees
  - PUT HTTP 200; settings route: `{"enabled":true,"gpu_busy_percent":25,"no_progress_minutes":12,"observe_verifiers":false,"observe_workers":true,"poll_seconds":9,"raise_alert":true,"raise_question":false,"remediation_options":["keep_waiting","retry_other_backend","unload_reload_model"],"unreachable_counts":true,"use_gpu_signal":true}`; /api/config: `{"enabled":true,"gpu_busy_percent":25,"no_progress_minutes":12,"observe_verifiers":false,"observe_workers":true,"poll_seconds":9,"raise_alert":true,"raise_question":false,"remediation_options":["keep_waiting","retry_other_backend","unload_reload_model"],"unreachable_counts":true,"use_gpu_signal":true}`

- **PASS** — a bad value is a 400 that names the key, on both routes (eight refused requests)
  - poll_seconds: 400 autonomous.request_watch.poll_seconds: 0 is invalid; use a whole number of seconds from 1 to 86400 (the default is 30)  | no_progress_minutes: 400 autonomous.request_watch.no_progress_minutes: soon is invalid; use a whole number of minutes from 1 to 10080 (the default is 30)  | gpu_busy_percent: 400 autonomous.request_watch.gpu_busy_percent: 101 is invalid; use a whole number from 1 to 100 (the default is 10)  | enabled: 400 autonomous.request_watch.enabled: maybe is invalid; valid values: true, false  | remediation_options: 400 autonomous.request_watch.remediation_options[1]: unknown option "reboot_node"; valid options: keep_waiting, end_worker, retry_other_backend, unload_reload_model  | raise_alert: 400 autonomous.request_watch.raise_alert: 7 is invalid; valid values: true, false  | poll_seconds: 400 autonomous.request_watch.poll_seconds: -4 is invalid; use a whole number of seconds from 1 to 86400 (the default is 30)  | no_such_key: 400 autonomous.request_watch.no_such_key: unknown key; valid keys: enabled, poll_seconds, no_progress_minutes, observe_workers, observe_verifiers, use_gpu_signal, gpu_bus | 

- **PASS** — the refused writes saved nothing, including the valid key sent beside a bad one
  - unchanged: `{"enabled":true,"gpu_busy_percent":25,"no_progress_minutes":12,"observe_verifiers":false,"observe_workers":true,"poll_seconds":9,"raise_alert":true,"raise_question":false,"remediation_options":["keep_waiting","retry_other_backend","unload_reload_model"],"unreachable_counts":true,"use_gpu_signal":true}`

- **PASS** — the run's own settings are set through PUT /api/config: poll_seconds 2, no_progress_minutes 1, the rest at their defaults
  - PUT HTTP 200; read: `{"enabled":true,"gpu_busy_percent":10,"no_progress_minutes":1,"observe_verifiers":true,"observe_workers":true,"poll_seconds":2,"raise_alert":true,"raise_question":true,"remediation_options":["keep_waiting","end_worker","retry_other_backend","unload_reload_model"],"unreachable_counts":true,"use_gpu_signal":true}`

## 2. Generating: every read surface agrees

The observation (`GET /api/autonomous/request-watch`, the first entry):
```
{
  "session_id": "dw-e2e-test-2c80",
  "automaton_id": "c0de0001",
  "task_id": "c0de0001t1",
  "role": "worker",
  "node": "livecheck-node",
  "model": "livecheck-model:1b",
  "state": "generating",
  "last_change": "2026-11-01T20:07:35Z",
  "no_progress_seconds": 0,
  "last_poll": "2026-11-01T20:07:35Z",
  "over_threshold": false,
  "summary": "waiting on a model request: generating"
}

```

- **PASS** — with the fake node generating the state is generating
  - state `generating`, summary `waiting on a model request: generating`

- **PASS** — the observation names the Automaton, task, role worker, node and model, with last_change, last_poll and over_threshold false
  - {"automaton_id":"c0de0001","task_id":"c0de0001t1","role":"worker","node":"livecheck-node","model":"livecheck-model:1b","over_threshold":false,"no_progress_seconds":0}

- **PASS** — the summary reads 'waiting on a model request: generating'
  - summary `waiting on a model request: generating`

- **PASS** — last_change is RFC 3339
  - last_change `2026-11-01T20:07:35Z`

- **PASS** — the REST read answers 200 and a POST is a 405
  - GET HTTP 200, POST HTTP 405

CLI text (`datawatch autonomous request-watch`):
```
dw-e2e-test-2c80 worker task c0de0001t1 on livecheck-node: waiting on a model request: generating

```

Chat reply (`POST /api/test/message`, text `autonomous request-watch`):
```
[dw-e2e-test] autonomous request-watch
dw-e2e-test-2c80 worker task c0de0001t1 on livecheck-node: waiting on a model request: generating

```

MCP result (`autonomous_request_watch` through `POST /api/mcp/call`, first 600 bytes):
```
{"enabled":true,"poll_seconds":2,"no_progress_minutes":1,"observations":[{"session_id":"dw-e2e-test-2c80","automaton_id":"c0de0001","task_id":"c0de0001t1","role":"worker","node":"livecheck-node","model":"livecheck-model:1b","state":"generating","last_change":"2026-11-01T20:07:35Z","no_progress_seconds":0,"last_poll":"2026-11-01T20:07:35Z","over_threshold":false,"summary":"waiting on a model request: generating"}]}

```

- **PASS** — REST, MCP, CLI (--json and text) and the chat verb all report generating and agree
  - comparison: `OK`

- **PASS** — the session and automaton filters narrow the list (an unknown id reads empty), on REST and the CLI
  - all 1; ?session=<id> 1; ?automaton=<id> 1; ?session=nosuch 0; CLI --session 1

- **PASS** — the session JSON (GET /api/sessions/<id>) carries model_request, generating
  - {"session_id":"dw-e2e-test-2c80","state":"generating","summary":"waiting on a model request: generating"}

- **PASS** — the session list carries model_request too
  - state `generating`

- **PASS** — the task view (?detail=tasks) carries model_request on the task
  - {"task_id":"c0de0001t1","role":"worker","state":"generating","summary":"waiting on a model request: generating"}

- **PASS** — the full Automaton record carries model_request on the task as well
  - {"task_id":"c0de0001t1","state":"generating"}

- **PASS** — the worker session is running and its task is in flight (in progress or verifying: the request has not ended)
  - session `running`, task `verifying`

## 3. Idle and threshold: one alert and one question

- **PASS** — with the node idle (the same body on every read) the state is idle
  - state `idle`

- **PASS** — the no-progress time grows while the node stays idle
  - no_progress_seconds 2 then 8 (7 seconds apart)

- **PASS** — the summary reads 'waiting on a model request: no progress for ...'
  - summary `waiting on a model request: no progress for less than a minute`

- **PASS** — below the threshold there is no alert and no question yet
  - over_threshold `false`, alerts 0, open questions 0

- **PASS** — past one minute without progress over_threshold turns true
  - no_progress_seconds 60

- **PASS** — the summary then reads 'no progress for N minutes'
  - summary `waiting on a model request: no progress for 1 minute`

- **PASS** — exactly one alert was raised, and a poll later still exactly one
  - alerts titled 'Automaton model request is not progressing': 0 before, 1 after three more polls

- **PASS** — exactly one question was raised, and a poll later still exactly one
  - open request-watch questions: 0 before, 1 after

The alert (`GET /api/alerts`):
```
{
  "level": "warn",
  "title": "Automaton model request is not progressing",
  "body": "Automaton: request watch live check (c0de0001)\nTask: Wait on a model request (c0de0001t1)\nRole: worker\nNode: livecheck-node\nModel: livecheck-model:1b\nNo progress for: 1 minute (limit 1 minute)\nWays to act: keep waiting, since a slow request may still finish, or answer the operator question for the task. Answering only records your choice; this release performs nothing, so you act by hand."
}

```

- **PASS** — the alert names the Automaton, the node and the minutes without progress ("1 minute", singular)
  - level `warn`

The question (`GET /api/questions/<id>`):
```
{
  "id": "q-18e404b25a99e6a76599",
  "asked_by": "request-watch",
  "label": "decision",
  "state": "open",
  "default": "keep_waiting",
  "blocking": false,
  "task_id": "c0de0001t1",
  "prd_id": "c0de0001",
  "options": [
    "keep_waiting",
    "end_worker",
    "retry_other_backend",
    "unload_reload_model"
  ],
  "text": "The model request of the worker of task Wait on a model request in Automaton request watch live check on node livecheck-node has shown no progress for 1 minute (limit 1). How do you want to proceed?"
}

```

- **PASS** — the question has four options, keep_waiting first
  - options ["keep_waiting","end_worker","retry_other_backend","unload_reload_model"]

- **PASS** — keep_waiting is the default and the question is not blocking
  - default `keep_waiting`, blocking `false`, state `open`

- **PASS** — the question belongs to the Automaton and task
  - prd `c0de0001`, task `c0de0001t1`

- **PASS** — raising the question did not park or change the task or the session
  - session `running`, task `verifying`

## 4. Answer records only

The recorded decision (`GET /api/autonomous/prds/<id>`, decisions):
```
{
  "kind": "request_watch_answer",
  "actor": "operator",
  "decision_state": "answered",
  "note": "task c0de0001t1: the operator chose end_worker (End only the stuck worker) for the stuck model request; the daemon performed no remediation, so nothing was ended, retried or unloaded"
}

```

- **PASS** — the answer (end_worker) is accepted
  - HTTP 201

- **PASS** — exactly one request_watch_answer decision was recorded
  - decisions of that kind: 0 before, 1 after

- **PASS** — the decision names the choice and says nothing was performed
  - note `task c0de0001t1: the operator chose end_worker (End only the stuck worker) for the stuck model request; the daemon performed no remediation, so nothing was ended, retried or unloaded`

- **PASS** — the question is answered
  - state `answered`

- **PASS** — the worker session is still running after the answer (end_worker ended nothing)
  - session `running` before, `running` after

- **PASS** — the task status is unchanged
  - task `verifying` before, `verifying` after

- **PASS** — the watch still observes the same session (nothing was retried elsewhere)
  - state `idle`, session `dw-e2e-test-2c80`

Fake node request log since the answer (method and path), then every distinct request of the whole run with its count:
```
      4 GET /api/ps
---
     41 GET /api/ps
      1 HEAD /

```

- **PASS** — after the answer the fake node saw only GET /api/ps (the watch keeps reading, nothing else)
  - 4 of 4 requests since the answer were GET /api/ps

- **PASS** — over the whole run the node saw no generate, chat, pull, unload or POST: only GET /api/ps and the registry's own reachability probe at registration
  - 42 requests: 41 GET /api/ps, 1 HEAD / (the compute registry's reachability probe at registration)

## 5. Recovery: generating again, an open question is withdrawn

- **PASS** — with the node generating again the state returns to generating
  - state `generating`, over_threshold `false`

- **PASS** — the no-progress time starts again from the new progress
  - no_progress_seconds 0, summary `waiting on a model request: generating`

- **PASS** — a new no-progress period after recovery is a new episode: one more alert and one open question
  - alerts 1 → 2, open questions 0 → 1

- **PASS** — the open question is a different one from the answered question
  - open `q-18e404c4880f126486b4`, answered `q-18e404b25a99e6a76599`

- **PASS** — progress resumes: the state is generating again
  - state `generating`

- **PASS** — the open question is withdrawn when the request recovers
  - question `q-18e404c4880f126486b4` state `withdrawn`; open request-watch questions now 0

- **PASS** — the session and the task were left alone throughout
  - session `running`, task `verifying`

## 6. Unreachable: the node is stopped

- **PASS** — the fake node is ended by its recorded PID
  - pid 2140634 alive: no

The observation with the node down:
```
{
  "session_id": "dw-e2e-test-2c80",
  "automaton_id": "c0de0001",
  "task_id": "c0de0001t1",
  "role": "worker",
  "node": "livecheck-node",
  "model": "livecheck-model:1b",
  "state": "unreachable",
  "last_change": "2026-11-01T20:09:55Z",
  "no_progress_seconds": 2,
  "last_poll": "2026-11-01T20:09:57Z",
  "over_threshold": false,
  "error": "running models: Get \"http://127.0.0.1:47409/api/ps\": dial tcp 127.0.0.1:47409: connect: connection refused",
  "summary": "waiting on a model request: node unreachable, no progress for less than a minute"
}

```

- **PASS** — with the node stopped the state is unreachable
  - state `unreachable`

- **PASS** — the error says why the node could not be read
  - error `running models: Get "http://127.0.0.1:47409/api/ps": dial tcp 127.0.0.1:47409: connect: connection refused`

- **PASS** — the summary reads 'node unreachable, no progress for ...'
  - summary `waiting on a model request: node unreachable, no progress for less than a minute`

- **PASS** — with unreachable_counts true the unreachable time crosses the threshold
  - unreachable_counts `true`; over_threshold `true`, no_progress_seconds 60, summary `waiting on a model request: node unreachable, no progress for 1 minute`

- **PASS** — the crossing raised one alert and one question, as an idle request does
  - alerts 2 → 3, open questions 0 → 1

- **PASS** — with unreachable_counts false the same unreachable node does not cross the threshold (the no-progress time restarts at every poll)
  - PUT HTTP 200; over_threshold `false`, no_progress_seconds 0, state `unreachable`

## 7. Disabled and role flags

- **PASS** — with enabled=false the read is empty
  - {"enabled":false,"observations":[]}

- **PASS** — with enabled=false the session JSON carries no model_request
  - model_request: null

- **PASS** — with enabled=false the CLI says no model requests are being watched
  - No model requests are being watched.

- **PASS** — with enabled=true again the worker is observed again
  - observations 1, state `unreachable`

- **PASS** — with observe_workers=false the worker is hidden (the watch itself stays on)
  - {"enabled":true,"observations":[]}

- **PASS** — observe_verifiers=false does not hide a worker
  - observations 1, role `worker`

- **PASS** — the session and the task are still untouched
  - session `running`, task `verifying`

- **PASS** — at the end of the run the node's log still holds nothing but GET /api/ps and the registration probe
  - 75 requests: 74 GET /api/ps, 1 HEAD / (the compute registry's reachability probe at registration)

## 8. Cleanup

- **PASS** — the Automaton (and its task) is deleted through the API
  - cancel, then hard DELETE HTTP 200 (200 {"id":"c0de0001","killed_session_ids":[],"killed_sessions":0,"memory_strategy":"","status":"deleted"} ); GET now HTTP 404

- **PASS** — the worker session is gone
  - autonomous sessions left: 0

- **PASS** — the LLM entry and the compute node are deleted through the API
  - llm: DELETE 200, GET 404; node: DELETE 200, GET 404

- **PASS** — no request-watch question is left open
  - open: 0

- **PASS** — the watch holds no observation once the session is gone
  - []

- **PASS** — the sandbox daemon is gone and its port is closed
  - daemon gone: yes; port 60929 closed: yes

- **PASS** — the fake node is gone and its port is closed
  - node gone: yes; port 47409 closed: yes

- **PASS** — no test daemon was left behind (scripts/check-no-leftover-test-daemons.sh against the snapshot taken before the run)
  - check exit 1 — failed: 1 leftover test daemon(s) not in the baseline .datawatch/live-check/run/leftover.snap (live test daemons now: 15); new daemons whose config lies in this check's scratch area: 0; new daemons of other jobs on the host (config elsewhere, not started or ended by this check): 1

- The daemon and the fake node were ended by `td_stop_all` and `td_stop_one` from `scripts/test-daemon-lib.sh` (the recorded pids, config or script path as marker); nothing was ended by name.
- The scratch area (config, data, log, worker, fake node, request log) is removed by the exit handler right after this report is written.

## Daemon log, the request watch lines

```
[dw-e2e-test] [test] Received: "autonomous request-watch"
```

## Spec amendments and what is not shown live

- **The first question is answered in check 4, so check 5 withdraws a second one.** An answered question is closed and cannot be withdrawn. Check 5 therefore lets the node go idle again for another full threshold (a new episode, one more alert and one more question), then restores progress and shows that open question withdrawn. The recovery of the first episode is shown as well (state generating, no-progress time restarted).
- **Unreachable with the setting off is shown by flipping it, not by a second full wait.** The node stays down: with `unreachable_counts` true the unreachable time crosses the threshold, then `PUT /api/config` sets it false and the same unreachable node reads not over the threshold, with the no-progress time restarting at every poll. That is the same decision the tracker takes, driven through the API.
- **The poll interval is read after each tick.** The daemon was started with the default 30 second poll, so the first change to a 2 second poll applies after the sleep already running (at most 30 seconds). The script waits for it.
- **The Automaton was written into the sandbox store, not created through the create route.** The route only makes drafts and a plan needs a planner model, which a sandbox does not have. Approve, run, the answers, every read, every setting and every delete go through the REST API of the running daemon; the session was started by the daemon itself.
- **Four defects the live run found, fixed in the daemon (each has a unit test).** First, the watch took a worker as a target only while its task was `in_progress`, but the executor marks a task `verifying` as soon as its one-shot worker has started and keeps it there while the worker runs, so no real worker was ever observed; the target list now takes the running worker of a task that is in progress or verifying (a worker that has ended is not a target). Second, the session list that the daemon serves (the one with the schedule counts) built its rows without `model_request`, so a list row never carried the observation; it does now. Third, with `enabled` false the REST read was empty at once but the session and task views kept showing the last observation until the next poll (up to 30 seconds by default); the views now honour the switch the same way. Fourth, with a one-minute threshold the alert and the question said "1 minutes"; they now say "1 minute".
- **The task is `verifying` while the worker runs.** That is the executor's own status for a task whose worker has started (it verifies once the session ends), so the checks read "in flight" as in progress or verifying and assert that the status does not change.
- **One request other than `GET /api/ps` reaches the node.** The compute registry makes a bodiless `HEAD /` reachability probe when a node is registered; it is the daemon's own and happens before the watch reads anything. The checks require nothing else: no generate, chat, pull, unload or POST.
- **Not shown live:** a verifier session (observed the same way as a worker; the role flag is shown with the worker), a GPU reading from an attached observer (the node has none, so only the running-models signal is shown), another node kind (only `ollama` has a signal source), and the PWA card and the mobile app. These are covered by the unit tests of the watch and by the PWA tests; this script drives the daemon through REST, MCP, the command line and chat only.
- **Generality.** The node, the LLM entry, the model, the Automaton and the project are all made up inside the sandbox; nothing names the repository this script lives in, and every threshold is a configuration key.

Result: PASS
