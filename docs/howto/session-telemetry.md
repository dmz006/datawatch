---
docs:
  index: true
  topics: [telemetry, sessions, hooks, tasks, guardrails]
exec_params:
  - {name: session_id, required: true, description: "Full session ID to read telemetry for"}
exec_steps:
  - tool: telemetry_get
    description: Retrieve structured telemetry for the session
    args: {id: "{{params.session_id}}"}
    read_only: true
---

# How-to: Session Telemetry

Structured telemetry gives you a machine-readable view of what a session
is doing: tasks with server-stamped timings, guardrail results, sprint
ancestry, and a failure buffer for drill-down. It is built on top of the
same hook event stream as the Status board.

## What it is

Every hook event a session emits can carry a structured `payload`. The
daemon accumulates fields from successive events into a `SessionTelemetry`
object per session. That object is served at:

```
GET /api/sessions/{id}/telemetry
```

Key properties:
- **Ephemeral** — in-memory per session; wiped on delete.
- **Server-stamped timings** — hook scripts send task `status` strings;
  the daemon computes `started_at`, `completed_at`, and `duration_ms`
  by diffing successive payloads. No client-side timing code needed.
- **Failure buffer** — when a task transitions to `failed`, the daemon
  captures the last 5 hook events as `failed_task_buf` for diagnosis.
- **Durable on request** — set `session.persist_telemetry_on_stop: true`
  to flush telemetry to episodic memory on `Stop`.

## Base requirements

- `datawatch start` — daemon up.
- A running session with hook scripts installed (auto-installed for
  claude-code sessions; see [`howto/claude-hooks.md`](claude-hooks.md)). Sessions require an LLM backend — see [llm-registry.md](llm-registry.md) for LLM setup.
- `jq` on PATH for automatic TodoWrite → tasks[] parsing.

## How structured tasks[] are emitted

### Automatic via TodoWrite (recommended)

The auto-installed `post-event.sh` reads Claude Code's `TodoWrite`
stdin. When `jq` is present, it converts the TodoWrite JSON into a
`tasks[]` array and includes it in the payload automatically. No changes
to the hook script or your project are required.

### Manual in state.json

Alternatively, write a `tasks[]` array into `.claude/sprint/state.json`
yourself. The hook script reads this file and includes it in the payload.

```json
{
  "current_task": "implement endpoint",
  "tasks": [
    {"id": "t1", "title": "Define schema",   "status": "completed"},
    {"id": "t2", "title": "Wire endpoint",   "status": "in_progress"},
    {"id": "t3", "title": "Write tests",     "status": "pending"}
  ],
  "tests": {"pass": 14, "fail": 0, "skip": 0},
  "progress": 60.0
}
```

### Direct POST (scripting / testing)

```sh
curl -sk -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "event": "PostToolUse",
    "tool": "Bash",
    "payload": {
      "current_task": "wire telemetry endpoint",
      "tasks": [
        {"id": "t1", "title": "Define schema",  "status": "completed"},
        {"id": "t2", "title": "Wire endpoint",  "status": "in_progress"}
      ],
      "progress": 50.0
    }
  }' \
  $BASE/api/sessions/$SID/hook-event
```

## Reading telemetry

### REST

```sh
curl -sk -H "Authorization: Bearer $TOKEN" \
  $BASE/api/sessions/$SID/telemetry
```

Response:

```json
{
  "current_task": "wire telemetry endpoint",
  "tool": "Bash",
  "sprint": {"name": "Sprint 1", "id": "s1", "automata": "my-automaton"},
  "tasks": [
    {
      "id": "t1",
      "title": "Define schema",
      "status": "completed",
      "started_at": "2026-05-13T10:00:00Z",
      "completed_at": "2026-05-13T10:04:30Z",
      "duration_ms": 270000
    },
    {
      "id": "t2",
      "title": "Wire endpoint",
      "status": "in_progress",
      "started_at": "2026-05-13T10:04:30Z"
    }
  ],
  "tests": {"pass": 14, "fail": 0, "skip": 0},
  "progress": 50.0,
  "guardrail_verdicts": [],
  "updated_at": "2026-05-13T10:05:00Z"
}
```

### MCP

```
telemetry_get  {id: "<session-id>"}
telemetry_list {}
```

### CLI

```sh
datawatch session telemetry <id>
```

### Comm channel

```
telemetry <id>
telemetry list
```

## Guardrail verdicts

If your hook script (or an orchestrator guardrail) emits
`guardrail_verdicts[]`, each entry appears in the telemetry response:

```json
"guardrail_verdicts": [
  {"guardrail": "sast-scan", "outcome": "pass", "summary": "no issues"},
  {"guardrail": "secrets-scan", "outcome": "warn", "summary": "1 low-entropy string"}
]
```

`outcome` is one of `pass`, `warn`, or `block`. Verdicts are replaced
(not appended) on each event that carries `guardrail_verdicts[]`.

## Failure drill-down

When a task transitions to `failed`, `failed_task_buf` contains the
last 5 hook events received before the failure. Use it to see what
tools ran, what output was produced, and what the session state was:

```sh
curl -sk -H "Authorization: Bearer $TOKEN" \
  $BASE/api/sessions/$SID/telemetry | jq '.failed_task_buf'
```

## Persist telemetry to memory on stop

By default, telemetry is wiped when the session is deleted. To persist
it to episodic memory so it survives daemon restarts and is searchable
via `memory_recall`:

```yaml
# ~/.datawatch/datawatch.yaml
session:
  persist_telemetry_on_stop: true
```

When a `Stop` or `SubagentStop` hook fires, the daemon serializes the
full `SessionTelemetry` struct to memory with a compact summary:

```
Session <id> telemetry: 5 tasks (4 done, 1 failed); last task: wire endpoint
```

Retrieve later with:

```sh
datawatch memory recall "session telemetry <id>"
# or via MCP: memory_recall {query: "session <id> telemetry"}
```

## Sprint ancestry

The `sprint` field carries the full ancestry of the task within the
Automata hierarchy:

```json
"sprint": {
  "name": "Sprint 1",
  "id": "s1",
  "automata": "my-automaton-title",
  "automata_id": "uuid",
  "task": "implement telemetry endpoint",
  "task_id": "t1"
}
```

`automata_id` links back to the originating Automaton (use
`autonomous_prd_get {id: automata_id}` to open it). In the Automata
hierarchy: Automaton → Story (= sprint) → Task.

## See also

- [howto/claude-hooks](claude-hooks.md) — hook script setup and payload schema
- [api/sessions](../api/sessions.md) — full REST surface including `/telemetry`
- [flow/telemetry-flow](../flow/telemetry-flow.md) — data flow diagram
- [datawatch-definitions](../datawatch-definitions.md) — SessionTelemetry, guardrail_verdict, failed_task_buf
