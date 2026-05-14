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

---

## Guardrail APIs (Automata surface)

Mobile and Auto surfaces can access the guardrail library and profile
management endpoints under `/api/autonomous/guardrails*`:

| Endpoint | Method | Purpose |
|---|---|---|
| `/api/autonomous/guardrails` | GET | List guardrail library |
| `/api/autonomous/guardrail_profiles` | GET | List profiles |
| `/api/autonomous/guardrail_profiles` | POST | Create profile |
| `/api/autonomous/guardrail_profiles/{id}` | GET | Get one profile |
| `/api/autonomous/guardrail_profiles/{id}` | PUT | Update profile |
| `/api/autonomous/guardrail_profiles/{id}` | DELETE | Delete profile |
| `/api/autonomous/prds/{id}/guardrails` | PUT | Set per-Automaton override |

Per-story approval payloads include `guardrail_verdicts` so Wear/Auto
surfaces can surface block reasons without polling full telemetry.

## Session Guardrail + Live Task Tree APIs (S3)

On-demand guardrail invocation for a session and real-time Status tab data:

| Endpoint | Method | Purpose |
|---|---|---|
| `/api/sessions/{id}/guardrail` | POST | Run a named guardrail on a session's project dir; appends verdict to telemetry |
| `/api/sessions/{id}/telemetry` | GET | Structured telemetry: task list with timings, guardrail verdicts, sprint ancestry |
| `/api/sessions/{id}/status` | GET | Derived status board (current focus, tests, git, hook health) |

**POST /api/sessions/{id}/guardrail body:**
```json
{"name": "sast-scan"}
```

**Response:**
```json
{
  "guardrail": "sast-scan",
  "outcome": "pass",
  "summary": "0 finding(s)",
  "verdict_at": "2026-05-13T12:34:56Z"
}
```

**WebSocket `hook_update` event** — broadcast whenever a hook event or guardrail verdict is recorded:
```json
{
  "type": "hook_update",
  "data": {
    "session_id": "myhost-abc123",
    "board": { "hook_health": "alive", "state": "running", "telemetry": {...} }
  }
}
```

All three endpoints and the `hook_update` WS event are stable for Android/Wear/Auto consumption.

---

## Dashboard APIs (S4)

The `/dashboard` view is PWA-only; mobile/Wear/Auto consume the same backend
events and APIs that the dashboard visualises.

**Node data for constellation equivalents (e.g. Wear summary tiles):**

Each connected session surfaces its state via the existing `session_state` WS
event:
```json
{
  "type": "session_state",
  "data": {
    "full_id": "myhost-abc123",
    "state": "running",
    "name": "auth-refactor",
    "hook_health": "alive"
  }
}
```

**Sprint pipeline data:**
```
GET /api/autonomous/prds
```
Returns the full list with `stories[].status` fields; filter for
`status ∈ {running, blocked, planning}` to build pipeline equivalents.

**Dashboard expand data (three-pane equivalent for mobile detail screens):**

| Pane | Source |
|------|--------|
| Task tree | `hook_update.board.telemetry.tasks[]` |
| Status board | `GET /api/sessions/{id}/status` or `hook_update.board` |
| Verdicts | `hook_update.board.telemetry.guardrail_verdicts[]` |

All APIs confirmed stable — documented in this file for Android/Wear/Auto integration.

---

<!-- BL279 see-also footer -->
## See also

- [datawatch-definitions](../datawatch-definitions.md)
- [architecture-overview](../architecture-overview.md)
- [howto/guardrail-library](../howto/guardrail-library.md)
- [howto/dashboard](../howto/dashboard.md)
