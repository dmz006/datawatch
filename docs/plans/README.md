# Plans, Bugs & Backlog

Single source of truth for all datawatch project tracking.

---

# Rules
## When plans are inspired by other projects (hackerdave, milla jovovich, etc.), credit the source — see [Plan Attribution Guide](../plan-attribution.md)
## make sure all implementation of bugs or features have 100% (or close) code test coverage and that the fixes or functionality is actually tested through web, api, or any means you have access to validate the code works as requested
## if testing involves creating testing sessions be sure to stop and delete those sessions when done
## No hard-coded configurations — every setting must be configurable via config file, web UI, API, CLI, comm channels, and MCP
## Never reuse bug (B#) or backlog (BL#) numbers — each number is permanent, even after completion. Always increment to the next unused number.
## Container maintenance — every release must audit the container product surface (14 Dockerfiles in `docker/dockerfiles/` + the Helm chart in `charts/datawatch/`) and decide per-image whether a rebuild/retag is needed. Daemon-behavior changes require rebuilding `parent-full`. Agent/validator image changes require rebuilding the relevant `agent-*` or `validator` image. Helm chart changes require bumping `Chart.yaml` `version` (chart SemVer) AND `appVersion` (datawatch tag). Document the image-delta per release in the release notes under a `## Container images` section. No silent image drift allowed.
## Versioning — sprint releases followed the 3.x track (3.5.0 → 3.6.0 → 3.7.0 → 3.10.0 → 3.11.0), then S8 bumped to **v4.0.0** per operator directive 2026-04-20. Major-version bumps are operator-triggered; do not jump to 5.x without an explicit "this is a major release" instruction. v4.0.0 ships with cumulative release notes covering v3.0 → v4.0: [`RELEASE-NOTES-v4.0.0.md`](RELEASE-NOTES-v4.0.0.md).

## Unclassified

_(cleared in v4.0.1; see below)_

- ✅ Directory selector "create folder" — shipped in v4.0.1 (`POST /api/files` with `{path, name}` body; root-path clamp enforced; UI affordance in the picker).
- ✅ Aperant integration review — **skipped** per 2026-04-20 research: AGPL-3.0 license (incompatible with datawatch distribution), Electron desktop app with no headless API, and it already sits on top of the same claude-code layer datawatch uses. Borrowing worktree-isolation + self-QA ideas into BL24 roadmap as prior art alongside nightwire, but no integration.

---

## Open Bugs

_(none open)_

> B22 fixed in v2.4.3; B23, B24 fixed in v2.4.4; B25 fixed in v2.4.5; B31 fixed in v3.0.1; B30 fixed in v3.1.0 — see Completed section

## Open Features

_(none active)_

## Frozen Features

| # | Description | Status | Notes |
|---|-------------|--------|-------|
| F7  | libsignal — replace signal-cli with native Go | 🧊 frozen 2026-04-20 | Signal-cli is working and stable; 3–6 mo rewrite deferred until there's a concrete operational need. Plan kept at [2026-03-29-libsignal.md](2026-03-29-libsignal.md). |

---

## Backlog — Sprint Plan

**v4.0.0 shipped 2026-04-20. Every planned S1–S8 backlog item is now landed.** The backlog remaining is operational tail (BL85 RTK auto-update, BL166 helm-tool re-add), long-running / frozen items, and v4.0.x follow-ups (web UI cards, BL103-per-guardrail wiring, etc.).

### Shipped timeline (v3.0.0 → v4.0.0)

| Release | Sprint | Items | Release notes |
|---------|--------|-------|---------------|
| v3.0.0 | F10 landing | 25 items (F10 platform, validator, peer broker, orchestrator bridge, mobile surface, memory federation) | [notes](RELEASE-NOTES-v3.0.0.md) |
| v3.1.0 | Test infra + B30 | 3 items (BL89/90/91, B30 fix) | [notes](RELEASE-NOTES-v3.1.0.md) |
| v3.2.0 | Intelligence core | 2 items (BL28 gates, BL39 cycle detection) | [notes](RELEASE-NOTES-v3.2.0.md) |
| v3.3.0 | Observability | 3 items (BL10/11/12) | [notes](RELEASE-NOTES-v3.3.0.md) |
| v3.4.0 / v3.4.1 | Operations + Windows fix | 4 items (BL17/22/37/87) + windows cross-build | [notes](RELEASE-NOTES-v3.4.0.md) |
| v3.5.0 | S1 — quick wins + UI | 5 items (BL1/34/35/41 + F14) | [notes](RELEASE-NOTES-v3.5.0.md) |
| v3.6.0 | S2 — sessions productivity | 6 items (BL5/26/27/29/30/40) | [notes](RELEASE-NOTES-v3.6.0.md) |
| v3.7.0 / v3.7.1 | S3 — cost + audit | 3 items (BL6/86/9) + cost-rates hotfix | [notes](RELEASE-NOTES-v3.7.0.md) |
| v3.7.2 | Sx — parity backfill | 20 MCP tools + 9 CLI commands (v3.5–v3.7 endpoints) | CHANGELOG |
| v3.7.3 | Sx2 — comm + mobile parity | comm router + mobile surface doc | CHANGELOG |
| v3.8.0 | S4 — messaging + UI | 4 items (BL15/31/42/69) | CHANGELOG |
| v3.9.0 | S5 — backends + chat UI | 4 items (BL20/78/79/72) | CHANGELOG |
| v3.10.0 | S6 — autonomous | 2 items (BL24/BL25) | [design](2026-04-20-bl24-autonomous-decomposition.md) · [usage](../api/autonomous.md) |
| v3.11.0 | S7 — plugin framework | 1 item (BL33) | [design](2026-04-20-bl33-plugin-framework.md) · [usage](../api/plugins.md) |
| **v4.0.0** | **S8 — PRD-DAG orchestrator** | **1 item (BL117) + cumulative release notes** | [design](2026-04-20-bl117-prd-dag-orchestrator.md) · [usage](../api/orchestrator.md) · [v3.0→v4.0 cumulative](RELEASE-NOTES-v4.0.0.md) |

Frozen / dropped: F13/BL19 (dropped), BL38 (dropped), BL45 (frozen), BL7 + BL8 (multi-user — frozen). F7 (libsignal) stays open as long-running.

### v4.0.1 — shipped 2026-04-20 (follow-up patch)

Every item flagged as a v4.0.x follow-up in v4.0.0 landed in v4.0.1, plus BL85, BL166, the directory-picker ergonomic, Aperant review, and the F7 freeze:

| Item | Status |
|---|---|
| Web UI Settings cards for autonomous / plugins / orchestrator | ✅ shipped — 14 new fields under General tab (7 autonomous, 3 plugins, 4 orchestrator) |
| BL117 real GuardrailFn (per-guardrail system prompt via `/api/ask`) | ✅ shipped — replaces the v1 stub; unparseable/unreachable → `warn`, doesn't halt the graph |
| Autonomous executor → `session.Manager.Start` wiring | ✅ shipped — `SpawnFn` loopback to `/api/sessions/start`, `VerifyFn` via `/api/ask`, fires async from `POST .../run` |
| Plugin hot-reload via fsnotify | ✅ shipped — `Registry.Watch(ctx)`, 500 ms debounce, wired at startup when `plugins.enabled` |
| `internal/server/web/openapi.yaml` resync | ✅ shipped — regenerated from `docs/api/openapi.yaml` |
| **BL85** — RTK auto-update REST surface | ✅ shipped — `GET /api/rtk/version`, `POST /api/rtk/check`, `POST /api/rtk/update`; background checker was already wired |
| **BL166** — tools-ops helm re-add | ✅ shipped — get.helm.sh reachable; installed from tarball with TARGETARCH |
| Directory-picker "create folder" | ✅ shipped — `POST /api/files` with `{path, name}`; root-path clamp enforced |
| Aperant integration review | ✅ skipped — AGPL-3.0 + Electron desktop app; sits on same claude-code layer; no headless API. Skip per 2026-04-20 research. |
| F7 libsignal | 🧊 frozen — deferred until a concrete need surfaces |

---

### Sprint S1 — Quick wins + UI diff → v3.5.0 — **shipped**

Five low-to-medium-risk items shipped in v3.5.0.

| ID | Item | Status |
|----|------|--------|
| BL1  | IPv6 listener support               | ✅ shipped — IPv6-safe `joinHostPort` at every bind site; `[::]:port` enables dual-stack |
| BL34 | Read-only ask mode                  | ✅ shipped — `POST /api/ask` (Ollama + OpenWebUI backends, no session, no tmux) |
| BL35 | Project summary command             | ✅ shipped — `GET /api/project/summary?dir=` (git status + commits + per-project session stats) |
| BL41 | Effort levels per task              | ✅ shipped — `Session.Effort` (quick/normal/thorough); REST + config + reload + UI parity |
| F14  | Live cell DOM diffing               | ✅ shipped — `tryUpdateSessionsInPlace()` per-card diff before falling back to full render |

### Sprint S2 — Sessions productivity → v3.6.0 — **shipped**

Six items shipped in v3.6.0.

| ID | Item | Status |
|----|------|--------|
| BL5  | Session templates                   | ✅ shipped — `/api/templates` CRUD + `template:` start field |
| BL26 | Recurring schedules                 | ✅ shipped — `recur_every_seconds` + `recur_until` on ScheduledCommand |
| BL27 | Project management                  | ✅ shipped — `/api/projects` CRUD + `project:` start field |
| BL29 | Git checkpoints + rollback          | ✅ shipped — `datawatch-pre/post-{id}` tags + `POST /api/sessions/{id}/rollback` |
| BL30 | Rate-limit cooldown                 | ✅ shipped — `/api/cooldown` (G/P/D) + `session.rate_limit_global_pause` opt-in |
| BL40 | Stale task recovery                 | ✅ shipped — `/api/sessions/stale` + `session.stale_timeout_seconds` |

### Sprint S3 — Cost + observability tail → v3.7.0 — **shipped**

Three items shipped in v3.7.0.

| ID | Item | Status |
|----|------|--------|
| BL6  | Cost tracking                       | ✅ shipped — `Session.tokens_in/out/est_cost_usd` + `/api/cost` + `/api/cost/usage` + per-backend rate table |
| BL86 | Remote GPU/system stats agent       | ✅ shipped — `cmd/datawatch-agent/` (linux-amd64/arm64) — `GET /stats` returns GPU+CPU+memory+disk JSON |
| BL9  | Audit log                           | ✅ shipped — append-only JSONL at `<data_dir>/audit.log` + `GET /api/audit` with filters |

### Sprint Sx — Parity backfill → v3.7.2 — **shipped**

**Audit finding 2026-04-20.** Endpoints shipped in v3.5.0–v3.7.0
had REST + YAML surfaces but were missing MCP / CLI parity, plus
end-to-end functional testing through a running daemon. v3.7.2
addresses the gap:

- **20 MCP tools** in `internal/mcp/sx_parity.go` (REST loopback proxies)
- **9 CLI subcommands** in `cmd/datawatch/cli_sx_parity.go`
- **Functional smoke** verified against a live daemon on port 18080;
  every endpoint returns valid JSON, POST/DELETE round-trips persist,
  cost-rate override applied to live `Manager` correctly.

**Sx2 → v3.7.3 (shipped 2026-04-20):**
- Comm router commands `cost`, `stale`, `audit`,
  `cooldown` (status/set/clear), and a generic `rest <METHOD> <PATH>
  [json]` passthrough that reaches every other Sx endpoint from chat.
- Mobile API surface documented at `docs/api/mobile-surface.md` —
  inventory of every v3.5–v3.7 endpoint plus use-case mapping for
  the `datawatch-app` paired client.

Full parity (REST + YAML + MCP + CLI + comm + mobile + web) for
v3.5–v3.7 is now achieved. S4 can start clean.

| Endpoint | Sprint shipped | Gaps |
|---|---|---|
| `/api/ask` (BL34) | S1 | MCP, comm, CLI |
| `/api/project/summary` (BL35) | S1 | MCP, comm, CLI |
| `/api/templates` (BL5) | S2 | MCP, comm, CLI, UI |
| `/api/projects` (BL27) | S2 | MCP, comm, CLI, UI |
| Recurring schedule fields (BL26) | S2 | MCP/comm/CLI for setting `recur_every_seconds` |
| `/api/sessions/{id}/rollback` (BL29) | S2 | MCP, comm, CLI |
| `/api/cooldown` (BL30) | S2 | MCP, comm, CLI |
| `/api/sessions/stale` (BL40) | S2 | MCP, comm, CLI |
| `/api/cost`, `/api/cost/usage`, `/api/cost/rates` (BL6) | S3 / v3.7.1 | MCP, comm, CLI |
| `/api/audit` (BL9) | S3 | MCP, comm, CLI |
| `datawatch-agent` (BL86) | S3 | parent integration (config, polling adapter, dashboard surface) |

Plus: **functional testing** for each — start a daemon, exercise the
endpoint via every channel, confirm round-trip works, then teardown.

This sprint MUST complete before S4 starts so we don't compound the
gap. Estimate ~2-3 days.

### Sprint S4 — Messaging + UI polish → v3.8.0 — **shipped**

| ID | Item | Status |
|----|------|--------|
| BL15 | Rich previews in alerts             | ✅ shipped — `messaging.FormatAlert` (Telegram MD escaping, Signal mono, Slack/Discord passthrough) + opt-in `session.alerts_rich_format` |
| BL31 | Device targeting (`@device` routing) | ✅ shipped — `session.device_aliases` config + `/api/device-aliases` CRUD |
| BL69 | Splash screen — custom logo         | ✅ shipped — `session.splash_logo_path/tagline` + `GET /api/splash/{logo,info}` |
| BL42 | Quick-response assistant            | ✅ shipped — `POST /api/assist` with dedicated assistant_* config |

Full parity for each: REST + YAML + MCP tool + CLI subcommand + comm + mobile (REST is mobile-friendly).

### Sprint S5 — Backends + chat UI → v3.9.0 — **shipped**

| ID | Item | Status |
|----|------|--------|
| BL20 | Backend auto-selection (routing rules) | ✅ shipped — `session.routing_rules` + `/api/routing-rules` + `/api/routing-rules/test` + MCP/CLI parity |
| BL78 | Chat UI: Gemini chat mode           | ✅ documented (config recipe at `docs/api/chat-mode-backends.md`) — `gemini.output_mode: chat` |
| BL79 | Chat UI: Aider/Goose chat mode      | ✅ documented — same `output_mode: chat` recipe for Aider + Goose |
| BL72 | OpenCode memory hooks               | ✅ documented — opencode chat-mode reuses BL65 memory hook path |

### Sprint S6 — Intelligence → v3.10.0 ✅ SHIPPED 2026-04-20

Design doc: [`2026-04-20-bl24-autonomous-decomposition.md`](2026-04-20-bl24-autonomous-decomposition.md) — maps every nightwire component to a datawatch primitive. Operator doc: [`../api/autonomous.md`](../api/autonomous.md).

| ID | Item | Status |
|----|------|--------|
| BL24 | Autonomous task decomposition       | ✅ shipped — `internal/autonomous/` package (models, JSONL store, decompose prompt+parser, security scanner, manager, executor with topo-sort + auto-fix retry), REST `/api/autonomous/*` + 10 MCP tools + `datawatch autonomous` CLI + comm via `rest` passthrough + `autonomous.*` YAML |
| BL25 | Independent verification            | ✅ shipped — `VerifyFn` indirection in executor; BL103 validator agent wiring deferred to v3.10.x patch |

### Sprint S7 — Extensibility → v3.11.0 ✅ SHIPPED 2026-04-20

Design doc: [`2026-04-20-bl33-plugin-framework.md`](2026-04-20-bl33-plugin-framework.md) — rejects `.so` / Lua; selects subprocess + JSON-RPC over stdio. Operator doc: [`../api/plugins.md`](../api/plugins.md).

| ID | Item | Status |
|----|------|--------|
| BL33 | Plugin framework                    | ✅ shipped — `internal/plugins/` subprocess driver, manifest discovery, 4 hooks, fan-out chaining, timeout/error stats; REST `/api/plugins/*` + 6 MCP tools + `datawatch plugins` CLI + comm via `rest` + `plugins.*` YAML. Disabled by default. |

### Sprint S8 — PRD-DAG orchestrator → **v4.0.0** ✅ SHIPPED 2026-04-20

Design doc: [`2026-04-20-bl117-prd-dag-orchestrator.md`](2026-04-20-bl117-prd-dag-orchestrator.md). Operator doc: [`../api/orchestrator.md`](../api/orchestrator.md). **Cumulative release notes** covering every shipped item since v3.0.0: [`RELEASE-NOTES-v4.0.0.md`](RELEASE-NOTES-v4.0.0.md).

| ID | Item | Status |
|----|------|--------|
| BL117 | PRD-driven DAG orchestrator + guardrail sub-agents | ✅ shipped — `internal/orchestrator/` package (Graph/Node/Verdict, JSONL store, Runner with Kahn topo-sort and verdict aggregation). 4 guardrail types (rules/security/release-readiness/docs-diagrams-architecture) with v1 stub GuardrailFn; plugin `on_guardrail` hook available for real guardrails. REST `/api/orchestrator/*` + 9 MCP tools + `datawatch orchestrator` CLI + comm via `rest` + `orchestrator.*` YAML. |

---

### Sprint summary

| Sprint | Items | Releases | Effort | Status |
|--------|-------|----------|--------|--------|
| S1 | 5 (4 quick wins + F14 DOM diff) | v3.5.0  | 1 day    | ✅ shipped |
| S2 | 6 sessions/productivity         | v3.6.0  | 1 week   | ✅ shipped |
| S3 | 3 cost + obs tail (+ new binary)| v3.7.0  | 1 week   | ✅ shipped (REST/YAML only — Sx gates full parity) |
| Sx | Parity backfill for v3.5–v3.7   | v3.7.2  | 2-3 days | ✅ shipped — MCP (20 tools) + CLI (9 commands) + functional smoke verified |
| Sx2| Comm + mobile parity            | v3.7.3  | 0.5 day  | ✅ shipped — router commands + mobile API surface doc |
| S4 | 4 messaging + UI polish         | v3.8.0  | 3 days   | ✅ shipped |
| S5 | 4 backends + chat UI            | v3.9.0  | 3 days   | ✅ shipped |
| S6 | 2 intelligence (BL24 + BL25)    | v3.10.0 | 2 weeks  | ✅ shipped — [design](2026-04-20-bl24-autonomous-decomposition.md) · [usage](../api/autonomous.md) |
| S7 | 1 plugin framework (BL33)       | v3.11.0 | 3 days   | ✅ shipped — [design](2026-04-20-bl33-plugin-framework.md) · [usage](../api/plugins.md) |
| S8 | 1 PRD-DAG orchestrator (BL117)  | **v4.0.0** | 2-3 weeks | ✅ shipped — [design](2026-04-20-bl117-prd-dag-orchestrator.md) · [usage](../api/orchestrator.md) · [v3.0→v4.0 release notes](RELEASE-NOTES-v4.0.0.md) |

---

### Per-category snapshot (cross-reference)

Quick reference. The sprint plan above is the source of truth — these tables only group items by domain so plans are easy to find.

| Category | Active items | Sprint(s) |
|---|---|---|
| **Sessions** | BL117 future (all S2/S3 sessions items shipped) | S8 |
| **Intelligence** | _(complete — BL24, BL25 shipped in v3.10.0)_ | — |
| **Observability** | _(complete — all shipped)_ | — |
| **Collaboration** | _(BL9 shipped; BL7 + BL8 frozen)_ | — |
| **Messaging** | _(complete — BL15, BL31 shipped)_ | — |
| **Backends & UI** | _(complete — BL20 shipped, BL78/BL79 documented)_ | — |
| **Memory & Security** | _(complete — BL72 documented)_ | — |
| **Extensibility** | _(complete — BL33 shipped in v3.11.0)_ | — |

Per-item plans live in [`2026-04-11-backlog-plans.md`](2026-04-11-backlog-plans.md). Quick-effort items are flagged with ⚡ in the sprint tables above.

> **Already shipped:** Operations (v3.4.0: BL17/22/37/87), Observability core (v3.3.0: BL10/11/12), Intelligence core (v3.2.0: BL28/39), Testing infrastructure (v3.1.0: BL89/90/91), and 25 items in v3.0.0 (BL92–BL116). See per-version release notes for the full shipped list.

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
| B31 | In-app upgrade reports success but doesn't replace binary — asset name mismatch between updater and release (pre-existing since v2.x; surfaced on v3.0.0 upgrade) | v3.0.1 |
| B30 | Scheduled command lands in prompt but requires a 2nd Enter to activate (claude-code TUI phase-4 race) | v3.1.0 |
| B32 | Tmux/scheduled command executes with a blank line, operator has to press Enter again to submit — trailing `\n` in the payload was interpreted by TUIs as multi-line input so the explicit Enter just added another blank | v4.0.2 |
| B33 | PWA "Input Required" yellow card stays visible after sending a reply; only disappears on session reconnect — added auto-dismiss on send + manual X button; re-appears on next distinct prompt | v4.0.2 |

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
| F4  | Channel parity (threaded conversations)         | v1.0.2 |
| F8  | Health check endpoint                           | v1.0.2 |
| F9  | Fallback chains                                 | v1.0.2 |
| F11 | Voice input (Whisper)                           | v1.1.0 |
| F12 | Prometheus metrics                              | v1.0.2 |
| F15 | Session chaining — pipeline DAG executor        | v2.4.0 |
| F10 | Ephemeral container-spawned agents              | v3.0.0 |
| F17 | Mobile device registry (`POST /api/devices/register`) — closes GH [#1](https://github.com/dmz006/datawatch/issues/1) | v3.0.0 |
| F18 | Voice transcription (`POST /api/voice/transcribe`) — closes GH [#2](https://github.com/dmz006/datawatch/issues/2) | v3.0.0 |
| F19 | Federation fan-out (`GET /api/federation/sessions`) — closes GH [#3](https://github.com/dmz006/datawatch/issues/3) | v3.0.0 |
| BL89 | Mock session manager for unit tests (TmuxAPI interface + FakeTmux) | v3.1.0 |
| BL90 | httptest server for API endpoint tests | v3.1.0 |
| BL91 | MCP tool handler tests (direct handler invocation) | v3.1.0 |
| BL28 | Quality gates (test baseline + regression detection wired into Executor) | v3.2.0 |
| BL39 | Circular dependency detection (NewPipeline rejects cycles, DFS path output) | v3.2.0 |
| BL10 | Session diffing — git shortstat captured into Session.DiffSummary on completion | v3.3.0 |
| BL11 | Anomaly detection — stuck-loop / long-input-wait / duration-outlier helpers | v3.3.0 |
| BL12 | Historical analytics — `GET /api/analytics?range=Nd` day-bucket aggregation | v3.3.0 |
| BL17 | Hot config reload — SIGHUP + `POST /api/reload` re-applies hot-reloadable subset | v3.4.0 |
| BL22 | RTK auto-install — `datawatch setup rtk` downloads platform binary into ~/.local/bin | v3.4.0 |
| BL37 | System diagnostics — `GET /api/diagnose` health checks (tmux, sessions, disk, goroutines) | v3.4.0 |
| BL87 | `datawatch config edit` — visudo-style safe editor with validate-on-save loop | v3.4.0 |
| BL1  | IPv6 listener support — every bind via `net.JoinHostPort`; `[::]:port` dual-stack | v3.5.0 |
| BL34 | Read-only ask mode — `POST /api/ask` (Ollama + OpenWebUI), no session/tmux | v3.5.0 |
| BL35 | Project summary — `GET /api/project/summary?dir=` git + per-project session stats | v3.5.0 |
| BL41 | Effort levels per task — `Session.Effort` (quick/normal/thorough), full config parity | v3.5.0 |
| F14  | Live cell DOM diffing — `tryUpdateSessionsInPlace()` per-card diff path | v3.5.0 |
| BL5  | Session templates — `/api/templates` CRUD + `template:` start field | v3.6.0 |
| BL26 | Recurring schedules — `recur_every_seconds` + `recur_until` on ScheduledCommand | v3.6.0 |
| BL27 | Project management — `/api/projects` CRUD + `project:` start field | v3.6.0 |
| BL29 | Git checkpoints + rollback — pre/post tags + `POST /api/sessions/{id}/rollback` | v3.6.0 |
| BL30 | Rate-limit cooldown — `/api/cooldown` + opt-in `rate_limit_global_pause` | v3.6.0 |
| BL40 | Stale task recovery — `/api/sessions/stale` + configurable threshold | v3.6.0 |
| BL6  | Cost tracking — Session.tokens_in/out/est_cost_usd + `/api/cost` + per-backend rates | v3.7.0 |
| BL86 | Remote GPU/system stats agent — `cmd/datawatch-agent/` standalone binary | v3.7.0 |
| BL9  | Operator audit log — append-only JSONL + `/api/audit` filtered query | v3.7.0 |

### Promoted to Features

Per the no-reuse rule, the original BL numbers stay reserved. Status reflects the current state of the parent F-feature.

| BL  | Promoted to | Status |
|-----|-------------|--------|
| BL2 | F14 (Live cell DOM diffing) | Open (F14 still in Open Features) |
| BL3 | F10 (Ephemeral container-spawned agents) | Shipped in v3.0.0 |
| BL4 | F15 (Session chaining — pipeline DAG executor) | Shipped in v2.4.0 |

### Dropped / Frozen

Numbers stay reserved (per the rule above) and are never reused.

| ID | Decision | Date | Reason |
|----|----------|------|--------|
| F13 | Dropped | 2026-04-19 | Copilot/Cline/Windsurf backends — operator decided not to support |
| BL19 | Dropped (with F13) | 2026-04-19 | Original BL that was promoted to F13 |
| BL38 | Dropped | 2026-04-19 | Message content privacy — operator decided not to pursue |
| BL45 | Frozen | 2026-04-19 | ChromaDB/Pinecone/Weaviate backends — operator unsure if needed; revisit if pgvector hits a limit |
| BL7  | Frozen | 2026-04-19 | Multi-user access control — single-operator use stays the supported model for now; no work scheduled |
| BL8  | Frozen | 2026-04-19 | Session sharing (time-limited links) — depends on BL7's auth model; frozen with BL7 |


See [testing.md](../testing.md) for test results and pre-release checklists.
