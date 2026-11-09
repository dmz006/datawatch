# Worker session profiles — implementation plan

**Date:** 2026-11-09
**Version:** v9.112.6 (planning and this change both leave `cmd/datawatch/main.go` and `internal/server/api.go` unchanged)
**Status:** Phases 1–5 Done; Phase 6 Planned (launch-time wiring is the sessions' own start work)

## Scope

Packages and files this plan touches:

- `internal/workerprofile/` — built-in profiles, the resolution rule, the applied-profile record (new).
- `internal/config` and `internal/server/worker_profile_config.go` — the `autonomous.worker_profile*` settings, validation and refusals on set.
- `internal/server/autonomous.go` — `POST /api/autonomous/prds/{id}/set_worker_profile`, which emits the `autonomous_worker_profile_set` audit event (`internal/audit/events.go`, `internal/audit/cef.go`).
- `internal/server/api.go` — the `GET`/`PUT /api/config` keys.
- `internal/mcp/worker_profile_tools.go` — the read-only `worker_profile_status` tool, and the MCP mapping for the per-Automaton setter.
- `internal/router/worker_profile_configure.go` — the comm-channel and CLI setters.
- `internal/channel/worker_mcp_config.go`, `internal/llm/claudecode/` — the per-session MCP config and the launcher application (built, tested; not yet called from a live launch).
- `internal/stats/worker_startup_stats.go`, `internal/stats/collector.go` — the `worker_startup` stats block.
- `internal/server/web/app.js` and `internal/server/web/locales/*.json` — the settings card and the Monitor-tab worker-startup card.
- `scripts/release-smoke.sh` (§96), `scripts/check-howto-coverage.sh`, `docs/howto/worker-session-profiles.md`, `docs/config-reference.yaml`, `docs/operations.md`, `docs/implementation.md`, `CHANGELOG.md`, `README.md`, `docs/datawatch-definitions.md`.

## Phases

1. **Profiles and resolution.** The four built-in profiles (`minimal`, `digest`, `datawatch-tools`, `full`), the per-role and per-kind default table, and the resolution order task → Automaton → project → daemon → kind → role. — Done (`internal/workerprofile/`).
2. **Settings and refusals.** `autonomous.worker_profiles`, `autonomous.worker_profile_defaults`, `autonomous.worker_profile`; validated on set; refused when a user profile reuses a built-in name. — Done (`internal/config`, `internal/server/worker_profile_config.go`).
3. **Read and set on every surface.** REST `GET`/`PUT /api/config`, the per-Automaton route with its audit event, MCP tools, comm-channel and CLI setters, the PWA settings card. — Done (this change adds the route's smoke coverage, the audit event's JSON-lines and CEF tests, and the Monitor-tab card).
4. **Per-session application.** Per-session `worker-mcp.json`, tool allow-list, skills switch, deterministic starting context, "not applied" record. — Built and tested; reached only through an applied profile.
5. **Measurement.** Startup size recorded per attempt; cost report per-profile and per-role tables; `worker_startup` stats block. — Done.
6. **Launch wiring.** An Automaton's worker, verifier and judge launches call the resolution rule and pass the resolved profile to the launcher; the defaults and the daemon-wide name take effect at launch. — Planned. Until it lands, those launches keep the full harness, and the docs say so.

## Parity surface

| Surface | Status | Notes |
|---|---|---|
| REST | Included | `GET`/`PUT /api/config`; `POST /api/autonomous/prds/{id}/set_worker_profile`; `/api/stats` `worker_startup`. |
| MCP | Included | `config_set`, `get_config`, `worker_profile_status`, `autonomous_prd_set_worker_profile`. |
| CLI | Included | Settings through the config commands; per-Automaton set through the autonomous commands. |
| comm channel | Included | Setters and reads routed through `internal/router/worker_profile_configure.go`. |
| YAML/config | Included | `docs/config-reference.yaml` documents the three keys. |
| PWA | Included | Settings card; Monitor-tab worker-startup card. The `datawatch-app` parity-issue draft is under `docs/plans/parity-issues/`. |
| Android | Excluded | No Android change in this change. Reason: the app reads the same REST keys; the parity draft tracks any follow-up. |
| iPhone | Excluded | Same reason as Android. |

## Verification

- `make lint` — 0 issues; all audit scripts pass.
- `go build ./...` — pass.
- `go test` over audit, server, stats, usagecalc, workerprofile, mcp, router, session, llm/claudecode and costreport — pass.
- `node --test internal/server/web/app-worker-profile-settings.test.js` — 7 of 7 pass.
- `bash -n scripts/release-smoke.sh` — pass. The full smoke run was not performed in this change (it starts an isolated daemon); §96 is the new coverage for the worker-profile surface.
