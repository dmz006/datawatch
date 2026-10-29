---
docs:
  index: true
  topics: [automation, chain, fork, join, nesting, budget, control, import-graph]
exec_params:
  - {name: automation_id, required: true, description: "Automation ID to read"}
  - {name: graph_id, required: true, description: "Orchestrator graph ID to preview as an Automation"}
exec_steps:
  - tool: automation_list
    description: List the Automations and their status
    args: {}
    read_only: true
  - tool: automation_get
    description: Read one Automation (status, members, edges, budget policy)
    args: {id: "{{params.automation_id}}"}
    read_only: true
  - tool: automation_import_graph
    description: Preview an orchestrator graph as an Automation (dry run only, nothing is stored)
    args: {graph_id: "{{params.graph_id}}", dry_run: true}
    read_only: true
---

# How-to: Automation chains

## 1. What an Automation is

An **Automation** is a named chain of Automatons. An **Automaton** is one unit
of work: a goal that is decomposed, approved, run and verified. An Automation
does not do the work itself. It decides which Automaton runs next, using one
edge per outcome on every member: `on_complete`, `on_blocked` and `on_failed`.
An empty edge means "stop here"; the Automation then becomes `blocked` and an
alert is raised.

Once an Automation is **active**, datawatch starts the next Automaton by
itself, with no chat session or operator call in between. Members can also be
a **fork** and **join** (run branches in parallel) or another Automation
(nesting). The lifecycle is `draft → active → paused → completed / blocked /
cancelled`. The hand-off itself is described in
[automation-queue-advance.md](automation-queue-advance.md).

Base requirements: a daemon with `autonomous.enabled: true`, and approved
Automatons that no other Automation owns.

Read what exists first:

```bash
datawatch automation list
datawatch automation get <id>
```

MCP: `automation_list`, `automation_get`. REST: `GET /api/automations`,
`GET /api/automations/{id}`.

## 2. Create a chain of three Automatons

Members go in as a JSON array, inline or as a file path. Each member has an
`id`, the `prd_id` of its Automaton, and its edges. Here A leads to B leads to
C:

```bash
datawatch automation create --name nightly \
  --entry-member a \
  --members '[
    {"id":"a","prd_id":"<automaton-1>","on_complete":"b","added_reason":"build"},
    {"id":"b","prd_id":"<automaton-2>","on_complete":"c","added_reason":"test"},
    {"id":"c","prd_id":"<automaton-3>","added_reason":"publish"}
  ]'
datawatch automation activate <id>
```

MCP: `automation_create` (`name`, `entry_member_id`, `members`), then
`automation_activate`. REST: `POST /api/automations`, then
`POST /api/automations/{id}/activate`. The Automation stays a draft, and does
nothing, until you activate it. Activation validates the whole graph (unknown
members, cycles, ownership) and refuses with the reason when it is wrong.

To route a failure somewhere else, add `on_blocked` or `on_failed` pointing at
a fix-up member, for example `"on_failed":"fixup"`. You can change a draft with
`datawatch automation update <id> --members <file>`.

## 3. Fork and join

A **fork** starts several branch members together. A **join** waits for those
same branches, then continues. This diamond runs `left` and `right` in
parallel between `start` and `finish`:

```bash
datawatch automation create --name diamond --entry-member start --members '[
  {"id":"start","prd_id":"<automaton-1>","on_complete":"split"},
  {"id":"split","kind":"fork","fork_members":["left","right"]},
  {"id":"left","prd_id":"<automaton-2>","on_complete":"merge"},
  {"id":"right","prd_id":"<automaton-3>","on_complete":"merge"},
  {"id":"merge","kind":"join","join_members":["left","right"],
   "on_branch_failure":"all_must_succeed","on_complete":"finish"},
  {"id":"finish","prd_id":"<automaton-4>"}
]'
```

A fork needs at least two branches, and the join's `join_members` must equal
the paired fork's `fork_members`. Set only the fields that belong to a
member's kind. `on_branch_failure` on the join decides what a failed branch
means:

| Choice | Result |
|---|---|
| `all_must_succeed` (default) | Every branch must complete; one failure is a failure of the join |
| `any_one_is_enough` | The join passes as soon as one branch completes |
| `continue_anyway` | The join passes once all branches have finished, whatever their outcome |

While branches run, the Automation has several active cursors. Pause, resume
and cancel act on the whole Automation; skip and edge edits name one member.
Same call over MCP: `automation_create` with the same `members` array.

## 4. Nest an Automation inside another

A member with `kind` `automation` runs a whole child Automation as one step of
the parent. Create the child first, then name it with `automation_id`:

```bash
datawatch automation create --name release-checks --entry-member x --members '[...]'
datawatch automation create --name release --entry-member build --members '[
  {"id":"build","prd_id":"<automaton-1>","on_complete":"checks"},
  {"id":"checks","kind":"automation","automation_id":"<child-id>","on_complete":"ship"},
  {"id":"ship","prd_id":"<automaton-2>"}
]'
```

The child completes, is blocked or fails like any member, and the matching
edge on the parent member is followed. A child can be nested by only one
unfinished parent, and an Automation cannot reach itself through its
children; activation refuses either with the reason. MCP: `automation_create`.
REST: `POST /api/automations`.

## 5. Budget

A budget policy caps the spend of one Automation, or of one member, with a
ceiling narrower than any global one. The tightest ceiling always wins; a
policy never widens a global limit.

```bash
datawatch automation set-budget <id> \
  --ceiling-type dollar --ceiling-value 40 --window week \
  --pacing even --on-breach pause --reset-weekday monday --reset-time 00:00
datawatch automation set-budget <id> --member b --estimated-cost-usd 6
datawatch automation set-budget <id> --clear
```

MCP: `automation_set_budget`. REST: `POST /api/automations/{id}/set_budget`.
`--window` is `automation` (lifetime), `day`, `week` or `month`;
`--ceiling-type` is `dollar` or `allocation_pct` (an estimate only).
`--pacing even` holds spend to the share of the ceiling that matches the
elapsed part of the window.

On a breach, `--on-breach` chooses the response, and an alert is raised either
way:

- **pause** (default): the Automation becomes `paused`. Where the reset time is
  known (a day, week or month window, or a reset time read from a rate-limit
  message) it resumes by itself at the reset. If the reset time is not known it
  waits for you.
- **block**: the Automation becomes `blocked` and never resumes by itself. You
  raise the ceiling or clear the policy, then resume.

## 6. Control operations

Every control call is checked against the Automation's current `version`
(CLI: `--expected-version`). Left out, the CLI reads the Automation's
current version first and uses it, retrying the call once if the version
moved in between. A `--expected-version` you supply yourself is sent as is,
and a stale one is still refused with HTTP 409 either way. `--actor` and
`--reason` are recorded in the audit trail.

| Operation | CLI | MCP | REST (`/api/automations/{id}/...`) | Allowed when |
|---|---|---|---|---|
| Pause | `automation pause <id>` | `automation_pause` | `pause` | Active. Running members finish; nothing new starts |
| Resume | `automation resume <id>` | `automation_resume` | `resume` | Paused or blocked |
| Skip | `automation skip <id> <member> --edge on_complete` | `automation_skip_member` | `skip_member` | The member is stuck or not started; follows the edge you choose |
| Insert | `automation insert <id> <automaton> --reason "..." [--urgent] [--after <member>]` | `automation_insert_member` | `insert_member` | Not finished; a reason is required; one urgent insert at a time |
| Remove | `automation remove <id> <member>` | `automation_remove_member` | `remove_member` | The member has not started |
| Reorder edges | `automation edges <id> <member> --on-complete <m> --on-blocked <m> --on-failed <m>` | `automation_reorder_edges` | `reorder_edges` | The member has not started, or is the running member (its own outgoing edges) |
| Cancel | `automation cancel <id> --cancel-in-flight true\|false` | `automation_cancel` | `cancel` | Draft, active, paused or blocked |

`edges` rewrites all three edges: one you leave out becomes "stop", so name
every edge you want to keep. A member that has already completed, been
blocked or been skipped is history and cannot be removed or rewired.

**Cancel makes you choose.** `--cancel-in-flight` has no default. `true` ends
the sessions of members running now; `false` lets them finish while nothing
new starts. Cancelling cannot be undone, so the call is refused without the
choice.

```bash
datawatch automation pause <id> --reason "deploy freeze"
datawatch automation skip <id> b --edge on_failed --reason "known flaky"
datawatch automation insert <id> <automaton> --urgent --reason "hotfix first"
datawatch automation cancel <id> --cancel-in-flight false
```

An insert made by an agent can be held for your approval. `datawatch
automation get` and `datawatch automation status` list each pending insert
with its member id, its `prd_id`, and the reason it was held, and show the
command that approves it —
`datawatch automation approve <automation id> <member id>`
(MCP `automation_approve_insert`).

## 7. The Automation tab in the web UI

The **Automation** tab sits left of the Automata tab. It shows one card per
Automation with its status, and opens a strip that draws the chain, including
forks and joins and the branches live right now. Above the strip are the
Pause, Resume and Cancel buttons (Cancel always asks the in-flight question);
each member card carries Skip, Insert, Remove, Edit edges and Approve insert.
Anything changed by someone else while you look is flagged before you save.
See [automation-tab.md](automation-tab.md) for the full tour.

## 8. Move from an old orchestrator graph

An orchestrator graph can be turned into a draft Automation. Always preview
first; `--dry-run` stores nothing and prints the Automation that would be
created, plus any warnings:

```bash
datawatch automation import-graph <graph-id> --dry-run
datawatch automation import-graph <graph-id>      # prints the new Automation ID
```

MCP: `automation_import_graph` with `graph_id` and `dry_run`. REST:
`POST /api/automations/import-graph/{graph_id}` with `{"dry_run": true}`.
Automaton nodes become members, fan-out and fan-in become a paired fork and
join, and guardrail nodes are dropped with one warning each (per-Automaton
guardrails cover them now).

A graph that an Automation cannot express is **refused** with the reason and
a non-zero exit (HTTP 422), because guessing would change what runs. That
covers a node with an empty or duplicate ID, an unknown node kind, a node that
depends on an unknown node, an Automaton node with no Automaton ID, a graph
with no Automaton nodes, a graph in several disconnected pieces (import each
separately), and a shape that cannot be placed in a chain.

The source graph is only read. It is **never deleted or changed**, so it keeps
working beside the new draft. Review the draft, activate it, and retire the
graph yourself when you are satisfied.

## Related

- [automation-queue-advance.md](automation-queue-advance.md): how the hand-off, idle alert and restart recovery work
- [automation-tab.md](automation-tab.md): the web UI in detail
- [automata-orchestrator.md](automata-orchestrator.md): the older graph orchestrator
