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
