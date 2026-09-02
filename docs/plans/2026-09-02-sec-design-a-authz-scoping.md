# Design A — Authorization & Credential Scoping

- **Date**: 2026-09-02
- **Version at planning**: v8.19.0
- **Status**: Planned (not started; design only — no code changes in this commit)
- **Finds this closes**:
  - **Core:** SEC-001 (empty token ⇒ no auth), SEC-002 (MCP open), SEC-006 (query token), SEC-007 (WS origin / empty token), SEC-008 (agent 0.0.0.0 bind), SEC-009 peer-scope residual, **SEC-014 (peer token leaked in `/api/federation/peers`)**.
  - **Hostile-LLM:** **HLLM-001** (admin token in 0644 `.mcp.json`), **HLLM-002** (session = full-admin principal), and the *enabler* of HLLM-004/005/007.
- **Design principle**: there must be **three** separable controls today; there is effectively zero.
  1. **The daemon must not be reachable without auth** unless the operator explicitly opts in.
  2. **Not every authenticated caller is an admin.** A session, a peer, and the operator
     must be distinct principals.
  3. **The credential a session holds must be scoped and revocable**, not the operator's admin token.

---

## 1. Current state (facts)

| Control | Where | Problem |
|---|---|---|
| Admin gate | `internal/server/federation_cap.go:68` `fedAuthMiddleware` — `if s.token == "" { next }` | empty token = **no auth at all** on all ~200 `/api/*` routes (SEC-001) |
| Capability gate | `internal/server/federation_cap.go:37 fedCap` + `internal/federation/capabilities.go` (groups incl. `federation-peer` caps) | exists, but **opt-in per route**; only a few REST routes call `s.fedCap(...)` (e.g. `/api/cert`, `/api/logs`). The default posture is "admin-or-open" |
| MCP gate | `internal/mcp/bl317_fed_auth.go:45 mcpFedAuthMiddleware` — `if s.cfg.Token == "" { pass }` | empty `mcp.token` = **open 361-tool surface** (SEC-002) |
| Session credential | `cmd/datawatch/main.go:1141-1146` — `channelEnv["DATAWATCH_TOKEN"] = cfg.Server.Token` | the session's bridge gets the **admin** `server.token` (HLLM-001/002) |
| Credential file | `internal/channel/mcp_config.go:110,148,220,251` — `os.WriteFile(path, out, 0644)` | the admin token file is **world-readable** (HLLM-001) |
| Peer token | `internal/server/federation_peers_api.go` returns `*multiserver.Entry` (incl. plaintext `Token`) via `GET /api/federation/peers` | any peer in `federation-peer` group **steals every peer's token** (SEC-014) |
| WS origin | `internal/server/ws.go:141` `CheckOrigin: func...{ return true }` | origin ignored; unauth under empty token (SEC-007) |
| Query token | `federation_cap.go:72` reads `?token=`; `bl317_fed_auth.go:47` same | loggable / redirectable (SEC-006) |
| Agent bind | `cmd/datawatch-agent/main.go:74` default `0.0.0.0:9877` | unauth `/stats` on all interfaces (SEC-008) |
| Default bind | `internal/wizard/defs.go:572` default `host: 0.0.0.0` | open-by-default on a LAN/TS peer (SEC-001) |

## 2. Design (best fix)

### A1. Fail-closed auth (SEC-001, SEC-007, SEC-008, SEC-002)

**Rule:** an authenticated surface must not be reachable without a credential.

- `fedAuthMiddleware`: **remove the `s.token == ""` pass-through.** When `server.token` is
  empty, require a *bootstrap* condition: on first start the daemon generates a
  token (crypto/rand, 32-byte) into the data dir, logs it once, and enforces it from
  then on. Operator can set their own via `datawatch config set server.token` (YAML).
  An **explicit opt-in escape hatch** `server.allow_empty_token: true` (new config key,
  defaults false, warned at boot and in `/api/health` `auth_required:false`) restores
  today's behavior for pure-local/dev; document it as insecure.
- **Bind policy:** when `server.token` is empty (or `allow_empty_token`), **refuse to
  bind any non-loopback interface** (bind `host` to `127.0.0.1`/`::1` and warn). A
  non-loopback bind with an empty token must be rejected at startup. This is the direct
  fix for "open on 0.0.0.0" without breaking a trusted-localhost operator.
- Wizard default bind `0.0.0.0` → `127.0.0.1` (`wizard/defs.go:572`).
- WS `CheckOrigin` (`ws.go:141`): enforce a same-origin / configured-allowlist origin when
  the request is unauthenticated; still accept token-authenticated upgrades (token is the
  real guard). Under a **set** token origin stays advisory; under empty token it enforces.
- `cmd/datawatch-agent/main.go:74` default listen `0.0.0.0:9877` → `127.0.0.1:9877`;
  add a `listen 0.0.0.0` opt-in flag only.

**Why not just "always require a token":** operators run pure-loopback instances where
"no token, loopback only" is the sane default; forcing token entry on a `curl
localhost` dev loop is friction. The opt-in escape hatch + bind-refuse covers both.

**Compat risk (must verify in Phase 5 smoke + PWA):** the PWA, CLI, comm router, and MCP
bridge all connect to loopback with the token from config — none should break. The
`allow_empty_token` escape hatch protects the rare "no token + remote" operator.

### A2. Make capabilities opt-*out*, not opt-in (HLLM-002, SEC-009; enables A3)

This is the load-bearing change. Today `fedCap` is called by a *handful* of routes; every
other route is "authenticated ⇒ allowed." Invert it:

- Introduce a **route-level capability map** (single source of truth) in
  `internal/server/route_caps.go`: `map[string]federation.Cap` for every `/api/*` route,
  defaulting to a permissive group for reads and a *narrow* group (`sessions:own`,
  `config:self`) for writes. `fedAuthMiddleware` consults this map **on every request**
  and 403s if the caller's capability set (admin ⇒ all; peer ⇒ their group; session ⇒ its
  scoped set) lacks the required cap. This replaces the scattered per-route `s.fedCap`
  calls and makes the gap structural, not opt-in.
- **Admin is still a distinct principal** with full caps (unchanged UX for the operator).
- This is a **behavior change for federation peers** (they already get 403s — good — but
  their *default grant* `federation-peer` group in `capabilities.go:209-217` currently
  includes `sessions:input`+`sessions:read`; tighten to read-only-by-default and make
  `sessions:input` an explicit opt-in grant). Re-run core-plan T5 verdicts.

**Acceptance:** a registered federation peer without a grant gets 403 on
`/api/restart`, `PUT /api/config`, `/api/secrets/*`, `agent spawn`, `plugin reload`,
`schedule spawn`, `autonomous_prd_run`, `tailscale_acl_push`; a *peer with* the grant gets
200. Re-verify the core T5 matrix.

### A3. Per-session scoped credential (HLLM-001, HLLM-002 root; cascades HLLM-004/005/007)

The session must not hold the admin token.

- Mint a **per-session capability token** at session start (`internal/server` + a new
  `internal/auth/session_token.go`): `crypto/rand` 32-byte, 256-bit, stored in an
  in-memory + WAL set keyed by full session id, with a **capability bitmap** = the
  session's allowed ops (default: `sessions:own`, `sessions:read`, `memory:namespace:<ns>`,
  `config:self`, and only if the operator enabled self-config, `config:all`). TTL + explicit
  revocation on session kill/`stop_all_sessions` (reuse the `Terminate` pattern from
  `spawn.go:722-724`).
- Replace `channelEnv["DATAWATCH_TOKEN"] = cfg.Server.Token` (`main.go:1144-1146`) with the
  scoped token; the bridge's `DATAWATCH_API_URL` is unchanged. The bridge's admin token is
  thus **gone from the session's reach** — it can no longer reach admin routes even if
  `mcp.allow_self_config` is off (closes HLLM-005's REST-bypass).
- **Fix the credential file perms** (`mcp_config.go:110,148,220,251`): `0644` → `0600`,
  and the containing dir `0700`. This alone stops the *different-uid* read of the token
  (HLLM-001). (A same-uid local process can still read a `0600` file it owns — that is the
  operator's local-trust assumption and is addressed by Plan B's isolation, not here.)
- **Session capability surface = its token's bitmap.** Route A2's route-cap map applies to
  the session token too, so a session hitting MCP / REST / comm all hits the same gate.

**Acceptance:** `GET /api/mcp/tools` with a *session* token returns **only** the tools the
session's bitmap covers; `GET /api/secrets/<op-secret>` with a session token = 403/404;
`PUT /api/config pipeline.max_parallel` with a session token = 403 unless the operator
explicitly granted `config:all` to that session type (and then it is audited, Plan C);
killing the session revokes the token immediately (re-verify core T9/T10 burn pattern).

### A4. Don't leak credentials (SEC-014, HLLM-001 residual)

- `GET /api/federation/peers` and `.../peers/{name}`: **mask the `token` field**
  (return `token_present: true` + a 4-hex short prefix, never the value). Audit path: any
  route that serializes a `multiserver.Entry` must pass through a redactor
  (`internal/server/credredact.go`). This is the direct SEC-014 fix.
- `/api/secrets` list endpoint already omits values — keep. Add **audit + actor** on every
  `secret_get` (Plan C adds the actor).

**Acceptance:** a registered peer calling `/api/federation/peers` sees peer names +
`token_present`, never raw tokens. A peer cannot mint usable credentials from the response.

### A5. Drop the query-string token path (SEC-006, SEC-002 MCP)

- Remove `?token=` acceptance on REST (`federation_cap.go:72`) and MCP
  (`bl317_fed_auth.go:47`); accept only the `Authorization: Bearer` header. This removes
  the access-log / redirect-history token capture surface. WS already rejects `?token=`
  (core T6) — keep.
- **Acceptance:** `GET /api/config?<good token>` ⇒ 401; `Authorization: Bearer <tok>` ⇒ 200.
  MCP same. Update the CLI/PWA/comm bridge to header-only (they already use the header —
  verify; no change expected).

## 3. Alternatives considered

- **Force a strong token on every deployment (no escape hatch):** simplest, but breaks the
  trusted-localhost dev loop and any "local-only, no-auth" posture an operator is running
  today. Rejected in favor of A1's bind-refuse + explicit opt-in.
- **RBAC per-operator (operator-facing roles):** overkill for the single-operator model and
  would change most of the authz verdicts (core §10 F-3); defer. The *session* scoping here
  is the smaller, higher-value slice.
- **Separate `server.token` vs `session.token` vs `mcp.token` in config only:** the tokens
  would still be shared/known to sessions and peers; does not fix HLLM-001/002. Rejected.

## 4. Phases

1. **A1** fail-closed auth + bind policy + WS origin + agent bind (REST/WS/agent).
2. **A4** credential redaction (`peers` endpoints, `.mcp.json` perms).
3. **A2** route-cap map (cap opt-out) + peer default-grant tightening; re-run core T5 matrix.
4. **A5** header-only token acceptance (REST + MCP).
5. **A3** per-session scoped token + bridge credential swap + revocation.
6. **Docs + tests:** `docs/config-reference.yaml` (`server.allow_empty_token`, bind
   default), `docs/operations.md` (auth posture), `docs/security-model.md`, 7-surface
   `server.allow_empty_token`; unit tests for the cap map, scoped-token issuance/revocation,
   peer-token redaction, and the bind-refuse path.

## 5. Definition of done

- With `server.token` empty **and** no `allow_empty_token`, the daemon refuses non-loopback
  bind, and every unauthenticated non-health route is 401 (loopback) / unreachable (remote).
- A registered federation peer cannot read any other peer's token and cannot reach
  admin verbs without an explicit grant (core T5 matrix re-passed).
- A spawned session, with no operator grant, cannot reach `secret_get` of an operator
  secret, `PUT /api/config`, `tail` of another session, or `tailscale_acl_push` — via MCP,
  REST, *or* comm (HLLM-001/002/005 closed).
- `.mcp.json` is `0600`/`0700`; `/api/federation/peers` never returns a raw token.
- Query-string token is rejected; header token still works for PWA/CLI/comm/bridge (no
  functional regression in release-smoke).

## 6. Sequencing & risk

- **Order:** A1 → A4 → A5 → A2 → A3 (A3 depends on A2's cap map existing; A2 must be in
  place before A3 or sessions keep the admin token with no new gates).
- **Risk:** A2/A3 are the highest-churn (every route + every session path). Mitigate by
  keeping admin behavior bit-identical for the operator; add the cap map *behind* an
  `authz.enforce_caps` flag (default on) so the release-smoke + a PWA click-through can
  prove no operator regression before flipping it on for peers/sessions (AGENT.md
  Security-Fix Downstream-Review Rule — v8.8.9 lesson: fix the *whole* surface, test
  end-to-end, not just the scanner's re-pass).
- **Rollback:** A1's bind-refuse + A2's cap map are each behind a config flag; A3's bridge
  credential is a one-line revert. No migration of stored data.
