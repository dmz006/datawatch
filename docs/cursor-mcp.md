# Cursor / Claude Desktop / VS Code MCP Integration

datawatch exposes an MCP (Model Context Protocol) server, letting Cursor and other
MCP-compatible clients list, start, and interact with AI coding sessions directly from
the IDE — no chat app required.

## Local Setup (Cursor / Claude Desktop)

The MCP server runs over stdin/stdout as a subprocess — no network port needed.

Add to your Cursor MCP config (`~/.cursor/mcp.json` or via **Cursor → Settings → MCP**):

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

For Claude Desktop (`~/.config/claude/claude_desktop_config.json`):

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

Restart Cursor/Claude Desktop after saving.

## Remote Setup (SSE — for remote AI agents)

The MCP SSE server allows **remote AI agents** (e.g. Claude API, OpenAI Assistants,
autonomous agents running elsewhere) to connect to datawatch over HTTPS and manage
sessions on your behalf.

Enable in `~/.datawatch/config.yaml`:

```yaml
mcp:
  sse_enabled: true
  sse_host: "0.0.0.0"
  sse_port: 8081
  token: "your-secret-token"      # Bearer token required for all SSE connections
  tls_enabled: true
  tls_auto_generate: true          # Auto-generates a self-signed cert in ~/.datawatch/tls/mcp/
```

Then start datawatch:

```bash
datawatch start   # starts SSE server alongside all other backends
# OR standalone:
datawatch mcp --sse
```

Remote AI client config (OpenAI Assistants, Claude API tool use, etc.):

```json
{
  "mcpServers": {
    "datawatch": {
      "url": "https://your-host:8081/sse",
      "headers": {
        "Authorization": "Bearer your-secret-token"
      }
    }
  }
}
```

### TLS and Post-Quantum Cryptography

When `tls_enabled: true`:

- TLS 1.3 is enforced (minimum version)
- Post-quantum hybrid key exchange (X25519Kyber768Draft00) is negotiated automatically
  by Go 1.23+ when a compatible client connects — no extra config needed
- A self-signed certificate is auto-generated at `~/.datawatch/tls/mcp/cert.pem`
  and persisted across restarts (valid 10 years)
- To use a CA-signed certificate, set `tls_cert` and `tls_key` paths instead

The same TLS configuration applies to the PWA server (`server.tls_enabled: true`).

## Remote Use via SSH (no port exposure needed)

If datawatch runs on a remote server, use SSH stdio forwarding for local IDE clients:

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

No ports to open — the MCP protocol runs over the SSH connection.

## Available Tools

datawatch exposes 100+ MCP tools across sessions, autonomous PRDs,
orchestrator, pipelines, memory, observer, agents, plugins,
profiles, templates, schedule, cooldown, devices, routing, cost,
audit, alerts. The authoritative live list is at
`GET /api/mcp/docs` (HTML or `?format=json`); see
[docs/mcp.md](mcp.md) for the family breakdown + per-tool detail.

Selected core tools:

| Tool | Description |
|------|-------------|
| `list_sessions` | List all active AI sessions on this host |
| `start_session` | Start a new AI session for a task |
| `session_output` | Get the last N lines of output from a session |
| `send_input` | Send a reply to a session waiting for input |
| `kill_session` | Terminate a session |
| `autonomous_prd_create` / `autonomous_prd_decompose` / `autonomous_prd_run` | BL24 autonomous PRD lifecycle |
| `autonomous_prd_children` | List child PRDs spawned via `Task.SpawnPRD` (v5.9.0) |
| `observer_stats` / `observer_envelopes` | Per-session + per-LLM-backend stats |
| `observer_envelopes_all_peers` | Federation-aware envelope view with cross-host caller attribution (v5.12.0) |
| `memory_remember` / `memory_recall` / `kg_query` | Episodic memory + knowledge graph |
| `orchestrator_graph_run` / `orchestrator_verdicts` | BL117 PRD-DAG orchestrator |
| `pipeline_start` / `pipeline_status` | F15 session chaining |

## Example Usage in Cursor

Once configured, you can ask Claude in Cursor:

```
Start a session to write unit tests for my auth package at /home/me/myproject
```

Claude will call `start_session` with your task, then use `session_output` to
follow the progress — all without leaving Cursor.

```
What sessions are running?
```
→ calls `list_sessions`

```
Session a3f2 is waiting — tell it yes, proceed with the changes
```
→ calls `send_input(session_id="a3f2", text="yes, proceed with the changes")`

## Remote AI Agent Example

A remote AI agent (e.g. running in a cloud function) can:

1. Connect to `https://your-server:8081/sse` with bearer token
2. Call `start_session` to kick off a coding task
3. Poll `session_output` to monitor progress
4. Call `send_input` when the session asks for confirmation
5. Call `list_sessions` to check all active sessions

This enables fully autonomous AI workflows that delegate coding tasks to
datawatch-managed AI sessions on your development machines.
