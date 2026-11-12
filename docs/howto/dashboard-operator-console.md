---
docs:
  index: true
  topics: [dashboard, operator console, one-glance summary, attention, arc, stages]
exec_params:
  - {name: enabled, required: false, description: "dashboard.console.enabled to set: true (default) or false"}
exec_steps:
  - tool: dashboard_console_get
    description: Read the operator console one-glance summary (open questions, held/blocked, lanes, usage, CI/release)
    args: {}
    read_only: true
  - tool: dashboard_arc_get
    description: Read the arc summary (stages in order, maintenance count, blocked stage, lanes, plan usage in points and tokens)
    args: {}
    read_only: true
  - tool: config_set
    description: Switch the console or one of its sections off and on again (dashboard.console.enabled, dashboard.console.show_lanes, …)
    args:
      dashboard.console.enabled: "{{params.enabled}}"
    read_only: false
---

# Operator console on the dashboard

The **operator console** is the one-glance "what needs my attention right now"
summary of a datawatch daemon. It rolls five live subsystems into a single
answer — each with a count, one readable summary line and the PWA anchor
where the operator acts on it:

| Section | What it reads | Where you act on it |
|---|---|---|
| `questions_waiting` | Open operator questions (the question channel, held questions and worker-marker questions) | the PWA questions view |
| `held_blocked` | Automata stuck in a failed or blocked triage state, plus uncommitted-work holds | the held questions / held state views |
| `lanes` | Lane fill status and the capacity admission wait queue | the lanes view |
| `usage` | The burn guard state and the usage calculator projection | the usage view |
| `ci_release` | Repo CI watch records and the latest-release check | the releases view |

The console is always readable: a section whose backing subsystem is not
wired into this daemon reports itself unavailable with a reason, instead of
the whole summary failing. It never writes anything — it is a roll-up of
other reads.

Every switch below defaults to **true**: omitting a key means the console
(and each section) is on.

## Access methods

The console can be read, and its switches set, from five surfaces:

| Surface | Read the console | Set the switches |
|---|---|---|
| **YAML** | — (reads come from the daemon) | `datawatch.yaml` → the `dashboard:` block (below) |
| **CLI** | `datawatch dashboard console` (or `--json`) | `datawatch dashboard console set <key> <value>` |
| **Web UI** | the five console cards opening the PWA Dashboard view | Settings → General, or `PUT /api/config` from the settings screen |
| **REST API** | `GET /api/dashboard/console` | `PUT /api/config` with the key (same path as every config key) |
| **Comm channel** | the `dashboard console` verb | `configure dashboard.console.enabled=false` |

All seven keys are named on every one of these surfaces; the MCP tool
`dashboard_console_get` reads the same body as the REST route.

## The seven keys

All under `dashboard.` — every one is a boolean, every one reads **true**
while absent. An explicit `false` sticks through saves and YAML round trips.

| Key | Gates |
|---|---|
| `dashboard.console.enabled` | the console as a whole — false makes every section report itself unavailable, and the PWA drops its console cards |
| `dashboard.console.show_questions` | the `questions_waiting` section |
| `dashboard.console.show_held_blocked` | the `held_blocked` section |
| `dashboard.console.show_lanes` | the `lanes` section |
| `dashboard.console.show_usage` | the `usage` section |
| `dashboard.console.show_ci_release` | the `ci_release` section |
| `dashboard.hide_inactive_cards` | the dashboard board itself — when true (the default), the five feature cards (Integration status, Guardrails, Smoke Run, Memory Scopes, Search Usage) hide from the grid and the Add-Card picker while they have no live data; the console cards and the other system cards (Automata, Network, Live Events, Sessions, Timeline · 6h, **Automaton Timeline**, 30-Day Activity, Multi-EKG) are not gated by it |

## YAML

```yaml
# datawatch.yaml — the whole dashboard block. Omit it and every switch
# reads its default, true.
dashboard:
  hide_inactive_cards: true      # true (default): inactive cards stay off the board
  console:
    enabled: true                # true (default): the console is on
    show_questions: true         # true (default): open-operator-questions section
    show_held_blocked: true      # true (default): held/blocked section
    show_lanes: true             # true (default): lanes section
    show_usage: true             # true (default): usage section
    show_ci_release: true        # true (default): CI & release section
```

Restart (or reload the daemon) so the new config is live.

## CLI

```console
$ datawatch dashboard console        # one line per section, in the endpoint's order
Operator console (2026-11-02T10:15:00Z)
  questions_waiting  2    2 open (1 blocking)
  held_blocked       1    1 Automaton held
  lanes              1    1 lane filling
  usage              0    under the pace line
  ci_release         2    1 failing run on main

$ datawatch dashboard console --json    # the raw endpoint JSON

$ datawatch dashboard console set dashboard.console.enabled false   # the same PUT /api/config the chat channel uses
$ datawatch dashboard console set dashboard.console.show_lanes true
```

## Web UI (PWA)

The five console cards open the Dashboard view when its console is enabled
— **Questions waiting**, **Held & blocked**, **Lanes**, **Usage**, **CI &
release**. Each card jumps to the PWA anchor of the section's data, and its
help link opens this document. When `dashboard.console.enabled` is false the
cards are dropped from the board (a stored layout is rewritten once to add
them back when you switch the console on again). The switches themselves are
ordinary config keys — set them from Settings while you are in the PWA, or
from any read surface above. With `dashboard.hide_inactive_cards` on (the
default), cards that have no live data stay out of the grid and the Add-Card
picker; set it false to keep them visible so you can pin your layout.

The board also carries its own system card besides the five console cards:
the **Automaton Timeline** card (part of the default layout, full board
width), the Gantt chart of your Automata across
every project — each stage a band, each member a bar with its start and end,
blocked and waiting spans hatched on top, a solid today line and a dashed
projected-finish line — with its own five filters (project, automation,
stage, kind, tag). It reads `GET /api/autonomous/timeline`, which is a
different read from the console's five sections: the console is the
roll-up, the Timeline card is the interval chart. It needs the autonomous
feature running for its data, and when the chart has nothing the card stays
in place and shows a one-line empty message — it is not one of the five
feature cards that `dashboard.hide_inactive_cards` (the default) hides. It
is a different thing from
the board's 6h Gantt card, which draws the last six hours of session
activity. See [automata-timeline.md](automata-timeline.md) for how the chart
reads and how its blocked and waiting time is measured.

## REST API

```console
$ curl -s "$DW_BASE/api/dashboard/console" | jq .
```

```json
{
  "generated_at": "2026-11-02T10:15:00Z",
  "questions_waiting": {
    "available": true,
    "count": 2,
    "summary": "2 open (1 blocking)",
    "link": "#questions"
  },
  "held_blocked":  { "available": true, "count": 1, "summary": "1 Automaton held", "link": "#held-questions" },
  "lanes":         { "available": true, "count": 1, "summary": "1 lane filling", "link": "#lanes" },
  "usage":         { "available": true, "count": 0, "summary": "under the pace line", "link": "#usage" },
  "ci_release":    { "available": true, "count": 2, "summary": "1 failing run on main", "link": "#releases" }
}
```

A section you switched off, or a subsystem this daemon does not run, comes
back as

```json
{ "available": false, "reason": "hidden by the dashboard.console.show_lanes switch — set it to true to show this section", "link": "#settings" }
```

The route is read-only and gated on the `dashboard:read` capability
(federated reads from a peer carry the same gate).

## Comm channel (chat)

```console
> dashboard console
dashboard console:
  questions_waiting 2 — 2 open (1 blocking) · held_blocked off (hidden by the dashboard.console.show_held_blocked switch — set it to true to show this section) · lanes 1 — 1 lane filling · usage 0 — under the pace line · ci_release 2 — 1 failing run on main
```

One summary line, in the endpoint's section order; sections you switched off
show as `off (…)` with the key they name. The `dashboard-console` spelling
parses the same verb.

## Arc summary

The **arc summary** shows one arc (an Automation) as its stages in the order
they run: `bugs`, then `features`, then `final`. Each stage reports `total`,
`done`, `running`, `blocked` and a `percent`. Maintenance work is a separate
lane and reports counts only (`maintenance_queue`); it is never a stage. When a
stage is blocked, `blocked_stage` names it with a plain-language reason. The
body also carries the shared lane usage (Claude and pi lanes: in flight, cap,
and queued work by priority) and the plan usage in **points and tokens**.
No dollar figure appears: the plan is flat.

```console
> dashboard arc
dashboard arc: arc Release 9.120 · bugs 3/3 done (100.0%), 0 running, 0 blocked · features 2/5 done (40.0%), 1 running, 0 blocked · final 0/1 done (0.0%), 0 running, 1 blocked · blocked: waiting on features · claude lane 1/1 · 41.0 of 100.0 points (41.0%) · 1200000 tokens
```

| Surface | How |
|---|---|
| REST | `GET /api/dashboard/arc` (`dashboard:read`; optional `?arc=<automation id>`, default is the first arc with staged work) |
| MCP | `dashboard_arc_get` (optional `arc`) |
| CLI | `datawatch dashboard arc [--arc <id>] [--json]` |
| Chat | `dashboard arc` (also `dashboard-arc`) |

Like the console, the read never 5xxs: a part this daemon cannot read reports
`available: false` with a `reason`, and `dashboard.console.enabled=false`
hides the whole summary the same way.

The PWA shows the same data as the opt-in **Arc** card on the Dashboard view.
The full guide — what each part shows, the response body, the web card and how
usage is shown in points and tokens — is
[The arc view on the dashboard](dashboard-arc-view.md).

## Where to see it

| You want … | Where |
|---|---|
| The roll-up itself | the PWA Dashboard view (five console cards), `datawatch dashboard console`, `dashboard_console_get` (MCP), `GET /api/dashboard/console`, or the `dashboard console` chat verb |
| The data behind one section | the section's PWA anchor — `#questions`, `#held-questions`, `#lanes`, `#usage`, `#releases` |
| The arc summary | the **Arc** card on the PWA Dashboard view, `datawatch dashboard arc`, `dashboard_arc_get` (MCP), `GET /api/dashboard/arc`, or the `dashboard arc` chat verb — see [the arc view](dashboard-arc-view.md) |
| The switches | Settings → General (`dashboard.*` keys), YAML, `datawatch config set`, or `PUT /api/config` |
| The Automata interval chart itself | the board's *Automaton Timeline* card — [automata-timeline.md](automata-timeline.md) |

## Notes and limits

- The console is a **read roll-up**: it never answers a question, unblocks a
  hold, drains a lane, or touches usage numbers. It points at the views that do.
- Section order is fixed: `questions_waiting`, `held_blocked`, `lanes`,
  `usage`, `ci_release`.
- Counts are point-in-time reads of the backing subsystems; refresh by
  re-reading.
- `dashboard.hide_inactive_cards` affects the dashboard board, not the
  console: the console's five sections are listed exactly as above, while
  the five feature-gated cards (Integration status, Guardrails, Smoke Run,
  Memory Scopes, Search Usage) are hidden from the grid and the Add-Card
  picker while they have no live data. The other cards — Multi-EKG, 6h
  Gantt, 30-Day Activity, the **Automaton Timeline**, and the rest of the
  default layout — stay in place and show their empty state instead.
