# Release Notes — v6.11.23

Released: 2026-05-05
GH release: https://github.com/dmz006/datawatch/releases/tag/v6.11.23

### Summary

Three follow-up fixes:

1. **Splash never dismissed for sessions with no recent output** — both `CapturePaneANSI` and `TailOutput` could return empty (brand-new session, just-spawned LLM), and v6.11.22's fallback only fired when one had content. Now subscribe always emits at least one frame (single-space placeholder) so the splash always clears.
2. **Channel completion detection missed multi-sentence wrap-ups** — v6.11.20's whole-message-suffix was too strict: typical claude-code closes look like *"I've completed the task. Here's what changed. Let me know if anything else."* — the completion phrase is the suffix of an early sentence, not the message as a whole. Switched to per-sentence suffix matching, with the mid-sentence false-positive safety preserved.
3. **Stats tab read from wrong source** — the WS `stats` event broadcasts `SystemStats`, not envelopes. Per-session envelopes live at `/api/observer/envelopes` (kind `session`). The tab now fetches that endpoint, prefers the matching `session:` envelope, falls back to the session's `backend:` envelope (e.g. `backend:claude-docker`) when no per-session envelope exists, and explains why if neither exists. Polls every 5 s while the tab is open.

### Fixed

- **`internal/session/manager.go` `detectChannelStateSignal`** — split message on `.!?` and run HasSuffix per sentence instead of on the whole message; added `splitSentencesForChannelClassifier` helper. Added patterns: `completed the task`, `finished the task`, `work is done`, `work is complete`.
- **`internal/server/api.go` subscribe handler** — always emit a `pane_capture` (single space if everything else is empty) so the loading splash clears even on brand-new sessions.
- **`internal/server/web/app.js` `renderSessionStats`** — fetch `/api/observer/envelopes`, prefer `session:` envelope, fall back to `backend:` envelope, poll every 5 s. Empty-state explains the actual cause (SessionAttribution off / containerised LLM).
- **`internal/server/web/locales/{en,de,es,fr,ja}.json`** — added `session_stats_backend_title`, `session_stats_no_envelope_title`, `session_stats_no_envelope_body`, `loading`.

### Mobile parity

[`datawatch-app#72`](https://github.com/dmz006/datawatch-app/issues/72) — apply the same per-sentence channel-classifier change on the Compose Multiplatform side.
