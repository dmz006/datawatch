---
docs:
  index: true
  topics: [automata, autonomous, task, routing, llm, model, profile, verification]
exec_steps:
  - tool: task_routing_get
    description: Read the task routing table - whether routing is on, the active profile, every profile's rows, the task types and the validation result
    args: {}
    read_only: true
  - tool: task_routing_profile_list
    description: List the saved routing profiles and show which one is active
    args: {}
    read_only: true
  - tool: task_routing_automaton
    description: For one Automaton, show each task's type, how it was classified, which row decided its LLM (or why none did) and the LLM it will run with
    args: {id: "<automaton-id>"}
    read_only: true
  - tool: task_routing_dry_run
    description: Preview what the active (or a named) profile would assign to each task of a draft Automaton; nothing is written, logged or run
    args: {id: "<automaton-id>", profile: ""}
    read_only: true
  - tool: task_routing_profile_export
    description: Export one profile as a JSON document you can import on another server
    args: {name: "default"}
    read_only: true
  - tool: task_routing_profile_save
    description: Save a profile under a name (copies the active profile's table when no rows are given)
    args: {name: "my-profile"}
    read_only: false
  - tool: task_routing_profile_activate
    description: Make a saved profile the active one; only tasks created afterwards use it
    args: {name: "my-profile"}
    read_only: false
  - tool: task_routing_set
    description: Turn routing on or off, or replace the profiles; the result is validated first and a bad table is refused
    args: {enabled: true}
    read_only: false
---

# Automata task routing: the right LLM for each task

When an Automaton is decomposed, every task it gets needs a worker LLM (and
optionally a verification LLM). By default a task uses the Automaton's LLM, or
the global default. **Task routing** adds a table that maps each *task type* (a
documentation task, a locale-string task, a daemon code task and so on) to a
backend and model.

The reason: most of the work in an Automaton is of two kinds. Mechanical work
(documentation, changelog text, locale strings, formatting, tests, version
notes, bounded load steps) is cheap to get right, so it can go to
`pi-prompt` with a local Qwen model. Code work (UI code, daemon and backend
code, MCP and API surface, merge conflict repair, review repair) needs a
stronger model, so it goes to `claude-code` with Sonnet. Verification follows
the **global verifier policy** unless a row names its own verifier. Without a
table you pick this by hand, task by task.

Task routing is **off by default**. Nothing changes for you until you turn it
on. The backends and models in the shipped table are one operator's starting
choice, not a rule; other users change them freely.

Out of scope: task routing decides the LLM when a task is created. It does not
move a task that is already waiting to a different backend while the Automaton
runs.

## The 14 task types

| Task type | What it covers |
|---|---|
| `docs_set` | Documentation: how-tos, definitions, reference |
| `changelog_entry` | A changelog entry |
| `parity_issue_draft` | A draft of a parity issue |
| `bounded_load_step` | A small, fixed-scope load or data step |
| `version_note` | A version note or version bump text |
| `locale_strings` | Translation and locale string files |
| `tests` | Writing or fixing tests |
| `formatting_cleanup` | Formatting and tidy-up with no behavior change |
| `ui_code` | Web app and other UI code |
| `daemon_backend_code` | Daemon and backend code (also the default type) |
| `mcp_api_surface` | MCP tools, REST routes and their parity |
| `merge_conflict_repair` | Resolving a merge conflict |
| `final_gate` | The final gate or a smoke task |
| `review_repair` | Fixing what a review or verification found |

## The shipped defaults

The shipped `default` profile:

| Task types | Worker backend | Worker model |
|---|---|---|
| `docs_set`, `changelog_entry`, `parity_issue_draft`, `locale_strings`, `formatting_cleanup`, `tests`, `version_note`, `bounded_load_step` | `pi-prompt` | `ollama/qwen3.8:27b` |
| `ui_code`, `daemon_backend_code`, `mcp_api_surface`, `merge_conflict_repair`, `review_repair` | `claude-code` | `sonnet` |
| `final_gate` | empty row, so the default LLM applies | |

Every verification field is empty, so verification follows the global verifier
policy. No shipped row names an Opus or Fable model. A config with no
`task_routing` block gets this table with routing off.

## How a task is classified

Both paths use the same classifier: the planner when it decomposes an
Automaton, and the add-task path (add a task by hand on any surface).

1. The task's **template kind**. A smoke or final-gate task is `final_gate`,
   and a task made from a template keeps that template's type.
2. Then ordered keyword rules over the **title and the spec**.
3. Then the default type, `daemon_backend_code`.

A task that lists a non-test Go file under `internal/` is never classified as a
mechanical type; the tie goes to the code type.

The task records **how** it was classified: `template_kind`, `title_spec` or
`default`. That record is what you read back to see why a task got a type.

## What always wins over the table

The most specific choice wins: a task LLM set by an operator, then a story LLM,
then an Automaton LLM, then the routing table, then the global default. The
table applies only where none of the others is set. A hand-set task, story or
Automaton LLM always wins.

The table never touches:

- a task that was **edited by hand** (edit task, set task LLM or set task
  verification LLM by anyone other than the router). A planner re-run never
  overwrites a hand-edited task;
- a task that has started, finished or has a live session. Applying never
  kills, restarts or signals a session.

A planner re-run re-applies the table only to tasks the router itself routed
and nobody edited.

## Rows, profiles and the second choice

A **row** holds a worker backend and model, an optional verification backend and
model (empty means the global verifier policy), an effort, an optional **second
choice** and `allow_premium_model`. The second choice is the alternative
backend and model used when the first choice is refused; the routing result
records whether it was used.

A **profile** is a named table of rows. One profile is the **active profile**.
You can keep several, for example a cheap profile for a quiet week and a
stronger one for release work.

- **Save** a profile under a name. With no rows it copies the active profile's
  current table; with rows it stores those. A profile of the same name is
  replaced.
- **Activate** a saved profile. It takes effect **only on tasks created
  afterwards**. Tasks already created keep what they have and running tasks
  are untouched.
- **Delete** a profile. The active profile cannot be deleted.

## Export, import and matrix results

A profile moves between servers as a JSON file: `format_version`, `name` and
`rows`. Export writes it, import reads it. An import is validated and refused
with every problem listed if it is not valid. Pass a name to store it under a
different name, and set overwrite to replace a profile that already has the
name.

To turn a **matrix test result** into a profile: take the winning backend and
model for each task type from the matrix result, write them as rows (one row per
task type, with `worker_backend` and `worker_model`, and a `second_choice` for
the runner-up), wrap them in the file shape above or pass them as the rows of
a profile save, then import or save it. Check it with a dry run on a draft
Automaton before you activate it. Name an Opus or Fable model only on a row
that sets `allow_premium_model: true`.

## Refusal rules

A block is checked before it is used and a bad one is rejected whole, with
every problem listed and nothing applied:

- the active profile must exist;
- every row's task type must be one of the 14;
- an **unknown LLM is refused**: every backend must be known to the LLM
  registry, and every model must pair with its backend (the same rule the
  task and story LLM setters use);
- a model with no backend is refused;
- a row naming an **Opus or Fable model is refused** unless that row sets
  `allow_premium_model: true`. Opus and Fable are never used unless a row
  names one.

### When a config write is refused

A routing change made as a config key (`autonomous.task_routing` for the whole
block, or `autonomous.task_routing.active_profile` and `.enabled` on their own)
goes through `PUT /api/config`. The whole block is checked against the live LLM
registry first, so an unknown backend or model is refused there too, and the
reply names the profile and row at fault. A key that fails is **skipped, not
stored**, and the rest of the change still applies. The reply is still HTTP 200,
so read it:

- `skipped` lists the keys that were not stored (for example
  `autonomous.task_routing`, or `autonomous.task_routing.active_profile` when
  the named profile does not exist);
- `skipped_reason` says why;
- `skipped_problems` lists every problem found, each naming its row, such as
  `profile "p" row "final_gate"` and what is wrong with it.

Every surface that writes config shows that report in place of a confirmation:
`config_set` on MCP returns an error, `datawatch config set` exits with an
error and the chat `configure` verb replies with it. Each line begins
`not stored:`. A refused block leaves the live table exactly as it was. The
dedicated routing routes (`PUT /api/autonomous/task-routing`, profile save and
import) refuse a bad table with an HTTP 400 instead.

## Seeing what was decided

Each task records a routing result: its type, the profile and row used, how it
was classified, what was chosen, whether the second choice was used, or the
reason nothing was applied (for example "task LLM set by operator", "story LLM
set", "Automaton LLM set", "routing disabled", "hand edited", "task already
started").

The same outcome is written to the Automaton's decision log as one
`task_routing` entry by the `task-router` actor. The note is one line:

```
task=<id> type=<type> profile=<profile> row=<row> worker=<backend>/<model> effort=<effort> verification=<backend>/<model> second_choice=<true|false>
```

Two reads are available per Automaton:

- **The per-Automaton read** lists every task with its type, how it was
  classified, the row that decided its LLM (or why none did) and the LLM it
  will run with. It writes nothing.
- **The dry run** takes a draft Automaton and optionally a profile name, and
  shows what that profile would assign to each task. It writes nothing, logs
  no decision and calls no LLM. Run it before you approve a draft or switch
  profiles.

## Access from every surface

| Surface | How |
|---------|-----|
| YAML | Edit the `autonomous.task_routing` block in `datawatch.yaml` (see `docs/config-reference.yaml`), then reload. |
| REST API | `GET` and `PUT /api/autonomous/task-routing` read and replace the table (the read includes its validation result). `GET` and `POST /api/autonomous/task-routing/profiles` list and save profiles; `PUT .../active-profile` switches; `DELETE .../profiles/{name}` deletes; `GET .../profiles/{name}/export` and `POST .../profiles/import` move a profile as JSON (`?name=` and `?overwrite=true` on import). `GET /api/autonomous/prds/{id}/task-routing` is the per-Automaton read and `POST .../prds/{id}/task-routing/dry-run` is the no-write dry run. `PUT /api/config` with `autonomous.task_routing.*` still works, with the validation and `skipped` report described under Refusal rules. |
| MCP | `task_routing_get`, `task_routing_set`, `task_routing_profile_list`, `task_routing_profile_save`, `task_routing_profile_activate`, `task_routing_profile_delete`, `task_routing_profile_export`, `task_routing_profile_import`, `task_routing_automaton` and `task_routing_dry_run`. `get_config` (section `autonomous`) and `config_set` also reach the block; `config_set` returns an error when the daemon did not store the key. |
| CLI | `datawatch autonomous task-routing` with `show`, `set` (a whole table from `--file`, or one row with `--type` and the `--worker-backend`, `--worker-model`, `--verify-backend`, `--verify-model`, `--effort` and `--second-choice` flags), `profile list\|save\|activate\|delete\|export\|import`, `automaton <id>` and `dry-run <id> [--profile NAME]`. Add `--json` for the raw response. |
| Comm channel | `autonomous task-routing show`, `profile list`, `profile use <name>`, `automaton <id>` and `dry-run <id> [profile]`. Only `profile use` writes. Saving, deleting, importing and exporting profiles and editing rows are not chat verbs; use the CLI, REST, MCP or the web app. |
| Web app | Settings, Automata tab, **Task routing** card: turn routing on, pick the active profile, edit every row, save the table, save, delete, export and import a profile, and preview what a draft Automaton would get. In an Automaton's detail view each task shows its task type and the row that decided its LLM (or the reason none did): a column in the task header on wide screens and a line under the title on phones. |

Reads need the autonomous read permission and writes the autonomous write
permission, like the planning routing routes.

## Check it worked

1. `datawatch autonomous task-routing show` (or the `task_routing_get` tool)
   shows `enabled: true`, your active profile and `validation` valid.
2. Run `datawatch autonomous task-routing dry-run <id>` on a draft Automaton
   and read the assignments.
3. Decompose it, open a task and compare its routing result with the dry run.
4. Set an LLM on one task by hand, decompose again, and confirm that task keeps
   your choice and its record says "hand edited" or "task LLM set by operator".
5. Open the Automaton's decision log and find the `task_routing` entries by the
   `task-router` actor.

## For the operator to review

- **Routing is off by default.** Nothing changes for existing Automata until you turn it on; check the shipped defaults table above fit your installed LLMs before you do.
- **Premium models need your approval per row.** A row naming an Opus or Fable model is refused unless that row sets `allow_premium_model`. Review every such row before saving.
- **Switching the active profile only affects tasks created afterwards.** Tasks already created keep what they were given; re-run decompose or edit the task to change them.
- **The mobile apps do not show routing yet.** The Settings card and the per-task column are in the web app only; mobile parity is tracked separately.
- **A config write that is skipped is not an error code.** `PUT /api/config` answers 200 and lists what it did not store in `skipped`, `skipped_reason` and `skipped_problems`. A script that checks only the status code will miss a refused routing block; read `skipped`, or use the dedicated routing routes, which return 400.
- **Docs and the live daemon can differ.** If a tool or command named here is missing on your daemon, it is older than this change; update it before relying on this guide.

See also: [Automata planning routing](automata-planning-routing.md),
[Automata capacity](automata-capacity.md).
