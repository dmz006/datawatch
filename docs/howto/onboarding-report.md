---
docs:
  index: true
  topics: [onboarding, setup, report, checks, tutorial, automata, autonomous]
exec_params:
  - {name: format, required: false, description: "text (default) or json"}
exec_steps:
  - tool: onboarding_report
    description: Read what is configured, what is missing and what to try next
    args: {format: "{{params.format}}"}
    read_only: true
---
# How-to: Onboarding report

The onboarding report tells you what is already set up, what is missing and what
to try next, so a new install does not need a hunt through settings. It is part
of the tutorial and onboarding material: start here after installing, and come
back after any big change.

It has three parts:

- **Configured**: the checks that pass.
- **Missing**: the checks that fail, each with the reason.
- **Try next**: ordered suggestions, one per missing check, each with a how-to
  link, the command, the MCP tool and the place in the PWA that does it.

## Read the report

- **PWA:** the Onboarding report card in the Automation area. Use the refresh
  control to run the checks again.
- **CLI:** `datawatch automation onboarding-report` (add `--format json` for the
  raw body).
- **MCP:** the `onboarding_report` tool, with `format` set to `text` (default) or
  `json`.
- **Chat:** the `onboarding-report` verb, optionally followed by `text` or `json`.
- **REST:** `GET /api/automation/onboarding-report`, with `?format=text` for plain
  text.

Every surface renders the same report from the same data.

## What each check means

| Check id | Passes when | If it fails |
|---|---|---|
| `backends` | At least one LLM backend is enabled. | Nothing can run tasks. Add a backend. |
| `models` | A model can be chosen from the enabled backends. | Refresh or add the models of a backend. |
| `projects` | At least one project directory is registered. | An Automaton has no place to work. Register a project. |
| `channels` | At least one comm channel is enabled. | Optional: Automation runs, but nothing reaches you outside the web app. |
| `memory` | Memory is enabled. | Optional: Automata cannot recall earlier work. |
| `merge_pipeline` | Every project has its own merge pipeline. | Optional: the project uses the built-in pipeline; see [merge pipeline](merge-pipeline.md). |

A check is either required (a failure blocks useful work) or optional (a
failure is a suggestion you may ignore).

## Switch checks off

Everything here is a setting, on by default and changed with `datawatch config
set`, the `config_set` MCP tool, the `configure` chat verb or Settings in the PWA.

| Key | Default | Effect |
|---|---|---|
| `onboarding_report.enabled` | `true` | Turns the report off. Every surface then says it is disabled and no check runs. |
| `onboarding_report.disabled_checks` | empty | A list of check ids to leave out, for example `memory` if you have chosen not to use it. An id that names no check is ignored. |
| `onboarding_report.include_next_steps` | `true` | Turn off to see only what is configured and what is missing. |

Each help link on the card opens the matching entry in
[the definitions](../datawatch-definitions.md#onboarding-report).
