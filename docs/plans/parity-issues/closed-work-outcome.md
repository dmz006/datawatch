# [sync] Closed work outcome: an outcome class on every finished Automaton, a deploy record, a standing check (datawatch next release)

Parent release: datawatch next release

Draft only, for the datawatch-app maintainers. No issue is filed.

## What changed in datawatch

"Archived" used to mix delivered, superseded and dropped work. A finished Automaton now carries an outcome class (`closed_outcome`: `delivered`, `superseded`, `dropped` or `held`) and, for delivered and superseded work, the twin Automaton that delivered or replaced it (`delivered_via`). Cancel, reject and archive record the class in the same step (a shipped record means delivered, a rejection or a cancel with work left undone means dropped). The archive step refuses a finished Automaton whose outcome cannot be proven or given, one whose outcome is held, and one with an unmet hold, so a sweep can no longer drop a hold. The deploy-after-ship step writes a deploy record (`deploy`: version, time, host, health result) onto every Automaton shipped in the release it deployed. A standing check flags work closed with no outcome, completed work with no shipped record, shipped work with no deploy record on a host that deploys after ship, a deployed version older than the shipped one, and delivered work whose evidence is no longer on the base branch.

## Settings to add to the mobile app

- `autonomous.closed_outcome_archive`: string, `on` (default), `shadow` (log only) or `off`. Select control. Applies live.
- `autonomous.closed_outcome_check`: string, `shadow` (default), `on` (also one alert per finding) or `off`. Select control. Applies live.

Both are read and written with `GET` and `PUT /api/config` and listed in `GET /api/autonomous/config` as `closed_outcome_archive` and `closed_outcome_check` (the effective values).

## Changed REST contract

- The Automaton read (`GET /api/autonomous/prds/{id}`) gains optional fields: `closed_outcome`, `delivered_via`, `closed_outcome_note`, `closed_outcome_at`, and a `deploy` object (`release`, `version`, `at`, `host`, `health` as `passed`, `failed` or `unchecked`, `detail`). Decision kinds `closed_outcome` and `closed_outcome_refused` can appear in the decision list.
- `POST /api/autonomous/prds/{id}/archive` can now answer 400 with a reason in the body: `automaton <id> cannot be archived: it has no outcome: ...`, `... its outcome is held ...`, `... it carries a hold that is not met ...`. It accepts an optional body `{outcome, delivered_via, note}` that records the outcome in the archive step. The app should show the 400 body text as the error.
- `POST /api/autonomous/prds/{id}/cancel` accepts the same optional body; `DELETE /api/autonomous/prds/{id}` accepts the same three fields as query parameters. Without an outcome the daemon derives it.
- New `POST /api/autonomous/prds/{id}/outcome` with `{outcome, delivered_via, note, actor}`: records or corrects the outcome of a finished Automaton without changing its status. 400 for an unknown class, a missing or unusable twin (the twin of a delivered record must be a stored Automaton with a shipped record), or an unfinished Automaton.
- New `GET /api/autonomous/outcome-check` (read, autonomous read capability) and `POST` (run now, write capability): `mode`, `run_at`, `scanned`, `counts`, `findings` (`automaton`, `title`, `status`, `kind`, `detail`; never null) and `proof` (`checked`, `proven`, `partial`, `unproven`). Kinds: `closed_no_outcome`, `completed_no_shipped`, `shipped_no_deploy`, `deployed_older_than_shipped`, `daemon_older_than_shipped`, `intent_unproven`.
- New `POST /api/autonomous/deploy/record` with `{release, version, host, health, detail, at}` (write capability): writes the deploy record onto every Automaton shipped in `release`.

## New locale keys with English values

| Key | English |
|---|---|
| `settings_closed_outcome_archive` | Closed outcome rule (on makes the archive step refuse work with no outcome or an unmet hold, shadow only logs, off disables; default on) |
| `settings_closed_outcome_archive_help` | Archived work must say how it ended: delivered, superseded, dropped or held. The daemon works the class out from the record where it can (a shipped record means delivered) and otherwise refuses to archive until you record one. |
| `settings_closed_outcome_check` | Closed work check (shadow records findings, on also raises an alert per finding, off disables; default shadow) |
| `settings_closed_outcome_check_help` | Once a day, and when you run it, the daemon flags work closed with no outcome, work with no shipped record, shipped work with no deploy record on a host that deploys after ship, and delivered work whose evidence is no longer on the base branch. |
| `closed_outcome_delivered` | Delivered |
| `closed_outcome_superseded` | Superseded |
| `closed_outcome_dropped` | Dropped |
| `closed_outcome_held` | Held |
| `closed_outcome_via` | via {0} |
| `release_deploy_chip` | Deployed {0} on {1} |
| `release_deploy_chip_tip` | Deployed version {0} on {1} at {2} |
| `release_deploy_health_passed` | Health check passed |
| `release_deploy_health_failed` | Health check failed |
| `release_deploy_health_unchecked` | No health check ran |
| `outcome_check_card_title` | Closed work check |
| `outcome_check_help_link` | What does this check? |
| `outcome_check_purpose` | Finds work that closed with no outcome, shipped work that was never deployed, and delivered work that is not on the base branch. |
| `outcome_check_mode_off` | The check is off. |
| `outcome_check_mode_shadow` | Shadow mode: findings are recorded, no alert is raised. |
| `outcome_check_mode_on` | On: each finding also raises one alert. |
| `outcome_check_scanned` | {0} Automata scanned. |
| `outcome_check_none` | No findings. |
| `outcome_check_more` | and {0} more |
| `outcome_check_daemon` | daemon |
| `outcome_check_finding_line` | {0}: {1} |
| `outcome_check_proof` | Intent proof: {0} checked, {1} proven, {2} partial, {3} unproven. |
| `outcome_check_run_now` | Run now |
| `outcome_check_running` | Running… |
| `outcome_check_failed` | Closed work check failed: {0} |
| `outcome_check_kind_closed_no_outcome` | Closed with no outcome |
| `outcome_check_kind_completed_no_shipped` | Completed with no shipped record |
| `outcome_check_kind_shipped_no_deploy` | Shipped, never deployed here |
| `outcome_check_kind_deployed_older_than_shipped` | Deployed version older than shipped |
| `outcome_check_kind_daemon_older_than_shipped` | Daemon older than the newest shipped release |
| `outcome_check_kind_intent_unproven` | Delivered, but the evidence is not all on the base branch |

The PWA ships real translations of all of these in de, es, fr and ja.

## Screens

- Settings, Automata: the two select rows.
- Automaton detail header: the outcome chip (class, hover shows the twin and the note) and the deploy chip (version and host, hover shows time, health and detail).
- Automata, Housekeeping: the Closed work check card (findings by kind, proof counts, Run now button, refreshed every 30 seconds).

## Not in this change

No outcome picker on the archive button in the PWA: an operator who needs to label a record uses the CLI (`datawatch autonomous prd-set-outcome`), chat (`autonomous outcome`), MCP (`autonomous_prd_set_outcome`) or REST. The mobile app needs none either; it shows the chips and the check.
