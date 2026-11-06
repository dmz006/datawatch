# [sync] Deploy after ship: GET /api/autonomous/deploy, POST /api/autonomous/deploy/run, autonomous.deploy settings, MCP, CLI, comm, PWA Deploy card, 38 locale keys

Issue: pending, filed at the merge

## Body of the issue

After a release ships, the daemon can build the new binary, install it, restart itself and check that it came back healthy. If it did not, it restores the previous binary, restarts again and raises a question. Every step is a setting and an empty setting means that step does nothing, so a daemon with no deploy settings never builds, installs or restarts anything. The deploy is the `deploy_latest` step of the merge pipeline and can also be started by hand. This records the exact contracts the Android and iPhone apps should render, the settings to expose and the locale keys.

### REST

Both routes answer `503` when Automata are disabled; a wrong method is a `405` with an `Allow` header.

#### `GET /api/autonomous/deploy`

Read access, the same as the other `/api/autonomous/*` reads. Response body:

| Field | Type | Notes |
|---|---|---|
| `settings` | object | the effective `autonomous.deploy` block: `build_command`, `install_command`, `binary_path`, `restart_command`, `health_check`, `drain_timeout`, `health_timeout`, all strings; an unset command is an empty string; an unset timeout reads its default (`10m0s` drain, `2m0s` health) |
| `phase` | string, omitted when no record | where the last deploy stands: `deferred`, `restarting`, `install_failed`, `restart_failed`, `done`, `rolled_back` or `rollback_failed` |
| `outcome` | string, omitted when none | the last finished outcome: `ok`, `rolled_back` or `rollback_failed` |
| `deferred` | object, omitted unless `phase` is `deferred` | `reason` (string) and `at` (RFC 3339 time) |
| `snapshot` | object, omitted when none | `time` (RFC 3339) and `sha256` (string): the identity of the saved previous binary, never its contents |
| `running` | boolean | `true` while a merge or a deploy holds the run lock |

#### `POST /api/autonomous/deploy/run`

Write access. No body. Starts the deploy sequence for the current build now. Response `200`:

| Field | Type | Notes |
|---|---|---|
| `status` | string | `skipped` (no build, install or restart command is set), `deferred` (final-gate tasks were still running when the drain timeout ended), `failed` or `restarting` (the restart command ran; the health check and any rollback finish after the daemon is back) |
| `note` | string | plain-language detail, may be empty |
| `error` | string, omitted when empty | the failure, for `failed` |

Errors: `409` when a merge or deploy is already running (the app shows the text and may retry later); `503` when the daemon has no merge base repository.

Unknown fields should be ignored, as before.

### `autonomous.deploy` settings

`GET` and `PUT /api/config`. `PUT` takes dotted keys and checks every key before applying any, so one bad value refuses the whole `PUT` with a message naming the key. A `null` value clears a key. Every key is a string.

| Key | Default | Valid values |
|---|---|---|
| `build_command` | empty: skip | a shell command line |
| `install_command` | empty: skip | a shell command line; needs `binary_path` |
| `binary_path` | empty | a clean absolute path; the previous binary is copied from here |
| `restart_command` | empty: skip | a shell command line |
| `health_check` | empty: no check | a shell command line; exit code 0 means healthy |
| `drain_timeout` | `10m` | a non-negative duration such as `10m` |
| `health_timeout` | `2m` | a non-negative duration such as `2m` |

A command that kills tmux without a throwaway socket is refused.

### Triage outcomes

A deploy is not tied to one Automaton, so it has its own small record, not the Automaton triage record. Outcomes the app should render (the `outcome` field, and `phase` for where a run stopped):

| Outcome | Meaning | Operator action |
|---|---|---|
| `ok` (phase `done`) | healthy, every session that was running came back, no capacity lease lost | none |
| `skipped` (run status) | no command set; not a failure | set commands if a deploy is wanted |
| `deferred` (phase and run status) | final-gate tasks still running at the drain timeout; nothing installed | run again, or raise `drain_timeout` |
| `rolled_back` | the restarted daemon failed health, lost a session or lost a lease; the previous binary was restored, restarted and checked again; one question is raised | answer the question |
| `rollback_failed` | the restore or the second restart failed; no further automatic action; one question is raised | inspect by hand |
| `failed` (run status; phase `install_failed` or `restart_failed`) | a build, install or restart command failed before the restart; a build failure leaves the installed binary alone | read the note and error |

The question comes from the `deploy` asker with the options **Acknowledge** (default) and **Investigate by hand**, and is answered through the existing question surfaces.

### MCP

- `autonomous_deploy_get` and `autonomous_deploy_run`, neither takes arguments; they return the REST bodies above.
- The settings travel through `config_set` and `autonomous_config_set` with the dotted keys.

### CLI

- `datawatch autonomous deploy show [--json]` and `datawatch autonomous deploy run [--json]`.
- `datawatch config set autonomous.deploy.<key> <value>`.

### Comm channel

- `autonomous deploy` (or `autonomous deploy show`) and `autonomous deploy run`; the replies render the same text as the CLI.
- `configure autonomous.deploy.<key>=<value>` sets a key.

### PWA (reference render)

- **Deploy card** in Settings > Automation > Deploy: the settings in effect (an unset one reads "(not set)"), the phase and last outcome (or "No deploy has been recorded yet."), the deferral reason, the running line, and a **Run now** button that stays disabled until a build, install or restart command is set, with the result line built from the run response.
- **Settings fields** in a Deploy group, listed below.

## Parity surface

| Surface | What it does or why it is excluded |
|---|---|
| REST | Done. `GET /api/autonomous/deploy`, `POST /api/autonomous/deploy/run`, and the `autonomous.deploy` keys on `/api/config`. |
| MCP | Done. `autonomous_deploy_get`, `autonomous_deploy_run`; settings through `config_set`. |
| CLI | Done. `datawatch autonomous deploy show|run`; settings through `datawatch config set`. |
| Comm channel | Done. `autonomous deploy [show|run]`; settings through `configure`. |
| YAML/config | Done. `autonomous.deploy` block with seven keys in `config.yaml`. |
| PWA | Done. Deploy card and Deploy settings group, with help links and the locale keys below in all five bundles. |
| Android | Pending. Render the card and settings from the contract here. |
| iPhone | Pending. Same as Android. |

### Settings to add to the mobile app

| Config key | Label (English) | Type | Default | Control |
|---|---|---|---|---|
| `autonomous.deploy.build_command` | Deploy build command (shell command line that builds the new binary; empty skips) | string | empty | text |
| `autonomous.deploy.install_command` | Deploy install command (shell command line that installs the new binary; empty skips) | string | empty | text |
| `autonomous.deploy.binary_path` | Deploy installed binary path (absolute path the install command replaces) | string | empty | text |
| `autonomous.deploy.restart_command` | Deploy restart command (shell command line that restarts the daemon; empty skips) | string | empty | text |
| `autonomous.deploy.health_check` | Deploy health check (shell command line; exit code 0 means healthy; empty means none) | string | empty | text |
| `autonomous.deploy.drain_timeout` | Deploy drain timeout (how long to wait for running final-gate tasks, default 10m) | string | `10m` | text |
| `autonomous.deploy.health_timeout` | Deploy health timeout (how long the health check may take, default 2m) | string | `2m` | text |

### New locale keys

Placement of every key is under "PWA (reference render)". The `de`, `es`, `fr` and `ja` PWA bundles carry the English text as a placeholder until the translations arrive. `%1$s` and `%2$s` are positional placeholders, kept as written.

#### Deploy card (23 keys)

| Key | EN value | Context |
|---|---|---|
| `deploy_card_title` | Deploy | card text |
| `deploy_placeholder` | The deploy settings have not loaded yet. | card text |
| `deploy_failed` | Could not read the deploy settings: %1$s | card text |
| `deploy_settings_heading` | Settings in effect | card text |
| `deploy_not_set` | (not set) | card text |
| `deploy_field_build_command` | Build command | card text |
| `deploy_field_install_command` | Install command | card text |
| `deploy_field_binary_path` | Installed binary path | card text |
| `deploy_field_restart_command` | Restart command | card text |
| `deploy_field_health_check` | Health check command | card text |
| `deploy_field_drain_timeout` | Drain timeout | card text |
| `deploy_field_health_timeout` | Health timeout | card text |
| `deploy_phase_label` | Phase: %1$s | card text |
| `deploy_outcome_label` | Last outcome: %1$s | card text |
| `deploy_none_recorded` | No deploy has been recorded yet. | card text |
| `deploy_deferred_reason` | Deferred: %1$s | card text |
| `deploy_running` | A merge or deploy is running now. | card text |
| `deploy_run_now` | Run now | card text |
| `deploy_run_now_disabled_hint` | Set a build, install or restart command in the Deploy settings to enable this button. | card text |
| `deploy_run_started` | Starting the deploy… | card text |
| `deploy_run_result` | Deploy result: %1$s | card text |
| `deploy_run_result_note` | Deploy result: %1$s (%2$s) | card text |
| `deploy_run_failed` | Deploy failed: %1$s | card text |

#### Deploy settings (15 keys)

| Key | EN value | Context |
|---|---|---|
| `settings_deploy_group` | Deploy | settings group heading |
| `settings_deploy_build_command` | Deploy build command (shell command line that builds the new binary; empty skips) | settings label |
| `settings_deploy_build_command_hint` | Runs in the repository after a ship. Empty means the build step is skipped. | settings hint |
| `settings_deploy_install_command` | Deploy install command (shell command line that installs the new binary; empty skips) | settings label |
| `settings_deploy_install_command_hint` | Replaces the installed binary. It needs the installed binary path below. | settings hint |
| `settings_deploy_binary_path` | Deploy installed binary path (absolute path the install command replaces) | settings label |
| `settings_deploy_binary_path_hint` | The previous binary is copied from here before it is replaced, so a failed restart can roll back. | settings hint |
| `settings_deploy_restart_command` | Deploy restart command (shell command line that restarts the daemon; empty skips) | settings label |
| `settings_deploy_restart_command_hint` | Runs after the install. Empty means the daemon is not restarted. | settings hint |
| `settings_deploy_health_check` | Deploy health check (shell command line; exit code 0 means healthy; empty means none) | settings label |
| `settings_deploy_health_check_hint` | Runs after the restart. A failing check rolls the binary back. | settings hint |
| `settings_deploy_drain_timeout` | Deploy drain timeout (how long to wait for running final-gate tasks, default 10m) | settings label |
| `settings_deploy_drain_timeout_hint` | A duration such as 10m. When it runs out the deploy is deferred. | settings hint |
| `settings_deploy_health_timeout` | Deploy health timeout (how long the health check may take, default 2m) | settings label |
| `settings_deploy_health_timeout_hint` | A duration such as 2m. | settings hint |

Please send translations of the 38 keys into DE, ES, FR and JA through the usual Compose Multiplatform pipeline (use the app's own word for Automaton: de Automat, es autómata, fr automate, ja オートマトン).

### Acceptance (mobile)

- Settings > Automation shows the deploy state from `GET /api/autonomous/deploy`: settings in effect, phase, last outcome, deferral reason and the running line.
- Run now calls `POST /api/autonomous/deploy/run`, is disabled while no build, install or restart command is set, and shows the result built from `deploy_run_result` or `deploy_run_result_note`; a refused or failed call shows `deploy_run_failed`.
- The seven settings read their values from the daemon config and write them back through the config endpoint; a refused `PUT` shows the daemon's message.
- Every label, hint and button is localized in EN, DE, ES, FR and JA.
- Unknown or absent new fields render exactly as today, so the app stays forward-compatible with older daemons.

## Filing record

Not filed yet. The supervisor checks for an open issue first (`gh issue list -R dmz006/datawatch-app --state all --search 'deploy'`), files this once at the merge with `gh issue create -R dmz006/datawatch-app --title "<the title above>" --body-file <scratch draft>`, and replaces the second line of this file with the issue reference.
