# datawatch v8.6.2 — Release Notes

**Released:** 2026-05-21
**Previous release:** v8.6.1 (2026-05-20)
**Type:** Patch — critical bug fix

---

## What v8.6.2 Is

v8.6.2 is a patch release that fixes a critical bug in session state management discovered during Android v1.0.0 field testing. When the mobile app (or any WebSocket client) subscribed to a waiting session, the session would cycle between `waiting_input` and `running` states every ~15 seconds, making the app appear unstable.

No breaking changes, no new features.

---

## Highlights

- **Session state stability fix** — WebSocket subscriptions no longer trigger spurious state transitions on waiting sessions.

---

## Bug Fixes

### Session state cycling on subscribe

When a mobile client subscribed to view a session in `waiting_input` state:

1. The screen capture handler would capture pane content on first tick (~200ms)
2. If content differed from initial state, `MarkChannelEvent(EventRunning)` was called
3. This transitioned the session to `running` and broadcast the state change
4. After 15 seconds, the gap watcher saw `Running` state with stale `LastChannelEventAt` and flipped it back to `waiting_input`
5. Cycle repeated every ~15 seconds

**Root cause:** The `firstTick` guard skipped state detection but not activity marking (`MarkChannelEvent`) on the initial capture.

**Fix:** Moved `MarkChannelEvent(EventRunning)` to after the `firstTick` guard. Prevents spurious activity signals from baseline captures when clients subscribe.

**Impact:** Resolves erratic state transitions in Android v1.0.0, Wear OS, and PWA when viewing waiting sessions. Session state now stable during subscription.

---

## Upgrade Notes

No migration required. Simply upgrade to v8.6.2 and restart the daemon.

If you're using the Android app v1.0.0, waiting sessions will now display stable state without cycling.
