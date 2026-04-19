# Release Notes — v3.0.0 (F10 ships)

**2026-04-19.** Datawatch graduates from single-host session manager
to a distributed control plane for AI-coding agents: ephemeral
workers in containers/Pods, federated memory, post-quantum bootstrap
tokens, peer-to-peer workflow, and a mobile-ready REST surface.

---

## Highlights

- **F10 — Ephemeral container-spawned agents** (8 sprints complete).
  Project + Cluster profiles, Docker + K8s drivers, token broker
  with ML-KEM/ML-DSA PQC envelope, worker git-token minting, idle
  reaper, service-mode workers, orchestrator DAG, peer broker, full
  audit trail in JSON-lines + CEF.
- **F17 / F18 / F19 — Mobile-ready API surface** (issues #1, #2, #3).
  `POST /api/devices/register` (FCM + ntfy), `POST /api/voice/transcribe`,
  `GET /api/federation/sessions`. Closes the datawatch-app parity
  dependencies.
- **Helm self-managing install.** One `helm install` boots the
  control plane in a cluster with every credential resolved from
  operator-supplied Secrets — no operator home dir at runtime.
- **Mempalace ports.** Per-agent diaries, temporal-KG contradiction
  detection, closets/drawers verbatim-summary chain.

---

## Backlog items shipped (alphabetical by ID)

| ID | What | Rationale |
|---|------|-----------|
| BL92 | Write-through session registry | Orphan directories caused when daemon restarts between Save intervals |
| BL93 | Startup session reconciler | Re-import orphaned session dirs on daemon boot |
| BL94 | `datawatch session import <dir>` | Manual escape hatch for cross-host migration |
| BL95 | PQC bootstrap envelope wiring | ML-KEM 768 + ML-DSA 65 opt-in via config |
| BL96 | Wake-up stack L4/L5 + per-agent L0 overlay | Spawned children inherit parent context + see siblings |
| BL97 | Per-agent diary helpers | Survives worker termination; queryable for retrospectives |
| BL98 | KG contradiction detection | Functional-predicate policing + operator-driven resolve |
| BL99 | Closets/drawers chain | Summary-embedded rows point to verbatim drawers |
| BL100 | Worker HTTP memory client | `shared` / `sync-back` federation modes |
| BL101 | Cross-profile namespace expansion | `GET /api/memory/search?profile=` uses mutual opt-in union |
| BL102 | Worker comm-channel proxy-send | `POST /api/proxy/comm/{ch}/send` |
| BL103 | Validator agent image + check logic | Read-only post-session attestor (distroless ~5MB) |
| BL104 | Peer broker REST + worker pull | `POST /api/agents/peer/send` + `GET /api/agents/peer/inbox` |
| BL105 | `pipelines.Executor` → `agents.Orchestrator` | Mixed single-host + multi-container DAG pipelines |
| BL106 | Runtime `on_crash` policy enforcement | fail_parent / respawn_once / respawn_with_backoff |
| BL107 | Agent audit trail query | `GET /api/agents/audit` + MCP + comm |
| BL108 | Idle-reaper sweeper goroutine | Period-configurable via `agents.idle_reaper_interval_seconds` |
| BL109 | Auto-wire MCP into every spawned LLM session | `.mcp.json` written per spawn |
| BL110 | MCP-callable `/api/config` with permission gate | `mcp.allow_self_config` flag + audit |
| BL111 | `secrets.Provider` wired into `CredsRef` | File / EnvVar concrete + 3 stubs |
| BL112 | Service-mode reconciler | Parent restart re-attaches service workers via label discovery |
| BL113 | Self-managing platform bootstrap | `docs/install.md` + Helm chart existingSecret refs |
| BL114 | Shared NFS / PVC / HostPath volumes | Cross-session artifact sharing, read-only-first pattern |
| BL115 | Pre-release K8s functional test suite | Live smoke verified; matrix in `docs/testing.md` |
| BL116 | Sessions list scheduled-commands badge | `ScheduleStore.CountForSession` + REST decorator + comm emoji |

Plus Sprint 8 stories S8.1–S8.7 (F10 wrap-up: secrets pluggability,
service mode, multi-cluster, audit trail, CF stub, idle timeout,
crash policy).

---

## Mobile API surface (closes datawatch-app issues #1, #2, #3)

```
POST   /api/devices/register           # issue #1
GET    /api/devices                    # issue #1
DELETE /api/devices/{id}               # issue #1
POST   /api/voice/transcribe           # issue #2 (multipart, whisper-backed)
GET    /api/federation/sessions        # issue #3 (parallel fan-out)
```

Schema details live in the per-handler `internal/server/*.go` files
+ the OpenAPI spec at `/api/openapi.yaml` (operator-pass task during
release validation).

---

## Breaking changes

**None intentional.** Every F10 addition is additive:

- The config schema grew (`agents.*` block); empty values preserve
  pre-F10 behaviour.
- Every new REST endpoint is additive; no existing endpoint's
  contract changed.
- Session records grew optional fields (`AgentID`, `scheduled_count`);
  older clients ignore them.

---

## Testing

- **943 tests / 48 packages**, all passing.
- **Live K8s smoke** against operator's 3-node cluster + NFS
  read-only mount — `docs/testing.md` §"Release Checkpoint — F10".
- **Operator-pass items** remain for the release host: image
  build/push, `spawn_docker.sh`, `spawn_k8s.sh RUN_BOOTSTRAP=1`,
  `helm install --dry-run`, UI walkthrough. All documented.

---

## Upgrading from v2.4.5

### Single host

```bash
datawatch update          # downloads v3.0.0 binary + restarts
```

State (`~/.datawatch/`) carries forward. The first boot runs the
BL93 reconciler (dry-run by default) and logs any orphan session
directories for review.

### Cluster (Helm)

```bash
helm upgrade dw datawatch/datawatch \
  -n datawatch \
  -f my-values.yaml \
  --set image.tag=v3.0.0
```

The schema for `my-values.yaml` gained optional fields
(`apiTokenExistingSecret`, `gitToken.existingSecret`,
`kubeconfig.existingSecret`). Pre-v3 values files work unchanged.

---

## What's next

- **BL117** — PRD-driven DAG orchestrator with guardrail sub-agents
  (deferred to post-release per the v3.0.0 scope).
- **F7** — libsignal native Go port (long-running).
- **F14** — Live cell DOM diffing (UI polish).
- **BL24 / BL25 / BL28 / BL39** — Intelligence items behind F15
  pipelines.

See `docs/plans/README.md` backlog table.
