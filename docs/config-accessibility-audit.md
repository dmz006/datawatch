# BL220 Configuration Accessibility Audit

**Rule:** Every feature must be reachable from YAML + REST + MCP + CLI + Comm + PWA (+ mobile app, tracked separately in datawatch-app).

**Scope:** v5.28.x → v6.0 gap closure audit. Captures what surfaces each feature area is currently reachable from and what gaps remain.

**Surfaces:**

| Abbreviation | Surface |
|---|---|
| YAML | `~/.config/datawatch/config.yaml` — static source of truth |
| REST | `/api/*` HTTP endpoints (direct or via `applyConfigPatch`) |
| MCP | MCP tools exposed to Claude Code / AI agents |
| CLI | `datawatch` binary subcommands |
| Comm | Chat-channel commands (Signal / Telegram / Discord / Slack / Matrix) |
| PWA | Web UI settings panel and management views |

---

## Feature Matrix

Legend: ✅ Implemented · 🟡 Partial / indirect only · 🔴 Missing

| Feature Area | YAML | REST | MCP | CLI | Comm | PWA | BL Gap Notes |
|---|:---:|:---:|:---:|:---:|:---:|:---:|---|
| **Session management** | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | Full parity |
| **Config get/set** | ✅ | ✅ | ✅ | ✅ | 🟡 | 🟡 | Comm: `configure` is high-level only; no field-level patch. PWA: ~66 of 230+ fields exposed in Settings |
| **Memory (remember/recall)** | ✅ | 🟡 | ✅ | ✅ | ✅ | 🟡 | REST: no dedicated `/api/memory` surface (config patch only). PWA: enable toggle + embedder model; no recall/search UI |
| **Knowledge Graph (KG)** | ✅ | 🟡 | ✅ | ✅ | ✅ | 🔴 | REST: via memory patch only. PWA: no KG query/browse UI |
| **Autonomous PRDs** | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | `type`, `skill`, `file_path`, `guided_mode` missing from REST/MCP/CLI/Comm (tracked in BL221 §12b Phase 4b) |
| **Agents (F10)** | ✅ | ✅ | ✅ | ✅ | ✅ | 🟡 | PWA: spawn/list/logs UI present; no agent definition config panel |
| **Observer / Federation** | ✅ | ✅ | ✅ | ✅ | 🟡 | 🔴 | Comm: `peers` covers peer CRUD only; no `observer` config/stats/envelopes. PWA: no observer settings or envelope browser |
| **Orchestrator** | ✅ | ✅ | ✅ | ✅ | 🔴 | 🔴 | No `orchestrator` comm command. No PWA orchestrator graph UI |
| **Plugins** | ✅ | ✅ | ✅ | ✅ | 🔴 | 🔴 | No `plugins` comm command. No PWA plugin management panel |
| **Detection (eBPF/BPF)** | ✅ | 🟡 | 🟡 | 🟡 | 🔴 | 🔴 | REST: only via `/api/diagnose` + config patch. MCP: `diagnose` (partial). CLI: `setup ebpf` + `diagnose` only. No dedicated detection surface on any channel |
| **DNS Channel** | ✅ | 🟡 | 🔴 | 🟡 | 🔴 | 🔴 | REST/MCP/Comm: only via generic `config set`/`configure`. CLI: `setup dns` for initial setup. No dedicated dns_channel tools anywhere |
| **Proxy** | ✅ | 🟡 | 🔴 | 🔴 | 🔴 | 🔴 | REST: `/api/proxy/comm/` (functional endpoint) + config patch. No dedicated proxy CLI subcommand, MCP tool, comm command, or PWA panel |
| **Communication channel config** | ✅ | 🟡 | 🔴 | 🟡 | 🟡 | 🟡 | REST: no `/api/channels/*`; config patch only. MCP: `channel_info` (read-only); no set. CLI: `setup channel/*` (initial setup); `channel` (info). Comm: `channel_info` (read-only). PWA: Discord/Slack/Telegram/Signal webhook fields only (4 of 9 channels) |
| **Profiles (project + cluster)** | ✅ | ✅ | ✅ | ✅ | ✅ | 🟡 | PWA: profiles reachable from session create modal; no standalone profile management panel |
| **Templates** | ✅ | ✅ | ✅ | ✅ | 🟡 | 🔴 | Comm: `rest` passthrough only (no `templates` command). PWA: no template management UI |
| **Routing rules** | ✅ | ✅ | ✅ | ✅ | 🟡 | 🔴 | Comm: `rest` passthrough only. PWA: no routing rules UI |
| **Device aliases** | ✅ | ✅ | ✅ | ✅ | 🔴 | 🔴 | No comm command. No PWA device alias management |
| **Cost / billing rates** | ✅ | ✅ | ✅ | ✅ | ✅ | 🟡 | PWA: cost stats shown in session panel; cost_rates config not editable from PWA |
| **Cooldown / rate limiting** | ✅ | ✅ | ✅ | ✅ | ✅ | 🟡 | PWA: cooldown status displayed; no set/clear from PWA |
| **Pipeline** | ✅ | ✅ | ✅ | ✅ | ✅ | 🟡 | PWA: no dedicated pipeline management view |
| **Audit log** | ✅ | ✅ | ✅ | ✅ | ✅ | 🟡 | PWA: no audit log browser |
| **Scheduling (cron/alerts)** | ✅ | ✅ | ✅ | ✅ | ✅ | 🟡 | PWA: schedule creation from session controls; no full schedule manager |
| **Splash / branding** | ✅ | ✅ | ✅ | ✅ | 🔴 | 🔴 | No comm command. No PWA branding config panel |
| **Server / TLS config** | ✅ | 🟡 | 🟡 | 🟡 | 🔴 | 🔴 | REST/MCP/CLI: only via `config set`. No dedicated server-config surface |
| **Stale sessions** | ✅ | ✅ | ✅ | ✅ | ✅ | 🟡 | PWA: stale indicator only; no bulk-clean action |
| **Update management** | ✅ | 🟡 | 🟡 | ✅ | ✅ | 🟡 | PWA: update notification shown; no configure-update-channel UI |
| **Analytics** | ✅ | ✅ | ✅ | 🟡 | 🔴 | 🟡 | CLI: no dedicated `analytics` subcommand (REST only). No comm command. PWA: stats panel shown; no analytics config |

---

## Gap Summary by Surface

### Comm channel — missing commands
Features accessible via REST + MCP + CLI but not Comm:

| Missing Command | Feature | Workaround |
|---|---|---|
| `orchestrator` | Graph orchestration lifecycle | `rest POST /api/orchestrator/...` |
| `plugins` | Enable / disable / test plugins | `rest GET /api/plugins` |
| `templates` | List / create / edit templates | `rest GET /api/templates` |
| `routing` (or `routing-rules`) | Inspect and test session routing rules | `rest GET /api/routing-rules` |
| `device-alias` | List / manage device aliases | `rest GET /api/device-aliases` |
| `splash` | Read splash/branding info | `rest GET /api/splash/info` |
| `detection` | eBPF detection status | `rest GET /api/diagnose` |
| `observer` (beyond `peers`) | Observer config, stats, envelopes | `rest GET /api/observer/...` |
| `analytics` | Session analytics query | `rest GET /api/analytics` |

Note: `rest <METHOD> <PATH>` passthrough exists as escape hatch for all of the above.

### PWA settings — missing panels / fields
Features accessible via REST + MCP but not from the web UI:

| Missing UI | Feature | REST surface |
|---|---|---|
| Observer panel | Observer config, envelope browser, peer stats | `GET/POST /api/observer/*` |
| Orchestrator panel | Graph list, create, run, monitor | `GET/POST /api/orchestrator/*` |
| Plugin management | Enable / disable / test plugins | `GET/POST /api/plugins/*` |
| Detection settings | eBPF / detection config toggle | `GET/POST /api/config` (detection.*) |
| DNS channel settings | DNS covert-channel config | `GET/POST /api/config` (dns_channel.*) |
| Proxy settings | Reverse-proxy config | `GET/POST /api/config` (proxy.*) |
| Comms config (5 missing channels) | Ntfy / Matrix / Twilio / Email / GitHub webhook | `GET/POST /api/config` (per-channel.*) |
| Template management | Create / edit / delete templates | `GET/POST /api/templates/*` |
| Routing rules editor | Create / test routing rules | `GET/POST /api/routing-rules/*` |
| Device alias manager | Map device IDs to friendly names | `GET/POST /api/device-aliases/*` |
| Cost rates editor | Configure per-model token rates | `GET /api/cost/rates` |
| Cooldown controls | Set / clear cooldown threshold | `POST /api/cooldown` |
| Audit log browser | Filter and page audit events | `GET /api/audit` |
| Pipeline manager | Start / cancel / list pipelines | `GET/POST /api/pipeline/*` |
| KG browser | Query, add, view knowledge graph | MCP `kg_*` tools |
| Branding / splash config | Logo upload, splash text | `GET/POST /api/splash/*` |
| Analytics view | Session analytics data | `GET /api/analytics` |
| Memory search / recall UI | Query episodic memory interactively | MCP `memory_recall` |

### MCP — missing dedicated tools
Features accessible via REST but relying solely on `config_set` from MCP (no dedicated tool):

| Missing Tool | Feature |
|---|---|
| `detection_status`, `detection_config_*` | eBPF detection surface |
| `dns_channel_config_*` | DNS covert-channel config |
| `proxy_config_*` | Reverse-proxy configuration |
| `analytics_query` | Analytics data access (exists in mcp but no CLI mirror for raw analytics) |

### CLI — missing subcommands
Features accessible via REST + MCP but not CLI:

| Missing Subcommand | Feature | MCP equivalent |
|---|---|---|
| `datawatch analytics` | Analytics queries | `analytics` MCP tool |
| `datawatch proxy` | Proxy config management | `config_set proxy.*` |

---

## Priority Tiers for v6.0 Closure

### Tier 1 — Operator-critical (impairs day-to-day use)
These gaps mean operators must fall back to `rest` passthrough or direct YAML editing for common tasks:

1. **PWA Observer panel** — operators managing federated peers have no web UI
2. **PWA Plugin management** — enable/disable/test without CLI access requires REST
3. **PWA Routing rules editor** — session routing config is settings-panel-adjacent
4. **Comm `orchestrator` command** — graph orchestration is an autonomous-adjacent workflow, chat-accessible peers need it
5. **Comm `plugins` command** — plugin enable/disable from chat channels is expected given agent parity

### Tier 2 — Configuration completeness
Features present but only partially exposed; completing them rounds out the surface:

6. **PWA Cost rates editor** — cost tracking without rate editing is half-useful
7. **PWA Comms config for all 9 channels** — only 4 channels in Settings
8. **Comm `templates` command** — template lifecycle from chat
9. **Comm `device-alias` command** — device management from mobile/chat
10. **PWA Cooldown controls** — status without set/clear misses the control half

### Tier 3 — Advanced / power-user
Present via raw REST; convenience wrappers are nice-to-have:

11. PWA Detection settings toggle
12. PWA DNS channel settings
13. PWA Proxy settings panel
14. CLI `analytics` subcommand
15. MCP `detection_*` / `dns_channel_*` / `proxy_*` dedicated tools
16. PWA Template management UI
17. PWA Audit log browser
18. PWA KG browser

---

## Relationship to Open Backlogs

| Gap | Related BL | Status |
|---|---|---|
| Autonomous `type`/`skill`/`file_path` surface parity | BL221 §12b Phase 4b | In design |
| Identity configure on all 7 surfaces | BL221 §12b (Q22) | In design |
| Full Configuration Accessibility Rule enforcement | BL220 | **This audit** |
| Observer/Federation surface | BL172 (S11) | Shipped (REST+MCP+CLI+peers Comm); PWA+full Comm gaps remain |
| Orchestrator surface | BL217 | Shipped (REST+MCP+CLI); Comm+PWA gaps remain |

---

## Notes on `rest` Passthrough

The Comm `rest <METHOD> <PATH> [json]` passthrough command (`sx2_parity.go:691`) provides a raw escape hatch for all REST endpoints from any chat channel. This means the gaps listed above do not prevent operators from using these features — they just require knowing the REST path rather than having a named command. For BL220 compliance the rule is dedicated named commands, not raw REST passthrough.

---

*Audit generated 2026-05-02. Last updated: 2026-05-02.*
*Source files reviewed: `internal/config/config.go`, `internal/router/commands.go`, `internal/mcp/sx_parity.go`, `internal/mcp/server.go`, `internal/router/sx2_parity.go`, `cmd/datawatch/main.go`, `cmd/datawatch/cli_sx_parity.go`, `cmd/datawatch/v52710_channel_cli.go`.*
