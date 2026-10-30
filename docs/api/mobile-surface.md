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
     "ws.session_list_window", "ws.session_view_summary", "ws.output_scoped",
     "ws.unsubscribe", "api.gzip"], ... }
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

#### Window form: `session_list: "window"`

Capability `ws.session_list_window`. This is the bounded form and the one
a phone list should use. The daemon sends only the default view, as compact
rows, and after that only the sessions that change.

The default view is every session that is not finished, plus each finished
session (complete, failed or killed) updated within the window. The window is
the daemon's `server.recent_session_minutes` setting (a value of 0 or less
means 5). The daemon echoes it as `window_minutes` on every list frame, so read
it there and never hard-code it.

```json
{"type":"hello","data":{"session_list":"window","session_view":"summary"}}
```

The daemon answers with `hello_ack` (`session_list` is `window`), then sends
exactly one `sessions` frame:

```json
{"type":"sessions","data":{"sessions":[{"id":"...","full_id":"...","state":"running","updated_at":"..."}, ...],"version":"...","window_minutes":5,"total":4000,"more":true},"ts":"..."}
```

- `data.sessions` — summary rows, the same shape as
  `GET /api/sessions?view=summary` (with `scheduled_count` where the schedule
  store is wired). A row has no `task`; see the merge rules below.
- `data.window_minutes` — the window this list is built on.
- `data.total` — how many sessions the daemon holds, inside the window or not.
- `data.more` — `true` when the daemon holds sessions this frame leaves out
  (finished and older than the window). A field the daemon omits means `0` or
  `false`.

After that frame the socket sends no `sessions` frame at all. Each change is
one `session_state` `{"session": {...}}` for the one session that changed:
merge it by `full_id`, and add it if it is new to the window.

`session_removed` `{"session_id": "..."}` is sent only when a session is
deleted. A session that ages out of the window gets no frame; see *Aging*
below.

**Frames before `hello_ack`.** The socket is open before you can send
`hello`, so the first frame on a new connection is the legacy bounded
`sessions` frame (described below). Treat it as provisional. The rows frame
that follows `hello_ack` replaces it. Sending `hello` again gets a fresh
`hello_ack` and a fresh rows frame, which replaces your list.

**Merge rules** (the PWA follows these):

- A row without `task` keeps the `task`, `last_response`, `input_mode`,
  `channel_ready` and the other fields you already hold for that session.
- A summary key the row omits is empty now: clear it, do not keep the old
  value. The summary keys are `id`, `full_id`, `name`, `state`, `backend`,
  `llm_ref`, `project_dir`, `hostname`, `created_at`, `updated_at`,
  `last_activity`, `parent_id`, `est_cost_usd`, `tokens_in`, `tokens_out`,
  `status_line` and `truncated_fields`.
- A text field named in `truncated_fields` was cut. If you hold the full text
  for the same session, keep it. Otherwise show the cut text and fetch
  `GET /api/sessions/{id}` when the user opens the session.
- A full record (one that carries `task`) replaces the held record.

**Aging.** The daemon never sends a removal because a finished session left
the window. A rows frame is the whole default view, so a held session it omits
and that you did not fetch over REST has left the view: drop it. Between
frames you may also drop a finished session whose `updated_at` is older than
`window_minutes`, so your list matches the daemon's.

**History.** Finished sessions older than the window never come over the
WebSocket, to any client mode. Fetch them over REST, with cursor paging:

```
GET /api/sessions?view=summary&limit=50
GET /api/sessions?view=summary&limit=50&cursor=<next_cursor>
GET /api/sessions?view=summary&limit=50&history=1&cursor=<next_cursor>
```

Repeat with each `next_cursor` until the response has none. `history=1` adds
the archived sessions; send the same parameters on every page. A single
session's full record is `GET /api/sessions/{id}`.

**Legacy bounded frame.** A client that does not announce `window` (no
`hello`, or `session_list` `full`) still gets a `sessions` frame on connect and
on every change. It is bounded: only the default-view sessions, long text cut
to `server.session_summary_text_chars` with `truncated_fields` set, and
`more`, `total` and `window_minutes` alongside. Its `session_state` frames stay
full records, for default-view sessions only. Get the full text of a cut field
with `GET /api/sessions/{id}`, and page history over REST as above.

**Adopting the window form, step by step:**

1. Read `capabilities` from `GET /api/info`. Use the window form only when
   both `ws.hello` and `ws.session_list_window` are present.
2. Open `/ws` with the Bearer token, as today.
3. Send `hello` with `"session_list":"window"` and your `session_view`.
4. Wait for `hello_ack`. If its `session_list` is not `window`, keep the
   legacy handling.
5. On the next `sessions` frame, replace the default-view list with its rows
   using the merge rules. Read `window_minutes`, `total` and `more` from the
   frame. Do not fetch the initial list over REST.
6. Apply each `session_state` (merge by `full_id`) and each `session_removed`
   (drop by `session_id`).
7. Age finished rows out locally, as described under *Aging*.
8. Page history over REST when the user asks for it (`limit=50`, `cursor`,
   `history=1`). The socket never carries history.
9. On reconnect, send `hello` again and apply the new rows frame, which
   replaces your list.

### 4. Load the initial list over REST

A `delta` client loads the list here. A `window` client does not: its list
comes in the rows frame (see the window form above), and it fetches history
only.

Right after `hello_ack` (`delta` clients):

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
- `sessions` (window rows frame) → replace the default-view list, using the
  merge rules in the window form above.
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
| `ws.session_list_window` | Do not send `session_list: "window"`. Use `delta` when `ws.session_list_delta` is present (load the list over REST, step 4), otherwise keep the full handling. Where a daemon sends the bounded legacy frame, read `more`, `total` and `window_minutes` when present, and page history over REST. |
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

## Triage records (Automata surface)

The triage records of the failed and blocked Automata are read from
`GET /api/autonomous/triage` — the same contract the PWA's triage card and the CLI, MCP and comm surfaces use. It is a stable read for Android/Wear/Auto:

| Endpoint | Method | Purpose |
|---|---|---|
| `/api/autonomous/triage` | GET | List triage records, newest first, optionally filtered |

Optional query params (combinable): `automation_id`, `prd_id`, `status` (`failed`\|`blocked` only; anything else is refused).

Response body — `records` plus the alert threshold in force (`alert_minutes`, the
`autonomous.automation.triage_alert_minutes` setting, default 30, `0` = off):

```json
{
  "records": [
    {
      "prd_id": "d0442737d38c",
      "prd_title": "nightly ingest",
      "automation_id": "4522f306c266",
      "automation_name": "nightly pipeline",
      "member_id": "member-2",
      "status": "failed",
      "task_id": "task-9",
      "task_title": "run integration tests",
      "error": "connection refused: testdb:5432 (truncated at 2000)",
      "blocked_reason": "waiting on operator decision",
      "session_end_reason": "runtime error: exit 2 (truncated at 2000)",
      "first_seen_at": "2026-10-27T09:12:44Z",
      "updated_at": "2026-10-27T09:44:02Z",
      "alerted_at": "2026-10-27T09:44:02Z",
      "retry_budget": {
        "infra":   { "limit": 3, "used": 1, "left": 2 },
        "content": { "limit": 4, "used": 4, "left": 0 },
        "scope":   { "limit": 2, "used": 0, "left": 2 },
        "policy":  { "limit": 1, "used": 0, "left": 1 },
        "ceiling_total": 10,
        "ceiling_left": 5
      },
      "notes": [
        { "at": "2026-10-27T09:12:44Z", "source": "triage", "text": "Automaton failed on task 'run integration tests'" }
      ]
    }
  ],
  "alert_minutes": 30
}
```

Shape notes for the app team:

- A record is a **read** of the engine's state: it is filled and refreshed by the
  daemon on every coordinator tick while the Automaton stays failed or blocked, and
  removed again when it leaves that state. Polling sees new records appear and old
  ones disappear.
- `retry_budget` is informational (the engine's existing budget for the four
  categories: infra, content, scope, policy, each with `limit`, `used`, `left`;
  plus `ceiling_total` and `ceiling_left`, the overall auto-retry ceiling —
  `ceiling_total` is `0` when there is none).
- `alerted_at` is absent until the triage alert for the episode has fired.
- The strings (`error`, `session_end_reason`) are already truncated to
  2000 chars by the daemon; treat them as hostile text and render sanitized.
- The optional fields (`prd_title`, `automation_id`, `automation_name`, `member_id`,
  `task_id`, `task_title`, `error`, `blocked_reason`, `session_end_reason`,
  `alerted_at`, `notes`) are simply absent when empty — a failed record normally
  carries the task fields and not `blocked_reason`, a blocked record the reverse.
- `alert_minutes` rides the list body beside `records`, not on each record.
- The record card on a client displays, in order: the Automaton name (title, or
  id), the status chip (failed/blocked), the owning Automation name with its
  member id when one exists, the failed task title, the task error, the block
  reason, the session end reason, the first-seen time, the alert state (when it
  fired, or that it has not fired yet), one row per retry-budget category (used
  of limit, left), and the overall ceiling when one is set; values the record
  does not carry are simply not shown (this is what the PWA's triage card renders
  from the same contract).
- A read that matches nothing is `records: []` with the same HTTP 200 — not an error.
- Access follows the daemon's rules: bearer token locally, the autonomous read
  capability for federated peers. The same endpoint is listed in the full OpenAPI
  spec (`TriageList`/`TriageRecord` schemas).

## Merge state and safe-to-delete report (Automata surface)

Whether the work branches of finished Automata have reached `main` is read from two GET-only routes — the same contract the PWA's merge badge and safe-to-delete card use, and the same one the CLI, MCP and comm surfaces read. Nothing on either route merges, deletes or pushes.

| Endpoint | Method | Purpose |
|---|---|---|
| `/api/autonomous/merge` | GET | Merge-pending mode, alert threshold, one entry per finished Automaton with a work branch, and the shadow decisions |
| `/api/autonomous/safe-to-delete` | GET | Read-only report of Automata branches and worktrees that are safe to delete and those held back |

Neither takes a query parameter. Any other method is 405, a federated peer needs the autonomous read capability (403 without it), and both answer 503 while Automata are disabled. Handle 401, 403, 405 and 503 without failing the screen.

`GET /api/autonomous/merge` response:

```json
{
  "mode": "shadow",
  "alert_minutes": 30,
  "automata": [
    {
      "automaton_id": "d0442737d38c",
      "name": "nightly ingest",
      "branch": "automaton/d0442737d38c",
      "state": "pending",
      "commits_ahead": 3,
      "pending_since": "2026-10-29T09:12:44Z",
      "alerted_at": null,
      "would_alert": true
    }
  ],
  "shadow_decisions": [
    {
      "at": "2026-10-29T09:12:44Z",
      "automaton_id": "d0442737d38c",
      "name": "nightly ingest",
      "branch": "automaton/d0442737d38c",
      "state": "pending",
      "commits_ahead": 3,
      "would_alert": false,
      "repeat_count": 31,
      "last_seen_at": "2026-10-29T09:42:44Z"
    }
  ]
}
```

`GET /api/autonomous/safe-to-delete` response:

```json
{
  "safe": [
    { "branch": "automaton/a1b2c3d4e5f6", "worktree": "/home/me/.datawatch/prd-worktrees/a1b2c3d4e5f6", "reason": "all commits on main", "commits": 4 }
  ],
  "held": [
    { "branch": "automaton/9f8e7d6c5b4a", "worktree": "/home/me/.datawatch/prd-worktrees/9f8e7d6c5b4a", "state": "merged", "dirty": true, "note": "Automaton worktree has uncommitted changes" }
  ],
  "held_dirty": 1
}
```

Shape notes for the app team:

- `state` is one of `merged`, `pending`, `needs_look`, `empty`, `unknown`. Only `pending` and `needs_look` get a badge ("Automaton merge pending", "Automaton needs a look"); the other three draw nothing. Treat any other value as plain text, not a failure.
- **`needs_look` is not a softer `pending`.** A commit whose patch changed while it was merged shows as `needs_look`, never as `pending`: the daemon matches a commit's patch and its subject line against `main`, and when the two disagree it cannot say whether the work landed. The badge is a prompt to compare the branch by hand; no alert is raised for it. On an Automation card that lists several member Automata, `needs_look` wins over `pending`.
- `mode` is `off`, `shadow` or `on`. While it is `off`, `automata` and `shadow_decisions` are empty arrays (not null) and no badge is drawn. `shadow_decisions` is empty outside `shadow`, and newest first.
- `alert_minutes` is the `autonomous.automation.merge_pending_alert_minutes` setting (default 30, `0` = alert off) and rides the body beside the entries it applies to.
- `pending_since` and `alerted_at` are `null` when they do not apply; `alerted_at` is always `null` in `shadow` mode, which never alerts. `would_alert` is present **only in `shadow` mode**.
- `state` comes from the daemon's in-memory comparison, so the merge read never runs git and an Automaton not compared yet reads `unknown`. Keep the last good result when a read fails, and do not repeat the read more than about once every 5 seconds.
- The safe-to-delete read **does run git**, so poll it slowly (the PWA refreshes every 60 seconds). It works in every mode, including `off`. `dirty` and `note` are omitted when false or empty; `worktree` is an empty string when the branch has none. `held_dirty` counts branches safe by state but held back for uncommitted changes. Both lists are always arrays.
- A 503 from the safe-to-delete read means Automata are disabled or no worktree base repository is configured: show the failed state, not an empty report.
- **The report never deletes anything**, and the app must not offer a delete, merge or push action next to it. Cleaning up a branch is a separate step the operator approves.
- Branch names, worktree paths and notes are daemon strings: render them sanitized and wrapped, never as markup.
- The Automaton `triage` record gains the status value `merge_pending`, with `branch` and `commits_ahead` (both omitted on `failed` and `blocked` records). A client that lists triage records must tolerate the new value.
- Two config keys (read and write through the existing config surfaces, under `autonomous.automation`): `merge_pending_mode` (`off`, `shadow`, `on`; default `off`; an empty or other value is refused with 400) and `merge_pending_alert_minutes` (whole number ≥ 0; default 30; `0` is kept as `0`, a negative value is refused with 400).
- Access follows the daemon's rules: bearer token locally, the autonomous read capability for federated peers. The schemas are `MergeStatus`, `MergeAutomaton`, `MergeShadowDecision` and `SafeDeleteReport` in the full OpenAPI spec. The operator reference is `docs/operations.md` (section *Merge-pending check*) and the full field documentation is `docs/api/autonomous.md` (section *Merge state and safe-to-delete report*).

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

## Listener auth posture (`/api/health` `listeners`, `server.acknowledge_unauthenticated`)

`GET /api/health` (no auth) returns, in addition to status/hostname/version/uptime_seconds, a `listeners` array with one entry per daemon listener: `rest`, `rest_tls`, `mcp_sse`, `proxy_sandbox`, `webhook`, `github_webhook`, `twilio_webhook`. Each entry is posture only:

```
{ "name": "rest", "enabled": true,
  "auth_required": false, "loopback": false,
  "acknowledged": false }
```

Fields are booleans: `enabled` (listener enabled in the active config), `auth_required` (a token/secret guards it), `loopback` (bound to `127.0.0.1`/`::1`/`localhost`), `acknowledged` (the operator set `server.acknowledge_unauthenticated`). Bind addresses and token/secret values are never included.

**Client guidance (Android and iPhone):** flag a listener when
`enabled && !auth_required && !loopback && !acknowledged` — the daemon's
"exposed" condition — the same set of listeners the daemon warns about at
start. Clear the flag when the operator sets
`server.acknowledge_unauthenticated` (then `acknowledged` is `true`).

`server.acknowledge_unauthenticated` (bool, default `false`) is read and
written with the standard config endpoints:

```
GET /api/config   → server.acknowledge_unauthenticated
PUT /api/config   { "server.acknowledge_unauthenticated": true }
```

It silences the daemon's startup warning for exposed, unauthenticated,
unacknowledged listeners and sets `acknowledged` in the health `listeners`
array (and in the `listener_*` diagnose checks). It does not change the
bind address, the token, or the warning condition itself.

---

<!-- BL279 see-also footer -->
## See also

- [datawatch-definitions](../datawatch-definitions.md)
- [architecture-overview](../architecture-overview.md)
- [howto/guardrail-library](../howto/guardrail-library.md)
- [howto/dashboard](../howto/dashboard.md)
