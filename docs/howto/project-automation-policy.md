---
docs:
  index: true
  topics: [policy, project, precedence, automaton, daemon, retry, plan coverage, lanes, burn guard, uncommitted work, PWA, settings]
exec_steps:
  - tool: project_policy_effective
    description: Read every setting of a project's effective policy with the tier that supplies each value, and optionally an Automaton's own tier on top
    args:
      name: "{{params.project_name}}"
    read_only: true
  - tool: project_upsert
    description: Register a project and/or set one or more keys of its policy tier; keys left out of policy keep the tier below
    args:
      name: "{{params.project_name}}"
      dir: "{{params.project_dir}}"
      policy: {"plan_review_mode": "block"}
    read_only: false
---

# Project automation policy

**Project automation policy** is the middle tier of the three tiers the daemon
resolves every Automaton setting through: **Automaton** (its own override)
over **project** over **daemon** defaults. The project tier is one block per
registered project — `projects.<name>.policy` in `datawatch.yaml` — and it is
the only tier that travels with the repository: clone the project, keep the
datawatch config folder, and the same rules apply on every host.

The PWA's **Settings → Automata → Project policy** panel is where you read and
edit this tier: one row per setting, the effective value in force, a badge
naming the tier that supplied it, and a control to set a value or clear the
key back to the tier below.

## The three tiers, concretely

For any one setting the daemon reads the tiers top to bottom and takes the
first that holds a value:

```
Automaton tier   the Automaton's own policy fields (set per run or on the record)
project tier     projects.<name>.policy     ← this panel edits here
daemon tier      the daemon-wide defaults and autonomous.* settings
```

A key that is *absent* from a tier is not "the daemon value was written" — the
tier below is simply in play again. Clearing a key in the panel therefore
restores whatever a daemon setting or an Automaton override supplies, which
can be more or less permissive than the daemon default alone. The precedence
badge on each row states *in force right now*, not what is stored in this
tier.

Reading any of the tiers has no side effect. Only writing the project tier or
the Automaton tier changes anything, and a write here never touches the daemon
tier.

## Base requirements

- A running daemon with automation on (`autonomous.enabled: true`) and at
  least one registered project, added in the Project Profiles panel one above,
  by `project_upsert`, or from `datawatch projects set <name> <dir>`.
- For the **Automaton** tier badge to be meaningful, an Automaton id to pass.
  The panel takes it as an optional field per project.

## Reading the panel

Open **Settings → Automata → Project policy** in the PWA:

1. Pick the **project**. The panel loads its stored policy keys and resolves
   every setting through the tiers.
2. Optionally fill in an **Automaton** id to include that Automaton's own
   policy fields as the top tier — the same read the daemon performs at run
   time.
3. Each row shows the setting's label, its effective value, the **precedence
   badge** (Automaton / Project / Daemon) and a control.
4. Groups appear in the order plan checks, retry & escalation, git defaults,
   lane limits, and the other recent keys. The legend under the title restates
   the precedence rule.

The same read, without the panel:

```bash
datawatch projects policy show <name> --effective --automaton <id>
# or on REST
GET /api/projects/<name>/policy/effective?automaton=<id>
```

Both answer with one entry per setting: the value in force and the tier that
supplied it.

## Setting a value

Choose the value in the row's control and press **Save** for that row. The
panel sends the project's full policy block with this one key changed, so no
other row moves. The daemon re-validates the block and refuses a value a key
does not accept — the panel shows the reason and keeps your input for a retry;
nothing half-written is stored.

Examples, all three surfaces:

```bash
# CLI: set one key (value parsed as a JSON scalar or object)
datawatch projects policy set <name> plan_review_mode block
datawatch projects policy set <name> max_concurrent_tasks 2
datawatch projects policy set <name> retry_policy '{"infra":3}'
```

```jsonc
// MCP: project_upsert carries the whole policy block
{ "name": "<alias>", "dir": "/abs/dir",
  "policy": { "plan_review_mode": "block",
              "max_concurrent_tasks": 2,
              "retry_policy": { "infra": 3 } } }
```

```bash
# REST: same body to POST /api/projects
```

The panel's controls are the same call wrapped: pick a value, the block is
built with every other stored key unchanged, and the response is the project
as saved.

## Clearing a key back to the tier below

Choose **inherit (clear)** and press **Save**. The key is deleted from the
project tier — not set to the daemon value. After a clear, the badge on that
row moves to **Daemon** (or back to **Automaton** while the optional Automaton
field still holds its own value for the key), and the effective value is
whatever the lower tier supplies. This is the right way to undo a project rule
you want to hand back, and it is visibly different from a save of the daemon
default.

## The fields, group by group

Every key is optional. **Plan checks** — `plan_coverage_check` (`off`, `warn`,
`block`): what the plan requirement-coverage gate does to a plan that misses
requirements; `plan_coverage_min_ratio`: the ratio a plan must reach before
the gate fires (a number in `(0, 1]`); `plan_review_mode` (`off`, `warn`,
`block`): the plan-quality review gate; `scope_lint` (`off`, `warn`,
`enforce`): the scope-integrity check on a task.

**Retry & escalation** — `retry_policy`: the per-category retry budgets, a
whole count of attempts for each of `infra`, `content`, `scope` and `policy`;
a category left out of the map keeps the tier below for that category only.
`retry_ceiling_total`: the cap on retries the daemon spends in total, an
integer of 0 or more. `auto_fix_retries`: how many times an auto-fix run may
be retried, an integer of 0 or more. `infra_retry_escalate_after`: after how
many infrastructure retries a task may be escalated rather than retried
(fixed-version bumps are never escalated). `failure_escalation`: the
escalation level reached when a task's retries are spent — tier, backend,
model, effort and attempts per step.

**Git defaults** — `default_auto_pr`: whether a finished task may open a pull
request without a per-task ask. `parallel_read_only_tasks`: whether read-only
tasks may run in parallel. `parallel_worktree_per_task`: whether each parallel
task gets its own worktree.

**Lane limits** — `max_concurrent_tasks`: the project's cap on in-flight
tasks, an integer of 1 or more. `plan_tier_max_inflight`: per plan tier, how
many Automata that tier may run at once. `lane_fill_gpu_lanes` and
`lane_fill_claude_lanes`: how many GPU and Claude lanes this project may hold,
integers of 0 or more, 0 meaning the daemon's own choice.

**Other recent keys** — `uncommitted_work_policy` (`hold`, `warn` or `off`)
and `uncommitted_work_scope` (`task_files` or `worktree`): the uncommitted-work
check for every Automaton that commits its own work inside this project, as
described in [definitions — Uncommitted-work check](../datawatch-definitions.md#uncommitted-work-check).
`burn_guard_ask_on_change`: whether a burn-guard change while a task runs asks
the operator first.

## Verifying a change

After saving, the row's value and badge update in place on the next **Refresh**
(or immediately, if the save response carried the resolved block). From a shell,
the standing read is:

```bash
datawatch projects policy show <name> --effective
```

and the stored block, independent of the tiers, is the project entry in
`datawatch.yaml` under `projects.<name>.policy`.

## What the panel never does

- It never writes the daemon tier; daemon-wide settings stay in
  `autonomous.*` and apply only where a project or Automaton has not spoken.
- It never rewrites an Automaton's own tier; the Automaton id is a read-only
  lens for the resolution.
- It never stores a value the daemon refuses: the daemon is the final
  authority on every key, and a refused save names the offending field.

See [definitions — Project automation policy](../datawatch-definitions.md#project-automation-policy)
and [mcp.md — projects](../mcp.md#projects).
