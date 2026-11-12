---
docs:
  index: true
  topics: [sessions, pwa, tags, automation, mcp, cli]
exec_params:
  - {name: group_by, required: false, description: "Group axis: tag, state, node, project or automaton"}
  - {name: tag_prefix, required: false, description: "With group_by=tag: only tags with this prefix (e.g. automation:) form groups; the rest collect in the ungrouped group"}
exec_steps:
  - tool: list_sessions
    description: "Inventory the host's sessions flat (no grouping) to see the pool the grouping will cut"
    args: {}
    read_only: true
  - tool: list_sessions
    description: "Read the same sessions grouped by the chosen axis; automaton blocks carry their stories and done/total progress"
    args:
      group_by: "{{params.group_by}}"
      tag_prefix: "{{params.tag_prefix}}"
    read_only: true
---
# How-to: Sessions — tiles, grouping and saved views

The Sessions page can now be read three ways — **list**, **tiles** or
**compact** — and cut by **group**: by tag, tag prefix, state, node, project,
or by the Automaton that spawned each session. A session's placement tags
(compute:, peer:, backend:, lane:, role:, project:) make the tag axis double
as a placement browser, and a sub-task nesting under the owning Automaton
lets you see a whole story's sessions in one block. Every cut is the same
shared read: the PWA, REST, MCP, the CLI and the comm channel all show the
same groups, counts and state mixes.

## What it is

Three independent dials on the Sessions page:

1. **View mode** — how each session is drawn: `list` (one card per row),
   `tiles` (a grid of work tiles that stacks left-to-right and up-down), or
   `compact` (a dense two-row grid).
2. **Group by** — none, or one axis: `tag`, `state`, `node`, `project` or
   `automaton`. On the tag axis you can narrow to a tag prefix, so only tags
   under that prefix form groups and everything else collects in the
   ungrouped group.
3. **Saved views** — a named capture of the mode + group axis (+ prefix) +
   session filter, kept per device, so "tiles grouped by the automation:
   prefix" is one tap away next time.

Groups are collapsible, remember their collapsed state per device, and the
group body renders in whatever view mode is active. Every mode and every
group label is localised in all five locales (en, de, es, fr, ja).

```
Sessions
┌─[ List │ Tiles │ Compact ]   Group by: [ No grouping ▾ ]
│  Views:  [View name ____]  [+ New view]   [my-cut ✎ ✕]
│
│  ● running 4   ● waiting_input 2   ● complete 11   ● failed 1   ● killed 3
│  ────────────────────────────────────────────────────────────
│  ▾ automation:lane-fill        4  (running 2, waiting_input 1, complete 1)
│      [tile / row per session in the active mode]
│  ▾ automation:queue            2  (running 1, failed 1)
│  ▸ Ungrouped                   11 (complete 9, failed 1, killed 1)
```

## Base requirements

- A running daemon with sessions on it — `datawatch session list` (or the
  PWA Sessions page) already shows you the flat list this feature extends.
- For the **tag** axis: sessions that carry tags, either their own or
  inherited from the Automaton that spawned them (a session spawned by an
  Automaton shows that Automaton's tags as inherited, read-only tags).
  Placement auto-tags (compute:, peer:, backend:, lane:, role:, project:)
  are added automatically by the lane and queue machinery. See
  [`automata-tags.md`](automata-tags.md) for the tag model.
- For the **automaton** axis: sessions that belong to an Automaton
  (spawned by one). Sessions without one land in the ungrouped group.
- For the **node** axis: multi-node or peer hosts are the interesting case;
  a single local host will show one group (named by its hostname) plus an
  ungrouped group. Federation peers must be connected for their sessions to
  appear under their own node in the web UI (see [the node axis](#the-node-axis-in-the-web-ui)).

No configuration to enable and no new settings: the feature is a pure
presentation cut over the existing session list, and every dial below is a
per-device UI preference or a read-only API parameter.

## Setup

There is nothing to configure. Open the PWA, go to **Sessions**, and the
mode toggle, the Group-by picker and the Views bar are already at the top
of the list.

If you only need one of the three, jump to the matching section:

- want the grid of tiles → [View modes](#view-modes)
- want the tag / prefix / Automaton cut → [Grouping](#grouping)
- want to keep a cut for later → [Saved views](#saved-views)
- want to read the same cut from another channel → [All channels](#all-channels--the-shared-read)

## View modes

The toggle at the top of the Sessions page has three buttons: **List**,
**Tiles**, **Compact**.

- **List** is the classic one-card-per-row layout. It stays the default on
  phone-width viewports (narrower than 600 px), where the dense rows read
  better on a small screen.
- **Tiles** is the grid: work tiles that stack left-to-right as well as
  up-down, reflowing with the window width. This is the default on wider
  viewports. Each tile shows:
  - the session's name (falling back to its task, then its short id),
  - a state chip (running / waiting_input / rate_limited / complete /
    failed / killed),
  - `backend · model` (e.g. `claude-code · claude-opus`),
  - the node or peer the session runs on,
  - its tags (own + inherited; more than fit collapse into a shared
    overflow chip),
  - the last-activity time, and
  - a lifecycle progress bar — a finished session fills the bar, a live one
    (running, waiting_input, rate_limited) shows a half bar so it reads as
    in flight, and a failed or stopped one shows an empty bar.
- All of the tile's actions move into the **⋯ overflow menu**:
  - active sessions: **Stop**, and **Commands** when the session is
    waiting for input;
  - finished sessions: **Restart** and **Delete**;
  - every session: **Summary** (when the summarizer is enabled),
    **Watch for updates**, and **Mute notifications**.
  The menu stays visible in every grid mode precisely because the tile
  carries menu items.
- **Compact** is the same tiles as one column of two-line rows, for scanning
  long lists. Line one is the session's name, its state chip and the count;
  line two is `backend · model`, the tag count and the last-activity time.
  The lifecycle bar, the tag chips and the node line are hidden, and the
  actions stay in the **⋯ overflow menu**. Unlike tiles it keeps that
  density on resizes instead of following the viewport width.

Two details worth knowing:

- The choice is **per device**. It is stored in this browser's local
  storage (`cs_sessions_view_mode`) and does not travel with federation
  peers or other devices. Each of your devices keeps its own mode.
- In **tiles** mode the grid keeps following the viewport width on resizes,
  so rotating a tablet re-lays the tiles out without a reload.

### The state summary bar

Above the list, a summary bar shows one chip per state present, with its
count, and the total session count at the end (e.g.
`● running 4   ● waiting_input 2   sessions 21`). Clicking a state chip
**filters the page to that state**; the **All** chip in the toolbar's
**State** dropdown clears the selection. The chip choice is remembered per
device (`cs_session_state_chip`). Picking a historical state (complete,
failed, killed, …) also auto-enables the History toggle so those sessions
actually become visible instead of silently filtering to nothing.

### "Show more" paging

Long lists render in pages of 60 sessions by default. When there are more
than a page, a **Show more** button appears under the list with a
`shown/total` counter (e.g. `Show more (60/214)`); each click appends the
next page. The counter resets whenever a filter, a group axis or a saved
view changes, so you never page through a stale list. The flat list keeps
its existing load-more behaviour for the non-paged view.

## Grouping

The **Group by** picker offers:

```
No grouping │ State │ Node │ Project │ Tag │ Tag(automation:) │ Tag(compute:) … │ Automaton
```

The tag entries after `Tag` are built from the prefixes present in your tag
registry (your automation:/stage:/kind: tags plus the placement auto-tags),
so the picker shows exactly the prefixes you can narrow to — e.g.
`Tag(automation:)`, `Tag(compute:)`, `Tag(peer:)`.

| Axis | What groups | Notes |
|---|---|---|
| `tag` | the first matching tag (own tags + inherited) | A session with several tags lands in exactly one group (the first in the sorted tag list; tags are trimmed and a repeated tag counts once). Sessions with no matching tag collect in the **ungrouped** group. |
| `tag` + prefix | the first tag carrying that prefix | Only tags with the prefix (e.g. `automation:`) form groups; everything else goes to the ungrouped group. This is how you get the "just the automation cut" or "just the compute-node cut". |
| `state` | the session's state | One group per state (running, waiting_input, complete, …) — handy for a quick "what is stuck" scan. |
| `node` | the node that runs the session | In the web UI a peer's session lands under the peer's name and every session of this daemon under its real hostname; the header shows the node's name. In the grouped read (REST, MCP, CLI, comm) the key is the session's hostname, and a session with no hostname of its own is keyed under this daemon's hostname because it ran here. The ungrouped group catches only a row with no node name at all. See [the node axis](#the-node-axis-in-the-web-ui). |
| `project` | the session's project directory | Trailing slashes are dropped, so `/work/app` and `/work/app/` form one group (the page and the grouped read use the same key). The ungrouped group catches sessions without one. |
| `automaton` | the Automaton the session belongs to | **Sub-task nesting**: sessions sit under the Automaton block, and inside it under their story block. In the grouped read (CLI, comm) each block also carries a done/total session tally. Sessions with no Automaton land in the ungrouped group. In the web UI, an Automaton's task sessions that run on a federation peer are included too (see [the node axis](#the-node-axis-in-the-web-ui)). |

Group mechanics, in every mode:

- **Collapse / expand.** Each group header has a caret; your collapsed
  state is remembered per device and per axis + prefix, so a grouping you
  live in stays the way you left it.
- **Count and state mix.** The header shows the group's session count and a
  per-state mix (e.g. `4 (running 2, waiting_input 1, complete 1)`), so you
  can read "how much of this group is alive" without expanding it.
- **Body.** The group's sessions render in the active view mode — list
  cards, tiles or compact rows — and the body pages with its own **Show
  more** step (shown/total) when it is longer than a page.
- **Sorting.** Groups sort by key, and the ungrouped group always sorts
  last — in the web UI and in the grouped read alike. Inside an Automaton
  block the root story (shown as **Story**) leads, then the other stories by id.
- **Header title.** In the web UI a group header on the **automaton**,
  **project** and **node** axes shows the record's human title — the
  Automaton's title, the project's name, the node's name — never the raw id.
  The id stays as small secondary text beside the title and in the header's
  tooltip. When no title is known yet (the lists load in the background on
  first use of the axis) or the title equals the id, the header shows the id
  alone. The grouped read on the other channels still carries the plain key.

### The node axis in the web UI

Grouping by node has to cover more than this daemon, so the web UI treats
it specially:

- **Where a session lands.** A session read from a federation peer is
  tagged with the peer's name and lands in that peer's group. Every other
  session is this daemon's own and lands under its real hostname (the row's
  own, else the hostname the page learnt from the daemon's health read).
  The aggregated read's internal `local` tag is never a group name, so this
  daemon's sessions are not split in two and never appear under "local".
- **Peers are loaded on demand.** While the page is in local mode and the
  node axis or the Automaton axis is selected (an Automaton's task sessions
  may run on a peer), the Sessions page also reads the aggregated
  sessions list and merges the peers' rows in. They refresh with the
  page's regular refreshes and are dropped again as soon as you switch to
  any other axis (or to an applied saved view on another axis; the node and Automaton axes keep them), so the
  plain local list is unchanged. The read is read-only; if it fails the
  page keeps the rows it holds.
- **Not the same as the grouped read.** The REST, MCP, CLI and comm
  `group_by=node` read covers this daemon's own sessions only and keys on
  the session's hostname, so a peer's sessions appear there only on that
  peer. Read the peer's daemon (or use the web UI) for its group.

### The automaton axis, in detail

This is the sub-task view: one block per Automaton, and inside each block
one sub-block per story (the root story is labelled "Story"). The Automaton
header carries the block's count and its per-state mix, so a glance tells you
how many sessions the Automaton has and how many of them are still live:

```
▾ a12f0001                                    3   (running 1, complete 2)
    ▾ Story                                   2   (running 1, complete 1)
        [tile/row] doc task …
        [tile/row] code task …
    ▾ story-2                                 1   (complete 1)
        [tile/row] verify task …
▸ Not part of an Automaton                    1   (complete 1)      ← sessions with no Automaton
```

The same cut over the grouped read (CLI and comm) adds a **done/total**
session tally per block — how many of the block's sessions have reached a
terminal state (complete, failed or killed) out of the block's total — so
`— done 2/3` answers "how far along is this Automaton's sessions":

On the **automaton** axis the ungrouped group is labelled
**"Not part of an Automaton"** in the PWA (it still carries the plain
`ungrouped` key on the other channels); on every other axis it shows as
**"Ungrouped"**.

## Saved views

The **Views** bar sits above the list: a "Views" label, a **View name**
input and a **+ New view** button, plus one chip per saved view:

- **New view** — type a name and click **+ New view**: that captures the
  *current* combination (view mode + group axis + tag prefix + session
  filter) under the name. The name is trimmed and capped at 80 characters;
  saving again adds another entry, so keep names distinct.
- **Apply** — click the chip's name to restore mode, axis, prefix and
  filter in one click.
- **✎** renames (inline, with an OK button), **✕** deletes.

Saved views are **per device** (this browser's local storage,
`cs_sessions_saved_views`): they do not travel with federation peers, and a
view you save on your phone is not on your laptop. That is intentional —
they are personal cuts, not shared configuration.

A useful starting set:

| View name | Mode | Group by | Filter |
|---|---|---|---|
| `automation cut` | tiles | `Tag(automation:)` | — |
| `live work` | compact | `State` | running |
| `my automaton` | list | `Automaton` | — |

## All channels — the shared read

The grouping is one read, shared by five channels. The same parameters, the
same groups, the same counts and state mixes, wherever you read them:

### PWA (web UI)

The Sessions page controls above: mode toggle, Group-by picker (with the
tag-prefix entries), the state summary bar, Show more paging, collapse
carets and the Views bar. All of it is localised in the five locales — the
labels you see are the translated ones in your PWA language.

### REST

```
GET /api/sessions?group_by=tag&tag_prefix=automation:
GET /api/sessions?group_by=automaton
```

- `group_by` — `tag`, `state`, `node`, `project` or `automaton`. Anything
  else is refused with `invalid group_by "<x>": want tag, state, node,
  project or automaton`.
- `tag_prefix` — only meaningful with `group_by=tag`; ignored on every
  other axis (the handler decides, not the client).
- Without `group_by` the endpoint returns the flat list exactly as before;
  the grouped response only appears when you ask for it, so existing
  scripts and dashboards are unaffected.

Grouped response shape (abridged):

```json
{
  "group_by": "automaton",
  "groups": [
    {
      "key": "a12f0001",
      "count": 3,
      "state_mix": { "running": 1, "complete": 2 },
      "progress": { "done": 2, "total": 3 },
      "stories": [
        { "key": "root", "count": 2, "state_mix": { "running": 1, "complete": 1 }, "sessions": [ … ] },
        { "key": "story-2", "count": 1, "state_mix": { "complete": 1 }, "sessions": [ … ] }
      ]
    },
    { "key": "ungrouped", "count": 1, "state_mix": { "complete": 1 },
      "progress": { "done": 1, "total": 1 },
      "stories": [ { "key": "root", "count": 1, "state_mix": { "complete": 1 }, "sessions": [ … ] } ] }
  ],
  "state_totals": { "running": 1, "complete": 3 },
  "total": 4
}
```

- On the **automaton** axis the sessions live inside the `stories[]` blocks
  (group-level `sessions` is absent); on every other axis each group simply
  carries its `sessions` array, with no `stories` or `progress`.
- The root story is the block keyed `root` — rendered as **Story** in the
  PWA and `(root)` in the CLI.
- `state_totals` and `total` are the page-level sums, so the groups always
  add up to what the flat list shows.
- Each member's `chips` lists its remaining tags (own + inherited): on the
  tag axis that is the tag list minus the tag that placed the session in
  the group; on the other axes it is the full tag list.

### MCP

`list_sessions` takes the same two parameters:

- `group_by` — `tag`, `state`, `node`, `project` or `automaton`. Returns
  the grouped response (one group per key with its count, per-state mix and
  its sessions, plus per-state totals; on `automaton` each Automaton also
  carries its story blocks and done/total progress). Always JSON.
- `tag_prefix` — with `group_by=tag`: only tags with this prefix (e.g.
  `automation:`) form groups; the rest collect in the ungrouped group.
  Ignored without `group_by=tag`.

Example: `list_sessions { "group_by": "automaton" }` and
`list_sessions { "group_by": "tag", "tag_prefix": "automation:" }`.

### CLI

```
datawatch session list --group-by <axis> [--tag-prefix <prefix>]
```

- `--group-by` — `tag`, `state`, `node`, `project` or `automaton`; with it,
  the read goes through the grouped endpoint.
- `--tag-prefix` — narrows the tag axis the same way as the API
  `tag_prefix`.
- The CLI renders the full grouped view: group headers with count and state
  mix, the group's sessions listed under it, story blocks on the automaton
  axis, and the per-state totals at the bottom.

```
$ datawatch session list --group-by tag --tag-prefix automation:
Sessions grouped by tag (tag prefix "automation:"), 10 total:
  automation:lane-fill  4 (complete×1, running×2, waiting_input×1)
    [a1f2] lane-fill worker | running
    [b3c4] verifier | complete
    …
  ungrouped  6 (complete×5, failed×1)
    [d5e6] one-off probe | failed
    …

Per-state totals: complete×6, failed×1, running×2, waiting_input×1
```

On the automaton axis the story blocks and the block's progress count come
with the response and are rendered under the block header:

```
$ datawatch session list --group-by automaton
Sessions grouped by automaton, 7 total:
  a12f0001  3 (complete×2, running×1) — done 2/3
    (root)  2 (complete×1, running×1)
      [a1f2] doc task | running
      [b3c4] code task | complete
    story 2:  1 (complete×1)
      [d5e6] verify task | complete
  ungrouped  4 (complete×3, failed×1) — done 4/4
    (root)  4 (complete×3, failed×1)
      [e7f8] ad-hoc | complete
      [f9a0] probe | complete
      [a0b1] probe | complete
      [c2d3] one-off | failed

Per-state totals: complete×6, failed×1, running×1
```

### Comm channel

```
sessions groups [axis] [tag-prefix]
```

- `axis` — optional; defaults to `tag`. One of `tag`, `state`, `node`,
  `project`, `automaton`; unknown axes are refused.
- `tag-prefix` — optional second argument; only applies to the tag axis.
- The reply is a per-group count summary — a `by <axis> — N session(s)`
  line, then one line per group (key, count, state mix, plus the block
  progress in `[done N/M]` on the automaton axis), and a `totals:` line —
  the same numbers the other channels show.

```
you: sessions groups tag automation:
datawatch (Sessions by tag):
by tag, tag prefix "automation:" — 10 session(s)
  automation:lane-fill  4 (complete×1, running×2, waiting_input×1)
  ungrouped  6 (complete×5, failed×1)
  totals: complete×6, failed×1, running×2, waiting_input×1
```

```
you: sessions groups automaton
datawatch (Sessions by automaton):
by automaton — 7 session(s)
  a12f0001  3 (complete×2, running×1)  [done 2/3]
  ungrouped  4 (complete×3, failed×1)  [done 4/4]
  totals: complete×6, failed×1, running×1
```

## Tag prefixes worth grouping by

The placement auto-tags make the tag axis a placement browser. The
prefixes to narrow to:

| Prefix | What it marks |
|---|---|
| `automation:` | the Automation (lane fill, queue advance) the session runs under |
| `compute:` | the compute node / lane the session is placed on |
| `peer:` | the federation peer that placed or hosts the work |
| `backend:` | the LLM backend family |
| `lane:` | the specific lane (e.g. GPU vs CPU) |
| `role:` | the role the session plays in the Automation (worker, verifier, planner) |
| `project:` | the project the work belongs to |

So `Tag(automation:)` in the PWA, `--group-by tag --tag-prefix
automation:` in the CLI and `list_sessions {group_by: "tag", tag_prefix:
"automation:"}` in MCP all give you the "which Automation does this
session serve" cut of the list — and the same goes for any prefix present
in your tag registry.

## All options, on one screen

| Control | Where | Per-device? | Notes |
|---|---|---|---|
| View mode (list / tiles / compact) | PWA Sessions page | yes (`cs_sessions_view_mode`) | viewport default: list < 600 px, tiles ≥ 600 px; tiles reflow on resize |
| Group by (none / state / node / project / tag / tag+prefix / automaton) | PWA, REST `group_by`+`tag_prefix`, MCP `group_by`+`tag_prefix`, CLI `--group-by`/`--tag-prefix`, comm `sessions groups [axis] [tag-prefix]` | axis choice is per-device in the PWA; the read is stateless | unknown axis refused; `tag_prefix` only on the tag axis |
| Collapse a group | PWA group header caret | yes (`cs_sessions_collapsed_groups`, keyed by axis + prefix) | body still renders in the active mode |
| Show more (page) | PWA, under the list and under each group | no | 60 per page by default, `shown/total` counter, resets on filter change |
| State summary bar + state filter | PWA, above the list | chip choice per device (`cs_session_state_chip`) | historical picks auto-enable History |
| Saved views (mode + axis + prefix + filter) | PWA Views bar | yes (`cs_sessions_saved_views`) | save / apply / rename / delete; names trimmed and capped |
| Tile ⋯ menu actions | PWA, per tile | no | Stop / Commands (waiting_input) / Restart / Delete / Summary (if summarizer on) / Watch / Mute |
| Progress on tiles; done/total on automaton blocks | PWA tile bar; grouped read (CLI, comm) | no | tile bar is lifecycle-based; automaton blocks carry their done/total terminal-session tally |

## Troubleshooting

- **A session is in the ungrouped group on the tag axis.** It has no tag
  matching the chosen prefix (or none at all). Check the tile's tag chip
  and the inherited tags of its Automaton — a session spawned by an
  Automaton carries that Automaton's tags as inherited, read-only tags, and
  those count for placement. (On the automaton axis the same group is
  labelled "Not part of an Automaton".)
- **I see a tile, not a row, and I want rows back.** Switch the mode toggle
  to **List** — the choice sticks on that device. On phones the default is
  already List.
- **My saved view isn't on my other device.** Saved views are per device
  by design; recreate it from the Views bar. The *data* it reads (groups,
  counts, mixes) is identical on every channel and every peer.
- **A group says N but I count fewer rows.** The group body is paged —
  expand with its **Show more** (shown/total) step; the header count is
  the full group, not the rendered page.
- **`invalid group_by` from the API/CLI.** The axis must be exactly one of
  `tag`, `state`, `node`, `project`, `automaton`.

## Related guides

- [`sessions-deep-dive.md`](sessions-deep-dive.md) — session anatomy,
  lifecycle and resume.
- [`automata-tags.md`](automata-tags.md) — the tag tree, assignment and
  bulk tagging behind the tag axis.
- [`automation-lane-fill.md`](automation-lane-fill.md) — the lane fill
  automation that produces the lane/compute placement tags.
- [`list-endpoints-and-paging.md`](list-endpoints-and-paging.md) — why
  list endpoints are small and how paging works.
