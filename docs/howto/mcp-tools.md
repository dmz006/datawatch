# How-to: MCP tools (drive datawatch from Claude / Cursor / any MCP host)

datawatch exposes its operator surface as MCP tools — every REST
endpoint has a matching tool that an LLM can invoke. Wire it into
Claude Code (or any MCP-aware host) and you can ask the model to
list sessions, approve PRDs, fetch envelope stats, etc., without
leaving the chat.

## What's available

`/api/mcp/docs` returns the full live tool list as HTML; the JSON
form is at `/api/mcp/docs?format=json`. As of v5.6.x the surface
covers:

| Group | Tools |
|-------|-------|
| Sessions | `list_sessions`, `start_session`, `send_input`, `copy_response`, `kill_session`, `delete_session`, `restart_session`, `rename_session`, `session_output`, `session_timeline`, `session_reconcile`, `session_rollback`, `session_bind_agent`, `session_import`, `sessions_stale`, `stop_all_sessions` |
| Autonomous PRDs | `autonomous_status`, `autonomous_config_get/set`, `autonomous_prd_list/create/get/decompose/approve/reject/request_revision/edit_task/instantiate/run/cancel/set_llm/set_task_llm`, `autonomous_learnings` |
| Orchestrator | `orchestrator_graph_create/plan/run/get/list/cancel`, `orchestrator_verdicts`, `orchestrator_config_get/set` |
| Pipelines | `pipeline_start`, `pipeline_list`, `pipeline_status`, `pipeline_cancel` |
| Memory | `memory_remember`, `memory_recall`, `memory_list`, `memory_forget`, `memory_export/import`, `memory_reindex`, `memory_stats`, `memory_learnings`, `kg_add`, `kg_query`, `kg_timeline`, `kg_stats`, `kg_invalidate`, `research_sessions`, `get_prompt`, `copy_response` |
| Observer | `observer_stats`, `observer_envelopes`, `observer_envelope`, `observer_peers_list`, `observer_peer_get/register/delete`, `observer_peer_stats`, `observer_agent_list/stats`, `observer_config_get/set`, `ollama_stats` |
| Agents (ephemeral workers) | `agent_list`, `agent_get`, `agent_spawn`, `agent_terminate`, `agent_logs`, `agent_audit` |
| Plugins | `plugins_list`, `plugins_reload`, `plugin_get`, `plugin_test`, `plugin_enable`, `plugin_disable` |
| Profiles & projects | `profile_list/get/create/update/delete/smoke`, `project_list`, `project_summary`, `project_upsert`, `project_alias_delete` |
| Templates / scheduling | `template_list/upsert/delete`, `schedule_list/add/cancel`, `cooldown_status/set/clear` |
| Devices / routing | `device_alias_list/upsert/delete`, `routing_rules_list`, `routing_rules_test` |
| Cost / audit / config | `cost_summary`, `cost_usage`, `cost_rates`, `analytics`, `audit_query`, `get_config`, `config_set`, `get_stats`, `get_version`, `diagnose`, `reload`, `restart_daemon`, `splash_info` |
| Saved commands / alerts | `list_saved_commands`, `send_saved_command`, `get_alerts`, `mark_alert_read` |
| Ask / assist | `ask`, `assist` |
| Voice | (no voice tools yet — REST `/api/voice/transcribe` + chat-channel auto-handle voice notes) |

Per the configuration-parity rule, every MCP tool mirrors a REST
endpoint 1:1.

## 1. Wire datawatch as an MCP server in Claude Code

Two paths:

### (a) Native Go bridge (recommended, since v4.6.0)

```bash
claude mcp add --scope user datawatch /home/$USER/.local/bin/datawatch-channel
```

The `datawatch-channel` binary is shipped in every release and
speaks MCP over stdio. Verify:

```bash
claude mcp list
#  → datawatch: /home/.../datawatch-channel - ✓ Connected
```

### (b) Per-session bridge (auto-wired by the daemon)

When you `datawatch session start --backend claude-code …`, the
daemon registers a `datawatch-<session-id>` MCP server scoped to
that session so the model sees only that session's context. This
is automatic; no operator action required.

```bash
claude mcp list
#  → datawatch-ralfthewise-787e: /home/.../datawatch-channel - ✓ Connected
```

## 2. Use a tool from inside Claude Code

Once registered, in any Claude Code session ask the model to call
the tool by name — Claude Code surfaces them under the registered
server (`datawatch.list_sessions`, `datawatch.autonomous_prd_list`,
`datawatch.observer_envelopes`, etc.):

```
> list my datawatch sessions
> show open autonomous PRDs
> dump the observer envelope tree for the last 30 seconds
```

(Tool-invocation style varies by MCP host; the underlying tool
names are the ones in the `What's available` table above.)

## 3. Use from Cursor

```bash
# Cursor reads MCP servers from ~/.config/Cursor/User/mcp.json
cat > ~/.config/Cursor/User/mcp.json <<EOF
{
  "mcpServers": {
    "datawatch": {
      "command": "/home/$USER/.local/bin/datawatch-channel"
    }
  }
}
EOF
```

Restart Cursor; the tools show up under the model's tool palette.

## 4. Use from any MCP-aware Python client

```python
import mcp
client = mcp.Client(stdio="/home/me/.local/bin/datawatch-channel")
tools = client.list_tools()        # → ['session_list', ...]
out   = client.call_tool("session_list", {})
```

## 5. Per-session vs. user-scoped

| Scope | Tool name pattern | What it sees |
|-------|-------------------|--------------|
| User (global) | `datawatch.<tool>` | All sessions, all PRDs, full daemon |
| Per-session | `datawatch-<session-id>.<tool>` | This session only — stats, response, parent PRD |

Per-session tokens are short-lived and scoped, so a model spawned
in session X can't reach session Y.

## 6. Discovering what changed across releases

Every MCP tool has a matching REST endpoint. To see what's new in
the current daemon vs. an older release:

```bash
diff <(curl -sk https://localhost:8443/api/mcp/docs?format=json | jq -r '.tools[].name | tostring' | sort) \
     <(cat /tmp/old-tools-v4.9.3.txt | sort)
```

The PWA Settings → About → MCP tools link opens `/api/mcp/docs` in
the same tab so you can browse the live surface.

## Reachability across channels

| Channel | Action | Command |
|---------|--------|---------|
| CLI | register globally | `claude mcp add --scope user datawatch <path>` |
| CLI | start MCP server (stdio) | `datawatch mcp` |
| REST | discover | `GET /api/mcp/docs[?format=json]` |
| MCP | (this is the surface — every tool) | call `datawatch.<tool_name>` from the host |
| Chat | (no chat verbs needed — chat = command surface, MCP = tool surface) | — |
| PWA | discover | Settings → About → "MCP tools" link |

## See also

- [How-to: Setup + install](setup-and-install.md) — first-time daemon install (MCP tools register automatically)
- [How-to: Autonomous review + approve](autonomous-review-approve.md) — uses many `autonomous_prd_*` MCP tools end-to-end
- [`docs/mcp.md`](../mcp.md) — protocol-level reference
- [`docs/api-mcp-mapping.md`](../api-mcp-mapping.md) — REST ↔ MCP table
