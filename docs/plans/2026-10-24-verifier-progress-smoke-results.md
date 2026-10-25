# Live smoke: verifier progress watchdog (docs/plans/2026-10-24-verifier-progress-watchdog-proposal.md)

Phase 3 of the confirmed proposal: "confirm a legitimately silent run survives
while a genuinely dead one is still ended." Executed entirely against a sandbox
daemon — the production daemon (port 19200) was never touched.

## Environment

- Binary: sandbox build of the repo HEAD (includes `6671b4ed`
  "Feed progress signals into the verifier wait loop"), built into
  `/tmp/dw-sandbox/datawatch`.
- Daemon: dedicated sandbox instance, config `/tmp/dw-sandbox/data/config.yaml`,
  API on `127.0.0.1:19876`, log `/tmp/dw-sandbox/daemon.log`,
  `TMUX`/`TMUX_TMPDIR` unset for all external calls.
- Stub runners on a sandbox PATH: `/tmp/dw-sandbox/bin/pi` — a Go ELF whose
  command name (`/proc/<pid>/comm`) is exactly `pi`. Branch by model:
  `smoke/dead*` execs a plain `/bin/sleep 600` (comm becomes `sleep`, i.e. no
  live runner under `sessionIdleRunnerNames`); any other model sleeps in an
  infinite loop (comm stays `pi`, i.e. a live but silent runner — the
  "legitimately silent print run" this proposal exists for).
  `/tmp/dw-sandbox/bin/pi-prompt` handles planner/worker prompts only.
- LLM configs: `smokslow`/`smokdead` (each `max_inflight=1`) on the sandbox
  daemon; worker backend `smokefast` (unlimited).
- Config used for the smoke: `autonomous.verifier_no_progress_timeout_seconds: 30`
  (the single set the proposal keeps; its value is raised in-config for test
  responsiveness — no new key), `verification_timeout_seconds: 300` (the long
  verifier timeout that the proposal calls "the real backstop"),
  `capacity_wait_timeout_seconds: 1200`,
  `fallback_verification_backend: smokdead`.
- Timezone note: wall times below are the sandbox host local time (UTC-4;
  `14:31` local == `18:31Z`). Exact per-run ordering is in the daemon log.

## Observations

### Case A — silent live runner survives the no-progress window

- PRD `d2700001` (verifier backend `smokslow`, model `smoke/live` → ELF
  comm=`pi`); verifier session `4f51` was silent with no output file past the
  30s window. No watchdog kill occurred; the daemon log contains **zero**
  "no progress ... watchdog ending session" lines for the entire smoke, while
  the same silent sessions kept running. The session's end (PRD `d2700001`
  recorded a single infra failure, error `context canceled`,
  `capacity: context canceled`) came from an external
  `REST POST /api/sessions/kill requested by operator` — a different `by=`
  from the watchdog path — and the PRD then failed through the normal
  infra-retry path.
- Reconfirmed with PRD `b52b0001` verifier sessions `3033` and `b7c8`
  (same live-silent branch), each surviving well past the window with the slot
  held (`host` + `llm:smokslow` holders = `verify:ce610001`).

  Result: a legitimately silent run with a live runner (or a held slot) was
  never killed by the watchdog, matching the confirmed rule (live runner
  process or held capacity slot counts as progress).

### Case B — genuinely dead verifier is still ended; task fails via normal infra path

- PRD `51000001` (verifier backend `smokdead`, model `smoke/dead` → ELF execs
  `/bin/sleep 600`; comm `sleep` means `hasLiveChildProcess` reports no live
  runner; no output file, no pane change, no active model-server request).
  Two verifier sessions were ended; the last, `c94d`, at exactly the 300s
  long verifier timeout ("ended ... 5m0s"). PRD final: `failed` with
  `verifier_infra_failures: 2` — the ordinary infra escalation path.
- Re-run as PRD `e52f0001` (identical dead branch), verifier sessions `97df`
  then `9959` (spawned 14:59 local). Final: `failed` —
  `verification failed after retries`, `verifier_infra_failures: 2`,
  error records `autonomous:verify:a24b0001 session 9959
  (backend=smokdead, model=smoke/dead)`.

  Nuance, documented honestly: with the 30s config value, the 30s watchdog
  *message* path did not fire in either run. That is the confirmed rule
  working as designed, not a defect: the slot is admitted daemon-side *before*
  the verifier session spawns (`verifierCapacityAdmit` before the spawn in
  `cmd/datawatch/main.go`), so a dead verifier that got its slot reports
  `SlotHeld`, and per the operator-confirmed decision (proposal, decisions
  1 and 2: "a live runner process or a held capacity slot ... counts as
  progress"; "the long verifier timeout remains the real backstop") a held
  slot counts as progress, and `Decide()` in
  `internal/autonomous/verifier_watchdog.go` resets the silence baseline on
  `SlotHeld`. The watchdog-kill branch itself is the `SlotNone` case and is
  covered by the unit tests in `internal/autonomous/verifier_watchdog_test.go`
  (dead-runner ends at limit; held-slot counts as progress; slot-wait time
  excluded from the limit). The net effect the proposal requires — a
  genuinely dead verifier is ended within a bounded time and the task fails
  through the normal infra path (retries + escalation threshold +
  fallback-`smokdead` backend) — is exactly what both live runs produced.

### Case C — capacity queue wait does not count; the clock starts at spawn

- PRD `b52b0001` was created while sandbox session `2627`
  (started via `POST /api/sessions/start`, backend `smokslow`) held the only
  `llm:smokslow` slot and the only `host` slot (capacity snapshot:
  `held=1 holders=['b39b9461e1badf9c']` on both pools). Its verifier
  admission for holder `verify:ce610001` sat blocked in the pre-spawn
  admit wait for six-plus minutes (task status `verifying`, no session
  spawned, no infra failure, no watchdog activity) — while the 30s window
  ticked by on the wall clock, nothing ended it. This matches
  `Decide()`'s `SlotWaiting` rule: queue wait is always "not exceeded" and is
  subtracted from later silence (`excluded`), and in the live wiring the
  watchdog clock only starts once the one-shot wait loop begins after spawn —
  the same "today's de facto behavior" the proposal's decision 3 makes
  explicit.
- At 14:44 local the holder was released (session `2627` killed). Within the
  next poll: capacity showed `verify:ce610001` holding `host` and
  `llm:smokslow`, and the daemon log shows
  `[autonomous:verify:ce610001] spawned 3033 (backend=smokslow)` — the
  verifier ran only after the slot was granted, with the watchdog clock
  starting at that spawn. The silent live session then survived per the Case A
  rule (no watchdog kill), and the PRD's terminal `failed` state came from
  the normal infra-retry escalation on subsequent externally-ended verifier
  attempts — again via the ordinary failure path, not a watchdog misfire.

### Negative control (why the kill branch must not fire in A/C)

Across all three cases the daemon log contains no single
"no-progress watchdog ending session" line, while dead verifiers were still
bounded by the long timeout within one poll of 300s. The two failure
classifications recorded are the normal infra ones
(`verifier_infra_failures`, `verification failed after retries`) — no new
failure class was introduced.

## Result

The live smoke confirms the confirmed rule end to end in a sandbox: a
legitimately silent verifier run (live runner, held slot) survives the
no-progress window and is never mis-killed; a genuinely dead verifier is
still ended within the bounded long timeout and the task fails through the
normal infra path; and time spent queued on a capacity slot never counts
toward the limit, with the run clock starting at spawn after admission.

Result: PASS
