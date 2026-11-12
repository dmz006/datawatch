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
records and alerts, and it acts on nothing. The four repair groups, their
settings and the grace periods below describe the behavior the repairs are
being built to have; the settings are not read by the daemon yet, so setting
one changes nothing today. Once a group is available it defaults to `report`,
so a daemon that never sets it behaves as it does now. The grace periods are the
waits before a repair acts; a repair never acts on the poll that first sees a
finding.

**Operator review before relying on this page.** The default grace values (30
minutes, 60 for `WORKTREE`), the single-reset rule for a failed task, and which
kinds sit in which group are the proposed design, not yet confirmed by you.
Check them against the shipped settings before you turn any group to `on`.

Names are the script's printed words. The watch's own `kind` for the same
problem is given where one exists (for example `STUCK` and `MERGE WAIT` have no
kind in the watch yet, and `CI STUCK` is `ci_stuck`).

## The finding table

| Finding | Cause | Safe for the daemon alone? | What the daemon does, and after what grace | What stays for an operator |
|---|---|---|---|---|
| `STUCK` | A running or blocked Automaton has every task done or cancelled, but the completion judge refuses to run while a task is cancelled; or it is blocked as "stuck running". | Yes for the cancelled-task case: the repair only re-runs the judge, it changes no work. | Group **advance and judge**: after the grace (default 30 minutes) it asks the judge to run again. A "stuck running" block is reported, not reset. | Deciding whether the cancelled tasks should be reset or removed; any "stuck running" block. |
| `MERGE WAIT` | Every task is done, but the completion rules review has not produced a verdict. | Yes: starting a review that has not run is a read of the work. | Group **advance and judge**: after the grace (default 30 minutes) it starts the review if none is running. | A review that keeps failing to start. |
| `MERGE READY` (`automaton_not_merged`) | Every task is done, the review passed or warned, and the branch is not yet on the base branch. | No: merging is never automatic from the watch. | Reports it. Nothing else. | The merge and the ship. |
| `JUDGE BLOCKED` | Every task is done but the rules review blocked the Automaton. | Partly: the Automaton's own corrective path may add work, but the watch does not decide a block is wrong. | Group **advance and judge**: reports it and makes sure the corrective-work path has run once. It never overrides the verdict and never merges. | Reading the verdict, and any decision to override or reject it. |
| `NEW failed task` (`task_failed`, `task_review_needed`) | A task failed, or is held for a person to review. | Yes for a first failure after an infrastructure cause; no for a review hold, which exists so a person looks. | Group **task chain**: after the grace (default 30 minutes) it resets a failed task once, within the retry rules the Automaton already has. A review hold is only reported. | Reading the verifier verdict; every review hold; a task that fails again. |
| `BACKLOG` | Failed or blocked Automata with work left, or Automata waiting for review, approval or planning. | Partly: it can reset the failed ones the same way as a failed task and start the next step of a chain. | Group **task chain**: the same single reset, and starting the next chain step once the one before it is done. Review, approval and planning waits are reported only. | Approving, planning and deciding what to repair or cancel. |
| `IDLE lanes` (`idle_lane_eligible_work`) | A lane is idle while work that is eligible for it waits. | Yes: it is exactly what lane fill does. | Group **lane fill**: on the lane fill's own rules and its own mode, it starts the next eligible Automaton. An Automaton lane fill refuses (a hold, a stale spec) is never started. | Anything lane fill refuses; the lane count and caps. |
| `SILENT` (`worktree_silent`) | A running Automaton has a task in progress, and nothing in its worktree or transcript changed for longer than `silent_minutes`. | No: a quiet worker may be loading a model or thinking. | Reports it. It never signals or ends the session. | Checking the worker's process and files, and deciding to reset it. |
| `LEASE` | A capacity lease holds a slot with no node lease, or with an unknown node after adoption. | Yes for a lease whose holder is gone; the repair only corrects the lease record. | Group **lane fill**: after the grace (default 30 minutes) it re-adopts or releases the lease record. It never touches the session. | A lease whose holder is still running. |
| `WORKTREE` (`worktree_no_gomod`, `worktree_not_registered`, `verify_script_not_executable`) | A running Automaton's project directory has no `go.mod`, is not a registered worktree, or its verify script is not executable. | Partly: pruning stale worktree registrations is metadata only; restoring tracked files is only safe when nothing in the worktree is uncommitted. | Group **leftovers**: after the grace (default 60 minutes) it prunes stale registrations and, in a clean worktree, restores tracked files or the execute bit. A worktree with uncommitted work is left alone. | Any worktree holding work; deleting a worktree. |
| `TMUX` (`tmux_restart`) | The terminal-session service restarted, so sessions may have died. | No: only a person can tell which Automata lost work. | Reports it. | Auditing every Automaton that ran there. |
| `PLACEMENT` | Compute nodes sit idle while requests wait for a busy node. | Yes: it only re-asks the placement of a waiting request. | Group **lane fill**: after the grace (default 30 minutes) it re-runs placement for the waiting request. Nodes set to be skipped are ignored. | A request that cannot be placed; node limits. |
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
and the repairs of a group act only in `on`.

| Group | Setting | Kinds it covers |
|---|---|---|
| Task chain | `autonomous.automation.supervisor_watch.repairs.task_chain` | `NEW failed task`, `BACKLOG` |
| Advance and judge | `autonomous.automation.supervisor_watch.repairs.advance_and_judge` | `STUCK`, `MERGE WAIT`, `JUDGE BLOCKED` |
| Lane fill | `autonomous.automation.supervisor_watch.repairs.lane_fill` | `IDLE lanes`, `LEASE`, `PLACEMENT` |
| Leftovers | `autonomous.automation.supervisor_watch.repairs.leftovers` | `WORKTREE` |

| Mode | What it does |
|---|---|
| `off` | The group does nothing and says nothing about repairs. The findings are still recorded. |
| `report` (default) | The group records what it would repair and when, and acts on nothing. This is the default; an unset value reads `report`. |
| `on` | The group repairs, once the grace has passed, and records each repair. |

Any other value is refused. Change a group at runtime, with no restart, through
`config set` on the CLI, `PUT /api/config`, `config_set` on MCP, `configure` on a
comm channel, or Settings in the web UI.

## What the daemon never does

Whatever the mode of any group, a repair never:

- **merges** an Automaton or **ships** a release;
- **deletes a session**, and never kills or signals a session;
- **changes a model or a backend**, and never loads or unloads a model;
- **touches a security workflow** or its result.

Kinds that are not in a group (`MERGE READY`, `SILENT`, `TMUX`, `NODE`, `MODEL`,
`MAIN`, `DAEMON`, `CI STUCK`, `CI RED`) are reported in every mode.

## Reading what a repair did

The finding keeps its first-seen time, and each repair is written on the
Automaton's triage record as a note from the supervisor watch. Read both through
the supervisor watch read on any surface, as described in
[automation-supervisor-watch.md](automation-supervisor-watch.md).
