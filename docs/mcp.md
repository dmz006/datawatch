# MCP Server

datawatch exposes a [Model Context Protocol](https://modelcontextprotocol.io) (MCP)
server, enabling Cursor, Claude Desktop, VS Code, and remote AI agents to manage AI
coding sessions directly — without leaving the IDE or chat app.

---

## Daemon Mode Compatibility

Since v0.2.0, `datawatch start` daemonizes by default.

- **Stdio transport** (`datawatch mcp`): unaffected — this is a separate foreground command invoked directly by the IDE. The daemon mode of `datawatch start` does not affect it.
- **SSE transport**: runs as a goroutine inside the daemon process. It starts and stops with the daemon regardless of whether the daemon is foreground or daemonized.

To run MCP SSE on a daemonized server, set `mcp.sse_enabled: true` in config and run `datawatch start` as usual.

---

## Overview

MCP is an open protocol for connecting AI models to tools and data sources. When you
configure datawatch as an MCP server, your IDE's AI assistant can:

- List active sessions on any connected machine
- Start new sessions for coding tasks
- Read live session output
- Send input to sessions waiting for a reply
- Terminate sessions

Two transport modes are supported:

| Mode | When to use |
|---|---|
| **stdio** | Local IDE clients (Cursor, Claude Desktop, VS Code) — no port required |
| **HTTP/SSE** | Remote AI agents, autonomous workflows, cross-machine access |

---

## Local Setup (stdio)

The stdio transport runs datawatch as a subprocess. Your IDE starts it on demand and
communicates over stdin/stdout. No port, no network, no TLS needed.

### Cursor

Add to `~/.cursor/mcp.json`:

```json
{
  "mcpServers": {
    "datawatch": {
      "command": "datawatch",
      "args": ["mcp"]
    }
  }
}
```

Or via **Cursor → Settings → MCP → Add Server**.

### Claude Desktop

Add to `~/.config/claude/claude_desktop_config.json` (Linux/macOS):

```json
{
  "mcpServers": {
    "datawatch": {
      "command": "datawatch",
      "args": ["mcp"]
    }
  }
}
```

On macOS, the config file is at `~/Library/Application Support/Claude/claude_desktop_config.json`.

### VS Code (Copilot / Continue)

For Continue extension, add to `.continue/config.json`:

```json
{
  "mcpServers": [
    {
      "name": "datawatch",
      "command": "datawatch",
      "args": ["mcp"]
    }
  ]
}
```

For GitHub Copilot workspace MCP (if supported), follow the workspace MCP config format
for your version of VS Code.

Restart Cursor, Claude Desktop, or VS Code after saving the config.

### Remote server via SSH

If datawatch runs on a remote machine and you don't want to expose any ports, use SSH
stdio forwarding:

```json
{
  "mcpServers": {
    "datawatch-remote": {
      "command": "ssh",
      "args": ["myserver", "datawatch", "mcp"]
    }
  }
}
```

The MCP protocol runs over the SSH connection — no firewall rules or port forwarding needed.

---

## Remote Setup (HTTP/SSE)

The SSE transport starts an HTTP server that remote AI agents connect to. This enables:

- Remote AI agents (running in cloud functions, other machines, etc.) to manage sessions
- Multi-machine datawatch orchestration
- Programmatic access from scripts and CI systems

### Enable in config

```yaml
mcp:
  enabled: true
  sse_enabled: true
  sse_host: "0.0.0.0"     # bind address; default: 0.0.0.0
  sse_port: 8081           # port; default: 8081
  token: "your-secret"    # bearer token — required for remote connections
  tls_enabled: true
  tls_auto_generate: true  # auto-generate self-signed cert in ~/.datawatch/tls/mcp/
```

Start datawatch normally — the SSE server starts alongside all other backends:

```bash
datawatch start
```

Or run standalone:

```bash
datawatch mcp --sse
```

### Remote AI client config

For OpenAI Assistants, Claude API tool use, or any MCP-compatible remote agent:

```json
{
  "mcpServers": {
    "datawatch": {
      "url": "https://your-server:8081/sse",
      "headers": {
        "Authorization": "Bearer your-secret"
      }
    }
  }
}
```

### TLS

When `tls_enabled: true`:

- TLS 1.3 is enforced (TLS 1.2 and below are rejected)
- Post-quantum hybrid key exchange (X25519Kyber768) is negotiated automatically by
  Go 1.23+ when the client supports it — no extra config needed
- `tls_auto_generate: true` (default) generates a self-signed certificate at
  `~/.datawatch/tls/mcp/cert.pem` (valid 10 years, persisted across restarts)
- To use a CA-signed certificate, set `tls_cert` and `tls_key`:

```yaml
mcp:
  tls_enabled: true
  tls_auto_generate: false
  tls_cert: /etc/ssl/certs/datawatch.crt
  tls_key: /etc/ssl/private/datawatch.key
```

### Trusting self-signed certificates

When using `tls_auto_generate: true`, MCP clients need to trust the self-signed cert:

**Download the certificate:**
- Web UI: Settings > Comms > Web Server > Download CA Certificate
- API: `GET /api/cert?format=der` (.crt) or `GET /api/cert` (.pem)
- File: `~/.datawatch/tls/mcp/cert.pem`

**For Cursor / VS Code MCP clients:**
- Most MCP clients over SSE use HTTPS. Set `NODE_TLS_REJECT_UNAUTHORIZED=0` in the
  client environment, or install the CA cert system-wide.

**For Claude Desktop:**
- Claude Desktop uses stdio transport (not SSE), so TLS is not needed for local use.
- For remote SSE access, configure the cert in the OS certificate store.

**System-wide cert install:**
```bash
# Linux (Debian/Ubuntu)
sudo cp ~/.datawatch/tls/mcp/cert.pem /usr/local/share/ca-certificates/datawatch.crt
sudo update-ca-certificates

# macOS
sudo security add-trusted-cert -d -r trustRoot -k /Library/Keychains/System.keychain ~/.datawatch/tls/mcp/cert.pem
```

**For mobile PWA (Android/iPhone):**
See the operations guide for device-specific cert install instructions.

---

## Available Tools

The MCP server exposes **149 tools** across the surfaces below
(v8.7.3 count — includes BL356–BL361 additions). The authoritative live list is served at
`GET /api/mcp/docs` (HTML) or `GET /api/mcp/docs?format=json` (JSON)
— `claude mcp list` queries this on connect, and the PWA Settings →
About → "MCP tools" link opens it. The selected tools below are
documented inline; the rest follow the same parameter-table-and-example
shape.

### Tool families (high-level)

| Family | Tools |
|--------|-------|
| Sessions | `list_sessions`, `start_session`, `send_input`, `copy_response`, `kill_session`, `delete_session`, `restart_session`, `rename_session`, `session_output`, `session_timeline`, `session_bind_agent`, `session_import`, `session_reconcile`, `session_rollback`, `sessions_stale`, `stop_all_sessions`, `session_children`, `reply_to_parent` |
| Exit Hooks | `exit_hook_list`, `exit_hook_add`, `exit_hook_delete`, `exit_hook_enable`, `exit_hook_disable` |
| Work Queue | `queue_push`, `queue_claim`, `queue_complete`, `queue_fail`, `queue_list` |
| Discussion Subscribe | `discussion_subscribe`, `discussion_unsubscribe`, `discussion_subscriptions` |
| Result Store | `result_put`, `result_get`, `result_list`, `result_delete` |
| Autonomous PRDs | `autonomous_status`, `autonomous_config_get/set`, `autonomous_prd_list/create/get/decompose/approve/reject/request_revision/edit_task/instantiate/run/cancel/set_llm/set_task_llm/children`, `autonomous_learnings` |
| Orchestrator | `orchestrator_graph_create/plan/run/get/list/cancel`, `orchestrator_verdicts`, `orchestrator_config_get/set` |
| Pipelines | `pipeline_start/list/status/cancel` |
| Memory + KG | `memory_remember/recall/list/forget/export/import/reindex/stats/learnings`, `kg_add/query/timeline/stats/invalidate`, `research_sessions`, `get_prompt`, `copy_response` |
| Observer | `observer_stats`, `observer_envelopes`, `observer_envelopes_all_peers` (BL180 cross-host), `observer_envelope`, `observer_peers_list/get/register/delete`, `observer_peer_stats`, `observer_agent_list/stats`, `observer_config_get/set`, `ollama_stats` |
| Agents | `agent_list/get/spawn/terminate/logs/audit` |
| Plugins | `plugins_list/reload`, `plugin_get/test/enable/disable` |
| Profiles + projects | `profile_list/get/create/update/delete/smoke`, `project_list/summary/upsert`, `project_alias_delete` |
| Templates / scheduling / cooldown | `template_list/upsert/delete`, `schedule_list/add/cancel`, `cooldown_status/set/clear` |
| Devices + routing | `device_alias_list/upsert/delete`, `routing_rules_list/test` |
| Cost + audit + config + alerts | `cost_summary/usage/rates`, `analytics`, `audit_query`, `get_config`, `config_set`, `get_stats`, `get_version`, `diagnose`, `reload`, `restart_daemon`, `splash_info`, `get_alerts`, `mark_alert_read` |
| Saved commands | `list_saved_commands`, `send_saved_command` |
| Ask / assist | `ask`, `assist` |
| Voice | (no MCP tools — REST `/api/voice/transcribe` + chat-channel auto-handle) |

Tools added in v5.9 → v5.26 (catch-up since the last doc sweep):

- **`autonomous_prd_children`** (v5.9.0, BL191 Q4) — list child PRDs spawned from a parent's `Task.SpawnPRD` shortcuts.
- **`autonomous_prd_edit_task`** (v5.9.0+) — edit task `spec` / `backend` / `effort` / `model` while a PRD is in `needs_review` or `revisions_asked`.
- **`autonomous_prd_set_llm`** + **`autonomous_prd_set_task_llm`** (v5.4.0, BL203) — operator-pinned LLM override at the PRD or task level.
- **`autonomous_prd_instantiate`** (v5.x) — instantiate from a template PRD with variable substitution.
- **`observer_envelopes_all_peers`** (v5.12.0, BL180 cross-host) — federation-aware envelope view; cross-peer caller attribution surfaces as `<peer>:<envelope-id>` rows on each matched server envelope.
- **`session_bind_agent`** (v5.x) — bind a manual session to an existing agent record so observer/cost roll-ups attribute correctly.
- **`session_import`** (v5.x) — import a foreign tmux session record into datawatch state without re-spawning.
- **`session_reconcile`** (v5.x) — reconcile post-restart: walk live tmux + persisted state, drop ghosts, re-attach orphans.
- **`session_rollback`** (v5.x) — checkout the per-session pre-run git tag.
- **`sessions_stale`** (v5.x) — list sessions whose tmux pane has gone away but state hasn't been reaped yet.
- **`stop_all_sessions`** (v5.x) — bulk stop with optional state filter.
- **`reload`** (v5.7.0) — `POST /api/reload` shortcut, mirrors the new `datawatch reload` CLI subcommand.
- **`session_children`** (v8.10.0) — list child sessions of a parent session. Returns ID, state, backend, and task for each child.
- **`reply_to_parent`** (v8.10.0) — send a message to the parent session that spawned this one. Sub-agents use this to report completion back to their orchestrator.
- **`start_session` extended** (v8.10.0) — added `caller_session_id` (records parent lineage), `kill_children` (cascade kill opt-in), `permission_mode` (v8.9.24), and `name` parameters.
- **`list_sessions` extended** (BL361) — added `name` (glob filter), `state`, `backend`, `alive`, and `format` parameters.
- **`restart_session`** — restarts a session from any terminal state; accepts `session_id` or `session_name` (BL354 name resolution) and optional `task` override.
- **`exit_hook_list/add/delete/enable/disable`** (BL356) — session crash/exit hooks: fire `restart` or `notify` actions when a named session goes zombie or enters failed/killed state.
- **`queue_push/claim/complete/fail/list`** (BL357) — durable role-based work queue for multi-agent coordination.
- **`discussion_subscribe/unsubscribe/subscriptions`** (BL358) — subscribe sessions to discussions so new entries are forwarded via `send_input`.
- **`result_put/get/list/delete`** (BL360) — structured named-result store for agents to share outputs without out-of-band coordination.



### `list_sessions`

List all AI coding sessions on this host, including their state and task description.

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `name` | string | No | BL361 — glob pattern to filter sessions by name (e.g. `worker-*`, `agent-?`). Empty = all. |
| `state` | string | No | BL361 — filter by exact state: `running`, `waiting_input`, `rate_limited`, `complete`, `failed`, `killed`. Empty = all. |
| `backend` | string | No | BL361 — filter by backend family name (e.g. `opencode`, `claudecode`). Empty = all. |
| `alive` | string | No | BL361 — filter by `claude_alive` field: `true`, `false`, or `any` (default). Empty = any. |
| `format` | string | No | BL361 — output format: `text` (default) or `json`. `json` returns a structured JSON array. |
| `orphaned` | boolean | No | BL350 — when `true`, return only sessions whose `parent_id` points to a session that no longer exists (orphaned by parent death or eviction). |

**Example response:**
```
Sessions on my-server:

ID:      a3f2
State:   running
Task:    write unit tests for the auth package
Dir:     /home/me/myproject
Updated: 2026-03-26T14:32:01Z

ID:      b7c1
State:   waiting_input
Task:    add Docker support
Dir:     /home/me/myproject
Updated: 2026-03-26T14:45:22Z
Prompt:  Overwrite existing Dockerfile? [y/N]
```

---

### `start_session`

Start a new AI coding session.

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `task` | string | Yes | Task description to send to the AI |
| `project_dir` | string | No | Absolute path to the project directory. Defaults to `session.default_project_dir` from config |
| `llm` | string | No | LLM registry name to use for this session (e.g. `"ollama"`, `"claude"`). Overrides daemon default. |
| `compute_node` | string | No | ComputeNode registry name. Requires `llm` to be set. |
| `name` | string | No | Human-readable session name (used by `send_input` name resolution). |
| `permission_mode` | string | No | Permission mode for this session: `default`, `plan`, `acceptEdits`, `auto`, `bypassPermissions`, `dontAsk`. Empty uses daemon config default. |
| `caller_session_id` | string | No | Full ID (`hostname-hex`) of the session spawning this one. Records parent-child lineage. Agents should pass `$CLAUDE_SESSION_ID`. |
| `kill_children` | boolean | No | When `true`, killing this session also kills all child sessions it spawns. Default: `false` (children survive independently). |

**Example response:**
```
Session started.
ID:      a3f2
Task:    write unit tests for the auth package
Dir:     /home/me/myproject
Tmux:    cs-myserver-a3f2

Use session_output(id="a3f2") to follow progress.
```

---

### `session_output`

Get the last N lines of output from a session.

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `session_id` | string | Yes | Session ID (4-char short form or full `hostname-id`) |
| `lines` | number | No | Number of lines to return. Default: 50 |

**Example response:**
```
[a3f2] State: waiting_input | Task: write unit tests for the auth package
---
Waiting for input: Found 3 test files. Overwrite? [y/N]
Use send_input(session_id="a3f2", text=...) to respond.
---
  Creating auth_test.go...
  Writing 14 test cases...
  Found existing file: auth_test.go
  Overwrite? [y/N]
```

---

### `send_input`

Send a reply to a session waiting for input.

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `session_id` | string | Yes | Session ID |
| `text` | string | Yes | Text to send as input |

**Example response:**
```
Input sent to session a3f2.
```

---

### `kill_session`

Terminate a session.

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `session_id` | string | Yes | Session ID to terminate |

**Example response:**
```
Session a3f2 killed.
```

---

### `session_children`

List child sessions spawned by a given parent session. Useful for agents orchestrating sub-agents to check progress.

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `session_id` | string | Yes | ID or name of the parent session |

**Example response:**
```
Children of session pp01 (2):
  [cc01] running | claude-code | write integration tests
  [cc02] complete | claude-code | update documentation
```

---

### `reply_to_parent`

Send a message to the parent session that spawned this session. Used by sub-agents to report results back to the spawning agent without out-of-band coordination.

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `text` | string | Yes | Text to send as input to the parent session |
| `session_id` | string | No | ID of the child session (used to look up `parent_id`). If omitted, the tool has no child reference and will return an error. |

**Example response:**
```
Sent to parent session pp01.
```

**Usage pattern for sub-agents:**
```
# At spawn time, parent passes its own session ID:
start_session(task="...", caller_session_id="host-abc123")

# When the sub-agent finishes, it calls:
reply_to_parent(session_id="cc01", text="Task complete. Wrote 14 tests, all passing.")
```

---

### `restart_session`

Restart a session from any state (running, waiting_input, complete, failed, or killed). Kills the session if still alive, then relaunches with the same task. Session ID and name are preserved.

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `session_id` | string | No | Session ID to restart (required if `session_name` not provided) |
| `session_name` | string | No | Session name as alternative to `session_id`; resolves to the oldest active session with this name (BL354 name resolution) |
| `task` | string | No | Optional new task string. If omitted, the session's existing task is reused. |

**Example response:**
```
Restarted session myhost-a3f2 (state: running)
```

---

## Exit Hooks (BL356)

Exit hooks fire when a named session exits unexpectedly — either its `claude_alive` flag flips to false (zombie detection) or the session enters `failed`/`killed` state. Two actions are supported: `restart` (relaunch the session with the same task) and `notify` (send a message to another session).

### `exit_hook_list`

List all configured session crash/exit hooks.

**Parameters:** none

**Example response:**
```
Exit hooks (1):

ID:       eh-abc123
Name:     worker-main
Action:   restart
Enabled:  true
Cooldown: 300s
Created:  2026-06-01T10:00:00Z
```

---

### `exit_hook_add`

Add a session crash/exit hook.

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `name` | string | Yes | Session name to watch (exact match) |
| `action` | string | Yes | Action to take: `restart` or `notify` |
| `notify_session` | string | No | For `action=notify`: name of the session to send a message to |
| `notify_message` | string | No | For `action=notify`: message text to send (default: auto-generated) |
| `cooldown_seconds` | number | No | Minimum seconds between firings. Default: 300. |

**Example response:**
```json
{
  "id": "eh-abc123",
  "name": "worker-main",
  "action": "restart",
  "enabled": true,
  "cooldown_seconds": 300,
  "created_at": "2026-06-01T10:00:00Z"
}
```

---

### `exit_hook_delete`

Delete a session crash/exit hook by ID.

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `id` | string | Yes | Exit hook ID to delete |

**Example response:**
```
Exit hook eh-abc123 deleted.
```

---

### `exit_hook_enable`

Enable a previously disabled session crash/exit hook.

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `id` | string | Yes | Exit hook ID to enable |

**Example response:**
```
Exit hook eh-abc123 enabled.
```

---

### `exit_hook_disable`

Disable a session crash/exit hook without deleting it.

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `id` | string | Yes | Exit hook ID to disable |

**Example response:**
```
Exit hook eh-abc123 disabled.
```

---

## Work Queue (BL357)

A durable, role-based work queue for coordinating multi-agent pipelines. Agents push items for a named role; worker agents atomically claim the oldest pending item, then mark it complete or failed. Items persist across daemon restarts.

### `queue_push`

Push a new work item onto the role-based work queue.

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `role` | string | Yes | Role that worker agents claim items from (e.g. `"coder"`, `"reviewer"`) |
| `payload` | string | No | Optional JSON object payload for the work item. Example: `{"key":"value"}` |

**Example response:**
```json
{
  "id": "qi-abc123",
  "role": "coder",
  "state": "pending",
  "payload": {"key": "value"},
  "created_at": "2026-06-01T10:00:00Z"
}
```

---

### `queue_claim`

Atomically claim the oldest pending work item for a given role. Returns the item and sets a lease timer; if the item is not completed or failed within `lease_seconds`, it reverts to `pending`.

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `role` | string | Yes | Role to claim a work item for |
| `claimed_by` | string | Yes | Session FullID or identifier of the claiming worker |
| `lease_seconds` | number | No | How long (seconds) to hold the claim before it auto-expires back to pending. Default: 300. |

**Example response:**
```json
{
  "id": "qi-abc123",
  "role": "coder",
  "state": "claimed",
  "claimed_by": "myhost-a3f2",
  "payload": {"key": "value"}
}
```
Returns `"No pending items available for role: <role>"` when the queue is empty.

---

### `queue_complete`

Mark a claimed work item as complete.

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `id` | string | Yes | ID of the queue item to complete |
| `result` | string | No | Optional JSON object result payload. Example: `{"output":"done"}` |

**Example response:**
```
Queue item qi-abc123 marked complete.
```

---

### `queue_fail`

Mark a claimed work item as failed.

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `id` | string | Yes | ID of the queue item to fail |
| `error` | string | Yes | Error message describing why the item failed |

**Example response:**
```
Queue item qi-abc123 marked failed.
```

---

### `queue_list`

List work queue items, optionally filtered by role and/or state.

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `role` | string | No | Filter by role. Empty = all roles. |
| `state` | string | No | Filter by state: `pending`, `claimed`, `complete`, or `failed`. Empty = all states. |

**Example response:**
```json
[
  {
    "id": "qi-abc123",
    "role": "coder",
    "state": "pending",
    "created_at": "2026-06-01T10:00:00Z"
  }
]
```

---

## Discussion Subscribe (BL358)

These tools let sessions subscribe to discussions so that new entries written to a discussion are automatically delivered to the session via `send_input`.

### `discussion_subscribe`

Subscribe a session to a discussion. New entries written to the discussion are delivered to the session via `send_input`.

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `discussion_id` | string | Yes | Discussion scope ID to subscribe to |
| `session_name` | string | Yes | Name of the session to deliver discussion entries to |

**Example response:**
```json
{"ok":true,"discussion_id":"proj-alpha","session_name":"monitor","status":"subscribed"}
```

---

### `discussion_unsubscribe`

Unsubscribe a session from a discussion. The session will no longer receive new entries from the discussion.

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `discussion_id` | string | Yes | Discussion scope ID |
| `session_name` | string | Yes | Session name to remove from the discussion |

**Example response:**
```json
{"ok":true,"discussion_id":"proj-alpha","session_name":"monitor","status":"unsubscribed"}
```

---

### `discussion_subscriptions`

List all active discussion subscriptions.

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `discussion_id` | string | No | Filter by discussion ID. Empty = all subscriptions. |

**Example response:**
```json
[
  {"discussion_id": "proj-alpha", "session_name": "monitor"},
  {"discussion_id": "proj-alpha", "session_name": "logger"}
]
```

---

## Result Store (BL360)

A structured named-result store for agents to exchange data without out-of-band coordination. Results are keyed by name and support optional TTL-based expiry.

### `result_put`

Store a named result payload in the agent result store. Upserts by name.

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `name` | string | Yes | Unique name / key for this result entry |
| `payload` | string | Yes | JSON object payload to store. Example: `{"output":"done","count":3}` |
| `ttl_seconds` | number | No | Time-to-live in seconds. `0` (default) = no expiry. |

**Example response:**
```json
{
  "name": "build-result",
  "payload": {"output": "done", "count": 3},
  "stored_at": "2026-06-01T10:00:00Z",
  "expires_at": null
}
```

---

### `result_get`

Retrieve a named result from the agent result store.

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `name` | string | Yes | Name / key of the result to retrieve |

**Example response:**
```json
{
  "name": "build-result",
  "payload": {"output": "done", "count": 3},
  "stored_at": "2026-06-01T10:00:00Z",
  "expires_at": null
}
```
Returns `"Not found: <name>"` when the key does not exist or has expired.

---

### `result_list`

List results in the agent result store. Optionally filter by name prefix.

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `prefix` | string | No | Filter results whose name starts with this prefix. Empty = all. |

**Example response:**
```json
[
  {"name": "build-result", "stored_at": "2026-06-01T10:00:00Z"},
  {"name": "build-logs",   "stored_at": "2026-06-01T10:01:00Z"}
]
```

---

### `result_delete`

Delete a named result from the agent result store.

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `name` | string | Yes | Name / key of the result to delete |

**Example response:**
```
Deleted: build-result
```

---

## Example Workflows in Cursor

Once configured, you can ask Claude in Cursor:

**Start a task:**
```
Start a datawatch session to write unit tests for the auth package in /home/me/myproject
```
→ Claude calls `start_session(task="write unit tests...", project_dir="/home/me/myproject")`

**Check progress:**
```
What's the status of my sessions?
```
→ Claude calls `list_sessions` and `session_output` for each active session

**Reply to a prompt:**
```
Session a3f2 is waiting — tell it yes, overwrite the file
```
→ Claude calls `send_input(session_id="a3f2", text="y")`

**Kill a runaway session:**
```
Kill session b7c1
```
→ Claude calls `kill_session(session_id="b7c1")`

---

## Remote AI Agent Example

A remote AI agent (running in a cloud function, GitHub Action, or autonomous pipeline)
can use the SSE transport to manage sessions on your machine:

```python
# Example: autonomous agent triggers a coding task via MCP
import anthropic

client = anthropic.Anthropic()

# The agent has access to datawatch MCP tools
response = client.messages.create(
    model="claude-opus-4-6",
    max_tokens=1024,
    tools=[...],  # MCP tools from datawatch SSE server
    messages=[{
        "role": "user",
        "content": "Start a session to run database migrations in /opt/myapp, wait for it to complete, and report the result."
    }]
)
```

The agent will:
1. Call `start_session` to kick off the task
2. Poll `session_output` to monitor progress
3. Call `send_input` if confirmation is needed
4. Return the final output when the session completes

---

## Configuration Reference

Full MCP config block:

```yaml
mcp:
  enabled: true            # enable MCP server (default: true)
  sse_enabled: false       # enable HTTP/SSE transport for remote clients
  sse_host: "0.0.0.0"     # SSE server bind address (default: 0.0.0.0)
  sse_port: 8081           # SSE server port (default: 8081)
  token: ""                # bearer token for SSE connections (strongly recommended)
  tls_enabled: false       # enable TLS for SSE server
  tls_auto_generate: true  # auto-generate self-signed cert (default: true when tls_enabled)
  tls_cert: ""             # path to PEM cert (overrides auto-generate)
  tls_key: ""              # path to PEM key (overrides auto-generate)
```

The `datawatch mcp` CLI command:

```bash
# stdio mode (for IDE clients)
datawatch mcp

# SSE mode (for remote clients, overrides config)
datawatch mcp --sse
```
