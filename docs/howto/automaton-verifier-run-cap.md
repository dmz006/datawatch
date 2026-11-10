# Automaton verifier run cap

The verifier run cap is the total wall-clock limit on one verifier session that
checks an Automaton's work. It is set with `autonomous.verifier_max_run_seconds`.

## What it bounds

- **Key:** `autonomous.verifier_max_run_seconds`
- **Default:** `2700` seconds (45 minutes)
- **`0`:** the cap is off. An explicit `0` stays `0`; it is never replaced by the default.
- **Negative values:** refused when the config loads.

The cap counts from when the verifier session starts. It is separate from the
no-progress timeout (`autonomous.verifier_no_progress_timeout_seconds`), which only
ends a session that shows no activity: no output file, no pane change, no live
runner process and no held capacity slot. A busy verifier that keeps producing
output is not stopped by the no-progress timeout, but the run cap still applies to
its total run time.

## Where to set it

- **Settings page:** Settings > Automaton > *Automaton verifier run cap*. Leave the
  field empty to use the default, or enter `0` to turn the cap off.
- **Config file:** the `autonomous.verifier_max_run_seconds` key.
- **Automaton config surfaces:** the autonomous config tools read and set the same key.

## Current enforcement status

The definitions page ([datawatch-definitions.md](../datawatch-definitions.md)) records
the current state of enforcement. At the time this page was written, the run loop that
enforces the cap had not yet landed, so the live bound on a verifier run is the
no-progress watchdog plus `autonomous.verification_timeout_seconds`. Check that page
before relying on the cap to stop a run.

## Related

- [Verifier throughput in operations](../operations.md#verifier-throughput)
- [Judge routing and time limit](judge-policy.md)
