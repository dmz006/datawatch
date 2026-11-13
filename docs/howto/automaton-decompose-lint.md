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
    description: Change mode and retry budget; the decompose_lint block you send replaces the current one whole
    args:
      decompose_lint:
        mode: "{{params.mode}}"
        max_retries: "{{params.max_retries}}"
    read_only: false
------

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

- **Standard tasks** (previously "task templates") — the built-in tasks (changelog entry,
  parity-issue draft, docs set, bounded load-step rule, final gate, version
  note) appended to every story the planner left incomplete, idempotently —
  the ones the Automata page's Templates tab shows as *standard tasks*
  (details in §5). Each runs as an LLM session, in the
  daemon, or not at all; see [daemon task modes and the script gate](automaton-daemon-task-modes.md)
  for the per-task modes, the defaults and the gate settings;
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

The built-in pack is the **floor** every run starts from: thirteen rules —
the nine `block` rules first (the six boundary rules, then the three
verification-script stability rules), then the four `warn` rules. Your operator
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
| `verify_script_task_files_only` | block | a verification script that compares only against the task's own files (a literal allowed-file list equal to them, or a file-listing diff of HEAD, the working tree or main) and never against `git merge-base`; a sourced helper (one level deep) that uses the merge base passes. The finding names the script path. A script that does not exist yet is skipped |
| `verify_script_docs_without_sync` | block | a verification script that checks docs, the embedded docs copy (`internal/server/web/docs`) or how-to coverage (`check-docs-sync`, `check-howto-coverage`, `docs/howto`) but never runs `make sync-docs`, directly or through a sourced helper. The finding names the script path |
| `final_gate_without_script` | block | a `final_gate` task with no verification script, no `verification_script_reason` and no usable report path |
| `no_verification_script` | warn | a task with no verification script |
| `unexecutable_verification_script` | warn | a task whose verification script is a command string, or (when the Automaton's project directory is known) a script file that is missing, not executable or without a shebang; a script the task lists in its own files is not flagged |
| `names_tracker_id` | warn | a spec or title that names a tracker id (`BL-0001`, `GH#123`, `#1234`) |
| `unbounded_load_step` | warn | a spec that asks for an unbounded CPU-load/stress step (busy loop, load/soak test, pinning cores) — bound it before it runs: niced (`nice -n 19`), at most a quarter of the cores, a hard timeout, a cleanup trap so it never outlives its owning task |

### The three verification-script stability rules

The last three `block` rules read the task's verification script from the
project directory (and any helper it sources, one level deep) and name the
**script path and the rule id** in the finding:

- `verify_script_task_files_only` — fires when the script compares only
  against the task's own files and never against `git merge-base`. Fix: compare
  against the merge base and a cumulative allowed-file list.
- `verify_script_docs_without_sync` — fires when the script checks docs, the
  embedded docs copy or how-to coverage without running `make sync-docs`. Fix:
  run `make sync-docs` first.
- `final_gate_without_script` — fires on a `final_gate` task that has no
  script, no `verification_script_reason` and no usable report path. Fix: give
  it a script, a report path, or a reason.

They use three match predicates (below): `verification_script_task_files_only`,
`verification_script_docs_without_sync` and `verification_script_reason_empty`
(the last with `kind: final_gate`). A script that does not exist yet is
skipped by the first two. The full authoring guide, with the fix for each rule
and how to set a task's script or reason on every surface, is
[Write verification scripts that stay stable](automaton-verification-scripts.md).

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
| `verification_script_unexecutable` | assert the task's verification script is not a runnable file (command string, or missing/not executable/no shebang when the project directory is known) |
| `verification_script_reason_empty` | assert the task states no reason for lacking a verification script |
| `verification_script_task_files_only` | assert the script (plus sourced helpers, one level) compares only against the task's own files and never the merge base; skipped when the script file does not exist |
| `verification_script_docs_without_sync` | assert the script checks docs / embedded docs / how-to coverage without running `make sync-docs`; skipped when the script file does not exist |
| `kind` | assert the task kind (for example `final_gate`), case-insensitive |
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
other tasks. Each standard task runs in its own mode (`llm`, `daemon` or `off`,
see [Template mode](#template-mode-who-runs-the-task) below); `llm` is a model
session, like any other task, and `daemon` is performed by the daemon with no session.

> **Naming.** These are called **standard tasks** (previously "task templates").
> The PWA's Automata page shows them under the Templates tab, whose first
> sub-tabs are *Templates* and *Standard tasks* (the 2026-11-08
> operator decision). The config keys keep the code-level name
> `autonomous.task_templates.*`; operator-facing text says *standard
> tasks*.

The **built-in pack** is the floor — six standard tasks, written to hold three
hard properties for every feature name a story can carry: no blocking
decompose-lint finding, no hosting-CLI side effects, and no verification
script that is not genuinely a script file (the read-only ones verify
through their report instead) — so a decomposed Automaton's closing tasks
reach the verifier and pass it, instead of being rejected at pre-start.

| Template id | Default mode | What it adds |
|---|---|---|
| `changelog_entry` | `daemon` | a changelog entry for the feature (`CHANGELOG.md`) |
| `parity_issue_draft` | `daemon` | a parity-gap draft file under `docs/plans/parity-issues/` — a file write only, never a tracker call |
| `docs_set` | `llm` | the feature's docs into shape, over the story's own files |
| `final_gate` | `llm` | a **read-only** closing task, applied **once per Automaton** (on the last story, depending on every task), that the **daemon itself** now performs — [the script gate](#the-script-gate-the-final-gate-no-longer-spends-a-session) runs it and closes it, no model session: the declared checks run, and the verdict and evidence go to the report its `report_path` names under `.datawatch/reports/`. The template carries no verification script of its own; the report is its output. A planner-written `final_gate` must declare a verification script, a `verification_script_reason` or a report path (lint rule `final_gate_without_script`) |
| `version_bump_left_to_merge` | `daemon` | a plain *note* that the release version is the next patch above main, resolved at merge — never an edit of the tree; in daemon mode the daemon records the note as the decision and completes the task, no session |
| `bounded_load_step` | `daemon` | a posture *note* — the task's spec is the bounded-load-step rule text so it can be found in search and the audit; writes no files |

### Template mode: who runs the task

Each built-in template has a **default mode** (column above). The mode
decides *who* runs the task when it is appended to a story:

| Mode | Meaning |
|---|---|
| `llm` | a worker session runs the task as a normal LLM task — except `final_gate`: the [script gate](#the-script-gate-the-final-gate-no-longer-spends-a-session) performs it and closes it, so `llm` no longer spends a session there either |
| `daemon` | the daemon performs the task itself — no model session, no worker — and records it as done |
| `off` | the template is **not appended** to the story at all (the task does not appear) — for `final_gate`, off means the Automaton has no closing gate at all |

The mode is set per-template with `autonomous.task_templates.mode` (a
map of template id → mode string):

```yaml
autonomous:
  task_templates:
    enabled: true
    mode:
      # The four daemon-mode templates below run without a model session.
      changelog_entry: daemon
      parity_issue_draft: daemon
      version_bump_left_to_merge: daemon
      bounded_load_step: daemon
      # docs_set still spawns its worker session. final_gate stays llm but
      # is the daemon's script gate that performs it — no session either way.
      docs_set: llm
      final_gate: llm
```

**How to change a mode.** There are two writes, and they behave differently:

- **Per-template mode keys** — `autonomous.task_templates.mode.<id>`,
  written by the per-template select in Settings,
  `datawatch config set autonomous.task_templates.mode.<id> daemon` on the
  CLI, `PUT /api/config` with that one key, chat or comm
  `configure autonomous.task_templates.mode.<id>=<mode>`, or the MCP
  `autonomous_config_set` `task_templates_mode` — **merge one mode onto the
  stored mode map**: an id you leave out keeps its mode, and `enabled`,
  `templates` and `projects` are untouched.
- **The decompose-lint config surface** — the MCP
  `autonomous_decompose_lint_config_set` tool, the REST
  `PUT /api/autonomous/decompose-lint/config`, and the Settings page's
  decompose-lint card — **replaces the `task_templates` block as a whole
  when it is sent**: a block that carries only `enabled` resets `mode`,
  `templates` and `projects`. The decompose-lint card sends back the
  `task_templates` block it loaded when the card was opened, with only its
  enabled checkbox changed, so `mode`, `templates` and `projects` survive a
  save. A change another client made to that block after the card loaded is
  overwritten by the save, so reopen the card before saving. The REST, MCP
  and CLI surfaces still replace the whole block when they send it, so read
  the live config first (`datawatch autonomous lint config`) and write the
  whole block back with the mode map changed.

**What a daemon-mode task does differently:**

- **No model session, no verifier, no auto-retry loop.** The executor
  performs the task itself — before it would ever spawn, verify or retry —
  then marks it completed (or failed, if the handler cannot) through the
  same status, save and lifecycle path a worker task takes, so the story
  roll-up counts it. There is no worker session entry in the session list
  for it.
- **The structural checks skip it.** The decompose lint and the
  plan-structure checks (per-task verification script, file tokens,
  read-only rules) skip daemon-mode tasks — a handler performs them, a
  judge does not, so they need no exemption to stand.
- **`changelog_entry` and `parity_issue_draft` write the generated
  artefact.** The daemon merges the change records of the story's
  completed tasks and writes the same tracked file, from the same
  template, under the same edit-protection rules the generated-artefacts
  feature uses (see [generated-artefacts.md](generated-artefacts.md)):

  - **An empty merge still renders** — if the story has no completed task
    yet, the draft file is still written, with a minimal placeholder
    entry (for example "0 files in 0 packages").
  - **A file you edited is left alone** — the write is skipped, and the
    task still completes with the skip reason in its decision note.
  - **A kind switched off in the generated-artefact settings writes
    nothing** — the task still completes, with the switch-off noted
    instead of a path in its decision note.
  - **No project directory means nowhere to write** — the task is marked
    failed with the reason, and nothing is written.
- **`bounded_load_step` and `version_bump_left_to_merge` write nothing.**
  The spec text is itself the note: the task completes with a decision
  note carrying the spec text, and that is the record an operator reads
  back on the story. Both templates also carry `kind: note` and a
  `verification_script_reason` exemption (a note task changes no files),
  which is what carries them if one is switched back to `llm` mode and
  runs as a session.
- **`final_gate` is not daemon mode — it is its own mechanism.** Its kind
  routes it to the [script gate](#the-script-gate-the-final-gate-no-longer-spends-a-session),
  which resolves the gate's declared checks and runs them, completing the
  task on a clean pass and failing it on a failed check, a timeout, or
  nothing runnable at all — never a model session, never a verifier, never
  a retry loop, even in its default `llm` mode.

Standard tasks are applied in **shadow and enforce** (off applies
nothing), and `autonomous.task_templates.enabled` (default **true**) gates
them globally. Your operator standard tasks are the ceiling over the built-in
pack: **same id replaces, new id appends**; `autonomous.task_templates.projects`
holds the per-project on/off switch (below), and the reads below list the
resolved pack.

**A template's `verification_script` is one file, not a command.** It must
name a single repo-relative script that already exists in the project as an
executable (for example `scripts/verify.sh`) — the verifier's pre-start
check runs it *as a file*, so a command string such as `go build ./... &&
go test ./...` is not a valid script and a task carrying it dies before it
can run. The built-in pack never uses one for that reason: the final gate
(whose declared checks the daemon's [script gate](#the-script-gate-the-final-gate-no-longer-spends-a-session) now runs) still verifies through its report, and the version note is read, not
run. A template whose `verification_script` looks like a command
(whitespace, shell operators, an absolute path, a parent reference or the
project root itself) is refused at config write with an error naming the
template id — nothing is stored — and a template that still carries such a
value is skipped when the templates are applied, so it never becomes a task
the verifier cannot start. Need checks like a full build on the closing
task? Put them in a script under `scripts/` and name that file.

### Reading the standard-task list

The list is read on every surface (fields: `id`, `title`, `kind`,
`summary`, `offered_when`, the effective `enabled`, `project_override`,
`mode` — the template's effective mode, `llm`, `daemon` or `off` — and
`runner`, which names what carries the task out: `session`, `script gate`,
`daemon` or `none`):

- **REST** — `GET /api/autonomous/standard-tasks`; optional
  `?project=<dir-or-alias>` resolves that project's overrides; the body
  is `{enabled, tasks[]}`. Any other method is 405.
- **MCP** — the `autonomous_standard_tasks` tool (optional `project`
  argument).
- **CLI** — `datawatch autonomous standard-tasks [--project <dir-or-alias>] [--json]`: a table of `ID TITLE KIND ENABLED MODE RUNNER`; `--json` prints the raw body.
- **Chat** — `standard tasks` (or `autonomous standard-tasks`): one line
  per task — `id | title | on|off | mode | runner`; an empty list replies "no
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

**Note on `handoff_gate`:** it is `kind: final_gate`, so the [script gate](#the-script-gate-the-final-gate-no-longer-spends-a-session)
performs it — the first declared check wins (its own script, else the project
gate command, else the story's other scripts), the task completes or fails,
and no model session is spent. This handoff gate declares no script of its
own, so make sure the story or the project gate command does — a gate with
nothing runnable is a failed task, not an unclosed story, with the reason
named. If you want it as a session task instead, give it a different kind
(say `docs`) and it takes the usual `llm` path.

### Per-project overrides

`autonomous.task_templates.projects` maps a **project directory or alias**
to an `{enabled: …}` override. The override wins for that project; an
absent flag means *follow the global*. A new project you have not
configured follows the global `enabled`. Reads of the list (§5)
take the same `project` / `--project` selector, so what you read is
what apply sees for that project.
There is no per-project mode — the `mode` map is global, per standard-task id.

```yaml
  projects:
    "/srv/projects/web":
      enabled: false   # standard-task apply off for this project only
    "billing":
      enabled: true     # on, even if the global were off
```

### Stable verification scripts: what the planner is told to write

The planner prompt carries one shared helper contract so the scripts it
writes keep passing after earlier tasks land. A generated script must:

1. run `make sync-docs` itself as its first step when it checks docs, the
   embedded docs copy (`internal/server/web/docs`) or how-to coverage;
2. use an allowed-file list that is **cumulative from the start** — every
   file any story of the Automaton may touch, the standard tasks' generated
   files (changelog draft, parity-issue draft and its hash file, docs, the
   registry plan files), gofmt targets and docs files — never one task's own
   file list;
3. compare against the **merge base** (`git diff --name-only $(git merge-base
   HEAD main)` plus untracked files), never the working tree, the current
   main or one task's own files;
4. run gofmt only on the listed changed Go files, never a whole package or
   directory.

The planner is also told to plan exactly one final gate, as the last task,
and to give every `final_gate` a `verification_script` (also listed in
`files_generated`) or an explicit `verification_script_reason`. The decompose
lint enforces the same three rules on what comes back — see
`verify_script_task_files_only`, `verify_script_docs_without_sync` and
`final_gate_without_script` in the rule table above. All three block; the
two script-content rules read the script file from the project directory and
are skipped when it does not exist yet.

**Correcting a script after decompose.** When a finding names a task's
verification script (`no_verification_script`, `unexecutable_verification_script`,
`final_gate_without_script`, or a script-content rule), you can fix that one
task without a re-decompose: set its script with `prd-edit-task
--verification-script <path>` (MCP `autonomous_prd_edit_task`, REST `edit_task`
body key `verification_script`, comm `edit-task … verification_script=<path>`,
or the **Verification script** field of the task editor), or state why it needs
none with `--verification-script-reason <text>`. The path must be one
repo-relative script file — a command string, absolute path, `..` path or the
project root is refused — and is added to the task's generated files. Edit the
script file itself first if it fails a script-content rule; the edit records the
path only. See [Recovering a blocked Automaton](../operations.md) for the
held-task flow.

### The script gate: the final gate no longer spends a session

There is **one** final gate per Automaton, on the **last** story, depending on
every task of every story. The standard-task pack applies it once there; a
planner-written `final_gate` on any earlier story is rejected by the plan check
at decompose time (the planner is asked to retry), and one that still remains is
dropped when the pack is applied (the other tasks' `depends_on` entries on it
are removed too). The
planner is told not to write a per-story gate: a story-level check is plain
task verification, and the one real gate closes the Automaton.

A `final_gate` used to be model labor: a worker session, a verifier, and
the report they wrote. The daemon needed no model for any of that, so it
now performs the gate itself. When the executor reaches a task of kind
`final_gate` — the template-appended one, a planner-written gate, or a
custom template such as `handoff_gate` — it runs the **script gate**: it
resolves the declared checks, runs them under one shared deadline, writes
the verdict and evidence to the task's report path, and completes or
fails the task. No worker session is spawned, no verifier runs and there
is no retry loop, whatever the mode allowed for it. The mode keeps its
meaning for the lint and the verifier exemption — what is gone is *who
performs the verification*.

**What runs — strict preference, and a higher step takes the whole run:**

1. the task's own `verification_script` — when it is a path under
   `.datawatch/verify/`; it runs alone (a path that is valid but whose file
   is missing fails the gate rather than falling through); or
2. the **project gate command** — the project's `policy
   project_gate_command` first, else the global
   `autonomous.project_gate_command` (the first set one wins); it runs as a
   single command through `sh`, alone; or
3. (only if neither of the above is available) the `verification_script`
   of **every** other task of **every story of the Automaton**, in story
   order, until the first failure. Standard-task (template) tasks and other
   final gates are skipped, so the gate never runs itself or a sibling.

The collection in step 3 spans the **whole Automaton**, not just the gate's
own story: with one final gate at the end, it runs every other task's script
from every story, so a story-level script is never skipped.

Steps 1 and 2 are exclusive — a command or the task's own script replaces the
story's scripts, it does not add to them — while step 3 runs all the scripts
it finds across the whole Automaton, each with the time still left on the shared deadline.

Every script the gate runs must resolve inside the project's
`.datawatch/verify/` — anything outside (or the project root itself) is
refused and dropped from the run, and nothing is written. A command string
is not a check at all: templates carrying one are refused before a task
exists (§5), and the gate command is the one place a raw command is run —
deliberately, and only through the gate.

**Deadline and report.** One shared deadline covers the whole gate run —
all checks together, not each one: `autonomous.script_gate_timeout_seconds`,
default **900** seconds; `0` removes the cap, and a negative value is
refused on write; when the deadline hits, the running check's whole process
group is killed, so nothing it spawned survives. Each check's combined
stdout and stderr goes into its own report section, and the report — every
section plus the final verdict — is written once, when the gate run ends, to
the task's own `report_path` —
`.datawatch/reports/\<feature\>-final-gate.txt` for the
template-appended gate — or, for a custom gate task that carries no path,
`.datawatch/reports/final-gate.txt`. So the evidence of what ran is on
disk before you read the task's status.

**Verdict.** A clean pass completes the task. A failed check, a check that
hit the deadline, or a gate the daemon could not run at all (no usable
script, no gate command) fails it, with the reason on the task: **a gate
with nothing runnable is a failed task, not an unclosed story**, and a
decision record is added either way saying what the gate did.

**Setup — three settings:**

| Setting | Meaning |
|---|---|
| `autonomous.project_gate_command` | the global gate command, run through `sh` — the repo's canonical entry point, e.g. `make check`. Empty by default, in which case the gate runs the scripts it finds |
| `policy project_gate_command` (per project) | the per-project gate command; wins over the global one for that project |
| `autonomous.script_gate_timeout_seconds` | the shared deadline for the whole gate run (integer seconds; 900 default, 0 = no cap; negative refused) |

The rule of thumb: declare what the gate should check — the gate command
per project or globally for the canonical entry point, or precise scripts
under `.datawatch/verify/` on the Automaton's tasks — and the Automaton's
closing gate stops spending a session.

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
- **Automata page → Templates tab →
  Standard tasks sub-tab** — the built-in pack by its operator-facing name
  (previously "task templates"); the same list the reads below return. The sub-tab
  is read-only: a per-task table (title, kind, summary, when offered, on/off, mode)
  with a note that explains the three modes (`llm`, `daemon`, `off`) and points at
  `autonomous.task_templates.mode` (the per-task selects live in Settings under
  **Standard task modes**; see [Template mode](#template-mode-who-runs-the-task)), an
  explanatory empty state when there is nothing to list, and stacked labelled rows at
  phone width; an empty list on the comm channel replies "no standard tasks are defined."
  The sub-tab has no runner column yet.
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
— a table of `ID TITLE KIND ENABLED MODE RUNNER`; `--json` prints the raw
`GET /api/autonomous/standard-tasks` body.

### Comm channel — `autonomous lint`

The same five verbs, one operator-facing wording everywhere (the CLI and
the comm channel share the finding-line renderer): `autonomous lint
config`, `lint config-set mode=enforce max_retries=2 disabled_rules=a,b`,
`lint dry-run <id>`, `lint findings <id>`. The standard-task list is the
word `standard tasks` (or `autonomous standard-tasks`): one line per
standard task — `id | title | on|off | mode | runner`; an empty list replies "no
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
- **Never spawns a session for the final gate.** The [script gate](#the-script-gate-the-final-gate-no-longer-spends-a-session)
  performs it: complete or failed, with the report. `off` removes the
  gate itself — that is a template-mode choice, not a fallback to a
  session.

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
6. Once the decompose looks right, **set the gate up**: declare the
   closing check the final gate should run — the project gate command (per
   project or globally) or a script under `.datawatch/verify/` — so the
   Automaton's one final gate runs it with the daemon doing it and closes
   without a session.
