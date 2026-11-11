---
docs:
  index: true
  topics: [list, tiles, compact, view mode, group by, grouping, tag, state, node, project, collapse, show more, sessions, automata, automation, placement, local storage]
---

# List, Tiles and Compact modes, and grouping

The long lists in the web UI can be drawn three ways and grouped. This how-to
covers the **Automata** list, the **Automation** list and the **Sessions**
list: what each control does, how the choice is remembered, and what is the
same or different between the three.

## Pick a view mode (Automata and Automation lists)

Above the flat Automata list and the flat Automation list is a **View mode**
toggle with three buttons:

| Mode | What you get |
|------|--------------|
| **List** | One line per item. The densest view; the default on a narrow phone. |
| **Tiles** | One card per item with the full detail. The default on a wide window. |
| **Compact** | Smaller cards, several to a row. The default on a mid-width window. |

Until you press a button, the mode follows the window width, and it keeps
following it when you resize (wide: Tiles, mid: Compact, narrow: List). Once
you press a button, that choice wins on this device and the window width no
longer changes it.

The Sessions list has no mode toggle; it keeps its own card layout.

## Group the list

Next to the toggle is a **Group by** select with these choices:

| Choice | Groups items by |
|--------|-----------------|
| **No grouping** | Nothing; one flat list (the default). |
| **tag** | Each tag an item carries; an item with several tags appears under each. |
| **state** | The item's status (running, blocked, done, failed and so on). |
| **node** | The compute node it runs on. |
| **project** | The project folder it works in. |
| **automation:**, **stage:**, **kind:**, **compute:**, **peer:**, **backend:**, **lane:**, **role:**, **project:** | The value of the tag that starts with that prefix, such as `stage:build`. |

Items that have no value on the chosen axis go under a group named **No tag**
(for tag and prefix choices) or **None** (for state, node and project). Tag
names come from the tag registry; see [Automata tags](automata-tags.md) and
[Automatic tags](automata-auto-tags.md) for where the `automation:`,
`stage:` and `kind:` tags come from.

Each group has a header that shows:

- a **collapse toggle** (▾ open, ▸ collapsed) with the group name,
- the **count** of items in the group,
- the **State mix**, such as how many are running or failed.

Click the toggle to collapse a group down to its header. The collapsed groups
are remembered per list, so they stay collapsed after a reload.

## Long groups: Show more

A group draws its first 50 items. If more are left, a **Show more** button
under the group adds the next 50 and tells you how many are left. Changing
the **Group by** choice starts every group back at 50.

## Sessions: group by placement

The Sessions list groups by **placement tag** instead: use **Group by
placement** to pick an axis, and the sessions fall under the tag they carry on
that axis (their own system tags, plus tags inherited from the Automaton that
started them). Sessions with no tag on that axis fall under **No <axis> tag**;
that header is shown only when the list also has tagged groups.

Sessions use the same group header, collapse toggle, count, state mix and
**Show more** paging as the other two lists. Each tagged group also has an
**Only this tag** button that filters the Sessions list to that tag; the
button becomes **Show all sessions** while the filter is on.

## Where the choice is kept

The view mode, the group-by choice and the collapsed groups are stored in your
browser, per list and per device. They are not stored on the daemon, so:

- they do not follow you to another browser or phone,
- clearing the browser's site data resets them to the defaults,
- there is no setting, API, command-line or MCP form of them, and no
  messaging-channel command.

## Troubleshooting

- **A group I expect is missing** — an item appears only under values it
  actually has. Check the item's tags, and clear any tag filter above the
  list.
- **Everything is under "No tag"** — the items carry no tag of that axis; try
  **state** or **project**, or check the tag prefix.
- **Back to the defaults** — set **Group by** to **No grouping** and clear
  the browser's site data for the daemon address to forget the view mode.

## Related

- [Automata tags](automata-tags.md)
- [Automation view](automation-tab.md)
- [How the Automata tab loads](automata-tab-loading.md)
