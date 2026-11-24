---
docs:
  index: true
  topics: [autonomous, automation, supervisor, supervisor watch, housekeeping, repairs, findings, grace period, task chain, judge, lane fill, leftovers, planning, planning failure, live daemon housekeeping routes]
exec_steps:
  - tool: supervisor_watch_list
    description: Read the supervisor watch findings, open first - the kinds in this table are the finding kinds you see there
    args: {}
    read_only: true
  - tool: autonomous_config_get
    description: Read the autonomous settings, including the supervisor watch block and the five repair groups
    args: {}
    read_only: true
---

# Automaton housekeeping repairs

The supervisor script (`scripts/supervisor-watch.py`) and the daemon's
[supervisor watch](automation-supervisor-watch.md) both find the same kinds of
trouble in the Automata and the machines that run them. The daemon's repair
framework runs the repairs itself, but every group defaults to `report` — so
until an operator turns a group to `on`, the operator still fixes each finding
by hand, and the [supervisor loop](supervisor-loop.md) keeps the mechanical
steps moving. This page is the single table of **every finding kind**:
what causes it, whether the daemon can fix it safely by itself, what it does and
after how long, and what is always left for you.

**Where the code stands.** The watch is report-only in every mode: in `report`
each group records what it would do and acts on nothing, and whatever a group's
mode, nothing here ever merges, ships, ends a session or touches a security
workflow. The repair framework is running: the daemon builds the Repairer at
startup and runs the pass on the supervisor loop's schedule (each idle tick,
after the loop's steps). The
five group settings and the grace setting under
`autonomous.housekeeping_repairs` are part of the config, are validated when
the daemon loads it, and are accepted at runtime on every config set path —
`config set` on the CLI, `PUT /api/config`, `config_set` on MCP, `configure` on
a comm channel, and the group-rows in Settings of the web UI. Each group
defaults to `report`, so a daemon that never sets one records what it would do
and acts on nothing; a group is `on` only because the operator set it. The
grace is the wait before a repair acts; a repair never acts on the poll that
first sees a finding.

The repairs the daemon builds for the pass, and the settings that switch them
on, are described group by group below, together with exactly what they do,
what they never do, and how to undo them. A live daemon answers the routes
listed under **Reading the repairs**; a **503** `autonomous disabled` there
means Automata are off on that daemon, not a bug. The housekeeping action
routes (unlanded-work keep, archive-branch and delete-worktree, the
uncommitted-work diff and commit, and fix-permissions) also work on the live
daemon: the wrapper the daemon wires into the server carries every method their
gate needs (the build proves it), so the list read answers an empty list, never
an error. A call is refused at the gate while Automata are off (**503**),
without the autonomous capability (**403**), or with the wrong method
(**405**); an action that passes the gate still fails with its own answers —
**400** for incomplete input (a delete without `confirm: true`, no base
repository, an empty commit message), **404** for a branch not in the current
unlanded list, **409** for the action's own failure (a delete whose bundle
backup fails removes nothing). An answer of **503** `Housekeeping actions
unavailable` on any of them comes from an old daemon build, not a setting —
upgrade the daemon. See
[Automaton worktree cleanup](automaton-worktree-cleanup.md#these-routes-work-on-the-live-daemon).

**Reading the repairs.** One read-only view shows, per group, the mode in
effect, what a report-mode pass would do (`would`), what an on-mode pass did
(`did`), and the cases that failed or were skipped as unsafe. Each entry says
what was found, what was done and how to undo it. Reading it never repairs
anything, raises an alert or changes a mode. It is on every surface:

| Surface | How |
|---|---|
| REST | `GET /api/autonomous/housekeeping-repairs` |
| MCP | `housekeeping_repairs` (no arguments; any argument is refused) |
| CLI | `datawatch autonomous housekeeping-repairs` (`--json` for the raw body) |
| Chat | `autonomous housekeeping-repairs` |
| Web UI | the **Housekeeping repairs** card on the Automation view's Housekeeping sub-tab, refreshed every 30 seconds, and a group row under Settings, Automata, Housekeeping repairs |

The same view lists **intermittent test failures** after the groups. When the
merge check of a finished Automaton runs the tests and one fails on the first
run and passes on the rerun, the daemon files a defect record with the test
name, the failure message, the first lines of the failing output, the date and
the commit under test, and raises one alert titled with the test name. The merge
is not blocked. The same test at the same commit is one record, however often
the check runs. These rows have no mode and nothing to repair: they are on every
surface above (REST field `intermittent_defects`, the same MCP tool, CLI and
chat verb, and the card) and need a fix in the test or the code.

A group's mode is changed only through the config set paths, key
`autonomous.housekeeping_repairs.<group>`; none of the read surfaces can change
it. In `report` only `would` entries are produced; in `on`, `did`, `failed` and
`skipped_unsafe` entries appear as the pass repairs.

**What the shipped framework repairs.** The framework is narrower than the
table below. The table is the full set of finding kinds and the design for each.
The repairs the daemon builds are: these five, one per group, each built on a
mechanical step the [supervisor loop](supervisor-loop.md) already has, plus the
extras described in the group sections below (three in `task_chain`, three in
`advance_judge` and one in `planning`). Two are in the code and tested but the
daemon does not build them yet, so turning their group to `on` does not
activate them: the `lane_fill` group's lane-fill start and the `leftovers`
group's three release repairs.

| Group | What the shipped repair finds | What it does in `on` |
|---|---|---|
| `task_chain` | An active Automation whose next chain step is waiting and nothing started it. | Starts the next chain step. |
| `advance_judge` | A task held for review after its infrastructure retries ran out. | Retries the failed infrastructure task. |
| `lane_fill` | The running lane count differs from the wanted count, inside the lane bounds. | Corrects the lane count to the wanted one. |
| `leftovers` | Leftover files crossed the disk cleanup alert threshold. | Runs the cleanup sweep, which only refreshes the cleanup status and raises the disk alert. |
| `planning` | A draft Automaton with no stories whose planning failed for a reason retrying will not change, where every spec lint finding is one the daemon can reword. | Rewords the spec with the lint suggestions, applies it as a revision and plans again: the same path as the planning question's *Fix the wording and plan again* answer. |

The `task_chain` group also has three repairs for stuck task chains, each run in
`on` and only reported in `report`:

- **Dependent reset.** A task that failed only because a task it depends on
  failed is reset (a plain reset, never a force-cancel) once every dependency is
  done, and not before.
- **Stale docs mirror.** A final gate that failed because the gitignored embedded
  docs mirror (`internal/server/web/docs`) is out of date runs `make sync-docs`
  in the Automaton worktree, then the gate is retried once.
- **Leaked file.** A leaked marker or stray file that blocks a finish is
  identified. Only if it is untracked and inside the Automaton worktree is it
  removed with `git clean -f`, and then the task is retried once.

Each refuses, records a skipped entry and raises one alert when the failure
reason is ambiguous, a second retry would be needed, or the file is tracked.
Their undo hints: the dependent reset is undone by cancelling the task again;
the docs sync only refreshes a gitignored copy, so cancel the retried gate to
stop it; for a removed stray file, restore it from the worker's session output
if needed and cancel the retried task to stop it.

The `advance_judge` group also has three repairs for finished and review-blocked
Automata, run in `on` and only reported in `report`:

- **Advance.** An Automaton that is running with every task finished (done,
  failed or cancelled) is moved to its next state by the normal completion step,
  once the grace has passed. It is left alone while a live run still holds it,
  and it is refused as unsafe while any task is unfinished.
- **Judge repair.** An Automaton blocked by the rules review with no repair task
  yet gets one repair task, made once from the review's real findings (a task
  carrying the findings is added to the last story, then reset with a plain
  reset). The fact is recorded on the Automaton so a second one is never made. If
  someone already made a repair task, it is theirs to finish and is left alone.
- **Merge report.** An Automaton waiting to merge or ready to merge is only
  reported. Nothing here can merge.

The advance and judge repairs refuse, as an unsafe finding (recorded as skipped,
one alert), when a task is not finished, when the judge repair was already made
once, or when the review recorded no findings. They each read the Automata once
per pass and make at most one finding per Automaton, worded from a single check
of its state, so a finding never says both that every task finished and that
some are not finished. Each read gives one finding per stuck Automaton, never the same
Automaton listed several times. An advance finding has one of two shapes:

| Shape | Finding text | Safe | What the pass does |
|---|---|---|---|
| Stuck but finished | `Automaton "<title>" is stuck: it is running and all of its tasks are finished, but it is not advancing` | Yes | After the grace, in `on`, advances it by the normal completion step |
| Unfinished tasks | `Automaton "<title>" is running with N of M tasks not finished` | No | Records it as skipped, with the reason `N of M tasks are not finished, so advancing could lose work`; never advances it |

A running Automaton whose tasks are all finished but which a live run still
holds is not a finding at all. See **What a skipped unsafe finding means**
below for how to act on the unsafe shape.

Their undo hints: the advance moved on by the normal completion step, so pause or cancel the Automaton from the
Automata page to stop it; for the judge repair, cancel the repair task from the
Automaton's task list (it is recorded and will not be made again); the merge
report changed nothing.

The `lane_fill` group also has one repair for an idle lane, run in `on` and only
reported in `report`. It is written and tested; the daemon does not build it
yet (the repairs in the same group do run), so it acts on nothing today:

- **Lane fill start.** A lane that is idle while eligible work waits for it gets
  the next eligible Automaton started, through the normal run path, once the
  grace has passed. It changes no model and no backend: the Automaton starts
  exactly as queued, and it can do nothing else (no merge, ship, session end or
  security workflow).

It refuses, as an unsafe finding (recorded as skipped, one alert), when no
eligible Automaton waits for the lane, capacity is full, the lane is at its
inflight cap, the usage guard is paused, or the only eligible Automata need a
backend the lane may not use. It starts at most one Automaton per finding and
checks again just before it acts, so a lane that has filled in the meantime is
left alone. Its undo hint: pause or cancel the started Automaton from the Automata
page; nothing else was changed. The lane fill rules themselves are in
[automation-lane-fill.md](automation-lane-fill.md).

The `leftovers` group also has three release repairs for what a **finished**
Automaton leaves behind, run in `on` and only reported in `report`. They are
written and tested; nothing in the daemon builds or starts them yet, so they
act on nothing today (the group's disk-cleanup sweeps do run):

- **Lease release.** Releases the capacity lease a finished Automaton still holds.
- **Worktree release.** Removes the worktree a finished Automaton still holds.
- **Tmux name release.** Releases the tmux session name a finished Automaton
  still holds.

Each acts on one leftover at a time, and only when the safe-to-delete check
passes for that Automaton: it has reached a final state, it holds no uncommitted
work, it has no commits that have not landed, and no session is running in it.
Just before releasing, the repair reads those facts again, so a leftover that
changed in the meantime is left alone. When any fact fails, or the check cannot be
answered, the repair refuses as an unsafe finding (recorded as skipped, one alert)
and nothing is released. It can only release one lease, remove one worktree or
release one tmux name: it never kills, ends or signals a session, never merges or
ships, never changes a model or backend, and never touches a security workflow.
Its undo hints: a released lease can be taken again by starting the Automaton from
the Automata page; a removed worktree held no work, so running the Automaton again
recreates it; a released tmux name is free and the next session start picks it up.

The review start, lease re-adoption, placement re-run and worktree pruning rows in
the table are not written yet. The new release repairs cover finished Automata
only; the `LEASE` and `WORKTREE` rows below describe running ones.

**Operator review before relying on this page.** Check these against the
shipped code before you turn any group to `on`:

- The table's "after the grace" waits are the grace setting
  `autonomous.housekeeping_repairs.grace_seconds`, which defaults to **300
  seconds (5 minutes)** and applies to every group; there is no longer or
  per-group wait (for example none for `WORKTREE`). `0` means no grace.
- The table's repair columns (a judge re-run, a single reset of a failed task,
  worktree pruning, lease and placement fixes) are the design. The repairs the
  daemon builds are the five above plus the three `task_chain` extras, the three
  `advance_judge` extras and the one `planning` extra; the `lane_fill` group's
  lane-fill start and the `leftovers` group's three release repairs are in the
  code but the daemon does not build them yet. Confirm which of the table's
  rows you still want built.
- The three task chain repairs run fixed commands (`make sync-docs`, `git clean -f -- <one path>`) in
  the Automaton worktree. Confirm you accept those commands, and that a final-gate failure
  naming `internal/server/web/docs` as stale is a reliable signal, before turning `task_chain` to `on`.
- The three leftover release repairs delete a worktree and release a lease and a
  tmux name of a finished Automaton. Confirm you accept that, and that "no
  uncommitted work, no unlanded commits and no live session" is a strong enough
  test for you, before turning `leftovers` to `on`. The `leftovers` group also
  holds the older disk cleanup sweep, so `on` switches both. The release repairs
  have no setting of their own and the daemon does not build them yet, so
  turning `leftovers` to `on` activates the sweep, not the release repairs.
- The planning repair rewrites a draft's spec and plans it again without you.
  Confirm you accept that, and that "a draft with no stories and a recorded
  non-retryable planning failure whose every spec lint finding can be reworded"
  is a reliable signal, before turning `planning` to `on`. In `report` it only
  records what it would do. The reworded spec is a normal revision on the
  decision log. It has no setting beyond the group mode.
- The lane fill start repair starts a real Automaton on an idle lane. Confirm you
  accept that it starts work without you, that it follows capacity, the inflight
  caps and the usage guard as described, and that it is separate from the lane
  fill's own shadow/live mode, before turning `lane_fill` to `on`. Note that the
  daemon does not build this repair yet, so turning the group to `on` does not
  activate it; the same group's lane-count correction and the table's `LEASE`
  and `PLACEMENT` rows are a different matter and are not changed by this
  repair.
- The advance repair re-fires the completion step, and the judge repair adds
  one task titled "Fix rules review findings" (actor `housekeeping-repair`) to
  the Automaton's last story. Confirm you accept both, and that "blocked with a
  rules review outcome" is a reliable signal, before turning `advance_judge` to `on`.
- The table's `STUCK` row describes asking the judge to run again, and `MERGE WAIT`
  describes starting a review; the shipped advance repair instead re-fires the
  completion step for a running Automaton whose tasks are all finished, and
  `MERGE WAIT` / `MERGE READY` are reported only. Decide whether the table rows
  should be reworded or built as written.
- The read view and its card are new. Confirm the card's wording (it says a
  stuck state "is looked at after N seconds") reads right to you. The five
  groups' Settings rows save the values, and the settings are accepted on
  every config set path as well as the config file; invalid values are refused
  on all of them.
- The pass runs on the supervisor loop's schedule, so in `report` (the
  default) the card, the REST route, the MCP tool, the CLI and the chat verb
  show `would` entries as soon as the pass sees a finding, and in `on` they
  show `did`, `failed` and `skipped_unsafe` entries as it repairs. Turn a
  group to `on` deliberately.
- An unsafe finding is never repaired: it is recorded as skipped, and one
  warning alert is raised for that subject until it stops being found.

Names are the script's printed words. The watch's own `kind` for the same
problem is given where one exists (for example `STUCK` and `MERGE WAIT` have no
kind in the watch yet, and `CI STUCK` is `ci_stuck`).

## The finding table

| Finding | Cause | Safe for the daemon alone? | What the daemon does, and after what grace | What stays for an operator |
|---|---|---|---|---|
| `STUCK` | A running or blocked Automaton has every task done or cancelled, but the completion judge refuses to run while a task is cancelled; or it is blocked as "stuck running". | Yes for the cancelled-task case: the repair only re-runs the judge, it changes no work. | Group **advance_judge**: after the grace it asks the judge to run again. A "stuck running" block is reported, not reset. | Deciding whether the cancelled tasks should be reset or removed; any "stuck running" block. |
| `MERGE WAIT` | Every task is done, but the completion rules review has not produced a verdict. | Yes: starting a review that has not run is a read of the work. | Group **advance_judge**: after the grace it starts the review if none is running. | A review that keeps failing to start. |
| `MERGE READY` (`automaton_not_merged`) | Every task is done, the review passed or warned, and the branch is not yet on the base branch. | No: merging is never automatic from the watch. | Reports it. Nothing else. | The merge and the ship. |
| `JUDGE BLOCKED` | Every task is done but the rules review blocked the Automaton. | Partly: the Automaton's own corrective path may add work, but the watch does not decide a block is wrong. | Group **advance_judge**: reports it and makes sure the corrective-work path has run once. It never overrides the verdict and never merges. | Reading the verdict, and any decision to override or reject it. |
| `NEW failed task` (`task_failed`, `task_review_needed`) | A task failed, or is held for a person to review. | Yes for a first failure after an infrastructure cause; no for a review hold, which exists so a person looks. | Group **task_chain**: after the grace it resets a failed task once, within the retry rules the Automaton already has. A review hold is only reported. | Reading the verifier verdict; every review hold; a task that fails again. |
| `BACKLOG` | Failed or blocked Automata with work left, or Automata waiting for review, approval or planning. | Partly: it can reset the failed ones the same way as a failed task and start the next step of a chain. | Group **task_chain**: the same single reset, and starting the next chain step once the one before it is done. Review, approval and planning waits are reported only. | Approving, planning and deciding what to repair or cancel. A draft whose planning failure is not one the daemon can reword is reported and left for you (see *Planning repair* below). |
| `IDLE lanes` (`idle_lane_eligible_work`) | A lane is idle while work that is eligible for it waits. | Yes: it is exactly what lane fill does. | Group **lane_fill**: on the lane fill's own rules and its own mode, it starts the next eligible Automaton. An Automaton lane fill refuses (a hold, a stale spec) is never started. | Anything lane fill refuses; the lane count and caps. |
| `SILENT` (`worktree_silent`) | A running Automaton has a task in progress, and nothing in its worktree or transcript changed for longer than `silent_minutes`. | No: a quiet worker may be loading a model or thinking. | Reports it. It never signals or ends the session. | Checking the worker's process and files, and deciding to reset it. |
| `LEASE` | A capacity lease holds a slot with no node lease, or with an unknown node after adoption. | Yes for a lease whose holder is gone; the repair only corrects the lease record. | Group **lane_fill**: after the grace it re-adopts or releases the lease record. It never touches the session. | A lease whose holder is still running. |
| `WORKTREE` (`worktree_no_gomod`, `worktree_not_registered`, `verify_script_not_executable`) | A running Automaton's project directory has no `go.mod`, is not a registered worktree, or its verify script is not executable. | Partly: pruning stale worktree registrations is metadata only; restoring tracked files is only safe when nothing in the worktree is uncommitted. | Group **leftovers**: after the grace it prunes stale registrations and, in a clean worktree, restores tracked files or the execute bit. A worktree with uncommitted work is left alone. | Any worktree holding work; deleting a worktree. |
| `TMUX` (`tmux_restart`) | The terminal-session service restarted, so sessions may have died. | No: only a person can tell which Automata lost work. | Reports it. | Auditing every Automaton that ran there. |
| `PLACEMENT` | Compute nodes sit idle while requests wait for a busy node. | Yes: it only re-asks the placement of a waiting request. | Group **lane_fill**: after the grace it re-runs placement for the waiting request. Nodes set to be skipped are ignored. | A request that cannot be placed; node limits. |
| `NODE` | A node holds more sessions than its limit, so balancing is broken. | No. | Reports it. | Finding why the limit was passed. |
| `MODEL` (`model_not_resident`) | The working model is not loaded on a compute node, or the model host cannot be reached. | No: loading or unloading a model, or changing the model, is never automatic. | Reports it. | Reloading the model or fixing the host. |
| `MAIN` (`main_checkout_dirty`) | The main checkout has modified tracked files, so a ship will abort; or it is commits ahead of origin. | No: those are your changes and your push. | Reports it. | Committing or discarding the files; pushing. |
| `PRD` | The Automaton list could not be read. | Yes: nothing to fix, the next poll reads it again. | The next poll retries; no finding is made from a read that failed. | A list that stays unreadable. |
| `DAEMON` (`daemon_inactive`) | The Automaton daemon is not active on a registered compute node. | No: restarting a service is never automatic. | Reports it. | Starting the service. |
| `CI STUCK` (`ci_stuck`) | A CI run has been in progress abnormally long, so the release pipeline is stalled. | No. | Reports it. | Cancelling or re-running the run. |
| `CI RED` (`ci_red`) | A CI run failed. | No: a red run needs a person, and security workflows are never touched. | Reports it. | Reading the failure and the fix. |
| `archived_unmerged` | An archived Automaton's branch still holds commits that are not on the base branch — its work never landed after the record was archived. One finding per such archived record. (The supervisor script has no line for it; the finding is watch-only.) | No: starting a new draft from an archived record, or merging, is an operator decision. | Reports it. | Carrying the work forward into a new draft on that branch (the remedy the finding names), or merging it first; the branch can also be handled from the **unlanded work** list (keep, archive-branch, delete-worktree). |
| `priority_stall` | A queued Automaton at or above the watch's priority floor has been waiting past the stall threshold. | No: starting it is an operator decision. | Reports it. | Looking at it, starting it, or changing its priority or the threshold. |
| `backlog` | The queue of Automata that need attention — the waiting ones plus the failed or blocked ones that still have work left — in one finding, highest priority first. | No. | Reports it. | Working the list in the order it names. |

A check that cannot tell says nothing. Every repair acts only on a finding that
is still open at the end of its grace, and a finding that closes by itself in
the meantime is never repaired.

## Planning repair

The `planning` group (default `report`) repairs a draft Automaton that sits
with zero stories because planning failed for a reason retrying will not change
(the recorded planning failure is not retryable; see
[automaton-decompose-lint.md](automaton-decompose-lint.md#6a-planning-failures-what-you-see-what-the-daemon-fixes-what-it-asks)).

- **When every finding can be reworded**, after the grace it does what the
  planning question's *Fix the wording and plan again* answer does: rewrites
  the spec with the spec lint's suggestions, applies it as a revision and plans
  again. It re-checks that the draft is still stuck and still correctable just
  before acting, and refuses if not.
- **When a finding cannot be reworded** (a private address, a worktree path),
  the finding is an unsafe finding: it is recorded as skipped with one warning
  alert for that draft and the spec is left alone. A person changes it.
- A retryable failure is never touched; it keeps its retry budget.
- It never touches, kills or signals a session, merges, ships, or changes a
  model or backend. Undo: edit the spec back from the Automaton's detail view
  and plan again.

## The six settings groups

The repairable kinds sit in six groups. Each group has one setting and one mode,
and the repairs of a group act only in `on`. The settings live under
`autonomous.housekeeping_repairs`, beside `grace_seconds`:

| Group | Setting | Kinds it covers |
|---|---|---|
| Task chain | `autonomous.housekeeping_repairs.task_chain` | `NEW failed task`, `BACKLOG` |
| Advance and judge | `autonomous.housekeeping_repairs.advance_judge` | `STUCK`, `MERGE WAIT`, `JUDGE BLOCKED` |
| Lane fill | `autonomous.housekeeping_repairs.lane_fill` | `IDLE lanes`, `LEASE`, `PLACEMENT` |
| Leftovers | `autonomous.housekeeping_repairs.leftovers` | `WORKTREE`, and the leftover lease, worktree and tmux name of a finished Automaton |
| Planning | `autonomous.housekeeping_repairs.planning` | A draft with no stories whose planning failed for a non-retryable reason (the planning part of `BACKLOG`) |
| Unarchive (one-time) | `autonomous.housekeeping_repairs.unarchive` | An archived Automaton whose branch still has unmerged commits: put it back to its pre-archive status, once |

| Mode | What it does |
|---|---|
| `off` | The group does nothing and records nothing. The watch findings are still recorded. |
| `report` (default) | The group records what it would repair and when, and acts on nothing. An unset or empty value reads `report`. |
| `on` | The group repairs, once the grace has passed, and records each repair. |

Any other value is refused with a message naming the key and the three allowed
values. `autonomous.housekeeping_repairs.grace_seconds` is a whole number of
seconds, 0 or more, default 300; a negative or non-whole value is refused. In the
config file the block is:

```yaml
autonomous:
  housekeeping_repairs:
    task_chain: report
    advance_judge: report
    lane_fill: report
    leftovers: report
    planning: report
    unarchive: report
    grace_seconds: 300
```

Runtime change through `config set` on the CLI, `PUT /api/config`, `config_set`
on MCP, `configure` on a comm channel and Settings in the web UI works today:
each path validates the value the same way the config file is validated, and
the change takes effect on the next pass. The config file is the durable home
for the same block.

## What the daemon never does

Whatever the mode of any group, a repair never:

- **merges** an Automaton or **ships** a release;
- **deletes a session**, and never kills or signals a session;
- **changes a model or a backend**, and never loads or unloads a model;
- **touches a security workflow** or its result;
- **changes a held Automaton or any of its tasks.** An Automaton an operator put
  on hold (see [Put an Automaton on hold](automaton-hold.md)) is skipped by every
  repair, by the supervisor loop, the stuck-running and verifier watchdogs, boot
  reconcile, infrastructure retries, merge retries and the completed migration;
  only an operator release or extend changes it.

Kinds that are not in a group (`MERGE READY`, `SILENT`, `TMUX`, `NODE`, `MODEL`,
`MAIN`, `DAEMON`, `CI STUCK`, `CI RED`, `priority_stall`, `backlog` and
`archived_unmerged`) are reported in every mode.

## What a skipped unsafe finding means

A `skipped_unsafe` entry is a finding the pass saw and chose not to repair,
because repairing it could lose or overwrite work. Nothing was changed. The
entry carries the finding text and the reason, and one warning alert is raised
for that subject until it stops being found; the alert is not repeated each
pass. It is the same in `report` and `on`: turning a group to `on` never makes
an unsafe finding repairable.

How to act on one, by its reason:

- **`N of M tasks are not finished, so advancing could lose work`** (advance).
  The Automaton is running but some tasks have not finished. Open it from the
  Automata page and look at those tasks: if they are still running or queued,
  wait; if they are stuck, reset or cancel them yourself. Once every task is
  finished, the next read reports it as stuck but finished, and the pass can
  advance it.
- **`N of M tasks are not finished, so a repair could lose work`** (judge
  repair). Same remedy: finish, reset or cancel the open tasks first.
- **`the repair task was already made once for this review`**. Finish or cancel
  that repair task; the pass will not make a second one.
- **`the review recorded no findings`**. There is nothing real to repair from.
  Read the rules review verdict and decide by hand whether to run the review
  again or override it.

For any other group, the reason on the entry names what made it unsafe; fix
that condition by hand, and the finding clears on a later read.

## Reading what a repair did

Every repair, and every report-mode "would repair", writes one record and one
decision-log line: what was found, what was done and how to undo it. The record
keeps the last 200 entries and survives a restart. Its kinds are `would`
(report mode, nothing changed), `did` (repaired), `failed` (the repair returned
an error) and `skipped_unsafe` (judged unsafe, one alert raised). The undo hint
for each shipped repair:

- `task_chain`: pause the Automation to stop the started step, or cancel that
  step's Automaton.
- `advance_judge`: cancel the task, or hold it again by resolving its review with
  the hold answer; for an advance, pause or cancel the Automaton; for the judge
  repair, cancel the repair task; the merge report changed nothing.
- `lane_fill`: set the lane count back with the lane count control; the bounds
  are `supervisor_loop.lane_count.min` and `.max`. For the lane fill start, pause or
  cancel the started Automaton from the Automata page.
- `leftovers`: the disk cleanup sweep has nothing to undo; it only refreshes
  status and raises the disk alert. For the release repairs, start the Automaton
  again from the Automata page to take a lease again or recreate a worktree; a
  released tmux name is simply free to reuse.
- `planning`: the spec change is on the Automaton's decision log; edit the spec
  back from the Automaton's detail view and plan again. No session was touched.
- `unarchive`: set the Automaton back to its previous archived status from the
  detail view; the `unarchive_unmerged` decision on the log says what was moved
  and back.

## Unarchive repair (one-time)

The `unarchive` group (default `report`) is a one-time repair for one specific
wrong outcome: an Automaton that the daemon archived while its branch still
carried unmerged commits. The daemon must confirm the branch is fully merged
before an archive (it checks with git, and when it cannot tell it keeps the
record instead of archiving it), but an archive that slipped past that check
leaves the work unlanded. This pass puts the Automaton back.

- **What it finds**, after the grace: an Automaton that is archived now and
  whose branch still has commits its base does not have (the same merge check
  the archive uses), with the commit count.
- **What it does (in `on`)**, for each one, re-checked just before acting:
  moves the Automaton back to the status it had before the archive — completed,
  or cancelled if it had been cancelled before the archive, read from the
  decision log. Only the status changes. It appends one decision (kind
  `unarchive_unmerged`, actor `housekeeping-repair`) — the old status, the new
  one, and the commit count — and raises one alert.
- **What it never does**: an Automaton the **operator archived or cancelled** stays as the
  operator left it; one that is **not archived anymore** is skipped and
  reported; one whose branch state **cannot be checked** (no worktree, no repo,
  no upstream base) is left archived — when in doubt, do nothing. It never
  touches, kills or signals a session, never merges or ships, and never changes
  a model or backend.
- **It runs once**: after the pass it records that it has run and never runs it
  again. In `report` it only shows which Automata it would unarchive, with the
  same commit counts; in `off` it does nothing at all.

Every repair and every report-mode "would repair" also writes a record, which
is the durable trail: the read view shows the last 200 entries, and the
decision log keeps the full history. The finding itself keeps its first-seen
time and is read through the supervisor watch on any surface, as described in
[automation-supervisor-watch.md](automation-supervisor-watch.md).

## Refusals, finished-work findings and dormant features

The same read (`housekeeping-repairs`) also returns two lists beside the repair
groups: `refusals`, the refusal ledger (one entry per Automaton and step that was
refused, with the latest reason, first and last seen and a repeat count, newest
first, at most 500), and `finished_work`, the finished-work findings (newest
first, at most 200). Both are empty lists, never null. The stores are in place
and read on REST, MCP, CLI and chat, but nothing records into them yet, so
expect empty lists today.

To see which shipped settings are off or in shadow mode and whether each was
ever exercised, use `datawatch autonomous dormant-features [--json]` (REST
`GET /api/autonomous/dormant-features`, MCP `autonomous_dormant_features`, chat
`autonomous dormant-features`). It only lists; change a setting with config set.
