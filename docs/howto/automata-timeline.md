---
docs:
  index: true
  topics: [autonomous, timeline, gantt, intervals, projection, projects]
exec_steps:
  - tool: autonomous_timeline
    description: Read the interval chart of the Automata that moved recently, across all projects
    args: {}
    read_only: true
---

# Automata timeline: the interval chart across all your projects

The timeline is the Gantt-style view of your Automata. It draws one chart
that covers **every project** your daemon runs Automata in — one project at a
time, each with its Automata, each with its stages, and each task (a *member*)
as a bar with its start and end. It shows when each member's work ran, the
time a member spent blocked or waiting (with the reason), and the projected
finish of each Automaton with the reason behind it.

It is read-only. It draws from the Automata records the daemon already holds;
nothing on the chart starts, stops or changes an Automaton, and reading it
stores no snapshot.

## What the chart is

- **Projects** — every project directory that has an Automaton in the chart,
  in order. With no filter the chart spans all of them: that is the
  all-projects view.
- **Automata** — under each project, each with its kind (feature, bug, other)
  and status. An Automaton's `end` is set only when it is finished; while it
  runs, its finish is the **projection** (below).
- **Stages** — the stories of the Automaton, each with its own start, end
  (set only when the stage is done) and status.
- **Members** — the tasks. Each carries a start, an end (set only when the
  member is finished), its status, and its **intervals**: the stretches it
  spent blocked or waiting, each with its reason and an end that is set only
  when the stretch closed.
- **`today`** — the server clock the chart is drawn against, so an open bar
  ends at the same instant on every surface.
- **`counts`** — how many projects, Automata, stages and members the chart
  shows, and how many members are blocked (an open blocked interval, or a
  blocked status).

Times are stamped in UTC. The text bars promise **day precision only** — the
same bar can hide hours of work, and a bar one day long is the shortest you
will see.

## Reading the bars

The CLI and the comm channels draw the chart as text; every bar is scaled to
**the same chart span** (everything drawn — member starts and ends, projected
finishes, plus today — widened to at least one day), so a bar that reaches
the edge of the row is the longest bar in the whole picture. The legend is
on its own line:

```text
#   work — from the member's start to its end, or to today while it is still open
X   blocked time (an interval with the reason it was blocked)
~   waiting time (an interval with the reason it was waiting)
T   the today marker column, on every bar
P   the projected finish line of the Automaton
```

Example (the numbers are an example):

```text
Timeline as of 2026-11-09: 2 projects, 3 Automata, 5 stages, 11 members, 2 blocked
span 2026-10-28 -> 2026-11-14   # work  X blocked  ~ waiting  T today  P projected finish

/home/dmz/projA
  Rework the export pipeline (feature, running) — projected 2026-11-12
    Stage 1: Extract (completed)
      Extract CSV exports          #                              T
      Extract JSON exports         #                              T
    Stage 2: Rebuild (running)
      Rebuild the writer           ##XXXXXXX                      T
      Rebuild the readers          ###########                    T
/home/dmz/projB
  Fix the reconnect stall (bug, blocked) — no projection — not measured yet
    Stage 1: Diagnose (running)
      Reproduce the stall          #                              T
```

- **Work** runs as `#` from the member's start to its end — or to the today
  column while the member is still open, so a running member's bar grows
  toward `T` day by day.
- **Blocked** time is drawn over the work as `X`; **waiting** time as `~`.
  The reason for each interval is on the member's data, not the bar — the
  chart says *where*, the record says *why*.
- **`T`** is the same column on every bar: the server's today. Anything
  left of it has happened; anything right of it is projection.
- **`P`** is the Automaton's projected finish, drawn on every one of its
  members. An Automaton that has finished shows its finished date instead.
- The span line names the two ends the whole picture is scaled to.

### The projection

Each Automaton's line ends one of three ways:

- `finished <date>` — it is done; the date is its end.
- `projected <date>` — the finish is paced from its finished members: the
  run has enough of them to measure a rate and extrapolate the rest.
- `no projection — <reason>` — it has not finished and the run cannot be
  paced yet (not enough finished members to measure a rate), which reads
  `no projection — not measured yet` when no reason is recorded. A missing
  projection is never shown as a date.

Treat the projection as a pacing line, the way you treat the completion range
on the [progress dashboard](automata-progress.md): a measure of the run so
far, not a promise.

### Not the dashboard's 6h Gantt card

The dashboard carries a **6h Gantt** feature card that draws the last six
hours of session activity from hook events. That card is a live, short-window
session view. The timeline here is a different thing: a server-drawn chart of
the Automata themselves, across every project, over whatever span the work
actually took, readable without a browser.

## Narrowing the chart

Every filter is optional; an unknown value comes back as an **empty chart,
not an error**, so a typo in a filter shows up as "no projects", not a
failure.

| Filter | Narrows to |
|---|---|
| `project` | the Automata of one project directory |
| `automaton` | one Automaton, by id or title (with it, only that Automaton's chart comes back) |
| `stage` | one stage (story), by id or title |
| `kind` | the Automata of one kind: `feature`, `bug` or `other` |
| `tag` | the Automata carrying that tag |

```bash
datawatch autonomous timeline
datawatch autonomous timeline --project /home/dmz/projA
datawatch autonomous timeline --automaton a1b2c3d4
datawatch autonomous timeline --kind bug
datawatch autonomous timeline --stage "Rebuild the writer"
```

Titles with spaces work on the CLI and the MCP tool; on the comm channel the
value must be a single word, so use the id there.

## Where to read it

| Surface | What you get |
|---|---|
| CLI | `datawatch autonomous timeline` draws the text bars (above). `--json` prints the `GET /api/autonomous/timeline` body untouched |
| Comm channels | `timeline [project=…] [automaton=…] [stage=…] [kind=…] [tag=…]` — the short form: the counts once, then per Automaton the members, done and blocked figures and the projection (or why there is none) |
| REST | `GET /api/autonomous/timeline` with the query parameters `project`, `automaton`, `stage`, `kind`, `tag` — the full chart object, for scripts and dashboards |
| MCP | `autonomous_timeline` with the same arguments — the same object the REST route returns |
| Web UI | Not yet; the web view of this chart is still to come |

Every surface decodes the exact same body, so the CLI bars, the comm summary,
the JSON and the MCP result can never disagree.

**Base requirements:** datawatch running with autonomous enabled (the route
answers 503 when it is off). A federated peer reading it needs the autonomous
read capability.

## Common pitfalls

- **An empty chart after a filter.** The filter value was not recognised —
  unknown values return an empty chart on purpose, not an error. Check the
  project directory spelling, the Automaton id or title, and the tag.
- **A bar ends at `T` although the work is not done.** Open members end at
  today by design; their true end appears when they finish.
- **`no projection` on a running Automaton.** Not enough of its members have
  finished to pace from. It is not a failure and it is not an estimate of
  zero.
- **Two bars look the same length.** Bars are day-precision and scaled to
  the whole chart's span, not to each other's project. Read the span line
  before comparing.

## See also

- [Automata progress](automata-progress.md) — how far along, and roughly when the rest is done (counts, rates, completion range)
- [Automata tags](automata-tags.md) — narrow the chart to one tag
- [Commands](../commands.md) — the `timeline` comm channel command and `datawatch autonomous timeline`
