---
docs:
  index: true
  topics: [lanes, lane utilization, utilization, busy, idle, queue wait, backend mix, lane fill, plan usage, points, tokens, gantt, dashboard]
exec_steps:
  - tool: autonomous_lane_utilization
    description: Read the view with no arguments. It covers the last 24 hours with 24 time buckets and the 20 newest lane fill decisions. Each lane carries its busy and idle share, the runs that filled it, the queue wait and the idle-while-eligible flag
    args: {}
    read_only: true
  - tool: autonomous_lane_utilization
    description: Ask for another window in one call. window is a duration such as 1h, 6h, 24h or 7d, from 1 hour to 30 days. bucket (1 to 288) sets how many time slices each lane is cut into, and decisions (1 to 20) sets how many lane fill decisions come back
    args: {window: 7d, bucket: 14, decisions: 10}
    read_only: true
  - tool: autonomous_lane_fill_status
    description: When a lane is flagged idle while eligible work existed, read the live lane fill status to see the lane's blocker, the queued eligible counts and the idle alerts
    args: {limit: 20}
    read_only: true
  - tool: autonomous_config_set
    description: Worked example, a state-changing call. Switch the lane fill mode from shadow to on so the daemon starts the work it has decided on. The view reads the mode back on its next load
    args:
      lane_fill_mode: "{{params.mode}}"
    read_only: false
---

# Lane utilization

**Lane utilization** is a read-only view of how the execution lanes were used
over a window that ends now. It answers three questions that the live
[Lane fill](automation-lane-fill.md) card cannot: how busy was each lane, how
long did work wait for a lane, and did any lane sit idle while work it could
have taken was queued.

It is a report. It never starts, stops or moves anything. Nothing in it is
stored: every figure is worked out from the Automata records and the lane fill
state each time you ask.

## Base requirements

- A running daemon with `autonomous.enabled: true`. With it off, the view is
  refused.
- Some history. A window with no Automaton runs shows every lane fully idle.
- Lanes as set up for [Lane fill](automation-lane-fill.md): GPU lanes, Claude
  lanes, and one extra lane for each other backend that ran work.

## What the view shows

| Part | What it tells you |
|---|---|
| Lanes | One entry per lane: its type (`gpu`, `claude` or another backend), busy time, idle time, busy share and idle share. |
| Runs | The task runs that filled the lane, each with its Automaton title, stage, kind, backend and start and end time. A run with no end is still running. |
| Buckets | The window cut into equal slices, with the busy share of each slice per lane. This is what the stacked graph draws. |
| Queue wait | For work that started on the lane inside the window: how many started, and the mean and longest wait between entering the queue and starting. |
| Idle while eligible | A flag on a lane that is idle now while work it could take is queued, with how long that has lasted and the first item it could have taken. |
| Backend mix | Each worker backend's share of all busy time in the window. |
| Lane fill | The lane fill mode, the mode for each lane kind, and the newest decisions. |
| Plan usage | The plan's weekly usage in points and tokens for the window, against the plan. |

## Reading busy and idle share

A lane's **busy share** is the part of the window in which it had a task
running. Its **idle share** is the rest, so the two add up to the whole window
(100%). A lane that ran work for 6 hours of a 24 hour window has a busy share
of 25% and an idle share of 75%.

Where several runs overlap on one lane they are counted once, so a lane is
never more than 100% busy.

How to read the number depends on the lane:

- A **GPU lane** costs no plan usage, so a low busy share is spare capacity you
  are not using. That is the one to look at first.
- A **Claude lane** spends plan usage while it is busy. A high busy share there
  is the plan being used; read it together with the plan usage line.
- A lane with a low busy share and no flag was simply not needed: nothing it
  could take was waiting.

## Queue wait

Queue wait is the time between work entering the queue and starting on a lane.
It is reported per lane as a count, a mean and a maximum, and only for work
that started inside the window. A lane with a count of `0` shows no wait.

A long mean wait on one lane type with a low busy share on the other usually
means work is waiting for a lane type it is not allowed to use. Check the
Automaton's lane preference in [Lane fill](automation-lane-fill.md#setting-a-lane-preference).

## The idle-while-eligible flag

A lane is flagged when it is idle **now** and work it could take is queued. The
flag carries how long that has been true and the first item the lane could have
taken.

This is the same condition that raises the idle-lane alert after
`autonomous.lane_fill.idle_alert_minutes`. The flag appears as soon as the
condition holds; the alert waits for the configured time.

A flagged lane is not always a fault. In the default shadow mode the daemon
records what it would start but starts nothing, so a flagged lane is expected.
In **on** mode a flag that lasts means something is stopping the start; read the
lane's blocker with the lane fill status (second step of the exec steps above).

## Backend mix

The backend mix is each backend's share of all busy time in the window, for
example `claude-code 70%, pi-prompt 30%`. Each lane also shows its own mix.
Use it to see whether work is going where you planned: GPU work on the GPU
backend, and Claude work on the Claude backend.

## Lane fill mode and decisions

The view carries the lane fill mode (`off`, `shadow` or `on`), the mode in
effect for each lane kind, and the newest decisions, newest first, up to 20.
Each decision has its action, its reason, whether it was executed and any
error. A decision with `executed: false` is a would-start recorded in shadow
mode; it is not something that happened. The meaning of the modes, and how to
switch from shadow to on, is in [Lane fill](automation-lane-fill.md).

## Plan usage: points and tokens, never dollars

The plan usage line shows the points used out of the points the plan holds, the
percent of the plan, and the tokens spent in the window.

**Dollars are never shown.** A bundled plan is a flat rate: what you pay does
not change with the work you run, so a per-request dollar figure would be an
invented number. Points are the plan's own unit of allowance, and tokens are
what was actually spent. No field in the response carries a currency amount.
For a per-backend and per-Automaton cost breakdown, which labels a flat-plan
figure as API-equivalent and not billed, see the [Cost report](cost-report.md).

With no usage source wired the line reads zero points against no plan.

## Choosing a window

| Where | Choices |
|---|---|
| REST, MCP, CLI, comm channel | Any duration from 1 hour to 30 days: `1h`, `6h`, `24h`, `7d`, `30d`, and so on. A value outside that range or not a duration is refused. The default is 24 hours. |
| Dashboard card | 1 hour, 6 hours, 24 hours (default) or 7 days. |

A short window shows what is happening now; a long one shows the pattern. Over
7 days or more, raise `bucket` so each slice stays readable, up to 288.

## Reaching it

| Surface | How |
|---|---|
| REST | `GET /api/autonomous/lane-utilization` with optional `window` (a duration such as `6h`, `24h` or `7d`), `bucket` (1 to 288) and `decisions` (1 to 20). A bad value answers 400. |
| MCP | `autonomous_lane_utilization` with the same optional `window`, `bucket` and `decisions`. Read-only; the JSON comes back unchanged. |
| CLI | `datawatch autonomous lane-utilization [--window 24h] [--bucket N] [--decisions N] [--json]`. Text shows one line per lane with a 20-cell busy bar; `--json` prints the REST body untouched. |
| Comm channel | `autonomous lane-utilization [window]`: a short reply of at most 15 lines, one per lane with its busy percent and an idle-with-eligible-work flag, then the lane fill mode, the newest decision and plan usage. |
| Dashboard card | The **Lane utilization** card on the dashboard: a window picker, a stacked graph of busy share per lane, a table per lane, the backend mix, the lane fill mode with its newest decisions and the plan usage line. It reloads on its own every 30 seconds. |
| Gantt view | The dashboard Gantt chart gains a **Lane utilization** layer: one row per lane with a bar for each run, drawn from the same data as the card. A lane with no runs in the Gantt window still gets a row, so an idle lane is visible. |

An example from the CLI:

```bash
datawatch autonomous lane-utilization --window 7d --bucket 14
```

The CLI and comm channel check the window and the numbers before they send
anything.

## Setting it up and what to look at first

There is nothing of its own to configure: the view reads the lanes and the lane
fill state that [Lane fill](automation-lane-fill.md) already keeps. To change
what it shows, change the lane counts, the lane fill mode or the Automata lane
preferences there.

A good first look: open the 24 hour window, find any GPU lane with a low busy
share, then check whether it is flagged. A low share without a flag means there
was nothing for it to take. A flag means work was waiting and the lane did not
take it.

## Related

- [Lane fill](automation-lane-fill.md): the lanes, the modes, the preferences and the idle alert.
- [Cost report](cost-report.md): spend per Automaton, backend and week.
- [Automata resources](automata-resources.md): the resource cards on the Automata view.
