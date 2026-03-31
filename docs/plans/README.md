# Plans, Bugs & Backlog
- the back arrow in a session needs to be bigger, more mobile friendly. since you can click to edit the name of a session
- the datawatch icon should be in the background centered (of entire page) and fill 85% to the width of the page of the sessions tab
- when things happen (new sesion, then prompt detected or even more commands at once) there are a lot of alerts at once.  there should be a little throttling and bundling. a bunch of messages send in a burst should be bundled and collated by session (incase 2 sessoins have things happen at once). if there are any settings for these they should be in the configuration file in the appropriate section and card.

Single source of truth for all datawatch project tracking.

---

## Open Bugs

| # | Description | Priority | Notes |
|---|-------------|----------|-------|
| B1 | openwebui backend uses curl/script — investigate interactive API session | low | Better UX but functional as-is |
| B2 | Terminal scroll not constrained within session detail | low | Needs browser validation |
| B3 | Claude/opencode state badges — validate updating during active work | low | Needs browser validation |

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
| RTK Integration (Rethink Toolkit frontend) | 2.5 weeks | planned | [rtk-integration](2026-03-30-rtk-integration.md) |
| libsignal (replace signal-cli with native Go) | 3-6 months | planned | [libsignal](2026-03-29-libsignal.md) |

## Backlog (no plan, low priority)

| Item | Category |
|------|----------|
| IPv6 listener support (`[::]` bind) | infrastructure |
| Live cell DOM diffing for dashboard (perf optimization) | frontend |
| Evaluate alternative covert channels beyond DNS | research |
| Container images and Helm chart | deployment |

## Completed Bugs (archived)

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
