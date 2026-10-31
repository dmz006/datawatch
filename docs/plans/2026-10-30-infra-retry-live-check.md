# Infrastructure retry — sandbox live check

Date: 2026-10-31 · Branch: `automaton/ced90001` (tree at commit `9597e6803`)

**Sandbox daemon: `datawatch v9.47.1`** (`GET /api/health` read `"version":"9.47.1"`), built from this worktree with
`go build -o .datawatch/live-check/bin/datawatch ./cmd/datawatch/`. Host: `johnnyjohnny`, Linux 7.0.0-34-generic, tmux 3.6. It ran from its own config and
data directory under `.datawatch/live-check/` (git-ignored, deleted at the end). The production daemon, its
service unit and its ports were never touched.

Script: `scripts/infra-retry-live-check.sh`, run as `env -u TMUX -u TMUX_TMPDIR bash scripts/infra-retry-live-check.sh`. It
regenerates this report on every run. No tmux server command was run against the operator's tmux; nothing
was ended by name, only by the PID the lifecycle helper recorded.

## Results

One line per step. A step passes only when every check under it in the evidence is met.

- (a) Config round trip — Result: PASS — the defaults read back (3 tries, back-off 60/300/540 = 15 minutes, all four classes, hold, dependents and question true); all seven keys were set and read back on config and the infra-retries route; four invalid requests were refused with 400 naming the key and nothing in them was applied
- (b) A transient failure retries — Result: PASS — a real spawn failure (the worker launcher missing) was recorded on the task as `spawn_error` retries with the reason text, the task was not counted as failed, and after the launcher was created the next retry spawned a live worker session and the task left the retry loop; the worker is a stand-in, so the task was not run to completion (see the limits)
- (c) Repeated failure holds the task — Result: PASS — with the launcher still missing the task used its 3 tries, became `review_needed` with the failure not counted, the review text, one error alert and an open question all named the `spawn_error` cause, and answering `retry` put the task back in play with a fresh round (held again because the cause persisted)
- (d) Dependents recover — Result: PASS — with exhausted_action `count`, the first task failed for an infrastructure reason, its dependent was marked `dependency-infra` with the root id, and when the first task was retried (reset, launcher present) the dependent returned to pending by itself with one `infra_dependents_recovered` decision; the first task then spawned a live worker (the stand-in does not complete, so recovery is shown at 'no longer failed', see the limits)
- (e) A non-transient failure is not retried — Result: PASS — a task refused for its file scope failed on the normal path with no infra_retries, no retry count, no row on the infra-retries route and no infrastructure decision
- (f) REST, MCP, CLI and comm agree — Result: PASS — the held task of step (c) read the same on REST, MCP and the CLI (identical JSON), and the CLI text and the comm reply carried the same status line, every retry, the held cause and the question id
- (g) Cleanup — Result: PASS — the daemon, the planner stand-in and the private tmux server were gone, no Automaton or session was left, and the leftover guard found no test daemon

**Steps:** 7 passed, 0 failed.

## Evidence

Scope: `GET /api/autonomous/infra-retries`, the `autonomous.infra_retry.*` config keys on `GET`/`PUT /api/config`, the executor retry loop, the hold after repeated failure (review text, alert, operator question), the recovery of dependent tasks, and the MCP, CLI and comm channel reads, all against a throwaway git project.

### Setup

- **Sandbox only.** Config generated from `testdata/datawatch.yaml` by the substitutions the test runner uses (data folder, loopback ports, test token), plus `autonomous.infra_retry.max_tries: 3`, `autonomous.infra_retry.backoff_seconds: [1, 1, 1]`, `autonomous.questions.enabled: true`, `server.allowed_project_roots` (one folder) and an `aider` backend entry pointing at a stand-in worker that sleeps.
- **Ports** (probed free just before use): HTTP 52989, TLS 36449, MCP 41803, DNS 34527, webhook 56549, notify 46479, planner stand-in 58483. Port 8080 and 8443 were not used.
- **Worker launcher.** A `tmux` shim first on the daemon's `PATH` pins every tmux call to a private socket and runs the launcher at `.datawatch/live-check/run/launcher/tmux`. That path does not exist at the start, so every worker spawn fails with a real error from the real spawn path until the check creates the launcher (a link to the system tmux). The operator's tmux server is not reachable from the daemon.
- **Planner stand-in.** A small HTTP server (`planner.py`, recorded by PID like the daemon) answers the daemon's planning call with a fixed one-task plan, so an Automaton can be planned without a model; the config points every model address at it (the daemon links its own model node to that address). The check replaces that plan with its own tasks.
- **Fixed-port listener.** The daemon's proxy sandbox listener has a fixed port of its own; if another process on the host holds it the daemon logs that and carries on, as it did on a host where the operator's daemon runs. It has nothing to do with this check.
- **Throwaway project.** A git repository (`.datawatch/live-check/run/projects/throwaway`, default branch `trunk`) with one commit.

Sandbox daemon pid `3289251` (recorded by the lifecycle helper) was healthy at 01:59:44; planner stand-in pid `3289241`.

### Steps

#### (a) Config round trip

`GET /api/config` → `autonomous.infra_retry` as the sandbox config set it (max_tries 3, back-off 1,1,1):
```
{"enabled":true,"max_tries":3,"backoff_seconds":[1,1,1],"categories":["spawn_error","storage_write","session_killed_before_delivery","daemon_restart"],"exhausted_action":"hold","retry_dependents":true,"raise_question":true}

```
- ok — the sandbox config values read back: max_tries 3, back-off 1,1,1
  - read: `[3,[1,1,1]]`

After `PUT /api/config` with back-off and categories set to null (unset), HTTP 200 — the defaults in effect:
```
{"enabled":true,"max_tries":3,"backoff_seconds":[60,300,540],"categories":["spawn_error","storage_write","session_killed_before_delivery","daemon_restart"],"exhausted_action":"hold","retry_dependents":true,"raise_question":true}

```
- ok — the defaults read back: enabled, 3 tries, back-off 60/300/540 (15 minutes in all), all four categories, hold, dependents true, question true
  - back-off total 900 s = 15 minutes; PUT HTTP 200
- ok — `GET /api/autonomous/infra-retries` serves the same seven values
  - read: `{"enabled":true,"max_tries":3,"backoff_seconds":[60,300,540],"categories":["spawn_error","storage_write","session_killed_before_delivery","daemon_restart"],"exhausted_action":"hold","retry_dependents":true,"raise_question":true}`

After `PUT /api/config` setting every key to a non-default value, HTTP 200:
```
{"enabled":false,"max_tries":5,"backoff_seconds":[10,20],"categories":["spawn_error"],"exhausted_action":"count","retry_dependents":false,"raise_question":false}

```
- ok — every one of the seven keys was applied and reads back (config and the infra-retries route)
  - PUT HTTP 200

Invalid values, each sent to `PUT /api/config` (HTTP status, then the refusal text):
```
mixed (max_tries 9 + exhausted_action sometimes): 400 autonomous.infra_retry.exhausted_action: "sometimes" is invalid; use "hold" or "count" 
max_tries 0: 400 autonomous.infra_retry.max_tries: 0 is invalid; use a whole number from 1 to 1000 (the default is 3) 
unknown class with max_tries 7: 400 autonomous.infra_retry.categories[1]: unknown class "not_a_class"; valid classes: spawn_error, storage_write, session_killed_before_delivery, daemon_restart 
back-off entry "soon": 400 autonomous.infra_retry.backoff_seconds[1]: soon is invalid; use a whole number of seconds 

```
- ok — a bad exhausted_action, a zero try limit, an unknown class and a non-numeric delay are each refused with 400 and the refusal names the key
- ok — nothing was applied by a refused request, including the valid keys sent beside the bad one (max_tries 9 and 7 not saved)
  - config after the four refusals is identical to before: `{"enabled":false,"max_tries":5,"backoff_seconds":[10,20],"categories":["spawn_error"],"exhausted_action":"count","retry_dependents":false,"raise_question":false}`
- ok — the short back-off (1,1,1) and 3 tries are back in force for the steps below
  - PUT HTTP 200; read: `{"enabled":true,"max_tries":3,"backoff_seconds":[1,1,1],"categories":["spawn_error","storage_write","session_killed_before_delivery","daemon_restart"],"exhausted_action":"hold","retry_dependents":true,"raise_question":true}`

#### (b) A transient failure retries

Automaton `77bc0001`, one task `8d670001`, worker backend `aider`. The worker launcher does not exist, so the spawn fails.

Task as read the moment the first retry was recorded (launcher still missing):
```
{
  "status": "in_progress",
  "wait_reason": "infrastructure retry 1 of 3 at 01:59:46 (spawn_error)",
  "error": "worker spawn failed: session start: 500 Internal Server Error — create tmux session: exit status 127",
  "infra_retry_count": 1,
  "infra_retries": [
    {
      "number": 1,
      "class": "spawn_error",
      "reason": "worker spawn failed: session start: 500 Internal Server Error — create tmux session: exit status 127",
      "at": "2026-10-31T01:59:45.046312916-04:00",
      "delay_seconds": 1,
      "next_at": "2026-10-31T01:59:46.046312916-04:00",
      "source": "executor"
    }
  ],
  "escalation_failures": null,
  "retry_count": null,
  "failure_reason": null,
  "attempts": [
    {
      "number": 1,
      "outcome": "failed",
      "failure_category": "infra",
      "failure_summary": "worker spawn failed: session start: 500 Internal Server Error — create tmux session: exit status 127"
    }
  ]
}

```

`GET /api/autonomous/infra-retries?automaton=77bc0001` at that moment, this task's row:
```
{
  "automaton_id": "77bc0001",
  "automaton_title": "Infrastructure retry live check",
  "task_id": "8d670001",
  "task_title": "First task",
  "status": "in_progress",
  "retry_count": 1,
  "max_tries": 3,
  "retries": [
    {
      "number": 1,
      "class": "spawn_error",
      "reason": "worker spawn failed: session start: 500 Internal Server Error — create tmux session: exit status 127",
      "at": "2026-10-31T01:59:45.046312916-04:00",
      "delay_seconds": 1,
      "next_at": "2026-10-31T01:59:46.046312916-04:00"
    }
  ],
  "held": false
}

```

The Automaton's `infra_retry` decisions at that moment:
```
[
  {
    "kind": "infra_retry",
    "note": "task \"First task\" infrastructure failure (spawn_error): retry 1 of 3 in 1s",
    "failure_category": "infra"
  }
]

```
- ok — a retry is recorded on the task with its class (`spawn_error`), the reason text from the failed spawn and a delay of 1 s
  - class `spawn_error`, reason `worker spawn failed: session start: 500 Internal Server Error — create tmux session: exit status 127`
- ok — the task is not counted as failed: not in status failed, no escalation failure, no failure reason
  - status `in_progress`, wait_reason `infrastructure retry 1 of 3 at 01:59:46 (spawn_error)`
- ok — the infra-retries route lists the task with its retry count and the maximum (retry n of 3), not held
- ok — the Automaton's decision log carries an `infra_retry` entry

Launcher created at 01:59:45. Task after the next retry (healthy spawn):
```
{
  "status": "verifying",
  "wait_reason": null,
  "session_id": "75b2",
  "infra_retry_count": 1,
  "infra_retries": [
    {
      "number": 1,
      "class": "spawn_error"
    }
  ],
  "escalation_failures": null,
  "failure_reason": null
}

```

The worker session as the daemon reports it, and the private tmux server's sessions:
```
{"id":"75b2","full_id":"dw-e2e-test-75b2","state":"running","backend_family":"aider","tmux_session":"cs-dw-e2e-test-75b2"}
cs-dw-e2e-test-75b2: 1 windows (created Sat Oct 31 01:59:46 2026)

```
- ok — once the launcher exists the next retry succeeds: the task has a worker session id and a live session on the stand-in backend
  - session `75b2` state `running`, backend `aider`
- ok — the task proceeded past the retry loop: status `verifying` (it left pending and the retry wait reason is gone)
- ok — the retry history stays on the task (records kept, none counted as a failure)

#### (c) Repeated failure holds the task

Automaton `e9b10001`, task `eec20001`. The launcher is missing and stays missing. 3 tries, back-off 1,1,1, exhausted action hold.

Task after the retries ran out (without spec and history):
```
{
  "status": "review_needed",
  "error": "held for operator review after 3 infrastructure retries (spawn_error)",
  "infra_retry_count": 3,
  "failure_reason": {
    "category": "infra",
    "detail": "spawn_error: worker spawn failed: session start: 500 Internal Server Error — create tmux session: exit status 127",
    "source": "infra-retry-hold",
    "retryable": false,
    "occurred_at": "2026-10-31T01:59:49.84252516-04:00"
  },
  "escalation_failures": null,
  "infra_retries": [
    {
      "number": 1,
      "class": "spawn_error",
      "delay_seconds": 1
    },
    {
      "number": 2,
      "class": "spawn_error",
      "delay_seconds": 1
    },
    {
      "number": 3,
      "class": "spawn_error",
      "delay_seconds": 1
    }
  ]
}

```

Review text (`review_summary.text`):
```
The Automaton task eec20001 (First task) was held after 3 infrastructure retries over 3s.
Infrastructure cause: spawn_error: worker spawn failed: session start: 500 Internal Server Error — create tmux session: exit status 127
The failure was not counted against the task's failed attempts.

Infrastructure retries:
- retry 1 (spawn_error) at 01:59:46, waited 1 s: worker spawn failed: session start: 500 Internal Server Error — create tmux session: exit status 127
- retry 2 (spawn_error) at 01:59:47, waited 1 s: worker spawn failed: session start: 500 Internal Server Error — create tmux session: exit status 127
- retry 3 (spawn_error) at 01:59:48, waited 1 s: worker spawn failed: session start: 500 Internal Server Error — create tmux session: exit status 127

Attempt 1
  Worker: aider/default model
  Duration: 0 s
  Outcome: failed (category infra)
  Failure: worker spawn failed: session start: 500 Internal Server Error — create tmux session: exit status 127
  Files changed: none recorded

Attempt 2
  Worker: aider/default model
  Duration: 0 s
  Outcome: failed (category infra)
  Failure: worker spawn failed: session start: 500 Internal Server Error — create tmux session: exit status 127
  Files changed: none recorded

Attempt 3
  Worker: aider/default model
  Duration: 0 s
  Outcome: failed (category infra)
  Failure: worker spawn failed: session start: 500 Internal Server Error — create tmux session: exit status 127
  Files changed: none recorded

Attempt 4
  Worker: aider/default model
  Duration: 0 s
  Outcome: failed (category infra)
  Failure: worker spawn failed: session start: 500 Internal Server Error — create tmux session: exit status 127
  Files changed: none recorded

Total time spent: 0 s across 4 recorded attempts.

```

Alert (`GET /api/alerts`, entries naming the hold):
```
[
  {
    "id": "1289e151",
    "level": "error",
    "title": "Automaton task held after infrastructure retries",
    "body": "Automaton: e9b10001 (Infrastructure retry live check)\nTask: eec20001 (First task)\nInfrastructure failure: spawn_error\nCause: worker spawn failed: session start: 500 Internal Server Error — create tmux session: exit status 127\nInfrastructure retries used: 3\nWays to act: retry the task now, which starts it with a fresh set of retries, or cancel it. Answer the operator question for the task, or resolve the task review.",
    "source": "system",
    "read": false,
    "created_at": "2026-10-31T01:59:49.851792436-04:00"
  }
]

```

Open operator question for the task (`GET /api/questions?state=open`):
```
{
  "id": "q-18e387ccb22ad4109812",
  "asked_by": "infra-retry-hold",
  "asker_kind": "system",
  "state": "open",
  "task_id": "eec20001",
  "text": "Task First task in Automaton Infrastructure retry live check was held after 3 infrastructure retries: spawn_error: worker spawn failed: session start: 500 Internal Server Error — create tmux session: exit status 127. Retry it now or cancel it?",
  "default": "retry",
  "options": [
    "retry",
    "cancel"
  ]
}

```

Decisions written with the hold:
```
[
  {
    "kind": "review_needed",
    "note": "task eec20001 held for operator review after 3 infrastructure retries: spawn_error"
  },
  {
    "kind": "infra_retry_exhausted",
    "note": "task eec20001 used up 3 infrastructure retries (spawn_error: worker spawn failed: session start: 500 Internal Server Error — create tmux session: exit status 127); the failure was not counted"
  }
]

```
- ok — after 3 tries the task is `review_needed` and its failure reason is the infrastructure hold (category infra, source infra-retry-hold)
  - retries used: 3, cause: `spawn_error: worker spawn failed: session start: 500 Internal Server Error — create tmux session: exit status 127`
- ok — the failure was not counted: no escalation failure on the task
- ok — the review text names the infrastructure cause (class and the spawn error) and lists the retries
- ok — one error-level alert, titled for the hold, names the Automaton, the task, the class and the cause
  - alerts found: 1, level error
- ok — an open operator question names the infrastructure cause and offers retry or cancel (default retry)
  - question `q-18e387ccb22ad4109812`
- ok — the infra-retries route shows the task as held, with the cause and the open question id

Answer `retry` to the question (HTTP status, then body head), and the task's status sampled every 20 ms from the answer on:
```
201 {"answer":{"id":"a-18e387ccc1bc5016d809","question_id":"q-18e387ccb22ad4109812","actor":"operator","surface":"cli","chosen_option_id":"retry","quote":"live check: retry the held task","quote_sha256":"585bc24a27e01949c8f5e4549f8aa3e3c30556f842c8b483252618fd...
01:59:50.119 review_needed
01:59:50.154 in_progress
01:59:53.332 review_needed


```

The Automaton's decision for the operator's retry:
```
[
  {
    "kind": "review_resolved",
    "actor": "operator",
    "note": "task=eec20001 retry after operator review requested by operator: no changes: answered operator question q-18e387ccb22ad4109812"
  }
]

```
- ok — answering `retry` was accepted and the task left review_needed at once: the sampled statuses start with review_needed and move on to a state that is not held
  - answer HTTP 201; pending seen: no (the executor picked the task up inside one 20 ms sample); statuses in order: review_needed in_progress review_needed 
- ok — the daemon recorded the requeue as a decision of the Automaton (retry after operator review)
- ok — the cause was still there, so a fresh round of 3 retries ran and the task is held again (history now 6 records, count 3)

#### (d) Dependents recover

Automaton `85670001`: task `89790001` first, task `8b1a0001` depends on it. `exhausted_action` is `count`; the launcher is missing.

After the first task's retries ran out and were counted — first task:
```
{
  "status": "failed",
  "error": "spawn: session start: 500 Internal Server Error — create tmux session: exit status 127\n",
  "infra_retry_count": 3,
  "failure_reason": {
    "category": "infra",
    "detail": "worker spawn failed: session start: 500 Internal Server Error — create tmux session: exit status 127",
    "source": "infra-retry-count",
    "retryable": true,
    "occurred_at": "2026-10-31T01:59:56.713704568-04:00"
  },
  "escalation_failures": 1,
  "retry_count": null
}

```

Second task (the dependent), marked with the infrastructure cause and the root:
```
{
  "status": "failed",
  "error": "dependency 89790001 failed",
  "failure_reason": {
    "category": "infra",
    "detail": "dependency 89790001 failed for an infrastructure reason (spawn_error): worker spawn failed: session start: 500 Internal Server Error — create tmux session: exit status 127",
    "source": "dependency-infra",
    "retryable": true,
    "occurred_at": "2026-10-31T01:59:56.727835963-04:00",
    "depends_on_task": "89790001"
  }
}

```
- ok — the first task failed for an infrastructure reason after its retries (class spawn_error recorded)
  - first task status `failed`, retries recorded 3
- ok — the dependent is marked: failed behind it with category infra, source dependency-infra and the root task id
  - root `89790001`

Launcher created; `reset_task` on the first task (HTTP status, then body head):
```
200 {"id":"85670001","spec":"Infrastructure retry live check: dependents-recover","title":"Infrastructure retry live check","project_dir":".datawatch/live-check/run/projects/throwaway","git":{},"backend":"aider","status":"running","created_at":"2026-10-31T

```

The dependent right after, and the decision written:
```
{
  "status": "pending",
  "error": null,
  "failure_reason": null,
  "infra_retry_count": null
}
[{"kind":"infra_dependents_recovered","actor":"autonomous","note":"dependency recovered; dependents returned to pending: root=89790001 reset=8b1a0001"}]

```
- ok — the dependent returned to pending by itself when its root was retried, with no reset of it
  - dependent status `pending`
- ok — one `infra_dependents_recovered` decision names the root and the task that was reset
  - note: `dependency recovered; dependents returned to pending: root=89790001 reset=8b1a0001`

The first task after the retry, with the launcher in place:
```
{
  "status": "verifying",
  "session_id": "80cf",
  "infra_retry_count": 3,
  "failure_reason": {
    "category": "infra",
    "detail": "worker spawn failed: session start: 500 Internal Server Error — create tmux session: exit status 127",
    "source": "infra-retry-count",
    "retryable": true,
    "occurred_at": "2026-10-31T01:59:56.713704568-04:00"
  }
}

```
- ok — the first task is no longer failed: it spawned a worker session (status `verifying`)

#### (e) A non-transient failure is not retried

Automaton `b1380001`, task `b6820001`. The task's text names a path outside the project, so the file-scope check refuses to start it (default mode enforce): a refusal about the task's scope, not a fault of the machinery. The launcher is present.

Task after the refusal (the error text is cut where its closing hint begins, marked [...]):
```
{
  "status": "failed",
  "error": "scope: task 'First task': spec line 1 references /etc/hostname outside the allowed directories [...]",
  "infra_retry_count": null,
  "infra_retries": null,
  "failure_reason": null,
  "escalation_failures": null,
  "retry_count": null
}

```

`GET /api/autonomous/infra-retries?automaton=b1380001`, tasks listed:
```
{"count":0,"tasks":[]}

```
- ok — the task failed for its scope: the error is the scope refusal
  - error: `scope: task 'First task': spec line 1 references /etc/hostname outside the allowed directories [...]`
- ok — it shows no infra_retries and no retry count
- ok — it is not on the infra-retries route and the Automaton has no infra decision
- ok — it took the normal failure path: the failure is counted (not an infrastructure hold) and no wait reason or hold is set
  - failure_reason: `null`

#### (f) REST, MCP, CLI and comm channel agree

Task `eec20001` of Automaton `e9b10001`, the task held in step (c). REST: `GET /api/autonomous/infra-retries?automaton=e9b10001`; MCP: `autonomous_infra_retries {"automaton":"e9b10001"}` over `datawatch mcp` (stdio); CLI: `datawatch autonomous infra-retries e9b10001` (text) and `--json`; comm: `POST /api/test/message {"text":"autonomous infra-retries e9b10001"}`.

REST row of the task:
```
{
  "task_id": "eec20001",
  "status": "review_needed",
  "retry_count": 3,
  "max_tries": 3,
  "held": true,
  "held_cause": "spawn_error: worker spawn failed: session start: 500 Internal Server Error — create tmux session: exit status 127",
  "question_id": "q-18e387cd803708c419b7",
  "retries": [
    {
      "number": 1,
      "class": "spawn_error"
    },
    {
      "number": 2,
      "class": "spawn_error"
    },
    {
      "number": 3,
      "class": "spawn_error"
    },
    {
      "number": 1,
      "class": "spawn_error"
    },
    {
      "number": 2,
      "class": "spawn_error"
    },
    {
      "number": 3,
      "class": "spawn_error"
    }
  ]
}

```

CLI text:
```
Infrastructure retry: on, 3 tries, back-off 1s/1s/1s, hold when used up
Automaton e9b10001 "Infrastructure retry live check", task eec20001 "First task": review_needed, retry 3 of 3
  retry 1: spawn_error: worker spawn failed: session start: 500 Internal Server Error — create tmux session: exit status 127 (2026-10-31 01:59:46)
  retry 2: spawn_error: worker spawn failed: session start: 500 Internal Server Error — create tmux session: exit status 127 (2026-10-31 01:59:47)
  retry 3: spawn_error: worker spawn failed: session start: 500 Internal Server Error — create tmux session: exit status 127 (2026-10-31 01:59:48)
  retry 1: spawn_error: worker spawn failed: session start: 500 Internal Server Error — create tmux session: exit status 127 (2026-10-31 01:59:50)
  retry 2: spawn_error: worker spawn failed: session start: 500 Internal Server Error — create tmux session: exit status 127 (2026-10-31 01:59:51)
  retry 3: spawn_error: worker spawn failed: session start: 500 Internal Server Error — create tmux session: exit status 127 (2026-10-31 01:59:52)
  held after infrastructure retries: spawn_error: worker spawn failed: session start: 500 Internal Server Error — create tmux session: exit status 127
  waiting on question q-18e387cd803708c419b7

```

Comm channel reply:
```
[dw-e2e-test] autonomous infra-retries
Infrastructure retry: on, 3 tries, back-off 1s/1s/1s, hold when used up
Automaton e9b10001 "Infrastructure retry live check", task eec20001 "First task": review_needed, retry 3 of 3
  retry 1: spawn_error: worker spawn failed: session start: 500 Internal Server Error — create tmux session: exit status 127 (2026-10-31 01:59:46)
  retry 2: spawn_error: worker spawn failed: session start: 500 Internal Server Error — create tmux session: exit status 127 (2026-10-31 01:59:47)
  retry 3: spawn_error: worker spawn failed: session start: 500 Internal Server Error — create tmux session: exit status 127 (2026-10-31 01:59:48)
  retry 1: spawn_error: worker spawn failed: session start: 500 Internal Server Error — create tmux session: exit status 127 (2026-10-31 01:59:50)
  retry 2: spawn_error: worker spawn failed: session start: 500 Internal Server Error — create tmux session: exit status 127 (2026-10-31 01:59:51)
  retry 3: spawn_error: worker spawn failed: session start: 500 Internal Server Error — create tmux session: exit status 127 (2026-10-31 01:59:52)
  held after infrastructure retries: spawn_error: worker spawn failed: session start: 500 Internal Server Error — create tmux session: exit status 127
  waiting on question q-18e387cd803708c419b7

```

MCP result (first 500 bytes):
```
{"enabled":true,"max_tries":3,"backoff_seconds":[1,1,1],"categories":["spawn_error","storage_write","session_killed_before_delivery","daemon_restart"],"exhausted_action":"hold","retry_dependents":true,"raise_question":true,"tasks":[{"automaton_id":"e9b10001","automaton_title":"Infrastructure retry live check","task_id":"eec20001","task_title":"First task","status":"review_needed","retry_count":3,"max_tries":3,"retries":[{"number":1,"class":"spawn_error","reason":"worker spawn failed: session sta

```
- ok — MCP and CLI `--json` return the same object as REST (policy, task rows, retries, held cause, question id)
  - comparison: `OK`
- ok — CLI text and the comm reply carry the same task line (status, retry n of max), every retry and the held cause and question id as REST
  - CLI stderr: 

### Cleanup

- Created through the API and removed through the API: 4 Automata (cancelled, hard-deleted) and their worker sessions. After the deletes `GET /api/autonomous/prds` listed 0 Automata and `GET /api/sessions` listed 0 sessions.
- **Sandbox daemon ended** by `td_stop_all` (recorded pid `3289251`, marker = its config path), planner stand-in ended the same way (pid `3289241`). Daemon gone: yes. Planner gone: yes. TLS port 36449 closed: yes.
- **Private tmux server**: no tmux command was run to end it; it exits by itself when its last session is gone. A session or server still on the private socket: no.
- **Leftover check** against the snapshot taken before this run: `scripts/check-no-leftover-test-daemons.sh check` exit 0 — OK: no leftover test daemons. New daemons whose config lies in this check's scratch area: 0. New daemons of other jobs on the host (config elsewhere, not started or ended by this check): 0.
- **Scratch area** (`.datawatch/live-check/`: project, config, data, log, launcher, shims, daemon binary) is removed by the exit handler right after this report is written.
- ok — the sandbox daemon, the planner stand-in and the private tmux server are gone, every created Automaton and session was removed, and no test daemon was left behind
  - daemon gone: yes; planner gone: yes; port closed: yes; private tmux left: no; leftover check exit 0 (new daemons from this check: 0, from other jobs: 0); Automata left: 0

### Defect found by this check, and fixed

Step (d) failed on the first live run. With `exhausted_action: count`, the first task failed after its retries with **no failure reason** (`failure_reason` was null). Its dependent therefore failed with the plain text `dependency <id> failed`, was not marked as an infrastructure failure, and was not reset when the first task was retried: there was no `infra_dependents_recovered` decision. The unit tests had seeded the failed first task's reason by hand, so nothing covered the path from a real spawn error.

Fix: when the retries of a worker spawn failure are used up under `count`, the task now gets a failure reason of category `infra`, source `infra-retry-count`, carrying the spawn error text (`markInfraCounted`). A regression test runs the real executor from the spawn error, on the sequential and the concurrent path, through marking, reset and completion; it fails without the change and passes with it. Files changed for it: `internal/autonomous/executor.go`, `internal/autonomous/infra_retry_hold.go`, `internal/autonomous/infra_retry_dependents_test.go`. The rows above are from the run after the fix.

### Finding not fixed

After the `reset_task` in step (d), the first task read `infra_retry_count: 3` while it was verifying, and it still carried its earlier `infra-retry-count` failure reason. The plain reset does not clear the consecutive count, so a task reset by hand that then fails again for the same reason is held at once without a fresh round of retries; the operator's retry of a held task clears the count (`reviewRequeue.apply`) and the how-to promises a fresh round only for that path. The plain reset has never cleared a failure reason of any category. Not changed: it is outside the steps of this check, and the operator decides whether a reset should also start a new incident.

The scope refusal that step (e) provokes ends in a hint that uses the old internal term for an Automaton in two messages of `internal/autonomous/scope.go`. The report cuts the text before that hint. Not changed: it belongs to the scope check, not to this feature.

## What is not shown live

- **No task ran to completion.** The worker is a stand-in that sleeps, and the sandbox has no model to verify work. Steps (b) and (d) therefore show the retry reaching a healthy spawn (a live worker session, the task past the retry loop and out of the failed state) and not a finished task. That a successful verification ends the incident and zeroes the consecutive retry count is covered by the unit tests in `internal/autonomous/infra_retry_executor_test.go`, not shown here.
- **The spawn failure is made by a missing worker launcher**, not by a missing backend binary: the daemon starts a session even when the backend's binary does not exist (the backend fails later, inside the pane), and only the launcher call can fail the spawn. The failure is real and comes from the real spawn path, but it is one cause of one class (`spawn_error`).
- **The other three classes are not induced live.** A full disk or quota (`storage_write`), a worker session killed before its task arrives (`session_killed_before_delivery`) and a daemon restart under a running task (`daemon_restart`) were not made to happen in the sandbox. Their classification and their retry handling are covered by `internal/autonomous/infra_failure_test.go` and `internal/autonomous/infra_retry_executor_test.go`.
- **Back-off was 1 second.** The default delays (1, 5 and 9 minutes) are shown as the configured default in step (a) only; no retry waited that long here. The half-delay cap on a restart-caused retry is not shown.
- **The `count` action** is shown once, in step (d), for the dependent chain. Its effect on the ladder of workers (the failure is booked as one ordinary failed attempt) is not read back in detail.
- **Web app, mobile app and the config surfaces other than REST** (the PWA card and badges, `config set` on the CLI, `configure` on comm, `autonomous_config_set` on MCP) were not driven; their rows in the testing tracker are not changed by this check.
- **The comm read** went through `POST /api/test/message` on the sandbox daemon with `server.token` set (the helper authenticated), so it is the router's own verb that ran. A real chat channel (Signal, Matrix and the others) was not connected.


Result: PASS
