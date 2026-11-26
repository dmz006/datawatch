---
docs:
  index: true
  topics: [automation, chain, fork, join, nesting, budget, control, import-graph, max-parallel, stage-gate, decision-wait, run-shape]
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
  {"id":"left","prd_id":"<automaton-2>"},
  {"id":"right","prd_id":"<automaton-3>"},
  {"id":"merge","kind":"join","join_members":["left","right"],
   "on_branch_failure":"all_must_succeed","on_complete":"finish"},
  {"id":"finish","prd_id":"<automaton-4>"}
]'
```

A fork needs at least two branches. A branch is a chain of members: the member
the fork lists in `fork_members` is the chain head, its `on_complete` edges lead
down the chain, and the last member (empty `on_complete`) is the branch tail.
The join's `join_members` names the tails. A branch of one member is the plain
case above, where the head is also the tail. Set only the fields that belong to a
member's kind. `on_branch_failure` on the join decides what a failed branch
means:

| Choice | Result |
|---|---|
| `all_must_succeed` (default) | Every branch must complete; one failure is a failure of the join |
| `any_one_is_enough` | The join passes as soon as one branch completes |
| `continue_anyway` | The join passes once all branches have finished, whatever their outcome |

### Branches as chains

To run several steps down one branch, link them with `on_complete` and list
the head in the fork and the tail in the join:

```json
{"id":"split","kind":"fork","fork_members":["build-a","build-b"]},
{"id":"build-a","prd_id":"<automaton-2>","on_complete":"test-a"},
{"id":"test-a","prd_id":"<automaton-3>"},
{"id":"build-b","prd_id":"<automaton-4>"},
{"id":"merge","kind":"join","join_members":["test-a","build-b"]}
```

A chain member that finishes moves only its own branch to the next member; the
other branches are untouched. Chain members carry `on_complete` only. If a chain
member blocks, fails or is cancelled, or cannot start, its branch ends as failed,
the members behind it are skipped, and the join's `on_branch_failure` rule
decides what that means. A member belongs to one chain, a chain stays between its
fork and join, and chain members must be Automaton, nested Automation or decision wait members.
Status shows each chain under `branch_chains`.

### Limit how many run at once

A fork over many branches starts them all by default. To cap that, set
`max_parallel` on the Automation (the most members running at once across the
whole Automation) or on the fork (the most of that fork's branch members running
at once). Zero or unset means no limit. When both are set, the smaller one wins
for that fork's branches.

Set the Automation limit with `max_parallel` in the body of `POST
/api/automations` (an update that omits it keeps the stored value, and `0`
clears it), with `--max-parallel` on `datawatch automation create` and
`update`, or with the `max_parallel` argument of `automation_create` and
`automation_update` over MCP. Set a fork's limit inside its member JSON, which
works the same from the CLI `--members` array and the MCP `members_json`
argument:

```json
{"id":"split","kind":"fork","fork_members":["a","b","c","d","e"],"max_parallel":2}
```

With a limit of 2 over five branches, the first two in member order start, and
the other three wait. A waiting member shows `waiting_for_slot` in status; it is
not blocked, and the Automation stays active. When a running member finishes,
completed, failed or cancelled, the next waiting member starts, again in member
order. A daemon restart keeps the waiting members waiting and starts them as
slots free. No member kind other than a fork takes the field.

To change the limit on an Automation that is already running, without
resending the whole record (an update replaces the description, budget and
defaults it is not given), use the dedicated call. It changes only the limit,
and on an active Automation it takes effect at once: raising or clearing the
limit starts the waiting members, and lowering it never stops a running member.
Add `member` (CLI), `member_id` (REST, MCP) or `member=` (chat) naming a fork to
change that fork's limit instead.

| Surface | Set the limit to 3 |
|---|---|
| REST | `POST /api/automations/{id}/set_max_parallel` with `{"max_parallel":3}` |
| MCP | `automation_set_max_parallel` with `max_parallel` 3 |
| CLI | `datawatch automation set-max-parallel <id> --max-parallel 3` |
| Chat | `automation set-max-parallel <id> 3` |

### Hold a later stage until an earlier one finishes

A member's `stage` is a label by default (for example `build` or `review`). Set
`stage_gate` to `true` on the Automation and the label becomes an order: a staged
member does not start while an earlier stage still has unfinished members, and it
starts when that stage finishes. Stages are ordered by first appearance in the
`members` array, not alphabetically, and a member with no stage is never held and
never holds anything.

```json
{"name":"ship","stage_gate":true,"entry_member_id":"start","members":[
  {"id":"start","prd_id":"<automaton-1>","on_complete":"split"},
  {"id":"split","kind":"fork","fork_members":["b1","b2","r1"]},
  {"id":"b1","prd_id":"<automaton-2>","stage":"build"},
  {"id":"b2","prd_id":"<automaton-3>","stage":"build"},
  {"id":"r1","prd_id":"<automaton-4>","stage":"review"},
  {"id":"merge","kind":"join","join_members":["b1","b2","r1"]}
]}
```

Here `b1` and `b2` start at once, and `r1` waits until both have finished. A held
member shows `waiting_for_stage` in status with the reason `waiting for stage
build`; it is not blocked, and the Automation stays active. Finished means
completed, failed or skipped, so skipping a build member that is stuck releases
the review stage, while a blocked build member keeps holding it until you deal
with it. A restart keeps held members held and releases them once the earlier
stage is finished.

The gate composes with the limit above: a released member that finds every slot
full waits for a slot instead of starting past `max_parallel`. It is off by
default, and an update that omits `stage_gate` keeps the stored value. Set it
with `stage_gate` in the body of `POST /api/automations`, or the `stage_gate`
argument of `automation_create` and `automation_update` over MCP, or
`--stage-gate` on `datawatch automation create` and `update`.

To flip the gate on a running Automation without resending the whole record,
use the dedicated call. Turning the gate off starts the held members at once;
turning it on holds members that have not started.

| Surface | Turn the gate on |
|---|---|
| REST | `POST /api/automations/{id}/set_stage_gate` with `{"stage_gate":true}` |
| MCP | `automation_set_stage_gate` with `stage_gate` true |
| CLI | `datawatch automation set-stage-gate <id> --stage-gate=true` |
| Chat | `automation set-stage-gate <id> on` |

### Wait for an operator answer on a branch

A member with `kind` `decision_wait` asks you a question and holds only its own
branch until you answer. Give it a `question` and, as on any chain member, an
`on_complete` edge:

```json
{"id":"approve","kind":"decision_wait","question":"Ship the migration now?","on_complete":"deploy"}
```

Put it at the head, in the middle or at the tail of a branch chain. When the
branch reaches it, the daemon raises an operator question (asker `decision-wait`)
that you answer in the Inbox or with `datawatch question answer`, and one alert,
"Automation waiting for you", names it. The other branches keep running, the
Automation is not blocked, and under `all_must_succeed` the join waits. In
status the member shows `waiting_for_operator` with the reason `waiting for you`.
Any answer completes the member and the chain moves on along `on_complete`. If
you withdraw the question instead, that branch fails with the reason on the
decision log and the join rule decides the rest. The question needs
`autonomous.questions.enabled`. A restart keeps the one open question linked to
its member.

Besides writing the member into the `members` array, you can add a decision wait
to an Automation with the insert call (on a draft it lands at once; on a started
Automation it is held until you approve it, like any insert):

| Surface | Add a decision wait |
|---|---|
| REST | `POST /api/automations/{id}/insert_member` with `{"member":{"kind":"decision_wait","question":"Ship it?"},"reason":"sign-off"}` |
| MCP | `automation_insert_member` with `question` and `reason` |
| CLI | `datawatch automation insert <id> --kind decision-wait --question "Ship it?" --reason "sign-off"` |
| Chat | `automation insert <id> decision-wait Ship it? \| sign-off` (question, then `\|`, then the reason) |

While branches run, the Automation has several active cursors. Pause, resume
and cancel act on the whole Automation; skip and edge edits name one member.
Same call over MCP: `automation_create` with the same members in `members_json`.

### Which wait is this?

When a member shows a wait, the status names the cause. Use this table to tell them apart and to see what moves it on:

| Status | Cause | What moves it on |
|---|---|---|
| `waiting_for_slot` | The max parallel limit is full | A running member finishes, or you raise the limit |
| `waiting_for_stage` | An earlier stage has unfinished members (stage gate on) | Finish or skip those members, or turn the gate off |
| `waiting_for_operator` | A decision wait is asking you a question | Answer the question in the Inbox |

None of the three is a failure: the Automation is not blocked and nothing needs a retry. A member that is `blocked` is different, and it keeps holding the later stage until you resume or skip it.

### A plan with three chains

One plan that uses all three run-shape controls together: two check chains and a sign-off chain under a single fork, a two-member limit, and a `check` before `ship` stage gate:

```bash
datawatch automation create --name ship-check --stage-gate=true --max-parallel 2 --members '[
  {"id":"start","prd_id":"<automaton-1>","on_complete":"split"},
  {"id":"split","kind":"fork","fork_members":["check-a","test-suite","sign-off"],
   "max_parallel":2},
  {"id":"check-a","prd_id":"<automaton-2>","stage":"check","on_complete":"package"},
  {"id":"package","prd_id":"<automaton-5>","stage":"ship"},
  {"id":"test-suite","prd_id":"<automaton-3>","stage":"check"},
  {"id":"sign-off","kind":"decision_wait","question":"Ship this build?","stage":"check","on_complete":"tag"},
  {"id":"tag","prd_id":"<automaton-4>","stage":"ship"},
  {"id":"merge","kind":"join","join_members":["package","test-suite","tag"],
   "on_branch_failure":"all_must_succeed","on_complete":"finish"},
  {"id":"finish","prd_id":"<automaton-6>"}
]'
```

The same shape is created with `automation_create` over MCP: the same members in `members_json`, with `max_parallel` set to `2` and `stage_gate` to `true`.

What happens next, in order:

1. `start` finishes and the fork releases its three branch heads in member order. With the limit at 2, `check-a` and `test-suite` start and `sign-off` waits with `waiting_for_slot`. The gate holds nothing yet: all three are in `check`, the first stage, and `start` has no stage, so it was never held and holds nothing.
2. `check-a` finishes. Chain A moves to `package`, but the `check` stage is still not finished — `test-suite` is still running and `sign-off` has not completed — so `package` waits with `waiting_for_stage`, reason `waiting for stage check`. `sign-off` takes the free slot, raises the question "Ship this build?" and holds only its own branch with `waiting_for_operator`.
3. You answer the question in the Inbox (or with `datawatch question answer`). The answer completes `sign-off` and chain C moves to `tag`, which the gate still holds because `test-suite` is still in `check`.
4. `test-suite` finishes. The `check` stage is now finished — members completed, failed or skipped count — so `package` and `tag` are released and start together. That is exactly the two Automation-wide slots; nothing can start a third.
5. `package` and `tag` both finish. All three tails the join waits on (`package`, `test-suite`, `tag`) have finished, the join passes under `all_must_succeed`, and `finish` runs.

If you withdraw that question in step 3 instead of answering it, that branch fails with the reason on the decision log; the gate keeps holding `package`, and under `all_must_succeed` the failed branch fails the join, whose `on_failed` edge decides what runs next.

In the web UI you can watch each step: `sign-off` reads **Waiting for the operator**, `package` reads **Waiting for the stage** and names the stage it waits on, and the line above the strip counts each kind of wait and shows the max parallel of 2 and the stage gate.

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
| Insert | `automation insert <id> <automaton> --reason "..." [--urgent] [--after <member>] [--stage <stage>]` | `automation_insert_member` | `insert_member` | Not finished; a reason is required; `--stage` records an optional stage label on the member (comm: `stage=<stage>` token) |
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

The Inbox cards carry their own **?** help entries: the needs-attention card and the held-questions card each have a **?** help entry that opens its section in the tab guide — ([the needs-attention card](automation-tab.md#the-needs-attention-card), [the held-questions card](automation-tab.md#the-held-questions-card)) — and the behaviour behind the waits those cards list is section 3 here.

The strip shows what the run shape does. A fork branch is drawn as its chain of
members in order, joined at the end by the join. A member waiting for your answer
(a decision wait) reads **Waiting for you**, not Blocked, and has an **Answer the
question** link that opens the question; the same question is in the Inbox with a
waiting-for-you badge, and the "Automation waiting for you" alert links to it. A
member held by the limit reads **Waiting for a free slot**, and one held by the
stage gate reads **Waiting for stage** and the stage's name. The line above the
strip counts each kind of wait and shows the Automation's max parallel and stage
gate when they are set. In the create and edit form, the **Run shape** section sets
max parallel and the stage gate, a fork row has its own max parallel, **Add a
decision wait** adds a member with its question, and each branch member has a
button that adds the picked Automaton to that branch.

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

## 9. Activate an Automation whose entry is a fork or a nested Automation

The entry member need not be a plain Automaton — you may point `entry`
(`--entry-member`) at a fork or at a nested Automation, and the activation
call itself is unchanged from section 2, on every surface:

```bash
datawatch automation activate <id>        # CLI
```

MCP: `automation_activate` (`id`; optional `actor`, `reason` — both recorded
in the audit trail). REST: `POST /api/automations/{id}/activate` with an
optional `{"actor": "...", "reason": "..."}` body (actor defaults to
`operator`). Comm channel: `automation activate <id>`.

What happens depends on the entry member's kind, and each kind goes through
the same approval gate the coordinator uses between members:

| Entry member | What activation does |
|---|---|
| A plain Automaton | Starts it through its own gate (a paused one resumes; an approvable one runs). If the gate refuses it (for example the Automaton is not approved), nothing starts: the Automation becomes **blocked**, a decision note names the entry Automaton with the reason, and **one** alert says why it cannot start |
| A **fork** | Fires every branch in sorted order, each starting through its own gate, and evaluates the paired join. A branch the gate denies is recorded as failed (the decision notes why) while the startable branches still begin — the Automation stays **active** until the join settles under its `on_branch_failure` rule |
| A **nested Automation** (`kind` `automation`) | Activates the child (it must be a draft with a startable entry of its own). When the child completes, is blocked or is cancelled, the matching edge on the parent member is followed, exactly as for any later nested member (section 4). A child that cannot be activated blocks the parent: one alert and a decision note carry the child's refusal |

Activation only takes a `draft` Automation: activating the same Automation
twice, or one that is already active, paused or terminal, is **refused with
HTTP 409** and names its current status. An unknown ID is 404. A fork entry
that fires is not an error: the fork member reads `completed` (outcome
`forked`), the branches run, and the cursors sit on the branches until the
join passes.

## 10. Return a blocked Automation to the draft (before anything started)

An Automation that went **blocked before any member started** — an entry the
gate refused, a join that could not settle, a member with no edge to follow —
can be put back where it was when you created it, instead of cancelling. The
call makes no model call and starts no member. Its effects:

- the status goes back to `draft`; the started time, the cursors and the
  budget episode of the blocked spell are cleared;
- **every member is `pending` again, with no started or finished
  timestamps**, so edits to members and edges are refused no more;
- activating it afterwards (section 9) runs it **as if it had never
  started**, through the same gates.

The same call on every surface:

```bash
datawatch automation return-to-draft <id>       # CLI (--actor, --reason,
                                                #  --expected-version like the
                                                #  other control verbs)
```

MCP: `automation_return_to_draft` (`id` required; `expected_version` optional,
`actor` optional and recorded in the audit trail). REST:
`POST /api/automations/{id}/return_to_draft` with an optional
`{"actor": "...", "reason": "...", "expected_version": n}` body (all optional; actor defaults to `operator`). Comm channel: `automation return-to-draft <id>` (actor set to `comm` by the daemon).

`200` returns the Automation back in `draft`; an unknown ID is 404. The call
is **refused with HTTP 409 and names the reason** when:

| Situation | Why |
|---|---|
| The Automation is not `blocked` (it is `draft`, `active`, `paused` or terminal) | only a blocked Automation can go back; the 409 names its real status |
| Any member already started, or a cursor sat on a member when it blocked | the Automation ran before it blocked, so its history must be kept — cancel it instead, with the `--cancel-in-flight true\|false` choice (section 6) |
| `expected_version` does not match the stored version | the same optimistic-lock rule as every other control call (section 6); omit an `expected_version` (or pass none) to skip the check |

The call writes an audit entry under `automation_return_to_draft` naming the
actor (and the reason, when you give one), so the trip blocked → draft →
active again is visible in the history.

## For operator review

These are not settled by the code and are left for you:

- **Mobile parity.** The Android and iPhone apps do not yet show the waiting
  states (waiting for you, for a slot, for a stage) or the Run shape form
  section. Parity is tracked in the datawatch-app repository, not here.
- **Decision waits need questions on.** A `decision_wait` member cannot raise its
  question while `autonomous.questions.enabled` is off. Check the setting before
  you build a plan that depends on one.
- **Concurrency limit.** `max_parallel` is unlimited until you set it. Choose a
  limit for any plan with a wide fork before you run it.
- **Join rules.** A failed chain member fails its branch, and the join's
  `on_branch_failure` decides what that means. Check each chain's join rule on
  your own plans.
- **Non-English wording.** The web app text for the limit, the decision wait, the
  stage gate and the waiting states is in the German, Spanish, French and
  Japanese bundles too. Read it once in the web app.

## Related

- [automation-queue-advance.md](automation-queue-advance.md): how the hand-off, idle alert and restart recovery work
- [automation-tab.md](automation-tab.md): the web UI in detail
- [automata-orchestrator.md](automata-orchestrator.md): the older graph orchestrator
- [datawatch-definitions.md](../datawatch-definitions.md): the entries for branch chain, max parallel, decision wait and stage gate
- [operations.md](../operations.md#automation-parallelism-decision-waits-and-stage-gating): the four run-shape features side by side
