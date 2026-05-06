# B6: Function Parity Across All Channels

**Date:** 2026-04-12
**Priority:** high
**Category:** architecture

---

## Audit Results

Comprehensive comparison of every function across API, MCP, comm channels, CLI, and web UI.

### Legend
- ✅ = implemented
- ❌ = missing (needs implementation)
- ➖ = not applicable for this channel
- 🔧 = partial (exists but incomplete)

---

### Sessions

| Function | API | MCP | Comm | CLI | Web UI |
|----------|-----|-----|------|-----|--------|
| List sessions | ✅ `/api/sessions` | ✅ `list_sessions` | ✅ `list` | ✅ `session list` | ✅ |
| Start session | ✅ `/api/sessions/start` | ✅ `start_session` | ✅ `new:` | ✅ `session new` | ✅ |
| Session output | ✅ `/api/output` | ✅ `session_output` | ✅ `status/tail` | ✅ `session tail` | ✅ |
| Session timeline | ✅ `/api/sessions/timeline` | ✅ `session_timeline` | ➖ | ✅ `session timeline` | ✅ |
| Send input | ✅ WS `send_input` | ✅ `send_input` | ✅ `send` | ✅ `session send` | ✅ |
| Kill session | ✅ `/api/sessions/kill` | ✅ `kill_session` | ✅ `kill` | ✅ `session kill` | ✅ |
| Rename session | ✅ `/api/sessions/rename` | ✅ `rename_session` | ➖ | ✅ `session rename` | ✅ |
| Delete session | ✅ `/api/sessions/delete` | ✅ `delete_session` | ➖ | ➖ | ✅ |
| Restart session | ✅ `/api/sessions/restart` | ✅ `restart_session` | ➖ | ➖ | ✅ |
| Stop all | ➖ | ✅ `stop_all_sessions` | ➖ | ✅ `session stop-all` | ➖ |
| Get response | ✅ `/api/sessions/response` | ✅ `copy_response` | ✅ `copy` | ➖ | ✅ |
| Get prompt | ✅ `/api/sessions/prompt` | ✅ `get_prompt` | ✅ `prompt` | ➖ | ➖ |
| Attach tmux | ➖ | ➖ | ✅ `attach` | ✅ `session attach` | ➖ |
| Session state | ✅ `/api/sessions/state` | ➖ | ➖ | ➖ | ✅ |

### Memory

| Function | API | MCP | Comm | CLI | Web UI |
|----------|-----|-----|------|-----|--------|
| Save/remember | ✅ `/api/memory/save` | ✅ `memory_remember` | ✅ `remember:` | ❌ | ✅ (chat) |
| Search/recall | ✅ `/api/memory/search` | ✅ `memory_recall` | ✅ `recall:` | ❌ | ✅ (browser) |
| List memories | ✅ `/api/memory/list` | ✅ `memory_list` | ✅ `memories` | ❌ | ✅ (browser) |
| Delete/forget | ✅ `/api/memory/delete` | ✅ `memory_forget` | ✅ `forget` | ❌ | ✅ (browser) |
| Stats | ✅ `/api/memory/stats` | ✅ `memory_stats` | ❌ | ❌ | ✅ (monitor) |
| Export | ✅ `/api/memory/export` | ✅ `memory_export` | ❌ | ❌ | ❌ |
| Import | ✅ `/api/memory/import` | ❌ | ❌ | ❌ | ❌ |
| Reindex | ❌ | ✅ `memory_reindex` | ✅ `memories reindex` | ❌ | ❌ |
| Tunnels | ❌ | ❌ | ✅ `memories tunnels` | ❌ | ❌ |
| Learnings | ❌ | ❌ | ✅ `learnings` | ❌ | ❌ |
| Research | ❌ | ✅ `research_sessions` | ✅ `research:` | ❌ | ❌ |
| WAL | ✅ `/api/memory/wal` | ❌ | ❌ | ❌ | ❌ |
| Test embedder | ✅ `/api/memory/test` | ❌ | ❌ | ❌ | ✅ |

### Knowledge Graph

| Function | API | MCP | Comm | CLI | Web UI |
|----------|-----|-----|------|-----|--------|
| Query entity | ✅ `/api/memory/kg/query` | ✅ `kg_query` | ✅ `kg query` | ❌ | ❌ |
| Add triple | ✅ `/api/memory/kg/add` | ✅ `kg_add` | ✅ `kg add` | ❌ | ❌ |
| Invalidate | ✅ `/api/memory/kg/invalidate` | ✅ `kg_invalidate` | ❌ | ❌ | ❌ |
| Timeline | ✅ `/api/memory/kg/timeline` | ✅ `kg_timeline` | ✅ `kg timeline` | ❌ | ❌ |
| Stats | ✅ `/api/memory/kg/stats` | ✅ `kg_stats` | ✅ `kg stats` | ❌ | ❌ |

### Pipelines

| Function | API | MCP | Comm | CLI | Web UI |
|----------|-----|-----|------|-----|--------|
| Start | ✅ `/api/pipelines` POST | ✅ `pipeline_start` | ✅ `pipeline:` | ❌ | ❌ (config only) |
| List | ✅ `/api/pipelines` GET | ✅ `pipeline_list` | ✅ `pipeline status` | ❌ | ❌ |
| Status | ✅ `/api/pipeline?id=` | ✅ `pipeline_status` | ✅ `pipeline status <id>` | ❌ | ❌ |
| Cancel | ✅ `/api/pipeline?action=cancel` | ✅ `pipeline_cancel` | ✅ `pipeline cancel` | ❌ | ❌ |

### System

| Function | API | MCP | Comm | CLI | Web UI |
|----------|-----|-----|------|-----|--------|
| Config get | ✅ `/api/config` | ✅ `get_config` | ❌ | ✅ `config show` | ✅ |
| Config set | ✅ `/api/config` PUT | ❌ | ✅ `configure` | ❌ | ✅ |
| Stats | ✅ `/api/stats` | ✅ `get_stats` | ✅ `stats` | ✅ `stats` | ✅ |
| Alerts | ✅ `/api/alerts` | ✅ `get_alerts` | ✅ `alerts` | ✅ `alerts` | ✅ |
| Restart | ✅ `/api/restart` | ✅ `restart_daemon` | ✅ `restart` | ✅ `restart` | ✅ |
| Version | ✅ `/api/info` | ✅ `get_version` | ✅ `version` | ✅ `--version` | ✅ |
| Schedules | ✅ `/api/schedules` | ✅ `schedule_*` | ✅ `schedule` | ✅ `schedule *` | ✅ |
| Saved commands | ✅ `/api/commands` | ✅ `list/send_saved_commands` | ✅ `library` | ❌ | ✅ |
| Diagnostics | ❌ | ❌ | ❌ | ❌ | ❌ |
| Update check | ✅ `/api/update` | ❌ | ✅ `update check` | ❌ | ✅ |

---

## Priority Gaps (to implement)

### High — Core parity gaps

1. **API: `/api/memory/reindex`** — reindex all memories after model change
2. **API: `/api/memory/learnings`** — list/search learnings
3. **API: `/api/memory/research`** — deep cross-session search
4. **MCP: `memory_import`** — import memories from JSON
5. **MCP: `config_set`** — change config via MCP (currently read-only)

### Medium — Useful additions

6. **Comm: `memory stats`** — show memory stats from comm channel
7. **Comm: `kg invalidate`** — invalidate KG triples from comm
8. **Comm: `memory export`** — export memories from comm
9. **CLI: memory subcommands** — `datawatch memory remember/recall/list/forget`
10. **CLI: pipeline subcommands** — `datawatch pipeline start/status/cancel`

### Low — Nice to have

11. **API: `/api/memory/tunnels`** — cross-project room connections
12. **MCP: `update_check`** — check for daemon updates
13. **Web UI: KG browser** — visual knowledge graph explorer
14. **Web UI: pipeline start/status view** — dedicated pipeline management UI

---

## Implementation Plan

### Phase 1: API gaps (1-2 hours)
Add `/api/memory/reindex`, `/api/memory/learnings`, `/api/memory/research`

### Phase 2: MCP gaps (1 hour)
Add `memory_import`, `config_set`

### Phase 3: Comm gaps (30 min)
Add `memory stats`, `kg invalidate`, `memory export`

### Phase 4: CLI gaps (1-2 hours)
Add `datawatch memory` and `datawatch pipeline` subcommand groups

### Phase 5: Documentation (1 hour)
Update all docs with complete parity tables, MCP TLS/cert setup
