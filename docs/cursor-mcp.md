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
| `start_session` | Start a new AI session (supports `caller_session_id`, `kill_children`, `permission_mode`) |
| `session_output` | Get the last N lines of output from a session |
| `send_input` | Send a reply to a session waiting for input |
| `kill_session` | Terminate a session |
| `session_children` | List child sessions spawned by a parent session (v8.10.0) |
| `reply_to_parent` | Send a message back to the spawning parent session (v8.10.0) |
| `autonomous_prd_create` / `autonomous_prd_decompose` / `autonomous_prd_run` | Autonomous PRD lifecycle |
| `question_raise` | raise an operator question to this daemon's operator (the operator-question channel) |
| `question_list` | list the daemon's stored operator questions, optionally filtered (the poll read) |
| `question_get` | read one stored question and its current answer (the asker's return path) |
| `question_answer` | answer a stored question by option id and/or the operator's own words; a changed mind supersedes, never edits |
| `question_withdraw` | withdraw an open question with a reason, attributed to a named actor |
| `autonomous_prd_children` | List child PRDs spawned via `Task.SpawnPRD` |
| `autonomous_prd_set_kind` | Set an Automaton's kind: `feature`, `bug` or `other` (empty derives it again) |
| `autonomous_progress` | Read the progress metrics object: counts by kind, throughput, utilization, remaining work and the completion range; optional `automation_id`, `tag_id`, `since`, `until` |
| `autonomous_progress_trend` | Read the progress trend from the saved daily snapshots; optional `days` (1 to 365, default 30) |
| `autonomous_lane_fill_status` | Read the lane fill status: lanes, idle state, recent planner decisions and idle alerts; optional `limit` (1 to 1000, default 50) |
| `autonomous_prd_set_lane_preference` | Set an Automaton's lane to `gpu`, `claude` or `any` (empty means `any`); takes `id` and `lane_preference` |
| `autonomous_triage` | Read the triage records: one record per failed or blocked Automaton, newest first, with its identity, the failed or blocked task and its error, the session end reason, the retry budgets and the append-only notes — beside `alert_minutes`, the triage alert threshold currently in effect; optional `automation_id`, `prd_id` and `status` (`failed` or `blocked`) |
| `autonomous_merge` | Read the Automaton merge state: the `merge_pending_mode` in effect (`off`, `shadow` or `on`), the alert threshold in minutes (`alert_minutes`, `0` = off), one entry per finished Automaton with a work branch and its state (`merged`, `pending`, `needs_look`, `empty` or `unknown`), and in `shadow` mode the decisions the check would have acted on; takes no parameters, read-only, nothing is merged, deleted or pushed |
| `autonomous_safe_to_delete` | Read the safe-to-delete report for Automaton work branches and worktrees: `safe` (can be cleaned up), `held` (must stay: not merged yet, needs a look, or a worktree with uncommitted changes) and `held_dirty`; takes no parameters, read-only, never deletes anything |
| `autonomous_prd_rejudge` | Re-judge the completion guardrail of a blocked Automaton whose tasks are all completed, without resetting any task — pass unblocks it, block keeps it blocked; refused otherwise |
| `autonomous_prd_set_failure_escalation` | Set or clear the failure escalation override on an Automaton or one task (`policy` object, or `clear=true`) |
| `autonomous_prd_resolve_task_review` | Retry (optionally with a changed spec, worker or file list) or cancel a task held for review |
| `autonomous_prd_set_task_verification_llm` / `autonomous_prd_set_story_verification_llm` / `autonomous_prd_set_verification_llm` | Pick the LLM that verifies one task / one story / a whole Automaton (`verification_backend`, `verification_model`, `verification_effort`; `""` clears, omit = unchanged). Precedence task > story > Automaton > global (v9.0.69) |
| `autonomous_prd_set_notify_events` | Set which lifecycle event kinds notify for one Automaton (`notify_events`, e.g. `["prd.blocked","task.session_started"]`); empty or omitted clears the Automaton-level set. Default set: `prd.needs_review`, `prd.blocked`, `prd.failed`, `prd.completed`, `prd.cancelled`; `task.session_started`, `task.session_cleared`, `scope_warning` and `guardrail_warn` are opt-in. Precedence: Automaton > guardrail profile > global default; a subscription filter can only narrow |
| `guardrail_profile_create` / `guardrail_profile_update` | Optional `notify_events_json` (JSON array of event kinds) sets the profile tier of the same notify_events precedence chain; on update omitted = unchanged, `"[]"` = clear; invalid JSON → error |
| `observer_stats` / `observer_envelopes` | Per-session + per-LLM-backend stats |
| `observer_envelopes_all_peers` | Federation-aware envelope view with cross-host caller attribution |
| `memory_remember` / `memory_recall` / `kg_query` | Episodic memory + knowledge graph |
| `orchestrator_graph_run` / `orchestrator_verdicts` | PRD-DAG orchestrator |
| `automation_list` / `automation_get` / `automation_create` / `automation_update` / `automation_delete` / `automation_set_llm` / `automation_set_verification_llm` | Automation records — a chain of Automatons with per-outcome edges, entry member, optional budget policy and execution/verification defaults (data records only — no automatic advance yet). `automation_set_llm` and `automation_set_verification_llm` set the Automation's default execution, planning and verification LLMs |
| `pipeline_start` / `pipeline_status` | Session chaining |
| `exit_hook_list` / `exit_hook_add` / `exit_hook_delete` / `exit_hook_enable` / `exit_hook_disable` | Session exit hooks (BL356) |
| `queue_push` / `queue_claim` / `queue_complete` / `queue_fail` / `queue_list` | Distributed work queue (BL357) |
| `discussion_subscribe` / `discussion_unsubscribe` / `discussion_subscriptions` | Discussion subscriptions (BL358) |
| `usage_advice` / `usage_readings` | Account usage calculator: the one advice result (lane choice, headroom, projected cycle end, run-out warnings, finish-early estimate, plain-English advice block) and the shared usage readings series |
| `usage_reading_add` / `usage_reading_delete` | Record (percent or spend, when, session percent, lanes) or delete one reading of the shared series |
| `usage_extra_credit_set` / `usage_remaining_work_set` | Set the bought extra credit (shared setting) and the typed remaining work (daemon-local, run-flat-out policy) |
| `result_put` / `result_get` / `result_list` / `result_delete` | Result store (BL360) |
| `post_result` / `schedule_results_list` | Scheduled-job result panel — post a session's structured result (same contract as `POST /api/sessions/{id}/result`) and list per-schedule retained history (same as `GET /api/schedule-results`) |
| `channel_info` | Channel bridge status for the current session |
| `channel_diagnostics` | Full MCP channel bridge health — all sessions, bridge kind/path, per-session liveness, hints (BL362) |
| `get_my_scope` | Self-scope confirmation: own identity, per-session MCP server name, mode (host vs container), and every session's name/state (BL463) |
| `vision_describe` | Describe an image file using the configured vision model (requires `vision.enabled: true`) |
| `web_search_stats` | Web search configuration and stats — provider, URL, engine, query/error counters (requires `web_search.enabled: true`) |

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
