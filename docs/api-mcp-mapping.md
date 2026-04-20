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
| Sessions | 10 | 10 | **100%** |
| Memory | 8 | 7 | **88%** (import is file-based) |
| Knowledge Graph | 5 | 5 | **100%** |
| Intelligence | 3 | 3 | **100%** |
| Monitoring | 4 | 2 | **50%** (probes are k8s-specific) |
| Config | 3 | 1 | **33%** (writes are security boundary) |
| Scheduling | 3 | 3 | **100%** |
| Commands | 2 | 2 | **100%** |
| Alerts | 2 | 2 | **100%** |
| Operations | 2 | 2 | **100%** |
| Autonomous (BL24+BL25) | 10 | 10 | **100%** |
| Plugins (BL33) | 6 | 6 | **100%** |
| Orchestrator (BL117) | 9 | 9 | **100%** |
| Infrastructure | 17 | 0 | **0%** (by design) |
| **Total** | **84** | **62** | **74%** (100% of user-facing features) |

All user-facing features have MCP coverage. The 22 endpoints without MCP tools
are infrastructure, operational, or security-sensitive operations that should
remain human-controlled.
