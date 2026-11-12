---
docs:
  index: true
  topics: [skills, registry, install, authoring, coverage]
exec_params:
  - {name: registry, required: true, description: "Registry name to search and install from (for example 'community')"}
  - {name: skills, required: false, default: "all", description: "Comma-separated skill names to install; 'all' to install everything"}
exec_steps:
  - tool: skills_registry_list
    description: Show the registries already configured
    args: {}
    read_only: true
  - tool: skills_registry_available
    description: List the skills the chosen registry offers
    args: {name: "{{params.registry}}"}
    read_only: true
  - tool: skills_registry_sync
    description: Install the requested skills from the registry
    args:
      name: "{{params.registry}}"
      skills: "{{params.skills}}"
    read_only: false
  - tool: skills_list
    description: Confirm the installed skills
    args: {}
    read_only: true
---
# How-to: Find, install and write a skill

This page covers the whole life of a skill: finding one in a registry, installing it,
writing a new one, reading the coverage matrix that says which features have a skill,
and keeping that matrix current. For syncing registries in detail see
[`skills-sync.md`](skills-sync.md).

## Finding a skill

Skills live in registries, which are git repositories. Two ship preconfigured: the PAI
registry and the community registry.

1. List the registries: MCP `skills_registry_list`, CLI `datawatch skills registries`.
2. List what one registry offers without installing anything: MCP
   `skills_registry_available` with the registry name, or the Skills page in the web app.
3. Read one skill before installing it: MCP `skills_get`, CLI `datawatch skills get <name>`.

`docs/skills/registry-skills.txt` is the repository's copy of the published community skill
names. It is only a list for the coverage check; the registry itself is the source of truth.

## Installing a skill

1. Sync the registry, naming the skills you want: MCP `skills_registry_sync`, CLI
   `datawatch skills registry-sync community`. Synced copies land in the daemon's skills folder.
2. Confirm with MCP `skills_list`.
3. Use it. Name it when a session starts, attach it to a project profile, or pull it into a
   running session with MCP `skill_load`. `skill_load` asks the operator to confirm before it
   copies a skill into a workspace that is not the default one.

Do not edit the synced copy. The next sync overwrites it. Change the registry instead.

## Writing a skill

A skill is a folder. In the staging area it is `internal/skills/testdata/staging/<name>/`:

```
<name>/
  SKILL.md          frontmatter plus the instructions
  USAGE.md          plain usage notes: what it is for, inputs, one example
  tests/cases.yaml  at least three cases
```

### SKILL.md frontmatter

The parser is `internal/skills/manifest.go`. It tolerates unknown keys and keeps them.

| Field | Meaning |
|---|---|
| `name` | Required. Must equal the folder name. |
| `description` | Required for staged skills. One sentence on what it does. |
| `version` | Required for staged skills. A short integer-like string is enough. |
| `tags` | Words used to find the skill. |
| `entrypoint` | The file the session reads first, if not `SKILL.md`. |
| `compatible_with` | Engines the skill suits; `[any]` for all. |
| `requires` | Other skills this one needs. |
| `applies_to` | Narrows when the skill attaches: `agents`, `session_types`, `comm_channels`. Empty means any. |
| `cost_hint` | `low`, `medium` or `high`. |
| `disk_mb` | Disk the skill needs. |
| `verify` | A command that checks the skill after sync. |
| `provides_mcp_tools` | MCP tools the skill declares. |
| `guardrails`, `guardrail_profile` | Guardrails the skill provides, and a profile to turn on with it. |
| `sampling_hook` | A template run after the skill, to ask for a summary of its output. |
| `accepts_images` | True when the skill can receive image descriptions in its task text. |

### Rules for a generic skill

A skill staged for the community registry must work for anyone, so it must not carry anything
that belongs to one installation. `go test ./internal/skills/` fails a staged folder that has:

- the product name, or the internal word for an Automaton
- tracker ids, version strings or semantic version numbers
- private network addresses
- absolute home paths or a leading tilde path
- email addresses

Write inputs as named placeholders (`PROJECT_DIR_OR_ALIAS`, `TASK_PROMPT`) that the user
supplies, ask once when one is missing, never guess, and require explicit confirmation before
anything destructive. Report results by name, not by identifier.

### USAGE.md and tests/cases.yaml

`USAGE.md` says what the skill is for, lists the inputs, and shows one prompt with its result.
`tests/cases.yaml` is a list under `cases:`; each case has a `name`, a `prompt`, an `expected`
list of behaviours and a `forbidden` list. At least three cases are required, and they should
cover the normal path, a missing input and a risky action.

Run the format and leak check:

```sh
go test ./internal/skills/ -run TestStagingSkills
```

Publishing is a separate step; see [`../skills/publish-instructions.md`](../skills/publish-instructions.md).

## Reading the coverage matrix

Two files describe how well skills cover the product:

- `docs/skills/inventory.yaml` is the **feature inventory**: one entry per area (sessions,
  memory, federation and so on) listing its MCP tools, how-tos, REST routes, commands and settings.
- `docs/skills/coverage-matrix.yaml` is the **coverage matrix**: one row per inventory entry,
  naming the skills that cover it, a status and a note.

| Status | Meaning |
|---|---|
| `none` | No skill covers the entry. This is a gap. |
| `partial` | A skill covers part of the entry; the note says which part is missing. |
| `full` | Skills cover every tool and how-to in the entry. |

Gaps are the rows with `none` or `partial`. Start there when choosing what to write next. A
skill named in a row must be a staged folder or a name in `docs/skills/registry-skills.txt`.

## Keeping it current

```sh
bash scripts/skills-coverage-check.sh
bash tests/ci/test-skills-coverage-check.sh
```

The first prints `ok: skills coverage` or one `FAIL:` line for each drift. When it fails:

- **A new MCP tool appears.** Add its name to the `tools` list of the right inventory entry. If
  it opens a new area, add a new entry and a matrix row for it.
- **A new how-to appears.** Add its file name without `.md` to the `howtos` list of the right
  entry. This page itself is listed under `skills`.
- **A new route, command or setting appears.** Add it to the same entry's `routes`, `commands`
  or `settings` list.
- **A skill is staged or published.** Put its name on the matching row's `skills` list, update the
  status and note, and when it is published refresh `docs/skills/registry-skills.txt`.

Also run `bash scripts/check-howto-coverage.sh` after adding a how-to; it requires the
`exec_steps` front-matter above.

## Common pitfalls

- **A skill name fails the coverage check.** The row names a skill that is not staged or listed in
  `registry-skills.txt`.
- **The staged folder fails its test.** The `name` does not equal the folder name, a file is empty,
  there are fewer than three cases, or a file contains one of the banned items above.
- **`skills_registry_sync` installs nothing.** Check the registry name with `skills_registry_list`.

## See also

- [`skills-sync.md`](skills-sync.md)
- [`../skills.md`](../skills.md)
- [`../skills/publish-instructions.md`](../skills/publish-instructions.md)
- [`../datawatch-definitions.md`](../datawatch-definitions.md)
