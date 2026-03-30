---
date: 2026-03-30
status: in_progress
---

# Plan: System Statistics Dashboard Redesign

## Current State

The Monitor tab in Settings shows a grid of stat cards with progress bars.
All metrics are flat — no visual hierarchy, no graphs, no expandable details.

## Design Goals

1. Visual hierarchy — important metrics prominent, details on demand
2. Appropriate chart types per metric kind
3. Responsive — works on mobile and desktop
4. Real-time — updates via WebSocket every 5s
5. eBPF status notice when enabled but degraded

## Layout

```
┌─────────────────────────────────────────────────────┐
│ [eBPF Notice Banner — only if enabled but degraded] │
├──────────────────┬──────────────────────────────────┤
│   CPU            │   Memory                         │
│   ████████░░ 72% │   ██████████░░ 85%               │
│   1.44 / 2 cores │   14.2 / 16.8 GB                │
├──────────────────┼──────────────────────────────────┤
│   Disk           │   GPU (if available)             │
│   ████░░░░░░ 42% │   ██░░░░░░░░░ 12% 47°C          │
│   1.2 / 2.9 TB   │   VRAM: ████░░ 512/2048 MB      │
├──────────────────┴──────────────────────────────────┤
│   Network        ↓ 160.5 GB  ↑ 154.7 GB            │
│   ▼▼▼▲▲▲ (mini sparkline if history available)      │
├─────────────────────────────────────────────────────┤
│   Sessions  ● 2 active  ○ 82 total                 │
│   [Pie: active/complete/killed/failed segments]     │
├─────────────────────────────────────────────────────┤
│   Active Session Resources                          │
│   ┌─ datawatch (daemon) running 25MB ↓160GB ↑154GB │
│   ├─ ▶ test (opencode-acp) waiting  354MB  3h12m   │
│   │    └─ Net: ↓12KB ↑8KB  PID: 245794             │
│   ├─ ▶ project (claude) running  45MB  1h30m       │
│   │    └─ Net: ↓1.2MB ↑890KB  PID: 123456          │
│   └─ [Kill Orphaned Tmux (2)]                       │
├─────────────────────────────────────────────────────┤
│   Daemon: 25MB RSS  19 goroutines  Uptime: 2h30m   │
│   Interfaces: 0.0.0.0  Tmux: 4 sessions            │
│   ● Live — updates every 5s                         │
└─────────────────────────────────────────────────────┘
```

## Chart Types

| Metric | Chart Type | Rationale |
|--------|-----------|-----------|
| CPU, Memory, Disk, Swap | Horizontal progress bar | Percentage of total |
| GPU Utilization | Horizontal progress bar | Percentage |
| GPU VRAM | Horizontal progress bar | Used/total |
| Sessions | Donut/pie mini chart | Categorical counts |
| Network | Value display with arrows | Cumulative totals |
| Per-session | Expandable rows with details | Variable detail |

## Phases

### Phase 1: eBPF notice + expandable sessions (this commit)
- Banner at top of Monitor tab if eBPF enabled but degraded
- Per-session rows with expand chevron showing PID, network, uptime details
- Session state color coding

### Phase 2: Session pie chart (CSS-only donut)
- Pure CSS donut chart for session state distribution
- Segments: active (green), complete (gray), killed (red), failed (orange)

### Phase 3: Communication channel stats (future)
- MCP connection status and message counts
- Per-messaging-backend: messages sent/received, errors
- Signal/Telegram/Discord/etc. health indicators
