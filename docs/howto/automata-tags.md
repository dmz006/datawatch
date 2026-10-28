---
docs:
  index: true
  topics: [autonomous, automata, tags, organization, attention, sessions]
exec_params:
  - name: tag_name
    description: "Name of the tag to create"
    required: true
  - name: prd_id
    description: "Automaton ID to tag"
    required: false
  - name: tag_ids_json
    description: "JSON array of tag IDs to put on the Automaton, e.g. [\"1a2b3c4d\"]"
    required: false
  - name: session_id
    description: "Session ID (full or short) whose own tags to replace"
    required: false
exec_steps:
  - tool: autonomous_tag_create
    description: Create a tag (pass parent_id to nest it under another tag)
    args:
      name: "{{params.tag_name}}"
    read_only: false
  - tool: autonomous_tag_list
    description: List the tag tree with subtree and direct counts for Automata and sessions
    args:
      include_sub: true
      counts: true
    read_only: true
  - tool: autonomous_prd_set_tags
    description: Replace the tags on one Automaton
    args:
      id: "{{params.prd_id}}"
      tag_ids_json: "{{params.tag_ids_json}}"
    read_only: false
  - tool: autonomous_attention_list
    description: List the Automata that need the operator's attention, with the statuses in force
    read_only: true
  - tool: session_set_tags
    description: Replace the tags a session carries itself (inherited tags are not changed)
    args:
      id: "{{params.session_id}}"
      tag_ids_json: "{{params.tag_ids_json}}"
    read_only: false
---
# How-to: Organise Automata and sessions with nested tags

When you run many unrelated efforts in the same working directory, the
Automata list becomes one long flat stream. **Tags** let you group
Automata and sessions the way you think about them — by project, by quarter,
by area — browse them by group, and see at a glance which ones need you.

## What it is

- A **tag** is a named object with an `id` (8 hex characters), a `name`,
  an optional `parent_id`, an optional `color` and `description`.
- Tags form a **tree**: `Work` → `Mobile App` → `iOS`.
- An Automaton carries **several tags** (`tag_ids` on its JSON). One
  tagged both `Mobile App` and `Q4 Security` shows up under both.
- An Automaton may have one **primary tag**. It orders the Automaton inside a
  tag branch and never hides a membership. It defaults to none.
- A **session** carries its own tags. A session started by an Automaton also
  **inherits** that Automaton's tags. Inherited tags are read-only on the session.
- The **attention group** holds the Automata that need you: blocked, needing
  review, paused, or holding a task for review.
- Tags are stored in `tags.jsonl` in the autonomous data directory, so
  they survive a daemon restart. When several daemons share a registry, the
  tags are shared too; see [automata-tag-registry](automata-tag-registry.md).
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

### 3. Browse by tag

The Automata list opens on the **attention view** or the **tag browser**
(see [step 6](#6-the-attention-group-and-the-landing-view)). Switch between
them with the **List** and **Tags** buttons above the list. The tag browser
shows the tag tree, and each tag shows a **subtree count**.

- **Subtree count.** The number of distinct Automata under the tag, including
  every tag below it. An Automaton under two children of `Work` is counted once
  under `Work`.
- **Several tags, one Automaton.** An Automaton with several tags appears under
  each of them, with an *also in:* line naming its other tags. It is counted once
  per tag branch.
- **Untagged.** Automata with no tag at all are listed in an *Untagged* section
  at the end of the tree.
- **Direct count.** Set `autonomous.tags.direct_counts_visible` to show the count
  of Automata tagged with exactly that tag as a tooltip beside the subtree count.

#### Selecting tags and the match mode

Pick one or more tags in the **Tags** filter (on a phone, the **Filters** button
opens a sheet). The default match mode is **match ANY**:

- **match ANY** (default): an Automaton matches when it carries at least one
  selected tag. Selecting a second tag **widens** the list.
- **match ALL**: an Automaton must carry every selected tag. Press the
  match-mode control to switch. Selecting a second tag **narrows** the list.

While a filter is active the browser shows a flat grid of the matches. If none
match, the browser says *No matches. Clear N filters*; press it to clear the
selection.

**Sub-tags are always included in the tag browser.** Choosing `Work` matches
Automata tagged `Work` **or any tag below it**, and the counts roll up. Tagging
an Automaton only at the leaf (`iOS`) is enough. There is no switch to match
exact tags only in the browser yet; the Automata-by-tag route
(`GET /api/autonomous/prds?tag=<id>`) takes `include_sub=false` for that.

The tag selection and the match mode are remembered in the browser between
visits. The match mode is a browser setting, and its starting value comes from
`autonomous.tags.default_match_mode`. REST, MCP, the CLI and the comm channel
do not take a match mode.

On a phone the tree drills down one level at a time with a breadcrumb trail and
a **Back** row; the URL hash follows the drill-down.

### The header Tags dropdown

The page header has an icon button showing a tag icon, with no visible word.
Its accessible name and its tooltip read **Tags**. It opens a dropdown that
holds the session list's filters and the actions for the session you have open.

- **Active-filter count.** A small number on the tag icon button counts the
  filters in force: one for **Watched only**, plus one for each ticked tag.
  It is empty when no filter is on.
- **Watched only.** A checkbox at the top. Tick it to list only the sessions
  you watch.
- **Tags.** One checkbox per tag, in tree order. Tick several to filter the
  session list by them. The match button switches between **match ANY**
  (default) and **match ALL**, and **Clear** appears once a tag is ticked.
  With no tags yet, the list reads *No tags yet*.
- **This session.** Shown only while a session is open. **Watch this session**
  (or **Stop watching this session**) replaces the watch eye the session
  header used to carry, and **Edit tags of this session** opens the session
  tag editor described in [step 7](#7-tag-a-session).

Press Escape, the button again, or click outside the dropdown to close it.

### 4. Primary tag (sorting only)

A primary tag orders an Automaton inside each branch it appears in. It is
optional and defaults to **No primary tag**.

1. Open the Automaton's **Tags** dialog and tick its tags.
2. Pick a **Primary tag** from the choices; the choices are the tags you ticked.
3. Press **Save**.

The primary tag changes only the order. It never hides an Automaton from a
branch, and it is cleared when its tag is removed from the Automaton.

### 5. Bulk tag

Switch the list to select mode, tick several Automata and press **Tag** in
the action bar. Pick the tags and press **Apply**; they are applied to every
selected Automaton.

### 6. The attention group and the landing view

The **attention group** is the set of Automata that need you. It holds an
Automaton when:

- its status is one of the statuses in `autonomous.tags.attention_statuses`.
  The default is **blocked, needs_review and paused**. A paused Automaton is in
  the group so that every paused item is in one place; or
- it has a task held for operator review. Its card reads *N task(s) held for
  review*.

The list shows the group as a pinned **Needs attention** section above the tag
browser. The view switch shows its count, as **Attention (N)**. When nothing
needs attention the section reads *Nothing needs attention right now.*

A pinned **Running** strip sits beside it and shows the Automata that are
running, with *+N more* when there are more than fit.

**The landing view** is set by `autonomous.tags.default_view`:

- `auto` (default): the attention view when at least one Automaton needs
  attention, otherwise the tag browser.
- `attention`: always the attention view.
- `tags`: always the tag browser.

The landing view is decided once per page load. Any viewer can switch views
afterwards; that choice is remembered in the browser for that viewer.

Counts stay current without a reload. A status change, a tag change or a tag
rename sends a live event, and the list updates. If the event stream drops, the
list catches up on its next poll.

### 7. Tag a session

A session carries its own tags, and shows the tags of the Automaton that
started it.

- **On the list.** Each session card, and each node of the session tree,
  shows its own tags as normal chips. Tags inherited from its Automaton are
  drawn as dashed chips with a small *inherited* label.
- **Inherited tags are read-only.** They change when you change the
  Automaton's tags, not on the session. The session editor never sends them.
- **Edit a session's own tags.** Open the session and choose **Tags → This
  session → Edit tags of this session** in the header (see
  [the header Tags dropdown](#the-header-tags-dropdown)). The session detail
  itself shows only the session's tag chips, with inherited ones marked
  read-only; it has no edit control of its own. Tick the tags and press **Save**.
  The editor lists every tag and names the inherited ones below the list. If
  the server refuses a tag, the toast shows its reason.
- **Start a session with tags.** The New Session form has a **Tags
  (optional)** picker; the ticked tags are sent with the start request.
  The picker is hidden when you start from a project profile.
- **Bulk tag.** Press the select button on the sessions list, tick finished
  sessions and press **Tag** in the bar. The picked tags are added to every
  selected session and the tags each already has are kept.
- **Counts.** A session counts under a tag through its own tags **and** through
  the tags it inherits from its Automaton, so the session totals in the tag tree
  include both.

A tag renamed, recolored, deleted or assigned elsewhere updates the chips
on its own; no reload is needed.

### 8. The two refused deletes

Deleting a tag is refused, with nothing changed, when:

1. **It still has child tags.** Move the children to another parent or
   delete them first. The REST response is `409` and the UI shows
   *Tag has child tags — move or delete them first*.
2. **It is still in use.** The refusal carries a `count` of the Automata and
   sessions that carry the tag (the UI shows *Tag is used by N Automata —
   unassign it first*, and the count includes sessions). Remove the tag from
   them, then delete it.

A third refusal protects the tree: moving a tag under itself or under one
of its own sub-tags is rejected as a cycle.

## Settings

Six keys under `autonomous.tags` control the tag browser. Each one is set the
same way from every channel, and an invalid value is refused on every channel
with the same message, with nothing written.

| Setting | Values | Default | PWA field |
|---|---|---|---|
| `autonomous.tags.default_view` | `auto`, `attention`, `tags` | `auto` | Settings → General → Automata → Tag browser: *Default view* |
| `autonomous.tags.attention_statuses` | a comma list of Automaton statuses | `blocked, needs_review, paused` | *Attention statuses* |
| `autonomous.tags.default_match_mode` | `any`, `all` | `any` | *Default tag match mode* |
| `autonomous.tags.direct_counts_visible` | `true`, `false` | `false` | *Show the direct count as a tooltip* |
| `autonomous.tags.registry_keeper` | a federation peer name, or empty | empty (standalone) | *Tag registry keeper* |
| `autonomous.tags.registry_sync_seconds` | whole seconds, minimum 5 | `30` | *Tag registry sync interval* |

The five ways to set any of them at runtime:

| Method | How |
|---|---|
| **REST** | `PUT /api/config` with `{"autonomous.tags.default_view": "attention"}`; read back with `GET /api/config`, under `autonomous.tags` |
| **PWA** | Settings → General → Automata → **Tag browser** group |
| **CLI** | `datawatch config set autonomous.tags.default_view attention`; read with `datawatch config get autonomous.tags.default_view` |
| **MCP** | `config_set` with the key `autonomous.tags.default_view` (the MCP server needs `mcp.allow_self_config=true`) |
| **Comm channel** | `configure autonomous.tags.default_view=attention` |

The YAML file takes the same keys under `autonomous.tags:`; the annotated
template is in [config-reference](../config-reference.yaml). Changes apply
without a restart.

## Same thing from every other surface

| Task | REST | MCP | CLI (`datawatch autonomous …`) | Comm channel (`autonomous …`) |
|---|---|---|---|---|
| List the tag tree | `GET /api/autonomous/tags[?include_sub=true&counts=true]` | `autonomous_tag_list` (`include_sub`, `counts`) | `tag-list [--include-sub] [--counts]` (alias `tags`) | `tags [counts]` |
| Create | `POST /api/autonomous/tags` | `autonomous_tag_create` | `tag-create <name> [--parent <id>] [--color <hex>]` | `tag-create <name> [parent=<id>] [color=<hex>]` |
| Rename | `PATCH /api/autonomous/tags/{id}` | `autonomous_tag_rename` | `tag-rename <id> <new-name>` | `tag-rename <id> <new-name>` |
| Move | `PATCH /api/autonomous/tags/{id}` (`parent_id`) | `autonomous_tag_set_parent` | `tag-move <id> --parent <id\|root>` | `tag-move <id> <parent-id\|root>` |
| Color | `PATCH /api/autonomous/tags/{id}` | `autonomous_tag_set_color` | `tag-color <id> <color>` | `tag-color <id> <color>` |
| Delete | `DELETE /api/autonomous/tags/{id}` | `autonomous_tag_delete` | `tag-delete <id>` | `tag-delete <id>` |
| Set an Automaton's tags | `POST /api/autonomous/prds/{id}/set_tags` | `autonomous_prd_set_tags` | `prd-set-tags <prd-id> --tags <id1,id2>` | `set-tags <prd-id> <id1,id2>` |
| Set an Automaton's primary tag | `POST /api/autonomous/prds/{id}/set_tags` (`primary_tag_id`) | `autonomous_prd_set_tags` (`primary_tag_id`) | `prd-set-tags <prd-id> --primary <tag-id>` | `set-primary <prd-id> <tag-id\|none>` |
| The attention group | `GET /api/autonomous/attention` | `autonomous_attention_list` | `attention` | `attention` |
| A session's own tags | `POST /api/sessions/tags` (`{id, tag_ids}`) | `session_set_tags` | `session tags <session-id> --tags <id1,id2>` | `session-tags <session-id> <id1,id2>` |
| List Automata under a tag | `GET /api/autonomous/prds?tag=<id>[&include_sub=false]` | — | — | — |

An empty tag list clears an Automaton's or a session's own tags.

## Common pitfalls

- **Delete refused with a count.** Unassign the tag from those Automata and
  sessions first (`set_tags`, or `session_set_tags` for a session), then delete.
- **Expecting a tag to hide an Automaton.** A primary tag only sorts. Membership
  comes from the tag set.
- **Selecting a second tag and getting more results.** That is ANY, the default.
  Press the match-mode control for ALL.
- **Inherited tags on a session do not change.** Change the Automaton's tags
  instead.
- **`include_sub` defaults differ.** The Automata-by-tag route includes sub-tags
  by default (`include_sub=false` turns it off). The plain tag list
  (`GET /api/autonomous/tags`, `autonomous_tag_list`) counts only exact tags unless
  you ask for `include_sub`. The tag browser always rolls up sub-tags.
- **Tags across daemons.** Tags are federated only when the daemons share a
  registry (`autonomous.tags.registry_keeper`). Otherwise each daemon has its own
  tags; see [automata-tag-registry](automata-tag-registry.md).

## Next steps

- [automata-tag-registry](automata-tag-registry.md) — one tag registry across
  federated daemons, the keeper, stale copies and merge notes.
- [autonomous-planning](autonomous-planning.md) — create and run Automata.
- [automata-capacity](automata-capacity.md) — how queued Automata wait.
