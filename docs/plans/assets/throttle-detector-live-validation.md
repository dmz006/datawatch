# Throttle-detector live validation

Date: 2026-10-28 (host `johnnyjohnny`, datawatch `9.30.3`, commit `da44be860`)
Scope: live end-to-end check that a line *quoting* one of the rate-limit
detector's list entries mid-sentence does **not** trip the detector on a
live pi-prompt session and does **not** activate a usage back-off. This run
changed no source files and did not attempt to produce a real throttle.

## Setup

- Branch binary built in the project worktree: `go build -o datawatch
  ./cmd/datawatch/` (commit `da44be860`, 2026-10-28).
- Sandbox under an **allowed project root** that is **git-ignored**:
  `/home/dmz/workspace/.datawatch-test-throttle/live/` — `workspace/.gitignore:14`
  matches `.datawatch-test-*`, so `git -C /home/dmz check-ignore -v` reports
  it as ignored and it never appears in `git status`. Config
  (`.../live/config.yaml`) has an isolated `data_dir`, `server` on
  `127.0.0.1:18219` (no `server.token`, so REST probes needed no bearer
  auth), `session.llm_backend: pi-prompt`, and a `project_dir` at
  `.../live/project` — an **empty plain directory, not a checkout and not a
  git repository of its own** (the daemon's `allowed_project_roots` are
  `/home/dmz/.datawatch/prd-worktrees` and `/home/dmz/workspace`; earlier
  attempts with a scratch path outside both roots were refused verbatim:
  `project dir refused: project directory is outside the allowed project
  roots ... add a root with server.allowed_project_roots`, which is why the
  workspace location is used).
- Daemon start follows the test-daemon minimum recipe
  (`source scripts/test-daemon-lib.sh`; `td_install_traps` first; `td_start`;
  `td_set_api`). Started with `env -u TMUX -u TMUX_TMPDIR` and an isolated
  `TD_STATE_DIR`/`TD_OWNER` so no sibling test-daemon state is touched.
- pi backend check: installed — `pi` v1.3.0 at
  `/home/dmz/.nvm/versions/node/v24.15.0/bin/pi`. Model:
  `ollama/qwen3.8-64k:27b` (local Ollama at 127.0.0.1:11434; verified
  present before the run). No backend substitution.

## Detector table context (verbatim)

The registered list entry quoted by the test line is the **first phrase of
the `claude-code` backend family** in
`internal/session/rate_limit_lines.go` → `builtinRateLimitPhrases`:

```
you've hit your limit
```

The file's package comment states the matching rules that make this test
meaningful: a line is a built-in signal only when the trimmed line **starts
with** a phrase of the session's own backend family (optionally behind the
family's recorded gutter prefix, `⎿  ` for claude-code), case-insensitive —
"Quoting a phrase anywhere else on the line is never a detection, for any
backend." The session's own family here is `pi-prompt`, whose table is empty
(`nil`) except for the shared protocol marker
`DATAWATCH_RATE_LIMITED:` (also line-start only).

## Commands and observations

### 0. Daemon start (recorded)

```
source scripts/test-daemon-lib.sh
td_install_traps "$@"                     # EXIT/INT/TERM/HUP, scoped TD_OWNER+TD_STATE_DIR
TD_PID=$(td_start "$ROOT/datawatch" "$CFG" "$LOG" --no-mcp)   # -> 3185782
td_set_api "$PORT"
tr -d '\0' < /proc/3185782/cmdline        # marker re-verified at start
```

Observed cmdline at start:
`/home/dmz/.datawatch/prd-worktrees/ea7c0001/datawatch start --foreground
--config /home/dmz/workspace/.datawatch-test-throttle/live/config.yaml
--no-mcp`

`GET /api/health` → `"status":"ok"`, `"version":"9.30.3"`, REST listener
`loopback:true`.
`GET /api/backends` → `{"active":"pi-prompt","llm":[{"name":"claude-code","available":true,"enabled":true}]}`

### 1. Usage back-off status before

```
curl -s http://127.0.0.1:18219/api/usage-backoff
```

```json
{"active":[],"config":{"mode":"pause_backend","max_wait_minutes":60},"patterns_version":"153b02a7e89e","patterns_stale":true,"claude_cli_version":"2.1.312 (Claude Code)"}
```

Observed: **no active back-off** (`"active":[]`).

### 2. Start one pi-prompt session (empty non-git project dir)

```
curl -s -X POST http://127.0.0.1:18219/api/sessions/start \
  -H 'Content-Type: application/json' \
  -d '{"task":"When you receive a message, respond with the single word ok
      and nothing else, then stay idle.",
       "project_dir":"/home/dmz/workspace/.datawatch-test-throttle/live/project",
       "backend":"pi-prompt","model":"ollama/qwen3.8-64k:27b",
       "name":"throttle-live-quoted-line"}'
```

Observed: session `65aa` (`johnnyjohnny-65aa`), `backend_family:
"pi-prompt"`, `state: "running"`, pane `cs-johnnyjohnny-65aa`,
`one_shot: true`, `input_mode: "tmux"`.

### 3. Send the quoted line as input

The line is an ordinary sentence about a throttle bug that quotes the
claude-code list entry verbatim **in the middle of the sentence** (the entry
is `you've hit your limit`). Neither it nor the shared marker sits at the
line start, and the session family (`pi-prompt`) has no phrase table — this
is exactly the combination the anchored, per-backend matcher must not fire
on:

```
curl -s -X POST http://127.0.0.1:18219/api/sessions/send \
  -H 'Content-Type: application/json' \
  -d '{"session_id":"johnnyjohnny-65aa",
       "text":"While debugging the throttle bug, I saw the retry log print
       \"you'\'ve hit your limit\" before the wait window began.",
       "source":"throttle-live-validation"}'
```

Observed: `{"ok":true,"session_id":"johnnyjohnny-65aa"}`.

Delivery was then verified in the session's pane output
(`.../data/sessions/johnnyjohnny-65aa/output.log`); the exact line is
present verbatim as typed into the pane (this is the line the detector's
output-line path scanned; the shell echo `While: command not found` after
it is the pane treating the sentence as a shell word, expected and
harmless):

```
...$ While debugging the throttle bug, I saw the retry log print "you've hit your limit" before the wait window began.
```

### 4. Usage back-off status and session state after (immediately, and
again after a 20 s settle)

```
curl -s http://127.0.0.1:18219/api/usage-backoff
curl -s http://127.0.0.1:18219/api/sessions | python3 -c '...state per session...'
```

Both times (t0 at 19:18:14 and t+20 s at 19:18:34):

```json
{"active":[],"config":{"mode":"pause_backend","max_wait_minutes":60},"patterns_version":"153b02a7e89e","patterns_stale":true,"claude_cli_version":"2.1.312 (Claude Code)"}
```

Session `65aa` state: `running` at the moment of the send; `complete`
afterwards (pi-prompt one-shot finished its turn normally — it replied and
the pane returned to the prompt). It never entered any
`rate_limited`/back-off state.

## Observations against the expected result

| Check | Expected | Observed |
|-------|----------|----------|
| Usage back-off before | clear | clear (`"active":[]`) |
| Sent line delivered to session pane | present verbatim | present verbatim (grep -F match in `output.log`) |
| Usage back-off after t0 | still clear | still clear (`"active":[]`) |
| Usage back-off after t+20 s | still clear | still clear (`"active":[]`) |
| Session state after | not rate-limited | `running` → `complete` (normal one-shot finish) |

The mid-sentence quote of the list entry `you've hit your limit` on a
pi-prompt session did not activate the detector: no back-off appeared at
either probe and the session did not flip to the rate-limited state —
consistent with the anchored, per-backend matching documented in
`rate_limit_lines.go`.

## Cleanup

- Session `65aa` ended via REST: `POST /api/sessions/kill`
  (`{"session_id":"johnnyjohnny-65aa"}`) → `{"status":"ok"}`;
  `GET /api/sessions` afterwards shows only that session in state
  `complete` (no new sessions were created by the run).
- Daemon stopped **only via its recorded PID**: `td_stop_one 3185782` — the
  lib re-verified the config marker in `/proc/<pid>/cmdline` before
  signalling — `rc=0`; `/proc/3185782` no longer existed and
  127.0.0.1:18219 stopped accepting connections
  (`curl: (7) Failed to connect`).
- Leftover check against the pre-start baseline (12 unrelated operator
  daemons, recorded and untouched): **no leftover test daemons**.
- Sandbox tree at `/home/dmz/workspace/.datawatch-test-throttle/`
  (config, project dir, data dir, daemon log, evidence snapshot) was removed
  after the evidence was written up; `git -C /home/dmz status` is clean and
  the scratch paths were git-ignored the whole time.

result: pass
