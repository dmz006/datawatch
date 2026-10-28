# Sessions — operator reference

Sessions are the unit of work in datawatch. Each session is a
running AI-coding agent (or a recently-completed one), backed by a
tmux pane (for interactive backends) or a process (for chat-mode
backends), with a stable ID, a project directory, output buffer,
state machine, and an audit trail.

The session lifecycle is documented architecturally in
[`docs/architecture.md`](../architecture.md) and via flow diagrams
under [`docs/flow/`](../flow/) (new-session-flow,
input-required-flow, persistence-flow, agent-spawn-flow).

## State machine

```
created → starting → running → waiting_input ↔ running → complete
                              ↘ rate_limited ↗
                              ↘ failed
                              ↘ killed
```

`waiting_input` is the state where the operator (or an output
filter) needs to send something to the running backend. The PWA's
yellow "Input Required" banner + the chat-channel notification fan
out from this transition.

## Surface

### REST

| Endpoint | Purpose |
|---|---|
| `GET /api/sessions` | List sessions (active + recent). Filter via `?name=`, `?state=`, `?backend=`, `?alive=`; compact list via `?view=summary`, narrow with `?since=` / `?states=` — see [Compact session list](#compact-session-list-viewsummary). |
| `POST /api/sessions/start` | Start a new session. Body: `{task, project_dir, backend, name?, template?, effort?, one_shot?, skip_snapshot?, tags?}`. `tags` is an array of tag IDs the session carries from the start; an unknown ID refuses the start with 404. `one_shot` terminates the session on `DATAWATCH_COMPLETE:`. `skip_snapshot` (bool, default false) is for planning/judgment sessions: when true the session must never stage or commit the repo — both the pre-session snapshot commit and the completion commit are skipped. |
| `GET /api/output?id=&n=` | Last N output lines. |
| `POST /api/command` | Send raw input to a session's tmux pane. |
| `POST /api/sessions/kill` | Kill the underlying backend; tmux pane stays. |
| `POST /api/sessions/rename` | Rename a session. |
| `POST /api/sessions/tags` | Replace a session's own tags. Body: `{id, tag_ids}`; `id` is the full or short session ID. Duplicates are dropped (first wins); an unknown tag ID is refused with 404 and nothing is written. Returns the session with `tags` and `inherited_tags`. Needs the federation sessions-write capability. See [Session tags](#session-tags). |
| `POST /api/sessions/restart` | Re-launch the backend with the same task + project. |
| `POST /api/sessions/delete` | Forget a stopped session entirely. |
| `POST /api/sessions/{id}/rollback` | Roll back to the pre-session git tag (`datawatch-pre-<id>`). |
| `GET /api/sessions/timeline` | Per-session event timeline. |
| `GET /api/sessions/stale` | Sessions stuck in `running` longer than `session.stale_timeout_seconds`. |
| `GET /api/sessions/reconcile` | List orphan tmux sessions on disk that the registry doesn't know about. |
| `POST /api/sessions/import` | Import an orphan from disk. |
| `POST /api/sessions/bind` | Bind a session to an ephemeral worker / agent. |
| `GET /api/sessions/response` | Last `response` event content for one session (used by the PWA "view last response" modal). |
| `GET /api/sessions/{id}/telemetry` | Structured telemetry for a session (tasks, guardrail verdicts, sprint, progress). |
| `GET /api/sessions/{id}/progress` | Live progress snapshot parsed from the session's own pi log: assistant turns, tool-call counts, last action (tool, args, summary), tokens. Same shape as the `progress` field of a running autonomous task. |

#### Session tags

A session carries its own managed tags (`tags`, a list of tag IDs). Set
them at start with the `tags` field of `POST /api/sessions/start`, or
replace them later with `POST /api/sessions/tags`:

```json
{"id": "a3f2", "tag_ids": ["5e6f7a8b", "1a2b3c4d"]}
```

A session spawned by an Automaton also shows that Automaton's tags as
`inherited_tags`. The value is computed on every read and never stored, so
it follows the Automaton: change the Automaton's tags and its sessions show
the new set on the next read. A session outside any Automaton has
`inherited_tags: []`. The session's own tags are not changed by inheritance.

Session list and detail responses carry `tags` and `inherited_tags`
additively; no existing field changed. Deleting a tag is refused with 409
while an Automaton or a session still carries it; the `count` in the 409
body covers both.

#### Compact session list (`view=summary`)

A busy daemon's full session list can reach several megabytes, almost
all of it long free text. `GET /api/sessions` takes three optional
query parameters for clients that only render a list:

| Parameter | Effect |
|---|---|
| `view=summary` | Same JSON array, but `task`, `prompt_context`, `last_response` and `last_summary_long` are cut to `server.session_summary_text_chars` characters (default 200, minimum 20) ending in `…`. Each record that was cut gets `truncated_fields`, e.g. `["task","last_response"]`. Terminal sessions (complete, failed, killed) are capped to the newest `server.session_summary_terminal_limit` (default 50, `0` = no cap) by `updated_at`; running, waiting and rate-limited sessions are never dropped. Any other `view` value returns 400. |
| `since=<time>` | Only sessions whose `updated_at` is after the given time. RFC3339 (`2026-10-14T12:00:00Z`) or unix seconds. Unparseable values return 400. |
| `states=<list>` | Comma list of states to include, e.g. `running,waiting_input`. `active` expands to every non-terminal state. Unknown or empty lists return 400. |

`since` and `states` also work without `view=summary` and then return
full records. They combine with `name`, `state`, `backend` and `alive`.
`tree=1` and `orphaned=1` take precedence and ignore all three.

**Omitted header.** When the terminal cap drops sessions, the response
carries `X-Datawatch-Sessions-Omitted: <n>`. The header is absent when
nothing was dropped.

**Full record.** A truncated record is for display only. Fetch
`GET /api/sessions/{id}` for the untruncated session.

**Retention decision.** The unfiltered list (no `view`, `since` or
`states`) is never capped or truncated, so existing clients keep
working unchanged. Retention and truncation apply only to
`view=summary`. Check for `sessions.view_summary` in the
`capabilities` array of `GET /api/info` before using these parameters;
an older daemon ignores them and returns the full list.

Both limits are read from the live config on every request, so a
change made through any of these takes effect without a restart:

| Setting | YAML (`server:`) | CLI | Web UI | REST | Chat |
|---|---|---|---|---|---|
| Ended sessions kept | `session_summary_terminal_limit: 50` | `datawatch config set server.session_summary_terminal_limit 50` | Settings → General → Sessions → *Session summary: ended sessions kept* | `PUT /api/config {"server.session_summary_terminal_limit": 50}` | `configure server.session_summary_terminal_limit=50` |
| Text length | `session_summary_text_chars: 200` | `datawatch config set server.session_summary_text_chars 200` | Settings → General → Sessions → *Session summary: text length (chars)* | `PUT /api/config {"server.session_summary_text_chars": 200}` | `configure server.session_summary_text_chars=200` |

```bash
# Running and waiting sessions only, compact records
curl -sk -H "Authorization: Bearer $TOKEN" \
  "$BASE/api/sessions?view=summary&states=active"

# What changed in the last five minutes
curl -sk -H "Authorization: Bearer $TOKEN" \
  "$BASE/api/sessions?view=summary&since=$(( $(date +%s) - 300 ))"
```

The same compact record is what WebSocket clients receive in
`session_state` after negotiating `session_view: summary` — see
[mobile-surface.md](mobile-surface.md#low-bandwidth-client-flow).

#### Paged summary list (`limit`, `cursor`, `status`, `active`, `q`, `view`)

`view=summary`, and any of `limit`, `cursor`, `status`, `active` and `q`, switch
`GET /api/sessions` to a row-only list: `id`, `full_id`, `name`, `state`, `backend`,
`project_dir`, `hostname`, `created_at`, `updated_at`, `last_activity`, `parent_id`,
cost and token counts, `scheduled_count` and a one-line `status_line`. No task text,
prompt context or summaries are included. A request with none of these parameters
still returns the full legacy array.

| Parameter | Effect |
|---|---|
| `view=summary` | Row-only list as above. Without `limit` or `cursor` the reply is a JSON array and terminal sessions are capped as described above. Any other `view` value returns 400. |
| `limit=<n>` | Switches to the paged envelope `{"sessions": [...], "total": N, "next_cursor": "..."}`. Default 50, maximum 500; below 1 or not a number returns 400. |
| `cursor=<opaque>` | The `next_cursor` of the previous page. Newest `updated_at` first, ties by `full_id`. An invalid cursor returns 400. |
| `status=<list>` | Comma list of states, e.g. `running,waiting_input`. Unknown names return 400. |
| `active=1` | Only non-terminal sessions. |
| `q=<text>` | Case-insensitive match against name, ids and the first 256 characters of the task. |

The filtered total is also sent as `X-Datawatch-Sessions-Total`.

**ETag.** Row-only replies carry `ETag` and `Cache-Control: no-cache`. Send the value
in `If-None-Match`; an unchanged list answers `304 Not Modified` with an empty body.
The full legacy array has no ETag. Fetch a whole session with
`GET /api/sessions/{id}`.

```bash
# First page of live work, then poll cheaply
curl -si -H "Authorization: Bearer $TOKEN" "$BASE/api/sessions?limit=50&active=1"
curl -si -H "Authorization: Bearer $TOKEN" -H 'If-None-Match: "<etag>"' "$BASE/api/sessions?limit=50&active=1"   # 304
```

#### GET /api/sessions/{id}/telemetry

Returns structured telemetry accumulated from hook payloads for
the session. Ephemeral — wiped when the session is deleted unless
`session.persist_telemetry_on_stop: true`.

**Response schema:**

```json
{
  "current_task": "string",
  "tool": "string",
  "file": "string",
  "sprint": {
    "name": "Sprint 1",
    "id": "s1",
    "automata": "string",
    "automata_id": "uuid",
    "task": "string",
    "task_id": "string"
  },
  "tasks": [
    {
      "id": "string",
      "title": "string",
      "status": "pending | in_progress | completed | failed",
      "started_at": "RFC3339 | null",
      "completed_at": "RFC3339 | null",
      "duration_ms": 0
    }
  ],
  "tests": {"pass": 0, "fail": 0, "skip": 0},
  "progress": 75.0,
  "guardrail_verdicts": [
    {"guardrail": "string", "outcome": "pass | warn | block", "summary": "string"}
  ],
  "parent_session_id": "string",
  "failed_task_buf": [...],
  "updated_at": "RFC3339"
}
```

`failed_task_buf` contains the last 5 hook events received before
any task transitioned to `failed`. Present only when a task failure
occurred; used for drill-down diagnosis.

Timings (`started_at`, `completed_at`, `duration_ms`) are
server-stamped on task status transitions. Hook scripts do not
need to compute or send them.

#### GET /api/sessions/{id}/progress

Parses the session's own pi JSONL log and returns how far it has gotten.
Read-only and stateless — works for any stored session whose log exists
(pi-family backend; the log is located under the sessions root from the
session's `project_dir` and start time). 404 when the session is unknown
or has no log yet (other backends, or a session whose log has not been
flushed); 405 for non-GET methods.

**Response schema:**

```json
{
  "session_id": "a3f2",
  "full_id": "host-a3f2",
  "state": "running",
  "log_path": "/sessions/--home-dmz-proj--/2026-10-14T12-00-00-000Z_a3f2.jsonl",
  "last_event_at": "2026-10-14T12:04:41Z",
  "assistant_turns": 12,
  "tool_calls": {"bash": 9, "write": 3},
  "last_tool_name": "bash",
  "last_tool_args": "{\"command\": \"go test ./...\"}",
  "last_summary": "Now running the full test suite before wrapping up.",
  "tokens": 48210
}
```

`last_event_at` is the newest event's timestamp (RFC3339) — check it
first to tell "just finished" from "still grinding". `last_summary` is the
first 200 characters of the agent's last planning/reporting block, which is
usually the most human-readable field here. This is the exact shape the
autonomous executor refreshes into a task's `progress` field — see
[autonomous.md](autonomous.md#live-task-progress--looping-warning).

```bash
curl -sk -H "Authorization: Bearer $TOKEN" "$BASE/api/sessions/a3f2/progress"
```

### MCP

`list_sessions`, `start_session`, `session_output`, `send_input`,
`kill_session`, `rename_session`, `restart_session`,
`delete_session`, `session_rollback`, `session_timeline`,
`sessions_stale`, `session_reconcile`, `session_import`,
`session_bind_agent`, `session_state`, `telemetry_get`,
`telemetry_list` — see [`docs/api-mcp-mapping.md`](../api-mcp-mapping.md).

`list_sessions` accepts `view` (`full` by default, or `summary`),
`states` and `since` with the same meaning as the REST parameters
above; any of them makes it return the REST JSON array, plus a note
when the summary cap omitted sessions.

### CLI

```bash
datawatch session start --task "fix the auth tests" --project /home/me/work/auth
datawatch session list
datawatch session list --summary --states active      # compact JSON, running/waiting only
datawatch session list --since 2026-10-14T12:00:00Z    # sessions updated since a time
datawatch session output <id>
datawatch session send <id> "approve"
datawatch session kill <id>
datawatch session restart <id>
datawatch session timeline <id>
datawatch session rollback <id>
datawatch session telemetry <id>
```

### Chat / messaging

Every bidirectional channel (Signal / Telegram / Discord / Slack /
Matrix / Twilio) supports the full session lexicon:

| Command | Effect |
|---|---|
| `new: <task>` | Start a session with the default backend in the default project. |
| `new: @<server>: <task>` | Same, on a remote (proxy mode). |
| `list` | List sessions. |
| `status <id>` | One-line status. |
| `tail <id> [n]` | Last N output lines (default 20). |
| `send <id>: <text>` | Send input. |
| `kill <id>` | Kill. |
| `attach <id>` | Get an attach URL for the PWA. |
| `telemetry <id>` | Return structured telemetry for a session. |
| `telemetry list` | List all sessions with non-empty telemetry. |

## Configuration

```yaml
session:
  max_sessions: 10
  input_idle_timeout: 30
  tail_lines: 100
  alert_context_lines: 10
  default_project_dir: /home/me/work
  root_path: /home/me        # file-browser clamp
  console_cols: 80
  console_rows: 24
  skip_permissions: true     # claude-code --dangerously-skip-permissions
  channel_enabled: true      # claude MCP channel mode
  auto_git_init: true
  auto_git_commit: false
  kill_sessions_on_exit: false
  stale_timeout_seconds: 1800
  task_delivery_window_seconds: 60 # max wait per attempt for the Claude prompt before redelivery
  task_delivery_attempts: 3    # redelivery attempts before the task fails
  schedule_settle_ms: 1500
  default_effort: normal     # quick | normal | thorough
  persist_telemetry_on_stop: false  # flush structured telemetry to episodic memory on Stop
```

**`persist_telemetry_on_stop`** — when `true`, structured telemetry
(task list, progress, guardrail verdicts) is written to episodic
memory when a session's `Stop` or `SubagentStop` hook fires. The
memory entry is searchable via `memory_recall`. Telemetry is
ephemeral by default (wiped on session delete); this flag makes it
durable across daemon restarts.

## See also

- [`docs/architecture.md`](../architecture.md) — process model + tmux lifecycle
- [`docs/flow/new-session-flow.md`](../flow/new-session-flow.md) — start sequence
- [`docs/flow/input-required-flow.md`](../flow/input-required-flow.md) — `waiting_input` detection
- [`docs/flow/persistence-flow.md`](../flow/persistence-flow.md) — when state writes hit disk
- [`docs/flow/agent-spawn-flow.md`](../flow/agent-spawn-flow.md) — ephemeral worker variant
- [`docs/api/autonomous.md`](autonomous.md) — sessions spawned by the autonomous PRD runner
- [`docs/api/orchestrator.md`](orchestrator.md) — sessions spawned by the PRD-DAG orchestrator

---

<!-- BL279 see-also footer -->
## See also

- [datawatch-definitions](../datawatch-definitions.md)
- [howto/sessions-deep-dive](../howto/sessions-deep-dive.md)
- [howto/mcp-tools](../howto/mcp-tools.md)
- [architecture-overview](../architecture-overview.md)
