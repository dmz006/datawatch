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

The cap counts from when the verifier session starts. It is separate from the
no-progress timeout (`autonomous.verifier_no_progress_timeout_seconds`), which only
ends a session that shows no activity: no output file, no pane change, no live
runner process and no held capacity slot. A busy verifier that keeps producing
output is not stopped by the no-progress timeout, but the run cap still bounds its
total run time.

## Where to set it

| Method | How |
|--------|-----|
| **YAML** | `autonomous.verifier_max_run_seconds: <seconds>` under the `autonomous:` block of the config file (an explicit `0` keeps the cap off). |
| **Web UI** | Settings page > General > **Automaton verifier run cap (sec, 0=off, default 2700)**. Enter `0` to turn the cap off; leave it at the stored value to keep it. |
| **CLI** | `datawatch config set autonomous.verifier_max_run_seconds <seconds>` (whole seconds ≥ 0; `0` = no cap; refused before it reaches the daemon otherwise). Read it back with `datawatch config get autonomous.verifier_max_run_seconds`. |
| **REST API** | `PUT /api/config` with `{"autonomous.verifier_max_run_seconds": <n>}` (whole number ≥ 0; a negative or fractional value rejects the whole PUT). Read it back from `GET /api/config`. |
| **Comm channel** | `configure autonomous.verifier_max_run_seconds=<n>` — the handler refuses a negative or non-whole value before it reaches the REST setter, which validates through the same function. |
| **MCP** | `autonomous_config_set` with `verifier_max_run_seconds` (whole seconds ≥ 0; `0` = no cap; negative or fractional values are refused). |

## Current enforcement status

**The enforcing run loop has not landed yet.** No runtime code outside config
loading reads the key: it is loaded, validated and settable from every surface
above, but nothing ends a run once it hits the cap. The live bound on a verifier
run today is the no-progress watchdog plus `autonomous.verification_timeout_seconds`.
[operations.md — Verifier throughput](../operations.md#verifier-throughput) records the
same state; check it before relying on the cap to stop a run.

## Related

- [Verifier throughput in operations](../operations.md#verifier-throughput)
- [Judge routing and time limit](judge-policy.md)
