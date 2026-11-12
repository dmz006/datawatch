Daemon version: the next minor release (a feature release, not a patch). It adds tag-based browsing and a redesigned tile for Automata, with session tags and a shared tag registry across federated daemons. The PWA is the reference. This issue lists the REST shapes the apps parse and the behaviour they should match, so Android, Wear OS, Android Auto and iOS stay at parity.

## What the PWA now does

**Shared Automata tile.** The Automata list and the Automation list draw each item with the same tile. The tile shows the title (two lines at most), the status chip, the short ID, the backend and model, progress as `done / total` with a bar, up to three tag chips with a `+N` chip for the rest, and a relative last-activity time. Progress comes from the list payload, so the tile makes no request of its own. Below 720 px the tile collapses to two lines (title, status chip and progress; backend, tag count and activity). Below 420 px it becomes one line (title, status dot and progress). Actions move into an overflow menu at the smaller widths. `Open` is always visible.

**Attention group and landing rule.** A pinned area sits above the tag browser.
- *Needs attention* is open. It lists the Automata whose status is in `autonomous.tags.attention_statuses` (default `blocked`, `needs_review`, `paused`) and every Automaton with a task held for operator review. Each row shows the blocked reason where there is one. The header reads `Attention (N)`.
- *Running* is a collapsed strip of up to five Automata with a `+N more` line.
- Empty groups take no height. When both are empty the pinned area is hidden.
- An Automaton in the pinned area also appears in its own tag section. The pinned area is a view, not a move.
- The landing view follows `autonomous.tags.default_view`. `auto` opens attention when N is at least 1, otherwise the tag browser. `attention` and `tags` always open that view. The choice is made once at launch, so the view does not change under the user. A viewer can pin a view as their own default.

**Sections and drill-down.** On web and tablet (720 px and up) the tag tree is shown as collapsible sections, with the children nested inside. On phones, one section is on screen at a time: opening a tag replaces the list with that tag's view, and a back row returns to the parent. A breadcrumb (`Tags › Work › Mobile App`) shows the path, and each crumb is tappable. The tree, the counts and the breadcrumb come from the same data on every layout. Only the presentation differs. An `Untagged` section is always last.

**Counts.** Each tag shows its subtree count: the distinct Automata under it, including its children, with an item under two children counted once. The direct count is a tooltip when `direct_counts_visible` is on. The counts refresh from the `tag_update` event and fall back to the existing poll if the stream drops.

**Multi-tag display.** An item appears under every one of its tags. Its other tags are named on an `also in:` line, and tapping one opens that tag. An optional primary tag per item is used for sorting only. It defaults to none.

**ANY and ALL.** Selecting several tags widens the list under ANY (the default, set by `default_match_mode`). The filter bar says `match ANY`, and a toggle switches to `match ALL`. The toggle has a visible label, not a hidden default. The match mode is view state in the browser; it has no REST parameter.

**Session tags.** A session carries its own tags. A session started by an Automaton also shows that Automaton's tags as `inherited` chips. Inherited tags are read-only on the session and change only with the Automaton. Session tags are edited from the session detail (`Edit tags`), from the New Session picker, and in bulk for several sessions.

**Stale marker and merge notes.** On a peer daemon, the tag browser reads the registry status. When the keeper is unreachable, a banner says the view shows the last copy and a `stale` mark appears on the counts. While the keeper is unreachable, rename, move, recolor, describe and delete are paused. Create still works and the new tag shows `pending sync` until the keeper has it. A merge note appears when two tags created apart were merged, and a removed-on-keeper note appears when the keeper deleted a tag this node still used. Each note has a Dismiss action.

**Settings.** The Settings group `Tag browser` holds the six `autonomous.tags` keys listed below. Changes apply without a restart.

## REST contract the apps parse

Every route below sits behind the existing federation capability guard. Reads need `autonomous:read`, Automaton tag writes need `autonomous:write`, and session tag writes need `sessions:write`. Field names are exact. Omitted optional fields mean empty or none.

**Automata list items** (`GET /api/autonomous/prds`, the existing route)
- `tags`: array of tag IDs on the item (omitted when empty).
- `primary_tag_id`: the optional primary tag ID (omitted when none).
- The three task count fields are unchanged. The tile reads progress from them.

**Set an Automaton's tags** (`POST /api/autonomous/prds/{id}/set_tags`)
- Body: `tag_ids` (array), `primary_tag_id` (string), or both.
- `primary_tag_id` alone changes only the primary tag and keeps the tags. An empty string clears the primary.
- A primary tag must be one of the item's tags.

**Tag tree** (`GET /api/autonomous/tags`)
- Reply: `{tags, registry}`. `include_sub=true` includes the sub-tags in `count`.
- Each tag has `id`, `name`, `parent_id` (empty for a root tag), `color`, `description`, `created_at` and `updated_at`, plus `count`.
- With `counts=true` each tag also has `subtree_count` and `direct_count` (Automata), and `session_subtree_count` and `session_direct_count` (sessions, including inherited tags). The reply then carries `untagged: {count, session_count}`.
- `subtree_count` is the figure the tile shows. Direct counts are for tooltips and for views that want them.

**Attention** (`GET /api/autonomous/attention`)
- Reply: `{statuses, items}`. `statuses` are the attention statuses in force. `items` are Automata summaries (`blocked_reason` included) whose status is one of them, plus every Automaton with a task held for review.

**Tag writes** (`POST /api/autonomous/tags`, `PATCH /api/autonomous/tags/{id}`, `DELETE /api/autonomous/tags/{id}`)
- Create body: `name`, `parent_id`, `color`, `description`. Rename, move, recolor and describe go through `PATCH`. Delete returns 409 when the tag has children or assigned Automata.
- On a peer the reply shows the keeper's value. A create on a peer returns the tag with `pending_sync: true` until the keeper has it.
- When the keeper is unreachable, rename, move, recolor, describe and delete return 503. Create still succeeds.
- A reply with `conflict: true` and `conflict_field` means the keeper's value won for that field. Show the new value and say the keeper decided.

**Registry object** (the `registry` field of `GET /api/autonomous/tags`)
- `role`: `standalone`, `keeper` or `peer`.
- `keeper`: the keeper's daemon name (empty when this daemon is standalone or is the keeper).
- `stale`: true when the keeper is unreachable and this node shows its last copy.
- `last_synced_at`: time of the last successful sync, or null.
- `revision`: the registry revision this node holds.
- `pending_sync_count`: tags created here and not yet on the keeper.
- `notes`: array of `{id, kind, name, parent_path, kept_id, replaced_id, tag_id, nodes, merged_at}`. `kind` is `merged` (two tags created apart were merged; `nodes` names both daemons) or `removed_on_keeper`.
- `last_error`: the last sync error, or empty.

**Dismiss a registry note** (`POST /api/autonomous/tags/registry/notes/{id}/dismiss`)
- Reply: `{dismissed}`. The note is removed from the `notes` list of the registry object.

**Session tags**
- Session list and detail objects carry `tags` (the session's own tag IDs) and `inherited_tags` (the IDs from its Automaton, computed on read, read-only).
- `POST /api/sessions/tags` with `{id, tag_ids}` sets the session's own tags. `id` may be the short or full session ID. It does not touch inherited tags.
- The session start request accepts `tags` (array of tag IDs).

**Live counts** (WebSocket frame `tag_update`)
- Fields: `kind`, `tag_id` (when a tag changed), `prd_id` (the Automaton ID, when an Automaton's tags changed) and `session_id` (when a session's tags changed). The field name `prd_id` is the existing wire name for the Automaton ID.
- Kinds: `created`, `renamed`, `reparented`, `recolored`, `deleted`, `assigned`, `session_assigned`, `registry_synced`.
- The frame carries IDs only. Refetch the tag list, the attention list or the session on receipt. Do not patch counts locally.

**Peer-to-peer routes** (`GET /api/autonomous/tags/registry`, `POST /api/autonomous/tags/registry/write`)
- These are daemon-to-daemon routes. Apps do not call them.

**Settings** (`GET` and `PUT /api/config`, under `autonomous.tags`)
- `default_view`: `auto`, `attention` or `tags`. Default `auto`.
- `attention_statuses`: list of Automaton statuses. Default `blocked`, `needs_review`, `paused`.
- `default_match_mode`: `any` or `all`. Default `any`.
- `direct_counts_visible`: boolean.
- `registry_keeper`: the federation peer name of the keeper. Empty means this daemon keeps the registry and nothing syncs.
- `registry_sync_seconds`: whole number, minimum 5, default 30.

## Android (Compose Multiplatform)

- Tag browser: tag tree with subtree counts, the `also in:` line, the Untagged section and the ANY and ALL toggle.
- Attention group, Running strip, and the landing rule above.
- Automaton tile with row degradation at 720 px and 420 px.
- Automaton tag edits (`set_tags` with `primary_tag_id`) and session tag edits (`sessions/tags`).
- Registry status line, the stale banner, the stale mark, merge and removed-on-keeper notes with Dismiss.
- Handle `tag_update` by refetching. The current app has no handler for it.
- Translation keys are in the companion issue for the i18n pipeline (`feat(i18n): add tag browsing keys for parity`).

## Wear OS and Android Auto

- This change adds no tag browser to Wear OS or Android Auto. No new tag data is needed there.
- A surface that already lists Automata may show the attention count from `GET /api/autonomous/attention` (the length of `items`). This is optional.
- Attention notifications use the existing mobile topic sink. No tag-specific delivery is added.

## Not built in this release

- Saved filter combos (no code and no route exist).
- The `pending sync` badge on the tag list. The registry knows the flag, but the tag list does not carry it yet, so the PWA does not draw the badge.
- An include-sub-tags switch. Sub-tags always roll up into the counts.

## Acceptance for the apps

- A list response with no tags renders the same tile as before, with no tag chips.
- Counts never come from a local sum. They are the server's `subtree_count`.
- A `tag_update` frame causes a refetch, not a local patch.
- A 503 on a tag rename or delete shows the keeper-unreachable message, not a generic error.
- Inherited session tags are shown and cannot be removed on the session.

## Companion issues

- Android and Wear OS sync: this issue (dmz006/datawatch-app#273).
- Translations: dmz006/datawatch-app#274 (feat(i18n): add tag browsing keys for parity).
- iOS: dmz006/datawatch-app#275.
