<!-- Filed 2026-11-09 as https://github.com/dmz006/datawatch-app/issues/407 (release v9.113.0). -->
# Release size and milestone views in the web UI (Part of finishing review and merge by the daemon, Phase 4 of the autonomy handover plan)

## Affected area

The web UI surfaces for the release size of an Automaton and the milestone mark of an Automation, in
`internal/server/web/app.js`: the release-size helpers around the Automaton list and detail (the size
normalization, the source label map and the badge that shows the size and where it came from), and the
milestone helpers for the Automation list badge, the detail-header toggle and the update body that sends
`milestone` with the rest of the Automation. The tests are `app-automaton-release-size.test.js` and
`app-automation-milestone.test.js`. The strings live in the five web locale bundles
(`internal/server/web/locales/en.json`, `de.json`, `es.json`, `fr.json`, `ja.json`), each carrying the same
19 release and merge-pipeline keys. The operator documentation is `docs/howto/release-size-and-milestones.md`,
`docs/howto/merge-pipeline.md` and the Release size entry of `docs/datawatch-definitions.md`. This work sits
on top of the release-size resolution and settings shipped earlier in the same arc (see
`2026-11-08-release-size-rules-parity.md`) and is the web half of the operator surface that
`2026-11-08-release-settings-app-parity.md` describes for the app.

## Current behavior

The web UI now shows what the daemon already resolves. An Automaton row or header shows its release size
as one of patch, minor, major or none, and the badge names the layer that decided it (an explicit size, the
milestone layer, the project kind map or the built-in default), using the daemon's `release_size_resolved`
and `release_size_source` fields when no explicit size is set. An Automation shows a milestone badge on its
list row, and its detail header has a toggle that marks or unmarks the milestone through the Automation
update call, sending the full update body so the members, decision assistant and auto tags are not lost.
The Merge pipeline settings card carries the four release controls (release size by kind, major trigger,
release cadence and version scheme) with help text, and the reserved cadences and schemes are refused with
the daemon's reason.

Three gaps remain on this surface. First, the setting controls for an Automaton's explicit `release_size` and
`breaking_change` live only inside the PRD settings dialog (a size select and a breaking-change checkbox that
call the set_release_size and set_breaking_change endpoints). The release-size badge on the Automaton list and
detail does not open that dialog or link to it, so an operator who reads a resolved size there has to know to
look in a separate settings view to change it. Second, the release-size and milestone views say nothing about what the merge will actually do: the
merge runner's version step does not read the resolved size yet (the howto states this plainly), so a size
shown as minor on a row is a report, not a promise, and nothing on the page says so next to the badge.
Third, the milestone toggle is a plain mark with no check of whether the Automation is complete; the badge and
toggle do not show how many members remain, so an operator cannot tell from the page when a milestone would
raise its release step to major.

## Expected behavior

An operator on the web UI can see, for each Automaton, its release size and the layer that decided it, and
can open the controls for an explicit size and the breaking-change flag from that same badge, with the same
reserved and invalid values refused by the daemon and the refusal reason shown on the page. The release size badge
carries a short line stating that the size is recorded and the merge's version step follows it once that step
is wired, so the page never implies a bump that has not happened. The milestone toggle shows the Automation's
completed and remaining member counts, so the operator sees what marking the milestone will mean under the
`milestone` trigger. Every new label and help line exists in all five locales with the same key set, and each
control links to the Release size entry of `docs/datawatch-definitions.md`.

## Suggested next step

Make the release-size badge on the Automaton list and detail open the existing PRD settings controls for
`release_size` and `breaking_change` (the endpoints `POST /api/autonomous/prds/{id}/set_release_size` and
`.../set_breaking_change` are already called from there), and add a test in `app-automaton-release-size.test.js`
that covers the refusal path shown on the page. Add the one-line "recorded, not yet applied to the version step" note to the badge and its
locale keys in all five bundles, and remove that note in the same merge that wires the merge runner's version
step to the resolved size. Show the completed and remaining member counts beside the milestone toggle, reading
them from the Automation the page already loads, and extend `app-automation-milestone.test.js` to cover them.
Keep the locale files in step: each new key goes into en, de, es, fr and ja together, and the existing 19-key
set is the baseline the parity check compares against. File the matching datawatch-app parity issue when this
merge lands, as the app issue `2026-11-08-release-settings-app-parity.md` already requires, and record its URL
in this draft.

## Filing record

Not yet filed. This draft was written as a file only: no `gh` invocation, no issue created on any tracker and
nothing created on any host. File it as a datawatch daemon follow-up (the web UI in `internal/server/web`),
cross-referencing `2026-11-08-release-size-rules-parity.md` and `2026-11-08-release-settings-app-parity.md`.
Drafted 2026-11-08. The code-level claims above (the badge source map, the milestone toggle and update body,
the two test files, the 19-key locale set and the PRD settings controls that call the two set endpoints, which
sit apart from the badge) were checked against the working tree at drafting time.
