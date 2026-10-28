# Throttle-detector live validation

Date: 2026-10-28
Scope: live end-to-end check that a line *quoting* one of the rate-limit
detector's list entries mid-sentence does **not** trip the detector on a
live pi-prompt session and does **not** activate a usage back-off. This run
changed no source files and did not attempt to produce a real throttle.

## Setup

- Branch binary built in the project directory: `go build -o datawatch ./cmd/datawatch/`.
- Sandbox under a git-ignored path (`git check-ignore` confirmed `.sec-sandbox/`):
  config with an isolated `data_dir`, `server` on `127.0.0.1:18091` (no
  `server.token`, so the REST probes needed no bearer auth),
  `session.llm_backend: pi-prompt`, and a `project_dir` that was an empty
  directory, not a git repository.
- Sandbox daemon started with an empty TMUX environment
  (`env -u TMUX`, isolated `TMUX_TMPDIR`) as required by the test-daemon
  cleanup rules; its PID was recorded at start. PID `2832862` was carried
  over from the prior run of this same sandbox; before reusing it its
  `/proc/<pid>/cmdline` was re-verified to still carry the recorded config
  (`./datawatch start --config .../throttle-validate/config.yaml --no-mcp
  --foreground`), so it satisfied the "recorded PID + marker" rule.
- pi backend check: installed — `pi` v1.3.0 at
  `/home/dmz/.nvm/versions/node/v24.15.0/bin/pi`. No backend substitution
  was needed.

## Commands and observations

### 1. Usage back-off status before

```
curl -s http://127.0.0.1:18091/api/usage-backoff
```

```json
{"active":[],"config":{"mode":"pause_backend","max_wait_minutes":60},"patterns_version":"153b02a7e89e","patterns_stale":true,"claude_cli_version":"2.1.312 (Claude Code)"}
```

Observed: **no active back-off** (`"active":[]`).

### 2. Start one pi-prompt session

```
curl -s -X POST http://127.0.0.1:18091/api/sessions/start \
  -H 'Content-Type: application/json' \
  -d '{"task":"Write a detailed 800-word technical essay about retry and
      exponential backoff logic for HTTP clients, covering jitter,
      idempotency, and circuit breakers.",
       "project_dir":".../.sec-sandbox/throttle-validate/project",
       "backend":"pi-prompt","model":"ollama/qwen3.8-64k:27b",
       "name":"throttle-validate-live3"}'
```

Observed: session `158c` (`johnnyjohnny-158c`), `backend_family: "pi-prompt"`,
state **`running`**, pane `cs-johnnyjohnny-158c`.

Note (env detail, not a detector issue): two earlier session starts died
before any input because the configured model could not be resolved on this
host — first `anthropic:claude-sonnet-4-5` (no credentials, `pi` reported
"Model ... not found"), then `ollama:qwen3.8-64k:27b` (`pi --model` expects
the `provider/model` slash form; `ollama/qwen3.8-64k:27b` resolves). Both
dead sessions were ended (REST kill) before the session above was started.
Neither of those runs ever delivered a live input line, so they are excluded
from the pass/fail observation.

### 3. Send the quoted line as input

The line is an ordinary sentence about a throttle bug that quotes one of the
list entries verbatim in the middle of the sentence (the entry is a
registered built-in detector phrase for the claude-code backend family, so
per-backend scoping plus anchored line-start matching are what must keep it
from matching on this pi-prompt session):

```
curl -s -X POST http://127.0.0.1:18091/api/sessions/send \
  -H 'Content-Type: application/json' \
  -d '{"session_id":"158c",
       "text":"I checked what the GitHub-token-throttled security workflow
       prints when the API bucket is drained; it outputs <the list entry>
       and then sleeps for the retry window before attempting the next
       request.","source":"live-validation"}'
```

Observed: `{"ok":true,"session_id":"158c"}`. The quoted line was confirmed
present in the monitored pane output of the session
(`.../data/sessions/johnnyjohnny-158c/output.log`), so the detector's live
output-line path saw it.

### 4. Usage back-off status and session state after (immediately, and again
after a 10 s settle)

```
curl -s http://127.0.0.1:18091/api/usage-backoff
curl -s http://127.0.0.1:18091/api/sessions
```

Both times, immediately after the send and 10 s later:

```json
{"active":[],"config":{"mode":"pause_backend","max_wait_minutes":60},"patterns_version":"153b02a7e89e","patterns_stale":true,"claude_cli_version":"2.1.312 (Claude Code)"}
```

and session `158c` state **`running`** (unchanged from before the send; never
`rate_limited`).

## Observations against the expected result

| Check | Expected | Observed |
|-------|----------|----------|
| Usage back-off before | clear | clear (`"active":[]`) |
| Usage back-off after | still clear | still clear (`"active":[]`) |
| Session state after | not rate-limited | `running` (unchanged) |

The mid-sentence quote of a list entry on a pi-prompt session did not
activate the detector: no back-off appeared and the session did not flip to
the rate-limited state, matching the anchored, per-backend matcher behavior.

## Cleanup

- Both sandbox session runs (the two that failed to resolve a model and the
  live one, `158c`) were ended via REST (`/api/sessions/kill`, then
  deleted); `GET /api/sessions` returned `[]` afterwards.
- The sandbox daemon was ended only by its recorded PID (marker re-verified
  in `/proc/<pid>/cmdline` before signalling); afterwards `ps` showed no
  process carrying the sandbox config path and port 18091 stopped accepting
  connections, confirming the PID was gone.
- Sandbox directories (config, data dir, project dir, logs) were removed.

result: pass
