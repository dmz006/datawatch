# [sync] draft(automaton): standard tasks shown under the Automata page's Templates tab

**Repo:** dmz006/datawatch-app
**Status:** DRAFT — not yet filed. This file write only; it never runs any hosting CLI and creates nothing on any host.

The issue text between the two body markers below is the issue body to file once the parent release lands. The 2026-11-08 operator decision (recorded in the datawatch howto for decompose lint) is that the PWA's Automata page shows the built-in standard tasks under the **Templates** tab, whose first sub-tabs are *Automaton templates* and *Standard tasks*. The Android and iPhone clients must match that surface.

<!-- body-start -->
## What this is

datawatch's decomposer appends a fixed, deterministic set of **standard tasks** to every story — the built-in pack of `changelog_entry`, `parity_issue_draft`, `docs_set`, `bounded_load_step`, `final_gate` and `version_bump_left_to_merge`, with operator overlays and per-project on/off overrides. They are operator-visible configuration surface, and the PWA shows them under the Automata page's Templates tab in a *Standard tasks* sub-tab (alongside the existing *Automaton templates* sub-tab of reusable PRD-template definitions).

## Current behavior

The daemon already exposes the resolved standard-task list as a read-only read on four surfaces, all rendering the same deterministic listing:

- **REST** — `GET /api/autonomous/standard-tasks`, optional `?project=<dir-or-alias>` to resolve that project's override; body is `{enabled, tasks[]}`; any other method is 405.
- **MCP** — the `autonomous_standard_tasks` tool, optional `project` argument, same body.
- **CLI** — `datawatch autonomous standard-tasks [--project <dir-or-alias>] [--json]`: a table of `ID TITLE KIND ENABLED MODE`; `--json` prints the raw body.
- **Chat** — the word `standard tasks` (or `autonomous standard-tasks`): the global switch, then one line per task, `id | title | on|off | mode`; an empty list replies "no standard tasks are defined."

Each task row carries `id`, `title`, `kind`, `summary`, `offered_when`, the effective `enabled`, `project_override` (per-project override, absent when the project has none) and `mode` — `model session` for every row. The list is built from the same definitions the decomposer applies; the summaries come from a static per-id map (operator templates with no entry fall back to their own title). No model call is made to produce or summarize the list, and nothing in the standard-task work touches a hosting tracker: the `parity_issue_draft` standard task is a draft file write only.

The gap: the Android and iPhone clients have no surface for this list at all. The Templates tab, where both clients mirror the PWA's operator templates (`/api/autonomous/templates`), says nothing about the built-in standard tasks the decomposer will add to every story, and today that is only discoverable by reading the datawatch howto or asking the chat/CLI/MCP surfaces.

## Expected behavior

- The Templates tab of the Automata page (or the equivalent screen in each client) gains a *Standard tasks* sub-tab beside *Automaton templates*, per the operator decision, in the same client wording. Never "template tasks"; the config keys keep the code-level name `autonomous.task_templates.*`, but operator-facing text says *standard tasks*.
- The sub-tab reads `GET /api/autonomous/standard-tasks` (optionally with `?project=` when the client knows the active project) and shows each of the built-in six — with any operator overlays resolved — as a read-only row: id, title, kind, summary, on/off state for the project, and the mode (`model session`). It is a read; there is no create/edit/delete control on this sub-tab, and it should not pretend there is one.
- Gating and empty state: when the daemon answers 503 (autonomous disabled) or the read fails, hide the sub-tab's content rather than showing a stale list; when the body's `enabled` is `false` or `tasks` is empty, show a plain empty state ("no standard tasks are defined"), never an error.
- This is read-only capability parity; the clients must not draft or apply standard tasks, must not call any hosting CLI on its account, and the `project_override` distinction (a per-project switch that exists versus none) is display detail — render the effective `enabled` as the on/off state and treat `project_override` as an optional annotation.

## Acceptance

- Both the Android and iPhone clients show the standard-task list under the Templates tab (or the platform's idiomatic equivalent), matching the PWA's *Standard tasks* sub-tab wording and row content.
- The list matches `GET /api/autonomous/standard-tasks` — the same rows the PWA, MCP, CLI and chat surface report, including any operator overlays in their resolved order.
- 503 / failure hides the sub-tab (no stale state) and an empty task list shows the empty state, not an error.
- No write, no tracker call, no LLM call on either side of this surface.

Parent: datawatch feat(automaton) standard-task listing — `GET /api/autonomous/standard-tasks`, the `autonomous_standard_tasks` MCP tool, `datawatch autonomous standard-tasks`, and the chat word `standard tasks`.
<!-- body-end -->

## Filing record

- Drafted 2026-11-08 as part of the standard-task work (the deterministic listing and its four read surfaces). Not filed: this session is a file-write-only task and never runs the hosting CLI; file it in dmz006/datawatch-app from a session that can, after the parent release lands, per the Mobile-Parity Rule.
