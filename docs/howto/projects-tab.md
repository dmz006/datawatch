---
docs:
  index: true
  topics: [projects, project-record, arcs, priority, state, target, lanes, weekly-share, retention, progress, history, timeline, project-link, project-membership, automata-without-a-project, backfill, set-project, project-source]
exec_steps:
  - tool: project_list
    description: List the registered projects. Each entry carries its directory, its default backend and its stored policy. Read-only
    args: {}
    read_only: true
  - tool: project_rollup
    description: Read one project's roll-up. Plans in order with their Automata, task progress, cost in points and throughput over the window. Read-only
    args: {name: core-api, window: 7}
    read_only: true
  - tool: project_history
    description: Read one project's history, newest first, one page at a time. Finished and archived Automata, daily progress and Gantt rows. Read-only
    args: {name: core-api, limit: 20}
    read_only: true
  - tool: project_timeline
    description: Read when each plan, Automaton and task of one project started and finished, one level at a time (plans by default). Work still running has no last time. Read-only
    args: {name: core-api, level: arcs}
    read_only: true
  - tool: project_policy_effective
    description: Read the effective policy of one project and the tier that supplies each setting. Read-only
    args: {name: core-api}
    read_only: true
  - tool: project_upsert
    description: Set the record fields of a project. Pass the whole record you want to keep; an absent record field keeps its stored value. Owner, repository and default branch are set here with the target, lane defaults and weekly share
    args: {name: core-api, dir: /srv/projects/core-api, owner: platform-team, target: {name: "Core API 2.0", definition_of_done: "All v1 endpoints served by v2 and the v1 routes removed"}, lane_defaults: {lane: gpu, backend: claude-code, model: qwen3.8:27b}, weekly_share: 40, branch_retention_days: 14}
  - tool: project_set_arcs
    description: Deprecated old name and plan list (arcs); the registry plan records replace it (see plan_migrate). Replace the ordered plan list of a project. The array order is the plan order; each plan has an automation id, a priority and a state (planned, active or done). Sending an empty list clears the plans
    args: {name: core-api, arcs: [{automation_id: a1b2c3d4, priority: 1, state: active}, {automation_id: e5f6a7b8, priority: 2, state: planned}]}
  - tool: autonomous_prd_set_project
    description: Set or clear the explicit project link of an Automaton. The project must be a configured project name; an empty project clears the link. Only the Automaton record changes and the change is written to its decision log
    args: {id: a1b2c3d4, project: core-api}
  - tool: autonomous_project_link_backfill
    description: Report which Automata with no project link belong to the datawatch project. The default is a dry run that writes nothing; pass apply true to write the links
    args: {apply: false}
    read_only: true
---

# How-to: the Projects sub-tab

The **Projects** sub-tab is where you look at one project as a whole. It gathers the
project's plans (the ordered Automations the project works through), the Automata
and sessions under each plan, progress in points, an expandable Gantt timeline with a focus view per Automation, the
finished history, the policy and merge pipeline that apply to it, and the events
that belong to it. Use it to answer "where does this project stand, what is next,
and what did it cost" without opening each Automation.

Projects is the first sub-tab of the **Automata** view, to the left of
**Automation**. The sub-tab order is **Projects**, **Automation**, **Automata**,
**Events**, **Templates**. It is not a tab of its own in the bottom navigation.

Once you pick a project, its page is split into its own strip of **page
sub-tabs** (labelled **Project sections** for screen readers), and only one body
shows at a time: **Overview**, **Plans**, **Records**, **Gantt**, **Unassigned**,
**History**, **Policy** and, when there is something to show, **Events**. They are the
sub-tabs of one project's page, not of the Automata view; this guide says "page
sub-tab" for them and "sub-tab" for **Projects** itself. See
[The page sub-tabs](#the-page-sub-tabs).

The sub-tab shows what the daemon holds and links to the Settings entries where
you change things. It also has a **Manage projects** block where you add, edit and
remove a project (its name, directory, description and default backend) without
leaving the page; see [Manage projects in the web app](#manage-projects-in-the-web-app).
The rest of the project record (owner, target, plans, lane defaults, weekly share,
branch retention, major trigger) is changed from the CLI, REST, MCP and the comm
channel, as shown below.

Four words are used in this guide for work, and only these four:

- **Automaton**: one unit of autonomous work.
- **Automata**: more than one Automaton.
- **Automation**: a named chain of Automata, run in order.
- **Plan**: the registry record of what you are working on. A plan links down:
  plan, then Automation, then Automata, then tasks. A finding attaches to a plan
  or to an Automaton. The word *record* is used only for what is stored (the
  registry rows and findings) and for the **Records** sub-tab. The older project
  plan list (arcs) is deprecated in favor of plan records: `datawatch plans migrate`
  previews plans built from that list and the registry rows, and `--apply` writes
  them; the older routes keep answering meanwhile. See
  [plan-records.md](plan-records.md#building-plans-from-arcs-and-rows).

```
Project  "core-api"
 ├─ Plan 1 priority 1  active   ── Automation a1b2c3d4 ── Automata ── tasks ── sessions
 ├─ Plan 2 priority 2  planned  ── Automation e5f6a7b8 ── Automata ── tasks ── sessions
 └─ Unassigned Automata (of this project, held by no plan)
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
   is what every surface uses afterwards. In the web app you can do this with the
   **Manage projects** form on the Projects sub-tab (see
   [Manage projects in the web app](#manage-projects-in-the-web-app)); the CLI form is:

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
   project. Pick `core-api`. On a phone the Automata sub-tab bar scrolls
   sideways; the header title reads **Projects** in its short form so it stays on
   one line (see
   [Tab strips at phone width](navigation-mobile-fit.md#tab-strips-at-phone-width)).
   The Inbox count follows this picker too: with a project picked it counts only
   that project's items, and under **Overview** it counts all projects (see
   [What the Inbox counts](automation-tab.md#what-the-inbox-counts)).

3. **Fill in the record.** Set the owner, the target and the lane defaults, then
   the plans. Each step is shown in the walkthrough.

## Manage projects in the web app

**Manage projects** is a collapsible block on the Projects sub-tab, under the
project picker. It is open when there are no projects, while you edit one and when
an error is showing; otherwise it is collapsed.

| To do this | Do this |
|---|---|
| Add a project | Fill in **Name**, **Directory (absolute path)** and, if you want, **Description** and **Default backend**, then choose **Add project**. Name and directory are required; the page says "Enter a project name." or "Enter the project directory." when one is missing |
| Edit a project | Choose **Edit** on its row. The form switches to "Edit project <name>", with the name read-only. Change the directory, description or default backend and choose **Save changes**, or **Cancel** |
| Remove a project | Choose **Remove** on its row and confirm. Only the project record is removed. Running sessions and Automaton work are not stopped, and the directory is not touched |

After a save or a remove the list and the picker reload and the page shows
"Project <name> saved." or "Project <name> removed." If the daemon refuses the
change, its message is shown above the form and nothing is changed. Removing the
project you are viewing returns the picker to **Overview**.

Editing sends the project's whole record back with only these three fields
changed, so its policy, plans, owner, target and other record fields keep their
stored values. The block is the web form of `POST /api/projects` and
`DELETE /api/projects/{name}`; the same actions are on the CLI, MCP and comm
channel below.

With no projects the picker area reads "No projects yet. Add your first project
with the form below." and the form is open, so setup starts there.

## Walkthrough: from an empty project to a working page

This walkthrough uses the CLI. Each step has the matching REST, MCP and comm
form under [All the options](#all-the-options).

### 1. Read the project record

```bash
datawatch projects get core-api
```

You see the directory, the default backend, the stored policy, the record fields
that are set and the plans. Nothing is written.

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

### 3. Replace the plan list

> Deprecated: this older plan list (arcs) is replaced by the registry plan records. Run `datawatch plans migrate` to preview plans built from it; see [plan-records.md](plan-records.md#building-plans-from-arcs-and-rows). This step keeps working until you retire the older plan list.

A plan is an Automation in the project's order. `--set` takes `id:priority:state`
entries, comma-separated. The state is `planned`, `active` or `done`; an empty
state means `planned`. The lower priority runs first on the page.

```bash
datawatch projects arcs core-api --set a1b2c3d4:1:active,e5f6a7b8:2:planned
datawatch projects arcs core-api
```

The first command replaces the whole list; it does not add to it. The second
prints the stored plans in order. Send `--set` with an empty value to clear them.

### 4. Read the roll-up

```bash
datawatch projects show core-api
```

`show` prints the record followed by the roll-up: plans in order, the Automata
under each plan with their task progress, cost in points and throughput over the
last seven days. Ask for another window through REST or MCP (`window`, 1 to 365
days).

Throughput counts an Automaton as finished when it is completed, when it is
cancelled or archived and its work branch is already on the project's default
branch, or when it carries a shipped record. Its finish time is the merge or ship
time when known, else its last update. A task counts from its own completion time
in the window, whatever the Automaton's final status, and never twice.

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

Choose **Automata**, then the **Projects** sub-tab, then `core-api`. The project
name and a strip of page sub-tabs sit at the top of the page; the blocks below
are spread across them, one page sub-tab at a time.

| Page sub-tab | Blocks on it | What they show |
|---|---|---|
| Overview | Header, Progress | Description, directory, default backend, owner, repository, default branch, target name, definition of done, lane defaults, weekly share; then tasks and Automata done, cost in points and throughput over the last seven days. A summary shows the active plans with their state and progress, what is next, and counts of plans, registry rows and findings (the counts link to the Plans and Records sub-tabs) |
| Plans | Plans browser, then the roll-up plans | **Plans browser:** plans in priority order, each with its goal, state, a progress bar of finished Automata and its points. The **up** and **down** buttons on a plan row move it and save the new order. Click a plan to open it: its Automations, then its Automata in run order (finished first by completion time, then running, next, blocked and held; the **Next** marker skips blocked and held Automata) with their own up and down buttons, a **Tasks** section that reads tasks only when you open it and has **Load more**, and the findings and registry rows linked to the plan with their old row numbers. A **Backlog** group after the plans holds plans with no Automaton yet, in old row number order. The search box looks through plan, Automaton, registry row and finding titles, the state filter narrows by plan state, and reordering is turned off while either is in use. Under each open plan, attach an Automaton, an Automation or a finding by id (a finding can also be picked from a list); a link held by another plan is refused with the holder named. **Create plan** at the top adds a plan with a title and a goal. Which plans and Tasks sections are open is remembered in this browser only. **Roll-up plans:** each plan with its priority, Automation id, state (Planned, Active, Done), a task bar and its points. Click a plan to open the Automata and sessions under it. An **active** plan opens by default |
| Records | Records browser, Registry and findings | Every registry row and finding of the project with the plan and Automata each links to. A search box, a kind filter and **Load more** narrow and page the list; a plan link opens the Plans sub-tab with that plan expanded. "Record" here names the stored registry rows and findings only; the work itself is always a plan, an Automation or an Automaton |
| Gantt | Gantt | One expandable timeline. It opens with one row per plan and a bar from the plan's first to its last task, coloured by its status (Completed, Running, Waiting, Blocked, Failed or Cancelled); work still running is drawn to now and reads "now". Hover or focus a bar for its status, dates and, for a shipped Automaton, its release. Click a row's arrow to open the Automata under the plan, and an Automaton's arrow to open its tasks; a plan with no activity yet reads **Planned** with no bar. Automata that no plan holds sit in the **Automata with no plan** group at the foot. The range selector offers **All time** (the default), 90, 30 and 7 days; **All time** fits the rows that are showing, expanded Automata and tasks included, with a 2 percent margin at each end, and the mouse wheel zooms and a drag pans the chart. Rows you open later are ordered by completion, then running and waiting work by start, with a task or Automaton below the ones it depends on. **Edges** draws thin arrows between the rows that are showing: from the end of a task to the start of each task that waits for it, and from an Automaton to the Automata that follow it. Hover a bar, or tab to it, to light its own edges and dim the rest. **Edges** turns the arrows off, and **All edges** (shown while they are on) lifts the cap of 120 drawn edges, beyond which only the edges of the hovered bar show. The choice is remembered in this browser only. Each plan row has a **Focus** button (see [Focus on one Automation](#focus-on-one-automation)). It reads the project timeline (see [The project timeline](#the-project-timeline)) one level at a time, so children load only when you expand a row. It draws at most 40 rows and says "Showing 40 of N rows." when there are more, and a level with more children than one page has **Load more**. On a wide browser it is capped at 720 pixels wide, left-aligned; on a phone the chart keeps a minimum width inside a sideways-scrolling frame and the label column stays visible |
| Unassigned | Unassigned Automata, Automata without a project | Automata of this project that no plan holds. Below them, Automata with no project link and no directory match are listed once in a separate `without_project` group, the same whichever project you open and not counted in any total (see [Which Automata belong to a project](#which-automata-belong-to-a-project)), with a hint to set a link from the Automaton detail |
| History | History | Finished and archived Automata, daily progress snapshots, newest first, with **Load more** |
| Policy | Effective policy, Merge pipeline | Read-only summary of the project's policy, with an **Open settings** link that opens Settings on the Automata tab at the Project policy section; then the read-only merge pipeline, with an **Open settings** link at the Merge pipeline group |
| Events | Notes, event stream | Notes for empty sections (no plans, no unassigned Automata), an Automaton with no sessions, a failed roll-up and a missing weekly share, with the event stream filtered to this project below them. This page sub-tab is not in the strip while the project has no notes |

A page sub-tab that holds one block (Plans, Gantt, Unassigned, History) shows
that block directly, with no card around it. A page sub-tab that holds more than
one block (Progress on Overview, Effective policy and Merge pipeline on Policy,
Notes and the event stream on Events) draws those blocks as collapsible cards
with a toggle in each header. Whether such a card is open or closed is
remembered per section in this browser, not by the daemon, so it differs between
browsers, and a browser that blocks storage starts with the defaults each time.
Collapsing only changes what is shown. On a wide browser the Gantt is capped at
720 pixels wide, left-aligned, so its text keeps its authored size instead of
growing with the window; on a phone it is unchanged. The cards cannot be hidden
or reordered.

**For the operator to review (timeline):** the operator decision of 2026-11-12 (rebuild
the Gantt as one expandable timeline with a focus view per Automation) is now in the
web app. Which rows are expanded is remembered per project in this browser's local
storage (key `cs_projects_gantt_expanded_<project>`), like the section state below, and has
no REST, MCP, CLI, chat or configuration form; a browser that blocks storage shows
everything collapsed. The old fixed 7, 30 and 90 day window and the history Gantt rows in
the page are gone (the range selector now defaults to **All time**); `GET /api/projects/{name}/history` still returns its `gantt` rows
for other clients. The mobile apps are not covered by this change and need a parity
check (see `docs/plans/parity-issues/`).

**For the operator to review:** the remembered open or closed state uses the
browser's local storage and has no REST, MCP, CLI, chat or configuration form.
The mobile apps are not covered by this change and need a parity check.

A row links to its Automaton view, and a session link opens the session. The
**Overview** choice in the project picker is the cross-project in-flight view,
not a project, and it has no project page sub-tabs. It has its own strip of
in-flight sub-tabs instead (see [The Overview sub-tabs](#the-overview-sub-tabs)).

#### The Overview sub-tabs

With **Overview** chosen in the picker, the page shows the cross-project in-flight
sub-tabs of the Automation view (Inbox, Automations, Resources, Progress and cost,
and Housekeeping) in a strip under the picker. The strip uses the
same look as the project page sub-tabs (the strip labelled Project sections), not
the look of the Automation view's own second row, and every sub-tab in it responds
to a click or tap.

- **Choosing one shows its body.** The sub-tab you choose is marked selected and its
  body replaces the previous one; the picker stays above the strip.
- **The Inbox badge stays live.** The count on the Inbox sub-tab is updated in place
  and still counts every project's items under Overview (see
  [What the Inbox counts](automation-tab.md#what-the-inbox-counts)).
- **Remembered.** The choice is kept in the same browser value as the project page
  sub-tabs (`cs_projects_subtab`) and is restored when you open Overview again,
  including after a refresh. A remembered value that is not an Overview sub-tab is
  ignored, and a browser that blocks storage opens the default sub-tab each time.
- **Not addressable.** There is no `#projects/<subtab>` address for an Overview
  sub-tab; the address stays `#projects`.
- **No daemon side.** It is a display rule of the web app: no setting, and no REST,
  MCP, CLI, chat or configuration form.

#### The page sub-tabs

- **Eight in a fixed order.** Overview, Plans, Records, Gantt, Unassigned, History,
  Policy and Events, left to right.
- **One body at a time.** Choose a page sub-tab to show its body; the others are
  not drawn. The strip scrolls sideways on a phone, so every sub-tab stays
  reachable on a narrow screen.
- **Picker and checkbox stay put.** The **Project** picker and the **Show empty
  lines and sections** checkbox sit above the strip and do not change when you
  switch page sub-tabs.
- **Events is hidden while empty.** The Events sub-tab is not in the strip while
  the project has nothing to show there.
- **Policy is read-only.** It shows the effective policy and the merge pipeline,
  with **Open settings** links into Settings where you change them.
- **The old Projects address opens Overview.** The old Projects link and the
  `#projects` address land on the Projects sub-tab with **Overview** chosen in the
  picker.
- **Remembered.** The page sub-tab you last chose is remembered per viewer in the
  browser (`cs_projects_subtab`), not by the daemon, so it has no REST, MCP, CLI,
  chat or configuration form. It applies to whichever project you open next, and a
  browser that blocks storage opens **Overview** each time.
- **Addressable.** The address of a page sub-tab is `#projects/<name>/<subtab>`,
  where `<subtab>` is `plans` (the old `arcs` still opens it), `records`, `gantt`, `unassigned`, `history`, `policy` or
  `events`. `#projects/<name>` opens **Overview**, and so does an address that
  names an unknown page sub-tab. Opening `#projects` with no project name keeps
  the remembered page sub-tab. Choosing a page sub-tab rewrites the address in
  place without adding a browser history entry.
- **Events falls back.** If the remembered or addressed page sub-tab is **Events**
  and the project has no notes, the page opens **Overview** instead.
- **Overview first.** The first thing a project page shows is the header and the
  progress, as before; the blocks that used to follow them on the same page are
  now one choice away.

#### The show-empty checkbox

**Show empty lines and sections**, the checkbox next to the project picker, is
off by default. While it is off the page draws only what has content: empty
header fields (an unset owner, repository, target or lane default), the
"none recorded" lines under a plan (plan documents, design documents, issues),
an empty Plans or Unassigned Automata section, an empty merge pipeline and an
empty event stream are left out. Tick it to see every line and section, with
"none recorded" or "no plans" shown where there is nothing yet. The choice is
remembered per viewer in the browser, not by the daemon, so it has no REST,
MCP, CLI, chat or configuration form, and a browser that blocks storage starts
hidden each time. Nothing is lost while it is off: a section that needs
attention still appears, and a missing weekly share is reported in the page's
Events list whenever the project has a plan or an unassigned Automaton that
needs one.

### 7. Read the history and the Gantt

```bash
datawatch projects history core-api --limit 20
```

Each page carries `next_cursor` when there is more. Pass it back with
`--cursor` to read the next page. The Gantt rows cover every matching Automaton
and are not paged. The web app no longer draws them; it draws the project
timeline below.

#### The project timeline

The timeline read answers "when did each plan, Automaton and task of this project
start and finish" without fetching every task up front. It returns one level at a
time. Ask for the plans first, then ask only for the rows you open:

```bash
datawatch projects timeline core-api                                  # arcs
datawatch projects timeline core-api --level automata --parent <arc-id>
datawatch projects timeline core-api --level tasks --automaton <automaton-id>
datawatch projects timeline core-api --focus <arc-id>                 # one arc, everything under it
```

- **Plans** (the default): one row per plan, spanning the earliest start to the
  latest finish of its Automata and their tasks. A row of kind `unassigned`
  holds Automata that no plan holds (matched by project directory only as a
  fallback). `--parent none` lists those Automata.
- **Automata**: the Automata under one plan. **Tasks**: the tasks of one
  Automaton.
- **Focus**: one plan's Automata and all their tasks in a single reply. It is not
  paged.
- Each row has `first` and `last`. Work still running has `last` empty and
  `running` true, so a client draws it to now. A plan with no Automata or no
  recorded times has both empty. `has_children` and `children_count` say
  whether a row can be opened.
- Each row also has `start` (the same as `first`), `end` (empty while running),
  `status` (completed, running, waiting, blocked, failed or cancelled; a shipped
  Automaton reads completed and its release is in `release`) and, on task rows,
  `depends_on`. An Automaton spans its created time to its completion time; a
  task spans its start to its completion; a plan spans the earliest Automaton
  start to the latest Automaton end. Rows inside a plan are ordered by
  completion, then adjusted so a dependency sits above its dependent. Automata-level and
  focus replies also carry `edges`, a list of `{from, to, kind: "chain"}` taken
  from the Automation's member order and its on-complete, on-blocked and
  on-failed links, drawn through fork and join members.
- Levels other than focus are paged: `--limit` (1 to 200, default 50) and
  `--cursor` with the `next_cursor` of the previous page.
- Templates are left out. Reading needs the autonomous read capability, and the
  answer is 503 while Automata are disabled.

The timeline is a pure read. It has no configuration key and stores nothing. It
never signals, stops or ends a session or an Automaton.

**Which Automata belong to a plan, and to the project.** The explicit project link
on the Automaton comes first. Matching by project directory is only a fallback,
used when an Automaton carries no explicit link. An Automaton that no plan holds
sits in the `unassigned` group.

The same read is available over REST, MCP and chat:

```bash
curl -s "$DATAWATCH/api/projects/core-api/timeline?level=arcs&limit=50"   # REST
# MCP: project_timeline {name: core-api, level: arcs}
# Chat: project timeline core-api level=arcs
```

Example chat reply (a running plan reads `now`):

```
core-api timeline (arcs, 2 of 2)
arc   Core API 2.0     2026-10-02 09:14 → now      active
arc   Docs refresh     2026-10-20 11:02 → 2026-11-03 16:40   done
```

REST and MCP return the same rows as JSON (`rows`, with `has_more` and
`next_cursor` when there is another page).

#### Focus on one Automation

**Focus** opens one Automation's own timeline on a page of its own: every Automaton
under it with every task, in one read of the timeline with `focus=<id>`, so nothing
is capped or paged. The range runs from the first task to the last, each end
extended by the same 2 percent margin the Gantt's **All time** uses; the header
reads "First task … – last task …" with the two ends of that displayed range —
the range ends, not the exact start and end of the first and last task, so each
can read up to a day earlier or later than the oldest or newest task's own date
depending on the range's width and your local calendar day. It has the same
presets, wheel zoom and drag pan as the Gantt, the same bars, hover highlight
and **Edges** toggle, and each Automaton can be collapsed with the state
remembered. An
Automation with nothing started reads "This Automation has no Automata or tasks yet.
It is planned." and is not an error. **Back** returns to the project page, or to the
Automation page when you came from its header. From the Automation page the Focus
button looks the Automation up in each project in turn and skips projects that do not
hold it. The page only reads; it stores nothing and sends no write.

### 8. Move work through the plans

When a plan starts, mark it active; when it is finished, mark it done. Replacing
the list is the only way to change the state, so send the whole list:

```bash
datawatch projects arcs core-api --set a1b2c3d4:1:done,e5f6a7b8:2:active,c9d0e1f2:3:planned
```

### Order and link plans

The **Plans** page sub-tab does this with buttons. The same changes are on every
other surface; a plan is named by its id or its title:

```bash
datawatch plans list core-api                        # plans in priority order, then the backlog
datawatch plans create core-api "Release 9.x" --goal "Ship the next minor"
datawatch plans reorder "Release 9.x" "Docs refresh" # these take positions 1 and 2
datawatch plans reorder-automata "Release 9.x" a1b2c3d4 e5f6a7b8
datawatch plans attach "Release 9.x" --automaton a1b2c3d4
datawatch plans detach "Release 9.x" --automaton a1b2c3d4
datawatch plans migrate                              # dry run: nothing is written
datawatch plans migrate --apply                      # build plans from the older plan list and registry rows
```

MCP has `plan_create`, `plan_reorder`, `plan_reorder_automata`, `plan_attach`,
`plan_detach` and `plan_migrate`; chat has `autonomous plan list|new|move|attach|detach`
and `autonomous plan-migrate [apply]`. A link held by another plan is refused and
the holder is named; detach first. The older `datawatch projects arcs`,
`project_set_arcs` and `PUT /api/projects/{name}/arcs` are deprecated aliases
that keep working. Full tables:
[plan-records.md](plan-records.md#ordering-and-linking-plans).

### Which Automata belong to a project

Every roll-up decides membership the same way: the project's page, its plans, the
Gantt rows, the History list, the daily progress snapshots, the cost and progress
grouping and the project filter in **Events**. An Automaton is placed in the
first project that matches, in this order:

1. **Its own project link**: the name of a configured project, set on the Automaton.
2. **The link of the Automation holding it.** A member without its own link
   inherits the Automation's link.
3. **Its parent Automaton's project**, found by the same two steps up the parent
   chain (a spawned child follows its parent; the walk is capped and cannot loop).
4. **Its directory.** The Automaton's project directory is matched to a project's
   directory. A git worktree maps to its repository, so work done in a worktree
   counts toward the project that owns the repository.

A link always wins over a directory match, so an Automaton linked to `core-api`
is never counted under another project because it ran in that project's folder.
A link naming a project that is not configured is ignored and the next step
applies. An Automaton a plan holds stays in that plan's project's roll-up as
before. An Automaton that resolves to another project is never in this one.
Template Automata are never counted.

An Automaton no step places is in the **Automata without a project** group.
Each appears there once with its own tasks, points and throughput. The group is
not part of any project's `total`, so a project's totals never include work that
belongs to nobody.

### Link an Automaton to a project

The link is the first thing the roll-up checks, so setting it is the surest way
to place an Automaton. The name must be a configured project; an unknown name is
refused and the message lists the configured names. An empty name clears the
link, and the Automaton is then placed by its Automation, its parent or its
directory. Each change is written to the Automaton's decision log and touches
only the Automaton record, never a session.

| Where | How |
|---|---|
| Web app | Open the Automaton, then **Settings**. The **Project** field says where the current project came from (set directly on this Automaton, from its Automation, from its parent Automaton, from its directory, or no project). Pick a project and choose **Save project**, or choose **Clear link** |
| REST | `POST /api/autonomous/prds/{id}/set_project` with `{"project":"core-api"}`; send `""` to clear. A 400 names the configured projects, a 404 means the Automaton is unknown |
| CLI | `datawatch autonomous prd-set-project <id> core-api`; use `""` or `clear` as the name to clear |
| MCP | `autonomous_prd_set_project` with `id` and `project` |
| Chat | `automaton project <id> <name>` or `automaton project <id> clear` (also `autonomous project ...`) |

The Automaton read reply (`GET /api/autonomous/prds/{id}`) carries `project`,
the project the Automaton belongs to, and `project_source`, how it was found:
`link`, `automation`, `parent`, `directory` or `none`.

### Link existing Automata in one pass

Automata created before the link existed have none. The **backfill** finds the
ones that belong to the `datawatch` project and links them. It matches only that
project: the project named `datawatch`, or, if none has that name, the first
configured project whose directory or repository is the datawatch repository.
It tries the Automation's project, then the parent's project, then the
repository directory (a worktree maps to its repository). An Automaton that
already has a link is never changed, a template is never touched, and an
Automaton that matches nothing is listed as skipped and left alone.

The default is a **dry run**: it produces the report and writes nothing.

```bash
datawatch autonomous project-link-backfill           # dry run: print the report table
datawatch autonomous project-link-backfill --apply   # write the links
```

The report lists each Automaton with its project, how it matched
(`automation`, `parent` or `directory`) and the result (would link, linked,
skipped with the reason, or failed), then the counts: scanned, matched, applied,
skipped and failed. A failure on one Automaton is recorded and the rest go on.
Each Automaton the backfill links gets one decision-log entry of kind
`project_link_backfill` (a link set by hand is `project_link_set`), so you can see
when and why it was placed. A second `--apply` finds nothing to do, because every
matched Automaton now carries a link.

The same pass is on REST (`POST /api/autonomous/project-link-backfill` with
`{"apply":true}`; omit `apply` or send `false` for a dry run; `GET` on the same
path returns the latest report, or 404 if none has run yet), MCP
(`autonomous_project_link_backfill`, `apply` true to write) and chat
(`automaton project-backfill`, or `automaton project-backfill apply`).

The daemon also runs it once at start. With
`autonomous.project_link_backfill_apply: false` (the default) that run is a dry
run only and the report is kept for `GET`. With `true` the run is applied, and
when every write succeeded the daemon writes a done marker
(`project_link_backfill.done` in the autonomous data directory) so later starts
only produce the dry-run report. To run it again after the marker exists, use the
CLI, REST, MCP or chat command above; delete the marker to let the start-up run
apply again. The setting is in `docs/config-reference.yaml`, and can be changed
with `datawatch config set autonomous.project_link_backfill_apply true`.

## Reading the fields

| Field | Where it is set | What it means | What the daemon does with it |
|---|---|---|---|
| `owner` | CLI `set --owner`, REST, MCP | Who owns the project | Shown on the page and by `show` |
| `repository` | CLI `set --repository`, REST, MCP | Source repository, for example `example-org/core-api` | Shown on the page |
| `default_branch` | CLI `set --default-branch`, REST, MCP | Default git branch | Shown on the page |
| `target.name` | CLI `set --target`, REST, MCP | The outcome the project works toward | Shown on the page |
| `target.definition_of_done` | REST, MCP | When the target counts as reached | Shown on the page |
| `lane_defaults` | REST, MCP | Lane, backend and model new work starts with | Shown on the page; not yet used to place new Automata |
| `arcs` | `projects arcs`, REST `PUT .../arcs`, MCP `project_set_arcs` | The ordered Automations. Array position is the order; `priority` and `state` are stored as sent | Drives the order of the plans on the page and the roll-up |
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

### Plan records on the project page

The **Records** sub-tab opens with a browser of every registry row and finding of the
project, read from `GET /api/plan-records/browse`. A search box, a kind filter and a state
filter narrow the list, and Load more pages it. Each row shows its plan and Automata as links;
a link opens the Plans sub-tab with that plan expanded. The Registry and findings card sits
below the browser and is no longer on the other sub-tabs.

Below the history, the project page lists the project's plan records: switch
between **Registry rows** and **Findings**, filter by state, search titles and
text, open one to change its title, state, notes or linked Automaton, create a
record, or preview and apply an import of the old plans files. Records are never
deleted; closing one is a state change. Full guide:
[plan-records.md](plan-records.md).

### Plan states

| State | Meaning on the page |
|---|---|
| `planned` | Not started. Shown first in the order, closed by default |
| `active` | In progress. Opened by default |
| `done` | Finished. Its Automata stay visible under it |

A plan with no Automata yet shows "Plan has no Automata yet" in Events.

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
| Switch between the Overview sub-tabs (Inbox, Automations, ...) | With **Overview** picked, the strip under the picker; the choice is remembered in this browser |
| Open a project directly | Address `#projects/<name>`; the last project you opened is remembered in this browser |
| Open a page sub-tab directly | Address `#projects/<name>/<subtab>` with `plans` (the old `arcs` still works), `records`, `gantt`, `unassigned`, `history`, `policy` or `events`; no sub-tab in the address opens **Overview** |
| Switch between Overview, Plans, Records, Gantt, Unassigned, History, Policy and Events | The strip of page sub-tabs under the project name; the choice is remembered in this browser |
| Change the range of the Gantt | Gantt header: **All time**, 90, 30 or 7 days; wheel to zoom, drag to pan |
| Show or hide dependency edges | Gantt header **Edges** (on by default; also on the Focus page); **All edges** shows every drawn edge instead of the first 120; hover or tab to a bar to highlight its edges |
| Expand or collapse a Gantt row | The arrow at the left of a plan, an Automaton or the **Automata with no plan** group; **Load more** pages a long level |
| Focus on one Automation | **Focus** on a plan's Gantt row or card, or in the header of the Automation page; **Back** returns |
| Page through history | **Load more** at the foot of History |
| Change policy | **Settings → Automata → Project policy** (the **Open settings** link on the **Policy** page sub-tab opens the Automata tab, expands that section and scrolls to it) |
| Change the merge pipeline | **Settings → Automata**, the **Merge pipeline** group of the Automata settings card (the **Open settings** link on the **Policy** page sub-tab opens the Automata tab, expands that card and scrolls to it) |
| Add a project | **Manage projects** block under the picker: fill in the form, **Add project**. With no projects the form is already open |
| Edit a project's directory, description or default backend | **Manage projects**, **Edit** on the project's row, then **Save changes** |
| Remove a project | **Manage projects**, **Remove** on the project's row, then confirm. Only the record goes; sessions keep running |

The block edits the directory, description and default backend only. Plans,
target, lane defaults, owner and the numeric record fields are changed with the
surfaces below.

### REST

| Method and path | Purpose |
|---|---|
| `GET /api/projects` | List every project alias with its record |
| `GET /api/projects/{name}` | One project's record |
| `POST /api/projects` | Create or update an alias; absent record fields keep their stored value; invalid values return 400 and nothing is saved |
| `PUT /api/projects/{name}/arcs` | Replace the ordered plans (a bare array or `{"arcs": [...]}`); 404 for an unknown project |
| `GET /api/projects/{name}/rollup?window=7` | Roll-up: plans in order, Automata, tasks, points, throughput; `window` 1 to 365 days |
| `GET /api/projects/{name}/history?limit=50&cursor=…` | History page and Gantt rows |
| `GET /api/projects/{name}/timeline?level=arcs\|automata\|tasks&parent=…&automaton=…&focus=…&limit=50&cursor=…` | One level of the project timeline: start, end, status and release per plan, Automaton and task, `depends_on` on tasks and `edges` between Automata; 400 for a bad level, limit, cursor or a missing `automaton`; 404 for an unknown project, plan or Automaton |
| `GET /api/projects/{name}/policy/effective?automaton=<id>` | Effective policy with the tier of each setting |
| `DELETE /api/projects/{name}` | Remove the alias; the directory is not touched |

Example, replace the plans:

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
| `project_set_arcs` | `name`, `arcs` (required) | Replace the plans; `[]` clears them |
| `project_rollup` | `name`; `window` | The roll-up |
| `project_history` | `name`; `limit`, `cursor` | One history page |
| `project_timeline` | `name`; `level`, `parent`, `automaton`, `focus`, `limit`, `cursor` | One level of the project timeline (plans by default) |
| `project_policy_effective` | `name`; `automaton` | Effective policy with tiers |
| `project_alias_delete` | `name` | Remove the alias |

Example call, set the plans:

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
| `datawatch projects arcs <name>` | Print the plans in order |
| `datawatch projects arcs <name> --set id:priority:state,…` | Replace the plans; `--set ""` clears them |
| `datawatch projects history <name> [--limit N] [--cursor …]` | History page |
| `datawatch projects timeline <name> [--level arcs\|automata\|tasks] [--parent <arc>\|none] [--automaton <id>] [--focus <arc>\|none] [--limit N] [--cursor …]` | One level of the project timeline |
| `datawatch projects policy …` | Policy verbs; see [project-automation-policy](project-automation-policy.md) |
| `datawatch projects delete <name>` | Remove the alias; the directory is not touched |

Target definition of done and lane defaults are not CLI flags; use REST or MCP.

### Comm channel

Send these from Signal, Telegram or another connected channel:

| Command | Purpose |
|---|---|
| `project show <name>` | Record fields, one per line; `-` for unset |
| `project arcs <name>` | Plans in order |
| `project progress <name>` | Roll-up: tasks, points and throughput per plan |
| `project timeline <name> [level=arcs\|automata\|tasks] [parent=<arc>\|none] [automaton=<id>] [focus=<arc>\|none] [limit=<n>] [cursor=<c>]` | One line per row: kind, title, start and end (`now` while running), then `[status]`, `depends on …` and `release …` when present; Automaton chain edges follow as `from -> to (chain)` lines; says how to continue when more rows exist |
| `project set <name> <field> <value>` | Set one field: `owner`, `repository`, `default_branch`, `description`, `default_backend`, `major_trigger`, `weekly_share`, `branch_retention_days` |
| `project policy <name> [automaton]` | Effective policy (see [project-automation-policy](project-automation-policy.md)) |

The comm channel does not replace plans, set the target, the definition of done or
the lane defaults. Use REST, MCP or the CLI for those.

## Troubleshooting

| Symptom | Cause and fix |
|---|---|
| The Projects sub-tab says "No projects yet" | No alias is registered. Use the **Manage projects** form on the page, or run `datawatch projects upsert` (step 1) |
| **Add project** says "Enter a project name." or "Enter the project directory." | Both fields are required. The directory must be an absolute path |
| A save or remove shows an error above the form | The daemon refused the change and nothing was saved; read the message, fix the field and try again |
| Roll-up shows "Could not load the roll-up" | The autonomous store is off or the alias is unknown. Check the daemon with `datawatch projects get <name>` |
| A block I used to see on the project page is missing | The page now shows one page sub-tab at a time. Find the block in the table under step 6 (for example the Gantt is on **Gantt** and the policy and merge pipeline are on **Policy**) |
| There is no **Events** page sub-tab | It is left out of the strip while the project has no notes. An `#projects/<name>/events` address or a remembered **Events** choice opens **Overview** in that case |
| The page opens a page sub-tab I did not pick | The last choice is remembered in this browser. Choose **Overview**, or open `#projects/<name>`, which always opens **Overview** |
| A plan shows no Automata | Its Automation has no member Automata yet, or the Automata are of another project |
| An Automaton is missing from a project's page, or appears under a different one | Membership follows the order in [Which Automata belong to a project](#which-automata-belong-to-a-project). Its own link or its Automation's link beats its directory. Look for it under **Automata without a project** if nothing placed it, then set its link as in [Link an Automaton to a project](#link-an-automaton-to-a-project) |
| The backfill report lists an Automaton as skipped | It matched no datawatch source (Automation, parent or directory), or no project that is the datawatch project is configured. Link it by hand, or add the project first |
| `arcs` returns 400 "automation_id is required" or "duplicate automation_id" | Each plan needs a unique id; a plan whose state is not `planned`, `active` or `done` is refused |
| `set --weekly-share 150` is refused | Weekly share must be 0 to 100 |
| `set --branch-retention-days -1` is refused | Retention cannot be negative |
| Policy disappeared after an `upsert` | `upsert` writes the policy it is sent, which is none; set the policy again with `datawatch projects policy set`, or use `set` and `arcs` for later changes |
| Plan documents, design documents or issues show "none recorded" | The plan record has no fields for them yet, so the page cannot show them. The plan section shows what the record holds |

## Limits in this release

- The record's weekly share, branch retention and major trigger are kept and
  shown. The scheduler, the worktree cleanup and the release decision do not read
  the project's own values yet.
- Lane defaults are recorded for new work but do not yet decide where an Automaton
  runs.
- The **Manage projects** block changes only the name, directory, description
  and default backend. Plans, target, lane defaults, owner and the numeric record
  fields go through REST, MCP, the CLI or the comm channel.

## For the operator to review

- **Mobile parity.** The Android and iPhone apps still show Projects as its own
  screen and a project as one screen. Moving Projects under Automata, matching
  page sub-tabs, and the Manage projects add, edit and remove controls are tracked
  in datawatch-app and are not part of this change.
- **Old links keep working.** The `#projects` and `#projects/<name>` addresses
  and the Projects link open the sub-tab, so bookmarks and the Settings help
  links stay valid. This page keeps its file name (`projects-tab.md`) for the
  same reason; rename it only if you also redirect the links that point here.
- **Plan records.** The project page has a Plan records section (registry rows
  and findings, state filter, search, detail edit, New record, Import archive…).
  It hides when `autonomous.plan_records.enabled` is false. Confirm that its
  position below the history and above the links is where you want it, and that
  the section label and the project label wording read well; see
  [plan-records.md](plan-records.md). Mobile apps do not have it yet.
- **Sub-tab order.** Projects, Automation, Automata, Events, Templates is the
  order shipped. Confirm it is the order you want before the next release.
- **Page sub-tab order and grouping.** Inside a project the strip reads Overview,
  Plans, Gantt, Unassigned, History, Policy, Events (the 2026-11-12 decision "b - sub
  tabs", "the order is good"). The strip is not in the order the old single page
  listed its blocks (Gantt used to follow Unassigned). Header and Progress share
  **Overview**, and Effective policy and Merge pipeline share **Policy**; confirm
  that grouping.
- **Events can be unreachable.** The **Events** page sub-tab appears only while the
  project has notes (empty sections, a missing weekly share, a failed roll-up). The
  event stream filtered to the project sits under those notes, so with no notes the
  stream has no page sub-tab to show it. Confirm that is intended.
- **Overview sub-tab strip look and memory.** Under **Overview** the in-flight
  sub-tabs now use the Projects strip look and share the one remembered value
  (`cs_projects_subtab`) with the project page sub-tabs, so a choice made in one
  place can be restored in the other when it is a valid name there. Confirm that
  sharing, and that the Overview strip should have no address of its own.
- **Remembered page sub-tab is global.** The remembered page sub-tab is one value
  per browser, not per project, so it carries over when you open another project.
- **Screenshots and translations.** This page has no screenshots of the old single
  page to replace. The eight labels and the "Project sections" label ship in the
  English, German, Spanish, French and Japanese locales; a native speaker has not
  reviewed the four translations.
- **Empty-state link.** The empty Projects picker used to show an **Open Settings**
  button that went to Project policy. This page now describes the **Manage
  projects** form as the place to start, and the button is no longer listed.
  Confirm that is the intended behavior in the shipped web app.
- **Edit keeps the record.** The page says editing keeps the policy, plans, owner,
  target and other record fields. That follows the shipped form's behavior as
  documented in the change; confirm it on a project that has a policy and plans.

- **Gantt edges and ordering (2026-11-12).** The Gantt now draws dependency edges, an
  **Edges** toggle and a hover highlight, fits **All time** to the visible rows, and orders
  late-loaded rows by completion with dependencies first. The cap of 120 edges and the 2
  percent margin are the shipped defaults, not settings. The edge choice is the browser
  key `cs_projects_gantt_edges` (`on`, `all` or `off`), a convenience only. The new labels
  (Edges, All edges, the six status words, Release) ship in the English, German, Spanish,
  French and Japanese locales; a native speaker has not reviewed the four translations.
  Android and iPhone do not draw edges yet; tracked in datawatch-app. Confirm the cap and
  the default of edges on, and check the edge routing on a project with many dependencies.

- **Focus header dates (2026-11-13).** The Focus header's "First task … – last
task …" line prints the two ends of the displayed range (the first task pulled
back and the last task pushed out by the 2 percent margin), not the exact dates
of the first and last task; each side can read up to a day off. Confirm that the
range-end wording is what you want under the "First task" and "last task"
words, rather than changing the label or the dates. The date format is the
browser's local `toLocaleDateString()`; the label ships in the five locales,
untranslated native review pending as with the other labels here.

- **The Automation link has no control.** Setting a link works on an Automaton
  from the web app, REST, MCP, the CLI and chat. An Automation also carries a
  `project` field that its members inherit, but no surface added with this change
  sets it. Confirm whether the operator wants one, or whether linking each
  Automaton is enough.
- **The backfill only knows `datawatch`.** It links Automata to the datawatch
  project and nothing else. Work belonging to other projects keeps resting on
  its directory until it is linked by hand. Check the dry-run report before
  turning on `autonomous.project_link_backfill_apply`, and confirm a project named
  `datawatch` (or one whose directory is the datawatch repository) is configured.
- **Roll-up behavior is from tests.** The order above, the backfill and the
  `without_project` group follow the resolver and its tests, not a check on a
  live daemon. Open a project that has worktree-based or spawned Automata and
  confirm the page, History and Events agree.
- **Mobile apps.** The Project field in Automaton settings and the
  *Automata without a project* group on the project page are web-only; the
  Android and iPhone apps need the matching control and must ignore the new
  `project_source` and `without_project` fields without error (tracked in
  datawatch-app).
- **Totals changed for some projects.** A project that used to count an
  Automaton by directory alone no longer does if that Automaton is linked to
  another project. Expect a project's totals to shift once links are set or the
  backfill is applied.

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
