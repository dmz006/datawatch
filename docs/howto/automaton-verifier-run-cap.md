---
docs:
  index: true
  topics: [autonomous, verifier, run cap, wall-clock, timeout, verification, worker slot, throughput]
exec_steps:
  - tool: autonomous_config_set
    description: Set the verifier run cap in whole seconds (0 = no cap; a negative or fractional value is refused, so is any value that would truncate the stored cap)
    args: {verifier_max_run_seconds: "{{params.seconds}}"}
    read_only: false
---

# Automaton verifier run cap

The verifier run cap is the total wall-clock limit on one verifier session that
checks an Automaton's work. It is set with `autonomous.verifier_max_run_seconds`
and exists so a single verifier that keeps producing output — and therefore never
trips the no-progress watchdog — cannot hold a worker slot for hours while other
work waits.

## What it bounds

- **Key:** `autonomous.verifier_max_run_seconds`
- **Default:** `2700` seconds (45 minutes)
- **`0`:** the cap is off. An explicit `0` stays `0`; the field has no `omitempty`,
  so it round-trips as an explicit key through YAML and is never replaced by the default.
- **Negative or non-whole values:** refused. A negative value fails the config load;
  a negative or fractional value sent through a settings surface is rejected (the REST
  `PUT` names the key: "must be a whole number of seconds (0 = no cap)").

The cap counts run time from the verifier's capacity grant; time spent waiting for a
slot is not counted. It is separate from the no-progress timeout (`autonomous.verifier_no_progress_timeout_seconds`), which only
ends a session that shows no activity: no output file, no pane change, no live
runner process and no held capacity slot. A busy verifier that keeps producing
output is not stopped by the no-progress timeout, but the run cap still bounds its
total run time.

## Where to set it

There are five places to set the cap at runtime: the web Settings page, the REST API,
the comm `configure` verb, the MCP tool and `datawatch config set`. The config file is
the sixth place, for a daemon's starting value.

| Method | How |
|--------|-----|
| **YAML** | `autonomous.verifier_max_run_seconds: <seconds>` under the `autonomous:` block of the config file (an explicit `0` keeps the cap off). |
| **Web UI** | Settings page > General > **Automaton verifier run cap (sec, 0=off, default 2700)**. Enter `0` to turn the cap off; leave it at the stored value to keep it. |
| **CLI** | `datawatch config set autonomous.verifier_max_run_seconds <seconds>` (whole seconds ≥ 0; `0` = no cap; refused before it reaches the daemon otherwise). Read it back with `datawatch config get autonomous.verifier_max_run_seconds`. |
| **REST API** | `PUT /api/config` with `{"autonomous.verifier_max_run_seconds": <n>}` (whole number ≥ 0; a negative or fractional value rejects the whole PUT). Read it back from `GET /api/config`. |
| **Comm channel** | `configure autonomous.verifier_max_run_seconds=<n>` — the handler refuses a negative or non-whole value before it reaches the REST setter, which validates through the same function. |
| **MCP** | `autonomous_config_set` with `verifier_max_run_seconds` (whole seconds ≥ 0; `0` = no cap; negative or fractional values are refused). |

## How it is enforced

The cap is enforced. On every poll of a verifier session, datawatch adds the time
since the previous poll to a run-time total and ends the session once the total
reaches `autonomous.verifier_max_run_seconds`.

- **Progress-blind:** output, pane changes and a live runner do not extend the cap. A
  verifier that keeps working is still ended at the limit.
- **Queue time is not counted:** time a verifier spends waiting for a capacity slot is
  left out of the total, so the cap measures the run, not the queue.
- **Off with `0`:** no run is ever ended by the cap.
- **Verifier sessions only:** decompose and worker sessions are not bound by it.
- **Read once per session:** the value is read when the verifier session starts, so a
  change applies to the next session, not one already running.

A cap end is counted under the `run_cap` verifier category, not under `infra`, and it is
not a judgment about the task's work. It is still verifier trouble: the session was cut
short, so the verification is retried as described below.

## How many cap ends you have seen

The capacity API (`GET /api/capacity`) carries a `verifier` object with the rolling
verifier aggregates since the daemon started. Two fields cover the cap:

- `run_cap_seconds`: the cap in effect (`0` = no cap). It is read from the loaded
  config at daemon start, so it reflects the configured value, not a default.
- `run_cap_ends`: how many verifier runs the cap ended. It is the same count as the
  `run_cap` category in `categories`; a cap end is counted under `run_cap` and not
  under `infra`.

The status text line adds `, run_cap N` after the `watchdog` count once N is above
zero, for example `verifiers: 12 runs, avg wait 4s, avg run 9m, infra 0, watchdog 1, run_cap 2`.
The counters reset with the daemon; the cap itself is configuration and is not reset.

## What a cap end records

When the cap ends a verifier session, datawatch frees the verifier's capacity slot
at once, so queued work can take it before the retry starts. It writes a
`verifier_run_cap` entry in the Automaton's decision log, with the task id, the
elapsed seconds and the cap in seconds, and a matching audit record. The task's
attempt ends with the reason `verifier_max_run`, so you can tell a cap end from a
crashed session.

## Retry after a cap end

A verifier session ended at the cap is retried **once**, on the next backend in the
verifier policy order after the one that was cut. The retry backend runs on its
registry default model. A cut run is **not a failed task**: the cap ends a verifier
session, not the Automaton's work, so the verification runs again on the retry. If the
retry was already spent, or the policy lists only one backend, there is no retry and the
verification ends as an infrastructure result (not a content verdict), which the usual
infra escalation handles.

## Related

- [Verifier throughput in operations](../operations.md#verifier-throughput)
- [Judge routing and time limit](judge-policy.md)
- [Verifier slots on compute nodes](compute-nodes.md#verifier-slots)

## Operator review

- Raise the cap if a verification legitimately runs longer than 45 minutes; a cap
  that is too low turns every long verification into a cap end plus one retry.
- Single-backend verifier policies get no retry after a cap end.
- Look for `verifier_run_cap` entries in the decision log and `autonomous.verifier_run_cap`
  audit records to see how often the cap fires.
- **For the operator to confirm:** the retry model. This page says the retry runs on the
  backend's registry default model; the code resolves it through a `modelFor` callback
  (`cmd/datawatch/verifier_run_cap_retry.go`), which this page did not trace. Confirm the
  wording against the live resolver before release.
- **For the operator to confirm:** the default of 2700 seconds against real verification
  durations on the current verifier backends. The default has not been tuned from data here.
