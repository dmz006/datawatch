# Plans, Bugs & Backlog

Single source of truth for all datawatch project tracking.

---

## Open Bugs

| # | Description | Priority | Notes |
|---|-------------|----------|-------|
| B1 | openwebui backend uses curl/script — investigate Go-side conversation manager for interactive mode | medium | Plan exists: Go conversation manager like ACP pattern |
| B4 | opencode TUI does not identify prompts or show status changes after results return | high | "Ask anything" detected initially but lost after TUI redraws |
| B5 | Exiting opencode TUI drops to shell prompt — session stays active instead of completing | high | Need to detect shell prompt after opencode exits and mark complete |
| B6 | Alert burst flooding — many alerts fire at once (new session + prompt + state changes). Need throttling/bundling per session with configurable settings | medium | Settings should be in config file in appropriate section and card |
| B7 | Back arrow in session detail too small for mobile — hard to tap, conflicts with name edit click target | medium | UI/UX |
| B8 | Datawatch icon should be background watermark on sessions tab (centered, 85% page width) | low | UI/UX polish |
| B9 | Toast alerts breaking out of PWA border on right side of browser — should be constrained within .app max-width | high | Currently right-justified against browser edge, not app edge |
| B10 | LLM config: terminal mode and input mode should be dropdowns with available options, not text fields | low | Currently uses select_inline type which may not render as dropdown |

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

## Completed Bugs (archived)

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
