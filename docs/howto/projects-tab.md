---
docs:
  index: true
  topics: [projects, project-record, arcs, priority, state, target, lanes, weekly-share, retention, progress, history]
exec_steps:
  - tool: project_list
    description: List the registered projects. Each entry carries its directory, its default backend and its stored policy. Read-only
    args: {}
    read_only: true
  - tool: project_rollup
    description: Read one project's roll-up. Arcs in order with their Automata, task progress, cost in points and throughput over the window. Read-only
    args: {name: core-api, window: 7}
    read_only: true
  - tool: project_history
    description: Read one project's history, newest first, one page at a time. Finished and archived Automata, daily progress and Gantt rows. Read-only
    args: {name: core-api, limit: 20}
    read_only: true
  - tool: project_policy_effective
    description: Read the effective policy of one project and the tier that supplies each setting. Read-only
    args: {name: core-api}
    read_only: true
  - tool: project_upsert
    description: Set the record fields of a project. Pass the whole record you want to keep; an absent record field keeps its stored value. Owner, repository and default branch are set here with the target, lane defaults and weekly share
    args: {name: core-api, dir: /srv/projects/core-api, owner: platform-team, target: {name: "Core API 2.0", definition_of_done: "All v1 endpoints served by v2 and the v1 routes removed"}, lane_defaults: {lane: gpu, backend: claude-code, model: qwen3.8:27b}, weekly_share: 40, branch_retention_days: 14}
  - tool: project_set_arcs
    description: Replace the ordered arc list of a project. The array order is the arc order; each arc has an automation id, a priority and a state (planned, active or done). Sending an empty list clears the arcs
    args: {name: core-api, arcs: [{automation_id: a1b2c3d4, priority: 1, state: active}, {automation_id: e5f6a7b8, priority: 2, state: planned}]}
---

# How-to: the Projects sub-tab

The **Projects** sub-tab is where you look at one project as a whole. It gathers the
project's arcs (the ordered Automations the project works through), the Automata
and sessions under each arc, progress in points, a Gantt of recent work, the
finished history, the policy and merge pipeline that apply to it, and the events
that belong to it. Use it to answer "where does this project stand, what is next,
and what did it cost" without opening each Automation.

Projects is the first sub-tab of the **Automata** view, to the left of
**Automation**. The sub-tab order is **Projects**, **Automation**, **Automata**,
**Events**, **Templates**. It is not a tab of its own in the bottom navigation.

The sub-tab is read-only. It shows what the daemon holds and links to the Settings
entries where you change things. The project record itself (owner, target, arcs,
lane defaults, weekly share, branch retention, major trigger) is changed from the
CLI, REST, MCP and the comm channel, as shown below.

Three words are used in this guide, and only these three:

- **Automaton**: one unit of autonomous work.
- **Automata**: more than one Automaton.
- **Automation**: a named chain of Automata, run in order. An arc *is* an Automation.

```
Project  "core-api"
 ├─ Arc 1  priority 1  active   ── Automation a1b2c3d4 ── Automata ── sessions
 ├─ Arc 2  priority 2  planned  ── Automation e5f6a7b8 ── Automata ── sessions
 └─ Unassigned Automata (of this project, held by no arc)
```

## Base requirements

- A running daemon you can reach from the web app, the CLI, MCP or a comm channel.
- The Automata features enabled. The Projects sub-tab reads the autonomous store, so
  with Automata off the roll-up and history report "Automations are not
  available" (HTTP 503 on the API).
- At least one project registered as a **project alias**: a name, an absolute
  directory and, optionally, a default backend. Sessions and Automata refer to a
  project by its alias.
- Read access for the reads (the autonomous read capability for a federated
  peer), and write access for every change below.

## Setup

1. **Register the project.** Give it a name and an absolute directory. The alias
   is what every surface uses afterwards.

   ```bash
   datawatch projects upsert core-api --dir /srv/projects/core-api --backend claude-code --description "Public REST API"
   ```

   > `projects upsert` replaces the project's policy tier with the policy it is
   > sent, which is none. Use it to create the alias. To change one record field
   > later, use `datawatch projects set` or `datawatch projects arcs`, which keep
   > the stored policy. If the project has a policy you want to keep, set it with
   > the policy verbs in [project-automation-policy](project-automation-policy.md)
   > after you upsert.

2. **Open the sub-tab.** In the web app choose **Automata** in the bottom
   navigation, then **Projects**, the first sub-tab on the left. The old
   Projects link and the `#projects` address open the same sub-tab. The
   **Project** picker at the top lists **Overview** and every registered
   project. Pick `core-api`.

3. **Fill in the record.** Set the owner, the target and the lane defaults, then
   the arcs. Each step is shown in the walkthrough.

## Walkthrough: from an empty project to a working page

This walkthrough uses the CLI. Each step has the matching REST, MCP and comm
form under [All the options](#all-the-options).

### 1. Read the project record

```bash
datawatch projects get core-api
```

You see the directory, the default backend, the stored policy, the record fields
that are set and the arcs. Nothing is written.

### 2. Set the record fields

Only the flags you pass change. Everything else keeps its stored value.

```bash
datawatch projects set core-api --owner platform-team --repository example-org/core-api --default-branch main
datawatch projects set core-api --target "Core API 2.0"
datawatch projects set core-api --weekly-share 40 --branch-retention-days 14 --major-trigger milestone
```

Expected output: the daemon's JSON answer `{"status":"ok","name":"core-api"}`.

`--target` sets the target **name**. The **definition of done** and the **lane
defaults** have no CLI flag yet; set them with REST or MCP (step 5).

### 3. Replace the arc list

An arc is an Automation in the project's order. `--set` takes `id:priority:state`
entries, comma-separated. The state is `planned`, `active` or `done`; an empty
state means `planned`. The lower priority runs first on the page.

```bash
datawatch projects arcs core-api --set a1b2c3d4:1:active,e5f6a7b8:2:planned
datawatch projects arcs core-api
```

The first command replaces the whole list; it does not add to it. The second
prints the stored arcs in order. Send `--set` with an empty value to clear them.

### 4. Read the roll-up

```bash
datawatch projects show core-api
```

`show` prints the record followed by the roll-up: arcs in order, the Automata
under each arc with their task progress, cost in points and throughput over the
last seven days. Ask for another window through REST or MCP (`window`, 1 to 365
days).

Costs are **points** from the billing profile, never API dollars. A project with
no priced Automaton shows the note that the backends are not on a bundled plan.

### 5. Set the target, the definition of done and the lane defaults

These three live on the record and are written with one REST or MCP call. The
body keeps every other field you do not send.

```bash
curl -s -X POST http://localhost:8080/api/projects -H 'Content-Type: application/json' -d '{
  "name": "core-api",
  "dir": "/srv/projects/core-api",
  "target": {"name": "Core API 2.0", "definition_of_done": "All v1 endpoints served by v2 and the v1 routes removed"},
  "lane_defaults": {"lane": "gpu", "backend": "claude-code", "model": "qwen3.8:27b"}
}'
```

`lane_defaults.lane` takes the same values as an Automaton's lane preference:
`gpu`, `claude` or `any`. The backend and model are the defaults new work in the
project starts with.

### 6. Open the page in the web app

Choose **Automata**, then the **Projects** sub-tab, then `core-api`. From the top:

| Block | What it shows |
|---|---|
| Header | Description, directory, default backend, owner, repository, default branch, target name, definition of done, lane defaults, weekly share |
| Progress | Tasks and Automata done, cost in points, throughput over the last seven days |
| Arcs | Each arc with its priority, Automation id, state (Planned, Active, Done), a task bar and its points. Click an arc to open the Automata and sessions under it. An **active** arc opens by default |
| Unassigned Automata | Automata of this project that no arc holds |
| Gantt | One row per Automaton and per task, over the last 7, 30 or 90 days. Running work reads "now" |
| History | Finished and archived Automata, daily progress snapshots, newest first, with **Load more** |
| Effective policy | Read-only summary of the project's policy, with a link to the policy panel in Settings |
| Merge pipeline | Read-only, with a link to the merge pipeline settings |
| Events | The event stream filtered to this project. It is hidden while it has nothing to show, and it lists notes for empty sections (no arcs, no unassigned Automata) and a missing weekly share, but only when the project has an arc or an unassigned Automaton that needs one |

A row links to its Automaton view, and a session link opens the session. The
**Overview** choice in the picker is the cross-project in-flight view, not a
project.

#### The show-empty checkbox

**Show empty lines and sections**, the checkbox next to the project picker, is
off by default. While it is off the page draws only what has content: empty
header fields (an unset owner, repository, target or lane default), the
"none recorded" lines under an arc (plan documents, design documents, issues),
an empty Arcs or Unassigned Automata section, an empty merge pipeline and an
empty event stream are left out. Tick it to see every line and section, with
"none recorded" or "no arcs" shown where there is nothing yet. The choice is
remembered per viewer in the browser, not by the daemon, so it has no REST,
MCP, CLI, chat or configuration form, and a browser that blocks storage starts
hidden each time. Nothing is lost while it is off: a section that needs
attention still appears, and a missing weekly share is reported in the page's
Events list whenever the project has an arc or an unassigned Automaton that
needs one.

### 7. Read the history and the Gantt

```bash
datawatch projects history core-api --limit 20
```

Each page carries `next_cursor` when there is more. Pass it back with
`--cursor` to read the next page. The Gantt rows cover every matching Automaton
and are not paged.

### 8. Move work through the arcs

When an arc starts, mark it active; when it is finished, mark it done. Replacing
the list is the only way to change the state, so send the whole list:

```bash
datawatch projects arcs core-api --set a1b2c3d4:1:done,e5f6a7b8:2:active,c9d0e1f2:3:planned
```

## Reading the fields

| Field | Where it is set | What it means | What the daemon does with it |
|---|---|---|---|
| `owner` | CLI `set --owner`, REST, MCP | Who owns the project | Shown on the page and by `show` |
| `repository` | CLI `set --repository`, REST, MCP | Source repository, for example `example-org/core-api` | Shown on the page |
| `default_branch` | CLI `set --default-branch`, REST, MCP | Default git branch | Shown on the page |
| `target.name` | CLI `set --target`, REST, MCP | The outcome the project works toward | Shown on the page |
| `target.definition_of_done` | REST, MCP | When the target counts as reached | Shown on the page |
| `lane_defaults` | REST, MCP | Lane, backend and model new work starts with | Shown on the page; not yet used to place new Automata |
| `arcs` | `projects arcs`, REST `PUT .../arcs`, MCP `project_set_arcs` | The ordered Automations. Array position is the order; `priority` and `state` are stored as sent | Drives the order of the arcs on the page and the roll-up |
| `weekly_share` | CLI `set --weekly-share`, REST, MCP | Percent of the weekly usage budget the project should get (0 to 100) | **Stored and shown only.** No scheduler reads it yet |
| `branch_retention_days` | CLI `set --branch-retention-days`, REST, MCP | Days to keep finished Automaton branches | **Stored and shown only.** The worktree cleanup uses its own retention setting; see [automaton-worktree-cleanup](automaton-worktree-cleanup.md) |
| `major_trigger` | CLI `set --major-trigger`, REST, MCP | `manual`, `milestone` or `never` | **Stored and shown only.** The release decision reads the merge pipeline's major trigger, set per project with the merge pipeline settings; see [merge-pipeline](merge-pipeline.md) |

Set the weekly share and retention only as a record of intent until the matching
behaviour ships; the page will show the value you set.

### The `project` field on Automata and Automations

Automaton and Automation reads (REST list and detail) carry a read-only `project`
field. It is the registered project alias the record belongs to: the explicit
project profile when it names a registered alias, otherwise the longest registered
alias directory that contains the record's project directory. It is worked out on
every read from the aliases you register with `projects upsert`; nothing is stored,
so registering or removing an alias changes the field on the next read. When no
alias matches, the field is left out. See
[Derived project alias](../datawatch-definitions.md#derived-project-alias).

### Arc states

| State | Meaning on the page |
|---|---|
| `planned` | Not started. Shown first in the order, closed by default |
| `active` | In progress. Opened by default |
| `done` | Finished. Its Automata stay visible under it |

An arc with no Automata yet shows "Arc has no Automata yet" in Events.

### Defaults and inheritance

Every record field is optional. An absent field keeps what is stored; an empty
string clears a text field. The daemon-wide defaults for weekly share, retention
and major trigger live under `projects_defaults` in the configuration:

```bash
datawatch config set projects_defaults.branch_retention_days 14
datawatch config set projects_defaults.weekly_share_percent 25
datawatch config set projects_defaults.major_trigger manual
```

They are used when a project sets none of its own. Like the record values, they
are stored; see the note above.

## Precedence of the project policy

The Projects sub-tab shows the **effective policy** of the project. Each setting
resolves from the top down: the Automaton's own setting, then the project's
policy, then the daemon default. Read it for one Automaton with the
`automaton` argument, which adds that Automaton as the top tier. The full
reference is [project-automation-policy](project-automation-policy.md).

## All the options

### Web app

| Action | Where |
|---|---|
| Open the sub-tab | Bottom navigation **Automata**, then the **Projects** sub-tab (first, left of **Automation**); the old Projects link and `#projects` land here |
| Pick a project | **Project** picker at the top (the same Overview picker as before); **Overview** is the cross-project view |
| Open a project directly | Address `#projects/<name>`; the last project you opened is remembered in this browser |
| Change the window of the Gantt | Gantt header: 7, 30 or 90 days |
| Page through history | **Load more** at the foot of History |
| Change policy | **Settings → Automation → Project policy** (link on the page) |
| Change the merge pipeline | **Settings → Automation → Merge pipeline** (link on the page) |

The page has no edit control for the record. Arcs, target, lane defaults and the
numeric record fields are changed with the surfaces below.

### REST

| Method and path | Purpose |
|---|---|
| `GET /api/projects` | List every project alias with its record |
| `GET /api/projects/{name}` | One project's record |
| `POST /api/projects` | Create or update an alias; absent record fields keep their stored value; invalid values return 400 and nothing is saved |
| `PUT /api/projects/{name}/arcs` | Replace the ordered arcs (a bare array or `{"arcs": [...]}`); 404 for an unknown project |
| `GET /api/projects/{name}/rollup?window=7` | Roll-up: arcs in order, Automata, tasks, points, throughput; `window` 1 to 365 days |
| `GET /api/projects/{name}/history?limit=50&cursor=…` | History page and Gantt rows |
| `GET /api/projects/{name}/policy/effective?automaton=<id>` | Effective policy with the tier of each setting |
| `DELETE /api/projects/{name}` | Remove the alias; the directory is not touched |

Example, replace the arcs:

```bash
curl -s -X PUT http://localhost:8080/api/projects/core-api/arcs -H 'Content-Type: application/json' -d '[
  {"automation_id":"a1b2c3d4","priority":1,"state":"active"},
  {"automation_id":"e5f6a7b8","priority":2,"state":"planned"}
]'
```

Example, read the roll-up over 30 days:

```bash
curl -s "http://localhost:8080/api/projects/core-api/rollup?window=30"
```

Writes need the autonomous write capability and reads need the autonomous read
capability; a federated peer is checked against the same two.

### MCP

| Tool | Parameters | Purpose |
|---|---|---|
| `project_list` | none | List the aliases and their records |
| `project_upsert` | `name`, `dir` (required); `default_backend`, `description`, `policy`, `owner`, `repository`, `default_branch`, `arcs`, `target`, `lane_defaults`, `weekly_share`, `branch_retention_days`, `major_trigger` | Create or update; omit a field to keep it |
| `project_set_arcs` | `name`, `arcs` (required) | Replace the arcs; `[]` clears them |
| `project_rollup` | `name`; `window` | The roll-up |
| `project_history` | `name`; `limit`, `cursor` | One history page |
| `project_policy_effective` | `name`; `automaton` | Effective policy with tiers |
| `project_alias_delete` | `name` | Remove the alias |

Example call, set the arcs:

```json
{
  "tool": "project_set_arcs",
  "arguments": {
    "name": "core-api",
    "arcs": [
      {"automation_id": "a1b2c3d4", "priority": 1, "state": "active"},
      {"automation_id": "e5f6a7b8", "priority": 2, "state": "planned"}
    ]
  }
}
```

### CLI

| Command | Purpose |
|---|---|
| `datawatch projects list` | List the aliases |
| `datawatch projects get <name>` | One alias and its record |
| `datawatch projects upsert <name> --dir <abs> [--backend …] [--description …]` | Create or update an alias (replaces the policy; see the note in Setup) |
| `datawatch projects show <name> [--window N]` | Record plus roll-up |
| `datawatch projects set <name> [--owner …] [--repository …] [--default-branch …] [--target …] [--weekly-share N] [--branch-retention-days N] [--major-trigger …]` | Change only the flags you pass |
| `datawatch projects arcs <name>` | Print the arcs in order |
| `datawatch projects arcs <name> --set id:priority:state,…` | Replace the arcs; `--set ""` clears them |
| `datawatch projects history <name> [--limit N] [--cursor …]` | History page |
| `datawatch projects policy …` | Policy verbs; see [project-automation-policy](project-automation-policy.md) |
| `datawatch projects delete <name>` | Remove the alias; the directory is not touched |

Target definition of done and lane defaults are not CLI flags; use REST or MCP.

### Comm channel

Send these from Signal, Telegram or another connected channel:

| Command | Purpose |
|---|---|
| `project show <name>` | Record fields, one per line; `-` for unset |
| `project arcs <name>` | Arcs in order |
| `project progress <name>` | Roll-up: tasks, points and throughput per arc |
| `project set <name> <field> <value>` | Set one field: `owner`, `repository`, `default_branch`, `description`, `default_backend`, `major_trigger`, `weekly_share`, `branch_retention_days` |
| `project policy <name> [automaton]` | Effective policy (see [project-automation-policy](project-automation-policy.md)) |

The comm channel does not replace arcs, set the target, the definition of done or
the lane defaults. Use REST, MCP or the CLI for those.

## Troubleshooting

| Symptom | Cause and fix |
|---|---|
| The Projects sub-tab says "No projects yet" | No alias is registered. Run `datawatch projects upsert` (step 1) |
| Roll-up shows "Could not load the roll-up" | The autonomous store is off or the alias is unknown. Check the daemon with `datawatch projects get <name>` |
| An arc shows no Automata | Its Automation has no member Automata yet, or the Automata are of another project |
| `arcs` returns 400 "automation_id is required" or "duplicate automation_id" | Each arc needs a unique id; an arc whose state is not `planned`, `active` or `done` is refused |
| `set --weekly-share 150` is refused | Weekly share must be 0 to 100 |
| `set --branch-retention-days -1` is refused | Retention cannot be negative |
| Policy disappeared after an `upsert` | `upsert` writes the policy it is sent, which is none; set the policy again with `datawatch projects policy set`, or use `set` and `arcs` for later changes |
| Plan documents, design documents or issues show "none recorded" | The arc record has no fields for them yet, so the page cannot show them. The arc section shows what the record holds |

## Limits in this release

- The record's weekly share, branch retention and major trigger are kept and
  shown. The scheduler, the worktree cleanup and the release decision do not read
  the project's own values yet.
- Lane defaults are recorded for new work but do not yet decide where an Automaton
  runs.
- The page is read-only. Changes go through REST, MCP, the CLI or the comm
  channel.

## For the operator to review

- **Mobile apps.** The Android and iPhone apps still show Projects as their own
  screen. Moving it under Automata there is tracked in datawatch-app and is not
  part of this change.
- **Old links keep working.** The `#projects` and `#projects/<name>` addresses
  and the Projects link open the sub-tab, so bookmarks and the Settings help
  links stay valid. This page keeps its file name (`projects-tab.md`) for the
  same reason; rename it only if you also redirect the links that point here.
- **Plan records.** The registry rows and findings of `docs/plans/` now have a
  record store and a read-only importer, but no REST, MCP, CLI, comm or web app
  surface yet, so this page does not list them. Decide which surface comes first
  and where a records view belongs before it is built; see
  [operations.md](../operations.md#plan-records-store-and-import).
- **Sub-tab order.** Projects, Automation, Automata, Events, Templates is the
  order shipped. Confirm it is the order you want before the next release.

## Related

- [automation-tab.md](automation-tab.md): the Automation view, where the
  Automations of a project are listed and run
- [project-automation-policy.md](project-automation-policy.md): the project tier
  of the policy and its precedence
- [merge-pipeline.md](merge-pipeline.md): the merge pipeline and the major release
  trigger the release decision reads
- [automaton-worktree-cleanup.md](automaton-worktree-cleanup.md): the finished
  branch cleanup
- [automata-progress.md](automata-progress.md): progress across all projects
- [cost-report.md](cost-report.md): spend per Automaton, Automation and backend
- [../datawatch-definitions.md](../datawatch-definitions.md#projects-sub-tab): the
  glossary entries for the terms on this page
