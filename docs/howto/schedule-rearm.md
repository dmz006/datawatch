---
docs:
  index: true
  topics: [schedules, scheduled command, schedule cli, rate limit resume, scheduled spawn, cron, re-arm, retry, backoff, session cap, capacity, alert, restart, supervisor, detector, session name, waiting_session]
exec_steps:
  - tool: schedule_list
    description: Read every pending and failed schedule with its next fire, last result and re-arm state
    args: {}
    read_only: true
  - tool: get_config
    description: Read the settings, including the schedule block that holds the re-arm policy
    args: {}
    read_only: true
---

# Scheduled spawns that keep running

A **scheduled spawn** starts a fresh session at a time or on a cron, for example the
supervisor's detector pass every few minutes. This page says what the daemon does when
a scheduled start does not work, and how to read and change that behaviour.

## What survives a restart

Schedules are saved to `schedule.json` in the data directory on every change, so a
daemon restart loses none of them. That covers the entries, the next fire time, the
fire counters, the last result and the re-arm state. A schedule whose fire time passed
while the daemon was down fires once when it comes back, not once per missed slot, and
then moves on to its next scheduled time.

## A refused start is a wait, not a failure

Some refusals only mean "no room right now":

- the session cap is full (`max sessions (N) reached`)
- the global rate-limit cooldown is active
- the daemon is shutting down
- a capacity wait timed out

For these the schedule is **never marked failed**, whatever the settings below say, and
a recurring schedule is never left behind. It stays scheduled and tries again after a
pause that starts at `schedule.rearm_backoff_seconds` (30 s) and doubles each time up
to `schedule.rearm_backoff_max_seconds` (15 min). A recurring schedule is never pushed
past its next scheduled time. A one-time schedule that is refused for capacity waits
the same way, but only for `schedule.one_time_max_wait_hours` (12 hours) from its first
refusal. If it has not started by then it **fails with the reason** (`gave_up`, with the
text "gave up waiting for capacity after 12 hours" and the last refusal) and you get an
**error** alert. Recurring schedules keep waiting without a limit. These waits do not
count as failures and do not use up the retry limit.

After 5 waits in a row you get one **warn** alert (then one per 60 waits), naming the
schedule and the reason, so a cap that stays full does not go unnoticed.

## When a start really fails

Any other failure (a project directory that is refused, a backend that cannot launch)
follows `schedule.rearm_mode`:

| Mode | What happens |
|------|--------------|
| `next_fire` (default) | A recurring schedule tries again at its next scheduled time and never gives up. A one-time schedule is marked failed. This is how schedules behaved before the setting existed. |
| `backoff` | The schedule retries sooner, with the same growing pause as above, up to `schedule.rearm_max_retries` failures in a row (0 means never give up). After that it **gives up**: it stays failed. |
| `stay_failed` | The first real failure marks the schedule failed. |

When the policy gives up you get an **error** alert naming the schedule, the number of
failures and the last reason, and the schedule stays `failed` until you re-create it. In
`next_fire` mode the first failure and every 24th after it also raise a warn alert. One
successful start clears the counters, the re-arm state and the last error.

## Reading the state

Every list shows the same three things for a schedule: the **next fire**, the **last
result** with its reason, and the **re-arm state** (`retrying`, `waiting_capacity` or
`gave_up`, with a count).

| Surface | How |
|---------|-----|
| Web | Monitor tab → **Scheduled Events**: a line under each schedule; Settings → General → **Scheduled Spawns** holds the five settings |
| CLI | `datawatch schedule list`: the `REARM` column beside `FIRES`, `LAST-FIRE`, `LAST-RESULT` |
| Chat | `schedule list` |
| MCP | `schedule_list` (pending and failed entries, with the state line) |
| REST | `GET /api/schedules`: `last_fire_result`, `last_error`, `rearm_state`, `consecutive_failures`, `consecutive_waits`, `fire_history` (the newest 20 fires and attempts) |

`last_fire_result` is `spawned`, `skipped` (the previous run was still going), `failed`
or `deferred` (refused for capacity).

## Settings

| Key | Default | Meaning |
|-----|---------|---------|
| `schedule.rearm_mode` | `next_fire` | `next_fire`, `backoff` or `stay_failed`, as above |
| `schedule.rearm_max_retries` | `0` | Failures in a row after which `backoff` gives up; 0 = never |
| `schedule.rearm_backoff_seconds` | `30` | First pause before a retry; doubles each time. Also used for waits |
| `schedule.rearm_backoff_max_seconds` | `900` | Longest pause; never below the first pause |
| `schedule.one_time_max_wait_hours` | `12` | How long a one-time schedule refused for capacity waits before it fails; at least 1. Recurring schedules never use it |

All five are read at every failed start, so a change applies without a restart. Set them
in `datawatch.yaml`, with `datawatch config set schedule.rearm_mode backoff`, the MCP
`config_set` tool, `PUT /api/config`, the chat `configure` command or the web card.

## Commands sent to a session by name

A schedule can also send a **command** to a running session. How you address it
decides what happens when that session goes away.

| Created with | When the session is killed, deleted or ends |
|--------------|----------------------------------------------|
| a session id only | Kill and delete **cancel** the schedule. A session that ends by itself, or no longer exists, makes datawatch **fail** it after two minutes, a schedule waiting for the next input prompt included. Each one raises an alert. |
| a session name (with or without an id) | The schedule **stays pending** and goes to the next live session with that name. |

A schedule with a name goes to the session it first resolved to while that session
can take input. After that it goes to a **live** session with the name: running or
waiting for input first, then rate limited, and the oldest when several tie.

While no live session has the name, the schedule waits:

- A recurring schedule (cron or interval) **skips** each fire it misses and moves to
  the next one, so a new session does not get a stale command. It still ends at its
  deadline, if it has one.
- A one-time schedule stays due and is sent once a session with the name can take it.
  This is within seconds of the session appearing, even if it is still starting.
- A schedule that fires on the next input prompt waits until the session with the
  name asks for input.
- You get **one** warn alert for each wait. A delivery ends the wait.
- Lists show the state `waiting_session` until the next delivery.

There is no time limit on the wait. Cancel the schedule when you no longer want it.

A scheduled command is **never typed into a session that shows an approval or
confirmation prompt**. It stays pending until the prompt is gone. This also holds
for a session the schedule has moved to.

Schedules that had already failed or been cancelled stay that way. Create them again.

## When a command schedule does not run

datawatch tells you with a **warn** alert whenever it ends a schedule that was not
typed: its session was killed, deleted or ended, its send failed, its output filter
may no longer type, or the schedule it follows did not run. When more than ten end
in one pass you get one alert that lists them. A schedule you cancel yourself raises
no alert; the schedules chained behind it are failed, and those do.

- **One send error does not end a recurring schedule that is addressed by session
  id** while that session can still take input. It moves to its next fire, keeps the
  error in `last_error`, and alerts on the first failure in a row and every 24th.
  A one-time schedule becomes `failed`. A recurring schedule addressed by session
  **name** still becomes `failed` on a send error.
- **A chain follows its parent.** If the parent fails, the chained commands fail at
  once; if it is cancelled or deleted, they fail within 30 seconds. A chained command
  whose parent was typed but which never was itself is failed after two minutes. It
  is never typed late.
- **A schedule that could never run is refused** when you create it: a session id
  with no session, or one that has ended, or `run_after_id` naming a schedule that is
  not pending. A schedule with a session name is accepted and waits for the name.

## The rate-limit resume

When a session hits a rate limit, datawatch queues the command that resumes it, with
`kind: rate_limit_resume`. Other schedules for the same session do not count as that
command. At daemon start, a rate-limited session without one gets one. It is not made
again when the earlier one was cut off while being typed, or was already typed since
the session last changed, so it is never typed twice.

## The CLI and the running daemon

`datawatch schedule add`, `list` and `cancel` (also under `datawatch session
schedule`) call the daemon, so the list shows what will fire. They use
`schedule.json` directly only when the daemon is proven to be down: it does not
answer **and** its lock (`daemon.lock`, beside `schedule.json`) is free. The command
then says that datawatch is not running. Otherwise it stops with an error and writes
nothing: when the daemon holds the lock but does not answer, when the daemon answers
with an error, on a secure (encrypted) install, with `--url`, and on a system without
file locks (Windows). If something else changes `schedule.json` while the daemon
runs, the daemon loads the schedules that were added and writes this to its log. It
never brings back one that was removed.

## One fire of a command is typed once

Two passes of the daemon, and the handler for an input prompt, can see the same due
command. Before typing, one of them **claims** the fire, so it is typed at most once.

- A send that is held by a prompt, or that waits for a session, gives the claim back:
  the schedule stays due.
- Only one scheduled command at a time is typed into a session. A second one that is
  due stays due until a later pass, so two lines never run together.
- A command chained after another (`run_after_id`) is typed right after it, by
  whichever pass typed the first one.
- The claim is saved with the schedule before any key is sent (`firing_since`). If the
  daemon stops in the middle of a send, the command may or may not have arrived. At the
  next start it is **not typed again**: a recurring schedule moves to its next fire
  (`skipped`), any other is marked `failed`. The list shows the reason and you get a
  **warn** alert. Check the session, and create the schedule again if the command did
  not arrive.
- If the claim cannot be saved, nothing is typed and the schedule stays due.

## One path fires due spawns

The manager's own schedule loop and the daemon's scheduler both look for due spawns.
They now share one path with a lock, so a due schedule starts exactly one session and a
refused start is handled the same way whichever loop saw it first.

## See also

- [Supervisor loop](supervisor-loop.md): the detector schedule is one of these spawns
- [Automata capacity](automata-capacity.md): the pools behind a capacity refusal
- [Sessions deep dive](sessions-deep-dive.md): scheduled spawn options and the overlap guard
