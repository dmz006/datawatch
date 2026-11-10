---
docs:
  index: true
  topics: [projects, policy, precedence, tiers, automaton, plan-checks, retry, lanes, uncommitted-work]
exec_steps:
  - tool: project_policy_effective
    description: Read the effective policy first. It shows every setting, the value in force, and the tier (Automaton, Project or Daemon) that supplied it
    args: {name: core-api}
    read_only: true
  - tool: project_policy_effective
    description: Worked example, read-only. Resolve with one Automaton's own policy fields as the top tier, so you see which of the Automaton's keys shadow the project's
    args: {name: core-api, automaton: 1}
    read_only: true
  - tool: project_upsert
    description: A state-changing call. Upsert the project entry with its policy block as a whole at the project tier (not one key at a time). One invalid value rejects the whole block and stores nothing
    args: {name: core-api, dir: /srv/projects/core-api, policy: {max_concurrent_tasks: 2}}
---

# Project automation policy

**Project automation policy** is the per-project layer of the three tiers the daemon resolves every
Automaton setting through. It sits **between the daemon and the Automaton**:

```
Automaton's own settings   (top)     — the Automaton overrides any one policy
          over
Project policy              (middle) — this document: stored per project
          over
Daemon defaults             (base)   — your daemon-wide configuration
```

The project tier is what travels with the repository. The daemon tier is what the machine holds,
and the Automaton tier is what a single Automaton decides for itself. The project tier is the one
you tune when *this project* should behave differently from *this machine*, without touching the
daemon-wide defaults that every other project still reads.

Every field is optional. An **absent field is not the same as a set field**: an absent field
*inherits* whatever is below it — the Automaton's own value if it set one, otherwise the daemon
default. That way you can keep one daemon serving two projects whose plan gates point at different
repositories or different retry budgets, without two daemons.

A project with no policy at all reads exactly like it did before policies existed — every setting
resolves to the **daemon** tier — because every field's default is *unset*, i.e. *inherit the daemon
value*. **Nothing in this repository changes until you set a key.**

## The settings

Each field is optional. If a field is absent, the daemon falls back to its own value for that key.

**Plan checks**

- **`plan_coverage_check`** — what the plan requirement-coverage gate does to a short plan. `off`
  lets every plan through, `warn` leaves the plan as-is and only names the shortfall, and `block`
  stops the gate short of accepting the plan.
- **`plan_coverage_min_ratio`** — the coverage ratio a plan must reach, between just above zero and
  one. A plan that carries less of the requirement set than this line does not pass.
- **`plan_review_mode`** — the plan-quality review gate, in the same three vocabularies: `off`, `warn`
  or `block`.
- **`scope_lint`** — the task-scope lint for this project: `off` turns it off, `warn` leaves the task as
  is and names the problem, `enforce` refuses a task that does not fit its stated scope. These four
  apply to every Automaton that plans inside this project, before any Automaton's own settings are
  consulted.

**Retry & escalation**

- **`retry_policy`** — the per-category retry budgets: a whole number of attempts from zero up, for
  each of `infra`, `content`, `scope` and `policy`. Leave a category out of the map and only that
  category keeps the daemon's number — the others you did set still apply.
- **`retry_ceiling_total`** — a cap, in whole attempts, on the retries the daemon spends across all of
  a task's categories. Set it low when you want a project's retries to stay bounded no matter how
  many categories each fail.
- **`auto_fix_retries`** — how many times an auto-fix run may be retried before the work is given up on. Zero
  turns auto-fix off for the project; a negative value is refused when saved.
- **`infra_retry_escalate_after`** — after how many infrastructure retries a task is allowed to
  *escalate* — move to a stronger backend — instead of retrying the same class. Zero reads as *never,
  keep retrying*; a negative value is refused.
- **`failure_escalation`** — the escalation level the daemon reaches when a task's retries are spent:
  the tier, its backend/model/effort, and how many attempts each step gets.

**Git defaults**

- **`default_auto_pr`** — whether a task may open a pull request for itself without asking per task.
- **`parallel_read_only_tasks`** — whether read-only tasks may run in parallel in this project
  rather than one at a time.
- **`parallel_worktree_per_task`** — whether each parallel task gets its own worktree rather
  than sharing one. Off means the tasks share the project's single worktree, which is the right
  choice only when they also have nothing to write.

**Lane limits**

- **`max_concurrent_tasks`** — the project's cap on in-flight tasks at once. Zero or below refuses
  when saved; one is the floor.
- **`plan_tier_max_inflight`** — per plan tier, how many Automata of this project may be running at once.
- **`lane_fill_gpu_lanes`** and **`lane_fill_claude_lanes`** — how many GPU and Claude lanes this
  project may hold from the shared pools. Zero on each means *no preference*, which reads the
  daemon's own caps.

**Other recent keys**

- **`uncommitted_work_policy`** — what an Automaton must do with work left uncommitted before it
  is called done: `hold` keeps the work and parks the Automaton for a human, `ask` raises a
  question the operator answers, and `discard` throws the uncommitted work away.
- **`uncommitted_work_scope`** — how wide that check looks: `commit` judges only the work a session
  committed, `worktree` also counts files it found sitting in the worktree. `worktree` is the
  default and the stricter of the two.
- **`burn_guard_ask_on_change`** — whether, when the burn guard changes a setting for a task in this
  project, the change is a confirmation question to the operator or a silent write. Off (the default)
  keeps it a silent write the operator can audit afterwards; on asks first.

## How it is stored

One object per project, in `~/.datawatch/config.yaml`, under `projects.<name>.policy`:

```yaml
projects:
  core-api:
    dir: ~/projects/core-api
    policy:
      plan_coverage_check: warn
      plan_coverage_min_ratio: 0.85
      retry_policy:
        infra: 2
        content: 1
      max_concurrent_tasks: 4
```

Set a key and it wins for every Automaton in that project unless the Automaton overrode the same
key. The CLI (`datawatch projects policy set <name> <key> <value>`) and the panel both upsert through
the same `POST /api/projects` endpoint and the daemon re-validates the key before it lands, so a bad
value lands nowhere at all.

## How to read it

### In the panel

Open Settings on the PWA, go to **Automata > Project policy**, and ask for a project by name.
The panel shows every setting in one view.

### Before you change anything

- **Read the effective value.** The panel already shows each field's effective value — that is the
  value the daemon will actually use, resolved across the tiers.
- **Read the badge.** The badge next to each field names the tier that supplied the effective value:
  **Automaton**, **Project**, or **Daemon**. A field whose badge says *Daemon* has no project value
  at all; its effective value is the daemon's, and it is not a copy of it that you set.

## How to set it

### In the panel

Pick the field whose badge you want to flip, choose a value, and the panel writes one key of
`projects.<name>.policy`. The daemon re-checks the key before it stores it — a value outside the
field's range is refused with the reason shown, and nothing is half-written.

### From the command line or API

- **YAML** — `projects.<name>.policy` in `~/.datawatch/config.yaml`, as in the block above.
- **MCP** — `project_upsert` with the entry's `policy` object (upserts the whole policy object, so
  send the block you want as a whole, not one key at a time).
- **REST** — `POST /api/projects` with the `policy` field.

The effective value — every setting with its supplying tier — is readable several ways, all read
only:

- **MCP** — `project_policy_effective(name)` — add the optional `automaton` argument to resolve with
  an Automaton's own fields in play as the top tier.
- **CLI** — `datawatch projects policy show <name> [--effective] [--automaton <id>]` — without
  `--effective` it shows the stored policy block.
- **Comm channel** — type `project policy <name>` (with an optional `<automaton-id>` for the
  top tier) and the daemon answers with the same effective table, one line per setting in the form
  `setting = value (source-tier)`. Comm carries the read only — it never writes the policy.
- **REST** — `GET /api/projects` (stored, on each entry) and
  `GET /api/projects/{name}/policy/effective?automaton=<id>`.

## How to clear it

The panel's *inherit (clear)* action removes the key — it does not write `null` or a daemon default
in its place. On the CLI the counterpart is `datawatch projects policy unset <name> <key>`; by hand,
delete the key from `projects.<name>.policy` in `~/.datawatch/config.yaml`. After clearing, the badge on the
panel's next read shows the tier below in play — **Automaton** if that Automaton sets the key,
otherwise **Daemon** — so you can confirm the fall-through before you close the panel:

- Clear **`plan_coverage_check`** and the gate reverts to the daemon's mode for this project.
- Clear **`max_concurrent_tasks`** and the project's cap reverts to the daemon's cap. Clear
  **`plan_tier_max_inflight`** and the per-tier cap reverts the same way.
- Clear **`lane_fill_gpu_lanes`** / **`lane_fill_claude_lanes`** and each reverts to *no
  preference*.
- Clear **`uncommitted_work_policy`** and the check reverts to the daemon's policy for this
  project; clear **`uncommitted_work_scope`** and it reverts to `worktree`.
- Clear **`burn_guard_ask_on_change`** and burn-guard changes for the project's tasks go back to
  silent writes.

## What the panel does not do

Every value is validated on the write path: a whole number stays a whole number, an enum stays in
its vocabulary, a ratio stays in `(0, 1]`. A value the daemon refuses does not land, and the panel
shows the reason it was refused.

There is no *apply to every project* — the panel edits one project at a time.

Every write is a no-op for the daemon tier: a project field never writes through to
`autonomous.<key>` or `usage.<key>` in the daemon's config. The daemon's values are the floor the
project tier sits above, and they stay put whatever you set above them.

## What stays daemon-wide until you ask

The policy block has the keys above — and only the keys above. The keys the daemon already holds
for everything but which do not have a project form yet stay daemon-wide until someone asks for a
per-project form:

- **Timeouts & cadences** — session and lane timeouts, the lane-fill poll interval, and the idle-lane
  alerting.
- **Backend & model selection** — the `gpu` and `claude` backends and the models they call, and
  the session default backend.
- **Worktree layout** — the worktree base and the worktree directory the projects draw from.
- **Burn guard** — its whole configuration (interval, margin, ladder) stays daemon-wide; only the
  `ask_on_change` switch moved into the policy.

None of these moves on its own. If a daemon-wide key needs a per-project form, that is a design
decision, not a setting.

## A worked example

You run one daemon that builds two repositories. `core-api` is a tight trunk: plans must reach 85%
requirement coverage before the work starts, infrastructure failures get exactly two retries before
escalation, and at most four of its tasks may be in flight at once. `docs-site` is looser and just
inherits the daemon.

```yaml
projects:
  core-api:
    dir: ~/projects/core-api
    policy:
      plan_coverage_check: warn
      plan_coverage_min_ratio: 0.85
      retry_policy:
        infra: 2
      max_concurrent_tasks: 4
  docs-site:
    dir: ~/projects/docs-site
    # no policy: every setting resolves to the daemon tier
```

Then from the PWA open Settings → Automata → **Project policy** and ask for `core-api`. The rows
show effective values and badges:

- `plan_coverage_check` — *warn* — **Project** (you set it; with the daemon at `off`, the effective value is the project's).
- `plan_coverage_min_ratio` — *0.85* — **Project**: you set it; an Automaton that sets its own ratio
  would badge **Automaton** instead and win.
- `max_concurrent_tasks` — *4* — **Project**.
- `plan_review_mode` — whatever the daemon holds — **Daemon**: the project never set it, and no
  Automaton overrides it.
- `burn_guard_ask_on_change` — *false* — **Daemon**: burn-guard changes in `core-api` stay silent
  writes you can audit, unless you flip the key.

And `datawatch projects policy show core-api --effective` on the CLI,
`project_policy_effective("core-api")` on MCP, or `project policy core-api` on the comm
channel, give the same table — and `--automaton <id>` / the `automaton` argument /
`project policy core-api <automaton-id>` add the top tier, showing which of that
Automaton's own fields would shadow which project keys.

Reading any of these never writes anything; only a key you set in the panel, a
`datawatch projects policy set`, a `project_upsert` or a `POST /api/projects` does.
(The comm `project policy` line is a read, like the rest of the list above.)

## How it behaves

- **A project is a value, not a switch.** A policy block set on a project applies to every Automaton
  the daemon runs inside that project's directory, and does not apply to any other project. No
  per-project on/off flag: absence of the key *is* the off position, and it means *defer to the
  daemon*.
- **The effective read tells you which side won.** Every read surface resolves the same way the
  daemon does at use time: Automaton first, then project, then daemon. So the badge is a promise —
  the value you see is the value in force for that Automaton right now, not a stored field.
- **A value you set stays until you unset it.** A project value persists in `~/.datawatch/config.yaml`
  across daemon restarts, like the rest of the file.
- **The daemon's values are never changed by this panel.** Setting
  `projects.core-api.policy.max_concurrent_tasks: 4` does not edit `autonomous.`
  The two tiers stay independent; the project tier just wins the resolution.

## Daemon-level project defaults

The `projects_defaults` block sets three defaults that a project inherits when it sets none of its own:

| Key | Meaning | Default |
|---|---|---|
| `projects_defaults.branch_retention_days` | Days a project's merged branches are kept before cleanup, used when the project sets no `branch_retention_days`. | 0 (unset: branches are kept) |
| `projects_defaults.weekly_share_percent` | A project's share of the weekly usage budget, 0 to 100, used when the project sets no `weekly_share`. | 0 (unset) |
| `projects_defaults.major_trigger` | When a major release is raised for a project that sets no `major_trigger`: `manual`, `milestone` or `never`. | `manual` |

The block is named `projects_defaults` rather than `projects.defaults` because `projects` is a map keyed by project name, and a project called `defaults` would collide with it.

Set them in `config.yaml`:

```yaml
projects_defaults:
  branch_retention_days: 30
  weekly_share_percent: 20
  major_trigger: manual
```

Or through the running daemon, on any surface:

- REST: `PUT /api/config` with `{"projects_defaults.major_trigger": "milestone"}`; read back with `GET /api/config` under `projects_defaults`.
- Comm channel: `configure projects_defaults.branch_retention_days=30`.
- MCP: `config_set` with key `projects_defaults.weekly_share_percent`; `get_config` returns the block.
- CLI: `datawatch config set projects_defaults.major_trigger milestone`.

A project's own value always wins over these defaults. A refused value (a negative day count, a share outside 0 to 100, or a trigger other than `manual`, `milestone` or `never`) is rejected with the key named in the error, and nothing is changed.
