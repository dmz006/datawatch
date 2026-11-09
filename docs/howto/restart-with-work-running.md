# Restart with work running

When the daemon restarts, two settings decide what happens to Automata:

- `autonomous.restart_policy` covers Automata with tasks running.
- `autonomous.planning_restart_policy` covers an Automaton that is still in
  planning, meaning its decompose is still running.

This page covers the planning setting. For running tasks, see
[operations.md](../operations.md) (restart policy values).

## Current status

The setting is stored, validated and reported by `GET /api/config`, and the
daemon's boot path reads it. With `reattach` (the default), a planning Automaton
whose planner session is still alive stays in planning: the daemon attaches a
reader to that session and its output file, and when the planner writes its plan
the decompose completes as if the restart had not happened. If the planner
ended while the daemon was down and left a complete plan, that plan is ingested
at boot. Otherwise the Automaton goes back to draft, as before. The `restart`
value is not yet run at boot: it also goes back to draft, so the operator
requests planning. Confirm on the build in use that a restarted daemon keeps a
mid-decompose PRD in planning before relying on `reattach`.

## The three values

| Value | What the daemon does on restart, once the setting is read |
|-------|-----------------------------------------------------------|
| `reattach` (default) | Keeps planning running. The decompose is re-attached to the restarted daemon. |
| `restart` | Restarts planning once after the restart and records a decision saying so. |
| `draft` | Resets the Automaton to draft, as the daemon does today. |

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

The value is saved at once. A restart reads it only in the way described under
[Current status](#current-status).

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
