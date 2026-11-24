---
docs:
  index: true
  topics: [autonomous, automation, supervisor, supervisor watch, shadow mode, alert, findings, triage, comparison, health check]
exec_steps:
  - tool: supervisor_watch_list
    description: Read the supervisor watch - the mode in effect, the last poll, the stored findings (open first) and the comparison against the supervisor script's record
    args: {}
    read_only: true
  - tool: supervisor_watch_stats
    description: Read the supervisor watch numbers - mode, seconds since the last poll, open, would-alert and alerted finding counts, days of full agreement
    args: {}
    read_only: true
  - tool: autonomous_config_get
    description: Read the autonomous settings, including the autonomous.automation.supervisor_watch block
    args: {}
    read_only: true
---

# Supervisor watch

The **supervisor watch** is the daemon's standing health check over the Automata
and the machines that run them. On every coordinator poll, with **no model
call**, it runs eighteen checks — one per finding kind, the
eighteen (18) finding kinds this howto lists. Fourteen of them are checks the
supervisor script (`scripts/supervisor-watch.py`) runs by hand today; four
(`worktree_not_registered`, `verify_script_not_executable`,
`main_checkout_automaton` and
`archived_unmerged`) are new in the
watch, and the script has several checks of its own the watch does not have yet
— see "Where the watch and the script differ" below. Each thing it finds is kept
as one **finding** with a stable kind, shown on every surface, and — once the
watch is switched to `on` — raised through the alert and notify channels you
already have. A finding about one Automaton is also written on that Automaton's
triage record the first time it is seen.

It ships in **shadow mode**. Shadow reads and records everything, marks what it
*would* have alerted, and never raises an alert and never acts. You run it
beside the script, compare the two, and only then switch it to `on` and retire
the script.

The watch is **report-only in every mode**. It never merges, never starts,
holds, retries or cancels an Automaton, never restarts a service and never
reloads a model. It reads, it records, it tells you.

## Base requirements

- A running daemon with the autonomous engine enabled and the Automation
  coordinator running — the watch is a pass on the coordinator's poll. There is
  no separate service.
- Nothing else to switch on: an unset mode **reads shadow**, so the watch runs
  in shadow on a daemon that has never been configured for it.
- For the comparison: the supervisor script run with `--record <path>`, and that
  path set as `autonomous.automation.supervisor_watch.script_output_path`.
- For the dirty-checkout finding: `main_checkout` set to your main checkout.

## What the watch reports

The finding kinds are a fixed vocabulary of the eighteen (18) kinds in the
table below; the names there are exactly what you see in the `kind` field on
every surface. The **subject** is the stable id the
finding is about. A kind plus a subject is one finding, so the same problem on
the same subject is one record that is refreshed, not a new one every poll.

| Kind | In plain words | Subject |
|---|---|---|
| `tmux_restart` | The terminal-session service on this machine restarted (its restart count or its active-since time differs from the starting point in `tmux_restart_baseline`). Every Automaton that was running in a session there needs an audit. With no starting point set there is nothing to compare against and this check reports nothing. | The service name |
| `daemon_inactive` | The Automaton daemon is not active on a registered compute node. | The node name |
| `model_not_resident` | The working model is not loaded on a registered compute node, so routing more Automata there would stall. Context variants of the model's base name count as loaded. Not checked while no working model is set. | The node name |
| `task_failed` | A task in an Automaton failed. Read the verifier verdict before retrying. | The Automaton id |
| `task_review_needed` | A task in a running or blocked Automaton is held for a person to review (for example after it used up its attempts). It is read from the tasks themselves, not from the triage record, because the record of an Automaton held for review names no task. | The Automaton id |
| `automaton_not_merged` | An Automaton finished all of its work, every task completed, and is waiting to be merged. | The Automaton id |
| `archived_unmerged` | An archived Automaton's branch still holds commits that are not on the base branch — its work never landed after the record was archived. One finding per such archived record; the line names the title, the id, the branch and how many commits are ahead, and says to carry the work forward into a new draft on that branch. The check is bounded and read-only: at most eight branch checks per pass and each result is cached for a couple of minutes, so a large archive does not turn every pass into git work. A branch the check cannot verify does not open a finding on its own, but it does not clear an open one either — the finding stays until the branch is merged, is deleted, or the check can tell. | The Automaton id |
| `idle_lane_eligible_work` | A lane is idle while Automata work that is eligible to run on it is waiting. This reads the lane fill's own status; the watch applies no eligibility rules of its own. | The lane name |
| `worktree_no_gomod` | A running Automaton's worktree has no `go.mod`, so its project directory is broken. Only raised when the Automaton's project folder is a datawatch worktree (see [Worktree check scope](#worktree-check-scope)). | The Automaton id |
| `worktree_not_registered` | A running Automaton's project directory is not one of the registered worktrees of its repository. | The Automaton id |
| `worktree_silent` | A running Automaton has a task in progress or being verified, but nothing in its worktree and no worker transcript has changed for longer than the silence window (`silent_minutes`, default 60). | The Automaton id |
| `verify_script_not_executable` | A running Automaton's verify script exists inside its worktree but has no execute permission, so its check cannot run by itself. Run the check manually. | The Automaton id |
| `main_checkout_dirty` | The main checkout has modified tracked files, so any Automaton ship will abort until they are committed. Untracked files do not count. Not checked while `main_checkout` is empty. | The main checkout path |
| `main_checkout_automaton` | An open Automaton (a non-terminal status) has a project directory that is the daemon's own main checkout. Approve and run refuse it there, so it cannot run until it is recreated. Recreate the Automaton without a project directory and it runs isolated in its own worktree. An Automaton with no project directory never raises it, and neither does one that is completed, failed, rejected, cancelled or archived. | The Automaton id |
| `ci_red` | A CI run is red. This is read from the repo CI watch's records — see [automation-repo-ci-watch.md](automation-repo-ci-watch.md) — and nothing new is fetched from the CI service. | The CI run id |
| `ci_stuck` | A CI run has been in progress abnormally long (the repo CI watch has marked it hung). The Automata release pipeline is stalled until it is done. | The CI run id |
| `priority_stall` | A high-priority Automaton has been waiting in the queue for more than the stall threshold without a lane picking it up. The line names the Automaton, its priority, how long it has been waiting, the one blocker (`lane_full`, `plan_coverage_blocked`, `held`, `stale`, `proposal_not_confirmed`, `judge_timed_out`, `merge_pending` or `other_refusal`) and the one action that clears it. Only Automata at or above the `priority_floor` are named, only once the age is past the `stall_threshold_minutes`. The watch also wakes the supervisor for it; see [Priority stall wakes the supervisor](#priority-stall-wakes-the-supervisor). | The Automaton id |
| `backlog` | The queue of Automata that need attention, in one finding: the Automata still waiting to be picked up and the failed or blocked Automata that still have work left, ordered highest priority first and, within a priority, oldest first; it names up to sixteen Automata and then the `+k more` tail. This is one finding about the queue, not one per Automaton. | The fixed subject `backlog` |

**When a check cannot tell, it says nothing.** A probe that fails, a node that
cannot be reached, a tool that is missing or a path that cannot be read gives
*unknown*, and unknown never becomes a finding. A finding means the watch saw
the problem, not that it failed to look.

**A finding closes by itself.** On every poll, a finding whose check no longer
reports it is marked resolved. If the same problem comes back, the same record
is re-opened in place and keeps its first-seen time.

## Worktree check scope

The finding `worktree_no_gomod` ("has no go.mod") applies only when an
Automaton's project folder is a datawatch worktree. An Automaton whose project
folder is another repository, for example a non-Go project, never raises it,
because that repository is not expected to carry a `go.mod`. The supervisor
watch script and the daemon-side watch follow the same rule, so they agree on
which Automata are checked.

If the finding does appear, the Automaton's project folder really is a datawatch
worktree that lost its `go.mod`. Check that the worktree is intact (the folder
still holds the repository checkout and was not emptied or partly removed), then
repair or recreate the worktree and re-run the Automaton. If the Automaton was
meant to work on a different repository, correct its project folder instead.

**Operator review:** findings for a `worktree_no_gomod` raised before this change on an Automaton whose project folder is another repository were false. Review and dismiss any such open finding; no repair is needed.

## The record states

Every surface words a finding's standing the same way:

| State | Meaning |
|---|---|
| `open` | The check currently reports it, and it has not been open long enough to alert. |
| `would alert` | Shadow mode only: the finding has been open for the alert time and shadow marked it alert-due **without raising an alert**. This is the line you compare. |
| `alerted` | `on` mode only: the alert for this episode was raised. |
| `resolved` | The check stopped reporting it. |

## Per-band wait stats

The queue findings are per-Automaton, which is right for triage but hard to
read as a trend. The read surfaces therefore also carry **per-band wait
stats**: the waiting Automata grouped into three fixed bands by priority,

| Band | Priority range |
|---|---|
| `high` | 80 or more |
| `medium` | 50 through 79 |
| `low` | below 50 |

Each band row carries the band's name and range, how many of its Automata are
in one of the waiting statuses (`waiting`), the band's oldest wait in whole
minutes (`oldest_waiting_minutes`) and how many of the band's waiters also
satisfy the watch's priority-stall rule (`stalled`) — the same rule a
`priority_stall` finding names one-by-one. The bands are fixed; they cannot be
reconfigured. Bands appear in `high`, `medium`, `low` order, and a band that
waits nothing is omitted, so an empty list means no Automaton is waiting in
any band. The stats are derived from the same waiting set the findings use, in
the same pass; they change nothing and raise no alert.

## Shadow mode first — and how to compare it with the script

**Shadow** is where the watch starts. On every poll it runs all the checks,
records the findings, resolves the ones that cleared and writes the triage-record
note for Automaton-scoped findings. When a finding has been open for
`alert_minutes`, shadow stamps it `would alert` and **raises nothing**. No alert
goes to the alert history or to any chat channel, and nothing is acted on.

**The one thing shadow writes besides its own records.** The triage-record note
for an Automaton-scoped finding is written in shadow too, not only in `on`. It is
a line of text added to a record the daemon already keeps for that Automaton; it
changes no state, starts, holds or retries nothing, and raises no alert. It is
what lets you see in shadow what the watch would have told the operator on the
Automaton's own record. Set `mode` to `off` if even that note is unwanted.

**The comparison record.** If `script_output_path` is set, every pass also reads
the file the script wrote with `--record` and compares the two sets of findings,
kind and subject together:

- what the watch raised that the script did not (**only the watch**),
- what the script raised that the watch did not (**only the script**),
- and whether the two **agreed** — both lists empty.

The daemon keeps the last 500 comparisons and, for each calendar day (local
time), how many ran and how many disagreed. A day counts toward **days of full
agreement** only when it had at least one comparison and every one of them
agreed. The read shows the last comparison, the one-sided differences and that
count.

**What does not count as a disagreement.** If the script's record file is
missing, cannot be read, or is **stale**, the pass records *no comparison at
all*. A stale record is one older than three times `poll_seconds` — three
minutes at the default. So run the script with `--record` more often than that,
or raise `poll_seconds` to match how often you run it. A script that has
stopped running therefore shows up as a comparison history that stops growing,
never as the watch and the script "disagreeing".

**The agreement to reach before retiring the script.** Retire
`scripts/supervisor-watch.py` only after the daemon-side watch has agreed with it
for **one full day**:

1. Set `script_output_path` and run the script with `--record <path>` on its
   usual schedule, more often than three poll intervals.
2. Leave the watch in `shadow` for a whole calendar day.
3. At the end of the day, read the watch (any surface below). You want
   **days of full agreement of at least 1**, a **last poll that is recent**
   (the watch really polled all day), and the last comparison showing **agreed**.
4. If a real finding appeared during the day — an Automaton failed, a lane sat
   idle, CI went red — look at it too. A day that included a real finding and
   still agreed is stronger evidence than a quiet one.
5. If the two differed, the difference **is** the finding. Read the **only the
   watch** and **only the script** entries, decide which side was wrong, fix that
   side, and start the day again.

When the day agrees, switch to `on`. Keep the script around for one more week of
spot checks if you like; the watch no longer needs it.

## Where the watch and the script differ

The comparison is kind-and-subject against what the script recorded, so these
differences are expected and are not bugs. Know them before you read the first
day's result.

- **Four watch-only kinds.** `worktree_not_registered`,
  `verify_script_not_executable`, `main_checkout_automaton` and
  `archived_unmerged` are watch-only: the script never prints a line for any
  of them, so the comparison cannot match them. Whenever one is open, the
  comparison lists it under **only the watch**, and that day does not count
  toward the days of full agreement. Treat each as a real finding to look at
  (or fix) rather than as a disagreement to chase, and expect a quiet day to be
  the only kind that can agree fully.
- **Script checks the watch does not cover.** The script also prints: a model
  host that is unreachable, a node holding sessions against its limit
  (balancing), a capacity lease held without a reason, idle compute nodes while
  requests wait for a busy one (placement), and the main checkout being
  commits ahead of origin. None of these has a finding kind in the watch. The
  script leaves them out of its `--record` file (they are outside the stable
  vocabulary), so they never show up as **only the script** and never block
  agreement — but while the script is your only source for them, keep reading
  the script's own output until the watch grows those checks.
- **Different notions of "new".** The script prints a failed or review task only
  the first time it sees it; the watch keeps the finding open as long as the
  task stays in that state.

## Reading it on every surface

The watch is readable from all five operator surfaces, and all of them read the
same stored state: the mode, the last poll, the findings (open first, then most
recently seen first) and the comparison. None of them changes anything or raises
an alert.

| Surface | Read the state | Notes |
|---|---|---|
| REST | `GET /api/autonomous/supervisor-watch` | Body: `mode`, `last_poll_at`, `findings`, `comparison`, `priority_bands` (see Per-band wait stats). Takes no parameters; any other method is a 405; 503 while the autonomous engine is disabled. A federated peer needs the autonomous read capability. |
| MCP | `supervisor_watch_list` (read-only) | The same body as the GET route. Takes no arguments; any argument is refused rather than ignored. |
| CLI | `datawatch autonomous supervisor-watch` | The mode, the last poll, one line per finding (kind, state, last seen, the operator line), one line per band that waits something, and the comparison summary; `--json` prints the body untouched. |
| Comm channel | `autonomous supervisor-watch` (alias `supervisor_watch`) | The same lines as the CLI, in the same order. The read takes no arguments. |
| PWA | Automation view → Inbox sub-tab → the **Supervisor watch** card | The mode, how long ago the coordinator last polled, up to twenty open findings with their kind and line (a finding shadow marked alert-due says so), one line per band that waits something, and the comparison line. Refreshes every 30 seconds. The card is hidden when the endpoint is unavailable, so it never shows a stale state. |

### Monitoring

The numbers behind the watch ride on the existing monitoring surfaces too, so
you can graph or alert on them without reading the whole finding list:

| Where | What |
|---|---|
| REST | `GET /api/autonomous/supervisor-watch/stats` — `mode`, `last_poll_at`, `last_poll_age_seconds` (-1 when it never polled), `findings_open`, `findings_would_alert`, `findings_alerted`, `findings_resolved`, `agreed_days`, `priority_bands` (the same band rows) |
| MCP | `supervisor_watch_stats` (read-only, no arguments) — the same object |
| Web UI | Settings → Monitor → the **Supervisor watch** card (mode, open findings, would alert, alerted, last poll age, days of full agreement, and one row per band that waits something), live over the stats broadcast |
| Prometheus | `GET /metrics` → `datawatch_supervisor_watch_findings{state="open"|"would_alert"|"alerted"}`, `datawatch_supervisor_watch_agreed_days`, `datawatch_supervisor_watch_last_poll_age_seconds` |
| Comm channel | `autonomous supervisor-watch` already answers the status question from chat; there is no separate stats verb |

The alerts the watch raises in `on` mode are ordinary system alerts. They reach
your chat channels through the existing alert fan-out, not through the read verb.

## The alert

In `on` mode, a finding that has been open for longer than `alert_minutes`
(default **30**) raises **exactly one warning alert per episode**. The title
names the kind and the subject; the body carries the finding's one-line text,
the mode, how many minutes it has been open and when it was first seen. The time
the alert was raised is stamped on the finding (`alerted_at`), which is what
keeps it to one per episode. If the finding resolves and later returns, that is a
new episode and may alert again.

`alert_minutes: 0` turns the alert off in every mode. The findings are still
recorded.

### How to set the alert time

The alert time is one key, `autonomous.automation.supervisor_watch.alert_minutes`,
and it is settable at runtime with no restart:

- REST: `PUT /api/config` with `{"autonomous.automation.supervisor_watch.alert_minutes": 15}`
- CLI: `datawatch config set autonomous.automation.supervisor_watch.alert_minutes 15`
- Comm channel: `configure autonomous.automation.supervisor_watch.alert_minutes=15`
- PWA: Settings → General → the **Supervisor watch alert** field

Unset reads 30 — the same default the Automaton triage alert uses. A negative
or fractional value is refused and nothing is written.

## The triage record

When a finding about one Automaton is seen for the first time (`task_failed`,
`task_review_needed`, `automaton_not_merged`, `worktree_no_gomod`,
`worktree_not_registered`, `worktree_silent`, `verify_script_not_executable`,
`main_checkout_automaton`),
the pass writes the finding's line on that Automaton's triage record as a note
from the `supervisor_watch` source. An Automaton that is failed or blocked and
has no triage record yet gets a minimal one carrying the note. A running
Automaton has no triage record by convention — its finding record is its
record — so nothing is written for it. Findings about the machines, the lanes,
CI and the main checkout belong to no Automaton and write no triage note.

## Priority stall wakes the supervisor

A `priority_stall` finding is no longer only a line on the watch. On each pass
the daemon now reacts to the stall itself: for every Automaton it names it
publishes one `priority_stall` lifecycle event, and the event-driven supervisor
wake set delivers that event to the supervisor session. The supervisor is woken
to clear the blocker instead of finding it on its next scheduled check.

- **One event per Automaton per blocker.** The daemon remembers the blocker it
  last woke for each Automaton. It publishes again only when the blocker changes,
  or after the stall has cleared and returns. An unchanged stall does not repeat.
- **Only the stalled set.** The same rules as the finding apply: at or above the
  `priority_floor`, past the `stall_threshold_minutes`, waiting, and not on hold.
  The detector reads only. It never approves, runs, acknowledges or merges.
- **The event names the one action.** Its text is the blocker and its one
  action, for example "every lane is busy: wait for a lane or free one", the
  same text the finding carries.
- **Off switch.** `autonomous.automation.supervisor_watch.stall_wake` turns the
  event off (default on). The finding on the watch is still recorded.
- **Needs the pass to run.** The event is published from the watch pass, so `mode`
  `off` stops it: no pass runs and no event is published. Unlike the rest of the
  watch, the wake is **not** held back by `shadow`: in `shadow` the event is still
  published and can wake the supervisor, while the findings stay report-only.
  Operators who expect `shadow` to act on nothing must set `stall_wake` to `false`
  or `mode` to `off`.
- **Needs the supervisor wake.** The event reaches a supervisor session only when
  the supervisor event wake is on (`autonomous.supervisor_loop.wake.enabled`,
  off by default). `priority_stall` is in the default wake kinds. With the
  supervisor wake off, no session is woken and the scheduled check covers the
  stall.
- **Default.** `stall_wake` is `true`, the threshold is 30 minutes and the
  priority floor is 80. Confirm these suit your installation before turning the
  supervisor wake on.

### The blockers and their single action

Each stalled Automaton is named with exactly one blocker and one action.

| Blocker | Meaning | The one action |
|---|---|---|
| `lane_full` | Every lane is busy. | Wait for a lane or free one. |
| `plan_coverage_blocked` | The plan coverage check failed and nobody has acknowledged it. | Acknowledge the coverage failure or re-decompose the Automaton. |
| `held` | The Automaton is on hold. | Release the hold. |
| `stale` | The Automaton has not been touched for too long. | Refresh the Automaton or re-decompose it. |
| `proposal_not_confirmed` | Its proposal is not confirmed. | Confirm the proposal. |
| `judge_timed_out` | The judge timed out on every backend. | Re-run the judge. |
| `merge_pending` | Every task is done and the branch is waiting for a merge. | Merge the branch. |

Any other lane-fill refusal is reported as `other_refusal`, with the refusal's
own text and the action "clear the reason named in the text".

A change of blocker for the same Automaton is a new wake; the same blocker is
never woken twice.

### Reading the current stalls

The stalls can be read at any time, whatever `stall_wake` says:

- REST: `GET /api/autonomous/priority-stalls`, body `stalls`: each with
  `automaton_id`, `priority`, `waiting_minutes`, `blocker_code`, `blocker_text`
  and `action`, high priority first. An empty list means nothing is stalled.
- MCP: `priority_stalls` (read-only, no arguments).
- CLI: `datawatch autonomous priority-stalls [--json]`.

The read is toward Automata only and read only: it never wakes the supervisor
and never approves, runs, acknowledges or merges an Automaton. The action is
yours or the supervisor's to take. Run `datawatch autonomous priority-stalls`
first to see what is stalled and why.

## Configuration

| Key | Default | Meaning |
|---|---|---|
| `autonomous.automation.supervisor_watch.mode` | `shadow` (unset reads `shadow`) | `off` (the pass does nothing and records nothing new), `shadow` (everything runs and is recorded, would-alert is marked, nothing is raised and nothing is acted on) or `on` (the alert is raised, once per episode). Case-insensitive; any other value is refused. |
| `autonomous.automation.supervisor_watch.alert_minutes` | `30` | How long a finding may stay open before the one alert for that episode. `0` turns the alert off; negative is refused. |
| `autonomous.automation.supervisor_watch.silent_minutes` | `60` | How long an Automaton with a task in progress may show no activity before `worktree_silent`. `0` is legal; negative is refused. |
| `autonomous.automation.supervisor_watch.stall_threshold_minutes` | `30` | How long a queued Automaton at or above the priority floor may wait before a `priority_stall` finding names it. The age must be strictly past the threshold, so `0` is legal (named as soon as the waiting age is positive); negative is refused. |
| `autonomous.automation.supervisor_watch.priority_floor` | `80` | The priority at or above which a queued Automaton is high enough for a `priority_stall` finding. Below the floor a queue is a `backlog`, not a stall. `0` is legal (every queued Automaton is above it); negative is refused. |
| `autonomous.automation.supervisor_watch.stall_wake` | `true` | Whether a `priority_stall` also publishes a lifecycle event that can wake the supervisor. `false` keeps the finding and stops the event. Read on every pass. See [Priority stall wakes the supervisor](#priority-stall-wakes-the-supervisor). |
| `autonomous.automation.supervisor_watch.poll_seconds` | `60` (unset or `0` reads `60`) | The poll interval the script record's freshness is judged against: a record older than three times this is "unavailable", not a disagreement. The coordinator runs the pass at most once a minute. Below 60 is refused; nothing is ever rounded up for you. |
| `autonomous.automation.supervisor_watch.script_output_path` | empty | The file the supervisor script writes with `--record`. Empty means no comparison is made. |
| `autonomous.automation.supervisor_watch.tmux_restart_baseline` | empty | The starting point (restart count and active-since time of the terminal-session service) that `tmux_restart` compares against. Empty means no starting point is configured and that check reports nothing. |
| `autonomous.automation.supervisor_watch.main_checkout` | empty | The main checkout to inspect for `main_checkout_dirty`. Empty turns that check off. |
| `autonomous.automation.supervisor_watch.working_model` | empty | The model every registered compute node should keep loaded; `model_not_resident` looks for it. Empty means no working model is set and that check reports nothing. |

Every key is read live: a change takes effect on the next pass with no restart. A bad
value is refused on every write path — an unknown mode, a negative minute
threshold, a `poll_seconds` below 60, a non-text path or model name — and nothing
is written. Until `working_model` is set, the `model_not_resident` check stays off
and the other seventeen are unaffected.

## The stuck-running message

When an Automaton sits in `running` with every task finished, the stuck-running
watchdog (see [Stuck-running watchdog](autonomous-recovery.md#stuck-running-watchdog))
acts after `autonomous.stuck_running_seconds` (default `900`). If cancelled
tasks are the only thing in the way, the watch reads one line that names them:

> stuck running: every task terminal for 15m0s, blocked by 2 cancelled tasks:
> Task A, Task B; cancelled tasks are skipped, the Automaton advances to its
> completion review after the grace period, and a cancelled task can be removed
> while the Automaton is running

It means a cancelled task is a deliberate outcome, not a fault. Nothing needs
fixing: the Automaton goes on to its completion review by itself. To clear it
sooner, remove the cancelled task (any surface, while the Automaton is running),
or reset it. An Automaton whose tasks are all cancelled is never advanced; see
[Cancelled tasks at completion](autonomous-recovery.md#cancelled-tasks-at-completion).

## What the watch never does

It never calls a model. It never merges, never starts, holds, cancels or retries
an Automaton, never restarts the terminal-session service or the daemon, and
never loads or unloads a model. It never writes to the repository, the CI
service or a worktree. It never alerts in `shadow`, and never alerts twice in the
same episode. It never turns *unknown* into a finding, and it never counts a
missing or stale script record as a disagreement.

## Operations

**Turning it off.** Set `mode` to `off`. The pass stops and no new alert or note
is produced; the findings and the comparison history already stored stay readable
on every surface.

**Restarting the daemon.** The findings are stored in
`<data-dir>/autonomous/supervisor_watch.jsonl` and the comparison history, with
its per-day summary, in `<data-dir>/autonomous/supervisor_watch_comparisons.json`.
Both are reloaded at startup, so the watch resumes with its full history, and a
finding that already alerted does not alert again after the restart.

**Restarting the daemon and priority stalls.** The wake memory (which blocker was
last woken for each Automaton) is kept in memory only. After a restart the first
pass that finds a stall publishes its `priority_stall` event again, even if the
same blocker was already woken before the restart. This is one extra wake per
stalled Automaton, not a repeat every pass.

**Audit.** The durable trace is the finding itself (first and last seen, the
would-alert mark, the alert time), the triage-record notes and the comparison
history. Read it through any surface above.
