# [sync] Projects tab added to the PWA: picker, project page, roll-up, history, Gantt, policy and events (datawatch next release)

Parent release: datawatch next release

## What changed in datawatch

The PWA gains a top-level **Projects** tab in the bottom navigation. The page has a **Manage projects** block to add, edit and remove a project's name, directory, description and default backend; the rest of the record is changed through REST, MCP, the CLI or the comm channel.

- **Projects picker.** The tab opens with a **Project** picker fed by `GET /api/projects`. **Overview** is the first choice and is the cross-project in-flight view. An empty list shows "No projects yet. Add your first project with the form below." with the **Manage projects** form open; there is no **Open Settings** button in the empty state.
- **Project page.** Choosing a project (or opening `#projects/<name>`) shows the project header: owner, repository, default branch, target, definition of done, lane defaults, weekly share and priority. The last project opened is remembered in the browser.
- **Arcs, Automata and sessions.** Arcs are listed in their stored order with their state (planned, active, done). Each arc shows its Automata and their sessions, and sessions link to the existing session view. Automata not in any arc sit under **Unassigned Automata**.
- **Progress roll-up.** Per-arc and whole-project progress bars, cost in points, and throughput over a window of days. Cost in points is marked "Not priced" when the backends are not on a bundled plan.
- **History and Gantt.** **History** lists finished Automata and tasks with progress snapshots and pages with **Load more**. The **Gantt** draws Automata and tasks over a window of 7, 30 or 90 days; the newest rows are drawn first when the window is bounded.
- **Effective policy and merge pipeline.** The page shows the effective project policy, with links to **Settings → Automata → Project policy** and the **Merge pipeline** group in **Settings → Automata**. Each link switches Settings to the Automata tab, expands the section if it is collapsed, and scrolls to it.
- **Manage projects.** A collapsible block under the picker: a list with **Edit** and **Remove** per project and one form. **Add project** posts the new record to `POST /api/projects`; **Edit** loads the listed record, makes the name read-only and posts it back whole with only directory, description and default backend changed, so policy and arcs are kept; **Remove** asks for confirmation, then calls `DELETE /api/projects/{name}` and removes only the record (no session is stopped). Name and directory are required. Errors from the daemon are shown above the form. Removing the viewed project returns the picker to Overview.
- **Events.** A read-only stream of events filtered to the project, with a note of the queue it reads and a notice when earlier events were dropped before they were read.
- **Project defaults in Settings.** The Settings automation area gains a **Project defaults** card for `projects_defaults`: branch retention days, weekly share percent and major release trigger. These are stored daemon-wide defaults, used when a project sets none of its own.

## REST endpoints and fields

The PWA reads these endpoints. Writes go through the same endpoints from other surfaces.

| Method and path | Used by the page for |
|---|---|
| `GET /api/projects` | The picker and the Overview list: every project alias with its record |
| `GET /api/projects/{name}` | The project header and record fields |
| `POST /api/projects` | Create or update an alias (absent fields keep their stored value; invalid values return 400 and nothing is saved) |
| `PUT /api/projects/{name}/arcs` | Replace the ordered arcs; accepts a bare array or `{"arcs": [...]}`; 404 for an unknown project |
| `GET /api/projects/{name}/arcs` | The arc list with Automata and sessions |
| `GET /api/projects/{name}/rollup?window=7` | Progress, cost in points and throughput; `window` 1 to 365 days |
| `GET /api/projects/{name}/history?limit=50&cursor=…` | History page and Gantt rows |
| `GET /api/projects/{name}/policy/effective?automaton=<id>` | Effective policy, with the tier of each setting |
| `DELETE /api/projects/{name}` | Removes the alias only; the directory is not touched |

Record fields on the project entry:

| Field | Meaning | Daemon behaviour |
|---|---|---|
| `owner`, `repository`, `default_branch` | Ownership and source | Shown on the page |
| `target.name`, `target.definition_of_done` | The outcome and when it counts as reached | Shown on the page |
| `lane_defaults` | Lane, backend and model new work starts with | Shown; not yet used to place new Automata |
| `arcs` | Ordered Automations; array position is the order; `priority` and `state` (`planned`, `active`, `done`) stored as sent | Drives arc order and the roll-up |
| `weekly_share` (0 to 100) | Share of the weekly usage budget | Stored and shown only; no scheduler reads it yet |
| `branch_retention_days` | Days to keep finished Automaton branches | Stored and shown only |
| `major_trigger` (`manual`, `milestone`, `never`) | Major release trigger | Stored and shown only; the merge pipeline's setting decides |

Daemon-wide defaults live under `projects_defaults` in the configuration: `branch_retention_days`, `weekly_share_percent` and `major_trigger`.

Access: reads need the autonomous read capability and writes need the autonomous write capability; a federated peer is checked against the same two.

## Locale keys

The Projects tab uses 67 `projects_*` keys, plus the three `settings_projects_defaults_*` labels and help texts for the Project defaults card. All five bundles (EN, DE, ES, FR, JA) carry the same key set with real translations, not English copies, and matching positional placeholders (`%1$s`, `%2$s`). Representative keys:

| Key | EN |
|---|---|
| `projects_overview` | Overview |
| `projects_picker_label` | Project |
| `projects_none` | No projects yet. Add your first project with the form below. |
| `projects_manage_title` | Manage projects |
| `projects_manage_add_title` / `projects_manage_edit_title` | Add project / Edit project %1$s |
| `projects_manage_col_name`, `_col_dir`, `_col_backend`, `_col_description` | Table column headings |
| `projects_manage_field_name`, `_field_dir`, `_field_description`, `_field_backend` | Form field labels |
| `projects_manage_add_action`, `_save_action`, `_cancel_action`, `_edit_action`, `_remove_action` | Button labels |
| `projects_manage_confirm_remove` | Remove project %1$s? Only the project record is removed. Running sessions and Automaton work are not stopped. |
| `projects_manage_err_name`, `_err_dir` | Enter a project name. / Enter the project directory. |
| `projects_manage_saved`, `_removed` | Project %1$s saved. / Project %1$s removed. |
| `projects_page_arcs` | Arcs |
| `projects_rollup_heading` | Progress |
| `projects_history_heading` | History |
| `projects_gantt_heading` | Gantt |
| `projects_links_policy_heading` | Effective policy |
| `projects_links_events_heading` | Events |
| `settings_projects_defaults` | Project defaults |

Where the keys are used: `internal/server/web/app.js` (the picker, the project page, roll-up, history, Gantt, policy links and events), `internal/server/web/index.html` (the nav button `navBtnProjects`), and `internal/server/web/locales/{en,de,es,fr,ja}.json`. `internal/server/web/app-projects-locales.test.js` asserts the key sets match across bundles, that values are translated and not copied from English, that no English-only placeholder syntax survives, and that every `projects_` key `app.js` names exists in English.

## Acceptance

Android and iOS should match the PWA on these points. The daemon contract above is the source for each.

- A **Projects** tab sits in the main navigation. Its first choice is **Overview**; the other choices are the projects returned by `GET /api/projects`.
- An empty project list shows the empty state with a route to the project defaults in Settings.
- The project page shows the header fields from the record, with absent values omitted rather than shown as blank.
- Arcs appear in stored order with their state; Automata not in any arc appear under **Unassigned Automata**; each session opens its session view.
- The roll-up shows per-arc and whole-project progress, cost in points (or "Not priced" when no bundled plan applies) and throughput over the window; `window` is sent as 1 to 365 days.
- History pages with **Load more** using the returned cursor and ends when no cursor comes back. The Gantt offers 7, 30 and 90 day windows and marks running rows as "now".
- The effective policy lists each setting with its tier, and the two links open the matching Settings sections.
- The events list is read-only and filtered to the project; a dropped-events notice is shown when the daemon reports it.
- All five locales are present; no screen shows an untranslated English string in DE, ES, FR or JA.
- Reads never change the record. Only the **Manage projects** block writes, and only the directory, description and default backend; an edit keeps the stored policy and arcs.
- Add, edit and remove each show a result line or the daemon's error; a removed project leaves the picker and its sessions keep running.

## Filing record

Filed: pending (this document is the parity issue body for the Projects tab; file it with the datawatch-app tracker when the release is cut).
