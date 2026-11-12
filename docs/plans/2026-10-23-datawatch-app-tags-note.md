# datawatch-app — Automata tags — app-side note

**Date:** 2026-10-23
**Repo:** `dmz006/datawatch-app` (Android + iPhone/iOS)
**Daemon counterpart:** this repo — the Automata tags feature (plan: [`historical-plans/2026-10-03-bl393-automata-tags.md`](historical-plans/2026-10-03-bl393-automata-tags.md); operator guide: [`howto/automata-tags.md`](../howto/automata-tags.md))
**Status:** **a GitHub issue on `dmz006/datawatch-app` is needed and has not been filed.** It cannot be filed from the session that wrote this note (no access to that repository), so it must be filed by the supervisor or the operator, using this note as the issue text. This is a filed document only: no mobile code is written here, and the app UI is owned by the app repo.

## Context

Automata can now carry nested tags so an operator running many unrelated efforts can group and browse them. The PWA has a tag registry, a picker, card chips, a browse selector and a bulk action. The apps need to catch up with the viewing, assigning and browsing parts. **Registry management (create, rename, recolor, move, delete) stays web-only for now**, the same as the Automaton type registry.

## New endpoints

| Endpoint | Purpose |
|---|---|
| `GET /api/autonomous/tags[?include_sub=true]` | `{"tags":[…]}`: every tag with its Automata `count`. `include_sub=true` rolls each count up to include descendant tags. |
| `GET /api/autonomous/prds?tag=<id>[&include_sub=false]` | `{"prds":[…]}`: only Automata carrying the tag. `include_sub` defaults to **true** here (the tag and every descendant). 404 for an unknown tag. |
| `POST /api/autonomous/prds/{id}/set_tags` | Body `{"tag_ids":["1a2b3c4d","5e6f7a8b"]}`. Replaces the Automaton's whole tag list; `[]` clears it. 404 if any ID is not a tag. Returns the updated PRD. |
| `POST` / `PATCH` / `DELETE /api/autonomous/tags[/{id}]` | Registry management. Web-only for now; listed so the app can show the error shapes below if it ever calls them. |

## Fields to parse

`tag_ids` on every PRD JSON (list and get) is an array of tag IDs. It is absent or empty on an Automaton with no tags and on an older daemon; never treat a missing array as an error.

```json
{ "id": "5dfb0001", "title": "…", "tag_ids": ["1a2b3c4d", "5e6f7a8b"] }
```

A tag object:

```json
{ "id": "1a2b3c4d", "name": "Mobile App", "parent_id": "9f8e7d6c",
  "color": "#6366f1", "description": "", "created_at": "…", "updated_at": "…", "count": 4 }
```

- `parent_id` is empty for a root tag. The list is flat; build the tree from `parent_id`.
- `color` is a CSS color and may be empty.
- Resolve `tag_ids` to names and colors from the tag list; refetch the list when an ID is unknown (the tag may be new).

Error shapes (JSON `{"error": "…"}`): `409` on a delete or move refusal. A delete refused because Automata still carry the tag also has `"count": N`.

## Acceptance criteria

1. **View tags on an Automaton card.** The card shows a chip per tag in `tag_ids`, using the tag's name and color. An Automaton with no tags shows none.
2. **Assign and unassign.** From the Automaton detail screen the operator opens a picker showing the tag tree (indented by depth, multi-select), and saving calls `set_tags` with the full chosen list. Unticking everything and saving sends `[]`.
3. **Browse by tag.** The Automata list has a tag selector: **All** (the unchanged flat list, default) or one tag. An **include sub-tags** switch, on by default, decides whether Automata tagged only with a descendant tag are included. The list may filter locally from `tag_ids` using the tag tree, or call `GET /api/autonomous/prds?tag=`. Counts shown beside tags use `include_sub` to match the switch. The selection is remembered on the device.
4. **A tag deleted elsewhere.** If a card carries an ID that no longer exists, skip the chip rather than failing.
5. **Out of scope:** creating, renaming, recoloring, moving or deleting tags; bulk tagging; the project-alias suggestion.

## Parity entry

`docs/parity-status.md` carries an "Automata tags" row: PWA done, Android and iOS pending this issue.
