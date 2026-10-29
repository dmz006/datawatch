# Decision gate — sandbox live check

Date: 2026-10-29 · Branch: `automaton/f1f20053` (tree at commit `bf3155901`)

**Sandbox daemon: `datawatch v9.34.3`** (`GET /api/health` read `"version":"9.34.3"`), built from this worktree
with `go build -o .datawatch/live-check/bin/datawatch ./cmd/datawatch/`. It ran from its own config and data
directory under `.datawatch/live-check/` (git-ignored, deleted at the end). The production daemon and its
service unit were never touched.

This is the live check the Testing Tracker Rules ask for: a feature that gates work and persists state, run
against a running daemon rather than unit fakes. Every shell command ran under `env -u TMUX -u TMUX_TMPDIR`;
no tmux server command was run; nothing was killed by name, only by exact PID.

## Results

One line per step.

- (a) Shadow mode, gate on — Result: PASS — `run` did not hold the task (it went straight to its start attempt), the Automaton log gained one `decision_gate_shadow` entry, and the decision line read `open`.
- (b) Mode `on` — Result: PASS — the task became `waiting_decision` with its reason in `wait_reason`, and the same reason was read on REST, MCP, the CLI and comm; comm read it on the default empty `server.token` only, and with a token set comm is refused with HTTP 401 (Finding 1).
- (c) Answer with a quote — Result: PASS — the task left `waiting_decision` and reached its start attempt within 51 ms of the answer with no `run` call, and the decision line showed `answered` with the quote verbatim; the backend part (a worker session running) was NOT RUN because the sandbox has no tmux or worker backend.
- (d) Withdrawn decision and unknown id — Result: PASS — a task naming a withdrawn decision and a task naming an unknown id both became and stayed `waiting_decision`, through an answer attempt on each (409 and 404), an unrelated answer and a 36 s wait.

## Evidence

### (a) Shadow mode

`autonomous.questions.decision_gate` read `true` and `decision_gate_mode` read `shadow` (the defaults). `run` on
Automaton `696c0001` at 19:00:22.398 was followed 6 ms later by the log entry `decision_gate_shadow` with note
`would hold: operator decision q-18e32248ba3b521bad29 is waiting for an answer: Live check: use the existing table or add a new one? (task d9d80001)`
and `decision_state: open`. The task was not held. It failed at its start attempt, because the sandbox has no
tmux (below). The decision line then read `state: open`, `task_ids: ["d9d80001"]`, `waiting_task_ids: []`,
`answer: null`. A second shadow scan to test the once-per-task dedup was not done in this run.

### (b) Mode `on`

`PUT /api/config {"autonomous.questions.decision_gate_mode": "on"}` read back as `on`. After `reset_task`
the task `d9d80001` was `waiting_decision` with `wait_reason` = `operator decision q-18e32248ba3b521bad29 is waiting for an answer: Live check: use the existing table or add a new one?`.
The alert `Automaton task waiting for a decision` was raised.

| Surface | What it returned |
|---|---|
| REST | `GET /api/autonomous/decisions?prd_id=696c0001&state=open`: `waiting_task_ids ["d9d80001"]`, `waiting_tasks[0].wait_reason` identical to the reason above |
| MCP | `autonomous_decisions {"prd_id":"696c0001","state":"open"}` over stdio (`datawatch mcp`): the same object, same reason |
| CLI | `q-18e32248ba3b521bad29 open Live check: use the existing table or add a new one? \| waiting: task d9d80001 (operator decision q-18e32248ba3b521bad29 is waiting for an answer: Live check: use the existing table or add a new one?)` |
| comm | `POST /api/test/message {"text":"autonomous decisions 696c0001"}` ran the router verb; reply `[dw-live-check] autonomous decisions` plus the same line as the CLI |

The mode has to be `on` before the executor scans the task: a task that already passed the gate in shadow mode
has already started, so the failed task from (a) was put back to pending with `reset_task` after the switch.

**Finding 1 (not fixed, outside this task).** The comm loopback helper `commGet` in
`internal/router/sx2_parity.go` calls `commHTTP.Get(u)` and sends no `Authorization` header. `server.token` is
optional and empty by default, so comm worked above. After the daemon was restarted with `server.token` set
(`config-token.yaml`), the same comm read returned `[dw-live-check] autonomous decisions failed` /
`HTTP 401: unauthorized`, while REST with the bearer token worked. Every comm verb built on `commGet` is
refused by a token-secured daemon, not only this one. Step (b) is counted PASS for the default configuration and
that limit is stated in its line; it is the gate's own comm read that was observed, not a fault in the gate.

### (c) Answer releases the task

Quote sent: `Add a new table.\nKeep the old one read-only. Café — "quoted" & <b>raw</b>` (a real newline, non-ASCII
and HTML characters), through `POST /api/questions/q-18e32248ba3b521bad29/answer` (201), answer id
`a-18e3225e4c63b8918674`, actor `operator`, surface `cli`. A 20 ms poll of the task and the log's `run` count:

| Time | Task status | `run` entries in the Automaton log |
|---|---|---|
| 19:01:04.046 | `waiting_decision` | 2 |
| 19:01:05.027 | answer stored | — |
| 19:01:05.078 | `in_progress` | 2 |
| 19:01:05.119 | `failed` | 2 |

So the task left the hold and began its start attempt within 51 ms of the stored answer, with no `run` call
(the count stayed at 2). The start attempt then failed with `spawn: session start: 500 Internal Server Error — create tmux session: exit status 1`:
the sandbox has no tmux and no worker backend (a stub `tmux` first on the daemon's `PATH` answers `-V` and fails
everything else, so the check could not start a tmux server). The backend part, a released task running a
worker to completion, was therefore not run.

The decision line then read `state: answered`, `waiting_task_ids: []`, with the answer `quote` equal to the sent
text and `quote_sha256` = `8ce9693f91d1b02ee4d76169413d437084bfb8190aaaef723aac4f7ae5c78b19`, the SHA-256 of that
text. The CLI and comm replies print the same quote after `| answer a-18e3225e4c63b8918674 by operator:`. (REST JSON
escapes `&`, `<` and `>` as `&`, `<`, `>`; decoded, the strings are equal.) The first answer
attempt had a malformed JSON body (unescaped quotes) and was refused with 400, and the task stayed held until the
corrected request.

### (d) Withdrawn and unknown ids

Automaton `798e0001`, mode `on`. Task `3a6c0001` named the withdrawn decision `q-18e3226a36b0ef591a6f`, and task
`a0050001` named `q-0123456789abcdef0123`, which was never raised. Three seconds after `run`:

- `3a6c0001` `waiting_decision`: `operator decision q-18e3226a36b0ef591a6f was withdrawn without an answer`
- `a0050001` `waiting_decision`: `operator decision q-0123456789abcdef0123 was not found`

An answer attempt on the withdrawn id returned 409, one on the unknown id returned 404, and an answer on the
unrelated question `q-18e3226a37c145b56c42` (same Automaton) returned 201. After a further 36 s (past the 30 s
re-check) both tasks were unchanged. The alert count for `Automaton task waiting for a decision` was 3 (one per
held task, three tasks held in all). After the daemon restart for Finding 1 both tasks were still
`waiting_decision` with the same reasons.

## Commands run

`B=http://127.0.0.1:18710`. The daemon started with `PATH=$PWD/.datawatch/live-check/stub:$PATH` so its `tmux` is the stub. Bodies are shown as sent.

Sandbox config (`.datawatch/live-check/config.yaml`): `session.llm_backend: shell` with `auto_git_init` and `auto_git_commit` false; `server` on `127.0.0.1:18710`, plain HTTP, `token: ""`; `mcp` SSE on `127.0.0.1:18711`; `autonomous.enabled: true`, `planning_backend: ollama`; `ollama.host: http://127.0.0.1:18730`; its own `data_dir` and project dir under the scratch folder.

```bash
# setup
go build -o .datawatch/live-check/bin/datawatch ./cmd/datawatch/
python3 .datawatch/live-check/standin.py &            # planner stand-in on 127.0.0.1:18730, answers POST /api/generate with a fixed one-story plan (PID 1074756)
.datawatch/live-check/bin/datawatch start --foreground --config .datawatch/live-check/config.yaml   # PID 1074757

# channels on, through the config API
curl -s -X PUT -H "Content-Type: application/json" -d '{"autonomous.enabled": true, "autonomous.questions.enabled": true}' $B/api/config

# Automaton 1, its question, its plan
curl -s -X POST -H "Content-Type: application/json" -d '{"spec":"Live check of the decision gate: add the sessions table.","title":"dw-decision-live-check","project_dir":"<worktree>/.datawatch/live-check/proj","backend":"shell"}' $B/api/autonomous/prds          # -> 696c0001
curl -s -X POST -H "Content-Type: application/json" -d '{"label":"decision","context":"prd","prd_id":"696c0001","text":"Live check: use the existing table or add a new one?","options":[{"id":"reuse","label":"reuse the table","tradeoffs":"no migration"},{"id":"new","label":"add a new table","tradeoffs":"a migration"}],"default":"hold","asked_by":"operator","asker_kind":"operator"}' $B/api/questions   # -> q-18e32248ba3b521bad29
curl -s -X POST -H "Content-Type: application/json" -d '{"name":"datawatch-ollama","kind":"ollama","address":"http://127.0.0.1:18730"}' $B/api/compute/nodes
curl -s -X POST -H "Content-Type: application/json" -d '{}' $B/api/autonomous/prds/696c0001/decompose         # stand-in plan; the task spec names the question id
curl -s -X POST -H "Content-Type: application/json" -d '{"actor":"operator","note":"live check"}' $B/api/autonomous/prds/696c0001/plan_coverage/acknowledge
curl -s -X POST -H "Content-Type: application/json" -d '{"actor":"operator","note":"live check"}' $B/api/autonomous/prds/696c0001/approve

# (a) shadow mode, gate on
curl -s -X POST -H "Content-Type: application/json" -d '{}' $B/api/autonomous/prds/696c0001/run
curl -s "$B/api/autonomous/decisions?prd_id=696c0001"

# (b) mode on, then the reason on every surface
curl -s -X PUT -H "Content-Type: application/json" -d '{"autonomous.questions.decision_gate_mode": "on"}' $B/api/config
curl -s -X POST -H "Content-Type: application/json" -d '{"task_id":"d9d80001"}' $B/api/autonomous/prds/696c0001/reset_task
curl -s "$B/api/autonomous/decisions?prd_id=696c0001&state=open"                                               # REST
.datawatch/live-check/bin/datawatch --config .datawatch/live-check/config.yaml --url $B autonomous decisions 696c0001   # CLI
curl -s -X POST -H "Content-Type: application/json" -d '{"text":"autonomous decisions 696c0001"}' $B/api/test/message   # comm
# MCP, over stdio: these three JSON-RPC lines piped into `datawatch --config ... --url $B mcp`
{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"live-check","version":"1"}}}
{"jsonrpc":"2.0","method":"notifications/initialized"}
{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"autonomous_decisions","arguments":{"prd_id":"696c0001","state":"open"}}}

# (c) answer with a quote (body in answer.json: chosen_option_id new, the quote above, actor operator, surface cli)
python3 .datawatch/live-check/poll.py &              # prints the task status and the `run` count on every change, every 20 ms
curl -s -X POST -H "Content-Type: application/json" --data-binary @.datawatch/live-check/answer.json $B/api/questions/q-18e32248ba3b521bad29/answer
curl -s "$B/api/autonomous/decisions?prd_id=696c0001"; datawatch ... autonomous decisions 696c0001; curl ... $B/api/test/message   # answered line on REST, CLI, comm

# (d) Automaton 2
curl -s -X POST -H "Content-Type: application/json" -d '{"spec":"Live check of the decision gate, held cases.","title":"dw-decision-live-check-held","project_dir":"<worktree>/.datawatch/live-check/proj","backend":"shell"}' $B/api/autonomous/prds     # -> 798e0001
curl -s -X POST -H "Content-Type: application/json" -d '{"label":"decision","context":"prd","prd_id":"798e0001","text":"<text>","options":[{"id":"a","label":"A","tradeoffs":"x"},{"id":"b","label":"B","tradeoffs":"y"}],"default":"hold","asked_by":"operator","asker_kind":"operator"}' $B/api/questions   # twice -> q-18e3226a36b0ef591a6f (withdrawn next), q-18e3226a37c145b56c42 (unrelated)
curl -s -X POST -H "Content-Type: application/json" -d '{}' $B/api/questions/q-18e3226a36b0ef591a6f/withdraw
curl -s -X POST -H "Content-Type: application/json" -d '{}' $B/api/autonomous/prds/798e0001/decompose         # task 3a6c0001 names the withdrawn id
curl -s -X POST -H "Content-Type: application/json" -d '{"story_id":"s1","title":"Second gated task","spec":"Wire the gate. This follows operator decision q-0123456789abcdef0123.","actor":"operator"}' $B/api/autonomous/prds/798e0001/add_task
curl -s -X POST -H "Content-Type: application/json" -d '{"actor":"operator","note":"live check"}' $B/api/autonomous/prds/798e0001/plan_coverage/acknowledge
curl -s -X POST -H "Content-Type: application/json" -d '{"actor":"operator","note":"live check"}' $B/api/autonomous/prds/798e0001/approve
curl -s -X POST -H "Content-Type: application/json" -d '{}' $B/api/autonomous/prds/798e0001/run
curl -s -o /dev/null -w "HTTP %{http_code}" -X POST -H "Content-Type: application/json" -d '{"chosen_option_id":"a","quote":"too late","actor":"operator","surface":"cli"}' $B/api/questions/q-18e3226a36b0ef591a6f/answer     # 409
curl -s -o /dev/null -w "HTTP %{http_code}" -X POST -H "Content-Type: application/json" -d '{"chosen_option_id":"a","quote":"nothing","actor":"operator","surface":"cli"}' $B/api/questions/q-0123456789abcdef0123/answer         # 404
curl -s -o /dev/null -w "HTTP %{http_code}" -X POST -H "Content-Type: application/json" -d '{"chosen_option_id":"b","quote":"unrelated","actor":"operator","surface":"cli"}' $B/api/questions/q-18e3226a37c145b56c42/answer         # 201

# Finding 1: restart with server.token set, then the comm read
kill 1074757
.datawatch/live-check/bin/datawatch start --foreground --config .datawatch/live-check/config-token.yaml      # PID 1112304
curl -s -X POST -H "Authorization: Bearer <token>" -H "Content-Type: application/json" -d '{"text":"autonomous decisions 798e0001"}' $B/api/test/message   # comm: HTTP 401: unauthorized

# cleanup through the API (bearer token on each)
curl -s -X POST -H "Content-Type: application/json" -d '{"id":"dw-live-check-0be1"}' $B/api/sessions/delete   # and dw-live-check-d62e
curl -s -X POST -H "Content-Type: application/json" -d '{}' $B/api/autonomous/prds/798e0001/cancel
curl -s -X DELETE "$B/api/autonomous/prds/696c0001?hard=true"; curl -s -X DELETE "$B/api/autonomous/prds/798e0001?hard=true"
curl -s -X DELETE $B/api/compute/nodes/datawatch-ollama

# stop by exact PID
kill 1112304; kill 1074756
```

## Deviations from a clean run

These all happened in the sandbox and were cleaned up with it:

- A first question was raised with a placeholder `prd_id` before any Automaton existed, `q-18e322465362bee6490d`, and withdrawn at once. It is not used by any step.
- The first decompose returned 503 (`compute node "datawatch-ollama": compute node not found`), so the stand-in was registered as that compute node and decompose was run again.
- An Automaton `05ae0001` was created for step (d), a question call without `options` was refused with 400, and `05ae0001` was hard-deleted unused; `798e0001` replaced it.
- The first answer request in (c) was malformed and refused with 400, see (c).

## Cleanup

- Sandbox daemon stopped by exact PID: `kill 1074757` for the first start and `kill 1112304` for the token start, each confirmed gone with `kill -0`; the second was signalled only after `ps -p` showed its command line named the sandbox config. The stand-in was stopped as `kill 1074756` and confirmed gone. Ports 18710, 18711 and 18730 were confirmed closed, and `ps` listed no process running the sandbox binary or stand-in.
- Records removed through the API: both Automata (hard delete), the two failed sessions the start attempts created, and compute node `datawatch-ollama`; a final `GET /api/sessions` read `[]` and `GET /api/autonomous/prds` read `{"prds":[],"total":0}`. The four questions and the alerts have no delete call that was used; they lived in the sandbox data dir and went with it.
- The scratch folder `.datawatch/live-check/` (config, data dir, binary, stub `tmux`, stand-in, poll script, project dir) was deleted, and `git status` was clean before this report was written.
- On its first start the daemon logged `[channel] cleaned up 1 stale MCP registration(s)`: the channel bridge tidying a registration outside the project directory. The check did not read or write it.

## Not covered

The Web UI (the Decisions card, the `Waiting for a decision` label) was not driven; its rows in the tracker stay at Validated No, as do the config-key rows for MCP, CLI, comm and the Web UI settings and the config unit row. The datawatch-app screens are covered by the parity issue, not here. A released task running on a real worker backend is the NOT RUN part of step (c). Not exercised live: the gate switched off, the two-reason join, `limit` and the 400/403/503 cases of the read route, and a second shadow scan for dedup.
