---
docs:
  index: true
  topics: [push, ntfy, notifications, unified-push]
exec_params:
  - name: topic
    description: "Push topic name (e.g. my-datawatch-alerts)"
    required: true
exec_steps:
  - tool: get_config
    description: Read current daemon config to check push/ntfy state
    args: {}
    read_only: true
  - tool: config_set
    description: Enable ntfy push notifications and set the topic
    args:
      key: ntfy.enabled
      value: "true"
    read_only: false
  - tool: config_set
    description: Set the push topic name
    args:
      key: ntfy.topic
      value: "{{params.topic}}"
    read_only: false
---
# How-to: Push notifications — ntfy and UnifiedPush

Datawatch ships a first-party HTTP SSE push provider. Any ntfy-compatible
app — or any UnifiedPush app that does auto-discovery — can subscribe to
daemon events without a third-party relay, FCM, or Google services.

## What it is

The daemon exposes two complementary push surfaces on the same HTTPS port:

**ntfy-compatible SSE stream** (`GET /api/push/<topic>`) — a
standard server-sent events stream whose JSON envelope matches the ntfy
message format. Point any ntfy client at the daemon URL and it works
out of the box; no ntfy.sh account required.

**UnifiedPush endpoint** (`POST /api/push/register` +
`GET /.well-known/unifiedpush`) — the standard UnifiedPush discovery
document lets compatible Android apps find and register with the daemon
automatically. Once registered the daemon POSTs events directly to the
app's push endpoint; no server-side relay needed.

Both surfaces share the same topic namespace and the same publish API.
The daemon auto-emits to both channels when a monitored event fires.

**Events that trigger a push:**

All session backends emit hook events automatically (v7.0+). Push fires
for any backend that uses the session state engine (claude-code,
opencode-acp, openwebui, ollama, council, autonomous workers).

| Event | Hook type | Topic(s) published |
|---|---|---|
| Session started | `Start` | `session-<id>` and `alerts` |
| Session waiting for input | `Stop` (→ waiting_input) | `session-<id>` and `alerts` |
| Session resumed (input sent) | `UserPromptSubmit` | `session-<id>` |
| Session completed / failed / killed | `Stop` (terminal) | `session-<id>` and `alerts` |
| Tool use inside session | `Activity` | `session-<id>` |

**Topic taxonomy:** Clients subscribe to `session-<short-id>` for
single-session streams, or `alerts` to receive every event across all
sessions.

## Base requirements

- `datawatch start` — daemon up and reachable at `https://<host>:<port>`.
- A push-capable client on the receiving end: the **ntfy app** (Android /
  iOS / desktop) pointing at the daemon URL, or any
  **UnifiedPush-compatible app** (e.g. Gotify-UP, FluffyChat, Element
  with UP support).
- The daemon must be reachable from the client over HTTPS — on the same
  LAN, over Tailscale, or through a reverse proxy. A self-signed cert
  works; ensure the client trusts it.

## Setup

### YAML block (`~/.datawatch/datawatch.yaml`)

```yaml
ntfy:
  enabled: true
  server_url: https://<your-daemon-host>:8443   # base URL of the daemon itself
  topic: my-datawatch-alerts                     # topic name; pick anything URL-safe
  token: ""                                      # optional: daemon bearer token for auth
```

`server_url` is the base URL the ntfy **client** uses when building the
subscribe URL. Set it to the public / LAN address of the daemon. The
daemon's own SSE endpoint is always `<server_url>/api/push/<topic>`.

Apply changes: `datawatch reload`.

### Topic namespace

Two built-in topics are always available once push is enabled:

| Topic | What triggers it |
|---|---|
| `alerts` | Catch-all — every auto-emitted event lands here. |
| `session-<id>` | Per-session events (waiting-input, etc.). |

You can publish to any topic name via the REST API or CLI (see below).

## Happy path — CLI

```sh
# 1. Enable push and set topic.
datawatch config set ntfy.enabled true
datawatch config set ntfy.topic my-datawatch-alerts
datawatch config set ntfy.server_url https://datawatch.local:8443

# 2. (Optional) set a bearer token so only authenticated clients subscribe.
datawatch secrets set DATAWATCH_PUSH_TOKEN "<random-string>"
datawatch config set ntfy.token '${secret:DATAWATCH_PUSH_TOKEN}'

# 3. Reload to apply.
datawatch reload

# 4. Verify push is active.
datawatch config show | grep -A4 ntfy
#  ntfy:
#    enabled: true
#    server_url: https://datawatch.local:8443
#    topic: my-datawatch-alerts

# 5. Send a test publish.
curl -sk -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"title":"Test","message":"Push is working"}' \
  https://datawatch.local:8443/api/push/my-datawatch-alerts
# → {"ok":true,"topic":"my-datawatch-alerts"}

# 6. Subscribe in a terminal to watch the raw SSE stream.
curl -sk -H "Authorization: Bearer $TOKEN" \
  https://datawatch.local:8443/api/push/my-datawatch-alerts
# data: {"id":"open-c-...","time":1746970...,"event":"open","topic":"my-datawatch-alerts","message":"stream open"}
# data: {"id":"dw-...","time":...,"event":"message","topic":"my-datawatch-alerts","title":"Test","message":"Push is working"}
# data: {"id":"ka-...","time":...,"event":"keepalive","topic":"my-datawatch-alerts"}
```

## Happy path — PWA

1. Open the PWA → **Settings** → **Comms** → **Communication
   Configuration** card.
2. Find the **ntfy** row. If not yet configured, click **Configure**.
3. Fill in the modal:
   - **Server URL**: `https://datawatch.local:8443` (the daemon URL,
     not `ntfy.sh` unless you are routing through ntfy.sh).
   - **Topic**: `my-datawatch-alerts`.
   - **Token** (optional): paste the bearer token if you set one.
4. **Save**. The ntfy row shows a toggle; flip it on.
5. A restart prompt appears — click **Restart now** or run
   `datawatch restart` from the CLI.

Screenshot needed: Settings → Comms → Communication Configuration card
with ntfy row enabled.

## Happy path — ntfy app (mobile)

1. Install the ntfy app (Android: F-Droid or Play Store; iOS: App Store).
2. Add a subscription:
   - **Server**: `https://datawatch.local:8443` (the daemon URL).
   - **Topic**: `my-datawatch-alerts`.
   - If you set a bearer token: tap **Credentials** → **Token** → paste it.
3. Tap **Subscribe**. The app connects and holds an SSE stream open.
4. Trigger an event (start a session that waits for input) — a push
   notification appears on the phone within a second.

Screenshot needed: ntfy app displaying a "waiting input" push notification
from datawatch.

## Happy path — UnifiedPush (Android)

Any app that supports UnifiedPush auto-discovery can find the daemon
without manual URL entry.

1. Ensure the daemon is reachable and `ntfy.enabled: true`.
2. In your UnifiedPush-compatible app, choose **Custom gateway** /
   **Self-hosted** as the push provider and enter the daemon URL
   (`https://datawatch.local:8443`).
3. The app fetches `GET /.well-known/unifiedpush` and discovers the
   register endpoint (`/api/push/register`) automatically.
4. The app POSTs its push endpoint to `/api/push/register` — the daemon
   stores it and fans out events to it on every publish.
5. From this point forward the daemon pushes directly to the app's
   registered HTTPS callback without any intermediary.

## REST endpoints

```sh
# Subscribe to an SSE topic stream (long-lived connection).
GET /api/push/<topic>
# → text/event-stream; each event is a JSON-encoded PushEvent on a "data:" line.
# → First event: {"event":"open",...}
# → Every 25 s: {"event":"keepalive",...} (keeps proxies alive)

# Publish a message to a topic.
POST /api/push/<topic>
# Body: {"title":"...","message":"...","priority":4,"tags":["..."],"click":"..."}
# Response: {"ok":true,"topic":"<topic>"}
# priority: 1=min 2=low 3=default 4=high 5=max (ntfy-compat)

# Register a UnifiedPush mobile endpoint (BL330).
POST /api/push/register
# Body: {"endpoint":"https://...","keys":{"p256dh":"<base64>","auth":"<base64>"}}
# Response: {"ok":true,"id":"reg-<nanosecond>"}

# Un-register a push endpoint.
DELETE /api/push/unregister
# Body: {"id":"reg-..."} OR {"endpoint":"https://..."}
# Response: {"ok":true}

# Send a push notification to all (or one) registered endpoint(s).
POST /api/push/notify
# Body: {"title":"...","message":"..."[,"id":"reg-..."]}
# Response: 202 Accepted

# UnifiedPush discovery document.
GET /.well-known/unifiedpush
# Response: {"version":1,"unifiedpush":{"gateway":"/api/push/notify"}}
```

All endpoints require a `Authorization: Bearer <token>` header if the
daemon has `server.token` set.

## MCP surface

```
push_subscribe   topic:<name>   → stream events to the MCP session
push_publish     topic:<name> title:<t> message:<m> priority:<1-5>
push_register    endpoint:<url> client_id:<id> [token:<t>]
```

## Comm channel verb

```
push test <topic>     — publish a test message to the given topic
push status           — list topics with active SSE subscriber counts
```

## Diagram

```
  ntfy app / UP app           Browser / terminal
      │                             │
      │  GET /api/push/<topic>      │  GET /api/push/<topic>
      │  (SSE, persistent)          │  (SSE, persistent)
      └──────────────┬──────────────┘
                     │
          ┌──────────▼────────────┐
          │   datawatch push hub  │
          │  (in-process fanout)  │
          └──────────┬────────────┘
                     │  PublishToTopic()
                     │
          ┌──────────▼────────────┐
          │  session manager /    │
          │  alert pipeline       │
          │  (waiting_input, etc.)│
          └───────────────────────┘

  UnifiedPush flow (POST fan-out):
  daemon ──POST──► registered endpoint (app's UP server)
```

## Common pitfalls

- **Trailing slash breaks the client URL.** The ntfy app builds the
  subscribe path by appending `/<topic>` to the server URL. If you set
  the server URL with a trailing slash (e.g. `https://host:8443/`) the
  result becomes `https://host:8443//topic` — a 404. Set the URL
  without a trailing slash.

- **SSE requires a persistent connection.** Many reverse proxies
  (nginx default, some load-balancers) impose short read/write timeouts
  (30–60 s) that silently drop the stream. The daemon sends a
  `keepalive` frame every 25 s; configure `proxy_read_timeout 3600s`
  (nginx) or equivalent. The `X-Accel-Buffering: no` header is already
  set by the daemon; ensure nginx honors it with `proxy_buffering off`.

- **Authentication mismatch.** If you set `ntfy.token` the SSE
  endpoint requires `Authorization: Bearer <token>`. The ntfy app
  supports this under Subscription → Credentials → Token; set it there
  or the subscription returns 401.

- **Topic not auto-emitting.** Auto-emit currently fires on
  `waiting_input` events only. Explicitly publish to any custom topic
  via `POST /api/push/<topic>` or `datawatch comm push test <topic>`.

- **Mobile app dropped the SSE connection.** The app will reconnect;
  events published while disconnected are not buffered server-side
  (best-effort fanout). For reliable delivery with UnifiedPush
  registration the daemon fans out to the app's registered HTTP
  endpoint instead, which is more resilient.

## Linked references

- See also: [`comm-channels.md`](comm-channels.md) — ntfy as a
  messaging backend (bidirectional) vs. push notifications (SSE emit only).
- See also: [`secrets-manager.md`](secrets-manager.md) — store push token
  with `${secret:DATAWATCH_PUSH_TOKEN}`.
- See also: [`daemon-operations.md`](daemon-operations.md) — `datawatch reload`.

---

## See also

- [howto/comm-channels](comm-channels.md)
- [howto/secrets-manager](secrets-manager.md)
- [howto/daemon-operations](daemon-operations.md)
- [architecture-overview](../architecture-overview.md)
