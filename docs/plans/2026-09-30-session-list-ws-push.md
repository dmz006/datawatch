# Session-list WS push for mobile clients (GH#162)

- **Date**: 2026-09-30
- **Version at planning**: v8.36.14
- **Status**: In Progress
- **Ships in**: v8.37.0 (minor — new capability, not a bug fix)

## 1. Context

`dmz006/datawatch#162` ("feat(ws): session-list subscription channel for
mobile clients") asks the server to add a WS channel so `datawatch-app` can
receive session-list push updates instead of REST-polling `GET
/api/sessions` every 5s. The issue proposes a new explicit subscribe
mechanism (`{"type":"subscribe","channel":"sessions"}`).

Direct investigation of `internal/server/ws.go` and `api.go` found the
requested capability **already exists in full, unconditionally**, for any
connected `/ws` client — no subscribe step needed:

- `handleWS` (`internal/server/api.go:1770-1806`) sends the full session
  list (`MsgSessions` / `"sessions"`) immediately on every new connection —
  exactly the issue's request #1.
- `Hub.BroadcastSessions` (`internal/server/ws.go:208-211`) is called from
  ~15 call sites across `api.go`/`server.go`/`session_reconcile.go` on every
  session-affecting state change, pushing the full list to every connected
  client — exactly the issue's request #2.
- `/ws` is registered behind the same `fedAuthMiddleware` Bearer-token auth
  REST already uses (`server.go:583`) — no mobile-specific barrier.

Cross-checked the client side (`datawatch-app`): `WebSocketTransport.kt`
already exists and is used for **per-session output** streaming (its own
`subscribe` frame for one session's output — a different, existing
mechanism). `PrdHub.kt` proves this exact "any open WS connection is a free
broadcast pipe" pattern is already understood and used client-side — its
own doc comment says so verbatim, for `prd_update` frames. There is no
`SessionsHub.kt` analog, and `WebSocketTransport.kt` never parses the
`"sessions"` frame type at all.

**Conclusion**: closing the issue's actual symptom (mobile REST-polls
instead of listening) is a `datawatch-app`-only change, tracked via a
cross-repo issue, not implemented here.

There IS one genuine, minor-release-worthy server gap directly relevant to
the issue's own stated concern ("Battery / data... WS push would eliminate
the gap... event-driven and quieter"): `BroadcastSessions` always sends the
**entire session list** on every change, regardless of which one session
changed. `MsgSessionState` (`"session_state"`) has been defined in the
protocol enum since it was written but was **never actually constructed or
broadcast anywhere** (confirmed via grep — zero non-definition matches).
This is the "diff" half of the issue's own suggested design that was never
built. Implementing it is scoped, low-risk, and directly serves the issue's
stated goal.

## 2. Scope

Server-side only (`dmz006/datawatch`). No `datawatch-app` code in this
change — tracked via a filed cross-repo issue (Phase 3).

## 3. Phases

**Phase 1 — implement `MsgSessionState` broadcasts.** Status: Done.
- `internal/server/ws.go`: `SessionStateData` struct + `Hub.BroadcastSessionState`.
- `internal/server/server.go`: `HTTPServer.NotifyStateChange` now calls it
  alongside the existing `BroadcastSessions` call — additive, not a
  replacement. No `cmd/datawatch/main.go` or `internal/session/manager.go`
  changes needed; `NotifyStateChange` is already the single choke point
  every session state transition passes through (confirmed call site:
  `cmd/datawatch/main.go:6290`, inside `mgr.SetStateChangeHandler`).

**Phase 2 — tests.** Status: Done.
- `internal/server/gh162_session_state_broadcast_test.go`: unit test
  asserting `BroadcastSessionState` produces a `session_state`-typed
  `WSMessage` carrying the single session, and that `NotifyStateChange`
  emits both messages (`sessions` and `session_state`) from one state
  transition. Full `internal/server` suite (483 tests) clean.

**Phase 3 — docs + parity.** Status: Done.
- `docs/howto/sessions-deep-dive.md`: new `### 5f. WebSocket push (mobile /
  external clients)` subsection (under `## Other channels`, alongside
  5a-5e) — connect to `/ws` with the same Bearer token as REST; server
  sends `"sessions"` on connect and on every change; `"session_state"`
  (new) sends single-session diffs; no subscribe frame needed for either.
- Filed `datawatch-app#204`: (a) the existing full-list push requires
  zero server changes and can be adopted today via a `SessionsHub`
  mirroring `PrdHub`, (b) the new `session_state` diff message, once
  shipped, as an optional lighter-weight alternative.

**Phase 4 — release.** Status: Planned.
- `CHANGELOG.md` entry, `Version` bump to `8.37.0` (both
  `cmd/datawatch/main.go` and `internal/server/api.go`), tag, push, CI
  watch, deploy.

## 4. Parity surface

| Surface | Status |
|---|---|
| REST | Not touched — capability is delivered entirely over the existing `/ws` endpoint; no new REST route. |
| MCP | Excluded — WS push is a long-lived stream, not representable as a request/response MCP tool call; `list_sessions` remains the poll-based equivalent. |
| CLI | Excluded — same reason as MCP; `datawatch sessions list` remains the poll-based equivalent. |
| comm channel | Excluded — comm channels are message-based, not long-lived streams. |
| YAML/config | Excluded — not a configurable toggle; the broadcast is unconditional protocol behavior, same as the pre-existing `sessions` broadcast (also not configurable). |
| PWA | Available, optional adoption — PWA already receives the full-list `sessions` broadcast unchanged; could adopt `session_state` for efficiency in a future PWA change, not required this release. |
| Android | Not in this change — `datawatch-app#204` covers both adopting the existing full-list push and the new diff message. |
| iPhone | Not in this change — `datawatch-app` is the single KMP client covering Android/iOS; `datawatch-app#204` applies once the iOS target implements session-list handling at all (capability parity per the Mobile-Parity Rule; no iOS-specific server work needed — same `/ws` endpoint). |

## 5. Verification

- `go test ./...` clean.
- Manual smoke: connect a raw WS client to the live daemon's `/ws` with a
  valid Bearer token, trigger a session state change, confirm a
  `session_state` frame arrives in addition to the existing `sessions`
  frame.
