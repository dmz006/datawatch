# Placement tags on every session — parity issue draft

> Draft only. Not filed. Nothing in this file has been posted to any tracker. Post it to the app repo only after the operator confirms the scope below.

**Feature:** every session that can carry a placement (system) tag carries the full set of placement axes that apply to it.
**Found:** 2026-11-08, from the operator's answer to the planner / supervisor tag question.
**Related plan:** [placement auto-tags parity plan](../2026-11-03-placement-auto-tags-parity.md).
**Related docs:** [howto/automata-tags.md](../../howto/automata-tags.md) (section *Stamping, re-stamping and the startup backfill*) and [operations.md](../../operations.md) (Tag browser section, placement tags check).

## Affected area

Placement (system) tags on sessions, for the three session kinds the autonomous executor starts:

- **Worker** sessions, which run an Automaton task.
- **Planner** (decompose) sessions, which break an Automaton into tasks.
- **Supervisor** sessions, which watch Automata and their lanes.

The axes in question are `compute:<node>`, `peer:<name>`, `backend:<name>`, `lane:<name>`, `role:<name>` and `project:<name>`, plus any operator-defined custom axes. The app-side surfaces that show these tags are the session list, the Automaton list and the settings for placement tags.

## Current behavior

The daemon stamps a session from its placement record. Before this work, that left some sessions with a partial set. Planner sessions carried `automaton:`, `kind:decompose`, `lane:` and `role:planner`, but no `compute:` tag, even though they run on a node. The supervisor session carried no system tags at all.

The docs set shipped on 2026-11-08 describes the new rule. The howto and the operations page say that every placement-capable session carries every axis that applies to it. A planner session gains `compute:<node>`. A supervisor session carries its applicable axes, with `role:supervisor` and `compute:<node>`, plus `backend:` and `lane:` when it runs in one. The docs also say that existing sessions gain the tags only through the tag sweep, and that the sweep should be run with `--dry-run` first.

The docs commit records that verification was not run before it landed. Whether the running daemon already stamps the new planner and supervisor axes is not confirmed in this draft.

## Expected behavior

- A new worker, planner or supervisor session carries every placement axis that has a fact behind it and is enabled. An axis with no fact writes no tag.
- A planner session carries `compute:<node>` for the node it runs on, next to its existing tags.
- A supervisor session carries `role:supervisor`, `compute:<node>`, and `backend:` and `lane:` when it runs in one.
- Turning an axis off stops that tag from being written on any session.
- Existing sessions gain the new tags only through the tag sweep. The sweep is run as a dry run first, then applied.
- The app shows these tags as read-only system tags, in the same way as the PWA. Local work shows no `peer:` tag.

## Parity surface

| Surface | Status | Reason |
|---|---|---|
| REST | Included | Session JSON carries `system_tags`. The sweep and tag listing are reached through the existing autonomous tag endpoints. |
| MCP | Included | The tag listing and the placement config keys are reachable through the existing tools. |
| CLI | Included | `datawatch autonomous tags sweep --dry-run` and the sweep without the flag. |
| Comm channel | Included, with a caveat | The tag verbs are shared. The axis and system filters are not exposed on the comm channel, which is an existing gap, not new in this work. |
| YAML/config | Included | The `autonomous.tags.placement` block in `docs/config-reference.yaml` holds the axis switches. No new key is added by this draft. |
| PWA | Included | The session list grouping and the placement filter already exist. The new axes appear in them without a new control. |
| Android | Owed to the app repo | The app must show the new axes on the same sessions as the PWA. This draft does not file that work. |
| iPhone/iOS | Owed to the app repo | Same as Android. |

## Suggested next step

1. The operator confirms the exact axis set for the supervisor session on a live daemon. The docs say it is `role:supervisor`, `compute:`, and `backend:` and `lane:` when it runs in one. This must be checked before the app issue is filed.
2. Run a dry-run tag sweep on the operator's daemon and read the list of sessions that would change. Apply the sweep only after that list is reviewed.
3. Once the live check passes, file the app-side note in the datawatch-app repo. Use the wording in the placement parity plan as the starting point, and add one acceptance line: a new planner session and a new supervisor session show the new axes on both platforms.
4. Record the live check result in the parity plan's open-items list. Do not mark the feature validated until that check has been run on a live daemon.

## Open questions for the operator

- Is a tag sweep the wanted rollout for existing sessions, or should the daemon re-stamp them automatically?
- Should a supervisor session show `backend:` and `lane:` only when it runs in a lane, as the docs say, or always?
