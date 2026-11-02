# Burn guard: plan

**Date:** 2026-11-02
**Version:** set at merge time. The merge helper (`scripts/merge-branches-bump-version.sh`) sets the next version above main; this plan names no number and adds no versioned changelog heading.
**Design record:** the Friday-night Claude burn entry in `docs/plans/README.md` (internal bug table) holds the measurement and the operator's words.
**Parity issue:** `docs/plans/parity-issues/burn-guard.md`, filed as dmz006/datawatch-app#332
**How-to:** `docs/howto/burn-guard.md`

## Scope

- `internal/burnguard/` (token reader, points fit, projection, ladder state machine)
- `internal/config/burn_guard.go`, `internal/server/burn_guard*.go`, `internal/server/api.go` (config GET), `internal/autonomous/` (effort ceiling, Claude retry limit)
- `internal/mcp/burn_guard_tools.go`, `internal/mcp/effort_ceiling_tools.go`, `internal/router/`, `cmd/datawatch/` (CLI, wiring)
- `internal/server/web/app.js`, locales (en, de, es, fr, ja), `openapi.yaml` (both copies)
- `docs/`, `scripts/release-smoke.sh`, `scripts/test-stories/`

## What this delivers

A guard that reads the Claude session transcripts, prices the tokens in points of the weekly allowance, projects them to the reset and, when the projection runs over the pace line plus a margin, steps down a ladder of admission changes (lower the Claude inflight cap, route new Claude work to `pi-prompt`, hold new Claude work) and steps back when the burn is under the line. It never ends a running session. It also adds a per-task-kind effort ceiling and a Claude retry limit.

## Phases and status

| # | Phase | Status |
|---|---|---|
| 1 | Burn guard settings (`usage.burn_guard`) with normalisation | Done |
| 2 | Token reader, points fit and projection | Done |
| 3 | Ladder state machine | Done |
| 4 | Hourly guard and its actions wired into the daemon (admission, alerts, questions, metrics, stats) | Done |
| 5 | Effort ceiling and Claude retry limit | Done |
| 6 | REST, MCP, CLI and comm channel surfaces | Done |
| 7 | PWA card, locales, Settings section and Monitor stat card | Done |
| 8 | Docs, openapi, release smoke section, browser story TS-843, app parity issue | Done |
| 9 | Live daemon validation on a sandbox daemon | Done, see the testing tracker section "Burn guard surfaces" |

Shipped in: the release that carries this change (the merge helper records the number in the changelog).

## Parity surface

| Surface | Included | Reason or where |
|---|---|---|
| REST | Yes | `/api/usage/burn-guard`, `/run`, `/config`; `usage.burn_guard` in `GET /api/config`; `POST /api/automations/{id}/set_effort_ceiling` |
| MCP | Yes | `burn_guard_*` tools; `automation_set_effort_ceiling`; `autonomous_prd_set_task_effort_ceiling` |
| CLI | Yes | `datawatch usage burn-guard`; `datawatch automation set-effort-ceiling` |
| comm channel | Yes | `usage burn-guard`; `configure usage.burn_guard.<key>=<value>`; `automation set-effort-ceiling` |
| YAML/config | Yes | `usage.burn_guard` in `docs/config-reference.yaml` |
| PWA | Yes | usage calculator card, Settings section, Monitor stat card |
| Android | Yes | filed as a parity issue on the app repository (see `docs/plans/parity-issues/burn-guard.md`) |
| iPhone | Yes, tracked on the same issue | no separate iOS release exists to file against |

Excluded: the effort ceiling is not a `PUT /api/config` key. It is per Automaton and per task by design, so it has its own routes and tools.

## Phase completion checklist (every phase)

- [x] Functionality implemented.
- [x] Unit tests added and passing.
- [x] Live test on a sandbox daemon (release smoke section and the testing tracker row).
- [x] CHANGELOG entry (under Unreleased).
- [x] `docs/config-reference.yaml`, `docs/operations.md`, `docs/howto/burn-guard.md`, `docs/cursor-mcp.md`, both `openapi.yaml` copies.
- [x] `docs/testing-tracker.md` section.
- [x] Locales in five languages.
- [x] App parity issue filed: dmz006/datawatch-app#332.
