# Plan: GH#201 — access/audit logging completeness

- **Date**: 2026-10-09
- **Version at planning**: v8.73.40
- **Status**: Phase 1 shipped (v8.73.41) and gap-closed (v8.74.0 — see
  "Phase 1 gap closure" below). Phases 2-5 planned, not started.
- **Filed by**: datawatch-app, operator-requested — could not confirm
  whether Apple TestFlight reviewers had connected to the demo server;
  no HTTP access log, no WS connect/disconnect log, no auth-failure log,
  and the existing operator audit log stops after 3 seed entries (later
  session/config/Automata changes are missing).
- **Scope**: `internal/audit` (new CEF capability), `internal/server`
  (`accesslog.go`, `federation_cap.go`, `api.go`, `server.go`),
  `internal/config/config.go`, `internal/metrics/prometheus.go`,
  `internal/stats/collector.go`, `internal/mcp` (`server.go`,
  `sx_parity.go`), `internal/federation/mcp_tool_caps.go`,
  `internal/server/web/app.js` + 5 locale bundles,
  `scripts/release-smoke.sh`, `docs/config-reference.yaml`,
  `docs/implementation.md`, `docs/operations.md`, `docs/mcp.md`.

## Status at a glance

| Phase | What | Status | Shipped in |
|---|---|---|---|
| 1 | HTTP access / WS lifecycle / auth-failure log — core | ✅ Done | v8.73.41 |
| 1 (gaps) | AGENT.md compliance closure (CEF, config/doc/MCP parity, observability, smoke, release checklist) | ✅ Done | v8.74.0 |
| 2 | Federation-hop actor attribution | ⬜ Not started — open design questions, see below | — |
| 3 | Chained-children (`ParentAgentID`) in the agent audit trail | ⬜ Not started | — |
| 4 | State-changing-action completeness sweep | ⬜ Not started | — |
| 5 | Create-alert API + MCP tool | ⬜ Not started | — |

Update this table (and each phase's own Status line) every time a
phase's state changes — this is the first thing anyone re-opening this
plan should be able to trust without reading the whole document.

## What already existed (don't rebuild this)

- `internal/audit` (BL9) — append-only JSON-lines log, `Write`/`Read`
  with a real `QueryFilter` (actor/action/session_id/since/until/limit),
  already exposed as `GET /api/audit` (`internal/server/audit.go`) and
  the `audit_query` MCP tool (`internal/mcp/sx_parity.go`). This is a
  solid foundation — Phase 1 reuses the same type for a second log
  rather than inventing new machinery.
- 12 files already call into it: `algorithm.go`, `compute.go`,
  `council.go`, `evals.go`, `identity.go`, `inference.go`, `secrets.go`,
  `skills.go`, and `sec017_config_audit.go` (the generic `PUT
  /api/config` path, which masks credential-shaped values before
  logging — reuse `isSensitiveConfigKey`/`maskConfigValue` from that
  file for any new masked-value logging, don't reinvent it).
- `internal/agents/audit.go` (F10, separate subsystem) — agent
  spawn/terminate/revoke/sweep events, `FileAuditor` IS correctly wired
  in `main.go` (`agentMgr.Auditor = a`, confirmed by reading the code,
  not assumed). **`audit/agents.jsonl` being 0 bytes in the original
  report is not a bug** — it's empty because this demo deployment has
  never spawned an F10 remote-agent-cluster worker, the only thing this
  file logs.
- `auth/audit.jsonl` (`internal/auth.TokenBroker`) is **also not a
  general auth-failure log** — it only logs git-token mint/revoke/sweep
  events for F10's GitHub token broker. Its being empty is correctly
  explained by the same reason. The original report's "auth-failure
  log" ask needed a log that didn't exist anywhere yet — see Phase 1.

## Phase 1 — HTTP access / WS lifecycle / auth-failure log ✅ shipped (v8.73.41)

Everything in this phase is done, tested, and on `main`:

- **One choke point, three surfaces.** `datawatch` CLI subcommands
  (`cmd/datawatch/cli_*.go`) and MCP tools (`internal/mcp`'s
  `proxyGet`/`proxyPost`) are both HTTP clients against the same
  `/api/*` REST surface the PWA and direct API callers use — there is
  no separate code path per surface. That surface, plus `/ws`, is
  wrapped by exactly one middleware, `fedAuthMiddleware`
  (`internal/server/federation_cap.go`). Logging there covers CLI + API
  + MCP access uniformly, in one hook, not three.
- **New `access.log`**, separate file from `audit.log` (access-log
  volume is much higher and individually less significant than a
  state-changing operator action — mixing them would drown the signal).
  Same `audit.Log` type, opened via a new `audit.NewAt(path)` (exact
  path, vs `audit.New(dir)`'s hardcoded `audit.log` filename).
- Every request through `fedAuthMiddleware` logs `http_access` (success)
  or `auth_failure` (the final `401` fallthrough), with `method`, `path`,
  `status`, `remote_ip` (`r.RemoteAddr` with the port stripped),
  `user_agent`, and `actor` resolved to one of `admin`, `session-scoped`,
  `peer:<name>`, `proxy:<name>`, or `unauthenticated` — **never the
  Authorization header or token value**, confirmed by a test that greps
  the written entries for the actual token strings used in the test
  (`TestGH201_FedAuthMiddleware_LogsAccessAndAuthFailure`).
- WS connect/disconnect logged the same way from `handleWS`
  (`internal/server/api.go`), capturing principal/remote_ip/user_agent
  from the pre-upgrade request (unavailable once it's a bare
  `websocket.Conn`).
- Query surface: `GET /api/audit/access` (mirrors `GET /api/audit`'s
  filter shape) + MCP tool `audit_access_query` (mirrors `audit_query`,
  same `CapAuditRead` capability).
- Config: `cfg.Audit.AccessLogEnabled` (default true, pointer-bool
  tri-state) and `cfg.Audit.RetentionDays` (default 30; negative =
  never prune). `audit.Log.Prune(cutoff)` rewrites the file dropping
  older entries; applied to both `audit.log` and `access.log` at daemon
  startup and on a 24h ticker in `main.go`.
- Tests: `internal/audit/log_test.go` (Prune, NewAt),
  `internal/server/gh201_accesslog_test.go` (middleware logs both
  success and failure, the no-token-in-log property, the REST handler).

## Phase 1 gap closure ✅ shipped (v8.74.0)

Phase 1 initially shipped without checking AGENT.md first — a direct
operator instruction ("make sure plan follows AGENT.md and
DATAWATCH-CONTEXT.md rules") caught 8 real gaps against the Audit
Logging Rule, Versioning, Planning Rules, and the Documentation/
Config-Parity/Observability checklists. All closed in this version:

1. **CEF support (the one that actually changed the design).**
   AGENT.md's Audit Logging Rule requires every audit-style event be
   emittable as *both* JSON-lines and CEF. `internal/audit.Log` only
   did JSON-lines. Unlike `internal/agents/audit.go`'s `FileAuditor`
   (one format per file, chosen at construction — fine for F10's low-
   volume agent events), this package's logs are read back by their
   own REST/MCP query surface, so CEF can't be a format *switch* (CEF
   isn't JSON; `Read()` would break). Implemented as an additive
   **mirror**: `Log.EnableCEFMirror()` opens `<path>.cef` and every
   `Write()` appends a CEF line there too, alongside the unaffected
   JSON-lines file. New `internal/audit/cef.go`:
   `FormatCEFLine`/`cefSignature`/escape helpers, gated by
   `cfg.Audit.CEFMirrorEnabled` (default false — opt-in, most operators
   don't run a SIEM). `cefSignature` maps the 4 new access-log actions
   plus the operator-log actions named in `audit/log.go`'s own doc
   comment (`start`/`kill`/`send_input`/`configure`/`rollback`/
   `schedule`); anything else gets a stable generic fallback
   (sigID 0, "AuditEvent", severity 3) — add a case here as each new
   Action is introduced (Phase 4 will add several), not an exhaustive
   upfront list. Tests: `internal/audit/cef_test.go` — header
   pipe/backslash escaping, extension `=`/`\`/`\n`/`\r` escaping, every
   (signatureID, name, severity) triple, and the mirror-writes-
   alongside-JSON property.
2. **Versioning.** "Every completed feature = minor bump" — Phase 1
   (new log, new endpoint, new MCP tool, new config section) is a
   feature, not a patch; it shipped as v8.73.41 (patch) by mistake.
   Per "never reuse a version," that can't be un-shipped — this
   gap-closure commit is the minor bump instead (v8.74.0), since the
   feature genuinely isn't complete without this closure anyway.
3. **This plan was missing its required `## Parity surface` section**
   (Planning Rules item 8) — added below.
4. **Config parity (B6).** `cfg.Audit.{AccessLogEnabled,RetentionDays,
   CEFMirrorEnabled}` now round-trip through `GET`/`PUT /api/config`
   (`handleGetConfig`'s map, `applyConfigPatch`'s switch —
   `audit.access_log_enabled`, `audit.retention_days`,
   `audit.cef_mirror_enabled`), a PWA settings card
   (`GENERAL_CONFIG_FIELDS`'s new `audit` section in `app.js`, 3 new
   locale keys × 5 bundles), `docs/config-reference.yaml`, and
   `docs/implementation.md`'s config table. Known limitation, not yet
   fixed: changing `retention_days` or `cef_mirror_enabled` live via
   `PUT /api/config` updates the config value but doesn't re-trigger
   `EnableCEFMirror()` on the already-open `*audit.Log` — takes effect
   on next restart, same as several other structural config fields in
   this codebase.
5. **New-MCP-tool checklist.** `audit_access_query` documented in
   `docs/mcp.md` under Available Tools (parameter table + example) and
   added to the tool-family summary row. `docs/cursor-mcp.md` was
   skipped — `audit_query`, this tool's own sibling, was never added
   there either; adding only the new one would be more misleading than
   the pre-existing gap. Flagged, not silently worked around.
6. **Observability (B7).** `datawatch_access_log_events_total{action,
   principal_kind}` Prometheus counter (`internal/metrics/
   prometheus.go`) — labeled by the *coarse* principal kind
   (`principalKind()` strips everything after `:`), never the exact
   peer/proxy name, which would be unbounded label cardinality.
   `stats.SystemStats` gained `AccessLogEnabled`/`AccessLogEventsTotal`/
   `AccessLogAuthFailuresTotal` (in-process counts since daemon start,
   wired via `Collector.SetAccessLogStatsFunc` →
   `Server.PopulateAccessLogStats`, mirroring the existing
   `memoryStatsFn` pattern exactly). **Not done**: a `renderStatsData()`
   Monitor card in `app.js` — checked, and the much older `web_search_*`
   stats fields never got one either; flagged as a real, pre-existing
   gap in this codebase's own B7 compliance, not newly introduced here,
   and not fixed in this pass.
7. **CI check (A11).** `gh run list` checked after every push in this
   phase; see the Release Checklist below for the actual run.
8. `docs/operations.md` gained an "Audit & Access Logging" section
   (event types, retention, CEF, metrics, a worked `curl`/`jq`
   example) — referenced from the new `docs/config-reference.yaml`
   `audit:` block's comment.

**Confirmed fine, not a gap**: CHANGELOG.md's `GH#201`/`BL399`
references were briefly suspected to violate the no-internal-tracker-
IDs rule — re-read the actual current rule text (corrected 2026-10-07)
and CHANGELOG.md is explicitly whitelisted as archaeology material,
same as `docs/plans/*.md`. No change needed.

## Parity surface

- **REST**: `GET /api/audit/access` (new, Phase 1). `GET`/`PUT
  /api/config`'s `audit.*` keys (new, gap closure). Every other
  `/api/*` route gets the *access-logging* side effect automatically
  via `fedAuthMiddleware` — not a new endpoint per route, the whole
  surface is covered by construction.
- **MCP**: `audit_access_query` (new, mirrors `audit_query`,
  `CapAuditRead`). No new tool needed for the config keys — they ride
  the existing generic `config_set`/`get_config` tools.
- **CLI**: no new subcommand. CLI traffic is itself one of the three
  surfaces `/api/audit/access` now has visibility *into* (confirmed:
  `cli_*.go` subcommands are `http.NewRequest` clients against the same
  REST surface) — adding a dedicated `datawatch audit access` CLI verb
  would be reasonable future polish but isn't required for the
  logging/query function to work, and wasn't asked for.
- **Comm channel**: excluded. There's no existing precedent for
  exposing audit/security logs through a chat-style comm channel
  (Signal/Discord/etc.), and doing so would mean a log entry — however
  filtered — reaching a possibly-group-shared channel. Deliberately
  not built; revisit only if explicitly requested.
- **YAML/config**: `audit.access_log_enabled`, `audit.retention_days`,
  `audit.cef_mirror_enabled` (new, gap closure).
- **PWA**: new "Audit & Access Log" settings card (gap closure, 3
  toggles/fields). No dedicated log *viewer* UI (e.g. a tailing view of
  `/api/audit/access`'s entries) — out of scope for this phase; the
  REST/MCP query surface is the intended consumption path for now
  (datawatch-app's own visitor-monitor tool is the first real
  consumer).
- **Android / iPhone**: excluded. This is a daemon-operator surface
  (who's hitting *my* server), not a per-device end-user feature either
  mobile app would show its own user. No mobile-parity issue filed —
  confirmed there's nothing on either app's side this logging
  duplicates or needs to match.

## Phase 2 — thread provenance through federation hops (not started)

**The real gap, raised directly by the operator.** Phase 1's
`peer:<name>` / `proxy:<name>` principal only records *which peer
presented the request to this daemon* — not who behind that peer
actually initiated it on their own daemon. Concretely: if operator
Alice on daemon A triggers an action that daemon A forwards to daemon B
as a federated peer call, daemon B's access log says `peer:daemon-a`,
never `Alice`. Nothing in the federation/proxy code today forwards an
origin-actor identity across a hop — confirmed by reading
`internal/server/agent_proxy.go`, `comm_proxy.go`, `bl320_proxy_llm.go`,
`proxy.go`: none carry an origin-actor field.

Open design questions this phase needs to settle before implementing
(don't guess at these — they're genuine tradeoffs):
- **A new forwarded header**, e.g. `X-Datawatch-Origin-Actor`, set by
  the forwarding daemon and read (not trusted blindly) by the receiving
  one. Trust model: a federation peer is already a trusted principal
  (it holds a real peer token) — is "I say this came from Alice"
  sufficient, or does the receiving daemon need a cryptographic chain
  (each hop signs/extends the chain) so a compromised mid-chain peer
  can't fabricate an upstream actor for its own requests?
- **Chain depth/format**: a single "origin" field, or a full hop list
  (`[Alice@daemon-a, daemon-a→daemon-b]`) so a 3+ hop chain stays fully
  attributable, not just collapsed to "whoever asked first"?
- **Multi-host compute nodes** (`internal/compute`) are a related but
  distinct case: a session's work can execute on a remote compute node
  the daemon doesn't directly operate. Does the audit entry need to
  record which *host* actually ran the action, separate from which
  *daemon* logged it? Check `internal/compute/node.go`/`probe.go` for
  what identity a compute node already reports back, before adding a
  new field — it may already carry enough to attribute with.

## Phase 3 — chained-children (F10 agent spawn) attribution (not started)

`internal/agents` already tracks `ParentAgentID` on every agent
instance (confirmed in `oncrash.go`, `post_session_validate.go`,
`reconcile.go`, `docker_driver.go`, `k8s_driver.go` — even threaded into
container labels: `datawatch.parent_agent_id`). But
`agents.AuditEvent` (`internal/agents/audit.go`) has no `ParentAgentID`
field — the spawn-chain data exists on the instance, it just never
makes it into the audit trail itself. This is a small, well-scoped fix
once someone's looking at it (add the field, thread it through the ~6
call sites that construct an `AuditEvent`), not a design question like
Phase 2 — just not done yet.

## Phase 4 — state-changing-action completeness sweep (not started)

The actual "record what changed, not just that a request happened"
ask. Access-logging `PUT /api/config` tells you a request happened;
it doesn't tell you *what changed* — that needs an explicit
`s.auditLog.Write(audit.Entry{...})` call with meaningful
action/details inside each handler, same pattern `sec017_config_audit.go`
already uses for the generic config path.

Confirmed-missing (grepped for every file that touches `s.auditLog`/
`audit.Entry` and compared against the handler list — these are NOT
covered today, ordered by how state-changing/sensitive they are):
1. **Session lifecycle** — start, kill, send_input, rollback. (The
   original report's own example: "later PUT /api/config, session and
   Automata changes are missing.")
2. **Automata/PRD lifecycle** — create, approve, reject, request
   revision, cancel, decompose, add/remove story or task, set_* config
   calls (`autonomous_prd_*` handlers).
3. **Alert rules** — create/update/delete/enable/disable.
4. **Federation peer management** — add/remove/update a peer, group
   changes (distinct from the *access* a peer makes, which Phase 1
   already logs — this is changing *who's allowed in*, a materially
   more sensitive action).
5. **Device registration** — register/delete (push-notification
   targets; who gets paged matters).
6. Lower-priority, still real gaps: templates, guardrail profiles,
   schedules, orchestrator graph create/run/cancel, council config.

Each of these is mechanical once the list is fixed — this phase is
sizing/sequencing, not inventing a new pattern (reuse `audit.Entry` and
`sec017`'s masking helper for any value that might be credential-shaped).

## Phase 5 — create-alert API + MCP tool (not started)

Separate ask, folded in because it's downstream of Phase 1-4: a
`POST /api/alerts {level, title, body, category, priority}` →
`alertStore.AddSystem` → normal push delivery (SSE/ntfy/FCM/APNs),
giving an external monitor (or the access log itself, e.g. a
first-seen-IP-authenticated event) a real way to raise a high-priority,
separately-routable alert instead of the existing endpoints' dead ends
(`POST /api/alerts` today only marks alerts read; `/api/push/notify`
skips SSE-registered devices; `/api/channel/notify` is WS-broadcast
only). Needs its own look at `internal/alerts` (`AddSystem`'s current
signature) and `internal/server/push.go` (dispatch) before sizing —
not investigated yet.

## Out of scope / already correct, don't touch

- `internal/agents/audit.go`'s `FileAuditor` wiring — already correct,
  confirmed by reading `main.go`, not just assumed.
- `auth/audit.jsonl` — correctly scoped to git-token-broker events only;
  not a general auth-failure log and was never supposed to be one.

## Reporting back to datawatch-app

Per the operator's explicit ask: once a phase ships, report (1) exactly
what's logged (event types/fields, file paths, rotation/retention,
config keys/defaults), (2) the exact API endpoint + MCP tool with
filters and a sample request/response for "which clients connected" and
"auth failures/new IPs", (3) recommended alerting (which events should
page the operator, whether the daemon can self-alert via Phase 5's
create-alert API once it exists). Phase 1 is ready to report now.

## Verification

- Each phase: Go unit tests in the same style as Phase 1's (`gh201_*`
  naming), plus a `docs/testing-tracker.md` entry per this repo's
  Tested/Validated convention.
- Phase 2 specifically needs a live two-daemon test (two local instances
  federated to each other) before calling cross-hop attribution done —
  a unit test against a single daemon can't prove a forwarded-header
  design actually survives a real hop.

## Release checklist

Filled in per release for this feature, mapping AGENT.md's Section A/B/
C tables to what this specific plan touches — copy this block forward
to the next phase's commit and tick it again rather than re-deriving it
from AGENT.md each time.

### Every commit (AGENT.md Section A) — this commit

- [x] A1 — rules: Audit Logging Rule, Versioning, Planning Rules
  (Parity surface), Documentation Rules (general checklist + new-MCP-
  tool checklist), Project Tracking, Testing Requirements (B6/B7/B12/
  B16)
- [x] A2 — `go test ./...`: 3268 passed, 0 failed
- [x] A3 — `version: v8.74.0 (both files)`
- [x] A4 — `changelog: added`
- [x] A5 — `readme: updated`
- [x] A6 — `backlog: refactored` (BL399 entry updated)
- [x] A7 — `id-check: clean` (CHANGELOG's GH#/BL# refs are whitelisted
  archaeology material; README's new line has none)
- [x] A8 — `leak-check: clean`
- [x] A9 — `node-check: ok`
- [x] A10 — `make-build: ok`
- [ ] A11 — `ci: <pending — check after push>`

### Conditional, this feature's actual triggers (AGENT.md Section B)

| # | Trigger | Applies here? | Status |
|---|---|---|---|
| B1 | New/changed endpoint contract | Yes — `GET /api/audit/access` | ✅ `docs/testing-tracker.md` entries added each phase |
| B2 | New PWA user-facing string | Yes — 3 audit settings keys (gap closure) | ✅ 5 locales updated |
| B3 | New high-visibility locale key | No — settings-card fields, not nav/action chips | N/A |
| B4 | Operator-visible PWA change | Yes — new settings card | Not yet filed as a `datawatch-app` comment — this is a daemon-operator surface, not something either mobile app renders; revisit if that judgment turns out wrong |
| B6 | New/changed config field | Yes — `audit.*` | ✅ YAML+REST+MCP+CLI(generic)+PWA done this phase; comm excluded (see Parity surface) |
| B7 | New feature, observability | Yes | ✅ Prometheus + in-process stats done; Monitor card explicitly not done (pre-existing gap pattern, flagged not fixed) |
| B8 | New feature, access-method docs | Yes | ✅ `docs/operations.md` new section |
| B12 | New operator-facing endpoint | Yes — smoke section 66 added | ✅ |
| B16 | New audit-event-emitting code path | Yes | ✅ CEF + escape tests added (`internal/audit/cef_test.go`) |

### Release cadence (AGENT.md Section C) — this is a minor release

- [x] C1 — `dep-audit: N/A` (no new dependency this phase — only
  stdlib `fmt`/`sort`/`strings`/`os`/`io` plus the already-vendored
  `prometheus` client)
- [x] C2 — `gosec: clean` (516 pre-existing findings repo-wide, none in
  `internal/audit/cef.go`, `internal/server/accesslog.go`,
  `internal/metrics/prometheus.go`, or `internal/stats/collector.go`)
- [x] C3 — `smoke: 66 sections, 185 passed, 35 skipped, 0 failed`
  (section 66 itself skipped — sandbox has no admin token configured,
  identical to its neighbor section 65, not a regression)

### Phase-specific gate before marking any future phase ✅ Done

Don't mark a phase done in the status table above until:
1. Its own unit tests pass (`go test ./...` clean).
2. Its own `docs/testing-tracker.md` row exists.
3. The Release checklist above has been run for the commit(s) that
   shipped it.
4. This plan doc's "Status at a glance" table and the phase's own
   heading are both updated in the same commit — not left to a
   follow-up.
