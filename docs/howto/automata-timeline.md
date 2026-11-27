---
docs:
  index: true
  topics: [autonomous, timeline, gantt, intervals, projection, projects, header scope, project filter, plan filter]
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
  and status. Work finished by hand (a shipped record, or closed with outcome
  delivered) shows status `completed` and `finished`; see
  [what counts as done](automata-progress.md#what-counts-as-done). Its members
  keep their own recorded status. An Automaton's `end` is set only when it is finished; while it
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

/srv/projects/exporter
  Rework the export pipeline (feature, running) — projected 2026-11-12
    Stage 1: Extract (completed)
      Extract CSV exports          #                              T
      Extract JSON exports         #                              T
    Stage 2: Rebuild (running)
      Rebuild the writer           ##XXXXXXX                      T
      Rebuild the readers          ###########                    T
/srv/projects/gateway
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

### How blocked and waiting time is measured

The intervals are derived from what the run already recorded — the chart
itself adds nothing:

- **A member sitting in place.** When a member's newest state is blocked,
  needs a review, or failed, the member gets an **open** blocked interval
  (no end — it is still stuck) that starts at the member's last update: the
  moment the daemon wrote that state. A member waiting on capacity, on a
  decision, or on approval gets an open **waiting** interval from the same
  last update, labelled with its recorded wait reason.
- **Delivery failures.** Every delivery failure recorded on a member is
  drawn as a **closed** blocked interval — a moment that already ended — at
  its recorded time, carrying that failure's reason. Past failures land on
  the bar, not only what is stuck right now.
- **Operator holds on the Automaton.** When the run holds for the operator,
  the hold decision opens an interval — a review-needed hold opens a
  blocked one, a decision-gate hold opens a waiting one — and the next
  decision that resolves it (your decision, your answer, a resume, or an
  approve) closes it. A hold that is still un-answered shows open, just
  like a stuck member.

The reason on each interval is the recorded state name, wait reason or
failure reason: the bar says *where* the time went, and the record says
*why*.

### The projection

Each Automaton's line ends one of three ways:

- `finished <date>` — it is done; the date is its end.
- `projected <date>` — the finish is paced from its finished members per
  active hour: the run has enough of them to measure a rate and
  extrapolate the rest.
- `no projection — <reason>` — it has not finished and the run cannot be
  paced yet. The reason is shown, not hidden: fewer than two members have
  finished — which also covers an Automaton that has no member start
  recorded at all, because pacing needs a start to count from — or no
  active time recorded, or no throughput measured (and
  `no projection — not measured yet` when no reason is recorded at all).
  A missing projection is never shown as a date.

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

Every filter is optional. For `project`, `automaton`, `stage` and `kind`, an
unknown value comes back as an **empty chart, not an error**, so a typo in one
of those shows up as "no projects", not a failure. An unknown **tag** is
refused with `tag not found` (HTTP 404).

| Filter | Narrows to |
|---|---|
| `project` | the Automata of one project directory |
| `automaton` | one Automaton, by id or title (with it, only that Automaton's chart comes back) |
| `stage` | one stage (story), by id or title |
| `kind` | the Automata of one kind: `feature`, `bug` or `other` |
| `tag` | the Automata carrying that tag, given by its name or its id. If several tags share the name, use the id (the refusal lists them) |

```bash
datawatch autonomous timeline
datawatch autonomous timeline --project /srv/projects/exporter
datawatch autonomous timeline --automaton a1b2c3d4
datawatch autonomous timeline --kind bug
datawatch autonomous timeline --stage "Rebuild the writer"
```

Titles with spaces work on the CLI and the MCP tool; on the comm channel the
value must be a single word, so use the id there.

## In the web UI

Two places draw the same chart object:

- **The Automaton Timeline card.** A dashboard card in the default board
  layout (a full-width system card, present on every board) that stacks
  every project's
  Automata as Gantt charts, grouped by project under a one-line summary of
  what is showing. Above the charts is a filter bar with five dropdowns —
  project, automation, stage, kind, tag — each with an *All* option; the
  project, automation, stage and tag choices are drawn from the data in
  front of you (the kind dropdown is the exception — see the flag under
  [Common pitfalls](#common-pitfalls)). The
  chart is read when the card is first rendered and again on each filter
  change (a read already in flight is awaited, not duplicated); the
  dashboard's ticks in between just repaint the card from the already-loaded
  state, they do not re-read it. When a re-read fails the card keeps its last good
  data and says it is stale; when it never loaded, it says so with the
  error. A project with nothing left to draw after the filters is not
  listed.
- **The Timeline tab.** In an Automaton's detail view, the *Timeline* tab
  draws that one Automaton's full chart — all of its stages — with its
  today and projected-finish markers. While the tab is shown it refreshes
  every five seconds; switching tabs or leaving the detail stops the
  refresh.

What a chart shows in the web UI:

- Each **stage is a band** with its title (and status on the right); each
  **member is a bar** inside its band, placed by its recorded start and end
  across one shared time span per Automaton, with UTC labels on the axis.
- A bar runs from its start to its end; a member still open runs to the
  **today line** (solid) instead. Finished members are green, running
  members take the accent colour.
- **Blocked and waiting spans are hatched overlays** on the member's bar —
  red hatch for blocked, blue for waiting — with the kind and reason in
  the hover tooltip.
- A **dashed line** marks the **projected finish** when there is one.

The project, automation, stage and tag dropdowns carry the same filter
names and values as the REST route and the MCP tool, so a filtered chart
reads the same everywhere. The **kind** dropdown is the one that differs:
it currently offers `automaton` and `automation`, which are not kinds the
chart recognises — the chart's kind is `feature`, `bug` or `other`, the
value the REST, MCP and CLI surfaces take. That mismatch is flagged under
[Common pitfalls](#common-pitfalls).

## The header scope and the project timeline

The funnel **scope button** in the app header (see
[The header scope button](projects-tab.md#the-header-scope-button)) filters the
**project timeline**: the Gantt page of the Projects sub-tab, which draws one
project's plans, their Automata and their tasks. It does not change the chart
that the CLI, the comm channel, REST and MCP draw, and it does not change the
*Automaton Timeline* dashboard card, which keeps its own five filters; the scope
is a display rule of the web app only.

| Scope | What the project timeline shows |
|---|---|
| **All** | The Projects sub-tab shows the cross-project **Overview**, which has no timeline; nothing is filtered |
| A project | That project's timeline: every plan, with its Automata and tasks below. Choosing the project in the scope window and picking it in the page's **Project** picker are the same choice |
| A project and a plan | One row: the plan. Its Automata and tasks open below it as usual. Dependency edges are not drawn, because there is no second plan row to connect to |

A **Filtered to \<scope\>** banner with **Clear** sits above the chart while a
scope is active.

**Paging note for a plan scope.** The timeline route pages the plans of a
project and has no plan filter, so for a plan scope the page picks the plan out
of each page it reads. If the plan is not on the first page and more pages
remain, the page reads the next ones by itself, up to ten extra pages, and stops
as soon as it has the plan; the plan is one row, so there is nothing more to
**Load more**. A reply that arrives after you changed the scope is dropped, so a
slow read for an old scope never paints over the new one. The rows below a plan
are read for that plan only and need no filtering.

The **History** page follows the scope too: with a plan scope it lists only that
plan's finished Automata, and leaves out the daily project snapshots, which have
no plan.

## Where to read it

| Surface | What you get |
|---|---|
| CLI | `datawatch autonomous timeline` draws the text bars (above). `--json` prints the `GET /api/autonomous/timeline` body untouched |
| Comm channels | `timeline [project=…] [automaton=…] [stage=…] [kind=…] [tag=…]` — the short form: the counts once, then per Automaton the members, done and blocked figures and the projection (or why there is none) |
| REST | `GET /api/autonomous/timeline` with the query parameters `project`, `automaton`, `stage`, `kind`, `tag` — the full chart object, for scripts and dashboards |
| MCP | `autonomous_timeline` with the same arguments — the same object the REST route returns |
| Web UI | the *Automaton Timeline* dashboard card (all projects, with its five filters) and the *Timeline* tab of one Automaton's detail view — [In the web UI](#in-the-web-ui) |

Every surface decodes the exact same body, so the CLI bars, the comm summary,
the JSON and the MCP result can never disagree.

**Base requirements:** a running datawatch daemon. The chart is read from the
stored records, so it also answers with `autonomous.enabled` off (the route
answers 503 only when the daemon could not load the Automata store). A federated peer reading it needs the autonomous
read capability.

## Common pitfalls

- **An empty chart after a filter.** The filter value was not recognised —
  an unknown project, Automaton, stage or kind returns an empty chart on
  purpose, not an error. Check the project directory spelling and the
  Automaton id or title. An unknown tag is refused with `tag not found`.
- **The card's kind dropdown does not match the chart's kind.** Picking a
  value in the card's *kind* dropdown (`automaton` or `automation`) leaves
  the card empty: those are not kinds the chart knows. The chart's kind is
  `feature`, `bug` or `other` — the value `GET /api/autonomous/timeline?kind=…`,
  `autonomous_timeline(kind=…)`, `datawatch autonomous timeline --kind …` and
  the comm channel `timeline kind=…` all take. **Flagged for review:** the
  card's kind options should become `feature`, `bug`, `other` (the unused
  locale keys `timeline_card_kind_automaton` / `timeline_card_kind_automation`
  in the five locale bundles are where that fix lands); until then, narrow by
  kind on the other surfaces and use the card's other four filters.
- **A bar ends at `T` although the work is not done.** Open members end at
  today by design; their true end appears when they finish.
- **`no projection` on a running Automaton.** Not enough of its members
  have finished to pace from — or no member start is recorded yet. It is
  not a failure and it is not an estimate of zero.
- **Two bars look the same length.** Bars are day-precision and scaled to
  the whole chart's span, not to each other's project. Read the span line
  before comparing.

## See also

- [Automata progress](automata-progress.md) — how far along, and roughly when the rest is done (counts, rates, completion range)
- [Automata tags](automata-tags.md) — narrow the chart to one tag
- [Commands](../commands.md) — the `timeline` comm channel command and `datawatch autonomous timeline`
