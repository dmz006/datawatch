# Release Notes — v6.11.24

Released: 2026-05-05
GH release: https://github.com/dmz006/datawatch/releases/tag/v6.11.24

### Summary — BL266 channel-driven state engine (Option C)

Operator-directed: replace the natural-language phrase classifier (v6.11.18→v6.11.23) as the **primary** state-change driver. Five releases of tuning string patterns kept missing real claude wraps, false-firing on mid-sentence text, or breaking the PWA. New architecture:

- **Path B — structured ACP events**: opencode-acp's own SSE event stream (`session.status: busy/idle`, `session.idle`, `session.completed`, `message.completed`, `step-start`, `message.part.delta`, `message.part.updated`) drives state transitions directly. Authoritative — opencode's state machine is the source of truth for opencode-acp sessions; the NLP classifier is bypassed entirely.
- **Path A — universal event-rate watcher**: any Running session whose last channel event (or pane-content change, or operator input) is older than **15 s** flips to WaitingInput. Handles backends with no structural idle signal (claude-code MCP, ollama chat).
- **2 s "stale comms" PWA indicator**: the daemon broadcasts `last_channel_event_at` on every Session payload; the PWA renders an amber pulsing dot adjacent to the state badge after 2 s of silence (no state change). Gives early visual + alerting hook before the 15 s state flip.
- **NLP classifier demoted to advisory**: it can still promote a "complete" or "input-needed" pattern through the structural engine (which enforces sticky-state guards), but it can no longer flip state on its own. Removes 5 releases of whack-a-mole.

### Definitions

- **Running** — LLM is actively processing.
- **WaitingInput** — LLM is paused, awaiting your next message. **Reversible** — you type, it goes back to Running.
- **Complete** — LLM finished its task. **Terminal** — won't resume on its own; needs a fresh session/restart.

### Added

- **`internal/session/state_engine.go`** (new) — `MarkChannelEvent(fullID, kind)` for structural EventRunning/EventIdle/EventComplete; `MarkACPEvent(fullID, eventType, statusType)` typed entry for opencode SSE; `StartChannelStateWatcher(ctx, tick, gap)` background goroutine; `classifyACPEventType` + `classifyMCPMarker` pure-function classifiers.
- **`internal/session/store.go` `Session.LastChannelEventAt`** — broadcast as JSON `last_channel_event_at`; PWA computes staleness client-side.
- **`internal/llm/backends/opencode/acpbackend.go`** — `OnACPEvent` hook fired for every SSE event (extracts `status.type` for `session.status` events).
- **`cmd/datawatch/main.go`** — wires `OnACPEvent` to `mgr.MarkACPEvent`; starts `mgr.StartChannelStateWatcher(ctx, 1s, 15s)` at daemon startup.
- **`internal/server/web/style.css`** — `.output-area-channel { overflow-y: auto }` + `-webkit-overflow-scrolling: touch` so the channel tab supports native swipe-back through the existing 1000-entry buffer (operator request).
- **`internal/server/web/style.css`** — `.state .stale-dot` + `.state.stale-comms .stale-dot` pulsing amber dot.
- **`internal/server/web/app.js`** — 1 s `setInterval` scans `.state[data-channel-evt]` badges and toggles `.stale-comms` based on `(now - last_channel_event_at) > 2000`.
- **5 locale bundles** — `session_stale_comms` tooltip key.

### Fixed

- **`internal/server/web/style.css` `.scroll-bar-btn`** — operator: "scroll back for tmux feature isn't working since the buttons were resized". v6.11.20 stacked `flex: 1 1 0` (basis 0) AND `flex-basis: 33%` (overrode it to 33%); combined with `gap: 8px` the children's preferred sizes totalled >100% on narrow viewports, pushing the rightmost button outside the row where touch hits missed it. Dropped the explicit 33% basis.
- **`internal/session/manager.go` `MarkChannelActivityFromText`** — DEMOTED from primary state-driver to advisory wrapper; bumps activity timestamp + promotes `complete`/`input` NLP signals through the structural engine (which enforces sticky-state guards).
- **`internal/session/manager.go` `StartScreenCapture`** — pane-content change now bumps `LastChannelEventAt` so claude-code MCP sessions (which have no structural idle signal but do update the pane during tool calls) don't false-flip to WaitingInput during long tool calls.
- **`internal/session/manager.go` `SendInput`** — operator typing bumps `LastChannelEventAt` so the gap watcher doesn't flip Running → WaitingInput while the LLM is still processing the just-sent input.

### Tests

- **`internal/session/bl266_state_engine_test.go`** — 12 cases, both paths independently:
  - **Path B**: busy→Running, idle→WaitingInput, session.idle→WaitingInput, session.completed→Complete, message.completed→Complete, step-start counts as activity, terminal/RateLimited never resurrected, full classifier mapping table.
  - **Path A**: 15 s gap flips Running→WaitingInput, leaves non-Running alone, skips zero LastChannelEventAt (fresh sessions), respects fresh UpdatedAt as activity.
- 523 session+server tests pass (was 511).

### Mobile parity

[`datawatch-app#73`](https://github.com/dmz006/datawatch-app/issues/73) — apply same Option C architecture (structured ACP signals + 15 s gap watcher + 2 s stale indicator) to Compose Multiplatform.
