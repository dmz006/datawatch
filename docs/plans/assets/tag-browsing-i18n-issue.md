Parent release: the next minor release (a feature release). The keys below come with the Automata tag browser, the shared Automata tile, the attention group, session tags and the `Tag browser` settings group.

The PWA loads these keys from `internal/server/web/locales/<lang>.json`, and the Android client is the source for the translations. Every key is already in the DE, ES, FR and JA bundles. Where a bundle still holds the English text, that text is a placeholder. Please add the keys to the Compose Multiplatform strings and send back translations for German (DE), Spanish (ES), French (FR) and Japanese (JA) through the same pipeline.

Keys are snake_case, follow the Android naming convention (`tag_*`, `tag_browse_*`, `tag_registry_*`, `settings_tags_*`, `work_tile_*`), and are stable. Placeholders use `%1$s` and `%1$d` in the order shown.

## Keys and English values

### Shared Automata tile (Automata and Automation lists)

| Key | English value | Screen |
|---|---|---|
| `work_tile_open` | Open | Tile action button |
| `work_tile_more` | More actions | Tile overflow menu |
| `work_tile_tags_more` | +%1$d | Tile tag chip for the tags not shown |
| `work_tile_tags_count` | %1$d tags | Tile line at phone width |
| `work_tile_progress` | %1$d / %2$d | Tile progress count |
| `work_tile_age_now` | just now | Tile last-activity time |
| `work_tile_age_minutes` | %1$dm ago | Tile last-activity time |
| `work_tile_age_hours` | %1$dh ago | Tile last-activity time |
| `work_tile_age_days` | %1$dd ago | Tile last-activity time |

### Tag browser (Automata and Automation lists)

| Key | English value | Screen |
|---|---|---|
| `tag_view_list` | List | View switch: list |
| `tag_view_tags` | Tags | View switch: tag browser |
| `tag_view_attention` | Attention (%1$d) | Attention header, %1$d = count |
| `tag_browse_root` | Tags | Breadcrumb, first crumb |
| `tag_browse_back` | Back | Drill-down back row (phone) |
| `tag_browse_untagged` | Untagged | Last section of the tag tree |
| `tag_browse_items_label` | Items (incl. sub-tags) | Heading above the items under a tag |
| `tag_browse_no_items` | Nothing under this tag yet. | Empty tag view |
| `tag_browse_also_in` | also in: | Line naming an item's other tags |
| `tag_browse_direct_count` | %1$d directly tagged | Tooltip beside a subtree count |
| `tag_browse_crumbs_label` | Tag path | Accessible label for the breadcrumb |
| `tag_browse_filter_tags` | Tags | Filter bar: tags facet |
| `tag_browse_filter_head` | Filters | Filter sheet heading |
| `tag_browse_filters` | Filters (%1$d) | Filter button, %1$d = active filters |
| `tag_browse_match_any` | match ANY | Filter bar: match mode, ANY |
| `tag_browse_match_all` | match ALL | Filter bar: match mode, ALL |
| `tag_browse_mode_title` | Choose whether an item must match any selected tag or all of them | Match-mode toggle, accessible label |
| `tag_browse_done` | Done | Filter sheet close |
| `tag_browse_no_matches` | No matches. Clear %1$d filters | Empty result with active filters |
| `tag_browse_no_matches_plain` | No matches. | Empty result without filters |
| `tag_browse_showing` | Showing %1$d of %2$d | Result count line |

### Attention group and running strip

| Key | English value | Screen |
|---|---|---|
| `tag_attention_title` | Needs attention | Pinned attention group heading |
| `tag_attention_review_held` | %1$d task(s) held for review | Attention row, reason line |
| `tag_attention_none` | Nothing needs attention right now. | Attention group, empty |
| `tag_running_title` | Running | Pinned running strip heading |
| `tag_running_more` | +%1$d more | Running strip, overflow line |

### Primary tag and Automaton tags

| Key | English value | Screen |
|---|---|---|
| `tag_primary_label` | Primary tag | Automaton tag editor: primary tag selector |
| `tag_primary_none` | No primary tag | Automaton tag editor: primary tag, none |

### Session tags

| Key | English value | Screen |
|---|---|---|
| `tag_inherited_label` | inherited | Chip on a tag the session inherits |
| `tag_inherited_tip` | Inherited from the Automaton; change it there | Tooltip on an inherited chip |
| `tag_inherited_note` | Inherited from the Automaton (read-only): | Session detail, inherited tags line |
| `tag_session_none` | No tags | Session detail, no tags |
| `tag_session_edit` | Edit tags | Session detail: edit button |
| `tag_session_edit_title` | Session tags | Session tag editor heading |
| `tag_session_batch_title` | Add tags to %1$d sessions | Bulk tag sheet heading, %1$d = session count |

### Tag registry status (shared registry across federated daemons)

| Key | English value | Screen |
|---|---|---|
| `tag_registry_status` | Shared registry: %1$s · keeper: %2$s | Tag browser, registry status line (%1$s = role, %2$s = keeper name) |
| `tag_registry_role_keeper` | keeper | Role word in the status line |
| `tag_registry_role_peer` | peer | Role word in the status line |
| `tag_registry_stale_banner` | Showing the last copy from %1$s; the keeper is unreachable | Stale banner (%1$s = time) |
| `tag_registry_stale_mark` | stale | Mark on a count shown from the last copy |
| `tag_registry_stale_tip` | Count from the last copy; the keeper is unreachable | Tooltip on the stale mark |
| `tag_registry_edits_paused` | Rename, move, recolor and delete are paused until the keeper is reachable again. Creating a tag still works and shows pending sync until the keeper has it. | Tag editor notice while the keeper is unreachable |
| `tag_registry_pending_sync` | pending sync | Badge on a tag created here and not yet on the keeper |
| `tag_registry_conflict_toast` | The keeper's version won: %1$s is now %2$s | Toast after a conflict (%1$s = field, %2$s = keeper value) |
| `tag_registry_field_description` | Description | Tag editor field label |
| `tag_registry_note_merged` | Tag %1$s was created on two nodes while they were apart and has been merged | Merge note (%1$s = tag name) |
| `tag_registry_note_removed` | Tag %1$s was deleted on the keeper while this node still had Automata under it | Removed-on-keeper note (%1$s = tag name) |
| `tag_registry_note_dismiss` | Dismiss | Action on a registry note |
| `tag_registry_time_unknown` | an unknown time | Stale banner when the sync time is missing |

### Tag browser settings (group `Tag browser`)

| Key | English value | Screen |
|---|---|---|
| `settings_tags_group` | Tag browser | Settings group heading |
| `settings_tags_group_help` | Which view the Automata list opens on, how tag filters combine, and how the shared tag registry is kept in step across federated daemons. | Settings group help text |
| `settings_tags_default_view` | Default view (auto opens attention when an item needs it, otherwise tags; attention or tags always opens that view) | Settings: `autonomous.tags.default_view` |
| `settings_tags_attention_statuses` | Attention statuses (comma-separated Automaton statuses that raise the attention view) | Settings: `autonomous.tags.attention_statuses` |
| `settings_tags_default_match_mode` | Default tag match mode (any widens the list when several tags are selected; all narrows it) | Settings: `autonomous.tags.default_match_mode` |
| `settings_tags_direct_counts_visible` | Show the direct count as a tooltip beside the subtree count | Settings: `autonomous.tags.direct_counts_visible` |
| `settings_tags_registry_keeper` | Tag registry keeper (federation peer name; empty = this daemon keeps it and nothing syncs) | Settings: `autonomous.tags.registry_keeper` |
| `settings_tags_registry_sync_seconds` | Tag registry sync interval (seconds, minimum 5, default 30) | Settings: `autonomous.tags.registry_sync_seconds` |

## What to send back

- A translated value for each key above, in DE, ES, FR and JA, in the same order as the table.
- Keep the placeholders (`%1$s`, `%1$d`, `%2$s`) and their order.
- Use the existing translations for the product words Automata and Automaton, and keep ANY and ALL as written.
- Once the translations are in, they are copied into the PWA locale bundles on the daemon side. No PWA code change is needed.

## Companion issues

- Sync: dmz006/datawatch-app#273.
- iOS: dmz006/datawatch-app#275.
