# Automata checks and ship steps: plan

**Date:** 2026-11-02
**Version:** 9.68.0. `Version` in `cmd/datawatch/main.go` and `internal/server/api.go` is bumped together, and the CHANGELOG carries a `v9.68.0` heading. If the merge helper renumbers above main, it updates all of these together.
**Design record:** the proposals index in `docs/plans/README.md`.
**Parity issue:** `docs/plans/parity-issues/checks-and-ship.md`, filed at merge (not filed yet)
**How-to:** `docs/howto/automata-checks-and-ship.md`
**Live check:** `docs/plans/2026-11-02-checks-and-ship-live-check.md` (`scripts/checks-and-ship-live-check.sh`)

## Scope

- `internal/config/autonomous_checks.go`, `internal/config/autonomous_ship.go` (settings, normalisation, validation)
- `internal/autonomous/` (check registry, pre-start verifier, plan rules, after-done checklist, registry check, ship policy, push and tag step, CI watch, status views)
- `internal/git/tag.go` (annotated tag and push)
- `internal/server/checks_ship*.go`, `internal/server/api.go`, `internal/server/web/openapi.yaml` (REST)
- `internal/mcp/checks_ship_tools.go`, `internal/router/checks_ship_comm.go`, `cmd/datawatch/` (MCP, chat, CLI)
- `internal/server/web/app.js` and the five locale bundles (PWA cards and Settings groups)
- `docs/`, `scripts/checks-and-ship-live-check.sh`

## What this delivers

Four automatic checks on an Automaton's work (pre-start verifier, plan rules, after-done checklist, registry check) and three ship steps for a finished release (push, annotated tag, CI watch). Every one has a mode: `off`, `shadow` (record only) or `on`. Findings and outcomes are recorded on the Automaton's triage record and read from REST, MCP, CLI, the chat channel and the PWA.

## Phases and status

| # | Phase | Status |
|---|---|---|
| 1 | Checks settings and the check registry | Done |
| 2 | Pre-start verifier check, plan-rule findings, after-done record, registry check | Done |
| 3 | Ship policy, push and tag step, CI watch of the pushed commit and tag | Done |
| 4 | REST, MCP, CLI and comm channel surfaces | Done |
| 5 | PWA cards, Settings groups and locales | Done |
| 6 | In-process integration test (`TestChecksShipIntegration`) | Done |
| 7 | Live sandbox-daemon check of settings, routes, CLI and served PWA code | Done, 25 of 25; the ship step and the pre-start block are not run on a live daemon (they need a model-driven run) |
| 8 | Docs: config reference, implementation, MCP lists, indexes, how-to, testing tracker section | Done |
| 9 | Version bump, release changelog heading, datawatch-app parity issue | Version 9.68.0 and the changelog heading done; the parity issue is filed at merge (not filed yet) |

## Parity surface

| Surface | Included | Reason or where |
|---|---|---|
| REST | Yes | `GET /api/autonomous/checks`, `GET /api/autonomous/ship`; `autonomous.checks` and `autonomous.ship` in `GET`/`PUT /api/config` |
| MCP | Yes | `checks_status`, `ship_status` |
| CLI | Yes | `datawatch autonomous checks`, `datawatch autonomous ship` |
| comm channel | Yes | `autonomous checks`, `autonomous ship` |
| YAML/config | Yes | `autonomous.checks.*`, `autonomous.ship.*` in `docs/config-reference.yaml` and `docs/implementation.md` |
| PWA | Yes | Automata checks card, Automata ship status card, Settings groups Checks and Ship steps |
| Android | Pending | `docs/plans/parity-issues/checks-and-ship.md`, filed at merge |
| iPhone | Pending, tracked on the same issue | no separate iOS release exists to file against |

## Phase completion checklist

- [x] Functionality implemented.
- [x] Unit tests added and passing (including `internal/config` tests for the new structs).
- [x] Live test on a sandbox daemon (settings, routes, CLI, served PWA code): `docs/plans/2026-11-02-checks-and-ship-live-check.md`, 25 of 25 checks. The earlier integration report (`docs/plans/reports/checks-and-ship-integration.md`) was in-process only, with no daemon, and is not the live evidence.
- [x] CHANGELOG entry under the `v9.68.0` heading.
- [x] `docs/config-reference.yaml`, `docs/implementation.md`, `docs/cursor-mcp.md`, `docs/mcp.md`, `docs/operations.md`, how-to, indexes.
- [x] `docs/testing-tracker.md` section "Automata checks and ship surfaces".
- [x] Locales in five languages.
- [x] Version bump to 9.68.0 and release changelog heading.
- [ ] datawatch-app parity issue filed: at merge.
