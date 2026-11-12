---
docs:
  index: true
  topics: [automaton, automata, standard tasks, daemon mode, script gate, project gate, settings]
exec_params:
  - {name: task_templates_mode, required: false, description: "Per-template modes, as id=mode pairs or a JSON object, e.g. changelog_entry=daemon,final_gate=llm"}
  - {name: script_gate_timeout_seconds, required: false, description: "Time cap on one script gate run, in whole seconds (0 = no cap, default 900)"}
  - {name: project_gate_command, required: false, description: "The project gate command, one line of shell text (empty = unset)"}
exec_steps:
  - tool: autonomous_config_get
    description: Read the current standard task modes, script gate time cap and project gate command
    args: {}
    read_only: true
  - tool: autonomous_config_set
    description: Set one or more of the three daemon task settings; a refused value stores nothing
    args:
      task_templates_mode: "{{params.task_templates_mode}}"
      script_gate_timeout_seconds: "{{params.script_gate_timeout_seconds}}"
      project_gate_command: "{{params.project_gate_command}}"
    read_only: false
------

# How-to: daemon task modes and the script gate

An Automaton is planned as stories and tasks. The planner adds some of those
tasks from **standard tasks** (a changelog entry, a parity issue draft, a docs
set, a final gate and so on). Each template runs in one of three **modes**:
an LLM session, the daemon itself, or not at all. A closing check, the **script
gate**, is run by the daemon with no session. This how-to covers the modes and
the defaults, what no longer starts a session and how to bring a session back,
the script gate, and the three settings that shape the gate.

## 1. The three modes, per template

`autonomous.task_templates.mode` is a map from template id to mode. Each
built-in template has a default. Empty or missing ids fall back to these
defaults.

| Mode | Meaning |
|---|---|
| `llm` | A worker session carries the task out as a normal task, verifier included. |
| `daemon` | The daemon carries the task out itself. No session, no verifier, no retry loop. |
| `off` | The template is not appended to the story at all. |

The six built-in templates and their defaults:

| Template id | Default mode | Accepted modes | What it adds |
|---|---|---|---|
| `changelog_entry` | `daemon` | `llm`, `daemon`, `off` | a changelog entry for the feature |
| `parity_issue_draft` | `daemon` | `llm`, `daemon`, `off` | a parity-gap draft file |
| `bounded_load_step` | `daemon` | `llm`, `daemon`, `off` | a posture note on bounded load |
| `version_bump_left_to_merge` | `daemon` | `llm`, `daemon`, `off` | a note that the release version is resolved at merge |
| `docs_set` | `llm` | `llm`, `off` | the feature's docs, over the story's own files |
| `final_gate` | `llm` | `llm`, `off` | the closing check, run by the script gate (section 4) |

`daemon` is accepted only for the four daemon-capable ids. Writing `daemon` for
`docs_set` or `final_gate` is refused, and nothing is stored.

**Docs stay on `llm`.** `docs_set` needs a model to write prose, so its default
remains `llm`. Setting it to `off` removes it from the story.

## 2. Which templates run in the daemon without starting a session

Four templates default to `daemon`. The daemon performs each one itself and
marks it complete, through the same status path a worker task takes, so the
story's roll-up counts it. None of them shows a worker session in the session
list.

- **`changelog_entry`** and **`parity_issue_draft`** write the generated
  artefact. The daemon merges the change records of the story's completed tasks
  and writes the same tracked file the generated-artefacts feature writes (see
  [generated-artefacts.md](generated-artefacts.md)). An empty merge still
  renders a minimal entry. A file you have edited by hand is left alone, and the
  task completes with the skip reason in its decision note. If the kind is
  switched off in the generated-artefact settings, nothing is written and the
  task says so. If the project has no directory, the task fails with the reason.
- **`bounded_load_step`** and **`version_bump_left_to_merge`** write no files.
  The spec text is the note. The task completes with a decision note holding
  that text, which is the record an operator reads back on the story. The
  version note never edits the tree.

The daemon-mode tasks skip the decompose lint and the plan-structure checks,
because the daemon performs them rather than a judge.

## 3. What no longer starts a session

Two sets of tasks no longer start a model session by default. Both are listed
here so you can see where a session used to run and how to bring it back.

**The four daemon-default templates.** `changelog_entry`, `parity_issue_draft`,
`bounded_load_step` and `version_bump_left_to_merge` now run in the daemon.
To turn any one of them back into a session task, set its mode to `llm`:

```
task_templates_mode: "changelog_entry=llm"
```

The task then runs as a normal worker task, verifier included. The two note
templates keep their note kind, so a session runs them as a note and changes no
files.

**The final gate.** `final_gate` never starts a session in any allowed mode.
`llm` is its default, but for this template `llm` means the script gate
(section 4), not a worker session. `off` removes the closing gate from the story
entirely. Because `daemon` is refused for `final_gate`, there is no mode that
turns it into a session task. If you want a session-run closing check, write a
custom template with a different kind (for example `docs`) and give that
template the mode `llm`.

To change a mode over MCP, use `autonomous_config_set` with
`task_templates_mode`; the Settings page's per-template select, the CLI
`datawatch config set autonomous.task_templates.mode.<id> <mode>`, chat or
comm `configure autonomous.task_templates.mode.<id>=<mode>`, and REST
`PUT /api/config` with that one key do the same. These merge one mode onto
the stored map, so an id you leave out keeps its mode, and `enabled`,
`templates` and `projects` are untouched.

A write through the decompose-lint configuration surface (the MCP
`autonomous_decompose_lint_config_set` tool, REST
`PUT /api/autonomous/decompose-lint/config`, or the Settings page's
decompose-lint panel) replaces the whole `task_templates` block instead: a
block that carries only `enabled` resets `mode`, `templates` and `projects`,
and ids fall back to their shipped defaults. The decompose-lint panel is
exactly such a write — it sends only its enabled checkbox — so saving it
resets your per-template modes; check your modes after you save there. If
you write the block yourself through that surface, read the block back
first, change the mode map, and write the whole block.

## 4. The script gate

When the executor reaches a task of kind `final_gate`, the daemon runs the
**script gate**. It resolves the declared checks, runs them, writes the verdict
and the evidence to the task's report, and completes or fails the task. No
worker session is spawned, no verifier runs and there is no retry loop.

**What runs.** The preference is strict, and a higher step takes the whole
run over a lower one:

1. the task's own verification script, when it is a path under the project's
   `.datawatch/verify/` folder. It runs alone — a valid path whose file is
   missing fails the gate with "script missing", it does not fall through;
2. otherwise, the **project gate command**: the project's own policy value first,
   then the global `autonomous.project_gate_command`. It runs alone, as one line
   of shell text through `sh`;
3. otherwise, only if neither of the above is available, **every**
   verification script the story's other tasks declare, in story order,
   until the first failure. Steps 1 and 2 are exclusive — a higher step
   replaces the lower ones, it does not add to them — while step 3 runs all
   the scripts it finds.

Every script the gate runs must resolve inside `.datawatch/verify/`. A script
path outside that folder, or the project root itself, is refused and dropped
from the run, and nothing is written for it.

**Exit status decides.** Each check's combined stdout and stderr goes into its
own report section, and the report — every section plus the final verdict — is
written once, when the gate run ends. A clean pass (every check exit status
zero) completes the task; a failed check fails it and stops the run. The
report is the task's own `report_path`. For the
template-appended gate that is `.datawatch/reports/<feature>-final-gate.txt`. A
custom gate with no path writes `.datawatch/reports/final-gate.txt`. The
evidence is on disk before you read the task status. A decision record is added
either way, saying what the gate did.

**A gate the daemon cannot run.** If the gate has nothing runnable (no usable
script and no gate command), the task fails with that reason named. A gate
with nothing to run is a failed task, not an unclosed story.

## 5. The time cap: `autonomous.script_gate_timeout_seconds`

One shared deadline covers the whole gate run, all checks together, not each
check on its own.

- Whole seconds. The default is **900**.
- `0` removes the cap. The gate may run as long as its script needs.
- A negative value is refused on write, and the refusal text is returned. Nothing
  is stored.
- A check that hits the deadline fails the task, and the running check's whole
  process group is killed — nothing it spawned survives the deadline.

Keep the cap above the longest honest run of your build-and-test command.

## 6. The project gate command: `autonomous.project_gate_command`

This is the project's own build-and-test command, run by the script gate as one
line of shell text, for example `make check` or `npm test`.

- **Global.** `autonomous.project_gate_command` applies to every project that
  has no value of its own. It is empty by default.
- **Per project.** A project's policy block can set `project_gate_command`
  (`projects.<name>.policy.project_gate_command`). That value wins for that
  project only. An explicit empty string there unsets the command for that
  project, even when the global command is set.
- **Unset.** When neither the policy nor the global value is set, the gate falls
  back to the scripts it finds (section 4, step 3). If there are none, the task
  fails with the reason named.
- **Refused values.** A value with a line break, a NUL byte, or more than 1024
  bytes is refused, and nothing is stored.

## 7. Where the settings live

The three keys sit in the autonomous block of the configuration:

- `autonomous.task_templates.mode`
- `autonomous.script_gate_timeout_seconds`
- `autonomous.project_gate_command`

You can read and set them with the `autonomous_config_get` and
`autonomous_config_set` MCP tools, with the Settings page, or with the config
API.

## 8. What a refused write looks like

Every write is checked by the daemon before anything is stored. A refused write
returns an error with the reason as plain text (HTTP 400 over the REST surface,
a tool error over MCP). A write that refuses one value stores none of the
values in that request.
