---
docs:
  index: true
  topics: [autonomous, automata, checks, pre-start verifier, plan rules, after-done checklist, registry check, ship, push, tag, ci watch, shadow mode]
exec_params:
  - name: check_key
    description: One of autonomous.checks.pre_start_verifier, autonomous.checks.plan_rules, autonomous.checks.after_done, autonomous.checks.registry_readme
  - name: ship_key
    description: One of autonomous.ship.push, autonomous.ship.tag, autonomous.ship.ci_watch
  - name: mode
    description: off, shadow or on
exec_steps:
  - tool: checks_status
    description: Read the resolved mode of each check and the findings recorded so far, newest first (read-only)
    args: {}
    read_only: true
  - tool: ship_status
    description: Read the resolved push, tag and CI-watch modes, the tag format, the remote and the recorded ship outcomes, newest first (read-only)
    args: {}
    read_only: true
  - tool: get_config
    description: Read the daemon settings, including the autonomous.checks and autonomous.ship keys
    args: {}
    read_only: true
  - tool: config_set
    description: Set the mode of one check (off, shadow or on); it applies to the next Automaton that reaches that check
    args:
      key: "{{params.check_key}}"
      value: "{{params.mode}}"
    read_only: false
  - tool: config_set
    description: Set the mode of one ship step (off, shadow or on); it applies to the next release that is finished
    args:
      key: "{{params.ship_key}}"
      value: "{{params.mode}}"
    read_only: false
---

# Automata checks and ship steps

The daemon reviews an Automaton's work with four automatic **checks** and,
when a release is finished, carries it out with three **ship steps**. This
guide shows how to see what they found and how to change how strict they are.
The terms are defined in the
[definitions](../datawatch-definitions.md#checks) and the operations
reference is in [operations.md](../operations.md).

## The checks

| Check | Setting | What it looks at |
|---|---|---|
| Pre-start verifier | `autonomous.checks.pre_start_verifier` | A task's verification script exists, is executable and starts with a shebang line, before the task starts |
| Plan rules | `autonomous.checks.plan_rules` | A plan with a feature but no test, a tracker id in a new name, or a fixed release version |
| After-done checklist | `autonomous.checks.after_done` | A completed Automaton: version files, change log, configuration reference, parity issue, registry row |
| Registry check | `autonomous.checks.registry_readme` | The registry table against the plan documents |

Every check is report-only: none edits a file. Each has a mode.

| Mode | What it does |
|---|---|
| `off` | The check does not run. |
| `shadow` | The check runs and records its findings, with no alert and no block. |
| `on` | The default. The check runs, records and alerts; the pre-start verifier also blocks a task start. |

## The ship steps

| Step | Setting | What it does |
|---|---|---|
| Push | `autonomous.ship.push` | Pushes the branch to the remote (`autonomous.ship.remote`, default `origin`) |
| Tag | `autonomous.ship.tag` | Creates an annotated tag named from `autonomous.ship.tag_format` (default `v{version}`) and pushes it |
| CI watch | `autonomous.ship.ci_watch` | Follows the CI runs of the pushed commit and the tag, limited to `autonomous.ship.ci_workflows` |

The modes are the same three. In `shadow` the exact git commands are recorded
and not run. The step never force-pushes and refuses a working tree with
uncommitted changes.

## See the findings

Open the **Automation** view and stay on the **Overview** sub-tab. The
**Automata checks** card lists the mode of each check and the findings;
the **Automata ship status** card says in a line what shipping is, shows
**Push**, **Tag** and **CI watch** as three small tiles with their mode
words, carries the tag format, the remote and the CI workflow names on a
short second line, and reads the outcomes as rows of the Automaton's title
and the result sentence. Both cards are read-only and
refresh every 30 seconds. The small link beside each title opens the
definition.

Without the web UI:

```
datawatch autonomous checks
datawatch autonomous ship --prd <automaton-id>
```

Over the chat channel send `autonomous checks` or `autonomous ship`, with an
Automaton id to narrow the list. Over REST read `GET /api/autonomous/checks`
and `GET /api/autonomous/ship`; MCP agents call `checks_status` and
`ship_status`.

## Change a mode

1. Start in `shadow` for a check or step you have not used before. Run a few
   Automata and read the findings it would have acted on.
2. When the findings look right, set the mode to `on`.

In the web UI use **Settings, General, Automata settings**, groups **Checks**
and **Ship steps**. From the command line:

```
datawatch config set autonomous.checks.plan_rules shadow
datawatch config set autonomous.ship.tag shadow
```

In the configuration file (`autonomous:` block of `datawatch.yaml`), then reload
the daemon with `datawatch reload`:

```
autonomous:
  checks:
    plan_rules: shadow
  ship:
    tag: shadow
    tag_format: v{version}
```

A value other than `off`, `shadow` or `on`, or a `tag_format` without
`{version}`, is refused and nothing is changed.
