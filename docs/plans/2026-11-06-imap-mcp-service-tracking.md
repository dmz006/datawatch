# imap-mcp service tracking: plan

**Date:** 2026-11-06
**Status:** In Progress — phases 1–12 Done, phase 13 In Progress, phase 14 Planned
**Automaton:** 784f0001 (tasks a0d00003–a0d00018; this record is a0d0001a)
**Version:** set at merge time. The merge helper (`scripts/merge-branches-bump-version.sh`) sets the next version above main; this plan names no number and adds no versioned changelog heading.
**Parity issue:** `docs/plans/parity-issues/imap-mcp-service-tracking.md` (draft owed; to be filed on datawatch-app by the merge step)
**Design basis:** `docs/plans/2026-10-09-plugin-extension-surfaces.md` — its Phase 9 names imap-mcp as the first real adopter, and its Decisions carry the BL340 backend-folding question explicitly deferred to a future decision.

## Scope

- `examples/plugins/imap-mcp/manifest.yaml` (new) — the worked `kind: service` manifest
- `internal/plugins/` — attach-only health probe (`attach_health.go`), service status, the service-event recorder (`service_events.go`, `model_node_events.go`), model use + Ollama node memory findings (`model_use.go`), token listing/rotation (`token_usage_test.go`), supervisor wiring (`supervisor.go`, `registry_attach.go`, `plugins.go`, `api.go`)
- `internal/server/` — REST: the service object on `GET /api/plugins/{name}`, token list/rotate/remove, models and status wiring, `plugins.services` config apply
- `internal/mcp/` — read tools (`plugin_service_read_tools.go`) and write tools (`plugin_service_write_tools.go`), `server.go` registration
- `internal/router/` — the `plugin status <name>` comm verb and `configure plugins.services.*`; `cmd/datawatch/` — `datawatch config set`, the `plugins service …` / `plugins widgets …` subcommands (`plugin_service_cli.go`)
- `internal/audit/` — the service-health, lifecycle and service-token actions, CEF signatures and tests
- `internal/config/` (`plugins.managed`, `plugins.services`), `internal/federation/mcp_tool_caps.go`, `internal/observer/ollama_tap.go`
- `internal/server/web/app.js`, the plugin-service card/settings/tokens/models test files, locales (en, de, es, fr, ja), both `openapi.yaml` copies
- `docs/` — `CHANGELOG.md`, `docs/mcp.md`, `docs/cursor-mcp.md`, `docs/api/openapi.yaml`, `docs/config-reference.yaml`, `docs/testing-tracker.md`
- **Not touched:** `internal/messaging/backends/imapmcp/` — the chat messaging backend stays separate (see "Recommendation")

## What this delivers

Tracking and control for service-kind plugins, proven against a real standalone tool. datawatch can now attach to an already-running imap-mcp (or any `kind: service` plugin), health-probe it on a fixed interval, report what it learned (lifecycle mode, state, address, version, uptime, last check), notice the four operator-relevant conditions (service down, token rejected, enrichment backlog growing, a model's Ollama node unreachable) as one alert plus one audit entry per transition, list/rotate/remove the tokens a manifest references, and read model use with node-memory findings. Every one of those is reachable on every surface — REST, MCP, CLI, comm channel, YAML/config, and the PWA — with the same capability and operator-token gating the rest of the service surface uses.

## Prerequisites the plugin-extension-surfaces design already built (shipped, not re-worked here)

| Piece | Where | Status |
|---|---|---|
| Manifest v3 — `kind: service`, `connection` (`base_url`, `health_path`, auth token reference), `lifecycle` (`supported_modes`, `entry`, `args`, `restart_policy`, `stop_timeout_ms`), widgets, push tokens | `internal/plugins/registry_services.go` | Shipped |
| Managed-service supervisor — starts the manifest entry, health checks, restart policy and back-off; refuses modes the manifest does not declare | `internal/plugins/supervisor.go` | Shipped |
| Widget store, scheduler, REST routes (`/api/plugins/{name}/widgets…`) | `internal/plugins/`, `internal/server/plugins.go` | Shipped |
| PWA widget renderer and dashboard | `internal/server/web/app.js` | Shipped |
| Federation caps `plugin:read` / `plugin:write`; the lifecycle REST route (`POST /api/plugins/{name}/enable` with `{"managed": true}`) | `internal/federation/`, `internal/server/plugins.go` | Shipped |

Design source: `docs/plans/2026-10-09-plugin-extension-surfaces.md`. Every row above shipped in earlier releases; this plan's work builds on top of them and changes none of their contract.

## Phases and status (this plan's work)

| # | Phase | Status |
|---|---|---|
| 1 | The imap-mcp `kind: service` manifest (`attach_only` + `managed`), with its widgets, as the worked example in this repo | Done |
| 2 | Attach-only health probe on a fixed interval — up/down/unknown, last check, latency, version capture (`plugins.attach_health.*`) | Done |
| 3 | Status payload on `GET /api/plugins/{name}`: lifecycle mode, state, address, version, uptime, last health check | Done |
| 4 | Service-event recorder: service down, token rejected (401/403), enrichment backlog growing, model node unreachable — one alert + one audit entry per transition, with recovery, thresholds as settings | Done |
| 5 | Per-plugin token usage listing, then rotate/remove through the secrets vault (old token revoked, new minted under the same name; a value is never echoed) | Done |
| 6 | Model use collection (incl. Ollama node memory) and the small-model / node-load / large-context findings | Done |
| 7 | CEF signatures and tests for the full plugin action set (service-health, lifecycle, widget and service-token actions) | Done |
| 8 | MCP tools: read (`plugin_service_status`, `plugin_widget_list`, `plugin_widget_get`, `plugin_token_list`) and write (`plugin_service_start|stop|restart`, `plugin_token_rotate`), with the federation caps; attach-only refusal with a clear reason | Done |
| 9 | CLI subcommands (`datawatch plugins service status|start|stop|restart`, `datawatch plugins widgets list|get`), the `plugin status <name>` comm verb, `configure plugins.services.*` | Done |
| 10 | `plugins.services` settings block (probe interval, backlog streak, node-load ratio, small-model bounds, per-service lifecycle mode): config load validation, `GET`/`PUT /api/config`, `datawatch config set`, `docs/config-reference.yaml` | Done |
| 11 | PWA: the plugin service status card in the Plugin Manager, the Settings section (five locales, help entries), the tokens panel with rotate/remove, the models panel with findings | Done |
| 12 | This plan record | Done |
| 13 | Operator docs for the remaining surfaces (CHANGELOG entries for phases 2–4, 6, 9–11; `docs/howto/`; CLI and comm references; `docs/testing-tracker.md` rows) | In Progress |
| 14 | Mobile parity issue draft and filing (datawatch-app, at the merge step) | Planned |

Shipped in: the release that carries this change (the merge helper records the number in the changelog).

## Recommendation

**Keep `internal/messaging/backends/imapmcp/` separate from the service plugin, for now.**

- The messaging backend is the **chat receive/send event stream**: SSE receive (`GET /api/events`), REST send (`POST /api/accounts/{account}/messages/send`), subject prefixing, the `imap_mcp` channel config. It routes mail into sessions and back.
- The service plugin is **supervision and status**: attach-only probes, lifecycle mode, tokens, model use, alerts, the panels. It watches and controls.
- The manifest is already the join point: `examples/plugins/imap-mcp/manifest.yaml` points `connection.base_url` at the running imap-mcp and shares one token via the secret reference — datawatch attaches as an ordinary HTTP client, and imap-mcp stays fully standalone for anyone who never touches datawatch (the design's decision 3).
- Fold later, and only if one manifest-owned connection can serve both planes — the chat event stream and the supervision surface — without changing the send path. As long as the two live on different planes, folding couples them; it does not remove the overlap. The recommendation holds until the operator says otherwise.

## Open question

**Should the BL340 messaging-backend integration fold into the `service`-kind manifest?** The design carries it "explicitly deferred, not resolved, flagged for a future decision"; it is to be revisited now that a `service`-kind imap-mcp plugin exists and the overlap is concrete. This is the operator's decision — this plan does not implement a fold either way. Anything that would depend on folding (for example one connection config replacing the `imap_mcp` channel block) waits for the operator's answer.

## Parity surface

| Surface | Included | Reason or where |
|---|---|---|
| REST | Yes | the service object on `GET /api/plugins/{name}`; `GET /api/plugins/{name}/tokens`, `POST …/tokens/{secret}/rotate`, `DELETE …/tokens/{secret}`; `POST /api/plugins/{name}/enable {"managed": true}` for the lifecycle; widget routes; `plugins.managed` and `plugins.services` in `GET`/`PUT /api/config`; both `openapi.yaml` copies |
| MCP | Yes | read: `plugin_service_status`, `plugin_widget_list`, `plugin_widget_get`, `plugin_token_list` (cap `plugin:read`); write: `plugin_service_start`, `plugin_service_stop`, `plugin_service_restart`, `plugin_token_rotate` (cap `plugin:write`; token rotation operator-token-only, federation peers get the upstream 403); documented in `docs/mcp.md` and `docs/cursor-mcp.md` |
| CLI | Yes | `datawatch plugins service status|start|stop|restart`, `datawatch plugins widgets list|get`, `datawatch config set plugins.services.<key>` |
| comm channel | Yes | `plugin status <name>`; `configure plugins.services.<key>=<value>` |
| YAML/config | Yes | `plugins.managed` and the `plugins.services` block (probe interval, backlog streak, node-load ratio, small-model bounds, per-service lifecycle mode) in `docs/config-reference.yaml`, with defaults and minimums |
| PWA | Yes | the plugin service status card in the Plugin Manager, the Settings section with help entries, the tokens panel with rotate/remove, the models panel with findings; five locales, node tests per surface |
| Android | Yes | to be filed as a parity issue on the app repository (see `docs/plans/parity-issues/imap-mcp-service-tracking.md`) — the service card, tokens and models panels exist only in the web app today |
| iPhone | Yes, tracked on the same issue | no separate iOS release exists to file against |

Excluded: nothing. `internal/messaging/backends/imapmcp/` is deliberately out of scope rather than a surface exclusion — see "Recommendation".

## Decisions

- **The backend stays separate** (see "Recommendation"); the manifest's `connection` block is the coupling, and the token is shared through one secret reference.
- **Attach-only means datawatch never starts the process.** The probe connects to the service the operator runs (`plugins.attach_health`); the managed supervisor only knows services it launched itself, and both paths report into the same status vocabulary. Lifecycle mode per plugin is a `plugins.services.per_service` setting and is only honoured when the manifest declares it.
- **Tokens are operator-token-only and never echoed.** The rotate response carries only status and names; federation peers and session-scoped credentials get the upstream 403; a secret the manifest does not reference is refused.
- **Thresholds are settings, not constants.** Probe interval default 60s / minimum 5s (below the minimum is a config error at load), backlog-streak default 3, node-load ratio 1.5, small-model bound 8B parameters / 8192 context — all overridable in `plugins.services`.
- **The manifest ships here as the worked example** under `examples/plugins/imap-mcp/`; the operator's own manifest stays with imap-mcp (per the design, in the community repo), and imap-mcp's code is unchanged by anything in this plan.
- **Skills.** `skills-hook: N/A` — this plan adds no new session, Automaton, agent, comm-channel or plugin path a skill would need to hook; it tracks and supervises the existing plugin-service surface. imap-mcp's existing skill (teaching an agent to *use* its MCP tools) is untouched.
- **Reuse audit.** The manifest registry, supervisor, widget store, capability gates and CEF machinery all pre-date this plan; new pieces are the attach-only probe, the status payload fields, the service-event recorder and its four conditions, the model-use findings, the token routes, and the per-surface wiring that proxies the existing REST contracts. Nothing new was built where one of those fits.
- **Live smoke.** The automaton runs under the no-daemon rule, so the Phase Completion Checklist's live item is owed at the merge step on a sandbox daemon rather than in this branch's commits.

## Phase completion checklist (every phase)

- [x] Functionality implemented for phases 1–12 (commits a0d00003–a0d00018 on this branch).
- [x] Unit tests added and passing (probe, recorder, tokens, models, CEF, config, MCP tools, CLI, comm verb, PWA node tests).
- [ ] Live test on a sandbox daemon: owed at the merge step (see Decisions).
- [x] `CHANGELOG.md` entries for the MCP tools, the service-event recorder, and the token routes; `docs/config-reference.yaml`; `docs/mcp.md` and `docs/cursor-mcp.md` for the MCP tools; `docs/api/openapi.yaml` and the web `openapi.yaml`; `docs/testing-tracker.md` section for the token routes.
- [ ] `CHANGELOG.md` entries and references for the attach probe, status payload, `plugins.services` settings, CLI, comm verb, and the four PWA surfaces; a `docs/howto/` page: owed (phase 13, In Progress).
- [x] Locales in five languages for every new PWA string.
- [ ] App parity issue: draft owed under `docs/plans/parity-issues/imap-mcp-service-tracking.md`; the merge step files it on datawatch-app.
- [ ] Open question (the fold) answered by the operator before any folding work starts.
