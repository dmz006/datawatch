# [sync] Merge run and deploy run request switch: autonomous.merge_deploy_run_enabled

Draft only. No issue has been filed on any tracker.

## Affected area

The deploy view and the autonomous settings in the datawatch-app mobile clients (Android and iOS). The daemon, REST, MCP, CLI, chat and the PWA carry the setting.

## What the daemon does

`autonomous.merge_deploy_run_enabled` (boolean, default `false`) must be `true` for `POST /api/autonomous/merge/run` or `POST /api/autonomous/deploy/run` to act. While it is `false` both answer HTTP 403 with a plain-text message that names the setting, run nothing and write a `merge_deploy_run_refused` audit entry. Reads are unchanged. `GET /api/autonomous/deploy` has a new boolean, `run_enabled`. The setting also holds the supervisor loop jobs `run_merge_until_conflict` and `deploy_after_ship`, in daemon passes and in a hand run (`POST /api/supervisor-loop/jobs/<job>/run`), and the retry of a deferred deploy at daemon start. The operator pause holds the two jobs too. A held hand run answers HTTP 403 (setting) or 409 (pause) with a plain-text reason; a held daemon pass records the run as `ok` with a result starting `held:`. It does not hold finishing a deploy a restart interrupted.

It is accepted by YAML, `PUT /api/config` (a boolean, or the text `true` or `false`; anything else is HTTP 400), `datawatch config set`, the comm `configure` verb and MCP `config_set`.

## What the PWA does

- The deploy card disables **Run now** when `run_enabled` is `false` and shows one line naming the setting. A reply with no `run_enabled` leaves the card as before.
- Settings, Automation, Deploy group: a toggle "Allow merge run and deploy run requests" (default off) with a hint and a help link.

New locale keys, present in en, de, es, fr and ja: `deploy_run_switched_off_hint`, `settings_deploy_run_enabled`, `settings_deploy_run_enabled_hint`.

## Expected behavior in the apps

1. If the app has a Run now action for deploy, disable it when `run_enabled` is `false` and name the setting.
2. Show the daemon's 403 or 409 text if a run request or a supervisor loop job run is refused.
3. Settings: the same toggle, showing off when the value is `false` or unset; send it with `PUT /api/config`.
