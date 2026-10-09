# Plan: GH#201 — access/audit logging completeness

- **Date**: 2026-10-09
- **Version at planning**: v8.73.40
- **Status**: Phase 1 shipped. Phases 2-5 planned, not started.
- **Filed by**: datawatch-app, operator-requested — could not confirm
  whether Apple TestFlight reviewers had connected to the demo server;
  no HTTP access log, no WS connect/disconnect log, no auth-failure log,
  and the existing operator audit log stops after 3 seed entries (later
  session/config/Automata changes are missing).

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
