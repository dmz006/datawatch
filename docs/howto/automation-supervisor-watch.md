---
docs:
  index: true
  topics: [autonomous, automation, supervisor, supervisor watch, shadow mode, alert, findings, triage, comparison, health check]
exec_steps:
  - tool: supervisor_watch_list
    description: Read the supervisor watch - the mode in effect, the last poll, the stored findings (open first) and the comparison against the supervisor script's record
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
call**, it runs the same fourteen checks that the supervisor script
(`scripts/supervisor-watch.py`) runs by hand today. Each thing it finds is kept
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

The finding kinds are a fixed vocabulary; the names below are exactly what you
see in the `kind` field on every surface. The **subject** is the stable id the
finding is about. A kind plus a subject is one finding, so the same problem on
the same subject is one record that is refreshed, not a new one every poll.

| Kind | In plain words | Subject |
|---|---|---|
| `tmux_restart` | The terminal-session service on this machine restarted (its restart count or its active-since time differs from the starting point in `tmux_restart_baseline`). Every Automaton that was running in a session there needs an audit. With no starting point set there is nothing to compare against and this check reports nothing. | The service name |
| `daemon_inactive` | The Automaton daemon is not active on a registered compute node. | The node name |
| `model_not_resident` | The working model is not loaded on a registered compute node, so routing more Automata there would stall. Context variants of the model's base name count as loaded. Not checked while no working model is set. | The node name |
| `task_failed` | A task in an Automaton failed. Read the verifier verdict before retrying. | The Automaton id |
| `task_review_needed` | A task in an Automaton is waiting for a person to review it. | The Automaton id |
| `automaton_not_merged` | An Automaton finished all of its work, every task completed, and is waiting to be merged. | The Automaton id |
| `idle_lane_eligible_work` | A lane is idle while Automata work that is eligible to run on it is waiting. This reads the lane fill's own status; the watch applies no eligibility rules of its own. | The lane name |
| `worktree_no_gomod` | A running Automaton's worktree has no `go.mod`, so its project directory is broken. Only raised for a project that is a Go project. | The Automaton id |
| `worktree_not_registered` | A running Automaton's project directory is not one of the registered worktrees of its repository. | The Automaton id |
| `worktree_silent` | A running Automaton has a task in progress or being verified, but nothing in its worktree and no worker transcript has changed for longer than the silence window (`silent_minutes`, default 60). | The Automaton id |
| `verify_script_not_executable` | A running Automaton's verify script exists inside its worktree but has no execute permission, so its check cannot run by itself. Run the check manually. | The Automaton id |
| `main_checkout_dirty` | The main checkout has modified tracked files, so any Automaton ship will abort until they are committed. Untracked files do not count. Not checked while `main_checkout` is empty. | The main checkout path |
| `ci_red` | A CI run is red. This is read from the repo CI watch's records — see [automation-repo-ci-watch.md](automation-repo-ci-watch.md) — and nothing new is fetched from the CI service. | The CI run id |
| `ci_stuck` | A CI run has been in progress abnormally long (the repo CI watch has marked it hung). The Automata release pipeline is stalled until it is done. | The CI run id |

**When a check cannot tell, it says nothing.** A probe that fails, a node that
cannot be reached, a tool that is missing or a path that cannot be read gives
*unknown*, and unknown never becomes a finding. A finding means the watch saw
the problem, not that it failed to look.

**A finding closes by itself.** On every poll, a finding whose check no longer
reports it is marked resolved. If the same problem comes back, the same record
is re-opened in place and keeps its first-seen time.

## The record states

Every surface words a finding's standing the same way:

| State | Meaning |
|---|---|
| `open` | The check currently reports it, and it has not been open long enough to alert. |
| `would alert` | Shadow mode only: the finding has been open for the alert time and shadow marked it alert-due **without raising an alert**. This is the line you compare. |
| `alerted` | `on` mode only: the alert for this episode was raised. |
| `resolved` | The check stopped reporting it. |

## Shadow mode first — and how to compare it with the script

**Shadow** is where the watch starts. On every poll it runs all the checks,
records the findings, resolves the ones that cleared and writes the triage-record
note for Automaton-scoped findings. When a finding has been open for
`alert_minutes`, shadow stamps it `would alert` and **raises nothing**. No alert
goes to the alert history or to any chat channel, and nothing is acted on.

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

## Reading it on every surface

The watch is readable from all five operator surfaces, and all of them read the
same stored state: the mode, the last poll, the findings (open first, then most
recently seen first) and the comparison. None of them changes anything or raises
an alert.

| Surface | Read the state | Notes |
|---|---|---|
| REST | `GET /api/autonomous/supervisor-watch` | Body: `mode`, `last_poll_at`, `findings`, `comparison`. Takes no parameters; any other method is a 405; 503 while the autonomous engine is disabled. A federated peer needs the autonomous read capability. |
| MCP | `supervisor_watch_list` (read-only) | The same body as the GET route. Takes no arguments; any argument is refused rather than ignored. |
| CLI | `datawatch autonomous supervisor-watch` | The mode, the last poll, one line per finding (kind, state, last seen, the operator line) and the comparison summary; `--json` prints the body untouched. |
| Comm channel | `autonomous supervisor-watch` (alias `supervisor_watch`) | The same lines as the CLI, in the same order. The read takes no arguments. |
| PWA | Automation view → Chains sub-tab → the **Supervisor watch** card | The mode, how long ago the coordinator last polled, up to twenty open findings with their kind and line (a finding shadow marked alert-due says so), and the comparison line. Refreshes every 30 seconds. The card is hidden when the endpoint is unavailable, so it never shows a stale state. |

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
`worktree_not_registered`, `worktree_silent`, `verify_script_not_executable`),
the pass writes the finding's line on that Automaton's triage record as a note
from the `supervisor_watch` source. An Automaton that is failed or blocked and
has no triage record yet gets a minimal one carrying the note. A running
Automaton has no triage record by convention — its finding record is its
record — so nothing is written for it. Findings about the machines, the lanes,
CI and the main checkout belong to no Automaton and write no triage note.

## Configuration

| Key | Default | Meaning |
|---|---|---|
| `autonomous.automation.supervisor_watch.mode` | `shadow` (unset reads `shadow`) | `off` (the pass does nothing and records nothing new), `shadow` (everything runs and is recorded, would-alert is marked, nothing is raised and nothing is acted on) or `on` (the alert is raised, once per episode). Case-insensitive; any other value is refused. |
| `autonomous.automation.supervisor_watch.alert_minutes` | `30` | How long a finding may stay open before the one alert for that episode. `0` turns the alert off; negative is refused. |
| `autonomous.automation.supervisor_watch.silent_minutes` | `60` | How long an Automaton with a task in progress may show no activity before `worktree_silent`. `0` is legal; negative is refused. |
| `autonomous.automation.supervisor_watch.poll_seconds` | `60` (unset or `0` reads `60`) | The poll interval the script record's freshness is judged against: a record older than three times this is "unavailable", not a disagreement. The coordinator runs the pass at most once a minute. Below 60 is refused; nothing is ever rounded up for you. |
| `autonomous.automation.supervisor_watch.script_output_path` | empty | The file the supervisor script writes with `--record`. Empty means no comparison is made. |
| `autonomous.automation.supervisor_watch.tmux_restart_baseline` | empty | The starting point (restart count and active-since time of the terminal-session service) that `tmux_restart` compares against. Empty means no starting point is configured and that check reports nothing. |
| `autonomous.automation.supervisor_watch.main_checkout` | empty | The main checkout to inspect for `main_checkout_dirty`. Empty turns that check off. |
| `autonomous.automation.supervisor_watch.working_model` | empty | The model every registered compute node should keep loaded; `model_not_resident` looks for it. Empty means no working model is set and that check reports nothing. |

Every key is read live: a change takes effect on the next pass with no restart. A bad
value is refused on every write path — an unknown mode, a negative minute
threshold, a `poll_seconds` below 60, a non-text path or model name — and nothing
is written. Until `working_model` is set, the `model_not_resident` check stays off
and the other thirteen are unaffected.

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

**Audit.** The durable trace is the finding itself (first and last seen, the
would-alert mark, the alert time), the triage-record notes and the comparison
history. Read it through any surface above.
