# [sync] Verifier run cap: new setting, cap-ended verifier runs, and new fields on the capacity read

Draft only. No issue has been filed on any tracker.

## Affected area

The Automata verifier in the daemon and every client that edits the autonomous settings or reads verifier status: the PWA (already updated), the Android and iOS apps (datawatch-app), the CLI, MCP and the comm channel `configure` verb.

## What changed in the daemon

Every verifier session now has a hard maximum run time. It is counted from the moment the verifier is granted capacity, so queue wait does not count, and it applies even while the verifier keeps producing output (the older no-progress timeout only ends a session that goes quiet).

- **Setting:** `autonomous.verifier_max_run_seconds`, a whole number of seconds. Default 2700 (45 minutes). `0` turns the cap off. A negative or non-whole value is refused on every surface; the REST write answers 400 and the config loader errors when it reads a negative value from YAML.
- **What happens at the cap:** the verifier session is ended and its worker slot is freed at once. The Automaton's decision log gets an entry of kind `verifier_run_cap`. The task's last attempt ends with reason `verifier_max_run`. The verification is retried once on the next backend in the verifier policy order; a single-backend policy gets no retry. A cut run is recorded as verifier infrastructure trouble, never as a content failure. An audit entry with action `autonomous.verifier_run_cap` is written (CEF signature 86, name `AutonomousVerifierRunCap`, severity 4).
- **Where the key is accepted:** YAML `autonomous.verifier_max_run_seconds`; `PUT /api/config` with the key `autonomous.verifier_max_run_seconds`; `datawatch config set autonomous.verifier_max_run_seconds <seconds>`; the comm `configure` verb; MCP `autonomous_config_set` with `verifier_max_run_seconds`. `GET /api/config` returns it under `autonomous.verifier_max_run_seconds`.

## New fields on the capacity read

`GET /api/capacity` carries the rolling verifier aggregates under `verifier`. Three additions:

- `verifier.run_cap_seconds`: the cap in effect, in seconds, `0` meaning no cap. Note this is a different field from the existing `verifier.run_max_seconds`, which is the longest observed run and was not renamed or repurposed.
- `verifier.run_cap_ends`: how many verifier runs were ended by the cap since the daemon started.
- `verifier.categories.run_cap`: the same count as one more outcome category, next to `pass`, `content`, `infra`, `watchdog`, `timeout`, `capacity` and `escalated`.

The verifier status text line adds `run_cap N` after the `watchdog` count once N is above zero.

## What the PWA already does

- Settings, General: a number field "Automaton verifier run cap (sec, 0=off, default 2700)", minimum 0, placeholder 2700, with help text and a help link to the howto `howto/automaton-verifier-run-cap.md`.
- New locale keys, present in all five locales (en, de, es, fr, ja) with the same key set:
  - `settings_verifier_max_run_seconds`: "Automaton verifier run cap (sec, 0=off, default 2700)"
  - `settings_verifier_max_run_seconds_help`: "Total wall-clock limit for one verifier session, counted from when it starts. It is separate from the no-progress timeout above, which only ends a session that shows no activity. 0 turns the cap off. The default is 2700 seconds (45 minutes)."

## Expected behavior in the apps

1. Settings: show the same field in the autonomous settings section, in seconds, with 0 meaning no cap. Reject negative and non-whole values before sending, and show the daemon's error text if one comes back. Reuse the two locale strings above.
2. Verifier status: if the apps render the verifier block of the capacity read, show `run_cap_seconds` (or "no cap" for 0) and the `run_cap` outcome count, and do not confuse `run_cap_seconds` with `run_max_seconds`.
3. Automaton detail: a decision-log entry of kind `verifier_run_cap` and a task attempt end reason `verifier_max_run` should read as "verifier ended at its run cap", not as a failed verification. If the app has a fixed list of end reasons or decision kinds, add both.

## Compatibility

No migration is needed for the apps. Older apps ignore the new config key and the new capacity fields. The 45-minute default applies on upgrade, so a verification that legitimately runs longer is now cut; the operator raises the cap or sets 0 to get the old unbounded behavior.

## Tests

Daemon side: unit tests for the config validation, the cap enforcement in the verifier watchdog, the one-time retry on the next backend, the decision and audit entries, the stats counting, and the REST, CLI, MCP and comm surfaces. PWA side: a node test for the settings field and the locale keys. Live-daemon validation of an actual cap-ended session was not run before release.
