---
docs:
  index: true
  topics: [standard tasks, standard task, automaton templates, automaton template, formerly task templates, decomposer, story tasks, per-project override, templates tab, standard-task packs, pack, pack inputs, secret input, pack_inputs, pack trust, auto-accept, first-party pack source]
exec_params:
  - {name: project, required: false, description: "Project directory or alias: shows the standard-task list as that project sees it (its override applied)"}
exec_steps:
  - tool: autonomous_standard_tasks
    description: List the standard tasks the decomposer appends to a story (id, title, kind, enabled, mode) — a read, writes nothing
    args: {}
  - tool: autonomous_config_get
    description: Read the autonomous config, including the autonomous.task_templates block (enabled and the per-project overrides)
    args: {}
  - tool: autonomous_template_list
    description: List the Automaton templates in the template store — a different thing from a standard task — a read, writes nothing
    args: {}
---

# Standard tasks

A **standard task** is a built-in task definition that the decomposer appends to a
story when the plan left that task out. Standard tasks cover the chores most features
need anyway: a changelog entry, a parity-issue draft, the feature's docs, the bounded
load-step rule, a closing read-only gate, and a version note for merge.

A standard task is **not** an Automaton template. The two are easy to confuse because
both appear on the Automata page's **Templates** tab, and both are reusable definitions
with a name, a title and a spec. The table in
[How a standard task differs from an Automaton template](#how-a-standard-task-differs-from-an-automaton-template)
sets them apart.

This page covers standard tasks. For the decompose-lint pass that runs alongside them,
see [Decompose lint, standard tasks and guidance](automaton-decompose-lint.md).

## What a standard task does

When a story is decomposed:

1. The planner writes the story's tasks.
2. The decomposer checks the story against the standard-task list. That list is built from the
   active packs by the pack resolver (see [How the list is resolved](#how-the-list-is-resolved)).
3. Each standard task the planner did not already produce is appended to the story as a
   new task, in the `model session` mode. A model session does the work like any other task.
4. Applying is **idempotent**. A task an earlier apply already added is detected and never
   added twice. The detection uses the `template:<id>` marker in the task title or spec,
   and it also matches a planner task whose title closely resembles the standard task.

Standard tasks are applied in the `shadow` and `enforce` decompose-lint modes. With
`off`, the decomposer applies none.

## The built-in standard tasks

The built-in pack holds six standard tasks. Each one is written to avoid blocking
decompose-lint findings and to have no side effects on the hosting CLI.

| Standard-task id | What it adds | Kind |
|---|---|---|
| `changelog_entry` | A changelog entry for the feature in `CHANGELOG.md` | `docs` |
| `parity_issue_draft` | A parity-gap draft file under `docs/plans/parity-issues/`. It is a file write only, never a tracker call | `docs` |
| `docs_set` | The feature's docs brought into shape, over the story's own files | `docs` |
| `bounded_load_step` | The bounded load-step rule for a load step the plan names. The rule itself is in [Orphan reaping and host load](automata-orphan-reaping.md) | `note` |
| `final_gate` | A **read-only** closing task: the build is clean, tests pass and the files hang together. It writes the gate report to `.datawatch/reports/` | `final_gate` |
| `version_bump_left_to_merge` | A plain note that the version is the next patch above main, resolved at merge. It never edits a file | `note` |

## How the list is resolved

The standard-task list is produced by the pack resolver (`packs.ResolveTaskList`, in
`internal/packs/resolve.go`). The resolver takes an Automaton kind and the ordered list of
active packs, and it returns the tasks of every pack whose `kinds` include that kind:

- **Order.** Packs are walked in the order they are given. Each pack's tasks are appended
  after the tasks already collected, so the list keeps manifest order within a pack.
- **Kind filter.** A pack that does not list the kind contributes nothing to that list.
- **Duplicate titles are refused.** Two tasks with the same title are an error, and the
  error names both packs that contributed them. A list is never built with a silent
  duplicate.
- **Provenance.** Each resolved task records the pack name and pack version it came from,
  so any row on any surface can be traced to its source pack.

Today the built-in list is resolved **for the `feature` kind only**, from the built-in pack
alone. The built-in pack declares all four kinds (`feature`, `bug`, `other`, `maintenance`),
but the resolved list is not filtered by kind again when it is applied, so the same six
standard tasks are appended to a story whatever its Automaton kind. An operator who wants a
different list per kind should review this; see [Open items to check](#open-items-to-check).

Each standard task has an `id`, a `title`, a `kind`, a `summary`, an `offered_when`
condition and an effective `enabled` flag. Standard tasks take slots: `{{feature}}`,
`{{files}}` and `{{files_list}}` are filled from the feature name and the union of the
story's task files.

## Turning standard tasks on or off

There are two switches. The global switch covers every project. The per-project switch
overrides it for one project.

### Global switch

`autonomous.task_templates.enabled` is the global gate. It defaults to **`true`**. When it
is `false`, no standard task is applied to any story.

### Per-project override

`autonomous.task_templates.projects` maps a **project directory or alias** to an
`enabled` override. The override wins for that project. If a project has no entry, or
its entry has no `enabled` flag, it follows the global switch.

```yaml
autonomous:
  task_templates:
    enabled: true          # global: standard tasks on for every project
    projects:
      "/srv/projects/web":
        enabled: false     # off for this project only
      "billing":
        enabled: true      # on, even if the global switch were off
```

Your own entries in `autonomous.task_templates.templates` overlay the built-in pack. A
new id appends a standard task, and the same id replaces the built-in one.

### Mode setting: not yet available

Each standard task runs in the `model session` mode. A per-task or per-project **mode**
setting, which would let you choose another mode, is **not yet available**. Every
standard task reports `mode: model session` on every read, and there is no setting that
changes it.

## Reading the standard-task list

The list reads the same resolved pack on every surface. Passing a project shows that
project's override applied, so what you read is what the decomposer sees for that
project.

| Method | How |
|---|---|
| **YAML** | Edit `~/.datawatch/config.yaml` → `autonomous.task_templates` (the `enabled` switch and the `projects` overrides) |
| **CLI** | `datawatch autonomous standard-tasks [--project <dir-or-alias>] [--json]`. A table of `ID TITLE KIND ENABLED MODE`; `--json` prints the raw body |
| **Web UI** | Automata page → the **Templates** tab (labelled **Automaton templates** in the current UI) → **Standard tasks** sub-tab. A read-only table; your sub-tab choice is remembered |
| **REST API** | `GET /api/autonomous/standard-tasks` with an optional `?project=<dir-or-alias>`. The body is `{enabled, tasks[]}`. Any other method returns 405. The config itself is read and written through `GET` and `PUT /api/config` |
| **MCP** | The `autonomous_standard_tasks` tool, with an optional `project` argument. The switches are read and written with `autonomous_config_get` and `autonomous_config_set` |
| **Comm channel** | `standard tasks` (or `autonomous standard-tasks`): one line per task, `id \| title \| on\|off \| mode`. An empty list replies "no standard tasks are defined." |

All of these render the same listing, so they always agree.

### What the web sub-tab shows

The **Standard tasks** sub-tab is read-only. It loads the same body as
`GET /api/autonomous/standard-tasks` and draws one row per task: **title**,
**kind**, **summary**, **when it is offered** (the `offered_when` text),
**on or off**, and **mode** (always `model session` — a note under the table says
the mode is fixed for now and links back to this page).

- **Empty state.** When there are no tasks to show, the sub-tab says what standard
  tasks are — the tasks the decomposer adds to every story, added when
  `autonomous.task_templates.enabled` is on — instead of drawing an empty table.
- **Load failure.** If the read fails, a "Load failed" line shows where the table
  would be (with the error) and clears any stale list; a failed reload never leaves
  an old list behind.
- **Phone width.** The sub-tab bar wraps inside its own container, each row stacks
  into its labelled cells (title, kind, summary, when, on/off, mode) and the body
  keeps a side gutter, so nothing scrolls sideways.
- **Choice persisted.** The sub-tab you leave it on (Automaton templates or
  Standard tasks) is the one it opens on next time; a fresh install with no stored
  choice opens **Automaton templates**.

## How a standard task differs from an Automaton template

Both kinds of thing are reusable definitions, and both are shown under the Automata
page's **Templates** tab. They do different jobs.

| | Standard task | Automaton template |
|---|---|---|
| What it is | A built-in task definition the decomposer appends to a story | A reusable Automaton definition: a complete spec (type, stories, tasks, backend, effort, model, skills) stored in the template store |
| Where it lives | The built-in pack, plus your overlay in `autonomous.task_templates` | The template store |
| Where it shows up | Automata → **Templates** → **Standard tasks** sub-tab | Automata → **Templates** → **Automaton templates** sub-tab |
| What uses it | The decomposer, while it decomposes a story | An operator, who starts a new Automaton from it |
| Creates | One task inside a story | A whole Automaton |
| Switched by | `autonomous.task_templates.enabled` and `autonomous.task_templates.projects` | Nothing to switch. You create, edit or delete it |
| Read on REST | `GET /api/autonomous/standard-tasks` | `GET /api/autonomous/templates` |
| Read on MCP | `autonomous_standard_tasks` | `autonomous_template_list` |
| Read on CLI | `datawatch autonomous standard-tasks` | `datawatch autonomous templates`, and `datawatch autonomous template-get <id>` for one |

A simple test: if it is added to a story by the decomposer, it is a standard task. If it
is a complete Automaton you start from, it is an Automaton template.

For Automaton templates, open the Automata tab's **Templates** tab and its
**Automaton templates** sub-tab. The **Start from template** strip on the Automaton create form and the **Clone to Template**
action both use it.

<a id="installable-packs-backend-only-not-yet-wired"></a>

## Installable packs (manage them on every surface; not yet applied to tasks)

A **pack** is a named, versioned folder that ships standard tasks and Automaton
templates together: the task prompts, their verification scripts and the template
definitions. The folder holds a `pack.yaml` manifest at its root. The built-in
standard tasks are the first pack, named `builtin`. Its task and template definitions
are embedded in the binary from the pack folder `internal/packs/builtinpacks/builtin/`
(`go:embed`, as data files, not Go literals). On first use (the result is then cached) `packs.LoadBuiltin` writes that
folder out and loads it through the same path an installed pack takes, and
`internal/autonomous/task_templates_defaults.go` resolves it with the pack resolver. So
`builtin` is not a folder the operator installs or edits, and it needs no install. Packs
from the community registry use the same path.

**The repository's pack folders are test fixtures.** Two folders under
`internal/packs/testdata/` are used only by the tests and are not embedded in the binary:

- `internal/packs/testdata/standard-tasks-pack/` holds a loadable copy of the standard
  tasks in the pack folder format: a `pack.yaml`, six prompts, five verification scripts
  and a `templates/README.md` that lists the Automaton templates.
- `internal/packs/testdata/fixture-one-kind/` is a one-task fixture. Its `pack.yaml` names
  only the `feature` kind and adds one extra task, "Extra check". The tests use it to
  confirm that the resolver adds a task for the kind a pack names and leaves the other
  kinds unchanged.

The real standard-tasks pack is published in the community registry repository
(operator decision, 2026-11-09); the fixtures do not replace it, and installing either
fixture is not a way to get the standard tasks on a daemon.

**Status: you can manage packs on every surface, but the active list does not yet change the
tasks an Automaton gets.** Installing, accepting, removing, enabling and setting inputs all
work (see [Managing packs](#managing-packs-on-each-surface)). What is **not** wired yet:
the resolver is not given the result of `packs.ActivePacks`, so `packs.active` and an
installed pack do not yet change the standard-task list, and stored pack inputs are not
read when a task is created. The open items are listed in
[Open items to check](#open-items-to-check) and in
[operations](../operations.md#standard-task-packs-what-is-wired-and-what-is-not).

### Managing packs on each surface

All surfaces call the same REST routes (`/api/packs`), so they behave alike. Reads need the
config-read permission and writes the config-write permission, including over federation.

| Action | REST | MCP tool | CLI (`datawatch packs ...`) | Chat command | Web |
|---|---|---|---|---|---|
| List installed packs and trust state | `GET /api/packs` | `packs_list` | `list` | `packs` | not in Settings |
| Read the setting | `GET /api/packs/config` | `packs_setting_get` | not a verb; use `list` | `packs setting` | Settings > General > Standard task packs |
| Set `enabled` / `active` | `PUT /api/packs/config` | `packs_setting_set` | `enable`, `disable` | `packs setting enabled=true active=a,b` | the two fields in that card |
| Install a folder | `POST /api/packs/install` | `packs_install` | `install <source-dir>` | `packs install <source_dir>` | none |
| Accept a pack | `POST /api/packs/accept` | `packs_accept` | `accept <name> <version>` | `packs accept <name> <version>` | none |
| Remove a pack | `DELETE /api/packs/{name}/{version}` | `packs_remove` | `remove <name> <version>` | `packs remove <name> <version>` | none |
| Read / replace inputs | `GET` / `PUT /api/packs/inputs` | `packs_inputs_get`, `packs_inputs_set` | `inputs get`, `inputs set` | `packs inputs get` / `set` | the four input fields |

- **Install root.** Packs install to `<data dir>/packs/<name>/<version>/`, beside
  `trust.json` and `inputs.json`.
- **Setting writes are checked.** `packs.active` is refused (HTTP 400) when it holds an
  empty name, a repeat, or a name that is neither `builtin` nor an installed pack.
  Turning `enabled` from off to on is refused (HTTP 409) with "packs need at least one
  connected skills registry source" when no registry source is connected and enabled. The
  web switch is put back to off and shows the refusal.
- **Accept** records the operator's acceptance of the installed digest as accepted by
  `operator`. A pack that is not installed gives HTTP 404.
- **Remove** deletes one installed version and its trust record. It does not touch
  Automata, their tasks or trace records: an Automaton keeps the tasks it was created with.
  The trust record goes first, so a failed delete leaves a pack that is on disk but no
  longer runnable. Removing does **not** edit `packs.active`; a removed name stays in the
  list until you change it, and the next write of that list is then refused.
- **Inputs** are stored per pack at the `global`, `project` (key is the project
  directory) or `automaton` (key is the Automaton id) level. A save **replaces** that
  scope. Every name must be an input the pack declares, and a secret input accepts only a
  `${secret:NAME}` reference. Reads show secrets as references or `***`.
- **Web inputs** take the `global` or `project` level only; the web form has no Automaton
  level. Use REST, MCP, CLI or chat for Automaton-level inputs.
- **Not on the web yet:** install, accept, remove and list. Use the CLI, chat command,
  MCP or REST for those.

### Turning packs on

The `packs` config block has two keys: `packs.enabled`, off by default, and
`packs.active`, the ordered list of packs in use, which defaults to `["builtin"]`.
An entry is `builtin` or the name of an installed pack. The packs library has a check
that packs may be enabled only when at least one skills registry source is connected
and enabled; the community registry is that source by default. The check is
`packs.CheckEnable` (`internal/packs/enable_gate.go`): it looks only at the list of
registry sources it is given and refuses with "packs need at least one connected skills
registry source; connect one in the skills registry settings" when none is enabled. The
daemon calls it when `packs.enabled` is changed from off to on through REST, MCP, CLI,
chat or the web switch, so the switch is refused when no source is connected. Editing the
config file by hand skips the check.

### Which packs apply to an Automaton

`packs.ActivePacks` (`internal/packs/order.go`) returns the ordered pack list for one
Automaton:

- **Packs off.** When `packs.enabled` is `false` the answer is always `["builtin"]`,
  whatever any list says.
- **First defined list wins.** When packs are on, the Automaton's own list is used if it
  has entries; otherwise the project's list; otherwise the global `packs.active`. The
  lists are **not merged**.
- **Nothing defined.** When all three are empty the answer is the global default,
  `["builtin"]`.

An Automaton's own list is the `packs` field on the Automaton (JSON `packs`, omitted
when empty). It is assigned with `Automation.SetPacks`, which trims each name, drops
empty names and later repeats, and keeps the order given (it is a priority order and is
never sorted). **Not wired yet:** no REST, MCP, CLI, chat or web call sets the field, and
the resolver is not yet given the result of `ActivePacks`. A per-project pack list was
planned and is **not** in the config today: the project entry has no `packs` key, so
`ActivePacks` has no project list to read until one is added. Operator review needed:
confirm that the project list belongs on the project entry in config.

### What a pack needs to pass

- **Install checks every task first.** Each task is checked against the same
  standard-task rules as the built-in tasks. The first task that fails refuses the
  whole install, names the rule, and nothing is copied.
- **Names are single folder names.** An unsafe pack name is refused.
- **A version is never overwritten.** Installing a name and version that already exists
  is refused. Install a new version under a new `version` in `pack.yaml`.
- **Links stay inside the pack.** A symbolic link is followed only when it resolves to a
  regular file inside the pack folder. Any other link refuses the install.

An installed pack is copied to `<install root>/<name>/<version>/`.

### Trust: who accepted a pack, and what changed

A pack from a **first-party source** runs without a per-pack record. A first-party source
is a registry flagged `first_party`, and the community registry
(`dmz006/datawatch-community`) is flagged so. Its packs are read from the registry's
git clone; they are never copied into the binary.

Every other pack runs only when the install root's `trust.json` holds a record of an
operator's acceptance for that pack's **name, version and exact content digest**. A
record names the pack, the version, the digest, who accepted it and when. If a single
file in the pack changes, the digest changes and the old record no longer applies. A
pack that has not been accepted is refused with `pack not accepted: accept it first`.

For a first-party source, acceptance covers the versions that are current when the
operator accepts the source. That call writes one record per current pack, and it
refuses the whole source if any pack is missing its digest or belongs to another source.
A later version has no record and goes through the version-diff rules below.

### Version diff and auto-accept

When a new version of a pack appears, the difference from the accepted version is
listed as **new tasks, new tools, new scripts and new file scopes**. Only a new tool,
script or file scope **widens access**. New tasks alone do not.

A registry source can opt in to **auto-accept** with `auto_accept_packs`. It is off by
default. When it is on, a new version is accepted automatically, recorded as accepted by
`auto:<source name>`, **only** when the diff adds no tool, script or file scope. A version
that widens access is never auto-accepted; the operator accepts it by hand.

### Summary of a pack

A pack summary lists the task titles in manifest order, the names of the verification
script files (never their full paths), and the tools and file scopes the pack's tasks
ask for. Use it to read what a pack would allow before you accept it.

### Pack inputs: how values resolve, secrets, and what a task records

A pack can declare **inputs** in `pack.yaml` (`name`, `type`, `default`, `required`,
`secret`). The resolver (`packs.ResolveInputs`, `internal/packs/inputs.go`) gives each
declared input one value:

- **Override order.** The first level that defines the input wins: the Automaton, then
  the project, then the global level. A level defines an input when its map holds the
  input's name, so an empty string set at a level still wins over a lower level.
- **Default.** When no level defines it, the declared `default` is used. An input that is
  not required and has no default resolves to an empty string.
- **Required.** A required input with no value at any level and no default is an error:
  `pack input <name> is required and has no value; set it at the automaton, project or global level`.

**Secret inputs** (`secret: true`) never hold a literal value. `packs.CheckSecretInput`
refuses any value that is not a vault reference of the form `${secret:NAME}` (`NAME` is
letters, digits, `_`, `.` and `-`). Non-secret inputs accept any value. `packs.RedactInputs`
returns the copy that is safe to show: a secret input keeps its `${secret:NAME}` reference,
and a secret input whose value is not a reference is masked as `***`.

**What a created task records.** When the daemon creates a daemon-run standard task it
resolves the built-in pack's inputs, redacts them, and stores the result on the task as
`pack_inputs` (JSON, omitted when empty). The map is a private copy: a later change to an
input does not alter a task that already exists, because a task keeps the values it was
created with. If resolving fails, the error is logged and the task is created without
`pack_inputs`.

Limits today, flagged for review:

- **Inputs can be saved but are not read.** REST, MCP, CLI, chat and the web settings
  fields write inputs to `inputs.json` at any level (the web at global and project
  level), but the daemon still creates tasks with no Automaton, project or global values
  (`RecordPackTaskInputs` is called with empty maps), so only declared defaults apply
  and a saved value changes nothing yet.
- **The built-in pack declares no inputs**, so saving an input for `builtin` is refused
  (the pack declares no input of that name) and `pack_inputs` is empty on every task the
  daemon creates today.
- **Secrets are checked at write time.** The save refuses a literal value for a secret
  input; only `${secret:NAME}` is accepted.
- **Only daemon-run tasks record inputs.** Tasks that run as a model session do not.

### Pack attribution: traces, audit and cost

Pack attribution is the read side of a pack's name and version on trace events and
audit entries. **Today nothing writes it on a running daemon**, so the filters and
roll-ups below return no pack-attributed rows yet. The pieces are:

- **Trace events** have `pack_name` and `pack_version` fields. The trace roll-up can
  group by them: `dimension=pack_name` or `dimension=pack_version` (REST
  `GET /api/trace/rollup`, and the `trace_rollup` MCP tool). See
  [agent tracing](agent-tracing.md). The helper that stamps the `builtin` pack onto a
  trace event (`autonomous.StampBuiltinPack`) exists, and
  `autonomous.BuiltinStandardTaskTraceEvent` builds an event for one built-in standard task
  (its `task_id` and name come from the task, `pack_name` is `builtin`). No production
  code calls either, so the standard tasks' events are not stamped yet.
- **Audit entries are read-only for `pack_name`.** The audit query accepts a `pack_name`
  filter, an exact match, on REST, MCP, CLI and the comm channel, and it reads the
  `pack_name` detail. No production code writes a `pack_name` detail on an audit entry,
  so a `pack_name` filter returns nothing until a writer ships. Do not expect filtered
  audit results from it yet.
- **Cost report** rows can be broken down by pack name and version (`costreport`'s pack
  breakdown: one row per pack and version, with events, tokens and cost). Events with no
  pack name fall into one `unattributed` row. No surface calls the breakdown yet.

Events and entries written before attribution have no pack name and show as
unattributed. Once a writer is wired, the `builtin` pack will record `pack_name: builtin`
on the trace events of its standard tasks.

The MCP tools `trace_session` and `trace_rollup` accept an optional `pack` argument. The
MCP tool forwards it as `?pack=`, but **the REST trace routes do not read that parameter
yet**, so over REST and through the MCP tools the argument currently has no effect.
Filter by `dimension=pack_name` and `key=<pack>` on the roll-up instead until it is
wired.

A per-pack usage summary (runs, cost and last use per pack name and version, counted per
session, task or Automaton) is computed by `packs.UsageByPack`. **No daemon surface calls
it yet**, so the Packs sub-tab does not show it.

## Open items to check

Flagged here so an operator looking at the web tab knows what it does **not** have
yet:

- **Mode setting.** The per-task/per-project mode choice is not available yet; every
  standard task runs in the `model session` mode (see the section above).
- **No per-project selection in the web tab.** The sub-tab reads the global list; the
  per-project resolved read (the `?project=` parameter) is on REST, MCP, CLI and the
  comm channel. If you rely on a project override, read the list with `--project`
  to see the resolved state.
- **The built-in list ignores the Automaton kind when applied.** The built-in list is
  resolved for the `feature` kind only, and the apply step does not filter by kind, so
  bug, other and maintenance Automata receive the same standard tasks. Operator review
  needed: decide whether the list should be resolved and applied per kind.
- **Mode wording to review.** The **Mode setting** bullet above says every standard task
  runs as a model session. The glossary's **Standard task** entries describe per-template
  modes (`llm`, `daemon`, `off`) with notes and changelog defaulting to `daemon`. Confirm
  which describes the running daemon before relying on either.
- **Per-project pack list is missing.** `packs.ActivePacks` takes a project list, but the
  project config entry has no `packs` key (it was removed from the change as out of
  scope). Only the Automaton's own list and the global `packs.active` can exist today.
  Operator review needed: decide whether to add the project key and where it is edited.
- **Packs are managed but not applied.** Install, accept, remove, the enable check, the
  `packs.active` validation and inputs work on every surface except as listed in
  [Managing packs](#managing-packs-on-each-surface). The resolver is still not given
  `packs.active`, so an installed pack does not yet add tasks, and nothing calls the
  auto-accept or first-party acceptance rules from the daemon. See the
  [Installable packs](#installable-packs-manage-them-on-every-surface-not-yet-applied-to-tasks)
  section and [operations](../operations.md#standard-task-packs-what-is-wired-and-what-is-not).
- **The web has no list, install, accept or remove.** Only the setting and the inputs are
  in Settings. Operator review needed: decide where the installed-pack list and actions
  live in the web UI, and whether the web inputs form needs an Automaton level.
- **Removing a pack does not update `packs.active`.** A removed name stays in the list
  until edited. Operator review needed: decide whether remove should refuse while the
  pack is active, or drop it from the list.
- **The trace `pack` filter is not honoured over REST.** The MCP `trace_session` and
  `trace_rollup` tools send `?pack=`, and the REST trace routes ignore it. Operator review
  needed: wire the parameter on the REST routes, or drop it from the MCP tools.
- **Saved pack inputs are not read.** Inputs can be set at the Automaton, project and
  global levels, but task creation passes empty maps, and the built-in pack declares none.
  Operator review needed: decide when the daemon reads `inputs.json` when it creates a
  task, and whether `pack_inputs` should be shown on the task in the web UI and the REST
  task read.
- **The per-pack usage summary and the pack cost breakdown have no surface.**
  `packs.UsageByPack` and the cost report's pack breakdown are library functions only.
  The Packs sub-tab named in the change does not call them yet. Decide where they show
  (the Packs sub-tab, the cost report or both) before they are documented as user-facing.
- **The datawatch app does not show this tab yet, and does not yet use the new
  wording.** Two parity drafts are tracked under `docs/plans/parity-issues/`
  (the Templates-tab surface, and the standard-tasks rename on Android/iPhone).
  They are drafts, not yet filed: the operator opens them on the app tracker when
  ready.

## Troubleshooting

- **A standard task is missing from a story.** Check that the global switch is on and
  that the project's override does not turn it off. Read the list with `--project` to see
  the resolved state. Check the decompose-lint mode too: with `off`, nothing is applied.
- **You want a template to start a new Automaton.** That is an Automaton template. Open
  Automata → **Templates** → **Automaton templates**, not the **Standard tasks** sub-tab.
- **You want to change how a standard task runs.** The mode is fixed at `model session`
  for now. The mode setting is not yet available.
