# Security Remediation — Activation Plan (status refresh + first slice)

- **Date**: 2026-09-22
- **Version at planning**: v8.34.1
- **Status**: **Planning only — no code changed, no issues filed.** This revises
  `2026-09-02-security-remediation.md` (the design-complete, fixes-not-started
  milestone) with an 18-day-later status check, folds in two new findings, and
  proposes a first concrete slice sized to actually start. Written at operator
  request: *"iterate on the security findings and plan for enhancing datawatch
  security — no changes, just revising the plan, no actions now."*

## 1. What changed since 2026-09-02 (confirmed today, all read-only checks)

- **Nothing from the remediation plan has been implemented.** `git log --since=2026-09-04
  --all` against every `SEC-0`/`HLLM-0`/design-doc keyword returns zero commits. All 33
  findings (24 core + 9 hostile-LLM) remain exactly `planned` — this is 18 days of a
  fully-designed, reviewed plan sitting idle. That's the headline finding of this refresh:
  the gap isn't design, it's activation.
- **SEC-010/011 (cilium/ebpf, Go stdlib CVEs) hold.** Verified live: `go.mod` shows
  `github.com/cilium/ebpf v0.22.0` and `go 1.26.6` — both at the fixed versions. No
  regression.
- **The 2026-09-04 retest addendum's corrections still apply** — worth restating since
  they change how two findings should be *talked about*, not their fix priority:
  - SEC-023 (crypto params): severity reads as a **hardening gap**, not "operator data sits
    in the clear" — `--secure` mode's `--secure` + Argon2id/XChaCha20 *does* work when the
    operator opts in. The Argon2id params (t=1/m=64MiB, below OWASP) and the AEAD-error→
    plaintext fallback are still real bugs; just not the "no encryption at all" framing.
  - Secret scopes and federation CBAC are **real controls for a different boundary**
    (peer↔peer, plugin↔plugin) — neither one gates the admin-token LLM-session path, so
    neither can be cited as already mitigating HLLM-004/005/007. Design A's per-session
    scoped token is still the only thing that closes that gap.
  - Container workers are **packaging, not containment**, for the LLM threat model — PQC
    private keys land in the container/pod env either way, caps aren't dropped, no
    `securityContext`. F-2 (gVisor/microVM) is still the only real isolation build.

## 2. Two findings to fold in (new since the Sept 2 plan)

| id | surface | severity | finding | maps to |
|---|---|---|---|---|
| SEC-025 | `channel/` bridge npm deps | **HIGH** (4) + **MED** (5) — 9 total, live via `gh api .../dependabot/alerts` | `hono`, `qs`, `fast-uri` — direct deps of the per-session channel bridge (`channel/package.json`) — carry 9 open CVEs: DoS via unbounded dot-notation nesting (`hono` `parseBody`), incomplete path-traversal fix in `toSSG()`, query-parser fragment/cache-key differential, `qs` array-limit/isBuffer DoS, and **3 SSRF-class `fast-uri` bugs** (IDN/percent-decoding host confusion, IPv6 normalization). All are version-bump fixes, no code change. Notable: `hono` is the HTTP framework serving the bridge's `/send`+`/permission` endpoints — the *exact* component SEC-003 already flags as unauthenticated on loopback, so these CVEs stack on an already-open finding rather than standing alone. | **B4** (bumps ride along with the per-session bridge token work; independently doable today as a plain `npm update` with no design dependency) |
| SEC-026 | CI hygiene | INFO | `gosec baseline-diff` job annotation (v8.34.1 run): "live count 20 BELOW baseline 60 — lower the baseline so regressions get caught at the new bar." The allowed-findings ceiling is 3x the actual current count — a real regression could land 40 findings above today's baseline before CI would ever flag it. | **D** (or standalone — zero design dependency, one-line CI config change) |

Coverage-check update: HIGH count goes 13→14 (+SEC-025's severity, taking the worse of its
9 sub-CVEs), MED stays effectively the same band (+SEC-026 is INFO, doesn't move the count).
No other new findings surfaced in a CHANGELOG/README/`.trivyignore`/`.gosec-exclude`/
`.zap/rules.tsv` scan back to 2026-08-30 — the suppression files gained no new entries in
that window (checked via `git log` on each file), so nothing was quietly excluded instead of
fixed.

## 3. Why 18 days of inertia — and the actual unlock

The Sept 2 plan's own "Implementation order" is still the right shape (A→C→B→D→F-2), but it
sequences by *design*, not by *shippable unit* — "Design A" is a new auth model, per-session
scoped credentials, and capability opt-out in one bucket, which reads like a multi-week
rewrite before a single PR lands. That's very plausibly why nothing shipped: there's no
"first PR" in the plan, only "first design."

**The unlock is picking the smallest slice that (a) needs zero new design work — the fix
is already fully specified in Design A/B/C/D — and (b) closes or meaningfully narrows a
CRIT/HIGH finding on its own, without waiting on the rest of its design bucket.**

## 4. Proposed first slice (ready to act on — awaiting go-ahead, not started)

Ordered by (severity closed) ÷ (design dependency), cheapest-and-highest-value first:

1. **SEC-026 — lower gosec baseline 60→~25.** Zero design dependency, one CI config line,
   zero code risk. Ships same day it's approved. Pure hygiene, but it's the fastest way to
   stop *new* findings from hiding inside slack in the existing ceiling.
2. **SEC-025 — `npm update` in `channel/`** for `hono`/`qs`/`fast-uri` to patched versions.
   Zero design dependency (Design B4's per-session-token work is unrelated to *which
   version* of the HTTP framework the bridge runs). Pure dependency bump + re-run the
   bridge's existing test suite. Closes 9 open dependabot alerts in one PR.
3. **SEC-014 — redact peer token in `/api/federation/peers`.** Design A4's smallest piece:
   change the response shape from raw `token` to `token_present: bool` + a short prefix.
   No auth-model change, no new credential type — just stop echoing a live secret back to
   every peer that can already list peers. HIGH severity, closes on its own, ships as a
   single small PR ahead of the rest of A.
4. **SEC-004 — verify `X-Hub-Signature-256` on the GitHub webhook.** The secret is *already
   stored and migrated* (per the finding) — this is "add the `hmac.Equal` check that reads
   the secret already sitting there," not a new secrets-handling path. HIGH, self-contained,
   no dependency on Design A/B being done first.
5. **SEC-021 — `escHtml` before `marked.parse` in the PWA docs viewer.** One-line change
   mirroring the pattern `renderChatMarkdown` already uses for session output (the finding's
   own note: "the docs path forgot"). Closes the stored-XSS chain's *render* side
   immediately; the file-service root-fallback side (writing into the repo tree) is the
   second half and can follow as a separate PR without blocking this one.
6. **SEC-016/017 — token rotation atomicity + config-write audit.** Slightly larger (touches
   `handlePutConfig`), but both are Design C2's smallest coherent unit and don't need A or B
   first. Landing them together makes sense (same handler, same PR).

Everything past this point (the rest of Design A — fail-closed auth + scoped tokens +
capability opt-out; Design B's egress control and F-2 isolation; the remaining D items) is
correctly sequenced in the Sept 2 plan as "after the foundation" and doesn't need
re-litigating here — items 1-6 above are specifically the ones that don't need to wait for
that foundation and were likely just never separated out as their own PRs.

## 5. What this plan does *not* decide (operator calls, restated from Sept 2, still open)

- **HLLM-009**: accept local-trust (LLM sessions run as daemon uid, no container boundary)
  for now, vs. commit to the F-2 isolation build.
- **SEC-005**: what "Twilio sender auth" should actually require (signature verify is the
  fix; whether Twilio SMS ingress is worth keeping at all is separate).
- **SEC-019/020**: signature scheme for self-update + releases — cosign/sigstore vs. a
  pinned GPG/minisign key. Both work; picking one is the only remaining decision, not more
  design.

## 6. Definition of "ready to act on"

This doc is that milestone for items 1-6 above: each has a cited fix, a file:line starting
point (from the Aug 28 assessment or this doc), zero remaining design work, and no
dependency on a bigger bucket landing first. Nothing here has been implemented. Next step,
on operator go-ahead, is normal BL flow: file GH issues for 1-6 (or however many the
operator wants to start with), then implement in the order above.

## 7. Walkthrough decisions (running log, started 2026-09-23)

Findings are reviewed one at a time with the operator; discussion only, no code until the plan is final. Every change carries the standing requirement: 1:1 unit + smoke tests, howto/docs/definitions updates where the surface changed, and e2e test-flow updates. Target: **v9.0.0** (a real major release) after the plan is built, tested, and the full e2e run passes.

| id | decision |
|---|---|
| SEC-001 | No forced fail-closed; an empty `server.token` stays a legitimate operator choice. Verified in code: when `server.token` is set, REST and WS enforce it, and the loopback HTTP bypass skips only the TLS redirect, not auth. Work: docs only (how to turn auth on; loopback is still enforced). The PWA already shows a token-entry prompt when `auth_required` is true. |
| SEC-002 | **Reopened 2026-09-24.** An earlier note here said MCP SSE reuses `server.token`; that was wrong. `mcp.Server.cfg` is `*config.MCPConfig`, so MCP SSE is gated by a separate `mcp.token` ("empty = no auth", `internal/config/template.go:66`) and nothing copies `server.token` into it. With `server.token` set and `mcp.token` empty, MCP SSE (200+ tools) is open even though REST is protected. Options to decide: inherit `server.token` when `mcp.token` is empty, plus a startup warning and a per-listener auth flag in health/diagnose. |
| SEC-003 | Per-session bridge token minted at spawn, passed to the bridge by env, checked on `/send` and `/permission`. |
| SEC-004 | Keep the GitHub webhook; verify `X-Hub-Signature-256` with the stored secret, constant-time compare. |
| SEC-005 | Keep Twilio (product feature, unused today); verify `X-Twilio-Signature`; require `to_number`. Shared `internal/messaging/webhookauth` helpers, same 401 + log shape as GitHub. Slack/Discord/ntfy connect outbound (no exposure); Matrix AS already verifies. |
| SEC-006 | Remove `?token=` from REST and MCP SSE, and from `docs/api/openapi.yaml`. One unified replacement for the three browser-API cases that cannot send headers: file download/view links, the Signal-link `EventSource`, and the main WS connection (`buildWsUrl`). Short-lived single-purpose nonces for downloads/EventSource; token carried in `Sec-WebSocket-Protocol` (or a nonce) for WS. Design still to be written. No Go-side internal callers use `?token=` (verified). Fold in the constant-time admin-token compare (assessment T3) in `fedAuthMiddleware` and the MCP middleware, since the same lines change. |
| SEC-007 | Same-origin `CheckOrigin` (Origin host must equal request Host); single-origin deployment, no allowlist. Check non-browser WS clients that send no Origin before finalizing. |
| SEC-008 | Delete the legacy `cmd/datawatch-agent` (not built by goreleaser, untouched since v3.7.0). |
| SEC-009 | Default `federation-peer` group becomes `health:read` + new `federation:self` only, with new `GET /api/federation/peers/self`; everything else opt-in via custom groups. Breaking change, called out in the v9.0.0 upgrade notes. |
| SEC-010/011 | Already shipped (cilium/ebpf v0.22.0, Go 1.26.6); re-verified 2026-09-22. |
| SEC-012 | Proposed, awaiting explicit confirmation: pin the daemon cert fingerprint in the bridge via a new `internal/tlspin` package and `DATAWATCH_TLS_FINGERPRINT`, implemented together with SEC-015. |
| SEC-013 | Do not rewrite the shell sinks. Closed through the scoped per-session tokens (Design A3: session tokens exclude `autonomous:write` and `config:write`), with a test proving it. Document those two caps as host-command-grade. Audit every `test_command` change (ties to SEC-017). Fix `pipeline.RunTests`: honor the ignored `timeout` via a context, parse args with shell-word splitting instead of `strings.Fields`. Optional: env allowlist for evals graders instead of the whole daemon environment. Evals keep `/bin/sh -c` (`INPUT`/`EXPECTED` already pass as env vars). |
| SEC-025 | Remove the legacy Node channel bridge (`channel/` source, embedded JS fallback, the `npm install` path). The native Go bridge is the live path. Closes the 9 dependabot alerts and the runtime `npm install` supply-chain exposure (unpinned `@modelcontextprotocol/sdk ^1.10.0`, no lockfile, install scripts). Correction to the earlier write-up: `hono` is only an `overrides` pin for MCP SDK transitives, not the bridge's HTTP framework. Keep the `LegacyJSArtifacts` cleanup for one release so old installs tidy up. |
| SEC-014 | Redact at serialization: every outbound `multiserver.Entry` drops `token` and gains `token_present`. Covers `GET/POST/PUT /api/federation/peers[/{name}]`, `GET /api/servers[/{name}]` (that token is the credential used to reach the operator's other daemons), and the MCP tools `federation_peer_list/get` and `server_list/get`. Applies to the admin token too; create and update responses do not echo it. The update handler already merges, so redacted round-trips are safe. Constant-time compare in `GetByToken`. Storing inbound peer tokens hashed is deferred (needs a migration; the store is 0600 and encrypted under `--secure`). Tests over every surface; `openapi.yaml` schema change; howto on rotating a peer token now that it cannot be read back. TS-565/577/618 only send tokens, so they are unaffected (TS-565 also asserts default caps, which SEC-009 changes). |
| SEC-015 | **Proposed; walkthrough paused here 2026-09-24, operator answers pending.** In the default config (`tls_auto_generate: true`, `tls_cert` empty) the startup fingerprint is skipped (`cmd/datawatch/main.go` around line 1806 fingerprints only a configured `tls_cert`), so spawned workers get no `DATAWATCH_PARENT_CERT_FINGERPRINT` and bootstrap with `InsecureSkipVerify` (`internal/agents/client.go:131` and `:268`), with no warning; `/api/agents/ca.pem` also 404s (`internal/server/agent_api.go:302`). Proposal: one `tlsutil` resolver for the certificate in use (configured or auto-generated at `DataDir/tls/server/`), used by the ca.pem handler, the worker-pin fingerprint and the SEC-012 bridge pin; loud startup warning plus a `diagnose` line; worker fails closed on an https parent with no pin unless an explicit opt-out env var is set (breaking, v9.0.0). Tests: resolver (configured, auto-generated, disabled, no regeneration), served ca.pem fingerprint equals the live listener's, driver env injection, worker pin match/mismatch/no-pin/opt-out; e2e sandbox uses auto-generated TLS so it can assert ca.pem returns 200. Docs: how the pin is derived, how to check it, the opt-out. |
| SEC-016 | Live token update, a dedicated rotate endpoint, and a howto. |

## 8. Queued for 9.0.0 (raised during the walkthrough)

- **PRD session visibility.** (a) Deploy the last-activity indicator: it is in v8.34.1 but not on the operator's running daemon; restart only after the running PRD finishes, because boot-resume resets `planning` PRDs to draft. (b) Diagnose the missing scrollback in the tmux view for `opencode run` sessions. (c) Test a throwaway PRD with `opencode-acp` for planning and execution (verify decompose and verifier completion detection); if it works, make ACP the PRD default (chat view, history, structured state); fallback is a transcript view built from `opencode run --format json`. (d) File the Android parity issue once the shape is settled.
  **Result of (b)/(c), 2026-09-26.** (b) `opencode run` sessions have real tmux scrollback; the PWA wrongly used the client-side frame ring for them (fixed: only task-less interactive opencode TUI sessions use it). (c) ACP planning works (output-file based; needs a model that follows the planning prompt). ACP execution hung because opencode never sends `session.completed`, unmapped events (heartbeats, `message.updated`) resurrected idle sessions, and the 15 s silence fallback flipped busy sessions to waiting; all three fixed (one-shot ACP sessions complete on their first working turn going idle; ACP silence window 5 min) and verified end to end with a real session. Remaining before ACP can be the PRD default: server-side chat transcript persistence and an endpoint (history is browser-only today and lost on reload); an end-to-end PRD run with a model strong enough to follow the planning and task prompts.
- **HLLM-001/002/008** (admin token in container `.mcp.json`, session as full-admin principal, self-propagation): review at the HLLM section.
- **HLLM-009 / Design B2, pulled forward.** Observed on PRD `ef9dcac1`: the session browsed outside its `project_dir`; the global opencode `"permission": "allow"` neutralises `external_directory`. Candidate fix: a scoped permission block in the generated per-PRD `opencode.json` with per-PRD `allowed_dirs` (read-only vs writable). Best effort only; real confinement is F-2.
- **Capacity-aware Automata admission and queueing (BL389).** Verified gap: PRDs do not observe each other, nothing makes them wait for capacity, and hitting `session.max_sessions` fails the task; declared compute-node capacity is stored but never enforced. Plan: [`2026-09-24-bl389-capacity-admission.md`](2026-09-24-bl389-capacity-admission.md), three phases, Phase 1 is a small standalone fix. Targeted v9.0.0.
- **CI loose end.** The v8.34.x CI-monitoring agent died on a rate limit mid-fix; v8.34.1 published, confirm all its checks are green.

## See also

- [`2026-08-28-security-assessment-core.md`](2026-08-28-security-assessment-core.md) — the 24-finding register (SEC-001…024), full evidence.
- [`2026-08-28-security-assessment-hostile-llm.md`](2026-08-28-security-assessment-hostile-llm.md) — the 9-finding LLM-as-attacker register (HLLM-001…009).
- [`2026-09-02-security-remediation.md`](2026-09-02-security-remediation.md) — the master fix-design index (A/B/C/D) this doc refreshes.
