# datawatch v5.26.44 — release notes

**Date:** 2026-04-27
**Spans:** v5.26.43 → v5.26.44
**Patch release** (no binaries — operator directive).
**Closed:** Yellow "Input Required" banner dismiss no longer leaves the terminal mis-sized.

## What's new

### Banner-dismiss now refits the xterm viewport immediately

Operator: *"if there is a yellow popup in pwa and i close it, the screen doesn't resize properly and i have to exit and go back into sessions to fix."*

The yellow popup is the per-session **"Input Required"** banner that appears when claude / opencode / aider hits a `waiting_input` state. It sits above the xterm.js terminal in the session-detail view. Pre-v5.26.44 dismiss flow:

```
operator clicks ✕  →  state.needsInputDismissed[sessionId] = true
                   →  refreshNeedsInputBanner() patches slot.innerHTML = ""
                   →  banner div shrinks to 0
                   →  ResizeObserver on the terminal container DOES fire,
                      but the 200ms debounce + the lack of an explicit
                      tmux-side resize meant the operator saw a broken
                      layout for a noticeable beat
                   →  operator workaround: leave session view + return,
                      which triggers a fresh init + fit
```

The `ResizeObserver` was supposed to handle this but the debounce and the lack of an immediate `resize_term` round-trip to tmux made it look broken. v5.26.44 forces the resize synchronously after the next animation frame (DOM flushed):

```js
function dismissNeedsInputBanner(sessionId) {
  state.needsInputDismissed[sessionId] = true;
  refreshNeedsInputBanner(sessionId);
  if (state.activeView === 'session-detail' &&
      state.activeSession === sessionId &&
      state.termFitAddon) {
    requestAnimationFrame(() => {
      try { state.termFitAddon.fit(); } catch(e) {}
      const t = state.terminal;
      if (t && t.cols && t.rows) {
        send('resize_term', { session_id: sessionId, cols: t.cols, rows: t.rows });
      }
    });
  }
}
```

`requestAnimationFrame` ensures the DOM has reflowed (the slot's height is now 0) before `fit()` reads container dimensions. The follow-up `resize_term` WS message tells the daemon to reshape the underlying tmux pane to the new cols/rows so the LLM redraws at the correct width.

### Why not just rely on ResizeObserver

The observer is still in place — it'll catch font-size changes, window resizes, and any other layout shift. But for the dismiss-banner path we know exactly which observer trigger we want and exactly which size to forward, so the explicit call is faster + more predictable than waiting for the debounce.

## Configuration parity

No new config knob — UI fix.

## Tests

UI-only change. Manually validated by:

```bash
# 1. Open a session in PWA
# 2. Wait for it to enter waiting_input (banner appears)
# 3. Click the ✕ on the banner
# 4. Verify: terminal expands into freed space immediately (no
#    visible busted layout, no need to navigate away + back)
```

Go test suite unaffected (still 465 passing). Smoke unaffected (40/0/1).

## Known follow-ups

Same backlog as v5.26.43 — see `docs/plans/2026-04-27-v6-prep-backlog.md`.

## Upgrade path

```bash
git pull
datawatch restart
# Hard-reload the PWA (SW cache bumped to datawatch-v5-26-44).
# Open a session that's waiting on input, dismiss the yellow
# banner — the terminal should immediately grow into the freed
# vertical space.
```
