# [sync] Unarchive repair setting: autonomous.housekeeping_repairs.unarchive

Draft only. No issue has been filed on any tracker.

## Affected area

The housekeeping settings and the housekeeping repairs view in the datawatch-app mobile clients (Android and iOS). The daemon, REST, MCP, CLI, chat and the PWA already carry the setting.

## What the daemon does

`autonomous.housekeeping_repairs.unarchive` (values `off`, `report`, `on`; default `report`) controls a one-time repair for one wrong outcome: an Automaton that was archived while its work branch still had commits the base branch does not have.

- **What it finds**, after the housekeeping grace: an archived Automaton whose branch is still ahead of its base, with the commit count.
- **In `on`**, for each one, re-checked just before acting: the Automaton goes back to the status it had before the archive (`completed`, or `cancelled` if it was cancelled before the archive). Only the status changes. One decision (`unarchive_unmerged`, actor `housekeeping-repair`) and one alert are written.
- **Never:** an Automaton the operator archived or cancelled stays as left; one no longer archived is skipped and reported; one whose branch cannot be checked stays archived. No session is touched, nothing is merged or shipped.
- **Once:** after the pass it records that it ran and never runs again. `report` only records what it would do; `off` does nothing.

Where it is accepted: YAML `autonomous.housekeeping_repairs.unarchive`; `PUT /api/config`; `datawatch config set`; the comm `configure` verb; MCP `config_set`. The result is part of `GET /api/autonomous/housekeeping-repairs` (group `unarchive`) and the `housekeeping_repairs` tool.

## What the PWA does

- Settings, General: a select row "Unarchive unmerged repairs" with `off`, `report` and `on` (default `report`), with help text. Locale keys `settings_housekeeping_unarchive` and `settings_housekeeping_unarchive_help`.
- The housekeeping repairs view lists the group `unarchive` under the label `housekeeping_repairs_group_unarchive`.

Locale keys are present in all five locales (en, de, es, fr, ja).

## Expected behavior in the apps

1. Settings: the same select, three options, showing `report` when no value is set. Send it with `PUT /api/config` (key `autonomous.housekeeping_repairs.unarchive`); show the daemon's refusal text if one comes back.
2. If the app shows the housekeeping repairs read, include the `unarchive` group row.
3. Reuse the locale strings above.

## Compatibility

No migration is needed. Older apps ignore the key. The default `report` changes no Automaton.

## Tests

Daemon side: the unarchive repair tests (finds, acts once, skips operator archives and unknown branch state, one decision and one alert). PWA side: node tests for the settings row and the locale keys.
