# [sync] Projects page: Gantt width cap, Gantt above Unassigned, collapsible section cards

**Filed:** https://github.com/dmz006/datawatch-app/issues/468 (datawatch v9.156.1).

## What changed in datawatch

Layout fixes on the Projects page of the web page. No endpoint, field or setting changed.

- The project Gantt is capped at 720 px wide and left-aligned, so its text stays at the authored size on a wide screen.
- The Gantt is drawn above the Unassigned Automata list.
- Each section card on the project page (policy, merge pipeline, event stream, unassigned and the rest) has a collapse and expand toggle in its header. The state is per card and is not stored on the daemon.

## New locale keys (five locales)

| Key | English | Use |
|---|---|---|
| `projects_section_collapse` | Collapse %1$s | label of the toggle while the card is open; `%1$s` is the card's heading |
| `projects_section_expand` | Expand %1$s | label of the toggle while the card is collapsed |

## What the mobile app needs

- Collapsible section cards on its project page, with the two labels above in all five locales.
- Gantt above the Unassigned Automata, with a sensible maximum width on tablets.
