---
docs:
  index: true
  topics: [automata, autonomous, task, routing, llm, model, profile]
exec_steps:
  - tool: get_config
    description: Read the autonomous settings, including the autonomous.task_routing block (enabled, active profile and the profile tables)
    args: {section: "autonomous"}
    read_only: true
---

# Automata task routing: the right LLM for each task

When an Automaton is decomposed, every task it gets needs a worker LLM (and
optionally a verification LLM). By default a task uses the Automaton's LLM, or
the global default. **Task routing** adds a table that maps each *task type*
(a documentation task, a locale-string task, a daemon code task and so on) to
a backend and model, so a mechanical task can go to a cheap local model and a
code task to a stronger one.

Task routing is **off by default**. Nothing changes for you until you turn it
on. The backends and models in the shipped table are the operator's starting
choice, not a rule; other users may change them freely.

## How a task gets its LLM

1. The task is **classified** into one task type. The order is fixed:
   the task's own kind (a smoke or final-gate task is `final_gate`, and a task
   made from a template keeps that template's type), then ordered keyword rules
   over the title and the spec, then the default type `daemon_backend_code`.
2. A task that lists a non-test Go file under `internal/` is never classified
   as a mechanical type. The tie goes to the code type.
3. The **active profile** row for that type is written into the task's own
   backend, model, effort and verification fields.

The task types are `docs_set`, `changelog_entry`, `parity_issue_draft`,
`bounded_load_step`, `version_note`, `locale_strings`, `tests`,
`formatting_cleanup`, `ui_code`, `daemon_backend_code`, `mcp_api_surface`,
`merge_conflict_repair`, `final_gate` and `review_repair`.

## What always wins over the table

The most specific choice wins: a task LLM set by an operator, then a story LLM,
then an Automaton LLM, then the routing table, then the global default. The
table applies only where none of the others is set. It also never touches:

- a task that was edited by hand (edit, set task LLM, set task verification
  LLM by anyone other than the router);
- a task that has started, finished or has a live session. Applying never
  kills, restarts or signals a session.

A planner re-run re-applies the table only to tasks the router itself routed
and nobody edited. Switching the active profile changes only tasks created
afterwards.

## Rows, profiles and the shipped table

A **row** holds a worker backend and model, an optional verification backend and
model (empty means the global verifier policy), an effort, an optional second
choice used when the first choice is refused, and `allow_premium_model`. A
**profile** is a named table of rows; one profile is active.

The shipped `default` profile sends the mechanical types (`docs_set`,
`changelog_entry`, `parity_issue_draft`, `locale_strings`, `formatting_cleanup`,
`tests`, `version_note`, `bounded_load_step`) to `pi-prompt` with
`ollama/qwen3.8:27b`, and the code types (`ui_code`, `daemon_backend_code`,
`mcp_api_surface`, `merge_conflict_repair`, `review_repair`) to `claude-code`
with `sonnet`. `final_gate` has an empty row, so it uses the default. No shipped
row names an Opus or Fable model and every verification field is empty. A config
with no `task_routing` block gets this table with routing off.

## Validation

A block is checked before it is used and a bad one is rejected with nothing
applied. The checks are: the active profile exists; every row's task type is
known; every backend is known to the LLM registry; every model pairs with its
backend (the same rule the task and story LLM setters use); a model with no
backend is refused; and a row naming an Opus or Fable model is refused unless
that row sets `allow_premium_model: true`. On a REST config write, a name that
fails is skipped and reported, and the rest of the change still applies.

## Seeing what was decided

Each task records a routing result: its type, the profile and row used, how it
was classified (`template_kind`, `title_spec` or `default`), what was chosen,
whether the second choice was used, or the reason nothing was applied (for
example "task LLM set by operator", "story LLM set", "Automaton LLM set",
"routing disabled", "hand edited", "task already started"). The same outcome is
written to the Automaton's decision log as a `task_routing` entry by the
`task-router` actor.

## Access from every surface

| Surface | How |
|---------|-----|
| YAML | Edit the `autonomous.task_routing` block in `datawatch.yaml` (see `docs/config-reference.yaml`), then reload. |
| REST API | `GET` and `PUT /api/autonomous/task-routing` read and replace the table (the read includes its validation result). `GET` and `POST /api/autonomous/task-routing/profiles` list and save profiles; `PUT .../active-profile` switches; `DELETE .../profiles/{name}` deletes; `GET .../profiles/{name}/export` and `POST .../profiles/import` move a profile as JSON. `GET /api/autonomous/prds/{id}/task-routing` shows which row decided each task and `POST .../prds/{id}/task-routing/dry-run` is the no-write dry run. `PUT /api/config` with `autonomous.task_routing.*` still works. |
| MCP | `task_routing_get`, `task_routing_set`, `task_routing_profile_list`, `task_routing_profile_save`, `task_routing_profile_activate`, `task_routing_profile_delete`, `task_routing_profile_export`, `task_routing_profile_import`, `task_routing_automaton` and `task_routing_dry_run`. `get_config` (section `autonomous`) and `config_set` also reach the block. |
| CLI | `datawatch autonomous task-routing` with `show`, `set` (a whole table from `--file`, or one row with `--type` and the `--worker-backend`, `--worker-model`, `--verify-backend`, `--verify-model`, `--effort` and `--second-choice` flags), `profile list\|save\|activate\|delete\|export\|import`, `automaton <id>` and `dry-run <id> [--profile NAME]`. Add `--json` for the raw response. |
| Comm channel | `autonomous task-routing show`, `profile list`, `profile use <name>`, `automaton <id>` and `dry-run <id> [profile]`. Only `profile use` writes. Saving, deleting, importing and exporting profiles and editing rows are not chat verbs; use the CLI, REST, MCP or the web app. |
| Web app | Settings, Automata tab, **Task routing** card: turn routing on, pick the active profile, edit every row, save the table, save, delete, export and import a profile, and preview what a draft Automaton would get. In an Automaton's detail view each task shows its task type and the row that decided its LLM (or the reason none did): a chip in the task header on wide screens and a line under the title on phones. |

Reads need the autonomous read permission and writes the autonomous write
permission, like the planning routing routes.

### Switching, previewing and moving profiles

- **Switch** with `profile activate` (CLI, MCP), `profile use` (chat), the
  active-profile route (REST) or the Profile picker (web). Only tasks created
  afterwards use it.
- **Dry run** takes an Automaton and, optionally, a profile name. It shows what
  that profile would assign to each task and writes nothing. Use it on a draft
  before approving.
- **Export and import** use the same JSON file on every surface. An import is
  validated and refused with every problem listed if it is not valid. Pass a
  name to store it under a different name and set overwrite to replace a
  profile that already has the name. The active profile cannot be deleted.

## Check it worked

1. `datawatch autonomous task-routing show` (or `get_config` with section
   `autonomous`) shows `task_routing` with
   `enabled: true` and your active profile.
2. Decompose a test Automaton, then open a task. Its routing result names the
   type, the profile and the backend and model chosen.
3. Run `datawatch autonomous task-routing dry-run <id>` on a draft and compare
   it with the routing result shown on the tasks after decompose.
4. Set an LLM on one task by hand, decompose again, and confirm that task keeps
   your choice.
