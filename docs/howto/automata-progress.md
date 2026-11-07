---
docs:
  index: true
  topics: [autonomous, progress, estimate, throughput, backlog, snapshots, trend]
exec_steps:
  - tool: autonomous_progress
    description: Read the progress numbers - counts by kind, rates, utilization, work left (known and estimated) and the completion range
    args: {}
    read_only: true
  - tool: autonomous_progress_trend
    description: Read how the completion range moved over the saved daily snapshots (last 30 days)
    args: {days: 30}
    read_only: true
---

# Automata progress: how far along, and when will it be done

The progress dashboard answers two questions about the Automata you run:
**how much is finished**, and **roughly when will the rest be**. It counts
Automata and their tasks by kind (feature, bug, other), measures how fast
work really finishes, keeps the work you know about apart from the work you
can only estimate, and turns that into a **range of days**, never a single
promised date.

It is read-only. Nothing on the card or in the commands starts, stops or
changes an Automaton; the only thing you can set from here is an
Automaton's *kind* (see [Setting an Automaton kind](#setting-an-automaton-kind)).

## What it is

- **Counts by kind.** Every Automaton is a *feature*, a *bug* or *other*.
  Each kind gets a row with the number of Automata, how many are done,
  running, waiting and blocked, the tasks they hold, and how many tasks are
  done and left. A *total* row sums them. The *other* row only shows when it
  has something in it.
- **Rates, per active day.** How many Automata and tasks finish per day of
  **active time** (below), the worker time used per day, and how much of the
  lane capacity that is (utilization).
- **Work left, in two parts.** *Known* work is Automata that exist and the
  tasks they still hold. *Estimated* work is what does not exist yet: backlog
  rows not filed as Automata, and Automata with no tasks yet. The two are
  never blended into one figure without saying so.
- **A completion range.** Four ways of dividing the work left by a measured
  rate, shown as a low to high number of active days, with the sentence of
  assumption behind each and what is widening the range.
- **Lead time.** How long Automata sit in each stage between creation and the
  last finished task. It is shown for information only.
- **Inactive windows.** Every stretch of time left out of the rates, listed,
  never hidden.
- **A trend.** Once a day the numbers are saved; the trend says whether the
  completion range is converging, widening or flat.

## What counts as done

An Automaton counts as **done** when its status is completed. Finished work
that was filed away still counts: a cancelled or archived Automaton whose
tasks all completed is merged work whose record was closed, so it is counted
as done, in the totals, the rates and the estimate sample. A cancelled,
archived or rejected Automaton with unfinished tasks, or with no tasks, was
replaced or abandoned and stays out of every count. Templates are never
counted.

## Totals by project, Automation and Automaton

Every total is available **overall** and split three ways. Pick the split with
the *group* setting:

| Group | One row per | Use it to see |
|---|---|---|
| `project` | project | where the work sits across your projects |
| `automation` | Automation (an arc of work) | how far along each arc is |
| `automaton` | Automaton | the same counts one Automaton at a time |

Each row carries the same figures as the overall totals: Automata, done,
running, waiting, blocked, tasks, tasks done and left, worker hours and the
median and mean times. Records that have no project, or that belong to no
Automation, are shown together in a row named **(none)**, so the rows always
add up to the overall total and nothing is dropped.

Name one row to read just that project or Automaton: `--project NAME` on the
CLI, `project=NAME` on the chat channel, `project=NAME` in the API query, and
`automaton=ID` on the chat channel for one Automaton. Naming a project turns
the project split on by itself, and `(none)` as the name reads the records
with no project. The name filters the rows of the chosen axis: the chat
channel refuses a name that does not fit the axis (a project together with a
different group, or `project=` together with `automaton=`). The CLI and the
API take the combination instead: the API always returns every row of the
tables, and on the CLI the name then merely picks a row in the chosen axis's
table, which a name of another axis does not match (so the table comes back
empty rather than being refused).

### History per project

The daily snapshots (see [Daily snapshots and the trend](#daily-snapshots-and-the-trend))
also keep each project's own counts. Reading the trend with a project returns
that project's points, oldest first: date, reason, Automata, done, running,
waiting, blocked, tasks, tasks done, tasks left and worker hours. That is the
history of one project over time; the overall trend is unchanged.

### Estimates and their sample size

The completion range is a measured rate applied to the work left, so it is
only as good as the finished work behind it. The progress reply states the
**sample**: how many finished Automata and how many completed tasks the rates
are based on, archived finished work included. Every surface shows it as a
line such as "based on 12 Automata and 140 tasks". When fewer than five
Automata have finished, the line adds "too few to rely on" and the range
carries a sparse-history driver; read the range as a rough guide until the
sample grows.

## Reading the numbers

### Active time

Rates are measured per **active** day, not per calendar day. An *inactive
window* is a stretch where nothing was happening, so including it would make
the rates look slower than the workers really are. Two sources feed it:

- **Inferred gaps.** Any gap between two consecutive task starts or
  completions that is at least `autonomous.progress.inactive_gap_hours`
  (default 3 hours) counts as an inactive window.
- **The explicit waits file** (see [The waits file](#the-waits-file)), for
  stretches you know about: a weekend away, a quota reset, a host that was
  down.

Windows that overlap or touch are merged first, so no stretch is subtracted
twice. Active days are the calendar days since the start date minus the
merged windows. Inferred windows are marked *inferred* on the card.

Active time is why a pause you took does not wreck the estimate, and why the
card still shows the pause: the WAITS line gives the number of windows, the
hours left out, their share of calendar time, and **what the finish would be
in calendar days if the same share of idle time recurs**.

### Why lead time never feeds the estimate

Lead time (created to approved, approved to first task, run time, created to
last task done) includes everything an Automaton waited on: your approval, a
full capacity pool, a back-off. Those waits are real, but they are not how fast
the workers go, and they overlap between Automata running side by side. Using
them to predict the finish would count the same waiting time again for each
Automaton. So lead time is **tracked, shown, and kept out of every estimate**.
Look at it to see where Automata spend their life; do not read a finish date
from it.

### Known versus estimated remaining

The LEFT block has three lines:

- **known** - Automata not done (how many are running, how many wait or are
  blocked), the tasks still open in them, and how many have not been
  decomposed into tasks yet.
- **estimated** - backlog rows not filed as Automata (a row marked
  `{phases=3}` counts as three), plus the Automata with no tasks yet, times
  the **mean tasks per Automaton** measured over Automata that do have tasks.
- **combined estimate** - known plus estimated, always labelled an estimate.

If something could not be measured (no backlog file was read, no Automaton
has tasks to average), the card says **not measured**. It never shows a zero in
place of an unknown. A missing backlog file is not an error; the estimated
part just excludes the rows.

### Reading the range and the drivers

The ETA line gives a **low to high** number of active days. The four methods
behind it each divide the combined work left by one measured rate:

| Method | Assumes |
|---|---|
| Automaton rate | Automata finish at the rate of the last three active days. |
| Task rate | Tasks finish at the average rate over all active days so far. |
| Task rate, last 24 h | Tasks finish at the rate of the last 24 hours of active time. |
| Worker hours | Each task takes the average worker time of a finished task, and workers stay as busy as they have been per active day. |

A method with no rate yet reads *not measured* and is left out; the low and
high are the smallest and largest of those that were measured. A wide range
means the methods disagree. The **drivers** line says why:

- *Workers were idle much of the time, so the rate may rise* - utilization is
  under half.
- *A few large Automata pull the average task count up* - the mean task count
  is well above the median.
- *Much of the remaining work is estimated because it has no tasks yet* - more
  than 30% of the work left is estimated.
- *Few Automata have finished, so every rate is thin* - fewer than five done.

Treat the range as a planning aid. It is an estimate; it narrows as more work
finishes and as Automata are decomposed.

### An example

This is what `datawatch autonomous progress` prints, shortened (the numbers are an example):

```text
Progress since 2026-10-10: 12.0 calendar days, 10.2 active days after excluding 3 inactive windows of 43 h; 5 lanes
           Automata  done   run   wait blocked |  tasks   done  left |      work       median    mean/task
feature          31    24     3      3       1 |    142    118    24 |    96.5 h       5.1 h        0.8 h
bug              11     9     1      1       0 |     38     33     5 |    21.0 h       3.2 h        0.6 h
total            42    33     4      4       1 |    180    151    29 |   117.5 h       4.8 h        0.8 h

LEFT  known: 9 Automata not done (4 running, 5 waiting or blocked) with 29 tasks still open; 2 not decomposed yet
      estimated: 6 Automata not filed yet; about 40 tasks (5.0 tasks per Automaton)
      combined estimate: about 15 Automata and about 69 tasks (known plus estimated)
ETA   2.9 to 5.6 active days (estimate; a range, not a promise)
      by Automaton rate: 5.6 days - Assumes Automata finish at the rate of the last three active days.
      ...
      drivers: much of the remaining work is estimated because it has no tasks yet
WAITS 3 inactive windows, 43 h = 15% of calendar time (left out of every rate above, never hidden); ...
UNFILED ROWS (first 6 of 6):
  - ...
```

## Base requirements

- datawatch running with autonomous enabled.
- Some finished Automata. With none, every rate reads *not measured*.
- Optional: a backlog file for the estimated part (default
  `docs/plans/README.md`, a table whose rows start with a number, see
  [Backlog markers](#backlog-markers)).

## Setting an Automaton kind

The kind decides which row an Automaton is counted in and which tasks-per-Automaton
average is used for it. It is resolved in this order:

1. the kind you set on the Automaton;
2. the kind of the backlog row that links the Automaton (`{kind=...}`);
3. a guess from the title and the start of the spec: *bug* when it contains a
   whole word such as fix, regression, crash or leak, else *feature*.

Set it explicitly when the guess is wrong. An empty kind clears your setting
so the guess applies again.

```bash
# CLI: set, and set at creation
datawatch autonomous prd-set-kind a1b2c3d4 bug
datawatch autonomous prd-create "Fix the stall on reconnect" --kind bug

# REST
curl -sk -X POST https://localhost:8443/api/autonomous/prds/a1b2c3d4/set_kind \
  -H 'Content-Type: application/json' -d '{"kind":"bug"}'
```

Other channels:

- **Web UI:** Automaton settings > *Kind* (feature, bug, other, or "Derived from
  title and spec").
- **MCP:** `autonomous_prd_set_kind` (`id`, `kind`), and the `kind` argument of
  `autonomous_prd_create`.
- **Chat channels:** `autonomous set-kind a1b2c3d4 bug`.

An unknown kind is refused (HTTP 400, or an error from the CLI before anything
is sent).

## Backlog markers

The estimated part counts the open rows of the backlog table that no Automaton
is linked to yet. Two optional markers in a row's item cell steer it:

```markdown
| 41 | Rework the export pipeline {kind=feature} {phases=3} | not filed |
| 42 | Fix the reconnect stall {kind=bug} | not filed |
| 43 | Tidy the docs index | `a1b2c3d4` running |
```

- `{kind=feature}`, `{kind=bug}` or `{kind=other}` sets the kind of the row,
  and of the Automata linked to it that have no kind of their own. Without it
  the kind is guessed from the item text.
- `{phases=N}` says the row will become *N* Automata (a whole number, at least
  1). An unfiled row adds *N* to the estimate. A row that already has linked
  Automata adds only the phases beyond the ones linked, so a row is never
  counted twice.

A row is **linked** when its cells hold the Automaton's 8-character id in
backticks. A row is **open** while its status reads as open work (not started,
draft, running, approved, blocked, queued, merge pending, and so on) and is not
done, shipped, superseded or deferred past the next major release. The markers
are removed from the text shown to you.

## The waits file

A file named `operator-wait-windows.tsv`, **in the same folder as the backlog
file**, lists stretches to leave out in addition to the inferred ones. One line
per window, tab separated:

```text
# start<TAB>end<TAB>kind<TAB>reason
2026-10-14T18:00:00Z	2026-10-15T08:00:00Z	operator	away overnight
2026-10-18T00:00:00Z	2026-10-18T06:30:00Z	quota	provider quota reset
```

- Timestamps are ISO 8601; with no zone they are read as UTC. A bare date means
  midnight UTC.
- `kind` is a short label shown on the card; `reason` is optional free text.
- Lines starting with `#`, blank lines, lines with fewer than three fields, a
  bad timestamp, or an end that is not after the start are skipped silently.
- Overlaps with inferred windows (or each other) are merged, not double counted.

A missing file means no explicit windows. The file is read on every request, so
an edit shows up on the next refresh.

## Daily snapshots and the trend

Once a day (UTC) the numbers are saved to `progress-snapshots.jsonl` in the
Automata data folder, first checked about 30 seconds after the daemon starts and re-checked hourly. A
**milestone** snapshot is also saved each time an Automaton completes.
Snapshots older than `autonomous.progress.snapshot_retention_days` (default 90)
are dropped.

`progress trend` reads them back: each point carries the date, the reason
(daily or milestone), the completion range low and high in days, the known and
estimated tasks, the 3-day throughput, utilization and completed count. The
**direction** says how the range width moved from the first point to the last:
*converging* (narrower by more than 10%), *widening* (wider by more than 10%),
*flat* (within 10%), or *unknown* with fewer than two points that have a range. Converging is what you want to see as
the work is decomposed and finished.

## Setup

The defaults need no setup. To tune them:

```yaml
autonomous:
  progress:
    lanes: 5                      # parallel lanes used for utilization (0 = 5)
    inactive_gap_hours: 3         # gap without activity that makes an inactive window (0 = 3)
    backlog_path: docs/plans/README.md   # relative paths resolve against autonomous.worktree_base_repo
    since: "2026-10-01"           # first day counted; empty = the first Automaton's creation date
    snapshot_retention_days: 90   # days snapshots are kept (0 = 90)
```

All five keys apply live; no restart. A bad value is rejected and nothing is
written.

### The five access methods

| What you want | YAML | CLI | Web UI | REST | Chat |
|---|---|---|---|---|---|
| Read the numbers | - | `datawatch autonomous progress [--group project|automation|automaton] [--project NAME] [--automation ID] [--tag ID]... [--tag-match any|all] [--since D] [--until D] [--json]` | Automation sub-tab > *Progress and cost* > *Progress* card, *Group the totals* choice | `GET /api/autonomous/progress[?group=project\|automation\|automaton&project=NAME]` | `progress [group=project|automation|automaton] [project=NAME] [automaton=ID] [automation=ID] [tag=ID] [tag_match=any|all] [since=D] [until=D]` |
| Read the trend | - | `datawatch autonomous progress-trend [--days N] [--group project|automation|automaton] [--project NAME] [--tag ID]... [--tag-match any|all] [--json]` | *Trend* section of the card | `GET /api/autonomous/progress/trend?days=N[&project=NAME][&tag=ID&tag_match=all]` | `progress trend [days] [tag=ID] [tag_match=all]` |
| Set an Automaton kind | - | `datawatch autonomous prd-set-kind ID KIND` | Automaton settings > *Kind* | `POST /api/autonomous/prds/{id}/set_kind` | `autonomous set-kind ID KIND` |
| Change a setting | `autonomous.progress.*` | `datawatch config set autonomous.progress.lanes 5` | Settings > General > Autonomous > *Progress ...* | `PUT /api/config` `{"autonomous.progress.lanes": 5}` | `configure autonomous.progress.lanes=5` |

MCP agents use `autonomous_progress` (argument `group` adds the split and
`project` is the row name) and `autonomous_progress_trend` (`project` returns
that project's points), `autonomous_prd_set_kind`, and `config_set` (or the
`progress_*` arguments of
`autonomous_config_set`). The commands and tools print or return the **same
object** as the REST route, so every surface shows the same figures.

`since` and `until` are dates (`YYYY-MM-DD`); `until` includes the whole day.
`automation` narrows the count to the Automata of one Automation. `tag` is
repeatable and narrows the count to the Automata carrying the given tag(s):
`--tag-match all` (or `tag_match=all`) requires every listed tag to be
present; `any`, the default, takes at least one. Every surface takes tag
IDs. A bad date or a bad `tag_match` is refused (HTTP 400).

The progress object and every trend point report the **scope** the counts
were taken over — `all`, `automation:<id>`, `tag:<id>`, `tags:<id1,id2>`
(any) or `tags(all):<id1,id2>` (all), combined as needed — so a reader can
tell exactly what the numbers cover. The trend reply appends
` (scope …)` when it is narrower than the whole fleet.

## Where to see it

| Where | What you see |
|---|---|
| Web UI, Automation sub-tab | The *Progress* card on the **Progress and cost** sub-tab of the Automation sub-tab (the list itself is on the **Automations** sub-tab): the kind table, work left, completion range with methods and drivers, throughput, utilization, inactive windows, unfiled backlog count and the trend. It loads when the sub-tab is selected and stops refreshing when you leave — nothing keeps polling after that — and it is not shown on the Automata or Templates sub-tabs. It keeps the last good data and says so when a refresh fails or the data is over five minutes old. |
| `datawatch autonomous progress` | The status table as text (`--json` for the raw object); with `--group`, the totals split by project, Automation or Automaton, `(none)` included, and the estimate sample line. |
| `datawatch autonomous progress-trend` | The saved snapshots and the direction; with `--project`, that project's own points; with `--group`, the current totals split by that axis, appended after the points (they come from the live progress route, not the snapshots). |
| Chat channels | `progress` and `progress trend` reply with the same text as the CLI, including `group=`, `project=` and `automaton=`. |
| REST / MCP | The metrics object and the trend object as JSON, for dashboards and scripts. With `group`, the reply adds `group` and `group_totals` (row name to totals, `(none)` for records with no project or Automation); the completion range carries `sample_records` and `sample_tasks`. |
| Android and iOS apps | Not yet; the web UI is the only graphical view today. |

## Common pitfalls

- **Everything reads "not measured".** No Automaton has finished yet, or the
  start date is later than all of them. Lower `autonomous.progress.since` or
  wait for the first completions.
- **The estimated part is zero.** No backlog file was read. Check
  `autonomous.progress.backlog_path`; a relative path resolves against the
  Automata base repository when one is set, else the daemon's working directory.
- **A bug is counted as a feature.** The guess missed it. Set the kind.
- **The range is very wide.** Read the drivers line. Decomposing the Automata
  with no tasks and filing the backlog rows narrows it.
- **A pause made the finish look late.** Add the stretch to the waits file so
  it stays out of the rates.

## See also

- [Automata capacity](automata-capacity.md) - the slots the lanes stand for
- [Automata planning](autonomous-planning.md)
- [Automata tags](automata-tags.md) - narrow the count to one tag
