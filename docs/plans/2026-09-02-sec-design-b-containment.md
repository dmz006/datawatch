# Design B — LLM Containment & Confused-Deputy

- **Date**: 2026-09-02
- **Version at planning**: v8.19.0
- **Status**: Planned (not started; design only — no code changes in this commit)
- **Finds this closes**:
  - **Hostile-LLM:** **HLLM-004** (unscoped secrets/memory/sessions), **HLLM-006** (model-refusal masks an absent exfil control), **HLLM-007** (session reaches peers / `tailscale_acl_push`), **HLLM-008** (self-propagation: skill/plugin install + self-update), **HLLM-009** (local session = daemon uid, no container), and the *residual* of **HLLM-005** that A3/A5 do not cover.
  - **Core:** **SEC-003** (per-session channel bridge unauth), **SEC-005** (Twilio sender auth), **SEC-012** (bridge `InsecureSkipVerify`), **SEC-013** (evals/pipeline `sh -c`), **SEC-022** (skill `verify` never executed).
- **Assumes** Design A is in place (scoped session token + cap opt-out). Where A and B
  overlap, B references A; B adds the **content-boundary** and **execution-isolation**
  controls that credential scoping alone does not provide.
- **Design principle**: a session is **a confused deputy, not a principal**. Credential
  scoping (A) stops it reaching admin verbs; **this** plan stops it from (a) acting on
  injected content as if it were instruction, (b) propagating itself, and (c) touching
  host resources it shouldn't.

---

## 1. Why credential scoping alone is not enough

Plan A makes the session's *token* narrow, so `secret_get`/`PUT config`/`tailscale_acl_push`
are 403 **at the boundary**. But three classes remain even with A:

1. **Exfil-by-compliance (HLLM-006):** the *legitimate* session capabilities (read its own
   project files, make one outbound call) are enough — if the session is *instructed by
   injected content* to `Read` `/home/<op>/.datawatch/.../secrets` (a file it *can* read,
   HLLM-009) and `curl` it to a URL it *can* reach. The model's refusal is the only thing
   standing between that file and egress. A weaker model complies. The **control** must be
   datawatch's, not the model's.
2. **Self-propagation (HLLM-008):** even a "scoped" session that is allowed `plugin_install`
   / `skills_registry_connect` / `rtk_update` can persist attacker code for the *operator's
   future* sessions. Credential scope does not remove these tools; it only 403s them if the
   operator hasn't granted them. The operator may grant them. We need the **install path
   verified + confirm-gated**.
3. **Host reach (HLLM-009, SEC-003):** a local session literally *is* the daemon process
   family — same uid, same host fs, same loopback. There is no boundary to scope *against*
   for fs/net. This is the architectural call.

## 2. Design (best fix)

### B1. Content-boundary on the exfil primitive (HLLM-006)

Close the gap between "the model said no" and "a control says no."

- **Egress boundary (datawatch-side, model-independent):** the per-session bridge
  (`cmd/datawatch-channel`) and the comm router are the *only* sanctioned outbound paths
  a session should use. Introduce a **session egress allowlist** in config
  (`session.egress.allow: [hosts/ports]`, default = loopback + `localhost`, i.e. *no
  arbitrary internet egress from a session tool*): 
  - The bridge's `DATAWATCH_API_URL` (loopback) is always allowed.
  - Any other HTTP a session surfaces (e.g. via a `WebFetch`-equivalent MCP tool, or a
    comms send) passes through a **bridge-side filter** that refuses non-allowlist
    destinations unless the operator extended the list.
  - This is the *datawatch control* that stands where model-refusal currently stands.
- **Injection guard already shipped (BL369, v8.18.0)** wraps *autonomous-executor* prompt
  interpolations in `<user_data>` tags. Extend the same primitive to the **session-input
  path**: task text, session-output injection, comm-channel inbound text, and MCP
  tool-result text should carry a *data-not-instruction* boundary when they are fed
  back into an LLM prompt. This is the defense-in-depth for a *weak* model; the egress
  allowlist (above) is the hard control.

**Acceptance:** with the default allowlist, a spawned session cannot `curl` an external
canary even if the model complies with an injected "exfil" instruction (canary empty, and
the bridge logs a *refused* egress line). With the operator extending the allowlist, the
specific destination is permitted and audited (C).

### B2. Execution isolation for local sessions (HLLM-009) — *operator decision*

This is the big fork. Two postures:

- **B2.a — Accept local-trust (default today):** document that a datawatch local session
  runs as the daemon uid with host fs/net reach, so **do not run a hostile model locally
  with access to operator data**; the trust boundary is the *operator's choice of model +
  the A/B1 controls*. Keep A's credential scoping (a session can't reach *other* sessions'
  credentials or admin verbs) as the mitigation for "session X reading session Y."
- **B2.b — Isolate (recommended for the "hostile model" threat model):** run local sessions
  in a **  container or microVM** (gVisor/Firecracker) with: no host fs binds (mount only
  the project dir), no host network (or a controlled egress per B1), non-root, dropped
  caps. This is a **new build**, tracked as **F-2** (container/microVM hardening) and is
  explicitly *out of scope* of Design A. It is the only thing that turns HLLM-009 from "by
  design" into "contained."

**Operator call required:** B2.a vs B2.b. This design ships **B2.a** (documented trust
model) and **B1** (the model-independent egress control) so that even under B2.a the
exfil-by-compliance path is gated. B2.b is filed as F-2 with this design as the acceptance bar.

### B3. Gate the self-propagation surface (HLLM-008, SEC-022)

- **Execute skill `verify`:** today `Manifest.Verify` is parsed but never run (SEC-022).
  Run it (argv-list, not `sh -c`; timeout; as the daemon user in a *scrubbed env*) before
  a skill is enabled. A skill with no `verify` is **flagged `unverified`** in the PWA/CLI/
  comm surfaces and requires an explicit operator `--trust-unverified` to enable. This
  closes the "malicious skill ships a self-assertion of safety datawatch ignores" gap.
- **Confirm-gate `plugin_install` / `skills_registry_connect`:** these already route to
  admin; under A they require the grant. Additionally they must **show what will be
  installed (author, entrypoint, hooks) and require a second confirmation** before executing
  the install (defense against "a session silently installs a plugin that then runs for
  future sessions"). Log the confirm in the audit trail (C).
- **`rtk_update` / `datawatch update` / `tooling_*`** (T3.5): these are host-level self-
  update paths. Under A, a *scoped session token* 403s them (no `update` cap in the session
  default grant). The operator token may perform them; each is audited (C). Add an explicit
  `update` capability and require the admin token — do **not** place `update` in any
  `federation-peer` grant.

**Acceptance:** a session (even with a generous session grant) cannot enable a skill with
no `verify` without `--trust-unverified`; `plugin_install` emits a reviewable diff and a
confirmation step; `rtk_update`/`datawatch update` from a *session* token = 403.

### B4. Channel-bridge & ingress hardening (SEC-003, SEC-005, SEC-012)

- **SEC-003 (bridge unauth):** the per-session bridge listens on `127.0.0.1:random` with
  *no* auth on `/send` + `/permission`. Bind policy (A) already restricts to loopback;
  add a **per-session bridge token** (minted with the session token, A3) that the daemon
  sends and the bridge requires — so a *sibling* local session (or a hostile model) cannot
  hit *another* session's bridge `/send`. Re-verify the daemon is the only `/send` caller.
- **SEC-005 (Twilio sender auth):** when `twilio.enabled`, verify the webhook's Twilio
  signature (`X-Twilio-Signature` HMAC over `auth_token`) — the `from_number`↔`to_number`
  filter is a *poor* auth (and is skipped when `to_number` empty). The signature check is
  the control; the number filter stays as a second gate. Document that an *unauthenticated*
  Twilio endpoint = any network party can inject SMS.
- **SEC-012 (bridge `InsecureSkipVerify`):** the loopback self-signed client uses
  `InsecureSkipVerify:true`. Add **fingerprint pinning** (store the daemon's cert
  fingerprint at setup; verify each bridge→daemon call against it) so the pattern stays
  safe even if the bridge is ever reachable non-locally.

**Acceptance:** with `twilio.enabled`, a forged SMS without a valid `X-Twilio-Signature`
is refused; a sibling session's bridge `/send` from another local process = 401 (bridge
token); bridge→daemon calls verify the pinned fingerprint (fails if the daemon cert changes
without operator re-setup).

### B5. Exec-sink review (SEC-013 — *record, not rebuild*)

Core C1 found the shell-string sinks (`evals.go:334` `gradeBinaryTest`, `pipeline/
quality.go:49` `RunTests`) run **operator-config-file commands** as the daemon user. This
is **operator-authored config**, not attacker-controlled by default; the *threat* is
"an attacker-writable suite/pipeline config." Under A, a scoped session cannot *author*
an evals suite or pipeline `TestCommand` (no `evals:write`/`pipeline:write` in the session
grant), which removes the LLM→sink taint path. **Action:** (1) add `evals:write` and
`pipeline:write` to the cap map as **admin-only**; (2) keep these sinks argv-listed where
the command comes from config; (3) document the residual (a config file an attacker can
already write = RCE, same as any shell — out of scope). No new control beyond A needed.

## 3. Alternatives considered

  - **Block *all* session outbound egress (no allowlist, deny-by-default):** more secure
  but breaks legitimate session work (a session that must `curl` an operator-designated
  internal API). Rejected in favor of an **allowlist default = loopback-only** that the
  operator extends — same security, less breakage.
- **B2: always containerize local sessions (force B2.b):** removes the "daemon uid = host"
  trust assumption, but is a large new build (gVisor/microVM plumbing, fs/net policy,
  volume sync) that changes the *local* session model fundamentally. Rejected as a blocker;
  shipped as the *documented default trust* (B2.a) with the isolated variant filed as F-2,
  gated on B1's egress control for interim safety.
- **Rely on model refusal (status quo for the exfil path):** explicitly rejected — the
  honesty bar of the hostile-LLM assessment (§3) and T1.1 show a weaker model complies;
  refusal is not a control. B1 provides the control.

## 4. Phases

1. **B1** egress allowlist in bridge + comm router; `<user_data>` boundary on
   session-input/comm/MCP-tool-result paths (extend BL369).
2. **B3** skill `verify` execution + `unverified` flag + `--trust-unverified`;
   `plugin_install` / `skills_registry_connect` confirm-gate.
3. **B4** per-session bridge token; Twilio signature verify; bridge fingerprint pinning.
4. **B5** `evals:write`/`pipeline:write` admin-only in the cap map (depends on A2).
5. **B2.a** document the local-trust model (`docs/security-model.md`, `docs/operations.md`);
   file **F-2** (isolation build) with this design as acceptance.
6. **Docs + tests:** config-reference (`session.egress.allow`, `--trust-unverified`),
   7-surface for egress + verify-flag; unit tests: egress-allowlist enforcement (canary
   empty by default), bridge token /send authz, Twilio signature verify, skill-verify-run
   + unverified-gate, `evals:write` admin-only.

## 5. Definition of done

- A spawned session (default grant) cannot exfil to an external canary **even if the
  model complies** with an injected instruction — the bridge refuses non-allowlist egress
  and logs it (HLLM-006 closed).
- A skill with no `verify` cannot be enabled without `--trust-unverified`; it is marked
  `unverified` in every surface (HLLM-008 / SEC-022 closed).
- `plugin_install` / `skills_registry_connect` show what will be installed and require
  confirmation (HLLM-008).
- Sibling-session bridge `/send` cross-access is refused (SEC-003); Twilio endpoint rejects
  forged SMS without a valid signature (SEC-005); bridge→daemon calls verify a pinned
  fingerprint (SEC-012).
- A scoped session token cannot write an evals suite / pipeline `TestCommand` (SEC-013
  LLM-taint path closed by A+B5).
- **Operator decision recorded:** B2.a (documented local-trust) in this release; B2.b
  isolation filed as F-2 with a design + acceptance bar.

## 6. Sequencing & risk

- **B1 is independent and highest-value** (turns "model refused" into "control refused") —
  land it first; it is the fix for the CRIT-adjacent HLLM-006 without needing B2.
- **B2 depends on A** (bridge token from A3; `evals:write`/`pipeline:write` caps from A2)
  and on B1 (the egress control is what makes B2.a safe to ship).
- **B2.b is a separate build** (F-2) — do not block B here; file it.
- **Risk (downstream-review rule):** the egress allowlist must default to *loopback-inclusive*
  so a legitimate session calling an operator-internal API keeps working; test the PWA's
  own loopback calls (they go through the *same* daemon, not the session bridge, but verify),
  and a session that must reach a designated internal host (operator extends allowlist).
  Run release-smoke + a PWA click-through before defaulting the allowlist (v8.8.9 lesson:
  a tightening that passes the scanner but breaks the app is a regression).
