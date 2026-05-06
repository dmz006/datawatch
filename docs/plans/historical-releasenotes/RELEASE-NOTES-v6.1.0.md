# Release Notes — v6.1.0 (2026-05-03)

v6.1.0 is a minor release collecting the v6.0.6–v6.0.9 infrastructure hygiene patch series. All changes were delivered as patch releases and are aggregated here for version tracking.

## What changed since v6.0.0

### BL228 — Scheduled commands (v6.0.6)

Operators can now schedule a comm command to fire at a future time or on the next session input prompt.

- `schedule add <session> <delay> <command>` — schedule a command with a duration offset or `prompt` keyword
- `schedule list` — list pending scheduled commands for all sessions
- `schedule cancel <id>` — cancel a pending scheduled command
- REST: `POST /api/schedule`, `GET /api/schedule`, `DELETE /api/schedule/{id}`
- MCP: `schedule_add`, `schedule_list`, `schedule_cancel` tools
- CLI: `datawatch schedule add|list|cancel`
- Comm: `schedule add|list|cancel`
- PWA: Schedule panel in session detail view

### BL218 — Channel session-start hygiene (v6.0.7)

Three hardening improvements to the channel bridge lifecycle:

- `EnsureExtracted` now uses SHA-256 content hash instead of file size to detect stale/corrupt `channel.js`. The old size-only check missed bit-flip corruption when sizes matched.
- New `SweepUserScopeMCPConfig` rewrites the `datawatch` entry in `~/.mcp.json` on every pre-launch when it is stale (e.g. still pointing at `node + channel.js` after Go bridge installation).
- `onPreLaunch` now logs `[channel] pre-launch: wiring <go|js> bridge for session <id>` before MCP registration for easier debugging.

### BL219 — LLM tooling artifact lifecycle (v6.0.8)

Per-backend artifact hygiene across all 6 surfaces:

- **Backend artifact registry** mapping each LLM backend to its known project-dir patterns (`claude-code`, `opencode`, `aider`, `goose`, `gemini`).
- **`EnsureIgnored(projectDir, backend)`** — appends artifact patterns to `.gitignore` idempotently on every session start.
- **`CleanupArtifacts(projectDir, backend)`** — removes ephemeral backend files on session end.
- Three YAML config fields: `session.gitignore_check_on_start`, `session.gitignore_artifacts`, `session.cleanup_artifacts_on_end`.
- Full 6-surface parity: YAML + REST + MCP + CLI + Comm + PWA.

### BL226 — Service-level alert stream (v6.0.9)

Internal subsystem failures now surface as structured alerts:

- `Source` field added to `Alert` struct; `source:"system"` for daemon-generated alerts.
- `alerts.EmitSystem(level, title, body)` global — any internal package emits without DI via `SetGlobal(store)`.
- Instrumented: pipeline task failure, executor panic, eBPF probe load failure, plugin `Fanout` error.
- REST `?source=system` filter, MCP `source` param, CLI `--system` flag, Comm `alerts system`.
- PWA: dedicated **System** tab in Alerts view with red unread badge.

## Upgrade notes

Standard patch upgrade: `datawatch update && datawatch restart`. No config migration needed. New YAML fields have safe defaults and are all optional.

## Test coverage

- 1644 unit tests pass
- Smoke test suite (91 checks) pass
