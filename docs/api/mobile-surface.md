# Mobile API surface

This document is the contract between the datawatch daemon and the
[`datawatch-app`](https://github.com/dmz006/datawatch-app) mobile
client. Every endpoint listed here is mobile-friendly: stable
semantics, JSON request/response, no streaming-only operations.

The mobile client authenticates via the bearer token configured in
`server.token` and sends/receives JSON over HTTPS (operators expose
the daemon via `tls_enabled: true` or terminate TLS at a reverse
proxy).

---

## Mobile-specific endpoints (v3.0.0 baseline)

```
POST   /api/devices/register      # F17 — register an FCM/ntfy push token
GET    /api/devices               # F17 — list registered devices
DELETE /api/devices/{id}          # F17 — remove a registration
POST   /api/voice/transcribe      # F18 — Whisper transcription (multipart upload)
GET    /api/federation/sessions   # F19 — fan-out session list across remote servers
```

These are the original "datawatch-app v1.0.0 paired client" surface.

---

## v3.5.0–v3.7.x mobile-friendly additions

Every endpoint shipped in v3.5.0 → v3.7.2 returns JSON and is safe to
call from the mobile client. Inventory:

| Endpoint | Method(s) | Sprint | Use case in mobile |
|---|---|---|---|
| `/api/ask` | POST | S1 | Ask-an-LLM tile from any view |
| `/api/project/summary?dir=` | GET | S1 | Project overview drawer |
| `/api/templates` | GET, POST | S2 | Session-template picker on the new-session screen |
| `/api/templates/{name}` | GET, DELETE | S2 | Edit/delete templates from settings |
| `/api/projects` | GET, POST | S2 | Project alias manager |
| `/api/projects/{name}` | GET, DELETE | S2 | Per-project drawer |
| `/api/sessions/{id}/rollback` | POST | S2 | "Roll back" button on completed session card |
| `/api/cooldown` | GET, POST, DELETE | S2 | Status banner + manual clear |
| `/api/sessions/stale` | GET | S2 | Stale-session banner / pull-to-refresh |
| `/api/cost` | GET | S3 | Cost dashboard |
| `/api/cost?session={id}` | GET | S3 | Per-session cost detail |
| `/api/cost/usage` | POST | S3 | (typically server-side; mobile rarely needs) |
| `/api/cost/rates` | GET, PUT | S3 | Rate-table editor in settings |
| `/api/audit` | GET | S3 | Operator audit log viewer |
| `/api/diagnose` | GET | Operations (v3.4.0) | Settings → diagnostics |
| `/api/reload` | POST | Operations (v3.4.0) | Settings → "Reload config" button |
| `/api/analytics?range=Nd` | GET | Observability (v3.3.0) | Trends/charts view |

All payloads stay under 100 KB except `/api/voice/transcribe`
(multipart) and `/api/sessions/{id}/output` (paginated tail). No
WebSocket required for any of the new endpoints — they're all
request/response JSON.

---

## Authentication

```
Authorization: Bearer <server.token>
```

Set the token in datawatch config:
```yaml
server:
  token: <random-string>
```

The mobile app stores the token in OS keychain / iOS Keychain; never
in plain user storage.

---

## Versioning

The mobile app should send `User-Agent: datawatch-app/<version>` and
read the daemon version from `GET /api/info`. The daemon stays
backward-compatible within the 3.x line per the operator's versioning
rule (no 4.0 without explicit instruction).

When the mobile app needs to check whether a new endpoint exists
(e.g. `/api/cost` was added in v3.7.0), check `version >= "3.7.0"`
before calling. The daemon returns 404 for unknown paths.

---

## OpenAPI / schema

The full machine-readable spec lives at `/api/openapi.yaml`. The
mobile app should regenerate its client stubs from that file on each
release.

---

## Push notifications

Push delivery channel is configured per-device at registration time
(`platform: "fcm" | "ntfy"`). Datawatch sends push payloads via the
operator-configured FCM/ntfy backend. The mobile client receives:

- `session_state` — when a watched session changes state
- `alert` — operator-defined alerts (BL11 anomaly, BL30 cooldown, …)
- `audit` — when an audit-eligible action occurs (BL9; opt-in)

These are best-effort; the source of truth is always
`GET /api/sessions` + `GET /api/audit`.
