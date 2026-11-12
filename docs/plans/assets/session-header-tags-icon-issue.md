[sync] session header Tags button is an icon; session tag row only shows chips (datawatch next release)

Daemon version: the next patch release above main. This is a layout change in the PWA only. No REST, MCP or config shape changes. The PWA is the reference; please follow on Android and iOS so the apps stay at parity.

## What the PWA now does

**Header Tags dropdown button.** The button in the session header shows a tag icon with no visible word. Its `title` and `aria-label` both read `Tags`. The dropdown contents are unchanged (Watched only, tag filter, Watch this session, Edit tags).

**Session detail tag row.** The row under the session header shows only the tag chips: the session's own tags and the tags inherited from the Automaton that started it. The chips are read-only. The row is hidden when the session has no tags at all, so no empty placeholder is drawn. There is no inline `Edit tags` link or `No tags` text any more.

**Edit tags.** The `Edit tags` entry stays in the header Tags dropdown. It is now the only place to edit a session's tags from the session detail.

## Locale keys removed

These two keys are no longer used by the PWA and can be dropped from the app string tables:

- `tag_session_none` = `No tags`
- `tag_session_edit` = `Edit tags`

The dropdown entry keeps using its existing `Edit tags` label key.

## Ask for the apps

- Android: show a tag icon with the content description `Tags` on the session header Tags menu button, and draw the tag row as read-only chips only, hidden when empty. Keep `Edit tags` in the menu.
- iOS: same layout, with the accessibility label `Tags`.
- Remove the two locale keys above from the Compose-Multiplatform string pipeline (DE, ES, FR, JA included).

This builds on the Tags dropdown parity issue `dmz006/datawatch-app#285`.
