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

The axes in question are the ten built-in ones — `compute:<node>`, `peer:<name>`, `backend:<name>`, `lane:<name>`, `role:<name>`, `project:<name>`, `automaton:<name>`, `automation:<name>`, `stage:<name>` and `kind:<name>` — plus any operator-defined custom axes. The app-side surfaces that show these tags are the session list, the Automaton list and the settings for placement tags.

## Current behavior

The daemon stamps a session from its placement record. Before this work, that left some sessions with a partial set. Planner sessions carried `automaton:`, `kind:decompose`, `lane:` and `role:planner`, but no `compute:` tag, even though they run on a node. The supervisor session carried no system tags at all.

This work landed the shared building blocks: one pure builder (`SessionPlacement`) that maps a session record to its placement — including the local node for local work that stored no node, and the launch role, kind and stage recorded on the session record — and a tag-sweep rebuild that uses that same builder for sessions that carry launch facts, while sessions with none keep exactly the legacy derivation (role and kind read from the session name, no node fallback), so the sweep leaves them unchanged rather than guessing. The launch facts themselves live on the session record (`LaunchRole`, `LaunchKind`, `LaunchStage`, persisted in the store) and are written by the manager's `SetLaunchFacts`.

But the launch side does not call it yet: no production launch path records launch facts, so a new planner or supervisor session is still stamped from the placement record its launch side gives it, the way it always was. The docs set shipped on 2026-11-08 says so plainly: it promises that a session recording its launch facts carries every axis that applies — a planner gains `compute:<node>`, a supervisor carries `role:supervisor` and `compute:<node>` plus `backend:` and `lane:` when it runs in one — and it notes that a session gains the extra axes once its launch side starts recording them. The docs also say that existing sessions gain the tags through the tag sweep, and that the sweep should be run with `--dry-run` first. That launch-side wiring is the missing half: the builder, the store fields and the sweep path are all in place, but the full axes are not yet observable on a running daemon until a launch path records its facts (or the operator's daemon is already on a build where it does — the docs commit records that verification was not run before it landed, and this draft confirms it in neither direction).

## Expected behavior

- A new worker, planner or supervisor session carries every placement axis that has a fact behind it and is enabled. An axis with no fact writes no tag. For a planner and a supervisor session that means their launch records the role, kind and stage so the shared builder can build the full placement — until that wiring lands, they carry only the axes their placement record already gives them, and the app side must not promise the new axes before it does.
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
| Comm channel | Included, with a caveat | The tag verbs and the sweep (`autonomous tags sweep [dry-run]`) are on the comm channel, like the REST and CLI surfaces. The session-list axis filter shortcuts (`--stage`, `--kind`, and the rest) are not exposed on the comm channel, which is an existing gap, not new in this work. |
| YAML/config | Included | The `autonomous.tags.placement` block in `docs/config-reference.yaml` holds the axis switches. No new key is added by this draft. |
| PWA | Included | The session list grouping and the placement filter already exist. The new axes appear in them without a new control. |
| Android | Owed to the app repo | The app must show the new axes on the same sessions as the PWA. This draft does not file that work. |
| iPhone/iOS | Owed to the app repo | Same as Android. |

## Suggested next step

1. The operator confirms the exact axis set for the supervisor session on a live daemon. The docs say it is `role:supervisor`, `compute:`, and `backend:` and `lane:` when it runs in one. This must be checked before the app issue is filed.
2. Run a dry-run tag sweep on the operator's daemon and read the list of sessions that would change. Apply the sweep only after that list is reviewed.
3. Confirm the launch-side wiring: check whether a follow-up task is already planned to have the executor record launch facts (`SetLaunchFacts`) on the planner and supervisor starts, and if not, scope it — without it the "full axes" rule applies only to sessions the launch side starts recording, and the app-side issue would ask for visible behavior that a stock daemon does not have.
4. Once the live check passes, file the app-side note in the datawatch-app repo. Use the wording in the placement parity plan as the starting point, and add one acceptance line: a new planner session and a new supervisor session show the new axes on both platforms.
5. Record the live check result in the parity plan's open-items list. Do not mark the feature validated until that check has been run on a live daemon.

## Open questions for the operator

- Is a tag sweep the wanted rollout for existing sessions, or should the daemon re-stamp them automatically?
- Should a supervisor session show `backend:` and `lane:` only when it runs in a lane, as the docs say, or always?
