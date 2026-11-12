# feat(i18n): add projects_subtab_* keys for Projects page sub-tabs parity (datawatch v9.157.0)

**Filed:** https://github.com/dmz006/datawatch-app/issues/469 (datawatch v9.157.0).

Parent release: datawatch v9.157.0

## What changed in datawatch

The project page on the Projects view (Automata, Projects) is split into sub-tabs. A strip under the project name shows Overview, Arcs, Gantt, Unassigned, History, Policy and Events, and only one body is drawn at a time:

| Sub-tab | Blocks |
|---|---|
| Overview | Header fields, Progress |
| Arcs | Arcs |
| Gantt | Gantt (window selector, width cap kept) |
| Unassigned | Unassigned Automata |
| History | History with Load more |
| Policy | Effective policy, Merge pipeline |
| Events | Notes, the project's event stream |

Events is in the strip only while the project has notes. A sub-tab that holds one block shows it without a card; the sub-tabs with several blocks keep the collapsible cards. The choice is remembered per device, and the web address `#projects/<name>/<subtab>` opens a sub-tab directly (none opens Overview).

## API contract

No endpoint, field or setting changed. The page reads the same project, roll-up, history, policy, merge pipeline and event stream replies as before.

## New strings (English values)

| Key | English |
|---|---|
| `projects_subtab_overview` | Overview |
| `projects_subtab_arcs` | Arcs |
| `projects_subtab_gantt` | Gantt |
| `projects_subtab_unassigned` | Unassigned |
| `projects_subtab_history` | History |
| `projects_subtab_policy` | Policy |
| `projects_subtab_events` | Events |
| `projects_subtabs_label` | Project sections |

Changed value: `projects_show_empty_help` now reads "Empty fields, the none-recorded lines and the empty event stream are hidden by default. This shows them on every sub-tab."

## What the mobile app needs

- The same sub-tab strip on the project page (scrolls sideways on a phone), the same grouping of blocks, the remembered choice, and the Events sub-tab hidden while there are no notes. All five locales.
