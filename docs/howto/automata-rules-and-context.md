---
docs:
  index: true
  topics: [autonomous, automata, rules, context, rules file, context files, digest, rules digest, planning record, rules checklist, rules checklist gap, project override, new repository]
exec_params:
  - name: project_dir
    description: Optional project directory whose resolved values and override you want to read
  - name: rules_file
    description: Rules file name relative to the project directory, for example AGENT.md or RULES.md
  - name: context_files
    description: Context file names relative to the project directory, at most eight, for example ["CONTEXT.md", "DATAWATCH-CONTEXT.md"]
  - name: digest_mode
    description: off, no_tools or all
  - name: config_key
    description: One of autonomous.rules_context.rules_file, autonomous.rules_context.context_files, autonomous.rules_context.digest_mode, autonomous.rules_context.digest_max_bytes
  - name: config_value
    description: The value for config_key
exec_steps:
  - tool: rules_context_status
    description: Read the rules file, context files, digest mode and digest size limit resolved for a project, with the configured values and any per-project overrides (read-only)
    args:
      project_dir: "{{params.project_dir}}"
    read_only: true
  - tool: get_config
    description: Read the daemon settings, including the autonomous.rules_context block and the older autonomous.rules_file and autonomous.context_file keys
    args: {}
    read_only: true
  - tool: rules_context_set
    description: Name the rules file, the context files and the digest mode for the global values, or for one project when project_dir is given; only the fields you give change
    args:
      project_dir: "{{params.project_dir}}"
      rules_file: "{{params.rules_file}}"
      context_files: "{{params.context_files}}"
      digest_mode: "{{params.digest_mode}}"
    read_only: false
  - tool: config_set
    description: Set one autonomous.rules_context key by its dotted name
    args:
      key: "{{params.config_key}}"
      value: "{{params.config_value}}"
    read_only: false
---

# Automata rules and context

When the daemon plans an Automaton it does not know your repository. It reads
what you tell it to read: a **rules file** (how work is done here: tests,
documentation, naming, release steps) and optional **context files** (what the
project is and where it stands). This guide shows how to make a new
repository's rules and context reach the planner, and how to check that they
did. The terms are defined in the
[definitions](../datawatch-definitions.md#rules-and-context) and the operations
reference is in [operations.md](../operations.md#rules-and-context-for-planning).

## What the planner reads and why

Before it decomposes an Automaton, the daemon looks in the Automaton's project
directory for the rules file and each context file. For every file that is
there, the planning prompt gets a short instruction to read it before
planning. A backend that can read files (every session backend) follows the
pointer and reads the full file itself, with its own budget. A file that is
named in the settings and is not in the project directory is mentioned in the
prompt as not found, so the planner knows the rule is missing instead of
guessing.

The reason is simple: a plan that ignores your rules produces tasks with no
tests, no documentation step or a wrong name, and you find out after the work
is done. Reading the rules first moves that cost to the start.

## Set up a new repository

1. **Put the files at the project root.** Add a rules file and, if you want, a
   context file. The defaults are `AGENT.md` for the rules and `CONTEXT.md` for
   the context. If your files already have those names, you are done: nothing
   needs to be set.
2. **Name them in the settings when they are not the defaults.** In
   **Settings, General** open the **Rules and context** group, or use the
   commands below. This repository, for example, keeps its project state in
   `DATAWATCH-CONTEXT.md`, a renamed context file. You can list several files
   through `context_files`, at most eight, in the order you want them read.
3. **Check what was resolved.** Read the settings back and confirm the names
   are the ones you meant (next section), then run an Automaton and open its
   planning record.

File names are always relative to the project directory. A name that starts
with `/`, contains `..` or is empty is refused and nothing is changed.

### Set the names

```
datawatch autonomous rules-context set --project-dir /srv/my-repo \
  --rules-file RULES.md \
  --context-file CONTEXT.md --context-file DATAWATCH-CONTEXT.md
```

The same change, by every other surface:

| Surface | How |
|---|---|
| Web UI | **Settings, General, Rules and context** card; edit the fields, add a project override, save |
| CLI | `datawatch autonomous rules-context set ...` (above); `datawatch config set autonomous.rules_context.rules_file RULES.md` also works |
| Chat | `autonomous rules set rules_file RULES.md` or `autonomous rules set context_files CONTEXT.md,DATAWATCH-CONTEXT.md /srv/my-repo` (the last word is the project directory) |
| REST | `PUT /api/autonomous/rules-context` (optional `project_dir` query) |
| MCP | `rules_context_set` |
| Configuration file | the `autonomous.rules_context` block, below |

```
autonomous:
  rules_context:
    rules_file: AGENT.md
    context_files: [CONTEXT.md, DATAWATCH-CONTEXT.md]
    digest_mode: no_tools
    digest_max_bytes: 4096
    projects:
      - project_dir: /srv/my-repo
        rules_file: RULES.md
        context_files: [STATE.md]
```

After a change over a command or the API the new values apply to the next
Automaton that is decomposed; no restart is needed. After editing the
configuration file, run `datawatch reload`.

## Per-project override and the global fallback

The values for a project are resolved in this order, and the first layer that
gives a value wins, one field at a time:

1. The **override** for that project directory (`projects`).
2. The **global** `autonomous.rules_context` values.
3. The older `autonomous.rules_file` and `autonomous.context_file` keys, which
   still work for a setup made before this block existed. `context_file` names
   one file.
4. The defaults: `AGENT.md` and `CONTEXT.md`.

A project that renames only the rules file keeps the global context files and
digest settings. An empty value in an override means "inherit", not "none". To
see the result for one project, read the status with its directory:

```
datawatch autonomous rules-context --project-dir /srv/my-repo
```

## The planning record

When an Automaton is decomposed, the daemon stores a **planning record** on it:
what the planner was pointed at. It lists every file by name, kind (rules or
context), a short hash of the content and its size in bytes, a note for every
named file that was missing, and whether a rules digest went into the prompt
and how large it was. It never stores or shows file content. Two plans made
from the same file show the same hash; a changed hash tells you the rules moved
between two plans.

| Surface | Where |
|---|---|
| Web UI | Open the Automaton in the Automation view; the **Planning record** section lists the files, a warning for each missing file and the digest line |
| CLI | `datawatch autonomous rules-context --prd <automaton-id>` |
| Chat | `autonomous rules <automaton-id>` |
| REST | `GET /api/autonomous/rules-context?prd=<automaton-id>` (the `record` object) |
| MCP | `rules_context_status` with `prd` |

An Automaton decomposed before the record existed has none; the read says it
was not recorded.

## The rules digest

A backend without file tools (the headless ask-style backends such as Ollama
and Open WebUI) cannot follow a pointer to a file. For those the planning
prompt carries a **rules digest** instead: a short, deterministic summary of
the rules file.

- **What it contains.** Every heading of the rules file, in order, and under
  each heading only the lines that carry a mandatory word (must, mandatory,
  required, never, always or "no exceptions"). Long lines are cut. Fenced code
  blocks are skipped. For each context file only the headings are added.
- **The size cap.** `digest_max_bytes`, default 4096, allowed 512 to 65536.
  When the digest is too big, mandatory lines are dropped from the end first,
  then the headings are cut at a line boundary and a truncation note is added.
- **The modes.** `no_tools` (the default) adds the digest only for a backend
  without file tools. `all` adds it for every backend. `off` never adds it.
- **Cached by file hash.** The digest is rebuilt only when the file's content
  hash changes, so a repeated plan costs nothing extra.

## The per-task rules checklist

The planner is asked to give every task a `rules_applied` list: the sections of
your rules file the task must follow, such as tests, documentation,
localization, parity or versioning, using the section names from the rules
file or the digest. Only a read-only task may have an empty list.

After the plan is made, the structural check compares each task's list with the
areas the task touches (from its files and title): a task that edits web files
needs localization, one that changes a settings or interface directory needs
parity and configuration, one that edits documents needs documentation, a code
file needs tests, and a release task needs versioning.

### The "rules checklist gap" finding

The finding is advisory: it never fails the plan and never stops a task. It
appears beside the plan with the task title and the area names, never any file
content. It has two forms:

- *has an empty rules_applied list although a rules file was present*: the
  planner skipped the checklist for a task that changes files.
- *rules_applied is missing: documentation, tests*: the task touches those
  areas and its list does not name them.

To act on it, open the task and either add the missing rule to the task's spec
and `rules_applied` list, or request a revision of the plan with the finding as
feedback. If the finding is wrong (a task really does not touch that area),
approve the plan as it is. A gap on every task usually means the rules file was
not found: read the planning record first.

## Worker and verification prompts

The same pointer is added to the prompt of each worker session that runs a task,
worded as "read these before starting this task and follow them", and to the
prompt that asks for a task's verification script. A retried task is not given
the block twice. A worker backend without file tools gets the digest by the
same mode rule as the planner.

## Troubleshooting

- **File not found.** The planning record shows the name as missing. The name
  is relative to the project directory, not to the repository you are in: check
  the Automaton's project directory, and check the file name spelling and case.
- **Renamed file is ignored.** The settings still point at the old name, or the
  override for that project directory does not match the directory the
  Automaton uses. Read the status with that project directory and compare.
- **Digest too large or cut.** The digest was cut at `digest_max_bytes`. Raise
  the limit (up to 65536), or shorten the rules file's headings, or move
  rarely needed rules into a context file.
- **A headless backend ignores the pointer.** It has no file tools and cannot
  read the file. Keep `digest_mode` at `no_tools` or `all` so it receives the
  digest, and read the **digest injected** line in the planning record to
  confirm it did.
- **A setting is refused.** A name with `..` or a leading `/`, more than eight
  context files, an unknown digest mode, or a size outside 512 to 65536. The
  message names the key; nothing is changed.
