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

Starting with v8.2.0, `POST /api/autonomous/prds/{id}/decompose`
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

## When the decompose output cannot be parsed

Since the 2026-11-18 abort fix, a decompose whose *output* is bad fails the
Automaton — it can no longer abort the daemon, and nothing restarts the
daemon as a result:

- **Bounded before parsing.** The raw output is refused above 8 MiB before it
  is cleaned or parsed, so an over-size or runaway reply fails the Automaton
  the same way instead of growing the daemon's heap.
- **Guarded parse and lint.** The parse-plus-lint step — shared by the sync
  and streaming decompose paths and the re-attached planner ingest — runs
  under a panic guard. A Go panic in the parser or the lint is recovered and
  returned as the named failure, not as an abort.
- **One named failure.** Every one of these — an over-size output, an empty
  result, a parse failure, a lint failure, a panic — is recorded as the
  Automaton's planning failure with source `decompose-parse`, category
  `invalid-response`, and error text starting
  `decompose output unparseable: ` (the reason, the output length, and a
  200-byte head), `retryable: false`, outside the `infra` budget — a plan
  the model wrote that the daemon cannot parse is a content judgment, and
  re-running the identical call would reproduce the identical output.
  (Contrast: a decompose LLM call that fails *before* producing output is
  source `decompose-llm`, category `infra`, and takes the ordinary
  infra-retry path.) See [retry-policy.md](retry-policy.md) for the
  `invalid-response` category.
- **Back to draft, with the reason.** On a give-up the Automaton rolls back
  to `draft`, and the exact `decompose output unparseable: …` text is kept as
  its last decompose failure (`last_decompose_failure`) — visible in the PWA
  decompose panel, in the Automaton's REST snapshot, and in
  `autonomous_prd_get`. One warning alert is raised, and the raw output is
  kept at `<data>/autonomous/scratch/<prd-id>/decompose-output.json.failed`
  (a collision suffix if one already exists) so you can inspect exactly what
  the model wrote. The daemon itself keeps running — a failed plan is not a
  daemon death.

### How to fix it

The daemon does not re-run the call for you — `invalid-response` is terminal
by design. Either re-run the decompose, or revise the spec:

1. **Re-run the decompose** — the PWA **Plan** button, or
   `POST /api/autonomous/prds/{id}/decompose`, or the MCP
   `autonomous_prd_decompose`. A fresh attempt is sometimes enough: when it
   runs, the previous failure reason is fed back to the model ("your
   previous attempt failed: … Fix this in your next response.").
2. **Revise the spec** — when the same failure repeats, the spec is what to
   change. Tighten or split the part that produced the malformed plan, then
   re-run. Start from the `.failed` artifact above and
   `last_decompose_failure.detail` to see what the model actually wrote and
   which phrase tripped the lint.

If instead the *daemon* dies while a decompose is in flight, that is a
different class of failure — see
[operations.md — "Decompose output and daemon aborts"](../operations.md#decompose-output-and-daemon-aborts-sigabrt-core-dump).

## If the daemon itself aborts (SIGABRT, core dump)

The 2026-11-18 abort — the daemon dying with SIGABRT and a core dump right
after reading a decompose session's output — came from a decompose-lint help
probe whose fallback ran a command's bare prefix with no working directory,
no process group and no output cap; a long-running command in that position
grew the daemon's memory until the Go runtime aborted the process. It is
fixed: the probe runs `prefix --help` only (never `-h`, never the bare
prefix), under a 5-second timeout, in its own process group, with its output
capped at 64 KiB — see [automaton-decompose-lint.md](automaton-decompose-lint.md).
If the daemon still dies silently, look in this order:

1. `<data>/daemon-crash.log` — the daemon runs with `GOTRACEBACK=crash` and
   registers this file for the runtime's fatal crash output at start, so a
   Go fatal error (the SIGABRT class) leaves its full dump here. Stderr
   stays untouched — the journal still receives it — and each process
   appends a header line `=== datawatch vX started pid=… at … ===`, so
   crashes from different starts are distinguishable.
2. `coredumpctl list`, then `coredumpctl info <pid>` — the core behind the
   SIGABRT, when the host keeps cores. (The 2026-11-18 host had no
   `coredumpctl` at all, which is why the abort first surfaced as the daemon
   simply vanishing.)
3. The decompose output itself, if a decompose was in flight: the
   `decompose-output.json.failed` artifact named above.

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
