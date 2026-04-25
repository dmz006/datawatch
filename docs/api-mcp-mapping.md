# API ↔ MCP Tool Mapping

This document maps every REST API endpoint to its MCP tool equivalent,
documenting coverage gaps and the reasoning behind them.

## Full Mapping

### Session Management

| API Endpoint | MCP Tool | Status | Notes |
|-------------|----------|--------|-------|
| `GET /api/sessions` | `list_sessions` | Complete | |
| `POST /api/sessions/start` | `start_session` | Complete | |
| `GET /api/output?id=&n=` | `session_output` | Complete | |
| `GET /api/sessions/timeline` | `session_timeline` | Complete | |
| `POST /api/sessions/kill` | `kill_session` | Complete | |
| `POST /api/sessions/rename` | `rename_session` | Complete | |
| `POST /api/sessions/restart` | `restart_session` | Complete | v2.2.1 |
| `POST /api/sessions/delete` | `delete_session` | Complete | v2.2.1 |
| `POST /api/sessions/state` | — | Not in MCP | Internal use: manual state override. Not useful for MCP clients |
| `POST /api/command` | `send_input` | Partial | API accepts raw command strings; MCP has typed `send_input` for session input. Use `send_saved_command` for named commands |
| `POST /api/sessions/bind` | `session_bind_agent` | Complete | F10 — bind a session to an agent worker |
| `POST /api/sessions/import` | `session_import` | Complete | BL94 — import an orphan session from disk |
| `GET /api/sessions/reconcile` | `session_reconcile` | Complete | BL93 — list orphan session dirs |
| `POST /api/sessions/{id}/rollback` | `session_rollback` | Complete | BL29 — roll back to pre-session checkpoint |
| `GET /api/sessions/stale` | `sessions_stale` | Complete | BL40 — running sessions stuck longer than threshold |
| `GET /api/sessions/aggregated` | — | Not in MCP | Multi-server aggregation — proxy/federation feature |

### Memory

| API Endpoint | MCP Tool | Status | Notes |
|-------------|----------|--------|-------|
| `GET /api/memory/stats` | `memory_stats` | Complete | |
| `GET /api/memory/list` | `memory_list` | Complete | |
| `GET /api/memory/search?q=` | `memory_recall` | Complete | |
| `POST /api/memory/delete` | `memory_forget` | Complete | |
| `GET /api/memory/export` | `memory_export` | Complete | v2.2.1 |
| `POST /api/memory/import` | — | Not in MCP | Import is a file upload — MCP tools return text, not receive file uploads. Use API directly |
| `GET /api/memory/test` | — | Not in MCP | Pre-enable test — used by web UI toggle. MCP clients don't enable/disable features |
| `GET /api/memory/wal` | — | Not in MCP | Audit log — operational debugging tool. Low value for MCP clients |

### Knowledge Graph

| API Endpoint | MCP Tool | Status | Notes |
|-------------|----------|--------|-------|
| `GET /api/memory/kg/query` | `kg_query` | Complete | |
| `POST /api/memory/kg/add` | `kg_add` | Complete | |
| `POST /api/memory/kg/invalidate` | `kg_invalidate` | Complete | |
| `GET /api/memory/kg/timeline` | `kg_timeline` | Complete | |
| `GET /api/memory/kg/stats` | `kg_stats` | Complete | |

### Intelligence

| API Endpoint | MCP Tool | Status | Notes |
|-------------|----------|--------|-------|
| — (comm channel) | `research_sessions` | MCP only | Cross-session research. No dedicated API endpoint (uses memory search + KG + session scan) |
| `GET /api/sessions/response` | `copy_response` | Complete | |
| `GET /api/sessions/prompt` | `get_prompt` | Complete | |

### Monitoring

| API Endpoint | MCP Tool | Status | Notes |
|-------------|----------|--------|-------|
| `GET /api/stats` | `get_stats` | Complete | v2.2.1 |
| `GET /api/ollama/stats` | `ollama_stats` | Complete | |
| `GET /healthz` | — | Not in MCP | Kubernetes probe — not useful for AI clients |
| `GET /readyz` | — | Not in MCP | Kubernetes probe |
| `GET /metrics` | — | Not in MCP | Prometheus scrape endpoint — not for MCP |

### Configuration

| API Endpoint | MCP Tool | Status | Notes |
|-------------|----------|--------|-------|
| `GET /api/config` | `get_config` | Complete | v2.2.1 |
| `PUT /api/config` | — | Not in MCP | Config changes should be deliberate human actions, not AI-driven. Security boundary: AI agents shouldn't modify daemon config |
| `GET /api/backends` | — | Not in MCP | Backend list is static — MCP clients can read from `get_config` section |
| `GET /api/info` | `get_version` | Complete | Version + hostname |

### Scheduling

| API Endpoint | MCP Tool | Status | Notes |
|-------------|----------|--------|-------|
| `GET /api/schedules` | `schedule_list` | Complete | |
| `POST /api/schedules` | `schedule_add` | Complete | |
| `DELETE /api/schedules` | `schedule_cancel` | Complete | |

### Commands & Filters

| API Endpoint | MCP Tool | Status | Notes |
|-------------|----------|--------|-------|
| `GET /api/commands` | `list_saved_commands` | Complete | |
| — | `send_saved_command` | MCP only | Sends named command to session |
| `GET /api/filters` | — | Not in MCP | Output filters are operational config — not useful for AI clients |
| `POST /api/filters` | — | Not in MCP | Same reasoning |

### Alerts

| API Endpoint | MCP Tool | Status | Notes |
|-------------|----------|--------|-------|
| `GET /api/alerts` | `get_alerts` | Complete | |
| `POST /api/alerts` (mark read) | `mark_alert_read` | Complete | |

### Autonomous PRD Decomposition (BL24+BL25 — v3.10.0)

| API Endpoint | MCP Tool | Status | Notes |
|-------------|----------|--------|-------|
| `GET /api/autonomous/status` | `autonomous_status` | Complete | |
| `GET /api/autonomous/config` | `autonomous_config_get` | Complete | |
| `PUT /api/autonomous/config` | `autonomous_config_set` | Complete | |
| `GET /api/autonomous/prds` | `autonomous_prd_list` | Complete | |
| `POST /api/autonomous/prds` | `autonomous_prd_create` | Complete | |
| `GET /api/autonomous/prds/{id}` | `autonomous_prd_get` | Complete | |
| `DELETE /api/autonomous/prds/{id}` | `autonomous_prd_cancel` | Complete | |
| `POST /api/autonomous/prds/{id}/decompose` | `autonomous_prd_decompose` | Complete | |
| `POST /api/autonomous/prds/{id}/run` | `autonomous_prd_run` | Complete | |
| `GET /api/autonomous/learnings` | `autonomous_learnings` | Complete | |

### Observer peers (BL172 — v4.4.0+)

| API Endpoint | MCP Tool | Status | Notes |
|-------------|----------|--------|-------|
| `GET /api/observer/peers` | `observer_peers_list` | Complete | Federated Shape A / B / C peers |
| `POST /api/observer/peers` | `observer_peer_register` | Complete | Mints token (only opportunity) |
| `GET /api/observer/peers/{name}` | `observer_peer_get` | Complete | TokenHash redacted |
| `GET /api/observer/peers/{name}/stats` | `observer_peer_stats` | Complete | Last-known StatsResponse v2 |
| `DELETE /api/observer/peers/{name}` | `observer_peer_delete` | Complete | Auto re-register on next push |
| `GET /api/observer/peers/{agent_id}` | `observer_agent_stats` | Complete | S13 alias — agent_id is the peer name for F10 workers |
| `GET /api/observer/peers` (filtered shape=A) | `observer_agent_list` | Complete | S13 alias — agent peers in the federation |

### Observer — unified stats + sub-process monitoring (BL171 — v4.1.0)

| API Endpoint | MCP Tool | Status | Notes |
|-------------|----------|--------|-------|
| `GET /api/observer/stats` | `observer_stats` | Complete | StatsResponse v2 snapshot |
| `GET /api/observer/envelopes` | `observer_envelopes` | Complete | per-session + per-backend rollup |
| `GET /api/observer/envelope?id=` | `observer_envelope` | Complete | drill-down to one envelope's tree |
| `GET /api/observer/config` | `observer_config_get` | Complete | |
| `PUT /api/observer/config` | `observer_config_set` | Complete | |
| `GET /api/stats?v=2` | `observer_stats` | Alias | Negotiates v2 on the existing stats path |

### PRD-DAG Orchestrator (BL117 — v4.0.0)

| API Endpoint | MCP Tool | Status | Notes |
|-------------|----------|--------|-------|
| `GET /api/orchestrator/config` | `orchestrator_config_get` | Complete | |
| `PUT /api/orchestrator/config` | `orchestrator_config_set` | Complete | |
| `GET /api/orchestrator/graphs` | `orchestrator_graph_list` | Complete | |
| `POST /api/orchestrator/graphs` | `orchestrator_graph_create` | Complete | |
| `GET /api/orchestrator/graphs/{id}` | `orchestrator_graph_get` | Complete | |
| `DELETE /api/orchestrator/graphs/{id}` | `orchestrator_graph_cancel` | Complete | |
| `POST /api/orchestrator/graphs/{id}/plan` | `orchestrator_graph_plan` | Complete | |
| `POST /api/orchestrator/graphs/{id}/run` | `orchestrator_graph_run` | Complete | Fire-and-forget |
| `GET /api/orchestrator/verdicts` | `orchestrator_verdicts` | Complete | |

### Cost tracking (BL6)

| API Endpoint | MCP Tool | Status | Notes |
|-------------|----------|--------|-------|
| `GET /api/cost` | `cost_summary` | Complete | Token + USD rollup |
| `GET /api/cost/usage` | `cost_usage` | Complete | Per-session detail |
| `GET /api/cost/rates` | `cost_rates` | Complete | Per-model price-per-token |

### Cooldown (BL30)

| API Endpoint | MCP Tool | Status | Notes |
|-------------|----------|--------|-------|
| `GET /api/cooldown` | `cooldown_status` | Complete | Global rate-limit pause state |
| `PUT /api/cooldown` | `cooldown_set` | Complete | Set cooldown (G/P/D scopes) |
| `DELETE /api/cooldown` | `cooldown_clear` | Complete | Clear cooldown |

### Routing rules (BL20)

| API Endpoint | MCP Tool | Status | Notes |
|-------------|----------|--------|-------|
| `GET /api/routing-rules` | `routing_rules_list` | Complete | Backend auto-selection rules |
| `POST /api/routing-rules/test` | `routing_rules_test` | Complete | Test which backend a task would route to |

### Templates (BL5)

| API Endpoint | MCP Tool | Status | Notes |
|-------------|----------|--------|-------|
| `GET /api/templates` | `template_list` | Complete | |
| `POST /api/templates` | `template_upsert` | Complete | |
| `DELETE /api/templates` | `template_delete` | Complete | |

### Project aliases (BL27)

| API Endpoint | MCP Tool | Status | Notes |
|-------------|----------|--------|-------|
| `GET /api/projects` | `project_list` | Complete | |
| `POST /api/projects` | `project_upsert` | Complete | |
| `DELETE /api/projects` | `project_alias_delete` | Complete | |
| `GET /api/project/summary?dir=` | `project_summary` | Complete | BL35 — git status + sessions + stats |

### Device aliases (BL31)

| API Endpoint | MCP Tool | Status | Notes |
|-------------|----------|--------|-------|
| `GET /api/device-aliases` | `device_alias_list` | Complete | For `new: @<alias>:` routing |
| `POST /api/device-aliases` | `device_alias_upsert` | Complete | |
| `DELETE /api/device-aliases` | `device_alias_delete` | Complete | |

### Profiles (Project + Cluster, F10)

| API Endpoint | MCP Tool | Status | Notes |
|-------------|----------|--------|-------|
| `GET /api/profiles` | `profile_list` | Complete | |
| `GET /api/profiles/{name}` | `profile_get` | Complete | |
| `POST /api/profiles` | `profile_create` | Complete | |
| `PUT /api/profiles/{name}` | `profile_update` | Complete | |
| `DELETE /api/profiles/{name}` | `profile_delete` | Complete | |
| `POST /api/profiles/{name}/smoke` | `profile_smoke` | Complete | Smoke-test a profile |

### Agents (F10 sprints 3+)

| API Endpoint | MCP Tool | Status | Notes |
|-------------|----------|--------|-------|
| `GET /api/agents` | `agent_list` | Complete | |
| `POST /api/agents` | `agent_spawn` | Complete | |
| `GET /api/agents/{id}` | `agent_get` | Complete | |
| `GET /api/agents/{id}/logs` | `agent_logs` | Complete | |
| `DELETE /api/agents/{id}` | `agent_terminate` | Complete | |
| `GET /api/agents/audit` | `agent_audit` | Complete | BL107 — agent audit log |

### Singletons (BL9, BL12, BL17, BL34, BL37, BL42, BL69)

| API Endpoint | MCP Tool | Status | Notes |
|-------------|----------|--------|-------|
| `POST /api/ask` | `ask` | Complete | BL34 — single-shot LLM ask |
| `POST /api/assist` | `assist` | Complete | BL42 — quick-response assistant |
| `GET /api/audit` | `audit_query` | Complete | BL9 — operator-action log |
| `GET /api/analytics` | `analytics` | Complete | BL12 — session counts, backend distribution |
| `POST /api/reload` | `reload` | Complete | BL17 — hot config reload |
| `GET /api/diagnose` | `diagnose` | Complete | BL37 — backend reachability + recent errors |
| `GET /api/splash/info` | `splash_info` | Complete | BL69 |

### Voice + Channel (issue #2 / BL174)

| API Endpoint | MCP Tool | Status | Notes |
|-------------|----------|--------|-------|
| `POST /api/voice/transcribe` | — | Not in MCP | File upload — MCP tools don't accept binary blobs. Use REST directly |
| `POST /api/channel/reply` | — | Not in MCP | Internal MCP-bridge callback (channel.js / datawatch-channel) |

### Plugin Framework (BL33 — v3.11.0)

| API Endpoint | MCP Tool | Status | Notes |
|-------------|----------|--------|-------|
| `GET /api/plugins` | `plugins_list` | Complete | |
| `POST /api/plugins/reload` | `plugins_reload` | Complete | |
| `GET /api/plugins/{name}` | `plugin_get` | Complete | |
| `POST /api/plugins/{name}/enable` | `plugin_enable` | Complete | |
| `POST /api/plugins/{name}/disable` | `plugin_disable` | Complete | |
| `POST /api/plugins/{name}/test` | `plugin_test` | Complete | Synthetic hook invocation for debugging |

### System Operations

| API Endpoint | MCP Tool | Status | Notes |
|-------------|----------|--------|-------|
| `POST /api/restart` | `restart_daemon` | Complete | |
| `POST /api/update` | — | Not in MCP | Binary update — security-sensitive, must be human-initiated |
| — | `stop_all_sessions` | MCP only | Kill all running sessions |
| — | `memory_reindex` | MCP only | Re-embed all memories |

### Infrastructure (Not in MCP — by design)

| API Endpoint | Reason not in MCP |
|-------------|-------------------|
| `GET /api/interfaces` | Network interface list — operational, not for AI |
| `GET /api/logs` | Daemon log viewer — debugging tool |
| `GET /api/servers` | Remote server list — proxy config |
| `GET /api/servers/health` | Server health — operational monitoring |
| `GET /api/profiles` | Profile CRUD — config management |
| `POST /api/link/start` | Signal device linking — interactive setup |
| `GET /api/link/status` | Signal link status |
| `GET /api/link/stream` | SSE stream for linking |
| `GET /api/files` | File browser — web UI utility |
| `GET /api/mcp/docs` | MCP tool documentation — meta |
| `POST /api/test/message` | Comm channel test — debugging |
| `POST /api/channel/*` | MCP channel internals |
| `GET /api/ollama/models` | Model list for web UI dropdowns |
| `GET /api/openwebui/models` | Model list for web UI dropdowns |
| `GET /api/rtk/discover` | RTK optimization analysis |
| `POST /api/stats/kill-orphans` | Kill orphaned tmux — operational |
| `GET /api/proxy/*` | Proxy forwarding — infrastructure |
| `GET /api/sessions/aggregated` | Multi-server aggregation — proxy feature |

## Summary

| Category | API Endpoints | MCP Tools | Coverage |
|----------|--------------|-----------|----------|
| Sessions (incl. bind/import/reconcile/rollback/stale) | 16 | 14 | **87%** |
| Memory | 8 | 7 | **88%** (import is file-based) |
| Knowledge Graph | 5 | 5 | **100%** |
| Intelligence | 3 | 3 | **100%** |
| Monitoring | 4 | 2 | **50%** (probes are k8s-specific) |
| Config | 3 | 1 | **33%** (writes are security boundary) |
| Scheduling | 3 | 3 | **100%** |
| Commands | 2 | 2 | **100%** |
| Alerts | 2 | 2 | **100%** |
| Operations | 2 | 2 | **100%** |
| Cost (BL6) | 3 | 3 | **100%** |
| Cooldown (BL30) | 3 | 3 | **100%** |
| Routing rules (BL20) | 2 | 2 | **100%** |
| Templates (BL5) | 3 | 3 | **100%** |
| Project aliases (BL27 + BL35) | 4 | 4 | **100%** |
| Device aliases (BL31) | 3 | 3 | **100%** |
| Profiles (F10) | 6 | 6 | **100%** |
| Agents (F10) | 6 | 6 | **100%** |
| Singletons (BL9/12/17/34/37/42/69) | 7 | 7 | **100%** |
| Voice + Channel | 2 | 0 | **0%** (file uploads + internal callbacks) |
| Autonomous (BL24+BL25) | 10 | 10 | **100%** |
| Observer (BL171) | 4 | 4 | **100%** |
| Plugins (BL33) | 6 | 6 | **100%** |
| Orchestrator (BL117) | 9 | 9 | **100%** |
| Infrastructure | 17 | 0 | **0%** (by design) |
| **Total** | **132** | **102** | **77%** (100% of user-facing features that fit MCP's tool model) |

All user-facing features have MCP coverage. The 30 endpoints without MCP tools
are infrastructure, operational, file-upload, or security-sensitive operations
that should remain human-controlled or don't fit MCP's text-in / text-out
contract.
