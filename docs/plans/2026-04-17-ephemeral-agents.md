# F10 — Ephemeral Container-Spawned Agents

**Status:** planning, awaiting Sprint 1 kickoff
**Owner:** dmz006
**Author:** Claude Opus 4.7 + dmz006 (planning Q&A 2026-04-17)
**Supersedes / absorbs:** BL3 (container images), F10 (originally "container images, Helm chart"), BL21 (profile fallback chains), BL27 (project management), F16 (proxy mode phases 1–5)
**Cross-cutting prereq:** BL16 (`/healthz` + `/readyz`) — folded into Sprint 1
**Defer until later:** Cloud Foundry execution path (architecture must accommodate; no implementation in any sprint here)

---

## 1. Goal

Datawatch becomes the *control plane* for ephemeral, AI-driven agent containers. A user (or another agent) starts a session against a **Project Profile** + **Cluster Profile**; the parent daemon spawns a containerized full-daemon worker on the chosen Docker host or Kubernetes cluster, the worker clones the profile's git repo using a short-lived minted token, runs the AI backend (claude-code, etc.), commits + opens a PR, gets validated, and is torn down. Memory is federated (shared, sync-back, or fully ephemeral per profile policy). Workers can recursively spawn child workers under monitored gates. Everything is reachable via the parent's proxy — no per-pod ingress.

This is the foundation for **multi-agentic** datawatch: orchestrator-and-workers as the principal pattern, peer-to-peer as a fallback for cross-instance communication.

## 2. Non-goals (out of scope for these sprints)

- Cloud Foundry deployment (architecture must accommodate; no impl)
- Per-pod Ingress / Tailscale-in-pods (parent proxies all traffic)
- Multi-tenant SaaS (single user / single team for now)
- Public-facing agent marketplace
- GUI builder for DAGs (BL24 retains its own UI direction)
- Browser-based git creds; non-GitHub providers (gitlab stub only)

## 3. Architectural pillars

### 3.1 Two-tier profile model

```
┌─────────────────────┐        ┌─────────────────────┐
│  Project Profile    │        │  Cluster Profile    │
│  (what to do)       │        │  (where to run)     │
├─────────────────────┤        ├─────────────────────┤
│ name                │        │ name                │
│ git.url, branch     │        │ kind: docker|k8s|cf │
│ backend (claude...) │        │ context / endpoint  │
│ env (overrides)     │        │ namespace           │
│ image variant       │        │ image registry      │
│ memory policy       │        │ resource defaults   │
│ idle timeout        │        │ creds reference     │
│ post-task hooks     │        │ network policy      │
└─────────┬───────────┘        └──────────┬──────────┘
          │     attached via session      │
          └────────────────┬──────────────┘
                           ▼
                   ┌──────────────┐
                   │   Session    │
                   │  references  │
                   │  both        │
                   └──────────────┘
```

A session must reference one of each. They're stored separately so the same project can run on different clusters (dev k8s vs. prod docker swarm later) and the same cluster can host many projects.

### 3.2 Spawning model — start ephemeral, plan for persistent

- **v1 (Sprints 1–7)**: per-session container, dies when validation passes
- **v2 (Sprint 8 onwards)**: optional **service mode** — a long-lived daemon spawned and proxied under the parent that hosts its own sessions (children of children)
- **Container = full daemon** (so workers can recursively spawn children if their profile grants the access)

### 3.3 Termination requires validation

A worker container does *not* self-terminate. The parent decides. Termination flow:

1. Worker reaches a "task complete" candidate state (LLM signals done, or operator command, or PR opened)
2. Parent triggers **validation orchestrator**: spawns a small read-only agent (or runs an LLM check) that inspects the worker's output, git commits, PR state, memory writes
3. If validation passes: parent reaps the container, optionally syncs memory back, finalizes git (auto-merge if profile allows, otherwise leaves PR open)
4. If validation fails: container stays alive; parent surfaces the discrepancy in the UI and waits for operator or further LLM action

### 3.4 All traffic through parent proxy

Children listen on ClusterIP (k8s) or bridge network (docker). Their HTTP/WS/MCP/terminal streams are reverse-proxied through the parent's `/api/proxy/{instance}/...`. No direct browser-to-pod ingress. F16 phases 1–5 deliver this.

### 3.5 Bootstrap: zero-config images

Worker images bake **no config and no secrets**. On startup the worker calls back to a parent endpoint (`POST /api/agents/bootstrap` with a single-use token) and pulls:

- The full effective config (profile-merged)
- A short-lived git token (gh PAT, deleted on session end)
- A pgvector connection string for memory (if shared mode)
- A signed worker identity (used for all subsequent calls home)

The **only** bits passed at spawn time are: parent URL, single-use bootstrap token, worker ID. All else flows through the bootstrap call.

### 3.6 PQC-secured token broker

Single-use bootstrap tokens are protected with **post-quantum-secure crypto**: Cloudflare CIRCL's ML-KEM (Kyber) for key encapsulation and ML-DSA (Dilithium) for signing. Tokens are minted by the parent, stored in an in-memory broker indexed by worker ID, and zeroized on first use. Validation broker also reaps unused tokens after configurable TTL.

### 3.7 Memory federation: 3 modes per profile

| Mode | Behavior | When to use |
|---|---|---|
| `shared` | Worker connects directly to parent's pgvector. All writes immediately visible. | Same VPC, latency low |
| `sync-back` | Worker has its own postgres+pgvector; on termination (or periodic), changes sync to parent. | Cross-cluster, child needs local low-latency memory |
| `ephemeral` | Worker uses local sqlite, nothing syncs. | One-shot tasks where nothing needs to persist |

Selectable per Project Profile, overridable per session. Per-profile memory **namespace** isolates writes; profiles can opt-in to `shared_with: [profile-x, profile-y]` for cooperative work.

### 3.8 Workspace lock — no two workers in the same profile

A profile is a *singleton* concurrency unit. The parent rejects a spawn that would put two workers on the same Project Profile. Recursion exception: a worker can spawn children that use **derived sub-profiles** (forked branch, dedicated memory namespace) but never the parent's exact profile. Future: forks merged via PR into parent's branch.

### 3.9 Recursion gates

A worker can spawn its own children only if its profile carries an explicit `allow_spawn_children: true`. Even then, a `spawn_budget_total` and `spawn_budget_per_minute` cap runaway behavior. The parent enforces both — children call home to spawn; nothing happens locally.

### 3.10 IP / network reality (concrete for this dev environment)

- Parent runs at **192.168.1.51** (desktop, internal network)
- Test k8s cluster: `kubectl config use-context testing` — must be able to reach 192.168.1.51 (we'll discover via in-cluster DNS to home gateway, or static service-of-type-ExternalName)
- Children must call `https://192.168.1.51:8443` for bootstrap + ongoing comms
- No Tailscale in pods (yet); rely on internal routing
- Sprint 4 includes **network discovery test** as the first acceptance criterion

---

## 4. Sprint plan

Each sprint is two weeks of focused work; story points are rough effort. Acceptance criteria are testable. Risks listed are gating — not "could be a problem."

---

### Sprint 1 — Foundations: health, slim image, BL3 phase 1+2

**Goal:** ship a `datawatch:slim` container image, confirm the daemon runs healthily inside it, and make the proxy-readiness signals real.

**Stories:**

- **S1.1 — BL16: health endpoints** *(2h)*
  - `GET /healthz` — process-up, basic config loaded
  - `GET /readyz` — bootstrap done, memory reachable, signal-cli warmed (if enabled)
  - JSON body with subsystem statuses
  - Probes wired into Helm later
  - **Files:** `internal/server/health.go` (new), `internal/server/server.go`
  - **Tests:** `internal/server/health_test.go`

- **S1.2 — Dockerfile (full + slim variants)** *(1d)*  *(superseded by S1.7 image taxonomy 2026-04-18)*
  - Original delivery: `Dockerfile.slim` + `Dockerfile.full` on debian-slim base. Both built and smoke-tested locally; both pushed to harbor.
  - Now superseded — see S1.7 below.

- **S1.3 — Workspace volume convention** *(2h)*
  - `session.workspace_root` config field (defaults to `/workspace` in container)
  - Session `project_dir` resolves under it for portability
  - **Files:** `internal/config/config.go`, `internal/session/manager.go`

- **S1.4 — Local build pipeline (Makefile + .env.build)** *(4h)*  *(revised 2026-04-18)*
  - **Decision:** local-first build. GitHub Actions stubbed off; harbor.dmzs.com is the primary registry; local `registry:2` is the fallback for air-gap.
  - `Makefile` targets:
    - `make container` — build slim + full, multi-arch (amd64+arm64) via `docker buildx`, push to `$REGISTRY` from `.env.build`
    - `make container-load` — build for current arch only and `docker load` into local docker daemon (no push, fastest dev loop)
    - `make container-tarball` — `dist/datawatch-{slim,full}-{arch}-vX.Y.Z.tar.xz` for offline transport
    - `make container-clean` — wipe build cache
  - `.env.build` (gitignored, with `.env.build.example` checked in):
    ```
    REGISTRY=harbor.dmzs.com/datawatch
    PLATFORMS=linux/amd64,linux/arm64
    PUSH=true
    ```
  - **Harbor auth**: user runs `docker login harbor.dmzs.com` once; push errors on first run prompt them to do so.
  - **Acceptance:** `make container REGISTRY=harbor.dmzs.com/datawatch` produces `harbor.dmzs.com/datawatch/datawatch:{slim,full}-vX.Y.Z` images visible via `docker pull`.

- **S1.6 — Local registry fallback helper** *(2h)*  *(new 2026-04-18)*
  - `make registry-up` / `registry-down` — runs `registry:2` on desktop port 5000 (HTTP, internal-only)
  - Use case: harbor unreachable, or air-gapped k8s cluster with insecure-registry config
  - Document `containerd` config for the testing cluster to allow `192.168.1.51:5000` insecure
  - Cluster Profile's `image_registry` field accepts both URLs; one switch, no code change
  - **Acceptance:** with harbor offline, `make registry-up && REGISTRY=192.168.1.51:5000/datawatch make container` works end-to-end

- **S1.4b — GitHub Actions stub** *(30m)*
  - `.github/workflows/container.yaml.disabled` — workflow body present but extension disables auto-trigger
  - Documented: rename to `.yaml` to enable on-tag push to GHCR
  - **Reason:** keep the option without coupling to GH availability today.

- **S1.5b — k8s smoke (deferred to Sprint 4)** *(noted 2026-04-18)*
  - Initial run revealed: TKGI test cluster nodes don't trust the harbor.dmzs.com CA (Pivotal-issued, same root the desktop docker daemon needed).
  - Pod scheduled correctly, manifest is sound, image is pushed and visible at `harbor.dmzs.com/datawatch/datawatch:{slim,full}-2.4.5` — only failure is `failed to verify certificate: x509: certificate signed by unknown authority` on `containerd` pulling.
  - Fix lives at the cluster layer (BOSH manifest / TKGI cluster-template `trusted_certificates`, or per-node `/etc/containerd/certs.d/harbor.dmzs.com/hosts.toml`) and **needs node SSH or TKGI admin access** I don't have.
  - **Sprint 4's Cluster Profile gains a `trusted_cas: []` field** (PEM blobs) and the K8s driver projects them into the worker Pod's container `volumeMounts` + sets `SSL_CERT_DIR`. Worker bootstrap also writes them under `/etc/containerd/certs.d/` if it has nodeAccess (rare; mostly Cluster Profile prerequisite docs).
  - For S1.5b acceptance: documented working `kubectl run` happens once the cluster is configured to pull from harbor (or once we set up a registry the cluster already trusts). Tracking moved to Sprint 4.

- **S1.9 — Image taxonomy v2: per-agent + per-language (Pod composition)** *(2d)*  *(new 2026-04-18, supersedes S1.7)*
  - Initial S1.7 baked agents into per-language images. Building agent-base alone hit 1.96GB; agent-go was 2.83GB. Investigation: `claude.exe` (236MB native ELF), `opencode-ai` (534MB npm with 4 platform binaries), `aider` (958MB pipx tree from litellm) — agents themselves dominate. /usr/include 65MB, /usr/share/{doc,man,locale} 84MB pure waste.
  - **New rule:** ONE agent OR ONE language toolchain per image. Composed via two-container Pods sharing /workspace volume (k8s driver in Sprint 4 builds the manifest from a Project Profile's `(agent, language)` tuple).
  - **Variants:**
    - `agent-base` — datawatch + tmux + git + gh + rtk + unix tooling. NO node, NO python, NO agents. Target ~250-300MB.
    - `agent-claude` — claude.exe (236MB native ELF, no node needed at runtime).
    - `agent-opencode` — pruned to one platform binary (saves 400MB) + bare node runtime.
    - `agent-gemini` — npm @google/gemini-cli + node.
    - `agent-aider` — pipx aider-chat + python + build deps.
    - `lang-go`, `lang-node`, `lang-python`, `lang-rust`, `lang-kotlin` — pure language toolchains, no agent.
    - `parent-full` — agent-base + signal-cli + JRE (control plane).
  - **Aggressive size cleanup applied to agent-base** (and inherited by all): strip `/usr/share/{doc,man,locale,info}` (keep en_US locales only), `/usr/include`, `/var/cache/apt`, `/tmp`, `/root/.cache`. lang-* images that need C headers re-install build-essential.
  - **opencode platform-binary pruning** in agent-opencode builder stage saves 400MB on its own.
  - **Files:** `docker/dockerfiles/Dockerfile.{agent-base,agent-claude,agent-opencode,agent-gemini,agent-aider,lang-go,lang-node,lang-python,lang-rust,lang-kotlin,parent-full}`
  - **Makefile:** `AGENT_TYPES` + `LANG_TYPES` env-driven build matrix. `make container` builds the dependency chain in order.
  - **Old artifacts removed:** `Dockerfile.agent-{go,node,python,rust,kotlin,polyglot}` (the S1.7 fat-style files); old `agent-go:v2.4.5` image deleted from harbor.

- **S1.7 — Image taxonomy v1: agent-base + per-language** *(1.5d)*  *(superseded by S1.9 — kept for trace)*
  - Replace `slim`/`full` with a stack: every per-language worker FROM's a common `agent-base`. Profiles pick a variant; auto-detection from cloned repo on first spawn (Sprint 2 wires this).
  - **Base swap** to `bitnami/minideb:bookworm` (~28MB vs 75MB for debian-slim, kept current monthly).
  - **Variants:**
    - `agent-base` — tmux, git, gh, ripgrep, jq, fd, build-essential, ssh, curl, claude-code, opencode, aider, gemini-cli, rtk, datawatch binary. ~400MB target.
    - `agent-go` — adds Go 1.25, gopls, golangci-lint, delve.
    - `agent-node` — adds typescript, tsx, eslint, bun (node 22 already in base).
    - `agent-python` — adds uv, poetry, ruff, pyright (python3 + pipx already in base).
    - `agent-rust` — adds rustup-managed Rust + clippy/rustfmt/rust-analyzer.
    - `agent-kotlin` — adds JDK 21, Kotlin compiler, Gradle, Android cmdline-tools (for the dmz006/datawatch-app KMP project — wear/auto/Android targets).
    - `agent-polyglot` — kitchen sink (Go + Node + Python + Rust + Kotlin/Android).
    - `parent-full` — `agent-polyglot` + signal-cli (replaces the former `Dockerfile.full`).
  - **Pinned versions** for every CLI tool in `ARG` defaults (rtk 0.37.0, claude 2.1.114, opencode 1.4.11, gemini-cli 0.38.2, aider 0.86.0, signal-cli 0.14.2, etc.). Reproducible. `make container-upgrade` (S1.8) bumps them.
  - **Files:** `docker/dockerfiles/Dockerfile.{agent-base,agent-go,agent-node,agent-python,agent-rust,agent-kotlin,agent-polyglot,parent-full}`, Makefile rewrite.
  - **Multi-stage caching** to harbor via `--cache-to/from type=registry,ref=…:buildcache` — every variant's intermediate layers reusable across builds.
  - **AGENT_VARIANTS** Makefile var picks which language variants `make container` produces. Defaults to all five.
  - **LLM runtimes (ollama, etc.)** intentionally **not** baked in — workers reach back to parent's ollama via bootstrap-injected `OLLAMA_HOST`. Saves ~500MB and avoids GPU concerns until Sprint 8+.
  - **Acceptance:** `make container-load` produces `agent-base` for current arch; `docker run agent-base version` prints v2.4.5+; `claude --version`, `opencode --version`, `aider --version`, `rtk --version` all run inside the container.

- **S1.8 — make container-upgrade** *(3h)*  *(new 2026-04-18)*
  - `scripts/container-upgrade.sh` — resolves latest version of every pinned tool from upstream APIs (npm registry, GH releases, PyPI), prints a diff. `--apply` rewrites the ARG defaults in-place across all Dockerfiles.
  - **Release rule:** rebuild + push all variants whenever a `vX.Y.Z` tag is cut. Documented in CONTRIBUTING.md.
  - **Acceptance:** dry-run lists pending bumps; `--apply` modifies files; `git diff` shows only ARG lines changed.

- **S1.5 — Smoke test image with real session** *(2h)*
  - `tests/integration/container_smoke.sh` — runs the slim image, calls the API, starts a `bash` backend session, asserts state transitions
  - **Acceptance:** test passes locally + in CI

**Risks:**
- Distroless lacks tmux → may need Alpine. Decide during S1.2.
- Multi-arch builds slow CI. Use buildx + cache aggressively.

**Exit criteria:** `docker run datawatch:slim` boots in <5s, `/readyz=200`, can run a non-AI session end-to-end. Image published.

---

### Sprint 2 — Profile system (Project + Cluster), settings UI

**Goal:** user can create, edit, list, smoke-test Project Profiles and Cluster Profiles via UI, MCP, API, and config channels. Storage + RBAC scaffolding ready.

**Stories:**

- **S2.1 — Schema + storage** *(1d)*
  - `internal/profile/project.go` — ProjectProfile struct, JSON storage in `~/.datawatch/profiles/projects.json` (encrypted under `--secure`)
  - `internal/profile/cluster.go` — ClusterProfile struct, JSON storage in `clusters.json`
  - CRUD interface, validation (required fields, unique names, reachability check stubs)
  - **Fields (Project):** name, git.url, git.branch, backend, env (map), image_variant (`full|slim|custom:tag`), memory.mode (`shared|sync-back|ephemeral`), memory.namespace, memory.shared_with[], idle_timeout, allow_spawn_children, spawn_budget_total, spawn_budget_per_minute, post_task_hooks[]
  - **Fields (Cluster):** name, kind (`docker|k8s|cf`), context_or_endpoint, namespace, image_registry, default_resources (cpu/mem requests+limits), creds_ref (vault key id or filepath), network_policy_ref, parent_callback_url (defaults to detected, override possible)

- **S2.2 — REST API** *(4h)*
  - `GET/POST /api/profiles/projects`, `GET/PUT/DELETE /api/profiles/projects/{name}`
  - Same for `/api/profiles/clusters`
  - `POST /api/profiles/projects/{name}/smoke` — dry-run validation
  - **Tests:** httptest unit tests for each endpoint

- **S2.3 — MCP tools** *(3h)*
  - `profile_project_list/get/create/update/delete/smoke`
  - `profile_cluster_list/get/create/update/delete/smoke`
  - Wired to parity test suite

- **S2.4 — Settings UI: Project Profiles card** *(1d)*
  - New card on Settings → General
  - List view, click-through edit form, "+ Add" button, Smoke Test button per row
  - Form fields with inline validation
  - **YAML view toggle** (form ↔ raw YAML)
  - **Files:** `internal/server/web/app.js`, `style.css`, `templates/profile-form.html` (or inline)

- **S2.5 — Settings UI: Cluster Profiles card** *(1d)*
  - Mirror of Project Profiles UI
  - Cluster-kind-specific fields (k8s shows context dropdown, docker shows host)
  - Connection test button (calls `/api/profiles/clusters/{name}/smoke`)

- **S2.6 — CLI parity** *(3h)*
  - `datawatch profile project create|list|edit|delete|smoke`
  - `datawatch profile cluster create|list|edit|delete|smoke`

- **S2.7 — Comm-channel parity** *(2h)*
  - Add `profile list/show` commands to router for signal/telegram/etc.

**Risks:**
- Schema sprawl. Lock fields in S2.1 review meeting before coding the UI.
- Encryption of profiles when `--secure`: mirror existing `EncryptedStore` pattern; don't reinvent.

**Exit criteria:** Create both profile types from UI, MCP, API, CLI, signal — all reach the same state. Smoke test catches a deliberately-broken profile.

---

### Sprint 3 — Container spawning: docker driver first

**Goal:** `POST /api/agents/spawn {project_profile, cluster_profile, task}` brings up a real container that boots, calls bootstrap, and reports `ready`. Local docker only — k8s in Sprint 4.

**Stories:**

- **S3.1 — Spawn API + abstraction** *(1d)*
  - `internal/agents/spawn.go` — `Driver` interface (Docker, K8s, CF impls)
  - `POST /api/agents/spawn` — payload validates profiles exist, calls driver
  - `GET /api/agents` — list active workers
  - `GET /api/agents/{id}` — status + logs
  - `DELETE /api/agents/{id}` — terminate

- **S3.2 — Docker driver** *(1.5d)*
  - Uses official Go docker client
  - Pulls image, creates container with: env (parent URL, bootstrap token, worker ID), bridge network, volume mount for workspace
  - Reads logs via stream
  - Inspects status; reaps on terminate
  - **Files:** `internal/agents/docker_driver.go`, `..._test.go`

- **S3.3 — Bootstrap endpoint** *(4h)*
  - `POST /api/agents/bootstrap` — accepts single-use token + worker ID
  - Returns: effective merged config, git token (Sprint 5), memory connection (Sprint 6), worker identity certificate
  - Burns the token after first use
  - **Files:** `internal/agents/bootstrap.go`

- **S3.4 — Worker self-registration** *(4h)*
  - On `start --foreground` with `DATAWATCH_BOOTSTRAP_URL` + `DATAWATCH_BOOTSTRAP_TOKEN` env: call bootstrap, write returned config to in-memory state, skip local config file
  - Calls `POST /api/agents/{id}/register` once `/readyz=200`
  - **Files:** `cmd/datawatch/main.go` (early in runStart), `internal/agents/client.go`

- **S3.5 — Reverse proxy for child** *(1d)*
  - `/api/proxy/{worker_id}/...` routes HTTP + WS to the worker
  - Auth: parent's session token, internally rewritten to worker's identity
  - **Files:** `internal/server/agent_proxy.go`
  - **Reuses:** F16 proxy code path

- **S3.6 — Session-on-worker binding** *(4h)*
  - When session created with `agent_id` set, all session API calls forwarded to that worker via proxy
  - UI shows worker badge on session card

- **S3.7 — Smoke flow** *(3h)*
  - End-to-end: profile → spawn → bootstrap → register → start session → kill agent
  - Integration test: `tests/integration/spawn_docker.sh`

**Risks:**
- Bootstrap-call timing: if the worker calls home before parent has the token registered, retry/backoff matters. Define the contract precisely in S3.3.
- Network topology: containers on default bridge can usually reach the host; document the hostname fallback (`host.docker.internal` on mac/win; gateway IP on linux).

**Exit criteria:** `datawatch agent spawn --project foo --cluster local-docker --task "echo hi"` produces a session whose terminal output is visible in the parent UI, sourced from a real docker container.

---

### Sprint 4 — Kubernetes driver + network handshake

**Carry-in from Sprint 1:**
- S1.5b k8s smoke (blocked on cluster CA trust). Becomes the Sprint 4 readiness gate: once the K8s driver can spawn a Pod and the worker bootstraps successfully, Sprint 1's deferred smoke retroactively passes.
- New Cluster Profile field: `trusted_cas: [PEM, …]` — projected into spawned Pods so workers (and kubelet via per-node config when feasible) trust private registry / API CAs.


**Goal:** spawn into the testing k8s cluster (`kubectl config use-context testing`) from the desktop parent at `192.168.1.51`, with traffic flowing both ways through the parent proxy.

**Stories:**

- **S4.1 — K8s driver** *(2d)*
  - Uses `k8s.io/client-go`
  - Creates Pod (not Deployment for v1) with init container that handles bootstrap timing, main container running `datawatch start --foreground`
  - ConfigMap for non-secret bootstrap params; Secret for the bootstrap token (rotated on use)
  - Owner references so reaping the Agent record removes the Pod
  - **Files:** `internal/agents/k8s_driver.go`

- **S4.2 — Network discovery** *(4h)*
  - Configurable `parent_callback_url` on Cluster Profile (overrides auto-detect)
  - Auto-detect logic: parent IP from `Server.PublicURL` config, fallback to `192.168.1.51`
  - **Acceptance test (manual):** apply a test pod, `kubectl exec -- curl -k https://192.168.1.51:8443/healthz` returns 200

- **S4.3 — In-cluster TLS** *(3h)*
  - Parent serves a CA cert via `/api/agents/ca.pem`
  - Bootstrap response includes the parent's cert fingerprint
  - Worker pins the cert
  - **Files:** `internal/agents/tls.go`

- **S4.4 — Helm chart for the parent** *(1d)*
  - `charts/datawatch/` — Deployment, Service (ClusterIP), ConfigMap, Secret, optional PVC
  - Values: image.tag, image.variant, postgres.url, replicas (always 1 for v1; HA later)
  - **Acceptance:** `helm install dw ./charts/datawatch -f testing-values.yaml` produces a healthy parent inside the test cluster

- **S4.5 — Cluster Profile: smoke that spawns a real Pod** *(4h)*
  - Smoke action creates a tiny ephemeral Pod, waits for `/readyz=200` via proxy, deletes Pod
  - UI shows pass/fail with logs

**Risks:**
- Cluster-to-desktop connectivity may need a NodePort or LB on the parent side. Document setup.
- TLS pinning vs. cert rotation: keep the rotation cycle long (30d) and store fingerprints with timestamps.

**Exit criteria:** Spawn a session in the testing cluster from the desktop. View its terminal in the parent UI. Validate end-to-end TLS + auth.

---

### Sprint 5 — Git lifecycle: clone, work, commit, exit

**Goal:** session begins by cloning the profile's repo using a parent-minted short-lived token; on session-complete, work is committed and a PR is opened (or branch pushed); the token is revoked.

**Stories:**

- **S5.1 — Token broker** *(1d)*
  - `internal/auth/token_broker.go` — mint, store, revoke, audit
  - Uses host's `gh auth` to mint via `gh api -X POST /user/installations/.../access_tokens` (or equivalent for fine-grained PATs)
  - Tokens scoped to the single repo, expire in 1h, revoked on session end
  - Best-effort revoke + periodic sweep of orphaned tokens older than max-TTL
  - **Tests:** mock gh API, unit test mint+revoke flow

- **S5.2 — PQC-protected bootstrap tokens** *(1d)*
  - Add `github.com/cloudflare/circl` dependency
  - ML-KEM 768 for key encapsulation, ML-DSA 65 for signing
  - Bootstrap tokens become: `{worker_id, kem_ciphertext, signature}` — only the parent can decapsulate, only the worker that has the spawn-time KEM secret can use
  - Replace plain UUID tokens from Sprint 3
  - **Files:** `internal/agents/pqc_token.go`, `..._test.go`

- **S5.3 — Worker git clone on bootstrap** *(4h)*
  - Bootstrap response includes `git.token`, `git.url`, `git.branch`
  - Worker clones into `/workspace/{repo-name}` at session start
  - Sets `project_dir` to that path automatically

- **S5.4 — Commit + PR on session complete** *(1d)*
  - Hooks into existing `auto_git` infrastructure (already commits)
  - Push branch via `gh pr create`
  - Optional auto-merge if profile has `auto_merge_on_validate: true`

- **S5.5 — Token cleanup** *(3h)*
  - Session terminate → `tokenBroker.Revoke(workerID)`
  - Background sweeper every 5 min: list active workers, expire tokens not paired with any
  - **Tests:** simulate orphaned token, verify sweep removes it

- **S5.6 — Gitlab stub** *(2h)*
  - Interface `GitProvider` with `Github` impl
  - `Gitlab` stub returns `ErrNotImplemented`
  - Profile schema accepts `git.provider: github|gitlab`

**Risks:**
- gh CLI auth model changes — pin a version; document the install assumption.
- PQC libs are evolving (CIRCL is solid but APIs do shift). Vendor or pin a minor.

**Exit criteria:** Spawn a session against a real test repo, watch claude-code make a one-line change, see the commit + PR appear, confirm the token is gone from gh's list of active tokens.

---

### Sprint 6 — Memory federation (3 modes)

**Goal:** profiles drive memory behavior — `shared`, `sync-back`, or `ephemeral`. Workers can read parent's memory and (mode-dependently) write back. Namespacing isolates profiles unless they opt to share.

**Stories:**

- **S6.1 — Memory namespace enforcement** *(1d)*
  - Add `namespace` column to memory tables (or filter via key prefix in sqlite)
  - All memory queries take a namespace; default `__global__` for back-compat
  - **Files:** `internal/memory/store.go`, `pg_store.go`

- **S6.2 — Shared mode** *(4h)*
  - Bootstrap returns parent's pgvector connection
  - Worker uses pg directly, namespaced
  - **Acceptance:** worker writes a memory; parent recalls it

- **S6.3 — Sync-back mode** *(2d)*
  - Worker runs local sqlite (default) or its own postgres
  - On session-complete: collect new rows since session-start, POST to `/api/memory/import` on parent
  - Conflict policy: append-only (timestamps win); KG triples merged; embeddings re-computed if embedder differs
  - **Files:** `internal/memory/sync.go`

- **S6.4 — Ephemeral mode** *(2h)*
  - Worker uses sqlite in tmpfs / overlay
  - Nothing syncs back
  - Just need to wire the policy

- **S6.5 — Cross-profile sharing** *(4h)*
  - Profile field `shared_with: [list of profile names]`
  - Memory queries union namespaces of all `shared_with` profiles + own
  - Gated by mutual opt-in

- **S6.6 — Memory federation UI** *(4h)*
  - Profile editor surfaces mode + namespace + shared_with
  - Memory browser shows namespace badges
  - "Pull memory from worker" manual button on agent detail page

- **S6.7 — pgvector required-or-fallback** *(3h)*
  - Slim image's bootstrap can request `memory.fallback_sqlite: true`
  - If pgvector unreachable + fallback enabled: warn + use sqlite

**Risks:**
- Embedding model drift: parent on `nomic-embed-text`, worker on something else → vectors not comparable. Reject mismatched embedders unless `force_reindex_on_sync`.
- pgvector resource sizing in cluster: document a recommended StatefulSet config.

**Exit criteria:** Run two sessions on two different profiles in `shared_with` mode; confirm one's memory recall surfaces the other's facts. Run an `ephemeral` profile; confirm nothing persists.

---

### Sprint 7 — Multi-agent orchestration (the agentic story)

**Goal:** the parent's BL24 (autonomous task decomposition) can dispatch subtasks to remote workers, with fan-out and fan-in. Workers can recursively spawn children when their profile permits.

**Stories:**

- **S7.1 — Orchestrator core** *(2d)*
  - Plug F15 pipeline executor into `internal/agents/spawn` so DAG nodes become spawn requests
  - Each node: `{project_profile, cluster_profile, task, depends_on[]}`
  - Parent maintains the DAG state; workers report progress + outputs

- **S7.2 — Fan-in: result aggregation** *(1d)*
  - Workers post structured results (`/api/agents/{id}/result`)
  - Orchestrator merges results into a parent session's context
  - Memory writes from children visible per federation mode

- **S7.3 — Workspace lock** *(4h)*
  - Spawn API rejects a request that would put two workers on the same `(project_profile, branch)` tuple
  - Recursion exception: child uses derived sub-profile (auto-generated branch name, namespace `parent.namespace.{child_id}`)
  - **Tests:** simulate concurrent spawns, expect rejection

- **S7.4 — Recursion gates** *(4h)*
  - Worker's spawn requests go through parent (children call home)
  - Parent enforces `allow_spawn_children`, `spawn_budget_total`, `spawn_budget_per_minute`
  - Audit log of every spawn

- **S7.5 — Validation orchestrator** *(1d)*
  - On worker reports "task complete": parent spawns a small read-only validation agent (could be a tiny ephemeral profile)
  - Validator checks: PR diff sanity, memory writes, declared task vs. observed work
  - Pass → reap; Fail → leave alive + alert

- **S7.6 — Peer-to-peer messaging** *(1d)*
  - Workers can address each other: `parent.proxy.broadcast({to: [worker_ids], topic, body})`
  - Optional bridge to shared signal/telegram channel for cross-instance human-visible chatter
  - Default: orchestrator-only; P2P opt-in per profile

- **S7.7 — Comm-channel inheritance** *(4h)*
  - Workers can be configured to use the *parent's* signal/telegram for outbound alerts (so the user sees them in one stream)
  - Or have their own (rare; for fully isolated agents)

**Risks:**
- Runaway recursion is the biggest production risk. Default budgets to 0 unless explicitly set on profile.
- Validation agent could itself need a profile + cluster — risk of validation loop. Use a hard-coded internal "validator" image, not user-config.

**Exit criteria:** A parent BL24-style decomposition produces 3 child workers in the test cluster; each commits its sub-PR; orchestrator merges; validator approves; all containers reaped; all tokens revoked.

---

### Sprint 8 — Hardening, secrets pluggability, service mode

**Goal:** prod-ish posture; service mode (long-lived spawned service that itself spawns sessions); CF accommodation hooks (no impl).

**Stories:**

- **S8.1 — Secrets pluggability** *(1.5d)*
  - `SecretProvider` interface: `File`, `EnvVar`, `K8sSecret`, `Vault`, `CSI` (latter two are stubs with docs)
  - Cluster Profile's `creds_ref` resolves through provider
  - Token broker writes minted tokens via provider too

- **S8.2 — Service mode workers** *(2d)*
  - Profile flag: `mode: ephemeral | service`
  - Service workers proxy under parent, accept sessions, never auto-terminate
  - Operator can stop them manually

- **S8.3 — Multi-cluster** *(1d)*
  - List of Cluster Profiles can include multiple k8s clusters (gcp, aws, on-prem)
  - Spawn picks based on session payload + profile defaults
  - kubeconfig contexts loaded lazily, cached

- **S8.4 — Audit trail** *(4h)*
  - Every spawn, bootstrap, token mint/revoke, validation result logged to `~/.datawatch/audit/` (rotating)
  - Searchable via API + UI

- **S8.5 — CF placeholder** *(2h)*
  - `internal/agents/cf_driver.go` — stub returning `ErrNotImplemented`
  - Doc the `cf` Cluster Profile shape (org, space, service-broker bindings)
  - `memory.mode: shared` design accepts a `bound_postgres_uri` env var for CF

- **S8.6 — Idle-timeout enforcement** *(3h)*
  - Workers reporting no activity for `profile.idle_timeout` get gracefully shut down by parent
  - Activity = session input, memory write, agent log, MCP call

- **S8.7 — Crash policy** *(3h)*
  - Configurable per profile: `on_crash: fail_parent | respawn_once | respawn_with_backoff`
  - Default: `fail_parent` (safer)

**Exit criteria:** A service-mode worker survives a restart of the parent; a multi-cluster profile spawns to the right place; an `idle_timeout` reaps a stuck worker; all secrets paths use the provider abstraction.

---

## 5. Decisions locked in this Q&A round (2026-04-17)

| # | Decision |
|---|---|
| A1 | Per-session ephemeral by default; service mode added in Sprint 8. |
| A2 | Termination requires validation. Validator is its own agent. Worker does not self-terminate. |
| A3 | Workers run the **full daemon** (recursion supported, gated). |
| B4–B6 | All traffic through parent proxy. WS, MCP, terminal — all proxied. |
| C7 | **Two-tier profiles**: Project Profile (what) + Cluster Profile (where). Both required on spawn. Profile overrides config. |
| D10 | Image bakes nothing. Workers call home for config + creds. |
| D11 | Single-use tokens, **PQC-protected** (CIRCL ML-KEM + ML-DSA). |
| D'12 | Parent uses host gh to mint short-lived PATs, injects via bootstrap, revokes on exit. |
| D'13 | Best-effort revoke + sweeper validates orphans. |
| D'14 | Gitlab stubbed for v1. |
| E15 | All three modes (`shared`, `sync-back`, `ephemeral`). Start with shared; sync-back follows. |
| E16 | Per-profile namespace; `shared_with` opt-in for cross-profile collaboration. |
| E17 | Sync-back configurable per session/profile. |
| E18 | pgvector preferred, sqlite fallback supported. Multiple image variants. |
| F19 | Configurable namespace per profile. |
| F20 | Parent at 192.168.1.51 today; future cluster-internal parent supported. |
| F21 | ClusterIP + parent-proxies-all. No per-pod ingress. No Tailscale in pods. |
| G22 | Children may spawn children if profile + budget allow. |
| G23 | Orchestrator owns DAG; P2P available; communication channels can be shared (signal). |
| G24 | Two workers cannot share the same Project Profile. Recursion uses derived sub-profiles. |
| H25 | All config follows existing rules: yaml + form + MCP + API + comms + smoke + min-config check. |
| I26 | BL16 health endpoints folded into Sprint 1. |
| I27 | F16 (proxy mode) merged into this plan. |
| I28 | BL21 (profile fallback) + BL27 (project mgmt) merged into Sprint 2. |
| J29 | Cloud Foundry: plan accommodates, no impl. |

## 6. Open / deferred questions

- **Validator profile design** — what model, which checks default to enabled, where does its image live?
- **Multi-tenant story** — for now single-user. Multi-user RBAC is a separate future plan.
- **Cost accounting** — when workers consume LLM API budget, who pays? Sprint 8+ idea: per-profile API key + usage caps.
- **Audit log format** — JSONL + rotating, vs. SQLite-indexed for fast search? Decide in S8.4.
- **HA parent** — single-instance v1. HA is a Sprint 9+ topic (StatefulSet, leader election).
- **Image registry auth** — assume public ghcr.io for v1; private registries via standard k8s imagePullSecrets in Cluster Profile.

## 7. Deliverables snapshot (acceptance demo at end of Sprint 7)

1. Operator opens UI → Settings → Project Profiles → creates `dw-test-repo` (git URL, slim image, `sync-back` memory).
2. Creates Cluster Profile `testing-k8s` (kind=k8s, context=testing, namespace=dw-agents).
3. New Session → picks both profiles, task = "add a TODO to README.md".
4. Worker pod appears in `kubectl get pods -n dw-agents`. Terminal streams in browser via parent proxy. Session card shows the trust prompt (B25 ✓) and the worker badge.
5. claude-code clones the repo, edits, commits.
6. Session reports done. Validator pod spawned, checks PR, marks pass.
7. Worker pod terminated. Memory synced back. gh PAT revoked.
8. UI shows green tick + PR link.

## 8. Sprint 0 (immediate) — kick-off chores

Before Sprint 1 starts, do:

- [ ] Open this plan as `F10` in plans/README.md (replacing existing F10 row)
- [ ] Add `BL16` row if not present
- [ ] Decide cadence (2-week sprints? 1-week?)
- [ ] Tag a `pre-f10` baseline so we can measure regressions
- [ ] Confirm `kubectl config use-context testing` works from this desktop and the parent can be reached from inside

---

*This plan is intended to be re-read and revised at each sprint review. Mark sprint exits in plans/README.md as they ship.*
