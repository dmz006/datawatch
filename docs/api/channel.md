# Channel API

The channel endpoints route messages between the operator (PWA / mobile companion / chat backends) and the LLM CLI side-car (`claude` MCP channel server, `opencode` ACP server, etc.). Every claude/opencode-acp session has a per-session channel; the daemon brokers messages over WS to the PWA and the mobile companion.

All endpoints live on the local daemon. The MCP channel server runs on a per-session port (default base 7433) and the daemon forwards `/api/channel/send` to it. Loopback POSTs to `/api/channel/*` from the channel server bypass the HTTP→HTTPS redirect (v5.18.0 fix).

## `POST /api/channel/reply`

Inbound message — the channel server pushes claude's text replies here so the daemon can broadcast them to the PWA and the mobile companion.

```json
{ "text": "...", "session_id": "host-id" }
```

Response: `{"status":"ok"}`. Side-effect: WS broadcast `MsgChannelReply` and append to the per-session ring buffer.

## `POST /api/channel/notify`

Inbound notification — same shape as `reply` but with a `subtype` (`permission`, `alert`, etc.) and `request_id`. Used by claude MCP for permission-relay prompts.

```json
{ "text": "...", "type": "permission", "request_id": "abc123" }
```

WS broadcast: `MsgChannelNotify`.

## `POST /api/channel/send`

Outbound message — the operator's input from the PWA / mobile companion gets forwarded to the channel server's `/send` endpoint.

```json
{ "text": "...", "session_id": "host-id" }
```

Looks up the session's `ChannelPort` for the per-session channel server; falls back to the global `server.channel_port` config. Returns `502 channel server unreachable` if the channel server is down.

## `POST /api/channel/ready`

Called by the MCP channel server once it has connected to claude. Marks the session as channel-ready, persists the channel port, broadcasts `MsgChannelReady`, and (if the session has a non-empty `Task`) forwards the task as the first outgoing message.

```json
{ "session_id": "host-id", "port": 7434 }
```

Response: `{"status":"ok|no_task|send_failed", ...}`.

## `GET /api/channel/history?session_id=<id>`

**Added in v5.26.1.** Per-session channel ring buffer (cap = 100 messages). The PWA Channel tab fetches this on session-detail open so the backlog is visible even when the operator opened a long-running session for the first time. datawatch-app stays connected continuously and didn't need this; the PWA was empty until v5.26.1.

```
GET /api/channel/history?session_id=host-id
```

Response:

```json
{
  "session_id": "host-id",
  "messages": [
    { "text": "...", "session_id": "host-id", "direction": "incoming",  "timestamp": "2026-04-27T10:00:00Z" },
    { "text": "...", "session_id": "host-id", "direction": "outgoing",  "timestamp": "2026-04-27T10:00:05Z" }
  ]
}
```

- Empty `session_id` → `400 session_id required`.
- Unknown `session_id` → `200` with `messages: []` (so the PWA can fetch unconditionally without surfacing a spurious error).
- Non-GET → `405`.

The buffer is in-memory only — daemon restart wipes it. Durable history is the responsibility of the chat-backend transcript (datawatch-app's ACP store, OpenWebUI's chat history, etc.).

## WS message types

| Type | When |
|------|------|
| `channel_reply` | Inbound claude → operator (or operator-typed `send` echoed back; `direction` field disambiguates) |
| `channel_notify` | Permission relay / alert from claude |
| `channel_ready` | Channel server connected |

## Direction values on `channel_reply`

| Value | Meaning |
|-------|---------|
| `incoming` | Claude → operator. Default. |
| `outgoing` | Operator → claude (echo from `/api/channel/send` and the initial-task delivery in `/api/channel/ready`). |
| `notify` | (notify channel only) — surfaced by the PWA as amber `channel-notify-line`. |

## Reachability

| Channel | Action | Notes |
|---------|--------|-------|
| REST | All endpoints above | |
| MCP | (no MCP tools — channel is a transport, not a feature surface) | |
| CLI | (no CLI subcommand — operator interacts via PWA / mobile / chat backends) | |
| PWA | Session detail → Channel tab | Auto-refreshes via WS; backlog seeded via `/api/channel/history` since v5.26.1 |
| Mobile companion | Session detail → Channel tab | Stays connected; doesn't need history fetch |
