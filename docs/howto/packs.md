---
docs:
  index: true
  topics: [task packs, pack, standard-task pack, pack inputs, pack trust, pack accept, pack install, pack remove, skills registry, builtin pack, automaton templates, secret input, auto-accept, first-party source, pack reporting]
exec_params:
  - {name: source_dir, required: false, description: "Folder on the daemon host holding a pack.yaml and its task files, to install"}
  - {name: name, required: false, description: "Pack name, e.g. builtin or an installed pack's name"}
  - {name: version, required: false, description: "Pack version, e.g. 0.1.0"}
exec_steps:
  - tool: packs_list
    description: List the installed packs with name, version, digest, trust state and a summary (read-only)
    args: {}
    read_only: true
  - tool: packs_setting_get
    description: Read the packs setting: {enabled, active[]}
    args: {}
    read_only: true
  - tool: packs_setting_set
    description: Turn packs on or off and/or set the ordered active pack list (a field left out is unchanged)
    args: {enabled: true}
    read_only: false
  - tool: packs_install
    description: Install a pack from a folder on the daemon host; the pack is not usable until it is accepted
    args: {source_dir: "{{params.source_dir}}"}
    read_only: false
  - tool: packs_accept
    description: Record operator acceptance of an installed pack version so it may run
    args: {name: "{{params.name}}", version: "{{params.version}}"}
    read_only: false
  - tool: packs_remove
    description: Remove one installed version of a pack (its folder and its trust record)
    args: {name: "{{params.name}}", version: "{{params.version}}"}
    read_only: false
  - tool: packs_inputs_get
    description: Read the stored inputs of one pack at one level; secret inputs come back redacted
    args: {level: global, pack: "{{params.name}}"}
    read_only: true
  - tool: packs_inputs_set
    description: Replace the stored inputs of one pack at one level; every name must be an input the pack declares
    args: {level: global, pack: "{{params.name}}", inputs: {}}
    read_only: false
---

# Task packs

A **pack** is a named, versioned folder that ships standard tasks and Automaton
templates together: the task prompts, their verification scripts and the template
definitions, plus any values (inputs) the operator must supply. The built-in
standard tasks are the first pack, named `builtin`; its definitions are embedded in
the binary and it needs no install. Other packs are authored as plain folders,
installed onto a daemon, trusted by the operator, and then removed when they are no
longer wanted.

Packs are **off by default** (`packs.enabled: false`), and with packs off only
`builtin` applies whatever any list says. Everything in this guide works through the
daemon, so all surfaces behave alike: they call the same REST routes, so the same
refusals, the same stored state, and the same trust records apply on every channel.

## How the pieces fit

```
 author a pack                      install                         trust
┌─────────────────────┐        ┌───────────────────────────┐   ┌───────────────────────┐
│ pack folder         │  ───▶  │ <data dir>/packs/         │   │ trust.json (digest)   │
│ pack.yaml           │  rules │   <name>/<version>/       │   │ inputs.json           │
│ prompts/  verify/   │  copy  │ trust.json  inputs.json   │   │ registry auto-accept  │
│ templates/          │        └───────────────────────────┘   └───────────────────────┘
└─────────────────────┘                    │                              │
┌─────────────────────┐                    ▼                              ▼
│ reporting           │   packs.enabled + packs.active   the standard tasks and
│ usage, cost, audit  │   + pack inputs                  Automaton templates an
│ (computed from the  │   (global / project / automaton) Automaton receives
│  files, on demand)  │                                  (packs.ActivePacks)
└─────────────────────┘
```

There is exactly one place the work happens: the daemon's `/api/packs` routes. The
MCP tools, the CLI, the comm channel verbs and the web settings card all proxy to
them.

## Writing a pack

A pack folder holds:

```
my-pack/
  pack.yaml          # the manifest (required)
  prompts/           # one file per task, the task's spec text
  verify/            # verification scripts, one file per non read-only task
  templates/         # Automaton template definitions the pack ships
```

### The manifest

`pack.yaml` at the pack root:

```yaml
name: my-pack                 # required; a single safe folder name
version: 1.0.0                # required; a semantic version (MAJOR.MINOR.PATCH,
                              # optional pre-release / build suffix)
description: One line about what this pack adds.
kinds:                        # which Automaton kinds the pack applies to
  - feature                   # feature, bug, other, maintenance
tasks:
  - id: docs_set              # the template id an operator references the task by
    title: Docs set for {{feature}}
    prompt: prompts/docs-set.md
    verification_script: verify/docs-set.sh
    default_tier: small
    read_only: false
    file_scopes: [docs/**]
    tools: []
    kind: docs
    files: docs/**
    applies_when: "When the slice lands user-visible behavior."
    # read_only tasks instead carry:
    # read_only: true
    # report_path: .datawatch/reports/<file>
    # verification_script_reason: "A note that changes no files."
inputs:
  - name: api_base
    type: string
    required: true
    default: ""
    secret: false
  - name: token
    type: string
    required: false
    secret: true              # accepts only a ${secret:NAME} vault reference
templates:
  - my-automaton-template
```

What the manifest is checked for:

- **Name** is required and must be a single safe folder name (no separators, no
  traversal).
- **Version** is required and must be a semantic version.
- **Kinds** must be one of `feature`, `bug`, `other`, `maintenance`.
- **Task titles** must be unique within the pack.

### The rules every task must pass

Install checks **every** task against the standard-task rules and the **first** task
that fails refuses the whole install, naming the rule. Nothing is copied in that case.

- A task that **changes files** must name a `verification_script`. The script is one
  file path under `verify/` of the pack folder: no whitespace, no shell metacharacters
  (`;&|<>$` and friends), no path that leaves `verify/` — including through a symlink —
  and the file must exist, be a regular file, and start with a shebang line. A
  `verification_script` value that looks like a command is a rule failure, not a
  convenience.
- A **read-only** task must list no files and must name a `report_path` under
  `.datawatch/reports/`.

### Pack-wide install rules

- **Versions are never overwritten.** Installing a `name` + `version` that already
  exists is refused. Ship a new version as a new `version` in `pack.yaml` and install
  that.
- **Links stay inside the pack.** A symbolic link is followed only when it resolves to
  a regular file inside the pack folder; any other link refuses the install, so the
  copy can never pull in content from outside the pack.

### Declaring inputs

Each input has `name`, `type`, `default`, `required` and `secret` (see the example
above). A **secret** input never holds a literal value: the only accepted value is a
vault reference of the form `${secret:NAME}` (`NAME` is letters, digits, `_`, `.`, `-`).
A **required** input with no value at any level and no default is an error at resolve
time: `pack input <name> is required and has no value; set it at the automaton, project
or global level`.

## Installing a pack

Install copies the checked pack folder to the install root,
`<data dir>/packs/<name>/<version>/`, beside the `trust.json` and `inputs.json` state
files. The source folder must be on the daemon host.

| Surface | Command |
|---|---|
| REST | `POST /api/packs/install` with `{"source_dir": "/path/to/my-pack"}` |
| MCP | `packs_install` with `source_dir` |
| CLI | `datawatch packs install /path/to/my-pack` |
| Comm channel | `packs install /path/to/my-pack` |
| Web | the Packs sub-tab install button is built but not rendered yet; use one of the other surfaces |

What happens, in order: the folder is loaded (`pack.yaml` parsed), every task is
checked against the rules, the name is checked, the target is checked for an existing
version, and then — and only then — the folder is copied. An install failure names the
rule that refused it.

**Installing is not trusting.** A pack that is not from a first-party source is copied
onto disk but will not run until the operator accepts it (below).

## Trusting a pack (acceptance)

The install root holds a `trust.json`. A trust record names the pack, the version, the
**exact content digest** of the installed files, who accepted it and when.

- **Non first-party packs** run only when `trust.json` holds a record matching the
  pack's name, version *and current digest*. Change any one file in the pack and the
  digest changes; the old record stops applying and the pack is refused with
  `pack not accepted: accept it first`.
- **First-party sources** are skills registries flagged `first_party` (the community
  registry is one). A pack read from a first-party source runs without a per-pack
  record. Accepting the source writes one record per pack that is current at that
  time, and refuses the whole source if any pack is missing its digest or belongs to
  another source. A later version has no record and is judged by the version-diff
  rules.

### Version diff and auto-accept

When a new version of an accepted pack appears, its difference from the accepted
version is listed as **new tasks, new tools, new scripts and new file scopes**. A new
tool, script or file scope **widens access**; new tasks alone do not.

A registry source can opt in to **auto-accept** with `auto_accept_packs` (off by
default). When it is on, a new version is accepted automatically, recorded as accepted
by `auto:<source name>`, **only** when the diff adds no tool, script or file scope. A
version that widens access is never auto-accepted — the operator accepts it by hand.

### Accepting on each surface

Accept records the operator's acceptance of the installed digest; a pack that is not
installed is refused (404).

| Surface | Command |
|---|---|
| REST | `POST /api/packs/accept` with `{"name": "my-pack", "version": "1.0.0"}` |
| MCP | `packs_accept` with `name`, `version` |
| CLI | `datawatch packs accept my-pack 1.0.0` |
| Comm channel | `packs accept my-pack 1.0.0` |
| Web | the Packs sub-tab accept button is built but not rendered yet; use one of the other surfaces |

Before you accept, read the pack's **summary** (task titles in manifest order, the
verification script file names — never full paths — and the tools and file scopes its
tasks ask for): `GET /api/packs`, `packs_list`, `datawatch packs list`, or the comm
verb `packs`. It is the quick answer to "what would this pack allow?"

## Removing a pack

Removal deletes one installed version's folder **and its trust record** (the record is
removed first, so a failed delete leaves a pack that is on disk but no longer
runnable). It does **not** touch Automata, their tasks or trace records — an Automaton
keeps the tasks it was created with — and it does **not** edit `packs.active`: a
removed name stays in the list until you change it, and the next write of that list is
refused.

| Surface | Command |
|---|---|
| REST | `DELETE /api/packs/my-pack/1.0.0` |
| MCP | `packs_remove` with `name`, `version` |
| CLI | `datawatch packs remove my-pack 1.0.0` |
| Comm channel | `packs remove my-pack 1.0.0` (the name and version are checked to be safe) |
| Web | not on the web yet; use one of the other surfaces |

## The settings, and how to change each one

The `packs` block has two keys, `packs.enabled` and `packs.active`, and pack inputs
are stored at three levels. Every change below goes through the daemon and is checked
the same way on every surface. Reads need the config-read permission and writes the
config-write permission, including over federation.

### `packs.enabled` — the global switch (default `false`)

Turning it on is refused — HTTP 409 — with
`packs need at least one connected skills registry source; connect one in the skills
registry settings` when no skills registry source is connected and enabled (the
community registry is the default source). The web switch is put back to off and shows
the refusal. **A hand edit of the config file skips this check.** With the switch off,
the active list is always `["builtin"]`, whatever it says.

| Surface | How to change it |
|---|---|
| Config file | `packs.enabled: true` (or `false`) in the daemon config |
| REST | `PUT /api/packs/config` with `{"enabled": true}` |
| MCP | `packs_setting_set` with `{"enabled": true}` (give `enabled`, `active`, or both; a field left out is unchanged) |
| CLI | `datawatch packs enable` / `datawatch packs disable` |
| Comm channel | `packs setting enabled=true` (or `enabled=false`) |
| Web | Settings → General → **Standard task packs** → the enabled switch |

### `packs.active` — the ordered list of packs in use (default `["builtin"]`)

An entry is `builtin` or the name of an installed pack. Order is a priority order and
is never sorted. Every write is validated and refused (HTTP 400) when the list holds
an empty name, a repeated name, or a name that is neither `builtin` nor an installed
pack.

| Surface | How to change it |
|---|---|
| Config file | `packs.active: [builtin, my-pack]` |
| REST | `PUT /api/packs/config` with `{"active": ["builtin", "my-pack"]}` |
| MCP | `packs_setting_set` with `{"active": ["builtin", "my-pack"]}` |
| CLI | `datawatch packs enable`/`disable` change the switch; the list itself is set in the config file, or via REST, MCP, chat or the web |
| Comm channel | `packs setting active=builtin,my-pack` (`none` clears the list) |
| Web | Settings → General → **Standard task packs** → the active pack order list (comma separated) |

### Pack inputs — the three levels

A pack can declare inputs (above). Stored values live in `inputs.json` in the install
root, per pack, at one of three levels:

| Level | Key | Meaning |
|---|---|---|
| `global` | — | applies to every project and every Automaton |
| `project` | the project directory | applies to every Automaton in that project |
| `automaton` | the Automaton id | applies to that one Automaton |

Resolution (`packs.ResolveInputs`) takes the first level that defines the input —
Automaton, then project, then global — and falls back to the declared `default`. A
level "defines" an input when its map holds the name, so an empty string set at a
level still wins over a lower level. A required input with no value anywhere is an
error. Secret inputs are always stored as `${secret:NAME}` references (a literal value
is refused at write time) and reads show secrets as the reference or `***`.

A save **replaces** the scope it writes (the CLI merges by default and replaces with
`--replace`). Every name must be an input the pack declares — a name the pack does not
declare is refused. The built-in pack declares no inputs, so saving an input for
`builtin` is refused today.

| Surface | Read | Set |
|---|---|---|
| Config file | — (inputs are stored in the install root, not the config) | — |
| REST | `GET /api/packs/inputs?level=global&pack=my-pack` (add `&key=` at the project or automaton level) | `PUT /api/packs/inputs?level=…&pack=…[&key=…]` with `{"inputs": {"api_base": "https://…", "token": "${secret:MY_TOKEN}"}}` |
| MCP | `packs_inputs_get` with `level`, `pack` (+ `key`) | `packs_inputs_set` with `level`, `pack`, `inputs` (replaces the scope) |
| CLI | `datawatch packs inputs get <level> <pack> [name] --key <dir-or-automaton-id>` | `datawatch packs inputs set <level> <pack> name=value … --key <dir-or-automaton-id>` (add `--replace` to replace the whole scope) |
| Comm channel | `packs inputs get <global\|project\|automaton> <pack> [key]` | `packs inputs set <global\|project\|automaton> <pack> [key] name=value …` |
| Web | Settings → General → **Standard task packs** → the input fields | the four fields: **level** (`global` or `project` — the web form has no Automaton level; use REST, MCP, CLI or chat for that), **pack name**, **project key** (project level only) and **values** (`name=value; name=value`) |

`--key` (REST `key`, the web project-key field) is required for the `project` level
(the project directory) and the `automaton` level (the Automaton id); the `global`
level takes no key.

**Status of inputs:** values can be saved and read on every surface today, but task
creation does not read the stored values yet, and the built-in pack declares none — so
a saved value changes nothing until the wiring lands. When a daemon-run standard task
is created, the resolved inputs are redacted and stored on the task as `pack_inputs`;
a task keeps the values it was created with, and a later change to an input does not
alter it.

### Which packs apply to an Automaton

`packs.ActivePacks` resolves the ordered pack list for one Automaton:

- **Packs off** (`packs.enabled: false`): always `["builtin"]`, whatever any list says.
- **Packs on**: the first non-empty list wins — the Automaton's own list, then the
  project's list, then the global `packs.active`. The lists are **not merged**.
- **All empty**: the global default, `["builtin"]`.

An Automaton's own list is its `packs` field (set with `Automation.SetPacks`, which
trims, drops later repeats and keeps the given order). No surface sets that field yet,
and the project config entry has no `packs` key, so the two lists that can hold
entries today are the Automaton's own and the global one.

**Status:** management (list, install, accept, remove, the enable check, the
`packs.active` validation, inputs) works on every surface, but the resolver is not
yet given the resolved active list, so an installed pack does not yet change the tasks
an Automaton gets. Until then `builtin` is what applies.

## Reporting: the files are the source of truth

Pack reporting is the read side of a pack's name and version on the records the daemon
keeps:

- **Trace events** carry `pack_name` and `pack_version`; the trace roll-up groups by
  them (`dimension=pack_name`, `dimension=pack_version`).
- **Audit entries** carry a `pack_name` detail; the audit query filters on it.
- **The cost report** has a pack breakdown: one row per pack name and version (events,
  tokens, cost); events with no pack name fall into one `unattributed` row.
- **The usage summary** (`packs.UsageByPack`) counts runs (distinct sessions, tasks or
  Automatons), cost and last use per pack name and version, from the trace store.

These summaries are computed **from the files, on demand**. The **derived reporting
index is left for a later Automaton**: there is no pre-built index of pack usage or
attribution today, and none should be expected in the meantime — the trace events,
audit entries, `trust.json` and `inputs.json` remain the source of truth, and the
reporting pieces read them directly.

**Status:** this is read-side only today. No production code writes `pack_name` on an
audit entry, the `builtin` stamp helpers for trace events have no production caller,
and no surface calls the usage summary or the cost breakdown — so attribution
currently returns no pack rows (everything is `unattributed`) and the Packs sub-tab's
usage table has no route to read (`GET /api/packs/usage` is not registered). Until a
writer ships, treat the reporting rows as empty by design, not as an error.

## Where to look on the web

- **Settings → General → Standard task packs** — the enabled switch, the active pack
  order list, and the four pack-input fields (level, pack, project key, values). The
  card shows the enable refusal inline when no registry source is connected.
- **Automata → Templates → Packs** — the sub-tab shell. It reads `GET
  /api/packs/config`; with packs off it explains what packs are and shows an **Enable**
  button that opens the settings card; in both states it links to the skills registry
  settings. The install, accept, change-summary and usage pieces exist as scripts but
  are not rendered by the shell yet.

## Troubleshooting

- **`packs need at least one connected skills registry source…`** when enabling —
  connect and enable a skills registry source (the community registry by default) in
  the skills registry settings, then enable again. A hand edit of the config file
  skips this check, so a config that passed here will still load.
- **HTTP 400 on a `packs.active` write** — the list holds an empty name, a repeated
  name, or a name that is neither `builtin` nor an installed pack. Remove the offending
  name (for example after a `remove`) and write the list again.
- **`pack not accepted: accept it first`** — the pack's digest no longer matches a
  trust record (a new or modified version). Accept it, or let a source with
  `auto_accept_packs` accept a non-widening version.
- **Install refused** — the error names the rule: a task missing its verification
  script, a script that looks like a command, a read-only task that lists files, a
  duplicate task title, an unsafe name, an existing `name`/`version`, or a link that
  leaves the pack.
- **`input … must be name=value` / `--key is required`** — the `project` and
  `automaton` levels need the key (project directory or Automaton id); the `global`
  level takes none.
- **A saved input changes nothing** — expected today: task creation does not read
  stored inputs yet, and the built-in pack declares none.
- **The usage table shows an error** — the route is not registered yet; the usage
  summary is computed from the trace files, which remain the source of truth.

## For the operator to review

Open items that this how-to describes but the operator must still decide or check:

- **The resolver is not given `packs.active` yet** — an installed pack does not add
  or change tasks until that wiring ships; confirm the timing before relying on a pack.
- **No web list, install, accept or remove** — the Packs sub-tab is a shell today;
  use REST, MCP, CLI or the chat command for those actions.
- **Pack inputs are saved but not read** — task creation ignores stored inputs.
- **The usage route and the derived index are not live** — the trace files stay the
  source of truth until the later Automaton lands.
- **Trust is the operator's call** — review each pack's digest and version diff
  before accepting, and before turning on auto-accept for a source.

## See also

- [Standard tasks](automata-standard-tasks.md) — what a standard task is and how the
  built-in list is resolved
- [Decompose lint, standard tasks and guidance](automaton-decompose-lint.md) — the
  quality check the standard tasks back
- [Config reference: `packs`](../config-reference.yaml#packs)
- [Datawatch definitions: pack entries](../datawatch-definitions.md)
