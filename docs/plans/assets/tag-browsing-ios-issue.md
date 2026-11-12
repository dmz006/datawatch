Daemon version: the next minor release (a feature release). This issue is the iOS (iPhone and iPad) companion to the sync issue for tag-based browsing and the tile redesign for Automata. The REST shapes, the PWA behaviour, the attention rule, the ANY and ALL toggle, the session tag rules and the registry object are all described in the sync issue. Read that first. This issue lists what is specific to iOS.

## SwiftUI navigation for the drill-down

- **Phone (compact width).** Use a `NavigationStack`. Opening a tag pushes a view for that tag, with its child tags and its items. The back action pops to the parent tag. Only one section is on screen at a time, as in the PWA phone layout.
- **Breadcrumb.** The breadcrumb (`Tags › Work › Mobile App › iOS`) mirrors the navigation path. Each crumb pops the stack back to that tag, and the first crumb returns to the top level.
- **Tablet (regular width, iPad).** Use the tablet layout described in the sync issue: collapsible inline sections with nested sub-sections, in a two-column tile grid. When a filter is active, the sections give way to one flat grid. Expansion state is kept per user.
- The tree, the counts and the breadcrumb come from the same data on iPhone and iPad. Only the layout differs.
- Keep the tile's row degradation at 720 px and 420 px, as the sync issue describes. Use the same tile on both the Automata list and the Automation list.

## Attention and push

- The attention group and the landing rule are the same as in the sync issue.
- Attention pushes use the existing mobile topic sink. No APNs topic, payload or Keychain change is needed for this feature. No tag-specific delivery is added.

## Live counts

- Handle the `tag_update` WebSocket frame by refetching the tag list, the attention list or the session. The frame carries IDs only. Do not patch counts locally.
- The frame has no handler on iOS yet. Add one with the poll as the fallback, as the PWA does.

## Parity checklist for iOS

- Tag tree with subtree counts, the `also in:` line, the Untagged section, and the ANY and ALL toggle.
- Attention group, Running strip, and the landing rule.
- Automaton tile with row degradation, and Automaton tag edits (`set_tags` with `primary_tag_id`).
- Session tag edits (`POST /api/sessions/tags`), with inherited tags shown and read-only on the session.
- Registry status line, the stale banner and the stale mark, merge and removed-on-keeper notes with Dismiss, and the 503 message for keeper-unreachable edits.
- Translation keys are in the companion i18n issue (`feat(i18n): add tag browsing keys for parity`).

## Not built in this release

- Saved filter combos (no code and no route exist).
- The `pending sync` badge on the tag list (the tag list does not yet carry the per-tag flag).
- An include-sub-tags switch. Sub-tags always roll up into the counts.

## Companion issues

- Sync (Android, Wear OS, Auto): dmz006/datawatch-app#273.
- Translations: dmz006/datawatch-app#274.
