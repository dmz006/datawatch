# Agent tracing: plan

**Date:** 2026-11-04
**Version:** set at merge time. The merge helper (`scripts/merge-branches-bump-version.sh`) sets the next version above main; this plan names no number and adds no versioned changelog heading.
**Parity issue:** `docs/plans/parity-issues/agent-tracing.md` (to be filed on datawatch-app by the merge step)
**How-to:** `docs/howto/agent-tracing.md`

## Scope

- `internal/trace/` (event, settings, redaction, append-only store and index, roll-ups), `internal/trace/adapters/` (claude-code, pi, opencode, goose, aider, gemini transcript readers), `internal/trace/observe/`
- `internal/session/trace_ingest.go`, `internal/server/trace_*.go`, `internal/server/tracing_config.go`, `internal/llm/backends/{ollama,openwebui,shell}/record.go`, `internal/pilog/turns.go`
- `internal/mcp/trace_tools.go`, `internal/router/trace_comm.go`, `cmd/datawatch/cli_trace.go`, `cmd/datawatch/trace_stats_wiring.go`
- `internal/server/web/app.js`, locales (en, de, es, fr, ja), both `openapi.yaml` copies
- `docs/`, `scripts/release-smoke.sh`, `scripts/test-stories/`

## What this delivers

A normalized record of what each agent did, from every backend: tool calls and results, model requests, state changes, capability checks, permission decisions, loop warnings and errors. It is read as a timeline for one session or an Automaton and as roll-ups by session, task, story, Automaton, agent or backend, with a coverage level per backend (`full`, `partial`, `events-only`, `unknown`) so a gap is visible. It records and reads; it never changes how an agent runs. Redaction (secrets, home paths, argument cap) runs before anything is stored.

## Phases and status

| # | Phase | Status |
|---|---|---|
| 1 | Event model and `tracing.*` settings with validation | Done |
| 2 | Redaction before storage | Done |
| 3 | Append-only store, index, retention prune | Done |
| 4 | Roll-ups feeding the usage and cost views | Done |
| 5 | Transcript adapters (claude-code, pi, opencode, goose, aider, gemini) and daemon-observed adapters (ollama, openwebui, shell) | Done |
| 6 | Daemon capture, capability and permission events, live timeline | Done |
| 7 | REST routes, MCP tools, CLI verbs and comm verbs | Done |
| 8 | PWA Trace card, Settings group, five locales | Done |
| 9 | Observability: store stats, `GET /api/trace/stats`, system stats fields, Prometheus series, Monitor Tracing card | Done |
| 10 | Docs, openapi (both copies), release smoke section, browser story TS-848 | Done |
| 11 | Live run on a sandbox daemon | Not done here, see "Decisions" |

Shipped in: the release that carries this change (the merge helper records the number in the changelog).

## Parity surface

| Surface | Included | Reason or where |
|---|---|---|
| REST | Yes | `GET /api/trace/session/{id}`, `/api/trace/automaton/{id}`, `/api/trace/rollup`, `/api/trace/coverage`, `/api/trace/stats`; `tracing` in `GET`/`PUT /api/config` |
| MCP | Yes | `trace_session`, `trace_automaton`, `trace_rollup`, `trace_coverage`, `trace_stats` (the observability read of `/api/trace/stats`, also on `get_stats`) |
| CLI | Yes | `datawatch trace session|automaton|rollup|coverage` |
| comm channel | Yes | `trace session|automaton|rollup|coverage`; `configure tracing.<key>=<value>` |
| YAML/config | Yes | the `tracing` block in `docs/config-reference.yaml` |
| PWA | Yes | Trace sub-tab under the session Status tab and a Trace tab in the Automaton view, Settings > Tracing, the Monitor Tracing card |
| Android | Yes | filed as a parity issue on the app repository (see `docs/plans/parity-issues/agent-tracing.md`) |
| iPhone | Yes, tracked on the same issue | no separate iOS release exists to file against |

Excluded: OpenTelemetry export is a follow-up. Trace events are not written to the audit trail, see "Decisions".

## Decisions

- **Audit logging (B16).** `audit-tests: N/A`. The capability and permission events in the trace are a read-only, redacted *copy* for the timeline. Each denial is already audited by its own source (the federation capability gate and config writes keep their audit entries, JSON-lines with the CEF mirror in `internal/audit`), and the trace store is not a security audit trail: it is pruned by `tracing.retention_days`, can be switched off and records at summary level by default. Adding a second CEF writer for a copy would duplicate the SIEM feed. If the operator wants trace events forwarded to a SIEM, that is a separate item that would add `FormatCEFLine` and an escape test for the trace kinds.
- **Live smoke.** The automaton that built this change runs under a no-daemon rule (no daemon or real LLM session is started), which conflicts with the Phase Completion Checklist item "live test on a sandbox daemon". The conflict is recorded here rather than worked around: the release smoke section "Agent tracing" and story TS-848 are written and syntax-checked, and the unit suites exercise the real handlers, store and wiring. The live run is owed at the merge step on a sandbox daemon: `bash scripts/release-smoke.sh` (section "Agent tracing") and `bash scripts/run-tests.sh --stories=TS-848`.
- **Final gate scope.** The earlier gate covered 11 Go packages and one node file. This revision ran `go test ./...` and the full node suite (`node --test internal/server/web/*.test.js`); counts are in `docs/plans/reports/70c90001-final-gate.md`.

## Reuse audit — what existing primitive does this extend?

The usage and cost roll-ups (`cost_report.go` reads the trace roll-up tokens), the stats collector and Prometheus mirror pattern of the burn guard, the list loader and card shell of the PWA, the `fedCap` capability gate (`CapAnalyticsRead`), `scripts/check-howto-coverage.sh`, and the per-backend transcript readers already used for session summaries. Nothing new was built where one of these fits.

## Skills-awareness

`skills-hook: N/A` — tracing adds no new session, Automaton, agent, comm-channel or plugin path that a skill needs to hook into. It observes the existing ones.

## Phase completion checklist (every phase)

- [x] Functionality implemented.
- [x] Unit tests added and passing.
- [ ] Live test on a sandbox daemon: owed at merge (see Decisions).
- [x] CHANGELOG entry (under Unreleased).
- [x] `docs/config-reference.yaml`, `docs/implementation.md`, `docs/operations.md`, `docs/howto/agent-tracing.md`, `docs/cursor-mcp.md`, `docs/mcp.md`, `docs/api-mcp-mapping.md`, both `openapi.yaml` copies, `README.md`.
- [x] `docs/testing-tracker.md` section and `docs/testing.md` results.
- [x] Locales in five languages; `settings_tracing_group`, `trace_card_title` and `stats_trace` in the locale guard list.
- [ ] App parity issue filed: the draft is `docs/plans/parity-issues/agent-tracing.md`; the merge step files it.
