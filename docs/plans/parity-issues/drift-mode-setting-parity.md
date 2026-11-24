# [sync] File drift mode setting: autonomous.drift_mode in the settings screen

Draft only. No issue has been filed on any tracker.

## Affected area

The autonomous settings in the datawatch-app mobile clients (Android and iOS). The daemon, the PWA, the CLI, REST, the comm channel `configure` verb and the MCP `autonomous_config_set` tool already carry the setting.

## What changed in the daemon and the PWA

`autonomous.drift_mode` decides what happens when a direct-mode task's commit swept in a file that was already uncommitted before the task started.

- **Values:** `block` (the default; the task is stopped and the Automaton halts) or `warn` (the finding is recorded and the task completes). Any other value is refused with an error naming the two legal values. An unset value reads as `block`.
- **Where the key is accepted:** YAML `autonomous.drift_mode`; `PUT /api/config` with the key `autonomous.drift_mode`; `PUT /api/autonomous/config` with `drift_mode`; `datawatch config set autonomous.drift_mode <value>`; the comm `configure` verb; MCP `autonomous_config_set` with `drift_mode` (new; the schema lists `block` and `warn`). `GET /api/config` and `GET /api/autonomous/config` return the effective value.
- **Per-Automaton opt-out:** an explicit `block_on_file_drift: false` on an Automaton or its guardrail profile keeps that Automaton warn-only. That is separate from this setting.

## What the PWA does

Settings, General, Autonomous Automata: a select row "File drift mode (block | warn, default block)" with the options `block` and `warn`. Locale key `settings_drift_mode`, present in all five locales (en, de, es, fr, ja).

## Expected behavior in the apps

1. Settings: show the same select in the autonomous settings section, options `block` and `warn`, showing `block` when the daemon reports no value. Reuse the `settings_drift_mode` string in the five locales.
2. Send the value with `PUT /api/config` (key `autonomous.drift_mode`) and show the daemon's error text if one comes back.
3. Do not offer `off`: that value belongs to the separate undeclared edit mode setting.

## Compatibility

No migration is needed. Older apps ignore the key and the daemon keeps `block` unless an operator sets `warn` elsewhere.

## Tests

Daemon side: the existing config round-trip and surface tests, plus the MCP tests that check the schema, the description and the forwarding of `drift_mode`. PWA side: a node test for the settings row and the locale keys.
