# Verifier-wait lane: sandbox live check

**Date:** 2026-10-29

Feature: a Claude lane whose Automaton is only waiting on verifier results counts as
available to the lane fill planner, per `docs/plans/2026-10-29-verifier-wait-lane-occupancy.md`.

Result: PASS
live: PASS
Daemon: http://127.0.0.1:18512 — sandbox only: loopback listener, scratch data dir, no TLS, no bearer token, no sessions; daemon recorded by PID via `scripts/test-daemon-lib.sh` and ended on every exit path.

## Sandbox daemon

Started from a scratch dir (`mktemp -d`) with a minimal config — loopback host,
port 18512, scratch `data_dir`, TLS off, no token. Lifecycle came from
`scripts/test-daemon-lib.sh` (source → `td_install_traps` → `td_start <bin> <config> <log>`);
`TD_EXTRA_CLEANUP` removed the scratch dir afterwards. The daemon was ended by its
recorded PID (`td_stop_one 1685003`); no name-based kills, and a port-close check
confirmed 18512 was released.

`GET /api/health` (abridged; only fields relevant here):

```
{"auth_required":false,"status":"ok",...}
```

## Commands (timestamps are start/end, local time)

1. Build the sandbox binary
   `[19:41:24] start`
   `env -u TMUX -u TMUX_TMPDIR go build -o <scratch>/bin/datawatch ./cmd/datawatch`
   `[19:41:26] done`

2. Start the sandbox daemon (as `<bin> start --foreground --config <scratch>/config.yaml`,
   backgrounded and recorded by `td_start`)
   `[19:41:26] start`
   daemon PID 1685003; `GET http://127.0.0.1:18512/api/health` answered `{"status":"ok"}`
   before the test ran
   `[19:41:29] ready`

3. Live smoke test, pointed at the sandbox
   `[19:41:29] start`
   `DW_LIVE_VERIFIER_WAIT_SMOKE=http://127.0.0.1:18512 env -u TMUX -u TMUX_TMPDIR go test -count=1 -run TestVerifierWaitSmokeLive -v ./internal/autonomous/`
   `[19:41:30] done (1s, rc=0)`
   observed output:
   ```
   === RUN   TestVerifierWaitSmokeLive
       lane_fill_verifier_wait_live_test.go:402: sandbox lane fill: mode=shadow lanes=0 (waiting_on_verifier=0) decisions=0
       lane_fill_verifier_wait_live_test.go:439: sandbox config: autonomous.lane_fill.count_verifier_wait = true (shipped default; the sandbox leaves the key unset)
   --- PASS: TestVerifierWaitSmokeLive (0.00s)
   PASS
   ok  	github.com/dmz006/datawatch/internal/autonomous	0.006s
   ```

4. All `TestVerifierWait*` tests, in-process (run against the final test file)
   `[19:39:41] start`
   `env -u TMUX -u TMUX_TMPDIR go test -count=1 -run 'TestVerifierWait' -v ./internal/autonomous/`
   `[19:39:47] done (6s, rc=0)`
   observed output (smoke section; the 18 pre-existing `TestVerifierWait*` tests all PASS):
   ```
   === RUN   TestVerifierWaitSmokeInProcess
       lane_fill_verifier_wait_live_test.go:111: held=ec620001 (task ec630002 waiting as verify:ec630002), ready=ec650001
       lane_fill_verifier_wait_live_test.go:194: shadow: &{ID:L3-20261029-64015 LaneID:claude-1 LaneType:claude CreatedAt:2026-10-29T23:41:52.156042129Z Action:start_prd PRDID:ec650001 TaskID:ec630002 WaitReason:waiting for node solo: 1/1 engines in use, held by occupant Executed:false Mode:shadow}
   --- PASS: TestVerifierWaitSmokeInProcess (0.01s)
   === RUN   TestVerifierWaitSmokeLive
   --- SKIP: TestVerifierWaitSmokeLive (0.00s)   (skipped without the env gate; run in step 3)
   ...
   PASS
   ok  	github.com/dmz006/datawatch/internal/autonomous	0.727s
   ```

5. Stop the sandbox daemon
   `[19:41:30] start`
   `td_stop_one 1685003` → `stopped pid 1685003`; `td_wait_port_closed 18512` → `port 18512 closed`;
   scratch dir removed by `TD_EXTRA_CLEANUP`
   `[19:41:30] done`

## What the live check stands behind

`TestVerifierWaitSmokeLive` (skipped unless `DW_LIVE_VERIFIER_WAIT_SMOKE` names a daemon;
it got `http://127.0.0.1:18512`) asserts over real HTTP, on the raw JSON:

- `GET /api/autonomous/lane-fill` carries the top-level keys `mode`, `lanes`,
  `queued_eligible`, `evidence`, `decisions`, `alerts`; `mode` is one of `off`,
  `shadow`, `on`; `queued_eligible.gpu` and `.claude` are present non-negative ints;
  `evidence` is a JSON object; `decisions` and `alerts` are JSON lists.
- Every lane in that payload carries the keys `id`, `type`, `state`,
  `eligible_waiting`; a lane reporting an occupant `waiting_on_verifier` must carry a
  non-empty `wait_reason`; a `waiting_on_verifier` lane must carry a non-empty
  `wait_reason`; shadow-mode decisions in the list must not be `executed`.
- `GET /api/config` → `autonomous.lane_fill` carries `count_verifier_wait`, it is a
  boolean, and its value **is** `true` — the test fails otherwise. The sandbox config
  leaves the key unset, so this is the shipped default.

Trimmed observed payloads from the same daemon (identical to what the test consumed):

```
GET /api/autonomous/lane-fill
{"mode":"shadow","last_poll_at":"0001-01-01T00:00:00Z","lanes":[],
 "queued_eligible":{"gpu":0,"claude":0},
 "evidence":{"checked_at":"0001-01-01T00:00:00Z","gpu_active_sessions":0,"loaded_models":[],"error":""},
 "decisions":[],"alerts":[]}

GET /api/config — autonomous.lane_fill
{"claude_backend":"claude-code","claude_lanes":3,"count_verifier_wait":true,
 "decompose_ahead_min":2,"gpu_backend":"pi-prompt","gpu_lanes":2,"gpu_model":"",
 "idle_alert_minutes":30,"mode":"shadow","poll_seconds":60}
```

The in-process smoke (`TestVerifierWaitSmokeInProcess`, always runs) is the part that
produces the live surface's waiting state and checks the behavior end to end:

- One Claude lane, one-slot pool, held Automaton whose single task is queued on the
  verifier slot (holder `verify:<task id>` — one engine in use in a one-engine pool),
  ready Automaton with eligible work.
- Shadow poll: the held lane is `state=waiting_verifier` with the occupant's wait
  reason — observed `waiting for node solo: 1/1 engines in use, held by occupant`
  (the lane's own slot text) — and a `start_prd` decision is recorded for the ready
  Automaton, planned for that lane, unexecuted, with that same reason in
  `wait_reason`; the starter is never called; the held Automaton is not planned.
- Switch to `mode: on`: exactly one start (`run:<ready>`), the ready Automaton is
  running on the lane; a follow-up poll starts nothing more, and the ready Automaton
  started exactly once.

The wait reason format comes from `wait_reason.go`: `waiting for node <node>:
<used>/<limit> engines in use, held by <holder>`.

## What was not covered live

- A lane actually reporting `waiting_on_verifier: true` over HTTP was not observed:
  the sandbox has no running Automata or pool sessions, so `lanes` came back empty.
  The field names, value types and the `waiting_on_verifier` → `wait_reason`
  implication are asserted on the wire, and the full state-and-behavior path is
  covered by the in-process smoke above with the exact fields the live surface serves.
- Non-empty `decisions` / `alerts` payloads over HTTP (empty in the fresh sandbox);
  their list types are asserted.
- The GPU lane surface and the TLS/token paths (sandbox: 0 GPU sessions, plain HTTP
  loopback, no token).

## Result

Result: PASS
live: PASS
live `waiting_verifier` lane observed over HTTP: not observed (sandbox had no running Automata; state and behavior covered by the in-process smoke, wire names asserted live)
