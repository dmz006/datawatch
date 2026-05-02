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
| **Observer / Federation** | ✅ | ✅ | ✅ | ✅ | ✅ | 🔴 | Comm: `observer` command added (G17 — v5.28.9). PWA: no observer settings or envelope browser |
| **Orchestrator** | ✅ | ✅ | ✅ | ✅ | ✅ | 🔴 | Comm: `orchestrator` command added (G4 — v5.28.9). PWA: stub nav button present; no graph UI yet |
| **Plugins** | ✅ | ✅ | ✅ | ✅ | ✅ | 🔴 | Comm: `plugins` command added (G5 — v5.28.9). PWA: stub nav button present; no management panel yet |
| **Detection (eBPF/BPF)** | ✅ | 🟡 | ✅ | 🟡 | ✅ | 🔴 | MCP: `detection_status`/`detection_config` tools added (G11 — v5.28.9). Comm: `detection` command added. CLI: `setup ebpf` + `diagnose` only. REST: only via `/api/diagnose` + config patch. PWA: no detection panel |
| **DNS Channel** | ✅ | 🟡 | ✅ | 🟡 | 🔴 | 🔴 | MCP: `dns_channel_config` tool added (G12 — v5.28.9). REST: config patch only. CLI: `setup dns` only. No Comm command or PWA panel |
| **Proxy** | ✅ | 🟡 | ✅ | ✅ | 🔴 | 🔴 | MCP: `proxy_config` tool added (G13 — v5.28.9). CLI: `datawatch proxy` added. REST: `/api/proxy/comm/` + config patch. No Comm command or PWA panel |
| **Communication channel config** | ✅ | 🟡 | 🔴 | 🟡 | 🟡 | ✅ | REST: no `/api/channels/*`; config patch only. MCP: `channel_info` (read-only); no set. CLI: `setup channel/*` (initial setup); `channel` (info). Comm: `channel_info` (read-only). PWA: all 10 channels exposed in Settings → Comms (G7 — already complete in codebase) |
| **Profiles (project + cluster)** | ✅ | ✅ | ✅ | ✅ | ✅ | 🟡 | PWA: profiles reachable from session create modal; no standalone profile management panel |
| **Templates** | ✅ | ✅ | ✅ | ✅ | ✅ | 🔴 | Comm: `templates` command added (G8 — v5.28.9). PWA: no template management UI |
| **Routing rules** | ✅ | ✅ | ✅ | ✅ | ✅ | 🔴 | Comm: `routing` command added (G9 — v5.28.9). PWA: stub nav button present; no routing rules editor yet |
| **Device aliases** | ✅ | ✅ | ✅ | ✅ | ✅ | 🔴 | Comm: `device-alias` command added (G16 — v5.28.9). No PWA device alias management |
| **Cost / billing rates** | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | Settings → LLM → Cost Rates (G6 — v5.28.9) |
| **Cooldown / rate limiting** | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | Settings → Monitor → Global Cooldown (G10 — v5.28.9) |
| **Pipeline** | ✅ | ✅ | ✅ | ✅ | ✅ | 🟡 | PWA: no dedicated pipeline management view |
| **Audit log** | ✅ | ✅ | ✅ | ✅ | ✅ | 🟡 | PWA: no audit log browser |
| **Scheduling (cron/alerts)** | ✅ | ✅ | ✅ | ✅ | ✅ | 🟡 | PWA: schedule creation from session controls; no full schedule manager |
| **Splash / branding** | ✅ | ✅ | ✅ | ✅ | ✅ | 🔴 | Comm: `splash` command added (G24 — v5.28.9). No PWA branding config panel |
| **Server / TLS config** | ✅ | 🟡 | 🟡 | 🟡 | 🔴 | 🔴 | REST/MCP/CLI: only via `config set`. No dedicated server-config surface |
| **Stale sessions** | ✅ | ✅ | ✅ | ✅ | ✅ | 🟡 | PWA: stale indicator only; no bulk-clean action |
| **Update management** | ✅ | 🟡 | 🟡 | ✅ | ✅ | 🟡 | PWA: update notification shown; no configure-update-channel UI |
| **Analytics** | ✅ | ✅ | ✅ | ✅ | ✅ | 🟡 | CLI: `datawatch analytics` added (G14 — v5.28.9). Comm: `analytics` command added. PWA: stats panel shown; no analytics config panel |

---

## Gap Summary by Surface

### Comm channel — missing commands
All 9 previously-missing Comm commands shipped in v5.28.9 (G4, G5, G8, G9, G11, G14, G16, G17, G24). No Comm gaps remain except DNS channel (no dedicated `dns_channel` command; covered by `config set`).

### PWA settings — missing panels / fields
Features accessible via REST + MCP but not yet from the web UI (Bundle F targets):

| Missing UI | Feature | REST surface |
|---|---|---|
| Observer panel | Observer config, envelope browser, peer stats | `GET/POST /api/observer/*` |
| Orchestrator panel | Graph list, create, run, monitor | `GET/POST /api/orchestrator/*` |
| Plugin management | Enable / disable / test plugins | `GET/POST /api/plugins/*` |
| Routing rules editor | Create / test routing rules | `GET/POST /api/routing-rules/*` |
| Detection settings | eBPF / detection config toggle | `GET/POST /api/config` (detection.*) |
| DNS channel settings | DNS covert-channel config | `GET/POST /api/config` (dns_channel.*) |
| Proxy settings | Reverse-proxy config | `GET/POST /api/config` (proxy.*) |
| Template management | Create / edit / delete templates | `GET/POST /api/templates/*` |
| Device alias manager | Map device IDs to friendly names | `GET/POST /api/device-aliases/*` |
| ~~Comms config (5 missing channels)~~ | ✅ All 10 channels present in `loadConfigStatus()` + `BACKEND_FIELDS` | `GET/POST /api/config` |
| ~~Cost rates editor~~ | ✅ Settings → LLM → Cost Rates (G6 — v5.28.9) | `GET/PUT /api/cost/rates` |
| ~~Cooldown controls~~ | ✅ Settings → Monitor → Global Cooldown (G10 — v5.28.9) | `GET/POST/DELETE /api/cooldown` |
| Audit log browser | Filter and page audit events | `GET /api/audit` |
| Pipeline manager | Start / cancel / list pipelines | `GET/POST /api/pipeline/*` |
| KG browser | Query, add, view knowledge graph | MCP `kg_*` tools |
| Branding / splash config | Logo upload, splash text | `GET/POST /api/splash/*` |
| Analytics view | Session analytics data | `GET /api/analytics` |
| Memory search / recall UI | Query episodic memory interactively | MCP `memory_recall` |

### MCP — missing dedicated tools
~~`detection_status`/`detection_config`~~, ~~`dns_channel_config`~~, ~~`proxy_config`~~ all shipped v5.28.9 (G11, G12, G13). No MCP gaps remain.

### CLI — missing subcommands
~~`datawatch analytics`~~ and ~~`datawatch proxy`~~ shipped v5.28.9 (G13, G14). No CLI gaps remain.

---

## Priority Tiers for v6.0 Closure

### Tier 1 — Operator-critical (impairs day-to-day use)
These gaps mean operators must use REST directly; no workaround-free web UI exists:

1. **PWA Observer panel** — operators managing federated peers have no web UI
2. **PWA Plugin management** — enable/disable/test without CLI access requires REST
3. **PWA Routing rules editor** — session routing config is settings-panel-adjacent
4. ~~**Comm `orchestrator` command**~~ — ✅ done (G4 — v5.28.9)
5. ~~**Comm `plugins` command**~~ — ✅ done (G5 — v5.28.9)

### Tier 2 — Configuration completeness
6. ~~**PWA Cost rates editor**~~ — ✅ done (G6 — v5.28.9)
7. ~~**PWA Comms config for all 9 channels**~~ — ✅ already complete in codebase (all 10 channels)
8. ~~**Comm `templates` command**~~ — ✅ done (G8 — v5.28.9)
9. ~~**Comm `device-alias` command**~~ — ✅ done (G16 — v5.28.9)
10. ~~**PWA Cooldown controls**~~ — ✅ done (G10 — v5.28.9)

### Tier 3 — Advanced / power-user (Bundle F targets)
Present via raw REST; PWA panels are the remaining gaps:

11. PWA Detection settings toggle
12. PWA DNS channel settings
13. PWA Proxy settings panel
14. ~~CLI `analytics` subcommand~~ — ✅ done (G14 — v5.28.9)
15. ~~MCP `detection_*` / `dns_channel_*` / `proxy_*` dedicated tools~~ — ✅ done (G11–G13 — v5.28.9)
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
| Observer/Federation surface | BL172 (S11) | Shipped (REST+MCP+CLI+`observer` Comm v5.28.9); PWA panel gap remains |
| Orchestrator surface | BL217 | Shipped (REST+MCP+CLI+`orchestrator` Comm v5.28.9); PWA graph UI gap remains |

---

## Notes on `rest` Passthrough

The Comm `rest <METHOD> <PATH> [json]` passthrough command (`sx2_parity.go:691`) provides a raw escape hatch for all REST endpoints from any chat channel. This means the gaps listed above do not prevent operators from using these features — they just require knowing the REST path rather than having a named command. For BL220 compliance the rule is dedicated named commands, not raw REST passthrough.

---

*Audit generated 2026-05-02. Last updated: 2026-05-02 (v5.28.9 — Bundle A–E closure).*
*Source files reviewed: `internal/config/config.go`, `internal/router/commands.go`, `internal/mcp/sx_parity.go`, `internal/mcp/server.go`, `internal/router/sx2_parity.go`, `cmd/datawatch/main.go`, `cmd/datawatch/cli_sx_parity.go`, `cmd/datawatch/v52710_channel_cli.go`, `cmd/datawatch/cli_bl220.go`, `internal/server/web/app.js`, `internal/server/web/index.html`.*
