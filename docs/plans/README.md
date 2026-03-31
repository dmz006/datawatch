# Plans, Bugs & Backlog

Single source of truth for all datawatch project tracking.

---

## Open Bugs

| # | Description | Priority | Notes |
|---|-------------|----------|-------|
| B1 | openwebui backend uses curl/script — investigate Go-side conversation manager for interactive mode | medium | Plan exists: Go conversation manager like ACP pattern |

## Completed Plans

| Plan | Version | Date | File |
|------|---------|------|------|
| Backlog Bugs & Channel | v0.6.x | 2026-03-28 | [bugs-and-channel](2026-03-28-bugs-and-channel.md) |
| Patch 0.6.1 | v0.6.1 | 2026-03-28 | [patch-0.6.1](2026-03-28-patch-0.6.1.md) |
| Patch 0.6.3 | v0.6.3 | 2026-03-28 | [patch-0.6.3](2026-03-28-patch-0.6.3.md) |
| Scheduled Prompts | v0.9.0 | 2026-03-29 | [scheduled-prompts](2026-03-29-scheduled-prompts.md) |
| Config Restructure | v0.10.0 | 2026-03-29 | [config-restructure](2026-03-29-config-restructure.md) |
| Flexible Filters | v0.11.0 | 2026-03-29 | [flexible-filters](2026-03-29-flexible-filters.md) |
| System Statistics | v0.12.0 | 2026-03-29 | [system-statistics](2026-03-29-system-statistics.md) |
| ANSI Console (xterm.js) | v0.13.0 | 2026-03-29 | [ansi-console](2026-03-29-ansi-console.md) |
| eBPF Per-Session Stats | v0.16.0 | 2026-03-30 | [ebpf-stats](2026-03-30-ebpf-stats.md) |
| Dashboard Redesign | v0.18.0 | 2026-03-30 | [dashboard-redesign](2026-03-30-dashboard-redesign.md) |
| Encryption at Rest | v0.18.0 | 2026-03-30 | — (secfile/migrate.go, tracker encryption, export cmd) |
| DNS Channel | v0.7.0+ | 2026-03-30 | — (internal/messaging/backends/dns/) |

## Future Plans

| Plan | Effort | Status | File |
|------|--------|--------|------|
| OpenWebUI interactive (Go conversation manager) | 2-3 hours | planned | — |
| RTK Integration (Rethink Toolkit frontend) | 2.5 weeks | planned | [rtk-integration](2026-03-30-rtk-integration.md) |
| libsignal (replace signal-cli with native Go) | 3-6 months | planned | [libsignal](2026-03-29-libsignal.md) |
| Encryption: session.json + daemon.log | 1-2 days | planned | — (plan in Claude plan file) |

## Backlog (no plan, low priority)

| Item | Category |
|------|----------|
| IPv6 listener support (`[::]` bind) | infrastructure |
| Live cell DOM diffing for dashboard (perf optimization) | frontend |
| Evaluate alternative covert channels beyond DNS | research |
| Container images and Helm chart | deployment |
| Animated GIF tour of web interface for README | documentation |
| Session chaining — pipelines where output triggers next session, conditional branching on exit status | sessions |
| Session templates — reusable workflows (dir, backend, env, auto-git bundled) | sessions |
| Cost tracking — aggregate token usage and estimated cost per session/backend | sessions |
| Multi-user access control — role-based permissions (viewer/operator/admin), per-user channel bindings | collaboration |
| Session sharing — time-limited read-only or interactive links for teammates | collaboration |
| Audit log — append-only record of who started/killed/sent input, exportable | collaboration |
| Session diffing — auto git diff summary in completion alerts (+47/-12, 3 files changed) | observability |
| Anomaly detection — flag stuck loops, unusual CPU/memory, long input-wait | observability |
| Historical analytics — trend charts in PWA (sessions/day, duration by backend, failure rates) | observability |
| Threaded conversations — keep session alerts in threads on Slack/Discord/Matrix | messaging |
| Voice input — accept voice messages on Signal/Telegram, transcribe via Whisper | messaging |
| Rich previews — syntax-highlighted code snippets or terminal screenshots in alerts | messaging |
| Health check endpoint — `/healthz` and `/readyz` for K8s probes | deployment |
| Hot config reload — SIGHUP or API to reload config.yaml without restart | operations |
| Prometheus metrics export — `/metrics` endpoint for Grafana | operations |
| Copilot/Cline/Windsurf backends | backends |
| Backend auto-selection — route to best backend based on task type, load, or rules | backends |
| Fallback chains — retry on alternate backend when primary hits rate limit or errors | backends |

## Completed Bugs (archived)

- opencode TUI prompt detection after results (B4) — v1.0.0: matchPromptInLines(10), waiting→running flip
- opencode exit to shell detection (B5) — v1.0.0: `$` suffix detection on last capture-pane line
- Saved command expansion from messaging channels (!cmd /cmd) — v1.0.0: expandSavedCommand in router, Enter key fix
- Alert quick reply only on final waiting_input state — v0.19.1: HasSuffix check on last event
- Input logging for all paths (terminal typing, quick buttons, saved commands) — v0.19.1: rawInputBuf accumulator
- Alert accepted prompt logging (Enter key shows what was accepted) — v0.19.1: LastPrompt fallback
- Alert burst flooding (B6) — v0.19.1: goroutine bundler per session, 5s quiet + 30s keepalive, alert listener removed from router broadcast
- Back button (B7) — v0.19.1: CSS chevron, 44x44px touch target
- Watermark icon (B8) — v0.19.1: fixed-position centered SVG, 85% width, 4.5% opacity
- Toast positioning (B9) — v0.19.1: container inside .app div, position:absolute
- LLM config dropdowns (B10) — v0.19.1: select_inline renders as \<select\> dropdown
- Terminal scroll constrained within session detail (B2) — v0.19.0: xterm scrollbar hidden, overflow-x auto
- Claude/opencode state badges updating during active work (B3) — v0.19.0: universal capture-pane detection, prompt patterns expanded
- Terminal rendering garbled display — v0.19.0+: single display source (pane_capture only), cursor-home overwrite (no flash)
- Completion false positive from command echo — v0.19.0: HasPrefix instead of Contains
- Interface binding: localhost forced on, connected detection, mutual exclusion — v0.18.0
- Interface checkbox mutual exclusion — v0.17.3
- Detection filter add/remove managed list — v0.14.5
- Real-time stats streaming via WS — v0.17.2
- Settings sub-tabs (Monitor/General/Comms/LLM/About) — v0.16.0
- Multi-machine README shared vs unique channels — v0.16.0
- Per-session network statistics (eBPF) — v0.16.0
- Browser debug tools (triple-tap panel) — v0.16.0
- View persistence across refresh — v0.17.2
- Comms server status indicator — v0.17.3
- Session donut chart (active of max) — v0.17.2
- TLS redirect double-port — v0.14.6
- Shell backend script_path detection — v0.15.0
- Claude exit auto-complete — v0.15.0
- Restart command (os.Args override) — v0.15.0
