---
docs:
  index: true
  topics: [automation, autonomous, automaton, queue, chain, advance, alerts]
exec_steps:
  - tool: automation_list
    description: List the Automations and their status
    args: {}
    read_only: true
  - tool: automation_get
    description: Read one Automation (status, members, edges, execution defaults)
    args: {id: "{{params.automation_id}}"}
    read_only: true
  - tool: autonomous_status
    description: Read the status rows for the Automations, with the current member and idle time
    args: {}
    read_only: true
  - tool: automation_activate
    description: Activate a draft Automation so the coordinator runs its chain
    args:
      id: "{{params.automation_id}}"
    read_only: false
  - tool: automation_set_llm
    description: Give the Automation its own execution backend/model/effort defaults
    args:
      id: "{{params.automation_id}}"
      backend: "{{params.backend}}"
      model: "{{params.model}}"
    read_only: false
  - tool: automation_set_verification_llm
    description: Give the Automation its own verification LLM default
    args:
      id: "{{params.automation_id}}"
      backend: "{{params.backend}}"
      model: "{{params.model}}"
    read_only: false
---

# Automation queue advance

An **Automation** is a named chain of Automatons. Each member says what runs
next when it completes, is blocked, or fails. Once an Automation is
**active**, datawatch starts the next Automaton by itself when the current one
finishes. No chat session, LLM or operator call is involved in the hand-off.

## Base requirements

- A running daemon with `autonomous.enabled: true`.
- At least two **approved** Automatons that are not owned by another Automation.
- An Automation record that lists them as members, with an entry member and
  `on_complete` edges. See the Automation create call in the
  [API reference](../api/openapi.yaml) (`POST /api/automations`).

## Setup

Optionally turn on the idle alert (default off). It is read live, so no restart
is needed:

| Channel | How |
|---|---|
| Config file | `autonomous.automation.idle_alert_minutes: 30` |
| CLI | `datawatch config set autonomous.automation.idle_alert_minutes=30` |
| Chat | `configure autonomous.automation.idle_alert_minutes=30` |
| REST | `PUT /api/config` with the same key |
| MCP | `autonomous_config_set` with `automation_idle_alert_minutes: 30` |
| PWA | Settings → General → Automate → Automation idle alert (minutes) |

Whole minutes only; `0` turns it off; a negative or non-integer value is
rejected.

## Walkthrough: activate → advance → idle alert

1. **Create** a draft Automation `nightly` with members A then B (A's
   `on_complete` points at B). A draft does nothing.
2. **Activate** it. The entry member (A) is validated and started through the
   normal approval gate:

   ```
   datawatch automation activate <id>
   ```

   The Automation becomes `active` and A is `running`. If A is not approved, the
   Automation becomes `blocked` with the reason and an alert is raised.
3. **Advance.** When A completes, the coordinator takes A's `on_complete` edge
   and starts B. Watch it in the status array:

   ```
   curl -s $BASE/api/autonomous/status | jq .automations
   ```

   Each row shows the current member and Automaton, pending members, idle time
   and any blocked reason. A duplicate or racing completion event starts B once.
4. **Blocked.** If B is not approved when its turn comes, the Automation becomes
   `blocked` and you get a "Automation blocked" alert naming the reason.
   Approve B so it is ready; the Automation stays blocked until a later release
   adds the pause, resume and skip controls. A paused B is resumed automatically
   when its turn comes. An empty edge (for example A is blocked and has no
   `on_blocked` target) also blocks the Automation.
5. **Idle alert.** With `idle_alert_minutes: 30`, if an active Automation has no
   member running for 30 minutes, one "Automation idle" alert is raised for that
   idle episode. When a member starts again the episode ends; a later idle spell
   alerts again.
6. **Finish.** When the last member completes, the Automation is `completed`,
   an info alert is raised and its members are free to join another Automation.

If the daemon restarts mid-chain, it reconciles once its normal resume has
finished: a member that finished while the daemon was down advances the chain, a
member still running is left alone, and a start that had not happened yet is
retried without starting it twice. A **failed** member is not advanced on the
live event; it waits for the restart-time recovery, so a restart that kills a
task never routes the chain by mistake.

## All channels

| Action | REST | MCP | CLI | Chat |
|---|---|---|---|---|
| Activate | `POST /api/automations/{id}/activate` (body `{actor?, reason?}`) | `automation_activate` | `datawatch automation activate <id>` | `automation activate <id>` |
| List / get | `GET /api/automations`, `GET /api/automations/{id}` | `automation_list`, `automation_get` | — (use REST or MCP) | — |
| Status rows | `GET /api/autonomous/status` → `automations` | `autonomous_status` | `datawatch autonomous status` | — |
| Set execution and planning defaults | `POST /api/automations/{id}/set_llm` | `automation_set_llm` | `datawatch automation set-llm <id>` | `automation set-llm <id> backend=<b> model=<m> ...` |
| Set verification defaults | `POST /api/automations/{id}/set_verification_llm` | `automation_set_verification_llm` | `datawatch automation set-verification-llm <id>` | `automation set-verification-llm <id> backend=<b> model=<m> ...` |

Activation only works on a draft (409 otherwise); an unknown ID is 404. Every
activation, advance and block is written to the audit log with its actor
(`operator`, or `automation` for the coordinator) and reason; see
[audit logging](audit-logging.md). The PWA has no Automation view yet, so
activation and status rows are API, MCP, CLI and chat only for now.

## Give an Automation its own model defaults

By default every member Automaton runs and is verified on the daemon's global
backend and model. If a whole chain should use something else, for example a
stronger model for a risky nightly job, set the defaults once on the
Automation instead of editing each Automaton.

The Automation's defaults sit between the Automaton and the global default.
For the execution backend, model and effort, and for the verification LLM,
the first level that sets a value wins: task, then story, then Automaton, then
Automation, then global. Backend and model are taken from the same level;
effort is resolved on its own. So an Automaton or task that names its own
backend keeps it, and the Automation default only fills what they leave open.

1. **Set the execution default** (while the Automation is draft, active,
   paused or blocked):

   ```bash
   datawatch automation set-llm <id> --backend claude-code --model claude-opus-5-5 --effort high
   ```

   Chat: `automation set-llm <id> backend=claude-code model=claude-opus-5-5 effort=high`.
   REST: `POST /api/automations/{id}/set_llm` with the same names in the body.
   MCP: `automation_set_llm`.

2. **Set the verification default** the same way:

   ```bash
   datawatch automation set-verification-llm <id> --backend claude-code --model claude-sonnet-5-5
   ```

3. **Optionally pick a planner.** `--planning-backend` and `--planning-model`
   on `set-llm` choose the LLM that decomposes a member into stories and tasks.
   Leave them unset and the global planning backend is used. They only affect
   decomposing; they never change which LLM runs or verifies the work.

4. **Clear a field** by sending it empty, for example `--model ""` or `model=`
   in chat. A field you leave out is kept as it is. Name at least one field.

`expected_version` is optional on both operations: leave it out (or send `0`)
to skip the check, or send the version you read to be told with a 409 if
someone else changed the Automation first.

A change applies to members that start afterwards; a task already running is
not touched. To see which level supplied the backend for a task, read its
`executor_source` (and, after verification, `verifier_source`), which can be
`task`, `story`, `prd`, `automation`, `global` (or `pool` for the verifier).
The PWA shows `executor_source` as an **Executor** line on the task and on the
session status card; there is no Automation view in the PWA yet, so setting the
defaults is API, MCP, CLI and chat only for now.
