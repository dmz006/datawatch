---
docs:
  index: true
  topics: [autonomous, decompose, decompose lint, standard tasks, standard task, guidance, findings]
exec_params:
  - {name: mode, required: false, description: "Decompose-lint mode to set: off, shadow (default) or enforce"}
  - {name: max_retries, required: false, description: "Enforce retry budget: whole number 0–5 (default 2, 0 = never auto-retry)"}
  - {name: prd_id, required: false, description: "The id of the Automaton to dry-run or read the stored record for"}
exec_steps:
  - tool: autonomous_standard_tasks
    description: List the standard tasks the decomposer adds to each story (id, title, kind, enabled, mode) — a read, writes nothing
    args: {}
    read_only: true
  - tool: autonomous_decompose_lint_config_get
    description: Read the live decompose-lint configuration: mode, retry budget, the active rule pack, the standard-task pack and the guidance text
    args: {}
    read_only: true
  - tool: autonomous_decompose_lint_findings
    description: Read the last recorded decompose-lint pass for one Automaton — mode, attempts and the final findings
    args:
      id: "{{params.prd_id}}"
    read_only: true
  - tool: autonomous_decompose_lint_dry_run
    description: Lint one Automaton's stored plan read-only with the current rule pack; nothing is stored or re-decomposed
    args:
      id: "{{params.prd_id}}"
    read_only: true
  - tool: autonomous_decompose_lint_config_set
    description: Change mode and retry budget; each supplied block fully replaces the current one, blocks left out are left untouched
    args:
      decompose_lint:
        mode: "{{params.mode}}"
        max_retries: "{{params.max_retries}}"
    read_only: false
---

# How-to: the decompose-lint quality check and the standard tasks

## 1. What it is, and when you need it

Every Automaton starts life as a decomposition: a spec turned into a
story-and-task graph by the planner. **Decompose lint** is the quality
check that runs over each fresh decomposition, at decompose time, before
the plan is stored. It answers one question — *does this plan stay inside
the boundaries the operator drew* — with a rule pack, and it can give the
planner a second (or third) shot at the part that crosses a boundary.

You need it when a decomposed task does something that belongs to the
operator, not to a task:

- a task spec that **bumps a version variable** — versioning is owned by
  the release path;
- a task that **calls the hosting CLI** (`gh`, `glab`, `hub`) — hosting
  operations run on the operator's host, not inside a task;
- a task that **starts a daemon** (`systemctl`, `pm2`, `nohup … &`) outside
  the sandbox;
- a task that **reads the operator's dot folders** (`~/.datawatch`,
  `~/.ssh`, `$HOME/.config`, …) — tasks operate on the project tree only;
- a **read-only task that still lists files** it would change;
- a task with **no verification script** — its result cannot be checked;
- a task whose text **names a tracker id** (`BL-0001`, `#1234`) instead of
  describing the work.

The check has hard guarantees, whichever mode you run it in:

- **A decompose is never failed by the check.** Findings are attached to
  the stored plan; they are recorded, never fatal. If every retry comes
  back worse, the last good plan is what is stored.
- **The check is deterministic and local.** It runs in the daemon with the
  active rule pack — no LLM call, no network, no writes anywhere but the
  plan's own record.
- **Off is a complete no-op.** No standard tasks, no lint pass, no record — a
  decompose behaves exactly as if the feature did not exist.

Two companion pieces travel with it:

- **Standard tasks** — the built-in tasks (changelog entry, parity-issue
  draft, docs set, bounded load-step rule, final gate, version note)
  appended to a story the planner left incomplete, idempotently, run in
  the `model session` mode — the ones the Automata page's Templates tab
  shows as *standard tasks* (previously "task templates"; details in §5);
- **Decompose guidance** — the text handed to the planner *before* it
  writes the plan, generated from your active rules unless you supply your
  own.

## 2. The three modes, and the retry budget

`autonomous.decompose_lint.mode` picks the mode. Empty (unset) resolves to
`shadow` — the safe default. A misspelled mode (`enforc`) is a **config
error, not a guess**: `Load` refuses the file and the daemon rejects the
write, rather than quietly reading it as `enforce`.

| Mode | Standard tasks | Lint pass | Stored record | Retry |
|---|---|---|---|---|
| `off` | not applied | none | none | — |
| `shadow` (default) | applied | one | yes — all findings | never |
| `enforce` | applied | one, plus retries | yes — what remained | while block findings remain and budget lasts |

**Shadow.** The standard tasks are applied, one lint pass runs, and the pass — mode,
attempts, findings, timestamp — is recorded on the Automaton. Nothing is
blocked and nothing is retried: the plan the planner produced is the plan
that is stored, with the findings attached so you can judge them.

**Enforce.** Same as shadow, with one addition: while the pass contains a
`block`-severity finding and the retry budget is unspent, the decomposer
is **re-run with the findings fed back** as guidance. Each retry is
re-parsed, the standard tasks re-applied and re-linted, so a fix is only credited once it
survives the check. When the budget is spent (or a retry fails or comes
back empty — in which case the last good plan is kept), the pass that
remained is what is recorded.

**The retry budget** is `autonomous.decompose_lint.max_retries`:

- default **2** (so enforce = one initial pass + at most two retries);
- **0 is a valid explicit value** — enforce mode then lints, records and
  stops, never re-running;
- negatives clamp to 0, values above the cap (**5**) clamp to 5;
- **only `block` findings trigger retries** — `warn` findings are
  reported, never retried.

A stored record says how it went: `attempts` is 1 for the initial pass,
1 + the retries consumed (at most 1 + `max_retries`) in enforce.

## 3. The rule pack that ships

The built-in pack is the **floor** every run starts from: nine rules —
the six `block` rules first, then the three `warn` rules. Your operator
rules are the **ceiling over it** (see §4). The regular expressions are
deliberately generic: they describe the *shape* of a dangerous task spec
and never name a particular project, repository or home directory.

| Rule id | Sev. | Fires on |
|---|---|---|
| `edits_version_variable` | block | a spec that bumps or edits a version field/constant |
| `runs_hosting_cli` | block | a spec that invokes `gh` / `glab` / `hub` with a subcommand |
| `starts_daemon_outside_sandbox` | block | a spec that starts or restarts a daemon (`systemctl`, `launchctl`, `pm2`, `nohup … &`) |
| `reads_operator_data_or_config_folders` | block | a spec that reads a home-directory dot folder (`.datawatch`, `.config`, `.ssh`, `.aws`, …) |
| `read_only_but_writes_files` | block | a task marked `read_only` that still lists files |
| `read_only_unverifiable` | block | a read-only task (the `read_only` flag, or a spec whose first line is `READ-ONLY:`) with neither a verification script nor a report path under `.datawatch/reports/`; without one, a failure has nothing to inspect |
| `no_verification_script` | warn | a task with no verification script |
| `names_tracker_id` | warn | a spec or title that names a tracker id (`BL-0001`, `GH#123`, `#1234`) |
| `unbounded_load_step` | warn | a spec that asks for an unbounded CPU-load/stress step (busy loop, load/soak test, pinning cores) — bound it before it runs: niced (`nice -n 19`), at most a quarter of the cores, a hard timeout, a cleanup trap so it never outlives its owning task |

## 4. Writing your own rules — a worked example

A rule is `{id, severity, match, message}` (plus an optional `disabled`).

```yaml
autonomous:
  decompose_lint:
    mode: enforce
    max_retries: 2
    rules:
      # New operator rule — fires on raw-SQL tasks, which on this
      # deployment must go through the review tool, not a task.
      - id: runs_review_bypass_sql
        severity: block
        match:
          spec_regex: "(?i)\\b(SET\\s+GLOBAL|ALTER\\s+SYSTEM|DROP\\s+TABLE)\\b"
        message: >
          Destructive SQL does not run inside a task: hand it to the
          review workflow and reference its result.
      # Override a built-in rule: same id replaces it at the same
      # position. Here the operator wants version notes but the bump
      # wording the planner likes; block only the bump shape.
      - id: edits_version_variable
        severity: block
        match:
          spec_regex: "(?i)\\bbump\\b[^.\\n]{0,30}\\bversion\\b|\\bversion\\b[^.\\n]{0,24}[=:]"
        message: >
          Versioning is owned by the release path, not by tasks —
          record a note, do not edit the field.
      # Disable a built-in rule you do not want at all. A bare
      # {id, disabled: true} entry is valid: severity/message/match
      # stay unused and are not validated.
      - id: names_tracker_id
        disabled: true
```

### The five rule fields

| Field | Rule |
|---|---|
| `id` | **required**, unique across the config, `[a-z0-9_.-]+`. Named in findings, docs and feedback. |
| `severity` | `block` (stops the decompose in enforce mode) or `warn` (reported only). Empty normalizes to `warn`; anything else is a config error. |
| `match` | which tasks the rule fires on — see below. |
| `message` | the finding text. **Required** on an active rule; a rule with no message cannot be surfaced. Exempt for `disabled` rules. |
| `disabled` | removes the rule from the effective set. `{id, disabled: true}` is the clean way to silence a built-in. |

### Match: which tasks a rule fires on

`match` is a set of **AND-ed** conditions — every condition present must
hold for the same task. The leaves:

| Key | Matches |
|---|---|
| `spec_regex` | Go regex against the task **spec** |
| `title_regex` | Go regex against the task **title** |
| `files_regex` | Go regex against the task's **files list** |
| `read_only` | tri-state: absent = don't check; `true`/`false` = assert the flag |
| `verification_script_empty` | assert the task declares no verification script |
| `report_path_empty` | assert the task has no usable report path (empty, or not a clean path under `.datawatch/reports/`) |
| `any_of` | at least **one** of these nested conditions must match |
| `all_of` | **every** one of these nested conditions must match |

Nesting is capped (depth 8) so a corrupt config cannot stack-overflow the
validator. Two shapes worth knowing:

- **Spec-or-title.** A bare `spec_regex` + `title_regex` pair would
  *require both* — use `any_of: [{spec_regex: …}, {title_regex: …}]` to
  mean either (this is exactly how the built-in `names_tracker_id` is
  written).
- **Read-only but writes files.** `read_only: true` + `files_regex: ".+"`
  — the built-in `read_only_but_writes_files` — combines a flag predicate
  with a text predicate on one task.
- **Read-only, however it was declared.** The built-in
  `read_only_unverifiable` uses `any_of: [{read_only: true}, {spec_regex:
  "(?i)\\A\\s*READ-ONLY:"}]` so the flag and the spec marker both count,
  then requires `verification_script_empty` and `report_path_empty`.

### Turning rules off, three ways

1. **`disabled_rules`** on the config — a flat list of rule ids dropped
   from the effective set (built-in or operator). Used by the CLI / comm
   `config-set` surface, which preserves the rest of the pack.
2. **`{id: <id>, disabled: true}`** in `rules` — names the rule explicitly
   in your pack; good when the pack is self-documenting.
3. **Same id, new value** in `rules` — an operator rule with a built-in's
   id **replaces** that built-in at its original position (your rule's
   match wins; the built-in's does not run).

## 5. Standard tasks (pack, per-project overrides, reading the list)

**Standard tasks** are the built-in task definitions appended to a story
when the planner did not produce a task for them. Apply is **idempotent**
— a task a previous apply added (or the planner marked with
`template:<id>` in its title or spec, or whose title closely matches the
task's) is detected, and nothing is added twice. Slots `{{feature}}`,
`{{files}}` and `{{files_list}}` are filled from the feature name and the
union of the story's task files; a task's `depends_on` lists *standard-task* ids
it must run after, and the resulting task also depends on the story's
other tasks. Each standard task runs in the `model session` mode — a model
session does the work like any other task.

> **Naming.** These are called **standard tasks** (previously "task templates").
> The PWA's Automata page shows them under the Templates tab, whose first
> sub-tabs are *Automaton templates* and *Standard tasks* (the 2026-11-08
> operator decision). The config keys keep the code-level name
> `autonomous.task_templates.*`; operator-facing text says *standard
> tasks*.

The **built-in pack** is the floor — six standard tasks, written to hold
two hard properties for every feature name a story can carry: no blocking
decompose-lint finding, and no hosting-CLI side effects.

| Standard-task id | What it adds | kind |
|---|---|---|
| `changelog_entry` | a changelog entry for the feature (`CHANGELOG.md`) | `docs` |
| `parity_issue_draft` | a parity-gap draft file under `docs/plans/parity-issues/` — a file write only, never a tracker call | `docs` |
| `docs_set` | the feature's docs into shape, over the story's own files | `docs` |
| `bounded_load_step` | the bounded load-step rule for a load step the plan names (the rule itself: [howto/automata-orphan-reaping.md](automata-orphan-reaping.md)) | `note` |
| `final_gate` | a **read-only** closing task: build clean, tests pass, files hang together — writes the gate report to `.datawatch/reports/` | `final_gate` |
| `version_bump_left_to_merge` | a plain *note* that the version is the next patch above main, resolved at merge — never an edit | `note` |

Standard tasks are applied in **shadow and enforce** (off applies
nothing), and `autonomous.task_templates.enabled` (default **true**) gates
them globally. Your operator standard tasks are the ceiling over the built-in
pack: **same id replaces, new id appends**; `autonomous.task_templates.projects`
holds the per-project on/off switch (below), and the reads below list the
resolved pack.

### Reading the standard-task list

The list is read on every surface (fields: `id`, `title`, `kind`,
`summary`, `offered_when`, the effective `enabled`, `project_override`
and `mode` — `model session` for each):

- **REST** — `GET /api/autonomous/standard-tasks`; optional
  `?project=<dir-or-alias>` resolves that project's overrides; the body
  is `{enabled, tasks[]}`. Any other method is 405.
- **MCP** — the `autonomous_standard_tasks` tool (optional `project`
  argument).
- **CLI** — `datawatch autonomous standard-tasks [--project <dir-or-alias>] [--json]`: a table of `ID TITLE KIND ENABLED MODE`; `--json` prints the raw body.
- **Chat** — `standard tasks` (or `autonomous standard-tasks`): one line
  per task — `id | title | on|off | mode`; an empty list replies "no
  standard tasks are defined."

All four render the same `autonomous.ListStandardTasks` listing, so they
always agree (pinned against `testdata/standard-tasks/expected.json`).

### A custom standard task with slots

```yaml
autonomous:
  task_templates:
    enabled: true
    templates:
      - id: operator_handoff_note
        title: "Operator handoff note for {{feature}}"
        spec: >
          Write docs/ops/handoff/{{feature}}.md: what changed, how to
          smoke-test it, and any follow-up the operator should queue.
          Story files involved: {{files_list}}.
        files: "docs/ops/handoff/{{feature}}.md"
        kind: docs
        applies_when: >
          Offered when the slice changes something the operator touches
          in the day-to-day.
      # A dependent read-only gate that runs after the handoff note.
      - id: handoff_gate
        title: "Handoff gate for {{feature}}"
        spec: >
          Read-only: confirm the handoff note exists, names the right
          files ({{files}}), and that none of the listed files is a
          version variable or a dot-folder path.
        kind: final_gate
        read_only: true
        depends_on: [operator_handoff_note]
```

### Per-project overrides

`autonomous.task_templates.projects` maps a **project directory or alias**
to an `{enabled: …}` override. The override wins for that project; an
absent flag means *follow the global*. A new project you have not
configured follows the global `enabled`. Reads of the list (§5)
take the same `project` / `--project` selector, so what you read is
what apply sees for that project.

```yaml
  projects:
    "/srv/projects/web":
      enabled: false   # standard-task apply off for this project only
    "billing":
      enabled: true     # on, even if the global were off
```

## 6. Decompose guidance — text or file

`autonomous.decompose_guidance` is the block **appended to the planner's
prompt** (under a `--- Decompose guidance ---` header), resolved by this
precedence:

1. **Literal text** — used verbatim. A value that is prose (no path
   separator, no document extension) is never mistaken for a file.
2. **An existing readable file** — the file's **contents** are the
   block. Point it at a `docs/plans/<your-guidance>.md` and keep it there.
3. **A path-like value whose file is missing, unreadable or empty** — a
   dead path is not prose to ship, so the **generated block** stands in.
4. **Unset (empty)** — the block is **generated from the active rule
   pack**: one line per rule, `id (severity): message`, in pack order.

So a freshly configured operator with no guidance string sees their rules
in the prompt automatically; once you set `decompose_guidance`, yours wins
until you clear it (clear it by writing an empty string through the same
surface).

## 7. Reading the findings and the standard-task list — every surface

A finding is `{rule_id, severity, message, story, task, matched}` — the
offending task, or its story when the finding is story-scoped, or `-`
when it is whole-plan. Every surface renders the same line:
`[severity] rule_id task: message`; an empty set renders as
`clean: 0 findings`.

```
decompose lint of prd-9f3a (mode enforce, 9 active rules): 1 block, 2 warn
[block] runs_hosting_cli task-2: Task spec invokes the hosting CLI (gh/glab/hub); hosting operations run on the operator's host, not inside a task.
[warn] no_verification_script task-5: Task has no verification script; every decomposed task should declare one so its result can be checked.
[warn] no_verification_script story-1: Task has no verification script; every decomposed task should declare one so its result can be checked.
```

### PWA

- **Settings → “Decompose lint”** — the mode, the retry budget, the
  standard-task gate, the guidance string and the rules (as JSON). Saves
  through `PUT /api/autonomous/decompose-lint/config`.
- **Automata page → Templates tab (labelled "Automaton templates" in the current UI) →
  Standard tasks sub-tab** — the built-in pack by its operator-facing name
  (previously "task templates"); the same list the reads below return. The sub-tab
  is read-only: a per-task table (title, kind, summary, when offered, on/off, mode)
  with a note that the mode is fixed at `model session` for now, an explanatory
  empty state when there is nothing to list, and stacked labelled rows at phone
  width; an empty list on the comm channel replies "no standard tasks are defined."
- **Automaton detail → “Decompose lint” panel** — the last recorded pass
  (mode, attempts, findings with severity chips), with a **dry-run
  button** that lints the current stored plan read-only and says
  “Dry run (not recorded)”.

### REST

| Method & path | What it is |
|---|---|
| `GET /api/autonomous/decompose-lint/config` | the live config: `{decompose_lint, task_templates, decompose_guidance}` |
| `PUT /api/autonomous/decompose-lint/config` | full-replace of **any** of those blocks; each present key replaces its whole block, omitted keys are untouched; the whole value is validated before any of it is applied; 400 with the rejection on error |
| `GET /api/autonomous/prds/{id}/decompose-lint` | the stored record — `decompose_lint: null` when none is stored (mode off records nothing; the same reply is the one before the first decompose and after deletion) |
| `POST /api/autonomous/prds/{id}/decompose-lint` | **read-only dry run**: lints the stored plan with the current pack and returns the report; nothing is stored, re-decomposed or mutated |
| `GET /api/autonomous/standard-tasks[?project=…]` | the standard-task list: `{enabled, tasks[]}`, each task's `id`, `title`, `kind`, `summary`, `offered_when`, effective `enabled`, `project_override` and `mode`; `project` is a project directory or alias; other methods are 405 |

### MCP (5 tools)

| Tool | Surfaces |
|---|---|
| `autonomous_decompose_lint_config_get` | live config read (read-only) |
| `autonomous_decompose_lint_config_set` | config write — pass a `decompose_lint`, `task_templates` and/or `decompose_guidance` block; each replaces the current one, the daemon's PUT is the gate |
| `autonomous_decompose_lint_dry_run` | the per-Automaton dry run (read-only, takes `id`) |
| `autonomous_decompose_lint_findings` | the stored record read (read-only, takes `id`) |
| `autonomous_standard_tasks` | the standard-task list (read-only; optional `project` argument) |

### CLI — `datawatch autonomous lint`

| Verb | What it is |
|---|---|
| `lint config` | the live config (read-only) |
| `lint config-set key=value …` | set `mode`, `max_retries` or `disabled_rules`, **preserving the active rule pack**; the daemon's PUT is the gate and returns the new config |
| `lint dry-run <id>` | lint one Automaton's stored plan without storing or retrying |
| `lint findings <id>` | the stored record — mode, attempts and final findings |

`--json` prints the raw daemon reply. Exit 0 on success, 1 otherwise.

The standard-task list is one verb up the `autonomous` group:
`datawatch autonomous standard-tasks [--project <dir-or-alias>] [--json]`
— a table of `ID TITLE KIND ENABLED MODE`; `--json` prints the raw
`GET /api/autonomous/standard-tasks` body.

### Comm channel — `autonomous lint`

The same five verbs, one operator-facing wording everywhere (the CLI and
the comm channel share the finding-line renderer): `autonomous lint
config`, `lint config-set mode=enforce max_retries=2 disabled_rules=a,b`,
`lint dry-run <id>`, `lint findings <id>`. The standard-task list is the
word `standard tasks` (or `autonomous standard-tasks`): one line per
standard task — `id | title | on|off | mode`; an empty list replies "no
standard tasks are defined."

## 8. What it will never do

- **Never fails a decompose.** A failing pass attaches findings to the
  stored plan; a failed retry keeps the last good plan.
- **Never calls the planner in shadow or in a dry run.** Retries are
  enforce-only; the dry run lints the stored plan and mutates nothing.
- **Never applies standard tasks in off mode.** Off is a complete no-op —
  no standard tasks, no pass, no record.
- **Never guesses a mode.** `off`, `shadow`, `enforce` (case-insensitive)
  or empty; anything else is rejected, never coerced.
- **Never re-triggers on config change.** A pass is recorded with the
  pack it ran under; a rule change applies on the next decompose (or dry
  run).

## 9. A reasonable end-to-end sequence

1. Leave the default **shadow** for a while; read the records on the PWA
   panel (or `lint findings <id>`).
2. Silence the rules you do not care about (`{id, disabled: true}` or
   `disabled_rules`); tighten the ones you do (same-id replacement).
3. Add your own `block` rules for the boundaries that matter *here*.
4. Flip `mode: enforce` with the `max_retries` you can afford (0 is legal
   and is “record, but let me decide”).
5. If a class of finding is systemic, add a **guidance file** or template
   so the planner writes the plan right the first time.
