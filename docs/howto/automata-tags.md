---
docs:
  index: true
  topics: [prd, autonomous, automata, tags, organization]
exec_params:
  - name: tag_name
    description: "Name of the tag to create"
    required: true
  - name: prd_id
    description: "Automaton (PRD) ID to tag"
    required: false
  - name: tag_ids_json
    description: "JSON array of tag IDs to put on the Automaton, e.g. [\"1a2b3c4d\"]"
    required: false
exec_steps:
  - tool: autonomous_tag_create
    description: Create a tag (pass parent_id to nest it under another tag)
    args:
      name: "{{params.tag_name}}"
    read_only: false
  - tool: autonomous_tag_list
    description: List the tag tree with the number of Automata carrying each tag
    args:
      include_sub: true
    read_only: true
  - tool: autonomous_prd_set_tags
    description: Replace the tags on one Automaton
    args:
      id: "{{params.prd_id}}"
      tag_ids_json: "{{params.tag_ids_json}}"
    read_only: false
---
# How-to: Organise Automata with nested tags

When you run many unrelated efforts in the same working directory, the
Automata list becomes one long flat stream. **Tags** let you group
Automata the way you think about them — by project, by quarter, by area
— and browse them by group.

## What it is

- A **tag** is a named object with an `id` (8 hex characters), a `name`,
  an optional `parent_id`, an optional `color` and `description`.
- Tags form a **tree**: `Work` → `Mobile App` → `iOS`.
- An Automaton carries **several tags** (`tag_ids` on its JSON). One
  tagged both `Mobile App` and `Q4 Security` shows up under both.
- Tags are stored in `tags.jsonl` in the autonomous data directory, so
  they survive a daemon restart.
- Tags only organise. They never start, stop or schedule anything.

## Base requirements

- A running daemon with the autonomous feature enabled.
- At least one Automaton to tag (see [autonomous-planning](autonomous-planning.md)).

## Walk-through (web UI)

### 1. Create a tag tree

1. Open **Settings → Automata → Tag Registry**.
2. Type a name (`Work`), leave **Parent** at *— top level —*, pick a
   color if you like, and press **Create**.
3. Create `Mobile App` with **Parent** = `Work`, then `iOS` with
   **Parent** = `Mobile App`.

Use **Rename**, the **Parent** dropdown and the color swatch on a row to
edit a tag later. Renaming or moving a tag updates it everywhere it is
used, because Automata store the tag's ID, not its name.

### 2. Assign tags to an Automaton

- On an Automaton's detail view press **Tags**, tick the tags in the
  tree and press **Save**.
- In the create-Automaton dialog the **Tags (optional)** picker does the
  same. If the project directory matches a registered project alias and a
  tag with that name already exists, it is pre-ticked with a hint
  (*Suggested from the project alias*). A suggestion never creates a tag
  and nothing is applied until you save.

Chips for the assigned tags appear on the Automaton's card.

### 3. Browse by tag, with the include sub-tags switch

On the Automata list, the **Browse by tag** selector shows **All** (the
unchanged flat list) or one tag.

- With **include sub-tags** on (the default), choosing `Work` lists
  Automata tagged `Work` **or any tag below it**, and the counts roll up.
  Tagging an Automaton only at the leaf (`iOS`) is enough.
- With the switch off you see only Automata carrying exactly the chosen
  tag.

The selection is remembered in the browser between visits.

### 4. Bulk tag

Switch the list to select mode, tick several Automata and press **Tag** in
the action bar. Pick the tags and press **Apply**; they are applied to every
selected Automaton.

### 5. Tag a session

A session carries its own tags, and shows the tags of the Automaton that
started it.

- **On the list.** Each session card, and each node of the session tree,
  shows its own tags as normal chips. Tags inherited from its Automaton are
  drawn as dashed chips with a small *inherited* label.
- **Inherited tags are read-only.** They change when you change the
  Automaton's tags, not on the session. The session editor never sends them.
- **Edit a session's own tags.** Open the session; the **Tags** row under
  the header has an **Edit tags** button. Tick the tags and press **Save**.
  The editor lists every tag and names the inherited ones below the list. If
  the server refuses a tag, the toast shows its reason.
- **Start a session with tags.** The New Session form has a **Tags
  (optional)** picker; the ticked tags are sent with the start request.
  The picker is hidden when you start from a project profile.
- **Bulk tag.** Press the select button on the sessions list, tick finished
  sessions and press **Tag** in the bar. The picked tags are added to every
  selected session and the tags each already has are kept.

A tag renamed, recolored, deleted or assigned elsewhere updates the chips
on its own; no reload is needed.

### 6. The two refused deletes

Deleting a tag is refused, with nothing changed, when:

1. **It still has child tags.** Move the children to another parent or
   delete them first. The REST response is `409` and the UI shows
   *Tag has child tags — move or delete them first*.
2. **It is still assigned to Automata.** The refusal carries the number of
   Automata using it (`count` in the REST body; the UI shows *Tag is used
   by N Automata — unassign it first*). Remove the tag from those
   Automata, then delete it.

A third refusal protects the tree: moving a tag under itself or under one
of its own sub-tags is rejected as a cycle.

### 7. A shared tag registry across nodes

When several daemons share one tag registry, the Automata tag browser shows a
status line with the node's role (keeper or peer) and the keeper it follows.
A standalone daemon shows no status line.

- **Stale copy.** When a peer cannot reach its keeper it keeps showing its last
  copy. A banner reads *Showing the last copy from <time>; the keeper is
  unreachable*, every tag count carries a *stale* mark, and rename, move,
  recolor and delete are disabled with the reason as their tooltip. Creating a
  tag still works.
- **Pending sync.** A tag created while the keeper is unreachable shows a
  *pending sync* badge until the keeper takes it.
- **Conflicts.** When the keeper keeps its own value for a field you edited, a
  short notice says the keeper's version won, and the registry panel shows the
  keeper's value.
- **Merge notes.** A tag created on two nodes while they were apart is merged
  and listed as *Tag <name> was created on two nodes while they were apart and
  has been merged*. Click *Dismiss* to remove the note on this node.

## Same thing from every other surface

| Task | REST | MCP | CLI (`datawatch autonomous …`) | Comm channel (`autonomous …`) |
|---|---|---|---|---|
| List tags | `GET /api/autonomous/tags[?include_sub=true]` | `autonomous_tag_list` | `tag-list [--include-sub]` | `tags` |
| Create | `POST /api/autonomous/tags` | `autonomous_tag_create` | `tag-create <name> [--parent <id>] [--color <hex>]` | `tag-create <name> [parent=<id>] [color=<hex>]` |
| Rename | `PATCH /api/autonomous/tags/{id}` | `autonomous_tag_rename` | `tag-rename <id> <new-name>` | `tag-rename <id> <new-name>` |
| Move | `PATCH /api/autonomous/tags/{id}` (`parent_id`) | `autonomous_tag_set_parent` | `tag-move <id> --parent <id\|root>` | `tag-move <id> <parent-id\|root>` |
| Color | `PATCH /api/autonomous/tags/{id}` | `autonomous_tag_set_color` | `tag-color <id> <color>` | `tag-color <id> <color>` |
| Delete | `DELETE /api/autonomous/tags/{id}` | `autonomous_tag_delete` | `tag-delete <id>` | `tag-delete <id>` |
| Set an Automaton's tags | `POST /api/autonomous/prds/{id}/set_tags` | `autonomous_prd_set_tags` | `prd-set-tags <prd-id> --tags <id1,id2>` | `set-tags <prd-id> <id1,id2>` |
| List Automata under a tag | `GET /api/autonomous/prds?tag=<id>[&include_sub=false]` | — | — | — |

An empty tag list clears an Automaton's tags.

## Common pitfalls

- Delete refused with a count: unassign the tag from those Automata first
  (`set_tags` with the list minus that tag).
- `include_sub` defaults to **on** when listing Automata by tag, and to
  **off** for tag counts unless you ask for it.
- Tags are local to one daemon; a peer in a federation has its own registry.

## Next steps

- [autonomous-planning](autonomous-planning.md) — create and run Automata.
- [automata-capacity](automata-capacity.md) — how queued Automata wait.
