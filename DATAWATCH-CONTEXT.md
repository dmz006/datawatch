# DATAWATCH-CONTEXT.md — Datawatch Context & Quick Start

**Last Updated**: 2026-09-06
**Version**: v8.19.8
**Load this before any session** — architecture, AGENT.md rule index, package map, code navigation, and common workflows.

---

## What is Datawatch?

**A distributed control plane for orchestrating AI work** — recursive, episodic, secure, and structured across hosts, clusters, channels, and mobile clients.

### Elevator Pitch

Single-binary daemon (`cmd/datawatch/`) that:

- **Runs AI coding sessions** in tmux — Claude Code, Aider, OpenCode ACP, Goose, Gemini CLI, OpenWebUI, Ollama, Shell
- **Remembers** via episodic memory (PostgreSQL or SQLite, vector search, temporal KG)
- **Plans autonomously** via PRDs (multi-phase reasoning, quality gates, prompt-injection hardening)
- **Debates** via multi-persona Council (12+ built-in personas + custom; v6.x+)
- **Orchestrates** via DAG pipelines, federation mesh (Tailscale + direct + proxy routing)
- **Bridges** messaging — Signal, Telegram, Slack, Discord, Matrix, Email, IMAP-MCP, DNS, Ntfy, Twilio, Webhook, GitHub
- **Attests** security via eBPF observer (CPU, mem, net, GPU live metrics from `/proc` + BPF)
- **Encrypts** sessions, discussions, and configs (BL241 secrets manager + `${secret:name}` references)
- **Sees** via vision backend (`internal/vision/`) — image description injected into session input via `[image:<path>]` tags
- **Extends** via plugins, skills, and the Docs-as-MCP howto system
- **Supports** iOS + Android + Wear OS companion apps (PWA parity target)

### Core Use Cases

1. **Autonomous AI workflows** — spawn child PRDs, debate proposals, quality-gate outputs, execute structured tasks
2. **Multi-channel messaging** — operators get notifications via Signal/Telegram/Slack/Discord and can reply or direct sessions
3. **Session orchestration** — lifecycle state machine, tmux environment, concurrent execution, spawn/council/agent sub-sessions
4. **Mobile companion** — Android, Wear OS, Android Auto, iOS (SwiftUI); REST + WebSocket API is platform-neutral
5. **Team collaboration** — federated discussion WAL, multi-user access control, observer peer mesh
6. **Scheduled automation** — `schedule spawn` for shell/LLM crons without a running session

---

## AGENT.md Rules Index (current line numbers)

**Read AGENT.md before any code changes.** All line numbers reflect the post-2026-09-06 audit state.

| Rule | Line | When it fires |
|---|---|---|
| Pre-Execution Rule | 9 | Every task — re-read relevant sections before coding |
| Session Safety | 19 | Never stop/kill user sessions without explicit ask |
| Scope Constraints | 25 | Work only within `/home/dmz/workspace/datawatch/` |
| Code Quality Rules | 31 | Every commit — compile, interface stability, doc.go |
| Testing Tracker Rules | 40 | Every new interface or changed endpoint contract |
| Git Discipline | 59 | Every commit — conventional format, no force-push |
| Versioning | 67 | Every release — bump BOTH `main.go` + `api.go` |
| Dependency Rules | 96 | When adding a Go module dependency |
| Planning Rules | 102 | Large implementation (3+ files, non-trivial arch) |
| Documentation Rules | 117 | Every behavior-changing commit |
| Project Tracking | 179 | Bugs/backlog in `docs/plans/README.md` — never reuse B#/BL#/F# |
| Release vs Patch Discipline | 204 | Every release — patch vs minor vs major build cadence |
| Configuration Accessibility Rule | 382 | Every new config field — all 6 channels required |
| Localization Rule | 406 | Every new user-facing string — 5 locales + app issue |
| Mobile-Parity Rule | 446 | Every operator-visible PWA change |
| Skills-Awareness Rule | 510 | New session/PRD/agent/comm path — skill hook? |
| Rate Limit Handling | 653 | API quota hit — pause, resume, PAUSED.md |
| Security Rules | 661 | Never log tokens; security-fix downstream review |
| Session Management Rules | 785 | Session lifecycle, timeline.md, git commit |
| Background Shell Cleanup Rule | 791 | After every build+test+smoke cycle |
| Memory Use Rule | 809 | Every session — recall before work, save after |
| Audit Logging Rule | 912 | New audit-event emitter — JSON-lines + CEF tests |
| Testing Requirements | 953 | Every feature/fix — 100% coverage + live tests |
| **Per-sprint checklist (Sections A–E)** | ~976 | **Every release — all rows, all tokens** |
| Monitoring & Observability Rule | 1111 | Every new feature — stats + API + MCP + UI + Prometheus |
| BL274 Docs-as-MCP rules | 1145 | New howto — exec_steps must reference live MCP tools |
| Live Project Cookbook Rule | 1205 | Multi-sprint project — task list always current |
| RTK Integration | 1235 | New LLM backend — check RTK support matrix |
| Detection Pattern Governance | 1243 | New backend/pattern — use DefaultDetection(), not manager.go |
| Decision Making / DIP | 1250 | Unresolved design decision — ask before coding |
| External Issue Triage Rule | 1297 | External (non-dmz006) GH issues — Skills/Plugins first |
| Error-Filing Rule | 1332 | Internal bugs → `docs/plans/` B-number; no GH issue |
| Mobile App Sync Rule | 1353 | PWA change without prior datawatch-app issue |
| Configuration Rules | 1368 | New config field — all 6 registration points |
| Feature Documentation | 1381 | New feature — all 5 access-method rows in docs |
| Work Tracking | 1432 | Multi-task request — plan checklist before starting |
| Memory & Intelligence (MCP tools) | 1484 | Reference for memory tool list + usage patterns |
| Loading DATAWATCH-CONTEXT.md | 1516 | How daemon-launched sessions load this file |

**Release sign-off template**: `docs/agent/RELEASE-SIGNOFF.md` — fill in and include in every release commit body.

---

## Directory Structure

```
.
├── artifacts/
│   └── sec-assess/          Security assessment outputs (phase1 2026-08-28, phase2 2026-08-30)
├── bin/                     Built binaries (gitignored)
├── channel/                 MCP channel bridge binary source + dist
├── charts/datawatch/        Helm chart (templates/ + Chart.yaml + values.yaml)
├── cmd/
│   ├── datawatch/           Main CLI + daemon (var Version here)
│   ├── datawatch-agent/     Agent worker binary (spawned by F10 orchestrator)
│   ├── datawatch-channel/   MCP stdio↔socket bridge (2 per Claude Code session)
│   ├── datawatch-stats/     Stats-cluster sidecar binary
│   ├── datawatch-validator/ Autonomous PRD validator binary
│   └── docs-index-gen/      Howto index generator (feeds BL274 Docs-as-MCP)
├── deploy/
│   ├── launchd/             macOS launchd plist
│   └── systemd/             Linux systemd unit
├── docker/dockerfiles/      Container images (parent-full, agent-*, validator)
├── docs/
│   ├── agent/               RELEASE-SIGNOFF.md template (fill per release)
│   ├── api/                 OpenAPI spec fragments
│   ├── flow/                Mermaid data-flow diagrams
│   ├── howto/               Docs-as-MCP howtos (exec_steps front-matter, BL274)
│   ├── img/                 Documentation images
│   ├── plans/               Feature plans (YYYY-MM-DD-<slug>.md) + README.md backlog tracker
│   │   ├── historical-plans/     Archived plan docs
│   │   └── historical-releasenotes/
│   └── testing/             Testing checkpoint records per major release
│       └── runs/
├── hooks/                   Git hooks (pre-commit, post-commit)
├── install/                 Platform install scripts (macos, systemd, windows)
├── internal/                All daemon packages (see §Internal Package Inventory)
├── packaging/               Arch Linux + Debian packaging specs
├── scripts/                 Build, release, validation, smoke (see §Scripts)
├── templates/               session-CLAUDE.md (injected into daemon-launched sessions)
├── AGENT.md                 ALL guardrails — read before any code change
├── CHANGELOG.md             User-facing change history (no B/BL/F IDs)
├── CLAUDE.md                RTK instructions only (auto-managed, ephemeral)
├── DATAWATCH-CONTEXT.md     This file — load at session start
├── Makefile                 build, install, test, fmt, lint, cross, sync-docs
└── README.md                Feature showcase, quick start, current-release line
```

---

## Internal Package Inventory

All 47 packages under `internal/`. Read the relevant package before touching it.

| Package | Purpose |
|---|---|
| `agents/` | F10 worker orchestration: spawn, reconcile, peer broker, audit trail, Docker/k8s/CF drivers, PQC token, TLS |
| `alertrules/` | Alert rule CRUD, evaluation engine, firing history |
| `alerts/` | Alert model, delivery, MCP surface |
| `algorithm/` | Step-wise algorithm executor (advance, reset, measure) |
| `audit/` | Audit event types, JSON-lines + CEF emitter, `FileAuditor` |
| `auth/` | Token broker (mint/revoke/validate), CBAC, `token_broker.go` |
| `autonomous/` | PRD lifecycle manager, decompose, executor, guardrail registry, scan, quality gates, injection hardening |
| `autonomous/scan/` | `ScanForInjection` — 11+ pattern detector for hostile content |
| `channel/` | MCP channel bridge (stdio↔socket); embed/ contains the compiled channel binary |
| `compute/` | Compute node registry (Ollama remotes, GPU stats), model pull/remove |
| `config/` | `Config` struct, `DefaultDetection()`, secrets resolver (`${secret:name}`), loader |
| `council/` | Multi-persona debate framework, persona CRUD, draft lifecycle, council runner |
| `council/council/` | Council run engine (parallel persona calls, synthesis) |
| `devices/` | Device token registry (FCM + APNs), alias CRUD |
| `docsindex/` | BL274 Docs-as-MCP index builder, howto chunker, exec-steps runner |
| `evals/` | `claude plugin eval` — eval suite runner, sandbox, reporting |
| `federation/` | Cross-instance routing, peer registry, proxy relay, Tailscale mesh |
| `git/` | Git integration helpers (auto-commit, repo detection, clone) |
| `hookinstaller/` | Post-session exit-hook installation and management |
| `identity/` | Operator identity (name, email, avatar, org), configure/update |
| `inference/` | LLM dispatcher (model-agnostic), proxy router, session inference adapters |
| `inference/adapters/` | Per-backend inference adapters for autonomous use |
| `llm/` | LLM backend interface + registry |
| `llm/backends/` | Backend implementations (see §LLM Backends) |
| `llm/claudecode/` | Claude Code-specific helpers (permission mode, effort, hooks) |
| `mcp/` | MCP server: all tool registrations, sampling, elicit, prompts surface |
| `memory/` | Episodic memory (embeddings, semantic search, WAL, KG, scopes, wake-up stack) |
| `messaging/` | Messaging bridge: inbound router, outbound sender, command dispatcher |
| `messaging/backends/` | Backend implementations (see §Messaging Backends) |
| `metrics/` | Prometheus metrics registry, `SetOnCollect` callbacks |
| `observer/` | eBPF observer: CPU/mem/net/GPU metrics collection, alert evaluation |
| `observer/ebpf/` | BPF program generation and loading |
| `observerpeer/` | Observer peer mesh: register, push envelopes, cross-host aggregation |
| `orchestrator/` | DAG orchestrator (graph CRUD, plan, run, cancel, verdicts) |
| `pipeline/` | Pipeline runner (ordered stage execution, cancel, status broadcast) |
| `plugins/` | Plugin lifecycle (install, enable, disable, test, run-subcommand, manifest validation) |
| `profile/` | Session profile CRUD (LLM ref, agent settings, guardrail profiles) |
| `proxy/` | Reverse proxy gateway to federated instances (`/api/proxy/<server>/…`) |
| `router/` | Routing rules engine (pattern matching, LLM routing, routing-rules test) |
| `rtk/` | RTK token-killer integration helpers, discover endpoint |
| `secfile/` | Encrypted file storage (session transcripts, config snapshots) |
| `secrets/` | BL242 secrets vault: store, retrieve, rotate, list (audit-logged) |
| `server/` | REST API + WebSocket + PWA host; `api.go` contains var Version + all routes |
| `server/multiserver/` | Multi-port server (TLS + plain text, Tailscale-aware) |
| `server/web/` | Embedded PWA: `app.js`, `index.html`, `locales/`, `docs/` |
| `session/` | Session lifecycle state machine, `manager.go`, tmux wrappers, completion detection |
| `signal/` | Signal-CLI bridge helpers (group management, attachment sending) |
| `skills/` | Skills registry, manifest parser, PAI-compatible format, resolution into session context |
| `stats/` | `SystemStats` collector, WS broadcast, `bpf/` BPF stats bridge |
| `summarizer/` | LLM-backed session summarizer (dual: memory + TTS), humanization |
| `tailscale/` | Tailscale API client (nodes, ACL, auth key generation) |
| `tlsutil/` | TLS certificate helpers (self-signed, ACME, fingerprint) |
| `tooling/` | Backend artifact cleanup (opencode.json, .mcp.json), gitignore writer |
| `transcribe/` | Whisper transcription backend (local binary, venv at `~/.datawatch/.venv`) |
| `validator/` | Autonomous PRD post-session validator (binary: `datawatch-validator`) |
| `vision/` | Image description backend — `Describe(ctx, data, contentType, prompt)` interface + implementations |
| `wizard/` | Setup wizard (interactive config, per-backend setup steps) |

---

## LLM Backends (`internal/llm/backends/`)

| Backend name | Directory | Notes |
|---|---|---|
| `claude-code` | `llm/claudecode/` | Claude Code CLI in tmux; primary backend; CLAUDE_CONFIG_DIR must be in tmux env (v8.6.3+) |
| `aider` | `backends/aider/` | Aider CLI in tmux |
| `gemini` | `backends/gemini/` | Gemini CLI in tmux |
| `goose` | `backends/goose/` | Goose 1.43.0+ in tmux; PromptBackend variant (`goose-prompt`) |
| `ollama` | `backends/ollama/` | Local Ollama HTTP API; used by autonomous inference |
| `opencode` | `backends/opencode/` | OpenCode ACP in tmux; also `opencode-prompt` and `opencode-acp` variants; per-session `opencode.json` written by daemon |
| `openwebui` | `backends/openwebui/` | OpenWebUI conversation API; `InteractiveBackend` variant |
| `shell` | `backends/shell/` | Shell command one-shot; used by `schedule spawn` |

**Claude Code specifics**: `internal/llm/claudecode/` handles permission modes, effort levels, hook injection, CLAUDE_CONFIG_DIR env var in tmux (`hookEnv` in manager.go, fixed v8.6.3).

**OpenCode specifics**: Per-session `opencode.json` written to project dir at spawn (model + LSP). Ollama provider shape: `{"npm":"@ai-sdk/openai-compatible","options":{"baseURL":"<host>/v1"},"models":{"<name>":{}}}` — the `provider.ollama.apiUrl` block is silently ignored by OpenCode.

---

## Messaging Backends (`internal/messaging/backends/`)

| Backend | Directory | Notes |
|---|---|---|
| Signal | `signal/` | signal-cli subprocess; group + direct messaging |
| Telegram | `telegram/` | Bot API |
| Slack | `slack/` | Events API + web client |
| Discord | `discord/` | Bot gateway |
| Matrix | `matrix/` | nio client; `matrix-nio` Python stack; BL241 secrets-store-only |
| Email / SMTP | `email/` | SMTP outbound + IMAP polling |
| IMAP-MCP | `imapmcp/` | imap-mcp sidecar (SSE receive + REST send); BL340 / GH#127; transport: `imap_mcp.{enabled,url,account,subject_prefix}` config |
| DNS | `dns/` | DNS TXT record polling; second daemon instance required for test env |
| Ntfy | `ntfy/` | ntfy.sh HTTP push |
| Twilio | `twilio/` | SMS via Twilio REST API |
| Webhook | `webhook/` | Generic inbound/outbound HTTP webhook |
| GitHub | `github/` | GitHub issue/PR comment notifications |

---

## REST API Surface (Key Endpoints)

All routes registered in `internal/server/api.go`. Full OpenAPI spec at `docs/api/`.

| Area | Key routes |
|---|---|
| **Sessions** | `GET/POST /api/sessions`, `GET /api/sessions/{id}`, `POST /api/sessions/{id}/restart\|summarize\|bind\|send\|state`, `GET /api/sessions/{id}/children\|last-summary\|current-status\|timeline`, `DELETE /api/sessions/{id}`, `GET /api/sessions/orphaned\|self\|response` |
| **Config** | `GET /api/config`, `PUT /api/config` |
| **Memory** | `POST /api/memory/save\|wakeup\|sweep\|spellcheck\|extract-facts\|pin` |
| **Autonomous** | `GET/POST /api/autonomous/prds`, `GET/PUT/DELETE /api/autonomous/prds/{id}`, `/api/autonomous/prds/{id}/decompose\|run\|cancel\|approve\|reject\|scan\|tasks/{tid}/edit` |
| **Schedules** | `GET/POST /api/schedule`, `DELETE /api/schedule`, `GET /api/schedules` |
| **Stats** | `GET /api/stats` (v=2 for full roll-up), `GET /api/interfaces` |
| **Pipelines** | `GET/POST /api/pipelines`, `POST /api/pipeline/cancel`, `GET /api/pipeline/status` |
| **Files** | `GET/POST /api/files`, `DELETE /api/files`, `GET /api/files/meta`, `GET /api/files/peers/{name}` |
| **MCP sampling** | `GET /api/mcp/sampling-log`, `POST /api/mcp/sample\|elicit`, `GET/POST /api/mcp/prompts` |
| **OpenCode** | `GET /api/opencode/providers`, `PUT /api/opencode/providers` |
| **Council** | `POST /api/council/run`, `GET /api/council/runs/{id}` |
| **Profiles** | `GET /api/profiles`, `POST /api/profiles`, `DELETE /api/profiles/{id}` |
| **RTK** | `GET /api/rtk/discover` |
| **Health** | `GET /healthz`, `GET /readyz` |
| **Test** | `POST /api/test/message` — simulate inbound comm message |
| **Channel** | `GET /api/channel/history` |
| **Proxy** | `* /api/proxy/{server}/…` — federated instance relay |

**Version strings**: Both at `cmd/datawatch/main.go:var Version` and `internal/server/api.go:var Version`. They MUST match on every commit — verify with `grep "var Version" cmd/datawatch/main.go internal/server/api.go`.

---

## MCP Tools by Category

All tools registered in `internal/mcp/server.go`. Full list in `docs/mcp.md` and `docs/cursor-mcp.md`.

| Category | Key tools |
|---|---|
| **Sessions** | `list_sessions`, `start_session`, `kill_session`, `delete_session`, `rename_session`, `restart_session`, `stop_all_sessions`, `send_input`, `session_output`, `session_timeline`, `session_children`, `session_summarize`, `session_set_state`, `session_bind_agent`, `reply_to_parent`, `get_my_session_id`, `list_orphaned_sessions` |
| **Memory** | `memory_recall`, `memory_remember`, `memory_list`, `memory_forget`, `memory_stats`, `memory_pin`, `memory_sweep`, `memory_spellcheck`, `memory_extract_facts`, `memory_wakeup`, `memory_reindex`, `memory_export`, `memory_import`, `memory_learnings`, `memory_wal`, `memory_scope_recall\|borrow\|seed\|promote`, `memory_discussion_*` |
| **Knowledge Graph** | `kg_query`, `kg_add`, `kg_invalidate`, `kg_timeline`, `kg_stats` |
| **System** | `get_version`, `get_config`, `config_set`, `get_stats`, `restart_daemon`, `reload`, `diagnose`, `splash_info`, `get_identity`, `set_identity`, `configure_identity`, `update_identity` |
| **Autonomous PRDs** | `autonomous_prd_create\|get\|list\|edit\|decompose\|run\|cancel\|approve\|reject\|scan\|scan_fix\|scan_results\|scan_rules\|set_llm\|set_type\|set_quality_gates\|set_skills\|set_guided_mode\|set_task_llm\|children\|clone_to_template\|instantiate`, `autonomous_prd_edit_task`, `autonomous_template_*`, `autonomous_type_*`, `autonomous_status`, `autonomous_config_get\|set`, `autonomous_learnings`, `autonomous_scan_config_*` |
| **LLM Registry** | `llm_list\|get\|add\|update\|delete\|test\|enable\|disable\|in_use\|refresh_models\|add_model\|remove_model\|list_models` |
| **Compute Nodes** | `compute_node_list\|get\|add\|update\|delete\|detail\|health\|pull_model\|remove_model\|attach_observer\|detach_observer` |
| **Council** | `council_run\|cancel`, `council_get_run\|list_runs`, `council_persona_draft_*`, `council_persona_oneshot`, `council_personas\|personas_get\|personas_set`, `council_config_get\|set` |
| **Orchestrator** | `orchestrator_graph_list\|create\|get\|plan\|run\|cancel`, `orchestrator_verdicts`, `orchestrator_config_get\|set` |
| **Federation** | `federation_sessions`, `federation_peer_list\|get\|add\|update\|delete\|test`, `federation_group_*`, `federation_meta_peers` |
| **Observer** | `observer_stats\|envelopes\|envelopes_all_peers\|envelope\|config_get\|set`, `observer_peers_list\|get\|stats\|register\|delete\|free\|by_node`, `observer_agent_stats\|agent_list` |
| **Schedules** | `schedule_add\|list\|cancel`, `schedule_spawn` |
| **Guardrails** | `guardrail_library_list`, `guardrail_profile_create\|get\|list\|update\|delete`, `per_automaton_guardrails_set`, `session_guardrail_run` |
| **Skills** | `skills_list\|get\|skill_load`, `skills_registry_list\|get\|create\|update\|delete\|sync\|unsync\|connect\|add_default\|available` |
| **Alerts** | `get_alerts`, `mark_alert_read`, `alert_rule_list\|get\|create\|update\|delete\|enable\|disable\|firings` |
| **Files** | `files_list\|upload\|delete\|meta` |
| **Vision** | `vision_describe` |
| **Secrets** | `secret_list\|get\|set\|delete\|exists`, `secrets_vault_status` |
| **Devices** | `device_register\|list\|delete`, `device_alias_list\|upsert\|delete` |
| **Profiles** | `profile_list\|get\|create\|update\|delete\|set_agent_settings\|smoke` |
| **Queue** | `queue_push\|claim\|complete\|fail\|list` |
| **Results** | `result_put\|get\|list\|delete` |
| **Tailscale** | `tailscale_status\|nodes\|acl_push\|acl_generate\|auth_key` |
| **Telemetry** | `telemetry_get\|list` |
| **Algorithms** | `algorithm_list\|get\|start\|advance\|abort\|reset\|edit\|measure` |
| **Pipelines** | `pipeline_start\|status\|cancel\|list` |
| **Hooks** | `exit_hook_list\|add\|delete\|enable\|disable` |
| **Discussion** | `discussion_subscribe\|unsubscribe\|subscriptions`, `memory_discussion_write\|recall\|wal\|participants` |
| **Misc** | `ask`, `assist`, `copy_response`, `get_prompt`, `research_sessions`, `cooldown_set\|clear\|status`, `agent_list\|get\|spawn\|terminate\|logs\|audit`, `ollama_stats`, `matrix_status\|test`, `backends_list\|active`, `cost_summary\|usage\|rates`, `analytics`, `rtk_check\|discover\|update\|version`, `tooling_status\|gitignore\|cleanup`, `smoke_forward_config_get\|set`, `dashboard_config_get\|cards_list\|card_update\|add\|delete`, `channel_info\|diagnostics\|routing_config_get\|set`, `filter_list\|add\|delete\|toggle`, `detection_config_get\|set\|status`, `proxy_config_get\|set`, `dns_channel_config_get\|set`, `docs_read\|search\|list_howtos\|apply\|trust_*`, `claude_models\|efforts\|permission_modes`, `eval_list_suites\|run\|get_run\|list_runs` |

---

## PWA & Frontend (`internal/server/web/`)

- **`app.js`** — Single-file PWA frontend. All session management, settings, autonomous PRDs, council, memory, alerts, monitor, federation panels. i18n via `t(key)` / `data-i18n` wired at startup.
- **`index.html`** — Shell; mounts the PWA.
- **`locales/`** — 5 JSON bundles: `en.json`, `de.json`, `es.json`, `fr.json`, `ja.json`. Keyed in Compose-Multiplatform Android naming convention (`nav_*`, `action_*`, `settings_*`, `sessions_*`, etc.).
- **`docs/`** — Embedded docs tree (mirrored from canonical `docs/` by `make sync-docs`). `make build` + `make cross` run this automatically; `go build` does NOT.

**Locale guard test**: `internal/server/v5280_locales_test.go::TestLocales_CommonNavKeysPresent` — `mustHave` slice guards high-visibility keys. Add every new operator-visible key here.

**`apiFetch` contract** (v8.19.8): 204/205 responses resolve to `null` (not parsed as JSON). Every `/api/…` route returning 204 is safe. Prior to v8.19.8, `r.json()` was called unconditionally on `r.ok` — caused `Unexpected end of JSON input` on empty 204 bodies (RFC 7231 §3.3).

**`fetchCurrentStatus`**: `GET /api/sessions/{id}/current-status` — returns 200 + `{no_change:true}` on no-new-output, or 200 + session output delta. Never 204 (v8.19.8 fix for B54).

---

## Configuration System (`internal/config/`)

**`config.go`** — `Config` struct top-level fields (yaml tags):

| Field | yaml tag | Purpose |
|---|---|---|
| `Session` | `session` | `SessionConfig` — tmux, file service root, expand image tags, settle ms |
| `Server` | `server` | TLS, port, token (never log this), CORS, allowed origins |
| `Memory` | `memory` | Backend (sqlite/postgres), embedder, dimensions, auto-save, retention |
| `LLM` | `llm` | Default backend, named backends list |
| `Messaging` | `messaging` | Per-backend enable/disable + creds |
| `Observer` | `observer` | eBPF enable, metrics interval, alert thresholds |
| `Federation` | `federation` | Peer list, Tailscale config, routing rules |
| `Autonomous` | `autonomous` | PRD defaults, decompose LLM, verifier, quality gates |
| `Detection` | `detection` | `DefaultDetection()` patterns — completion, rate-limit, input-needed, prompt |
| `Secrets` | `secrets` | Vault backend (sqlite/postgres), encryption key ref |

**Config accessibility**: Every field must be reachable via YAML + `GET/PUT /api/config` + `config_set` MCP + `configure key=value` comm + CLI + PWA Settings. Use `POST /api/test/message` to verify comm channel commands.

**`${secret:name}` references**: Resolved by `internal/config/secrets_resolver.go`. New backends MUST use `${secret:...}` for credential fields (Secrets-Store Rule, AGENT.md line 661+).

**`DefaultDetection()`**: All completion, rate-limit, input-needed, and prompt patterns live here. Never hardcode patterns in `manager.go` or backend code — add to `DefaultDetection()` and let operators override in config.

---

## Vision System (`internal/vision/`)

Introduced v8.15.0. Interface: `Describe(ctx, []byte data, string contentType, string prompt) (string, error)`.

**`expandImageTags()`** in `internal/server/api.go`: Replaces `[image:<path>]` tokens in session `send_input` text with `[image: <description> | path: <path>]` before delivery. Wired into both REST `handleSessionSend` and WebSocket `executeCommand/CmdSend`. Path-traversal guard via `checkPathTraversal(root, path)` (AGENT.md Security Rules). `fileServiceRoot()` priority: `FileServiceRoot` config → `RootPath` → `$HOME`.

**Tests**: `internal/server/vision_test.go` — 8 unit tests (NoVisioner, NoTag, FileNotFound, PathOutsideRoot, VisionerError, HappyPath, EmptyDescription, MultipleTags). All use `t.TempDir()` as root via `expandImageTagsServer` helper.

**PWA integration**: v8.19.0 image attachment (📎 / 📷 buttons) uploads to `POST /api/files` then references the server path in `[image:<path>]` tag in send_input.

**MCP tool**: `vision_describe` — takes `file_path` or `url`, returns description string.

---

## Key Files for Rule Contract Work

When working on a rule from the AGENT.md checklist, these are the exact files to touch:

### Versioning (A3)
- `cmd/datawatch/main.go` — `var Version = "X.Y.Z"`
- `internal/server/api.go` — `var Version = "X.Y.Z"` (must match)
- Verify: `grep "var Version" cmd/datawatch/main.go internal/server/api.go`

### CHANGELOG + README (A4, A5)
- `CHANGELOG.md` — add entry under `## [X.Y.Z] — YYYY-MM-DD`; no B/BL/F IDs
- `README.md` — update `**Current release: vX.Y.Z (DATE).**` line at top
- Verify no IDs leaked: `grep -n "B[0-9]\+\|BL[0-9]\+\|F[0-9]\+" CHANGELOG.md README.md`

### Backlog Refactor (A6)
- `docs/plans/README.md` — close shipped B/BL/F items, clear `## Unclassified`
- Move completed bugs to `## Completed Bugs (archived)`
- Move completed backlog to `## Completed Backlog` (with version shipped)

### Testing Tracker (B1)
- `docs/testing-tracker.md` — add section per new interface or changed contract
- Two rows minimum: unit test (Tested=Yes) + live validation requirement (Validated=No until confirmed)
- Format: `| Test case | Tested | Validated | Test Conditions | Notes |`

### Localization (B2, B3)
- `internal/server/web/locales/en.json` — add key + English value
- `internal/server/web/locales/{de,es,fr,ja}.json` — add same key with EN placeholder or translation
- `internal/server/v5280_locales_test.go` — add key to `mustHave` slice in `TestLocales_CommonNavKeysPresent`
- `internal/server/web/app.js` — wire `t(key)` or `data-i18n="key"` at call site

### Mobile Parity (B4)
- File `gh issue create` against `dmz006/datawatch-app` with: version, description, acceptance criteria
- Title format: `[sync] <feature> — <description>`; body references daemon version + what changed

### Internal Bug Tracking (B5)
- `docs/plans/README.md` — add row to `## Open Bugs` table: `| B<N+1> | description | fix | vX.Y.Z |`
- Reference `B<N>` in commit message (`plans: B<N>`)

### Config Parity (B6)
- `internal/config/config.go` — add field to appropriate sub-struct
- `internal/server/api.go` — `handleGetConfig` map + `applyConfigPatch` switch
- `internal/server/web/app.js` — `GENERAL_CONFIG_FIELDS` / `LLM_CONFIG_FIELDS` / `COMMS_CONFIG_FIELDS` array
- `docs/config-reference.yaml` — add field with type, default, description
- `docs/implementation.md` — add field
- MCP: `internal/mcp/server.go` `config_set` handler (or auto-covered if using `handleGetConfig`/`handlePutConfig`)
- Comm: `internal/messaging/` command dispatcher (usually `configure key=value` is generic)

### Observability (B7)
- `internal/stats/collector.go` — add field(s) to `SystemStats`
- `internal/server/api.go` — add `GET /api/<subsystem>/stats` handler
- `internal/mcp/server.go` — add `<subsystem>_stats` tool
- `internal/server/web/app.js` — add card to `renderStatsData()`
- `internal/metrics/metrics.go` — add Prometheus counter/gauge + `SetOnCollect` callback

### Smoke Extension (B12)
- `scripts/release-smoke.sh` — add new numbered section before `H "Summary"` block
- Pattern: `H "<N>. <description>"`, curl the endpoint, assert shape, `ok`/`ko`/`skip`
- Add `add_cleanup <kind> <id>` for any entity created; extend `cleanup_all` with the new `kind`

### Gosec / Security (C2)
- `.gosec-exclude` — global rule suppressions (one rule ID per line)
- Inline `//nolint:gosec // <justification>` for per-finding suppression after validation
- G304 (file inclusion): must have `checkPathTraversal` guard before `os.ReadFile`
- G112 (Slowloris): add `ReadHeaderTimeout` to every `http.Server` definition

### Trivy / CVE (container security)
- `.trivyignore` — CVE suppressions with documented rationale comment
- `docker/dockerfiles/` — per-image Dockerfiles; bump base image on CVE fix

### Release Build (A10, D1)
- Patch: `make build` → `./bin/datawatch` (host-arch only, runs `make sync-docs`)
- Minor/Major GH release: `make cross` → `./bin/datawatch-{linux,darwin}-{amd64,arm64}` + `datawatch-windows-amd64.exe`
- Never `go build ./cmd/datawatch/` directly — embedded docs won't sync

### CI Check (A11)
- `gh run list --limit 20 --json status,conclusion,name,databaseId,createdAt | jq -r '.[] | select(.conclusion=="failure") | "fail [\(.databaseId)] \(.name) \(.createdAt)"'`
- Investigate every failure before deleting; security failures escalate to operator (never auto-delete)

### Release Sign-Off
- `docs/agent/RELEASE-SIGNOFF.md` — template; copy into commit body, fill every row
- Every row maps to a Section A–E item in the per-sprint checklist (AGENT.md ~line 976)

---

## Build, Test, Release

```bash
# Build (host-arch) — always use make, not go build
make build          # → ./bin/datawatch; also runs make sync-docs
make install        # → ~/.local/bin/datawatch

# Cross-compile (minor/major GH releases only)
make cross          # → ./bin/datawatch-{linux,darwin}-{amd64,arm64} + windows

# Test
make test           # = go test ./...
rtk go test ./...   # Token-efficient equivalent
go test ./internal/server/... -run TestLocales   # Locale guard

# Lint / format
make fmt
make lint           # golangci-lint (errcheck, gosec, etc.)

# Sync embedded docs
make sync-docs      # Mirrors docs/ → internal/server/web/docs/ (auto-runs via make build/cross)

# Smoke (release validation)
bash scripts/release-smoke.sh                  # Full smoke
SMOKE_ONLY="S1,S4,S54" bash scripts/release-smoke.sh  # Targeted

# Pre-release scans
~/go/bin/gosec -exclude="$(grep -v '^#' .gosec-exclude | tr '\n' ',')" -fmt text -quiet ./...
go list -m -u all 2>/dev/null | grep '\['      # Outdated deps

# Version check
grep "var Version" cmd/datawatch/main.go internal/server/api.go

# RTK — always prefix commands
rtk go build ./...
rtk git status && rtk git diff
rtk go test ./...
```

---

## Scripts Inventory (`scripts/`)

| Script | Purpose |
|---|---|
| `release-smoke.sh` | Main smoke test — 55 sections, `H "N. title"` pattern; run before every minor/major |
| `release-smoke-secure.sh` | Security-focused smoke (authenticated paths) |
| `release-smoke-stdio-mcp.sh` | MCP stdio channel smoke |
| `release-smoke-wakeup.sh` | Memory wake-up stack smoke |
| `pre-sprint-rules.sh` | Prints the per-sprint checklist table from AGENT.md |
| `sprint-audit.sh` | Audit helper — checks current commit message for required tokens |
| `check-curated-howtos.sh` | BL274: every curated howto's exec_steps must reference live MCP tools |
| `check-howto-coverage.sh` | BL274: every howto must be curated or in LLM-only allowlist |
| `check-plugin-manifests.sh` | Plugin manifest validation (docs.files existence) |
| `check-docs-sync.sh` | Verifies `internal/server/web/docs/` matches `docs/` |
| `check-no-internal-refs.sh` | Greps user-facing docs for B/BL/F tracker IDs |
| `delete-past-minor-assets.sh` | Asset retention cleanup (run post-GH-release) |
| `delete-past-minor-containers.sh` | GHCR container retention cleanup |
| `container-upgrade.sh` | Base image upgrade helper |
| `sync-docs-to-webfs.sh` | Manual trigger for sync-docs (also run by Makefile) |
| `howto-seed-fixtures.sh` | Seeds howto test fixtures |
| `run-tests.sh` | Full test suite runner with coverage |
| `tidy-plans.sh` | Plans/ hygiene helper |

---

## Common Workflows

### Fix a Bug (Error-Filing Rule, line 1332)

1. `memory_recall "description of bug area"` — check prior fixes
2. Reproduce + locate (tests or live session)
3. Fix (minimal scope, one logical change)
4. `rtk go test ./...` must pass
5. Add B-number to `docs/plans/README.md`
6. Commit with conventional message; include `plans: B<N>` token
7. `memory_remember` the pattern after confirming fix

**Do NOT version-bump or write CHANGELOG unless user asks to release.**

### Add a Feature (New Feature checklist)

1. Create plan doc `docs/plans/YYYY-MM-DD-<slug>.md` if 3+ files or non-trivial
2. Add BL-number to backlog in `docs/plans/README.md`
3. Implement with 100% coverage (`go test ./...`)
4. Wire all 6 config channels (B6 config-parity)
5. Add observability — `SystemStats` + `/api/<sub>/stats` + MCP tool + Monitor card (B7)
6. Add locale key + 5 bundles + guard test + app issue (B2, B3, B4)
7. Update `docs/testing-tracker.md` (B1)
8. Document all 5 access methods (B8)
9. CHANGELOG + README + backlog refactor (A4, A5, A6)
10. Run sign-off checklist (Section A + applicable B rows)

### Release (when user says "release")

1. Bump version in BOTH files — verify with grep (A3)
2. Fill `docs/agent/RELEASE-SIGNOFF.md` — every row
3. `make build` (patch) or `make cross` (minor/major)
4. Run smoke per cadence (C3)
5. Pre-release dep audit + gosec (C1, C2)
6. Commit with all tokens in body
7. Tag + push: `git tag -a vX.Y.Z -m "vX.Y.Z" && git push && git push --tags`
8. For GH releases: `gh release create` with binaries; then `scripts/delete-past-minor-assets.sh`
9. CI runner check: `gh run list --limit 20` (A11)

**Never `make cross` or `gh release create` manually for patches — only for minor/major.**

### Memory-Driven Development

```
memory_recall "area of work"          # Before starting
kg_query "entity or module"           # Understand relationships
research_sessions "cross-session Q"   # Prior session context

# After finding something non-obvious:
memory_remember "Decision/pattern: ... Why: ... Apply when: ..."
kg_add "BL96" "depends_on" "federation"
```

---

## Recent Context (v8.14.0 → v8.19.8)

| Version | Feature |
|---|---|
| **v8.19.8** | `fix(pwa)`: "What's it doing?" JSON-parse crash on no-new-output — `handleSessionCurrentStatus` 204→200+`{no_change:true}`; `apiFetch` 204/205→null guard; 5 locale bundles + datawatch-app#160 |
| **v8.19.5–7** | Path traversal guard in `expandImageTags`; errcheck fixes; 7 Trivy CVE suppressions |
| **v8.19.3–4** | `expandImageTags()` — vision injection in REST + WebSocket `send_input`; 8 unit tests |
| **v8.19.2** | PWA session-list select-all scoped to filtered visible sessions |
| **v8.19.0** | `feat(pwa)`: image attachment + camera capture (📎/📷 buttons); `POST /api/files` multipart; `GET /api/files/meta` |
| **v8.18.0** | `feat(autonomous)`: Prompt Injection Hardening — `ScanForInjection` (11+ patterns), `CheckInjectionGuard`, data-boundary tags in all 3 LLM call sites, security preamble in verifier/guardrail prompts |
| **v8.17.0** | `feat(autonomous)`: PRD Quality Gates — `SetPRDQualityGates`, `resolveQualityGates`, `Task.QualityGateResult`, `go build .` as gate command |
| **v8.16.0** | `feat(autonomous)`: Verifier Git-Diff Grounding — verifier receives actual diff, not just spec |
| **v8.15.0** | `feat(vision)`: Vision Input System — `internal/vision/`, `vision_describe` MCP tool, `expandImageTags` foundation |
| **v8.14.0** | BL363 testing gap closure for Goose backend |
| **v8.13.36–39** | Goose backend (4-task delivery) — `internal/llm/backends/goose/` |
| **v8.13.34–35** | OpenCode: remote compute-node Ollama model picker fix; alternate-screen detection |
| **v8.13.21–33** | CI/security: ZAP stability, goreleaser retry, Trivy CVE suppressions, release.yaml reliability |

### Security Assessment Context

`artifacts/sec-assess/2026-08-28-phase1/` and `artifacts/sec-assess/2026-08-30-phase2/` contain the hostile-LLM security assessment outputs (BL365 + HLLM-001…009 findings). See `docs/plans/README.md` for current status of security backlog items.

---

## Known Good Patterns

1. **Session state machine** — `FirstTick` guard must skip both detection AND activity marking
2. **Backend resume** — When backend server restarts, create fresh session; never reconnect old ID
3. **CLAUDE_CONFIG_DIR** — Must be in tmux session env for claude-code sessions; set via `hookEnv` in `manager.go` (v8.6.3+). Sessions started before v8.6.3 won't have it — restart daemon to fix
4. **SendInput settle** — `scheduleSettleMs` (default 200ms) applies to ALL send sources since v8.12.9. Set >0 in config to fix Enter not delivered to React Ink TUIs
5. **MCP channel "waiting" error** — `CLAUDE_CONFIG_DIR` not injected into tmux session env; fixed in v8.6.3 hookEnv
6. **OpenCode Ollama provider shape** — `{"provider":{"ollama":{"apiUrl":...}}}` silently ignored. Use `{"npm":"@ai-sdk/openai-compatible","options":{"baseURL":"<host>/v1"},"models":{"<name>":{}}}` in opencode.json
7. **expandImageTags + path guard** — tests must set `FileServiceRoot` to `t.TempDir()` via `expandImageTagsServer` helper; default home-dir root blocks `/tmp` paths
8. **schedule spawn** — `datawatch schedule spawn --shell <cmd> --cron "0 * * * *" --path <dir>` — overlap guard prevents stacked runs; `TmuxSession==""` sessions must be skipped in `ResumeMonitors`
9. **E2E tests** — tests needing live sessions/hooks/Automata/smoke/bindings must create them inline, never skip. Whisper: `~/.datawatch/.venv`. DNS: 2nd test instance
10. **SSE + polling** — SSE primary, 202+polling utility, exponential backoff, Tailscale-aware low-power reconnect
11. **`extra_mcp_servers`** — `session.extra_mcp_servers` YAML field injects MCP servers into `.mcp.json` at every spawn
12. **imap-mcp transport** — SSE receive (`inbound.command` only) + REST send; config: `imap_mcp.{enabled,url,account,subject_prefix}`; v8.9.23 / BL340 / GH#127
13. **Autonomous: `TmuxSession==""` skip** — spawn/council/agent sessions must be skipped before `SessionExists()` in `ResumeMonitors`; missing guard force-failed all imap-hourly-rules runs

---

## Gotchas to Avoid

- **No version bump without ask** — user must say "release" or "bump the version"
- **Interface stability** — `SignalBackend`, `LLMBackend` are breaking-change boundaries; changes require major bump
- **Both Version strings must match** — `cmd/datawatch/main.go` AND `internal/server/api.go`; the v8.9.2 incident missed one file
- **No internal IDs in user-facing docs** — B/BL/F numbers only in `docs/plans/`, not CHANGELOG/README/operations/setup
- **No `go build` for releases** — use `make build` (patch) or `make cross` (minor/major) so `make sync-docs` runs
- **Never `make cross` or `gh release create` manually for a patch** — CI handles goreleaser for tagged releases; only run `make cross` + `gh release create` when user explicitly says "release"
- **`server.token` must never appear in logs** — it's the daemon auth token; any log call touching config must exclude it
- **GH issues are external-only** — internal bugs go to `docs/plans/` B-numbers; GH issues only for external reporters or cross-project items (datawatch-app, imap-mcp)
- **Locale guard test** — when adding a high-visibility locale key, always add it to `TestLocales_CommonNavKeysPresent` mustHave slice
- **Smoke extension** — every new operator-facing endpoint or behaviour gets a smoke section before ship

---

## Quick Reference

| Resource | Purpose |
|---|---|
| [AGENT.md](AGENT.md) | All guardrails + per-sprint checklist (Sections A–E) |
| [docs/agent/RELEASE-SIGNOFF.md](docs/agent/RELEASE-SIGNOFF.md) | Release commit body template — fill every row |
| [docs/testing-tracker.md](docs/testing-tracker.md) | Interface test matrix (unit + live validation) |
| [docs/plans/README.md](docs/plans/README.md) | Backlog tracker — B/BL/F numbers, open bugs, features |
| [CHANGELOG.md](CHANGELOG.md) | User-facing change history (no internal IDs) |
| [README.md](README.md) | Feature showcase; current-release line at top |
| [docs/mcp.md](docs/mcp.md) | MCP tool documentation |
| [docs/architecture.md](docs/architecture.md) | Component overview + data flow |
| [docs/operations.md](docs/operations.md) | Deployment, config, security |
| [docs/config-reference.yaml](docs/config-reference.yaml) | All config fields with types and defaults |
| [docs/llm-backends.md](docs/llm-backends.md) | LLM backend setup docs |
| [docs/messaging-backends.md](docs/messaging-backends.md) | Messaging backend setup docs |
| [docs/howto/](docs/howto/) | Docs-as-MCP operator howtos (BL274) |
| [docs/parity-status.md](docs/parity-status.md) | PWA / Android / iOS feature parity table |
| [docs/security-review.md](docs/security-review.md) | Security review findings and status |
| [scripts/release-smoke.sh](scripts/release-smoke.sh) | Smoke test runner (55 sections) |
| [templates/session-CLAUDE.md](templates/session-CLAUDE.md) | Rules injected into daemon-launched sessions |
