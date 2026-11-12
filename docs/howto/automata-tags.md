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
- A second kind of tag is written by the **machine**, not by you: **placement
  tags** (system tags) record where a piece of work ran — which node, which
  federated peer, which backend, lane and role, and which project — and which
  Automaton and Automation the work belongs to, in which stage and of what
  kind. They are stamped on every session and also carried by the objects the
  Automata own (Automata, tasks, Automations, decision records, held
  questions, templates). They are read-only everywhere: you filter by them
  and group by them, but you do not rename, move or delete them. See
  [Placement tags](#placement-tags-the-system-tags).
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

How a selection is checked: an object's derived system tags (such as
`project:datawatch`) and its operator-assigned tags are pooled, then compared
with the selected tags. Match ANY needs one selected tag in the pool; match ALL
needs every selected tag in the pool. A selection with no tags matches every
object. The check itself did not change in the code-scanning fix (it only
dropped a computed size hint on the internal lookup set), so results are the
same as before.

On a phone the tree drills down one level at a time with a breadcrumb trail and
a **Back** row; the URL hash follows the drill-down.

### The header Tags dropdown

The page header has an icon button showing a tag icon, with no visible word.
The tag is a solid tag filled in the theme accent color, with a lighter hole and a
subtle darker edge, so it reads clearly on both the dark and the light theme.
It looks brighter when the dropdown is open or a tag filter is on, it carries a
count badge while filters are active, and it shows a visible focus ring for
keyboard users. Its accessible name and its tooltip read **Tags**. It opens a dropdown that
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

## Placement tags (the system tags)

Everything above is about tags **you** name, assign and edit. Placement tags
are the other kind: the machine derives them from **where a piece of work
ran** and stamps them on every session it starts, and records them as the
**system tags** of the objects the Automata own — Automata, their tasks,
Automations, decision records, held questions and templates. You never assign
them yourself and you can't rename, move or delete them; you use them to see
at a glance which node ran what, on which peer, under which backend or lane,
and which Automaton and stage the work belongs to — and to filter and group
the session list, the object lists, the dashboard and the Gantt by exactly
that.

### The ten axes

| Axis | Default tag shape | Records |
|---|---|---|
| `compute` | `compute:<node>` | the compute node that ran the session |
| `peer` | `peer:<name>` | the federation peer that ran the session (federated work only) |
| `backend` | `backend:<name>` | the backend the session runs on (e.g. `claude`, `pi`) |
| `lane` | `lane:<name>` | the lane that ran the session |
| `role` | `role:<name>` | the capacity role (e.g. `worker`) |
| `project` | `project:<name>` | the project the work belongs to |
| `automaton` | `automaton:<name>` | the Automaton the work belongs to |
| `automation` | `automation:<name>` | the Automation the work runs under |
| `stage` | `stage:<name>` | the stage the work runs in |
| `kind` | `kind:<name>` | the kind of work (e.g. `task`) |

**Defaults:** every axis is **on** — `compute`, `peer`, `backend`, `lane`,
`role`, `project`, `automaton`, `automation`, `stage` and `kind` — so a task
that sets all its facts is tagged along every axis by default; turn an axis
off with `enabled: false`. A master switch covers all of them.

The two axes with a fixed vocabulary: `role` takes one of `worker`,
`verifier`, `planner`, `judge`, `supervisor`, and `kind` one of `task`,
`decompose`, `review`, `rejudge`, `auto-fix`. The rest take the name of
whatever the engine records for them.

To start the operator's supervisor session with the role, pass `launch_role: supervisor` to the session start (REST `POST /api/sessions/start`, MCP `start_session`, or `datawatch session new --launch-role supervisor`); it then carries `role:supervisor` plus the compute, backend, lane and project tags and no automaton tag.

A tag is written **per enabled axis with a value**. A missing value is not an
error and produces no tag: local work writes **no `peer:` tag at all**, and
`peer:<name>` appears when the work ran on another daemon.

### The peer axis (federated work)

The `peer:<name>` tag is how federated work shows up in the tag list. The
name comes only from the **peer name in the explicit placement record** the
daemon wrote for that session — never inferred from a DNS name, an IP
address or a hostname. So the tag always says the peer's name as the daemon
knows it, and a value no operator typed can never leak into a tag.

### Which objects carry which axes

Five of the axes — `project`, `automation`, `automaton`, `stage` and
`kind` — are recorded on the objects the Automata own, not only on sessions.
The other five — `compute`, `peer`, `backend`, `lane` and `role` — describe
a session run; an object never runs, so those axes never put a tag on an
object, no matter how the block is configured.

| Object | Carries |
|---|---|
| **Automaton** | its project, its Automation (none when it stands alone) and itself. No stage and no kind: the stage belongs to a member slot, the kind to a run. |
| **Task** | the fact of its Automaton (project, Automation, Automaton), plus the stage its member slot carries (no stage when the Automaton is a standalone or has no typed slot). |
| **Automation** | itself, and the project of its first resolvable member (none when it can't name one). It has no Automaton or stage axis of its own. |
| **Decision record** and **held question** | the fact of the Automaton they name (project, Automation, Automaton). One that names no Automaton — or whose Automaton is gone — carries no axis, and that is not an error. |
| **Template** | the fact of the Automaton it was cloned from, when one is recorded; a hand-written template carries no axis. |

An object's system tags are re-derived from its stored facts under the
in-force block whenever the object is saved, so they stay in step with the
settings. The tags **you** assign to an object and the tags its sessions
inherit are separate fields that none of this reads or rewrites.

### Which sessions carry which axes

Every kind of session the daemon starts carries the run axes its record
supports. In the table, **✓** means the session carries the axis, **—** means
it never does (it records no such fact), and **if set** means it carries the
axis when its start named one (a launch role, kind, stage or Automaton). The
`peer` axis is written only when the work ran on another daemon, so it is
listed as **federated** for every kind that can be sent to a peer.

| Session kind | compute | peer | backend | lane | role | project | automaton | automation | stage | kind |
|---|---|---|---|---|---|---|---|---|---|---|
| Task worker | ✓ | federated | ✓ | ✓ | ✓ | ✓ | ✓ | if set | if set | ✓ |
| Planner | ✓ | federated | ✓ | ✓ | ✓ | ✓ | ✓ | if set | if set | ✓ |
| Verifier | ✓ | federated | ✓ | ✓ | ✓ | ✓ | ✓ | if set | if set | ✓ |
| Judge | ✓ | federated | ✓ | ✓ | ✓ | ✓ | ✓ | if set | if set | ✓ |
| One-shot | ✓ | federated | ✓ | ✓ | if set | ✓ | if set | if set | if set | if set |
| Scheduled | ✓ | federated | ✓ | ✓ | if set | ✓ | if set | if set | if set | if set |
| Supervisor | ✓ | federated | ✓ | ✓ | ✓ | ✓ | — | — | — | if set |
| Other operator-started | ✓ | federated | ✓ | ✓ | if set | ✓ | if set | if set | if set | if set |
| Restarted | keeps the full set its record holds | | | | | | | | | |
| Council | ✓ | federated | ✓ | ✓ | if set | ✓ | — | — | — | if set |
| Eval | ✓ | federated | ✓ | ✓ | if set | ✓ | — | — | — | if set |

- **Local sessions carry the local node name.** A session on the local host
  carries the local node name in its `compute:` tag (`compute:local` when
  the record stored no node), so no session is left without a compute tag.
- **No Automaton, no `automaton:` tag.** A session with no Automaton carries
  no `automaton:` tag (and no `automation:` tag), whatever role it runs as.
  That is not an error.
- **Restarted sessions keep the full set.** A restart re-runs the placement
  stamp from the session's record, so a restarted session carries the same
  axes it carried before, including role, kind, stage and Automaton.
- An axis you have turned off (see [The ten axes](#the-ten-axes)) is left out
  of every row.
- **Sessions started before this change** keep the tags they had. To bring
  them up to the full set, preview with `datawatch autonomous tags sweep
  --dry-run`, then run `datawatch autonomous tags sweep`.

**For the operator to review:** the table reflects the start paths in the
current code. Confirm that the Council and Eval rows (no Automaton axes) and
the Supervisor row (no `automaton`, `automation` or `stage`) match what you
expect before the next release.

### Filtering by a tag or an axis

Every axis can be used as a filter on every surface:

- **The session list.** `GET /api/sessions` takes `?tag=` (repeatable) with
  `?tag_match=any` (default) or `all`; a session's own, system and inherited
  tags all count. The CLI adds the axis shortcuts `--project`, `--automaton`,
  `--automation`, `--stage`, `--kind`, `--role`; the MCP `list_sessions`
  tool takes the same six plus `lane` and `compute`. Each shortcut expands
  to an `<axis>:<value>` tag under the same `tag_match`, and they combine
  with the `tags` list. `backend` is the session's own backend filter, not
  an axis shortcut — to filter the backend axis, pass `backend:<value>` as
  a tag.
- **The objects.** The list routes for Automata (`GET /api/autonomous/prds`),
  Automations, templates, decision records and held questions share one
  object-tag filter: `?tag=<axis>:<value>` (repeatable), `?tag_match=any|all`,
  the single-value shortcuts `?project=`, `?automation=`, `?automaton=`, and
  `?tag_id=` for a registry tag id. The legacy bare `?tag=<id>` id form keeps
  working on the Automata list. For example
  `GET /api/autonomous/prds?tag=stage:task&tag=project:web&tag_match=all`.
- **The tags in use.** `GET /api/sessions/tags` (MCP `session_tag_list`,
  comm `autonomous session-tags list`) lists every tag in use across live and
  archived sessions with its axis, its value and the number of sessions that
  carry it — the quickest way to see what values an axis actually has before
  filtering on one.

### Templates

Each axis stamps its tag from a **template** in which the axis's own
placeholder is replaced by the value. The default template of each axis is
the conventional `<axis>:<placeholder>`:

| Axis | Default template | Placeholder |
|---|---|---|
| `compute` | `compute:{node}` | `{node}` |
| `peer` | `peer:{peer}` | `{peer}` |
| `backend` | `backend:{backend}` | `{backend}` |
| `lane` | `lane:{lane}` | `{lane}` |
| `role` | `role:{role}` | `{role}` |
| `project` | `project:{project}` | `{project}` |

A template **must contain its axis's placeholder** — the compute template
must contain `{node}`, the project template `{project}`, and so on — anything
else is refused and nothing is written. You may add your own text around it:
`compute:{node}-box` writes `compute:nodeA-box` for a session that ran on
`nodeA`.

### Custom axes

The YAML config file can declare additional axes, one block per axis, under
`autonomous.tags.placement.custom`. Each entry has:

| Field | Rule |
|---|---|
| `name` | lowercase letters, digits and hyphens; unique; not one of the ten built-in axis names |
| `field` | one of the record fields: `Name`, `Backend`, `Model`, `Node`, `PRDID`, `Role`, `Project`, `Automaton`, `Automation`, `Stage`, `Kind` |
| `template` | must contain the placeholder `{value}`, replaced by the field's value |
| `enabled` | on or off for that axis |

A custom axis writes its tag whenever the field has a value; empty fields
write nothing. There are no custom axes by default. Because the block is a
list, it is not settable as a single `key=value`: write it in the YAML file
(or a full config write), not with `config set`.

### System tags: visible everywhere, immutable to you

Placement tags land in the tag registry as **system tags**, top-level in the
tree:

- **Create-or-return.** A placement tag name always resolves to the same
  registry entry: a repeated placement on this daemon, or the same fact
  stamped by another daemon, returns the existing tag. Repeats never create
  duplicates and never change the tag that is found.
- **Refused edits.** Renaming, moving and deleting a system tag are refused
  with nothing changed. That is what makes a federated tree trustworthy:
  every daemon that derives the same fact resolves the same name, so the tag
  keeps pointing at the same registry entry.
- **Filter and group, don't edit.** Every surface can filter by a system
  tag or by a whole axis (`compute:*`); the PWA groups them by axis in the
  session list and the dashboard.
- **One row per tag in the Sessions header Tags menu.** The menu lists each
  tag exactly once: the placement tags (`axis:value`) appear only in the
  **Placement tags (system)** section, under their own axis heading; the
  Tags section beneath it carries the remaining system tags (with the
  padlock mark) and your operator tags, exactly as before. Ticking a
  placement tag checks its single row, and the match-mode (any/all) toggle
  and Clear work as before even with a placement tag selected. The tag
  filter rows elsewhere (Automata, tasks, facets) are unchanged and still
  show system tags with the padlock. It is display-only: tags, stored
  selections and the results of an active tag filter are untouched —
  refresh the PWA and the menu is correct.

### Stamping, re-stamping and the startup backfill

- **Every session the daemon starts is stamped at placement** with the axes
  its record supports: node (`compute:`), backend, lane, role, project and,
  for an Automaton's work, its Automaton, Automation, stage and kind (and
  the peer, when the placement names one). The stamp rides the daemon's
  start hook, so it runs on every start path — a session you start, a
  scheduled start, a comm-channel start, an Automaton's work — and a session
  that belongs to no Automaton carries no `automaton:` or `automation:`
  tag, just the axes its record supports. The tags live in the session's
  `system_tags`, which the machine owns.
- **Every session you start carries the axes its record supports.** A
  start through `POST /api/sessions/start` (REST, the MCP `start_session`
  tool or `datawatch session new`) stamps the new session with its
  compute node — the local node, so the tag reads `compute:local`, when
  it stored no node — its backend, its lane (when the backend names one)
  and its project. Tagging is best-effort after the session already
  exists: a failure is logged and the session stays usable without its
  tags.
- **A session started with launch facts carries every axis that applies
  to it.** `launch_role`, `launch_kind` and `launch_stage` are stored on
  the session record, and the session's placement is built from those
  facts plus the record's own: the node it ran on — for a session that
  ran locally and stored no node, the local node, so the tag reads
  `compute:local` — its role and kind (such as `role:planner` or
  `role:supervisor`) and its stage, alongside the backend, lane and
  project it records and the Automaton and Automation the work belongs
  to (when `launch_automaton` names one). The axes present are exactly
  the ones the record supports (the rule is in [The ten axes](#the-ten-axes)
  above), and a start with no launch facts carries none of the
  role, kind, stage, automaton or automation axes because it records
  none of those facts. Turn an axis off and its tag is no longer written
  on any session.
- **A restarted session re-stamps from its record.** Restarting a session
  (the PWA Restart button, `POST /api/sessions/{id}/restart`, the MCP
  `restart_session` tool, the CLI `datawatch session restart`, or the
  restart path that reuses a completed or failed session's ID) runs the
  same placement stamp a fresh start does, so a restarted session keeps
  the role, kind, stage and automaton axes its record holds — no sweep is
  needed after a restart. A restart that fails changes no tags.
- **An existing session with no recorded launch facts keeps the tags it
  already has.** Records written before launch facts existed load with
  empty values, and the sweep re-derives them the way it always has: role
  and kind from the session name, and no node fallback when the record
  names no node. The daemon never invents an axis it cannot read from the
  record, so a pass over such sessions leaves them unchanged. Where the
  record does carry launch facts, the record — not the session name — is
  what the daemon reads.
- **A retry on another node re-stamps the same session.** The whole
  `system_tags` list is replaced, so the old node's `compute:` tag and any
  old `peer:` tag are gone and the new placement's tags are the ones you
  see. The list always says where the work last ran, not where it first ran.
- **Starting up fills in what it missed.** Once at daemon startup a
  backfill pass derives tags for the live sessions the daemon still holds
  a lease on, from the lease's facts (node, backend, model, project).
  Finished sessions and sessions the daemon no longer owns are skipped;
  rerunning the pass at the next startup changes nothing. Right after it,
  the one-time tag sweep below re-derives the tags of every session and
  record still on disk against the in-force block.
- **Turning the switch off clears.** When you set the master switch (or one
  axis) to `false`, the next time a session is (re-)stamped the placement
  tags (or that axis's tag) are removed instead of rewritten.

### The tag sweep

The tag sweep applies the in-force `tags.placement` block to **everything
that already exists**: one pass over every session (live and archived) and
every record the Automata own — Automata, tasks, Automations, decision
records, templates and held questions — re-deriving each one's system tags
from its stored facts and writing only the ones that changed. For a
session that records its launch facts the pass rebuilds the placement
through the same build as at launch — including the local node
(`compute:local`) for a local session that stored no node — while a
session without launch facts keeps the derivation it always had, so the
sweep leaves those sessions unchanged rather than guessing at missing
facts.

It is safe to run any time:

- **Idempotent.** A second pass changes nothing, so the sweep never breaks
  what is already consistent.
- **Follows the block.** Turning an axis off removes that axis's tags,
  turning it on adds them, and a template change re-stamps the tag where
  the fact exists.
- **Never touches your tags.** The tags you assign yourself and the tags a
  session inherits are separate fields the sweep never reads or writes.

It runs on its own in two cases:

- **When you save a change under `autonomous.tags.placement.*`.** The save
  answers first, and the pass runs in the background right after the write;
  a change that touched no placement key runs no sweep.
- **At daemon startup**, once, after the lease backfill. The daemon
  remembers the last block it swept, so a restart sweeps again only when the
  block has actually changed since.

And it is available on demand, on every surface:

| Surface | How |
|---|---|
| **REST** | `POST /api/autonomous/tags/sweep`, with `?dry_run=true` or `dry_run` in the JSON body for a no-write pass |
| **MCP** | `autonomous_tag_sweep` (`dry_run`) |
| **CLI** | `datawatch autonomous tags sweep [--dry-run]` (alias `tag-sweep`) |
| **Comm channel** | `autonomous tags sweep [dry-run]` |
| **PWA** | the sweep button in Settings → General → Automata → **Placement tags** |

The reply is a **per-kind tally** — sessions, automata, automations, tasks,
templates, decision records, held questions — each with examined, changed,
unchanged and failed. `dry_run` reports the same tally without writing a
single tag.

### Inherited by sessions and Automata

- **Sessions** carry their placement tags in `system_tags`, separate from
  the tags **you** assign on the session. They are read-only: the session
  tag editor never sends them, and editing your own tags on the session
  leaves them in place.
- **Automata** show placement tags as a **read-only inherited set**: the
  union of the system tags of the sessions the Automaton owns, computed when
  the Automaton is read and never stored on it. The Automaton's own tag list
  stays exactly yours; the placement tags only ride along for display, so an
  Automaton's card shows where its work ran without you touching it.

### One shared read, not one per Automaton

- **A list is one read.** The inherited tags of every Automaton in a list
  come from a **single read of the sessions — live and archived alike —
  taken when the list is opened**, not one read per Automaton. A long list
  opens in about the time of a single read, even with thousands of archived
  sessions.
- **The result is kept briefly.** That read's result is cached for a short
  time (`autonomous.inherited_tags_cache_seconds`, default **15 seconds**;
  `0` means the default, a negative value turns the cache off), so the web
  app polling the list does not repeat the work on every poll.
- **Tag changes show at once.** The kept result is dropped at the moment a
  session is archived, deleted, restored, or its placement tags change, so a
  tag change shows on the next read — it never waits out the cache window.
- **A single Automaton reads the same kept result.** Opening one Automaton
  uses the same shared result and does not scan the archive again on its
  own; if the result is fresh it is used as-is.
- **The archive no longer blocks the rest of the daemon.** Reading the
  archive happens pass by pass, without holding the live sessions up, so a
  slow or large archive cannot stall other daemon work.

### Configuration

Under `autonomous.tags.placement`, one key per axis, plus the master:

| Key | Default | Meaning |
|---|---|---|
| `autonomous.tags.placement.enabled` | `true` | master switch; `false` turns the whole block off |
| `autonomous.tags.placement.<axis>.enabled` | `true` | on/off for that axis (`<axis>` is `compute`, `peer`, `backend`, `lane`, `role`, `project`, `automaton`, `automation`, `stage`, `kind`) |
| `autonomous.tags.placement.<axis>.template` | `<axis>:<placeholder>` (see the table above) | the tag text with the axis's placeholder |
| `autonomous.tags.placement.custom` | empty list | user-defined axes (YAML file or full config write) |
| `autonomous.inherited_tags_cache_seconds` | `15` | lifetime of the shared inherited-tags read (the section above); `0` means the default, negative turns the cache off — note this one lives directly under `autonomous`, not under `tags.placement` |

Set and read them the same way as the tag-browser keys above: `PUT/GET
/api/config`, the PWA **Settings → General → Automata → Placement tags**
group, `datawatch config set|get autonomous.tags.placement.…`, the MCP
`config_set`/`config_get` tools, and the comm channel's `configure` /
`config get` with the same keys. An invalid template (one missing its axis's
placeholder) or an invalid custom block is refused on every surface with the
same message and nothing written. Changes apply without a restart, and
saving a change under this block runs one tag sweep over the existing
sessions and records right after the save — see [The tag sweep](#the-tag-sweep).

### Tuning the shared read: `inherited_tags_cache_seconds`

How long the daemon keeps the result of the shared inherited-tags read
before repeating it. Default **15 seconds**; `0` means "use the default",
a negative number turns the cache off entirely (every read repeats the
work, which you only want for testing). Changes apply without a restart, and
a tag change always shows on the next read regardless of the setting.

| Surface | Example |
|---|---|
| MCP | `autonomous_config_set` `{"inherited_tags_cache_seconds": 30}` |
| REST | `PUT /api/config` `{"key":"autonomous.inherited_tags_cache_seconds","value":30}` |
| CLI | `datawatch config set autonomous.inherited_tags_cache_seconds 30` |
| Comm channel | `configure autonomous.inherited_tags_cache_seconds=30` |
| PWA | Settings → **General** → **Inherited tags cache (sec, 0=15, negative=off)** |
| YAML | `autonomous:` → `inherited_tags_cache_seconds: 30` |

Raise it if you poll the Automata list often and want the daemon to repeat
the read less often; keep it near the default if you want tag changes to be
visible within a few seconds.

Listing the tags takes the same five surfaces as the rest of this howto, plus
two filters for the system tags: `GET /api/autonomous/tags` accepts
`?system=true` (system tags only) and `?axis=<name>` (one axis — the
`<axis>:<value>` names and the bare axis name when one exists); the MCP
`autonomous_tag_list` tool takes the same `system` and `axis` parameters, and
the CLI `tag-list` takes `--axis`.

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
| List Automata by an object axis | `GET /api/autonomous/prds?tag=stage:task[&tag_match=all]` (also `?project=`, `?automation=`, `?automaton=`, `?tag_id=`) | — | — | — |
| Sweep the in-force tag settings | `POST /api/autonomous/tags/sweep[?dry_run=true]` | `autonomous_tag_sweep` (`dry_run`) | `tags sweep [--dry-run]` (alias `tag-sweep`) | `tags sweep [dry-run]` |
| The tags in use on sessions | `GET /api/sessions/tags` | `session_tag_list` | — | `session-tags list` |

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
- **Tried to rename or delete a placement tag.** They are system tags: the
  registry refuses rename, move and delete. Filter by the tag or its axis
  (`axis:*`) instead.
- **Looking for a `peer:` tag on local work.** Local sessions run on this
  daemon and write no peer tag; `peer:<name>` appears only when the session
  ran on another daemon.
- **Expecting the sweep to add a `compute:` tag to an old local session.**
  A session that stored no node only gains `compute:local` once its record
  carries launch facts — the sweep then rebuilds its placement from the
  record, with the local-node fallback. A record without launch facts keeps
  the derivation it always had, node tag or not. To see exactly what a
  sweep would do to your sessions, run it with `--dry-run` first.
- **Expects a `stage:` or `kind:` tag on an Automaton.** An Automaton carries
  its own project, its Automation and itself; the stage rides on its tasks
  and the kind on the runs. Filter a task or session list by the axis.
- **Expects the sweep to touch its own tags.** The operator-assigned tags
  and the tags a session inherits are separate fields the sweep never reads
  or writes; only the derived system tags are re-derived.

## Next steps

- [automata-auto-tags](automata-auto-tags.md) — the automation, stage and kind
  tags the daemon keeps on every Automaton, and the filters they power.
- [automata-tag-registry](automata-tag-registry.md) — one tag registry across
  federated daemons, the keeper, stale copies and merge notes.
- [autonomous-planning](autonomous-planning.md) — create and run Automata.
- [automata-capacity](automata-capacity.md) — how queued Automata wait.
