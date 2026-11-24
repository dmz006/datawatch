# [sync] Standard task packs settings fields in the settings screen

Draft only. No issue has been filed on any tracker.

## Affected area

The General settings in the datawatch-app mobile clients (Android and iOS). The daemon, the CLI, MCP and chat already manage packs; the PWA has a Standard task packs card in Settings, General (`internal/server/web/app-packs-settings-fields.js`).

## What the daemon does

Packs are installable, versioned folders of standard tasks and Automaton templates. Two settings control them:

- `packs.enabled` (default `false`): the switch. Turning it on is refused with a 409 when no skills registry source is connected and enabled.
- `packs.active` (default `["builtin"]`): the packs in use, first to last. The write is refused with a 400 when a name is empty, repeated, or neither `builtin` nor an installed pack.

A pack can also ask for inputs. They are stored per pack at a level: `global`, or `project` with a project key. A save replaces the inputs stored for that scope. Secret values stay references.

REST: `GET` and `PUT /api/packs/config` (body `{"enabled": bool}` or `{"active": [names]}`), and `GET` and `PUT /api/packs/inputs?level=&pack=[&key=]` (body `{"inputs": {"name": "value"}}`). The same settings are on the CLI (`datawatch packs enable|disable`), the MCP tool `packs_setting_set` and the chat `packs setting` command.

## What the PWA does

Settings, General, Standard task packs has six fields. Each saves on change and shows the daemon's refusal text under the field (a refused switch is put back); a network failure shows a fixed line and changes nothing.

| Field | Control | Saves through |
| --- | --- | --- |
| `packs.enabled` | switch | `PUT /api/packs/config` `{enabled}` |
| `packs.active` | text, comma separated, order kept, empty entries dropped, placeholder `builtin` | `PUT /api/packs/config` `{active}` |
| `packs.inputs.level` | text, `global` or `project`, placeholder `global` | choice only, sends nothing |
| `packs.inputs.pack` | text, pack name, placeholder `builtin` | choice only, sends nothing |
| `packs.inputs.key` | text, project key, project level only | choice only, sends nothing |
| `packs.inputs.values` | text, `name=value` pairs separated by semicolons (a value may contain `=`) | `PUT /api/packs/inputs` with the level, pack and key above |

Before sending the values the PWA checks: level is `global` or `project`, a pack name is present, a project key is present for project level, and every entry is `name=value`. Each failed check shows its own line and sends nothing.

Locale keys, present in all five locales (en, de, es, fr, ja): `settings_packs_enabled`, `settings_packs_enabled_help`, `settings_packs_active`, `settings_packs_active_help`, `settings_packs_inputs_level`, `settings_packs_inputs_pack`, `settings_packs_inputs_key`, `settings_packs_inputs_values`, `settings_packs_inputs_help`, `settings_packs_error_refused`, `settings_packs_error_network`, `settings_packs_error_level`, `settings_packs_error_pack`, `settings_packs_error_project_key`, `settings_packs_error_pairs`. The help link points to the howto `howto/automata-standard-tasks.md`.

## Expected behavior in the apps

1. Settings, General: add a Standard task packs section with the switch and the active list, saving through `PUT /api/packs/config`. Put the switch back when the daemon refuses and show its text.
2. Add the pack inputs editor (level, pack, key, values) with the same four checks before sending, and replace-the-scope semantics on save. A list or per-pack form instead of one text box is fine if the same checks and the same request are kept.
3. Never show a secret value: inputs read back from the daemon are references.
4. Reuse the locale strings above.

## Compatibility

No migration is needed. Older apps ignore the section. Packs are off by default and turning them on changes no task by itself yet.

## Tests

Daemon side: the packs config and inputs handler tests. PWA side: `app-packs-settings-fields.test.js` covers the field list, the parsers, the saves and the refusal display.
