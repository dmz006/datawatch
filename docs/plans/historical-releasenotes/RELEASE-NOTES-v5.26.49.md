# datawatch v5.26.49 — release notes

**Date:** 2026-04-27
**Spans:** v5.26.48 → v5.26.49
**Patch release** (no binaries — operator directive).
**Closed:** Yellow "Input Required" banner now refreshes on bulk `sessions` WS messages too.

## What's new

### Banner-on-waiting-input no longer needs an exit + re-enter

Operator: *"If I'm in a session and it ends, the yellow box with prompt details doesn't show up, i have to exit and re enter the session for it to display."*

Two WS message types carry session state:

- `session_state` — single-session update. Calls `updateSession()` → calls `refreshNeedsInputBanner()` if in session-detail view. ✓
- `sessions` — bulk replacement of `state.sessions`. Calls `onSessionsUpdated()` → which only called `updateSessionDetailButtons()` ✗ (no banner refresh).

When the daemon broadcasts a session transition into `waiting_input` AND the prompt-context comes through the bulk `sessions` push (which is what happens after a session-end transition's hub broadcast), the banner stayed hidden. Re-entering the session view ran the full `renderSessionDetail()` which built the banner from scratch — that's why exit + re-enter "fixed" it.

v5.26.49 closes the gap by adding `refreshNeedsInputBanner(state.activeSession)` to the session-detail branch of `onSessionsUpdated`:

```js
} else if (state.activeView === 'session-detail' && state.activeSession) {
  updateSessionDetailButtons(state.activeSession);
  refreshNeedsInputBanner(state.activeSession);   // ← v5.26.49
}
```

The same idempotent refresh that was already firing on `session_state` messages now also fires on bulk `sessions` pushes. No-op when not in waiting_input or when the operator has already dismissed the current banner round (the `state.needsInputDismissed` flag is checked inside `buildNeedsInputBannerHTML`).

### Why this works without breaking dismiss

`refreshNeedsInputBanner` already handles the dismiss invariant:

- If the session is in `waiting_input` AND `needsInputDismissed[id]` is `false` AND `prompt_context` is populated → render banner.
- If the session has transitioned out of `waiting_input` AND was dismissed → reset the dismissed flag (next prompt round shows again).
- Otherwise → `slot.innerHTML = ''` (no banner).

Calling it more often is safe because the function is idempotent. v5.26.49 just adds another trigger point.

## Configuration parity

No new config knob — UI fix.

## Tests

UI-only change. Manually validated:

```
1. Open a session (state=running) in PWA
2. Send it a task that will prompt for input
3. Wait for waiting_input transition
4. Verify: yellow Input Required banner appears immediately
   (no need to exit + re-enter the session view)
```

Go test suite unaffected: 465 passing. Smoke unaffected: 46/0/1.

## Known follow-ups

Same backlog as v5.26.48 — see `docs/plans/2026-04-27-v6-prep-backlog.md`.

## Upgrade path

```bash
git pull
datawatch restart
# Hard-reload the PWA (SW cache bumped to datawatch-v5-26-49).
# Open a session, wait for it to enter waiting_input — the
# yellow banner appears in-place.
```
