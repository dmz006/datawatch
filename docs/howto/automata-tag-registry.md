---
docs:
  index: true
  topics: [automata, tags, federation, registry, keeper, sync]
exec_params:
  - name: tag_name
    description: "Name of a tag to create on this daemon (on a peer it is forwarded to the keeper)"
    required: true
  - name: note_id
    description: "Registry note ID to dismiss, as listed by the registry status"
    required: false
exec_steps:
  - tool: autonomous_tag_registry_status
    description: Show this daemon's role, keeper, stale flag, last sync, pending count and merge notes
    read_only: true
  - tool: autonomous_tag_list
    description: List the tag tree with subtree and direct counts (the registry this daemon serves)
    args:
      counts: true
    read_only: true
  - tool: autonomous_tag_create
    description: Create a tag; a peer forwards the create to the keeper
    args:
      name: "{{params.tag_name}}"
    read_only: false
  - tool: autonomous_tag_registry_dismiss_note
    description: Dismiss one merge or removal note on this daemon
    args:
      id: "{{params.note_id}}"
    read_only: false
---
# How-to: Share one tag registry across federated daemons

Several daemons in a federation can share one set of Automata tags. The same
tag names, parents, colors and descriptions then appear on each of them, so a
tag browser on any daemon shows the same tree. This how-to covers how the
shared registry is set up, what happens when the keeper is unreachable, and
how to read and dismiss the notes a merge leaves behind.

Read [automata-tags](automata-tags.md) first for the tag tree itself. The
sync rules are in [flow/tag-registry-sync-flow](../flow/tag-registry-sync-flow.md).

## What it is

- One daemon is the **keeper**. It holds the registry.
- The other daemons are **peers**. A peer keeps a cached copy of the keeper's
  tags on disk, pulls the keeper's copy on a timer, and sends its own tag edits
  to the keeper.
- A daemon with **no keeper** is **standalone**. It keeps its own tags, and
  nothing syncs. This is the default, and what a daemon did before the shared
  registry existed.
- The roles follow from one setting, `autonomous.tags.registry_keeper`. It is
  a federation peer name, and it is **chosen by the operator**, not inferred:
  you set it on each peer to the keeper's name, and on the keeper to its own
  name.

## Base requirements

- At least two daemons that are federation peers of each other. The keeper's
  name must be the name of a federation peer on each peer daemon (see
  [federated-observer](federated-observer.md) for adding a peer).
- The federation grants on the keeper must allow the peer's autonomous read
  (to pull the registry) and autonomous write (to forward edits). A peer
  without a write grant can still read, but its edits are refused the way
  any federated write is refused.
- A tag a peer creates while the keeper is unreachable is pushed to the keeper
  when it returns, and merged if the keeper already has the same name under
  the same parent (see steps 4 and 5).

## Walk-through

### 1. Name the keeper on every daemon

Set the keeper on each peer and on the keeper. Each change applies at once; no
restart is needed.

- **On the keeper:** `autonomous.tags.registry_keeper` = the keeper's own name.
- **On each peer:** `autonomous.tags.registry_keeper` = the keeper's name.
- Optional: `autonomous.tags.registry_sync_seconds` (a whole number of
  seconds, minimum 5, default 30) sets how often a peer pulls the keeper's
  copy.

The settings are listed with every channel in [the settings table](#the-settings-and-the-five-ways-to-set-them).

### 2. Check the role and the last sync

Open **Settings → Automata → Tag Registry**, or read the registry object
returned with the tag list. A keeper or a peer shows one status line:

> Shared registry: keeper · keeper: `<name>`

or, on a peer:

> Shared registry: peer · keeper: `<keeper name>`

A standalone daemon shows no status line at all. The same information is in
`registry.role`, `registry.keeper`, `registry.stale`, `registry.last_synced_at`
and `registry.revision` on `GET /api/autonomous/tags`.

### 3. Edit on a peer

Create, rename, move, recolor, describe or delete a tag on a peer as you
would on the keeper. The peer forwards the edit to the keeper, and the reply
shows the **keeper's** value, so what you see is what the keeper holds.

Each field of a tag has its own revision on the keeper. Two edits to
**different** fields of one tag both apply. Two edits to the **same** field,
made while apart, produce a conflict:

- The keeper keeps its own value.
- The peer shows a notice in the form *The keeper's version won: `<field>` is
  now `<value>`*, and the registry panel shows the keeper's value.

### 4. When the keeper is unreachable

A peer keeps working from its last copy and says so.

- **Stale copy.** The tag browser shows the banner *Showing the last copy from
  `<time>`; the keeper is unreachable*. Every tag count carries a *stale*
  mark, and the tag count is the count from that copy.
- **Refused writes.** Rename, move, recolor, describe and delete are disabled,
  with the reason as the tooltip. Over the API they return `503`, and nothing
  changes.
- **Creates still work.** A tag created on the peer is marked *pending sync*
  until the keeper takes it. The registry status counts these tags in
  `pending_sync_count`.

When the keeper answers again, the next sync (within `registry_sync_seconds`)
pulls its copy, pushes the pending tags, and clears the stale mark.

> **Current gap.** The PWA *pending sync* badge is not drawn yet in this
> build: the tag list the browser reads does not carry the per-tag pending
> flag. The count is correct in the registry status, on the API, in the CLI
> and in the comm channel. Until the badge is drawn, read the pending count
> from `tag-registry` or `pending_sync_count` instead of the browser.

### 5. Read and dismiss the merge notes

A tag created on two nodes while they were apart is **merged** when it reaches
the keeper, if the keeper already has a tag with the same name (ignoring case)
under the same parent. The peer rewrites its assignments to the keeper's tag,
and both nodes record a note, so the merge is never silent:

> Tag `<name>` was created on two nodes while they were apart and has been merged

A tag the keeper **deletes** while a peer still has Automata or sessions under
it leaves a note as well:

> Tag `<name>` was deleted on the keeper while this node still had Automata under it

Click **Dismiss** to remove a note on this node. Dismissing changes no tag and
no assignment, and a dismissed note stays dismissed when later snapshots
arrive. Each daemon keeps its own notes, so dismissing one on the keeper does
not clear the note on a peer.

## The settings and the ways to set them

Each setting below is changed the same way from every runtime channel. An invalid
value is refused on every channel with the same message, and nothing is written.

| Setting | Values | Default | Notes |
|---|---|---|---|
| `autonomous.tags.registry_keeper` | a federation peer name, or empty | empty (standalone) | This daemon's own name makes it the keeper. |
| `autonomous.tags.registry_sync_seconds` | whole seconds, minimum 5 | 30 | How often a peer pulls the keeper's copy. |

Channels for the settings (the five ways to set a value, plus the YAML file):

| Channel | How to set `autonomous.tags.registry_keeper` |
|---|---|
| **REST** | `PUT /api/config` with `{"autonomous.tags.registry_keeper": "<name>"}`; read back with `GET /api/config` under `autonomous.tags` |
| **PWA** | Settings → General → Automata → **Tag browser** group, the *Tag registry keeper* field |
| **CLI** | `datawatch config set autonomous.tags.registry_keeper <name>`; read with `datawatch config get autonomous.tags.registry_keeper` |
| **MCP** | `config_set` with the key `autonomous.tags.registry_keeper` (the MCP server needs `mcp.allow_self_config=true`) |
| **Comm channel** | `configure autonomous.tags.registry_keeper=<name>` |
| **YAML** | `autonomous.tags.registry_keeper:` in `config.yaml`, with the comment in [config-reference](../config-reference.yaml) |

Set `registry_sync_seconds` the same way, with the key
`autonomous.tags.registry_sync_seconds`.

## Same thing from every other surface

| Task | REST | MCP | CLI (`datawatch autonomous …`) | Comm channel (`autonomous …`) |
|---|---|---|---|---|
| Registry status (role, keeper, stale, pending, notes) | `GET /api/autonomous/tags` (the `registry` object) | `autonomous_tag_registry_status` | `tag-registry` | `tag-registry` |
| Dismiss a note | `POST /api/autonomous/tags/registry/notes/{id}/dismiss` | `autonomous_tag_registry_dismiss_note` | `tag-registry-dismiss <note-id>` | `tag-registry-dismiss <note-id>` |
| Edit a tag (forwarded on a peer) | `POST`, `PATCH`, `DELETE /api/autonomous/tags[/{id}]` | `autonomous_tag_create`, `autonomous_tag_rename`, `autonomous_tag_set_parent`, `autonomous_tag_set_color`, `autonomous_tag_delete` | `tag-create`, `tag-rename`, `tag-move`, `tag-color`, `tag-delete` | `tag-create`, `tag-rename`, `tag-move`, `tag-color`, `tag-delete` |

The keeper-to-peer routes (`GET /api/autonomous/tags/registry` and
`POST /api/autonomous/tags/registry/write`) are for daemons, not operators. A
peer asked for the keeper's snapshot answers `409`, because only the keeper
holds the registry.

## Common pitfalls

- **Edits refused on a peer with `503`.** The keeper is unreachable. Check
  the federation peer's address and the keeper's health; creates still work
  in the meantime.
- **The peer shows a stale copy from the start.** The keeper name must be the
  name of a federation peer on that daemon. When it is not, the peer treats the
  keeper as unreachable: it serves its cached copy, `stale` is true, and
  `last_error` names the keeper as not a federation peer. Fix the name, or add
  the peer.
- **A tag appears twice across nodes.** A same-name, same-parent tag created on
  a peer while apart is merged into the keeper's tag when the peer syncs. If
  the names differ, both tags stay; rename one.
- **Expecting an instant pull.** A peer pulls on its interval (default 30 s).
  Changing `registry_keeper` triggers an immediate reconfiguration; an ordinary
  tag edit on the keeper reaches a peer on the next interval.
- **Expecting a per-tag pending badge in the browser.** Not drawn in this
  build; see the gap note in step 4.

## Next steps

- [automata-tags](automata-tags.md) — the tag tree, browsing, the attention
  group and session tags.
- [federated-observer](federated-observer.md) — adding and trusting the
  federation peers the registry depends on.
- [flow/tag-registry-sync-flow](../flow/tag-registry-sync-flow.md) — the pull,
  write, stale, merge and note paths in one page.
