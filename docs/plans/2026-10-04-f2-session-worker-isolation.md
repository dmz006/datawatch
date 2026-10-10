# F-2 — LLM-Session Worker Isolation & Sandboxing Controls

- **Date**: 2026-10-04
- **Version at planning**: v8.39.16
- **Status**: Planned (design only; no code changes in this commit)
- **Closes**: HLLM-009 (`docs/plans/historical-plans/2026-08-28-security-assessment-hostile-llm.md` §13) — specifically Design B's **B2.b** tier, filed there as a separate build and left undesigned until now.
- **Overlaps** (lands in the same PRs, not duplicated elsewhere): SEC-024's Helm `securityContext` work (`docs/plans/historical-plans/2026-09-02-sec-design-d-supplychain-crypto.md` §D6) — the daemon pod and the worker pod need the same hardening shape, write it once.
- **Reuses, does not rebuild**: the SEC-012/015 TLS-pinning mechanism (`internal/agents/tls.go` `PinnedTLSConfig`, `DATAWATCH_PARENT_CERT_FINGERPRINT`) — already correct when it fires; F-2 doesn't touch it.
- **Informs downstream work** (per operator direction — this doc is written first because it changes assumptions in the others):
  - **Design A** (`2026-09-02-sec-design-a-authz-scoping.md`) — A3's per-session scoped credential is minted at spawn time, same moment F-2's sandbox tier is selected and the PQC bootstrap exchange happens. §6 below specifies how these two spawn-time mechanisms share one flow instead of landing as two unrelated changes to the same code path.
  - **Design B** (`2026-09-02-sec-design-b-containment.md`) — B1's egress allowlist (HLLM-006, decided: loopback-only default, named one-flag categories for Tailscale/federation-peers/compute-nodes) gets a **second, stronger enforcement point** here: a real sandbox boundary plus a generated NetworkPolicy is harder to bypass than bridge-side filtering alone, because it holds even if the bridge itself is compromised. §7 below.
  - **Design C** (`2026-09-02-sec-design-c-audit-config.md`) — new audit-event classes (sandbox tier selected, runtime-capability-probe result, PQC bootstrap exchange) need the same `audit.AuditContext` actor-stamping C1 builds; this doc defines the event shapes, C1's middleware stamps them.
  - **Design D** (`2026-09-02-sec-design-d-supplychain-crypto.md`) — SEC-024's Helm `securityContext` (§D6) is the same manifest template this doc hardens for the worker pod; one PR, not two.

---

## 1. What's being closed, validated against current code (2026-10-04)

HLLM-009's original finding (2026-09-01) and the Design B retest addendum (2026-09-04) both concluded "container workers are packaging, not containment, for the LLM threat model." Re-validated against today's source (v8.39.16, not the v8.18.0/v8.19.0 the assessments were written against) — **nothing has changed**:

| Gap | Evidence (file:line, current source) |
|---|---|
| No container-level confinement, Docker | `internal/agents/docker_driver.go` `Spawn` — plain `docker run -d`, zero `--cap-drop`/`--read-only`/`--user`/`--security-opt no-new-privileges`. Default bridge network. |
| No container-level confinement, k8s | `internal/agents/k8s_driver.go` `podManifest` — zero `securityContext` block on the worker container. The **Tailscale sidecar** gets one, but it *adds* `NET_ADMIN`+`SYS_MODULE` rather than dropping anything (`k8s_driver.go:188-194`). |
| Non-root is an image default, not an enforced policy | `docker/dockerfiles/Dockerfile.agent-base:166` `USER datawatch` (uid 10001) — real, but overridable at `docker run --user root` or in a pod spec; nothing in either driver pins it at the runtime-policy level. |
| PQC bootstrap keys are plain env vars | `internal/agents/spawn.go:543-547` mints the keypair; `docker_driver.go:135-146` and `k8s_driver.go:142-151` inject `DATAWATCH_PQC_KEM_PRIV`/`DATAWATCH_PQC_SIGN_PRIV` as plaintext `-e`/pod-env — visible via `docker inspect` / `kubectl get pod -o yaml` to anything with read access, for the container's entire lifetime. |
| Resource limits are opt-in and Docker-side is silently ignored | `internal/profile/cluster.go` `ClusterProfile.DefaultResources` only renders into the k8s pod template when set; `docker_driver.go` never reads `Resources` at all — a **separate, pre-existing bug**, fixed as part of this work since it's the same code path. |
| Network policy is a hook with nothing behind it | `ClusterProfile.NetworkPolicyRef` — "names a pre-existing NetworkPolicy... empty = no isolation (default)." Datawatch never generates or enforces one; Docker has no equivalent concept at all. |
| The worker's MCP/bridge surface was mis-documented | `docs/howto/container-workers.md` describes the worker as "a smaller-surface companion to the full daemon." **This is incorrect** — `Dockerfile.agent-base:159,174-175` copies the full `datawatch` + `datawatch-channel` binaries and runs `start --foreground` in bootstrap mode. A container worker has the **same** ~390-tool MCP catalog as a local session (per HLLM-002), reached over the Tailscale mesh instead of loopback. Fixed in §8 (docs). This matters here specifically: confinement **must** happen at the container/pod boundary this doc builds — it cannot be assumed away by "the worker has a smaller surface," because it doesn't. |

The chart already contains a correct template to crib the *shape* from: `charts/datawatch/templates/observer-cluster.yaml:114-119` (the unrelated eBPF/DCGM sidecar) has `capabilities.drop:[ALL]`, `allowPrivilegeEscalation:false`, `readOnlyRootFilesystem:true` — proving the authors know the pattern. It was just never applied to the LLM-worker pod. The worker's ideal block is the *inverse* of the observer's (the observer needs `BPF`/`PERFMON` caps; the worker needs none).

## 2. Decisions made (operator, this session — not re-litigated here)

| # | Decision |
|---|---|
| D1 | Harden the existing `runc` path immediately (cap-drop, non-root enforcement, read-only-root, resource-limit fix, PQC-key-delivery fix) — ships with no new runtime dependency, closes real exposure today regardless of what follows. |
| D2 | Add a pluggable per-`ClusterProfile` `sandbox_runtime` field: `""`/`runc` (hardened default) · `runsc` (gVisor) · `kata` (Kata Containers). |
| D3 | gVisor and Kata land **together, in the same release** (not phased) — operator's own host has strong KVM/nested-virt support, so Kata's host-prerequisite risk is low here; the capability-probe gate (§6.3) still exists in code for portability to hosts that don't. |
| D4 | PQC bootstrap keys move into the `POST /api/agents/bootstrap` **response body** — never pre-seeded into the container/pod spec at all, closing the exposure class structurally rather than practically. |
| D5 | HLLM-006's egress allowlist default is loopback-only with named one-flag categories (Tailscale mesh / federation peers / compute nodes) — reused here as the shape for F-2's auto-generated default-deny NetworkPolicy (§7). |

## 3. Design principles (AGENT.md / DATAWATCH-CONTEXT.md, applied)

- **Configuration Accessibility Rule**: `sandbox_runtime` is a `ClusterProfile` field, not a global config key, so it follows the Profiles feature's own parity surfaces, not the generic 6-channel `handleGetConfig`/`handlePutConfig` pattern. Verified today's actual surface (§9) rather than assuming.
- **Mobile-Parity Rule**: a new field in the PWA's Cluster Profiles settings card (`app.js` `gc_clusterprofiles` section) is an affordance addition (trigger #4) — files a `datawatch-app` issue per §9.
- **Audit Logging Rule**: every spawn emits sandbox-tier-selected, capability-probe-result, and PQC-bootstrap-exchange events, in both JSONL and CEF, through the same `audit.AuditContext` Design C1 introduces.
- **Security-Fix Downstream-Review Rule**: hardening a container's filesystem/capabilities can break legitimate in-session tool use (package installs, compilers writing temp files) exactly the way the v8.8.4→v8.8.9 CSP tightening broke inline handlers. §6.1 specifies the writable-path allowlist explicitly rather than shipping a blanket `--read-only` and finding out what broke after the fact.
- **Testing Tracker Rule**: every new interface needs both a unit/protocol test *and* a live-connection test. For F-2 that means not just asserting the right flags appear in rendered Docker args / pod manifests, but actually spawning a container under each tier and confirming the policy is *enforced*, not just *specified*. §10.
- **No-local-environment-leaks**: gVisor/Kata install instructions in docs use example registry/host values, never this operator's actual infra.

## 4. Scope

- `internal/agents/docker_driver.go` — hardening flags, `sandbox_runtime` → `--runtime`, capability probe, resource-limit fix.
- `internal/agents/k8s_driver.go` — `securityContext` block, `sandbox_runtime` → `RuntimeClassName`, capability probe, NetworkPolicy generation.
- `internal/agents/spawn.go` — PQC key delivery moves from pre-seed to bootstrap-response.
- `internal/agents/client.go` — worker-side: read PQC keys from the bootstrap response instead of env.
- `internal/profile/cluster.go` — new `SandboxRuntime` field.
- `internal/server/profile_api.go`, `internal/mcp/*` (profile tools), `internal/server/web/app.js` (Cluster Profiles card) — expose the new field (§9).
- `charts/datawatch/templates/deployment.yaml` — daemon pod `securityContext` (SEC-024, landed in this PR).
- `docker/dockerfiles/Dockerfile.agent-base` — no change expected (already non-root); re-verified as part of this work, not assumed.
- `docs/howto/container-workers.md`, `docs/security-model.md`, `docs/config-reference.yaml`, `CHANGELOG.md`.
- `tests/integration/spawn_docker.sh`, `tests/integration/spawn_k8s.sh`, new `scripts/test-stories/TS-785.sh` onward (§10).

## 5. Phases

1. **F2.1 — Harden `runc`** (D1): cap-drop, non-root enforcement, read-only-root + writable-path allowlist, resource-limit fix (Docker side), PQC-key-delivery fix (D4). No new dependency; ships alone if needed.
2. **F2.2 — Pluggable `sandbox_runtime`** (D2, D3): `runsc` + `kata` added to both drivers together, capability probe gating `kata` (and, defensively, `runsc`).
3. **F2.3 — Network policy at the sandbox boundary** (reinforces HLLM-006/D5): auto-generated default-deny NetworkPolicy per worker when `NetworkPolicyRef` is unset; Docker-side equivalent via a dedicated per-worker bridge + firewall rules.
4. **F2.4 — SEC-024 Helm hardening** (same PR as F2.1's k8s changes): daemon pod `securityContext`, `apiToken` render-error-on-empty.
5. **Docs + tests** (§8, §10): throughout, not deferred to the end.

## 6. F2.1 — Hardening detail

### 6.1 Writable-path allowlist (not a blanket `--read-only`)

A session legitimately needs to write: its project directory (`/workspace/<repo>`, already a mounted volume — stays writable), a scratch/tmp area (`/tmp`, becomes a `tmpfs` mount under the read-only root), and whatever a package manager or compiler the *operator's own tooling* needs (e.g. a Go build cache, `node_modules`, pip cache) — all of which already live under the project directory in normal usage, so they're covered by the workspace mount without a separate allowlist entry. Everything else (`/`, `/usr`, `/etc`, the daemon binary's own install path) becomes read-only. This must be verified against a **real session** running a real build (`go build`, `npm install`) inside a hardened container before shipping — per the Downstream-Review Rule, not assumed from the design alone (see §10's live-connection test).

- Docker: `--read-only --tmpfs /tmp:rw,noexec,nosuid,size=512m` plus the existing workspace volume mount (already writable, unaffected).
- k8s: `readOnlyRootFilesystem: true` on the container `securityContext`, an `emptyDir` volume mounted at `/tmp`, workspace `PersistentVolumeClaim`/`emptyDir` mount unchanged.

### 6.2 Capability + non-root enforcement

- Docker: `--cap-drop=ALL --security-opt=no-new-privileges --user 10001:10001` (matches `Dockerfile.agent-base`'s `USER datawatch`; explicit flag is defense-in-depth against a spec that overrides `USER`).
- k8s pod + container `securityContext`: `runAsNonRoot: true`, `runAsUser: 10001`, `allowPrivilegeEscalation: false`, `capabilities: {drop: [ALL]}`, `seccompProfile: {type: RuntimeDefault}` — same shape as `observer-cluster.yaml:114-119`, inverted (no caps added, since the worker needs none).
- Resource limits: Docker driver gains the `--memory`/`--cpus` flags from `ClusterProfile.DefaultResources` that the k8s driver already has but Docker currently ignores entirely — closing that pre-existing, separate gap in the same commit since it's the same `Spawn` function.

### 6.3 Capability probe (gates `kata`, defensively also `runsc`)

Before honoring a non-default `sandbox_runtime`, the daemon checks availability **before** attempting a real spawn, and fails with a specific, actionable error rather than a cryptic container-create failure:

- Docker: parse `docker info --format '{{json .Runtimes}}'` for the configured runtime name.
- k8s: `GET /apis/node.k8s.io/v1/runtimeclasses/<name>` via the existing k8s client — 404 means "not installed," not "spawn and see."

Error surfaces through the same path a failed spawn already uses (`agent_api.go`/`spawn.go` error returns) — no new error-handling pattern. Datawatch does **not** install gVisor/Kata itself; `docs/howto/container-workers.md` documents the host/cluster prerequisite (containerd shim, `RuntimeClass` object) explicitly, with example (not this operator's real) registry/host values.

## 7. F2.3 — Network policy (reinforces HLLM-006)

gVisor and Kata workers are still ordinary pods from the CNI's perspective, so a real k8s `NetworkPolicy` applies regardless of which runtime is selected. Today, `ClusterProfile.NetworkPolicyRef` is "a hook with nothing behind it" — empty means no isolation. This changes the default:

- When `NetworkPolicyRef` is unset, the k8s driver **generates** a minimal default-deny `NetworkPolicy` for the worker's pod, matching HLLM-006's decided default (loopback-equivalent egress only — in k8s terms, egress restricted to the daemon's own Service, DNS, and nothing else) plus the same named one-flag categories (Tailscale mesh / federation peers / compute nodes) as the bridge-side allowlist, so extending one extends both consistently rather than drifting into two separate allow-lists that can disagree.
- Docker has no NetworkPolicy equivalent; the Docker driver creates a **dedicated bridge network per worker** (rather than the shared default bridge today) with firewall rules mirroring the same allow-list shape. This is a real, separate piece of new scope — flagged explicitly rather than assumed trivial, and is the one part of this doc most likely to need its own follow-up iteration once implementation starts surfacing edge cases (e.g. Docker Desktop vs native dockerd firewall-rule mechanics differ).
- This is additive to, not a replacement for, Design B1's bridge-side egress filter — the sandbox network boundary holds even if the bridge process itself were somehow compromised, which bridge-side filtering alone cannot claim.

## 8. Documentation

- **`docs/howto/container-workers.md`** — correct the "smaller-surface companion" claim (§1); document `sandbox_runtime`, its three values, host/cluster prerequisites (not installed by datawatch), the capability-probe error shape, and the new default-deny NetworkPolicy behavior.
- **`docs/security-model.md`** — add the F-2 section: local-trust (B2.a, already documented) vs. sandboxed (F-2) vs. what specifically closes HLLM-009 and to what degree (gVisor = syscall-interception boundary; Kata = hardware-virtualized kernel boundary; neither claims to be unbreakable, both are real security upgrades over today's zero-confinement state).
- **`docs/config-reference.yaml`** — new `cluster_profile.sandbox_runtime` field, with the prerequisite note inline.
- **`CHANGELOG.md`** — at ship time, each phase gets its own entry per this session's established cadence.
- Not a "new install method" for datawatch itself (AGENT.md §new-install-checklist) — gVisor/Kata are host/cluster prerequisites the *operator* installs, the same documentation pattern already used for `kind`/Tailscale/Headscale elsewhere, not a new datawatch install path.

## 9. Parity surface (required)

`ClusterProfile` fields today — verified against current code, not assumed:

| Surface | Current `ClusterProfile` support | `sandbox_runtime` plan |
|---|---|---|
| REST | Yes — `internal/server/profile_api.go` | Add field to the existing profile CRUD handlers. |
| MCP | Yes — `profile_create`/`profile_update`/`profile_get`/`profile_list`/`profile_smoke` tools | Add field to the same tool schemas. |
| PWA | Yes — `app.js` Cluster Profiles settings card (`gc_clusterprofiles`) | New dropdown (`runc`/`runsc`/`kata`) in the existing profile editor. **Triggers Mobile-Parity Rule** (affordance added) — file a `datawatch-app` issue once shipped, per AGENT.md §530-541. |
| CLI | **No** — no `datawatch profile` subcommand exists for cluster profiles today (pre-existing gap, not introduced by this doc) | Out of scope to fix here; `sandbox_runtime` simply has the same (lack of) CLI parity every other `ClusterProfile` field already has. Noted, not silently matched without acknowledgment. |
| Comm channel | **No** — same pre-existing gap as CLI | Same as above. |
| YAML/config | N/A — `ClusterProfile`s are stored/managed records, not static `config.yaml` keys | Not applicable; profiles are created via REST/MCP/PWA, not a config file field. |
| Android | Indirect, via REST (no native profile-editor UI confirmed in this pass) | No native change planned; flagged in the `datawatch-app` issue above for the app team to assess. |
| iPhone/iOS | Same as Android | Same. |

## 10. Testing (1:1 — unit *and* live-connection, per the Testing Tracker Rule)

- **Unit** (`docker_driver_test.go`, `k8s_driver_test.go`): extend existing patterns — assert the right `--cap-drop`/`--read-only`/`--user`/`--runtime` flags (Docker) or `securityContext`/`RuntimeClassName` fields (k8s) render for each `sandbox_runtime` value; assert PQC keys are **absent** from rendered Docker args and pod env entirely (not just masked); assert the capability-probe error message names the missing runtime/RuntimeClass specifically.
- **Live-connection** ("Validated", not just unit-tested): actually spawn a real worker under hardened-`runc`, and under `runsc`/`kata` where the test host supports them, and confirm — not just specify — that: (a) a privileged syscall attempt is denied (cap-drop enforced), (b) a write outside `/workspace`/`/tmp` is denied (read-only-root enforced), (c) `docker inspect`/`kubectl get pod -o yaml` on the live worker shows no PQC key material anywhere in its output, (d) a representative real build (`go build` or `npm install` inside the project workspace) still succeeds — the Downstream-Review Rule check for §6.1's writable-path allowlist.
- **E3 Kubernetes smoke** (`tests/integration/spawn_k8s.sh`) — extend the existing child-Pod-spawn-and-bootstrap-and-terminate-no-orphans assertions to also check: `securityContext` present and matches §6.2's shape; `RuntimeClassName` honored when set; the default-deny `NetworkPolicy` is created when `NetworkPolicyRef` is unset. If the test cluster lacks `gvisor`/`kata` `RuntimeClass` objects installed, skip those specific assertions with a clear log line — same pattern as the existing ~15-category infra-floor skip list — rather than failing the whole suite on a missing optional prerequisite.
- **New E2E stories**, starting at **TS-785** (confirmed next free ID against the current `scripts/test-stories/` ceiling of TS-784): profile CRUD round-trip for `sandbox_runtime` via REST + MCP (PWA covered by the existing profile-editor PWA stories, extended); a Docker-sim spawn under `runsc`; a k8s spawn under `kata` (skip-if-unavailable per above); a negative test — requesting an unsupported/unavailable runtime returns the specific capability-probe error, not a generic failure.
- **Security-Fix Downstream-Review Rule**: before this ships, run the relevant slice of `release-smoke.sh` *and* a manual real-session smoke (spawn a worker, do real work in it — not just the automated E2E stories) — the same two-track verification this session already used for Tier 1's fixes, applied here because container hardening is exactly the class of change (like the v8.8.4 CSP tightening) that can pass every automated check while quietly breaking a legitimate workflow the tests didn't think to exercise.

## 11. Open items for the next pass (not blocking this doc, named so they aren't lost)

- The Docker-side per-worker bridge + firewall rules (§7) is the one piece of real new infrastructure here, not a straightforward flag addition — expect it to need its own short follow-up note once implementation starts, per §7's own flag.
- CLI/comm-channel parity for `ClusterProfile` as a whole (§9) is a pre-existing gap worth its own backlog item if the operator wants it closed generally, independent of F-2.
- Android/iOS native profile-editor support (§9) depends on what the `datawatch-app` team reports back on the filed issue.
- **Session output/telemetry is pull-only for container/cluster-dispatched
  sessions, confirmed 2026-10-10 while designing BL406/BL407's PRD-worktree
  work** — `forwardSessionToAgent` (`internal/server/api.go:1726`) proxies
  every output request live to the remote worker; nothing accumulates a
  durable copy on the orchestrating daemon. An abrupt container death
  (OOM, node eviction, network partition, a hard `cancel_task`, host
  reboot) before a clean session-end loses the entire transcript/
  telemetry history with no forensic record — and, separately, breaks any
  future checkpoint-based resume (the GuidedMode `guardrail_auto`/
  `council` sources BL406 Phase 5 will add need *something* to resume
  from). Operator decision (2026-10-10): want **both** — push as the
  primary durability mechanism (container streams output/telemetry back
  to the orchestrating daemon as events happen, independent of whether
  the container survives), with the existing pull path
  (`TailOutput`/`forwardSessionToAgent`) kept as an on-demand fallback
  outside the push cycle — same push-primary/poll-fallback shape this
  project already uses elsewhere (the session-list SSE+polling pattern).
  Not designed or built yet. This is an F-2 concern (container-worker
  hardening), separate from BL407's git-completion story (branch push +
  PR on PRD completion already covers the *final* diff; this is about
  *interim* visibility during the run) — but BL407's own plan should
  cross-reference it, since a worktree-mode PRD has the same "what if the
  session dies mid-run" question, just without the container angle.
