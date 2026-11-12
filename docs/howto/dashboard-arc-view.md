---
docs:
  index: true
  topics: [dashboard, arc, stages, bugs, features, final stage, maintenance, lanes, plan usage, points, tokens]
exec_params:
  - {name: arc, required: false, description: "Arc (Automation) id to read; omit to read the first arc that has staged work"}
exec_steps:
  - tool: dashboard_arc_get
    description: Read the arc summary (stages in order, maintenance count, blocked stage, shared lanes, plan usage in points and tokens)
    args:
      arc: "{{params.arc}}"
    read_only: true
---

# The arc view on the dashboard

The **arc view** shows one arc as its stages in the order they run, so you can
see at a glance how far along it is and what is holding it up. An arc is an
Automation: a planned run of work that moves through three stages, **bugs**
first, then **features**, then the **final stage**. Maintenance work runs
beside the arc in its own lane and is never a stage.

Next to the stages the view shows how busy the shared lanes are and how much
of your plan you have used, in **points and tokens**. It never shows a billed
dollar figure: the plan is flat, so a dollar amount would say nothing about
what you actually pay. See [Usage is points and tokens](#usage-is-points-and-tokens).

The view only reads. It never starts, pauses, unblocks or reorders anything;
it points you at the views that do.

## What the view shows

| Part | What it tells you |
|---|---|
| **Stages** | `bugs`, `features`, `final`, always in that order. Each reports how many pieces of work it holds (`total`), how many are `done`, `running` and `blocked`, and a `percent` done (one decimal place). A stage with nothing in it reads 0 of 0. |
| **Blocked stage** | When a stage has blocked or failed work, the view names the first such stage and gives a plain-language reason: the blocked work's own reason when it has one, otherwise a sentence saying how many pieces are blocked. When nothing is blocked it says so. |
| **Maintenance queue** | Counts only — total, done, running and blocked. Maintenance work is its own lane: it never appears as a stage and never moves a stage's percent. |
| **Shared lanes** | The Claude lane and the pi lane: work in flight against the cap, plus queued work counted by priority (highest priority first within a lane). |
| **Plan usage** | Points used against the points allowance, with points remaining, and the tokens used in the plan window (in, out and total). |

How work is counted: work that is completed or skipped counts as done; running
counts as running; blocked or failed counts as blocked; anything else (not yet
started) counts only toward the total. Work is placed in a stage by the stage
label an operator gave it (a label naming bugs, features, the final stage or
maintenance); work with no recognisable label is left out of the view.

Every part is read on its own. A part this daemon cannot read — for example no
autonomous mode, no lane data or no usage calculator — reports itself
unavailable with a reason and the rest of the view still renders. The read
never fails the whole summary.

## Which arc you see

With no arc named, the view shows the first arc that has staged work. To see a
different arc, name it by its id: `?arc=<id>` on REST, the `arc` argument on
MCP, `--arc <id>` on the CLI. An id that matches nothing reports that no arc
with that id was found.

The chat verb has no arc argument; it shows the default arc.

## Access methods

The arc view reads the same data on every surface:

| Surface | How to read it |
|---|---|
| **REST API** | `GET /api/dashboard/arc` (optional `?arc=<id>`) |
| **MCP tool** | `dashboard_arc_get` (optional `arc`) |
| **CLI** | `datawatch dashboard arc [--arc <id>] [--json]` |
| **Comm channel** | the `dashboard arc` verb (also spelled `dashboard-arc`) |
| **Web UI (PWA)** | the **Arc** card on the Dashboard view |

There is no setting of its own. The arc view is part of the dashboard summary,
so `dashboard.console.enabled` governs it: set it to false and every surface
reports the summary as off with that reason. See
[Operator console](dashboard-operator-console.md) for the switch.

## REST API

```console
$ curl -s "$DW_BASE/api/dashboard/arc" | jq .
$ curl -s "$DW_BASE/api/dashboard/arc?arc=<automation id>" | jq .
```

```json
{
  "generated_at": "2026-11-11T10:15:00Z",
  "available": true,
  "arc": {
    "available": true,
    "id": "…",
    "name": "Release 9.120",
    "stages": [
      { "stage": "bugs",     "total": 3, "done": 3, "running": 0, "blocked": 0, "percent": 100 },
      { "stage": "features", "total": 5, "done": 2, "running": 1, "blocked": 0, "percent": 40 },
      { "stage": "final",    "total": 1, "done": 0, "running": 0, "blocked": 1, "percent": 0 }
    ],
    "maintenance_queue": { "total": 2, "done": 1, "running": 1, "blocked": 0 },
    "blocked_stage": { "stage": "final", "reason": "Cut the release is blocked: waiting on features" }
  },
  "lanes": {
    "available": true,
    "lanes": [
      { "kind": "claude", "in_flight": 1, "cap": 1 },
      { "kind": "pi",     "in_flight": 2, "cap": 2 }
    ],
    "queued_by_priority": [ { "lane": "claude", "priority": 5, "count": 2 } ]
  },
  "usage": {
    "available": true,
    "points": { "used": 41.0, "allowance": 100.0, "remaining": 59.0, "percent": 41.0 },
    "tokens": { "in": 900000, "out": 300000, "total": 1200000 }
  }
}
```

A part this daemon cannot read comes back as `available: false` with a
`reason`, for example

```json
{ "available": false, "reason": "no arc with staged work was found", "stages": [] }
```

Usage has a second reason, `tokens_reason`, because points and tokens come
from different sources: points can be present while the token totals say why
they are missing.

With `dashboard.console.enabled` false the top-level `available` is false and
`reason` says to set the switch to true. The route is read-only and gated on
the `dashboard:read` capability; federated reads from a peer carry the same
gate. It answers GET only.

## MCP tool

`dashboard_arc_get` returns the same body as the REST route. It takes one
optional argument, `arc`, the arc id. It is read-only.

## CLI

```console
$ datawatch dashboard arc
Arc Release 9.120 (2026-11-11T10:15:00Z)
  bugs      3/3 done (100.0%)  0 running  0 blocked
  features  2/5 done (40.0%)  1 running  0 blocked
  final     0/1 done (0.0%)  0 running  1 blocked
  maintenance: 2 total, 1 done, 1 running, 0 blocked
  BLOCKED (final): Cut the release is blocked: waiting on features
  lane claude  1/1 in flight
  lane pi      2/2 in flight
  points: 41.0 of 100.0 used (41.0%), 59.0 remaining
  tokens: 900000 in, 300000 out, 1200000 total

$ datawatch dashboard arc --arc <automation id>   # a specific arc
$ datawatch dashboard arc --json                  # the raw endpoint JSON
```

When the whole summary is off the command prints `Arc summary off:` followed
by the reason.

## Comm channel (chat)

```console
> dashboard arc
dashboard arc: arc Release 9.120 | bugs 3/3 done (100.0%), 0 running, 0 blocked | features 2/5 done (40.0%), 1 running, 0 blocked | final 0/1 done (0.0%), 0 running, 1 blocked | blocked: Cut the release is blocked: waiting on features | claude lane 1/1 | pi lane 2/2 | 41.0 of 100.0 points (41.0%) | 1200000 tokens
```

One summary line: the arc name, each stage, the blocked reason when there is
one, each lane, then points and tokens. When the summary is off the reply is
`arc off (<reason>)`.

## Web UI (PWA)

The arc view is the **Arc** card on the Dashboard view. It is not on the board
by default: open the dashboard, choose **Edit**, then **Add Card**, and pick
**Arc**. Once on the board it stays in your saved layout.

Top to bottom the card shows:

1. The arc name as its title.
2. **Stages** — one row per stage in order (Bugs, Features, Final stage), each
   with "N of M done", the percent, a progress bar and "N running · N blocked".
   A stage with blocked work turns its bar red.
3. **Blocked stage** — the stage and the plain-language reason, or "No stage is
   blocked."
4. **Maintenance queue** — total, done, running and blocked counts.
5. **Shared lanes** — the Claude lane and the pi lane, each with "N of M in
   flight" and a bar, then **Queued by priority** with one line per lane and
   priority.
6. **Plan usage** — points used of the allowance with a bar and the points
   remaining, then the tokens used (total, in and out).

The card refreshes about every 30 seconds while it is on the board, and again
when the board is built. If a refresh fails it keeps the last data on screen.
A part the daemon cannot read shows its reason in place of its data.

Every label is translated into the five interface languages (English, German,
Spanish, French and Japanese). The arc name and the blocked reasons are data
from the daemon and are shown as sent. Numbers are grouped for the active
language. The card uses the theme colours, so it follows light and dark mode,
and it fits a phone-width screen.

## Usage is points and tokens

Your plan is a flat fee with a weekly allowance, so the arc view measures usage
the way the plan does:

- **Points** — a share of the weekly allowance. The allowance is 100 points plus
  any extra you have bought; points used is how much of it is spent so far. The
  view shows used, allowance, remaining and the percent.
- **Tokens** — the input and output tokens used in the plan window, with a
  total.

No dollar figure is shown on any surface, and none is stored in the response.
Cost reports elsewhere in datawatch may show an API-equivalent estimate; that is
not what the plan bills and it is not part of this view. See
[Usage calculator](usage-calculator.md) for how the points and the pace line are
worked out.

## Where to see it

| You want … | Where |
|---|---|
| The arc at a glance, always on screen | the **Arc** card on the PWA Dashboard view |
| The arc in a terminal | `datawatch dashboard arc` (or `--json`) |
| The arc from chat | the `dashboard arc` verb |
| The arc from an agent or script | `dashboard_arc_get` (MCP) or `GET /api/dashboard/arc` |
| The five "what needs my attention" sections | [Operator console](dashboard-operator-console.md) |

## Notes and limits

- The view is a point-in-time read; read it again to refresh.
- Stage order is fixed: `bugs`, `features`, `final`.
- Maintenance work never counts toward a stage or a stage's percent.
- A stage's percent counts completed and skipped work as done.
- One arc is shown at a time; use the arc id to pick a different one.
- Hiding the dashboard summary with `dashboard.console.enabled=false` hides the
  arc view's data on every surface. The `dashboard.hide_inactive_cards` setting
  does not hide the Arc card; it is always offered in the Add-Card picker.

## Related how-tos

- [Operator console](dashboard-operator-console.md) — the dashboard's one-glance summary and its switches
- [Dashboard](dashboard.md) — the Mission Control view the Arc card lives on
- [Usage calculator](usage-calculator.md) — points, the pace line and plan usage
- [Autonomous planning](autonomous-planning.md) — Automata and Automations
