---
docs:
  index: true
  topics: [sessions, pwa, tags, automation, mcp, cli]
exec_params:
  - {name: group_by, required: false, description: "Group axis: tag, state, node, model, project or automaton"}
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
**compact** — and cut by **group**: by tag, tag prefix, state, node, model, project,
or by the Automaton that spawned each session. A session's placement tags
(compute:, peer:, backend:, lane:, role:, project:) make the tag axis double
as a placement browser, and a sub-task nesting under the owning Automaton
lets you see a whole story's sessions in one block. Every cut is the same
shared read: the PWA, REST, MCP, the CLI and the comm channel all show the
same groups, counts and state mixes.

## What it is

The Sessions page keeps all of its main controls in **one compact toolbar
line** that never splits into multiple rows, at any width: a **server
select** (All servers, this server, one entry per enabled peer), a
**search** box, a **Filters** button (with a badge counting what is active),
a **Fold all** button (while a grouping with openable groups is active) and
the **view mode buttons** — three icon buttons, one for each mode:
**List / Tiles / Compact**, the active one marked. One
line below the toolbar, the **Tree** and **History** buttons sit with the
clickable **🌐 <name>** indicator that appears only while a federated server
is the active one and returns you to this server when clicked (see
[multi-servers](multi-servers.md#switch-between-servers-header-dropdown)). Behind the Filters
button sit the grouping and saved-view controls. Three independent dials come out
of that:

1. **View mode** — how each session is drawn: `list` (one card per row),
   `tiles` (a grid of work tiles that stacks left-to-right and up-down), or
   `compact` (the same tile grid drawn at a smaller size).
2. **Group by** — none, or one axis: `tag`, `state`, `node`, `model`, `project` or
   `automaton`. On the tag axis you can narrow to a tag prefix, so only tags
   under that prefix form groups and everything else collects in the
   ungrouped group.
3. **Saved views** — a named capture of the mode + group axis (+ prefix) +
   session filter, kept per device, so "tiles grouped by the automation:
   prefix" is one tap away next time.

Groups are collapsible, remember their collapsed state per device, and the
group body renders in whatever view mode is active. Every mode and every
group label is localised in all five locales (en, de, es, fr, ja).

Nothing changed in settings or in server behavior: this is the same page with
its controls packed into fewer rows. There is no new setting, and the
daemon, REST, MCP, CLI and comm channel behave exactly as before.

```
header (fixed, every page):   …  [ ▾ All servers │ This server │ server-a │ server-b ]

Sessions (one toolbar line, at phone and wide widths):
┌─[ ▾ All servers ] [ Search…  ] [ Filters (2) ] [ ⊟ Fold all ] [ ☰ ⊞ ▤ ]
│  (⊟ Fold all shows only while a grouping is active; ☰ ⊞ ▤ are the three
│   view mode buttons — List, Tiles, Compact — with the active one marked;
│   the line below the toolbar carries Tree and History — and the 🌐
│   server-a indicator, shown only while a federated server is active)
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
one-line toolbar is already at the top of the list: the **server select**,
the search box, the **Filters** button and the **List / Tiles / Compact**
view mode buttons (icon buttons, the active one marked), plus the **Fold
all** button while a grouping is active. The
**Tree** and **History** buttons sit on the line below the toolbar, with
the 🌐 indicator while a federated server is the active one — that line
is behind the header's 🔍 *search & filters* button (right side of the
header bar): hidden by default, shown after you switch it on for this
browser. **Group by** and
the saved **Views** are inside the Filters sheet.

If you only need one of the three, jump to the matching section:

- want the grid of tiles → [View modes](#view-modes)
- want the tag / prefix / Automaton cut → [Grouping](#grouping)
- want to keep a cut for later → [Saved views](#saved-views)
- want to read the same cut from another channel → [All channels](#all-channels-the-shared-read)

## The toolbar row

Everything you need to find sessions sits in one non-splitting line at the
top of the page, at any width, in this order: **server select**, **search**,
**Filters**, **Fold all** (while grouping) and the **List / Tiles /
Compact** view mode buttons:

- **Server select** — the line starts with the Sessions page's own server
dropdown (an **All servers** entry, **This server**, and one entry per
enabled peer; a peer whose reachability check failed is marked
`(unreachable)`): picking an entry re-renders the page you're on against
that server (its sessions, its counts, its search text), and picking the
active one is a no-op. It is the same shared picker as the header dropdown
— choosing an entry in one updates the other, and both ride on the one
page-wide selection. See [multi-servers: Switch between
servers](multi-servers.md#switch-between-servers-header-dropdown).
- **Search box** — type to narrow the list. It matches a session's name, its
  id, its backend, its tag text and its state, so `claude`, `a1f2`,
  `automation:` or `waiting` all work. While a search is active the page
  shows an **N of M** count (for example `3 of 21`): how many sessions match
  out of how many there are. The search text is remembered on this device,
  so it is still there when you come back to the page; clear the box to see
  everything again.
- **Filters button** — toggles the Filters panel (below): one click opens
  it, clicking it again closes it. It also closes on **Escape** and on any
  click outside the panel, the same on the phone bottom sheet and the
  desktop panel. A badge on the button shows how many filters are active,
  so you can tell at a glance that the list is narrowed even with the
  panel closed.
- **Fold all** — an icon button (⊟ while at least one group can be folded, ⊞ once everything is folded; tooltip **Fold every group**, and **Unfold every group** after) that appears while a grouping is active and the visible list has foldable groups. It is a compact icon button, not a text button, and it folds or unfolds every group at once. It replaces the former separate fold row under the list, so there is no second row of controls for folding.
- **View mode buttons** — the **List / Tiles / Compact** control: three
  icon buttons (List ☰, Tiles ⊞, Compact ▤) sitting side by side in the
  toolbar line, with the mode in use marked on the active button. Tap (or
  click) the button for the mode you want; it is remembered on this device.
  Each button is an ordinary focusable button that carries its name —
  **List**, **Tiles**, **Compact** — as its accessible label for keyboard
  and screen-reader use, and the same name shows as the button's label on
  hover; on a touch screen holding a button for a moment shows its name as
  a small hint and does *not* change the mode. The three buttons take no
  more width than the dropdown they replaced — at 360 px phone width the
  line still does not split or wrap. Described next under
  [View modes](#view-modes).
- **One line below the toolbar** — the **Tree** and **History** buttons sit
  on their own line, with the clickable **🌐 <name>** indicator while a
  federated server is the active one: clicking it returns you to this
  server (picking **This server** in either server dropdown does the same).
  That line is behind the header's 🔍 *search & filters* button (right side of
  the header bar): hidden by default, shown after you switch it on for
  this browser.

### The Filters sheet

The **Filters** button opens a sheet that holds the controls you use less
often than search. The same button toggles it closed again — and so does a
tap outside the sheet, its close button, or Escape:

- **Group by** — the axis picker described under [Grouping](#grouping).
- **Views** — your saved views: save the current cut under a name, apply,
  rename or delete one (see [Saved views](#saved-views)).
- **Status chips** — one chip per state that is present in the list
  (running, waiting, stalled, rate-limited, complete, failed, killed),
  each with its count; a state whose count is zero stays off the sheet
  unless you have it selected. The group is a **multi-select set**
  (nothing picked = all): pick any number of states and the list keeps
  the sessions matching *any* of them, tap an active chip again to
  remove exactly that one, and the **All** chip — active while nothing is
  picked — clears the whole group. The same section holds the
  **watched only** toggle (plain on/off) and the **Backend** chips,
  which are a second multi-select set over the backend families present
  in the list with the same toggle-off and **All** behaviour; the two
  groups apply together (a session must match a picked state *and* a
  picked backend). The **Backend** group appears only when two or more
  backend families are present in the list, and it never touches the
  free-text search box. When you use tags, the section also carries the
  **Tags**
  filter (described next).
- **The floating "Start session" button clears out while the sheet is
  open.** On a phone the sheet is a bottom sheet, and while it is open the
  floating **Start session** button (bottom right) is hidden — it is one of
  the overlays that button steps aside from — and the sheet carries extra
  bottom padding, so its last rows (the **Status** chips and below) sit
  above the button's old zone and are fully visible and tappable. The same
  clearance applies to long pages: the Sessions list, Automata, Projects,
  Alerts, Observer, Dashboard, Settings and the detail views all keep extra
  padding under their last card, so nothing there hides behind the button,
  which itself keeps its size and position while no sheet or modal is open.

Close the sheet by tapping outside it, by pressing its close button, or by
pressing **Escape**. Your choices apply as you make them, so closing needs no
separate save.

While the sheet is open it is never redrawn out from under you: a list
refresh (timer or live) updates the session list behind the sheet and
patches only the numbers and options inside the sheet — the same controls,
with their values, focus and scroll position kept. While a sheet control is
engaged — an open dropdown or a search box you are typing in — even that
in-place patch waits, then runs once when the control is released; the tag
search boxes in particular keep focus and cursor as you type.

#### The Tags filter

A busy daemon carries hundreds of tags — one per Automaton, stage and task —
so the Tags section does not draw one chip per tag. It is built like this:

- **Search tags** — a box at the top narrows the chips as you type (case
  insensitive; it matches the tag's value, its axis, your tag's name and the
  full tag name). Typing keeps the cursor in the box. While you search every
  matching group is open and shows all of its matches.
- **Names lead, ids trail** — no list in the sheet leads with a raw id. A
  chip's main text is the readable name and the id rides behind it as small
  secondary text (the tooltip is "Name (id)", never the id alone): your
  operator tags show their name, the **Project** group shows the registered
  project name or the folder name (the directory path, when present, is the
  secondary text — a path is never the main text), the **Compute node**
  group shows the node's registered name with the recorded key as secondary
  text, and the **Find an Automaton** picker shows the title first with the
  id small beside it. The selected-tags chips above the list follow the same
  rule (an Automaton shows its title, not its id). When a lookup resolves
  to no name the chip says **unnamed** with the id small — a bare id is
  never the primary text. Filtering is untouched: selections, match mode,
  saved views and the tags themselves all key on the tag's value as before.
- **Groups** — chips sit under collapsible headers in a fixed order: **Kind**,
  **Role**, **Lane**, **Compute node**, **Project**, **Your tags**, **Other**.
  Each header shows how many tags it holds for the current list (the
  per-axis count); tap it to fold or unfold the
  group. Each group shows its first six chips (the busiest first) and a
  **Show all (N)** control; **Show fewer** folds it back. A selected chip is
  always shown, even beyond the first six.
- **Counts** — the number on a chip is how many sessions in the list you are
  looking at carry that tag (own, inherited and placement tags together), not
  the daemon-wide inventory count. A tag no listed session carries is left
  out unless you have it selected, so you can always deselect it.
- **The Compute node group is named per node, from the list itself** — its
  entries are built from the sessions you are looking at, not from the tag
  inventory: one entry per node, a local host under its real hostname and a
  federated peer under the peer name it is registered under. A list that
  holds only peers' sessions therefore shows exactly those peers — a peer's
  session carries no `compute:` tag of its own, so an inventory of tag
  values cannot see it — and a local session that recorded the host under an
  alias merges into that host's single entry with the counts summed. There
  is no "local" or "all" entry: the group uses the same names as
  [grouping by node](#the-node-axis-in-the-web-ui).
- **No chips for Automatons, stages and tasks** — the per-Automaton,
  per-stage and per-task system tags (`automaton:`, `stage:`, `task:`,
  `story:`) never get a chip. Reach one Automaton through the **Automaton**
  picker under the chips instead: type in **Find an Automaton** to narrow by
  title or id, then tap a row. Rows show the title first and the id small
  beside it (an Automaton with no title in the registry shows **unnamed**
  with the id small, never the bare id); selected rows come first, and about
  eight rows show until you narrow the search. For stages and tasks use
  [Grouping](#grouping).
- **Any / all and Clear** — the match mode and the clear action sit below the
  picker and work as before.

With the sheet closed, the picked **Status** chips, picked **Backend**
chips and selected tags show together as a compact row of small chips
above the session list, each with a **✕** to remove just that one (an
Automaton shows its title, not its id) and a **Clear all** control that
empties all three groups at once. The row is absent while nothing is
picked in any of the three groups, and a stored selection or an active tag
filter is not changed by any of this: it is how the groups are drawn,
with no new setting, endpoint, MCP tool, CLI command or chat verb.

### The one-line rule on the other pages

The toolbar is an instance of a one-line rule: a page's server select, its
search box (taking the free width) and its compact controls sit on one
non-wrapping line under the page heading. When a page has no page-level
search or filters, or its controls need more room than a phone-width
non-wrapping line allows, the page is left as it is and the reason is
recorded in the code next to the page's header render. The rule was
inspected for the four adjacent pages, and on each of them the fallback
applied — their layouts are unchanged:

- **Alerts** — it has the shared header server select and always-visible
  search/filter controls, but the labelled filter chips, the search input,
  the labelled sort toggle, the icon action buttons and the server select
  together exceed one phone-width non-wrapping line → the page keeps its
  current layout, filter logic and handlers.
- **Automata** — its search and filter controls exist only inside the
  toggle-driven filter bar (a sheet), so there is no always-visible
  page-level line to tighten into one → the filter bar stays
  toggle-driven, its logic unchanged.
- **Observer** — a scrolling settings page with no page-level search or
  filters; the Memory Browser's controls are scoped to that section →
  unchanged.
- **Dashboard** — its stat bar is already one non-wrapping line carrying
  the placement filter and the icon-sized buttons, there is no text search
  and no room for another line → unchanged.

## View modes

The **view mode buttons** in the toolbar line (☰ **List**, ⊞ **Tiles**,
▤ **Compact**) offer three choices — tap the button for the mode you want;
the active button is marked and the choice is remembered on this device.
Each button carries its name as its accessible label (shown on hover, read
out by screen readers, usable from the keyboard).

- **List** is the classic one-card-per-row layout. It stays the default on
  phone-width viewports (narrower than 600 px), where the dense rows read
  better on a small screen. Each card carries the same labelled placement
  facts as a tile — **Runs on** (the real host name, or the peer's name
  when the session is federated) and **Model on** (the compute node serving
  the model for pi and Ollama, **Anthropic cloud** for a Claude session) —
  each line left out when it cannot be named, each with a hover title that
  says what it means.
- **Tiles** is the grid: work tiles that stack left-to-right as well as
  up-down, reflowing with the window width. This is the default on wider
  viewports. Each tile shows:
  - the session's name (falling back to its task, then its short id),
  - a state chip (running / waiting_input / rate_limited / complete /
    failed / killed),
  - `backend · model` (e.g. `claude-code · claude-opus`),
  - **Runs on** — the real name of the host the session runs on (a federated
    session names its peer, never the word “local”), and **Model on** — where the
    model is served, the compute node for pi and Ollama or **Anthropic cloud** for a
    Claude session (the line is left out when it cannot be named). Each fact
    carries a hover title that says what it means,
  - its tags (own + inherited; more than fit collapse into a shared
    overflow chip — on a phone-width screen the tags collapse to a single
    **+N** chip, where N is how many tags are hidden),
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
- **Compact** is the same tile grid as Tiles at a smaller size: the same
  tiles, with the same content (name, state chip, `backend · model`, the Runs on and Model on facts,
  tags, last-activity time and lifecycle bar) and the same **⋯ overflow
  menu**, laid out in the same multi-column grid with smaller tiles, tighter
  padding and smaller type, so more sessions fit on screen. It never turns
  into rows: on resizes it stays a tile grid instead of following the
  viewport width. In **list** mode the sessions stay a row list: the one
  card per row layout, one session per row.

The mode changes how sessions are drawn **with or without a grouping**: with
**Group by** set to none the whole list is drawn in the chosen mode, and with
an axis selected each group body is drawn in it. Tiles and Compact are the
same grid at two sizes; List is the row layout.

Two details worth knowing:

- The choice is **per device** (per browser). It is stored in this browser's local
  storage (`cs_sessions_view_mode`) and does not travel with federation
  peers or other devices. Each of your devices keeps its own mode.
- In **tiles** mode the grid keeps following the viewport width on resizes,
  so rotating a tablet re-lays the tiles out without a reload.

### View mode with no grouping, and the Tree toggle

With **Group by** set to none, **List**, **Tiles** and **Compact** always draw
three different layouts: list rows, a tile grid, and a smaller tile grid.
The **Tree** button (which groups sessions under their parent by lineage) is a
variant of **List** only. Earlier builds kept a saved Tree choice (stored as
`cs_session_tree_view` in this browser) over the mode toggle, and the Tree
button is hidden while the toolbar is collapsed, so a stale Tree choice could
make Tiles and Compact look identical to List with nothing visible to switch
off. That is fixed in two ways:

- A saved Tree choice no longer overrides Tiles or Compact; the tree shows
  only while the mode is **List**.
- Choosing any mode (tapping the List, Tiles or Compact button) switches
  the Tree choice off, so the mode you pick is the layout you get. To see
  the lineage tree again, choose **List** and press **Tree** on the line
  below the toolbar.

This is a browser-side change only: no setting, endpoint, MCP tool, CLI
command or chat verb changed, and the stored `cs_sessions_view_mode` value is
read as before.

### The state and backend filters

The page has no count line above the list. The **Status** and **Backend**
chip groups in the Filters sheet work the way the Tags group works:
each is a set (empty means "all"), several picks match any of the picked
values, a second tap on an active chip removes exactly that chip, and the
group's **All** chip clears the whole group. Picking a historical state
(complete, failed, killed, …) also auto-enables the History toggle so
those sessions actually become visible instead of silently filtering to
nothing.

The choices are remembered per device in local storage: the status set
under `cs_session_state_chips` and the backend set under
`cs_session_backend_sel`. A pick stored the old single-way under
`cs_session_state_chip` by an earlier build is read in as that one chip,
so a device moving to the new build keeps its filter.

### "Show more" paging

Long lists render in pages of 60 sessions by default. When there are more
than a page, a **Show more** button appears under the list with a
`shown/total` counter (e.g. `Show more (60/214)`); each click appends the
next page. The counter resets whenever a filter, a group axis or a saved
view changes, so you never page through a stale list. The flat list keeps
its existing load-more behaviour for the non-paged view.

## Grouping

The **Group by** picker (in the Filters sheet) offers:

```
No grouping │ State │ Node │ Model │ Project │ Tag │ Tag(automation:) │ Tag(compute:) … │ Automaton
```

Each option groups on exactly one thing, and a session with no match for the
chosen option falls in the **ungrouped** group (it sorts last).

The tag entries after `Tag` are built from the prefixes present in your tag
registry (your automation:/stage:/kind: tags plus the placement auto-tags)
plus the placement axes already visible on the loaded sessions (`compute:`,
`peer:`, …), so the picker shows exactly the prefixes you can narrow to —
e.g. `Tag(automation:)`, `Tag(compute:)`, `Tag(peer:)`.

The PWA remembers the grouping you choose on this device. It restores it
after a page reload and keeps it through list refreshes, so the picker never
snaps back to its default. This dropdown is the **one** Group by control
on the Sessions page: the Filters sheet has a single Group by dropdown —
there is no second gear (⚙) dropdown, and the toolbar carries no second
grouping control either (the former toolbar placement control is gone).
Grouping by a placement axis is just one of the
`Tag(<prefix>)` entries here. The choice is saved in this browser's local
storage (not on the daemon). Applying a saved view replaces it, and an older
placement choice saved before this change is picked up once and carried over.

| Axis | What groups | Notes |
|---|---|---|
| `tag` | the first matching tag (own tags + inherited) | A session with several tags lands in exactly one group (the first in the sorted tag list; tags are trimmed and a repeated tag counts once). Sessions with no matching tag collect in the **ungrouped** group. |
| `tag` + prefix | the first tag carrying that prefix | Only tags with the prefix (e.g. `automation:`) form groups; everything else goes to the ungrouped group. This is how you get the "just the automation cut" or "just the compute-node cut". |
| `state` | the session's state | One group per state (running, waiting_input, complete, …) — handy for a quick "what is stuck" scan. |
| `node` | the node that runs the session | Lists sessions from every node, this daemon and federation peers, each under the node that runs it. In the web UI a peer's session lands under the peer's name and every session of this daemon under its real hostname; the header shows the node's name. In the grouped read (REST, MCP, CLI, comm) the key is the session's hostname, and a session with no hostname of its own is keyed under this daemon's hostname because it ran here. The ungrouped group catches only a row with no node name at all. See [the node axis](#the-node-axis-in-the-web-ui). |
| `model` | where the session's model is served | The PWA-only **Model on** fact as a group: pi/Ollama sessions gather under the compute node that serves them (the same name as the tile's Model on line), every Claude session lands under **Anthropic cloud**, and a session whose model cannot be named lands in the ungrouped group. This axis groups the model's home, not the session's host — the **node** axis does that. |
| `project` | the datawatch project the session belongs to | Read in this order: the project of the session's Automaton (its worktree is mapped back to the repository it belongs to and matched against the registered projects); then a `project:` tag on the session (several tags: the first in sorted order); then the registered project of the session's own working folder, mapped the same way, so a session in a worktree lands with the repository's sessions. When an Automaton is known but no project resolves, the group is that Automaton's title, never the worktree folder. The working folder is used only when no project or Automaton is known (trailing slashes are dropped, so `/work/app` and `/work/app/` form one group); a session with none of these lands in the ungrouped group. The same rule applies on REST, MCP, CLI, the comm verb and the PWA. |
| `automaton` | the Automaton the session belongs to | **Story and task nesting**: sessions sit under the Automaton block, and inside it under their story block, with the task sessions nested in their story. In the grouped read (CLI, comm) each block also carries a done/total session tally. Sessions with no Automaton land in the ungrouped group. In the web UI, an Automaton's task sessions that run on a federation peer are included too (see [the node axis](#the-node-axis-in-the-web-ui)). |

Group mechanics, in every mode:

- **Collapse / expand.** Each group header is collapsible: tap it to fold or
  unfold the group. Your fold state is remembered per device and per axis +
  prefix, so a grouping you live in stays the way you left it. The
  **Fold all** button in the toolbar line folds or unfolds every group at
  once (it appears while a grouping with openable groups is active).
- **Count and state mix.** The header shows the group's session count and a
  per-state mix (e.g. `4 (running 2, waiting_input 1, complete 1)`), so you
  can read "how much of this group is alive" without expanding it.
- **Body.** The group's sessions render in the active view mode — list
  cards, tiles or compact tiles — and the body pages with its own **Show
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
- **How the node label is chosen.** For a peer's session the label is the
  peer's name. For this daemon's own sessions it is the session's own
  hostname, else the hostname the page learnt from the daemon's health
  read. The header then shows the node's registered name when the compute
  node list knows that name, id or hostname, and the plain label otherwise.
  Only a row with neither a peer name nor any hostname falls in the
  ungrouped group.
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

Saved views live in the **Views** section of the Filters sheet: a **View
name** input and a **+ New view** button, plus one chip per saved view:

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

The Sessions page controls above: the one-line toolbar (server select,
search, Filters with its badge, Fold all while grouping is active, the List /
Tiles / Compact view mode buttons) and the Tree / History line (with the 🌐 indicator while a federated
server is active), the Filters sheet (Group by with the
tag-prefix entries, Views, status chips, and the grouped, searchable Tags
filter with the Automaton picker and the selected-filter row), Show
more paging, collapsible group headers with the fold-all control and the
**+N** tag chip on phones. All of it is localised in the five locales — the
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
| Search (name, id, backend, tag text, state) | PWA toolbar row | text remembered per device | shows `N of M` while active |
| Server picker (All / this server / each federated peer) | PWA toolbar select (and the header dropdown) | no — page-wide, not per device; defaults to the local server on load | one shared picker: the toolbar select and the header dropdown are the same selection, so choosing an entry in one updates the other; **All** aggregates local + federated; not one of the sheet's filters |
| Filters sheet (Group by, Views, status chips) | PWA Filters button | no | button toggles open/closed, also closes on tap outside, close button or Escape; badge counts active filters |
| View mode (list / tiles / compact) | PWA toolbar row (three icon buttons, the active one marked) | yes (`cs_sessions_view_mode`) | viewport default: list < 600 px, tiles ≥ 600 px; tiles reflow on resize; with no grouping the three modes always differ, and the Tree toggle (`cs_session_tree_view`) applies to List only and is cleared by any mode tap |
| Group by (none / state / node / project / tag / tag+prefix / automaton) | PWA, REST `group_by`+`tag_prefix`, MCP `group_by`+`tag_prefix`, CLI `--group-by`/`--tag-prefix`, comm `sessions groups [axis] [tag-prefix]` | axis choice is remembered on this device in the PWA and restored after a reload and after list refreshes; the read is stateless | unknown axis refused; `tag_prefix` only on the tag axis |
| Collapse a group, fold all | PWA group header, **Fold all** button in the toolbar line | yes (`cs_sessions_collapsed_groups`, keyed by axis + prefix) | Fold all appears only while a grouping with openable groups is active; body still renders in the active mode |
| Show more (page) | PWA, under the list and under each group | no | 60 per page by default, `shown/total` counter, resets on filter change |
| State filter | PWA, Filters sheet | status chip set per device (`cs_session_state_chips`; an older single pick in `cs_session_state_chip` is read in) | multi-select set: union of the picked states, second tap removes one chip, **All** clears the group; historical picks auto-enable History |
| Backend filter | PWA, Filters sheet | backend chip set per device (`cs_session_backend_sel`) | multi-select set: union of the picked backend families, second tap removes one chip, **All** clears the group; leaves the search box untouched |
| Saved views (mode + axis + prefix + filter) | PWA Filters sheet | yes (`cs_sessions_saved_views`) | save / apply / rename / delete; names trimmed and capped |
| Tile ⋯ menu actions | PWA, per tile | no | Stop / Commands (waiting_input) / Restart / Delete / Summary (if summarizer on) / Watch / Mute |
| Progress on tiles; done/total on automaton blocks | PWA tile bar; grouped read (CLI, comm) | no | tile bar is lifecycle-based; automaton blocks carry their done/total terminal-session tally |

## Troubleshooting

- **The toolbar placement dropdown (⚙️) is gone.** There is one Group by
  control, not two: the Filters sheet's **Group by** dropdown. Grouping by
  a placement axis is one of its `Tag(<prefix>)` entries (`Tag(compute:)`,
  `Tag(peer:)`, …); the sheet's and the (former) toolbar control shared one
  saved choice, so nothing about your saved grouping is lost.
- **A session is in the ungrouped group on the tag axis.** It has no tag
  matching the chosen prefix (or none at all). Check the tile's tag chip
  and the inherited tags of its Automaton — a session spawned by an
  Automaton carries that Automaton's tags as inherited, read-only tags, and
  those count for placement. (On the automaton axis the same group is
  labelled "Not part of an Automaton".)
- **The mode buttons seem to do nothing with no grouping.** Fixed: a saved
  Tree choice used to override Tiles and Compact. Reload the PWA on a current
  build and click the mode you want; the click also clears the Tree choice
  (see [View mode with no grouping, and the Tree toggle](#view-mode-with-no-grouping-and-the-tree-toggle)).
  If you still see list rows in Tiles or Compact, confirm the PWA is the
  current build (hard reload) before looking at storage.
- **I see a tile, not a row, and I want rows back.** Choose **List** in the
  view mode buttons — the choice sticks on that device. On phones the default
  is already List.
- **My saved view isn't on my other device.** Saved views are per device
  by design; recreate it from the Views section of the Filters sheet. The *data* it reads (groups,
  counts, mixes) is identical on every channel and every peer.
- **The list looks short and I did not filter.** Check the badge on the
  **Filters** button and the search box: a remembered search or an active
  filter narrows the list (the `N of M` count shows it). Clear the search
  text and tap **All** in the **Status** and **Backend** groups (or
  **Clear all** in the chip row above the list) to see everything.
- **A group says N but I count fewer rows.** The group body is paged —
  expand with its **Show more** (shown/total) step; the header count is
  the full group, not the rendered page.
- **`invalid group_by` from the API/CLI.** The axis must be exactly one of
  `tag`, `state`, `node`, `project`, `automaton`. `model` is a PWA-only axis:
  it groups the **Model on** fact on the web page and is not an API/CLI value.
- **Project groups show a folder path instead of a project name.** The session has no `project:` placement tag and no owning Automaton project, so it falls back to its working folder. Tag the session with a `project:` tag to group it by project.

## Still to review

- **The view mode buttons (this fix).** The toolbar's view control is the
  three icon buttons shown above — a `role=group` of three `sessions-view-btn`
  buttons labelled **List** / **Tiles** / **Compact** (`_sessionsViewSelectHTML`
  in `internal/server/web/app.js`), client-side PWA behaviour, confirmed by
  code and pinned by
  `app-sessions-one-line-toolbar.test.js`, `app-sessions-view-modes.test.js`
  and `app-sessions-filters-sheet-layout.test.js`. Confirm on the build in
  use: the toolbar line ends in three compact icon buttons (not a dropdown),
  the mode in use is marked on the active one, tapping the other button
  switches the list immediately and sticks on that device, the line still
  fits at 360 px without wrapping, and on a touch screen a held button shows
  its name as a hint without changing the mode. The stored
  `cs_sessions_view_mode` value and its meaning are unchanged.
- **The Filters button's close paths (this fix).** Client-side PWA
  behaviour (`sessionsFiltersToggle` / `sessionsFiltersClose` in
  `internal/server/web/app.js`), confirmed by code and pinned by the PWA
  tests `app-sessions-filters-toggle.test.js` and
  `app-sessions-filters-sheet.test.js`. Confirm on the build in use: one
  click on the **Filters** button opens the sheet, the same button closes
  it again, and the sheet also closes on the close button, a tap on the
  backdrop or anywhere outside, or **Escape** — none of these touches the
  picks you made or the badge count, and a refresh with the sheet open
  keeps it open.

## Related guides

- [`sessions-deep-dive.md`](sessions-deep-dive.md) — session anatomy,
  lifecycle and resume.
- [`automata-tags.md`](automata-tags.md) — the tag tree, assignment and
  bulk tagging behind the tag axis.
- [`automation-lane-fill.md`](automation-lane-fill.md) — the lane fill
  automation that produces the lane/compute placement tags.
- [`list-endpoints-and-paging.md`](list-endpoints-and-paging.md) — why
  list endpoints are small and how paging works.
