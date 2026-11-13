# Parity: Plans replace Arcs, plan records and the ordered Plans browser (datawatch v9.172.0)

Parent release: datawatch v9.172.0

## What changed in datawatch

Arcs are renamed Plans on every person-facing surface. A plan is a record of its own kind: it links down to Automations, Automata and findings and carries a goal and a priority order. A finding attaches to a plan or to an Automaton. An Automaton or Automation already linked to a different plan is refused with its holder named, never moved. Plans can be reordered, and so can the Automata inside a plan; a plan with no Automaton is the backlog. A migration builds plans from the project arcs and the registry rows. The Projects page gained a plans browser, a Records sub-tab and an overview summary, all read from paged routes. `GET /api/projects/{name}/arcs`, the roll-up `arcs` field, `datawatch projects arcs`, the `project_set_arcs` tool and the chat verb `arcs` are deprecated and keep returning the same data, with a `plans` list beside it.

## New routes

| Route | Purpose |
|---|---|
| `GET /api/projects/{name}/plans` | Plans in order with goal, state, progress, points, Automation ids and a next marker, plus a Backlog group. Paged with `limit` and `offset`. |
| `GET /api/projects/{name}/overview-summary` | Header, progress, one line per active plan, what is next, record counts. |
| `GET /api/plans/{ref}/automata` | A plan's Automata in run order: completed, running, next, then blocked and held (shown, skipped by the next marker). |
| `GET /api/plans/{ref}/tasks` | Tasks of an expanded plan, paged. |
| `GET /api/plan-records/browse` | All registry rows and findings, with search, kind and state filters and links to plans and Automata. |
| `POST /api/plans` | Create a plan. |
| `PUT /api/plans/order` | Reorder plans. |
| `PUT /api/plans/{ref}/automata/order` | Reorder the Automata in a plan. |
| `POST /api/plans/{ref}/attach` and `/detach` | Link or unlink an Automaton, an Automation or a finding (kind `automaton`, `automation` or `finding`). |
| `POST /api/plans/migrate` | Preview (default) or apply (`apply`) the plans built from arcs and registry rows. |

The same controls exist as the MCP tools `plan_create`, `plan_reorder`, `plan_reorder_automata`, `plan_attach`, `plan_detach` and `plan_migrate`, the CLI `datawatch plans list|create|reorder|reorder-automata|attach|detach|migrate`, and the chat verbs `autonomous plan list|new|move|attach|detach` and `autonomous plan-migrate [apply]`.

## What the mobile app needs

- Rename Arcs to Plans in the Projects screens and use the plans routes instead of the deprecated arcs route.
- A plans browser on the project page: plans in priority order with goal, state, progress and points; up and down reorder; an expanded plan showing its Automations, its Automata in run order with the next marker and reorder, a Tasks section loaded one page at a time, and the linked findings and registry rows; a Backlog group; search over plan, Automaton, registry row and finding titles; a state filter; create plan; attach an Automaton, an Automation or a finding. Which plans are open is remembered on the device.
- A Records sub-tab with search, kind and state filters, paging and links to plans and Automata; an Overview summary (progress, active plans, next, record counts).
- New web strings in all five locales: `plans_browser_*`, `automation_plans_browser_*`, `records_tab_*`, `projects_overview_*`, `projects_subtab_plans`, `projects_subtab_records`, `projects_page_plans`, `projects_page_loading_plans`, `projects_event_no_plans`, `projects_event_plan_empty`, `projects_gantt_no_plan`, `dash_arc_*` plan wording and `plan_records_title`.
