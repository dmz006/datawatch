# Design C — Audit Fidelity & Config-Write Integrity

- **Date**: 2026-09-02
- **Version at planning**: v8.19.0
- **Status**: Planned (not started; design only — no code changes in this commit)
- **Finds this closes**:
  - **Hostile-LLM:** **HLLM-003** (audit cannot distinguish operator from LLM).
  - **Core:** **SEC-016** (token rotation via REST inert — old token stays valid, new one
    401s; no rotate-then-activate), **SEC-017** (config writes emit **no** audit event),
    **SEC-021** (PWA stored-XSS chain: file-service in-repo write + docs-viewer unescaped
    markdown → script execution with the operator session).
- **Assumes** A3 (per-session token) exists, so there *is* a distinguishable actor to log.
  Without A3 the actor is "the admin token" for everything; with A3, "operator token" vs
  "session `<id>` token (cap set X)" is a real distinction — and *that* is the audit fix.
- **Design principle** (AGENT.md Audit-Logging Rule): every security-relevant lifecycle
  event (config write, secret access, token mint/revoke, install, egress, session write)
  must be emittable in **both** JSONL and CEF, and must name the **actor** (human vs
  session vs peer vs bridge) with enough fidelity that "who did X" is answerable from the
  log alone.

---

## 1. Current state (facts)

| Gap | Where | Problem |
|---|---|---|
| No actor on MCP/bridge actions | `internal/mcp/server.go` (all handlers), `internal/mcp/bl317_fed_auth.go` | `secret_get` by a session logs `{"actor":"operator","via":"rest"}` — **the session is invisible** (HLLM-003, live-proven) |
| Config writes unaudited | `internal/server/api.go` `handlePutConfig`/`applyConfigPatch` — zero `audit.Add` calls | no trace of *who/what* changed `server.token`, `detection.*`, `autonomous.*`, etc. (SEC-017, live: several writes, `/api/audit` empty) |
| Token rotation inert | `internal/server/api.go` `handlePutConfig` `case "server.token"` — updates `cfg`/disk but **not** the in-memory `s.token` the middleware reads; no restart call | `PUT server.token=<new>` → old token keeps working, new token 401s; no rotate-then-activate (SEC-016, live) |
| Docs-viewer stored XSS | `internal/server/web/diagrams.js:306-330` — `marked.parse(proseMd)` → `innerHTML`, **no `escHtml`** (contrast the chat path which escapes) + file-service root falls back to operator repo, in-repo write allowed | an in-repo file with `<img onerror>` executes in the PWA with the operator session (SEC-021, live) |

## 2. Design (best fix)

### C1. Actor attribution on the audit trail (HLLM-003)

**Introduce a single `audit.AuditContext`** (actor + via + source) threaded through every
auditable call, instead of the current ad-hoc `"actor":"operator"`.

- `internal/audit/context.go`: `AuditContext{Actor audit.Actor, Via string, RequestID, SessionID, PeerName}` where `actor ∈ {operator, session:<fullID>, peer:<name>, bridge-<session>, system}`.
- **Derive the actor at the edge**, not in each handler:
  - REST: `mcpFedAuthMiddleware` / `fedAuthMiddleware` sets the context from the presenting
    token. With A3, the token *is* the actor: admin token → `operator`; session token →
    `session:<id>`; peer token → `peer:<name>`. This is the key enabler: **the credential
    (A) and the audit (C) share the same identity.** No handler changes needed for
    attribution — the middleware stamps it.
  - MCP / bridge: `mcp/server.go` proxies to REST with the bridge token — the bridge
    presents the *session* token (A3), so the REST middleware attributes it to
    `session:<id>` automatically. The MCP SDK context (caller id) is a second line.
  - Comm router / `configure` verb: set `actor = "comm:<channel>"` (or the origin session
    if routed from one).
- **Emit both formats** (AGENT.md rule) on every security-relevant event: config write,
  secret get/set/delete, token mint/rotate/revoke, skill/plugin install, egress-allowlist
  decision (B1), session kill/spawn, `tailscale_acl_push`, file-service write. Both
  `audit.jsonl` (JSONL) and a CEF line (for SIEM) must carry `actor`, `via`, the
  resource, and `request_id` so a single log line answers "who did X."
- **Secret-access event specifically** (HLLM-003 close): `secret_get` logs
  `{"actor":"session:sec-sandbox-18a6","action":"secret_access","resource":"hllm-probe",
  "via":"mcp","request_id":"…","cef":"…"}` — *not* `actor:operator`.

**Acceptance:** for a given `request_id`, the audit line names the exact actor
(operator / session `<id>` / peer `<name>`) and the source surface (rest/mcp/comm).
A session's `secret_get` no longer reads as the operator. (Live re-run of the
`hllm-probe` repro in the sandbox must show `actor: session:<id>`, not `operator`.)

### C2. Config-write integrity (SEC-016, SEC-017)

Two fixes, both small, both blocking the "silent config tamper" class:

- **Audit every config write (SEC-017):** `handlePutConfig` / `applyConfigPatch`
  emits an audit event (C1 context) *per key written*: `{"action":"config_write",
  "key":"server.token","value":<masked-or-length>,"actor":…,"via":…,"request_id":…}`.
  Sensitive keys log the *presence* (`"value":"<set, 32ch>"`), never the value. This
  satisfies the Audit-Logging Rule and gives an operator-visible trace of
  who/what changed which key.
- **Make token rotation atomic + effective (SEC-016):** `PUT /api/config server.token`
  (and `mcp.token`) must **refresh the in-memory gate** the middleware reads, and
  either (a) take effect immediately if `server.auto_restart_on_config` is false via a
  direct `s.token` reassign, or (b) trigger the same controlled restart the operator
  expects. The fix: after `applyConfigPatch`, if the changed key is a *credential
  rotation* key, reassign the Server/MCP token pointers under lock **and** rotate the
  per-session/peer token set so the **new** token is the one accepted and **old**
  tokens from the same *issuer* are revoked (keep the operator's active session
  tokens valid for a grace window or require re-auth). Document that `PUT
  server.token=""` (clear) is rejected unless `allow_empty_token` and a second
  confirmation (prevents a one-line auth-downgrade).
  - The comm `configure` verb routes through the *same* handler, so it inherits both.

**Acceptance (SEC-017):** `PUT /api/config <any key>` produces an audit line naming
the actor + key. **Acceptance (SEC-016):** `PUT server.token=<new>` → old admin token
401s, new token 200s, and the change is audited; a concurrent `allow_empty_token` clear is
rejected without re-confirmation.

### C3. Close the PWA docs-viewer stored-XSS chain (SEC-021)

The finding is a *chain*; fix every link (Security-Fix Downstream-Review Rule):

- **Escape before render (primary fix):** in `internal/server/web/diagrams.js` and the
  `renderDoc` path, run the markdown through `escHtml` **before** `marked.parse` —
  matching what `renderChatMarkdown` already does. This is the direct XSS close (the
  chat path proves the pattern).
- **Don't let file-service writes land in the app source (root fix):** the file-service
  root falls back to `session.root_path` (operator repo) when `file_service_root` is
  empty (`bl333_file_service.go:26-47`). **Default `file_service_root` to a
  data-dir subpath** (`<data_dir>/files`) rather than the repo, so in-repo writes
  require an *explicit* operator opt-in (a config, not a fallback). Keep the traversal
  guard (outside-root ⇒ 404), and add a **deny-list** so file-service never writes into
  `internal/server/web/` or `docs/` even when the root includes them (defense in depth —
  the escape is the source of the render path).
- **CSP:** confirm the docs-viewer is covered by the same nonce CSP; the `renderDoc`
  `innerHTML` must not rely on `'unsafe-inline'`. (v8.8.9 hybrid CSP: keep nonce for
  `<script>`, and ensure `renderDoc` emits no inline handlers — the escape in step 1 is
  what makes the HTML inert.)

**Acceptance (SEC-021):** `POST /api/files/upload` with an in-repo absolute path (e.g.
`docs/plans/<x>.html` containing `<img onerror=…>`) → either refused (default file-service
root is the data dir) or, if written, the PWA docs viewer renders it **inert** (escape).
Re-run the v8.8.9 ZAP re-scan + a manual PWA click-through; the inline-handler regression
that v8.8.4 caused must not recur (the escape approach is the same one that fixed it).

## 3. Alternatives considered

- **C1: tag actor in handler code per call site:** rejected — 361 MCP tools + ~200 routes;
  per-site tagging is what *caused* the gap. Stamping at the auth middleware + credential
  (A) identity is the *structural* fix.
- **C1: log "via: mcp/bridge" only, derive actor later from session-id correlation:**
  works but requires a join to know it was a *session* vs the *operator*. Deriving from the
  presenting token is cleaner and is already available after A3.
- **C2: require a full daemon restart for token rotation:** that is the status quo and is
  the SEC-016 bug (old token stays valid through the restart window + operator must hand-
  restart). A controlled in-place reassign is the fix; a forced restart window is the
  regression.
- **C3: remove `marked` / ship no HTML in docs:** kills a feature (the docs viewer renders
  markdown). Escape-then-parse preserves the feature.

## 4. Phases

1. **C1** `audit.AuditContext` + middleware stamping + both-format emission; re-run the
   `hllm-probe` live repro to confirm `actor: session:<id>`.
2. **C2** config-write audit (per key) + atomic/effective token rotation + reject silent
   clear.
3. **C3** docs-viewer `escHtml` + file-service default root to data dir + deny-list;
   re-run ZAP + PWA click-through.
4. **Docs + tests:** `docs/security-model.md` (audit actor model), 7-surface for
   `allow_empty_token` clear-confirm, config-reference; unit tests: actor stamping from
   each credential type, config-write audit line present, token rotation atomic (old 401 /
   new 200), `escHtml` on renderDoc (XSS payload inert), file-service default root.

## 5. Definition of done

- A session's `secret_get` is audited as `actor: session:<id>` (not `operator`), in both
  JSONL and CEF, keyed by `request_id` (HLLM-003 closed).
- Every `PUT /api/config <key>` (REST + comm `configure`) emits an audit line naming
  actor + key with a masked value (SEC-017 closed).
- `PUT server.token=<new>` → old admin token invalid, new valid, audited; clearing the
  token requires an explicit opt-in + confirmation (SEC-016 closed).
- The PWA docs viewer renders a crafted `<img onerror>` **inertly**; file-service no
  longer writes into the app/docs tree by default (SEC-021 closed).

## 6. Sequencing & risk

- **C1 depends on A3** (needs a distinguishable credential to attribute). Order: A, then C1.
  C2/C3 are independent and can land in parallel with A.
- **Risk (downstream-review rule):** C3 is exactly the v8.8.4 → v8.8.9 failure mode
  (a "hardening" that passes the scanner but breaks the PWA). The `escHtml`-before-parse
  approach is the proven fix, but it must be verified with a **manual PWA click-through**
  (render a real doc with code blocks, a mermaid diagram, and a link) before defaulting —
  not just the ZAP re-pass. C2's token rotation touches the *running* daemon's in-memory
  state; test concurrency (a token rotation mid-session) and the grace/revocation window so
  an active operator session isn't dropped by its own token rotation.
