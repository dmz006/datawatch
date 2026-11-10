# [sync] Projects tab added to the PWA: picker, project page, arcs, roll-up, history, Gantt and project settings (datawatch next release)

Parent release: datawatch next release

## What changed in datawatch

The PWA gains a top-level **Projects** tab in the bottom navigation. It shows one project as a whole: its arcs (the ordered Automations the project works through), the Automata and sessions under each arc, progress in points, a Gantt of recent work, the finished history, the effective policy and merge pipeline links, and the events that belong to the project. The tab is read-only. Changes to the project record go through REST, MCP, the CLI or the comm channel.

- **Projects tab (new).** A **Project** picker at the top of the tab lists **Overview** and every registered project from `GET /api/projects`. **Overview** is the cross-project in-flight view; choosing a project opens its page. The last project chosen is remembered in the browser and mirrored in the address (`#projects` or `#projects/<name>`).
- **Project page header.** Description, directory, default backend, owner, repository, default branch, target name, definition of done, lane defaults and weekly share.
- **Progress card.** Tasks and Automata done, cost in points (or a note that the backends are not on a bundled plan), and throughput over the last seven days.
- **Arcs.** Each arc in order with its priority, Automation id, state (Planned, Active, Done), a task bar and its points. Clicking an arc opens the Automata and sessions under it; an **active** arc opens by default.
- **Unassigned Automata.** Automata of the project that no arc holds.
- **Gantt.** One row per Automaton and per task over the last 7, 30 or 90 days; running work reads "now". The window is chosen in the Gantt header.
- **History.** Finished and archived Automata and daily progress snapshots, newest first, one page at a time with **Load more**.
- **Effective policy and merge pipeline.** Read-only summaries, each with a link to its Settings panel (project automation policy; merge pipeline settings).
- **Events.** The event stream filtered to this project, with a note for each section that is empty or unavailable.
- **Settings → Project defaults (new).** Three daemon-wide defaults for projects that set none of their own: branch retention days, weekly share percent and the major release trigger. The Settings entries write the `projects_defaults` configuration block.
- **No change to the Automation view.** The Automata view keeps its own sub-tabs; the Projects tab only reads the same daemon data.

## REST endpoints the PWA calls

| Method and path | Used for | Fields the PWA reads |
|---|---|---|
| `GET /api/projects` | Picker list and the record of each project | `name`, `dir`, `default_backend`, `description`, `owner`, `repository`, `default_branch`, `target` (`name`, `definition_of_done`), `lane_defaults` (`lane`, `backend`, `model`), `weekly_share`, `branch_retention_days`, `major_trigger`, `arcs` |
| `GET /api/projects/{name}/rollup` | Progress card and arc list | Arcs in order (`automation_id`, `priority`, `state`, task progress, points), Automata under each arc, unassigned Automata, throughput over the window |
| `GET /api/projects/{name}/history?limit=50&cursor=…` | History list and Gantt rows | Items (finished and archived Automata, daily snapshots), `next_cursor` for the next page |
| `GET /api/projects/{name}/policy/effective` | Effective policy summary on the page | Effective values and the tier that supplies each one |
| `GET /api/autonomous/merge-pipeline?project={name}` | Merge pipeline summary on the page | The project's merge pipeline record |
| `GET /api/autonomous/events?queue=pwa&project={name}&limit=…` | Events section on the page | Events for the project, and a `dropped` flag when earlier events were lost before they were read |

Read capability is required for every call above. The PWA does not write to any of these endpoints: the write surfaces (`POST /api/projects`, `PUT /api/projects/{name}/arcs`, `DELETE /api/projects/{name}`) exist for the CLI, MCP and comm channel and are not called from the PWA page.

The Settings entries for the project defaults write through the existing configuration path, using the keys `projects_defaults.branch_retention_days`, `projects_defaults.weekly_share_percent` and `projects_defaults.major_trigger`.

## New locale keys

The Projects tab adds about 75 keys per bundle. The full list is in `internal/server/web/locales/en.json`; the groups are below. Each key exists in EN, DE, ES, FR and JA. The DE, ES, FR and JA bundles carry translations, not English placeholders.

| Group | Example keys | Count (approx.) |
|---|---|---|
| Navigation and picker | `nav_projects`, `projects_overview`, `projects_picker_label`, `projects_none`, `projects_none_settings_action`, `projects_load_failed` | 6 |
| Record fields | `projects_field_dir`, `projects_field_backend`, `projects_field_description`, `projects_page_owner`, `projects_page_repository`, `projects_page_default_branch`, `projects_page_target`, `projects_page_done_when`, `projects_page_lane_defaults`, `projects_page_weekly_share`, `projects_page_arcs`, `projects_page_priority` | 12 |
| Arc states and page sections | `projects_page_arc_state_planned`, `projects_page_arc_state_active`, `projects_page_arc_state_done`, `projects_page_tasks`, `projects_page_points`, `projects_page_unassigned`, `projects_page_loading`, `projects_page_rollup_failed`, `projects_page_events` | 9 |
| Links: policy, merge pipeline, events, plans | `projects_links_policy_heading`, `projects_links_policy_settings`, `projects_links_policy_empty`, `projects_links_merge_heading`, `projects_links_merge_settings`, `projects_links_events_heading`, `projects_links_events_note`, `projects_links_events_empty`, `projects_links_events_dropped`, `projects_links_arc_plans`, `projects_links_arc_designs`, `projects_links_arc_issues`, `projects_links_none`, `projects_links_loading`, `projects_links_failed` | 15 |
| Events text | `projects_event_unset`, `projects_event_no_arcs`, `projects_event_arc_empty`, `projects_event_no_unassigned`, `projects_event_automaton_no_sessions` | 5 |
| Progress card | `projects_rollup_heading`, `projects_rollup_whole_project`, `projects_rollup_points_label`, `projects_rollup_points_unpriced`, `projects_rollup_throughput_label`, `projects_rollup_throughput_value` | 6 |
| History | `projects_history_heading`, `projects_history_loading`, `projects_history_failed`, `projects_history_empty`, `projects_history_load_more`, `projects_history_snapshot` | 6 |
| Gantt | `projects_gantt_heading`, `projects_gantt_window_label`, `projects_gantt_window_7d`, `projects_gantt_window_30d`, `projects_gantt_window_90d`, `projects_gantt_loading`, `projects_gantt_empty`, `projects_gantt_bounded`, `projects_gantt_running` | 9 |
| Settings: project defaults | `settings_projects_defaults`, `settings_projects_defaults_branch_retention_days` (and `_help`), `settings_projects_defaults_weekly_share_percent` (and `_help`), `settings_projects_defaults_major_trigger` (and `_help`) | 7 |

## Where the keys are used

- `internal/server/web/index.html`: the **Projects** navigation button (`nav_projects` for its label and title).
- `internal/server/web/app.js`: the Projects view, the picker (`projects_overview`, `projects_picker_label`), the page sections (the `projects_page_*`, `projects_rollup_*`, `projects_history_*` and `projects_gantt_*` keys through `t(...)`), the links and events section (`projects_links_*`, `projects_event_*`), and the Settings entries (`labelKey` values `settings_projects_defaults_*`).
- `internal/server/web/locales/{en,de,es,fr,ja}.json`: one entry per key in each of the five bundles.
- `internal/server/web/app-projects-locales.test.js`: asserts the same `projects_*` key set in every bundle, real (non-placeholder) translations, matching positional placeholders, and that every key the app names literally exists in English.
- `internal/server/web/app-projects-nav.test.js`, `app-projects-links.test.js`, `app-projects-gantt.test.js`: cover the navigation, the links and events section, and the Gantt.

## Acceptance

Acceptance points for the datawatch-app (Android and iOS). These describe what a parity build must show; the daemon contract above is the source of truth.

**Android and iOS**

- The app has a **Projects** entry in its primary navigation, with the same place in the navigation the PWA gives it.
- The Projects screen opens with a **Project** picker listing **Overview** and every project returned by `GET /api/projects`. An empty list shows the "no projects yet" text with an action that opens the app's Settings.
- Choosing a project shows its header (description, directory, default backend, owner, repository, default branch, target, definition of done, lane defaults, weekly share).
- The progress section shows tasks and Automata done, cost in points, and throughput over the last seven days. When an Automaton is not priced, it shows the "not priced" note rather than a dollar amount.
- Arcs are listed in `priority` order with their state (Planned, Active, Done). Tapping an arc shows its Automata and sessions. An active arc is open by default.
- Automata that no arc holds appear under **Unassigned Automata**.
- The Gantt has 7, 30 and 90 day windows. Running work reads "now". An empty window shows the empty text.
- History lists finished and archived Automata and daily snapshots, newest first, and loads the next page from `next_cursor` through **Load more**.
- The policy and merge pipeline sections are read-only and link to their settings screens. The events section shows the project's events and the "some earlier events were dropped" note when `dropped` is set.
- Every label comes from the locale keys in the five bundles (EN, DE, ES, FR, JA).
- Read failures show the section's error text in place of the section; they do not blank the whole screen.
- The app does not offer editing of arcs, the target or the record on this screen. Those stay in the CLI, REST, MCP and comm channel.
- Settings shows the three project defaults (branch retention days, weekly share percent, major release trigger) with their help text, and writes them to the `projects_defaults` keys.
- The Projects screen needs no daemon field beyond those listed under REST endpoints above.

## Filing record

Not yet filed. The app-side issue is to be filed from this document when the operator asks for it.
