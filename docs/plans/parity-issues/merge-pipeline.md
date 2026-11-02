Issue: pending, filed at the merge

# [sync] Merge pipeline and onboarding report: GET/PUT/DELETE/POST /api/autonomous/merge-pipeline routes, GET /api/automation/onboarding-report, autonomous.merge_pipeline and onboarding_report settings, MCP, CLI, comm, PWA cards, 80 locale keys

## Body of the issue

Every project now has a merge pipeline: the ordered steps that take finished branches to shipped. It is defined by a file in the project's repository, by a record the daemon holds, or by a built-in default, and the file wins over the record. Merge conflicts are handled by an operator-chosen policy, with a selectable registry model. Separately, an onboarding report says what is configured, what is missing and what to try next. This records the exact contracts the Android and iPhone apps should render, the settings to expose and the locale keys.

### REST: merge pipeline

All four routes live under `/api/autonomous/merge-pipeline`. Reads need the same read access as the other `/api/autonomous/*` reads (the validate route is a read although it is a `POST`); `PUT` and `DELETE` need write access. A wrong method is a `405` with an `Allow` header. `503` when the daemon has no config. In every route the `project` is the project name; when omitted it defaults to the base name of the merge base repository, and when neither exists the answer is `400`.

#### `GET /api/autonomous/merge-pipeline?project=NAME`

Response body:

| Field | Type | Notes |
|---|---|---|
| `project` | string | the project the answer is about |
| `definition` | object or `null` | the pipeline in effect; `null` when none could be chosen (the file is unusable and the setting allows no substitute), in which case a merge run does not start |
| `source` | string | `file`, `record` or `default`; may be empty when no pipeline could be chosen |
| `reason` | string | plain-language reason that source was chosen |
| `file_error` | string, omitted when empty | the problem found with the project's file, reported even when another source stands in |
| `settings` | object | `file_name` (string), `source_order` (string array, never `null`), `fallback_on_bad_file` (`record`, `default` or `none`), `version_files` (string array, never `null`); each is the project's override, else the global value, else the default |
| `conflict_policy` | string | the policy configured: `stop_and_ask`, `mechanical` or `mechanical_and_model` |
| `conflict_policy_in_effect` | string | the policy acted on; `stop_and_ask` when a model is needed and not available |
| `conflict_model` | string | the registry model name configured |
| `conflict_model_in_effect` | string | the model that would be used; empty when none |
| `conflict_degraded_reason` | string, omitted when empty | why the policy in effect differs from the one configured |
| `overrides` | object or `null` | the per-project settings set for this project (the same keys as the global block, each omitted when not overridden); `null` when the project has none |
| `record_updated_at` | RFC 3339 time, omitted when empty | when the daemon's record was last set; present only when `source` is `record` |

`definition` is `{ "steps": [ ... ] }` and each step has:

| Field | Type | Notes |
|---|---|---|
| `name` | string | label shown in the run record |
| `kind` | string | `test`, `lint`, `version_bump`, `changelog`, `ship`, `tag`, `deploy`, `post_ship_hook` or `custom` |
| `command` | string, omitted when empty | a shell command line; exclusive with `action` |
| `action` | string, omitted when empty | a built-in: `check_branch_versions`, `merge_branches`, `set_release_version`, `insert_changelog_heading`, `commit_release_bump`, `tag_release`, `push_release` or `deploy_latest` |
| `dir` | string, omitted when empty | a folder inside the project; empty means the project root |
| `on_failure` | string, omitted when empty | `stop` (the default) or `continue` |
| `requires_make_target` | string, omitted when empty | run the step only if the Makefile defines that target |
| `timeout` | integer, omitted when zero | nanoseconds, as Go serializes a duration; zero means the runner's default. The app only displays it and may ignore it |

#### `PUT /api/autonomous/merge-pipeline/record`

Sets the project's daemon-held pipeline record. Body (JSON, at most 1 MiB): `project` (string; the `project` query parameter wins when both are given) and exactly one of `definition` (the object above) or `yaml` (the pipeline as YAML text). Giving both, or neither, is a `400`. A pipeline that does not validate is a `400` whose message names every problem. Response `200`: `{ "project": string, "definition": object, "updated_at": RFC 3339 time }`.

#### `DELETE /api/autonomous/merge-pipeline/record?project=NAME`

Removes the record. Response `200`: `{ "status": "deleted", "project": string }`. `404` when the project has no record.

#### `POST /api/autonomous/merge-pipeline/validate`

Checks a pipeline and saves nothing. Body (JSON, at most 1 MiB): either a pipeline (`definition` object or `yaml` text) or `file` (`true` for the project's configured file, or a file name inside the project) with an optional `project`; a pipeline and `file` together is a `400`. Response `200`, including when the pipeline is invalid:

| Field | Type | Notes |
|---|---|---|
| `valid` | boolean | |
| `errors` | string array | every problem found, never `null`; empty when valid |
| `definition` | object, omitted when invalid | the parsed pipeline |
| `source` | string | `body` or `file`, what was checked |
| `file` | string, omitted for a body | the file name that was checked |

Errors: `400` for an unreadable body or a bad `file` value; `404` when `file` is used and the project has no directory on this daemon.

Unknown fields should be ignored, as before.

### REST: onboarding report

`GET /api/automation/onboarding-report` (read-only; same read access as the other `/api/autonomous/*` reads). Query parameter `format` is optional: `json` (the default) or `text` (plain text, `text/plain`, the same text the CLI and chat print); anything else is a `400`. `503` when the daemon has no config.

JSON response body:

| Field | Type | Notes |
|---|---|---|
| `enabled` | boolean | false when `onboarding_report.enabled` is off; no check runs then and the app shows the disabled text |
| `configured` | object | see below |
| `checks` | array | one entry per check that ran, in a fixed order |
| `next_steps` | array | ordered suggestions, one per check that is not `ok`; empty when `include_next_steps` is off |

`configured`: `backends` (string array), `models` (string array), `projects` (array of `{ "name": string, "dir": string }`), `channels` (string array), `memory` (boolean) and `merge_pipeline` (array of `{ "project": string, "source": string }` where `source` is `file`, `record` or `default`).

Each entry of `checks[]`:

| Field | Type | Notes |
|---|---|---|
| `id` | string | `backends`, `models`, `projects`, `channels`, `memory` or `merge_pipeline` |
| `title` | string | |
| `status` | string | `ok`, `missing` (a required check that fails) or `optional` (an optional check that fails) |
| `reason` | string | why it passed or failed |

Each entry of `next_steps[]`:

| Field | Type | Notes |
|---|---|---|
| `check_id` | string | the check it answers |
| `title` | string | |
| `howto` | string | the how-to page to link |
| `anchor` | string | the entry in the definitions page to link |
| `command` | string, omitted when empty | the CLI command that does it |
| `mcp_tool` | string, omitted when empty | the MCP tool that does it |
| `pwa_place` | string, omitted when empty | where in the app it is done |

### New settings

`GET` and `PUT /api/config`. `PUT` takes dotted keys and checks every key before applying any, so one bad value refuses the whole `PUT` with a message naming the key and the valid values.

#### `autonomous.merge_pipeline`

Every key can also be set for one project under `autonomous.merge_pipeline.projects.<project>.<key>`; an unset key inherits the global value, then the default.

| Key | Type | Default | Valid values |
|---|---|---|---|
| `file_name` | string | `.datawatch/merge-pipeline.yaml` | a path inside the project; an absolute or escaping path is refused |
| `source_order` | string array | `["file","record"]` | JSON array or comma list of `file`, `record`; the built-in pipeline always applies last |
| `fallback_on_bad_file` | string | `record` | `record`, `default` or `none` |
| `conflict_policy` | string | `mechanical_and_model` | `stop_and_ask`, `mechanical` or `mechanical_and_model` |
| `conflict_model` | string | the built-in default model name | a name in the LLM registry; an explicit empty string means no model; never adds or changes a registry entry |
| `version_files` | string array | `[]` | project paths that carry the `var Version = "X.Y.Z"` line |


#### `onboarding_report`

| Key | Type | Default | Valid values |
|---|---|---|---|
| `enabled` | boolean | `true` | `true`, `false` |
| `include_next_steps` | boolean | `true` | `true`, `false` |
| `disabled_checks` | string array | `[]` | JSON array or comma list of check ids (`backends`, `models`, `projects`, `channels`, `memory`, `merge_pipeline`); an unknown id is ignored |

### MCP

- Merge pipeline tools: `autonomous_merge_pipeline_get` (argument `project`), `autonomous_merge_pipeline_validate` (arguments `project`, `file`, `yaml`, `definition`) and `autonomous_merge_pipeline_set_record` (arguments `project`, `yaml`, `definition`, `clear`). They return the REST bodies above.
- `onboarding_report` returns the report; argument `format` is `text` (default) or `json`.
- The settings travel through `config_set` with the dotted keys, and `autonomous_config_set` gained `merge_pipeline_<key>` arguments.

### CLI

- `datawatch autonomous merge-pipeline show [--project P]`, `validate [--repo-file]`, `set-record --from-file F` (`-` for standard input) and `clear-record`.
- `datawatch automation onboarding-report [--format json]`.
- `datawatch config set autonomous.merge_pipeline.<key> <value>` and `datawatch config set onboarding_report.<key> <value>`.

### Comm channel

- `autonomous merge-pipeline` (show), `autonomous merge-pipeline validate file`, `autonomous merge-pipeline set-record <YAML or JSON>`.
- `onboarding-report [text|json]`.
- `configure` sets the keys through the existing config path.

### PWA (reference render)

- **Merge pipeline card** in Settings > Automation > Merge pipeline: the project, the source in effect (project file, daemon record, built-in default, or none chosen) with its reason, any file problem, the steps in order (name, kind, command or built-in action, "continues on failure"), the conflict policy and model with the policy in effect, and an Edit the daemon record area (steps as JSON) with Save record and Remove record.
- **Onboarding report card** in the Automation area: the configured summary, the missing checks with their status badge (Missing or Optional) and reason, and the Try next list, each step with a how-to link, a definitions link, the command, the MCP tool and the place in the app. A Refresh control reloads it. When `enabled` is false only the disabled text shows; an empty `next_steps` list shows either the "switched off" or the "nothing to try next" text depending on `include_next_steps`.
- **Settings fields** in a Merge pipeline group and an Onboarding report group, listed below.

### Settings to add to the mobile app

| Config key | Label (English) | Type | Default | Control |
|---|---|---|---|---|
| `autonomous.merge_pipeline.file_name` | Merge pipeline file (path in the project, default .datawatch/merge-pipeline.yaml) | string | `.datawatch/merge-pipeline.yaml` | text |
| `autonomous.merge_pipeline.source_order` | Merge pipeline sources in order (comma separated: file, record; default file, record) | string array | `["file","record"]` | comma-separated text, sent as an array |
| `autonomous.merge_pipeline.fallback_on_bad_file` | Merge pipeline when the project file is bad (record, default or none; default record) | string | `record` | select: Use the daemon record / Use the built-in pipeline / Use nothing |
| `autonomous.merge_pipeline.conflict_policy` | Merge conflict policy (stop_and_ask, mechanical or mechanical_and_model; default mechanical_and_model) | string | `mechanical_and_model` | select: Stop and ask the operator / Resolve mechanically only / Mechanical, then ask a model |
| `autonomous.merge_pipeline.conflict_model` | Merge conflict model (a name in the LLM registry; empty means no model) | string | built-in default name | select of the registry's model names, plus No model |
| `autonomous.merge_pipeline.version_files` | Merge pipeline version files (comma separated project paths that carry the version line; none by default) | string array | `[]` | comma-separated text, sent as an array |
| `onboarding_report.enabled` | Onboarding report (on by default) | boolean | `true` | toggle |
| `onboarding_report.include_next_steps` | Onboarding report suggests what to try next (on by default) | boolean | `true` | toggle |
| `onboarding_report.disabled_checks` | Onboarding report checks left out (comma separated check ids; none by default) | string array | `[]` | comma-separated text, sent as an array |

The conflict model select lists the daemon's LLM registry entries and never writes to the registry.

### New locale keys

Placement of every key is under "PWA (reference render)". The `de`, `es`, `fr` and `ja` PWA bundles carry the English text as a placeholder until the translations arrive. `%1$s` is a positional placeholder, kept as written.

#### Merge pipeline (47 keys)

| Key | EN value | Context |
|---|---|---|
| `merge_pipeline_card_title` | Merge pipeline | card text |
| `merge_pipeline_placeholder` | The merge pipeline has not loaded yet. | card text |
| `merge_pipeline_failed` | Could not load the merge pipeline: %1$s | card text |
| `merge_pipeline_project` | Project | card text |
| `merge_pipeline_source_label` | Source in effect | card text |
| `merge_pipeline_source_file` | Project file | card text |
| `merge_pipeline_source_record` | Daemon record | card text |
| `merge_pipeline_source_default` | Built-in default | card text |
| `merge_pipeline_source_none` | None chosen | card text |
| `merge_pipeline_reason` | Why: %1$s | card text |
| `merge_pipeline_file_error` | Problem with the project file: %1$s | card text |
| `merge_pipeline_steps_heading` | Steps in order | card text |
| `merge_pipeline_empty` | No pipeline could be chosen, so a merge run would not start. | card text |
| `merge_pipeline_step_action` | Built-in action: %1$s | card text |
| `merge_pipeline_step_command` | Command: %1$s | card text |
| `merge_pipeline_step_continue` | continues on failure | card text |
| `merge_pipeline_conflict_policy_label` | Conflict policy | card text |
| `merge_pipeline_policy_stop_and_ask` | Stop and ask the operator | card text |
| `merge_pipeline_policy_mechanical` | Resolve mechanically only | card text |
| `merge_pipeline_policy_mechanical_and_model` | Mechanical, then ask a model | card text |
| `merge_pipeline_policy_in_effect` | In effect: %1$s. | card text |
| `merge_pipeline_conflict_model_label` | Conflict model | card text |
| `merge_pipeline_model_none` | No model | card text |
| `merge_pipeline_edit_record` | Edit the daemon record | card text |
| `merge_pipeline_record_hint` | The steps as JSON: {"steps": [...]}. The daemon checks every step before it saves. | card text |
| `merge_pipeline_save_record` | Save record | card text |
| `merge_pipeline_clear_record` | Remove record | card text |
| `merge_pipeline_record_invalid` | The record is not valid JSON: %1$s | card text |
| `merge_pipeline_saved` | Saved. | card text |
| `merge_pipeline_cleared` | Record removed. | card text |
| `merge_pipeline_save_failed` | Could not save: %1$s | card text |
| `settings_merge_pipeline_group` | Merge pipeline | settings group heading |
| `settings_merge_pipeline_file_name` | Merge pipeline file (path in the project, default .datawatch/merge-pipeline.yaml) | settings label |
| `settings_merge_pipeline_file_name_hint` | The file looked for in each project before the daemon record is used. | settings hint |
| `settings_merge_pipeline_source_order` | Merge pipeline sources in order (comma separated: file, record; default file, record) | settings label |
| `settings_merge_pipeline_source_order_hint` | The sources are tried first to last; the built-in pipeline always applies last. | settings hint |
| `settings_merge_pipeline_fallback_on_bad_file` | Merge pipeline when the project file is bad (record, default or none; default record) | settings label |
| `settings_merge_pipeline_fallback_on_bad_file_hint` | What is used when the project file exists but cannot be used. The problem is always reported. | settings hint |
| `settings_merge_pipeline_fallback_record` | Use the daemon record | select option |
| `settings_merge_pipeline_fallback_default` | Use the built-in pipeline | select option |
| `settings_merge_pipeline_fallback_none` | Use nothing | select option |
| `settings_merge_pipeline_conflict_policy` | Merge conflict policy (stop_and_ask, mechanical or mechanical_and_model; default mechanical_and_model) | settings label |
| `settings_merge_pipeline_conflict_policy_hint` | A policy that needs a model stops and asks when no model is set or the model is not available. | settings hint |
| `settings_merge_pipeline_conflict_model` | Merge conflict model (a name in the LLM registry; empty means no model) | settings label |
| `settings_merge_pipeline_conflict_model_hint` | The registry entry that proposes a resolution. This setting never adds or changes a registry entry. | settings hint |
| `settings_merge_pipeline_version_files` | Merge pipeline version files (comma separated project paths that carry the version line; none by default) | settings label |
| `settings_merge_pipeline_version_files_hint` | The release-version step edits the version line in these files. Without any, a pipeline that bumps the version fails that step. | settings hint |

#### Onboarding report (33 keys)

| Key | EN value | Context |
|---|---|---|
| `onboarding_report_card_title` | Onboarding report | card text |
| `onboarding_report_placeholder` | The onboarding report has not loaded yet. | card text |
| `onboarding_report_failed` | Could not load the onboarding report: %1$s | card text |
| `onboarding_report_refresh` | Refresh | card text |
| `onboarding_report_disabled` | The onboarding report is switched off. Turn it on in the settings to see what is configured and what is missing. | card text |
| `onboarding_report_configured_heading` | Configured | card text |
| `onboarding_report_missing_heading` | Missing | card text |
| `onboarding_report_next_heading` | Try next | card text |
| `onboarding_report_all_ok` | Every check passes. Nothing is missing. | card text |
| `onboarding_report_none` | none | card text |
| `onboarding_report_backends` | LLM backends: %1$s | card text |
| `onboarding_report_models` | Models: %1$s | card text |
| `onboarding_report_projects` | Projects: %1$s | card text |
| `onboarding_report_channels` | Channels: %1$s | card text |
| `onboarding_report_memory_on` | Memory: on | card text |
| `onboarding_report_memory_off` | Memory: off | card text |
| `onboarding_report_pipeline` | Merge pipeline: %1$s | card text |
| `onboarding_report_status_missing` | Missing | card text |
| `onboarding_report_status_optional` | Optional | card text |
| `onboarding_report_howto` | How-to | card text |
| `onboarding_report_definitions` | Definitions | card text |
| `onboarding_report_command` | Command: %1$s | card text |
| `onboarding_report_mcp_tool` | MCP tool: %1$s | card text |
| `onboarding_report_pwa_place` | In the app: %1$s | card text |
| `onboarding_report_next_off` | Suggestions are switched off in the settings. | card text |
| `onboarding_report_next_none` | Nothing to try next. | card text |
| `settings_onboarding_report_group` | Onboarding report | settings group heading |
| `settings_onboarding_report_enabled` | Onboarding report (on by default) | settings label |
| `settings_onboarding_report_enabled_hint` | Shows what is configured, what is missing and what to try next for Automation. It only reads; it never changes a setting. | settings hint |
| `settings_onboarding_report_include_next_steps` | Onboarding report suggests what to try next (on by default) | settings label |
| `settings_onboarding_report_include_next_steps_hint` | Adds an ordered list of suggestions, each with a how-to link, for every check that is not set up. | settings hint |
| `settings_onboarding_report_disabled_checks` | Onboarding report checks left out (comma separated check ids; none by default) | settings label |
| `settings_onboarding_report_disabled_checks_hint` | A check listed here is not run and does not appear in the report. An id that names no check is ignored. | settings hint |

Please send translations of the 80 keys into DE, ES, FR and JA through the usual Compose Multiplatform pipeline (use the app's own word for Automaton: de Automat, es autómata, fr automate, ja オートマトン); the PWA bundles will then mirror them in place of the placeholders.

### Acceptance (mobile)

- Settings > Automation shows the merge pipeline from `GET /api/autonomous/merge-pipeline`: source and reason, file problem, steps in order, conflict policy (and the policy in effect when it differs) and conflict model; a `null` definition shows the empty text.
- The record can be set from JSON steps through `PUT .../record` and removed through `DELETE .../record`; a refused save shows the daemon's message built from `merge_pipeline_save_failed`, and invalid JSON is caught before sending (`merge_pipeline_record_invalid`).
- The six merge pipeline settings and three onboarding report settings read their current values from the daemon config and write them back through the config endpoint; an absent field falls back to the default in the table; list fields are edited as comma-separated text and sent as arrays; a refused `PUT` shows the daemon's message.
- The conflict model control only lists registry names and never edits the registry.
- The onboarding report card renders `configured`, the missing and optional checks, and the next steps with their how-to and definitions links, command, MCP tool and place in the app; Refresh reloads it; a disabled report shows only the disabled text; a failed read shows an error line built from `onboarding_report_failed`.
- Every label, hint, badge and button is localized in EN, DE, ES, FR and JA, with every user-visible string saying Automaton or Automata.
- Unknown or absent new fields render exactly as today, so the app stays forward-compatible with older daemons.

## Filing record

Not filed yet. The supervisor checks for an open issue first (`gh issue list -R dmz006/datawatch-app --state all --search 'merge pipeline'`), files this once at the merge with `gh issue create -R dmz006/datawatch-app --title "<the title above>" --body-file <scratch draft>`, and replaces the first line of this file with the issue reference.
