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
| — | No open bugs | — | |

## Open Features

| # | Description | Priority | Effort | Notes |
|---|-------------|----------|--------|-------|
| F7 | libsignal — replace signal-cli with native Go | low | 3-6 months | Plan: [libsignal](2026-03-29-libsignal.md) |
| F10 | Container images, Helm chart | medium | 1-2 days | Plan: [backlog-plans](2026-04-01-backlog-plans.md#bl3-container-images-and-helm-chart) |
| F13 | Copilot/Cline/Windsurf backends | low | 1-2hr each | Plan: [backlog-plans](2026-04-01-backlog-plans.md#bl19-copilotclinewindsurf-backends) |
| F14 | Live cell DOM diffing | low | 3-4hr | Plan: [backlog-plans](2026-04-01-backlog-plans.md#bl2-live-cell-dom-diffing) |

---

## Backlog — Remaining Items (38)

All items have plans. Quick wins marked with ⚡.

### Sessions (10)

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
