---
docs:
  index: true
  topics: [dashboard, websocket, constellation, EKG, architecture]
---
# Flow: Dashboard Mission Control

Data flow through the `/dashboard` view — from WebSocket events to rendered
constellation, EKG, and sprint pipeline.

## End-to-end flow

```
Daemon                         WebSocket                    PWA /dashboard
──────────────────────────────────────────────────────────────────────────

hook script fires                                              ┌─────────────┐
  └─ POST /api/sessions/{id}/hook-event                        │ _dash state │
       └─ globalHookStore.record()                             │  .nodes{}   │
            └─ BroadcastHookUpdate(sid, board)    ──────────►  │  .ekg[]     │
                                                               │  ._boards{} │
session state change                                           └─────────────┘
  └─ hub.Broadcast(MsgSessionState, sess)         ──────────►  updateSession()
                                                               └─ update node
                                                                  colour/state

requestAnimationFrame loop (60fps):
  ├─ _drawEKG()            ← reads _dash.ekg[]   (canvas repaint)
  ├─ _dashForceStep()      ← Euler physics step  (every 3rd frame)
  ├─ _drawConstellation()  ← SVG innerHTML update (every 3rd frame)
  └─ _renderSprintPipeline() ← HTML update        (every 6th frame)

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
     _dash.ekg.push(...)         — adds EKG spike
```

## Sprint pipeline data

```
renderDashboardView()
  └─ if _automataState.allPrds available: use in-memory cache
     else: GET /api/autonomous/prds  → _dash._prds
               └─ filter: status ∈ {running, blocked, planning}

prd_update WS event (existing handler)
  └─ reloads _automataState.allPrds when on autonomous view
     (dashboard uses cached _automataState.allPrds on next mount)
```

## Performance budget

| Component | Update rate | Cost |
|-----------|-------------|------|
| EKG canvas | 60fps | ~0.2ms (clearRect + drawLine) |
| Force step | 20fps | O(n²) repulsion, bounded at n≤50 |
| Constellation SVG | 20fps | innerHTML string; ~1ms for 20 nodes |
| Sprint pipeline | 10fps | HTML string; ~0.5ms |
| EKG ring buffer | capped at 400 | memory O(400) |

## See also

- [websocket-flow.md](websocket-flow.md) — full WS event catalogue including `hook_update`
- [telemetry-flow.md](telemetry-flow.md) — hook payload → telemetry store
- [howto/dashboard.md](../howto/dashboard.md) — operator how-to guide
- [api/mobile-surface.md](../api/mobile-surface.md) — /dashboard WS events for mobile
