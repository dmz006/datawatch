# Changelog

All notable changes to datawatch will be documented here.
Format based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).

## [Unreleased]

_(nothing pending)_

## [6.1.0] - 2026-05-03

### Summary

Minor release collecting the v6.0.x infrastructure hygiene patch series (BL218, BL219, BL226, BL228) into a tagged minor. No new features beyond what was already in v6.0.6–v6.0.9.

See [RELEASE-NOTES-v6.1.0.md](docs/plans/RELEASE-NOTES-v6.1.0.md) for a consolidated summary.

## [6.0.9] - 2026-05-03

### Added (BL226)

- **Service-level alert stream** — internal subsystem failures now emit structured alerts visible across all 6 surfaces:
  - **`Source` field** added to `Alert` struct (`source:"system"` for daemon-generated alerts, empty for session alerts). JSON field `source` (omitempty).
  - **`AddSystem(level, title, body)`** convenience method — creates a system alert with `source="system"` and no `SessionID`.
  - **`SetGlobal(store)` / `EmitSystem(level, title, body)`** package-level functions so any internal package can emit alerts without dependency injection.
  - **Instrumented failure sites**: pipeline task failure, pipeline executor panic, eBPF kprobe load failure, plugin `Fanout` invocation error.
  - **REST**: `GET /api/alerts?source=system` returns only system-sourced alerts.
  - **MCP**: `get_alerts` tool accepts `source` parameter (e.g. `source:"system"`); output includes `[system]` label.
  - **CLI**: `datawatch alerts --system` shows only system-sourced alerts.
  - **Comm**: `alerts system` filters to system-only alerts.
  - **PWA**: Alerts view gains a dedicated **System** tab (alongside Active/Inactive) with a red unread badge; system alerts no longer buried in Inactive collapsible.

## [6.0.8] - 2026-05-02

### Added (BL219)

- **LLM tooling artifact lifecycle** — new `internal/tooling` package and full 6-surface parity:
  - **Backend artifact registry** maps each LLM backend (`claude-code`, `opencode`, `aider`, `goose`, `gemini`) to its known project-dir file/dir patterns.
  - **`EnsureIgnored(projectDir, backend)`** appends artifact patterns to `.gitignore` (and `.cfignore`/`.dockerignore` if present) idempotently. Called on every `onPreLaunch` when `session.gitignore_check_on_start: true`.
  - **`CleanupArtifacts(projectDir, backend)`** removes ephemeral backend files on session end when `session.cleanup_artifacts_on_end: true`.
  - **REST**: `GET /api/tooling/status`, `POST /api/tooling/gitignore`, `POST /api/tooling/cleanup`
  - **MCP**: `tooling_status`, `tooling_gitignore`, `tooling_cleanup` tools
  - **CLI**: `datawatch tooling status|gitignore|cleanup`
  - **Comm**: `tooling [status [backend]] | gitignore <backend> | cleanup <backend>`
  - **PWA**: Backend Artifact Lifecycle section in Settings → General with per-backend status, gitignore, and cleanup buttons
- **3 new config fields** (YAML + REST-writable):
  - `session.cleanup_artifacts_on_end` (default: `false`) — remove ephemeral artifacts on session end
  - `session.gitignore_artifacts` (default: `["aider","goose","gemini"]`) — which backend patterns to manage
  - `session.gitignore_check_on_start` (default: `true`) — verify + update ignore files on every start

## [6.0.7] - 2026-05-02

### Changed (BL218)

- **Channel session-start hygiene** — three hardening improvements to the channel bridge lifecycle:
  - `EnsureExtracted` now uses a SHA-256 content hash instead of file size to detect a stale or corrupt `channel.js` on disk. The old size-only check would miss bit-flip corruption when the embedded and on-disk files were the same length.
  - New `SweepUserScopeMCPConfig` function checks `~/.mcp.json` on every pre-launch and rewrites the `datawatch` entry when it is stale — e.g. still pointing at `node + channel.js` after the Go bridge was installed, or pointing at a non-existent path. Preserves all operator-added entries.
  - `onPreLaunch` hook now logs `[channel] pre-launch: wiring <go|js> bridge for session <id>` at the top of the hook (before any MCP registration) so operators can confirm the active bridge kind without grepping boot logs.

## [6.0.6] - 2026-05-02

### Added (BL228)

Security scanner tools added to all five language Dockerfiles:

- **lang-go** — `govulncheck` v1.1.4 (`golang.org/x/vuln/cmd/govulncheck`) — scans Go modules for known CVEs
- **lang-python** — `bandit` v1.8.3 + `pip-audit` v2.8.0 (via pipx) — SAST + dependency vulnerability scanning
- **lang-node** — `eslint-plugin-security` v3.0.1 (global npm) — ESLint rules for common JS security anti-patterns
- **lang-rust** — `cargo-audit` v0.21.0 — scans Cargo.lock for RustSec advisories
- **lang-ruby** — `brakeman` v7.0.1 + `bundler-audit` v0.9.2 (via gem) — Rails SAST + Gemfile.lock CVE scanning

All tools are pinned to specific versions via `ARG` declarations and recorded in image `LABEL` metadata.

## [6.0.5] - 2026-05-02

### Added

- **Smoke coverage for BL220 additions** — 3 new sections in `scripts/release-smoke.sh`:
  - `§7y` — detection/dns_channel/proxy config sections readable (MCP tool prerequisite verification)
  - `§7z` — analytics endpoint shape check (`buckets` array with correct field names)
  - `§7aa` — comm commands `analytics` + `detection` via `/api/test/message` (SKIPped when loopback unavailable)

## [6.0.4] - 2026-05-02

### Added (BL210-remaining)

- **filter_list** — list all detection filters (pattern/action rules)
- **filter_add** — create a detection filter (pattern + action: alert|kill|redact|tag)
- **filter_delete** — delete a filter by ID
- **filter_toggle** — enable/disable a filter without deleting it
- **backends_list** — list configured LLM backends with cached availability status
- **backends_active** — probe backends for live reachability + version
- **session_set_state** — manually override a session's lifecycle state
- **federation_sessions** — list sessions aggregated from all federated observer peers
- **device_register** — register a mobile device for push notifications (FCM/APNS)
- **device_list** — list registered push-notification devices
- **device_delete** — deregister a device by ID
- **files_list** — browse the operator's configured project directory tree

All tools forward to the matching `/api/*` endpoint via the existing `proxyJSON` helper. 12 new tests added.

## [6.0.3] - 2026-05-02

### Changed (BL238)

- **BL238** — PWA layout restructure: Plugins, Routing, and Orchestrator panels removed from the bottom nav bar and promoted to dedicated sub-tabs in Settings. Navigation to these views (via `navigate('plugins')`, `navigate('routing')`, `navigate('orchestrator')`) automatically redirects to Settings with the corresponding sub-tab pre-selected. The nav bar now has four fixed items (Sessions, Autonomous, Alerts, Observer) plus Settings — no more hidden-and-shown conditional buttons for config panels.

## [6.0.2] - 2026-05-02

### Fixed (BL230–BL237)

- **BL230** — Session analytics panel was always empty: JS code read `b.total`/`b.errors` but the API returns `b.session_count`/`b.failed`+`b.killed`. Field names corrected; bar chart and success rate now render live data.
- **BL231** — Observer page config section showed `[object Object]` for nested config keys (process_tree, envelopes, peers, cluster, federation, ollama_tap). Nested objects now render as a compact `{key1, key2, …}` summary instead of the useless string conversion.
- **BL232** — Memory Maintenance card had a version-stamped intro paragraph ("v5.27.0 mempalace alignment surfaces…" with a link to old release notes) which violates the no-internal-version-in-UI rule. Intro removed; docs link added to section header.
- **BL233** — Container Workers settings section header showed "(F10)" sprint label in user-facing UI. Removed; label is now simply "Container Workers".
- **BL234** — Settings → General had a duplicate Language card that was also in Settings → About. Removed from General; About is the canonical location (matches v5.28.3 design intent).
- **BL235** — Branding / Splash config was in Settings → General but belongs in the app identity card. Moved to Settings → About alongside version, update, and orphaned-session controls.
- **BL236** — `permission_mode` and `default_effort` fields in Settings → LLM → claude-code were free-text inputs requiring the operator to remember valid values. Changed to `<select>` dropdowns: permission mode offers `plan / acceptEdits / auto / bypassPermissions / dontAsk / default`; effort offers `quick / normal / thorough`.
- **BL237** — BL220 new settings cards (Cost Rates, Global Cooldown, Session Templates, Device Aliases, Session Analytics, Audit Log, Pipeline Manager, Knowledge Graph) had no docs chip links. Docs links added to all section headers. "Global Cooldown (BL30)" internal ID removed from label.

## [6.0.1] - 2026-05-02

### Fixed

- **Scrollable bottom nav** — the nav bar now scrolls horizontally when more than ~6 items are visible. Each button has a fixed minimum width (68px) so icons and labels are always readable; the scrollbar is hidden (touch/drag works natively on mobile, mouse-drag on desktop). Tapping any nav item calls `scrollIntoView` so the active tab is always fully in view. Previously 8 buttons squeezed to unusable widths at typical PWA card sizes.

## [6.0.0] - 2026-05-02

### Major release — full surface parity + configuration accessibility closure

This release closes the v5.28.x patch window and marks the start of the v6.x feature series. All v5.26.x–v5.28.x work is included. See the comprehensive release notes at `docs/plans/RELEASE-NOTES-v6.0.0.md` for the full changelog since v5.0.0.

**BL220 Configuration Accessibility (Bundles A–F — v5.28.9 + v5.28.10):**
- 9 Comm channel commands: orchestrator, plugins, templates, routing, device-alias, detection, observer, analytics, splash
- 3 dedicated MCP tools: detection_status/config, dns_channel_config, proxy_config
- 2 CLI subcommands: `datawatch analytics`, `datawatch proxy`
- 4 PWA nav views: Observer, Plugins, Routing, Orchestrator
- 7 PWA settings panels: Session Templates, Device Aliases, Branding/Splash, Session Analytics, Audit Log, Pipeline Manager, Knowledge Graph
- 2 PWA settings panels: Cost Rates (LLM tab), Global Cooldown (Monitor tab)
- All 6 surfaces (YAML + REST + MCP + CLI + Comm + PWA) now cover every feature area

**Bug fixes (v5.28.5–v5.28.8):**
- BL227: Terminal refits after session-completion indicator clears
- BL223: RTK upgrade card no longer renders raw JS as visible text
- BL224/BL225: Mermaid diagrams in orchestrator-flow.md and prd-phase3-phase4-flow.md now render
- BL222: Settings → General no longer duplicates Claude-specific LLM fields
- BL216: Session state transitions correctly on completion for claude-code sessions
- MCP mode (SSE/stdio) visibility added to `/api/channel/info`

**Security:** G115 integer-overflow conversions in OS/syscall interface code documented and globally suppressed in `.gosec-exclude` (pre-existing, not new code).

## [5.28.10] - 2026-05-02

### Added (BL220 Bundle F — PWA management panels)

- **Settings → General → Session Templates** — lists all templates from `GET /api/templates`; inline "Add template" form (name, backend, project dir, effort, description) posts to `POST /api/templates`; × button deletes via `DELETE /api/templates/{name}`.
- **Settings → General → Device Aliases** — lists all device aliases from `GET /api/device-aliases`; inline "Add alias" form (alias → server) posts to `POST /api/device-aliases`; × button deletes via `DELETE /api/device-aliases/{alias}`.
- **Settings → General → Branding / Splash** — loads `GET /api/splash/info` to show current tagline and logo state; editable fields save `session.splash_tagline` and `session.splash_logo_path` via `PUT /api/config`; links to current logo when set.
- **Settings → Monitor → Session Analytics** — configurable range selector (7d / 14d / 30d / 90d), loads `GET /api/analytics`, renders per-day bar chart with total / ok / error counts and overall success rate percentage.
- **Settings → Monitor → Audit Log** — loads `GET /api/audit` with actor, action, and limit filters; renders timestamped entry list with details; "Load" button reruns the filtered query.
- **Settings → Monitor → Pipeline Manager** — loads `GET /api/pipelines`; shows state, task progress count, pipeline ID; "Cancel" button calls `POST /api/pipeline?action=cancel` for running/pending pipelines; refresh button.
- **Settings → Monitor → Knowledge Graph** — query panel calls `GET /api/memory/kg/query?entity=X`; renders subject/predicate/object triple table; "Add triple" form calls `POST /api/memory/kg/add` with source=pwa.

## [5.28.9] - 2026-05-02

### Added (BL220 Bundle A–E — configuration accessibility gap closure)

- **Nav: Observer / Plugins / Routing / Orchestrator panel stubs** (G1–G3, G15) — four new nav buttons added to `index.html` (hidden by default, shown when respective API endpoints respond). Locale keys `nav_observer`, `nav_plugins`, `nav_routing`, `nav_orchestrator` added across all five locale bundles.
- **9 new Comm channel commands** (G4, G5, G8, G9, G11, G14, G16, G17, G24) — `orchestrator`, `plugins`, `templates`, `routing`, `device-alias`, `detection`, `observer`, `analytics`, `splash` commands added to all Comm adapters via `sx2_parity.go`. Operators no longer need `rest` passthrough for these feature areas.
- **3 new MCP dedicated tools** (G11, G12, G13) — `detection_status`/`detection_config`, `dns_channel_config`, and `proxy_config` MCP tools added to `sx_parity.go`; no longer require raw `config_set` workaround.
- **CLI `analytics` and `proxy` subcommands** (G13, G14) — `datawatch analytics [--range=7d]` and `datawatch proxy [get|set]` added via `cli_bl220.go` and wired into the root command.
- **Settings → LLM → Cost Rates** (G6) — per-backend token rate table loads from `GET /api/cost/rates`; number inputs let operators override in/out rates; saved via `PUT /api/cost/rates`. "Reset to defaults" sends empty rates map so daemon falls back to built-in `DefaultCostRates()`.
- **Settings → Monitor → Global Cooldown** (G10) — active/inactive state, remaining time, and reason displayed. Six preset buttons (15m / 30m / 1h / 4h / 8h / 24h) plus optional reason field call `POST /api/cooldown`; "Clear" calls `DELETE /api/cooldown`. Re-renders in-place after each action.

## [5.28.8] - 2026-05-02

### Fixed (BL222)

- **Settings → General no longer shows Claude-specific fields** — four fields (`skip_permissions`, `channel_enabled`, `claude_auto_accept_disclaimer`, `permission_mode`) were duplicated between the General tab and Settings → LLM → claude-code, creating two edit surfaces for the same config keys. Removed from General; all four remain exclusively in the LLM → claude-code card. `session.default_effort` (effort hint: quick/normal/thorough) also moved from General to LLM → claude-code for the same reason.

### Fixed (BL223)

- **RTK upgrade card no longer renders raw JS as visible text** — the "→ latest version" and `curl …|sh` install command were broken by `JSON.stringify()` inside `onclick="…"` attributes, causing double-quotes to prematurely close the attribute and everything after the first `"` to appear as literal text. Replaced with `data-cmd` attribute + `addEventListener` wired after innerHTML assignment. Clicking the version badge or install command line now copies correctly to the clipboard.

### Fixed (BL224)

- **`orchestrator-flow.md` Mermaid diagram now renders** — unquoted node labels containing `]` characters (e.g. `V[…issues[]]`) and `<br/>` HTML caused premature token boundary splits. Quoted all affected labels; diagram renders cleanly in `/diagrams.html`.

### Fixed (BL225)

- **`prd-phase3-phase4-flow.md` Mermaid diagram now renders** — similar unquoted label issue: `G[story._conflictSet[file] = …]` and `L[render … <br/>'conflicts…']` had `[`, `]`, and `<br/>` in unquoted context. Quoted both labels; all three diagrams in the file render cleanly.

### Fixed (BL227)

- **Terminal refits after session completes** — when a session transitioned from `running` to `complete`/`killed`/`failed`, the 3-dot "generating…" indicator was removed from `#generatingSlot` but `fitAddon.fit()` was never called, leaving xterm undersized until the operator navigated away and back. Added the same `requestAnimationFrame(() => { fitAddon.fit(); send('resize_term', …) })` pattern already used by the dismiss-banner path (v5.26.44 / BL211).

## [5.28.7] - 2026-05-01

### Fixed

- **MCP mode visibility parity** — `/api/channel/info` now exposes `stdio_enabled` and `sse_enabled` fields (actual runtime MCP transport status), displayed in Settings → Monitor → MCP Channel Bridge. Resolves operator confusion when SSE is running but the Comms settings page toggles show as disabled (config vs. runtime state are now visually distinct).

## [5.28.6] - 2026-05-01

### Added (datawatch#34)

- **Process stats panel in PWA session view** — displays live CPU, RAM, thread count, file descriptor count, network throughput (Rx/Tx), and GPU metrics for the active session envelope. Metrics update every 1 second from `/api/stats?v=2` observer snapshot; colored CPU indicator (green <50%, yellow 50-80%, red >80%); network and GPU metrics appear only when non-zero. Mirrored from Android app's session details card for parity.

## [5.28.5] - 2026-05-01

### Fixed (datawatch#33)

- **KG tools now available in claude-code sessions** — the per-session `datawatch-channel` bridge exposed memory tools (v5.27.7) but was missing KG (knowledge graph) tools. Added `kg_add`, `kg_query`, `kg_timeline`, `kg_invalidate`, `kg_stats` to complete the MCP surface. All forward to existing `/api/kg/*` REST endpoints; same pattern as memory tools.

### Fixed (datawatch#36)

- **`observer.ebpf_enabled` boolean representation normalized** — YAML may represent the string field as quoted `"true"` or unquoted `true`, mixed case variants. Added `normalizeBooleanFields()` called after config load to ensure consistent lowercase form: `"true"`, `"false"`, or `"auto"`. Prevents silent parsing issues when the quoted form fails strict bool unmarshaling.

### Improved (datawatch#35)

- **CAP_BPF capability check now logs diagnostic info** — helps debug issue #35 (capability detection returning false despite `setcap`). Logs to stderr: running binary path (`os.Executable()`), CapEff hex value from `/proc/self/status`, and whether bit 39 is set. Enable with `observer.ebpf_enabled=true` or `auto`, visible in daemon logs.

## [5.28.4] - 2026-05-01

### Fixed (BL217)

- **`session.quick_commands` PUT /api/config now works** — the configuration field existed in YAML (writable via `datawatch config set` or direct YAML edit + reload) but the REST API had no handler case, causing all PUT writes to silently no-op. Operators using MCP `config_set` / CLI `datawatch config set` / comm `configure` could not modify the field. Configuration parity restored: reads and writes now work across YAML, REST, MCP, CLI, chat, and PWA.

## [5.28.3] - 2026-04-30

### Changed (BL214 UX fix)

- **Language picker promoted** to the top of Settings → About (the datawatch identity card), right under the icon + "AI Session Monitor & Bridge" header. Settings → General → Language kept for discoverability; both stay in sync.
- **PWA UI language now the default app language** — `setLocaleOverride()` syncs `whisper.language` (transcription input language) to the chosen UI locale via PUT /api/config. Picking `Auto` deliberately leaves `whisper.language` alone.
- **`whisper.language` form field removed from PWA Whisper card** — replaced with a read-only "tracks PWA language (Settings → About → Language)" indicator. New `readonly` field type added to the config-form renderer. Override path (`datawatch config set whisper.language <code>` or YAML) preserved per the configuration parity rule.
- **datawatch-app#40 + #41** filed for mobile parity (language picker placement + whisper sync; BL208 #30 PRD card style audit gap).

## [5.28.2] - 2026-04-30

### Closed

- **BL173-followup** — cluster→parent push verified end-to-end in the operator's testing cluster. Deployed `ghcr.io/dmz006/datawatch-parent-full:latest` v5.28.1 as a Deployment + ClusterIP Service in `bl173-verify` ns; ran a separate `curlimages/curl` peer Pod that hit `parent.bl173-verify.svc.cluster.local:8080` cross-node — register/push/aggregate/cleanup all returned `status:ok`. Resolves the original "dev-workstation parent isn't reachable from testing-cluster pod overlay" gap (production topology runs parent in-cluster anyway).

## [5.28.1] - 2026-04-30

### Added (BL214 wave-2 i18n)

- Confirm-modal Yes/No buttons translated via new `action_yes`/`action_no` keys.
- Session dialogs (Stop/Delete + batch-delete with `%1$d` count placeholder) wired to existing Android `dialog_*` keys.
- Alerts loading + empty state translated (`common_loading`/`common_no_alerts`).
- Autonomous-tab `templates` filter + New-PRD FAB title translated.
- 4 new universal keys added to all 5 bundles + filed at [datawatch-app#39](https://github.com/dmz006/datawatch-app/issues/39) for upstream Compose-Multiplatform mirror per the v5.28.0 Localization Rule.

### Added (BL173-followup runbook)

- Cluster→parent push handler verified end-to-end on the local daemon (peer `bl173-verify` round-tripped).
- New "Production-cluster reachability check (BL173-followup)" section in `docs/howto/federated-observer.md` with operator pod-side curl + cleanup + failure-mode triage.

## [5.28.0] - 2026-04-30

### Added (BL214 — PWA i18n foundation)

- 5 locale bundles (`internal/server/web/locales/{en,de,es,fr,ja}.json`, ~240 keys each) sourced 1:1 from datawatch-app `composeApp/src/androidMain/res/values{,-de,-es,-fr,-ja}/strings.xml`.
- Zero-dep `window._i18n` + `t(key, vars)` helper with Android-style `%1$s`/`%1$d` placeholders.
- `applyI18nDOM(root)` sweeps `data-i18n="<key>"` (with `-attr` and `-html` variants).
- Auto-detect: `localStorage('datawatch.locale')` override → `navigator.language` strip-to-base → `en` fallback.
- **Settings → General → Language** picker (Auto / EN / DE / ES / FR / JA), persisted in localStorage; reload applies.
- Initial coverage: bottom nav (Sessions/Autonomous/Alerts/Settings) + Settings tabs (Monitor/General/Comms/LLM/About).
- 3 parity tests in `internal/server/v5280_locales_test.go`.

### Added (Localization Rule)

- New `AGENT.md` section "Localization Rule (BL214, v5.28.0)" — every new user-facing string adds keys to all 5 bundles + wires through `t()`/`data-i18n` + files a datawatch-app issue requesting matching translations.

## [5.27.10] - 2026-04-30

### Added (BL216 — MCP channel bridge introspection, full parity)

- `GET /api/channel/info` returns `{kind, path, ready, hint, node_path, node_modules, stale_mcp_json: [...]}`.
- `channel_info` MCP tool (forwards to `/api/channel/info` via `proxyJSON`).
- `datawatch channel info` CLI subcommand (with `--json` flag).
- `datawatch channel cleanup-stale-mcp-json` CLI subcommand (with `--dry-run`).
- Chat-channel `channel info` command.
- PWA Settings → Monitor → MCP channel bridge panel (kind badge + path + ready state + stale-mcp-json warnings).
- Per-session register-time daemon log `[channel] session <id> registered with <kind> bridge at <path>`.

### Fixed (BL109)

- `WriteProjectMCPConfig` now writes `Command: <go-bridge>, Args: []` when `BridgePath()` is set, instead of hardcoding `node + channel.js`. The old behaviour produced stale `.mcp.json` files on Go-bridge hosts since v5.4.0.

## [5.27.9] - 2026-04-30

### Added (BL213 — Signal device-linking API completion, datawatch#31)

- `GET /api/link/qr` aliased to the existing SSE QR-pair stream.
- `GET /api/link/status` upgraded from placeholder to real impl: shells out to `signal-cli listDevices` and returns parsed device list via new `parseListDevicesOutput` helper.
- `DELETE /api/link/{deviceId}` invokes `signal-cli removeDevice -d <id>` with guardrails (rejects non-DELETE/missing/non-numeric ids, device id 1, missing `signal.account_number`).

### Added (BL212 follow-up, datawatch#29)

- Embedded JS channel fallback (`internal/channel/embed/channel.js`) gains `memory_remember`/`memory_recall`/`memory_list`/`memory_forget`/`memory_stats` MCP tools to match the Go bridge that got them in v5.27.7. Operator caught that ring-laptop / storage testing instances still hit the JS path via `~/.mcp.json` pointing at node.

## [5.27.8] - 2026-04-30

### Added (BL208 #30)

- New `.prd-card` CSS class harmonised with the Sessions card style; status drives the 4px left-border colour via `.prd-card-status-{draft,decomposing,needs_review,approved,running,completed,cancelled,blocked,rejected}` modifiers.
- Operator-asked: redundant "PRDs" sub-header on the Autonomous tab dropped.

### Added (BL210 — daemon-MCP coverage gap closures)

- 11 new MCP tools in `internal/mcp/v5278_gap_closures.go`: `memory_wal`, `memory_test_embedder`, `memory_wakeup`, `claude_models`, `claude_efforts`, `claude_permission_modes`, `rtk_version`, `rtk_check`, `rtk_update`, `rtk_discover`, `daemon_logs`. All forward to the matching `/api/*` path via the existing `proxyJSON` helper.

## [5.27.7] - 2026-04-30

### Added (BL208 #26 + #27)

- Running pulse animation + 3-dot generating indicator on session cards (CSS `@keyframes`, prefers-reduced-motion respected).
- Scroll-mode button glyph swapped `↕` → `📜` to match Android TerminalToolbar.

### Added (BL209, datawatch#28)

- New `GET /api/quick_commands` endpoint (config: `session.quick_commands`); falls back to a 15-entry baseline. Mobile + PWA migration off hardcoded button lists tracked at datawatch-app#31.

### Added (BL212, datawatch#29)

- `cmd/datawatch-channel/main.go` Go bridge gains `memory_remember/recall/list/forget/stats` MCP tools forwarding to the parent's `/api/memory/*` endpoints via new `callParent` helper.

## [5.27.6] - 2026-04-30

### Fixed (BL211 — scrollback state-detection)

- New `CapturePaneLiveTail()` method on `TmuxAPI`. State detection at `manager.go:1489` switched off `CapturePaneVisible` (which captures scrolled view in copy-mode for PWA display) onto the live tail. Operator scenario fixed: scrolling up no longer pins state detection on stale content.

### Fixed (BL215 — rate-limit miss)

- Per-line rate-limit length gate raised 200 → 1024 chars at `manager.go:3791` (modern claude rate-limit dialogs are paragraph-length).

## [5.27.5] - 2026-04-29

### Added (BL207 — claude permission_mode + model + effort)

- New REST endpoints `GET /api/llm/claude/{models,efforts,permission_modes}` (hardcoded lists; BL206 frozen per operator decision — no Anthropic /v1/models query).
- New `session.permission_mode` config field (`plan`/`acceptEdits`/`auto`/`bypassPermissions`/`dontAsk`/`default`); when set, claude-code launches with `--permission-mode <value>`.
- Per-session overrides via `POST /api/sessions/start` body (`permission_mode`, `model`, `claude_effort`).
- `PRD.PermissionMode` + `Task.PermissionMode` so PRDs can run a single design-only step inside an otherwise execute-the-plan PRD.
- PWA New Session modal gains a claude-only options block (Permission mode / Model / Effort dropdowns).
- New AGENT.md rule: every major release refreshes the hardcoded alias list against Anthropic's current set.

## [5.27.4] - 2026-04-29

### Added (BL205, datawatch#25)

- New read-only `GET /api/update/check` endpoint so mobile + PWA clients can implement "check → confirm → install" UX without firing the install on the first call.
- PWA `checkForUpdate()` migrated off direct api.github.com calls onto the daemon endpoint.

### Fixed

- Operator-reported rate-limit regression — `rateLimitPatterns` extended with modern claude-code phrasings (`limit reached`, `weekly usage limit`, `5-hour limit`, `opus/sonnet limit reached`).

## [5.27.3] - 2026-04-29

### Fixed

- v5.27.2 wired `SetReloadFn` on the production comm router but missed the `testRouter` that backs `POST /api/test/message`. Fixed by wiring symmetrically.

### Refactored

- `claudeDisclaimerResponse` extracted as a pure helper for unit-testability (4 new test cases).

## [5.27.2] - 2026-04-28

### Added (BL204 — subsystem hot-reload)

- New `POST /api/reload?subsystem=<name>` endpoint + `Server.RegisterReloader` API + named reloaders for `config` / `filters` / `memory`.
- Full parity: CLI `datawatch reload [subsystem]`, MCP `reload` tool with `subsystem` arg, chat `reload [subsystem]` command, REST endpoint, PWA Settings → General → Auto-restart.

### Added (claude auto-accept disclaimer)

- New `session.claude_auto_accept_disclaimer` config flag. When on + backend is `claude-code`, the existing FilterEngine `DetectPrompt` hook auto-sends `1\n` for "trust this folder" / "Quick safety check" and `\n` for "Loading development channels" after a 750ms debounce.
- Full parity: YAML + REST `PUT /api/config` + MCP `config_set` + CLI + comm + PWA.

## [5.27.1] - 2026-04-28

### Fixed

- Operator-reported: submitting a follow-up prompt resized xterm wrong + dropped the tmux input element's Enter handler. `refreshNeedsInputBanner` was patching slot innerHTML without the immediate `requestAnimationFrame → fitAddon.fit() → resize_term` sync. Fixed by comparing before/after banner HTML and running the same fit sequence on any change.

## [5.27.0] - 2026-04-28

Minor — Mempalace alignment with full configuration parity.

### Added

- New PWA **Memory Maintenance** section under Settings → Monitor → Memory mirrors the new tools (`sweep_stale`, `spellcheck`, `extract_facts`, `schema_version`).
- All Mempalace alignment work (v5.26.70 + v5.26.72) bundled behind full configuration parity per the project rule: every feature reachable from REST + MCP + CLI + comm channels + PWA.
- 1469 unit tests (+5 router parsing); smoke 72/0/4.
- [datawatch-app#21](https://github.com/dmz006/datawatch-app/issues/21) filed for mobile mirror.

### Notes

- Earlier v6.0.0 draft backed out before publish per operator clarification — that was supposed to be a minor pre-6.0 testing release. Re-cut as v5.27.0.

## [5.26.69] - 2026-04-28

Patch — close all remaining audit + plan items in one bundle.

### Added

- **`scripts/release-smoke-secure.sh`** — encryption-mode smoke
  runner (#57). Brings up encrypted daemon at port 18444 in temp
  data dir; verifies `encrypted=true`, memory save round-trip,
  restart-survives-encryption. SKIPs when DATAWATCH_SECURE_PASSWORD
  unset.
- **`docs/flow/prd-phase3-phase4-flow.md`** — mermaid sequence +
  state-machine diagrams for Phase 3+4 (#51). Added to
  diagrams.html index.
- **Mempalace audit gap table** in
  `docs/plans/2026-04-27-mempalace-alignment-audit.md`. 24 modules
  audited; 12 ✅ ported, 7 🟡 partial, 5 ⏳ gaps. Refined 5-item
  quick-win shortlist (#52).
- **`docs/plans/RELEASE-NOTES-v6.0.0-DRAFT.md`** — cumulative
  v5.0.0 → v6.0.0 release narrative (#60). Operator finalizes at
  cut.
- **3 new datawatch-app issues** filed under #10 umbrella:
  #18 Phase 3 per-story approval, #19 Phase 4 file association,
  #20 Unified Profile dropdown in New Session.

### Refreshed

- **`docs/howto/autonomous-planning.md`** — new Phase 4 file
  association subsection.
- **`docs/howto/screenshots/autonomous-*.png`** — 4 shots
  recaptured against v5.26.69 PWA (Phase 3 widgets + Phase 4
  file pills visible).

### Backlog cleared

#47–#60 all closed. Remaining v6-prep items are operator-prepared
(v6.0 cut) or PAT-gated (GHCR cleanup).

## [5.26.68] - 2026-04-28

Patch — six new smoke sections (§7n–§7s) closing 6 of 7 §41 audit gaps.

### Added

- **§7n KG add+query round-trip** (gap #1).
- **§7o Spatial-dim filtered search** (gap #2).
- **§7p Entity detection round-trip BL60** (gap #4).
- **§7q Per-backend channel send** — detects enabled backends;
  SKIPs when none (gap #3 partial; outbound smoke needs per-CI
  recipient config).
- **§7r Stdio-mode MCP tools** — subcommand presence check (gap
  #6 partial; full client wrapper deferred).
- **§7s Wake-up L4/L5 prerequisite check** (#39 prerequisite;
  full composition covered in unit tests).

## [5.26.67] - 2026-04-28

Patch — Phase 4 follow-ups: decomposer prompt + post-session diff + conflict detection + PWA edit modal. **Phase 4 fully implemented.**

### Added

- **Decomposer prompt** extended to ask LLM for `files: [...]`
  per story/task. JSON tag `files_planned` → `files` for
  consistency across LLM output / stored PRD / REST body.
- **`ProjectGit.DiffNames()`** — `git diff --name-only` runner;
  50-path cap.
- **Post-session diff callback** in `cmd/datawatch/main.go`. When
  a session is an autonomous-task spawn, fires
  `RecordTaskFilesTouched` automatically. No-op otherwise.
- **`Manager.FindTaskBySessionID`** lookup helper.
- **File-conflict detection** in `renderPRDRow` — flags two
  pending stories that plan the same file with ⚠ markers.
- **`openPRDEditStoryFilesModal` + `openPRDEditTaskFilesModal`** —
  PWA file-edit modals (textarea + mic + 50-cap). ✎ files button
  on story/task rows behind the lock-after-approve gate.

## [5.26.66] - 2026-04-28

Patch — docs/testing.md ↔ smoke coverage audit (#41).

### Added

- **`docs/plans/2026-04-28-testing-md-smoke-coverage-audit.md`**.
  Maps each operator-facing feature area in `docs/testing.md`
  to its current smoke section. 5 concrete future smoke
  additions identified. Session task list (#32–#46) cleared.

## [5.26.65] - 2026-04-28

Patch — §7m wake-up stack L0–L3 surface smoke (#39 partial).

### Added

- **§7m wake-up stack surface checks** in
  `scripts/release-smoke.sh`. Probes the underlying surfaces a
  regression in `internal/memory/layers.go` would break:
  - L0: `<data_dir>/identity.txt` present
  - L1+: memory subsystem reachable (via §7f / §9 reuse)
  L4 (parent context) + L5 (sibling visibility) still need a
  spawned-agent fixture; tracked. Full L0-L5 unit-test coverage
  remains in `internal/memory/layers_recursive_test.go`.

## [5.26.64] - 2026-04-28

Patch — PRD-flow Phase 4: file association (schema + REST + tests + PWA pills). **Phase 4 done; design backlog complete.**

### Added

- **`Story.FilesPlanned`** + **`Task.FilesPlanned`** +
  **`Task.FilesTouched`** fields. 50-path cap per list.
- **`Manager.SetStoryFiles`** / `SetTaskFiles` (operator-edit;
  lock-after-approve) + **`Manager.RecordTaskFilesTouched`**
  (daemon-internal post-spawn hook; no lock).
- **REST endpoints**:
  `POST /api/autonomous/prds/{id}/set_story_files`,
  `POST /api/autonomous/prds/{id}/set_task_files`.
- **`AutonomousAPI` interface** extended; test fake updated.
- **PWA file pills** on story rows (📝 planned, accent-blue) +
  task rows (📝 planned + ✅ touched, green).
- **4 unit tests** (RewritesAndCaps / RefusesAfterApprove /
  TaskRewritesAndAudits / PostSpawnNoLock). 475 unit tests
  passing total.

### Phase status — design backlog complete

- Phase 1 ✅ v5.26.30/34
- Phase 2 ✅ v5.26.32
- Phase 3 ✅ v5.26.60-62
- Phase 4 ✅ this patch
- Phase 5 ✅ v5.26.33
- Phase 6 🟡 howto + screenshots done; diagrams pending

### Deferred (follow-ups)

- Decomposer prompt update to extract `files: [...]` per
  story/task at decompose time.
- Post-session diff callback wiring to populate
  `Task.FilesTouched` from `git diff --name-only`.
- File-conflict detection (two stories planning the same file).
- PWA file-edit modal (currently REST-only).

## [5.26.63] - 2026-04-28

Patch — New Session unified Profile dropdown (operator-asked, parity with New PRD).

### Added

- **Profile + Cluster dropdowns in the New Session modal**.
  Operator-asked: *"New session should have same directory or
  profile and local daemon or cluster profile to start."*
  Mirrors the New PRD modal flow (v5.26.30/34/46). `__dir__`
  default → existing /api/sessions/start path; project profile
  selected → /api/agents spawn with project_profile +
  cluster_profile (empty cluster = local daemon).
- **`_sessProfileChanged`** + **`populateSessionProjectClusterDropdowns`**
  helpers — same shape as `_prdNewProfileChanged` /
  `openPRDCreateModal`. Reuse `state._prdProjectProfiles` /
  `state._prdClusterProfiles` caches.

## [5.26.62] - 2026-04-28

Patch — PRD-flow Phase 3.C + 3.D (PWA widgets + Settings toggle + smoke + howto). **Phase 3 complete.**

### Added

- **Per-story PWA widgets**: status pill, profile pill (`prof:
  (inherit)` button → set_story_profile modal), Approve / Reject
  buttons (visible when story is `awaiting_approval` and PRD is
  approved/running), rejected-reason rendered in red below the
  title.
- **Settings → General → Autonomous → "Per-story approval gate"**
  toggle. Saves through the existing config-section dotted-key
  handler — full parity (YAML / REST / MCP / CLI / comm).
- **§7l smoke** — toggles `autonomous.per_story_approval` on,
  runs through PRD decompose → approve → verify stories
  transition to `awaiting_approval` → exercises
  `set_story_profile` / `approve_story` / `reject_story` →
  validates audit decisions → restores flag. Gates skip-style
  when decompose backend is slow.
- **`docs/howto/autonomous-planning.md`** subsection on per-story
  profile + approval gate, with REST endpoint shapes and audit
  decision kinds.

### Phase 3 status — done

- 3.A schema + manager + REST + tests ✅ (v5.26.60)
- 3.B Manager.Run gating + config flag ✅ (v5.26.61)
- 3.C PWA per-story widgets ✅ (this patch)
- 3.D smoke + howto refresh ✅ (this patch)

## [5.26.61] - 2026-04-28

Patch — PRD-flow Phase 3.B: Manager.Run gating + per_story_approval config flag.

### Added

- **`autonomous.per_story_approval` config knob** (default false)
  with full parity (YAML / REST GET+PUT / MCP / CLI / comm).
  When true, `Manager.Approve` transitions every fresh story
  to `awaiting_approval` and the runner skips those.
- **`Manager.Config.PerStoryApproval`** field (Manager-side
  mirror of the daemon config knob).

### Changed

- **`flattenTasks` skips `awaiting_approval` and `blocked`
  stories** so the runner ignores them. Re-entering the runner
  after `ApproveStory` (which transitions to `pending`) picks
  up the now-runnable tasks.

### Phase 3 status

- 3.A schema + REST + tests ✅ (v5.26.60)
- 3.B Manager.Run gating + config flag ✅ (this patch)
- 3.C PWA per-story widgets — pending
- 3.D smoke probe + howto refresh — pending

471 unit tests passing; smoke unaffected by default (gate stays
off unless operator opts in).

## [5.26.60] - 2026-04-28

Patch — PRD-flow Phase 3.A: schema + manager methods + REST + tests.

### Added

- **`PRD.DecompositionProfile`** field (existing
  `PRD.ProjectProfile` re-purposed as default execution profile).
- **`Story.ExecutionProfile`**, `Story.Approved`,
  `Story.ApprovedBy`, `Story.ApprovedAt`,
  `Story.RejectedReason` fields.
- **`StoryStatus = "awaiting_approval"`** const.
- **`Manager.SetStoryProfile`** (lock-after-approve;
  validates against the profile resolver if wired).
- **`Manager.ApproveStory`** (transitions
  `awaiting_approval`→`pending`).
- **`Manager.RejectStory`** (sets `blocked` + reason; reason
  required).
- **REST endpoints** under `/api/autonomous/prds/{id}/`:
  `set_story_profile`, `approve_story`, `reject_story`.
- **`AutonomousAPI` interface** extended; test fake updated.
- **6 unit tests** in `internal/autonomous/lifecycle_test.go`
  for the new methods.

### Phase 3 status

- 3.A schema + REST + tests ✅ (this patch)
- 3.B Manager.Run gating + config flag — pending
- 3.C PWA per-story widgets — pending
- 3.D smoke probe + howto refresh — pending

471 unit tests passing (was 465; +6 net). Smoke unaffected
(58/0/1) — Phase 3 doesn't activate without the config flag.

## [5.26.59] - 2026-04-28

Patch — ZAP customized per interface (PWA + API + diagrams).

### Changed

- **`.github/workflows/owasp-zap.yaml` upgraded to three scan
  passes:** PWA baseline (passive spider), API scan
  (schema-driven against `docs/api/openapi.yaml`), and a
  separate diagrams.html baseline. All three share one kind +
  helm setup so total runtime ~25-35 min vs the previous
  ~12 min single-baseline. Issue titles prefix the pass so
  operator can route findings. Operator-asked: *"Are zap
  audits customized to api, pwa and other interfaces?"*
  Authenticated scans still tracked as a follow-up.

## [5.26.58] - 2026-04-28

Patch — Session worker badge + 3 new security workflows (gitleaks, dep-review, OWASP ZAP).

### Added

- **Session-card "⬡ worker" badge** when `sess.agent_id` is set
  (session lives inside a parent-spawned container worker).
  Operator-asked driver-kind / recursion indicator. PRD
  recursion-depth + parent badges already exist (BL191 Q4
  v5.16.0).
- **`.github/workflows/secret-scan.yaml`** — gitleaks on PR,
  push, weekly cron, manual. Operator-asked CI parity with
  datawatch-app.
- **`.github/workflows/dependency-review.yaml`** — actions/
  dependency-review-action on PR. Fails on HIGH-severity vuln
  or `GPL-3.0-only` / `AGPL-3.0-only` license.
- **`.github/workflows/owasp-zap.yaml`** — workflow_dispatch
  only. Spins up kind, deploys chart, runs ZAP baseline
  (passive). For operator pre-cut audits.

All three use SHA-pinned actions per v5.26.38 convention.

## [5.26.57] - 2026-04-28

Patch — §7k claude skip_permissions smoke + targetable smoke + smoke-frequency rule revised.

### Added

- **§7k claude `skip_permissions` config round-trip.** GET +
  PUT /api/config toggle/verify/restore for
  `session.skip_permissions` (maps to
  `cfg.Session.ClaudeSkipPermissions` internally; controls
  whether claude-code launches with
  `--dangerously-skip-permissions`). 2 new PASS; smoke now
  58/0/1 (was 56/0/1).
- **`SMOKE_ONLY=` env var** for targeted smoke runs:
  `SMOKE_ONLY=1,7k bash scripts/release-smoke.sh` runs only the
  matching sections; others print
  `(skipped — not in SMOKE_ONLY=...)`. Operator-asked.

### Changed

- **Smoke-frequency rule revised** in `AGENT.md` and the
  matching memory file. Operator directive 2026-04-28: smoke
  required on minor + major releases plus the first patch of
  any new feature. Patches with no new feature can ship
  without a full smoke pass; targeted runs cover regressions
  for what specifically changed. Supersedes the 2026-04-27
  "every release" rule which was overcorrection.

## [5.26.56] - 2026-04-27

Patch — Container Workers PWA config + interactive Whisper test dialog.

### Added

- **Settings → General → Container Workers (F10)** section.
  Operator-asked: *"Where in the pwa settings is the agent
  configuration that was enabled for the smoke tests being set
  up?"* Renders every `cfg.Agents` knob (`image_prefix`,
  `image_tag`, `docker_bin`, `kubectl_bin`, `callback_url`,
  `bootstrap_token_ttl_seconds`,
  `worker_bootstrap_deadline_seconds`) as labelled inputs.
  Saves through `PUT /api/config` dotted-keys (same path every
  other channel uses). Closes the F10 config-parity gap.

### Changed

- **Settings → Voice → Test transcription endpoint** now opens
  an interactive recording dialog. Operator-asked: *"the test
  whisper i expected it would open a dialog with a mic button
  to test, not just backend test."* Mic button → record → stop
  → transcript appears in the modal with latency + char count.
  The pre-v5.26.56 silent-WAV health check stays reachable via
  the "Run silent-WAV health check" button inside the dialog.
  New flow does NOT auto-disable Whisper on bad transcribe;
  operator decides.

## [5.26.55] - 2026-04-27

Patch — §7j F10 agent lifecycle smoke probe (operator-asked).

### Added

- **`scripts/release-smoke.sh` §7j F10 agent lifecycle**: GET
  `/api/agents` shape → POST spawn against the persistent
  `datawatch-smoke` + `smoke-testing` fixtures → verify agent
  appears in list → check `~/.datawatch/auth/audit.jsonl`
  growth (BL113 broker mint or mint-fail entry) → DELETE
  /api/agents/{id} → 204 with token-revoke path triggered.
  Token cleanup invariant per operator: each spawn either
  mints+revokes cleanly or records mint-fail (no leaked
  unrevoked token). 5 new PASS; smoke now 56/0/1 (was 51/0/1).

## [5.26.54] - 2026-04-27

Patch — Phase 6 screenshots recaptured against current PWA shape.

### Changed

- **`scripts/howto-shoot.mjs` `autonomous-new-prd-modal` recipe**
  updated to find the FAB by `id="prdNewFab"` (with text + aria-
  label fallbacks for older PWA builds). v5.26.36's FAB rework
  removed the "New PRD" button text the recipe was relying on.

### Refreshed

- `docs/howto/screenshots/autonomous-landing.png`
- `docs/howto/screenshots/autonomous-prd-expanded.png`
- `docs/howto/screenshots/autonomous-mobile.png`
- `docs/howto/screenshots/autonomous-new-prd-modal.png`

All four reflect the v5.26.30/32/36/37/46 PWA UX (unified Profile
dropdown, story description rendering, ✎ edit affordances, FAB,
filter toggle, dir picker).

## [5.26.53] - 2026-04-27

Patch — three design plan docs + 7 datawatch-app issues for PWA mirror.

### Added

- **`docs/plans/2026-04-27-prd-phase3-per-story-execution.md`** —
  per-story execution profile + per-story approval gate.
  Schema additions, 4 new REST endpoints, PWA changes,
  Manager.Run impact, test plan. Design only.
- **`docs/plans/2026-04-27-prd-phase4-file-association.md`** —
  `FilesPlanned` (LLM-extracted) + `FilesTouched` (post-hoc from
  diff). REST surface, PWA file-pill + conflict highlight,
  decomposer prompt change, post-session callback wiring.
- **`docs/plans/2026-04-27-mempalace-alignment-audit.md`** —
  audit frame, current-state matrix, three-step procedure,
  provisional quick-win shortlist (auto-tagging / pinning /
  conversation-window stitching).

### Issues filed

- 7 child issues under [datawatch-app#10](https://github.com/dmz006/datawatch-app/issues/10):
  #11–#17 covering Profile dropdown, story edit, FAB, dir
  picker, response filter, banner refresh, diagrams viewer.

## [5.26.52] - 2026-04-27

Patch — schedule + channel-send smoke probes (service-function audit).

### Added

- **§7h schedule store CRUD** — POST `new_session` with future
  `run_at`, GET round-trip, DELETE cancel. 3 new PASS.
- **§7i channel send round-trip** — POST `/api/test/message`
  with `help` + `list`, verify canonical `{count, responses}`
  shape. 2 new PASS. Smoke now 51/0/1 (was 46/0/1).

## [5.26.51] - 2026-04-27

Patch — diagrams viewer: `<img>` rewrite + heading ids + anchor scroll (v5.26.50 follow-ups).

### Added

- **`<img>` rewriter** in `rewriteRelativeMdLinks`. Howto
  screenshots like `![](screenshots/foo.png)` now resolve to
  `/docs/howto/screenshots/foo.png` instead of 404'ing against
  `/diagrams.html`. Same shape as the `<a>` rewriter.
- **Heading slug ids** (`<h1>`–`<h6>`) on prose render —
  lowercase Unicode-alphanumerics + dashes, collision-safe.
- **Scroll-to-anchor** after render: if the location hash
  carries a `#anchor` suffix, smooth-scroll the matching
  heading into view.

Closes the v5.26.50 follow-ups: anchor-target scrolling and
`<img>` rewrite for image paths inside howtos.

## [5.26.50] - 2026-04-27

Patch — diagrams viewer: howto README default + anchor-fragment links work.

### Changed

- **Diagrams viewer default landing page is `docs/howto/README.md`**
  (was `docs/architecture-overview.md`). Operator-asked: *"Howto
  readme should be default page."*

### Fixed

- **Doc links with anchor fragments now resolve.** Operator-asked:
  *"Not all docs links work in the diagrams, verify."* The
  `openFromHash` matcher gated on `h.endsWith('.md')`, which
  rejected hashes like `#docs/howto/profiles.md#walkthrough`
  (the trailing `#walkthrough` made the hash NOT end in `.md`)
  and silently redirected to the default doc. v5.26.50 splits
  on the first `#` between path and anchor, validates the path
  part, opens the doc. Anchor-target scrolling not yet honored
  (separate slug-id pass); doc-level link fidelity restored.

## [5.26.49] - 2026-04-27

Patch — yellow "Input Required" banner now refreshes on bulk `sessions` WS pushes too.

### Fixed

- **`onSessionsUpdated` calls `refreshNeedsInputBanner` on the
  session-detail branch.** Operator-reported: *"If I'm in a
  session and it ends, the yellow box with prompt details
  doesn't show up, i have to exit and re enter the session for
  it to display."* The single-session `session_state` WS message
  triggered the banner refresh via `updateSession`; the bulk
  `sessions` push went through `onSessionsUpdated` which only
  refreshed the action buttons. When the prompt-context arrived
  via the bulk path (which is what fires after most state
  transitions), the banner stayed hidden until a full re-render.
  v5.26.49 calls the idempotent refresh on both paths.

## [5.26.48] - 2026-04-27

Patch — MCP tool surface smoke probe (service-function audit).

### Added

- **`scripts/release-smoke.sh` §7g — MCP tool surface**.
  `/api/mcp/docs` returns the canonical JSON tool list (39 tools
  today). Smoke asserts (a) count ≥30 floor, (b) foundational
  subset registered: `list_sessions`, `start_session`,
  `send_input`, `schedule_add`, `profile_list`, `agent_list`.
  Catches MCP-registration regressions without needing an MCP
  client. Smoke now 46/0/1 (was 45/0/1).

## [5.26.47] - 2026-04-27

Patch — memory + KG round-trip smoke probe (service-function audit, expanded).

### Added

- **`scripts/release-smoke.sh` §7f — memory + KG round-trip**.
  Five new checks: `/api/memory/stats` health gate;
  `/api/memory/kg/stats` shape (4 counter keys); `POST
  /api/memory/save` with spatial dims (wing/room/hall); search
  round-trip; cleanup. Smoke now reports 45 pass / 0 fail / 1
  skip (was 40/0/1). The §9 memory check only covered search;
  this expands to the write + KG sides. Wake-up stack layers,
  agent diaries, KG contradictions, closets/drawers still
  require richer fixtures and stay in the audit gap list.

## [5.26.46] - 2026-04-27

Patch — three operator-asked UX items: filter icon to header; PRD dir picker; mkdir while browsing.

### Changed

- **Autonomous filter toggle moved to top header bar.** Operator-
  asked: filter icon should match the magnifying-glass on
  sessions and live in the header bar next to the server-status
  indicator. The in-panel `⛁` button is removed; clicking the
  header `#headerSearchBtn` on autonomous calls
  `_toggleAutonomousFilters()`. Tooltip switches per view.
- **New PRD project-directory is now a click-to-browse picker.**
  Operator-asked: should be a selector like New Session. The
  `<input type="text">` is replaced with `selectedDirDisplay` +
  `dirBrowser` matching the existing pattern. Element IDs are
  shared (modals are mutually exclusive). Submit handler reads
  `newSessionState.selectedDir`.

### Added

- **"+ New folder" button in the dir browser.** Operator-asked:
  *"need to be able to create a directory while browsing"*. The
  daemon-side `POST /api/files {action:"mkdir"}` endpoint already
  existed — the UI now exposes it. Refuses path-separator chars
  client-side; refreshes the listing on success. Available in
  both New Session and New PRD dir browsers.

## [5.26.45] - 2026-04-27

Patch — daemon-restart-in-session screen recovery (v5.26.35 follow-up).

### Fixed

- **Reconnect now sends fresh `resize_term` after daemon restart.**
  Operator-reported repeat: *"when datawatch daemon restarts and
  i'm in a session the screen gets messed up, loses tmux chat
  and i have to exit and reenter session to get view back."*
  v5.26.35 kept the DOM healthy on reconnect but missed the tmux
  side: pane geometry could drift during the disconnect window
  (browser resize, tmux re-attach default size). xterm at the new
  size receiving frames at the old size → garbled output.
  v5.26.45 sends an explicit `resize_term` with the live xterm
  `cols`/`rows` immediately after re-subscribe so the daemon
  reshapes the tmux pane and the next `pane_capture` redraws
  cleanly.

## [5.26.44] - 2026-04-27

Patch — yellow "Input Required" banner dismiss no longer leaves the terminal mis-sized.

### Fixed

- **`dismissNeedsInputBanner` now refits the xterm viewport
  synchronously.** Operator-reported: *"if there is a yellow popup
  in pwa and i close it, the screen doesn't resize properly and i
  have to exit and go back into sessions to fix."* The
  ResizeObserver on the terminal container DID fire but the 200ms
  debounce + the lack of an explicit `resize_term` round-trip
  meant the operator saw a busted layout long enough to need the
  navigate-away-and-back workaround. After the dismiss flag flips,
  v5.26.44 calls `state.termFitAddon.fit()` inside
  `requestAnimationFrame` (ensures DOM has reflowed) and sends a
  `resize_term` WS message with the freshly-computed cols/rows so
  tmux reshapes immediately.

## [5.26.43] - 2026-04-27

Patch — kind-cluster smoke workflow (last big CI residual from v5.26.25 audit).

### Added

- **`.github/workflows/kind-smoke.yaml`**. Spins up kind v0.24.0,
  builds `agent-base` locally, `kind load`s the image, helm-
  installs the chart with `image.pullPolicy=Never`, port-forwards
  to `:18443`, and runs `scripts/release-smoke.sh` against the
  in-kind daemon. Triggers on `pull_request` paths-filtered to
  chart/Dockerfile/smoke/cmd/internal/server changes plus
  `workflow_dispatch`; not on tag pushes (5–7 min runtime; PR
  coverage is the gate). Failure path dumps pod logs +
  `kubectl describe`. Always tears down the cluster.

## [5.26.42] - 2026-04-27

Patch — `Dockerfile.agent-goose` written + wired into CI publish.

### Added

- **`docker/dockerfiles/Dockerfile.agent-goose`**. Single-stage:
  pulls Block goose's self-contained Rust binary tarball from
  GitHub Releases (default `GOOSE_VERSION=1.32.0`), extracts to
  `/usr/local/bin/goose`. URL pattern + tarball layout (`./goose`
  entry) validated locally; defensive `test -x` post-check fails
  the build loudly if the binary is missing.
- **`agent-goose` row in `containers.yaml` stage 2 build matrix**.
  After v5.26.42, GHCR carries
  `ghcr.io/dmz006/datawatch-agent-goose:<version>` alongside the
  other agent images. Closes the agent-goose CI residual from the
  v5.26.25 audit.

## [5.26.41] - 2026-04-27

Patch — filter store CRUD smoke probe (service-function audit, partial).

### Added

- **`scripts/release-smoke.sh` §7e — filter CRUD round-trip**.
  Operator directive (service-function audit): every store with
  REST CRUD should round-trip in smoke. Filters are the simplest
  shape (pattern + action + value). Three checks: create via POST,
  visibility via GET, delete via DELETE. Smoke now reports 40/0/1
  (was 37/0/1). Schedule CRUD deferred (deferred-execution timing
  not smoke-friendly without time control); alerts won't get a
  CRUD smoke (mostly read-only acks).

## [5.26.40] - 2026-04-27

Patch — gosec baseline-diff: gosec job now blocking on net-new findings.

### Added

- **`.gosec-baseline.json`** committed at the repo root. Records
  the documented accepted gosec finding count (42 total) and
  per-rule breakdown. CI compares live count against this file;
  fails on net-new findings.
- **`security-scan.yaml` gosec job is now blocking**
  (`continue-on-error` removed). Runs gosec, captures JSON,
  prints per-rule live-vs-baseline diff, exits 1 when live
  exceeds baseline. Closes the "gosec baseline-diff mechanism"
  CI residual from the v5.26.25 audit. Also emits `::notice::`
  when live count drops *below* baseline so operator gets a
  prompt to tighten the gate.

## [5.26.39] - 2026-04-27

Patch — autonomous-planning howto refreshed (PRD-flow phase 6 partial).

### Docs

- **`docs/howto/autonomous-planning.md` updated for v5.26.30 → v5.26.37
  UX changes.** New header-layout note (PRDs label + filter toggle +
  collapsed filter row + bottom-right (+) FAB). New "Submit a spec
  from the PWA" subsection covering the unified Profile dropdown
  (v5.26.30) + cluster "Local service instance" default (v5.26.34).
  Story-level review + edit subsection (v5.26.32) covers the ✎
  button + `POST .../edit_story` endpoint + title-only-keeps-
  description rule. Reachability table's PWA row updated.
  Screenshots recapture remains pending (phase 6 full sweep).

## [5.26.38] - 2026-04-27

Patch — pinned action SHAs across every workflow (CI residual from v5.26.25 audit).

### Changed

- **All 22 `uses:` references across `.github/workflows/*.yaml`
  pinned to commit SHAs.** Tracks: `actions/checkout@v4`,
  `actions/setup-go@v5`, `docker/setup-qemu-action@v3`,
  `docker/setup-buildx-action@v3`, `docker/login-action@v3`,
  `docker/build-push-action@v5`,
  `softprops/action-gh-release@v2`. Zero floating `@vN`
  references remain. Each pin carries a trailing `# vN` comment
  naming the major version. Bump procedure documented in
  `containers.yaml` header.

## [5.26.37] - 2026-04-27

Patch — FAB size consistency + remove FAB from alerts page.

### Fixed

- **PRD FAB now reuses canonical `.new-session-fab` CSS class.**
  Operator-asked: *"FAB on automations page is not the same size as
  FAB on sessions page."* v5.26.36 used inline 48×48 styling; the
  sessions FAB is 56×56 with bottom-nav clearance + safe-area
  handling. Both FABs now identical.
- **Alerts page no longer shows the new-session FAB.** Operator-
  asked: *"FAB is not necessary on alerts page."* The visibility
  condition `view === 'sessions' || view === 'alerts'` always
  pointed the FAB at `openNewSessionModal()` regardless of which
  view was open — misleading on alerts. Now `view === 'sessions'`
  only.

## [5.26.36] - 2026-04-27

Patch — PRD panel UX polish (FAB + collapsible filter row) + backlog refactor.

### Changed

- **PRD panel header** simplified to a label + filter toggle (⛁).
  Operator-asked: *"new prd should be a FAB (+) and not the new prd
  button at top. There should be a filter icon like sessions list
  to hide/show the filter and sort options, with it hidden by
  default."* Filter row (status dropdown + templates checkbox)
  collapsed by default; toggle reveals it.
- **New PRD is now a Floating Action Button** anchored bottom-right
  of the autonomous view (48×48 circle, accent2 fill, large `+`
  glyph). The top-of-panel "+ New PRD" button is gone.

### Docs

- **`docs/plans/2026-04-27-v6-prep-backlog.md` refactored.** Operator-
  asked: *"refactor backlog. make sure active work and other areas
  that things are done are refactored into the correct closed
  sections."* Open section now reflects only actual remaining work
  (Per-session workspace reaper moved to Closed; CI items
  consolidated to residual-only); Closed section expanded from 9 to
  30 entries covering every patch v5.26.6 → v5.26.35.

## [5.26.35] - 2026-04-27

Patch — tmux toolbar + screen format survive daemon restarts.

### Fixed

- **Session view no longer breaks on daemon restart.** Operator-
  reported: *"when service restarts, if i'm in a session and it
  refreshes the tmux bar goes away and the screen format is messed
  up, i have to exit the session and go back in to reset."* The WS
  reconnect handler was calling `renderSessionDetail()`
  unconditionally, which rebuilt the toolbar HTML and detached the
  xterm.js mount, orphaning `state.terminal`. New rule: when the
  reconnect lands and we still have a working terminal for the
  same session, just re-subscribe to the pane stream + flag a
  one-shot pane_capture refresh. The next frame heals any drift
  via `terminal.reset() + write(lines)` without touching the
  toolbar DOM. Full re-render still happens on first visit / view
  switch / failed init.

## [5.26.34] - 2026-04-27

Patch — Cluster dropdown leads with "Local service instance"; v5.26.30 cluster-required check reverted.

### Changed

- **New PRD modal cluster dropdown** — operator clarification: *"the
  prd automation, since it supports profile on local disk the
  cluster profile selection should support local service instance
  in addition to a cluster. cluster should be only if non local
  disk profile is selected."* First option is now `— Local service
  instance (daemon-side) —` (empty value, daemon-side clone path).
  v5.26.30's required-field check on cluster removed when a project
  profile is selected — operator can pick a real cluster from the
  list to dispatch remotely or leave the default to run on the
  local daemon.

## [5.26.33] - 2026-04-27

Patch — persistent smoke fixtures (PRD-flow phase 5).

### Added

- **Persistent smoke fixtures: `smoke-testing` cluster + `datawatch-
  smoke` project profile** in `scripts/release-smoke.sh`. Operator
  directive: *"the testing cluster can be configured on the local
  server and left there for future tests and a test profile can be
  used with datawatch git and opencode as llm for prd and opencode
  as llm for coding for smoke tests."* New §7d creates the pair
  idempotently — created once, reused on every subsequent smoke run
  via name-keyed GET probes, never added to cleanup_log so they
  outlive the trap-on-EXIT cleanup. Project profile pinned to the
  datawatch repo with `image_pair.agent: agent-opencode`. Smoke
  reports 37 pass / 0 fail / 1 skip (was 34/0/1).

## [5.26.32] - 2026-04-27

Patch — story-level review + edit (phase 2 of operator's PRD-flow rework).

### Added

- **Story edit affordance** — operator-asked: *"i don't see a story
  review or approval or story edit option."* The PRD detail panel
  now renders each story's `Description` (was stored but never
  shown) and exposes an ✎ button while the PRD is in
  `needs_review` / `revisions_asked`. Modal takes title + multi-
  line description; Save round-trips through the new
  `POST /api/autonomous/prds/{id}/edit_story` endpoint.
- **`Manager.EditStory`** + matching `API.EditStory` wrapper +
  `AutonomousAPI` interface entry. Same lock-after-approve gate as
  `EditTaskSpec`; appends a `kind: edit_story` decision with actor
  + char counts to the PRD's audit timeline. Empty
  `new_description` preserves the existing value (title-only edits
  don't clobber the description).

### Tests

- 3 new unit tests in `internal/autonomous/lifecycle_test.go`:
  rewrite-and-audit, title-only-keeps-description, refuse-after-approve.
  465 unit tests passing total (was 462).

## [5.26.31] - 2026-04-27

Patch — response capture filter regression (third pass) + README cleanup + mempalace audit backlog.

### Fixed

- **Response capture leaked TUI noise from still-thinking sessions.**
  Operator-reported: *"Last response is now only garbage and not any
  text from the last set of responses."* v5.26.23 was too charitable
  — `hasWord3` kept any line with 3 consecutive letters, anchored
  footer matching missed lines with leading TUI glyphs, and several
  shapes (single-spinner-counter, bare-digit, embedded status timer,
  labeled border) had no rule. Added `isLabeledBorder`,
  `hasEmbeddedStatusTimer`, `isSpinnerCounter`, `isPureDigitLine`;
  broadened the noise-pattern check to apply unconditionally. Trade-
  off: rare false-positives on real prose that mentions footer
  phrases (e.g. *"the doc says press esc to interrupt"*) accepted
  for the much larger correct-positive volume on TUI noise.

### Changed

- **README.md** — removed `### Highlights since v4.0.0` and `### What's
  new since v3.0` sections per operator request. Latest-release
  summary stays. Features that lived only in those sections
  (cross-cluster observer federation, slim distroless agent
  containers, `datawatch-app` mobile companion) folded into "What it
  does" so high-level coverage is preserved.

### Added

- **Mempalace alignment audit** task in
  `docs/plans/2026-04-27-v6-prep-backlog.md`. Operator clarification
  that the spatial-memory comparison is against mempalace; audit
  produces a plan doc + quick-win shortlist for v6.1.

## [5.26.30] - 2026-04-27

Patch — unified Profile dropdown in New PRD modal (phase 1 of operator's PRD-flow rework).

### Changed

- **New PRD modal collapses three semi-independent fields into one
  "Profile" dropdown.** First option is `__dir__` (project directory
  / local checkout); subsequent options are configured project
  profiles. Selecting a profile hides the directory input AND the
  backend/effort/model row (profile's `image_pair` carries the
  worker LLM) and reveals a required Cluster dropdown. Selecting
  `__dir__` shows the path input + backend row, hides the cluster
  dropdown. Submit-time validation matches the UI ("pick a cluster"
  / "enter a directory"). REST contract unchanged.

## [5.26.29] - 2026-04-27

Patch — pre-release security scan automation.

### Added

- **`.github/workflows/security-scan.yaml`** wires the AGENT.md
  pre-release security scan rule into CI. `govulncheck` blocks the
  workflow on any reachable vuln; `gosec` runs advisory
  (`continue-on-error: true`) using the existing `.gosec-exclude`
  rule list — output surfaces in workflow logs, the operator
  reviews manually pre-tag. Triggers on tag push, PRs to main, and
  workflow_dispatch. Closes a follow-up flagged in v5.26.25's
  gh-actions audit.

## [5.26.28] - 2026-04-27

Patch — smoke memory check was silently broken since it was added.

### Fixed

- **`scripts/release-smoke.sh` memory recall section** had two bugs
  that made it always SKIP. (1) Wrong endpoint
  (`/api/memory/recall` — actual route is `/api/memory/search`).
  (2) Python `d.get("results",[])` called on a top-level JSON list
  raised `AttributeError`, swallowed by `2>/dev/null`, falling
  through to the SKIP branch. Both fixed; smoke now correctly
  exercises memory recall when the subsystem is enabled. Operator-
  reported: *"Memory should be working"*.

## [5.26.27] - 2026-04-27

Patch — startup orphan-workspace reaper for crash-recovery.

### Added

- **`Manager.ReapOrphanWorkspaces()`** sweeps direct children of
  `<data_dir>/workspaces/` that no live session's `ProjectDir`
  points to. Called once at daemon startup alongside
  `ResumeMonitors` / `StartReconciler`. Closes the crash-recovery
  gap left by v5.26.26 (which only reaped on `Manager.Delete`).

### Tests

- 2 new tests in `internal/session/ephemeral_workspace_test.go`:
  removes-unreferenced-dirs, no-root-dir-is-harmless. 460 unit
  tests passing total (was 458).

## [5.26.26] - 2026-04-27

Patch — per-session workspace reaper for daemon-cloned project_profile workspaces.

### Added

- **`Session.EphemeralWorkspace`** (and matching `StartOptions`
  field). Persisted; set to `true` by `handleStartSession` only when
  it actually creates a clone target under `<data_dir>/workspaces/`.
  Operator-supplied `project_dir` keeps the field `false`.
- **`Manager.Delete` now reaps ephemeral workspaces.** When
  `EphemeralWorkspace=true` AND the path resolves under
  `<data_dir>/workspaces/` (defense-in-depth path guard), the
  workspace tree is removed. Runs regardless of `deleteData` since
  the workspace is ephemeral by definition. Operator-supplied
  project_dirs are never touched.

### Tests

- 3 new tests in `internal/session/ephemeral_workspace_test.go`:
  reap-on-delete, leave-operator-dir-alone, refuse-out-of-bounds-reap.

## [5.26.25] - 2026-04-27

Patch — gh-actions audit: silent generator failures, missing parent-full publish, retag race window.

### Fixed

- **eBPF drift workflow no longer swallows generator failures**.
  `go generate ./... || true` masked clang errors — the artifacts
  simply weren't regenerated, so the diff check passed against
  unchanged files even when `netprobe.bpf.c` had a syntax error.
  Removed the `|| true`; the workflow now fails loudly on generator
  failure, which is what it was meant to do all along.

### Added

- **`parent-full` image now publishes to GHCR**. The Dockerfile has
  existed for a while but was missing from `containers.yaml`'s build
  matrix; it now ships alongside the other stage-2 agent images.
- **Per-tag concurrency guard on `containers.yaml`**. Two tag pushes
  in close succession would race the GHCR upload path — `concurrency:
  containers-${{ github.ref }}` with `cancel-in-progress: false`
  serializes them so a second run waits for the first.

## [5.26.24] - 2026-04-27

Patch — BL113 token broker integration for daemon-side `project_profile` clone.

### Added

- **Daemon-side clone now uses per-spawn ephemeral tokens** when the
  BL113 broker is wired. v5.26.21 introduced `project_profile`-driven
  clone in `handleStartSession`; v5.26.22 abstracted the credential
  sourcing for k8s. Both relied on the long-lived
  `DATAWATCH_GIT_TOKEN` env. v5.26.24 routes that path through the
  same broker `agents.Manager` already uses, so HTTPS clone URLs get
  a 5-minute scoped token that's revoked immediately after the clone
  finishes. SSH URLs still use the mounted key (no rewrite). Local
  dev with no broker falls back to the env (or git's local credential
  helper) — unchanged.

### Changed

- `server.HTTPServer` gained `SetGitTokenMinter(GitTokenMinter)`
  delegating to the underlying api `Server`. The interface mirrors
  `agents.GitTokenMinter` (`MintForWorker` / `RevokeForWorker`) so
  the existing `brokerAdapter` in `cmd/datawatch/main.go` satisfies
  both. Wiring captures the adapter into a `pendingGitMinter`
  variable during early auth setup and applies it after
  `server.New(...)` completes.

## [5.26.23] - 2026-04-27

Patch — response capture filter regression: prose-in-borders preserved, multi-spinner pollution dropped.

### Fixed

- **Response viewer showed only animated noise, no real text**
  (operator-reported: *"Last response now has only the animated stuff
  and not real response data"*). v5.26.15's filter caught box-
  drawing characters anywhere in the line via
  `strings.Contains(s, "│  ")` etc., which killed real prose framed
  by claude / opencode TUI borders (`│  Here is your answer  │`
  → dropped). Filter rewritten:
  - **Prose-detection gate** (`hasWord3`) — any line with a 3+-letter
    word passes through regardless of decoration around it. Real
    answers framed in box borders survive.
  - **Pure-decoration detection** (`isPureBoxDrawing`) — lines that
    are 100% box-drawing chars + whitespace still get dropped, so
    the lone `╭────────╮` border lines disappear.
  - **Pure status-timer detection** (`isPureStatusTimer`) runs
    BEFORE the prose gate so `(7s · timeout 1m)` (which has the
    word "timeout") still gets dropped.
  - **Anchored footer matching** — bare `esc to interrupt` at line
    start drops; prose like *"the doc says press esc to interrupt"*
    is kept. (Pre-fix: `strings.Contains` matched anywhere.)
  - **Multi-spinner pollution** — new heuristic drops lines with
    ≥2 spinner glyphs (operator-reported example:
    `Ex50 ✶            1 ✽            2 ✢`).
  3 new unit tests (`PreservesProseInBoxBorder`,
  `DropsMultiSpinnerLine`, `FooterAnchoringIsPositional`) plus the
  existing 6 still pass.

### Changed

- SW `CACHE_NAME` bumped → `datawatch-v5-26-23`.
- README.md marquee → v5.26.23.

## [5.26.22] - 2026-04-27

Patch — git credentials abstracted for k8s + SSH-key Secret support in the Helm chart.

### Added

- **`DATAWATCH_GIT_TOKEN` env auto-rewrites HTTPS git URLs**
  (operator-asked: "Can the credentials for the recent changes be
  abstracted and documented to work in k8s?"). When the env is set
  (Helm chart already projects it from `gitToken.existingSecret`),
  daemon-side clone of `project_profile`-based PRDs rewrites
  `https://...` URLs to `https://x-access-token:<token>@...` before
  calling git. SSH URLs (`git@host:...`) are NOT rewritten — those
  use the new `ssh.existingSecret` mount below.
- **`ssh.existingSecret` chart value**. Mounts a Secret containing
  `id_ed25519` and `known_hosts` keys at `/root/.ssh/` inside the
  daemon Pod (`defaultMode: 0400`). Pairs with profile URLs that use
  the `git@host:owner/repo.git` SSH form. Operator workflow:
  ```
  kubectl create secret generic datawatch-ssh \
    --from-file=id_ed25519=$HOME/.ssh/id_ed25519 \
    --from-file=known_hosts=...
  helm upgrade dw ./charts/datawatch --reuse-values \
    --set ssh.existingSecret=datawatch-ssh
  ```
- **`internal/server/git_auth.go`** — `injectGitToken(rawURL, token)`
  + `redactGitToken(blob, originalURL)`. Token-injection is
  idempotent (URLs that already carry userinfo are not overwritten).
  Redaction masks both injected and pre-existing embedded tokens
  from clone-error output. 9 new unit tests cover the matrix
  (HTTPS public, HTTPS with auth, SSH passthrough, empty token, bad
  URL, GitLab-style, and the two redact paths).
- **`docs/howto/setup-and-install.md` § Git credentials in k8s** —
  operator-facing comparison of the three auth patterns: HTTPS+PAT
  (simplest), SSH key in Secret (private repos / deploy keys), and
  the future BL113 token broker (multi-tenant; v5.26.23+ scope).
  Plus rationale on which to pick when, plus the redaction guarantee
  on clone-error logs.

### Changed

- Helm chart `version` bumped 0.22.0 → **0.23.0** (chart change:
  `ssh.existingSecret`). `appVersion` bumped 5.26.5 → **5.26.22**.
- SW `CACHE_NAME` bumped → `datawatch-v5-26-22`.
- README.md marquee → v5.26.22.

## [5.26.21] - 2026-04-27

Patch — daemon-side clone for `project_profile` + local session.

### Added

- **`POST /api/sessions/start` accepts `project_profile`** as a clone
  hint when `project_dir` is not provided. Closes the v5.26.19
  follow-up: when an autonomous PRD has `project_profile` set
  without `cluster_profile`, the autonomous executor passes it
  through to the local-session spawn; the handler now resolves the
  named profile via `s.projectStore.Get`, shells out to
  `git clone --depth 1` (with `--branch` when set) into a per-spawn
  workspace `<data_dir>/workspaces/<profile>-<8hex>/`, and uses
  that path as the worker's `project_dir`. Errors short-circuit
  with HTTP 400 (profile not found / no git URL) or 502 (git clone
  failed) so the autonomous executor sees the failure on the spawn
  round-trip.
- Cloning uses whatever auth the daemon's user has locally (SSH
  agent, git credential helper, or embedded HTTPS token in the
  profile URL). F10 BL113 token-broker integration is a v5.26.22
  follow-up.

### Changed

- SW `CACHE_NAME` bumped → `datawatch-v5-26-21`.
- README.md marquee → v5.26.21.

## [5.26.20] - 2026-04-27

Patch — PRD profile attachment via PWA New PRD modal + REST PUT /profiles.

### Added

- **PWA New PRD modal — profile dropdowns** (operator-asked v5.26.19
  follow-up). Modal pre-fetches `/api/profiles/projects` +
  `/api/profiles/clusters`, renders two dropdowns:
  - **Project profile** — `(none — use project_dir below)` plus all
    F10 project profile names.
  - **Cluster profile** — `(local — tmux session on this host)` plus
    all F10 cluster profile names.
  Operator picks either profile-based work source or an explicit
  project_dir; client-side validation rejects "no work source"
  before posting (matching daemon-side rule).
- **`PUT /api/autonomous/prds/{id}/profiles`** — post-create profile
  attach / detach for PRDs that need profile changes after they were
  initially created via project_dir. Body shape:
  `{ project_profile, cluster_profile }`. Empty values clear the
  field. Validates names + refuses while running (re-uses
  `Manager.SetPRDProfiles`).
- **Smoke `§7c`** extended with the PUT round-trip — asserts cleared
  values come back empty in the response.

### Changed

- SW `CACHE_NAME` bumped → `datawatch-v5-26-20`.
- README.md marquee → v5.26.20.

## [5.26.19] - 2026-04-27

Patch — PRD project_profile + cluster_profile attachment.

### Added

- **PRD `project_profile` and `cluster_profile` fields**
  (operator-reported: "Prd should be based on directory or profile,
  should be able to check out repo and do work" + "Prd should also
  support using cluster profiles"). New fields land on the
  `autonomous.PRD` model + storage round-trip + REST `POST
  /api/autonomous/prds` body. At-create validation:
  - At least one of `project_dir` or `project_profile` is required
    (rejects requests with neither).
  - `project_profile` and `cluster_profile` are validated against
    F10 stores via the new `autonomouspkg.ProfileResolver`
    interface, wired in `cmd/datawatch/main.go` to a
    `*profile.ProjectStore` + `*profile.ClusterStore` adapter.
- **`autonomouspkg.Manager.SetPRDProfiles`** — operator-callable
  attach/detach for an existing non-running PRD. Validates names,
  appends a `set_profiles` Decision audit row, refuses while the
  PRD is running. 6 new unit tests (`profiles_test.go`) cover the
  validation + running-refusal + no-resolver-fallback +
  decisions-row matrix.
- **`autonomouspkg.SpawnRequest.ProjectProfile` + `.ClusterProfile`**
  threaded from the executor through to `cmd/datawatch/main.go`'s
  `autonomousSpawn` callback.
- **Cluster-profile dispatch.** When `cluster_profile` is set, the
  spawn callback POSTs to `/api/agents` (F10 cluster spawn) instead
  of `/api/sessions/start` (local tmux). Returned agent ID is
  prefixed with `agent:` in the SpawnResult so downstream readers
  can distinguish.
- **Server-side `/api/sessions/start` now accepts `project_profile`**
  as an optional clone hint (passed through from autonomous spawn);
  daemon-side clone-handling lands as v5.26.20 follow-up. For now
  operators using `project_profile` alone (no cluster_profile)
  should pre-clone OR pair with a cluster profile so the agent
  worker handles the clone.
- **`release-smoke.sh §7c`** — pre-creates a project profile,
  asserts unknown-profile-name rejection (HTTP 400 with "project
  profile %q not found"), asserts known-profile-name persists on
  the PRD record. Profile cleanup added to `cleanup_all` for both
  `project-profile` and `cluster-profile` kinds.

### Changed

- `AutonomousAPI` interface gained `SetPRDProfiles(prdID,
  projectProfile, clusterProfile) error`.
- SW `CACHE_NAME` bumped → `datawatch-v5-26-19`.
- README.md marquee → v5.26.19.

## [5.26.18] - 2026-04-27

Patch — loopback URL sweep finished + smoke kills race-condition orphans.

### Fixed

- **Smoke leftover `autonomous:*` orphan sessions** (operator-reported
  again: "Another orphaned prd test session"). Pre-v5.26.18 the
  smoke leaked ~1 orphan per run because the executor goroutine had
  spawn HTTP calls already in flight when cancel propagated; the
  v5.26.13 SessionIDsForPRD walk caught most but not all. v5.26.18:
  smoke captures a baseline of `autonomous:*` running session IDs
  before any work; cleanup_all in the EXIT trap diffs the live list
  against the baseline and kills any new orphans. Real
  operator-initiated autonomous runs that pre-existed are NOT
  touched. Should bring per-run orphan count to 0.

### Changed

- **Remaining 8 hardcoded `http://127.0.0.1:port` sites** in
  `cmd/datawatch/main.go` swept to `loopbackBaseURL(cfg)`:
  - channel.js subprocess `DATAWATCH_API_URL` env (2 sites)
  - `/api/test/message` invoke (1 site)
  - `/api/config` PUT/GET in CLI helpers (4 sites)
  - `/api/stats` + `/api/alerts` in CLI commands (2 sites)
  Combined with the 6 sites done in v5.26.17, all daemon-internal
  loopback HTTP calls now respect `cfg.Server.Host`.

### Changed

- SW `CACHE_NAME` bumped → `datawatch-v5-26-18`.
- README.md marquee → v5.26.18.

## [5.26.17] - 2026-04-27

Patch — loopback URL respects bind config + startup health probe.

### Fixed

- **127.0.0.1 hardcoded everywhere**, breaking when the daemon binds
  to a specific non-loopback interface (operator-reported: "Loopback
  may not exist if someone changes the default interfaces, validate"
  — when `cfg.Server.Host = 192.168.1.5`, the daemon listens at
  192.x and DOESN'T accept on 127.0.0.1, so all 14 internal loopback
  HTTP calls — autonomous decompose, voice transcribe, channel
  bridge, orchestrator guardrails, etc. — silently fail).
  - New `loopbackBaseURL(cfg)` helper resolves to the right base URL:
    `http://127.0.0.1:port` when bound to 0.0.0.0 / "" / IPv6 ::,
    `http://[::1]:port` when bound to ::, the actual bound IP
    otherwise (with IPv6 brackets where needed).
  - Replaced 6 highest-priority hardcoded sites in
    `cmd/datawatch/main.go`'s autonomous + orchestrator callbacks.
  - 7 new tests in `loopback_url_test.go` cover the resolution
    matrix (default empty, bind-all, specific IPv4, ::, specific
    IPv6, default port, nil cfg).

### Added

- **Startup loopback health probe.** `validateLoopback(cfg)` GETs
  `/api/health` on the resolved URL 2s after the HTTP listener is
  up. On failure, prints a 3-line WARNING explaining what's broken
  and how to fix (set `server.host = 0.0.0.0` or fix the bind so
  loopback works). Daemon continues — operators get an explicit
  signal rather than silent autonomous/orchestrator failure.

### Changed

- SW `CACHE_NAME` bumped → `datawatch-v5-26-17`.
- README.md marquee → v5.26.17.

## [5.26.16] - 2026-04-27

Patch — Settings reorder + LLM backend dropdowns with paired model dropdowns for autonomous + orchestrator config + executor goroutine cancellation on PRD cancel/delete.

### Fixed

- **Orphan `autonomous:*` sessions accumulating after PRD cancel /
  delete** (operator-reported: "I see lots of sessions all active
  but no prd"). v5.26.13 added session-kill on PRD-delete REST call
  but it walked `SessionIDsForPRD()` at the moment of delete; the
  executor goroutine kept spawning new sessions afterwards because
  it ran with `context.Background()` (no cancellation tied to the
  PRD lifecycle). v5.26.16: API now tracks
  `runCancels map[string]context.CancelFunc` per PRD; `Run` stores
  the cancel func, `Cancel` + `DeletePRD` invoke it before the
  store mutation. The executor's existing `ctx.Err()` check between
  tasks now fires on cancellation and the loop bails. Reduced
  orphan-leak rate ~8x in smoke (8 → 1) — the residual 1 is a race
  where a spawn HTTP call was already in flight when cancel
  propagated; the v5.26.13 SessionIDsForPRD walk catches most of
  these on hard-delete.

### Added

- **Decomposition / Verification / Guardrail backend fields are now
  dropdowns** of enabled+available LLM backends (shell excluded) with
  paired Model dropdowns that refresh when the backend selection
  changes. Same UX as the New PRD modal. Operator-reported.
- **`autonomous.decomposition_model`,
  `autonomous.verification_model`, `orchestrator.guardrail_model`**
  config fields. YAML + REST PUT /api/config + /api/autonomous/config
  all round-trip them. `cmd/datawatch/main.go`'s decomposeFn,
  autonomousVerify, autonomousGuardrail and orchestrator-guardrail
  callbacks now thread the model through to /api/ask payloads.
- **PWA renderer** in `loadGeneralConfig` learned `type: 'llm_backend'`
  + `type: 'llm_model'` field types. `llm_backend` filters
  `NON_LLM_BACKENDS` (shell) and falls back to "(not configured)" if
  the saved value isn't in the enabled set. `llm_model` reuses the
  existing `refreshLLMModelField` + `ensureLLMModelLists` helpers.

### Changed

- **PRD-DAG orchestrator section moved above Plugin framework** in
  Settings → General. Operator-reported: orchestrator is the
  workflow-level concern operators reach for next after Autonomous;
  Plugin framework is daemon extensibility set up rarely.
- SW `CACHE_NAME` bumped → `datawatch-v5-26-16`.
- README.md marquee → v5.26.16.

## [5.26.15] - 2026-04-27

Patch — response capture filters out animation spinners + TUI status decoration.

### Added

- **`stripResponseNoise`** filter applied to `Manager.CaptureResponse`.
  Operator-reported: response capture should filter out animation
  spinning icons and things not text or useful to read. The fallback
  path that returns the last 30 tmux lines now drops:
  - Single-glyph spinner / progress markers (`●`, `✶`, `✻`, `⠋…⠏`,
    `❯`, etc.).
  - Status timer fragments like `(7s · timeout 1m)` and `(5s)`.
  - claude-code TUI footer hints (`esc to interrupt`, `shift+tab to
    cycle`, `↑↓ to navigate`, etc.).
  - Box-drawing decoration (╭ ╰ │ ─).
  Bullet lists (`* item`) are preserved — only PURE-spinner lines
  (where the line is exactly `*`) get filtered. ANSI is stripped
  before noise-matching so colour codes don't shield the markers.
  Three-or-more consecutive blank lines collapse to one.
  6 new unit tests in `response_filter_test.go` cover the spinner /
  timer / footer / ANSI / collapse / empty cases.

### Changed

- SW `CACHE_NAME` bumped → `datawatch-v5-26-15`.
- README.md marquee → v5.26.15.

## [5.26.14] - 2026-04-27

Patch — scroll mode no longer leaks live updates from the running session (third iteration).

### Fixed

- **Scroll mode still getting live updates from running session**
  (operator-reported, third iteration after v5.26.9 + v5.26.10).
  Root cause: claude-style TUIs include a status timer in the
  captured pane content (e.g. `(7s · timeout 1m)` ticking every
  second). v5.26.10's content-aware dedupe correctly observed the
  frame "changed" each tick and fired a redraw, defeating the
  scroll-mode preservation. v5.26.14: redraws now skip while
  `state._scrollMode === true` UNLESS
  `state._scrollPendingRefresh` is set. The flag is set in three
  places:
  - `toggleScrollMode` on entry (so the operator sees the
    scroll-back position immediately).
  - `scrollPage('up' | 'down')` before each PageUp / PageDown send
    (so each scroll click triggers exactly one redraw to surface
    the new tmux scroll position).
  - Cleared (set false) once the redraw consumes it, so the next
    idle tick skips silently.
  `exitScrollMode` also clears `state._lastPaneFrame` so the first
  post-exit pane_capture is forced through (otherwise it would
  match the cached scroll view and skip-as-identical).

### Changed

- SW `CACHE_NAME` bumped → `datawatch-v5-26-14`.
- README.md marquee → v5.26.14.

## [5.26.13] - 2026-04-27

Patch — shell backend hidden from autonomous LLM dropdowns + cancel/delete now kills the spawned worker sessions + smoke uses an LLM worker.

### Fixed

- **Cancelling or deleting a running PRD didn't kill the spawned
  worker sessions.** Operator-reported: leftover `autonomous:*`
  tmux sessions accumulating after PRD runs, eventually hitting
  `session.max_sessions` cap and blocking new spawns with
  `500 max sessions`. v5.26.13: the REST DELETE handler now walks
  `SessionIDsForPRD()` BEFORE the cancel/delete (since hard-delete
  cascades and we lose the pointers afterwards) and best-effort
  calls `s.manager.Kill(sessionID)` for each. Response payload
  includes `killed_sessions: N` so the operator sees how many were
  reaped. Same path for both DELETE (cancel) and DELETE?hard=true.

### Changed

- **`shell` backend filtered out of `renderBackendSelect`.**
  Operator-reported: shell isn't an LLM, so it shouldn't appear in
  the per-PRD / per-task LLM-override dropdowns. New
  `NON_LLM_BACKENDS` set excludes it; an existing PRD that already
  has `backend: shell` still renders the value via the
  fall-through "(not configured)" option so the assignment doesn't
  drop silently.
- **`release-smoke.sh §7b`** (autonomous run lifecycle) switched off
  `backend: shell` to align with the v5.26.13 exclusion rule. Now
  picks the first available LLM in priority order (`ollama` →
  `openwebui` → `opencode` → `claude-code`); skips if none
  available.

### Changed

- SW `CACHE_NAME` bumped → `datawatch-v5-26-13`.
- README.md marquee → v5.26.13.

## [5.26.12] - 2026-04-27

Patch — children load eagerly with the rest of the PRD list.

### Changed

- **Children no longer lazy-loaded.** Operator-reported: children
  should load with everything else, not behind a Load button. The
  GET /api/autonomous/prds response already contains every PRD
  (parents and children) flat with `parent_prd_id` pointers.
  loadPRDPanel now builds an O(N) parent_id → [children] index
  (`state._prdChildIndex`) once and renderPRDRow reads from it
  inline. The `<details>` block opens with `<details open>` so the
  child rows are visible without an extra click. `loadPRDChildren`
  + the Load button + the per-row N+1 GET are all gone. Same row
  shape as v5.26.6 (clickable child IDs scroll-to-row, stories+tasks
  count, verdict-count badges).

### Notes

- **claude trust-dir auto-accept** (operator question): already
  handled. `session.claude.skip_permissions: true` in the operator's
  config (default since v3.0) makes the launcher pass
  `--dangerously-skip-permissions` to claude-code, which bypasses
  the trust-folder dialog and every permission prompt for the
  duration of the session. Autonomous PRDs spawned with backend
  `claude-code` inherit the same launch path so no separate
  auto-accept is needed. If `skip_permissions: false`, the
  trust-folder prompt is detected by the existing promptPatterns
  list (manager.go:120) and surfaces as `waiting_input` — operator
  needs to reply manually for that mode.

### Changed

- SW `CACHE_NAME` bumped → `datawatch-v5-26-12`.
- README.md marquee → v5.26.12.

## [5.26.11] - 2026-04-27

Patch — autonomous spawn effort-enum translation (post-decompose run was failing every PRD with default effort=low) + smoke now exercises PRD full lifecycle.

### Fixed

- **Autonomous PRD run failed every task with `invalid effort: must be
  one of quick, normal, thorough`.** The autonomous Effort enum
  defines `low/medium/high/max` plus `quick/normal/thorough` aliases;
  session.EffortLevels only accepts `quick/normal/thorough`. The
  PWA + decomposer happily accepted `low` (the default) but the
  spawn POST to `/api/sessions/start` rejected it, sending every
  task straight to `TaskFailed` without ever spawning a session.
  Fixed by adding `mapEffortToSession` translation in
  `cmd/datawatch/main.go`'s spawn callback:
  - `low → quick`, `medium → normal`, `high|max → thorough`
  - existing session-side aliases (`quick/normal/thorough`) pass
    through unchanged
  - empty effort stays empty (daemon falls back to
    `session.default_effort`)

### Added

- **`release-smoke.sh §7b — Autonomous PRD full lifecycle.**
  Decompose → approve → run → wait 8s → confirm executor reached
  spawn (no `pre_spawn` failures with `invalid effort`). Catches
  this class of regression going forward. Initial run: 4 new PASS;
  total 29 PASS / 0 FAIL / 2 SKIP.

### Changed

- SW `CACHE_NAME` bumped → `datawatch-v5-26-11`.
- README.md marquee → v5.26.11.

## [5.26.10] - 2026-04-27

Patch — scroll mode redraw fix (regression from v5.26.9) + smoke covers all three worker backends.

### Fixed

- **Scroll mode wasn't scrolling.** v5.26.9 added
  `if (state._scrollMode) break;` to skip pane_capture redraws while
  in scroll mode, but tmux copy-mode needs those redraws to surface
  scroll-back lines as the operator pages through. v5.26.10 replaces
  the unconditional skip with a content-aware dedupe — skip only
  when the captured frame is byte-identical to the last one written.
  Idle tmux + redrawing status bar = no flash; scrolling = redraw =
  scroll-back updates render. `state._lastPaneFrame` cleared in
  `destroyXterm` so cross-session state doesn't leak.

### Added

- **`release-smoke.sh` exercises every supported worker backend.**
  Operator-reported: smoke must validate PRD CRUD with claude-code,
  opencode, AND ollama as the worker backend, not just claude-code.
  Section 6 now iterates every backend whose `enabled && available`
  is true on the running daemon and runs create + record-shape
  check + `/children` + `set_llm` round-trip + hard-delete per
  backend. Initial run: 25 PASS / 0 FAIL / 2 SKIP.

### Changed

- SW `CACHE_NAME` bumped → `datawatch-v5-26-10`.
- README.md marquee → v5.26.10.

## [5.26.9] - 2026-04-27

Patch — autonomous loopback fix (broken since v3.10.0) + per-release functional smoke + scroll-mode redraw guard.

### Fixed

- **Autonomous decompose / verify / guardrail loopback** has been broken
  since v3.10.0. Three layers:
  1. HTTP→HTTPS redirect chain ate the loopback POST (x509 from
     self-signed cert). v5.18.0 bypassed this for `/api/channel/*`
     but not the autonomous paths. Bypass extended to
     `/api/ask`, `/api/sessions`, `/api/sessions/`,
     `/api/orchestrator/`, `/api/autonomous/`.
  2. Field-name mismatch — autonomous senders used `"prompt"` but
     `/api/ask` decodes `"question"`. All four sites in `cmd/datawatch/main.go`
     fixed.
  3. Backend incompatibility — `/api/ask` only supports ollama +
     openwebui as headless targets. The decomposer inheriting
     `claude-code` from the PRD's worker backend got 400. New
     `askCompatible(b)` predicate; resolution order:
     `amgrCfg.DecompositionBackend` → `req.Backend` (only if
     ask-compatible) → `"ollama"`. Verifier + guardrails fall back
     to ollama identically.
  4. First-token timeout 60s → 300s in `askOllama` + `askOpenWebUI`.
- **`pane_capture` redraw clobbered scroll-back position** when the
  operator was in tmux scroll mode (`Ctrl-b [`). v5.24.0's xterm
  buffer-position check missed this because xterm was "at-bottom" of
  the captured frame even though tmux was showing scroll-back.
  Handler now checks `state._scrollMode` first and skips the redraw
  entirely.
- **`GET /api/channel/history`** returns `messages: []` (not `null`)
  for unknown / empty sessions. PWA tolerated both; smoke didn't.

### Added

- **`scripts/release-smoke.sh`** — runs against a live daemon and
  exercises 11 operator-facing surfaces (health, backends, stats,
  diagnose, channel-history shape, autonomous CRUD, autonomous
  decompose loopback, observer peer push, memory recall, voice
  availability, orchestrator graph CRUD). Cleanup via `EXIT` trap so
  every PRD / peer / graph the smoke creates is removed on success
  OR failure. Initial run: 14 PASS / 0 FAIL / 2 SKIP.
- **AGENT.md release-discipline rule:** every release (patch + minor +
  major) runs `scripts/release-smoke.sh` before tagging.
  Saved to memory as `feedback_per_release_smoke.md`.
- **`redirect_bypass_test.go`** new cases for the 5 added loopback
  bypass paths + a deny-overshoot for `/api/asksomething` (exact
  match prevents `/api/ask` from accidentally matching a future
  endpoint).

### Changed

- SW `CACHE_NAME` bumped → `datawatch-v5-26-9`.
- README.md marquee → v5.26.9.

## [5.26.8] - 2026-04-27

Patch — Autonomous tab UX sweep: cascade delete, dynamic model dropdown across every LLM modal, tab hide-when-disabled, mic/CSV-expand affordances, auto badge removed.

### Fixed

- **PRD delete UX** (operator-reported error). `confirmPRDDelete`
  now pre-fetches `/children` so the confirm message names the
  cascade count ("…and 3 child PRD(s) under it"). Failure toast
  strips the leading "Error:" prefix and surfaces the daemon's
  message verbatim.
- **`Manager.DeletePRD` cascade-aware running guard.** Pre-v5.26.8
  the top-level PRD's status was checked but a running descendant
  would be silently deleted, leaving its executor goroutine
  writing to a now-deleted PRD. v5.26.8 walks descendants and
  refuses with a specific error pointing at the running child.
  Two new tests cover the running-descendant + cancelled-descendant
  matrix.

### Added

- **Dynamic model dropdown across every LLM modal.** `openPRDCreateModal`
  already had this since v5.27.0; `openPRDEditTaskModal` and
  `openPRDSetLLMModal` now share the same `refreshLLMModelField` +
  `ensureLLMModelLists` helpers. Selecting a backend repopulates the
  Model dropdown from `/api/ollama/models` + `/api/openwebui/models`;
  field hides when the backend has no model list. Operator-pinned
  custom models survive backend toggles via a "(custom) <name>"
  option.
- **Mic-button factory** (`micButtonHTML(targetId)`) gated on
  `state._whisperEnabled` (cached on boot from `/api/config`).
  Wired into the PRD-create spec textarea, edit-task spec textarea,
  edit-PRD spec textarea, and the new CSV-edit modal textarea.
  Operator-reported: large editing dialogs should have mic input.
- **CSV expand-to-modal** (`csvExpandButtonHTML(targetId, label)`).
  CSV-list config inputs (`per_task_guardrails`,
  `per_story_guardrails`) get a `[✎]` button next to them that
  opens a textarea-based modal (one item per line) with a mic
  button. Save normalizes back to comma-separated. Field metadata
  gains a `csv: true` flag — easy to extend to other lists.
- **Hide Autonomous tab when not enabled.** `index.html` button
  starts `display:none`; JS unhides only when
  `/api/autonomous/config` returns `enabled:true`. Operator-reported:
  tabs for disabled subsystems shouldn't render at all.

### Changed

- Autonomous-tab `● auto` indicator removed entirely. Header status
  dot is the single source of WS-state truth.
- SW `CACHE_NAME` bumped → `datawatch-v5-26-8`.
- README.md marquee → v5.26.8.

## [5.26.7] - 2026-04-27

Patch — Autonomous-tab Refresh button removed entirely (auto-refresh is reliable; button was clutter).

### Changed

- **Autonomous-tab manual `↻ Refresh` button removed.** v5.26.6 hid it
  when WS was connected; v5.26.7 drops it altogether. The
  `prd_update` WS broadcast already covers every PRD mutation
  (create / decompose / approve / reject / run / cancel / edit /
  delete / set-llm / set-task-llm); the header status dot already
  surfaces WS-disconnect; the panel's `auto`/`offline` indicator
  flips color + label based on WS state. Operator-reported v5.26.6
  cleanup.
- SW `CACHE_NAME` bumped → `datawatch-v5-26-7` so installed PWAs
  pick up the cleanup on next activate.
- README.md marquee → v5.26.7.

## [5.26.6] - 2026-04-27

Patch — Autonomous-tab final polish + BL173 cluster validation + Helm chart `v`-prefix fix.

### Fixed

- **PWA Autonomous tab buttons silently no-op'd + edits didn't auto-refresh** (operator-reported). Root cause: stale cached app.js in installed PWAs (the v5.26.3 escHtml fix and v5.24.0 prd_update WS handler are both in current source but PWAs that hit a transient offline window during v5.7→v5.26 ended up locked on `datawatch-v5-6-1`). SW `CACHE_NAME` bumped to `datawatch-v5-26-6` to force every install to drop the stale cache on next activate.
- **Helm chart `v`-prefix tag mismatch.** CI publishes GHCR tags without the `v` prefix (`5.26.5`, not `v5.26.5`); operators pasting release tags with `v` got ImagePullBackOff. `templates/_helpers.tpl` now `trimPrefix "v"` from `image.tag`/`appVersion`. Either form works.

### Added

- **Autonomous tab WS-aware Refresh button.** Manual `↻ Refresh` is now hidden when WS is connected (replaced with a small green `● auto` badge); reappears as a fallback when the status dot goes red. Wired through `updateStatusDot`.
- **BL202 verdict drill-down panel.** Verdict badges on stories + tasks are now clickable → inline panel showing guardrail / outcome / severity / summary / full issues list. Tooltip kept for desktop hover; click handles mobile / Wear OS where tooltips don't work.
- **BL202 child-PRD navigation.** Children disclosure on parent PRDs now shows clickable child IDs that scroll the panel to that child's row, plus stories+tasks counts and verdict-count badges (green for non-block, red for any block).
- **BL173 live-cluster validation report** (`docs/plans/2026-04-27-bl173-cluster-validation.md`). Helm chart deployed to local testing cluster, peer-registration + push + cross-host aggregation verified end-to-end. Closes BL173 follow-up.

### Changed

- `charts/datawatch/Chart.yaml` `version: 0.21.1 → 0.22.0`, `appVersion: v4.7.1 → 5.26.5` (was 21 releases stale).
- README.md marquee → v5.26.6.

## [5.26.5] - 2026-04-27

Patch — last pre-v6.0 cut. Container hygiene runbook + GHCR cleanup script + datawatch-app#10 catch-up issue. Pure docs + ops tooling.

### Added

- **`docs/container-hygiene.md`** — day-two operator runbook for the
  GHCR image inventory. Covers what CI publishes, the
  `parent-full` + `agent-goose` gaps (closed in v6.0), retag
  one-liners, vulnerability scan commands.
- **`scripts/delete-past-minor-containers.sh`** — counterpart to
  `delete-past-minor-assets.sh` for GHCR. Same retention algorithm:
  every major + latest minor + latest patch on latest minor.
  Requires `read:packages + delete:packages` PAT.
- **datawatch-app#10** filed
  ([link](https://github.com/dmz006/datawatch-app/issues/10)) —
  comprehensive catch-up issue covering every PWA addition since
  v5.3.0 (BL191 + BL203 + BL202 autonomous, channel history,
  Settings sweeps, BL180 federation, eBPF, helm/k8s setup,
  long-press, button-revival).
- `docs/security-review.md` + `docs/container-hygiene.md` wired into
  `/diagrams.html` Subsystems group.

### Changed

- README.md marquee → v5.26.5.

## [5.26.4] - 2026-04-27

Patch — doc-alignment sweep (mcp.md, commands.md, README interface tables, channel API doc, testing-tracker, design + architecture trio with v5 appendix). Pure docs.

### Added

- **`docs/mcp.md`** — full v5.26.3 tool catalog (132 tools); refreshed
  family table with the 6 missing session tools; v5.9 → v5.26
  additions callout.
- **`docs/commands.md`** — comprehensive v5.x CLI reference covering
  every cobra factory added since v3.x: `reload`, `ask`, `assist`,
  `autonomous*`, `observer*`, `orchestrator*`, `agent*`, `profile*`,
  `projects*`, `cooldown`, `device-alias`, `routing-rules`,
  `template`, `plugins*`, `cost*`, `audit`, `diagnose`, `alerts`,
  `health`, `link`, `logs`, `rollback`, `stale`, `export`,
  `config*`, `test [--pr]`, `session schedule*`.
- **`docs/api/channel.md`** — new. Full reference for the channel
  endpoints (`reply`, `notify`, `send`, `ready`) plus the v5.26.1
  `GET /api/channel/history`. Wired into `/diagrams.html` API group.
- **README interface tables** — MCP Tools section refreshed to "132
  tools" with v5.x families; REST API section expanded from a
  13-row table to a 30+-row reference covering sessions / config /
  reload / observer / federation / autonomous CRUD + actions /
  orchestrator / agents / profiles / memory / channel + history /
  voice / devices / proxy / diagnose / health.
- **`docs/testing.md`** — Unit Test Summary refreshed to v5.26.3
  (1395 tests across 58 packages); v2.4.1 section preserved as
  historical reference.
- **`docs/architecture-overview.md` v5.x deltas appendix** — every
  subsystem added since v3.x (BL191/BL203/BL17/BL201/BL180/eBPF/
  BL190/BL202/BL202 learnings/parity sweeps/auto-refresh/docs
  chips/retention rule/configured-only/channel history/howto
  links/helm/k8s setup/long-press/button revival/security review)
  with shipped-in version + howto/API doc reference.
- **`docs/architecture.md`** package list expanded with `autonomous`,
  `orchestrator`, `observer`, `observerpeer`, `plugins`, `agents`,
  `profile`, `channel`, `audit`, `alerts`, `devices`, `cost`,
  `pipeline`, `kg`+`memory`.
- **`docs/design.md` v4 → v5 design evolution appendix** — 9-section
  walkthrough of each major subsystem (autonomous, orchestrator,
  observer, plugins, agents, federation, helm, channel, parity
  backbone) and the design decisions behind them.

### Changed

- README.md marquee → v5.26.4.

## [5.26.3] - 2026-04-27

Patch — long-press server-status refresh + autonomous CRUD button revival + reload CLI test + security review pre-v6.0.

### Added

- **Long-press server-status indicator → force-refresh WS connection.**
  Delegated `pointerdown/up/move/cancel` listener detects 600 ms hold
  on `#statusDot` (header) or `.connection-indicator` (Settings →
  Comms → Servers); closes the live WS, resets reconnect back-off,
  and reconnects. Tooltip + cursor styling on both indicators.
- **Reload CLI test.** `cmd/datawatch/reload_cmd_test.go` (3 tests)
  covers cobra shape + happy-path POST /api/reload + non-2xx error
  propagation. Closes the v5.7.0 audit gap.
- **`docs/security-review.md`.** Documents the gosec + govulncheck
  triage and procedural rules for future patches.

### Fixed

- **Autonomous tab CRUD buttons silently no-op'd.** `renderPRDActions`
  built `<button onclick="${fn}">` where `fn` interpolated
  `JSON.stringify` outputs containing literal `"` chars. Browser
  closed the `onclick` attribute mid-string, never wired the handler.
  v5.22.0 fixed this for the modal-Save button only; v5.26.3 fixes
  the parent helper plus two more sites (`renderTask` Edit ✎ button,
  `loadPRDChildren` Load button, alerts-view session link). All PRD
  buttons (Edit, Delete, Run, Cancel, Approve, Reject, Revise,
  Decompose, LLM, Instantiate) now fire correctly.
- **G702 false-positive cleanup (7 → 0).** All `syscall.Exec` +
  `exec.Command("git", …)` sites annotated `// #nosec G702 -- argv-list
  invocation, not shell` after triage.
- **G402 false-positive cleanup (7 → 0).** All `InsecureSkipVerify=true`
  sites annotated with `// #nosec G402 -- <reason>` after triage. New
  rule documented in `docs/security-review.md`: every `G402` annotation
  must cite pinning or a security-review entry.
- **govulncheck: 0 vulnerabilities.** Bumped `golang.org/x/net`
  v0.50.0 → v0.53.0, `filippo.io/edwards25519` v1.1.0 → v1.2.0, and
  the routine `go mod tidy` cascade (crypto, mod, sync, sys, term,
  text, tools). All transitive — no API changes.

### Changed

- README.md marquee → v5.26.3.

## [5.26.2] - 2026-04-27

Patch — setup howto gains Helm/k8s install (with secrets + NFS storage) and a "ready to code" walk. Pure docs; no code changes outside version bumps.

### Added

- **`docs/howto/setup-and-install.md` Option E — Helm on Kubernetes.**
  Walks the minimum-viable install: pre-create `datawatch-api-token`
  Secret, optional TLS + git-token Secrets, `helm install` with
  `existingSecret` references and `persistence.enabled=true`, verify
  via `kubectl rollout status` + `kubectl port-forward`.
- **NFS-backed persistence** for the K8s install. Walks both CSI
  driver (recommended, dynamic) and nfs-subdir-external-provisioner
  (sidecar), then `helm upgrade --set persistence.storageClass=…`
  with the `ReadWriteMany` callout for future HA.
- **Cross-cluster kubeconfig + Shape-C observer DaemonSet** patterns,
  for ops that want spawned workers in different clusters or
  per-cluster observer rolled into the parent's federation card.
- **"Ready to code" walkthrough** — clone the repo, wire one LLM
  backend (claude-code / Ollama / OpenWebUI), first real
  `datawatch session start --project-dir … --task …` with a
  walk-through of what each PWA tab shows, plus an optional
  `--before-cmd` / `--after-cmd` lint+test gate one-liner.

### Changed

- README.md marquee → v5.26.2.

## [5.26.1] - 2026-04-27

Patch — New PRD modal + PWA Channel-tab history + howto README relative links. (No binary/container build per operator directive: every release until v6.0 is a patch.)

### Added

- **New PRD modal — configured backends only.** `renderBackendSelect`
  filters `b.enabled === false` so disabled/unconfigured backends are
  hidden from the Backend dropdown. Pre-v5.27.0 every registered
  backend showed regardless of credentials/endpoints.
- **New PRD modal — model dropdown follows backend.**
  `openPRDCreateModal` pre-fetches `/api/ollama/models` +
  `/api/openwebui/models` into `state._availableModels` on open;
  `updatePRDNewModelField(backend)` toggles the Model field — populated
  list → `<select>` of model names; no list → field hidden entirely.
- **Per-session channel ring buffer.** `Server.channelHist`
  (`map[string][]channelHistEntry`, cap=100) records every WS-broadcast
  channel message. `recordChannelHistory` is called from
  `BroadcastChannelReply`, `handleChannelReply`, `handleChannelReady`,
  and `handleChannelSend`.
- **`GET /api/channel/history?session_id=...`** returns the ring
  buffer for a session as JSON. Empty session-id → 400; unknown
  session-id → 200 with an empty list (so the PWA can fetch
  unconditionally without surfacing a spurious error).
- **PWA Channel tab seeds from the daemon.** `renderSessionDetail`
  fetches `/api/channel/history` once per session-id, merges with any
  WS-arrived messages (de-dup by `ts|text`), sorts, caps at 50, and
  re-renders. `state._channelHistoryLoaded[sessionId]` prevents
  re-fetch.
- **Howto README links work from the diagrams viewer.**
  `rewriteRelativeMdLinks(path)` runs after marked.js renders the
  prose; relative `.md` anchors get `href` rewritten to
  `#docs/.../foo.md` and a click handler that updates `location.hash`.
  Resolves `..` correctly so cross-howto and `../setup.md` links also
  start working.

### Tests

- `internal/server/channel_history_test.go` (2 tests) — ring-buffer
  cap + handler responses.

### Changed

- README.md marquee → v5.26.1.

## [5.26.0] - 2026-04-26

Minor — Settings card-section docs chips populated.

### Added

- **Every Settings card section now has a "docs" chip** linking to
  the relevant howto (for complex sections — autonomous, orchestrator,
  voice, pipelines, memory, sessions, RTK) or architecture doc (for
  simpler ones — web server, MCP server, plugins, datawatch, auto-
  update). The `settingsSectionHeader(key, title, docsPath)` helper
  already supported the docs arg but no caller passed one. v5.26.0
  threads `sec.docs` through `COMMS_CONFIG_FIELDS` / `LLM_CONFIG_FIELDS`
  / `GENERAL_CONFIG_FIELDS` to the render paths. The existing
  `Show inline doc links` toggle hides all chips when off.

### Changed

- README.md marquee → v5.26.0.

## [5.25.0] - 2026-04-26

Minor — diagrams page restructure + asset retention rule refinement.

### Changed

- **Diagrams page (`/diagrams.html`)** dropped the Plans group
  (operator-internal — gitignored from the embedded viewer since
  v5.3.0); added a top-level How-tos group covering all 13
  walkthroughs; Subsystems gained `mcp.md` + `cursor-mcp.md`; API
  gained `observer.md`, `memory.md`, `sessions.md`, `devices.md`,
  `voice.md`.
- **Asset retention rule refined** in `AGENT.md`. Keep-set: every
  major (X.0.0) + the latest minor (X.Y.0, Y >= 1) + the latest
  patch on the latest minor (X.Y.Z, Z > 0). `scripts/delete-past-
  minor-assets.sh` rewritten to implement the new logic.
- **README.md** marquee → v5.25.0.

## [5.24.0] - 2026-04-26

Minor — autonomous tab WS auto-refresh on PRD changes
(operator-reported v5.22.0 carry-over).

### Added

- **`MsgPRDUpdate` WS message** broadcast on every PRD persist.
  Payload `{prd_id, status?, deleted?}`. PWA Autonomous tab handles
  via a 250 ms-debounced `loadPRDPanel()` so bursty mutations (a Run
  flipping many tasks in a second) reload the panel once at the end.
- **`Manager.SetOnPRDUpdate(PRDUpdateFn)`** + **`Manager.EmitPRDUpdate(id)`**
  indirection. main.go binds the callback to the WS hub. Every
  mutating `*API` method (CreatePRD, Decompose, Run, Cancel, Approve,
  Reject, RequestRevision, EditTaskSpec, InstantiateTemplate,
  SetTaskLLM, SetPRDLLM, DeletePRD, EditPRDFields) emits after save.
  Trailing emit fires inside the Run goroutine when the executor
  walk finishes so terminal states reach the PWA.
- 4 new unit tests (1390 total). README marquee → v5.24.0.

### Fixed

- **Saved-commands dropdown narrowed** (operator-reported). Was
  `max-width: 200px` → too wide for the 480 px PWA card so the
  [📄] [Commands ▾] [arrows] row wrapped. Now `max-width: 130px;
  min-width: 110px` so the row fits on one line on narrow viewports.
- **Scroll-back preserved during pane_capture / raw_output**
  (operator-reported). Pre-fix: every redraw yanked the operator
  back to the bottom of the tmux pane / log viewer when reading
  earlier output. Now: xterm pane_capture skips the redraw when the
  operator is scrolled up (detected via
  `buffer.active.viewportY < baseY`); log-mode raw_output captures
  `wasAtBottom` before appending and only auto-scrolls if it was
  already at the bottom.
- **Tmux send button is now icon-only** (operator-reported). The
  ▶ glyph alone with the existing `title="Send via tmux"` tooltip
  replaces "▶ tmux" in all four render sites. Channel send button
  keeps the "ch" suffix to disambiguate the two sibling buttons.

## [5.23.0] - 2026-04-26

Minor — operator-reported PWA fixes + asset retention rule + AGENT.md additions.

### Fixed

- **Settings → Comms bind interface multi-select** — was rendering
  empty options because the comms-config branch treated
  `state._interfaces` items as strings, but the API returns
  `{addr, label, name}` objects. Now mirrors the general-config
  branch: checkbox-list + auto-protect for the connected interface
  (prevents self-disconnect when "All interfaces" is unchecked).
- **Session-detail channel mode-badge** removed for `channel`/`acp`
  sessions — the Channel/ACP output tab already conveys the mode.
  The `tmux` mode-badge stays since plain tmux mode has no tab system.
- **Response button icon-only** in the saved-commands quick row —
  📄 glyph alone (with title tooltip "View last response") instead
  of "📄 Response". Saves space, fits the v5.22.0 right-justified
  arrow layout.

### Added

- **AGENT.md release-discipline rule: embedded docs must be current
  at binary build time.** Codifies that release binaries must be
  built via `make cross` / `make build` (which depend on `sync-docs`)
  rather than `go build ./cmd/datawatch/` directly.
- **AGENT.md release-discipline rule: asset retention.** Only major
  releases (X.0.0) retain binary attachments + container images
  indefinitely. Past minor + patch releases get assets pruned on the
  next subsequent release. Release notes themselves stay forever.
- **`scripts/delete-past-minor-assets.sh`** — helper script
  implementing the asset retention rule (idempotent; iterates
  every non-major-non-current release and deletes its assets via
  `gh release delete-asset`).

### Operator audit cleanup

- Ran `scripts/delete-past-minor-assets.sh` against 105 past-minor
  releases (v1.1.0 → v5.21.0): deleted 477 binary attachments. Major
  releases (v1.0.0 / v2.0.0 / v3.0.0 / v4.0.0 / v5.0.0) keep their
  binaries forever per the new rule. v5.22.0 (immediate prior)
  retains binaries so the upgrade path works.
- README.md marquee → v5.23.0.

### Carry-over (future patches)

- Autonomous tab WS auto-refresh on PRD changes
- Diagrams page restructure (drop plans, add howtos + app-docs)
- Design doc audit / refresh
- Settings card-section docs chips + howto links for complex sections
- datawatch-app#10 catch-up issue for v5.3.0 → v5.23.0 PWA changes
- Container parent-full retag
- GHCR container image cleanup for past-minor versions
- gosec HIGH-severity review

## [5.22.0] - 2026-04-26

Minor — observability fill-in + arrow-buttons layout fix.

### Added

- **`LoopStatus`** carries BL191 Q4 + Q6 counters: `ChildPRDsTotal`,
  `MaxDepthSeen`, `BlockedPRDs`, `VerdictCounts` (outcome → count
  rollup across Story.Verdicts + Task.Verdicts). Surfaces through
  `GET /api/autonomous/status`, the `autonomous_status` MCP tool,
  and the `autonomous status` chat verb. 4 new unit tests.

### Fixed

- **Tmux arrow buttons** now right-justified (`margin-left:auto`)
  next to the saved-commands dropdown. v5.19.0 restored them but
  placed them before the dropdown which let flex-wrap put them on
  the next line.
- **`containers` CI workflow.** Every tag push since v5.21.0 failed
  with `agent-base:VERSION: not found` because Stage 1 pushes
  agent-base to `ghcr.io/dmz006/datawatch-agent-base` (hyphen) while
  Stage 2's agent-* Dockerfiles `FROM ${REGISTRY}/agent-base` resolves
  to the slash-namespaced path. Stage 1 now ALSO tags agent-base under
  the slash form (`ghcr.io/dmz006/datawatch/agent-base`) so the layer
  chain works. Hyphen tag remains primary for operator pulls.
- **PRD Edit modal button.** v5.19.0's `onclick="submitPRDEdit(${JSON.stringify(id)})"`
  produced unescaped double-quotes inside a double-quoted attribute
  and broke the handler. Now HTML-escapes via `escHtml(JSON.stringify(id))`.
- **README.md** marquee → v5.22.0.

## [5.21.0] - 2026-04-26

Minor — observer + whisper config-parity sweep (audit follow-up #2).

### Fixed

- **`internal/config.ObserverConfig`** gained `ConnCorrelator` (BL293,
  v5.6.1) + `Peers` (BL172, v4.5.0). Pre-v5.21.0 these fields lived
  only on `internal/observer.Config` so YAML round-trip + REST
  `PUT /api/config` couldn't reach them. New `ObserverPeersConfig`
  mirrors `observer.PeersCfg` (AllowRegister + token-rotation grace
  + push interval + listen addr).
- **`cmd/datawatch/main.go`** observer-Manager bridge now copies
  the two new fields.
- **`applyConfigPatch`** gained 20 new cases for observer scalars,
  pointer-bools, federation, peers, ollama_tap, plus the missing
  whisper HTTP fields (backend/endpoint/api_key).
- **README.md** marquee → v5.21.0.

### Tests

- 6 new in `internal/server/observer_whisper_patch_test.go`. 1382
  total (1376 → 1382).

## [5.20.0] - 2026-04-26

Minor — documentation alignment sweep (audit follow-up #1).

### Fixed

- **docs/mcp.md** — stale "41 tools" count corrected to "100+"; added
  family-breakdown table (Sessions / Autonomous / Orchestrator / Pipelines
  / Memory+KG / Observer / Agents / Plugins / etc.); called out
  v5.9 → v5.19 tool additions.
- **docs/cursor-mcp.md** — tool table extended beyond the original five
  to include autonomous PRD lifecycle, observer cross-host, memory+KG,
  orchestrator, pipelines.
- **docs/api/autonomous.md** — documented every REST endpoint added
  since v5.2.0 (approve/reject/request_revision/edit_task/instantiate/
  set_llm/set_task_llm/children/PATCH/DELETE?hard=true).
- **docs/api/observer.md** — documented the cross-host endpoint, every
  observer MCP tool name, every observer CLI subcommand.
- **internal/server/web/openapi.yaml** — added paths for
  `/api/autonomous/prds/{id}/children`, `PATCH
  /api/autonomous/prds/{id}`, the `?hard=true` query parameter on
  the existing DELETE, and `/api/observer/envelopes/all-peers`.
- **README.md** — marquee → v5.20.0.

## [5.19.0] - 2026-04-26

Minor — operator-blocking CRUD + UX cleanup + audit-gap closure.

### Added

- **Autonomous full CRUD.** New `Store.DeletePRD` (recursion-aware:
  removes descendants spawned via `Task.SpawnPRD`) + `Store.UpdatePRDFields`
  (edit title + spec on non-running PRDs). Manager wrappers refuse to
  delete a running PRD. New `DELETE /api/autonomous/prds/{id}?hard=true`
  + `PATCH /api/autonomous/prds/{id}` REST endpoints. New CLI
  `datawatch autonomous prd-delete` + `prd-edit`. New PWA Edit + Delete
  buttons on every PRD card (Edit on non-running only) with confirm
  dialogs. 8 new unit tests.

### Fixed

- **PWA whisper test-button.** `loadGeneralConfig` lacked an
  `f.type === 'button'` branch; the test transcription button rendered
  as `<input type="button">` (empty box). Added the branch mirroring
  the comms renderer.
- **PWA tmux arrow keys regression.** `loadSavedCmdsQuick` was
  overwriting `#savedCmdsQuick.innerHTML` after the initial render
  placed the Response button + ↑↓←→ group there. Restored Response +
  arrows in the rebuild path.
- **PWA "Input Required" label duplicated.** Removed the inline
  `<div class="input-label">Input Required</div>` above the tmux
  input box — the top-of-page yellow badge already conveys
  waiting_input state.
- **PWA RTK section duplicated.** Removed from `GENERAL_CONFIG_FIELDS`;
  the fuller version (with `auto_update` + `update_check_interval`)
  remains in `LLM_CONFIG_FIELDS`.
- **README.md marquee bumped to v5.19.0** — was 12 releases stale
  (v5.0.4 → v5.18.0) across two sessions per the audit findings.

## [5.18.0] - 2026-04-26

Patch — MCP channel one-way bug for TLS + claude-code daemons.

### Fixed

- **MCP channel `/api/channel/ready` blocked by HTTP→HTTPS redirect.**
  Symptom: `claude mcp list` shows `✓ Connected` but the daemon
  never pushes messages back to claude (reply tool works one-way
  only). Root cause: the bridge POSTs to
  `http://127.0.0.1:8080/api/channel/ready`, the HTTP listener 307s
  to HTTPS, the bridge's HTTP client follows the redirect and fails
  TLS verify on the self-signed cert. Fix: the HTTP→HTTPS redirect
  handler bypasses the redirect for loopback requests to
  `/api/channel/*` paths and serves them via the main mux directly.
  New `isLoopbackRemote` helper covers 127/8 + ::1 + IPv4-mapped IPv6.

## [5.17.0] - 2026-04-26

Minor — operator-surface bridge for v5.9.0 (BL191 Q4) + v5.10.0
(BL191 Q6) config knobs. Pre-v5.17.0 the runtime feature shipped
but `datawatch config set autonomous.max_recursion_depth …` /
`datawatch config set autonomous.per_task_guardrails …` silently
no-op'd through PUT /api/config; YAML round-trip dropped the
fields; PWA Settings → Autonomous didn't expose them.

### Fixed

- **`internal/config/AutonomousConfig`** gained the four fields
  (`MaxRecursionDepth`, `AutoApproveChildren`, `PerTaskGuardrails`,
  `PerStoryGuardrails`) so YAML + JSON round-trip preserves them.
- **`cmd/datawatch/main.go`** autonomous-Manager translation copies
  the four new fields. When operator hasn't touched the recursion
  knobs, falls back to package `DefaultConfig()`.
- **`internal/server/api.go.applyConfigPatch`** now handles the four
  keys. List-shaped keys accept both JSON arrays + CSV strings
  (new `splitCSV` helper) so the PWA text-input shape works.
- **PWA Settings → General → Autonomous** gained four field entries
  (number / toggle / text / text). Round-trips through the existing
  `String(arr)` display path.

### Tests

- 2 new (1357 total): `TestApplyConfigPatch_AutonomousRecursionAndGuardrails`
  + `TestSplitCSV`.

## [5.16.0] - 2026-04-26

Minor — PWA visualizations for the v5.9.0 / v5.10.0 / v5.12.0
data-model work.

### Added

- **BL191 Q4 PWA viz.** PRD cards on the Autonomous tab show
  `↗ parent <id>` + `depth N` badges when present; new
  **Children (lazy)** disclosure calls
  `GET /api/autonomous/prds/{id}/children` to render the genealogy
  tree. Per-task: `↳ spawn` badge for `task.spawn_prd === true`,
  `→ child <id>` link once the executor has spawned the child.
- **BL191 Q6 PWA viz.** Story-level + task-level verdict badges
  render inline next to titles. Color-coded by outcome
  (pass / warn / block); tooltip surfaces severity + summary +
  first three issues.
- **BL180 cross-host PWA viz.** New "↔ Cross-host view" button on
  the Federated peers filter row. Opens a modal that fetches
  `/api/observer/envelopes/all-peers` and renders one collapsible
  section per peer with listen addrs, outbound edges, and caller
  rows (cross-host attributions get a `🔗 cross` badge).

## [5.15.0] - 2026-04-26

Minor — BL190 density expansion (first cut).

### Added

- **BL190 density.** Recipe map in `scripts/howto-shoot.mjs` grew
  from 11 to 19; PNG total from 11 to 22. New recipes cover mobile
  viewports (sessions/autonomous/settings-monitor/session-detail),
  Settings deep-scrolls (general → autonomous block, general →
  auto-update, comms → Signal, LLM → Ollama, LLM → Episodic Memory),
  the autonomous-prd-expanded toggle (with seeded fixrich PRD
  carrying 1 story + 3 tasks + 3 decisions + 1 verdict), the
  diagrams-flow content view, and the header-search filter chips.
  Multi-shot sequences in 8 howtos: chat-and-llm-quickstart (6),
  daemon-operations (6), autonomous-review-approve (4), autonomous-
  planning (3), comm-channels (2), cross-agent-memory (2), federated-
  observer (2), prd-dag-orchestrator (2). Seed-fixtures script also
  fixed: shell counter was matching `fixture:` instead of
  `"fixture":true`.

### Changed

- **`scripts/howto-shoot.mjs`** — per-recipe `viewport` overrides
  added so the same driver handles desktop + portrait captures
  without two passes.

## [5.14.0] - 2026-04-26

Minor — BL190 expand-and-fill: every howto has at least one screenshot.

### Added

- **BL190 — Full howto screenshot coverage.** Recipe map in
  `scripts/howto-shoot.mjs` extended from 6 to 11 (`settings-monitor`,
  `settings-about`, `alerts-tab`, `autonomous-new-prd-modal`,
  `session-detail` added). Inline coverage extended from 4 to all 13
  howtos. Per-howto density stays at 1-3 PNGs (below the original
  15-20 target); the load-bearing pipeline is in place for iterative
  expansion. 11 PNGs in `docs/howto/screenshots/`, mirrored into the
  embedded PWA docs viewer via `make sync-docs`.

## [5.13.0] - 2026-04-26

Minor — BL180 Phase 2 eBPF kprobes resumed. **BL180 fully closed**
(procfs cut v5.1.0 + cross-host v5.12.0 + eBPF kprobes v5.13.0).

### Added

- **BL180 Phase 2 eBPF kprobes (resumed).** Per BL292 v5.6.0 commit
  roadmap. New `tcp_connect` (outbound) + `inet_csk_accept` (inbound)
  kprobes feed a `conn_attribution` `BPF_MAP_TYPE_LRU_HASH` (key =
  sock pointer, value = {pid, ts_ns}). LRU eviction bounds memory
  under heavy connection churn. New userspace
  `realLinuxKprobeProbe.ReadConnAttribution()` iterates the map;
  `PruneConnAttribution(olderThanNs)` walks + deletes stale entries
  for freshness. Loader attaches non-fatally — failure on the new
  pair leaves the byte counters live. bpf2go regenerated cleanly
  under clang 20.1.8 with both committed `vmlinux_amd64` +
  `vmlinux_arm64` headers; new `.o` artifacts updated in tree. 3 new
  unit tests cover nil-safe iterator + post-Close idempotence + row
  shape; real attach requires CAP_BPF + matching kernel symbols and
  is validated via the operator's Thor smoke-test.

## [5.12.0] - 2026-04-26

Minor — BL180 Phase 2 cross-host federation correlation (Q5c) closed.

### Added

- **BL180 Phase 2 — Cross-host correlation.** New `Envelope.ListenAddrs`
  + `Envelope.OutboundEdges` fields populated by the local procfs
  correlator. New `observer.CorrelateAcrossPeers` function joins
  outbound edges from one peer against listen addrs on another,
  producing `CallerAttribution{Caller: "<peer>:<envelope-id>", ...}`
  rows on the matched server envelope. Reachable as
  `GET /api/observer/envelopes/all-peers`, `observer_envelopes_all_peers`
  MCP tool, and `datawatch observer envelopes-all-peers` CLI. 7 new
  unit tests (1352 total). Operator's "don't close until cross-host
  works" satisfied.

## [5.11.0] - 2026-04-26

Minor — BL190 howto screenshot capture pipeline first cut.

### Added

- **BL190 — Howto screenshot capture pipeline**. Operator removed the
  chrome MCP plugin (suspected memory leak); new path goes through
  puppeteer-core in `/tmp/puppet` driving `/usr/bin/google-chrome`
  headless — entirely outside the chrome MCP. New `scripts/howto-shoot.mjs`
  (recipe-driven; 6 recipes shipping: sessions-landing, autonomous-
  landing, settings-llm, settings-comms, settings-voice, diagrams-
  landing). New `scripts/howto-seed-fixtures.sh` (idempotent; wipes
  `fixture:true` JSONL rows + re-seeds one PRD per status pill, one
  orchestrator graph + guardrail node, one pipeline with before/after
  gates). 6 screenshots committed under `docs/howto/screenshots/`,
  inlined into chat-and-llm-quickstart, autonomous-planning, voice-
  input, mcp-tools. Makefile sync-docs already includes `*.png` so the
  embedded PWA docs viewer carries them too.

## [5.10.0] - 2026-04-26

Minor — BL191 Q6 (guardrails-at-all-levels) closed; with it, **BL191 is done in its entirety** (Q1+Q2+Q3 v5.2.0, Q4 v5.9.0, Q5 v5.5.0, Q6 v5.10.0).

### Added

- **BL191 Q6 — Guardrails at all levels.** Per-story + per-task
  guardrails run after the corresponding unit verifies green; a
  `block` verdict halts the PRD with status=blocked. New
  `Story.Verdicts` + `Task.Verdicts` slices, new `Config.PerTaskGuardrails`
  + `Config.PerStoryGuardrails` knobs (defaults empty = opt-in), new
  `Manager.SetGuardrail(GuardrailFn)` indirection. main.go wires the
  function to a `/api/ask` loopback (same path BL25 verifier uses;
  `verification_backend` / `verification_effort` apply). New
  `PRDBlocked` status. Permissive parse — unparseable LLM output =
  `warn` so best-effort runs still progress. 6 new unit tests cover
  empty-config no-op, all-pass append, task-block-halts-PRD,
  story-fires-after-all-tasks-done, story-block-halts, no-fn-wired
  silent no-op.

## [5.9.0] - 2026-04-26

Minor — BL191 Q4 recursive child-PRDs closed.

### Added

- **BL191 Q4 — Recursive child-PRDs** (option (a) shortcut from the
  design doc). New `Task.SpawnPRD bool` flag turns a task spec into a
  child PRD spec. New `PRD.ParentPRDID/ParentTaskID/Depth` fields
  give every spawned PRD a genealogy pointer + cycle-safe depth
  counter. New `Config.MaxRecursionDepth` (default 5; 0 disables) +
  `Config.AutoApproveChildren` (default true) gate the recursion
  behavior. Full configuration parity: REST `GET /api/autonomous/prds/
  {id}/children`, MCP `autonomous_prd_children`, CLI
  `datawatch autonomous prd-children <id>`, chat verb
  `autonomous children <id>`, YAML `autonomous.{max_recursion_depth,
  auto_approve_children}`. 5 new unit tests cover the recursion matrix.

## [5.8.0] - 2026-04-26

Minor — BL201 voice/whisper backend inheritance closed.

### Added

- **BL201 — Voice/whisper backend inheritance**. New
  `inheritWhisperEndpoint` helper in `cmd/datawatch/main.go` fills
  `whisper.endpoint` + `whisper.api_key` from `cfg.OpenWebUI.URL` +
  `APIKey` when `whisper.backend = openwebui`, and from
  `cfg.Ollama.Host + /v1` when `whisper.backend = ollama`. Explicit
  values always win. New `ollama` case added to
  `internal/transcribe/factory.go` (routes through the OpenAI-compat
  client). 8 new unit tests cover the inheritance matrix.

## [5.7.0] - 2026-04-26

Minor — BL200 howto coverage expansion + `datawatch reload` CLI parity fix.

### Added

- **BL200 — How-to coverage expansion** (13 walkthroughs, up from 6).
  Seven new docs: `setup-and-install.md`, `chat-and-llm-quickstart.md`,
  `autonomous-review-approve.md`, `voice-input.md`, `comm-channels.md`,
  `federated-observer.md`, `mcp-tools.md`. Original six refreshed
  against everything shipped since v4.9.3. Each walkthrough keeps
  the per-channel reachability matrix (CLI / REST / MCP / chat / PWA)
  at the bottom.
- **`datawatch reload` CLI subcommand** — closes the
  configuration-parity gap. BL17 already had SIGHUP + `POST /api/reload`
  + the MCP `reload` tool; the CLI is now the fifth surface. Lets
  every howto recommend `datawatch reload` after `datawatch config set`.

### Fixed

- **`internal/server/api.go` version drift** — `var Version` was
  stuck at `"5.0.3"` while `cmd/datawatch/main.go` marched through
  5.0.x → 5.6.1. LDFLAGS injection masked the runtime impact, but
  the AGENT.md "must be updated together" rule was being violated.
  Both files re-synced to 5.7.0.
- **`docs/howto/mcp-tools.md` tool-table accuracy** — the new doc
  named tools that didn't exist (`session_list`, `memory_save`,
  `pipeline_create`, `observer_envelope_summary`, `plugins_status`).
  Verified against `internal/mcp/*.go` `NewTool(...)` registry; table
  now reflects the real surface.
- **`docs/howto/federated-observer.md` flag accuracy** —
  `peer revoke` doesn't exist (it's `peer delete`); `--token` flag
  doesn't exist (it's `--token-file <path>`). Added the missing
  `peer register` step where the token is actually minted.

### Carry-over

- BL180 Phase 2 (eBPF kprobes + cross-host federation correlation).
- BL191 Q4 (recursive child-PRDs through BL117) + Q6 (guardrails-
  at-all-levels).
- BL201 (voice/whisper backend inheritance — daemon-side resolution).
- BL190 (PWA screenshot rebuild against the now-13-doc suite — chrome
  plugin removed; will use puppeteer-core + seeded JSONL fixtures).

## [5.6.1] - 2026-04-26

Patch — emergency: disable BL180 Phase 2 conn correlator by default.

Operator hit OOM after today's v5.0.0→v5.6.0 release cycle ("not
happening yesterday"). eBPF is off on the host (no CAP_BPF). Suspect
narrowed to the BL180 Phase 2 procfs `CorrelateCallers` shipped in
v5.1.0: every observer tick (1 s default) opens
`/proc/<pid>/net/tcp` + `tcp6` for every tracked envelope PID and
allocates a fresh 64 KB `bufio.Scanner` buffer per file. With ollama
running (backend envelope present) + a busy host (200+ envelope
PIDs), that's ~25 MB/sec of GC churn — enough to pressure the
kernel page cache and eventually trigger OOM-killer on neighbour
processes (claude, tmux, browser).

### Fixed

- **Conn-correlator now opt-in** — added `observer.conn_correlator`
  config field, defaults to `false`. The v5.1.0 envelope-shape
  change (`Envelope.Callers []CallerAttribution`) stays so the
  wire contract is stable; the per-tick procfs walk just doesn't
  fire unless the operator opts in. Phase 1 ollama tap continues
  to fill `Caller` for ollama-model envelopes regardless.

### Operator note

After install + restart, daemon RSS + system memory pressure should
drop within seconds. Re-enable later via:

```yaml
observer:
  conn_correlator: true
```

(or the equivalent `PUT /api/config` payload).

This is a single-line patch behind a feature gate; no other
behaviour changes.

## [5.6.0] - 2026-04-26

Minor — BL292 leak audit pass 2 (deeper sweep after v5.5.0 didn't
fully calm the operator's OOM concern). Two real leaks fixed in
session manager + autonomous learnings store. All 5 cross-compile
binaries attached per the minor-release rule.

### Fixed

- **`session.Manager.promptOscillation` map+slice double leak** —
  per-session `[]time.Time` slice grew on every running↔waiting flip
  with no cap, AND the map entry itself was never deleted on session
  removal. A long-lived daemon that ran thousands of sessions
  accumulated thousands of dead entries plus per-session slices that
  could grow into the hundreds of thousands of timestamps. Now: (a)
  capped to the last 100 timestamps per session (backoff only needs
  recent transitions), (b) deleted on session-removal alongside the
  existing monitors / mcpRetryCounts / rawInputBuf cleanup. While
  there, also drop the matching `promptFirstSeen` /
  `promptLastNotify` entries (same lifecycle gap) and the
  `lastResponseCache` entry from BL291.
- **`autonomous.Store.AddLearning` unbounded slice** — BL57 KG
  learnings appended on every PRD task completion with no cap; the
  rewrite-everything `persist()` path then re-marshalled the whole
  slice on each call. Now capped at 1000 most-recent — older
  learnings are already mirrored into episodic memory + the KG so
  the autonomous store doesn't need to be the source of truth.

### Operator note

BL180 Phase 2 eBPF kprobe work was in flight at the v5.5.0 ship
and crashed mid-edit. Backed out cleanly: the `.bpf.c` source
edits never compiled (bpf2go choked on `BPF_CORE_READ` macro
chains against the vmlinux.h dump) and the `netprobe_x86_bpfel.o`
was restored from HEAD. Daemon stays on the v5.1.0 procfs userspace
correlator. eBPF kprobe work resumes in a separate cycle with
`BPF_MAP_TYPE_LRU_HASH` (auto-evicts) + a userspace TTL pruner so
kernel memory growth is bounded by design.

### Changed

- **`internal/server/web/sw.js`** — `CACHE_NAME` bumped
  `datawatch-v5-5-0` → `datawatch-v5-6-0`.

## [5.5.0] - 2026-04-26

Minor — BL291 since-v4 memory-leak audit + BL202/BL203 PWA LLM
dropdowns. All 5 cross-compile binaries attached per the
minor-release rule.

### Fixed

- **`session.GetLastResponse` re-capture storm** — BL178 reopen in
  v5.1.0 made every API read re-capture from tmux for live sessions;
  for encrypted session logs `TailOutput` reads the entire file (no
  seek-tail path), so a chat reply burst on a multi-MB encrypted log
  was repeatedly allocating large byte slices. New 2-second TTL cache
  (`sync.Map` of `cachedResponse`) with periodic bounded eviction
  (cap 256 entries, walk every ~10 s).
- **`autonomous.PRD.Decisions` unbounded growth** — BL191 v5.2.0
  appends one row on every transition (decompose / approve / reject /
  run / verify / edit / set_llm / set_task_llm). No cap. A long-lived
  PRD that's been re-decomposed + re-run repeatedly grew multi-MB
  Decisions slices that bloated every JSONL row + the in-memory Store
  snapshot. New `trimDecisions()` called from `Store.SavePRD` caps
  at 200 most-recent.
- **`observer.CorrelateCallers` per-tick procfs walk** — BL180 Phase 2
  v5.1.0 opens `/proc/<pid>/net/tcp` + `tcp6` for every tracked PID
  every observer tick (1 s default). Short-circuit added when no
  envelope of `Kind=Backend` is present — attribution flows
  client→backend so the per-tick walk has no work to do without a
  backend in scope. Saves ~200 file opens/sec on hosts that aren't
  running an LLM-server-shaped envelope.
- **PWA `state.lastResponse` map** — accumulated one entry per
  observed session ID with no eviction; long-lived browser tabs
  handling many sessions grew the map forever. Now bounded to 128
  entries; oldest 16 dropped on overflow.

### Added (BL202 / BL203 — PWA LLM flexibility, second cut)

- **PRD-create modal** rebuilt as a real form (replacing the v5.3.0
  `prompt()` chain) with backend / effort / model dropdowns wired
  to the v5.4.0 `set_llm` endpoint. Backends list pulled live from
  `/api/backends`.
- **Per-task edit modal** — same shape: spec textarea + per-task
  backend / effort / model dropdowns wired to `set_task_llm`. Both
  edits land in one round trip (the modal calls `edit_task` +
  `set_task_llm` only when the field actually changed).
- **PRD-row "LLM" action button** — opens a modal that sets the
  PRD-level worker LLM via `set_llm`. Only visible pre-Run.
- **Per-PRD + per-task LLM badges** — small chips render the
  current override values inline so operators can see what'll run
  before clicking Approve.

### Changed

- **`internal/server/web/sw.js`** — `CACHE_NAME` bumped
  `datawatch-v5-4-0` → `datawatch-v5-5-0`.

## [5.4.0] - 2026-04-26

Minor — flexible LLM selection across autonomous operations
(per-task / per-PRD / per-stage / global cascade), plus stale-MCP
cleanup, voice-input test surface, and a voice howto. All 5
cross-compile binaries attached per the minor-release rule.

### Added

- **Flexible LLM selection at every autonomous level** — per-task
  beats per-PRD beats stage default beats global. New
  `Task.{Backend,Effort,Model}` fields plus `PRD.Model`. Two new
  surfaces:
  - `POST /api/autonomous/prds/{id}/set_llm {backend,effort,model}` —
    PRD-level override that all tasks inherit.
  - `POST /api/autonomous/prds/{id}/set_task_llm {task_id,backend,
    effort,model}` — per-task override; allowed pre-Run only.
  - CLI: `datawatch autonomous prd-set-llm` + `prd-set-task-llm`.
  - Chat: `autonomous set-llm <prd> <backend> [effort] [model]` +
    `autonomous set-task-llm <prd> <task> <backend> [effort] [model]`
    (also under `prd` alias).
  - MCP: `autonomous_prd_set_llm` + `autonomous_prd_set_task_llm`.
- **Voice transcription test surface** — `POST /api/voice/test`
  feeds a 1 KB silent WAV through the configured backend. Settings
  → General → Voice Input gains a "Test transcription endpoint"
  button that calls it; on failure the daemon refuses to leave
  `whisper.enabled=true` so a broken backend doesn't keep firing.
- **`docs/howto/voice-input.md`** — end-to-end walkthrough for all
  five backend variants (whisper local, openai, openai_compat,
  openwebui, ollama) with the inheritance rules called out
  explicitly per operator request.

### Fixed

- **Stale node + channel.js MCP registration auto-cleanup** —
  operator on v5.3.0 saw `/usr/bin/node ~/.datawatch/channel/channel.js`
  spawn for new sessions even though `[channel] using native Go
  bridge` was logged. Root cause: a leftover unsuffixed `datawatch`
  entry in `claude mcp list` (project-scope `.mcp.json`) from
  before the Go-bridge migration. New `channel.CleanupStaleJSRegistrations()`
  scans all scopes on daemon start and removes any `datawatch*`
  entry pointing at `node + channel.js`. Logged as
  `[channel] removed stale JS-bridge MCP registration(s): ...`.
- **Internal-ID leak in PWA voice-input label** — the v5.3.0 label
  contained `see [task #282]` in operator-visible UI. Rewritten in
  operator language; the only such leak in the PWA today.

### Changed

- **`docs/howto/voice-input.md`** explicitly documents that
  `openai`, `openai_compat`, `openwebui`, and `ollama` backends
  inherit endpoint + API key from their LLM-config block — no
  duplicate config required.
- **`internal/server/web/sw.js`** — `CACHE_NAME` bumped
  `datawatch-v5-3-0` → `datawatch-v5-4-0`.

## [5.3.0] - 2026-04-26

Minor — BL202 (BL191 PWA full CRUD) lifted to its own top-level
**Autonomous** tab + a few embedded-doc + doc-back-button polish items.
Howto coverage cycle paused per operator (resumes after the open
BL182/BL201/BL180-Phase2-followup land).

### Added

- **PWA top-level "Autonomous" tab** — operator directive 2026-04-26:
  PRDs are first-class workflow on par with Sessions, not buried
  inside Settings → General. New `Autonomous` button in the bottom
  nav opens a list of every PRD with status pill, click-to-expand
  stories + tasks, decisions log, and per-status action buttons:
  Decompose / Approve / Reject / Request-revision / Run / Cancel.
  Inline task-spec editor (pencil icon) when status is
  `needs_review` / `revisions_asked`. Templates surface an
  Instantiate button. New-PRD modal at the top of the panel.
- **Howto drafts (paused work, kept on disk)** —
  `setup-and-install.md`, `chat-and-llm-quickstart.md`,
  `autonomous-review-approve.md` written this cycle. Resume of
  the BL200 howto cycle pulls in the rest after the open BLs ship.

### Changed

- **`docs/_embed_skip.txt`** — added `plans` so `docs/plans/`
  doesn't ship inside the daemon binary (operator-internal). The
  embedded `/diagrams.html` viewer now only carries `howto/` and
  user-facing docs.
- **`/diagrams.html`** — added a `← PWA` back button in the header
  next to the drawer toggle so operators can return to the main
  PWA without hitting browser back.
- **`internal/server/web/sw.js`** — `CACHE_NAME` bumped
  `datawatch-v5-2-0` → `datawatch-v5-3-0`.

### Mobile alignment

`datawatch-app` issue with the new top-level Autonomous tab + the
v5.2.0 arrow-keys row tracked in the existing
[catch-up issue #9](https://github.com/dmz006/datawatch-app/issues/9)
plus a follow-up issue for the new tab.

## [5.2.0] - 2026-04-26

Minor — BL191 first cut (autonomous PRD lifecycle: review/approve gate
+ decisions log + templates) per operator-answered design doc, plus a
cluster of PWA-side polish landed during the cycle. All 5 cross-compile
binaries attached per the minor-release rule.

### Added

- **BL191 Q1 — review/approve gate**: PRD status machine extended with
  `decomposing → needs_review → approved → running → complete` plus
  `revisions_asked` / `rejected` / `cancelled` terminals. Decompose now
  lands in `needs_review`; Run refuses unless status is `approved`
  (legacy `active` honored for back-compat with v5.1.x stores so
  upgraded daemons don't strand in-flight work).
- **BL191 Q3 — decisions log**: new `Decision` struct + per-PRD
  `Decisions []Decision` slice. Decompose / Run / Approve / Reject /
  RequestRevision / EditTask / TemplateInstantiate each append a row
  with backend, prompt-chars, response-chars, actor, note. Surfaces
  via `GET /api/autonomous/prds/{id}` (in the existing payload).
- **BL191 Q2 — templates (scaffold)**: PRD gains `IsTemplate bool` +
  `TemplateVars []TemplateVar` + `TemplateOf` back-pointer. Templates
  are PRDs with `IsTemplate=true` (one schema, two views per Q2
  recommendation); `POST /api/autonomous/prds/{id}/instantiate {vars,
  actor}` substitutes `{{var}}` markers in spec / title / per-task
  spec and stores a fresh executable PRD with `Status=needs_review`.
- **5 new REST endpoints**: `POST /api/autonomous/prds/{id}/{approve|reject|request_revision|edit_task|instantiate}`.
- **5 new CLI subcommands**: `datawatch autonomous prd-{approve,reject,request-revision,edit-task,instantiate}`.
- **5 new chat verbs** in `internal/router/sx2_parity.go::handleAutonomous`:
  `autonomous {approve|reject|request-revision|edit-task|instantiate}`
  + the same under the `prd` alias.
- **5 new MCP tools**: `autonomous_prd_{approve,reject,request_revision,edit_task,instantiate}`.
- **9 new lifecycle tests** in `internal/autonomous/lifecycle_test.go`
  covering each transition + the template substitution pass + the
  Run gate.
- **Settings → About** — added a `datawatch-app` GitHub link with a
  note that the Play Store link will land here once the mobile app
  publishes (operator 2026-04-26).
- **Settings → About** — orphaned-tmux clear affordance moved here
  from Settings → Monitor → System Statistics (operator 2026-04-26
  — it's a maintenance affordance, not a live metric).
- **Settings → General → Voice Input** — backend dropdown now exposes
  `whisper / openai / openai_compat / openwebui / ollama` so operators
  can pick from any backend the daemon already knows about; venv-path
  field is documented as whisper-only. Inheritance from already-
  configured LLM backends (no separate endpoint+key) is queued as
  task #282 for the next release.
- **`internal/server/web/app.js`** — generic `select` + `button` field
  renderers added so future config blocks can wire dropdowns and
  action buttons without bespoke HTML.

### Changed

- **PWA settings** — `Settings → About` reordered so Mobile-app and
  Orphaned-tmux rows sit below Project (cleanest natural reading
  order).

### Open (deferred from v5.2.0 per design-doc Q5)

- BL191 Q5 — full PWA CRUD UI for the PRD lifecycle (list, expand,
  per-task edit modal, template instantiation flow, decisions log
  viewer) and the LLM dropdown with model + effort selection. Tracked
  as task #283.
- BL191 Q4 — recursive child-PRD spawning via the BL117 orchestrator.
- Q6 guardrails-at-all-levels — orchestrator/Manager integration.

## [5.1.0] - 2026-04-26

Minor — BL180 Phase 2 first cut (per-caller envelope attribution) +
session-detail toolbar cleanup. All 5 cross-compile binaries attached
to the GitHub release per the minor-release rule.

### Added

- **`Envelope.Callers []CallerAttribution`** — per-client breakdown
  of who's hitting an envelope's process(es) right now. Sorted by
  conns desc so `Callers[0]` is the loudest. Empty when only one
  caller is attributable. Existing `Caller` / `CallerKind` become
  derived "loudest caller" aliases for back-compat single-caller
  renders. Phase 1 (ollama tap) `Caller` values are preserved.
- **`internal/observer/conn_correlator.go`** — userspace correlator.
  Walks each tracked PID's `/proc/<pid>/net/tcp` (+ tcp6), builds
  a `(localIP, localPort) → envelope` listen map from the LISTEN
  rows, then joins ESTABLISHED outbound rows back to their server
  envelope. Operator answer Q5: localhost + private-bridge ranges
  only this release (10.x / 172.x / 192.168.x). Cross-host
  federation correlation stays open per Q5c.
- **9 unit tests** in `conn_correlator_test.go` covering hex-IP
  parsing (v4 + v6), `/proc/net/tcp` row parsing, scope filter,
  end-to-end join, Phase 1 caller preservation, and the
  `FormatCallerSummary` log helper.

### Fixed

- **BL178 reopen** — operator on v5.0.5: the session-list response icon
  was opening to text from "weeks ago" on long-lived running sessions.
  Daemon-side `Manager.GetLastResponse` returned only the stored
  `Session.LastResponse` (captured on running→waiting_input
  transitions), which can stay frozen for a session that's been
  running for days. Fix: when the session is `running` or
  `waiting_input`, `GetLastResponse` re-captures from the live tmux
  pane on every read and persists if changed; terminated sessions
  keep their last-word stored value.
- **Session-list FAB position (Chrome desktop)** — FAB was anchored
  to the viewport's right edge so on a wide window the `+` sat
  outside the centered 480px PWA card. Fix: scoped a
  `right: calc(50vw - 240px + 16px)` override into the
  `@media (min-width:600px)` block so the FAB tracks the card.
- **Session-list FAB position (phone overlap)** — FAB `bottom`
  was `64px` while `--nav-h` is `60px` (4px gap → visual overlap on
  Chrome mobile). Fix: switched to `calc(var(--nav-h) + 16px +
  safe-area)` for a proper 16px clearance above the bottom nav.

### Changed

- **PWA session detail** — removed the `toggle terminal toolbar`
  affordance + `_termToolbarHidden` state + `cs_term_toolbar_hidden`
  localStorage key + `term-toolbar-hidden` CSS rules. Operator
  feedback 2026-04-26: the term-toolbar layout (tmux/channel pills,
  font controls, scroll button) reads cleanly at every viewport;
  the toggle was just getting in the way. Mobile shell aligned via
  [datawatch-app#8](https://github.com/dmz006/datawatch-app/issues/8).
- **Session-list "show / hide history" toggle** — renamed to just
  "History (N)". Keeps the count, drops the verb churn.
- **`internal/server/web/sw.js`** — `CACHE_NAME` bumped
  `datawatch-v5-0-5` → `datawatch-v5-1-0` so installed PWAs
  invalidate cleanly and pick up the toolbar-cleanup app.js + style.css.

### Open (per operator answers in design doc)

- BL180 Phase 2 eBPF kprobe layer (Q1) — `__sys_connect` +
  `inet_csk_accept` + `conn_attribution` map for byte-level
  precision and lower scan cost. Procfs path covers operator
  visibility today.
- BL180 Phase 2 cross-host correlation (Q5c) — federation-aware
  joins so an opencode session on host A is attributed to an
  ollama backend envelope on host B. Operator: don't close until
  this works.
- BL180 Phase 2 Thor smoke test (Q6) — needs operator-provisioned
  arm64 host with ollama serving openwebui + opencode at once.

## [5.0.5] - 2026-04-26

Patch — BL198 reopened and properly fixed. Operator on v5.0.4 reported
the diagrams.html drawer "doesn't fully hide; shows a small piece on
left" plus a blank docs/diagram pane when the drawer was collapsed on
mobile. Same closed-against-source pattern as BL187: the v4.8.18 fix
landed CSS that worked in the dev viewport but fell apart in two
places. Plus infrastructure groundwork for the upcoming BL190 howto
screenshot rebuild.

### Fixed

- **`internal/server/web/diagrams.html`** — two distinct CSS bugs:
  1. **Desktop collapsed**: the aside's 1px `border-right` leaked as
     a 1-pixel strip at x=-1 because `box-sizing:border-box` + a 0px
     grid column didn't suppress it. Fixed by adding
     `border-right:none; width:0; visibility:hidden; overflow:hidden`
     on `.body.aside-collapsed aside`.
  2. **Mobile collapsed**: the desktop rule
     `.body.aside-collapsed { grid-template-columns: 0px 1fr }` won by
     specificity inside the mobile media query. With aside
     `position:fixed` and out of grid flow, auto-placement put `main`
     into the 0px first cell so it rendered at ~28 px (just its
     padding) — that was the "blank screen" the operator reported.
     Fixed by adding `.body.aside-collapsed { grid-template-columns: 1fr }`
     inside the mobile media query so the layout stays single-column
     when collapsed.
  Both verified via puppeteer at desktop-open / desktop-collapsed /
  mobile-default / mobile-open.
- **`internal/server/web/sw.js`** — `CACHE_NAME` bumped
  `datawatch-v5` → `datawatch-v5-0-5` so installed PWAs invalidate
  cleanly and pick up the new diagrams.html on next activate.

### Changed

- **`Makefile sync-docs`** + **`scripts/check-docs-sync.sh`** —
  rsync include lists extended to `*.png`, `*.svg`, `*.jpg`, `*.gif`
  so embedded image assets stay in sync with `docs/` and the
  pre-commit / CI guards keep working. Groundwork for the BL190
  howto screenshot rebuild (planned, not executed in this release).

### Added

- **`docs/plans/2026-04-26-bl190-howto-screenshot-rebuild.md`** — plan
  doc for the upcoming BL190 follow-up: ~15-20 PWA screenshots per
  how-to via puppeteer-core driving system Chrome, with seeded JSONL
  fixtures so captures don't burn LLM credits or pollute the
  operator store. Pending operator go-ahead in a future session.

## [5.0.4] - 2026-04-26

Patch — BL187 reopened and properly fixed. Stale-PWA service-worker
cache was holding installed clients on the pre-BL187 bottom-nav HTML
even after every daemon upgrade; operator confirmed on v5.0.3 that
the old "New" tab was still visible and the FAB was missing.

### Fixed

- **`internal/server/web/sw.js`** — app-shell (`/`, `/index.html`,
  `/app.js`, `/style.css`, `/manifest.json`, `/diagrams.html`,
  `/docs/*`) switched from cache-first to network-first with cache
  fallback. Installed PWAs now pick up new web assets the next
  time they're online; offline still serves the cached copy.
  `CACHE_NAME` bumped `datawatch-v2` → `datawatch-v5` so every
  existing install invalidates cleanly on next activate. Static
  binary-style assets (icons, fonts, xterm.css) stay cache-first.
- **BL187 audit correction** — the v4.8.12 audit ("no code change
  needed; HTML is already clean") was wrong because it stopped
  at the source. The bug lived in the service worker's caching
  strategy, not the markup. Closed-section entry updated.

## [5.0.3] - 2026-04-26

Patch — BL180 Phase 2 design questions ready + binary upload
backlog resolved.

### Added

- **`docs/plans/2026-04-26-bl180-phase2-ebpf-correlation.md`** —
  six structured design questions for the eBPF socket-tuple
  cross-correlation that closes the BL180 attribution loop:
  connection-direction, granularity, multi-caller envelope shape,
  attribution scope, localhost-vs-cross-host, Thor verification.
  Each question carries a recommendation. Operator alignment
  needed before implementing — the data shape (specifically the
  `Callers []CallerAttribution` envelope addition) needs sign-off.

### Closed (verification)

- **Binary upload backlog** — `gh release create v5.0.0` attached
  all five binaries + checksums cleanly. The historical
  v4.5.x–v4.8.x patches that shipped code-only stay code-only;
  the v5.0.0 ABI is identical so v5.0.0 binaries cover every
  operator.

## [5.0.2] - 2026-04-26

Patch — closes BL177 longer-term (eBPF artifact CI drift-check).

### Added

- **`.github/workflows/ebpf-gen-drift.yaml`** — runs on every push
  / PR that touches `internal/observer/ebpf/**`. Re-runs `make
  ebpf-gen` and fails when the committed `netprobe_*_bpfel.{go,o}`
  artifacts drift from `netprobe.bpf.c`. Forces operators to
  regenerate locally + commit the refreshed bytes; no silent BPF
  source/object skew.

## [5.0.1] - 2026-04-26

Patch — closes BL184 secondary (thinking-message UX) + BL173 task 1
(eBPF kprobe attach loader).

### Fixed

- **BL184 secondary** — opencode-acp `Thinking... (reason)` lines
  no longer render as a broken-feeling empty `<details>` element.
  The reason is shown inline as a visible italic line with a brain
  emoji; the bubble persists in chat history. ACP doesn't surface
  the actual chain-of-thought as a separate stream today; if/when
  it does, restoring the collapsible body is a one-line change to
  `chat-thinking-bubble`.

### Added

- **BL173 task 1 — eBPF kprobe attach loader** —
  `internal/observer/ebpf/loader_linux.go` wires the bpf2go-emitted
  `loadNetprobeObjects` + four kprobes (`tcp_sendmsg`,
  `udp_sendmsg`, `tcp_recvmsg`-return, `udp_recvmsg`-return) into a
  working `realLinuxKprobeProbe`. Per-pid TX/RX byte counters are
  read from the eBPF maps every observer tick.
- Pre-loads kernel BTF via `btf.LoadKernelSpec` (same as BL181's
  v1 fix) — no `CAP_SYS_PTRACE` requirement.
- Partial attach is non-fatal: if only `tcp_sendmsg` survives, the
  probe stays loaded with TX-only counters.
- `generatedAvailable` flipped to `true` (artifacts have shipped
  for both arches since v4.8.22).

### Unblocks

- **BL180 Phase 2** (eBPF socket-tuple `(client_pid, server_pid)`
  cross-correlation) is now structurally unblocked — the kprobe
  attach loader is wired. Phase 2 itself still needs the new probe
  programs + the cross-correlation logic; that's a separate
  feature.

## [5.0.0] - 2026-04-26

**Major release** — closes 24 backlog items across the v4.7.2 →
v4.9.3 stretch. The single in-flight design (BL191 — autonomous
PRD lifecycle) ships with a structured questions-and-options
document ready for the design conversation; everything else is
done or deferred to operator-side work.

Comprehensive cumulative release notes:
[`docs/plans/RELEASE-NOTES-v5.0.0.md`](docs/plans/RELEASE-NOTES-v5.0.0.md).

### Highlights

- **Cross-cluster federation foundation** (S14a, v4.8.0) — primary
  datawatch can register itself as a peer of a root primary;
  push-with-chain loop prevention; per-envelope source
  attribution.
- **Public container distribution to GHCR** (BL195, v4.8.22) —
  every `v*` tag pushes 8 multi-arch images and a `docker save`
  tarball for `stats-cluster`.
- **eBPF without `CAP_SYS_PTRACE`** (BL181, v4.8.21) — kernel BTF
  preload via `/sys/kernel/btf/vmlinux`; eBPF arm64 artifacts
  committed (BL177, v4.8.22) so Nvidia Thor / Apple Silicon
  operators get full eBPF.
- **Whisper backend factory** (BL189, v4.9.0) — local Python venv
  *or* OpenAI-compat HTTP (cloud OpenAI / OpenWebUI fronting
  ollama / faster-whisper-server / whisper.cpp).
- **Observer ollama runtime tap** (BL180 Phase 1, v4.9.1) —
  per-loaded-model envelopes from `/api/ps`. Phase 2 (eBPF socket-
  tuple correlation) waits on the kprobe attach loader.
- **Binary size shrink** (BL196, v4.8.17) — HTTP gzip middleware +
  `make cross` rebuilt with `-trimpath -s -w` + opt-in UPX.
- **Six-doc how-to suite** (BL190, v4.9.3) — autonomous-planning,
  cross-agent-memory, prd-dag-orchestrator, container-workers,
  pipeline-chaining, daemon-operations.
- **Doc-coverage audit** (BL192/BL193, v4.8.13–19) — added
  `docs/api/{voice,devices,sessions,memory}.md`, swept stale
  comparison tables across 5 docs.
- **Backlog discipline rules** added to AGENT.md (binary-build
  cadence, README marquee, backlog-refactor-each-release,
  container-maintenance audit) — v4.8.8 / v4.8.12.

### Open carry-overs

- **BL191** (autonomous PRD lifecycle) — design conversation
  pending; questions doc ready.
- **BL184 secondary** (thinking-message UX) — deferred.
- **BL180 Phase 2** (eBPF socket-tuple correlation) — depends on
  the in-progress kprobe attach loader.
- **BL173-followup** + **Binary upload backlog** — operator-side.

### Container images

This release uses the new `.github/workflows/containers.yaml`
pipeline (BL195) for the first time. On tag push, multi-arch
images are pushed to:

- `ghcr.io/dmz006/datawatch-parent-full:v5.0.0`
- `ghcr.io/dmz006/datawatch-agent-base:v5.0.0`
- `ghcr.io/dmz006/datawatch-agent-claude:v5.0.0`
- `ghcr.io/dmz006/datawatch-agent-opencode:v5.0.0`
- `ghcr.io/dmz006/datawatch-agent-aider:v5.0.0`
- `ghcr.io/dmz006/datawatch-agent-gemini:v5.0.0`
- `ghcr.io/dmz006/datawatch-validator:v5.0.0`
- `ghcr.io/dmz006/datawatch-stats-cluster:v5.0.0`

Plus a `docker save` tarball as a release asset:
`datawatch-stats-cluster-5.0.0-linux-amd64.tar.gz`.

## [4.9.3] - 2026-04-26

Patch — closes BL190 (how-to documentation suite).

### Added

- **`docs/howto/prd-dag-orchestrator.md`** — full walkthrough:
  create graph, plan, run, watch, inspect verdicts, resume after a
  block. Per-channel reach matrix.
- **`docs/howto/container-workers.md`** — full walkthrough:
  Project + Cluster Profiles, spawn worker, watch federated
  observer, validator attestation, termination + cleanup. Per-
  channel matrix.
- **`docs/howto/pipeline-chaining.md`** — full walkthrough: define
  pipeline, run, watch, inspect failed gate, resume from failed
  step. "When to use what" comparison vs. autonomous + orchestrator.
  Per-channel matrix.
- **`docs/howto/daemon-operations.md`** — day-two operator
  walkthroughs: start / stop / restart / upgrade / hot reload /
  diagnose / logs / runtime state / auto-restart. Per-channel
  matrix.

### Changed

- **`docs/howto/README.md`** — index lists all six how-tos.

### Deferred

- PWA screenshots for each how-to — operator-side asset capture
  task, not a code change. Queued for a v5.x patch when an
  operator captures the visuals.

## [4.9.2] - 2026-04-26

Patch — closes BL197 partial (chat-channel autonomous PRD parity).

### Audit findings (BL197)

- **CLI** ✓ — `datawatch autonomous {status, config, prd*}`.
- **REST** ✓ — 9 endpoints under `/api/autonomous`.
- **MCP** ✓ — 9 tools mirroring REST 1:1.
- **Chat / comms** ✗ — zero autonomous commands. **Now fixed.**
- **PWA** — only the config card (Settings → General → Autonomous);
  no PRD-lifecycle UI. **Deferred to BL191** since the operator-
  flagged gaps (review-and-edit gate, PRD library, decisions log,
  recursive view) all need the same UI surface.

### Added

- **Chat-channel autonomous PRD lifecycle** —
  `internal/router/sx2_parity.go::handleAutonomous`. Verbs:
  - `autonomous status`
  - `autonomous list`
  - `autonomous get <prd-id>`
  - `autonomous decompose <prd-id>`
  - `autonomous run <prd-id>`
  - `autonomous cancel <prd-id>`
  - `autonomous learnings`
  - `autonomous create <spec…>`
  - `prd …` is accepted as a shorter alias.
- **`internal/router/commands.go`** parser dispatches both
  `autonomous` and `prd` heads to the new `CmdAutonomous`. Bare
  `autonomous` / `prd` returns a help-text reply.
- **`internal/router/router.go`** dispatch wired.
- **`internal/router/autonomous_test.go`** — verb-coverage parser
  tests + `prd` alias.

## [4.9.1] - 2026-04-26

Patch — BL180 Phase 1 (observer many-to-one LLM attribution via the
ollama runtime tap).

### Added

- **Ollama runtime tap** (`internal/observer/ollama_tap.go`) — when
  `observer.ollama_tap.endpoint` is configured (e.g.
  `http://localhost:11434`), the observer polls ollama's `/api/ps`
  every 5 s and emits one envelope per loaded model with
  `Caller="<model>"`, `CallerKind="ollama_model"`, and
  `GPUMemBytes` populated from the ollama `size_vram` field.
  Operators see per-model GPU/RAM attribution on hosts that share
  one ollama between multiple LLM clients (openwebui + opencode +
  claude in parallel).
- **`observer.Envelope.Caller` + `Envelope.CallerKind`** — new
  optional fields. Phase 1 fills them from the ollama tap; Phase 2
  (eBPF socket-tuple correlation) will fill them from
  `(client_pid, server_pid)` pairs once the kprobe attach is wired.
- **`observer.OllamaTapCfg{Endpoint}`** + **`config.ObserverOllamaTapConfig`** —
  YAML/REST/MCP-reachable config plumbing.
- **4 new tests** (`internal/observer/ollama_tap_test.go`):
  /api/ps decode, HTTP-error surfacing, disabled-on-empty-endpoint,
  Start/cancel goroutine cleanup.

### Changed

- **`internal/observer/collector.go`** — tick now appends ollama
  tap envelopes to the per-tick list when the tap is enabled.
  Tap goroutine is owned by the collector lifecycle.

### Phase 2 still open

eBPF socket-tuple `(client_pid, server_pid)` cross-correlation —
attribution for any TCP-talking client, not just ollama. Depends
on the kprobe attach (BL173 task 1, in progress) and the BL177
arm64 artifacts (just shipped) for testing on Thor.

## [4.9.0] - 2026-04-26

Minor — closes BL189 (Whisper backend factory: local + OpenAI-
compatible HTTP).

### Added

- **OpenAI-compatible HTTP transcribe backend**
  (`internal/transcribe/openai_compat.go`) — POSTs the audio file as
  `multipart/form-data` to `<endpoint>/audio/transcriptions` and
  parses the JSON `{"text":"…"}` response. Works against:
  - cloud OpenAI (`https://api.openai.com/v1`)
  - OpenWebUI fronting any backend incl. ollama
    (`http://<host>/api/v1`)
  - faster-whisper-server / whisper.cpp server-mode / any other
    OpenAI-compat host.
  Bare ollama doesn't ship audio — operators wanting the
  "ollama-flavoured" path point this at OpenWebUI.
- **`internal/transcribe/factory.go`** — `NewFromConfig(BackendConfig)`
  routes to `whisper` (default; existing local Python venv) or
  `openai` / `openai_compat` (new HTTP path).
- **New config fields** on `whisper`:
  - `whisper.backend` — `whisper` (default) | `openai` | `openai_compat`
  - `whisper.endpoint` — base URL for HTTP backends
  - `whisper.api_key` — bearer for HTTP backends
- **`docs/api/voice.md`** updated with the new config block + the
  reach matrix for each backend choice.
- **`cmd/datawatch/main.go`** — voice transcriber init swapped to
  `transcribePkg.NewFromConfig`. Default behaviour unchanged for
  existing operators.

### Tests

- 4 new tests in `internal/transcribe/openai_compat_test.go`:
  multipart shape (correct file, model, language, auth),
  HTTP-error surfacing, auth-omitted-when-empty, and factory
  routing.

## [4.8.23] - 2026-04-26

Patch — closes BL185 (rate-limit auto-detect for the new claude
format).

### Fixed

- **BL185** — investigation revealed the auto-detect + auto-select-1
  + schedule-resume machinery already exists in
  `internal/session/manager.go` (since v3.6.0/BL30). The miss was
  the **parser**: the newer claude format `"resets 10pm
  (America/New_York)"` (without `at`) wasn't matched. Added
  `"resets "` to the marker list ahead of the existing
  `"resets at "` / `"reset at "` / `"will reset at "` markers in
  `parseRateLimitResetTime`. Order matters: more-specific markers
  win first, broad `"resets "` falls through to whichever family
  picks up the time form.
- **`internal/session/ratelimit_parser_test.go`** — new
  `TestParseRateLimitResetTime_ClaudeNewFormat` covers operator's
  exact repro string + 3 variants (with/without zone, 24h, 12h).
  Existing tests still pass.

## [4.8.22] - 2026-04-26

Patch — closes BL177 (eBPF arm64 artifacts) + BL195 (public
container distribution).

### Added (BL177 closed)

- **Per-arch vmlinux.h tree** — `internal/observer/ebpf/vmlinux_amd64/vmlinux.h`
  (locally BTF-dumped) and `internal/observer/ebpf/vmlinux_arm64/vmlinux.h`
  (sourced from libbpf/vmlinux.h community-maintained per-arch dumps).
- **`netprobe.go`** `//go:generate` split into two lines — one per
  arch — each with its own `-cflags -I` pointing at the matching
  vmlinux dir.
- **Both arch artifacts committed**: `netprobe_x86_bpfel.{go,o}`
  (amd64) + `netprobe_arm64_bpfel.{go,o}` (arm64). Same Go symbol
  names; build tags isolate compilation per arch.
- Cross-build verified: `GOOS=linux GOARCH=arm64 go build` builds
  cleanly. Operators on arm64 (Nvidia Thor, Apple Silicon, Pi 5)
  no longer fall back to the noop probe.

### Added (BL195 closed)

- **`.github/workflows/containers.yaml`** — runs on every `v*` tag
  push. Matrix builds + pushes 8 operator-facing images to GHCR
  (`ghcr.io/dmz006/datawatch-{parent-full,agent-base,agent-claude,agent-opencode,agent-aider,agent-gemini,validator,stats-cluster}`),
  each tagged `:VERSION` and `:latest`. Multi-arch
  (linux/amd64 + linux/arm64). Auth via the built-in
  `${{ secrets.GITHUB_TOKEN }}` — no extra secrets.
- **`stats-cluster` air-gap tarball** — second job pulls the
  just-pushed image and `docker save | gzip`'s it as a release
  asset (`datawatch-stats-cluster-<version>-linux-amd64.tar.gz`).
  Operators install via `docker load -i …`.
- **`make containers-push VERSION=…`** + **`make containers-tarball VERSION=…`** —
  local mirrors of the same pipeline.
- **Removed** the old `.github/workflows/container.yaml.disabled` —
  superseded by the new pipeline.

## [4.8.21] - 2026-04-26

Patch — closes BL181 (eBPF /proc/self/mem permission).

### Fixed

- **BL181** — `internal/stats/ebpf_collector.go` now pre-loads
  kernel BTF via `btf.LoadKernelSpec()` (which reads
  `/sys/kernel/btf/vmlinux`, world-readable on every BTF-shipping
  kernel) and passes it as
  `CollectionOptions{Programs: ProgramOptions{KernelTypes: kspec}}`.
  This bypasses the cilium/ebpf default detection that reads
  `/proc/self/mem` and required `CAP_SYS_PTRACE` ambient on the
  systemd unit. Operators no longer need to add a third capability
  beyond `CAP_BPF` + `CAP_PERFMON`.
- **`internal/stats/ebpf_btf_test.go`** — new test
  `TestKernelBTFLoadable` exercises the BTF discovery path on
  every CI run; skip cleanly when BTF isn't shipped in the test
  kernel.

## [4.8.20] - 2026-04-26

Patch — closes BL184 primary (opencode-acp recognition lag);
thinking-message UX deferred.

### Fixed

- **BL184 primary** — opencode-acp chat-mode recognition lag is
  fixed. Extracted `markChannelReadyIfDetected(sessionId, lines)`
  helper in `internal/server/web/app.js` that runs unconditionally
  on every `output` and `chat_message` WS event. Previously the
  readiness scan only ran when the connection-banner element was
  already in the DOM — which missed the case where the operator
  opens the session right after the ACP became ready (the
  banner-render path doesn't see the historical buffer
  consistently). Cached `state.channelReady[sessionId]` is now
  correct on the next render even if the banner wasn't on screen
  at detection time. The banner is also removed in-place when
  detection wins, so a stale spinner doesn't persist.

### Deferred

- **BL184 secondary (thinking-message UX)** — opencode-acp emits
  thinking text right before the response and the existing
  collapsible-thinking regex (`^Thinking\.\.\.\s*\(reason\)$`)
  doesn't match. Needs either the daemon to wrap thinking blocks
  in `<thinking>…</thinking>` (already supported in
  `formatChatContent`) or a relaxed regex. Tracked under BL184
  remaining.

## [4.8.19] - 2026-04-26

Patch — closes BL192 (doc-coverage audit) with three new operator
references.

### Added

- **`docs/api/voice.md`** — voice transcription operator reference
  (REST + MCP + CLI + chat reachability + config).
- **`docs/api/devices.md`** — device push registry reference.
- **`docs/api/sessions.md`** — full session lifecycle reference,
  state machine, every endpoint + MCP tool + CLI command + chat
  shortcut, plus `session.*` config block.

### Changed

- **`docs/architecture-overview.md`** — Sessions / Voice
  transcription / Device push registry / Episodic memory rows now
  point at the new `docs/api/<feature>.md` operator references
  first, with architecture/flow diagrams as secondary links.

## [4.8.18] - 2026-04-26

Patch — closes BL175 (docs duplication strategy) + BL198 + BL199
(operator-flagged mobile diagrams.html bugs) + files BL197.

### Fixed

- **BL198** — `/diagrams.html` mobile drawer no longer leaves a
  visible strip when collapsed, and the diagram no longer
  disappears on Chrome mobile / installed PWA. The collapsed
  aside now sets `visibility: hidden`, drops `pointer-events`,
  kills the box-shadow; the main element claims full width with
  `min-width: 0` + `overflow-x: auto`; the diagram-shell gets a
  `min-height: 240px` floor on mobile so the diagram always has
  guaranteed room.
- **BL199** — `/diagrams.html` header cleanup: dropped the
  redundant "← back to web UI" link; **API spec** + **MCP tools**
  links now open in the current browser tab (not new tab/window),
  per operator preference.

### Added (BL175 closed)

Operator approved the recommendation. Hybrid (a) keep-rsync +
(c) skip-manifest:

- **`docs/_embed_skip.txt`** — manifest of files to never embed.
  Empty today; single-line addition is enough to mark a future
  operator-internal plan as "private".
- **`make sync-docs`** rebuilt to honour the skip manifest.
- **`scripts/check-docs-sync.sh`** — fails when the embedded copy
  drifts from `docs/`. rsync-dry-run, no Go build needed.
- **`hooks/pre-commit-docs-sync`** — installable pre-commit hook
  (`ln -sf ../../hooks/pre-commit-docs-sync .git/hooks/pre-commit`).
- **`.github/workflows/docs-sync.yaml`** — CI guard mirroring the
  hook on every push / PR.

### Backlog filed

- **BL197** — Autonomous planning surface parity audit. Operator
  flag: today the autonomous PRD lifecycle has CLI + REST + MCP +
  chat surfaces; need to confirm every action is also reachable
  from the PWA Settings → Autonomous card and from comm-channel
  shortcuts. Per AGENT.md "no hard-coded configs / full channel
  parity" rule. Distinct from BL191 (which is a design
  conversation about *new* lifecycle features); BL197 is the
  parity audit on what's already shipped.

## [4.8.17] - 2026-04-26

Patch — closes BL196 (binary size).

### Added

- **HTTP gzip middleware** (`internal/server/server.go`) — wraps the
  embedded static-file server. Text payloads (JS, CSS, Markdown,
  JSON, SVG) ride `Content-Encoding: gzip` when the client supports
  it; binary assets (PNG/JPG/font/etc.) skip the wrapper. Verified
  on the live daemon: `app.js` 372 KB → 90 KB on the wire (~76 %
  reduction). Uses a `sync.Pool` of `gzip.Writer`s so per-request
  allocation stays flat.
- **Cross-compile pipeline shrink** (`Makefile cross` target):
  - Added `-trimpath -ldflags="-s -w …"` to all five binary
    targets (linux/darwin amd64+arm64, windows amd64). Strips
    debug info + absolute build paths; typically 30-40 % smaller
    binary at zero runtime cost.
  - Added an **opt-in UPX pack step** that runs only if `upx` is
    on the PATH. Packs linux + windows binaries (`--best --lzma`);
    skips macOS Mach-O (UPX has known notarization issues there).
    Failure to pack any single binary is non-fatal.
  - Operators install `upx` (`apt install upx-ucl` on Debian/Ubuntu)
    once for the ~50 % release-binary shrink; otherwise the build
    just emits "skipping pack step" and continues.

### Discipline (AGENT.md rule reaffirmed)

- **Version sync** — `internal/server/api.go` was stuck at
  `4.7.1` because all my recent patches only updated
  `cmd/datawatch/main.go`. Live daemons were correct at build time
  (the Makefile's `LDFLAGS` overrides both via `-X`), but the
  source files drifted. Re-synced; future patches update both per
  AGENT.md "the version string lives in two places".

## [4.8.16] - 2026-04-26

Patch — BL192 partial (memory operator doc) + BL190 progress
(cross-agent memory how-to) + plan-attribution memory rows
corrected.

### Added

- **`docs/api/memory.md`** — operator reference for the memory
  subsystem. Covers REST + MCP + CLI + chat reachability, the
  full spatial vocabulary (wing / room / hall / closet / drawer),
  tunnels, the 4-layer wake-up stack, and namespace + sharing
  rules. Was missing entirely despite a 12-endpoint REST surface.
- **`docs/howto/cross-agent-memory.md`** — full walkthrough:
  spawn two agents, agent A writes decisions to its own diary
  + a closet/drawer chain, agent B reads via the shared room
  through mutual `memory.shared_with` opt-in, find tunnels
  across wings, wake-up stack auto-injection. Replaces the stub
  shipped in v4.8.12.

### Fixed (plan-attribution accuracy)

- **`docs/plan-attribution.md`** — three mempalace-comparison
  rows were stale and said "not implemented" when the code has
  full implementations:
  - **Closets + drawers** (BL99) — small summary embeddings
    pointing at verbatim drawers. `internal/memory/closets_drawers.go`.
  - **Agent diaries** (BL97) — per-agent canonical
    `wing = "agent-<id>"` with hall-typed entries.
    `internal/memory/agent_diary.go`.
  - **KG contradiction detection** (BL98) — functional-predicate
    slice of mempalace's `fact_checker`.
    `internal/memory/kg_contradictions.go`.
  Both the "compares to" table and the "Directly included" table
  now reflect the actual implementations. Operator confirmed
  these spatial structures shipped.

### BL192 progress

- ✅ `docs/api/memory.md` (this release) — closes the most
  visible doc-coverage gap (operator-flagged: "episodic memory").
- ⏳ Per-row audit of `docs/architecture-overview.md` — confirm
  every feature has a `docs/api/<feature>.md` or a direct plan
  link. Pending.

### BL190 progress

- ✅ `docs/howto/autonomous-planning.md` (v4.8.12)
- ✅ `docs/howto/cross-agent-memory.md` (this release)
- ⏳ Remaining stubs to expand: PRD-DAG, container workers,
  pipeline chaining.

## [4.8.15] - 2026-04-26

Patch — **BL193 closed**: final three docs in the audit are clean.

### Changed

- **`docs/messaging-backends.md`** — verified Backend Comparison +
  Command Support + Feature Parity tables against current code.
  Added missing **DNS Channel (Covert)** row to the Backend
  Overview table (it was documented further down under
  "DNS Channel (Covert)" but absent from the top-level table).
  Cross-checks confirmed: Slack `SendMarkdown` /
  `SendWithButtons` / `UploadFile`, Discord `SendMarkdown` /
  `SendWithButtons` (Components) / `Files: []*discordgo.File`
  / `MessageThreadStartComplex`, Telegram `SendMarkdown` +
  `ReplyToMessageID` (threading), Matrix no rich-format helpers
  (table correctly shows —).
- **`docs/architecture-overview.md`** — swept of internal tracker
  IDs per the AGENT.md rule. Stripped: `F17`, `F18`, `F19`, `F10`
  (multiple), `BL103`, `BL85`, `BL24+BL25`, `BL33`, `B41`, `B39`,
  `BL117`, `BL171`, `BL174`, "sprints 3-5", "sprints 4-7". Headings
  and table cells now read in operator language; the version
  ship-stamps (e.g. "shipped v4.0.0") are kept where they're
  load-bearing for understanding what's available.
- **`docs/data-flow.md`** — flow-index table titles cleaned:
  "F10 Agent Spawn Flow" → "Agent Spawn Flow"; "BL117 PRD-DAG
  Orchestrator Flow" → "PRD-DAG Orchestrator Flow". Description
  cells lost their `BL24` and `(sprints 3-5)` tags too.

### BL193 progress

- ✅ `docs/llm-backends.md` (v4.8.13)
- ✅ `docs/api-mcp-mapping.md` (v4.8.14)
- ✅ `docs/messaging-backends.md` (this release)
- ✅ `docs/architecture-overview.md` (this release)
- ✅ `docs/data-flow.md` (this release)

**BL193 fully closed.**

## [4.8.14] - 2026-04-26

Patch — BL193 progress: `docs/api-mcp-mapping.md` swept of
internal IDs (per the AGENT.md "no internal tracker IDs in
user-facing docs" rule) and section headers tightened.

### Changed

- **Section headers** in `docs/api-mcp-mapping.md` had
  parenthetical internal IDs (`(BL117 — v4.0.0)`,
  `(BL172 — v4.4.0+)`, `(BL24+BL25 — v3.10.0)`, etc.). All
  stripped — the headers now read as feature names only.
- **Renamed ambiguous-after-strip headers**:
  - "Profiles" → "Profiles (Project + Cluster)"
  - "Singletons" → "Singleton operations"
  - "Voice + Channel" → "Voice + Channel bridge"
- **Per-row Notes** in the body table cleaned of `BL94`, `BL93`,
  `BL29`, `BL40`, `BL35`, `BL34`, `BL42`, `BL9`, `BL12`, `BL17`,
  `BL37`, `BL69`, `BL107`, `F10`, `S13`, "Shape A / B / C"
  internal references. Each note now describes what the endpoint
  does in operator language.
- **Coverage summary table** rebuilt: `Cost (BL6)` →
  `Cost tracking`, `Plugins (BL33)` → `Plugin framework`,
  `Orchestrator (BL117)` → `PRD-DAG orchestrator`, etc. Numbers
  unchanged.

### BL193 progress

`docs/llm-backends.md` (v4.8.13) and `docs/api-mcp-mapping.md`
(this release) complete. Remaining audit: `messaging-backends.md`,
`docs/architecture-overview.md`, `docs/data-flow.md`.

## [4.8.13] - 2026-04-26

Patch — BL193 partial: `docs/llm-backends.md` comparison rewrite
from the source-of-truth.

### Changed

- **`docs/llm-backends.md` Backend Comparison table** rewritten
  from each backend's `SupportsInteractiveInput()`. The previous
  table was incorrect (operator-flagged): `shell` was listed as
  No when the code returns true, several backends shipped two
  variants (regular task launcher + interactive chat/conversation
  mode) but only one was visible, and the explanatory paragraph
  said "Only `claude-code` supports interactive input" while the
  table contradicted itself. Now correctly lists 5 interactive
  backends — `claude-code`, `shell`, `opencode-acp`,
  `openwebui` (chat mode), `ollama` (chat mode) — and 6
  single-shot variants. The Interactive Input Support paragraph
  reflects the same.
- **BL193 progress** — `docs/llm-backends.md` complete; remaining
  files to audit: `messaging-backends.md`, `api-mcp-mapping.md`,
  `architecture-overview.md`, `data-flow.md`.

## [4.8.12] - 2026-04-26

Patch — closes BL187 (audit-only) + lands the BL190 how-to docs
scaffold + first walkthrough.

### Added

- **`docs/howto/`** — new directory with practical walkthroughs.
  Each guide follows the same template: base requirements →
  setup → walkthrough with commands + expected output. Linked
  from `docs/README.md` Getting Started and the project README
  marquee.
- **`docs/howto/autonomous-planning.md`** — full end-to-end
  walkthrough: enable autonomous, submit a spec, decompose,
  inspect the LLM's stories before running, run + watch, inspect
  verifier verdicts, plus a channel-reachability matrix
  (CLI / REST / MCP / chat / PWA).
- **`docs/howto/{prd-dag-orchestrator,container-workers,
  pipeline-chaining,cross-agent-memory}.md`** — stubs that link
  to the existing reference docs / design plans / flow diagrams.
  These will be expanded in follow-up commits.

### Closed (audit)

- **BL187** — "Drop the PWA New tab; FAB-only modal" — the audit
  shows the bottom nav already carries only Sessions / Alerts /
  Settings (no New tab); the floating `+` FAB
  (`#newSessionFab`) opens a full-screen modal-like view via
  `openNewSessionModal()` that returns to the previous tab on
  close. Pre-existing implementation already satisfies the
  request — no code change needed.

### Documented retroactively

- **BL194** — "MCP tools" link in `/diagrams.html` header
  (alongside the existing "API spec" link) — actually shipped in
  v4.8.11; recorded under Recently closed for the audit trail.

### Rules consolidation (AGENT.md is the single source of truth)

- **`docs/plans/README.md` "# Rules" block replaced** with a
  pointer to `/AGENT.md`. The duplicated rules are gone; AGENT.md
  is the canonical source and matches what already lives in
  operator memory.
- **`AGENT.md` § Release vs Patch Discipline** gained two
  subsections:
  - **Binary-build cadence** — patch releases still build the
    host-arch binary (so `datawatch restart` picks up the patch
    on the dev workstation) but skip `make cross` + binary
    asset attachment. Minor / major releases attach all five.
  - **Release-discipline rules** (README marquee + backlog
    refactor each release) and **Container maintenance** moved
    here from `docs/plans/README.md`.

### Backlog filed

- **BL195** — Public container image distribution. Operator
  question: can we push images to a public registry (ghcr.io /
  Docker Hub) or attach `docker save` tarballs to GH releases so
  operators can `docker pull` / `docker load` directly? Today's
  Makefile pushes to `harbor.dmzs.com` (private). Options
  outlined for operator decision.

## [4.8.11] - 2026-04-25

Patch — three operator-flagged UX wins on the docs viewer +
Settings inline doc links.

### Fixed

- **Inline docs links now open in a new tab** — `docsLink()`
  helper (`internal/server/web/app.js`) emits
  `target="_blank" rel="noopener noreferrer"` so clicking a
  Settings → docs chip doesn't take the operator out of the
  Settings view.
- **`/diagrams.html` — markdown render no longer fails on certain
  files** (operator repro: `data-flow.md`). The marked v12
  upgrade dropped support for the `mangle` and `headerIds`
  options that the call site was passing inline; the API
  silently tolerates unknown options at runtime in some envs but
  threw in others. Removed the obsolete options + wrapped
  `marked.parse` in a try/catch with a visible error fallback
  instead of silent failure.
- **Settings link label** — "Architecture diagrams" →
  "System documentation &amp; diagrams" (matches the v4.8.5
  retitling of `/diagrams.html` itself, since prose now renders
  alongside diagrams).

### Added

- **`/diagrams.html` header gains an "MCP tools" link** in the
  upper right, alongside the existing "API spec" link, pointing
  at `/api/mcp/docs`.

## [4.8.10] - 2026-04-25

Patch — closes BL178 (PWA "view last response" stale) + files
BL192/BL193 from operator's docs-coverage feedback.

### Fixed

- **BL178** — `showResponseViewer` (`internal/server/web/app.js`)
  always fetches the live response from
  `/api/sessions/response`. The cached value (populated from
  WebSocket `response` events on session completion, never
  invalidated) is rendered first with an `(updating…)` badge for
  instant feedback, then **patched in place** when the fresh
  fetch returns — no modal teardown, no scroll-position loss.
  The `state.lastResponse[sessionId]` cache is also overwritten
  with the fresh value so the next click is immediate. Repro on
  session 787e: a multi-day-old response was sticking; gone now.

### Backlog filed

- **BL192** — Doc-coverage gap: features without standalone
  operator docs (e.g. episodic memory). Either link from
  `docs/api/*` to the underlying plan, or write a new `docs/api/*`
  page derived from the plan. Per-feature checklist needed.
- **BL193** — Doc mapping + comparison tables stale. Operator
  flagged `docs/llm-backends.md` comparison incorrect; same audit
  needed for `docs/messaging-backends.md`,
  `docs/api-mcp-mapping.md`, `docs/architecture-overview.md`,
  `docs/data-flow.md`. Process: read code/types → regenerate
  table from source-of-truth → diff-review.

## [4.8.9] - 2026-04-25

Patch — closes BL176 (RTK upgrade-string sweep) + BL188 (attribution
guide refresh).

### Fixed

- **BL176** — three surfaces fixed:
  - **PWA RTK card** (`internal/server/web/app.js`) — the
    click-to-copy chip now writes the upstream install one-liner
    (`curl -fsSL https://raw.githubusercontent.com/rtk-ai/rtk/refs/heads/master/install.sh | sh`)
    instead of the legacy `rtk update`. The hover/title text and
    "copied!" feedback both reflect the new command.
  - **OpenAPI** (`docs/api/openapi.yaml`) — `POST /api/rtk/update`
    description rewritten to point at the install one-liner.
  - **Chat-help table** (`docs/flow/rtk-auto-update-flow.md`) —
    Signal/Telegram row now shows the one-liner.
  - The CLI fallback was already fixed in v4.0.7.
- **BL188** — `docs/plan-attribution.md` refreshed:
  - nightwire credit expanded to include the PRD-decomposition
    workflow patterns referenced in BL24.
  - New "Researched and skipped" subsection credits Aperant as
    prior art (AGPL-incompatible / Electron / sits on the same
    claude-code layer; worktree-isolation + self-QA ideas kept
    in BL24 roadmap).
  - Explicit operator-action note inviting per-feature additions
    for BL117 / BL33 / F10 / BL173 — the **Source:** rule for
    new plan docs is already in place; this catches historical
    gaps.

## [4.8.8] - 2026-04-25

Patch — closes BL182, BL183, BL186 and adds two release-discipline
rules to the backlog.

### Fixed

- **BL182** — PWA "Input Required" yellow popup now shows up while
  the operator is already inside the session view. Previously the
  banner was only painted by `renderSessionDetail()` (called on
  navigation); state-change WebSocket events updated `state.sessions`
  but didn't repaint. Extracted the banner build into
  `buildNeedsInputBannerHTML(sess, sessionId)` and added
  `refreshNeedsInputBanner(sessionId)` which patches the new
  `#needsInputSlot` element in place. Wired both `updateSession`
  and `dismissNeedsInputBanner` to call it. Closing the popup now
  re-evaluates state on the spot — no more back-out/re-enter dance.
- **BL183** — Orphan-cleanup affordance restored to a stable
  location. The "Kill All Orphaned" button + tmux session list
  is now **always visible** in Settings → Monitor → System
  Statistics (previously hidden when the orphan count was zero,
  so operators couldn't find it on a clean system). Shows
  "(none)" + a disabled button when there's nothing to clean.
- **BL186** — Sweep code-shipped strings for internal IDs in CLI
  long help / Println output:
  - `cmd/datawatch/cli_observer.go` — agent + peer subcommand
    Short and Long strings replaced "F10 / S13 / Shape A / Shape
    B / Shape C" with operator-language wording.
  - `cmd/datawatch/main.go:8040` — eBPF setup epilogue replaced
    "Shape C (k8s / docker cluster container)" with
    "Cluster-container deployments".
  - Test `TestObserverPeerCmd_LongHelpMentionsBothPeerKinds` updated
    to assert "standalone" + "cluster" instead of "Shape B" /
    "Shape C".

### Backlog filed

- **BL190** — How-to documentation suite (autonomous planning · PRD-DAG · containers · pipeline + session chaining · cross-agent memory) under `docs/howto/`. Filed from operator's Unclassified note 2026-04-25.

### Added

- **Two release-discipline rules** in `docs/plans/README.md`:
  - "README.md must reflect the current release" — every release
    commit updates the marquee version line and refreshes the
    "Highlights since vN.0.0" bullets if anything notable shipped.
  - "Backlog refactor each release" — each release commit also
    clears `## Unclassified` into BL entries, marks closed items
    as `✅ Closed in vX.Y.Z`, and confirms `#### Open` is only
    actually-open work.
- **README.md marquee** updated to v4.8.8 + a new "Highlights
  since v4.0.0" section listing federation, observer enrichment,
  agent peers, slim containers, and PWA refinements.

## [4.8.7] - 2026-04-25

Patch — operator request: inline doc links in Settings (with a
toggle to hide them) + the proxy-flow recursive variant the user
asked for + a proper backlog refactor.

### Added

- **Settings → General → "Show inline doc links"** toggle
  (per-browser, persists in localStorage). When ON, a small
  `docs` chip appears next to selected Settings section headers
  (Proxy Resilience, Communication Configuration, LLM
  Configuration, System Statistics) and deep-links into
  `/diagrams.html#docs/...`. Default ON. Rendering helper
  `docsLink(path, label?)` honors the toggle so additional
  sections can opt in by passing a third arg to
  `settingsSectionHeader(key, title, docsPath)`.
- **`/diagrams.html` deep-linking** — the viewer now honors a
  `#docs/<path>.md` URL fragment and a `hashchange` listener so
  external pages can jump straight to a specific doc.
- **Proxy-flow recursive variant** (`docs/flow/proxy-flow.md`) —
  added a Mermaid flow showing edge → mid → origin chain with
  per-hop loop guard via `X-Datawatch-Request-ID` LRU; covers
  bastion fan-in, DR mirror, and federated-PRD use cases.

### Changed

- **Backlog refactor** (`docs/plans/README.md`) — Unclassified
  cleared (all items pulled into BL### entries); Open Bugs / Open
  Features sections now point at numbered backlog rows instead of
  free-form prose; the big "Open" table split into **Open** /
  **Recently closed** / **Frozen / external** subsections so
  recently-shipped items don't crowd actually-open work.
- **New backlog entries** filed from Open Bugs / Features prose:
  - **BL184** — opencode-acp chat-mode recognition lag +
    collapsible thinking messages.
  - **BL185** — rate-limit auto-detect + scheduled wait
    (auto-select option 1 + insert a `schedule.Schedule` for the
    parsed reset time).
  - **BL186** — sweep code-shipped strings (log lines, warn
    messages, setup output) for internal IDs (sprint / version /
    BL / B / F refs).
  - **BL187** — drop the PWA "New" tab, FAB-only modal (mobile
    parity).
  - **BL188** — inspiration attribution sweep beyond
    nightwire/aperant.
  - **BL189** — Whisper integration with local whisper or
    ollama / openwebui.

### Notes on rules

- The new "Show inline doc links" toggle is intentionally a
  per-browser localStorage preference (matches existing
  `cs_filters_collapsed`, `cs_show_history`, etc.) — not a
  daemon-wide YAML/REST/MCP/CLI configuration. The "no
  hard-coded config" rule applies to *server behavior*; per-user
  UI preferences belong in the browser. If operators want
  server-wide control later, a `server.pwa.show_docs_links`
  default could be added without breaking the existing toggle.

## [4.8.6] - 2026-04-25

Patch — closes BL179. Search icon now lives in the top header bar
next to the daemon-status light, reachable without scrolling.

### Changed

- **Header search-icon** (`internal/server/web/index.html`) — added
  a magnifying-glass button to the header, immediately to the left
  of the daemon-status dot. Visible only when the active view is
  Sessions; hidden on Alerts / Settings / New / session-detail
  (toggled in the `navigate()` handler).
- **Click handler** (`internal/server/web/app.js`) — wired in the
  `DOMContentLoaded` block; toggles `state._filtersCollapsed`,
  persists to localStorage, and re-renders when the active view is
  Sessions.
- **In-card duplicate removed** — the previous in-card icon (added
  in v4.7.1 / B44) is gone; the toolbar row collapses entirely
  when filters are hidden so the session list takes the full
  window.

### Closed

- **BL179** — PWA sessions search-icon location.

## [4.8.5] - 2026-04-25

Patch — `/diagrams.html` upgrades to address operator feedback:
"should be rotatable on cell phone, menu collapsible, are all docs
available in this view?"

### Added

- **Sidebar collapse toggle** in the header (☰ button) — hides the
  file list to give the diagram full width on any screen size.
- **Mobile + portrait responsive layout** (CSS media query at
  ≤ 820 px or `orientation: portrait`): aside becomes a slide-out
  drawer, header gives the diagram its full width by default,
  rotation works because the layout doesn't lock to landscape.
- **Markdown body always renders** below diagrams via marked.js
  (CDN). Previously, files without a Mermaid block showed "No
  Mermaid diagrams in this file" + a GitHub link; now the prose
  appears in the viewer itself, with the GitHub link kept as a
  footer for the source.
- **Title updated** to "datawatch — Docs &amp; Diagrams" since the
  view is no longer diagrams-only.

### Changed

- Body uses `100dvh` so mobile browser chrome doesn't clip the
  viewport.

## [4.8.4] - 2026-04-25

Patch — adds Mermaid diagrams to the orchestrator + observer flow
docs so they render in `/diagrams.html` (operator: "orchestrator
flow doesn't have diagrams"). Also files BL181.

### Changed

- **`docs/flow/orchestrator-flow.md`** — replaced ASCII art with a
  Mermaid `flowchart TD` covering the operator → Runner → PRDRunFn /
  GuardrailFn → persistence → graph status path. Renders in
  `/diagrams.html` instead of the old "No Mermaid diagrams in this
  file" placeholder.
- **`docs/flow/observer-flow.md`** — same treatment: ASCII tick
  pipeline replaced with a Mermaid `flowchart TD` covering
  `/proc/` walk + session attribution + envelope classifier +
  host probes + StatsResponse v2 assembly + REST/WS/peer fanout.

### Backlog filed

- **BL181** — `datawatch setup ebpf` reports success but daemon
  startup logs `eBPF load failed: detecting kernel version:
  opening mem: open /proc/self/mem: permission denied` (cilium/ebpf
  reads `/proc/self/mem` for kernel BTF detection; needs
  `CAP_SYS_PTRACE` ambient or a switch to the
  `/sys/kernel/btf/vmlinux` discovery path). Repro on dev
  workstation foreground daemon. Filed 2026-04-25.

## [4.8.3] - 2026-04-25

Patch — diagrams and flow docs refactor (operator directive: "diagrams
should not have F10 / BL / sprint references — they're documenting
features, not when/where they were done").

### Changed

- **Renamed flow files** to feature-named slugs:
  - `docs/flow/bl117-orchestrator-flow.md` → `orchestrator-flow.md`
  - `docs/flow/bl171-observer-flow.md` → `observer-flow.md`
  - `docs/flow/f10-agent-spawn-flow.md` → `agent-spawn-flow.md`
- **Cleaned diagram + flow content** of internal IDs: removed
  `BL`/`F`/`Sprint`/`Shape A/B/C` from titles, body prose, and
  ASCII/mermaid diagram boxes across 8 flow docs (orchestrator,
  observer, agent-spawn, channel-mode, memory-recall, plugin-
  invocation, voice-transcribe, rtk-auto-update).
- **Updated all referrers** to the new filenames:
  `docs/README.md`, `docs/data-flow.md`, `docs/architecture-overview.md`,
  `docs/plans/2026-04-22-bl171-datawatch-observer.md`,
  `internal/server/web/diagrams.html`.
- Embedded copies under `internal/server/web/docs/` regenerated via
  `make sync-docs`.

### Backlog filed

- **BL180** — Observer many-to-one LLM attribution. When ollama
  serves both openwebui and opencode on the same host, today's
  envelope rolls all GPU/CPU/net under "ollama"; want call-graph
  attribution back to the *requesting* LLM (openwebui vs. opencode
  request) so cost + load can be split correctly. Filed
  2026-04-25 from operator's Nvidia Thor + ollama-shared-host
  scenario. Operator decision pending.

### Verified (no code change)

- **`installed plugins does not list datawatch-observer`** — bug
  was filed while running v4.7.x. Live `/api/plugins` against the
  v4.8.2 daemon returns `{"native":[{…datawatch-observer…}]}`
  correctly; the native-plugin registration in `cmd/datawatch/main.go`
  has been in place since v4.1.0. PWA should pick it up on next
  Settings → Monitor view after the daemon restart.

## [4.8.2] - 2026-04-25

Patch — second sweep of the "no internal IDs in user-facing
strings" rule. Catches what v4.8.1 missed.

### Fixed

- **Profile editor** (`internal/server/web/app.js`) — the
  "Memory shared_with" field placeholder read "F10 S6.5: peer
  profiles must reciprocate". Replaced with "peer profiles must
  reciprocate".
- **Cluster nodes section** (`internal/server/web/app.js`) — the
  Settings → Monitor "Cluster nodes" header had a "(Shape C)"
  subscript. Removed; the rendered rows already show the source
  per-row when present.

## [4.8.1] - 2026-04-25

Patch — operator-flagged PWA cleanup for the rule "no internal
ticket IDs in user-facing strings."

### Fixed

- **eBPF status message** (`internal/observer/ebpf/probe_linux.go`) —
  the noop fallback when the kprobe loader hasn't landed used to
  read "kprobe attach not yet implemented (BL173 task 1 — bpf2go
  integration pending)". Operator-visible via
  `host.ebpf.message` → PWA Settings → Monitor card. Replaced
  with: "kprobe attach is not wired yet — eBPF objects are linked
  but the loader is pending".
- **Federated peers card empty-state** (`internal/server/web/app.js`) —
  the "no peers registered" hint emitted "deploy datawatch-stats
  on a Shape B host, or spawn an F10 agent (auto-peers in v4.7.0+)".
  Replaced internal IDs and version reference with operator-friendly
  wording: "deploy `datawatch-stats` on a remote host, or spawn an
  autonomous worker (it auto-peers)" with a link to the docs.

## [4.8.0] - 2026-04-25

Minor release — opens the **S14a cross-cluster federation** sprint
with the foundation slice, plus ships the eBPF generated artifacts
so per-process net telemetry works without a clang toolchain on the
operator's host.

### Added

- **S14a foundation — cross-cluster federation push-out.** A
  datawatch primary can now register itself as a Shape "P" peer
  of another *root* primary, giving operators with multiple
  clusters a single pane of glass. Opt-in via the new
  `observer.federation.parent_url` config; empty (default) leaves
  federation off so existing single-cluster deployments are
  unchanged.
  - **`observer.FederationCfg`** + **`config.FederationConfig`** —
    `parent_url`, `peer_name` (defaults to host name), 
    `push_interval_seconds` (default 10), `token_path`
    (default `<data_dir>/observer/federation.token`), `insecure`.
  - **`observer.Envelope.Source`** field — federation attribution
    so the root can render envelopes grouped by originating
    primary. Empty / `"local"` for self-produced.
  - **Loop prevention.** Push body gains an optional `chain` field;
    server-side `handlePeerPush` rejects with **HTTP 409** when
    the chain already contains the receiver's own primary name.
    Set per-server via `httpServer.SetFederationSelfName(name)`.
  - **`observerpeer.Client.PushWithChain(ctx, snap, chain, …)`** —
    new public method; the original `Push` is preserved as a
    chainless single-hop wrapper so Shape A/B/C peers stay
    wire-identical.
  - **Federation push goroutine** wired in `cmd/datawatch/main.go`
    when `parent_url` is set. Registers (or reuses persisted
    token) and pushes `coll.Latest()` every interval; failures
    are logged and retried on the next tick.

### Channel parity

- **YAML / REST / MCP / CLI** — `observer.federation.*` lives on
  `ObserverConfig`, so it's settable via the config file, the
  `/api/observer/config` endpoint (existing PUT/POST), the MCP
  `observer_set_config`, and the `datawatch observer config set`
  CLI without surface-specific code changes.
- **PWA / mobile** — the additive `chain` and `Source` fields are
  forward-compatible JSON; existing clients read them or ignore
  them. PWA "Cluster" filter pill (rendering the new `Source`
  attribution as a group) ships in v4.8.x.

### eBPF generated artifacts now committed (BL177 partial)

- **`internal/observer/ebpf/vmlinux.h`** (3.4 MB, BTF-dumped from
  amd64 6.x kernel) — committed so `make ebpf-gen` works without
  the Debian/Ubuntu `linux-bpf-dev` stub vmlinux.h that was
  missing arch-specific types like `user_pt_regs`.
- **`internal/observer/ebpf/netprobe_x86_bpfel.{go,o}`** — bpf2go
  output committed so `go build` succeeds without clang. Loader
  is still a stub (`generatedAvailable` returns false today;
  attach path lands in BL173 task 1 follow-up); the artifacts
  unblock that work.
- **`netprobe.go`** `//go:generate` updated to use a vendored
  vmlinux.h (`-I .` + `-target amd64`). The arm64 generation path
  needs a separate arm64-host BTF dump — see BL177.
- **`netprobe.bpf.c`** include path unchanged.

### Tests

- `internal/observerpeer/client_test.go` — 2 new tests for
  `PushWithChain` chain-field serialization (with and without
  chain).
- `internal/server/observer_peers_federation_test.go` — 4 new
  tests covering chain-loop rejection (HTTP 409), accepted
  chain-not-containing-self push, empty-self-name skip-the-check
  path, and chainless single-hop legacy compatibility.

### Backlog filed (do-not-implement; operator decisions)

- **BL175** — `docs/` vs `internal/server/web/docs/` duplication
  review (4 options listed, no decision).
- **BL176** — RTK update command string still emits `rtk update`
  in PWA + OpenAPI + chat help; canonical path is the install.sh
  one-liner.
- **BL177** — eBPF generated-artifacts distribution (this release
  ships the amd64 slice; arm64 + CI drift-check + setup-doc note
  still pending operator decision).
- **BL178** — PWA "view last response" returns stale (multi-day-
  old) entries; reproduced on session 787e.
- **BL179** — PWA search-icon should live in the top header bar
  next to the daemon-status light, not inside the sessions card.

### Container images

No daemon ABI changes that affect container behaviour; the
existing `parent-full`/`agent-*`/`stats-*` images at
`harbor.dmzs.com/datawatch/*:v4.7.0` continue to work.
Federation requires only the new YAML key — clients running v4.7.x
images push to a v4.8.0 root with no changes (the `chain` field is
optional). Container retags to v4.8.0 ship as a follow-up commit
once the federation rollup story (PWA Cluster pill) is wired.

## [4.7.2] - 2026-04-25

Patch release — closes the S13 follow-up. The orchestrator graph
endpoint now actually populates the `observer_summary` field that
the v4.7.1 wire shape reserved.

### Added

- **`SessionIDsForPRD(prdID)` accessor** on `AutonomousAPI`
  (`internal/autonomous`) — walks the PRD's stories+tasks and
  returns every non-empty `Task.SessionID`. Pure read, in-memory.
- **`EnvelopeSummary(id)` accessor** on `ObserverAPI`
  (`internal/observer`) — looks up one envelope by ID in the local
  collector's latest snapshot and returns `(cpu_pct, rss_bytes,
  ok)`. Companion to the peer registry's `LastPayload`.
- **Server-side enrichment** in
  `GET /api/orchestrator/graphs/{id}` — for every PRD node, joins
  `prd_id → session IDs → envelope "session:<sid>"` across the
  local observer + every peer's last snapshot, sums CPU% +
  RSS bytes + envelope count, takes max(`last_push_at`), and
  inlines the result as `node.observer_summary`. Best-effort: if
  `autonomousMgr` / `observerAPI` / `peerRegistry` is nil, or no
  envelope matches, the field is omitted (graph wire shape stays
  unchanged).

### Channel parity (no code change required)

- **MCP** (`orchestrator_graph_get`) and **CLI**
  (`datawatch orchestrator graph get`) both proxy through
  `/api/orchestrator/graphs/{id}`, so they receive the enriched
  `observer_summary` automatically — no surface changes needed.
- **PWA / mobile** can render the field as soon as it appears
  (additive JSON field; clients already tolerated `null` from
  v4.7.1).

### Tests

- `internal/autonomous/sessionids_test.go` — 3 tests covering
  unknown PRD, scheduled-tasks-only collection across multiple
  stories, and empty-PRD edge.
- `internal/server/orchestrator_enrich_test.go` — 4 tests:
  local-only path, peer-snapshot fold-in (incl. `last_push_at`),
  no-match omission, and nil-deps no-op safety.

### Closed

- **S13 follow-up** — orchestrator graph `observer_summary` join
  ([design doc](docs/plans/2026-04-25-s13-followup-orchestrator-observer.md);
  GH [#20](https://github.com/dmz006/datawatch/issues/20)).

## [4.7.1] - 2026-04-25

Patch release — operator UX polish + verification close-outs +
observer wire-shape forward-compat field.

### Added

- **PWA sessions list — search-icon toggle (B44)**. Mobile parity
  with datawatch-app: a magnifying-glass icon replaces the "▴ filters"
  text pill on the sessions list. **Default OFF** (filters hidden,
  session list takes the full window); click to reveal the filter
  input + backend badges + history toggle. State persists in
  localStorage. Operators with an existing collapsed/expanded
  preference keep it.
- **`internal/orchestrator.Node.ObserverSummary`** field — wire-shape
  scaffolding for the deferred S13 follow-up
  (`/api/orchestrator/graphs/{id}` per-node observer attribution).
  Always nil today; populated server-side once the
  `SessionIDsForPRD` accessor on `AutonomousAPI` lands. PWA / MCP /
  mobile clients can render it as soon as it appears.

### Verified (BL173 + BL174 close-outs)

- **Shape C build+push pipeline** ✅ end-to-end on the dev
  workstation: `Dockerfile.stats-cluster` builds a 11 MB distroless
  image (`docker run --once --shape C` produces the wrapped
  StatsResponse v2 envelope correctly), pushed to
  `harbor.dmzs.com/datawatch/stats-cluster:v4.7.0`. Helm DaemonSet
  manifest still applies dry-run against the testing cluster. The
  one remaining gap (live cluster→parent push) is a network-
  topology issue specific to the dev env.
- **BL174 image sizes captured** in
  [`docs/plans/2026-04-25-bl174-image-measurements.md`](docs/plans/2026-04-25-bl174-image-measurements.md):
    - agent-base       127 → 133 MB (+6 MB channel-bridge bundle)
    - agent-claude     199 → 205 MB (+6 MB inherits agent-base)
    - **agent-opencode 232 → 182 MB (−50 MB / −22%)** ← BL174 win
    - **stats-cluster (new) 11 MB** distroless

### Fixed

- `datawatch-stats --once --shape C` debug dump labelled `shape: "B"`
  in the JSON wrapper. Now respects `--shape` correctly.

### Removed

- Dead files `cmd/datawatch-stats/peer.go` + `peer_test.go` — S13
  hoisted them into `internal/observerpeer/` but the v4.7.0 commit
  forgot the `git rm`. Cleaned up.

### Internal

- Three new design docs added today (S13-followup, S14a/b/c
  umbrella, BL174 measurements). Backlog README reflects all
  closures.

## [4.7.0] - 2026-04-25

S13 ships. Every F10 ephemeral agent worker now registers as a Shape A
peer in the observer federation. The parent's PWA Federated peers card
shows live per-agent CPU / mem / envelopes alongside Shape B (standalone)
and Shape C (cluster) peers; tapping an agent opens the v4.6.0
process-tree drill-down modal with the worker's own /proc tree.

### Added — BL S13 agent observer peers

- **`internal/observerpeer/`** (new package). Hoisted out of
  `cmd/datawatch-stats/peer.go` so `cmd/datawatch-stats` and the
  parent's worker-mode datawatch share one peer client. Adds
  `Client.SetToken` for the agent flow (parent mints + injects via
  bootstrap-env; worker skips register).
- **`Manager.Spawn`** mints an observer-peer token via
  `Manager.ObserverPeers.Register` alongside the bootstrap token.
  Token surfaced on the bootstrap response under
  `Env.DATAWATCH_OBSERVER_PEER_TOKEN` + `_NAME` +
  `DATAWATCH_PARENT_URL`. Warn-only on registry failure (worker
  still functions, just no per-agent peer view).
- **Spawn-failure cleanup**: orphan peer dropped when `Driver.Spawn`
  returns an error before the worker comes up.
- **`Manager.Terminate`** drops the peer so it stops appearing in the
  Federated peers card immediately.
- **Worker push loop** (`startWorkerObserverPush` in `cmd/datawatch`):
  runs an in-process `observer.Collector` + `observerpeer.Client`,
  pushes one immediate snapshot at boot then settles to 5s cadence.
  `host.shape="agent"`. Insecure-TLS by default — workers already
  trust the parent through the bootstrap-channel pinning.
- **PWA Federated peers card** gains a filter-pill row:
  `All · Agents · Standalone · Cluster`, with per-shape counts.
  Filter persists in localStorage. Friendlier shape labels (`agent` /
  `standalone` / `cluster` instead of `shape A / B / C`).
- **MCP**: `observer_agent_stats { agent_id }` and
  `observer_agent_list` — thin aliases over `observer_peer_*` so
  callers can ask "what's agt_a1b2 doing right now" without first
  learning that agents-are-peers.
- **CLI**: `datawatch observer agent {list,stats <agent_id>}`
  mirrors the MCP aliases.

### Tests

- `internal/observerpeer/client_test.go` (12) — ports the
  cmd/datawatch-stats peer tests and adds:
  - `SetToken` skips Register (the agent flow)
  - explicit shape lands in the push body (Shape A vs B)
  - default shape is B
- `internal/agents/observerpeer_test.go` (5):
  - Spawn registers an observer peer + records the token
  - Registry failure is warn-only (spawn succeeds)
  - Driver-spawn failure cleans up the orphan peer
  - Terminate drops the peer + clears the token
  - No registry wired → no-op without panic
- `internal/mcp/observer_peers_test.go` (+3) for the agent aliases.
- `cmd/datawatch/cli_observer_peer_test.go` (+2) for the agent CLI.

### Deferred to a future patch

- **Orchestrator graph `observer_summary`**: the BL117 graph nodes
  don't currently carry `agent_id`, so per-node observer attribution
  needs an orchestrator-level change first. Tracked separately;
  doesn't block S13's main deliverable.
- **Mobile parity** filed as
  [datawatch-app#6](https://github.com/dmz006/datawatch-app/issues/6)
  (Agents filter pill) and
  [datawatch-app#7](https://github.com/dmz006/datawatch-app/issues/7)
  (per-node observer_summary badge — also gated on the orchestrator
  change above).

### Internal

No breaking changes. Old running agents keep working unchanged
until terminated; new spawns auto-peer up on the next boot. Existing
peer registry, Helm chart, and openapi entries are unchanged
(observer_agent_* are aliases over the same `/api/observer/peers/*`
endpoints).

## [4.6.0] - 2026-04-25

Closes BL173 + BL174. Three streams in this minor release:

### Closed — BL173 (Shape C cluster observer) follow-ups

- **K8sMetricsScraper output → snap.Cluster.Nodes**. The scraper landed
  in v4.5.1 alongside the parser tests (validated against a real
  PKS / Tanzu k8s 1.33 metrics-server payload), but the daemon
  didn't surface the result. v4.6.0 wires `Collector.SetClusterNodesFn`
  → `cmd/datawatch-stats --shape C` → `K8sMetricsScraper.Latest`. The
  PWA's "Cluster nodes" card now shows what the scraper sees on a
  multi-node cluster.
- **PWA process-tree drill-down modal** (BL173 task 6). The
  v4.5.1 toast on the federated-peer 📊 button is replaced with a
  proper modal: peer header, sortable envelope rows, click any row
  to lazy-load its process tree from `/api/observer/envelope?id=`.
  Top 50 processes per envelope by CPU desc.

### Closed — BL174 (slim claude container) node removal

The v4.4.0 work eliminated node from agent-base/claude **runtime**
but the **builder** still pulled nodejs (~80 MB apt) just to
`npm install` and extract one binary. v4.6.0 closes the loop:

- `Dockerfile.agent-claude`: builder fetches the per-platform native
  tarball (`@anthropic-ai/claude-code-linux-{x64,arm64}`) directly
  from the npm registry CDN with `curl + tar`. Verified the
  package layout (`package/claude` → 236 MB ELF) against the live
  registry at build doc time. Multi-arch via `$TARGETARCH`.
- `Dockerfile.agent-opencode`: same pattern — pull
  `opencode-linux-{x64,arm64}/package/bin/opencode` (native ELF)
  directly. Previous Dockerfile installed nodejs in **both** the
  builder AND the runtime stage (the runtime layer carried ~80 MB
  of node it didn't need); v4.6.0 drops both.

Audit summary: the only Dockerfiles that still install nodejs are
`Dockerfile.agent-gemini` (gemini-cli is a JS bundle — no native
release; can't avoid it without Google) and `Dockerfile.lang-node`
(it IS the node lang image — by design).

### Improved — BL172 follow-up

- **PWA peer-stale badge** on the Settings nav cog. Counts federated
  peers whose `last_push_at` is >60 s old or never. Polls
  `/api/observer/peers` every 30 s; 503 / network errors hide
  silently. Shipped in main but reflected here for completeness.

### Tests

- `internal/observer/observer_test.go`: 2 new tests for the
  `ClusterNodesFn` wiring (populates snap.Cluster when fn set;
  empty fn keeps Cluster nil so the PWA card hides).
- All 6 `cluster_k8s_test.go` tests still pass against the captured
  real metrics-server payload.

### Internal

No breaking changes. Existing v4.5.x deployments continue to work;
the v4.6.0 image rebuilds drop node from claude+opencode entirely
but the daemon ABI is unchanged. Helm chart values unchanged.

## [4.5.1] - 2026-04-25

Parity patch — closes the configuration-accessibility gap on the
BL172 peer registry that v4.4.0 / v4.5.0 left half-finished.

### Added — peer registry MCP + CLI + comm parity

- **MCP tools** (5): `observer_peers_list`, `observer_peer_get`,
  `observer_peer_stats`, `observer_peer_register`, `observer_peer_delete`.
  All return the same JSON the REST endpoints serve; required `name`
  arg validated; webPort=0 (loopback unavailable) surfaces a clear
  error rather than panicking.
- **CLI** subcommand: `datawatch observer peer
  {list,get,stats,register,delete}`. Mirrors the REST surface so an
  operator on the parent host can manage peers without curl.
- **Comm-channel parity**: new `peers` command parsed by the router.
  Forms:
    - `peers` — list
    - `peers <name>` — detail
    - `peers <name> stats` — last snapshot
    - `peers register <name> [shape] [version]` — mint a token
    - `peers delete <name>` — de-register / rotate
  Replies are pretty-printed JSON when valid, raw text otherwise.
- **`docs/api-mcp-mapping.md`** updated with the Observer peers
  section.

### Tests

- 5 new in `internal/mcp/observer_peers_test.go` (tool names,
  required-arg enforcement, missing-name → IsError, webPort=0
  surfaces error not panic).
- 5 new in `cmd/datawatch/cli_observer_peer_test.go` (subcommand
  registration, register Args range, get/stats/delete require name,
  Long help mentions both Shape B and Shape C).
- 12 new in `internal/router/peers_test.go` (parser forms; handler
  smoke against an httptest fake parent for list / get / stats /
  register / delete + missing-name guards).

### Internal

No behaviour change for operators who don't use the new surfaces.
Existing peer registry, PWA card, and openapi entries are unchanged.

## [4.5.0] - 2026-04-25

S12 lands. Shape C — the privileged cluster observer container — is
now buildable and deployable via Helm DaemonSet, completing the BL171
three-shape vision. The standalone binary (`datawatch-stats --shape C`)
is the same one Shape B uses; the difference is the Dockerfile, the
manifest, and the eBPF + DCGM + k8s-metrics-scrape sidecars enabled
by default.

### Added — BL173 / S12 Shape C cluster observer

- **`internal/observer/ebpf/`** package: per-pid network byte
  counters via TCP/UDP kprobes. Includes `netprobe.bpf.c` source +
  bpf2go `//go:generate` directive (run `make ebpf-gen` to compile;
  requires clang + kernel headers). Without pre-generated objects the
  loader degrades to a noop probe with a clear `host.ebpf.message`
  reason — Shapes A/B keep working /proc-only.
- **`internal/observer/gpu_dcgm.go`**: Prometheus-format scraper for
  NVIDIA's DCGM exporter. Pulls `DCGM_FI_PROF_PROCESS_USAGE` +
  `DCGM_FI_DEV_FB_USED` per-pid; nil-safe when the URL is empty;
  best-effort on errors (cluster pods on hosts without GPU still push
  a useful StatsResponse).
- **`datawatch-stats --shape C`** flag: longer default push interval
  (10 s for cluster scale), eBPF defaults to "true" (mandatory in
  Shape C; loader still degrades on missing CAP_BPF), DCGM URL
  defaults to `http://localhost:9400/metrics`.
- **`Dockerfile.stats-cluster`**: distroless multi-stage image. Tries
  `make ebpf-gen` in the builder stage; tolerates clang absence.
  Runtime is `gcr.io/distroless/cc-debian12:nonroot`. New Makefile
  target `make cluster-image VERSION=…` for multi-arch buildx push to
  `ghcr.io/dmz006/datawatch-stats-cluster`.
- **Helm DaemonSet** at `charts/datawatch/templates/observer-cluster.yaml`
  + `observer.shapeC.*` values block. Deploys ServiceAccount +
  ClusterRole/Binding for `metrics.k8s.io`, hostPID + CAP_BPF +
  CAP_PERFMON + CAP_SYS_RESOURCE, hostPath /sys + /proc, peer-token
  Secret (operator seeds out-of-band).
- **PWA Cluster nodes** subsection on Settings → Monitor card.
  Hidden when `cluster.nodes` is empty; otherwise renders one row
  per node with health dot, CPU + memory bars, pod count, pressure
  flags.

### Tests

- 4 new in `internal/observer/ebpf/probe_test.go` (noop contract,
  graceful degrade, Reason exposure).
- 6 new in `internal/observer/gpu_dcgm_test.go` (parser happy +
  malformed + ignore-unrelated, scraper polling, unreachable
  fallback, idempotent stop).
- All BL172 / BL170 / BL174 tests still pass.

### Internal

`observer.shapeC.enabled` defaults to `false` in the Helm chart, so
existing installs see no change. Operators register each node as a
peer on the primary datawatch and seed the
`datawatch-observer-cluster-token` Secret with the returned bearer
token before flipping the value to `true`.

### Things to verify in production

This release ships the complete framework but several pieces can't
be exercised on the dev laptop — they're noted here so the operator
checks them on the cluster:

- **eBPF kprobe attach**: requires kernel ≥ 5.10 with BTF +
  `CAP_BPF`/`CAP_PERFMON`. The loader falls back gracefully when
  these aren't met; verify `host.ebpf.kprobes_loaded: true` after
  `make ebpf-gen` + `make cluster-image` + cluster deploy.
- **DCGM scrape**: requires NVIDIA's DCGM exporter on each GPU node.
  Verify `cluster.nodes[*].gpu_pct` populates within 60 s of pod
  start.
- **k8s metrics-server scrape**: the Helm chart ServiceAccount has
  `get/list` on `metrics.k8s.io`; verify `cluster.nodes[]` populates
  on a multi-node cluster.

## [4.4.0] - 2026-04-25

S11 ships. The Shape B standalone observer daemon (`datawatch-stats`)
lands as a new binary that registers with a primary datawatch and
pushes `StatsResponse v2` snapshots over HTTPS — federated monitoring
for Ollama / GPU / mobile-edge boxes that don't run the full parent.

### Added — BL172 / S11 standalone observer daemon

A new `datawatch-stats` binary (~9 MB stripped) reuses
`internal/observer.Collector` end-to-end and pushes to a primary
parent over HTTPS.

- **Peer-side**: `datawatch-stats --datawatch <url> --name <peer>`
  registers (POST /api/observer/peers), persists the bearer token
  to `~/.datawatch-stats/peer.token` (mode 0600), and pushes a
  Shape-B-wrapped snapshot every `--push-interval` (default 5 s).
  Auto-recovers on 401 via re-register + retry.
- **Parent-side**: new `/api/observer/peers/*` REST surface.
  `POST /api/observer/peers` mints + bcrypt-hashes a token; the
  plaintext is returned exactly once. Peers persisted to
  `<data_dir>/observer/peers.json` so a parent restart doesn't drop
  them. `GET /api/observer/peers/{name}/stats` returns the last-
  pushed snapshot. `DELETE /api/observer/peers/{name}` rotates the
  token (peer auto-re-registers).
- **Sidecar mode**: `--listen 127.0.0.1:9001` exposes a local
  `/api/stats` endpoint so an operator can curl the standalone
  peer without going through the parent.
- **One-shot mode**: `--once` prints one wrapped snapshot to stdout
  and exits — handy for evaluating what would be pushed.
- **systemd** unit at `deploy/systemd/datawatch-stats.service`
  (User=datawatch, hardened defaults, `AmbientCapabilities=CAP_BPF`
  commented for explicit operator opt-in).
- **launchd** plist at `deploy/launchd/com.datawatch.stats.plist`.
- **Cross-build**: `make cross-stats` produces 5 platform binaries.
- **`datawatch setup ebpf --target stats`** capability-patches the
  standalone binary instead of the parent.
- **PWA**: Settings → Monitor → "Federated peers" card lists every
  registered peer with health dot (green <15 s, amber <60 s, red
  ≥60 s), shape badge, last-push age, plus 📊 (snapshot) and ×
  (remove) actions.

### Improved — BL174 part 2: container builds bundle the Go bridge

- `Dockerfile.agent-base` builds + installs `datawatch-channel`
  alongside `datawatch` at `/usr/local/bin/datawatch-channel`.
  agent-claude / parent-full inherit it — channel mode now works
  inside the container with no Node.js install. Adds ~7-8 MB.

### Tests

- 8 new tests in `internal/observer/peer_registry_test.go` (mint +
  persist, rotation, redaction, RecordPush, unknown-peer, delete +
  persist, snapshot omission, corrupt-file rejection).
- 8 new tests in `internal/server/observer_peers_test.go` (disabled
  503, register happy + bad-input, list, delete, push happy +
  bad-token + mismatched name, get-last-snapshot).
- 9 new tests in `cmd/datawatch-stats/` (snapshot wrap format,
  sidecar, register persistence, load-skips-register, push happy +
  401 recovery + 5xx surface, constructor validation).

### Internal

No breaking changes. `observer.peers.allow_register` defaults to
true so the registry is reachable on fresh installs; operators who
don't run a Shape B peer see the empty `/api/observer/peers` list
and the calm "no peers registered" PWA state.

## [4.3.0] - 2026-04-25

The Node.js dependency for Claude channel mode goes away. This release
ships a native Go MCP bridge as the preferred path; the JS bridge stays
as a transparent fallback so existing installs keep working.

### Added — Native Go MCP channel bridge (BL174)

A new `datawatch-channel` binary (~7.7 MB stripped, single static Go
binary) replaces the embedded `channel.js` + `node_modules` runtime
for the daemon ↔ Claude MCP bridge.

- Wire-compatible with `channel.js`: same env vars
  (`DATAWATCH_CHANNEL_PORT`, `DATAWATCH_API_URL`, `DATAWATCH_TOKEN`,
  `CLAUDE_SESSION_ID`), same HTTP routes (`/send`, `/permission`,
  `/health`), same MCP `reply` tool. Drop-in swap.
- Resolution order: `$DATAWATCH_CHANNEL_BIN` → `<data_dir>/channel/`
  → sibling of the running `datawatch` binary → `PATH`. The daemon
  picks it up automatically and logs `[channel] using native Go
  bridge: <path>` on startup.
- When the Go bridge is in use, the daemon **skips** the JS extract
  + `npm install` entirely (no more rewriting `channel.js` on every
  boot).
- Backward compatible: when no Go binary is found, the existing JS
  flow still runs unchanged.
- Migration: `datawatch setup channel --cleanup` removes the legacy
  `channel.js`, `package.json`, `package-lock.json`, and `node_modules`
  from `<data_dir>/channel/`. The daemon prints a one-time
  `[migrate] …` notice on startup until the operator runs the cleanup.
- New cross-build target `make cross-channel` produces 5 platform
  binaries shipped as release artifacts.

### Changed

- `datawatch setup channel` rewrites its help to explain the two
  implementations + their precedence; reports clean `Go bridge: …
  ✓ Ready` when the binary is found.
- `internal/channel.Probe` short-circuits to `Ready=true` when the
  Go binary is present, regardless of node/npm.

### Documentation (BL170 phase 1-5)

This release also closes most of the BL170 feature-completeness audit
work that was in flight in v4.2.x:

- **REST coverage in OpenAPI** went from **47/127 (37%)** to **115/127
  (90.5%)**. The 12 deliberately omitted routes are worker-internal /
  PWA-only and annotated inline (`/api/proxy/*`, `/api/test/message`,
  `/api/mcp/docs`, `/api/agents/peer/*`, `/api/channel/{ready,notify}`,
  `/api/stats/kill-orphans`, `/api/update`, `/api/pipeline*`).
- **5 new flow diagrams**: voice transcription, RTK auto-update,
  memory + KG recall, plugin invocation, channel mode end-to-end.
- **architecture-overview.md** refreshed to link every major subsystem
  to its diagram + design doc.
- **Audit report** in `docs/plans/2026-04-25-bl170-feature-completeness-audit.md`
  with a phased plan; phases 1-5 complete, MCP-mapping refresh deferred.

### Tests

- 8 new unit tests in `cmd/datawatch-channel/` cover the reply tool
  (happy + bad-input + env-vs-explicit precedence), HTTP `/send` +
  `/permission` + `/health` handlers, idempotent `notifyReady`, and
  parent-error surfacing.
- 4 new tests in `internal/channel` cover `BinaryPath` env override,
  `Probe` Go-bridge short-circuit, and `LegacyJSArtifacts` /
  `RemoveLegacyJSArtifacts`.

### Internal

No breaking changes. Operators who don't drop in the Go binary keep
running the JS bridge with no behaviour change. Operators who do get
a smaller, faster, dependency-free bridge.

## [4.2.0] - 2026-04-24

A bundle release covering nine operator-visible items: a string-hygiene
sweep, native plugin surfacing, an explicit Node.js dependency story
for channel mode, broader Claude rate-limit handling, opencode-acp
chain-of-thought UX, and four PWA composer / navigation polish items.
No breaking changes.

### Added

- **PWA voice input** (`#21`) — 🎤 button in the session input bar
  records via MediaRecorder, POSTs to `/api/voice/transcribe`, and
  pastes the transcript into the input field. Works on Chromium
  (webm/opus), Firefox (webm), and Safari (mp4).
- **Floating Action Button** (`#22`) — replaces the bottom-nav "New"
  tab with a + FAB on top-level views; routes to the existing new-
  session view, which now ships with a top-right close affordance.
- **Sessions list design polish** (`#23`) — drops top whitespace,
  adds a collapsible filter pill (state persisted in localStorage),
  lowers the FAB so it sits just above the bottom nav.
- **Terminal toolbar collapse** (`#24`) — `toolbar` pill on the
  session-info-bar hides the term controls (font, fit, scroll) to
  reclaim vertical space; persisted in localStorage.
- **Native plugins surfaced in `/api/plugins`** (`B41`) — the endpoint
  now returns `{plugins, native}`. Native entries (built-in subsystems
  like `datawatch-observer`) appear in the PWA's plugin status list
  with a small "native" tag and a per-subsystem status line. The
  endpoint no longer 503s when subprocess plugins are off.
- **`datawatch setup channel`** (`B39`) — pre-installs the Node.js
  MCP bridge dependencies up-front so the first session does not
  block on `npm install`. Probes node + npm and prints what's
  missing.
- **Channel runtime probe + startup warn** (`B39`) — daemon prints a
  clear `[warn]` at startup when `claude_channel_enabled=true` but
  Node.js / npm / `node_modules` are missing. `internal/channel.Probe`
  is the underlying check.
- **BL174 backlog entry** — native Go MCP server replacement for
  `channel.js` (using `mark3labs/mcp-go`) plus a redo of the agent-
  claude container build to drop Node.js and shrink the image.

### Improved

- **Claude rate-limit handling** (`B43`) — auto-select-wait + schedule-
  resume now also fires on modern claude-code prompts ("Your usage
  limit will reset at 9pm…", "try again in 4h 23m"). Reset-time
  parsing handles three families: `DATAWATCH_RATE_LIMITED:` protocol,
  prose clock-time (12h / 24h / "5:30 PM PST"), and relative
  durations ("in 30m" / "in 4h 23m"). Prevents the schedule from
  defaulting to +60 min when the real reset is many hours later.
- **opencode-acp chain-of-thought UX** (`B42`) — `step-start` events
  with a non-empty reason now persist as a Thinking-role chat bubble
  with a collapsed `<details>` showing the reason. Bare `Thinking…`
  with no reason keeps its transient typing-dots indicator.
- **Operator-visible string hygiene** (`B40`) — strips internal ticket
  IDs (Sprint/Sn, vN.N.x, BL/F/B refs) from cobra Short fields,
  runtime warn lines, and the eBPF status message. Comments and
  CHANGELOG entries are unchanged.

### Documentation

- `docs/claude-channel.md` adds a "Runtime requirements" section
  explaining the Node.js dependency, the `datawatch setup channel`
  command, and how to disable channel mode.
- `docs/plans/README.md` adds BL174 to the Pending backlog table.

### Tests

- `internal/channel/probe_test.go` — covers the channel runtime probe
  (ready/not-ready paths).
- `internal/session/ratelimit_parser_test.go` — exercises all three
  reset-time families and unparseable inputs.

### Internal

No backwards-incompatible changes. The legacy `'new'` view still
works; `openNewSessionModal()` is just a router into it.

## [4.1.1] - 2026-04-22

### Added — eBPF visibility (closes operator gap reported same-day)

After running `datawatch setup ebpf`, the PWA gave no visible feedback
about whether anything actually changed. v4.1.1 surfaces the state
honestly so the operator can tell `setup ebpf` from `setup ebpf
worked` from `kprobes are live`.

- New `host.ebpf` field on `StatsResponse v2`:
  - `configured` — operator opted in via `observer.ebpf_enabled` or
    `datawatch setup ebpf`.
  - `capability` — Linux probe: reads `/proc/self/status:CapEff`
    bit 39 (CAP_BPF). True only when `setcap` succeeded.
  - `kprobes_loaded` — false in v4.1.x; the `kprobe/tcp_*` /
    `kprobe/udp_*` programs ship in Sprint S12 alongside the cluster
    container.
  - `message` — human-readable explanation of the current state and
    what the operator should expect next.
- New PWA card "eBPF (per-process net)" inside Settings → Monitor →
  System Statistics, just above the existing "Installed plugins"
  strip. Shows a colored status dot (green = live, purple =
  configured + capability, amber = configured but capability
  missing, grey = off) plus the `message` text.

### Bug-fix-shaped (operator workflow)

- `datawatch setup ebpf` already flipped `observer.ebpf_enabled` in
  v4.1.0 — that part was working — but the new visible status above
  removes the "did anything happen?" question after running it.

### Container images

- `parent-full`: rebuild + retag to v4.1.1 required (daemon binary +
  web bundle).
- Other images unchanged.
- Helm: `version: 0.15.1`, `appVersion: v4.1.1`.

### Breaking changes

- None. `host.ebpf` is additive; older clients ignore unknown fields.

## [4.1.0] - 2026-04-22

### Added — Sprint S9 (BL171 datawatch-observer — substrate)

- **`internal/observer/` package** — `StatsResponse v2` types,
  Linux `/proc` walker (with per-pid static-field cache between
  ticks), envelope classifier (session / backend / container /
  system), default in-process collector, config + API adapters.
  Non-Linux builds get a trimmed-output collector so host + cpu +
  mem + disk + sessions still render.
- **Full sub-process tree monitoring.** Every tick walks `/proc`,
  groups processes into envelopes keyed by session id and LLM
  backend signature (claude, ollama, aider, goose, openwebui,
  gemini, opencode). Docker containers picked up via
  `/proc/<pid>/cgroup` parsing; container id + image correlate
  onto `backend:<name>-docker` envelopes.
- **`StatsResponse v2` wire contract.** Structured top-level
  objects: `host`, `cpu`, `mem`, `disk[]`, `gpu[]`, `net`,
  `sessions`, `backends[]`, `processes.tree`, `envelopes[]`,
  `cluster?`, `peers[]`. Back-compat: every v1 flat scalar
  (`cpu_pct`, `mem_pct`, `uptime_seconds`, etc.) preserved at
  the root so v1 clients keep working.
- **REST**: `GET /api/stats?v=2` negotiates the v2 shape on the
  existing path. New dedicated endpoints: `GET /api/observer/stats`,
  `GET /api/observer/envelopes`, `GET /api/observer/envelope?id=`,
  `GET/PUT /api/observer/config`.
- **Full channel parity**: 5 new MCP tools (`observer_stats`,
  `observer_envelopes`, `observer_envelope`, `observer_config_get/set`),
  new `datawatch observer` CLI subtree (stats / envelopes /
  envelope / config-get / config-set), comm via the existing
  `rest` passthrough, `observer:` YAML block.
- **eBPF across all three shapes** (new 2026-04-22 direction).
  The `ebpf_enabled: auto|true|false` config is shared by Shape A
  (plugin), Shape B (standalone daemon — v4.2.0), and Shape C
  (cluster container — v4.2.x). `datawatch setup ebpf` now flips
  both the legacy `stats.ebpf_enabled` and the new
  `observer.ebpf_enabled` in one step, and attempts a live REST
  flip via `/api/observer/config` so the change takes effect on
  the next tick without a restart. Shape C still uses manifest
  capabilities (documented in `docs/api/observer.md#shape-c`).
- **PWA Settings → Monitor → System Statistics** gains an
  "Installed plugins" strip at the bottom listing each registered
  plugin with enabled/disabled state, hooks, invoke count, and
  last-error badge. Sources from `/api/plugins`; gracefully shows
  "plugin framework off" when `plugins.enabled=false`.

### Docs
- `docs/api/observer.md` — full operator + AI-ready contract.
- `docs/flow/bl171-observer-flow.md` — tick pipeline + degradation
  modes.
- `docs/plans/2026-04-22-bl171-datawatch-observer.md` — design +
  S9–S13 sprint plan. Updated with eBPF-across-all-shapes
  correction on 2026-04-22.
- `docs/api/openapi.yaml` — 5 new paths documented; resynced to
  `internal/server/web/openapi.yaml`. 59 paths total.
- `docs/api-mcp-mapping.md` — new "Observer" row covering all 5
  MCP tools + the v2 alias on `/api/stats`.
- `docs/config-reference.yaml` — `observer:` block with every key
  commented.

### Tests
- 6 new unit tests in `internal/observer/observer_test.go`:
  classifier priority (session beats backend for nested claude),
  docker container fallback, system catch-all, container ID
  extraction across Docker v2 cgroup formats, collector v1
  aliases, API adapter config round-trip.
- Full suite: 1176 passing, 54 packages.

### Container images
- `parent-full`: **rebuild + retag to v4.1.0 required** (daemon
  binary + web UI bundle change).
- Other images unchanged.
- Helm: `version: 0.15.0`, `appVersion: v4.1.0`.

### Breaking changes
- None. v1 `/api/stats` shape is preserved.
- Go `internal/config.ObserverConfig` is new; it defaults through
  `observer.DefaultConfig()` when missing from YAML.

### Not in v4.1.0
Deferred to later sprints per the plan:
- Shape B standalone daemon (`cmd/datawatch-stats/`) — Sprint S11 (v4.2.0)
- Shape C cluster container (`Dockerfile.stats-cluster`) — Sprint S12 (v4.2.x)
- Actual eBPF kprobes — Sprint S12; v4.1.0 ships the config toggle + setup flow but the kprobe objects land alongside the cluster container build.
- PWA Monitor tab full rework (envelope table + drill-down modal) — Sprint S9.x follow-up.
- datawatch-app Phase 1+2 consumption — Sprint S10 (gated on this v2 contract shipping, now unblocked).

## [4.0.9] - 2026-04-21

### Bug fixes
- **B38 follow-up — `GET /api/config` response now includes
  `autonomous`, `plugins`, `orchestrator` sections.** v4.0.8 fixed
  the PUT path (saves persist to `config.yaml`) but the GET path
  was still emitting a response map that skipped those three
  blocks entirely. On reload, the PWA + mobile Settings cards saw
  no state and rendered empty fields even though the values were
  on disk. Added the three sections next to `pipeline` / `whisper`
  in the GET response shape.

### Container images
- `parent-full`: **rebuild + retag to v4.0.9 required**.
- Others unchanged. Helm `version: 0.14.9`, `appVersion: v4.0.9`.

### Breaking changes
- None.

## [4.0.8] - 2026-04-21

### Bug fixes
- **B38 — Autonomous/Plugins/Orchestrator Settings saves silently
  no-op'd.** `applyConfigPatch` had no case-branches for
  `autonomous.*`, `plugins.*`, or `orchestrator.*` dot-path keys,
  so unknown keys fell through the switch without error. The
  handler still returned 200, so both the PWA (v4.0.1 card rollout)
  and the mobile client (v0.33.x) showed the save as successful
  but nothing landed in `config.yaml` or the live Config. Added
  case-branches for all 17 keys (7 autonomous + 3 plugins + 4
  orchestrator + the 3 effort aliases). Also added a `default:`
  branch that logs unknown keys to stderr so future schema drift
  surfaces instead of silently dropping. Closes [issue #19](https://github.com/dmz006/datawatch/issues/19).
  - 2 new unit tests in `internal/server/applyconfigpatch_b38_test.go`
    — one exercises all 14 first-class fields round-trip; the other
    verifies that an unknown key in the same patch doesn't block
    known keys from landing.

### Container images
- `parent-full`: **rebuild + retag to v4.0.8 required** (daemon
  binary change).
- Other images unchanged.
- Helm: `version: 0.14.8`, `appVersion: v4.0.8`.

### Breaking changes
- None.

## [4.0.7] - 2026-04-21

### Bug fixes
- **B35 — diagram viewer "Failed to load docs/architecture.md"**:
  the service worker (`sw.js`) was serving the v4.0.3 `app.js` /
  `diagrams.html` out of a cache it never invalidated. Bumped the
  cache name from `datawatch-v1` → `datawatch-v2` (old cache is
  deleted on the next activation) and made `/docs/*` +
  `/diagrams.html` network-first, so fresh docs show up without a
  hard-reload.
- **B36 — internal ticket IDs in PWA UI**: stripped `(BL24+BL25)`,
  `(BL33)`, `(BL117)`, `(BL41)` from Settings card titles and
  field labels. Added a project rule forbidding BL/F/B/S numbers
  in any operator-facing string (web, mobile, comm, CLI user
  output); internal refs stay in CHANGELOG / backlog / code
  comments.
- **B37 — wrong RTK install command**: CLI auto-install fallback
  and `docs/rtk-integration.md` now point at the upstream
  installer `curl -fsSL https://raw.githubusercontent.com/rtk-ai/rtk/refs/heads/master/install.sh | sh`
  instead of the old release-asset URL.

### Docs
- Closes [issue #16](https://github.com/dmz006/datawatch/issues/16)
  — openapi.yaml now documents:
  - `/api/schedules` (plural — the path the server actually serves)
    GET/POST/DELETE with the missing `session_id` + `state` query
    params the PWA relies on.
  - `/api/profiles`, `/api/profiles/projects`, `/api/profiles/clusters`
    (none of which were in the spec).
  - Fleshed-out `ScheduledCommand` schema with `session_id`,
    `command`, `run_at`, and `state` fields that the server emits.
  - Resynced to `internal/server/web/openapi.yaml`. 55 paths total.
- Added **BL170 feature-completeness audit** to the pending
  backlog: every shipped feature must have operator doc + OpenAPI
  entry (both copies) + MCP tool + CLI command where applicable
  + comm reachability + flow diagram where it adds a new path +
  architecture mention. Audit task will surface + fill gaps.

### Container images
- `parent-full`: **rebuild + retag to v4.0.7 required** (web UI
  bundle + docs tree change).
- Other images unchanged.
- Helm: `version: 0.14.7`, `appVersion: v4.0.7`.

### Breaking changes
- None.

## [4.0.6] - 2026-04-20

### Added
- **Self-update progress bar in the PWA.** The `/api/update`
  handler now streams a new `update_progress` WebSocket message on
  every ~64 KB / 250 ms (whichever lands first) during the release
  asset download. The PWA pops a fixed-bottom overlay with:
  - the target version,
  - a filled progress bar (indeterminate-striped when the server
    doesn't send Content-Length),
  - live `<downloaded> of <total>` byte counts,
  - phase chip (starting → downloading → installed → restarting →
    or failed with the error string),
  - auto-dismiss 1.5 s after the WS reconnects to the new binary.
  Failed phase keeps the overlay for 15 s so the operator can read
  the error.
- `installPrebuiltBinary(version, progressFn)` and
  `installBareBinary(url, version, progressFn)` accept a callback
  invoked with `(downloaded, total)` during the download stream;
  CLI self-update callers pass `nil` and continue printing their
  own text progress to stdout.

### Container images
- `parent-full`: **rebuild + retag to v4.0.6 required** (daemon
  binary + web UI bundle change).
- Others unchanged.
- Helm: `version: 0.14.6`, `appVersion: v4.0.6`.

### Breaking changes
- None for config/REST consumers. `SetUpdateFuncs` signature
  changed in-tree only (Go package callers need to add the
  progress-callback parameter); out-of-tree callers of the server
  package are expected to be rare.

## [4.0.5] - 2026-04-20

### Bug fixes
- **B33 follow-up** — The "Input Required" yellow banner dismiss
  still wasn't sticking: the v4.0.2 fix used a signature-based
  "new prompt → un-dismiss" branch that was too sensitive. The
  daemon re-captures the terminal between polls and the first 200
  chars of the prompt context shift slightly, so the dismiss was
  reset on almost every render and the banner kept coming back.
  Fix: dismiss is now sticky for the entire `waiting_input`
  episode. It only clears when the session transitions out of
  `waiting_input`. Matches the original operator spec ("if it goes
  away after closing and can't reopen, that is ok").
- **Dismiss button visibility** — the X button grew 22×22 → 32×32,
  got a subtle filled background, and its opacity went from 0.7 to
  0.95 so it reads as a real close affordance rather than a faint
  decoration.
- **Swagger UI load error on `/api/docs`** — two lines in
  `docs/api/openapi.yaml` had unquoted colons inside scalar values
  ("(default: user home directory)" and "Accepts {id} or {ids:
  [...]}"), which js-yaml 5 rejected. Both wrapped in quotes; spec
  now parses cleanly (51 paths).

### Container images
- `parent-full`: **rebuild + retag to v4.0.5 required** (web UI
  asset bundle change).
- Other images unchanged.
- Helm: `version: 0.14.5`, `appVersion: v4.0.5`.

### Breaking changes
- None.

## [4.0.4] - 2026-04-20

### Bug fixes
- **B34** — Tmux command lands the text but requires a second Enter
  to actually submit. The v4.0.2 fix (trim trailing `\n`) was
  necessary but not sufficient: the real root cause is that modern
  bracketed-paste TUIs (claude-code, ink, opencode, Textual apps)
  fold a single-tmux-call `Enter` into the paste event so it becomes
  "newline inside input" rather than "submit". Fix: `SendKeys` now
  always uses the two-step pattern — `-l` literal push, 120 ms
  settle, explicit `Enter` keypress — that v3.6's B30 introduced for
  scheduled commands. The settle is imperceptible for interactive
  use. Existing `SendKeysWithSettle(..., 0)` also clamps to the
  default rather than falling back to the broken single-call path.

### Changed — diagram viewer rework
- **`/diagrams.html` now browses the real docs tree.** The v4.0.3
  viewer shipped a curated set of diagrams inlined in JS, which the
  operator correctly flagged as not what was asked for. v4.0.4
  rewrites it as a proper docs browser:
  - The build-time `sync-docs` Makefile target copies
    `docs/**/*.md` into `internal/server/web/docs/`, which the
    existing `//go:embed web` directive picks up. Static fileserver
    makes every doc reachable at `/docs/...`.
  - Viewer has a left-hand file picker (grouped: Architecture /
    Flow / Subsystems / Plans / API) with a filter box. Each file
    loads, its Mermaid blocks are extracted client-side and rendered
    via Mermaid.js. Every diagram gets its own zoom (+/- or
    Ctrl+wheel), pan (drag), reset, and fullscreen (Esc to exit).
  - Files with no Mermaid blocks link out to GitHub for the full
    narrative rather than trying to render markdown in-place.

### Container images
- `parent-full`: **rebuild + retag to v4.0.4 required** (daemon
  binary change for B34; embedded web assets grow by ~1.2 MB of
  markdown).
- Other images unchanged.
- Helm: `version: 0.14.4`, `appVersion: v4.0.4`.

### Breaking changes
- None.

## [4.0.3] - 2026-04-20

### Added — mobile-client parity
Closes / addresses 10 open GitHub issues filed by the datawatch-app
(mobile) team. All wire shapes match the proposals in those issues.

- **`POST /api/backends/active`** (issue #7) — switch the default
  LLM backend for new sessions. Validates against the registered
  backend set, persists to `session.llm_backend`, updates the live
  Manager via a new name-only `SetLLMBackendName` method so running
  workers keep their existing backend until they exit.
- **`GET/PATCH/DELETE /api/channels[/{id}]`** (issue #8) —
  messaging-channel enumeration + enable/disable toggle across all
  9 supported backends (Discord, Slack, Telegram, Matrix, Twilio,
  ntfy, email, webhook, GitHub webhook). `POST /api/channels`
  create returns 501 for v4.0.3 with a pointer to `PUT /api/config`
  for the provider-specific schema (a later patch can add dedicated
  per-type create handlers).
- **OpenAPI documentation** for the already-implemented endpoints
  the mobile team needs documented (issues #5, #6, #9, #10, #11,
  #12, #13): `POST /api/sessions/delete`, `GET /api/cert`,
  `GET /api/sessions/timeline`, `GET /api/{ollama,openwebui}/models`,
  `GET /api/logs`, `GET /api/interfaces`, `POST /api/restart`.
  `docs/api/openapi.yaml` grew from 1347 → 1485 lines; the web-
  served copy `internal/server/web/openapi.yaml` was resynced.

### Added — architecture diagram viewer
- **`/diagrams.html`** — standalone PWA page with six key diagrams
  (architecture overview, PRD-DAG orchestrator, ephemeral worker
  spawn, autonomous PRD flow, plugin framework, memory + wake-up
  stack) rendered client-side with Mermaid.js. Each diagram gets
  zoom (+/-/mousewheel), pan (drag), reset, and fullscreen toggle
  (Esc to exit). Addresses the operator's feedback that horizontal
  expansion in the markdown render isn't enough to browse dense
  diagrams comfortably. Linked from Settings → API Tools.

### Container images
- `parent-full`: **rebuild + retag to v4.0.3 required** (daemon
  binary + web UI assets change).
- Other images unchanged.
- Helm: `version: 0.14.3`, `appVersion: v4.0.3`.

### Breaking changes
- None. Every v4.0.2 config loads unchanged.

## [4.0.2] - 2026-04-20

### Bug fixes
- **B32** — Tmux and scheduled commands lands the payload but requires
  a second Enter to submit. Root cause: when the command text carried
  a trailing `\n` (copy-paste, a stored schedule entry, a comm
  channel payload that ended with a newline, etc.), tmux interpreted
  the `\n` inside the TUI input buffer as "newline inside input" and
  the explicit `Enter` keypress that `SendKeys` / `SendKeysWithSettle`
  appended afterwards just added another blank line instead of
  submitting. Fix: both send paths now strip trailing `\r\n\r\n...`
  from `keys` before appending the explicit `Enter`. +3 unit tests
  covering the trim helper, `SendKeys` with trailing `\n`, and
  `SendKeysWithSettle` with multiple trailing newlines.
- **B33** — PWA "Input Required" yellow banner stayed visible after
  the operator answered the prompt; only disappeared on a session
  reconnect. Fix: auto-dismiss the banner client-side when the user
  sends input (regular send, tmux-direct send, quick-input keys); add
  a manual X button that dismisses per-session; track a prompt
  signature so a new distinct prompt re-shows the banner even if a
  previous round was dismissed.

### Container images
- `parent-full`: **rebuild + retag to v4.0.2 required** (daemon
  binary change in the tmux send path, plus the web UI asset bundle).
- All other images: no change.
- Helm: `version: 0.14.2`, `appVersion: v4.0.2`.

### Breaking changes
- None.

## [4.0.1] - 2026-04-20

### Added — v4.0.x follow-ups (patch)
- **BL85 RTK auto-update REST surface** — `GET /api/rtk/version`,
  `POST /api/rtk/check`, `POST /api/rtk/update`. Reuses the
  pre-existing `rtk.CheckLatestVersion` + `rtk.UpdateBinary` +
  `rtk.StartUpdateChecker` machinery that was already wired at
  startup when `rtk.auto_update` is true. The background checker
  does a GitHub query per `update_check_interval`; the new REST
  endpoints let the operator trigger fresh checks + installs on
  demand from any channel.
- **BL166 tools-ops helm re-add** — get.helm.sh is reachable from
  buildkit now; the `helm` binary is back in `agent-base`-derived
  `tools-ops` image, installed from the official tarball with
  arch detection.
- **Directory picker "create folder"** — `POST /api/files` with
  `{path, name}` creates a directory under the operator-configured
  root path. Name must be a single component (no path traversal);
  parent must already exist; root-path clamp enforced identically
  to GET listing. Returns 409 on collision.
- **BL117 real GuardrailFn** — orchestrator guardrails now route
  through `/api/ask` with a focused system prompt per guardrail
  name (rules, security, release-readiness,
  docs-diagrams-architecture). Unparseable LLM output → `warn`
  (doesn't halt the graph); network failures → `warn`. v1 stub
  replaced in-place.
- **Autonomous executor → session.Manager wiring** — REST
  `POST /api/autonomous/prds/{id}/run` now walks the task DAG via
  `Manager.Run`. `SpawnFn` = loopback to `/api/sessions/start`,
  so each autonomous Task becomes a real F10 worker session.
  `VerifyFn` = `/api/ask` with a strict-JSON response contract.
  Falls back to v4.0 status-only mode when `SetExecutors` isn't
  called (bare-daemon tests).
- **Plugin hot-reload via fsnotify** — BL33 registry gets a
  `Watch(ctx)` method that watches the plugin discovery dir and
  re-runs `Discover()` 500 ms after create/remove/rename bursts.
  Wired at startup when `plugins.enabled`. SIGHUP / `POST
  /api/plugins/reload` still work.
- **Web UI Settings cards** — new General-tab sections for
  Autonomous (7 fields), Plugins (3 fields), Orchestrator (4
  fields). Closes the full-parity gap flagged in the v4.0.0
  release notes; every operator-tunable v4.0 knob now reaches from
  YAML + REST + MCP + CLI + comm + web UI.
- **OpenAPI resync** — `internal/server/web/openapi.yaml` synced
  from `docs/api/openapi.yaml`; the web-served API docs now
  include all autonomous / plugins / orchestrator paths.

### Closed / reclassified
- ✅ Aperant integration reviewed and **skipped** — AGPL-3.0
  license (incompatible with datawatch distribution), Electron
  desktop app with no headless API, sits on top of the same
  claude-code layer datawatch already wraps. Borrowing worktree +
  self-QA ideas into the BL24 roadmap as prior art alongside
  nightwire; no integration.
- 🧊 **F7 libsignal — frozen**. 3–6 mo signal-cli replacement
  deferred until there's a concrete operational need. Plan kept
  at `docs/plans/2026-03-29-libsignal.md` for later.

### Container images
- `parent-full`: **rebuild + retag to v4.0.1 required** (daemon
  binary change: new endpoints + wiring).
- `tools-ops`: **rebuild required** — helm re-added.
- All other images (agent-*, lang-*, validator): no change.
- Helm: `version: 0.14.1`, `appVersion: v4.0.1`.

### Breaking changes
- None. Every v4.0.0 config loads unchanged; new fields are
  optional with existing defaults.

## [4.0.0] - 2026-04-20

### Added — Sprint S8 (PRD-DAG orchestrator — major release)
- **BL117** — PRD-DAG orchestrator with guardrail sub-agents.
  Composes BL24 autonomous PRDs into a graph; each PRD is attested
  by a configured set of guardrails (`rules`, `security`,
  `release-readiness`, `docs-diagrams-architecture`) before the DAG
  advances. `block` verdict halts; `warn` records; `pass` clears.
  - New package `internal/orchestrator/`: `Graph`, `Node`,
    `Verdict`, `Runner` (Kahn topo-sort, verdict aggregation),
    JSONL store, API adapter.
  - Wired into `main.go` with PRDRunFn loopback to
    `/api/autonomous/prds/{id}/run` and a v1 stub GuardrailFn
    (real BL103-validator-image-per-guardrail lands in v4.0.x).
  - Plugin contract: register `on_guardrail` on BL33 plugins to
    author a real guardrail today.
  - 9 unit tests; full suite 1165 green.

### REST
```
GET    /api/orchestrator/config
PUT    /api/orchestrator/config
POST   /api/orchestrator/graphs
GET    /api/orchestrator/graphs
GET    /api/orchestrator/graphs/{id}
DELETE /api/orchestrator/graphs/{id}
POST   /api/orchestrator/graphs/{id}/plan
POST   /api/orchestrator/graphs/{id}/run
GET    /api/orchestrator/verdicts
```

### Parity (full per the rule)
- 9 new MCP tools: `orchestrator_config_get/set`,
  `orchestrator_graph_list/create/get/plan/run/cancel`,
  `orchestrator_verdicts`.
- 1 new CLI command: `datawatch orchestrator` with 9 subcommands.
- Comm via `rest` passthrough.
- `orchestrator:` YAML block (5 keys).

### Configuration
```yaml
orchestrator:
  enabled:                false
  default_guardrails:     ["rules", "security", "release-readiness", "docs-diagrams-architecture"]
  guardrail_timeout_ms:   120000
  guardrail_backend:      ""
  max_parallel_prds:      2
```

### Docs
- `docs/api/orchestrator.md` — operator + AI-ready usage doc.
- `docs/plans/2026-04-20-bl117-prd-dag-orchestrator.md` — design doc.
- **`docs/plans/RELEASE-NOTES-v4.0.0.md` — comprehensive cumulative
  release notes covering every BL/Fxx shipped since v3.0.0**, organized
  by theme (agent platform, sessions+productivity, intelligence,
  observability, operations, cost+audit, parity backfills, messaging,
  backends, memory, extensibility, mobile, bugs). Operator directive
  2026-04-20: v4.0.0 positioned as the milestone release with
  comprehensive retrospective.

### Container images
- `parent-full`: rebuild required.
- Helm: `version: 0.14.0`, `appVersion: v4.0.0`.

### Breaking changes
- None in the REST/CLI/MCP/YAML surface. Every v3.x config loads
  without change; new `orchestrator:` block is optional and disabled
  by default.

## [3.11.0] - 2026-04-20

### Added — Sprint S7 (Plugin framework)
- **BL33** — Subprocess plugin framework. Auto-discovery under
  `<data_dir>/plugins/<name>/` via `manifest.yaml` + executable
  entry. Line-oriented JSON-RPC over stdio. 4 hooks in v1:
  `pre_session_start`, `post_session_output`,
  `post_session_complete`, `on_alert`. Fan-out chaining for filter
  hooks in plugin-name alphabetical order.
  - Rejected `.so` plugins as brittle (Go-version + CGO + glibc
    locks); rejected embedded Lua/JS as runtime bloat.
  - Security model: plugins run with daemon privileges. Documented
    explicitly in `docs/api/plugins.md`.
  - 8 new unit tests (discovery, invoke, timeout, fan-out,
    enable/disable).
  - Design doc: `docs/plans/2026-04-20-bl33-plugin-framework.md`.

### REST
```
GET    /api/plugins                     list
POST   /api/plugins/reload              rescan dir
GET    /api/plugins/{name}              one plugin
POST   /api/plugins/{name}/enable
POST   /api/plugins/{name}/disable
POST   /api/plugins/{name}/test         synthetic hook invocation
```

### Parity (full per the rule)
- 6 new MCP tools: `plugins_list`, `plugins_reload`, `plugin_get`,
  `plugin_enable`, `plugin_disable`, `plugin_test`.
- 1 new CLI command: `datawatch plugins` with 6 subcommands.
- Comm reachable via the `rest` passthrough.
- New YAML block `plugins:` (4 keys, all reachable from every channel).

### Configuration
```yaml
plugins:
  enabled:    false
  dir:        ~/.datawatch/plugins
  timeout_ms: 2000
  disabled:   []
```

### Docs
- `docs/api/plugins.md` — operator + AI-ready usage doc with
  security disclosure, Python example, contract reference.

### Container images
- `parent-full`: rebuild required.
- Helm: `version: 0.13.0`, `appVersion: v3.11.0`.

## [3.10.0] - 2026-04-20

### Added — Sprint S6 (Autonomous PRD decomposition)
- **BL24 + BL25** — LLM-driven PRD → Stories → Tasks decomposition with
  independent verification. Inspired by HackingDave/nightwire's
  autonomous module; design map at
  `docs/plans/2026-04-20-bl24-autonomous-decomposition.md`.
  - New package `internal/autonomous/` — models (PRD/Story/Task/
    Learning/VerificationResult/LoopStatus), JSON-lines store under
    `<data_dir>/autonomous/`, decomposition prompt + LLM-output
    parser (handles fences, smart quotes, // comments per nightwire
    `prd_builder.py`), security pattern scanner (port of nightwire
    `_DANGEROUS_PATTERNS`), Manager + Executor (Kahn topo-sort,
    auto-fix retry on verifier failure), API adapter for the REST
    surface.
  - Disabled by default — opt in with `autonomous.enabled: true`.
  - 15 new unit tests.

### REST
```
GET    /api/autonomous/status              loop snapshot
GET    /api/autonomous/config              read config
PUT    /api/autonomous/config              replace config
POST   /api/autonomous/prds                create PRD
GET    /api/autonomous/prds                list PRDs
GET    /api/autonomous/prds/{id}           one PRD with tree
DELETE /api/autonomous/prds/{id}           cancel + archive
POST   /api/autonomous/prds/{id}/decompose run LLM decomposition
POST   /api/autonomous/prds/{id}/run       kick executor
GET    /api/autonomous/learnings           extracted learnings
```

### Parity (full per the rule)
- 10 new MCP tools: `autonomous_status`, `autonomous_config_get/set`,
  `autonomous_prd_list/create/get/decompose/run/cancel`,
  `autonomous_learnings`.
- 1 new CLI command: `datawatch autonomous` with 10 subcommands.
- Comm reachable via the `rest` passthrough on every channel.
- New YAML block `autonomous:` (10 keys, all reachable from every channel).

### Configuration
```yaml
autonomous:
  enabled:                false
  poll_interval_seconds:  30
  max_parallel_tasks:     3
  decomposition_backend:  ""
  verification_backend:   ""
  decomposition_effort:   "thorough"
  verification_effort:    "normal"
  stale_task_seconds:     0
  auto_fix_retries:       1
  security_scan:          true
```

### Docs
- `docs/api/autonomous.md` — operator + AI-ready usage doc.
- `docs/plans/2026-04-20-bl24-autonomous-decomposition.md` — design
  map showing each nightwire component → datawatch primitive.

### Container images
- `parent-full`: rebuild required.
- Helm: `version: 0.12.0`, `appVersion: v3.10.0`.

## [3.9.0] - 2026-04-20

### Added — Sprint S5 (Backends + chat UI)
- **BL20** — Backend auto-selection routing rules:
  `session.routing_rules: [{pattern, backend, description}]` config +
  `/api/routing-rules` GET/POST + `/api/routing-rules/test` for dry
  runs. Wired into the start handler before existing fallthrough.
  Docs: `docs/api/routing-rules.md`.
- **BL78 / BL79 / BL72** — Chat-mode backend recipes documented at
  `docs/api/chat-mode-backends.md`. Every backend already supports
  `output_mode: "chat"`; the doc covers Gemini, Aider, Goose
  configuration and the OpenCode memory-hook reuse.

### Parity (full per the rule)
- 2 new MCP tools: `routing_rules_list`, `routing_rules_test`.
- 1 new CLI subcommand: `datawatch routing-rules` with `list` + `test`.
- Comm reachable via the `rest` passthrough.

### Container images
- `parent-full`: rebuild required.
- Helm: `version: 0.11.0`, `appVersion: v3.9.0`.

## [3.8.0] - 2026-04-20

### Added — Sprint S4 (Messaging + UI polish)
- **BL15** — Rich-preview formatter (`messaging.FormatAlert`): detects
  fenced ``` code blocks and emits backend-flavoured output (Telegram
  MarkdownV2, Slack/Discord passthrough, Signal " │ "-prefixed mono).
  Operator opt-in: `session.alerts_rich_format: true`.
- **BL31** — Device aliases: `session.device_aliases` config +
  `/api/device-aliases` GET/POST/DELETE for dynamic update. Used by
  router for `new: @<alias>: <task>` routing.
- **BL42** — Quick-response assistant: `POST /api/assist` with
  configurable `session.assistant_backend`, `assistant_model`,
  `assistant_system_prompt`. Wraps `/api/ask`.
- **BL69** — Splash screen logo: `session.splash_logo_path` +
  `session.splash_tagline` config; `GET /api/splash/logo` serves the
  custom file; `GET /api/splash/info` returns render context.

### Parity (full per the rule)
- 5 MCP tools: `assist`, `device_alias_list/upsert/delete`, `splash_info`.
- 3 CLI subcommands: `datawatch assist`, `device-alias`, `splash-info`.

### Container images
- `parent-full`: rebuild required.
- Helm: `version: 0.10.0`, `appVersion: v3.8.0`.

## [3.7.3] - 2026-04-20

### Added — Sprint Sx2 (Comm + Mobile parity)

Closes the remaining parity gap from Sx2 audit (2026-04-20). Every
v3.5.0–v3.7.0 endpoint is now reachable from comm channels (Signal /
Telegram / Discord / etc.) and documented for the mobile client.

- **Comm router commands** (`internal/router/commands.go` +
  `internal/router/sx2_parity.go`):
  - `cost [<full_id>]` — token + USD rollup
  - `stale [<seconds>]` — list stuck running sessions
  - `audit [actor=x action=y limit=N ...]` — query operator audit log
  - `cooldown` / `cooldown set <seconds> [reason]` / `cooldown clear`
  - `rest <METHOD> <PATH> [json]` — generic REST passthrough so any
    Sx endpoint not bound to a top-level command is still reachable
- **Help text** updated to advertise the new commands.
- **Mobile API surface doc** added at `docs/api/mobile-surface.md`
  listing every v3.5–v3.7.x endpoint that's mobile-friendly, with
  use-case mapping to the [`datawatch-app`] paired client.

### Container images
- `parent-full`: rebuild required.
- Helm: `version: 0.9.3`, `appVersion: v3.7.3`.

## [3.7.2] - 2026-04-20

### Added — Sprint Sx (Parity backfill)

Per-channel parity for every endpoint shipped in v3.5.0–v3.7.0.
Previously REST + YAML only; now reachable from MCP and CLI as well.

- **MCP tools (20 new in `internal/mcp/sx_parity.go`):**
  `ask`, `project_summary`, `template_list`, `template_upsert`,
  `template_delete`, `project_list`, `project_upsert`,
  `project_alias_delete`, `session_rollback`, `cooldown_status`,
  `cooldown_set`, `cooldown_clear`, `sessions_stale`, `cost_summary`,
  `cost_usage`, `cost_rates`, `audit_query`, `diagnose`, `reload`,
  `analytics`. Each is a thin REST loopback proxy through the local
  HTTP server, so MCP and REST share validation + business logic.

- **CLI subcommands (9 new in `cmd/datawatch/cli_sx_parity.go`):**
  `ask`, `project-summary`, `template`, `projects`, `rollback`,
  `cooldown`, `stale`, `cost`, `audit` — each routes through the
  running daemon's REST API and pretty-prints the response.

### Verified
- **Functional smoke** against a live daemon on port 18080 confirmed
  every Sx endpoint returns valid JSON and POST/DELETE round-trips
  persist (project upsert + delete, cooldown set + clear).
  Cost-rate override (`session.cost_rates.claude-code: 0.005/0.020`)
  applied correctly to the live `Manager`.

### Container images
- `parent-full`: rebuild required.
- Helm: `version: 0.9.2`, `appVersion: v3.7.2`.

## [3.7.1] - 2026-04-19

### Fixed — config rule compliance
- **BL6** — Cost rates were hard-coded in `DefaultCostRates()` with
  no YAML/REST surface, violating the no-hard-coded-config rule.
  Added `session.cost_rates: {backend: {in_per_k, out_per_k}}` config
  block + `GET/PUT /api/cost/rates` REST surface. Operators can now
  override per-backend rates via every channel (YAML, REST, hot-reload
  via SIGHUP / `POST /api/reload`). Empty entries fall through to the
  built-in defaults.

### Docs
- New `docs/api/cost.md` covers the full cost-tracking surface
  including rate override.
- `docs/config-reference.yaml` now lists `default_effort`,
  `stale_timeout_seconds`, `rate_limit_global_pause`, and
  `cost_rates` (every config field added in v3.5.0–v3.7.0).

### Container images
- `parent-full`: rebuild required.
- Helm: `version: 0.9.1`, `appVersion: v3.7.1`.

## [3.7.0] - 2026-04-19

### Added — Sprint S3 (Cost + observability tail)
- **BL6** — Cost tracking: `Session.tokens_in/tokens_out/est_cost_usd`,
  per-backend rate table, `GET /api/cost`, `POST /api/cost/usage`.
- **BL86** — `cmd/datawatch-agent/` standalone stats binary (linux-amd64,
  linux-arm64). `GET /stats` returns GPU + CPU + memory + disk JSON.
- **BL9** — Operator audit log: append-only JSONL at
  `<data_dir>/audit.log`, `GET /api/audit` with multi-field filters.

### Container images
- `parent-full`: rebuild required.
- `datawatch-agent`: ships as bare binary; container wrap is a
  follow-up.
- Helm: `version: 0.9.0`, `appVersion: v3.7.0`.

## [3.6.0] - 2026-04-19

### Added — Sprint S2 (Sessions productivity)
- **BL5** — `/api/templates` CRUD + `template:` field on session start.
- **BL26** — Recurring schedules via `recur_every_seconds`/`recur_until`.
- **BL27** — `/api/projects` CRUD + `project:` field on session start.
- **BL29** — Git pre-/post-checkpoint tags + `POST /api/sessions/{id}/rollback`.
- **BL30** — Global rate-limit cooldown: `/api/cooldown` (GET/POST/DELETE),
  config gate `session.rate_limit_global_pause`.
- **BL40** — `GET /api/sessions/stale` + config `session.stale_timeout_seconds`
  (default 1800).

### Container images
- `parent-full`: rebuild required.
- Helm: `version: 0.8.0`, `appVersion: v3.6.0`.

## [3.5.0] - 2026-04-19

### Added — Sprint S1 (Quick Wins + UI Diff)
- **BL1** — IPv6 listener support: every server bind site routes
  through `net.JoinHostPort` (`internal/server/listen_addr.go`).
  `server.host: "::"` enables dual-stack listening.
- **BL34** — `POST /api/ask` read-only LLM ask (Ollama + OpenWebUI
  backends) with no session, no tmux. Docs: `docs/api/ask.md`.
- **BL35** — `GET /api/project/summary?dir=<abs>` returns git status,
  recent commits, per-project session list, and aggregate stats.
  Docs: `docs/api/project-summary.md`.
- **BL41** — `Session.Effort` field (quick/normal/thorough); default
  via `session.default_effort` config (hot-reloadable).
- **F14** — Live cell DOM diffing in the web UI's session list:
  `tryUpdateSessionsInPlace()` per-card diff before falling back to
  full re-render. Eliminates flicker + scroll-reset on WS updates.

### Container images
- `parent-full`: rebuild required.
- Helm: `version: 0.7.0`, `appVersion: v3.5.0`.

## [3.4.1] - 2026-04-19

### Fixed
- Cross-build: `internal/server/diagnose.go` referenced `syscall.Statfs`
  which is unix-only, breaking the windows build. Split into
  `diagnose_unix.go` (linux/darwin/bsd) + `diagnose_windows.go`
  (skipped with informational note). v3.4.0 binaries shipped were
  built post-fix; v3.4.1 brings the source tag back in sync so
  `make cross` works against the tag.

## [3.4.0] - 2026-04-19

### Added — Operations (complete)
- **BL17** — Hot config reload via `POST /api/reload` and SIGHUP.
  Re-applies hot-reloadable session settings to the live Manager;
  reports `requires_restart` for fields that need a full restart.
- **BL22** — `datawatch setup rtk` auto-installs platform-matched
  RTK binary into `~/.local/bin/rtk` when absent.
- **BL37** — `GET /api/diagnose` health snapshot covering tmux,
  session manager, config, data-dir, disk, goroutines.
- **BL87** — `datawatch config edit` visudo-style safe editor
  (validates YAML on save, loops on failure).

### Container images
- `parent-full`: rebuild required.
- Helm: `version: 0.6.0`, `appVersion: v3.4.0`.

## [3.3.0] - 2026-04-19

### Added — Observability (partial)
- **BL10** — Session diff summary captured into `Session.DiffSummary`
  after `PostSessionCommit` runs `git diff --shortstat HEAD~1..HEAD`.
- **BL11** — Pure-logic anomaly detectors: stuck-loop, long-input-wait,
  duration-outlier. Operator-tunable thresholds.
- **BL12** — `GET /api/analytics?range=Nd` day-bucket aggregation:
  session count, completed/failed/killed splits, avg duration, success
  rate.

### Deferred
- **BL86** — Remote GPU/system stats agent. Needs a separate
  `datawatch-agent` binary product; deferred to dedicated release.

### Container images
- `parent-full`: rebuild required.
- Helm: `version: 0.5.0`, `appVersion: v3.3.0`.

## [3.2.0] - 2026-04-19

### Added — Intelligence (partial)
- **BL28** — Quality gates wired into pipeline executor: pre-run
  test baseline, post-run comparison, optional block-on-regression
  via `pipeline.quality_gates.*` config.
- **BL39** — Circular dependency detection in pipeline DAGs.
  `NewPipeline` now returns `(*Pipeline, error)` and rejects cycles.
  DFS three-coloring with ordered cycle-path output.

### Deferred
- **BL24** (autonomous task decomposition, 1-2 weeks) and **BL25**
  (independent verification, depends on BL24) deferred to a dedicated
  v3.5.0 release. See `docs/plans/RELEASE-NOTES-v3.2.0.md` rationale.

### Container images
- `parent-full`: rebuild required (executor embeds new behaviour).
- Helm: `version: 0.4.0`, `appVersion: v3.2.0`.

## [3.1.0] - 2026-04-19

### Fixed
- **B30** — Scheduled commands no longer require a 2nd Enter to
  execute. `TmuxManager.SendKeysWithSettle` splits text push and
  Enter into two `tmux send-keys` calls with a configurable delay
  (`session.schedule_settle_ms`, default 200 ms).

### Added — Testing Infrastructure
- **BL89** — `TmuxAPI` interface + `FakeTmux` for in-memory tests.
  `mgr.WithFakeTmux()` helper swaps the tmux dependency.
- **BL90** — httptest-based API endpoint test coverage.
- **BL91** — Direct MCP handler tests (no stdio/SSE transport).

### Docs
- `docs/plans/RELEASE-NOTES-v3.1.0.md`
- Container-maintenance rule added to `docs/plans/README.md`.

### Container images
- `parent-full`: rebuild required (daemon behaviour changed).
- All agent, lang, tools, validator images: unchanged.
- Helm chart bumped to `version: 0.3.0`, `appVersion: v3.1.0`.

## [3.0.1] - 2026-04-19

### Fixed
- **B31** — In-app `datawatch update` now downloads the bare binary
  that matches every release's actual asset names, rather than a
  goreleaser-style tar.gz/zip that was never shipped.

## [3.0.0] - 2026-04-19

See `docs/plans/RELEASE-NOTES-v3.0.0.md` for the full F10 + mobile
API surface release notes.

## [2.4.1] - 2026-04-12

### Fixed — B6: Function Parity Across All Channels

**API endpoints added (3):**
- `POST /api/memory/reindex` — re-embed all memories after model change
- `GET /api/memory/learnings?q=&limit=` — list/search task learnings
- `GET /api/memory/research?q=&limit=` — deep cross-session/cross-project search

**MCP tools added (2, total 43):**
- `memory_import` — import memories from JSON (output of memory_export)
- `config_set` — change config values via MCP (key/value)

**Comm channel commands added (3):**
- `memories stats` — show memory count, size, encryption status
- `memories export` — export all memories (truncated for messaging)
- `kg invalidate <s> <p> <o>` — invalidate KG triples from comm channels

**CLI subcommands added (12):**
- `datawatch memory remember/recall/list/stats/forget/learnings/export/reindex/research`
- `datawatch pipeline start/status/cancel`
- All route through running daemon via test/message API with TLS support

### Docs
- MCP TLS cert trust instructions (Cursor, VS Code, Claude Desktop, system-wide, mobile)
- B6 parity audit plan with comprehensive gap analysis

## [2.4.0] - 2026-04-12

### Added — F15: Session Chaining (Pipelines) — Complete
- **Pipeline API endpoints** — `GET/POST /api/pipelines` (list/start), `GET /api/pipeline?id=X` (status), `POST /api/pipeline?id=X&action=cancel`. Full REST access.
- **Pipeline MCP tools** — `pipeline_start`, `pipeline_status`, `pipeline_cancel`, `pipeline_list`. 4 new tools (41 total).
- **Pipeline config** — `pipeline.max_parallel` (default 3), `pipeline.default_backend`. Configurable via API, web UI, CLI, comm channels, config file.
- **Pipeline web UI config** — Settings > General > Pipelines (Session Chaining) section with max parallel and default backend fields.
- **`POST /api/memory/save`** (BL88) — direct REST endpoint for saving memories.

### Fixed
- **B5: Session history controls off-screen** — replaced absolute popup with fixed bottom bar. Select All/Delete/Cancel buttons always visible on mobile.
- **B4: Input bar disappearing** — scroll mode reset on re-render, display:none safety net, periodic 3s self-heal check.
- **RTK web UI config** — RTK section added to Settings > LLM tab (7 fields).
- **Pipeline docs** — expanded with examples, access methods table, memory-aware session examples.
- **README diagram** — removed tracker IDs, kept all components.

### Docs
- **config-reference.yaml** — pipeline section added.
- **commands.md** — pipeline usage with examples.
- **README** — memory-aware session and cross-session research examples.

### Tests
- 211 tests, go vet clean, deps verified.

## [2.3.8] - 2026-04-11

### Added
- **TLS certificate download** — `GET /api/cert` serves the CA certificate. `?format=der` returns DER-encoded .crt for Android. Download link in Settings > Comms > Web Server with expandable install instructions for Android and iPhone.
- **Auto-generated cert includes hostname + all IPs** — SANs now include machine hostname and all local network interface IPs, not just localhost.
- **TLS port defaults to 8443** — enabling TLS now runs HTTP on 8080 (with redirect) and HTTPS on 8443 by default (dual-mode).
- **BL87 backlog plan** — `datawatch config edit` visudo-style safe config editor with encrypted config support.
- **BL86 backlog plan** — remote GPU/system stats agent for monitoring Ollama servers on different machines.

### Fixed
- **PWA icons** — regenerated with Chrome headless renderer. Previous ImageMagick PNGs were dark/muddy. Now clearly shows purple eye with targeting reticle.
- **PWA standalone mode docs** — documented HTTPS requirement, CA cert install for Android and iPhone, Tailscale option.
- **Chrome notification docs** — added to operations.md with 4 options (TLS, Tailscale, CA cert install, Chrome flags).

### Docs
- **AGENT.md** — added release vs patch rules (release = full GH release, default = commit only), strengthened no-hardcoded-config rule.
- **operations.md** — PWA install guide, Chrome notifications, CA cert install instructions.

## [2.3.6] - 2026-04-11

### Added
- **RTK config in web UI** — Settings > LLM tab now has RTK (Token Savings) section with all 7 config fields (enabled, binary, show_savings, auto_init, auto_update, update_check_interval, discover_interval).

### Docs
- **Plans README refactored** — clean separation: rules → unclassified → open bugs → open features → backlog by category (37 items) → completed (42 items). Quick wins marked with ⚡. Testing results moved to testing.md.

### Tests
- 211 tests, go vet clean, gosec clean (pre-existing only), deps verified.

## [2.3.5] - 2026-04-11

### Added — BL85: RTK Auto-update Check
- **Version check** — `CheckLatestVersion()` queries GitHub releases API for latest RTK version, compares with installed version. Caches result with timestamp.
- **Auto-update** — `UpdateBinary()` downloads platform-specific binary from GitHub release assets, replaces current binary, verifies. Enabled via `rtk.auto_update: true`.
- **Background checker** — `StartUpdateChecker()` runs periodic version checks (configurable interval, default daily). Auto-updates if enabled and binary is writable.
- **Stats integration** — Monitor page shows `rtk_latest_version` and `rtk_update_available` in stats data.
- **Config options** — `rtk.auto_update` (bool) and `rtk.update_check_interval` (seconds) configurable via API, web UI, comm channels, config file.
- **Documentation** — config-reference.yaml updated, plan in backlog-plans.md.

## [2.3.4] - 2026-04-11

### Fixed — BL84: Tmux Scroll Mode (fully working)
- **Root cause: `capture-pane` doesn't capture copy-mode view** — `tmux capture-pane -e -p` always captures the bottom of the pane buffer, not the scrolled view in copy-mode. Fixed `CapturePaneVisible()` to detect copy-mode via `#{pane_in_mode}` and use `-S`/`-E` offset flags based on `#{scroll_position}` to capture the actual scrolled view.
- **Browser tested**: enter scroll → PgUp ×2 (web shows numbers 1-67, was 123-200) → ESC exits and restores.

### Fixed
- **Chat bubble colors** — user messages brighter blue (right-aligned), assistant stronger green (left-aligned), system left-aligned with gray. All roles visually distinct.
- **Channel `?` help** — only visible when Channel tab active, hidden on Tmux tab.

## [2.3.3] - 2026-04-11

### Fixed — BL84: Tmux Scroll Mode
- **Scroll controls moved to bottom bar** — PgUp/PgDn/ESC buttons now appear in a dedicated bar at the bottom (replacing the input bar), with large touch-friendly buttons for phone use. Previously crammed into the narrow top toolbar.
- **`tmux copy-mode` command** — uses native tmux `copy-mode` command instead of unreliable `send-keys C-b [` two-step sequence.
- **Dropdown scroll commands fixed** — `__scroll__`, `__pageup__`, `__pagedown__`, `__quitscroll__` in both command handlers now route through the proper functions instead of raw sendkey.
- **Channel help `?` hidden on Tmux tab** — only shows when Channel tab is active.
- **ESC added to system commands** — accessible from saved commands dropdown for phone keyboards.

### Browser Tested
- Scroll button enters copy-mode, bottom bar appears ✓
- PgUp ×2 scrolls through history ✓
- ESC exits, input bar restored ✓
- Channel `?` hidden on Tmux tab ✓

## [2.3.2] - 2026-04-11

### Added — BL84: Tmux History Scrolling
- **Scroll toolbar button** — "↕ Scroll" button in terminal toolbar enters tmux copy-mode (`Ctrl-b [`)
- **Scroll controls** — when active, PgUp/PgDn buttons and red ESC button replace the Scroll button
- **ESC exits scroll mode** — sends `q` to tmux and restores normal toolbar
- **ESC in system commands** — added to saved commands dropdown for phone keyboard access (no hardware ESC key)
- **Multi-key sendkey** — `sendkey` command now supports space-separated key sequences (`C-b [`)
- **Scroll commands in dropdown** — page up, page down, quit scroll, scroll mode all available in Commands dropdown

### Added
- **27 backlog plans** — comprehensive plans for all unplanned items in `docs/plans/2026-04-11-backlog-plans.md`

### Browser Tested
- Scroll button visible in toolbar ✓
- Click Scroll → PgUp/PgDn/ESC controls appear ✓
- PgUp scrolls through history ✓
- ESC exits and restores normal toolbar ✓

## [2.3.1] - 2026-04-11

### Fixed — B2: Claude Code Prompt Detection (zero false positives)
- **Status bar check** — `esc to interrupt` in Claude's status bar immediately rejects prompt matches. Most effective guard — catches 100% of active states.
- **Expanded active indicators** — 21 verbs (was 6): Reading, Writing, Searching, Editing, Crunching, Finagling, Reasoning, Considering, past-tense timing patterns ("Crunched for 52s"), task checkbox detection.
- **Output velocity check** — skips prompt detection if log file had output within last 5 seconds. Fast timeout increased 1s → 3s.
- **Oscillation backoff** — if session flips running↔waiting >3 times in 60s, debounce auto-increases to 30s.
- **Result:** zero false positives in 60-second live test (was ~4/min).

### Completed — BL83: ACP Chat UI (all phases)
- **Transient status indicators** — "Thinking..." and "Processing..." system messages render as animated indicators that auto-disappear when the assistant response starts streaming. Not persisted in chat history.
- **Step reason display** — ACP step-start events include reason text when available.
- **Fade-in animation** — transient indicators animate in smoothly.

### Tests
- 211 tests across 40 packages — all passing
- gosec clean (pre-existing only), go vet clean, deps verified

## [2.3.0] - 2026-04-11

Consolidates all v2.2.3–v2.2.9 fixes into a stable release. Highlights:

### Added
- **Prompt debounce** (`detection.prompt_debounce`, default 3s) — waits for sustained inactivity before alerting on detected prompts. Configurable via API, web UI, comm channels, config file.
- **Notification cooldown** (`detection.notify_cooldown`, default 15s) — rate limits needs-input notifications per session.
- **Session reconnect on restart** (B3) — `backend_state.json` persists ACP connection state and Ollama/OpenWebUI conversation history. Auto-reconnects on daemon startup with full context.
- **ACP rich chat UI** (BL83) — OpenCode-ACP defaults to `output_mode: chat` with SSE event streaming mapped to chat messages (thinking, processing, streaming response, ready).
- **Chat output_mode in web settings** — all backends now offer terminal/log/chat in the output mode dropdown.
- **Detection timing in web UI** — Settings > Detection Filters shows numeric inputs for prompt debounce and notify cooldown.

### Fixed
- **xterm.js 20s load → 32ms** (B1) — TailOutput was reading entire 82MB log file; now seeks to last 64KB. Output batched at 100ms. Send channel 256→2048. ResizeObserver leak fixed. pane_capture crash guard added. Pending capture buffer for subscribe race.
- **Duplicate user prompts in chat** — removed double emission from Ollama/OpenWebUI backends.
- **Ollama chat not using API** — Launch() now routes to LaunchChat() when chat emitter is set.
- **Chat-mode false waiting_input** — sessions with output_mode=chat skip tmux prompt detection.
- **Alert oscillation noise** — running↔waiting_input state changes no longer generate bundled remote alerts.
- **Input bar 60px gap** — session detail view now extends to page bottom when nav is hidden.

### Docs
- operations.md: chat mode, debounce, reconnect, terminal performance sections
- testing.md: v2.3.0 test summary (211 tests), pre-release validation checklist
- llm-backends.md: chat mode backend table, all three output modes documented
- config-reference.yaml: prompt_debounce, notify_cooldown, opencode_acp section
- commands.md: configure command added to command list
- plan-attribution.md: detailed feature maps for nightwire and mempalace

### Tests
- 211 tests across 40 packages (5 new debounce tests)
- Pre-release: go vet clean, gosec (pre-existing only), deps verified, API/comm/WS/reconnect validated

## [2.2.9] - 2026-04-11

### Added — B3: LLM Session Reconnect on Daemon Restart
- **Backend state persistence** — `backend_state.json` saved to each session's tracking directory containing backend-specific connection state (ACP: baseURL + sessionID, Ollama: host + model + conversation history, OpenWebUI: URL + model + apiKey + conversation history).
- **Automatic reconnect on startup** — `ReconnectBackends()` scans all running sessions, reads persisted state, and re-establishes connections: ACP re-subscribes to SSE event stream, Ollama/OpenWebUI restore conversation history into conversation managers.
- **Conversation context preserved** — multi-turn conversation history survives daemon restarts. Follow-up prompts maintain full context from previous exchanges.
- **Dead session cleanup** — sessions whose tmux pane no longer exists are automatically marked complete during reconnect.
- **State cleanup on session end** — `backend_state.json` removed when session completes/fails/kills to prevent orphaned state files.
- **Exported ChatMessage types** — `ollama.ChatMessage` and `openwebui.ChatMessage` exported for cross-package persistence.

### Tested
- Ollama: session created → state saved → 2 messages exchanged → daemon restarted → session restored with history → follow-up prompt answered correctly using context from before restart → state cleaned on kill.

## [2.2.8] - 2026-04-11

### Fixed — B1: xterm.js Stability and Faster Load
- **TailOutput 82MB hang** (root cause) — `TailOutput()` read the entire log file (82MB for long sessions) on every WebSocket subscribe. Now seeks to last 64KB. **Subscribe+pane_capture: 79ms (was 20+ seconds).**
- **WebSocket output flood** — each output line broadcast individually, filling the 256-entry send channel and dropping `pane_capture` messages. Output now batched per session at 100ms intervals. Channel increased to 2048.
- **xterm.js ResizeObserver leak** — observers accumulated on session switches (never disconnected). Now stored in state and cleaned up in `destroyXterm()`.
- **Terminal write crash** — `pane_capture` handler had no try/catch; null terminal reference after navigation caused uncaught errors. Added error boundary with auto-recovery (destroyXterm on failure).
- **Missed initial pane_capture** — subscribe fired before `initXterm()`, so the first capture was received when `state.terminal` was null and silently dropped. Added `_pendingPaneCapture` buffer that flushes when terminal initializes.
- **Frame rate throttle** — pane_capture writes now capped at ~30fps to prevent xterm.js buffer overload from rapid screen captures.

## [2.2.7] - 2026-04-11

### Added
- **BL83: OpenCode-ACP rich chat interface** — ACP sessions now default to `output_mode: chat` with full rich chat UI. SSE events (`message.part.delta`, `session.status`, `session.idle`, `step-start`/`step-finish`) map to structured chat messages: streaming response chunks, Processing/Thinking/Ready system indicators, error messages. Chat emitter wired via `SetACPChatEmitter`.

### Fixed
- **ACP showed CLI text instead of chat** — ACP defaulted to `output_mode: log` showing raw tmux log lines. Now defaults to `chat` with conversation managed via SSE event stream.
- **ACP duplicate user prompt** — initial task now emitted as user chat message from Launch only (follow-up prompts emitted by session manager, not duplicated).

### Docs
- **README.md** — rich chat UI description updated to include OpenCode-ACP alongside Ollama and OpenWebUI.
- **llm-backends.md** — chat mode table showing all backends with default output modes and conversation managers.
- **memory-usage-guide.md** — updated to list ACP as a default chat-mode backend.
- **config-reference.yaml** — added `opencode_acp` section with `output_mode: chat` default.

## [2.2.6] - 2026-04-10

### Fixed
- **Ollama chat Launch routing** — `Launch()` now checks for chat emitter and routes to `LaunchChat()` (API-based conversation manager) instead of `ollama run` in tmux. Previously, chat-mode Ollama sessions fell back to interactive tmux mode.
- **Chat-mode prompt detection skip** — sessions with `output_mode=chat` now skip tmux capture-pane and idle timeout prompt detection entirely. Chat sessions use their conversation manager for state; tmux shell prompts caused false `waiting_input` transitions.
- **Notification oscillation suppression** — `running↔waiting_input` state changes no longer generate bundled remote alerts or local alert store entries. Only the `onNeedsInput` handler (with cooldown) sends notifications for prompts. Eliminates alert noise from Claude Code's brief prompt visibility during tool calls.

## [2.2.5] - 2026-04-10

### Fixed
- **Detection config fully exposed** — `prompt_debounce` and `notify_cooldown` now accessible through all configuration methods: API (GET/PUT), MCP (`get_config`), Web UI (Settings > Detection Filters > Timing), comm channels (`configure detection.prompt_debounce=5`), and CLI.

### Added
- **Web UI detection timing controls** — numeric inputs for prompt debounce and notify cooldown in Settings > Detection Filters section with auto-save on change.
- **API config endpoints** — `detection.prompt_debounce` and `detection.notify_cooldown` added to both `GET /api/config` response and `PUT /api/config` accepted fields.
- **Comm channel configure help** — `configure help` and `configure list` now include detection timing keys.

### Docs
- **config-reference.yaml** — added `prompt_debounce` and `notify_cooldown` with descriptions and defaults.
- **commands.md** — added `configure` command to the command list.

## [2.2.4] - 2026-04-10

### Fixed
- **Prompt detection debounce** — all prompt detection paths (idle timeout, capture-pane, screen capture) now go through a centralized `tryTransitionToWaiting` method with configurable debounce. When a prompt is first detected, the system waits `prompt_debounce` seconds (default: 3) before transitioning to `waiting_input`. If new output arrives during this window, the timer resets. Prevents false positives during LLM thinking pauses.
- **Notification cooldown** — `onNeedsInput` notifications are throttled to at most once per `notify_cooldown` seconds (default: 15) per session. Eliminates notification floods where 30+ alerts were sent while the LLM was still computing.
- **Unified state transitions** — all 7 code paths that set `StateWaitingInput` now route through `tryTransitionToWaiting`, ensuring consistent debounce and cooldown behavior across all backends (Claude Code, Ollama, OpenWebUI, OpenCode-ACP, filters).

### Added
- **`detection.prompt_debounce`** config option — seconds to wait after detecting a prompt before confirming (default: 3). Configurable globally or per-backend.
- **`detection.notify_cooldown`** config option — minimum seconds between repeated needs-input notifications per session (default: 15). Configurable globally or per-backend.
- **5 debounce unit tests** — debounce suppression, reset on output, skipDebounce for protocol markers, notification cooldown, config defaults.

### Tests
- 211 tests across 40 packages (5 new debounce tests)

## [2.2.3] - 2026-04-10

### Fixed
- **Duplicate user prompt in chat** — user messages were emitted by both the session manager and the backend's `sendAndStream`, causing prompts to appear twice. Removed duplicate emission from Ollama and OpenWebUI backends; initial launch task uses `emitUser` flag since it bypasses `SendInput`.
- **Input bar not flush with page bottom** — session detail view reserved 60px for the bottom nav even though nav is hidden. Added `view-full` CSS class that sets `bottom: 0` when in session detail, eliminating the empty gap below the input bar.
- **Ollama chat processing indicator** — added "Processing..." system message before Ollama API call so users see immediate feedback.
- **Chat bottom spacing** — added 24px bottom padding to chat area so the last message isn't flush against the input bar.

## [2.2.2] - 2026-04-09

### Fixed
- **Ollama chat conversation manager** — Ollama chat-mode sessions now use the `/api/chat` HTTP endpoint with streaming instead of `ollama run` in tmux. Enables structured `chat_message` events, model loading visibility, and proper response streaming in the rich chat UI.
- **OpenWebUI processing indicator** — chat-mode sessions now emit a "Processing..." system message before the API request, so users see immediate feedback after sending a prompt.
- **Chat scroll history** — chat area CSS fixed with `overflow-y: auto !important` and `max-height: 70vh`, allowing users to scroll back through conversation history.

### Enhanced
- **Ollama conversation manager** — new `internal/llm/backends/ollama/conversation.go` provides Go-native multi-turn conversation support via Ollama's `/api/chat` endpoint with streaming JSON responses.
- **Chat memory commands for all backends** — memory command interception wired at session manager level, so Ollama chat sessions get the same memory commands as OpenWebUI.

## [2.2.1] - 2026-04-10

### Fixed
- **Chat mode output isolation** — chat-mode sessions (`outputAreaTmux` → `chatArea`) now create the chat div directly in HTML instead of renaming after render. Eliminates race condition where raw tmux output could leak into chat before the rename. Both `output` and `raw_output` WS handlers explicitly skip when `chatArea` exists.
- **Channel mode + chat** — when a session has both channel tabs and chat mode, the Tmux tab is labeled "Chat" and renders the chat area instead of terminal.

### Added
- **5 new MCP tools** (37 total):
  - `get_config` — read datawatch config (optional section filter)
  - `delete_session` — delete a completed/failed session
  - `restart_session` — restart a session with same task
  - `get_stats` — system statistics (CPU, memory, GPU, sessions, Ollama)
  - `memory_export` — export all memories as JSON backup

## [2.2.0] - 2026-04-10

### Added — Chat UI Features (BL77, BL80, BL81, BL82)
- **BL77: Ollama chat mode** — Ollama now defaults to `output_mode: chat` with rich chat UI, avatars, timestamps, memory command quick bar, and in-chat memory commands. Memory command interception works for ALL chat-mode backends (not just OpenWebUI).
- **BL80: Image and diagram rendering** — detects image URLs (`![alt](url)`) and renders inline in chat bubbles. Mermaid code blocks shown with diagram label.
- **BL81: Thinking/reasoning overlay** — detects `<think>...</think>` or `<thinking>...</thinking>` blocks from reasoning models, renders as collapsible "Thinking..." section with expandable details.
- **BL82: Conversation threads** — when >6 messages, older messages collapse into an expandable "N earlier messages" thread. Recent messages always visible.

### Fixed
- **Schedule bar not refreshing** — scheduled jobs now refresh the schedule bar on session state changes. Previously executed schedules stayed visible until manual refresh.
- **Chat mode hides raw tmux output** — chat-mode sessions no longer show CLI commands, printf, echo from tmux pane. Only structured `chat_message` events render in the chat UI.
- **Comm channel input shows in chat** — when input arrives from Signal/Telegram/API/CLI to a chat-mode session, it now emits a `chat_message` event so it appears as a user bubble in the chat UI.

### Enhanced
- **Markdown rendering** — added headers (##/###), horizontal rules, numbered lists, links, in addition to existing code blocks/bold/italic.

### Tests
- 206 tests across 40 packages (1 new: Ollama defaults to chat)

## [2.1.3] - 2026-04-10

### Fixed
- **Schedule input bug** — `submitScheduleInput` called `r.text()` on parsed JSON from `apiFetch`. Fixed to use `.then`/`.catch` pattern. Schedule now works correctly from web UI.

### Enhanced
- **Rich chat UI** — complete visual overhaul for `output_mode: chat` sessions:
  - Rounded message bubbles with user (right-aligned) and assistant (left-aligned) layout
  - Avatar icons (U/AI/S) with role-colored backgrounds
  - Timestamps on every message
  - Hover action buttons: Copy to clipboard, Remember (save to memory)
  - Animated typing indicator (bouncing dots during streaming)
  - Memory command quick bar: memories, recall, kg query, research buttons
  - Empty state with chat icon and instructions
  - Enhanced code blocks with borders and better contrast
  - System messages centered with subtle styling
  - Reusable: any backend can use `output_mode: chat` (not hardcoded to OpenWebUI)

## [2.1.2] - 2026-04-10

### Fixed
- **RTK instructions merge** — guardrails writer now appends RTK instructions to existing CLAUDE.md/AGENT.md when `rtk.enabled=true`, same as memory. Detects presence via `rtk-instructions` marker. Concise version with golden rule + key savings table.
- Previously RTK instructions were only added by `rtk init` — now they're auto-merged on session start if RTK is enabled and instructions are missing.

## [2.1.1] - 2026-04-10

### Fixed
- **Session guardrails merge with existing files** — `WriteSessionGuardrails` no longer skips projects that already have CLAUDE.md/AGENT.md. Instead, it reads the existing file and appends missing sections (memory instructions) without overwriting user content. Memory section only appended when `memory.enabled=true`. RTK instructions left to `rtk init`.
- **Conditional memory section** — session template's "Memory & Knowledge" section is stripped when memory is disabled, so sessions without memory don't get irrelevant instructions.
- **GuardrailsOptions** — new struct passed to `WriteSessionGuardrails` with `MemoryEnabled` and `RTKEnabled` flags read from config.

## [2.1.0] - 2026-04-10

### Added — Memory Intelligence (BL74, BL75, BL76)
- **BL74: Memory-aware session template** — session guardrails (AGENT.md/CLAUDE.md) now include memory instructions telling AI agents to proactively use `memory_recall`, `kg_query`, and `research_sessions`. Config: `memory.session_awareness` (default true).
- **BL75: `research_sessions` MCP tool + comm command** — deep cross-session research that searches ALL session outputs + memories + KG for a topic. Returns synthesized results with context. `research: <query>` from any comm channel.
- **BL76: Session awareness broadcast** — when a session completes/fails/is killed, broadcasts summary to all connected WS clients including other active sessions. Config: `memory.session_broadcast` (default true). Shows as toast notification in web UI.
- **CLAUDE.md memory instructions** — project CLAUDE.md updated with memory tool reference table for this project.
- Config: `memory.session_awareness`, `memory.session_broadcast` in YAML/web UI/API/comm channels.

### Tests
- 205 tests across 40 packages — all passing

## [2.0.2] - 2026-04-10

### Added
- **BL43: PostgreSQL+pgvector backend** — full memory store implementation using pgx driver. Vector search via application-side cosine similarity (works with or without pgvector extension). Deduplication, spatial search (wings/rooms), KG tables, encryption support. `memory.backend: postgres`, `memory.postgres_url: postgres://user:pass@host/db`.
- **Backend interface** — `memory.Backend` interface abstracts SQLite and PostgreSQL stores. Retriever, adapters, and all wiring work with either backend transparently.
- **KG unified adapter** — `KGUnified` wraps both SQLite KnowledgeGraph and PGStore KG methods, implementing router, HTTP server, and MCP interfaces.
- **7 PostgreSQL integration tests** — Save, dedup, vector search, spatial search, KG, stats, encryption. All tested against real PostgreSQL 17 + pgvector.

### Tests
- 205 tests across 40 packages (7 new PG integration tests)

## [2.0.1] - 2026-04-10

### Fixed
- **B28: Ollama test before enabling memory** — `POST /api/memory/test` performs full functional test (connect + embed + validate vector). Web UI "Test" button next to memory toggle. If test fails, toggle reverts with error message.
- **B29: Memory encryption docs** — `docs/encryption.md` now includes full Memory Content Encryption section (hybrid encryption, what's encrypted/visible, key management, configuration table).
- **Monitor tab scroll jump** — real-time stats updates now preserve scroll position (saves/restores `scrollTop` and `window.scrollY` around DOM rebuild).
- **Comms tab toggle switches** — Web Server and MCP Server settings now use proper toggle switches instead of On/Off buttons. All settings tabs use consistent toggle switch controls.

### Added
- **AGENT.md configuration accessibility rule** — every feature must have config in YAML, web UI, API, comm channel, MCP, and CLI. Must verify round-trip before marking complete.
- **`/api/memory/test` endpoint** — tests Ollama connectivity AND embedding capability (sends test phrase, validates non-zero vector response with dimension count).

## [2.0.0] - 2026-04-09

### Major Release — Memory Complete, Pipelines, Intelligence Infrastructure

This release completes the entire episodic memory system (25 BL items), adds session
chaining with DAG execution, quality gates, Ollama server monitoring, rich chat UI,
and conversation mining. **All memory backlog items (BL43-BL67) are now implemented.**

### Added — Session Chaining & Intelligence
- **F15: Session chaining** — `pipeline: task1 -> task2 -> task3` chains sessions in a DAG with parallel execution, dependency tracking, and cycle detection. Pipeline status/cancel commands. Fully integrated with session manager.
- **BL39: Cycle detection** — Kahn's algorithm validates DAG before execution, reports cycle path
- **BL28: Quality gates** — run test command before/after sessions, detect regressions vs preexisting failures, block on new failures. `CompareResults()` with REGRESSION/IMPROVED/STABLE classification
- **BL24: Task decomposition** — pipeline infrastructure supports LLM-driven decomposition (prompt tuning deferred)
- **BL25: Independent verification** — verification hook point in pipeline executor (prompt tuning deferred)

### Added — Monitoring & UI
- **BL71: Remote Ollama server stats** — real-time GPU/VRAM/model stats from Ollama API in Monitor dashboard. Models, running inference, disk usage. `/api/ollama/stats` endpoint. `ollama_stats` MCP tool
- **BL73: Rich chat UI** — markdown rendering (code blocks, bold, italic), typing indicator animation, streaming status, improved chat bubble styling for OpenWebUI/Ollama sessions

### Added — Documentation & Architecture
- **Root README** — complete rewrite with Memory & Intelligence section, updated architecture diagram (pipelines, quality gates, Ollama stats), 31+ MCP tools
- **Security** — gosec pre-release scan with `.gosec-exclude`, Slowloris protection (ReadHeaderTimeout)

### Tests
- 198 tests across 40 packages — all passing (new: 11 pipeline tests)

## [1.7.0] - 2026-04-09

### Added — Memory Tier 4 (9 features, all memory BL items complete)
- **BL47: Retention policies** — per-role TTL pruning (`PruneByRole`, `ApplyRetention`). Configurable `retention_session_days`, `retention_chunk_days`. Manual and learning memories kept forever by default.
- **BL51: Batch reindex** — `memories reindex` re-embeds all memories after model change. `Reindex()` method on retriever. MCP `memory_reindex` tool. Async execution with progress logging.
- **BL53: Learning quality scoring** — `SetScore()` on store for rating learnings 1-5. Score stored alongside summary.
- **BL49: Cross-project search** — `recall` already searches all projects via `RecallAll`. Documented as default behavior.
- **BL64: Cross-project tunnels** — `memories tunnels` shows rooms shared across multiple wings. `FindTunnels()` query groups by room with distinct wing count > 1.
- **BL59: Conversation mining** — `MineConversation()` ingests Claude JSONL, ChatGPT JSON, and generic JSON conversation exports. Normalizes, pairs user-assistant exchanges, stores as memories.
- **BL65: Claude Code save hook** — `hooks/datawatch_save_hook.sh` auto-saves to memory every N exchanges (default 15). Parses Claude Code transcript JSONL, extracts last exchanges, POSTs to `/api/test/message`.
- **BL66: Pre-compact hook** — `hooks/datawatch_precompact_hook.sh` saves topic summary before context window compression.
- **BL67: Mempalace import** — conversation mining supports generic JSON format compatible with mempalace exports.

### Fixed
- **Nil embedder crash** — `Remember()` now handles nil embedder gracefully (saves without vector) instead of panicking.

### Tests
- 187 tests across 39 packages — all passing (8 new Tier 4 tests)
- gosec scan: 185 findings (all expected, G104 excluded via `.gosec-exclude`)

## [1.6.1] - 2026-04-09

### Fixed
- **Memory config in API response** — `GET /api/config` now includes full `memory` and `proxy` sections with all fields. Previously these sections were missing from the manually-built response map, causing the web UI to show incorrect toggle states.
- **Memory toggle switches** — LLM tab now uses proper toggle switches (same as LLM backend checkboxes) instead of "On/Off" buttons. Boolean values correctly read from config (false no longer shows as "Off" when enabled).
- **Embedder host default** — shows the actual configured `ollama.host` value instead of a placeholder. Empty field means "using ollama.host" and displays the resolved value.
- **G112 Slowloris protection** — added `ReadHeaderTimeout` to all HTTP servers (main server: 10s, redirect server: 5s).

### Added
- **AGENT.md gosec rule** — pre-release security scan with `gosec ./...` required before every release. Documented expected suppressions (subprocess, SSRF, file inclusion) and must-fix categories.

## [1.6.0] - 2026-04-09

### Added — Memory Tier 3 (Enterprise & Integration)
- **BL61: MCP KG tools** — 5 new MCP tools: `kg_query`, `kg_add`, `kg_invalidate`, `kg_timeline`, `kg_stats`. Accessible via stdio (Claude Code, Cursor) and SSE (network LLMs).
- **BL54: KG REST API** — `GET /api/memory/kg/query?entity=`, `POST /api/memory/kg/add`, `POST /api/memory/kg/invalidate`, `GET /api/memory/kg/timeline?entity=`, `GET /api/memory/kg/stats`. Wing/room filter params on `/api/memory/list`.
- **`get_prompt` MCP tool** — get the last user prompt for a session (mirrors `copy_response` for prompts).

### Fixed
- **Memory config visibility** — `MemoryConfig` JSON serialization no longer uses `omitempty` on parent struct, so the entire memory section always appears in `/api/config` response. Toggle now correctly reflects enabled state.
- **Fallback chain in claude-code setup** — profiles and fallback chain field moved into the claude-code LLM backend config popup.

### Changed
- **Settings tab reorganization**:
  - **LLM tab**: LLM backends, Episodic Memory config, RTK, Detection/Output Filters, Saved Commands
  - **Comms tab**: Servers, Web Server, MCP Server, Proxy Resilience, Communication Config
  - **General tab**: Datawatch core, Auto-Update, Session, Notifications
- **Root README**: Updated architecture diagram with memory system, KG, response capture, 30 MCP tools. Updated feature list, doc index, Go version badge.
- **MCP tool count**: 30 tools (was 17) — session management (17) + memory (8) + KG (5)

### Tests
- 179 tests across 39 packages — all passing

## [1.5.2] - 2026-04-09

### Fixed
- **B27: Alerts now include user prompt** — alert body shows "Prompt: {what user asked}\n---\n{LLM response}" instead of just the response. All input paths (web UI, comm channel, MCP, direct tmux) capture the prompt via LastInput.
- **Memory stats show enabled/disabled** — Monitor card always shows memory status badge (enabled/disabled), encryption status with key fingerprint when encrypted. Previously only showed when enabled.

### Added
- **`prompt` command** — `prompt` or `prompt <id>` returns the last user input for a session. Works from all comm channels, API (`GET /api/sessions/prompt?id=`), and MCP (`get_prompt` tool).
- **Settings UI reorganization**:
  - Episodic Memory config moved from General → **LLM tab** (alongside RTK, detection filters)
  - Web Server, MCP Server, Proxy Resilience moved from General → **Comms tab** (after servers, before comm config)
  - Profiles & Fallback removed from General (belongs in claude-code backend setup)

### Changed
- Alert body format: `Prompt: {input}\n---\n{response}` when both are available

## [1.5.1] - 2026-04-09

### Added — Memory Encryption (BL68, BL70)
- **Hybrid content encryption**: XChaCha20-Poly1305 encrypts `content` and `summary` fields at rest while keeping embeddings and metadata (role, wing, room, timestamps) searchable. Enabled automatically when `--secure` mode is active or a `memory.key` file exists.
- **Key management**: `KeyManager` with Generate, Load, Fingerprint. Auto-detects key from `--secure` encKey or `{data_dir}/memory.key`. Key rotation via `RotateKey()` re-encrypts all content. Migration from plaintext via `MigrateToEncrypted()`.
- **Stats show encryption**: `memory_stats` and Monitor card display `encrypted: true/false` and `key_fingerprint`.
- **Config**: `memory.storage_mode` (summary/verbatim), `memory.entity_detection` toggle added to API config handler.

### Tests
- 179 tests across 39 packages (9 new encryption tests: roundtrip, wrong key, encrypted save/read/search, unencrypted preserved, key rotation, migration, fingerprint, key manager)

## [1.5.0] - 2026-04-09

### Added — Memory Tier 2 (5 features)
- **BL55: Spatial organization** — wing/room/hall columns for hierarchical memory structure. Auto-derive wing from project path, hall from role. `SearchFiltered()` with metadata filtering for +34pp retrieval improvement. `ListWings()`, `ListRooms()` taxonomy queries.
- **BL56: 4-layer wake-up stack** — L0 identity from `identity.txt`, L1 auto-generated critical facts from top learnings+manual, L2 topic-triggered room context, L3 deep search (existing recall). `WakeUpContext()` auto-loaded on session start alongside task-specific retrieval.
- **BL57: Temporal knowledge graph** — SQLite-backed entity-relationship triples with validity windows. `kg add/query/timeline/stats` commands from all comm channels. Point-in-time queries, invalidation support. Auto entity creation. `KnowledgeGraph` struct with full CRUD.
- **BL58: Verbatim storage mode** — `memory.storage_mode: verbatim` stores full prompt+response text instead of summaries. Higher retrieval accuracy at cost of storage.
- **BL60: Entity detection** — lightweight regex-based extraction of people (capitalized multi-word names), tools (Go/Docker/PostgreSQL/etc), and projects from text. `PopulateKG()` auto-adds detected entities to knowledge graph.

### Added — Plans & Governance
- **BL68-70: Memory encryption plan** — hybrid content encryption using XChaCha20-Poly1305. Key management (generate, rotate, import/export with key). Plan document created.
- **BL69: Splash screen enhancements** added to backlog
- **AGENT.md pre-release dependency audit** rule (72-hour stability window for upgrades)
- **Merged testing docs** — testing.md and testing-tracker.md combined into single organized document

### Tests
- 170 tests across 39 packages (12 new Tier 2 tests: spatial, KG, layers, entity detection)

## [1.4.0] - 2026-04-09

### Added — Memory Tier 1 (7 features)
- **BL63: Deduplication** — content hash (SHA-256) prevents storing identical memories. `Save()` returns existing ID on duplicate. New `content_hash` column with index.
- **BL62: Write-ahead log** — JSONL audit trail at `{data_dir}/memory-wal.jsonl` for all Save/Delete operations. `memories wal` command, `/api/memory/wal` endpoint, `memory_wal` MCP tool.
- **BL50: Embedding cache** — LRU cache wrapping the embedder avoids re-computing identical vectors. 1000 entry default, tracks hit rate in stats.
- **BL44: Auto-retrieve on session start** — when memory is enabled, new sessions automatically search for relevant past context and display it as a preamble in tmux. Filters to memories with >30% similarity.
- **BL52: Session output auto-index** — on session completion, output is chunked into ~500 word segments, embedded, and stored for granular semantic search via `recall`.
- **BL48: Memory browser enhancements** — role filter (manual/session/learning/chunk), date range filter (7d/30d/90d), export button. API: `/api/memory/list` supports `role`, `since`, `project` query params.
- **BL46: Export/import** — `GET /api/memory/export` downloads JSON backup, `POST /api/memory/import` restores. Dedup-aware import skips existing memories. WAL logs import operations.

### Added — API & MCP
- `/api/memory/export` GET — download all memories as JSON
- `/api/memory/import` POST — upload JSON backup, dedup-aware
- `/api/memory/wal` GET — view write-ahead log entries
- `/api/memory/list` supports `role`, `since`, `project` filter params
- `ListFiltered()`, `Export()`, `Import()`, `WALRecent()` on MemoryAPI and MemoryMCP interfaces

### Tests
- 158 tests across 39 packages (10 new memory Tier 1 tests)

## [1.3.1] - 2026-04-09

### Added
- **Memory statistics in Monitor tab**: real-time memory metrics card (total, manual, session, learning, chunk counts, DB size) in the Monitor dashboard with live WS updates
- **Memory browser in Monitor tab**: memory browser with search, list, and delete in the Monitor tab under Session Statistics. Memory stats card in Monitor dashboard with real-time updates
- **Memory REST API**: `GET /api/memory/stats`, `GET /api/memory/list`, `GET /api/memory/search?q=`, `POST /api/memory/delete` endpoints
- **MCP memory tools**: `memory_remember`, `memory_recall`, `memory_list`, `memory_forget`, `memory_stats`, `copy_response` — 6 new MCP tools for IDE integration
- **Rich text copy output**: `copy` command uses markdown formatting (bold header + code block) for Slack, Discord, Telegram backends that support RichSender
- **Splash screen 24h throttle**: startup splash only shows once per 24 hours unless version changed; shows "Updated" badge on new version
- **Ctrl-b tmux prefix**: system saved command for tmux prefix key in both session detail and card quick commands
- **AGENT.md monitoring rule**: all new features must include stats metrics, API endpoint, MCP tool, web UI card, comm channel access, and Prometheus metrics

### Changed
- Memory stats callback wired into stats collector for real-time broadcasting
- Remote alert bundler prefers captured response over screen scraping for all backends
- Memory browser and stats card in Monitor tab (stats card only visible when memory enabled)

## [1.3.0] - 2026-04-09

### Added
- **Episodic memory system** (BL23/BL32/BL36): vector-indexed project knowledge with semantic search. Pure-Go SQLite backend (no cgo, no root), Ollama or OpenAI embeddings, configurable via YAML/web UI/API/comm channels. Enterprise PostgreSQL+pgvector backend option. New `internal/memory` package with store, embeddings, retriever, and chunker.
- **Memory commands**: `remember:`, `recall:`, `memories`, `forget`, `learnings` — accessible from Signal, Telegram, Slack, Discord, web UI, and all comm channels
- **Memory settings card**: Settings -> General -> Episodic Memory with backend selector (SQLite/PostgreSQL), embedder selector (Ollama/OpenAI), model, host, top-K, retention, auto-save, learnings toggles
- **Response capture system**: captures LLM's last response on every running->waiting_input transition from `/tmp/claude/response.md` (Claude Code) or tmux fallback. Stored on session, broadcast via WS, used in alerts and memory
- **`copy` command**: `copy` or `copy <id>` returns last LLM response from any comm channel or web UI
- **Response viewer modal**: clickable response icon on session cards and session detail opens scrollable modal with rich-formatted response content, copy-to-clipboard button
- **OpenWebUI chat UI** (B26): structured chat bubbles for OpenWebUI interactive sessions with WS streaming. New `chat` output mode, `chat_message` WS type, CSS chat bubble styles
- **Proxy mode phases 4-5** (F16): PWA reverse proxy (`/remote/{server}/`), HTTP client pool with circuit breaker, offline command queue with auto-replay, `/api/servers/health` endpoint, ProxyConfig UI
- **Memory documentation**: `docs/memory.md` with architecture, flow diagrams, configuration, usage
- **13 new memory backlog items** (BL43-BL54): spatial organization, wake-up stack, knowledge graph, verbatim storage, mining, entity detection, MCP tools, WAL, deduplication, tunnels, hooks
- **13 new mempalace-inspired backlog items** (BL55-BL67): import, cross-project search, embedding cache, batch reindex

### Fixed
- **Alert body uses response content**: alerts now show the actual LLM response instead of raw screen-scraped terminal output with ANSI artifacts
- **Terminal scroll mess on new commands**: `\x1b[3J` clears scrollback buffer on each pane capture frame, preventing scroll accumulation
- **Session exit flashes shell prompt**: pane capture display frozen once session state is complete/failed/killed; frames containing `DATAWATCH_COMPLETE:` suppressed
- **Channel ready re-render resets terminal**: `handleChannelReadyEvent` now dismisses banner in-place without full `renderSessionDetail` re-render

### Changed
- OpenWebUI interactive sessions default to `output_mode: chat` instead of `terminal`
- `modernc.org/sqlite` added as pure-Go dependency (no cgo required)

## [1.2.2] - 2026-04-02

### Fixed
- **Session restart resumes in-place**: restarting a completed/failed/killed session now reuses the same session ID, tmux session, and tracking directory instead of creating a duplicate entry. New `POST /api/sessions/restart` endpoint and `Manager.Restart()` method handle the full lifecycle (kill tmux, reset state, relaunch with resume).
- **Session resume "No conversation found"**: `Launch()` now sets `--session-id` with a deterministic UUID derived from the datawatch session ID, so `--resume` can find the conversation later. Previously Claude generated a random UUID that the resume logic couldn't predict.
- **Backend loading blocks page**: `/api/backends` now always returns cached data immediately when the cache is stale, refreshing version checks in the background. Previously a stale cache (>5 min) blocked the response while running `--version` on every backend binary.
- **"Needs input" notification spam**: added deduplication checks to the capture-pane and idle-timeout prompt detection paths. Previously these paths fired `onNeedsInput` every 2 seconds for the same prompt, spamming Signal/Telegram/WebSocket notifications. The structured channel path already had the check — now all three paths are consistent.

### Added
- **`POST /api/sessions/restart` endpoint**: restarts a terminal-state session in-place with LLM conversation resume support
- **Claude-code backend tests**: unit tests for `deriveSessionUUID` determinism, `isUUID` validation, and launch/resume session ID consistency

## [1.2.1] - 2026-04-02

### Fixed
- **Session detail loading splash**: when opening a session, the terminal area now shows a "Connecting to session…" splash with the datawatch logo while waiting for the first terminal capture. Previously the terminal was blank/black during the delay. Includes retry logic (re-subscribes after 5s, up to 3 attempts) and error indicator with manual retry and dismiss buttons if connection fails.

## [1.2.0] - 2026-04-02

### Added
- **Voice input via Whisper transcription**: voice messages sent via Telegram or Signal are automatically transcribed to text and routed as commands. Uses OpenAI Whisper from a Python venv (CPU-only). Configurable model size (tiny/base/small/medium/large) and language (99 languages supported via ISO 639-1 codes, or `auto` for detection). Per-user language preferences deferred to multi-user access control feature.
- **Whisper settings card in web UI**: Settings → Voice Input (Whisper) with model dropdown, language field, venv path, and enable toggle
- **Whisper config in REST API**: `GET /api/config` includes `whisper` section; `PUT /api/config` supports `whisper.enabled`, `whisper.model`, `whisper.language`, `whisper.venv_path`
- **`messaging.Attachment` type**: messages from backends can now carry file attachments with MIME type and local path
- **Telegram voice/audio download**: Telegram backend detects `Voice` and `Audio` messages, downloads via Bot API, and attaches to the message for transcription
- **Signal attachment parsing**: Signal backend now parses `attachments` from signal-cli envelopes and propagates them through the messaging pipeline
- **Transcription echo**: when a voice message is transcribed, the router echoes `Voice: <text>` back to the channel before processing the command
- **RTK Token Savings card in Monitor**: stats dashboard now renders RTK version, hooks status, tokens saved, average savings %, and command count when RTK is installed
- **`POST /api/test/message` endpoint**: simulates incoming messaging backend messages through the router, returning the responses that would be sent back. Enables testing all comm channel commands (help, list, version, stats, new, send, kill, configure, schedule, alerts) without actual Signal/Telegram connections
- **Proxy mode**: single datawatch instance relays commands and session output to/from multiple remote instances. Enables multi-machine management from one Signal/Telegram group or one PWA.
  - **WebSocket proxy relay**: `/api/proxy/{server}/ws` — bidirectional WS relay between browser and remote instance with token injection
  - **Aggregated sessions**: `GET /api/sessions/aggregated` — parallel fetch from all remotes + local, sessions tagged with `server` field
  - **Remote command routing**: `status`, `send`, `kill`, `tail` auto-fallback to remote when session not found locally
  - **`new: @server: task` syntax**: route session creation to a specific remote server from any comm channel
  - **Aggregated `list` command**: shows sessions from all configured servers in one response
  - **Server badges**: session cards in web UI show server name when viewing aggregated sessions
  - **Server picker**: selecting a remote server in Settings reconnects WS and routes all API calls through proxy
  - **Web-only daemon keepalive**: daemon stays alive with just HTTP server (no messaging backends required) for proxy-only deployments
- **`proxy` package** (`internal/proxy`): `RemoteDispatcher` with session discovery cache (30s TTL), `ForwardCommand`, `ListAllSessions`, `ForwardHTTP`, token auth injection

### Fixed
- **MCP channel reconnect delay**: navigating to an already-established claude session showed "Waiting for MCP channel…" for a long time. Root cause: the web UI did not populate its `channelReady` cache from session data on initial WS sync. Fixed with three sync points: initial sessions message, session state updates, and direct session object check in the detail view
- **Configure command broken on all comm channels**: the `configure` chat command used `http.Post` to call `/api/config`, but that endpoint only accepts PUT. Changed to `http.NewRequest(PUT)`. This affected Signal, Telegram, Discord, Slack, and all other messaging backends
- **`server.Version` hardcoded**: the HTTP server's `/api/health` and `/api/info` endpoints always returned version `"1.0.0"` instead of the actual build version. Fixed by wiring `server.Version = Version` from main.go

### Documentation
- **messaging-backends.md**: added Voice Input section with setup, model guide, supported languages; added feature parity matrix (threading, markdown, buttons, file upload, voice by backend)
- **config-reference.yaml**: added `whisper` config section with all fields documented
- **setup.md**: full Whisper setup section (venv, ffmpeg, config, model guide, 99 languages); full RTK setup section; profiles and fallback chains section; encryption clarified — can enable at any time, not just at init
- **operations.md**: `/healthz` + `/readyz` Kubernetes probe docs; Prometheus `/metrics` endpoint with metric table; profiles/fallback section; voice input section; test message endpoint documentation

## [1.0.2] - 2026-04-01

### Added
- **OpenWebUI interactive mode**: replaced curl/python3 single-shot backend with a native Go conversation manager. Maintains message history for multi-turn follow-ups, streams SSE responses directly, and routes input through the Go HTTP client instead of tmux send-keys. No external dependencies (curl, python3) needed.
- **RTK integration**: detects RTK installation, shows version/hooks status in stats API, collects token savings metrics (total_saved, avg_savings_pct, total_commands). Auto-runs `rtk init -g` if hooks not installed and `auto_init: true`. Only activates for RTK-supported LLM backends (claude-code, gemini, aider).
- **Channel feature parity review**: comprehensive audit of all 11 communication backends. Identified gaps in threading, rich formatting, interactive components, and file handling. Prioritized plan created.
- **Threaded conversations**: session alerts are now threaded per-session on Slack (via `thread_ts`), Discord (via `MessageThreadStart`), and Telegram (via `reply_to_message_id`). Thread IDs stored on Session for follow-up replies. Backends without threading fall back to flat messages.
- **Rich markdown formatting**: `RichSender` interface for platforms supporting formatted text. Alert headers in bold, tasks in italics, output context in code blocks. Implemented for Slack (mrkdwn), Discord (native markdown), and Telegram (Markdown parse_mode).
- **RTK setup wizard**: `datawatch setup rtk` CLI command — detects RTK, installs hooks, enables integration. `--disable` flag to turn off.
- **Schedule management improvements**: edit/delete buttons on all scheduled events, multi-select with checkboxes + bulk delete, "on input" time parsing fixed, improved preset buttons (5/15/30 min, 1/2 hr, "On next prompt")
- **Compact LLM filter badges**: short backend names with session count badges, replacing full-name buttons that overflowed horizontally
- **Interactive buttons on alerts**: Slack Block Kit buttons ([Approve] [Reject] [Enter]) and Discord component buttons on waiting_input alerts. `ButtonSender` interface. Falls back to text-only for backends without button support.
- **File upload on completion**: session output log uploaded to Slack/Discord thread when session completes. `FileSender` interface.
- **Profile CRUD API**: `/api/profiles` GET/POST/DELETE endpoints. Profile dropdown in New Session form. Profile field in session start API.
- **Multi-profile fallback chains**: named profiles with different accounts/API keys per backend. `session.fallback_chain` config auto-switches to the next profile on rate limit. Profile env vars applied to tmux session on launch. Configurable via YAML, web UI (Settings → Profiles & Fallback), and REST API.
- **`channel_ready` + `channel_port` session fields**: set automatically when the per-session MCP channel calls `/api/channel/ready`; exposed in REST API `/api/sessions`. Used for debounced state detection and per-session channel routing
- **Prompt context capture**: prompt alerts now include up to 10 surrounding screen lines (`prompt_context` field on Session) giving meaningful context about what is being asked, instead of a single matched line or noisy fallback
- **Alert title format**: all alert titles now use `hostname: name [id]: event` (e.g. `ralfthewise: myproject [a1b2]: running → waiting_input`) for consistent identification across local and remote channels
- **Rate-limit auto-continue**: when a rate limit is detected with a reset time, datawatch creates a persisted scheduled command to auto-resume the session after the limit resets — survives daemon restarts (replaces previous in-memory timer that was lost on reboot)
- **`ScheduleStore.CancelBySession`**: new method to cancel all pending scheduled commands for a session; called automatically on session kill and delete to prevent orphan entries
- **Channel tab bidirectional**: channel tab now shows outgoing sends (blue `→`), incoming replies (amber `←`), and notifications (purple `⚡`) — previously only showed Claude's rare `reply` tool calls
- **Per-session channel port routing**: `/api/channel/send` uses the session's actual channel port (stored on channel_ready) instead of a global fallback, enabling correct routing to per-session MCP servers on random ports
- **Quick command dropdown redesign**: commands dropdown now has System/Saved `<optgroup>` sections with visual divider and a "Custom..." option that reveals an inline text input for freeform prompts. Hardcoded quick buttons (y/n/Enter/Up/Down/Esc) removed from both session list and session detail — all consolidated into the dropdown

### Changed
- **Toast notifications**: title-only (truncated to 60 chars), no body text in toasts — full details remain in the Alerts view
- **Toast styling**: smaller font (10px), right-aligned text, subtle left accent border colored by level, lower-contrast background
- **MCP connection banner**: simplified to "Waiting for MCP channel…" — removed the prompt-specific "Accept prompt below to continue" text that was incorrect on reconnect/refresh

### Fixed
- **Spurious alerts on web connect/refresh**: `StartScreenCapture` now skips state detection on the first tick (baseline capture only), eliminating the flood of prompt-detection alerts when opening or refreshing a session in the web UI
- **Claude state detection accuracy**: active-processing check now scans lines above the `❯` prompt for spinner (`✢ Verb…`) and tool execution (`⎿ Running…`) indicators, skipping separators and status bars. Removed "esc to interrupt" from `activeIndicators` (it's Claude's permanent status bar, not an active-work signal). For `channel_ready` sessions, prompt must persist for 3 seconds (15 captures) before transitioning to `waiting_input`, preventing false triggers between tool calls
- **Prompt context noise**: `extractPromptContext` filters separator lines, shell launch commands, Claude startup warnings, spinners, and status bar fragments from the context shown in alerts
- **Rate-limit resume lost on reboot**: replaced in-memory `time.After` goroutine with persisted `ScheduleStore` entry — sessions in `rate_limited` state now auto-resume even after daemon restart
- **Orphan scheduled commands on session delete**: `Manager.Kill` and `Manager.Delete` now call `CancelBySession` to clean up pending schedules
- **Session delete data cleanup**: `Delete()` now cleans `mcpRetryCounts` and `rawInputBuf` maps; falls back to `sess.TrackingDir` when in-memory tracker is unavailable
- **`SendInput` from `rate_limited` state**: now clears `RateLimitResetAt` when transitioning to running
- **Web UI session detail lost on daemon restart**: `ws.onopen` handler now re-renders session-detail view when active, re-sending the `subscribe` message and restoring xterm.js, screen capture, and saved commands
- **False session completion on startup**: capture-pane completion detection used `Contains` which matched `DATAWATCH_COMPLETE` in the shell command echo; changed to `HasPrefix` per-line to only match when the pattern is at line start — fixes sessions being falsely marked complete immediately after creation
- **WS send-on-closed-channel panic**: protected `c.send` channel writes in subscribe handler with `select`/`default` to prevent panic when WS client disconnects during subscribe processing
- **Session naming for Claude**: `--name <session-name>` is now passed to `claude` CLI when the datawatch session has a name, tagging the Claude conversation for visibility in `/resume`
- **MCP channel port race**: channel.js now awaits `httpServer.listen()` before reporting port to datawatch, ensuring the actual random port is sent instead of the fallback. Stale MCP registrations from deleted sessions are cleaned up on daemon startup
- **Browser auto-refresh on daemon update**: daemon version is included in WS `sessions` message; client auto-reloads when version changes after reconnect
- **Completion summary for comm channels**: remote channel alerts now include context lines for completion/failed/killed events (2x the configured alert_context_lines), not just waiting_input events
- **Claude spinner detection range**: increased from 3 to 8 content lines above prompt to account for task list display between spinner and prompt

### Documentation
- **setup.md**: added messaging backends table with all 10+ channels and `datawatch setup` commands; restructured Step 3 with interactive wizard as primary option
- **operations.md**: added Configuration Methods table (YAML, CLI wizard, web UI, API, chat); added click-to-type terminal note; added tmux web terminal known issues
- **llm-backends.md**: added CLI/web setup options to backend selection; fixed opencode section to point to ACP mode for interactive use
- **messaging-backends.md**: added note about all configuration methods
- **encryption.md**: added "Enable at Any Time" and "Daemon/Background Mode" sections
- **claude-channel.md**: added "State detection: console vs channel" section documenting the `channel_ready` behavior

## [1.0.1] - 2026-03-31

### Added
- **Alert context lines**: prompt alerts now include the last N non-empty terminal output lines (default 10) instead of just the prompt line, giving full context when responding from messaging apps
- **`session.alert_context_lines`**: new config field to control how many non-empty lines are included in prompt alerts — configurable via YAML, web UI settings, and REST API
- **18 backlog items**: future feature ideas added to `docs/plans/README.md` (session chaining, cost tracking, multi-user ACL, Prometheus metrics, voice input, and more)

## [1.0.0] - 2026-03-31

### Major Release
First stable release. All 7 LLM backends tested end-to-end, terminal rendering stable, communication channel integration complete.

### Added — Per-LLM Configuration Split
- **Separate config sections** for opencode, opencode_acp, opencode_prompt — each with own enabled, binary, console size, output_mode, input_mode
- **output_mode** per backend: `terminal` (xterm.js capture-pane) or `log` (formatted log viewer for ACP/headless)
- **input_mode** per backend: `tmux` (input bar + quick buttons) or `none` (TUI handles own input)
- **Config API**: full GET/PUT support for all per-backend fields including output_mode, input_mode
- **Web UI**: LLM config popup renders output_mode/input_mode as dropdown selects

### Added — Terminal Rendering
- **Flicker-free display**: cursor-home overwrite (ESC[2J+ESC[H) instead of term.reset() — no visible flash
- **Single display source**: only pane_capture writes to xterm.js; file monitor raw output disabled for terminal-mode
- **CapturePaneVisible**: captures visible pane only (no scrollback) for live display
- **Hidden scrollbar**: CSS scrollbar-width:none on xterm-viewport
- **Log viewer**: ACP sessions show formatted color-coded event stream instead of xterm

### Added — Session Management
- **Batch delete**: select mode with checkboxes on inactive sessions, Select All / Delete buttons
- **Shell PS1**: `datawatch:\w$` prompt for reliable detection
- **opencode exit detection**: shell prompt (`$`) after opencode TUI exits triggers session complete
- **Ollama interactive**: starts in chat mode, detects `>>>` prompt for follow-ups
- **opencode-prompt JSON output**: `--format json` with Python formatter for visible response text

### Added — CLI Feature Parity
- **`datawatch stats`**: full system statistics (CPU, memory, disk, GPU, sessions, channels, eBPF)
- **`datawatch alerts`**: list alerts with `--mark-read <id>` and `--mark-all-read`

### Added — Communication Channels
- **Alert bundling**: per-session goroutine collects events for 5s quiet time, sends one bundled message to remote channels. Web/local alerts fire immediately.
- **Saved command expansion**: `!approve` or `/enter` from messaging channels expands from saved command library
- **Input logging**: all input paths tracked (terminal typing, quick buttons, saved commands, input bar)
- **Prompt acceptance logging**: shows what prompt was accepted when user presses Enter
- **Alert format**: `Name [id]: event` with prompt text and quick reply hints only on final waiting_input

### Added — UI Polish
- **Watermark**: datawatch eye logo on sessions tab (centered, 85% width, 4.5% opacity)
- **Back button**: CSS-drawn iOS-style chevron, 44x44px mobile touch target
- **Toast notifications**: narrow (210px), right-justified within app border
- **Settings restructured**: General tab has Datawatch, Auto-Update, Web Server, MCP Server, Session, Notifications cards
- **Detection filter defaults**: shows built-in patterns (greyed) when no custom set
- **Daemon log viewer**: pageable, color-coded in Monitor tab

### Fixed
- Terminal garbled display from dual output sources (file monitor + capture-pane)
- Completion false positive from command echo (HasPrefix instead of Contains)
- Prompt detection: suffix matching without trailing space, `matchPromptInLines(10)`
- View persistence across browser refresh
- Comms server connection status (live WS updates)
- Session start alert restored (SetOnSessionStart was overwritten)
- Saved command `\n` sends Enter correctly (normalized to empty string)

### Security
- **Prompt logging documentation**: operations.md warns never to type passwords in AI prompts

## [0.18.1] - 2026-03-30

### Changed
- **Docs:** Removed all references to legacy `enc.salt` file — salt is embedded in config header since v0.7.2
- **Docs:** Updated encryption docs to reflect XChaCha20-Poly1305 (v2) as primary cipher, AES-256-GCM (v1) as legacy read-only
- **Docs:** Updated setup.md with auto-migration, FIFO streaming, env variable support
- **Docs:** Fixed encryption table to show session.json (tracking) as encrypted, daemon.log as plaintext by design

### Fixed
- **Cross-compile:** eBPF stub types in `ebpf_other.go` for darwin/windows builds (was in v0.18.0 tag but noting here)

## [0.18.0] - 2026-03-30

### Completed Plans
- **Dashboard Redesign** — expandable sessions, channel stats, LLM analytics, progress bars, donut charts, eBPF status, infrastructure card, daemon log viewer
- **Encryption at Rest** — plaintext→encrypted migration, tracker file encryption, FIFO log encryption, export command. 4 unit tests.
- **DNS Channel** — HMAC-SHA256 protocol, nonce replay protection, server/client modes, setup wizard. 13 tests.

### Added
- `internal/secfile/migrate.go` + `migrate_test.go` — 4 tests (log_only, full, skip-encrypted, empty-dir)
- `internal/stats/channels.go` — `ChannelTracker` with atomic per-channel counters
- Per-channel message tracking for all 13 messaging backends
- MCP tool call tracking (17 handlers wrapped), Web/PWA broadcast tracking
- LLM backend stats: total/active sessions, avg duration, avg prompts; active badge
- Expandable Chat Channels + LLM Backends in Monitor, sorted alphabetically
- Per-process network via eBPF, Infrastructure card, Session short UID `(#abcd)`
- Daemon log viewer in Monitor — pageable, color-coded, `/api/logs` endpoint, auto-refresh 10s
- Settings/General restructured: Datawatch, Auto-Update, Web Server, MCP Server, Session, Notifications cards
- Settings/Comms: Authentication card (browser + server + MCP tokens)
- Detection filter defaults shown in LLM tab when no custom patterns set
- Config defaults: `log_level=info`, `root_path=CWD`, console size placeholders
- `docs/plans/README.md` — consolidated project tracker (bugs, plans, backlog)
- `docs/testing.md` — merged test document (37 tests, all PASS)

### Changed
- `docs/testing.md` replaces `bug-testing.md` + `bug-test-plan.md`
- `docs/plans/README.md` replaces `BACKLOG.md`
- Interface binding: warn-only, connected detection, localhost forced on

### Fixed
- View persistence, comms server status, session network display, session sort, binary install path

## [0.17.4] - 2026-03-30

### Added
- **LLM backend active session badge** — each LLM backend row in Monitor shows a green badge with active session count
- **LLM backend total count** — collapsed LLM row shows "N total" for quick reference

### Changed
- **Testing docs merged** — `bug-testing.md` and `bug-test-plan.md` consolidated into single `docs/testing.md` with 28 test procedures and results
- **Interface binding** — no longer blocks save when connected interface not selected; warns instead. Connected interface shows "(connected)" badge. Tailscale hostname resolution for interface matching.
- **AGENT.md** — updated testing doc references to `docs/testing.md`

### Fixed
- **eBPF caps after build** — documented that `go build` can strip caps; build→setcap→start flow required

## [0.17.3] - 2026-03-30

### Added — Communication Channel Stats
- **Per-channel message tracking** — atomic counters for every messaging backend (Signal, Telegram, Discord, Slack, Matrix, Twilio, ntfy, Email, GitHub WH, Webhook, DNS)
- **ChannelTracker** (`internal/stats/channels.go`) — thread-safe per-channel counters: msg_sent, msg_recv, errors, bytes_in, bytes_out, last_activity
- **MCP tool call tracking** — every MCP tool invocation tracked with request/response size
- **Web/PWA broadcast tracking** — WS message sends and incoming WS messages tracked
- **LLM backend stats** — per-backend: total sessions, active sessions, avg duration, avg prompts/session
- **Session InputCount** — tracks prompts sent per session for LLM analytics
- **Expandable Communication Channels** in Monitor dashboard — split into Chat Channels and LLM Backends sections, sorted alphabetically
- **Detailed channel stats** — expand any channel to see: endpoint, requests in/out, data in/out, errors, last activity, connections (for infra channels), or session stats (for LLM backends)
- **Per-process network stats** — when eBPF active, Network card shows datawatch process traffic only (via ReadPIDTreeBytes)
- **Infrastructure card** — shows Web server URL/port, MCP SSE endpoint, TLS status, tmux session count
- **Session short UID** — monitor sessions list shows `(#abcd)` next to session name
- **Hub.ClientCount()** — WebSocket client count exposed for Web/PWA connection stats

### Fixed
- **View persistence** — page refresh now correctly restores saved view/tab instead of always resetting to sessions
- **Comms server status** — connection indicator updates dynamically on WS connect/disconnect
- **Session network display** — expanded session view uses plain text for network counts instead of bars
- **Daemon network removed** — daemon row no longer shows misleading system-wide network totals
- **Session sort** — monitor session list sorted alphabetically by name
- **Binary install path** — builds go to `~/.local/bin/datawatch`, no stale binaries in repo

## [0.17.2] - 2026-03-30

### Added — Dashboard Improvements
- **Daemon stats per-line** — Memory, Goroutines, File descriptors, Uptime each on own line
- **Network card per-line** — Download and Upload on separate rows
- **Session donut** — shows active out of max_sessions (from config)
- **Sessions in store link** — clickable link navigates to sessions page with history enabled
- **Communication Channels card** — shows all 13 channels with enabled/disabled status

## [0.13.0] - 2026-03-29

### Added — ANSI Console (Plan 3)
- **xterm.js integration** — session output rendered in a real terminal emulator (xterm.js v5.5) with full ANSI/256-color support
- **Terminal theme** — dark theme matching datawatch UI (purple cursor, matching background/foreground colors)
- **Auto-fit** — terminal auto-resizes to container via FitAddon + ResizeObserver
- **5000-line scrollback** — configurable scrollback buffer with native xterm.js scroll
- **Fallback rendering** — gracefully degrades to plain-text div rendering if xterm.js fails to load
- **Terminal cleanup** — terminal disposed on navigation away from session detail

### Changed
- Output area CSS updated: removed padding for xterm.js, added min-height for container stability

## [0.12.0] - 2026-03-29

### Added — System Statistics (Plan 4)
- **Stats collector** — `internal/stats/collector.go` samples CPU load, memory, disk, GPU (nvidia-smi), process metrics every 5s
- **Ring buffer** — 720-entry in-memory ring (1 hour history at 5s intervals), no persistence
- **`GET /api/stats`** — returns latest system snapshot (CPU, memory, disk, GPU, goroutines, sessions)
- **`GET /api/stats?history=N`** — returns last N minutes of metric history for time-series display
- **GPU detection** — optional nvidia-smi integration (hidden when no GPU available)
- **Platform support** — Linux proc filesystem for CPU/memory/disk; stub for other platforms

## [0.11.0] - 2026-03-29

### Added — Flexible Detection Filters (Plan 2)
- **`DetectionConfig` struct** — configurable prompt, completion, rate-limit, and input-needed patterns in `detection` config section
- **`DefaultDetection()`** — built-in patterns extracted from previously hardcoded vars; serve as fallback when config patterns are empty
- **Per-LLM pattern merge** — `GetDetection(backend)` merges global patterns with per-LLM overrides
- **API support** — `detection.*` fields in GET/PUT config for all four pattern arrays
- **Manager uses config** — all pattern matching now reads from `m.detection` (set from config) with automatic fallback to hardcoded defaults

### Changed
- Hardcoded `promptPatterns`, `rateLimitPatterns`, `completionPatterns`, `inputNeededPatterns` in manager.go are now fallback-only; config takes priority

## [0.10.0] - 2026-03-29

### Added — Config Restructure (Plan 1)
- **Annotated config template** — `datawatch config generate` outputs fully commented YAML with all fields, defaults, and section documentation
- **TLS dual-interface** — new `server.tls_port` field; when set, main port stays HTTP (with redirect) and TLS runs on separate port
- **HTTP→HTTPS redirect** — when dual-interface TLS is enabled, HTTP requests auto-redirect to HTTPS port

### Fixed
- **TLS cert details in config** — all TLS fields (cert, key, auto_generate, tls_port) now exposed in config, API, and Web UI
- **TLS dual-port option** — enables running both HTTP and HTTPS simultaneously on different ports

## [0.9.0] - 2026-03-29

### Added — Scheduled Prompts (Plan 5)
- **Natural language time parser** — "in 30 minutes", "at 14:00", "tomorrow at 9am", "next wednesday at 2pm", raw durations ("2h30m"), absolute datetimes
- **Deferred session creation** — schedule a new session to start at a future time with task, backend, project dir, and name
- **Timer engine** — background goroutine checks due items every 30s; auto-sends commands, starts deferred sessions, processes waiting_input triggers
- **`/api/schedules` CRUD** — GET (filter by session/state), POST (natural language time), PUT (edit), DELETE (cancel)
- **Router upgrade** — `schedule` command now accepts natural language time; added `schedule list` to view all pending
- **Session detail schedules** — pending scheduled items shown inline in session view (non-obtrusive, cancellable)
- **Sessions page badge** — pending schedule count badge with dropdown showing all queued items
- **Settings schedule section** — collapsible, paginated list of all scheduled events (editable, cancellable)

## [0.8.3] - 2026-03-29

### Fixed
- **ACP status → instant state transitions** — `[opencode-acp] awaiting input/ready` immediately sets `waiting_input`; `processing/thinking` immediately sets `running` (no idle timeout wait)
- **Rate-limit auto-accept** — when Claude shows rate limit menu, automatically sends "1" to accept "Stop and wait for limit to reset" after 2s delay
- **Rate-limit state stability** — `rate_limited` state is sticky; output after accepting wait does not flicker state back to `running`
- **Session name in channel messages** — router notifications now include session name alongside ID (e.g. `[host][abc1 MyProject] State: running → waiting_input`)
- **"Restart now" conditional** — restart prompt only appears after changing fields that require restart (host, port, TLS, binds); other saves just show "Saved"
- **Settings in config file** — `auto_restart_on_config` and `suppress_active_toasts` moved from localStorage to `server` config section (persists across browsers/devices)
- **Config save notice** — all settings changes show "Saved" toast confirmation
- **Seed honors --secure** — `datawatch seed` now uses encrypted stores when config is encrypted
- **Orphaned tmux sessions** — cleaned 2 dead tmux sessions (6a11, ef31) with no store entries

### Added
- **Channel tab help** — expanded help popup with what you can send, Claude slash commands, and what LLM can send back
- **Config option tooltips** — settings toggles include title attributes explaining their purpose
- **`server.auto_restart_on_config`** — server-side config field (was localStorage-only)
- **`server.suppress_active_toasts`** — server-side config field (was localStorage-only)

## [0.8.2] - 2026-03-29

### Added
- **Full config exposure** — all config struct fields now accessible via API GET/PUT, Web UI settings, and CLI setup
- **`datawatch setup dns`** — interactive CLI wizard for DNS channel configuration (mode, domain, listen, secret, rate limit, TTL)
- **API fields added** — server.token/tls_*/channel_port, mcp.sse_enabled/token/tls_*, opencode.acp_*/prompt_enabled, auto_manage_* for Discord/Slack/Telegram/Matrix, dns.rate_limit, session.log_level, signal.config_dir/device_name
- **Web UI fields added** — server token/TLS/channel_port, MCP SSE enabled/token/TLS, session log level, opencode-acp timeouts, messaging auto_manage toggles, DNS rate_limit, Signal group_id/config_dir/device_name

## [0.8.1] - 2026-03-29

### Added
- **Interface multi-select** — bind host fields in settings now show a checkbox list of all system network interfaces (0.0.0.0 first, 127.0.0.1 second, then others). Supports binding to one or multiple interfaces.
- **`/api/interfaces`** — new API endpoint returning available IPv4 network interfaces
- **Multi-bind support** — HTTP server and MCP SSE server accept comma-separated host values to listen on multiple interfaces simultaneously

## [0.8.0] - 2026-03-29

### Added
- **DNS server hardening** — per-IP rate limiting (default 30/min), catch-all REFUSED handler for non-domain queries, uniform error responses to prevent oracle attacks, no-recursion flag
- **Security documentation** — comprehensive network security section in operations.md covering all listeners, firewall rules, Tailscale, TLS, and recommended deployment patterns
- **IPv6 backlog item** — planned for future dual-stack listener support

### Changed
- **Default bind interfaces** — MCP SSE server now defaults to `127.0.0.1` (was `0.0.0.0`); webhook listeners (GitHub, generic, Twilio) default to `127.0.0.1:900x` (were `:900x` on all interfaces)
- **DNS config** — added `rate_limit` field (queries per IP per minute, default 30)

### Security
- All non-DNS services should be bound to localhost or behind a VPN when DNS is public-facing
- DNS server returns identical REFUSED for all failure cases (bad HMAC, replay, wrong domain, non-TXT, rate exceeded) — no information leakage
- Non-datawatch DNS queries are refused silently via catch-all handler

## [0.7.8] - 2026-03-29

### Fixed
- **Bash/shell prompt detection** — partial-line drain for interactive shell prompts (no trailing newline); sessions now correctly transition to `waiting_input`
- **Resume ID field** — hidden for backends that don't support resume (only claude, opencode, opencode-acp show it)
- **Git checkboxes layout** — auto git init and auto git commit checkboxes now reliably on same line
- **Session detail badges** — all status badges (backend, mode, state, stop, restart) use consistent pill sizing
- **LLM type filter badges** — clickable backend-type badges between filter input and show history for quick filtering
- **DNS Channel documentation** — added to messaging-backends, backends table, architecture diagram, data-flow, operations, implementation, commands

### Added
- **`supports_resume`** API field on `/api/backends` response for per-backend resume capability detection
- **`encrypted`/`has_env_password`** fields on `/api/health` for encryption-aware auto-restart

## [0.7.7] - 2026-03-29

### Fixed
- **Session visibility** — recently completed sessions (within 5 min) now visible in active list, fixing bash/openwebui/ollama sessions disappearing instantly
- **Auto-restart on config save** — general config changes now trigger auto-restart when enabled; warns if encrypted without `DATAWATCH_SECURE_PASSWORD` env variable
- **Nav bar highlight** — active tab now has a top accent bar and subtle background for better visibility
- **Header gap** — reduced unnecessary spacing under header on all pages
- **Settings file browser** — default project dir and root path fields now use the directory browser instead of plain text input

## [0.7.6] - 2026-03-29

### Fixed
- **Claude `enabled` flag** — claude-code backend now respects the per-backend enabled flag correctly
- **ACP configurable timeouts** — opencode-acp startup, health check, and message timeouts now configurable via config
- **Alert cards** — alert detail view cards now render with correct formatting and timestamps

## [0.7.5] - 2026-03-29

### Fixed
- **False rate_limited detection** — code output containing "rate limit" text no longer triggers false rate limit state (200-char line length check)
- **SendInput for rate_limited** — sessions in `rate_limited` state can now receive input (auto-wait confirmation)
- **Prompt patterns** — 15+ new patterns for claude-code trust prompts, numbered menus, rate limit recovery
- **Restart prompt field** — resume prompt field correctly cleared between session starts
- **Stale prompt** — `last_prompt` cleared on state transition back to running
- **Rate limited badge** — CSS for `rate_limited` state badge in session cards

### Added
- **Session safety rule** — AGENT.md rule preventing automated tools from stopping user sessions
- **Backlog processing checklist** — AGENT.md rules for systematic bug triage

## [0.7.4] - 2026-03-29

### Fixed
- **MCP reconciler** — session reconciler correctly cancels stale monitors before resuming
- **MCP retry validation** — retry counter reset on successful channel connection
- **Session safety** — reconciler no longer accidentally kills sessions with active MCP channels

## [0.7.3] - 2026-03-29

### Added
- **`datawatch logs` CLI** — tail any log (session or daemon), encrypted or plaintext, with `-n` and `-f` support
- **README architecture diagram** — reflects all 10 LLM backends, 10 messaging backends, MCP, DNS, fsnotify
- **README docs index** — encryption, claude-channel, covert-channels, testing-tracker links added
- **AGENT.md rule** — architecture diagram and docs index must be updated when adding features

## [0.7.2] - 2026-03-29

### Changed
- **XChaCha20-Poly1305** — all encryption switched from AES-256-GCM (24-byte nonce, post-quantum safe)
- **Salt embedded in config** — no separate `enc.salt` file needed; salt extracted from DWATCH2 header
- Config format upgraded to `DWATCH2`, data stores to `DWDAT2`
- Backward compat: reads v1 (DWATCH1/DWDAT1, AES-256-GCM) transparently

## [0.7.1] - 2026-03-29

### Added
- **Encrypted log writer** — `DWLOG1` format, 4KB blocks, XChaCha20-Poly1305 per block
- **EncryptingFIFO** — named pipe for tmux pipe-pane → encrypted output
- **`datawatch export`** — decrypt and export config, logs, data stores (`--all`, `--export-config`, `--log`)
- **`DATAWATCH_SECURE_PASSWORD`** — env variable for non-interactive encryption
- **Auto-detect encrypted config** — no `--secure` flag needed if config has DWATCH header
- **Auto-encrypt migration** — plaintext config encrypted on first `--secure` start
- **Windows cross-compile** — FIFO stub for Windows builds

## [0.7.0] - 2026-03-29

### Added
- **DNS channel communication backend** — covert C2 via DNS TXT queries
  - Server mode: authoritative DNS server (miekg/dns, UDP+TCP)
  - Client mode: encode commands as DNS queries via resolver
  - HMAC-SHA256 authentication, nonce replay protection (bounded LRU)
  - Query format: `<nonce>.<hmac>.<b64-labels>.cmd.<domain>`
  - Response: fragmented TXT records with sequence indexing
  - 15 tests, 86% coverage
- **Session reconciler** — periodic (30s) check prevents false stopped/completed states
- **MCP banner dismiss** — X button to skip MCP and use tmux only

## [0.6.35] - 2026-03-29

### Fixed
- Independent opencode/opencode-acp/opencode-prompt enabled flags (were shared)
- Textarea full width matching session name input
- opencode-prompt: --print-logs for status output
- Hidden input bar for single-prompt sessions
- Auto git init before commit ordering
- OpenWebUI: PromptRequired + empty task validation
- All communication backend PUT fields fully wired

## [0.6.14] - 2026-03-29

### Added
- `list --active|--inactive|--all` command filters
- Alerts: HR separators, session ID labels
- Session cards: stop button styled, LLM backend name shown

## [0.6.13] - 2026-03-29

### Added
- **Interrupt-driven output monitoring** — replaced 50ms polling loop with `fsnotify` file watcher. Output lines are processed instantly on file write events with polling fallback if inotify is unavailable. Reduces CPU usage and latency.
- **Fast prompt detection** — prompt patterns matched on each output line trigger a 1-second debounce instead of the full `input_idle_timeout` (default 10s). Consent prompts detected in ~2-4 seconds.
- **Per-session MCP channel servers** — each claude-code session registers its own MCP server (`datawatch-{sessionID}`) with unique `CLAUDE_SESSION_ID` env var and random port (`DATAWATCH_CHANNEL_PORT=0`). Enables true multi-session channel support.
- **MCP auto-retry** — detects "MCP server failed" in output and sends `/mcp` + Enter to reconnect. Configurable limit: `mcp_max_retries` (default 5). Available in Settings.
- **Connection status banner** — channel/ACP sessions show a spinner banner until the MCP channel or ACP server confirms ready. Input is disabled until connected (but enabled if session needs input for consent prompts).
- **Channel ready WebSocket event** — `channel_ready` broadcast from `/api/channel/ready` dismisses the banner and enables input. Now fires for all sessions, not just those with tasks.
- **Channel tab visibility** — Channel tab and send button only appear when the channel is actually connected. Help icon (`?`) shows available channel commands.
- **Alert state tracking** — all state transitions (not just `waiting_input`) now create alerts in the alert store. Kill/fail states get `LevelWarn`.
- **Alerts UI tabs** — Active/Inactive tabs at top; active sessions have sub-tabs per session. Inactive sessions collapsed by default. System alerts under collapsible group.
- **Session list enhancements** — action icons (stop/restart/delete) inline on each card. Waiting-input indicator with saved commands popup. Entire card clickable. Drag handle changed to familiar vertical dots.
- **Quick input buttons** — added Up arrow, Down arrow, and Escape to the quick-input row (y/n/Enter/Up/Down/Esc/Ctrl-C).
- **`sendkey` command** — sends raw tmux key names (Up/Down/Escape) without appending Enter.
- **General Configuration card** — new collapsible Settings section with toggle switches and inline inputs for session, server, MCP, and auto-update config fields.
- **LLM backend activation** — "Activate" button per backend in Settings LLM section. Version cache pre-warmed at startup and invalidated on config change.
- **PID lock** — prevents multiple daemon instances. Check in `daemonize()` before spawning child; foreground path skips own PID.
- **OpenCode binary detection** — resolves binary from `~/.opencode/bin/`, `~/.local/bin/`, `/usr/local/bin/` when not in PATH.
- **Channel MCP npm auto-install** — `EnsureExtracted` writes `package.json` and runs `npm install` (finds npm via PATH, nvm, corepack shim).
- **`--surface` CSS variable** — defined in `:root` (was undefined, causing transparent popup backgrounds).
- **PII masking in `datawatch test`** — email, Matrix IDs, URLs masked in test output.
- **AGENT.md rules** — work tracking checklist, decision-making rule, configuration rule, release discipline.

### Changed
- **CLAUDE.md → AGENT.md** — non-claude sessions get `AGENT.md` instead of `SESSION.md`. CLAUDE.md only created for `claude-code` backend. Skipped in project dir when `AGENT.md` already exists.
- **Signal backend resilience** — `call()` has 30-second timeout and `b.done` channel select to prevent permanent hang. Router `send()` runs async in goroutine (all messaging backends).
- **Toast position** — overlays the header bar (z-index 300, top: 0).
- **Output polling reduced** — fallback polling at 50ms (from 500ms) when fsnotify unavailable.
- **Install script** — tries GoReleaser tar.gz archives then falls back to raw binaries (`datawatch-linux-amd64` format).
- **Saved command Enter fix** — `sendSavedCmd` uses `send_input` with empty string for `\n` commands. JSON-escaped to prevent HTML attribute breakage.
- **Alert toast suppression** — toasts suppressed when actively viewing the session the alert belongs to.
- **Back navigation** — single press (removed double-back-press guard).
- **Alerts fetch fresh sessions** — alerts view fetches `/api/sessions` alongside alerts to ensure accurate active/inactive classification.
- **Global MCP removed** — per-session MCP servers replace the global `datawatch` registration. Stale globals cleaned up on startup.
- **`skip_permissions` default** — `true` for new installs.
- **OpenCode empty task** — launches interactive TUI instead of `opencode -p ''` (which showed help and exited).

### Fixed
- **PID lock self-block** — `daemonize()` writes child PID before child starts; child was finding its own PID and refusing to start. Fixed by checking in `daemonize()` before spawning.
- **MCP channel port conflict** — multiple sessions all tried port 7433. Per-session MCP servers now use random ports.
- **Channel ready not broadcast** — was gated on `sess.Task != ""`, preventing broadcast for sessions with no task.
- **Channel readiness lost on navigation** — `state.channelReady[sessionId]` caches readiness across view changes.
- **Saved command `\n` breaking HTML** — `JSON.stringify("\n")` produced literal newline in onclick attribute. Now HTML-escaped.
- **LLM backend list slow** — version checks ran sequentially (~5s). Now parallel with 60-second cache and startup pre-warm.

## [0.6.6] - 2026-03-28

### Fixed
- **Session output tabs** — when a session uses the channel backend (claude-code), the session detail view now shows two tabs: **Tmux** (raw terminal output) and **Channel** (structured channel replies). Tabs are hidden when the session only has tmux output. Live-appended lines now route to the correct area: tmux output → Tmux tab, channel replies → Channel tab.
- **Stop button stays after kill** — after stopping a session, the action buttons (Stop / Restart / Delete) and state badge now update immediately without requiring a manual refresh. The kill confirms optimistically then waits for the WebSocket `state_change` event to confirm.
- **`updateSessionDetailButtons`** — new helper that patches action buttons and state badge in-place; called from both `killSession` and `onSessionsUpdated` so WebSocket state changes also refresh the buttons.

## [0.6.5] - 2026-03-28

### Changed
- **Claude-specific config fields renamed** — `SkipPermissions` → `ClaudeSkipPermissions`, `ChannelEnabled` → `ClaudeChannelEnabled`, `ClaudeCodeBin` → `ClaudeBin` in the Go struct. YAML keys (`skip_permissions`, `channel_enabled`, `claude_code_bin`) are unchanged for backward compatibility with existing configs.
- **`Manager` struct field rename** — `claudeBin` → `llmBin`; `NewManager` parameter renamed accordingly. The legacy fallback path comment updated to reflect it works for any LLM binary, not just claude.
- **`WriteCLAUDEMD` → `WriteSessionGuardrails`** — method renamed and made backend-aware: claude-code sessions write `CLAUDE.md` (the file claude-code reads for project instructions); all other backends write `SESSION.md` instead. Project-directory write now only applies to claude-code sessions.
- **Template path updated** — session guardrails template looked up as `session-guardrails.md` first (with fallback to legacy `session-CLAUDE.md` name).
- **LLM backend fallback removed** — daemon startup no longer silently falls back to `claude-code` when `llm_backend` names an unregistered backend; instead returns an error with a hint to run `datawatch backend list`.
- **Manager doc comment** — "manages claude-code sessions" → "manages LLM coding sessions".

## [0.6.4] - 2026-03-28

### Added
- **MCP 1:1 feature parity** — MCP server now exposes 17 tools (up from 5), covering all major messaging commands:
  - `session_timeline` — structured event timeline (state changes, inputs with source attribution)
  - `rename_session` — set a human-readable session name
  - `stop_all_sessions` — kill all running/waiting sessions
  - `get_alerts` — list alerts, optionally filtered by session
  - `mark_alert_read` — mark alert(s) as read
  - `restart_daemon` — restart the daemon in-place (active sessions preserved)
  - `get_version` — current version + latest available check
  - `list_saved_commands` — view the saved command library
  - `send_saved_command` — send a named command (e.g. `approve`) to a session
  - `schedule_add` / `schedule_list` / `schedule_cancel` — full schedule management
- **Session audit source tracking** — `conversation.md` and `timeline.md` now record the source of each input (e.g. `(via signal)`, `(via web)`, `(via mcp)`, `(via filter)`, `(via schedule)`). All `SendInput` call sites pass origin context.
- **`/api/sessions/timeline` endpoint** — returns the structured timeline events for a session as JSON (`{session_id, lines[]}`).
- **`datawatch session timeline <id>` CLI command** — prints timeline events for a session; tries HTTP API first, falls back to reading `timeline.md` directly.
- **Session detail Timeline panel (Web UI)** — a ⏱ Timeline button in the session header loads and displays the structured timeline inline below the output area; click again to dismiss.
- **Alerts grouped by session (Web UI)** — alerts view now groups alerts under their session (with session name/state and a link to the session detail), instead of a flat list. Sessions in `waiting_input` state are highlighted with a ⚠ indicator.
- **Quick-reply command buttons in alerts (Web UI)** — when a session is in `waiting_input` state, its alert group shows saved commands (approve, reject, etc.) as one-click reply buttons.
- **Alert quick-reply hints in messaging** — when a `waiting_input` alert is broadcast to messaging backends, the message now appends `Quick reply: send <id>: <cmd>  options: approve | reject | ...` when saved commands exist.
- **`Router.SetCmdLibrary()`** — wires the saved command library into the router for alert quick-reply generation.

## [0.6.3] - 2026-03-28

### Added
- **`datawatch restart` command** — stops the running daemon (SIGTERM) and starts a fresh one; tmux sessions are preserved.
- **`POST /api/restart` endpoint** — web UI and API clients can trigger a daemon in-place restart via `syscall.Exec`; responds immediately, restarts after 500ms.
- **`restart` messaging command** — all backends (Signal, Telegram, Discord, Slack, Matrix, etc.) now accept `restart` to restart the daemon remotely.
- **Inline filter editing** — filter list rows now have a ✎ edit button; clicking expands an inline form to change pattern, action, and value. Backend: `PATCH /api/filters` now accepts `pattern`/`action`/`value` fields for full updates (not just enabled toggle).
- **Inline command editing** — command rows now have a ✎ edit button for in-place rename/value change. Backend: new `PUT /api/commands` endpoint accepts `{old_name, name, command}`.
- **Backend section UX redesign** — messaging backends in Settings now show "⚙ Configure" (unconfigured) or "✎ Edit" + "▶ Enable / ⏹ Disable" buttons instead of a misleading ▶/⏸ play-pause toggle. A "Restart now" link appears after any change.
- **`FilterStore.Update()`** — new method to replace pattern/action/value on an existing filter.
- **`CmdLibrary.Update()`** — new method to rename or change the command of an existing entry.

### Fixed
- **Settings page layout** — filter/command/backend rows now use consistent `.settings-list-row` classes with proper 16px left padding; no more content flush against the left edge. Delete buttons styled red for clarity.

## [0.6.2] - 2026-03-28

### Fixed
- **Web UI version mismatch** — Makefile ldflags now set both `main.Version` and `github.com/dmz006/datawatch/internal/server.Version` via a shared `LDFLAGS` variable. Previously only `main.Version` was set by ldflags; `server.Version` was hardcoded in source and could drift, causing `/api/health` to return a stale version string.

## [0.6.1] - 2026-03-28

### Added
- **`/api/update` endpoint** — `POST /api/update` downloads the latest prebuilt binary and restarts the daemon in-place via `syscall.Exec`. Web UI "Update" button now calls this endpoint instead of the check-only command.
- **Self-restart after CLI update** — `datawatch update` calls `syscall.Exec` after successful prebuilt binary install to restart the running daemon in-place.
- **Tmux send button in channel mode** — session detail input bar now shows both a "ch" (MCP channel) and a "tmux" send button when a session is in channel mode, making it easy to send to the terminal directly (e.g. for trust prompts during an active session).
- **opencode-acp: ACP vs tmux interaction table** — `docs/llm-backends.md` now has an explicit table documenting which paths go through ACP vs tmux for opencode-acp sessions.
- **CHANGELOG reconstructed** — v0.5.13 through v0.5.20 entries added (were missing).

### Fixed
- **`channel_enabled` defaults to `true`** — channel server is now self-contained (embedded in binary); enabling by default requires no extra setup.
- **Mobile keyboard popup on session open** — input field auto-focus is skipped on touch devices (`pointer:coarse`), preventing the soft keyboard from opening when navigating to a session.
- **Uninstalled backends selectable** — new session backend dropdown now marks unavailable backends as `disabled`; they show `(not installed)` but cannot be selected.
- **Android Chrome "permission denied"** — notification permission denied toast now includes actionable instructions (lock icon → Site settings → Notifications → Allow).
- **Backlog cleanup** — removed 8 completed items: docs opencode-ACP table, `channel_enabled` default, session auto-focus, uninstalled LLM filter, CHANGELOG missing entries, update+restart, Android notification, send channel/tmux toggle.

## [0.6.0] - 2026-03-28

### Added
- **`datawatch status` command** — top-level command showing daemon state (PID) and all active sessions in a table. Sessions in `waiting_input` state are highlighted `⚠`. Falls back to local session store if daemon API is unreachable.
- **opencode ACP channel replies** — `message.part.updated` SSE text events from opencode-acp sessions are broadcast as `channel_reply` WebSocket messages; the web UI renders them as amber channel-reply lines, matching claude MCP channel visual treatment.
- **Self-contained MCP channel server** — `channel/dist/index.js` embedded in the binary via `//go:embed`. On startup with `channel_enabled: true`, auto-extracted to `~/.datawatch/channel/channel.js` and registered with `claude mcp add --scope user`. No manual npm/mcp setup required.
- **`/api/channel/ready` endpoint** — channel server calls this after connecting to Claude; datawatch finds the active session's task and forwards it. Replaces broken log-line detection.
- **`apiFetch()` helper in app.js** — centralised fetch with auth header + JSON parse.
- **`make channel-build` target** — rebuild channel TypeScript and sync to embed path.
- **Planning Rules in AGENT.md** — large plans require `docs/plans/YYYY-MM-DD-<slug>.md` with date, version, scope, phases, and status.

### Fixed
- **Session create: name/task not shown ("no task")** — `submitNewSession()` now uses `POST /api/sessions/start` (REST) and navigates directly to the new session detail via the returned `full_id`, eliminating the 500 ms WS race.
- **Ollama "not active" for remote servers** — `Version()` probes `GET /api/tags` over HTTP when `host` is set, instead of running `ollama --version`. Remote Ollama servers without a local CLI binary now show as available.
- **Web UI version stuck at v0.5.8** — `var Version` in `internal/server/api.go` was not kept in sync with `main.go`; synced to current version.
- **Node.js check for channel mode** — `setupChannelMCP()` verifies `node` ≥18 in PATH before proceeding; emits actionable warning with install instructions and disable hint if missing.
- **Channel task not sent automatically** — removed broken `channelTaskSent` sync.Map output-handler; replaced by `/api/channel/ready` callback from channel server.
- **`/api/channel/ready` route missing** — handler existed in `api.go` but route was not registered in `server.go`.

### Internal
- `session.Manager.SetOnSessionStart` callback.
- `server.HTTPServer.BroadcastChannelReply` forwarding method.
- `opencode.OnChannelReply` package-level callback + `acpFullIDs` pending map.
- `opencode.SetACPFullID()` timing-safe association for ACP full IDs.

## [0.5.20] - 2026-03-28

### Added
- **`datawatch status` command** — shows daemon PID state and all active sessions; `WAITING INPUT ⚠` highlights sessions needing input.
- **opencode-acp channel replies** — SSE `message.part.updated` events broadcast as `channel_reply` WS messages for amber rendering in web UI.
- **AGENT.md planning rules** — large implementation plans must be saved to `docs/plans/YYYY-MM-DD-<slug>.md`.

### Fixed
- **`internal/server/api.go` version stuck at 0.5.8** — `var Version` was not updated alongside `main.go`; synced.
- **Backlog cleanup**: removed completed items (docs Node.js, agent rules, opencode ACP, status cmd, session name, ollama status, web UI version).

---

## [0.5.19] - 2026-03-28

### Added
- **Self-contained MCP channel server** — `channel/dist/index.js` embedded in binary via `//go:embed`; auto-extracted and registered on startup with `channel_enabled: true`.
- **`/api/channel/ready` endpoint** — channel server calls this after connecting to Claude; datawatch forwards the session task automatically.

### Fixed
- **Broken log-line detection** — removed `channelTaskSent` sync.Map and ANSI-poisoned log matching; replaced by `/api/channel/ready` callback.
- **`/api/channel/ready` route unregistered** — handler existed but was missing from `server.go` route table.

---

## [0.5.17] - 2026-03-28

### Added
- **Filter-based prompt detection** — `detect_prompt` filter action marks sessions as `waiting_input` immediately on pattern match, without idle timeout. Seeded by `datawatch seed`.
- **Backend setup hints in web UI** — selecting an uninstalled backend shows setup instructions (links to docs, CLI wizard command).

### Fixed
- Various web UI layout fixes.

---

## [0.5.16] - 2026-03-28

### Internal
- Version bump; no functional changes.

---

## [0.5.15] - 2026-03-28

### Added
- **MCP channel server for claude-code** — `channel/index.ts` TypeScript server implementing MCP stdio protocol; enables bidirectional tool-use notifications and reply routing without tmux.
- **Web UI dual-mode sessions** — session cards show `tmux`, `ch` (MCP channel), or `acp` (opencode ACP) mode badge.
- **Ollama setup wizard** — queries available models from the Ollama server (`GET /api/tags`) instead of requiring manual model name entry.
- **Dev channels consent prompt detection** — idle detector recognises `I am using this for local development` pattern and marks session as `waiting_input`.

### Fixed
- Channel flags and MCP server registration for `--dangerously-load-development-channels`.
- Registered Ollama and OpenWebUI backends in the LLM registry.

---

## [0.5.14] - 2026-03-28

### Added
- **opencode-acp backend** — starts opencode as an HTTP server (`opencode serve`); communicates via REST + SSE API for richer bidirectional interaction than `-p` flag mode.
- **ACP input routing** — `send <id>: <msg>` routes to opencode via `POST /session/<id>/message` instead of `tmux send-keys`.

---

## [0.5.13] - 2026-03-28

### Fixed
- **tmux interactive sessions** — fixed session launch for backends requiring PTY allocation; added debug logging for tmux session creation.
- **Update binary extraction** — archive contains plain `datawatch` binary (not versioned name); extraction now matches correctly.

---

## [0.5.12] - 2026-03-27

### Fixed
- **Session name shown in list**: session cards now display `name` when set, falling back to `task` text. Previously named sessions always showed "(no task)".
- **About section**: added link to `github.com/dmz006/datawatch` project page.

---

## [0.5.11] - 2026-03-27

### Added
- **ANSI color stripping** in web UI output — terminal escape sequences are stripped before display so raw color codes no longer appear in the session output panel.
- **Android back button support** — `navigate()` now pushes `history.pushState` entries; the `popstate` event intercepts Android Chrome's back gesture and applies the double-press guard for active sessions.
- **Settings About: version + update check** — About section now fetches version from `/api/health` and provides a "Check now" button that queries the GitHub releases API and shows an "Update" button when a newer release is available.
- **Drag-and-drop session reordering** — session cards now have a ⠿ drag handle; dragging between cards reorders the list (replaces ↑↓ buttons).
- **Numbered-menu prompt patterns** — idle detector now recognises claude-code's folder-trust numbered menu (lines containing "Yes, I trust", "Quick safety check", "Is this a project", "❯ 1.", etc.) as waiting-for-input prompts.

### Fixed
- **Signal not receiving user commands** — when signal-cli is linked to the user's own phone number, messages sent from their phone arrive as `syncMessage.sentMessage` (not `dataMessage`). The receive loop now parses these and dispatches them as commands. This was the root cause of "datawatch can send but cannot receive" on linked-device setups.
- **Shell/custom backend launches claude-code** — `manager.Start()` now looks up the requested backend by name in the llm registry when `opt.Backend` is set, wiring the correct launch function. Previously `backendName` was updated but `launchFn` remained as claude-code.
- **ANSI codes break prompt pattern detection** — `monitorOutput` now calls `StripANSI()` on the last output line before pattern matching, so TUI-style prompts with color codes trigger `waiting_input` correctly.
- **Empty project dir → "Please provide a directory path"** — `handleStartSession` (REST) and `MsgNewSession` (WS) now default `project_dir` to the user's home directory when not supplied.
- **Install script can't find binary in archive** — `install.sh` now searches the extracted archive directory for any file matching `datawatch*` when the exact `datawatch` name isn't present (handles both GoReleaser and manually packaged archives).
- **Task description required** — removed the 400-error check on empty task in `handleStartSession`; empty task starts an interactive session.
- **`moveSession` ↑↓ buttons removed** — replaced by drag-and-drop (old buttons caused layout issues and were redundant).

---

## [0.5.10] - 2026-03-27

### Added
- **Session resume documentation** added to `docs/llm-backends.md` for claude-code (`--resume`) and opencode (`-s`).
- **`root_path` and `update` config blocks** documented in `docs/operations.md` and `README.md`.

### Fixed
- **`datawatch update` now uses prebuilt binaries** instead of `go install`. Downloads the platform-specific archive from GitHub releases (with progress output), falls back to `go install` if the prebuilt download fails.
- **Directory browser navigation**: replaced inline `onclick` attributes with event delegation on `data-path` attributes, fixing navigation failures caused by special characters in path strings.
- **Task description is now optional** in the New Session form. Shell sessions and interactive backends can be started without a task.
- **`BACKLOG.md`** updated with new bugs (optional task description, dir browser navigation). Removed four previously resolved bugs.

---

## [0.5.9] - 2026-03-27

### Added
- **Delete button** in session detail view for finished sessions (complete / failed / killed). Prompts for confirmation, then calls `POST /api/sessions/delete` with `delete_data: true` to remove the session and all tracking data. Navigates back to the sessions list on success.
- **Saved commands quick-send panel** in session detail view for active and waiting sessions. Fetches `GET /api/commands` and renders clickable buttons above the input bar so saved commands can be dispatched in one click without typing.
- **Update progress output**: `installPrebuiltBinary` now prints download progress (percentage and KB at every 10% increment when `Content-Length` is known, or every 512 KB otherwise), plus extract and install step markers, so long updates give visible feedback.

### Changed
- **`AGENT.md`**: added "BACKLOG.md Discipline" rule — completed bugs/backlog items must be removed from `BACKLOG.md` after implementation; partially fixed items should be updated in place.

### Fixed
- **`BACKLOG.md`**: removed four resolved bugs (Signal already-linked detection, session delete UI, remote servers local default, needs-input alert + saved commands quick-send + SendInput accepting running state). Updated remaining bug to note partial status.

---

## [0.5.8] - 2026-03-27

### Added
- **Session history toggle**: sessions list hides stopped sessions by default; "Show history (N)" toolbar button reveals them. Active sessions are always shown.
- **Double-back-press guard**: pressing Back from an active session requires a second press within 2.5 s; a toast warning appears on the first press to prevent accidental navigation.
- **Saved sessions clickable link**: the "X saved sessions" count on the Settings page is now a clickable link that navigates to the sessions list filtered to history (completed/failed/killed).
- **Saved commands management in web UI**: Settings page now has a collapsible "Add Command" form (name + command + description fields) posting to `POST /api/commands`. Existing commands have a Delete button.
- **Output filters management in web UI**: Settings page has a collapsible "Add Filter" form (pattern + action + description) posting to `POST /api/filters`. Existing filters have a Delete button.
- **Directory browser navigation**: file browser in New Session form now has separate navigate-into and select actions. Clicking a folder navigates into it; a "✓ Use This Folder" button selects the current directory. `..` entry navigates up.
- **`session.root_path` config field**: restricts the file browser to a subtree — users cannot navigate above this path. At the root boundary, `..` is hidden and the path is clamped silently.
- **Session resume**: New Session form has an optional "Resume session ID" field. When set, claude-code launches with `--resume <ID>` and opencode with `-s <ID>`. Restart button pre-fills the resume ID from `sess.llm_session_id`.
- **Auto-update daemon**: new `update` config section with `enabled`, `schedule` (hourly/daily/weekly), and `time_of_day` (HH:MM). On schedule, downloads a prebuilt binary from GitHub releases and hot-swaps the running executable.
- **Backend availability in `/api/backends`**: each entry now includes `available` (bool) and `version` string. Web UI shows "(not installed)" for unavailable backends and a warning div when an unavailable backend is selected.

### Fixed
- **Bug: Signal already-linked detection** (`runLink`): detects existing `accounts.json` and `data/<number>/` directory; prints removal instructions instead of overwriting a working setup.
- **Bug: Session delete endpoint** (`DELETE /api/sessions/delete`): `Manager.Delete()` added; kills active session, removes from store, optionally removes tracking directory on disk.
- **Bug: Remote servers "local" default**: local server row now shows as active/connected when `state.activeServer === null`.
- **Bug: needs-input alert**: `NeedsInputHandler` now calls `alertStore.Add` so a Level-Info alert fires whenever a session enters waiting-for-input state.
- **Bug: `SendInput` accepts `running` state**: previously rejected sessions not in `waiting_input`; now also accepts `running`. State transition back to `running` only occurs if session was `waiting_input`.

---

## [0.5.7] - 2026-03-27

### Added
- **Stop button** in session detail view for active sessions (running / waiting_input / rate_limited). Sends a kill request and updates state immediately.
- **Restart button** in session detail view for finished sessions (complete / failed / killed). Pre-fills the New Session form with the original task, backend, and project directory.
- **Session backlog panel** in the New Session view: lists the last 20 completed/failed/killed sessions with one-click Restart to resume any prior task.
- **`POST /api/sessions/kill`** REST endpoint — previously session termination was only accessible via the command parser; now has a dedicated authenticated endpoint.
- **Symlink support in file browser**: `GET /api/files` now follows symlinks to directories so symlinked project folders appear as navigable directories (shown with 🔗 icon).

### Fixed
- **`make install`** now passes `-ldflags "-X main.Version=..."` so the installed binary reports the correct version string instead of the default `0.5.x`.

---

## [0.5.6] - 2026-03-27

### Fixed
- **install.sh**: `--version X.Y.Z` argument parsing was broken (`${!@}` syntax error under `set -euo pipefail`), causing the script to exit immediately with no output. Replaced with a portable `while` loop over a bash array.

---

## [0.5.5] - 2026-03-27

### Fixed
- **Windows cross-compile**: moved `daemonize()` to `daemon_unix.go` (`//go:build !windows`) and `daemon_windows.go` so the Windows binary builds without the Unix-only `syscall.SysProcAttr.Setsid` field. Pre-built Windows/amd64 binary now included in releases.

---

## [0.5.4] - 2026-03-27

### Added
- **`datawatch diagnose [signal|telegram|discord|slack|all]`**: connectivity diagnostic command for all messaging backends. Signal diagnose lists all known groups and validates the configured `group_id`; Telegram, Discord, Slack check live API connectivity. `--send-test` flag sends a test message to verify outbound delivery.
- **`datawatch diagnose --send-test`**: sends a test message to the configured group/channel to verify end-to-end delivery.
- **Signal backend stderr capture**: signal-cli's stderr is now always piped and logged (`[signal-cli stderr] ...`). Java startup errors, auth failures, and exceptions are now visible in daemon.log.
- **Signal backend verbose logging**: `datawatch start -v` now logs every raw JSON-RPC line sent/received from signal-cli, and full pretty-printed notifications for debugging.
- **Signal 4MB scanner buffer**: increased from 64KB default to 4MB to handle large group messages without silent truncation.
- **Signal self-filter fix**: normalises both sides of the number comparison with `strings.TrimPrefix("+")` to avoid format mismatch false-positives/negatives; uses `EffectiveSource()` (preferring `sourceNumber` in signal-cli v0.11+ responses).
- **Signal `EffectiveSource()`**: `Envelope.EffectiveSource()` returns `sourceNumber` (populated in signal-cli v0.11+) if set, falling back to `source`.
- **Signal scanner error logging**: `readLoop` now logs scanner errors explicitly instead of silently exiting.
- **Installer `--version X.Y.Z`**: install script now accepts `--version X.Y.Z` to install a specific release instead of the latest.

### Changed
- **Signal notification dispatch**: non-data envelopes (receipts, typing, sync) are logged in verbose mode and silently skipped; previously they caused confusing empty-message drops.
- **AGENT.md**: added "Supported Commands / Notification Events" requirement to the "New messaging/communication interface" rule; added interactive input, filter, and saved command documentation requirements to the "New LLM backend" rule.
- **`docs/messaging-backends.md`**: added "Command Support by Backend" comparison table; added "Supported Commands" section to Signal, Telegram, Discord, Slack, Matrix, and Twilio; added "Notification Events" table to ntfy and Email.
- **`docs/llm-backends.md`**: added "Command and Filter Support" section covering saved commands, output filters, and interactive input support by backend.

---

## [0.5.3] - 2026-03-27

### Changed
- **AGENT.md**: Added "Functional Change Checklist" under Release Discipline — after any functional change, bump version, run `make release-snapshot` to verify build, verify `datawatch update --check` reports the new version, and confirm install script downloads the prebuilt binary. Documents that `datawatch update --check` (CLI) and `update check` (messaging) are the canonical ways to check for available upgrades.

---

## [0.5.2] - 2026-03-27

### Fixed
- **install.sh**: prebuilt binary is now tried first; Go source build is only used as a fallback when the release archive download fails. Previously, if Go was installed on the host, the installer skipped the prebuilt download entirely and built from source (downloading Go if it wasn't new enough), which was slow and unnecessary.

---

## [0.5.1] - 2026-03-27

### Fixed
- **install.sh**: version is now resolved dynamically from the GitHub releases API at install time (was hardcoded as `"0.1.0"`); falls back to `"0.5.0"` if the API is unreachable.
- **install.sh**: Go fallback installer no longer fails with `mv: cannot overwrite` when the Go versioned directory already exists (`~/.local/go-X.Y.Z` is removed and re-extracted cleanly).
- **install.sh**: prebuilt binary download now uses GoReleaser archive format (`datawatch_VERSION_linux_ARCH.tar.gz`) instead of the old bare-binary URL that was never published.

---

## [0.5.0] - 2026-03-27

### Added
- **`alerts` messaging command**: send `alerts [n]` to any messaging backend to view recent alert history; alerts are also broadcast proactively to all active messaging backends when they fire.
- **`setup llm <backend>`**: CLI and messaging-channel setup wizards for all LLM backends — `claude-code`, `aider`, `goose`, `gemini`, `opencode`, `ollama`, `openwebui`, `shell`.
- **`setup session`**: wizard to configure session defaults (LLM backend, max sessions, idle timeout, tail lines, project dir, skip permissions).
- **`setup mcp`**: wizard to configure the MCP server (enable, SSE, host, port, TLS, token).
- **`datawatch test [--pr]`**: collects non-sensitive interface status (endpoints, binary paths, model names) for all enabled interfaces and optionally opens a GitHub PR updating `docs/testing-tracker.md`.
- **GoReleaser integration**: `.goreleaser.yaml` added; `make release` creates GitHub releases with pre-built binaries for linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, windows/amd64.
- **AGENT.md rules**: added rules for new communication interfaces (testing tracker, data flow docs, config field documentation, security options), and for release discipline (GoReleaser, PR check before commit/release).
- **DNS channel design**: expanded `docs/covert-channels.md` with command subset, query format specification, Mermaid sequence diagram, config block, security model, and threat model table.
- **`docs/testing-tracker.md`**: expanded with LLM backend rows, Web/API section, validation checklists per interface type, and DNS channel row.

### Changed
- `BACKLOG.md`: removed implemented `#future` items (LLM setup wizards, alerts command). Remaining backlog: DNS channel implementation.
- `Makefile`: updated VERSION to 0.5.0; added `release` and `release-snapshot` targets; added `windows/amd64` to `cross` target.

---

## [0.4.1] - 2026-03-27

### Added
- **Upgrade guide** in `docs/operations.md` (new section 2): covers `datawatch update`, in-place upgrade procedure, data compatibility across versions, encrypted store stability, and rollback.
- **`docs/covert-channels.md`**: research notes on DNS tunneling, ICMP, NTP, HTTPS, and steganographic channels for constrained network environments.

### Changed
- `docs/operations.md`: section numbering shifted by one (new section 2 = Upgrading; former sections 2–8 are now 3–9).
- `BACKLOG.md`: removed completed `#today` and `#next` sections; retained `#future` and `#backlog`.

---

## [0.4.0] - 2026-03-27

### Added
- **`--secure` now encrypts all data stores**: sessions.json, schedule.json, commands.json, filters.json, alerts.json are all encrypted with AES-256-GCM when `--secure` is set. A 32-byte symmetric key is derived once at startup via Argon2id and a persistent salt at `~/.datawatch/enc.salt`. Per-write operations use a fresh nonce with no KDF overhead.
- **`internal/secfile` package**: `Encrypt`/`Decrypt`/`ReadFile`/`WriteFile` helpers for AES-256-GCM store encryption without re-running the KDF. All stores have `*Encrypted` constructor variants.
- **`config.DeriveKey` + `config.LoadOrGenerateSalt`**: key derivation and salt persistence for the data encryption layer.
- **Command library** (`internal/session/cmdlib.go`): named reusable command strings backed by `~/.datawatch/commands.json`
- **`datawatch cmd add/list/delete`**: CLI commands for managing the command library
- **`datawatch seed`**: pre-populates the command library and filter store with useful defaults for common AI session interactions
- **Session output filters** (`internal/session/filter.go`): regex-based rules that fire `send_input`, `alert`, or `schedule` actions when output lines match
- **`FilterStore` + `FilterEngine`**: persistent filter store; engine processes each output line against enabled filters via `onOutput` callback on session Manager
- **`Manager.SetOutputHandler`**: new callback on the session manager, called for each output line; used by the filter engine
- **System alert channel** (`internal/alerts/store.go`): persistent alert store at `~/.datawatch/alerts.json`; listener pattern for WebSocket broadcast on new alerts
- **`GET /api/commands`**, **`POST /api/commands`**, **`DELETE /api/commands`**: REST endpoints for command library management
- **`GET /api/filters`**, **`POST /api/filters`**, **`PATCH /api/filters`**, **`DELETE /api/filters`**: REST endpoints for filter management
- **`GET /api/alerts`**, **`POST /api/alerts`** (mark read): REST endpoints for alert history
- **`MsgAlert` WebSocket type**: server pushes new alerts to all connected Web UI clients in real time
- **Alerts view in Web UI**: dedicated Alerts nav tab with unread badge counter; shows full alert history; marks all as read on open
- **Saved Commands section in Web UI Settings**: lists saved commands, allows deletion
- **Output Filters section in Web UI Settings**: lists filters with enable/disable toggle and deletion
- **Scheduler NeedsInputHandler bug fix**: `runScheduler` previously overwrote the combined NeedsInputHandler set in `runStart`. Fixed by extracting `fireInputSchedules` as a standalone helper called from the combined handler instead.

### Changed
- `--secure` mode previously only encrypted `config.yaml`; now all data stores are encrypted when the flag is set
- `session.NewManager` accepts an optional `encKey []byte` variadic parameter for encrypted session store
- `ScheduleStore`, `CmdLibrary`, `FilterStore`, `alerts.Store` all expose `*Encrypted` constructors
- Version bumped to 0.4.0

## [0.3.0] - 2026-03-27

### Added
- **`datawatch update [--check]`**: check for and install updates via `go install` from GitHub releases
- **`datawatch setup server`**: CLI wizard to add/edit remote datawatch server connections with connectivity test
- **`--server <name>` global flag**: target any CLI command at a configured remote server
- **`datawatch session schedule` subcommands**: `add`, `list`, `cancel` for scheduling commands to sessions
- **Schedule daemon goroutine**: fires time-based scheduled commands every 10s; on-input commands fire when sessions enter `waiting_input` state
- **`version` messaging command**: reply with current daemon version
- **`update check` messaging command**: reply with version + update availability
- **`schedule <id>: <when> <cmd>` messaging command**: schedule a command from any messaging backend
- **`GET /api/servers`**: list configured remote servers (tokens masked)
- **`GET/POST/DELETE /api/schedule`**: REST endpoints for schedule management
- **`GET|POST /api/proxy/{serverName}/{path}`**: proxy endpoint for Web UI to reach remote servers
- **Remote Server setup wizard** over all messaging channels (`setup server`)
- **Session ordering in Web UI**: up/down buttons on session cards; order persisted in localStorage
- **Remote server selector in Web UI Settings**: lists configured servers with reachability status; click to select active server
- **`RemoteServerConfig` + `Servers []RemoteServerConfig`** config fields
- **`internal/session/ScheduleStore`**: persistent schedule store at `~/.datawatch/schedule.json`
- **MCP daemon compatibility note** in `docs/mcp.md`

### Changed
- Router `handleSetup` now lists `server` as an available service
- `newRouter` helper in `runStart` wires schedule store, version, and update checker into every router
- Version bumped to 0.3.0 (new features, non-breaking additions)

## [0.2.0] - 2026-03-27

### Added
- **Daemon mode**: `datawatch start` now daemonizes by default (PID file at `~/.datawatch/daemon.pid`, logs to `~/.datawatch/daemon.log`). Use `--foreground` to run in terminal.
- **`datawatch stop` command**: sends SIGTERM to the running daemon; `--sessions` flag also kills all active AI sessions.
- **`datawatch setup <service>` command**: interactive CLI wizards for all 11 backends (signal, telegram, discord, slack, matrix, twilio, ntfy, email, webhook, github, web). Telegram/Discord/Slack/Matrix wizards auto-discover channels/rooms via API.
- **Setup wizards over messaging channels**: `setup <service>` command available via every messaging backend (Signal, Telegram, Discord, Slack, Matrix, etc.) — stateful multi-turn conversation engine in `internal/wizard/`.
- **Config encryption**: `--secure` flag enables AES-256-GCM config file encryption with Argon2id key derivation. Use with `datawatch --secure config init` and `datawatch --secure start --foreground`.
- **`GET/PUT /api/config` REST endpoint**: read and patch backend enable/disable status and server settings. Sensitive fields (tokens, passwords) are masked in GET responses.
- **Web UI backend status**: Settings view now shows enable/disable status for all backends with toggle buttons (calls `/api/config`).
- **Quick-input buttons in Web UI**: when a session is waiting for input, y/n/Enter/Ctrl-C buttons appear above the text input for one-click responses.
- **Web server setup wizard** (`setup web`): enable/disable the web UI and configure host/port/bearer token/TLS from CLI or any messaging backend.
- `server.tls_enabled` and `server.tls_auto_generate` config fields for TLS configuration.
- `docs/testing-tracker.md`: interface validation status tracker for all 14 backends.
- `internal/wizard/` package: `Manager`, `Session`, `Step`, `Def` types for cross-channel stateful wizards.

### Changed
- `datawatch start` now defaults to daemon mode. Existing `--foreground` flag keeps the old behavior.
- `datawatch config init` wizard is now service-agnostic (Signal section is optional).
- "No backends enabled" error message now points at `datawatch setup <service>`.
- `datawatch link` auto-creates the config file if it does not exist.
- `router.NewRouter()` signature updated to accept an optional `*wizard.Manager`.
- `server.New()` signature updated to accept `fullCfg` and `cfgPath` for `/api/config`.
- Version bumped to 0.2.0 (new features, non-breaking additions).

### Dependencies
- `golang.org/x/crypto` promoted from indirect to direct (Argon2id for config encryption)
- `golang.org/x/term` promoted from indirect to direct (password prompts for `--secure`)

## [0.1.4] - 2026-03-26

### Added
- Session naming: set a human-readable name at creation (`--name` flag or PWA) and rename at any time (`session rename`, `/api/sessions/rename`)
- Per-session LLM backend override: `--backend` flag on `session new`, backend selector in PWA
- `session stop-all` CLI command: kill all running sessions on this host
- `backend list` CLI command: list registered LLM backends with active marker
- `completion` CLI command: shell completion for bash, zsh, fish, powershell
- New REST endpoints: `/api/backends`, `/api/files`, `/api/sessions/start`, `/api/sessions/rename`
- Directory browser in PWA: navigate the filesystem to select a project directory when starting a session
- `skip_permissions` config field: pass `--dangerously-skip-permissions` to claude-code
- `kill_sessions_on_exit` config field: kill all active sessions when the daemon exits
- `--llm-backend`, `--host`, `--port`, `--no-server`, `--no-mcp` flags on `datawatch start`
- ANSI escape code stripping from session log output sent via messaging backends
- `NO_COLOR=1` set when launching claude-code to reduce color noise
- Extended claude-code permission dialog patterns for `waiting_input` detection
- Debug logging for Signal group ID mismatches to aid troubleshooting
- Session list now shows `NAME/TASK` and `BACKEND` columns
- AGENT.md versioning rule: every push requires a patch version bump
- AGENT.md docs rule: every commit must include documentation updates

### Changed
- `session new` now uses `/api/sessions/start` REST endpoint when daemon is running
- Config `show` subcommand displays all sections including messaging and LLM backends
- Signal self-filter is now lenient for phone number format variations

## [0.1.0] - 2026-03-26

### Added
- Signal group integration via signal-cli JSON-RPC daemon mode
- QR code device linking (terminal and PWA)
- claude-code session management via tmux
- Session state machine: running, waiting_input, complete, failed, killed
- Async Signal notifications for state changes and input prompts
- Session persistence across daemon restarts (JSON file store)
- Multi-machine support with hostname-prefixed session IDs
- Progressive Web App with real-time WebSocket interface
- Mobile-first dark theme PWA installable on Android home screen
- Browser push notifications for session input requests
- OpenAPI 3.0 specification with Swagger UI at /api/docs
- REST API: sessions, output, command, health, info, link endpoints
- Server-Sent Events for QR linking flow in PWA
- Pluggable LLM backend interface (internal/llm)
- Pluggable messaging backend interface (internal/messaging)
- claude-code directory constraints via --add-dir flag
- Automatic git tracking: pre/post session commits
- CLI session subcommands (no daemon required for local ops)
- Universal Linux installer (root and non-root modes)
- Systemd service with security hardening
- macOS LaunchAgent support
- Windows/WSL2 installation instructions
- Debian .deb packaging configuration
- RPM .spec for RHEL/CentOS/Fedora
- Arch Linux PKGBUILD
- Comprehensive documentation suite
