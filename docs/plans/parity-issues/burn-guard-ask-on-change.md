# Parity issue draft — burn guard ask_on_change (datawatch-app)

To file on **datawatch-app** at merge, as a follow-up to dmz006/datawatch-app#332 (`[sync] feat(usage): burn guard status, settings and alerts`). If #332 is not yet merged, fold this into #332.
Parent release: the next patch above main at merge time.

## What changed in datawatch

The burn guard gets a new setting, `ask_on_change` (boolean, default `false`), under `usage.burn_guard`. When false (the default) the guard raises one alert per change and no question; when true it keeps at most one open question per change until it is answered, instead of alerting. The state is read from the same config endpoints as the other burn guard keys and saved with the same PUT semantics: one invalid value rejects the whole change and stores nothing.

This repository's other surfaces already carry the key: the REST config endpoints and `usage.burn_guard` under `/api/config`, the MCP tools, the CLI `datawatch usage burn-guard config`, and the comm channel `configure` path. The PWA Burn guard card (usage calculator view) now renders the setting as a checkbox next to the other burn guard fields, bound to the config read and writes it back through the same PUT, with a `?` help link to the Settings section of `docs/howto/burn-guard.md`. Howto: `docs/howto/burn-guard.md` (Settings section).

This is a settings-parity note: the app's burn guard settings UI needs one more row.

## Setting (read and write)

| Key | Label | Type | Default |
|---|---|---|---|
| `ask_on_change` | Ask instead of alerting (off: one alert per change, no question; on: at most one open question per change) | boolean | `false` |

Read it with the rest of the burn guard config (`GET /api/usage/burn-guard/config` or `usage.burn_guard` under `GET /api/config`) and write it back with the same `PUT`; a boolean only, so validation is trivial, but show the daemon's rejection message like the other burn guard keys.

## Locale keys

The app picks up the key from the five bundles (`en`, `de`, `es`, `fr`, `ja`) with the same names as in `internal/server/web/locales/en.json` of the datawatch repository: the field label `burn_guard_ask_on_change`, the settings-screen label `settings_usage_burn_guard_ask_on_change`, and the help text `burn_guard_ask_on_change_help`.

## Acceptance

- The new toggle loads, saves and round-trips with the other burn guard settings.
- An invalid value shows the daemon's rejection message and changes nothing.
- The label and help text come from the locale bundles, with English fallback.

## Parity surface

| Surface | Included | Reason or where |
|---|---|---|
| REST | Already done | `GET`/`PUT /api/usage/burn-guard/config`, `usage.burn_guard.ask_on_change` under `GET`/`PUT /api/config` |
| MCP | Already done | `burn_guard_config_get`, `burn_guard_config_set` |
| CLI | Already done | `datawatch usage burn-guard config ask_on_change=<true\|false>` |
| comm channel | Already done | `configure usage.burn_guard.ask_on_change=<true\|false>` |
| YAML/config | Already done | `usage.burn_guard.ask_on_change` in `docs/config-reference.yaml` |
| PWA | Already done | Burn guard card checkbox on the usage calculator view, Settings > Usage burn guard, and the help link to the howto Settings section |
| Android | Yes | this issue: the settings row plus the label/help locale keys |
| iPhone | Yes | the same issue text applies once the iOS app exists; there is no iOS app in this repository's release set, so it is tracked on the same issue |
