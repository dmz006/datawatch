# Release Notes — v6.11.25

Released: 2026-05-05
GH release: https://github.com/dmz006/datawatch/releases/tag/v6.11.25

### Summary — BL266 follow-up: kill the splash gate, kill NLP→Complete, move generating slot

Three operator-reported regressions from v6.11.24:

1. **Top badge says Running while popup says session ended, lights still pulsing** — and a follow-up: **typing no longer makes the screen appear either**. Root cause: NLP per-sentence suffix matcher was promoting multi-sentence claude wraps like *"Done. Let me know if you want anything else."* to `EventComplete`. Once Complete (sticky), the PWA's pane_capture skip gate (added v6.11.12) dropped every frame for 10 s — splash stayed forever, badge eventually corrected via state broadcast but visuals were already stale.
2. **Generating indicator on the channel tab appears at the top instead of under the history.** Slot was DOM-positioned between tmux and channel areas — fine when tmux was active (slot is below), wrong when channel was active (slot is above the channel content).
3. **Entering an ended session shows blank "connecting…" instead of the saved last screen.** Same skip-gate cause as #1, plus the NLP false-Complete promoting fresh sessions into the gate.

### Fixed

- **`internal/session/manager.go` `MarkChannelActivityFromText`** — REMOVED the NLP→Complete promotion. NLP→input-needed promotion (the conservative `?` and `should I proceed` patterns) is kept. Complete now requires a structural signal (ACP `session.completed` / `message.completed`, MCP `DATAWATCH_COMPLETE:` marker, operator-issued kill).
- **`internal/server/web/app.js` `pane_capture` handler** — REMOVED the v6.11.12 terminal-state skip gate entirely. With BL266 Complete only fires on real signals; if a session is genuinely Complete, drawing the saved final frame is what the operator wants (their explicit ask 2026-05-05: *"it doesn't take the screenshot and send it showing the inactive screen instead of stays at loading session message"*).
- **`internal/server/web/app.js` session detail layout** — moved `<div id="generatingSlot">` from between tmux and channel areas to AFTER both, so the indicator appears under whichever output area is active (operator: *"the generating status indicator on channel tab should be under the history, it is currently on top"*).

### Tests

- **`internal/session/bl266_state_engine_test.go`** — added 2 new cases:
  - `TestBL266Followup_NLP_DoesNotPromoteCompleteFromText` — multi-sentence wraps like *"Done. Let me know if you want anything else."* and the bare *"Task complete."* must NOT flip Running → Complete.
  - `TestBL266Followup_NLP_StillPromotesInputNeeded` — the conservative `?`/`should I proceed` path is preserved.
- **`internal/session/bl265_channel_content_test.go`** — updated 2 cases that asserted the now-removed Complete promotion behaviour to assert the inverse (NLP completion text leaves state unchanged). Renamed with `_v6_11_25` suffix to make the policy change explicit in test history.
- 523 session+server tests pass.
