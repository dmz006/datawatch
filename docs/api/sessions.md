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
| `GET /api/sessions` | List sessions (active + recent). Filter via `?state=`, `?backend=`, `?project=`. |
| `POST /api/sessions/start` | Start a new session. Body: `{task, project_dir, backend, name?, template?, effort?}`. |
| `GET /api/output?id=&n=` | Last N output lines. |
| `POST /api/command` | Send raw input to a session's tmux pane. |
| `POST /api/sessions/kill` | Kill the underlying backend; tmux pane stays. |
| `POST /api/sessions/rename` | Rename a session. |
| `POST /api/sessions/restart` | Re-launch the backend with the same task + project. |
| `POST /api/sessions/delete` | Forget a stopped session entirely. |
| `POST /api/sessions/{id}/rollback` | Roll back to the pre-session git tag (`datawatch-pre-<id>`). |
| `GET /api/sessions/timeline` | Per-session event timeline. |
| `GET /api/sessions/stale` | Sessions stuck in `running` longer than `session.stale_timeout_seconds`. |
| `GET /api/sessions/reconcile` | List orphan tmux sessions on disk that the registry doesn't know about. |
| `POST /api/sessions/import` | Import an orphan from disk. |
| `POST /api/sessions/bind` | Bind a session to an ephemeral worker / agent. |
| `GET /api/sessions/response` | Last `response` event content for one session (used by the PWA "view last response" modal). |

### MCP

`list_sessions`, `start_session`, `session_output`, `send_input`,
`kill_session`, `rename_session`, `restart_session`,
`delete_session`, `session_rollback`, `session_timeline`,
`sessions_stale`, `session_reconcile`, `session_import`,
`session_bind_agent`, `session_state` — see
[`docs/api-mcp-mapping.md`](../api-mcp-mapping.md).

### CLI

```bash
datawatch session start --task "fix the auth tests" --project /home/me/work/auth
datawatch session list
datawatch session output <id>
datawatch session send <id> "approve"
datawatch session kill <id>
datawatch session restart <id>
datawatch session timeline <id>
datawatch session rollback <id>
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
  schedule_settle_ms: 1500
  default_effort: normal     # quick | normal | thorough
```

## See also

- [`docs/architecture.md`](../architecture.md) — process model + tmux lifecycle
- [`docs/flow/new-session-flow.md`](../flow/new-session-flow.md) — start sequence
- [`docs/flow/input-required-flow.md`](../flow/input-required-flow.md) — `waiting_input` detection
- [`docs/flow/persistence-flow.md`](../flow/persistence-flow.md) — when state writes hit disk
- [`docs/flow/agent-spawn-flow.md`](../flow/agent-spawn-flow.md) — ephemeral worker variant
- [`docs/api/autonomous.md`](autonomous.md) — sessions spawned by the autonomous PRD runner
- [`docs/api/orchestrator.md`](orchestrator.md) — sessions spawned by the PRD-DAG orchestrator
