# Release Notes — v6.0.0 (2026-05-02)

Comprehensive release notes covering all changes from v5.0.0 → v6.0.0.
For per-patch detail see [CHANGELOG.md](../../CHANGELOG.md) and the individual
`RELEASE-NOTES-v5.x.y.md` files in this directory.

---

## What's new in v6.0.0

### 1. Configuration Accessibility closure (BL220)

Every feature area in datawatch is now reachable from all six surfaces:
**YAML config → REST API → MCP tool → CLI subcommand → Comm channel command → PWA / Web UI.**

This was a multi-sprint audit and closure effort (v5.28.9–v5.28.10, Bundles A–F):

**New Comm channel commands (9):**
- `orchestrator` — graph lifecycle (start / stop / status / list) from chat
- `plugins` — enable / disable / test plugins from chat
- `templates` — list / create / delete session templates from chat
- `routing` — routing rules from chat
- `device-alias` — list / manage device aliases from chat
- `detection` — detection filter status and config from chat
- `observer` — full observer config / stats / envelopes from chat
- `analytics` — session analytics summary from chat
- `splash` — branding / splash info and config from chat

**New dedicated MCP tools (3):**
- `detection_status` / `detection_config_get` / `detection_config_set` — detection filter management from IDE
- `dns_channel_config_get` / `dns_channel_config_set` — DNS channel configuration from IDE
- `proxy_config_get` / `proxy_config_set` — proxy settings from IDE

**New CLI subcommands (2):**
- `datawatch analytics [--range=Nd]` — session analytics summary
- `datawatch proxy [get|set ...]` — proxy configuration

**New PWA panels — full-page management views (4):**
- Observer — peer stats, envelope browser, observer config
- Plugins — enable / disable / test all configured plugins
- Routing — create, test, delete routing rules
- Orchestrator — graph list, create, run, monitor

**New PWA panels — Settings sections (9):**
- Settings → LLM → Cost Rates — per-model token rate editor with reset-to-defaults
- Settings → Monitor → Global Cooldown — set / clear with 6 preset durations + reason
- Settings → General → Session Templates — create / delete session templates
- Settings → General → Device Aliases — map device IDs to friendly names
- Settings → General → Branding / Splash — tagline + logo path editor
- Settings → Monitor → Session Analytics — range-selectable bar chart with success rate
- Settings → Monitor → Audit Log — actor / action / limit-filtered event browser
- Settings → Monitor → Pipeline Manager — list + cancel running pipelines
- Settings → Monitor → Knowledge Graph — entity query + triple add

**Audit deliverable:** `docs/config-accessibility-audit.md` — 6-surface feature matrix (24 gap-closure sub-items G1–G24, all closed).

---

### 2. LLM model alias refresh

Per the major-release rule, the hardcoded Claude alias list in `handleClaudeModels()` is refreshed to the current Anthropic model set:

| Alias | Model |
|-------|-------|
| `opus` | `claude-opus-4-7` |
| `sonnet` | `claude-sonnet-4-6` |
| `haiku` | `claude-haiku-4-5-20251001` |

---

### 3. Security: G115 integer-overflow suppressions documented

gosec G115 (integer overflow conversion) produced 45 HIGH findings, all in pre-existing OS/syscall interface code: disk stats (`statfs`), fd numbers (dup2), voice encoding (bit-packing), ollama tap, and diagnostics. Conversions are safe in practice (disk sizes are always positive int64, fd numbers are small). Added global suppression to `.gosec-exclude` with full justification rather than 45 individual `//nolint` annotations.

---

## v5.28.x patch window (2026-04-30 → 2026-05-02)

### v5.28.8 — PWA bug fixes

- **BL222** — Settings → General no longer duplicates Claude-specific fields (`skip_permissions`, `channel_enabled`, `claude_auto_accept_disclaimer`, `permission_mode`, `default_effort`). All now live exclusively in Settings → LLM → claude-code.
- **BL223** — RTK upgrade card renders correctly; was broken by `JSON.stringify()` inside `onclick` attributes. Replaced with `data-cmd` + `addEventListener`.
- **BL224** — `orchestrator-flow.md` Mermaid diagram renders; unquoted `]` chars and `<br/>` in node labels fixed.
- **BL225** — `prd-phase3-phase4-flow.md` Mermaid diagrams render; same label-quoting fix.
- **BL227** — Terminal refits after session-completion indicator is removed; `fitAddon.fit()` + `resize_term` now fires on every indicator-slot change, not just on explicit dismiss.
- `GET /api/channel/info` now exposes `mcp_mode` (SSE vs stdio) and `sse_clients` count.

### v5.28.5–v5.28.7 — Session state + diagnostics

- **BL216 fix** — Session state no longer stalls on completion for claude-code sessions; the final line of output is captured before the state-machine transition.
- Stats panel (BL34) lands as a live PWA metrics overlay with session counts, memory stats, and observer throughput.
- CAP_BPF capability check added to `datawatch diagnose` output.

---

## v5.28.0–v5.28.4 — i18n foundation + federated observer verification

### v5.28.0–v5.28.3 — PWA internationalization (BL214)

Five locale bundles (EN / DE / ES / FR / JA) embedded in the daemon binary from the datawatch-app Compose Multiplatform source. Zero-dependency `window._i18n` with Android-style `%1$s`/`%1$d` placeholders. Auto-detection via `navigator.language`. Settings → General → Language picker. `data-i18n` / `data-i18n-attr` / `data-i18n-html` sweep. Three test guards: key presence, ≥90% EN-parity, required-key list.

Wave 2 (v5.28.1): i18n coverage extended to confirm-modal buttons, session dialog titles, batch-delete count placeholder, alerts-tab loading/empty state, Autonomous templates filter label + New PRD FAB title. Four new universal keys across all five bundles.

Wave 3 (v5.28.3): Language picker promoted to top of Settings → About. PWA UI language drives `whisper.language` automatically when a concrete locale is selected (Auto leaves whisper alone). `whisper.language` form field replaced with read-only "tracks PWA language" indicator.

### v5.28.2 — BL173 federated-observer end-to-end verification

Verified cross-host cluster→parent push end-to-end in the operator's `testing` cluster (3-node Ubuntu 22.04, 10.8.2.0/24 overlay). Deployed parent in-cluster as a Deployment + ClusterIP Service; peer Pod round-tripped: register → push snapshot → aggregator shows peer envelope → cleanup. Runbook in `docs/howto/federated-observer.md`.

### v5.28.4 — BL215 + BL211

- **BL215** — Per-line rate-limit length gate raised 200 → 1024 chars; modern claude rate-limit dialogs are paragraph-length.
- **BL211** — Scrollback no longer pins state detection on stale content; switched `CapturePaneVisible` → `CapturePaneLiveTail` on the state-machine read path.

---

## v5.27.x — Parity sweep, channel bridge, LLM surfaces

### v5.27.10 — BL216 + BL109: channel bridge introspection

- `GET /api/channel/info` returns `{kind, path, ready, hint, node_path, node_modules, stale_mcp_json}` — operators can see which bridge (Go or JS) is active.
- `channel_info` MCP tool; `datawatch channel info` + `datawatch channel cleanup-stale-mcp-json` CLI subcommands.
- PWA Monitor → MCP channel bridge panel with kind badge and stale-mcp-json warning.
- **BL109 fix** — `WriteProjectMCPConfig` now writes the Go bridge path when `BridgePath()` is set (was hardcoding `node + channel.js` since v5.4.0).

### v5.27.9 — BL213 Signal device-linking + BL212 JS fallback memory tools

- `GET /api/link/qr` aliased to the QR SSE stream; `GET /api/link/status` returns live device list from `signal-cli listDevices`; `DELETE /api/link/{deviceId}` removes a linked device with guardrails.
- JS fallback `channel.js` gains `memory_remember`/`memory_recall`/`memory_list`/`memory_forget`/`memory_stats` — operators on ring-laptop / storage instances get memory tools via the JS path.

### v5.27.8 — BL208 + BL210: PRD card + MCP gap closure

- PRD cards harmonised with Sessions card style (`.prd-card` CSS class, status-driven 4px left-border color).
- 11 new MCP tools close remaining BL210 gaps: `memory_wal`, `memory_test_embedder`, `memory_wakeup`, `claude_models`, `claude_efforts`, `claude_permission_modes`, `rtk_version`, `rtk_check`, `rtk_update`, `rtk_discover`, `daemon_logs`.

### v5.27.7 — BL208 animation + BL209 quick-commands + BL212 Go bridge memory tools

- Running badge pulse animation + 3-dot generating indicator below terminal output.
- `GET /api/quick_commands` — operator-editable quick-command list (config: `session.quick_commands`), 15-entry baseline fallback.
- Go bridge binary gains all 5 memory MCP tools; memory tools now work from IDE sessions.

### v5.27.5–v5.27.6 — LLM session options + scroll/rate-limit fixes

- Per-session `permission_mode` / `model` / `claude_effort` overrides via `POST /api/sessions/start`.
- New REST endpoints `GET /api/llm/claude/{models,efforts,permission_modes}`.
- PWA New Session modal: claude-only options block (Permission mode / Model / Effort dropdowns).
- PRD `PermissionMode` + `Task.PermissionMode` for design-only story steps.
- **BL211** scrollback + **BL215** rate-limit fixes landed.

### v5.27.4 — BL205: update check + modern rate-limit patterns

- `GET /api/update/check` — read-only update availability endpoint; PWA migrated off direct api.github.com calls.
- Rate-limit patterns extended: `limit reached`, `weekly usage limit`, `hit weekly limit`, `5-hour limit`, `opus/sonnet limit reached`.

### v5.27.2–v5.27.3 — BL204: subsystem hot-reload + claude disclaimer

- `POST /api/reload?subsystem=<name>` — hot-reload individual subsystems (config / filters / memory) without daemon restart.
- `session.claude_auto_accept_disclaimer` — auto-accepts "trust this folder" and "Quick safety check" prompts when enabled.
- Chat-channel reload wire-up fixed for test router; `claudeDisclaimerResponse` extracted as a testable pure helper.

### v5.27.0–v5.27.1 — Memory maintenance parity + xterm fix

- **Mempalace alignment** — 5 memory quick-wins ported to Go: auto-tag, memory pinning (column + REST `POST /api/memory/pin`), conversation-window stitching, query sanitizer (10 OWASP-LLM01 patterns), repair self-check.
- PWA Settings → Monitor → Memory Maintenance section: `sweep_stale`, `spellcheck`, `extract_facts`, `schema_version` controls.
- **BL198 fix** (carried from v4.x) — aside-collapsed desktop 1px border leak + mobile blank screen resolved; both verified via puppeteer at 4 viewport states.
- **v5.27.1** — xterm refit + input rebind on follow-up prompt; `fitAddon.fit()` + `resize_term` now fires whenever the banner slot changes, not just on explicit dismiss.

---

## v5.26.x — Observer, eBPF, PRD flow, docs

### v5.26.70–v5.26.71 — Mempalace + stdio MCP + GHCR cleanup

- Five mempalace quick-wins: auto-tag, memory pinning, conversation-window stitching, query sanitizer, repair self-check. ZAP workflow gains two active-scan passes.
- `scripts/release-smoke-stdio-mcp.sh` — spawns `datawatch mcp` as a subprocess, validates JSON-RPC initialize + tools/list + tools/call(memory_recall). Fixed nil-reader segfault in `ServeStdio`; memory tools registered always-on.
- `GET /api/memory/wakeup` — on-demand L0+L1+L4+L5 bundle compose.
- GHCR cleanup workflow (weekly + dispatch) deletes past-minor release versions while keeping latest patch + `latest` tag.

### v5.26.64–v5.26.69 — PRD Phase 4: file association + smoke coverage

- `FilesPlanned` in decomposer prompt; per-task `FilesTouched` post-session diff hook (capped at 50 paths); PWA file-edit modal; ⚠ conflict markers when two stories plan the same file.
- Six new smoke sections covering KG add+query, spatial filter, entity detection, per-backend send, stdio MCP, wake-up L4/L5.

### v5.26.60–v5.26.63 — PRD Phase 3: per-story execution profile + approval gate

- Per-story state machine: `pending → awaiting_approval → pending → in_progress → completed`.
- Per-story `ExecutionProfile` override (most-specific-wins).
- `Approve` / `Reject` per story with `RejectedReason` rendered inline.
- Unified Profile dropdown in New Session modal.

### v5.26.0–v5.26.59 — Docs chips, diagrams, eBPF, observer federation

- Settings card-section docs chips wired across all Settings tabs (`settingsSectionHeader` now threads `sec.docs`).
- Diagrams page restructured; asset retention refined (keep-set = every major + latest minor + latest patch on latest minor).
- eBPF Phase 2: `tcp_connect` + `inet_csk_accept` kprobes; `conn_attribution` BPF LRU hash; cross-host federation correlation joining outbound edges to peer listen addrs; `GET /api/observer/envelopes/all-peers`.
- Mempalace alignment: `room_detector`, pinning, conversation-window, query sanitizer, repair self-check.
- Autonomous tab WebSocket auto-refresh on every PRD mutation; 250ms debounce.
- Various PWA fixes: Settings → Comms bind interface (multi-select + self-disconnect guard), FAB position on desktop + mobile, response button icon-only.

---

## v5.0.0–v5.25.0 — Foundation sprint

### v5.25.0 — Diagrams + retention

Diagrams page restructured; asset retention policy: keep every major + latest minor + latest patch on latest minor. `scripts/delete-past-minor-assets.sh` rewritten.

### v5.24.0 — Autonomous tab WS refresh

PRD mutations (Create/Decompose/Run/Cancel/Approve/Reject/…) emit `MsgPRDUpdate` WebSocket messages; PWA debounces 250ms. Session saved-commands dropdown narrowed to 130px.

### v5.23.0 — Settings Comms multi-select + asset retention

Comms bind-interface fields fixed (multi-select + auto-protect connected interface). Session-detail channel/acp mode-badge removed (output-tab conveys it). New `scripts/delete-past-minor-assets.sh`; ran against 105 past releases, deleted 477 assets.

### v5.21.0–v5.22.0 — Observer + whisper config parity

Observer `ConnCorrelator` + `Peers` config fields promoted to `internal/config.ObserverConfig` (YAML/REST reachable). 20 new `applyConfigPatch` cases. `LoopStatus` exposes BL191 Q4+Q6 counters.

### v5.19.0–v5.20.0 — Autonomous full CRUD + docs

`Store.DeletePRD` (recursion-aware) + `Store.UpdatePRDFields` + `DELETE /api/autonomous/prds/{id}?hard=true` + `PATCH /api/autonomous/prds/{id}` + CLI + PWA Edit/Delete. Documentation alignment sweep: `docs/mcp.md` (100+ tools), `docs/api/autonomous.md` (all endpoints since v5.2.0), `openapi.yaml` updated.

### v5.17.0–v5.18.0 — Operator-surface bridge + MCP channel fix

Autonomous config knobs surfaced (`max_recursion_depth`, `auto_approve_children`, `per_task_guardrails`, `per_story_guardrails`) through all 6 surfaces. MCP channel one-way bug fixed: HTTP→HTTPS redirect was blocking bridge `notifyReady()` POST; loopback bypass added.

### v5.16.0 — PRD genealogy + verdicts + cross-host PWA

PRD cards show `↗ parent` + `depth N` badges; Children disclosure; per-task `↳ spawn` affordances. Inline verdict badges (color-coded by severity). Cross-host view button walking `all-peers` envelopes.

### v5.14.0–v5.15.0 — BL190 howto screenshot pipeline

Puppeteer-based screenshot capture via `scripts/howto-shoot.mjs`. 6 → 11 → 19 recipe map; 22 PNGs across 8 howtos; 13 walkthroughs total.

### v5.12.0–v5.13.0 — eBPF kprobes + cross-host federation

`tcp_connect` + `inet_csk_accept` kprobes with `conn_attribution` BPF LRU hash. Cross-host observer federation: `CorrelateAcrossPeers` joins outbound edges from one peer to listen addrs on another.

### v5.10.0–v5.11.0 — PRD guardrails at all levels + howto pipeline

Per-story + per-task guardrails (`Story.Verdicts`, `Task.Verdicts`); config `per_task_guardrails` / `per_story_guardrails`; `GuardrailFn` indirection on Manager. Howto screenshot pipeline first cut.

### v5.8.0–v5.9.0 — Whisper backend inheritance + recursive child PRDs

`inheritWhisperEndpoint` fills whisper endpoint/api_key from openwebui or ollama config when backend is reused. Recursive child PRDs: `Task.SpawnPRD` flag; `Config.MaxRecursionDepth` (default 5); `Config.AutoApproveChildren`. Full 5-surface parity.

### v5.5.0–v5.7.0 — Memory leak audit + flexible LLM + PWA CRUD

Memory leak audit: `session.GetLastResponse` TTL cache, `autonomous.PRD.Decisions` cap at 200, `observer.CorrelateCallers` short-circuit, PWA `state.lastResponse` bounded to 128 entries. Per-task + per-PRD LLM overrides with most-specific-wins. Full CRUD modals with backend/effort/model dropdowns. `datawatch reload` CLI. Two-place version sync (api.go ↔ main.go). BL288 stale MCP cleanup at daemon start.

### v5.1.0–v5.4.0 — Autonomous lifecycle, channel bridge, CRUD first cut

PRD lifecycle state machine (draft → decomposing → needs_review → approved → running → complete + revisions). `PRD.Decisions` audit timeline. Templates via `IsTemplate` + `InstantiateTemplate`. Per-PRD full CRUD second cut with proper modals. Go bridge binary (`cmd/datawatch-channel/main.go`) replaces node+channel.js path. `CleanupStaleJSRegistrations()` sweeps stale `.mcp.json` entries at daemon start. eBPF kprobe attach loader wired (`loader_linux.go`). GHCR container distribution matrix (8 images).

### v5.0.0 — eBPF BTF + BL173 + BL177

eBPF BTF discovery via `/sys/kernel/btf/vmlinux` (no CAP_SYS_PTRACE). eBPF arm64 artifacts (per-arch vmlinux.h tree, dual go:generate). BL173 cluster→parent push verified. CI eBPF drift-check workflow.

---

## Upgrade notes

**v5.x → v6.0.0:**

1. **Binary replacement** — download the v6.0.0 binary for your platform, install, and restart the daemon (`datawatch update && datawatch restart` if the daemon is online, or replace the binary manually and restart the service).

2. **No breaking API changes** — all REST and MCP surfaces are additive. Existing integrations continue to work.

3. **LLM alias refresh** — if you use the `opus`, `sonnet`, or `haiku` shorthand in session profiles or CLI flags, they now resolve to the 2026-Q2 model IDs (Opus 4.7, Sonnet 4.6, Haiku 4.5). If you have hardcoded full model IDs in YAML config, no change is needed.

4. **gosec G115** — if you run gosec against the source tree and see G115 findings, they are documented and suppressed globally via `.gosec-exclude`. No action needed.

5. **BL220 Comm commands** — the 9 new Comm channel commands are available immediately after upgrade. No config change required; they inherit the existing Comm channel authentication.

---

## Statistics

| Metric | Count |
|--------|-------|
| Releases since v5.0.0 | 96 (v5.0.0 → v5.28.10 + v6.0.0) |
| New REST endpoints | 28+ |
| New MCP tools | 25+ |
| New CLI subcommands | 12+ |
| New Comm channel commands | 9 |
| New PWA panels / sections | 13 |
| Go test count | 1508+ |
| Smoke test sections | §7a–§7z+ |
| Locale bundles | 5 (EN / DE / ES / FR / JA) |
| i18n keys per bundle | ~245 |
| howto walkthroughs | 13 |
| howto screenshots | 22 |

---

## What's next (v6.1)

- **BL218** — Channel session-start hygiene: SHA-256 staleness check, user-scope `~/.mcp.json` sweep, JS fallback `Probe()` call, pre-launch log line.
- **BL219** — LLM tooling lifecycle: per-backend artifact setup/teardown, ignore-file hygiene, cross-backend cleanup.
- **BL226** — Service-level alert stream: eBPF/memory/plugin/pipeline failure → Alerts tab System tab.
- **BL228** — Security scanner tools in language layer Dockerfiles (prerequisite for BL221 scan framework).
- **BL210 remaining** — Filters CRUD, backends listing, federation sessions, device register, files browser, 3 session sub-endpoints (MCP tools).
- **Smoke + functional test coverage** — BL220 MCP/CLI/Comm additions added in v5.28.9–v5.28.10 need dedicated smoke sections.
- Secrets manager interface (design discussion).
- BL221 Automata redesign — implementation sprint targeted v6.2.
