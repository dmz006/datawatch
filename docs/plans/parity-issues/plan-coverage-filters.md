# [sync] feat(automata): plan coverage filters, suppressed count and read route

Parent release: the next patch above main at merge time.

## What changed in datawatch

The plan coverage gate no longer reports files an Automaton plan itself creates, and it no longer counts stray words from an Automaton spec as missing requirements. Both filters are configurable and on by default. The stored plan coverage record now carries a `suppressed` field with `created_files` and `requirements` counts and the `filters` set that were in effect when the record was computed, so an operator can see exactly how many findings the filters hid and under which setting. A new read route exists so the PWA and the mobile app can fetch the plan coverage record directly.

## Settings to add to the mobile app

| Config key | REST field (under `autonomous` in `GET`/`PUT /api/config`) | Label | Type | Default | Placement |
|---|---|---|---|---|---|
| `autonomous.plan_coverage_suppress_created_files` | `plan_coverage_suppress_created_files` | Plan coverage: skip files an Automaton plan creates | boolean | on | Automata settings card, next to the other plan coverage settings (plan coverage check, minimum plan coverage ratio) |
| `autonomous.plan_coverage_scoped_requirements` | `plan_coverage_scoped_requirements` | Plan coverage: count only scope and constraint sentences | boolean | on | Automata settings card, next to the other plan coverage settings |
| `autonomous.plan_coverage_requirement_sections` | `plan_coverage_requirement_sections` | Plan coverage: requirement section names | comma-separated string | `scope, constraints` (empty means this default) | Automata settings card, next to the other plan coverage settings |
| `autonomous.plan_coverage_min_requirement_chars` | `plan_coverage_min_requirement_chars` | Plan coverage: shortest requirement (characters) | whole number | 30 (shown as the placeholder; 0 or empty means use 30) | Automata settings card, next to the other plan coverage settings |

## REST contract

New route:

- `GET /api/autonomous/prds/{id}/plan_coverage` — response shape `id, status, plan_coverage, filters, suppressed{created_files, requirements, total}`. `plan_coverage` is the stored record (null when none is stored), `filters` is the live plan coverage filter set from the daemon config, and `suppressed` repeats the record's suppressed counts together with their `total` (all zeroes when no record is stored).

The two new fields inside the `plan_coverage` record, also returned by the existing plan read, are:

- `suppressed` — `{created_files, requirements}`, the count of findings each filter removed when the record was computed; zero when the filters were off at compute time or suppressed nothing.
- `filters` — the finding-filter set pinned on the record when it was computed (the same four settings), so a stored record can be told apart from the live config.

## PWA changes

The plan coverage panel shows a "Suppressed by the plan coverage filters" line with the created-file and requirement findings counts plus a `?` help link explaining what the filters do, and Settings gains the four new plan coverage settings on the Automata card (labels and help text from the locale keys below).

## New locale keys

The DE, ES, FR and JA bundles carry an English placeholder, as the Localization Rule requires. Please send translations of all keys through the Compose Multiplatform pipeline, using the app's own word for "Automaton"; the PWA bundles will then mirror them in place of the placeholders.

| Key | EN | Placeholder in de, es, fr, ja |
|---|---|---|
| `settings_plan_coverage_suppress_created_files` | Plan coverage: skip files an Automaton plan creates | Plan coverage: skip files a plan creates |
| `settings_plan_coverage_suppress_created_files_help` | When on (the default), a file that a task says it creates, or that is new in the plan's own file list inside an existing folder, is not reported as a missing file. A file whose folder cannot be created stays reported. | When on (the default), a file that a task says it creates, or that is new in the plan's own file list inside an existing folder, is not reported as a missing file. A file whose folder cannot be created stays reported. |
| `settings_plan_coverage_scoped_requirements` | Plan coverage: count only scope and constraint sentences | Plan coverage: count only scope and constraint sentences |
| `settings_plan_coverage_scoped_requirements_help` | When on (the default), only sentences in the scope and constraint sections of an Automaton spec count as requirements. Single words, file names in a read-first list and quoted phrases do not. | When on (the default), only sentences in the scope and constraint sections of the spec count as requirements. Single words, file names in a read-first list and quoted phrases do not. |
| `settings_plan_coverage_requirement_sections` | Plan coverage: requirement section names | Plan coverage: requirement section names |
| `settings_plan_coverage_requirement_sections_help` | Comma-separated section names whose sentences count as requirements (default: scope, constraints). If a spec has none of them, the whole spec is used. | Comma-separated section names whose sentences count as requirements (default: scope, constraints). If a spec has none of them, the whole spec is used. |
| `settings_plan_coverage_min_requirement_chars` | Plan coverage: shortest requirement (characters) | Plan coverage: shortest requirement (characters) |
| `settings_plan_coverage_min_requirement_chars_help` | A sentence shorter than this is not counted as a requirement. 0 uses the default (30). | A sentence shorter than this is not counted as a requirement. 0 uses the default (30). |
| `prd_plan_coverage_suppressed_title` | Suppressed by the plan coverage filters | Suppressed by the plan coverage filters |
| `prd_plan_coverage_suppressed_files` | created-file findings | created-file findings |
| `prd_plan_coverage_suppressed_requirements` | requirement findings | requirement findings |
| `prd_plan_coverage_filters_help` | What the plan coverage filters do | What the plan coverage filters do |

## Acceptance

- All four settings appear on the Automata settings card, read the current values from the daemon config and write them back through the config endpoint.
- The plan coverage panel shows the suppressed counts for the stored record and the `?` help link; with no stored record the counts read zero.
- Fetching `GET /api/autonomous/prds/{id}/plan_coverage` returns the record, the live filters and the suppressed counts with the total, and matches what the plan detail read already returns for `suppressed` and `filters`.
- Labels, help texts and the panel strings are localized in EN, DE, ES, FR and JA.

## Filing record

Filed: https://github.com/dmz006/datawatch-app/issues/322 (2026-11-27, via gh issue create -R dmz006/datawatch-app)
The issue body is the text above this section.
