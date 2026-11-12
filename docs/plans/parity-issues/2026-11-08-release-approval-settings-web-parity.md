# [web] Release approval settings: the Settings page sets the three modes, but nothing acts on them yet (Phase 4 of the autonomy handover plan)

**Filed 2026-11-09 as dmz006/datawatch-app#419, one consolidated issue for all the release approval drafts** (shipped in v9.116.0).

**Not filed yet.** — this is the tracked draft; the GitHub issue is opened when
the operator files it. Filing the issue does not change a line of this repo.
This draft is a file write only: it never runs any hosting CLI and creates
nothing on any host.

**Story** (22da0001, "Part of finishing review and merge by the daemon"): the
web Settings page now has the release approval modes as a setting, one select
per release size — `autonomous.merge_pipeline.release_approval.{patch|minor|major}`,
each `auto`, `ask` or `manual` — with the labels, hints and docs links in five
locales (en, es, de, fr, ja). The step the Settings page sets up is a value
the daemon's merge path does not read yet.

## Affected area

The gap is on the operator's surface, across four places this story touches:

- `internal/server/web/app.js` — the release approval rows in the merge
  pipeline card (one select per size, showing the mode in effect), the
  `mergePipelineSetReleaseApproval` handler that checks the size and mode and
  then saves `release_approval.<size>` as the shown project's override, and the
  Settings registry entries that give each of the three keys its label, hint,
  docs link and default (patch `auto`, minor `auto`, major `ask`).
- `internal/server/web/locales/{en,es,de,fr,ja}.json` — the 13
  `release_approval` keys in each file. A grep of all five files finds the same
  13 keys in each, so the locale set is complete for this story.
- `internal/server/web/app-merge-pipeline.test.js` — the tests for the rows, the
  select options, the size and mode checks, and the override save.
- `internal/mcp/merge_pipeline_tools.go`, `internal/router/merge_pipeline_comm.go`,
  `cmd/datawatch/main.go` and `cmd/datawatch/merge_pipeline_test.go` — the same
  key is reachable from MCP (`merge_pipeline_release_approval_{size}` fields)
  and the comm channel, so the web control and the other two surfaces write the
  same dotted key.

The daemon side of the value is the same as the config-layer draft,
[2026-11-08-release-approval-parity.md](2026-11-08-release-approval-parity.md):
`internal/mergepipeline` does not consult `EffectiveReleaseApproval` when it
reviews, merges, tags or deploys. The read-surface side is the view draft,
[2026-11-08-release-approval-view-parity.md](2026-11-08-release-approval-view-parity.md),
which covers the merge pipeline view's per-size fields. This draft covers only
the Settings-page surface and what it promises an operator.

## Current behavior

An operator can open Settings, pick `auto`, `ask` or `manual` for patch, minor
or major releases, and the value is saved as that size's override through
the merge pipeline setting handler (the save path was read in `app.js`, not
exercised live in this draft). The labels and hints say what each mode is meant to do, and the
merge pipeline card shows the mode in effect per size. The 13 keys are present
in all five locales, so no surface falls back to raw key names in those five
languages.

What the saved value does is still nothing new. The merge path ignores it, so
a patch, minor or major release ships exactly as it did before the setting
existed. The hints read as if the mode is enforced — `auto` says the release
ships without waiting, `ask` says the operator is asked first, `manual` says
the release is left for the operator — and at present none of those is true.
A major release set to `auto` on the Settings page will not ship any sooner,
and a major release left at the default `ask` is not asked about by anything.

## Expected behavior

The Settings page should not promise an effect the daemon does not produce.
Once the Phase 4 enforcement step is in, the value the page saves is the value
the review-and-merge path resolves for each size and project, and the hint for
each mode describes what the daemon does at that boundary. Until then, the
operator should be able to see on the page that the setting is recorded but not
yet enforced, so the page and the howto (*Choose the release approval* in
[docs/howto/merge-pipeline.md](../../howto/merge-pipeline.md)) tell the same
story.

The locale keys are the other half of this. Every key the page reads should
exist in all five locales with the same meaning, and the test file should keep
the key sets equal so a new key cannot land in en alone.

## Suggested next step

Two small items, both in the web and locale layers, and neither needs the
daemon to change:

1. Add a visible "recorded, not yet enforced" note to the release approval
   rows, keyed through the five locales, and show it until the enforcement
   story lands. Remove the note in the same change that makes the daemon honour
   the mode, so the page never claims enforcement the daemon lacks.
2. Extend `app-merge-pipeline.test.js` with a check that the 13
   `release_approval` keys match across en, es, de, fr and ja, so a missing
   translation fails a test rather than shipping silently.

Then follow the daemon-side order in the config-layer draft: the shadow step
first, then enforcement, then push-and-tag. Once enforcement is on, drop the
note in the same change.
