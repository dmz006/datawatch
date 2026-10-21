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

Activation only works on a draft (409 otherwise); an unknown ID is 404. Every
activation, advance and block is written to the audit log with its actor
(`operator`, or `automation` for the coordinator) and reason; see
[audit logging](audit-logging.md). The PWA has no Automation view yet, so
activation and status rows are API, MCP, CLI and chat only for now.
