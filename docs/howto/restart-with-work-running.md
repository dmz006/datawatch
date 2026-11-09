# Restart with work running

When the daemon restarts, two settings decide what happens to Automata:

- `autonomous.restart_policy` covers Automata with tasks running.
- `autonomous.planning_restart_policy` covers an Automaton that is still in
  planning, meaning its decompose is still running.

This page covers the planning setting. For running tasks, see
[operations.md](../operations.md) (restart policy values).

## Current status

The setting is stored, validated and reported by `GET /api/config`, and the
daemon's boot path reads it. At boot the daemon decides for each planning
Automaton from the setting, the planner session it recorded when it spawned the
planner, and what that session left behind:

- the planner session is still alive → a reader is attached to it and its
  output file, and when the planner writes its plan the decompose completes as
  if the restart had not happened;
- the planner ended while the daemon was down and left a complete plan → that
  plan is ingested at boot and the Automaton moves on without a new planner
  run;
- nothing usable survives → planning is restarted once, with a decision
  recording it; a further restart with nothing usable left sends the Automaton
  back to draft.

That is the `reattach` (default) value. `restart` skips straight to the one
automatic planning restart at boot, and `draft` resets the Automaton to draft,
as before the setting existed. Every outcome is recorded in the Automaton's
decisions and the boot-resume report, and live planner sessions that no reader
owns are ended.

The boot path is in this tree, but the `planning` row of the restart matrix
and its release-smoke run are still open, and no live deploy-restart has
confirmed it: confirm on the build in use that a restarted daemon behaves as
above before relying on `reattach` (the *Still to review (operator)* notes in
[operations.md](../operations.md)).

## The three values

| Value | What the daemon does on restart |
|-------|-----------------------------------------------------------|
| `reattach` (default) | Keeps planning running. The decompose is re-attached to the restarted daemon. |
| `restart` | Restarts planning once after the restart and records a decision saying so. |
| `draft` | Resets the Automaton to draft, as the daemon did before the setting existed. |

An unset or unrecognised value reads as `reattach`. The write paths (REST, CLI,
MCP, comm channel and the PWA Settings select) refuse any value other than the
three above, and name the valid values in the refusal.

## How to set it

| Method | How |
|--------|-----|
| **YAML** | In `~/.datawatch/config.yaml`, under `autonomous:`, set `planning_restart_policy: reattach` (or `restart` or `draft`). |
| **CLI** | `datawatch autonomous config-set '{"planning_restart_policy":"restart"}'` |
| **Web UI** | PWA Settings, **Automata** tab, *Autonomous Automata planning* card — the **Planning restart policy** select with the three values `reattach`, `restart`, `draft`. Its localized help text (the `settings_planning_restart_policy*` keys in `internal/server/web/locales/*.json`) describes the values and links here. |
| **REST API** | `PUT /api/config` with `{"autonomous.planning_restart_policy": "draft"}` |
| **MCP** | `autonomous_config_set` with `planning_restart_policy` set to `reattach`, `restart` or `draft`. |
| **Comm channel** | `configure autonomous.planning_restart_policy=restart` |

The value is saved at once; the boot path reads it at the next restart, as
described under [Current status](#current-status).

## Choosing a value

- Use `reattach` when planning is healthy and you expect the restart to be quick.
  The intent is that nothing is lost and no planning work is repeated.
- Use `restart` when you suspect the planning run in the old process is stuck. The
  intent is that planning starts again once, and the Automaton's decisions show it.
- Use `draft` when you want to review the Automaton before any planning runs again.
  The Automaton returns to draft and you request planning yourself.

## Checking the value

`GET /api/config` returns the stored value as `planning_restart_policy` in the
`autonomous` block. The `datawatch autonomous config-get` command reads the
autonomous manager config and does not show this key.
