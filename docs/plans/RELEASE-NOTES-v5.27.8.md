# datawatch v5.27.8 — release notes

**Date:** 2026-04-30
**Patch.** BL208 #30 (PRD card alignment) + BL210 (daemon-MCP coverage gap closures).

## What's new

### 1. PRD card style harmonised with Sessions cards (BL208 #30, datawatch#30)

The PRD/Autonomous tab cards used a different visual treatment from Sessions cards (1px grey border + 6px radius + bg/var(--bg) + 8px padding). v5.27.8 brings them in line:

- New `.prd-card` CSS class with the same bg2 / system-radius / 12px-14px padding the Sessions card uses.
- Status drives the 4px left-border colour via `.prd-card-status-<status>` modifiers — `draft` (grey) / `decomposing` (purple) / `needs_review` + `revisions_asked` (warning amber) / `approved` (accent) / `running` (success green) / `completed`+`cancelled` (grey) / `blocked`+`rejected` (error red).
- `renderPRDRow` switched off inline border/padding styles to the class.
- `.prd-row` alias kept on the same element so the v5.26.6 `scrollToPRD` selector keeps working.

Operator-asked: drop the redundant **"PRDs"** sub-header that sat below the Autonomous tab label. The tab label already conveys context and the heading wasted vertical space. Done.

Mobile companion is mirroring the alignment in datawatch-app B66.

### 2. Daemon MCP coverage gap closures (BL210)

Operator-flagged 2026-04-29 audit identified ~12 daemon REST endpoints with no MCP equivalent. v5.27.8 lands the priority subset (11 new tools):

| Tool | REST endpoint | Why |
|---|---|---|
| `memory_wal` | `GET /api/memory/wal` | **Operator priority** — audit memory write history from an IDE |
| `memory_test_embedder` | `POST /api/memory/test` | **Operator priority** — probe Ollama embedder before enabling memory |
| `memory_wakeup` | `GET /api/memory/wakeup` | **Operator priority** — inspect what an agent would see at session start |
| `claude_models` | `GET /api/llm/claude/models` | v5.27.5 listing endpoint, MCP follow-up |
| `claude_efforts` | `GET /api/llm/claude/efforts` | same |
| `claude_permission_modes` | `GET /api/llm/claude/permission_modes` | same |
| `rtk_version` | `GET /api/rtk/version` | RTK quartet had no MCP coverage |
| `rtk_check` | `POST /api/rtk/check` | same |
| `rtk_update` | `POST /api/rtk/update` | same |
| `rtk_discover` | `GET /api/rtk/discover` | same |
| `daemon_logs` | `GET /api/logs?n=…` | tail daemon.log from an IDE while debugging |

All forward to existing `/api/*` paths via the existing `proxyJSON` helper — no new daemon-side state. Bodies in `internal/mcp/v5278_gap_closures.go`; registration inlined into `mcp.New()` alongside the other memory tool block.

Remaining BL210 gaps deferred to v5.28.x — none are operator-priority:
- Filters CRUD (`filter_list` / `filter_upsert` / `filter_delete`)
- Backends listing (`backends_list` / `backends_active`)
- Federation aggregated sessions (`federation_sessions`)
- Mobile push device register (`device_register`)
- Files browser (`files_list`)
- Sessions sub-endpoints (`session_aggregated` / `session_set_state` / `session_set_prompt`)

## Tests

```
Go build:  Success (via `make build` + `make cross`)
Go test:   1519 passed in 58 packages (no new test count; the new
           MCP tools are forwarders covered by underlying REST tests)
Smoke:     run after install
```

## datawatch-app sync

datawatch-app B66 (PRD card style mirror) tracks the mobile-side parity work; no parent-side mobile issue needed.

## Backwards compatibility

- All changes additive. PWA cards keep working for any client that doesn't pick up the new CSS (and the new CSS only adds — `prd-row` alias preserves selectors).
- 11 new MCP tools — IDE clients that don't list them keep working.

## Upgrade path

```bash
git pull
datawatch update && datawatch restart
# Hard-reload the PWA (cache name → datawatch-v5-27-8).
```

No data migration. No new schema.
