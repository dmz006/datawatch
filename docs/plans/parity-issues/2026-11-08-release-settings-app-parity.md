<!-- Filed 2026-11-09 as https://github.com/dmz006/datawatch-app/issues/405 (release v9.113.0). -->
# [sync] Release and milestone settings have a daemon and web surface the app does not

File 2026-11-08 as [datawatch-app#NNN]

The parent release is the daemon's current unreleased change "Settings > Automation > Merge pipeline shows the release settings" (last tagged daemon release v9.112.3), part of "Part of finishing review and merge by the daemon" (Phase 4 of the autonomy handover plan).

## Affected area

This change closes the gap on the daemon side and the web settings page — and widens the one on the app (Android + iOS) side, where the settings, the record fields and the help text do not exist at all.

This slice adds the operator surfaces for the release-size rules that the prior task of the same arc introduced (see `2026-11-08-release-size-rules-parity.md` for the resolution rules themselves). Concretely, on the daemon and web half it ships:

- Four new editable settings on the web **Settings > Automation > Merge pipeline** card: **release size by kind** (a row per Automaton kind with its size: `patch`, `minor`, `major`, `none`), **major release trigger** (`manual`, `milestone`, `never`), **release cadence** (`per_merge`; `batch` and `manual` are shown but reserved) and **version scheme** (`semver`; other schemes shown but reserved). Each control carries a help link to the Release size entry of `docs/datawatch-definitions.md`, and the new `docs/howto/release-size-and-milestones.md` explains them. A save of a reserved cadence or scheme is refused with the daemon's own reason, which names the value to use.
- The same settings over every operator channel: REST `autonomous.merge_pipeline.release_size_map`, `.major_trigger`, `.release_cadence`, `.version_scheme` (each with the `autonomous.merge_pipeline.projects.<project>.<key>` per-project form, surfaced through the merge pipeline config and pipeline-read endpoints, OpenAPI documented), the comm channel `configure` verb and the release verbs (`set-release-size`, `set-breaking-change`), the CLI (`datawatch autonomous config-set` plus the Automaton-level verbs), and MCP (`autonomous_config_set` arguments, the `autonomous_prd_set_release_size` and `autonomous_prd_set_breaking_change` tools, the `milestone` flag on Automation create and update, and `autonomous_merge_pipeline_get` now reporting the release settings in effect).
- Record fields the operator can set and read: an Automaton now carries an explicit `release_size` (patch, minor, major, none — empty resolves again) and a `breaking_change` flag (when set, the release runs in the major approval mode and is never merged quietly), and an Automation carries a `milestone` mark (a completed milestone Automation raises its release step to major under the `milestone` trigger).
- 19 new web locale keys in all five bundles (en, de, es, fr, ja): `settings_merge_pipeline_release_size_map` with `_add`, `_duplicate`, `_empty_kind`, `_help`, `_kind`, `_remove`, `_save`, `_size`; `settings_merge_pipeline_major_trigger` with `_help`; `settings_merge_pipeline_release_cadence` with `_help`; `settings_merge_pipeline_version_scheme` with `_help`; and the four size keys `merge_pipeline_release_size_patch`, `merge_pipeline_release_size_minor`, `merge_pipeline_release_size_major`, `merge_pipeline_release_size_none`.

The app half — `datawatch-app` — is where the gap is. The app has no merge pipeline settings card at all (already noted in `2026-09-14-automation-settings-parity.md`), and now it is missing three more things this slice introduced.

## Current behavior

- The daemon, the CLI, the comm channel, MCP and the web settings page all expose the release and milestone settings above, and the app talks to the same daemon — but none of it is reachable from the app.
- **Settings**: the app's settings area has no Merge pipeline card, so none of the six settings shown there (pipeline file name, source order, fallback, conflict policy, conflict model, version files) and none of the four new release and milestone settings can be read or changed from Android or iOS. On the web these four are first-class card controls with per-kind rows, help text and reserved-value refusal; on the app they do not exist.
- **Record fields**: Automaton records now carry `release_size` and `breaking_change` and Automation records carry `milestone`, and the daemon's list and detail responses (the same REST APIs the app consumes) include them. The app's Automaton and Automation screens have no UI for these fields, so an operator on the app cannot see which Automatons have an explicit release size, which are flagged as breaking, or which Automations are milestone arcs — only through the daemon surfaces.
- **Text**: the 19 locale keys listed above exist in the web bundles' five locales but in none of the app's string resources (Android string tables and iOS localized strings, all five locales each), so none of the labels, help text or size names is available in the app's languages.
- **Help**: the web card links each control to the Release size entry of `docs/datawatch-definitions.md`; the app has no link to that entry or to the new `docs/howto/release-size-and-milestones.md` anywhere, and its in-app documentation link list has no entry for it.

## Expected behavior

- The app's Automation settings area should have a Merge pipeline card (per `2026-09-14-automation-settings-parity.md`), and — because this slice landed — that card must include the four release and milestone controls: the per-kind release size rows (add a kind, choose `patch`/`minor`/`major`/`none`, remove a row), the major trigger choice, the cadence and scheme choices with `batch`/`manual` and non-semver schemes presented as reserved, the help text per control, and the refused save carrying the daemon's reason when a reserved value is stored. The per-project form of each setting should be reachable the same way the web settings reach it.
- The app should show the new record fields where Automaton and Automation details are shown: the Automaton's explicit release size and breaking-change flag, and the Automation's milestone mark. Read-only display is the minimum; the setting and clearing of `release_size` and `breaking_change` should follow the REST endpoints `POST /api/autonomous/prds/{id}/set_release_size` and `.../set_breaking_change` once the app has actions against Automatons, and marking an Automation a milestone should follow its create and update.
- The app's string resources should carry the same 19 keys in all five locales (en, de, es, fr, ja) as the web bundles, so the labels, help and size names read identically on Android and iOS.
- The app's in-app documentation link list should include `docs/howto/release-size-and-milestones.md`, and the settings card (and any Automaton detail that shows the release fields) should link to it, the way the web card already does.

## Suggested next step

1. Build the Merge pipeline settings card in Android (the app's settings module string tables, all five locale folders) and iOS (the app target's localized strings, all five locales), including the four release and milestone controls from this slice with reserved-value refusal shown as the daemon's reason, following `2026-09-14-automation-settings-parity.md`.
2. Add the 19 locale keys from the web bundles to the Android and iOS string resources in all five locales.
3. Display `release_size` and `breaking_change` on the Automaton detail and `milestone` on the Automation, reading the fields the daemon already returns.
4. Add `docs/howto/release-size-and-milestones.md` to the app's documentation link list and wire it as the help target for the release controls.
5. Where the app acts on Automatons, set and clear `release_size` and `breaking_change` through the REST endpoints named in this file, and set the Automation `milestone` flag through its create and update.
