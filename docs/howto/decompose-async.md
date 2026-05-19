---
docs:
  index: true
  topics: [autonomous, prd, decompose, sse, streaming, android]
exec_params:
  - name: prd_id
    description: "PRD ID to decompose (UUID)"
    required: true
exec_steps:
  - tool: autonomous_prd_decompose
    description: Start async decompose — returns 202 with stream_url
    args:
      id: "{{params.prd_id}}"
    read_only: false
  - tool: autonomous_prd_get
    description: Poll decompose status (polling fallback)
    args:
      id: "{{params.prd_id}}"
    read_only: true
---
# How-to: Async PRD decompose with SSE streaming

Starting with v8.2.0 (BL328), `POST /api/autonomous/prds/{id}/decompose`
returns **202 Accepted** immediately. Stories stream back via server-sent
events as the LLM produces them, so the client never hits a timeout.

This unblocks the Android T13 requirement: the Android app can display an
incremental progress panel without holding a long HTTP connection open.

## What it is

```
POST /api/autonomous/prds/{id}/decompose
→ 202 Accepted
  {"task_id":"<uuid>","stream_url":"/api/autonomous/prds/{id}/decompose/stream"}
```

The daemon runs the LLM decompose in a background goroutine and emits each
story as an SSE event. Clients choose between two consumption patterns:

**SSE stream** (`GET /api/autonomous/prds/{id}/decompose/stream`) —
real-time incremental; each story appears as it's generated. Supports
`Last-Event-ID` for lossless resume after a dropped connection.

**Polling fallback** (`GET /api/autonomous/prds/{id}/decompose/status`) —
JSON snapshot of `{status, progress, stories_so_far}` for clients that
cannot hold persistent connections (e.g. background services).

## SSE event format

```
id: <sequential-number>
event: story
data: {"index":0,"total":8,"story":{...}}

id: <n>
event: complete
data: {"total":8}

id: <n>
event: error
data: {"error":"<message>"}
```

Keepalive comments (`: keepalive`) are sent every 25 seconds. The stream
includes a `retry:1000` hint so browsers and native SSE clients reconnect
within 1 second.

## Resuming after a disconnect

Send the `Last-Event-ID` header on reconnect:

```sh
curl -sk \
  -H "Authorization: Bearer $TOKEN" \
  -H "Last-Event-ID: 3" \
  https://datawatch.local:8443/api/autonomous/prds/<id>/decompose/stream
```

The server replays all events with `id > 3` from its in-memory log so no
stories are lost.

## Happy path — CLI

```sh
# 1. Start decompose — returns immediately.
RESP=$(curl -sk -X POST \
  -H "Authorization: Bearer $TOKEN" \
  https://datawatch.local:8443/api/autonomous/prds/<id>/decompose)
echo $RESP
# {"task_id":"...","stream_url":"/api/autonomous/prds/<id>/decompose/stream"}

# 2. Subscribe to the SSE stream.
STREAM=$(echo $RESP | jq -r .stream_url)
curl -sk \
  -H "Authorization: Bearer $TOKEN" \
  -H "Accept: text/event-stream" \
  "https://datawatch.local:8443$STREAM"
# event: story
# data: {"index":0,"total":8,"story":{"id":"...","title":"...",...}}
# ...
# event: complete
# data: {"total":8}

# 3. Or poll for status.
curl -sk \
  -H "Authorization: Bearer $TOKEN" \
  https://datawatch.local:8443/api/autonomous/prds/<id>/decompose/status
# {"status":"running","progress":3,"total":8,"stories_so_far":[...]}
# {"status":"complete","total":8,"stories":[...]}
```

## Happy path — PWA

1. Open the PWA → **Autonomous** → select or create a PRD.
2. Tap **Plan** (decompose button).
3. An inline progress panel opens immediately showing "Decomposing PRD…"
   and a running story count as each story appears.
4. Once complete the panel collapses and the PRD task list is populated.

The PWA uses an `EventSource` with exponential-backoff reconnects. It pauses
the SSE connection when the tab is hidden (`visibilitychange`) and resumes
with `Last-Event-ID` when the tab becomes visible again.

## Happy path — MCP

```
autonomous_prd_decompose  id:<prd-id>
→ {"task_id":"...","stream_url":"..."}
```

The MCP tool returns the 202 payload. Use `autonomous_prd_get` to poll
completion or connect to the SSE stream directly.

## REST endpoints

```sh
# Start async decompose.
POST /api/autonomous/prds/{id}/decompose
→ 202 {"task_id":"<uuid>","stream_url":"/api/autonomous/prds/{id}/decompose/stream"}

# SSE stream of story events (real-time).
GET /api/autonomous/prds/{id}/decompose/stream
Headers: Last-Event-ID: <n>  (optional; replays events with id > n)
→ text/event-stream

# JSON polling snapshot.
GET /api/autonomous/prds/{id}/decompose/status
→ {"status":"pending|running|complete|error","progress":<n>,"total":<n>,"stories":[...]}
```

## Android integration

The Android T13 story maps to this flow:

1. App POSTs to decompose, stores `stream_url` and `task_id`.
2. Opens an SSE connection to `stream_url` using OkHttp/EventSource.
3. On each `story` event, appends to the task list and updates a progress bar.
4. On `complete`, persists the story list and closes the stream.
5. On network drop: reconnects with `Last-Event-ID` from the last received
   event ID — no stories are duplicated or lost.

## Common pitfalls

- **Job expires.** In-memory job state is held for 10 minutes after
  completion. If the client reconnects after expiry it gets a 404 on
  the stream URL; re-POST to `/decompose` to start a fresh job.
- **Proxy buffering.** Ensure your reverse proxy does not buffer SSE
  responses. For nginx: `proxy_buffering off; proxy_read_timeout 3600s;`.
- **Long decompose timeout.** The default LLM call timeout is 5 minutes.
  For very large PRDs or slow LLMs, increase `llm.timeout` in the daemon
  config.

## Linked references

- See also: [`autonomous-planning.md`](autonomous-planning.md) — PRD
  create / review / approve lifecycle.
- See also: [`autonomous-review-approve.md`](autonomous-review-approve.md) —
  running a PRD after decompose completes.

---

## See also

- [howto/autonomous-planning](autonomous-planning.md)
- [howto/autonomous-review-approve](autonomous-review-approve.md)
- [architecture-overview](../architecture-overview.md)
