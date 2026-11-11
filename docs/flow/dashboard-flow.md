---
docs:
  index: true
  topics: [dashboard, websocket, network, work-map, lane-pulse, architecture]
---
# Flow: Dashboard Mission Control

Data flow through the `/dashboard` view — from WebSocket events and the two
dashboard APIs to the rendered Network (work map), Lane pulse (with burn panel)
and session cards. The session constellation, the EKG waveform and the sprint
pipeline strip have been removed; the Network card replaces the constellation
and the Lane pulse card replaces the EKG.

## End-to-end flow

```
Daemon                         WebSocket                    PWA /dashboard
──────────────────────────────────────────────────────────────────────────

hook script fires                                              ┌─────────────┐
  └─ POST /api/sessions/{id}/hook-event                        │ _dash state │
       └─ globalHookStore.record()                             │  .nodes{}   │
            └─ BroadcastHookUpdate(sid, board)    ──────────►  │  ._boards{} │
                                                               │  ._workMap  │
session state change                                           │  ._lanePulse│
  └─ hub.Broadcast(MsgSessionState, sess)         ──────────►  └─────────────┘
  └─ updateSession()                                             │
       └─ update node colour/state                               │
                                                                 │
GET /api/dashboard/work-map        (one shared fetch per ~30s) ──┤
GET /api/dashboard/lane-pulse?window=6h (one shared fetch per ~30s) ┘

requestAnimationFrame loop (60fps):
  ├─ _dashForceStep()          ← Euler physics step for the Network layout (every 3rd frame)
  ├─ _dashRenderLanePulse()    ← from _dash._lanePulse, incl. burn panel (the 'ekg' card)
  ├─ _dashRenderWorkMap()      ← from _dash._workMap (the 'orbital' Network card and 'work-map' card)
  └─ work-map and lane-pulse refetch every 900 frames (~30s), once per tick

Each fetch also re-renders its card as soon as the payload arrives.

User clicks node / ⊞ button
  └─ openDashExpand(sid)
       ├─ reads _dash._boards[sid]  (cached board from last hook_update)
       ├─ if missing: GET /api/sessions/{sid}/status  (one-time fetch)
       └─ renders 3-col expand overlay:
            ├─ left:   renderLiveTaskTree(board.telemetry, sessionType)
            ├─ centre: renderSessionStatusBoardInner(board, sid)
            └─ right:  renderSessionGuardrailVerdicts(verdicts)

hook_update arrives while expand is open
  └─ _dashUpdateExpand(sid)  → refreshes all 3 panes in-place
```

## Node lifecycle

```
session_state WS event
  └─ updateSession(sess) — existing handler
       └─ if activeView === 'dashboard':
            _dash.nodes[sid].color  = _dashNodeColor(sess.state)
            _dash.nodes[sid].state  = sess.state
            _dash.nodes[sid].label  = sess.name || ...

hook_update WS event
  └─ _dash.nodes[sid].hookHealth = board.hook_health
     _dash.nodes[sid].threats    = guardrail block+warn count
     _dash._boards[sid]          = board (used by the expand panel)
```

## Work map and lane pulse data

```
Network / Work map cards
  └─ GET /api/dashboard/work-map  → _dash._workMap
       one node per project, Automation, Automaton, task and live session;
       one edge per parent-to-child link. Answers 503 only when the daemon has
       neither a session store nor a trace store.

Lane pulse card (and its burn panel)
  └─ GET /api/dashboard/lane-pulse?window=<duration>  → _dash._lanePulse
       default window 6h, maximum 168h (7 days). One trace per capacity lane
       and node: busy (lease held), waiting (request queued), idle (neither).
       The burn panel reads the usage readings in the same payload, in points.
       The card is hidden until a lane held or waited in the window, or the
       burn panel can read a level.
```

Series cards (opt-in)
  └─ GET /api/dashboard/series/<name>  (lane-utilization, throughput-forecast,
       verifier-health, release-train, review-backlog, defects-per-day)
       Each card's `active()` entry in DASH_CARD_FEATURES keeps it off the grid
       until its series has data; the one-line reason is shown only when
       dashboard.hide_inactive_cards is off. Every card's "?" link opens its
       own section in howto/dashboard.md.

The sprint pipeline strip no longer renders. Sprint and Automaton detail for a
session appears in the expand panel's status board (`board.sprint`), not on the
grid.

## Performance budget

| Component | Update rate | Notes |
|-----------|-------------|-------|
| Force step (Network layout) | every 3rd frame | O(n²) repulsion, bounded at n≤50 |
| Work map and Network render | frame loop, from cached payload | rebuilt from `_dash._workMap` |
| Lane pulse and burn panel | frame loop, from cached payload | rebuilt from `_dash._lanePulse` |
| Work-map and lane-pulse fetch | every 900 frames (~30s) | one shared request each |

The EKG ring buffer (`_dash.ekg`, capped at 400) is still filled on each
`hook_update`, but no card reads it. It is a candidate for removal in a code change.

## See also

- [websocket-flow.md](websocket-flow.md) — full WS event catalogue including `hook_update`
- [telemetry-flow.md](telemetry-flow.md) — hook payload → telemetry store
- [howto/dashboard.md](../howto/dashboard.md) — operator how-to guide
- [api/mobile-surface.md](../api/mobile-surface.md) — /dashboard WS events for mobile
