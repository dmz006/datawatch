# API ↔ MCP Tool Mapping

This document maps every REST API endpoint to its MCP tool equivalent,
documenting coverage gaps and the reasoning behind them.

## Full Mapping

### Session Management

| API Endpoint | MCP Tool | Status | Notes |
|-------------|----------|--------|-------|
| `GET /api/sessions` | `list_sessions` | Complete | `name`/`state`/`backend`/`alive` filters match the REST params. `view` (`full` default, or `summary`), `states` and `since` map to the REST `view`/`states`/`since` params; using any of them returns the REST JSON array, plus a text note with the `X-Datawatch-Sessions-Omitted` count when the summary cap dropped sessions. Not combinable with `orphaned`. |
| `POST /api/sessions/start` | `start_session` | Complete | `git` boolean param (opt-in git init/snapshot/auto-commit; default false) is forwarded to the REST `git` body field. |
| `GET /api/output?id=&n=` | `session_output` | Complete | |
| `GET /api/sessions/timeline` | `session_timeline` | Complete | |
| `POST /api/sessions/kill` | `kill_session` | Complete | |
| `POST /api/sessions/rename` | `rename_session` | Complete | |
| `POST /api/sessions/restart` | `restart_session` | Complete | v2.2.1 |
| `POST /api/sessions/delete` | `delete_session` | Complete | v2.2.1 |
| `POST /api/sessions/state` | — | Not in MCP | Internal use: manual state override. Not useful for MCP clients |
| `POST /api/command` | `send_input` | Partial | API accepts raw command strings; MCP has typed `send_input` for session input. Use `send_saved_command` for named commands |
| `POST /api/sessions/bind` | `session_bind_agent` | Complete | Bind a session to an agent worker |
| `POST /api/sessions/import` | `session_import` | Complete | Import an orphan session from disk |
| `GET /api/sessions/reconcile` | `session_reconcile` | Complete | List orphan session dirs |
| `POST /api/sessions/{id}/rollback` | `session_rollback` | Complete | Roll back to pre-session checkpoint |
| `GET /api/sessions/stale` | `sessions_stale` | Complete | Running sessions stuck longer than threshold |
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
| `GET /api/alerts` | `get_alerts` | Complete | `limit`/`unread`/`before` paging maps to the tool's `limit`/`unread_only`/`before` |
| `GET /api/alerts/count` | — | Not in MCP | Badge counts `{unread_count, total}` for polling clients; MCP clients use `get_alerts` |
| `POST /api/alerts` (mark read) | `mark_alert_read` | Complete | |
| `POST /api/alerts/create` | `create_alert` | Complete | GH#201 Phase 5 — raises a new system alert (`CapAlertsWrite`, not granted to any built-in group by default) |

### Autonomous PRD Planning

| API Endpoint | MCP Tool | Status | Notes |
|-------------|----------|--------|-------|
| `GET /api/autonomous/status` | `autonomous_status` | Complete | |
| `GET /api/autonomous/config` | `autonomous_config_get` | Complete | includes `per_prd_guardrails` (completion guardrails: `rules-review`, `ci-watch`; empty = off) and `ci_watch_timeout_seconds` (default 1500) — [howto](howto/ci-watch-guardrail.md); `block_on_file_drift` (default `false`, warn-only) — [howto](howto/file-drift-guardrail.md) |
| `PUT /api/autonomous/config` | `autonomous_config_set` | Complete | accepts `per_prd_guardrails` (e.g. `["rules-review","ci-watch"]`) and `ci_watch_timeout_seconds` — [howto](howto/ci-watch-guardrail.md); accepts `block_on_file_drift` (bool; `true` blocks a task on foreign file drift) — [howto](howto/file-drift-guardrail.md); accepts `max_concurrent_tasks` (int, default 1 = sequential; daemon-wide default an Automaton's own setting overrides); `max_parallel_tasks` is deprecated — accepted, ignored, and reported under `deprecated_fields` in the response |
| `GET /api/autonomous/prds` | `autonomous_prd_list` | Complete | |
| `POST /api/autonomous/prds` | `autonomous_prd_create` | Complete | `?view=compact` returns the write view (`id`, `status`, `rev`, `message`) instead of the whole Automaton; absent or `view=full` = full. MCP sends `view=compact` |
| `GET /api/autonomous/prds/{id}` | `autonomous_prd_get` | Complete | `ci_watch {status, sha, repo, runs[], summary, category, issues, checked_at}` when the `ci-watch` completion guardrail ran; status `skipped`/`pass`/`warn`/`pending`/`failed_policy`/`failed_content`/`failed_infra`/`error` — [howto](howto/ci-watch-guardrail.md); per-task `foreign_file_drift[]` and `file_drift_detected` decisions when a direct-mode task committed someone else's pre-existing uncommitted files — [howto](howto/file-drift-guardrail.md). `?detail=summary|tasks|full` picks how much comes back: absent = `full` on REST, and `foreign_file_drift` is only in `full`. The REST default is unchanged; the MCP tool `autonomous_prd_get` sends `detail=summary` by default |
| `PUT /api/autonomous/prds/{id}/guardrails` | `per_automaton_guardrails_set` | Complete | body `{guardrail_profile, per_task_guardrails[], per_story_guardrails[], per_prd_guardrails[]}` — replaces all four; `per_prd_guardrails` takes `rules-review` / `ci-watch`; optional `block_on_file_drift` (bool; omitted = inherit from profile/global); `?view=compact` returns the write view (absent or `view=full` = full; MCP sends `view=compact`) |
| `POST /api/autonomous/guardrail_profiles` | `guardrail_profile_create` | Complete | body `{name, description, guardrails[], block_on_file_drift?}`; `block_on_file_drift` omitted = inherit `autonomous.block_on_file_drift` — [howto](howto/file-drift-guardrail.md) |
| `PUT /api/autonomous/guardrail_profiles/{id}` | `guardrail_profile_update` | Complete | optional `block_on_file_drift` (bool; omitted = unchanged) — [howto](howto/file-drift-guardrail.md) |
| `DELETE /api/autonomous/prds/{id}` | `autonomous_prd_cancel` | Complete | response names killed sessions: `killed_sessions` (count) + `killed_session_ids` (array); `?view=compact` returns the write view with `killed_sessions` (count) and no `killed_session_ids` (absent or `view=full` = full; MCP sends `view=compact`) |
| `POST /api/autonomous/prds/{id}/decompose` | `autonomous_prd_decompose` | Complete | `?view=compact` (absent or `view=full` = full; MCP sends `view=compact`) |
| `POST /api/autonomous/prds/{id}/set_priority` | `autonomous_prd_set_priority` | Complete | capacity-queue priority; higher runs first; `?view=compact` (absent or `view=full` = full; MCP sends `view=compact`) |
| `POST /api/autonomous/prds/{id}/set_llm` | `autonomous_prd_set_llm` | Complete | Set the PRD's worker LLM fields; OMITTED fields stay unchanged, an empty value clears that field (falls back to global) |
| `GET /api/capacity` | `capacity_status` | Complete | admission ledger: pools, leases, wait queue; each `llm:*` pool carries `effective_max_inflight` (cap admitted against) + `inflight_source` (`explicit` | `plan_tier` | `kind`); response also carries `patterns_version`, `patterns_stale` and usage-backoff `claude_cli_version` |
| `PUT /api/compute/nodes/{name}` (`max_concurrent_sessions`) | `compute_node_update` / `compute_node_add` | Complete | per-node cap on autonomous sessions |
| `PUT /api/llms/{name}` (`max_inflight`, `plan_tier`) | `llm_update` / `llm_add` | Complete | per-LLM cap on autonomous sessions; `plan_tier` (`pro`/`max_5x`/`max_20x`/`api`) admits against that tier's `plan_tier_max_inflight` value unless an explicit `max_inflight > 0` overrides it; `GET /api/llms` responses carry both `effective_max_inflight` and `inflight_source` |
| `POST /api/autonomous/prds/{id}/run` | `autonomous_prd_run` | Complete | `?view=compact` (absent or `view=full` = full; MCP sends `view=compact`) |
| `GET /api/autonomous/learnings` | `autonomous_learnings` | Complete | |
| `GET /api/autonomous/subscriptions` | `autonomous_subscriptions` | Complete | list the operator's persisted lifecycle event subscriptions (optional `?owner=` filter) |
| `POST /api/autonomous/subscriptions` | `autonomous_subscribe` | Complete | body `{filter:{kinds[], level?, prd_id?, task_id?, story_id?, owner?}, sink:{type, target}, action:{type, text}?, coalesce?, one_shot?, expires_at?}`; 400 with the validation message on bad input; sinks `comm`/`session`/`topic`/`alert`/`mcp_queue` (the queue name is the sink target; webhook deferred to a later phase). MCP `expires_in_seconds` maps to `expires_at` |
| `DELETE /api/autonomous/subscriptions/{id}` | `autonomous_unsubscribe` | Complete | 404 for unknown ids (also accepted as `DELETE ?id={id}`); one-shot subscriptions are also auto-deleted after first delivery; changes take effect live (engine re-attaches), no restart needed |
| `PUT /api/autonomous/subscriptions/{id}` | `autonomous_subscription_update` | Complete | full replace of the mutable fields (filter, sink, action, coalesce, one_shot, expires_at; owner kept unless supplied); id and created_at are kept; 200 + the record, 400 validation message, 404 unknown id; auth `autonomous:write`; live re-attach, no restart |
| `POST /api/autonomous/subscriptions/{id}/test` | `autonomous_subscription_test` | Complete | delivers a synthetic `[test] ` prd.updated event to just that subscription's sink (never the action text, never consuming a one-shot); 200 `{ok, sink, error}`, 404 unknown id; auth `autonomous:write` |
| `GET /api/autonomous/events` | `autonomous_events_pending` | Complete | `?queue=` (required), `?since=` cursor (default 0), `?limit=` (default 50, capped at 200); returns `{events, next_cursor, dropped}` with `events` always an array; 400 for a missing queue or non-numeric since/limit, 503 when no queue is wired; reads the queue behind an `mcp_queue` subscription sink. CLI `datawatch autonomous events`; comm `autonomous events` |

### Automation records

| API Endpoint | MCP Tool | Status | Notes |
|-------------|----------|--------|-------|
| `GET /api/automations` | `automation_list` | Complete | Data records only — no automatic advance yet |
| `POST /api/automations` | `automation_create` | Complete | body `{name, description, entry_member_id, members[], budget_policy, project_dir, backend, model, effort, verification_*}` |
| `GET /api/automations/{id}` | `automation_get` | Complete | |
| `PUT /api/automations/{id}` | `automation_update` | Complete | optional `expected_version` — optimistic concurrency, `409` if the record moved since |
| `DELETE /api/automations/{id}` | `automation_delete` | Complete | member Automatons are unaffected — the back-reference is cleared |

### Observer peers

| API Endpoint | MCP Tool | Status | Notes |
|-------------|----------|--------|-------|
| `GET /api/observer/peers` | `observer_peers_list` | Complete | Federated peers (any of the three shapes) |
| `POST /api/observer/peers` | `observer_peer_register` | Complete | Mints token (only opportunity) |
| `GET /api/observer/peers/{name}` | `observer_peer_get` | Complete | TokenHash redacted |
| `GET /api/observer/peers/{name}/stats` | `observer_peer_stats` | Complete | Last-known StatsResponse v2 |
| `DELETE /api/observer/peers/{name}` | `observer_peer_delete` | Complete | Auto re-register on next push |
| `GET /api/observer/peers/{agent_id}` | `observer_agent_stats` | Complete | Agent-flavoured alias — agent_id is the peer name |
| `GET /api/observer/peers` (filtered shape=A) | `observer_agent_list` | Complete | Agent-flavoured alias — peers running in agent shape |

### Observer — unified stats + sub-process monitoring

| API Endpoint | MCP Tool | Status | Notes |
|-------------|----------|--------|-------|
| `GET /api/observer/stats` | `observer_stats` | Complete | StatsResponse v2 snapshot |
| `GET /api/observer/envelopes` | `observer_envelopes` | Complete | per-session + per-backend rollup |
| `GET /api/observer/envelope?id=` | `observer_envelope` | Complete | drill-down to one envelope's tree |
| `GET /api/observer/config` | `observer_config_get` | Complete | |
| `PUT /api/observer/config` | `observer_config_set` | Complete | |
| `GET /api/stats?v=2` | `observer_stats` | Alias | Negotiates v2 on the existing stats path |

### PRD-DAG Orchestrator

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

### Cost tracking

| API Endpoint | MCP Tool | Status | Notes |
|-------------|----------|--------|-------|
| `GET /api/cost` | `cost_summary` | Complete | Token + USD rollup |
| `GET /api/cost/usage` | `cost_usage` | Complete | Per-session detail |
| `GET /api/cost/rates` | `cost_rates` | Complete | Per-model price-per-token |

### Cooldown

| API Endpoint | MCP Tool | Status | Notes |
|-------------|----------|--------|-------|
| `GET /api/cooldown` | `cooldown_status` | Complete | Global rate-limit pause state |
| `PUT /api/cooldown` | `cooldown_set` | Complete | Set cooldown (G/P/D scopes) |
| `DELETE /api/cooldown` | `cooldown_clear` | Complete | Clear cooldown |

### Usage back-off

| API Endpoint | MCP Tool | Status | Notes |
|-------------|----------|--------|-------|
| `GET /api/usage-backoff` | `usage_backoff_status` | Complete | Active per-backend flags, effective config, pattern staleness (`patterns_version`, `patterns_stale`) and `claude_cli_version` |
| `POST /api/usage-backoff` | `usage_backoff_set` | Complete | Set a flag (`backend` + `minutes` or `until`, optional `reason`) |
| `DELETE /api/usage-backoff` | `usage_backoff_clear` | Complete | Clear one backend, or all |
| `POST /api/usage-backoff/recheck` | `usage_backoff_recheck` | Complete | Record rate-limit patterns and the current `claude_cli_version` as rechecked (clears `patterns_stale`) |

### Routing rules

| API Endpoint | MCP Tool | Status | Notes |
|-------------|----------|--------|-------|
| `GET /api/routing-rules` | `routing_rules_list` | Complete | Backend auto-selection rules |
| `POST /api/routing-rules/test` | `routing_rules_test` | Complete | Test which backend a task would route to |

### Templates

| API Endpoint | MCP Tool | Status | Notes |
|-------------|----------|--------|-------|
| `GET /api/templates` | `template_list` | Complete | |
| `POST /api/templates` | `template_upsert` | Complete | |
| `DELETE /api/templates` | `template_delete` | Complete | |

### Project aliases

| API Endpoint | MCP Tool | Status | Notes |
|-------------|----------|--------|-------|
| `GET /api/projects` | `project_list` | Complete | |
| `POST /api/projects` | `project_upsert` | Complete | |
| `DELETE /api/projects` | `project_alias_delete` | Complete | |
| `GET /api/project/summary?dir=` | `project_summary` | Complete | Git status + sessions + stats |

### Device aliases

| API Endpoint | MCP Tool | Status | Notes |
|-------------|----------|--------|-------|
| `GET /api/device-aliases` | `device_alias_list` | Complete | For `new: @<alias>:` routing |
| `POST /api/device-aliases` | `device_alias_upsert` | Complete | |
| `DELETE /api/device-aliases` | `device_alias_delete` | Complete | |

### Profiles (Project + Cluster)

| API Endpoint | MCP Tool | Status | Notes |
|-------------|----------|--------|-------|
| `GET /api/profiles` | `profile_list` | Complete | |
| `GET /api/profiles/{name}` | `profile_get` | Complete | |
| `POST /api/profiles` | `profile_create` | Complete | |
| `PUT /api/profiles/{name}` | `profile_update` | Complete | |
| `DELETE /api/profiles/{name}` | `profile_delete` | Complete | |
| `POST /api/profiles/{name}/smoke` | `profile_smoke` | Complete | Smoke-test a profile |

### Agents

| API Endpoint | MCP Tool | Status | Notes |
|-------------|----------|--------|-------|
| `GET /api/agents` | `agent_list` | Complete | |
| `POST /api/agents` | `agent_spawn` | Complete | |
| `GET /api/agents/{id}` | `agent_get` | Complete | |
| `GET /api/agents/{id}/logs` | `agent_logs` | Complete | |
| `DELETE /api/agents/{id}` | `agent_terminate` | Complete | |
| `GET /api/agents/audit` | `agent_audit` | Complete | Agent audit log |

### Singleton operations

| API Endpoint | MCP Tool | Status | Notes |
|-------------|----------|--------|-------|
| `POST /api/ask` | `ask` | Complete | Single-shot LLM ask |
| `POST /api/assist` | `assist` | Complete | Quick-response assistant |
| `GET /api/audit` | `audit_query` | Complete | Operator-action log |
| `GET /api/analytics` | `analytics` | Complete | Session counts, backend distribution |
| `POST /api/reload` | `reload` | Complete | Hot config reload |
| `GET /api/diagnose` | `diagnose` | Complete | Backend reachability + recent errors |
| `GET /api/splash/info` | `splash_info` | Complete | Splash banner info |

### Voice + Vision + Channel bridge

| API Endpoint | MCP Tool | Status | Notes |
|-------------|----------|--------|-------|
| `POST /api/voice/transcribe` | — | Not in MCP | File upload — MCP tools don't accept binary blobs. Use REST directly |
| `POST /api/vision/describe` | `vision_describe` | Partial | REST accepts multipart file upload; MCP tool accepts a local file path. Both require `vision.enabled: true` |
| `POST /api/channel/reply` | — | Not in MCP | Internal MCP-bridge callback (channel.js / datawatch-channel) |

### Plugin Framework

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

### Infrastructure

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
| `GET /api/sessions/finished-oneshots` | List finished one-shot sessions still open — operational (Settings → About) |
| `POST /api/sessions/finished-oneshots` | Close finished one-shot sessions — operational (Settings → About) |
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
| Cost tracking | 3 | 3 | **100%** |
| Cooldown | 3 | 3 | **100%** |
| Routing rules | 2 | 2 | **100%** |
| Templates | 3 | 3 | **100%** |
| Project aliases | 4 | 4 | **100%** |
| Device aliases | 3 | 3 | **100%** |
| Profiles (Project + Cluster) | 6 | 6 | **100%** |
| Agents (ephemeral workers) | 6 | 6 | **100%** |
| Singleton operations | 7 | 7 | **100%** |
| Voice + Channel | 2 | 0 | **0%** (file uploads + internal callbacks) |
| Autonomous PRD planning | 10 | 10 | **100%** |
| Automation records | 5 | 5 | **100%** (data records only — no automatic advance yet) |
| Observer (unified stats + envelopes) | 4 | 4 | **100%** |
| Plugin framework | 6 | 6 | **100%** |
| PRD-DAG orchestrator | 9 | 9 | **100%** |
| Infrastructure | 17 | 0 | **0%** (by design) |
| **Total** | **137** | **107** | **78%** (100% of user-facing features that fit MCP's tool model) |

All user-facing features have MCP coverage. The 30 endpoints without MCP tools
are infrastructure, operational, file-upload, or security-sensitive operations
that should remain human-controlled or don't fit MCP's text-in / text-out
contract.
