---
docs:
  index: true
  topics: [autonomous, automation, supervisor, supervisor watch, housekeeping, repairs, findings, grace period, task chain, judge, lane fill, leftovers]
exec_steps:
  - tool: supervisor_watch_list
    description: Read the supervisor watch findings, open first - the kinds in this table are the finding kinds you see there
    args: {}
    read_only: true
  - tool: autonomous_config_get
    description: Read the autonomous settings, including the supervisor watch block and the four repair groups
    args: {}
    read_only: true
---

# Automaton housekeeping repairs

The supervisor script (`scripts/supervisor-watch.py`) and the daemon's
[supervisor watch](automation-supervisor-watch.md) both find the same kinds of
trouble in the Automata and the machines that run them. Today an operator fixes
each one by hand, and the [supervisor loop](supervisor-loop.md) keeps the
mechanical steps moving. This page is the single table of **every finding kind**:
what causes it, whether the daemon can fix it safely by itself, what it does and
after how long, and what is always left for you.

**Where the code stands.** The watch is report-only in every mode today: it
records and alerts, and it acts on nothing. The repair framework and its
settings now exist: the four group settings and the grace setting under
`autonomous.housekeeping_repairs` are part of the config and are read and
checked when the daemon loads it, and each group defaults to `report`, so a
daemon that never sets one behaves as it does now. The daemon does not run the
repair pass yet, and the settings are not yet accepted by `config set`,
`PUT /api/config`, `config_set`, `configure` or Settings in the web UI, so the
only way to set one today is the config file, and setting one changes nothing
until the pass is switched on in a later release. The grace is the wait before
a repair acts; a repair never acts on the poll that first sees a finding.

**What the shipped framework repairs.** The framework is narrower than the
table below. The table is the full set of finding kinds and the design for each;
the repairs written so far are these four, one per group, each built on a
mechanical step the [supervisor loop](supervisor-loop.md) already has:

| Group | What the shipped repair finds | What it does in `on` |
|---|---|---|
| `task_chain` | An active Automation whose next chain step is waiting and nothing started it. | Starts the next chain step. |
| `advance_judge` | A task held for review after its infrastructure retries ran out. | Retries the failed infrastructure task. |
| `lane_fill` | The running lane count differs from the wanted count, inside the lane bounds. | Corrects the lane count to the wanted one. |
| `leftovers` | Leftover files crossed the disk cleanup alert threshold. | Runs the cleanup sweep, which only refreshes the cleanup status and raises the disk alert. |

The `task_chain` group also has three repairs for stuck task chains, each run in
`on` and only reported in `report`. They are written and tested but, like the
rest of the pass, the daemon does not start them yet, so none acts today:

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
Automata, run in `on` and only reported in `report`. They are written and tested
but, like the rest of the pass, the daemon does not start them yet, so none acts
today:

- **Advance.** An Automaton that is running with every task finished (done or
  cancelled) is moved to its next state by the normal completion step, once the
  grace has passed. It is left alone while a live run still holds it, and it is
  refused as unsafe while any task is unfinished.
- **Judge repair.** An Automaton blocked by the rules review with no repair task
  yet gets one repair task, made once from the review's real findings (a task
  carrying the findings is added to the last story, then reset with a plain
  reset). The fact is recorded on the Automaton so a second one is never made. If
  someone already made a repair task, it is theirs to finish and is left alone.
- **Merge report.** An Automaton waiting to merge or ready to merge is only
  reported. Nothing here can merge.

The advance and judge repairs refuse, as an unsafe finding (recorded as skipped,
one alert), when a task is not finished, when the judge repair was already made
once, or when the review recorded no findings. Their undo hints: the advance moved
on by the normal completion step, so pause or cancel the Automaton from the
Automata page to stop it; for the judge repair, cancel the repair task from the
Automaton's task list (it is recorded and will not be made again); the merge
report changed nothing.

The review start, lease re-adoption, placement re-run and worktree pruning rows in
the table are not written yet.

**Operator review before relying on this page.** Check these against the
shipped code before you turn any group to `on`:

- The table's "after the grace" waits are the grace setting
  `autonomous.housekeeping_repairs.grace_seconds`, which defaults to **300
  seconds (5 minutes)** and applies to every group; there is no longer or
  per-group wait (for example none for `WORKTREE`). `0` means no grace.
- The table's repair columns (a judge re-run, a single reset of a failed task,
  worktree pruning, lease and placement fixes) are the design; the shipped
  repairs are the four above. Confirm which of the table's rows you still want
  built.
- The three task chain repairs run fixed commands (`make sync-docs`, `git clean -f -- <one path>`) in
  the Automaton worktree. Confirm you accept those commands, and that a final-gate failure
  naming `internal/server/web/docs` as stale is a reliable signal, before turning `task_chain` to `on`.
- The advance repair re-fires the completion step, and the judge repair adds
  one task titled "Fix rules review findings" (actor `housekeeping-repair`) to
  the Automaton's last story. Confirm you accept both, and that "blocked with a
  rules review outcome" is a reliable signal, before turning `advance_judge` to `on`.
- The table's `STUCK` row describes asking the judge to run again, and `MERGE WAIT`
  describes starting a review; the shipped advance repair instead re-fires the
  completion step for a running Automaton whose tasks are all finished, and
  `MERGE WAIT` / `MERGE READY` are reported only. Decide whether the table rows
  should be reworded or built as written.
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
| `BACKLOG` | Failed or blocked Automata with work left, or Automata waiting for review, approval or planning. | Partly: it can reset the failed ones the same way as a failed task and start the next step of a chain. | Group **task_chain**: the same single reset, and starting the next chain step once the one before it is done. Review, approval and planning waits are reported only. | Approving, planning and deciding what to repair or cancel. |
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

A check that cannot tell says nothing. Every repair acts only on a finding that
is still open at the end of its grace, and a finding that closes by itself in
the meantime is never repaired.

## The four settings groups

The repairable kinds sit in four groups. Each group has one setting and one mode,
and the repairs of a group act only in `on`. The settings live under
`autonomous.housekeeping_repairs`, beside `grace_seconds`:

| Group | Setting | Kinds it covers |
|---|---|---|
| Task chain | `autonomous.housekeeping_repairs.task_chain` | `NEW failed task`, `BACKLOG` |
| Advance and judge | `autonomous.housekeeping_repairs.advance_judge` | `STUCK`, `MERGE WAIT`, `JUDGE BLOCKED` |
| Lane fill | `autonomous.housekeeping_repairs.lane_fill` | `IDLE lanes`, `LEASE`, `PLACEMENT` |
| Leftovers | `autonomous.housekeeping_repairs.leftovers` | `WORKTREE` |

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
    grace_seconds: 300
```

Runtime change through `config set` on the CLI, `PUT /api/config`, `config_set`
on MCP, `configure` on a comm channel and Settings in the web UI is not wired
yet; edit the config file for now.

## What the daemon never does

Whatever the mode of any group, a repair never:

- **merges** an Automaton or **ships** a release;
- **deletes a session**, and never kills or signals a session;
- **changes a model or a backend**, and never loads or unloads a model;
- **touches a security workflow** or its result.

Kinds that are not in a group (`MERGE READY`, `SILENT`, `TMUX`, `NODE`, `MODEL`,
`MAIN`, `DAEMON`, `CI STUCK`, `CI RED`) are reported in every mode.

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
  are `supervisor_loop.lane_count.min` and `.max`.
- `leftovers`: nothing to undo; the sweep only refreshes status and raises the
  disk alert.

No surface reads the record yet: it is reachable only through the decision log.
The finding itself keeps its first-seen time and is read through the supervisor
watch on any surface, as described in
[automation-supervisor-watch.md](automation-supervisor-watch.md).
