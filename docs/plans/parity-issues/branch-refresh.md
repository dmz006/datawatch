# [sync] Branch refresh: Automaton page card, override route, conflict question and stats route

Draft only. No issue has been filed on any tracker.

## Affected area

The Automata branch refresh in the daemon and every client that shows an Automaton's detail or sets its options: the PWA (already updated), the Android and iOS apps (datawatch-app), the CLI, MCP and the comm channel.

## What changed in the daemon

In the gap between tasks, an Automaton whose branch has fallen behind its base gets the base merged in. It is off by default.

- **Settings:** `autonomous.branch_refresh_enabled` (default false) and `autonomous.branch_refresh_behind_limit` (whole number of 0 or more; 0 reads as 5), on YAML, `PUT /api/config`, `datawatch config set`, the comm `configure` verb and MCP `config_set`.
- **Per-Automaton override:** `POST /api/autonomous/prds/{id}/branch-refresh` with `{"enabled": true|false|null}` (null clears it); MCP `autonomous_prd_set_branch_refresh`; CLI `datawatch autonomous branch-refresh <id> enabled|disabled|inherit`; chat `autonomous branch-refresh`. Setting the override also resets a refresh that a merge conflict stopped.
- **Automaton read:** `GET /api/autonomous/prds/{id}` carries `branch_refresh` (`last_refresh_at`, `commits_behind`, `last_result`, `last_main_commit`, `stopped`; null when none has run), `branch_refresh_override` and `branch_refresh_enabled`.
- **Conflict question:** a conflict stops refreshing for that Automaton and raises one question asked by `branch-refresh` with the options `resolve_by_hand`, `retry_later` and `leave_alone` (the default). Answering `retry_later` allows refreshing again; the other two keep it stopped. Each answer is a `branch_refresh` entry on the decision log.
- **Numbers:** `GET /api/autonomous/branch-refresh/stats` (MCP `autonomous_branch_refresh_stats`, CLI `datawatch autonomous branch-refresh-stats`, chat `autonomous branch-refresh-stats`) returns `enabled`, `behind_limit`, `tracked`, `clean`, `conflict`, `skipped`, `error`, `stopped`, `override_on`, `override_off`. The live stats broadcast carries `branch_refresh_clean`, `branch_refresh_conflict`, `branch_refresh_error` and `branch_refresh_stopped`.

## What the PWA already does

- Settings: the two settings as a toggle and a number field.
- Automaton page, Overview tab: a Branch refresh card with the last refresh time, the commits behind, the last result, whether refreshing is active or stopped by a conflict, and an override select (daemon default, always refresh, never refresh).
- New locale keys in all five locales (en, de, es, fr, ja): the `branch_refresh_*` keys, including `branch_refresh_state_label`, `branch_refresh_state_stopped` and `branch_refresh_state_active`.

## Expected behavior in the apps

1. Settings: show both settings in the autonomous section; reject a negative or non-whole limit before sending.
2. Automaton detail: show the card above, with the stopped state, and send the override as true, false or null.
3. Questions: the conflict question is an ordinary question with three options; show it like any other and do nothing special on answer, since the daemon applies the choice.
4. Decision log: an entry of kind `branch_refresh` should read as a refresh note, not as an error.
5. If the apps show a stats screen, the four `branch_refresh_*` counters are optional.

## Compatibility

Every field is new and optional. An older app ignores them and keeps working; an Automaton that is not stopped carries no `stopped` field.
