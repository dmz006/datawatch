---
docs:
  index: true
  topics: [standard tasks, standard task, automaton templates, automaton template, formerly task templates, decomposer, story tasks, per-project override, templates tab, standard-task packs, pack, pack trust, auto-accept, first-party pack source]
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

## Installable packs (backend only, not yet wired)

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

**Status: the rules below exist in the `internal/packs` package, and the config keys
are read, but no daemon surface calls them yet.** There is no REST, MCP, CLI or PWA
action to install, accept or enable a pack, no default install folder, and the
`packs.*` settings are not checked at start-up. Treat this section as the contract the
surfaces will follow, not as a feature you can use today. The open items are listed in
[Open items to check](#open-items-to-check) and in
[operations](../operations.md#standard-task-packs-what-is-wired-and-what-is-not).

### Turning packs on

The `packs` config block has two keys: `packs.enabled`, off by default, and
`packs.active`, the ordered list of packs in use, which defaults to `["builtin"]`.
An entry is `builtin` or the name of an installed pack. The packs library has a check
that packs may be enabled only when at least one skills registry source is connected
and enabled; the community registry is that source by default. The check is not yet
run when you set `packs.enabled: true`.

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

### Pack attribution: traces, audit and cost

Work that runs from a pack records which pack it came from, so you can see what each
pack did after the fact. The attribution is recorded in three places:

- **Trace events** carry `pack_name` and `pack_version`. The trace roll-up can group by
  them: `dimension=pack_name` or `dimension=pack_version` (REST `GET /api/trace/rollup`,
  and the `trace_rollup` MCP tool). See [agent tracing](agent-tracing.md).
- **Audit entries** carry a `pack_name` detail. The audit query accepts a `pack_name`
  filter, an exact match, on REST, MCP, CLI and the comm channel.
- **Cost report** rows can be broken down by pack name and version (one row per pack and
  version, with events, tokens and cost). Events with no pack name fall into one
  `unattributed` row.

The `builtin` pack records `pack_name: builtin` on the trace events of its standard
tasks. Events from before this attribution existed have no pack name and show as
unattributed.

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
- **Packs are not wired.** The install, trust, auto-accept and enable-check rules are in
  the `internal/packs` package only. Nothing calls them from the daemon, so the
  `packs.enabled` switch is not enforced, `packs.active` is not validated, and no surface
  installs or accepts a pack. See the [Installable packs](#installable-packs-backend-only-not-yet-wired)
  section and [operations](../operations.md#standard-task-packs-what-is-wired-and-what-is-not).
- **The trace `pack` filter is not honoured over REST.** The MCP `trace_session` and
  `trace_rollup` tools send `?pack=`, and the REST trace routes ignore it. Operator review
  needed: wire the parameter on the REST routes, or drop it from the MCP tools.
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
