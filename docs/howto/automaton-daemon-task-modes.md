---
docs:
  index: true
  topics: [automaton, automata, task templates, daemon mode, script gate, project gate, settings]
exec_params:
  - {name: task_templates_mode, required: false, description: "Per-template modes, as id=mode pairs or a JSON object, e.g. changelog_entry=daemon,final_gate=llm"}
  - {name: script_gate_timeout_seconds, required: false, description: "Time cap on one script gate run, in whole seconds (0 = no cap, default 900)"}
  - {name: project_gate_command, required: false, description: "The project gate command, one line of shell text (empty = unset)"}
exec_steps:
  - tool: autonomous_config_get
    description: Read the current task template modes, script gate time cap and project gate command
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

# How-to: daemon task modes, the script gate time cap and the project gate command

An Automaton is planned as stories and tasks. Some of the tasks it adds from
its **task templates** (changelog entry, parity issue draft, docs set and so
on) can be carried out by an LLM session, by the daemon itself, or not at all.
Three settings control that, plus how the gates run. You can set them on the
Settings page under **Autonomous Automata planning**, with the
`autonomous_config_set` MCP tool, or with `PUT /api/config`.

## 1. Task template modes

`autonomous.task_templates.mode` sets one mode per template id:

| Mode     | Meaning                                                                   |
|----------|---------------------------------------------------------------------------|
| `llm`    | An LLM session carries the task out. This is the default for `docs_set` and `final_gate`. |
| `daemon` | The daemon carries the task out itself. Offered only for the daemon-capable ids. |
| `off`    | The template is skipped.                                                  |

Daemon-capable ids (the Settings page offers `daemon` only for these):

- `changelog_entry`
- `parity_issue_draft`
- `bounded_load_step`
- `version_bump_left_to_merge`

`final_gate` is never run by the daemon. Its modes are `llm` or `off`.

On the Settings page each template has its own select. Changing one saves just
that template's key (`autonomous.task_templates.mode.<id>`).

Over MCP, give `task_templates_mode` as a JSON object or as `id=mode` pairs.
Entries merge onto the stored map, so an id you leave out keeps its mode:

```
task_templates_mode: "changelog_entry=off,final_gate=llm"
```

## 2. Script gate time cap

`autonomous.script_gate_timeout_seconds` caps how long one script gate run may
take before it is stopped.

- Whole seconds, `0` or more. `0` means no cap.
- Default `900`.
- A negative or fractional value is refused. The daemon answers with the
  reason, and the Settings page shows that text as the error. Nothing is stored.

## 3. Project gate command

`autonomous.project_gate_command` is the project's own build-and-test command.
The gate runs it as one line of shell text.

- Empty means unset.
- A value with a line break, or longer than 1024 bytes, is refused. The
  refusal text is shown on the Settings page and nothing is stored.

## 4. What a refused write looks like

Every write is checked by the daemon before anything is stored. A refused
write returns HTTP 400 with the reason as plain text. The Settings page shows
that text in an error notice and puts the control back to its last saved
value. Over MCP the same text comes back as a tool error. A write that refuses
one value stores none of the values in that request.

## 5. Where these settings live

The keys live in the autonomous block of the configuration:
`autonomous.task_templates.mode`, `autonomous.script_gate_timeout_seconds` and
`autonomous.project_gate_command`.
