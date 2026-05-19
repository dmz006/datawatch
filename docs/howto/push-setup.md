---
docs:
  index: true
  topics: [push, unified-push, android, registration, notify, bl330]
exec_params:
  - name: endpoint
    description: "Push endpoint URL to register (e.g. https://up.example.com/UP?token=...)"
    required: true
exec_steps:
  - tool: get_config
    description: Verify push is reachable
    args: {}
    read_only: true
---
# How-to: UnifiedPush registration API (BL330)

v8.2.0 adds three new push REST endpoints that complete the UnifiedPush
server-side contract. The **datawatch Android app** and any
UnifiedPush-compatible client can register, receive fan-out messages, and
deregister — all without a third-party relay.

For the ntfy SSE setup (topic subscribe/publish) see
[`push-notifications.md`](push-notifications.md). This howto covers
**registration**, **unregistration**, and the **notify fan-out endpoint**.

## Endpoints added in BL330

| Method | Path | Purpose |
|---|---|---|
| `POST` | `/api/push/register` | Register a push endpoint |
| `DELETE` | `/api/push/unregister` | Remove a registration |
| `POST` | `/api/push/notify` | Fan out to all (or one) registered endpoint |
| `GET` | `/.well-known/unifiedpush` | Discovery document |

## Discovery

```sh
curl -sk https://datawatch.local:8443/.well-known/unifiedpush
# {"version":1,"unifiedpush":{"gateway":"/api/push/notify"}}
```

UnifiedPush clients fetch this document to find the server's registration
and notify endpoints. No manual URL configuration needed in compliant apps.

## Register

```sh
# Register a push endpoint with encryption keys.
curl -sk -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "endpoint": "https://up.ntfy.example.com/UP?token=abc123",
    "keys": {
      "p256dh": "<base64url-encoded-ECDH-public-key>",
      "auth": "<base64url-encoded-auth-secret>"
    }
  }' \
  https://datawatch.local:8443/api/push/register
# {"ok":true,"id":"reg-1716150012345678901"}
```

Save the returned `id` — you need it to unregister or to target a specific
device with `/api/push/notify`.

**`keys` field**: If the push endpoint does not use Web Push encryption
(e.g. a plain HTTP callback), omit `keys` entirely. The daemon stores the
endpoint as-is and POSTs raw payloads.

## List registrations

```sh
# GET returns all current registrations.
curl -sk \
  -H "Authorization: Bearer $TOKEN" \
  https://datawatch.local:8443/api/push/register
# [{"id":"reg-...","endpoint":"https://...","registered_at":"..."}]
```

## Unregister

```sh
# By registration ID.
curl -sk -X DELETE \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"id":"reg-1716150012345678901"}' \
  https://datawatch.local:8443/api/push/unregister
# {"ok":true}

# By endpoint URL (if ID was lost).
curl -sk -X DELETE \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"endpoint":"https://up.ntfy.example.com/UP?token=abc123"}' \
  https://datawatch.local:8443/api/push/unregister
# {"ok":true}
```

## Fan-out notify

```sh
# Send to ALL registered endpoints.
curl -sk -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"title":"Session complete","message":"Task finished successfully"}' \
  https://datawatch.local:8443/api/push/notify
# 202 Accepted

# Send to ONE registration by ID.
curl -sk -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"registration_id":"reg-...","title":"Alert","message":"Session waiting for input"}' \
  https://datawatch.local:8443/api/push/notify
# 202 Accepted
```

The daemon POSTs the payload to each registered endpoint's URL. Failures
are logged but do not block the response (best-effort delivery).

## CLI

```sh
# List registered push endpoints.
datawatch push list

# Send a test notification.
datawatch push test --message "Hello from datawatch"

# Target a specific registration.
datawatch push test --id reg-1716150012345678901 --message "Device-specific test"

# Unregister by endpoint URL.
datawatch push unregister --endpoint "https://up.example.com/UP?token=..."

# Unregister by ID.
datawatch push unregister --id reg-1716150012345678901
```

## PWA

The PWA Settings → Comms → Push Notifications card (v8.2.0) shows:
- Current registration status (registered / not registered).
- A **Register** button that calls the browser's Push API and POSTs
  to `/api/push/register` with the subscription's `endpoint` and `keys`.
- A **Test** button that calls `POST /api/push/notify`.
- A **Unregister** button that calls `DELETE /api/push/unregister`.

## Android app integration

The Android `UnifiedPushSseService` implements this flow:

1. On app start (or push provider change): fetch `/.well-known/unifiedpush`
   to discover the gateway URL.
2. Call `POST /api/push/register` with the UP endpoint URL and encryption
   keys from the UP library.
3. Store the returned `id` in SharedPreferences for unregistration.
4. On app uninstall / user sign-out: call `DELETE /api/push/unregister`
   with the stored `id`.
5. The daemon fans out to the registered URL whenever a monitored event
   fires (session waiting-input, session complete, alert threshold).

## Common pitfalls

- **401 on registration.** All push endpoints require `Authorization: Bearer
  <token>` when `server.token` is set. Ensure the Android app sends the token.
- **Registration lost after daemon restart.** Registrations are in-memory.
  The Android app should re-register on connection loss or after a
  daemon restart (monitor the daemon version via `/api/version` and
  re-register when it changes).
- **Endpoint URL must be HTTPS.** The daemon will POST to the registered
  URL; ensure it is reachable over HTTPS. Plain HTTP endpoints are accepted
  for local testing but not recommended in production.

## Linked references

- See also: [`push-notifications.md`](push-notifications.md) — ntfy SSE
  setup, topic subscribe/publish, UnifiedPush auto-discovery overview.
- See also: [`comm-channels.md`](comm-channels.md) — bidirectional comms
  channels vs. push notifications.
- See also: [`secrets-manager.md`](secrets-manager.md) — store API token
  with `${secret:DATAWATCH_TOKEN}`.

---

## See also

- [howto/push-notifications](push-notifications.md)
- [howto/comm-channels](comm-channels.md)
- [howto/daemon-operations](daemon-operations.md)
