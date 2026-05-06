# datawatch v5.26.1 — release notes

**Date:** 2026-04-27
**Spans:** v5.26.0 → v5.26.1
**Patch release** (no binaries / containers — operator directive: every release until v6.0 is a patch).
**Closed:** New PRD configured-only backends + model dropdown, PWA Channel tab history, howto README relative links

## What's new

### New PRD modal — configured backends only, model dropdown follows backend

Operator: *"In new prd, the llm backend should only show what is configured, and then model should list what is available, if list isn't available then the model selector should be hidden."*

- `renderBackendSelect` now filters `b.enabled === false` — disabled or unconfigured LLM backends are hidden from the Backend dropdown in the New PRD modal. Pre-v5.26.1 every registered backend showed up regardless of whether it had credentials/endpoints.
- `openPRDCreateModal` pre-fetches `/api/ollama/models` + `/api/openwebui/models` into `state._availableModels` on open (best-effort — failures leave the cache empty rather than blocking the modal).
- New `updatePRDNewModelField(backend)` helper toggles the **Model** field based on the selected backend:
  - Has a populated list → Model field is shown as a `<select>` of available model names.
  - No list (backend offline, no credentials, claude-code, gemini-via-CLI, etc.) → Model field is hidden entirely.
  - Backend changed → list re-resolves from the cache; no extra fetch.

This means the New PRD modal now matches what's actually runnable. No more typing a model name into a free-form box for a backend that has no endpoint configured.

### PWA Channel tab — backlog seeded from the daemon

Operator: *"In datawatch-app channel tab shows activity, in pwa i don't see anything in channel tab."*

The PWA Channel tab was empty for any session that had been chatting before the operator opened it — channel messages are broadcast over WS, but a fresh page load missed every prior message. datawatch-app stays connected so it always had history; the PWA didn't.

v5.26.1 adds a per-session ring buffer on the daemon and a fetch on session-detail open:

- New `Server.channelHist` (`map[string][]channelHistEntry`) keeps the last `channelHistoryMax` (=100) messages per session FullID. Mutations go through `recordChannelHistory(sessionID, text, direction)`.
- Every existing channel handler that broadcasts over WS now also records to the buffer:
  - `BroadcastChannelReply` (used by opencode-acp + claude MCP)
  - `handleChannelReply` (`POST /api/channel/reply`)
  - `handleChannelReady` (the initial outgoing task delivery)
  - `handleChannelSend` (`POST /api/channel/send` — operator-typed input)
- New `GET /api/channel/history?session_id=...` returns `{session_id, messages: [{text, session_id, direction, timestamp}, …]}`. Empty session-id → 400; unknown session-id → 200 with empty list (so the PWA can fetch unconditionally).
- PWA `renderSessionDetail` fetches the history once per session-id, merges with any messages already in `state.channelReplies[sessionId]` (de-dup by `ts|text`), sorts by timestamp, caps at 50, and re-renders. Subsequent renders skip the fetch via `state._channelHistoryLoaded[sessionId]`.

Rule consequence: the buffer is in-memory only — daemon restart wipes history. That's intentional (the file backing would race with the existing per-channel-server state). For durable history, datawatch-app's ACP transcript already covers it.

### Howto README links work from the diagrams viewer

Operator: *"Howto readme links don't work, if i go to individual howto they work, just not links from readme."*

`docs/howto/README.md` uses relative links like `[Setup + install](setup-and-install.md)`. When marked.js renders that into the diagrams viewer at `/diagrams.html`, the browser resolves the link against the page URL — so it would hit `https://localhost:8443/setup-and-install.md` and 404. Direct links worked because the URL hash already pointed at `docs/howto/<file>.md`.

v5.26.1 adds a post-render link rewriter:

- After `marked.parse` injects HTML into the prose container, walk every `<a href>`.
- Skip absolute URLs (`http://`, `https://`, `mailto:`), in-page hashes, the GitHub-footer link.
- For relative `.md` links, resolve against the current doc's directory (e.g. `docs/howto/` for the README) using a `..`-aware path resolver.
- Rewrite the `href` to `#docs/howto/setup-and-install.md` and bind a click handler that updates `location.hash` — that triggers `openFromHash` and the viewer loads the doc through the same code path the sidebar uses.

The fix is general: it works for any relative `.md` link in any rendered doc, so the README's `../setup.md` and any cross-howto links also start working.

## Configuration parity

No new config knob. The new endpoint is read-only (GET).

## Tests

1392 passing (1390 baseline + 2 new):

- `TestRecordChannelHistoryRingBuffer` — cap honored, empty session-id dropped.
- `TestHandleChannelHistory` — missing `session_id` → 400, POST → 405, happy path returns ordered messages, unknown session-id → 200 + empty list.

(One pre-existing flaky test, `TestClusterProfiles_Roundtrip`, intermittently fails on a 422 response from the k8s smoke check; passes on re-run. Unrelated to v5.26.1.)

## Known follow-ups

- Design doc audit / refresh — `docs/design.md` + `docs/architecture.md` + `docs/architecture-overview.md`
- datawatch-app#10 catch-up issue
- Container parent-full retag
- GHCR container image cleanup
- gosec HIGH-severity review
- Channel history persistence (currently in-memory only, lost on daemon restart)

## Upgrade path

```bash
datawatch update                        # check + install
datawatch restart                       # apply
# New PRD: only configured backends; Model field follows backend
# Session detail: Channel tab shows backlog on open
# /diagrams.html → How-tos → README: links resolve correctly
```
