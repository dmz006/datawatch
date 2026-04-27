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

The MCP server exposes **100+ tools** across the surfaces below. The
authoritative live list is served at `GET /api/mcp/docs` (HTML) or
`GET /api/mcp/docs?format=json` (JSON) — `claude mcp list` queries
this on connect, and the PWA Settings → About → "MCP tools" link
opens it. The selected tools below are documented inline; the rest
follow the same parameter-table-and-example shape.

### Tool families (high-level)

| Family | Examples |
|--------|----------|
| Sessions | `list_sessions`, `start_session`, `send_input`, `copy_response`, `kill_session`, `delete_session`, `restart_session`, `rename_session`, `session_output`, `session_timeline` |
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
| Voice | (no tools yet — REST `/api/voice/transcribe` + chat-channel auto-handle) |

Tools added in v5.9 → v5.19:

- **`autonomous_prd_children`** (v5.9.0, BL191 Q4) — list child PRDs spawned from a parent's `Task.SpawnPRD` shortcuts.
- **`observer_envelopes_all_peers`** (v5.12.0, BL180 cross-host) — federation-aware envelope view; cross-peer caller attribution surfaces as `<peer>:<envelope-id>` rows on each matched server envelope.



### `list_sessions`

List all active AI coding sessions on this host.

**Parameters:** none

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
