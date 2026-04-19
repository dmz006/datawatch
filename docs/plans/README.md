# Plans, Bugs & Backlog

Single source of truth for all datawatch project tracking.

---

# Rules
## When plans are inspired by other projects (hackerdave, milla jovovich, etc.), credit the source — see [Plan Attribution Guide](../plan-attribution.md)
## make sure all implementation of bugs or features have 100% (or close) code test coverage and that the fixes or functionality is actually tested through web, api, or any means you have access to validate the code works as requested
## if testing involves creating testing sessions be sure to stop and delete those sessions when done
## No hard-coded configurations — every setting must be configurable via config file, web UI, API, CLI, comm channels, and MCP
## Never reuse bug (B#) or backlog (BL#) numbers — each number is permanent, even after completion. Always increment to the next unused number.

## Unclassified
- In the directory selector in new session and settings, need to be able to create a folder if it doesn't exist
- Review https://github.com/AndyMik90/Aperant for integration as a session service

---

## Open Bugs

| # | Description | Priority | Notes |
|---|-------------|----------|-------|
| | (none) | | |

> B22 fixed in v2.4.3; B23, B24 fixed in v2.4.4; B25 fixed in v2.4.5 — see Completed section

## Open Features

| # | Description | Priority | Effort | Notes |
|---|-------------|----------|--------|-------|
| F7 | libsignal — replace signal-cli with native Go | low | 3-6 months | Plan: [libsignal](2026-03-29-libsignal.md) |
| F10 | Ephemeral container-spawned agents (absorbs BL3, BL16, BL21, BL27, F16) | high | 8 sprints | Plan: [ephemeral-agents](2026-04-17-ephemeral-agents.md) |
| F13 | Copilot/Cline/Windsurf backends | low | 1-2hr each | Plan: [backlog-plans](2026-04-01-backlog-plans.md#bl19-copilotclinewindsurf-backends) |
| F14 | Live cell DOM diffing | low | 3-4hr | Plan: [backlog-plans](2026-04-01-backlog-plans.md#bl2-live-cell-dom-diffing) |
| F17 | Mobile device registry & push token API (`POST /api/devices/register`) — mobile MVP blocker | high | 3-4 days | Plan: [f17-mobile-device-registry](2026-04-18-f17-mobile-device-registry.md) — GH [#1](https://github.com/dmz006/datawatch/issues/1) |
| F18 | Generic voice transcription endpoint (`POST /api/voice/transcribe`) — mobile MVP blocker | high | 2 days | Plan: [f18-voice-transcription-endpoint](2026-04-18-f18-voice-transcription-endpoint.md) — GH [#2](https://github.com/dmz006/datawatch/issues/2) |
| F19 | Federation fan-out sessions (`GET /api/federation/sessions`) — mobile-friendly aggregation | medium | 2-3 days | Plan: [f19-federation-fanout](2026-04-18-f19-federation-fanout.md) — GH [#3](https://github.com/dmz006/datawatch/issues/3) |

---

## Backlog — Remaining Items (46)

All items have plans. Quick wins marked with ⚡.

### Sessions (30)

| ID | Item | Effort | Notes |
|----|------|--------|-------|
| BL5 | Session templates | 1 day | [plan](2026-04-11-backlog-plans.md#bl5-session-templates) |
| BL6 | Cost tracking | 2-3 days | [plan](2026-04-11-backlog-plans.md#bl6-cost-tracking) |
| BL26 | Cron-style schedules | 1-2 days | [plan](2026-04-11-backlog-plans.md#bl26-scheduled-prompts-cron-style) |
| BL27 | Project management | 3-4hr | [plan](2026-04-11-backlog-plans.md#bl27-project-management) |
| BL29 | Git checkpoints + rollback | 1 day | [plan](2026-04-11-backlog-plans.md#bl29-git-checkpoints) |
| BL30 | Rate limit cooldown | 3-4hr | [plan](2026-04-11-backlog-plans.md#bl30-rate-limit-cooldown-system) |
| ⚡BL34 | Read-only ask mode | 2-3hr | [plan](2026-04-11-backlog-plans.md#bl34-read-only-ask-mode) |
| ⚡BL35 | Project summary command | 2-3hr | [plan](2026-04-11-backlog-plans.md#bl35-project-summary-command) |
| BL40 | Stale task recovery | 3-4hr | [plan](2026-04-11-backlog-plans.md#bl40-stale-task-recovery) |
| ⚡BL41 | Effort levels per task | 1-2hr | [plan](2026-04-11-backlog-plans.md#bl41-effort-levels-per-task) |
| ✅BL92 | Write-through session registry | shipped | `Store.Save` already write-through; added explicit `Flush()` API + `TestStore_Save_WriteThrough` regression test pinning the contract. |
| ✅BL93 | Startup session reconciler | shipped | `Manager.ReconcileSessions(autoImport)` walks `<data_dir>/sessions/*/session.json`; main daemon runs it on boot, gated by `session.reconcile_on_startup` config (default false → dry-run + log). REST/MCP/CLI/comm parity. |
| ✅BL94 | `datawatch session import <dir>` | shipped | `Manager.ImportSessionDir` + REST `POST /api/sessions/import` + MCP `session_import` + comm `session import <dir|id>` + CLI `datawatch session import <dir-or-id>`. |
| ✅BL95 | Wire PQC bootstrap envelope into spawn driver + handler | shipped | `agents.pqc_bootstrap` config knob, `Manager.PQCBootstrap` flag, key generation in `Spawn`, `DATAWATCH_PQC_*` env injection in Docker + K8s drivers, dual-path acceptance in `ConsumeBootstrap`, keys burned on consume. |
| ✅BL96 | Wake-up stack extension for F10 recursive/nested agents | shipped | New `Layers.L0ForAgent(id)` overlays per-agent identity from `<data_dir>/agents/<id>/identity.txt` (falls back to host); new `Layers.L4(parentNamespace)` loads parent agent's working context via the BL101 namespace search; new `Layers.L5(selfID, parentAgentID)` lists sibling workers via a new `PeerLister` interface (wired in main from `agents.Manager.List()`); `Layers.WakeUpContextForAgent(self, parent, ns, projectDir)` composes L0+L1+L4+L5 for a spawned child. Top-level spawns degrade gracefully (empty parent = empty L4/L5). |
| BL97 | Agent diaries (mempalace per-agent wing) for F10 workers | 1 day | Mempalace has per-agent wings with diary-style entries; datawatch uses session auto-save. For ephemeral F10 workers, a per-agent wing in the parent's memory captures what the worker did, decisions made, files touched — outlives the worker pod, queryable for retrospectives + future spawn context-priming. |
| BL98 | Contradiction detection (mempalace fact_checker port) | 1 day | Mempalace has `fact_checker.py` scanning the temporal KG for triples that contradict each other (overlapping validity windows). Port to Go: scan on add/query, flag in UI + MCP, optional auto-invalidate. Becomes more useful as multi-agent writes scale up the KG. |
| BL99 | Closets/drawers (mempalace verbatim→summary chain) | 1-2 days | Datawatch implements 3 of mempalace's 6 palace levels — closets (summaries pointing to originals) + drawers (verbatim originals) skipped because verbatim mode stores directly. With F10 multi-agent producing high memory volume, the two-tier chain becomes valuable: queries hit small/fast summary embeddings first, drill into verbatim only when needed. |
| ✅BL100 | Worker memory client (HTTP adapter for shared/sync-back) | shipped | New `memory.HTTPClient` (`NewHTTPClientFromEnv`): reads `DATAWATCH_MEMORY_MODE/NAMESPACE/PROFILE` + `DATAWATCH_BOOTSTRAP_URL/TOKEN`; `Remember` is synchronous in shared mode, buffered in sync-back; `Search` round-trips to parent's `/api/memory/search?q=&profile=` (auto-uses BL101 cross-profile expansion when Profile is set); `Flush` drains the sync-back queue and re-queues unflushed entries on partial failure. Workers wire this in their session-start hook (in worker images that adopt it). |
| ✅BL101 | Server-side cross-profile namespace expansion in /api/memory/search | shipped | `?profile=<name>` (or `?agent_id=<id>`) on `/api/memory/search` triggers `ProjectStore.EffectiveNamespacesFor` resolution + `SearchInNamespaces` (new `NamespacedBackend` interface; SQLite implements, PG returns `ErrNamespaceUnsupported`). Workers query without knowing peer namespaces. |
| ✅BL102 | Worker comm-channel proxy-send (parent route `/api/proxy/comm/{ch}/send`) | shipped | Parent registers every active comm backend by name in main; new `Server.SetCommBackends` + `SetCommDefaults` setters; `POST /api/proxy/comm/{channel}/send {recipient?, message}` looks up the backend, falls back to `commDefaults[channel]` for the recipient, and calls `backend.Send`. 404 on unknown channel, 502 on backend failure, 503 when no registry wired. Worker-side outbound helper deferred to BL100 (shared shape). |
| ✅BL103 | Validator agent image + check logic | shipped | New `internal/validator` pkg: 5 read-only checks (result reported, status==ok, memory-write present, clean terminal event, declared task non-empty) + Verdict (pass/fail/inconclusive) + `Report()` POST to parent. New `cmd/datawatch-validator/main.go` entrypoint reads `DATAWATCH_VALIDATE_TARGET_AGENT_ID` (with `DATAWATCH_TASK` fallback). New `Dockerfile.validator` builds a distroless static image (~5MB). 8 tests: Pass/Fail/Inconclusive paths + transport errors + Report endpoint. |
| ✅BL104 | Peer broker REST proxy + worker pull endpoint | shipped | `POST /api/agents/peer/send` (broker.Send), `GET /api/agents/peer/inbox?id=&peek=` (broker.Drain or Peek). Sender authorization via `AllowPeerMessaging` profile flag enforced inside `broker.Send`. Worker-side outbound helper deferred to BL100 (HTTP memory client lands the same shape). |
| ✅BL105 | Wire `pipelines.Executor` → `agents.Orchestrator` | shipped | New `pipeline.Task.ProjectProfile`/`ClusterProfile`/`Branch` fields + `agents.OrchestratorPlanFromPipeline(p)` translator. Returns `(*OrchestratorPlan, legacyTasks, err)` so a single Pipeline can mix multi-container agent spawns with legacy single-host sessions; BOTH profile fields must be set for orchestrator dispatch (partial = legacy fallback). Executor-side dispatch decision (run agents path vs SessionStarter path) lands in BL105-followup. |
| ✅BL106 | Runtime enforcement of `on_crash` policy in Manager loop | shipped | `Manager.HandleCrash` consults `profile.OnCrash` on Spawn failure, dispatches to `respawnOnce` (single retry, per-(project,branch,parent) budget) or `respawnWithBackoff` (immediate first retry; subsequent crashes deferred via 1m → 2m → 4m → … capped 30m). `ResetCrashRetries` clears the per-key book-keeping. Polling-based crash detection (worker-level Failed transitions outside Spawn) tracked separately under BL112's reconciler. |
| ✅BL107 | REST + UI for agent audit trail query | shipped | `agents.NewFileAuditor` wired in main; `agents.audit_path` + `agents.audit_format_cef` config knobs; `ReadEvents(path, filter, limit)` reader; GET `/api/agents/audit?event=&agent_id=&project=&limit=`; MCP `agent_audit` tool; comm `agent audit [<id>]` verb. UI surfaces in BL107-UI follow-up. |
| ✅BL108 | Wire idle-reaper sweeper into main daemon goroutine | shipped | `Manager.RunIdleReaper(ctx, interval)` background loop wired from main; `agents.idle_reaper_interval_seconds` config knob (default 60s; clamp to 10s minimum; negative disables). Threading `NoteActivity` into the proxy/memory/peer/MCP paths is the still-open companion to this — tracked separately on those paths' BL items. |
| ✅BL109 | Auto-wire datawatch MCP into every spawned LLM session | shipped | New `channel.WriteProjectMCPConfig(projectDir, channelJSPath, env)` writes (and idempotently rewrites) `<projectDir>/.mcp.json` in the modelcontextprotocol/spec shape on every session pre-launch. claude-code keeps its bespoke `claude mcp add` registration alongside; opencode/aider/goose/gemini etc. that honour `.mcp.json` now pick the datawatch server up automatically. Backend-specific writers (aider's `--mcp-config` flag, etc.) are the BL109-followups for each backend that doesn't honour `.mcp.json`. |
| ✅BL110 | MCP-callable `/api/config` (with permission gate) | shipped | `mcp.allow_self_config` + `mcp.self_config_audit_path` knobs (round-trip in TestSave_RoundTrip_MCPSelfConfig). `config_set` MCP tool now refuses unless the gate is true; refuses to flip the gate itself (bootstrap protection); every approved mutation goes to stderr + JSON-lines audit. New `datawatch config set <key> <value>` + `datawatch config get <key>` CLI commands close the every-channel parity loop. |
| ✅BL111 | Wire `secrets.Provider` into `ClusterProfile.CredsRef` + token broker | shipped | `agents.secrets_provider` + `agents.secrets_base_dir` config knobs (default file provider rooted at `<data_dir>/secrets`). New `Manager.SecretsProvider` field + `Manager.ResolveCreds(ref)` method: empty key → no-op, nil provider → literal-key fallback (back-compat for kubeconfig path callers), stub providers bubble `ErrNotImplemented`. Token-broker integration deferred to BL111-tokens follow-up so the existing tokens.json file isn't pulled out from under in-flight workers. |
| ✅BL112 | Service-mode reconciler (re-track service workers after parent restart) | shipped | New `Driver.Discovery` capability interface + `DiscoveredInstance`; Docker + K8s drivers implement it (`docker ps --filter label=…` + `kubectl -n <ns> get pods -l <selector>`). Spawn injects `datawatch.branch` + `datawatch.parent_agent_id` labels alongside the existing role/agent_id/project/cluster labels. New `Manager.ReconcileServiceMode(ctx)` runs at parent boot; per-cluster K8s scan; only profiles with `Mode="service"` are reattached, ephemerals + missing-profile rows are reported as orphans (operator-prune is a follow-up). |
| ✅BL113 | Self-managing platform bootstrap (host + cluster install paths) | shipped | New `docs/install.md` covers single-host (systemd unit) + cluster Helm paths. Helm chart extended with `apiTokenExistingSecret`, `postgres.existingSecret`/`existingSecretKey`, `gitToken.existingSecret`/`existingSecretKey`, and `kubeconfig.existingSecret` value pickups so every credential resolves from k8s Secrets — the parent never reads the operator's home dir at runtime. The deployment template projects `KUBECONFIG=/etc/datawatch/kubeconfig/config` when the kubeconfig Secret is supplied, enabling cross-cluster spawns. Self-managing flow described in §2.5 ties into BL110's `mcp.allow_self_config` gate. |
| ✅BL114 | Shared NFS volume mount for cross-session work | shipped | New `ClusterProfile.SharedVolumes []SharedVolume` schema (`host_path | nfs | pvc` — exactly one source per entry); validation at profile create. Docker driver injects `-v src:dst[:ro]` for HostPath only (NFS + PVC silently skipped — operator pre-mounts NFS on host at any path and refers via HostPath; **no `/mnt/...` prefix is inferred** per the no-hardcoded-config rule). K8s driver renders `volumes` + `volumeMounts` for NFS / PVC / HostPath sources. Documented in `docs/registry-and-secrets.md` §6 with the read-only-first safety pattern. |
| BL115 | Pre-release K8s functional test suite (documented) | 1 day | Per AGENT.md "Release testing" rule: run + document the full functional suite before tagging the major F10 release. Single-host smoke (`spawn_docker.sh`), K8s smoke (`spawn_k8s.sh` against operator's reachable cluster — kind/k3d/real), cross-feature flow (spawn → audit → memory recall → peer broker), UI smoke walk, config-channel parity audit for every new knob added since v2.4.5. Capture results in `docs/testing.md` release-checkpoint section. Failures block the release. |
| BL116 | Sessions list: badge for scheduled commands | 2-3hr | Sessions list (web UI + comm channels) doesn't currently surface a badge when a session has scheduled commands queued. Add `ScheduleStore.CountForSession(id)` + render alongside the waiting-input badge (web) + append `[N scheduled]` to `session list` comm output. |
| BL117 | PRD-driven DAG orchestrator with guardrail sub-agents (post-release) | 2-3 weeks | Big future feature, deferred until after the F10 major release. Master orchestrator agent breaks a feature description into PRD → stories → tasks → DAG, decides per-node whether to fork sub-agents (parallel) or run sequentially based on dependency timeline; eventually surfaces as Gantt-style project tracking. Tied to the DAG: four independent guardrail sub-agents (rules validator / security review / release-readiness / docs+diagrams+architecture) that fork off the master OR fork themselves recursively. **Prior art to review:** nightwire (in `docs/plan-attribution.md`) already implements PRD breakdown + DAG orchestration — review its design before re-deriving the schema. Mempalace's wing/room/hall structure is the natural memory layer. **Builds on:** F10 (spawn primitives), F15 (pipelines + executor), BL96 (recursive wake-up stack), BL103 (validator agent). |

### Intelligence (4 — all depend on F15 pipelines)

| ID | Item | Effort | Notes |
|----|------|--------|-------|
| BL24 | Autonomous task decomposition | 1-2 weeks | [plan](2026-04-11-backlog-plans.md#bl24-autonomous-task-decomposition). Depends on F15 |
| BL25 | Independent verification | 2-3 days | [plan](2026-04-11-backlog-plans.md#bl25-independent-verification). Depends on BL24 |
| BL28 | Quality gates | 2-3 days | [plan](2026-04-11-backlog-plans.md#bl28-quality-gates). Depends on BL24 |
| ⚡BL39 | Circular dep detection | 2-3hr | [plan](2026-04-11-backlog-plans.md#bl39-circular-dependency-detection). Depends on BL24 |

### Observability (4)

| ID | Item | Effort | Notes |
|----|------|--------|-------|
| ⚡BL10 | Session diffing (git diff in alerts) | 2-3hr | [plan](2026-04-11-backlog-plans.md#bl10-session-diffing) |
| BL11 | Anomaly detection | 1-2 days | [plan](2026-04-11-backlog-plans.md#bl11-anomaly-detection) |
| BL12 | Historical analytics + charts | 2-3 days | [plan](2026-04-11-backlog-plans.md#bl12-historical-analytics) |
| BL86 | Remote GPU/system stats agent | 1-2 days | [plan](2026-04-11-backlog-plans.md#bl86-remote-gpu-stats-agent) |

### Operations (4)

| ID | Item | Effort | Notes |
|----|------|--------|-------|
| BL17 | Hot config reload (SIGHUP) | 3-4hr | [plan](2026-04-11-backlog-plans.md#bl17-hot-config-reload) |
| ⚡BL22 | RTK auto-install | 1-2hr | [plan](2026-04-11-backlog-plans.md#bl22-rtk-auto-install) |
| ⚡BL37 | System diagnostics command | 2-3hr | [plan](2026-04-11-backlog-plans.md#bl37-system-diagnostics) |
| BL87 | `datawatch config edit` — safe config editor | 3-4hr | [plan](2026-04-11-backlog-plans.md#bl87-config-edit-command) |

### Collaboration (3)

| ID | Item | Effort | Notes |
|----|------|--------|-------|
| BL7 | Multi-user access control | 1-2 weeks | [plan](2026-04-11-backlog-plans.md#bl7-multi-user-access-control) |
| BL8 | Session sharing (time-limited links) | 1 day | [plan](2026-04-11-backlog-plans.md#bl8-session-sharing) |
| BL9 | Audit log | 3-4hr | [plan](2026-04-11-backlog-plans.md#bl9-audit-log) |

### Messaging (2)

| ID | Item | Effort | Notes |
|----|------|--------|-------|
| BL15 | Rich previews in alerts | 1 day | [plan](2026-04-11-backlog-plans.md#bl15-rich-previews) |
| BL31 | Device targeting (@device routing) | 1 day | [plan](2026-04-11-backlog-plans.md#bl31-device-targeting) |

### Backends & UI (5)

| ID | Item | Effort | Notes |
|----|------|--------|-------|
| BL20 | Backend auto-selection (routing rules) | 1 day | [plan](2026-04-11-backlog-plans.md#bl20-backend-auto-selection) |
| BL42 | Quick-response assistant | 3-4hr | [plan](2026-04-11-backlog-plans.md#bl42-quick-response-assistant) |
| BL69 | Splash screen — custom logo support | 2-3hr | Partially done v1.3.1 |
| BL78 | Chat UI: Gemini chat mode | 3-4hr | Extends BL73 |
| BL79 | Chat UI: Aider/Goose chat mode | 1 day | Extends BL73 |

### Memory & Security (4)

| ID | Item | Effort | Notes |
|----|------|--------|-------|
| BL45 | ChromaDB/Pinecone/Weaviate backends | 1-2 days each | [plan](2026-04-09-memory-backlog.md) Tier 3 |
| BL72 | OpenCode memory hooks | 3-4hr | Extends BL65 to opencode |
| ⚡BL38 | Message content privacy | 2-3hr | [plan](2026-04-11-backlog-plans.md#bl38-message-content-privacy) |
| ⚡BL1 | IPv6 listener support | 1-2hr | [plan](2026-04-11-backlog-plans.md#bl1-ipv6-listener-support) |

### Testing Infrastructure (3)

| ID | Item | Effort | Notes |
|----|------|--------|-------|
| BL89 | Mock session manager for unit tests | 1 day | Interface-based mock for router/server handler tests without tmux |
| BL90 | httptest server for API endpoint tests | 1-2 days | Test all 65 API endpoints with mock dependencies, verify request/response contracts |
| BL91 | MCP tool handler tests | 1 day | Mock MCP client, test all 44 tool handlers without stdio/SSE transport |

### Extensibility (1)

| ID | Item | Effort | Notes |
|----|------|--------|-------|
| BL33 | Plugin framework | 2-3 days | [plan](2026-04-11-backlog-plans.md#bl33-plugin-framework) |

---

## Completed

### Bugs Fixed

| # | Description | Fixed |
|---|-------------|-------|
| B1 | xterm.js crashes and slow load (20s → 32ms) | v2.3.0 |
| B2 | Claude Code prompt detection false positives | v2.3.1 |
| B3 | LLM session reconnect on daemon restart | v2.2.9 |
| B4 | Input bar sometimes disappears in session detail | v2.3.8 |
| B5 | Session history controls off-screen on mobile | v2.3.8 |
| B6 | Function parity gaps across API/MCP/CLI/comm | v2.4.1 |
| B7 | Code test coverage 11.2% → 14.5% (318 tests, pure-logic ceiling reached) | v2.4.1 |
| B20 | RTK update available not showing in Monitor page stats card | v2.4.1 |
| B21 | Monitor Infrastructure card shows wrong protocol and bad formatting | v2.4.1 |
| B22 | Daemon crashes from unrecovered panics in background goroutines | v2.4.3 |
| B23 | Silent daemon death — remaining goroutine recovery, BPF map purge, crash log | v2.4.4 |
| B24 | Update check shows downgrade as "update available" (semver compare in UI/router/auto-updater) | v2.4.4 |
| B25 | Trust prompt invisible — MCP spinner hides what user needs to do (full prompt context in card + Input Required banner with key tip) | v2.4.5 |

### Features & Backlog Completed

| ID | Item | Version |
|----|------|---------|
| BL23 | Episodic memory (SQLite + embeddings) | v1.3.0 |
| BL32 | Semantic search across sessions | v1.3.0 |
| BL36 | Task learnings capture | v1.3.0 |
| BL44 | Memory: auto-retrieve on session start | v1.4.0 |
| BL46 | Memory: export/import | v1.4.0 |
| BL48 | Memory: browser enhancements | v1.4.0 |
| BL50 | Memory: embedding cache | v1.4.0 |
| BL52 | Memory: session output auto-index | v1.4.0 |
| BL62 | Memory: write-ahead log | v1.4.0 |
| BL63 | Memory: deduplication | v1.4.0 |
| BL55 | Memory: spatial organization (wings/rooms/halls) | v1.5.0 |
| BL56 | Memory: 4-layer wake-up stack | v1.5.0 |
| BL57 | Memory: temporal knowledge graph | v1.5.0 |
| BL58 | Memory: verbatim storage mode | v1.5.0 |
| BL60 | Memory: entity detection | v1.5.0 |
| BL68 | Memory: hybrid content encryption | v1.5.1 |
| BL70 | Memory: key rotation and management | v1.5.1 |
| BL54 | Memory: REST API enhancements | v1.6.0 |
| BL61 | Memory: MCP KG tools | v1.6.0 |
| BL47 | Memory: retention policies | v2.0.0 |
| BL49 | Memory: cross-project search | v2.0.0 |
| BL51 | Memory: batch reindexing | v2.0.0 |
| BL53 | Memory: learning quality scoring | v2.0.0 |
| BL59 | Memory: conversation mining | v2.0.0 |
| BL64 | Memory: cross-project tunnels | v2.0.0 |
| BL65 | Memory: Claude Code auto-save hook | v2.0.0 |
| BL66 | Memory: pre-compact hook | v2.0.0 |
| BL67 | Memory: mempalace import | v2.0.0 |
| BL43 | Memory: PostgreSQL+pgvector backend | v2.0.2 |
| BL73 | Rich chat UI (bubbles, avatars, markdown) | v2.1.3 |
| BL77 | Chat UI: Ollama native chat mode | v2.2.0 |
| BL80 | Chat UI: image/diagram rendering | v2.2.0 |
| BL81 | Chat UI: thinking/reasoning overlay | v2.2.0 |
| BL82 | Chat UI: conversation threads | v2.2.0 |
| BL83 | OpenCode-ACP rich chat interface | v2.3.1 |
| BL84 | Tmux history scrolling | v2.3.4 |
| BL85 | RTK auto-update check | v2.3.5 |
| BL88 | `POST /api/memory/save` endpoint | v2.3.8 |
| F4 | Channel parity (threaded conversations) | v1.0.2 |
| F8 | Health check endpoint | v1.0.2 |
| F9 | Fallback chains | v1.0.2 |
| F11 | Voice input (Whisper) | v1.1.0 |
| F12 | Prometheus metrics | v1.0.2 |
| F15 | Session chaining — pipeline DAG executor | v2.4.0 |

### Promoted to Features (still open)

| ID | Promoted to | Status |
|----|-------------|--------|
| BL2 | F14 (Live cell DOM diffing) | Open |
| BL3 | F10 (Container images) | Open |
| BL4 | F15 (Session chaining) | Open |
| BL19 | F13 (Copilot/Cline/Windsurf) | Open |


See [testing.md](../testing.md) for test results and pre-release checklists.
