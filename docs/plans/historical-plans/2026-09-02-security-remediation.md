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

---

## Addendum — Retest of hardening controls (2026-09-04)

**Operator request (2026-09-04):** the register is weighted toward the
*out-of-the-box* (empty-token / default-bind) posture. Re-test the controls that are
actually *available and documented* (encryption, secret scopes, federation CBAC,
container workers) via the howtos/daemon, and correct any finding that a shipped control
already mitigates.

Method: live sandbox on an isolated port (throwaway token, dummy data — production
untouched) + a fresh `--secure` boot with a throwaway passphrase + a code read of the
four control paths. No real secrets; teardown after each.

| # | Control (howto / doc) | Retest result | Effect on register |
|---|---|---|---|
| 1 | **Encryption at rest** (`docs/encryption.md`, `datawatch --secure`) | **WORKS, confirmed live.** `--secure` boot → `/api/health` `encrypted:true`; `config.yaml` = `DWATCH2` (XChaCha20-Poly1305 + Argon2id); a stored secret's value is **not present as plaintext on disk** yet `/api/secrets/{name}` still returns the value to an authed caller. | **Refine SEC-023.** At-rest data *is* protected when the operator opts in to `--secure` — the assessment measured it *only* under the plaintext default, so it overstated the exposure. SEC-023's **real** residual stands (Argon2id t=1/m=64MiB below OWASP; AEAD-error→plaintext fallback in the memory store; no key zeroing) — those are genuine crypto-param/failure-mode bugs independent of `--secure` — but the headline "operator data is on disk in the clear" is **not true once `--secure` is on**. Severity should read MEDIUM as a hardening gap, not an at-rest loss. |
| 2 | **Secret scopes** (`docs/howto/secrets-manager.md` "scoped per-plugin / caller scope") | **ENFORCED ONLY FOR `agent:`/`plugin:` callers — NOT for the LLM session path.** Code: `secrets/scope.go:27` "Operator access (CLI, REST with daemon bearer, **MCP**) is always **unrestricted and never calls CheckScope**"; `CheckScope` gates only `internal/agent`/`internal/plugin` resolution (`secrets.go:67`, `plugins.go:421`). Live: a secret scoped to `agent:other-profile` was still returned in full by `GET /api/secrets/{name}` and the MCP `secret_get`. | **Confirms HLLM-004 (does NOT mitigate).** A hostile LLM session authenticates with the admin/bridge token → it **bypasses scopes entirely**, so scoping a secret does not stop the LLM reading it. Scopes protect *other* operator subsystems (plugins, other agents) from the same secret — they are an *internal* ACL, not a *confined-LLM* boundary. This validates the Design A (per-session scoped token) root and shows scoping **cannot** close HLLM-004/005 on its own. |
| 3 | **Federation CBAC** (`docs/howto/federation-cbac.md`) | **WORKS, confirmed live** (re-run of core T5): a `federation-peer` token gets `403 "federation peer lacks capability: secrets:read"` on `/api/secrets/{name}`; admin passes. **But** `GET /api/federation/peers` still returns raw peer tokens (`token` set, no `token_present`) — re-confirmed live. | **Confirms the T5 PASS / SEC-009 correction (CBAC is a real peer-boundary control).** **Re-confirms SEC-014** (peer-token leak) live. CBAC protects *peers from peers*; it does **not** protect an LLM session (which uses the admin token and skips CBAC) — so CBAC is not a mitigation for HLLM-004/007. |
| 4 | **Container workers** (`docs/howto/container-workers.md`; design doc B "F-2 isolation") | **NOT a stronger security boundary than the local session.** Code: `docker_driver.go:139-145` injects `DATAWATCH_PQC_KEM_PRIV` + `DATAWATCH_PQC_SIGN_PRIV` into the container env; `k8s_driver.go:142-151` injects the same **PQC private keys** into the k8s pod env spec; **no `--cap-drop`/`--user`/`--read-only`/`no-new-privileges`** on the worker container (Docker) and **no `securityContext`** on the worker pod (k8s) — the only `drop:All` is the Tailscale sidecar. Image *is* non-root (`Dockerfile.agent-base:166 USER datawatch` — good). | **Corrects the T3.1 deferral framing.** The plan deferred "container escape" on the basis of *no worker image available* (pull denied). A code-level read shows the container adds **little isolation for the LLM threat model**: the same LLM runs inside, holding the PQC private keys in process env, with caps not dropped and (on k8s) no `readOnlyRootFilesystem`. So "put it in a container" is **weaker than the plan implies** — it is a portability/packaging story, not confinement. Design B's F-2 (gVisor/microVM + fs/net policy + cap drops) is the *only* thing that turns container workers into a boundary; the current container path does not contain the HLLM-006/009 exfil path. |

### Net of the retest

- **The OOTB findings (SEC-001/002, HLLM-001/002) stand and are the most severe** — they
  describe the shipped default, and the operator can be in it without doing anything
  wrong.
- **Encryption (SEC-023 context) was under-stated as a risk, but the *control works*** —
  the finding is a crypto-hardening gap, not plaintext exposure by default-on-disk.
- **Secret scopes + federation CBAC are real controls, but for a different trust boundary
  than the LLM threat model targets.** They gate *operator subsystems and peers*; the
  hostile session reaches the same data with the admin token and neither control fires.
  That is exactly the Design A/B gap — good, but it must not be cited as a mitigation.
- **Container workers are a packaging isolation, not a containment control**, for the
  LLM threat; the design docs are right to file F-2 as the isolation build, but the
  current container path should not be read as "already isolated."

**No findings were dropped.** Three findings get their evidence/state clarified
(SEC-023 severity framing, SEC-014 live re-confirm); two controls (secret scopes,
container workers) are re-classified from "available mitigations" to "available
*packaging / internal-ACL* features that are orthogonal to the confined-LLM threat."
The remediation plan (A/B/C/D) remains the correct fix surface; Design A's
per-session scoped token + Design B's egress/isolation are still the load-bearing
controls for HLLM-001/002/004/005/006/009.
