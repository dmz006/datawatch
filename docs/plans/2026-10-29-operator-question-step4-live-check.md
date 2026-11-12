# Operator question channel — Step 4 live check of the worker marker

Date: 2026-10-29 · Branch: `automaton/f1f200be`

Scope: a live check, on a throwaway sandbox daemon, of the worker marker: a line a worker session prints that starts with `DATAWATCH_QUESTION:` and is raised into the operator question store when `autonomous.questions.worker_marker` is on. Nothing in the code was changed by this check.

## Setup

- **Sandbox only.** The production daemon and its port 8443 were not touched. The sandbox daemon ran on loopback port 57993 (plain HTTP, TLS off), with its own `--config`, its own data directory and a random bearer token, all under `.datawatch/wm-live/` (git ignores `.datawatch`).
- **Binary.** `go build -o .datawatch/wm-live/bin/datawatch ./cmd/datawatch/` from this tree; the daemon reported `datawatch v9.35.0`.
- **Config.** `session.llm_backend: shell`, `session.kill_sessions_on_exit: true`, `autonomous.enabled: true`, `server.proxy_sandbox_port: 0`. Both question switches were off at boot and were turned on through `PUT /api/config`.
- **Shell rules.** Every shell command ran under `env -u TMUX -u TMUX_TMPDIR`. No tmux server command was run and nothing was killed by name. The daemon was started under `timeout 2h` as a last-resort backstop, and was stopped by its exact PID.
- **Notation.** `$API` is `http://127.0.0.1:57993` with the bearer token sent as an `Authorization` header. Responses are trimmed to the fields that matter and say so.
- **The four lines.** `lines.sh` in the sandbox directory waits 9 seconds, then prints, each at the start of its own line:
  1. a valid worker question line: a question, two options (`a`, `b`), `"default":"hold"`, `"blocked":false`;
  2. an instruction sentence that mentions the marker in the middle: `Remember to print the DATAWATCH_QUESTION: marker only when a design choice is needed.`;
  3. a malformed line that starts with the marker and an open brace: `DATAWATCH_QUESTION: {this is not json`;
  4. a placeholder line that starts with the marker and a less-than sign: `DATAWATCH_QUESTION: <json>`.
- **Session.** The shell backend only echoes the `task` as a comment and does not run it, so the session was started through `POST /api/sessions/start` and the command `bash …/lines.sh` was typed into it through `POST /api/sessions/send`.

## Commands and results

Exit codes are the shell exit code of the call (curl or the process command). "HTTP" is the status the daemon returned.

| # | Command | Exit | Response (trimmed) |
|---|---------|------|--------------------|
| 1 | `go build -o .datawatch/wm-live/bin/datawatch ./cmd/datawatch/` | 0 | binary built (7 s) |
| 2 | start the daemon: `timeout 2h …/datawatch start --foreground --config …/config.yaml` | 0 | log: `datawatch server listening on http://127.0.0.1:57993`; daemon PID 3187797 under the timeout wrapper 3187795 |
| 3 | `PUT $API/api/config` `{"autonomous.questions.enabled":true,"autonomous.questions.worker_marker":true}` | 0 | HTTP 200 `{"status":"ok"}` |
| 4 | `GET $API/api/config` (the `autonomous.questions` block) | 0 | HTTP 200 `"enabled": true … "worker_marker": true` |
| 5 | `POST $API/api/sessions/start` `{"backend":"shell","name":"wm-live-1",…}` | 0 | HTTP 200, session `wm-live-sbx-e2dc`, state `running` |
| 6 | `POST $API/api/sessions/send` `{"session_id":"wm-live-sbx-e2dc","text":"bash …/lines.sh"}` | 0 | HTTP 200 `{"ok":true,…}` |
| 7 | `GET $API/api/questions` (after the four lines printed) | 0 | HTTP 200, `count` 1: `id q-18e32a76c5123e55aa6c`, `asked_by worker-e2dc`, `asker_kind session`, `session_id wm-live-sbx-e2dc`, `state open`, `default hold`, `source marker`, `source_line_sha256 634fe5bd…76ef`, `return_path false`, two options. No `blocking` key (the field is omitted when false) |
| 8 | `GET $API/api/audit?limit=50`, question entries only | 0 | HTTP 200. `question_raised` (actor `autonomous`; question id, asker, option count, default `hold`, the question text). `question_marker_refused` (actor `autonomous`; `reason` `malformed`, `line_sha256` `d1edeb41…8567`, `session_id`). Nothing else for the other two lines |
| 9 | text of the refused and placeholder lines searched for in `audit.log` | 0 | `this is not json` 0 hits, `<json>` 0 hits. The raised entry carries the question text a REST raise also carries, never the marker line |
| 10 | `sha256` of the exact malformed line, and of the exact valid line | 0 | equal to the audit `line_sha256` and to the question's `source_line_sha256` |
| 11 | `POST $API/api/questions/q-18e32a76c5123e55aa6c/answer` with `chosen_option_id` `a` and a quote `Go with the shared cache.⏎Say "yes" to the plan.` (a newline and a double quote) | 0 | HTTP 201, answer `a-18e32a7c311a49b54dcf`, question `state answered` |
| 12 | `GET $API/api/questions/q-18e32a76c5123e55aa6c/answers`; the quote decoded and compared to the sent bytes | 0 | HTTP 200, one answer, `"quote":"Go with the shared cache.\nSay \"yes\" to the plan."`. Byte compare: equal, 48 bytes against 48 |
| 13 | `PUT $API/api/config` `{"autonomous.questions.worker_marker":false}`, then `GET $API/api/config` | 0 | HTTP 200; `worker_marker: false` |
| 14 | `POST $API/api/sessions/kill` for `wm-live-sbx-e2dc`, then `POST …/start` for a fresh shell `wm-live-2` | 0 | session `wm-live-sbx-7154` running. (The first session was still inside the 10-minute `sleep` at the end of `lines.sh`, so a second command typed there would not have run; see Notes) |
| 15 | `POST $API/api/sessions/send` into `wm-live-sbx-7154`: `bash …/lines2.sh`, which prints one valid line with a different question text | 0 | HTTP 200. Pane read from the session log: the valid `DATAWATCH_QUESTION: {…"Retry the lookup once more?"…}` line and `lines2-done` both printed |
| 16 | `GET $API/api/questions` after step 15 | 0 | HTTP 200, `count` 1: still only `q-18e32a76c5123e55aa6c` (answered); no question for "Retry the lookup once more?" |
| 17 | `kill -TERM 3187797`, then wait and `ps -p 3187797` and `ps -p 3187795` | kill 0; ps 1, 1 | both gone; port 57993 not listening; log: sessions closed on shutdown |
| 18 | start the daemon again (same config, same data directory) | 0 | new daemon PID 3216079 under wrapper 3216077 |
| 19 | `GET $API/api/config` and `GET $API/api/questions` after the restart | 0 | HTTP 200 both. `worker_marker: false` kept across the restart. `count` 1: the same answered question, same id, no duplicate. Both sessions `killed` (`kill_sessions_on_exit`) |
| 20 | extra probe: `PUT worker_marker true`, wait 12 s, `GET /api/questions`, `GET /api/audit`, `PUT worker_marker false` | 0 | `count` 1, the same answered question; question audit actions unchanged: `question_answered`, `question_marker_refused`, `question_raised` (one each) |
| 21 | delete both sessions through `POST /api/sessions/delete` (`delete_data` true), `kill -TERM 3216079`, wait, `ps -p 3216079`, `ps -p 3216077`, `ss -ltn` for 57993 | 0; kill 0; ps 1, 1; ss 0 hits | sessions list `[]`; daemon and wrapper gone; port closed |
| 22 | `rm -rf .datawatch/wm-live` (after checking the path ends with `f1f200be/.datawatch/wm-live`), then `test -e` on it | rm 0; test 1 | sandbox directory deleted; a `ps` listing for the sandbox name returned nothing |

## Verdict

| Step | Verdict | What was seen |
|------|---------|---------------|
| Both switches set through `PUT /api/config` and confirmed by `GET` | **PASS** | rows 3, 4 |
| Valid worker line raises exactly one open question, source `marker`, not blocking, default `hold` | **PASS** | row 7. `blocking` is not in the body because the field is omitted when false, so "blocking false" is read from its absence |
| Instruction line that names the marker mid-sentence raises nothing and is not audited | **PASS** | rows 7, 8: one question, one refusal, no entry for it |
| Malformed line is refused and audited with a reason category and a hash, without its text | **PASS** | rows 8, 9, 10. The hash equals the SHA-256 of the exact line |
| Placeholder line starting with `<` is ignored | **PASS** | rows 7, 8: no question, no audit entry |
| `question_raised` and `question_marker_refused` are in the audit log | **PASS** | row 8. The refused entry holds only the session id, the reason and the hash. The raised entry holds the question text, as any raise does |
| Answer with a newline and a double quote is stored and returned byte for byte | **PASS** | rows 11, 12 |
| Marker off: a further valid line raises nothing | **PASS** | rows 13, 15, 16. The line was seen in the pane, and the store did not change |
| Daemon stopped by exact PID and started again: no duplicate question, config kept | **PASS** | rows 17, 18, 19. The stored question survived once, answered |
| Daemon stopped by exact PID and confirmed gone; sandbox directory deleted | **PASS** | rows 21, 22 |

## Notes

- **The marker-off line came from a second session.** The first session was still inside the `sleep 600` that ends `lines.sh`, so a command typed there would have waited behind it. The first session was killed and a fresh shell session printed the line. Both sessions ran with the same daemon and the same switches, so the check holds, but the same-session case was not run.
- **The restart check did not re-read a pane.** The daemon was set to end its sessions on shutdown, so after the restart both sessions were `killed` and nothing was scanned again. Row 20 shows no new question when the marker was turned back on, but it does not prove the same-line de-duplication of a pane that is read twice. That rule (an open question for the same session and line is not raised twice) is covered by the Go tests only.
- **Where the refusal log line lands.** In this sandbox the one daemon log line of the refusal (`[autonomous] worker question marker refused: session=… reason=malformed line_sha256=…`, no line text) was written to `data/daemon-crash.log`, not to the daemon's standard output file.
- **tmux.** The first run's sandbox daemon opened its panes on the default tmux socket because `TMUX_TMPDIR` was unset, and the tmux server was not inspected afterwards. That left the aim of the Session Safety rule (test work stays away from the operator's tmux server) unverified, so it was checked again in the second run below, with a private tmux directory.
- **PIDs.** The daemon was the child of the `timeout` wrapper. The first run's wrapper was 3187795 and daemon 3187797. The second run's wrapper was 3216077 and daemon 3216079. Only the daemon PIDs were signalled; the wrappers ended by themselves.

## Second run: the tmux server the daemon used

Run on 2026-10-29 after the first report, to close the tmux gap above. Same binary and the same marker path, on a fresh sandbox daemon. The point of this run is the tmux server, so it printed one valid line only.

- **Daemon.** Started through `scripts/test-daemon-lib.sh` (start, record the PID with the config path as the marker, traps on every exit path), with `env -u TMUX TMUX_TMPDIR=<private dir>`. The private directory is `.datawatch/pt` inside this tree (git ignores `.datawatch`), 57 characters long, so the socket path (77 characters) is under the unix socket limit. A first attempt with a directory under the scratch area failed with `create tmux session: exit status 1` because the socket path was too long; the short directory fixed it. Data directory, config and project directory were under `.datawatch/wm-recheck`.
- **Checked from `/proc` before any session was made.** The daemon's `cmdline` carried the recorded `--config` path (1 match), its environment had `TMUX_TMPDIR` set to the private directory (1 match) and no `TMUX` variable (0 matches).
- **Marker.** Both switches set through `PUT /api/config` (HTTP 200). One shell session was started and `bash lines.sh` was typed into it; the script waits 9 seconds, then prints one valid `DATAWATCH_QUESTION:` line (a first attempt printed the JSON through an unquoted `echo`, which dropped the quotes, so nothing was raised; that was a fault of the test script, and the quoted line is what was used). `GET /api/questions` then returned `count` 1: `asked_by worker-2d5a`, `asker_kind session`, `state open`, `source marker`, `default hold`, no `blocking` key.
- **tmux.** `tmux list-sessions` against the private directory listed exactly one session, `cs-dw-wm-recheck-2d5a`, and the private socket directory held the `default` socket: the daemon's panes were on the private server, not on the default one. After `POST /api/sessions/delete` the same command printed `no server running on …/.datawatch/pt/tmux-1000/default`, and it printed the same after the daemon was stopped. The operator's own tmux server was never named in any command, because the daemon had no way to reach it: no `TMUX`, and a different `TMUX_TMPDIR`.
- **Stop.** `td_stop_one` with the recorded PID; the daemon was gone afterwards (`kill -0` failed), the sandbox and private directories were removed, and the session ended in the same call. No tmux server command that kills anything was run.

## What was not run live

- **The park path for a blocking question.** The worker line used here had `"blocked": false`. A blocking line parks its task on the question, keeps the retry count, releases the lease and resumes when the question is answered. That path is covered by the Go tests (`TestAwaitDecision_*`, `TestWaitReason_AwaitDecisionTextAndPredicate`, `TestQuestionTick_*`) and was **not validated live**.
- A marker line printed by a real worker of an Automaton, with a task id and a PRD id on the session. The session here was a plain shell session, so the question context was `none`.
- The raise notifications for a marker question (WebSocket event, messaging line, high-priority alert), and the PWA toggle for `worker_marker`.
- The other config surfaces for `worker_marker` (MCP, CLI, comm channel); only REST was driven.
- The restart-grace and task-echo windows (the first seconds after a restart or a task delivery, in which a marker is ignored). The 9-second wait before printing stayed outside them, so they were not tested.
- Lines over the size caps, an invalid default, a duplicate option id, an unknown label or risk, and a recommendation that names no option (the Go tests cover each refusal reason).
- Two sessions printing the same line, and a line printed twice by one session while its question is open.

## Parity surface

This document records a live check and changes nothing; the surfaces below are the ones the checked feature reaches, with the check's coverage of each.

| Surface | Reached by the feature? | Covered by this check |
|---|---|---|
| REST | Yes: `/api/questions`, `/api/questions/{id}/answer`, `/api/questions/{id}/answers`, `/api/config` | Driven directly (rows 3 to 20, second run) |
| MCP | Config key through `config_set`, and the stats read `question_stats` | Not driven live; covered by Go tests |
| CLI | Config key through `datawatch config set` | Not driven live |
| Comm channel | Config key through `configure` | Not driven live |
| YAML/config | `autonomous.questions.worker_marker` | Driven through `PUT /api/config` and read back |
| PWA | Settings toggle, answer link, Monitor card | Not driven live; covered by the PWA story TS-838 and the node tests |
| Android | Tracked in datawatch-app#283 | Not applicable: no app is built here |
| iPhone/iOS | Tracked in datawatch-app#283 | Not applicable: no app is built here |
