---
docs:
  index: true
  topics: [standard tasks, standard task, automaton templates, automaton template, formerly task templates, decomposer, story tasks, per-project override, templates tab]
exec_params:
  - {name: project, required: false, description: "Project directory or alias: shows the standard-task list as that project sees it (its override applied)"}
exec_steps:
  - tool: autonomous_standard_tasks
    description: List the standard tasks the decomposer appends to a story (id, title, kind, enabled, mode, runner) — a read, writes nothing
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
2. The decomposer checks the story against the standard-task pack.
3. Each standard task the planner did not already produce is appended to the story as a
   new task. Its **mode** decides who carries it out: `llm` (a model session, like any
   other task), `daemon` (the daemon completes it with no model session) or `off` (it is
   not appended at all). See [Mode: who runs each task](#mode-who-runs-each-task).
4. Applying is **idempotent**. A task an earlier apply already added is detected and never
   added twice. The detection uses the `template:<id>` marker in the task title or spec,
   and it also matches a planner task whose title closely resembles the standard task.

Standard tasks are applied in the `shadow` and `enforce` decompose-lint modes. With
`off`, the decomposer applies none.

## The built-in standard tasks

The built-in pack holds six standard tasks. Each one is written to avoid blocking
decompose-lint findings and to have no side effects on the hosting CLI.

| Standard-task id | What it adds | Kind | Default mode |
|---|---|---|---|
| `changelog_entry` | A changelog entry for the feature in `CHANGELOG.md` | `docs` | `daemon` |
| `parity_issue_draft` | A parity-gap draft file under `docs/plans/parity-issues/`. It is a file write only, never a tracker call | `docs` | `daemon` |
| `docs_set` | The feature's docs brought into shape, over the story's own files | `docs` | `llm` |
| `bounded_load_step` | The bounded load-step rule for a load step the plan names. The rule itself is in [Orphan reaping and host load](automata-orphan-reaping.md) | `note` | `daemon` |
| `final_gate` | A **read-only** closing task: the build is clean, tests pass and the files hang together. The daemon's script gate runs it and writes the gate report to `.datawatch/reports/` | `final_gate` | `llm` |
| `version_bump_left_to_merge` | A plain note that the version is the next patch above main, resolved at merge. It never edits a file | `note` | `daemon` |

Each standard task has an `id`, a `title`, a `kind`, a `summary`, an `offered_when`
condition, an effective `enabled` flag, a `mode` and a `runner`. Standard tasks take slots: `{{feature}}`,
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

## Mode: who runs each task

Each standard task has a **mode**, set per template in `autonomous.task_templates.mode`
(a map of template id → mode). The mode says who carries the task out:

| Mode | Meaning |
|---|---|
| `llm` | A worker session runs the task as a normal task, verifier included. For `final_gate` the daemon's script gate performs it instead, so no session is spent |
| `daemon` | The daemon performs the task itself and completes it with no model session, no verifier and no retry loop |
| `off` | The task is not appended to the story at all. For `final_gate`, `off` means the story has no closing gate |

```yaml
autonomous:
  task_templates:
    mode:
      docs_set: llm            # the default; a worker session writes the docs
      changelog_entry: llm     # send the changelog entry through a session instead
      final_gate: off          # no closing gate on any story
```

- **Defaults.** `changelog_entry`, `parity_issue_draft`, `bounded_load_step` and
  `version_bump_left_to_merge` default to `daemon`. `docs_set` and `final_gate` default
  to `llm`. An id you do not list keeps its default; an id outside the built-in pack
  defaults to `llm`.
- **Which modes each id accepts.** `daemon` is accepted only for `changelog_entry`,
  `parity_issue_draft`, `bounded_load_step` and `version_bump_left_to_merge`. `final_gate`
  takes `llm` or `off` only, because it is a read-only gate. Any other value, or `daemon`
  on `final_gate` or `docs_set`, is a validation error and nothing is stored.
- **Global only.** The mode map is not per project. The per-project `projects` map only
  turns standard-task application on or off for a project.

### How to set a mode

| Method | How |
|---|---|
| **Web UI** | Settings → the autonomous card → the per-template **Standard task modes** select. It merges one mode onto the stored map |
| **CLI** | `datawatch config set autonomous.task_templates.mode.<id> <mode>` |
| **Comm channel** | `configure autonomous.task_templates.mode.<id>=<mode>` |
| **REST** | `PUT /api/config` with the dotted key `autonomous.task_templates.mode.<id>` |
| **MCP** | `autonomous_config_set` with the `task_templates_mode` argument, for example `changelog_entry=llm,final_gate=off` |
| **YAML** | The `autonomous.task_templates.mode` block in `~/.datawatch/config.yaml` |

The per-template keys **merge** one mode onto the stored map. The decompose-lint config
surface (REST `PUT /api/autonomous/decompose-lint/config`, its MCP tool, and the
decompose-lint card) **replaces the whole `task_templates` block** when it is sent. If you
write through that surface, read the live config, change the `mode` map in the returned
block, and write the whole block back. See
[Decompose lint, standard tasks and guidance](automaton-decompose-lint.md).

### The runner column

The list reports two fields that describe how a task runs. `mode` is the setting above.
`runner` names what actually carries the task out, as the daemon resolves it:

| `mode` | `runner` | Who does it |
|---|---|---|
| `llm` | `session` | A model session |
| `llm` on `final_gate` | `script gate` | The daemon's script gate, with no session |
| `daemon` | `daemon` | The daemon, with no session |
| `off` | `none` | Nothing. The task is not appended |

So `final_gate` with `mode: llm` shows `runner: script gate`, not `session`.

For the daemon-mode behaviour itself (what a changelog or parity draft writes, and the
script gate's checks and time cap), see
[daemon task modes and the script gate](automaton-daemon-task-modes.md).

## Reading the standard-task list

The list reads the same pack on every surface. Passing a project shows that project's
override applied, so what you read is what the decomposer sees for that project. The
one exception is the comm channel, which always shows the global setting: it takes no
project argument.

| Method | How |
|---|---|
| **YAML** | Edit `~/.datawatch/config.yaml` → `autonomous.task_templates` (the `enabled` switch and the `projects` overrides) |
| **CLI** | `datawatch autonomous standard-tasks [--project <dir-or-alias>] [--json]`. A table of `ID TITLE KIND ENABLED MODE RUNNER`; `--json` prints the raw body |
| **Web UI** | Automata page → the **Templates** tab (labelled **Automaton templates** in the current UI) → **Standard tasks** sub-tab. A read-only table; your sub-tab choice is remembered |
| **REST API** | `GET /api/autonomous/standard-tasks` with an optional `?project=<dir-or-alias>`. The body is `{enabled, tasks[]}`, and each row carries `mode` and `runner`. Any other method returns 405. The config itself is read and written through `GET` and `PUT /api/config` |
| **MCP** | The `autonomous_standard_tasks` tool, with an optional `project` argument. Each row carries `mode` and `runner`. The switches are read and written with `autonomous_config_get` and `autonomous_config_set` |
| **Comm channel** | `standard tasks` (or `autonomous standard-tasks`): one line per task, `id \| title \| on\|off \| mode \| runner`, for the global setting only (no project argument). An empty list replies "no standard tasks are defined." |

All of these render the same listing, so they always agree. The fixture that pins
them is `testdata/standard-tasks/expected.json`.

### What the web sub-tab shows

The **Standard tasks** sub-tab is read-only. It loads the same body as
`GET /api/autonomous/standard-tasks` and draws one row per task: **title**,
**kind**, **summary**, **when it is offered** (the `offered_when` text),
**on or off**, and **mode** (the task's real mode: `llm`, `daemon` or `off`). The
sub-tab has **no runner column** yet, so use the CLI, REST, MCP or comm channel to see
the runner. A note under the table explains the three modes and points at the
`autonomous.task_templates.mode` setting; the per-task selects for that setting live in
**Settings → Daemon → Standard task modes** (one select per built-in task, each saving
its own mode).

- **Empty state.** When there are no tasks to show, the sub-tab says what standard
  tasks are — the tasks the decomposer adds to every story, added when
  `autonomous.task_templates.enabled` is on — instead of drawing an empty table.
- **Load failure.** If the read fails, a "Load failed" line shows where the table
  would be (with the error) and clears any stale list; a failed reload never leaves
  an old list behind.
- **Phone width.** The sub-tab bar stays on one row and scrolls sideways inside its
  own container, with a soft fade on the side that still hides tabs (see
  [Tab strips at phone width](navigation-mobile-fit.md#tab-strips-at-phone-width)).
  Each table row stacks into its labelled cells (title, kind, summary, when, on/off,
  mode) and the body keeps a side gutter, so the page itself never scrolls sideways.
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

## Worked example: turning one standard task off

Say the project writes its changelog with its own release script, so the daemon's
`changelog_entry` write is duplicate work. Turn off just that one standard task and
leave the other five in place:

1. Open **Settings → Daemon → Standard task modes** and set the **Changelog entry**
   select to **off** — or, on the command line:

   ```bash
   datawatch config set autonomous.task_templates.mode.changelog_entry off
   ```

2. Confirm. The **Standard tasks** sub-tab now shows the task's mode as `off`, and the
   CLI read agrees:

   ```bash
   datawatch autonomous standard-tasks --json
   ```

   where its `runner` reads `none` — nothing carries the task out.

3. From the next decomposed story on, `changelog_entry` is not appended to the story at
   all; the other standard tasks are untouched — the save writes only that id's mode onto
   the stored mode map, so `enabled`, the custom templates and the per-project overrides
   are left alone. The mode is global: it turns that one standard task off for every
   project, and a project override only switches whether the pack is applied in that
   project, not its modes. To bring it back, set the mode to `daemon` again — the
   shipped default for that task.

## Open items to check

Flagged here so an operator looking at the web tab knows what it does **not** have
yet:

- **No runner column in the web tab.** The sub-tab shows `mode` but not `runner`, so a
  `final_gate` on `llm` looks like a session task there. Read `runner` on the CLI, REST,
  MCP or comm channel.
- **No per-project selection in the web tab.** The sub-tab reads the global list; the
  per-project resolved read (the `?project=` parameter) is on REST, MCP and CLI. The
  comm channel reads the global setting only. If you rely on a project override, read
  the list with `--project` (CLI) or `project` (MCP or REST) to see the resolved state.
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
- **You want to change how a standard task runs.** Set its mode with
  `autonomous.task_templates.mode.<id>` (see [Mode: who runs each task](#mode-who-runs-each-task)).
  Read `runner` afterwards to confirm what carries it out.
- **A `daemon` or `off` value is refused.** `daemon` is accepted only for the four
  daemon-capable ids, and `final_gate` takes `llm` or `off` only. The write fails with a
  validation error and stores nothing.
