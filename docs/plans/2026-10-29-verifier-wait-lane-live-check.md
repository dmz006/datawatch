# Verifier-wait lane: sandbox live check

**Date:** 2026-10-29
**Version:** next minor above main (set at merge time)

Feature: a Claude lane whose Automaton is only waiting on verifier results counts as
available to the lane fill planner, per `docs/plans/2026-10-29-verifier-wait-lane-occupancy.md`.

## What was checked

### In-process smoke (always run, real store)

`TestVerifierWaitSmokeInProcess` in
`internal/autonomous/lane_fill_verifier_wait_live_test.go`:

- One-slot pool, one Claude lane, `autonomous.lane_fill.mode: shadow`.
- Held Automaton: one task held running while its verifier wait is queued on the pool
  (holder `verify:<task id>`). Ready Automaton: eligible work, Claude preference.
- Poll 1 (shadow): the lane reports `state=waiting_verifier` with the occupant's wait
  reason (`waiting for slot: 1/1 engines in use, held by occupant`), the planner records
  `start_prd` for the ready Automaton **with the reason in `decision.wait_reason`**, and
  the fake starter is never called — shadow starts nothing.
- Poll 2 (shadow): nothing new — the planner waits for the held verifier, no repeat
  decisions, no starts.
- Switch to `mode: on`, poll: exactly one Automaton starts (the ready one) on the one
  Claude lane, and no second start is recorded on the follow-up poll.
- Live-wait reason text uses the lane's own slot reason — the lane's occupant was
  waiting and the ready item started behind it.

Result: PASS (`ok github.com/dmz006/datawatch/internal/autonomous`, all
`TestVerifierWait*` tests green, including the pre-existing alert-episode, planner,
record and JSON-name tests).

### Live (opt-in, sandbox daemon)

Opt-in gate: `DW_LIVE_VERIFIER_WAIT_SMOKE=<base URL>` starts a sandbox daemon
(`datawatch start --foreground --config <sandbox config>`) in an isolated scratch data
dir, waits for `GET /api/health`, and exercises:

- `GET /api/autonomous/lane-fill`: the surface answers and the exact JSON field names
  from the plan are present — `mode`, `lanes` (each lane carries `occupant`,
  `waiting_on_verifier`, `wait_reason`, `eligible_waiting`), `queued_eligible`,
  `evidence`, `decisions`, `alerts`. With no sessions in the sandbox, the lanes list is
  empty, so no lane is in the waiting-on-verifier state live.
- `GET /api/config`: the `autonomous.lane_fill` block names
  `count_verifier_wait`, unset in the sandbox config, served as `true` — the default
  the plan says is the default-true case.
- `TestVerifierWaitSmokeLive` asserts both over HTTP; with the env gate unset it skips.

live: PASS

A live verifier-wait lane (a lane actually reporting `waiting_on_verifier: true` over
HTTP) was NOT RUN as a live observation: the sandbox daemon has no running Automata or
pool sessions, so no occupant can be in the verifier-wait state there. The in-process
smoke above produces that exact state and asserts every field that the live surface
would serve, so the behavior is covered without a live reproduction.

## Result

Result: PASS
live: PASS
live verifier-wait lane observed over HTTP: NOT RUN (sandbox has no running Automata; covered in-process)
