---
docs:
  index: true
  topics: [schedules, scheduled spawn, cron, re-arm, retry, backoff, session cap, capacity, alert, restart, supervisor, detector]
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
past its next scheduled time. A one-time schedule that is refused for capacity keeps
waiting the same way until it starts or you cancel it. These waits do not count as
failures and do not use up the retry limit.

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
| Web | Monitor tab → **Scheduled Events**: a line under each schedule; Settings → General → **Scheduled Spawns** holds the four settings |
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

All four are read at every failed start, so a change applies without a restart. Set them
in `datawatch.yaml`, with `datawatch config set schedule.rearm_mode backoff`, the MCP
`config_set` tool, `PUT /api/config`, the chat `configure` command or the web card.

## One path fires due spawns

The manager's own schedule loop and the daemon's scheduler both look for due spawns.
They now share one path with a lock, so a due schedule starts exactly one session and a
refused start is handled the same way whichever loop saw it first.

## See also

- [Supervisor loop](supervisor-loop.md): the detector schedule is one of these spawns
- [Automata capacity](automata-capacity.md): the pools behind a capacity refusal
- [Sessions deep dive](sessions-deep-dive.md): scheduled spawn options and the overlap guard
