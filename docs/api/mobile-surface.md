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

---

## File Service / Image Attachment (v8.19.0)

The file service API is stable and safe for mobile clients. Use it for image attachment in the session input bar (parity with the PWA 📷 button).

### Upload a file (image attachment)

```
POST /api/files
Content-Type: multipart/form-data
Authorization: Bearer <token>

fields:
  file   — binary content of the image
  path   — destination filename (e.g. "dw_attach_<timestamp>_photo.jpg")
```

**Response:**
```json
{ "path": "/home/user/dw_attach_1234567890_photo.jpg", "bytes": 204800 }
```

Use the returned `path` value. Append `[image:<path>]` to the outgoing session message text — the same format the PWA uses.

### Delete a file

```
DELETE /api/files
Content-Type: application/json
Authorization: Bearer <token>

{ "path": "/home/user/dw_attach_1234567890_photo.jpg" }
```

**Response:** `{ "deleted": "<path>" }`

### Authorization

Both endpoints require `CapConfigWrite` federation capability. Operator-level bearer token satisfies this; federation peers without write permission will receive 403.

### Android implementation notes

- Use `ActivityResultContracts.GetContent("image/*")` for gallery, `ActivityResultContracts.TakePicture` for camera (requires `CAMERA` permission with rationale dialog).
- Build `MultipartBody` with OkHttp or Ktor; field names match above (`file`, `path`).
- Show thumbnail chip above the send button while pending; clear on session switch or back nav.
- Tracked: [datawatch-app#158](https://github.com/dmz006/datawatch-app/issues/158)

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

For optional behavior on an existing endpoint (query parameters,
WebSocket modes, compression), prefer the `capabilities` array in
`GET /api/info` over version comparisons — see
[Low-bandwidth client flow](#low-bandwidth-client-flow).

---

## Low-bandwidth client flow

A phone that only shows a session list should not download every
session's full record and every terminal's output. The daemon offers a
lighter path for each of the heavy streams. Use each one only when
`GET /api/info` advertises it, so the same client keeps working against
an older daemon.

### 1. Check capabilities

```
GET /api/info
→ { "version": "...", "capabilities": ["sessions.view_summary", "alerts.paging",
     "alerts.count", "ws.alerts_read", "ws.hello", "ws.session_list_delta",
     "ws.session_view_summary", "ws.output_scoped", "ws.unsubscribe", "api.gzip"], ... }
```

Treat a missing `capabilities` array as an empty one. Capability strings
are never renamed; new features add new strings.

### 2. Send `Accept-Encoding: gzip` on every request

With `server.api_gzip` on (the default; advertised as `api.gzip`), any
`/api/*` JSON response larger than `server.api_gzip_min_bytes` (default
1024 bytes) comes back with `Content-Encoding: gzip`. OkHttp and
URLSession add the header and decompress transparently; a hand-built
client must check `Content-Encoding` before parsing. Small responses
are never compressed.

### 3. Open the WebSocket and send `hello`

```json
{"type":"hello","data":{"session_list":"delta","session_view":"summary","output":"subscribed"}}
```

The daemon answers with the modes now in effect:

```json
{"type":"hello_ack","data":{"session_list":"delta","session_view":"summary","output":"subscribed","capabilities":["ws.hello","ws.session_list_delta","ws.session_view_summary","ws.output_scoped","ws.unsubscribe"]}}
```

- `session_list: delta` — no whole-list `sessions` frames. Each new or
  changed session arrives as one `session_state` `{"session": {...}}`;
  each removed session as one `session_removed` `{"session_id": "..."}`.
- `session_view: summary` — `session_state` carries the compact record
  (long text fields truncated, `truncated_fields` lists which).
- `output: subscribed` — `output`, `raw_output`, `pane_capture` and
  `chat_message` frames arrive only for sessions you `subscribe` to.

Send only the keys you want; each is independent. Frames that arrive
before `hello_ack` can be ignored.

### 4. Load the initial list over REST

Right after `hello_ack`:

```
GET /api/sessions?view=summary
```

This returns the compact list: long text fields cut to
`server.session_summary_text_chars` characters, and only the newest
`server.session_summary_terminal_limit` ended sessions (the response
header `X-Datawatch-Sessions-Omitted` says how many older ones were
left out). Add `states=active` for running/waiting sessions only, or
`since=<RFC3339|unix>` to fetch only what changed after a reconnect.
Fetch `GET /api/sessions/{id}` when the user opens a session and needs
the full record. Contract: [sessions.md](sessions.md#compact-session-list-viewsummary).

### 5. Apply the stream

- `session_state` → replace (or insert) the session keyed by `full_id`.
- `session_removed` → drop that session.
- On reconnect, send `hello` again and refetch with `since=` set to the
  last `updated_at` you saw (or refetch the whole summary list).

### 6. Subscribe only while a terminal is open

```json
{"type":"subscribe","data":{"session_id":"myhost-a1b2"}}
{"type":"unsubscribe","data":{"session_id":"myhost-a1b2"}}
```

Send `subscribe` when the session's terminal view opens and
`unsubscribe` when it closes. `unsubscribe` also stops the screen
capture the subscription started. Full and short session IDs both work.

### 7. Alerts without polling

- Badge: `GET /api/alerts/count` → `{"unread_count": N, "total": M}`
  once at start-up.
- Updates: the `alert` WS event carries each new alert; `alerts_read`
  (`{"ids": [...], "unread_count": N}` or `{"all": true,
  "unread_count": 0}`) arrives whenever alerts are marked read on any
  client. Update the badge from these instead of polling.
- List screen: `GET /api/alerts?limit=20` (add `unread=1` for unread
  only) and page with `before=<next_before>` while `has_more` is true.

### Fallback on an older daemon

| Capability missing | Do this instead |
|---|---|
| `sessions.view_summary` | `GET /api/sessions` (full list; the params are ignored by older daemons, so check before relying on truncation or the omitted header). |
| `ws.hello` | Do not send `hello`; expect the full `sessions` frame on connect and on every change. |
| `ws.session_list_delta` / `ws.session_view_summary` | Keep handling `sessions` frames and full `session_state` records. `hello_ack` echoes what was actually applied, so trust it over what you asked for. |
| `ws.output_scoped` | Expect output for every session; drop frames for sessions you have not opened. |
| `ws.unsubscribe` | Skip `unsubscribe`; the subscription ends when the socket closes. |
| `alerts.count` / `alerts.paging` | `GET /api/alerts` and count unread client-side. |
| `ws.alerts_read` | Refetch the badge after your own mark-read calls; changes from other clients appear on the next refresh. |
| `api.gzip` | Nothing to change — the header is harmless; responses are just uncompressed. |

Operators can tune these defaults (`server.ws_session_list_default`,
`server.ws_output_scope_default`, the summary limits and the gzip
threshold) — see [operations.md](../operations.md#payload-settings-for-low-bandwidth-clients).

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
