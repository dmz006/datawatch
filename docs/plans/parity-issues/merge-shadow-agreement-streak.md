# [sync] Merge shadow agreement streak: new setting and a streak on the shadow status

Draft only. No issue has been filed on any tracker.

## Affected area

Every client that reads the merge shadow status or edits the autonomous settings: the PWA (already updated), the Android and iOS apps (datawatch-app), the CLI, MCP and the comm channel.

## What changed in the daemon

- `GET /api/autonomous/merge/shadow` (MCP `autonomous_merge_shadow_status`) now carries, per project, a `streak` object: `length` (the most recent consecutive stored comparisons with no major difference), `total` (stored comparisons), `threshold` and `met`. It is absent while a project has no stored comparisons. It is a read, not a gate: nothing is approved or switched by it.
- New setting `autonomous.merge_shadow.agreement_streak` (whole number >= 1, default 5): the threshold. Accepted in YAML, `PUT /api/config`, `datawatch config set`, comm `configure` and MCP `autonomous_config_set`; a value below 1 or not whole is refused on every surface.
- CLI `datawatch autonomous merge-shadow status` prints `agreement streak N of T (met|not met, C compared)` per project; the comm verb `autonomous merge-shadow` adds `streak: a 2/3 not met, b 4/3 met` (project, length/threshold, met or not met) to its summary line.
- An `intermittent` test gate now counts as a pass when the plan is compared with the supervisor's manual gate record, and the lint gate runs a pinned golangci-lint from the daemon's own tools folder (new finding code `lint_tool_unavailable`). These are daemon-side only.

## What the PWA already does

- Settings, Merge shadow card: the number field "Merge shadow agreement streak threshold (whole number, minimum 1, default 5)" with help text and a help link to the merge shadow how-to.
- New locale keys in en, de, es, fr and ja: `settings_merge_shadow_agreement_streak` and `settings_merge_shadow_agreement_streak_help`.

## Expected behavior in the apps

1. Settings: add the field to the Merge shadow section with the same bounds and reuse the two locale strings.
2. If an app renders the shadow status, show the streak (length of total, threshold, met) per project; treat a missing `streak` as "no comparisons yet".
3. Do not present the streak as an approval; it is evidence for the handover decision only.

## Compatibility

`streak` is additive. An older daemon returns no `streak` and refuses the new setting.
