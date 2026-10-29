# Decision gate — sandbox live check

Date: 2026-10-29 · Branch: `automaton/f1f20053` (tree at commit `0890c7380`)

Sandbox daemon: `datawatch v9.34.3`, built from this worktree with
`go build -o <scratch>/bin/datawatch ./cmd/datawatch/`, started with
`start --foreground --config <scratch>/config.yaml`. The scratch folder was
`.datawatch/live-check/` under the project directory (git-ignored; deleted at
the end). The production daemon and its service unit were never touched.

This is the live check the Testing Tracker Rules ask for: a feature that gates
work and persists state, run against a real daemon, not unit fakes.

## Sandbox setup

| Item | Value |
|---|---|
| Daemon | own config and data dir under the scratch folder; REST `127.0.0.1:18710` (plain HTTP), MCP SSE `127.0.0.1:18711`; `session.llm_backend: shell`; `auto_git_init` and `auto_git_commit` off |
| Channels | turned on through `PUT /api/config` with `{"autonomous.enabled": true, "autonomous.questions.enabled": true}` (reply `{"status":"ok"}`); `GET /api/config` then read `decision_gate: true`, `decision_gate_mode: shadow` |
| tmux | none reachable. The daemon was started with a stub `tmux` first on its `PATH` that answers `-V` and fails every other call, so no tmux server could be started by the check. The daemon logged `TMUX_TMPDIR was unset — using dedicated socket ...` (it computes that path at start); every tmux call it made hit the stub |
| Planner | the only REST path from `draft` to `needs_review` (which unlocks `add_story`/`add_task`) is a real decompose. A 40-line Ollama stand-in on loopback (`127.0.0.1:18730`, answers `POST /api/generate` with a fixed JSON plan) served as the planner, registered as compute node `datawatch-ollama` (kind `ollama`) behind the migrated `ollama` LLM entry. The plan has one story and the task spec names the question id |
| Data created | Automaton `14bd0001` (task `7cb00002`), question `q-18e3216c4b139225f927` (context `prd`, `prd_id` `14bd0001`, options `reuse`/`new`, default `hold`); for step (d) Automaton `fa630001` with two tasks, a withdrawn question `q-18e321dafbb3d36d87ff` and the never-raised id `q-0123456789abcdef0123`, plus one unrelated question. No production data was read or reused |
| Backend | none. A worker session needs tmux, so a task that passes the gate fails at its start attempt with `spawn: session start: 500 Internal Server Error — create tmux session: exit status 1` |

## Commands run

All shell commands ran under `env -u TMUX -u TMUX_TMPDIR`. No tmux server
command was run, nothing was killed by name, and every process was stopped by
its exact PID.

| # | Command | Used for |
|---|---|---|
| 1 | `go build -o .datawatch/live-check/bin/datawatch ./cmd/datawatch/` | build the sandbox daemon |
| 2 | `datawatch start --foreground --config .datawatch/live-check/config.yaml` | sandbox daemon (PIDs 915344, 939165, 969883 over three starts) |
| 3 | `PUT /api/config` `{"autonomous.enabled": true, "autonomous.questions.enabled": true}` | channels on |
| 4 | `POST /api/autonomous/prds`, `POST /api/questions`, `POST /api/autonomous/prds/{id}/decompose`, `.../plan_coverage/acknowledge`, `.../approve`, `.../run` | create the Automaton and the question, plan, approve, run |
| 5 | `PUT /api/config` `{"autonomous.questions.decision_gate_mode": "on"}` | step (b) |
| 6 | `POST /api/autonomous/prds/14bd0001/reset_task` | put the failed task back to pending (see step b) |
| 7 | `GET /api/autonomous/decisions?prd_id=...`, `GET /api/autonomous/prds/{id}` | REST reads |
| 8 | `datawatch --config <cfg> --url http://127.0.0.1:18710 autonomous decisions 14bd0001` | CLI read |
| 9 | `datawatch --config <cfg> mcp` (stdio), JSON-RPC `initialize` then `tools/call` `autonomous_decisions {"prd_id":"14bd0001","state":"open"}` | MCP read |
| 10 | `POST /api/test/message` `{"text":"autonomous decisions 14bd0001"}` | comm read (runs the router verb) |
| 11 | `POST /api/questions/{id}/answer` with a multi-line quote | step (c) |
| 12 | `POST /api/questions/{id}/withdraw`, answer attempts on the withdrawn and unknown ids | step (d) |
| 13 | `DELETE /api/autonomous/prds/{id}?hard=true` (after `.../cancel` for `fa630001`), `POST /api/sessions/delete` for the two failed sessions, `DELETE /api/compute/nodes/datawatch-ollama` | cleanup through the API |
| 14 | `kill 969883`, `kill 999783` (final sandbox daemon, final stand-in), each followed by `kill -0` | stop by exact PID |
| 15 | `bash scripts/check-no-leftover-test-daemons.sh check <empty file>` | leftover-daemon scan |

## Results

### (a) Shadow mode, gate on

Result: PASS — with `decision_gate: true` and `decision_gate_mode: shadow`, `run` did not hold task `7cb00002`: it went straight to its start attempt; the Automaton log gained exactly one `decision_gate_shadow` entry (`would hold: operator decision q-18e3216c4b139225f927 is waiting for an answer: Live check: use the existing table or add a new one? (task 7cb00002)`, `decision_state: open`), and the decision line showed the question `open` with `task_ids: ["7cb00002"]`, `waiting_task_ids: []`, `answer: null`.

Observed: the task then failed on the stubbed tmux (no backend, see setup). After `reset_task` the executor scanned the task a second time still in shadow mode, and the log still held one entry (the dedup works against a live store).

### (b) Mode `on`

Result: PASS — after `PUT /api/config {"autonomous.questions.decision_gate_mode": "on"}` (read back as `on`) and `reset_task`, task `7cb00002` became `waiting_decision` with `wait_reason` = `operator decision q-18e3216c4b139225f927 is waiting for an answer: Live check: use the existing table or add a new one?`, and that same reason was read on REST, MCP, the CLI and comm; one warning alert (`Automaton task waiting for a decision`) was raised and stayed at one after a daemon restart.

| Surface | What it returned |
|---|---|
| REST | `GET /api/autonomous/prds/14bd0001` task `status: waiting_decision`, `wait_reason` as above; `GET /api/autonomous/decisions?prd_id=14bd0001&state=open` `waiting_tasks[0].wait_reason` identical |
| MCP | `autonomous_decisions` over stdio: `isError` unset, `waiting_task_ids ["7cb00002"]`, same `wait_reason` |
| CLI | `q-18e3216c4b139225f927 open Live check: use the existing table or add a new one? \| waiting: task 7cb00002 (operator decision q-18e3216c4b139225f927 is waiting for an answer: Live check: use the existing table or add a new one?)` |
| comm | the router reply `[dw-decision-live-check] autonomous decisions` followed by the same line as the CLI |

Notes on how this ran:

- The mode must be `on` before the executor scans the task: a task that already passed the gate in shadow mode has already started. The failed task from step (a) was put back to pending with `reset_task` after the mode was switched, and the executor then parked it.
- **Finding (not fixed, not part of this task):** the first comm read returned `HTTP 401: unauthorized` while `server.token` was set. The router's loopback helper `commGet` (`internal/router/sx2_parity.go`) sends no `Authorization` header, so every comm verb built on it (133 call sites) is refused by a daemon with a server token. The comm read in this report was taken after restarting the sandbox daemon with an empty `server.token`. The restart also showed the held task and its reason surviving a daemon restart (`boot-resume ... re-launched executor`, task still `waiting_decision`).

### (c) Answer releases the task

Result: PASS — `POST /api/questions/q-18e3216c4b139225f927/answer` (201) with the quote `Add a new table.\nKeep the old one read-only. Café — "quoted" & <b>raw</b>` moved the task out of `waiting_decision` about 70 ms later (answer stored 18:48:55.916, task seen failed at 18:48:55.988 by a 50 ms poll) with no `run` call (the Automaton log's `run` entries were 4 before the answer and 4 after), and the decision line then showed `state: answered`, no waiting tasks, answer `a-18e321b489f4551fd28f` by `operator` via `cli`, the quote byte-for-byte equal to the one sent and `quote_sha256` equal to the SHA-256 of that text; the CLI and comm replies print the same quote.

Result: NOT RUN — the backend part (the released task actually running a worker session): the sandbox has no tmux and no worker backend, so the start attempt failed with `spawn: session start: 500 Internal Server Error — create tmux session: exit status 1`. The status record up to the start attempt is `waiting_decision` → (start attempt) → `failed`; the intermediate `pending`/`running` states fell between 50 ms polls. That the task reached its start attempt without a manual `run` is what the gate promises; a worker running to completion was not observed.

### (d) Withdrawn and unknown ids

Result: PASS — on Automaton `fa630001` (mode `on`), a task naming the withdrawn decision `q-18e321dafbb3d36d87ff` and a task naming the unknown id `q-0123456789abcdef0123` both became `waiting_decision` with the reasons `operator decision q-18e321dafbb3d36d87ff was withdrawn without an answer` and `operator decision q-0123456789abcdef0123 was not found`, and both stayed held after an answer attempt on the withdrawn id (409 `question is not open`), an answer attempt on the unknown id (404 `unknown question`), an unrelated question answered on the same Automaton, and a 36 s wait past the 30 s re-check; the alert count was one per held task (3 in all with step b's).

## Cleanup

- Sandbox daemon stopped by exact PID (`kill 969883`, confirmed gone with `kill -0`; the two earlier starts, PIDs 915344 and 939165, were stopped the same way before each restart). The stand-in was stopped by PID too (933783, then 999783). Ports 18710, 18711 and 18730 were confirmed closed, and a read-only scan of `/proc` found no process still naming the scratch folder. The stdio `datawatch mcp` run exited on its own (exit 0).
- Records removed through the API: both Automata (`14bd0001`, `fa630001`, hard delete), the two failed sessions the start attempts created, and compute node `datawatch-ollama`. The three questions and the alerts have no delete route; they lived in the sandbox data dir and went with it.
- The scratch folder `.datawatch/live-check/` (config, data dir, binary, stub `tmux`, stand-in, scratch project dir) was deleted. `git status` was clean afterwards.
- `scripts/check-no-leftover-test-daemons.sh` was run against an empty baseline and listed 12 test daemons, all days old and with configs under other `/tmp` paths: none started by this check, none touched. The sandbox daemon is not among them.
- Side effect to be aware of: on its first start the daemon logged `[channel] cleaned up 1 stale MCP registration(s)`, which is the channel bridge tidying a registration outside the project directory. The check did not read or write it.

## Not covered

The Web UI (the Decisions card, the `Waiting for a decision` label) was not driven; its row in the tracker stays at Validated No. The datawatch-app screens are covered by the parity issue, not here. A released task running on a real worker backend is the NOT RUN part of step (c).
