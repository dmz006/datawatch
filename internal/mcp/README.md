# datawatch MCP Server

Model Context Protocol (MCP) server for AI coding session management.

## Transports

- **stdio**: Run as a subprocess from Cursor, Claude Desktop, or any MCP client
- **HTTP/SSE**: Remote AI clients connect over HTTPS

## Configuration

```yaml
mcp:
  enabled: true
  sse_host: 0.0.0.0
  sse_port: 8081
  tls_enabled: false
  token: ""  # optional bearer token for SSE transport
```

## Tools (17)

| Tool | Description |
|------|-------------|
| `list_sessions` | List all AI coding sessions on this host |
| `start_session` | Start a new AI session (params: task, project_dir) |
| `session_output` | Get last N lines of output (params: session_id, lines) |
| `session_timeline` | Get structured event timeline (params: session_id) |
| `send_input` | Send text input to a waiting session (params: session_id, text) |
| `kill_session` | Terminate a session (params: session_id) |
| `rename_session` | Set session name (params: session_id, name) |
| `stop_all_sessions` | Kill all running/waiting sessions |
| `get_alerts` | List recent system alerts (params: session_id, limit) |
| `mark_alert_read` | Mark alert(s) as read (params: id, all) |
| `restart_daemon` | Restart the datawatch daemon |
| `get_version` | Get current and latest version info |
| `list_saved_commands` | List the saved command library |
| `send_saved_command` | Send a named command to a session (params: session_id, name) |
| `schedule_add` | Schedule a command (params: session_id, command, run_at) |
| `schedule_list` | List pending scheduled commands |
| `schedule_cancel` | Cancel a scheduled command (params: schedule_id) |

## API Documentation

- **HTML**: `GET /api/mcp/docs` (Accept: text/html)
- **JSON**: `GET /api/mcp/docs` (Accept: application/json)
- **MCP Discovery**: `tools/list` via MCP protocol (automatic in compatible clients)

## Usage with Cursor

```json
{
  "mcpServers": {
    "datawatch": {
      "command": "datawatch",
      "args": ["mcp", "--stdio"]
    }
  }
}
```

## Usage with Claude Desktop

```json
{
  "mcpServers": {
    "datawatch": {
      "command": "datawatch",
      "args": ["mcp", "--stdio"]
    }
  }
}
```

## Remote SSE Connection

```
SSE endpoint: https://host:8081/
Authorization: Bearer <token>
```
