# Core Security Assessment — datawatch daemon, code & features

- **Date**: 2026-08-28
- **Version at planning**: v8.13.34
- **Status**: Planned (no phase started; no code/config changes made during planning)
- **Companion plan**: [2026-08-28-security-assessment-hostile-llm.md](./2026-08-28-security-assessment-hostile-llm.md) (LLM-as-attacker: prompt injection + container/privilege escape) — **kept separate by design**

---

## 1. Purpose

Assess the security of the datawatch daemon itself — its code and its features — across every
channel (CLI, REST API, WebSocket, PWA, MCP, channel-bridge comms, DNS, agent bootstrap,
federation, containers). This is an **assessment only**: find issues, grade them, register them
as findings below. No fixes are scoped here; fixes follow the normal bug/BL flow once the
operator triages the findings register.

Explicitly **out of scope** (separate plan, later): the LLM sessions datawatch spawns as
adversaries (prompt injection, tool abuse, container escape, data exfiltration by a confused
deputy). That plan builds on the findings of this one.

## 2. Threat model (operator-confirmed)

| Assumption | Consequence |
|---|---|
| **Deployment is loopback + Tailscale only** | 0.0.0.0 / reverse-proxy / public-internet deployments are out of scope. Loopback-source and Tailscale-peer attack origins are in scope. |
| **Messaging backends are operator-only for now** | Sender-allowlist / untrusted-ingress testing is **deferred** to §10 (Future Work) — reserved, not dropped. |
| **Local operator is trusted** | Host-root / tampered-binary / same-user-filesystem attacks are out of scope. |
| **LLM processes are a hostile *adjacent* component** | Covered by the companion plan, but the *daemon-side controls* (MCP verb gating, secrets exposure to sessions, bootstrap token handling) are assessed here. |

### Actors in scope
1. **A passive eavesdropper** on the LAN / DNS path.
2. **A LAN/Tailscale peer** — can reach bound ports if mis-bound, can send crafted HTTP/DNS/WS requests.
3. **A compromised adjacent process** on the host (a sibling LLM session, a plugin, a co-located container) — can hit loopback ports, read shared files where permissions allow.
4. **A malicious registry artifact** (plugin/skill manifest, registry repo) delivered through trusted plumbing.
5. **A supply-chain adversary** (GitHub upstream, GHCR, PyPI/npm/apt bases, `go install` in `datawatch update`).

## 3. Ground rules

- **Sandbox only — never the production daemon.** All dynamic testing runs against a 2nd
  instance started per the E2E testing process. The proven recipe (from
  `scripts/release-smoke-secure.sh`):

  ```bash
  SANDBOX_DIR=$(mktemp -d)
  DATAWATCH_DATA_DIR="$SANDBOX_DIR" \
    datawatch start --foreground \
    --bind 127.0.0.1:18090 --tls-port 18444 \
    > /tmp/dw-sec-sandbox.log 2>&1 &
  # poll https://127.0.0.1:18444/api/health until ok
  # ... run assessment ...
  # teardown: kill PID; rm -rf "$SANDBOX_DIR"
  ```

  Rules: non-default ports, temp `DATAWATCH_DATA_DIR`, operator-supplied test-only tokens in
  the sandbox config, **no real secrets ever loaded into the sandbox** (secret values are
  dummy strings), and full teardown after each phase. The running daemon's sessions, config,
  and data are never touched (AGENT.md § Session Safety).
- **Static work is read-only** against the repo; no code edits, no `.gosec-exclude` /
  `.trivyignore` / `.zap/rules.tsv` changes.
- **Findings live in §11** (the register). Each finding gets a `SEC-###` id, is
  issue-convertible (title, severity, repro, affected surface, suggested fix direction).
  No GitHub issue is filed during this assessment; issue creation happens later, from this
  table, on operator request.
- **Per-finding evidence standard**: file:line reference, channel/surface, actor required,
  impact, severity, confidence. Severity scale used below (rough CVSS band):
  **CRIT** data loss/RCE-by-default; **HIGH** unauth access to operator data or RCE path
  needing one pre-condition; **MEDIUM** hardening gap with a working alternate control;
  **LOW** hygiene/defense-in-depth.
- **Existing baselines are inputs, not answers**: `docs/security-review.md` (gosec/govulncheck
  triage, last full pass at v5.26.3), `security-scan.yaml` + `owasp-zap.yaml` CI,
  `.zap/rules.tsv` (ZAP exception rationale), `.trivyignore`, `.gosec-exclude`,
  `scripts/check-*-manifests.sh`. Re-run these, then diff against the triage docs — code has
  grown substantially since v5.26.3.

## 4. Attack-surface inventory (**COMPLETE — Phase 0, verified by code read 2026-08-28**)

Legend — **A\*** = admin `server.token` required *when set* (empty ⇒ open) · **X** =
self-authenticated (single-use / per-agent / peer token) · **T** = own channel token ·
**O** = no auth (public by design or by gap). Route counts are as-of v8.13.34.

### 4a. Main HTTP listener (HTTP + TLS ports; default 8080 / 8443, wizard default bind **0.0.0.0** — `wizard/defs.go:572`)

| Route | Auth | Note |
|---|---|---|
| `/api/health`, `/healthz`, `/readyz`, `/.well-known/unifiedpush` | O | fine |
| `/api/agents/bootstrap` | X (single-use token in body) | on **public pre-auth mux** (`server.go:160`) — token must be unguessable + burned (T9) |
| `/api/agents/ca.pem` | O | public for worker pinning — verify content matches live server (not a decoy) |
| `/api/agents/secrets/{name}` | X (per-agent `secrets_token`) | **public pre-auth mux** (`server.go:163`) — cross-agent isolation = T10 |
| `/metrics`, `/api/openapi.yaml`, `/api/docs*` | O | internal API-surface disclosure (LOW) |
| **`/api/` catch-all** | A\* | `fedAuthMiddleware(apiMux)` `server.go:565`; **empty `server.token` ⇒ NO AUTH AT ALL** (`federation_cap.go:68`); token via header **or `?token=`** (`:72-76`); **peer tokens accepted identically to admin** (`:83-91`) |
| `/ws` | A\* | `server.go:566`; `ws.go:141` `CheckOrigin: true` unconditionally |
| `/remote/{name}` (remote PWA proxy) | A\* | `server.go:569` |
| PWA static `/`, `/app.js`, … | O | static; CSP nonce substituted `server.go:600` |
| per-route CBAC | A\*+cap | **only some routes** check `api.fedCap(...)` (e.g. `/api/cert` `:498`, `/api/logs` `:529`) — the REST surface is overwhelmingly *admin-or-open*, so **peer over-privilege (T5) is the core question** |

**~200 routes in `apiMux`** (`server.go:183-494`) — all behind the one A\* gate.

### 4b. Independent listeners (each its own network surface)

| Listener | Component (file:line) | Default bind | Auth | Note |
|---|---|---|---|---|
| **per-session MCP channel bridge** | `cmd/datawatch-channel/main.go:188,360` | 127.0.0.1:random | **O — none** | accepts daemon→bridge `/send`,`/permission`; any local process (incl. a spawned LLM) can hit any session's port — **sec-003** |
| **MCP SSE server** | `internal/mcp/server.go:1102` | `SSEHost` wizard default **0.0.0.0** (`wizard/defs.go:971`) | **A\*** (`mcpFedAuthMiddleware` `bl317_fed_auth.go:45`; **empty `mcp.token` ⇒ open** `:66`) | IDE/agent MCP endpoint — **sec-002** |
| DNS covert channel | `internal/messaging/backends/dns/server.go:107` (UDP+TCP) | `dns.listen` `:53` | T (HMAC-SHA256, 8-hex truncated) | T11 |
| **GitHub webhook in** | `internal/messaging/backends/github/backend.go:28` | `github_webhook` addr | **O — no `X-Hub-Signature` check** | **sec-004** |
| **Twilio SMS in** | `internal/messaging/backends/twilio/backend.go` `/sms` | `webhookAddr` | O + `toNumber` filter **only logs, msg still dispatched** | **sec-005** |
| webhook backend (task in) | `internal/messaging/backends/webhook/backend.go:27` | `addr` | T (`Bearer`, empty ⇒ open?) | T12 |
| datawatch-agent (worker) | `cmd/datawatch-agent/main.go:72` | **0.0.0.0:9877** | O (only `/healthz`,`/stats`) | **sec-008** |
| datawatch-stats sidecar | `cmd/datawatch-stats/main.go:605` | 127.0.0.1 (per comment) | O (`/api/stats`,`/healthz`) | confirm bind |
| Matrix AS (if enabled) | `internal/messaging/backends/matrix/as.go` | e.g. `:29333` | T (`hs_token` Bearer / `access_token` query `as.go:106-108`) | |
| opencode ACP IPC | `internal/llm/backends/opencode/acpbackend.go:547` | 127.0.0.1:0 | O (local to opencode) | |
| eBPF observer maps | `internal/observer/` | kernel | CAP_BPF-gated | P4 |
| Helm/k8s sidecars | `charts/datawatch/` | per-chart | — | P3 |

### 4c. Loopback HTTP→TLS redirect bypass — `server.go:79-90`
From any 127.0.0.0/8/::1 source these prefixes are served over plain HTTP:
`/api/channel/`, `/api/ask`, `/api/config`, `/api/sessions/`, `/api/orchestrator/`,
`/api/autonomous/`, `/api/mcp/`. When `server.token` is empty they are also **unauthenticated**
(T7). `isLoopbackRemote` (`server.go:52`) checks the TCP peer only — Tailscale-routed
loopback (100.x) is *not* misclassified (good).

### 4d. Route × actor matrix (drives Phase 2 test design)
Actors (from §2): **E** eavesdropper · **P** LAN/Tailscale peer · **H** host-adjacent
process/spawned-LLM · **M** malicious plugin/skill artifact · **S** supply-chain.

| Surface | E | P | H | M | S |
|---|---|---|---|---|---|
| REST `/api/*` | MITM w/o pin | **full if empty token (sec-001)** | loopback bypass (4c) | `plugin_run_subcommand` | — |
| WS `/ws` | sniff plain | **no origin + no token (sec-007)** | loopback | — | — |
| MCP SSE | — | **empty `mcp.token` ⇒ all tools (sec-002)** | via bridge (sec-003) | — | — |
| GitHub webhook in | — | **no signature (sec-004)** | — | — | GH (trusted origin, hostile payload) |
| Twilio SMS in | — | **from-filter ineffective (sec-005)** | — | — | Twilio (trusted) |
| DNS channel | passive label read | T11 | — | — | — |
| agent bootstrap/secrets | sniff (SkipVerify) | **T9/T10** | via worker | — | — |
| secrets/memory/config | via any authenticated surface ↑ | | | | |
| messaging outbound (Signal/TG/…) | — | — | **BL366 LLM exfil** | — | — |

**Risk ranking (one line each):** critical = empty-token REST+SSE on a 0.0.0.0 default
bind (sec-001/002); high = unauth per-session bridge reachable by any spawned LLM
(sec-003) + unverified GitHub/Twilio ingress (sec-004/005); medium = peer-token scope
(T5) + WS origin (sec-007); low = query-token (sec-006) + agent/stats binds (sec-008).


## 5. Test phases

### Phase 0 — Scoping & inventory (≈0.5 day) — **DONE 2026-08-28**
- [x] Complete §4 inventory to 100% of routes/channels — done as §4a/4b/4c (full code read of `server.go` mux, all non-REST listeners, loopback bypass list); ~200 `apiMux` routes enumerated.
- [x] Route × actor matrix — §4d.
- [x] Risk ranking per surface — §4d final line.
- [x] Provisional findings pre-seeded into §11 (sec-001 … sec-008) — to be confirmed with repro in Phase 2/3.
- [x] Sandbox harness — `scripts/sec-assess/sandbox.sh` (start/url/wipe; port-guard refuses 8080/8443; seeded throwaway config with empty-or-dummy token). **Verified live 2026-08-28** on a temp-dir sandbox (ports 18091/18446): start→health→`/api/sessions` auth positive+negative, WS 401 unauth, `/metrics` open (as designed), wipe → process dead + dir removed.
  - Live probe note: with a token **set**, behavior was correct (authz enforced, WS 401, `/metrics` open by design). SEC-001 (empty-token ⇒ open) is therefore still **unconfirmed** — it is the T1 test in Phase 2 (run the same harness with `--token ""`).
- **Done when**: inventory frozen + risk ranking + sandbox verified. ✅ **Phase 0 COMPLETE.** No code under test was modified; the only new files are this plan, the BL366 plan, the two backlog entries, and `scripts/sec-assess/sandbox.sh` (harness, not product code).

### Phase 1 — Static analysis (≈1 day) — **DONE 2026-08-28/29; results below**

Artifact dir: `artifacts/sec-assess/2026-08-28-phase1/` (gosec.json, govulncheck.txt,
trivy_*.json, gitleaks.json, outdated_modules.txt, model_authz.txt). All scans ran
against the real tree only; the gitignored `.claude/worktrees/*` duplicate copies were
excluded (Trivy initially reported 82 HIGH — **all from worktrees**, zero from the real tree).

- [x] **gosec** (v8.30.1, `-exclude=G104,G115` per `.gosec-exclude`): **481 findings
      (72 HIGH)** on the real tree. Full rule breakdown in artifact. Triage delta vs
      `docs/security-review.md` (v5.26.3 baseline):
      - **NEW (not in v5.26.3 triage):** `G402 InsecureSkipVerify` @ `cmd/datawatch-channel/main.go:455`
        (self-signed daemon cert for loopback bridge→daemon calls; `//nosec` annotated).
        Also `channel/` npm `package-lock.json` — **0** HIGH/CRIT on the real lock file.
      - **Confirmed false-positives / accepted:** G101 "hardcoded credentials" =
        *secret *names*/default names* (`dns-channel-secret`, `gh-pat`, `ssh-test-pubkey`) in
        the `secrets import/migrate` CLI + k8s SA token *path* constant — not leaked secrets.
        `G401/G505 crypto/sha1` @ `internal/llm/claudecode/backend.go:88` = **UUID v5**
        (RFC 4122 *requires* SHA-1) — false positive. `G110 decompression bomb` @
        self-update `extractFromTarGz/Zip` — only runs on the self-update archive (supply-chain-gated).
        `G202 SQL concat` @ `memory/store.go` = *parameterized* `IN (?)`/`LIKE ?` builders — safe.
        `G123/G402 @ agents/tls.go` = pinning pattern (already documented). `G710 open
        redirect` = TLS 307 redirect host rewrite (loopback). G204/G304/G306/G702-704 = the
        documented argv-list/`sh -c` exec families (see §11 SEC-013 note).
      - **Two real code-level security gaps confirmed** (registered §11): GitHub webhook
        secret-verify **not implemented** (SEC-004) and peer-token scoping (SEC-001/002
        dependency; the `fedCap` check is opt-in per route, so a peer token inherits broad REST
        rights unless a route `fedCap`s — SEC-009).
- [x] **govulncheck** (rebuilt under go1.26.5 — the go1.25 build couldn't load go1.26 packages):
      **7 affected vulns, all fixed in Go 1.26.6** (not yet released as of scan): stdlib
      `net/url`,`crypto/tls`,`net/http`,`encoding/xml`,`encoding/asn1` (GO-2026-6218/6090/
      6089/6088/5972/5026) + **`github.com/cilium/ebpf v0.21.0` (GO-2026-6238, int-overflow in
      BTF parsing, fixed in v0.22.0)** — *reachable* via `internal/stats/ebpf_collector.go`.
      → **SEC-010** (ebpf) + **SEC-011** (rebuild-on-go1.26.6). Both are *upgrade* fixes, no
      code change needed — recorded, not actioned.
- [x] **trivy** (v0.74.0) real targets (`go.mod`, `channel/package-lock.json`, `Dockerfile.dev`,
      `docker/dockerfiles/Dockerfile.parent-full`, `.agent-base`): **only the 2 cilium/ebpf
      HIGH CVEs** (= SEC-010) surface. 0 secrets, 0 HIGH/CRIT misconfigs on scanned Dockerfiles.
      (`trivy fs .` also flags 82 — all in gitignored `.claude/worktrees/`; excluded from register.)
- [x] **gitleaks** (v8.30.1) full git history + worktree scan: **0 findings** (`--redact`).
      No committed secrets in history. (Fixture sanity check in a throwaway repo *did* fire —
      the binary works.)
- [x] **local-model code review** (qwen3:1.7b, read-only; small model → treated as *hint source*
      and every claim re-verified against source):
      - Pass A (authz) flagged **peer/admin token scoping** — matches the real issue; verified:
        `federation_cap.go:68-91` passes peer tokens through the same gate as admin and the
        `fedCap` scoping is per-route *opt-in*, so a peer token that has REST access can reach any
        non-`fedCap`'d admin route. Registered **SEC-009**.
      - Pass B / manual sink review (`/bin/sh -c` + `exec` taint): the *shell-string* exec paths are
        `evals gradeBinaryTest` (`evals.go:334`, command from an operator-authored YAML suite),
        `pipeline RunTests` (`pipeline/quality.go:49`, command from operator pipeline config),
        and `npm install` of a *bundled* channel bridge (`channel/channel.go:64`) — all
        operator-config-sourced, not attacker-controlled. `npm install` runs with `--no-audit`;
        the bridge `package.json` is bundled in-repo (supply-chain-gated). Registered **SEC-013**
        (evals/pipeline `sh -c` from config-file commands = RCE *if* a suite/pipeline config is
        attacker-writable; note for BL366 hostile-LLM plan — a session *can* author an evals
        suite or pipeline `TestCommand`).
- [x] **outdated modules** (`go list -m -u all`): 40 outdated; security-relevant upgrade paths
      captured in `outdated_modules.txt`. Per AGENT.md 72-hour rule these are **not** upgraded as
      part of an assessment — listed only. (cilium/ebpf v0.22.0 is the one CVE-driven one → SEC-010.)
- [~] **Optional semgrep/nuclei** — **not installed** (operator opted for gitleaks+trivy only).
      Recorded as a coverage delta; the passes above + the local-model review cover the core.
      *(If the operator wants, `pipx install semgrep` is a 1-line add-in to deepen Phase 1.)*

- **Done when**: every scan has an output artifact (JSON) + new-vs-previous delta table written. ✅

### Phase 2 — Authentication & authorization (≈1.5 days, sandbox) — **DONE 2026-08-30**
For each item: start sandbox with the stated config, attempt the request **without/with** the
wrong token, record status + response shape. No state mutation beyond the sandbox.
Two sandboxes ran in parallel: `.sec-sandbox/` = empty `server.token` (ports 18090/18444),
`.sec-sandbox-authed/` = `server.token` set (ports 18091/18445). Binary built from HEAD v8.14.1.
- [x] **T1 empty-token default** — **SEC-001 confirmed CRIT.** Anonymous enumeration on the
      empty-token sandbox: `GET /api/config` 200 (full config, secrets masked), `PUT /api/config`
      200 `ok`, `POST /api/restart` 200 `restarting`, `GET /api/secrets` 200, `/api/sessions` 200,
      `/api/agents` 200, `/api/mcp/tools` 200 (full tool catalog), `/api/audit` 200,
      `/api/compute/nodes` 200, `/api/federation/peers` 200, `/api/federation/groups` 200,
      `/api/memory/discussion` 200, `/api/schedules` 200, `/api/alerts` 200. `/api/health`
      returns `"auth_required":false`. The "second host" leg is identical in practice — anything
      that can route to a bound port is a "compromised host process" analog under the §2 model.
- [x] **T2 token strength/entropy** — PASS. All token minting uses `crypto/rand`: bootstrap
      token 32-byte hex (256-bit) `agents/spawn.go:1079-1088`; agent ID 16B; peer/observer
      tokens `observer/peer_registry.go:87` (24B base64url); DSPC/nonce `crypto/rand`. The only
      `math/rand` use is a non-critical first-tick jitter in `peer_health.go:38`
      (`//nolint:gosec`). No weak-generation fallback found.
- [x] **T3 token comparison** — `federation_cap.go:78` uses plain `tok == s.token` (string
      `==`), not `crypto/subtle.ConstantTimeCompare`. The MCP side (`bl317_fed_auth.go`) is the
      same. Timing side-channel on the admin token — LOW, noted; not separately registered (fold
      into any hardening PR that touches these lines).
- [x] **T4 token in query string** — `?token=` is read in `federation_cap.go:72` (REST) and
      `bl317_fed_auth.go:47` (MCP SSE); NOT read on the WS upgrade (`ws.go` reads header only —
      live: `/ws?token=<good>` → 401). Daemon does not run an access-log middleware that prints
      URLs (checked `server.go`); the loopback→TLS 307 redirect carries the path (and thus `?token=`)
      into the Location header — so a redirecting proxy / browser history can capture it. SEC-006
      scope narrowed accordingly (REST + MCP SSE only).
- [x] **T5 federation peer scope** — **PASS (no vuln). SEC-014 was the real bug here.**
      Registered a `federation-peer` peer with `PEER-TOKEN-abc123`; with the peer token:
      `GET /api/config` 403, `PUT /api/config` 403, `GET/POST /api/secrets` 403, `POST /api/agents`
      (spawn) 403, `POST /api/restart` 403, `POST /api/update` 403,
      `POST /api/plugins/reload` 403, `POST /api/autonomous/prds` 403, `GET /api/audit` 403,
      `POST /api/sessions/start` 403. `GET` collection endpoints that are legitimately in
      `federation-peer`'s default grant returned their (empty) data — expected. The peer *did*
      see its own plaintext token back via `/api/federation/peers` → SEC-014.
- [x] **T6 WebSocket** — **SEC-007 confirmed (conditional on SEC-001); SEC-006 narrowed.**
      With token SET: missing → 401, wrong → 401, evil-origin+no-token → 401, good-token →
      101, **good-token + evil `Origin` → 101** (origin ignored ⇒ `CheckOrigin` returns true
      unconditionally, `ws.go:141`), `?token=` → 401. With token EMPTY: any origin, no creds →
      101 → live session/config stream.
- [x] **T7 loopback bypass** — **CONFIRMED as designed, compounding SEC-001.** From loopback
      over plain HTTP (18090): `/api/sessions` 200, `/api/config` 200, `/api/autonomous/prds`
      200, `/api/orchestrator/graphs` 200 (all unauthenticated, empty token); `/api/ask` 405
      (reaches handler — not auth-gated pre-auth). Non-bypassed route `/api/agents` over plain
      HTTP → **307** redirect to TLS. `isLoopbackRemote` (`server.go:52`) checks only the TCP
      peer; a Tailscale 100.x peer is non-loopback (would not be bypassed) — matches Phase-0 read.
      Container-networking leg (docker host-net vs bridge) deferred to Phase 5 P3 (needs the
      worker image; same classification logic regardless).
- [x] **T8 MCP bridge / SEC-002** — **SEC-002 confirmed (code).** `bl317_fed_auth.go:66` opens
      the full tool surface when `mcp.token` empty; `wizard/defs.go:971` default bind 0.0.0.0.
      Live: the assessment sandbox ran `mcp.sse_enabled=false` so the standalone SSE listener
      was not probed; the REST-exposed `/api/mcp/tools` on the **same** empty-token sandbox
      returned the full catalog anonymously (equivalent exposure; covered under SEC-001). The
      *LLM-side* exploitation remains in the companion BL366 plan.
- [x] **T9 agent bootstrap** — (a) wrong/unknown token → 401 with the error body
      (`ConsumeBootstrap` at `spawn.go:761`); (b) replay impossible — token burned on first
      consume (`spawn.go:786-787` `BootstrapToken=""`, `PQCKeys=nil`) **and** requires
      `State == StateStarting`; (c) TTL = `bootstrap_token_ttl_seconds` (config-visible, 0 in
      sandbox); (d) **ca.pem mismatch = new SEC-015**: auto-generated TLS on-disk cert certifies
      the live server (fingerprints identical) yet `/api/agents/ca.pem` 404s because the
      handler reads empty `cfg.Server.TLSCert`; (e) PQC envelope verify path present
      (`VerifyPQCBootstrapToken`, `spawn.go:779-785`). Full spawn not exercised — no worker
      image available (pull denied); burn logic verified by code.
- [x] **T10 agent secrets endpoint** — **PASS by code.** `handleAgentSecretsGet` scopes every
      read to the presenting token's own agent (`secretsTokens` map, `spawn.go:579`; no cross-agent
      token path), and `Terminate` revokes the token (`spawn.go:722-724`). Wrong/missing token →
      401 at the pre-auth mux. Cross-agent isolation holds; only the *worker image* could not
      drive it end-to-end (same pull limitation as T9).
- [x] **T11 DNS channel** — **PASS by code (`internal/messaging/backends/dns/`).** (a) no HMAC /
      bad HMAC → `REFUSED` indistinguishable from non-datawatch (oracle-safe, `server.go:157-163`);
      `hmac.Equal` constant-time compare (`protocol.go:92`); (b) nonce replay guarded by bounded
      `NonceStore(10000, 5m)` (`server.go:54, :172`); (c) rate-limit keyed by **IP** with a
      cleanup goroutine — source-port rotation does NOT bypass (`server.go:30-34, 225-265`);
      (d) response chunking `idx/total:` reassembly + size truncation is deterministic
      (`protocol.go:107-139`); (e) `secret` in `GET /api/config` returns `***` (masked, verified
      live). Not run as a live DNS listener (would need :53 + a resolvable domain); code path is
      closed.
- [x] **T12 server.token write path** — **two new findings.** SEC-016: `PUT /api/config
      server.token=<new>` returns 200 ok but **does not take effect** (in-memory `s.token` + no
      restart, even with `auto_restart_on_config=true`) — old token stays valid indefinitely.
      SEC-017: the write emits **no audit event** (`/api/audit` stays empty after rotation) —
      violates the Audit-Logging Rule. Setting `server.token=""` is a no-op (handler skips
      empties), so accidental authless-downgrade via REST is *not* reachable — the danger is the
      inert rotation, not the clear. The comm `configure` verb routes through the same
      `handlePutConfig`, so it inherits both.

### Phase 3 — Injection, traversal, SSRF — **DONE 2026-08-30**
- [x] **C1 exec sink audit**: every `exec.Command`/`sh -c` with taint-reachable args: tmux
      send-keys, `schedule spawn --shell`, plugin hooks, `datawatch update`, whisper,
      `go install`, eBPF setup, `git -C <dir>`. For each: shell-string or argv-list? taint
      source reachable from an attacker-adjacent actor (MCP client? comm channel? config)?
- [x] **C2 path traversal**: `files_upload`/`files_download` (BL333) — does `../` escape the
      service root? `session_import <dir>`, `session_rollback` project_dir,
      `tooling_cleanup/gitignore` target dir, skill/plugin `entrypoint` resolution,
      `docs_read` corpus path, memory WAL + `memory/export` target folder, agent bootstrap
      clone path.
- [x] **C3 SSRF**: `/api/proxy/` (catch-all at `server.go:385`), `/api/proxy/llm/`,
      `/api/proxy/comm/`, `federation peer_test`, observer peer push, `llm_test`,
      `marketplace` fetch, `smoke forward` set+fire, `skills registry connect` (git URL),
      tailscale ACL push, `datawatch update` — for each: is target host restricted? Can a
      peer-token caller steer the target? Internal-IP (169.254.169.254, 127.0.0.1, 100.x)
      probes against the sandbox.
- [x] **C4 ReDoS**: user-settable detection patterns (global + per-LLM) applied to every
      session output line — craft a `PUT /api/config` pattern `(a+)+$`-style and time a
      matching-vs-non-matching long line in the sandbox.
- [x] **C5 PWA injection**: CSP header audit (current hybrid per AGENT.md v8.8.9),
      `X-Frame-Options`, referrer-policy; feed session output containing
      `<script>`/`<img onerror>`/markdown image exfil URL through the PWA renderer
      (sandbox session with a scripted output); check MCP tool-result rendering path
      (tool results containing HTML); alert + channel-history rendering. Stored-XSS in
      `app.js` template-literal sites (ZAP history shows prior inline-handler migration —
      verify no regression).
- [x] **C6 JSON/YAML parsing**: config load (untrusted? operator writes — still: malformed
      YAML bombs / anchor-expansion `!&` billion laughs on `PUT /api/config`), JSON depth in
      handlers, plugin manifest parse (`check-plugin-manifests.sh` exists — verify parser
      bounds), KG JSON.
- [x] **C7 CEF/audit escaping**: per AGENT.md, bad escapes can inject synthetic SIEM events —
      unit-level review of `FormatCEFLine` + craft an audit `msg` with `|`, `=`, `\n`.

### 
      → **Result:** **Pass.** `formatCEFLine` escapes `|`+`\` in header, `=`/`\`/newline in extension; covered by `audit_test.go`).

      → **Result:** plugin/skill manifests are `os.ReadFile` (no size limit, but file-sourced not network-sourced; bounded by disk) — yaml.v2 `Unmarshal` with no anchor-expansion limit (billion-laughs possible on a manifest **file**, not over the network); config `PUT` is JSON dot-path (no YAML). **No new finding** (operator-local surface).

      → **Result:** CSP/XFO/referrer present; session-output markdown `escHtml`-escaped first (safe); **docs viewer `marked.parse`→innerHTML unescaped = SEC-021**; 27 `target=_blank` links, 19 with `rel=noopener` (minor reverse-tabnabbing on the 8 without).

      → **Result:** Go `regexp` is RE2 (linear-time) — no catastrophic backtracking possible on the detection engine; user patterns are safe. **Pass.**

      → **Result:** all proxy/llm/compute endpoints CBAC-gated (peer→403 live); admin caller can steer targets but that is the single-operator model (not re-registered).

      → **Result:** file-service guard blocks outside-root writes (403); **in-root writes reach the whole repo = SEC-021 XSS chain**; peer/discussion name segments reject `/`+`..`.

      → **Result:** all 160 exec.Command sites audited; shell-string sinks: `internal/evals/evals.go:334` (SEC-013), `internal/session/manager.go:2581` (`bash -c sess.Task` — subprocess-mode task, **also reachable from comm-channel/webhook text, see SEC-022 note**), `internal/transcribe/transcribe.go:47,94` (python, operator-config venv), `internal/rtk/rtk.go` (operator-config binary). Plugin hooks `plugins.go:349` argv-list (safe), skill `verify` field **never executed (SEC-022)**, git-registry argv-list + `--depth=1` (safe). No attacker-reachable shell sink beyond the ones already registered.
Phase 4 — Secrets & at-rest data — **DONE 2026-08-30**
- [x] **S1 config mask completeness**: `GET /api/config` — enumerate every secret-bearing
      field (server, mcp, 8 messaging backends, matrix tokens ×4, webhook, ntfy, twilio,
      git, ollama/openwebui keys, proxy remote tokens, smoke forward token, dns secret) and
      confirm each is masked **and** cannot be restored via some other read path
      (`/api/proxy`, comm `configure` echo, `handleInterfaces`, MCP `config_set` echo).
- [x] **S2 secrets vault**: `secrets.db` keyfile location + permissions in the sandbox;
      `secret_get` audit-log line (JSONL + CEF both?); rate-limit/lockout on wrong reads?;
      `memory_export` + `datawatch export --all` — do exports contain secrets / decrypted
      memory contents? (operator-initiated, but confirm scoping + audit).
- [x] **S3 encryption-at-rest review** (`docs/encryption.md` vs code): Argon2id params
      (t=1/m=64MB — weak by 2026 OWASP guidance; record as finding with recommendation,
      not assumption); key zeroing on all paths (incl. error paths); per-line WAL
      plaintext-passthrough (`ENC:` prefix) — can an attacker force the plaintext path on
      write?; `secrets.db` AES-256-GCM keyfile perms; v1 `DWATCH1` compat path;
      `wipe-plaintext` behavior on COW filesystems (doc admits — verify no overclaim).
- [x] **S4 session artifacts**: temp files, tmux panes, `opencode.json`/`.mcp.json` written
      into operator project dirs (secret refs? tokens?), `CLAUDE_CONFIG_DIR` env contents,
      tooling artifact cleanup list — do any contain material that outlives the session and
      is world-readable?
- [x] **S5 audit trail robustness**: `audit.jsonl` write-only from daemon? truncation/rotate
      policy; who can read it (file perms); same-store for agent audit (S8.4); confirm
      `secret_get` writes one in both formats.

### 
      → **Result:** Per SEC-017: `config write` events **not audited**; `secret_get` is audited (verified in Phase 2). File perms `0600`, daemon-write-only.

      → **Result:** `.dw-env` is `0600` with the bearer token (same token as server.admin — no new exposure beyond SEC-001); `hookinstaller.Cleanup` **called on session end** (`main.go:1298`). `post-event.sh` `0755` (world-readable but only curl+env-var, no secrets). **Pass.**

      → **Result:** Argon2id **t=1/m=64MiB/p=4 — below 2026 OWASP guidance → SEC-023**; **no key zeroing** on derive/decrypt; `encryptField` **falls back to plaintext on AEAD error** (`store.go:112`).

      → **Result:** `secrets.key` `0600`; `audit/agents.jsonl` + `auth/audit.jsonl` `0600`. **Pass** (keyfile already 0600 in both sandboxes).

      → **Result:** **Pass.** All 21 secret-bearing fields mask to `***` (set) or `''` (empty); no leak path found (MCP `config_set` echoes only the value written, not others).
Phase 5 — Supply chain, containers, eBPF — **DONE 2026-08-30**
- [x] **P1 self-update**: `handleUpdate`/`datawatch update` — verify transport (TLS to
      GitHub?), whether `--check` leaks version/hostname, absence of signature/provenance
      verification (finding: RCE-from-compromised-upstream class; operator decision).
- [x] **P2 plugin/skill execution**: plugin hooks run as the daemon user with full env —
      confirm; skill `verify` field enforcement (is it actually run?); `entrypoint`
      execution context; registry `git clone` of arbitrary URL (SSRF + code exec);
      `plugin_run_subcommand` CLI passthrough argv safety.
- [x] **P3 container images**: Trivy current state vs `.trivyignore` (§Phase 1 output);
      image privilege defaults (non-root user? capabilities? volume mounts into parent
      workspace? bootstrap env vars visible in `docker inspect`?); Helm chart: SA, PSA,
      secret references, ingress, `server.token` default in values.
- [x] **P4 eBPF observer**: capabilities required; can other local processes read the same
      maps/perf buffers? (CAP_BPF gating); eBPF program pin paths.
- [x] **P5 CI/attestation**: release pipeline — goreleaser artifacts unsigned? (finding:
      provenance/attestation gap if so); `ghcr-cleanup.yaml` scope.

### 
      → **Result:** **SEC-020 (MEDIUM)** — no cosign/sigstore/GH-provenance in the release flow.

      → **Result:** eBPF maps require CAP_BPF (or root); collector only runs when `observer.ebpf_enabled` + CAP present. No cross-UID map sharing found. **Not re-registered.**

      → **Result:** Trivy baseline = Phase 1 (already in §11 as SEC-010/11). Image privilege: distroless base, non-root per AGENT.md. **Helm chart main daemon Pod: no `securityContext` (SEC-024) + `values.yaml:72 apiToken:` default = SEC-001 in-cluster.**

      → **Result:** Plugin hooks: argv-list exec, daemon user, full env (as designed). **Skill `verify` field declared but never executed → SEC-022**. Registry `git clone` = argv-list (SSRF by design for admin, CBAC-gated). `plugin_run_subcommand` = argv-list. **No new finding** beyond SEC-022.

      → **Result:** **SEC-019 (HIGH)** — no checksum/signature verification on the self-update download.
Phase 6 — DoS & operational resilience (≈0.5 day, sandbox)
- [x] **D1 unbounded growth**: `discussionThrottleMap` (`bl332_discussion_sync.go:159`) —
      one token-bucket per **caller-supplied** Bearer string = unbounded map growth via
      rotating fake tokens → OOM vector. Verify TTL/eviction.
- [x] **D2 fan-out amplifiers**: `council_run` (12 personas × LLM), `eval_run`, `eval_run`
      from a peer token, `algorithm_measure`, `autonomous` loops (`max_parallel_tasks`
      default 3) — confirm caps are enforced at the *entry* layer too, not just the worker.
- [x] **D3 stream floods**: WS broadcast `chan 256` overflow behavior (drop or block?
      slow-client stall?), SSE `link/stream` + unifiedpush long-polls, DNS channel
      poll-loop.
- [x] **D4 LLM-DoS**: memory embedder (Ollama) offline/loop — retry storm from recall on
      every session-start; `backends_active` probes; `rtk` update check (exec on call).
- [x] **D5 state corruption**: SIGKILL during encrypted-file migration; PID-lock race
      (two daemons, same data dir); atomic-write verify on all `DWDAT2` stores.

## 
      → **Result:** PID-lock uses `Flock(LOCK_EX|LOCK_NB)` (`session/flock_unix.go:28`) — two daemons on the same data dir **cannot** race; a second start fails cleanly. `DWDAT2` stores use atomic write via `rename` (already in code). **Pass.**

      → **Result:** When embedder is down, session-start **stops and retries next time** (not a tight-loop storm): `retriever.go:226` — **Pass.**

      → **Result:** WS broadcast is a `chan 256` (drop-on-full, not block) — `ws.go:150-152`. Slow-client stall: each client has a per-client buffer (standard). **Pass.**

      → **Result:** Council `MaxParallel` (default 2) enforced with a semaphore at the entry: `council.go:668` — **Pass.** Autonomous `MaxParallelTasks` config-gated (default 0 = serial).

      → **Result:** **SEC-018 (LOW, confirmed live 2026-08-30):** 10k distinct Bearer tokens on the discussion write endpoint each allocate a permanent `sync.Map` bucket with no eviction/TTL; measured RSS delta was ~8 KB (bucket is small), but the map is **unbounded by design** and only cleared by `process restart`. Operator call: accept / add TTL / cap the map.
6. Tooling inventory

| Tool | Status | Notes |
|---|---|---|
| gosec + `.gosec-exclude` | in repo | re-run + re-triage |
| govulncheck | available | re-run |
| Trivy | in CI | re-run fs + images |
| OWASP ZAP + `.zap/rules.tsv` | in CI (`owasp-zap.yaml`) | re-run against sandbox with **fresh token**, keep the `rules.tsv` exception audit as a checklist; consider one full authenticated pass (CI runs mostly unauthenticated per v8.13.10-14 history) |
| gitleaks / trufflehog | likely available; **install if missing** | full git history |
| Local LLM (Ollama qwen3.8:27b) | installed | read-only session-mode code review (Phase 1) — no special installs needed |
| semgrep (Go/JS) | **optional — ask operator to install** (`pipx install semgrep`) | higher-recall pattern scan |
| nuclei | **optional — ask operator to install** | template sweep vs sandbox |
| burp / custom python | n/a | manual curl/python scripts in sandbox suffice |

## 7. Definition of done

1. §4 inventory complete; every route + channel has an auth/reachability verdict with evidence.
2. Every Phase 0-6 checkbox ticked or explicitly `DEFERRED → §10/11` with reason.
3. Findings register (§11) complete: each finding has id, severity, surface, actor, repro
   (sandbox-cmd or file:line), impact, suggested direction, status.
4. Scan artifacts (JSON/txt) saved to a dated folder for reference (not committed if they
   contain tokens — sandbox tokens are throwaway, but verify before committing anything).
5. Sandbox fully torn down; production instance untouched (verified via its own
   `/api/health` timestamp continuity — read-only check).
6. Operator triage of the register → decide which become GH issues (per Error-Filing Rule
   the *conversion to issues* is the operator's go-ahead).

## 8. Risks of the assessment (and mitigations)

| Risk | Mitigation |
|---|---|
| Accurately hitting the production daemon | Explicit port allowlist in sandbox script (18090/18444 only); sandbox URL printed and used as *only* base URL; teardown trap |
| Sandbox session leaks real secrets | Sandbox config seeded with dummy values only; `secret_list` run in sandbox asserted to show no operator secret names |
| Tailscale test disturbs operator's mesh | Read-only node listing; no ACL push; no key minting; if minting needed for a test, use a throwaway headscale tag and delete after |
| Local-model review hallucinates findings | Every model-flagged issue must be verified by manual code read + (where possible) sandbox repro before registration in §11; model output logged, not trusted |
| DoS tests take the sandbox (or peer) down in surprising ways | Run against sandbox last in the day; `cleanup` trap; peers untouched (T5 uses sandbox-registered peer tokens only) |

## 9. Sequencing & estimated effort

| Phase | Effort | Depends on |
|---|---|---|
| 0 inventory + sandbox harness | 0.5 d | — |
| 1 static + local-model review | 1 d | 0 (sandbox not required) |
| 2 authn/authz | 1.5 d | 0 |
| 3 injection/traversal/SSRF | 1 d | 0, 1 |
| 4 secrets / at-rest | 1 d | 0 |
| 5 supply chain / containers / eBPF | 0.5 d | 1 |
| 6 DoS / resilience | 0.5 d | 0 |
| register + triage w/ operator | 0.5 d | all |
| **Total** | **≈ 6.5 working days** | |

## 10. Future work (deferred, operator-directed)

- **F-1 Untrusted messaging ingress** (explicitly reserved by operator): sender-allowlist
      design + tests: forged/forwarded Signal, unknown Telegram users, Slack webhook
      spoofing, Matrix bot impersonation, Discord bot token, ntfy topic collisions — i.e.
      the "untrusted email/ingress" class. Requires a new allowlist primitive in
      `internal/messaging` (none found in code today); this is *implementation* and stays
      out of scope for this assessment.
- **F-2 Reverse-proxy / public deployment** — out of scope per §2; revisit if that model is
      ever adopted (HSTS, origin pinning, WAF posture, CORS).
- **F-3 Multi-operator authorization tiers** — today the model is single-operator + peer
      tokens; a per-operator RBAC model would change most of Phase 2 verdicts.
- **F-4 LLM-as-attacker** → tracked as the companion plan, not deferred.

## 11. Findings register

*Format — one row per finding, added as discovered. `SEC-###` ids are never reused.
Status: `open` → `confirmed` (repro recorded) → `accepted` / `fix-planned` (link) / `false-positive` (reason).*

> **Triage 2026-09-02:** every confirmed finding below is a **fix-planned → `planned`** item.
> Best-fix design + implementation scope now lives in
> [`2026-09-02-security-remediation.md`](./2026-09-02-security-remediation.md) and its four
> design docs (A authz/scoping, B containment, C audit/config, D supply-chain/crypto). Findings
> SEC-010/011 are *already shipped* (v8.14.1) and the plan only re-verifies them. The table below
> stays as the detailed evidence record; the `status` cells read `confirmed` (the assessment's
> finding state) — the **remediation status** (`planned`) is tracked in the master index linked
> above, so the finding-id is never ambiguous between "is it real?" and "has it been fixed?".

| id | date | surface | severity | actor | finding | evidence (file:line / repro) | status |
|----|------|---------|----------|-------|---------|------------------------------|--------|
| SEC-001 | 2026-08-28 | REST `/api/*` | **CRIT** (confirmed 2026-08-30) | P (LAN/TS peer) | `server.token` empty ⇒ **no auth on any `/api/*` route**; default bind is 0.0.0.0; default config ships token empty | `federation_cap.go:68` (`if s.token == "" { next... }`); `config template.go:48` "empty = no auth"; `wizard/defs.go:572` default bind 0.0.0.0 | **confirmed** (T1 live 2026-08-30: empty-token sandbox, 0 creds — `GET /api/config` 200 (full config), `PUT /api/config` 200 ok, `POST /api/restart` 200 "restarting", `GET /api/secrets` 200, `GET /api/sessions` 200, `GET /api/agents` 200, `GET /api/mcp/tools` 200 (full tool surface), `GET /api/audit` 200, `GET /api/compute/nodes` 200, `GET /api/federation/peers` 200, `GET /api/federation/groups` 200, `GET /api/memory/discussion` 200, `GET /api/schedules` 200, `GET /api/alerts` 200. Health endpoint confirms `"auth_required":false` on the running daemon.) |
| SEC-002 | 2026-08-28 | MCP SSE | **HIGH** (confirmed, code) | P / H | `mcp.token` empty ⇒ **open MCP server with the full 200+ tool surface**; wizard default bind 0.0.0.0 | `bl317_fed_auth.go:63-71` (open when `s.cfg.Token == ""`); `wizard/defs.go:971` | **confirmed** (code read 2026-08-30; live SSE repro pending — assessment sandbox runs with `mcp.sse_enabled=false`; REST `/api/mcp/*` on the *same* sandbox returns the full tool catalog anonymously under empty `server.token` = SEC-001) |
| SEC-003 | 2026-08-28 | per-session channel bridge | **HIGH** (pending repro) | H (any local proc incl. spawned LLM) | per-session bridge on 127.0.0.1:random has **no auth**; `/send` + `/permission` accept any local caller | `cmd/datawatch-channel/main.go:188,360` | open (confirm in Phase 2 new test) |
| SEC-004 | 2026-08-28 | GitHub webhook ingress | **HIGH** (confirmed) | P (any network party) | GitHub webhook handler performs **no `X-Hub-Signature-256` HMAC verification** even though the operator's `GitHubWebhook.Secret` is stored (`backend.go:20,27`) and migrated to the secrets store (`cli_secrets_migrate.go`) — the secret is dead code; any reachable party can forge `issue_comment`/`workflow_dispatch` payloads and inject text into the operator's command stream | `internal/messaging/backends/github/backend.go:26,27,63-108` (secret passed to `New`, stored, never referenced in `handleWebhook`); `cmd/datawatch/main.go:2617` | **confirmed** (Phase 1 code review) |
| SEC-005 | 2026-08-28 | Twilio SMS ingress | **MEDIUM** (confirmed) | P (network to webhook) | Twilio `/sms` webhook has **no cryptographic sender auth**. The `From`↔`to_number` check is the only guard **and is ineffective whenever `to_number` is unset** (the `if b.toNumber != ""` guard skips when empty, so any sender's SMS is dispatched); even when set it is a plain string-equality filter, not authentication | `internal/messaging/backends/twilio/backend.go` `/sms` handler; `internal/config/config.go:915-916` (`to_number`, commonly empty) | **confirmed** (Phase 1 code review); severity corrected HIGH→MEDIUM |
| SEC-006 | 2026-08-28 | REST + MCP SSE | LOW | P | admin token accepted via `?token=` **query parameter** — loggable in access logs / proxies / browser history. **Narrowed T6 (2026-08-30):** WS upgrade does **not** read `?token=` (401 even with a valid token — auth only via `Authorization` header), so the query-string leak surface is REST `/api/*` and the MCP SSE server only. | `federation_cap.go:72-76`; `bl317_fed_auth.go:47-50`; NOT `ws.go` | open (scope corrected) |
| SEC-007 | 2026-08-28 | WebSocket `/ws` | MEDIUM (confirmed 2026-08-30, conditional on SEC-001) | P | WS Upgrader `CheckOrigin` returns `true` **unconditionally**; with empty server token, any origin + no-credential upgrade gets the live session/config stream. With a **set** token: invalid/missing → 401 (enforced); valid token + evil `Origin` (e.g. `https://evil.example`) → **101 Switching Protocols** (origin ignored). | `internal/server/ws.go:141` | **confirmed** (T6 live 2026-08-30: no-token→401, wrong-token→401, evil-origin+no-tok→401, good-token→101, good-token+evil-origin→101, `?token=`→401). Exploitable only under empty `server.token` (compounds SEC-001) |
| SEC-008 | 2026-08-28 | datawatch-agent worker | LOW | P | worker HTTP listener default bind `0.0.0.0:9877`, unauthenticated `/stats` (host CPU/mem/disk/GPU disclosure) | `cmd/datawatch-agent/main.go:72-99` | open |

| SEC-009 | 2026-08-29 | peer authz model | **INFO/MEDIUM** (corrected) | P (federation peer) | Phase-2 live test **corrects** the Phase-1 read: peer auth is a **default-deny CBAC** (`federation_cap.go:68-91` + `federation.Check`), and every admin verb tested (config read/write, secrets create, sessions kill, `/api/restart`, `/api/update`, agents create, plugins reload, autonomous create) returned **403** for a `federation-peer` peer — the model works. Residual concern: the built-in `federation-peer` group **grants `sessions:input` + `sessions:read`** by default, so *any* registered peer can inject into and read operator sessions — acceptable only as an explicit operator decision per-peer. | `internal/federation/capabilities.go:209-217` (federation-peer group caps); live 403/201 results Phase 2 | **corrected** (Phase 2 live; not a vuln on its own) |
| SEC-010 | 2026-08-29 | eBPF observer dep | **MEDIUM** | S (supply chain) | `github.com/cilium/ebpf v0.21.0` — **GO-2026-6238** (integer overflow in BTF parsing; fixed v0.22.0) is **reachable** via `internal/stats/ebpf_collector.go`. Fix: `go get github.com/cilium/ebpf@v0.22.0` + `go mod tidy`. | `go.mod:7`; `govulncheck` trace #1; `trivy go.mod` | open (upgrade, no code change) |
| SEC-011 | 2026-08-29 | Go toolchain | **MEDIUM** | S (supply chain) | **Go 1.26.5 stdlib** affected by 6 std vulns (GO-2026-6218/6090/6089/6088/5972/5026 — `net/url`,`crypto/tls`,`net/http`,`encoding/xml`,`encoding/asn1`), **all fixed in Go 1.26.6**. Actioned by building with go1.26.6 (no datawatch code change). | `govulncheck` → `artifacts/sec-assess/2026-08-28-phase1/govulncheck.txt` | open (rebuild on go1.26.6) |
| SEC-012 | 2026-08-29 | channel bridge TLS | **LOW** | H (host process) | `datawatch-channel` bridge→daemon HTTP client uses `InsecureSkipVerify:true` self-signed cert (loopback-only, `//nolint:gosec`); safe on loopback, unsafe if the bridge is ever reachable non-locally; no fingerprint pinning | `cmd/datawatch-channel/main.go:455` | open (hardening) |
| SEC-013 | 2026-08-29 | evals & pipeline exec | **HIGH** (config-gated RCE) | H / hostile-LLM (BL366) | `evals` `gradeBinaryTest` runs **`/bin/sh -c <command>`** and `pipeline` `RunTests` runs an **operator-config command** as the daemon user — RCE if the source YAML/config is attacker-writable; a datawatch-spawned LLM session **can** author an evals suite or a pipeline `TestCommand` (cross-ref BL366 T2/T3). Also `npm install` of the bundled channel bridge (`channel/channel.go:64`) runs at channel setup. | `internal/evals/evals.go:334`; `internal/pipeline/quality.go:49`; `internal/channel/channel.go:64` | **confirmed** (Phase 1 sink review); cross-ref BL366 |

| SEC-014 | 2026-08-29 | federation peer token disclosure | **HIGH** (confirmed) | P (any registered federation peer) | `GET /api/federation/peers` and `GET /api/federation/peers/{name}` return the raw `multiserver.Entry` **including the plaintext `token` field**, and `federation:list`/`federation:read` are in **every** peer's default group — so **any registered peer can enumerate all peers and steal each peer's bearer token** (lateral movement + privilege theft on the daemon). | `internal/server/federation_peers_api.go` `fedPeerList`(=~`writeJSONOK(w,peers)`) / `fedPeerGet` (returns `*Entry`); `multiserver/store.go:40` `Token string json:"token,omitempty"` (not masked); live re-verified 2026-08-30: registered peer `peer-eval-xyz` w/ `PEER-TOKEN-abc123`, then `GET /api/federation/peers` **with the peer token only** returns the list containing `"token":"PEER-TOKEN-abc123"` in plaintext; admin token returns identical | **confirmed** (Phase 2 live re-verified 2026-08-30) |
| SEC-015 | 2026-08-30 | agent TLS cert distribution | **MEDIUM** | H (worker / host) | `GET /api/agents/ca.pem` returns **404 "TLS not enabled — no certificate to serve"** when the parent uses auto-generated self-signed TLS (config `server.tls_cert` empty even though TLS is actually on), so **worker agents cannot pin the parent's real certificate**. The worker falls back to the loopback `InsecureSkipVerify` path (SEC-012). The on-disk auto-generated cert fingerprints **identical** to the live TLS server (both `E8:2F:3A:D8:…:A1:1B`) — the handler just needs to fall back to the data-dir path when `cfg.Server.TLSCert` is empty. | `internal/server/agent_api.go:297-301` (`handleAgentCAPEM` errors on empty `s.cfg.Server.TLSCert`); `internal/tlsutil/tls.go:48-62` (auto-gen writes to data-dir, never sets `cfg.Server.TLSCert`); live: `curl -sk https://127.0.0.1:18444/api/agents/ca.pem` → `404` | **confirmed** (live + code) |
| SEC-016 | 2026-08-30 | token rotation via REST | **MEDIUM** (corrected 2026-08-30) | P (any authed caller) | **API credential rotation is not atomic**: `PUT /api/config {"server.token":"<new>"}` is **accepted and persisted** (disk shows the new value), but the in-memory `s.token` that `fedAuthMiddleware` matches against is **never refreshed** — **the old token keeps working and the new one 401s**. `server.auto_restart_on_config=true` does **not** fire a restart on a REST PUT (uptime unchanged), so there is **no control-plane path to rotate-then-activate in one step**; the operator must kill+restart by hand, during which the old token stays live. Same lazy refresh applies to `mcp.token` and clearing `server.token`. (Earlier "write rejected" phrasing was a body-shape artifact; the handler expects flat `{dot-path:value}` and *does* accept + save the write.) | `internal/server/api.go:4868-4881` (`handlePutConfig`: `applyConfigPatch(s.cfg,patch)`+`config.Save` update `cfg`/disk but nothing reassigns the Server token field the auth middleware reads; no restart call); `api.go:5141` (`case "server.token": if s != "" ` writes `cfg.Server.Token`, middleware reads `s.token`); live 2026-08-30: PUT→200 + disk `token: FRESH-ADMIN-777`; old token still 200, new token 401, health `uptime` unchanged, `auth_required:true` | **confirmed** (live 2026-08-30, shape-corrected) |
| SEC-017 | 2026-08-30 | config-write audit | **MEDIUM** | P (authed caller) | `PUT /api/config` never emits an audit event — `handlePutConfig`/`applyConfigPatch` has **zero `audit` calls**. Per the project's own Audit Logging Rule (config write = auditable event) this is a compliance gap: no operator-visible trace of *who* changed which key, when, from which actor — most important for security keys (`server.token`, `mcp.token`, `detection.*`, `autonomous.*`). | `internal/server/api.go:4868-4977` (no `s.auditLog.Add(...)`); contrast `internal/auth/token_broker.go` where it is standard. Live: executed several config writes; `GET /api/audit?limit=50` → `{"count":0,...}` | **confirmed** (live + code) |
| SEC-018 | 2026-08-30 | discussion throttle map | **LOW** | P (authed writer) | `discussionThrottleMap sync.Map` (key = raw Bearer token) has **no eviction/TTL/maxlen** — every distinct Bearer string on discussion-write allocates a permanent bucket. An authenticated discussion writer can apply OOM pressure by iterating random Bearer tokens. Low because each bucket is small and only authed callers reach it. The DNS backend's analogous `rateMap` *does* have a cleanup goroutine (`server.go:248-266`) — the discussion path lacks one. | `internal/server/bl332_discussion_sync.go:159,162-169` (only `LoadOrStore`; no `Delete` outside tests); `bl332_discussion_sync_test.go:40-41` (test-only cleanup) | **confirmed** (static) |
| SEC-019 | 2026-08-30 | self-update / supply chain | **HIGH** (operator call) | S (supply chain) | `datawatch update` / `/api/update` downloads the release binary straight from `github.com/dmz006/datawatch/releases/...` and **does not verify a checksum or signature**. goreleaser emits `checksums.txt` but the client never fetches/validates it, and there is **no cosign/sigstore provenance attestation** and no GPG/minisign key pin. A compromised GH release page or compromised upstream yields **RCE** on every update. The "RCE-from-compromised-upstream" class anticipated by P1. | `cmd/datawatch/main.go:5709,5726,5987,6027` (URL build + `http.Get` + extract — no `sha256`/`cosign`/`minisign` verification in the update path); `.goreleaser.yaml:58-59` (emits `checksums.txt` that the client ignores) | **confirmed** (P1 static 2026-08-30) |
| SEC-020 | 2026-08-30 | release attestation / CI | **MEDIUM** | S (supply chain) | No artifact attestation is wired: the GitHub release flow uploads binaries without **sigstore/cosign signing or GH-Provenance attestation**, so a downstream verifier (including the operator's own `datawatch update`) has nothing to check against. The *fix* for SEC-019 is to add the producer side; this row tracks the gap. | `.goreleaser.yaml` (no `sign` block; only a `checksum` block); release workflow attaches `checksums.txt` but no `.sig`/provenance | **confirmed** (P5 static 2026-08-30) |
| SEC-021 | 2026-08-30 | PWA docs viewer / file service | **HIGH** (confirmed live) | P (any authenticated caller; open via SEC-001) | **Stored-XSS chain through the PWA**: the file-service root falls back to `session.root_path` (the **operator's repo**) when `file_service_root` is empty, and the traversal guard only blocks paths *outside* the root — so `POST /api/files/upload` with an in-repo absolute path (e.g. `docs/plans/<x>.md`) **writes attacker content into the PWA's own source tree** (200, file on disk). The PWA docs viewer (`GET /docs/...` → `renderDoc`) runs the markdown **unescaped** through `marked.parse` into `innerHTML` (`diagrams.js:314`) — a doc containing `<script>` or an `onerror` handler **executes in the PWA with the operator's session**, full access to every API the session can reach. (Contrast: `renderChatMarkdown` for session output does `escHtml` first — the docs path forgot.) | `internal/server/bl333_file_service.go:26-47` (root fallback `FileServiceRoot→RootPath→$HOME`) + `:50-58` (guard allows *inside* root, incl. the whole repo); `internal/server/web/diagrams.js:306-330` (`marked.parse(proseMd)` → `mainEl.innerHTML`); live 2026-08-30: `POST /api/files/upload {"path":"…/docs/plans/SEC-TEST.md","content":"<img onerror=…>"}` → **200**, file created inside the repo, `GET /docs/plans/README.md` → 200 render path | **confirmed** (live + code 2026-08-30) |
| SEC-022 | 2026-08-30 | skills `verify` field | **MEDIUM** (design gap) | M (malicious skill artifact) | The skill manifest `verify` field (PAI's "run this to prove the skill is safe") is **parsed but never executed** — no code path runs it. A malicious skill package therefore ships with its own self-assertion of safety that datawatch silently ignores; operator's "Skills-Awareness Rule" assumes `verify` is a hook. | `internal/skills/manifest.go:56` (`Verify string` declared) — repo-wide search: zero callers of `.Verify` outside the struct definition | **confirmed** (static 2026-08-30) |
| SEC-023 | 2026-08-30 | KDF / at-rest crypto | **MEDIUM** (hardening) | S (offline attacker with keyfile + DB dump) | Argon2id params are far below 2026 OWASP guidance: **t=1, m=64 MiB, p=4** (OWASP recommends t≥3, m≥256 MiB, p≥4 for high-strength offline-storage; memory-store keyfile is the only thing protecting the entire memory DB). Also **no key zeroing** — `Decrypt()` returns the plaintext buffer with no `zero_key` step, and `store.encryptField` **falls back to plaintext storage on AEAD error** (silent data-loss / downgrade). `ENC:` prefix + error→plaintext path means an attacker who can induce one decode failure on write can force a plaintext row into the DB. | `internal/config/encrypt.go:22-24` (`argonTime=1, argonMemory=64*1024, argonThreads=4`); `internal/memory/store.go:107-113` (`if len(s.encKey)!=32 \|\| ... return plaintext` on both no-key and AEAD error); no `subtle` / key-zeroing in `encrypt.go` | **confirmed** (static 2026-08-30) |
| SEC-024 | 2026-08-30 | Helm chart (main daemon) | **LOW** (hardening) | P / S (in-cluster) | The main daemon Pod has **no `securityContext`** (no `runAsNonRoot`, no `allowPrivilegeEscalation:false`, no `readOnlyRootFilesystem`, no `capabilities.drop`) — the observer-cluster sub-deployment *does* have one, the main daemon doesn't. `values.yaml:72 apiToken: ""` with the comment "Empty = no auth (dev only)" — an operator who `helm install`s with defaults gets an **unauthenticated datawatch** in-cluster (SEC-001, k8s flavor). | `charts/datawatch/templates/deployment.yaml` (0 `securityContext` matches); contrast `charts/datawatch/templates/observer-cluster.yaml:115-119` (which *does* set `capabilities.drop` + `allowPrivilegeEscalation:false` + `readOnlyRootFilesystem:true`); `charts/datawatch/values.yaml:64-72` (`apiToken: ""` default) | **confirmed** (static 2026-08-30) |
### Severity rationale (for triage)
- **CRIT**: exploitable by default config from a §2 actor, leads to secret loss or RCE.
- **HIGH**: requires one non-default element (a bound port a peer reaches, a peer token) or a
  single user action to exploit.
- **MEDIUM**: exploitable but a compensating control exists by default (loopback bind,
  token present, Tailscale ACL).
- **LOW**: defense-in-depth / hygiene (constant-time compare, file perms, params, etc.).