# datawatch-app — WebSocket session window, the bounded session list — app-side note

**Date:** 2026-10-24
**Repo:** `dmz006/datawatch-app` (Android + iPhone/iOS)
**Daemon counterpart:** this repo — the websocket session-list bound (`ws_session_stream.go`, `ws_session_window.go`, `ws_session_broadcast.go`), shipped in the daemon's unreleased development build
**Status:** **a GitHub issue on `dmz006/datawatch-app` is needed and has not been filed.** It cannot be filed from the session that wrote this note (no access to that repository). The supervisor files it on merge, using the draft at the end of this note. The daemon surface below lands with this build. The PWA already uses it; the app UI is not built here (per this repo's parity policy — the app repo owns its UI).

## Context

Until now every connected WebSocket client received the whole session list on connect and again on every change. On a daemon with about 4,000 sessions that was about 35 MB per change, which a phone on a metered connection cannot carry. This build bounds the list to the **default view**: sessions that are not finished, plus sessions that finished within the last `recent_session_minutes` minutes. Finished sessions older than that are never sent over the socket. They come from the paged REST list.

A client that announces the new form in `hello` gets compact summary rows and then one small `session_state` frame per change. A client that does not announce it keeps the old `sessions` frame, but bounded in the same way.

The daemon reuses the existing `hello` / `hello_ack` handshake, the summary row shape of `GET /api/sessions?view=summary`, and the `session_state` frame. Nothing is renamed. The new piece is one capability string, `ws.session_list_window`.

Measured on the daemon's byte-budget test fixture (4,000 sessions, 40 in the default view, 2,500–3,000 characters of text per session), raw WebSocket message sizes:

| Message | Before | After |
|---|---|---|
| Connect, legacy client | 35.8 MB | 44.8 KB |
| Connect with window form, rows frame | — | 20.2 KB, once |
| One session changes, window form | 35.8 MB | 588 bytes |

The daemon's test fails any single message over 300 KB.

## Wire contract

### Capability

`GET /api/info` → `capabilities` now includes `ws.session_list_window`. It is also echoed in the `hello_ack` capabilities. Use the window form only when both `ws.hello` and `ws.session_list_window` are present.

### Opening and `hello`

The socket is open before the client can say anything, so the first frame on a new connection is the bounded legacy `sessions` frame (see *Legacy*). The client then sends:

```json
{"type":"hello","data":{"session_list":"window","session_view":"summary"}}
```

The daemon answers with the modes now in effect:

```json
{"type":"hello_ack","data":{"session_list":"window","session_view":"summary","output":"all","capabilities":["ws.hello","ws.session_list_delta","ws.session_list_window","ws.session_view_summary","ws.output_scoped","ws.unsubscribe"]}}
```

`hello_ack` is followed at once by exactly one rows frame. Trust `session_list` in the ack over what was asked for.

### The rows frame (once per connection, after `hello_ack`)

```json
{"type":"sessions","data":{"sessions":[{"id":"...","full_id":"...","name":"...","state":"running","updated_at":"...", ...}],"version":"...","window_minutes":5,"total":4000,"more":true},"ts":"..."}
```

- `data.sessions` — summary rows, the `GET /api/sessions?view=summary` row shape (`scheduled_count` where the schedule store is wired). A row has no `task`.
- `data.window_minutes` — the window the list is built on. It is the daemon's `server.recent_session_minutes` (a value of 0 or less means 5). Read it; never hard-code it. If a frame omits it, keep the last value you had.
- `data.total` — how many sessions the daemon holds, in or out of the window.
- `data.more` — `true` when the daemon holds sessions this frame leaves out. Absent means `false`; a `total` of 0 is absent too.

The rows frame replaces the connect-time frame. Sending `hello` again gets a new `hello_ack` and a new rows frame, which replaces the list.

### After the rows frame

- **No more `sessions` frames** on that socket.
- `session_state` `{"session": <summary row>}` — one per changed default-view session. Merge by `full_id`; insert it if it is new.
- `session_removed` `{"session_id": "..."}` — **only** when a session is deleted. A session that ages out of the window produces no frame.

### Merge rules (the PWA's rules; the app must match them)

1. A row with no `task` keeps the `task`, `last_response`, `input_mode`, `channel_ready` and every other field the client already holds for that session.
2. A summary key the row omits is **empty now**: clear it, do not keep the old value. The summary keys are `id`, `full_id`, `name`, `state`, `backend`, `llm_ref`, `project_dir`, `hostname`, `created_at`, `updated_at`, `last_activity`, `parent_id`, `est_cost_usd`, `tokens_in`, `tokens_out`, `status_line` and `truncated_fields`.
3. A text field named in `truncated_fields` was cut. If the client holds the full text for the same session, keep it. Otherwise show the cut text and fetch `GET /api/sessions/{id}` when the user opens the session.
4. A full record (one that carries `task`) replaces the held record.
5. A rows frame is the whole default view. A held session it omits, and that the client did not fetch over REST, has left the view: drop it. History rows fetched over REST are kept.

### Aging

The daemon never sends a removal because a finished session left the window. The client ages finished rows out itself: between frames it may drop a finished session whose `updated_at` is older than `window_minutes`, so its list matches the daemon's. The rows frame on reconnect is the authoritative correction.

### History (REST, cursor-paged)

Finished sessions older than the window never come over the socket, to any client mode. Fetch them over REST:

```
GET /api/sessions?view=summary&limit=50
GET /api/sessions?view=summary&limit=50&cursor=<next_cursor>
GET /api/sessions?view=summary&limit=50&history=1&cursor=<next_cursor>
```

- Repeat with each `next_cursor` until the response has none. Keep the same parameters on every page.
- `history=1` adds archived sessions (ones that have left the active store).
- The full record for one session is `GET /api/sessions/{id}`.
- The socket never carries history.

## Legacy fallback (clients that do not announce `window`)

A client that sends no `hello`, or `session_list: "full"`, keeps receiving a `sessions` frame on connect and on every change. From this build the frame is bounded:

- only default-view sessions, so no finished session older than the window;
- long text fields cut to `server.session_summary_text_chars` (default 200), with `truncated_fields` set;
- `data.more`, `data.total` and `data.window_minutes` alongside, as above;
- its `session_state` frames stay full records, for default-view sessions only.

A legacy client must also page history over REST, because old finished sessions no longer arrive over the socket. An app on a daemon **without** `ws.session_list_window` (an older daemon) gets the full, unbounded frame and keeps working as it does today. It should take `delta` when `ws.session_list_delta` is advertised, and treat `hello_ack` as the truth.

| Capability missing | Do this instead |
|---|---|
| `ws.session_list_window` | Do not send `session_list: "window"`. Use `delta` if `ws.session_list_delta` is present (load the list over REST first), otherwise keep the full handling. Where the frame is the bounded legacy one, read `more`, `total` and `window_minutes` when present, and page history over REST. |
| `ws.hello` | Do not send `hello`. Expect the full `sessions` frame on connect and on every change, as before. |

## Acceptance criteria — Android

1. `/api/info` is read on connect; window mode is requested only when `ws.hello` and `ws.session_list_window` are both advertised.
2. `hello` carries `"session_list":"window"`; the client checks `session_list` in `hello_ack` and falls back to the legacy handling when the ack says otherwise.
3. The connect-time `sessions` frame is treated as provisional and replaced by the rows frame that follows `hello_ack`.
4. The rows frame replaces the default-view list and applies the merge rules above. A row never clears held text; a cut field keeps the held full text; a summary key the row omits is cleared.
5. `session_state` merges by `full_id` and inserts new sessions; `session_removed` removes only the named session, and no other frame removes a session.
6. Finished rows older than `window_minutes` are aged out locally. `window_minutes` is read from each frame and never hard-coded; an absent value keeps the last one.
7. History is loaded from `GET /api/sessions?view=summary&limit=50` with `cursor` paging and `history=1`. History rows survive a later rows frame.
8. On reconnect, `hello` is sent again and the new rows frame replaces the list.
9. Unknown frame fields are ignored; `more`, `total` and `window_minutes` are optional.
10. A socket message over 300 KB on a 4,000-session daemon is treated as a defect (the daemon's own test fails above it).
11. Any new string (for example a "history" action) goes through the app's locale pipeline. No English-only UI.

## Acceptance criteria — iOS

Items 1–11 above, applied to the Automata and session list screens. No iOS-specific transport change: the same `URLSessionWebSocketTask` and REST client carry the new messages, and the same merge logic is shared where the app has one.

## Parity surface for this note (app repo)

| Surface | Status |
|---|---|
| PWA window form: connect-time bounded frame, rows frame after `hello`, `session_state` merge, history from REST | ✅ done in the daemon repo |
| Android: window hello, rows frame, merge rules, aging, history paging | ❌ — work item for this note |
| iOS: the same items as Android | ❌ — same issue, same scope |

## Open questions (app repo)

1. Should history be a "Load more" control at the end of the list, or a separate Show history switch as in the PWA?
2. Should aging run on each frame, or on a timer while the list is on screen?

## Draft issue text (for the supervisor to file on merge — do not file from here)

**Title:** Adopt the WebSocket session window (`ws.session_list_window`) for a bounded session list

**Body:**

The daemon now bounds the WebSocket session list to the default view: sessions that are not finished, plus sessions that finished within `recent_session_minutes`. On a daemon with about 4,000 sessions, a full-list frame was about 35 MB per change. With the new form the connect message is a rows frame of about 20 KB, and each change is one `session_state` of under 1 KB.

To use it, the app should:

1. Read `capabilities` from `GET /api/info` and use the window form only when `ws.hello` and `ws.session_list_window` are present.
2. Send `hello` with `{"session_list":"window","session_view":"summary"}`.
3. Replace the list with the single `sessions` rows frame that follows `hello_ack`; read `window_minutes`, `total` and `more` from it.
4. Merge each `session_state` by `full_id`; drop a session only on `session_removed`.
5. Merge rows without dropping held task text, clear summary keys a row omits, and keep full text when a field is in `truncated_fields`.
6. Age finished rows out after `window_minutes` locally; the daemon sends no removal for that.
7. Load history over `GET /api/sessions?view=summary&limit=50` with `cursor` paging and `history=1`.

Older daemons without the capability keep working on the existing frames. Nothing is renamed. The full acceptance criteria and the merge rules are in `docs/plans/2026-10-24-datawatch-app-ws-session-window-note.md` in the daemon repository. Android and iOS are both needed; the PWA is already done.
