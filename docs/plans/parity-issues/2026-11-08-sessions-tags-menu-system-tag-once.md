# [sync] Sessions Tags menu: each system tag listed once, placement tags under their axis — layout + locale parity (datawatch app)

**Filed 2026-11-08 as https://github.com/dmz006/datawatch-app/issues/400 (release v9.112.6).** Original note: — this is the tracked draft; the GitHub issue is opened when
the operator files it. Filing the issue does not change a line of this repo.
This draft is a file write only: it never runs any hosting CLI and creates
nothing on any host.

**Operator report this work addresses** (found 2026-11-08, from a phone
screenshot of the Sessions tab): the Tags menu listed every placement system
tag twice — once under its axis heading in the "Placement tags (system)"
section and once more in the general Tags section below it.

## Affected area

The header Tags menu in the PWA's Sessions tab — the panel opened from the
Tags button (`headerTagsBtn`) that carries the watched-only switch, the
session tag filter, and the per-session watch/edit actions. The change is a
pure PWA render change in `_tagsMenuHTML()`: the Tags section's rows
(`_tagFilterRowsHTML('session', …, { hidePlacementTags: true })`) now skip
every system tag whose name is a placement `axis:value` string, so the
menu's placement section — the one grouped under the axis headings
(`compute`, `peer`, `backend`, `lane`, `role`, `project`, then any custom
axis) — is the only place a placement tag is listed. The menu's section
heading ("Placement tags (system)") is a new locale key,
`tags_menu_placement`, which ships with the English text as a placeholder in
`de`, `es`, `fr` and `ja`. Nothing else on the wire or in the registry
changed.

## Current behavior (what this work delivers on the server and PWA)

Each tag name in the Sessions Tags menu now has exactly one checkbox row:

- **Placement tags** (system tags whose name is an `axis:value` string, such
  as `compute:<node>` or a custom axis's value) appear only in the
  "Placement tags (system)" section, under the heading of their own axis,
  axes in canonical order and values sorted within an axis. They carry the
  gear prefix and tick/untick through the same `tagBrowseToggleTag`
  handler as every other row.
- **System tags without an axis** appear only in the Tags section below the
  placement section, with their padlock mark, exactly as before.
- **Operator tags** appear only in the Tags section, exactly as before.
- Selection state is shared across both sections: selecting a placement tag
  ticks its single row, the header count chip counts it once, and the
  match-mode (any/all) toggle and the Clear button act as before even with a
  placement tag selected.
- The other tag surfaces are untouched: the tag filter rows elsewhere
  (Automata filter control, task facets) still list system tags — including
  placement tags — once each with the padlock, since they were never
  duplicated.

The layout is pinned by
`internal/server/web/app-tags-menu-system-tag-once.test.js` (five tests,
each tag once in the whole menu, the right section per tag kind, axis
headings, tick/match-mode/Clear behavior, and the unchanged filter rows),
the behavior change and the new locale key are recorded in `CHANGELOG.md`,
and the key exists in all five locale bundles. Served from the embedded PWA,
so the new render takes a daemon rebuild + restart and a hard refresh of the
PWA tab.

The gap this leaves is on the operator-facing mobile clients, on two counts.
First, layout: a native client whose tag menu mirrors the old PWA shape now
diverges — it either still lists placement system tags in both the grouped
section and the general list (the exact duplication the operator flagged),
or lacks the axis-grouped placement section its PWA counterpart grew, and its
selection count for a placement tag may tick in two places. Second,
localization: the new section heading key `tags_menu_placement` carries the
English text "Placement tags (system)" as a placeholder in de/es/fr/ja in
this repo's bundles, so a client that mirrors these bundles for the key ships
an untranslated heading until the translations are produced through
datawatch-app's own Compose Multiplatform locale pipeline.

## Expected behavior

Once parity is met, a native tag menu (Android and iPhone/iOS) shows each
tag once — placement (system, `axis:value`) tags grouped under their axis
headings in a clearly marked system/placement section, axis-less system tags
and operator tags in the general list, with the platform-idiomatic mark for
system tags instead of the PWA's gear/padlock glyphs. Selecting any tag —
placement or operator — produces exactly one checked row, counts toward the
menu's active count once, and the match-mode and Clear affordances behave
identically with a placement tag selected, so the operator's phone and the
PWA agree on what is filtered. The placement section's heading is translated
in every locale the app ships (de, es, fr, ja), no longer an English
placeholder, and the translation lands in this repo's locale bundles by
mirroring it back from the app once produced. In the absence of that, a
client must at minimum render each tag exactly once rather than duplicated —
the duplication this work closed.

## Suggested next step

File one `feat(tags):`-prefixed issue in datawatch-app covering both counts —
the tag-menu layout (each system tag listed once; placement tags grouped by
axis in a system/placement section; shared selection, match mode and Clear
semantics) and the `tags_menu_placement` locale key (key, English value,
context: the header Tags menu's placement section heading, parent: this
datawatch patch release — requesting DE/ES/FR/JA translations through the
Compose Multiplatform pipeline, with the values mirrored back into
`internal/server/web/locales/*.json`). Since no REST, MCP, CLI, comm-channel
or config surface changed, the acceptance criteria are a pure pass-through:
opening the tag menu with a mix of placement, axis-less system and operator
tags shows each name exactly once with correct grouping and marks, and the
section heading reads in the user's language on both Android and iPhone.

## Parity surface

| Surface | Decision | Why |
|---------|----------|-----|
| REST | included | No change. The menu renders from the tag registry endpoints the client already calls; no field added, renamed or removed. |
| MCP | included | No change — tag registry/tag-browse is unchanged for the MCP surface. |
| CLI | included | No change — sessions are still filterable by tag via the existing CLI paths; only the PWA's drawing of them moved. |
| comm channel | included | No change — no new tag-related command surface. |
| YAML/config | included | No change — placement tag axes and their enablement (`autonomous.tags.placement`) already round-trip; this work only de-duplicates the PWA menu. |
| PWA | included | The Sessions Tags menu render changed (placement rows drawn once, under their axis) and one new locale key ships in all five bundles with DE/ES/FR/JA placeholders. |
| Android | excluded | No app code lives in this repo. Capability parity is required and is tracked by this draft: the once-per-tag menu layout and the translated `tags_menu_placement` heading — implementation parity is not. |
| iPhone/iOS | excluded | Same as Android: no code in this repo, and the same two items are tracked here for the native client. |

## Filing record

- Filed: — (pending; issue number / URL once opened)
