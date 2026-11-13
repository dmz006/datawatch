---
docs:
  index: true
  topics: [autonomous, plan-records, registry-rows, findings, projects, import]
exec_params: []
exec_steps:
  - tool: get_config
    description: Read the plan records settings (master switch, write-through and import conflict policy)
    args: {section: "autonomous"}
    read_only: true
  - tool: plan_record_list
    description: List the plan records, newest first
    args: {}
    read_only: true
---
# How-to: plan records, registry rows and findings kept by datawatch

A **plan record** is a *plan* (the thing you are working on, which links down
to Automations and Automata), one item of the plans registry (a *registry row*)
or one problem raised by an Automaton or an operator (a *finding*), kept by the daemon
instead of as a numbered line in `docs/plans/README.md` or an entry in a
findings file. Operators and Automata read and change the same records through
the same surfaces. Terms are in
[datawatch-definitions.md](../datawatch-definitions.md#plan-records).

This guide covers what a record holds, how to name one, the five surfaces,
importing the old files, what happens when an Automaton ships, the settings,
and how to turn it off.

## What a record holds

| Field | Meaning |
|---|---|
| `title` | The human name. Unique inside one project (case-insensitive, trimmed). |
| `id` | A short readable id, unique in the installation. |
| `uid` | An identifier that never changes. |
| `kind` | `plan`, `registry_row` or `finding`. |
| `project` | The registered project alias the record belongs to. Fixed when the record is created. |
| `state` | Registry rows: `open`, `planned`, `in_progress`, `blocked`, `shipped`, `done`, `deferred`, `cancelled`. Findings: `open`, `triaged`, `fixed`, `wontfix`, `duplicate`. |
| `body` | The notes. |
| `legacy_number` | The old registry row number, when the record came from the registry file. |
| `shipped_by`, `shipped_version` | The Automaton id and version that shipped a row. |
| `raised_by` | The Automaton id that raised a finding. |
| `goal`, `order` | Plans only. The goal text, and the priority order (lowest runs first). |
| `automations`, `automata`, `findings` | Plans only. The ids the plan links down to, in display order. They change through the link calls, never by editing the record body. |
| `links`, `aliases` | Links, and older titles that still find the record. |
| `needs_review`, `review_reason` | Set by an import when a conflict was resolved for you. |

Records are never deleted. Closing one is a state change, and a closed record
still resolves.

## Naming a record

Every surface takes a **reference**. It is tried in this order: id, uid,
`row <number>` (`row-<number>` in a URL), exact title, an older title, an
unambiguous title prefix. A reference that matches more than one record is not
guessed: REST answers 409, the CLI exits non-zero, and each lists the
candidates so you can repeat the call with an id.

## The surfaces

| Surface | How |
|---|---|
| REST | `GET` and `POST /api/plan-records/registry-rows` and `/api/plan-records/findings`; `GET` and `PUT` on `.../{ref}`; `GET /api/plan-records/search?q=`; `POST /api/plan-records/import[?dry_run=true]`. List filters: `project`, `state`, `legacy_number`, `limit` (default 50, most 500), `offset`. There is no delete route. Reads need `autonomous:read`, writes `autonomous:write`. |
| MCP | `plan_record_list`, `plan_record_get`, `plan_record_create`, `plan_record_update`, `plan_record_search`, `plan_record_import`. |
| CLI | `datawatch plan-record list\|get\|create\|update\|search\|import` (alias `plan-records`). `import` previews by default; add `--dry-run=false` to write. |
| Chat | `autonomous plan-records [registry-row\|finding] [project=P] [state=S]`, `autonomous plan-record get <ref>`, `create <json>`, `update <ref> <json>`, `search <text>`, `import [dry-run] [project=P] [source=S] [policy=P]`. Lists and searches reply with the ten best lines, title first. |
| Plans read API | `GET /api/projects/{name}/plans`, `GET /api/projects/{name}/overview-summary`, `GET /api/plans/{ref}/automata`, `GET /api/plans/{ref}/tasks`, `GET /api/plan-records/browse`. See *Reading plans* below. |
| Plans write API | `POST /api/plans` (create), `PUT /api/plans/order`, `PUT /api/plans/{ref}/automata/order`, `POST /api/plans/{ref}/attach` and `POST /api/plans/{ref}/detach` (kind `automaton`, `automation` or `finding`). MCP `plan_create`, `plan_reorder`, `plan_reorder_automata`, `plan_attach`, `plan_detach`. CLI `datawatch plans list\|create\|reorder\|reorder-automata\|attach\|detach`. Chat `autonomous plan list\|new\|move\|attach\|detach`. See *Ordering and linking plans* below. |
| Plans roll-up | `GET /api/plans/rollup?project=&dry_run=true` (preview) and `POST /api/plans/rollup` (apply), MCP `plan_rollup`, CLI `datawatch plans rollup [--project P] [--dry-run] [--json]`, chat `autonomous plan rollup [dry-run] [project=P]`. See *Rolling plan status up from the work* below. |
| Plans migration | `POST /api/plans/migrate` with `{"apply": bool}`, MCP `plan_migrate`, CLI `datawatch plans migrate [--apply]`, chat `autonomous plan-migrate [apply]`. See *Building plans from arcs and rows* below. |
| PWA | On the project page of the Projects sub-tab: the **Plans** page sub-tab (the Plans browser, see *The Plans browser* below), the **Records** page sub-tab (a search box, a kind filter and **Load more** over every registry row and finding, each with its plan and Automata as links) and, under Records, the **Registry and findings** section: Registry rows and Findings views, a state filter, a detail panel to change the title, state, notes and the linked Automaton, **New record**, and **Import archive…** with a preview before anything is written. The sections hide themselves when plan records are off. |

Each created or changed record writes one `plan_record_created` or
`plan_record_updated` audit event; a real import writes one
`plan_record_imported` summary event.

## Creating and changing records

A new record needs a `kind`, a registered `project` alias and a `title`. The
daemon gives it its own id (never the old row number) and a uid, sets the
dates, and starts it in state `open`. A title already used in the same project
is refused; the same title in another project is fine. Project, kind and
`legacy_number` cannot be changed afterwards. Renaming a record keeps the old
title as an older title that still finds it.

```sh
# CLI: a registry row, then a finding
datawatch plan-record create --kind registry-row --project datawatch \
  --title "Plan records in the registry" --state planned --body "Keep rows in datawatch."
datawatch plan-record create --kind finding --project datawatch \
  --title "Import skipped a row" --raised-by <automaton-id>

# CLI: change by title, id or row number; flags you leave out are untouched
datawatch plan-record update "Plan records in the registry" --state in_progress
datawatch plan-record update "row 412" --title "Plan records, step one" --state shipped
```

On REST send the same fields as JSON: `POST /api/plan-records/registry-rows`
with `{"project":"datawatch","title":"...","state":"planned"}`, then
`PUT /api/plan-records/registry-rows/{ref}` with only the fields to change.
Over MCP use `plan_record_create` and `plan_record_update`
(`{"kind":"registry-row","ref":"row 412","state":"shipped"}`). In chat send
`autonomous plan-record create <json>` or `autonomous plan-record update <ref>
<json>`. In the web app use **New record** or open a record in the detail
panel.

## Finding a record

Use whatever you remember. `datawatch plan-record get "row 412"`,
`get plan-records-in-the-registry` (an id), `get "Plan records in"` (a title
prefix) and `get "Old title"` (an older title) all reach the same record when
the match is unique. When two projects use the same title, add `--project`
(`project` on the other surfaces) to choose. `datawatch plan-record search
<words>` looks through titles, older titles and notes, with title hits first,
and `list` filters by `project`, `state` or `legacy_number`.

## Importing the old files

The import reads the numbered rows of `docs/plans/README.md` and the findings
files (`docs/plans/**/*findings*.md` by default; the REST body field
`findings_glob` overrides it) of a registered project and creates one record
each.

1. Preview first: `datawatch plan-record import --project datawatch`, REST
   `POST /api/plan-records/import?dry_run=true`, or **Preview import** in the PWA.
   Read the counts (to create, unchanged, changed, conflicts, skipped) and each
   conflict.
2. Apply: `datawatch plan-record import --project datawatch --dry-run=false`,
   REST without `dry_run`, or **Apply import**.

The import only reads: the files stay in place as the archive. It is
idempotent, keyed by project, kind and `legacy_number` (rows) or
`legacy_source` (findings), so a second run creates nothing; when a source
changed since the first run it reports the record as changed and updates it
only when nobody edited the record in between. The project must be a
registered alias (an unknown one is refused with a message to register it
first) whose directory sits under an allowed project root. The default
project is `datawatch`; `source` is `registry`, `findings` or `all`.

The **conflict policy** applies to every conflict in a run: `suffix` (default)
appends ` (row <n>)` or ` (<file>)` and flags the record `needs_review`,
`rename` takes a new title from the caller, `skip` leaves the record out.
After an import, look through the records for the **Needs review** mark (`needs_review` true) and check each one.

## What happens when an Automaton ships

The merge pipeline's `registry_row` post-ship hook normally edits the registry
file. With **write-through** on, it records the shipped row as a plan record
instead and never touches the README:

- It looks for an existing registry row in the Automaton's project: first by
  the row number its row pattern carries, then by the pattern text and the
  Automaton title (exact title or an older title).
- A match is moved to `shipped` and gets `shipped_by` (the Automaton id) and
  `shipped_version`. No match creates a new registry row titled by the
  Automaton. The change is made by the actor `merge-pipeline`.
- A failed write fails the hook with a redacted message. It never falls back
  to editing the README.
- With write-through off (the default), or when no record store is wired, the
  hook behaves exactly as before.

**The supervisor.** With write-through on, a supervisor or any other Automaton
that needs to add or change a row does it through the `plan_record_*` tools:
`plan_record_search` or `plan_record_get` to find the row by title, id or
`row <n>`, then `plan_record_update` (or `plan_record_create` when there is
none) to set the state, the notes and `shipped_by`. It does not edit
`docs/plans/README.md`. With write-through off, the README stays the place to
edit and the tools are only for reading and for records you keep on purpose.
Only the operator turns the setting on or off.

Write-through changes only the registry-row hook. It does not write record
changes back into the plans files, and the files are not updated when a record
is edited by hand.

## Plans: links and order

A plan links down: plan, then Automation, then Automaton, then tasks. A finding
attaches to a plan or to an Automaton. Rules that are easy to trip over:

- **A link is never overwritten.** An Automaton or Automation already linked to
  a different plan is refused (the reply names the plan holding it), and so is a
  finding already attached elsewhere. Detach it first, then attach it again.
- **Order is a priority.** The lowest `order` runs first. Reordering plans, or
  the Automata inside one plan, is its own change and writes a
  `plan_record_reordered` audit event; attaching and detaching write
  `plan_record_linked` and `plan_record_unlinked`.
- A plan can exist with no Automaton yet. It then shows in the *backlog*.
- **Each link remembers a title and a state.** Attaching stores the Automaton's
  or Automation's title and state at that moment; the plan status roll-up is
  derived from those stored states (see *Rolling plan status up from the work*).
- Older records have none of these fields and load unchanged.

## The model

Work is linked down in one chain: **plan**, then **Automation**, then
**Automata**, then tasks. A plan is the registry record of what you are working
on; an Automation is a named chain of Automata; an Automaton is one unit of
autonomous work. A finding attaches to a plan or to an Automaton. The word
*record* is only for what is stored (a plan, a registry row or a finding) and
for the **Records** page sub-tab of the project page.

## Settings

All under `autonomous.plan_records.`; change them with `PUT /api/config`,
`datawatch config set`, the chat `configure` verb or the MCP `config_set`
tool. They take effect at once, and a bad value is refused. In the PWA they
are in **Settings → Automata**.

| Key | Default | Meaning |
|---|---|---|
| `enabled` | `true` | Master switch. While `false` every `/api/plan-records` route answers 404 and the PWA section hides. |
| `write_through` | `false` | Make the `registry_row` post-ship hook record the shipped row as a plan record. |
| `import_conflict_policy` | `suffix` | `suffix`, `rename` or `skip`; used when an import names no policy. |

## Storage and security

One JSON file per record under `<data dir>/plan-records/<kind>/<uid>.json`,
written through a temporary file and an atomic rename; the index is rebuilt
from the files when the store opens. With secure mode on, each record is
sealed with the same file sealing the other per-record data files use.
Exactly these fields stay readable without the key, so records can still be
filtered: `kind`, `project`, `state`, `id`, `uid`, `legacy_number`, and the
dates `created_at`, `updated_at` and `closed_at`. Everything else (title,
notes, older titles, links, Automaton ids, review fields, legacy source and
shipped version) is only inside the sealed payload. Without secure mode the
files are plain JSON. See [encryption](../encryption.md).

## Turning it off, and rollback

Set `autonomous.plan_records.enabled` to `false` (or leave `write_through` off
to keep the registry file as the hook's target). Records are kept on disk. The
switch hides the routes and the section but does not stop the once-a-day plan
status roll-up (see *Rolling plan status up from the work*); each status it
wrote is in the audit log as `plan_record_rolled_up` with the old status.
Removing the `plan-records` directory under the data dir discards every
record; the original files are never changed by an import, so re-importing
rebuilds them. One caveat: rows shipped by write-through while it was on exist
only as records, so copy them back into the registry file by hand before you
remove the directory.

## Still for the operator to review

Nothing here turns on by itself. These are yours to settle:

- **Records marked `needs_review`.** After the first real import, look through
  the records an import flagged and settle each one (rename, merge or accept).
- **Write-through.** `autonomous.plan_records.write_through` stays off until
  you turn it on. Decide when the registry hook should write records instead of
  editing `docs/plans/README.md`.
- **The old files.** `docs/plans/README.md` and the findings files stay in
  place as the read-only archive. Whether and when to clean them up is a later
  step you decide.
- **The arcs are deprecated, not removed.** After `datawatch plans migrate
  --apply` the project arcs still exist and still answer on
  `GET /api/projects/{name}/arcs`, the roll-up and `project_set_arcs`. Decide
  when the Projects page and your scripts move to the plans routes, and when the
  arcs may be cleared. Nothing deletes them for you.
- **Migration conflicts.** A title already taken by a record the migration did
  not make is left alone and listed as a conflict; rename one side by hand and
  run the migration again.
- **Backlog order.** Open rows with no Automaton become backlog plans ordered by
  their old row number. Check that this is the order you want.
- **Renaming and editing a plan.** No surface changes a plan's title, goal or
  status after it is created: the plan-record routes and tools take
  `registry-row` and `finding` only, `datawatch plans` has no update command,
  and the Plans browser creates, orders and links but does not edit. Decide
  where plan edit belongs (and whether it keeps the old title as an older title,
  as a registry row does) before this is described to anyone as available.
- **Handing a status back to the roll-up.** Nothing clears `set_by_hand`, so a
  plan marked by hand would stay out of the roll-up for good. Decide how an
  operator hands it back, for example a clear option next to the status edit.
- **The roll-up sees recorded state, not live state.** See *Rolling plan status
  up from the work*. Decide whether the daily run should first refresh each
  link's state from the live Automata, and whether it should also run at daemon
  start.
- **Turning plan records off does not stop the daily roll-up.** The switch
  hides the routes and the section (404), but the daily run still rolls plan
  statuses up in the background. Decide whether the switch should stop it.
- **Ship-time roll-up needs write-through.** With `write_through` off, only the
  daily run and your own calls move a plan to `shipped`.
- **Mobile apps.** Android and iPhone do not show plan records yet; that is
  tracked in the datawatch-app repository.

## If something looks wrong

- **404 from a plan record route, or the PWA section is missing.** Plan records
  are off: set `autonomous.plan_records.enabled` to `true`.
- **409 on a reference.** More than one record matches; repeat with an id.
- **Import refuses the project.** Register the alias first (Projects sub-tab or
  `datawatch projects upsert`), and check its directory is under an allowed
  project root.
- **A shipped row did not appear.** Check `write_through` is on and the hook
  result in the merge run; with it off the hook edits the README as before.

## Building plans from arcs and rows

A plan groups work to come and links down to Automations and Automata. The
migration builds plans from what you already have:

- Every project arc becomes one plan: the title and goal come from its
  Automation, the arc priority becomes the plan order, the arc state
  (planned, active, done) becomes the plan state, and the plan links to that
  Automation.
- Every registry row whose text or links name an Automaton or Automation id
  becomes a plan linked to it. The plan is titled `Plan: <row title>`, because a
  title is unique inside a project and the row keeps its own. See *How a plan
  gets its title* below.
- Every open row that names none becomes a backlog plan, ordered by its old row
  number. A closed row that names none is left out.

It previews by default. Read the lists (would create, would link, left alone,
conflicts), then apply:

```
datawatch plans migrate            # preview, writes nothing
datawatch plans migrate --apply    # write
```

Every plan the migration makes carries a `migrated-from` link naming its source,
so a second apply finds it again and changes nothing. An Automaton or
Automation already linked to a plan is left where it is and reported as left
alone; a title already taken by a record the migration did not make is reported
as a conflict and left alone. No arc, row or config key is deleted. An apply
writes one audit event (`plan_record_imported`, subject `plans migrate`) besides
the event for each plan and link it writes.

### How a plan gets its title

Some registry rows hold a status note where the title should be, for example
`done, shipped in 9.5.92 (PRD 332c0001)`. The migration does not use such text
as a name. For each row it looks at the row's title text, then at its
`Description`, `Summary`, `Goal` or `Title` column if the row has one, and takes
the first one that reads like a name:

- Leading status words (`done`, `shipped`, `failed`, `blocked`, `not started`
  and the like) are dropped, so `Done: Fix the retry check` becomes
  `Plan: Fix the retry check`.
- The title is one line and at most 100 characters, `Plan: ` included.
- Text that is only a status note, or starts with a PRD id or a version, is not
  a title. If nothing in the row is a title but the row names an Automaton (or
  an Automation), the plan takes that one's own title, on one line, at most 100
  characters with `Plan: ` included. Only a row with neither is named from its
  number: `Plan: (untitled row 15)`. No screen or command renames a plan yet
  (see *Still for the operator to review*), so check the titles after an apply.
- The row's status is kept as the plan's status; a done row becomes a finished
  plan.

The preview line ends with three counts, `titles derived N, from linked
Automaton K, fell back M`: how many of the plans this run makes got a title from
their row, how many took the title of the Automaton or Automation the row
names, and how many were named `(untitled row N)`. A plan that already exists
is never renamed, and it is counted in none of them, so after an apply all
three read 0.

## Reading plans

The plans read routes show a project as plans in priority order. They need
`autonomous:read`, answer 404 while plan records are off, and page every list
with `limit` (default 50, most 500) and `offset`.

- `GET /api/projects/{name}/plans` lists the plans that have at least one
  Automaton. Each row has the title, goal, state, progress (Automata done out of
  total, with a whole-number percent for a bar), points, the ids of its
  Automations and Automata, and a `next` marker. Plans with no Automaton yet are
  in the `backlog` group, which pages with `backlog_limit` and `backlog_offset`.
- `GET /api/projects/{name}/overview-summary` is the small answer for the top of
  the project overview: header, overall progress and points, one line per active
  plan with its state and progress, what is next, and counts of plans, registry
  rows and findings by state.
- `GET /api/plans/{ref}/automata` lists one plan's Automata in run order:
  completed first (oldest completion first), then running, then next, then
  blocked, then held. Blocked and held Automata are shown and marked `skipped`;
  the next marker never lands on them.
- `GET /api/plans/{ref}/tasks` lists the plan's tasks. Call it when a plan is
  expanded. It is the one plans read that opens Automaton records, and only for
  the plan you asked about.
- `GET /api/plan-records/browse` lists every registry row and finding with the
  plan and Automata each links to. Filter with `q` (words in the title), `kind`,
  `state` and `project`.

Points are usage points, never dollars; an Automaton with no figure counts as 0.
The lists read the plan-record summary index, so they stay fast with thousands of
records. The older `GET /api/projects/{name}/arcs` and the `arcs` field of the
roll-up still answer, with the same arcs data and a `plans` list added; they are
deprecated.

## The Plans browser

The **Plans** page sub-tab of a project shows plans in priority order, each with
its goal, state, a progress bar of finished Automata and its points, then a
**Backlog** group of plans with no Automaton yet. Open a plan to see its
Automations, its Automata in run order (with the **Next** marker, skipping
blocked and held Automata), its tasks (read only when the plan is opened, with
**Load more**), and the findings and registry rows linked to it with their old
row numbers. The up and down buttons reorder plans and a plan's Automata;
**Create plan** adds one, and the attach controls link an Automaton, an
Automation or a finding by id. The search box covers plan, Automaton, registry
row and finding titles and the state filter narrows by plan state; reordering is
off while either is in use. Which plans are expanded is remembered in this
browser only. The **Overview** page sub-tab starts with a summary (active
plans, what is next, counts), and the **Records** page sub-tab lists every
registry row and finding. The controls call the routes in the next section.

## Ordering and linking plans

Every change below is on REST, CLI, MCP and chat, and in the web app except detaching (reading the ordered list is not in MCP). A plan is named by its id or its title.
They need `autonomous:write` for a federated peer and answer 404 while plan
records are off.

| To | Web app | REST | CLI | MCP | Chat |
|---|---|---|---|---|---|
| See a project's plans | Plans sub-tab | `GET /api/projects/{name}/plans` | `datawatch plans list <project>` | not in MCP; `plan_record_list` lists the plan records | `autonomous plan list <project>` |
| Create a plan | **Create plan** | `POST /api/plans` `{project, title, goal}` | `datawatch plans create <project> <title> [--goal TEXT]` | `plan_create` | `autonomous plan new <project> <title...>` |
| Reorder plans | up and down buttons | `PUT /api/plans/order` `{plans: [...]}` | `datawatch plans reorder <plan>...` | `plan_reorder` | `autonomous plan move <project> <plan> <position>` |
| Reorder a plan's Automata | up and down buttons on an open plan | `PUT /api/plans/{ref}/automata/order` `{automata: [...]}` | `datawatch plans reorder-automata <plan> <automaton>...` | `plan_reorder_automata` | `autonomous plan move <plan> automaton <id> <position>` |
| Attach | **Attach** controls | `POST /api/plans/{ref}/attach` `{kind, id}` | `datawatch plans attach <plan> --automaton\|--automation\|--finding ID` | `plan_attach` | `autonomous plan attach <plan> <kind> <id>` |
| Detach | (use REST, CLI, MCP or chat) | `POST /api/plans/{ref}/detach` `{kind, id}` | `datawatch plans detach <plan> --automaton\|--automation\|--finding ID` | `plan_detach` | `autonomous plan detach <plan> <kind> <id>` |

Reordering plans: the plans you list take positions 1 to n in that order and
every other plan follows in its old order. A plan that does not exist, or one
listed twice, fails the whole call and nothing changes. Reordering a plan's
Automata must list every Automaton of the plan once. The answer shows the run
order and the **next marker**. The next marker skips blocked and held Automata,
so moving a held Automaton to the front does not make it run next; it stays
shown, marked skipped, until it is released.

Attaching checks the id first: an unknown Automaton or Automation answers 404.
A link is never replaced. An Automaton or Automation already linked to another
plan, or a finding already attached elsewhere, answers 409 and names where it
is; detach it first, then attach it. Attaching the same item to the same plan
again changes nothing. A detach must name the plan the item is in, so it can
never reach into a different plan.

`datawatch projects arcs <name>` and the `project_set_arcs` tool stay as
deprecated aliases. The CLI prints a note and runs the plans list; in chat,
`arcs` is accepted in place of `plan` (`autonomous arcs list <project>`).

## Rolling plan status up from the work

A plan's status follows the work linked to it. The roll-up looks at the state
last reported for each linked Automaton and Automation and derives the status:

| Linked work | Plan status |
|---|---|
| none linked | left alone |
| any item failed or blocked | `blocked` (it wins over running) |
| any item running | `open` |
| every item shipped | `shipped` |
| every item finished | `done` |
| anything else (some waiting or held) | `open` |

A status that someone set by hand is never changed. A plan list marks it:
`datawatch plans list` and chat `plan list` print `(by hand)` after the status,
and REST and MCP carry `set_by_hand: true` next to `state` on the plan list, on
the plan header of the Automata and tasks reads, and on `plan_record_get` and
`plan_record_list`. A plan whose status was written by hand keeps it: the
roll-up reports what it would have written (`set_by_hand_kept`) and changes
nothing. Plans the daemon makes (a create, a migration) are not by hand, so
they roll up. Today no REST, MCP, CLI, chat or web screen writes a plan's
status or clears the marker, so `set_by_hand` is only ever true on a plan that
was marked in the store directly; see *Still for the operator to review*.

| To | REST | CLI | MCP | Chat |
|---|---|---|---|---|
| Preview what would change | `GET /api/plans/rollup?project=&dry_run=true` | `datawatch plans rollup --dry-run [--project P]` | `plan_rollup` with `dry_run` true | `autonomous plan rollup dry-run [project=P]` |
| Apply it | `POST /api/plans/rollup` `{project?, dry_run?}` | `datawatch plans rollup [--project P]` | `plan_rollup` | `autonomous plan rollup [project=P]` |

A preview never writes: the GET route refuses `dry_run=false` with 400, and a
`POST` with `{"dry_run": true}` (or `?dry_run=true`) previews too. Without a
project every project is rolled up; a project must be a registered alias. Each
change in the answer names the plan (title first, then id), the status it has,
the status it gets and the reason; `changed` counts the plans written (or that
would be) and `kept` the hand-set plans left alone. A plan that becomes closed
gets its closed time, and one that becomes open again loses it. Each write goes
to the audit log as `plan_record_rolled_up` with the actor `rollup`, carrying the
old status, the new one and the reason.

**What it reads.** The roll-up does not ask the running Automata. It reads the
state each link last had on the plan: the state seen when the Automaton or
Automation was attached, and `shipped` when the merge pipeline records the
Automaton as shipped. A link whose Automaton has since failed, blocked or
started running still counts with its old state until it is attached again or
ships. Run a preview before you trust a `blocked` or `open` answer, and attach
the item to the same plan again to refresh the state the plan holds about it.

**When it runs by itself.** Three triggers, and only these:

- The daemon rolls every plan up once a day. The 24 hours count from daemon
  start; there is no run at start, so a daemon restarted more than once a day
  never reaches it. The log line `[plans] daily reconcile changed N plan
  status(es)` appears only when something changed.
- When an Automaton is recorded as shipped, the plan it is linked to rolls up.
  This runs from the write-through registry hook, so it needs
  `autonomous.plan_records.write_through` on (see *What happens when an
  Automaton ships*). An Automaton that is not linked to a plan changes nothing.
- When you call the roll-up yourself, as in the table above.

A plan with no linked work is left alone on every trigger. The roll-up needs
`autonomous:write` for a federated peer to apply and `autonomous:read` to
preview, and answers 404 while plan records are off.
