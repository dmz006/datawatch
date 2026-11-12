# Hung pi-prompt request investigation — findings (PRD f3420001)

**Date:** 2026-10-29
**Version:** none (planning document; plan-document commits are exempt from the version bump)
**Status:** Findings, complete

Two hung pi-prompt sessions (ab4c, ef70) on Ollama node `<node-address>:11434`, model
`qwen3.8-64k:27b`, local time 2026-10-28. Everything below was gathered through the local
datawatch daemon's own read-only interfaces: daemon REST `https://127.0.0.1:8443`
(`/api/sessions?limit=300`, `/api/sessions/timeline` at internal/server/server.go:255,
`/api/logs` at internal/server/server.go:662, `/api/compute/nodes` at
internal/server/server.go:385, `/api/observer/envelopes` at internal/server/server.go:471) and
the MCP bridge `POST /api/mcp/call` (internal/server/api.go:1242) using the `session_output`,
`session_children` and `session_timeline` tools defined in internal/mcp/server.go (lines 384,
1703, handler at line 2294). One additional read-only stats probe was run directly against the
node's Ollama (`GET http://<node-address>:11434/api/ps`); no generation requests were ever sent.
Statements from the incident background are marked *operator recollection* where they are not
verifiable through the interfaces.

## Scope

PRD **f3420001**, task **bd730001**, story **bd300001**; worktree
`<home>/.datawatch/prd-worktrees/f3420001` (this directory). The PRD's subject is the
descendant-only stall detector whose implementation lives in cmd/datawatch/descendant_stall_wait.go,
cmd/datawatch/descendant_processes.go and cmd/datawatch/exit_silence_wait.go, specified in
docs/plans/2026-10-28-pi-descendant-only-stall.md; the incident investigated here is the live
hang that this PRD category targets. In scope:

- **ab4c** (`local-node-ab4c`, `autonomous:Auto-fix: rules-review findings (attempt 1)`,
  PRD f3420001 / task bd730001): created 2026-10-28 07:51:34 (-04:00), killed 09:30:52
  (-04:00) — the ~1 h 39 m hang.
- **ef70** (`local-node-ef70`, same name): created 09:30:52 (-04:00), one second
  after ab4c's kill, terminal state 10:35:14 (-04:00) — the ~1 h 04 m hang.

Both records show `one_shot=true`, `backend_family=pi-prompt`, `model=ollama/qwen3.8-64k:27b`,
`node_address=<compute-node>`, `compute_node_ref=<compute-node>`. The per-session pane
logs referenced by the records (`<home>/.datawatch/sessions/local-node-ab4c/output.log`,
`.../local-node-ef70/output.log`) sit outside this worktree and were not read (rule 2).
Out of scope: anything needing generation requests, files outside the worktree, code changes.

## Evidence: session ab4c

Facts from the sanctioned interfaces (daemon record `local-node-ab4c`; PRD f3420001, task
bd730001):

- State `killed`; created `2026-10-28T07:51:34.292761232-04:00`, updated
  `2026-10-28T09:30:52.493793566-04:00` — elapsed 5958 s (1 h 39 m 18 s).
  `last_channel_event_at = 2026-10-28T07:51:36.96091244-04:00` (≈2 s after creation).
- Timeline (`session_timeline`, exactly 2 entries): `2026-10-28T11:51:34Z created
  (autonomous:Auto-fix: rules-review findings (attempt 1))`; then
  `2026-10-28T13:30:52Z killed by="autonomous.verifier.idle-detector" event_type=killed
  reason="session idle detector: waiting_input for 5937s, limit 900s, no live child process,
  output age 5958s, no nudge sent"`. Note 5937 s ≈ 07:51:35→09:30:52: per the detector the
  session sat in `waiting_input` for its entire life with the last pane output at the
  07:51:34 mark (`output age 5958 s`). The 900 s `limit` matches
  `session_idle_timeout_seconds` (default 900: "waiting_input/idle with no live child process
  … infra failure", internal/mcp/autonomous.go:117).
- `session_children` → "Session ab4c has no child sessions."
- Final pane content (`session_output` tail): the whole wrapper command, then exactly two
  lines: `DATAWATCH_PI_FAILED: pi exited with status 143 (model ollama/qwen3.8-64k:27b)` and
  `logout`. The wrapper is the template in internal/llm/backends/pi/backend.go:516-563
  (`buildLaunchCmdWithAgentDir`): `cd '<home>/.datawatch/prd-worktrees/f3420001' && {
  PI_CODING_AGENT_DIR='/tmp/datawatch-pi-agent-cs-local-node-ab4c-2690530149' pi --print
  --mode text --approve --model 'ollama/qwen3.8-64k:27b' "$(cat
  '/tmp/datawatch-pi-task-cs-local-node-ab4c-151365485.txt')"; echo $? >
  '/tmp/datawatch-pi-status-…'` piped to `tee '/tmp/datawatch-pi-out-…'`, followed by the
  `dw_status` checks at internal/llm/backends/pi/backend.go:537. Exit **143 = 128 + 15 = the
  pi process died of SIGTERM while running**; it did not terminate on its own error path.
- Daemon verdict for this window (quoted in the retry prompts of ef70 and 1f48 via
  `session_output`; task bd730001): `Attempt 1 — Worker: pi-prompt/ollama/qwen3.8-64k:27b,
  Duration: 5958 s, Outcome: stalled (category infra), Failure: session idle without completion
  (session=ab4c, backend=pi-prompt, model=ollama/qwen3.8-64k:27b, idle 1h38m56.999289012s >
  limit 15m0s, no nudge sent): worker stalled, Files changed: none recorded`.
- Discrepancy (fact vs. operator recollection): the background says the operator manually
  killed ab4c at 09:30; the daemon record attributes the kill to
  `autonomous.verifier.idle-detector`. Not reconcilable through the interfaces.

**Hypothesis (inference, not fact):** the SIGTERM that produced status 143 came from the
09:30:52 kill itself — the detector's "output age 5958 s" shows no pane byte arrived before
the decision, so the `DATAWATCH_PI_FAILED`/`logout` lines necessarily postdate it; pi was
blocked inside its request for the whole window rather than crashed. Note: the "no live
child process" clause is the **expected** reading for a bare `pi --print` blocked in
generation — `hasLiveChildProcess` (cmd/datawatch/session_idle_probe.go:36-53) reports live
work only for runner *descendants* beyond its own MCP/channel servers, and the file's comment
on `hasLiveRunnerProcess` says a generating runner "may have no child process at all" (which
is why pi-prompt has the dedicated exit-silence detector, cmd/datawatch/exit_silence_wait.go).
The clause alone does not prove the runner was dead or alive.

## Evidence: session ef70

Facts from the sanctioned interfaces (daemon record `local-node-ef70`; PRD f3420001, task
bd730001):

- State `complete` (neither `failed` nor `killed` — see discrepancy below). Created
  `2026-10-28T09:30:52.578014283-04:00` — **86 ms after ab4c's kill timestamp**, i.e. the
  automatic retry. `updated_at = 2026-10-28T10:35:14.156294913-04:00`; `last_channel_event_at
  = 10:35:05.27685298`. `diff_summary = "1 file(s), +38/-0"`, workdir
  `<home>/.datawatch/prd-worktrees/f3420001` (this directory). `one_shot=true`, same node
  and model as ab4c.
- Timeline (`session_timeline`, 4 entries): `2026-10-28T13:30:52Z created`;
  `13:30:58Z running → waiting_input`, whose needs-input content is the wrapped *wrapper
  command tail* `"${dw_status:-1}" 'ollama/qwen3.8-64k:27b'; exit "${dw_status:-1}"; fi; if
  [ "$` (text from internal/llm/backends/pi/backend.go:537) — i.e. six seconds in, the pane
  contained only the typed command line, zero model output; `14:35:05Z waiting_input →
  running`; terminal state `complete` at 10:35:14 local. **No entry carries a `by=`
  attribution for the terminal transition** (contrast with ab4c).
- `session_children` → "Session ef70 has no child sessions."
- Final pane content (`session_output` tail): the full wrapper command
  (`PI_CODING_AGENT_DIR='/tmp/datawatch-pi-agent-cs-local-node-ef70-19174962' pi --print
  --mode text --approve --model 'ollama/qwen3.8-64k:27b' "$(cat
  '/tmp/datawatch-pi-task-cs-local-node-ef70-3076274759.txt')"`, scratch paths
  `/tmp/datawatch-pi-out-…-1102636443`, `/tmp/datawatch-pi-status-…-3567820164`), then the
  same two lines as ab4c: `DATAWATCH_PI_FAILED: pi exited with status 143 (model
  ollama/qwen3.8-64k:27b)` and `logout` — again a SIGTERM death of pi while running.
- Daemon verdict for this window (quoted in the 1f48 prompt via `session_output`; task
  bd730001): `Attempt 2 — Worker: pi-prompt/ollama/qwen3.8-64k:27b, Duration: 4497 s,
  Outcome: failed, Verifier summary: The diff only adds an unwired, untested
  output-freshness helper (outputIsFresh has zero call sites) and leaves every one of the 11
  rules-review findings unfixed; build, vet, and 63 targeted tests pass but none of the
  required corrections were made.` The verifier issues name the exact artifacts:
  `descendantOutputObservation` and `outputIsFresh` defined in
  cmd/datawatch/descendant_stall_wait.go but referenced nowhere; the wait loop at
  cmd/datawatch/main.go still observing only log mtime / workdir mtime / descendant count;
  internal/server/web/app-autonomous-settings-field-values.test.js absent while
  docs/testing-tracker.md (line 39) and docs/plans/2026-10-28-pi-descendant-only-stall.md
  (lines 52, 68) cite it as passing; docs/testing-tracker.md line 43 count mismatch;
  non-conventional commit type `task(f3420001):`; CHANGELOG.md line 10 wording;
  docs/implementation.md line 1599 and cmd/datawatch/descendant_processes.go line 26
  comments; phase-6 live validation pending.
- What followed (from `/api/sessions?limit=300`, PRD f3420001 / task bd730001):
  `b00c autonomous:verify:bd730001` 10:35:16→10:45:49 killed; `1f48` attempt retry
  10:45:50→12:34:14 complete (3 files, +156/−4); `f7bc autonomous:verify:bd730001`
  12:34:14→12:47:11 killed.
- Discrepancies (facts): (a) the pane shows `DATAWATCH_PI_FAILED … 143` + `logout` yet the
  session state is `complete` and the verdict is `failed` — the wrapper's terminal marker
  did not drive session state; (b) +38/−0 of real code landed in the worktree during a window
  in which the pane shows *zero* model output — pi produced partial code, then blocked.

**Hypothesis (inference, not fact):** the terminal SIGTERM at ~10:35:05 has no recorded
source (no `by=`, no nudge entry); given the operator recollection that the 5-token probe
found `qwen3.8-64k:27b` wedged at that time and that unload/reload restored it, the most
plausible trigger is operator intervention on the stuck session. **Hypothesis:** the 6-second
`waiting_input` entry classifies the idle pane (only the typed command line visible) rather
than a genuine question to the user; it predates the 120 s "session never started" bound
(session_start_confirm_seconds, internal/mcp/autonomous.go:123) and the ~2 s
`last_channel_event_at` for ab4c shows the same early classification in both sessions.

## Runner and model state

Facts from the interfaces (node `<compute-node>`, PRD f3420001):

- Runner: `pi-prompt` is one-shot, non-interactive (internal/llm/backends/pi/backend.go:15;
  internal/llm/backends/pi/backend_test.go:98 asserts `SupportsInteractiveInput()==false`).
  `Launch` (internal/llm/backends/pi/backend.go:472) runs a single blocking `pi --print`
  inside a tmux pane; the wrapper (template at internal/llm/backends/pi/backend.go:516-563)
  tees pi's stdout to a scratch file, checks exit status and non-empty output, prints
  `DATAWATCH_PI_FAILED: …` or `DATAWATCH_COMPLETE: pi done`, and — critically — runs
  `rm -f` on the task/out/status scratch files and `rm -rf` on the `PI_CODING_AGENT_DIR`
  (`/tmp/datawatch-pi-agent-cs-local-node-ab4c-2690530149`,
  `/tmp/datawatch-pi-agent-cs-local-node-ef70-19174962`) before the outcome checks.
- Bounds active at the time (deployed daemon text, cf. internal/mcp/autonomous.go:117-123):
  idle limit 900 s ("waiting_input/idle with no live child process … infra failure"),
  start-confirm 120 s; waiting-input alert semantics at
  internal/mcp/bl220_mcp_tools.go:92-94 (prompt_debounce, alert_settle). The descendant
  tracking this PRD reworks lives in cmd/datawatch/descendant_stall_wait.go,
  cmd/datawatch/descendant_processes.go, cmd/datawatch/exit_silence_wait.go
  (docs/plans/2026-10-28-pi-descendant-only-stall.md).
- Node registry (node `<compute-node>`, `/api/compute/nodes`): kind `ollama`, address
  `<compute-node>`, `node_pinned_model=qwen3.8-64k:27b`,
  `max_concurrent_sessions=1` (so ab4c and ef70 serialized on one slot),
  `scheduling_priority=90`.
- Node detail (`compute_node_detail`, queried 2026-10-29 14:52 local): aarch64 14 cores,
  kernel `6.8.12-tegra`, GPU `NVIDIA Thor` (util 97%, 40.7 W, 71 °C — **current** reading,
  not the incident window), CPU 0.5%, load1 0.94, mem 77.5/131.8 GB (58.8%), disk
  422.06/1005.31 GB (41.97%), uptime 643,281 s ≈ 7.4 days (booted ≈2026-10-22, i.e. **not
  rebooted between or after the hang window**). Envelope: backend:ollama RSS ≈13.8 GB,
  pids [3379173, 2780756]; container `c3dda340918d` = the node's datawatch daemon.
- Model state (read-only `GET http://<node-address>:11434/api/ps`, 2026-10-29 14:52):
  `qwen3.8-64k:27b` loaded — 27.3B params, Q4_K_M, 17.95 GB, context 65536,
  `expires_at 2319-02-08` (i.e. keep_alive −1, persistent); also `nomic-embed-text:latest`
  and `Gemma3:12b` loaded. This matches the operator recollection that recovery left the model
  reloaded with keep_alive −1.
- The daemon-side `ollama_stats` tool (handleOllamaStats, internal/mcp/memory_tools.go:753-766;
  fetchOllamaStatsHTTP, internal/mcp/memory_tools.go:45) queries the daemon's configured
  local Ollama host, not node `<compute-node>` — for the node, its own `/api/ps` is authoritative.
- Operator recollection (not verifiable since generation was prohibited): the 5-token probe
  to `qwen3.8-64k:27b` timed out at 60 s in both windows while a different model answered in
  7 s; node CPU ≈0% and GPU ≈0% during the hangs; unloading (keep_alive 0) and reloading
  (keep_alive −1) restored a 12 s answer.

**Hypothesis (inference, not fact):** the pattern — one pinned model wedged with zero other
load, sibling models answering, recovery by model unload/reload with no node restart and a
node uptime spanning the incident — points to the `qwen3.8-64k:27b` model instance being
stuck inside the node's Ollama engine (a blocked inference queue/request slot), with pi
clients blocked on it; both sessions hit the same instance. Not proven: no Ollama server
logs or server-side request records were available through any sanctioned interface.

## Gaps

1. **Daemon logs for the window** — `/api/logs` (internal/server/server.go:662) returned the
   full current daemon.log (31,881 lines) containing **zero** lines for `ab4c`, `ef70`,
   `f3420001`, `bd730001`, `keep_alive` or `timeout` → the 2026-10-28 07:51-10:35 window was
   rotated/truncated before this investigation; the only "hunger" evidence there is the
   `[recovery] … (state=waiting_input)` screen-capture mechanism for later sessions.
   Per-session logs named in the records (`<home>/.datawatch/sessions/local-node-ab4c/output.log`,
   `.../local-node-ef70/output.log`) are outside the project worktree and were
   intentionally not read (rule 2).
2. **Ollama server logs on <node-address>** — no sanctioned interface exposes them; the
   server-side hang point (which request held the queue, first-token timeout) cannot be pinned.
3. **pi transcripts** — the wrapper (internal/llm/backends/pi/backend.go:516-563) deletes
   `PI_CODING_AGENT_DIR` (`/tmp/datawatch-pi-agent-cs-local-node-ab4c-2690530149` and
   `/tmp/datawatch-pi-agent-cs-local-node-ef70-19174962`) before the outcome checks, so the
   session JSONL for both hangs no longer exists by design; only the pane tail (recovered via
   the `session_output` tool) survives in the daemon records.
4. **Window-state metrics** — node process history (ps/ethtool/pcap), Ollama per-request
   metrics for 07:51-09:30 and 09:30-10:35, and GPU utilization for the window are unavailable;
   `/api/ps` and the observer envelopes (internal/server/server.go:471) return current
   snapshots only (envelope `last_activity_unix_ms` equals query time).
5. **ef70's terminal SIGTERM source** — the timeline shows only `waiting_input → running` at
   10:35:05 with no `by=` attribution; operator action versus automatic timeout cannot be
   distinguished through the interfaces.
6. **ab4c kill attribution conflict** — operator recollection says a manual kill at 09:30;
   the daemon record says `by="autonomous.verifier.idle-detector"`. Unresolved.
7. **Why the generic idle detector ended both sessions first** — both were ended by the
   `autonomous.verifier.idle-detector` path (reason builder at cmd/datawatch/end_attribution.go:71-88;
   end-caller constant at cmd/datawatch/end_attribution.go:17). Given the probe semantics above
   (cmd/datawatch/session_idle_probe.go:36-53) that is consistent with a blocked bare pi, but the
   interfaces do not record why the dedicated pi-prompt detectors (exit-silence,
   cmd/datawatch/exit_silence_wait.go; the descendant-only detector of PRD f3420001 in
   cmd/datawatch/descendant_stall_wait.go) did not fire earlier — the daemon log window is
   rotated (gap 1) and no other record of those decisions survives.


## Generation limit

No generation limit is sent on the pi-prompt → node request path — no `num_predict`,
no `max_tokens`, no equivalent field, anywhere in the launch machinery. The complete
launch the daemon builds is

    pi --print --mode text --approve --model '<provider/model>' [--thinking <level>] '<task>'

assembled by `buildLaunchCmdWithAgentDir` in `internal/llm/backends/pi/backend.go`.
The only tunables that travel with the request are `--model` (mandatory — an empty
model is refused by `ErrNoModel` in the same file) and `--thinking`
(`off|minimal|low|medium|high`), which sets pi's *thinking level*, not a length cap.
The request body itself is constructed by pi (`github.com/earendil-works/pi`), outside
this repo: the daemon hands it a per-session agent directory whose only mutation is
`nodeHostProviderTable` (`internal/llm/backends/pi/backend.go`), and that function
replaces exactly one key of the named provider entry, `baseUrl`
(`nodeBaseURL` = node address + `/v1`), leaving every other key of the table untouched.
So any output-length cap in the final `/api/chat` body (Ollama's field for it is
`num_predict`) comes from pi's own defaults or from the operator's `models.json`,
never from datawatch code. Repository-wide, the string `num_predict` appears in no Go
file; `max_tokens` appears only in unrelated daemon-owned adapters that are not on the
pi → node path — `internal/inference/adapters/claude.go` (4096 for the Claude
adapter), `internal/vision/service.go` (512), and `internal/mcp/sampling.go` (a
sampling-trigger field). The daemon's own Ollama backend
(`internal/llm/backends/ollama/backend.go`, `Launch`) is likewise just
`ollama run <model>` with no token cap, and the one Ollama HTTP body the daemon
constructs — the model-load request in `internal/inference/model_load.go`
(`requestLoad`) — carries `keep_alive` (`loadKeepAlive = "10m"`), which is a
resident-model retention, not a generation length. Net: the daemon neither caps nor
relaxes the output length of the hung request; the exact limit field is whatever pi
chose, and datawatch sends none.

## Why the detectors stayed quiet

The worker in the hung window had exactly the shape the code calls out as "running":
a live pi runner process, zero work descendants ("pi generates or stalls with no child
process at all", per the `hasLiveChildProcess` doc in
`cmd/datawatch/session_idle_probe.go`), an open socket to the node, and no output,
because a one-shot run writes nothing until it completes (the `staleWorkerAppliesTo`
comment in `cmd/datawatch/stale_worker.go`). Every detector's fire condition is
*runner death + silence*, *children + silence*, *state flip + stale output*, or
*error text + family match* — none of which that shape satisfies. Detector by
detector, the signal and the condition under which it stays quiet:

- **Session-idle detector** — `internal/autonomous/session_idle.go`
  (`SessionIdleObservation.Stalled`), evaluated from the wait loop in
  `cmd/datawatch/main.go` (the `idleFor > idleLimit` block). Signals: pane wait-state
  (`waiting_input`/`idle`), `HasLiveChildProcess` — for pi-prompt that probe is
  `hasLiveRunnerProcess`, per `idleLivenessProbe` in `cmd/datawatch/exit_silence_wait.go` —
  idle duration, pane-log age (`OutputAge`), and the pi session-log age.
  `Stalled()` returns false while the runner is alive; a live, wedged, output-less pi
  is "live" in exactly this sense, so the detector sits out the whole hang and is only
  the last thing that ends the session once its gates align (it is the caller that
  actually ended both incident sessions, per the Gaps section).
- **pi session-log age** — `piLogLastEventAge`, `cmd/datawatch/pi_log_age.go`.
  Signal: age of the last event in the pi session log under the agent directory.
  It is *not a detector* — it feeds `SessionIdleObservation.LogLastEventAge`
  (`main.go`, idle-evidence block). During a wedged first request the initial events'
  age simply grows (or it reads 0 if no log is found), and on its own it never ends
  anything. Guard: pi families only.
- **Exit-silence** — `cmd/datawatch/exit_silence_wait.go` +
  `internal/autonomous/exit_silence.go` (pi-prompt only, same limit budget as the
  idle limit, `main.go` idle setup). Signal: `hasLiveRunnerProcess` + tmux-log mtime.
  `Observe` returns "not stalled" on every tick while `runnerAlive` — a wedged pi is
  alive, so its clock is reset forever; by design this detector catches the opposite
  state, "pi finished and emitted nothing".
- **Descendant stall (PRD f3420001)** — `cmd/datawatch/descendant_stall_wait.go` +
  `internal/autonomous/descendant_stall.go`, default 2700 s
  (`internal/config/descendant_stall.go`). Signal: number of *work* descendants
  (MCP/channel servers excluded), log mtime, newest workdir mtime. `Observe` returns
  not-stalled whenever `workDescendants == 0`. A wedged pi with no work children
  matches "zero descendants" and is explicitly the case built *not* to fire: the
  detector targets "runner exited with a hung child", the inverse of a live wedged
  runner.
- **stale-worker log-mtime** — `staleWorkerCheck`, `cmd/datawatch/stale_worker.go`.
  Signal: tmux log file mtime. `staleWorkerAppliesTo` excludes pi-prompt on purpose
  because `pi --print` produces no tmux log before it completes, so the file is silent
  by construction. `main.go` still calls the check, but only to populate `OutputAge`
  for the idle observation — it cannot kill the session itself for this family.
- **SSE stall** — `cmd/datawatch/sse_stall.go`. Signal: a fresh provider-error line in
  the pane (the `sseStallPatterns` list: `SSE Timeout`, `connection refused`, ...).
  Double-quiet: `sseStallAppliesTo` gates the detector to opencode* families (the
  comment states pi-prompt is deliberately out), and a wedged pi emits no lines at
  all, so there is no candidate text to match either.
- **One-shot no-output** — `internal/autonomous/one_shot_no_output.go`. Not a live
  detector: `OneShotNoOutputError` is the error type for one-shot decompose/verify
  sessions that have *already terminated* (timeout or terminal) with no output file.
  A hung session with an open connection never reaches that branch, so by
  construction it is silent during the hang.
- **Probe layer** — `cmd/datawatch/session_idle_probe.go`: `hasLiveChildProcess` /
  `hasLiveRunnerProcess`. Not a detector, but the substrate: the child probe
  documents that an idle worker's runner stays alive and a live runner is not
  "work", and every probe failure reports false, so liveness is the one fact these
  paths treat as progress.
- **Verifier no-progress watchdog** — `internal/autonomous/verifier_watchdog.go` +
  `internal/config/verifier_no_progress_timeout.go` (default 900 s, normalized via
  `session_judgment.go`). Signals: output file present, pane marker change,
  `RunnerAlive`, `ActiveRequest`, capacity-slot state. `Decide` counts `RunnerAlive`
  as fresh progress and resets the clock each tick, so a live wedged validator pi
  also keeps this watchdog quiet; `ActiveRequest` (model loaded and serving at probe
  time) is the only signal in the set that touches the node side, and it is about
  the model being resident, not about the request in flight.

Common thread: across all nine, "runner still alive" is counted as evidence of
work, and no detector reads the socket, the node, or the request in flight. The
wedged state — request admitted, zero tokens, socket open, both ends alive — matches
none of the fire conditions, which is why nothing fires during the hang and the
session-idle detector is the only path that eventually ends it.

## Explanation

Of the three candidate mechanisms, the evidence in the preceding Evidence and
Runner-and-model-state sections supports **client-side wait on a wedged model
instance** — the pi clients blocked reading a response the node's qwen instance
never produced — and it rules out the other two on the recorded facts. Runaway
generation is refuted by the node-side window: ~0% CPU and ~0% utilization (per the
Runner and model state section and gap 4 on metrics resolution), and by the pane
record — across hours, no session in either log shows any model output during the
wedged window, which a generating instance would stream through `tee` into the pane
(the pi-prompt launch tees stdout, `buildLaunchCmdWithAgentDir` in
`internal/llm/backends/pi/backend.go`). A wedged-runner hypothesis — the pi process
or its event loop itself broken, independent of the node — is not supported either:
a fresh 5-token probe from a different client times out on the same pinned model
while a sibling model on the same node answers in ~7 s (Runner and model state), so
the wedge is per model instance, not per client process; and recovery came from the
node side (unload/reload of the instance), not from anything done to the clients.
The instance-wedge is also why the detectors stayed quiet: every fire condition in
`internal/autonomous` (see the previous section) keys off liveness, descendants,
log mtimes, state, or error text — all of which a "request admitted, zero tokens,
socket open, both ends alive" state satisfies as benign. What the current evidence
cannot confirm, needed to distinguish the three cleanly: (1) an Ollama
server-side request/connection log for the qwen instance — whether pi's request was
in-flight/queued there at all (gap 2: the server log is a separate concern) — which
separates "request wedged on the node" from "client waiting for a request that is
not on the node"; (2) node-side socket state during the wedged window (packet
capture or TCP stats) to show the request reached the server and read the socket;
(3) engine-level token counters or a first/last-token marker for the instance, to
settle runaway generation independently of the host-level CPU sample (gap 4: node
metrics resolution and collection); (4) a client-side process capture of the wedged
pi during the window (stack/strace, or the blocked file descriptor), to confirm the
runner was in a read on the request socket rather than locally deadlocked — the
daemon-side liveness probes in `cmd/datawatch/session_idle_probe.go` only say "alive
or not"; and (5) the discriminating recovery experiment: whether *killing* the
in-flight request (rather than a full unload/reload, the recovery actually used in
the Runner and model state section) frees the instance — if a request kill suffices,
the wedge is state in the instance's queue/worker (consistent with
`internal/inference/model_load.go`'s `keep_alive` residency model, where a loaded
instance holds its slot); if only a full unload recovers it, the wedge is deeper in
the inference runtime. The two live-incident facts that most constrain the choice —
sibling models answering while the pinned model times out, and node CPU/GPU at
idle during the hang — both point at the instance state on the node, neither at
what the pi client is doing, and that is the narrowest claim the evidence supports.

## Parity surface

No surface changes are made by this investigation and proposal. Each surface is
intentionally excluded (this is an investigation and a proposal, so no implementation
surface is touched):

- **REST** — no new endpoints or config fields; nothing to expose.
- **MCP** — no new tools or tool-schema fields added.
- **CLI** — no new flags or commands added.
- **Comm channel** — no new events emitted beyond existing session/timeline events.
- **YAML/config** — no new config keys introduced.
- **PWA** — no settings-form or test changes.
- **Android** — not affected; investigation/proposal only.
- **iPhone** — not affected; investigation/proposal only.
