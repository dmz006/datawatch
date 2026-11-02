# Release Notes — v6.11.17

Released: 2026-05-05
GH release: https://github.com/dmz006/datawatch/releases/tag/v6.11.17

### Summary

Channel-tab scroll-back. Operator: "It is working, but I can't scroll back in history."

### Fixed

`internal/server/web/app.js` — bumped `channelReplies[sessionId]` cap from 50 → 1000 entries. v6.11.16 routed every `output` WS line through `handleChannelReply`, so the 50-entry cap was dropping ~20 seconds of activity in claude-code sessions. 1000 entries cover ~5–10 minutes of dense output — enough scroll-back without unbounded growth. Same bump applied to the channel-history fetch merge.

### Note: running-vs-waiting detection from channel state

Operator asked: "are you using the state in the channel to detect if session is active or waiting or what the state is?"

Answer: **No, not yet.** I held that wiring in v6.11.14 pending operator clarification on which specific channel signal to use as the active/waiting indicator. Now that the channel tab is showing the full event stream (v6.11.16 + v6.11.17), point at the specific signal — particular line patterns, presence/absence of activity within an N-second window, specific markers — and I'll wire it into `internal/session/manager.go` state-detection.

### Tests

1767 pass.

### Mobile parity

Not needed — pure PWA cap bump.
