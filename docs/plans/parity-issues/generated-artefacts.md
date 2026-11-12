# [sync] feat(automata): generated artefacts from the change record (settings, REST contract, Automaton detail controls)

Parent release: the next patch above main at merge time.

## What changed in datawatch

An Automaton now records what each task changed (files, packages, settings, routes, locale keys, tests and commands) and the daemon generates five drafts from that record: a gate report (counts only), a changelog entry, a datawatch-app parity issue draft, testing-tracker rows and a registry row. A person can edit any generated file and an edited file is never overwritten. Each artefact has a setting, on by default, and decompose plans at most one combined documentation task. The web app gains a settings group and, on the Automaton detail view, a read-only change record summary with a kind picker and a Regenerate button.

## Settings to add to the mobile app

| Config key | REST field (under `autonomous.generated_artefacts` in `GET`/`PUT /api/config`) | Label | Type | Default | Placement |
|---|---|---|---|---|---|
| `autonomous.generated_artefacts.change_record` | `change_record` | Change record | boolean | on | Automata settings card, group "Generated artefacts" |
| `autonomous.generated_artefacts.gate_report` | `gate_report` | Gate report | boolean | on | Automata settings card, group "Generated artefacts" |
| `autonomous.generated_artefacts.changelog_draft` | `changelog_draft` | Changelog draft | boolean | on | Automata settings card, group "Generated artefacts" |
| `autonomous.generated_artefacts.parity_draft` | `parity_draft` | Parity draft | boolean | on | Automata settings card, group "Generated artefacts" |
| `autonomous.generated_artefacts.tracker_rows` | `tracker_rows` | Tracker rows | boolean | on | Automata settings card, group "Generated artefacts" |
| `autonomous.generated_artefacts.registry_row` | `registry_row` | Registry row | boolean | on | Automata settings card, group "Generated artefacts" |
| `autonomous.generated_artefacts.combined_docs_task` | `combined_docs_task` | One combined documentation task | boolean | on | Automata settings card, group "Generated artefacts" |

## REST contract

New routes:

- `GET /api/autonomous/generated-artefacts` — the effective switches (all seven booleans) and the five kinds, each with its format description and default tracked path.
- `GET /api/autonomous/prds/{id}/change-record` — the merged change record of an Automaton: `files`, `packages`, `settings_added`, `routes_added`, `locale_keys_added` (`key`, `value`), `tests_added`, `commands` (`command`, `exit_code`, `pass`, `fail`).
- `GET /api/autonomous/prds/{id}/generated/{kind}` — preview of one artefact: rendered text, tracked path and whether the kind is enabled. Writes nothing. `kind` is one of `gate_report`, `changelog_draft`, `parity_draft`, `tracker_rows`, `registry_row`.
- `POST /api/autonomous/prds/{id}/generated/{kind}/regenerate` — re-render and write the artefact. The result carries `status` (`written`, `skipped` or `disabled`), the `path` and, for a skip, the `reason` (for example a file a person edited).

The seven switches are written through `PUT /api/config` as `autonomous.generated_artefacts.<name>`; a value that is not a boolean is a 400.

## Screens

- Settings, Automata card: a "Generated artefacts" group with one toggle and help line per setting.
- Automaton detail: a read-only change record summary (counts of files, packages, settings, routes, locale keys and tests), a "Generated artefact" kind picker and a Regenerate button that posts to the regenerate route for the chosen kind and shows a done or failed message.

## New locale keys

The DE, ES, FR and JA bundles carry the daemon's translations; please send translations through the Compose Multiplatform pipeline using the app's own word for "Automaton".

| Key | EN |
|---|---|
| `settings_generated_artefacts_group` | Generated artefacts |
| `settings_generated_change_record` | Change record |
| `settings_generated_change_record_help` | When on (the default), an Automaton records what each task changed: files, packages, settings, routes, locale keys and tests. |
| `settings_generated_gate_report` | Gate report |
| `settings_generated_gate_report_help` | When on (the default), an Automaton writes a gate report from its change record. |
| `settings_generated_changelog_draft` | Changelog draft |
| `settings_generated_changelog_draft_help` | When on (the default), an Automaton drafts a changelog entry from its change record. |
| `settings_generated_parity_draft` | Parity draft |
| `settings_generated_parity_draft_help` | When on (the default), an Automaton drafts the mobile parity issue text from its change record. |
| `settings_generated_tracker_rows` | Tracker rows |
| `settings_generated_tracker_rows_help` | When on (the default), an Automaton drafts the testing tracker rows from its change record. |
| `settings_generated_registry_row` | Registry row |
| `settings_generated_registry_row_help` | When on (the default), an Automaton drafts its registry row from its change record. |
| `settings_generated_combined_docs_task` | One combined documentation task |
| `settings_generated_combined_docs_task_help` | When on (the default), decompose plans one documentation task for an Automaton instead of several. |
| `prd_change_record_title` | Change record |
| `prd_change_record_loading` | Loading the change record… |
| `prd_change_record_empty` | No change record is stored yet for this Automaton. |
| `prd_change_record_files` | Files |
| `prd_change_record_packages` | Packages |
| `prd_change_record_settings` | Settings added |
| `prd_change_record_routes` | Routes added |
| `prd_change_record_locale_keys` | Locale keys added |
| `prd_change_record_tests` | Tests added |
| `prd_generated_kind_label` | Generated artefact |
| `prd_generated_regenerate` | Regenerate |
| `prd_generated_regenerate_done` | Artefact regenerated |
| `prd_generated_regenerate_failed` | Regenerate failed |
| `prd_generated_kind_gate_report` | Gate report |
| `prd_generated_kind_changelog_draft` | Changelog draft |
| `prd_generated_kind_parity_draft` | Parity draft |
| `prd_generated_kind_tracker_rows` | Tracker rows |
| `prd_generated_kind_registry_row` | Registry row |

## Acceptance

- All seven settings appear in the Generated artefacts group, read the current values from the daemon config and write them back through the config endpoint.
- The kind picker lists the five kinds; Regenerate calls the route and shows the result, and a skipped result shows its reason.
- The change record summary matches `GET .../change-record`.
- Labels, help texts and messages are localized in EN, DE, ES, FR and JA.

## Filing record

Not yet filed. File this text as a datawatch-app issue when the change merges.
