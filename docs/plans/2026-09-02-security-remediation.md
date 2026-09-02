# Security Remediation — Master Index

- **Date**: 2026-09-02
- **Version at planning**: v8.19.0
- **Status**: **Design complete (4 plans), fixes not yet started.** This is the operator-
  review milestone: every finding from the core (SEC-001…024) + hostile-LLM (HLLM-001…009)
  assessments is mapped to the best fix and a plan doc. **No code changed in this commit** —
  per the operator request, the plans + triage are committed first for review, then
  implementation follows per the phase order below.

## The two underlying defects (~2/3 of findings flow from these)

1. **The admin `server.token` is the only lock, and sessions/peers hold *that* token.**
   → **Design A** (`2026-09-02-sec-design-a-authz-scoping.md`) — fail-closed auth,
   capability opt-*out*, **per-session scoped credentials**, credential redaction.
2. **There is no boundary between a session and the operator it runs as, and no control
   independent of the model's refusal.** → **Design B** (`2026-09-02-sec-design-b-containment.md`)
   — datawatch-side egress control, skill/plugin-verify gate, bridge/Twilio hardening,
   local-trust model (isolation filed as F-2).

Cross-cutting: **Design C** (`2026-09-02-sec-design-c-audit-config.md`) — actor-attributable
audit, config-write integrity, token-rotation atomicity, PWA docs-viewer XSS.
**Design D** (`2026-09-02-sec-design-d-supplychain-crypto.md`) — supply chain, at-rest
crypto, Helm/container defaults.

## Finding → fix → plan

| Finding | Sev | Fix | Plan | Status |
|---|---|---|---|---|
| SEC-001 empty token ⇒ open, 0.0.0.0 bind | CRIT | fail-closed auth + bind-refuse + default bind loopback | A (A1) | planned |
| SEC-002 MCP open when `mcp.token` empty | HIGH | A1 fail-closed (empty `mcp.token` ⇒ require admin + grant surface) | A (A1) | planned |
| SEC-003 per-session bridge unauth | HIGH | per-session bridge token | B (B4) | planned |
| SEC-004 GitHub webhook no `X-Hub-Signature-256` | HIGH | verify HMAC (secret already stored) | B (B4-adjacent, ingress) | planned |
| SEC-005 Twilio sender auth ineffective | MED | Twilio `X-Twilio-Signature` verify | B (B4) | planned |
| SEC-006 query-string token | LOW | header-only token (REST + MCP) | A (A5) | planned |
| SEC-007 WS `CheckOrigin` open / empty token | MED | enforce origin when unauth; A1 fail-closed | A (A1) | planned |
| SEC-008 agent `0.0.0.0:9877` bind | LOW | default `127.0.0.1:9877` | A (A1) | planned |
| SEC-009 federated peer scope | INFO/MED | capability opt-out + peer default-grant tightening | A (A2) | planned |
| SEC-010 cilium/ebpf CVE | MED | *shipped v8.14.1* — re-verify + CI gate | D (D1) | planned (verify) |
| SEC-011 Go 1.26.6 stdlib vulns | MED | *shipped v8.14.1* — re-verify + CI gate | D (D1) | planned (verify) |
| SEC-012 bridge `InsecureSkipVerify` | LOW | fingerprint pinning | B (B4) | planned |
| SEC-013 evals/pipeline `sh -c` from config | HIGH | `evals:write`/`pipeline:write` admin-only (A); LLM-taint path closed | A (A2) + B (B5) | planned |
| SEC-014 peer token leaked in `/api/federation/peers` | HIGH | redact token field (return `token_present` + prefix) | A (A4) | planned |
| SEC-015 `ca.pem` 404 with auto-gen TLS | MED | fallback to data-dir cert | D (D2) | planned |
| SEC-016 token rotation via REST inert | MED | atomic, effective rotation + reject silent clear | C (C2) | planned |
| SEC-017 config writes unaudited | MED | audit every config write (actor + key, masked) | C (C2) | planned |
| SEC-018 discussion throttle map unbounded | LOW | TTL/eviction + maxlen (mirror DNS backend) | D (D3) | planned |
| SEC-019 self-update no checksum/signature | HIGH | SHA-256 + signature verify, refuse-on-fail, keep old binary | D (D4) | planned |
| SEC-020 no cosign/sigstore/GH-provenance | MED | cosign/sigstore block + `cosign sign` on release | D (D4) | planned |
| SEC-021 PWA docs-viewer stored-XSS chain | HIGH | `escHtml`-before-`marked.parse` + file-service default root to data dir + deny-list (app/docs tree) | C (C3) | planned |
| SEC-022 skill `verify` field never executed | MED | execute `verify`; mark unverified; `--trust-unrequired` gate | B (B3) | planned |
| SEC-023 Argon2id < OWASP; AEAD-error→plaintext; no key zeroing | MED | raise params (versioned envelope migration) + key zeroing + refuse-error-path | D (D5) | planned |
| SEC-024 Helm daemon pod no securityContext; `apiToken:""` | LOW | `securityContext` (match observer) + require token / error on empty | D (D6) | planned |
| HLLM-001 admin token in 0644 `.mcp.json` | CRIT | per-session scoped token (A3) + file `0644`→`0600` | A (A3+A4) | planned |
| HLLM-002 session = full-admin principal (uniform 361-tool) | CRIT | per-scoped token + capability opt-out (route cap map) | A (A2+A3) | planned |
| HLLM-003 audit can't tell operator from LLM | HIGH | actor attribution at auth edge (operator / session:<id> / peer) in JSONL + CEF | C (C1) | planned |
| HLLM-004 session reads all secrets/memory/sessions | HIGH | scoped token → secrets/memory gated by namespace (A3); B1 egress closes exfil | A (A3) + B (B1) | planned |
| HLLM-005 `allow_self_config` gate defeatable via REST | HIGH | scoped token can't reach admin REST verbs; `config:all` only if granted + audited | A (A3+A2) | planned |
| HLLM-006 exfil control absent (model-refusal masks gap) | HIGH | datawatch-side egress allowlist (bridge/comm) + `<user_data>` boundary on session input path | B (B1) | planned |
| HLLM-007 session reaches peers / `tailscale_acl_push` | HIGH | capability opt-out + `federation`/`tailscale` admin-only caps (A2) | A (A2) | planned |
| HLLM-008 self-propagation (skill/plugin install + self-update) | HIGH | verify-then-enable + confirm-gate installs (B3); `update` admin-only (B3) | B (B3) + A (A2) | planned |
| HLLM-009 local session = daemon uid, no container | MED (operator call) | **B2.a documented local-trust** (this release); **B2.b isolation = F-2** (separate build) | B (B2) | planned / operator decision |

## Coverage check (every finding has a plan)

- **CRIT (4):** SEC-001, HLLM-001, HLLM-002 → **A** (fail-closed + scoping). All 4 covered.
- **HIGH (13):** SEC-002/003/004/005/013/014/019/021 + HLLM-003/004/005/006/007/008 → A/B/C. All covered.
- **MED (12):** SEC-007/010/011/015/016/017/020/023 + HLLM-009 → A/B/C/D. All covered.
- **LOW (7):** SEC-006/008/012/018/024 → A/B/D. All covered.
- **INFO (1):** SEC-009 → A (A2). Covered.

## Implementation order (recommended)

1. **A** (authz/scoping) — the foundation; C1 (audit actor) and B5 (cap-gated exec sinks)
   depend on A3/A2. Lands as a **minor** (new auth model + scoped tokens).
2. **C** (audit + config integrity + XSS) — depends on A for the actor identity; C2/C3 are
   otherwise independent.
3. **B** (containment) — B1 (egress) independent/high-value; B3/B4/B5 build on A. B2.a
   documented; **B2.b = F-2 (separate build, filed, not blocked here).**
4. **D** (supply chain / crypto / helm) — D4 and D5 ship as pairs (consume↔produce,
   params↔migration). D6 independent, small.
5. **F-2** (isolation) — the large build that fully closes HLLM-009; acceptance bar = this
   design's B section.

## What is *not* in scope here (deferred, operator-directed)

- **F-1** untrusted messaging ingress (allowlist primitive) — core plan §10, still reserved.
- **F-3** per-user RBAC tiers — the single-operator model holds; session scoping (A) is the
  smaller, higher-value slice.
- **HLLM-009 → B2.b** isolation build (F-2) — assessed + acceptance-bared, not built.
- **Operator decisions** this plan surfaces: HLLM-009 (accept local-trust now vs isolate),
  SEC-005 (Twilio sender auth), SEC-019/020 (signature scheme: cosign/sigstore vs pinned GPG).

## Files added in this commit (design only)

- `2026-09-02-security-remediation.md` (this file)
- `2026-09-02-sec-design-a-authz-scoping.md`
- `2026-09-02-sec-design-b-containment.md`
- `2026-09-02-sec-design-c-audit-config.md`
- `2026-09-02-sec-design-d-supplychain-crypto.md`

Both assessment plan docs (`2026-08-28-security-assessment-core.md`,
`2026-08-28-security-assessment-hostile-llm.md`) have their finding-register **status**
columns updated from `open`/`confirmed` → **`planned`** with a link to the fix plan above,
so the register and the plans stay in sync.
