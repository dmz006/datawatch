---
date: 2026-03-29
version: 0.7.3
scope: System statistics dashboard — CPU, GPU, disk, session metrics, real-time web UI
status: planned
---

# Plan: System Statistics Dashboard

## Problem

Users managing datawatch need visibility into system resource usage (CPU, memory, GPU, disk) and session metrics (count, duration, output size) without SSH. This data should be available via the web UI, communication channels, and MCP.

## Scope

- `internal/stats/` — new package for system metric collection
- `internal/server/api.go` — /api/stats endpoint
- `internal/server/ws.go` — real-time stats broadcast
- `internal/server/web/app.js` — Settings tab split: Settings + Statistics
- `internal/mcp/server.go` — new `get_stats` MCP tool
- `internal/router/router.go` — new `stats` command

## Phases

### Phase 1 — Stats Collector (Planned)

- New package: `internal/stats/collector.go`
- Collects every 5 seconds (configurable):
  - CPU: load average (1/5/15 min), per-core usage
  - Memory: total, used, available, swap
  - Disk: usage for data_dir partition
  - GPU: nvidia-smi or rocm-smi if available (model, utilization, memory, temperature)
  - Process: datawatch daemon RSS, goroutine count, open FDs
- Session metrics:
  - Active/total session count
  - Per-session: duration, output.log size, LLM backend, state
  - Total output bytes written

### Phase 2 — API Endpoint (Planned)

- `GET /api/stats` — returns current snapshot as JSON
- `GET /api/stats/history?minutes=60` — time-series data (stored in ring buffer)
- Ring buffer: 720 entries (1 hour at 5s intervals) in memory
- No persistence — stats reset on daemon restart

### Phase 3 — Web UI Dashboard (Planned)

- Settings tab splits into sub-tabs: **Settings** | **Statistics**
- Statistics tab sections (all collapsible):
  - **System**: CPU gauge, memory bar, disk usage
  - **GPU**: utilization, temperature, memory (hidden if no GPU detected)
  - **Daemon**: uptime, version, goroutines, memory usage
  - **Sessions**: active count, total count, table with per-session metrics
  - **Output**: total bytes written, bytes per session
- Real-time updates via WebSocket (new `stats` message type, every 5s)
- Sparkline charts for CPU/memory history (lightweight, no chart library)

### Phase 4 — Communication Channel Support (Planned)

- `stats` command via Signal/Telegram/webhook/DNS: returns text summary
- Format: `[host] CPU: 23% | RAM: 4.2/16 GB | Disk: 45% | GPU: RTX 3080 42°C 15% | Sessions: 3 active`
- MCP tool: `get_stats` returns structured JSON

### Phase 5 — Per-Session Resource Tracking (Planned)

- Track CPU time consumed by each tmux session's process tree
- Track cumulative network bytes (if measurable via /proc)
- Display in session detail view alongside timeline
- Include in session timeline events

## Key Files

- `internal/stats/collector.go` — metric collection goroutine
- `internal/stats/types.go` — SystemStats, SessionStats structs
- `internal/server/api.go` — /api/stats handler
- `internal/server/ws.go` — stats broadcast message type
- `internal/server/web/app.js` — Statistics tab rendering
- `internal/mcp/server.go` — get_stats tool
- `internal/router/router.go` — stats command handler

## Dependencies

- `/proc/stat`, `/proc/meminfo`, `/proc/diskstats` — Linux proc filesystem
- `nvidia-smi` / `rocm-smi` — optional GPU monitoring (exec, not library)
- No new Go module dependencies

## Estimated Effort

2-3 weeks. Phase 1-3 can be delivered as MVP; Phase 4-5 are enhancements.
