---
docs:
  index: true
  topics: [dashboard, sessions, telemetry, constellation, guardrail]
---
# How-to: /dashboard Mission Control

The `/dashboard` view gives you a live, full-fleet picture of everything
running on a datawatch instance: session constellation, EKG activity
waveform, sprint pipeline for active Automata, and an expand panel for
per-session deep-dives — all driven by WebSocket, zero polling.

> **Pre-conditions**: The dashboard surfaces live data from multiple subsystems. For the full experience:
> - Sessions and LLM cards require an LLM backend. See [llm-registry.md](llm-registry.md).
> - Automata / Timeline / Network cards require Automata to be running. See [autonomous-planning.md](autonomous-planning.md).
> - Guardrail verdict cards require guardrail profiles to be configured. See [guardrail-library.md](guardrail-library.md).

## What it is

- **Session constellation** — every active session rendered as a node in an
  SVG force-directed graph. Node colour reflects session state; pulse ring
  indicates activity; hook-health ring shows hook event status.
- **EKG waveform** — a scrolling canvas trace fed by every incoming
  `hook_update` WebSocket event. Spikes decay over time; the line shows
  aggregate fleet activity at a glance.
- **Sprint pipeline** — shown when Automata are running. Horizontal stage
  bar with story nodes, gate rings (pass/fail from guardrail verdicts), and
  stage colours matching story status.
- **Integration status (opt-in)** — one row per schedule: status badge,
  summary and time of the latest posted result, an inline SVG sparkline of
  recent statuses (oldest → newest, capped at 60), and a click-through into
  the session that posted it. Not in the default layout — add it via the
  Add-Card panel. When no schedules have posted yet, the empty state
  explains how jobs post results and links the result endpoint in the API
  reference (`/api/openapi.yaml`).
- **Expand panel** — click any constellation node (or the `⊞` button on a
  session or Automata card) to open a three-column overlay:
  - **Left sidebar**: live task tree (reuses S3 `renderLiveTaskTree`).
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
2. The EKG canvas animates immediately (even with no hook events — flatline
   is the baseline state).
3. Sessions appear as nodes in the constellation. Nodes for running sessions
   pulse; waiting-input nodes glow amber; failed nodes are red.
4. Click any node to open the **expand panel** for that session.
5. In the expand panel, the status board auto-refreshes via WebSocket — no
   manual reload needed.
6. Click **← Back** to return to the full constellation view.
7. When Automata are running, the sprint pipeline appears at the bottom: each
   story is a stage; gate rings show guardrail pass/fail between stages.

### 3b. Happy path — session/Automata card maximize

1. From the **Sessions** list or the **Automata** list, click the `⊞` button
   on any card.
2. The dashboard opens with that session's expand panel immediately visible.
3. Close the expand panel to see the full constellation.

## WebSocket events

The dashboard receives two event types:

| Event | Effect |
|-------|--------|
| `hook_update` | Updates the node's hook health + state; adds a spike to the EKG; refreshes expand panel if open for that session |
| `session_state` | Updates node colour and label when session state changes |

All updates are zero-polling — no `setInterval` / REST calls inside the
animation loop.

## Performance notes

- The constellation SVG re-renders at ~20fps (every 3 animation frames).
- The EKG canvas renders at full 60fps; the force-layout physics step also
  runs at ~20fps.
- The spring pipeline re-renders at ~10fps.
- The EKG ring buffer caps at 400 events; older events are discarded.
- Force layout uses simple Euler integration with damping (not a full Barnes-Hut). For fleets > 30 sessions the layout may be slow to converge — expect ~5s for the graph to stabilise.

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
| **Work map** | `GET /api/dashboard/work-map` — a graph of projects, Automations, Automata, tasks and live sessions, with one edge per parent-to-child link and a list of the running sessions. | "No live sessions or Automaton tasks yet — the map fills as work runs." |

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
- **Locale text.** The empty-state sentences above are the English strings. The
  de, es, fr and ja locale files were not changed by this docs pass, so those
  translations may not match.

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
