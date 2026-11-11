---
docs:
  index: true
  topics: [dashboard, sessions, telemetry, network, lane-pulse, guardrail, work map, series]
exec_steps:
  - tool: dashboard_work_map_get
    description: Read the work map (projects, Automations, Automata, tasks and sessions)
    args: {}
    read_only: true
  - tool: dashboard_lane_pulse_get
    description: Read the lane pulse traces and burn panel
    args: {}
    read_only: true
  - tool: dashboard_series_get
    description: Read one dashboard series (lane-utilization, throughput-forecast, verifier-health, release-train, review-backlog, defects-per-day)
    args: {series: release-train}
    read_only: true
---
# How-to: /dashboard Mission Control

The `/dashboard` view gives you a live picture of the work running on a
datawatch instance: a **Network** card that maps projects, Automations,
Automata, tasks and live sessions as one graph, a **Lane pulse** card that
shows when each capacity lane was busy or waiting, and the feature cards
described below. Cards are arranged on a grid you can change, and the
cards read their data from the daemon's REST endpoints, refreshed while the
view is open.

> **Pre-conditions**: The dashboard surfaces live data from multiple subsystems. For the full experience:
> - Sessions and LLM cards require an LLM backend. See [llm-registry.md](llm-registry.md).
> - The Network and Work map cards need live sessions or Automaton tasks. See [autonomous-planning.md](autonomous-planning.md).
> - The Lane pulse card needs a capacity lane to have held or waited on a lease in its window, or a usage reading for the burn panel. See [usage-calculator.md](usage-calculator.md).
> - Guardrail verdict cards require guardrail profiles to be configured. See [guardrail-library.md](guardrail-library.md).

## What it is

- **Network** — the work map. One node per project, Automation, Automaton,
  task and live session, with an edge for every parent-to-child link. Click a
  project to expand it and a session to open it. The footer shows the node
  count, and a button steps through the running sessions. See
  [Work map details](#work-map-details).
- **Lane pulse** — one trace per capacity lane and node over a time window,
  drawn from busy (a lease held), waiting (a request queued for the lane) and
  idle time. Under the traces, the **burn panel** shows the weekly usage
  level in points against the pace line. See
  [Lane pulse details](#lane-pulse-details).
- **Integration status (opt-in)** — one row per schedule: status badge,
  summary and time of the latest posted result, an inline SVG sparkline of
  recent statuses (oldest → newest, capped at 60), and a click-through into
  the session that posted it. Not in the default layout — add it via the
  Add-Card panel. When no schedules have posted yet, the empty state
  explains how jobs post results and links the result endpoint in the API
  reference (`/api/openapi.yaml`).
- **Expand panel** — click the `⊞` button on a session or Automata card to
  open a three-column overlay:
  - **Left sidebar**: live task tree (reuses the telemetry task tree renderer).
  - **Main area**: session status board (hook health, current focus, tests,
    git).
  - **Right rail**: guardrail verdicts from the session's telemetry.

## Navigation

```
Bottom nav → ⊞ Dashboard
```

Or from any session card / Automata card via the `⊞` icon button.

## Two happy paths

### 3a. Happy path — PWA

1. Tap **⊞ Dashboard** in the bottom nav.
2. The **Network** card draws the work map. Projects, Automations, Automata,
   tasks and live sessions appear as nodes; a running session is the one to
   look for in the **Running now** strip.
3. Click a session in the map to open it. Click a project to expand it.
4. The **Lane pulse** card appears once a lane has held or waited on a lease in
   its window. Each lane and node is a row; busy segments are one colour and
   waiting segments another.
5. Open the `⊞` button on a session card or an Automata card to see the
   **expand panel** for that item. Its status board refreshes via WebSocket,
   so no manual reload is needed.

### 3b. Happy path — session/Automata card maximize

1. From the **Sessions** list or the **Automata** list, click the `⊞` button
   on any card.
2. The dashboard opens with that session's expand panel immediately visible.
3. Close the expand panel to return to the board.

## WebSocket events

The dashboard receives two event types:

| Event | Effect |
|-------|--------|
| `hook_update` | Updates the session's hook health and state on the cards; adds a sample to the Sessions sparkline; refreshes the expand panel if it is open for that session |
| `session_state` | Updates the session state shown on the cards when it changes |

The WebSocket keeps the session cards and the expand panel current. The
Network, Work map and Lane pulse cards read their REST endpoints while the
view is open, so they can lag the WebSocket by a few seconds.

## Performance notes

- The Sessions sparkline keeps a ring buffer of 400 hook events; older
  events are discarded.
- A longer Lane pulse window draws more segments (see
  [Lane pulse details](#lane-pulse-details)).

## Expand panel data sources

| Pane | Data source |
|------|-------------|
| Task tree (left) | `hook_update.board.telemetry` (live from WS); fetches `/api/sessions/{id}/status` on first open if no cached board |
| Status board (centre) | Same `board` from WS / initial fetch |
| Verdicts (right) | `board.telemetry.guardrail_verdicts` |

## Card data sources

Each feature card reads from one live source and shows a one-line reason
when it has nothing to draw. A card with no data is hidden from the grid while
`dashboard.hide_inactive_cards` is true (the default); its reason and help link
are what you see in the Add-Card picker.

| Card | Reads | Empty state |
|------|-------|-------------|
| **Live Events** | The daemon's own event stream, `GET /api/autonomous/event-log` — the same feed as the Events tab, not hook events. Its kind chips, project filter and search are described in [automata-events.md](automata-events.md). | "No Automata or session events yet — this card fills as work runs." |
| **Timeline** (6-hour Gantt) | Each Automaton task's start and end timestamps. Bars are drawn to the task times and the card is bounded to 960 px wide. | "No Automaton task has run in the last 6 hours — its bar appears here when one starts." |
| **30-Day Activity** (heatmap) | The daily progress trend, the shipped counts and the release tags. The card draws the whole 30-day month; each day's tooltip gives the Automata completed to date, the shipped count and any release. | "No Automata completed or shipped in the last 30 days — the month fills as work lands." |
| **CI & release** | The operator console's `ci_release` section: running and latest release, ship policy, repo CI watch and failing or hung runs. See [dashboard-operator-console.md](dashboard-operator-console.md#ci-and-release-detail). | "No release or ship state reported yet — this card fills after the first release check." |
| **Smoke Run** | Release smoke and end-to-end run progress, from `GET /api/smoke/progress`. A run against an external daemon (`DW_BASE`) posts its progress to that daemon, so the card shows it there; the throwaway test daemon of a default run is not posted. | "No smoke runs recorded on this machine." |
| **Cost report** | `GET /api/cost/report`, with the reconciliation against usage readings. See [cost-report.md](cost-report.md). | "No recorded sessions with token usage in this window — the card fills as sessions report usage." |
| **Search Usage** | `GET /api/websearch/stats`. Searches are counted by caller: session, Automaton task, MCP client, REST and CLI. See [chat-and-llm-quickstart.md](chat-and-llm-quickstart.md). | "Web search is off or no providers are configured." |
| **Network** | `GET /api/dashboard/work-map` — the same graph as the Work map card, drawn as the Network card's map. Its footer shows the node count and a button to step to the next running session. | "No live sessions or Automaton tasks yet — the map fills as work runs." |
| **Work map** | `GET /api/dashboard/work-map` — a graph of projects, Automations, Automata, tasks and live sessions, with one edge per parent-to-child link and a list of the running sessions. | "No live sessions or Automaton tasks yet — the map fills as work runs." |
| **Lane pulse** | `GET /api/dashboard/lane-pulse` — one row per lane and node, busy, waiting and idle over the window, and the usage burn panel. See [Lane pulse details](#lane-pulse-details). | "No lane has held or waited on a lease in this window yet, and no usage reading is available — this card fills as lanes run." |

### Work map details

- The graph is read-only. When the daemon has no trace store, the note
  **placement only (no trace store)** appears on the card's footer counts line,
  after the node count (`nodes: N`). A daemon with neither a session store nor a
  trace store returns 503.
- Tasks are capped per Automaton, newest first. The overflow is counted in the
  Automaton node's `tasks_omitted`, so one long-lived Automaton cannot flood the
  card.
- The **Running now** strip lists up to eight live sessions in an active state,
  newest first, and shows the total count beside its heading.
- The Network card and the Work map card show the same payload. Each one is
  hidden from the grid while the map has no nodes.

### Lane pulse details

- **Window.** `GET /api/dashboard/lane-pulse?window=<duration>` takes a Go
  duration from `1m` to `168h`. The default is `6h`. A value outside that range,
  or one that does not parse, is a `400`. The response's `window_from`,
  `window_to` and `window_seconds` say which window was answered.
- **Rows.** Each `series` entry is one lane on one node, with `busy_seconds`,
  `waiting_seconds` and `idle_seconds` for the window and the time-ordered
  `segments`. Busy is a capacity lease held; waiting is a request queued for that
  lane and node. A request often waits while the lane is full, so waiting can
  overlap busy. Idle is the time neither is true. Idle is only counted after the
  capacity ledger began keeping history, so the earliest part of a window can be
  uncovered (`recording_from` shows where history starts).
- **Burn panel.** The usage level is shown in **points** of the weekly
  allowance, against the **pace line**. The card says *ahead of pace* or *on
  pace*, and *stale* when the reading is old. The panel never shows dollars.
- **Burn reasons.** When the usage calculator cannot answer, the burn block has
  `available: false` and a reason code, and the card shows the matching text:
  `not_wired` (the calculator is not configured), `advice_unavailable`,
  `needs_percent`, `needs_reading` or `needs_reset`. The calculator's own
  settings are described in [usage-calculator.md](usage-calculator.md).
- **Card visibility.** The card stays off the grid until a lane held or waited in
  the window, or the burn panel can read a level. With
  `dashboard.hide_inactive_cards` off, it shows its reason instead.

## Read the dashboard data from other surfaces

The work map, the lane pulse and the six series each have a read-only surface
beyond the web page, and every one returns the same body as its REST route.
There is no setting: the cards' own visibility rules do not apply to these
reads, which always return the data (an empty one when nothing has run).

| Data | REST | MCP tool | CLI | Chat verb |
|------|------|----------|-----|-----------|
| Work map | `GET /api/dashboard/work-map` | `dashboard_work_map_get` | `datawatch dashboard work-map` | `dashboard work-map` |
| Lane pulse | `GET /api/dashboard/lane-pulse?window=6h` | `dashboard_lane_pulse_get` (`window`) | `datawatch dashboard lane-pulse --window 6h` | `dashboard lane-pulse [window]` |
| Series | `GET /api/dashboard/series/<name>` | `dashboard_series_get` (`series`, `window`, `days`, `limit`) | `datawatch dashboard series <name> [--window\|--days\|--limit]` | `dashboard series <name> [value]` |

The series names are `lane-utilization` and `verifier-health` (a `window`
duration), `throughput-forecast`, `release-train` and `defects-per-day` (a
number of `days`) and `review-backlog` (a `limit`). In chat the optional value
after the name feeds whichever of those the series takes. An unknown series is
refused before anything is sent.

## Open for operator review

These points are documented from the code as it stands. The operator should
confirm each before the wording is treated as final:

- **CI & release count changed meaning.** `ci_release.count` is now the number
  of CI runs under watch, not the number failing. The failing or hung runs are
  in `summary` and on the card. Any client that read `count` as "failing" needs
  updating.
- **Work-map empty and no-trace wording.** The no-trace note sits on the footer
  line, and the 503 for a daemon with no session or trace store is documented.
  Confirm both are the intended operator-facing behaviour.
- **Smoke Run for default runs.** A default run's throwaway test daemon is not
  posted to the dashboard. Confirm that a card showing nothing for such a run is
  acceptable, rather than a gap to close.
- **Cost report USD label.** The card labels the column **API-equivalent USD
  (unbilled)**. Confirm the wording with the operator before release notes repeat it.
- **Constellation and EKG are gone from the board.** The session constellation
  was replaced by the Network card and the EKG card by the Lane pulse card. The
  Sprint pipeline no longer renders anywhere on the dashboard. The help links
  for the Network and Lane pulse cards still open the old anchors
  `#session-constellation` and `#ekg-waveform` in
  [datawatch-definitions.md](../datawatch-definitions.md#dashboard). Those
  anchors are kept and now describe the new cards. Confirm the Sprint pipeline
  is meant to stay removed.
- **Lane pulse live check is owed.** The Go and PWA tests pass, but the
  testing tracker records no live check against a daemon with real lease traffic
  and usage readings. Confirm before the card is called validated.
- **Lane pulse help link.** The Lane pulse card's help link uses the key of the
  Live Events card, so its *Dashboard card reference* link opens the Live Events
  entry. Confirm whether it should get its own entry.
- **Locale text.** The work map and lane pulse strings now exist in the English,
  German, Spanish, French and Japanese bundles. The empty-state sentences above
  are the English strings. Confirm the translations of the new strings with a
  native speaker before release.

## Related how-tos

- [sessions-deep-dive.md](sessions-deep-dive.md) — per-session Status tab detail
- [session-telemetry.md](session-telemetry.md) — hook payload schema
- [guardrail-library.md](guardrail-library.md) — guardrail library and profiles
- [autonomous-planning.md](autonomous-planning.md) — Automata lifecycle
- [api/mobile-surface.md](../api/mobile-surface.md) — WebSocket events for mobile/Wear/Auto

## See also

- [datawatch-definitions](../datawatch-definitions.md)
- [howto/sessions-deep-dive](sessions-deep-dive.md)
- [howto/guardrail-library](guardrail-library.md)
- [api/autonomous](../api/autonomous.md)
