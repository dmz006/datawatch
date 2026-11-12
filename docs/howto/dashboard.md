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
- **Automaton Timeline card** — the Gantt chart of your Automata across
  every project: each stage a band, each member a bar with its start and
  end, blocked/waiting time hatched on top, a solid today line and a dashed
  projected-finish line, plus five filters (project, automation, stage,
  kind, tag). A different thing from the 6h Gantt card; see
  [automata-timeline.md](automata-timeline.md).
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

Each heading in this section is the target of its card's **?** help link, so a
card's name and its section here always match. Every section says what the card
is for, where its data comes from and what *off* means for that card.

A card that has nothing to draw is **not shown**. The grid and the Add-Card
picker leave it out while `dashboard.hide_inactive_cards` is true (the default),
so the board never carries an empty frame. The test for each card is its entry in
the page's `DASH_CARD_FEATURES` table (its `active()` check), which is why *off*
below always means "no data yet", not "broken". With the setting off, the card
stays on the board and shows a single line saying why it is empty. There is no
setting per card.

### Questions waiting card

- **For:** the questions the Automata are waiting for you to answer, as a count with a link to where you answer them.
- **Data:** the `questions_waiting` section of `GET /api/dashboard/console`.
- **Off:** the card stays on the default board. If `dashboard.console.enabled` or `dashboard.console.show_questions` is false, it shows that reason and a link to Settings instead of a count. See [dashboard-operator-console.md](dashboard-operator-console.md).

### Held and blocked card

- **For:** Automata that are held or blocked, with the reason each stopped.
- **Data:** the `held_blocked` section of `GET /api/dashboard/console`.
- **Off:** stays on the default board; `dashboard.console.show_held_blocked` false (or the console switched off) shows the reason and a Settings link in place of the count.

### Lanes card

- **For:** how many capacity lanes are busy, waiting or idle right now.
- **Data:** the `lanes` section of `GET /api/dashboard/console`, a one-number summary of the same picture the Network card draws.
- **Off:** stays on the default board; `dashboard.console.show_lanes` false shows the reason and a Settings link.

### Usage card

- **For:** your plan usage so far, in points of the weekly allowance against the pace line. Never dollars.
- **Data:** the `usage` section of `GET /api/dashboard/console`, from the usage calculator ([usage-calculator.md](usage-calculator.md)).
- **Off:** stays on the default board; `dashboard.console.show_usage` false, or a calculator with no reading, shows the reason instead of a level.

### CI and release card

- **For:** the running and latest release, the ship policy, the repo CI watch and any failing or hung runs, with the next action.
- **Data:** the `ci_release` section of `GET /api/dashboard/console`. See [dashboard-operator-console.md](dashboard-operator-console.md#ci-and-release-detail) for every field.
- **Off:** hidden until the daemon has reported a release check or a ship policy. A repo CI watch that is off does not hide it; the card still shows the release and ship state and says *repo CI watch off*. Empty line: "No release or ship state reported yet — this card fills after the first release check."

### Integration status card

- **For:** the latest result each scheduled job posted.
- **Data:** one row per schedule from the schedule results, with the time of the latest posted result, a sparkline of recent statuses (oldest to newest, capped at 60) and a click-through to the session that posted it. Jobs post through the result endpoint in the API reference (`/api/openapi.yaml`).
- **Off:** hidden until a schedule has posted a result. Empty line: "No scheduled jobs on this install — create one to fill this card." Opt-in: not in the default layout.

### Automata card

- **For:** Automata with their stories and tasks, as a live tree.
- **Data:** the Automaton list (`GET /api/autonomous/prds`), refreshed while the view is open.
- **Off:** stays on the default board; with no Automata the tree is empty.

### Network card

- **For:** the work map as a graph: projects, Automations, Automata, tasks and live sessions.
- **Data:** `GET /api/dashboard/work-map`, the same payload as the Work map card. The footer shows the node count and a button that steps to the next running session. See [Work map details](#work-map-details).
- **Off:** hidden while the map has no nodes. Empty line: "No live sessions or Automaton tasks yet — the map fills as work runs."

### Live Events card

- **For:** a live feed of Automata and session events.
- **Data:** the daemon's own event stream, `GET /api/autonomous/event-log`, the same feed as the Events tab. These are not hook events. The kind chips, project filter and search are described in [automata-events.md](automata-events.md).
- **Off:** hidden until the feed has an event. Empty line: "No Automata or session events yet — this card fills as work runs."

### Sessions card

- **For:** session activity over time, one line per session.
- **Data:** the `hook_update` events on the WebSocket, kept in a ring buffer of 400; older events are dropped.
- **Off:** stays on the default board; with no hook events the lines are flat.

### Timeline card

- **For:** Automaton tasks over the last 6 hours, drawn as bars.
- **Data:** each task's start and end timestamps. Bars are drawn to the task times and the card is bounded to 960 px wide.
- **Off:** hidden while no task ran in the window. Empty line: "No Automaton task has run in the last 6 hours — its bar appears here when one starts."

### 30-Day Activity card

- **For:** Automata completed and shipped each day this month.
- **Data:** the daily progress trend, the shipped counts and the release tags. The card draws the whole 30-day month; each day's tooltip gives the Automata completed to date, the shipped count and any release.
- **Off:** hidden until a day has progress, a shipped count or a tag. Empty line: "No Automata completed or shipped in the last 30 days — the month fills as work lands."

### Guardrails card

- **For:** the verdicts from the guardrail gates that checked recent work.
- **Data:** the `guardrail_verdicts` in each session's telemetry board, as in the expand panel. Profiles are set up in [guardrail-library.md](guardrail-library.md).
- **Off:** hidden until a gate has produced a verdict. Empty line: "No guardrail verdicts yet — nothing to show until a gate runs." Opt-in.

### Lane pulse card

- **For:** busy, waiting and idle time for each lane, plus the usage burn panel.
- **Data:** `GET /api/dashboard/lane-pulse?window=<duration>`. See [Lane pulse details](#lane-pulse-details).
- **Off:** hidden until a lane held or waited on a lease in the window, or the burn panel can read a usage level. Empty line: "No lane has held or waited on a lease in this window yet, and no usage reading is available — this card fills as lanes run."

### Smoke Run card

- **For:** progress of the release smoke and end-to-end runs.
- **Data:** `GET /api/smoke/progress`. A run against an external daemon (`DW_BASE`) posts its progress to that daemon, so the card shows it there; the throwaway test daemon of a default run is not posted.
- **Off:** hidden until a run is recorded. Empty line: "No smoke runs recorded on this machine." Opt-in.

### Memory scopes card

- **For:** how many memories each scope holds.
- **Data:** `GET /api/memory/stats`. See [memory.md](../memory.md).
- **Off:** hidden until the install holds a memory entry. Empty line: "No memory entries on this install yet." Opt-in.

### Search Usage card

- **For:** web searches by provider and by who made them.
- **Data:** `GET /api/websearch/stats`. Searches are counted by caller: session, Automaton task, MCP client, REST and CLI. See [chat-and-llm-quickstart.md](chat-and-llm-quickstart.md).
- **Off:** hidden while web search is off or no provider is configured. Empty line: "Web search is off or no providers are configured." Opt-in.

### Work map card

- **For:** projects, Automations, Automata, tasks and sessions as a graph, with a list of the running sessions.
- **Data:** `GET /api/dashboard/work-map`, one node for each project, Automation, Automaton, task and live session and one edge per parent-to-child link. See [Work map details](#work-map-details).
- **Off:** hidden while the map has no nodes. Empty line: "No live sessions or Automaton tasks yet — the map fills as work runs." Opt-in.

### Lane utilization card

- **For:** how much of each lane's time was busy over the window.
- **Data:** `GET /api/dashboard/series/lane-utilization?window=<duration>` (default `24h`, `1h` to `168h`): for each lane and clock hour, the share of that hour a lane held a lease.
- **Off:** hidden until a lane held a lease in the window. Empty line: "No lane has held a lease in the last 24 hours — this card fills as lanes run." Opt-in.

### Throughput forecast card

- **For:** how many Automata should finish soon at the current pace.
- **Data:** `GET /api/dashboard/series/throughput-forecast?days=<n>` (default `30`, `1` to `365`), from the saved progress snapshots: each day's completion range in days (low and high) and completions per active day over the 24 hours before that snapshot.
- **Off:** hidden until a saved snapshot has a throughput or completion estimate. Empty line: "No saved progress snapshot has a throughput or completion estimate yet — the forecast fills as Automata finish." Opt-in.

### Verifier health card

- **For:** how often verifiers pass, fail or have to wait for a node.
- **Data:** `GET /api/dashboard/series/verifier-health?window=<duration>` (default `24h`, `1m` to `168h`): verifier waits, runs and a pass rate per compute node. The pass rate is the passing verifier results over all results logged for that node in the window; a node with none shows a dash, not 0%. The results are kept in memory, so the card starts empty with the daemon.
- **Off:** hidden until a verifier ran or waited on a node in the window. Empty line: "No verifier has run or waited on a node in this window — this card fills as verifiers run." Opt-in.

### Release train card

- **For:** tags, CI runs and deploys in the window, in order.
- **Data:** `GET /api/dashboard/series/release-train?days=<n>` (default `14`, `1` to `90`): tags cut, CI state and deploy state from the ship status and the repo CI watch.
- **Off:** hidden until the window holds a tag, a CI run or deploy state. Empty line: "No tags, CI runs or deploy state in this window yet — this card fills after the first release or ship." Opt-in.

### Review backlog card

- **For:** plans and held questions waiting for a decision.
- **Data:** `GET /api/dashboard/series/review-backlog?limit=<n>` (default `50`, `1` to `200`): each waiting plan or held question with its age, oldest first.
- **Off:** hidden while nothing waits. Empty line: "No plans or held questions are waiting for a decision — this card fills when one waits." Opt-in.

### Defects per day card

- **For:** defects found and fixed each day.
- **Data:** `GET /api/dashboard/series/defects-per-day?days=<n>` (default `14`, `1` to `90`): defects found and fixed per UTC day from the defect intake log. A fix is a tiny fix applied without error.
- **Off:** hidden until a defect was found or fixed in the window. Empty line: "No defects were found or fixed in this window — this card fills as defects are recorded." Opt-in.

### Cost report card

- **For:** token spend grouped by Automaton, Automation, backend or week.
- **Data:** `GET /api/cost/report`, with the reconciliation against usage readings. The column is labelled **API-equivalent USD (unbilled)**. See [cost-report.md](cost-report.md).
- **Off:** hidden until the report has token usage, or until you touch one of its controls (so a window with no usage can be changed back). Empty line: "No recorded sessions with token usage in this window — the card fills as sessions report usage."

Each of the six series cards shows four lines when it has data: what it is for,
its state, the reason it is empty when it is, and the next action for you. None
of them has a setting of its own.

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
- **Per-card help links.** Every card's **?** link opens its own section under
  [Card data sources](#card-data-sources). The related-setting link on a card's
  one-line *off* state still opens the dashboard entry of
  [datawatch-definitions.md](../datawatch-definitions.md#dashboard). Confirm the
  section wording with the operator.
- **Verifier health starts empty after a restart.** Its per-node results are
  kept in memory, so the card is blank until verifiers run again after the
  daemon starts. Confirm that is acceptable, or ask for the results to be kept
  on disk.
- **Six new cards are opt-in.** Lane utilization, Throughput forecast, Verifier
  health, Release train, Review backlog and Defects per day appear only through
  Edit → Add Card, and only once they have data. Confirm none should be on the
  default board. The mobile app does not draw them yet; see
  [the parity issue](../plans/parity-issues/dashboard-cards-data-fix.md).
- **Locale text.** The work map and lane pulse strings now exist in the English,
  German, Spanish, French and Japanese bundles. The empty-state sentences above
  are the English strings. Confirm the translations of the new strings with a
  native speaker before release.

## Related how-tos

- [sessions-deep-dive.md](sessions-deep-dive.md) — per-session Status tab detail
- [session-telemetry.md](session-telemetry.md) — hook payload schema
- [guardrail-library.md](guardrail-library.md) — guardrail library and profiles
- [autonomous-planning.md](autonomous-planning.md) — Automata lifecycle
- [dashboard-arc-view.md](dashboard-arc-view.md) — the opt-in Arc card: stages, maintenance lane, blocked stage, lanes and plan usage in points and tokens
- [automata-timeline.md](automata-timeline.md) — the Automata interval chart across all projects (not the 6h Gantt card on this dashboard)
- [api/mobile-surface.md](../api/mobile-surface.md) — WebSocket events for mobile/Wear/Auto

## See also

- [datawatch-definitions](../datawatch-definitions.md)
- [howto/sessions-deep-dive](sessions-deep-dive.md)
- [howto/guardrail-library](guardrail-library.md)
- [api/autonomous](../api/autonomous.md)
