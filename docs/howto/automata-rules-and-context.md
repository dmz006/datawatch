---
docs:
  index: true
  topics: [autonomous, automata, rules, context, digest, planning record, rules file, context files]
exec_params:
  - name: project_dir
    description: Optional project directory whose override you want to read
exec_steps:
  - tool: rules_context_status
    description: Read the rules file, context files, digest mode and digest size limit resolved for a project (read-only)
    args:
      project_dir: "{{params.project_dir}}"
    read_only: true
---

# Automata rules and context

Tell Automata which project files hold your working rules and background, and how much of them is handed to a worker that cannot read files itself.

## What you can set

- **Rules file** — one file name, relative to the project directory (for example `AGENT.md`).
- **Context files** — up to eight file names, one per line, relative to the project directory.
- **Digest mode** — `off`, `no_tools` (only for backends without file tools) or `all`.
- **Digest size limit** — 0 for the default, or 512 to 65536 bytes.
- **Per-project overrides** — a project directory plus the same fields; the override wins for that project.

## Change it

1. Open **Settings** and find the **Rules and context** group beside the Automata checks group.
2. Edit the fields, add or remove project overrides, and save. A rejected value shows an error under the card.
3. The same settings are available through `GET` and `PUT /api/autonomous/rules-context`, the `rules_context_status` and `rules_context_set` MCP tools, and `datawatch autonomous rules-context`.

## See what a plan used

Open an Automaton. The **planning record** lists each file with a short hash and size, a warning for every missing file, and whether a digest was injected. File content is never shown.

See also [the definitions page](../datawatch-definitions.md#rules-and-context).
